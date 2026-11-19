package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Plan struct {
	ID           string    `json:"id"`
	Target       Target    `json:"target"`
	Identity     Row       `json:"identity"`
	Change       Change    `json:"change"`
	SQL          string    `json:"sql"`
	Warnings     []string  `json:"warnings"`
	Confirmation string    `json:"confirmation"`
	Expires      time.Time `json:"expires"`
	Snapshot     string    `json:"-"`
	used         bool
}
type Operation struct {
	ID       string     `json:"id"`
	State    string     `json:"state"`
	Action   string     `json:"action"`
	Target   Target     `json:"target"`
	JobID    string     `json:"jobId"`
	Message  string     `json:"message"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
}
type Engine struct {
	mu         sync.Mutex
	plans      map[string]*Plan
	operations map[string]Operation
	dir        string
	active     bool
	vault      tokenVault
}

func newEngine(dir string) (*Engine, error) {
	if dir == "" {
		return nil, errors.New("缺少 DBX_PLUGIN_DATA_DIR")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	return &Engine{plans: map[string]*Plan{}, operations: map[string]Operation{}, dir: dir, vault: newTokenVault()}, nil
}
func uid() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hash(v any) string { h := sha256.Sum256([]byte(canonical(v))); return hex.EncodeToString(h[:]) }
func (e *Engine) connect(ctx context.Context, t Target, token string) (*MCP, Row, error) {
	if err := validText(t.ConnectionID, 256, true); err != nil {
		return nil, nil, err
	}
	token, err := e.resolveToken(t.Endpoint, token)
	if err != nil {
		return nil, nil, err
	}
	m, err := newMCP(t.Endpoint, token)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*MCP, Row, error) { m.close(); return nil, nil, err }
	if err = m.initialize(ctx); err != nil {
		return fail(err)
	}
	rs, err := m.readBatch(ctx, t.ConnectionID, "msdb", identitySQL)
	if err != nil {
		return fail(err)
	}
	result, err := rows(rs)
	if err != nil || len(result) != 1 {
		return fail(errors.New("无法确认 SQL Server 实例身份"))
	}
	r := result[0]
	if field(r, "database_name") != "msdb" || field(r, "server_name") == "" || field(r, "product_version") == "" {
		return fail(errors.New("目标不是预期 SQL Server/msdb"))
	}
	if strings.Contains(strings.ToLower(field(r, "edition")), "express") {
		return fail(errors.New("Express 不支持 SQL Server Agent"))
	}
	if number(r, "is_sysadmin") != 1 && number(r, "agent_user") != 1 && number(r, "agent_reader") != 1 && number(r, "agent_operator") != 1 {
		return fail(errors.New("需要 msdb SQLAgentUserRole/ReaderRole/OperatorRole 或相应管理员权限"))
	}
	return m, r, nil
}
func fetch(ctx context.Context, m *MCP, t Target, resource, id string) ([]Row, error) {
	q, err := querySQL(resource, id)
	if err != nil {
		return nil, err
	}
	rs, err := m.readBatch(ctx, t.ConnectionID, "msdb", q)
	if err != nil {
		return nil, err
	}
	return rows(rs)
}
func (e *Engine) read(ctx context.Context, t Target, token, resource, id string) (any, error) {
	if resource == "detail" || resource == "steps" || resource == "schedules" || resource == "history" {
		if !guidRE.MatchString(id) {
			return nil, errors.New("请先选择作业")
		}
	}
	m, identity, err := e.connect(ctx, t, token)
	if err != nil {
		return nil, err
	}
	defer m.close()
	if resource == "identity" {
		return map[string]any{"identity": identity}, nil
	}
	if resource == "detail" {
		detail := map[string]any{"identity": identity}
		for _, part := range []string{"job", "steps", "schedules", "history"} {
			r, er := fetch(ctx, m, t, part, id)
			if er != nil {
				return nil, er
			}
			detail[part] = r
		}
		if len(detail["job"].([]Row)) != 1 {
			return nil, errors.New("作业不存在或不可见")
		}
		return detail, nil
	}
	r, err := fetch(ctx, m, t, resource, id)
	if err != nil {
		return nil, err
	}
	truncated := false
	if resource == "jobs" && len(r) > 2000 {
		truncated = true
		r = r[:2000]
	}
	return map[string]any{"identity": identity, "rows": r, "truncated": truncated}, nil
}
func snapshot(ctx context.Context, m *MCP, t Target, id string) (string, Row, []Row, error) {
	job, err := fetch(ctx, m, t, "job", id)
	if err != nil {
		return "", nil, nil, err
	}
	if len(job) != 1 {
		return "", nil, nil, errors.New("作业不存在或不可见")
	}
	parts := map[string]any{"job": job}
	steps, err := fetch(ctx, m, t, "steps", id)
	if err != nil {
		return "", nil, nil, err
	}
	stable := []Row{}
	for _, s := range steps {
		c := Row{}
		for k, v := range s {
			if !strings.HasPrefix(k, "last_run_") {
				c[k] = v
			}
		}
		stable = append(stable, c)
	}
	parts["steps"] = stable
	sch, err := fetch(ctx, m, t, "schedules", id)
	if err != nil {
		return "", nil, nil, err
	}
	stable = []Row{}
	for _, s := range sch {
		c := Row{}
		for k, v := range s {
			if k != "next_run_date" && k != "next_run_time" {
				c[k] = v
			}
		}
		stable = append(stable, c)
	}
	parts["schedules"] = stable
	return hash(parts), job[0], steps, nil
}
func checkExisting(c Change, steps []Row) error {
	if c.Action == "stepUpdate" || c.Action == "stepDelete" || c.Action == "startStep" {
		found := false
		for _, s := range steps {
			if c.Action == "startStep" && field(s, "step_name") == c.Step.Name || c.Action != "startStep" && number(s, "step_id") == c.Step.ID {
				found = true
				if c.Action == "stepUpdate" && field(s, "subsystem") != "TSQL" {
					return errors.New("此版本只编辑 T-SQL 步骤，其他子系统只读")
				}
			}
		}
		if !found {
			return errors.New("所选步骤不存在")
		}
	}
	if c.Action == "update" {
		found := false
		for _, s := range steps {
			if number(s, "step_id") == c.StartStep {
				found = true
			}
		}
		if !found {
			return errors.New("起始步骤不存在")
		}
	}
	if c.Action == "stepAdd" || c.Action == "stepUpdate" {
		for _, jump := range []struct{ action, id int }{{c.Step.Success, c.Step.SuccessStep}, {c.Step.Failure, c.Step.FailureStep}} {
			if jump.action == 4 {
				found := false
				for _, s := range steps {
					if number(s, "step_id") == jump.id {
						found = true
					}
				}
				if !found {
					return errors.New("跳转目标步骤不存在")
				}
			}
		}
	}
	return nil
}
func (e *Engine) plan(ctx context.Context, t Target, token string, c Change) (*Plan, error) {
	sql, warnings, err := buildChange(c)
	if err != nil {
		return nil, err
	}
	endpoint, err := normalizeEndpoint(t.Endpoint)
	if err != nil {
		return nil, err
	}
	t.Endpoint = endpoint
	m, identity, err := e.connect(ctx, t, token)
	if err != nil {
		return nil, err
	}
	defer m.close()
	stamp := ""
	name := c.Name
	if c.Action != "create" {
		var job Row
		var steps []Row
		stamp, job, steps, err = snapshot(ctx, m, t, c.JobID)
		if err != nil {
			return nil, err
		}
		name = field(job, "name")
		if err = checkExisting(c, steps); err != nil {
			return nil, err
		}
	}
	p := &Plan{ID: uid(), Target: t, Identity: identity, Change: c, SQL: sql, Warnings: warnings, Expires: time.Now().Add(10 * time.Minute), Snapshot: stamp, Confirmation: t.ConnectionID + " / " + field(identity, "server_name") + " / " + c.Action + " / " + name}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, old := range e.plans {
		if time.Now().After(old.Expires) {
			delete(e.plans, id)
		}
	}
	if len(e.plans) > 100 {
		return nil, errors.New("计划过多，请等待旧计划过期")
	}
	e.plans[p.ID] = p
	return p, nil
}
func (e *Engine) persist(op Operation) error {
	b, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(e.dir, "operation-"+op.ID+".json")
	f, err := os.OpenFile(p+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}
func (e *Engine) start(id, confirmation, token string) (Operation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.plans[id]
	if p == nil || p.used || time.Now().After(p.Expires) {
		return Operation{}, errors.New("计划不存在、已使用或过期；请重新预览")
	}
	if confirmation != p.Confirmation {
		return Operation{}, errors.New("确认文本不匹配")
	}
	if e.active {
		return Operation{}, errors.New("本插件已有操作进行中")
	}
	lock, err := os.OpenFile(filepath.Join(e.dir, ".agent-operation-lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Operation{}, errors.New("操作锁存在；可能仍在执行或上次异常退出，请先核验，不能自动重试")
	}
	lock.Close()
	op := Operation{ID: uid(), State: "running", Action: p.Change.Action, Target: p.Target, JobID: p.Change.JobID, Started: time.Now(), Message: "准备复核目标与作业定义"}
	if err = e.persist(op); err != nil {
		os.Remove(filepath.Join(e.dir, ".agent-operation-lock"))
		return Operation{}, err
	}
	p.used = true
	e.active = true
	e.operations[op.ID] = op
	go e.execute(*p, token, op)
	return op, nil
}
func (e *Engine) execute(p Plan, token string, op Operation) {
	state := "rejected"
	message := ""
	defer func() {
		if r := recover(); r != nil {
			state = "unknown"
			message = "内部异常，操作结果待核验"
		}
		now := time.Now()
		op.State = state
		op.Message = message
		op.Finished = &now
		e.mu.Lock()
		defer e.mu.Unlock()
		if err := e.persist(op); err != nil {
			op.State = "unknown"
			op.Message = "操作已结束但日志保存失败；核验服务器后再操作"
		} else if state != "unknown" {
			os.Remove(filepath.Join(e.dir, ".agent-operation-lock"))
		}
		e.operations[op.ID] = op
		e.active = false
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	m, identity, err := e.connect(ctx, p.Target, token)
	if err != nil {
		message = err.Error()
		return
	}
	defer m.close()
	if hash(identity) != hash(p.Identity) {
		message = "实例、登录身份或角色已变化，请重新预览"
		return
	}
	if p.Change.Action != "create" {
		stamp, _, _, er := snapshot(ctx, m, p.Target, p.Change.JobID)
		if er != nil {
			message = er.Error()
			return
		}
		if stamp != p.Snapshot {
			message = "作业、步骤或计划定义已改变，请重新预览"
			return
		}
	}
	state = "unknown" // Everything after submission may have reached the server, never retry automatically.
	_, err = m.batch(ctx, p.Target.ConnectionID, "msdb", p.SQL)
	if err != nil {
		message = m.clean(err).Error()
		return
	}
	state = "accepted"
	message = "SQL Server 已接受并完成本次管理调用；请刷新核对。启动/停止请求受理不代表作业最终成功/已停止。"
}
func (e *Engine) status(id string) (Operation, error) {
	if len(id) != 32 {
		return Operation{}, errors.New("操作 ID 无效")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return Operation{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if op, ok := e.operations[id]; ok {
		return op, nil
	}
	b, err := os.ReadFile(filepath.Join(e.dir, "operation-"+id+".json"))
	if err != nil {
		return Operation{}, errors.New("没有找到操作记录")
	}
	var op Operation
	if err = json.Unmarshal(b, &op); err != nil {
		return op, err
	}
	if op.State == "running" {
		op.State = "unknown"
		op.Message = "进程重启后结果不确定，请核验服务器，勿直接重试"
	}
	return op, nil
}
func (e *Engine) export(ctx context.Context, t Target, token, id string) (any, error) {
	m, identity, err := e.connect(ctx, t, token)
	if err != nil {
		return nil, err
	}
	defer m.close()
	_, job, steps, err := snapshot(ctx, m, t, id)
	if err != nil {
		return nil, err
	}
	name := field(job, "name") + "_copy"
	if len([]rune(name)) > 128 {
		return nil, errors.New("名称过长，请人工编辑脚本")
	}
	// One transaction for a portable disabled copy; unsupported subsystems never silently omitted.
	body := "DECLARE @job uniqueidentifier;\n" + proc("sp_add_job", "@job_name="+literal(name)+",@enabled=0,@description="+literal(field(job, "description"))+",@job_id=@job OUTPUT")
	for _, r := range steps {
		if field(r, "subsystem") != "TSQL" {
			return nil, errors.New("含非 T-SQL 步骤；为避免不完整导出，此版本拒绝自动生成副本")
		}
		s := Step{ID: number(r, "step_id"), Name: field(r, "step_name"), Database: field(r, "database_name"), Command: field(r, "command"), Success: number(r, "on_success_action"), SuccessStep: number(r, "on_success_step_id"), Failure: number(r, "on_fail_action"), FailureStep: number(r, "on_fail_step_id"), Retries: number(r, "retry_attempts"), RetryInterval: number(r, "retry_interval")}
		if err = checkStep(s, true); err != nil {
			return nil, err
		}
		if field(r, "output_file_name") != "" || number(r, "flags") != 0 || field(r, "database_user_name") != "" || number(r, "proxy_id") != 0 {
			return nil, errors.New("步骤含输出文件、标志、执行用户或代理设置，不能无损导出；请使用 SSMS 完整脚本")
		}
		body += proc("sp_add_jobstep", fmt.Sprintf("@job_id=@job,@step_id=%d,%s", s.ID, stepArgs(s)))
	}
	if len(steps) > 0 {
		body += proc("sp_update_job", fmt.Sprintf("@job_id=@job,@start_step_id=%d", number(job, "start_step_id")))
	}
	body += proc("sp_add_jobserver", "@job_id=@job,@server_name=N'(LOCAL)'")
	return map[string]any{"sql": "-- Disabled portable T-SQL job copy. REVIEW BEFORE EXECUTING.\n" + transaction(body), "name": name, "identity": identity, "warnings": []string{"不是完整备份：不导出 owner、类别、通知、删除策略或计划绑定；新副本默认禁用，禁止直接当原作业恢复脚本。"}}, nil
}

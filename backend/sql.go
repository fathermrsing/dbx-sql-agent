package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Row map[string]any
type Target struct {
	Endpoint     string `json:"endpoint"`
	ConnectionID string `json:"connectionId"`
}
type Step struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Database      string `json:"database"`
	Command       string `json:"command"`
	Success       int    `json:"success"`
	SuccessStep   int    `json:"successStep"`
	Failure       int    `json:"failure"`
	FailureStep   int    `json:"failureStep"`
	Retries       int    `json:"retries"`
	RetryInterval int    `json:"retryInterval"`
}
type Schedule struct {
	Name           string `json:"name"`
	Frequency      int    `json:"frequency"`
	Interval       int    `json:"interval"`
	Relative       int    `json:"relative"`
	Recurrence     int    `json:"recurrence"`
	Subday         int    `json:"subday"`
	SubdayInterval int    `json:"subdayInterval"`
	StartDate      int    `json:"startDate"`
	EndDate        int    `json:"endDate"`
	StartTime      int    `json:"startTime"`
	EndTime        int    `json:"endTime"`
}
type Change struct {
	Action      string   `json:"action"`
	JobID       string   `json:"jobId"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	StartStep   int      `json:"startStep"`
	Step        Step     `json:"step"`
	Schedule    Schedule `json:"schedule"`
	ScheduleID  int      `json:"scheduleId"`
}

var guidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func literal(s string) string { return "N'" + strings.ReplaceAll(s, "'", "''") + "'" }
func field(r Row, k string) string {
	if r[k] == nil {
		return ""
	}
	return fmt.Sprint(r[k])
}
func number(r Row, k string) int { n, _ := strconv.Atoi(field(r, k)); return n }
func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}
func validText(v string, limit int, required bool) error {
	if (required && strings.TrimSpace(v) == "") || len([]rune(v)) > limit || strings.ContainsRune(v, 0) {
		return errors.New("名称/文本为空、超长或包含 NUL")
	}
	return nil
}

const visibility = `(ISNULL(IS_SRVROLEMEMBER('sysadmin'),0)=1 OR ISNULL(IS_MEMBER('SQLAgentReaderRole'),0)=1 OR ISNULL(IS_MEMBER('SQLAgentOperatorRole'),0)=1 OR j.owner_sid=SUSER_SID())`
const identitySQL = `SELECT CAST(SERVERPROPERTY('ServerName') AS nvarchar(256)) AS server_name, CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)) AS product_version, CAST(SERVERPROPERTY('Edition') AS nvarchar(256)) AS edition, DB_NAME() AS database_name, ORIGINAL_LOGIN() AS login_name, IS_SRVROLEMEMBER('sysadmin') AS is_sysadmin, IS_MEMBER('SQLAgentUserRole') AS agent_user, IS_MEMBER('SQLAgentReaderRole') AS agent_reader, IS_MEMBER('SQLAgentOperatorRole') AS agent_operator;`

func querySQL(resource, id string) (string, error) {
	if id != "" && !guidRE.MatchString(id) {
		return "", errors.New("作业 ID 必须是 GUID")
	}
	scope := " WHERE " + visibility
	if id != "" {
		scope += " AND j.job_id=CONVERT(uniqueidentifier," + literal(id) + ")"
	}
	switch resource {
	case "identity":
		return identitySQL, nil
	case "jobs":
		return `SELECT TOP (2001) CONVERT(nvarchar(36),j.job_id) AS job_id,j.name,j.enabled,j.description,j.start_step_id,j.version_number,SUSER_SNAME(j.owner_sid) AS owner_name,c.name AS category,j.date_created,j.date_modified,
 h.run_status AS last_run_status,h.run_date AS last_run_date,h.run_time AS last_run_time,h.run_duration AS last_run_duration,
 CASE WHEN a.start_execution_date IS NOT NULL AND a.stop_execution_date IS NULL THEN 1 ELSE 0 END AS running,a.start_execution_date,a.last_executed_step_id,a.next_scheduled_run_date
 FROM msdb.dbo.sysjobs j LEFT JOIN msdb.dbo.syscategories c ON c.category_id=j.category_id
 OUTER APPLY (SELECT TOP(1) run_status,run_date,run_time,run_duration FROM msdb.dbo.sysjobhistory WHERE job_id=j.job_id AND step_id=0 ORDER BY instance_id DESC) h
 LEFT JOIN msdb.dbo.sysjobactivity a ON a.job_id=j.job_id AND a.session_id=(SELECT MAX(session_id) FROM msdb.dbo.syssessions)` + scope + ` ORDER BY j.name;`, nil
	case "job":
		if id == "" {
			return "", errors.New("缺少作业 ID")
		}
		return `SELECT CONVERT(nvarchar(36),j.job_id) AS job_id,j.name,j.enabled,j.description,j.start_step_id,j.version_number,SUSER_SNAME(j.owner_sid) AS owner_name,j.notify_level_email,j.notify_level_eventlog,j.notify_email_operator_id,j.delete_level FROM msdb.dbo.sysjobs j` + scope + `;`, nil
	case "steps":
		return `SELECT s.* FROM msdb.dbo.sysjobsteps s JOIN msdb.dbo.sysjobs j ON j.job_id=s.job_id` + scope + ` ORDER BY s.step_id;`, nil
	case "schedules":
		return `SELECT s.*, js.next_run_date,js.next_run_time,(SELECT COUNT(*) FROM msdb.dbo.sysjobschedules z WHERE z.schedule_id=s.schedule_id) AS attached_jobs FROM msdb.dbo.sysschedules s JOIN msdb.dbo.sysjobschedules js ON js.schedule_id=s.schedule_id JOIN msdb.dbo.sysjobs j ON j.job_id=js.job_id` + scope + ` ORDER BY s.schedule_id;`, nil
	case "history":
		return `SELECT TOP(500) h.instance_id,h.step_id,h.step_name,h.run_status,h.run_date,h.run_time,h.run_duration,h.sql_message_id,h.sql_severity,h.retries_attempted,h.message FROM msdb.dbo.sysjobhistory h JOIN msdb.dbo.sysjobs j ON j.job_id=h.job_id` + scope + ` ORDER BY h.instance_id DESC;`, nil
	case "operators":
		return `SELECT id,name,enabled,email_address,last_email_date,last_email_time FROM msdb.dbo.sysoperators ORDER BY name;`, nil
	case "alerts":
		return `SELECT id,name,enabled,message_id,severity,database_name,delay_between_responses,occurrence_count,last_occurrence_date,last_occurrence_time FROM msdb.dbo.sysalerts ORDER BY name;`, nil
	case "categories":
		return `SELECT category_id,name,category_type FROM msdb.dbo.syscategories WHERE category_class=1 ORDER BY name;`, nil
	case "databases":
		return `SELECT name FROM sys.databases WHERE state=0 AND HAS_DBACCESS(name)=1 ORDER BY name;`, nil
	case "service":
		// Dynamic SQL prevents name resolution on versions without this DMV.
		return `EXEC(N'IF OBJECT_ID(N''sys.dm_server_services'') IS NOT NULL EXEC(N''SELECT servicename,startup_type_desc,status_desc,last_startup_time FROM sys.dm_server_services WHERE servicename LIKE ''''SQL Server Agent%'''';''); ELSE SELECT N''SQL Server Agent'' AS servicename,N''不可用'' AS startup_type_desc,N''此版本未提供服务 DMV 或当前登录不可见；不代表 Agent 已停止'' AS status_desc,CAST(NULL AS datetime) AS last_startup_time;');`, nil
	}
	return "", errors.New("未知查询资源")
}
func checkStep(s Step, update bool) error {
	if update && s.ID < 1 {
		return errors.New("编辑步骤需要有效 ID")
	}
	if s.ID < 0 || s.ID > 9999 {
		return errors.New("步骤 ID 越界")
	}
	if err := validText(s.Name, 128, true); err != nil {
		return err
	}
	if err := validText(s.Database, 128, true); err != nil {
		return err
	}
	if err := validText(s.Command, 500000, true); err != nil {
		return err
	}
	if s.Success < 1 || s.Success > 4 || s.Failure < 1 || s.Failure > 4 {
		return errors.New("步骤结果动作必须为 1..4")
	}
	if s.SuccessStep < 0 || s.SuccessStep > 9999 || s.FailureStep < 0 || s.FailureStep > 9999 || s.Success == 4 && s.SuccessStep < 1 || s.Failure == 4 && s.FailureStep < 1 {
		return errors.New("跳转步骤无效")
	}
	if s.Retries < 0 || s.Retries > 9999 || s.RetryInterval < 0 || s.RetryInterval > 9999 {
		return errors.New("重试参数越界")
	}
	return nil
}
func validDate(n int) bool { _, e := time.Parse("20060102", strconv.Itoa(n)); return e == nil }
func validTime(n int) bool { return n >= 0 && n/10000 < 24 && n/100%100 < 60 && n%100 < 60 }
func checkSchedule(s Schedule) error {
	if err := validText(s.Name, 128, true); err != nil {
		return err
	}
	if !validDate(s.StartDate) || !validDate(s.EndDate) || s.StartDate > s.EndDate || !validTime(s.StartTime) || !validTime(s.EndTime) || s.StartTime > s.EndTime {
		return errors.New("计划日期/时间无效（YYYYMMDD / HHMMSS，使用服务器时间）")
	}
	switch s.Frequency {
	case 1:
	case 4:
		if s.Interval < 1 || s.Interval > 9999 {
			return errors.New("每日间隔无效")
		}
	case 8:
		if s.Interval < 1 || s.Interval > 127 || s.Recurrence < 1 || s.Recurrence > 99 {
			return errors.New("每周位掩码/周间隔无效")
		}
	case 16:
		if s.Interval < 1 || s.Interval > 31 || s.Recurrence < 1 || s.Recurrence > 99 {
			return errors.New("每月日期/间隔无效")
		}
	case 32:
		if s.Interval < 1 || s.Interval > 10 || s.Recurrence < 1 || s.Recurrence > 99 || !(s.Relative == 1 || s.Relative == 2 || s.Relative == 4 || s.Relative == 8 || s.Relative == 16) {
			return errors.New("每月相对日期无效")
		}
	case 64, 128:
	default:
		return errors.New("不支持的频率类型")
	}
	if !(s.Subday == 1 || s.Subday == 2 || s.Subday == 4 || s.Subday == 8) || s.Subday != 1 && (s.SubdayInterval < 1 || s.SubdayInterval > 9999) || s.Subday == 2 && s.SubdayInterval < 10 {
		return errors.New("日内频率无效（秒间隔至少 10）")
	}
	return nil
}
func proc(name, args string) string {
	return "EXEC @rc=msdb.dbo." + name + " " + args + "; IF @rc<>0 BEGIN RAISERROR(N'Agent 存储过程失败',16,1); RETURN; END;\n"
}
func transaction(body string) string {
	return "EXEC(" + literal("SET XACT_ABORT ON;\nBEGIN TRY\nBEGIN TRANSACTION;\nDECLARE @rc int;\n"+body+"COMMIT;\nEND TRY\nBEGIN CATCH\nIF @@TRANCOUNT>0 ROLLBACK;\nDECLARE @agent_error nvarchar(4000);\nSET @agent_error=ERROR_MESSAGE();\nRAISERROR(N'%s',16,1,@agent_error);\nRETURN;\nEND CATCH;") + ");"
}
func stepArgs(s Step) string {
	return fmt.Sprintf("@step_name=%s,@subsystem=N'TSQL',@database_name=%s,@command=%s,@on_success_action=%d,@on_success_step_id=%d,@on_fail_action=%d,@on_fail_step_id=%d,@retry_attempts=%d,@retry_interval=%d", literal(s.Name), literal(s.Database), literal(s.Command), s.Success, s.SuccessStep, s.Failure, s.FailureStep, s.Retries, s.RetryInterval)
}
func buildChange(c Change) (string, []string, error) {
	warnings := []string{"实际权限由 SQL Server 验证；作业执行身份可能不同于当前登录。", "请求超时或断线不自动重试，先核对服务器状态。"}
	if c.Action != "create" && !guidRE.MatchString(c.JobID) {
		return "", nil, errors.New("作业 ID 无效")
	}
	job := "@job_id=@job"
	prefix := "DECLARE @job uniqueidentifier=" + literal(c.JobID) + ";\n"
	body := ""
	switch c.Action {
	case "create":
		if e := validText(c.Name, 128, true); e != nil {
			return "", nil, e
		}
		if e := validText(c.Description, 512, false); e != nil {
			return "", nil, e
		}
		prefix = "DECLARE @job uniqueidentifier;\n"
		body = proc("sp_add_job", fmt.Sprintf("@job_name=%s,@enabled=0,@description=%s,@job_id=@job OUTPUT", literal(c.Name), literal(c.Description))) + proc("sp_add_jobserver", job+",@server_name=N'(LOCAL)'")
		warnings = append(warnings, "新作业默认禁用；添加步骤和计划后再单独启用。")
	case "update":
		if e := validText(c.Name, 128, true); e != nil {
			return "", nil, e
		}
		if e := validText(c.Description, 512, false); e != nil {
			return "", nil, e
		}
		if c.StartStep < 1 || c.StartStep > 9999 {
			return "", nil, errors.New("起始步骤无效")
		}
		body = proc("sp_update_job", fmt.Sprintf("%s,@new_name=%s,@description=%s,@start_step_id=%d", job, literal(c.Name), literal(c.Description), c.StartStep))
	case "enable", "disable":
		body = proc("sp_update_job", fmt.Sprintf("%s,@enabled=%d", job, bit(c.Action == "enable")))
	case "delete":
		body = proc("sp_delete_job", job+",@delete_unused_schedule=0")
		warnings = append(warnings, "删除作业及其历史，不删除共享或未使用的计划；请先导出备份。")
	case "start", "stop":
		body = proc("sp_"+c.Action+"_job", job)
		if c.Action == "start" && c.Step.ID > 0 {
			return "", nil, errors.New("指定起始步骤请使用 startStep 操作")
		}
		return "EXEC(" + literal(prefix+"DECLARE @rc int;\n"+body) + ");", warnings, nil
	case "startStep":
		if e := validText(c.Step.Name, 128, true); e != nil {
			return "", nil, e
		}
		body = proc("sp_start_job", job+",@step_name="+literal(c.Step.Name))
		return "EXEC(" + literal(prefix+"DECLARE @rc int;\n"+body) + ");", warnings, nil
	case "stepAdd", "stepUpdate":
		if e := checkStep(c.Step, c.Action == "stepUpdate"); e != nil {
			return "", nil, e
		}
		method := "sp_add_jobstep"
		args := job + "," + stepArgs(c.Step)
		if c.Action == "stepUpdate" {
			method = "sp_update_jobstep"
			args += fmt.Sprintf(",@step_id=%d", c.Step.ID)
		}
		body = proc(method, args)
		warnings = append(warnings, "仅新增/编辑 T-SQL 步骤；编辑不会主动清除已有高级输出/执行身份设置。")
	case "stepDelete":
		if c.Step.ID < 1 || c.Step.ID > 9999 {
			return "", nil, errors.New("步骤 ID 无效")
		}
		body = proc("sp_delete_jobstep", fmt.Sprintf("%s,@step_id=%d", job, c.Step.ID))
		warnings = append(warnings, "删除步骤可能改变步骤编号及跳转关系，完成后重新审核所有步骤。")
	case "scheduleCreate":
		if e := checkSchedule(c.Schedule); e != nil {
			return "", nil, e
		}
		s := c.Schedule
		body = "DECLARE @sid int;\n" + proc("sp_add_schedule", fmt.Sprintf("@schedule_name=%s,@enabled=1,@freq_type=%d,@freq_interval=%d,@freq_subday_type=%d,@freq_subday_interval=%d,@freq_relative_interval=%d,@freq_recurrence_factor=%d,@active_start_date=%d,@active_end_date=%d,@active_start_time=%d,@active_end_time=%d,@schedule_id=@sid OUTPUT", literal(s.Name), s.Frequency, s.Interval, s.Subday, s.SubdayInterval, s.Relative, s.Recurrence, s.StartDate, s.EndDate, s.StartTime, s.EndTime)) + proc("sp_attach_schedule", job+",@schedule_id=@sid")
		warnings = append(warnings, "新计划立即启用；若作业已启用，将可能按服务器时间触发。")
	case "scheduleAttach", "scheduleDetach":
		if c.ScheduleID < 1 {
			return "", nil, errors.New("计划 ID 无效")
		}
		method := "sp_attach_schedule"
		extra := ""
		if c.Action == "scheduleDetach" {
			method = "sp_detach_schedule"
			extra = ",@delete_unused_schedule=0"
		}
		body = proc(method, fmt.Sprintf("%s,@schedule_id=%d%s", job, c.ScheduleID, extra))
		warnings = append(warnings, "只更改此作业的绑定；不编辑或删除共享计划本体。")
	default:
		return "", nil, errors.New("不支持的操作")
	}
	return transaction(prefix + body), warnings, nil
}
func rows(results []BatchResult) ([]Row, error) {
	out := []Row{}
	for _, result := range results {
		for _, values := range result.Rows {
			if len(values) != len(result.Columns) {
				return nil, errors.New("结果列数不匹配")
			}
			r := Row{}
			for i, k := range result.Columns {
				r[k] = values[i]
			}
			out = append(out, r)
		}
	}
	return out, nil
}
func canonical(v any) string { b, _ := json.Marshal(v); return string(b) }

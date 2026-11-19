package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testJob = "11111111-2222-3333-4444-555555555555"

type fixture struct {
	server          *httptest.Server
	writes          atomic.Int32
	version         atomic.Int32
	fail            atomic.Bool
	identity        atomic.Bool
	unsupported     atomic.Bool
	admin           atomic.Bool
	stepsChanged    atomic.Bool
	scheduleChanged atomic.Bool
	loginChanged    atomic.Bool
}

func mock(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.version.Store(1)
	f.admin.Store(true)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Arguments struct {
					SQL        string `json:"sql"`
					DB         string `json:"database"`
					Connection string `json:"connection_id"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-11-25"}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]string{"name": "dbx_execute_batch"}}}
		case "tools/call":
			a := req.Params.Arguments
			if a.DB != "msdb" || a.Connection != "test" {
				t.Error("incorrect DBX target")
			}
			rs := []Row{}
			q := a.SQL
			bad := false
			switch {
			case strings.Contains(q, "SERVERPROPERTY"):
				db := "msdb"
				if f.identity.Load() {
					db = "wrong"
				}
				admin := 1
				if !f.admin.Load() {
					admin = 0
				}
				login := "test-login"
				if f.loginChanged.Load() {
					login = "other-login"
				}
				rs = []Row{{"server_name": "SQL-TEST", "product_version": "16.0", "edition": "Developer", "database_name": db, "login_name": login, "is_sysadmin": admin, "agent_user": 0, "agent_reader": 0, "agent_operator": 0}}
			case strings.Contains(q, "sys.dm_server_services"):
				rs = []Row{}
			case strings.HasPrefix(q, "EXEC("):
				f.writes.Add(1)
				bad = f.fail.Load()
			case strings.Contains(q, "SELECT s.* FROM msdb.dbo.sysjobsteps"):
				command := "SELECT N'你好';\nGO\nSELECT 2;"
				if f.stepsChanged.Load() {
					command = "SELECT 99"
				}
				sub := "TSQL"
				if f.unsupported.Load() {
					sub = "CmdExec"
				}
				rs = []Row{{"step_id": 1, "step_name": "read", "subsystem": sub, "database_name": "sandbox", "command": command, "on_success_action": 1, "on_success_step_id": 0, "on_fail_action": 2, "on_fail_step_id": 0, "retry_attempts": 0, "retry_interval": 0, "last_run_date": 0, "flags": 0, "proxy_id": 0}}
			case strings.Contains(q, "FROM msdb.dbo.sysschedules"):
				name := "daily"
				if f.scheduleChanged.Load() {
					name = "modified"
				}
				rs = []Row{{"schedule_id": 3, "name": name, "enabled": 1, "next_run_time": 100000}}
			case strings.Contains(q, "TOP(500)"):
				rs = []Row{{"instance_id": 1, "step_id": 0, "run_status": 1, "message": "done"}}
			case strings.Contains(q, "TOP (2001)"):
				rs = []Row{{"job_id": testJob, "name": "Test job", "enabled": 0, "running": 0, "owner_name": "test-login"}}
			case strings.Contains(q, "FROM msdb.dbo.sysjobs j"):
				rs = []Row{{"job_id": testJob, "name": "Test job", "enabled": 0, "description": "test", "start_step_id": 1, "version_number": f.version.Load(), "owner_name": "test-login"}}
			default:
				rs = []Row{}
			}
			cols := []string{}
			values := [][]any{}
			if len(rs) > 0 {
				for k := range rs[0] {
					cols = append(cols, k)
				}
				for _, r := range rs {
					row := []any{}
					for _, k := range cols {
						row = append(row, r[k])
					}
					values = append(values, row)
				}
			}
			item := map[string]any{"columns": cols, "rows": values}
			if bad {
				item["execution_error"] = true
				item["error_message"] = "simulated secret failure"
			}
			result = map[string]any{"structuredContent": map[string]any{"results": []any{item}}}
		default:
			t.Error("unexpected method")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(f.server.Close)
	return f
}
func env(t *testing.T) (*Engine, *fixture, Target) {
	t.Helper()
	e, err := newEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := mock(t)
	return e, f, Target{Endpoint: f.server.URL, ConnectionID: "test"}
}
func waitOp(t *testing.T, e *Engine, id string) Operation {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		op, err := e.status(id)
		if err != nil {
			t.Fatal(err)
		}
		if op.State != "running" {
			return op
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("operation timeout")
	return Operation{}
}
func planAction(t *testing.T, e *Engine, target Target, action string) *Plan {
	t.Helper()
	p, err := e.plan(context.Background(), target, "secret", Change{Action: action, JobID: testJob})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestReadOnlyNoWrites(t *testing.T) {
	e, f, target := env(t)
	for _, resource := range []string{"identity", "jobs", "detail", "operators", "alerts", "categories", "databases", "service", "steps", "schedules", "history"} {
		if _, err := e.read(context.Background(), target, "secret", resource, testJob); err != nil {
			t.Fatal(resource, err)
		}
	}
	if f.writes.Load() != 0 {
		t.Fatal("read wrote")
	}
}
func TestPreviewConfirmationAndOnce(t *testing.T) {
	e, f, target := env(t)
	p := planAction(t, e, target, "start")
	if f.writes.Load() != 0 {
		t.Fatal("preview wrote")
	}
	if _, err := e.start(p.ID, "wrong", "secret"); err == nil {
		t.Fatal("wrong confirm accepted")
	}
	op, err := e.start(p.ID, p.Confirmation, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitOp(t, e, op.ID); got.State != "accepted" {
		t.Fatal(got)
	}
	if f.writes.Load() != 1 {
		t.Fatal(f.writes.Load())
	}
	if _, err = e.start(p.ID, p.Confirmation, "secret"); err == nil {
		t.Fatal("replay")
	}
}
func TestExpiredPlan(t *testing.T) {
	e, f, target := env(t)
	p := planAction(t, e, target, "stop")
	p.Expires = time.Now().Add(-time.Second)
	if _, err := e.start(p.ID, p.Confirmation, "secret"); err == nil || f.writes.Load() != 0 {
		t.Fatal("expired")
	}
}
func TestChangedDefinitionsReject(t *testing.T) {
	for _, part := range []string{"job", "steps", "schedule", "login"} {
		t.Run(part, func(t *testing.T) {
			e, f, target := env(t)
			p := planAction(t, e, target, "enable")
			switch part {
			case "job":
				f.version.Add(1)
			case "steps":
				f.stepsChanged.Store(true)
			case "schedule":
				f.scheduleChanged.Store(true)
			case "login":
				f.loginChanged.Store(true)
			}
			op, err := e.start(p.ID, p.Confirmation, "secret")
			if err != nil {
				t.Fatal(err)
			}
			if waitOp(t, e, op.ID).State != "rejected" || f.writes.Load() != 0 {
				t.Fatal("stale definition submitted")
			}
		})
	}
}
func TestFailureUnknownRetainsLock(t *testing.T) {
	e, f, target := env(t)
	p := planAction(t, e, target, "enable")
	f.fail.Store(true)
	op, _ := e.start(p.ID, p.Confirmation, "secret")
	got := waitOp(t, e, op.ID)
	if got.State != "unknown" || strings.Contains(got.Message, "secret") {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".agent-operation-lock")); err != nil {
		t.Fatal(err)
	}
	next := planAction(t, e, target, "enable")
	if _, err := e.start(next.ID, next.Confirmation, "secret"); err == nil {
		t.Fatal("unknown not blocked")
	}
}
func TestRestartUnknown(t *testing.T) {
	e, _, _ := env(t)
	op := Operation{ID: uid(), State: "running"}
	if err := e.persist(op); err != nil {
		t.Fatal(err)
	}
	got, err := e.status(op.ID)
	if err != nil || got.State != "unknown" {
		t.Fatal(got, err)
	}
	if _, err = e.status("../escape"); err == nil {
		t.Fatal("unsafe operation path")
	}
}
func TestIdentityAndRoleRejected(t *testing.T) {
	for _, kind := range []string{"db", "role", "token"} {
		t.Run(kind, func(t *testing.T) {
			e, f, target := env(t)
			token := "secret"
			if kind == "db" {
				f.identity.Store(true)
			}
			if kind == "role" {
				f.admin.Store(false)
			}
			if kind == "token" {
				token = "wrong"
			}
			if _, err := e.read(context.Background(), target, token, "jobs", ""); err == nil {
				t.Fatal("bad identity accepted")
			}
		})
	}
}
func TestNonTSQLNotEditedOrExported(t *testing.T) {
	e, f, target := env(t)
	f.unsupported.Store(true)
	s := validStep()
	_, err := e.plan(context.Background(), target, "secret", Change{Action: "stepUpdate", JobID: testJob, Step: s})
	if err == nil {
		t.Fatal("non-tsql edited")
	}
	if _, err = e.export(context.Background(), target, "secret", testJob); err == nil {
		t.Fatal("partial export")
	}
}
func TestPortableDisabledExport(t *testing.T) {
	e, f, target := env(t)
	r, err := e.export(context.Background(), target, "secret", testJob)
	if err != nil {
		t.Fatal(err)
	}
	v := r.(map[string]any)
	sql := v["sql"].(string)
	if !strings.Contains(sql, "@enabled=0") || !strings.Contains(sql, "_copy") || !strings.Contains(sql, "sp_add_jobstep") || f.writes.Load() != 0 {
		t.Fatal(sql)
	}
}
func validStep() Step {
	return Step{ID: 1, Name: "Step", Database: "sandbox", Command: "SELECT 1", Success: 1, Failure: 2}
}
func validSchedule() Schedule {
	return Schedule{Name: "daily", Frequency: 4, Interval: 1, Recurrence: 1, Relative: 1, Subday: 1, SubdayInterval: 1, StartDate: 20261119, EndDate: 99991231, StartTime: 90000, EndTime: 235959}
}
func TestAllChangeSQL(t *testing.T) {
	for _, action := range []string{"create", "update", "enable", "disable", "delete", "start", "stop", "startStep", "stepAdd", "stepUpdate", "stepDelete", "scheduleCreate", "scheduleAttach", "scheduleDetach"} {
		t.Run(action, func(t *testing.T) {
			c := Change{Action: action, JobID: testJob, Name: "O'Brien", Description: "description", StartStep: 1, Step: validStep(), Schedule: validSchedule(), ScheduleID: 3}
			if action == "start" {
				c.Step.ID = 0
			}
			q, w, err := buildChange(c)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(q, "EXEC(N'") || len(w) < 2 || strings.Contains(q, "USE ") {
				t.Fatal(q)
			}
			if strings.Contains(q, "@delete_unused_schedule=1") {
				t.Fatal("shared schedule destruction")
			}
		})
	}
}
func TestLiteralAndCommandPreserved(t *testing.T) {
	s := validStep()
	s.Command = "SELECT N'x';\nGO\nEXEC dbo.example @x=N'O''Brien';"
	q, _, err := buildChange(Change{Action: "stepAdd", JobID: testJob, Step: s})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "N''''O''''''''Brien''''") || !strings.Contains(q, "\nGO\n") {
		t.Fatal("escaping lost", q)
	}
	if literal("a'b") != "N'a''b'" {
		t.Fatal("literal")
	}
}
func TestInvalidInputs(t *testing.T) {
	bad := []Change{{Action: "raw", JobID: testJob}, {Action: "start", JobID: "x';DROP TABLE x;--"}, {Action: "create", Name: ""}, {Action: "stepUpdate", JobID: testJob}, {Action: "stepDelete", JobID: testJob}, {Action: "scheduleAttach", JobID: testJob}, {Action: "update", JobID: testJob, Name: "x"}}
	for _, c := range bad {
		if _, _, err := buildChange(c); err == nil {
			t.Fatal(c)
		}
	}
	if _, err := querySQL("raw", testJob); err == nil {
		t.Fatal("raw query")
	}
	if _, err := querySQL("steps", "injection"); err == nil {
		t.Fatal("guid validation")
	}
}
func TestScheduleValidation(t *testing.T) {
	for _, f := range []int{1, 4, 8, 16, 32, 64, 128} {
		s := validSchedule()
		s.Frequency = f
		if err := checkSchedule(s); err != nil {
			t.Fatal(f, err)
		}
	}
	for _, mutate := range []func(*Schedule){func(s *Schedule) { s.StartDate = 20260230 }, func(s *Schedule) { s.EndDate = 20200101 }, func(s *Schedule) { s.StartTime = 246000 }, func(s *Schedule) { s.Subday = 2; s.SubdayInterval = 1 }, func(s *Schedule) { s.Frequency = 999 }, func(s *Schedule) { s.Frequency = 8; s.Interval = 128 }, func(s *Schedule) { s.Frequency = 32; s.Relative = 3 }} {
		s := validSchedule()
		mutate(&s)
		if checkSchedule(s) == nil {
			t.Fatal(s)
		}
	}
}
func TestStepValidation(t *testing.T) {
	for _, mutate := range []func(*Step){func(s *Step) { s.Database = "" }, func(s *Step) { s.Command = "" }, func(s *Step) { s.Success = 0 }, func(s *Step) { s.Failure = 4; s.FailureStep = 0 }, func(s *Step) { s.Retries = -1 }, func(s *Step) { s.ID = -1 }} {
		s := validStep()
		mutate(&s)
		if checkStep(s, true) == nil {
			t.Fatal(s)
		}
	}
}
func TestJumpAndStartStepChecks(t *testing.T) {
	steps := []Row{{"step_id": 1, "step_name": "one", "subsystem": "TSQL"}}
	s := validStep()
	s.Success = 4
	s.SuccessStep = 9
	if checkExisting(Change{Action: "stepUpdate", Step: s}, steps) == nil {
		t.Fatal("missing jump")
	}
	if checkExisting(Change{Action: "startStep", Step: Step{Name: "missing"}}, steps) == nil {
		t.Fatal("missing start step")
	}
}
func TestResultShape(t *testing.T) {
	if _, err := rows([]BatchResult{{Columns: []string{"x"}, Rows: [][]any{{1, 2}}}}); err == nil {
		t.Fatal("shape")
	}
	rs, err := rows([]BatchResult{{Columns: []string{"x"}, Rows: [][]any{{"<script>"}}}})
	if err != nil || rs[0]["x"] != "<script>" {
		t.Fatal(rs, err)
	}
}
func TestEndpointGuards(t *testing.T) {
	for _, s := range []string{"https://127.0.0.1/mcp", "http://example.com/mcp", "http://user:pass@localhost/mcp", "http://localhost/mcp?token=x"} {
		if _, err := normalizeEndpoint(s); err == nil {
			t.Fatal(s)
		}
	}
	if _, err := newMCP("http://localhost:5225/mcp", "a\nb"); err == nil {
		t.Fatal("token header injection")
	}
}
func TestMCPTransport(t *testing.T) {
	for _, kind := range []string{"sse", "wrong-id", "unstructured", "redirect", "iserror"} {
		t.Run(kind, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID int `json:"id"`
				}
				json.NewDecoder(r.Body).Decode(&req)
				switch kind {
				case "redirect":
					http.Redirect(w, r, "http://example.com", 302)
				case "sse":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{}}\n\n", req.ID)
				case "wrong-id":
					fmt.Fprint(w, `{"jsonrpc":"2.0","id":999,"result":{}}`)
				case "unstructured":
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{}}`, req.ID)
				case "iserror":
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"isError":true,"content":[{"type":"text","text":"secret denied"}]}}`, req.ID)
				}
			}))
			defer s.Close()
			m, _ := newMCP(s.URL, "secret")
			defer m.close()
			if kind == "sse" {
				if err := m.rpc(context.Background(), "test", nil, nil); err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := m.batch(context.Background(), "test", "msdb", "SELECT 1")
				if err == nil || strings.Contains(err.Error(), "secret") {
					t.Fatal(err)
				}
			}
		})
	}
}

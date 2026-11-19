package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type MCP struct {
	URL, Token, Protocol, Session string
	client                        *http.Client
	seq                           atomic.Int64
}
type BatchResult struct {
	Columns        []string `json:"columns"`
	ExecutionError bool     `json:"execution_error"`
	ErrorMessage   string   `json:"error_message"`
	Rows           [][]any  `json:"rows"`
	Outcome        string   `json:"transaction_outcome"`
}
type ToolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Structured *struct {
		Results []BatchResult `json:"results"`
	} `json:"structuredContent"`
}

func normalizeEndpoint(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", errors.New("MCP 地址无效")
	}
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("仅支持无认证参数的本机 http MCP 地址")
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	} else {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", errors.New("MCP 必须使用本机回环地址")
		}
	}
	if u.Port() == "" {
		u.Host = host
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		}
	} else {
		u.Host = net.JoinHostPort(host, u.Port())
	}
	if u.Path == "" {
		u.Path = "/mcp"
	}
	return u.String(), nil
}
func newMCP(endpoint, token string) (*MCP, error) {
	u, e := normalizeEndpoint(endpoint)
	if e != nil {
		return nil, e
	}
	if strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("令牌包含无效字符")
	}
	if token == "" {
		return nil, errors.New("请填写 DBX MCP Bearer Token，或设置 DBX_AGENT_MCP_TOKEN")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Minute}
	return &MCP{URL: u, Token: token, client: &http.Client{Transport: transport, Timeout: 6 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("拒绝 MCP 重定向") }}}, nil
}
func (m *MCP) close() { m.client.CloseIdleConnections() }
func (m *MCP) clean(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), m.Token, "[REDACTED]"))
}
func (m *MCP) rpc(ctx context.Context, method string, params any, out any) error {
	id := m.seq.Add(1)
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, e := http.NewRequestWithContext(ctx, "POST", m.URL, bytes.NewReader(payload))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+m.Token)
	if m.Protocol != "" {
		req.Header.Set("MCP-Protocol-Version", m.Protocol)
	}
	if m.Session != "" {
		req.Header.Set("Mcp-Session-Id", m.Session)
	}
	resp, e := m.client.Do(req)
	if e != nil {
		return m.clean(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("MCP HTTP %d；请检查服务状态、令牌和权限", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024+1))
	if e != nil {
		return m.clean(e)
	}
	if len(b) > 16*1024*1024 {
		return errors.New("MCP 响应超过 16 MiB")
	}
	if v := resp.Header.Get("Mcp-Session-Id"); v != "" {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("MCP session header 无效")
		}
		m.Session = v
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		// Join multiline SSE data fields; ignore notifications, match this request id.
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 4096), 16*1024*1024)
		var event []string
		found := false
		consume := func() {
			if len(event) == 0 {
				return
			}
			data := []byte(strings.Join(event, "\n"))
			var env struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &env) == nil && env.ID == id {
				b = append([]byte(nil), data...)
				found = true
			}
			event = nil
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				consume()
			} else if strings.HasPrefix(line, "data:") {
				event = append(event, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		consume()
		if e = sc.Err(); e != nil {
			return e
		}
		if !found {
			return errors.New("MCP SSE 未返回匹配的响应")
		}
	}
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if e = json.Unmarshal(b, &env); e != nil {
		return errors.New("MCP 返回非 JSON 响应")
	}
	if env.JSONRPC != "2.0" || env.ID != id {
		return errors.New("MCP 响应协议或 ID 不匹配")
	}
	if env.Error != nil {
		return m.clean(fmt.Errorf("MCP %d: %s", env.Error.Code, env.Error.Message))
	}
	if len(env.Result) == 0 {
		return errors.New("MCP 响应缺少 result")
	}
	if out != nil {
		if e = json.Unmarshal(env.Result, out); e != nil {
			return errors.New("MCP result 格式不兼容")
		}
	}
	return nil
}
func (m *MCP) initialize(ctx context.Context) error {
	var init struct {
		Protocol string `json:"protocolVersion"`
	}
	if e := m.rpc(ctx, "initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "dbx-sql-agent", "version": "0.1.3"}}, &init); e != nil {
		return e
	}
	if init.Protocol != "2025-11-25" && init.Protocol != "2025-06-18" && init.Protocol != "2025-03-26" {
		return fmt.Errorf("不支持 MCP 协议 %s", init.Protocol)
	}
	m.Protocol = init.Protocol
	// DBX's HTTP endpoint is stateless: no initialized notification/session needed.
	var listing struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Cursor string `json:"nextCursor"`
	}
	cursor := ""
	for page := 0; page < 20; page++ {
		p := map[string]any{}
		if cursor != "" {
			p["cursor"] = cursor
		}
		listing.Tools = nil
		listing.Cursor = ""
		if e := m.rpc(ctx, "tools/list", p, &listing); e != nil {
			return e
		}
		for _, t := range listing.Tools {
			if t.Name == "dbx_execute_batch" {
				return nil
			}
		}
		if listing.Cursor == "" || listing.Cursor == cursor {
			break
		}
		cursor = listing.Cursor
	}
	return errors.New("DBX MCP 未开放 dbx_execute_batch；请检查工具白名单及连接授权")
}
func (m *MCP) batch(ctx context.Context, connection, database, sql string) ([]BatchResult, error) {
	return m.batchResult(ctx, connection, database, sql, false)
}

// Only trusted, internally generated read queries use this compatibility path.
// Never infer read-only status from arbitrary SQL text or use it for mutations.
func (m *MCP) readBatch(ctx context.Context, connection, database, sql string) ([]BatchResult, error) {
	return m.batchResult(ctx, connection, database, sql, true)
}

func (m *MCP) batchResult(ctx context.Context, connection, database, sql string, readOnly bool) ([]BatchResult, error) {
	var r ToolResult
	e := m.rpc(ctx, "tools/call", map[string]any{"name": "dbx_execute_batch", "arguments": map[string]any{"connection_id": connection, "database": database, "sql": sql, "continue_on_error": false, "use_transaction": false}}, &r)
	if e != nil {
		return nil, e
	}
	if r.IsError {
		msg := "DBX 拒绝执行"
		for _, c := range r.Content {
			if c.Type == "text" {
				msg += "\n" + c.Text
			}
		}
		return nil, m.clean(errors.New(msg))
	}
	if r.Structured == nil || len(r.Structured.Results) == 0 {
		return nil, errors.New("DBX 未提供结构化批次结果；执行结果需人工核验")
	}
	for _, b := range r.Structured.Results {
		if !b.ExecutionError && (b.Columns == nil || b.Rows == nil) {
			return nil, errors.New("批次结果缺少 columns/rows，必须人工核验")
		}
		// DBX backend.rs historically populated error_message from rows[0][0]
		// even on success. Tolerate only that exact shape for internal reads.
		legacyMessage := false
		if readOnly && len(b.Columns) > 0 && len(b.Rows) > 0 && len(b.Rows[0]) == len(b.Columns) {
			first, ok := b.Rows[0][0].(string)
			legacyMessage = ok && first == b.ErrorMessage
		}
		for _, row := range b.Rows {
			if len(row) != len(b.Columns) {
				return nil, errors.New("批次结果行列数量不匹配，必须人工核验")
			}
		}
		if b.ExecutionError || (b.ErrorMessage != "" && !legacyMessage) || (b.Outcome != "" && b.Outcome != "committed") {
			return nil, m.clean(fmt.Errorf("DBX 批次未成功: %s %s", b.ErrorMessage, b.Outcome))
		}
	}
	return r.Structured.Results, nil
}

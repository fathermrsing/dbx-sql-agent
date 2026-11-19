package main

import (
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"os"
	"time"
)

const pluginID = "io.github.fathermrsing.sql-agent"

func main() {
	e, err := newEngine(os.Getenv("DBX_PLUGIN_DATA_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h := sdk.HandlerFunc(func(_ sdk.RequestContext, method string, raw json.RawMessage, _ *sdk.Emitter) (any, *sdk.PluginError) {
		if method == "plugin/shutdown" {
			return map[string]bool{"success": true}, nil
		}
		var p struct {
			Target       Target `json:"target"`
			Token        string `json:"token"`
			Resource     string `json:"resource"`
			JobID        string `json:"jobId"`
			Change       Change `json:"change"`
			PlanID       string `json:"planId"`
			Confirmation string `json:"confirmation"`
			OperationID  string `json:"operationId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, sdk.NewError(-32602, "无效 JSON")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
		defer cancel()
		var value any
		var err error
		switch method {
		case "agent/token/status", "agent/token/save", "agent/token/delete":
			value, err = e.credential(method, p.Target.Endpoint, p.Token)
		case "agent/read":
			value, err = e.read(ctx, p.Target, p.Token, p.Resource, p.JobID)
		case "agent/plan":
			value, err = e.plan(ctx, p.Target, p.Token, p.Change)
		case "agent/start":
			value, err = e.start(p.PlanID, p.Confirmation, p.Token)
		case "agent/status":
			value, err = e.status(p.OperationID)
		case "agent/export":
			value, err = e.export(ctx, p.Target, p.Token, p.JobID)
		default:
			return nil, sdk.MethodNotFound(method)
		}
		if err != nil {
			return nil, sdk.NewError(-32000, err.Error())
		}
		return value, nil
	})
	if err = sdk.NewServer(sdk.Metadata{ID: pluginID, Version: "0.1.3", Capabilities: []string{"events"}}, h).Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

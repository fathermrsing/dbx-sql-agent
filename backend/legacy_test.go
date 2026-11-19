package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLegacyIdentityCompatibility(t *testing.T) {
	for _, kind := range []string{"original", "explicit-false", "modern", "write", "execution-error", "tool-error", "different-message", "rollback", "unknown", "future-outcome", "missing-rows", "empty-rows", "bad-row", "later-error"} {
		t.Run(kind, func(t *testing.T) {
			data, err := os.ReadFile("testdata/legacy-identity.json")
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err = json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			items := result["structuredContent"].(map[string]any)["results"].([]any)
			item := items[0].(map[string]any)
			switch kind {
			case "explicit-false":
				item["execution_error"] = false
			case "modern":
				item["execution_error"] = false
				delete(item, "error_message")
			case "execution-error":
				item["execution_error"] = true
			case "tool-error":
				result["isError"] = true
			case "different-message":
				item["error_message"] = "permission denied"
			case "rollback":
				item["transaction_outcome"] = "rolled_back"
			case "unknown":
				item["transaction_outcome"] = "unknown"
			case "future-outcome":
				item["transaction_outcome"] = "unexpected"
			case "missing-rows":
				delete(item, "rows")
			case "empty-rows":
				item["rows"] = []any{}
			case "bad-row":
				item["rows"] = []any{[]any{"SQL-LEGACY-TEST"}}
			case "later-error":
				result["structuredContent"].(map[string]any)["results"] = append(items, map[string]any{"execution_error": true, "error_message": "later failure"})
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID int64 `json:"id"`
				}
				json.NewDecoder(r.Body).Decode(&req)
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
			}))
			defer s.Close()
			m, err := newMCP(s.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}
			defer m.close()
			var rs []BatchResult
			if kind == "write" {
				rs, err = m.batch(context.Background(), "test", "msdb", identitySQL)
			} else {
				rs, err = m.readBatch(context.Background(), "test", "msdb", identitySQL)
			}
			success := kind == "original" || kind == "explicit-false" || kind == "modern"
			if success {
				if err != nil {
					t.Fatal(err)
				}
				records, e := rows(rs)
				if e != nil || len(records) != 1 || field(records[0], "server_name") != "SQL-LEGACY-TEST" {
					t.Fatal(records, e)
				}
			} else if err == nil {
				t.Fatal("ambiguous/error response must not pass")
			}
		})
	}
}

func TestSQL2008CompatibleErrors(t *testing.T) {
	sql, _, err := buildChange(Change{Action: "create", Name: "compatibility-test"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "THROW") || !strings.Contains(sql, "RAISERROR") || !strings.Contains(sql, "ERROR_MESSAGE()") || !strings.Contains(sql, "ROLLBACK") {
		t.Fatal(sql)
	}
	p := proc("sp_start_job", "@job_id=@job")
	if !strings.Contains(p, "IF @rc<>0 BEGIN RAISERROR") || !strings.Contains(p, "RETURN; END") {
		t.Fatal(p)
	}
	service, err := querySQL("service", "")
	if err != nil || !strings.Contains(service, "IF OBJECT_ID") || !strings.Contains(service, "ELSE SELECT") {
		t.Fatal(service, err)
	}
}

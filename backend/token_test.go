package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Tests never open or modify a real OS keychain.
type memoryVault struct {
	data           map[string]string
	reads, changes int
	fail           bool
}

func (v *memoryVault) supported() bool { return true }
func (v *memoryVault) has(a string) (bool, error) {
	if v.fail {
		return false, errors.New("vault denied")
	}
	_, ok := v.data[a]
	return ok, nil
}
func (v *memoryVault) get(a string) (string, error) {
	v.reads++
	if v.fail {
		return "", errors.New("vault denied")
	}
	return v.data[a], nil
}
func (v *memoryVault) set(a, s string) error {
	if v.fail {
		return errors.New("vault denied")
	}
	v.changes++
	v.data[a] = s
	return nil
}
func (v *memoryVault) delete(a string) error {
	if v.fail {
		return errors.New("vault denied")
	}
	v.changes++
	delete(v.data, a)
	return nil
}

func TestTokenStorageScopeAndPriority(t *testing.T) {
	t.Setenv("DBX_AGENT_MCP_TOKEN", "")
	e, err := newEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := &memoryVault{data: map[string]string{}}
	e.vault = v
	endpoint := "http://localhost:5225/mcp"
	status, err := e.credential("agent/token/save", endpoint, "saved-secret")
	if err != nil || !status.Saved {
		t.Fatal(status, err)
	}
	status, err = e.credential("agent/token/status", endpoint, "")
	data, _ := json.Marshal(status)
	if err != nil || !status.Saved || strings.Contains(string(data), "saved-secret") || v.reads != 0 {
		t.Fatal(status, err, v.reads)
	}
	token, err := e.resolveToken("http://127.0.0.1:5225/mcp", "")
	if err != nil || token != "saved-secret" {
		t.Fatal("normalized endpoint failed", err)
	}
	for _, other := range []string{"http://127.0.0.1:5226/mcp", "http://127.0.0.1:5225/other"} {
		if token, err = e.resolveToken(other, ""); err != nil || token != "" {
			t.Fatal("cross endpoint secret", err)
		}
	}
	t.Setenv("DBX_AGENT_MCP_TOKEN", "environment-secret")
	if token, err = e.resolveToken(endpoint, ""); err != nil || token != "environment-secret" {
		t.Fatal("environment priority", err)
	}
	if token, err = e.resolveToken(endpoint, "typed-secret"); err != nil || token != "typed-secret" {
		t.Fatal("typed priority", err)
	}
	t.Setenv("DBX_AGENT_MCP_TOKEN", "")
	if _, err = e.credential("agent/token/save", endpoint, "replacement-secret"); err != nil {
		t.Fatal(err)
	}
	if token, err = e.resolveToken(endpoint, ""); err != nil || token != "replacement-secret" {
		t.Fatal("replacement failed", err)
	}
	if _, err = e.credential("agent/token/delete", endpoint, ""); err != nil {
		t.Fatal(err)
	}
	if token, err = e.resolveToken(endpoint, ""); err != nil || token != "" {
		t.Fatal("deletion failed", err)
	}
}

func TestTokenValidationAndVaultFailure(t *testing.T) {
	t.Setenv("DBX_AGENT_MCP_TOKEN", "")
	e, err := newEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := &memoryVault{data: map[string]string{}}
	e.vault = v
	for _, token := range []string{"", "a\nb", "a\rb", "a\x00b", strings.Repeat("a", 8193)} {
		if _, err = e.credential("agent/token/save", "http://localhost:5225/mcp", token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	for _, endpoint := range []string{"http://example.com/mcp", "http://user:password@localhost/mcp", "file:///tmp/a"} {
		if _, err = e.credential("agent/token/save", endpoint, "secret"); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
	if v.changes != 0 {
		t.Fatal("invalid input reached vault")
	}
	v.fail = true
	if _, err = e.resolveToken("http://localhost:5225/mcp", ""); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err = e.credential("agent/token/save", "http://localhost:5225/mcp", "secret"); err == nil {
		t.Fatal("failure hidden")
	}
	token, err := e.resolveToken("http://localhost:5225/mcp", "manual")
	if err != nil || token != "manual" {
		t.Fatal("manual override must work despite denied keychain", err)
	}
}

func TestRememberedTokenReadAfterRestart(t *testing.T) {
	t.Setenv("DBX_AGENT_MCP_TOKEN", "")
	e, f, target := env(t)
	v := &memoryVault{data: map[string]string{}}
	e.vault = v
	if _, err := e.credential("agent/token/save", target.Endpoint, "secret"); err != nil {
		t.Fatal(err)
	}
	reopened, err := newEngine(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened.vault = v
	if _, err = reopened.read(context.Background(), target, "", "jobs", ""); err != nil {
		t.Fatal("restored credential failed", err)
	}
	if _, err = reopened.credential("agent/token/delete", target.Endpoint, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.read(context.Background(), target, "", "jobs", ""); err == nil {
		t.Fatal("deleted credential still usable")
	}
	if f.writes.Load() != 0 {
		t.Fatal("read-only restore issued writes")
	}
}

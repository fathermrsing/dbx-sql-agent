package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// Only this plugin's own per-endpoint MCP credentials are accessible.
type tokenVault interface {
	supported() bool
	has(account string) (bool, error)
	get(account string) (string, error)
	set(account, token string) error
	delete(account string) error
}
type tokenStatus struct {
	Supported bool `json:"supported"`
	Saved     bool `json:"saved"`
}

func tokenAccount(endpoint string) (string, error) {
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}
func (e *Engine) credential(method, endpoint, token string) (tokenStatus, error) {
	status := tokenStatus{Supported: e.vault.supported()}
	account, err := tokenAccount(endpoint)
	if err != nil {
		return status, err
	}
	if !status.Supported {
		if method == "agent/token/status" {
			return status, nil
		}
		return status, errors.New("此构建不支持系统凭据存储；请本次填写 Token")
	}
	switch method {
	case "agent/token/status":
		status.Saved, err = e.vault.has(account)
	case "agent/token/save":
		if token == "" || len(token) > 8192 || strings.ContainsAny(token, "\r\n\x00") {
			return status, errors.New("Token 为空、超长或包含无效字符")
		}
		err = e.vault.set(account, token)
		status.Saved = err == nil
	case "agent/token/delete":
		err = e.vault.delete(account)
	default:
		return status, errors.New("未知凭据操作")
	}
	return status, err
}
func (e *Engine) resolveToken(endpoint, token string) (string, error) {
	if token != "" {
		return token, nil
	}
	if token = os.Getenv("DBX_AGENT_MCP_TOKEN"); token != "" {
		return token, nil
	}
	account, err := tokenAccount(endpoint)
	if err != nil {
		return "", err
	}
	if !e.vault.supported() {
		return "", nil
	}
	return e.vault.get(account)
}

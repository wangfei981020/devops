package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// MCPClient 极简 MCP JSON-RPC over HTTP 客户端（连 CMDB MCP：tools/list + tools/call）。
type MCPClient struct {
	url    string
	token  string
	http   *http.Client
	nextID int64
}

func NewMCPClient(url, token string) *MCPClient {
	return &MCPClient{url: url, token: token, http: &http.Client{Timeout: 60 * time.Second}}
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (m *MCPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	body, _ := json.Marshal(rpcReq{JSONRPC: "2.0", ID: atomic.AddInt64(&m.nextID, 1), Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连 MCP 失败: %w", err)
	}
	defer resp.Body.Close()
	var out rpcResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析 MCP 响应: %w (HTTP %d)", err, resp.StatusCode)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("MCP 错误 %d: %s", out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

// MCPTool 是 tools/list 返回的一个工具。
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Initialize 走一遍 MCP 握手（有的实现需要，无状态实现也无害）。
func (m *MCPClient) Initialize(ctx context.Context) error {
	_, err := m.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "opsplatform-ai-copilot", "version": "0.1"},
	})
	return err
}

// ListTools 拉取全部工具定义。
func (m *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := m.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 tools/list: %w", err)
	}
	return out.Tools, nil
}

// CallTool 调一个工具，把 content 里的 text 拼成字符串返回。
func (m *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	} else {
		params["arguments"] = map[string]any{}
	}
	raw, err := m.call(ctx, "tools/call", params)
	if err != nil {
		return "", true, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw), false, nil // 非标准返回，原样回灌
	}
	buf := ""
	for _, c := range out.Content {
		buf += c.Text
	}
	if buf == "" {
		buf = string(raw)
	}
	return buf, out.IsError, nil
}

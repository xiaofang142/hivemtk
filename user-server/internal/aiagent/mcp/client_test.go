package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

// withTestTransport 换出站 Transport 为普通实现：httptest 替身绑 127.0.0.1，
// SSRF 守闸按设计拒绝环路地址，协议层用例需要穿透（守闸覆盖在 urlguard/service 层）。
func withTestTransport(t *testing.T) {
	t.Helper()
	orig := newClientTransport
	newClientTransport = func() http.RoundTripper { return &http.Transport{} }
	t.Cleanup(func() { newClientTransport = orig })
}

// fakeMCPServer 标准形状的 MCP JSON-RPC server（initialize/tools/list/tools/call）。
func fakeMCPServer(t *testing.T, failFirst int32) *httptest.Server {
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[
				{"name":"fs_read","description":"读文件","inputSchema":{"type":"object","properties":{"path":{"type":"string","description":"路径"}},"required":["path"]}}
			]}}`))
		case "tools/call":
			// 熔断测试：前 failFirst 次返回 500
			if atomic.AddInt32(&calls, 1) <= failFirst {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			out, _ := json.Marshal(CallToolResult{Content: []ContentBlock{{Type: "text", Text: "file content here"}}, IsError: false})
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":` + string(out) + `}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":9,"error":{"code":-32601,"message":"method not found"}}`))
		}
	}))
}

func TestDiscoverAndRegister(t *testing.T) {
	withTestTransport(t)
	srv := fakeMCPServer(t, 0)
	defer srv.Close()
	reg := tooluse.NewToolRegistry()
	n, err := DiscoverAndRegister(context.Background(), ClientConfig{Name: "fs", Endpoint: srv.URL}, reg)
	if err != nil || n != 1 {
		t.Fatalf("发现注册: n=%d err=%v", n, err)
	}
	tool, err := reg.Get("mcp_fs_fs_read")
	if err != nil {
		t.Fatalf("mcp_fs_fs_read 未注册: %v", err)
	}
	if !strings.Contains(tool.Description(), "[MCP:fs]") {
		t.Errorf("描述: %s", tool.Description())
	}
	// 参数映射
	params := tool.Parameters()
	if params.Type != "object" || len(params.Properties) != 1 {
		t.Errorf("参数 schema: %+v", params)
	}
	if len(params.Required) != 1 || params.Required[0] != "path" {
		t.Errorf("required: %v", params.Required)
	}
	// 执行转发
	res, err := tool.Execute(context.Background(), map[string]any{"path": "/tmp/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" || !strings.Contains(fmt.Sprint(res.Data), "file content") {
		t.Errorf("执行结果: %+v", res)
	}
}

func TestClientCircuitBreaker(t *testing.T) {
	withTestTransport(t)
	srv := fakeMCPServer(t, 100) // 永远 500
	defer srv.Close()
	c := NewClient(ClientConfig{Name: "bad", Endpoint: srv.URL, FailLimit: 2, Cooldown: 50 * time.Millisecond})
	ctx := context.Background()
	// 连续失败 2 次后熔断
	_, _, err1 := c.CallTool(ctx, "x", nil)
	_, _, err2 := c.CallTool(ctx, "x", nil)
	if err1 == nil || err2 == nil {
		t.Fatal("前两次应失败")
	}
	_, _, err3 := c.CallTool(ctx, "x", nil)
	if err3 == nil || !strings.Contains(err3.Error(), "熔断") {
		t.Errorf("第三次应命中熔断，实际: %v", err3)
	}
	// 冷却后恢复尝试（仍失败但错误不再是熔断）
	time.Sleep(60 * time.Millisecond)
	_, _, err4 := c.CallTool(ctx, "x", nil)
	if err4 != nil && strings.Contains(err4.Error(), "熔断中") {
		t.Error("冷却后不应仍在熔断")
	}
}

func TestClientProtocolErrorNotCircuit(t *testing.T) {
	withTestTransport(t)
	srv := fakeMCPServer(t, 0)
	defer srv.Close()
	c := NewClient(ClientConfig{Name: "ok", Endpoint: srv.URL, FailLimit: 1})
	// unknown method → JSON-RPC error（协议层业务错），不应累计熔断
	err := c.call(context.Background(), "bogus/method", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "-32601") {
		t.Fatalf("应报协议错: %v", err)
	}
	// 再来一次仍然发请求（未熔断）
	err2 := c.call(context.Background(), "bogus/method", nil, nil)
	if err2 != nil && strings.Contains(err2.Error(), "熔断") {
		t.Error("协议业务错不应触发熔断")
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

type mockTool struct {
	name      string
	desc      string
	params    tooluse.ToolParameters
	executeFn func(ctx context.Context, args map[string]any) (tooluse.ToolResult, error)
}

func (m *mockTool) Name() string                       { return m.name }
func (m *mockTool) Category() tooluse.ToolCategory     { return tooluse.CategoryBusiness }
func (m *mockTool) Description() string                { return m.desc }
func (m *mockTool) Parameters() tooluse.ToolParameters { return m.params }
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, args)
	}
	return tooluse.ToolResult{Success: true, Data: map[string]any{"echo": args}}, nil
}

func send(t *testing.T, s *Server, method string, params any) JSONRPCResponse {
	t.Helper()
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"1"`),
		Method:  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		req.Params = raw
	}
	raw, _ := json.Marshal(req)
	resp, err := s.HandleRequest(context.Background(), raw)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	var out JSONRPCResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("unmarshal response: %v (raw=%s)", err, string(resp))
	}
	return out
}

func initializeReq(t *testing.T, s *Server) {
	t.Helper()
	send(t, s, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test", Version: "1.0"},
	})
}

// TestServer_Initialize 验证 initialize 流程
func TestServer_Initialize(t *testing.T) {
	s := NewServer(nil)
	resp := send(t, s, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test-client", Version: "0.1"},
	})
	if resp.Error != nil {
		t.Fatalf("expected success, got error: %v", resp.Error)
	}
	var result InitializeResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol %s, got %s", ProtocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != ServerInfo {
		t.Errorf("expected server name %s, got %s", ServerInfo, result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("server should advertise tools capability")
	}
	if !s.IsInitialized() {
		t.Error("server should be initialized")
	}
}

// TestServer_Initialize_Duplicate 验证重复 initialize 拒绝
func TestServer_Initialize_Duplicate(t *testing.T) {
	s := NewServer(nil)
	initializeReq(t, s)
	resp := send(t, s, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "test", Version: "1.0"},
	})
	if resp.Error == nil {
		t.Fatal("duplicate initialize should fail")
	}
	if resp.Error.Code != ErrCodeAlreadyInit {
		t.Errorf("expected error code %d, got %d", ErrCodeAlreadyInit, resp.Error.Code)
	}
}

// TestServer_ParseError 验证非法 JSON 返回 parse error
func TestServer_ParseError(t *testing.T) {
	s := NewServer(nil)
	resp, err := s.HandleRequest(context.Background(), []byte("not json"))
	if err != nil {
		t.Fatal(err)
	}
	var r JSONRPCResponse
	json.Unmarshal(resp, &r)
	if r.Error == nil || r.Error.Code != ErrCodeParse {
		t.Errorf("expected parse error, got %+v", r.Error)
	}
}

// TestServer_InvalidJSONRPCVersion 验证非 2.0 协议拒绝
func TestServer_InvalidJSONRPCVersion(t *testing.T) {
	s := NewServer(nil)
	raw := []byte(`{"jsonrpc":"1.0","id":"1","method":"ping"}`)
	resp, _ := s.HandleRequest(context.Background(), raw)
	var r JSONRPCResponse
	json.Unmarshal(resp, &r)
	if r.Error == nil || r.Error.Code != ErrCodeInvalidRequest {
		t.Errorf("expected invalid request, got %+v", r.Error)
	}
}

// TestServer_MethodNotFound 验证未知方法
func TestServer_MethodNotFound(t *testing.T) {
	s := NewServer(nil)
	initializeReq(t, s)
	resp := send(t, s, "unknown/method", nil)
	if resp.Error == nil {
		t.Fatal("unknown method should fail")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
	}
}

// TestServer_Ping 验证 ping 方法
func TestServer_Ping(t *testing.T) {
	s := NewServer(nil)
	initializeReq(t, s)
	resp := send(t, s, "ping", nil)
	if resp.Error != nil {
		t.Errorf("ping should succeed, got %v", resp.Error)
	}
}

// TestServer_NotificationsInitialized 验证 initialized 通知无 response
func TestServer_NotificationsInitialized(t *testing.T) {
	s := NewServer(nil)
	initializeReq(t, s)

	resp, err := s.HandleRequest(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp) != 0 {
		t.Errorf("notification should return empty body, got %s", string(resp))
	}
}

// TestServer_ToolsList 验证 tools/list
func TestServer_ToolsList(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{
		name: "echo",
		desc: "echo back the args",
		params: tooluse.ToolParameters{
			Type: "object",
			Properties: map[string]tooluse.ToolParam{
				"msg": {Type: "string", Description: "message"},
			},
			Required: []string{"msg"},
		},
	})
	r.Register(&mockTool{
		name: "add",
		desc: "add two numbers",
		params: tooluse.ToolParameters{
			Type: "object",
			Properties: map[string]tooluse.ToolParam{
				"a": {Type: "number"},
				"b": {Type: "number"},
			},
		},
	})

	s := NewServer(r)
	initializeReq(t, s)

	resp := send(t, s, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %v", resp.Error)
	}
	var result ListToolsResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}

	var found bool
	for _, t2 := range result.Tools {
		if t2.Name == "echo" {
			found = true
			if t2.InputSchema["type"] != "object" {
				t.Error("inputSchema.type should be object")
			}
			props, _ := t2.InputSchema["properties"].(map[string]any)
			if _, ok := props["msg"]; !ok {
				t.Error("echo should have msg property")
			}
		}
	}
	if !found {
		t.Error("echo tool not found")
	}
}

// TestServer_ToolsCall_Success 验证成功调用
func TestServer_ToolsCall_Success(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{
		name: "echo",
		params: tooluse.ToolParameters{
			Type: "object",
			Properties: map[string]tooluse.ToolParam{
				"msg": {Type: "string"},
			},
		},
		executeFn: func(_ context.Context, args map[string]any) (tooluse.ToolResult, error) {
			return tooluse.ToolResult{Success: true, Data: map[string]any{"echoed": args["msg"]}}, nil
		},
	})
	s := NewServer(r)
	initializeReq(t, s)

	resp := send(t, s, "tools/call", CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "hello"},
	})
	if resp.Error != nil {
		t.Fatalf("tools/call failed: %v", resp.Error)
	}
	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if result.IsError {
		t.Error("call should succeed")
	}
	if len(result.Content) != 1 {
		t.Errorf("expected 1 content block, got %d", len(result.Content))
	}
	if !strings.Contains(result.Content[0].Text, "hello") {
		t.Errorf("content should contain 'hello', got %q", result.Content[0].Text)
	}
}

// TestServer_ToolsCall_NotFound 验证调用不存在的工具
func TestServer_ToolsCall_NotFound(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	initializeReq(t, s)
	resp := send(t, s, "tools/call", CallToolParams{Name: "nonexistent"})
	if resp.Error != nil {
		t.Fatalf("expected isError=true response, got error: %v", resp.Error)
	}
	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if !result.IsError {
		t.Error("call to nonexistent tool should have isError=true")
	}
}

// TestServer_ToolsCall_ExecutionFails 验证工具执行失败
func TestServer_ToolsCall_ExecutionFails(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{
		name: "failing",
		executeFn: func(_ context.Context, _ map[string]any) (tooluse.ToolResult, error) {
			return tooluse.ToolResult{Success: false, Error: "intentional failure"}, nil
		},
	})
	s := NewServer(r)
	initializeReq(t, s)
	resp := send(t, s, "tools/call", CallToolParams{Name: "failing"})
	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if !result.IsError {
		t.Error("failed call should have isError=true")
	}
}

// TestServer_ToolsCall_EmptyName 验证空工具名
func TestServer_ToolsCall_EmptyName(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	initializeReq(t, s)
	resp := send(t, s, "tools/call", CallToolParams{Name: ""})
	if resp.Error == nil {
		t.Fatal("empty name should return error")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Errorf("expected code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// TestServer_ToolsCall_NotInitialized 验证未初始化时拒绝
func TestServer_ToolsCall_NotInitialized(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())

	resp := send(t, s, "tools/call", CallToolParams{Name: "x"})
	if resp.Error == nil {
		t.Fatal("not initialized should fail")
	}
	if resp.Error.Code != ErrCodeNotInitialized {
		t.Errorf("expected code %d, got %d", ErrCodeNotInitialized, resp.Error.Code)
	}
}

// TestServer_ToolsList_NotInitialized 验证 tools/list 未初始化
func TestServer_ToolsList_NotInitialized(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	resp := send(t, s, "tools/list", nil)
	if resp.Error == nil {
		t.Fatal("not initialized should fail")
	}
	if resp.Error.Code != ErrCodeNotInitialized {
		t.Errorf("expected code %d, got %d", ErrCodeNotInitialized, resp.Error.Code)
	}
}

// TestServer_ResourcesList_Empty 验证空资源列表
func TestServer_ResourcesList_Empty(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	initializeReq(t, s)
	resp := send(t, s, "resources/list", nil)
	if resp.Error != nil {
		t.Errorf("resources/list should succeed (empty), got %v", resp.Error)
	}
}

// TestServer_PromptsList_Empty 验证空 prompts 列表
func TestServer_PromptsList_Empty(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	initializeReq(t, s)
	resp := send(t, s, "prompts/list", nil)
	if resp.Error != nil {
		t.Errorf("prompts/list should succeed (empty), got %v", resp.Error)
	}
}

// TestServer_ConcurrentRequests 验证并发请求安全
func TestServer_ConcurrentRequests(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{name: "ping"})
	s := NewServer(r)
	initializeReq(t, s)

	const N = 50
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			resp := send(t, s, "ping", nil)
			if resp.Error != nil {
				errs <- errors.New("ping failed")
			} else {
				errs <- nil
			}
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ping: %v", err)
		}
	}
}

// TestServer_NilRegistry 验证 nil registry 不 panic
func TestServer_NilRegistry(t *testing.T) {
	s := NewServer(nil)
	initializeReq(t, s)
	resp := send(t, s, "tools/list", nil)
	if resp.Error != nil {
		t.Errorf("nil registry tools/list should succeed (empty), got %v", resp.Error)
	}
	resp = send(t, s, "tools/call", CallToolParams{Name: "x"})
	if resp.Error != nil {
		t.Fatalf("nil registry tools/call should return isError, got protocol error: %v", resp.Error)
	}
	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if !result.IsError {
		t.Error("nil registry should report tool as not found")
	}
}

// TestServer_ToolCallWithComplexArgs 验证复杂参数
func TestServer_ToolCallWithComplexArgs(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{
		name: "complex",
		executeFn: func(_ context.Context, args map[string]any) (tooluse.ToolResult, error) {
			obj, _ := args["obj"].(map[string]any)
			arr, _ := args["arr"].([]any)
			return tooluse.ToolResult{
				Success: true,
				Data: map[string]any{
					"obj_keys": len(obj),
					"arr_len":  len(arr),
				},
			}, nil
		},
	})
	s := NewServer(r)
	initializeReq(t, s)

	args := map[string]any{
		"obj": map[string]any{"a": 1, "b": 2},
		"arr": []any{1, 2, 3},
	}
	resp := send(t, s, "tools/call", CallToolParams{Name: "complex", Arguments: args})
	if resp.Error != nil {
		t.Fatalf("complex call failed: %v", resp.Error)
	}
	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if result.IsError {
		t.Error("complex call should succeed")
	}
	if !strings.Contains(result.Content[0].Text, `"obj_keys":2`) {
		t.Errorf("content should have obj_keys:2, got %s", result.Content[0].Text)
	}
}

// TestConvertParameters 验证 schema 转换
func TestConvertParameters(t *testing.T) {
	params := tooluse.ToolParameters{
		Type: "object",
		Properties: map[string]tooluse.ToolParam{
			"name": {Type: "string", Description: "user name", Default: "anonymous"},
			"role": {Type: "string", Enum: []string{"admin", "user"}},
		},
		Required: []string{"name"},
	}
	schema := convertParameters(params)
	if schema["type"] != "object" {
		t.Error("type should be object")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be map[string]any")
	}
	if _, ok := props["name"]; !ok {
		t.Error("name property missing")
	}
	role, ok := props["role"].(map[string]any)
	if !ok {
		t.Fatal("role should be map[string]any")
	}

	switch e := role["enum"].(type) {
	case []string:
		if len(e) != 2 {
			t.Errorf("expected 2 enum values, got %d", len(e))
		}
	case []any:
		if len(e) != 2 {
			t.Errorf("expected 2 enum values, got %d", len(e))
		}
	default:
		t.Errorf("enum type unexpected: %T (val=%v)", role["enum"], role["enum"])
	}
	req, _ := schema["required"].([]string)
	if len(req) != 1 || req[0] != "name" {
		t.Errorf("expected required=[name], got %v", req)
	}
}

func TestServer_ProtocolVersionCompliance(t *testing.T) {
	if ProtocolVersionLatest != "2025-11-25" {
		t.Errorf("latest protocol version should be 2025-11-25 per spec, got %s", ProtocolVersionLatest)
	}
	if len(SupportedProtocolVersions) != 2 {
		t.Fatalf("expected 2 supported versions, got %v", SupportedProtocolVersions)
	}
	if SupportedProtocolVersions[0] != "2025-06-18" || SupportedProtocolVersions[1] != "2025-11-25" {
		t.Errorf("unexpected supported versions: %v", SupportedProtocolVersions)
	}
}

// TestServer_ContentBlockType 验证 content block type
func TestServer_ContentBlockType(t *testing.T) {
	cb := ContentBlock{Type: "text", Text: "hello"}
	if cb.Type != "text" {
		t.Error("content type should be text")
	}
}

var _ = fmt.Sprintf

// =============================================================================
// 凭证 scope 过滤 + 审计钩子（2026-10-10 新增）
// =============================================================================

func TestServer_SetAllowedToolsFiltersListAndCall(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{name: "allowed", executeFn: func(_ context.Context, _ map[string]any) (tooluse.ToolResult, error) {
		return tooluse.ToolResult{Success: true, Data: map[string]any{"ok": 1}}, nil
	}})
	r.Register(&mockTool{name: "forbidden", executeFn: func(_ context.Context, _ map[string]any) (tooluse.ToolResult, error) {
		return tooluse.ToolResult{Success: true, Data: map[string]any{"ok": 1}}, nil
	}})
	s := NewServer(r)
	s.SetAllowedTools("allowed")
	initializeReq(t, s)

	// tools/list 只暴露白名单内
	resp := send(t, s, "tools/list", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %v", resp.Error)
	}
	var list ListToolsResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &list)
	if len(list.Tools) != 1 || list.Tools[0].Name != "allowed" {
		t.Errorf("scope 过滤后应只剩 allowed，got %+v", list.Tools)
	}

	// 白名单外 tools/call 拒绝执行
	resp = send(t, s, "tools/call", CallToolParams{Name: "forbidden", Arguments: map[string]any{}})
	if resp.Error != nil {
		t.Fatalf("expected isError response for forbidden tool, got error: %v", resp.Error)
	}
	var result CallToolResult
	data, _ = json.Marshal(resp.Result)
	json.Unmarshal(data, &result)
	if !result.IsError {
		t.Error("白名单外工具应 isError=true 且不执行")
	}
	if !strings.Contains(result.Content[0].Text, "授权范围") {
		t.Errorf("越权文案应点名'授权范围'，got %q", result.Content[0].Text)
	}
}

func TestServer_AuditSinkRecordsCallAndReject(t *testing.T) {
	r := tooluse.NewToolRegistry()
	r.Register(&mockTool{name: "echo", executeFn: func(_ context.Context, args map[string]any) (tooluse.ToolResult, error) {
		return tooluse.ToolResult{Success: true, Data: map[string]any{"echoed": args["msg"]}}, nil
	}})
	s := NewServer(r)
	s.SetAllowedTools("echo")
	initializeReq(t, s)

	var audits []AuditRecord
	s.SetAuditSink(func(rec AuditRecord) { audits = append(audits, rec) })

	send(t, s, "tools/call", CallToolParams{Name: "echo", Arguments: map[string]any{"msg": "hi"}})
	if len(audits) != 1 {
		t.Fatalf("应有 1 条审计，got %d", len(audits))
	}
	a := audits[0]
	if a.Method != "tools/call" || a.Tool != "echo" || !a.Allowed || !a.OK {
		t.Errorf("成功调用审计字段不对：%+v", a)
	}
	if a.ArgsHash == "" {
		t.Error("参数摘要不应为空")
	}

	// 白名单外调用：allowed=false 且未执行
	send(t, s, "tools/call", CallToolParams{Name: "other", Arguments: map[string]any{"x": 1}})
	if len(audits) != 2 {
		t.Fatalf("越权调用也应留痕，got %d", len(audits))
	}
	b := audits[1]
	if b.Tool != "other" || b.Allowed || b.OK {
		t.Errorf("越权调用审计应为 allowed=false OK=false，got %+v", b)
	}
}

func TestServer_AuditSinkNotSetIsNoop(t *testing.T) {
	s := NewServer(tooluse.NewToolRegistry())
	initializeReq(t, s)
	send(t, s, "tools/call", CallToolParams{Name: "nope", Arguments: map[string]any{}})
	// 无 sink 不应 panic（回归：审计是旁路职责）
}

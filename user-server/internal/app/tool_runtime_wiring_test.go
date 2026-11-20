package app

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/aiagent/mcp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// 管理服务提交成功后，生产装配的两个执行器必须立即拒绝暖缓存调用。
func TestToolRuntimeWiring_ManagementRevokesBothExecutors(t *testing.T) {
	restoreApprovalState(t)
	t.Setenv(ApprovalGateFlagEnv, "off")
	database := testutil.NewTestDB(t, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	probe := newApprovalProbeTool("runtime-test.managed", tooluse.CategoryBusiness)
	registry := tooluse.GetGlobalRegistry()
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.Unregister(probe.Name()) })
	previous := tooluse.GetGlobalExecutor()
	t.Cleanup(func() { tooluse.SetGlobalExecutor(previous) })
	InitGlobalToolExecutor()
	first := tooluse.GetGlobalExecutor()
	InitGlobalToolExecutor()
	second := tooluse.GetGlobalExecutor()
	config := model.AIToolConfig{TenantID: 1, ToolName: probe.Name(), IsEnabled: true}
	if err := database.WithContext(ctx).Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	management := service.NewAIToolConfigService(repository.NewAIToolConfigRepository(database), nil)
	check := func(allow bool) {
		t.Helper()
		for _, executor := range []*tooluse.ToolExecutor{first, second} {
			before := probe.count()
			result := executor.Execute(ctx, tooluse.ExecuteRequest{ToolName: probe.Name(), Args: map[string]any{}})
			if allow && (result.Err != nil || !result.Success || probe.count() != before+1) {
				t.Fatalf("启用状态应执行一次: %+v", result)
			}
			if !allow && (result.Err == nil || result.Success || probe.count() != before) {
				t.Errorf("管理停用后必须零副作用: %+v", result)
			}
		}
	}
	check(true)
	if err := management.UpdateStatus(ctx, probe.Name(), false); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := management.BatchUpdateStatus(ctx, []string{probe.Name()}, true); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := management.BatchUpdateStatus(ctx, []string{probe.Name()}, false); err != nil {
		t.Fatal(err)
	}
	check(false)
}

// MCP 与注册表直调不经过 Executor，也必须使用生产包装的同一权威准入。
func TestToolRuntimeWiring_MCPAndDirectRevocation(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	probe := newApprovalProbeTool("runtime-test.direct", tooluse.CategoryBusiness)
	registry := tooluse.NewToolRegistry()
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	rewirePermissionDecorators(registry)
	server := mcp.NewServer(registry)
	request := func(method string, params any) json.RawMessage {
		t.Helper()
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		body, err = server.HandleRequest(ctx, body)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Result json.RawMessage   `json:"result"`
			Error  *mcp.JSONRPCError `json:"error"`
		}
		if err := json.Unmarshal(body, &response); err != nil || response.Error != nil {
			t.Fatalf("JSON-RPC 响应异常: %s / %v", body, err)
		}
		return response.Result
	}
	request("initialize", map[string]any{"protocolVersion": mcp.ProtocolVersionLatest, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "local-revocation-test", "version": "1"}})
	call := func(allow bool) {
		t.Helper()
		before := probe.count()
		var result mcp.CallToolResult
		if err := json.Unmarshal(request("tools/call", mcp.CallToolParams{Name: probe.Name(), Arguments: map[string]any{}}), &result); err != nil {
			t.Fatal(err)
		}
		if allow && (result.IsError || probe.count() != before+1) {
			t.Fatalf("启用时 MCP 应真实调用一次: %+v", result)
		}
		if !allow && (!result.IsError || probe.count() != before) {
			t.Errorf("管理停用后 MCP 必须零副作用: count=%d -> %d / %+v", before, probe.count(), result)
		}
	}
	if err := database.WithContext(ctx).Create(&model.AIToolConfig{TenantID: 1, ToolName: probe.Name(), IsEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	call(true)
	management := service.NewAIToolConfigService(repository.NewAIToolConfigRepository(database), nil)
	if err := management.UpdateStatus(ctx, probe.Name(), false); err != nil {
		t.Fatal(err)
	}
	call(false)
	wrapped, err := registry.Get(probe.Name())
	if err != nil {
		t.Fatal(err)
	}
	before := probe.count()
	result, err := wrapped.Execute(ctx, map[string]any{})
	if err == nil || result.Success || probe.count() != before {
		t.Errorf("管理停用后注册表直调必须零副作用: %+v / %v", result, err)
	}
	if err := management.UpdateStatus(ctx, probe.Name(), true); err != nil {
		t.Fatal(err)
	}
	call(true)
}

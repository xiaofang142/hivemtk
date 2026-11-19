package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestManagedMCP_RevocationAcrossReplicas(t *testing.T) {
	withTestTransport(t)
	var calls atomic.Int64
	var requests atomic.Int64
	var expectedAuth atomic.Value
	expectedAuth.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}}`))
		case "tools/call":
			if r.Header.Get("Authorization") != expectedAuth.Load().(string) {
				t.Error("工具调用使用了旧的模拟凭据")
			}
			calls.Add(1)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"simulated"}],"isError":false}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	database := testutil.NewTestDB(t, &model.MCPServer{}, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	row := model.MCPServer{TenantID: 1, Name: "revocation", Endpoint: server.URL, Enabled: true}
	if err := database.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverAndRegisterRow(ctx, database, &row, tooluse.NewToolRegistry(), nil); err != repository.ErrToolRuntimeUnavailable || requests.Load() != 0 {
		t.Fatalf("未装配权威端口应拒绝且不得发现请求，错误 %v，请求 %d", err, requests.Load())
	}
	registries := []*tooluse.ToolRegistry{tooluse.NewToolRegistry(), tooluse.NewToolRegistry()}
	executors := make([]*tooluse.ToolExecutor, 0, 2)
	for _, registry := range registries {
		if _, err := DiscoverAndRegisterRow(ctx, database, &row, registry, repository.NewToolRuntimeRepositoryWithDB(database)); err != nil {
			t.Fatal(err)
		}
		executors = append(executors, tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{}))
	}
	old, err := registries[0].Get("mcp_revocation_echo")
	if err != nil {
		t.Fatal(err)
	}
	check := func(label string, allow bool) {
		t.Helper()
		for _, executor := range executors {
			before := calls.Load()
			result := executor.Execute(ctx, tooluse.ExecuteRequest{ToolName: "mcp_revocation_echo"})
			if allow && (result.Err != nil || !result.Success || calls.Load() != before+1) {
				t.Errorf("%s 应调用一次: %+v", label, result)
			}
			if !allow && (result.Err == nil || result.Success || calls.Load() != before) {
				t.Errorf("%s 应拒绝且零新 tools/call，增量 %d: %+v", label, calls.Load()-before, result)
			}
		}
	}
	check("初始暖缓存", true)
	repo := repository.NewMCPServerRepository()
	if err := repo.SetEnabled(ctx, row.ID, false); err != nil {
		t.Fatal(err)
	}
	check("停用后两个副本", false)
	if err := repo.SetEnabled(ctx, row.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&row).Update("timeout_seconds", 9).Error; err != nil {
		t.Fatal(err)
	}
	check("来源执行配置变化", false)
	if err := database.WithContext(ctx).First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, registry := range registries {
		if _, err := DiscoverAndRegisterRow(ctx, database, &row, registry, repository.NewToolRuntimeRepositoryWithDB(database)); err != nil {
			t.Fatal(err)
		}
	}
	check("重新发现替换完整旧处理器", true)
	if err := database.WithContext(ctx).Model(&row).Update("api_key_enc", dbencrypt.Encrypt("simulated-v2")).Error; err != nil {
		t.Fatal(err)
	}
	check("仅凭据变化也必须拒绝旧实例", false)
	expectedAuth.Store("Bearer simulated-v2")
	if err := database.WithContext(ctx).First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, registry := range registries {
		if _, err := DiscoverAndRegisterRow(ctx, database, &row, registry, repository.NewToolRuntimeRepositoryWithDB(database)); err != nil {
			t.Fatal(err)
		}
	}
	check("重发现后使用当前模拟凭据", true)
	if err := repo.Delete(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
	check("删除后两个副本", false)
	row.ID = 0
	if err := database.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if _, err := old.Execute(ctx, nil); err == nil || calls.Load() != before {
		t.Error("同名新 ID 不能复活旧 MCP 实例")
	}
	for _, registry := range registries {
		if _, err := DiscoverAndRegisterRow(ctx, database, &row, registry, repository.NewToolRuntimeRepositoryWithDB(database)); err != nil {
			t.Fatal(err)
		}
	}
	check("同名重建后发现", true)
}

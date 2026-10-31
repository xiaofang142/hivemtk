package tooluse

// tenant_registry_test.go 验收：租户自带工具（custom_tools / mcp_servers）的隔离边界。
//
// 缺陷形态（本文件存在的理由）：ToolRegistry 是进程级单命名空间，启动时若用
// 系统作用域把各租户的工具都塞进全局表，A 租户的 Agent 就能调 B 租户的工具
// ——而工具背后挂的是 B 的凭据与基线 URL，等于跨租户越权。
// 这里钉住三条：
//   1) 租户工具只对该租户可见（另一租户看不到、也拿不到）；
//   2) 内置工具对所有租户都可见（叠加层不遮蔽内置）；
//   3) 多租户模式下没有租户上下文时 fail-closed（返回 0 作用域，不回落默认租户）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/tenant"
)

type fakeTool struct {
	name string
}

func (f *fakeTool) Name() string               { return f.name }
func (f *fakeTool) Category() ToolCategory     { return CategoryBusiness }
func (f *fakeTool) Description() string        { return "fake " + f.name }
func (f *fakeTool) Parameters() ToolParameters { return ToolParameters{Type: "object"} }
func (f *fakeTool) Execute(context.Context, map[string]any) (ToolResult, error) {
	return ToolResult{Success: true}, nil
}

func TestTenantRegistryIsolatesToolsPerTenant(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 清零，避免与其它用例共享全局状态
	tenantRegsMu.Lock()
	tenantRegs = map[uint]*ToolRegistry{}
	tenantRegsMu.Unlock()

	if err := RegisterTenantTool(1, &fakeTool{name: "audit_t1_tool"}); err != nil {
		t.Fatalf("注册租户 1 工具失败: %v", err)
	}
	if err := RegisterTenantTool(2, &fakeTool{name: "audit_t2_tool"}); err != nil {
		t.Fatalf("注册租户 2 工具失败: %v", err)
	}

	ctx1 := tenant.WithTenantID(context.Background(), 1)
	ctx2 := tenant.WithTenantID(context.Background(), 2)

	names1 := toolNames(ListToolsForTenant(ctx1))
	if !hasToolName(names1, "audit_t1_tool") {
		t.Fatalf("租户 1 看不到自己的工具: %v", names1)
	}
	if hasToolName(names1, "audit_t2_tool") {
		t.Fatalf("租户 1 看到了租户 2 的工具（跨租户越权）: %v", names1)
	}

	names2 := toolNames(ListToolsForTenant(ctx2))
	if !hasToolName(names2, "audit_t2_tool") || hasToolName(names2, "audit_t1_tool") {
		t.Fatalf("租户 2 的工具可见性错误: %v", names2)
	}

	if _, err := GetToolForTenant(ctx1, "audit_t2_tool"); err == nil {
		t.Fatal("租户 1 通过 GetToolForTenant 拿到了租户 2 的工具")
	}
	if _, err := GetToolForTenant(ctx2, "audit_t1_tool"); err == nil {
		t.Fatal("租户 2 通过 GetToolForTenant 拿到了租户 1 的工具")
	}
	if _, err := GetToolForTenant(ctx1, "audit_t1_tool"); err != nil {
		t.Fatal("租户 1 拿不到自己的工具")
	}
}

func TestTenantRegistryKeepsBuiltinsVisible(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	builtin := &fakeTool{name: "audit_builtin_tool"}
	if err := GetGlobalRegistry().Register(builtin); err != nil && err != ErrToolAlreadyExists {
		t.Fatalf("注册内置工具失败: %v", err)
	}
	t.Cleanup(func() { GetGlobalRegistry().Unregister("audit_builtin_tool") })

	ctx := tenant.WithTenantID(context.Background(), 7)
	names := toolNames(ListToolsForTenant(ctx))
	if !hasToolName(names, "audit_builtin_tool") {
		t.Fatalf("内置工具在租户叠加层里丢失: %v", names)
	}
	if _, err := GetToolForTenant(ctx, "audit_builtin_tool"); err != nil {
		t.Fatal("GetToolForTenant 取不到内置工具")
	}
}

func TestScopeForFailsClosedWithoutTenant(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	if got := ScopeFor(context.Background()); got != 0 {
		t.Fatalf("多租户下无租户上下文应 fail-closed 返回 0，实际 %d", got)
	}

	tenant.SetMode(tenant.ModeOff)
	if got := ScopeFor(context.Background()); got != tenant.DefaultTenantID {
		t.Fatalf("单租户模式应返回默认租户 %d，实际 %d", tenant.DefaultTenantID, got)
	}
}

func toolNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name())
	}
	return out
}

func hasToolName(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

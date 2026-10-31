package tooluse

// tenant_registry.go 租户级工具叠加层（SaaS 多租户隔离）。
//
// 背景：`custom_tools`（OpenAPI 导入）和 `mcp_servers`（外部 MCP）都是**租户表**，
// 而 ToolRegistry 是进程级共享的单一命名空间。多租户模式下：
//   - 启动期用根 ctx 查这两张表会被 tenant 插件拒绝（需要租户上下文），
//     于是所有租户的自定义工具/MCP 工具在 SaaS 上全部不可用；
//   - 反过来，如果直接用 SystemScope 把它们塞进全局注册表，A 租户的 Agent 就能
//     调用 B 租户的工具——工具背后带的是 B 的凭据与数据，这是跨租户越权。
//
// 因此把「租户自带资源」放进按租户隔离的叠加层（overlay）：
//   - 内置工具仍在全局注册表（对所有租户可见）；
//   - 租户工具只登记到该租户自己的 registry，派发时按请求 ctx 的租户 id 合并；
//   - 同名冲突只发生在同一租户内部（重复导入 → 覆盖），不会跨租户互相顶掉。
//
// 单租户（off）模式下所有行都属于默认租户，行为与改造前一致。

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"hivemtk-user/internal/pkg/tenant"
)

var (
	tenantRegsMu sync.RWMutex
	tenantRegs   = map[uint]*ToolRegistry{}
)

// EnsureTenantRegistry 返回（必要时创建）租户级注册表。
func EnsureTenantRegistry(tenantID uint) *ToolRegistry {
	tenantRegsMu.Lock()
	defer tenantRegsMu.Unlock()
	if r, ok := tenantRegs[tenantID]; ok {
		return r
	}
	r := NewToolRegistry()
	tenantRegs[tenantID] = r
	return r
}

// TenantRegistry 返回已存在的租户级注册表；没有则返回 nil。
func TenantRegistry(tenantID uint) *ToolRegistry {
	tenantRegsMu.RLock()
	defer tenantRegsMu.RUnlock()
	return tenantRegs[tenantID]
}

// RegisterTenantTool 登记租户工具（同名覆盖：重复导入/重启重建都幂等）。
func RegisterTenantTool(tenantID uint, t Tool) error {
	if t == nil {
		return fmt.Errorf("cannot register nil tool")
	}
	reg := EnsureTenantRegistry(tenantID)
	_ = reg.Unregister(t.Name()) // 不存在时返回错误，忽略即可
	return reg.Register(t)
}

// ListTenantTools 列出某租户登记的工具。
func ListTenantTools(tenantID uint) []Tool {
	if reg := TenantRegistry(tenantID); reg != nil {
		return reg.List()
	}
	return nil
}

// GetTenantTool 按名字取某租户的工具。
func GetTenantTool(tenantID uint, name string) (Tool, error) {
	if reg := TenantRegistry(tenantID); reg != nil {
		return reg.Get(name)
	}
	return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
}

// ScopeFor 解析请求 ctx 对应的租户 id。
//
// 返回值 0 表示「多租户模式下拿不到租户」——此时**不**回落到默认租户，
// 否则会把默认租户的工具暴露给一个身份不明的请求（fail-closed）。
func ScopeFor(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	if tenant.IsMultiTenant() {
		return 0
	}
	return tenant.DefaultTenantID
}

// ListToolsForTenant 合并「内置工具 + 当前租户的工具」。
// 内置工具优先（同名时保留内置，避免租户工具遮蔽平台能力）。
func ListToolsForTenant(ctx context.Context) []Tool {
	base := GetGlobalRegistry().List()
	tid := ScopeFor(ctx)
	if tid == 0 {
		return base
	}
	extra := ListTenantTools(tid)
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]Tool, 0, len(base)+len(extra))
	for _, t := range base {
		seen[t.Name()] = struct{}{}
		out = append(out, t)
	}
	for _, t := range extra {
		if _, dup := seen[t.Name()]; dup {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// GetToolForTenant 先查内置，再查当前租户的叠加层。
func GetToolForTenant(ctx context.Context, name string) (Tool, error) {
	if t, err := GetGlobalRegistry().Get(name); err == nil {
		return t, nil
	}
	if tid := ScopeFor(ctx); tid > 0 {
		return GetTenantTool(tid, name)
	}
	return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
}

// TenantToolStats 观测用：已装载的租户数与其工具总数。
func TenantToolStats() (tenants int, tools int) {
	tenantRegsMu.RLock()
	defer tenantRegsMu.RUnlock()
	for _, r := range tenantRegs {
		tenants++
		tools += r.Count()
	}
	return tenants, tools
}

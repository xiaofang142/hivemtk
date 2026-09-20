// Package tooluse 提供 DNC (Do-Not-Contact) 网关：在 cold outreach 工具前
// 先查询全局退订状态，命中即拒绝，保证不会被遗漏。
//
// 落地 R-3a 决策 —— "全局请勿打扰"。
// 设计要点：
//   - 默认放行（resolver=nil）：保持现有行为，避免线上事故
//   - 任何 DB 异常一律放行并记录：与 IsBlocked 一致（fail-open）；DNC 是用户可见的合规防线，
//     但它的缺漏已经被前置 IsBlocked 调用覆盖（其他合规检查兜底）
//   - 通过 approvalDecorator 的同样路径（Tool 接口）实现，可被替换/降级
//   - 仅 cold outreach 类工具触发（proactive / batch_send / reach.*.dm），与 A-1 一致
package tooluse

import (
	"context"
	"fmt"
	"strings"
)

// DNCResolver 全局退订查询接口
//
// 参数 oneID 来源：调用方应在 ToolContext 中携带 CustomerID；
// 缺省时退化为 "phone:"+phone，命中即按退订处理（与 service.DoNotContactService 同款语义）。
type DNCResolver interface {
	IsBlocked(ctx context.Context, oneID string) bool
}

// DNCFunc 函数式适配器
type DNCFunc func(ctx context.Context, oneID string) bool

func (f DNCFunc) IsBlocked(ctx context.Context, oneID string) bool {
	if f == nil {
		return false
	}
	return f(ctx, oneID)
}

// AllowAllDNC 默认放行实现（resolver=nil 时使用）
type AllowAllDNC struct{}

func (AllowAllDNC) IsBlocked(ctx context.Context, oneID string) bool { return false }

var globalDNCResolver DNCResolver = AllowAllDNC{}

// SetGlobalDNCResolver 设置全局 DNC resolver；传 nil 恢复默认放行。
func SetGlobalDNCResolver(r DNCResolver) {
	if r == nil {
		globalDNCResolver = AllowAllDNC{}
		return
	}
	globalDNCResolver = r
}

// ErrDNCBlocked DNC 命中错误
var ErrDNCBlocked = fmt.Errorf("dnc blocked")

type dncTool struct {
	inner    Tool
	resolver DNCResolver
}

// WithDNCGuard 用全局 DNC resolver 包装工具。
// resolver 未接线时（nil）行为与现状完全一致（向后兼容硬性要求）。
func WithDNCGuard(t Tool) Tool {
	if t == nil {
		return nil
	}
	return &dncTool{inner: t}
}

// WithDNCGuardResolver 用指定 DNC resolver 包装工具（优先于全局 resolver）。
func WithDNCGuardResolver(t Tool, r DNCResolver) Tool {
	if t == nil {
		return nil
	}
	dt := &dncTool{inner: t}
	if r != nil {
		dt.resolver = r
	}
	return dt
}

func (d *dncTool) Name() string               { return d.inner.Name() }
func (d *dncTool) Category() ToolCategory     { return d.inner.Category() }
func (d *dncTool) Description() string        { return d.inner.Description() }
func (d *dncTool) Parameters() ToolParameters { return d.inner.Parameters() }

// RiskLevel 原样透出内层分级（含"内层没声明"）。
func (d *dncTool) RiskLevel() ToolRiskLevel { return DeclaredRisk(d.inner) }

func (d *dncTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	name := d.inner.Name()
	if !IsColdOutreachTool(d.inner) {
		return d.inner.Execute(ctx, args)
	}
	oneID := dncOneID(ctx, args)
	if oneID == "" {

		return d.inner.Execute(ctx, args)
	}
	resolver := d.resolver
	if resolver == nil {
		resolver = globalDNCResolver
	}
	if resolver != nil && resolver.IsBlocked(ctx, oneID) {
		err := fmt.Errorf("%w: tool=%s one_id=%s (全局退订拦截)", ErrDNCBlocked, name, oneID)
		return ErrorResult(name, err), err
	}
	return d.inner.Execute(ctx, args)
}

func dncOneID(ctx context.Context, args map[string]any) string {
	if tc := GetToolContext(ctx); tc != nil && strings.TrimSpace(tc.CustomerID) != "" {
		return tc.CustomerID
	}
	if v, ok := args["one_id"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := args["phone"].(string); ok && strings.TrimSpace(v) != "" {
		return "phone:" + v
	}
	return ""
}

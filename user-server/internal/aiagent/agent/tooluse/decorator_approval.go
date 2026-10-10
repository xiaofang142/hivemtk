package tooluse

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// ApprovalChecker 冷触达工具审批检查接口。
// accountIDorOwnerKey：调用方账号 ID / owner key（取 ToolContext.CallerID，缺省回退 AgentID）。
type ApprovalChecker interface {
	IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool
}

var ErrApprovalDenied = fmt.Errorf("approval denied")

var globalApprovalChecker atomic.Pointer[ApprovalChecker]

// SetGlobalApprovalChecker 设置全局审批检查器；传 nil 恢复默认放行行为。
func SetGlobalApprovalChecker(c ApprovalChecker) {
	if c == nil {
		globalApprovalChecker.Store(nil)
		return
	}
	globalApprovalChecker.Store(&c)
}

// AllowAllChecker 默认放行实现（保持现有行为不变）。
type AllowAllChecker struct{}

func (AllowAllChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	return true
}

var coldOutreachNameSegs = map[string]bool{
	"batch":       true,
	"schedule":    true,
	"outbound":    true,
	"dm":          true,
	"lead":        true,
	"proactive":   true,
	"telegram_dm": true,
}

var coldOutreachNameSubstrs = []string{"batch_", "schedule_", "outbound_", "lead_", "proactive_", "_outreach"}

// IsColdOutreachTool 判定工具是否需要冷触达审批门（A-1）。
//
// 双重门禁：Category 必须是 CategoryReach + 名称段命中冷触达关键词。
// 会话内回复（confidence 门禁 + 六否决链）不触发审批——这类工具：
//   - Category 为 CategoryPrivateMessage / CategoryCustomer / CategoryKnowledge / CategoryBusiness
//   - 或 CategoryReach 但名称段仅为 recall/health/history/template/account（非外发）
//
// 命中场景示例：
//   - reach.batch, reach.schedule, reach.*.outbound → 批量/计划外发
//   - reach.telegram.dm, reach.*.lead_outreach → 冷 DM
//   - proactive_reach.* → 主动触达计划
func IsColdOutreachTool(t Tool) bool {
	if t == nil {
		return false
	}
	if t.Category() != CategoryReach {
		return false
	}
	name := strings.ToLower(t.Name())
	for _, seg := range strings.Split(name, ".") {
		if coldOutreachNameSegs[seg] {
			return true
		}
		for _, sub := range coldOutreachNameSubstrs {
			if strings.Contains(seg, sub) {
				return true
			}
		}
	}
	return false
}

type approvalTool struct {
	inner   Tool
	checker *ApprovalChecker
}

// WithApproval 用全局审批检查器包装工具。
// 未接线时（全局 checker 为空）行为与现状完全一致。
func WithApproval(t Tool) Tool {
	if t == nil {
		return nil
	}
	return &approvalTool{inner: t}
}

// WithApprovalChecker 用指定审批检查器包装工具（优先于全局 checker）。
func WithApprovalChecker(t Tool, c ApprovalChecker) Tool {
	if t == nil {
		return nil
	}
	at := &approvalTool{inner: t}
	if c != nil {
		at.checker = &c
	}
	return at
}

func (a *approvalTool) Name() string               { return a.inner.Name() }
func (a *approvalTool) Category() ToolCategory     { return a.inner.Category() }
func (a *approvalTool) Description() string        { return a.inner.Description() }
func (a *approvalTool) Parameters() ToolParameters { return a.inner.Parameters() }

// RiskLevel 原样透出内层分级（含"内层没声明"）。
func (a *approvalTool) RiskLevel() ToolRiskLevel { return DeclaredRisk(a.inner) }

func (a *approvalTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if IsColdOutreachTool(a.inner) {
		checker := a.checker
		if checker == nil {
			checker = globalApprovalChecker.Load()
		}
		ownerKey := approvalOwnerKey(ctx)
		if checker != nil && !(*checker).IsApproved(ctx, a.inner.Name(), ownerKey) {
			err := approvalDeniedError(a.inner, ownerKey)
			return ErrorResult(a.inner.Name(), err), err
		}
	}
	return a.inner.Execute(ctx, args)
}

// approvalDeniedError 拼一句**能照着处置**的拒绝原因。
//
// 只说 "requires cold outreach approval" 时，两种完全不同的故障长得一样：
//   - 这个账号确实没被放行（去授权即可）；
//   - 这次调用根本没带上可归属的账号（CallerID/AgentID 都空）—— 于是白名单**永远**
//     匹配不上，授权多少次都照旧被拒。
//
// 第二种的修法在调用链上游，而操作者按第一种读完会反复去点授权按钮。把实际用于匹配的
// 那个键写进错误里，两端读的是同一个数（这里传的就是 IsApproved 收到的那一份，
// 不是另算一次 —— 另算会让提示与被执行的判据在 approvalOwnerKey 改动的那天分开）。
func approvalDeniedError(t Tool, ownerKey string) error {
	if strings.TrimSpace(ownerKey) == "" {
		return fmt.Errorf("%w: tool %s (%s) requires cold outreach approval —— "+
			"本次调用没有带上可归属的账号身份（CallerID/AgentID 均为空），白名单按空键查过、无从匹配；"+
			"请先修调用方传入的上下文，再谈授权",
			ErrApprovalDenied, t.Name(), t.Category())
	}
	return fmt.Errorf("%w: tool %s (%s) requires cold outreach approval —— 白名单按 owner=%q 查过，没有有效条目",
		ErrApprovalDenied, t.Name(), t.Category(), ownerKey)
}

func approvalOwnerKey(ctx context.Context) string {
	tc := GetToolContext(ctx)
	if tc == nil {
		return ""
	}
	if tc.CallerID != "" {
		return tc.CallerID
	}
	return tc.AgentID
}

// ApprovalGateDecorator 把审批门挂到装饰器链上（T-P1-05）。
//
// 为什么不用上面的 WithApproval（Tool 包装）：那条路要在每个注册点各包一层，而它在生产里
// 一个调用点都没有（只剩测试在用）；装饰器形态只需在 executor 唯一的建链点表态一次。
// 位置因此变成显式决策：放在 permission/ratelimit/audit/feedback 之外，
// 被拒的冷触达既不消耗配额也不留下"执行过一次外发"的记录。
// 重试不在考虑范围内——ClassifyToolError 已把 TOOL_APPROVAL_DENIED 归入
// isNonRetryableError，两种包装形态都不会重试。
//
// shadow 的语义必须是结构性的：shadow=true 时**没有一条路径**会返回拒绝，
// 判定结果只经 checker 自己的 OnDecision 回调留痕（见 internal/approval）。
// 拒绝结论由 checker.IsApproved 决定，本装饰器不复制白名单逻辑。
func ApprovalGateDecorator(t Tool, checker ApprovalChecker, shadow bool) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			if checker == nil || t == nil || !IsColdOutreachTool(t) {
				return next(ctx, args)
			}
			ownerKey := approvalOwnerKey(ctx)
			if checker.IsApproved(ctx, t.Name(), ownerKey) {
				return next(ctx, args)
			}
			if shadow {
				return next(ctx, args)
			}
			// 与 approvalTool.Execute 共用同一个构造点：两条包装形态给的拒绝必须说同一句话，
			// 否则换一条接线方式（装饰器链 ↔ Tool 包装）操作者读到的处置方向就换了。
			err := approvalDeniedError(t, ownerKey)
			return ErrorResult(t.Name(), err), err
		}
	}
}

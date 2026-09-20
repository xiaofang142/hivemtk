// Package approval 落地 A-1 决策：reach 类工具审批门。
//
// 提供默认实现 WhiteListApprovalChecker：
//   - 默认拒绝所有 cold outreach 工具，除非白名单显式开启
//   - 支持按 (toolName, accountID) 维度白名单
//   - 所有拒绝/放行行为都会留痕（通过 OnDecision 回调）
//
// 注意：本包不直接触达任何 DB/Redis；持久化由调用方在 OnDecision 中实现。
package approval

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/featureflag"
)

const (
	FlagKey = "ai.safety.tool_approval_gate"
)

// Decision 决策结果。
type Decision struct {
	Allowed    bool
	Reason     string
	FlagReason string
}

// OnDecision 决策钩子（异步调用方负责，调用方需自行处理重试/降级）。
type OnDecision func(ctx context.Context, toolName, accountID string, d Decision)

// WhiteListApprovalChecker 白名单实现。
type WhiteListApprovalChecker struct {
	mu        sync.RWMutex
	whitelist map[string]map[string]time.Time
	callback  OnDecision
	nowFn     func() time.Time
}

// NewWhiteList 创建实例；nowFn 可注入便于测试。
func NewWhiteList(cb OnDecision, nowFn func() time.Time) *WhiteListApprovalChecker {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &WhiteListApprovalChecker{
		whitelist: make(map[string]map[string]time.Time),
		callback:  cb,
		nowFn:     nowFn,
	}
}

// Whitelist 把 (tool, account) 加入白名单，expiresAt 为零表示永不过期。
func (w *WhiteListApprovalChecker) Whitelist(toolName, accountID string, expiresAt time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m := w.whitelist[toolName]
	if m == nil {
		m = make(map[string]time.Time)
		w.whitelist[toolName] = m
	}
	m[accountID] = expiresAt
}

// Revoke 撤销白名单。
func (w *WhiteListApprovalChecker) Revoke(toolName, accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if m, ok := w.whitelist[toolName]; ok {
		delete(m, accountID)
	}
}

// IsApproved 实现 ApprovalChecker 接口。
//
// 需要知道"为什么"的调用方（转阻断时的放行抑制判断）走 Verdict，两者同源同回调，
// 分成两次调用会留下判定与理由脱节的空间。
func (w *WhiteListApprovalChecker) IsApproved(ctx context.Context, toolName, accountID string) bool {
	allowed, _ := w.Verdict(ctx, toolName, accountID)
	return allowed
}

// Verdict 返回判定与理由，并按 IsApproved 同样的方式回调留痕一次。
//
// 理由必须与判定同源于 decide()：报告里 `would_deny` 的含义（"切阻断后会被拦的量"）
// 依赖这一点，若理由另算一份，disabled_by_flag 与 denied_default 的占比就会失真。
func (w *WhiteListApprovalChecker) Verdict(ctx context.Context, toolName, accountID string) (bool, string) {
	flagOn := featureflag.Get(FlagKey).Bool()
	d := w.decide(ctx, toolName, accountID, flagOn)
	if w.callback != nil {
		w.callback(ctx, toolName, accountID, d)
	}
	return d.Allowed, d.Reason
}

// ActiveEntryCount 当前仍有效（未过期）的白名单条目数。
//
// 供转阻断前的自检与观测端点使用：block 态 + 0 条有效条目 = 所有冷触达都会被拒，
// 这个数字必须能在开旗之前读到，而不是靠"上线后没人能外发"发现。
// 过期条目在此按判定时同口径排除（用 nowFn，与 decide 一致）。
func (w *WhiteListApprovalChecker) ActiveEntryCount() int {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	now := w.nowFn()
	n := 0
	for _, accounts := range w.whitelist {
		for _, exp := range accounts {
			if exp.IsZero() || now.Before(exp) {
				n++
			}
		}
	}
	return n
}

// ActiveEntryCountFor 某个入口（toolName）名下仍有效的授权条数。
//
// 与 ActiveEntryCount 同口径（过期不计、nil 接收者返回 0），只是把遍历收在这一个 key 上。
// 需要分项的原因：冷触达工具与外发闸门共用同一张表，只报总数会让运维看不出
// "reach 一条授权都没有，那 3 条在别的入口上"——block 态下这个差别就是全拦与全放。
func (w *WhiteListApprovalChecker) ActiveEntryCountFor(toolName string) int {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	now := w.nowFn()
	n := 0
	for _, exp := range w.whitelist[toolName] {
		if exp.IsZero() || now.Before(exp) {
			n++
		}
	}
	return n
}

func (w *WhiteListApprovalChecker) decide(ctx context.Context, toolName, accountID string, flagOn bool) Decision {
	if !flagOn {
		return Decision{Allowed: false, Reason: ReasonDisabledByFlag}
	}
	w.mu.RLock()
	m, ok := w.whitelist[toolName]
	if !ok {
		w.mu.RUnlock()
		return Decision{Allowed: false, Reason: ReasonDeniedDefault}
	}
	exp, hit := m[accountID]
	w.mu.RUnlock()
	if !hit {
		return Decision{Allowed: false, Reason: ReasonDeniedDefault}
	}
	if !exp.IsZero() && w.nowFn().After(exp) {
		return Decision{Allowed: false, Reason: ReasonDeniedExplicit}
	}
	return Decision{Allowed: true, Reason: ReasonWhitelisted}
}

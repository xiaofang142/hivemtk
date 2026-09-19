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
func (w *WhiteListApprovalChecker) IsApproved(ctx context.Context, toolName, accountID string) bool {
	flagOn := featureflag.Get(FlagKey).Bool()
	d := w.decide(ctx, toolName, accountID, flagOn)
	if w.callback != nil {
		w.callback(ctx, toolName, accountID, d)
	}
	return d.Allowed
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

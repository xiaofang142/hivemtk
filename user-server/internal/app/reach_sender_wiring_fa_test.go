package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hivemtk-user/internal/service"
)

// faReachAdapter 记录被真实投递的发送意图（零外发断言的凭据）。
type faReachAdapter struct {
	mu    sync.Mutex
	calls []*service.ReachSendRequest
}

func (a *faReachAdapter) Send(_ context.Context, req *service.ReachSendRequest) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	return "mid-" + req.Channel, nil
}

func (a *faReachAdapter) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func (a *faReachAdapter) first() *service.ReachSendRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.calls) == 0 {
		return nil
	}
	return a.calls[0]
}

// faConsent 可控同意判定：revoked/paused 拒发，granted 放行。
type faConsent struct{ status string }

func (c faConsent) CheckSendConsent(_ context.Context, _, _, _ string) service.ConsentDecision {
	return service.ConsentDecision{Status: c.status, Reason: c.status}
}

func faGatedSender(cfg service.SendPipelineConfig) (*pipelineReachSender, *faReachAdapter) {
	adapter := &faReachAdapter{}
	if cfg.Adapter == nil {
		cfg.Adapter = adapter
	}
	if cfg.PermissionChecker == nil {
		cfg.PermissionChecker = service.AllowAllSendPermission{}
	}
	if cfg.RateLimiter == nil {
		cfg.RateLimiter = service.NoOpSendRateLimiter{}
	}
	if cfg.AuditLogger == nil {
		cfg.AuditLogger = service.NewMemorySendAuditLogger(10)
	}
	if cfg.CostTracker == nil {
		cfg.CostTracker = service.NoOpSendCostTracker{}
	}
	if cfg.JourneyTracker == nil {
		cfg.JourneyTracker = service.NoOpSendJourneyTracker{}
	}
	gate := service.NewSendPipeline(cfg)
	return &pipelineReachSender{gate: gate}, adapter
}

// TestFAReach01_RevokedConsentZeroSend 撤回同意 → 零外发。
func TestFAReach01_RevokedConsentZeroSend(t *testing.T) {
	sender, adapter := faGatedSender(service.SendPipelineConfig{
		ConsentChecker: faConsent{status: service.ConsentStatusRevoked},
		ConsentMode:    service.ConsentModeRevocationOnly,
	})
	_, err := sender.SendReach(context.Background(), "sms", "acc-1", "u-revoked", "内容")
	if err == nil {
		t.Fatal("撤回同意必须拒发")
	}
	if adapter.count() != 0 {
		t.Fatalf("撤回同意零外发，实际投递 %d 次", adapter.count())
	}
}

// TestFAReach01_PausedConsentZeroSend 暂停偏好 → 零外发。
func TestFAReach01_PausedConsentZeroSend(t *testing.T) {
	sender, adapter := faGatedSender(service.SendPipelineConfig{
		ConsentChecker: faConsent{status: service.ConsentStatusPaused},
		ConsentMode:    service.ConsentModeRevocationOnly,
	})
	_, err := sender.SendReach(context.Background(), "wecom", "acc-1", "u-paused", "内容")
	if err == nil {
		t.Fatal("暂停偏好必须拒发")
	}
	if adapter.count() != 0 {
		t.Fatalf("暂停偏好零外发，实际投递 %d 次", adapter.count())
	}
}

// TestFAReach01_ApprovalDeniedZeroSend 审批拒绝（强制闸）→ 零外发。
func TestFAReach01_ApprovalDeniedZeroSend(t *testing.T) {
	sender, adapter := faGatedSender(service.SendPipelineConfig{
		MandatoryGate: func(context.Context, *service.ReachSendRequest) error {
			return service.ErrReachApprovalDenied
		},
	})
	_, err := sender.SendReach(context.Background(), "email", "acc-1", "u-denied", "内容")
	if !errors.Is(err, service.ErrReachApprovalDenied) {
		t.Fatalf("审批拒绝应返回该哨兵，实际 %v", err)
	}
	if adapter.count() != 0 {
		t.Fatalf("审批拒绝零外发，实际投递 %d 次", adapter.count())
	}
}

// TestFAReach01_QuotaDeniedZeroSend 配额不足（强制闸）→ 零外发。
func TestFAReach01_QuotaDeniedZeroSend(t *testing.T) {
	sender, adapter := faGatedSender(service.SendPipelineConfig{
		MandatoryGate: func(context.Context, *service.ReachSendRequest) error {
			return service.ErrSendQuotaDenied
		},
	})
	_, err := sender.SendReach(context.Background(), "sms", "acc-1", "u-quota", "内容")
	if !errors.Is(err, service.ErrSendQuotaDenied) {
		t.Fatalf("配额不足应返回该哨兵，实际 %v", err)
	}
	if adapter.count() != 0 {
		t.Fatalf("配额不足零外发，实际投递 %d 次", adapter.count())
	}
}

// TestFAReach01_OnlySendConfigStillGated Steps 只配 send 也不能裁掉强制闸。
func TestFAReach01_OnlySendConfigStillGated(t *testing.T) {
	gateCalls := 0
	sender, adapter := faGatedSender(service.SendPipelineConfig{
		Steps: []string{service.SendStepSend}, // 仅 send 配置
		MandatoryGate: func(context.Context, *service.ReachSendRequest) error {
			gateCalls++
			return service.ErrReachApprovalDenied
		},
	})
	_, err := sender.SendReach(context.Background(), "sms", "acc-1", "u-only-send", "内容")
	if err == nil {
		t.Fatal("仅 send 配置下强制闸仍应拒发")
	}
	if gateCalls != 1 {
		t.Fatalf("强制闸应恒跑一次，实际 %d 次", gateCalls)
	}
	if adapter.count() != 0 {
		t.Fatalf("强制闸拒发零外发，实际投递 %d 次", adapter.count())
	}
}

// TestFAReach01_AllowedPreservesRouting 正常放行恰好一次投递，且渠道/账号/收件人/内容不变。
func TestFAReach01_AllowedPreservesRouting(t *testing.T) {
	sender, adapter := faGatedSender(service.SendPipelineConfig{})
	mid, err := sender.SendReach(context.Background(), "telegram", "acc-42", "u-ok", "你好")
	if err != nil {
		t.Fatalf("正常请求不应被拒: %v", err)
	}
	if mid != "mid-telegram" {
		t.Fatalf("message_id 应回传适配器结果，实际 %q", mid)
	}
	if adapter.count() != 1 {
		t.Fatalf("恰好一次投递，实际 %d 次", adapter.count())
	}
	got := adapter.first()
	if got.Channel != "telegram" || got.AccountID != "acc-42" || got.RecipientID != "u-ok" || got.Content != "你好" {
		t.Fatalf("路由/租户标识被改写: %+v", got)
	}
}

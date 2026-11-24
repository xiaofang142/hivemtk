package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// 网络响应丢失不能证明未发送，不能重试或换渠道。
func TestReachDispatch_UnknownNeverRetriesOrFallsBack(t *testing.T) {
	for _, outcome := range []error{context.DeadlineExceeded, errors.New("connection reset by peer"), errors.New("provider response lost")} {
		t.Run(outcome.Error(), func(t *testing.T) {
			primary := NewAlwaysFailAdapter(outcome)
			backup := newSuccessAdapter("backup")
			cfg := DefaultSendPipelineConfig(primary)
			cfg.RetryPolicy = SendRetryPolicy{MaxRetries: 3, IntervalMs: 1}
			cfg.FallbackAdapters = map[string]ChannelAdapter{"email": backup}
			resp := NewSendPipeline(cfg).Send(t.Context(), &ReachSendRequest{Channel: "sms", RecipientID: "fixture-phone", Content: "fixture", Fallback: &FallbackConfig{Enabled: true, BackupChannel: "email", MaxAttempts: 2}})
			require.False(t, resp.Success)
			require.EqualValues(t, 1, primary.Count(t.Context()), "未知结果禁止重试")
			require.Zero(t, backup.Count(t.Context()), "未知结果禁止换渠")
			require.Zero(t, resp.RetryCount)
		})
	}
}

type dispatchPermission struct {
	calls int
}

func (p *dispatchPermission) CheckSendPermission(ctx context.Context, req *ReachSendRequest) error {
	p.calls++
	if p.calls > 1 {
		return ErrSendConsentRevoked
	}
	return nil
}

// 首次许可不能永久授权后续重试，撤回后必须重新拒绝。
func TestReachDispatch_RechecksBeforeRetry(t *testing.T) {
	primary := NewAlwaysFailAdapter(&ChannelError{Category: CategoryRateLimited, Retryable: true, StatusCode: 429, Raw: "fixture rejection"})
	permission := &dispatchPermission{}
	cfg := DefaultSendPipelineConfig(primary)
	cfg.PermissionChecker = permission
	cfg.RetryPolicy = SendRetryPolicy{MaxRetries: 2, IntervalMs: 1}
	resp := NewSendPipeline(cfg).Send(t.Context(), &ReachSendRequest{Channel: "sms", RecipientID: "fixture", Content: "fixture"})
	require.False(t, resp.Success)
	require.EqualValues(t, 1, primary.Count(t.Context()), "撤回后不得再次调用提供方")
	require.GreaterOrEqual(t, permission.calls, 2)
}

// DryRun 在最外层隔离生产适配器及费用；无消息 ID，不伪造平台受理。
func TestReachDispatch_DryRunNeverInvokesProduction(t *testing.T) {
	adapter := NewAlwaysFailAdapter(errors.New("production must be unreachable"))
	resp := NewSendPipeline(DefaultSendPipelineConfig(adapter)).Send(t.Context(), &ReachSendRequest{Channel: "sms", RecipientID: "fixture", Content: "fixture", DryRun: true})
	require.True(t, resp.Success)
	require.True(t, resp.DryRun)
	require.Equal(t, "simulated", resp.DeliveryState)
	require.Empty(t, resp.MessageID)
	require.Zero(t, resp.ActualAttempts)
	require.Zero(t, adapter.Count(t.Context()))
}

// 备用出口必须独立具有收件身份并通过许可，不能复用手机为邮箱。
func TestReachDispatch_FallbackRequiresIndependentRecipient(t *testing.T) {
	primary := NewAlwaysFailAdapter(&ChannelError{Category: CategoryBadRequest, StatusCode: 400, Raw: "rejected"})
	backup := newSuccessAdapter("backup")
	cfg := DefaultSendPipelineConfig(primary)
	cfg.FallbackAdapters = map[string]ChannelAdapter{"email": backup}
	resp := NewSendPipeline(cfg).Send(t.Context(), &ReachSendRequest{Channel: "sms", RecipientID: "fixture", Fallback: &FallbackConfig{Enabled: true, BackupChannel: "email"}})
	require.False(t, resp.Success)
	require.Zero(t, backup.Count(t.Context()))
	require.False(t, resp.OutcomeUnknown)
}

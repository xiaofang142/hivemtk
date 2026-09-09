package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func newTestPipeline(adapter ChannelAdapter) SendPipeline {
	return NewSendPipeline(DefaultSendPipelineConfig(adapter))
}

func newSuccessAdapter(prefix string) *FuncChannelAdapter {
	return NewFuncChannelAdapter(func(ctx context.Context, req *ReachSendRequest) (string, error) {
		return fmt.Sprintf("%s-%s", prefix, req.RecipientID), nil
	})
}

func findStepResult(resp *SendResponse, step string) *SendStepLog {
	if resp == nil {
		return nil
	}
	for i := range resp.StepResults {
		if resp.StepResults[i].Step == step {
			return &resp.StepResults[i]
		}
	}
	return nil
}

func stepExists(resp *SendResponse, step string) bool {
	return findStepResult(resp, step) != nil
}

type denyAllPermission struct{}

func (denyAllPermission) CheckSendPermission(ctx context.Context, req *ReachSendRequest) error {
	return ErrSendPermissionDenied
}

func TestSendPipeline_HappyPath_All9StepsExecuted(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	pipeline := newTestPipeline(adapter)

	req := &ReachSendRequest{
		Channel:     "sms",
		AccountID:   "acc-1",
		RecipientID: "13800138000",
		CustomerID:  "cust-1",
		OperatorID:  "op-1",
		Content:     "Hello, this is a normal message",
	}

	resp := pipeline.Send(context.Background(), req)

	if !resp.Success {
		t.Fatalf("期望成功，实际失败: %s", resp.Error)
	}
	if resp.MessageID == "" {
		t.Fatal("期望返回 message_id，实际为空")
	}
	if resp.Channel != "sms" {
		t.Errorf("期望 channel=sms，实际 %s", resp.Channel)
	}
	if resp.PrimaryChannel != "sms" {
		t.Errorf("期望 primary_channel=sms，实际 %s", resp.PrimaryChannel)
	}
	expectedSteps := []string{
		SendStepPermission, SendStepRateLimit,
		SendStepRetry, SendStepFallback, SendStepAudit,
		SendStepCost, SendStepJourney, SendStepSend,
	}
	if len(resp.StepResults) != len(expectedSteps) {
		t.Fatalf("期望 %d 步，实际 %d 步", len(expectedSteps), len(resp.StepResults))
	}
	for _, step := range expectedSteps {
		if !stepExists(resp, step) {
			t.Errorf("期望步骤 %s 已执行", step)
		}
	}
	if resp.RetryCount != 0 {
		t.Errorf("期望 retry_count=0（一次成功），实际 %d", resp.RetryCount)
	}
	if resp.FallbackUsed {
		t.Error("期望未使用降级")
	}
}

func TestSendPipeline_PermissionDenied(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.PermissionChecker = denyAllPermission{}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)

	if resp.Success {
		t.Fatal("期望失败（权限拒绝），实际成功")
	}
	if !errors.Is(errors.New(resp.Error), ErrSendPermissionDenied) && !strings.Contains(resp.Error, ErrSendPermissionDenied.Error()) {
		t.Errorf("期望错误包含 %q，实际 %q", ErrSendPermissionDenied.Error(), resp.Error)
	}
	permStep := findStepResult(resp, SendStepPermission)
	if permStep == nil {
		t.Fatal("期望有 permission 步骤")
	}
	if permStep.Success {
		t.Error("期望 permission 步骤失败")
	}
	if stepExists(resp, SendStepRateLimit) {
		t.Error("权限拒绝后不应执行 rate_limit 步骤")
	}
	if stepExists(resp, SendStepSend) {
		t.Error("权限拒绝后不应执行 send 步骤")
	}
}

func TestSendPipeline_RateLimited(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.RateLimiter = NewMemorySendRateLimiter()
	cfg.RateLimitSpec = RateLimitSpec{QPS: 1, Burst: 1}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		AccountID:   "acc-1",
		RecipientID: "13800138000",
		CustomerID:  "cust-1",
		Content:     "Hello",
	}

	resp1 := pipeline.Send(context.Background(), req)
	if !resp1.Success {
		t.Fatalf("第一次发送应该成功，失败: %s", resp1.Error)
	}
	resp2 := pipeline.Send(context.Background(), req)
	if resp2.Success {
		t.Fatal("第二次发送应该被限流")
	}
	rlStep := findStepResult(resp2, SendStepRateLimit)
	if rlStep == nil {
		t.Fatal("期望有 rate_limit 步骤")
	}
	if rlStep.Success {
		t.Error("期望 rate_limit 步骤失败")
	}
	if !strings.Contains(resp2.Error, ErrSendRateLimited.Error()) {
		t.Errorf("期望错误包含 %q，实际 %q", ErrSendRateLimited.Error(), resp2.Error)
	}
	if stepExists(resp2, SendStepSend) {
		t.Error("限流后不应执行 send 步骤")
	}
}

func TestSendPipeline_RetryWithFlakyAdapter(t *testing.T) {
	adapter := NewFlakyAdapter(2)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.RetryPolicy = SendRetryPolicy{
		MaxRetries:    3,
		IntervalMs:    10,
		Backoff:       "exponential",
		MaxIntervalMs: 100,
	}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	}
	start := time.Now()
	resp := pipeline.Send(context.Background(), req)
	elapsed := time.Since(start)

	if !resp.Success {
		t.Fatalf("期望最终成功（重试后），失败: %s", resp.Error)
	}
	if resp.RetryCount < 2 {
		t.Errorf("期望 retry_count>=2（前 2 次失败），实际 %d", resp.RetryCount)
	}
	if adapter.Count(context.Background()) < 3 {
		t.Errorf("期望 adapter 至少调用 3 次，实际 %d", adapter.Count(context.Background()))
	}
	if elapsed < 25*time.Millisecond {
		t.Errorf("期望指数退避至少 30ms，实际 %v", elapsed)
	}
}

func TestSendPipeline_RetryExhaustedAllFail(t *testing.T) {
	adapter := NewAlwaysFailAdapter(errors.New("network error"))
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.RetryPolicy = SendRetryPolicy{
		MaxRetries:    2,
		IntervalMs:    5,
		Backoff:       "exponential",
		MaxIntervalMs: 50,
	}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)

	if resp.Success {
		t.Fatal("期望失败（全部重试失败）")
	}
	if adapter.Count(context.Background()) != 3 {
		t.Errorf("期望 adapter 调用 3 次（1 + 2 retries），实际 %d", adapter.Count(context.Background()))
	}
	if resp.RetryCount != 2 {
		t.Errorf("期望 retry_count=2，实际 %d", resp.RetryCount)
	}
}

func TestSendPipeline_FallbackToBackupChannel(t *testing.T) {
	primaryAdapter := NewAlwaysFailAdapter(errors.New("primary down"))
	backupAdapter := newSuccessAdapter("backup-msg")
	cfg := DefaultSendPipelineConfig(primaryAdapter)
	cfg.FallbackAdapters = map[string]ChannelAdapter{
		"email": backupAdapter,
	}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
		Fallback: &FallbackConfig{
			Enabled:       true,
			BackupChannel: "email",
			BackupAccount: "backup-acc-1",
			MaxAttempts:   1,
		},
	}
	resp := pipeline.Send(context.Background(), req)

	if !resp.Success {
		t.Fatalf("期望降级后成功，失败: %s", resp.Error)
	}
	if resp.MessageID == "" {
		t.Error("期望返回 message_id")
	}
	if primaryAdapter.Count(context.Background()) < 1 {
		t.Errorf("主渠道至少调用 1 次，实际 %d", primaryAdapter.Count(context.Background()))
	}
	if backupAdapter.Count(context.Background()) < 1 {
		t.Errorf("备用渠道至少调用 1 次，实际 %d", backupAdapter.Count(context.Background()))
	}
}

func TestSendPipeline_FallbackDisabled(t *testing.T) {
	primaryAdapter := NewAlwaysFailAdapter(errors.New("primary down"))
	backupAdapter := newSuccessAdapter("backup-msg")
	cfg := DefaultSendPipelineConfig(primaryAdapter)
	cfg.FallbackAdapters = map[string]ChannelAdapter{
		"email": backupAdapter,
	}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
		Fallback: &FallbackConfig{
			Enabled: false,
		},
	}
	resp := pipeline.Send(context.Background(), req)

	if resp.Success {
		t.Fatal("期望失败（降级未启用）")
	}
	if backupAdapter.Count(context.Background()) > 0 {
		t.Error("降级未启用时备用渠道不应被调用")
	}
}

func TestSendPipeline_AuditLogRecorded(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	auditLogger := NewMemorySendAuditLogger(100)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.AuditLogger = auditLogger
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		AccountID:   "acc-1",
		RecipientID: "13800138000",
		CustomerID:  "cust-1",
		OperatorID:  "op-1",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)

	if !resp.Success {
		t.Fatalf("期望成功，失败: %s", resp.Error)
	}
	entries := auditLogger.Entries(context.Background())
	if len(entries) == 0 {
		t.Fatal("期望至少 1 条审计日志，实际 0 条")
	}
	last := entries[len(entries)-1]
	if last.Channel != "sms" {
		t.Errorf("期望 channel=sms，实际 %s", last.Channel)
	}
	if last.Recipient != "13800138000" {
		t.Errorf("期望 recipient=13800138000，实际 %s", last.Recipient)
	}
	if !last.Success {
		t.Error("期望审计日志标记为成功")
	}
	if last.MessageID == "" {
		t.Error("期望审计日志包含 message_id")
	}
}

func TestSendPipeline_AuditLogRecordsContent(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	auditLogger := NewMemorySendAuditLogger(100)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.AuditLogger = auditLogger
	pipeline := NewSendPipeline(cfg)

	content := "Hello test message"
	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     content,
	}
	resp := pipeline.Send(context.Background(), req)
	if !resp.Success {
		t.Fatalf("期望成功，失败: %s", resp.Error)
	}

	entries := auditLogger.Entries(context.Background())
	if len(entries) == 0 {
		t.Fatal("期望审计日志非空")
	}
	found := false
	for _, e := range entries {
		if e.Content == content {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("期望审计日志包含内容 %q", content)
	}
}

func TestSendPipeline_AuditLogOnFailure(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	auditLogger := NewMemorySendAuditLogger(100)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.AuditLogger = auditLogger
	cfg.RateLimiter = NewMemorySendRateLimiter()
	cfg.RateLimitSpec = RateLimitSpec{QPS: 1, Burst: 0}
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		AccountID:   "acc-audit",
		RecipientID: "13800138000",
		CustomerID:  "cust-1",
		OperatorID:  "op-1",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)
	if resp.Success {
		t.Fatal("期望失败（限流）")
	}
	entries := auditLogger.Entries(context.Background())
	if len(entries) == 0 {
		t.Fatal("PRD 合规失败：失败时未记录审计日志")
	}
	last := entries[len(entries)-1]
	if last.Success {
		t.Error("期望审计日志标记为失败")
	}
	if !strings.Contains(last.Error, ErrSendRateLimited.Error()) {
		t.Errorf("期望审计日志 error 包含 %q，实际 %q", ErrSendRateLimited.Error(), last.Error)
	}
	if last.Channel != "sms" {
		t.Errorf("期望 channel=sms，实际 %s", last.Channel)
	}
	if last.Recipient != "13800138000" {
		t.Errorf("期望 recipient=13800138000，实际 %s", last.Recipient)
	}
}

func TestSendPipeline_CostTrackerCharged(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	costTracker := NewMemorySendCostTracker(100.0)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.CostTracker = costTracker
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)
	if !resp.Success {
		t.Fatalf("期望成功，失败: %s", resp.Error)
	}
	balance := costTracker.Balance(context.Background())
	if balance >= 100.0 {
		t.Errorf("期望余额 < 100，实际 %f", balance)
	}
	used := costTracker.TotalUsed(context.Background())
	if used <= 0 {
		t.Errorf("期望累计消费 > 0，实际 %f", used)
	}
	costStep := findStepResult(resp, SendStepCost)
	if costStep == nil {
		t.Fatal("期望有 cost 步骤")
	}
	if !costStep.Success {
		t.Errorf("期望 cost 步骤成功: %s", costStep.Error)
	}
}

func TestSendPipeline_CostTrackerInsufficientBalance(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	costTracker := NewMemorySendCostTracker(0.01)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.CostTracker = costTracker
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)
	if resp.Success {
		t.Fatal("期望失败（余额不足）")
	}
	costStep := findStepResult(resp, SendStepCost)
	if costStep == nil {
		t.Fatal("期望有 cost 步骤")
	}
	if costStep.Success {
		t.Error("期望 cost 步骤失败")
	}
	if !strings.Contains(resp.Error, ErrSendInsufficientCost.Error()) {
		t.Errorf("期望错误包含 %q，实际 %q", ErrSendInsufficientCost.Error(), resp.Error)
	}
}

type recordingJourneyTracker struct {
	mu     sync.Mutex
	calls  []journeyCall
	hasErr bool
}

type journeyCall struct {
	CustomerID string
	Channel    string
	Source     string
}

func (m *recordingJourneyTracker) RecordTouch(ctx context.Context, customerID, channel, source string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, journeyCall{CustomerID: customerID, Channel: channel, Source: source})
	if m.hasErr {
		return errors.New("journey tracker error")
	}
	return nil
}

func (m *recordingJourneyTracker) Calls(ctx context.Context) []journeyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]journeyCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func TestSendPipeline_JourneyTrackerRecorded(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	tracker := &recordingJourneyTracker{}
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.JourneyTracker = tracker
	pipeline := NewSendPipeline(cfg)

	req := &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		CustomerID:  "cust-123",
		Content:     "Hello",
	}
	resp := pipeline.Send(context.Background(), req)
	if !resp.Success {
		t.Fatalf("期望成功，失败: %s", resp.Error)
	}
	calls := tracker.Calls(context.Background())
	if len(calls) == 0 {
		t.Fatal("期望客户轨迹至少记录 1 次")
	}
	c := calls[0]
	if c.CustomerID != "cust-123" {
		t.Errorf("期望 customer_id=cust-123，实际 %s", c.CustomerID)
	}
	if c.Channel != "sms" {
		t.Errorf("期望 channel=sms，实际 %s", c.Channel)
	}
	if c.Source != "reach_pipeline" {
		t.Errorf("期望 source=reach_pipeline，实际 %s", c.Source)
	}
	jStep := findStepResult(resp, SendStepJourney)
	if jStep == nil {
		t.Fatal("期望有 journey 步骤")
	}
	if !jStep.Success {
		t.Errorf("期望 journey 步骤成功: %s", jStep.Error)
	}
}

func TestCountedSendPipeline_Stats(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	inner := newTestPipeline(adapter)
	pipeline := NewCountedSendPipeline(inner)
	counted, ok := pipeline.(*countedSendPipeline)
	if !ok {
		t.Fatal("期望 *countedSendPipeline 类型")
	}

	for i := 0; i < 3; i++ {
		req := &ReachSendRequest{
			Channel:     "sms",
			RecipientID: fmt.Sprintf("1380013800%d", i),
			Content:     "Hello",
		}
		resp := pipeline.Send(context.Background(), req)
		if !resp.Success {
			t.Errorf("第 %d 次发送应该成功: %s", i, resp.Error)
		}
	}

	stats := counted.Stats(context.Background())
	if stats.TotalSends != 3 {
		t.Errorf("期望 total_sends=3，实际 %d", stats.TotalSends)
	}
	if stats.SuccessSends != 3 {
		t.Errorf("期望 success_sends=3，实际 %d", stats.SuccessSends)
	}
	if stats.FailedSends != 0 {
		t.Errorf("期望 failed_sends=0，实际 %d", stats.FailedSends)
	}
}

func TestCountedSendPipeline_StatsWithFailures(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.RateLimiter = NewMemorySendRateLimiter()
	cfg.RateLimitSpec = RateLimitSpec{QPS: 1, Burst: 1}
	pipeline := NewCountedSendPipeline(NewSendPipeline(cfg))
	counted := pipeline.(*countedSendPipeline)

	req := func() *ReachSendRequest {
		return &ReachSendRequest{
			Channel:     "sms",
			AccountID:   "acc-stats",
			RecipientID: "13800138000",
			CustomerID:  "cust-stats",
			Content:     "Hello",
		}
	}
	if !pipeline.Send(context.Background(), req()).Success {
		t.Fatal("第 1 次发送应成功")
	}
	resp2 := pipeline.Send(context.Background(), req())
	if resp2.Success {
		t.Fatal("第 2 次发送应被限流")
	}

	stats := counted.Stats(context.Background())
	if stats.TotalSends != 2 {
		t.Errorf("期望 total_sends=2，实际 %d", stats.TotalSends)
	}
	if stats.SuccessSends != 1 {
		t.Errorf("期望 success_sends=1，实际 %d", stats.SuccessSends)
	}
	if stats.FailedSends != 1 {
		t.Errorf("期望 failed_sends=1，实际 %d", stats.FailedSends)
	}
	if stats.RateLimited != 1 {
		t.Errorf("期望 rate_limited=1，实际 %d", stats.RateLimited)
	}
}

func TestMemorySendRateLimiter_TokenBucket(t *testing.T) {
	ctx := context.Background()
	limiter := NewMemorySendRateLimiter()
	spec := RateLimitSpec{QPS: 2, Burst: 2}

	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("第 1 次应该允许（burst）")
	}
	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("第 2 次应该允许（burst）")
	}
	if limiter.Allow(ctx, "k1", spec) {
		t.Error("第 3 次应该被限流")
	}
	time.Sleep(550 * time.Millisecond)
	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("等待 500ms 后应该有 1 个 token 可用")
	}
}

func TestMemorySendRateLimiter_DifferentKeys(t *testing.T) {
	ctx := context.Background()
	limiter := NewMemorySendRateLimiter()
	spec := RateLimitSpec{QPS: 1, Burst: 1}

	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("k1 第 1 次应该允许")
	}
	if !limiter.Allow(ctx, "k2", spec) {
		t.Error("k2 第 1 次应该允许")
	}
	if limiter.Allow(ctx, "k1", spec) {
		t.Error("k1 第 2 次应该被限流")
	}
}

func TestMemorySendRateLimiter_Reset(t *testing.T) {
	ctx := context.Background()
	limiter := NewMemorySendRateLimiter()
	spec := RateLimitSpec{QPS: 1, Burst: 1}

	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("k1 第 1 次应该允许")
	}
	if limiter.Allow(ctx, "k1", spec) {
		t.Error("k1 第 2 次应该被限流")
	}
	limiter.Reset(context.Background(), "k1")
	if !limiter.Allow(ctx, "k1", spec) {
		t.Error("Reset 后 k1 应该允许")
	}
}

func TestMemorySendAuditLogger_MaxSize(t *testing.T) {
	logger := NewMemorySendAuditLogger(3)
	for i := 0; i < 5; i++ {
		logger.LogSend(context.Background(), &ReachSendRequest{
			Channel:     "sms",
			RecipientID: fmt.Sprintf("r%d", i),
			Content:     "x",
		}, &SendResponse{Success: true, MessageID: fmt.Sprintf("m%d", i)})
	}
	entries := logger.Entries(context.Background())
	if len(entries) != 3 {
		t.Errorf("期望保留 3 条（容量上限），实际 %d", len(entries))
	}
	if entries[0].Recipient != "r2" {
		t.Errorf("期望第一条 recipient=r2，实际 %s", entries[0].Recipient)
	}
	if entries[2].Recipient != "r4" {
		t.Errorf("期望最后一条 recipient=r4，实际 %s", entries[2].Recipient)
	}
}

func TestNoOpSendCostTracker_ZeroCost(t *testing.T) {
	tracker := NoOpSendCostTracker{}
	cost, err := tracker.Charge(context.Background(), "sms", &ReachSendRequest{})
	if err != nil {
		t.Errorf("NoOp CostTracker 不应报错: %v", err)
	}
	if cost != 0 {
		t.Errorf("NoOp CostTracker 应返回 0，实际 %f", cost)
	}
}

func TestNoOpSendRateLimiter_AlwaysAllow(t *testing.T) {
	limiter := NoOpSendRateLimiter{}
	for i := 0; i < 100; i++ {
		if !limiter.Allow(context.Background(), "k", RateLimitSpec{QPS: 1, Burst: 1}) {
			t.Errorf("NoOp RateLimiter 第 %d 次应该允许", i)
		}
	}
}

func TestNoOpSendJourneyTracker_NoError(t *testing.T) {
	tracker := NoOpSendJourneyTracker{}
	if err := tracker.RecordTouch(context.Background(), "c1", "sms", "test"); err != nil {
		t.Errorf("NoOp JourneyTracker 不应报错: %v", err)
	}
}

func TestAllowAllSendPermission_AlwaysAllow(t *testing.T) {
	p := AllowAllSendPermission{}
	if err := p.CheckSendPermission(context.Background(), &ReachSendRequest{}); err != nil {
		t.Errorf("AllowAllSendPermission 不应报错: %v", err)
	}
}

func TestDefaultSendRetryPolicy_Values(t *testing.T) {
	p := DefaultSendRetryPolicy()
	if p.MaxRetries != 3 {
		t.Errorf("期望 MaxRetries=3，实际 %d", p.MaxRetries)
	}
	if p.Backoff != "exponential" {
		t.Errorf("期望 Backoff=exponential，实际 %s", p.Backoff)
	}
	if p.IntervalMs <= 0 {
		t.Errorf("期望 IntervalMs>0，实际 %d", p.IntervalMs)
	}
	if p.MaxIntervalMs <= 0 {
		t.Errorf("期望 MaxIntervalMs>0，实际 %d", p.MaxIntervalMs)
	}
}

func TestComputeBackoff_Exponential(t *testing.T) {
	p := &defaultSendPipeline{}
	policy := SendRetryPolicy{
		MaxRetries:    3,
		IntervalMs:    100,
		Backoff:       "exponential",
		MaxIntervalMs: 10000,
	}
	cases := []struct {
		attempt int
		expect  time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
	}
	for _, c := range cases {
		got := p.computeBackoff(context.Background(), policy, c.attempt)
		if got != c.expect {
			t.Errorf("attempt %d: 期望 %v，实际 %v", c.attempt, c.expect, got)
		}
	}
}

func TestComputeBackoff_MaxIntervalCap(t *testing.T) {
	p := &defaultSendPipeline{}
	policy := SendRetryPolicy{
		MaxRetries:    3,
		IntervalMs:    1000,
		Backoff:       "exponential",
		MaxIntervalMs: 5000,
	}
	got := p.computeBackoff(context.Background(), policy, 5)
	if got != 5000*time.Millisecond {
		t.Errorf("期望被 cap 到 5000ms，实际 %v", got)
	}
}

func TestComputeBackoff_Fixed(t *testing.T) {
	p := &defaultSendPipeline{}
	policy := SendRetryPolicy{
		MaxRetries: 3,
		IntervalMs: 200,
		Backoff:    "fixed",
	}
	for attempt := 0; attempt < 5; attempt++ {
		got := p.computeBackoff(context.Background(), policy, attempt)
		if got != 200*time.Millisecond {
			t.Errorf("attempt %d: 期望固定 200ms，实际 %v", attempt, got)
		}
	}
}

func TestIsRetryable_DefaultAllRetryable(t *testing.T) {
	p := &defaultSendPipeline{}
	if !p.isRetryable(context.Background(), errors.New("any error"), nil) {
		t.Error("默认所有错误应该可重试")
	}
	if !p.isRetryable(context.Background(), errors.New("network error"), []string{}) {
		t.Error("空 RetryableErrors 应该所有错误可重试")
	}
}

func TestIsRetryable_SpecificErrors(t *testing.T) {
	p := &defaultSendPipeline{}
	retryable := []string{"timeout", "connection reset"}
	if !p.isRetryable(context.Background(), errors.New("request timeout"), retryable) {
		t.Error("'timeout' 应该匹配")
	}
	if !p.isRetryable(context.Background(), errors.New("connection reset by peer"), retryable) {
		t.Error("'connection reset' 应该匹配")
	}
	if p.isRetryable(context.Background(), errors.New("invalid argument"), retryable) {
		t.Error("'invalid argument' 不应该匹配")
	}
	if p.isRetryable(context.Background(), nil, retryable) {
		t.Error("nil 错误不应该可重试")
	}
}

func TestSendPipeline_NoAdapter_ReturnsError(t *testing.T) {
	cfg := DefaultSendPipelineConfig(nil)
	pipeline := NewSendPipeline(cfg)

	resp := pipeline.Send(context.Background(), &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	})
	if resp.Success {
		t.Fatal("期望失败（无 adapter）")
	}
	if !strings.Contains(resp.Error, ErrSendChannelNotConfig.Error()) {
		t.Errorf("期望错误包含 %q，实际 %q", ErrSendChannelNotConfig.Error(), resp.Error)
	}
}

func TestSendPipeline_CustomStepsOrder(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.Steps = []string{SendStepPermission, SendStepRetry}
	pipeline := NewSendPipeline(cfg)

	resp := pipeline.Send(context.Background(), &ReachSendRequest{
		Channel:     "sms",
		RecipientID: "13800138000",
		Content:     "Hello",
	})
	if !resp.Success {
		t.Fatalf("期望成功，失败: %s", resp.Error)
	}
	if len(resp.StepResults) != 2 {
		t.Errorf("期望 2 步，实际 %d", len(resp.StepResults))
	}
	if stepExists(resp, SendStepRateLimit) {
		t.Error("rate_limit 不应执行（未在自定义 Steps 中）")
	}
	if stepExists(resp, SendStepAudit) {
		t.Error("audit 不应执行（未在自定义 Steps 中）")
	}
}

func TestSendPipeline_ConcurrentSends(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	pipeline := newTestPipeline(adapter)

	const N = 50
	var wg sync.WaitGroup
	results := make([]*SendResponse, N)
	start := make(chan struct{})

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			req := &ReachSendRequest{
				Channel:     "sms",
				RecipientID: fmt.Sprintf("r%d", idx),
				Content:     "Hello",
			}
			results[idx] = pipeline.Send(context.Background(), req)
		}(i)
	}
	close(start)
	wg.Wait()

	successCnt := 0
	for i, r := range results {
		if r == nil {
			t.Errorf("第 %d 个响应为 nil", i)
			continue
		}
		if r.Success {
			successCnt++
		}
	}
	if successCnt != N {
		t.Errorf("期望全部 %d 个成功，实际 %d", N, successCnt)
	}
}

func TestMemorySendCostTracker_DifferentChannelCosts(t *testing.T) {
	tracker := NewMemorySendCostTracker(10.0)
	cases := []struct {
		channel   string
		expectErr bool
	}{
		{"sms", false},
		{"email", false},
		{"wecom", false},
		{"dingtalk", false},
		{"card", false},
	}
	for _, c := range cases {
		_, err := tracker.Charge(context.Background(), c.channel, &ReachSendRequest{})
		if c.expectErr && err == nil {
			t.Errorf("channel %s: 期望报错", c.channel)
		}
		if !c.expectErr && err != nil {
			t.Errorf("channel %s: 不期望报错: %v", c.channel, err)
		}
	}
	tracker.SetCost(context.Background(), "sms", 5.0)
	balanceBefore := tracker.Balance(context.Background())
	_, err := tracker.Charge(context.Background(), "sms", &ReachSendRequest{})
	if err != nil {
		t.Errorf("设置单价后扣费不应报错: %v", err)
	}
	balanceAfter := tracker.Balance(context.Background())
	if balanceAfter != balanceBefore-5.0 {
		t.Errorf("期望扣 5.0，实际从 %f 到 %f", balanceBefore, balanceAfter)
	}
}

func TestCustomerJourneySendTracker_NilService(t *testing.T) {
	tracker := CustomerJourneySendTracker{Service: nil}
	if err := tracker.RecordTouch(context.Background(), "c1", "sms", "test"); err != nil {
		t.Errorf("Service 为 nil 不应报错: %v", err)
	}
	if err := tracker.RecordTouch(context.Background(), "", "sms", "test"); err != nil {
		t.Errorf("空 customerID 不应报错: %v", err)
	}
}

func TestDefaultSendPipelineConfig_AllComponents(t *testing.T) {
	adapter := newSuccessAdapter("msg")
	cfg := DefaultSendPipelineConfig(adapter)
	if cfg.PermissionChecker == nil {
		t.Error("PermissionChecker 不应为 nil")
	}
	if cfg.RateLimiter == nil {
		t.Error("RateLimiter 不应为 nil")
	}
	if cfg.AuditLogger == nil {
		t.Error("AuditLogger 不应为 nil")
	}
	if cfg.CostTracker == nil {
		t.Error("CostTracker 不应为 nil")
	}
	if cfg.JourneyTracker == nil {
		t.Error("JourneyTracker 不应为 nil")
	}
	if cfg.Adapter == nil {
		t.Error("Adapter 不应为 nil")
	}
	if len(cfg.Steps) != 8 {
		t.Errorf("期望 8 步，实际 %d", len(cfg.Steps))
	}
	if cfg.RetryPolicy.MaxRetries != 3 {
		t.Errorf("期望 MaxRetries=3，实际 %d", cfg.RetryPolicy.MaxRetries)
	}
}

func TestSendPipeline_RetryStopsOnContextCancel(t *testing.T) {
	var sendCalls int
	adapter := NewFuncChannelAdapter(func(ctx context.Context, req *ReachSendRequest) (string, error) {
		sendCalls++
		return "", errors.New("always fail")
	})
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.RetryPolicy = SendRetryPolicy{MaxRetries: 5, IntervalMs: 50, Backoff: "exponential"}
	pipeline := NewSendPipeline(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp := pipeline.Send(ctx, &ReachSendRequest{
		Channel:     "sms",
		AccountID:   "acc-cancel",
		CustomerID:  "c1",
		RecipientID: "r1",
		Content:     "hi",
	})
	if resp.Success {
		t.Fatal("已取消上下文，期望发送失败")
	}
	if sendCalls != 1 {
		t.Errorf("ctx 取消后仍重发 %d 次，期望 1 次（首次失败后即刻停止）", sendCalls)
	}
}

func TestRateLimiterShard_EvictStalest(t *testing.T) {
	s := &rateLimiterShard{buckets: make(map[string]*sendRateBucket)}
	now := time.Now()
	s.buckets["a"] = &sendRateBucket{lastFill: now.Add(-3 * time.Minute)}
	s.buckets["b"] = &sendRateBucket{lastFill: now.Add(-1 * time.Minute)}
	s.buckets["c"] = &sendRateBucket{lastFill: now}
	s.evictStalestLocked()
	if _, ok := s.buckets["a"]; ok {
		t.Error("a (最久未用) 应被驱逐")
	}
	if len(s.buckets) != 2 {
		t.Errorf("期望剩余 2 个桶，实际 %d", len(s.buckets))
	}
}

func TestMemorySendRateLimiter_BoundedByCap(t *testing.T) {
	orig := rateLimiterMaxBuckets
	rateLimiterMaxBuckets = 4
	defer func() { rateLimiterMaxBuckets = orig }()

	limiter := NewMemorySendRateLimiter()
	spec := RateLimitSpec{QPS: 1, Burst: 1}
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		limiter.Allow(ctx, fmt.Sprintf("k-%d", i), spec)
	}
	if got := limiter.totalBucketCount(); got > rateLimiterShards*rateLimiterMaxBuckets {
		t.Errorf("桶总数 %d 超过上限 %d（内存泄漏）", got, rateLimiterShards*rateLimiterMaxBuckets)
	}
}

type fakeRedisCache struct {
	mu       sync.Mutex
	counters map[string]int64
	fail     bool
}

func newFakeRedisCache() *fakeRedisCache {
	return &fakeRedisCache{counters: map[string]int64{}}
}

func (f *fakeRedisCache) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.fail {
		return 0, errors.New("redis down")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[key]++
	return f.counters[key], nil
}

func (f *fakeRedisCache) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.counters, key)
	return nil
}

func (f *fakeRedisCache) Get(_ context.Context, _ string) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeRedisCache) Set(_ context.Context, _ string, _ any, _ time.Duration) error { return nil }
func (f *fakeRedisCache) SetNX(_ context.Context, _ string, _ any, _ time.Duration) (bool, error) {
	return false, nil
}
func (f *fakeRedisCache) ReleaseLock(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (f *fakeRedisCache) Exists(_ context.Context, _ string) (bool, error)         { return false, nil }
func (f *fakeRedisCache) GetJSON(_ context.Context, _ string, _ any) error {
	return errors.New("not implemented")
}
func (f *fakeRedisCache) SetJSON(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}
func (f *fakeRedisCache) LPush(_ context.Context, _ string, _ any, _ time.Duration) error { return nil }
func (f *fakeRedisCache) RPush(_ context.Context, _ string, _ any, _ time.Duration) error { return nil }
func (f *fakeRedisCache) LPop(_ context.Context, _ string) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeRedisCache) PopAll(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeRedisCache) LRange(_ context.Context, _ string, _, _ int64) ([]string, error) {
	return nil, nil
}
func (f *fakeRedisCache) LLen(_ context.Context, _ string) (int64, error) { return 0, nil }
func (f *fakeRedisCache) Clear(_ context.Context) error                   { return nil }

func cstTime(hour, minute int) time.Time {
	return time.Date(2026, 8, 20, hour, minute, 0, 0, cstZone)
}

func newQuietHoursPipeline(t *testing.T, now time.Time) (SendPipeline, *FuncChannelAdapter, *MemoryQuietHoursQueue) {
	t.Helper()
	q := NewMemoryQuietHoursQueue()
	adapter := NewFuncChannelAdapter(nil)
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.QuietHoursEnabled = true
	cfg.QuietHoursDeferrer = q
	cfg.QuietHoursClock = func() time.Time { return now }
	return NewSendPipeline(cfg), adapter, q
}

// TestQuietHours_Boundary_2159_Passes 窗口前一刻（21:59 CST）正常发送不延迟
func TestQuietHours_Boundary_2159_Passes(t *testing.T) {
	pipeline, adapter, q := newQuietHoursPipeline(t, cstTime(21, 59))
	resp := pipeline.Send(context.Background(), &ReachSendRequest{Channel: "wecom", CustomerID: "c1", RecipientID: "r1", Content: "hi"})
	if !resp.Success || resp.Deferred {
		t.Fatalf("21:59 应直接发送，got success=%v deferred=%v err=%q", resp.Success, resp.Deferred, resp.Error)
	}
	if adapter.Count(context.Background()) != 1 {
		t.Errorf("adapter 应被调用 1 次，got %d", adapter.Count(context.Background()))
	}
	if q.Len() != 0 {
		t.Errorf("延迟队列应为空，got %d", q.Len())
	}
}

// TestQuietHours_Boundary_2201_Deferred 窗口开始后（22:01 CST）入延迟队列且不实际发送
func TestQuietHours_Boundary_2201_Deferred(t *testing.T) {
	pipeline, adapter, q := newQuietHoursPipeline(t, cstTime(22, 1))
	resp := pipeline.Send(context.Background(), &ReachSendRequest{Channel: "wecom", CustomerID: "c1", RecipientID: "r1", Content: "hi"})
	if resp.Error == ErrSendQuietHoursDeferred.Error() && !resp.Deferred {
		t.Fatalf("22:01 应命中 quiet hours 延迟，got deferred=%v", resp.Deferred)
	}
	if !resp.Deferred {
		t.Fatalf("22:01 应命中 quiet hours 延迟，got success=%v err=%q", resp.Success, resp.Error)
	}
	if adapter.Count(context.Background()) != 0 {
		t.Errorf("延迟期间 adapter 不应被调用，got %d", adapter.Count(context.Background()))
	}
	if q.Len() != 1 {
		t.Fatalf("延迟队列应有 1 条，got %d", q.Len())
	}
	want := time.Date(2026, 8, 21, 8, 0, 0, 0, cstZone)
	if !resp.DeferredAt.Equal(want) {
		t.Errorf("首发时间应为次日 08:00 CST，got %s", resp.DeferredAt)
	}

	for _, s := range resp.StepResults {
		if s.Step == SendStepSend && s.Output != nil {
			t.Errorf("延迟后不应执行 send 步骤输出: %+v", s.Output)
		}
	}
}

// TestQuietHours_Boundary_0759_Deferred 窗口内末刻（07:59 CST）仍延迟
func TestQuietHours_Boundary_0759_Deferred(t *testing.T) {
	pipeline, adapter, _ := newQuietHoursPipeline(t, cstTime(7, 59))
	resp := pipeline.Send(context.Background(), &ReachSendRequest{Channel: "email", CustomerID: "c1", RecipientID: "a@b.c", Content: "hi"})
	if !resp.Deferred {
		t.Fatal("07:59 应命中 quiet hours 延迟")
	}
	if adapter.Count(context.Background()) != 0 {
		t.Errorf("延迟期间 adapter 不应被调用，got %d", adapter.Count(context.Background()))
	}
	want := time.Date(2026, 8, 20, 8, 0, 0, 0, cstZone)
	if !resp.DeferredAt.Equal(want) {
		t.Errorf("07:59 的首发时间应为当日 08:00 CST，got %s", resp.DeferredAt)
	}
}

// TestQuietHours_Boundary_0800_Passes 窗口结束（08:00 CST）恢复直发
func TestQuietHours_Boundary_0800_Passes(t *testing.T) {
	pipeline, adapter, _ := newQuietHoursPipeline(t, cstTime(8, 0))
	resp := pipeline.Send(context.Background(), &ReachSendRequest{Channel: "wecom", CustomerID: "c1", RecipientID: "r1", Content: "hi"})
	if !resp.Success || resp.Deferred {
		t.Fatalf("08:00 应直接发送，got success=%v deferred=%v", resp.Success, resp.Deferred)
	}
	if adapter.Count(context.Background()) != 1 {
		t.Errorf("adapter 应被调用 1 次，got %d", adapter.Count(context.Background()))
	}
}

// TestQuietHours_SMSExempt 短信渠道不受 pipeline quiet hours 影响（既有铁律保持：sms.go 拒绝式拦截）
func TestQuietHours_SMSExempt(t *testing.T) {
	pipeline, adapter, q := newQuietHoursPipeline(t, cstTime(23, 30))
	resp := pipeline.Send(context.Background(), &ReachSendRequest{Channel: "sms", CustomerID: "c1", RecipientID: "13800138000", Content: "hi"})
	if resp.Deferred {
		t.Fatal("短信不应进入 pipeline quiet hours 延迟队列（铁律保持）")
	}
	if !resp.Success || adapter.Count(context.Background()) != 1 {
		t.Fatalf("短信在 pipeline 层应正常放行（夜间拒绝由 sms.go 负责），success=%v calls=%d", resp.Success, adapter.Count(context.Background()))
	}
	if q.Len() != 0 {
		t.Errorf("延迟队列应为空，got %d", q.Len())
	}
}

// TestCountedPipeline_QuietHoursDeferred 统计包装器对 QuietHoursDeferred 计数
func TestCountedPipeline_QuietHoursDeferred(t *testing.T) {
	q := NewMemoryQuietHoursQueue()
	inner := NewSendPipeline(SendPipelineConfig{
		PermissionChecker:  AllowAllSendPermission{},
		RateLimiter:        NoOpSendRateLimiter{},
		AuditLogger:        NewMemorySendAuditLogger(10),
		CostTracker:        NoOpSendCostTracker{},
		JourneyTracker:     NoOpSendJourneyTracker{},
		Adapter:            NewFuncChannelAdapter(nil),
		QuietHoursEnabled:  true,
		QuietHoursDeferrer: q,
		QuietHoursClock:    func() time.Time { return cstTime(23, 0) },
	})
	counted := NewCountedSendPipeline(inner)
	counted.Send(context.Background(), &ReachSendRequest{Channel: "wecom", CustomerID: "c1", RecipientID: "r1"})
	cp := counted.(interface {
		Stats(ctx context.Context) SendPipelineStats
	})
	if got := cp.Stats(context.Background()).QuietHoursDeferred; got != 1 {
		t.Errorf("Expected QuietHoursDeferred=1, got %d", got)
	}
}

// TestTransactionalBypass_PerUserLimit 交易类消息绕过 PerUser 频控
func TestTransactionalBypass_PerUserLimit(t *testing.T) {
	svc := &ReachPipelineService{}
	svc.SetRateCache(newFakeRedisCache())
	rl := &RateLimitConfig{PerUserLimit: 2, CooldownSecs: 60}

	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-tx", rl, false) {
		t.Fatal("第 1 次普通触达应通过")
	}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-tx", rl, false) {
		t.Fatal("第 2 次普通触达应通过")
	}
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-tx", rl, false) {
		t.Fatal("第 3 次普通触达应被 PerUser 频控拦截")
	}

	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-tx", rl, true) {
		t.Fatal("transactional 消息应绕过 PerUser 频控")
	}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-tx", rl, true) {
		t.Fatal("transactional 消息应绕过 PerUser 频控（多次）")
	}
}

// TestIsTransactionalPayload transactional 标记解析
func TestIsTransactionalPayload(t *testing.T) {
	cases := []struct {
		val  any
		want bool
	}{
		{true, true},
		{"true", true},
		{"1", true},
		{float64(1), true},
		{false, false},
		{"false", false},
		{nil, false},
		{123, false},
	}
	for i, c := range cases {
		job := &model.ReachJob{Payload: model.JSONMap{"transactional": c.val}}
		if c.val == nil {
			job.Payload = model.JSONMap{}
		}
		if got := isTransactionalPayload(job); got != c.want {
			t.Errorf("case %d: isTransactionalPayload(%v) = %v, want %v", i, c.val, got, c.want)
		}
	}
	if isTransactionalPayload(&model.ReachJob{}) {
		t.Error("空 payload 应为非 transactional")
	}
}

// TestPerUser_DegradedToMemoryOnRedisFailure Redis 不可用时降级进程内滑动窗口且仍然生效
func TestPerUser_DegradedToMemoryOnRedisFailure(t *testing.T) {
	svc := &ReachPipelineService{}
	svc.SetRateCache(&fakeRedisCache{fail: true})
	rl := &RateLimitConfig{PerUserLimit: 2, CooldownSecs: 60}

	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-deg", rl, false) {
		t.Fatal("降级后第 1 次应通过")
	}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-deg", rl, false) {
		t.Fatal("降级后第 2 次应通过")
	}
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-deg", rl, false) {
		t.Fatal("降级后第 3 次应被进程内计数拦截")
	}
}

// TestDailyQuota_CrossInstanceShared R-6：多实例共享同一 Redis 后端时配额合并计算
func TestDailyQuota_CrossInstanceShared(t *testing.T) {
	shared := newFakeRedisCache()
	instA := &ReachPipelineService{}
	instA.SetRateCache(shared)
	instB := &ReachPipelineService{}
	instB.SetRateCache(shared)
	rl := &RateLimitConfig{DailyQuota: 3}

	if !instA.checkRateLimit(context.Background(), "sms", "acc", "u1", rl, false) {
		t.Fatal("实例 A 第 1 次应通过")
	}
	if !instA.checkRateLimit(context.Background(), "sms", "acc", "u1", rl, false) {
		t.Fatal("实例 A 第 2 次应通过")
	}
	if !instB.checkRateLimit(context.Background(), "sms", "acc", "u2", rl, false) {
		t.Fatal("实例 B 第 1 次应通过（跨实例共享配额 3/3）")
	}
	if instB.checkRateLimit(context.Background(), "sms", "acc", "u2", rl, false) {
		t.Fatal("实例 B 第 2 次应被拒（跨实例配额已耗尽 3/3）")
	}
}

// TestDailyQuota_DegradedToMemoryOnRedisFailure R-6：Redis 故障降级进程内配额且仍然生效
func TestDailyQuota_DegradedToMemoryOnRedisFailure(t *testing.T) {
	svc := &ReachPipelineService{}
	svc.SetRateCache(&fakeRedisCache{fail: true})

	if !svc.checkDailyQuota(context.Background(), "wecom", 2) {
		t.Fatal("降级后第 1 次应通过")
	}
	if !svc.checkDailyQuota(context.Background(), "wecom", 2) {
		t.Fatal("降级后第 2 次应通过")
	}
	if svc.checkDailyQuota(context.Background(), "wecom", 2) {
		t.Fatal("降级后第 3 次应被进程内配额拦截")
	}
}

// TestNextCSTMidnight R-6：TTL 至当日 CST 24:00
func TestNextCSTMidnight(t *testing.T) {
	now := cstTime(23, 30)
	d := nextCSTMidnight(now)
	if d != 30*time.Minute {
		t.Errorf("23:30 距零点应为 30m，got %s", d)
	}
	now = cstTime(0, 0)
	d = nextCSTMidnight(now)
	if d != 24*time.Hour {
		t.Errorf("00:00 距次日零点应为 24h，got %s", d)
	}
}

// TestResolvePerUserLimit_Default R-5：未配置时默认 3
func TestResolvePerUserLimit_Default(t *testing.T) {
	svc := &ReachPipelineService{}
	if got := svc.resolvePerUserLimit(context.Background(), 0); got != defaultPerUserLimit {
		t.Errorf("默认 PerUser 上限应为 %d，got %d", defaultPerUserLimit, got)
	}
	if got := svc.resolvePerUserLimit(context.Background(), 7); got != 7 {
		t.Errorf("pipeline 显式配置优先，want 7 got %d", got)
	}
}

// TestComplianceAuditLogger_FlushWritesDB 缓冲记录经 Flush 批量写入 reach_compliance_log
func TestComplianceAuditLogger_FlushWritesDB(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &ReachComplianceLog{})
	l := &ComplianceAuditLogger{repo: repository.NewReachComplianceLogRepository(db)}
	l.record("sms", "13800000001")
	l.record("wecom", "user-b")
	if l.BufferedCount() != 2 {
		t.Fatalf("缓冲应为 2 条，got %d", l.BufferedCount())
	}
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	var rows []ReachComplianceLog
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应落库 2 条，got %d", len(rows))
	}
	if rows[0].Channel != "sms" || rows[0].RecipientID != "13800000001" {
		t.Errorf("首条内容不符: %+v", rows[0])
	}
	if l.BufferedCount() != 0 {
		t.Errorf("Flush 后缓冲应为空，got %d", l.BufferedCount())
	}
}

// TestComplianceAuditLogger_BufferFullTriggersSignal 缓冲满触发刷盘信号（不阻塞）
func TestComplianceAuditLogger_BufferFullTriggersSignal(t *testing.T) {
	l := &ComplianceAuditLogger{flushCh: make(chan struct{}, 1), stop: make(chan struct{})}
	for i := 0; i < complianceFlushBatchSize+1; i++ {
		l.record("email", fmt.Sprintf("user-%d", i))
	}
	select {
	case <-l.flushCh:
	default:
		t.Error("缓冲满后应触发刷盘信号")
	}
	if l.BufferedCount() != complianceFlushBatchSize+1 {
		t.Errorf("无 db 时 record 仅入缓冲，got %d", l.BufferedCount())
	}
}

// TestLogComplianceReminder_NilLoggerNoPanic 未初始化全局 logger 时 LogComplianceReminder 不 panic
func TestLogComplianceReminder_NilLoggerNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogComplianceReminder panic: %v", r)
		}
	}()
	LogComplianceReminder("test-channel", "test-recipient")
}

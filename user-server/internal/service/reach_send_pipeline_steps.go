package service

// reach_send_pipeline_steps.go 发送流水线编排与步骤实现：
// Send 主流程、permission（含 DNC 与静默时段守卫）、rate_limit、retry（指数退避）、
// fallback、audit、cost、journey、send 各步骤。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

func (p *defaultSendPipeline) Send(ctx context.Context, req *ReachSendRequest) *SendResponse {
	LogComplianceReminder(req.Channel, req.RecipientID)

	start := time.Now()
	resp := &SendResponse{
		PrimaryChannel: req.Channel,
		Channel:        req.Channel,
		AccountID:      req.AccountID,
		StepResults:    []SendStepLog{},
	}

	stepFuncs := map[string]func(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog{
		SendStepPermission: p.runPermission,
		SendStepRateLimit:  p.runRateLimit,
		SendStepRetry:      p.runRetry,
		SendStepFallback:   p.runFallback,
		SendStepAudit:      p.runAudit,
		SendStepCost:       p.runCost,
		SendStepJourney:    p.runJourney,
		SendStepSend:       p.runSend,
	}

	for _, step := range p.config.Steps {
		fn, ok := stepFuncs[step]
		if !ok {
			continue
		}
		log := fn(ctx, req, resp)
		resp.StepResults = append(resp.StepResults, log)

		if resp.Deferred {
			resp.Success = true
			resp.Error = ""
			resp.DurationMs = time.Since(start).Milliseconds()
			resp.SentAt = time.Now()
			return resp
		}
		if !log.Success && !log.Skipped {
			resp.Success = false
			resp.Error = log.Error
			resp.DurationMs = time.Since(start).Milliseconds()
			resp.SentAt = time.Now()
			if p.config.AuditLogger != nil {
				p.config.AuditLogger.LogSend(ctx, req, resp)
			}
			return resp
		}
	}

	resp.Success = true
	resp.DurationMs = time.Since(start).Milliseconds()
	resp.SentAt = time.Now()
	if p.config.AuditLogger != nil {
		p.config.AuditLogger.LogSend(ctx, req, resp)
	}
	return resp
}

func (p *defaultSendPipeline) runPermission(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepPermission, StartedAt: start}
	if p.config.PermissionChecker == nil {
		log.Skipped = true
		log.Success = true
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}
	if err := p.config.PermissionChecker.CheckSendPermission(ctx, req); err != nil {
		log.Success = false
		log.Error = err.Error()
	} else if p.config.DoNotContact != nil {

		oneID := req.CustomerID
		if v, ok := req.Metadata["one_id"]; ok && v != "" {
			oneID = v
		}
		if oneID != "" && p.config.DoNotContact.IsBlocked(ctx, oneID, req.Channel) {
			log.Success = false
			log.Error = ErrSendDoNotContact.Error()
			logger.Warnf("[DNC] pipeline 跳过发送 customer=%s channel=%s（命中全局退订标志位）", oneID, req.Channel)
		} else if p.checkQuietHours(ctx, req, resp) {
			log.Success = true
			log.Skipped = true
			log.Output = []any{map[string]any{
				"deferred": true,
				"send_at":  resp.DeferredAt.Format(time.RFC3339),
				"reason":   ErrSendQuietHoursDeferred.Error(),
			}}
		} else {
			log.Success = true
		}
	} else if p.checkQuietHours(ctx, req, resp) {
		log.Success = true
		log.Skipped = true
		log.Output = []any{map[string]any{
			"deferred": true,
			"send_at":  resp.DeferredAt.Format(time.RFC3339),
			"reason":   ErrSendQuietHoursDeferred.Error(),
		}}
	} else {
		log.Success = true
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) checkQuietHours(ctx context.Context, req *ReachSendRequest, resp *SendResponse) bool {
	if !p.config.QuietHoursEnabled || req.Channel == "sms" {
		return false
	}
	now := time.Now()
	if p.config.QuietHoursClock != nil {
		now = p.config.QuietHoursClock()
	}
	if !inQuietHoursWindow(now, quietHoursStartHour, quietHoursEndHour) {
		return false
	}
	deferrer := p.config.QuietHoursDeferrer
	if deferrer == nil {
		deferrer = GetGlobalQuietHoursQueue()
	}
	sendAt := nextQuietHoursRelease(now, nextDayFirstSendHour)
	if err := deferrer.Defer(ctx, req, sendAt); err != nil {
		logger.Errorf("[R-4] quiet hours 延迟入队失败，按拒绝处理 channel=%s recipient=%s: %v",
			req.Channel, req.RecipientID, err)
		resp.Deferred = false
		return false
	}
	resp.Deferred = true
	resp.DeferredAt = sendAt
	return true
}

func (p *defaultSendPipeline) runRateLimit(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepRateLimit, StartedAt: start}
	if p.config.RateLimiter == nil {
		log.Skipped = true
		log.Success = true
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}
	key := fmt.Sprintf("%s:%s:%s", req.Channel, req.AccountID, req.CustomerID)
	spec := p.config.RateLimitSpec
	if spec.QPS <= 0 && spec.Burst <= 0 {
		spec = DefaultThirdPartyRateLimitSpec(req.Channel)
	}
	if !p.config.RateLimiter.Allow(ctx, key, spec) {
		log.Success = false
		log.Error = ErrSendRateLimited.Error()
	} else {
		log.Success = true
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) runRetry(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepRetry, StartedAt: start}
	policy := p.config.RetryPolicy
	if policy.MaxRetries <= 0 {
		policy = DefaultSendRetryPolicy()
	}

	var lastErr error
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		msgID, err := p.executeSendWithFallback(ctx, req)
		if err == nil {
			resp.RetryCount = attempt
			resp.MessageID = msgID
			log.Success = true
			log.Output = []any{map[string]any{
				"attempt":    attempt,
				"message_id": msgID,
			}}
			log.EndedAt = time.Now()
			log.DurationMs = time.Since(start).Milliseconds()
			return log
		}
		lastErr = err
		if !p.isRetryable(ctx, err, policy.RetryableErrors) {
			break
		}
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			resp.RetryCount = attempt
			break
		}
		if attempt < policy.MaxRetries {
			wait := p.computeBackoff(ctx, policy, attempt)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				lastErr = ctx.Err()
			}
			if ctx.Err() != nil {
				resp.RetryCount = attempt
				break
			}
		}
	}
	resp.RetryCount = policy.MaxRetries
	log.Success = false
	log.Error = fmt.Sprintf("重试 %d 次后仍失败: %v", policy.MaxRetries, lastErr)
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) executeSendWithFallback(ctx context.Context, req *ReachSendRequest) (string, error) {
	if p.config.Adapter == nil {
		return "", ErrSendChannelNotConfig
	}
	msgID, err := p.config.Adapter.Send(ctx, req)
	if err == nil {
		return msgID, nil
	}
	if req.Fallback == nil || !req.Fallback.Enabled || req.Fallback.BackupChannel == "" {
		return "", err
	}
	if req.Fallback.MaxAttempts <= 0 {
		req.Fallback.MaxAttempts = 1
	}
	for i := 0; i < req.Fallback.MaxAttempts; i++ {
		backupAdapter, ok := p.config.FallbackAdapters[req.Fallback.BackupChannel]
		if !ok {
			continue
		}
		backupReq := *req
		backupReq.Channel = req.Fallback.BackupChannel
		backupReq.AccountID = req.Fallback.BackupAccount
		backupReq.Fallback = nil
		msgID, err2 := backupAdapter.Send(ctx, &backupReq)
		if err2 == nil {
			return msgID, nil
		}
		err = err2
	}
	return "", err
}

func (p *defaultSendPipeline) isRetryable(ctx context.Context, err error, retryableErrors []string) bool {
	if err == nil {
		return false
	}
	if len(retryableErrors) == 0 {
		return true
	}
	errStr := err.Error()
	for _, re := range retryableErrors {
		if strings.Contains(errStr, re) {
			return true
		}
	}
	return false
}

func (p *defaultSendPipeline) computeBackoff(ctx context.Context, policy SendRetryPolicy, attempt int) time.Duration {
	interval := policy.IntervalMs
	if policy.Backoff == "exponential" {
		mult := 1
		for i := 0; i < attempt; i++ {
			mult *= 2
		}
		interval = policy.IntervalMs * mult
		if policy.MaxIntervalMs > 0 && interval > policy.MaxIntervalMs {
			interval = policy.MaxIntervalMs
		}
	}
	return time.Duration(interval) * time.Millisecond
}

func (p *defaultSendPipeline) runFallback(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepFallback, StartedAt: start}
	if req.Fallback != nil && req.Fallback.Enabled && req.Fallback.BackupChannel != "" {
		log.Success = true
		log.Output = []any{map[string]any{
			"backup_channel": req.Fallback.BackupChannel,
			"max_attempts":   req.Fallback.MaxAttempts,
		}}
	} else {
		log.Skipped = true
		log.Success = true
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) runAudit(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepAudit, StartedAt: start}
	if p.config.AuditLogger == nil {
		log.Skipped = true
		log.Success = true
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}
	log.Success = true
	log.Output = []any{map[string]any{
		"note": "audit log will be recorded at pipeline finalize",
	}}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) runCost(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepCost, StartedAt: start}
	if p.config.CostTracker == nil {
		log.Skipped = true
		log.Success = true
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}
	cost, err := p.config.CostTracker.Charge(ctx, req.Channel, req)
	if err != nil {
		log.Success = false
		log.Error = err.Error()
	} else {
		log.Success = true
		log.Output = []any{map[string]any{"cost": cost}}
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) runJourney(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepJourney, StartedAt: start}
	if p.config.JourneyTracker == nil {
		log.Skipped = true
		log.Success = true
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}
	if err := p.config.JourneyTracker.RecordTouch(ctx, req.CustomerID, req.Channel, "reach_pipeline"); err != nil {
		log.Success = false
		log.Error = err.Error()
	} else {
		log.Success = true
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

func (p *defaultSendPipeline) runSend(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepSend, StartedAt: start}
	if resp.MessageID == "" {
		log.Success = false
		log.Error = "no message_id (send may have failed in retry step)"
	} else {
		log.Success = true
		log.Output = []any{map[string]any{
			"message_id": resp.MessageID,
			"channel":    resp.Channel,
		}}
	}
	log.EndedAt = time.Now()
	log.DurationMs = time.Since(start).Milliseconds()
	return log
}

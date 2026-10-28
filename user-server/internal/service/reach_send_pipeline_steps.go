package service

// reach_send_pipeline_steps.go 发送流水线编排与步骤实现：
// Send 主流程、permission（含 DNC 与静默时段守卫）、rate_limit、retry（指数退避）、
// fallback、audit、cost、journey、send 各步骤。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

func (p *defaultSendPipeline) Send(ctx context.Context, req *ReachSendRequest) *SendResponse {
	LogComplianceReminder(ctx, req.Channel, req.RecipientID)

	start := time.Now()
	resp := &SendResponse{
		PrimaryChannel: req.Channel,
		Channel:        req.Channel,
		AccountID:      req.AccountID,
		StepResults:    []SendStepLog{},
	}

	// D05：意图级配额预占。只有调用方显式给了 action_intent_id 才参与——
	// 没有幂等键就无法区分重试与新动作，预占账会失真。预占失败（超限/冲突）
	// 一律拒发并留原因；成功后按结局结算：成功 commit、确定失败 release、
	// 可重试/延后保持 reserved（重试复用同一预占）。
	intentID := ""
	quotaToken := ""
	if p.config.QuotaReserver != nil && p.config.QuotaScopesFn != nil && req != nil {
		if v, ok := req.Metadata["action_intent_id"]; ok && v != "" {
			intentID = v
			token, _, err := p.config.QuotaReserver.ReserveIntent(ctx, intentID, ReachPayloadHash(req), p.config.QuotaScopesFn(ctx, req))
			if err != nil {
				resp.Success = false
				resp.Error = fmt.Sprintf("%s: %v", ErrSendQuotaDenied.Error(), err)
				resp.DurationMs = time.Since(start).Milliseconds()
				resp.SentAt = time.Now()
				if p.config.AuditLogger != nil {
					p.config.AuditLogger.LogSend(ctx, req, resp)
				}
				return resp
			}
			quotaToken = token
			defer p.settleQuota(ctx, req, resp, intentID, quotaToken)
		}
	}

	stepFuncs := map[string]func(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog{
		SendStepPermission:       p.runPermission,
		SendStepCompliancePolicy: p.runCompliancePolicy,
		SendStepRateLimit:        p.runRateLimit,
		SendStepRetry:            p.runRetry,
		SendStepFallback:         p.runFallback,
		SendStepAudit:            p.runAudit,
		SendStepCost:             p.runCost,
		SendStepJourney:          p.runJourney,
		SendStepSend:             p.runSend,
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
	// E1：只有真正委派成功才计当日触达次数。
	// 放在这里而不是判定处：被策略拒绝的请求不能烧掉配额（否则连续几次拒发之后，
	// 该客户当天再也发不出去——拒发自己把自己锁死）。
	if p.config.CompliancePolicy != nil {
		p.config.CompliancePolicy.RecordSent(ctx, req)
	}
	if p.config.AuditLogger != nil {
		p.config.AuditLogger.LogSend(ctx, req, resp)
	}
	return resp
}

// settleQuota D05 结算：按发送结局处置预占额。
//   - Deferred（进延迟队列）：保持 reserved——队列重发带同一 intent，复用同一预占；
//   - Success：commit（幂等，重复回执不重复计数）；
//   - 失败：按渠道错误分类——可重试（network/429/5xx）保持 reserved 等重试复用；
//     不可重试（授权失效/参数错）= 确定未发送，release 返还额度。
//   - 分类不出来（unknown 类）→ MarkUnknown：额度保留待人工核对，不自动返还。
func (p *defaultSendPipeline) settleQuota(ctx context.Context, req *ReachSendRequest, resp *SendResponse, intentID, token string) {
	if intentID == "" || p.config.QuotaReserver == nil {
		return
	}
	var err error
	switch {
	case resp.Deferred || resp.Success:
		err = p.config.QuotaReserver.CommitIntent(ctx, intentID, token)
	default:
		ce := AsChannelError(errors.New(resp.Error))
		if ce != nil && ce.Retryable {
			return // 保持 reserved：重试复用同一预占
		}
		if ce == nil {
			err = p.config.QuotaReserver.MarkUnknownIntent(ctx, intentID, token)
		} else {
			err = p.config.QuotaReserver.ReleaseIntent(ctx, intentID, token)
		}
	}
	if err != nil {
		logger.Warnf("[QUOTA] 预占结算失败 intent=%s err=%v", intentID, err)
	}
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
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
		return log
	}

	// D04：主动外发的退订检查优先走故障显式通道——状态查不清就拒发
	//（fail-closed），留原因码；未配 strict 时退回旧 fail-open。
	if oneID := permissionOneID(req); oneID != "" && p.config.DoNotContact != nil {
		blocked := false
		if p.config.DoNotContactStrict != nil {
			b, err := p.config.DoNotContactStrict.IsBlockedStrict(ctx, oneID, req.Channel)
			if err != nil {
				log.Success = false
				log.Error = ErrSendDoNotContactUnavailable.Error()
				log.Output = []any{map[string]any{"reason": "dnc_unavailable_fail_closed"}}
				logger.Warnf("[DNC] pipeline 拒发 customer=%s channel=%s（退订状态查询失败，fail-closed）: %v", oneID, req.Channel, err)
				log.EndedAt = time.Now()
				log.DurationMs = time.Since(start).Milliseconds()
				return log
			}
			blocked = b
		} else {
			blocked = p.config.DoNotContact.IsBlocked(ctx, oneID, req.Channel)
		}
		if blocked {
			log.Success = false
			log.Error = ErrSendDoNotContact.Error()
			logger.Warnf("[DNC] pipeline 跳过发送 customer=%s channel=%s（命中全局退订标志位）", oneID, req.Channel)
			log.EndedAt = time.Now()
			log.DurationMs = time.Since(start).Milliseconds()
			return log
		}
	}

	// D04：同意闸（独立于 DNC 配置）。撤回即生效（含延迟队列重发前）；
	// paused 按延后处理；missing 按模式放行留痕或拒发。
	if oneID := permissionOneID(req); oneID != "" {
		if terminated := p.checkSendConsent(ctx, req, resp, &log, oneID); terminated {
			log.EndedAt = time.Now()
			log.DurationMs = time.Since(start).Milliseconds()
			return log
		}
	}

	if p.checkQuietHours(ctx, req, resp) {
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

// permissionOneID 发送前置检查用的客户归一键：metadata.one_id 优先，退回 CustomerID。
func permissionOneID(req *ReachSendRequest) string {
	if req == nil {
		return ""
	}
	if v, ok := req.Metadata["one_id"]; ok && v != "" {
		return v
	}
	return req.CustomerID
}

// checkSendConsent D04 同意闸。
// 返回 true 表示本次请求已被 consent 闸终结（拒绝/延后），调用方应立即返回该 step log。
func (p *defaultSendPipeline) checkSendConsent(ctx context.Context, req *ReachSendRequest, resp *SendResponse, log *SendStepLog, oneID string) bool {
	checker := p.config.ConsentChecker
	if checker == nil {
		return false
	}
	purpose := ReachSendPurpose(req)
	dec := checker.CheckSendConsent(ctx, oneID, purpose, req.Channel)
	switch dec.Status {
	case ConsentStatusRevoked:
		// 撤回即生效：metadata 自述 consent=true 也压不过真实撤回事实。
		log.Success = false
		log.Error = ErrSendConsentRevoked.Error()
		log.Output = []any{map[string]any{"reason": ConsentReasonRevoked, "purpose": purpose}}
		logger.Warnf("[CONSENT] pipeline 拒发 customer=%s channel=%s purpose=%s（已撤回同意，metadata 自述不构成授权）", oneID, req.Channel, purpose)
		return true
	case ConsentStatusPaused:
		// 暂停 = 持久延后：恢复时刻明确则延到该点，否则顺延到静默窗口放开。
		sendAt := time.Now().Add(time.Minute)
		if dec.PausedUntil != nil {
			sendAt = *dec.PausedUntil
		}
		if deferrer := p.config.QuietHoursDeferrer; deferrer != nil {
			if err := deferrer.Defer(ctx, req, sendAt); err == nil {
				resp.Deferred = true
				resp.DeferredAt = sendAt
				log.Success = true
				log.Skipped = true
				log.Output = []any{map[string]any{"deferred": true, "send_at": sendAt.Format(time.RFC3339), "reason": ConsentReasonPaused}}
				return true
			}
		}
		// 延迟队列不可用：按拒绝处理（与 deferSend 的入队失败口径一致）
		log.Success = false
		log.Error = "consent: preference paused and defer unavailable"
		log.Output = []any{map[string]any{"reason": ConsentReasonPaused}}
		return true
	case ConsentStatusMissing:
		if p.config.ConsentMode == ConsentModeEnforce {
			log.Success = false
			log.Error = ErrSendConsentRequired.Error()
			log.Output = []any{map[string]any{"reason": ConsentReasonMissing, "purpose": purpose}}
			logger.Warnf("[CONSENT] pipeline 拒发 customer=%s channel=%s purpose=%s（enforce 模式无可信同意凭据）", oneID, req.Channel, purpose)
			return true
		}
		// revocation_only：无凭据放行，但 metadata 自述只能标 legacy_assertion 留痕。
		log.Success = true
		note := map[string]any{"reason": ConsentReasonMissing, "purpose": purpose}
		if metadataOptIn(req.Metadata) {
			note["metadata_opt_in"] = ConsentReasonLegacyAssertion
		}
		log.Output = []any{note}
		return false
	default: // granted
		log.Success = true
		return false
	}
}

func (p *defaultSendPipeline) checkQuietHours(ctx context.Context, req *ReachSendRequest, resp *SendResponse) bool {
	if !p.config.QuietHoursEnabled || req.Channel == "sms" {
		return false
	}
	// D02：延迟队列 worker 的重发带豁免标记——这条消息已经等到窗口放开才重发，
	// 再判延会让它永远在队列里自旋。
	if quietHoursBypassed(ctx) {
		return false
	}
	now := time.Now()
	if p.config.QuietHoursClock != nil {
		now = p.config.QuietHoursClock()
	}
	if !inQuietHoursWindow(now, reachQuietHoursStart(ctx), reachQuietHoursEnd(ctx)) {
		return false
	}
	deferrer := p.config.QuietHoursDeferrer
	if deferrer == nil {
		deferrer = GetGlobalQuietHoursQueue()
	}
	sendAt := nextQuietHoursRelease(now, reachQuietHoursEnd(ctx))
	return p.deferSend(ctx, req, resp, deferrer, sendAt, "[R-4] quiet hours")
}

// deferSend 把一条消息放进延迟队列并标记 resp.Deferred。
//
// 抽成公共函数是因为现在有两个来源会延后同一条消息：全局静默时段（R-4）与
// 地区合规静默窗口（E1）。两处各写一遍入队逻辑，早晚会出现「一处失败按拒绝、
// 另一处失败按成功」的分歧——而这条分歧的后果是消息丢了却回执成功。
//
// 入队失败一律按拒绝处理：延迟队列不可用时把消息当"已延后"是静默丢消息。
func (p *defaultSendPipeline) deferSend(ctx context.Context, req *ReachSendRequest, resp *SendResponse, deferrer SendQuietHoursDeferrer, sendAt time.Time, cause string) bool {
	if deferrer == nil {
		logger.Errorf("%s 延迟入队器不可用，按拒绝处理 channel=%s recipient=%s", cause, req.Channel, req.RecipientID)
		resp.Deferred = false
		return false
	}
	if err := deferrer.Defer(ctx, req, sendAt); err != nil {
		logger.Errorf("%s 延迟入队失败，按拒绝处理 channel=%s recipient=%s: %v",
			cause, req.Channel, req.RecipientID, err)
		resp.Deferred = false
		return false
	}
	resp.Deferred = true
	resp.DeferredAt = sendAt
	return true
}

// runCompliancePolicy E1：地区×行业合规策略判定。
//
// 三种结局：
//   - 判定器未装配（nil）      ⇒ skipped，Success=true（默认路径，零行为变更）
//   - 策略判 allow            ⇒ Success=true，把 region/industry/policy_scope 打进 Output
//     （排障要能回答"这次按哪条规则放行的"，D2 可观测性的最小切片）
//   - 策略判 deny             ⇒ Success=false，Error 带原因码（终态，不重试）
//   - 策略判 defer（静默窗口）  ⇒ 复用 R-4 的延迟队列，resp.Deferred=true
func (p *defaultSendPipeline) runCompliancePolicy(ctx context.Context, req *ReachSendRequest, resp *SendResponse) SendStepLog {
	start := time.Now()
	log := SendStepLog{Step: SendStepCompliancePolicy, StartedAt: start}
	defer func() {
		log.EndedAt = time.Now()
		log.DurationMs = time.Since(start).Milliseconds()
	}()

	if p.config.CompliancePolicy == nil {
		log.Skipped = true
		log.Success = true
		return log
	}

	// D02：延迟队列 worker 的重发带豁免标记，合规静默窗口同样不再判延（防自旋）。
	if quietHoursBypassed(ctx) {
		log.Skipped = true
		log.Success = true
		return log
	}

	decision := p.config.CompliancePolicy.Evaluate(ctx, req)
	recordComplianceDecision(ctx, req, decision)

	if decision.Deferred {
		deferrer := p.config.QuietHoursDeferrer
		if deferrer == nil {
			deferrer = GetGlobalQuietHoursQueue()
		}
		if p.deferSend(ctx, req, resp, deferrer, decision.SendAt, "[E1] compliance quiet hours") {
			log.Success = true
			log.Skipped = true
			log.Output = []any{map[string]any{
				"deferred":     true,
				"send_at":      decision.SendAt.Format(time.RFC3339),
				"reason":       decision.Reason,
				"region":       decision.Region,
				"industry":     decision.Industry,
				"policy_scope": decision.PolicyScope,
			}}
			return log
		}
		log.Success = false
		log.Error = fmt.Sprintf("%s: %s (defer failed)", ErrSendComplianceDenied.Error(), decision.Reason)
		return log
	}

	if !decision.Allowed {
		log.Success = false
		log.Error = fmt.Sprintf("%s: %s", ErrSendComplianceDenied.Error(), decision.Reason)
		log.Output = []any{map[string]any{
			"reason":       decision.Reason,
			"region":       decision.Region,
			"industry":     decision.Industry,
			"policy_scope": decision.PolicyScope,
		}}
		logger.Warnf("[E1] 合规策略拒绝外发 channel=%s region=%s industry=%s reason=%s scope=%s",
			req.Channel, decision.Region, decision.Industry, decision.Reason, decision.PolicyScope)
		return log
	}

	log.Success = true
	log.Output = []any{map[string]any{
		"reason":       decision.Reason,
		"region":       decision.Region,
		"industry":     decision.Industry,
		"policy_scope": decision.PolicyScope,
	}}
	return log
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

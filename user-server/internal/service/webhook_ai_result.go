package service

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
)

// webhookReplyRequest 固定一轮客户请求的投递目标；重生成不能改写账号、会话或入站身份。
type webhookReplyRequest struct {
	channel   WebhookChannel
	accountID string
	payload   *ParsedPayload
	hub       *model.MessageHub
	incoming  *IncomingContext
	agent     *AgentContext
}

type webhookResultGenerator func(context.Context, *IncomingContext, *AgentContext) (*HandleResult, error)

// deliverAIResult 首次与一次有界重生成共用安全出口；生成结果不是交付成功。
func (s *WebhookService) deliverAIResult(
	ctx context.Context,
	req webhookReplyRequest,
	result *HandleResult,
	generate webhookResultGenerator,
) {
	for attempt := 0; attempt < 2; attempt++ {
		ctx = tracing.WithCarrier(ctx, tracing.CarrierFromContext(ctx).WithAnswerAttempt(attempt+1))
		decisionStarted := time.Now()
		// 只记录选择及投递交接，不存候选正文，也不把决策当平台已收到。
		recordDecision := func(outcome, status string, accepted bool, decisionErr error) {
			completed := time.Now()
			tracing.RecordNode(ctx, tracing.NodeSpan{Node: tracing.NodeAIResult, Status: status,
				StartedAt: &decisionStarted, CompletedAt: &completed, DurationMeasured: true,
				DurationMs: completed.Sub(decisionStarted).Milliseconds(), Error: tracing.ErrStr(decisionErr),
				Output:   map[string]any{"outcome": outcome, "send_accepted": accepted, "enqueued": accepted && isBridgeChannel(string(req.channel)), "platform_delivery_confirmed": false},
				Expected: "每次生成都明确安全出口；决策与平台交付分离"})
		}
		if result == nil {
			recordDecision("nil_result", tracing.StatusAbnormal, false, nil)
			logger.Ctx(ctx).Error().Int("answer_attempt", attempt+1).
				Str("event_id", req.payload.EventID).Msg("AI result exit: nil decision; no outbound")
			return
		}
		outCtx := HandleResultToContext(ctx, result)
		if req.agent != nil && req.agent.AgentCode != "" {
			outCtx = AgentIDToContext(outCtx, req.agent.AgentCode)
		}
		// 公告不等于 AI 答案；已在人工手里的会话没有新 notice 时不得重复播报。
		if result.Transferred || result.HandlerType == model.HandlerTypeHuman {
			logger.Ctx(ctx).Info().Int("answer_attempt", attempt+1).
				Str("session_id", result.SessionID).Str("reason", result.TransferReason).
				Bool("has_transfer_notice", strings.TrimSpace(result.TransferNotice) != "").
				Msg("transferred to human")
			if result.Transferred && strings.TrimSpace(result.TransferNotice) != "" {
				accepted, noticeErr := s.sendOutbound(outCtx, req.channel, req.accountID, req.payload, result.TransferNotice, req.hub, nil)
				status := tracing.StatusOk
				if noticeErr != nil || !accepted {
					status = tracing.StatusAbnormal
				}
				recordDecision("handoff_notice", status, accepted, noticeErr)
			} else {
				recordDecision("handoff", tracing.StatusSkipped, false, nil)
			}
			return
		}
		if strings.EqualFold(strings.TrimSpace(result.Reply), "NO_REPLY") {
			recordDecision("no_reply", tracing.StatusSkipped, false, nil)
			logger.Ctx(ctx).Info().Int("answer_attempt", attempt+1).
				Str("channel", string(req.channel)).Str("conv_id", req.hub.ConversationID).
				Msg("[Webhook] LLM 判定本轮无需回复（NO_REPLY）——不发送不落库")
			return
		}
		if !result.AIReplied || (strings.TrimSpace(result.Reply) == "" && len(result.Cards) == 0) {
			recordDecision("no_answer", tracing.StatusSkipped, false, nil)
			logger.Ctx(ctx).Info().Int("answer_attempt", attempt+1).
				Str("session_id", result.SessionID).Str("handler", string(result.HandlerType)).Msg("no outbound")
			return
		}
		// 两次生成都可能与坐席接管并发；出站只读锁、不释放或改写人工状态。
		locked, lockErr := s.ingressSvc.ConfirmHumanLockBeforeSend(ctx, result.SessionID)
		if locked || lockErr != nil {
			outcome, status := "human_locked", tracing.StatusSkipped
			if lockErr != nil {
				outcome, status = "lock_unconfirmed", tracing.StatusAbnormal
			}
			recordDecision(outcome, status, false, lockErr)
			logger.Ctx(ctx).Warn().Err(lockErr).Int("answer_attempt", attempt+1).
				Str("session_id", result.SessionID).Str("event_id", req.payload.EventID).
				Msg("发送前复核：会话已被人工接管或锁不可确认，AI 回复静默放弃（防抢答）")
			return
		}
		sent, sendErr := s.sendOutbound(outCtx, req.channel, req.accountID, req.payload, result.Reply, req.hub, result.Cards)
		isDuplicate := !sent && sendErr != nil && strings.Contains(sendErr.Error(), "duplicate reply")
		outcome, status := "answer_accepted", tracing.StatusOk
		if !sent || sendErr != nil {
			outcome, status = "send_failed", tracing.StatusAbnormal
		}
		if isDuplicate {
			outcome = "duplicate_reply"
		}
		recordDecision(outcome, status, sent, sendErr)
		if !isDuplicate || attempt > 0 || generate == nil || req.incoming == nil {
			return
		}
		logger.Ctx(ctx).Info().Str("channel", string(req.channel)).Str("conv_id", req.hub.ConversationID).
			Msg("[Webhook] FAQ 答案重复——禁用 fastpath 重生成一次，仍走同一安全出口")
		in := *req.incoming
		in.NoFastpath = true
		var generateErr error
		rerunCtx := tracing.WithCarrier(ctx, tracing.CarrierFromContext(ctx).WithAnswerAttempt(attempt+2))
		rerunSpan := tracing.Start(rerunCtx, tracing.NodeAIDispatch).
			Input(map[string]any{"regenerated": true, "event_id": req.payload.EventID}).
			Expected("一次重生成后仍走相同安全结果出口")
		result, generateErr = generate(rerunCtx, &in, req.agent)
		output := map[string]any{"generated": result != nil, "ai_failed": generateErr != nil}
		if result != nil {
			output["ai_replied"], output["transferred"] = result.AIReplied, result.Transferred
			output["reply_len"], output["handler_type"] = len(result.Reply), result.HandlerType
			output["no_reply"] = strings.EqualFold(strings.TrimSpace(result.Reply), "NO_REPLY")
		}
		rerunSpan.End(output, generateErr)
		if generateErr != nil {
			failedAt := time.Now()
			tracing.RecordNode(rerunCtx, tracing.NodeSpan{Node: tracing.NodeAIResult, Status: tracing.StatusAbnormal,
				StartedAt: &failedAt, CompletedAt: &failedAt, DurationMeasured: true, Error: tracing.ErrStr(generateErr),
				Output: map[string]any{"outcome": "generation_failed", "enqueued": false, "platform_delivery_confirmed": false}})
			logger.Ctx(ctx).Error().Err(generateErr).Int("answer_attempt", attempt+2).
				Str("event_id", req.payload.EventID).Msg("AI result exit: regeneration failed; duplicate audit retained")
			return
		}
	}
}

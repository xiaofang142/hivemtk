package service

import (
	"context"

	"fmt"

	"strconv"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils/logger"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/trace"
	"hivemtk-user/internal/pkg/tracing"
)

func (s *WebhookService) SetSalesEngine(ctx context.Context, e *SalesEngine) {
	s.salesEngine = e
}

func (s *WebhookService) SetSmartOrchestrator(ctx context.Context, o *SmartCSOrchestrator) {
	s.smartOrchestrator = o
}

func (s *WebhookService) retryWithBackoff(ctx context.Context, job *webhookJob, payload *ParsedPayload, origErr error) {

	delays := retryDelaysFor(AsChannelError(origErr))
	if delays == nil {
		logger.Ctx(ctx).Error().Str("event", job.event.EventID).Msg("[Webhook] non-retryable channel error, giving up immediately")
		return
	}
	for i := 0; i < WebhookMaxRetries; i++ {

		if i >= len(delays) {
			i = len(delays) - 1
		}
		time.Sleep(delays[i])
		um := s.ToUnifiedMessage(ctx, job.channel, job.account, payload)
		if err := s.dispatchToUnified(ctx, um); err == nil {
			s.markProcessed(ctx, job.event)
			return
		}
	}
	logger.Errorf("[Webhook] 多次重试失败 event=%s err=%v", job.event.EventID, origErr)
}

func (s *WebhookService) markProcessed(ctx context.Context, evt *model.WebhookEvent) {
	now := time.Now()
	evt.Processed = true
	evt.ProcessedAt = &now
	if s.eventRepo != nil && s.db != nil {
		_ = s.eventRepo.Update(ctx, evt)
	}
}

func (s *WebhookService) shouldTriggerAI(ctx context.Context, channel WebhookChannel, accountID string) bool {
	if s.salesEngine == nil {
		return false
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		return false
	}
	switch channel {
	case ChannelWeCom:
		if s.wecomRepo == nil {
			return false
		}
		acc, err := s.wecomRepo.GetByID(ctx, uint(accID))
		if err != nil {
			return false
		}
		return acc.AIAgentEnabled
	case ChannelFeishu:
		if s.feishuRepo == nil {
			return false
		}
		acc, err := s.feishuRepo.GetByID(ctx, uint(accID))
		if err != nil {
			return false
		}
		return acc.AIAgentEnabled
	case ChannelTelegram:
		if s.telegramRepo == nil {
			return false
		}
		acc, err := s.telegramRepo.GetByID(ctx, uint(accID))
		if err != nil {
			return false
		}
		return acc.AIAgentEnabled && acc.Status == 1
	case ChannelWhatsapp:
		if s.waRepo == nil {
			return false
		}
		acc, err := s.waRepo.GetByID(ctx, uint(accID))
		if err != nil {
			return false
		}
		return acc.AIAgentEnabled
	default:
		return false
	}
}

func (s *WebhookService) triggerSalesEngine(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, hubMsg *model.MessageHub) {

	{
		customerID := ""
		sessionID := ""
		if hubMsg != nil {
			customerID = hubMsg.SenderID
			if hubMsg.ConversationID != "" {
				sessionID = hubMsg.ConversationID
			}
		}
		agent_runtime.PublishCustomerMessage(string(channel), accountID, customerID, sessionID, p.Content, p.EventID)
	}

	if s.smartOrchestrator != nil {
		s.triggerSmartOrchestrator(ctx, channel, accountID, p, hubMsg)
		return
	}

	if s.salesEngine == nil {
		return
	}

	parentCtx := context.Background()
	if c := tracing.CarrierFromContext(ctx); c != nil {
		parentCtx = tracing.WithCarrier(parentCtx, c)
	} else if parentTraceID := trace.TraceIDFromContext(ctx); parentTraceID != "" {
		parentCtx = trace.NewContextWithTraceID(parentCtx, parentTraceID)
	}

	aiTimeout := webhookEnvInt("WEBHOOK_AI_TIMEOUT_SECONDS", 180)
	if aiTimeout < 60 {
		aiTimeout = 60
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(aiTimeout)*time.Second)
	defer cancel()
	ctx = logger.WithModule(ctx, "webhook")

	agentCtx, _ := s.loadAgentForChannel(ctx, channel, accountID)

	req := &SalesRequest{
		SessionID:   p.ChatID,
		UserMessage: p.Content,
		Platform:    string(channel),
		AutoExecute: true,
		Config:      DefaultSalesEngineConfig(),
	}

	if hubMsg != nil {
		req.CustomerID = hubMsg.SenderID
		req.OneID = hubMsg.SenderID
	}

	resp, err := s.salesEngine.HandleWithAgent(ctx, req, agentCtx)
	if err != nil {
		logger.Errorf("[Webhook] sales engine error: %v", err)
		return
	}
	if resp == nil || resp.Reply == "" {
		return
	}
	if resp.TransferredToHuman {

		logger.Infof("[Webhook] transferred to human: %s", resp.TransferReason)
		return
	}

	s.sendOutbound(ctx, channel, accountID, p, resp.Reply, hubMsg, RichCardsFromDTO(resp.Cards))
}

func (s *WebhookService) loadAgentForChannel(ctx context.Context, channel WebhookChannel, accountID string) (*AgentContext, error) {
	if s.agentBindingSvc == nil {
		return nil, nil
	}
	channelType := NormalizeChannelType(string(channel))
	return s.agentBindingSvc.LoadAgentForChannel(ctx, channelType, accountID)
}

func (s *WebhookService) triggerSmartOrchestrator(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, hubMsg *model.MessageHub) {
	defer func() {
		if r := recover(); r != nil {
			logger.Ctx(ctx).Error().
				Interface("panic", r).
				Str("channel", string(channel)).
				Str("account_id", accountID).
				Str("event_id", p.EventID).
				Msg("[Webhook] triggerSmartOrchestrator panic recovered — AI 链路已断开，请查 root cause")
		}
	}()
	if s.smartOrchestrator == nil {
		logger.Ctx(ctx).Error().
			Str("channel", string(channel)).
			Str("account_id", accountID).
			Str("event_id", p.EventID).
			Str("conv_id", hubMsg.ConversationID).
			Msg("[Webhook] smartOrchestrator 未注入 — 桥接入站消息不会创建 customer_sessions / 不会生成 AI 回复。请检查 router.Setup() 中 webhookCtrl.SetSmartOrchestrator(orchestrator) 是否先于 bridgeIngressSvc.SetAITrigger(webhookSvc) 执行。")
		return
	}
	logger.Ctx(ctx).Info().
		Str("channel", string(channel)).
		Str("account_id", accountID).
		Str("event_id", p.EventID).
		Msg("[Webhook] triggerSmartOrchestrator start")

	routeCtx := context.Background()
	if parentTraceID := trace.TraceIDFromContext(ctx); parentTraceID != "" {
		routeCtx = trace.NewContextWithTraceID(routeCtx, parentTraceID)
	}
	routeCtx = logger.WithModule(routeCtx, "webhook")
	agentCtx, _ := s.loadAgentForChannel(routeCtx, channel, accountID)

	in := &IncomingContext{
		Platform:  model.Platform(channel),
		AccountID: accountID,
		SenderID:  p.Sender,
		Content:   p.Content,
		MessageID: p.EventID,
	}
	if hubMsg != nil {
		in.OneID = hubMsg.SenderID
		in.SenderName = hubMsg.SenderName
		in.MediaURL = hubMsg.MediaURL

		in.IsGroup = hubMsg.IsGroup
		in.GroupID = hubMsg.GroupID
		in.GroupName = groupNameFromHub(hubMsg)
	}

	if p.Extra != nil {
		if v, ok := p.Extra["sender_name"]; ok {
			if name, _ := v.(string); name != "" {
				in.SenderName = name
			}
		}
		if v, ok := p.Extra["media_url"]; ok {
			if url, _ := v.(string); url != "" {
				in.MediaURL = url
			}
		}
	}

	go s.runAIGeneration(ctx, channel, accountID, p, hubMsg, in, agentCtx)
}

func (s *WebhookService) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption) {
	defer func() {
		if r := recover(); r != nil {
			logger.Ctx(ctx).Error().
				Interface("panic", r).
				Str("channel", channel).
				Str("account_id", accountID).
				Str("conv_id", conversationID).
				Str("event_id", eventID).
				Str("sender", customerID).
				Msg("[Webhook] TriggerInboundAI panic recovered — AI 链路已断开，请查 root cause")
		}
	}()

	if eventID != "" && s.isDuplicate(ctx, eventID) {
		logger.Ctx(ctx).Debug().Str("event_id", eventID).Msg("[Webhook] TriggerInboundAI duplicate, skip")
		return
	}
	rateKey := string(channel) + ":" + accountID
	if !s.allowRate(ctx, rateKey) {
		logger.Ctx(ctx).Warn().Str("channel", channel).Str("account_id", accountID).
			Msg("[Webhook] TriggerInboundAI rate limited, skip")
		return
	}

	meta := &TriggerInboundMeta{}
	for _, opt := range opts {
		if opt != nil {
			opt(meta)
		}
	}
	p := &ParsedPayload{
		EventID: eventID,
		Sender:  customerID,
		Content: content,
	}
	if meta.SenderName != "" {
		p.Extra = map[string]any{"sender_name": meta.SenderName}
	}
	hubMsg := &model.MessageHub{
		MsgID:          eventID,
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: conversationID,
		SenderID:       customerID,
		SenderName:     meta.SenderName,
		Content:        content,
		Direction:      "inbound",
		IsGroup:        meta.IsGroup,
		GroupID:        meta.GroupID,
		IsRead:         true,
		SentAt:         time.Now(),
		Extra:          map[string]any{"sender_name": meta.SenderName},
	}

	if meta.SessionWebhook != "" {
		hubMsg.Extra["session_webhook"] = meta.SessionWebhook
		if meta.SessionWebhookExpiredAt > 0 {
			hubMsg.Extra["session_webhook_expired_at"] = meta.SessionWebhookExpiredAt
		}
	}
	if meta.IsGroup {
		if hubMsg.Extra == nil {
			hubMsg.Extra = model.JSONMap{}
		}
		hubMsg.Extra["is_group"] = true
		if meta.GroupID != "" {
			hubMsg.Extra["group_id"] = meta.GroupID
		}
		if meta.GroupName != "" {
			hubMsg.Extra["group_name"] = meta.GroupName
		}
	}
	s.triggerSmartOrchestrator(ctx, WebhookChannel(channel), accountID, p, hubMsg)
}

func (s *WebhookService) runAIGeneration(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, hubMsg *model.MessageHub, in *IncomingContext, agentCtx *AgentContext) {

	defer func() {
		if r := recover(); r != nil {
			logger.Ctx(ctx).Error().
				Interface("panic", r).
				Str("channel", string(channel)).
				Str("account_id", accountID).
				Str("event_id", p.EventID).
				Str("conv_id", hubMsg.ConversationID).
				Str("sender", p.Sender).
				Msg("[Webhook] runAIGeneration panic recovered — AI 链路已断开，请查 root cause")
		}
	}()

	if hubMsg != nil && hubMsg.ConversationID != "" {
		convID := hubMsg.ConversationID
		ingress := s.ingressSvc
		defer func() {
			if ingress != nil {
				ingress.ReleaseAIProcessingFlag(context.Background(), convID)
			}
		}()
	}

	// replySem 并发限流：正常 5s 超时；商机触发放宽到 30s（高意向不能丢）
	replySemTimeout := 5 * time.Second
	replyReason := "normal"
	if meta := extractTelegramReplyMetaFromCtx(ctx); meta != nil && meta.TriggerReason == "opportunity" {
		replySemTimeout = 30 * time.Second
		replyReason = "opportunity"
	}
	select {
	case s.replySem <- struct{}{}:
		defer func() { <-s.replySem }()
	case <-time.After(replySemTimeout):
		logger.Ctx(ctx).Error().
			Str("channel", string(channel)).
			Str("account_id", accountID).
			Str("event_id", p.EventID).
			Dur("timeout", replySemTimeout).
			Int("sem_capacity", cap(s.replySem)).
			Str("trigger_reason", replyReason).
			Msg("[Webhook] runAIGeneration replySem 满 / 阻塞超时 — 跳过本轮 AI 推理，避免 goroutine 堆积 OOM；依赖下轮 inbound 重试")
		return
	case <-ctx.Done():
		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Str("event_id", p.EventID).Msg("[Webhook] runAIGeneration ctx 取消，跳过")
		return
	}

	parentCtx := context.Background()
	if c := tracing.CarrierFromContext(ctx); c != nil {
		parentCtx = tracing.WithCarrier(parentCtx, c)
	} else if parentTraceID := trace.TraceIDFromContext(ctx); parentTraceID != "" {
		parentCtx = trace.NewContextWithTraceID(parentCtx, parentTraceID)
	}

	aiTimeout := webhookEnvInt("WEBHOOK_AI_TIMEOUT_SECONDS", 180)
	if aiTimeout < 60 {
		aiTimeout = 60
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(aiTimeout)*time.Second)
	defer cancel()
	ctx = logger.WithModule(ctx, "webhook")

	traceID := hubMsg.TraceID
	if traceID == "" {
		traceID = tracing.LinkInboundTraceID(ctx, hubMsg.ConversationID)
	}
	traceCarrier := &tracing.Carrier{
		TraceID:        traceID,
		ConversationID: hubMsg.ConversationID,
		AccountID:      hubMsg.AccountID,
		Channel:        string(channel),
	}
	ctx = tracing.WithCarrier(ctx, traceCarrier)

	ctx = tracing.InitRecalledChunks(ctx)

	var result *HandleResult
	var err error
	aiSpan := tracing.Start(ctx, tracing.NodeAIDispatch).
		Input(map[string]any{
			"channel":     string(channel),
			"account_id":  accountID,
			"conv_id":     hubMsg.ConversationID,
			"event_id":    p.EventID,
			"content_len": len(p.Content),
			"sender":      p.Sender,

			"message": map[string]any{"content": p.Content, "sender": p.Sender},
		}).
		Expected("AI 编排器生成回复并决策（自动回复 / 转人工 / 接管）")
	for attempt := 0; attempt <= WebhookMaxRetries; attempt++ {
		result, err = s.smartOrchestrator.HandleIncomingWithAgent(ctx, in, agentCtx)
		if err == nil {
			break
		}
		logger.Ctx(ctx).Warn().
			Err(err).
			Str("event_id", p.EventID).
			Int("attempt", attempt).
			Msg("orchestrator handle retry")
	}

	aiAbnormal := ""
	aiStatus := tracing.StatusOk
	if err != nil {
		aiStatus = tracing.StatusAbnormal
		aiAbnormal = "AI 编排器在重试后仍失败：" + err.Error()
	} else if result == nil {
		aiStatus = tracing.StatusAbnormal
		aiAbnormal = "AI 编排器返回 nil 结果（无回复决策）"
	}

	recalledChunkIDs := tracing.RecalledChunksOf(ctx)
	seenChunk := make(map[string]struct{})
	uniqRecalled := make([]string, 0, len(recalledChunkIDs))
	for _, id := range recalledChunkIDs {
		if _, ok := seenChunk[id]; !ok {
			seenChunk[id] = struct{}{}
			uniqRecalled = append(uniqRecalled, id)
		}
	}
	aiOutput := map[string]any{
		"ai_failed":          aiStatus == tracing.StatusAbnormal,
		"recalled_chunk_ids": uniqRecalled,
	}
	if result != nil {
		replyText := result.Reply
		if len(replyText) > 3000 {
			replyText = replyText[:3000]
		}
		aiOutput["ai_replied"] = result.AIReplied
		aiOutput["transferred"] = result.Transferred
		aiOutput["handler_type"] = result.HandlerType
		aiOutput["confidence"] = result.Confidence
		aiOutput["reply"] = replyText
		aiOutput["reply_len"] = len(result.Reply)
		aiOutput["session_id"] = result.SessionID
	}
	var aiSpanErr error
	if aiStatus == tracing.StatusAbnormal {
		aiSpanErr = fmt.Errorf("%s", aiAbnormal)
	}
	aiSpan.End(aiOutput, aiSpanErr)
	if err != nil {
		logger.Ctx(ctx).Error().
			Err(err).
			Str("event_id", p.EventID).
			Msg("orchestrator handle failed after retries")
		return
	}
	if result == nil {
		return
	}

	if result.Transferred {
		logger.Ctx(ctx).Info().
			Str("session_id", result.SessionID).
			Str("reason", result.TransferReason).
			Msg("transferred to human")
		return
	}

	if result.AIReplied && (result.Reply != "" || len(result.Cards) > 0) {
		outCtx := HandleResultToContext(ctx, result)
		if agentCtx != nil && agentCtx.AgentCode != "" {
			outCtx = AgentIDToContext(outCtx, agentCtx.AgentCode)
		}
		s.sendOutbound(outCtx, channel, accountID, p, result.Reply, hubMsg, result.Cards)
		return
	}

	logger.Ctx(ctx).Info().
		Str("session_id", result.SessionID).
		Str("handler", string(result.HandlerType)).
		Msg("no outbound")
}

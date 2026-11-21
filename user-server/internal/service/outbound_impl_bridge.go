package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// outboundBridge 是渠道 Bridge 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundBridge(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, cardsFallbackCount int, fail func(error), failUntracked func(error)) (sent bool) {
	// 桥接族（B5 单一 sender 能力位，CH-37b刀3 批准决议）：分支不再枚举渠道常量——
	// 可出站判定查询 registry OutboundPath=bridge_outbox 能力投影（channelgw/registry.go
	// init 经 SetBridgeOutboxChannels 装配）。新渠道只登记 registry 即自动获得本路径，
	// 第二份渠道清单就此消灭；恒等由 TestBridgeOutboxParityWithWebhookOutbound 双向钉住
	// （registry 有能力但出站不认 = 红；出站认但 registry 无能力 = 红）。
	// 不在集合内的渠道（webhook 直发渠道各自有 case；未知渠道落这里）保持原
	// 不支持分支不变：宁可明确拒绝，也不静默收下。
	if !bridgeOutboxCapable(channel) {
		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Str("account_id", accountID).Msg("unsupported outbound channel, skipped")
		failUntracked(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("出站不支持该渠道(%s)：webhookInboundCapable 与 sendOutbound 分支表已漂移", channel)))
		return
	}
	if err := authorizeBridgeReplySubmit(ctx, channel, accountID, hubMsg); err != nil {
		failUntracked(err)
		return
	}
	// Markdown 清洗已在 sendOutbound 的 RenderForChannel 里完成（specs/024 T404）：
	// sender 只负责投递，不再做格式变换。重复清洗会掩盖渲染层的漏配——
	// 渲染层少剥一次，这里也不会补上，问题会直接在客户侧暴露。
	if hubMsg != nil {
		outMsg := &model.MessageHub{
			MsgID:          ContentHashMsgID(string(channel), hubMsg.ConversationID, content),
			Platform:       string(channel),
			AccountID:      accountID,
			Direction:      "outbound",
			Status:         "pending",
			MsgType:        "text",
			SenderID:       accountID,
			ReceiverID:     hubMsg.SenderID,
			Content:        content,
			ConversationID: hubMsg.ConversationID,
			IsGroup:        hubMsg.IsGroup,
			GroupID:        hubMsg.GroupID,
			IsAIReply:      true,
			AIAgent:        "sales_engine",
			IsRead:         true,
			SentAt:         time.Now(),
		}
		outMsg.TraceID = tracing.LinkOutboundTraceID(ctx, hubMsg.ConversationID)
		// AI 回复的租户归属 = 触发它的入站行（2026-11-14 生产事故根修，douyin 4610）：
		// 重试/补触发等异步路径的 ctx 可能丢租户（detached/后台 ctx），此前 outMsg
		// 不带 TenantID → 隔离 callback 不填列 → gorm default:1 落进 tenant 1，
		// 成为认领/管理台双盲孤儿（AI 侧却记 ai_replied=true 三重静默）。
		// 入站行才是归属权威：继承它的租户；ctx 租户仅作兜底。**只填行列、不重建
		// ctx**——OnlyTenant 的 detachedCtx 会剥掉全部 value（HandleResult/tracing
		// 载体），下游 result 注入与轨迹链都会断；两者皆缺则由
		// CreateMessageHubIdempotent 的 fail-closed 门显式拒绝（不静默写孤儿）。
		if outMsg.TenantID == 0 {
			if hubMsg.TenantID != 0 {
				outMsg.TenantID = hubMsg.TenantID
			} else if tid, tok := tenant.FromContext(ctx); tok && tid != 0 {
				outMsg.TenantID = tid
			}
		}

		outMsg.DedupHash = ContentHashWithSender(string(channel), accountID, content)

		outMsg.Extra = model.JSONMap{
			"dm_target":    "conv",
			"scenario":     "auto_reply",
			"triggered_by": "ai_dispatch",
		}
		if agentID := extractAgentIDFromCtx(ctx); agentID != "" {
			outMsg.Extra["agent_id"] = agentID
		}
		if result := HandleResultFromContext(ctx); result != nil {
			// 降级轮（模型空输出 → emptyReplyFallback）没有模型答案：摘掉继承来的
			// 意图置信度并写 degraded 标记——应答质量指标据此把降级轮与正常 AI
			// 回答分开计数（CS-02 口径修复；handler_type/scenario/放行语义不变）。
			if result.Confidence > 0 && !result.Degraded {
				outMsg.Extra["confidence"] = result.Confidence
			}
			if result.IntentType != "" {
				outMsg.Extra["intent"] = result.IntentType
			}
			if result.HandlerType != "" {
				outMsg.Extra["handler_type"] = string(result.HandlerType)
			}
			if result.Degraded {
				outMsg.Extra["degraded"] = "model_empty_reply"
			}
		}
		if cardsFallbackCount > 0 {
			outMsg.Extra["cards_fallback_count"] = cardsFallbackCount
		}
		outCarrier := tracing.CarrierForHub(ctx, hubMsg).WithMsgID(outMsg.MsgID)
		outCarrier.Direction = "outbound"
		ctx = tracing.WithCarrier(ctx, outCarrier)
		if outCarrier.TurnID != "" {
			outMsg.Extra["turn_id"] = outCarrier.TurnID
		}
		if outCarrier.AnswerAttempt > 0 {
			outMsg.Extra["answer_attempt"] = outCarrier.AnswerAttempt
		}
		if undeliverable, reason := isBridgeChannelUndeliverableLocal(accountID, outMsg.ConversationID); undeliverable {
			outMsg.Status = "failed"
			if outMsg.Extra == nil {
				outMsg.Extra = model.JSONMap{}
			}
			outMsg.Extra["undeliverable_reason"] = reason
			outMsg.Extra["scenario"] = "undeliverable"
			logger.Ctx(ctx).Warn().
				Str("module", "bridge").
				Str("channel", string(channel)).
				Str("account_id", accountID).
				Str("conversation_id", outMsg.ConversationID).
				Str("reason", reason).
				Msg("bridge outbound target undeliverable; marked failed instead of pending")
		}

		persisted := outMsg
		// duplicateOccupied：Create 无错但 ID 未回填 = ON CONFLICT DO NOTHING 命中
		//（同键占位：同会话同文案已 delivered/pending 在途）——与真 DB 错误严格区分，
		// 两者在 id=0 分支走不同处置（占位=转人工旁路不可重试；真错误=可重试进队列）。
		duplicateOccupied := false
		outSpan := tracing.Start(ctx, tracing.NodeOutboundEnqueue).
			Input(map[string]any{"channel": string(channel), "account_id": accountID, "conv_id": outMsg.ConversationID,
				"content_len": len(outMsg.Content), "is_ai_reply": outMsg.IsAIReply}).
			Expected("回复实际落库并进入可认领队列；不是平台送达").MsgID(outMsg.MsgID)
		if _, err := s.delayedRepo.CreateMessageHubIdempotent(ctx, outMsg); err != nil {
			retryMsg := &model.MessageHub{
				MsgID: outMsg.MsgID + ":" + hubMsg.ConversationID,
				// 租户必须显式随行（2026-11-14 生产事故修复）：此前的重试行靠 gorm
				// default:1 兜底落进 tenant 1，成为认领/管理台双盲的孤儿——即使
				// CreateMessageHubIdempotent 已加 fail-closed，这里也要带上正确租户。
				TenantID:       outMsg.TenantID,
				Platform:       outMsg.Platform,
				AccountID:      outMsg.AccountID,
				Direction:      "outbound",
				Status:         outMsg.Status,
				MsgType:        outMsg.MsgType,
				SenderID:       outMsg.SenderID,
				ReceiverID:     outMsg.ReceiverID,
				Content:        outMsg.Content,
				ConversationID: outMsg.ConversationID,
				IsGroup:        outMsg.IsGroup,
				GroupID:        outMsg.GroupID,
				IsAIReply:      outMsg.IsAIReply,
				AIAgent:        outMsg.AIAgent,
				IsRead:         outMsg.IsRead,
				SentAt:         outMsg.SentAt,
				TraceID:        outMsg.TraceID,
				DedupHash:      outMsg.DedupHash,
				Extra:          outMsg.Extra,
			}
			if _, err2 := s.delayedRepo.CreateMessageHubIdempotent(ctx, retryMsg); err2 != nil {
				logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").Str("channel", string(channel)).Msg("failed to persist bridge outbound reply to message_hub")
			} else {
				persisted = retryMsg
			}
		} else if persisted.ID == 0 {
			duplicateOccupied = true
		}
		if s.inboxConvRepo != nil && persisted.ID != 0 {

			if err := s.inboxConvRepo.UpsertFromMessage(ctx, repository.UpsertFromMessageInput{
				Platform:           persisted.Platform,
				AccountID:          persisted.AccountID,
				CustomerID:         persisted.ReceiverID,
				ConversationID:     persisted.ConversationID,
				LastMessageID:      persisted.ID,
				LastMessagePreview: persisted.Content,
				LastMessageAt:      persisted.SentAt,
				LastMessageFrom:    "ai",
			}); err != nil {
				logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").Msg("failed to upsert inbox_conversations for outbound")
			}
		}

		if GlobalSSEPublisher != nil && persisted != nil && persisted.ID != 0 &&
			(persisted.Status == "pending" || persisted.Status == "inflight") {
			logger.Ctx(ctx).Debug().
				Int("hub_id", int(persisted.ID)).
				Str("channel", string(channel)).
				Str("account_id", accountID).
				Str("conv_id", persisted.ConversationID).
				Msg("[SSE] webhook_outbound calling GlobalSSEPublisher")
			// 派生带租户的 ctx（认领要 UPDATE message_hub，on 模式下无租户即被守卫拒绝），
			// 口径同 bridge_outbound.go 的同名调用点。
			pubCtx := tenant.BackgroundWithTenant(ctx)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Ctx(ctx).Error().Any("error", r).Msg("[SSE] GlobalSSEPublisher panic recovered in webhook_outbound")
					}
				}()
				GlobalSSEPublisher(
					pubCtx,
					string(channel), accountID,
					uint64(persisted.ID),
					persisted.ConversationID,
					persisted.MsgType,
					persisted.ReceiverID,
					persisted.Content,
					persisted.MediaURL,
					persisted.IsAIReply,
					persisted.SentAt,
				)
			}()
		}

		enqueueAbnormal := ""
		enqueueStatus := tracing.StatusOk
		if persisted.ID == 0 {
			enqueueStatus = tracing.StatusAbnormal
			enqueueAbnormal = "没有新可投递行：重复占位或持久化失败"
		} else if persisted.Status == "failed" {
			enqueueStatus = tracing.StatusAbnormal
			enqueueAbnormal = "目标不可达（占位账号/未知账号），标记 failed 而非 pending，避免污染下行出库队列"
		}

		outSpan.MsgID(persisted.MsgID)
		outSpan.Output(map[string]any{
			"msg_id":   persisted.MsgID,
			"status":   persisted.Status,
			"id":       persisted.ID,
			"enqueued": persisted.ID != 0 && (persisted.Status == "pending" || persisted.Status == "inflight"),
		})
		var outSpanErr error
		if enqueueStatus == tracing.StatusAbnormal {
			outSpanErr = fmt.Errorf("%s", enqueueAbnormal)
		}
		outSpan.End(nil, outSpanErr)

		// sent 的口径（D-02）：= 这条回复确实交接给了桥接出库队列
		// （message_hub 落库成功且状态可被扩展领取），**不等于**客户已收到
		// ——真达由扩展 ack 走 UpdateDeliveryStatus 收口。
		// 未落库 / 被标 failed / 无入站上下文都不能算成功，否则重放 worker
		// 会把这条 MarkSent 掉，回复静默丢失。
		switch {
		case persisted.ID == 0 && duplicateOccupied:
			// 同键占位（同会话同文案已 delivered/pending 在途）——AI 对重复问题
			// 确定性输出同一话术时的必然分支（2026-11-14 生产终局实证：19:10/19:12/
			// 19:14 三次重试全部 id=0，客户「给我一份功能清单」两次追问石沉大海）。
			// 处置三件事：①落一条 failed 占位行（坐席在收件箱看得到「客户问过且
			// 未解决」）；②markSendFailed 置不可重试（同一占位行不会消失，重试
			// 只制造噪音——19:10 批三次重试全部 id=0 即实证）；③Error 级出声。
			dupRow := &model.MessageHub{
				TenantID:  persisted.TenantID,
				MsgID:     persisted.MsgID + ":dup",
				Platform:  string(channel),
				AccountID: accountID,
				Direction: "outbound",
				// 2026-11-19 口径修正：同会话同文案已 delivered/pending = 买家已收到过，
				// 占位行不再标 failed（旧口径灌进 DLQ 制造投递故障假象），落 duplicate 终态。
				Status:         "duplicate",
				MsgType:        "text",
				SenderID:       accountID,
				ReceiverID:     hubMsg.ReceiverID,
				Content:        content,
				ConversationID: persisted.ConversationID,
				IsAIReply:      true,
				AIAgent:        "sales_engine",
				IsRead:         false,
				SentAt:         time.Now(),
				TraceID:        persisted.TraceID,
				Extra:          model.JSONMap{"duplicate_of": persisted.MsgID, "duplicate_reason": "same reply already delivered/pending"},
			}
			if _, derr := s.delayedRepo.CreateMessageHubAudit(ctx, dupRow); derr != nil {
				logger.Ctx(ctx).Warn().Err(derr).Str("module", "bridge").
					Str("conv_id", persisted.ConversationID).Msg("[Bridge] 重复话术占位行落库失败（坐席可见性降级）")
			}
			logger.Ctx(ctx).Error().
				Str("module", "bridge").
				Str("channel", string(channel)).
				Str("conv_id", persisted.ConversationID).
				Str("msg_id", persisted.MsgID).
				Msg("[Bridge] AI 重复话术已投递过（同会话同文案占位）——本条转 failed 占位行，需人工跟进客户追问")
			fail(&ChannelError{
				Category:  CategoryBadRequest,
				Retryable: false,
				Raw:       "duplicate reply already delivered/pending (same conv+content); needs human follow-up",
			})
		case persisted.ID == 0:
			// 真 DB 错误（非占位冲突）：保持可重试——回复既没出库也没人再投就永久丢了
			//（webhook_batchc_bridge_sent_test 契约），占位冲突走上面的 duplicateOccupied 分支。
			fail(&ChannelError{
				Category:  CategoryUnknown,
				Retryable: true,
				Raw:       "桥接出站未写入 message_hub(channel=" + string(channel) + " conv=" + persisted.ConversationID + ")",
			})
		case persisted.Status == "failed":
			reason, _ := persisted.Extra["undeliverable_reason"].(string)
			// 桥接族的失败轨迹就是它自己那一行（status=failed + scenario=undeliverable +
			// 原因），这里只补结构化错误与日志；再走一次轨迹落库会变成同一会话两行出站。
			failUntracked(preSendFailure(channel, CategoryBadRequest,
				"桥接目标不可达: "+reason))
		default:
			sent = true
		}
	} else {
		logger.Ctx(ctx).Error().Str("channel", string(channel)).Str("account_id", accountID).
			Msg("[Bridge] 缺少入站消息上下文（无 conversation 目标），AI 回复未出库")
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("%s 桥接出站缺少入站上下文，无 conversation 目标，未出库", channel)))
	}
	return
}

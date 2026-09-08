package service

import (
	"bytes"

	"context"

	"encoding/json"

	"fmt"

	"io"

	"net/http"

	"net/url"

	"os"
	"regexp"
	"strconv"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/channelbot/telegram"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	aiReplyQuietStartHour = 23
	aiReplyQuietEndHour   = 7

	delayedOutboundPollInterval = 30 * time.Second
	delayedOutboundBatchSize    = 20
)

// DelayedOutboundReply AI 回复延迟出站记录（表 reach_delayed_outbound）
type DelayedOutboundReply struct {
	ID             uint          `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform       string        `gorm:"type:varchar(30);index" json:"platform"`
	AccountID      string        `gorm:"type:varchar(64)" json:"account_id"`
	ConversationID string        `gorm:"type:varchar(128);index" json:"conversation_id"`
	SenderID       string        `gorm:"type:varchar(128)" json:"sender_id"`
	Content        string        `gorm:"type:text" json:"content"`
	Cards          model.JSONMap `gorm:"type:jsonb" json:"cards"`
	SendAt         time.Time     `gorm:"index" json:"send_at"`
	Status         string        `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	SentAt         *time.Time    `json:"sent_at"`
	CreatedAt      time.Time     `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (DelayedOutboundReply) TableName() string { return "reach_delayed_outbound" }

func isAIReplyQuietHours(t time.Time) bool {
	if os.Getenv("DISABLE_AI_QUIET_HOURS") != "" {
		return false
	}
	return inQuietHoursWindow(t, aiReplyQuietStartHour, aiReplyQuietEndHour)
}

var aiReplyQuietHoursFn = isAIReplyQuietHours

type delayedReplayCtxKey struct{}

// DelayedReplayToContext 标记 ctx 为延迟队列重放路径
func DelayedReplayToContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, delayedReplayCtxKey{}, true)
}

func isDelayedReplay(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(delayedReplayCtxKey{}).(bool)
	return ok && v
}

func init() { _db.RegisterExtraModels(&DelayedOutboundReply{}) }

func (s *WebhookService) enqueueDelayedOutbound(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard) bool {
	if s.db == nil {
		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Msg("[H-3] db 未初始化，quiet hours 延迟入队失败，按原路径直接发送")
		return false
	}
	var cardsPayload model.JSONMap
	if len(cards) > 0 {
		if raw, err := json.Marshal(cards); err == nil {
			cardsPayload = model.JSONMap{"cards": json.RawMessage(raw)}
		}
	}

	rec := &DelayedOutboundReply{
		Platform:  string(channel),
		AccountID: accountID,
		SenderID:  p.Sender,
		Content:   content,
		Cards:     cardsPayload,
		SendAt:    nextQuietHoursRelease(time.Now(), aiReplyQuietEndHour),
		Status:    "pending",
	}
	if hubMsg != nil {
		rec.ConversationID = hubMsg.ConversationID
	}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", string(channel)).Msg("[H-3] 延迟入队失败，按原路径直接发送")
		return false
	}
	logger.Ctx(ctx).Info().
		Str("channel", string(channel)).
		Str("conv_id", rec.ConversationID).
		Time("send_at", rec.SendAt).
		Msg("[H-3] AI 回复命中 quiet hours(23:00-7:00)，进入延迟队列次日首发")
	s.startDelayedOutboundDispatch()
	return true
}

var delayedDispatchStop chan struct{}

func (s *WebhookService) startDelayedOutboundDispatch() {
	dispatchOnce.Do(func() {
		delayedDispatchStop = make(chan struct{})

		utils.SafeGo(nil, "webhook_outbound.delayed_dispatch", func(ctx context.Context) {
			ticker := time.NewTicker(delayedOutboundPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-delayedDispatchStop:
					return
				case <-ticker.C:
					s.dispatchDueDelayedOutbound(ctx)
				}
			}
		})
	})
}

var dispatchOnce sync.Once

func (s *WebhookService) dispatchDueDelayedOutbound(ctx context.Context) {
	if s.db == nil {
		return
	}
	now := time.Now()
	var picked []DelayedOutboundReply
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT * FROM reach_delayed_outbound WHERE status = ? AND send_at <= ? ORDER BY send_at ASC LIMIT ? FOR UPDATE SKIP LOCKED`,
			"pending", now, delayedOutboundBatchSize).Scan(&picked).Error; err != nil {
			return err
		}
		if len(picked) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(picked))
		for _, r := range picked {
			ids = append(ids, r.ID)
		}
		return tx.Model(&DelayedOutboundReply{}).Where("id IN ?", ids).
			Update("status", "sending").Error
	})
	if err != nil {

		picked = nil
		var ids []uint
		if err2 := s.db.WithContext(ctx).Model(&DelayedOutboundReply{}).
			Where("status = ? AND send_at <= ?", "pending", now).
			Order("send_at ASC").Limit(delayedOutboundBatchSize).
			Pluck("id", &ids).Error; err2 != nil || len(ids) == 0 {
			return
		}
		res := s.db.WithContext(ctx).Model(&DelayedOutboundReply{}).
			Where("id IN ? AND status = ?", ids, "pending").Update("status", "sending")
		if res.Error != nil || res.RowsAffected == 0 {
			return
		}
		if err2 := s.db.WithContext(ctx).Where("id IN ? AND status = ?", ids, "sending").
			Order("send_at ASC").Limit(delayedOutboundBatchSize).
			Find(&picked).Error; err2 != nil || len(picked) == 0 {
			return
		}
	}
	for i := range picked {
		s.replayDelayedOutbound(ctx, &picked[i])
	}
}

func (s *WebhookService) replayDelayedOutbound(ctx context.Context, rec *DelayedOutboundReply) {
	channel := WebhookChannel(rec.Platform)
	hubMsg := &model.MessageHub{
		ConversationID: rec.ConversationID,
		SenderID:       rec.SenderID,
		SentAt:         rec.CreatedAt,
	}
	p := &ParsedPayload{EventID: fmt.Sprintf("delayed-%d", rec.ID), Sender: rec.SenderID}

	var cards []model.RichCard
	if rec.Cards != nil {
		if raw, ok := rec.Cards["cards"]; ok {
			if data, err := json.Marshal(raw); err == nil {
				_ = json.Unmarshal(data, &cards)
			}
		}
	}

	s.sendOutbound(DelayedReplayToContext(ctx), channel, rec.AccountID, p, rec.Content, hubMsg, cards)

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&DelayedOutboundReply{}).Where("id = ?", rec.ID).
		Updates(map[string]any{"status": "sent", "sent_at": &now}).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 延迟回复状态回写失败")
	}
}

func (s *WebhookService) sendOutbound(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard) {

	if !isDelayedReplay(ctx) && aiReplyQuietHoursFn(time.Now()) {
		if s.enqueueDelayedOutbound(ctx, channel, accountID, p, content, hubMsg, cards) {
			return
		}
	}

	if !agent_runtime.ClaimReply(p.EventID) {
		logger.Ctx(ctx).Info().Str("event_id", p.EventID).Msg("skip duplicate outbound (event already replied)")

		return
	}

	sent := false
	ctx, cancel := context.WithTimeout(ctx, utils.MediumTimeout)
	defer func() {
		if !sent {
			agent_runtime.ReleaseReply(p.EventID)
		}
		cancel()
	}()
	switch channel {
	case ChannelWeCom:

		if s.integration == nil {
			return
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			return
		}
		if _, err := s.integration.SendMessage(ctx, &WeComSendRequest{
			AccountID:      uint(accID),
			ExternalUserID: p.Sender,
			MsgType:        "text",
			Content:        content,
			IsAIReply:      true,
			AIAgent:        "sales_engine",
		}); err != nil {
			s.outboundSendFailed(ctx, channel, accountID, hubMsg, err)
		} else {
			sent = true
		}
	case ChannelFeishu:
		if s.feishuIntegration == nil {
			s.feishuIntegration = NewFeishuIntegrationService(s.db)
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			return
		}

		target := p.Sender
		idType := "open_id"
		if hubMsg != nil && hubMsg.IsGroup && hubMsg.GroupID != "" {
			target = hubMsg.GroupID
			idType = "open_chat_id"
		}
		outConv := ""
		if hubMsg != nil {
			outConv = hubMsg.ConversationID
		}
		if err := s.feishuIntegration.SendMessage(ctx, uint(accID), target, content, idType, outConv); err != nil {
			s.outboundSendFailed(ctx, channel, accountID, hubMsg, err)
		} else {
			sent = true
		}
		// 富卡片随行下发（card.show 等工具产出），映射为飞书 interactive 卡片
		for _, card := range cards {
			cardJSON, merr := feishuCardToInteractiveJSON(&card)
			if merr != nil {
				logger.Ctx(ctx).Warn().Err(merr).Str("channel", "feishu").Msg("feishu card marshal failed, skip")
				continue
			}
			if cerr := s.feishuIntegration.SendInteractiveCard(ctx, uint(accID), target, cardJSON, idType, outConv); cerr != nil {
				logger.Ctx(ctx).Error().Err(cerr).Str("channel", "feishu").Str("open_id", target).Msg("outbound feishu card failed")
			} else {
				sent = true
			}
		}
	case ChannelTelegram:
		if s.tgIntegration == nil {
			s.tgIntegration = NewTelegramIntegrationService(s.db)
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			return
		}

		var chatID int64
		if hubMsg != nil && hubMsg.ConversationID != "" {
			chatID, _ = strconv.ParseInt(hubMsg.ConversationID, 10, 64)
		}
		if chatID == 0 {
			return
		}

		// 群聊增强：@mention 原发言人 + reply-to 消息
		// 私聊保持原样
		sendContent := content
		sendOpts := telegram.SendMessageOptions{DisableWebPreview: true}
		if hubMsg != nil && hubMsg.IsGroup {
			if meta := extractTelegramReplyMetaFromCtx(ctx); meta != nil && meta.TriggerReason != "start" {
				sendContent, sendOpts = buildTelegramGroupReply(content, meta)
			}
		}

		if err := s.tgIntegration.SendMessageEx(ctx, uint(accID), chatID, sendContent, sendOpts); err != nil {
			s.outboundSendFailed(ctx, channel, accountID, hubMsg, err)
		} else {
			sent = true
		}

		for _, card := range cards {
			if err := s.tgIntegration.SendCard(ctx, uint(accID), chatID, &card); err != nil {
				logger.Ctx(ctx).Error().Err(err).Str("channel", "telegram").Int64("chat_id", chatID).Msg("outbound card failed")
			} else {
				sent = true
			}
		}
	case ChannelQQ:
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			return
		}
		convID := ""
		if hubMsg != nil && hubMsg.ConversationID != "" {
			convID = hubMsg.ConversationID
		}
		if convID == "" {
			return
		}
		// 被动回复关联原消息 ID（QQ 平台 5 分钟窗口 5 条被动回复额度）。
		// 惰性单例：msg_seq 计数与 access_token 缓存必须在多次出站间保持，
		// 每次新建实例会导致 seq 恒为 1（平台按重复丢弃）+ token 重复获取。
		if s.qqIntegration == nil {
			s.qqIntegration = NewQQIntegrationService(s.db)
		}
		if err := s.qqIntegration.SendMessage(ctx, uint(accID), convID, QQOutboundMsgID(hubMsg), content); err != nil {
			s.outboundSendFailed(ctx, channel, accountID, hubMsg, err)
		} else {
			sent = true
		}
	case ChannelWhatsapp:
		if s.waIntegration == nil {
			s.waIntegration = NewWhatsAppCloudIntegrationService(s.db)
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			return
		}

		s.ensureReposFromDB(ctx)
		if s.messageHubRepo != nil && hubMsg != nil && hubMsg.ConversationID != "" {
			if last, qerr := s.messageHubRepo.GetLastInboundByConversation(ctx, hubMsg.ConversationID); qerr == nil && last != nil && !last.SentAt.IsZero() {
				if time.Since(last.SentAt) > 24*time.Hour {
					// 超 24h 客服窗口：自由文本会被 Meta 拒收，降级走预审批模板兜底
					//（模板名可用环境变量按账号/全局配置；未配置模板时维持原失败语义）。
					tplName := os.Getenv("WHATSAPP_FALLBACK_TEMPLATE")
					if tplName == "" {
						logger.Ctx(ctx).Warn().
							Str("channel", "whatsapp").Str("account_id", accountID).
							Str("to", p.Sender).
							Time("last_inbound_at", last.SentAt).
							Msg("[WhatsApp] 超出 24h 客服窗口且未配置 WHATSAPP_FALLBACK_TEMPLATE，AI 文本回复不可送达，标记失败")
						return
					}
					logger.Ctx(ctx).Info().
						Str("channel", "whatsapp").Str("account_id", accountID).
						Str("to", p.Sender).
						Str("template", tplName).
						Msg("[WhatsApp] 超出 24h 客服窗口，AI 回复降级为模板消息发送")
					if terr := s.waIntegration.SendTemplateMessage(ctx, uint(accID), p.Sender, content, &WhatsAppTemplatePayload{
						TemplateName: tplName,
						Language:     os.Getenv("WHATSAPP_FALLBACK_TEMPLATE_LANG"),
					}); terr != nil {
						s.outboundSendFailed(ctx, channel, accountID, hubMsg, terr)
					} else {
						sent = true
					}
					return
				}
			}
		}

		if err := s.waIntegration.SendMessage(ctx, uint(accID), p.Sender, content); err != nil {
			s.outboundSendFailed(ctx, channel, accountID, hubMsg, err)
		} else {
			sent = true
		}
	case ChannelDingTalk:

		webhookURL := ""
		var expiredAt int64
		if hubMsg != nil && hubMsg.Extra != nil {
			if v, ok := hubMsg.Extra["session_webhook"].(string); ok {
				webhookURL = v
			}
			switch t := hubMsg.Extra["session_webhook_expired_at"].(type) {
			case int64:
				expiredAt = t
			case float64:
				expiredAt = int64(t)
			}
		}
		if webhookURL == "" {
			logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
				Str("conv_id", convIDOrEmpty(hubMsg)).
				Msg("[DingTalk] 缺少 sessionWebhook（回调未携带或非机器人消息），AI 回复无法送达")
			return
		}

		if expiredAt > 1_000_000_000_000 {
			expiredAt /= 1000
		}
		if expiredAt > 0 && time.Now().Unix() > expiredAt {
			logger.Ctx(ctx).Warn().Str("channel", "dingtalk").Str("account_id", accountID).
				Int64("expired_at", expiredAt).
				Msg("[DingTalk] sessionWebhook 已过期，AI 回复无法送达（用户重新发言可恢复）")
			return
		}

		u, perr := url.Parse(webhookURL)
		if perr != nil || !dingtalkWebhookHostAllowed(u) {
			logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
				Msg("[DingTalk] sessionWebhook 域名非法（仅允许 *.dingtalk.com），拒绝发送")
			return
		}
		payload := map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("build dingtalk reply request failed")
			return
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("dingtalk reply send failed")
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var dtResult struct {
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(respBody, &dtResult); err != nil || dtResult.Errcode != 0 {
			logger.Ctx(ctx).Error().Int("http_status", resp.StatusCode).Int("errcode", dtResult.Errcode).
				Str("errmsg", dtResult.Errmsg).
				Msg("[DingTalk] AI 回复出站被平台拒绝")
			return
		}
		sent = true
	case ChannelWechat:

		if s.wechatIntegration == nil {
			s.wechatIntegration = NewWechatService(s.db)
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil {
			accID = 0
		}
		if _, err := s.wechatIntegration.SendCustomMessage(ctx, uint(accID), p.Sender, "text", content); err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "wechat").Str("account_id", accountID).
				Str("open_id", p.Sender).Msg("[Wechat] AI 回复出站失败")
		} else {
			sent = true
		}
	case ChannelDouyin, ChannelXiaohongshu, ChannelTiktok, ChannelXianyu, ChannelKuaishou:

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
				if result.Confidence > 0 {
					outMsg.Extra["confidence"] = result.Confidence
				}
				if result.SalesResponse != nil && result.SalesResponse.Intent != nil && result.SalesResponse.Intent.IntentType != "" {
					outMsg.Extra["intent"] = string(result.SalesResponse.Intent.IntentType)
				}
				if result.HandlerType != "" {
					outMsg.Extra["handler_type"] = string(result.HandlerType)
				}
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
			if err := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "platform"}, {Name: "msg_id"}, {Name: "conversation_id"}}, DoNothing: true}).Create(outMsg).Error; err != nil {
				retryMsg := &model.MessageHub{
					MsgID:          outMsg.MsgID + ":" + hubMsg.ConversationID,
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
				}
				if err2 := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "platform"}, {Name: "msg_id"}, {Name: "conversation_id"}}, DoNothing: true}).Create(retryMsg).Error; err2 != nil {
					logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").Str("channel", string(channel)).Msg("failed to persist bridge outbound reply to message_hub")
				} else {
					persisted = retryMsg
				}
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
				go func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Ctx(ctx).Error().Any("error", r).Msg("[SSE] GlobalSSEPublisher panic recovered in webhook_outbound")
						}
					}()
					GlobalSSEPublisher(
						string(channel), accountID,
						uint64(persisted.ID),
						persisted.ConversationID,
						persisted.MsgType,
						persisted.ReceiverID,
						persisted.Content,
						persisted.IsAIReply,
						persisted.SentAt,
					)
				}()
			}

			enqueueAbnormal := ""
			enqueueStatus := tracing.StatusOk
			if persisted.Status == "failed" {
				enqueueStatus = tracing.StatusAbnormal
				enqueueAbnormal = "目标不可达（占位账号/未知账号），标记 failed 而非 pending，避免污染下行出库队列"
			}

			ctx = tracing.WithCarrier(ctx, &tracing.Carrier{
				TraceID:        persisted.TraceID,
				ConversationID: persisted.ConversationID,
				AccountID:      persisted.AccountID,
				Channel:        persisted.Platform,
			})

			outSpan := tracing.Start(ctx, tracing.NodeOutboundEnqueue).
				Input(map[string]any{
					"channel":     string(channel),
					"account_id":  accountID,
					"conv_id":     persisted.ConversationID,
					"content_len": len(persisted.Content),
					"is_ai_reply": persisted.IsAIReply,
				}).
				Expected("AI 回复落库 message_hub(status=pending)，进入下行出库队列").
				MsgID(persisted.MsgID)
			outSpan.Output(map[string]any{
				"msg_id": persisted.MsgID,
				"status": persisted.Status,
				"id":     persisted.ID,
			})
			var outSpanErr error
			if enqueueStatus == tracing.StatusAbnormal {
				outSpanErr = fmt.Errorf("%s", enqueueAbnormal)
			}
			outSpan.End(nil, outSpanErr)
		}

		sent = true
	default:

		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Str("account_id", accountID).Msg("unsupported outbound channel, skipped")
	}

	if s.ingressSvc != nil && hubMsg != nil && hubMsg.ConversationID != "" {
		s.ingressSvc.ReleaseAIProcessingFlag(ctx, hubMsg.ConversationID)

		go s.ingressSvc.RecheckUnrepliedAndTrigger(context.WithoutCancel(ctx), hubMsg.ConversationID, "")
	}
}

func isBridgeChannelUndeliverableLocal(accountID, conversationID string) (bool, string) {
	return bridgeOutboundUndeliverable(accountID, conversationID)
}

type handleResultCtxKey struct{}

func convIDOrEmpty(h *model.MessageHub) string {
	if h == nil {
		return ""
	}
	return h.ConversationID
}

var dingtalkWebhookHostAllowed = func(u *url.URL) bool {
	return u != nil && (u.Host == "oapi.dingtalk.com" || strings.HasSuffix(u.Host, ".dingtalk.com"))
}

// HandleResultToContext 把 HandleResult 注入 ctx，供 sendOutbound 取出补字段。
func HandleResultToContext(ctx context.Context, r *HandleResult) context.Context {
	if ctx == nil || r == nil {
		return ctx
	}
	return context.WithValue(ctx, handleResultCtxKey{}, r)
}

// HandleResultFromContext 从 ctx 取出 HandleResult（注入方未设时返回 nil）。
func HandleResultFromContext(ctx context.Context) *HandleResult {
	if ctx == nil {
		return nil
	}
	if r, ok := ctx.Value(handleResultCtxKey{}).(*HandleResult); ok {
		return r
	}
	return nil
}

type agentIDCtxKey struct{}

// AgentIDToContext 把 agentID 注入 ctx（多 AI 智能体路由时填充）。
func AgentIDToContext(ctx context.Context, agentID string) context.Context {
	if ctx == nil || agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, agentIDCtxKey{}, agentID)
}

func extractAgentIDFromCtx(ctx context.Context) string {
	if ctx == nil {
		return "unknown"
	}
	if v, ok := ctx.Value(agentIDCtxKey{}).(string); ok && v != "" {
		return v
	}
	return "unknown"
}

// ─── Telegram 群回复 @mention 上下文工具 ────────────────────────────

type telegramReplyMetaCtxKey struct{}

// TelegramReplyMeta 群回复 @mention 原发言人 + 触发原因
type TelegramReplyMeta struct {
	FromUsername  string // Telegram @username（可能为空 → fallback 用 fromName）
	FromName      string // 真实姓名（HTML 转义后的展示名）
	FromUserID    int64  // tg://user?id= 链接用
	ReplyToMsgID  int64  // ReplyToMessageID（Telegram int64，0 表示不引用）
	TriggerReason string // "mention" | "opportunity" | "private" | "start"
}

// TelegramReplyMetaToContext 把群回复元信息注入 ctx
func TelegramReplyMetaToContext(ctx context.Context, meta *TelegramReplyMeta) context.Context {
	if ctx == nil || meta == nil {
		return ctx
	}
	return context.WithValue(ctx, telegramReplyMetaCtxKey{}, meta)
}

func extractTelegramReplyMetaFromCtx(ctx context.Context) *TelegramReplyMeta {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(telegramReplyMetaCtxKey{}).(*TelegramReplyMeta); ok {
		return v
	}
	return nil
}

// buildTelegramGroupReply 把 AI 回复内容包装成 Telegram 群聊友好格式：
//  1. 顶部 @mention 原发言人（<a href="tg://user?id=X">@username</a>）
//  2. ReplyToMessageID 引用原消息（群内对话更直观）
//  3. ParseModeHTML 确保 @mention 渲染成可点击链接
func buildTelegramGroupReply(content string, meta *TelegramReplyMeta) (string, telegram.SendMessageOptions) {
	opts := telegram.SendMessageOptions{
		ParseMode:                 "HTML",
		DisableMarkdownConversion: true, // 我们自己写 HTML（@mention + escape AI 回复）
		DisableWebPreview:         true,
	}
	if meta == nil {
		// 无 meta 也走 HTML escape（防御 LLM XSS）
		return htmlEscapeAndPreserveMarkdown(content), opts
	}
	if meta.ReplyToMsgID > 0 {
		opts.ReplyToMessageID = meta.ReplyToMsgID
	}

	// 1. 构造 @mention 头部（我们自己写的 HTML，可控安全）
	var mentionPrefix string
	displayName := meta.FromName
	if displayName == "" {
		displayName = meta.FromUsername
	}
	escapedDisplayName := htmlEscapeText(displayName)
	if escapedDisplayName == "" {
		escapedDisplayName = "朋友"
	}
	if meta.FromUserID > 0 {
		// 可点击链接 @mention
		mentionPrefix = fmt.Sprintf(`<a href="tg://user?id=%d">@%s</a> `, meta.FromUserID, escapedDisplayName)
	} else if meta.FromUsername != "" {
		mentionPrefix = "@" + htmlEscapeText(meta.FromUsername) + " "
	} else {
		mentionPrefix = escapedDisplayName + " "
	}

	// 2. AI 回复做 escape + 保留 **bold** 等 Markdown → HTML
	escapedContent := htmlEscapeAndPreserveMarkdown(content)

	// 3. 给商机触发加轻量引导提示
	var leadHint string
	if meta.TriggerReason == "opportunity" {
		leadHint = "\n💡 看到你对这个话题感兴趣，我是 HiveMtk 的 AI 助手，想帮你进一步了解~"
	}

	full := mentionPrefix + escapedContent + leadHint
	return full, opts
}

// htmlEscapeAndPreserveMarkdown 先 escape <>&" 防 XSS，再把 **bold** → <b>、`code` → <code>
// 对应 Telegram HTML parse_mode 支持的标签
func htmlEscapeAndPreserveMarkdown(s string) string {
	if s == "" {
		return ""
	}
	// Step 1: 完整 escape
	s = htmlEscapeText(s)
	// Step 2: 恢复 Markdown → HTML（和底层 markdownToTelegramHTML 一致）
	s = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(s, "<b>$1</b>")
	s = regexp.MustCompile(``+"`"+`(.+?)`+"`").ReplaceAllString(s, "<code>$1</code>")
	s = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(s, "<i>$1</i>")
	return s
}

// htmlEscapeText Telegram HTML parse_mode 需要的最小转义
func htmlEscapeText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

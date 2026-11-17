package service

import (
	"context"
	"fmt"
	"strconv"

	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outboundTelegram 是渠道 Telegram 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundTelegram(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {
	if s.tgIntegration == nil {
		s.tgIntegration = s.integrations.tg()
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("TG 出站账号 id 非法(account_id=%q)，无法定位 bot", accountID)))
		return
	}

	var chatID int64
	if hubMsg != nil && hubMsg.ConversationID != "" {
		chatID, _ = strconv.ParseInt(hubMsg.ConversationID, 10, 64)
	}
	if chatID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("TG 会话键无法解析为 chat_id(conversation_id=%q)", convIDOrEmpty(hubMsg))))
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
		fail(err)
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
	return
}

package service

import (
	"context"
	"fmt"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outboundFeishu 是渠道 Feishu 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundFeishu(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {
	if s.feishuIntegration == nil {
		s.feishuIntegration = s.integrations.feishu()
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("飞书出站账号 id 非法(account_id=%q)，无法定位发送账号", accountID)))
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
		fail(err)
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
	return
}

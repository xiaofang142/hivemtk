package service

import (
	"context"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outboundWechat 是渠道 Wechat 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundWechat(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {

	if s.wechatIntegration == nil {
		s.wechatIntegration = s.integrations.wechat()
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil {
		accID = 0
	}
	if _, err := s.wechatIntegration.SendCustomMessage(ctx, uint(accID), p.Sender, "text", content); err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "wechat").Str("account_id", accountID).
			Str("open_id", p.Sender).Msg("[Wechat] AI 回复出站失败")
		fail(err)
	} else {
		sent = true
	}
	return
}

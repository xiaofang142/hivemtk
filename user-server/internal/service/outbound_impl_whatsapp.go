package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outboundWhatsapp 是渠道 Whatsapp 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundWhatsapp(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {
	if s.waIntegration == nil {
		s.waIntegration = s.integrations.wa()
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("WhatsApp 出站账号 id 非法(account_id=%q)，无法定位 WABA", accountID)))
		return
	}

	s.ensureReposFromDB(ctx)
	if s.messageHubRepo != nil && hubMsg != nil && hubMsg.ConversationID != "" {
		if last, qerr := s.messageHubRepo.GetLastInboundByConversation(ctx, hubMsg.ConversationID); qerr == nil && last != nil && !last.SentAt.IsZero() {
			if time.Since(last.SentAt) > 24*time.Hour {
				// 超 24h 客服窗口：自由文本会被 Meta 拒收，降级走预审批模板兜底
				// （模板名入参数中心 channelbot 组；未配置模板时维持原失败语义）。
				tplName := whatsappFallbackTemplate(ctx)
				if tplName == "" {
					logger.Ctx(ctx).Warn().
						Str("channel", "whatsapp").Str("account_id", accountID).
						Str("to", p.Sender).
						Time("last_inbound_at", last.SentAt).
						Msg("[WhatsApp] 超出 24h 客服窗口且未配置回退模板（channelbot.whatsapp_fallback_template），AI 文本回复不可送达，标记失败")
					fail(preSendFailure(channel, CategoryWindowExpired,
						fmt.Sprintf("WhatsApp 24h 客服窗口已关闭(最后入站 %s)且未配置回退模板，自由文本不可送达",
							last.SentAt.Format("2006-01-02 15:04:05"))))
					return
				}
				logger.Ctx(ctx).Info().
					Str("channel", "whatsapp").Str("account_id", accountID).
					Str("to", p.Sender).
					Str("template", tplName).
					Msg("[WhatsApp] 超出 24h 客服窗口，AI 回复降级为模板消息发送")
				if terr := s.waIntegration.SendTemplateMessage(ctx, uint(accID), p.Sender, content, &WhatsAppTemplatePayload{
					TemplateName: tplName,
					Language:     whatsappFallbackTemplateLang(ctx),
				}); terr != nil {
					fail(terr)
				} else {
					sent = true
				}
				return
			}
		}
	}

	if err := s.waIntegration.SendMessage(ctx, uint(accID), p.Sender, content); err != nil {
		fail(err)
	} else {
		sent = true
	}
	return
}

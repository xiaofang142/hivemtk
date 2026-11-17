package service

import (
	"context"
	"fmt"
	"strconv"

	"hivemtk-user/internal/model"
)

// outboundWeCom 是渠道 WeCom 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundWeCom(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {

	if s.integration == nil {
		fail(preSendFailure(channel, CategoryBadRequest, "企微出站客户端未接线（integration 为 nil），本进程无法投递"))
		return
	}
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("企微出站账号 id 非法(account_id=%q)，无法定位发送账号", accountID)))
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
		fail(err)
	} else {
		sent = true
	}
	return
}

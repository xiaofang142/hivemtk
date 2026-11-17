package service

import (
	"context"
	"fmt"
	"strconv"

	"hivemtk-user/internal/model"
)

// outboundQQ 是渠道 QQ 的出站实现（specs/024 D1：自 sendOutbound 原样搬迁，行为不变）。
func (s *WebhookService) outboundQQ(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, fail func(error)) (sent bool) {
	accID, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || accID == 0 {
		fail(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("QQ 出站账号 id 非法(account_id=%q)，无法定位机器人", accountID)))
		return
	}
	convID := ""
	if hubMsg != nil && hubMsg.ConversationID != "" {
		convID = hubMsg.ConversationID
	}
	if convID == "" {
		fail(preSendFailure(channel, CategoryBadRequest, "QQ 出站缺少会话 id，无被动回复目标"))
		return
	}
	// 被动回复关联原消息 ID（QQ 平台 5 分钟窗口 5 条被动回复额度）。
	// 惰性单例：msg_seq 计数与 access_token 缓存必须在多次出站间保持，
	// 每次新建实例会导致 seq 恒为 1（平台按重复丢弃）+ token 重复获取。
	if s.qqIntegration == nil {
		s.qqIntegration = s.integrations.qq()
	}
	if err := s.qqIntegration.SendMessage(ctx, uint(accID), convID, QQOutboundMsgID(hubMsg), content); err != nil {
		fail(err)
	} else {
		sent = true
	}
	return
}

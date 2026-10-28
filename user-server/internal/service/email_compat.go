package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service/email"
)

// 兼容别名：email 域（发信/追踪/打开率/退订）已整体迁入 internal/service/email
// （按域拆包第二刀，OPEN_ITEMS §2，配方同 sms 首刀）。既有根包内调用点
// （auth/customer_service_plus/password_reset/proactive_reach/system_user）与
// pkg/cron 的装配继续用 service.* 这一层，不改调用面；controller/router 已直引子包。

type EmailService = email.EmailService

type EmailTrackingService = email.EmailTrackingService

type EmailOpenTrackerService = email.EmailOpenTrackerService

type EmailUnsubscribeService = email.EmailUnsubscribeService

func NewEmailService() *EmailService {
	return email.NewEmailService(NewMessageHubService())
}

func NewEmailServiceAuto() *EmailService {
	return email.NewEmailServiceAuto(NewMessageHubService())
}

func NewEmailTrackingService(repo repository.EmailTrackingRepository) *EmailTrackingService {
	return email.NewEmailTrackingService(repo)
}

func NewEmailOpenTrackerService(tracking *EmailTrackingService, repo repository.EmailTrackingRepository) *EmailOpenTrackerService {
	return email.NewEmailOpenTrackerService(tracking, repo)
}

func NewEmailUnsubscribeService(repo repository.EmailUnsubscribeRepository) *EmailUnsubscribeService {
	return email.NewEmailUnsubscribeService(repo)
}

// PushEmailOutbound 让 *MessageHubService 满足 email.OutboundHubPusher 接缝
// （接口注入，见子包 email.go）。email 出站属于「发送成功才落库」的直投渠道，
// 出站站点守卫（outbound_status_settlement_guard_test.go）按本方法认站点。
func (s *MessageHubService) PushEmailOutbound(ctx context.Context, rec email.EmailOutboundRecord) {
	_, _ = s.Push(ctx, &PushMessageRequest{
		Platform:       model.ChannelEmail,
		AccountID:      rec.AccountID,
		MsgID:          rec.MsgID,
		Direction:      "outbound",
		Status:         model.BridgeAckStatusDelivered,
		MsgType:        "text",
		ReceiverID:     rec.ReceiverID,
		Content:        rec.Content,
		ConversationID: "email-" + rec.ReceiverID,
		IsAIReply:      true,
	})
}

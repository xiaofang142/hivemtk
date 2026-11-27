package service

import (
	"context"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
)

// FA-IM-01：统一收件箱人工回复必须按平台路由到真实出站发送器，并把
// 「已受理 / 平台确认 / 送达 / 失败 / 无路由」分开返回；不能再用一条本地消息行
// 替代真实投递，也不能无条件回「回复成功」。

// 投递状态：三段正常态 + 两种明确失败态。
const (
	InboxReplyAccepted          = "accepted"           // 仅本地落库
	InboxReplyPlatformConfirmed = "platform_confirmed" // 平台已受理但无 ACK 回执
	InboxReplyDelivered         = "delivered"          // 平台确认且有回执 / web 本地 WS 已推
	InboxReplyFailed            = "failed"             // 离线 / 拒收 / 本地写失败
	InboxReplyNoRoute           = "no_route"           // 平台无出站发送器
)

// InboxReplyDelivery 是人工回复的投递事实，供 controller 原样回给前端。
type InboxReplyDelivery struct {
	Status        string `json:"delivery_status"`
	Channel       string `json:"channel,omitempty"`
	PlatformMsgID string `json:"platform_msg_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

// InboxReplyRequest 是统一出站的最小输入（controller 已做人工接管）。
// Hub 可选：外部渠道出站须传完整 Hub（会话/收件人/租户），否则出站发送器
// 无法构造可投递消息与失败轨迹；controller 从客服会话构造并注入。
type InboxReplyRequest struct {
	Platform   model.Platform
	AccountID  string
	SessionID  string
	Content    string
	AgentID    uint
	SenderName string
	Hub        *model.MessageHub
}

// 可注入 seam：默认绑真实实现；测试注入 fake 计数/可配置回执，不依赖真实 HTTP/DB。
type inboxReplyDispatchFn func(ctx context.Context, ch WebhookChannel, accountID, eventID, content string, hub *model.MessageHub) (sent bool, platformMsgID string, err error)
type inboxReplyLocalSendFn func(ctx context.Context, sessionID, content, senderID, senderName string) (*model.SessionMessage, error)

// InboxReplyOutboundService 按平台把人工回复路由到统一出站意图。
type InboxReplyOutboundService struct {
	dispatch  inboxReplyDispatchFn
	localSend inboxReplyLocalSendFn
}

func NewInboxReplyOutboundService() *InboxReplyOutboundService {
	return &InboxReplyOutboundService{
		dispatch:  defaultInboxReplyDispatch,
		localSend: defaultInboxReplyLocalSend,
	}
}

func defaultInboxReplyLocalSend(ctx context.Context, sessionID, content, senderID, senderName string) (*model.SessionMessage, error) {
	return NewCustomerSessionService().SendMessage(ctx, &SendMessageRequest{
		SessionID:     sessionID,
		CorrelationID: OutboundCorrelationFromContext(ctx),
		Content:       content,
		SenderType:    "agent",
		SenderID:      senderID,
		SenderName:    senderName,
	})
}

func defaultInboxReplyDispatch(ctx context.Context, ch WebhookChannel, accountID, eventID, content string, hub *model.MessageHub) (bool, string, error) {
	svc := NewWebhookService()
	p := &ParsedPayload{EventID: eventID, EventType: "human_reply", Content: content}
	// sendOutbound 只回 sent/error；Sent=至少一跳成功，无平台消息 ID 视为「平台确认但 ACK 丢失」。
	// B01：必须传 Hub，缺会话/收件人时出站发送器无法形成可投递消息。
	sent, err := svc.sendOutbound(ctx, ch, accountID, p, content, hub, nil)
	return sent, "", err
}

// Reply 先本地落库（受理），再按平台路由：web/web_embed 走本地 WS 即送达；
// 外部平台走注册发送器，按 sent / 回执给出平台确认或送达，失败明确返回不假装成功。
func (s *InboxReplyOutboundService) Reply(ctx context.Context, req *InboxReplyRequest) (*InboxReplyDelivery, *model.SessionMessage, error) {
	senderID := strconv.FormatUint(uint64(req.AgentID), 10)
	// 唯一 EventID，避免被 AI reply 的 ClaimReply 去重误吞人工回复。
	eventID := "human_reply:" + req.SessionID + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	correlationID := newOutboundCorrelation(WebhookChannel(req.Platform), req.AccountID, req.SessionID, eventID)
	ctx = OutboundCorrelationToContext(ctx, correlationID)
	ctx = OutboundOriginToContext(ctx, OutboundOriginHuman)
	ctx = OutboundSessionIDToContext(ctx, req.SessionID)
	msg, err := s.localSend(ctx, req.SessionID, req.Content, senderID, req.SenderName)
	if err != nil {
		return &InboxReplyDelivery{Status: InboxReplyFailed, Error: "本地消息写入失败: " + err.Error()}, nil, nil
	}
	ch, ok := mapInboxPlatformToChannel(req.Platform)
	if !ok {
		if req.Platform == model.PlatformWeb || req.Platform == model.PlatformWebEmbed {
			return &InboxReplyDelivery{Status: InboxReplyDelivered}, msg, nil
		}
		return &InboxReplyDelivery{Status: InboxReplyNoRoute, Error: "平台无对应出站发送器"}, msg, nil
	}
	// correlation/origin/session 已由 Reply 注入同一 ctx，本地与会话外发始终同身份。
	sent, platformMsgID, err := s.dispatch(ctx, ch, req.AccountID, eventID, req.Content, req.Hub)
	if err != nil {
		return &InboxReplyDelivery{Status: InboxReplyFailed, Channel: string(ch), Error: err.Error()}, msg, nil
	}
	if !sent {
		return &InboxReplyDelivery{Status: InboxReplyFailed, Channel: string(ch), Error: "平台拒收或未送达"}, msg, nil
	}
	delivery := &InboxReplyDelivery{Status: InboxReplyPlatformConfirmed, Channel: string(ch), PlatformMsgID: platformMsgID}
	if platformMsgID != "" {
		delivery.Status = InboxReplyDelivered
	}
	return delivery, msg, nil
}

// mapInboxPlatformToChannel 把客服会话平台映射到统一出站渠道。
// web/web_embed 是本地 WS 访客，无外部发送器（由调用方走本地路径）。
func mapInboxPlatformToChannel(p model.Platform) (WebhookChannel, bool) {
	switch p {
	case model.PlatformDouyin:
		return ChannelDouyin, true
	case model.PlatformKuaishou:
		return ChannelKuaishou, true
	case model.PlatformXiaohongshu:
		return ChannelXiaohongshu, true
	case model.PlatformXianyu:
		return ChannelXianyu, true
	case model.PlatformTiktok:
		return ChannelTiktok, true
	case model.PlatformWeChat:
		return ChannelWechat, true
	case model.PlatformWeb, model.PlatformWebEmbed:
		return "", false
	default:
		return "", false
	}
}

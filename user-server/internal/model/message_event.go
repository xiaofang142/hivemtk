package model

import "time"

const (
	ChannelWeb       = "web"
	ChannelTelegram  = "telegram"
	ChannelWhatsApp  = "whatsapp"
	ChannelXHS       = "xiaohongshu"
	ChannelWeCom     = "wecom"
	ChannelXianyu    = "xianyu"
	ChannelDouyin    = "douyin"
	ChannelKuaishou  = "kuaishou"
	ChannelTikTok    = "tiktok"
	ChannelSMS       = "sms"
	ChannelEmail     = "email"
	ChannelFeishu    = "feishu"
	ChannelDingTalk  = "dingtalk"
	ChannelQQ        = "qq"
	ChannelPersonal  = "personal_wx"
	ChannelInstagram = "instagram"
)

// 消息类型
const (
	MsgTypeText     = "text"
	MsgTypeImage    = "image"
	MsgTypeFile     = "file"
	MsgTypeAudio    = "audio"
	MsgTypeVideo    = "video"
	MsgTypeLink     = "link"
	MsgTypeCard     = "card"
	MsgTypeLocation = "location"
)

const (
	SenderTypeCustomer = "customer"
	SenderTypeAI       = "ai"
	SenderTypeSystem   = "system"
	SenderTypeAgent    = "agent"
)

// MessageEvent 渠道接入消息中台统一消息标准
//
// 设计要点：
//  1. 作为 InboxService.HandleIngressMessage 的入参，是渠道适配器与中台之间的协议
//  2. 包含唯一的 EventID（用于幂等去重）和 SessionID（用于会话锁定）
//  3. 不映射到数据库表，仅作为运行时事件结构在内存与 Redis 中流转
//  4. 与 model.MessageHub（持久化消息）通过 Normalize 转换：MessageEvent -> MessageHub
type MessageEvent struct {
	EventID        string                    `json:"event_id"`
	SessionID      string                    `json:"session_id"`
	Channel        string                    `json:"channel"`
	SenderID       string                    `json:"sender_id"`
	SenderName     string                    `json:"sender_name,omitempty"`
	SenderType     string                    `json:"sender_type,omitempty"`
	ReceiverID     string                    `json:"receiver_id,omitempty"`
	MsgType        string                    `json:"msg_type"`
	Content        string                    `json:"content"`
	MediaURL       string                    `json:"media_url,omitempty"`
	ConversationID string                    `json:"conversation_id,omitempty"`
	IsGroup        bool                      `json:"is_group,omitempty"`
	GroupID        string                    `json:"group_id,omitempty"`
	IsAIReply      bool                      `json:"is_ai_reply,omitempty"`
	AIAgent        string                    `json:"ai_agent,omitempty"`
	Extra          map[string]any            `json:"extra,omitempty"`
	Timestamp      time.Time                 `json:"timestamp"`
	History        []MessageEventHistoryItem `json:"history,omitempty"`
}

// MessageEventHistoryItem 多轮历史中的单轮（与扩展 HistoryItem 对齐的轻量镜像）
type MessageEventHistoryItem struct {
	EventID    string `json:"event_id,omitempty"`
	SenderType string `json:"sender_type,omitempty"`
	SenderID   string `json:"sender_id,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	ReceiverID string `json:"receiver_id,omitempty"`
	MsgType    string `json:"msg_type"`
	Content    string `json:"content"`
	MediaURL   string `json:"media_url,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Direction  string `json:"direction,omitempty"`
	IsGroup    bool   `json:"is_group,omitempty"`
	GroupID    string `json:"group_id,omitempty"`
	GroupName  string `json:"group_name,omitempty"`
}

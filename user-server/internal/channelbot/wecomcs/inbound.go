package wecomcs

import (
	"strconv"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
)

// ToInbound 把客服消息归一为统一入站结构。
//
// 只收 origin=3（客户发送）：4=系统、5=客服自己发的消息。把客服侧消息当入站
// 会形成「AI 回复自己的回复」的自激环（Slack/QQ 侧同类问题已各自加过滤）。
func (m SyncMsg) ToInbound(accountID string) *core.InboundMessage {
	if m.Origin != 3 || m.ExternalUserID == "" {
		return nil
	}
	msgType := model.MsgTypeText
	content := m.Text.Content
	switch m.MsgType {
	case "image":
		msgType = model.MsgTypeImage
		if content == "" {
			content = "[图片]"
		}
	case "voice":
		msgType = model.MsgTypeAudio
		content = "[语音]"
	case "video":
		msgType = model.MsgTypeVideo
		content = "[视频]"
	case "file":
		msgType = model.MsgTypeFile
		content = "[文件]"
	case "link":
		msgType = model.MsgTypeLink
	case "miniprogram":
		msgType = model.MsgTypeCard
		content = "[小程序]"
	case "location":
		msgType = model.MsgTypeLocation
	case "text":
	default:
		msgType = model.MsgTypeText
	}
	extra := map[string]any{"open_kfid": m.OpenKfID, "msg_type": m.MsgType}
	if m.Image.MediaID != "" {
		extra["media_id"] = m.Image.MediaID
	}
	return &core.InboundMessage{
		Platform:       model.ChannelWeComCS,
		AccountID:      accountID,
		MessageID:      m.MsgID,
		ConversationID: m.ExternalUserID,
		SenderID:       m.ExternalUserID,
		Content:        content,
		MsgType:        msgType,
		IsGroup:        false,
		Timestamp:      m.SendTime,
		Extra:          extra,
	}
}

// EventID 返回客服消息的幂等键（平台 msgid 原值，重投不变）。
func (m SyncMsg) EventID() string {
	if m.MsgID == "" {
		return ""
	}
	return model.ChannelWeComCS + "_" + m.MsgID
}

// ConversationKey 会话键（客户 external_userid）。
func (m SyncMsg) ConversationKey() string { return m.ExternalUserID }

// String 便于日志。
func (m SyncMsg) String() string {
	return model.ChannelWeComCS + ":" + m.MsgID + "@" + strconv.FormatInt(m.SendTime, 10)
}

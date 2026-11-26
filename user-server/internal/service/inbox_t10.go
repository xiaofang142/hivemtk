package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// T10 统一收件箱消息展示：自他判据、群聊只读归并、搜索/组织参数。
//
// 自他的权威来源是 message_hub.direction（服务端入站时写死），
// T03 IdentifySpeakerWithSenderName 只在服务端做 AI 触发闸门，不把昵称当身份证据下发前端。

var inboxPlatformSenderTypes = map[string]bool{
	"staff": true, "agent": true, "ai": true, "system": true,
	"self": true, "human": true, "bot": true,
}

// inboxHubToMessage 把 message_hub 行投影成统一收件箱消息 DTO。
// 新增 T10 字段（is_self / sender_avatar / sender_type / group_name）均为只读附加，
// 不改动既有字段语义，T01–T09 消费者无感。
func inboxHubToMessage(h *model.MessageHub) map[string]any {
	if h == nil {
		return map[string]any{}
	}
	senderType := inboxHubSenderType(h)
	groupName := ""
	bridge := ""
	transport := ""
	if h.Extra != nil {
		if v, ok := h.Extra["group_name"].(string); ok {
			groupName = strings.TrimSpace(v)
		}
		if v, ok := h.Extra["bridge"].(string); ok {
			bridge = v
		}
		if v, ok := h.Extra["transport"].(string); ok {
			transport = v
		}
	}
	return map[string]any{
		"id":              h.ID,
		"source":          "hub",
		"msg_id":          h.MsgID,
		"conversation_id": h.ConversationID,
		"platform":        h.Platform,
		"account_id":      h.AccountID,
		"sender_id":       h.SenderID,
		"sender_name":     h.SenderName,
		"sender_avatar":   h.SenderAvatar,
		"sender_type":     senderType,
		"receiver_id":     h.ReceiverID,
		"content":         h.Content,
		"content_type":    h.MsgType,
		"media_url":       h.MediaURL,
		"is_ai_reply":     h.IsAIReply,
		"is_read":         h.IsRead,
		// LM-16 方向/群信息直传；T10 再补 is_self 让前端不再从 sender 猜。
		"direction":       h.Direction,
		"is_group":        h.IsGroup,
		"group_id":        h.GroupID,
		"group_name":      groupName,
		"is_self":         inboxHubIsSelf(h, senderType),
		"bridge":          bridge,
		"transport":       transport,
		"sent_at":         h.SentAt,
		"created_at":      h.CreatedAt,
		"cursor":          inboxMessageCursor(h.SentAt, h.ID),
		"source_state":    inboxSourceState(nil),
		"platform_msg_id": h.PlatformMsgID,
	}
}

// inboxHubSenderType 出站行即使旧 APK 没上报 sender_type 也归一为 agent，
// 入站行默认 customer（T09 新 APK 的 Extra.sender_type 优先），system 公告保留原值。
func inboxHubSenderType(h *model.MessageHub) string {
	if h == nil {
		return ""
	}
	if h.Extra != nil {
		if v, ok := h.Extra["sender_type"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if h.Direction == "outbound" {
		if h.IsAIReply {
			return "ai"
		}
		return "agent"
	}
	return "customer"
}

// inboxHubIsSelf 平台发言（右侧）的权威判据：方向优先，标记兜底。
func inboxHubIsSelf(h *model.MessageHub, senderType string) bool {
	if h == nil {
		return false
	}
	if h.Direction == "outbound" {
		return true
	}
	if h.IsAIReply || inboxPlatformSenderTypes[strings.ToLower(strings.TrimSpace(senderType))] {
		return true
	}
	return false
}

// InboxGroupThreadQuery 群合并线程查询条件（群名 + 渠道 + 账号 + 分页）。
type InboxGroupThreadQuery struct {
	Platform  string
	AccountID string
	GroupName string
	Page      int
	PageSize  int
}

// GetGroupThread 按「渠道+账号+群名称」把同一群的所有会话键下的消息合并成一个时间流。
//
// 这是 T10 的核心读口：旧 APK 把群按人拆成多个 conversation（线上实测最多 46 人），
// 若让前端逐会话拉消息就是 46 次请求；这里一次查询完成，并按 sender_id 去重出成员列表。
// 租户/渠道/账号三重限定，缺一 fail-closed 返回空。
func (s *InboxService) GetGroupThread(ctx context.Context, q InboxGroupThreadQuery, sc InboxScope) ([]map[string]any, int64, []repository.InboxGroupMember, error) {
	if s == nil || s.hubRepo == nil || q.Platform == "" || q.AccountID == "" || strings.TrimSpace(q.GroupName) == "" {
		return []map[string]any{}, 0, []repository.InboxGroupMember{}, nil
	}
	tenantID := currentTenantID(ctx)
	// 非超管必须至少能看到该群下的一个会话，否则不返回群内容。
	if sc.restricted() {
		visible := s.inboxRepo != nil
		if visible {
			var err error
			visible, err = s.inboxRepo.HasVisibleConversationInGroup(ctx, tenantID, q.Platform, q.AccountID, q.GroupName, sc.Username)
			if err != nil {
				visible = false
			}
		}
		if !visible {
			return []map[string]any{}, 0, []repository.InboxGroupMember{}, ErrInboxForbidden
		}
	}
	convIDs, err := s.hubRepo.ResolveGroupConversationIDs(ctx, tenantID, q.Platform, q.AccountID, q.GroupName)
	if err != nil || len(convIDs) == 0 {
		return []map[string]any{}, 0, []repository.InboxGroupMember{}, err
	}
	page, pageSize := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	hubs, total, err := s.hubRepo.ListByConversationIDs(ctx, tenantID, q.Platform, q.AccountID, convIDs, page, pageSize)
	if err != nil {
		return nil, 0, nil, err
	}
	members, err := s.hubRepo.ListGroupMembers(ctx, tenantID, q.Platform, q.AccountID, convIDs)
	if err != nil {
		return nil, 0, nil, err
	}
	// 成员下拉只列「群内用户」，平台账号自身的发言不占一个成员项。
	users := make([]repository.InboxGroupMember, 0, len(members))
	for _, m := range members {
		if m.SenderID != "" && m.SenderID != q.AccountID {
			users = append(users, m)
		}
	}
	members = users
	out := make([]map[string]any, 0, len(hubs))
	for _, h := range hubs {
		out = append(out, inboxHubToMessage(h))
	}
	return out, total, members, nil
}

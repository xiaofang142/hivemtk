package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
)

// message_hub_platform_events.go 平台事件对消息行的窄写入口（specs/024 D2 / T202）。
//
// 与 message_hub_recall.go 的分工：
//   - MarkRecalled 的语义是「我方撤回自己发出的消息」，WHERE 带 direction='outbound'；
//   - 平台侧的编辑 / 撤回作用于客户发来的消息，方向是 inbound，必须有独立入口，
//     否则调用方要么绕过方向条件（放行误撤自己的回复），要么改条件（破坏出站撤回语义）。
//
// 两个方法都按 (platform, msg_id) 定位：msg_id 是入站幂等键，平台编辑/撤回事件携带的是同一个 id。

// UpdateContentByChannelMsgID 用平台编辑后的内容覆盖原消息。
// 返回 false 表示没有命中（消息未落库、渠道不匹配或已撤回）。
func (r *MessageHubRepository) UpdateContentByChannelMsgID(ctx context.Context, channel, accountID, msgID, content string) (bool, error) {
	if r == nil || r.db == nil || msgID == "" {
		return false, nil
	}
	q := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("platform = ? AND msg_id = ? AND direction = ? AND recalled_at IS NULL", channel, msgID, "inbound")
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	res := q.Updates(map[string]any{
		"content":    content,
		"updated_at": time.Now(),
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkInboundRecalledByChannelMsgID 标记客户消息已被平台撤回（终态）。
// 返回 false 表示未命中或已被撤过（重复投递按幂等命中处理）。
func (r *MessageHubRepository) MarkInboundRecalledByChannelMsgID(ctx context.Context, channel, accountID, msgID string, at time.Time, reason string) (bool, error) {
	if r == nil || r.db == nil || msgID == "" {
		return false, nil
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	q := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("platform = ? AND msg_id = ? AND direction = ? AND recalled_at IS NULL", channel, msgID, "inbound")
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	res := q.Updates(map[string]any{
		"recalled_at":  at,
		"recall_error": reason,
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

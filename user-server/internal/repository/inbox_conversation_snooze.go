package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
)

// inbox_conversation_snooze.go 「稍后处理」读写（specs/024 T601–T602）。
//
// 为什么不用 Save/FirstOrCreate：snooze 是**条件迁移**——只有当前仍是可处理状态时才允许
// 置为 snoozed。先读后写会让并发操作（同事刚关掉会话）被覆盖回 snoozed。

// SnoozeInboxConversation 把会话置为 snoozed，并记录到期时间。
//
// 返回值 ok=false 表示当前状态不允许 snooze（已 closed / 已 snoozed）。
// 重复 snooze 不算错误，但由调用方决定是否覆盖到期时间——这里选择「只有一次」，
// 因为重复点击 snooze 不应该无限延后提醒。
func (r *InboxConversationRepository) SnoozeInboxConversation(ctx context.Context, id uint, until time.Time) (bool, error) {
	if r == nil || r.db == nil || id == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("id = ? AND status IN ?", id, []string{
			model.InboxStatusUnread, model.InboxStatusOpen, model.InboxStatusAssigned,
		}).
		Updates(map[string]any{
			"status":        model.InboxStatusSnoozed,
			"snoozed_until": until,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UnsnoozeInboxConversation 手动取消稍后处理（回到 open）。
func (r *InboxConversationRepository) UnsnoozeInboxConversation(ctx context.Context, id uint) (bool, error) {
	if r == nil || r.db == nil || id == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("id = ? AND status = ?", id, model.InboxStatusSnoozed).
		Updates(map[string]any{
			"status":        model.InboxStatusOpen,
			"snoozed_until": nil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ReopenDueSnoozed 把已到期的 snoozed 会话改回 open，返回影响行数。
//
// 条件更新（不是「先查再改」）：并发两次扫描同一批行时，第二次的 WHERE 已经命中不到
// status='snoozed'，天然不会重复重置（验收要求「并发两次只重置一次」）。
func (r *InboxConversationRepository) ReopenDueSnoozed(ctx context.Context, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("status = ? AND snoozed_until IS NOT NULL AND snoozed_until <= ?", model.InboxStatusSnoozed, now).
		Updates(map[string]any{
			"status":        model.InboxStatusOpen,
			"snoozed_until": nil,
		})
	return res.RowsAffected, res.Error
}

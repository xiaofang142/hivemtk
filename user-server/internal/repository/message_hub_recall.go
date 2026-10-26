package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// message_hub_recall.go B2：撤回状态格的写入口。
//
// 这里只做"把结论记下来"，出网动作在 service 层通过 ReachRecaller 端口完成
// （仓储不许自带 HTTP 客户端——那会让"记一笔撤回"变成不可复算的行为）。
//
// 两条方法的条件更新都是乐观锁：并发两次撤回只会有一笔把 recalled_at 从 NULL 改掉，
// 另一笔的 RowsAffected 为 0，调用方据此判定"别人已经撤过了"，不再出网。

// MarkRecalled 把一行出站消息置为已撤回（终态）。
//
// 条件：id 命中 + direction='outbound' + recalled_at IS NULL。
// 返回 (是否真的写了这一行)：false 表示这一行已经撤过（并发/重复请求），
// 调用方应把本次视为幂等命中而不是失败。
func (r *MessageHubRepository) MarkRecalled(ctx context.Context, id uint, at time.Time) (bool, error) {
	if r == nil || r.db == nil || id == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ? AND direction = ? AND recalled_at IS NULL", id, "outbound").
		Updates(map[string]any{
			"recalled_at":  at,
			"recall_error": "",
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkRecallFailed 记下最近一次撤回失败的原因（不改 recalled_at）。
//
// 失败不是终态：平台窗口过期就永远失败，但网络抖动十分钟后可以再试，
// 所以这里只写原因，让下一次请求能重新出网。
func (r *MessageHubRepository) MarkRecallFailed(ctx context.Context, id uint, reason string) error {
	if r == nil || r.db == nil || id == 0 {
		return nil
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ? AND direction = ? AND recalled_at IS NULL", id, "outbound").
		Update("recall_error", reason).Error
}

// GetByIDForRecall 取撤回所需的最小行（含 tenant 过滤由调用方 context 承担）。
// 单独一个方法而不是复用 GetByID：撤回要判 direction/platform/msg_id/recalled_at，
// 读全列会把 content/extra 这些大字段一起拉出来，而它们对撤回没有任何用。
func (r *MessageHubRepository) GetByIDForRecall(ctx context.Context, id uint) (*model.MessageHub, error) {
	if r == nil || r.db == nil || id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var row model.MessageHub
	err := r.db.WithContext(ctx).
		Select("id", "tenant_id", "platform", "msg_id", "direction", "account_id",
			"conversation_id", "platform_msg_id", "status", "sent_at",
			"recalled_at", "recall_error").
		Where("id = ?", id).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

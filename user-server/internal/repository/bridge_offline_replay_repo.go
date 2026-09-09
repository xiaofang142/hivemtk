// bridge_offline_replay_repo.go 桥接离线回扫仓储（五层 L5）
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// BridgeOfflineReplayRepository 离线渠道检测与延迟出站重放查询收口
type BridgeOfflineReplayRepository struct {
	db *gorm.DB
}

// NewBridgeOfflineReplayRepository 构造
func NewBridgeOfflineReplayRepository(db *gorm.DB) *BridgeOfflineReplayRepository {
	return &BridgeOfflineReplayRepository{db: db}
}

// OfflineChannelStat bridge_metrics 按渠道聚合的最近活跃时间
type OfflineChannelStat struct {
	Platform  string    `gorm:"column:platform"`
	AccountID string    `gorm:"column:account_id"`
	LastSeen  time.Time `gorm:"column:last_seen"`
}

// OfflineAccountRow bridge_accounts 非 online 渠道快照
type OfflineAccountRow struct {
	Platform  string    `gorm:"column:platform"`
	AccountID string    `gorm:"column:account_id"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// GroupBridgeMetricsLastSeen bridge_metrics 按 (platform, account_id) 聚合最近更新时间
func (r *BridgeOfflineReplayRepository) GroupBridgeMetricsLastSeen(ctx context.Context) ([]OfflineChannelStat, error) {
	if r.db == nil {
		return nil, nil
	}
	var stats []OfflineChannelStat
	err := r.db.WithContext(ctx).
		Table("bridge_metrics").
		Select("platform, account_id, MAX(updated_at) as last_seen").
		Group("platform, account_id").
		Scan(&stats).Error
	return stats, err
}

// ListNonOnlineBridgeAccounts bridge_accounts 中 status != online 的渠道
func (r *BridgeOfflineReplayRepository) ListNonOnlineBridgeAccounts(ctx context.Context) ([]OfflineAccountRow, error) {
	if r.db == nil {
		return nil, nil
	}
	var accs []OfflineAccountRow
	err := r.db.WithContext(ctx).
		Table("bridge_accounts").
		Select("platform, account_id, updated_at").
		Where("status != ?", "online").
		Scan(&accs).Error
	return accs, err
}

// DelayedOutboundRow reach_delayed_outbound 待重放消息行
type DelayedOutboundRow struct {
	ID             uint64 `gorm:"column:id"`
	Platform       string `gorm:"column:platform"`
	AccountID      string `gorm:"column:account_id"`
	ConversationID string `gorm:"column:conversation_id"`
	SenderID       string `gorm:"column:sender_id"`
	ReceiverID     string `gorm:"column:receiver_id"`
	MsgType        string `gorm:"column:msg_type"`
	Content        string `gorm:"column:content"`
	EventID        string `gorm:"column:event_id"`
	RetryCount     int    `gorm:"column:retry_count"`
}

// ListPendingDelayedOutbound 取指定渠道 pending 的延迟出站消息
func (r *BridgeOfflineReplayRepository) ListPendingDelayedOutbound(ctx context.Context, platform, accountID string, limit int) ([]DelayedOutboundRow, error) {
	if r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).
		Table("reach_delayed_outbound").
		Where("platform = ? AND account_id = ? AND status = ?", platform, accountID, "pending").
		Order("send_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var msgs []DelayedOutboundRow
	err := q.Scan(&msgs).Error
	return msgs, err
}

// MarkDelayedOutboundReplayFailed 重放失败：累计 retry 并记错误
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayFailed(ctx context.Context, id uint64, lastErr string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Exec(
		"UPDATE reach_delayed_outbound SET retry_count = retry_count + 1, last_error = ?, status = ? WHERE id = ?",
		lastErr, "replay_failed", id,
	).Error
}

// MarkDelayedOutboundReplayed 重放成功：置 replayed 状态
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayed(ctx context.Context, id uint64) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Exec(
		"UPDATE reach_delayed_outbound SET status = ?, replayed_at = NOW(), retry_count = retry_count + 1 WHERE id = ?",
		"replayed", id,
	).Error
}

// bridge_offline_replay_repo.go 桥接离线回扫仓储（五层 L5）
package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BridgeOfflineReplayRepository 离线渠道检测与延迟出站重放查询收口
type BridgeOfflineReplayRepository struct {
	db *gorm.DB
}

// NewBridgeOfflineReplayRepository 构造
// NewBridgeOfflineReplayRepository 构造（无参，内部取全局 DB；对齐 customer_session.go 先例）
func NewBridgeOfflineReplayRepository() *BridgeOfflineReplayRepository {
	return NewBridgeOfflineReplayRepositoryWithDB(_db.GetDB())
}

// NewBridgeOfflineReplayRepositoryWithDB 显式注入 DB（测试/装配用）
func NewBridgeOfflineReplayRepositoryWithDB(db *gorm.DB) *BridgeOfflineReplayRepository {
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
//
// 列集合必须与 model.DelayedOutboundReply 一致：表里没有 receiver_id / msg_type /
// event_id / retry_count / replayed_at 这五列，结构体宣称有只会让 gorm 的 SELECT *
// 静默把字段留成零值，重放带着空 msg_type 出站且无人报警。
type DelayedOutboundRow struct {
	ID             uint          `gorm:"column:id"`
	Platform       string        `gorm:"column:platform"`
	AccountID      string        `gorm:"column:account_id"`
	ConversationID string        `gorm:"column:conversation_id"`
	SenderID       string        `gorm:"column:sender_id"`
	Content        string        `gorm:"column:content"`
	Kind           string        `gorm:"column:kind"`
	Cards          model.JSONMap `gorm:"column:cards"`
	Attempts       int           `gorm:"column:attempts"`
}

// ListPendingDelayedOutbound 取指定渠道已到期的 pending 延迟出站消息
//
// 必须带 send_at <= now：quiet_hours 行的 send_at 就是窗口开放时刻，
// 不看它等于在客户免打扰时段把回复推出去。
func (r *BridgeOfflineReplayRepository) ListPendingDelayedOutbound(ctx context.Context, platform, accountID string, limit int) ([]DelayedOutboundRow, error) {
	if r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Select("id, platform, account_id, conversation_id, sender_id, content, kind, cards, attempts").
		Where("platform = ? AND account_id = ? AND status = ? AND send_at <= ?",
			platform, accountID, model.DelayedStatusPending, time.Now()).
		Order("send_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var msgs []DelayedOutboundRow
	err := q.Scan(&msgs).Error
	return msgs, err
}

// ClaimDelayedOutboundForReplay 抢占重放权：pending → sending，返回是否拿到入场券。
//
// H-3 主链路 drain 的是同一张表的同一批到期 pending 行，两边都直接投递就是
// 一次周期里把同一份内容发给客户两次；只有把这条 CAS 的 RowsAffected 当门票才互斥。
func (r *BridgeOfflineReplayRepository) ClaimDelayedOutboundForReplay(ctx context.Context, id uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ? AND status = ?", id, model.DelayedStatusPending).
		Update("status", model.DelayedStatusSending)
	return res.RowsAffected == 1, res.Error
}

// MarkDelayedOutboundReplayFailed 重放失败：回到 pending 待下一轮，累计 attempts 并记错误
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayFailed(ctx context.Context, id uint, lastErr string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.DelayedStatusPending,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": truncateForColumn(lastErr, 1024),
		}).Error
}

// MarkDelayedOutboundReplayed 重放成功：收口到 sent
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayed(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  model.DelayedStatusSent,
			"sent_at": time.Now(),
		}).Error
}

// MarkDelayedOutboundAbandoned 重投次数用尽：收口到终态 failed，不再被回扫取到
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundAbandoned(ctx context.Context, id uint, reason string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.DelayedStatusFailed,
			"sent_at":    time.Now(),
			"last_error": truncateForColumn(reason, 1024),
		}).Error
}

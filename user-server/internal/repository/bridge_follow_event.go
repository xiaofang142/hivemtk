// bridge_follow_event.go —— 新用户（非关注好友）事件/自动关注动作审计的数据访问（M-6）。
package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BridgeFollowEventRepository bridge_follow_events 数据访问接口。
type BridgeFollowEventRepository interface {
	// InsertIdempotent 幂等插入（dedup_key 唯一索引冲突 = 已登记，静默跳过）。
	// 返回是否真的新插入了一行（false = 重复事件被幂等吞掉）。
	InsertIdempotent(ctx context.Context, event *model.BridgeFollowEvent) (bool, error)
	// GetByDedupKey 按幂等键取行（nil = 不存在）。
	GetByDedupKey(ctx context.Context, dedupKey string) (*model.BridgeFollowEvent, error)
	// ListRecent 最近 N 条（运营观测/排查用，时间倒序）。
	ListRecent(ctx context.Context, limit int) ([]*model.BridgeFollowEvent, error)
	// CountByEventTypeSince 统计窗口内某事件类型的行数（运营统计口径）。
	CountByEventTypeSince(ctx context.Context, eventType string, since time.Time) (int64, error)
}

type bridgeFollowEventRepository struct{}

// NewBridgeFollowEventRepository 创建 bridge_follow_events 仓储。
func NewBridgeFollowEventRepository() BridgeFollowEventRepository {
	return &bridgeFollowEventRepository{}
}

func (r *bridgeFollowEventRepository) InsertIdempotent(ctx context.Context, event *model.BridgeFollowEvent) (bool, error) {
	if event == nil || event.DedupKey == "" {
		return false, nil
	}
	// dedup_key 唯一索引 + OnConflict DoNothing：并发首触只有一行落库；
	// RowsAffected==0 即幂等跳过（既有事件已登记，不报错——fail-open 口径）。
	res := dbFromCtx(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *bridgeFollowEventRepository) GetByDedupKey(ctx context.Context, dedupKey string) (*model.BridgeFollowEvent, error) {
	if dedupKey == "" {
		return nil, nil
	}
	var ev model.BridgeFollowEvent
	err := dbFromCtx(ctx).Where("dedup_key = ?", dedupKey).First(&ev).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ev, nil
}

func (r *bridgeFollowEventRepository) ListRecent(ctx context.Context, limit int) ([]*model.BridgeFollowEvent, error) {
	var events []*model.BridgeFollowEvent
	query := dbFromCtx(ctx).Model(&model.BridgeFollowEvent{}).Order("id DESC")
	query = applyListLimit(query, limit)
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (r *bridgeFollowEventRepository) CountByEventTypeSince(ctx context.Context, eventType string, since time.Time) (int64, error) {
	var count int64
	q := dbFromCtx(ctx).Model(&model.BridgeFollowEvent{}).Where("event_type = ?", eventType)
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelInteractionRepository 交互上下文（specs/024 D8 / T208）。
type ChannelInteractionRepository struct {
	db *gorm.DB
}

func NewChannelInteractionRepository(db *gorm.DB) *ChannelInteractionRepository {
	return &ChannelInteractionRepository{db: db}
}

func NewChannelInteractionRepositoryDefault() *ChannelInteractionRepository {
	return &ChannelInteractionRepository{db: _db.GetDB()}
}

// CreateIdempotent 幂等登记一次交互。返回 written=false 表示平台重投（行已存在）。
func (r *ChannelInteractionRepository) CreateIdempotent(ctx context.Context, row *model.ChannelInteraction) (bool, error) {
	if r == nil || r.db == nil || row == nil || row.InteractionID == "" {
		return false, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkAnswered 把 pending 置为 answered；重复答复返回 false（幂等命中，不报错）。
func (r *ChannelInteractionRepository) MarkAnswered(ctx context.Context, channel, interactionID string, at time.Time) (bool, error) {
	if r == nil || r.db == nil || interactionID == "" {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.ChannelInteraction{}).
		Where("channel = ? AND interaction_id = ? AND status = ?", channel, interactionID, model.ChannelInteractionPending).
		Updates(map[string]any{"status": model.ChannelInteractionAnswered, "answered_at": at})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkExpired 把到期仍未答复的行置为 expired，返回影响行数（供清理 worker 打点）。
func (r *ChannelInteractionRepository) MarkExpired(ctx context.Context, before time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.ChannelInteraction{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", model.ChannelInteractionPending, before).
		Update("status", model.ChannelInteractionExpired)
	return res.RowsAffected, res.Error
}

// GetByInteractionID 查一条交互（未命中返回 nil, nil，便于调用方区分「没有」与「出错」）。
func (r *ChannelInteractionRepository) GetByInteractionID(ctx context.Context, channel, interactionID string) (*model.ChannelInteraction, error) {
	if r == nil || r.db == nil || interactionID == "" {
		return nil, nil
	}
	var row model.ChannelInteraction
	err := r.db.WithContext(ctx).Where("channel = ? AND interaction_id = ?", channel, interactionID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

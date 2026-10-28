// contact_consent_repo.go 客户同意事件与偏好仓储（五层 L5）
package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ContactConsentRepository 同意事件与偏好数据访问
type ContactConsentRepository struct {
	db *gorm.DB
}

// NewContactConsentRepository 构造
func NewContactConsentRepository(db *gorm.DB) *ContactConsentRepository {
	return &ContactConsentRepository{db: db}
}

// AppendEvent 追加一条同意/撤回事实（只 INSERT，不 UPDATE 历史）。
func (r *ContactConsentRepository) AppendEvent(ctx context.Context, ev *model.ContactConsentEvent) error {
	if r == nil || r.db == nil {
		return errors.New("contact consent repository not initialized")
	}
	if ev == nil {
		return errors.New("contact consent: nil 事件不是合法写入")
	}
	return r.db.WithContext(ctx).Create(ev).Error
}

// LatestEvent 取 (oneID, purpose) 在渠道范围（channel 精确行或全渠道行）内最新一条事实。
// 排序 (occurred_at DESC, id DESC)：同一时刻后写入的事实赢。
func (r *ContactConsentRepository) LatestEvent(ctx context.Context, oneID, purpose, channel string) (*model.ContactConsentEvent, error) {
	if r.db == nil {
		return nil, nil
	}
	var ev model.ContactConsentEvent
	q := r.db.WithContext(ctx).
		Where("one_id = ? AND purpose = ?", oneID, purpose).
		Where("channel_scope IN ?", []string{channel, ""})
	if channel == "" {
		// 查询本身就是全渠道维度：只看全渠道行
		q = r.db.WithContext(ctx).
			Where("one_id = ? AND purpose = ? AND channel_scope = ?", oneID, purpose, "")
	}
	err := q.Order("occurred_at DESC, id DESC").First(&ev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// ListEventsByOneID 客户授权时间线（客户 360 / 管理页）
func (r *ContactConsentRepository) ListEventsByOneID(ctx context.Context, oneID string, limit int) ([]model.ContactConsentEvent, error) {
	if r.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.ContactConsentEvent
	err := r.db.WithContext(ctx).
		Where("one_id = ?", oneID).
		Order("occurred_at DESC, id DESC").Limit(limit).
		Find(&rows).Error
	return rows, err
}

// UpsertPreference 幂等写偏好快照，唯一键 (tenant_id, one_id, purpose, channel)。
func (r *ContactConsentRepository) UpsertPreference(ctx context.Context, pref *model.ContactPreference) error {
	if r == nil || r.db == nil {
		return errors.New("contact consent repository not initialized")
	}
	if pref == nil {
		return errors.New("contact consent: nil 偏好不是合法写入")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "one_id"}, {Name: "purpose"}, {Name: "channel"}},
		DoUpdates: clause.AssignmentColumns([]string{"paused", "paused_until", "timezone", "updated_at"}),
	}).Create(pref).Error
}

// GetPreference 读偏好：渠道精确行优先，其次全渠道行；都无返回 nil。
func (r *ContactConsentRepository) GetPreference(ctx context.Context, oneID, purpose, channel string) (*model.ContactPreference, error) {
	if r.db == nil {
		return nil, nil
	}
	var exact model.ContactPreference
	err := r.db.WithContext(ctx).
		Where("one_id = ? AND purpose = ? AND channel = ?", oneID, purpose, channel).
		First(&exact).Error
	if err == nil {
		return &exact, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if channel == "" {
		return nil, nil
	}
	var general model.ContactPreference
	err = r.db.WithContext(ctx).
		Where("one_id = ? AND purpose = ? AND channel = ?", oneID, purpose, "").
		First(&general).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &general, nil
}

// ListPreferencesByOneID 客户全部偏好（管理页）
func (r *ContactConsentRepository) ListPreferencesByOneID(ctx context.Context, oneID string) ([]model.ContactPreference, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.ContactPreference
	err := r.db.WithContext(ctx).
		Where("one_id = ?", oneID).
		Order("purpose ASC, channel ASC").
		Find(&rows).Error
	return rows, err
}

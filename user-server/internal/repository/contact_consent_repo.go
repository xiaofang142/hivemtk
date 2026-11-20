// contact_consent_repo.go 客户同意事件与偏好仓储（五层 L5）
package repository

import (
	"context"
	"errors"
	"sort"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

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

// LinkedOneIDs 遍历租户内的身份连通组；去重使链式、反向关联不会循环。
// 使用 GORM 查询，任一步失败均返回错误，不能降级为原单一身份。
func (r *ContactConsentRepository) LinkedOneIDs(ctx context.Context, oneID string) ([]string, error) {
	database, err := r.scopedDB(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{oneID: true}
	frontier := []string{oneID}
	for len(frontier) > 0 {
		var links []model.ContactConsentIdentityLink
		if err := database.Where("left_one_id IN ? OR right_one_id IN ?", frontier, frontier).Find(&links).Error; err != nil {
			return nil, err
		}
		frontier = nil
		for _, link := range links {
			for _, id := range []string{link.LeftOneID, link.RightOneID} {
				if !seen[id] {
					seen[id] = true
					frontier = append(frontier, id)
				}
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (r *ContactConsentRepository) scopedDB(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("contact consent repository not initialized")
	}
	id := tenant.WriteTenantID(ctx)
	if id == 0 {
		return nil, errors.New("contact consent: tenant context required")
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", id).Session(&gorm.Session{}), nil
}

// AppendEvent 追加一条同意/撤回事实（只 INSERT，不 UPDATE 历史）。
func (r *ContactConsentRepository) AppendEvent(ctx context.Context, ev *model.ContactConsentEvent) error {
	if ev == nil {
		return errors.New("contact consent: nil 事件不是合法写入")
	}
	database, err := r.scopedDB(ctx)
	if err != nil {
		return err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if ev.TenantID != 0 && ev.TenantID != tenantID {
		return errors.New("contact consent: event tenant mismatch")
	}
	ev.TenantID = tenantID
	return database.Create(ev).Error
}

// LatestEvent 取 (oneID, purpose) 在渠道范围（channel 精确行或全渠道行）内最新一条事实。
// 排序 (occurred_at DESC, id DESC)：同一时刻后写入的事实赢。
func (r *ContactConsentRepository) LatestEvent(ctx context.Context, oneID, purpose, channel string) (*model.ContactConsentEvent, error) {
	database, err := r.scopedDB(ctx)
	if err != nil {
		return nil, err
	}
	var ev model.ContactConsentEvent
	q := database.
		Where("one_id = ? AND purpose = ?", oneID, purpose).
		Where("channel_scope IN ?", []string{channel, ""})
	if channel == "" {
		// 查询本身就是全渠道维度：只看全渠道行
		q = database.
			Where("one_id = ? AND purpose = ? AND channel_scope = ?", oneID, purpose, "")
	}
	err = q.Order("occurred_at DESC, id DESC").First(&ev).Error
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
	ids, err := r.LinkedOneIDs(ctx, oneID)
	if err != nil {
		return nil, err
	}
	database, err := r.scopedDB(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.ContactConsentEvent
	err = database.
		Where("one_id IN ?", ids).
		Order("occurred_at DESC, id DESC").Limit(limit).
		Find(&rows).Error
	return rows, err
}

// UpsertPreference 幂等写偏好快照，唯一键 (tenant_id, one_id, purpose, channel)。
func (r *ContactConsentRepository) UpsertPreference(ctx context.Context, pref *model.ContactPreference) error {
	if pref == nil {
		return errors.New("contact consent: nil 偏好不是合法写入")
	}
	database, err := r.scopedDB(ctx)
	if err != nil {
		return err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if pref.TenantID != 0 && pref.TenantID != tenantID {
		return errors.New("contact consent: preference tenant mismatch")
	}
	pref.TenantID = tenantID
	return database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "one_id"}, {Name: "purpose"}, {Name: "channel"}},
		DoUpdates: clause.AssignmentColumns([]string{"paused", "paused_until", "timezone", "updated_at"}),
	}).Create(pref).Error
}

// GetPreference 读偏好：渠道精确行优先，其次全渠道行；都无返回 nil。
func (r *ContactConsentRepository) GetPreference(ctx context.Context, oneID, purpose, channel string) (*model.ContactPreference, error) {
	database, err := r.scopedDB(ctx)
	if err != nil {
		return nil, err
	}
	var exact model.ContactPreference
	err = database.
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
	err = database.
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
	ids, err := r.LinkedOneIDs(ctx, oneID)
	if err != nil {
		return nil, err
	}
	database, err := r.scopedDB(ctx)
	if err != nil {
		return nil, err
	}
	var rows []model.ContactPreference
	err = database.
		Where("one_id IN ?", ids).
		Order("purpose ASC, channel ASC, one_id ASC").
		Find(&rows).Error
	return rows, err
}

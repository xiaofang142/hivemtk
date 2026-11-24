package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ErrWebhookDeliveryStoreUnavailable 表示 delivery 仓储未装配，调用方必须可见失败。
var ErrWebhookDeliveryStoreUnavailable = errors.New("webhook delivery repository unavailable")

// WebhookDeliveryClaim 是一次 replay claim 的行版本 token；UpdatedAt 同时是 lease 时间戳。
type WebhookDeliveryClaim struct {
	ID        uint
	TenantID  uint
	Attempts  int
	UpdatedAt time.Time
}

// Token 由 claim 行版本派生，旧 worker 无法用它完成更新后的行。
func (c WebhookDeliveryClaim) Token() string {
	return fmt.Sprintf("%d:%d:%d:%d", c.ID, c.TenantID, c.Attempts, c.UpdatedAt.UnixNano())
}

// WebhookDeliveryRepository 收口 hook delivery 的持久化、候选、CAS claim 与条件完成。
type WebhookDeliveryRepository interface {
	Create(ctx context.Context, delivery *model.WebhookDelivery) error
	List(ctx context.Context, status string, limit int) ([]model.WebhookDelivery, error)
	ListReplayCandidates(ctx context.Context, tenantID uint, maxAttempts int, staleBefore time.Time, limit int) ([]model.WebhookDelivery, error)
	ClaimReplay(ctx context.Context, id, tenantID uint, maxAttempts int, now time.Time, lease time.Duration) (*WebhookDeliveryClaim, error)
	CompleteReplay(ctx context.Context, claim WebhookDeliveryClaim, status string, responseCode int, deliveryErr error, completedAt time.Time) (bool, error)
}

type webhookDeliveryRepository struct {
	db *gorm.DB
}

func NewWebhookDeliveryRepository() WebhookDeliveryRepository {
	return NewWebhookDeliveryRepositoryWithDB(_db.GetDB())
}

func NewWebhookDeliveryRepositoryWithDB(database *gorm.DB) WebhookDeliveryRepository {
	return &webhookDeliveryRepository{db: database}
}

func (r *webhookDeliveryRepository) ready() error {
	if r == nil || r.db == nil {
		return ErrWebhookDeliveryStoreUnavailable
	}
	return nil
}

func (r *webhookDeliveryRepository) Create(ctx context.Context, delivery *model.WebhookDelivery) error {
	if err := r.ready(); err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("webhook delivery is nil")
	}
	return r.db.WithContext(ctx).Create(delivery).Error
}

func (r *webhookDeliveryRepository) List(ctx context.Context, status string, limit int) ([]model.WebhookDelivery, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.WebhookDelivery{}).Order("id DESC").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []model.WebhookDelivery
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *webhookDeliveryRepository) ListReplayCandidates(ctx context.Context, tenantID uint, maxAttempts int, staleBefore time.Time, limit int) ([]model.WebhookDelivery, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("attempts < ?", maxAttempts).
		Where("(status = ? OR (status = ? AND updated_at < ?))", "dead_letter", "replaying", staleBefore).
		Order("updated_at ASC").Order("id ASC").Limit(limit)
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var rows []model.WebhookDelivery
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *webhookDeliveryRepository) ClaimReplay(ctx context.Context, id, tenantID uint, maxAttempts int, now time.Time, lease time.Duration) (*WebhookDeliveryClaim, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errors.New("webhook delivery id required")
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if lease <= 0 {
		lease = 10 * time.Second
	}
	q := r.db.WithContext(ctx).Model(&model.WebhookDelivery{}).
		Where("id = ? AND attempts < ?", id, maxAttempts).
		Where("(status = ? OR (status = ? AND updated_at < ?))", "dead_letter", "replaying", now.Add(-lease))
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	result := q.Updates(map[string]any{
		"status":     "replaying",
		"attempts":   gorm.Expr("attempts + 1"),
		"updated_at": now,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, nil
	}
	var row model.WebhookDelivery
	read := r.db.WithContext(ctx).Where("id = ?", id)
	if tenantID > 0 {
		read = read.Where("tenant_id = ?", tenantID)
	}
	if err := read.First(&row).Error; err != nil {
		return nil, err
	}
	return &WebhookDeliveryClaim{ID: row.ID, TenantID: row.TenantID, Attempts: row.Attempts, UpdatedAt: row.UpdatedAt}, nil
}

func (r *webhookDeliveryRepository) CompleteReplay(ctx context.Context, claim WebhookDeliveryClaim, status string, responseCode int, deliveryErr error, completedAt time.Time) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	if claim.ID == 0 {
		return false, errors.New("webhook delivery claim id required")
	}
	updates := map[string]any{
		"status":        status,
		"response_code": responseCode,
		"updated_at":    completedAt,
	}
	if deliveryErr != nil {
		updates["last_error"] = deliveryErr.Error()
	} else {
		updates["last_error"] = ""
	}
	if status == "success" {
		updates["delivered_at"] = completedAt
	}
	result := r.db.WithContext(ctx).Model(&model.WebhookDelivery{}).
		Where("id = ? AND tenant_id = ?", claim.ID, claim.TenantID).
		Where("status = ? AND attempts = ? AND updated_at = ?", "replaying", claim.Attempts, claim.UpdatedAt).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

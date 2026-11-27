package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

const (
	leadNotifyClaimLease = 5 * time.Minute
	leadNotifyMaxBatch   = 100
)

var ErrLeadNotifyStoreUnavailable = errors.New("lead notify repository not initialized")

// LeadNotifyRepository 收口 item2 的耐久投递意图与独立送达历史。
type LeadNotifyRepository struct {
	db *gorm.DB
}

func NewLeadNotifyRepository(db *gorm.DB) *LeadNotifyRepository {
	if db == nil {
		db = _db.GetDB()
	}
	return &LeadNotifyRepository{db: db}
}

func (r *LeadNotifyRepository) ready() error {
	if r == nil || r.db == nil {
		return ErrLeadNotifyStoreUnavailable
	}
	return nil
}

func leadNotifyTenantID(ctx context.Context) (uint, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return 0, ErrLeadTenantRequired
		}
		return tenant.DefaultTenantID, nil
	}
	return tid, nil
}

func leadNotifyTenantQuery(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	if tenant.IsSystemScope(ctx) {
		return db.WithContext(ctx), nil
	}
	tid, err := leadNotifyTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return db.WithContext(ctx).Where("tenant_id = ?", tid), nil
}

func ensureLeadNotifyTenantScope(ctx context.Context, tenantID uint) error {
	if tenantID == 0 {
		return errors.New("lead notify outbox: tenant required")
	}
	if tenant.IsSystemScope(ctx) {
		return nil
	}
	tid, err := leadNotifyTenantID(ctx)
	if err != nil {
		return err
	}
	if tid != tenantID {
		return errors.New("lead notify outbox: tenant scope mismatch")
	}
	return nil
}

// Enqueue 幂等写入一条投递意图；重复键返回 existing + created=false。
func (r *LeadNotifyRepository) Enqueue(ctx context.Context, item *model.LeadNotifyOutbox) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	if item == nil || item.TenantID == 0 || item.IdempotencyKey == "" {
		return false, errors.New("lead notify outbox: tenant/idempotency key required")
	}
	if !tenant.IsSystemScope(ctx) {
		tid, err := leadNotifyTenantID(ctx)
		if err != nil {
			return false, err
		}
		if item.TenantID != tid {
			return false, errors.New("lead notify outbox: tenant mismatch")
		}
	}
	if item.Status == "" {
		item.Status = model.LeadNotifyStatusPending
	}
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = model.LeadNotifyDefaultMaxAttempts
	}
	if item.NextAttemptAt.IsZero() {
		item.NextAttemptAt = time.Now().UTC()
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(item)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	q, err := leadNotifyTenantQuery(ctx, r.db)
	if err != nil {
		return false, err
	}
	var existing model.LeadNotifyOutbox
	if err := q.Where("tenant_id = ? AND idempotency_key = ?", item.TenantID, item.IdempotencyKey).First(&existing).Error; err != nil {
		return false, err
	}
	*item = existing
	return false, nil
}

func (r *LeadNotifyRepository) claimOne(tx *gorm.DB, row *model.LeadNotifyOutbox, now time.Time) error {
	token := uuid.NewString()
	leaseUntil := now.Add(leadNotifyClaimLease)
	res := tx.Model(&model.LeadNotifyOutbox{}).
		Where("id = ? AND status IN ?", row.ID, []string{model.LeadNotifyStatusPending, model.LeadNotifyStatusRetry}).
		Updates(map[string]any{
			"status":      model.LeadNotifyStatusSending,
			"claim_token": token,
			"lease_until": leaseUntil,
			"attempts":    gorm.Expr("attempts + 1"),
		})
	if err := _db.EnsureAffected(res); err != nil {
		return err
	}
	row.Status = model.LeadNotifyStatusSending
	row.ClaimToken = token
	row.LeaseUntil = &leaseUntil
	row.Attempts++
	return nil
}

// ClaimDueForUpdate 以 SKIP LOCKED 认领到期 pending/retry，逐行签发 claim token 与租约。
func (r *LeadNotifyRepository) ClaimDueForUpdate(ctx context.Context, now time.Time, limit int) ([]*model.LeadNotifyOutbox, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > leadNotifyMaxBatch {
		limit = leadNotifyMaxBatch
	}
	var picked []*model.LeadNotifyOutbox
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q, err := leadNotifyTenantQuery(ctx, tx)
		if err != nil {
			return err
		}
		if err := q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND next_attempt_at <= ?", []string{model.LeadNotifyStatusPending, model.LeadNotifyStatusRetry}, now).
			Order("next_attempt_at ASC, id ASC").Limit(limit).Find(&picked).Error; err != nil {
			return err
		}
		for i := range picked {
			if err := r.claimOne(tx, picked[i], now); err != nil {
				return err
			}
		}
		return nil
	})
	return picked, err
}

// ClaimByIDForDelivery 即时路径与后台 worker 共享同一认领/栅栏原语。
func (r *LeadNotifyRepository) ClaimByIDForDelivery(ctx context.Context, id uint64, now time.Time) (*model.LeadNotifyOutbox, bool, error) {
	if err := r.ready(); err != nil {
		return nil, false, err
	}
	var picked *model.LeadNotifyOutbox
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q, err := leadNotifyTenantQuery(ctx, tx)
		if err != nil {
			return err
		}
		var row model.LeadNotifyOutbox
		if err := q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND status = ?", id, model.LeadNotifyStatusPending).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if err := r.claimOne(tx, &row, now); err != nil {
			return err
		}
		picked = &row
		return nil
	})
	if err != nil || picked == nil {
		return nil, false, err
	}
	return picked, true, nil
}

func (r *LeadNotifyRepository) fencedUpdate(ctx context.Context, id uint64, token string, updates map[string]any) error {
	q, err := leadNotifyTenantQuery(ctx, r.db)
	if err != nil {
		return err
	}
	res := q.Model(&model.LeadNotifyOutbox{}).
		Where("id = ? AND status = ? AND claim_token = ?", id, model.LeadNotifyStatusSending, token).
		Updates(updates)
	return _db.EnsureAffected(res)
}

func (r *LeadNotifyRepository) MarkDelivered(ctx context.Context, id uint64, token string, at time.Time) error {
	if err := r.ready(); err != nil {
		return err
	}
	return r.fencedUpdate(ctx, id, token, map[string]any{
		"status": model.LeadNotifyStatusDelivered, "delivered_at": at, "lease_until": nil, "claim_token": "",
	})
}

func (r *LeadNotifyRepository) ScheduleRetry(ctx context.Context, id uint64, token string, nextAt time.Time, lastError string) error {
	if err := r.ready(); err != nil {
		return err
	}
	return r.fencedUpdate(ctx, id, token, map[string]any{
		"status":          gorm.Expr("CASE WHEN attempts >= max_attempts THEN 'exhausted' ELSE 'retry' END"),
		"next_attempt_at": nextAt,
		"last_error":      truncateForColumn(lastError, 4096),
		"lease_until":     nil,
		"claim_token":     "",
	})
}

func (r *LeadNotifyRepository) MarkExhausted(ctx context.Context, id uint64, token, lastError string) error {
	if err := r.ready(); err != nil {
		return err
	}
	return r.fencedUpdate(ctx, id, token, map[string]any{
		"status": model.LeadNotifyStatusExhausted, "last_error": truncateForColumn(lastError, 4096),
		"lease_until": nil, "claim_token": "",
	})
}

func (r *LeadNotifyRepository) MarkReceiptUnknown(ctx context.Context, id uint64, token, reason string) error {
	if err := r.ready(); err != nil {
		return err
	}
	return r.fencedUpdate(ctx, id, token, map[string]any{
		"status": model.LeadNotifyStatusReceiptUnknown, "last_error": truncateForColumn(reason, 4096),
		"lease_until": nil, "claim_token": "",
	})
}

// ReclaimStaleSending 将过期 sending 转 receipt_unknown，避免崩溃后自动重复业务副作用。
func (r *LeadNotifyRepository) ReclaimStaleSending(ctx context.Context, olderThan time.Time) (int64, error) {
	if err := r.ready(); err != nil {
		return 0, err
	}
	q, err := leadNotifyTenantQuery(ctx, r.db)
	if err != nil {
		return 0, err
	}
	res := q.Model(&model.LeadNotifyOutbox{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", model.LeadNotifyStatusSending, olderThan).
		Updates(map[string]any{
			"status": model.LeadNotifyStatusReceiptUnknown, "claim_token": "", "lease_until": nil,
			"last_error": "stale claim reclaimed; manual retry required",
		})
	return res.RowsAffected, res.Error
}

func (r *LeadNotifyRepository) RetryManually(ctx context.Context, id uint64) error {
	if err := r.ready(); err != nil {
		return err
	}
	q, err := leadNotifyTenantQuery(ctx, r.db)
	if err != nil {
		return err
	}
	res := q.Model(&model.LeadNotifyOutbox{}).
		Where("id = ? AND status IN ?", id, []string{model.LeadNotifyStatusRetry, model.LeadNotifyStatusExhausted, model.LeadNotifyStatusReceiptUnknown}).
		Updates(map[string]any{
			"status": model.LeadNotifyStatusPending, "attempts": 0, "next_attempt_at": time.Now().UTC(),
			"last_error": "", "claim_token": "", "lease_until": nil,
		})
	return _db.EnsureAffected(res)
}

func (r *LeadNotifyRepository) Cancel(ctx context.Context, id uint64) error {
	if err := r.ready(); err != nil {
		return err
	}
	q, err := leadNotifyTenantQuery(ctx, r.db)
	if err != nil {
		return err
	}
	res := q.Model(&model.LeadNotifyOutbox{}).
		Where("id = ? AND status IN ?", id, []string{model.LeadNotifyStatusPending, model.LeadNotifyStatusRetry, model.LeadNotifyStatusExhausted, model.LeadNotifyStatusReceiptUnknown}).
		Update("status", model.LeadNotifyStatusCancelled)
	return _db.EnsureAffected(res)
}

// RecordDelivery 以 delivery_key 幂等追加/刷新一条独立历史。
func (r *LeadNotifyRepository) RecordDelivery(ctx context.Context, record *model.LeadNotifyDelivery) error {
	if err := r.ready(); err != nil {
		return err
	}
	if record == nil || record.TenantID == 0 || record.DeliveryKey == "" {
		return errors.New("lead notify delivery: tenant/delivery key required")
	}
	if !tenant.IsSystemScope(ctx) {
		tid, err := leadNotifyTenantID(ctx)
		if err != nil {
			return err
		}
		if record.TenantID != tid {
			return errors.New("lead notify delivery: tenant mismatch")
		}
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "delivery_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"outbox_id", "clue_id", "event_id", "customer_id", "recipient", "channel", "route_id",
			"route_scope", "success", "status", "error", "mask_policy", "attempt",
		}),
	}).Create(record).Error
}

func (r *LeadNotifyRepository) deliveryQuery(ctx context.Context, query model.LeadNotifyDeliveryQuery) (*gorm.DB, error) {
	q, err := leadNotifyScoped(ctx, r.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	q = q.Model(&model.LeadNotifyDelivery{})
	if query.ClueID != "" {
		q = q.Where("clue_id = ?", query.ClueID)
	}
	if query.EventID != "" {
		q = q.Where("event_id = ?", query.EventID)
	}
	if query.RouteID != "" {
		q = q.Where("route_id = ?", query.RouteID)
	}
	if query.Channel != "" {
		q = q.Where("channel = ?", query.Channel)
	}
	return q, nil
}

func (r *LeadNotifyRepository) ListDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]*model.LeadNotifyDelivery, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q, err := r.deliveryQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var rows []*model.LeadNotifyDelivery
	err = q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *LeadNotifyRepository) SummarizeDeliveries(ctx context.Context, query model.LeadNotifyDeliveryQuery) ([]model.LeadNotifyDeliverySummary, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q, err := r.deliveryQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	var rows []model.LeadNotifyDeliverySummary
	err = q.Select("status, COUNT(*) AS total, COUNT(*) FILTER (WHERE success) AS succeeded, COUNT(*) FILTER (WHERE NOT success) AS failed").
		Group("status").Scan(&rows).Error
	return rows, err
}

func (r *LeadNotifyRepository) ListOutboxByEvent(ctx context.Context, tenantID uint, eventID string) ([]*model.LeadNotifyOutbox, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if tenantID == 0 || eventID == "" {
		return nil, errors.New("lead notify outbox: tenant/event required")
	}
	if err := ensureLeadNotifyTenantScope(ctx, tenantID); err != nil {
		return nil, err
	}
	var rows []*model.LeadNotifyOutbox
	err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).
		Where("tenant_id = ? AND event_id = ?", tenantID, eventID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *LeadNotifyRepository) GetOutbox(ctx context.Context, tenantID uint, id uint64) (*model.LeadNotifyOutbox, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if err := ensureLeadNotifyTenantScope(ctx, tenantID); err != nil {
		return nil, err
	}
	var row model.LeadNotifyOutbox
	if err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Where("tenant_id = ? AND id = ?", tenantID, id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// LeadNotifyUserRepository 是通知收件人授权的窄查询边界，只读取当前租户的 system_users。
type LeadNotifyUserRepository struct {
	db *gorm.DB
}

func NewLeadNotifyUserRepository(db *gorm.DB) *LeadNotifyUserRepository {
	if db == nil {
		db = _db.GetDB()
	}
	return &LeadNotifyUserRepository{db: db}
}

func (r *LeadNotifyUserRepository) GetByID(ctx context.Context, id uint) (*model.SystemUser, error) {
	if r == nil || r.db == nil || id == 0 {
		return nil, ErrLeadNotifyStoreUnavailable
	}
	tid, err := leadNotifyTenantID(ctx)
	if err != nil {
		return nil, err
	}
	var user model.SystemUser
	if err := r.db.WithContext(tenant.OnlyTenant(ctx, tid)).Where("tenant_id = ? AND id = ?", tid, id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

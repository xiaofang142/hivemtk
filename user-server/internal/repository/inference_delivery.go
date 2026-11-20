package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InferenceDeliveryRepository interface {
	Put(context.Context, *model.InferenceDelivery) (*model.InferenceDelivery, error)
	Get(context.Context, string) (*model.InferenceDelivery, error)
	Claim(context.Context, string, string, time.Time, time.Time) (*model.InferenceDelivery, bool, error)
	MarkSend(context.Context, string, string) error
	Finish(context.Context, string, string, string, string, string, *time.Time, *time.Time) error
}

type inferenceDeliveryRepo struct{ db *gorm.DB }

func NewInferenceDeliveryRepository(db *gorm.DB) InferenceDeliveryRepository {
	return &inferenceDeliveryRepo{db}
}

// scoped 即使单租户模式也要求显式身份，系统作用域不能绕过这条账本约束。
func (r *inferenceDeliveryRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, 0, delivery.ErrTenant
	}
	if r == nil || r.db == nil {
		return nil, 0, delivery.ErrUnavailable
	}
	return r.db.WithContext(tenant.OnlyTenant(ctx, id)), id, nil
}

func (r *inferenceDeliveryRepo) Put(ctx context.Context, row *model.InferenceDelivery) (*model.InferenceDelivery, error) {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Key == "" || row.Fingerprint == "" || row.TenantID != id {
		return nil, delivery.ErrIdentity
	}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "key"}}, DoNothing: true}).Create(row).Error; err != nil {
		return nil, err
	}
	existing, err := r.Get(ctx, row.Key)
	if err != nil {
		return nil, err
	}
	if existing.Fingerprint != row.Fingerprint || existing.IntentJSON != row.IntentJSON {
		return nil, delivery.ErrConflict
	}
	return existing, nil
}

func (r *inferenceDeliveryRepo) Get(ctx context.Context, key string) (*model.InferenceDelivery, error) {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var row model.InferenceDelivery
	if err = db.Where("tenant_id = ? AND key = ?", id, key).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, delivery.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *inferenceDeliveryRepo) Claim(ctx context.Context, key, token string, now, until time.Time) (*model.InferenceDelivery, bool, error) {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return nil, false, err
	}
	if token == "" || now.IsZero() || !until.After(now) {
		return nil, false, delivery.ErrIdentity
	}
	var row model.InferenceDelivery
	claimed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND key = ?", id, key).First(&row).Error; err != nil {
			return err
		}
		if row.State == string(delivery.Confirmed) || row.State == string(delivery.Rejected) || (row.LeaseUntil != nil && row.LeaseUntil.After(now)) {
			return nil
		}
		result := tx.Model(&model.InferenceDelivery{}).Where("tenant_id = ? AND key = ?", id, key).Updates(map[string]any{"state": string(delivery.Sending), "lease_token": token, "lease_until": until})
		claimed = result.RowsAffected == 1
		return result.Error
	})
	return &row, claimed, err
}

func (r *inferenceDeliveryRepo) MarkSend(ctx context.Context, key, token string) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	res := db.Model(&model.InferenceDelivery{}).Where("tenant_id = ? AND key = ? AND lease_token = ? AND state = ?", id, key, token, string(delivery.Sending)).UpdateColumn("send_attempts", gorm.Expr("send_attempts + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return delivery.ErrLease
	}
	return nil
}

func (r *inferenceDeliveryRepo) Finish(ctx context.Context, key, token, state, code, receipt string, accepted, confirmed *time.Time) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	res := db.Model(&model.InferenceDelivery{}).Where("tenant_id = ? AND key = ? AND lease_token = ? AND state = ?", id, key, token, string(delivery.Sending)).Updates(map[string]any{"state": state, "error_code": code, "receipt_id": receipt, "accepted_at": accepted, "confirmed_at": confirmed, "lease_token": "", "lease_until": nil})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return delivery.ErrLease
	}
	return nil
}

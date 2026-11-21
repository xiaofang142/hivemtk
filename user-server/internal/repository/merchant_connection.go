package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

var (
	ErrMerchantConnectionStorage = errors.New("merchant connection store unavailable")
	ErrMerchantConnectionStale   = errors.New("merchant connection attempt invalid or superseded")
)

type MerchantConnectionRepository interface {
	FindStatus(ctx context.Context, provider, shop string) (model.MerchantConnection, error)
	Begin(ctx context.Context, attempt model.MerchantConnection) error
	Claim(ctx context.Context, provider, shop, digest, contract string, actor uint) (model.MerchantConnection, error)
	Finish(ctx context.Context, claim model.MerchantConnection, grant *model.MerchantConnection) error
	ClaimRefresh(ctx context.Context, row model.MerchantConnection, timeout time.Duration) error
	FinishRefresh(ctx context.Context, claim model.MerchantConnection, grant *model.MerchantConnection) error
	Revoke(ctx context.Context, provider, shop string) error
}

type merchantConnectionRepository struct{ db *gorm.DB }

func NewMerchantConnectionRepository(db *gorm.DB) MerchantConnectionRepository {
	return &merchantConnectionRepository{db: db}
}

func (r *merchantConnectionRepository) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || r.db == nil {
		return nil, 0, ErrMerchantConnectionStorage
	}
	// Even the SQL error logger must not record state digests or encrypted secrets.
	db := r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(tenant.OnlyTenant(ctx, id))
	return db, id, nil
}

func merchantConnectionTarget(db *gorm.DB, tenantID uint, provider, shop string) *gorm.DB {
	return db.Model(&model.MerchantConnection{}).Where("tenant_id = ? AND provider = ? AND shop_id = ?", tenantID, provider, shop)
}

func (r *merchantConnectionRepository) FindStatus(ctx context.Context, provider, shop string) (model.MerchantConnection, error) {
	var row model.MerchantConnection
	db, id, err := r.scoped(ctx)
	if err != nil {
		return row, err
	}
	err = merchantConnectionTarget(db, id, provider, shop).
		Select("id", "provider", "shop_id", "version", "attempt_contract", "attempt_expires_at", "attempt_failed",
			"credential_version", "consent_version", "credential_contract", "credential_ciphertext", "scopes", "expires_at", "refresh_until", "refresh_failed", "revoked").Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.MerchantConnection{}, ErrMerchantConnectionStorage
	}
	return row, nil
}

func (r *merchantConnectionRepository) Begin(ctx context.Context, attempt model.MerchantConnection) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	attempt.TenantID, attempt.Version = id, 1
	update := clause.AssignmentColumns([]string{"attempt_actor", "attempt_contract", "attempt_expires_at", "state_digest", "verifier_ciphertext", "attempt_failed", "updated_at"})
	update = append(update, clause.Assignment{Column: clause.Column{Name: "version"}, Value: gorm.Expr("merchant_connections.version + 1")})
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "provider"}, {Name: "shop_id"}},
		DoUpdates: update,
	}).Create(&attempt).Error
	if err != nil {
		return ErrMerchantConnectionStorage
	}
	return nil
}

func (r *merchantConnectionRepository) Claim(ctx context.Context, provider, shop, digest, contract string, actor uint) (model.MerchantConnection, error) {
	var claim model.MerchantConnection
	db, id, err := r.scoped(ctx)
	if err != nil {
		return claim, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		q := merchantConnectionTarget(tx, id, provider, shop)
		if err := q.Session(&gorm.Session{}).Select("id", "version", "verifier_ciphertext").
			Where("state_digest = ? AND attempt_actor = ? AND attempt_contract = ? AND attempt_expires_at > clock_timestamp()", digest, actor, contract).
			Clauses(clause.Locking{Strength: "UPDATE"}).Take(&claim).Error; err != nil {
			return err
		}
		result := q.Where("id = ? AND version = ? AND attempt_expires_at > clock_timestamp()", claim.ID, claim.Version).
			Updates(map[string]any{"state_digest": "", "verifier_ciphertext": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMerchantConnectionStale
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrMerchantConnectionStale) {
		return model.MerchantConnection{}, ErrMerchantConnectionStale
	}
	if err != nil {
		return model.MerchantConnection{}, ErrMerchantConnectionStorage
	}
	claim.Provider, claim.ShopID = provider, shop
	return claim, nil
}

func (r *merchantConnectionRepository) Finish(ctx context.Context, claim model.MerchantConnection, grant *model.MerchantConnection) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	updates := map[string]any{"attempt_expires_at": nil, "attempt_failed": grant == nil}
	if grant != nil {
		updates["consent_version"] = claim.Version
		if err := merchantConnectionGrantUpdates(updates, claim.Version, grant); err != nil {
			return err
		}
	}
	return merchantConnectionCAS(merchantConnectionTarget(db, id, claim.Provider, claim.ShopID).
		Where("id = ? AND version = ? AND state_digest = '' AND attempt_expires_at > clock_timestamp()", claim.ID, claim.Version).Updates(updates))
}

func merchantConnectionGrantUpdates(updates map[string]any, version int64, grant *model.MerchantConnection) error {
	scopes, err := json.Marshal(grant.Scopes)
	if err != nil {
		return ErrMerchantConnectionStorage
	}
	updates["credential_version"] = version
	updates["credential_contract"] = grant.CredentialContract
	updates["credential_ciphertext"] = grant.CredentialCiphertext
	updates["scopes"] = string(scopes)
	updates["expires_at"] = grant.ExpiresAt
	updates["refresh_until"] = nil
	updates["refresh_failed"] = false
	updates["revoked"] = false
	return nil
}

// ClaimRefresh retires the stored secret before the provider can receive it. A crash after
// commit leaves a deadline for status recovery and nothing another process can exchange.
func (r *merchantConnectionRepository) ClaimRefresh(ctx context.Context, row model.MerchantConnection, timeout time.Duration) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	return merchantConnectionCAS(merchantConnectionTarget(db, id, row.Provider, row.ShopID).
		Where("id = ? AND version = ? AND credential_version = ? AND credential_contract = ?", row.ID, row.Version, row.CredentialVersion, row.CredentialContract).
		Where("NOT revoked AND NOT attempt_failed AND attempt_expires_at IS NULL AND refresh_until IS NULL AND NOT refresh_failed").
		Where("credential_ciphertext = ?", row.CredentialCiphertext).
		Updates(map[string]any{"credential_ciphertext": "", "refresh_until": gorm.Expr("clock_timestamp() + (? * interval '1 second')", timeout.Seconds())}))
}

func (r *merchantConnectionRepository) FinishRefresh(ctx context.Context, claim model.MerchantConnection, grant *model.MerchantConnection) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	q := merchantConnectionTarget(db, id, claim.Provider, claim.ShopID).
		Where("id = ? AND version = ? AND credential_version = ? AND NOT revoked AND refresh_until IS NOT NULL", claim.ID, claim.Version, claim.CredentialVersion)
	updates := map[string]any{"refresh_until": nil, "refresh_failed": grant == nil}
	if grant != nil {
		q = q.Where("refresh_until > clock_timestamp()")
		updates["version"] = claim.Version + 1
		if err := merchantConnectionGrantUpdates(updates, claim.Version+1, grant); err != nil {
			return err
		}
	}
	return merchantConnectionCAS(q.Updates(updates))
}

func (r *merchantConnectionRepository) Revoke(ctx context.Context, provider, shop string) error {
	db, id, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	res := merchantConnectionTarget(db, id, provider, shop).Where("NOT revoked OR attempt_expires_at IS NOT NULL OR attempt_failed").Updates(map[string]any{
		"version": gorm.Expr("version + 1"), "revoked": true,
		"attempt_actor": 0, "attempt_contract": "", "attempt_expires_at": nil, "attempt_failed": false, "state_digest": "", "verifier_ciphertext": "",
		"credential_version": 0, "consent_version": 0, "credential_contract": "", "credential_ciphertext": "", "scopes": nil, "expires_at": nil,
		"refresh_until": nil, "refresh_failed": false,
	})
	if res.Error != nil {
		return ErrMerchantConnectionStorage
	}
	return nil
}

func merchantConnectionCAS(res *gorm.DB) error {
	if res.Error != nil {
		return ErrMerchantConnectionStorage
	}
	if res.RowsAffected != 1 {
		return ErrMerchantConnectionStale
	}
	return nil
}

package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	appdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func init() {
	appdb.RegisterExtraModels(&model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
}

type CapabilityEvidenceRepository interface {
	capregistry.CapabilityEvidenceReader
}

type capabilityEvidenceRepository struct {
	db   *gorm.DB
	lock bool
}

// NewCapabilityEvidenceRepository builds a reader for one database handle.
func NewCapabilityEvidenceRepository(db *gorm.DB) CapabilityEvidenceRepository {
	return &capabilityEvidenceRepository{db: db}
}

// DefaultCapabilityEvidenceRepository binds the currently initialized application database.
// Callers invoke this per decision, including when startup has not installed the handle yet.
func DefaultCapabilityEvidenceRepository() CapabilityEvidenceRepository {
	return NewCapabilityEvidenceRepository(appdb.GetDB())
}

func (r *capabilityEvidenceRepository) scope(ctx context.Context, queryTenant uint) (*gorm.DB, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id != queryTenant || tenant.IsSystemScope(ctx) {
		return nil, errors.New("capability evidence requires matching tenant scope")
	}
	if r.db == nil {
		return nil, ErrDBNotReady
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", id), nil
}

func (r *capabilityEvidenceRepository) read(q *gorm.DB, lock bool, dest any) error {
	if r.lock && lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	err := q.Take(dest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func (r *capabilityEvidenceRepository) FindCapabilityBinding(ctx context.Context, tenantID uint, source capregistry.CapabilitySource, lock bool) (model.CapabilitySourceBinding, error) {
	var row model.CapabilitySourceBinding
	q, err := r.scope(ctx, tenantID)
	if err != nil {
		return row, err
	}
	err = r.read(q.Where("source = ? AND product = ? AND resource_id = ?", source.Kind, source.Product, source.ResourceID), lock, &row)
	return row, err
}

func (r *capabilityEvidenceRepository) FindCapabilityConnection(ctx context.Context, key capregistry.CellKey) (model.MerchantConnection, error) {
	var row model.MerchantConnection
	q, err := r.scope(ctx, key.TenantID)
	if err != nil {
		return row, err
	}
	// Project only fields needed to decide. Ciphertext never leaves this repository.
	q = q.Model(&model.MerchantConnection{}).
		Select("id", "tenant_id", "provider", "shop_id", "version", "credential_version", "consent_version", "credential_contract", "revoked", "scopes", "expires_at", "refresh_until", "refresh_failed", "attempt_expires_at", "attempt_failed").
		Where("provider = ? AND shop_id = ?", key.Product, key.ShopID)
	err = r.read(q, true, &row)
	return row, err
}

func (r *capabilityEvidenceRepository) FindCapabilityEvidence(ctx context.Context, key capregistry.CellKey) (model.CapabilityEvidence, error) {
	var row model.CapabilityEvidence
	q, err := r.scope(ctx, key.TenantID)
	if err != nil {
		return row, err
	}
	err = r.read(q.Where("shop_id = ? AND product = ? AND region = ? AND action = ? AND carrier = ?", key.ShopID, key.Product, key.Region, key.Action, key.Carrier), true, &row)
	return row, err
}

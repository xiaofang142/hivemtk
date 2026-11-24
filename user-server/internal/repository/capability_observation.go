package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCapabilityObservationConflict = errors.New("capability observation no longer matches current connection")

type CapabilityObservationRepository interface {
	capregistry.CapabilityEvidenceReader
	StoreCapabilityObservation(context.Context, model.CapabilitySourceBinding, model.CapabilityEvidence, string) error
}

func NewCapabilityObservationRepository(db *gorm.DB) CapabilityObservationRepository {
	return &capabilityEvidenceRepository{db: db}
}

// StoreCapabilityObservation 同事务保存来源和证据，采集期间的撤权或轮换使旧响应失效。
func (r *capabilityEvidenceRepository) StoreCapabilityObservation(ctx context.Context, binding model.CapabilitySourceBinding, evidence model.CapabilityEvidence, contract string) error {
	if _, err := r.scope(ctx, evidence.TenantID); err != nil {
		return err
	}
	id, _ := tenant.FromContext(ctx)
	if binding.TenantID != id || binding.Product != evidence.Product || binding.ShopID != evidence.ShopID || binding.Region != evidence.Region ||
		binding.ConnectionID != evidence.ConnectionID || binding.ConsentVersion != evidence.ConsentVersion || binding.Revoked || binding.Source == "" || binding.ResourceID == "" || contract == "" {
		return ErrCapabilityObservationConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var connection model.MerchantConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND provider = ? AND shop_id = ?", id, evidence.ConnectionID, evidence.Product, evidence.ShopID).Take(&connection).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if connection.Revoked || connection.Version <= 0 || connection.CredentialVersion <= 0 || connection.ConsentVersion <= 0 ||
			connection.Version != evidence.ConnectionVersion || connection.CredentialVersion != evidence.CredentialVersion ||
			connection.ConsentVersion != evidence.ConsentVersion || connection.CredentialContract != contract || connection.ExpiresAt == nil || !now.Before(*connection.ExpiresAt) ||
			connection.AttemptExpiresAt != nil || connection.AttemptFailed || connection.RefreshUntil != nil || connection.RefreshFailed || !now.Before(evidence.ExpiresAt) {
			return ErrCapabilityObservationConflict
		}
		var existing model.CapabilitySourceBinding
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND source = ? AND product = ? AND resource_id = ?", id, binding.Source, binding.Product, binding.ResourceID).Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing.ID != 0 && (existing.ShopID != binding.ShopID || existing.Region != binding.Region || existing.ConnectionID != binding.ConnectionID || existing.Revoked) {
			return ErrCapabilityObservationConflict
		}
		binding.ID = 0
		bindingConflict := clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "source"}, {Name: "product"}, {Name: "resource_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"consent_version"}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Eq{Column: clause.Column{Table: "capability_source_bindings", Name: "shop_id"}, Value: binding.ShopID},
				clause.Eq{Column: clause.Column{Table: "capability_source_bindings", Name: "region"}, Value: binding.Region},
				clause.Eq{Column: clause.Column{Table: "capability_source_bindings", Name: "connection_id"}, Value: binding.ConnectionID},
				clause.Eq{Column: clause.Column{Table: "capability_source_bindings", Name: "revoked"}, Value: false},
			}},
		}
		result := tx.Clauses(bindingConflict).Create(&binding)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCapabilityObservationConflict
		}
		evidence.ID = 0
		simulated := evidence.Simulated
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "shop_id"}, {Name: "product"}, {Name: "region"}, {Name: "action"}, {Name: "carrier"}}, DoUpdates: clause.AssignmentColumns([]string{"connection_id", "connection_version", "credential_version", "consent_version", "contract_version", "documentation_version", "scopes", "state_limits", "expires_at", "documentation", "implementation", "provenance", "runtime_proof_ref", "simulated"})}).Create(&evidence).Error; err != nil {
			return err
		}
		// GORM 的 default:true 会替换零值 false，须在同一事务中恢复采集器的显式标记。
		return tx.Model(&model.CapabilityEvidence{}).
			Where("tenant_id = ? AND shop_id = ? AND product = ? AND region = ? AND action = ? AND carrier = ?", id, evidence.ShopID, evidence.Product, evidence.Region, evidence.Action, evidence.Carrier).
			Update("simulated", simulated).Error
	})
}

package migrations

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type MerchantCallbackInboxMigration struct{ db *gorm.DB }

func NewMerchantCallbackInboxMigration(db *gorm.DB) *MerchantCallbackInboxMigration {
	return &MerchantCallbackInboxMigration{db: db}
}

func (*MerchantCallbackInboxMigration) Version() string { return "v3.105.0" }
func (*MerchantCallbackInboxMigration) Name() string    { return "商家回调收件箱分页补偿" }
func (*MerchantCallbackInboxMigration) Description() string {
	return "为回调事件建立资源顺序水位和可恢复分页高水位，保留并整理既有收件箱记录"
}

func (m *MerchantCallbackInboxMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("merchant callback inbox migration requires a database")
	}
	db := m.db.WithContext(tenant.WithSystemScope(ctx))
	if err := db.AutoMigrate(&model.MerchantCallbackEvent{}, &model.MerchantCallbackResourceWatermark{}, &model.MerchantCallbackCompensationCursor{}); err != nil {
		return fmt.Errorf("migrate merchant callback inbox recovery: %w", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return backfillMerchantCallbackResourceWatermarks(tx)
	}); err != nil {
		return fmt.Errorf("backfill merchant callback resource watermarks: %w", err)
	}
	return nil
}

// Backfill rows in stable resource/version order. Re-running the migration is safe:
// each resource watermark remains at the lexicographically greatest (signed_at, id).
func backfillMerchantCallbackResourceWatermarks(tx *gorm.DB) error {
	// Materialize each page before issuing writes on the transaction connection.
	for offset := 0; ; offset += 256 {
		var events []model.MerchantCallbackEvent
		if err := tx.Model(&model.MerchantCallbackEvent{}).
			Select("id", "tenant_id", "provider", "subscription_id", "consent_version", "connection_version", "credential_version", "kind", "resource_id", "signed_at", "disposition").
			Order("tenant_id ASC, provider ASC, subscription_id ASC, kind ASC, resource_id ASC, signed_at ASC, id ASC").
			Limit(256).Offset(offset).Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			var head model.MerchantCallbackResourceWatermark
			err := tx.Where("tenant_id = ? AND provider = ? AND subscription_id = ? AND kind = ? AND resource_id = ?",
				event.TenantID, event.Provider, event.SubscriptionID, event.Kind, event.ResourceID).Take(&head).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				head = model.MerchantCallbackResourceWatermark{TenantID: event.TenantID, Provider: event.Provider,
					SubscriptionID: event.SubscriptionID, Kind: event.Kind, ResourceID: event.ResourceID, EventID: event.ID,
					SignedAt: event.SignedAt, ConsentVersion: event.ConsentVersion, ConnectionVersion: event.ConnectionVersion,
					CredentialVersion: event.CredentialVersion}
				if err := tx.Create(&head).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			if head.EventID == event.ID {
				continue
			}
			newer := event.SignedAt.After(head.SignedAt) || (event.SignedAt.Equal(head.SignedAt) && event.ID > head.EventID)
			if !newer {
				if event.Disposition != model.MerchantCallbackDispositionApplied {
					if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND id = ? AND disposition <> ?", event.TenantID, event.ID, model.MerchantCallbackDispositionApplied).
						Updates(map[string]any{"disposition": model.MerchantCallbackDispositionSuperseded, "superseded_by_id": head.EventID}).Error; err != nil {
						return err
					}
				}
				continue
			}
			if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND id = ? AND disposition = ?", event.TenantID, head.EventID, model.MerchantCallbackDispositionPending).
				Updates(map[string]any{"disposition": model.MerchantCallbackDispositionSuperseded, "superseded_by_id": event.ID}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.MerchantCallbackResourceWatermark{}).Where("tenant_id = ? AND id = ?", event.TenantID, head.ID).
				Updates(map[string]any{"event_id": event.ID, "signed_at": event.SignedAt, "consent_version": event.ConsentVersion,
					"connection_version": event.ConnectionVersion, "credential_version": event.CredentialVersion}).Error; err != nil {
				return err
			}
			head.EventID, head.SignedAt, head.ConsentVersion = event.ID, event.SignedAt, event.ConsentVersion
			head.ConnectionVersion, head.CredentialVersion = event.ConnectionVersion, event.CredentialVersion
		}
	}
}

// Keep evidence and authorization fences across rollbacks.
func (*MerchantCallbackInboxMigration) Down(context.Context) error { return nil }

var _ migration.Migration = (*MerchantCallbackInboxMigration)(nil)

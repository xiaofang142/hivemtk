package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

var (
	ErrMerchantCallbackBinding   = errors.New("merchant callback binding is not current")
	ErrMerchantCallbackConflict  = errors.New("merchant callback identity conflicts with the stored event")
	ErrMerchantCallbackStorage   = errors.New("merchant callback persistence unavailable")
	ErrMerchantCallbackPageStale = errors.New("merchant callback compensation page is stale or incomplete")
)

type MerchantCallbackCompensationPage struct {
	TenantID          uint
	SubscriptionID    uint
	ConnectionID      uint
	ConnectionVersion int64
	CredentialVersion int64
	ConsentVersion    int64
	HighWaterID       uint
	AfterEventID      uint
	PageEndID         uint
	PageLimit         int
	Events            []model.MerchantCallbackEvent
	Final             bool
}

func (p MerchantCallbackCompensationPage) EventIDs() []uint {
	ids := make([]uint, 0, len(p.Events))
	for _, event := range p.Events {
		ids = append(ids, event.ID)
	}
	return ids
}

type MerchantCallbackRepository interface {
	LookupSubscription(ctx context.Context, provider, key string) (model.MerchantCallbackSubscription, error)
	Record(ctx context.Context, bind model.MerchantCallbackSubscription, connection model.MerchantConnection, event model.MerchantCallbackEvent) (uint, bool, error)
	ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (MerchantCallbackCompensationPage, error)
	AcknowledgeCompensationPage(ctx context.Context, page MerchantCallbackCompensationPage, appliedEventIDs []uint) error
}

type merchantCallbackRepository struct{ db *gorm.DB }

func NewMerchantCallbackRepository(db *gorm.DB) MerchantCallbackRepository {
	return &merchantCallbackRepository{db: db}
}

func (r *merchantCallbackRepository) LookupSubscription(ctx context.Context, provider, key string) (model.MerchantCallbackSubscription, error) {
	var bind model.MerchantCallbackSubscription
	if provider == "" || key == "" {
		return bind, ErrMerchantCallbackBinding
	}
	if r.db == nil {
		return bind, ErrMerchantCallbackStorage
	}
	// Sole tenant-bootstrap operation: one enabled row using the global unique key.
	// The verified adapter supplies both predicates. All later queries are tenant scoped.
	ctx = tenant.WithSystemScope(tenant.OnlyTenant(ctx, 0))
	err := r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(ctx).
		Where("provider = ? AND subscription_key = ? AND enabled", provider, key).Take(&bind).Error
	return bind, merchantCallbackStoreError(err)
}

func (r *merchantCallbackRepository) Record(ctx context.Context, bind model.MerchantCallbackSubscription, connection model.MerchantConnection, event model.MerchantCallbackEvent) (uint, bool, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || id != bind.TenantID || id != event.TenantID || !bind.Enabled || bind.ID == 0 ||
		event.SubscriptionID != bind.ID || event.Provider != bind.Provider || event.ShopID != bind.ShopID ||
		event.ConsentVersion != bind.ConsentVersion || event.Scope != bind.RequiredScope || event.Kind != bind.EventKind || event.VerifierVersion != bind.VerifierVersion ||
		event.ConnectionVersion != connection.Version || event.CredentialVersion != connection.CredentialVersion {
		return 0, false, ErrMerchantCallbackBinding
	}
	if r.db == nil {
		return 0, false, ErrMerchantCallbackStorage
	}
	var eventID uint
	var duplicate bool
	db := r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(tenant.OnlyTenant(ctx, id))
	err := db.Transaction(func(tx *gorm.DB) error {
		// Match the already authenticated credential snapshot, then pin both rows through commit.
		// Connection precedes subscription in lock order; refresh, reconnect and revoke update it.
		err := lockMerchantCallbackBinding(tx, bind, connection, event.ValidUntil)
		if err != nil {
			return err
		}
		eventID, duplicate, err = insertMerchantCallbackEvent(tx, event)
		if err != nil || duplicate {
			return err
		}
		event.ID = eventID
		return recordMerchantCallbackResourceHead(tx, event)

	})
	if err != nil {
		return 0, false, merchantCallbackStoreError(err)
	}
	return eventID, duplicate, nil
}

func lockMerchantCallbackBinding(tx *gorm.DB, bind model.MerchantCallbackSubscription, snapshot model.MerchantConnection, validUntil time.Time) error {
	var row model.MerchantConnection
	err := merchantConnectionTarget(tx, bind.TenantID, bind.Provider, bind.ShopID).
		Select("id", "version", "credential_version", "credential_ciphertext", "scopes", "expires_at", "refresh_until").Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND consent_version = ? AND credential_contract = ?", bind.ConnectionID, bind.ConsentVersion, bind.CredentialContract).
		Where("NOT revoked AND NOT attempt_failed AND attempt_expires_at IS NULL AND NOT refresh_failed").
		Take(&row).Error
	if err != nil {
		return err
	}
	var locked model.MerchantCallbackSubscription
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&bind, "ID", "TenantID", "Provider", "SubscriptionKey", "ConnectionID",
		"ShopID", "ConsentVersion", "CredentialContract", "VerifierVersion", "EventKind", "RequiredScope", "Enabled").Take(&locked).Error
	if err != nil {
		return err
	}
	now := time.Now()
	if !slices.Contains(row.Scopes, bind.RequiredScope) || row.ExpiresAt == nil || !row.ExpiresAt.After(now) || !validUntil.After(now) {
		return ErrMerchantCallbackBinding
	}
	if row.RefreshUntil != nil && !row.RefreshUntil.After(now) {
		return ErrMerchantCallbackBinding
	}
	if row.RefreshUntil != nil || row.Version != snapshot.Version || row.CredentialVersion != snapshot.CredentialVersion ||
		row.CredentialCiphertext != snapshot.CredentialCiphertext {
		// Rotation keeps the consent. Retry from verification against the new credential snapshot.
		return ErrMerchantCallbackStorage
	}
	return nil
}

func insertMerchantCallbackEvent(tx *gorm.DB, event model.MerchantCallbackEvent) (uint, bool, error) {
	result := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "provider"}, {Name: "subscription_id"}, {Name: "event_key"}}, DoNothing: true,
	}).Create(&event)
	if result.Error != nil || result.RowsAffected != 0 {
		return event.ID, false, result.Error
	}
	var first model.MerchantCallbackEvent
	err := tx.Where("tenant_id = ? AND provider = ? AND subscription_id = ? AND event_key = ?", event.TenantID,
		event.Provider, event.SubscriptionID, event.EventKey).Take(&first).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, ErrMerchantCallbackConflict
	}
	if err != nil {
		return 0, false, err
	}
	if first.ConsentVersion != event.ConsentVersion || first.ShopID != event.ShopID || first.Kind != event.Kind ||
		first.Scope != event.Scope || first.ResourceID != event.ResourceID || first.BodyDigest != event.BodyDigest ||
		first.VerifierVersion != event.VerifierVersion {
		return 0, false, ErrMerchantCallbackConflict
	}
	return first.ID, true, nil
}

func recordMerchantCallbackResourceHead(tx *gorm.DB, event model.MerchantCallbackEvent) error {
	var head model.MerchantCallbackResourceWatermark
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND provider = ? AND subscription_id = ? AND kind = ? AND resource_id = ?",
		event.TenantID, event.Provider, event.SubscriptionID, event.Kind, event.ResourceID).Take(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		head = model.MerchantCallbackResourceWatermark{TenantID: event.TenantID, Provider: event.Provider, SubscriptionID: event.SubscriptionID,
			Kind: event.Kind, ResourceID: event.ResourceID, EventID: event.ID, SignedAt: event.SignedAt, ConsentVersion: event.ConsentVersion,
			ConnectionVersion: event.ConnectionVersion, CredentialVersion: event.CredentialVersion}
		return tx.Create(&head).Error
	}
	if err != nil {
		return err
	}
	newer := event.SignedAt.After(head.SignedAt) || (event.SignedAt.Equal(head.SignedAt) && event.ID > head.EventID)
	if !newer {
		return tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND id = ?", event.TenantID, event.ID).
			Updates(map[string]any{"disposition": model.MerchantCallbackDispositionSuperseded, "superseded_by_id": head.EventID}).Error
	}
	if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND id = ? AND disposition = ?", event.TenantID, head.EventID, model.MerchantCallbackDispositionPending).
		Updates(map[string]any{"disposition": model.MerchantCallbackDispositionSuperseded, "superseded_by_id": event.ID}).Error; err != nil {
		return err
	}
	return tx.Model(&model.MerchantCallbackResourceWatermark{}).Where("tenant_id = ? AND id = ?", event.TenantID, head.ID).
		Updates(map[string]any{"event_id": event.ID, "signed_at": event.SignedAt, "consent_version": event.ConsentVersion,
			"connection_version": event.ConnectionVersion, "credential_version": event.CredentialVersion}).Error
}

func (r *merchantCallbackRepository) ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (MerchantCallbackCompensationPage, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || subscriptionID == 0 {
		return MerchantCallbackCompensationPage{}, ErrMerchantCallbackBinding
	}
	if r.db == nil {
		return MerchantCallbackCompensationPage{}, ErrMerchantCallbackStorage
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	ctx = tenant.OnlyTenant(ctx, id)
	var page MerchantCallbackCompensationPage
	err := r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bind, connection, err := loadAndLockMerchantCallbackBinding(tx, id, subscriptionID)
		if err != nil {
			return err
		}
		cursor := model.MerchantCallbackCompensationCursor{TenantID: id, SubscriptionID: subscriptionID}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "subscription_id"}}, DoNothing: true}).Create(&cursor).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND subscription_id = ?", id, subscriptionID).Take(&cursor).Error; err != nil {
			return err
		}
		if cursor.Active && !merchantCallbackCursorFenceMatches(cursor, bind, connection) {
			cursor.Active, cursor.HighWaterID, cursor.AfterEventID, cursor.PageEndID, cursor.PageLimit = false, 0, 0, 0, 0
		}
		if !cursor.Active {
			var high uint
			if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND subscription_id = ?", id, subscriptionID).
				Select("COALESCE(MAX(id), 0)").Scan(&high).Error; err != nil {
				return err
			}
			if high <= cursor.AfterEventID {
				cursor.HighWaterID, cursor.PageEndID, cursor.PageLimit = 0, 0, 0
				cursor.Active = false
				cursor.ConnectionID, cursor.ConnectionVersion, cursor.CredentialVersion, cursor.ConsentVersion = connection.ID, connection.Version, connection.CredentialVersion, bind.ConsentVersion
				if err := tx.Model(&model.MerchantCallbackCompensationCursor{}).Where("tenant_id = ? AND subscription_id = ?", id, subscriptionID).Updates(cursorUpdateMap(cursor)).Error; err != nil {
					return err
				}
				page = merchantCallbackPageFromCursor(cursor, nil, true)
				return nil
			}
			cursor.Active, cursor.HighWaterID, cursor.PageEndID, cursor.PageLimit = true, high, 0, limit
			cursor.ConnectionID, cursor.ConnectionVersion, cursor.CredentialVersion, cursor.ConsentVersion = connection.ID, connection.Version, connection.CredentialVersion, bind.ConsentVersion
		}
		if cursor.PageEndID == 0 {
			var ids []uint
			if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND subscription_id = ? AND id > ? AND id <= ?", id, subscriptionID, cursor.AfterEventID, cursor.HighWaterID).
				Order("id ASC").Limit(cursor.PageLimit).Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				cursor.Active, cursor.HighWaterID, cursor.AfterEventID, cursor.PageEndID, cursor.PageLimit = false, 0, 0, 0, 0
				if err := tx.Model(&model.MerchantCallbackCompensationCursor{}).Where("tenant_id = ? AND subscription_id = ?", id, subscriptionID).Updates(cursorUpdateMap(cursor)).Error; err != nil {
					return err
				}
				page = merchantCallbackPageFromCursor(cursor, nil, true)
				return nil
			}
			cursor.PageEndID = ids[len(ids)-1]
			if err := tx.Model(&model.MerchantCallbackCompensationCursor{}).Where("tenant_id = ? AND subscription_id = ?", id, subscriptionID).Updates(cursorUpdateMap(cursor)).Error; err != nil {
				return err
			}
		}
		var events []model.MerchantCallbackEvent
		if err := tx.Where("tenant_id = ? AND subscription_id = ? AND id > ? AND id <= ?", id, subscriptionID, cursor.AfterEventID, cursor.PageEndID).
			Order("id ASC").Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return ErrMerchantCallbackPageStale
		}
		if err := markMerchantCallbackPageHeads(tx, id, subscriptionID, events); err != nil {
			return err
		}
		page = merchantCallbackPageFromCursor(cursor, events, cursor.PageEndID >= cursor.HighWaterID)
		return nil
	})
	if err != nil {
		return MerchantCallbackCompensationPage{}, merchantCallbackStoreError(err)
	}
	return page, nil
}

func loadAndLockMerchantCallbackBinding(tx *gorm.DB, tenantID, subscriptionID uint) (model.MerchantCallbackSubscription, model.MerchantConnection, error) {
	var bind model.MerchantCallbackSubscription
	if err := tx.Where("tenant_id = ? AND id = ? AND enabled", tenantID, subscriptionID).Take(&bind).Error; err != nil {
		return bind, model.MerchantConnection{}, err
	}
	var connection model.MerchantConnection
	if err := merchantConnectionTarget(tx, tenantID, bind.Provider, bind.ShopID).
		Select("id", "tenant_id", "provider", "shop_id", "version", "credential_version", "consent_version", "credential_contract", "credential_ciphertext", "scopes", "expires_at", "refresh_until", "refresh_failed", "revoked", "attempt_failed", "attempt_expires_at").
		Where("id = ?", bind.ConnectionID).Take(&connection).Error; err != nil {
		return bind, connection, err
	}
	if err := lockMerchantCallbackBinding(tx, bind, connection, time.Now().Add(time.Minute)); err != nil {
		return bind, connection, err
	}
	return bind, connection, nil
}

func merchantCallbackCursorFenceMatches(cursor model.MerchantCallbackCompensationCursor, bind model.MerchantCallbackSubscription, connection model.MerchantConnection) bool {
	return cursor.ConnectionID == connection.ID && cursor.ConnectionVersion == connection.Version &&
		cursor.CredentialVersion == connection.CredentialVersion && cursor.ConsentVersion == bind.ConsentVersion
}

func cursorUpdateMap(cursor model.MerchantCallbackCompensationCursor) map[string]any {
	return map[string]any{"active": cursor.Active, "high_water_id": cursor.HighWaterID, "after_event_id": cursor.AfterEventID,
		"page_end_id": cursor.PageEndID, "page_limit": cursor.PageLimit, "connection_id": cursor.ConnectionID,
		"connection_version": cursor.ConnectionVersion, "credential_version": cursor.CredentialVersion,
		"consent_version": cursor.ConsentVersion, "updated_at": time.Now().UTC()}
}

func merchantCallbackPageFromCursor(cursor model.MerchantCallbackCompensationCursor, events []model.MerchantCallbackEvent, final bool) MerchantCallbackCompensationPage {
	return MerchantCallbackCompensationPage{TenantID: cursor.TenantID, SubscriptionID: cursor.SubscriptionID, ConnectionID: cursor.ConnectionID,
		ConnectionVersion: cursor.ConnectionVersion, CredentialVersion: cursor.CredentialVersion, ConsentVersion: cursor.ConsentVersion,
		HighWaterID: cursor.HighWaterID, AfterEventID: cursor.AfterEventID, PageEndID: cursor.PageEndID, PageLimit: cursor.PageLimit, Events: events, Final: final}
}

func markMerchantCallbackPageHeads(tx *gorm.DB, tenantID, subscriptionID uint, events []model.MerchantCallbackEvent) error {
	for i := range events {
		var head model.MerchantCallbackResourceWatermark
		err := tx.Where("tenant_id = ? AND subscription_id = ? AND provider = ? AND kind = ? AND resource_id = ?", tenantID, subscriptionID,
			events[i].Provider, events[i].Kind, events[i].ResourceID).Take(&head).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		events[i].Current = err == nil && head.EventID == events[i].ID
	}
	return nil
}

func (r *merchantCallbackRepository) AcknowledgeCompensationPage(ctx context.Context, page MerchantCallbackCompensationPage, appliedEventIDs []uint) error {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || id != page.TenantID || page.SubscriptionID == 0 || page.HighWaterID == 0 || page.PageEndID == 0 || len(page.Events) == 0 {
		return ErrMerchantCallbackBinding
	}
	if r.db == nil {
		return ErrMerchantCallbackStorage
	}
	ctx = tenant.OnlyTenant(ctx, id)
	err := r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bind, connection, err := loadAndLockMerchantCallbackBinding(tx, id, page.SubscriptionID)
		if err != nil {
			return err
		}
		var cursor model.MerchantCallbackCompensationCursor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND subscription_id = ?", id, page.SubscriptionID).Take(&cursor).Error; err != nil {
			return err
		}
		if !cursor.Active || !merchantCallbackCursorFenceMatches(cursor, bind, connection) || !merchantCallbackPageMatchesCursor(page, cursor) {
			return ErrMerchantCallbackPageStale
		}
		var persisted []model.MerchantCallbackEvent
		if err := tx.Where("tenant_id = ? AND subscription_id = ? AND id > ? AND id <= ?", id, page.SubscriptionID, cursor.AfterEventID, cursor.PageEndID).
			Order("id ASC").Find(&persisted).Error; err != nil {
			return err
		}
		if len(persisted) != len(page.Events) {
			return ErrMerchantCallbackPageStale
		}
		expected := make(map[uint]struct{}, len(persisted))
		current := make(map[uint]struct{}, len(persisted))
		for i := range persisted {
			if persisted[i].ID != page.Events[i].ID {
				return ErrMerchantCallbackPageStale
			}
			expected[persisted[i].ID] = struct{}{}
			var head model.MerchantCallbackResourceWatermark
			err := tx.Where("tenant_id = ? AND subscription_id = ? AND provider = ? AND kind = ? AND resource_id = ?", id, page.SubscriptionID,
				persisted[i].Provider, persisted[i].Kind, persisted[i].ResourceID).Take(&head).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if persisted[i].Disposition == model.MerchantCallbackDispositionPending && err == nil && head.EventID == persisted[i].ID {
				current[persisted[i].ID] = struct{}{}
			}
		}
		provided := make(map[uint]struct{}, len(appliedEventIDs))
		for _, eventID := range appliedEventIDs {
			if _, duplicate := provided[eventID]; duplicate {
				return ErrMerchantCallbackPageStale
			}
			if _, exists := expected[eventID]; !exists {
				return ErrMerchantCallbackPageStale
			}
			if _, isCurrent := current[eventID]; !isCurrent {
				return ErrMerchantCallbackPageStale
			}
			provided[eventID] = struct{}{}
		}
		if len(provided) != len(current) {
			return ErrMerchantCallbackPageStale
		}
		for eventID := range current {
			if err := tx.Model(&model.MerchantCallbackEvent{}).Where("tenant_id = ? AND subscription_id = ? AND id = ? AND disposition = ?", id, page.SubscriptionID, eventID, model.MerchantCallbackDispositionPending).
				Update("disposition", model.MerchantCallbackDispositionApplied).Error; err != nil {
				return err
			}
		}
		cursor.AfterEventID, cursor.PageEndID = cursor.PageEndID, 0
		if cursor.AfterEventID >= cursor.HighWaterID {
			cursor.Active, cursor.HighWaterID, cursor.PageEndID, cursor.PageLimit = false, 0, 0, 0
		}
		return tx.Model(&model.MerchantCallbackCompensationCursor{}).Where("tenant_id = ? AND subscription_id = ?", id, page.SubscriptionID).Updates(cursorUpdateMap(cursor)).Error
	})
	if err != nil {
		return merchantCallbackStoreError(err)
	}
	return nil
}

func merchantCallbackPageMatchesCursor(page MerchantCallbackCompensationPage, cursor model.MerchantCallbackCompensationCursor) bool {
	return page.TenantID == cursor.TenantID && page.SubscriptionID == cursor.SubscriptionID && page.ConnectionID == cursor.ConnectionID &&
		page.ConnectionVersion == cursor.ConnectionVersion && page.CredentialVersion == cursor.CredentialVersion && page.ConsentVersion == cursor.ConsentVersion &&
		page.HighWaterID == cursor.HighWaterID && page.AfterEventID == cursor.AfterEventID && page.PageEndID == cursor.PageEndID && page.PageLimit == cursor.PageLimit
}

func merchantCallbackStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, ErrMerchantCallbackBinding):
		return ErrMerchantCallbackBinding
	case errors.Is(err, ErrMerchantCallbackConflict):
		return ErrMerchantCallbackConflict
	case errors.Is(err, ErrMerchantCallbackPageStale):
		return ErrMerchantCallbackPageStale
	default:
		return ErrMerchantCallbackStorage
	}
}

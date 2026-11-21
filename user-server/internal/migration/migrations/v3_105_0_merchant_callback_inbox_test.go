package migrations

import (
	"testing"
	"time"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

type merchantCallbackEventBeforeV3105 struct {
	ID              uint      `gorm:"primaryKey"`
	TenantID        uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_event,priority:1"`
	Provider        string    `gorm:"not null;size:64;uniqueIndex:uq_merchant_callback_event,priority:2"`
	SubscriptionID  uint      `gorm:"not null;uniqueIndex:uq_merchant_callback_event,priority:3"`
	EventKey        string    `gorm:"not null;size:160;uniqueIndex:uq_merchant_callback_event,priority:4"`
	ShopID          string    `gorm:"not null;size:160"`
	ConsentVersion  int64     `gorm:"not null"`
	Kind            string    `gorm:"not null;size:64"`
	Scope           string    `gorm:"not null;size:160"`
	ResourceID      string    `gorm:"not null;size:160"`
	BodyDigest      string    `gorm:"not null;size:64"`
	VerifierVersion string    `gorm:"not null;size:64"`
	SignedAt        time.Time `gorm:"not null"`
	ValidUntil      time.Time `gorm:"not null"`
	CreatedAt       time.Time
}

func (merchantCallbackEventBeforeV3105) TableName() string { return "merchant_callback_events" }

func TestMerchantCallbackInboxMigration_BackfillsOldInboxIdempotently(t *testing.T) {
	db := testutil.NewTestDB(t, &merchantCallbackEventBeforeV3105{}, &model.MerchantConnection{}, &model.MerchantCallbackSubscription{})
	ctx := tenant.WithSystemScope(t.Context())
	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []merchantCallbackEventBeforeV3105{
		{TenantID: 71, Provider: "fixture", SubscriptionID: 9, EventKey: "new", ShopID: "shop-a", ConsentVersion: 4,
			Kind: "order.updated", Scope: "read_orders", ResourceID: "order-1", BodyDigest: "digest-new", VerifierVersion: "v1", SignedAt: now, ValidUntil: now.Add(time.Hour)},
		{TenantID: 71, Provider: "fixture", SubscriptionID: 9, EventKey: "old", ShopID: "shop-a", ConsentVersion: 4,
			Kind: "order.updated", Scope: "read_orders", ResourceID: "order-1", BodyDigest: "digest-old", VerifierVersion: "v1", SignedAt: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)},
	}
	for i := range rows {
		require.NoError(t, db.WithContext(ctx).Create(&rows[i]).Error)
	}
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.105.0")
	require.True(t, ok)
	require.NoError(t, m.Up(ctx))
	require.NoError(t, m.Up(ctx))

	var events []model.MerchantCallbackEvent
	require.NoError(t, db.WithContext(ctx).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, rows[0].ID, events[0].ID)
	require.Equal(t, model.MerchantCallbackDispositionPending, events[0].Disposition)
	require.Equal(t, rows[1].ID, events[1].ID)
	require.Equal(t, model.MerchantCallbackDispositionSuperseded, events[1].Disposition)
	require.NotNil(t, events[1].SupersededByID)
	require.Equal(t, events[0].ID, *events[1].SupersededByID)
	var watermark model.MerchantCallbackResourceWatermark
	require.NoError(t, db.WithContext(ctx).Where("tenant_id = ? AND provider = ? AND subscription_id = ? AND resource_id = ?", 71, "fixture", 9, "order-1").Take(&watermark).Error)
	require.Equal(t, events[0].ID, watermark.EventID)

	// Composite uniqueness fences both resource-head and cursor identity per tenant.
	duplicateHead := watermark
	duplicateHead.ID = 0
	require.Error(t, db.WithContext(ctx).Create(&duplicateHead).Error)
	cursor := model.MerchantCallbackCompensationCursor{TenantID: 71, SubscriptionID: 9}
	require.NoError(t, db.WithContext(ctx).Create(&cursor).Error)
	duplicateCursor := cursor
	duplicateCursor.ID = 0
	require.Error(t, db.WithContext(ctx).Create(&duplicateCursor).Error)
}

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	merchantCallbackInboxTenant   = 881
	merchantCallbackInboxProvider = "callback-inbox-fixture"
)

type merchantCallbackInboxFixture struct {
	db interface{}
}

func newMerchantCallbackInboxFixture(t *testing.T) (*gorm.DB, context.Context, model.MerchantCallbackSubscription, model.MerchantConnection) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MerchantConnection{}, &model.MerchantCallbackSubscription{}, &model.MerchantCallbackEvent{},
		&model.MerchantCallbackResourceWatermark{}, &model.MerchantCallbackCompensationCursor{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(t.Context(), merchantCallbackInboxTenant)
	now := time.Now().UTC()
	connection := model.MerchantConnection{TenantID: merchantCallbackInboxTenant, Provider: merchantCallbackInboxProvider, ShopID: "shop-a",
		Version: 4, CredentialVersion: 3, ConsentVersion: 2, CredentialContract: "credential-v1", CredentialCiphertext: "enc:v1:fixture",
		Scopes: []string{"read_orders"}, ExpiresAt: ptrMerchantCallbackTime(now.Add(time.Hour))}
	require.NoError(t, db.WithContext(ctx).Create(&connection).Error)
	subscription := model.MerchantCallbackSubscription{TenantID: merchantCallbackInboxTenant, Provider: merchantCallbackInboxProvider,
		SubscriptionKey: "subscription-a", ConnectionID: connection.ID, ShopID: connection.ShopID, ConsentVersion: connection.ConsentVersion,
		CredentialContract: connection.CredentialContract, VerifierVersion: "verifier-v1", EventKind: "order.updated",
		RequiredScope: "read_orders", Enabled: true}
	require.NoError(t, db.WithContext(ctx).Create(&subscription).Error)
	return db, ctx, subscription, connection
}

func ptrMerchantCallbackTime(t time.Time) *time.Time { return &t }

func merchantCallbackInboxEvent(sub model.MerchantCallbackSubscription, key, resource string, signedAt time.Time) model.MerchantCallbackEvent {
	return model.MerchantCallbackEvent{TenantID: sub.TenantID, Provider: sub.Provider, SubscriptionID: sub.ID, EventKey: key,
		ShopID: sub.ShopID, ConsentVersion: sub.ConsentVersion, ConnectionVersion: 4, CredentialVersion: 3,
		Kind: sub.EventKind, Scope: sub.RequiredScope, ResourceID: resource, BodyDigest: "digest-" + key,
		VerifierVersion: sub.VerifierVersion, SignedAt: signedAt.UTC(), ValidUntil: signedAt.Add(time.Hour).UTC()}
}

func TestMerchantCallbackInbox_ResourceWatermarkPreventsStaleAndBreaksTies(t *testing.T) {
	db, ctx, sub, conn := newMerchantCallbackInboxFixture(t)
	repo := NewMerchantCallbackRepository(db)
	newerTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	newerID, duplicate, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, "newer", "order-7", newerTime))
	require.NoError(t, err)
	require.False(t, duplicate)
	olderID, duplicate, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, "older", "order-7", newerTime.Add(-time.Second)))
	require.NoError(t, err)
	require.False(t, duplicate)
	var old model.MerchantCallbackEvent
	require.NoError(t, db.WithContext(tenant.WithSystemScope(t.Context())).First(&old, olderID).Error)
	require.Equal(t, model.MerchantCallbackDispositionSuperseded, old.Disposition)
	require.NotNil(t, old.SupersededByID)
	require.Equal(t, newerID, *old.SupersededByID)
	var head model.MerchantCallbackResourceWatermark
	require.NoError(t, db.WithContext(tenant.WithSystemScope(t.Context())).Where("tenant_id = ? AND subscription_id = ? AND resource_id = ?", sub.TenantID, sub.ID, "order-7").Take(&head).Error)
	require.Equal(t, newerID, head.EventID)

	tieID, _, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, "equal-time-later", "order-7", newerTime))
	require.NoError(t, err)
	require.Greater(t, tieID, newerID)
	old = model.MerchantCallbackEvent{}
	require.NoError(t, db.WithContext(tenant.WithSystemScope(t.Context())).Where("id = ?", newerID).Take(&old).Error)
	require.Equal(t, model.MerchantCallbackDispositionSuperseded, old.Disposition)
	require.NotNil(t, old.SupersededByID)
	require.Equal(t, tieID, *old.SupersededByID)
	require.NoError(t, db.WithContext(tenant.WithSystemScope(t.Context())).Where("tenant_id = ? AND subscription_id = ? AND resource_id = ?", sub.TenantID, sub.ID, "order-7").Take(&head).Error)
	require.Equal(t, tieID, head.EventID)
}

func TestMerchantCallbackInbox_CompensationPageSurvivesRestartAndFindsLaterHighWater(t *testing.T) {
	db, ctx, sub, conn := newMerchantCallbackInboxFixture(t)
	repo := NewMerchantCallbackRepository(db)
	base := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 5; i++ {
		_, _, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, fmt.Sprintf("event-%d", i), fmt.Sprintf("order-%d", i), base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
	}

	first, err := repo.ReadCompensationPage(ctx, sub.ID, 2)
	require.NoError(t, err)
	require.Len(t, first.Events, 2)
	require.False(t, first.Final)
	require.Positive(t, first.HighWaterID)
	// A fresh repository instance must reconstruct the durable, unacknowledged page.
	restarted := NewMerchantCallbackRepository(db)
	replayed, err := restarted.ReadCompensationPage(ctx, sub.ID, 2)
	require.NoError(t, err)
	require.Equal(t, first.EventIDs(), replayed.EventIDs())
	require.Equal(t, first.HighWaterID, replayed.HighWaterID)
	require.Equal(t, first.PageEndID, replayed.PageEndID)

	require.ErrorIs(t, restarted.AcknowledgeCompensationPage(ctx, first, first.EventIDs()[:1]), ErrMerchantCallbackPageStale)
	cursor := merchantCallbackInboxCursor(t, db, sub)
	require.Zero(t, cursor.AfterEventID)
	require.NoError(t, restarted.AcknowledgeCompensationPage(ctx, replayed, replayed.EventIDs()))

	second, err := restarted.ReadCompensationPage(ctx, sub.ID, 2)
	require.NoError(t, err)
	require.Len(t, second.Events, 2)
	require.Greater(t, second.AfterEventID, first.PageEndID-2)
	require.NoError(t, restarted.AcknowledgeCompensationPage(ctx, second, second.EventIDs()))

	third, err := restarted.ReadCompensationPage(ctx, sub.ID, 2)
	require.NoError(t, err)
	require.Len(t, third.Events, 1)
	require.True(t, third.Final)
	// A commit after the fixed high-water is not smuggled into this sweep.
	lateID, _, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, "event-late", "late-order", base.Add(10*time.Second)))
	require.NoError(t, err)
	require.Less(t, lateID, third.HighWaterID+2)
	require.NoError(t, restarted.AcknowledgeCompensationPage(ctx, third, third.EventIDs()))
	fourth, err := restarted.ReadCompensationPage(ctx, sub.ID, 2)
	require.NoError(t, err)
	require.Len(t, fourth.Events, 1)
	require.Equal(t, lateID, fourth.Events[0].ID)
	require.True(t, fourth.Final)
}

func TestMerchantCallbackInbox_CompensationPageRejectsWrongTenantAndStaleConnection(t *testing.T) {
	db, ctx, sub, conn := newMerchantCallbackInboxFixture(t)
	repo := NewMerchantCallbackRepository(db)
	_, _, err := repo.Record(ctx, sub, conn, merchantCallbackInboxEvent(sub, "scoped", "order-x", time.Now().UTC()))
	require.NoError(t, err)
	page, err := repo.ReadCompensationPage(ctx, sub.ID, 10)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	foreign, err := repo.ReadCompensationPage(tenant.WithTenantID(t.Context(), merchantCallbackInboxTenant+1), sub.ID, 10)
	require.ErrorIs(t, err, ErrMerchantCallbackBinding)
	require.Empty(t, foreign.Events)
	_, err = repo.ReadCompensationPage(tenant.WithSystemScope(t.Context()), sub.ID, 10)
	require.ErrorIs(t, err, ErrMerchantCallbackBinding)

	require.NoError(t, db.WithContext(tenant.WithSystemScope(t.Context())).Model(&model.MerchantConnection{}).
		Where("tenant_id = ? AND id = ?", sub.TenantID, sub.ConnectionID).Update("version", conn.Version+1).Error)
	require.ErrorIs(t, repo.AcknowledgeCompensationPage(ctx, page, page.EventIDs()), ErrMerchantCallbackPageStale)
	cursor := merchantCallbackInboxCursor(t, db, sub)
	require.Zero(t, cursor.AfterEventID)

	restarted, err := repo.ReadCompensationPage(ctx, sub.ID, 10)
	require.NoError(t, err)
	require.Equal(t, page.EventIDs(), restarted.EventIDs())
	require.NotEqual(t, page.ConnectionVersion, restarted.ConnectionVersion)
}

func merchantCallbackInboxCursor(t *testing.T, db *gorm.DB, sub model.MerchantCallbackSubscription) model.MerchantCallbackCompensationCursor {
	t.Helper()
	var cursor model.MerchantCallbackCompensationCursor
	err := db.WithContext(tenant.WithSystemScope(t.Context())).Where("tenant_id = ? AND subscription_id = ?", sub.TenantID, sub.ID).Take(&cursor).Error
	require.NoError(t, err)
	return cursor
}

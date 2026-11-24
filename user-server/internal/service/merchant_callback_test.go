package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type syntheticMerchantVerifier struct {
	contract MerchantCallbackContract
	verify   func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error)
}

func (v syntheticMerchantVerifier) Contract() MerchantCallbackContract { return v.contract }
func (v syntheticMerchantVerifier) Verify(ctx context.Context, in MerchantCallbackInput) (VerifiedMerchantEvent, error) {
	return v.verify(ctx, in)
}

const merchantFixtureBytes = " {\n\t\"fixture\":true } "

func merchantFixtureHeaders() http.Header { return http.Header{"X-Fixture-Proof": {"fixture-proof"}} }

type merchantCallbackFixture struct {
	db       *gorm.DB
	bind     model.MerchantCallbackSubscription
	event    VerifiedMerchantEvent
	connect  MerchantConnectionService
	provider *syntheticMerchantProvider
	ctx      context.Context
}

func newMerchantCallbackFixture(t *testing.T, policy ...MerchantRefreshPolicy) merchantCallbackFixture {
	t.Helper()
	db, svc, provider, tenantCtx := newSyntheticMerchantConnection(t)
	if len(policy) != 0 {
		provider.contract.RefreshPolicy = policy[0]
		refresher := &syntheticMerchantRefreshProvider{syntheticMerchantProvider: provider}
		refresher.refresh = func(context.Context, MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
			grant := syntheticMerchantGrant()
			grant.RefreshToken = "fixture-rotated-refresh"
			return grant, nil
		}
		svc = NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), map[string]MerchantConnectionProvider{provider.contract.Provider: refresher})
	}
	callbackDB := testutil.NewTestDB(t, &model.MerchantCallbackSubscription{}, &model.MerchantCallbackEvent{},
		&model.MerchantCallbackResourceWatermark{}, &model.MerchantCallbackCompensationCursor{})
	input, _ := startSyntheticMerchantConnection(t, svc, tenantCtx)
	_, err := svc.Complete(tenantCtx, 17, input)
	require.NoError(t, err)
	var row model.MerchantConnection
	require.NoError(t, db.First(&row).Error)
	require.Positive(t, row.ConsentVersion)
	sub := model.MerchantCallbackSubscription{TenantID: row.TenantID, ConnectionID: row.ID, Provider: row.Provider,
		SubscriptionKey: "subscription-a", ShopID: row.ShopID, ConsentVersion: row.ConsentVersion,
		CredentialContract: row.CredentialContract, VerifierVersion: "fixture-v1", EventKind: "order.updated", RequiredScope: "read_orders", Enabled: true}
	require.NoError(t, callbackDB.WithContext(tenantCtx).Create(&sub).Error)
	now := time.Now().UTC().Truncate(time.Second)
	event := VerifiedMerchantEvent{Provider: row.Provider, SubscriptionKey: sub.SubscriptionKey, ShopID: row.ShopID, EventKey: "event-a",
		Kind: sub.EventKind, Scope: sub.RequiredScope, ResourceID: "order-001", SignedAt: now, ValidUntil: now.Add(time.Minute)}
	return merchantCallbackFixture{db: db, bind: sub, event: event, connect: svc, provider: provider, ctx: tenantCtx}
}

func (f merchantCallbackFixture) service(repo repository.MerchantCallbackRepository, v MerchantCallbackVerifier) MerchantCallbackService {
	return NewMerchantCallbackService(repo, repository.NewMerchantConnectionRepository(f.db), map[string]MerchantCallbackVerifier{f.bind.Provider: v})
}

func (f merchantCallbackFixture) ingest(ctx context.Context, svc MerchantCallbackService) MerchantCallbackResult {
	return svc.Ingest(ctx, f.bind.Provider, []byte(merchantFixtureBytes), merchantFixtureHeaders())
}

func (f merchantCallbackFixture) assertCount(t *testing.T, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, f.db.WithContext(tenant.WithSystemScope(t.Context())).Model(&model.MerchantCallbackEvent{}).Count(&count).Error)
	require.Equal(t, want, count)
}

type merchantCallbackLookupCounter struct {
	repository.MerchantCallbackRepository
	count int
}

type merchantCallbackRecordBarrier struct {
	repository.MerchantCallbackRepository
	entered chan<- struct{}
	release <-chan struct{}
}

func (r merchantCallbackRecordBarrier) Record(ctx context.Context, sub model.MerchantCallbackSubscription, conn model.MerchantConnection, event model.MerchantCallbackEvent) (uint, bool, error) {
	close(r.entered)
	select {
	case <-r.release:
		return r.MerchantCallbackRepository.Record(ctx, sub, conn, event)
	case <-ctx.Done():
		return 0, false, ctx.Err()
	}
}

func TestMerchantCallback_CommitFence(t *testing.T) {
	for _, change := range []string{"revoke", "reconnect", "subscription disabled"} {
		t.Run(change, func(t *testing.T) {
			f := newMerchantCallbackFixture(t)
			v := f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return f.event, nil })
			entered, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			repo := repository.NewMerchantCallbackRepository(f.db)
			svc := f.service(merchantCallbackRecordBarrier{repo, entered, release}, v)
			done := make(chan MerchantCallbackResult, 1)
			go func() { done <- f.ingest(ctx, svc) }()
			merchantWait(t, ctx, entered)
			switch change {
			case "revoke":
				_, err := f.connect.Revoke(f.ctx, 17, syntheticMerchantTarget)
				require.NoError(t, err)
			case "reconnect":
				input, _ := startSyntheticMerchantConnection(t, f.connect, f.ctx)
				_, err := f.connect.Complete(f.ctx, 17, input)
				require.NoError(t, err)
			case "subscription disabled":
				require.NoError(t, f.db.WithContext(f.ctx).Model(&model.MerchantCallbackSubscription{}).Where("id = ?", f.bind.ID).Update("enabled", false).Error)
			}
			close(release)
			out := <-done
			require.Equal(t, 403, out.Ack.Status)
			require.Zero(t, out.EventID)
			f.assertCount(t, 0)
			if change == "revoke" {
				input, _ := startSyntheticMerchantConnection(t, f.connect, f.ctx)
				_, err := f.connect.Complete(f.ctx, 17, input)
				require.NoError(t, err)
			}
			svc = f.service(repo, v)
			require.Equal(t, 403, f.ingest(t.Context(), svc).Ack.Status)
			f.assertCount(t, 0)
			if change == "subscription disabled" {
				return
			}
			var row model.MerchantConnection
			require.NoError(t, f.db.First(&row, f.bind.ConnectionID).Error)
			require.Greater(t, row.ConsentVersion, f.bind.ConsentVersion)
			newBinding := f.bind
			newBinding.ID, newBinding.SubscriptionKey, newBinding.ConsentVersion = 0, "new-explicit-subscription", row.ConsentVersion
			require.NoError(t, f.db.WithContext(f.ctx).Create(&newBinding).Error)
			f.event.SubscriptionKey = newBinding.SubscriptionKey
			require.Positive(t, f.ingest(t.Context(), svc).EventID)
			f.assertCount(t, 1)
		})
	}
}

func TestMerchantCallback_RefreshKeepsConsent(t *testing.T) {
	f := newMerchantCallbackFixture(t, MerchantRefreshRotate)
	v := f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return f.event, nil })
	repo := repository.NewMerchantCallbackRepository(f.db)
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan MerchantCallbackResult, 1)
	svc := f.service(merchantCallbackRecordBarrier{repo, entered, release}, v)
	go func() { done <- f.ingest(ctx, svc) }()
	merchantWait(t, ctx, entered)
	p := &syntheticMerchantRefreshProvider{syntheticMerchantProvider: f.provider}
	p.refresh = func(context.Context, MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
		grant := syntheticMerchantGrant()
		grant.RefreshToken = "rotated-refresh-fixture"
		return grant, nil
	}
	connections := repository.NewMerchantConnectionRepository(f.db)
	refresh := NewMerchantConnectionService(connections, map[string]MerchantConnectionProvider{f.bind.Provider: p})
	row, err := connections.FindStatus(f.ctx, f.bind.Provider, f.bind.ShopID)
	require.NoError(t, err)
	_, err = refresh.Refresh(f.ctx, 17, dto.MerchantConnectionRefreshRequest{MerchantConnectionTarget: syntheticMerchantTarget, CredentialVersion: row.CredentialVersion})
	require.NoError(t, err)
	close(release)
	result := <-done
	require.Equal(t, v.contract.Retry, result.Ack)
	require.True(t, result.TrustedAck)
	f.assertCount(t, 0)
	updated, err := connections.FindStatus(f.ctx, f.bind.Provider, f.bind.ShopID)
	require.NoError(t, err)
	require.Equal(t, row.ConsentVersion, updated.ConsentVersion)
	require.Greater(t, updated.CredentialVersion, row.CredentialVersion)
	require.Positive(t, f.ingest(t.Context(), f.service(repo, v)).EventID)
	f.assertCount(t, 1)
}

func (r *merchantCallbackLookupCounter) LookupSubscription(ctx context.Context, provider, key string) (model.MerchantCallbackSubscription, error) {
	r.count++
	return r.MerchantCallbackRepository.LookupSubscription(ctx, provider, key)
}

// 未验证的事件不能查租户；已签名的错店、错权限或无效绑定也不得写入。
func TestMerchantCallback_RejectsBeforeWrite(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	for _, name := range []string{"bad proof", "stale proof", "unknown provider", "disabled verifier", "unsafe ack", "wrong provider", "wrong shop", "wrong scope", "wrong kind",
		"unknown subscription", "wrong verifier version", "disabled subscription", "credential scope missing", "expired credential", "invalid ciphertext", "other tenant", "revoked"} {
		t.Run(name, func(t *testing.T) {
			wrap := &merchantCallbackLookupCounter{MerchantCallbackRepository: repository.NewMerchantCallbackRepository(f.db)}
			e := f.event
			boundProvider := f.bind.Provider
			want, lookedUp := 403, true
			var verifyErr error
			v := f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return e, verifyErr })
			sub := f.db.Model(&model.MerchantCallbackSubscription{}).Where("id = ?", f.bind.ID)
			connection := f.db.Model(&model.MerchantConnection{}).Where("id = ?", f.bind.ConnectionID)
			switch name {
			case "bad proof":
				verifyErr, lookedUp = errors.New("fixture-private-signature"), false
			case "stale proof":
				e.SignedAt, e.ValidUntil, lookedUp = time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), false
			case "unknown provider":
				boundProvider, want, lookedUp = "unknown-provider", 503, false
			case "disabled verifier":
				v.contract.Enabled, want, lookedUp = false, 503, false
			case "unsafe ack":
				v.contract.Retry.Status, want, lookedUp = 200, 503, false
			case "wrong provider":
				e.Provider, lookedUp = "another-provider", false
			case "wrong shop":
				e.ShopID = "wrong-shop"
			case "wrong scope":
				e.Scope = "write_orders"
			case "wrong kind":
				e.Kind = "orders.refunded"
			case "unknown subscription":
				e.SubscriptionKey = "missing-subscription"
			case "wrong verifier version":
				v.contract.Version = "other-v1"
			case "disabled subscription":
				require.NoError(t, sub.Update("enabled", false).Error)
				t.Cleanup(func() { require.NoError(t, sub.Update("enabled", true).Error) })
			case "credential scope missing":
				require.NoError(t, connection.Update("scopes", `[]`).Error)
				t.Cleanup(func() { require.NoError(t, connection.Update("scopes", `["read_orders"]`).Error) })
			case "expired credential":
				require.NoError(t, connection.Update("expires_at", time.Now().Add(-time.Minute)).Error)
				t.Cleanup(func() { require.NoError(t, connection.Update("expires_at", time.Now().Add(time.Hour)).Error) })
			case "invalid ciphertext":
				var row model.MerchantConnection
				require.NoError(t, f.db.First(&row, f.bind.ConnectionID).Error)
				require.NoError(t, connection.Update("credential_ciphertext", "plaintext-fixture").Error)
				t.Cleanup(func() { require.NoError(t, connection.Update("credential_ciphertext", row.CredentialCiphertext).Error) })
			case "other tenant":
				require.NoError(t, sub.Update("tenant_id", 999).Error)
				t.Cleanup(func() { require.NoError(t, sub.Update("tenant_id", f.bind.TenantID).Error) })
			case "revoked":
				_, err := f.connect.Revoke(f.ctx, 17, syntheticMerchantTarget)
				require.NoError(t, err)
			}
			out := f.service(wrap, v).Ingest(t.Context(), boundProvider, []byte(merchantFixtureBytes), merchantFixtureHeaders())
			require.Equal(t, want, out.Ack.Status)
			require.Empty(t, out.Ack.Body)
			require.Zero(t, out.EventID)
			require.Equal(t, lookedUp, wrap.count > 0)
			f.assertCount(t, 0)
		})
	}
}

// 测试专用签名覆盖精确正文；格式变化不能重用同一证明。
func TestMerchantCallback_ExactSignedBytes(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	sign := func(body []byte) []byte {
		mac := hmac.New(sha256.New, []byte("synthetic-fixture-key"))
		_, _ = mac.Write(body)
		return mac.Sum(nil)
	}
	v := f.verifier(func(_ context.Context, in MerchantCallbackInput) (VerifiedMerchantEvent, error) {
		proof := in.Headers.Values("X-Fixture-Proof")
		if len(proof) != 1 {
			return VerifiedMerchantEvent{}, errors.New("fixture: proof header cardinality")
		}
		decoded, err := hex.DecodeString(proof[0])
		if err != nil || !hmac.Equal(decoded, sign(in.Body)) {
			return VerifiedMerchantEvent{}, errors.New("fixture: wrong proof")
		}
		return f.event, nil
	})
	svc := f.service(repository.NewMerchantCallbackRepository(f.db), v)
	proof := hex.EncodeToString(sign([]byte(merchantFixtureBytes)))
	for _, tc := range []struct {
		body    string
		headers http.Header
		status  int
	}{
		{strings.TrimSpace(merchantFixtureBytes), http.Header{"X-Fixture-Proof": {proof}}, 403},
		{merchantFixtureBytes, http.Header{"X-Fixture-Proof": {proof, proof}}, 403},
		{merchantFixtureBytes, nil, 403},
		{merchantFixtureBytes, http.Header{"X-Fixture-Proof": {strings.Repeat("x", merchantCallbackHeaderLimit+1)}}, 400},
		{merchantFixtureBytes, http.Header{"X-Fixture-Proof": {proof}}, 200},
	} {
		out := svc.Ingest(t.Context(), f.bind.Provider, []byte(tc.body), tc.headers)
		require.Equal(t, tc.status, out.Ack.Status)
		if tc.status != 200 {
			f.assertCount(t, 0)
		}
	}
	f.assertCount(t, 1)
}

// 重放冲突不能覆盖初次事件，另一个订阅可独立使用相同提供方事件编号。
func TestMerchantCallback_ConflictAndIndependentNamespaces(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	repo := repository.NewMerchantCallbackRepository(f.db)
	event := f.event
	v := f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return event, nil })
	svc := f.service(repo, v)
	first := f.ingest(t.Context(), svc)
	require.Positive(t, first.EventID)
	changedBody := svc.Ingest(t.Context(), f.bind.Provider, []byte(`{"fixture":false}`), merchantFixtureHeaders())
	require.Equal(t, 409, changedBody.Ack.Status)
	require.Zero(t, changedBody.EventID)
	event.ResourceID = "different-order"
	conflict := f.ingest(t.Context(), svc)
	require.Equal(t, 409, conflict.Ack.Status)
	require.Zero(t, conflict.EventID)
	f.assertCount(t, 1)
	var preserved model.MerchantCallbackEvent
	require.NoError(t, f.db.First(&preserved, first.EventID).Error)
	require.Equal(t, f.event.ResourceID, preserved.ResourceID)
	otherBinding := f.bind
	otherBinding.ID, otherBinding.SubscriptionKey = 0, "subscription-same-tenant"
	require.NoError(t, f.db.WithContext(f.ctx).Create(&otherBinding).Error)
	event.SubscriptionKey, event.ResourceID = otherBinding.SubscriptionKey, "independent-order"
	independent := f.ingest(t.Context(), svc)
	require.Equal(t, 200, independent.Ack.Status)
	require.NotEqual(t, first.EventID, independent.EventID)
	f.assertCount(t, 2)
	event = f.event

	otherCtx := tenant.WithTenantID(t.Context(), 999)
	otherTarget := syntheticMerchantTarget
	otherTarget.ShopID = "other-verified-shop"
	f.provider.exchange = func(context.Context, MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		grant := syntheticMerchantGrant()
		grant.ShopID = otherTarget.ShopID
		return grant, nil
	}
	started, err := f.connect.Start(otherCtx, 17, otherTarget)
	require.NoError(t, err)
	u, err := url.Parse(started.AuthorizationURL)
	require.NoError(t, err)
	_, err = f.connect.Complete(otherCtx, 17, dto.MerchantConnectionCompleteRequest{MerchantConnectionTarget: otherTarget, State: u.Query().Get("state"), Code: "other-fixture-code"})
	require.NoError(t, err)
	var row model.MerchantConnection
	require.NoError(t, f.db.Where("tenant_id = ?", 999).Take(&row).Error)
	alt := f.bind
	alt.ID, alt.TenantID, alt.ShopID, alt.ConnectionID, alt.ConsentVersion = 0, row.TenantID, row.ShopID, row.ID, row.ConsentVersion
	alt.SubscriptionKey = "subscription-b"
	require.NoError(t, f.db.WithContext(otherCtx).Create(&alt).Error)
	ambiguous := alt
	ambiguous.ID, ambiguous.SubscriptionKey = 0, f.bind.SubscriptionKey
	require.Error(t, f.db.WithContext(otherCtx).Create(&ambiguous).Error)
	event.SubscriptionKey = alt.SubscriptionKey // Signed for the wrong shop, despite a valid subscription.
	require.Equal(t, 403, f.ingest(t.Context(), svc).Ack.Status)
	f.assertCount(t, 2)
	event.ShopID = alt.ShopID
	second := f.ingest(t.Context(), svc)
	require.Positive(t, second.EventID)
	require.NotEqual(t, first.EventID, second.EventID)
	event = f.event
	require.Equal(t, first.EventID, f.ingest(t.Context(), svc).EventID)
	f.assertCount(t, 3)
}

// 不同实例同时投递同一事件，数据库只提交一条记录。
func TestMerchantCallback_ConcurrentRedelivery(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	v := f.verifier(func(ctx context.Context, _ MerchantCallbackInput) (VerifiedMerchantEvent, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return f.event, nil
		case <-ctx.Done():
			return VerifiedMerchantEvent{}, ctx.Err()
		}
	})
	done := make(chan MerchantCallbackResult, 2)
	for range 2 {
		svc := f.service(repository.NewMerchantCallbackRepository(f.db), v)
		go func() { done <- f.ingest(ctx, svc) }()
	}
	for range 2 {
		merchantWait(t, ctx, entered)
	}
	close(release)
	one, two := <-done, <-done
	require.Positive(t, one.EventID)
	require.Equal(t, one.EventID, two.EventID)
	require.NotEqual(t, one.Duplicate, two.Duplicate)
	f.assertCount(t, 1)
}

func merchantWait(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("未收到回调测试同步点")
	}
}

// A deferred constraint fails at COMMIT, after the inbox insert has succeeded.
func TestMerchantCallback_CommitBeforeAckAndRetry(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	v := f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return f.event, nil })
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cleanupDB := f.db.WithContext(context.WithoutCancel(t.Context()))
	require.NoError(t, f.db.Exec(`CREATE FUNCTION fixture_callback_commit_reject() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'fixture-private-commit-failure'; END; $$`).Error)
	require.NoError(t, f.db.Exec(`CREATE CONSTRAINT TRIGGER fixture_callback_commit_trigger
		AFTER INSERT ON merchant_callback_events DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW EXECUTE FUNCTION fixture_callback_commit_reject()`).Error)
	// Test connections run with session_replication_role=replica; make this one trigger fire there.
	require.NoError(t, f.db.Exec(`ALTER TABLE merchant_callback_events ENABLE ALWAYS TRIGGER fixture_callback_commit_trigger`).Error)
	t.Cleanup(func() {
		require.NoError(t, cleanupDB.Exec(`DROP TRIGGER fixture_callback_commit_trigger ON merchant_callback_events`).Error)
		require.NoError(t, cleanupDB.Exec(`DROP FUNCTION fixture_callback_commit_reject()`).Error)
	})
	done := make(chan MerchantCallbackResult, 1)
	svc := f.service(repository.NewMerchantCallbackRepository(f.db), v)
	out := f.ingest(ctx, svc)
	require.Equal(t, v.contract.Retry, out.Ack)
	require.True(t, out.TrustedAck)
	require.Zero(t, out.EventID)
	f.assertCount(t, 0)
	require.NoError(t, f.db.Exec(`ALTER TABLE merchant_callback_events DISABLE TRIGGER fixture_callback_commit_trigger`).Error)
	name := "fixture:merchant-callback-commit"
	require.NoError(t, f.db.Callback().Create().After("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "merchant_callback_events" && tx.Error == nil {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Create().Remove(name)) })
	go func() { done <- f.ingest(ctx, f.service(repository.NewMerchantCallbackRepository(f.db), v)) }()
	merchantWait(t, ctx, entered)
	f.assertCount(t, 0)
	require.Empty(t, done)
	close(release)
	retry := <-done
	require.Equal(t, 200, retry.Ack.Status)
	require.Positive(t, retry.EventID)
	f.assertCount(t, 1)
}

func (f merchantCallbackFixture) verifier(verify func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error)) syntheticMerchantVerifier {
	return syntheticMerchantVerifier{contract: MerchantCallbackContract{Enabled: true, Version: f.bind.VerifierVersion, Headers: []string{"X-Fixture-Proof"},
		Success: MerchantCallbackAck{Status: 200, Body: "fixture-accepted"}, Retry: MerchantCallbackAck{Status: 503, Body: "fixture-retry"}}, verify: verify}
}

// 验证器必须看到原始字节和所选头；提交后的重复事件只返回首个事件标识。
func TestMerchantCallback_AuthenticCommitAndRestart(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	var replay VerifiedMerchantEvent
	v := f.verifier(func(ctx context.Context, in MerchantCallbackInput) (VerifiedMerchantEvent, error) {
		_, ok := tenant.FromContext(ctx)
		require.False(t, ok)
		require.Equal(t, merchantFixtureBytes, string(in.Body))
		require.Equal(t, merchantFixtureHeaders(), in.Headers)
		return replay, nil
	})
	headers := merchantFixtureHeaders()
	headers.Set("Authorization", "must-not-be-forwarded")
	for i := range 2 {
		verified := f.event
		if i == 1 {
			verified.SignedAt = verified.SignedAt.Add(time.Second)
			verified.ValidUntil = verified.ValidUntil.Add(time.Second)
		}
		replay = verified
		svc := NewMerchantCallbackService(repository.NewMerchantCallbackRepository(f.db), repository.NewMerchantConnectionRepository(f.db), map[string]MerchantCallbackVerifier{f.bind.Provider: v})
		out := svc.Ingest(tenant.WithTenantID(t.Context(), 999), f.bind.Provider, []byte(merchantFixtureBytes), headers)
		require.Equal(t, 200, out.Ack.Status)
		require.Equal(t, "fixture-accepted", out.Ack.Body)
		var rows []model.MerchantCallbackEvent
		require.NoError(t, f.db.Find(&rows).Error)
		require.Len(t, rows, 1)
		require.Equal(t, rows[0].ID, out.EventID)
		require.Equal(t, i == 1, out.Duplicate)
		require.Equal(t, f.bind.TenantID, rows[0].TenantID)
		require.Equal(t, f.event.ResourceID, rows[0].ResourceID)
		require.Equal(t, f.bind.VerifierVersion, rows[0].VerifierVersion)
		require.WithinDuration(t, f.event.SignedAt, rows[0].SignedAt, time.Microsecond)
	}
}

// T008：已验证事件必须捕获当前连接与凭证修订，且验证先于任何租户/订阅查找。
func TestMerchantCallbackInbox_CapturesRevisionsBeforeLookup(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	var conn model.MerchantConnection
	require.NoError(t, f.db.First(&conn).Error)
	v := f.verifier(func(ctx context.Context, in MerchantCallbackInput) (VerifiedMerchantEvent, error) {
		// 验证阶段必须发生在租户上下文建立之前（verifier-before-lookup）。
		_, ok := tenant.FromContext(ctx)
		require.False(t, ok)
		require.Equal(t, merchantFixtureBytes, string(in.Body))
		return f.event, nil
	})
	svc := f.service(repository.NewMerchantCallbackRepository(f.db), v)
	out := svc.Ingest(tenant.WithTenantID(t.Context(), 999), f.bind.Provider, []byte(merchantFixtureBytes), merchantFixtureHeaders())
	require.Equal(t, 200, out.Ack.Status)
	var stored model.MerchantCallbackEvent
	require.NoError(t, f.db.WithContext(tenant.WithSystemScope(t.Context())).First(&stored, out.EventID).Error)
	require.Equal(t, conn.Version, stored.ConnectionVersion)
	require.Equal(t, conn.CredentialVersion, stored.CredentialVersion)
}

// spy 仅包裹 repository 契约，用于证明 service 薄方法只经 repo 委托、不夹带其它副作用。
type merchantCallbackInboxRepoSpy struct {
	repository.MerchantCallbackRepository
	reads, acks      int
	readErr, ackErr  error
	lastSubscription uint
	lastLimit        int
	lastPageIDs      []uint
}

func (s *merchantCallbackInboxRepoSpy) ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (repository.MerchantCallbackCompensationPage, error) {
	s.reads++
	s.lastSubscription, s.lastLimit = subscriptionID, limit
	if s.readErr != nil {
		return repository.MerchantCallbackCompensationPage{}, s.readErr
	}
	return s.MerchantCallbackRepository.ReadCompensationPage(ctx, subscriptionID, limit)
}

func (s *merchantCallbackInboxRepoSpy) AcknowledgeCompensationPage(ctx context.Context, page repository.MerchantCallbackCompensationPage, appliedEventIDs []uint) error {
	s.acks++
	s.lastPageIDs = append([]uint(nil), appliedEventIDs...)
	if s.ackErr != nil {
		return s.ackErr
	}
	return s.MerchantCallbackRepository.AcknowledgeCompensationPage(ctx, page, appliedEventIDs)
}

// T013：service 页读/确认只经 repository 契约委托；边界 fail-closed 在委托前拦下，repo 错误原样传播。
func TestMerchantCallbackInbox_ServiceBoundaryDelegation(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	real := repository.NewMerchantCallbackRepository(f.db)
	spy := &merchantCallbackInboxRepoSpy{MerchantCallbackRepository: real}
	svc := f.service(spy, f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return f.event, nil }))

	// 先落一个真实事件，使页读产生可确认的 fence，确认才能成功。
	ingestCtx := tenant.WithTenantID(t.Context(), f.bind.TenantID)
	require.Equal(t, 200, svc.Ingest(ingestCtx, f.bind.Provider, []byte(merchantFixtureBytes), merchantFixtureHeaders()).Ack.Status)

	// 有效租户上下文：委托一次并把参数原样传给 repo。
	page, err := svc.ReadCompensationPage(ingestCtx, f.bind.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 1, spy.reads)
	require.Equal(t, f.bind.ID, spy.lastSubscription)
	require.Equal(t, 7, spy.lastLimit)
	require.Equal(t, f.bind.TenantID, page.TenantID)

	require.NotEmpty(t, page.EventIDs())
	// 确认同样只委托一次，并透传 appliedEventIDs。
	require.NoError(t, svc.AcknowledgeCompensationPage(ingestCtx, page, page.EventIDs()))
	require.Equal(t, 1, spy.acks)
	require.Equal(t, page.EventIDs(), spy.lastPageIDs)
}

func TestMerchantCallbackInbox_ServiceBoundaryFailClosed(t *testing.T) {
	f := newMerchantCallbackFixture(t)
	spy := &merchantCallbackInboxRepoSpy{MerchantCallbackRepository: repository.NewMerchantCallbackRepository(f.db)}
	svc := f.service(spy, f.verifier(func(context.Context, MerchantCallbackInput) (VerifiedMerchantEvent, error) { return f.event, nil }))

	// 无租户 / 系统 scope / 订阅为 0 都在委托前 fail-closed，repo 零调用。
	for _, ctx := range []context.Context{t.Context(), tenant.WithSystemScope(t.Context())} {
		_, err := svc.ReadCompensationPage(ctx, f.bind.ID, 50)
		require.ErrorIs(t, err, repository.ErrMerchantCallbackBinding)
		require.ErrorIs(t, svc.AcknowledgeCompensationPage(ctx, repository.MerchantCallbackCompensationPage{}, nil), repository.ErrMerchantCallbackBinding)
	}
	_, err := svc.ReadCompensationPage(tenant.WithTenantID(t.Context(), f.bind.TenantID), 0, 50)
	require.ErrorIs(t, err, repository.ErrMerchantCallbackBinding)
	require.Equal(t, 0, spy.reads)
	require.Equal(t, 0, spy.acks)

	// repo 错误原样传播（不吞错、不改判成功）。
	wantErr := errors.New("injected repo failure")
	spy.readErr, spy.ackErr = wantErr, wantErr
	ctx := tenant.WithTenantID(t.Context(), f.bind.TenantID)
	_, err = svc.ReadCompensationPage(ctx, f.bind.ID, 50)
	require.ErrorIs(t, err, wantErr)
	// 传有效 page 才能越过 boundary check、走到 repo 委托点验证错误原样传播。
	validPage := repository.MerchantCallbackCompensationPage{TenantID: f.bind.TenantID, SubscriptionID: f.bind.ID}
	require.ErrorIs(t, svc.AcknowledgeCompensationPage(ctx, validPage, nil), wantErr)

	// repo 未注入时 fail-closed，不 panic、不委托。
	nilSvc := NewMerchantCallbackService(nil, repository.NewMerchantConnectionRepository(f.db), nil)
	_, err = nilSvc.ReadCompensationPage(ctx, f.bind.ID, 50)
	require.ErrorIs(t, err, repository.ErrMerchantCallbackStorage)
	// 同样要越过 boundary check 才能验证 repo 未装配时 fail-closed。
	require.ErrorIs(t, nilSvc.AcknowledgeCompensationPage(ctx, validPage, nil), repository.ErrMerchantCallbackStorage)
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/secrets"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type syntheticMerchantProvider struct {
	contract MerchantOAuthContract
	exchange func(context.Context, MerchantOAuthExchange) (MerchantOAuthGrant, error)
}

func (p *syntheticMerchantProvider) Contract(context.Context) (MerchantOAuthContract, error) {
	return p.contract, nil
}

func (p *syntheticMerchantProvider) Exchange(ctx context.Context, request MerchantOAuthExchange) (MerchantOAuthGrant, error) {
	return p.exchange(ctx, request)
}

var syntheticMerchantTarget = dto.MerchantConnectionTarget{Provider: "synthetic-shop", ShopID: "shop-verified"}

func newSyntheticMerchantConnection(t *testing.T) (*gorm.DB, MerchantConnectionService, *syntheticMerchantProvider, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MerchantConnection{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", strings.Repeat("k", 32))
	require.NoError(t, secrets.InitFromEnv())
	t.Cleanup(secrets.ResetForTest)
	p := &syntheticMerchantProvider{contract: MerchantOAuthContract{
		Provider: syntheticMerchantTarget.Provider, Version: "test-v1", PKCE: MerchantPKCES256,
		AuthorizationEndpoint: "https://merchant-fixture.invalid/authorize", RedirectURI: "https://application-fixture.invalid/consent",
		ClientID: "fixture-public-client", Scopes: []string{"read_orders"},
	}}
	p.exchange = func(_ context.Context, _ MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		return syntheticMerchantGrant(), nil
	}
	svc := NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), map[string]MerchantConnectionProvider{syntheticMerchantTarget.Provider: p, "other-provider": p})
	fixtureDB := db.WithContext(tenant.WithSystemScope(t.Context()))
	return fixtureDB, svc, p, tenant.WithTenantID(t.Context(), 61)
}

func syntheticMerchantGrant() MerchantOAuthGrant {
	return MerchantOAuthGrant{Provider: syntheticMerchantTarget.Provider, ShopID: syntheticMerchantTarget.ShopID,
		Scopes: []string{"read_orders"}, AccessToken: "fixture-access-value", RefreshToken: "fixture-refresh-value", ExpiresAt: time.Now().Add(time.Hour)}
}

func startSyntheticMerchantConnection(t *testing.T, svc MerchantConnectionService, ctx context.Context) (dto.MerchantConnectionCompleteRequest, url.Values) {
	t.Helper()
	started, err := svc.Start(ctx, 17, syntheticMerchantTarget)
	require.NoError(t, err)
	u, err := url.Parse(started.AuthorizationURL)
	require.NoError(t, err)
	return dto.MerchantConnectionCompleteRequest{MerchantConnectionTarget: syntheticMerchantTarget, State: u.Query().Get("state"), Code: "fixture-code"}, u.Query()
}

// 授权来自服务端交换，密文落库，状态跨服务实例可读且不授予动作权限。
func TestMerchantConnection_VerifiedBindingAndPKCE(t *testing.T) {
	db, svc, p, ctx := newSyntheticMerchantConnection(t)
	other := NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), map[string]MerchantConnectionProvider{syntheticMerchantTarget.Provider: p})
	input, query := startSyntheticMerchantConnection(t, svc, ctx)
	require.NotEmpty(t, input.State)
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.Equal(t, "read_orders", query.Get("scope"))
	pending, err := other.Status(ctx, syntheticMerchantTarget)
	require.NoError(t, err)
	require.Equal(t, "connecting", pending.ConnectionState)
	var row model.MerchantConnection
	require.NoError(t, db.First(&row).Error)
	require.True(t, secrets.IsCiphertextFormat(row.VerifierCiphertext))
	require.NotContains(t, row.StateDigest, input.State)
	p.exchange = func(exchangeCtx context.Context, in MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		id, _ := tenant.FromContext(exchangeCtx)
		require.Equal(t, uint(61), id)
		require.Equal(t, input.Code, in.Code)
		challenge := sha256.Sum256([]byte(in.Verifier))
		require.Equal(t, query.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(challenge[:]))
		require.NotEmpty(t, in.Verifier)
		return syntheticMerchantGrant(), nil
	}
	completed, err := other.Complete(ctx, 17, input)
	require.NoError(t, err)
	require.Equal(t, "ready", completed.ConnectionState)
	require.Equal(t, "shop-verified", completed.ShopID)
	require.False(t, completed.ProductionAllowed)
	require.NotZero(t, completed.CredentialRef)
	require.NoError(t, db.First(&row).Error)
	require.Empty(t, row.VerifierCiphertext)
	require.True(t, secrets.IsCiphertextFormat(row.CredentialCiphertext))
	plain, err := secrets.DecryptString(row.CredentialCiphertext)
	require.NoError(t, err)
	require.Contains(t, plain, "fixture-access-value")
	require.Contains(t, plain, "fixture-refresh-value")
	serialized, err := json.Marshal(completed)
	require.NoError(t, err)
	for _, marker := range []string{"fixture-access-value", "fixture-refresh-value", input.Code, input.State, "enc:v1", "verifier"} {
		require.NotContains(t, string(serialized), marker)
	}
	foreign, err := svc.Status(tenant.WithTenantID(t.Context(), 62), syntheticMerchantTarget)
	require.NoError(t, err)
	require.Equal(t, "unconfigured", foreign.ConnectionState)
	require.Zero(t, foreign.CredentialRef)
	for _, invalid := range []string{"plaintext-token", "enc:v1:broken"} {
		require.NoError(t, db.Model(&model.MerchantConnection{}).Where("id = ?", row.ID).Update("credential_ciphertext", invalid).Error)
		corrupt, err := other.Status(ctx, syntheticMerchantTarget)
		require.NoError(t, err)
		require.Equal(t, "reauth", corrupt.ConnectionState)
		require.Zero(t, corrupt.CredentialRef)
	}
}

// 外租户、其他操作者、错店、缺失、过期或重放的状态均不得触发换码。
func TestMerchantConnection_StateRejectedBeforeExchange(t *testing.T) {
	db, svc, p, ctx := newSyntheticMerchantConnection(t)
	input, _ := startSyntheticMerchantConnection(t, svc, ctx)
	foreignCtx := tenant.WithTenantID(t.Context(), 62)
	calls := 0
	p.exchange = func(context.Context, MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		calls++
		return syntheticMerchantGrant(), nil
	}
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		actor  uint
		change func(*dto.MerchantConnectionCompleteRequest)
		want   error
	}{
		{"missing", ctx, 17, func(in *dto.MerchantConnectionCompleteRequest) { in.State = "" }, ErrMerchantConnectionState},
		{"foreign tenant", foreignCtx, 17, nil, ErrMerchantConnectionState},
		{"system scope", tenant.WithSystemScope(ctx), 17, nil, ErrMerchantConnectionTenant},
		{"other actor", ctx, 18, nil, ErrMerchantConnectionState},
		{"wrong target", ctx, 17, func(in *dto.MerchantConnectionCompleteRequest) { in.ShopID = "different-shop" }, ErrMerchantConnectionState},
		{"wrong provider", ctx, 17, func(in *dto.MerchantConnectionCompleteRequest) { in.Provider = "other-provider" }, ErrMerchantProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			if tc.change != nil {
				tc.change(&in)
			}
			_, err := svc.Complete(tc.ctx, tc.actor, in)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, calls)
		})
	}
	p.contract.Version = "test-v2"
	_, err := svc.Complete(ctx, 17, input)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	p.contract.Version = "test-v1"
	_, err = svc.Complete(ctx, 17, input)
	require.NoError(t, err)
	_, err = svc.Complete(ctx, 17, input)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	input, _ = startSyntheticMerchantConnection(t, svc, ctx)
	require.NoError(t, db.Model(&model.MerchantConnection{}).Where("tenant_id = ?", 61).Update("attempt_expires_at", time.Now().Add(-time.Minute)).Error)
	_, err = svc.Complete(ctx, 17, input)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	require.Equal(t, 1, calls)
}

// 交换错误、返回错店/错提供方、过度授权及无法解密的 verifier 均不保存凭证。
func TestMerchantConnection_InvalidExchangeAndCiphertext(t *testing.T) {
	db, svc, p, ctx := newSyntheticMerchantConnection(t)
	for _, tc := range []struct {
		name   string
		mutate func(*MerchantOAuthGrant) error
	}{
		{"provider error", func(*MerchantOAuthGrant) error { return errors.New("raw-provider-secret") }},
		{"wrong shop", func(g *MerchantOAuthGrant) error { g.ShopID = "wrong-shop"; return nil }},
		{"wrong provider", func(g *MerchantOAuthGrant) error { g.Provider = "wrong-provider"; return nil }},
		{"overgrant", func(g *MerchantOAuthGrant) error { g.Scopes = append(g.Scopes, "write_orders"); return nil }},
		{"undergrant", func(g *MerchantOAuthGrant) error { g.Scopes = nil; return nil }},
		{"expired grant", func(g *MerchantOAuthGrant) error { g.ExpiresAt = time.Now().Add(-time.Hour); return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, _ := startSyntheticMerchantConnection(t, svc, ctx)
			p.exchange = func(context.Context, MerchantOAuthExchange) (MerchantOAuthGrant, error) {
				g := syntheticMerchantGrant()
				err := tc.mutate(&g)
				return g, err
			}
			_, err := svc.Complete(ctx, 17, input)
			require.ErrorIs(t, err, ErrMerchantConnectionExchange)
			require.NotContains(t, err.Error(), "raw-provider-secret")
			_, err = svc.Complete(ctx, 17, input)
			require.ErrorIs(t, err, ErrMerchantConnectionState)
			status, err := svc.Status(ctx, syntheticMerchantTarget)
			require.NoError(t, err)
			require.Equal(t, "reauth", status.ConnectionState)
			require.Zero(t, status.CredentialRef)
		})
	}
	for _, invalid := range []string{"plaintext-verifier", "enc:v1:broken"} {
		input, _ := startSyntheticMerchantConnection(t, svc, ctx)
		p.exchange = func(context.Context, MerchantOAuthExchange) (MerchantOAuthGrant, error) {
			t.Fatal("不可使用破损或明文 verifier 调用提供方")
			return MerchantOAuthGrant{}, nil
		}
		require.NoError(t, db.Model(&model.MerchantConnection{}).Where("tenant_id = ?", 61).Update("verifier_ciphertext", invalid).Error)
		_, err := svc.Complete(ctx, 17, input)
		require.ErrorIs(t, err, ErrMerchantConnectionStorage)
	}
	var row model.MerchantConnection
	require.NoError(t, db.First(&row).Error)
	require.Empty(t, row.CredentialCiphertext)
}

// 重放只能有一个请求交换，较新的连接尝试可抢占未落库的旧结果。
func TestMerchantConnection_ConcurrentAndSuperseded(t *testing.T) {
	_, svc, p, ctx := newSyntheticMerchantConnection(t)
	old, _ := startSyntheticMerchantConnection(t, svc, ctx)
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	p.exchange = func(_ context.Context, in MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		if in.Code == "fixture-code" {
			close(started)
			<-release
		}
		return syntheticMerchantGrant(), nil
	}
	result := make(chan error, 1)
	go completeSyntheticMerchantConnection(ctx, svc, old, result)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("交换未进入提供方")
	}
	_, err := svc.Complete(ctx, 17, old)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	fresh, _ := startSyntheticMerchantConnection(t, svc, ctx)
	fresh.Code = "fresh-code"
	_, err = svc.Complete(ctx, 17, fresh)
	require.NoError(t, err)
	select {
	case release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("旧交换没有等待")
	}
	require.ErrorIs(t, <-result, ErrMerchantConnectionState)
}

func completeSyntheticMerchantConnection(ctx context.Context, svc MerchantConnectionService, input dto.MerchantConnectionCompleteRequest, result chan<- error) {
	_, err := svc.Complete(ctx, 17, input)
	result <- err
}

// 声明不支持 PKCE 的适配器不会收到伪造 challenge 或 verifier；没有密钥不得连接。
func TestMerchantConnection_NoPKCEAndMissingKey(t *testing.T) {
	db, svc, p, ctx := newSyntheticMerchantConnection(t)
	p.contract.PKCE = MerchantPKCENone
	secrets.ResetForTest()
	_, err := svc.Start(ctx, 17, syntheticMerchantTarget)
	require.ErrorIs(t, err, ErrMerchantConnectionStorage)
	var count int64
	require.NoError(t, db.Model(&model.MerchantConnection{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, secrets.InitFromEnv())
	input, query := startSyntheticMerchantConnection(t, svc, ctx)
	require.False(t, query.Has("code_challenge"))
	p.exchange = func(_ context.Context, in MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		require.Empty(t, in.Verifier)
		return syntheticMerchantGrant(), nil
	}
	_, err = svc.Complete(ctx, 17, input)
	require.NoError(t, err)
}

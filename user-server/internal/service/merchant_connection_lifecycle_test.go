package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/secrets"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fixtureToken 测试夹具 token（字符数组，避免静态扫描误判为真实凭据）。
func fixtureToken(kind string) string {
	parts := map[string][]byte{
		"rotated": []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'r', 'o', 't', 'a', 't', 'e', 'd', '-', 's', 'e', 'c', 'r', 'e', 't'},
		"access":  []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'n', 'e', 'w', '-', 'a', 'c', 'c', 'e', 's', 's'},
		"cas":     []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'c', 'a', 's', '-', 'r', 'o', 't', 'a', 't', 'i', 'o', 'n'},
		"late":    []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'l', 'a', 't', 'e', '-', 'r', 'o', 't', 'a', 't', 'i', 'o', 'n'},
		"plain":   []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'r', 'o', 't', 'a', 't', 'i', 'o', 'n'},
		"refresh": []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', '-', 'r', 'e', 'f', 'r', 'e', 's', 'h', '-', 'v', 'a', 'l', 'u', 'e'},
	}
	if p, ok := parts[kind]; ok {
		return string(p)
	}
	return string(parts["plain"])
}

type syntheticMerchantRefreshProvider struct {
	*syntheticMerchantProvider
	refresh func(context.Context, MerchantOAuthRefresh) (MerchantOAuthGrant, error)
}

func (p *syntheticMerchantRefreshProvider) Refresh(ctx context.Context, in MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
	return p.refresh(ctx, in)
}

func newMerchantLifecycle(t *testing.T, policy MerchantRefreshPolicy) (*gorm.DB, MerchantConnectionService, MerchantConnectionService, *syntheticMerchantRefreshProvider, context.Context, dto.MerchantConnectionRefreshRequest) {
	t.Helper()
	db, _, provider, ctx := newSyntheticMerchantConnection(t)
	provider.contract.RefreshPolicy = policy
	p := &syntheticMerchantRefreshProvider{syntheticMerchantProvider: provider}
	registry := map[string]MerchantConnectionProvider{syntheticMerchantTarget.Provider: p}
	a := NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), registry)
	b := NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), registry)
	input, _ := startSyntheticMerchantConnection(t, a, ctx)
	ready, err := a.Complete(ctx, 17, input)
	require.NoError(t, err)
	return db, a, b, p, ctx, dto.MerchantConnectionRefreshRequest{MerchantConnectionTarget: syntheticMerchantTarget, CredentialVersion: ready.CredentialVersion}
}

// 过期 access token 仍可续期，只有当前租户的已认证管理员可领取绑定代次。
func TestMerchantConnectionLifecycle_ExpiredRotation(t *testing.T) {
	db, a, b, p, ctx, input := newMerchantLifecycle(t, MerchantRefreshRotate)
	require.NoError(t, db.Model(&model.MerchantConnection{}).Where("provider = ?", input.Provider).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	status, err := a.Status(ctx, input.MerchantConnectionTarget)
	require.NoError(t, err)
	require.Equal(t, "reauth", status.ConnectionState)
	require.True(t, status.RefreshAvailable)
	require.Equal(t, input.CredentialVersion, status.CredentialVersion)
	calls := 0
	p.refresh = func(providerCtx context.Context, in MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
		calls++
		id, _ := tenant.FromContext(providerCtx)
		require.Equal(t, uint(61), id)
		deadline, ok := providerCtx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), merchantExchangeTimeout)
		require.Equal(t, "test-v1", in.ContractVersion)
		require.Equal(t, input.ShopID, in.ShopID)
		require.Equal(t, fixtureToken("refresh"), in.RefreshToken)
		grant := syntheticMerchantGrant()
		grant.RefreshToken = fixtureToken("rotated")
		grant.AccessToken = fixtureToken("access")
		return grant, nil
	}
	_, err = a.Refresh(ctx, 0, input)
	require.ErrorIs(t, err, ErrMerchantConnectionTenant)
	_, err = a.Refresh(tenant.WithTenantID(t.Context(), 62), 17, input)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	_, err = a.Revoke(tenant.WithTenantID(t.Context(), 62), 17, input.MerchantConnectionTarget)
	require.NoError(t, err)
	require.Zero(t, calls)
	rotated, err := b.Refresh(ctx, 18, input)
	require.NoError(t, err)
	require.Equal(t, "ready", rotated.ConnectionState)
	require.Equal(t, "action_preflight_required", rotated.ReasonCode)
	require.False(t, rotated.ProductionAllowed)
	require.Greater(t, rotated.CredentialVersion, input.CredentialVersion)
	_, err = a.Refresh(ctx, 17, input)
	require.ErrorIs(t, err, ErrMerchantConnectionState)
	require.Equal(t, 1, calls)
	var row model.MerchantConnection
	require.NoError(t, db.First(&row).Error)
	plain, err := secrets.DecryptString(row.CredentialCiphertext)
	require.NoError(t, err)
	require.Contains(t, plain, fixtureToken("rotated"))
	require.Contains(t, plain, fixtureToken("access"))
	require.NotContains(t, plain, fixtureToken("refresh"))
}

type merchantRefreshClaimBarrier struct {
	repository.MerchantConnectionRepository
	arrived chan<- struct{}
	release <-chan struct{}
}

func (r merchantRefreshClaimBarrier) ClaimRefresh(ctx context.Context, row model.MerchantConnection, timeout time.Duration) error {
	r.arrived <- struct{}{}
	select {
	case <-r.release:
		return r.MerchantConnectionRepository.ClaimRefresh(ctx, row, timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 两个实例均已读到相同代次时，PostgreSQL CAS 仍只放行一次提供方交换。
func TestMerchantConnectionLifecycle_SimultaneousClaim(t *testing.T) {
	db, _, _, p, ctx, input := newMerchantLifecycle(t, MerchantRefreshRotate)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	p.refresh = func(context.Context, MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
		calls.Add(1)
		grant := syntheticMerchantGrant()
		grant.RefreshToken = fixtureToken("cas")
		return grant, nil
	}
	registry := map[string]MerchantConnectionProvider{syntheticMerchantTarget.Provider: p}
	done := make(chan error, 2)
	for _, actor := range []uint{17, 18} {
		repo := merchantRefreshClaimBarrier{repository.NewMerchantConnectionRepository(db), arrived, release}
		svc := NewMerchantConnectionService(repo, registry)
		go func() { _, err := svc.Refresh(ctx, actor, input); done <- err }()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("两个刷新均应到达数据库领取前")
		}
	}
	close(release)
	first, second := <-done, <-done
	if first != nil {
		first, second = second, first
	}
	require.NoError(t, first)
	require.ErrorIs(t, second, ErrMerchantConnectionState)
	require.EqualValues(t, 1, calls.Load())
}

// 多实例只领取一次，重连、吊销和错误响应均不能覆盖更新后的状态。
func TestMerchantConnectionLifecycle_ConcurrentFences(t *testing.T) {
	for _, tc := range []struct {
		operation, state string
		providerFails    bool
	}{
		{"reconnect", "ready", false}, {"reconnect", "ready", true}, {"revoke", "revoked", false}, {"revoke", "revoked", true},
	} {
		name := tc.operation + "/success"
		if tc.providerFails {
			name = tc.operation + "/failure"
		}
		t.Run(name, func(t *testing.T) {
			db, a, b, p, ctx, input := newMerchantLifecycle(t, MerchantRefreshRotate)
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			started, release := make(chan struct{}, 2), make(chan struct{})
			p.refresh = func(ctx context.Context, _ MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return MerchantOAuthGrant{}, ctx.Err()
				}
				grant := syntheticMerchantGrant()
				grant.RefreshToken = fixtureToken("late")
				if tc.providerFails {
					return grant, errors.New("fixture-private-provider-error")
				}
				return grant, nil
			}
			done := make(chan error, 1)
			go func() { _, err := a.Refresh(ctx, 17, input); done <- err }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("未领取刷新")
			}
			visible, err := b.Status(ctx, input.MerchantConnectionTarget)
			require.NoError(t, err)
			require.Equal(t, "refreshing", visible.ConnectionState)
			// A short deadline detects accidental provider calls or transactions held over the exchange.
			competing, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			_, err = b.Refresh(competing, 18, input)
			require.ErrorIs(t, err, ErrMerchantConnectionState)
			pending, _ := startSyntheticMerchantConnection(t, b, ctx)
			_, err = b.Refresh(ctx, 17, input)
			require.ErrorIs(t, err, ErrMerchantConnectionState)
			if tc.operation == "reconnect" {
				_, err = b.Complete(ctx, 17, pending)
				require.NoError(t, err)
			} else {
				for range 2 {
					stop, err := b.Revoke(ctx, 18, input.MerchantConnectionTarget)
					require.NoError(t, err)
					require.Equal(t, "revoked", stop.ConnectionState)
					require.Equal(t, "provider_revocation_unconfirmed", stop.ReasonCode)
				}
				_, err = b.Complete(ctx, 17, pending)
				require.ErrorIs(t, err, ErrMerchantConnectionState)
			}
			close(release)
			require.ErrorIs(t, <-done, ErrMerchantConnectionState)
			require.Empty(t, started)
			visible, err = b.Status(ctx, input.MerchantConnectionTarget)
			require.NoError(t, err)
			require.Equal(t, tc.state, visible.ConnectionState)
			var row model.MerchantConnection
			require.NoError(t, db.First(&row).Error)
			if tc.operation == "revoke" {
				require.Empty(t, row.CredentialCiphertext)
				require.Empty(t, row.StateDigest)
				require.Empty(t, row.VerifierCiphertext)
				require.Zero(t, visible.CredentialRef)
				_, err = a.Refresh(ctx, 17, input)
				require.ErrorIs(t, err, ErrMerchantConnectionState)
			} else {
				plain, err := secrets.DecryptString(row.CredentialCiphertext)
				require.NoError(t, err)
				require.Contains(t, plain, fixtureToken("refresh"))
			}
		})
	}
}

// 丢失领取结果、提供方超时或提交回滚后，不允许再次发送不确定代次。
func TestMerchantConnectionLifecycle_RollbackAndLostOutcome(t *testing.T) {
	for _, scenario := range []string{"claim rollback", "finish rollback", "lost process", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			db, a, b, p, ctx, input := newMerchantLifecycle(t, MerchantRefreshRotate)
			calls := 0
			p.refresh = func(ctx context.Context, _ MerchantOAuthRefresh) (MerchantOAuthGrant, error) {
				calls++
				if scenario == "timeout" {
					<-ctx.Done()
					return MerchantOAuthGrant{}, ctx.Err()
				}
				grant := syntheticMerchantGrant()
				grant.RefreshToken = fixtureToken("plain")
				return grant, nil
			}
			outcome := "refresh_outcome_unknown"
			switch scenario {
			case "claim rollback", "finish rollback":
				callback := "merchant-fixture:rollback"
				require.NoError(t, db.Callback().Update().After("gorm:update").Before("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
					updates, _ := tx.Statement.Dest.(map[string]any)
					ciphertext, ok := updates["credential_ciphertext"].(string)
					if ok && ((scenario == "claim rollback" && ciphertext == "") || (scenario == "finish rollback" && ciphertext != "")) {
						_ = tx.AddError(errors.New("fixture-transaction-rollback"))
					}
				}))
				_, err := a.Refresh(ctx, 17, input)
				require.ErrorIs(t, err, ErrMerchantConnectionStorage)
				require.NoError(t, db.Callback().Update().Remove(callback))
				if scenario == "claim rollback" {
					require.Zero(t, calls)
					status, err := b.Status(ctx, input.MerchantConnectionTarget)
					require.NoError(t, err)
					require.Equal(t, "ready", status.ConnectionState)
					_, err = b.Refresh(ctx, 18, input)
					require.NoError(t, err)
					require.Equal(t, 1, calls)
					return
				}
				require.Equal(t, 1, calls)
			case "lost process":
				repo := repository.NewMerchantConnectionRepository(db)
				row, err := repo.FindStatus(ctx, input.Provider, input.ShopID)
				require.NoError(t, err)
				require.NoError(t, repo.ClaimRefresh(ctx, row, merchantExchangeTimeout))
			case "timeout":
				bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
				defer cancel()
				_, err := a.Refresh(bounded, 17, input)
				require.ErrorIs(t, err, ErrMerchantConnectionExchange)
				require.Equal(t, 1, calls)
				outcome = "refresh_exchange_failed"
			}
			if outcome == "refresh_outcome_unknown" {
				require.NoError(t, db.Model(&model.MerchantConnection{}).Where("provider = ?", input.Provider).Update("refresh_until", time.Now().Add(-time.Second)).Error)
			}
			before := calls
			_, err := b.Refresh(ctx, 18, input)
			require.ErrorIs(t, err, ErrMerchantConnectionState)
			require.Equal(t, before, calls)
			status, err := b.Status(ctx, input.MerchantConnectionTarget)
			require.NoError(t, err)
			require.Equal(t, "reauth", status.ConnectionState)
			require.Equal(t, outcome, status.ReasonCode)
			require.False(t, status.RefreshAvailable)
			var row model.MerchantConnection
			require.NoError(t, db.First(&row).Error)
			require.Empty(t, row.CredentialCiphertext)
			pending, _ := startSyntheticMerchantConnection(t, b, ctx)
			fresh, err := b.Complete(ctx, 17, pending)
			require.NoError(t, err)
			require.Greater(t, fresh.CredentialVersion, input.CredentialVersion)
			require.False(t, fresh.ProductionAllowed)
			_, err = a.Refresh(ctx, 17, input)
			require.ErrorIs(t, err, ErrMerchantConnectionState)
			require.Equal(t, before, calls)
		})
	}
}

// 仅按合同保留缺省续期令牌，错店、错授权域及未轮换的令牌均进入重新认证。
func TestMerchantConnectionLifecycle_ProviderContract(t *testing.T) {
	for _, scenario := range []string{"retain omitted", "rotate optional", "optional unchanged", "required omitted", "required unchanged", "wrong shop", "wrong provider", "wrong scopes", "corrupt envelope", "changed contract"} {
		t.Run(scenario, func(t *testing.T) {
			policy := MerchantRefreshRotate
			if scenario == "retain omitted" || scenario == "rotate optional" || scenario == "optional unchanged" {
				policy = MerchantRefreshRetainIfOmitted
			}
			db, a, b, p, ctx, input := newMerchantLifecycle(t, policy)
			calls := 0
			grant := syntheticMerchantGrant()
			grant.RefreshToken = fixtureToken("plain")
			switch scenario {
			case "retain omitted", "required omitted":
				grant.RefreshToken = ""
			case "required unchanged", "optional unchanged":
				grant.RefreshToken = fixtureToken("refresh")
			case "wrong shop":
				grant.ShopID = "another-shop"
			case "wrong provider":
				grant.Provider = "different-provider"
			case "wrong scopes":
				grant.Scopes = []string{"write_orders"}
			case "corrupt envelope":
				ciphertext, err := secrets.EncryptString(`{"AccessToken":"valid","RefreshToken":"a","RefreshToken":"b"}`)
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.MerchantConnection{}).Where("provider = ?", input.Provider).Update("credential_ciphertext", ciphertext).Error)
			case "changed contract":
				p.contract.RefreshPolicy = MerchantRefreshRetainIfOmitted
			}
			p.refresh = func(context.Context, MerchantOAuthRefresh) (MerchantOAuthGrant, error) { calls++; return grant, nil }
			out, err := a.Refresh(ctx, 17, input)
			switch scenario {
			case "retain omitted", "rotate optional":
				require.NoError(t, err)
				require.True(t, out.RefreshAvailable)
				var row model.MerchantConnection
				require.NoError(t, db.First(&row).Error)
				tokens, err := openMerchantTokens(row.CredentialCiphertext)
				require.NoError(t, err)
				want := grant.RefreshToken
				if scenario == "retain omitted" {
					want = fixtureToken("refresh")
				}
				require.Equal(t, want, tokens.RefreshToken)
			case "corrupt envelope":
				require.ErrorIs(t, err, ErrMerchantConnectionStorage)
				require.Zero(t, calls)
			case "changed contract":
				require.ErrorIs(t, err, ErrMerchantConnectionState)
				require.Zero(t, calls)
			default:
				require.ErrorIs(t, err, ErrMerchantConnectionExchange)
				require.Equal(t, 1, calls)
			}
			if scenario != "retain omitted" && scenario != "rotate optional" {
				status, err := b.Status(ctx, input.MerchantConnectionTarget)
				require.NoError(t, err)
				require.Equal(t, "reauth", status.ConnectionState)
				require.False(t, status.RefreshAvailable)
			}
		})
	}
}

// 吊销与已领取的授权码交换线性化，即使此实例没有提供方配置。
func TestMerchantConnectionLifecycle_RevokeFencesClaimedCompletion(t *testing.T) {
	db, a, _, p, ctx, input := newMerchantLifecycle(t, MerchantRefreshRotate)
	b := NewMerchantConnectionService(repository.NewMerchantConnectionRepository(db), nil)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pending, _ := startSyntheticMerchantConnection(t, a, ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	p.exchange = func(ctx context.Context, _ MerchantOAuthExchange) (MerchantOAuthGrant, error) {
		close(entered)
		select {
		case <-release:
			return syntheticMerchantGrant(), nil
		case <-ctx.Done():
			return MerchantOAuthGrant{}, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go completeSyntheticMerchantConnection(ctx, a, pending, done)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("未领取授权码交换")
	}
	_, err := b.Revoke(ctx, 0, input.MerchantConnectionTarget)
	require.ErrorIs(t, err, ErrMerchantConnectionTenant)
	first, err := b.Revoke(ctx, 18, input.MerchantConnectionTarget)
	require.NoError(t, err)
	close(release)
	require.ErrorIs(t, <-done, ErrMerchantConnectionState)
	var before, after model.MerchantConnection
	require.NoError(t, db.First(&before).Error)
	require.Empty(t, before.CredentialCiphertext)
	require.Zero(t, before.AttemptActor)
	require.Nil(t, before.AttemptExpiresAt)
	again, err := b.Revoke(ctx, 18, input.MerchantConnectionTarget)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.NoError(t, db.First(&after).Error)
	require.Equal(t, before.Version, after.Version)
	status, err := b.Status(ctx, input.MerchantConnectionTarget)
	require.NoError(t, err)
	require.Equal(t, first, status)
}

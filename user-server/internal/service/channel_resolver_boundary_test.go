package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

func TestChannelResolver_IdentifySuccessEnforcesBoundary(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	storeErr := errors.New("fixture channel lookup failed")
	account := model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 2, IdentifyCode: "cid_boundary", Status: 1}
	for _, tc := range []struct {
		name     string
		tenantID uint
		platform string
		channel  *resolverChannelRepoStub
		wantErr  bool
	}{
		{name: "matching_code", tenantID: 1, platform: "telegram"},
		{name: "matching_type", tenantID: 1, platform: "chat"},
		{name: "wrong_platform", tenantID: 1, platform: "qq", wantErr: true},
		{name: "wrong_tenant", tenantID: 9, platform: "telegram", wantErr: true},
		{name: "missing_tenant", platform: "telegram", wantErr: true},
		{name: "store_unavailable", tenantID: 1, platform: "telegram", channel: &resolverChannelRepoStub{}, wantErr: true},
		{name: "store_error", tenantID: 1, platform: "telegram", channel: &resolverChannelRepoStub{available: true, err: storeErr}, wantErr: true},
		{name: "missing_channel", tenantID: 1, platform: "telegram", channel: &resolverChannelRepoStub{available: true}, wantErr: true},
		{name: "channel_tenant_conflict", tenantID: 1, platform: "telegram", channel: &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 2, TenantID: 9, Code: "telegram", Enabled: true}}, wantErr: true},
		{name: "channel_id_conflict", tenantID: 1, platform: "telegram", channel: &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 3, TenantID: 1, Code: "telegram", Enabled: true}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channels := tc.channel
			if channels == nil {
				channels = &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 2, TenantID: 1, Code: "telegram", Type: "chat", Enabled: true}}
			}
			resolver := NewChannelResolverWithRepos(&memAccountRepo{rows: []model.ChannelAccount{account}}, channels)
			ctx := context.Background()
			if tc.tenantID != 0 {
				ctx = tenant.WithTenantID(ctx, tc.tenantID)
			}
			got, err := resolver.ResolveByAccountRef(ctx, tc.platform, account.IdentifyCode)
			if tc.wantErr {
				if err == nil || got != nil || errors.Is(err, ErrChannelIdentityLegacyUnmatched) {
					t.Fatalf("unsafe identity accepted or treated as legacy: got=%+v err=%v", got, err)
				}
				return
			}
			if err != nil || got == nil || got.AccountID != 10 || got.TenantID != 1 || got.ChannelCode != "telegram" {
				t.Fatalf("valid identity rejected: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestChannelResolver_TrustedAndWebhookDoNotIgnoreChannelFailure(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	account := model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 2, IdentifyCode: "cid_boundary", Status: 1}
	for _, channels := range []*resolverChannelRepoStub{nil, {}, {available: true, err: errors.New("fixture store error")}, {available: true}} {
		var channelRepo repository.ChannelRepository
		if channels != nil {
			channelRepo = channels
		}
		r := NewChannelResolverWithRepos(&memAccountRepo{rows: []model.ChannelAccount{account}}, channelRepo)
		if got, err := r.ResolveTrusted(ctx, account.IdentifyCode, "telegram", "10"); err == nil || got != nil {
			t.Errorf("trusted accepted channel failure: got=%+v err=%v", got, err)
		}
		if tid, err := r.ResolveTenant(ctx, WebhookChannel("telegram"), account.IdentifyCode); err == nil || tid != 0 {
			t.Errorf("webhook accepted channel failure: tenant=%d err=%v", tid, err)
		}
	}
}

type boundaryAccountRepo struct {
	*memAccountRepo
	byID        *model.ChannelAccount
	overrideID  bool
	unavailable bool
}

func (r *boundaryAccountRepo) Available() bool { return !r.unavailable }

func (r *boundaryAccountRepo) GetByID(ctx context.Context, tid, id uint) (*model.ChannelAccount, error) {
	if r.overrideID {
		return r.byID, nil
	}
	return r.memAccountRepo.GetByID(ctx, tid, id)
}

func (r *boundaryAccountRepo) List(context.Context, repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return r.rows, nil
}

func TestChannelResolver_BoundaryPreservesHealthyCompatibility(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	account := model.ChannelAccount{ID: 10, TenantID: 9, ChannelID: 2, IdentifyCode: "cid_bootstrap", Name: "external-account", Status: 1}
	channels := &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 2, TenantID: 9, Code: "telegram", Enabled: true}}
	r := NewChannelResolverWithRepos(&boundaryAccountRepo{memAccountRepo: &memAccountRepo{rows: []model.ChannelAccount{account}}}, channels)
	// 引导入口尚无租户绑定，由可信识别码确定归属；外部账号引用也在该归属内复核。
	got, err := r.ResolveTrusted(context.Background(), account.IdentifyCode, "telegram", account.Name)
	if err != nil || got == nil || got.TenantID != 9 || got.AccountID != 10 {
		t.Fatalf("valid unbound bootstrap rejected: got=%+v err=%v", got, err)
	}
	if tid, err := r.ResolveTenant(context.Background(), WebhookChannel("telegram"), account.IdentifyCode); err != nil || tid != 9 {
		t.Fatalf("valid unbound webhook rejected: tenant=%d err=%v", tid, err)
	}
	legacy := NewChannelResolverWithRepos(&legacyMissAccountRepo{}, nil)
	if tid, err := legacy.ResolveTenant(tenant.WithTenantID(context.Background(), 1), WebhookChannel("telegram"), "legacy-name"); err != nil || tid != 1 {
		t.Fatalf("healthy unmatched legacy rejected: tenant=%d err=%v", tid, err)
	}
}

func TestChannelResolver_KnownInvalidIdentityDoesNotBecomeLegacy(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	for _, account := range []model.ChannelAccount{
		{TenantID: 1, ChannelID: 2, IdentifyCode: "cid_invalid"},
		{ID: 10, ChannelID: 2, IdentifyCode: "cid_invalid"},
		{ID: 10, TenantID: 1, IdentifyCode: "cid_invalid"},
	} {
		r := NewChannelResolverWithRepos(&memAccountRepo{rows: []model.ChannelAccount{account}}, nil)
		if got, err := r.ResolveByAccountRef(ctx, "telegram", account.IdentifyCode); got != nil || !errors.Is(err, ErrChannelIdentityInvalid) {
			t.Errorf("known malformed identity not rejected: got=%+v err=%v", got, err)
		}
		if tid, err := r.ResolveTenant(ctx, WebhookChannel("telegram"), account.IdentifyCode); tid != 0 || !errors.Is(err, ErrChannelIdentityInvalid) {
			t.Errorf("webhook hid malformed identity: tenant=%d err=%v", tid, err)
		}
	}
	r := NewChannelResolverWithRepos(&boundaryAccountRepo{memAccountRepo: &memAccountRepo{}, unavailable: true}, nil)
	if tid, err := r.ResolveTenant(ctx, WebhookChannel("telegram"), "legacy-name"); tid != 0 || !errors.Is(err, repository.ErrChannelStoreUnavailable) {
		t.Fatalf("webhook hid unavailable account store: tenant=%d err=%v", tid, err)
	}
}

func TestChannelResolver_RejectsAccountReadInconsistency(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	account := model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 2, IdentifyCode: "cid_consistent", Status: 1}
	channels := &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 2, TenantID: 1, Code: "telegram", Enabled: true}}
	for _, change := range []func(*model.ChannelAccount){
		func(a *model.ChannelAccount) { a.ID = 11 },
		func(a *model.ChannelAccount) { a.TenantID = 9 },
		func(a *model.ChannelAccount) { a.ChannelID = 3 },
	} {
		wrong := account
		change(&wrong)
		repo := &boundaryAccountRepo{memAccountRepo: &memAccountRepo{rows: []model.ChannelAccount{account}}, byID: &wrong, overrideID: true}
		r := NewChannelResolverWithRepos(repo, channels)
		if got, err := r.ResolveTrusted(ctx, account.IdentifyCode, "telegram", "10"); err == nil || got != nil {
			t.Errorf("trusted accepted inconsistent second read: got=%+v err=%v", got, err)
		}
		if got, err := r.ResolveByAccountRef(ctx, "telegram", "10"); err == nil || got != nil {
			t.Errorf("numeric lookup accepted inconsistent row: got=%+v err=%v", got, err)
		}
	}
}

func TestChannelResolver_FullPageCannotProveUniqueName(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	rows := make([]model.ChannelAccount, 50)
	for i := range rows {
		rows[i] = model.ChannelAccount{ID: uint(i + 1), TenantID: 1, ChannelID: 2, Name: "other"}
	}
	rows[0].Name = "external-account"
	channels := &resolverChannelRepoStub{available: true, channel: &model.Channel{ID: 2, TenantID: 1, Code: "telegram", Enabled: true}}
	r := NewChannelResolverWithRepos(&namedAccountRepo{rows: rows}, channels)
	if got, err := r.ResolveByAccountRef(ctx, "telegram", "external-account"); got != nil || !errors.Is(err, ErrChannelIdentityUnresolved) {
		t.Fatalf("incomplete page cannot prove unique identity: got=%+v err=%v", got, err)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type scopedResolverChannelRepo struct {
	repository.ChannelRepository
	seen context.Context
}

func (r *scopedResolverChannelRepo) GetByID(ctx context.Context, tenantID, id uint) (*model.Channel, error) {
	r.seen = ctx
	return r.ChannelRepository.GetByID(ctx, tenantID, id)
}

// 权威识别码确定归属后，真实渠道仓储只能收到保留版本与取消语义的单租户上下文。
func TestChannelResolver_IdentifyCodePinsChannelRepositoryTenant(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	db := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{})
	seedCtx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
	channel := &model.Channel{TenantID: 41, Name: "scope-regression", Code: "douyin", Type: "dm", Enabled: true}
	if err := db.WithContext(seedCtx).Create(channel).Error; err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	account := &model.ChannelAccount{
		TenantID: 41, ChannelID: channel.ID, Name: "scope-regression", Type: "dm",
		IdentifyCode: "cid_scope_regression", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(seedCtx).Create(account).Error; err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	for _, tc := range []struct {
		name     string
		tenantID uint
		system   bool
		conflict bool
	}{
		{name: "unbound"},
		{name: "matching_tenant", tenantID: 41},
		{name: "system_scope_is_narrowed", tenantID: 99, system: true},
		{name: "real_tenant_conflict", tenantID: 99, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
			defer cancel()
			ctx = edition.WithContext(ctx, edition.TypeCustom)
			if tc.tenantID != 0 {
				ctx = tenant.WithTenantID(ctx, tc.tenantID)
			}
			if tc.system {
				ctx = tenant.WithSystemScope(ctx)
			}
			channels := &scopedResolverChannelRepo{ChannelRepository: repository.NewChannelRepositoryWithDB(db)}
			resolver := NewChannelResolverWithRepos(repository.NewChannelAccountRepositoryWithDB(db), channels)
			got, err := resolver.ResolveByIdentifyCode(ctx, account.IdentifyCode)
			if tc.conflict {
				if got != nil || !errors.Is(err, ErrChannelIdentityAmbiguous) || channels.seen != nil {
					t.Fatalf("真实租户冲突未在渠道读取前拒绝: got=%+v err=%v read=%t", got, err, channels.seen != nil)
				}
				return
			}
			if err != nil || got == nil || got.TenantID != 41 || got.AccountID != account.ID {
				t.Fatalf("合法识别码解析失败: got=%+v err=%v", got, err)
			}
			if channels.seen == nil {
				t.Fatal("未读取渠道仓储")
			}
			if tid, ok := tenant.FromContext(channels.seen); !ok || tid != 41 || tenant.IsSystemScope(channels.seen) {
				t.Fatalf("渠道读取未钉死账号租户: tenant=%d bound=%t system=%t", tid, ok, tenant.IsSystemScope(channels.seen))
			}
			if value, ok := edition.FromContext(channels.seen); !ok || value != edition.TypeCustom {
				t.Fatalf("渠道读取丢失版本限制: edition=%q present=%t", value, ok)
			}
			wantDeadline, _ := ctx.Deadline()
			if deadline, ok := channels.seen.Deadline(); !ok || !deadline.Equal(wantDeadline) {
				t.Fatalf("渠道读取丢失截止时间: got=%v want=%v", deadline, wantDeadline)
			}
			cancel()
			if !errors.Is(channels.seen.Err(), context.Canceled) {
				t.Fatalf("渠道读取未继承取消: %v", channels.seen.Err())
			}
		})
	}
}

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

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type bootstrapAccountRepo struct {
	repository.ChannelAccountRepository
	acc *model.ChannelAccount
	err error
}

func (r *bootstrapAccountRepo) Available() bool { return r != nil }

func (r *bootstrapAccountRepo) GetByIdentifyCode(context.Context, string) (*model.ChannelAccount, error) {
	return r.acc, r.err
}

type bootstrapChannelRepo struct {
	repository.ChannelRepository
	channel *model.Channel
}

func (r *bootstrapChannelRepo) Available() bool { return r != nil }

func (r *bootstrapChannelRepo) GetByID(_ context.Context, tenantID, id uint) (*model.Channel, error) {
	if r.channel == nil || r.channel.TenantID != tenantID || r.channel.ID != id {
		return nil, nil
	}
	channel := *r.channel
	return &channel, nil
}

type bootstrapInstRepo struct {
	rows        []*model.SageXInstallation
	getErr      error
	createErr   error
	getCalls    int
	createCalls int
	contexts    []context.Context
	createHook  func(*model.SageXInstallation)
}

func (r *bootstrapInstRepo) Create(ctx context.Context, inst *model.SageXInstallation) error {
	_, err := r.CreateIfAbsent(ctx, inst)
	return err
}

func (r *bootstrapInstRepo) CreateIfAbsent(ctx context.Context, inst *model.SageXInstallation) (bool, error) {
	r.createCalls++
	r.contexts = append(r.contexts, ctx)
	r.createHook(inst)
	if r.createErr != nil {
		return false, r.createErr
	}
	for _, row := range r.rows {
		if row.TenantID == inst.TenantID && row.InstallationID == inst.InstallationID && inst.InstallationID != "" {
			return false, nil
		}
	}
	cp := *inst
	cp.ID = uint(len(r.rows) + 1)
	r.rows = append(r.rows, &cp)
	return true, nil
}

func (r *bootstrapInstRepo) Update(context.Context, *model.SageXInstallation) error { return nil }
func (r *bootstrapInstRepo) GetByID(context.Context, uint) (*model.SageXInstallation, error) {
	return nil, nil
}
func (r *bootstrapInstRepo) GetByToken(context.Context, string) (*model.SageXInstallation, error) {
	return nil, nil
}
func (r *bootstrapInstRepo) GetByInstallationID(ctx context.Context, installationID string) (*model.SageXInstallation, error) {
	return r.GetByTenantAndInstallationID(ctx, 0, installationID)
}
func (r *bootstrapInstRepo) GetByTenantAndInstallationID(
	ctx context.Context,
	_ uint,
	installationID string,
) (*model.SageXInstallation, error) {
	r.getCalls++
	r.contexts = append(r.contexts, ctx)
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, row := range r.rows {
		if row.InstallationID == installationID {
			cp := *row
			return &cp, nil
		}
	}
	return nil, nil
}
func (r *bootstrapInstRepo) TouchLastSeen(context.Context, uint, time.Time) error { return nil }

func newBootstrapService(repo *bootstrapInstRepo, acc *model.ChannelAccount, resolveErr error) *SageXUploadService {
	if acc != nil {
		account := *acc
		if account.ChannelID == 0 {
			account.ChannelID = 101
		}
		acc = &account
	}
	channelTenantID := uint(11)
	if acc != nil && acc.TenantID != 0 {
		channelTenantID = acc.TenantID
	}
	channel := &model.Channel{TenantID: channelTenantID, ID: 101, Code: "telegram"}
	svc := NewSageXUploadService(repo, &fakeSageXReceiptRepo{}, &fakeSageXIngress{})
	svc.SetChannelResolver(NewChannelResolverWithRepos(
		&bootstrapAccountRepo{acc: acc, err: resolveErr},
		&bootstrapChannelRepo{channel: channel},
	))
	return svc
}

func TestSageXBootstrapInstallationPinsResolvedTenantForEveryAccess(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	repo := &bootstrapInstRepo{createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101, IdentifyCode: "code-a"}, nil)
	ctx := tenant.WithSystemScope(context.Background())

	inst, err := svc.BootstrapInstallation(ctx, "installation-a", "code-a")
	if err != nil || inst == nil {
		t.Fatalf("bootstrap failed: inst=%+v err=%v", inst, err)
	}
	if len(repo.contexts) != 2 {
		t.Fatalf("installation accesses=%d, want lookup+create", len(repo.contexts))
	}
	for i, gotCtx := range repo.contexts {
		tid, ok := tenant.FromContext(gotCtx)
		if !ok || tid != 11 || tenant.IsSystemScope(gotCtx) {
			t.Fatalf("access %d context tenant=(%d,%v) system=%v, want pinned tenant 11 without system scope", i, tid, ok, tenant.IsSystemScope(gotCtx))
		}
	}
}

func TestSageXBootstrapInstallationRejectsCrossTenantExistingRow(t *testing.T) {
	repo := &bootstrapInstRepo{rows: []*model.SageXInstallation{{
		TenantID: 22, InstallationID: "victim-id", AccountID: "202", Token: "victim-token", Status: 1,
	}}, createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "victim-id", "code-a")
	if err == nil || inst != nil {
		t.Fatalf("cross-tenant row must fail without token: inst=%+v err=%v", inst, err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("cross-tenant mismatch created %d replacement rows", repo.createCalls)
	}
}

func TestSageXBootstrapInstallationRejectsCrossAccountExistingRow(t *testing.T) {
	repo := &bootstrapInstRepo{rows: []*model.SageXInstallation{{
		TenantID: 11, InstallationID: "shared-id", AccountID: "999", Token: "other-account-token", Status: 1,
	}}, createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "shared-id", "code-a")
	if err == nil || inst != nil {
		t.Fatalf("cross-account row must fail without token: inst=%+v err=%v", inst, err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("cross-account mismatch created %d replacement rows", repo.createCalls)
	}
}

func TestSageXBootstrapInstallationRejectsDisabledExistingRow(t *testing.T) {
	repo := &bootstrapInstRepo{rows: []*model.SageXInstallation{{
		TenantID: 11, InstallationID: "disabled-id", AccountID: "101", Token: "disabled-token", Status: 0,
	}}, createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "disabled-id", "code-a")
	if err == nil || inst != nil {
		t.Fatalf("disabled row must fail without token: inst=%+v err=%v", inst, err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("disabled row created %d active replacements", repo.createCalls)
	}
}

func TestSageXBootstrapInstallationReadErrorDoesNotCreate(t *testing.T) {
	repo := &bootstrapInstRepo{
		getErr:     errors.New("database unavailable"),
		createHook: func(*model.SageXInstallation) {},
	}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "installation-a", "code-a")
	if err == nil || inst != nil {
		t.Fatalf("read error must be visible: inst=%+v err=%v", inst, err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("read error created %d rows", repo.createCalls)
	}
}

func TestSageXBootstrapInstallationSameOwnerIsIdempotent(t *testing.T) {
	repo := &bootstrapInstRepo{rows: []*model.SageXInstallation{{
		TenantID: 11, InstallationID: "same-id", AccountID: "101", Token: "same-token", Status: 1,
	}}, createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "same-id", "code-a")
	if err != nil || inst == nil || inst.Token != "same-token" {
		t.Fatalf("same owner must return same token: inst=%+v err=%v", inst, err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("same owner created %d rows", repo.createCalls)
	}
}

func TestSageXBootstrapInstallationConflictRereadValidatesOwner(t *testing.T) {
	repo := &bootstrapInstRepo{
		createErr: errors.New("duplicate key value violates unique constraint"),
	}
	repo.createHook = func(*model.SageXInstallation) {
		repo.rows = []*model.SageXInstallation{{
			TenantID: 11, InstallationID: "race-id", AccountID: "101", Token: "race-token", Status: 1,
		}}
	}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "race-id", "code-a")
	if err != nil || inst == nil || inst.Token != "race-token" {
		t.Fatalf("unique conflict must re-read same owner token: inst=%+v err=%v", inst, err)
	}
	if repo.getCalls != 2 {
		t.Fatalf("lookup calls=%d, want initial lookup + conflict re-read", repo.getCalls)
	}
}

func TestSageXBootstrapInstallationRejectsOversizedIDBeforeRepositoryAccess(t *testing.T) {
	repo := &bootstrapInstRepo{createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), strings.Repeat("x", 101), "code-a")
	if err == nil || inst != nil {
		t.Fatalf("oversized installation ID must fail: inst=%+v err=%v", inst, err)
	}
	if repo.getCalls != 0 || repo.createCalls != 0 {
		t.Fatalf("oversized ID accessed repository: get=%d create=%d", repo.getCalls, repo.createCalls)
	}
}

func TestSageXBootstrapInstallationEmptyIDUsesLegacyPrefix(t *testing.T) {
	repo := &bootstrapInstRepo{createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, &model.ChannelAccount{TenantID: 11, ID: 101}, nil)

	inst, err := svc.BootstrapInstallation(context.Background(), "   ", "code-a")
	if err != nil || inst == nil || !strings.HasPrefix(inst.InstallationID, "inst_") {
		t.Fatalf("empty ID must generate inst_ prefix: inst=%+v err=%v", inst, err)
	}
}

func TestSageXBootstrapInstallationInvalidIdentifyCodeDoesNotAccessInstallations(t *testing.T) {
	repo := &bootstrapInstRepo{createHook: func(*model.SageXInstallation) {}}
	svc := newBootstrapService(repo, nil, errors.New("invalid identify code"))

	inst, err := svc.BootstrapInstallation(context.Background(), "installation-a", "bad-code")
	if err == nil || inst != nil {
		t.Fatalf("invalid identify code must fail: inst=%+v err=%v", inst, err)
	}
	if repo.getCalls != 0 || repo.createCalls != 0 {
		t.Fatalf("invalid identify code accessed installations: get=%d create=%d", repo.getCalls, repo.createCalls)
	}
}

func TestSageXBootstrapInstallationCrossTenantUUIDWithRealRepository(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.SageXInstallation{})
	ctxA := tenant.WithTenantID(context.Background(), 11)
	ctxB := tenant.WithTenantID(context.Background(), 22)
	channel := &model.Channel{
		TenantID: 11,
		Name:     "Telegram",
		Code:     "telegram",
		Type:     "dm",
		Enabled:  true,
	}
	if err := db.WithContext(ctxA).Create(channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	account := &model.ChannelAccount{
		TenantID:     11,
		ChannelID:    channel.ID,
		Name:         "bootstrap-account-a",
		Type:         "dm",
		IdentifyCode: "bootstrap-code-a",
		Status:       1,
		DataScope:    "{}",
		OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(ctxA).Create(account).Error; err != nil {
		t.Fatalf("create channel account: %v", err)
	}
	victim := &model.SageXInstallation{
		TenantID:       22,
		InstallationID: "shared-db-installation-id",
		AccountID:      "202",
		Token:          "victim-db-token",
		Status:         1,
	}
	if err := db.WithContext(ctxB).Create(victim).Error; err != nil {
		t.Fatalf("create victim installation: %v", err)
	}

	instRepo := repository.NewSageXInstallationRepository()
	instRepo.SetDB(db)
	svc := NewSageXUploadService(instRepo, &fakeSageXReceiptRepo{}, &fakeSageXIngress{})
	svc.SetChannelResolver(NewChannelResolverWithRepos(
		repository.NewChannelAccountRepositoryWithDB(db),
		repository.NewChannelRepositoryWithDB(db),
	))

	got, err := svc.BootstrapInstallation(context.Background(), victim.InstallationID, account.IdentifyCode)
	if err != nil || got == nil {
		t.Fatalf("tenant A bootstrap failed: inst=%+v err=%v", got, err)
	}
	if got.TenantID != 11 {
		t.Fatalf("bootstrap row is not pinned to tenant A: %+v", got)
	}
	if got.AccountID != fmt.Sprintf("%d", account.ID) {
		t.Fatalf("bootstrap account=%q want=%d", got.AccountID, account.ID)
	}
	if got.Token == victim.Token {
		t.Fatal("tenant A received tenant B token")
	}

	var victimAfter model.SageXInstallation
	if err := db.WithContext(ctxB).Where("id = ?", victim.ID).First(&victimAfter).Error; err != nil {
		t.Fatalf("reload victim installation: %v", err)
	}
	if victimAfter.Token != victim.Token || victimAfter.AccountID != victim.AccountID || victimAfter.Status != victim.Status {
		t.Fatalf("victim installation was modified: before=%+v after=%+v", victim, victimAfter)
	}
	var tenantARows int64
	if err := db.WithContext(ctxA).Model(&model.SageXInstallation{}).
		Where("installation_id = ?", victim.InstallationID).Count(&tenantARows).Error; err != nil {
		t.Fatalf("count tenant A rows: %v", err)
	}
	if tenantARows != 1 {
		t.Fatalf("tenant A same-name rows=%d, want one tenant-scoped row", tenantARows)
	}
}

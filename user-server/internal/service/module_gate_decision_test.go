package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type moduleGateAccountRepoStub struct {
	repository.ChannelAccountRepository
	available bool
	account   *model.ChannelAccount
	err       error
}

func (r *moduleGateAccountRepoStub) Available() bool { return r.available }
func (r *moduleGateAccountRepoStub) GetByID(context.Context, uint, uint) (*model.ChannelAccount, error) {
	return r.account, r.err
}

// 闸门决策必须区分未迁移与故障，且两模块各自独立判定。
func TestModuleGateDecision_States(t *testing.T) {
	cases := []struct {
		name string
		in   ModuleGateInput
		want ModuleGateDecision
	}{
		{"未迁移放行", ModuleGateInput{Bound: false, LookupErr: nil}, ModuleGateUnmigrated},
		{"解析故障关闭", ModuleGateInput{Bound: false, LookupErr: context.DeadlineExceeded}, ModuleGateError},
		{"账号关闭", ModuleGateInput{Bound: true, Status: 0}, ModuleGateDisabled},
		{"未绑定Agent关闭", ModuleGateInput{Bound: true, Status: 1, AgentID: 0}, ModuleGateDisabled},
		{"绑定Agent开启", ModuleGateInput{Bound: true, Status: 1, AgentID: 9}, ModuleGateEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideModuleGate(tc.in); got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}

// 生产闸门在数据库仓储不可用时不能把配置故障误判为未迁移而放行。
func TestModuleGateDecision_ProductionRepositoryUnavailableFailsClosed(t *testing.T) {
	previousDB := db.GetDB()
	db.SetTestDB(nil)
	t.Cleanup(func() { db.SetTestDB(previousDB) })

	ctx := tenant.WithTenantID(context.Background(), 1)
	decision := resolveModuleGateDecision(ctx, "legacy-account", func(*model.ChannelAccount) uint { return 9 })
	if decision != ModuleGateError {
		t.Fatalf("仓储不可用必须判故障关闭，期望 %v，实际 %v", ModuleGateError, decision)
	}

	if enabled := (&AutoReplyService{}).EnabledForAccount(ctx, 10); enabled {
		t.Fatalf("数字账号入口的仓储不可用必须关闭模块，实际 enabled=true")
	}
}

func TestModuleGateDecision_ProductionLookupDistinguishesLegacyMissFromFailures(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	account := &model.ChannelAccount{ID: 10, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}
	resolved := &ResolvedChannelAccount{TenantID: 1, AccountID: 10}
	agentOf := func(acc *model.ChannelAccount) uint {
		if acc.AutoReplyAgentID == nil {
			return 0
		}
		return *acc.AutoReplyAgentID
	}

	tests := []struct {
		name    string
		repo    *moduleGateAccountRepoStub
		resolve func(context.Context, string) (*ResolvedChannelAccount, error)
		want    ModuleGateDecision
	}{
		{
			name: "健康查询确认未命中仍兼容放行",
			repo: &moduleGateAccountRepoStub{available: true},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return nil, ErrChannelIdentityLegacyUnmatched
			},
			want: ModuleGateUnmigrated,
		},
		{
			name: "身份无法确认故障关闭",
			repo: &moduleGateAccountRepoStub{available: true},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return nil, ErrChannelIdentityUnresolved
			},
			want: ModuleGateError,
		},
		{
			name: "身份歧义故障关闭",
			repo: &moduleGateAccountRepoStub{available: true},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return nil, ErrChannelIdentityAmbiguous
			},
			want: ModuleGateError,
		},
		{
			name: "仓储查询错误故障关闭",
			repo: &moduleGateAccountRepoStub{available: true},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return nil, errors.New("lookup failed")
			},
			want: ModuleGateError,
		},
		{
			name: "解析后账号消失故障关闭",
			repo: &moduleGateAccountRepoStub{available: true},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return resolved, nil
			},
			want: ModuleGateError,
		},
		{
			name: "解析账号ID不一致故障关闭",
			repo: &moduleGateAccountRepoStub{available: true, account: &model.ChannelAccount{ID: 11, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return resolved, nil
			},
			want: ModuleGateError,
		},
		{
			name: "明确启用放行",
			repo: &moduleGateAccountRepoStub{available: true, account: account},
			resolve: func(context.Context, string) (*ResolvedChannelAccount, error) {
				return resolved, nil
			},
			want: ModuleGateEnabled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveModuleGateDecisionWith(ctx, "legacy-account", agentOf, tc.repo, tc.resolve); got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}

func TestAutoReplyEnabledForAccountFailsClosedOnRepositoryFailure(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	for _, repo := range []*moduleGateAccountRepoStub{
		{available: false},
		{available: true, err: errors.New("lookup failed")},
		{available: true},
		{available: true, account: &model.ChannelAccount{ID: 11, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}},
		{available: true, account: &model.ChannelAccount{ID: 10, TenantID: 2, Status: 1, AutoReplyAgentID: uintPtr(9)}},
		{available: true, account: &model.ChannelAccount{ID: 10, TenantID: 1, Status: 0, AutoReplyAgentID: uintPtr(9)}},
		{available: true, account: &model.ChannelAccount{ID: 10, TenantID: 1, Status: 1}},
	} {
		if enabled := enabledForAccount(ctx, 10, repo); enabled {
			t.Fatalf("仓储不可用/查询失败/账号缺失时必须关闭，实际 enabled=true，repo=%+v", repo)
		}
	}
	account := &model.ChannelAccount{ID: 10, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}
	if !enabledForAccount(ctx, 10, &moduleGateAccountRepoStub{available: true, account: account}) {
		t.Fatalf("明确启用的账号应通过数字账号入口")
	}
}

func TestModuleGateDecision_RejectsTenantConflictsAndIndependentBindings(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	account := &model.ChannelAccount{ID: 10, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}
	repo := &moduleGateAccountRepoStub{available: true, account: account}
	foreign := func(context.Context, string) (*ResolvedChannelAccount, error) {
		return &ResolvedChannelAccount{TenantID: 2, AccountID: 10}, nil
	}
	if got := resolveModuleGateDecisionWith(ctx, "account", func(*model.ChannelAccount) uint { return 9 }, repo, foreign); got != ModuleGateError {
		t.Fatalf("跨租户解析必须关闭，实际 %v", got)
	}
	resolve := func(context.Context, string) (*ResolvedChannelAccount, error) {
		return &ResolvedChannelAccount{TenantID: 1, AccountID: 10}, nil
	}
	for _, ids := range [][2]uint{{0, 0}, {9, 0}, {0, 8}, {9, 8}} {
		for i, agentID := range ids {
			got := resolveModuleGateDecisionWith(ctx, "account", func(*model.ChannelAccount) uint { return agentID }, repo, resolve)
			if got.Enabled() != (agentID != 0) {
				t.Fatalf("两模块绑定必须独立生效，绑定=%v 模块=%d 决策=%v", ids, i, got)
			}
		}
	}
}

func TestAutoReplyEnabledForAccountRejectsMissingTenantInMultiTenantMode(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })

	account := &model.ChannelAccount{ID: 10, TenantID: 1, Status: 1, AutoReplyAgentID: uintPtr(9)}
	if enabledForAccount(context.Background(), 10, &moduleGateAccountRepoStub{available: true, account: account}) {
		t.Fatalf("多租户模式无租户上下文必须关闭数字账号入口")
	}
}

func uintPtr(value uint) *uint { return &value }

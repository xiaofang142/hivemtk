// account_service_test.go 桥接账号管理面服务：渠道归一、归属门、租户钉死与投递。
package bridge

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"
)

type svcAccountRepo struct {
	onlineCalled  bool
	onlineCtx     context.Context
	onlineChannel string
	online        bool
	onlineErr     error
}

func (f *svcAccountRepo) Upsert(context.Context, BridgeAccountUpsert) error { return nil }
func (f *svcAccountRepo) SetOffline(context.Context, string, string) error  { return nil }
func (f *svcAccountRepo) TouchLastSync(context.Context, string, string) error {
	return nil
}
func (f *svcAccountRepo) ListForUser(context.Context, uint) ([]BridgeAccountView, error) {
	return nil, nil
}
func (f *svcAccountRepo) IsOnline(ctx context.Context, channel, _ string) (bool, error) {
	f.onlineCalled = true
	f.onlineCtx = ctx
	f.onlineChannel = channel
	return f.online, f.onlineErr
}

func swapBridgeGlobals(t *testing.T, repo BridgeAccountRepo, checker OwnershipChecker) {
	t.Helper()
	prevRepo, prevChecker := GlobalBridgeAccountRepo, GlobalOwnershipChecker
	GlobalBridgeAccountRepo, GlobalOwnershipChecker = repo, checker
	t.Cleanup(func() {
		GlobalBridgeAccountRepo, GlobalOwnershipChecker = prevRepo, prevChecker
	})
}

// TestSendManualMessage_NormalizesChannelAndPinsTenant 别名必须归一再查；
// 归属成立后 ctx 要钉死在账号租户上并剥掉 SystemScope——否则超管代发会跨租户读，
// 出站行还会因 Create 跳过填列落到默认租户 1。
func TestSendManualMessage_NormalizesChannelAndPinsTenant(t *testing.T) {
	repo := &svcAccountRepo{online: false}
	var checkerChannel string
	swapBridgeGlobals(t, repo, func(_ context.Context, _ uint, channel, _ string) (bool, uint, error) {
		checkerChannel = channel
		return true, 501, nil
	})

	callerCtx := tenant.WithSystemScope(tenant.WithTenantID(context.Background(), 999))
	err := SendManualMessage(callerCtx, 9, "douyin_web", "acc", "conv", "hi")
	if !errors.Is(err, ErrBridgeAccountOffline) {
		t.Fatalf("期望 ErrBridgeAccountOffline（在线门未过不该继续投递），实际 %v", err)
	}
	if checkerChannel != "douyin" {
		t.Errorf("归属校验收到的渠道=%q want douyin（别名未归一）", checkerChannel)
	}
	if repo.onlineChannel != "douyin" {
		t.Errorf("在线门使用的渠道=%q want douyin", repo.onlineChannel)
	}
	if id, ok := tenant.FromContext(repo.onlineCtx); !ok || id != 501 {
		t.Errorf("在线门 ctx 租户=%v/%d want bound=true/501", ok, id)
	}
	if tenant.IsSystemScope(repo.onlineCtx) {
		t.Error("在线门 ctx 仍带 SystemScope：隔离 callback 会放行跨租户读")
	}
}

// TestSendManualMessage_RejectsUnowned 归属不成立时不得读在线位、更不得投递。
func TestSendManualMessage_RejectsUnowned(t *testing.T) {
	repo := &svcAccountRepo{online: true}
	swapBridgeGlobals(t, repo, func(context.Context, uint, string, string) (bool, uint, error) {
		return false, 0, nil
	})

	if err := SendManualMessage(context.Background(), 9, "douyin", "acc", "conv", "hi"); !errors.Is(err, ErrBridgeAccountNotOwned) {
		t.Fatalf("期望 ErrBridgeAccountNotOwned，实际 %v", err)
	}
	if repo.onlineCalled {
		t.Error("归属失败后仍查了在线位")
	}
}

// TestSendManualMessage_RepoNotReady 仓储未装配时给可映射的哨兵（503），不是裸错误。
func TestSendManualMessage_RepoNotReady(t *testing.T) {
	swapBridgeGlobals(t, nil, nil)
	if err := SendManualMessage(context.Background(), 9, "douyin", "acc", "conv", "hi"); !errors.Is(err, ErrBridgeAccountRepoNotReady) {
		t.Fatalf("期望 ErrBridgeAccountRepoNotReady，实际 %v", err)
	}
	if _, err := ListAccounts(context.Background(), 9); !errors.Is(err, ErrBridgeAccountRepoNotReady) {
		t.Fatalf("ListAccounts 期望 ErrBridgeAccountRepoNotReady，实际 %v", err)
	}
}

// 账号归属只证明租户隔离，不能替代店铺精确授权；未知 shop mapping 须零出站。
func TestSendManualMessage_AccountOwnershipDoesNotAuthorizeShop(t *testing.T) {
	db := setupBridgeTestDB(t, &model.BridgeAccount{}, &model.MessageHub{}, &model.CapabilitySourceBinding{})
	modeOn(t)

	repo := NewBridgeAccountRepository(db)
	baUpsert(t, repo, baCtx(777), BridgeAccountUpsert{
		Channel: "douyin", AccountID: "acc-svc", Status: "online",
	})

	prevRepo, prevChecker := GlobalBridgeAccountRepo, GlobalOwnershipChecker
	prevInbox := service.GlobalInboxIngressService()
	GlobalBridgeAccountRepo = repo
	GlobalOwnershipChecker = func(context.Context, uint, string, string) (bool, uint, error) {
		return true, 777, nil
	}
	service.SetGlobalInboxIngressService(service.NewInboxIngressService())
	t.Cleanup(func() {
		GlobalBridgeAccountRepo, GlobalOwnershipChecker = prevRepo, prevChecker
		service.SetGlobalInboxIngressService(prevInbox)
	})

	// 调用方带 SystemScope 且绑了另一个租户：两者都必须被 OnlyTenant 剥掉。
	callerCtx := tenant.WithSystemScope(tenant.WithTenantID(context.Background(), 999))
	err := SendManualMessage(callerCtx, 9, "douyin_web", "acc-svc", "conv-svc", "人工回复")
	var denied *capregistry.SubmissionDeniedError
	if !errors.As(err, &denied) || denied.ReasonCode != "capability_mapping_missing" {
		t.Fatalf("账号归属不得授予店铺发送权限，应拒绝 capability_mapping_missing，实际 %v", err)
	}

	var rows []model.MessageHub
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).
		Where("platform = ? AND account_id = ?", "douyin", "acc-svc").
		Find(&rows).Error; err != nil {
		t.Fatalf("回读出站行失败: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("缺少店铺授权不得在任何租户生成出站行，实际 %d", len(rows))
	}
}

var _ BridgeAccountRepo = (*svcAccountRepo)(nil)

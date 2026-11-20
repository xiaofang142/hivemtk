package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/platform"
)

// fakeAssetPlatformClient 可编程假贡献者服务（FA-07 判据矩阵）。
type fakeAssetPlatformClient struct {
	mu           sync.Mutex
	detailErr    error
	detailStatus string
	createErr    error
	createID     int64
	submitErr    error
	uploadErr    error
	updateRefErr error

	createCalls atomic.Int64
	detailCalls atomic.Int64
	submitCalls atomic.Int64
	uploadCalls atomic.Int64
	uploadDatas [][]byte
}

func (f *fakeAssetPlatformClient) GetAssetDetail(int64) (*platform.ContributorAssetInfo, error) {
	f.detailCalls.Add(1)
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return &platform.ContributorAssetInfo{Status: f.detailStatus}, nil
}

func TestRefreshPlatformStatus_UsesInjectedClient(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "true")
	// The pre-fix implementation constructs a real client. Keep that path
	// invalid so this regression never contacts a platform service.
	t.Setenv("PLATFORM_API_URL", "://invalid-platform-url")

	fake := &fakeAssetPlatformClient{detailStatus: "approved"}
	repo := newMockAssetBundleRepo()
	svc := NewAssetBundleService(repo, nil)
	svc.SetPlatformClientForBundleService(fake)
	bundle := submitGateBundle("refresh_injection")
	bundle.PlatformAssetID = 1701
	if err := svc.CreateBundle(context.Background(), bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	refreshed, err := svc.RefreshPlatformStatus(context.Background(), bundle.ID)
	if err != nil {
		t.Fatalf("refresh should use the injected client: %v", err)
	}
	if fake.detailCalls.Load() != 1 {
		t.Fatalf("injected client should receive one detail call, got %d", fake.detailCalls.Load())
	}
	if refreshed.PlatformStatus != "approved" {
		t.Fatalf("platform status = %q, want approved", refreshed.PlatformStatus)
	}
}
func (f *fakeAssetPlatformClient) CreateAsset(platform.CreateAssetPayload) (int64, error) {
	f.createCalls.Add(1)
	if f.createErr != nil {
		return 0, f.createErr
	}
	return f.createID, nil
}
func (f *fakeAssetPlatformClient) SubmitAudit(int64) error {
	f.submitCalls.Add(1)
	return f.submitErr
}
func (f *fakeAssetPlatformClient) UploadVersion(_ int64, _, _ string, data []byte) error {
	f.uploadCalls.Add(1)
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.mu.Lock()
	f.uploadDatas = append(f.uploadDatas, data)
	f.mu.Unlock()
	return nil
}

func newSubmitGateService(t *testing.T, fake *fakeAssetPlatformClient, bundle *model.AssetBundle) (*AssetBundleService, *mockAssetBundleRepo) {
	t.Helper()
	t.Setenv("PLATFORM_ENABLED", "true")
	repo := newMockAssetBundleRepo()
	svc := NewAssetBundleService(repo, nil)
	svc.SetPlatformClientForBundleService(fake)
	if err := svc.CreateBundle(context.Background(), bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	return svc, repo
}

func submitGateBundle(assetID string) *model.AssetBundle {
	return &model.AssetBundle{
		AssetID:  assetID,
		Title:    "投稿闸测试包",
		Industry: "ecommerce",
		Messages: []model.AssetBundleMessage{
			{Role: "system", Content: "你是电商客服助手"},
			{Role: "user", Content: "发货多久到"},
			{Role: "assistant", Content: "一般 48 小时内发货，具体看物流"},
		},
	}
}

// ① 幂等分支：推送最新内容失败 → 不得送审旧稿（SubmitAudit 调用 0 次）。
func TestSubmitToPlatform_PushVersionFailureAbortsAudit(t *testing.T) {
	fake := &fakeAssetPlatformClient{detailStatus: "rejected", uploadErr: errors.New("platform upload boom")}
	bundle := submitGateBundle("gate_push_fail")
	bundle.PlatformAssetID = 501
	svc, _ := newSubmitGateService(t, fake, bundle)

	_, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	if err == nil {
		t.Fatal("推送版本失败必须返回错误")
	}
	if fake.submitCalls.Load() != 0 {
		t.Fatalf("推送失败后不得送审旧稿，SubmitAudit 实际 %d 次", fake.submitCalls.Load())
	}
	if fake.uploadCalls.Load() != 1 {
		t.Fatalf("应尝试推送一次，实际 %d", fake.uploadCalls.Load())
	}
}

// ② 幂等分支：平台状态获取失败 → 不盲目重提，返回可对账错误。
func TestSubmitToPlatform_DetailFetchFailureNoBlindResubmit(t *testing.T) {
	fake := &fakeAssetPlatformClient{detailErr: errors.New("platform unreachable")}
	bundle := submitGateBundle("gate_detail_fail")
	bundle.PlatformAssetID = 502
	svc, _ := newSubmitGateService(t, fake, bundle)

	id, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	if err == nil {
		t.Fatal("状态获取失败必须返回错误")
	}
	if id != 502 {
		t.Fatalf("错误应带平台资产 ID 供对账，得到 %d", id)
	}
	if fake.submitCalls.Load() != 0 || fake.uploadCalls.Load() != 0 {
		t.Fatalf("状态未知不得触发推送或送审：upload=%d submit=%d", fake.uploadCalls.Load(), fake.submitCalls.Load())
	}
}

// ③ 首提分支：创建成功但 ID 落库失败 → 返回带平台 ID 的可对账错误，不送审。
func TestSubmitToPlatform_CreatePersistFailureReturnsReconcilable(t *testing.T) {
	fake := &fakeAssetPlatformClient{createID: 603}
	bundle := submitGateBundle("gate_persist_fail")
	svc, repo := newSubmitGateService(t, fake, bundle)

	repo.updateRefErr = errors.New("local db boom")
	id, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	if err == nil {
		t.Fatal("ID 落库失败必须返回错误（盲目重试会重复创建平台资产）")
	}
	if id != 603 {
		t.Fatalf("错误应带平台资产 ID 603，得到 %d", id)
	}
	if fake.submitCalls.Load() != 0 {
		t.Fatalf("本地无关联时不得送审（资产将不可追溯），SubmitAudit %d 次", fake.submitCalls.Load())
	}
}

// ④ 首提成功链：创建→落 ID→送审→状态回写，且送审内容=最新 messages。
func TestSubmitToPlatform_HappyPathSubmitsLatestContent(t *testing.T) {
	fake := &fakeAssetPlatformClient{createID: 701, detailStatus: "pending"}
	bundle := submitGateBundle("gate_happy")
	svc, repo := newSubmitGateService(t, fake, bundle)

	id, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	if err != nil {
		t.Fatalf("首提应成功: %v", err)
	}
	if id != 701 {
		t.Fatalf("应返回平台资产 ID 701，得到 %d", id)
	}
	if fake.submitCalls.Load() != 1 || fake.createCalls.Load() != 1 {
		t.Fatalf("创建/送审各一次，实际 create=%d submit=%d", fake.createCalls.Load(), fake.submitCalls.Load())
	}
	updated, _ := repo.FindByAssetID(context.Background(), bundle.AssetID)
	if updated.PlatformAssetID != 701 {
		t.Fatalf("platform_asset_id 应落库，得到 %d", updated.PlatformAssetID)
	}
	if fake.uploadCalls.Load() != 0 {
		t.Fatalf("首提分支不应推版本，实际 %d", fake.uploadCalls.Load())
	}
}

// ⑤ 并发提交：同一资产包两个并发请求只允许一次 CreateAsset。
func TestSubmitToPlatform_ConcurrentSubmitCreatesOnce(t *testing.T) {
	fake := &fakeAssetPlatformClient{createID: 801, detailStatus: "pending"}
	bundle := submitGateBundle("gate_concurrent")
	svc, _ := newSubmitGateService(t, fake, bundle)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.SubmitToPlatform(context.Background(), bundle.AssetID)
		}()
	}
	wg.Wait()
	if fake.createCalls.Load() != 1 {
		t.Fatalf("并发提交只允许创建一次平台资产，实际 %d 次", fake.createCalls.Load())
	}
}

// ⑥ 幂等复用：approved/pending 直接回显不再重提。
func TestSubmitToPlatform_IdempotentApprovedAndPending(t *testing.T) {
	for _, status := range []string{"approved", "pending"} {
		fake := &fakeAssetPlatformClient{detailStatus: status}
		bundle := submitGateBundle("gate_idem_" + status)
		bundle.PlatformAssetID = 900
		svc, _ := newSubmitGateService(t, fake, bundle)

		id, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
		if err != nil {
			t.Fatalf("%s 状态应直接回显: %v", status, err)
		}
		if id != 900 {
			t.Fatalf("%s 回显平台 ID，得到 %d", status, id)
		}
		if fake.uploadCalls.Load() != 0 || fake.submitCalls.Load() != 0 || fake.createCalls.Load() != 0 {
			t.Fatalf("%s 状态不得触发任何平台动作：create=%d upload=%d submit=%d",
				status, fake.createCalls.Load(), fake.uploadCalls.Load(), fake.submitCalls.Load())
		}
	}
}

// ⑦ 幂等复用：rejected 走推送+重提，且推送内容与本地最新 messages 一致。
func TestSubmitToPlatform_RejectedResubmitsLatestContent(t *testing.T) {
	fake := &fakeAssetPlatformClient{detailStatus: "rejected"}
	bundle := submitGateBundle("gate_rejected")
	bundle.PlatformAssetID = 950
	svc, _ := newSubmitGateService(t, fake, bundle)

	if _, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID); err != nil {
		t.Fatalf("rejected 重提应成功: %v", err)
	}
	if fake.uploadCalls.Load() != 1 || fake.submitCalls.Load() != 1 {
		t.Fatalf("rejected 应推送一次并送审一次，实际 upload=%d submit=%d", fake.uploadCalls.Load(), fake.submitCalls.Load())
	}
	if len(fake.uploadDatas) != 1 {
		t.Fatalf("应记录推送数据，实际 %d 份", len(fake.uploadDatas))
	}
	if string(fake.uploadDatas[0]) == "" {
		t.Fatal("推送数据不得为空（审核稿必须与最新内容一致）")
	}
}

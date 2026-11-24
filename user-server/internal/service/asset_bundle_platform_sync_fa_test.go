package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/platform"
)

// faSyncPlatformClient 可编程平台客户端，携带审核意见用于验证「保留最新平台响应」。
type faSyncPlatformClient struct {
	status    string
	note      string
	detailErr error
	createID  int64
}

func (f *faSyncPlatformClient) GetAssetDetail(int64) (*platform.ContributorAssetInfo, error) {
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return &platform.ContributorAssetInfo{Status: f.status, ReviewNote: f.note}, nil
}
func (f *faSyncPlatformClient) CreateAsset(platform.CreateAssetPayload) (int64, error) {
	return f.createID, nil
}
func (f *faSyncPlatformClient) SubmitAudit(int64) error { return nil }
func (f *faSyncPlatformClient) UploadVersion(int64, string, string, []byte) error {
	return nil
}

// faSyncRepo 在第 failOnCall 次 UpdatePlatformRef 注入写失败，其余透传。
type faSyncRepo struct {
	*mockAssetBundleRepo
	failOnCall int
	calls      int
}

func (r *faSyncRepo) UpdatePlatformRef(ctx context.Context, id, pid int64, status, note string) error {
	r.calls++
	if r.failOnCall > 0 && r.calls == r.failOnCall {
		return errors.New("injected local db failure")
	}
	return r.mockAssetBundleRepo.UpdatePlatformRef(ctx, id, pid, status, note)
}

func faNewSyncService(t *testing.T, fake *faSyncPlatformClient, bundle *model.AssetBundle, repo *faSyncRepo) *AssetBundleService {
	t.Helper()
	t.Setenv("PLATFORM_ENABLED", "true")
	svc := NewAssetBundleService(repo, nil)
	svc.SetPlatformClientForBundleService(fake)
	if err := svc.CreateBundle(context.Background(), bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	return svc
}

func faAssertStale(t *testing.T, err error, wantStatus, wantNote, wantLocal string) *PlatformProjectionStaleError {
	t.Helper()
	var stale *PlatformProjectionStaleError
	if !errors.As(err, &stale) {
		t.Fatalf("want PlatformProjectionStaleError, got %v", err)
	}
	if !errors.Is(err, ErrPlatformProjectionStale) {
		t.Fatalf("errors.Is(ErrPlatformProjectionStale) = false")
	}
	if stale.PlatformStatus != wantStatus || stale.PlatformReviewNote != wantNote || stale.LocalStatus != wantLocal {
		t.Fatalf("stale = {platform:%s note:%s local:%s}, want {%s %s %s}",
			stale.PlatformStatus, stale.PlatformReviewNote, stale.LocalStatus, wantStatus, wantNote, wantLocal)
	}
	return stale
}

// ① approved 幂等分支：本地投影写失败 → typed error，DB 仍旧状态。
func TestFAAssetSubmitApprovedWriteFailureStale(t *testing.T) {
	repo := &faSyncRepo{mockAssetBundleRepo: newMockAssetBundleRepo(), failOnCall: 1}
	bundle := submitGateBundle("fa_sync_approved")
	bundle.PlatformAssetID = 9001
	bundle.PlatformStatus = "pending" // 本地旧投影
	fake := &faSyncPlatformClient{status: "approved", note: "审核通过"}
	svc := faNewSyncService(t, fake, bundle, repo)

	_, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	faAssertStale(t, err, "approved", "审核通过", "pending")

	got, _ := repo.FindByID(context.Background(), bundle.ID)
	if got.PlatformStatus != "pending" {
		t.Fatalf("DB 投影应保持旧值 pending，实际 %s", got.PlatformStatus)
	}
}

// ② pending 幂等分支：写失败同样显式未同步。
func TestFAAssetSubmitPendingWriteFailureStale(t *testing.T) {
	repo := &faSyncRepo{mockAssetBundleRepo: newMockAssetBundleRepo(), failOnCall: 1}
	bundle := submitGateBundle("fa_sync_pending")
	bundle.PlatformAssetID = 9002
	bundle.PlatformStatus = "draft"
	fake := &faSyncPlatformClient{status: "pending", note: "审核中"}
	svc := faNewSyncService(t, fake, bundle, repo)

	_, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	faAssertStale(t, err, "pending", "审核中", "draft")
}

// ③ 送审后回程落库失败（首次提交）：draft ID 落库成功，第 2 次写失败 → typed error。
func TestFAAssetSubmitPostSubmitWriteFailureStale(t *testing.T) {
	repo := &faSyncRepo{mockAssetBundleRepo: newMockAssetBundleRepo(), failOnCall: 2}
	bundle := submitGateBundle("fa_sync_post_submit")
	fake := &faSyncPlatformClient{createID: 9003, status: "pending", note: "已受理"}
	svc := faNewSyncService(t, fake, bundle, repo)

	id, err := svc.SubmitToPlatform(context.Background(), bundle.AssetID)
	if id != 9003 {
		t.Fatalf("应返回平台资产 ID 9003 供对账，实际 %d", id)
	}
	faAssertStale(t, err, "pending", "已受理", "")
}

// ④ 刷新落库失败：返回 stale 本地 bundle + typed error，DB 仍旧状态。
func TestFAAssetSubmitRefreshWriteFailureStale(t *testing.T) {
	repo := &faSyncRepo{mockAssetBundleRepo: newMockAssetBundleRepo(), failOnCall: 1}
	bundle := submitGateBundle("fa_sync_refresh")
	bundle.PlatformAssetID = 9004
	bundle.PlatformStatus = "pending"
	fake := &faSyncPlatformClient{status: "rejected", note: "内容违规"}
	svc := faNewSyncService(t, fake, bundle, repo)

	got, err := svc.RefreshPlatformStatus(context.Background(), bundle.ID)
	faAssertStale(t, err, "rejected", "内容违规", "pending")
	if got == nil || got.PlatformStatus != "pending" {
		t.Fatalf("应返回未刷新的旧本地投影，实际 %+v", got)
	}
	dbRow, _ := repo.FindByID(context.Background(), bundle.ID)
	if dbRow.PlatformStatus != "pending" {
		t.Fatalf("DB 投影应保持旧值，实际 %s", dbRow.PlatformStatus)
	}
}

// ⑤ 重试成功：故障清除后刷新正常落库，平台实态与本地投影一致。
func TestFAAssetSubmitRefreshRetrySucceeds(t *testing.T) {
	repo := &faSyncRepo{mockAssetBundleRepo: newMockAssetBundleRepo(), failOnCall: 1}
	bundle := submitGateBundle("fa_sync_retry")
	bundle.PlatformAssetID = 9005
	bundle.PlatformStatus = "pending"
	fake := &faSyncPlatformClient{status: "approved", note: "通过"}
	svc := faNewSyncService(t, fake, bundle, repo)

	if _, err := svc.RefreshPlatformStatus(context.Background(), bundle.ID); err == nil {
		t.Fatal("首次刷新写失败应返回 typed error")
	}
	// 清除故障注入后重试。
	repo.failOnCall = 0
	got, err := svc.RefreshPlatformStatus(context.Background(), bundle.ID)
	if err != nil {
		t.Fatalf("重试刷新应成功: %v", err)
	}
	if got.PlatformStatus != "approved" || got.PlatformReviewNote != "通过" {
		t.Fatalf("重试后本地投影应同步为平台实态，实际 %+v", got)
	}
}

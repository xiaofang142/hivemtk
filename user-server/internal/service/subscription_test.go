package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/platform"
)

// 订阅镜像判定测试（003 T022）。
//
// 钉的是修订 #2 的三条硬约束：
//   1. 锁判定本地化：period_end/trial 与 now 惰性比较，**与平台可达性无关**；
//   2. fail-open 仅对「镜像无 period 数据」一种情形生效（堵死「断网 1h 白嫖」）；
//   3. fetched_at 只 Warn 不参与锁决策。

// fakeMirrorRepo 可编排的镜像仓储
type fakeMirrorRepo struct {
	row *model.TenantSubscriptionMirror
	err error
}

func (f *fakeMirrorRepo) Get(_ context.Context) (*model.TenantSubscriptionMirror, error) {
	return f.row, f.err
}
func (f *fakeMirrorRepo) Upsert(_ context.Context, row *model.TenantSubscriptionMirror) error {
	f.row = row
	return nil
}

func ts(offset time.Duration) *time.Time {
	t := time.Now().Add(offset)
	return &t
}

func TestSubscriptionSnapshotExpiredLocks(t *testing.T) {
	// period 已过 + trial 也过 → expired → locked
	repo := &fakeMirrorRepo{row: &model.TenantSubscriptionMirror{
		ID:               1,
		Status:           "expired",
		CurrentPeriodEnd: ts(-time.Hour),
		TrialExpiresAt:   ts(-24 * time.Hour),
		PriceCents:       14900,
		FetchedAt:        time.Now(),
	}}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Status != model.SubscriptionStatusExpired || !snap.Locked {
		t.Fatalf("已过期镜像应 expired+locked，got status=%s locked=%v", snap.Status, snap.Locked)
	}
	if !svc.IsLocked(context.Background()) {
		t.Fatal("IsLocked 对已过期镜像应返回 true")
	}
}

func TestSubscriptionSnapshotActiveNotLocked(t *testing.T) {
	// period 在未来 → active（即使 trial 没过完）→ 不锁
	repo := &fakeMirrorRepo{row: &model.TenantSubscriptionMirror{
		ID:               1,
		CurrentPeriodEnd: ts(72 * time.Hour),
		TrialExpiresAt:   ts(24 * time.Hour),
		FetchedAt:        time.Now(),
	}}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	snap, _ := svc.Snapshot(context.Background())
	if snap.Status != model.SubscriptionStatusActive || snap.Locked {
		t.Fatalf("未到期镜像应 active+不锁，got status=%s locked=%v", snap.Status, snap.Locked)
	}
}

func TestSubscriptionSnapshotTrialNotLocked(t *testing.T) {
	// trial 在未来、无 period → trial → 不锁
	repo := &fakeMirrorRepo{row: &model.TenantSubscriptionMirror{
		ID:             1,
		TrialExpiresAt: ts(72 * time.Hour),
		FetchedAt:      time.Now(),
	}}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	snap, _ := svc.Snapshot(context.Background())
	if snap.Status != model.SubscriptionStatusTrial || snap.Locked {
		t.Fatalf("体验期内应 trial+不锁，got status=%s locked=%v", snap.Status, snap.Locked)
	}
}

// TestSubscriptionFailOpenOnlyWithoutData fail-open 仅对「无 period 数据」生效。
//
// 修订 #2 的核心：平台断网 1h+ 时镜像里的 period_end 已过 → 必须锁 ——
// 有 period 数据的过期行**绝不 fail-open**（那是「断网白嫖」漏洞本体）。
func TestSubscriptionFailOpenOnlyWithoutData(t *testing.T) {
	cases := []struct {
		name     string
		row      *model.TenantSubscriptionMirror
		wantLock bool
	}{
		{"无行（从未同步）", nil, false},
		{"有行无 period 数据", &model.TenantSubscriptionMirror{ID: 1, FetchedAt: time.Now()}, false},
		{"period 已过 + fetched_at 三小时前（断网）", &model.TenantSubscriptionMirror{
			ID:               1,
			CurrentPeriodEnd: ts(-3 * time.Hour),
			FetchedAt:        time.Now().Add(-3 * time.Hour),
		}, true}, // ← 有数据的过期行必须锁，断网不豁免
		{"trial 已过 + 无 period", &model.TenantSubscriptionMirror{
			ID:             1,
			TrialExpiresAt: ts(-time.Minute),
			FetchedAt:      time.Now().Add(-2 * time.Hour),
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: c.row})
			if got := svc.IsLocked(context.Background()); got != c.wantLock {
				t.Fatalf("IsLocked = %v, want %v", got, c.wantLock)
			}
		})
	}
}

// TestSubscriptionFetchedAtDoesNotAffectLock fetched_at 只 Warn 不参与锁决策。
// 两个方向都钉：陈旧不豁免锁、新鲜也不额外锁。
func TestSubscriptionFetchedAtDoesNotAffectLock(t *testing.T) {
	svcExpiredStale := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: &model.TenantSubscriptionMirror{
		ID:               1,
		CurrentPeriodEnd: ts(-time.Minute),
		FetchedAt:        time.Now().Add(-48 * time.Hour), // 极陈旧
	}})
	snap, _ := svcExpiredStale.Snapshot(context.Background())
	if !snap.Locked {
		t.Fatal("period 已过 + fetched_at 极陈旧仍须锁（fetched_at 不参与锁决策）")
	}
	if !snap.Stale {
		t.Fatal("fetched_at 陈旧应置 Stale=true（可观测）")
	}

	svcActiveFresh := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: &model.TenantSubscriptionMirror{
		ID:               1,
		CurrentPeriodEnd: ts(72 * time.Hour),
		FetchedAt:        time.Now(),
	}})
	snap2, _ := svcActiveFresh.Snapshot(context.Background())
	if snap2.Locked || snap2.Stale {
		t.Fatalf("period 在未来 + fetched_at 新鲜：不该锁也不该 Stale，got locked=%v stale=%v", snap2.Locked, snap2.Stale)
	}
}

// TestSubscriptionRepoErrorFailOpen DB 读失败 fail-open（基础设施故障不锁死系统）。
func TestSubscriptionRepoErrorFailOpen(t *testing.T) {
	svc := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{err: errors.New("db down")})
	if svc.IsLocked(context.Background()) {
		t.Fatal("镜像读失败应 fail-open 放行（不锁死系统）")
	}
}

// TestSubscriptionSaveFromSnapshot 平台段写镜像：RFC3339 解析 + 本地惰性回填 status。
func TestSubscriptionSaveFromSnapshot(t *testing.T) {
	repo := &fakeMirrorRepo{}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	sub := &platform.SubscriptionSnapshot{
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339),
		TrialExpiresAt:   time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		PriceCents:       14900,
	}
	if err := svc.SaveFromSnapshot(context.Background(), sub); err != nil {
		t.Fatalf("SaveFromSnapshot: %v", err)
	}
	if repo.row == nil || repo.row.CurrentPeriodEnd == nil || repo.row.TrialExpiresAt == nil {
		t.Fatalf("镜像行未落/时间戳缺失: %+v", repo.row)
	}
	if repo.row.Status != model.SubscriptionStatusActive {
		t.Fatalf("status 应按本地惰性判定回填 active，got %s", repo.row.Status)
	}
	if repo.row.PriceCents != 14900 {
		t.Fatalf("price_cents 未透传: %d", repo.row.PriceCents)
	}
	if repo.row.FetchedAt.IsZero() {
		t.Fatal("fetched_at 未写")
	}
}

// TestSubscriptionSaveNilKeepsState 平台段缺失不写镜像（保留最后已知态）。
func TestSubscriptionSaveNilKeepsState(t *testing.T) {
	existing := &model.TenantSubscriptionMirror{ID: 1, CurrentPeriodEnd: ts(-time.Hour)}
	repo := &fakeMirrorRepo{row: existing}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	if err := svc.SaveFromSnapshot(context.Background(), nil); err != nil {
		t.Fatalf("SaveFromSnapshot(nil): %v", err)
	}
	if repo.row != existing {
		t.Fatal("平台段缺失时不得触碰镜像行（清掉会让锁判定退化 fail-open）")
	}
}

// TestSubscriptionSaveBadTimeKeepsPeriod 解析失败的 period 按无数据处理（不清另一侧）。
func TestSubscriptionSaveBadTimeKeepsPeriod(t *testing.T) {
	repo := &fakeMirrorRepo{}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	sub := &platform.SubscriptionSnapshot{
		Status:           "trial",
		CurrentPeriodEnd: "not-a-time",
		TrialExpiresAt:   time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
	}
	if err := svc.SaveFromSnapshot(context.Background(), sub); err != nil {
		t.Fatalf("SaveFromSnapshot: %v", err)
	}
	if repo.row.CurrentPeriodEnd != nil {
		t.Fatal("坏时间戳应按 nil 处理")
	}
	if repo.row.TrialExpiresAt == nil {
		t.Fatal("另一侧有效时间戳应保留")
	}
	if repo.row.Status != model.SubscriptionStatusTrial {
		t.Fatalf("status 应按 trial 判定，got %s", repo.row.Status)
	}
}

// ---------------------------------------------------------- 状态接口回源刷新
//
// 钉的是 2026-11-04 生产事故 ORD179377249492384KKX0 的根因：
// 平台确认收款并顺延成功，但非默认租户的镜像只有「该租户自己调 sync」才刷新，
// 状态接口只读镜像 → 到期时间永远停在续费前。
// 修法：状态接口按 TTL 回源刷新本租户镜像，失败回落最后已知态。

// stubSubscriptionPlatform 起一个只回答 modules/sync 的平台桩。
// calls 可为 nil（不关心调用次数）。
func stubSubscriptionPlatform(t *testing.T, sub *platform.SubscriptionSnapshot, calls *int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			*calls++
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "ok",
			"data": map[string]any{
				"merchant_key": "MK-REFRESH",
				"active_keys":  []string{},
				"menu_keys":    []string{},
				"catalog":      []any{},
				"subscription": sub,
			},
		})
	}))
	t.Cleanup(srv.Close)
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "{{SECRET_r3fr3sh}}"})
}

// TestSubscriptionRefreshSkipsFreshMirror TTL 内不回源（状态页/横幅高频调用不打平台）。
func TestSubscriptionRefreshSkipsFreshMirror(t *testing.T) {
	calls := 0
	sub := &platform.SubscriptionSnapshot{Status: "active", PriceCents: 900}
	stubSubscriptionPlatform(t, sub, &calls)
	row := &model.TenantSubscriptionMirror{ID: 1, FetchedAt: time.Now()}
	svc := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: row})
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if calls != 0 {
		t.Fatalf("TTL 内不得回源，实际调用 %d 次", calls)
	}
}

// TestSubscriptionRefreshStaleMirrorWritesPeriod 陈旧镜像回源并写回新的到期时间。
//
// 这正是事故现场：镜像 trial 已过（10-27 之后再没刷新），平台已顺延到 12-04。
func TestSubscriptionRefreshStaleMirrorWritesPeriod(t *testing.T) {
	newEnd := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	sub := &platform.SubscriptionSnapshot{
		Status:           "active",
		CurrentPeriodEnd: newEnd.Format(time.RFC3339),
		PriceCents:       900,
	}
	calls := 0
	stubSubscriptionPlatform(t, sub, &calls)
	row := &model.TenantSubscriptionMirror{
		ID:             1,
		Status:         model.SubscriptionStatusExpired,
		TrialExpiresAt: ts(-8 * 24 * time.Hour), // 8 天前到期，镜像停在旧态
		FetchedAt:      time.Now().Add(-8 * 24 * time.Hour),
	}
	repo := &fakeMirrorRepo{row: row}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if calls != 1 {
		t.Fatalf("陈旧镜像应回源一次，实际 %d 次", calls)
	}
	got := repo.row
	if got.CurrentPeriodEnd == nil || !got.CurrentPeriodEnd.Equal(newEnd) {
		t.Fatalf("到期时间未按平台段写回，got %v want %v", got.CurrentPeriodEnd, newEnd)
	}
	if got.Status != model.SubscriptionStatusActive {
		t.Fatalf("写回后 status 应按本地惰性判定为 active，got %s", got.Status)
	}
	if time.Since(got.FetchedAt) > time.Minute {
		t.Fatalf("fetched_at 未刷新: %v", got.FetchedAt)
	}
}

// TestSubscriptionRefreshFailureKeepsMirror 平台不可达时返回错误且不覆盖镜像。
func TestSubscriptionRefreshFailureKeepsMirror(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "{{SECRET_r3fr3sh}}"})

	oldEnd := time.Now().Add(-time.Minute)
	row := &model.TenantSubscriptionMirror{
		ID:               1,
		CurrentPeriodEnd: &oldEnd,
		FetchedAt:        time.Now().Add(-2 * time.Hour),
	}
	repo := &fakeMirrorRepo{row: row}
	svc := NewSubscriptionMirrorServiceWithRepo(repo)
	if err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("平台 500 时 Refresh 应返回错误（由调用方记日志并回落镜像）")
	}
	if repo.row != row {
		t.Fatalf("回源失败不得写镜像（fake repo 的行被替换说明发生了 Upsert）: %+v", repo.row)
	}
	if repo.row.CurrentPeriodEnd == nil || !repo.row.CurrentPeriodEnd.Equal(oldEnd) {
		t.Fatalf("回源失败不得改动到期时间，got %v want %v", repo.row.CurrentPeriodEnd, oldEnd)
	}
}

// TestSubscriptionRefreshDisabledIsNoop 平台关闭时 no-op（离线部署常态，不发请求）。
func TestSubscriptionRefreshDisabledIsNoop(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "false")
	calls := 0
	stubSubscriptionPlatform(t, &platform.SubscriptionSnapshot{Status: "active"}, &calls)
	row := &model.TenantSubscriptionMirror{ID: 1, FetchedAt: time.Now().Add(-24 * time.Hour)}
	svc := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: row})
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("平台关闭时 Refresh 应 no-op: %v", err)
	}
	if calls != 0 {
		t.Fatalf("平台关闭时不得回源，实际调用 %d 次", calls)
	}
}

// TestSubscriptionRefreshMultiTenantWithoutCodeSkips 多租户下解析不出 tenant_code 时
// 不得回源：平台会按 tenant_code=” 返回**商户级**订阅段，写进当前租户镜像即串数据。
func TestSubscriptionRefreshMultiTenantWithoutCodeSkips(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	calls := 0
	stubSubscriptionPlatform(t, &platform.SubscriptionSnapshot{
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
	}, &calls)
	row := &model.TenantSubscriptionMirror{ID: 7, FetchedAt: time.Now().Add(-48 * time.Hour)}
	svc := NewSubscriptionMirrorServiceWithRepo(&fakeMirrorRepo{row: row})
	if err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("多租户无 tenant_code 时应返回错误（跳过回源），不得静默写商户级数据")
	}
	if calls != 0 {
		t.Fatalf("解析不出 tenant_code 时不得回源，实际调用 %d 次", calls)
	}
}

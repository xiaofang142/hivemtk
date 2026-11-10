package service

// job_lock_handoff_test.go — 锁句柄交接的边界（2026-11-10 锁泄漏修法的配套回归）。
//
// 修法把"加锁/解锁必须同会话"落到一个扣住独占连接的句柄上，随之多出两个
// 新的失败面，必须钉住：
//
//  1. **交接前退出**：拿到句柄后、交给 execute 之前若有非预期退出（写运行记录
//     panic 等），句柄必须被归还 —— 否则就是换了个位置再泄漏一次。
//  2. **没有锁就不该释放**：没拿到锁（被别的实例持有）的轮次，绝不能调用释放
//     （那会把别人正在用的锁解掉，是多实例互斥被击穿的最坏形态）。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/robfig/cron/v3"
)

// recordingLockRepo 记录 Acquire/Release 次数，并可预置 Acquire 结果。
type recordingLockRepo struct {
	acquired bool
	acquires int32
	releases int32
}

func (r *recordingLockRepo) Acquire(_ context.Context, _ int64) (*db.AdvisoryLockHandle, bool, error) {
	atomic.AddInt32(&r.acquires, 1)
	if !r.acquired {
		return nil, false, nil
	}
	return &db.AdvisoryLockHandle{OnRelease: func() { atomic.AddInt32(&r.releases, 1) }}, true, nil
}

// TestStartJobForTenant_ReleasesLockWhenNotHandedOff 拿到锁后、交接前退出必须还锁。
//
// 构造：job run 仓储置 nil ⇒ `m.runRepo.Create(...)` 必然 panic（nil 接口方法调用）。
// 这正是「已持锁、尚未把句柄交给 execute」的窗口；defer 兜底必须在 panic 展开时
// 把锁还回去。
func TestStartJobForTenant_ReleasesLockWhenNotHandedOff(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.ResetRegistryForTest(); tenant.SetMode(tenant.ModeOff) })

	lock := &recordingLockRepo{acquired: true}
	m := &JobManager{
		runRepo: nil, // 故意不装配 ⇒ Create 必然 panic
		running: map[jobKey]*atomic.Bool{},
		specs:   map[jobKey]string{},
		entryID: map[jobKey]cron.EntryID{},
	}
	m.lockRepo = lock

	def := geoJobDef{
		Name:    "handoff_early_exit",
		Timeout: time.Second,
		Fn:      func(context.Context) (string, error) { return "ok", nil },
	}
	ctx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		m.startJobForTenant(def, ctx, "manual", tenant.DefaultTenantID)
	}()

	if !panicked {
		t.Fatal("runRepo 未装配时 Create 应当 panic（否则本用例没有覆盖到目标分支）")
	}
	if got := atomic.LoadInt32(&lock.acquires); got != 1 {
		t.Fatalf("应恰好尝试一次加锁，实际 %d", got)
	}
	if got := atomic.LoadInt32(&lock.releases); got != 1 {
		t.Fatalf("拿到锁后未交接就退出时，句柄必须被归还（否则又是一次锁泄漏），实际释放 %d 次", got)
	}
	if m.runningState(jobKey{name: def.Name, tenant: tenant.DefaultTenantID}) {
		t.Fatal("退出后应清掉 running 互斥位，否则该任务此后永久被跳过")
	}
}

// TestStartJobForTenant_ReleasesLockOnNormalPath 正常路径恰好释放一次。
func TestStartJobForTenant_ReleasesLockOnNormalPath(t *testing.T) {
	database := testutil.NewTestDB(t, &model.GeoJobRun{}, &model.GeoConfig{})
	if database == nil {
		t.Fatal("测试库不可达")
	}
	db.SetTestDB(database)

	lock := &recordingLockRepo{acquired: true}
	m := &JobManager{
		runRepo: repository.NewGeoJobRunRepositoryWithDB(database),
		cfgRepo: repository.NewGeoConfigRepositoryWithDB(database),
		running: map[jobKey]*atomic.Bool{},
		specs:   map[jobKey]string{},
		entryID: map[jobKey]cron.EntryID{},
	}
	m.lockRepo = lock

	def := geoJobDef{
		Name:    "handoff_normal",
		Timeout: 5 * time.Second,
		Fn:      func(context.Context) (string, error) { return "ok", nil },
	}
	ctx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)
	if !m.startJobForTenant(def, ctx, "manual", tenant.DefaultTenantID) {
		t.Fatal("正常路径应能启动")
	}
	k := jobKey{name: def.Name, tenant: tenant.DefaultTenantID}
	deadline := time.Now().Add(5 * time.Second)
	for m.runningState(k) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.runningState(k) {
		t.Fatal("任务应已结束")
	}
	if got := atomic.LoadInt32(&lock.releases); got != 1 {
		t.Fatalf("正常路径应恰好释放一次锁，实际 %d 次", got)
	}
}

// TestStartJobForTenant_NoLockMeansNoRelease 没拿到锁的轮次绝不能释放。
func TestStartJobForTenant_NoLockMeansNoRelease(t *testing.T) {
	lock := &recordingLockRepo{acquired: false}
	m := &JobManager{
		running: map[jobKey]*atomic.Bool{},
		specs:   map[jobKey]string{},
		entryID: map[jobKey]cron.EntryID{},
	}
	m.lockRepo = lock

	def := geoJobDef{Name: "nolock_job", Timeout: time.Second,
		Fn: func(context.Context) (string, error) { return "ok", nil }}
	ctx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)

	if m.startJobForTenant(def, ctx, "manual", tenant.DefaultTenantID) {
		t.Fatal("锁被别人持有时应跳过")
	}
	if got := atomic.LoadInt32(&lock.acquires); got != 1 {
		t.Fatalf("应恰好尝试一次加锁，实际 %d", got)
	}
	if got := atomic.LoadInt32(&lock.releases); got != 0 {
		t.Fatalf("没拿到锁就不该释放（会解掉别人的锁），实际 %d 次", got)
	}
	if m.runningState(jobKey{name: def.Name, tenant: tenant.DefaultTenantID}) {
		t.Fatal("跳过轮必须清掉 running 互斥位")
	}
}

package service

import (
	"context"
	"errors"
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

type fakeScheduler struct {
	specs  map[string]string
	nextID cron.EntryID
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{specs: map[string]string{}}
}

func (f *fakeScheduler) AddTask(spec string, cmd func()) (cron.EntryID, error) {
	if _, err := specParser.Parse(spec); err != nil {
		return 0, err
	}
	f.nextID++
	f.specs[spec] = "ok"
	return f.nextID, nil
}

func (f *fakeScheduler) RemoveTask(id cron.EntryID) {}

func newTestJobManager(t *testing.T) (*JobManager, *fakeScheduler) {
	t.Helper()
	// ⚠️ 必须同时迁移 geo_keywords 并注入全局 DB：
	// geoJobDefs 里的任务函数（如 sovRefreshJob）**不是**从 JobManager 取仓库，
	// 而是自己调 repository.NewGeoKeywordRepository()，该构造函数读全局 _db.GetDB()。
	// 只注入 runRepo/cfgRepo 而不设置全局 DB 时，任务函数拿到 nil *gorm.DB，
	// 触发空指针 panic，被 recover 吞掉后记成 status=failed（测试期望 success）。
	// 详见 TASKS_AUDIT_2026-09-16.md · GEO-01。
	database := testutil.NewTestDB(t, &model.GeoJobRun{}, &model.GeoConfig{}, &model.GeoKeyword{})
	db.SetTestDB(database)
	m := &JobManager{
		runRepo: repository.NewGeoJobRunRepositoryWithDB(database),
		cfgRepo: repository.NewGeoConfigRepositoryWithDB(database),
		running: map[jobKey]*atomic.Bool{},
		specs:   map[jobKey]string{},
		entryID: map[jobKey]cron.EntryID{},
	}
	for _, d := range geoJobDefs {
		m.specs[jobKey{name: d.Name, tenant: tenant.DefaultTenantID}] = d.DefaultSpec
	}
	sched := newFakeScheduler()
	return m, sched
}

// TestJobManager_RunRecordsHistory 成功执行的任务应留下 success 运行记录
func TestJobManager_RunRecordsHistory(t *testing.T) {
	m, _ := newTestJobManager(t)

	// off 模式：StartJob 经 Fanout 退化为单默认租户，互斥位键为 (任务, 默认租户)
	defaultKey := jobKey{name: JobSOVRefresh, tenant: tenant.DefaultTenantID}
	m.runningFor(defaultKey).Store(true)
	started, err := m.StartJob(JobSOVRefresh, "manual")
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if started {
		t.Fatal("任务运行中应跳过新触发")
	}
	m.runningFor(defaultKey).Store(false)

	started, err = m.StartJob(JobSOVRefresh, "manual")
	if err != nil || !started {
		t.Fatalf("StartJob started=%v err=%v", started, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for m.runningFor(defaultKey).Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.runningFor(defaultKey).Load() {
		t.Fatal("任务应已执行完成")
	}

	runs, total, err := m.runRepo.List(context.Background(), JobSOVRefresh, 1, 10)
	if err != nil {
		t.Fatalf("List runs failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("期望 1 条运行记录, got %d", total)
	}
	if runs[0].Status != "success" {
		t.Fatalf("期望 status=success, got %s (err=%s)", runs[0].Status, runs[0].Error)
	}
}

// TestJobManager_UnknownJob 未知任务应报错
func TestJobManager_UnknownJob(t *testing.T) {
	m, _ := newTestJobManager(t)
	if _, err := m.StartJob("not_exist", "manual"); err == nil {
		t.Fatal("未知任务应返回错误")
	}
}

// TestJobManager_UpdateSchedule 合法表达式更新成功并持久化，非法表达式被拒绝
func TestJobManager_UpdateSchedule(t *testing.T) {
	m, sched := newTestJobManager(t)

	if err := m.UpdateSchedule(context.Background(), JobSOVRefresh, "0 30 2 * * *"); err == nil {
		t.Fatal("调度器未初始化时应报错")
	}

	m.setup = true
	m.sched = sched
	if err := m.UpdateSchedule(context.Background(), JobSOVRefresh, "0 30 2 * * *"); err != nil {
		t.Fatalf("UpdateSchedule failed: %v", err)
	}
	// off 模式（Background 无租户绑定）：调度落在默认租户的 (任务, 租户) 键上
	if got := m.specs[jobKey{name: JobSOVRefresh, tenant: tenant.DefaultTenantID}]; got != "0 30 2 * * *" {
		t.Fatalf("spec 未生效: %s", got)
	}

	cfg, err := m.cfgRepo.Get(context.Background())
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.CronSpecs == "" || !containsStr(cfg.CronSpecs, "0 30 2 * * *") {
		t.Fatalf("CronSpecs 未持久化: %q", cfg.CronSpecs)
	}

	if err := m.UpdateSchedule(context.Background(), JobSOVRefresh, "invalid spec"); err == nil {
		t.Fatal("非法 cron 表达式应报错")
	}

	if err := m.UpdateSchedule(context.Background(), "not_exist", "0 30 2 * * *"); err == nil {
		t.Fatal("未知任务应报错")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// fakeJobLockRepo 分布式锁接缝的行为桩（AV-04c 批次3：句柄字段改仓储注入后的
// 接缝语义锁）。acquired 决定 Acquire 返回值；err 决定错误路径。
//
// 2026-11-10：接口从 TryLock/Unlock 两方法改成 Acquire 返回**持有独占连接的
// 句柄** —— 那正是修「加解锁落在不同池连接 ⇒ 锁永久泄漏」的核心。桩照新接口
// 形态实现；生产句柄的真实加解锁语义由 internal/pkg/db 的句柄测试覆盖（桩不连库）。
type fakeJobLockRepo struct {
	// acquired 是 Acquire 的返回值：true = 本次拿到锁；false = 锁在别人手里
	//（调用方跳过本轮）。err 决定错误路径（锁状态未知 ⇒ 调用方放行）。
	acquired bool
	err      error

	lockedCalls int
	// released 记录 JobManager 是否把句柄交回释放（真实句柄的 Release 在
	// 桩造的零值句柄上是空操作，只能靠仓储侧计数证明调用链完整）。
	released bool
}

func (f *fakeJobLockRepo) Acquire(_ context.Context, key int64) (*db.AdvisoryLockHandle, bool, error) {
	f.lockedCalls++
	if f.err != nil {
		return nil, false, f.err
	}
	if !f.acquired {
		return nil, false, nil
	}
	return &db.AdvisoryLockHandle{OnRelease: func() { f.released = true }}, true, nil
}

// TestJobManager_LockSeam 锁接缝三态：被别人锁定 ⇒ 跳过本轮且不落运行记录；
// 锁错误 ⇒ 放行本轮（宁多跑一轮，不可静默停摆，改前错误路径不变）；
// 未装配（nil）⇒ 不参与锁，直接起跑。
func TestJobManager_LockSeam(t *testing.T) {
	database := testutil.NewTestDB(t, &model.GeoJobRun{}, &model.GeoConfig{}, &model.GeoKeyword{})
	db.SetTestDB(database)

	mkManager := func() *JobManager {
		return &JobManager{
			runRepo: repository.NewGeoJobRunRepositoryWithDB(database),
			cfgRepo: repository.NewGeoConfigRepositoryWithDB(database),
			running: map[jobKey]*atomic.Bool{},
			specs:   map[jobKey]string{},
			entryID: map[jobKey]cron.EntryID{},
		}
	}
	const tid = uint(42)
	ctx := tenant.WithTenantID(context.Background(), tid)

	// 每个子用例独立任务名：运行记录按任务名统计，不与共享测试库里其他子用例串数。
	mkDef := func(name string) geoJobDef {
		return geoJobDef{
			Name:    name,
			Timeout: 5 * time.Second,
			Fn:      func(_ context.Context) (string, error) { return "ok", nil },
		}
	}
	waitDone := func(t *testing.T, m *JobManager, k jobKey) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for m.runningState(k) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if m.runningState(k) {
			t.Fatal("任务应已执行完成")
		}
	}
	countRuns := func(t *testing.T, m *JobManager, name string) int64 {
		t.Helper()
		_, total, err := m.runRepo.List(ctx, name, 1, 10)
		if err != nil {
			t.Fatalf("List runs 失败: %v", err)
		}
		return total
	}

	// 1. 锁被其他实例持有（TryLock=false）：跳过本轮，不落运行记录，不 unlock。
	t.Run("held_by_other", func(t *testing.T) {
		def := mkDef("lock_seam_job_held")
		m := mkManager()
		lock := &fakeJobLockRepo{acquired: false}
		m.lockRepo = lock
		if m.startJobForTenant(def, ctx, "manual", tid) {
			t.Fatal("锁被他人持有时应跳过本次触发")
		}
		if lock.lockedCalls != 1 || lock.released {
			t.Fatalf("应恰好尝试一次加锁且不释放，实际 locked=%d released=%v", lock.lockedCalls, lock.released)
		}
		if n := countRuns(t, m, def.Name); n != 0 {
			t.Fatalf("跳过轮不该落运行记录，实际 %d 条", n)
		}
	})

	// 2. 错误路径：锁状态未知 ⇒ 记日志后放行本轮（照常起跑并回写运行记录）。
	t.Run("lock_error_proceeds", func(t *testing.T) {
		def := mkDef("lock_seam_job_err")
		m := mkManager()
		lock := &fakeJobLockRepo{err: errors.New("db down")}
		m.lockRepo = lock
		if !m.startJobForTenant(def, ctx, "manual", tid) {
			t.Fatal("锁错误应放行本轮（宁多跑一轮，不可静默停摆）")
		}
		waitDone(t, m, jobKey{name: def.Name, tenant: tid})
		if n := countRuns(t, m, def.Name); n != 1 {
			t.Fatalf("放行轮应落 1 条运行记录，实际 %d 条", n)
		}
		if lock.released {
			t.Fatal("没拿到锁的轮次不该调用释放")
		}
	})

	// 3. 未装配：不参与锁直接起跑（单实例语义，改前 m.db == nil 同形）。
	t.Run("not_wired", func(t *testing.T) {
		def := mkDef("lock_seam_job_nw")
		m := mkManager()
		if !m.startJobForTenant(def, ctx, "manual", tid) {
			t.Fatal("未装配锁仓储应直接起跑")
		}
		waitDone(t, m, jobKey{name: def.Name, tenant: tid})
		if n := countRuns(t, m, def.Name); n != 1 {
			t.Fatalf("应落 1 条运行记录，实际 %d 条", n)
		}
	})
}

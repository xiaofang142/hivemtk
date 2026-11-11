// retention_cleanup_test.go 通用保留清理器服务层判据（CC-04 刀1）。
//
// 覆盖：多表多窗口独立定档、跳过原因（规格非法）、days<=0 回落兜底（非「永不清理」）、
// 空 specs no-op、on 模式逐租户扇出隔离负例（他租户窗外行本轮不删）、单轮上限
// 触发后循环删完、单表错误不阻断他表。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// svcProbeA / svcProbeB 两张探针表，窗口各自独立（A 30 天 / B 90 天）。
type svcProbeA struct {
	TenantID  uint `gorm:"not null;default:1;index"`
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
}

func (svcProbeA) TableName() string { return "retention_svc_probe_as" }

type svcProbeB struct {
	TenantID  uint `gorm:"not null;default:1;index"`
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
}

func (svcProbeB) TableName() string { return "retention_svc_probe_bs" }

// svcProbeGhost 指向不存在的表：注入单表错误，验证不阻断他表。
type svcProbeGhost struct {
	ID uint `gorm:"primarykey"`
}

func (svcProbeGhost) TableName() string { return "retention_svc_probe_ghost_not_exist" }

func setupRetentionSvcTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &svcProbeA{}, &svcProbeB{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	return db
}

func TestRetentionCleaner_RunOnce_MultiSpecWindows(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "probe_a", Model: svcProbeA{}, TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
		{Table: "probe_b", Model: svcProbeB{}, TimeCol: "created_at", Days: func(context.Context) int { return 90 }, DefaultDays: 90},
	})

	now := time.Now()
	seedA := []svcProbeA{
		{CreatedAt: now.Add(-31 * 24 * time.Hour)}, // A 窗外
		{CreatedAt: now.Add(-10 * 24 * time.Hour)}, // A 窗内
	}
	seedB := []svcProbeB{
		{CreatedAt: now.Add(-31 * 24 * time.Hour)}, // B 窗内（A 窗外但 B 窗内）
		{CreatedAt: now.Add(-91 * 24 * time.Hour)}, // B 窗外
	}
	if err := db.Create(&seedA).Error; err != nil {
		t.Fatalf("seed rows A: %v", err)
	}
	if err := db.Create(&seedB).Error; err != nil {
		t.Fatalf("seed rows B: %v", err)
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	byTable := map[string]RetentionTableResult{}
	for _, r := range results {
		byTable[r.Table] = r
	}
	if ra := byTable["probe_a"]; ra.Deleted != 1 || ra.Err != nil || ra.Skipped != "" {
		t.Errorf("probe_a = %+v, want Deleted=1 Err=nil Skipped=\"\"", ra)
	}
	if rb := byTable["probe_b"]; rb.Deleted != 1 || rb.Err != nil || rb.Skipped != "" {
		t.Errorf("probe_b = %+v, want Deleted=1 Err=nil Skipped=\"\"", rb)
	}

	var aKeep, bKeep int64
	if err := db.Model(&svcProbeA{}).Count(&aKeep).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&svcProbeB{}).Count(&bKeep).Error; err != nil {
		t.Fatal(err)
	}
	if aKeep != 1 || bKeep != 1 {
		t.Fatalf("窗内存活 = a:%d b:%d, want 各 1", aKeep, bKeep)
	}
}

func TestRetentionCleaner_InvalidSpecsSkippedNotErrored(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "no_timecol", Model: svcProbeA{}, Days: func(context.Context) int { return 30 }, DefaultDays: 30},
		{Table: "no_model", TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
		{Table: "no_days", Model: svcProbeA{}, TimeCol: "created_at", DefaultDays: 30},
	})

	results := cleaner.RunOnce(context.Background())
	if len(results) != 3 {
		t.Fatalf("结果数 = %d, want 3", len(results))
	}
	for _, r := range results {
		if r.Skipped == "" {
			t.Errorf("表 %s 规格非法应记跳过原因，实际 Skipped=\"\"", r.Table)
		}
		if r.Err != nil {
			t.Errorf("表 %s 跳过不应带 Err: %v", r.Table, r.Err)
		}
		if r.Deleted != 0 {
			t.Errorf("表 %s 跳过不应删行，实际 %d", r.Table, r.Deleted)
		}
	}
}

func TestRetentionCleaner_DaysNonPositiveFallsBackToDefault(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	// Days 读到非法值 0：必须回落 DefaultDays=30 继续清理（对齐 platform
	// heartbeat.retention_days 口径：0 不是「永不清理」，配置错误回落兜底）。
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "probe_a", Model: svcProbeA{}, TimeCol: "created_at", Days: func(context.Context) int { return 0 }, DefaultDays: 30},
	})

	now := time.Now()
	if err := db.Create(&svcProbeA{CreatedAt: now.Add(-31 * 24 * time.Hour)}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("结果异常: %+v", results)
	}
	if results[0].Deleted != 1 {
		t.Fatalf("days=0 应回落兜底 30 天并删除窗外行，实际 Deleted=%d", results[0].Deleted)
	}
}

func TestRetentionCleaner_EmptySpecsNoop(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), nil)
	if got := cleaner.RunOnce(context.Background()); got != nil {
		t.Fatalf("空 specs 应 no-op 返回 nil，实际 %+v", got)
	}
}

func TestRetentionCleaner_OnModeTenantScopeNegative(t *testing.T) {
	db := setupRetentionSvcTestDB(t)

	// on 模式 + 活跃租户仅 B：清理器逐租户扇出，本轮只够得到 B 的行。
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) {
		return []uint{2002}, nil
	})
	t.Cleanup(func() { tenant.SetActiveTenantLoader(nil) })

	// 插桩：on 模式 Create 经 callback 自动填 tenant_id，ctx 绑定即归属。
	now := time.Now()
	if err := db.WithContext(tenant.WithTenantID(context.Background(), 2001)).
		Create(&svcProbeA{CreatedAt: now.Add(-40 * 24 * time.Hour)}).Error; err != nil { // A 租户窗外行
		t.Fatalf("seed tenant 2001 row: %v", err)
	}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), 2002)).
		Create(&svcProbeA{CreatedAt: now.Add(-40 * 24 * time.Hour)}).Error; err != nil { // B 租户窗外行
		t.Fatalf("seed tenant 2002 row: %v", err)
	}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), 2002)).
		Create(&svcProbeA{CreatedAt: now.Add(-1 * 24 * time.Hour)}).Error; err != nil { // B 租户窗内行
		t.Fatalf("seed tenant 2002 in-window row: %v", err)
	}

	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "probe_a", Model: svcProbeA{}, TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
	})
	results := cleaner.RunOnce(context.Background())
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("结果异常: %+v", results)
	}

	sysCtx := tenant.WithSystemScope(context.Background())
	var aRows, bOut, bIn int64
	if err := db.WithContext(sysCtx).Model(&svcProbeA{}).Where("tenant_id = ?", 2001).Count(&aRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(sysCtx).Model(&svcProbeA{}).Where("tenant_id = ? AND created_at < ?", 2002, now.Add(-30*24*time.Hour)).Count(&bOut).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(sysCtx).Model(&svcProbeA{}).Where("tenant_id = ? AND created_at >= ?", 2002, now.Add(-30*24*time.Hour)).Count(&bIn).Error; err != nil {
		t.Fatal(err)
	}
	if aRows != 1 {
		t.Errorf("他租户（2001）窗外行本轮被误删 = %d, want 保留 1（逐租户扇出结构隔离）", aRows)
	}
	if bOut != 0 {
		t.Errorf("本租户（2002）窗外行未删 = %d, want 0", bOut)
	}
	if bIn != 1 {
		t.Errorf("本租户（2002）窗内行被误删 = %d, want 保留 1", bIn)
	}
	if results[0].Deleted != 1 {
		t.Errorf("本轮删除数 = %d, want 1（只删本租户窗外行）", results[0].Deleted)
	}
}

func TestRetentionCleaner_BatchLimitDrainsInOneRun(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "probe_a", Model: svcProbeA{}, TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
	})
	cleaner.batchLimit = 4 // 注入小批上限，制造 N+M 触发格

	now := time.Now()
	const total = 11 // 4+4+3：三轮删完，末轮不足上限收敛
	rows := make([]svcProbeA, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, svcProbeA{CreatedAt: now.Add(-45 * 24 * time.Hour)})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("结果异常: %+v", results)
	}
	if results[0].Deleted != total {
		t.Fatalf("单轮清理应循环批删至收敛 = %d, want %d", results[0].Deleted, total)
	}
	var left int64
	if err := db.Model(&svcProbeA{}).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("残留行数 = %d, want 0", left)
	}

	// 幂等：重跑 0 行。
	results2 := cleaner.RunOnce(context.Background())
	if results2[0].Deleted != 0 {
		t.Fatalf("幂等重跑应删 0 行，实际 %d", results2[0].Deleted)
	}
}

func TestRetentionCleaner_OneTableErrorDoesNotBlockOthers(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), []RetentionSpec{
		{Table: "ghost", Model: svcProbeGhost{}, TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
		{Table: "probe_a", Model: svcProbeA{}, TimeCol: "created_at", Days: func(context.Context) int { return 30 }, DefaultDays: 30},
	})

	now := time.Now()
	if err := db.Create(&svcProbeA{CreatedAt: now.Add(-40 * 24 * time.Hour)}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	results := cleaner.RunOnce(context.Background())
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	byTable := map[string]RetentionTableResult{}
	for _, r := range results {
		byTable[r.Table] = r
	}
	if byTable["ghost"].Err == nil {
		t.Error("不存在的表应记 Err（单表错误入结果），实际 Err=nil")
	}
	if byTable["ghost"].Deleted != 0 {
		t.Errorf("错误表删除数 = %d, want 0", byTable["ghost"].Deleted)
	}
	if byTable["probe_a"].Err != nil || byTable["probe_a"].Deleted != 1 {
		t.Errorf("他表不应被错误阻断: probe_a = %+v, want Deleted=1 Err=nil", byTable["probe_a"])
	}
}

func TestRetentionCleanupTask_RunOncePassesThrough(t *testing.T) {
	db := setupRetentionSvcTestDB(t)
	task := NewRetentionCleanupTask(NewRetentionCleaner(repository.NewRetentionCleanupRepoWithDB(db), nil))
	if got := task.RunOnce(context.Background()); got != nil {
		t.Fatalf("空 specs 应 no-op，实际 %+v", got)
	}
}

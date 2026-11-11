// retention_cleanup_test.go 通用保留清理仓储原语判据（CC-04 刀1）。
//
// 覆盖任务行判据：TTL 边界（恰在窗上沿不动/窗外删除）、单轮批量上限触发格、
// 幂等重跑 0 行、off 模式混合租户时间谓词保护、软删表物理删除、batchLimit<=0
// 配置错误不无界删除。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// retentionProbe 无软删列的租户表探针（created_at 度量）。
type retentionProbe struct {
	TenantID  uint `gorm:"not null;default:1;index"`
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
}

func (retentionProbe) TableName() string { return "retention_probes" }

// retentionSoftProbe 带 DeletedAt 的探针：验证 Unscoped 物理删除
// （layer_decision_logs 同形态 —— 软删残留同样占空间，保留清理必须回收）。
type retentionSoftProbe struct {
	TenantID  uint `gorm:"not null;default:1;index"`
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (retentionSoftProbe) TableName() string { return "retention_soft_probes" }

func setupRetentionRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &retentionProbe{}, &retentionSoftProbe{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	return db
}

func TestDeleteBeforeBatch_TTLBoundary(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	// 上沿取整到微秒，消除驱动精度舍入不对称：恰在 cutoff 的行必须存活。
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	rows := []retentionProbe{
		{CreatedAt: cutoff},                        // 恰在窗上沿：窗外删除的否定面
		{CreatedAt: cutoff.Add(time.Microsecond)},  // 窗内
		{CreatedAt: cutoff.Add(-time.Microsecond)}, // 窗外 1µs
		{CreatedAt: cutoff.Add(-24 * time.Hour)},   // 窗外深处
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	n, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", cutoff, 100)
	if err != nil {
		t.Fatalf("DeleteBeforeBatch: %v", err)
	}
	if n != 2 {
		t.Fatalf("删除行数 = %d, want 2（窗外两行；恰在 cutoff 的行必须保留）", n)
	}
	var kept int64
	if err := db.Model(&retentionProbe{}).Count(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if kept != 2 {
		t.Fatalf("存活行数 = %d, want 2（cutoff 当行 + 窗内行）", kept)
	}
	var atCutoff int64
	if err := db.Model(&retentionProbe{}).Where("created_at = ?", cutoff).Count(&atCutoff).Error; err != nil {
		t.Fatal(err)
	}
	if atCutoff != 1 {
		t.Fatalf("恰在窗上沿的行被误删 = %d, want 保留 1", atCutoff)
	}
}

func TestDeleteBeforeBatch_BatchCapPerStatement(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	now := time.Now()
	total := 8
	rows := make([]retentionProbe, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, retentionProbe{CreatedAt: now.Add(-40 * 24 * time.Hour)})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	const batchLimit = 3
	n, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", now.Add(-30*24*time.Hour), batchLimit)
	if err != nil {
		t.Fatalf("DeleteBeforeBatch: %v", err)
	}
	if n != batchLimit {
		t.Fatalf("单语句删除行数 = %d, want %d（单轮上限必须生效，防长事务锁）", n, batchLimit)
	}
	var left int64
	if err := db.Model(&retentionProbe{}).Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != int64(total-batchLimit) {
		t.Fatalf("剩余行数 = %d, want %d", left, total-batchLimit)
	}
}

func TestDeleteBeforeBatch_IdempotentRerun(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	now := time.Now()
	cutoff := now.Add(-30 * 24 * time.Hour)
	old := []retentionProbe{{CreatedAt: now.Add(-31 * 24 * time.Hour)}, {CreatedAt: now.Add(-32 * 24 * time.Hour)}}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	if _, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", cutoff, 100); err != nil {
		t.Fatalf("首轮: %v", err)
	}
	n2, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", cutoff, 100)
	if err != nil {
		t.Fatalf("重跑: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("幂等重跑应删 0 行，实际 %d", n2)
	}
}

func TestDeleteBeforeBatch_OffModeTimePredicateProtectsAnyTenant(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	// off 模式无租户注入，清理口径=全局按时间列。判据负例形态：
	// A 租户窗内行 + B 租户窗外行同跑 —— A 不动、B 删。
	now := time.Now()
	rows := []retentionProbe{
		{TenantID: 7, CreatedAt: now.Add(-10 * 24 * time.Hour)}, // A 窗内
		{TenantID: 8, CreatedAt: now.Add(-40 * 24 * time.Hour)}, // B 窗外
		{TenantID: 8, CreatedAt: now.Add(-10 * 24 * time.Hour)}, // B 窗内
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	if _, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", now.Add(-30*24*time.Hour), 100); err != nil {
		t.Fatalf("DeleteBeforeBatch: %v", err)
	}

	var aIn, bIn int64
	if err := db.Model(&retentionProbe{}).Where("tenant_id = ? AND created_at >= ?", 7, now.Add(-30*24*time.Hour)).Count(&aIn).Error; err != nil {
		t.Fatal(err)
	}
	if aIn != 1 {
		t.Fatalf("A 租户窗内行被误删 = %d, want 保留 1", aIn)
	}
	if err := db.Model(&retentionProbe{}).Where("tenant_id = ? AND created_at >= ?", 8, now.Add(-30*24*time.Hour)).Count(&bIn).Error; err != nil {
		t.Fatal(err)
	}
	if bIn != 1 {
		t.Fatalf("B 租户窗内行被误删 = %d, want 保留 1", bIn)
	}
	var bOut int64
	if err := db.Unscoped().Model(&retentionProbe{}).Where("tenant_id = ? AND created_at < ?", 8, now.Add(-30*24*time.Hour)).Count(&bOut).Error; err != nil {
		t.Fatal(err)
	}
	if bOut != 0 {
		t.Fatalf("B 租户窗外行未被删除 = %d, want 0", bOut)
	}
}

func TestDeleteBeforeBatch_UnscopedPhysicalDelete(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	now := time.Now()
	if err := db.Create(&retentionSoftProbe{CreatedAt: now.Add(-40 * 24 * time.Hour)}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if _, err := repo.DeleteBeforeBatch(context.Background(), retentionSoftProbe{}, "created_at", now.Add(-30*24*time.Hour), 100); err != nil {
		t.Fatalf("DeleteBeforeBatch: %v", err)
	}

	var physical int64
	if err := db.Unscoped().Model(&retentionSoftProbe{}).Count(&physical).Error; err != nil {
		t.Fatal(err)
	}
	if physical != 0 {
		t.Fatalf("软删列表应被物理删除（保留清理回收空间），残留 %d 行", physical)
	}
}

func TestDeleteBeforeBatch_ZeroBatchLimitDeletesNothing(t *testing.T) {
	db := setupRetentionRepoTestDB(t)
	repo := NewRetentionCleanupRepoWithDB(db)

	now := time.Now()
	if err := db.Create(&retentionProbe{CreatedAt: now.Add(-40 * 24 * time.Hour)}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	n, err := repo.DeleteBeforeBatch(context.Background(), retentionProbe{}, "created_at", now.Add(-30*24*time.Hour), 0)
	if err != nil {
		t.Fatalf("batchLimit=0: %v", err)
	}
	if n != 0 {
		t.Fatalf("batchLimit=0 应不删任何行（配置错误不无界删除），实际 %d", n)
	}
	var kept int64
	if err := db.Model(&retentionProbe{}).Count(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("行被误删 = %d, want 保留 1", kept)
	}
}

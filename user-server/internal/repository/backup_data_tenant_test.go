// backup_data_tenant_test.go 备份数据的租户契约。
//
// 备份/恢复天然是系统级操作：导出要看到全部租户，恢复要写回全部租户。
// 于是它最容易犯两类相反的错误：
//  1. 导出漏 tenant_id ⇒ 恢复时所有行落回默认租户 1（跨租户串数据）；
//  2. 恢复时整表 DELETE ⇒ 备份没覆盖的租户数据被一起清空。
//
// 本文件把这两条钉成会失败的测试。
package repository

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	bdTenantA = uint(211)
	bdTenantB = uint(212)
)

func bdSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t,
		&model.Clue{},
		&model.User{},
		&model.ShortLink{},
		&model.SystemConfig{},
	)
	if db == nil {
		t.Fatal("测试库不可达：备份数据租户契约判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

func bdSeed(t *testing.T, db *gorm.DB, row any) {
	t.Helper()
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("seed %T: %v", row, err)
	}
}

// bdSeedMap 用 map 播种：Clue.BeforeCreate 会无条件重写字符串主键，
// 结构体路径拿不到调用方指定的 ID；备份恢复本身也是 map 写入，形状一致。
func bdSeedMap(t *testing.T, db *gorm.DB, table string, row map[string]any) {
	t.Helper()
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Table(table).Create(row).Error; err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func bdDecodeRows(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("解析导出 JSON: %v", err)
	}
	return rows
}

func bdTenantIDOf(t *testing.T, row map[string]any) uint {
	t.Helper()
	v, ok := row["tenant_id"]
	if !ok {
		t.Fatalf("导出缺少 tenant_id 列: %v", row)
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("tenant_id 类型异常: %T", v)
	}
	return uint(n)
}

// TestBackupDataRepo_DumpsCoverAllTenantsAndCarryTenantID 导出必须是系统视角，
// 且每一行都带 tenant_id —— 恢复侧靠它把行写回原租户。
func TestBackupDataRepo_DumpsCoverAllTenantsAndCarryTenantID(t *testing.T) {
	db := bdSetupDB(t)
	repo := NewBackupDataRepositoryWithDB(db)
	sys := tenant.WithSystemScope(context.Background())

	bdSeed(t, db, &model.Clue{TenantID: bdTenantA, ID: "bd-clue-a", Name: "线索A"})
	bdSeed(t, db, &model.Clue{TenantID: bdTenantB, ID: "bd-clue-b", Name: "线索B"})
	bdSeed(t, db, &model.User{TenantID: bdTenantA, ID: "bd-user-a", Username: "bd_user_a", Password: "x"})
	bdSeed(t, db, &model.User{TenantID: bdTenantB, ID: "bd-user-b", Username: "bd_user_b", Password: "x"})
	bdSeed(t, db, &model.ShortLink{
		TenantID: bdTenantA, ShortCode: "bd211", OriginalURL: "https://example.com/a", Status: 1,
	})
	bdSeed(t, db, &model.ShortLink{
		TenantID: bdTenantB, ShortCode: "bd212", OriginalURL: "https://example.com/b", Status: 1,
	})

	clueRows, err := repo.DumpClues(sys)
	if err != nil {
		t.Fatalf("DumpClues: %v", err)
	}
	clues := bdDecodeRows(t, clueRows)
	if len(clues) != 2 {
		t.Fatalf("DumpClues 导出行数 = %d, want 2（必须跨租户全量）", len(clues))
	}
	gotClueTenants := map[uint]bool{}
	for _, row := range clues {
		gotClueTenants[bdTenantIDOf(t, row)] = true
	}
	if !gotClueTenants[bdTenantA] || !gotClueTenants[bdTenantB] {
		t.Fatalf("DumpClues 租户覆盖不全: %v", gotClueTenants)
	}

	userRows, err := repo.DumpUsers(sys)
	if err != nil {
		t.Fatalf("DumpUsers: %v", err)
	}
	users := bdDecodeRows(t, userRows)
	if len(users) != 2 {
		t.Fatalf("DumpUsers 导出行数 = %d, want 2", len(users))
	}
	for _, row := range users {
		if bdTenantIDOf(t, row) == 0 {
			t.Fatalf("DumpUsers 行 tenant_id 为 0: %v", row)
		}
	}

	linkRows, err := repo.DumpShortLinks(sys)
	if err != nil {
		t.Fatalf("DumpShortLinks: %v", err)
	}
	links := bdDecodeRows(t, linkRows)
	if len(links) != 2 {
		t.Fatalf("DumpShortLinks 导出行数 = %d, want 2", len(links))
	}
	for _, row := range links {
		if bdTenantIDOf(t, row) == 0 {
			t.Fatalf("DumpShortLinks 行 tenant_id 为 0: %v", row)
		}
		// 列名必须与真实表一致：short_links 存的是 original_url，不是 url。
		if row["original_url"] == nil || row["original_url"] == "" {
			t.Fatalf("DumpShortLinks 缺少 original_url（列名写错会让恢复写入空 URL）: %v", row)
		}
	}
}

// TestBackupDataRepo_ExistsChecksAreTenantQualified 恢复前的存在性判断必须带租户。
//
// 判断若漏租户，恢复租户 B 的备份时会把租户 A 的同名用户/同码短链判为
// 「已存在」而整行跳过 —— B 的数据静默缺失。
func TestBackupDataRepo_ExistsChecksAreTenantQualified(t *testing.T) {
	db := bdSetupDB(t)
	repo := NewBackupDataRepositoryWithDB(db)
	sys := tenant.WithSystemScope(context.Background())

	bdSeedMap(t, db, "clues", map[string]any{
		"tenant_id": bdTenantA, "id": "bd-exists-clue", "name": "线索",
	})
	bdSeed(t, db, &model.User{TenantID: bdTenantA, ID: "bd-exists-user", Username: "bd_exists_user", Password: "x"})
	bdSeed(t, db, &model.ShortLink{
		TenantID: bdTenantA, ShortCode: "bdexists", OriginalURL: "https://example.com/x", Status: 1,
	})

	clueA, err := repo.ClueExists(sys, bdTenantA, "bd-exists-clue")
	if err != nil || !clueA {
		t.Fatalf("ClueExists(A) = %v, %v；want true, nil", clueA, err)
	}
	clueB, err := repo.ClueExists(sys, bdTenantB, "bd-exists-clue")
	if err != nil || clueB {
		t.Fatalf("ClueExists(B) = %v, %v；want false, nil（看到了别的租户的行）", clueB, err)
	}

	userA, err := repo.UserExistsByUsername(sys, bdTenantA, "bd_exists_user")
	if err != nil || !userA {
		t.Fatalf("UserExistsByUsername(A) = %v, %v；want true, nil", userA, err)
	}
	userB, err := repo.UserExistsByUsername(sys, bdTenantB, "bd_exists_user")
	if err != nil || userB {
		t.Fatalf("UserExistsByUsername(B) = %v, %v；want false, nil", userB, err)
	}

	linkA, err := repo.ShortLinkExistsByCode(sys, bdTenantA, "bdexists")
	if err != nil || !linkA {
		t.Fatalf("ShortLinkExistsByCode(A) = %v, %v；want true, nil", linkA, err)
	}
	linkB, err := repo.ShortLinkExistsByCode(sys, bdTenantB, "bdexists")
	if err != nil || linkB {
		t.Fatalf("ShortLinkExistsByCode(B) = %v, %v；want false, nil", linkB, err)
	}
}

// TestBackupDataRepo_RestoreTableOnlyDeletesCoveredTenants 恢复单表的 DELETE
// 必须限定在备份行覆盖的租户内。整表清空会把备份未包含的租户一起抹掉。
func TestBackupDataRepo_RestoreTableOnlyDeletesCoveredTenants(t *testing.T) {
	db := bdSetupDB(t)
	repo := NewBackupDataRepositoryWithDB(db)
	sys := tenant.WithSystemScope(context.Background())

	bdSeed(t, db, &model.SystemConfig{TenantID: bdTenantA, Name: "A-原配置"})
	bdSeed(t, db, &model.SystemConfig{TenantID: bdTenantB, Name: "B-原配置"})

	rows := []map[string]any{
		{"tenant_id": bdTenantA, "name": "A-恢复配置"},
	}
	if err := repo.RestoreTable(sys, "system_config", rows); err != nil {
		t.Fatalf("RestoreTable: %v", err)
	}

	var gotA, gotB model.SystemConfig
	if err := db.WithContext(sys).Where("tenant_id = ?", bdTenantA).First(&gotA).Error; err != nil {
		t.Fatalf("回查租户 A: %v", err)
	}
	if gotA.Name != "A-恢复配置" {
		t.Errorf("租户 A 配置 = %q, want %q", gotA.Name, "A-恢复配置")
	}
	if err := db.WithContext(sys).Where("tenant_id = ?", bdTenantB).First(&gotB).Error; err != nil {
		t.Fatalf("租户 B 的配置被删了（DELETE 没带租户范围）: %v", err)
	}
	if gotB.Name != "B-原配置" {
		t.Errorf("租户 B 配置 = %q, want %q", gotB.Name, "B-原配置")
	}
}

// TestBackupDataRepo_RestoreTableRejectsOutsideWhitelist 恢复白名单不可绕过。
func TestBackupDataRepo_RestoreTableRejectsOutsideWhitelist(t *testing.T) {
	db := bdSetupDB(t)
	repo := NewBackupDataRepositoryWithDB(db)
	sys := tenant.WithSystemScope(context.Background())

	err := repo.RestoreTable(sys, "user", []map[string]any{{"tenant_id": bdTenantA}})
	if err == nil {
		t.Fatal("RestoreTable 接受了白名单外的表 user ⇒ 恢复端点可被用来覆写任意表")
	}
}

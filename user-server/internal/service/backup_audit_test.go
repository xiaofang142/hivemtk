package service

// 本文件是 AUD-04/05/09（备份导出/恢复对称、保留策略、落盘权限）的回归钉：
//   - 导出端必须覆盖恢复端认领的全部键（历史上导出 3 表 / 恢复 26 表漂移，
//     残缺备份被静默恢复成空库并报 completed）；
//   - 恢复端对缺键老格式 fail-closed，拒绝静默 no-op；
//   - 保留策略按类型留最新 30 份，清扫整目录删除（含 data.json）；
//   - 备份目录 0700 / zip 0600，含 PII 不对同组/其他用户开放。
//
// 导出/覆盖/权限三格用假仓储（auditDumpRepo）注入：不得为测导出而真实建
// 23 张最小形态表——槽位库跨用例共享，假表会污染后续用例的 AutoMigrate
// 与插入（bridge_accounts 等 id 列形态冲突，23502）。契约由假仓储的
// 「未播种即报错」保证：exportData 必须对 backupExtraTables 每张表都调用
// DumpTable，缺一即被 fake 拒答暴露。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"
)

// auditDumpRepo 是 BackupDataRepository 的导出侧假实现：只覆 exportData
// 实际调用的 4 个方法，其余方法嵌入接口零值（不会被调到，调到即 panic
// 暴露测试与实现耦合错误）。
type auditDumpRepo struct {
	repository.BackupDataRepository
	tables map[string]json.RawMessage
}

func (f *auditDumpRepo) DumpClues(ctx context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[{"id":"c1"}]`), nil
}

func (f *auditDumpRepo) DumpUsers(ctx context.Context, limit, offset int) (json.RawMessage, error) {
	return json.RawMessage(`[{"id":"u1"}]`), nil
}

func (f *auditDumpRepo) DumpShortLinks(ctx context.Context, limit, offset int) (json.RawMessage, error) {
	return json.RawMessage(`[{"id":"s1"}]`), nil
}

func (f *auditDumpRepo) DumpTable(ctx context.Context, table string) (json.RawMessage, error) {
	rows, ok := f.tables[table]
	if !ok {
		return nil, fmt.Errorf("fake dump: table %s not seeded（exportData 漏导此表）", table)
	}
	return rows, nil
}

func newAuditDumpRepo() *auditDumpRepo {
	fake := &auditDumpRepo{tables: map[string]json.RawMessage{}}
	for _, t := range backupExtraTables {
		fake.tables[t.tableName] = json.RawMessage(`[{"id":1}]`)
	}
	return fake
}

// TestBackupService_ExportData_CoversAllRestoreTables 钉住导出/恢复同源清单。
func TestBackupService_ExportData_CoversAllRestoreTables(t *testing.T) {
	svc := &BackupService{backupDataRepo: newAuditDumpRepo()}

	data, err := svc.exportData(context.Background())
	if err != nil {
		t.Fatalf("exportData failed: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal export failed: %v", err)
	}
	for _, key := range []string{"clues", "users", "short_link"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("导出缺基表键 %s", key)
		}
	}
	for _, tt := range backupExtraTables {
		if _, ok := parsed[tt.key]; !ok {
			t.Errorf("导出缺扩表键 %s（恢复端认此键，缺=恢复静默 no-op）", tt.key)
		}
	}
	var stats map[string]int
	if err := json.Unmarshal(parsed["stats"], &stats); err != nil {
		t.Fatalf("unmarshal stats failed: %v", err)
	}
	if want := 3 + len(backupExtraTables); len(stats) != want {
		t.Errorf("stats 键数=%d，期望 %d", len(stats), want)
	}
	for _, tt := range backupExtraTables {
		if stats[tt.key] != 1 {
			t.Errorf("stats[%s]=%d，期望 1（与导出行数同口径）", tt.key, stats[tt.key])
		}
	}
}

// TestRestoreService_RestoreDatabase_OldFormatBackupFailsClosed 钉住残缺备份拒绝。
func TestRestoreService_RestoreDatabase_OldFormatBackupFailsClosed(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	svc := &RestoreService{
		restoreRepo:    newTestRestoreRecordRepository(database),
		backupRepo:     newTestBackupRepository(database),
		backupDataRepo: repository.NewBackupDataRepositoryWithDB(database),
	}
	_, restoreTmp := storage.BackupSource()
	if err := os.MkdirAll(restoreTmp, 0700); err != nil {
		t.Fatalf("mkdir restoreTmp: %v", err)
	}
	// 旧格式只有三键——历史缺陷即「缺键静默 completed」，现在必须 fail-closed。
	old := `{"version":"1.0.0","clues":[],"users":[],"short_link":[]}`
	if err := os.WriteFile(filepath.Join(restoreTmp, "data.json"), []byte(old), 0600); err != nil {
		t.Fatalf("write data.json: %v", err)
	}
	defer os.RemoveAll(restoreTmp)

	err := svc.restoreDatabase(ctx, &model.Backup{BackupName: "old-format"})
	if err == nil {
		t.Fatal("旧格式残缺备份应被 fail-closed 拒绝，实际却恢复成功")
	}
	if !strings.Contains(err.Error(), "缺少键") {
		t.Errorf("错误应列出缺键，实际: %v", err)
	}
}

// TestBackupService_PruneBackups_RetainsLatest30PerType 钉住保留策略。
func TestBackupService_PruneBackups_RetainsLatest30PerType(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	svc := &BackupService{backupRepo: repo}

	oldDir := t.TempDir()
	oldZip := filepath.Join(oldDir, "old.zip")
	if err := os.WriteFile(oldZip, []byte("zip"), 0600); err != nil {
		t.Fatalf("write old zip: %v", err)
	}

	base := time.Now().Add(-100 * time.Hour)
	for i := 0; i < 32; i++ {
		b := &model.Backup{
			BackupName: fmt.Sprintf("prune_full_%02d", i),
			BackupType: model.BackupTypeFull,
			Status:     model.BackupStatusCompleted,
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
		}
		if i == 0 {
			b.FilePath = oldZip // 最旧的一份带真实目录，清扫应整目录删除
		}
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("create backup %d: %v", i, err)
		}
	}
	other := &model.Backup{
		BackupName: "prune_inc_00", BackupType: model.BackupTypeIncremental,
		Status: model.BackupStatusCompleted, CreatedAt: base,
	}
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("create incremental backup: %v", err)
	}

	var list []*model.Backup
	database.Where("backup_type = ?", model.BackupTypeFull).Order("created_at ASC").Find(&list)
	keep := list[len(list)-1]

	svc.pruneBackups(ctx, keep)

	var fullCount, otherCount int64
	database.Model(&model.Backup{}).Where("backup_type = ?", model.BackupTypeFull).Count(&fullCount)
	database.Model(&model.Backup{}).Where("backup_type = ?", model.BackupTypeIncremental).Count(&otherCount)
	if fullCount != backupRetentionCount {
		t.Errorf("full 保留 %d 份，期望 %d", fullCount, backupRetentionCount)
	}
	if otherCount != 1 {
		t.Errorf("增量类型不应被误删，剩余 %d", otherCount)
	}
	var oldest model.Backup
	if err := database.First(&oldest, "backup_name = ?", "prune_full_00").Error; err == nil {
		t.Errorf("最旧备份未被清扫，仍存在 id=%d", oldest.ID)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("被清扫备份的目录应整目录删除，oldDir 仍存在: %v", err)
	}
}

// TestBackupService_ExecuteBackup_ZipAndDirModeRestricted 钉住落盘权限。
// exportData 经假仓储驱动（理由见文件头），其余路径（落盘/压缩/权限/状态机）
// 走真实仓储与磁盘。
func TestBackupService_ExecuteBackup_ZipAndDirModeRestricted(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	svc := &BackupService{
		backupRepo:     newTestBackupRepository(database),
		backupDataRepo: newAuditDumpRepo(),
	}

	backup := &model.Backup{BackupName: "perm_probe", Status: model.BackupStatusPending, CreatedBy: 1}
	if err := svc.backupRepo.Create(ctx, backup); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	svc.executeBackup(ctx, backup)

	if backup.Status != model.BackupStatusCompleted {
		t.Fatalf("executeBackup status=%s err=%s，期望 completed", backup.Status, backup.ErrorMessage)
	}
	info, err := os.Stat(backup.FilePath)
	if err != nil {
		t.Fatalf("stat zip: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("zip 权限=%o，期望 0600（含 PII 不应对同组/其他用户可读）", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(backup.FilePath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("备份目录权限=%o，期望 0700", dirInfo.Mode().Perm())
	}
	// 清理：本测试在真实备份根目录落盘，清掉避免污染 backups/。
	if err := os.RemoveAll(filepath.Dir(backup.FilePath)); err != nil {
		t.Logf("清理备份目录失败: %v", err)
	}
}

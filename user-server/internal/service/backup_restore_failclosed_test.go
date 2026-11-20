package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"

	"gorm.io/gorm"
)

// fakeBackupDataRepo 可注入失败的恢复仓储（FA-05）+ 调用记录。
type fakeBackupDataRepo struct {
	clueExistsErr   error
	restoreClueErr  map[string]error // 按 id 注入写失败
	userExistsErr   error
	restoreUserErr  map[string]error
	existingClues   map[string]bool
	existingUsers   map[string]bool
	restoreUserRows []map[string]any
	mu              sync.Mutex
}

func (f *fakeBackupDataRepo) DumpClues(context.Context) (json.RawMessage, error) { return nil, nil }
func (f *fakeBackupDataRepo) DumpUsers(context.Context) (json.RawMessage, error) { return nil, nil }
func (f *fakeBackupDataRepo) DumpShortLinks(context.Context) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeBackupDataRepo) DumpTable(context.Context, string) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeBackupDataRepo) ClueExists(_ context.Context, _ uint, id string) (bool, error) {
	if f.clueExistsErr != nil {
		return false, f.clueExistsErr
	}
	return f.existingClues[id], nil
}
func (f *fakeBackupDataRepo) RestoreClue(_ context.Context, row map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, _ := row["id"].(string)
	if err, ok := f.restoreClueErr[id]; ok {
		return err
	}
	return nil
}
func (f *fakeBackupDataRepo) UserExistsByUsername(_ context.Context, _ uint, u string) (bool, error) {
	if f.userExistsErr != nil {
		return false, f.userExistsErr
	}
	return f.existingUsers[u], nil
}
func (f *fakeBackupDataRepo) RestoreUser(_ context.Context, row map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	username, _ := row["username"].(string)
	if err, ok := f.restoreUserErr[username]; ok {
		return err
	}
	f.restoreUserRows = append(f.restoreUserRows, row)
	return nil
}
func (f *fakeBackupDataRepo) ShortLinkExistsByCode(_ context.Context, _ uint, _ string) (bool, error) {
	return false, nil
}
func (f *fakeBackupDataRepo) RestoreShortLink(_ context.Context, _ map[string]any) error {
	return nil
}
func (f *fakeBackupDataRepo) RestoreTable(context.Context, string, []map[string]any) error {
	return nil
}

// restoreTestPayload 生成完整 data.json：扩表键从 backupExtraTables 同源生成（空数组），
// 保证完整性探针通过且不因清单扩表漂移而失效。
func restoreTestPayload(marker string) []byte {
	return []byte(fmt.Sprintf(`{
	  "version":"1.0.0",
	  "clues":[{"id":"clue-%s","tenant_id":7,"name":"线索%s"}],
	  "users":[{"username":"user-%s","tenant_id":7,"password":"pw","role":"admin","status":2,"account_id":"acc-%s","real_name":"真名%s","email":"u-%s@t.local","phone":"1380000%s"}],
	  "short_link":[{"short_code":"code-%s","tenant_id":7,"original_url":"https://example.com/%s"}],
	  %s
	}`, marker, marker, marker, marker, marker, marker, marker, marker, marker,
		extraTableKeysJSON()))
}

func writeRestoreDataJSON(t *testing.T, dir, marker string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), restoreTestPayload(marker), 0600); err != nil {
		t.Fatalf("write data.json: %v", err)
	}
}

func newRestoreServiceWithFake(t *testing.T, fake *fakeBackupDataRepo) (*RestoreService, *model.RestoreRecord, *gorm.DB) {
	t.Helper()
	database := setupBackupServiceTestDB(t)
	record := &model.RestoreRecord{BackupID: 1, BackupName: "synthetic", Status: "pending"}
	if err := database.Create(record).Error; err != nil {
		t.Fatalf("seed restore record: %v", err)
	}
	svc := &RestoreService{
		restoreRepo:    repository.NewRestoreRecordRepositoryWithDB(database),
		backupRepo:     repository.NewBackupRepositoryWithDB(database),
		backupDataRepo: fake,
	}
	return svc, record, database
}

// FA-05①：基础表写失败不得报 completed。
func TestRestoreDatabase_BaseTableWriteFailureFailsRestore(t *testing.T) {
	fake := &fakeBackupDataRepo{restoreClueErr: map[string]error{"clue-A": fmt.Errorf("db write boom")}}
	svc, record, _ := newRestoreServiceWithFake(t, fake)
	t.Setenv("RESTORE_TMP_DIR", t.TempDir())
	_, restoreTmp := storage.BackupSource()
	workDir := filepath.Join(restoreTmp, "task-a")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeRestoreDataJSON(t, workDir, "A")

	err := svc.restoreDatabase(context.Background(), &model.Backup{BackupName: "bk"}, workDir)
	if err == nil {
		t.Fatal("基础表写失败必须返回错误（不得静默 completed）")
	}
	if !strings.Contains(err.Error(), "线索失败 1 条") || !strings.Contains(err.Error(), "clue-A") {
		t.Fatalf("错误应带可对账计数与失败标识，实际: %v", err)
	}

	// executeRestore 级：任务必须落 failed 状态（用真 zip 走完整解压→恢复链）。
	zips := filepath.Join(t.TempDir(), "zips")
	if err := os.MkdirAll(zips, 0700); err != nil {
		t.Fatal(err)
	}
	backup := &model.Backup{BackupName: "bk", Status: model.BackupStatusCompleted,
		FilePath: writeSyntheticZipWithClueFailure(t, zips), BackupType: "db"}
	svc.executeRestore(context.Background(), record, backup)
	gotRecord, gerr := svc.restoreRepo.GetByID(context.Background(), record.ID)
	if gerr != nil {
		t.Fatalf("读回任务失败: %v", gerr)
	}
	if gotRecord.Status != "failed" {
		t.Fatalf("任务必须显示 failed，得到 %s", gotRecord.Status)
	}
	if !strings.Contains(gotRecord.ErrorMessage, "线索失败 1 条") {
		t.Fatalf("失败消息应带计数，实际: %s", gotRecord.ErrorMessage)
	}
}

// FA-05②：已存在记录跳过 + 用户角色/状态/关联字段透传。
func TestRestoreDatabase_SkipExistingAndUserFieldsCarried(t *testing.T) {
	fake := &fakeBackupDataRepo{existingClues: map[string]bool{"clue-B": true}}
	svc, _, _ := newRestoreServiceWithFake(t, fake)
	t.Setenv("RESTORE_TMP_DIR", t.TempDir())
	_, restoreTmp := storage.BackupSource()
	workDir := filepath.Join(restoreTmp, "task-b")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeRestoreDataJSON(t, workDir, "B")

	if err := svc.restoreDatabase(context.Background(), &model.Backup{BackupName: "bk"}, workDir); err != nil {
		t.Fatalf("已存在跳过语义下恢复应成功: %v", err)
	}
	if len(fake.restoreUserRows) != 1 {
		t.Fatalf("应恢复 1 个用户，实际 %d", len(fake.restoreUserRows))
	}
	row := fake.restoreUserRows[0]
	if row["role"] != "admin" || row["account_id"] != "acc-B" || row["real_name"] != "真名B" {
		t.Fatalf("角色/关联字段必须随备份还原: %v", row)
	}
	// data.json 经 JSON 反序列化，数字一律 float64
	if st, ok := row["status"].(float64); !ok || st != 2 {
		t.Fatalf("status 必须还原为 2，得到 %v(%T)", row["status"], row["status"])
	}
}

// FA-05③：exists 检查读失败同样计为该行失败，不得当"无记录"放行。
func TestRestoreDatabase_ExistsCheckFailureCountsAsFailure(t *testing.T) {
	fake := &fakeBackupDataRepo{userExistsErr: fmt.Errorf("db read boom")}
	svc, _, _ := newRestoreServiceWithFake(t, fake)
	t.Setenv("RESTORE_TMP_DIR", t.TempDir())
	_, restoreTmp := storage.BackupSource()
	workDir := filepath.Join(restoreTmp, "task-c")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeRestoreDataJSON(t, workDir, "C")

	err := svc.restoreDatabase(context.Background(), &model.Backup{BackupName: "bk"}, workDir)
	if err == nil || !strings.Contains(err.Error(), "用户失败 1 条") {
		t.Fatalf("exists 检查失败必须计为行失败，实际: %v", err)
	}
	if len(fake.restoreUserRows) != 0 {
		t.Fatalf("读失败行不得被写入，实际写入 %d 行", len(fake.restoreUserRows))
	}
}

// —— FA-06：任务私有目录与并发串包 ——

// writeSyntheticZip 生成带完整 data.json（扩表键同源）的备份 zip。
func writeSyntheticZip(t *testing.T, dir, marker string) string {
	t.Helper()
	zp := filepath.Join(dir, fmt.Sprintf("backup-%s.zip", marker))
	zf, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zf.Close() }()
	zw := zip.NewWriter(zf)
	w, err := zw.Create("data.json")
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"version":"1.0.0","clues":[],"users":[],"short_link":[],"marker":"%s",%s}`,
		marker, extraTableKeysJSON())
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zp
}

// writeSyntheticZipWithClueFailure 带一条 clue 的备份（供注入写失败的失败链用例）。
func writeSyntheticZipWithClueFailure(t *testing.T, dir string) string {
	t.Helper()
	zp := filepath.Join(dir, "backup-failclue.zip")
	zf, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zf.Close() }()
	zw := zip.NewWriter(zf)
	w, err := zw.Create("data.json")
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"version":"1.0.0","clues":[{"id":"clue-A","tenant_id":7,"name":"x"}],"users":[],"short_link":[],%s}`,
		extraTableKeysJSON())
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zp
}

// extraTableKeysJSON 从 backupExtraTables 同源生成 `"key":[]` 片段。
func extraTableKeysJSON() string {
	keys := make([]string, 0, len(backupExtraTables))
	for _, t := range backupExtraTables {
		keys = append(keys, fmt.Sprintf(`"%s":[]`, t.key))
	}
	return strings.Join(keys, ",")
}

// FA-06①：两份备份并发恢复——各自解压到私有目录、互不串包；
// 任务结束后私有目录全部清理。
func TestRestoreService_ConcurrentRestoresUsePrivateDirs(t *testing.T) {
	t.Setenv("RESTORE_TMP_DIR", t.TempDir())
	_, restoreTmp := storage.BackupSource()
	zips := filepath.Join(t.TempDir(), "zips")
	if err := os.MkdirAll(zips, 0700); err != nil {
		t.Fatal(err)
	}

	database := setupBackupServiceTestDB(t)
	var mkOnce sync.Mutex
	mkTask := func(idx int) (*RestoreService, *model.RestoreRecord, *model.Backup) {
		mkOnce.Lock()
		defer mkOnce.Unlock()
		zp := writeSyntheticZip(t, zips, fmt.Sprintf("m%d", idx))
		backup := &model.Backup{BackupName: fmt.Sprintf("bk%d", idx), Status: model.BackupStatusCompleted, FilePath: zp, BackupType: "db"}
		if err := database.Create(backup).Error; err != nil {
			t.Fatalf("seed backup: %v", err)
		}
		record := &model.RestoreRecord{BackupID: backup.ID, BackupName: backup.BackupName, Status: "pending"}
		if err := database.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
		svc := &RestoreService{
			restoreRepo:    repository.NewRestoreRecordRepositoryWithDB(database),
			backupRepo:     repository.NewBackupRepositoryWithDB(database),
			backupDataRepo: &fakeBackupDataRepo{},
		}
		return svc, record, backup
	}

	var wg sync.WaitGroup
	results := make([]*model.RestoreRecord, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			svc, record, backup := mkTask(idx)
			svc.executeRestore(context.Background(), record, backup)
			results[idx] = record
		}(i)
	}
	wg.Wait()

	for idx, r := range results {
		if r == nil || r.Status != "completed" {
			t.Fatalf("并发恢复任务 %d 必须各自完成，得到 %+v", idx, r)
		}
		if strings.Contains(r.ErrorMessage, "data.json") {
			t.Fatalf("并发任务 %d 读到了串包的 data.json: %s", idx, r.ErrorMessage)
		}
	}
	// 文件生命周期：所有任务目录清理干净，restore_tmp 根不留 restore_task_*。
	entries, err := os.ReadDir(restoreTmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "restore_task_") {
			t.Fatalf("任务私有目录未清理: %s", e.Name())
		}
	}
}

// FA-06②：解压目标=指定工作目录（不再写全局 restore_tmp 根）。
func TestRestoreService_DecompressWritesToTaskDir(t *testing.T) {
	svc := &RestoreService{}
	t.Setenv("RESTORE_TMP_DIR", t.TempDir())
	_, restoreTmp := storage.BackupSource()
	taskDir := filepath.Join(restoreTmp, "restore_task_9_9")
	zips := filepath.Join(t.TempDir(), "zips")
	if err := os.MkdirAll(zips, 0700); err != nil {
		t.Fatal(err)
	}
	zp := writeSyntheticZip(t, zips, "iso")

	if err := svc.decompressBackup(context.Background(), zp, taskDir); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "data.json")); err != nil {
		t.Fatalf("data.json 应落在任务目录: %v", err)
	}
	entries, _ := os.ReadDir(restoreTmp)
	for _, e := range entries {
		if e.Name() != "restore_task_9_9" {
			t.Fatalf("解压泄漏到共享根: %s", e.Name())
		}
	}
}

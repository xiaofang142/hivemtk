package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupBackupServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.Backup{},
		&model.RestoreRecord{},
		&model.Clue{},
		&model.User{},
		&model.ShortLink{},
	)
	db.SetTestDB(database)
	return database
}

func newTestBackupRepository(database *gorm.DB) *repository.BackupRepository {
	return repository.NewBackupRepositoryWithDB(database)
}

func newTestRestoreRecordRepository(database *gorm.DB) *repository.RestoreRecordRepository {
	return repository.NewRestoreRecordRepositoryWithDB(database)
}

// TestBackupService_CreateBackup 测试创建备份
func TestBackupService_CreateBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	req := &CreateBackupRequest{
		BackupName: "test-backup",
		BackupType: model.BackupTypeFull,
	}

	backup, err := service.CreateBackup(ctx, 1, req)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if backup == nil {
		t.Fatal("Expected non-nil backup")
	}

	_ = backup

	if backup.BackupName != "test-backup" {
		t.Errorf("Expected BackupName 'test-backup', got %s", backup.BackupName)
	}

	if backup.BackupType != model.BackupTypeFull {
		t.Errorf("Expected BackupType 'full', got %s", backup.BackupType)
	}

	if backup.Status != model.BackupStatusPending {
		t.Errorf("Expected Status 'pending', got %s", backup.Status)
	}

	if backup.CreatedBy != 1 {
		t.Errorf("Expected CreatedBy 1, got %d", backup.CreatedBy)
	}

	var count int64
	database.Model(&model.Backup{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 backup record, got %d", count)
	}
}

// TestBackupService_CreateBackup_EmptyName 测试创建备份时名称为空
func TestBackupService_CreateBackup_EmptyName(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	req := &CreateBackupRequest{
		BackupName: "",
		BackupType: model.BackupTypeFull,
	}

	backup, err := service.CreateBackup(ctx, 1, req)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if backup.BackupName == "" {
		t.Error("Expected auto-generated backup name")
	}
}

// TestBackupService_CreateBackup_EmptyType 测试创建备份时类型为空
func TestBackupService_CreateBackup_EmptyType(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	req := &CreateBackupRequest{
		BackupName: "test-backup",
		BackupType: "",
	}

	backup, err := service.CreateBackup(ctx, 1, req)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if backup.BackupType != model.BackupTypeFull {
		t.Errorf("Expected default BackupType 'full', got %s", backup.BackupType)
	}
}

// TestBackupService_GetBackupList 测试获取备份列表
func TestBackupService_GetBackupList(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	for i := 0; i < 5; i++ {
		backup := &model.Backup{
			BackupName: "backup-" + string(rune('0'+i)),
			BackupType: model.BackupTypeFull,
			Status:     model.BackupStatusCompleted,
			CreatedBy:  1,
		}
		repo.Create(context.Background(), backup)
	}

	backups, total, err := service.GetBackupList(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetBackupList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(backups) != 5 {
		t.Errorf("Expected 5 backups, got %d", len(backups))
	}
}

// TestBackupService_GetBackupList_Pagination 测试备份列表分页
func TestBackupService_GetBackupList_Pagination(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	for i := 0; i < 10; i++ {
		backup := &model.Backup{
			BackupName: "backup-" + string(rune('0'+i)),
			Status:     model.BackupStatusCompleted,
			CreatedBy:  1,
		}
		repo.Create(context.Background(), backup)
	}

	backups, total, err := service.GetBackupList(ctx, 1, 5)
	if err != nil {
		t.Fatalf("GetBackupList failed: %v", err)
	}

	if total != 10 {
		t.Errorf("Expected total 10, got %d", total)
	}

	if len(backups) != 5 {
		t.Errorf("Expected 5 backups on page 1, got %d", len(backups))
	}
}

// TestBackupService_GetBackupByID 测试根据 ID 获取备份
func TestBackupService_GetBackupByID(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	backup := &model.Backup{
		BackupName: "test-backup",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	retrievedBackup, err := service.GetBackupByID(ctx, backup.ID)
	if err != nil {
		t.Fatalf("GetBackupByID failed: %v", err)
	}

	if retrievedBackup.BackupName != "test-backup" {
		t.Errorf("Expected BackupName 'test-backup', got %s", retrievedBackup.BackupName)
	}
}

// TestBackupService_GetBackupByID_NotFound 测试获取不存在的备份
func TestBackupService_GetBackupByID_NotFound(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	_, err := service.GetBackupByID(ctx, 99999)
	if err == nil {
		t.Error("Expected error for non-existent backup")
	}
}

// TestBackupService_GetBackupByID_SingleTenant 单租户模式下任意用户可访问备份
func TestBackupService_GetBackupByID_SingleTenant(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	backup := &model.Backup{
		BackupName: "shared-backup",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	got, err := service.GetBackupByID(ctx, backup.ID)
	if err != nil {
		t.Fatalf("GetBackupByID failed: %v", err)
	}
	if got == nil || got.ID != backup.ID {
		t.Errorf("Expected backup ID %d, got %v", backup.ID, got)
	}
}

// TestBackupService_DeleteBackup 测试删除备份
func TestBackupService_DeleteBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	backup := &model.Backup{
		BackupName: "test-backup",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	err := service.DeleteBackup(ctx, backup.ID)
	if err != nil {
		t.Fatalf("DeleteBackup failed: %v", err)
	}

	var count int64
	database.Model(&model.Backup{}).Where("id = ?", backup.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected backup to be deleted, got count %d", count)
	}
}

// TestBackupService_DeleteBackup_NotFound 测试删除不存在的备份
func TestBackupService_DeleteBackup_NotFound(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	err := service.DeleteBackup(ctx, 99999)
	if err == nil {
		t.Error("Expected error for non-existent backup")
	}
}

// TestBackupService_DeleteBackup_SingleTenant 单租户模式下任意用户可删除备份
func TestBackupService_DeleteBackup_SingleTenant(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	backup := &model.Backup{
		BackupName: "shared-backup",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	err := service.DeleteBackup(ctx, backup.ID)
	if err != nil {
		t.Fatalf("DeleteBackup failed: %v", err)
	}

	var count int64
	database.Model(&model.Backup{}).Where("id = ?", backup.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected backup to be deleted, got count %d", count)
	}
}

// TestBackupService_DeleteBackup_WithFile 测试删除带文件的备份
func TestBackupService_DeleteBackup_WithFile(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "test-backup.zip")
	err := os.WriteFile(backupFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	backup := &model.Backup{
		BackupName: "test-backup",
		FilePath:   backupFile,
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	err = service.DeleteBackup(ctx, backup.ID)
	if err != nil {
		t.Fatalf("DeleteBackup failed: %v", err)
	}

	if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
		t.Error("Expected backup file to be deleted")
	}
}

// TestRestoreService_RestoreBackup 测试恢复备份
func TestRestoreService_RestoreBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	backup := &model.Backup{
		BackupName:  "test-backup",
		Status:      model.BackupStatusCompleted,
		FilePath:    "/backups/test-backup.zip",
		CreatedBy:   1,
		StartedAt:   time.Now(),
		CompletedAt: func() *time.Time { t := time.Now(); return &t }(),
	}
	backupRepo.Create(context.Background(), backup)

	req := &RestoreBackupRequest{
		BackupID: backup.ID,
	}

	record, err := service.RestoreBackup(ctx, 1, req)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	if record == nil {
		t.Fatal("Expected non-nil restore record")
	}

	if record.BackupID != backup.ID {
		t.Errorf("Expected BackupID %d, got %d", backup.ID, record.BackupID)
	}

	if record.Status != "pending" {
		t.Errorf("Expected Status 'pending', got %s", record.Status)
	}

	var count int64
	database.Model(&model.RestoreRecord{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 restore record, got %d", count)
	}
}

// TestRestoreService_RestoreBackup_NotFound 测试恢复不存在的备份
func TestRestoreService_RestoreBackup_NotFound(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	req := &RestoreBackupRequest{
		BackupID: 99999,
	}

	_, err := service.RestoreBackup(ctx, 1, req)
	if err == nil {
		t.Error("Expected error for non-existent backup")
	}
}

// TestRestoreService_RestoreBackup_SingleTenant 单租户模式下任意用户可恢复备份
func TestRestoreService_RestoreBackup_SingleTenant(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	backup := &model.Backup{
		BackupName: "shared-backup",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	backupRepo.Create(context.Background(), backup)

	req := &RestoreBackupRequest{
		BackupID: backup.ID,
	}

	_, err := service.RestoreBackup(ctx, 1, req)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
}

// TestRestoreService_RestoreBackup_Incomplete 测试恢复未完成的备份
func TestRestoreService_RestoreBackup_Incomplete(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	backup := &model.Backup{
		BackupName: "incomplete-backup",
		Status:     model.BackupStatusPending,
		CreatedBy:  1,
	}
	backupRepo.Create(context.Background(), backup)

	req := &RestoreBackupRequest{
		BackupID: backup.ID,
	}

	_, err := service.RestoreBackup(ctx, 1, req)
	if err == nil {
		t.Error("Expected error for incomplete backup")
	}
	if err.Error() != "备份未完成，无法恢复" {
		t.Errorf("Expected '备份未完成，无法恢复', got %s", err.Error())
	}
}

// TestRestoreService_GetRestoreList 测试获取恢复记录列表
func TestRestoreService_GetRestoreList(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	for i := 0; i < 5; i++ {
		record := &model.RestoreRecord{
			BackupID:   uint(i + 1),
			BackupName: "backup-" + string(rune('0'+i)),
			Status:     "completed",
			CreatedBy:  1,
		}
		restoreRepo.Create(context.Background(), record)
	}

	records, total, err := service.GetRestoreList(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetRestoreList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(records) != 5 {
		t.Errorf("Expected 5 records, got %d", len(records))
	}
}

// TestRestoreService_GetLastRestore 测试获取最近一次恢复记录
func TestRestoreService_GetLastRestore(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	restoreRepo.Create(context.Background(), &model.RestoreRecord{
		BackupID:   1,
		BackupName: "backup-1",
		Status:     "completed",
		CreatedBy:  1,
	})

	time.Sleep(10 * time.Millisecond)

	restoreRepo.Create(context.Background(), &model.RestoreRecord{
		BackupID:   2,
		BackupName: "backup-2",
		Status:     "completed",
		CreatedBy:  1,
	})

	lastRecord, err := service.GetLastRestore(ctx)
	if err != nil {
		t.Fatalf("GetLastRestore failed: %v", err)
	}

	if lastRecord.BackupID != 2 {
		t.Errorf("Expected last BackupID 2, got %d", lastRecord.BackupID)
	}
}

// TestRestoreService_GetLastRestore_Empty 测试没有恢复记录的情况
func TestRestoreService_GetLastRestore_Empty(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	_, err := service.GetLastRestore(ctx)
	if err == nil {
		t.Error("Expected error for empty restore records")
	}
}

// TestScheduleBackupService_CreateDailyBackup 测试创建每日备份
func TestScheduleBackupService_CreateDailyBackup(t *testing.T) {
	setupBackupServiceTestDB(t)
	ctx := context.Background()
	service := NewScheduleBackupService()

	err := service.CreateDailyBackup(ctx)
	if err != nil {
		t.Fatalf("CreateDailyBackup failed: %v", err)
	}
}

// TestRunDailyBackup 测试运行每日备份函数
func TestRunDailyBackup(t *testing.T) {
	setupBackupServiceTestDB(t)
	RunDailyBackup()
}

// TestBackupService_ExecuteBackup_DirCreationError 测试创建备份目录失败
func TestBackupService_ExecuteBackup_DirCreationError(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{
		backupRepo:     repo,
		backupDataRepo: repository.NewBackupDataRepositoryWithDB(database),
	}

	backup := &model.Backup{
		BackupName: "test-backup",
		Status:     model.BackupStatusPending,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	service.executeBackup(ctx, backup)

	time.Sleep(100 * time.Millisecond)

	var updatedBackup model.Backup
	database.First(&updatedBackup, backup.ID)

	if updatedBackup.Status != model.BackupStatusCompleted && updatedBackup.Status != model.BackupStatusFailed {
		t.Errorf("Expected status 'completed' or 'failed', got %s", updatedBackup.Status)
	}
}

// TestBackupService_CompressBackup 测试压缩备份功能
func TestBackupService_CompressBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	err := os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	outputFile := filepath.Join(tmpDir, "output.zip")

	err = service.compressBackup(ctx, tmpDir, outputFile)
	if err != nil {
		t.Fatalf("compressBackup failed: %v", err)
	}

	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Expected zip file to be created")
	}
}

// TestRestoreService_DecompressBackup 测试解压备份功能
func TestRestoreService_DecompressBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "data.json")
	testContent := `{"user_id": "test-merchant"}`
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	zipFile := filepath.Join(tmpDir, "backup.zip")

	testService := &BackupService{backupRepo: backupRepo}
	err = testService.compressBackup(ctx, tmpDir, zipFile)
	if err != nil {
		t.Fatalf("Failed to create valid zip: %v", err)
	}

	os.Remove(testFile)

	err = service.decompressBackup(ctx, zipFile, filepath.Join(tmpDir, "decompressed"))
	if err != nil {
		t.Fatalf("decompressBackup failed: %v", err)
	}
}

// TestRestoreService_ExecuteRestore_FailedBackup 测试恢复失败的处理
func TestRestoreService_ExecuteRestore_FailedBackup(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	record := &model.RestoreRecord{
		BackupID:   1,
		BackupName: "test-backup",
		Status:     "pending",
		CreatedBy:  1,
	}
	restoreRepo.Create(context.Background(), record)

	backup := &model.Backup{
		BackupName: "test-backup",
		FilePath:   "/nonexistent/backup.zip",
		Status:     model.BackupStatusCompleted,
		CreatedBy:  1,
	}
	backupRepo.Create(context.Background(), backup)

	service.executeRestore(ctx, record, backup)

	updatedRecord, _ := restoreRepo.GetByID(context.Background(), record.ID)

	if updatedRecord.Status != "failed" {
		t.Errorf("Expected status 'failed', got %s", updatedRecord.Status)
	}
}

func TestBackupService_GetBackupList_EmptyMerchant(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	backups, total, err := service.GetBackupList(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetBackupList failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0, got %d", total)
	}

	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}
}

func TestRestoreService_GetRestoreList_EmptyMerchant(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	backupRepo := newTestBackupRepository(database)
	restoreRepo := newTestRestoreRecordRepository(database)
	service := &RestoreService{
		restoreRepo: restoreRepo,
		backupRepo:  backupRepo,
	}

	records, total, err := service.GetRestoreList(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetRestoreList failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0, got %d", total)
	}

	if len(records) != 0 {
		t.Errorf("Expected 0 records, got %d", len(records))
	}
}

// TestBackupService_MultipleBackupTypes 测试多种备份类型
func TestBackupService_MultipleBackupTypes(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	ctx := context.Background()
	repo := newTestBackupRepository(database)
	service := &BackupService{backupRepo: repo}

	fullReq := &CreateBackupRequest{
		BackupName: "full-backup",
		BackupType: model.BackupTypeFull,
	}
	fullBackup, err := service.CreateBackup(ctx, 1, fullReq)
	if err != nil {
		t.Fatalf("CreateBackup (full) failed: %v", err)
	}
	if fullBackup.BackupType != model.BackupTypeFull {
		t.Errorf("Expected BackupType 'full', got %s", fullBackup.BackupType)
	}

	incrementalReq := &CreateBackupRequest{
		BackupName: "incremental-backup",
		BackupType: model.BackupTypeIncremental,
	}
	incrementalBackup, err := service.CreateBackup(ctx, 1, incrementalReq)
	if err != nil {
		t.Fatalf("CreateBackup (incremental) failed: %v", err)
	}
	if incrementalBackup.BackupType != model.BackupTypeIncremental {
		t.Errorf("Expected BackupType 'incremental', got %s", incrementalBackup.BackupType)
	}
}

// TestBackupService_RepositoryNil 测试仓库为 nil 的情况
func TestBackupService_RepositoryNil(t *testing.T) {
	service := &BackupService{backupRepo: nil}
	ctx := context.Background()

	req := &CreateBackupRequest{
		BackupName: "test-backup",
		BackupType: model.BackupTypeFull,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Logf("Expected panic when repository is nil: %v", r)
		} else {
			t.Error("Expected panic when repository is nil")
		}
	}()

	_, _ = service.CreateBackup(ctx, 1, req)
}

// TestRestoreService_RepositoryNil 测试恢复服务仓库为 nil 的情况
func TestRestoreService_RepositoryNil(t *testing.T) {
	service := &RestoreService{
		restoreRepo: nil,
		backupRepo:  nil,
	}
	ctx := context.Background()

	req := &RestoreBackupRequest{
		BackupID: 1,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Logf("Expected panic when repository is nil: %v", r)
		} else {
			t.Error("Expected panic when repository is nil")
		}
	}()

	_, _ = service.RestoreBackup(ctx, 1, req)
}

// TestBackupService_BackupStatusTransitions 测试备份状态转换
func TestBackupService_BackupStatusTransitions(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	repo := newTestBackupRepository(database)

	backup := &model.Backup{
		BackupName: "test-backup",
		Status:     model.BackupStatusPending,
		CreatedBy:  1,
	}
	repo.Create(context.Background(), backup)

	backup.Status = model.BackupStatusRunning
	repo.Update(context.Background(), backup)

	updated, _ := repo.GetByID(context.Background(), backup.ID)
	if updated.Status != model.BackupStatusRunning {
		t.Errorf("Expected status 'running', got %s", updated.Status)
	}

	backup.Status = model.BackupStatusCompleted
	repo.Update(context.Background(), backup)

	updated, _ = repo.GetByID(context.Background(), backup.ID)
	if updated.Status != model.BackupStatusCompleted {
		t.Errorf("Expected status 'completed', got %s", updated.Status)
	}
}

// TestRestoreService_RestoreStatusTransitions 测试恢复状态转换
func TestRestoreService_RestoreStatusTransitions(t *testing.T) {
	database := setupBackupServiceTestDB(t)
	restoreRepo := newTestRestoreRecordRepository(database)

	record := &model.RestoreRecord{
		BackupID:   1,
		BackupName: "test-backup",
		Status:     "pending",
		CreatedBy:  1,
	}
	restoreRepo.Create(context.Background(), record)

	record.Status = "running"
	restoreRepo.Update(context.Background(), record)

	updated, _ := restoreRepo.GetByID(context.Background(), record.ID)
	if updated.Status != "running" {
		t.Errorf("Expected status 'running', got %s", updated.Status)
	}

	record.Status = "completed"
	restoreRepo.Update(context.Background(), record)

	updated, _ = restoreRepo.GetByID(context.Background(), record.ID)
	if updated.Status != "completed" {
		t.Errorf("Expected status 'completed', got %s", updated.Status)
	}
}

// TestNewBackupService 测试创建备份服务实例
func TestNewBackupService(t *testing.T) {
	service := NewBackupService()
	if service == nil {
		t.Error("Expected non-nil backup service")
	}
}

// TestNewRestoreService 测试创建恢复服务实例
func TestNewRestoreService(t *testing.T) {
	service := NewRestoreService()
	if service == nil {
		t.Error("Expected non-nil restore service")
	}
}

// TestNewScheduleBackupService 测试创建定时备份服务实例
func TestNewScheduleBackupService(t *testing.T) {
	service := NewScheduleBackupService()
	if service == nil {
		t.Error("Expected non-nil schedule backup service")
	}
}

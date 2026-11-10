package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"time"

	"gorm.io/gorm"
)

type RegistryInitializer func(*MigrationRegistry, *gorm.DB)

type MigrationService struct {
	registry       *MigrationRegistry
	db             *gorm.DB
	taskRepo       *repository.UpgradeTaskRepository
	recordRepo     *repository.MigrationRecordRepository
	checkpointRepo *repository.MigrationCheckpointRepository
}

// NewMigrationService 创建迁移服务实例(保留旧签名,兼容旧调用)
func NewMigrationService(registry *MigrationRegistry, db *gorm.DB, initFunc RegistryInitializer) *MigrationService {
	initFunc(registry, db)
	return &MigrationService{
		registry:       registry,
		db:             db,
		taskRepo:       repository.NewUpgradeTaskRepository(),
		recordRepo:     repository.NewMigrationRecordRepository(),
		checkpointRepo: repository.NewMigrationCheckpointRepository(),
	}
}

// NewMigrationServiceDefault 创建迁移服务实例(无 db,内部用 dbUtil.GetDB())
func NewMigrationServiceDefault(registry *MigrationRegistry, initFunc RegistryInitializer) *MigrationService {
	return NewMigrationService(registry, dbUtil.GetDB(), initFunc)
}

func (s *MigrationService) ExecuteUpgrade(ctx context.Context, fromVersion, toVersion string) (*model.UpgradeTask, error) {
	task := &model.UpgradeTask{
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Status:      "pending",
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}

	// SafeGo：升级任务异步执行跑 DDL/数据迁移链，panic 不能击穿进程
	utils.SafeGo(ctx, "migration.execute_upgrade", func(_ context.Context) {
		s.executeUpgradeAsync(ctx, task.ID, fromVersion, toVersion)
	})
	return task, nil
}

// WaitForTask 阻塞等待升级任务完成（main 启动期保证 audit 表就绪）
//
// 超时或任务失败返回 error；成功（completed）返回 nil。
// 通过轮询 upgrade_tasks 表 status 字段实现，间隔 200ms。
func (s *MigrationService) WaitForTask(ctx context.Context, taskID uint, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		task, err := s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("查询 upgrade_tasks 失败: %w", err)
		}
		if task == nil {
			return fmt.Errorf("upgrade_task %d not found", taskID)
		}
		switch task.Status {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("upgrade_task %d failed: %s", taskID, task.ErrorMessage)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 upgrade_task %d 超时（%v）", taskID, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *MigrationService) executeUpgradeAsync(parentCtx context.Context, taskID uint, fromVersion, toVersion string) {
	// 后台升级 goroutine 是平台级操作：它脱离请求 ctx（原先固定 context.Background()），
	// TENANT_MODE=on 时 upgrade_tasks 状态落库、migration_records 读写与 migration.Up 的
	// 租户表访问没有租户可绑，会被隔离 callback fail-closed —— 任务状态永远停在 pending，
	// WaitForTask 超时。走 tenant.WithSystemScope 豁免通道（与启动期调用点同源；
	// HTTP 触发的升级同样经这条后台链路，其请求 ctx 本就被这里丢弃，故不改变请求路径语义）。
	bgCtx := tenant.WithSystemScope(context.Background())

	if e := s.taskRepo.UpdateStatus(bgCtx, taskID, "running", 0, 0, "开始升级", ""); e != nil {
		logger.Warnf("[Migration] 任务状态落库失败 taskID=%d: %v", taskID, e)
	}

	executedVersions, _ := s.recordRepo.GetExecutedVersions(bgCtx)
	pendingMigrations := s.registry.GetPending(executedVersions)

	if len(pendingMigrations) == 0 {
		if e := s.taskRepo.UpdateStatus(bgCtx, taskID, "completed", 100, 0, "无需升级", ""); e != nil {
			logger.Warnf("[Migration] 任务状态落库失败 taskID=%d: %v", taskID, e)
		}
		return
	}

	totalSteps := len(pendingMigrations)
	for i, migration := range pendingMigrations {
		currentStep := i + 1
		stepDesc := "执行迁移：" + migration.Name()

		progress := (currentStep * 100) / totalSteps
		if e := s.taskRepo.UpdateStatus(bgCtx, taskID, "running", progress, currentStep, stepDesc, ""); e != nil {
			logger.Warnf("[Migration] 任务进度落库失败 taskID=%d step=%d: %v", taskID, currentStep, e)
		}

		if err := migration.Up(bgCtx); err != nil {
			if e := s.taskRepo.UpdateStatus(bgCtx, taskID, "failed", progress, currentStep, stepDesc, err.Error()); e != nil {
				logger.Warnf("[Migration] 任务失败状态落库失败 taskID=%d: %v", taskID, e)
			}
			return
		}

		record := &model.MigrationRecord{
			Version:    migration.Version(),
			Name:       migration.Name(),
			Type:       "database",
			Status:     "completed",
			ExecutedAt: time.Now(),
			ExecutedBy: "system",
		}
		if e := s.recordRepo.Create(bgCtx, record); e != nil {
			logger.Errorf("[Migration] 迁移记录落库失败 version=%s: %v", record.Version, e)
		}
	}

	if e := s.taskRepo.UpdateStatus(bgCtx, taskID, "completed", 100, totalSteps, "升级完成", ""); e != nil {
		logger.Warnf("[Migration] 任务完成状态落库失败 taskID=%d: %v", taskID, e)
	}
}

func (s *MigrationService) GetUpgradeTask(ctx context.Context, id uint) (*model.UpgradeTask, error) {
	return s.taskRepo.GetByID(ctx, id)
}

func (s *MigrationService) GetUpgradeHistory(ctx context.Context, page, pageSize int) ([]*model.UpgradeTask, int64, error) {
	return s.taskRepo.GetAll(ctx, page, pageSize)
}

func (s *MigrationService) GetMigrationRecords(ctx context.Context) ([]*model.MigrationRecord, error) {
	return s.recordRepo.GetAll(ctx)
}

func (s *MigrationService) GetCurrentVersion(ctx context.Context) (string, error) {
	versions, err := s.recordRepo.GetExecutedVersions(ctx)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "v1.0.0", nil
	}
	return versions[len(versions)-1], nil
}

func (s *MigrationService) GetPendingMigrations(ctx context.Context) ([]Migration, error) {
	executedVersions, err := s.recordRepo.GetExecutedVersions(ctx)
	if err != nil {
		return nil, err
	}
	return s.registry.GetPending(executedVersions), nil
}

func (s *MigrationService) SaveCheckpoint(ctx context.Context, checkpoint string, data any) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	cp := &model.MigrationCheckpoint{
		Checkpoint: checkpoint,
		Data:       string(dataJSON),
	}
	return s.checkpointRepo.Upsert(ctx, cp)
}

func (s *MigrationService) GetCheckpoint(ctx context.Context, checkpoint string) (any, error) {
	cp, err := s.checkpointRepo.GetByCheckpoint(ctx, checkpoint)
	if err != nil {
		return nil, err
	}

	var data any
	if err := json.Unmarshal([]byte(cp.Data), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *MigrationService) Rollback(ctx context.Context, targetVersion string) error {
	migration, ok := s.registry.Get(targetVersion)
	if !ok {
		return errors.New("target version not found")
	}

	if err := migration.Down(ctx); err != nil {
		return err
	}

	record, err := s.recordRepo.GetByVersion(ctx, targetVersion)
	if err == nil && record != nil {
		record.Status = "rolled_back"
		if e := s.recordRepo.Update(ctx, record); e != nil {
			return fmt.Errorf("回滚已执行但状态更新失败 version=%s: %w", targetVersion, e)
		}
	}

	return nil
}

package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// backupUpdateColumns 备份记录的可变列白名单。
//
// 职责属性（backup_name / backup_type / created_by / created_at）不在其中：
// 后台执行体持有的是创建时的快照，整行 Save 会把期间可能发生的改名、
// 归属修正一并写回旧值。状态机只推进执行态字段。
var backupUpdateColumns = []string{
	"status", "file_path", "file_size", "error_message", "started_at", "completed_at",
}

// restoreRecordUpdateColumns 恢复记录的可变列白名单。
var restoreRecordUpdateColumns = []string{
	"status", "error_message", "restored_at",
}

// BackupRepository 备份仓库
type BackupRepository struct {
	db *gorm.DB
}

func NewBackupRepository() *BackupRepository {
	return &BackupRepository{
		db: _db.GetDB(),
	}
}

// NewBackupRepositoryWithDB 使用指定数据库创建备份仓库实例（用于测试）
func NewBackupRepositoryWithDB(database *gorm.DB) *BackupRepository {
	return &BackupRepository{
		db: database,
	}
}

// Create 创建备份记录
func (r *BackupRepository) Create(ctx context.Context, backup *model.Backup) error {
	return r.db.WithContext(ctx).Create(backup).Error
}

// GetByID 根据 ID 获取备份记录
func (r *BackupRepository) GetByID(ctx context.Context, id uint) (*model.Backup, error) {
	var backup model.Backup
	err := r.db.WithContext(ctx).First(&backup, id).Error
	return &backup, err
}

// ListByType 按备份类型全量列举（created_at 升序），供保留策略清点同型备份。
func (r *BackupRepository) ListByType(ctx context.Context, backupType model.BackupType) ([]*model.Backup, error) {
	var list []*model.Backup
	err := r.db.WithContext(ctx).
		Where("backup_type = ?", backupType).
		Order("created_at ASC, id ASC").
		Find(&list).Error
	return list, err
}

// GetAll 获取所有备份记录列表
func (r *BackupRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.Backup, int64, error) {
	var backups []*model.Backup
	var total int64

	page, pageSize = normalizeBackupPage(page, pageSize)
	if err := r.db.WithContext(ctx).Model(&model.Backup{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&backups).Error

	return backups, total, err
}

// normalizeBackupPage 归一分页入参：负 Offset 在 PostgreSQL 直接报错，
// 超大 pageSize 则会把整张备份清单一次拉出来。
func normalizeBackupPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	// 与 controller 的 DefaultBackupPageSizeCap（1000）对齐：仓储不再二次收紧，
	// 否则接口回了 1000 而底层只给 200，前端分页会静默缺页。
	if pageSize > 1000 {
		pageSize = 1000
	}
	return page, pageSize
}

// Update 更新备份记录（仅执行态列，不整行覆写）
func (r *BackupRepository) Update(ctx context.Context, backup *model.Backup) error {
	if backup == nil {
		return errors.New("backup is nil")
	}
	res := r.db.WithContext(ctx).Model(&model.Backup{}).
		Where("id = ?", backup.ID).
		Select(backupUpdateColumns).
		Updates(backup)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete 删除备份记录
func (r *BackupRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&model.Backup{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// RestoreRecordRepository 恢复记录仓库
type RestoreRecordRepository struct {
	db *gorm.DB
}

func NewRestoreRecordRepository() *RestoreRecordRepository {
	return &RestoreRecordRepository{
		db: _db.GetDB(),
	}
}

// NewRestoreRecordRepositoryWithDB 使用指定数据库创建恢复记录仓库实例（用于测试）
func NewRestoreRecordRepositoryWithDB(database *gorm.DB) *RestoreRecordRepository {
	return &RestoreRecordRepository{
		db: database,
	}
}

// Create 创建恢复记录
func (r *RestoreRecordRepository) Create(ctx context.Context, record *model.RestoreRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

// GetByID 根据 ID 获取恢复记录
func (r *RestoreRecordRepository) GetByID(ctx context.Context, id uint) (*model.RestoreRecord, error) {
	var record model.RestoreRecord
	err := r.db.WithContext(ctx).First(&record, id).Error
	return &record, err
}

// GetAll 获取所有恢复记录列表
func (r *RestoreRecordRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.RestoreRecord, int64, error) {
	var records []*model.RestoreRecord
	var total int64

	page, pageSize = normalizeBackupPage(page, pageSize)
	if err := r.db.WithContext(ctx).Model(&model.RestoreRecord{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error

	return records, total, err
}

// Update 更新恢复记录（仅执行态列，不整行覆写）
func (r *RestoreRecordRepository) Update(ctx context.Context, record *model.RestoreRecord) error {
	if record == nil {
		return errors.New("restore record is nil")
	}
	res := r.db.WithContext(ctx).Model(&model.RestoreRecord{}).
		Where("id = ?", record.ID).
		Select(restoreRecordUpdateColumns).
		Updates(record)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetLastRestore 获取最近一次恢复记录
func (r *RestoreRecordRepository) GetLastRestore(ctx context.Context) (*model.RestoreRecord, error) {
	var record model.RestoreRecord
	err := r.db.WithContext(ctx).Order("created_at DESC").
		First(&record).Error
	return &record, err
}

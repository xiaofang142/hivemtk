package repository

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/dbencrypt"
	_pagination "hivemtk-user/internal/pkg/pagination"

	"gorm.io/gorm"
)

// OperationLogRepository 操作日志仓库接口
// 独立部署版本：移除 merchantID 作用域
//
// 所有方法第一参数为 ctx context.Context。
type OperationLogRepository interface {
	Create(ctx context.Context, log *model.OperationLog) error
	GetByID(ctx context.Context, id uint) (*model.OperationLog, error)
	GetAll(ctx context.Context, page, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, error)
	// GetAllKeyset 使用 keyset（cursor）分页读取操作日志。
	// cursor 为空表示首页；返回的 nextCursor 为空表示已到末页。
	GetAllKeyset(ctx context.Context, cursor string, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, string, error)
	GetByUserID(ctx context.Context, userID uint, page, pageSize int) ([]*model.OperationLog, int64, error)
	DeleteOldLogs(ctx context.Context, beforeDate time.Time) error
	DeleteByIDs(ctx context.Context, ids []uint) (int64, error)
	UpdateNewValue(ctx context.Context, id uint, newValue string) error
}

type operationLogRepo struct {
	db *gorm.DB
}

// NewOperationLogRepository 创建操作日志仓库实例
func NewOperationLogRepository() OperationLogRepository {
	return &operationLogRepo{db: _db.GetDB()}
}

// NewOperationLogRepositoryWithDB 创建带数据库连接的操作日志仓库实例（用于测试 / 多 DB 场景）
func NewOperationLogRepositoryWithDB(db *gorm.DB) OperationLogRepository {
	return &operationLogRepo{db: db}
}

func (r *operationLogRepo) Create(ctx context.Context, log *model.OperationLog) error {
	// OPT-SEC-04：敏感字段 IP / User-Agent 落库前加密；dbencrypt 永不失败，
	// MASTER_KEY 缺失时降级明文，存量明文行读取时原样透传。
	log.IP = dbencrypt.Encrypt(log.IP)
	log.UserAgent = dbencrypt.Encrypt(log.UserAgent)
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *operationLogRepo) GetByID(ctx context.Context, id uint) (*model.OperationLog, error) {
	var log model.OperationLog
	err := r.db.WithContext(ctx).First(&log, id).Error
	if err == nil {
		decryptOperationLog(&log)
	}
	return &log, err
}

func (r *operationLogRepo) GetAll(ctx context.Context, page, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, error) {
	var logs []*model.OperationLog
	var total int64

	query := r.db.WithContext(ctx).Model(&model.OperationLog{})

	if userID, ok := filters["user_id"]; ok && userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if action, ok := filters["action"]; ok && action != "" {
		query = query.Where("action = ?", action)
	}
	if module, ok := filters["module"]; ok && module != "" {
		query = query.Where("module = ?", module)
	}
	if startTime, ok := filters["start_time"]; ok && startTime != "" {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime, ok := filters["end_time"]; ok && endTime != "" {
		query = query.Where("created_at <= ?", endTime)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	decryptOperationLogs(logs)
	return logs, total, nil
}

// GetAllKeyset 使用 keyset（cursor）分页读取操作日志。
//
// operation_logs 为 uint 自增 ID + created_at，直接复用 pagination 包的
// EncodeCursor/DecodeCursor（格式 <unix_nano>:<id>）。排序统一为
// created_at DESC, id DESC，行值比较走 (created_at, id) < (?, ?)。
// 多取 1 条用于判断是否还有下一页。
func (r *operationLogRepo) GetAllKeyset(ctx context.Context, cursor string, pageSize int, filters map[string]any) ([]*model.OperationLog, int64, string, error) {
	var logs []*model.OperationLog
	var total int64

	if pageSize <= 0 {
		pageSize = 20
	}

	query := r.db.WithContext(ctx).Model(&model.OperationLog{})

	if userID, ok := filters["user_id"]; ok && userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if action, ok := filters["action"]; ok && action != "" {
		query = query.Where("action = ?", action)
	}
	if module, ok := filters["module"]; ok && module != "" {
		query = query.Where("module = ?", module)
	}
	if startTime, ok := filters["start_time"]; ok && startTime != "" {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime, ok := filters["end_time"]; ok && endTime != "" {
		query = query.Where("created_at <= ?", endTime)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, "", err
	}

	if cursor != "" {
		ts, id, ok := _pagination.DecodeCursor(_pagination.Cursor(cursor))
		if !ok {
			return nil, 0, "", fmt.Errorf("invalid cursor: %s", cursor)
		}
		query = query.Where("(created_at, id) < (?, ?)", ts, id)
	}

	if err := query.Order("created_at DESC, id DESC").Limit(pageSize + 1).Find(&logs).Error; err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(logs) > pageSize {
		last := logs[pageSize-1]
		logs = logs[:pageSize]
		nextCursor = string(_pagination.EncodeCursor(last.CreatedAt, uint64(last.ID)))
	}

	decryptOperationLogs(logs)
	return logs, total, nextCursor, nil
}

func (r *operationLogRepo) GetByUserID(ctx context.Context, userID uint, page, pageSize int) ([]*model.OperationLog, int64, error) {
	var logs []*model.OperationLog
	var total int64

	query := r.db.WithContext(ctx).Model(&model.OperationLog{}).Where("user_id = ?", userID)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	decryptOperationLogs(logs)
	return logs, total, nil
}

func (r *operationLogRepo) DeleteOldLogs(ctx context.Context, beforeDate time.Time) error {
	return r.db.WithContext(ctx).Where("created_at < ?", beforeDate).Delete(&model.OperationLog{}).Error
}

func (r *operationLogRepo) DeleteByIDs(ctx context.Context, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.OperationLog{})
	return result.RowsAffected, result.Error
}

func (r *operationLogRepo) UpdateNewValue(ctx context.Context, id uint, newValue string) error {
	return r.db.WithContext(ctx).Model(&model.OperationLog{}).
		Where("id = ?", id).
		Update("new_value", newValue).Error
}

// decryptOperationLog 读取出口解密单条操作日志的敏感字段（OPT-SEC-04）。
// dbencrypt.Decrypt 对非 enc:v1: 前缀（存量明文）原样透传，故可无条件调用。
func decryptOperationLog(log *model.OperationLog) {
	if log == nil {
		return
	}
	log.IP = dbencrypt.Decrypt(log.IP)
	log.UserAgent = dbencrypt.Decrypt(log.UserAgent)
}

// decryptOperationLogs 批量读取出口解密。
func decryptOperationLogs(logs []*model.OperationLog) {
	for _, l := range logs {
		decryptOperationLog(l)
	}
}

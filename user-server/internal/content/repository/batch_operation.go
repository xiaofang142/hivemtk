package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	contentmodel "hivemtk-user/internal/content/model"
)

// BatchOperationRepository 批量操作历史仓储
type BatchOperationRepository struct {
	db *gorm.DB
}

// NewBatchOperationRepository 创建批量操作历史仓储实例
func NewBatchOperationRepository(db *gorm.DB) *BatchOperationRepository {
	return &BatchOperationRepository{db: db}
}

// ListHistories 分页查询批量操作历史（按 created_at DESC）
func (r *BatchOperationRepository) ListHistories(ctx context.Context, page, pageSize int) ([]*contentmodel.BatchOperationHistory, int64, error) {
	var list []*contentmodel.BatchOperationHistory
	var total int64
	if err := r.db.WithContext(ctx).
		Model(&contentmodel.BatchOperationHistory{}).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := r.db.WithContext(ctx).
		Model(&contentmodel.BatchOperationHistory{}).
		Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// GetHistoryByID 根据 ID 获取批量操作历史。
//
// 失败一律交出 (nil, err)：命中不到既可能是记录不存在，也可能是租户上下文没绑定，
// 回一个零值结构体等于让 handler 无从分辨，只能把配置级故障说成「记录不存在」。
func (r *BatchOperationRepository) GetHistoryByID(ctx context.Context, id uint) (*contentmodel.BatchOperationHistory, error) {
	var history contentmodel.BatchOperationHistory
	if err := r.db.WithContext(ctx).First(&history, id).Error; err != nil {
		return nil, err
	}
	return &history, nil
}

// ErrBatchOperationNotCancellable 操作记录不存在或状态不可取消
var ErrBatchOperationNotCancellable = errors.New("操作记录不存在或状态不可取消")

// NotCancellableError 记录存在但当前状态不允许取消。
//
// 与「记录不存在」分开表述：前端拿到 404 会以为记录被删了，实际只是已完成/已取消。
// Unwrap 回 ErrBatchOperationNotCancellable 以保持既有调用方（含单测）的 errors.Is 判定。
type NotCancellableError struct {
	Status string
}

func (e *NotCancellableError) Error() string {
	status := e.Status
	if status == "" {
		status = "未知"
	}
	return "当前状态为 " + status + "，仅待执行（pending）或执行中（running）的批量操作可取消"
}

func (e *NotCancellableError) Unwrap() error { return ErrBatchOperationNotCancellable }

// CreateHistory 写入一条批量操作历史。
//
// 历史表此前从未被写入过（四个写端点都不落库），导致前端的「操作历史」页恒空、
// 取消与详情只能返回 404。写入点放在操作完成后（旁路观测，失败只记日志）。
func (r *BatchOperationRepository) CreateHistory(ctx context.Context, h *contentmodel.BatchOperationHistory) error {
	if r == nil || r.db == nil {
		return errors.New("batch operation repository not initialized")
	}
	return r.db.WithContext(ctx).Create(h).Error
}

// CancelHistory 取消批量操作（仅 pending / running 状态可取消）
//
// 返回 nil 表示成功；记录不存在时返回同时匹配 ErrBatchOperationNotCancellable
// 与 gorm.ErrRecordNotFound 的错误；状态不允许时返回 *NotCancellableError。
func (r *BatchOperationRepository) CancelHistory(ctx context.Context, id uint) error {
	var current contentmodel.BatchOperationHistory
	if err := r.db.WithContext(ctx).Select("id", "status").First(&current, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %w", ErrBatchOperationNotCancellable, gorm.ErrRecordNotFound)
		}
		return err
	}
	if current.Status != "pending" && current.Status != "running" {
		return &NotCancellableError{Status: current.Status}
	}

	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&contentmodel.BatchOperationHistory{}).
		Where("id = ? AND status IN ?", id, []string{"pending", "running"}).
		Updates(map[string]any{
			"status":      "cancelled",
			"finished_at": now,
			"updated_at":  now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 读与写之间被别的请求改走了状态
		return &NotCancellableError{Status: "changed"}
	}
	return nil
}

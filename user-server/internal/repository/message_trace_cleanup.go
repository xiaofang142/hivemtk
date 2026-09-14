// message_trace_cleanup.go message_trace 正文 TTL/PII 清理仓储
package repository

import (
	"context"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// MessageTraceCleanupRepo message_trace 清理仓储
type MessageTraceCleanupRepo struct {
	db *gorm.DB
}

// NewMessageTraceCleanupRepo 构造
// NewMessageTraceCleanupRepo 构造（无参，内部取全局 DB；对齐 customer_session.go 先例）
func NewMessageTraceCleanupRepo() *MessageTraceCleanupRepo {
	return NewMessageTraceCleanupRepoWithDB(_db.GetDB())
}

// NewMessageTraceCleanupRepoWithDB 显式注入 DB（测试/装配用）
func NewMessageTraceCleanupRepoWithDB(db *gorm.DB) *MessageTraceCleanupRepo {
	return &MessageTraceCleanupRepo{db: db}
}

// TraceMaskRow PII 打码批次行
type TraceMaskRow struct {
	ID     uint
	Input  string
	Output string
}

// ListForPIIMask 取 PII 打码窗口内的一批行（30~90 天区间）
func (r *MessageTraceCleanupRepo) ListForPIIMask(ctx context.Context, upper, lower time.Time, lastID uint, limit int) ([]TraceMaskRow, error) {
	var rows []TraceMaskRow
	err := r.db.WithContext(ctx).
		Table("message_traces").
		Where("created_at <= ? AND created_at > ? AND id > ?", upper, lower, lastID).
		Order("id ASC").Limit(limit).
		Select("id", "input", "output").
		Scan(&rows).Error
	return rows, err
}

// UpdateBody 更新单行 input/output
func (r *MessageTraceCleanupRepo) UpdateBody(ctx context.Context, id uint, newIn, newOut string) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.MessageTrace{}).
		Where("id = ?", id).
		Updates(map[string]any{"input": newIn, "output": newOut})
	return res.RowsAffected, res.Error
}

// NullExpiredBodies 将 90 天前的正文置 NULL（结构字段与元数据保留）
func (r *MessageTraceCleanupRepo) NullExpiredBodies(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.MessageTrace{}).
		Where("created_at < ?", cutoff).
		Where("(input IS NOT NULL OR output IS NOT NULL)").
		UpdateColumns(map[string]any{"input": gorm.Expr("NULL"), "output": gorm.Expr("NULL")})
	return res.RowsAffected, res.Error
}

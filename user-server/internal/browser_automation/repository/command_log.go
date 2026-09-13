package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserCommandLogRepository append-only 命令-事件日志仓储。
// 不可变性契约：永不 UPDATE 单行、执行路径无 Delete；PruneBefore 是治理级时间窗整体裁剪
// （G19：无界增长不可接受），与"篡改审计记录"是两回事——保留期内审计/重放事实完整。
type BrowserCommandLogRepository interface {
	Append(ctx context.Context, entry *model.BrowserCommandLog) error
	ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserCommandLog, error)
	PruneBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type browserCommandLogRepo struct {
	db *gorm.DB
}

func NewBrowserCommandLogRepository() BrowserCommandLogRepository {
	return &browserCommandLogRepo{db: _db.GetDB()}
}

func NewBrowserCommandLogRepositoryWithDB(db *gorm.DB) BrowserCommandLogRepository {
	return &browserCommandLogRepo{db: db}
}

func (r *browserCommandLogRepo) Append(ctx context.Context, entry *model.BrowserCommandLog) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *browserCommandLogRepo) ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserCommandLog, error) {
	var list []*model.BrowserCommandLog
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("seq ASC").Find(&list).Error
	return list, err
}

// PruneBefore G19：删除 cutoff 之前的全部命令日志（分批 5000 防长事务）。
func (r *browserCommandLogRepo) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res := r.db.WithContext(ctx).
			Where("created_at < ? AND id IN (?)", cutoff,
				r.db.Model(&model.BrowserCommandLog{}).Where("created_at < ?", cutoff).Order("id ASC").Limit(5000).Select("id")).
			Delete(&model.BrowserCommandLog{})
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected == 0 {
			return total, nil
		}
	}
}

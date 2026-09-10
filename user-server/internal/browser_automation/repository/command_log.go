package repository

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserCommandLogRepository append-only 命令-事件日志仓储（只增查，无改删）
type BrowserCommandLogRepository interface {
	Append(ctx context.Context, entry *model.BrowserCommandLog) error
	ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserCommandLog, error)
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

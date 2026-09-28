package repository

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserAuditDigestRepository 裁剪摘要的**读侧**（/ A6）。
//
// 写侧不长在这里：摘要必须与它描述的那批删除同事务落库，所以由
// BrowserCommandLogRepository.PruneBefore 内部完成（见 command_log.go）。
// 把写侧拆成独立接口就等于邀请调用方「先删、后补记」——那正是本批要禁掉的动作顺序。
//
// 本表永不裁剪，故读侧只有「按 session 取全部」：一条会话的摘要条数就是它被裁过的批数
// （常态是个位数），不需要分页。
type BrowserAuditDigestRepository interface {
	ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserAuditDigest, error)
}

type browserAuditDigestRepo struct {
	db *gorm.DB
}

func NewBrowserAuditDigestRepository() BrowserAuditDigestRepository {
	return &browserAuditDigestRepo{db: _db.GetDB()}
}

func NewBrowserAuditDigestRepositoryWithDB(db *gorm.DB) BrowserAuditDigestRepository {
	return &browserAuditDigestRepo{db: db}
}

func (r *browserAuditDigestRepo) ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserAuditDigest, error) {
	var list []*model.BrowserAuditDigest
	// ordinal 是链序（不是入库时间序）：拿到手就该是能直接首尾相接核对的那个数。
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("ordinal ASC").Find(&list).Error
	return list, err
}

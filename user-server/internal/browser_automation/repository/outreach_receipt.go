package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"
)

// BrowserOutreachReceiptRepository 触达回执读写（Chunk5 验收交付物）。
//
// 与 BrowserOutreachDedupeRepository 的三处形状差异（写侧都必须遵守，见 RecordOutreachReceipt）：
// 去重表是闸门（有唯一键、重复插入是「已经发过了」），回执表是交付物（重复写入是「多留一份证据」）。
type BrowserOutreachReceiptRepository interface {
	// RecordOutreachReceipt 落一条回执。**刻意不做 OnConflict DoNothing**：
	// 去重表那样做是因为重复行等于重复打扰，这里重复行等于重复交付——吞掉会让
	// 「内容已发出但列表里没有回执」与「这步根本没执行过」无法区分。
	RecordOutreachReceipt(ctx context.Context, rec *model.BrowserOutreachReceipt) error
	// ListByTaskID 任务级回执行（新到旧）。userID=0 不做归属过滤，仅供后台/测试。
	ListByTaskID(ctx context.Context, taskID uint, userID uint, limit int) ([]*model.BrowserOutreachReceipt, error)
	// ListBySessionID 会话级回执行（新到旧）。
	ListBySessionID(ctx context.Context, sessionID uint, userID uint) ([]*model.BrowserOutreachReceipt, error)
}

type browserOutreachReceiptRepository struct {
	db *gorm.DB
}

// NewBrowserOutreachReceiptRepository 默认使用全局 DB。
func NewBrowserOutreachReceiptRepository() BrowserOutreachReceiptRepository {
	return &browserOutreachReceiptRepository{db: _db.GetDB()}
}

// NewBrowserOutreachReceiptRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）。
func NewBrowserOutreachReceiptRepositoryWithDB(db *gorm.DB) BrowserOutreachReceiptRepository {
	return &browserOutreachReceiptRepository{db: db}
}

func (r *browserOutreachReceiptRepository) RecordOutreachReceipt(ctx context.Context, rec *model.BrowserOutreachReceipt) error {
	if r.db == nil || rec == nil {
		return errors.New("回执仓储未装配")
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// receiptQuery 归属过滤：回执行自己没有 user_id 列，归属经 task_id / session_id 落到
// 所属任务与会话上再判——否则一个知道 task_id 的用户就能读别人的触达内容。
func (r *browserOutreachReceiptRepository) ListByTaskID(ctx context.Context, taskID uint, userID uint, limit int) ([]*model.BrowserOutreachReceipt, error) {
	if r.db == nil {
		return nil, errors.New("回执仓储未装配")
	}
	q := r.db.WithContext(ctx).Model(&model.BrowserOutreachReceipt{}).Where("task_id = ?", taskID)
	if userID != 0 {
		q = q.Where("task_id IN (?)",
			r.db.Model(&model.BrowserTask{}).Select("id").Where("id = ? AND user_id = ?", taskID, userID))
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []*model.BrowserOutreachReceipt
	return out, q.Order("created_at desc, id desc").Limit(limit).Find(&out).Error
}

func (r *browserOutreachReceiptRepository) ListBySessionID(ctx context.Context, sessionID uint, userID uint) ([]*model.BrowserOutreachReceipt, error) {
	if r.db == nil {
		return nil, errors.New("回执仓储未装配")
	}
	q := r.db.WithContext(ctx).Model(&model.BrowserOutreachReceipt{}).Where("session_id = ?", sessionID)
	if userID != 0 {
		q = q.Where("session_id IN (?)",
			r.db.Model(&model.BrowserSession{}).Select("id").Where("id = ? AND user_id = ?", sessionID, userID))
	}
	var out []*model.BrowserOutreachReceipt
	return out, q.Order("created_at asc, id asc").Find(&out).Error
}

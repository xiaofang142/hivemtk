package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// FeedbackRecordRepository 反馈记录仓储。FeedbackLearner 在 RecordFeedback
// 时通过本仓储落库，失败仅记 warn 不影响内存缓存与业务链路。
type FeedbackRecordRepository struct {
	db *gorm.DB
}

func NewFeedbackRecordRepository(db *gorm.DB) *FeedbackRecordRepository {
	return &FeedbackRecordRepository{db: db}
}

// NewFeedbackRecordRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
//
// AV-04c 批次6：service/inbox_ingress.go 装配原先自己取全局句柄再传进来
// （service 直取 DB 句柄的宽口径），收口为包级构造——调用方以 repository.DBReady()
// 探针保持「无库即留 nil」的原装配语义（单测密封验证依赖）。
func NewFeedbackRecordRepositoryDefault() *FeedbackRecordRepository {
	return NewFeedbackRecordRepository(GetDB())
}

func NewFeedbackRecordRepositoryWithTx(tx *gorm.DB) *FeedbackRecordRepository {
	return &FeedbackRecordRepository{db: tx}
}

// Create 写入一条反馈记录（失败仅由调用方记 warn，不阻断业务链路）
func (r *FeedbackRecordRepository) Create(ctx context.Context, orm *model.FeedbackRecordORM) error {
	if r == nil || r.db == nil {
		return errors.New("feedback record repository not initialized")
	}
	if orm == nil {
		return errors.New("feedback record orm is nil")
	}
	return r.db.WithContext(ctx).Create(orm).Error
}

// UpdateCustomerAcceptBySession 客户消息抵达时，按 session_id 标记最近一条
// feedback_record 的 customer_accept=true：创建时 CustomerAccept=false（生成时尚
// 不知客户是否接受），客户下一条消息进来 = 收到 AI 回复并继续对话 = 接受了。
// 幂等，重复调用无副作用。
func (r *FeedbackRecordRepository) UpdateCustomerAcceptBySession(ctx context.Context, sessionID string) (int64, error) {
	if r == nil || r.db == nil || sessionID == "" {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Model(&model.FeedbackRecordORM{}).
		Where("session_id = ?", sessionID).
		Where("customer_accept = ?", false).
		Order("id DESC").
		Limit(1).
		Update("customer_accept", true)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

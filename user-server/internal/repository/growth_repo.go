package repository

import (
	"context"
	"errors"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// WebhookSubscriptionRepository 出站 Webhook 订阅收口
type WebhookSubscriptionRepository struct {
	db *gorm.DB
}

func NewWebhookSubscriptionRepository() *WebhookSubscriptionRepository {
	return NewWebhookSubscriptionRepositoryWithDB(_db.GetDB())
}

func NewWebhookSubscriptionRepositoryWithDB(db *gorm.DB) *WebhookSubscriptionRepository {
	return &WebhookSubscriptionRepository{db: db}
}

// Create 创建订阅
func (r *WebhookSubscriptionRepository) Create(ctx context.Context, sub *model.WebhookSubscription) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(sub).Error
}

// List 全部订阅（id 升序）
func (r *WebhookSubscriptionRepository) List(ctx context.Context) ([]*model.WebhookSubscription, error) {
	if r.db == nil {
		return nil, nil
	}
	var list []*model.WebhookSubscription
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// Delete 删除订阅
func (r *WebhookSubscriptionRepository) Delete(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Delete(&model.WebhookSubscription{}, id).Error
}

// ListEnabledByEvent 取匹配事件的启用订阅（上限 100）
func (r *WebhookSubscriptionRepository) ListEnabledByEvent(ctx context.Context, event string) ([]model.WebhookSubscription, error) {
	if r.db == nil {
		return nil, nil
	}
	var subs []model.WebhookSubscription
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND (events LIKE ? OR events LIKE ?)", true, "%"+event+"%", "%all%").
		Limit(100).Find(&subs).Error
	return subs, err
}

// FindEnabledByID 返回启用订阅；不存在/禁用返回 nil，DB 错误原样上抛。
func (r *WebhookSubscriptionRepository) FindEnabledByID(ctx context.Context, id uint) (*model.WebhookSubscription, error) {
	if r.db == nil {
		return nil, nil
	}
	var sub model.WebhookSubscription
	err := r.db.WithContext(ctx).Where("id = ? AND enabled = ?", id, true).First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

type SavedViewRepository struct {
	db *gorm.DB
}

func NewSavedViewRepository() *SavedViewRepository {
	return &SavedViewRepository{db: _db.GetDB()}
}

func NewSavedViewRepositoryWithDB(db *gorm.DB) *SavedViewRepository {
	return &SavedViewRepository{db: db}
}

// DeleteByName 同名删除（同名覆盖语义）
func (r *SavedViewRepository) DeleteByName(ctx context.Context, userID uint, name, route string) {
	if r.db == nil {
		return
	}
	_ = r.db.WithContext(ctx).
		Where("user_id = ? AND name = ? AND route = ?", userID, name, route).
		Delete(&model.SavedView{}).Error
}

// Create 创建视图
func (r *SavedViewRepository) Create(ctx context.Context, v *model.SavedView) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(v).Error
}

// ListByUserAndRoute 按用户+路由列视图
func (r *SavedViewRepository) ListByUserAndRoute(ctx context.Context, userID uint, route string) ([]*model.SavedView, error) {
	if r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if route != "" {
		q = q.Where("route = ?", route)
	}
	var list []*model.SavedView
	err := q.Order("id ASC").Limit(100).Find(&list).Error
	return list, err
}

// DeleteOwned 删除本人视图，返回是否删除
func (r *SavedViewRepository) DeleteOwned(ctx context.Context, id, userID uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.SavedView{})
	return res.RowsAffected > 0, res.Error
}

type ReportSubscriptionRepository struct {
	db *gorm.DB
}

func NewReportSubscriptionRepository() *ReportSubscriptionRepository {
	return &ReportSubscriptionRepository{db: _db.GetDB()}
}

func NewReportSubscriptionRepositoryWithDB(db *gorm.DB) *ReportSubscriptionRepository {
	return &ReportSubscriptionRepository{db: db}
}

// DeleteByEmail 按邮箱删除（重订阅语义）
func (r *ReportSubscriptionRepository) DeleteByEmail(ctx context.Context, email string) {
	if r.db == nil {
		return
	}
	_ = r.db.WithContext(ctx).Where("email = ?", email).Delete(&model.ReportSubscription{}).Error
}

// Create 创建订阅
func (r *ReportSubscriptionRepository) Create(ctx context.Context, sub *model.ReportSubscription) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(sub).Error
}

// List 全部订阅
func (r *ReportSubscriptionRepository) List(ctx context.Context) ([]*model.ReportSubscription, error) {
	if r.db == nil {
		return nil, nil
	}
	var list []*model.ReportSubscription
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// Delete 删除订阅
func (r *ReportSubscriptionRepository) Delete(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Delete(&model.ReportSubscription{}, id).Error
}

// ListEnabled 全部启用订阅
func (r *ReportSubscriptionRepository) ListEnabled(ctx context.Context) ([]model.ReportSubscription, error) {
	if r.db == nil {
		return nil, nil
	}
	var subs []model.ReportSubscription
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&subs).Error
	return subs, err
}

// MarkSent 更新最后发送时间
func (r *ReportSubscriptionRepository) MarkSent(ctx context.Context, id uint, at time.Time) {
	if r.db == nil {
		return
	}
	_ = r.db.WithContext(ctx).Model(&model.ReportSubscription{}).Where("id = ?", id).Update("last_sent", at).Error
}

// DailyReportSummaryRow 每日报表汇总行
type DailyReportSummaryRow struct {
	Metric string `gorm:"column:metric"`
	Value  int64  `gorm:"column:value"`
}

// DailyReportSummary 每日新会话/新消息/新客户/发送消息汇总
func (r *ReportSubscriptionRepository) DailyReportSummary(ctx context.Context, day string) ([]DailyReportSummaryRow, error) {
	if r.db == nil {
		return nil, nil
	}
	countOf := func(table, where string) int64 {
		var n int64
		_ = r.db.WithContext(ctx).Table(table).Where(where, day).Count(&n).Error
		return n
	}
	rows := []DailyReportSummaryRow{
		{Metric: "新会话", Value: countOf("customer_sessions", "created_at::date = ?")},
		{Metric: "新消息", Value: countOf("session_messages", "created_at::date = ?")},
		{Metric: "新客户", Value: countOf("customers", "created_at::date = ?")},
		{Metric: "发送消息", Value: countOf("message_hub", "direction='outbound' AND sent_at::date = ?")},
	}
	return rows, nil
}

// TranscriptMessageRow 转录消息行
type TranscriptMessageRow struct {
	SenderType string    `gorm:"column:sender_type"`
	SenderName string    `gorm:"column:sender_name"`
	Content    string    `gorm:"column:content"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// ListSessionTranscriptMessages 会话对外消息（非内部备注，限 2000 条）
func (r *ReportSubscriptionRepository) ListSessionTranscriptMessages(ctx context.Context, sessionID string) ([]TranscriptMessageRow, error) {
	if r.db == nil {
		return nil, nil
	}
	var msgs []TranscriptMessageRow
	err := r.db.WithContext(ctx).
		Table("session_messages").
		Select("sender_type, COALESCE(sender_name,'') AS sender_name, content, created_at").
		Where("session_id = ? AND is_internal = ?", sessionID, false).
		Order("created_at ASC").Limit(2000).Scan(&msgs).Error
	return msgs, err
}

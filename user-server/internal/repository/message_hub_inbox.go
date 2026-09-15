package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

type MessageHubRepository struct {
	db *gorm.DB
}

func NewMessageHubRepository() *MessageHubRepository {
	return &MessageHubRepository{db: _db.GetDB()}
}

func NewMessageHubRepositoryWithDB(db *gorm.DB) *MessageHubRepository {
	return &MessageHubRepository{db: db}
}

func (r *MessageHubRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func SetMessageHubRepoDB(r *MessageHubRepository, db *gorm.DB) {
	r.SetDB(context.Background(), db)
}

func (r *MessageHubRepository) Create(ctx context.Context, hub *model.MessageHub) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(hub).Error
}

func (r *MessageHubRepository) ListRecentInboundBySender(ctx context.Context, platform, senderID string, limit int) ([]model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var hubs []model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND sender_id = ? AND direction = 'inbound'", platform, senderID).
		Order("sent_at DESC").
		Limit(limit).
		Find(&hubs).Error
	if err != nil {
		return nil, err
	}
	return hubs, nil
}

func (r *MessageHubRepository) GetByID(ctx context.Context, id uint) (*model.MessageHub, error) {
	var hub model.MessageHub
	err := r.db.First(&hub, id).Error
	return &hub, err
}

func (r *MessageHubRepository) GetByMsgID(ctx context.Context, msgID string) (*model.MessageHub, error) {
	var hub model.MessageHub
	err := r.db.Where("msg_id = ?", msgID).First(&hub).Error
	return &hub, err
}

func (r *MessageHubRepository) GetByContentHash(ctx context.Context, canonicalHash string) (*model.MessageHub, error) {
	if canonicalHash == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var hub model.MessageHub
	err := r.db.Where("msg_id = ?", canonicalHash).First(&hub).Error
	return &hub, err
}

func (r *MessageHubRepository) GetByPlatformContent(ctx context.Context, platform, content string) (*model.MessageHub, error) {
	if platform == "" || content == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var hub model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND direction = 'outbound' AND md5(content) = md5(?)", platform, content).
		Order("sent_at DESC").
		First(&hub).Error
	return &hub, err
}

func (r *MessageHubRepository) GetByPlatformContentNormalized(ctx context.Context, platform, content string) (*model.MessageHub, error) {
	if platform == "" || content == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var hub model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND direction = 'outbound' AND md5(regexp_replace(content, '\\s+', '', 'g')) = md5(regexp_replace(?, '\\s+', '', 'g'))", platform, content).
		Order("sent_at DESC").
		First(&hub).Error
	return &hub, err
}

func (r *MessageHubRepository) ListByConversation(ctx context.Context, conversationID string, page, pageSize int) ([]*model.MessageHub, int64, error) {
	var items []*model.MessageHub
	var total int64
	query := r.db.Model(&model.MessageHub{}).Where("conversation_id = ?", conversationID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	err := query.Order("sent_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

func (r *MessageHubRepository) Update(ctx context.Context, hub *model.MessageHub) error {
	return r.db.Save(hub).Error
}

func (r *MessageHubRepository) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return nil
	}
	// message_hub 是消息流/出站队列表，claim SQL 按 status 取行不感知软删，
	// 软删行可能被重新领取发送，故保持物理删除
	return r.db.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.MessageHub{}).Error
}

func (r *MessageHubRepository) GetByPlatformAccountMsgID(ctx context.Context, platform, accountID, msgID string) (*model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var existing model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND msg_id = ?", platform, accountID, msgID).
		First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *MessageHubRepository) GetByDedupHash(ctx context.Context, platform, dedupHash string) (*model.MessageHub, error) {
	if r == nil || r.db == nil || dedupHash == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var existing model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND dedup_hash = ?", platform, dedupHash).
		Order("id DESC").
		First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

type HubListQuery struct {
	Platform       string
	Status         string
	AccountID      string
	ConversationID string
	SenderID       string
	Direction      string
	MsgType        string
	Keyword        string
	IsRead         *bool
	IsGroup        *bool
	StartTime      *time.Time
	EndTime        *time.Time
	Page           int
	PageSize       int
	OrderBy        string
}

func (r *MessageHubRepository) ListByHubQuery(ctx context.Context, q HubListQuery) ([]*model.MessageHub, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, nil
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 200 {
		q.PageSize = 20
	}

	tx := r.db.WithContext(ctx).Model(&model.MessageHub{})
	if q.Platform != "" {
		tx = tx.Where("platform = ?", q.Platform)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.AccountID != "" {
		tx = tx.Where("account_id = ?", q.AccountID)
	}
	if q.ConversationID != "" {
		tx = tx.Where("conversation_id = ?", q.ConversationID)
	}
	if q.SenderID != "" {
		tx = tx.Where("sender_id = ?", q.SenderID)
	}
	if q.Direction != "" {
		tx = tx.Where("direction = ?", q.Direction)
	}
	if q.MsgType != "" {
		tx = tx.Where("msg_type = ?", q.MsgType)
	}
	if q.Keyword != "" {
		tx = tx.Where("content LIKE ?", "%"+q.Keyword+"%")
	}
	if q.IsRead != nil {
		tx = tx.Where("is_read = ?", *q.IsRead)
	}
	if q.IsGroup != nil {
		tx = tx.Where("is_group = ?", *q.IsGroup)
	}
	if q.StartTime != nil {
		tx = tx.Where("sent_at >= ?", *q.StartTime)
	}
	if q.EndTime != nil {
		tx = tx.Where("sent_at <= ?", *q.EndTime)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderBy := "sent_at DESC"
	switch q.OrderBy {
	case "sent_at ASC", "sent_at DESC", "created_at ASC", "created_at DESC", "id ASC", "id DESC":
		orderBy = q.OrderBy
	}

	var rows []*model.MessageHub
	if err := tx.Order(orderBy).
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *MessageHubRepository) GetLastByConversation(ctx context.Context, conversationID string) (*model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if conversationID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var msg model.MessageHub
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("sent_at DESC").First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

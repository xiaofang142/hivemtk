package repository

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

func (r *InboxConversationRepository) DeletePollutedInboxRows(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("conversation_id LIKE ? OR customer_id LIKE ?", "conv:% %", "conv:% %").
		Delete(&model.InboxConversation{})
	return res.RowsAffected, res.Error
}

func (r *InboxConversationRepository) DeleteOrphanConvInboxRows(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("conversation_id LIKE ? AND NOT EXISTS (SELECT 1 FROM message_hub m WHERE m.conversation_id = inbox_conversations.conversation_id)",
			"conv:%").
		Delete(&model.InboxConversation{})
	return res.RowsAffected, res.Error
}

type InboxConversationRepository struct {
	db *gorm.DB
}

func NewInboxConversationRepository() *InboxConversationRepository {
	return &InboxConversationRepository{db: _db.GetDB()}
}

func NewInboxConversationRepositoryWithDB(db *gorm.DB) *InboxConversationRepository {
	return &InboxConversationRepository{db: db}
}

// SetInboxConversationRepoDB 工具函数（service 层使用）
func SetInboxConversationRepoDB(r *InboxConversationRepository, db *gorm.DB) {
	r.SetDB(context.Background(), db)
}

// InboxConversationQuery 会话列表查询条件（与 service.InboxQuery 字段对齐）
type InboxConversationQuery struct {
	Platform    string
	AccountID   string
	CustomerID  string
	Keyword     string
	Status      string
	AssignedTo  string
	AssignedSOP uint
	Pinned      *bool
	Starred     *bool
	Muted       *bool
	Page        int
	PageSize    int
	OrderBy     string
}

// SetDB 注入 db（用于测试）
//
// 五层架构 §三.5 + §七：仓库方法必须首参为 ctx context.Context。
func (r *InboxConversationRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// FindByPlatformAccountCustomer 按平台/账号/客户查找会话
func (r *InboxConversationRepository) FindByPlatformAccountCustomer(ctx context.Context, platform, accountID, customerID string) (*model.InboxConversation, error) {
	var conv model.InboxConversation
	err := r.db.Where("platform = ? AND account_id = ? AND customer_id = ?",
		platform, accountID, customerID).First(&conv).Error
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

// CreateOrUpdateByChannel 创建会话；若并发下撞唯一键 uk_inbox_conv_channel (platform,account_id,customer_id)，
// 则退回查找并更新 last_message，保证 webhook 重发不会产生重复会话。
func (r *InboxConversationRepository) CreateOrUpdateByChannel(ctx context.Context, conv *model.InboxConversation) error {
	if r == nil || r.db == nil {
		return nil
	}
	err := r.db.Create(conv).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) && !strings.Contains(strings.ToLower(err.Error()), "duplicate") &&
		!strings.Contains(err.Error(), "SQLSTATE 23505") {
		return err
	}
	existing, findErr := r.FindByPlatformAccountCustomer(ctx, conv.Platform, conv.AccountID, conv.CustomerID)
	if findErr != nil || existing == nil {
		return err
	}
	return r.UpdateLastMessage(ctx, existing.ID, conv.LastMessagePreview, func() time.Time {
		if conv.LastMessageAt != nil {
			return *conv.LastMessageAt
		}
		return time.Now()
	}(), 1)
}

// Create 创建会话
func (r *InboxConversationRepository) Create(ctx context.Context, conv *model.InboxConversation) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(conv).Error
}

// UpdateLastMessage 更新最后消息字段（含 unread_count 自增）
func (r *InboxConversationRepository) UpdateLastMessage(ctx context.Context, id uint, lastMessage string, lastMessageAt time.Time, unreadInc int) error {

	const previewMaxLen = 500
	if len(lastMessage) > previewMaxLen {
		lastMessage = lastMessage[:previewMaxLen]
	}
	return r.db.Model(&model.InboxConversation{}).Where("id = ?", id).
		Updates(map[string]any{
			"last_message_preview": lastMessage,
			"last_message_at":      lastMessageAt,
			"unread_count":         gorm.Expr("unread_count + ?", unreadInc),
		}).Error
}

// GetByID 按 ID 获取会话
func (r *InboxConversationRepository) GetByID(ctx context.Context, id uint) (*model.InboxConversation, error) {
	var conv model.InboxConversation
	err := r.db.First(&conv, id).Error
	return &conv, err
}

// Update 更新会话
func (r *InboxConversationRepository) Update(ctx context.Context, conv *model.InboxConversation) error {
	return r.db.Save(conv).Error
}

// ListByAccount 按账号列出会话
func (r *InboxConversationRepository) ListByAccount(ctx context.Context, platform, accountID string, page, pageSize int) ([]*model.InboxConversation, int64, error) {
	var items []*model.InboxConversation
	var total int64
	query := r.db.Model(&model.InboxConversation{}).
		Where("platform = ? AND account_id = ?", platform, accountID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	err := query.Order("pinned DESC, last_message_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

// UpsertFromMessageInput upsert 入参（与 service 层字段一一对应）
type UpsertFromMessageInput struct {
	Platform           string
	AccountID          string
	CustomerID         string
	CustomerName       string
	ConversationID     string
	LastMessageID      uint
	LastMessagePreview string
	LastMessageAt      time.Time
	LastMessageFrom    string
}

// UpsertFromMessage 根据消息 upsert 会话（事务封装在 repo 内）
//
// 行为保持与原 service 层 upsertInternal 一致：
//   - 客户首条消息：unread_count=1，status=unread
//   - 客户后续消息：unread_count+1，closed 状态自动转 unread，assigned+无 assigned_to 也转 unread
//   - 非客户消息：不修改 unread_count
func (r *InboxConversationRepository) UpsertFromMessage(ctx context.Context, in UpsertFromMessageInput) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.UpsertFromMessageTx(ctx, tx, in)
	})
}

// UpsertFromMessageTx 与 UpsertFromMessage 等价，但接受外部 tx（用于跨表事务）。
// 调用方负责事务边界（如 MessageHubRepository.CreateWithInboxTx）。
// tx 为 nil 时返回 nil（防御性短路，与 UpsertFromMessage 行为一致）。
func (r *InboxConversationRepository) UpsertFromMessageTx(ctx context.Context, tx *gorm.DB, in UpsertFromMessageInput) error {
	if r == nil || tx == nil {
		return nil
	}
	tx = tx.WithContext(ctx)

	in.LastMessagePreview = sanitizeUTF8(in.LastMessagePreview)
	in.CustomerName = sanitizeUTF8(in.CustomerName)
	var conv model.InboxConversation
	err := tx.Where("platform = ? AND account_id = ? AND customer_id = ?",
		in.Platform, in.AccountID, in.CustomerID).
		First(&conv).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		conv = model.InboxConversation{
			Platform:           in.Platform,
			AccountID:          in.AccountID,
			CustomerID:         in.CustomerID,
			CustomerName:       in.CustomerName,
			ConversationID:     in.ConversationID,
			Status:             "unread",
			UnreadCount:        0,
			TotalCount:         1,
			LastMessageID:      in.LastMessageID,
			LastMessagePreview: in.LastMessagePreview,
			LastMessageAt:      &in.LastMessageAt,
			LastMessageFrom:    in.LastMessageFrom,
		}
		if in.LastMessageFrom == "customer" {
			conv.UnreadCount = 1
		} else {

			conv.Status = "open"
		}
		return tx.Create(&conv).Error
	}
	if err != nil {
		return err
	}

	updates := map[string]any{
		"last_message_id":      in.LastMessageID,
		"last_message_preview": in.LastMessagePreview,
		"last_message_at":      in.LastMessageAt,
		"last_message_from":    in.LastMessageFrom,
		"total_count":          conv.TotalCount + 1,
		"customer_name":        firstNonEmpty(in.CustomerName, conv.CustomerName),
		"conversation_id":      firstNonEmpty(in.ConversationID, conv.ConversationID),
	}
	if in.LastMessageFrom == "customer" {
		updates["unread_count"] = conv.UnreadCount + 1
		if conv.Status == "closed" {
			updates["status"] = "unread"
			updates["closed_at"] = nil
		} else if conv.Status == "assigned" && conv.AssignedTo == "" {
			updates["status"] = "unread"
		}
	} else {

		updates["unread_count"] = 0
		if conv.Status == "unread" {
			updates["status"] = "open"
		}
	}
	return tx.Model(&model.InboxConversation{}).
		Where("id = ?", conv.ID).
		Updates(updates).Error
}

// DeleteOrphanInboxByConversation 删除同一 (platform, account_id, conversation_id)
// 下、customer_id 与 keepCustomerID 不一致的孤儿收件箱行。群聊 sender_id 被时间戳
// 污染后，同一会话会生成多条 customer_id 各异的碎片行，合并为按 conversation_id
// 归属的单行后，其余碎片需清理以免在收件箱 UI 重复出现。
func (r *InboxConversationRepository) DeleteOrphanInboxByConversation(ctx context.Context, platform, accountID, conversationID, keepCustomerID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND conversation_id = ? AND customer_id <> ?",
			platform, accountID, conversationID, keepCustomerID).
		Delete(&model.InboxConversation{})
	return res.RowsAffected, res.Error
}

func sanitizeUTF8(s string) string {
	if s == "" {
		return s
	}
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// MarkRead 标记会话已读（重置未读计数 + 状态置为 open）
func (r *InboxConversationRepository) MarkRead(ctx context.Context, conversationID uint) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]any{
			"unread_count": 0,
			"status":       "open",
		}).Error
}

// UpdateField 更新单个字段（用于 Pin/Star/Mute/Tag 等开关型操作）
func (r *InboxConversationRepository) UpdateField(ctx context.Context, conversationID uint, field string, value any) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("id = ?", conversationID).
		Update(field, value).Error
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

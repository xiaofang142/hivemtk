package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

// SessionMessageRepository 会话消息仓库
type SessionMessageRepository struct {
	db *gorm.DB
}

// NewSessionMessageRepository 创建会话消息仓库实例
func NewSessionMessageRepository() *SessionMessageRepository {
	return &SessionMessageRepository{
		db: _db.GetDB(),
	}
}

// NewSessionMessageRepositoryWithDB 创建指定数据库连接的 SessionMessageRepository 实例（用于测试）
func NewSessionMessageRepositoryWithDB(db *gorm.DB) *SessionMessageRepository {
	return &SessionMessageRepository{
		db: db,
	}
}

// Create 创建消息
func (r *SessionMessageRepository) Create(ctx context.Context, message *model.SessionMessage) error {
	return r.db.Create(message).Error
}

// FindRecentDuplicate 查找最近 N 秒内同 (session, content, sender_type, sender_id) 的消息
// 用于：visitor 端 + orchestrator 双保存的去重（修复 chat 双发 bug）
// 返回找到的最近一条消息（如果存在），否则返回 nil
func (r *SessionMessageRepository) FindRecentDuplicate(ctx context.Context, sessionID, senderType, senderID, content string, window time.Duration) (*model.SessionMessage, error) {
	var existing model.SessionMessage
	threshold := time.Now().Add(-window)
	err := r.db.Where("session_id = ? AND sender_type = ? AND sender_id = ? AND content = ? AND created_at >= ?",
		sessionID, senderType, senderID, content, threshold).
		Order("id DESC").
		First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

// GetBySessionID 获取会话的消息列表
func (r *SessionMessageRepository) GetBySessionID(ctx context.Context, sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error) {
	var messages []*model.SessionMessage
	var total int64

	query := r.db.Model(&model.SessionMessage{}).Where("session_id = ?", sessionID)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Order("created_at ASC").Offset(offset).Limit(pageSize).Find(&messages).Error
	return messages, total, err
}

// ListBySessionIDsBatch 批量按多个 session_id 取消息，返回按 session_id 分组的 map（CC- N+1 优化）
//
// 单次 SQL 拉取所有 session 的消息，service 层按 session_id 分桶，
// 避免「for session → GetBySessionID」造成的 N+1 查询。
//   - sessionIDs: 待查询的 session id 列表，空时返回 nil
//   - perSessionLimit: 每个 session 最多取多少条（<=0 时默认 100）；为防止单 session 拉爆整体
//
// 返回值：map[sessionID][]*SessionMessage，key 一定是入参中的 sessionID 之一。
func (r *SessionMessageRepository) ListBySessionIDsBatch(ctx context.Context, sessionIDs []string, perSessionLimit int) (map[string][]*model.SessionMessage, error) {
	result := make(map[string][]*model.SessionMessage, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return result, nil
	}
	if perSessionLimit <= 0 {
		perSessionLimit = 100
	}
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var rows []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id IN ?", unique).
		Order("session_id ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		bucket := result[m.SessionID]
		if len(bucket) >= perSessionLimit {
			continue
		}
		result[m.SessionID] = append(bucket, m)
	}
	return result, nil
}

// MarkAsRead 标记消息已读
func (r *SessionMessageRepository) MarkAsRead(ctx context.Context, sessionID string, beforeTime time.Time) error {
	now := time.Now()
	return r.db.Model(&model.SessionMessage{}).
		Where("session_id = ? AND is_read = ? AND created_at <= ?", sessionID, false, beforeTime).
		Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		}).Error
}

// GetUnreadCount 获取未读消息数
func (r *SessionMessageRepository) GetUnreadCount(ctx context.Context, sessionID string, senderType string) int64 {
	var count int64
	r.db.Model(&model.SessionMessage{}).
		Where("session_id = ? AND sender_type = ? AND is_read = ?", sessionID, senderType, false).
		Count(&count)
	return count
}

// HasTable 检查 session_messages 表是否存在（用于统一收件箱合并消息流时的兼容判定）
func (r *SessionMessageRepository) HasTable(ctx context.Context) bool {
	if r == nil || r.db == nil {
		return false
	}
	return r.db.WithContext(ctx).Migrator().HasTable(&model.SessionMessage{})
}

// ListAllBySessionID 按 session_id 拉取全部消息（不分页，用于统一收件箱会话视图合并展示）
func (r *SessionMessageRepository) ListAllBySessionID(ctx context.Context, sessionID string) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var rows []*model.SessionMessage
	if err := r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id = ?", sessionID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *SessionMessageRepository) ListTranscriptBySession(ctx context.Context, sessionID string, limit int) ([]*model.SessionMessage, error) {
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	var rows []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND is_internal = ?", sessionID, false).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ListOfflineBySessionID 拉取访客离线期间未投递的坐席/AI 回复消息
//
// 离线消息定义（与 service.GetOfflineMessages 一致）：
//   - sender_type IN ('ai', 'agent')
//   - delivered_at IS NULL
//   - 按 created_at ASC 排序（投递顺序与产生顺序一致）
func (r *SessionMessageRepository) ListOfflineBySessionID(ctx context.Context, sessionID string) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var messages []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Where("sender_type IN ?", []string{"ai", "agent"}).
		Where("delivered_at IS NULL").
		Order("created_at ASC").
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// MarkDelivered 批量标记消息已投递
//
// 行为与原 service.MarkMessagesDelivered 一致：
//   - 仅更新 session_id + id IN 范围内的消息
//   - delivered_at = now（由调用方传入，便于测试与统一时间基准）
//   - messageIDs 为空时无操作
func (r *SessionMessageRepository) MarkDelivered(ctx context.Context, sessionID string, messageIDs []uint, deliveredAt time.Time) error {
	if r == nil || r.db == nil || len(messageIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id = ? AND id IN ?", sessionID, messageIDs).
		Update("delivered_at", &deliveredAt).Error
}

// Delete 按 id 删除单条会话消息（统一收件箱消息删除）
func (r *SessionMessageRepository) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.SessionMessage{}).Error
}

var _ = errors.New

// ListInternalBySession 会话内部备注列表（仅 is_internal=true）
func (r *SessionMessageRepository) ListInternalBySession(ctx context.Context, sessionID string, limit int) ([]*model.SessionMessage, error) {
	var list []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND is_internal = ?", sessionID, true).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// ListRecentDescBySessionID 按 session_id 倒序取最近 N 条消息（AI agent loop 历史上下文用）
func (r *SessionMessageRepository) ListRecentDescBySessionID(ctx context.Context, sessionID string, limit int) ([]model.SessionMessage, error) {
	var hist []model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id desc").Limit(limit).Find(&hist).Error
	return hist, err
}

// macro_session_ops_repo.go 会话动作仓储（五层 L5）
//
// 供 MacroService/RuleEngineService 等编排层执行会话级动作
// （打标签/指派/关单/延迟出站入队），SQL 全部收口在本文件。
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// SessionActionRepository customer_sessions 动作仓储
type SessionActionRepository struct {
	db *gorm.DB
}

// NewSessionActionRepository 构造
func NewSessionActionRepository(db *gorm.DB) *SessionActionRepository {
	return &SessionActionRepository{db: db}
}

// GetTags 读取会话 tags（JSON 数组字符串，空会话返回 ""）
func (r *SessionActionRepository) GetTags(ctx context.Context, sessionID string) (string, error) {
	if r == nil || r.db == nil {
		return "", gorm.ErrInvalidDB
	}
	var tagsRaw string
	err := r.db.WithContext(ctx).Table("customer_sessions").
		Select("COALESCE(tags,'')").Where("session_id = ?", sessionID).Scan(&tagsRaw).Error
	return tagsRaw, err
}

// SaveTags 写回会话 tags
func (r *SessionActionRepository) SaveTags(ctx context.Context, sessionID, tags string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Table("customer_sessions").
		Where("session_id = ?", sessionID).
		Update("tags", tags).Error
}

// AppendTag 追加单个标签（已存在则幂等跳过），返回是否发生追加。
func (r *SessionActionRepository) AppendTag(ctx context.Context, sessionID, tagCode string) (bool, error) {
	raw, err := r.GetTags(ctx, sessionID)
	if err != nil {
		return false, err
	}
	var arr []string
	if json.Unmarshal([]byte(raw), &arr) != nil {
		arr = []string{}
	}
	for _, t := range arr {
		if t == tagCode {
			return false, nil
		}
	}
	arr = append(arr, tagCode)
	merged, _ := json.Marshal(arr)
	if err := r.SaveTags(ctx, sessionID, string(merged)); err != nil {
		return false, err
	}
	return true, nil
}

// AssignAgent 指派坐席
func (r *SessionActionRepository) AssignAgent(ctx context.Context, sessionID, agentID string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Table("customer_sessions").
		Where("session_id = ?", sessionID).
		Update("agent_id", agentID).Error
}

// Close 关单
func (r *SessionActionRepository) Close(ctx context.Context, sessionID string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Table("customer_sessions").
		Where("session_id = ?", sessionID).
		Update("status", "closed").Error
}

// SetStatus 按 session_id 更新状态（escalated 等）
func (r *SessionActionRepository) SetStatus(ctx context.Context, sessionID, status string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Table("customer_sessions").
		Where("session_id = ?", sessionID).
		Update("status", status).Error
}

// GetPlatformAccount 取会话平台与账号（宏出站投递定位用）
func (r *SessionActionRepository) GetPlatformAccount(ctx context.Context, sessionID string) (platform, account string, err error) {
	if r == nil || r.db == nil {
		return "", "", gorm.ErrInvalidDB
	}
	var sess struct {
		Platform string
		Account  string
	}
	err = r.db.WithContext(ctx).Table("customer_sessions").
		Select("platform, COALESCE(account_id,'') AS account").
		Where("session_id = ?", sessionID).Scan(&sess).Error
	return sess.Platform, sess.Account, err
}

// EnqueueOutbound 以 system 名义向 message_hub 入队一条待发送文本（宏/规则引擎动作）。
func (r *SessionActionRepository) EnqueueOutbound(ctx context.Context, sessionID, content string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	platform, account, err := r.GetPlatformAccount(ctx, sessionID)
	if err != nil {
		return err
	}
	now := time.Now()
	rec := &model.MessageHub{
		Platform:       platform,
		MsgID:          fmt.Sprintf("macro_%s_%d", sessionID, now.UnixNano()),
		AccountID:      account,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       "system",
		SenderName:     "宏消息",
		Content:        content,
		ConversationID: sessionID,
		TraceID:        "macro",
		SentAt:         now,
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

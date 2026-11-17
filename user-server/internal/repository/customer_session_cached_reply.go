package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AcceptCachedAIReply 原子提交缓存答案的会话账本，不代表平台已经投递。
// 行锁保护人工状态与连续回复预算；任一写入失败都回滚整个账本。
func (r *CustomerSessionRepository) AcceptCachedAIReply(
	ctx context.Context,
	sessionID string,
	answer string,
	maxAIConsecutive int,
) (*model.CustomerSession, error) {
	if r == nil || r.db == nil || ctx == nil || tenant.IsSystemScope(ctx) ||
		strings.TrimSpace(sessionID) == "" || strings.TrimSpace(answer) == "" {
		return nil, errors.New("cached reply accounting requires an initialized tenant session")
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("cached reply accounting requires tenant identity")
	}
	var session model.CustomerSession
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
			First(&session).Error; err != nil {
			return err
		}
		budgetExhausted := maxAIConsecutive > 0 && session.AIReplyCount >= maxAIConsecutive
		inactive := session.Status == model.SessionStatusClosed || session.Status == model.SessionStatusResolved
		if session.HandlerType != model.HandlerTypeAI || session.HumanReplyCount > 0 ||
			session.DNCBlocked || budgetExhausted || inactive {
			return errors.New("cached reply is not authorized by current session state")
		}
		now := time.Now()
		message := &model.SessionMessage{
			TenantID: tenantID, SessionID: sessionID, Content: answer,
			ContentType: model.MessageTypeText, SenderType: "ai",
			SenderID: "ai_assistant", SenderName: "AI 助手", AISource: "ragcache",
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.CustomerSession{}).
			Where("tenant_id = ? AND id = ?", tenantID, session.ID).
			Updates(map[string]any{
				"ai_reply_count": gorm.Expr("ai_reply_count + 1"),
				"message_count":  gorm.Expr("message_count + 1"),
				"version":        gorm.Expr("version + 1"),
				"status":         model.SessionStatusAIHandling,
				"last_message":   answer, "last_message_by": "ai", "last_message_at": &now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		session.AIReplyCount++
		session.MessageCount++
		session.Version++
		session.Status = model.SessionStatusAIHandling
		session.LastMessage = answer
		session.LastMessageBy = "ai"
		session.LastMessageAt = &now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &session, nil
}

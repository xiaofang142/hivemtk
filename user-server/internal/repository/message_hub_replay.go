package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// HasDeliveredReplySince 查询已确认交付的新答复，不把失败、审计或其它账号充当已答证据。
func (r *MessageHubRepository) HasDeliveredReplySince(ctx context.Context, platform, accountID, conversationID string, since time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, gorm.ErrInvalidDB
	}
	if ctx == nil || tenant.IsSystemScope(ctx) || platform == "" || accountID == "" || conversationID == "" || since.IsZero() {
		return false, errors.New("replay answer: scope unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return false, errors.New("replay answer: tenant unavailable")
	}
	var count int64
	err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Model(&model.MessageHub{}).
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND conversation_id = ?", tenantID, platform, accountID, conversationID).
		Where("direction = ? AND status = ? AND sent_at >= ?", "outbound", "delivered", since).
		Where("COALESCE(msg_type, '') <> ?", "event").
		Where(outboundDeliveryPredicate("")).Count(&count).Error
	return count > 0, err
}

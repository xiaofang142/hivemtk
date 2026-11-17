package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ReplaySession 只接受完整作用域下唯一的未结束会话，绝不根据昵称或最近一行猜身份。
func (r *CustomerSessionRepository) ReplaySession(ctx context.Context, platform, accountID, userID string) (*model.CustomerSession, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if ctx == nil || tenant.IsSystemScope(ctx) || platform == "" || accountID == "" || userID == "" {
		return nil, errors.New("replay session: scope unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("replay session: tenant unavailable")
	}
	rows := []model.CustomerSession{}
	err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND user_id = ?", tenantID, platform, accountID, userID).
		Where("status NOT IN ?", []model.SessionStatus{model.SessionStatusClosed, model.SessionStatusResolved}).
		Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0].SessionID == "" {
		return nil, errors.New("replay session: identity missing or ambiguous")
	}
	return &rows[0], nil
}

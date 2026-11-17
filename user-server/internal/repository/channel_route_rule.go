package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ChannelRouteRuleRepository 入站路由规则（specs/024 T302）。
//
// 约定：**tenantID 永远是第一个业务参数**，且每条 SQL 都带 `tenant_id = ?`。
// 不提供「跨租户批量」接口——那种接口一旦被 Controller 直接调到，就是把租户隔离
// 交给调用方自觉。
type ChannelRouteRuleRepository struct {
	db *gorm.DB
}

func NewChannelRouteRuleRepository(db *gorm.DB) *ChannelRouteRuleRepository {
	return &ChannelRouteRuleRepository{db: db}
}

func NewChannelRouteRuleRepositoryDefault() *ChannelRouteRuleRepository {
	return &ChannelRouteRuleRepository{db: _db.GetDB()}
}

// ListByTenant 某租户全部规则（管理端列表用），按优先级升序。
func (r *ChannelRouteRuleRepository) ListByTenant(ctx context.Context, tenantID string) ([]model.ChannelRouteRule, error) {
	if r == nil || r.db == nil || tenantID == "" {
		return nil, nil
	}
	var rows []model.ChannelRouteRule
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("priority ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListEnabled 引擎用：某租户下「渠道/账号匹配（空串表示不限）」的启用规则，按优先级升序。
func (r *ChannelRouteRuleRepository) ListEnabled(ctx context.Context, tenantID, channel, accountID string) ([]model.ChannelRouteRule, error) {
	if r == nil || r.db == nil || tenantID == "" {
		return nil, nil
	}
	q := r.db.WithContext(ctx).
		Where("tenant_id = ? AND enabled = ?", tenantID, true).
		Where("channel = '' OR channel = ?", channel)
	if accountID != "" {
		q = q.Where("account_id = '' OR account_id = ?", accountID)
	} else {
		// 无账号上下文（系统事件）：只取「不限账号」的规则，
		// 否则会把某账号的专属规则误用到别处。
		q = q.Where("account_id = ''")
	}
	var rows []model.ChannelRouteRule
	if err := q.Order("priority ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Upsert 新增或更新（ID 为 0 时新增）。租户字段强制取入参，忽略调用方填的值：
// 规则归属只能由「当前请求租户」决定，不能由请求体声明。
func (r *ChannelRouteRuleRepository) Upsert(ctx context.Context, tenantID string, rule *model.ChannelRouteRule) error {
	if r == nil || r.db == nil || rule == nil || tenantID == "" {
		return nil
	}
	rule.TenantID = tenantID
	if rule.ID == 0 {
		return r.db.WithContext(ctx).Create(rule).Error
	}
	// 更新只允许改本租户的行：WHERE 带 tenant_id，命中 0 行时调用方据此返回 404。
	res := r.db.WithContext(ctx).Model(&model.ChannelRouteRule{}).
		Where("id = ? AND tenant_id = ?", rule.ID, tenantID).
		Updates(map[string]any{
			"channel":    rule.Channel,
			"account_id": rule.AccountID,
			"event_type": rule.EventType,
			"match":      rule.Match,
			"target":     rule.Target,
			"target_ref": rule.TargetRef,
			"priority":   rule.Priority,
			"enabled":    rule.Enabled,
			"note":       rule.Note,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete 删除本租户的一条规则；返回是否真的删掉（false → 调用方回 404）。
func (r *ChannelRouteRuleRepository) Delete(ctx context.Context, tenantID string, id uint) (bool, error) {
	if r == nil || r.db == nil || tenantID == "" || id == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&model.ChannelRouteRule{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

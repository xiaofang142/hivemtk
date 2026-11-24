package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrBridgeAccountTenantStoreUnavailable 桥接账号租户反查仓储未装配 DB 句柄。
var ErrBridgeAccountTenantStoreUnavailable = errors.New("bridge account tenant store unavailable")

// BridgeAccountTenantRepository 只读反查 bridge_accounts 的唯一租户归属。
// 0 行表示未命中，2 行表示歧义；运行错误不得被当成未命中。
type BridgeAccountTenantRepository interface {
	ResolveTenantID(ctx context.Context, channel, accountID string) (uint, error)
}

type bridgeAccountTenantRepo struct {
	db *gorm.DB
}

// NewBridgeAccountTenantRepository 使用全局 DB 装配。
func NewBridgeAccountTenantRepository() BridgeAccountTenantRepository {
	return &bridgeAccountTenantRepo{db: _db.GetDB()}
}

// NewBridgeAccountTenantRepositoryWithDB 用显式句柄构造（装配点/测试用）。
func NewBridgeAccountTenantRepositoryWithDB(gormDB *gorm.DB) BridgeAccountTenantRepository {
	return &bridgeAccountTenantRepo{db: gormDB}
}

func (r *bridgeAccountTenantRepo) ResolveTenantID(ctx context.Context, channel, accountID string) (uint, error) {
	if r == nil || r.db == nil {
		return 0, ErrBridgeAccountTenantStoreUnavailable
	}
	channel = strings.TrimSpace(channel)
	accountID = strings.TrimSpace(accountID)
	if channel == "" || accountID == "" {
		return 0, nil
	}
	var rows []model.BridgeAccount
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Model(&model.BridgeAccount{}).
		Distinct("tenant_id").
		Where("channel = ? AND account_id = ?", channel, accountID).
		Limit(2).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	switch len(rows) {
	case 0:
		return 0, nil
	case 1:
		return rows[0].TenantID, nil
	default:
		return 0, tenant.ErrAmbiguousTenantKey
	}
}

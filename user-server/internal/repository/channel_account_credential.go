package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelAccountCredentialKV 把平台账号凭据存进既有 system_config_kv（租户维度），
// 不改 schema、不新建平台账号表。键由服务层生成（含显式 tenantID），
// 本仓储再用 tenant_id 条件做二次隔离，防串租户。
type ChannelAccountCredentialKV struct {
	db *gorm.DB
}

// NewChannelAccountCredentialKV 创建凭据 KV 仓储（全局 DB 装配）。
func NewChannelAccountCredentialKV() *ChannelAccountCredentialKV {
	return &ChannelAccountCredentialKV{db: _db.GetDB()}
}

// NewChannelAccountCredentialKVWithDB 用显式句柄构造（装配点/测试用）。
func NewChannelAccountCredentialKVWithDB(gormDB *gorm.DB) *ChannelAccountCredentialKV {
	return &ChannelAccountCredentialKV{db: gormDB}
}

func (r *ChannelAccountCredentialKV) Available() bool { return r != nil && r.db != nil }

func (r *ChannelAccountCredentialKV) GetRaw(ctx context.Context, tenantID uint, key string) (string, error) {
	if !r.Available() || tenantID == 0 {
		return "", ErrChannelStoreUnavailable
	}
	var row model.SystemConfigKV
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND key = ?", tenantID, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

func (r *ChannelAccountCredentialKV) SetRaw(ctx context.Context, tenantID uint, key, value string) error {
	if !r.Available() || tenantID == 0 {
		return ErrChannelStoreUnavailable
	}
	row := model.SystemConfigKV{TenantID: tenantID, Key: key, Value: value}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

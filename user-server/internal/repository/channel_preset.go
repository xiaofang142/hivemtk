package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelGlobalTenantID 全局渠道模板归属租户。
//
// channels.code 是全局唯一索引（不改 schema），所以「预设模板」只能有一行；
// 每个租户的选择开启状态落在自己的 system_config_kv 行上，互不覆盖。
const ChannelGlobalTenantID uint = 0

const channelPresetKeyPrefix = "channel_preset:"

// ChannelPresetKey 租户内预设渠道启停状态的 KV 键。
func ChannelPresetKey(code string) string {
	return channelPresetKeyPrefix + strings.ToLower(strings.TrimSpace(code))
}

// ChannelPresetStateRepository 预设渠道的「租户启用配置」存储口。
//
// 复用既有 system_config_kv 表（主键 tenant_id+key），不新建表、不改 schema。
// 未写过 key 与显式关闭必须分开：Get 返回 (false,nil) 表示未配置，调用方据此
// 决定默认启用；仓储不可用/无租户上下文返回 error，不能静默当成关闭。
type ChannelPresetStateRepository interface {
	Available() bool
	Get(ctx context.Context, tenantID uint, code string) (value string, err error)
	Set(ctx context.Context, tenantID uint, code string, value string) error
}

type channelPresetStateRepo struct {
	db *gorm.DB
}

// NewChannelPresetStateRepository 创建预设状态仓储。
func NewChannelPresetStateRepository() ChannelPresetStateRepository {
	return &channelPresetStateRepo{db: _db.GetDB()}
}

// NewChannelPresetStateRepositoryWithDB 用显式句柄构造（装配点/测试用）。
func NewChannelPresetStateRepositoryWithDB(gormDB *gorm.DB) ChannelPresetStateRepository {
	return &channelPresetStateRepo{db: gormDB}
}

func (r *channelPresetStateRepo) Available() bool { return r != nil && r.db != nil }

func (r *channelPresetStateRepo) Get(ctx context.Context, tenantID uint, code string) (string, error) {
	if !r.Available() || tenantID == 0 {
		return "", ErrChannelStoreUnavailable
	}
	key := ChannelPresetKey(code)
	if key == channelPresetKeyPrefix {
		return "", errors.New("预设渠道码为空")
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

func (r *channelPresetStateRepo) Set(ctx context.Context, tenantID uint, code, value string) error {
	if !r.Available() || tenantID == 0 {
		return ErrChannelStoreUnavailable
	}
	key := ChannelPresetKey(code)
	if key == channelPresetKeyPrefix {
		return errors.New("预设渠道码为空")
	}
	row := model.SystemConfigKV{TenantID: tenantID, Key: key, Value: value}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

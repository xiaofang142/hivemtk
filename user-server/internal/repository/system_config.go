package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// SystemConfigRepository 系统配置仓库接口
type SystemConfigRepository interface {
	GetConfig(ctx context.Context) (*model.SystemConfig, error)
	SaveConfig(ctx context.Context, config *model.SystemConfig) (*model.SystemConfig, error)
	CountUsers(ctx context.Context) (int64, error)
	PingDB(ctx context.Context) bool
}

// ErrSystemConfigStoreUnavailable 表示没装配数据库句柄。
// 与 gorm.ErrRecordNotFound 分开：前者是"没指对库"（装配问题），后者是"库里还没这行"（业务状态）。
// 合成一种的话，运维把上传上限调大不生效时看到的是"没配置过"，真相是"库没指对"，查不到根因。
var ErrSystemConfigStoreUnavailable = errors.New("system_config: 未装配数据库句柄")

type systemConfigRepo struct {
	db *gorm.DB
}

// NewSystemConfigRepository 创建系统配置仓库实例
func NewSystemConfigRepository() SystemConfigRepository {
	return &systemConfigRepo{db: _db.GetDB()}
}

func (r *systemConfigRepo) GetConfig(ctx context.Context) (*model.SystemConfig, error) {
	// 句柄可能没装配（装配顺序不对，或测试里还没 SetDB）。gorm.DB 是 nil 时
	// WithContext 直接空指针崩，而崩在这里等于把"读不到配置"伪装成"进程挂了"。
	// 上传上限这条链路现在每次上传都会走一遍，更不能崩——返回 error 让调用方兜底。
	if r == nil || r.db == nil {
		return nil, ErrSystemConfigStoreUnavailable
	}
	var config model.SystemConfig
	err := r.db.WithContext(ctx).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// SaveConfig 保存系统配置。
// 系统配置是单例表：已存在（First 命中）时必须用 Save 原地更新——
// 原 FirstOrCreate 在已存在时不写入任何传入字段，前端保存永远是假动作。
func (r *systemConfigRepo) SaveConfig(ctx context.Context, config *model.SystemConfig) (*model.SystemConfig, error) {
	var existing model.SystemConfig
	err := r.db.WithContext(ctx).First(&existing).Error
	switch {
	case err == nil:
		config.ID = existing.ID
		if err := r.db.WithContext(ctx).Save(config).Error; err != nil {
			return nil, err
		}
		return config, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := r.db.WithContext(ctx).Create(config).Error; err != nil {
			return nil, err
		}
		return config, nil
	default:
		return nil, err
	}
}

func (r *systemConfigRepo) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.SystemUser{}).Count(&n).Error
	return n, err
}

func (r *systemConfigRepo) PingDB(ctx context.Context) bool {
	return r.db.WithContext(ctx).Exec("SELECT 1").Error == nil
}

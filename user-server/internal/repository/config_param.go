package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"

	"gorm.io/gorm"
)

// 变更审计里的改前/改后值默认加密落库（OPT-SEC-04 残项，与 api_logs 同一把钥匙）。
//
// 为什么审计表也要加密：config_params 里存着 API Key 一类的敏感参数（见
// config_param_group_key_unique 的复合唯一键分组），明文留痕等于把当前与历史
// 密钥全量抄进一张无访问控制的表 —— 比主表泄露更糟，主表至少有 config 读取
// 权限，审计表通常是「谁改了什么」的运维只读视图。
//
// 列类型已是 text，无需迁移（对照 api_logs 需要 057 迁移纯粹是因为
// ip_address 是 varchar(45) 装不下密文）。存量明文行不做批量改写：AES-256-GCM
// 的 nonce 随机生成且密钥只在应用侧，纯 SQL 改不了；读取出口 Decrypt 对非
// `enc:v1:` 前缀原样返回，故新旧混读安全。
func encryptAuditValues(oldValue, newValue string) (string, string) {
	return dbencrypt.Encrypt(oldValue), dbencrypt.Encrypt(newValue)
}

// ConfigParamRepository 动态参数仓储
type ConfigParamRepository struct {
	db *gorm.DB
}

func NewConfigParamRepository(db *gorm.DB) *ConfigParamRepository {
	return &ConfigParamRepository{db: db}
}

// List 返回全部参数（管理端用）
func (r *ConfigParamRepository) List(ctx context.Context) ([]model.ConfigParam, error) {
	var params []model.ConfigParam
	if err := r.db.WithContext(ctx).Order("param_group").Order("key").Find(&params).Error; err != nil {
		return nil, err
	}
	return params, nil
}

// ListByGroup 按分组返回参数（管理端分组展示）
func (r *ConfigParamRepository) ListByGroup(ctx context.Context, group string) ([]model.ConfigParam, error) {
	var params []model.ConfigParam
	if err := r.db.WithContext(ctx).Where(&model.ConfigParam{Group: group}).Order("key").Find(&params).Error; err != nil {
		return nil, err
	}
	return params, nil
}

// GetByGroupKey 单条查询（Service 层类型化读取的底层）
func (r *ConfigParamRepository) GetByGroupKey(ctx context.Context, group, key string) (*model.ConfigParam, error) {
	var p model.ConfigParam
	if err := r.db.WithContext(ctx).Where(&model.ConfigParam{Group: group, Key: key}).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateValue 更新参数值（含 actor 审计）
func (r *ConfigParamRepository) UpdateValue(ctx context.Context, group, key, newValue string, actorID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.ConfigParam
		if err := tx.Where(&model.ConfigParam{Group: group, Key: key}).First(&p).Error; err != nil {
			return err
		}
		if p.ReadOnly {
			return gorm.ErrRecordNotFound
		}
		oldValue := p.Value
		updates := map[string]any{
			"Value":      newValue,
			"updated_by": actorID,
		}
		if err := tx.Model(&p).Updates(updates).Error; err != nil {
			return err
		}
		encOld, encNew := encryptAuditValues(oldValue, newValue)
		return tx.Create(&model.ConfigParamAuditLog{
			ParamKey: key,
			OldValue: encOld,
			NewValue: encNew,
			Action:   "update",
			ActorID:  actorID,
		}).Error
	})
}

// ResetToDefault 重置单条为默认值
func (r *ConfigParamRepository) ResetToDefault(ctx context.Context, group, key string, actorID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.ConfigParam
		if err := tx.Where(&model.ConfigParam{Group: group, Key: key}).First(&p).Error; err != nil {
			return err
		}
		oldValue := p.Value
		if err := tx.Model(&p).Updates(map[string]any{
			"Value":      p.DefaultValue,
			"updated_by": actorID,
		}).Error; err != nil {
			return err
		}
		encOld, encNew := encryptAuditValues(oldValue, p.DefaultValue)
		return tx.Create(&model.ConfigParamAuditLog{
			ParamKey: key,
			OldValue: encOld,
			NewValue: encNew,
			Action:   "reset",
			ActorID:  actorID,
		}).Error
	})
}

// BulkResetGroup 整组重置默认值
func (r *ConfigParamRepository) BulkResetGroup(ctx context.Context, group string, actorID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var params []model.ConfigParam
		if err := tx.Where(&model.ConfigParam{Group: group}).Find(&params).Error; err != nil {
			return err
		}
		for _, p := range params {
			if p.ReadOnly || p.Value == p.DefaultValue {
				continue
			}
			oldValue := p.Value
			if err := tx.Model(&p).Update("Value", p.DefaultValue).Error; err != nil {
				return err
			}
			encOld, encNew := encryptAuditValues(oldValue, p.DefaultValue)
			if err := tx.Create(&model.ConfigParamAuditLog{
				ParamKey: p.Key,
				OldValue: encOld,
				NewValue: encNew,
				Action:   "bulk_reset",
				ActorID:  actorID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AuditLogs 变更日志查询（管理端只读）
func (r *ConfigParamRepository) AuditLogs(ctx context.Context, limit int) ([]model.ConfigParamAuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	var logs []model.ConfigParamAuditLog
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	for i := range logs {
		logs[i].OldValue = dbencrypt.Decrypt(logs[i].OldValue)
		logs[i].NewValue = dbencrypt.Decrypt(logs[i].NewValue)
	}
	return logs, nil
}

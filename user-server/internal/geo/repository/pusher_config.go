package repository

import (
	"context"
	"encoding/json"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/dbencrypt"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GeoPusherConfigRepository 蜘蛛平台配置仓储。
//
// config_json 存平台凭据（百度 token / Google Service Account 私钥 / IndexNow key）：
// 写入前经 dbencrypt 加密成 JSON 字符串落库，读取时解密还原成原 JSON 对象。
// 存量明文行无 `enc:v1:` 前缀，dbencrypt 原样透传（向后兼容）。
type GeoPusherConfigRepository interface {
	List(ctx context.Context, page, limit int) ([]*model.GeoPusherConfig, int64, error)
	GetByID(ctx context.Context, id string) (*model.GeoPusherConfig, error)
	Create(ctx context.Context, cfg *model.GeoPusherConfig) error
	// Update 只改可编辑列（platform/config_json/daily_limit/active）：
	// used_today/last_reset_at 由配额回写维护，tenant_id 由隔离 callback 维护。
	Update(ctx context.Context, cfg *model.GeoPusherConfig) error
	Delete(ctx context.Context, id string) error
	// ListAll 全量配置（装配期注册推送引擎用，调用方负责给系统作用域 ctx）
	ListAll(ctx context.Context) ([]*model.GeoPusherConfig, error)
	// UpdateUsage 回写当日已用量（配额同步）
	UpdateUsage(ctx context.Context, platform string, used int) error
}

type geoPusherConfigRepo struct{ db *gorm.DB }

func NewGeoPusherConfigRepository() GeoPusherConfigRepository {
	return &geoPusherConfigRepo{db: _db.GetDB()}
}

// NewGeoPusherConfigRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoPusherConfigRepositoryWithDB(db *gorm.DB) GeoPusherConfigRepository {
	return &geoPusherConfigRepo{db: db}
}

func (r *geoPusherConfigRepo) List(ctx context.Context, page, limit int) ([]*model.GeoPusherConfig, int64, error) {
	var list []*model.GeoPusherConfig
	var total int64
	q := r.db.WithContext(ctx).Model(&model.GeoPusherConfig{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("platform ASC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	openConfigs(list)
	return list, total, nil
}

func (r *geoPusherConfigRepo) GetByID(ctx context.Context, id string) (*model.GeoPusherConfig, error) {
	var cfg model.GeoPusherConfig
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&cfg).Error; err != nil {
		return nil, err
	}
	cfg.ConfigJSON = openConfig(cfg.ConfigJSON)
	return &cfg, nil
}

func (r *geoPusherConfigRepo) Create(ctx context.Context, cfg *model.GeoPusherConfig) error {
	plain := cfg.ConfigJSON
	cfg.ConfigJSON = sealConfig(plain)
	// 软删除行仍占着 (tenant_id, platform) 唯一索引：先找未删行会漏掉它，
	// 直接 Create 会抛 23505。命中同平台的历史行时原位复活并覆盖配置。
	var existing model.GeoPusherConfig
	if err := r.db.WithContext(ctx).Unscoped().
		Where("platform = ?", cfg.Platform).First(&existing).Error; err == nil && existing.DeletedAt.Valid {
		now := time.Now()
		if err := r.db.WithContext(ctx).Unscoped().Model(&model.GeoPusherConfig{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"config_json": cfg.ConfigJSON,
				"daily_limit": cfg.DailyLimit,
				"active":      cfg.Active,
				"deleted_at":  nil,
				"updated_at":  now,
			}).Error; err != nil {
			cfg.ConfigJSON = plain
			return err
		}
		cfg.ID = existing.ID
		cfg.TenantID = existing.TenantID
		cfg.CreatedAt = existing.CreatedAt
		cfg.UpdatedAt = now
		cfg.ConfigJSON = plain
		return nil
	}
	err := r.db.WithContext(ctx).Create(cfg).Error
	cfg.ConfigJSON = plain // 响应回显明文，不把密文吐给调用方
	return err
}

func (r *geoPusherConfigRepo) Update(ctx context.Context, cfg *model.GeoPusherConfig) error {
	res := r.db.WithContext(ctx).Model(&model.GeoPusherConfig{}).
		Where("id = ?", cfg.ID).
		Updates(map[string]any{
			"platform":    cfg.Platform,
			"config_json": sealConfig(cfg.ConfigJSON),
			"daily_limit": cfg.DailyLimit,
			"active":      cfg.Active,
			"updated_at":  time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoPusherConfigRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoPusherConfig{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoPusherConfigRepo) ListAll(ctx context.Context) ([]*model.GeoPusherConfig, error) {
	var list []*model.GeoPusherConfig
	if err := r.db.WithContext(ctx).Order("platform ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	openConfigs(list)
	return list, nil
}

func (r *geoPusherConfigRepo) UpdateUsage(ctx context.Context, platform string, used int) error {
	return r.db.WithContext(ctx).Model(&model.GeoPusherConfig{}).
		Where("platform = ?", platform).
		Updates(map[string]any{
			"used_today":    used,
			"last_reset_at": time.Now(),
			"updated_at":    time.Now(),
		}).Error
}

// sealConfig 落库加密：密文以 JSON 字符串形式存进 jsonb 列（`"enc:v1:..."`）。
// 未加密（MASTER_KEY 缺失/已是密文）时原样返回，避免把明文包装成字符串改变语义。
func sealConfig(raw datatypes.JSON) datatypes.JSON {
	if len(raw) == 0 {
		return raw
	}
	sealed := dbencrypt.Encrypt(string(raw))
	if sealed == string(raw) {
		return raw
	}
	b, err := json.Marshal(sealed)
	if err != nil {
		return raw
	}
	return datatypes.JSON(b)
}

// openConfig 读取解密；非「JSON 字符串形态」的存量明文对象原样返回。
func openConfig(raw datatypes.JSON) datatypes.JSON {
	if len(raw) == 0 {
		return raw
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw
	}
	plain := dbencrypt.Decrypt(s)
	if plain == s {
		return raw
	}
	return datatypes.JSON(plain)
}

func openConfigs(list []*model.GeoPusherConfig) {
	for _, cfg := range list {
		cfg.ConfigJSON = openConfig(cfg.ConfigJSON)
	}
}

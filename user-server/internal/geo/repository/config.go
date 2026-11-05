package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoConfigRepository GEO 配置仓储接口
//
// geo_config 带 tenant_id 列、在租户登记表内：**每个租户一行**。TENANT_MODE=on
// 下查询必须由 ctx 携带租户（或系统豁免），否则隔离 callback fail-closed。
// 因此方法一律收 ctx 首参并 db.WithContext(ctx)，不隐式 context.Background()。
type GeoConfigRepository interface {
	Get(ctx context.Context) (*model.GeoConfig, error)
	Update(ctx context.Context, config *model.GeoConfig) error
}

type geoConfigRepo struct {
	db *gorm.DB
}

func NewGeoConfigRepository() GeoConfigRepository {
	return &geoConfigRepo{db: _db.GetDB()}
}

// NewGeoConfigRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoConfigRepositoryWithDB(db *gorm.DB) GeoConfigRepository {
	return &geoConfigRepo{db: db}
}

func (r *geoConfigRepo) Get(ctx context.Context) (*model.GeoConfig, error) {
	var config model.GeoConfig
	err := r.db.WithContext(ctx).First(&config).Error
	return &config, err
}

func (r *geoConfigRepo) Update(ctx context.Context, config *model.GeoConfig) error {
	db := r.db.WithContext(ctx)
	var existing model.GeoConfig
	err := db.First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(config).Error
	}
	if err != nil {
		return err
	}
	config.ID = existing.ID
	// 不用 Save：并发删除后 Save 会把整行重新 INSERT（复活已删配置），且命中 0 行
	// 时无法与「不存在」区分。
	res := db.Model(&model.GeoConfig{}).Where("id = ?", existing.ID).
		Select("brand_name", "brand_description", "advantages", "competitors", "domain",
			"language", "default_model", "verify_models", "negative_keywords", "cron_specs",
			"product_category", "target_audience", "key_features", "generation_note", "version_label").
		Updates(config)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GeoPlatformAccountRepository GEO 平台账号仓储接口
type GeoPlatformAccountRepository interface {
	Create(ctx context.Context, account *model.GeoPlatformAccount) error
	GetByPlatformAndName(ctx context.Context, platform, accountName string) (*model.GeoPlatformAccount, error)
	GetLatestByPlatform(ctx context.Context, platform string) (*model.GeoPlatformAccount, error)
	GetList(ctx context.Context, platform string, page, limit int) ([]*model.GeoPlatformAccount, int64, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, account *model.GeoPlatformAccount) error
}

type geoPlatformAccountRepo struct {
	db *gorm.DB
}

// NewGeoPlatformAccountRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoPlatformAccountRepositoryWithDB(db *gorm.DB) GeoPlatformAccountRepository {
	return &geoPlatformAccountRepo{db: db}
}

func (r *geoPlatformAccountRepo) Create(ctx context.Context, account *model.GeoPlatformAccount) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *geoPlatformAccountRepo) GetByPlatformAndName(ctx context.Context, platform, accountName string) (*model.GeoPlatformAccount, error) {
	var account model.GeoPlatformAccount
	err := r.db.WithContext(ctx).Where("platform = ? AND account_name = ?", platform, accountName).First(&account).Error
	return &account, err
}

func (r *geoPlatformAccountRepo) GetLatestByPlatform(ctx context.Context, platform string) (*model.GeoPlatformAccount, error) {
	var account model.GeoPlatformAccount
	err := r.db.WithContext(ctx).Where("platform = ?", platform).
		Order("updated_at DESC, created_at DESC, id DESC").
		First(&account).Error
	return &account, err
}

func (r *geoPlatformAccountRepo) GetList(ctx context.Context, platform string, page, limit int) ([]*model.GeoPlatformAccount, int64, error) {
	var accounts []*model.GeoPlatformAccount
	var total int64
	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&model.GeoPlatformAccount{})

	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&accounts).Error
	return accounts, total, err
}

func (r *geoPlatformAccountRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoPlatformAccount{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoPlatformAccountRepo) Update(ctx context.Context, account *model.GeoPlatformAccount) error {
	// 不用 Save：已删除账号会被重新 INSERT，且命中 0 行时无法区分跨租户/不存在。
	res := r.db.WithContext(ctx).Model(&model.GeoPlatformAccount{}).
		Where("id = ?", account.ID).
		Select("platform", "account_id", "account_name", "status", "config").
		Updates(account)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GeoPublishRecordRepository GEO 发布记录仓储接口
type GeoPublishRecordRepository interface {
	Create(ctx context.Context, record *model.GeoPublishRecord) error
	GetList(ctx context.Context, articleID, platform string, page, limit int) ([]*model.GeoPublishRecord, int64, error)
}

type geoPublishRecordRepo struct {
	db *gorm.DB
}

// NewGeoPublishRecordRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoPublishRecordRepositoryWithDB(db *gorm.DB) GeoPublishRecordRepository {
	return &geoPublishRecordRepo{db: db}
}

func (r *geoPublishRecordRepo) Create(ctx context.Context, record *model.GeoPublishRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

func (r *geoPublishRecordRepo) GetList(ctx context.Context, articleID, platform string, page, limit int) ([]*model.GeoPublishRecord, int64, error) {
	var records []*model.GeoPublishRecord
	var total int64
	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&model.GeoPublishRecord{})

	if articleID != "" {
		query = query.Where("article_id = ?", articleID)
	}
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&records).Error
	return records, total, err
}

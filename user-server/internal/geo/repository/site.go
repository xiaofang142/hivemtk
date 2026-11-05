package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoSiteRepository 站点部署配置仓储
type GeoSiteRepository interface {
	List(ctx context.Context, page, limit int) ([]*model.GeoSite, int64, error)
	GetByID(ctx context.Context, id string) (*model.GeoSite, error)
	// GetActive 取当前启用站点（多行启用时按创建时间取最早的一行，保证确定性）
	GetActive(ctx context.Context) (*model.GeoSite, error)
	Create(ctx context.Context, site *model.GeoSite) error
	// Update 只改可编辑列（domain/provider/hugo_path/git_repo/deploy_cmd/active）；
	// health_score/last_deploy_at 由部署流程维护。
	Update(ctx context.Context, site *model.GeoSite) error
	Delete(ctx context.Context, id string) error
	UpdateLastDeployAt(ctx context.Context, id string, at time.Time) error
}

type geoSiteRepo struct{ db *gorm.DB }

func NewGeoSiteRepository() GeoSiteRepository {
	return &geoSiteRepo{db: _db.GetDB()}
}

// NewGeoSiteRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoSiteRepositoryWithDB(db *gorm.DB) GeoSiteRepository {
	return &geoSiteRepo{db: db}
}

func (r *geoSiteRepo) List(ctx context.Context, page, limit int) ([]*model.GeoSite, int64, error) {
	var list []*model.GeoSite
	var total int64
	q := r.db.WithContext(ctx).Model(&model.GeoSite{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoSiteRepo) GetByID(ctx context.Context, id string) (*model.GeoSite, error) {
	var site model.GeoSite
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&site).Error; err != nil {
		return nil, err
	}
	return &site, nil
}

func (r *geoSiteRepo) GetActive(ctx context.Context) (*model.GeoSite, error) {
	var site model.GeoSite
	// 不加排序时多行 active 命中哪一行由执行计划决定：导出与部署可能落到不同站点。
	err := r.db.WithContext(ctx).Where("active = true").Order("created_at ASC").First(&site).Error
	if err != nil {
		return nil, err
	}
	return &site, nil
}

func (r *geoSiteRepo) Create(ctx context.Context, site *model.GeoSite) error {
	// 软删除行仍占着 (tenant_id, domain) 唯一索引：删除站点后重新添加同域名，
	// 直接 Create 会抛 23505。命中历史行时原位复活并覆盖可编辑字段。
	var existing model.GeoSite
	if err := r.db.WithContext(ctx).Unscoped().
		Where("domain = ?", site.Domain).First(&existing).Error; err == nil && existing.DeletedAt.Valid {
		now := time.Now()
		if err := r.db.WithContext(ctx).Unscoped().Model(&model.GeoSite{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"provider":   site.Provider,
				"hugo_path":  site.HugoPath,
				"git_repo":   site.GitRepo,
				"deploy_cmd": site.DeployCmd,
				"active":     site.Active,
				"deleted_at": nil,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		site.ID = existing.ID
		site.TenantID = existing.TenantID
		site.CreatedAt = existing.CreatedAt
		site.UpdatedAt = now
		return nil
	}
	return r.db.WithContext(ctx).Create(site).Error
}

func (r *geoSiteRepo) Update(ctx context.Context, site *model.GeoSite) error {
	res := r.db.WithContext(ctx).Model(&model.GeoSite{}).
		Where("id = ?", site.ID).
		Updates(map[string]any{
			"domain":     site.Domain,
			"provider":   site.Provider,
			"hugo_path":  site.HugoPath,
			"git_repo":   site.GitRepo,
			"deploy_cmd": site.DeployCmd,
			"active":     site.Active,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoSiteRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoSite{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoSiteRepo) UpdateLastDeployAt(ctx context.Context, id string, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.GeoSite{}).
		Where("id = ?", id).Updates(map[string]any{
		"last_deploy_at": at,
		"updated_at":     time.Now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

package repository

// 模板市场仓储（实体审查 #16 从 internal/ops/repository/dashboard_screen.go 迁入）。
//
// 迁出理由：这两张表（`market_templates`/`market_template_downloads`）的模型属于 content 域，
// 原先挂在 ops 的大屏文件里，ops 反向 import contentmodel；调用方（content 服务层）又跨域引 opsrepo。
// 全仓所有读写链都必须带调用方 ctx：两张表都是租户表，on 模式下不带 ctx 会被租户闸直接拒绝。

import (
	"context"
	"errors"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
)

// ErrMarketTemplateMissing 按 id 改租户内可见的模板却命中 0 行（id 属于别租户，或已被删）。
var ErrMarketTemplateMissing = errors.New("模板不存在")

// MarketTemplateRepository 模板市场仓库
type MarketTemplateRepository struct {
	db *gorm.DB
}

// NewMarketTemplateRepository 创建模板市场仓库实例
func NewMarketTemplateRepository(db *gorm.DB) *MarketTemplateRepository {
	return &MarketTemplateRepository{db: db}
}

// GetList 获取模板列表
func (r *MarketTemplateRepository) GetList(ctx context.Context, category, templateType string, page, pageSize int) ([]*contentmodel.MarketTemplate, int64, error) {
	var templates []*contentmodel.MarketTemplate
	var total int64

	cond := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&contentmodel.MarketTemplate{})
		if category != "" {
			q = q.Where("category = ?", category)
		}
		if templateType != "" {
			q = q.Where("type = ?", templateType)
		}
		return q
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("download_count DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// GetByID 根据 ID 获取模板。
//
// 失败一律交出 (nil, err)：命中不到既可能是记录不存在，也可能是租户上下文没绑定，
// 回一个零值结构体等于让上层无从分辨，只能把配置级故障说成「模板不存在」。
func (r *MarketTemplateRepository) GetByID(ctx context.Context, id uint) (*contentmodel.MarketTemplate, error) {
	var template contentmodel.MarketTemplate
	if err := r.db.WithContext(ctx).First(&template, id).Error; err != nil {
		return nil, err
	}
	return &template, nil
}

// IncrementDownload 增加下载次数；命中 0 行如实报错，不把「没写成」报成成功。
func (r *MarketTemplateRepository) IncrementDownload(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Model(&contentmodel.MarketTemplate{}).Where("id = ?", id).
		Update("download_count", gorm.Expr("download_count + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrMarketTemplateMissing
	}
	return nil
}

// GetOfficialTemplates 获取官方模板
func (r *MarketTemplateRepository) GetOfficialTemplates(ctx context.Context, page, pageSize int) ([]*contentmodel.MarketTemplate, int64, error) {
	var templates []*contentmodel.MarketTemplate
	var total int64

	cond := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&contentmodel.MarketTemplate{}).Where("is_official = ?", true)
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("download_count DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// SearchTemplates 搜索模板（ILIKE 大小写不敏感，PostgreSQL 原生支持）。
func (r *MarketTemplateRepository) SearchTemplates(ctx context.Context, keyword string, page, pageSize int) ([]*contentmodel.MarketTemplate, int64, error) {
	var templates []*contentmodel.MarketTemplate
	var total int64

	pattern := "%" + utils.EscapeLike(keyword) + "%"
	cond := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&contentmodel.MarketTemplate{}).
			Where("name ILIKE ? ESCAPE '\\' OR description ILIKE ? ESCAPE '\\'", pattern, pattern)
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("download_count DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// Create 创建模板
func (r *MarketTemplateRepository) Create(ctx context.Context, template *contentmodel.MarketTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

// UpdateRating 更新模板评分；命中 0 行如实报错。
func (r *MarketTemplateRepository) UpdateRating(ctx context.Context, id uint, rating float64) error {
	res := r.db.WithContext(ctx).Model(&contentmodel.MarketTemplate{}).Where("id = ?", id).
		Update("rating", rating)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrMarketTemplateMissing
	}
	return nil
}

// MarketTemplateDownloadRepository 模板下载记录仓库
type MarketTemplateDownloadRepository struct {
	db *gorm.DB
}

// NewMarketTemplateDownloadRepository 创建模板下载记录仓库实例
func NewMarketTemplateDownloadRepository(db *gorm.DB) *MarketTemplateDownloadRepository {
	return &MarketTemplateDownloadRepository{db: db}
}

// Create 创建下载记录
func (r *MarketTemplateDownloadRepository) Create(ctx context.Context, record *contentmodel.MarketTemplateDownload) error {
	return r.db.WithContext(ctx).Create(record).Error
}

// GetByUser 获取某个用户的下载流水。
//
// 「我的下载」按 user_id 裁剪：只加租户谓词不够——同租户内其他人的流水仍是跨用户可见。
func (r *MarketTemplateDownloadRepository) GetByUser(ctx context.Context, userID uint, page, pageSize int) ([]*contentmodel.MarketTemplateDownload, int64, error) {
	var records []*contentmodel.MarketTemplateDownload
	var total int64

	cond := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&contentmodel.MarketTemplateDownload{}).Where("user_id = ?", userID)
	}

	if err := cond().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := cond().Order("downloaded_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/content/model"
	contentrepo "hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// TemplateMarketService 模板市场服务
type TemplateMarketService struct {
	templateRepo *contentrepo.MarketTemplateRepository
	downloadRepo *contentrepo.MarketTemplateDownloadRepository
}

// TemplateMarketDeps 模板市场服务的仓储依赖。
// 装配（仓储构造）由装配处完成：internal/router 直接填本结构体，service 层
// 不再持 *gorm.DB 形参（AV-04c）。
type TemplateMarketDeps struct {
	TemplateRepo *contentrepo.MarketTemplateRepository
	DownloadRepo *contentrepo.MarketTemplateDownloadRepository
}

// NewTemplateMarketService 创建模板市场服务实例（依赖由装配处注入）。
func NewTemplateMarketService(deps TemplateMarketDeps) *TemplateMarketService {
	return &TemplateMarketService{
		templateRepo: deps.TemplateRepo,
		downloadRepo: deps.DownloadRepo,
	}
}

// GetTemplateList 获取模板列表
func (s *TemplateMarketService) GetTemplateList(ctx context.Context, category, templateType string, page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.GetList(ctx, category, templateType, page, pageSize)
}

// GetTemplateByID 获取模板详情
func (s *TemplateMarketService) GetTemplateByID(ctx context.Context, id uint) (*model.MarketTemplate, error) {
	return s.templateRepo.GetByID(ctx, id)
}

// DownloadTemplate 下载模板：记一条归属当前用户的下载流水，并累加下载量。
//
// 读模板的 error 原样上交：命中不到既可能是模板不存在，也可能是调用方没带租户上下文，
// 一律包成「模板不存在」等于把配置级故障说成业务级 404。
func (s *TemplateMarketService) DownloadTemplate(ctx context.Context, userID, templateID uint) (*model.MarketTemplate, error) {
	template, err := s.templateRepo.GetByID(ctx, templateID)
	if err != nil {
		return nil, err
	}

	record := &model.MarketTemplateDownload{
		UserID:       userID,
		TemplateID:   templateID,
		TemplateType: template.Type,
	}
	if err := s.downloadRepo.Create(ctx, record); err != nil {
		return nil, err
	}

	if err := s.templateRepo.IncrementDownload(ctx, templateID); err != nil {
		logger.Warnf("[TemplateMarket] 累加下载量失败 templateID=%d: %v", templateID, err)
	}

	return template, nil
}

// GetOfficialTemplates 获取官方模板
func (s *TemplateMarketService) GetOfficialTemplates(ctx context.Context, page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.GetOfficialTemplates(ctx, page, pageSize)
}

// SearchTemplates 搜索模板
func (s *TemplateMarketService) SearchTemplates(ctx context.Context, keyword string, page, pageSize int) ([]*model.MarketTemplate, int64, error) {
	return s.templateRepo.SearchTemplates(ctx, keyword, page, pageSize)
}

// GetMyDownloads 获取当前用户的下载流水
func (s *TemplateMarketService) GetMyDownloads(ctx context.Context, userID uint, page, pageSize int) ([]*model.MarketTemplateDownload, int64, error) {
	return s.downloadRepo.GetByUser(ctx, userID, page, pageSize)
}

// CreateTemplate 创建模板
func (s *TemplateMarketService) CreateTemplate(ctx context.Context, template *model.MarketTemplate) error {
	if template.Name == "" {
		return errors.New("模板名称不能为空")
	}
	if template.Type == "" {
		return errors.New("模板类型不能为空")
	}
	return s.templateRepo.Create(ctx, template)
}

// RateTemplate 为模板评分（基于新评分更新平均评分）
func (s *TemplateMarketService) RateTemplate(ctx context.Context, id uint, rating float64) error {
	if rating < 0 || rating > 5 {
		return errors.New("评分范围 0-5")
	}
	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	var newRating float64
	if template.Rating > 0 {
		newRating = (template.Rating + rating) / 2
	} else {
		newRating = rating
	}
	return s.templateRepo.UpdateRating(ctx, id, newRating)
}

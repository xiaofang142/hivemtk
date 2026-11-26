package service

import (
	"context"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
)

// CrawlerVisitService 记录按 UA 识别的匿名访问；未验证请求方是否为外部 AI 引擎。
//
// 该入口挂在 JWT 之前，请求本身没有租户归属；SaaS 形态下没有可靠线索把匿名爬虫
// 归属到具体租户，宁可不记也不把各租户的访问混记到默认租户。
type CrawlerVisitService struct {
	repo repository.GeoCrawlerVisitRepository
}

func NewCrawlerVisitService(repo repository.GeoCrawlerVisitRepository) *CrawlerVisitService {
	return &CrawlerVisitService{repo: repo}
}

// RecordAnonymous 记录一条无身份爬虫访问；多租户形态下直接跳过。
func (s *CrawlerVisitService) RecordAnonymous(ctx context.Context, engine, path, userAgent, ip string) error {
	if tenant.IsMultiTenant() {
		return nil
	}
	return s.repo.Create(tenant.ContextWithDefault(ctx), &model.GeoCrawlerVisit{
		Source:    model.CrawlerSourceUAObservation,
		Engine:    engine,
		Path:      path,
		UserAgent: userAgent,
		IP:        ip,
	})
}

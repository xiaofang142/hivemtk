package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// WebVitalService 前端性能指标（Web Vitals）上报服务
type WebVitalService struct {
	repo *repository.WebVitalRepository
}

// NewWebVitalService 构造
func NewWebVitalService() *WebVitalService {
	return &WebVitalService{repo: repository.NewWebVitalRepository()}
}

// NewWebVitalServiceWithRepo 构造（注入仓储，用于测试；AV-04c 批次4 收口：
// service 不再直接 `_db.GetDB().WithContext(ctx)` 直写，改经仓储，连接语义不变）。
func NewWebVitalServiceWithRepo(repo *repository.WebVitalRepository) *WebVitalService {
	return &WebVitalService{repo: repo}
}

// Report 上报一条 Web Vitals 指标
func (s *WebVitalService) Report(ctx context.Context, rec *model.WebVitalRecord) error {
	return s.repo.Create(ctx, rec)
}

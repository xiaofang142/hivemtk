package service

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// WebVitalService 前端性能指标（Web Vitals）上报服务
type WebVitalService struct{}

// NewWebVitalService 构造
func NewWebVitalService() *WebVitalService {
	return &WebVitalService{}
}

// Report 上报一条 Web Vitals 指标
func (s *WebVitalService) Report(ctx context.Context, rec *model.WebVitalRecord) error {
	return _db.GetDB().WithContext(ctx).Create(rec).Error
}

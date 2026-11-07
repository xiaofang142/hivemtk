package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// WebVitalRepository 前端性能指标（Web Vitals: CLS/FID/LCP/FCP/TTFB）上报写入口。
//
// AV-04c 批次4 自 service 层下沉：原 ops/service/web_vital.go 的 Report 直接
// `_db.GetDB().WithContext(ctx).Create(rec)`（service 直取 DB 句柄宽口径），
// 现经本仓储收口，连接与写入口径不变（同表 web_vital_records，同 Create）。
type WebVitalRepository struct {
	db *gorm.DB
}

// NewWebVitalRepository 构造（全局句柄；遵循本包其他仓储约定）。
func NewWebVitalRepository() *WebVitalRepository {
	return &WebVitalRepository{db: GetDB()}
}

// NewWebVitalRepositoryWithDB 构造（注入连接，用于测试）。
func NewWebVitalRepositoryWithDB(db *gorm.DB) *WebVitalRepository {
	return &WebVitalRepository{db: db}
}

// Create 落一条 Web Vitals 指标。原样透传 GORM Create 的错误，不加包装：
// 上报失败对调用方只是"这条没记上"，错误文案不改变行为契约。
func (r *WebVitalRepository) Create(ctx context.Context, rec *model.WebVitalRecord) error {
	return r.db.WithContext(ctx).Create(rec).Error
}

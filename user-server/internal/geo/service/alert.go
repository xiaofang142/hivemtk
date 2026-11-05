package service

import (
	"context"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// AlertService GEO 告警中心服务
//
// 负面监控/异常检测定时任务将命中写入 geo_alerts（scheduler.negativeMonitorJob），
// 本服务向用户提供告警的消费闭环：查询、确认（已读）、删除、未读角标。
type AlertService struct {
	repo repository.GeoAlertRepository
}

// NewAlertService 创建告警服务
func NewAlertService(repo repository.GeoAlertRepository) *AlertService {
	return &AlertService{repo: repo}
}

// AlertQuery 告警列表查询参数
type AlertQuery struct {
	Type  string
	Level string
	Page  int
	Limit int
}

// AlertListResult 告警列表分页结果
type AlertListResult struct {
	List  []*model.GeoAlert `json:"list"`
	Total int64             `json:"total"`
}

// CreateAlert 写入一条告警（定时任务/审计复查共用）。
func (s *AlertService) CreateAlert(ctx context.Context, alert *model.GeoAlert) error {
	return s.repo.Create(ctx, alert)
}

// ListAlerts 分页查询告警列表
func (s *AlertService) ListAlerts(ctx context.Context, q AlertQuery) (*AlertListResult, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	list, total, err := s.repo.List(ctx, q.Type, q.Level, q.Page, q.Limit)
	if err != nil {
		return nil, err
	}
	return &AlertListResult{List: list, Total: total}, nil
}

// CountUnread 未确认告警数（前端角标）
func (s *AlertService) CountUnread(ctx context.Context) (int64, error) {
	return s.repo.CountUnread(ctx)
}

// MarkNotified 确认（标记已读）单条告警
func (s *AlertService) MarkNotified(ctx context.Context, id uint) error {
	return s.repo.MarkNotified(ctx, id)
}

// DeleteAlert 删除单条告警
func (s *AlertService) DeleteAlert(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

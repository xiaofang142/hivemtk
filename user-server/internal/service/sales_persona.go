package service

import (
	"time"

	"context"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// SalesPersonaService 销冠能力画像服务
type SalesPersonaService struct {
	repo *repository.SalesPersonaRepository
}

// NewSalesPersonaService 创建服务
func NewSalesPersonaService() *SalesPersonaService {
	return NewSalesPersonaServiceWithDB(repository.GetDB())
}

// NewSalesPersonaServiceWithDB 带 DB 的版本（用于测试）
func NewSalesPersonaServiceWithDB(db *gorm.DB) *SalesPersonaService {
	var repo *repository.SalesPersonaRepository
	if db != nil {
		repo = repository.NewSalesPersonaRepository(db)
	}
	return &SalesPersonaService{repo: repo}
}

// ensureReposFromDB repo 缺失时从全局 DB 懒构造（fail-open，无 DB 时由调用方空值防御兜底）
func (s *SalesPersonaService) ensureReposFromDB(ctx context.Context) {
	if s.repo != nil {
		return
	}
	if db := repository.GetDB(); db != nil {
		s.repo = repository.NewSalesPersonaRepository(db)
	}
}

// PersonaItem 能力画像项
type PersonaItem struct {
	Tag    string  `json:"tag"`
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Sample int64   `json:"sample"`
	Trend  string  `json:"trend"`
}

// PersonaReport 销冠画像报告
type PersonaReport struct {
	StaffID      uint          `json:"staff_id"`
	StaffName    string        `json:"staff_name"`
	OverallScore float64       `json:"overall_score"`
	Items        []PersonaItem `json:"items"`
	GeneratedAt  time.Time     `json:"generated_at"`
}

// BuildReport 构建销冠能力画像
func (s *SalesPersonaService) BuildReport(ctx context.Context, staffID uint) (*PersonaReport, error) {
	s.ensureReposFromDB(ctx)
	report := &PersonaReport{
		StaffID:     staffID,
		Items:       make([]PersonaItem, 0, 8),
		GeneratedAt: time.Now(),
	}

	avgResponseSec, _ := s.repo.AvgHumanResponseSec(ctx)
	respScore := 100.0
	if avgResponseSec > 60 {
		respScore = 60
	} else if avgResponseSec > 30 {
		respScore = 80
	} else if avgResponseSec > 10 {
		respScore = 90
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "response_speed", Name: "响应速度", Score: respScore, Sample: 100, Trend: "stable",
	})

	sessions, orders, _ := s.repo.SessionOrderCount(ctx, staffID)
	convScore := 0.0
	if sessions > 0 {
		convScore = float64(orders) / float64(sessions) * 100
		if convScore > 100 {
			convScore = 100
		}
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "conversion", Name: "转化能力", Score: convScore, Sample: sessions, Trend: "stable",
	})

	sopCount, _ := s.repo.CountSopExecutionsByStaff(ctx, staffID)
	sopScore := float64(sopCount) * 5
	if sopScore > 100 {
		sopScore = 100
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "sop_usage", Name: "SOP 使用率", Score: sopScore, Sample: sopCount, Trend: "up",
	})

	ragCount, _ := s.repo.CountRagQueryLogsByUser(ctx, staffID)
	ragScore := float64(ragCount) * 2
	if ragScore > 100 {
		ragScore = 100
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "knowledge_usage", Name: "知识库调用", Score: ragScore, Sample: ragCount, Trend: "up",
	})

	objCount, _ := s.repo.CountObjectionRecordsByStaff(ctx, staffID)
	objScore := float64(objCount) * 3
	if objScore > 100 {
		objScore = 100
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "objection_handling", Name: "异议处理", Score: objScore, Sample: objCount, Trend: "stable",
	})

	satStats, _ := s.repo.SatisfactionByStaff(ctx, staffID)
	if satStats == nil {
		satStats = &repository.SatisfactionStats{}
	}
	satScore := satStats.Avg / 5.0 * 100
	report.Items = append(report.Items, PersonaItem{
		Tag: "satisfaction", Name: "客户满意度", Score: satScore, Sample: satStats.Count, Trend: "up",
	})

	closeStats, _ := s.repo.SessionCloseByStaff(ctx, staffID)
	if closeStats == nil {
		closeStats = &repository.SessionCloseStats{}
	}
	closeScore := 0.0
	if closeStats.Total > 0 {
		closeScore = float64(closeStats.Closed) / float64(closeStats.Total) * 100
	}
	report.Items = append(report.Items, PersonaItem{
		Tag: "followup", Name: "跟进及时性", Score: closeScore, Sample: closeStats.Total, Trend: "stable",
	})

	total := 0.0
	for _, it := range report.Items {
		total += it.Score
	}
	report.OverallScore = total / float64(len(report.Items))

	return report, nil
}

// ListStaffs 列出有数据的员工
func (s *SalesPersonaService) ListStaffs(ctx context.Context) ([]map[string]any, error) {
	s.ensureReposFromDB(ctx)
	rows, err := s.repo.ListTopStaffsBySession(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"staff_id":      r.AgentID,
			"session_count": r.Count,
		}
	}
	return out, nil
}

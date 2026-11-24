package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/repository"
)

// personaWindowDays 画像观察窗口（天）。当前窗口 [now-30d, now)，上一窗口
// [now-60d, now-30d)；趋势必须由这两个相邻窗口的证据对比得出，不得固定 stable/up。
const personaWindowDays = 30

// SalesPersonaService 销冠能力画像服务
type SalesPersonaService struct {
	repo *repository.SalesPersonaRepository
}

// NewSalesPersonaService 创建服务
//
// AV-04c 批次6：仓储改 Default 构造在仓储包内捕获全局句柄；全局无句柄时仓储为
// nil，读路径由 BuildReport/ListStaffs 先经 ensureReposFromDB 再用仓储，8 个聚合
// 查询方法自带 r == nil || r.db == nil 守卫，输出与原「直取句柄拼仓储」一致。
func NewSalesPersonaService() *SalesPersonaService {
	return &SalesPersonaService{repo: repository.NewSalesPersonaRepositoryDefault()}
}

// ensureReposFromDB repo 缺失时从全局 DB 懒构造（fail-open，无 DB 时由调用方空值防御兜底）。
// Default 构造在无句柄时返回 nil：s.repo 保持 nil，下次调用再试（原懒构造语义不变）。
func (s *SalesPersonaService) ensureReposFromDB(ctx context.Context) {
	if s.repo != nil {
		return
	}
	s.repo = repository.NewSalesPersonaRepositoryDefault()
}

// PersonaItem 能力画像项
// Status: "ok" 有真实样本 / "no_data" 窗口内无样本 / "error" 查询失败（独立故障标识）。
type PersonaItem struct {
	Tag    string  `json:"tag"`
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Sample int64   `json:"sample"`
	Trend  string  `json:"trend"`
	Status string  `json:"status"`
}

// PersonaReport 销冠画像报告
type PersonaReport struct {
	StaffID      uint          `json:"staff_id"`
	StaffName    string        `json:"staff_name"`
	OverallScore float64       `json:"overall_score"`
	Items        []PersonaItem `json:"items"`
	GeneratedAt  time.Time     `json:"generated_at"`

	// 观察窗口显式化，读数可复算；Errors 汇总各指标的数据故障，不得静默吞成满分。
	WindowDays int       `json:"window_days"`
	WindowFrom time.Time `json:"window_from"`
	WindowTo   time.Time `json:"window_to"`
	Errors     []string  `json:"errors,omitempty"`
}

// personaSample 某观察窗口内该员工的全部原始指标与查询错误。
type personaSample struct {
	respAvg     float64
	respSample  int64
	sessions    int64
	orders      int64
	sop         int64
	rag         int64
	obj         int64
	satAvg      float64
	satCount    int64
	closeTotal  int64
	closeClosed int64

	respErr  error
	convErr  error
	sopErr   error
	ragErr   error
	objErr   error
	satErr   error
	closeErr error
}

// collectPersonaSample 读取单个窗口的全部指标；错误逐项保留，不在本层吞掉。
func (s *SalesPersonaService) collectPersonaSample(ctx context.Context, staffID uint, start, end time.Time) personaSample {
	var p personaSample
	resp, err := s.repo.HumanResponseByStaff(ctx, staffID, start, end)
	p.respAvg, p.respSample, p.respErr = resp.AvgSec, resp.Sample, err
	p.sessions, p.orders, p.convErr = s.repo.SessionOrderCount(ctx, staffID, start, end)
	p.sop, p.sopErr = s.repo.CountSopExecutionsByStaff(ctx, staffID, start, end)
	p.rag, p.ragErr = s.repo.CountRagQueryLogsByUser(ctx, staffID, start, end)
	p.obj, p.objErr = s.repo.CountObjectionRecordsByStaff(ctx, staffID, start, end)
	sat, err := s.repo.SatisfactionByStaff(ctx, staffID, start, end)
	if sat == nil {
		sat = &repository.SatisfactionStats{}
	}
	p.satAvg, p.satCount, p.satErr = sat.Avg, sat.Count, err
	closeStats, err := s.repo.SessionCloseByStaff(ctx, staffID, start, end)
	if closeStats == nil {
		closeStats = &repository.SessionCloseStats{}
	}
	p.closeTotal, p.closeClosed, p.closeErr = closeStats.Total, closeStats.Closed, err
	return p
}

// trendFrom 由当前窗口与上一窗口的归一化分数（0-100，越高越好）得出趋势。
// baselineOK=false（上一窗口无样本或查询失败）时返回 unknown，避免无证据的固定 stable/up。
func trendFrom(cur, prev float64, baselineOK bool) string {
	if !baselineOK {
		return "unknown"
	}
	delta := cur - prev
	if delta > 1 {
		return "up"
	}
	if delta < -1 {
		return "down"
	}
	return "stable"
}

func responseScore(avgSec float64) float64 {
	switch {
	case avgSec > 60:
		return 60
	case avgSec > 30:
		return 80
	case avgSec > 10:
		return 90
	default:
		return 100
	}
}

func cap100(v float64) float64 {
	if v > 100 {
		return 100
	}
	return v
}

// BuildReport 构建销冠能力画像。
// 每个指标：查询失败→Status=error 且不计分；窗口内无样本→Status=no_data 且不计分；
// 有样本→Score 由真实样本计算，Trend 由当前窗口与上一窗口相邻证据对比。
func (s *SalesPersonaService) BuildReport(ctx context.Context, staffID uint) (*PersonaReport, error) {
	s.ensureReposFromDB(ctx)

	now := time.Now()
	curFrom := now.AddDate(0, 0, -personaWindowDays)
	prevFrom := curFrom.AddDate(0, 0, -personaWindowDays)
	cur := s.collectPersonaSample(ctx, staffID, curFrom, now)
	prev := s.collectPersonaSample(ctx, staffID, prevFrom, curFrom)

	report := &PersonaReport{
		StaffID:    staffID,
		Items:      make([]PersonaItem, 0, 7),
		GeneratedAt: now,
		WindowDays: personaWindowDays,
		WindowFrom: curFrom,
		WindowTo:   now,
		Errors:     make([]string, 0),
	}
	addErr := func(tag string, err error) {
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", tag, err))
		}
	}

	// 响应速度：客户消息→该员工首答的真实配对；零响应不再评满分。
	resp := PersonaItem{Tag: "response_speed", Name: "响应速度", Status: "ok", Trend: "unknown"}
	switch {
	case cur.respErr != nil:
		resp.Status, resp.Trend = "error", "unknown"
		addErr("response_speed", cur.respErr)
	case cur.respSample == 0:
		resp.Status = "no_data"
	default:
		resp.Score = responseScore(cur.respAvg)
		resp.Sample = cur.respSample
		if prev.respErr == nil && prev.respSample > 0 {
			resp.Trend = trendFrom(resp.Score, responseScore(prev.respAvg), true)
		}
	}
	report.Items = append(report.Items, resp)

	// 转化能力：订单分子按该员工会话的 account_id 归属，分母为该员工会话数。
	conv := PersonaItem{Tag: "conversion", Name: "转化能力", Status: "ok", Trend: "unknown"}
	switch {
	case cur.convErr != nil:
		conv.Status, conv.Trend = "error", "unknown"
		addErr("conversion", cur.convErr)
	case cur.sessions == 0:
		conv.Status = "no_data"
	default:
		conv.Score = cap100(float64(cur.orders) / float64(cur.sessions) * 100)
		conv.Sample = cur.sessions
		if prev.convErr == nil && prev.sessions > 0 {
			prevScore := cap100(float64(prev.orders) / float64(prev.sessions) * 100)
			conv.Trend = trendFrom(conv.Score, prevScore, true)
		}
	}
	report.Items = append(report.Items, conv)

	// 以下计数型指标：查询成功即 Status=ok（0 是有效活动水平），趋势用相邻窗口分数对比。
	addCountItem := func(tag, name string, curCount int64, curErr error, prevCount int64, prevErr error, factor float64) {
		item := PersonaItem{Tag: tag, Name: name, Status: "ok", Trend: "unknown", Sample: curCount}
		if curErr != nil {
			item.Status, item.Trend, item.Sample = "error", "unknown", 0
			addErr(tag, curErr)
		} else {
			item.Score = cap100(float64(curCount) * factor)
			if prevErr == nil {
				item.Trend = trendFrom(item.Score, cap100(float64(prevCount)*factor), true)
			}
		}
		report.Items = append(report.Items, item)
	}
	addCountItem("sop_usage", "SOP 使用率", cur.sop, cur.sopErr, prev.sop, prev.sopErr, 5)
	addCountItem("knowledge_usage", "知识库调用", cur.rag, cur.ragErr, prev.rag, prev.ragErr, 2)
	addCountItem("objection_handling", "异议处理", cur.obj, cur.objErr, prev.obj, prev.objErr, 3)

	// 客户满意度：有评分样本才计分。
	sat := PersonaItem{Tag: "satisfaction", Name: "客户满意度", Status: "ok", Trend: "unknown"}
	switch {
	case cur.satErr != nil:
		sat.Status, sat.Trend = "error", "unknown"
		addErr("satisfaction", cur.satErr)
	case cur.satCount == 0:
		sat.Status = "no_data"
	default:
		sat.Score = cap100(cur.satAvg / 5.0 * 100)
		sat.Sample = cur.satCount
		if prev.satErr == nil && prev.satCount > 0 {
			sat.Trend = trendFrom(sat.Score, cap100(prev.satAvg/5.0*100), true)
		}
	}
	report.Items = append(report.Items, sat)

	// 跟进及时性（会话关闭率）：有会话样本才计分。
	follow := PersonaItem{Tag: "followup", Name: "跟进及时性", Status: "ok", Trend: "unknown"}
	switch {
	case cur.closeErr != nil:
		follow.Status, follow.Trend = "error", "unknown"
		addErr("followup", cur.closeErr)
	case cur.closeTotal == 0:
		follow.Status = "no_data"
	default:
		follow.Score = cap100(float64(cur.closeClosed) / float64(cur.closeTotal) * 100)
		follow.Sample = cur.closeTotal
		if prev.closeErr == nil && prev.closeTotal > 0 {
			prevScore := cap100(float64(prev.closeClosed) / float64(prev.closeTotal) * 100)
			follow.Trend = trendFrom(follow.Score, prevScore, true)
		}
	}
	report.Items = append(report.Items, follow)

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

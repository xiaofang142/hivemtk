package service

import (
	"context"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	opsrepo "hivemtk-user/internal/ops/repository"
)

// ConversionFunnelService 转化漏斗服务
//
// 这里是转化漏斗的**真实源**：所有数字都是对 customer_events / clues /
// intent_records / customer_sessions / opportunities 的实时聚合。同名的
// `conversion_funnels` 表与之无关，是一张只有 `cmd/seed` 在写、无人读的演示表
// （R-4 收口，判据与下线前提见 `docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md`
// §4.11）。要加阶段请改 `ops/repository.FunnelStageKey` 词表并在此处补一个计数分支，
// **不要往那张表写**。
type ConversionFunnelService struct {
	repo *opsrepo.ConversionFunnelRepository
}

// NewConversionFunnelService 创建转化漏斗服务
func NewConversionFunnelService() *ConversionFunnelService {
	return &ConversionFunnelService{repo: opsrepo.NewConversionFunnelRepository()}
}

// FunnelStage 漏斗阶段
type FunnelStage struct {
	Stage    string  `json:"stage"`
	Name     string  `json:"name"`
	Count    int64   `json:"count"`
	Rate     float64 `json:"rate"`
	DropRate float64 `json:"drop_rate"`
}

// FunnelReport 漏斗报告
type FunnelReport struct {
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	Stages      []FunnelStage `json:"stages"`
	Total       int64         `json:"total"`
	Conversion  float64       `json:"conversion"`
	GeneratedAt time.Time     `json:"generated_at"`
}

// BuildFunnel 构建漏斗（阶段清单以 `ops/repository.LiveFunnelStages()` 为准，
// 当前为 访问→线索→意向→会话→商机 五段）。下面的五个 append 必须与那份词表同序：
// 两边各有一条测试钉住同一个序列（repository 的 _LiveOrderIsTheResponseContract 钉词表，
// service 的 _GoldenContract 钉响应），所以任一侧单独调序也会红 —— 别把它拆成一条。
func (s *ConversionFunnelService) BuildFunnel(startTime, endTime time.Time) (*FunnelReport, error) {
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	ctx := context.Background()
	report := &FunnelReport{
		StartTime:   startTime,
		EndTime:     endTime,
		Stages:      make([]FunnelStage, 0, len(opsrepo.LiveFunnelStages())),
		GeneratedAt: time.Now(),
	}

	// 阶段键与中文名一律取词表（唯一真源），此处不写字面量 —— R-4 之前这里与
	// `conversion_funnels` 演示表各有一套阶段名，那正是"双源"的一部分。
	// 前四个 count 的错误刻意沿用现状不外抛（部分数据源不可用时宁可回一份看着齐全的
	// 报告），该口径的代价已登记为短板 G16，改它属改变现网行为，不在本卡。
	visitCount, _ := s.repo.CountCustomerEventsByTimeRange(ctx, startTime, endTime)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageVisit), Name: opsrepo.StageVisit.Label(), Count: visitCount,
	})

	clueCount, _ := s.repo.CountCluesByUnixTimeRange(ctx, startTime, endTime)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageClue), Name: opsrepo.StageClue.Label(), Count: clueCount,
	})

	intentCount, _ := s.repo.CountIntentRecords(ctx, startTime, endTime, []string{"buy", "purchase", "order", "interested"})
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageIntent), Name: opsrepo.StageIntent.Label(), Count: intentCount,
	})

	sessionCount, _ := s.repo.CountCustomerSessionsByTimeRange(ctx, startTime, endTime)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageSession), Name: opsrepo.StageSession.Label(), Count: sessionCount,
	})

	// 商机段（T-P4-06）是这条口径里**唯一**把取数失败打出来的一段，理由与四段不同：
	// 今日 `opportunities` 只有一个生产者（挖掘链达标那一跳，T-P4-05），所以"0"在这里
	// 有两种完全不同的来路——真没转化出商机，和这张表压根没建起来/接缝没装配上。
	// 四段那些老腿面对的是同一件事，但它们答"0"时没有人会拿它做决策；这一段答 0 会被
	// 读成"我们的线索转化不动"，而那条结论会一路走到 P8 的归因里。
	oppCount, oppErr := s.repo.CountOpportunitiesByTimeRange(ctx, startTime, endTime)
	if oppErr != nil {
		logger.Warnf("[Funnel] 商机段取数失败，本段按 0 上报（0 ≠ 没有商机）: %v", oppErr)
	}
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageOpportunity), Name: opsrepo.StageOpportunity.Label(), Count: oppCount,
	})

	report.Total = visitCount
	if visitCount > 0 {
		report.Conversion = float64(sessionCount) / float64(visitCount) * 100
	}
	for i := range report.Stages {
		if i == 0 {
			report.Stages[i].Rate = 100
			continue
		}
		prev := report.Stages[i-1].Count
		cur := report.Stages[i].Count
		if prev > 0 {
			report.Stages[i].Rate = float64(cur) / float64(prev) * 100
			report.Stages[i].DropRate = 100 - report.Stages[i].Rate
		}
	}

	return report, nil
}

// StageConversion 单阶段转化详情
type StageConversion struct {
	Stage       string       `json:"stage"`
	Name        string       `json:"name"`
	Count       int64        `json:"count"`
	Rate        float64      `json:"rate"`
	AvgDuration float64      `json:"avg_duration_seconds"`
	TopSources  []SourceStat `json:"top_sources"`
}

// SourceStat 来源统计
type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// GetStageDetails 阶段详情
func (s *ConversionFunnelService) GetStageDetails(stage string, startTime, endTime time.Time) (*StageConversion, error) {
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	ctx := context.Background()
	det := &StageConversion{Stage: stage}
	// 未登记的阶段名（演示表那套 exposure/click/…、以及拼错的键）一律保持现状：
	// 返回 200 + 空名字 + 0 计数，不判 404 —— 那是改变现网行为，
	// 且调用方（前端别名 `/conversion-funnel/stage`）当前就在按这个形状渲染。
	switch opsrepo.FunnelStageKey(stage) {
	case opsrepo.StageVisit:
		det.Name = opsrepo.StageVisit.Label()
		count, _ := s.repo.CountCustomerEventsByTimeRange(ctx, startTime, endTime)
		det.Count = count
	case opsrepo.StageClue:
		det.Name = opsrepo.StageClue.Label()
		count, _ := s.repo.CountCluesByUnixTimeRange(ctx, startTime, endTime)
		det.Count = count
		rows, _ := s.repo.GetClueSourceStats(ctx, startTime, endTime)
		for _, r := range rows {
			det.TopSources = append(det.TopSources, SourceStat{Source: r.Source, Count: r.Count})
		}
	case opsrepo.StageIntent:
		det.Name = opsrepo.StageIntent.Label()
		count, _ := s.repo.CountIntentRecords(ctx, startTime, endTime, nil)
		det.Count = count
	case opsrepo.StageSession:
		det.Name = opsrepo.StageSession.Label()
		count, _ := s.repo.CountCustomerSessionsByTimeRange(ctx, startTime, endTime)
		det.Count = count
	case opsrepo.StageOpportunity:
		det.Name = opsrepo.StageOpportunity.Label()
		count, err := s.repo.CountOpportunitiesByTimeRange(ctx, startTime, endTime)
		if err != nil {
			logger.Warnf("[Funnel] 商机详情取数失败，按 0 返回（0 ≠ 没有商机）: %v", err)
		}
		det.Count = count
	}

	return det, nil
}

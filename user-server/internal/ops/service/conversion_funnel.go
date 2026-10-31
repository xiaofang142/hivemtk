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
	// Degraded（D-3 方案①，显式降级不改数字）：任一阶段 count 取数失败 ⇒ true，
	// 且对应阶段记入 StageErrors。数字不因降级被清零——已取到的段原样返回，
	// 失败段按 0 上报但调用方能从 degraded/stage_errors 分辨"0"与"没查到"。
	Degraded bool `json:"degraded"`
	// StageErrors 逐段点名取数失败（stage 键 + 错误摘要）。正常态为空数组。
	StageErrors []StageError `json:"stage_errors,omitempty"`
}

// StageError 单段取数失败记录
type StageError struct {
	Stage string `json:"stage"`
	Error string `json:"error"`
}

// BuildFunnel 构建漏斗（阶段清单以 `ops/repository.LiveFunnelStages()` 为准，
// 当前为 访问→线索→意向→会话→商机 五段）。下面的五个 append 必须与那份词表同序：
// 两边各有一条测试钉住同一个序列（repository 的 _LiveOrderIsTheResponseContract 钉词表，
// service 的 _GoldenContract 钉响应），所以任一侧单独调序也会红 —— 别把它拆成一条。
func (s *ConversionFunnelService) BuildFunnel(startTime, endTime time.Time) (*FunnelReport, error) {
	return s.BuildFunnelCtx(context.Background(), startTime, endTime)
}

// BuildFunnelCtx 与 BuildFunnel 同口径，但用调用方 ctx 取数。
//
// 为什么必须单独开一口：`TENANT_MODE=on` 下租户插件按 ctx 注入 tenant_id，
// 用 `context.Background()` 建的报告会让五个阶段全部 fail-closed —— 响应仍是
// 200，但 degraded=true、五段全 0（用户看到的是"漏斗全空"，不是错误）。
// HTTP 入口一律走本方法并传 `ctx.Request.Context()`；无 ctx 的旧调用（测试/后台）
// 继续走 BuildFunnel，行为不变。
func (s *ConversionFunnelService) BuildFunnelCtx(ctx context.Context, startTime, endTime time.Time) (*FunnelReport, error) {
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	report := &FunnelReport{
		StartTime:   startTime,
		EndTime:     endTime,
		Stages:      make([]FunnelStage, 0, len(opsrepo.LiveFunnelStages())),
		GeneratedAt: time.Now(),
	}

	// 阶段键与中文名一律取词表（唯一真源），此处不写字面量 —— R-4 之前这里与
	// `conversion_funnels` 演示表各有一套阶段名，那正是"双源"的一部分。
	// D-3 方案①（2026-10-23）：五个 count 的取数失败**不再静默**——数字仍按现状
	// 回（失败段按 0 上报、已取到的段原样返回，不因降级清零），但 degraded=true 且
	// stage_errors 逐段点名。旧口径"宁可回一份看着齐全的报告"保留数字行为，
	// 只新增"让读的人知道哪些段没查到"的通道（G16 的两难由显式降级化解）。
	degraded := false
	stageErrors := make([]StageError, 0, 5)
	recordErr := func(stage opsrepo.FunnelStageKey, err error) {
		if err != nil {
			degraded = true
			stageErrors = append(stageErrors, StageError{Stage: string(stage), Error: err.Error()})
			logger.Warnf("[Funnel] %s 段取数失败，本段按 0 上报（0 ≠ 没有数据）: %v", stage.Label(), err)
		}
	}

	visitCount, visitErr := s.repo.CountCustomerEventsByTimeRange(ctx, startTime, endTime)
	recordErr(opsrepo.StageVisit, visitErr)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageVisit), Name: opsrepo.StageVisit.Label(), Count: visitCount,
	})

	clueCount, clueErr := s.repo.CountCluesByUnixTimeRange(ctx, startTime, endTime)
	recordErr(opsrepo.StageClue, clueErr)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageClue), Name: opsrepo.StageClue.Label(), Count: clueCount,
	})

	intentCount, intentErr := s.repo.CountIntentRecords(ctx, startTime, endTime, []string{"buy", "purchase", "order", "interested"})
	recordErr(opsrepo.StageIntent, intentErr)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageIntent), Name: opsrepo.StageIntent.Label(), Count: intentCount,
	})

	sessionCount, sessionErr := s.repo.CountCustomerSessionsByTimeRange(ctx, startTime, endTime)
	recordErr(opsrepo.StageSession, sessionErr)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageSession), Name: opsrepo.StageSession.Label(), Count: sessionCount,
	})

	// 商机段沿用"0 有两种来路"的告警语义（真没转化 / 接缝没装配上），
	// 现并入统一错误收集器：行为不变（Warnf + 按 0 上报），只多一条 degraded 通道。
	oppCount, oppErr := s.repo.CountOpportunitiesByTimeRange(ctx, startTime, endTime)
	recordErr(opsrepo.StageOpportunity, oppErr)
	report.Stages = append(report.Stages, FunnelStage{
		Stage: string(opsrepo.StageOpportunity), Name: opsrepo.StageOpportunity.Label(), Count: oppCount,
	})

	report.Degraded = degraded
	report.StageErrors = stageErrors

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
			r := float64(cur) / float64(prev) * 100
			report.Stages[i].Rate = r
			// D-3：阶段间无单调性 ⇒ 涨幅 >100% 的 rate 是"口径无单调约定"的证据，
			// 不是坏数，照旧返回（NonMonotonicStagesBaseline 钉住这一现状）；
			// 但 drop_rate 为负时不再给出会被读成"负流失"的数——置 0，
			// 语义"该段相对上一段是增长"由 rate>100 自身表达。
			if r <= 100 {
				report.Stages[i].DropRate = 100 - r
			} else {
				report.Stages[i].DropRate = 0
			}
		}
	}

	return report, nil
}

// StageConversion 单阶段转化详情
type StageConversion struct {
	Stage       string  `json:"stage"`
	Name        string  `json:"name"`
	Count       int64   `json:"count"`
	Rate        float64 `json:"rate"`
	AvgDuration float64 `json:"avg_duration_seconds"`
	// Degraded 本段取数失败（失败按 0 上报，与汇总腿的显式降级同口径）；
	// 调用方可据此分辨"真的是 0"与"没查到"。
	Degraded bool `json:"degraded"`
	// AvgDurationAvailable 平均停留时长是否真实可得。当前没有「阶段停留时长」的取数源
	// （事件表只有发生时刻，没有进入/离开配对），一律 false —— 别让调用方把 0 当实测值。
	AvgDurationAvailable bool         `json:"avg_duration_available"`
	TopSources           []SourceStat `json:"top_sources"`
}

// SourceStat 来源统计
type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// GetStageDetails 阶段详情
func (s *ConversionFunnelService) GetStageDetails(stage string, startTime, endTime time.Time) (*StageConversion, error) {
	return s.GetStageDetailsCtx(context.Background(), stage, startTime, endTime)
}

// countStage 按阶段键取计数（与 BuildFunnelCtx 的五个计数分支同源，新增阶段请同步改两处）。
func (s *ConversionFunnelService) countStage(ctx context.Context, stage opsrepo.FunnelStageKey, startTime, endTime time.Time) (int64, error) {
	switch stage {
	case opsrepo.StageVisit:
		return s.repo.CountCustomerEventsByTimeRange(ctx, startTime, endTime)
	case opsrepo.StageClue:
		return s.repo.CountCluesByUnixTimeRange(ctx, startTime, endTime)
	case opsrepo.StageIntent:
		return s.repo.CountIntentRecords(ctx, startTime, endTime, nil)
	case opsrepo.StageSession:
		return s.repo.CountCustomerSessionsByTimeRange(ctx, startTime, endTime)
	case opsrepo.StageOpportunity:
		return s.repo.CountOpportunitiesByTimeRange(ctx, startTime, endTime)
	}
	return 0, nil
}

// GetStageDetailsCtx 与 GetStageDetails 同口径，用调用方 ctx 取数（SaaS 租户隔离）。
//
// 两处行为收紧（此前都是「静默给 0/0.0」）：
//   - 取数失败不再完全无声：保留「按 0 上报」与既有 warn 告警（见
//     conversion_funnel_opportunity_log_test.go 的口径锁），并新增 degraded=true 让调用方分辨；
//   - rate 不再是永不上报的零值，按漏斗同口径算出（首段 100，其余 = 本段/上段 *100）。
func (s *ConversionFunnelService) GetStageDetailsCtx(ctx context.Context, stage string, startTime, endTime time.Time) (*StageConversion, error) {
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	det := &StageConversion{Stage: stage, TopSources: []SourceStat{}}
	key := opsrepo.FunnelStageKey(stage)
	// 未登记的阶段名（演示表那套 exposure/click/…、以及拼错的键）一律保持现状：
	// 返回 200 + 空名字 + 0 计数，不判 404 —— 那是改变现网行为，
	// 且调用方（前端别名 `/conversion-funnel/stage`）当前就在按这个形状渲染。
	stages := opsrepo.LiveFunnelStages()
	idx := -1
	for i, k := range stages {
		if k == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return det, nil
	}

	det.Name = key.Label()
	// 取数失败按 0 上报（口径同汇总腿：0 ≠ 没有数据），但不再静默 ——
	// 一条 warn 日志 + degraded=true，让调用方和运维都能分辨。
	count, err := s.countStage(ctx, key, startTime, endTime)
	if err != nil {
		logger.Warnf("[Funnel] %s详情取数失败，按 0 返回（0 ≠ 没有数据）: %v", key.Label(), err)
		det.Degraded = true
		count = 0
	}
	det.Count = count

	if idx == 0 {
		det.Rate = 100
	} else if prev, prevErr := s.countStage(ctx, stages[idx-1], startTime, endTime); prevErr != nil {
		logger.Warnf("[Funnel] %s 取数失败，%s 段转化率按 0 上报（0 ≠ 没有数据）: %v", stages[idx-1].Label(), key.Label(), prevErr)
		det.Degraded = true
	} else if prev > 0 {
		det.Rate = float64(count) / float64(prev) * 100
	}

	if key == opsrepo.StageClue {
		rows, err := s.repo.GetClueSourceStats(ctx, startTime, endTime)
		if err != nil {
			logger.Warnf("[Funnel] 线索来源统计失败，按空列表返回（0 ≠ 没有数据）: %v", err)
			det.Degraded = true
			rows = nil
		}
		for _, r := range rows {
			det.TopSources = append(det.TopSources, SourceStat{Source: r.Source, Count: r.Count})
		}
	}

	return det, nil
}

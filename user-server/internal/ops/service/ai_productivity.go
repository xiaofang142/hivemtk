package service

import (
	"context"
	"time"

	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/reporting"
)

// AIProductivityService AI 产能分析服务
type AIProductivityService struct {
	repo *opsrepo.AIProductivityRepository
}

// NewAIProductivityService 创建服务
func NewAIProductivityService() *AIProductivityService {
	return &AIProductivityService{repo: opsrepo.NewAIProductivityRepository()}
}

// 数据可用性标识：任一查询失败都不能把读到的零值伪装成真实零值。
const (
	ProductivityDataOK          = "ok"
	ProductivityDataPartial     = "partial"
	ProductivityDataUnavailable = "unavailable"
)

// ProductivityReport AI 产能报告
//
// 发送者身份采用一套规范集合（见 opsrepo.Productivity*Senders），与真实收件箱
// （inbox_reply.go 写 agent）、老转人工（human/staff）和 AI（ai）回复一致；入站
// 两种入口（customer/user）单独计入 InboundMessages。用量成本只取权威事实
// llm_routing_logs 一套，不与 platform_llm_usage_records 相加，避免双计。
// DataStatus/Errors 让故障显式化：查询失败时数值不伪装成真实零值。
type ProductivityReport struct {
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	TotalConversations int64     `json:"total_conversations"`
	InboundMessages    int64     `json:"inbound_messages"`
	AIReplies          int64     `json:"ai_replies"`
	HumanReplies       int64     `json:"human_replies"`
	AIRatio            float64   `json:"ai_ratio"`
	AvgResponseTime    float64   `json:"avg_response_time"`
	ConversionRate     float64   `json:"conversion_rate"`
	TotalConversions   int64     `json:"total_conversions"`
	LLMCalls           int64     `json:"llm_calls"`
	LLMTokens          int64     `json:"llm_tokens"`
	LLMCost            float64   `json:"llm_cost"`
	DataStatus         string    `json:"data_status"`
	Errors             []string  `json:"errors,omitempty"`
	GeneratedAt        time.Time `json:"generated_at"`
}

// dataStatus 汇总查询错误：全成功 ok、部分成功 partial、全失败 unavailable。
func productivityDataStatus(total, failed int) string {
	if failed == 0 {
		return ProductivityDataOK
	}
	if failed >= total {
		return ProductivityDataUnavailable
	}
	return ProductivityDataPartial
}

// BuildReport 构建产能报告。
//
// ctx 必须来自请求（`ctx.Request.Context()`）：SaaS 形态下租户插件按 ctx 注入
// tenant_id，用 context.Background() 会查不到任何行，同一份数据在独立部署有数、
// 在 SaaS 恒为 0。仓储层已按 ctx 取数，这里不再另造背景上下文。
func (s *AIProductivityService) BuildReport(ctx context.Context, startTime, endTime time.Time) (*ProductivityReport, error) {
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	rep := &ProductivityReport{
		StartTime:   startTime,
		EndTime:     endTime,
		DataStatus:  ProductivityDataOK,
		Errors:      make([]string, 0),
		GeneratedAt: time.Now(),
	}

	const queries = 7
	failed := 0
	collectErr := func(field string, err error) {
		if err != nil {
			failed++
			rep.Errors = append(rep.Errors, field+": "+err.Error())
		}
	}

	var err error
	rep.TotalConversations, err = s.repo.CountCustomerSessionsByTimeRange(ctx, startTime, endTime)
	collectErr("total_conversations", err)

	rep.InboundMessages, err = s.repo.CountInboundMessages(ctx, startTime, endTime)
	collectErr("inbound_messages", err)

	rep.AIReplies, err = s.repo.CountSessionMessagesBySenderTypes(ctx, startTime, endTime, opsrepo.ProductivityAISenders)
	collectErr("ai_replies", err)

	rep.HumanReplies, err = s.repo.CountSessionMessagesBySenderTypes(ctx, startTime, endTime, opsrepo.ProductivityHumanSenders)
	collectErr("human_replies", err)

	totalReplies := rep.AIReplies + rep.HumanReplies
	if totalReplies > 0 {
		rep.AIRatio = float64(rep.AIReplies) / float64(totalReplies) * 100
	}

	rep.AvgResponseTime, err = s.repo.GetAvgResponseTime(ctx, startTime, endTime)
	collectErr("avg_response_time", err)

	rep.TotalConversions, err = s.repo.CountOrdersByUnixTimeRangeAndStatus(ctx, startTime, endTime, 100)
	collectErr("total_conversions", err)
	if rep.TotalConversations > 0 {
		rep.ConversionRate = float64(rep.TotalConversions) / float64(rep.TotalConversations) * 100
	}

	usage, err := s.repo.GetLLMUsageStats(ctx, startTime, endTime)
	collectErr("llm_usage", err)
	rep.LLMCalls = usage.Calls
	rep.LLMTokens = usage.Tokens
	rep.LLMCost = usage.Cost

	rep.DataStatus = productivityDataStatus(queries, failed)
	return rep, nil
}

// SalesPersonaItem 销冠能力画像项
type SalesPersonaItem struct {
	Tag    string  `json:"tag"`
	Score  float64 `json:"score"`
	Sample int64   `json:"sample"`
}

// DailyMetric 每日指标（按业务时区自然日 [00:00, 次日00:00) 聚合）
type DailyMetric struct {
	Date          string   `json:"date"`
	Conversations int64    `json:"conversations"`
	AIReplies     int64    `json:"ai_replies"`
	Conversions   int64    `json:"conversions"`
	LLMCost       float64  `json:"llm_cost"`
	DataStatus    string   `json:"data_status"`
	Errors        []string `json:"errors,omitempty"`
}

// DailyTrend 日趋势
//
// 逐日窗口按租户业务时区（timeutil.StartOfBusinessDay）取自然日，不用"当前时分秒
// 切 24 小时"再标自然日期；跨午夜的合成数据据此归入各自自然日。任一查询失败
// 都记入 DataStatus/Errors，不把失败读到的零值伪装成真实零点。
func (s *AIProductivityService) DailyTrend(ctx context.Context, days int) ([]DailyMetric, error) {
	if days <= 0 || days > 90 {
		days = reporting.DefaultWindowDays(ctx)
	}
	todayStart := timeutil.StartOfBusinessDay(time.Now())
	trend := make([]DailyMetric, 0, days)

	for i := 0; i < days; i++ {
		// 从 days-1 天前到今天，按自然日顺序。
		day := todayStart.AddDate(0, 0, -(days - 1 - i))
		dayEnd := day.AddDate(0, 0, 1)
		m := DailyMetric{Date: timeutil.BusinessDate(day), DataStatus: ProductivityDataOK, Errors: make([]string, 0)}

		const queries = 4
		failed := 0
		collectErr := func(field string, err error) {
			if err != nil {
				failed++
				m.Errors = append(m.Errors, field+": "+err.Error())
			}
		}

		var err error
		m.Conversations, err = s.repo.CountCustomerSessionsByDayRange(ctx, day, dayEnd)
		collectErr("conversations", err)

		m.AIReplies, err = s.repo.CountSessionMessagesBySenderTypesAndDayRange(ctx, day, dayEnd, opsrepo.ProductivityAISenders)
		collectErr("ai_replies", err)

		m.Conversions, err = s.repo.CountOrdersByUnixDayRangeAndStatus(ctx, day, dayEnd, 100)
		collectErr("conversions", err)

		m.LLMCost, err = s.repo.GetLLMUsageCostSum(ctx, day, dayEnd)
		collectErr("llm_cost", err)

		m.DataStatus = productivityDataStatus(queries, failed)
		trend = append(trend, m)
	}
	return trend, nil
}

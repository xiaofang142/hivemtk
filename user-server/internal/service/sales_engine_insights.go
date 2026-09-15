package service

import (
	"context"
	"strings"

	tracelearning "hivemtk-user/internal/service/trace_learning"
)

const salesInsightLimit = 3

var insightIndustryFn func() string

// SetLearningInsightIndustryProvider 注入行业标签解析器（main.go 装配时调用）
func SetLearningInsightIndustryProvider(fn func() string) {
	insightIndustryFn = fn
}

func (e *SalesEngine) appendLearningInsights(sb *strings.Builder) {
	if e.sessionMsgRepo == nil || e.sessionMsgRepo.GetDB() == nil || insightIndustryFn == nil {
		return
	}
	industry := insightIndustryFn()
	if industry == "" {
		return
	}
	insights, err := tracelearning.TopInsights(context.Background(), e.sessionMsgRepo.GetDB(), industry, salesInsightLimit)
	if err != nil || len(insights) == 0 {
		return
	}
	sb.WriteString("\n【历史错误模式（回复时务必避免）】:\n")
	for _, t := range insights {
		sb.WriteString("- ")
		sb.WriteString(t)
		sb.WriteString("\n")
	}
}

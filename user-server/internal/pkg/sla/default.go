package sla

import (
	"log"
	"sync"
	"time"
)

// 本仓当前登记的三条 SLI。放在 sla 包而不是 app 装配层，是因为
// 记录点散落在 service 层（编排器、人工待办），而 service 不能反向 import app
// （app 装配 service，会成环）。名称与定义同处一包，读写两侧只认这一份。
const (
	SLOInboundHandled      = "inbound_message_handled"
	SLOAIAutoReplyCoverage = "ai_auto_reply_coverage"
	SLOHumanFirstResponse  = "human_first_response_sla"
)

var (
	defaultOnce    sync.Once
	defaultTracker *SLOTracker
)

// Default 返回进程级默认 SLO 跟踪器（懒初始化并登记三条 SLI）。
// 幂等：多次调用返回同一实例。
func Default() *SLOTracker {
	defaultOnce.Do(func() {
		InitMetrics()
		t := NewSLOTracker()
		t.Define(SLO{
			Name:        SLOInboundHandled,
			Service:     "smart_cs",
			SLITarget:   0.99,
			Window:      30 * 24 * time.Hour,
			Description: "入站消息处理成功率",
		})
		t.Define(SLO{
			Name:        SLOAIAutoReplyCoverage,
			Service:     "smart_cs",
			SLITarget:   0.60,
			Window:      30 * 24 * time.Hour,
			Description: "AI 自动回复覆盖率",
		})
		t.Define(SLO{
			Name:        SLOHumanFirstResponse,
			Service:     "human_task",
			SLITarget:   0.95,
			Window:      30 * 24 * time.Hour,
			Description: "人工首响 SLA 达成率",
		})
		t.OnBreach(func(s SLO, state SLOState) {
			log.Printf("[SLO-BREACH] %s: achievement=%.4f target=%.4f budget_used=%.4f",
				s.Name, state.Achievement, s.SLITarget, state.BudgetUsed)
		})
		defaultTracker = t
	})
	return defaultTracker
}

// Record 在默认跟踪器上记录一个 SLO 事件。
func Record(name string, success bool) {
	Default().Record(name, success)
}

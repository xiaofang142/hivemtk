package service

import (
	"context"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"
)

// faAN01Setup 建测试库并把全局 DB 句柄指向它；返回 *gorm.DB 供夹具写入。
func faAN01Setup(t *testing.T) {
	database := testutil.NewTestDB(t,
		&sysmodel.CustomerSession{},
		&sysmodel.SessionMessage{},
		&sysmodel.Order{},
		&sysmodel.LLMRoutingLog{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
}

// 两种入站（customer/user）、人工（agent/staff/human）与 AI（ai）回复按规范集合计数；
// 权威用量只取 llm_routing_logs 一套（自带 Key 与平台模型都在此记 cost），不与平台账本双计。
func TestFAAN01_CanonicalSendersAndAuthoritativeUsage(t *testing.T) {
	faAN01Setup(t)
	database := db.GetDB()
	base := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	start := base.Add(-time.Hour)
	end := base.Add(time.Hour)

	msgs := []sysmodel.SessionMessage{
		{SessionID: "s1", SenderType: "customer", CreatedAt: base},
		{SessionID: "s2", SenderType: "user", CreatedAt: base},
		{SessionID: "s1", SenderType: "ai", CreatedAt: base},
		{SessionID: "s1", SenderType: "agent", CreatedAt: base},
		{SessionID: "s2", SenderType: "staff", CreatedAt: base},
		{SessionID: "s2", SenderType: "human", CreatedAt: base},
	}
	for i := range msgs {
		if err := database.Create(&msgs[i]).Error; err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	// 自带 Key 一条 + 平台模型一条，成本都记在 llm_routing_logs（平台账本不在本测试库，
	// 故天然不双计）；明细与总数由 BuildReport 的 LLMCalls/LLMTokens/LLMCost 核对。
	logs := []sysmodel.LLMRoutingLog{
		{TraceID: "t1", Scenario: "self", Provider: "openai", Model: "gpt-4o", TotalTokens: 100, Cost: 0.5, CreatedAt: base, Source: "dispatch"},
		{TraceID: "t2", Scenario: "platform", Provider: "deepseek", Model: "ds-v3", TotalTokens: 200, Cost: 1.25, CreatedAt: base, Source: "platform"},
	}
	for i := range logs {
		if err := database.Create(&logs[i]).Error; err != nil {
			t.Fatalf("seed usage: %v", err)
		}
	}

	svc := NewAIProductivityService()
	rep, err := svc.BuildReport(context.Background(), start, end)
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if rep.InboundMessages != 2 {
		t.Fatalf("两种入站应计 2, got %d", rep.InboundMessages)
	}
	if rep.AIReplies != 1 {
		t.Fatalf("AI 回复应计 1, got %d", rep.AIReplies)
	}
	if rep.HumanReplies != 3 {
		t.Fatalf("人工回复 agent/staff/human 应计 3, got %d", rep.HumanReplies)
	}
	if rep.LLMCalls != 2 || rep.LLMTokens != 300 {
		t.Fatalf("用量明细 calls/tokens 应为 2/300, got %d/%d", rep.LLMCalls, rep.LLMTokens)
	}
	if rep.LLMCost < 1.749 || rep.LLMCost > 1.751 {
		t.Fatalf("用量总成本应≈1.75（只取一套不双计）, got %f", rep.LLMCost)
	}
	if rep.DataStatus != ProductivityDataOK || len(rep.Errors) != 0 {
		t.Fatalf("无故障应为 ok, got %s %v", rep.DataStatus, rep.Errors)
	}
	_ = repository.ProductivityHumanSenders // 规范集合引用防误删
}

// DailyTrend 按业务时区自然日聚合：跨午夜数据各归其日，Date 为自然日串而非"当前时分秒"切窗。
func TestFAAN01_NaturalDayAcrossMidnight(t *testing.T) {
	faAN01Setup(t)
	database := db.GetDB()

	// 业务时区（Asia/Shanghai）昨天 23:30 与今天 00:30，恰好落在 DailyTrend(2) 窗口内。
	today := timeutil.StartOfBusinessDay(time.Now())
	yesterday := today.AddDate(0, 0, -1)
	lateA := yesterday.Add(23*time.Hour + 30*time.Minute)
	earlyB := today.Add(30 * time.Minute)

	msgs := []sysmodel.SessionMessage{
		{SessionID: "late", SenderType: "ai", CreatedAt: lateA},
		{SessionID: "early", SenderType: "ai", CreatedAt: earlyB},
	}
	for i := range msgs {
		if err := database.Create(&msgs[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	logs := []sysmodel.LLMRoutingLog{
		{TraceID: "late", Scenario: "s", Provider: "p", Model: "m", TotalTokens: 10, Cost: 0.1, CreatedAt: lateA},
		{TraceID: "early", Scenario: "s", Provider: "p", Model: "m", TotalTokens: 20, Cost: 0.2, CreatedAt: earlyB},
	}
	for i := range logs {
		if err := database.Create(&logs[i]).Error; err != nil {
			t.Fatalf("seed usage: %v", err)
		}
	}

	svc := NewAIProductivityService()
	trend, err := svc.DailyTrend(context.Background(), 2)
	if err != nil {
		t.Fatalf("DailyTrend: %v", err)
	}
	if len(trend) != 2 {
		t.Fatalf("应返回 2 天, got %d", len(trend))
	}
	// days=2 从昨天到今天；索引 0=昨天（含跨午夜前的 23:30），索引 1=今天（含 00:30）。
	if trend[0].Date != timeutil.BusinessDate(yesterday) || trend[0].AIReplies != 1 {
		t.Fatalf("昨天自然日应含 1 条 AI 回复, got %+v", trend[0])
	}
	if trend[1].Date != timeutil.BusinessDate(today) || trend[1].AIReplies != 1 {
		t.Fatalf("今天自然日应含 1 条 AI 回复, got %+v", trend[1])
	}
	if trend[0].DataStatus != ProductivityDataOK || trend[1].DataStatus != ProductivityDataOK {
		t.Fatalf("无故障应 ok, got %+v", trend)
	}
	// 无论是否在窗口内，Date 必须是规范自然日串（非带时分秒）。
	for _, m := range trend {
		if _, err := time.Parse("2006-01-02", m.Date); err != nil {
			t.Fatalf("Date 必须是自然日串, got %q", m.Date)
		}
	}
}

// 查询失败（cancel ctx）不得把读到的零值伪装成真实零值：DataStatus 非 ok 且 Errors 非空。
func TestFAAN01_QueryFailureNeverShowsRealZero(t *testing.T) {
	faAN01Setup(t)
	svc := NewAIProductivityService()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rep, err := svc.BuildReport(ctx, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("BuildReport 不应返回 error（故障走 DataStatus）: %v", err)
	}
	if rep.DataStatus != ProductivityDataUnavailable && rep.DataStatus != ProductivityDataPartial {
		t.Fatalf("查询失败 DataStatus 应标记不可用/部分, got %s", rep.DataStatus)
	}
	if len(rep.Errors) == 0 {
		t.Fatal("查询失败必须留错误证据")
	}

	trend, err := svc.DailyTrend(ctx, 1)
	if err != nil {
		t.Fatalf("DailyTrend 不应返回 error: %v", err)
	}
	if len(trend) != 1 || (trend[0].DataStatus != ProductivityDataUnavailable && trend[0].DataStatus != ProductivityDataPartial) {
		t.Fatalf("趋势查询失败也应标记, got %+v", trend)
	}
}

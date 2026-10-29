// observability_dashboard_test.go specs/007：统一看板口径的**纯判定**用例。
//
// 判据全部落在「分母怎么取、状态怎么判」这条主线上，不连库：
//
//	· 送达率/在线率/转化率分母为 0 必须回 0（不是 NaN——看板把 NaN 渲染出来
//	  只会让运营以为接口坏了）；
//	· 渠道状态判定顺序：未接入不算异常、选择器全失效（封禁/改版）必须最先判 banned、
//	  全离线独立一档、局部失效降级；
//	· window 参数只收 24h/7d/30d，非法回落 7d；
//	· Snapshot 用 fake 仓储验证「六个读侧都聚合进一份快照、字段不串位」。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// fakeDashRepo 内存仓储：本组用例只判口径聚合与判定，不判 SQL 落地
// （SQL 落地属于 repository 真库用例，测试库被并发 lane 占用，本轮不跑）。
type fakeDashRepo struct {
	costRows     []repository.ObsDashLLMCostRow
	deliveryRows []repository.ObsDashDeliveryRow
	funnelRows   []repository.ObsDashFunnelStageRow
	accountRows  []repository.ObsDashChannelAccountRow
	selectorRows []model.BridgeSelectorHealth
	domainBan    repository.ObsDashDomainBanSummaryRow
	err          error
}

func (f *fakeDashRepo) LLMCost(ctx context.Context, since time.Time) ([]repository.ObsDashLLMCostRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.costRows, nil
}

func (f *fakeDashRepo) Delivery(ctx context.Context, since time.Time) ([]repository.ObsDashDeliveryRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.deliveryRows, nil
}

func (f *fakeDashRepo) Funnel(ctx context.Context, since time.Time) ([]repository.ObsDashFunnelStageRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.funnelRows, nil
}

func (f *fakeDashRepo) ChannelAccounts(ctx context.Context, graceSeconds int) ([]repository.ObsDashChannelAccountRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.accountRows, nil
}

func (f *fakeDashRepo) SelectorHealthRows(ctx context.Context) ([]model.BridgeSelectorHealth, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.selectorRows, nil
}

func (f *fakeDashRepo) DomainBanSummary(ctx context.Context) (repository.ObsDashDomainBanSummaryRow, error) {
	if f.err != nil {
		return repository.ObsDashDomainBanSummaryRow{}, f.err
	}
	return f.domainBan, nil
}

func newDashSvc() (*ObservabilityDashboardService, *fakeDashRepo) {
	repo := &fakeDashRepo{}
	svc := NewObservabilityDashboardService(repo)
	svc.now = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
	return svc, repo
}

func TestDeliveryRate_ZeroDenominatorIsZeroNotNaN(t *testing.T) {
	tests := []struct {
		name      string
		delivered int64
		failed    int64
		want      float64
	}{
		{"无已了结行 ⇒ 0", 0, 0, 0},
		{"全送达 ⇒ 100", 100, 0, 100},
		{"全失败 ⇒ 0", 0, 100, 0},
		{"50% ⇒ 50", 50, 50, 50},
		{"三分之二 ⇒ 66.7", 2, 1, 66.7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeliveryRate(tt.delivered, tt.failed); got != tt.want {
				t.Fatalf("DeliveryRate(%d,%d) = %v, want %v", tt.delivered, tt.failed, got, tt.want)
			}
		})
	}
}

func TestOnlineRate_ZeroAccountsIsZero(t *testing.T) {
	if got := OnlineRate(0, 0); got != 0 {
		t.Fatalf("0 账号在线率应为 0，实际 %v", got)
	}
	if got := OnlineRate(3, 4); got != 75 {
		t.Fatalf("3/4 在线率应为 75，实际 %v", got)
	}
}

func TestFunnelConversion_ZeroPrevIsZero(t *testing.T) {
	if got := FunnelConversion(0, 10); got != 0 {
		t.Fatalf("上一阶段为 0 时转化率应为 0，实际 %v", got)
	}
	if got := FunnelConversion(200, 150); got != 75 {
		t.Fatalf("150/200 转化率应为 75，实际 %v", got)
	}
}

func TestClassifyChannelHealth_OrderMatters(t *testing.T) {
	tests := []struct {
		name          string
		accounts      int64
		online        int64
		worstSelector string
		brokenFields  int
		want          string
	}{
		{"未接入渠道 ⇒ empty（不算异常）", 0, 0, "none", 0, ChannelHealthStatusEmpty},
		{"选择器全失效 ⇒ banned（平台改版/封禁强信号）", 5, 5, SelectorHealthStatusBroken, 2, ChannelHealthStatusBanned},
		{"全离线 ⇒ offline", 3, 0, SelectorHealthStatusOK, 0, ChannelHealthStatusOffline},
		{"选择器部分失效 ⇒ degraded", 3, 3, SelectorHealthStatusDegraded, 0, ChannelHealthStatusDegraded},
		{"存在 broken 字段 ⇒ degraded", 3, 3, SelectorHealthStatusOK, 1, ChannelHealthStatusDegraded},
		{"在线 + 选择器健康 ⇒ healthy", 3, 3, SelectorHealthStatusOK, 0, ChannelHealthStatusHealthy},
		{"在线 + 样本不足 ⇒ healthy（warming 不是问题）", 3, 3, SelectorHealthStatusWarming, 0, ChannelHealthStatusHealthy},
		{"无选择器读数 + 全在线 ⇒ healthy（missing 不构成信号）", 3, 3, "none", 0, ChannelHealthStatusHealthy},
		{"在线率低于阈值 ⇒ degraded（007 §3.6 补上的那一档：3 掉 1 就是 66.7%）", 3, 2, "none", 0, ChannelHealthStatusDegraded},
		{"在线率刚好等于阈值 ⇒ healthy（判据是「低于」不是「不高于」）", 10, 9, "none", 0, ChannelHealthStatusHealthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyChannelHealth(tt.accounts, tt.online, tt.worstSelector, tt.brokenFields); got != tt.want {
				t.Fatalf("ClassifyChannelHealth(%d,%d,%s,%d) = %s, want %s",
					tt.accounts, tt.online, tt.worstSelector, tt.brokenFields, got, tt.want)
			}
		})
	}
}

// 阈值来自参数中心，所以要能按任意阈值判（并要求非法阈值不改变判定）
func TestClassifyChannelHealthWithThreshold(t *testing.T) {
	cases := []struct {
		name      string
		accounts  int64
		online    int64
		threshold int
		want      string
	}{
		{"75% 在线 + 阈值 70 ⇒ healthy", 4, 3, 70, ChannelHealthStatusHealthy},
		{"75% 在线 + 阈值 80 ⇒ degraded", 4, 3, 80, ChannelHealthStatusDegraded},
		{"阈值 0（非法）⇒ 不看在线率档", 4, 3, 0, ChannelHealthStatusHealthy},
		{"阈值 101（非法）⇒ 不看在线率档", 4, 3, 101, ChannelHealthStatusHealthy},
		{"全离线优先于阈值（offline 而非 degraded）", 4, 0, 90, ChannelHealthStatusOffline},
		{"选择器 broken 优先于阈值（banned 而非 degraded）", 4, 3, 100, ChannelHealthStatusBanned},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyChannelHealthWithThreshold(c.accounts, c.online, "none", 0, c.threshold)
			if c.name == "选择器 broken 优先于阈值（banned 而非 degraded）" {
				got = ClassifyChannelHealthWithThreshold(c.accounts, c.online, SelectorHealthStatusBroken, 0, c.threshold)
			}
			if got != c.want {
				t.Fatalf("got=%s want=%s", got, c.want)
			}
		})
	}
}

// 默认窗口来自参数中心：展示串要跟着兜底值走（配 48h 就显示 2d，而不是永远 7d）
func TestNormalizeWindow_FollowsConfiguredDefault(t *testing.T) {
	if got := normalizeWindow("", 48*time.Hour); got != "2d" {
		t.Fatalf("48h 兜底应显示 2d，got %q", got)
	}
	if got := normalizeWindow("bogus", 36*time.Hour); got != "36h" {
		t.Fatalf("不能被 24 整除时应显示小时，got %q", got)
	}
	if got := normalizeWindow("24h", 168*time.Hour); got != "24h" {
		t.Fatalf("合法入参应原样展示，got %q", got)
	}
}

func TestWindowDuration_FallsBackTo7d(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"", DashboardDefaultWindow},
		{"1d", DashboardDefaultWindow},
		{"7D", DashboardDefaultWindow},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := windowDuration(tt.in, DashboardDefaultWindow); got != tt.want {
				t.Fatalf("windowDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSnapshot_AggregatesAllSections(t *testing.T) {
	svc, repo := newDashSvc()
	repo.costRows = []repository.ObsDashLLMCostRow{
		{Provider: "openai", Scenario: "reply", Calls: 10, SuccessCalls: 9,
			PromptTokens: 100, CompletionTokens: 50, Cost: 1.25, LatencyMs: 3000},
		{Provider: "local", Scenario: "reply", Calls: 2, SuccessCalls: 2,
			PromptTokens: 20, CompletionTokens: 10, Cost: 0, LatencyMs: 1000},
	}
	repo.deliveryRows = []repository.ObsDashDeliveryRow{
		{Channel: "douyin", Delivered: 80, Failed: 20, Inflight: 5},
		{Channel: "xhs", Delivered: 0, Failed: 0, Inflight: 0},
	}
	repo.funnelRows = []repository.ObsDashFunnelStageRow{
		{Node: "ingest", Cnt: 100},
		{Node: "ai_dispatch", Cnt: 90},
		{Node: "delivered_ack", Cnt: 60},
	}
	repo.accountRows = []repository.ObsDashChannelAccountRow{
		{Channel: "douyin", Accounts: 10, Online: 8},
		{Channel: "xhs", Accounts: 0, Online: 0},
	}
	repo.selectorRows = []model.BridgeSelectorHealth{
		{Channel: "douyin", Field: "messageItem", Attempts: 500, Hits: 0}, // broken
	}
	repo.domainBan = repository.ObsDashDomainBanSummaryRow{Blacklisted: 2, Total: 5}

	snap, err := svc.Snapshot(context.Background(), "7d")
	if err != nil {
		t.Fatalf("Snapshot 报错：%v", err)
	}
	if snap.Window != "7d" {
		t.Fatalf("window 应为 7d，实际 %s", snap.Window)
	}

	// AI 成本：跨 provider 累加，token 与成本不串位。
	if snap.AICost.Calls != 12 || snap.AICost.Cost != 1.25 {
		t.Fatalf("成本聚合错：calls=%d cost=%v", snap.AICost.Calls, snap.AICost.Cost)
	}
	if snap.AICost.TotalTokens != 180 {
		t.Fatalf("总 token 应为 180，实际 %d", snap.AICost.TotalTokens)
	}
	if snap.AICost.AvgLatencyMs != 333 { // (3000+1000)/12 = 333
		t.Fatalf("平均延迟应为 333，实际 %d", snap.AICost.AvgLatencyMs)
	}
	if len(snap.AICost.ByProvider) != 2 || len(snap.AICost.ByScenario) != 1 {
		t.Fatalf("分组数错：provider=%d scenario=%d",
			len(snap.AICost.ByProvider), len(snap.AICost.ByScenario))
	}

	// 送达率：全局 = (80+0)/(80+20+0) = 80%。
	if snap.Delivery.Rate != 80 || snap.Delivery.Delivered != 80 || snap.Delivery.Failed != 20 {
		t.Fatalf("送达聚合错：%+v", snap.Delivery)
	}
	if snap.Delivery.ByChannel[1].Rate != 0 {
		t.Fatalf("无已了结行的渠道送达率应为 0，实际 %v", snap.Delivery.ByChannel[1].Rate)
	}

	// 漏斗：六阶段顺序固定，缺数据的阶段补 0，转化率按相邻阶段算。
	if len(snap.Funnel.Stages) != 6 || snap.Funnel.Stages[0].Node != "ingest" {
		t.Fatalf("漏斗阶段数/顺序错：%+v", snap.Funnel.Stages)
	}
	if snap.Funnel.Stages[1].Count != 90 || snap.Funnel.Stages[1].Conversion != 90 {
		t.Fatalf("ai_dispatch 应为 90/90%%，实际 %+v", snap.Funnel.Stages[1])
	}
	if snap.Funnel.Stages[2].Conversion != 0 {
		t.Fatalf("outbound_enqueue 无数据转化率应为 0，实际 %v", snap.Funnel.Stages[2].Conversion)
	}

	// 渠道健康：douyin 选择器全失效 → banned；xhs 未接入 → empty。
	ch := snap.ChannelHealth.Channels
	if len(ch) != 2 {
		t.Fatalf("渠道数应为 2，实际 %d", len(ch))
	}
	if ch[0].Status != ChannelHealthStatusBanned || ch[0].OnlineRate != 80 {
		t.Fatalf("douyin 应 banned/80%%，实际 %+v", ch[0])
	}
	if ch[1].Status != ChannelHealthStatusEmpty {
		t.Fatalf("xhs 未接入应 empty，实际 %+v", ch[1])
	}
	if snap.ChannelHealth.DomainBanned != 2 || snap.ChannelHealth.DomainTotal != 5 {
		t.Fatalf("域名封禁聚合错：%+v", snap.ChannelHealth)
	}
}

func TestSnapshot_NilRepoIsExplicitErrorNotPanic(t *testing.T) {
	svc := NewObservabilityDashboardService(nil)
	if _, err := svc.Snapshot(context.Background(), "7d"); !errors.Is(err, ErrDashboardRepoNotReady) {
		t.Fatalf("仓储未注入应报 ErrDashboardRepoNotReady，实际 %v", err)
	}
}

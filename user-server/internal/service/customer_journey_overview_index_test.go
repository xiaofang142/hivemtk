// customer_journey_overview_index_test.go 总览/漏斗读数的跨实例可见性。
//
// 立起来的不变量：
//   - 总览与漏斗的客户数来自 Redis 权威态（经阶段索引入口），不是本实例的 L1 ——
//     一份实例写的旅程，另一份实例（或重启后的自己）读得到；
//   - 同一客户只算一次：索引只增不删，换了阶段的旧条目不能再算一个人；
//   - 零值 StageSince 不把平均停留算成天文数字。
//
// 触发这条修复的是 UI 验证 F14：库内 169 个 customers，`/customerJourney/dashboard`
// 客户总数读成 0 —— 旧实现只扫 s.states，而那是每实例 60s 的读缓存。

package service

import (
	"context"
	"math"
	"testing"
)

func TestCustomerJourney_GetOverviewSeesWritesFromAnotherInstance(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	writer := NewCustomerJourneyServiceWithCache(c)
	reader := NewCustomerJourneyServiceWithCache(c)

	if _, err := writer.Transition(ctx, "cust_overview", StageInterested,
		"ai", "ai", "约了演示", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}

	// 前置：读侧 L1 必须是空的，否则测不到跨实例这条腿。
	if _, ok := reader.getLiveL1("cust_overview"); ok {
		t.Fatal("前置不成立：读侧 L1 里已经有这条状态")
	}

	overview := reader.GetOverview(ctx)
	if overview.TotalCustomers != 1 {
		t.Fatalf("另一份实例写的客户没被总览数出来: total=%d，期望 1", overview.TotalCustomers)
	}

	var interested int
	for _, st := range overview.Stages {
		if st.Stage == StageInterested {
			interested = st.Count
		}
	}
	if interested != 1 {
		t.Errorf("interested 阶段计数=%d，期望 1", interested)
	}
}

func TestCustomerJourney_GetOverviewCountsCustomerOnceAfterStageMove(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	a := NewCustomerJourneyServiceWithCache(c)
	b := NewCustomerJourneyServiceWithCache(c)

	if _, err := a.Transition(ctx, "cust_move", StageLead, "ai", "ai", "留资", nil); err != nil {
		t.Fatalf("transition to lead: %v", err)
	}
	if _, err := b.Transition(ctx, "cust_move", StageQuoted, "sales", "sales_1", "已报价", nil); err != nil {
		t.Fatalf("transition to quoted: %v", err)
	}

	reader := NewCustomerJourneyServiceWithCache(c)
	overview := reader.GetOverview(ctx)
	if overview.TotalCustomers != 1 {
		t.Fatalf("换阶段后同一客户被重复计数: total=%d，期望 1", overview.TotalCustomers)
	}
	for _, st := range overview.Stages {
		want := 0
		if st.Stage == StageQuoted {
			want = 1
		}
		if st.Count != want {
			t.Errorf("阶段 %s 计数=%d，期望 %d（旧阶段索引条目必须被权威态筛掉）", st.Stage, st.Count, want)
		}
	}
}

func TestCustomerJourney_GetOverviewIgnoresZeroStageSince(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	svc := NewCustomerJourneyServiceWithCache(c)

	// 手工落一条 StageSince 为零值的历史状态：零值不是"从公元元年开始停留"。
	st := &JourneyState{CustomerID: "cust_zero_since", CurrentStage: StageContact}
	svc.persistState(ctx, st)

	reader := NewCustomerJourneyServiceWithCache(c)
	overview := reader.GetOverview(ctx)
	for _, s := range overview.Stages {
		if math.IsInf(s.AvgStayHours, 0) || s.AvgStayHours > 24*365*10 {
			t.Errorf("阶段 %s 的平均停留被零值 StageSince 放大: %v 小时", s.Stage, s.AvgStayHours)
		}
	}
}

func TestCustomerJourney_FunnelSeesWritesFromAnotherInstance(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	writer := NewCustomerJourneyServiceWithCache(c)
	reader := NewCustomerJourneyServiceWithCache(c)

	if _, err := writer.Transition(ctx, "cust_funnel", StageContact,
		"ai", "ai", "加了微信", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}

	funnel := reader.Funnel(ctx)
	if funnel.TotalEntered != 1 {
		t.Fatalf("漏斗总进入量=%d，期望 1（跨实例读的是 L2 不是 L1）", funnel.TotalEntered)
	}
	for _, st := range funnel.Stages {
		want := 0
		if st.Stage == StageContact {
			want = 1
		}
		if st.Customers != want {
			t.Errorf("阶段 %s 客户数=%d，期望 %d", st.Stage, st.Customers, want)
		}
	}
}

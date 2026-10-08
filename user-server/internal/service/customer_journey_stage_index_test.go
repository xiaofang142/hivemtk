// customer_journey_stage_index_test.go 按阶段列客户这条读路径的跨实例可见性。
//
// 立起来的不变量（判据逐条对应 ListByStage 函数头那段）：
//   - 名单来自阶段索引 + 权威态回读，不是本实例的 L1 —— 一份实例写的阶段，另一份实例查得到；
//   - 索引是提示：成员换了阶段不会留在旧阶段的名单里；
//   - 每个客户在同一阶段的索引里只有一条（互动不推索引）；
//   - 名单有序：写入顺序不影响返回顺序；
//   - 索引读失败退回本实例视图，而不是回一张空表。
//
// 触发这条修复的是 LTC 全链路验证的复购那一跳：回款钩子把客户切进复购期并已落 L2，
// 而 /api/customer-journey/by-stage 仍然数出 0 —— 写的那份实例与读的那份不是同一份。

package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
)

// indexFailingCache 只在阶段索引这一条读上失败，其余全部透传给真缓存。
// 用来把「索引读失败」和「这个阶段确实没有客户」这两件事分开测。
type indexFailingCache struct {
	cache.Cache
}

func (indexFailingCache) LRange(context.Context, string, int64, int64) ([]string, error) {
	return nil, context.DeadlineExceeded
}

func TestCustomerJourney_ListByStageSeesWritesFromAnotherInstance(t *testing.T) {
	c := newIsolatedCacheForTest(t)
	writer := NewCustomerJourneyServiceWithCache(c)
	reader := NewCustomerJourneyServiceWithCache(c)

	if _, err := writer.Transition(context.Background(), "cust_cross", StageRepurchase,
		"collection", "sales_1", "回款完成→赢单，复购期跟进", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}
	got := reader.ListByStage(context.Background(), StageRepurchase)
	if len(got) != 1 || got[0] != "cust_cross" {
		t.Errorf("另一份实例写的复购期客户没被查出来: got=%v", got)
	}
}

func TestCustomerJourney_ListByStageDropsMemberThatLeftTheStage(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	a := NewCustomerJourneyServiceWithCache(c)
	b := NewCustomerJourneyServiceWithCache(c)

	if _, err := a.Transition(ctx, "cust_moves", StageLead, "ai", "ai", "留资", nil); err != nil {
		t.Fatalf("transition to lead: %v", err)
	}
	if _, err := b.Transition(ctx, "cust_moves", StageInterested, "ai", "ai", "约了演示", nil); err != nil {
		t.Fatalf("transition to interested: %v", err)
	}
	// 旧阶段的索引条目还在（索引只增不删），它必须被权威态回读筛掉。
	if leads := a.ListByStage(ctx, StageLead); len(leads) != 0 {
		t.Errorf("客户已离开 lead，仍出现在 lead 名单里: %v", leads)
	}
	if wants := b.ListByStage(ctx, StageInterested); len(wants) != 1 || wants[0] != "cust_moves" {
		t.Errorf("当前阶段查不到: %v", wants)
	}
	// 反向那一格也要成立：换阶段那份实例写的，另一份同样看得见。
	if wants := a.ListByStage(ctx, StageInterested); len(wants) != 1 || wants[0] != "cust_moves" {
		t.Errorf("换阶段写在另一份实例上，本份查不到: %v", wants)
	}
}

func TestCustomerJourney_ListByStageIndexKeepsOneEntryPerCustomerPerStage(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	a := NewCustomerJourneyServiceWithCache(c)
	b := NewCustomerJourneyServiceWithCache(c)

	if _, err := a.Transition(ctx, "cust_touch", StageLead, "ai", "ai", "留资", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// Touch 不改阶段，但同样走 persistState；没有哨兵键的话每互动一次多一条索引。
	for i := 0; i < 5; i++ {
		a.Touch(ctx, "cust_touch", "ai_chat")
		b.Touch(ctx, "cust_touch", "ai_chat")
	}
	n, err := c.LLen(ctx, journeyStageIndexKey(StageLead))
	if err != nil {
		t.Fatalf("llen: %v", err)
	}
	if n != 1 {
		t.Errorf("同一客户在同一阶段的索引条目应有 1 条，实际 %d 条（互动在推索引）", n)
	}
	if got := b.ListByStage(ctx, StageLead); len(got) != 1 {
		t.Errorf("名单应恰好 1 个客户: %v", got)
	}
}

func TestCustomerJourney_ListByStageOrderIsIndependentOfWriteOrder(t *testing.T) {
	ctx := context.Background()
	ids := []string{"cust_c", "cust_a", "cust_b"}

	first := NewCustomerJourneyServiceWithCache(newIsolatedCacheForTest(t))
	for _, id := range ids {
		if _, err := first.Transition(ctx, id, StageWon, "ai", "ai", "成交", nil); err != nil {
			t.Fatalf("transition %s: %v", id, err)
		}
	}
	second := NewCustomerJourneyServiceWithCache(newIsolatedCacheForTest(t))
	for i := len(ids) - 1; i >= 0; i-- {
		if _, err := second.Transition(ctx, ids[i], StageWon, "ai", "ai", "成交", nil); err != nil {
			t.Fatalf("transition %s: %v", ids[i], err)
		}
	}

	want := []string{"cust_a", "cust_b", "cust_c"}
	for name, got := range map[string][]string{"正序写入": first.ListByStage(ctx, StageWon),
		"倒序写入": second.ListByStage(ctx, StageWon)} {
		if len(got) != len(want) {
			t.Fatalf("%s: 名单长度 %d，期望 %d", name, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: 第 %d 位 = %q，期望 %q（名单没排序或排序没生效）", name, i, got[i], want[i])
			}
		}
	}
}

func TestCustomerJourney_ListByStageFallsBackToLocalViewWhenIndexFails(t *testing.T) {
	ctx := context.Background()
	inner := newIsolatedCacheForTest(t)
	s := NewCustomerJourneyServiceWithCache(indexFailingCache{inner})

	if _, err := s.Transition(ctx, "cust_degraded", StageLead, "ai", "ai", "留资", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}
	got := s.ListByStage(ctx, StageLead)
	// 索引读不动时这张表必须是"本实例写过的那一个"，不能是空表——
	// 空表在调用方眼里与"lead 阶段没有客户"完全同形。
	if len(got) != 1 || got[0] != "cust_degraded" {
		t.Errorf("索引故障应退回本实例视图，实际=%v", got)
	}
}

func TestCustomerJourney_AutoDetectSleepingIndexesNewStage(t *testing.T) {
	ctx := context.Background()
	c := newIsolatedCacheForTest(t)
	a := NewCustomerJourneyServiceWithCache(c)
	b := NewCustomerJourneyServiceWithCache(c)

	if _, err := a.Transition(ctx, "cust_sleep", StageWon, "ai", "ai", "成交", nil); err != nil {
		t.Fatalf("transition: %v", err)
	}
	a.mu.Lock()
	a.states["cust_sleep"].LastTouchAt = time.Now().Add(-200 * 24 * time.Hour)
	a.mu.Unlock()
	if woke := a.AutoDetectSleeping(ctx); len(woke) != 1 {
		t.Fatalf("沉睡检测应命中 1 个客户，实际 %v", woke)
	}
	if sleeping := b.ListByStage(ctx, StageSleeping); len(sleeping) != 1 || sleeping[0] != "cust_sleep" {
		t.Errorf("自动转入的沉睡阶段没进索引（另一份实例查不到）: %v", sleeping)
	}
	if won := b.ListByStage(ctx, StageWon); len(won) != 0 {
		t.Errorf("客户已沉睡，仍留在 won 名单里: %v", won)
	}
}

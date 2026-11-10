package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// fakeIntentRouteDailyRepo 刀2 测试桩：记录 UpsertDiff 收到的增量行。
type fakeIntentRouteDailyRepo struct {
	rows []*model.IntentRouteDaily
	err  error
}

func (f *fakeIntentRouteDailyRepo) UpsertDiff(ctx context.Context, delta *model.IntentRouteDaily) error {
	if f.err != nil {
		return f.err
	}
	cp := *delta
	f.rows = append(f.rows, &cp)
	return nil
}

func (f *fakeIntentRouteDailyRepo) GetRow(ctx context.Context, date, intentType, chainName, toolName string) (*model.IntentRouteDaily, error) {
	return nil, nil
}

// TestWriteIntentRouteDiff_Counters 验证刀2 冻结口径：
// 命中绑定工具计 chain_hits+tool_hits；未命中绑定工具的实际调用计 tool_skips；
// tool_calls/tool_success/tool_fail 按调用序列分流；0 调用轮只写链级 miss。
func TestWriteIntentRouteDiff_Counters(t *testing.T) {
	fake := &fakeIntentRouteDailyRepo{}
	se := &SalesEngine{}
	se.SetIntentRouteDailyRepository(fake)

	// 1) 混合调用：a 命中成功 / c 未命中失败 / b 命中成功
	se.writeIntentRouteDiff(context.Background(), "order_status", "order_status",
		[]string{"a", "b"},
		[]agentLoopToolCallOutcome{
			{name: "a", success: true},
			{name: "c", success: false},
			{name: "b", success: true},
		})
	if len(fake.rows) != 1 {
		t.Fatalf("应写 1 行，实得 %d", len(fake.rows))
	}
	got := fake.rows[0]
	if got.ChainName != "order_status" || got.IntentType != "order_status" || got.ToolName != "" {
		t.Fatalf("链级行五列错误: %+v", got)
	}
	if got.Date == "" {
		t.Fatal("date 应为应用层日切 yyyy-mm-dd")
	}
	if got.ChainHits != 1 || got.ChainMisses != 0 {
		t.Fatalf("chain_hits 期望 1，实得 %d/%d", got.ChainHits, got.ChainMisses)
	}
	if got.ToolHits != 2 || got.ToolSkips != 1 {
		t.Fatalf("tool_hits/skips 期望 2/1，实得 %d/%d", got.ToolHits, got.ToolSkips)
	}
	if got.ToolCalls != 3 || got.ToolSuccess != 2 || got.ToolFail != 1 {
		t.Fatalf("tool_calls/success/fail 期望 3/2/1，实得 %d/%d/%d", got.ToolCalls, got.ToolSuccess, got.ToolFail)
	}

	// 2) 0 工具调用轮（slotGateNone）→ 链级 miss
	fake2 := &fakeIntentRouteDailyRepo{}
	se2 := &SalesEngine{}
	se2.SetIntentRouteDailyRepository(fake2)
	se2.writeIntentRouteDiff(context.Background(), "complaint", "complaint", []string{"x"}, nil)
	if len(fake2.rows) != 1 {
		t.Fatalf("0 调用轮应写 1 行链级 miss，实得 %d", len(fake2.rows))
	}
	if fake2.rows[0].ChainMisses != 1 || fake2.rows[0].ChainHits != 0 || fake2.rows[0].ToolCalls != 0 {
		t.Fatalf("链级 miss 行错误: %+v", fake2.rows[0])
	}

	// 3) 未命中绑定（chainName 空）/ 未装配（repo nil）→ 零写入
	fake3 := &fakeIntentRouteDailyRepo{}
	se3 := &SalesEngine{}
	se3.SetIntentRouteDailyRepository(fake3)
	se3.writeIntentRouteDiff(context.Background(), "faq", "", []string{"a"}, []agentLoopToolCallOutcome{{name: "a", success: true}})
	if len(fake3.rows) != 0 {
		t.Fatalf("chainName 空不应写入，实得 %d 行", len(fake3.rows))
	}
	se4 := &SalesEngine{}
	se4.writeIntentRouteDiff(context.Background(), "faq", "faq", []string{"a"}, []agentLoopToolCallOutcome{{name: "a", success: true}})
	if len(fake3.rows) != 0 {
		t.Fatal("repo nil 守卫后不应有写入")
	}
}

// TestWriteIntentRouteDiff_FailOpen DB 失败仅 logger，不 panic 不返回错误（调用方无返回）。
func TestWriteIntentRouteDiff_FailOpen(t *testing.T) {
	fake := &fakeIntentRouteDailyRepo{err: context.DeadlineExceeded}
	se := &SalesEngine{}
	se.SetIntentRouteDailyRepository(fake)
	se.writeIntentRouteDiff(context.Background(), "order_status", "order_status",
		[]string{"a"}, []agentLoopToolCallOutcome{{name: "a", success: true}})
	if len(fake.rows) != 0 {
		t.Fatal("桩返回错误时不应记录行")
	}
}

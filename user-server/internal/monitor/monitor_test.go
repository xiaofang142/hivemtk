package monitor

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/tracing"
)

// ⚠️ 2026-09-16 审计（TEST-06）：本文件原先自带一套 openTestDB + envOr +
// readEnvPassword + splitLines（约 80 行），与 internal/pkg/testutil 并存，
// 且两套默认值互相矛盾。已整段删除，统一改走 testutil.NewTestDB。
//
// 原实现的三个具体问题（保留说明以免回退）：
//
//  1. **默认连的是生产库**：`dbname` 默认值写成 `user_db` —— 那是主库名，
//     而 testutil 的默认是 `user_db_test`（进程级隔离库）。本地只要 PG 可达，
//     本用例就会在真实 `user_db` 上 AutoMigrate 并 `Create` 写入测试行
//     （即下面的 inbound 消息），且 AutoMigrate 会顺带补列/补索引 ——
//     测试**改动生产 schema 与数据**，不只是"跑测试"。
//  2. **失败即静默跳过**：连不上/迁移失败时 `t.Logf` + 返回 nil，调用方 `t.Skip`
//     → `go test` 退出 0，属本项目反复出现的假绿模式（RISK-01 同源）。
//     端口/口令写错这类真实故障因此从不报警。
//  3. **重复第三份连接参数**：端口默认 8232、口令另写一套 `.env` 解析器，
//     与 testutil 的候选探测（8232/8202）和 `POSTGRES_PASSWORD` 回落各说各话。
//
// 现在的口径：**测试库引导只有 testutil 一个入口**。新增测试请直接调用
// `testutil.NewTestDB(t, models...)`，不要自带 DSN 拼装。
func TestTracingAndMonitoring(t *testing.T) {
	// NewTestDB 在隔离库（user_db_test_<pid>）内 DropTable + AutoMigrate，
	// 不会触碰 user_db；不可达时本地 Skip / CI Fatal（不再无条件 Skip）。
	gdb := testutil.NewTestDB(t, &model.MessageHub{}, &model.MessageTrace{})
	db.SetTestDB(gdb)
	ctx := context.Background()
	conv := "conv-monitor-test-" + time.Now().Format("150405")

	inboundTrace := tracing.GenerateTraceID()
	inHub := &model.MessageHub{
		ConversationID: conv, AccountID: "acct-test", Platform: "xiaohongshu",
		Direction: "inbound", Status: "received", MsgID: "m-in-" + conv,
		TraceID: inboundTrace, Content: "hello",
	}
	if err := gdb.WithContext(ctx).Create(inHub).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	tracing.RecordNode(ctx, tracing.NodeSpan{
		TraceID: inboundTrace, ConversationID: conv, AccountID: "acct-test",
		Channel: "xiaohongshu", Node: tracing.NodeIngest, Direction: "inbound",
		MsgID: inHub.MsgID, Expected: "客户消息落库", Status: tracing.StatusOk,
	})
	tracing.RecordNode(ctx, tracing.NodeSpan{
		TraceID: inboundTrace, ConversationID: conv, AccountID: "acct-test",
		Channel: "xiaohongshu", Node: tracing.NodeInboxSync, Direction: "inbound",
		MsgID: inHub.MsgID, Expected: "inbox_conversations 同步", Status: tracing.StatusOk,
	})

	linked := tracing.LinkOutboundTraceID(ctx, conv)
	if linked != inboundTrace {
		t.Fatalf("LinkOutboundTraceID 期望复用 %q，实际 %q", inboundTrace, linked)
	}

	tracing.RecordNode(ctx, tracing.NodeSpan{
		TraceID: linked, ConversationID: conv, AccountID: "acct-test",
		Channel: "xiaohongshu", Node: tracing.NodeOutboundEnqueue, Direction: "outbound",
		MsgID: "m-out-" + conv, Input: map[string]any{"content_len": 5},
		Output:   map[string]any{"status": "pending"},
		Expected: "AI 回复落库 outbox(pending)", Status: tracing.StatusOk,
	})
	tracing.RecordNode(ctx, tracing.NodeSpan{
		TraceID: linked, ConversationID: conv, AccountID: "acct-test",
		Channel: "xiaohongshu", Node: tracing.NodeDeliveredAck, Direction: "outbound",
		MsgID: "m-out-" + conv, Output: map[string]any{"status": "delivered"},
		DurationMs: 42, Expected: "pending→delivered", Status: tracing.StatusOk,
	})

	ov, err := HealthOverview(ctx)
	if err != nil {
		t.Fatalf("HealthOverview: %v", err)
	}
	if ov.TotalTraces < 4 {
		t.Fatalf("TotalTraces 期望 >=4，实际 %d", ov.TotalTraces)
	}

	lcs, err := Lifecycle(ctx, conv, "", 5)
	if err != nil {
		t.Fatalf("Lifecycle: %v", err)
	}
	if len(lcs) == 0 {
		t.Fatalf("Lifecycle 应至少 1 轮，实际 %d", len(lcs))
	}
	first := lcs[0]
	if len(first.Nodes) < 4 {
		t.Fatalf("该轮节点数期望 >=4，实际 %d", len(first.Nodes))
	}
	if first.EndToEndMs == nil {
		t.Fatalf("应计算出端到端时延")
	}

	traces, err := Traces(ctx, 50)
	if err != nil {
		t.Fatalf("Traces: %v", err)
	}
	found := false
	for _, tr := range traces {
		if tr.ConversationID == conv {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Traces 应包含本会话 %q", conv)
	}

	nh, err := NodeHealthByChannel(ctx)
	if err != nil {
		t.Fatalf("NodeHealthByChannel: %v", err)
	}
	hit := false
	for _, n := range nh {
		if n.Channel == "xiaohongshu" && n.Total > 0 {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("NodeHealthByChannel 应包含 xiaohongshu 的节点聚合")
	}

	deleted, err := PurgeOld(ctx, 100*365*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeOld: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("PurgeOld 不应删除近期数据，实际删除 %d", deleted)
	}

	// 原实现在此处手工 `Delete` 清理本会话的测试行 —— 那是在**共享库**上跑测试
	// 才需要的补偿动作。现在跑在进程级隔离库内（NewTestDB 每次 DropTable 重建），
	// 数据不会外溢，手工清理已无必要，故移除（避免让读者误以为库是共享的）。
}

func TestGenerateTraceID(t *testing.T) {
	a := tracing.GenerateTraceID()
	b := tracing.GenerateTraceID()
	if a == b || len(a) < 20 {
		t.Fatalf("GenerateTraceID 异常: %q / %q", a, b)
	}
}

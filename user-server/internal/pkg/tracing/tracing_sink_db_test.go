package tracing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestSinkPersistsSpansToMessageTrace 端到端验证「异步缓冲 → 批量落库 → Stop 排空」全链路。
// 断言一律查库里的行（不是内存视图），并按本用例独有的 trace_id 过滤，
// 因此与同包其它用例的执行顺序无关。
func TestSinkPersistsSpansToMessageTrace(t *testing.T) {
	d := testutil.NewTestDB(t, &model.MessageTrace{})
	if d == nil {
		t.Fatal("NewTestDB 返回 nil（测试库不可达）")
	}
	orig := db.GetDB()
	db.SetTestDB(d)
	t.Cleanup(func() { db.SetTestDB(orig) })

	Init(d)

	tid := fmt.Sprintf("tr-sink-%d", time.Now().UnixNano())
	ctx := WithCarrier(context.Background(), &Carrier{
		TraceID: tid, ConversationID: "sink-conv", AccountID: "sink-acct", Channel: "sink",
	})

	pubBefore, droppedBefore := Stats()
	RecordNode(ctx, NodeSpan{Node: NodeIngest, Direction: "inbound", MsgID: "m-1", Expected: "消息进入"})
	Start(ctx, NodeAIDispatch).Input(map[string]any{"q": "你好"}).Output(map[string]any{"a": "你好呀"}).End(nil, nil)
	ReportToolCall(ctx, ToolTraceEvent{
		Kind: model.SpanKindToolCall, ToolName: "kb_search", TurnIndex: 1,
		DurationMs: 12, Input: "q", Output: "hits",
	})
	pubAfter, droppedAfter := Stats()

	if delta := pubAfter - pubBefore; delta != 3 {
		t.Fatalf("published 增量=%d want 3（Init 后应全部进入缓冲）", delta)
	}
	if droppedAfter != droppedBefore {
		t.Fatalf("低负载下不应发生丢弃: dropped %d -> %d", droppedBefore, droppedAfter)
	}

	var rows []model.MessageTrace
	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := d.Where("trace_id = ?", tid).Order("node_order asc").Find(&rows).Error; err != nil {
			t.Fatalf("查询 message_trace 失败: %v", err)
		}
		if len(rows) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(rows) != 3 {
		t.Fatalf("落库行数=%d want 3（异步 sink 未按时批量写入）nodes=%v", len(rows), nodeKeys(rows))
	}

	byNode := map[string]model.MessageTrace{}
	for _, r := range rows {
		byNode[r.Node] = r
	}
	ingest, ok := byNode[NodeIngest]
	if !ok {
		t.Fatalf("缺少 ingest 行, got nodes=%v", nodeKeys(rows))
	}
	if ingest.NodeOrder != 1 || ingest.Direction != "inbound" || ingest.MsgID != "m-1" || ingest.Status != StatusOk {
		t.Errorf("ingest 行字段错: %+v", ingest)
	}
	if ingest.ConversationID != "sink-conv" || ingest.AccountID != "sink-acct" || ingest.Channel != "sink" {
		t.Errorf("载体归属未落到行上: %+v", ingest)
	}
	if ingest.SpanKind != model.SpanKindLifecycle || ingest.Expected != "消息进入" {
		t.Errorf("ingest 种类/预期列错: %+v", ingest)
	}
	ai, ok := byNode[NodeAIDispatch]
	if !ok {
		t.Fatal("缺少 ai_dispatch 行")
	}
	if ai.Input != `{"q":"你好"}` || ai.Output != `{"a":"你好呀"}` {
		t.Errorf("input/output 未按 JSON 落库: in=%q out=%q", ai.Input, ai.Output)
	}
	if ai.ParentNode != "" {
		t.Errorf("lifecycle span 不应有父节点: %q", ai.ParentNode)
	}
	tool, ok := byNode[model.SpanKindToolCall]
	if !ok {
		t.Fatal("缺少 tool_call 行")
	}
	if tool.ToolName != "kb_search" || tool.TurnIndex != 1 || tool.ParentNode != NodeAIDispatch {
		t.Errorf("tool_call 层级列错: %+v", tool)
	}
	if tool.DurationMs != 12 || tool.SpanKind != model.SpanKindToolCall || tool.Status != StatusOk {
		t.Errorf("tool_call 耗时/种类/状态错: %+v", tool)
	}
	// 层级 span 的 node_order 复用 ai_dispatch 的值（非 99），供前端按链路排序
	if tool.NodeOrder != NodeOrder(NodeAIDispatch) {
		t.Errorf("tool_call node_order=%d want %d", tool.NodeOrder, NodeOrder(NodeAIDispatch))
	}
	if tool.Input != "q" || tool.Output != "hits" {
		t.Errorf("tool_call input/output 应原样入库: in=%q out=%q", tool.Input, tool.Output)
	}

	// 同一 ctx 下补一批下行拉取：验证「按 msg_id 去重」真的落在库上。
	dkSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	dk1, dk2 := "dk-1-"+dkSuffix, "dk-2-"+dkSuffix
	RecordDownlinkFetchBatch(ctx, "sink", "sink-acct", []*model.MessageHub{
		{MsgID: dk1, ConversationID: "sink-conv"},
		{MsgID: dk2, ConversationID: "sink-conv"},
	})
	waitTraceRows(t, d, tid, 5)

	// 再喂一次相同 msg_id：库里不应出现重复行（去重靠上一步已落库的行）
	RecordDownlinkFetchBatch(ctx, "sink", "sink-acct", []*model.MessageHub{
		{MsgID: dk1, ConversationID: "sink-conv"},
		{MsgID: dk2, ConversationID: "sink-conv"},
	})
	time.Sleep(1200 * time.Millisecond) // > 批量 ticker(300ms)：若有重复行此时已可见
	if got := countTraceRows(t, d, tid); got != 5 {
		t.Errorf("重复 msg_id 应被去重, 行数=%d want 5", got)
	}

	var fetchRows []model.MessageTrace
	if err := d.Where("trace_id = ? AND node = ?", tid, NodeDownlinkFetch).Order("msg_id asc").Find(&fetchRows).Error; err != nil {
		t.Fatalf("查询 downlink_fetch 行失败: %v", err)
	}
	if len(fetchRows) != 2 {
		t.Fatalf("downlink_fetch 行数=%d want 2", len(fetchRows))
	}
	for i, want := range []string{dk1, dk2} {
		r := fetchRows[i]
		if r.MsgID != want || r.NodeOrder != 5 || r.Direction != "outbound" || r.Status != StatusOk {
			t.Errorf("downlink_fetch 行 %d 错: %+v", i, r)
		}
		if r.ConversationID != "sink-conv" || r.AccountID != "sink-acct" || r.Channel != "sink" {
			t.Errorf("downlink_fetch 归属列错: %+v", r)
		}
	}

	Stop()

	// Stop 之后缓冲已被排空落库：同一 trace 的行数不再变化
	if got := countTraceRows(t, d, tid); got != 5 {
		t.Errorf("Stop 排空后行数=%d want 5", got)
	}
}

func countTraceRows(t *testing.T, d *gorm.DB, traceID string) int64 {
	t.Helper()
	var n int64
	if err := d.Model(&model.MessageTrace{}).Where("trace_id = ?", traceID).Count(&n).Error; err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	return n
}

// waitTraceRows 轮询等待异步 sink 把行数补到 want，超时即失败（不靠 sleep 猜时序）。
func waitTraceRows(t *testing.T, d *gorm.DB, traceID string, want int64) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		n := countTraceRows(t, d, traceID)
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待异步落库超时: 行数=%d want>=%d", n, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func nodeKeys(rows []model.MessageTrace) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Node)
	}
	return out
}

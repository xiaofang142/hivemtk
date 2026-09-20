package tracing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/trace"
)

func TestNodeOrderMatchesLifecycleSequence(t *testing.T) {
	cases := map[string]int{
		NodeIngest: 1, NodeAIDispatch: 2, NodeOutboundEnqueue: 3,
		NodeInboxSync: 4, NodeDownlinkFetch: 5, NodeDeliveredAck: 6,
	}
	for node, want := range cases {
		if got := NodeOrder(node); got != want {
			t.Errorf("NodeOrder(%s)=%d want %d", node, got, want)
		}
	}
	// 层级节点不在生命周期序列里 → 一律排末尾
	for _, node := range []string{NodeAgentTurn, NodeToolCall, "not_a_node"} {
		if got := NodeOrder(node); got != 99 {
			t.Errorf("NodeOrder(%s)=%d want 99", node, got)
		}
	}
}

func TestNodeLabelKnownAndFallback(t *testing.T) {
	if got := NodeLabel(NodeIngest); got != "消息上报接入" {
		t.Errorf("NodeLabel(ingest)=%q", got)
	}
	if got := NodeLabel(NodeAgentTurn); got != "Agent 一轮推理" {
		t.Errorf("NodeLabel(agent_turn)=%q", got)
	}
	if got := NodeLabel("mystery"); got != "mystery" {
		t.Errorf("未知节点应原样返回: %q", got)
	}
}

func TestCarrierRoundTripAndNilSafety(t *testing.T) {
	c := NewCarrier("wecom", "acct-1", "conv-1")
	if !strings.HasPrefix(c.TraceID, "tr-") || len(c.TraceID) < 20 {
		t.Fatalf("TraceID 形态异常: %q", c.TraceID)
	}
	if c.Channel != "wecom" || c.AccountID != "acct-1" || c.ConversationID != "conv-1" {
		t.Fatalf("载体字段未装配: %+v", c)
	}

	ctx := WithCarrier(context.Background(), c)
	if got := CarrierFromContext(ctx); got != c {
		t.Fatal("CarrierFromContext 应取回同一指针")
	}
	if TraceIDFromContext(ctx) != c.TraceID {
		t.Error("TraceIDFromContext 应优先取 Carrier")
	}

	if CarrierFromContext(context.Background()) != nil {
		t.Error("无载体且无 trace_id 时应返回 nil")
	}
	if TraceIDFromContext(context.Background()) != "" {
		t.Error("空上下文 trace_id 应为空串")
	}
	// 无载体但有 logger 注入的 trace_id → 降级为空载体（只带 TraceID）
	logCtx := trace.NewContextWithTraceID(context.Background(), "tid-9")
	if got := CarrierFromContext(logCtx); got == nil || got.TraceID != "tid-9" || got.ConversationID != "" {
		t.Errorf("应降级为仅含 trace_id 的空载体, got %+v", got)
	}
	if tid := TraceIDFromContext(logCtx); tid != "tid-9" {
		t.Errorf("TraceIDFromContext 应回落到 logger trace_id, got %q", tid)
	}
	if CarrierFromContext(nil) != nil {
		t.Error("nil ctx 不应 panic 且应返回 nil")
	}
	if TraceIDFromContext(nil) != "" {
		t.Error("nil ctx 的 trace_id 应为空串")
	}
}

func TestCarrierChildAndWithMsgID(t *testing.T) {
	var nilCarrier *Carrier
	fresh := nilCarrier.Child()
	if fresh == nil || !strings.HasPrefix(fresh.TraceID, "tr-") {
		t.Fatalf("nil 接收者 Child 应产出新载体, got %+v", fresh)
	}

	parent := NewCarrier("tg", "acct", "conv").WithMsgID("m-1")
	child := parent.Child()
	if child == parent {
		t.Error("Child 必须返回副本")
	}
	if child.TraceID != parent.TraceID || child.ConversationID != "conv" || child.MsgID != "m-1" {
		t.Errorf("Child 应保留会话维度与 msg_id: %+v", child)
	}
	child.MsgID = "changed"
	if parent.MsgID != "m-1" {
		t.Error("Child 副本不得影响父载体")
	}
	if got := NewCarrier("c", "a", "s").WithMsgID("x"); got.MsgID != "x" || got.Direction != "" {
		t.Errorf("WithMsgID 应写入新副本: %+v", got)
	}
}

func TestRecalledChunksLifecycle(t *testing.T) {
	ctx := InitRecalledChunks(context.Background())
	ctx = InitRecalledChunks(ctx) // 幂等：已存在容器时不得替换
	RecordRecalledChunks(ctx, []string{"c1", "", "c2"})
	RecordRecalledChunks(ctx, []string{"c3"})
	RecordRecalledChunks(ctx, nil) // 空批次直接返回

	got := RecalledChunksOf(ctx)
	want := []string{"c1", "c2", "c3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RecalledChunksOf=%v want %v", got, want)
	}

	if RecalledChunksOf(context.Background()) != nil {
		t.Error("未初始化容器应返回 nil")
	}
	RecordRecalledChunks(context.Background(), []string{"ignored"}) // 无容器不得 panic
	if n := len(RecalledChunksOf(context.Background())); n != 0 {
		t.Errorf("无容器时写入应被丢弃, got %d 条", n)
	}
}

func TestSpanToPendingAppliesDefaults(t *testing.T) {
	carrier := NewCarrier("wx", "acct-9", "conv-9")
	ctx := WithCarrier(context.Background(), carrier)

	p := Start(ctx, NodeToolCall).
		Kind(model.SpanKindToolCall).
		Parent("custom_parent").
		Turn(3).
		Tool("kb_search").
		Agent("agent-1").
		Direction("inbound").
		MsgID("msg-7").
		Input(map[string]any{"q": "退款"}).
		Expected("命中知识库").
		Abnormal("上游超时").
		toPending(42, "abnormal", "boom")

	if p.traceID != carrier.TraceID || p.conversationID != "conv-9" || p.accountID != "acct-9" || p.channel != "wx" {
		t.Errorf("归属字段未从载体继承: %+v", p)
	}
	if p.node != NodeToolCall || p.spanKind != model.SpanKindToolCall || p.parentNode != "custom_parent" {
		t.Errorf("节点/种类/父节点错: node=%s kind=%s parent=%s", p.node, p.spanKind, p.parentNode)
	}
	if p.turnIndex != 3 || p.toolName != "kb_search" || p.agentID != "agent-1" {
		t.Error("层级字段（turn/tool/agent）丢失")
	}
	if p.durationMs != 42 || p.status != "abnormal" || p.errorStr != "boom" || p.abnormal != "上游超时" {
		t.Error("耗时/状态/错误/异常原因未透传")
	}
	if p.nodeOrder != NodeOrder(NodeToolCall) {
		t.Errorf("nodeOrder=%d want %d", p.nodeOrder, NodeOrder(NodeToolCall))
	}

	// 层级 span 未显式给 parent 时，默认挂到 ai_dispatch；abnormal 缺省时回落为 error。
	q := Start(ctx, NodeAgentTurn).Kind(model.SpanKindAgentTurn).toPending(7, "abnormal", "llm 500")
	if q.parentNode != NodeAIDispatch {
		t.Errorf("非 lifecycle span 默认父节点应为 ai_dispatch, got %q", q.parentNode)
	}
	if q.abnormal != "llm 500" {
		t.Errorf("abnormal 缺省应回落 error 文本, got %q", q.abnormal)
	}

	// lifecycle span：kind 缺省补 lifecycle，parent 保持空，abnormal 保持空。
	r := Start(ctx, NodeIngest).toPending(1, "ok", "")
	if r.spanKind != model.SpanKindLifecycle || r.parentNode != "" || r.abnormal != "" {
		t.Errorf("lifecycle span 默认值错: kind=%q parent=%q abnormal=%q", r.spanKind, r.parentNode, r.abnormal)
	}

	// 无载体但有 logger trace_id 时，traceID 走降级载体分支。
	orphan := Start(trace.NewContextWithTraceID(context.Background(), "tid-orphan"), NodeInboxSync).
		toPending(1, "ok", "")
	if orphan.traceID != "tid-orphan" {
		t.Errorf("无载体应回落 logger trace_id, got %q", orphan.traceID)
	}
	if orphan.accountID != "" || orphan.channel != "" {
		t.Error("空载体不应凭空造出账号/渠道")
	}

	// TraceID 显式覆盖（出站复用入站 trace）
	cov := Start(ctx, NodeOutboundEnqueue).TraceID("tr-manual").toPending(1, "ok", "")
	if cov.traceID != "tr-manual" {
		t.Errorf("TraceID 覆盖失效, got %q", cov.traceID)
	}
	if got := Start(context.Background(), NodeDeliveredAck).TraceID("tr-new").toPending(1, "ok", ""); got.traceID != "tr-new" {
		t.Error("无载体时 TraceID 应自建载体并写入")
	} else if got.carrier == nil {
		t.Error("TraceID 应写入自建载体")
	}
}

func TestToModelFromPendingMapsEveryColumn(t *testing.T) {
	carrier := NewCarrier("dd", "acct-m", "conv-m")
	p := Start(WithCarrier(context.Background(), carrier), NodeDeliveredAck).
		MsgID("m-2").Direction("outbound").Expected("客户端确认送达").
		Abnormal("偶发重投").Input(map[string]any{"k": "in"}).Output(map[string]any{"k": "out"}).
		Kind(model.SpanKindLifecycle).toPending(13, "ok", "e-1")

	row := toModelFromPending(p)
	if row.TraceID != carrier.TraceID || row.ConversationID != "conv-m" || row.AccountID != "acct-m" || row.Channel != "dd" {
		t.Errorf("行归属列映射错: %+v", row)
	}
	if row.Node != NodeDeliveredAck || row.NodeOrder != 6 || row.MsgID != "m-2" || row.Direction != "outbound" {
		t.Error("节点/顺序/消息/方向列映射错")
	}
	if row.Input != `{"k":"in"}` || row.Output != `{"k":"out"}` {
		t.Errorf("input/output 应 JSON 化, got %q / %q", row.Input, row.Output)
	}
	if row.DurationMs != 13 || row.Status != "ok" || row.Expected != "客户端确认送达" {
		t.Error("耗时/状态/预期列映射错")
	}
	if row.SpanKind != model.SpanKindLifecycle || row.ParentNode != "" || row.Error != "e-1" || row.Abnormal != "偶发重投" {
		t.Errorf("层级/错误列映射错: %+v", row)
	}
}

func TestPublishIsNonBlockingAndNeverDrops(t *testing.T) {
	// 本用例不调 Init/Stop，因此必须在「worker 未启动」与「已被同包其它用例启动」两种进程状态下都成立。
	pubBefore, droppedBefore := Stats()
	Start(context.Background(), NodeIngest).Input("x").End("y", nil)
	RecordNode(context.Background(), NodeSpan{Node: NodeInboxSync})
	ReportToolCall(context.Background(), ToolTraceEvent{ToolName: "noop", DurationMs: 5})
	pubAfter, droppedAfter := Stats()

	if delta := pubAfter - pubBefore; delta != 0 && delta != 3 {
		t.Errorf("published 增量=%d, 只允许 0（未 Init）或 3（已 Init）", delta)
	}
	if droppedAfter != droppedBefore {
		t.Errorf("低负载下不得丢弃 span: dropped %d -> %d", droppedBefore, droppedAfter)
	}
}

func TestEndKeepsExplicitOutput(t *testing.T) {
	carrier := NewCarrier("ch", "a", "c")
	ctx := WithCarrier(context.Background(), carrier)

	set := Start(ctx, NodeIngest).Output("直接设置的输出")
	set.End("被忽略的输出", errors.New("boom"))
	if set.output != "直接设置的输出" {
		t.Errorf("已有 output 不应被 End 覆盖, got %v", set.output)
	}

	fallback := Start(ctx, NodeIngest)
	fallback.End("兜底输出", nil)
	if fallback.output != "兜底输出" {
		t.Errorf("未设置 output 时应采用 End 入参, got %v", fallback.output)
	}
}

func TestRecordNodeAndReportToolCallDoNotPanic(t *testing.T) {
	carrier := NewCarrier("ch", "acct", "conv")
	ctx := WithCarrier(context.Background(), carrier)

	RecordNode(ctx, NodeSpan{Node: NodeIngest})                                        // 空白字段由载体/NodeOrder/ok 补齐
	RecordNode(context.Background(), NodeSpan{Node: "unknown", Status: StatusSkipped}) // 未知节点 + 状态常量
	RecordNode(ctx, NodeSpan{Node: NodeAIDispatch, Status: StatusFailed, Error: "e"})  //
	RecordNode(ctx, NodeSpan{TraceID: "tr-given", Node: NodeInboxSync, NodeOrder: 77}) // 显式值不被载体/默认覆盖
	ReportToolCall(ctx, ToolTraceEvent{Kind: model.SpanKindAgentTurn, TurnIndex: 2, AgentID: "a1", Error: "llm down"})
	ReportToolCall(ctx, ToolTraceEvent{TraceID: "tr-explicit", Status: "ok"})
	ReportToolCall(context.Background(), ToolTraceEvent{}) // 无载体也不能 panic

	RecordDownlinkFetchBatch(context.Background(), "wx", "acct", nil) // 空批次直接返回
}

func TestTraceIDGenerationIsStableAndDistinct(t *testing.T) {
	a, b := GenerateTraceID(), GenerateTraceID()
	if !strings.HasPrefix(a, "tr-") || a == b {
		t.Fatalf("GenerateTraceID 应为 tr- 前缀且互不相同: %s / %s", a, b)
	}
	want := "tr-" + sha1Sum("conv-x|acct-x|inbound")
	if got := NodeTraceID("conv-x", "acct-x"); got != want {
		t.Errorf("NodeTraceID 不稳定: %q want %q", got, want)
	}

	c := NewCarrier("ch", "a", "conv-1")
	ctx := WithCarrier(context.Background(), c)
	if got := LinkInboundTraceID(ctx, "conv-1"); got != NodeTraceID("conv-1", "") || c.TraceID != got {
		t.Errorf("LinkInboundTraceID 应回写载体 trace_id: got %q carrier %q", got, c.TraceID)
	}

	// 无 DB：空会话 → 新生成 id；有会话 → 回落派生 id。两者都写回载体。
	c2 := NewCarrier("ch", "a", "conv-2")
	ctx2 := WithCarrier(context.Background(), c2)
	if got := LinkOutboundTraceID(ctx2, ""); !strings.HasPrefix(got, "tr-") || c2.TraceID != got {
		t.Errorf("空会话出站应生成新 trace: %q carrier=%q", got, c2.TraceID)
	}
	if got := LinkOutboundTraceID(ctx2, "conv-2"); got != NodeTraceID("conv-2", "") || c2.TraceID != got {
		t.Errorf("无 DB 时出站应回落派生 id 并回写载体: %q carrier=%q", got, c2.TraceID)
	}
	if got := LinkOutboundTraceID(context.Background(), "conv-3"); got == "" {
		t.Error("无载体时仍应返回可用 trace_id")
	}
	// 无载体也不能 panic，且不得凭空造出载体
	if got := LinkInboundTraceID(context.Background(), "conv-4"); got != NodeTraceID("conv-4", "") {
		t.Errorf("无载体入站仍应返回派生 id: %q", got)
	}
}

func TestTextHelpers(t *testing.T) {
	if ErrStr(nil) != "" || ErrStr(errors.New("x")) != "x" {
		t.Error("ErrStr 语义错")
	}
	if got := toJSON(nil); got != "" {
		t.Errorf("toJSON(nil)=%q want 空串", got)
	}
	if got := toJSON("原文"); got != "原文" {
		t.Errorf("字符串应原样返回, got %q", got)
	}
	if got := toJSON([]byte("by")); got != "by" {
		t.Errorf("[]byte 应转字符串, got %q", got)
	}
	if got := toJSON(map[string]int{"a": 1}); got != `{"a":1}` {
		t.Errorf("map 应 JSON 化, got %q", got)
	}
	// json.Marshal 不支持 complex → 回落 fmt 文本（此处必须稳定可断言）
	if got := toJSON(complex(1, 2)); got != "(1+2i)" {
		t.Errorf("不可序列化对象应回落 fmt 文本, got %q", got)
	}
	if len(sha1Sum("")) != 40 {
		t.Error("sha1Sum 应返回 40 位十六进制")
	}
	timer := StartSpan()
	time.Sleep(2 * time.Millisecond)
	if timer.ElapsedMs() < 1 {
		t.Errorf("ElapsedMs=%d 应为正", timer.ElapsedMs())
	}
}

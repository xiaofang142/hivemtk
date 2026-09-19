package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// TestSmartCSOrchestrator_NilSafe nil 入参安全
func TestSmartCSOrchestrator_NilSafe(t *testing.T) {
	var o *SmartCSOrchestrator
	_, err := o.HandleIncoming(context.Background(), &IncomingContext{Content: "x"})
	if err == nil {
		t.Fatal("nil orchestrator 应返回错误")
	}

	o = NewSmartCSOrchestrator(nil, nil, nil)
	_, err = o.HandleIncoming(context.Background(), nil)
	if err == nil {
		t.Fatal("nil incoming 应返回错误")
	}

	_, err = o.HandleIncoming(context.Background(), &IncomingContext{Content: "  "})
	if err == nil {
		t.Fatal("空内容应返回错误")
	}
}

// TestSmartCSOrchestrator_DefaultConfig 默认配置
func TestSmartCSOrchestrator_DefaultConfig(t *testing.T) {
	cfg := DefaultOrchestratorConfig()
	if cfg.ConfidenceThreshold != 0.7 {
		t.Errorf("默认置信度阈值应为 0.7，实际 %.2f", cfg.ConfidenceThreshold)
	}
	if !cfg.EnableAutoReply {
		t.Error("默认应启用自动回复")
	}
	if cfg.MaxAIConsecutive != 10 {
		t.Errorf("默认 AI 连续上限应为 10，实际 %d", cfg.MaxAIConsecutive)
	}
}

// TestSmartCSOrchestrator_ExtractConfidence 置信度提取
func TestSmartCSOrchestrator_ExtractConfidence(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)

	if c := o.extractConfidence(context.Background(), nil, "s-test", "hi"); c != 0 {
		t.Errorf("nil 响应置信度应为 0，实际 %.2f", c)
	}

	resp := &SalesResponse{
		Intent: &dto.RecognizeResult{Confidence: 0.85},
	}
	if c := o.extractConfidence(context.Background(), resp, "s-test", "hi"); c != 0.85 {
		t.Errorf("意图置信度 0.85 应直接返回，实际 %.2f", c)
	}

	resp2 := &SalesResponse{
		Reply:     "您好，有什么可以帮您？",
		Polished:  true,
		Audited:   true,
		RAGChunks: []RAGChunk{{Content: "x"}},
	}
	c := o.extractConfidence(context.Background(), resp2, "s-test", "hi")
	if c <= 0.5 {
		t.Errorf("完整链路置信度应 > 0.5，实际 %.2f", c)
	}
	if c > 1.0 {
		t.Errorf("置信度应 ≤ 1.0，实际 %.2f", c)
	}
}

// TestSmartCSOrchestrator_IsUrgentOrComplaint 紧急投诉识别
func TestSmartCSOrchestrator_IsUrgentOrComplaint(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)

	urgentCases := []string{
		"我要投诉你们",
		"我要举报",
		"赶紧给我处理",
		"马上退款",
		"你们是骗子",
		"315曝光你们",
		"退钱！",
	}
	for _, c := range urgentCases {
		if !o.isUrgentOrComplaint(context.Background(), c) {
			t.Errorf("应识别为紧急/投诉: %q", c)
		}
	}

	normalCases := []string{
		"你好，请问价格",
		"我想了解一下产品",
		"谢谢",
		"hello",
	}
	for _, c := range normalCases {
		if o.isUrgentOrComplaint(context.Background(), c) {
			t.Errorf("不应识别为紧急/投诉: %q", c)
		}
	}
}

// TestSmartCSOrchestrator_EngineNil 引擎未注入状态验证
// 注意：HandleIncoming 完整流程需要 DB 支持（findOrCreateSession），
// 此测试仅验证 engine==nil 的状态，不调用 HandleIncoming（避免无 DB panic）
func TestSmartCSOrchestrator_EngineNil(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	if o.engine != nil {
		t.Fatal("引擎应为 nil")
	}
	if o.sessionSvc == nil {
		t.Error("sessionSvc 不应为 nil")
	}
	if o.assignmentSvc == nil {
		t.Error("assignmentSvc 不应为 nil")
	}
	if o.sessionRepo == nil {
		t.Error("sessionRepo 不应为 nil")
	}
}

// TestSmartCSOrchestrator_ConfidenceThreshold 置信度阈值决策
func TestSmartCSOrchestrator_ConfidenceThreshold(t *testing.T) {
	cfg := &OrchestratorConfig{
		ConfidenceThreshold: 0.8,
		EnableAutoReply:     false,
		MaxAIConsecutive:    3,
	}
	o := NewSmartCSOrchestrator(nil, cfg, nil)
	if o.confidenceThreshold != 0.8 {
		t.Errorf("阈值应为 0.8，实际 %.2f", o.confidenceThreshold)
	}
	if o.enableAutoReply {
		t.Error("应关闭自动回复")
	}
	if o.maxAIConsecutive != 3 {
		t.Errorf("AI 连续上限应为 3，实际 %d", o.maxAIConsecutive)
	}
}

// TestSmartCSOrchestrator_SafeMessageID safeMessageID 工具
func TestSmartCSOrchestrator_SafeMessageID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"abcdef1234567890", "abcdef12"},
		{"short", "short"},
		{"", "nomsgid"},
		{"12345678", "12345678"},
	}
	for _, c := range cases {
		got := safeMessageID(c.input)
		if got != c.want {
			t.Errorf("safeMessageID(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestSmartCSOrchestrator_AgentTakeover_NoSession 座席接管不存在的会话
func TestSmartCSOrchestrator_AgentTakeover_NoSession(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	err := o.AgentTakeover(context.Background(), "nonexistent_session_xyz", 1)
	if err == nil {
		t.Error("不存在的会话应返回错误")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("错误信息应包含 'session not found'，实际: %v", err)
	}
}

// TestSmartCSOrchestrator_AgentReply_NoSession 座席回复不存在的会话
func TestSmartCSOrchestrator_AgentReply_NoSession(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	err := o.AgentReply(context.Background(), "nonexistent_session_xyz", 1, "您好")
	if err == nil {
		t.Error("不存在的会话应返回错误")
	}
}

// TestSmartCSOrchestrator_IsAgentOnline 座席在线检查（nil repo 安全）
func TestSmartCSOrchestrator_IsAgentOnline_NilRepo(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	if o.isAgentOnline(context.Background(), 99999) {
		t.Error("不存在的座席应不在线")
	}
}

// TestSmartCSOrchestrator_IncomingContext 入站上下文构造
func TestSmartCSOrchestrator_IncomingContext(t *testing.T) {
	in := &IncomingContext{
		Platform:   model.Platform("wecom"),
		AccountID:  "acc1",
		SenderID:   "user1",
		SenderName: "张三",
		Content:    "你好",
		MessageID:  "msg1",
		MediaURL:   "https://example.com/img.jpg",
		OneID:      "one_001",
	}
	if in.Platform != model.Platform("wecom") {
		t.Error("Platform 不匹配")
	}
	if in.SenderName != "张三" {
		t.Error("SenderName 不匹配")
	}
	if in.OneID != "one_001" {
		t.Error("OneID 不匹配")
	}
}

// TestSmartCSOrchestrator_HandleResult HandleResult 结构
func TestSmartCSOrchestrator_HandleResult(t *testing.T) {
	r := &HandleResult{
		SessionID:      "sess_1",
		HandlerType:    model.HandlerTypeAI,
		AIReplied:      true,
		Reply:          "您好",
		Confidence:     0.85,
		Transferred:    false,
		TransferReason: "",
	}
	if r.HandlerType != model.HandlerTypeAI {
		t.Error("HandlerType 应为 AI")
	}
	if !r.AIReplied {
		t.Error("AIReplied 应为 true")
	}
	if r.Confidence != 0.85 {
		t.Errorf("Confidence 应为 0.85，实际 %.2f", r.Confidence)
	}
}

func setupOrchestratorFindOrCreateTestDB(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
	)
	db.SetTestDB(database)
}

// TestSmartCSOrchestrator_FindOrCreateSession_DerivesOneIDFromPlatformSender
// 兜底：OneID 为空时，会话 OneID 字段应自动拼接 Platform:SenderID。
// 适用场景：未通过用户实名/手机号识别出的访客（如匿名 Web 访客首次进入），
// 后续同一 Platform+SenderID 的消息可命中 OneID 命中活跃会话。
func TestSmartCSOrchestrator_FindOrCreateSession_DerivesOneIDFromPlatformSender(t *testing.T) {
	setupOrchestratorFindOrCreateTestDB(t)

	o := NewSmartCSOrchestrator(nil, DefaultOrchestratorConfig(), nil)
	ctx := context.Background()

	in := &IncomingContext{
		Platform:  model.Platform("web"),
		AccountID: "acct-1",
		SenderID:  "visitor-007",
		Content:   "你好",
		MessageID: "msg-1",
	}
	sess, err := o.findOrCreateSession(ctx, in)
	if err != nil {
		t.Fatalf("findOrCreateSession 失败: %v", err)
	}
	if sess == nil {
		t.Fatal("应返回会话")
	}
	wantDerived := "web:visitor-007"
	if sess.OneID != wantDerived {
		t.Fatalf("OneID 应为派生值 %q，实际 %q", wantDerived, sess.OneID)
	}
	if sess.UserID != "visitor-007" {
		t.Fatalf("UserID 不匹配: got=%q", sess.UserID)
	}
	if sess.Platform != model.Platform("web") {
		t.Fatalf("Platform 不匹配: got=%q", sess.Platform)
	}
}

// TestSmartCSOrchestrator_FindOrCreateSession_GroupScoped
// 群聊（需求3）：会话应按 groupID 建独立会话（UserID/OneID 前缀 group:），
// 避免把群内不同成员当成不同客户/不同会话；同群后续消息应命中同一会话。
func TestSmartCSOrchestrator_FindOrCreateSession_GroupScoped(t *testing.T) {
	setupOrchestratorFindOrCreateTestDB(t)

	o := NewSmartCSOrchestrator(nil, DefaultOrchestratorConfig(), nil)
	ctx := context.Background()

	in1 := &IncomingContext{
		Platform:   model.Platform("xhs"),
		AccountID:  "acct-1",
		SenderID:   "group-1",
		SenderName: "张三",
		Content:    "@客服 帮我查订单",
		MessageID:  "g-msg-1",
		IsGroup:    true,
		GroupID:    "group-1",
		GroupName:  "产品交流群",
	}
	sess1, err := o.findOrCreateSession(ctx, in1)
	if err != nil {
		t.Fatalf("群聊建会话失败: %v", err)
	}
	if sess1 == nil {
		t.Fatal("应返回群会话")
	}
	wantGroupKey := "group:group-1"
	if sess1.UserID != wantGroupKey || sess1.OneID != wantGroupKey {
		t.Fatalf("群会话键错误: UserID=%q OneID=%q, 期望 %q", sess1.UserID, sess1.OneID, wantGroupKey)
	}
	if sess1.UserName != "产品交流群" {
		t.Fatalf("群名应作会话名: %q", sess1.UserName)
	}

	in2 := &IncomingContext{
		Platform:   model.Platform("xhs"),
		AccountID:  "acct-1",
		SenderID:   "group-1",
		SenderName: "李四",
		Content:    "我也要查",
		MessageID:  "g-msg-2",
		IsGroup:    true,
		GroupID:    "group-1",
		GroupName:  "产品交流群",
	}
	sess2, err := o.findOrCreateSession(ctx, in2)
	if err != nil {
		t.Fatalf("群聊命中会话失败: %v", err)
	}
	if sess2.SessionID != sess1.SessionID {
		t.Fatalf("同群应命中同一会话: got=%q want=%q", sess2.SessionID, sess1.SessionID)
	}
}

// TestSmartCSOrchestrator_FindOrCreateSession_HonorsExplicitOneID
// 验证显式 OneID 优先于派生 OneID（兜底不应覆盖显式值）。
func TestSmartCSOrchestrator_FindOrCreateSession_HonorsExplicitOneID(t *testing.T) {
	setupOrchestratorFindOrCreateTestDB(t)

	o := NewSmartCSOrchestrator(nil, DefaultOrchestratorConfig(), nil)
	ctx := context.Background()

	in := &IncomingContext{
		Platform:  model.Platform("telegram"),
		AccountID: "tg-acct-1",
		SenderID:  "tg-user-9",
		Content:   "hi",
		MessageID: "msg-2",
		OneID:     "phone:13800138000",
	}
	sess, err := o.findOrCreateSession(ctx, in)
	if err != nil {
		t.Fatalf("findOrCreateSession 失败: %v", err)
	}
	if sess.OneID != "phone:13800138000" {
		t.Fatalf("OneID 应保留显式值，实际 %q", sess.OneID)
	}
}

// TestSmartCSOrchestrator_FindOrCreateSession_DerivedOneIDMergesSameUser
// 验证派生 OneID 在 TTL 内能合并同 Platform+SenderID 的后续会话。
// 流程：第一次建会话，第二次同 SenderID（不同 MessageID）应命中活跃会话。
func TestSmartCSOrchestrator_FindOrCreateSession_DerivedOneIDMergesSameUser(t *testing.T) {
	setupOrchestratorFindOrCreateTestDB(t)

	o := NewSmartCSOrchestrator(nil, DefaultOrchestratorConfig(), nil)
	ctx := context.Background()

	first, err := o.findOrCreateSession(ctx, &IncomingContext{
		Platform:  model.Platform("web"),
		AccountID: "acct-1",
		SenderID:  "visitor-008",
		Content:   "first",
		MessageID: "msg-A",
	})
	if err != nil {
		t.Fatalf("first findOrCreateSession 失败: %v", err)
	}
	if first.OneID != "web:visitor-008" {
		t.Fatalf("派生 OneID 错误: got=%q", first.OneID)
	}

	second, err := o.findOrCreateSession(ctx, &IncomingContext{
		Platform:  model.Platform("web"),
		AccountID: "acct-1",
		SenderID:  "visitor-008",
		Content:   "second",
		MessageID: "msg-B",
	})
	if err != nil {
		t.Fatalf("second findOrCreateSession 失败: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("应命中同一会话；first=%s second=%s", first.SessionID, second.SessionID)
	}
}

// ------------------------------------------------ T-P2-06：订单草稿生产者注入面 ----

// 这几条测的是"挂上去的那一段"本身，不是整条会话链：HandleIncomingWithAgent 尾部只剩
// 一行 `if produce != nil { go runOrderDraftProduce(...) }`，判据（nil / 空回复 / 超时
// ctx / panic 兜底）全在 runOrderDraftProduce 里，所以这里可以直接驱动它。
// 装配侧（谁把闭包挂上来、挂的是不是生产那一份）见 internal/app/order_draft_wiring_test.go。

func TestRunOrderDraftProduce_NilProducerIsNoop(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	// 未注入（= 旗子 off 的形态）：调用不该 panic，也不该有副作用
	o.runOrderDraftProduce("cust-nil", &SalesResponse{Reply: "光子嫩肤 3 次 2280 元"})

	// 注入后再清空：与"从没注入过"逐字等价（app 层 attach 失败时靠这个回到零改动）
	called := 0
	o.SetOrderDraftProducer(func(context.Context, string, string, *SalesResponse) { called++ })
	o.runOrderDraftProduce("cust-nil", &SalesResponse{Reply: "x"})
	if called != 1 {
		t.Fatalf("注入后应调用一次，实际 %d", called)
	}
	o.SetOrderDraftProducer(nil)
	o.runOrderDraftProduce("cust-nil", &SalesResponse{Reply: "x"})
	if called != 1 {
		t.Errorf("清空后不应再调用，实际累计 %d（传 nil 与不调 setter 必须等价）", called)
	}
}

// 参数与 ctx：客户 ID 原样透传、归属留空由建草稿侧落 "system"、ctx 带 10s 上界且未取消。
func TestRunOrderDraftProduce_ArgsAndFreshDeadline(t *testing.T) {
	var (
		gotCustomer, gotOwner, gotReply string
		gotResp                         *SalesResponse
		gotDeadline                     time.Time
		hasDeadline                     bool
		gotErr                          error
	)
	o := NewSmartCSOrchestrator(nil, nil, nil)
	o.SetOrderDraftProducer(func(ctx context.Context, customerID, ownerID string, resp *SalesResponse) {
		gotCustomer, gotOwner, gotResp = customerID, ownerID, resp
		gotReply = resp.Reply
		gotDeadline, hasDeadline = ctx.Deadline()
		gotErr = ctx.Err()
	})

	resp := &SalesResponse{Reply: "好的，光子嫩肤 3 次 2280 元"}
	// 请求 ctx 此刻已随响应返回而取消 —— 这正是实现里挂新 ctx 的原因，
	// 所以这里断的是"传进来的那一份可继续用"。
	o.runOrderDraftProduce("cust_deadline", resp)

	if gotCustomer != "cust_deadline" {
		t.Errorf("customerID=%q，期望原样透传", gotCustomer)
	}
	if gotOwner != "" {
		t.Errorf("ownerID=%q，期望空串（会话侧没有稳定归属，交给 CreateFromIntent 落 system）", gotOwner)
	}
	if gotResp != resp || gotReply != resp.Reply {
		t.Errorf("应把同一条响应交给生产者，实际 resp=%v reply=%q", gotResp != resp, gotReply)
	}
	if !hasDeadline {
		t.Fatal("生产者拿到的 ctx 必须带超时（库卡住时 goroutine 要有收口）")
	}
	if gotErr != nil {
		t.Errorf("ctx 不应已取消: %v", gotErr)
	}
	left := time.Until(gotDeadline)
	if left <= 0 || left > orderDraftProduceTimeout {
		t.Errorf("剩余时限 %s 应落在 (0, %s] 区间", left, orderDraftProduceTimeout)
	}
}

// 空回复不建草稿：AI 没说话时也去提意向，等于把知识库里的一句报价当成客户下的单。
func TestRunOrderDraftProduce_EmptyReplySkips(t *testing.T) {
	called := 0
	o := NewSmartCSOrchestrator(nil, nil, nil)
	o.SetOrderDraftProducer(func(context.Context, string, string, *SalesResponse) { called++ })

	for _, c := range []struct {
		name string
		resp *SalesResponse
	}{
		{"nil 响应", nil},
		{"空回复", &SalesResponse{}},
		{"纯空白回复", &SalesResponse{Reply: "   \n\t "}},
	} {
		o.runOrderDraftProduce("cust_empty", c.resp)
		if called != 0 {
			t.Fatalf("%s：不应调用生产者，实际调了 %d 次", c.name, called)
		}
	}
	o.runOrderDraftProduce("cust_ok", &SalesResponse{Reply: "水光针 1 次 980"})
	if called != 1 {
		t.Errorf("非空回复应调用一次，实际 %d", called)
	}
}

// 生产者 panic 必须在 goroutine 里被吞掉：没兜住的话整个进程会随一次提取失败而挂掉。
//
// 这条是反向测试的固定靶：把 defer/recover 删掉，测试进程会 panic 退出（而不是用例 FAIL），
// 所以它同时也是"这条路径确实需要 recover"的证据。
func TestRunOrderDraftProduce_PanicRecovered(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	o.SetOrderDraftProducer(func(context.Context, string, string, *SalesResponse) {
		panic("boom: 提取器内部炸了")
	})
	done := make(chan struct{})
	go func() {
		defer func() { close(done) }()
		o.runOrderDraftProduce("cust_panic", &SalesResponse{Reply: "光子嫩肤"})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("生产者 panic 后 runOrderDraftProduce 没返回")
	}

	// 真 panic（非自定义 error 类型）也要接得住
	o.SetOrderDraftProducer(func(context.Context, string, string, *SalesResponse) {
		var p *OrderDraft
		_ = p.ID // 空指针解引用：与实现里 debug.Stack() 那条路径同型
	})
	o.runOrderDraftProduce("cust_nilptr", &SalesResponse{Reply: "光子嫩肤"})
}

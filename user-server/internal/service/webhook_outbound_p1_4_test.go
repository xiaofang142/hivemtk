package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestSendOutbound_BridgeChannel_ExtraMetadata_P1_4(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.InboxConversation{})
	// -count=N 隔离：释放上一轮固定 EventID 的出站认领
	releaseReplyClaimForTest(t, "evt-p14-1")
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const (
		platform = "xiaohongshu"
		account  = "acct-p14-real"
		conv     = "conv-p14-1"
	)
	hub := &model.MessageHub{
		MsgID:          "mh:in-p14-1",
		Platform:       platform,
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "sender-1",
		SenderName:     "客户甲",
		Content:        "原始消息",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("pre-create inbound failed: %v", err)
	}
	p := &ParsedPayload{
		EventID: "evt-p14-1",
		Sender:  "sender-1",
		Content: "原始消息",
		ChatID:  conv,
	}

	result := &HandleResult{
		HandlerType: model.HandlerTypeAI,
		AIReplied:   true,
		Confidence:  0.86,
		Reply:       "您好，关于代码二开请查看 Gitee AGPL-3.0 协议。",
		SalesResponse: &SalesResponse{
			Intent: &dto.RecognizeResult{IntentType: "inquiry"},
		},
	}
	ctx := AgentIDToContext(HandleResultToContext(context.Background(), result), "sales-prod-v1")

	svc.sendOutbound(ctx, ChannelXiaohongshu, account, p, result.Reply, hub, nil)

	var out model.MessageHub
	err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
		Order("id DESC").First(&out).Error
	if err != nil {
		t.Fatalf("query outbound failed: %v", err)
	}

	if out.Extra == nil {
		t.Fatalf("outbound.Extra must not be nil, got nil (P1-4 修复目标)")
	}
	if v, _ := out.Extra["dm_target"].(string); v != "conv" {
		t.Errorf("Extra.dm_target expected 'conv', got %q", v)
	}
	if v, _ := out.Extra["scenario"].(string); v != "auto_reply" {
		t.Errorf("Extra.scenario expected 'auto_reply', got %q", v)
	}
	if v, _ := out.Extra["triggered_by"].(string); v != "ai_dispatch" {
		t.Errorf("Extra.triggered_by expected 'ai_dispatch', got %q", v)
	}
	if v, _ := out.Extra["agent_id"].(string); v != "sales-prod-v1" {
		t.Errorf("Extra.agent_id expected 'sales-prod-v1' (from ctx), got %q", v)
	}
	if v, _ := out.Extra["confidence"].(float64); v != 0.86 {
		t.Errorf("Extra.confidence expected 0.86 (from HandleResult), got %v", v)
	}
	if v, _ := out.Extra["intent"].(string); v != "inquiry" {
		t.Errorf("Extra.intent expected 'inquiry' (from SalesResponse.Intent), got %q", v)
	}
	if v, _ := out.Extra["handler_type"].(string); v != string(model.HandlerTypeAI) {
		t.Errorf("Extra.handler_type expected %q, got %q", model.HandlerTypeAI, v)
	}
}

// 占位账号（前端 getAccountId DOM 兜底失败 → account_id=*-unknown）必须被识别为
// undeliverable，Extra 补 undeliverable_reason + scenario=undeliverable，message_hub
// 直接落 status=failed，避免污染 pending 出库队列。
func TestSendOutbound_PlaceholderAccount_Undeliverable_P1_4(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.InboxConversation{})
	// -count=N 隔离：释放上一轮固定 EventID 的出站认领
	releaseReplyClaimForTest(t, "evt-p14-undel")
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const (
		platform = "xiaohongshu"
		conv     = "conv-p14-undel"
	)
	hub := &model.MessageHub{
		MsgID:          "mh:in-p14-undel",
		Platform:       platform,
		AccountID:      "xiaohongshu-unknown",
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "sender-undel",
		Content:        "原始消息",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("pre-create inbound failed: %v", err)
	}
	p := &ParsedPayload{EventID: "evt-p14-undel", Sender: "sender-undel", Content: "原始消息", ChatID: conv}

	result := &HandleResult{
		HandlerType: model.HandlerTypeAI,
		AIReplied:   true,
		Confidence:  0.7,
		Reply:       "AI 回复（但账号不可达）",
	}
	ctx := HandleResultToContext(context.Background(), result)
	svc.sendOutbound(ctx, ChannelXiaohongshu, "xiaohongshu-unknown", p, result.Reply, hub, nil)

	var out model.MessageHub
	err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
		Order("id DESC").First(&out).Error
	if err != nil {
		t.Fatalf("query outbound failed: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("placeholder account should mark status=failed, got %q", out.Status)
	}
	if v, _ := out.Extra["scenario"].(string); v != "undeliverable" {
		t.Errorf("Extra.scenario expected 'undeliverable', got %q", v)
	}
	if v, _ := out.Extra["undeliverable_reason"].(string); !strings.Contains(v, "placeholder") {
		t.Errorf("Extra.undeliverable_reason expected to contain 'placeholder', got %q", v)
	}
}

// ctx 没有 HandleResult（异常路径,如 runAIGeneration panic 后兜底）时,Extra 仍要
// 有稳定的 dm_target/scenario/triggered_by，但 confidence/intent/handler_type 缺省
// 显式填空为 "unknown"（不允许 nil，否则 trace_learning group by key 漂移）。
func TestSendOutbound_NoCtxResult_StillFillsStableFields_P1_4(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.InboxConversation{})
	// -count=N 隔离：释放上一轮固定 EventID 的出站认领
	releaseReplyClaimForTest(t, "evt-p14-noctx")
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	const (
		platform = "douyin"
		account  = "acct-p14-noctx"
		conv     = "conv-p14-noctx-1"
	)
	hub := &model.MessageHub{
		MsgID:          "mh:in-p14-noctx",
		Platform:       platform,
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "sender-1",
		Content:        "原始消息",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("pre-create inbound failed: %v", err)
	}
	p := &ParsedPayload{EventID: "evt-p14-noctx", Sender: "sender-1", Content: "原始消息", ChatID: conv}

	svc.sendOutbound(context.Background(), ChannelDouyin, account, p, "AI 回复（无 ctx）", hub, nil)

	var out model.MessageHub
	err := db.Where("direction = ? AND conversation_id = ?", "outbound", conv).
		Order("id DESC").First(&out).Error
	if err != nil {
		t.Fatalf("query outbound failed: %v", err)
	}
	if out.Extra == nil {
		t.Fatalf("outbound.Extra must not be nil even without ctx HandleResult")
	}
	for _, key := range []string{"dm_target", "scenario", "triggered_by"} {
		if _, ok := out.Extra[key]; !ok {
			t.Errorf("Extra.%s must exist even without ctx (stable field), got nil", key)
		}
	}
	if v, _ := out.Extra["agent_id"].(string); v != "unknown" {
		t.Errorf("Extra.agent_id expected 'unknown' (no ctx), got %q", v)
	}
}

// TestIsAIReplyQuietHours_Boundaries 静默窗口边界：22:59 放行 / 23:00 起 / 06:59 内 / 07:00 放行
func TestIsAIReplyQuietHours_Boundaries(t *testing.T) {
	cases := []struct {
		hour, minute int
		want         bool
	}{
		{22, 59, false},
		{23, 0, true},
		{23, 1, true},
		{0, 0, true},
		{6, 59, true},
		{7, 0, false},
		{12, 0, false},
	}
	for _, c := range cases {
		ts := time.Date(2026, 8, 20, c.hour, c.minute, 0, 0, cstZone)
		if got := isAIReplyQuietHours(ts); got != c.want {
			t.Errorf("isAIReplyQuietHours(%02d:%02d CST) = %v, want %v", c.hour, c.minute, got, c.want)
		}
	}
}

// TestEnqueueDelayedOutbound 命中入队：落库 pending 记录且 SendAt 为次日 07:00 CST 首发
func TestEnqueueDelayedOutbound(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)

	p := &ParsedPayload{EventID: "evt-h3-1", Sender: "sender-h3", Content: "原始消息"}
	hub := &model.MessageHub{ConversationID: "conv-h3-1"}

	ok := svc.enqueueDelayedOutbound(context.Background(), ChannelXiaohongshu, "acct-h3", p, "延迟回复内容", hub, nil)
	if !ok {
		t.Fatal("入队应成功")
	}
	var rec DelayedOutboundReply
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("查询延迟记录失败: %v", err)
	}
	if rec.Status != "pending" {
		t.Errorf("status 应为 pending，got %q", rec.Status)
	}
	if rec.Platform != string(ChannelXiaohongshu) || rec.ConversationID != "conv-h3-1" || rec.Content != "延迟回复内容" {
		t.Errorf("字段不符: %+v", rec)
	}

	sendAtCST := rec.SendAt.In(cstZone)
	if !rec.SendAt.After(time.Now()) {
		t.Errorf("SendAt 应在未来，got %s", rec.SendAt)
	}
	if sendAtCST.Hour() != aiReplyQuietEndHour || sendAtCST.Minute() != 0 {
		t.Errorf("SendAt 应为 07:00 CST 首发点，got %s", sendAtCST)
	}
}

// TestDispatchDueDelayedOutbound 到期记录被抢占重放并标记 sent；重放路径不再二次入队
func TestDispatchDueDelayedOutbound(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)

	rec := &DelayedOutboundReply{
		Platform:       "unsupported-channel-x",
		AccountID:      "acct-h3",
		ConversationID: "conv-h3-d",
		SenderID:       "sender-h3",
		Content:        "due",
		SendAt:         time.Now().Add(-time.Minute),
		Status:         "pending",
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("预置到期记录失败: %v", err)
	}

	svc.dispatchDueDelayedOutbound(context.Background())

	var after DelayedOutboundReply
	if err := db.First(&after, rec.ID).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if after.Status == "pending" {
		t.Error("到期记录应被派发器抢占并处理，不应停留在 pending")
	}

	future := &DelayedOutboundReply{
		Platform: "x", AccountID: "a", ConversationID: "c", SenderID: "s", Content: "future",
		SendAt: time.Now().Add(time.Hour), Status: "pending",
	}
	if err := db.Create(future).Error; err != nil {
		t.Fatalf("预置未到期记录失败: %v", err)
	}
	svc.dispatchDueDelayedOutbound(context.Background())
	var futAfter DelayedOutboundReply
	if err := db.First(&futAfter, future.ID).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if futAfter.Status != "pending" {
		t.Errorf("未到期记录应保持 pending，got %q", futAfter.Status)
	}
}

// TestDelayedReplayContext 重放标记 ctx 生效（防循环入队）
func TestDelayedReplayContext(t *testing.T) {
	ctx := context.Background()
	if isDelayedReplay(ctx) {
		t.Error("普通 ctx 不应为重放路径")
	}
	if !isDelayedReplay(DelayedReplayToContext(ctx)) {
		t.Error("标记后的 ctx 应为重放路径")
	}
}

func init() {
	storeAIReplyQuietHoursFn(func(time.Time) bool { return false })
}

// TestSendOutbound_QuietHoursDefersToQueue H-3 端到端：窗口内 AI 回复不直发，入延迟队列
func TestSendOutbound_QuietHoursDefersToQueue(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	orig := loadAIReplyQuietHoursFn()
	storeAIReplyQuietHoursFn(func(time.Time) bool { return true })
	defer func() { storeAIReplyQuietHoursFn(orig) }()

	const (
		platform = "xiaohongshu"
		account  = "acct-h3-e2e"
		conv     = "conv-h3-e2e"
	)
	hub := &model.MessageHub{
		MsgID: "mh:in-h3-e2e", Platform: platform, AccountID: account, Direction: "inbound",
		MsgType: "text", SenderID: "sender-h3", Content: "原始消息", ConversationID: conv, SentAt: time.Now(),
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("pre-create inbound failed: %v", err)
	}
	p := &ParsedPayload{EventID: "evt-h3-e2e", Sender: "sender-h3", Content: "原始消息", ChatID: conv}

	svc.sendOutbound(context.Background(), ChannelXiaohongshu, account, p, "夜间回复", hub, nil)

	var cnt int64
	db.Model(&DelayedOutboundReply{}).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("quiet hours 内应入延迟队列 1 条，got %d", cnt)
	}
	var outCnt int64
	db.Model(&model.MessageHub{}).Where("direction = ? AND conversation_id = ?", "outbound", conv).Count(&outCnt)
	if outCnt != 0 {
		t.Errorf("延迟期间不应产生出站消息，got %d", outCnt)
	}
}

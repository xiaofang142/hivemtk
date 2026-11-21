package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"
)

func TestBridgeObservation_ResultExitIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		result        *HandleResult
	}{
		{name: "控制字不是成功交付", outcome: "no_reply", result: &HandleResult{AIReplied: true, Reply: "NO_REPLY"}},
		{name: "已人工没有新公告", outcome: "handoff", result: &HandleResult{Transferred: true, HandlerType: model.HandlerTypeHuman}},
		{name: "缺锁前置明确拒发", outcome: "lock_unconfirmed", result: &HandleResult{AIReplied: true, Reply: "合成答案"}},
		{name: "生成空结果可诊断", outcome: "nil_result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, baseCtx := newBridgeSentTestService(t)
			t.Cleanup(func() { ws.Stop(baseCtx) })
			database := testutil.NewTestDB(t, &model.MessageTrace{})
			hub := bridgeInboundHub("douyin", "result-observation-account", "result-observation-conv", "customer")
			hub.TenantID = c01TenantA
			turn := tracing.BusinessTurnID(c01TenantA, hub.Platform, hub.AccountID, hub.ConversationID, hub.MsgID)
			ctx := tracing.WithCarrier(tenant.WithTenantID(baseCtx, c01TenantA), &tracing.Carrier{TurnID: turn,
				Channel: hub.Platform, AccountID: hub.AccountID, ConversationID: hub.ConversationID, MsgID: hub.MsgID})
			ws.deliverAIResult(ctx, webhookReplyRequest{channel: ChannelDouyin, accountID: hub.AccountID, hub: hub,
				payload: &ParsedPayload{EventID: "result-observation", ChatID: hub.ConversationID}}, tc.result, nil)
			var rows []model.MessageTrace
			if err := database.Where("node = ?", "ai_result").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatal("每次生成的安全出口必须独立可观测，不能只看生成成功")
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(rows[0].Output), &out); err != nil {
				t.Fatal(err)
			}
			if out["outcome"] != tc.outcome || out["enqueued"] != false || rows[0].TurnID != turn || rows[0].AnswerAttempt != 1 || !rows[0].DurationMeasured {
				t.Fatalf("必须区分决策和交付并保留同轮身份：%+v", out)
			}
		})
	}
}

func TestBridgeObservation_EnqueueStartsBeforePersistence(t *testing.T) {
	ws, baseCtx := newBridgeSentTestService(t)
	t.Cleanup(func() { ws.Stop(baseCtx) })
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	hub := bridgeInboundHub("douyin", "observation-account", "observation-conversation", "observation-customer")
	hub.TenantID = c01TenantA
	turnID := tracing.BusinessTurnID(c01TenantA, hub.Platform, hub.AccountID, hub.ConversationID, hub.MsgID)
	ctx := tracing.WithCarrier(tenant.WithTenantID(baseCtx, c01TenantA), &tracing.Carrier{
		TenantID: c01TenantA, TraceID: "observation-trace", TurnID: turnID, MsgID: hub.MsgID, Direction: "inbound", AnswerAttempt: 1,
		Channel: hub.Platform, AccountID: hub.AccountID, ConversationID: hub.ConversationID,
	})
	// 真实出站关联会查询已落库的入站追踪，不能用不存在的固定ID伪造前置。
	if err := database.Create(&model.MessageTrace{TenantID: c01TenantA, TraceID: "observation-trace",
		Channel: hub.Platform, AccountID: hub.AccountID, ConversationID: hub.ConversationID,
		MsgID: hub.MsgID, Direction: "inbound", Node: tracing.NodeIngest}).Error; err != nil {
		t.Fatal(err)
	}
	eventID := "observation-enqueue"
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContext(t, ctx, hub.Platform, hub.AccountID, "text")
	releaseReplyClaimForTest(t, eventID)
	sent, err := ws.sendOutbound(ctx, ChannelDouyin, hub.AccountID,
		&ParsedPayload{EventID: eventID, ChatID: hub.ConversationID, Sender: hub.SenderID}, "合成完整答案", hub, nil)
	if !sent || err != nil {
		t.Fatalf("正常答案应该入队：%v", err)
	}
	var out model.MessageHub
	if err := database.Where("direction = ? AND status = ?", "outbound", "pending").First(&out).Error; err != nil {
		t.Fatal(err)
	}
	var node model.MessageTrace
	if err := database.Where("node = ? AND msg_id = ?", tracing.NodeOutboundEnqueue, out.MsgID).First(&node).Error; err != nil {
		t.Fatal(err)
	}
	if node.StartedAt == nil || node.StartedAt.After(out.CreatedAt) || !node.DurationMeasured {
		t.Fatal("入队操作必须在写库之前开始实测，不能写库结束后才开始")
	}
	if node.TurnID != turnID || node.AnswerAttempt != 1 || out.Extra["turn_id"] != turnID {
		t.Fatal("同轮身份必须进入trace和出站关联载荷")
	}
	if node.TraceID != "observation-trace" {
		t.Fatal("临时入站对象不能用空追踪覆盖同范围已确认追踪")
	}
	secondID := "observation-enqueue-duplicate"
	releaseReplyClaimForTest(t, secondID)
	_, _ = ws.sendOutbound(ctx, ChannelDouyin, hub.AccountID,
		&ParsedPayload{EventID: secondID, ChatID: hub.ConversationID, Sender: hub.SenderID}, "合成完整答案", hub, nil)
	var nodes []model.MessageTrace
	if err := database.Where("node = ?", tracing.NodeOutboundEnqueue).Order("id").Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[1].Status == tracing.StatusOk {
		t.Fatal("零ID重复占位不能冒充成功入队")
	}
}

func TestBridgeObservation_AckMeasuresActualUpdate(t *testing.T) {
	ws, baseCtx := newBridgeSentTestService(t)
	t.Cleanup(func() { ws.Stop(baseCtx) })
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	ctx := tenant.WithTenantID(baseCtx, c01TenantA)
	turnID := tracing.BusinessTurnID(c01TenantA, "douyin", "observation-ack-account", "observation-ack-conv", "inbound-ack")
	claimed := time.Now().Add(-time.Second)
	hub := &model.MessageHub{TenantID: c01TenantA, Platform: "douyin", AccountID: "observation-ack-account",
		ConversationID: "observation-ack-conv", MsgID: "observation-ack-out", Direction: "outbound", MsgType: "text",
		Status: "inflight", ClaimedAt: &claimed, PushAttempts: 2,
		Extra: model.JSONMap{"turn_id": turnID, "answer_attempt": 2}, SentAt: time.Now()}
	if err := database.Create(hub).Error; err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := ws.ingressSvc.AckOutboundDeliveredDetailed(ctx, hub.Platform, hub.AccountID, []string{hub.MsgID}, hub.ConversationID, "delivered", nil)
	if err != nil || result.AckedItemsCount != 1 {
		t.Fatalf("真实ACK失败：%v", err)
	}
	var node model.MessageTrace
	if err := database.Where("node = ? AND msg_id = ?", tracing.NodeDeliveredAck, hub.MsgID).First(&node).Error; err != nil {
		t.Fatal(err)
	}
	// 断言意图：StartedAt 必须是本次 ACK 操作的起点（早于它的只有调用前的时刻）。
	// 注意粒度陷阱：message_trace.started_at 是 PG timestamptz（微秒精度），
	// 而测试侧 started 是本地纳秒时钟——同微秒内启动时，回读值被截断到微秒后
	// 会比 started 早最多 999ns，纳秒级 Before() 必假红（2026-11-19 收口：本用例
	// 在 454514.0µs vs 454514.4µs 上稳定复现）。故按微秒粒度比较，意图不变。
	if !node.DurationMeasured || node.StartedAt == nil ||
		node.StartedAt.UnixMicro() < started.UnixMicro() || node.TurnID != turnID || node.DeliveryAttempt != 2 {
		t.Fatal("ACK必须包含实际更新操作计时及出站作用域/尝试")
	}
}

func TestBridgeObservation_IngestPersistsAuthoritativeTurn(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.MessageTrace{})
	svc := NewInboxIngressService()
	svc.hubRepo = repository.NewMessageHubRepositoryWithDB(database)
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	event := &model.MessageEvent{EventID: "observation-ingest", Channel: "douyin", ConversationID: "observation-ingest-conv",
		SenderID: "observation-customer", SenderType: "customer", Content: "合成客户事实", Timestamp: time.Now(),
		Extra: map[string]any{"account_id": "observation-ingest-account", "turn_id": "untrusted-client-turn"}}
	if err := svc.persistMessage(ctx, event); err != nil {
		t.Fatal(err)
	}
	var hub model.MessageHub
	if err := database.Where("msg_id = ?", event.EventID).First(&hub).Error; err != nil {
		t.Fatal(err)
	}
	turnID := tracing.BusinessTurnID(c01TenantA, event.Channel, "observation-ingest-account", event.ConversationID, event.EventID)
	var node model.MessageTrace
	if err := database.Where("node = ? AND msg_id = ?", tracing.NodeIngest, event.EventID).First(&node).Error; err != nil {
		t.Fatal(err)
	}
	if hub.Extra["turn_id"] != turnID || node.TurnID != turnID || !node.DurationMeasured {
		t.Fatal("入站必须使用权威作用域，不信任客户端自称turn")
	}
}

func TestBridgeObservation_RegenerationHasOwnAttempt(t *testing.T) {
	ws, baseCtx := newBridgeSentTestService(t)
	t.Cleanup(func() { ws.Stop(baseCtx) })
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	hub := bridgeInboundHub("douyin", "observation-rerun-account", "observation-rerun-conv", "observation-customer")
	hub.TenantID = c01TenantA
	reply := "已经排队的合成答案"
	if err := database.Create(&model.MessageHub{TenantID: c01TenantA, Platform: hub.Platform, AccountID: hub.AccountID,
		ConversationID: hub.ConversationID, Direction: "outbound", Status: "delivered", MsgType: "text", Content: reply,
		MsgID: ContentHashMsgID(hub.Platform, hub.ConversationID, reply), SentAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	turnID := tracing.BusinessTurnID(c01TenantA, hub.Platform, hub.AccountID, hub.ConversationID, hub.MsgID)
	ctx := tracing.WithCarrier(tenant.WithTenantID(baseCtx, c01TenantA), &tracing.Carrier{
		TraceID: "observation-rerun-trace", TurnID: turnID, MsgID: hub.MsgID, AnswerAttempt: 1,
		Channel: hub.Platform, AccountID: hub.AccountID, ConversationID: hub.ConversationID,
	})
	eventID := "observation-rerun"
	useLegacyBridgeFixture(t)
	ctx = localBridgeSubmitContext(t, ctx, hub.Platform, hub.AccountID, "text")
	releaseReplyClaimForTest(t, eventID)
	ws.deliverAIResult(ctx, webhookReplyRequest{channel: ChannelDouyin, accountID: hub.AccountID, hub: hub,
		payload:  &ParsedPayload{EventID: eventID, ChatID: hub.ConversationID, Sender: hub.SenderID},
		incoming: &IncomingContext{MessageID: eventID, Platform: model.Platform(hub.Platform)},
	}, &HandleResult{SessionID: "observation-session", AIReplied: true, Reply: reply},
		func(context.Context, *IncomingContext, *AgentContext) (*HandleResult, error) {
			return &HandleResult{SessionID: "observation-session", AIReplied: true, Reply: "NO_REPLY"}, nil
		})
	var nodes []model.MessageTrace
	if err := database.Where("node = ? AND turn_id = ?", tracing.NodeAIDispatch, turnID).Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].AnswerAttempt != 2 || !nodes[0].DurationMeasured {
		t.Fatal("重生成需要独立实测尝试，不可隐藏在首次快答耗时中")
	}
}

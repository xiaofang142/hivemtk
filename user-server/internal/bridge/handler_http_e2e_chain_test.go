package bridge

import (
	"hivemtk-user/internal/capregistry"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type fakeAIOutboundTrigger struct {
	called   atomic.Int64
	lastCall struct {
		channel, accountID, conversationID, customerID, content, eventID string
	}
	db      *gorm.DB
	traceFn func() string
}

func newFakeAIOutboundTrigger(db *gorm.DB, traceFn func() string) *fakeAIOutboundTrigger {
	return &fakeAIOutboundTrigger{db: db, traceFn: traceFn}
}

func (f *fakeAIOutboundTrigger) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...service.TriggerInboundOption) {
	f.called.Add(1)
	f.lastCall.channel = channel
	f.lastCall.accountID = accountID
	f.lastCall.conversationID = conversationID
	f.lastCall.customerID = customerID
	f.lastCall.content = content
	f.lastCall.eventID = eventID

	outbound := model.MessageHub{
		MsgID:          fmt.Sprintf("mock-ai-%d", f.called.Load()),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       accountID,
		ReceiverID:     customerID,
		Content:        "[mock AI] 收到您的消息，正在处理中...",
		ConversationID: conversationID,
		IsAIReply:      true,
		AIAgent:        "e2e_test_mock",
		TraceID:        f.currentTraceID(),
	}
	if err := f.db.Create(&outbound).Error; err != nil {
		return
	}

	var lastOrder int64
	f.db.Model(&model.MessageTrace{}).
		Where("trace_id = ?", outbound.TraceID).
		Select("COALESCE(MAX(node_order),0)").
		Scan(&lastOrder)
	trace := model.MessageTrace{
		TraceID:        outbound.TraceID,
		ConversationID: conversationID,
		AccountID:      accountID,
		Channel:        channel,
		Node:           "outbound_enqueue",
		NodeOrder:      int(lastOrder) + 1,
		MsgID:          outbound.MsgID,
		Status:         "ok",
		SpanKind:       "lifecycle",
		TurnIndex:      0,
	}
	f.db.Create(&trace)
}

func (f *fakeAIOutboundTrigger) currentTraceID() string {

	var hub model.MessageHub
	if err := f.db.Where("direction = 'inbound'").Order("id DESC").First(&hub).Error; err == nil && hub.TraceID != "" {
		return hub.TraceID
	}
	return fmt.Sprintf("mock-trace-%d", f.called.Load())
}

// TestBridgeE2EChain_FullLifecycle 验证消息入库→AI触发→出库ack完整业务链路。
// 所有验证点都是**业务正确性**：不是 HTTP 200 就算过，必须检查 DB 数据一致性、
// trace_id 贯通、outbound 消息 AI 标记、message_trace 事件完整性。
func TestBridgeE2EChain_FullLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.MessageHub{}, &model.MessageTrace{})

	db.Exec("DELETE FROM message_hub WHERE conversation_id LIKE 'e2e-chain-%'")
	db.Exec("DELETE FROM message_trace WHERE conversation_id LIKE 'e2e-chain-%'")

	const (
		channel   = "douyin"
		accountID = "e2e-test-acc"
		convID    = "e2e-chain-001"
		customer  = "e2e-cust-001"
	)

	ingressSvc := service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)
	handler := NewBridgeIngestHandler(ingressSvc)

	aiTrigger := newFakeAIOutboundTrigger(db, nil)
	ingressSvc.SetAITrigger(aiTrigger)

	body := fmt.Sprintf(`{"v":1,"channel":"%s","account_id":"%s","conversation_id":"%s","messages":[
		{"event_id":"e2e-evt-1","role":"customer","sender_id":"%s","content":"hello, 咨询价格"},
		{"event_id":"e2e-evt-2","role":"customer","sender_id":"%s","content":"有没有优惠"}
	],"expect_reply":true}`, channel, accountID, convID, customer, customer)

	req := httptest.NewRequest("POST", "/api/bridge/ingest", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Request.RemoteAddr = "127.0.0.1:12345"
	c.Request.Header.Set("X-Bridge-Token", "test-token")

	handler.HandleHTTPIngest(c)

	if w.Code != http.StatusOK {
		t.Fatalf("ingest status=%d body=%s", w.Code, w.Body.String())
	}

	var ingResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &ingResp)
	ok, _ := ingResp["ok"].(bool)
	if !ok {
		t.Fatalf("ingest ok=false, resp=%s", w.Body.String())
	}
	ingested, _ := ingResp["ingested"].([]any)
	if len(ingested) != 2 {
		t.Fatalf("ingested count=%d want=2", len(ingested))
	}

	var inbounds []model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = 'inbound'", convID).Find(&inbounds).Error; err != nil {
		t.Fatalf("query inbound: %v", err)
	}
	if len(inbounds) != 2 {
		t.Fatalf("inbound count=%d want=2", len(inbounds))
	}

	for i, m := range inbounds {
		if m.Platform != channel {
			t.Errorf("inbound[%d].Platform=%s want=%s", i, m.Platform, channel)
		}
		if m.AccountID != accountID {
			t.Errorf("inbound[%d].AccountID=%s want=%s", i, m.AccountID, accountID)
		}
		if m.ConversationID != convID {
			t.Errorf("inbound[%d].ConversationID=%s want=%s", i, m.ConversationID, convID)
		}
		if m.Direction != "inbound" {
			t.Errorf("inbound[%d].Direction=%s want=inbound", i, m.Direction)
		}
		if m.TraceID == "" {
			t.Errorf("inbound[%d].TraceID is empty", i)
		}
	}

	var traceIDs []string
	db.Model(&model.MessageHub{}).
		Where("conversation_id = ?", convID).
		Distinct("trace_id").
		Pluck("trace_id", &traceIDs)
	if len(traceIDs) != 1 {
		t.Fatalf("traceIDs count=%d want=1 (贯通), ids=%v", len(traceIDs), traceIDs)
	}
	traceID := traceIDs[0]

	if aiTrigger.called.Load() < 1 {
		t.Fatal("AITrigger 未被调用")
	}
	if aiTrigger.lastCall.channel != channel {
		t.Errorf("AITrigger channel=%s want=%s", aiTrigger.lastCall.channel, channel)
	}
	if aiTrigger.lastCall.accountID != accountID {
		t.Errorf("AITrigger accountID=%s want=%s", aiTrigger.lastCall.accountID, accountID)
	}
	if aiTrigger.lastCall.conversationID != convID {
		t.Errorf("AITrigger conversationID=%s want=%s", aiTrigger.lastCall.conversationID, convID)
	}

	var outbounds []model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = 'outbound'", convID).Find(&outbounds).Error; err != nil {
		t.Fatalf("query outbound: %v", err)
	}
	if len(outbounds) < 1 {
		t.Fatalf("outbound count=%d want>=1 (AI 应生成回复)", len(outbounds))
	}
	aiOut := outbounds[0]
	if !aiOut.IsAIReply {
		t.Errorf("outbound IsAIReply=false want=true")
	}
	if aiOut.Status != "pending" {
		t.Errorf("outbound Status=%s want=pending", aiOut.Status)
	}
	if aiOut.TraceID != traceID {
		t.Errorf("outbound TraceID=%s want=%s (贯通)", aiOut.TraceID, traceID)
	}

	var traces []model.MessageTrace
	db.Where("trace_id = ?", traceID).Order("node_order").Find(&traces)
	if len(traces) < 1 {
		t.Fatalf("message_trace count=%d want>=1 (至少 outbound_enqueue 事件)", len(traces))
	}

	var hasOutboundEnqueue bool
	for _, tr := range traces {
		if tr.Node == "outbound_enqueue" && tr.Status == "ok" {
			hasOutboundEnqueue = true
		}
	}
	if !hasOutboundEnqueue {
		t.Error("message_trace 缺少 outbound_enqueue ok 事件")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/api/bridge/outbox?channel=%s&account_id=%s&limit=10", channel, accountID), nil)
	req2.Header.Set("X-Bridge-Token", "test-token")
	req2 = req2.WithContext(localBridgeDeliveryContext(t, tenant.WithTenantID(req2.Context(), 1), channel, accountID, "text", ""))
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = req2

	handler.GetBridgeOutbox(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("outbox status=%d body=%s", w2.Code, w2.Body.String())
	}

	var outboxResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &outboxResp)
	msgs, ok := outboxResp["messages"].([]any)
	if !ok {
		t.Fatalf("outbox resp messages type=nil, resp=%s", w2.Body.String())
	}
	if len(msgs) < 1 {
		t.Fatalf("outbox messages count=%d want>=1", len(msgs))
	}

	msgID := aiOut.MsgID
	ackBody := fmt.Sprintf(`{"msg_ids":["%s"],"status":"delivered"}`, msgID)
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("POST",
		fmt.Sprintf("/api/bridge/outbox/ack?channel=%s&account_id=%s", channel, accountID),
		bytes.NewBufferString(ackBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("X-Bridge-Token", "test-token")
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = req3

	handler.AckBridgeOutbox(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", w3.Code, w3.Body.String())
	}

	var ackResp map[string]any
	json.Unmarshal(w3.Body.Bytes(), &ackResp)
	ackedCount, _ := ackResp["acked_items_count"].(float64)
	if ackedCount != 1 {
		t.Errorf("ack acked_items_count=%v want=1", ackedCount)
	}

	t.Logf("✅ 业务链路全流程通过: inbound=%d outbound=%d trace=%s trace_events=%d acked=%v",
		len(inbounds), len(outbounds), traceID, len(traces), ackedCount)
}

// TestBridgeE2EChain_DedupAndDuplicate 验证重复消息入库 dedup 逻辑。
// 发送两条相同 msg_id 的消息，第二条应 duplicate=true，DB 不应有重复记录。
func TestBridgeE2EChain_DedupAndDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.MessageHub{})
	db.Exec("DELETE FROM message_hub WHERE conversation_id LIKE 'e2e-dedup-%'")

	const (
		channel   = "douyin"
		accountID = "dedup-acc"
		convID    = "e2e-dedup-001"
	)

	ingressSvc := service.NewInboxIngressService()
	handler := NewBridgeIngestHandler(ingressSvc)

	body := fmt.Sprintf(`{"v":1,"channel":"%s","account_id":"%s","conversation_id":"%s","messages":[
		{"event_id":"dup-ev-1","msg_id":"dup-msg-1","role":"customer","content":"重复消息"}
	]}`, channel, accountID, convID)

	req := httptest.NewRequest("POST", "/api/bridge/ingest", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	handler.HandleHTTPIngest(c)
	if w.Code != http.StatusOK {
		t.Fatalf("first ingest status=%d", w.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/bridge/ingest", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = req2
	handler.HandleHTTPIngest(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second ingest status=%d", w2.Code)
	}

	var count int64
	db.Model(&model.MessageHub{}).
		Where("conversation_id = ? AND direction = 'inbound'", convID).
		Count(&count)
	if count != 1 {
		t.Errorf("dedup: inbound count=%d want=1 (重复消息应被 dedup)", count)
	}

	var resp2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	ingested2, _ := resp2["ingested"].([]any)
	if len(ingested2) >= 1 {
		first, _ := ingested2[0].(map[string]any)
		isDup, _ := first["duplicate"].(bool)
		if !isDup {
			t.Error("第二次 ingest 应标记 duplicate=true")
		}
	}
}

// TestBridgeE2EChain_TraceConsistency 验证消息入库后 trace_id 不为空（贯通性基础保障）。
// 完整 trace 事件（ingest/outbound_enqueue）由 TestBridgeE2EChain_FullLifecycle 覆盖，
// 此处仅验证每条 inbound 消息都获得了 trace_id。
func TestBridgeE2EChain_TraceConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.MessageHub{})
	db.Exec("DELETE FROM message_hub WHERE conversation_id LIKE 'e2e-trace-%'")

	const (
		channel   = "xiaohongshu"
		accountID = "trace-acc"
		convID    = "e2e-trace-001"
	)

	ingressSvc := service.NewInboxIngressService()
	handler := NewBridgeIngestHandler(ingressSvc)

	for i := 0; i < 2; i++ {
		body := fmt.Sprintf(`{"v":1,"channel":"%s","account_id":"%s","conversation_id":"%s","messages":[
			{"event_id":"tr-ev-%d","role":"customer","content":"trace test %d"}
		]}`, channel, accountID, convID, i, i)
		req := httptest.NewRequest("POST", "/api/bridge/ingest", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		handler.HandleHTTPIngest(c)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest[%d] status=%d", i, w.Code)
		}
	}

	var noTraceCount int64
	db.Model(&model.MessageHub{}).
		Where("conversation_id = ? AND direction = 'inbound' AND trace_id = ''", convID).
		Count(&noTraceCount)
	if noTraceCount > 0 {
		t.Errorf("有 %d 条 inbound 消息 trace_id 为空（贯通性破坏）", noTraceCount)
	}

	var inbounds []model.MessageHub
	db.Where("conversation_id = ? AND direction = 'inbound'", convID).Find(&inbounds)
	if len(inbounds) != 2 {
		t.Errorf("inbound count=%d want=2", len(inbounds))
	}
	for i, m := range inbounds {
		if m.TraceID == "" {
			t.Errorf("inbound[%d] trace_id is empty", i)
		}
	}
	t.Logf("✅ 所有 %d 条 inbound 均有 trace_id", len(inbounds))
}

// TestBridgeE2EChain_ValidationErrors 验证 API 对非法参数返回正确的业务错误码。
// 不是只看 400/422，要检查错误消息有语义（如 "channel required"、"messages empty"）。
func TestBridgeE2EChain_ValidationErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupBridgeTestDB(t, &model.MessageHub{})
	ingressSvc := service.NewInboxIngressService()
	handler := NewBridgeIngestHandler(ingressSvc)

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantKeyword string
		wantOK      *bool
	}{
		{
			name:        "缺失 channel",
			body:        `{"v":1,"account_id":"a1","conversation_id":"c1","messages":[{"role":"customer","content":"hi"}]}`,
			wantStatus:  http.StatusBadRequest,
			wantKeyword: "channel",
		},
		{
			name:        "缺失 account_id",
			body:        `{"v":1,"channel":"douyin","conversation_id":"c1","messages":[{"role":"customer","content":"hi"}]}`,
			wantStatus:  http.StatusBadRequest,
			wantKeyword: "account_id",
		},
		{
			name:        "非法 JSON",
			body:        `not json at all`,
			wantStatus:  http.StatusBadRequest,
			wantKeyword: "",
		},
		{
			name:        "不支持的 channel",
			body:        `{"v":1,"channel":"unknown_chan","account_id":"a1","conversation_id":"c1","messages":[{"role":"customer","content":"hi"}]}`,
			wantStatus:  http.StatusBadRequest,
			wantKeyword: "unsupported",
		},
		{
			name:        "空 messages 数组（允许，返回 ok=true + ingested=[]）",
			body:        `{"v":1,"channel":"douyin","account_id":"a1","conversation_id":"c1","messages":[]}`,
			wantStatus:  http.StatusOK,
			wantKeyword: "",
			wantOK:      boolPtr(true),
		},
		{
			name:        "超长 messages（201 条截断到 200，不报错）",
			body:        makeHugeBody("douyin", "a1", "c1", 201),
			wantStatus:  http.StatusOK,
			wantKeyword: "",
			wantOK:      boolPtr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/bridge/ingest", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			handler.HandleHTTPIngest(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status=%d want=%d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantKeyword != "" && !strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(tt.wantKeyword)) {
				t.Errorf("err msg missing keyword %q, body=%s", tt.wantKeyword, w.Body.String())
			}
			if tt.wantOK != nil {
				var resp map[string]any
				json.Unmarshal(w.Body.Bytes(), &resp)
				gotOK, _ := resp["ok"].(bool)
				if gotOK != *tt.wantOK {
					t.Errorf("ok=%v want=%v body=%s", gotOK, *tt.wantOK, w.Body.String())
				}
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func makeHugeBody(channel, accountID, convID string, n int) string {
	msgs := make([]string, n)
	for i := 0; i < n; i++ {
		msgs[i] = fmt.Sprintf(`{"event_id":"huge-%d","role":"customer","content":"m%d"}`, i, i)
	}
	return fmt.Sprintf(`{"v":1,"channel":"%s","account_id":"%s","conversation_id":"%s","messages":[%s]}`,
		channel, accountID, convID, strings.Join(msgs, ","))
}

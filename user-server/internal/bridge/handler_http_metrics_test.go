package bridge

import (
	"hivemtk-user/internal/capregistry"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/metrics"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func metricCounterValue(t *testing.T, name string, labels ...string) uint64 {
	t.Helper()
	_ = metrics.GetBridge()
	c := metrics.MustGetCounter(name)
	return c.WithLabel(labels...).Value()
}

func metricHistogramCount(t *testing.T, name string, labels ...string) uint64 {
	t.Helper()
	_ = metrics.GetBridge()
	samples := metrics.CollectSamples()
	for _, s := range samples {
		if s.Name != name || s.Agg != "count" || len(s.Labels) != len(labels) {
			continue
		}
		match := true
		for i, l := range labels {
			if s.Labels[i] != l {
				match = false
				break
			}
		}
		if match {
			return uint64(s.Value)
		}
	}
	return 0
}

// TestIngestMetrics_ValidationError 验证 ingest 参数缺失时 IngestErrors 错误码计数 + IngestDuration 耗时。
func TestIngestMetrics_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandlerWithMock(
		func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
			return &service.InboxIngressResult{Accepted: true}, nil
		},
		func(ctx context.Context, ev *model.MessageEvent, direction string) error { return nil },
	)

	beforeErr := metricCounterValue(t, "bridge_ingest_errors_total", "", "channel_required")
	beforeDur := metricHistogramCount(t, "bridge_ingest_duration_ms", "")

	body := `{"account_id":"acc1","messages":[]}`
	req := httptest.NewRequest("POST", "/api/bridge/ingest?account_id=acc1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.HandleHTTPIngest(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", rr.Code)
	}
	if got := metricCounterValue(t, "bridge_ingest_errors_total", "", "channel_required"); got != beforeErr+1 {
		t.Errorf("channel_required 错误计数应 +1，实际 %d（before=%d）", got, beforeErr)
	}
	if got := metricHistogramCount(t, "bridge_ingest_duration_ms", ""); got != beforeDur+1 {
		t.Errorf("ingest 耗时直方图应 +1，实际 %d（before=%d）", got, beforeDur)
	}
}

// TestIngestMetrics_Success 验证 ingest 成功时 IngestTotal 计数（channel + agent_id 维度）。
func TestIngestMetrics_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandlerWithMock(
		func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
			return &service.InboxIngressResult{Accepted: true}, nil
		},
		func(ctx context.Context, ev *model.MessageEvent, direction string) error { return nil },
	)

	const (
		channel = "douyin"
		agentID = "7"
	)
	beforeTotal := metricCounterValue(t, "bridge_ingest_total", channel, agentID)

	body := `{"channel":"douyin","account_id":"acc1","agent_id":7,"messages":[{"event_id":"e1","content":"hi","sender_type":"customer","timestamp":1234567890,"msg_type":"text"}]}`
	req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=douyin&account_id=acc1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.HandleHTTPIngest(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if got := metricCounterValue(t, "bridge_ingest_total", channel, agentID); got != beforeTotal+1 {
		t.Errorf("ingest_total(douyin,7) 应 +1，实际 %d（before=%d）", got, beforeTotal)
	}
}

// TestOutboxMetrics_ValidationError 验证 outbox 参数缺失时错误码计数。
func TestOutboxMetrics_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandlerWithMock(nil, nil)

	beforeErr := metricCounterValue(t, "bridge_ingest_errors_total", "douyin", "account_id_required")

	req := httptest.NewRequest("GET", "/api/bridge/outbox?channel=douyin", nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.GetBridgeOutbox(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", rr.Code)
	}
	if got := metricCounterValue(t, "bridge_ingest_errors_total", "douyin", "account_id_required"); got != beforeErr+1 {
		t.Errorf("account_id_required 错误计数应 +1，实际 %d（before=%d）", got, beforeErr)
	}
}

// TestOutboxMetrics_Success 验证 outbox 成功拉取时 OutboxFetched 计数。
// 依赖真实 PG：seed 2 条 pending，GET outbox 后 OutboxFetched +2。
func TestOutboxMetrics_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.MessageHub{})
	if db == nil {
		return
	}
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	const (
		channel   = "douyin"
		accountID = "acc_metrics_outbox"
		conv      = "conv_metrics_outbox"
	)
	for i, content := range []string{"out_a", "out_b"} {
		hub := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: conv,
			MsgID:          "mh:metrics_outbox_" + content,
			MsgType:        "text",
			Content:        content,
			Direction:      "outbound",
			Status:         "pending",
		}
		if i == 0 {
		}
		if err := db.Create(hub).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	beforeFetched := metricCounterValue(t, "bridge_outbox_fetched_total", channel, accountID)

	req := httptest.NewRequest("GET", "/api/bridge/outbox?channel="+channel+"&account_id="+accountID, nil)
	req = req.WithContext(localBridgeDeliveryContext(t, tenant.WithTenantID(req.Context(), 1), channel, accountID, "text", ""))
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.GetBridgeOutbox(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if got := metricCounterValue(t, "bridge_outbox_fetched_total", channel, accountID); got != beforeFetched+2 {
		t.Errorf("outbox_fetched_total(douyin) 应 +2，实际 %d（before=%d）", got, beforeFetched)
	}
}

// TestAckMetrics_ValidationError 验证 ack 参数缺失时错误码计数。
func TestAckMetrics_ValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandlerWithMock(nil, nil)

	beforeErr := metricCounterValue(t, "bridge_ingest_errors_total", "", "channel_required")

	body := `{"msg_ids":["m1"],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?account_id=acc1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", rr.Code)
	}
	if got := metricCounterValue(t, "bridge_ingest_errors_total", "", "channel_required"); got != beforeErr+1 {
		t.Errorf("channel_required 错误计数应 +1，实际 %d（before=%d）", got, beforeErr)
	}
}

// TestAckMetrics_Success 验证 ack 成功时 OutboxAcked 按状态计数（acked/duplicate/not_found）。
// 依赖真实 PG：seed 2 条 pending + 1 条 delivered，批量 ack 3 条 + 1 条不存在。
func TestAckMetrics_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.MessageHub{})
	if db == nil {
		return
	}
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressService()
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	const (
		channel   = "douyin"
		accountID = "acc_metrics_ack"
		conv      = "conv_metrics_ack"
	)
	seeds := []struct {
		msgID  string
		status string
	}{
		{"mh:ack_pending_a", "pending"},
		{"mh:ack_pending_b", "pending"},
		{"mh:ack_delivered_c", model.BridgeAckStatusDelivered},
	}
	for _, s := range seeds {
		hub := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: conv,
			MsgID:          s.msgID,
			MsgType:        "text",
			Content:        s.msgID,
			Direction:      "outbound",
			Status:         s.status,
		}
		if err := db.Create(hub).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	beforeAcked := metricCounterValue(t, "bridge_outbox_acked_total", channel, "acked")
	beforeDup := metricCounterValue(t, "bridge_outbox_acked_total", channel, "duplicate")
	beforeNF := metricCounterValue(t, "bridge_outbox_acked_total", channel, "not_found")
	beforeAckDur := metricHistogramCount(t, "bridge_ack_duration_ms", channel)

	body := `{"msg_ids":["mh:ack_pending_a","mh:ack_pending_b","mh:ack_delivered_c","mh:ack_missing_d"],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if got := metricCounterValue(t, "bridge_outbox_acked_total", channel, "acked"); got != beforeAcked+2 {
		t.Errorf("acked 计数应 +2，实际 %d（before=%d）", got, beforeAcked)
	}
	if got := metricCounterValue(t, "bridge_outbox_acked_total", channel, "duplicate"); got != beforeDup+1 {
		t.Errorf("duplicate 计数应 +1，实际 %d（before=%d）", got, beforeDup)
	}
	if got := metricCounterValue(t, "bridge_outbox_acked_total", channel, "not_found"); got != beforeNF+1 {
		t.Errorf("not_found 计数应 +1，实际 %d（before=%d）", got, beforeNF)
	}
	if got := metricHistogramCount(t, "bridge_ack_duration_ms", channel); got != beforeAckDur+1 {
		t.Errorf("ack 耗时直方图应 +1，实际 %d（before=%d）", got, beforeAckDur)
	}
}

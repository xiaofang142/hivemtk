package bridge

// CH-37b 刀4（R2/R6）：HTTP outbox ack 入口三值（delivered/failed/unknown）——
// v1 body status 与 v2 items[] status 均接受 unknown（「已提交但结果不明」真终态），
// 行级落 status='unknown'+push_error='ack_unknown'；三值之外仍 400 拒绝；
// legacy delivered 行为零变化（回归钉）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const unk4Platform = "douyin"
const unk4Channel = "douyin_web" // HTTP 入参走别名，platform 是规范名（与 p3d 测试同款双名夹具）
const unk4Account = "acc_unk4"
const unk4Conv = "conv_unk4"

func newUnknownAckHandler(t *testing.T) (*gorm.DB, *BridgeIngestHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := setupBridgeTestDB(t, &model.MessageHub{})
	if err := database.Exec("DELETE FROM message_hub WHERE platform = ?", unk4Platform).Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	h := NewBridgeIngestHandlerWithMock(
		func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
			return &service.InboxIngressResult{Accepted: true}, nil
		},
		func(ctx context.Context, ev *model.MessageEvent, direction string) error { return nil },
	)
	h.ingress = service.NewInboxIngressService()
	return database, h
}

func doAckRequest(t *testing.T, h *BridgeIngestHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+unk4Channel+"&account_id="+unk4Account, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	return rr
}

func seedAckPendingRow(t *testing.T, h *BridgeIngestHandler, msgID string) {
	t.Helper()
	hub := &model.MessageHub{
		Platform:       unk4Platform,
		AccountID:      unk4Account,
		ConversationID: unk4Conv,
		MsgID:          msgID,
		MsgType:        "text",
		Content:        msgID,
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := h.ingress.DeliverOutbound(context.Background(), hub); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
}

func readAckRow(t *testing.T, db *gorm.DB, msgID string) (status, pushError string) {
	t.Helper()
	var row struct {
		Status    string
		PushError string
	}
	if err := db.Model(&model.MessageHub{}).
		Select("status, push_error").
		Where("platform = ? AND msg_id = ?", unk4Platform, msgID).
		Scan(&row).Error; err != nil {
		t.Fatalf("回读 %s 失败: %v", msgID, err)
	}
	return row.Status, row.PushError
}

// TestAckBridgeOutbox_UnknownStatus_V1Body HTTP v1：body status="unknown" → 200，
// unknown_items_count 计数 + 行落 unknown 终态 + push_error='ack_unknown'。
func TestAckBridgeOutbox_UnknownStatus_V1Body(t *testing.T) {
	db, h := newUnknownAckHandler(t)
	seedAckPendingRow(t, h, "mh:unk4-v1")

	rr := doAckRequest(t, h, `{"msg_ids":["mh:unk4-v1"],"status":"unknown"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	for _, e := range []string{`"affected_count":1`, `"unknown_items_count":1`, `"msg_id":"mh:unk4-v1","status":"unknown"`} {
		if !strings.Contains(rr.Body.String(), e) {
			t.Errorf("响应缺少 %q\n实际: %s", e, rr.Body.String())
		}
	}
	status, pushError := readAckRow(t, db, "mh:unk4-v1")
	if status != model.BridgeAckStatusUnknown || pushError != "ack_unknown" {
		t.Errorf("行未落 unknown 终态: status=%q push_error=%q", status, pushError)
	}
}

// TestAckBridgeOutbox_UnknownStatus_V2Items HTTP v2：items[] 逐项 status="unknown" → 200，
// 行落 unknown 终态（含 conversation 维作用域）。
func TestAckBridgeOutbox_UnknownStatus_V2Items(t *testing.T) {
	db, h := newUnknownAckHandler(t)
	seedAckPendingRow(t, h, "mh:unk4-v2")

	rr := doAckRequest(t, h, `{"items":[{"msg_id":"mh:unk4-v2","conversation_id":"`+unk4Conv+`","status":"unknown"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"unknown_items_count":1`) {
		t.Errorf("响应缺少 unknown_items_count=1\n实际: %s", rr.Body.String())
	}
	status, pushError := readAckRow(t, db, "mh:unk4-v2")
	if status != model.BridgeAckStatusUnknown || pushError != "ack_unknown" {
		t.Errorf("行未落 unknown 终态: status=%q push_error=%q", status, pushError)
	}
}

// TestAckBridgeOutbox_BogusStatus_Still400 三值之外仍 400：v1 与 v2 两条路都验，
// 错误文案同步为三值词表，且行不得翻转。
func TestAckBridgeOutbox_BogusStatus_Still400(t *testing.T) {
	db, h := newUnknownAckHandler(t)
	seedAckPendingRow(t, h, "mh:unk4-bad")

	rr := doAckRequest(t, h, `{"msg_ids":["mh:unk4-bad"],"status":"sent"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("v1 body status=sent 期望 400，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "must be delivered, failed or unknown") {
		t.Errorf("错误文案未同步三值词表: %s", rr.Body.String())
	}

	rr = doAckRequest(t, h, `{"items":[{"msg_id":"mh:unk4-bad","conversation_id":"`+unk4Conv+`","status":"sent"}]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("v2 items status=sent 期望 400，实际 %d: %s", rr.Code, rr.Body.String())
	}

	if status, _ := readAckRow(t, db, "mh:unk4-bad"); status != "pending" {
		t.Errorf("拒绝后行不得翻转: status=%q", status)
	}
}

// TestAckBridgeOutbox_LegacyDelivered_Unchanged legacy 回归钉：status=delivered 走原路径，
// 行落 delivered 且不写 push_error（刀4 对 legacy 二值零变化）。
func TestAckBridgeOutbox_LegacyDelivered_Unchanged(t *testing.T) {
	db, h := newUnknownAckHandler(t)
	seedAckPendingRow(t, h, "mh:unk4-leg")

	rr := doAckRequest(t, h, `{"msg_ids":["mh:unk4-leg"],"status":"delivered"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"acked_items_count":1`) || !strings.Contains(rr.Body.String(), `"unknown_items_count":0`) {
		t.Errorf("legacy delivered 读数异常\n实际: %s", rr.Body.String())
	}
	if status, pushError := readAckRow(t, db, "mh:unk4-leg"); status != model.BridgeAckStatusDelivered || pushError != "" {
		t.Errorf("legacy delivered 行为变化: status=%q push_error=%q", status, pushError)
	}
}

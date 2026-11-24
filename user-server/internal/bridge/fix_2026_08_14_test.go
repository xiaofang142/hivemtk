package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"
)

func TestHTTPIngest_AccountIDRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("缺 account_id → 400 reason=account_id required", func(t *testing.T) {
		h := NewBridgeIngestHandler(nil)
		r := gin.New()
		r.POST("/api/bridge/ingest", h.HandleHTTPIngest)

		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=xiaohongshu", strings.NewReader(`{"messages":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want 400", w.Code)
		}
		var resp HTTPIngestResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应 JSON 解析失败: %v", err)
		}
		if resp.OK {
			t.Error("OK = true, want false")
		}
		if !strings.Contains(resp.Reason, "account_id required") {
			t.Errorf("Reason = %q, 应含 'account_id required'", resp.Reason)
		}
		bodyStr := w.Body.String()
		if strings.Contains(strings.ToLower(bodyStr), "default") {
			t.Errorf("响应体不能含 'default' 兜底字面值：%s", bodyStr)
		}
	})

	t.Run("account_id 空串（无 value）→ 400", func(t *testing.T) {
		h := NewBridgeIngestHandler(nil)
		r := gin.New()
		r.POST("/api/bridge/ingest", h.HandleHTTPIngest)

		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=xiaohongshu&account_id=", strings.NewReader(`{"messages":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want 400", w.Code)
		}
	})
}

func TestBridgeOutboxMessage_ExtraField(t *testing.T) {
	t.Run("Extra map[string]any 能正确序列化到 JSON", func(t *testing.T) {
		msg := channelgw.OutboxMessage{
			MsgID:          "m1",
			ConversationID: "c1",
			MsgType:        "text",
			Content:        "hi",
			SenderID:       "bot",
			ReceiverID:     "user_1",
			IsAIReply:      true,
			Extra: map[string]any{
				"dm_target":    "member",
				"is_proactive": true,
				"priority":     5,
			},
		}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Marshal 失败: %v", err)
		}
		s := string(data)
		if !strings.Contains(s, `"extra"`) {
			t.Errorf("序列化结果应含 extra 字段：%s", s)
		}
		if !strings.Contains(s, `"dm_target":"member"`) {
			t.Errorf("extra.dm_target 必须能被前端读取：%s", s)
		}
		if !strings.Contains(s, `"is_proactive":true`) {
			t.Errorf("extra.is_proactive 必须能被前端读取：%s", s)
		}
	})

	t.Run("Extra 为空时 omitempty 生效（不输出 extra 字段）", func(t *testing.T) {
		msg := channelgw.OutboxMessage{
			MsgID: "m1",
		}
		data, _ := json.Marshal(msg)
		s := string(data)
		if strings.Contains(s, `"extra"`) {
			t.Errorf("Extra 为空时不应输出 extra 字段：%s", s)
		}
	})
}

func TestWriteOutboxJSON_PassesExtra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	hubs := []*model.MessageHub{
		{
			MsgID:          "m1",
			ConversationID: "c1",
			MsgType:        "text",
			Content:        "ping",
			SenderID:       "bot",
			ReceiverID:     "user_1",
			IsAIReply:      true,
			CreatedAt:      now,
			Extra: model.JSONMap{
				"dm_target": "member",
			},
		},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeOutboxJSON(c, hubs)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"dm_target":"member"`) {
		t.Errorf("writeOutboxJSON 未透传 hub.Extra：%s", body)
	}
}

// identify_code-only：缺码必须 fail-closed；合法识别码可在省略 account_id 时补全身份。
func TestGetBridgeOutbox_IdentifyCodeRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{}, &model.MessageHub{})
	fixture := seedCanonicalBridgeFixture(t, db, 1, "douyin", "outbox-identify")
	h := NewBridgeIngestHandler(service.NewInboxIngressService())
	r := gin.New()
	r.GET("/api/bridge/outbox", h.GetBridgeOutbox)

	req := httptest.NewRequest("GET", "/api/bridge/outbox?channel=douyin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("缺 identify_code 且缺 account_id 应结构性 400，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "account_id required") || strings.Contains(strings.ToLower(w.Body.String()), `"account_id":"default"`) {
		t.Errorf("缺码响应不得回退默认账号: %s", w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/api/bridge/outbox?channel=douyin", nil)
	req2.Header.Set("X-Channel-Identify-Code", fixture.Code)
	req2 = req2.WithContext(localBridgeDeliveryContext(t, tenant.WithTenantID(req2.Context(), 1), "douyin", fixture.AccountID, "text", ""))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("合法识别码应补全 account_id 并返回 200，实际 %d body=%s", w2.Code, w2.Body.String())
	}
}

func TestAckBridgeOutbox_AccountIDRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandler(nil)
	r := gin.New()
	r.POST("/api/bridge/outbox/ack", h.AckBridgeOutbox)

	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel=xiaohongshu", strings.NewReader(`{"msg_ids":["m1"],"status":"delivered"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("状态码 = %d, want 400", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "account_id required") {
		t.Errorf("响应体应说明 account_id required：%s", body)
	}
}

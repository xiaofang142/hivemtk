package bridge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPIngestDefaults(t *testing.T) {
	t.Run("HTTPIngestMaxBodySize_4MB", func(t *testing.T) {
		if HTTPIngestMaxBodySize != 4<<20 {
			t.Errorf("HTTPIngestMaxBodySize 应为 4MB，实际 %d", HTTPIngestMaxBodySize)
		}
	})
	t.Run("HTTPIngestMaxMessages_200", func(t *testing.T) {
		if HTTPIngestMaxMessages != 200 {
			t.Errorf("HTTPIngestMaxMessages 应为 200，实际 %d", HTTPIngestMaxMessages)
		}
	})
}

func TestCollectHTTPRequestInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("完整 URL + 全部 query + headers + body 预览", func(t *testing.T) {
		bodyJSON := `{"channel":"xiaohongshu","account_id":"xhs_123","conversation_id":"conv_abc","messages":[{"event_id":"m1","content":"你好","sender_type":"customer","timestamp":1234567890,"msg_type":"text","conversation_id":"conv_abc"}],"expect_reply":true,"timeout_ms":30000}`
		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=xiaohongshu&account_id=xhs_123&token=eyJhbGciOiJIUzI1NiJ9.payload.sig", strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://www.xiaohongshu.com")
		req.Header.Set("User-Agent", "Mozilla/5.0 HiveBridge/1.0")
		req.Header.Set("X-Trace-Id", "trace_abc_123")
		req.RemoteAddr = "10.0.0.1:54321"
		req.ContentLength = int64(len(bodyJSON))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req

		info, _, _ := collectHTTPRequestInfoWithLimit(c, HTTPIngestMaxBodySize)

		if info.Method != "POST" {
			t.Errorf("Method = %q, want POST", info.Method)
		}
		if info.Path != "/api/bridge/ingest" {
			t.Errorf("Path = %q, want /api/bridge/ingest", info.Path)
		}
		if info.Channel != "xiaohongshu" {
			t.Errorf("Channel = %q, want xiaohongshu", info.Channel)
		}
		if info.AccountID != "xhs_123" {
			t.Errorf("AccountID = %q, want xhs_123", info.AccountID)
		}
		if info.RemoteAddr != "10.0.0.1:54321" {
			t.Errorf("RemoteAddr = %q, want 10.0.0.1:54321", info.RemoteAddr)
		}
		if info.Origin != "https://www.xiaohongshu.com" {
			t.Errorf("Origin = %q, want https://www.xiaohongshu.com", info.Origin)
		}
		if info.UserAgent != "Mozilla/5.0 HiveBridge/1.0" {
			t.Errorf("UserAgent = %q, want Mozilla/5.0 HiveBridge/1.0", info.UserAgent)
		}
		if !strings.Contains(info.TokenMasked, "***") {
			t.Errorf("TokenMasked = %q, 应含 ***", info.TokenMasked)
		}
		if v, ok := info.ParsedQuery["channel"]; !ok || v != "xiaohongshu" {
			t.Errorf("parsed_query.channel = %v, want xiaohongshu", v)
		}
		if v, ok := info.ParsedQuery["account_id"]; !ok || v != "xhs_123" {
			t.Errorf("parsed_query.account_id = %v, want xhs_123", v)
		}
		if v, ok := info.ParsedQuery["token"]; !ok || !strings.Contains(v, "***") {
			t.Errorf("parsed_query.token = %v, 应含 ***", v)
		}
		if !strings.Contains(info.BodyPreview, "xiaohongshu") {
			t.Errorf("BodyPreview 应包含 channel 字段，实际 %q", info.BodyPreview)
		}
		if info.BodySize != len(bodyJSON) {
			t.Errorf("BodySize = %d, want %d", info.BodySize, len(bodyJSON))
		}
	})

	t.Run("body 截断到 4KB 预览", func(t *testing.T) {

		var sb bytes.Buffer
		sb.WriteString(`{"channel":"x","account_id":"y","messages":[`)
		for i := 0; i < 5000; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(`{"event_id":"m","content":"` + strings.Repeat("a", 100) + `"}`)
		}
		sb.WriteString(`]}`)
		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=x&account_id=y", strings.NewReader(sb.String()))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(sb.Len())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req

		info, _, _ := collectHTTPRequestInfoWithLimit(c, HTTPIngestMaxBodySize)

		if !strings.Contains(info.BodyPreview, "truncated") {
			t.Errorf("BodyPreview 应标记 truncated，实际 %q 前 100 字符", info.BodyPreview[:min(100, len(info.BodyPreview))])
		}
		if info.BodySize != sb.Len() {
			t.Errorf("BodySize = %d, want %d", info.BodySize, sb.Len())
		}
		if len(info.BodyPreview) > 5000 {
			t.Errorf("BodyPreview 长度 %d 超过截断上限", len(info.BodyPreview))
		}
	})

	t.Run("body 写回后下游可读", func(t *testing.T) {
		body := `{"channel":"douyin","account_id":"dy_1","messages":[]}`
		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=douyin&account_id=dy_1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req

		_, _, _ = collectHTTPRequestInfoWithLimit(c, HTTPIngestMaxBodySize)

		var got HTTPIngestRequest
		if err := c.ShouldBindJSON(&got); err != nil {
			t.Fatalf("下游 BindJSON 失败: %v", err)
		}
		if got.Channel != "douyin" {
			t.Errorf("Channel = %q, want douyin", got.Channel)
		}
		if got.AccountID != "dy_1" {
			t.Errorf("AccountID = %q, want dy_1", got.AccountID)
		}
	})
}

func TestHTTPIngestRequest_JsonBind(t *testing.T) {
	body := `{
		"channel": "xiaohongshu",
		"account_id": "xhs_1",
		"conversation_id": "conv_1",
		"messages": [
			{
				"event_id": "evt_1",
				"sender_type": "customer",
				"sender_id": "u_1",
				"sender_name": "Alice",
				"msg_type": "text",
				"content": "你好",
				"timestamp": 1700000000000,
				"conversation_id": "conv_1"
			}
		],
		"expect_reply": true,
		"timeout_ms": 30000,
		"account_name": "测试账号",
		"agent_id": 1
	}`
	var req HTTPIngestRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&req); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if req.Channel != "xiaohongshu" {
		t.Errorf("Channel = %q, want xiaohongshu", req.Channel)
	}
	if req.AccountID != "xhs_1" {
		t.Errorf("AccountID = %q, want xhs_1", req.AccountID)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Messages 数量 = %d, want 1", len(req.Messages))
	}
	m := req.Messages[0]
	if m.SenderID != "u_1" {
		t.Errorf("SenderID = %q, want u_1", m.SenderID)
	}
	if m.Content != "你好" {
		t.Errorf("Content = %q, want 你好", m.Content)
	}
	if !req.ExpectReply {
		t.Error("ExpectReply = false, want true")
	}
	if req.TimeoutMs != 30000 {
		t.Errorf("TimeoutMs = %d, want 30000", req.TimeoutMs)
	}
	if req.AccountName != "测试账号" {
		t.Errorf("AccountName = %q, want 测试账号", req.AccountName)
	}
	if req.AgentID != 1 {
		t.Errorf("AgentID = %d, want 1", req.AgentID)
	}
}

func TestHTTPIngestResponse_Defaults(t *testing.T) {
	resp := HTTPIngestResponse{
		OK:         true,
		Ingested:   nil,
		ServerTime: 1700000000000,
	}
	if !resp.OK {
		t.Error("OK = false, want true")
	}
	if resp.Ingested != nil {
		t.Errorf("Ingested 应为 nil，实际 %v", resp.Ingested)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "outbound_replies") {
		t.Errorf("2026-08-18 二次审核：响应体不应再含 outbound_replies 字段：%s", s)
	}
	if !strings.Contains(s, `"ok":true`) {
		t.Errorf("响应体应含 ok=true：%s", s)
	}
}

// TestHTTPIngestHandler_BadRequest 验证缺参数/无效渠道时返回 400 且响应体含 reason
func TestHTTPIngestHandler_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("缺 channel", func(t *testing.T) {
		h := NewBridgeIngestHandler(nil)
		r := gin.New()
		r.POST("/api/bridge/ingest", h.HandleHTTPIngest)

		req := httptest.NewRequest("POST", "/api/bridge/ingest?account_id=xhs_1", strings.NewReader(`{"messages":[]}`))
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
		if resp.Reason == "" {
			t.Error("Reason 应非空")
		}
	})

	t.Run("无效渠道", func(t *testing.T) {
		h := NewBridgeIngestHandler(nil)
		r := gin.New()
		r.POST("/api/bridge/ingest", h.HandleHTTPIngest)

		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=invalid_channel&account_id=xhs_1", strings.NewReader(`{"messages":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want 400", w.Code)
		}
	})

	t.Run("body 超过 4MB", func(t *testing.T) {
		h := NewBridgeIngestHandler(nil)
		r := gin.New()
		r.POST("/api/bridge/ingest", h.HandleHTTPIngest)

		req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=xiaohongshu&account_id=xhs_1", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = HTTPIngestMaxBodySize + 1
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("状态码 = %d, want 413", w.Code)
		}
	})
}

func TestHttpMessageToEvent(t *testing.T) {
	m := &HTTPIngestMessage{
		EventID:        "evt_1",
		Channel:        "xiaohongshu",
		AccountID:      "xhs_1",
		ConversationID: "conv_1",
		SenderID:       "u_1",
		SenderName:     "Alice",
		SenderType:     "customer",
		MsgType:        "text",
		Content:        "你好",
		Timestamp:      1700000000000,
		IsGroup:        false,
	}
	ev := httpMessageToEvent(m)
	if ev.EventID != "evt_1" {
		t.Errorf("EventID = %q, want evt_1", ev.EventID)
	}
	if ev.Channel != "xiaohongshu" {
		t.Errorf("Channel = %q, want xiaohongshu", ev.Channel)
	}
	if ev.SenderType != "customer" {
		t.Errorf("SenderType = %q, want customer", ev.SenderType)
	}
	if ev.SessionID == "" {
		t.Error("SessionID 应非空（兜底为 channel:account_id:conversation_id）")
	}
	if ev.Extra["transport"] != "http" {
		t.Errorf("Extra.transport = %v, want http", ev.Extra["transport"])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

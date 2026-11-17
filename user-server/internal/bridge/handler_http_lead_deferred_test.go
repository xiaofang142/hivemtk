package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestHTTPIngest_DoesNotExecuteSynchronousLeadMiner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, reason := range []string{"", "self-echo", "duplicate"} {
		t.Run("入站结果"+reason, func(t *testing.T) {
			h := NewBridgeIngestHandlerWithMock(func(context.Context, *model.MessageEvent) (*service.InboxIngressResult, error) {
				return &service.InboxIngressResult{Accepted: true, Reason: reason}, nil
			}, nil)
			calls := 0
			h.SetLeadMiner(func(context.Context, *model.MessageEvent) { calls++ })
			payload, err := json.Marshal(map[string]any{"channel": "douyin", "account_id": "lead-account", "conversation_id": "lead-conversation", "messages": []map[string]any{{"event_id": "lead-http-inbound", "sender_id": "customer-a", "sender_type": "customer", "content": "采购咨询", "timestamp": time.Now().UnixMilli()}}})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/bridge/ingest", strings.NewReader(string(payload)))
			c.Request.Header.Set("Content-Type", "application/json")
			h.HandleHTTPIngest(c)
			if recorder.Code != http.StatusOK {
				t.Fatalf("入站正常响应不能改变：%d", recorder.Code)
			}
			if calls != 0 {
				t.Fatal("HTTP不能同步执行矿工，也不能依据Accepted挖掘自回显/重复消息")
			}
		})
	}
}

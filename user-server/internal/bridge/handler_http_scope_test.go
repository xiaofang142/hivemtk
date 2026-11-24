package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestHTTPIngestScope_ValidateWholeBatchBeforeSideEffects(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	fixture := seedCanonicalBridgeFixture(t, db, 1, "douyin", "scope-account")
	accountID := fixture.AccountID
	for _, tc := range []struct {
		name   string
		query  string
		change func(*HTTPIngestRequest)
		want   int
	}{
		{name: "内层渠道", change: func(r *HTTPIngestRequest) { r.Messages[1].Channel = "xiaohongshu" }, want: http.StatusBadRequest},
		{name: "内层账号", change: func(r *HTTPIngestRequest) { r.Messages[1].AccountID = "other-account" }, want: http.StatusBadRequest},
		{name: "限定会话", query: "?channel=douyin&account_id=" + accountID + "&conversation_id=scope-conversation", change: func(r *HTTPIngestRequest) { r.Messages[1].ConversationID = "other-conversation" }, want: http.StatusBadRequest},
		{name: "顶层与认证账号冲突", query: "?channel=douyin&account_id=" + accountID, change: func(r *HTTPIngestRequest) { r.AccountID = "other-account" }, want: http.StatusBadRequest},
		{name: "省略字段合法继承", want: http.StatusOK},
		{name: "合法多会话", change: func(r *HTTPIngestRequest) {
			r.ConversationID = ""
			r.Messages[0].ConversationID = "first-conversation"
			r.Messages[1].ConversationID = "second-conversation"
		}, want: http.StatusOK},
		{name: "同渠道别名", change: func(r *HTTPIngestRequest) { r.Messages[1].Channel = "douyin_web" }, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, histories := 0, 0
			h := NewBridgeIngestHandlerWithMock(func(_ context.Context, event *model.MessageEvent) (*service.InboxIngressResult, error) {
				calls++
				if event.Channel != "douyin" || event.Extra["account_id"] != accountID {
					t.Error("合法消息必须采用认证渠道和账号范围")
				}
				return &service.InboxIngressResult{Accepted: true}, nil
			}, func(context.Context, *model.MessageEvent, string) error { histories++; return nil })
			request := HTTPIngestRequest{Channel: "douyin", AccountID: accountID, ConversationID: "scope-conversation", Messages: []*HTTPIngestMessage{
				{EventID: "scope-first", SenderID: "customer", Content: "合成合法问话", History: []*HistoryItem{{EventID: "scope-history", Content: "合成历史"}}},
				{EventID: "scope-second", SenderID: "customer", Content: "合成第二问话"},
			}}
			if tc.change != nil {
				tc.change(&request)
			}
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/bridge/ingest"+tc.query, strings.NewReader(string(body)))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("X-Channel-Identify-Code", fixture.Code)
			h.HandleHTTPIngest(c)
			if recorder.Code != tc.want {
				t.Fatalf("完整批次校验响应：得到%d，期望%d", recorder.Code, tc.want)
			}
			if tc.want != http.StatusOK {
				if calls != 0 || histories != 0 {
					t.Fatalf("冲突批次不得先执行合法消息或历史：%d/%d", calls, histories)
				}
			} else if calls != 2 || histories != 1 {
				t.Fatalf("合法多会话与历史契约不能丢失：%d/%d", calls, histories)
			}
		})
	}
}

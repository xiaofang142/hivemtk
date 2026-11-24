package bridge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// 会话级上行帧（带 history）里的每一条历史项，方向由上报方按各自 sender_type 标注。
// 父消息的 sender_type 只描述「当前这条」是谁发的，不代表整段历史都是它发的：
// 用父消息把 inbound 历史改写成 outbound，会把客户的问题也塞进下行回放队列。
func TestHTTPIngest_HistoryDirection_PreservesItemDirection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
	fixture := seedCanonicalBridgeFixture(t, db, 1, "douyin", "history-direction")

	type rec struct {
		eventID   string
		direction string
	}
	got := make(chan rec, 8)
	h := NewBridgeIngestHandlerWithMock(
		func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
			return &service.InboxIngressResult{Accepted: true}, nil
		},
		func(ctx context.Context, ev *model.MessageEvent, direction string) error {
			got <- rec{eventID: ev.EventID, direction: direction}
			return nil
		},
	)

	body := fmt.Sprintf(`{
		"channel":"douyin","account_id":"%s",
		"messages":[{
			"event_id":"live-1","conversation_id":"conv_hist_dir","sender_type":"agent",
			"sender_id":"%s","receiver_id":"conv_hist_dir","msg_type":"text",
			"content":"我们的最新回复","timestamp":1700000300000,
			"history":[
				{"event_id":"h-in-1","sender_type":"customer","content":"客户的问题","msg_type":"text","timestamp":1700000100000,"direction":"inbound"},
				{"event_id":"h-out-1","sender_type":"agent","content":"我们先前的回复","msg_type":"text","timestamp":1700000200000,"direction":"outbound"}
			]
		}]
	}`, fixture.AccountID, fixture.AccountID)
	req := httptest.NewRequest("POST", "/api/bridge/ingest?channel=douyin&account_id="+fixture.AccountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Channel-Identify-Code", fixture.Code)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.HandleHTTPIngest(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}

	want := map[string]string{"h-in-1": "inbound", "h-out-1": "outbound"}
	for i := 0; i < len(want); i++ {
		select {
		case r := <-got:
			if want[r.eventID] == "" {
				t.Fatalf("未预期的 history 落库: %+v", r)
			}
			if r.direction != want[r.eventID] {
				t.Errorf("history %s 方向被改写: got=%s want=%s", r.eventID, r.direction, want[r.eventID])
			}
		default:
			t.Fatalf("history 落库次数不足，已收到 %d 条（want %d）", i, len(want))
		}
	}
}

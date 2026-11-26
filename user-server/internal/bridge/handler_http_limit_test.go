package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type limitInboundRecorder struct {
	ids []string
}

func (r *limitInboundRecorder) Deliver(_ context.Context, msg *model.InboundMessage) error {
	r.ids = append(r.ids, msg.EventID)
	return nil
}

func TestHTTPIngestLimitAppliesBeforeAllConsumers(t *testing.T) {
	previousConfig := service.GlobalConfigParam()
	service.SetGlobalForTest(nil)
	previousRepo := GlobalBridgeAccountRepo
	GlobalBridgeAccountRepo = nil
	t.Cleanup(func() {
		service.SetGlobalForTest(previousConfig)
		GlobalBridgeAccountRepo = previousRepo
	})
	gin.SetMode(gin.TestMode)
	for _, invalidTail := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_tail_%t", invalidTail), func(t *testing.T) {
			var live, history []string
			dispatcher := &limitInboundRecorder{}
			h := NewBridgeIngestHandlerWithMock(
				func(_ context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
					live = append(live, ev.EventID)
					return &service.InboxIngressResult{Accepted: true}, nil
				},
				func(_ context.Context, ev *model.MessageEvent, _ string) error {
					history = append(history, ev.EventID)
					return nil
				},
			)
			h.SetInboundDispatcher(dispatcher)
			body := HTTPIngestRequest{Channel: "douyin", AccountID: "limit-account", ConversationID: "limit-conversation"}
			for i := 0; i <= HTTPIngestMaxMessages; i++ {
				body.Messages = append(body.Messages, &HTTPIngestMessage{
					EventID: fmt.Sprintf("live-%d", i), Content: "fixture", SenderType: "customer",
					History: []*channelgw.HistoryItem{{EventID: fmt.Sprintf("history-%d", i), Content: "fixture"}},
				})
			}
			if invalidTail {
				body.Messages[HTTPIngestMaxMessages].AccountID = "other-account"
			}
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest", bytes.NewReader(payload))
			r = r.WithContext(tenant.WithTenantID(r.Context(), 1))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = r
			h.HandleHTTPIngest(c)
			if invalidTail {
				if w.Code != http.StatusBadRequest || len(dispatcher.ids)+len(live)+len(history) != 0 {
					t.Fatalf("scope rejection: status=%d dispatcher=%d live=%d history=%d", w.Code, len(dispatcher.ids), len(live), len(history))
				}
				return
			}
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			for name, ids := range map[string][]string{"dispatcher": dispatcher.ids, "live": live, "history": history} {
				if len(ids) != HTTPIngestMaxMessages {
					t.Errorf("%s received %d messages, want %d", name, len(ids), HTTPIngestMaxMessages)
				}
				for _, id := range ids {
					if id == fmt.Sprintf("live-%d", HTTPIngestMaxMessages) || id == fmt.Sprintf("history-%d", HTTPIngestMaxMessages) {
						t.Errorf("%s received truncated event %q", name, id)
					}
				}
			}
			var resp HTTPIngestResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if len(resp.Ingested) != HTTPIngestMaxMessages {
				t.Errorf("response has %d results", len(resp.Ingested))
			}
		})
	}
}

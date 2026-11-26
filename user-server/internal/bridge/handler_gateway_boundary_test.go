package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestGatewayMessagePinsTrustedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "missing"},
		{name: "forged_strings", extra: map[string]any{"account_id": "other-account", "tenant_id": "999", "diagnostic": "kept"}},
		{name: "forged_types", extra: map[string]any{"account_id": float64(99), "tenant_id": nil, "diagnostic": "kept"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := UnifiedGatewayMessage{EventID: "event-fixture", Content: "fixture", Extra: tc.extra}
			event := gatewayMessageToEvent("wechat", "trusted-account", 7, m)
			assertGatewayIdentity(t, event, "trusted-account", "7")
			if tc.extra != nil {
				if event.Extra["diagnostic"] != "kept" {
					t.Error("diagnostic metadata lost")
				}
				if tc.extra["account_id"] == "trusted-account" || tc.extra["tenant_id"] == "7" {
					t.Error("source metadata was mutated")
				}
			}
		})
	}
}

func assertGatewayIdentity(t *testing.T, ev *model.MessageEvent, account, tenant string) {
	t.Helper()
	if ev.Extra["account_id"] != account || ev.Extra["tenant_id"] != tenant {
		t.Errorf("event identity account=%v tenant=%v, want %s/%s", ev.Extra["account_id"], ev.Extra["tenant_id"], account, tenant)
	}
}

func TestGatewayReportAcknowledgesConsumerResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received []*model.MessageEvent
	h := NewBridgeIngestHandlerWithMock(func(_ context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
		received = append(received, ev)
		switch ev.EventID {
		case "accepted":
			return &service.InboxIngressResult{Accepted: true}, nil
		case "discarded":
			return &service.InboxIngressResult{Accepted: false, Reason: "discarded by route rule 8"}, nil
		case "missing":
			return nil, nil
		case "failed":
			return nil, errors.New("private database detail fixture")
		default:
			return &service.InboxIngressResult{Accepted: true}, errors.New("private database detail fixture")
		}
	}, nil)
	req := UnifiedChannelGatewayRequest{Action: "report", Channel: "wechat", AccountID: "trusted-account"}
	for _, id := range []string{"accepted", "discarded", "missing", "failed", "accepted_with_error"} {
		req.Messages = append(req.Messages, UnifiedGatewayMessage{EventID: id, Content: "fixture", Extra: map[string]any{"account_id": "forged", "tenant_id": "999"}})
	}
	w := invokeGatewayReport(t, h, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(received) != len(req.Messages) {
		t.Fatalf("consumer calls=%d", len(received))
	}
	for _, event := range received {
		assertGatewayIdentity(t, event, "trusted-account", "7")
	}
	var resp struct {
		Schema string `json:"schema_version"`
		Acks   []struct {
			EventID string `json:"event_id"`
			Status  string `json:"status"`
			Code    string `json:"code"`
		} `json:"acks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Schema != "hivemtk.channel.v1" || len(resp.Acks) != len(req.Messages) {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	wantStatus := []string{"accepted", "rejected", "retry", "retry", "retry"}
	wantCode := []string{"", "INGRESS_NOT_ACCEPTED", "INGRESS_RESULT_MISSING", "INGRESS_FAILED", "INGRESS_FAILED"}
	for i, ack := range resp.Acks {
		if ack.EventID != req.Messages[i].EventID || ack.Status != wantStatus[i] || ack.Code != wantCode[i] {
			t.Errorf("ack[%d]=%+v, want %s/%s/%s", i, ack, req.Messages[i].EventID, wantStatus[i], wantCode[i])
		}
	}
	if strings.Contains(w.Body.String(), "private database detail") || strings.Contains(w.Body.String(), "route rule 8") {
		t.Error("internal failure detail leaked in response")
	}
}

func TestGatewayReportUsesResolvedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		channel string
		account string
		ident   BridgeResolvedIdentity
		want    int
	}{
		{name: "canonical_channel_over_hint", channel: "qq", account: "trusted-account", ident: BridgeResolvedIdentity{TenantID: 7, Channel: "wechat", AccountID: "trusted-account"}, want: http.StatusOK},
		{name: "missing_hints", ident: BridgeResolvedIdentity{TenantID: 7, Channel: "wechat", AccountID: "trusted-account"}, want: http.StatusOK},
		{name: "account_conflict", channel: "wechat", account: "other-account", ident: BridgeResolvedIdentity{TenantID: 7, Channel: "wechat", AccountID: "trusted-account"}, want: http.StatusNotFound},
		{name: "tenant_conflict", channel: "wechat", account: "trusted-account", ident: BridgeResolvedIdentity{TenantID: 8, Channel: "wechat", AccountID: "trusted-account"}, want: http.StatusNotFound},
		{name: "missing_identity_account", channel: "wechat", account: "trusted-account", ident: BridgeResolvedIdentity{TenantID: 7, Channel: "wechat"}, want: http.StatusNotFound},
		{name: "missing_identity_tenant", channel: "wechat", account: "trusted-account", ident: BridgeResolvedIdentity{Channel: "wechat", AccountID: "trusted-account"}, want: http.StatusNotFound},
		{name: "missing_identity_channel", channel: "wechat", account: "trusted-account", ident: BridgeResolvedIdentity{TenantID: 7, AccountID: "trusted-account"}, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewBridgeIngestHandlerWithMock(func(_ context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
				calls++
				if ev.Channel != "wechat" {
					t.Errorf("consumer channel=%s, want canonical wechat", ev.Channel)
				}
				assertGatewayIdentity(t, ev, "trusted-account", "7")
				return &service.InboxIngressResult{Accepted: true}, nil
			}, nil)
			req := UnifiedChannelGatewayRequest{Action: "report", Channel: tc.channel, AccountID: tc.account,
				Messages: []UnifiedGatewayMessage{{EventID: "identity-fixture", Content: "fixture"}}}
			w := invokeGatewayReport(t, h, req, &tc.ident)
			if w.Code != tc.want {
				t.Errorf("status=%d body=%s, want %d", w.Code, w.Body.String(), tc.want)
			}
			wantCalls := 0
			if tc.want == http.StatusOK {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("consumer calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func invokeGatewayReport(t *testing.T, h *BridgeIngestHandler, req UnifiedChannelGatewayRequest, identities ...*BridgeResolvedIdentity) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/bridge/gateway", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(tenant.WithTenantID(r.Context(), 7))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = r
	if len(identities) > 0 {
		setBridgeIdentity(c, identities[0])
	}
	h.HandleGateway(c)
	return w
}

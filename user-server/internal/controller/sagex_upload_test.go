package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type ctrlSageXInstRepo struct{ inst *model.SageXInstallation }

func (r *ctrlSageXInstRepo) Create(context.Context, *model.SageXInstallation) error { return nil }
func (r *ctrlSageXInstRepo) Update(context.Context, *model.SageXInstallation) error { return nil }
func (r *ctrlSageXInstRepo) GetByID(context.Context, uint) (*model.SageXInstallation, error) {
	return r.inst, nil
}
func (r *ctrlSageXInstRepo) GetByToken(context.Context, string) (*model.SageXInstallation, error) {
	return r.inst, nil
}
func (r *ctrlSageXInstRepo) GetByInstallationID(context.Context, string) (*model.SageXInstallation, error) {
	return r.inst, nil
}
func (r *ctrlSageXInstRepo) TouchLastSeen(context.Context, uint, time.Time) error { return nil }

type ctrlSageXReceiptRepo struct{}

func (r *ctrlSageXReceiptRepo) GetByInstallationAndEventID(context.Context, uint, string, string) (*model.SageXEventReceipt, error) {
	return nil, nil
}
func (r *ctrlSageXReceiptRepo) Create(context.Context, *model.SageXEventReceipt) error { return nil }
func (r *ctrlSageXReceiptRepo) Update(context.Context, *model.SageXEventReceipt) error { return nil }

type ctrlSageXIngress struct{}

func (r *ctrlSageXIngress) HandleIngressMessage(context.Context, *model.MessageEvent) (*service.InboxIngressResult, error) {
	return &service.InboxIngressResult{Accepted: true}, nil
}

func TestSageXUploadController_ReturnsProtocolAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewSageXUploadService(&ctrlSageXInstRepo{inst: &model.SageXInstallation{TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1}}, &ctrlSageXReceiptRepo{}, &ctrlSageXIngress{})
	ctrl := NewSageXUploadController(svc)
	r := gin.New()
	r.POST("/api/integrations/sagex/upload", ctrl.Upload)

	body, _ := json.Marshal(map[string]any{
		"schema_version":  "1.0",
		"batch_id":        "b1",
		"sent_at":         "2026-11-23T10:00:00Z",
		"installation_id": "i1",
		"events": []map[string]any{{
			"event_id": "e1", "event_type": "notification.posted", "captured_at": "2026-11-23T10:00:00Z",
			"source":           map[string]any{"platform": "android", "package": "com.tencent.mobileqq"},
			"parse_status":     "ok",
			"conversation":     map[string]any{"type": "private", "title": "Alice"},
			"message":          map[string]any{"sender": "Alice", "text": "hi", "notification_posted_at": int64(1795435200000)},
			"raw_notification": map[string]any{"key": "k1", "id": 1, "is_group_summary": false, "messages": []any{}, "historic_messages": []any{}, "text_lines": []any{}},
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/sagex/upload", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp service.SageXUploadAckResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not json: %v body=%s", err, w.Body.String())
	}
	if resp.SchemaVersion != "1.0" || resp.BatchID != "b1" || len(resp.Acks) != 1 || resp.Acks[0].Status != "accepted" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}
func TestSageXUploadController_RealV1Payload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewSageXUploadService(&ctrlSageXInstRepo{inst: &model.SageXInstallation{TenantID: 1, ID: 7, InstallationID: "11111111-1111-4111-8111-111111111111", Token: "tok", Status: 1, AccountID: "qq-device-1"}}, &ctrlSageXReceiptRepo{}, &ctrlSageXIngress{})
	ctrl := NewSageXUploadController(svc)
	r := gin.New()
	r.POST("/api/integrations/sagex/upload", ctrl.Upload)

	body := []byte(`{
		"schema_version":"1.0",
		"batch_id":"22222222-2222-4222-8222-222222222222",
		"sent_at":"2026-11-23T08:00:00.000Z",
		"installation_id":"11111111-1111-4111-8111-111111111111",
		"events":[{
			"event_id":"33333333-3333-4333-8333-333333333333",
			"event_type":"notification.posted",
			"captured_at":"2026-11-23T08:00:00.000Z",
			"source":{"platform":"android","package":"com.tencent.mobileqq"},
			"parse_status":"parsed",
			"conversation":{"type":"direct","title":"Alice"},
			"message":{"sender":"Alice","text":"hello","notification_posted_at":1795420800000},
			"raw_notification":{"key":"0|com.tencent.mobileqq|1|null|10001","id":1,"tag":null,"title":"Alice","text":"hello","big_text":null,"sub_text":null,"summary_text":null,"conversation_title":null,"text_lines":[],"ticker_text":null,"category":"msg","channel_id":"qq","group_key":null,"is_group_summary":false,"is_group_conversation":false,"messages":[],"historic_messages":[]},
			"truncated_fields":[]
		}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/sagex/upload", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-SageX-Protocol", "1.0")
	req.Header.Set("X-Installation-Id", "11111111-1111-4111-8111-111111111111")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp service.SageXUploadAckResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	if len(resp.Acks) != 1 || resp.Acks[0].Status != "accepted" {
		t.Fatalf("acks=%+v", resp.Acks)
	}
}

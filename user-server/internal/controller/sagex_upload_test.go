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
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type ctrlSageXInstRepo struct{ inst *model.SageXInstallation }

func (r *ctrlSageXInstRepo) Create(context.Context, *model.SageXInstallation) error { return nil }
func (r *ctrlSageXInstRepo) CreateIfAbsent(_ context.Context, inst *model.SageXInstallation) (bool, error) {
	if r.inst != nil && r.inst.TenantID == inst.TenantID && r.inst.InstallationID == inst.InstallationID && inst.InstallationID != "" {
		return false, nil
	}
	return true, nil
}
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
func (r *ctrlSageXInstRepo) GetByTenantAndInstallationID(
	_ context.Context,
	tenantID uint,
	installationID string,
) (*model.SageXInstallation, error) {
	if r.inst != nil && r.inst.TenantID == tenantID && r.inst.InstallationID == installationID {
		return r.inst, nil
	}
	return nil, nil
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

const sageXUploadIdentifyCode = "identify-code-valid"

type ctrlUploadIdentifyAccountRepo struct {
	repository.ChannelAccountRepository
}

func (r *ctrlUploadIdentifyAccountRepo) Available() bool { return true }

func (r *ctrlUploadIdentifyAccountRepo) GetByIdentifyCode(_ context.Context, code string) (*model.ChannelAccount, error) {
	if code != sageXUploadIdentifyCode {
		return nil, nil
	}
	return &model.ChannelAccount{TenantID: 1, ID: 1, ChannelID: 1, IdentifyCode: sageXUploadIdentifyCode}, nil
}

func newSageXUploadTestController(inst *model.SageXInstallation) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewSageXUploadService(&ctrlSageXInstRepo{inst: inst}, &ctrlSageXReceiptRepo{}, &ctrlSageXIngress{})
	channels := &ctrlBootstrapChannelRepo{channel: &model.Channel{ID: 1, TenantID: 1, Code: "sagex"}}
	svc.SetChannelResolver(service.NewChannelResolverWithRepos(&ctrlUploadIdentifyAccountRepo{}, channels))
	ctrl := NewSageXUploadController(svc)
	r := gin.New()
	r.POST("/api/integrations/sagex/upload", ctrl.Upload)
	return r
}

func sageXUploadContractBody(identifyCode string) []byte {
	body, _ := json.Marshal(map[string]any{
		"schema_version":  "1.0",
		"batch_id":        "contract-batch",
		"installation_id": "i1",
		"identify_code":   identifyCode,
		"events": []map[string]any{{
			"event_id": "contract-event", "event_type": "notification.posted", "captured_at": "2026-11-23T10:00:00Z",
			"source":           map[string]any{"platform": "android", "package": "com.tencent.mobileqq"},
			"parse_status":     "ok",
			"conversation":     map[string]any{"type": "private", "title": "Alice"},
			"message":          map[string]any{"sender": "Alice", "text": "hi", "notification_posted_at": int64(1795435200000)},
			"raw_notification": map[string]any{"key": "k1", "id": 1, "is_group_summary": false, "messages": []any{}, "historic_messages": []any{}, "text_lines": []any{}},
		}},
	})
	return body
}

func serveSageXUploadContract(r *gin.Engine, body []byte, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/sagex/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSageXUploadController_ReturnsProtocolAck(t *testing.T) {
	r := newSageXUploadTestController(&model.SageXInstallation{TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1})

	body, _ := json.Marshal(map[string]any{
		"schema_version":  "1.0",
		"batch_id":        "b1",
		"sent_at":         "2026-11-23T10:00:00Z",
		"installation_id": "i1",
		"identify_code":   sageXUploadIdentifyCode,
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
	r := newSageXUploadTestController(&model.SageXInstallation{TenantID: 1, ID: 7, InstallationID: "11111111-1111-4111-8111-111111111111", Token: "tok", Status: 1, AccountID: "1"})

	body := []byte(`{
		"schema_version":"1.0",
		"batch_id":"22222222-2222-4222-8222-222222222222",
		"sent_at":"2026-11-23T08:00:00.000Z",
		"installation_id":"11111111-1111-4111-8111-111111111111",
		"identify_code":"identify-code-valid",
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

func TestSageXUploadController_BearerOnlyRejected(t *testing.T) {
	r := newSageXUploadTestController(&model.SageXInstallation{TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1})
	w := serveSageXUploadContract(r, sageXUploadContractBody(""), "tok")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Bearer-only upload must be unauthorized: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSageXUploadController_InvalidIdentifyCodeRejectedEvenWithBearer(t *testing.T) {
	r := newSageXUploadTestController(&model.SageXInstallation{TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1})
	w := serveSageXUploadContract(r, sageXUploadContractBody("invalid-code"), "tok")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid identify_code must be unauthorized: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSageXUploadController_ValidIdentifyCodeWithoutBearerAccepted(t *testing.T) {
	r := newSageXUploadTestController(&model.SageXInstallation{TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1})
	w := serveSageXUploadContract(r, sageXUploadContractBody(sageXUploadIdentifyCode), "")
	if w.Code != http.StatusOK {
		t.Fatalf("valid identify_code without Bearer must be accepted: status=%d body=%s", w.Code, w.Body.String())
	}
}

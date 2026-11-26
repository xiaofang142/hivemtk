package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type ctrlHardeningInstRepo struct {
	inst          *model.SageXInstallation
	getErr        error
	createErr     error
	touchErr      error
	forceConflict bool
}

func (r *ctrlHardeningInstRepo) Create(ctx context.Context, inst *model.SageXInstallation) error {
	_, err := r.CreateIfAbsent(ctx, inst)
	return err
}
func (r *ctrlHardeningInstRepo) CreateIfAbsent(context.Context, *model.SageXInstallation) (bool, error) {
	if r.createErr != nil {
		return false, r.createErr
	}
	if r.forceConflict {
		return false, nil
	}
	return true, nil
}
func (r *ctrlHardeningInstRepo) Update(context.Context, *model.SageXInstallation) error { return nil }
func (r *ctrlHardeningInstRepo) GetByID(context.Context, uint) (*model.SageXInstallation, error) {
	return r.inst, nil
}
func (r *ctrlHardeningInstRepo) GetByToken(context.Context, string) (*model.SageXInstallation, error) {
	return r.inst, nil
}
func (r *ctrlHardeningInstRepo) GetByInstallationID(context.Context, string) (*model.SageXInstallation, error) {
	return r.inst, r.getErr
}
func (r *ctrlHardeningInstRepo) GetByTenantAndInstallationID(
	_ context.Context,
	tenantID uint,
	installationID string,
) (*model.SageXInstallation, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.inst != nil && r.inst.TenantID == tenantID && r.inst.InstallationID == installationID {
		return r.inst, nil
	}
	return nil, nil
}
func (r *ctrlHardeningInstRepo) TouchLastSeen(context.Context, uint, time.Time) error {
	return r.touchErr
}

func newCtrlHardeningController(repo *ctrlHardeningInstRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewSageXUploadService(repo, &ctrlSageXReceiptRepo{}, &ctrlSageXIngress{})
	channels := &ctrlBootstrapChannelRepo{channel: &model.Channel{ID: 1, TenantID: 1, Code: "sagex"}}
	svc.SetChannelResolver(service.NewChannelResolverWithRepos(&ctrlUploadIdentifyAccountRepo{}, channels))
	ctrl := NewSageXUploadController(svc)
	r := gin.New()
	r.POST("/api/integrations/sagex/upload", ctrl.Upload)
	return r
}

func ctrlHardeningBody(installationID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"schema_version":  "1.0",
		"batch_id":        "batch-hardening",
		"installation_id": installationID,
		"identify_code":   sageXUploadIdentifyCode,
		"events": []map[string]any{{
			"event_id": "event-hardening", "event_type": "notification.posted", "captured_at": "2026-11-23T10:00:00Z",
			"source":           map[string]any{"platform": "android", "package": "com.tencent.mobileqq"},
			"parse_status":     "ok",
			"conversation":     map[string]any{"type": "private", "title": "Alice"},
			"message":          map[string]any{"sender": "Alice", "text": "hi", "notification_posted_at": int64(1795435200000)},
			"raw_notification": map[string]any{"key": "k1", "id": 1, "is_group_summary": false, "messages": []any{}, "historic_messages": []any{}, "text_lines": []any{}},
		}},
	})
	return body
}

func serveCtrlHardening(r *gin.Engine, installationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/sagex/upload", bytes.NewReader(ctrlHardeningBody(installationID)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func ctrlHardeningError(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, body)
	}
	return resp.Error
}

func TestSageXUploadControllerHardeningTypedErrors(t *testing.T) {
	tests := []struct {
		name       string
		repo       *ctrlHardeningInstRepo
		id         string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid installation id",
			repo:       &ctrlHardeningInstRepo{},
			id:         strings.Repeat("x", 101),
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_installation_id",
		},
		{
			name: "disabled installation",
			repo: &ctrlHardeningInstRepo{inst: &model.SageXInstallation{
				TenantID: 1, InstallationID: "disabled-id", AccountID: "1", Token: "secret-token", Status: 0,
			}},
			id:         "disabled-id",
			wantStatus: http.StatusForbidden,
			wantError:  "installation_disabled",
		},
		{
			name: "account mismatch",
			repo: &ctrlHardeningInstRepo{inst: &model.SageXInstallation{
				TenantID: 1, InstallationID: "shared-id", AccountID: "999", Token: "secret-token", Status: 1,
			}},
			id:         "shared-id",
			wantStatus: http.StatusForbidden,
			wantError:  "installation_forbidden",
		},
		{
			name: "conflict without reread row",
			repo: &ctrlHardeningInstRepo{
				forceConflict: true,
			},
			id:         "race-id",
			wantStatus: http.StatusConflict,
			wantError:  "installation_conflict",
		},
		{
			name: "lookup db error",
			repo: &ctrlHardeningInstRepo{
				getErr: errors.New("database unavailable"),
			},
			id:         "lookup-error-id",
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal_error",
		},
		{
			name: "create db error",
			repo: &ctrlHardeningInstRepo{
				createErr: errors.New("database unavailable"),
			},
			id:         "create-error-id",
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal_error",
		},
		{
			name: "touch db error",
			repo: &ctrlHardeningInstRepo{inst: &model.SageXInstallation{
				TenantID: 1, InstallationID: "touch-error-id", AccountID: "1", Token: "secret-token", Status: 1,
			}, touchErr: errors.New("database unavailable")},
			id:         "touch-error-id",
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveCtrlHardening(newCtrlHardeningController(tt.repo), tt.id)
			if w.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if got := ctrlHardeningError(t, w.Body.Bytes()); got != tt.wantError {
				t.Fatalf("error=%q want=%q body=%s", got, tt.wantError, w.Body.String())
			}
			body := strings.ToLower(w.Body.String())
			if strings.Contains(body, "secret-token") || strings.Contains(body, "database unavailable") || strings.Contains(body, `"token"`) {
				t.Fatalf("error response leaked token/internal detail: %s", w.Body.String())
			}
		})
	}
}

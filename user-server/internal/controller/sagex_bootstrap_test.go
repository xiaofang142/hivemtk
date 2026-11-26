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
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type ctrlBootstrapAccountRepo struct {
	repository.ChannelAccountRepository
	acc *model.ChannelAccount
	err error
}

func (r *ctrlBootstrapAccountRepo) Available() bool { return r != nil }

func (r *ctrlBootstrapAccountRepo) GetByIdentifyCode(context.Context, string) (*model.ChannelAccount, error) {
	return r.acc, r.err
}

type ctrlBootstrapChannelRepo struct {
	repository.ChannelRepository
	channel *model.Channel
}

func (r *ctrlBootstrapChannelRepo) Available() bool { return r != nil }

func (r *ctrlBootstrapChannelRepo) GetByID(_ context.Context, tenantID, id uint) (*model.Channel, error) {
	if r.channel == nil || r.channel.TenantID != tenantID || r.channel.ID != id {
		return nil, nil
	}
	channel := *r.channel
	return &channel, nil
}

type ctrlBootstrapInstRepo struct {
	rows        []*model.SageXInstallation
	getErr      error
	createErr   error
	createCalls int
	createHook  func(*model.SageXInstallation)
}

func (r *ctrlBootstrapInstRepo) Create(ctx context.Context, inst *model.SageXInstallation) error {
	_, err := r.CreateIfAbsent(ctx, inst)
	return err
}
func (r *ctrlBootstrapInstRepo) CreateIfAbsent(_ context.Context, inst *model.SageXInstallation) (bool, error) {
	r.createCalls++
	if r.createHook != nil {
		r.createHook(inst)
	}
	if r.createErr != nil {
		return false, r.createErr
	}
	for _, row := range r.rows {
		if row.TenantID == inst.TenantID && row.InstallationID == inst.InstallationID && inst.InstallationID != "" {
			return false, nil
		}
	}
	cp := *inst
	cp.ID = uint(len(r.rows) + 1)
	r.rows = append(r.rows, &cp)
	return true, nil
}
func (r *ctrlBootstrapInstRepo) Update(context.Context, *model.SageXInstallation) error {
	return nil
}
func (r *ctrlBootstrapInstRepo) GetByID(context.Context, uint) (*model.SageXInstallation, error) {
	return nil, nil
}
func (r *ctrlBootstrapInstRepo) GetByToken(context.Context, string) (*model.SageXInstallation, error) {
	return nil, nil
}
func (r *ctrlBootstrapInstRepo) GetByInstallationID(_ context.Context, installationID string) (*model.SageXInstallation, error) {
	return r.GetByTenantAndInstallationID(context.Background(), 0, installationID)
}
func (r *ctrlBootstrapInstRepo) GetByTenantAndInstallationID(
	_ context.Context,
	_ uint,
	installationID string,
) (*model.SageXInstallation, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, row := range r.rows {
		if row.InstallationID == installationID {
			cp := *row
			return &cp, nil
		}
	}
	return nil, nil
}
func (r *ctrlBootstrapInstRepo) TouchLastSeen(context.Context, uint, time.Time) error {
	return nil
}

func newSageXBootstrapTestController(
	repo *ctrlBootstrapInstRepo,
	acc *model.ChannelAccount,
	resolveErr error,
) *gin.Engine {
	if acc != nil {
		account := *acc
		if account.ChannelID == 0 {
			account.ChannelID = 101
		}
		acc = &account
	}
	channelTenantID := uint(11)
	channelID := uint(101)
	if acc != nil && acc.TenantID != 0 {
		channelTenantID = acc.TenantID
		channelID = acc.ChannelID
	}
	channel := &model.Channel{TenantID: channelTenantID, ID: channelID, Code: "telegram"}
	gin.SetMode(gin.TestMode)
	svc := service.NewSageXUploadService(repo, &ctrlSageXReceiptRepo{}, &ctrlSageXIngress{})
	svc.SetChannelResolver(service.NewChannelResolverWithRepos(
		&ctrlBootstrapAccountRepo{acc: acc, err: resolveErr},
		&ctrlBootstrapChannelRepo{channel: channel},
	))
	ctrl := NewSageXUploadController(svc)
	engine := gin.New()
	engine.POST("/api/integrations/sagex/bootstrap", ctrl.Bootstrap)
	return engine
}

func postSageXBootstrap(engine *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/sagex/bootstrap", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func sagexBootstrapReason(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Data struct {
			Reason string `json:"reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode bootstrap error response: %v body=%s", err, body)
	}
	return resp.Data.Reason
}

func TestSageXBootstrapControllerTypedErrors(t *testing.T) {
	validAccount := &model.ChannelAccount{TenantID: 11, ID: 101, ChannelID: 1, IdentifyCode: "code-a"}
	tests := []struct {
		name        string
		body        string
		acc         *model.ChannelAccount
		resolveErr  error
		rows        []*model.SageXInstallation
		getErr      error
		createErr   error
		createHook  func(*model.SageXInstallation)
		wantStatus  int
		wantReason  string
		wantCreates int
		secret      string
	}{
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantReason: "invalid_request",
		},
		{
			name:       "missing identify code",
			body:       `{"installation_id":"id-a"}`,
			wantStatus: http.StatusBadRequest,
			wantReason: "invalid_identify_code",
		},
		{
			name:       "oversized installation id",
			body:       `{"installation_id":"` + strings.Repeat("x", 101) + `","identify_code":"code-a"}`,
			acc:        validAccount,
			wantStatus: http.StatusBadRequest,
			wantReason: "invalid_installation_id",
		},
		{
			name:       "invalid identify code",
			body:       `{"installation_id":"id-a","identify_code":"bad"}`,
			resolveErr: errors.New("invalid identify code"),
			wantStatus: http.StatusUnauthorized,
			wantReason: "unauthorized",
		},
		{
			name: "cross tenant row",
			body: `{"installation_id":"victim-id","identify_code":"code-a"}`,
			acc:  validAccount,
			rows: []*model.SageXInstallation{{
				TenantID: 22, InstallationID: "victim-id", AccountID: "202", Token: "victim-secret-token", Status: 1,
			}},
			wantStatus:  http.StatusForbidden,
			wantReason:  "installation_forbidden",
			wantCreates: 0,
			secret:      "victim-secret-token",
		},
		{
			name: "cross account row",
			body: `{"installation_id":"shared-id","identify_code":"code-a"}`,
			acc:  validAccount,
			rows: []*model.SageXInstallation{{
				TenantID: 11, InstallationID: "shared-id", AccountID: "999", Token: "other-account-secret-token", Status: 1,
			}},
			wantStatus:  http.StatusForbidden,
			wantReason:  "installation_forbidden",
			wantCreates: 0,
			secret:      "other-account-secret-token",
		},
		{
			name: "disabled row",
			body: `{"installation_id":"disabled-id","identify_code":"code-a"}`,
			acc:  validAccount,
			rows: []*model.SageXInstallation{{
				TenantID: 11, InstallationID: "disabled-id", AccountID: "101", Token: "disabled-secret-token", Status: 0,
			}},
			wantStatus:  http.StatusForbidden,
			wantReason:  "installation_disabled",
			wantCreates: 0,
			secret:      "disabled-secret-token",
		},
		{
			name:       "lookup db error",
			body:       `{"installation_id":"id-a","identify_code":"code-a"}`,
			acc:        validAccount,
			getErr:     errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantReason: "internal_error",
		},
		{
			name:        "create db error",
			body:        `{"installation_id":"id-a","identify_code":"code-a"}`,
			acc:         validAccount,
			createErr:   errors.New("database unavailable"),
			wantStatus:  http.StatusInternalServerError,
			wantReason:  "internal_error",
			wantCreates: 1,
		},
		{
			name:        "conflict without reread row",
			body:        `{"installation_id":"race-id","identify_code":"code-a"}`,
			acc:         validAccount,
			createErr:   errors.New("duplicate key value violates unique constraint"),
			wantStatus:  http.StatusConflict,
			wantReason:  "installation_conflict",
			wantCreates: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ctrlBootstrapInstRepo{
				rows:       tt.rows,
				getErr:     tt.getErr,
				createErr:  tt.createErr,
				createHook: tt.createHook,
			}
			engine := newSageXBootstrapTestController(repo, tt.acc, tt.resolveErr)
			w := postSageXBootstrap(engine, tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if got := sagexBootstrapReason(t, w.Body.Bytes()); got != tt.wantReason {
				t.Fatalf("reason=%q want=%q body=%s", got, tt.wantReason, w.Body.String())
			}
			if repo.createCalls != tt.wantCreates {
				t.Fatalf("create calls=%d want=%d", repo.createCalls, tt.wantCreates)
			}
			if tt.secret != "" && strings.Contains(w.Body.String(), tt.secret) {
				t.Fatalf("error response leaked token %q: %s", tt.secret, w.Body.String())
			}
			if strings.Contains(strings.ToLower(w.Body.String()), `"token"`) {
				t.Fatalf("error response contains token field: %s", w.Body.String())
			}
		})
	}
}

func TestSageXBootstrapControllerSuccessReturnsTokenOnlyAfterValidation(t *testing.T) {
	acc := &model.ChannelAccount{TenantID: 11, ID: 101, ChannelID: 1, IdentifyCode: "code-a"}
	repo := &ctrlBootstrapInstRepo{}
	engine := newSageXBootstrapTestController(repo, acc, nil)
	w := postSageXBootstrap(engine, `{"installation_id":"id-a","identify_code":"code-a"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode success response: %v", err)
	}
	if !strings.HasPrefix(resp.Data.Token, "sx_") {
		t.Fatalf("token missing or invalid: %q", resp.Data.Token)
	}
}

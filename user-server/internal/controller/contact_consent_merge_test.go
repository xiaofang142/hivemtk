package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 客户360沿用原接口；合并后两端身份返回同一组原始事实，故障不能伪装空列表。
func TestContactConsentController_MergedIdentityTimelineAndPreferences(t *testing.T) {
	originalMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(originalMode) })
	database := testutil.NewTestDB(t,
		&model.Customer{}, &model.CustomerEvent{}, &model.CustomerTag{},
		&model.CustomerSession{}, &model.CustomerChannel{}, &model.CustomerDoNotContact{},
		&model.CSATSurvey{}, &model.Clue{}, &model.ScriptExposureLog{}, &model.OperationLog{},
		&model.ContactConsentEvent{}, &model.ContactPreference{}, &model.ContactConsentIdentityLink{},
	)
	db.SetTestDB(database)
	ctx := tenant.WithTenantID(context.Background(), 41)
	primary := model.Customer{TenantID: 41, ID: "fa14-api-primary", UnifiedID: "fa14-api-one-primary", Tags: "[]", ChurnRisk: "low"}
	secondary := model.Customer{TenantID: 41, ID: "fa14-api-secondary", UnifiedID: "fa14-api-one-secondary", Tags: "[]", ChurnRisk: "low"}
	require.NoError(t, database.WithContext(ctx).Create(&primary).Error)
	require.NoError(t, database.WithContext(ctx).Create(&secondary).Error)
	consents := service.NewContactConsentService(repository.NewContactConsentRepository(database))
	require.NoError(t, consents.Record(ctx, service.ConsentRecordInput{OneID: primary.UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}))
	require.NoError(t, consents.Record(ctx, service.ConsentRecordInput{OneID: secondary.UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke, OccurredAt: time.Now().Add(-time.Hour), EvidenceRef: "fa14-controlled-evidence"}))
	require.NoError(t, consents.SetPreference(ctx, secondary.UnifiedID, model.ConsentPurposeMarketing, "sms", true, nil, ""))
	require.NoError(t, service.NewCustomerService().MergeCustomers(ctx, primary.ID, secondary.ID))
	otherCtx := tenant.WithTenantID(context.Background(), 42)
	require.NoError(t, consents.Record(otherCtx, service.ConsentRecordInput{OneID: primary.UnifiedID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}))

	router := gin.New()
	controller := NewContactConsentController(consents)
	router.GET("/api/contacts/:customer_id/consents", controller.Timeline)
	router.GET("/api/contacts/:customer_id/preferences", controller.ListPreferences)
	get := func(requestCtx context.Context, oneID, endpoint string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/contacts/"+oneID+"/"+endpoint, nil).WithContext(requestCtx)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, oneID := range []string{primary.UnifiedID, secondary.UnifiedID} {
		response := get(ctx, oneID, "consents")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var timeline struct {
			Code int `json:"code"`
			Data struct {
				List  []model.ContactConsentEvent `json:"list"`
				Total int                         `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &timeline))
		require.Zero(t, timeline.Code)
		require.Equal(t, 2, timeline.Data.Total)
		require.Len(t, timeline.Data.List, 2)
		require.Equal(t, primary.UnifiedID, timeline.Data.List[0].OneID)
		require.Equal(t, secondary.UnifiedID, timeline.Data.List[1].OneID)
		require.Equal(t, "fa14-controlled-evidence", timeline.Data.List[1].EvidenceRef)
		response = get(ctx, oneID, "preferences")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var preferences struct {
			Code int `json:"code"`
			Data struct {
				List  []model.ContactPreference `json:"list"`
				Total int                       `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preferences))
		require.Zero(t, preferences.Code)
		require.Equal(t, 1, preferences.Data.Total)
		require.Len(t, preferences.Data.List, 1)
		require.Equal(t, secondary.UnifiedID, preferences.Data.List[0].OneID)
		require.True(t, preferences.Data.List[0].Paused)
	}
	response := get(otherCtx, primary.UnifiedID, "consents")
	require.Equal(t, http.StatusOK, response.Code)
	var other struct {
		Data struct {
			List []model.ContactConsentEvent `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &other))
	require.Len(t, other.Data.List, 1)
	require.Equal(t, uint(42), other.Data.List[0].TenantID)

	for _, endpoint := range []string{"consents", "preferences"} {
		response := get(context.Background(), primary.UnifiedID, endpoint)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	require.NoError(t, database.Callback().Query().Before("gorm:query").Register("fa14:api-alias-fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "contact_consent_identity_links" {
			tx.AddError(context.DeadlineExceeded)
		}
	}))
	t.Cleanup(func() { database.Callback().Query().Remove("fa14:api-alias-fault") })
	for _, endpoint := range []string{"consents", "preferences"} {
		response := get(ctx, primary.UnifiedID, endpoint)
		require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	}
}

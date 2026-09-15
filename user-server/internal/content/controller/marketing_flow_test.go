package controller

import (
	"bytes"
	"encoding/json"
	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/service"
	"net/http"
	"net/http/httptest"
	"testing"

	cdpmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupMarketingFlowTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&contentmodel.MarketingFlow{},
		&contentmodel.FlowExecution{},
		&cdpmodel.UserTag{},
	)
}

func setupMarketingFlowRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)

	flowService := service.NewMarketingFlowServiceWithDB(db)

	controller := &MarketingFlowController{
		flowService: flowService,
	}

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("user_id", "merchant-test")
		c.Set("user_id", uint(1))
		c.Next()
	})

	router.POST("/api/flows", controller.CreateFlow)
	router.GET("/api/flows", controller.GetFlowList)
	router.GET("/api/flows/:id", controller.GetFlowByID)
	router.PUT("/api/flows/:id", controller.UpdateFlow)
	router.DELETE("/api/flows/:id", controller.DeleteFlow)
	router.POST("/api/flows/:id/activate", controller.ActivateFlow)
	router.POST("/api/flows/:id/pause", controller.PauseFlow)
	router.POST("/api/flows/:id/stop", controller.StopFlow)
	router.GET("/api/flows/:id/executions", controller.GetExecutionList)
	router.GET("/api/flows/:id/stats", controller.GetExecutionStats)

	return router
}

// TestMarketingFlowController_CreateFlow 测试创建流程
func TestMarketingFlowController_CreateFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	tests := []struct {
		name           string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			requestBody: gin.H{
				"name":         "Test Flow",
				"description":  "A test marketing flow",
				"trigger_type": "event",
				"trigger_config": gin.H{
					"event": "user_signup",
				},
				"flow_data": gin.H{
					"nodes": []gin.H{
						{"id": "start", "type": "trigger"},
						{"id": "action1", "type": "action"},
					},
				},
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name: "missing_name",
			requestBody: gin.H{
				"description":  "A test marketing flow",
				"trigger_type": "event",
			},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name: "missing_trigger_type",
			requestBody: gin.H{
				"name":        "Test Flow",
				"description": "A test marketing flow",
			},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "invalid_json",
			requestBody:    "invalid",
			expectedStatus: 400,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w *httptest.ResponseRecorder
			var req *http.Request

			if tt.requestBody == "invalid" {
				w = httptest.NewRecorder()
				req = httptest.NewRequest("POST", "/api/flows", bytes.NewReader([]byte("invalid-json")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				jsonData, _ := json.Marshal(tt.requestBody)
				w = httptest.NewRecorder()
				req = httptest.NewRequest("POST", "/api/flows", bytes.NewBuffer(jsonData))
				req.Header.Set("Content-Type", "application/json")
			}

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_GetFlowList 测试获取流程列表
func TestMarketingFlowController_GetFlowList(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	for i := 1; i <= 5; i++ {
		db.Create(&contentmodel.MarketingFlow{
			Name:        "Flow " + string(rune('0'+i)),
			Description: "Description " + string(rune('0'+i)),
			Status:      contentmodel.FlowStatusDraft,
			TriggerType: contentmodel.TriggerTypeUserFollow,
			CreatedBy:   1,
		})
	}

	tests := []struct {
		name           string
		query          string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			query:          "",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_page_size",
			query:          "?page=1&page_size=20",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "page_2",
			query:          "?page=2&page_size=2",
			expectedStatus: 200,
			expectSuccess:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/flows"+tt.query, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
				assert.NotNil(t, response["data"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_GetFlowByID 测试获取流程详情
func TestMarketingFlowController_GetFlowByID(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	testFlow := &contentmodel.MarketingFlow{
		Name:        "Test Flow",
		Description: "Test Description",
		Status:      contentmodel.FlowStatusActive,
		TriggerType: contentmodel.TriggerTypeUserFollow,
		CreatedBy:   1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "valid_id",
			url:            "/api/flows/1",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id_format",
			url:            "/api/flows/invalid",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "negative_id",
			url:            "/api/flows/-1",
			expectedStatus: 400,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_UpdateFlow 测试更新流程
func TestMarketingFlowController_UpdateFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	testFlow := &contentmodel.MarketingFlow{
		Name:        "Original Name",
		Description: "Original Description",
		Status:      contentmodel.FlowStatusDraft,
		TriggerType: contentmodel.TriggerTypeUserFollow,
		CreatedBy:   1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			url:  "/api/flows/1",
			requestBody: gin.H{
				"name":        "Updated Name",
				"description": "Updated Description",
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name: "invalid_id",
			url:  "/api/flows/invalid",
			requestBody: gin.H{
				"name": "Updated Name",
			},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name: "nonexistent_id",
			url:  "/api/flows/9999",
			requestBody: gin.H{
				"name": "Updated Name",
			},
			expectedStatus: 404,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, _ := json.Marshal(tt.requestBody)
			w := httptest.NewRecorder()
			req := httptest.NewRequest("PUT", tt.url, bytes.NewBuffer(jsonData))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_DeleteFlow 测试删除流程
func TestMarketingFlowController_DeleteFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	testFlow := &contentmodel.MarketingFlow{
		Name:        "To Delete",
		Status:      contentmodel.FlowStatusDraft,
		TriggerType: contentmodel.TriggerTypeUserFollow,
		CreatedBy:   1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/flows/1",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/flows/invalid",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999",
			expectedStatus: 404,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("DELETE", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_ActivateFlow 测试激活流程
func TestMarketingFlowController_ActivateFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	flowData := `{"nodes": [{"id": "start", "type": "trigger", "name": "Start"}]}`
	testFlow := &contentmodel.MarketingFlow{
		Name:          "To Activate",
		Description:   "Test flow",
		Status:        contentmodel.FlowStatusDraft,
		TriggerType:   contentmodel.TriggerTypeUserFollow,
		TriggerConfig: "{}",
		FlowData:      flowData,
		CreatedBy:     1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/flows/1/activate",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/flows/invalid/activate",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999/activate",
			expectedStatus: 404,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_PauseFlow 测试暂停流程
func TestMarketingFlowController_PauseFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	flowData := `{"nodes": [{"id": "start", "type": "trigger", "name": "Start"}]}`
	testFlow := &contentmodel.MarketingFlow{
		Name:          "To Pause",
		Status:        contentmodel.FlowStatusActive,
		TriggerType:   contentmodel.TriggerTypeUserFollow,
		TriggerConfig: "{}",
		FlowData:      flowData,
		CreatedBy:     1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/flows/1/pause",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/flows/invalid/pause",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999/pause",
			expectedStatus: 404,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_StopFlow 测试停止流程
func TestMarketingFlowController_StopFlow(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	flowData := `{"nodes": [{"id": "start", "type": "trigger", "name": "Start"}]}`
	testFlow := &contentmodel.MarketingFlow{
		Name:          "To Stop",
		Status:        contentmodel.FlowStatusActive,
		TriggerType:   contentmodel.TriggerTypeUserFollow,
		TriggerConfig: "{}",
		FlowData:      flowData,
		CreatedBy:     1,
	}
	db.Create(testFlow)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/flows/1/stop",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/flows/invalid/stop",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999/stop",
			expectedStatus: 404,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_GetExecutionList 测试获取执行记录列表
func TestMarketingFlowController_GetExecutionList(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	testFlow := &contentmodel.MarketingFlow{
		Name:        "Flow with Executions",
		Status:      contentmodel.FlowStatusActive,
		TriggerType: contentmodel.TriggerTypeUserFollow,
		CreatedBy:   1,
	}
	db.Create(testFlow)

	for i := 1; i <= 3; i++ {
		db.Create(&contentmodel.FlowExecution{
			FlowID:      testFlow.ID,
			UserID:      "user-1",
			TriggerID:   "trigger-1",
			Status:      "running",
			CurrentNode: "Step " + string(rune('0'+i)),
		})
	}

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "valid_flow_id",
			url:            "/api/flows/1/executions",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_flow_id",
			url:            "/api/flows/invalid/executions",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "with_pagination",
			url:            "/api/flows/1/executions?page=1&page_size=10",
			expectedStatus: 200,
			expectSuccess:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestMarketingFlowController_GetExecutionStats 测试获取执行统计
func TestMarketingFlowController_GetExecutionStats(t *testing.T) {
	db := setupMarketingFlowTestDB(t)
	router := setupMarketingFlowRouter(t, db)

	testFlow := &contentmodel.MarketingFlow{
		Name:        "Flow with Stats",
		Status:      contentmodel.FlowStatusActive,
		TriggerType: contentmodel.TriggerTypeUserFollow,
		CreatedBy:   1,
	}
	db.Create(testFlow)

	for i := 1; i <= 5; i++ {
		status := "completed"
		if i%2 == 0 {
			status = "failed"
		}
		db.Create(&contentmodel.FlowExecution{
			FlowID:      testFlow.ID,
			UserID:      "user-1",
			TriggerID:   "trigger-1",
			Status:      status,
			CurrentNode: "Step " + string(rune('0'+i)),
		})
	}

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "valid_id",
			url:            "/api/flows/1/stats",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/flows/invalid/stats",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/flows/9999/stats",
			expectedStatus: 200,
			expectSuccess:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.url, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectSuccess {
				assert.Equal(t, float64(0), response["code"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

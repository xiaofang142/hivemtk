package controller

import (
	"bytes"
	"encoding/json"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupChurnPredictionTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.ChurnPrediction{},
		&model.ChurnWarning{},
		&model.ChurnModelConfig{},
		&model.ChurnStatistics{},
	)
}

func setupChurnPredictionRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)

	churnService := service.NewChurnPredictionServiceWithDB(db)

	controller := &ChurnPredictionController{
		churnService: churnService,
	}

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("user_id", "merchant-test")
		c.Set("user_id", uint(1))
		c.Next()
	})

	router.GET("/api/churn/prediction", controller.GetChurnPrediction)
	router.GET("/api/churn/predictions", controller.GetChurnPredictions)
	router.GET("/api/churn/high-risk", controller.GetHighRiskUsers)
	router.GET("/api/churn/warnings", controller.GetChurnWarnings)
	router.GET("/api/churn/warnings/unhandled", controller.GetUnhandledWarnings)
	router.POST("/api/churn/warnings/:id/handle", controller.MarkWarningHandled)
	router.GET("/api/churn/config", controller.GetModelConfig)
	router.POST("/api/churn/config", controller.SaveModelConfig)
	router.GET("/api/churn/statistics", controller.GetChurnStatistics)
	router.GET("/api/churn/risk-distribution", controller.GetRiskDistribution)

	return router
}

// TestChurnPredictionController_GetChurnPrediction 测试获取用户流失预测
func TestChurnPredictionController_GetChurnPrediction(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "missing_user_id",
			url:            "/api/churn/prediction",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "user_not_found",
			url:            "/api/churn/prediction?user_id=user-nonexistent",
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

// TestChurnPredictionController_GetChurnPredictions 测试获取流失预测列表
func TestChurnPredictionController_GetChurnPredictions(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			url:            "/api/churn/predictions",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_page_size",
			url:            "/api/churn/predictions?page=1&page_size=20",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "page_2",
			url:            "/api/churn/predictions?page=2&page_size=5",
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
				assert.NotNil(t, response["data"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestChurnPredictionController_GetHighRiskUsers 测试获取高风险用户列表
func TestChurnPredictionController_GetHighRiskUsers(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_limit",
			url:            "/api/churn/high-risk",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_limit",
			url:            "/api/churn/high-risk?limit=10",
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

// TestChurnPredictionController_GetChurnWarnings 测试获取流失预警列表
func TestChurnPredictionController_GetChurnWarnings(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			url:            "/api/churn/warnings",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_pagination",
			url:            "/api/churn/warnings?page=1&page_size=20",
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
				assert.NotNil(t, response["data"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestChurnPredictionController_GetUnhandledWarnings 测试获取未处理的流失预警
func TestChurnPredictionController_GetUnhandledWarnings(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			url:            "/api/churn/warnings/unhandled",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_pagination",
			url:            "/api/churn/warnings/unhandled?page=1&page_size=20",
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
				assert.NotNil(t, response["data"])
			} else {
				assert.NotEqual(t, float64(0), response["code"])
			}
		})
	}
}

// TestChurnPredictionController_MarkWarningHandled 测试标记预警为已处理
func TestChurnPredictionController_MarkWarningHandled(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			url:  "/api/churn/warnings/1/handle",
			requestBody: gin.H{
				"note": "已联系用户",
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/churn/warnings/invalid/handle",
			requestBody:    gin.H{"note": "test"},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "invalid_json",
			url:            "/api/churn/warnings/1/handle",
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
				req = httptest.NewRequest("POST", tt.url, bytes.NewReader([]byte("invalid-json")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				jsonData, _ := json.Marshal(tt.requestBody)
				w = httptest.NewRecorder()
				req = httptest.NewRequest("POST", tt.url, bytes.NewBuffer(jsonData))
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

// TestChurnPredictionController_GetModelConfig 测试获取模型配置
func TestChurnPredictionController_GetModelConfig(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/churn/config",
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

// TestChurnPredictionController_SaveModelConfig 测试保存模型配置
func TestChurnPredictionController_SaveModelConfig(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			url:  "/api/churn/config",
			requestBody: gin.H{
				"inactive_threshold":   30,
				"purchase_threshold":   2,
				"critical_risk_score":  80,
				"high_risk_score":      60,
				"inactive_days_weight": 0.4,
				"purchase_freq_weight": 0.3,
				"order_value_weight":   0.2,
				"engagement_weight":    0.1,
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_json",
			url:            "/api/churn/config",
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
				req = httptest.NewRequest("POST", tt.url, bytes.NewReader([]byte("invalid-json")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				jsonData, _ := json.Marshal(tt.requestBody)
				w = httptest.NewRecorder()
				req = httptest.NewRequest("POST", tt.url, bytes.NewBuffer(jsonData))
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

// TestChurnPredictionController_GetChurnStatistics 测试获取流失统计
func TestChurnPredictionController_GetChurnStatistics(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "missing_date_params",
			url:            "/api/churn/statistics",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "success",
			url:            "/api/churn/statistics?start_date=2024-01-01&end_date=2024-12-31",
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

// TestChurnPredictionController_GetRiskDistribution 测试获取风险分布
func TestChurnPredictionController_GetRiskDistribution(t *testing.T) {
	db := setupChurnPredictionTestDB(t)
	router := setupChurnPredictionRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/churn/risk-distribution",
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

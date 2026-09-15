package controller

import (
	"bytes"
	"encoding/json"
	sysmodel "hivemtk-user/internal/model"
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

func setupCustomReportTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.CustomReport{},
		&sysmodel.CustomerSession{},
		&sysmodel.Clue{},
		&sysmodel.UserRFM{},
	)
}

func setupCustomReportRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)

	reportService := service.NewCustomReportServiceWithDB(db)

	controller := &CustomReportController{
		reportService: reportService,
	}

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("user_id", "merchant-test")
		c.Set("user_id", uint(1))
		c.Next()
	})

	router.POST("/api/reports", controller.CreateReport)
	router.GET("/api/reports/:id", controller.GetReport)
	router.GET("/api/reports", controller.GetReportList)
	router.PUT("/api/reports/:id", controller.UpdateReport)
	router.DELETE("/api/reports/:id", controller.DeleteReport)
	router.GET("/api/reports/templates", controller.GetPublicTemplates)
	router.POST("/api/reports/templates/:id/use", controller.UseTemplate)
	router.GET("/api/reports/:id/data", controller.QueryReportData)

	return router
}

// TestCustomReportController_CreateReport 测试创建报表
func TestCustomReportController_CreateReport(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	tests := []struct {
		name           string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			requestBody: gin.H{
				"name":        "Test Report",
				"description": "A test report",
				"data_source": "sessions",
				"chart_type":  "bar",
				"dimensions":  []gin.H{{"field": "platform", "type": "categorical"}},
				"metrics":     []gin.H{{"field": "message_count", "aggregation": "sum"}},
				"is_public":   false,
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name: "missing_name",
			requestBody: gin.H{
				"description": "A test report",
				"data_source": "sessions",
				"chart_type":  "bar",
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name: "invalid_data_source",
			requestBody: gin.H{
				"name":        "Test Report",
				"data_source": "invalid_source",
				"chart_type":  "bar",
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
				req = httptest.NewRequest("POST", "/api/reports", bytes.NewReader([]byte("invalid-json")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				jsonData, _ := json.Marshal(tt.requestBody)
				w = httptest.NewRecorder()
				req = httptest.NewRequest("POST", "/api/reports", bytes.NewBuffer(jsonData))
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

// TestCustomReportController_GetReport 测试获取报表详情
func TestCustomReportController_GetReport(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	testReport := &model.CustomReport{
		Name:        "Test Report",
		Description: "Test Description",
		DataSource:  "sessions",
		ChartType:   "bar",
		IsPublic:    true,
	}
	db.Create(testReport)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "valid_id",
			url:            "/api/reports/1",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/reports/invalid",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/reports/9999",
			expectedStatus: 404,
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

// TestCustomReportController_GetReportList 测试获取报表列表
func TestCustomReportController_GetReportList(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	for i := 1; i <= 5; i++ {
		db.Create(&model.CustomReport{
			Name:        "Report " + string(rune('0'+i)),
			Description: "Description " + string(rune('0'+i)),
			DataSource:  "sessions",
			ChartType:   "bar",
		})
	}

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			url:            "/api/reports",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_page_size",
			url:            "/api/reports?page=1&page_size=20",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "page_2",
			url:            "/api/reports?page=2&page_size=2",
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

// TestCustomReportController_UpdateReport 测试更新报表
func TestCustomReportController_UpdateReport(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	// CreatedBy 必须等于测试路由注入的 user_id(1)：service.UpdateReport 会做
	// `!isAdmin && report.CreatedBy != userID` 归属校验，夹具不设 CreatedBy(=0)
	// 会让"成功"用例必然拿到 403 无权限。
	testReport := &model.CustomReport{
		Name:        "Original Name",
		Description: "Original Description",
		DataSource:  "sessions",
		ChartType:   "bar",
		CreatedBy:   1,
	}
	db.Create(testReport)

	tests := []struct {
		name           string
		url            string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			url:  "/api/reports/1",
			requestBody: gin.H{
				"name":        "Updated Name",
				"description": "Updated Description",
				"data_source": "sessions",
				"chart_type":  "bar",
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name: "invalid_id",
			url:  "/api/reports/invalid",
			requestBody: gin.H{
				"name": "Updated Name",
			},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "invalid_json",
			url:            "/api/reports/1",
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
				req = httptest.NewRequest("PUT", tt.url, bytes.NewReader([]byte("invalid-json")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				jsonData, _ := json.Marshal(tt.requestBody)
				w = httptest.NewRecorder()
				req = httptest.NewRequest("PUT", tt.url, bytes.NewBuffer(jsonData))
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

// TestCustomReportController_DeleteReport 测试删除报表
func TestCustomReportController_DeleteReport(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	// 同 UpdateReport：DeleteReport 同样校验归属，CreatedBy 须与注入的 user_id 一致。
	testReport := &model.CustomReport{
		Name:       "To Delete",
		DataSource: "sessions",
		ChartType:  "bar",
		CreatedBy:  1,
	}
	db.Create(testReport)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/reports/1",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/reports/invalid",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/reports/9999",
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

// TestCustomReportController_GetPublicTemplates 测试获取公开模板
func TestCustomReportController_GetPublicTemplates(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/reports/templates",
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

// TestCustomReportController_UseTemplate 测试使用模板
func TestCustomReportController_UseTemplate(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	template := &model.CustomReport{
		Name:        "Template",
		Description: "Public Template",
		DataSource:  "sessions",
		ChartType:   "bar",
		IsPublic:    true,
	}
	db.Create(template)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/reports/templates/1/use",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/reports/templates/invalid/use",
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "nonexistent_id",
			url:            "/api/reports/templates/9999/use",
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

// TestCustomReportController_QueryReportData 测试查询报表数据
func TestCustomReportController_QueryReportData(t *testing.T) {
	db := setupCustomReportTestDB(t)
	router := setupCustomReportRouter(t, db)

	testReport := &model.CustomReport{
		Name:       "Test Report",
		DataSource: "sessions",
		ChartType:  "bar",
		IsPublic:   true,
	}
	db.Create(testReport)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success_without_params",
			url:            "/api/reports/1/data",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "with_date_params",
			url:            "/api/reports/1/data?start_time=2024-01-01&end_time=2024-12-31",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_id",
			url:            "/api/reports/invalid/data",
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

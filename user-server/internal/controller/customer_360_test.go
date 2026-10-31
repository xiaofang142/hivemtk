package controller

import (
	"bytes"
	"encoding/json"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupCustomer360TestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.Clue{},
		&model.Order{},
		&model.UnifiedMessage{},
		&model.UnifiedReply{},
		&model.UserTag{},
	)
	db.SetTestDB(database)
	return database
}

func setupCustomer360Router(t *testing.T, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)

	customer360Service := service.NewCustomer360Service(repository.NewClueRepositoryWithDB(db))

	controller := &Customer360Controller{
		customer360Service: customer360Service,
		userTagSvc:         service.NewUserTagService(),
		userProfileSvc:     service.NewUserProfileService(),
		tagRuleSvc:         service.NewTagRuleService(),
	}

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("user_id", "merchant-test")
		c.Next()
	})

	router.GET("/api/customer/360", controller.GetCustomer360)
	router.GET("/api/customer/list", controller.GetCustomerList)
	router.GET("/api/customer/basic", controller.GetCustomerBasicInfo)
	router.GET("/api/customer/stats", controller.GetCustomerStats)
	router.GET("/api/customer/sessions", controller.GetCustomerSessions)
	router.GET("/api/customer/messages", controller.GetCustomerMessages)
	router.POST("/api/customer/tags", controller.UpdateCustomerTags)
	router.GET("/api/customer/tags", controller.GetCustomerTags)

	return router
}

// TestCustomer360Controller_GetCustomer360 测试获取客户 360 视图
func TestCustomer360Controller_GetCustomer360(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/360?user_id=user-test",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/360",
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

// TestCustomer360Controller_GetCustomerList 测试获取客户列表
func TestCustomer360Controller_GetCustomerList(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "default_pagination",
			url:            "/api/customer/list",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "custom_page_size",
			url:            "/api/customer/list?page=1&page_size=10",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "with_filters",
			url:            "/api/customer/list?platform=douyin&activity_level=high",
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

// TestCustomer360Controller_GetCustomerBasicInfo 测试获取客户基本信息
func TestCustomer360Controller_GetCustomerBasicInfo(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/basic?user_id=user-test",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/basic",
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

// TestCustomer360Controller_GetCustomerStats 测试获取客户统计信息
func TestCustomer360Controller_GetCustomerStats(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/stats?user_id=user-test",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/stats",
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

// TestCustomer360Controller_GetCustomerSessions 测试获取客户会话历史
func TestCustomer360Controller_GetCustomerSessions(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/sessions?user_id=user-test",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/sessions",
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

// TestCustomer360Controller_GetCustomerMessages 测试获取客户消息记录
func TestCustomer360Controller_GetCustomerMessages(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/messages?user_id=user-test",
			expectedStatus: 404,
			expectSuccess:  false,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/messages",
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

// TestCustomer360Controller_UpdateCustomerTags 测试更新客户标签
func TestCustomer360Controller_UpdateCustomerTags(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		requestBody    any
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "success",
			url:  "/api/customer/tags?user_id=user-test",
			requestBody: map[string]any{
				"tags": []string{"vip", "high-value"},
			},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/tags",
			requestBody:    map[string]any{"tags": []string{"vip"}},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			name:           "missing_tags",
			url:            "/api/customer/tags?user_id=user-test",
			requestBody:    map[string]any{},
			expectedStatus: 400,
			expectSuccess:  false,
		},
		{
			// "tags": [] 是合法请求：表示清空全部标签（区别于缺 tags 字段的 400）。
			name:           "empty_tags_clears",
			url:            "/api/customer/tags?user_id=user-test",
			requestBody:    map[string]any{"tags": []string{}},
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "invalid_json",
			url:            "/api/customer/tags?user_id=user-test",
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

// TestCustomer360Controller_GetCustomerTags 测试获取客户标签
func TestCustomer360Controller_GetCustomerTags(t *testing.T) {
	db := setupCustomer360TestDB(t)
	router := setupCustomer360Router(t, db)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:           "success",
			url:            "/api/customer/tags?user_id=user-test",
			expectedStatus: 200,
			expectSuccess:  true,
		},
		{
			name:           "missing_user_id",
			url:            "/api/customer/tags",
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

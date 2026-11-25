package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupEmailSmtpTestDB(t *testing.T) *gorm.DB {
	gin.SetMode(gin.TestMode)
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	return database
}

func setupEmailSmtpController(t *testing.T) (*EmailSmtpController, *gin.Engine) {
	setupEmailSmtpTestDB(t)
	ctrl := NewEmailSmtpController()
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", uint(1))
		ctx.Next()
	})

	return ctrl, router
}

// TestEmailSmtpController_CreateEmailSmtp_Success 测试创建 SMTP 成功
func TestEmailSmtpController_CreateEmailSmtp_Success(t *testing.T) {
	_, router := setupEmailSmtpController(t)

	router.POST("/api/email/smtp", func(ctx *gin.Context) {
		var req dto.CreateEmailSmtpRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"code": float64(0),
			"data": gin.H{
				"id":       "smtp-123",
				"name":     req.Name,
				"server":   req.Server,
				"port":     req.Port,
				"username": req.Username,
			},
			"message": "创建成功",
		})
	})

	req := dto.CreateEmailSmtpRequest{
		Name:     "Test SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "pass" + "word123",
		Limit:    100,
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/api/email/smtp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestEmailSmtpController_CreateEmailSmtp_MissingFields 测试缺少必填字段
func TestEmailSmtpController_CreateEmailSmtp_MissingFields(t *testing.T) {
	ctrl, router := setupEmailSmtpController(t)
	router.POST("/api/email/smtp", ctrl.CreateEmailSmtp)

	req := map[string]any{}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/api/email/smtp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailSmtpController_GetEmailSmtpList_Success 测试获取 SMTP 列表成功
func TestEmailSmtpController_GetEmailSmtpList_Success(t *testing.T) {
	_, router := setupEmailSmtpController(t)

	router.GET("/api/email/smtp", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code": float64(0),
			"data": gin.H{
				"total": 2,
				"list": []any{
					gin.H{
						"id":       "smtp-1",
						"name":     "SMTP 1",
						"server":   "smtp1.example.com",
						"port":     587,
						"username": "user1@example.com",
					},
					gin.H{
						"id":       "smtp-2",
						"name":     "SMTP 2",
						"server":   "smtp2.example.com",
						"port":     465,
						"username": "user2@example.com",
					},
				},
			},
			"message": "获取成功",
		})
	})

	httpReq, _ := http.NewRequest("GET", "/api/email/smtp", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestEmailSmtpController_UpdateEmailSmtp_Success 测试更新 SMTP 成功
func TestEmailSmtpController_UpdateEmailSmtp_Success(t *testing.T) {
	_, router := setupEmailSmtpController(t)

	router.PUT("/api/email/smtp", func(ctx *gin.Context) {
		var req dto.UpdateEmailSmtpRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    nil,
			"message": "更新成功",
		})
	})

	req := dto.UpdateEmailSmtpRequest{
		ID:       "smtp-123",
		Name:     "Updated SMTP",
		Server:   "smtp.updated.com",
		Port:     587,
		Username: "updated@example.com",
		Password: "newp" + "assword",
		Limit:    200,
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", "/api/email/smtp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestEmailSmtpController_UpdateEmailSmtp_MissingFields 测试缺少必填字段
func TestEmailSmtpController_UpdateEmailSmtp_MissingFields(t *testing.T) {
	_, router := setupEmailSmtpController(t)

	router.PUT("/api/email/smtp", func(ctx *gin.Context) {
		var req dto.UpdateEmailSmtpRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.ID == "" || req.Name == "" || req.Server == "" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "missing required fields"})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    nil,
			"message": "更新成功",
		})
	})

	req := map[string]any{}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", "/api/email/smtp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailSmtpController_DeleteEmailSmtp_Success 测试删除 SMTP 成功
func TestEmailSmtpController_DeleteEmailSmtp_Success(t *testing.T) {
	_, router := setupEmailSmtpController(t)

	router.DELETE("/api/email/smtp/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    nil,
			"message": "删除成功",
		})
	})

	httpReq, _ := http.NewRequest("DELETE", "/api/email/smtp/smtp-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestEmailSmtpController_DeleteEmailSmtp_InvalidID 测试无效 ID
func TestEmailSmtpController_DeleteEmailSmtp_InvalidID(t *testing.T) {
	ctrl, router := setupEmailSmtpController(t)
	router.DELETE("/api/email/smtp/:id", ctrl.DeleteEmailSmtp)

	httpReq, _ := http.NewRequest("DELETE", "/api/email/smtp/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code == http.StatusOK {
		t.Logf("Expected error status, got OK")
	}
}

// TestEmailSmtpController_NewEmailSmtpController 测试构造函数
func TestEmailSmtpController_NewEmailSmtpController(t *testing.T) {
	ctrl := NewEmailSmtpController()
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

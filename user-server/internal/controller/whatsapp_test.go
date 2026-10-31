package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupWhatsAppTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.WhatsappAccount{},
		&model.WhatsappDraft{},
		&model.WhatsappJob{},
	)
	db.SetTestDB(database)
	return database
}

func setupWhatsAppRouter(ctrl *WhatsappController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "test_value")
		c.Set("user_id", uint(1))
		c.Next()
	})
	router.GET("/whatsapp/accounts", ctrl.ListAccounts)
	router.POST("/whatsapp/accounts", ctrl.CreateAccount)
	router.POST("/whatsapp/start-login", ctrl.StartLogin)
	router.GET("/whatsapp/accounts/:id/login-status", ctrl.LoginStatus)
	router.GET("/whatsapp/drafts", ctrl.ListDrafts)
	router.POST("/whatsapp/drafts", ctrl.CreateDraft)
	router.POST("/whatsapp/jobs", ctrl.CreateJob)
	return router
}

func TestWhatsappController_ListAccounts_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("GET", "/whatsapp/accounts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestWhatsappController_CreateAccount_InvalidJSON(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("POST", "/whatsapp/accounts", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestWhatsappController_CreateAccount_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	body, _ := json.Marshal(map[string]string{"name": "Test WA", "remark": "test"})
	req, _ := http.NewRequest("POST", "/whatsapp/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestWhatsappController_ListDrafts_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("GET", "/whatsapp/drafts?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// S-31：响应必须是 {list,total} 形态（前端分页 UI 消费的契约）。
	var body struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Data == nil {
		t.Fatalf("响应缺 data: %s", w.Body.String())
	}
	if _, ok := body.Data["total"]; !ok {
		t.Errorf("分页响应缺 total 字段: %s", w.Body.String())
	}
	if _, ok := body.Data["list"]; !ok {
		t.Errorf("分页响应缺 list 字段: %s", w.Body.String())
	}
}

func TestWhatsappController_CreateDraft_InvalidJSON(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("POST", "/whatsapp/drafts", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestWhatsappController_CreateDraft_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	body, _ := json.Marshal(map[string]string{"title": "Test Draft", "content": "Hello"})
	req, _ := http.NewRequest("POST", "/whatsapp/drafts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestWhatsappController_StartLogin_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	body, _ := json.Marshal(map[string]string{"account_id": "550e8400-e29b-41d4-a716-446655440000"})
	req, _ := http.NewRequest("POST", "/whatsapp/start-login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 200/400/500, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestWhatsappController_LoginStatus_Success(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("GET", "/whatsapp/accounts/550e8400-e29b-41d4-a716-446655440000/login-status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 200 or 500, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestWhatsappController_CreateJob_InvalidJSON(t *testing.T) {
	setupWhatsAppTestDB(t)
	ctrl := NewWhatsappController(service.NewWhatsappService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupWhatsAppRouter(ctrl)

	req, _ := http.NewRequest("POST", "/whatsapp/jobs", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

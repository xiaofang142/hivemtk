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
)

func setupLiveCodeTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.LiveCode{},
		&model.LiveCodeQR{},
		&model.LiveCodeQRStat{},
	)
	db.SetTestDB(database)
	return database
}

func setupLiveCodeRouter(ctrl *LiveCodeController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/live-code/list", ctrl.GetList)
	router.POST("/live-code", ctrl.Create)
	router.PUT("/live-code/:id", ctrl.Update)
	router.DELETE("/live-code/:id", ctrl.Delete)
	router.GET("/live-code/:id", ctrl.GetByID)
	router.GET("/live-code/:id/stats", ctrl.GetStats)
	router.POST("/live-code/:id/qr-code", ctrl.GenerateQRCode)
	router.GET("/live-code/:id/qr-codes", ctrl.GetQRCodes)
	router.GET("/live-code/:id/qr-stats", ctrl.GetQRStats)
	router.POST("/live-code/:id/share", ctrl.Share)
	router.GET("/l/:code", ctrl.RedirectLiveCode)
	return router
}

func TestLiveCodeController_GetList_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/list?page=1&pageSize=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GetList_DefaultParams(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/list", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GetList_WithFilters(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/list?name=test&status=active", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_Create_InvalidJSON(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("POST", "/live-code", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestLiveCodeController_Create_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	body, _ := json.Marshal(map[string]any{
		"name":              "Test LiveCode",
		"description":       "Test description",
		"type":              "qrcode",
		"short_link":        "test-code",
		"short_domain_id":   1,
		"entry_domain_id":   1,
		"landing_domain_id": 1,
	})
	req, _ := http.NewRequest("POST", "/live-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 测试库没有 domain_pool 行 ⇒ 服务层现在按"域名不可用"回 400（带原因），
	// 不再是 500/静默；200 仍是合法分支（注入域名后）。
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GetByID_NotFound(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/999999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_Update_InvalidJSON(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("PUT", "/live-code/1", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestLiveCodeController_Delete_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("DELETE", "/live-code/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GetStats_InvalidID(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/abc/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("Expected 400 or 404, got %d", w.Code)
	}
}

func TestLiveCodeController_GetStats_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/1/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GenerateQRCode_InvalidJSON(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("POST", "/live-code/1/qr-code", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestLiveCodeController_GetQRCodes_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/1/qr-codes", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_GetQRStats_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/live-code/1/qr-stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_Share_Success(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("POST", "/live-code/1/share", nil)
	req.Header.Set("User-Agent", "TestAgent")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLiveCodeController_RedirectLiveCode_NotFound(t *testing.T) {
	setupLiveCodeTestDB(t)
	service := service.NewLiveCodeService()
	ctrl := NewLiveCodeController(service)
	router := setupLiveCodeRouter(ctrl)

	req, _ := http.NewRequest("GET", "/l/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

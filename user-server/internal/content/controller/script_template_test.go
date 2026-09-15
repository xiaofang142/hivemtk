package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupScriptTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.ScriptTemplate{},
		&model.ScriptCategory{},
	)
	db.SetTestDB(database)
	return database
}

func setupScriptRouter(ctrl *ScriptTemplateController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "test_value")
		c.Set("user_id", uint(1))
		c.Next()
	})
	router.GET("/scripts", ctrl.GetTemplateList)
	router.GET("/scripts/:id", ctrl.GetTemplateByID)
	router.POST("/scripts", ctrl.CreateTemplate)
	router.PUT("/scripts/:id", ctrl.UpdateTemplate)
	router.DELETE("/scripts/:id", ctrl.DeleteTemplate)
	router.GET("/scripts/categories", ctrl.GetCategories)
	router.GET("/scripts/search", ctrl.SearchTemplates)
	router.GET("/scripts/public", ctrl.GetPublicTemplates)
	router.POST("/scripts/recommend", ctrl.RecommendScript)
	return router
}

func TestScriptTemplateController_GetTemplateList_NoAuth(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/scripts", ctrl.GetTemplateList)

	req, _ := http.NewRequest("GET", "/scripts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_GetTemplateList_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("GET", "/scripts?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != float64(0) && resp["code"] != float64(200) && resp["code"] != "200" {
		t.Errorf("Expected success code, got %v", resp["code"])
	}
}

func TestScriptTemplateController_CreateTemplate_NoAuth(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/scripts", ctrl.CreateTemplate)

	body, _ := json.Marshal(map[string]string{
		"category": "test",
		"title":    "test",
		"content":  "test content",
	})
	req, _ := http.NewRequest("POST", "/scripts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 (panic on nil user_id), got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_CreateTemplate_InvalidJSON(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("POST", "/scripts", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestScriptTemplateController_CreateTemplate_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	body, _ := json.Marshal(service.CreateScriptTemplateRequest{
		Title:    "Test Script",
		Content:  "Hello, how can I help you?",
		Category: "customer_service",
	})
	req, _ := http.NewRequest("POST", "/scripts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_GetTemplateByID_InvalidID(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("GET", "/scripts/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid ID, got %d", w.Code)
	}
}

func TestScriptTemplateController_UpdateTemplate_InvalidID(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	body, _ := json.Marshal(service.UpdateTemplateRequest{Title: "Updated"})
	req, _ := http.NewRequest("PUT", "/scripts/abc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid ID, got %d", w.Code)
	}
}

func TestScriptTemplateController_DeleteTemplate_InvalidID(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("DELETE", "/scripts/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid ID, got %d", w.Code)
	}
}

func TestScriptTemplateController_GetCategories_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("GET", "/scripts/categories", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_SearchTemplates_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("GET", "/scripts/search?keyword=test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_GetPublicTemplates_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("GET", "/scripts/public?page=1&page_size=5", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_RecommendScript_NoAuth(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/scripts/recommend", ctrl.RecommendScript)

	body, _ := json.Marshal(map[string]string{"message": "help"})
	req, _ := http.NewRequest("POST", "/scripts/recommend", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_RecommendScript_InvalidJSON(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	req, _ := http.NewRequest("POST", "/scripts/recommend", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestScriptTemplateController_RecommendScript_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	body, _ := json.Marshal(map[string]string{
		"session_id": "sess-123",
		"message":    "How do I reset my password?",
	})
	req, _ := http.NewRequest("POST", "/scripts/recommend", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScriptTemplateController_DeleteTemplate_Success(t *testing.T) {
	setupScriptTestDB(t)
	ctrl := NewScriptTemplateController()
	router := setupScriptRouter(ctrl)

	body, _ := json.Marshal(service.CreateScriptTemplateRequest{
		Title:    "To Delete",
		Content:  "Test content",
		Category: "test",
	})
	createReq, _ := http.NewRequest("POST", "/scripts", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusOK {
		t.Fatalf("Failed to create template: %d %s", createW.Code, createW.Body.String())
	}

	var createResp map[string]any
	json.Unmarshal(createW.Body.Bytes(), &createResp)
	data, _ := createResp["data"].(map[string]any)
	id := uint(data["id"].(float64))

	deleteReq, _ := http.NewRequest("DELETE", "/scripts/"+strconv.FormatUint(uint64(id), 10), nil)
	deleteW := httptest.NewRecorder()
	router.ServeHTTP(deleteW, deleteReq)

	if deleteW.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", deleteW.Code, deleteW.Body.String())
	}
}

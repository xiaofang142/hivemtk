package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupClueTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.Clue{},
	)
	db.SetTestDB(database)
	return database
}

// newTestClueController 用当前测试库构造控制器（显式注入，不依赖全局构造器）。
func newTestClueController() *ClueController {
	return NewClueController(service.NewClueService(repository.NewClueRepositoryWithDB(db.GetDB()), nil))
}

func setupClueController(t *testing.T) (*ClueController, *gin.Engine) {
	setupClueTestDB(t)
	ctrl := newTestClueController()
	router := gin.New()

	return ctrl, router
}

// TestClueController_GetClueList_Success 测试获取线索列表成功
func TestClueController_GetClueList_Success(t *testing.T) {
	database := setupClueTestDB(t)
	ctrl := newTestClueController()
	router := gin.New()

	clues := []*model.Clue{
		{SourceID: "src-1", Account: "acc1", Name: "线索 1", City: "北京", Type: 1, IsVerify: 0},
		{SourceID: "src-2", Account: "acc2", Name: "线索 2", City: "上海", Type: 2, IsVerify: 1},
	}
	for _, clue := range clues {
		database.Create(clue)
	}

	router.GET("/clues", ctrl.GetClueList)

	req, _ := http.NewRequest("GET", "/clues?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_GetClueList_EmptyList 测试空列表
func TestClueController_GetClueList_EmptyList(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.GET("/clues", ctrl.GetClueList)

	req, _ := http.NewRequest("GET", "/clues?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestClueController_GetClueList_InvalidParams 测试无效参数
func TestClueController_GetClueList_InvalidParams(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.GET("/clues", ctrl.GetClueList)

	req, _ := http.NewRequest("GET", "/clues", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (defaults applied), got %d", w.Code)
	}
}

// TestClueController_DeleteClue_Success 测试删除线索成功
func TestClueController_DeleteClue_Success(t *testing.T) {
	database := setupClueTestDB(t)
	ctrl := newTestClueController()
	router := gin.New()

	clue := &model.Clue{SourceID: "src-1", Account: "acc1", Name: "线索 1", City: "北京", Type: 1, IsVerify: 0}
	database.Create(clue)

	router.DELETE("/clues/:id", ctrl.DeleteClue)

	req, _ := http.NewRequest("DELETE", "/clues/"+clue.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_DeleteClue_InvalidID 测试无效 ID
func TestClueController_DeleteClue_InvalidID(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.DELETE("/clues/:id", ctrl.DeleteClue)

	req, _ := http.NewRequest("DELETE", "/clues/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("Expected status Bad Request or Not Found, got %d", w.Code)
	}
}

// TestClueController_DeleteClue_NotFound 测试线索不存在
func TestClueController_DeleteClue_NotFound(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.DELETE("/clues/:id", ctrl.DeleteClue)

	req, _ := http.NewRequest("DELETE", "/clues/non-existent-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 不存在的线索必须是 404：0 行 DELETE 回 200 等于「假成功」（A5/A6 收口）。
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing clue, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_GetClueStatistics_Success 测试获取线索统计成功
func TestClueController_GetClueStatistics_Success(t *testing.T) {
	database := setupClueTestDB(t)
	ctrl := newTestClueController()
	router := gin.New()

	clues := []*model.Clue{
		{SourceID: "src-1", Account: "acc1", Name: "线索 1", City: "北京", Type: 1, IsVerify: 0},
		{SourceID: "src-2", Account: "acc2", Name: "线索 2", City: "上海", Type: 2, IsVerify: 1},
		{SourceID: "src-3", Account: "acc3", Name: "线索 3", City: "广州", Type: 1, IsVerify: 1},
	}
	for _, clue := range clues {
		database.Create(clue)
	}

	router.GET("/clues/statistics", ctrl.GetClueStatistics)

	req, _ := http.NewRequest("GET", "/clues/statistics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_GetClueStatistics_EmptyList 测试空列表统计
func TestClueController_GetClueStatistics_EmptyList(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.GET("/clues/statistics", ctrl.GetClueStatistics)

	req, _ := http.NewRequest("GET", "/clues/statistics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d", w.Code)
	}
}

// TestClueController_ImportClues_Success 测试导入线索成功
func TestClueController_ImportClues_Success(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.POST("/clues/import", ctrl.ImportClues)

	importReq := []dto.ImportClueRequest{
		{Name: "线索 1", Account: "acc1", Type: "1", City: "北京", Address: "地址 1"},
		{Name: "线索 2", Account: "acc2", Type: "2", City: "上海", Address: "地址 2"},
	}
	body, _ := json.Marshal(importReq)

	req, _ := http.NewRequest("POST", "/clues/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_ImportClues_EmptyArray 空数组导入按参数错误拒绝。
//
// 原实现回 200 {success_count:0,skip_count:0}：接口看起来成功、实际什么都没做，
// 调用方无法分辨「文件解析出 0 行」与「导入成功」，故改为 400。
func TestClueController_ImportClues_EmptyArray(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.POST("/clues/import", ctrl.ImportClues)

	importReq := []dto.ImportClueRequest{}
	body, _ := json.Marshal(importReq)

	req, _ := http.NewRequest("POST", "/clues/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest, got %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "导入数据不能为空") {
		t.Errorf("Expected 空数组原因，body: %s", w.Body.String())
	}
}

// TestClueController_ImportClues_InvalidJSON 测试无效 JSON
func TestClueController_ImportClues_InvalidJSON(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.POST("/clues/import", ctrl.ImportClues)

	req, _ := http.NewRequest("POST", "/clues/import", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestClueController_ImportClues_InvalidType 测试无效的线索类型
func TestClueController_ImportClues_InvalidType(t *testing.T) {
	setupClueTestDB(t)
	ctrl, router := setupClueController(t)
	router.POST("/clues/import", ctrl.ImportClues)

	importReq := []dto.ImportClueRequest{
		{Name: "线索 1", Account: "acc1", Type: "invalid", City: "北京", Address: "地址 1"},
	}
	body, _ := json.Marshal(importReq)

	req, _ := http.NewRequest("POST", "/clues/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestClueController_NewClueController 测试构造函数
func TestClueController_NewClueController(t *testing.T) {
	ctrl := newTestClueController()
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

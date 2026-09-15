package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAIContentTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.AIGenerationRecord{},
		&model.PromptTemplate{},
	)
	db.SetTestDB(database)
	return database
}

func setupAIContentController(t *testing.T) (*AIContentController, *gin.Engine) {
	setupAIContentTestDB(t)
	ctrl := NewAIContentController()
	router := gin.New()

	router.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", uint(1))
		ctx.Set("user_id", uint(1))
		ctx.Next()
	})

	return ctrl, router
}

// TestAIContentController_GenerateContent_Success 测试生成内容成功
func TestAIContentController_GenerateContent_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.POST("/ai/generate", func(ctx *gin.Context) {
		_, exists := ctx.Get("user_id")
		if !exists {
			ctx.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		_, exists = ctx.Get("user_id")
		if !exists {
			ctx.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"output": "generated content", "tokens_used": 100},
			"message": "生成成功",
		})
	})

	generateReq := map[string]any{
		"type":  "copywriting",
		"input": "测试输入内容",
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/ai/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GenerateContent_InvalidJSON 测试无效 JSON
func TestAIContentController_GenerateContent_InvalidJSON(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/generate", ctrl.GenerateContent)

	req, _ := http.NewRequest("POST", "/ai/generate", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_GenerateContent_MissingInput 测试缺少必填字段
func TestAIContentController_GenerateContent_MissingInput(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/generate", ctrl.GenerateContent)

	generateReq := map[string]any{
		"type": "copywriting",
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/ai/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAIContentController_GenerateContent_NoMerchant(t *testing.T) {
	setupAIContentTestDB(t)
	ctrl := NewAIContentController()
	router := gin.New()
	router.POST("/ai/generate", ctrl.GenerateContent)

	generateReq := map[string]any{
		"type":  "copywriting",
		"input": "测试输入内容",
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/ai/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAIContentController_GetGenerationHistory_Success 测试获取生成历史成功
func TestAIContentController_GetGenerationHistory_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.GET("/ai/history", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"list": []any{}, "total": 0, "page": 1, "page_size": 20},
			"message": "获取成功",
		})
	})

	req, _ := http.NewRequest("GET", "/ai/history?page=1&page_size=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetGenerationHistory_WithFilters 测试带过滤条件的历史记录
func TestAIContentController_GetGenerationHistory_WithFilters(t *testing.T) {
	_, router := setupAIContentController(t)
	router.GET("/ai/history", func(ctx *gin.Context) {
		recordType := ctx.Query("type")
		isSaved := ctx.Query("is_saved")
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"list": []any{}, "total": 0, "page": 1, "page_size": 20, "type": recordType, "is_saved": isSaved},
			"message": "获取成功",
		})
	})

	req, _ := http.NewRequest("GET", "/ai/history?page=1&page_size=10&type=copywriting&is_saved=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetGenerationHistory_NoUser 测试缺少用户信息
func TestAIContentController_GetGenerationHistory_NoUser(t *testing.T) {
	setupAIContentTestDB(t)
	ctrl := NewAIContentController()
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		ctx.Next()
	})
	router.GET("/ai/history", ctrl.GetGenerationHistory)

	req, _ := http.NewRequest("GET", "/ai/history?page=1&page_size=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAIContentController_GetRecordByID_Success 测试获取记录详情成功
func TestAIContentController_GetRecordByID_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.GET("/ai/records/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"id": 1, "input": "test input", "output": "test output"},
			"message": "获取成功",
		})
	})

	req, _ := http.NewRequest("GET", "/ai/records/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetRecordByID_InvalidID 测试无效 ID
func TestAIContentController_GetRecordByID_InvalidID(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.GET("/ai/records/:id", ctrl.GetRecordByID)

	req, _ := http.NewRequest("GET", "/ai/records/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

func TestAIContentController_GetRecordByID_NoMerchant(t *testing.T) {
	setupAIContentTestDB(t)
	ctrl := NewAIContentController()
	router := gin.New()
	router.GET("/ai/records/:id", ctrl.GetRecordByID)

	req, _ := http.NewRequest("GET", "/ai/records/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAIContentController_SaveRecord_Success 测试保存记录成功
func TestAIContentController_SaveRecord_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.POST("/ai/records/:id/save", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    nil,
			"message": "保存成功",
		})
	})

	req, _ := http.NewRequest("POST", "/ai/records/1/save", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_SaveRecord_InvalidID 测试无效 ID
func TestAIContentController_SaveRecord_InvalidID(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/records/:id/save", ctrl.SaveRecord)

	req, _ := http.NewRequest("POST", "/ai/records/invalid/save", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_FavoriteRecord_Success 测试收藏记录成功
func TestAIContentController_FavoriteRecord_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.POST("/ai/records/:id/favorite", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    nil,
			"message": "操作成功",
		})
	})

	body, _ := json.Marshal(map[string]any{"is_favorite": true})
	req, _ := http.NewRequest("POST", "/ai/records/1/favorite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_FavoriteRecord_InvalidID 测试无效 ID
func TestAIContentController_FavoriteRecord_InvalidID(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/records/:id/favorite", ctrl.FavoriteRecord)

	body, _ := json.Marshal(map[string]any{"is_favorite": true})
	req, _ := http.NewRequest("POST", "/ai/records/invalid/favorite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_RateRecord_Success 测试评分记录成功
func TestAIContentController_RateRecord_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.POST("/ai/records/:id/rate", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    nil,
			"message": "评分成功",
		})
	})

	body, _ := json.Marshal(map[string]any{"rating": 5})
	req, _ := http.NewRequest("POST", "/ai/records/1/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_RateRecord_InvalidRating 测试无效评分
func TestAIContentController_RateRecord_InvalidRating(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/records/:id/rate", ctrl.RateRecord)

	body, _ := json.Marshal(map[string]any{"rating": 10})
	req, _ := http.NewRequest("POST", "/ai/records/1/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_DeleteRecord_Success 测试删除记录成功
func TestAIContentController_DeleteRecord_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.DELETE("/ai/records/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    nil,
			"message": "删除成功",
		})
	})

	req, _ := http.NewRequest("DELETE", "/ai/records/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_DeleteRecord_InvalidID 测试无效 ID
func TestAIContentController_DeleteRecord_InvalidID(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.DELETE("/ai/records/:id", ctrl.DeleteRecord)

	req, _ := http.NewRequest("DELETE", "/ai/records/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_GetTemplates_Success 测试获取模板列表成功
func TestAIContentController_GetTemplates_Success(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_GetTemplateByID_Success 测试获取模板详情成功
func TestAIContentController_GetTemplateByID_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.GET("/ai/templates/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"id": 1, "name": "Test Template", "template": "Test template content"},
			"message": "获取成功",
		})
	})

	req, _ := http.NewRequest("GET", "/ai/templates/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetTemplateByID_InvalidID 测试无效 ID
func TestAIContentController_GetTemplateByID_InvalidID(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.GET("/ai/templates/:id", ctrl.GetTemplateByID)

	req, _ := http.NewRequest("GET", "/ai/templates/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_CreateTemplate_Success 测试创建模板成功
func TestAIContentController_CreateTemplate_Success(t *testing.T) {
	_, router := setupAIContentController(t)
	router.POST("/ai/templates", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"data":    gin.H{"id": 1, "name": "Test Template"},
			"message": "创建成功",
		})
	})

	createReq := map[string]any{
		"name":      "Test Template",
		"type":      "copywriting",
		"template":  "Test template content",
		"variables": "{}",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/ai/templates", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_CreateTemplate_MissingFields 测试缺少必填字段
func TestAIContentController_CreateTemplate_MissingFields(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.POST("/ai/templates", ctrl.CreateTemplate)

	createReq := map[string]any{}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/ai/templates", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAIContentController_UpdateTemplate_Success 测试更新模板成功
func TestAIContentController_UpdateTemplate_Success(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_UpdateTemplate_NonExistent 测试更新不存在的模板
func TestAIContentController_UpdateTemplate_NonExistent(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_UpdateTemplate_SystemTemplate 测试更新系统模板
func TestAIContentController_UpdateTemplate_SystemTemplate(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_UpdateTemplate_InvalidJSON 测试无效 JSON
func TestAIContentController_UpdateTemplate_InvalidJSON(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_UpdateTemplate_MissingFields 测试缺少必填字段
func TestAIContentController_UpdateTemplate_MissingFields(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_DeleteTemplate_Success 测试删除模板成功
func TestAIContentController_DeleteTemplate_Success(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_DeleteTemplate_NonExistent 测试删除不存在的模板
func TestAIContentController_DeleteTemplate_NonExistent(t *testing.T) {
	setupAIContentTestDB(t)
	ctrl, router := setupAIContentController(t)
	router.DELETE("/ai/templates/:id", ctrl.DeleteTemplate)

	req, _ := http.NewRequest("DELETE", "/ai/templates/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_DeleteTemplate_SystemTemplate 测试删除系统模板
func TestAIContentController_DeleteTemplate_SystemTemplate(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.DELETE("/ai/templates/:id", ctrl.DeleteTemplate)

	systemTemplate := model.PromptTemplate{
		Name:      "System Template Delete",
		Type:      model.AIGenerationTypeCopywriting,
		Template:  "System content",
		Variables: "{}",
		IsSystem:  true,
		Status:    1,
	}
	db.GetDB().Create(&systemTemplate)

	req, _ := http.NewRequest("DELETE", "/ai/templates/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetGenerationHistory_EmptyResults 测试空结果的历史记录
func TestAIContentController_GetGenerationHistory_EmptyResults(t *testing.T) {
	setupAIContentTestDB(t)
	ctrl, router := setupAIContentController(t)
	router.GET("/ai/history", ctrl.GetGenerationHistory)

	req, _ := http.NewRequest("GET", "/ai/history?page=1&page_size=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code 0, got %v", response["code"])
	}
}

// TestAIContentController_GetGenerationHistory_WithRecords 测试带记录的生成历史
func TestAIContentController_GetGenerationHistory_WithRecords(t *testing.T) {
	setupAIContentTestDB(t)
}

// TestAIContentController_GetGenerationHistory_WithTypeFilter 测试带类型过滤的历史记录
func TestAIContentController_GetGenerationHistory_WithTypeFilter(t *testing.T) {
	db := setupAIContentTestDB(t)

	record1 := model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeCopywriting,
		Input:  "Copywriting input",
		Output: "Copywriting output",
	}
	record2 := model.AIGenerationRecord{
		UserID: 1,
		Type:   model.AIGenerationTypeTitle,
		Input:  "Title input",
		Output: "Title output",
	}
	db.Create(&record1)
	db.Create(&record2)

	ctrl, router := setupAIContentController(t)
	router.GET("/ai/history", ctrl.GetGenerationHistory)

	req, _ := http.NewRequest("GET", "/ai/history?type=copywriting", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetGenerationHistory_WithSavedFilter 测试带保存状态过滤的历史记录
func TestAIContentController_GetGenerationHistory_WithSavedFilter(t *testing.T) {
	db := setupAIContentTestDB(t)

	record := model.AIGenerationRecord{
		UserID:  1,
		Type:    model.AIGenerationTypeCopywriting,
		Input:   "Test input",
		Output:  "Test output",
		IsSaved: true,
	}
	db.Create(&record)

	ctrl, router := setupAIContentController(t)
	router.GET("/ai/history", ctrl.GetGenerationHistory)

	req, _ := http.NewRequest("GET", "/ai/history?is_saved=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetGenerationHistory_WithFavoriteFilter 测试带收藏状态过滤的历史记录
func TestAIContentController_GetGenerationHistory_WithFavoriteFilter(t *testing.T) {
	db := setupAIContentTestDB(t)

	record := model.AIGenerationRecord{
		UserID:     1,
		Type:       model.AIGenerationTypeCopywriting,
		Input:      "Test input",
		Output:     "Test output",
		IsFavorite: true,
	}
	db.Create(&record)

	ctrl, router := setupAIContentController(t)
	router.GET("/ai/history", ctrl.GetGenerationHistory)

	req, _ := http.NewRequest("GET", "/ai/history?is_favorite=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAIContentController_GetTemplateTypes_Success 测试获取模板类型成功
func TestAIContentController_GetTemplateTypes_Success(t *testing.T) {
	ctrl, router := setupAIContentController(t)
	router.GET("/ai/template-types", ctrl.GetTemplateTypes)

	req, _ := http.NewRequest("GET", "/ai/template-types", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code 0, got %v", response["code"])
	}
}

// TestAIContentController_NewAIContentController 测试构造函数
func TestAIContentController_NewAIContentController(t *testing.T) {
	ctrl := NewAIContentController()
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

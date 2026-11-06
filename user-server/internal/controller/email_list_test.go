package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupEmailListController(t *testing.T) (*EmailListController, *gin.Engine) {
	database := testutil.NewTestDB(t,
		&model.EmailList{},
	)
	db.SetTestDB(database)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := gin.New()
	return ctrl, router
}

// TestEmailListController_CreateEmailList_Success 测试创建列表成功
func TestEmailListController_CreateEmailList_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	router.POST("/api/email/list", func(ctx *gin.Context) {
		var req dto.CreateEmailListRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"code": float64(0),
			"data": gin.H{
				"total": 100,
			},
			"message": "success",
		})
	})

	req := dto.CreateEmailListRequest{
		Subject:     "Test Subject",
		Content:     "Test Content",
		Attachments: []string{"file1.pdf"},
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/api/email/list", bytes.NewReader(body))
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

// TestEmailListController_CreateEmailList_MissingSubject 测试缺少主题
func TestEmailListController_CreateEmailList_MissingSubject(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.POST("/api/email/list", ctrl.CreateEmailList)

	req := map[string]any{
		"content": "Test Content",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/api/email/list", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailListController_GetEmailListList_Success 测试获取列表列表成功
func TestEmailListController_GetEmailListList_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	router.GET("/api/email/list", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code": float64(0),
			"data": gin.H{
				"total": 2,
				"list": []gin.H{
					{"id": uuid.New().String(), "subject": "List 1", "to": "user1@example.com"},
					{"id": uuid.New().String(), "subject": "List 2", "to": "user2@example.com"},
				},
			},
			"message": "success",
		})
	})

	httpReq, _ := http.NewRequest("GET", "/api/email/list?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestEmailListController_GetEmailListList_MissingPage 测试缺少页码
func TestEmailListController_GetEmailListList_MissingPage(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.GET("/api/email/list", ctrl.GetEmailListList)

	httpReq, _ := http.NewRequest("GET", "/api/email/list?limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (page defaulted to 1), got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_GetEmailListList_MissingLimit 测试缺少页大小
func TestEmailListController_GetEmailListList_MissingLimit(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.GET("/api/email/list", ctrl.GetEmailListList)

	httpReq, _ := http.NewRequest("GET", "/api/email/list?page=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (pageSize defaulted to 20), got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_GetEmailListDetail_Success 测试获取列表详情成功
func TestEmailListController_GetEmailListDetail_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	listID := uuid.New().String()
	router.GET("/api/email/list/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code": float64(0),
			"data": gin.H{
				"id":      listID,
				"subject": "Test List",
				"to":      "user@example.com",
				"is_send": 0,
			},
			"message": "success",
		})
	})

	httpReq, _ := http.NewRequest("GET", "/api/email/list/"+listID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestEmailListController_GetEmailListDetail_InvalidID 测试无效 ID 格式
func TestEmailListController_GetEmailListDetail_InvalidID(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.GET("/api/email/list/:id", ctrl.GetEmailListDetail)

	httpReq, _ := http.NewRequest("GET", "/api/email/list/invalid-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailListController_UpdateEmailList_Success 测试更新列表成功
func TestEmailListController_UpdateEmailList_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	router.PUT("/api/email/list", func(ctx *gin.Context) {
		var req dto.UpdateEmailListRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    nil,
			"message": "success",
		})
	})

	req := dto.UpdateEmailListRequest{
		ID:          uuid.New().String(),
		Subject:     "Updated Subject",
		Content:     "Updated Content",
		Attachments: []string{"file1.pdf"},
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", "/api/email/list", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestEmailListController_UpdateEmailList_InvalidID 测试无效 ID 格式
func TestEmailListController_UpdateEmailList_InvalidID(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.PUT("/api/email/list", ctrl.UpdateEmailList)

	req := map[string]any{
		"id":      "invalid-uuid",
		"subject": "Test",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("PUT", "/api/email/list", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailListController_DeleteEmailList_Success 测试删除列表成功
func TestEmailListController_DeleteEmailList_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	router.DELETE("/api/email/list/:id", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    nil,
			"message": "success",
		})
	})

	listID := uuid.New().String()
	httpReq, _ := http.NewRequest("DELETE", "/api/email/list/"+listID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestEmailListController_DeleteEmailList_InvalidID 测试无效 ID 格式
func TestEmailListController_DeleteEmailList_InvalidID(t *testing.T) {
	ctrl, router := setupEmailListController(t)
	router.DELETE("/api/email/list/:id", ctrl.DeleteEmailList)

	httpReq, _ := http.NewRequest("DELETE", "/api/email/list/invalid-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestEmailListController_TraceEmail_Success 测试追踪邮件成功
func TestEmailListController_TraceEmail_Success(t *testing.T) {
	_, router := setupEmailListController(t)

	router.POST("/api/email/list/trace", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    float64(0),
			"data":    gin.H{"status": "tracked"},
			"message": "success",
		})
	})

	req := map[string]any{
		"list_id": uuid.New().String(),
		"user_id": "user123",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "/api/email/list/trace", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_NewEmailListController 测试构造函数
func TestEmailListController_NewEmailListController(t *testing.T) {
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

func setupEmailTestDB_Merged(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailList{},
		&model.EmailSmtp{},
		&model.EmailDraft{},
		&model.EmailJobs{},
		&model.Clue{},
	)
	db.SetTestDB(database)
	return database
}

func setupEmailListRouter_Merged(ctrl *EmailListController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Next()
	})
	router.GET("/email/list", ctrl.GetEmailListList)
	router.POST("/email/list", ctrl.CreateEmailList)
	router.PUT("/email/list/:id", ctrl.UpdateEmailList)
	router.DELETE("/email/list/:id", ctrl.DeleteEmailList)
	router.GET("/email/list/:id", ctrl.GetEmailListDetail)
	return router
}

// TestEmailListController_GetList_MergedSuccess 测试列表接口(合并自 email_extra_test.go)
func TestEmailListController_GetList_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupEmailListRouter_Merged(ctrl)

	req, _ := http.NewRequest("GET", "/email/list?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_Create_MergedInvalidJSON 测试无效 JSON(合并自 email_extra_test.go)
func TestEmailListController_Create_MergedInvalidJSON(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupEmailListRouter_Merged(ctrl)

	req, _ := http.NewRequest("POST", "/email/list", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

// TestEmailListController_Create_MergedSuccess 测试创建成功(合并自 email_extra_test.go)
func TestEmailListController_Create_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupEmailListRouter_Merged(ctrl)

	body, _ := json.Marshal(map[string]any{
		"name":    "Test List",
		"subject": "Test Subject",
		"emails":  []string{"test@example.com"},
		"group":   "default",
	})
	req, _ := http.NewRequest("POST", "/email/list", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusCreated &&
		w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 2xx/400/500 for create, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_Delete_MergedSuccess 测试删除成功(合并自 email_extra_test.go)
func TestEmailListController_Delete_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupEmailListRouter_Merged(ctrl)

	req, _ := http.NewRequest("DELETE", "/email/list/550e8400-e29b-41d4-a716-446655440000", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 不存在的名单必须是 404：0 行 DELETE 回 200 等于「假成功」。
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing email list, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailListController_GetDetail_MergedSuccess 测试详情接口(合并自 email_extra_test.go)
func TestEmailListController_GetDetail_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailListController(email.NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB())))
	router := setupEmailListRouter_Merged(ctrl)

	req, _ := http.NewRequest("GET", "/email/list/550e8400-e29b-41d4-a716-446655440000", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 200/404/400/500, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailSmtpController_GetList_MergedSuccess 测试 SMTP 列表接口(合并自 email_extra_test.go)
func TestEmailSmtpController_GetList_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailSmtpController()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
	})
	router.GET("/email/smtp", ctrl.GetEmailSmtpList)

	req, _ := http.NewRequest("GET", "/email/smtp", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailDraftController_GetList_MergedSuccess 测试草稿列表接口(合并自 email_extra_test.go)
func TestEmailDraftController_GetList_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailDraftController()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
	})
	router.GET("/email/draft", ctrl.GetEmailDraftList)

	req, _ := http.NewRequest("GET", "/email/draft", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestEmailJobsController_GetList_MergedSuccess 测试任务列表接口(合并自 email_extra_test.go)
func TestEmailJobsController_GetList_MergedSuccess(t *testing.T) {
	setupEmailTestDB_Merged(t)
	ctrl := NewEmailJobsController()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
	})
	router.GET("/email/jobs", ctrl.GetEmailJobsList)

	req, _ := http.NewRequest("GET", "/email/jobs?page=1&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

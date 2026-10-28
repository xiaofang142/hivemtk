package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupShortLinkControllerTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.ShortLink{},
		&model.DomainPool{},
		&model.ShortLinkAccess{},
	)
}

func setupGinEngineForShortLink() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// TestShortLinkController_Create_BasicSuccess 测试创建短链基本成功场景
func TestShortLinkController_Create_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com/very/long/url/path",
		Title:       "Example Page",
		Description: "This is an example page",
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_InvalidJSON 测试无效 JSON 请求
func TestShortLinkController_Create_InvalidJSON(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Create_EmptyShortCode 测试空短码
func TestShortLinkController_Create_EmptyShortCode(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "",
		OriginalURL: "https://www.example.com",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Create_EmptyOriginalURL 测试空原始链接
func TestShortLinkController_Create_EmptyOriginalURL(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Create_InvalidURL 测试无效 URL 格式
func TestShortLinkController_Create_InvalidURL(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "not-a-valid-url",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Create_DuplicateShortCode 测试重复短码
func TestShortLinkController_Create_DuplicateShortCode(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "duplicate",
		OriginalURL: "https://www.example.com/first",
		Title:       "First",
		DomainID:    1,
	})

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "duplicate",
		OriginalURL: "https://www.example.com/second",
		Title:       "Second",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Internal Server Error or Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Create_InvalidDomain 测试无效域名 ID
func TestShortLinkController_Create_InvalidDomain(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com",
		DomainID:    999,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Expected status Internal Server Error or Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_Create_WithPassword 测试创建带密码的短链
func TestShortLinkController_Create_WithPassword(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "protected",
		OriginalURL: "https://www.example.com",
		Title:       "Protected Link",
		Password:    "secret123",
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_WithExpireTime 测试创建带过期时间的短链
func TestShortLinkController_Create_WithExpireTime(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	expireTime := time.Now().Add(24 * time.Hour)
	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "expiring",
		OriginalURL: "https://www.example.com",
		Title:       "Expiring Link",
		DomainID:    1,
		ExpireTime:  &expireTime,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_WithoutDomain 测试创建不带域名的短链
func TestShortLinkController_Create_WithoutDomain(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "nodomain",
		OriginalURL: "https://www.example.com",
		Title:       "No Domain Link",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_LongURL 测试超长 URL
func TestShortLinkController_Create_LongURL(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	longURL := "https://www.example.com/" + string(bytes.Repeat([]byte("a"), 500))

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "longurl",
		OriginalURL: longURL,
		Title:       "Long URL Link",
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_SpecialCharacters 测试特殊字符
func TestShortLinkController_Create_SpecialCharacters(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "special-chars-test",
		OriginalURL: "https://www.example.com/path?query=value&other=123",
		Title:       "Special Characters Test",
		Description: "Testing special chars",
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_UnicodeCharacters 测试 Unicode 字符
func TestShortLinkController_Create_UnicodeCharacters(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "unicode-test",
		OriginalURL: "https://www.example.com",
		Title:       "Unicode 测试中文",
		Description: "Description with emoji",
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Create_VeryLongTitle 测试超长标题
func TestShortLinkController_Create_VeryLongTitle(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks", ctrl.Create)

	createReq := dto.CreateShortLinkRequest{
		ShortCode:   "long-title-test",
		OriginalURL: "https://www.example.com",
		Title:       string(bytes.Repeat([]byte("a"), 255)),
		DomainID:    1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/shortlinks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Update_BasicSuccess 测试更新短链基本成功
func TestShortLinkController_Update_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	link, _ := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com/original",
		Title:       "Original Title",
		DomainID:    1,
	})

	if link == nil {
		t.Fatalf("short link creation failed: 前置条件失败（svc.Create 返回 nil），需修复创建逻辑而非跳过测试")
		return
	}

	router.PUT("/shortlinks/:id", ctrl.Update)

	linkID := "1"
	if link.ID != 0 {
		linkID = fmt.Sprintf("%d", link.ID)
	}
	updateReq := dto.UpdateShortLinkRequest{
		ID:          link.ID,
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com/updated",
		Title:       "Updated Title",
		Description: "Updated description",
		Status:      1,
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/"+linkID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Update_InvalidJSON 测试无效 JSON
func TestShortLinkController_Update_InvalidJSON(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.PUT("/shortlinks/:id", ctrl.Update)

	req, _ := http.NewRequest("PUT", "/shortlinks/1", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Update_IDMismatch 测试 ID 不匹配
func TestShortLinkController_Update_IDMismatch(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.PUT("/shortlinks/:id", ctrl.Update)

	updateReq := dto.UpdateShortLinkRequest{
		ID:          999,
		ShortCode:   "abc",
		OriginalURL: "https://example.com",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Update_NotFound 测试短链不存在
func TestShortLinkController_Update_NotFound(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.PUT("/shortlinks/:id", ctrl.Update)

	updateReq := dto.UpdateShortLinkRequest{
		ID:          999,
		ShortCode:   "abc",
		OriginalURL: "https://example.com",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_Update_InvalidIDFormat 测试无效 ID 格式
func TestShortLinkController_Update_InvalidIDFormat(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.PUT("/shortlinks/:id", ctrl.Update)

	updateReq := dto.UpdateShortLinkRequest{
		ID:          1,
		ShortCode:   "abc",
		OriginalURL: "https://example.com",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Update_EmptyID 测试空 ID
func TestShortLinkController_Update_EmptyID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.PUT("/shortlinks/:id", ctrl.Update)

	updateReq := dto.UpdateShortLinkRequest{
		ID:          0,
		ShortCode:   "abc",
		OriginalURL: "https://example.com",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/0", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound && w.Code != http.StatusOK {
		t.Errorf("Expected 200/400/404/500, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Update_InvalidURL 测试无效 URL
func TestShortLinkController_Update_InvalidURL(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com",
		Title:       "Test",
		DomainID:    1,
	})

	router.PUT("/shortlinks/:id", ctrl.Update)

	updateReq := dto.UpdateShortLinkRequest{
		ID:          1,
		ShortCode:   "abc123",
		OriginalURL: "not-a-valid-url",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Update_DisabledStatus 测试更新为禁用状态
func TestShortLinkController_Update_DisabledStatus(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	link, _ := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "to-disable",
		OriginalURL: "https://www.example.com",
		Title:       "To Disable",
		DomainID:    1,
	})

	if link == nil {
		t.Fatalf("short link creation failed: 前置条件失败（svc.Create 返回 nil），需修复创建逻辑而非跳过测试")
		return
	}

	router.PUT("/shortlinks/:id", ctrl.Update)

	linkID := "1"
	if link.ID != 0 {
		linkID = fmt.Sprintf("%d", link.ID)
	}
	updateReq := dto.UpdateShortLinkRequest{
		ID:          link.ID,
		OriginalURL: "https://www.example.com",
		Status:      2,
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/shortlinks/"+linkID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Delete_BasicSuccess 测试删除短链基本成功
func TestShortLinkController_Delete_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	link, _ := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "to-delete",
		OriginalURL: "https://www.example.com",
		Title:       "To Delete",
		DomainID:    1,
	})

	if link == nil {
		t.Fatalf("short link creation failed: 前置条件失败（svc.Create 返回 nil），需修复创建逻辑而非跳过测试")
		return
	}

	router.DELETE("/shortlinks/:id", ctrl.Delete)

	linkID := "1"
	if link.ID != 0 {
		linkID = fmt.Sprintf("%d", link.ID)
	}
	req, _ := http.NewRequest("DELETE", "/shortlinks/"+linkID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_Delete_InvalidID 测试无效 ID
func TestShortLinkController_Delete_InvalidID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.DELETE("/shortlinks/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/shortlinks/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_Delete_NotFound 测试短链不存在
func TestShortLinkController_Delete_NotFound(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.DELETE("/shortlinks/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/shortlinks/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_Delete_ZeroID 测试 ID 为 0
func TestShortLinkController_Delete_ZeroID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.DELETE("/shortlinks/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/shortlinks/0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status Not Found or Internal Server Error, got %d", w.Code)
	}
}

// TestShortLinkController_Delete_NegativeID 测试负数 ID（字符串形式）
func TestShortLinkController_Delete_NegativeID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.DELETE("/shortlinks/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/shortlinks/-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_GetByID_BasicSuccess 测试根据 ID 获取短链基本成功
func TestShortLinkController_GetByID_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	link, _ := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://www.example.com",
		Title:       "Test Link",
		DomainID:    1,
	})

	if link == nil {
		t.Fatalf("short link creation failed: 前置条件失败（svc.Create 返回 nil），需修复创建逻辑而非跳过测试")
		return
	}

	router.GET("/shortlinks/:id", ctrl.GetByID)

	linkID := "1"
	if link.ID != 0 {
		linkID = fmt.Sprintf("%d", link.ID)
	}
	req, _ := http.NewRequest("GET", "/shortlinks/"+linkID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GetByID_NotFound 测试短链不存在
func TestShortLinkController_GetByID_NotFound(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/shortlinks/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_GetByID_InvalidID 测试无效 ID 格式
func TestShortLinkController_GetByID_InvalidID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/shortlinks/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_GetByID_ZeroID 测试 ID 为 0
func TestShortLinkController_GetByID_ZeroID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/shortlinks/0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Expected status Internal Server Error or Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_GetByID_WithAllFields 测试获取包含所有字段的短链
func TestShortLinkController_GetByID_WithAllFields(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	expireTime := time.Now().Add(24 * time.Hour)
	link, _ := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "full-link",
		OriginalURL: "https://www.example.com/full",
		Title:       "Full Link",
		Description: "A link with all fields",
		Password:    "secret",
		DomainID:    1,
		ExpireTime:  &expireTime,
	})

	if link == nil {
		t.Fatalf("short link creation failed: 前置条件失败（svc.Create 返回 nil），需修复创建逻辑而非跳过测试")
		return
	}

	router.GET("/shortlinks/:id", ctrl.GetByID)

	linkID := "1"
	if link.ID != 0 {
		linkID = fmt.Sprintf("%d", link.ID)
	}
	req, _ := http.NewRequest("GET", "/shortlinks/"+linkID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GetList_BasicSuccess 测试获取短链列表基本成功
func TestShortLinkController_GetList_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	for i := 1; i <= 5; i++ {
		svc.Create(context.Background(), &dto.CreateShortLinkRequest{
			ShortCode:   "code" + string(rune('0'+i)),
			OriginalURL: "https://www.example.com/" + string(rune('0'+i)),
			Title:       "Link " + string(rune('0'+i)),
			DomainID:    1,
		})
	}

	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?page=1&pageSize=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GetList_DefaultPagination 测试默认分页参数
func TestShortLinkController_GetList_DefaultPagination(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_WithShortCodeFilter 测试按短码过滤
func TestShortLinkController_GetList_WithShortCodeFilter(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "test123",
		OriginalURL: "https://www.example.com",
		Title:       "Test Link",
		DomainID:    1,
	})
	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "other456",
		OriginalURL: "https://www.other.com",
		Title:       "Other Link",
		DomainID:    1,
	})

	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?shortCode=test123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_WithStatusFilter 测试按状态过滤
func TestShortLinkController_GetList_WithStatusFilter(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "active1",
		OriginalURL: "https://www.example.com",
		Title:       "Active Link",
		DomainID:    1,
	})
	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "disabled1",
		OriginalURL: "https://www.example.com/disabled",
		Title:       "Disabled Link",
		DomainID:    1,
	})

	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?status=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_WithOriginalURLFilter 测试按原始 URL 过滤
func TestShortLinkController_GetList_WithOriginalURLFilter(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "url-filter",
		OriginalURL: "https://www.example.com/specific",
		Title:       "URL Filter Test",
		DomainID:    1,
	})

	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?original_url=https://www.example.com/specific", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_EmptyList 测试空列表
func TestShortLinkController_GetList_EmptyList(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?page=1&pageSize=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_LargePageSize 测试大页码
func TestShortLinkController_GetList_LargePageSize(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	for i := 1; i <= 100; i++ {
		svc.Create(context.Background(), &dto.CreateShortLinkRequest{
			ShortCode:   "large" + string(rune(i)),
			OriginalURL: "https://www.example.com/" + string(rune(i)),
			Title:       "Large List Link " + string(rune(i)),
			DomainID:    1,
		})
	}

	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?page=1&pageSize=100", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_NegativePage 测试负数页码
func TestShortLinkController_GetList_NegativePage(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?page=-1&pageSize=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_GetList_NegativePageSize 测试负数页大小
func TestShortLinkController_GetList_NegativePageSize(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.GET("/shortlinks", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/shortlinks?page=1&pageSize=-10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_BasicSuccess 测试访问短链基本成功
func TestShortLinkController_AccessShortLink_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "access-test",
		OriginalURL: "https://www.example.com/target",
		Title:       "Access Test",
		DomainID:    1,
	})

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "access-test",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_AccessShortLink_InvalidJSON 测试无效 JSON
func TestShortLinkController_AccessShortLink_InvalidJSON(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_NotFound 测试短链不存在
func TestShortLinkController_AccessShortLink_NotFound(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "non-existent",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status Not Found, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_EmptyShortCode 测试空短码
func TestShortLinkController_AccessShortLink_EmptyShortCode(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_WithPassword 测试带密码访问
func TestShortLinkController_AccessShortLink_WithPassword(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "protected-access",
		OriginalURL: "https://www.example.com/protected",
		Title:       "Protected Access Test",
		Password:    "correctpassword",
		DomainID:    1,
	})

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "protected-access",
		Password:  "correctpassword",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_AccessShortLink_WrongPassword 测试错误密码
func TestShortLinkController_AccessShortLink_WrongPassword(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "wrong-pass-test",
		OriginalURL: "https://www.example.com/protected",
		Title:       "Wrong Password Test",
		Password:    "correctpassword",
		DomainID:    1,
	})

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "wrong-pass-test",
		Password:  "wrongpassword",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("Expected status Unauthorized or Forbidden, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_WithMetadata 测试带元数据访问
func TestShortLinkController_AccessShortLink_WithMetadata(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "metadata-test",
		OriginalURL: "https://www.example.com/metadata",
		Title:       "Metadata Test",
		DomainID:    1,
	})

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "metadata-test",
		UserAgent: "Mozilla/5.0 Test Browser",
		IP:        "192.168.1.1",
		Referer:   "https://www.google.com",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_AccessShortLink_DisabledLink 测试访问禁用的短链
func TestShortLinkController_AccessShortLink_DisabledLink(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	shortLink := &model.ShortLink{
		ShortCode:   "disabled-access",
		OriginalURL: "https://www.example.com/disabled",
		Title:       "Disabled Access Test",
		DomainID:    1,
		Status:      2,
	}
	database.Create(shortLink)

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "disabled-access",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("Expected status Unauthorized or Forbidden, got %d", w.Code)
	}
}

// TestShortLinkController_AccessShortLink_ExpiredLink 测试访问过期的短链
func TestShortLinkController_AccessShortLink_ExpiredLink(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{
		Domain:  "example.com",
		Port:    80,
		Purpose: "test",
		Status:  1,
	})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()

	expiredTime := time.Now().Add(-24 * time.Hour)
	svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode:   "expired-access",
		OriginalURL: "https://www.example.com/expired",
		Title:       "Expired Access Test",
		DomainID:    1,
		ExpireTime:  &expiredTime,
	})

	router.POST("/shortlinks/access", ctrl.AccessShortLink)

	accessReq := dto.AccessShortLinkRequest{
		ShortCode: "expired-access",
	}
	body, _ := json.Marshal(accessReq)

	req, _ := http.NewRequest("POST", "/shortlinks/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("Expected status Unauthorized or Forbidden, got %d", w.Code)
	}
}

// TestShortLinkController_GenerateShortCode_BasicSuccess 测试生成短码基本成功
func TestShortLinkController_GenerateShortCode_BasicSuccess(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 6,
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GenerateShortCode_InvalidJSON 测试无效 JSON
func TestShortLinkController_GenerateShortCode_InvalidJSON(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_GenerateShortCode_MinLength 测试最小长度
func TestShortLinkController_GenerateShortCode_MinLength(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 4,
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GenerateShortCode_MaxLength 测试最大长度
func TestShortLinkController_GenerateShortCode_MaxLength(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 10,
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestShortLinkController_GenerateShortCode_TooShortLength 测试长度太短
func TestShortLinkController_GenerateShortCode_TooShortLength(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 2,
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_GenerateShortCode_TooLongLength 测试长度太长
func TestShortLinkController_GenerateShortCode_TooLongLength(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 15,
	}
	body, _ := json.Marshal(generateReq)

	req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestShortLinkController_GenerateShortCode_MultipleGenerations 测试多次生成唯一性
func TestShortLinkController_GenerateShortCode_MultipleGenerations(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	ctrl := NewShortLinkController(service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)))
	router := setupGinEngineForShortLink()
	router.POST("/shortlinks/generate-code", ctrl.GenerateShortCode)

	generateReq := dto.GenerateShortCodeRequest{
		Length: 8,
	}
	body, _ := json.Marshal(generateReq)

	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("POST", "/shortlinks/generate-code", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
		}
	}
}

// 生产路由 PUT /shortlink/update 的 id 在 JSON body 里，不在 path。
// 历史用例只覆盖 /shortlinks/:id，导致线上更新恒 400「无效的ID格式」而测试全绿。
func TestShortLinkController_Update_ProductionRouteUsesBodyID(t *testing.T) {
	database := setupShortLinkControllerTestDB(t)
	database.Create(&model.DomainPool{Domain: "example.com", Port: 80, Purpose: "test", Status: 1})
	svc := service.NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database))
	ctrl := NewShortLinkController(svc)
	router := setupGinEngineForShortLink()
	router.PUT("/shortlink/update", ctrl.Update)

	link, err := svc.Create(context.Background(), &dto.CreateShortLinkRequest{
		ShortCode: "prod01", OriginalURL: "https://www.example.com/original", Title: "Original", DomainID: 1,
	})
	if err != nil || link == nil {
		t.Fatalf("前置创建失败: %v", err)
	}
	body, _ := json.Marshal(dto.UpdateShortLinkRequest{
		ID: link.ID, ShortCode: "prod01", OriginalURL: "https://www.example.com/updated", Title: "Updated", Status: 1,
	})
	req, _ := http.NewRequest("PUT", "/shortlink/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("生产路由应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	got, _ := svc.GetByID(context.Background(), link.ID)
	if got == nil || got.Title != "Updated" || got.OriginalURL != "https://www.example.com/updated" {
		t.Fatalf("更新未落库: %+v", got)
	}
}

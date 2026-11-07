package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupXianyuCardTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.XianyuCard{},
		&model.XianyuCardActivity{},
		&model.ShortLink{},
		&model.DomainPool{},
	)
	db.SetTestDB(database)
	return database
}

// TestXianyuCardController_Create_Success 测试创建闲鱼卡片成功
func TestXianyuCardController_Create_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:        "Test Card",
		Description:  "This is a test card",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xianyu.com",
		DomainPoolID: 1,
		Tags:         "test,card",
		IsActive:     boolPtr(true),
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Create_InvalidJSON 测试无效 JSON 请求
func TestXianyuCardController_Create_InvalidJSON(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_Create_EmptyTitle 测试空标题
func TestXianyuCardController_Create_EmptyTitle(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:       "",
		Description: "This is a test card",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_Create_InvalidImageURL 测试无效图片 URL
func TestXianyuCardController_Create_InvalidImageURL(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:       "Test Card",
		Description: "This is a test card",
		ImageURL:    "invalid-url",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_Create_DefaultRedirectURL 测试默认跳转链接
func TestXianyuCardController_Create_DefaultRedirectURL(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:       "Test Card",
		Description: "This is a test card",
		ImageURL:    "https://example.com/image.jpg",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Update_Success 测试更新卡片成功
func TestXianyuCardController_Update_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	card, err := svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:        "Original Card",
		Description:  "Original description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create card failed: %v", err)
	}
	cardID := "1"
	if card != nil && card.ID != 0 {
		cardID = fmt.Sprintf("%d", card.ID)
	}

	router.PUT("/xianyu/cards/:id", ctrl.Update)

	updateReq := dto.XianyuCardUpdateRequest{
		ID:           card.ID,
		Title:        "Updated Card",
		Description:  "Updated description",
		ImageURL:     "https://example.com/new-image.jpg",
		RedirectURL:  "https://www.xianyu.com/new",
		DomainPoolID: 1,
		Tags:         "updated,card",
		ViewCount:    100,
		IsActive:     boolPtr(true),
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/xianyu/cards/"+cardID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Update_InvalidJSON 测试无效 JSON
func TestXianyuCardController_Update_InvalidJSON(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.PUT("/xianyu/cards/:id", ctrl.Update)

	req, _ := http.NewRequest("PUT", "/xianyu/cards/1", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_Update_IDMismatch 测试 ID 不匹配
func TestXianyuCardController_Update_IDMismatch(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.PUT("/xianyu/cards/:id", ctrl.Update)

	updateReq := dto.XianyuCardUpdateRequest{
		ID:    999,
		Title: "Test",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/xianyu/cards/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_Delete_Success 测试删除卡片成功
func TestXianyuCardController_Delete_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:        "Card to Delete",
		Description:  "This card will be deleted",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	})

	router.DELETE("/xianyu/cards/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/xianyu/cards/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Delete_InvalidID 测试无效 ID
func TestXianyuCardController_Delete_InvalidID(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.DELETE("/xianyu/cards/:id", ctrl.Delete)

	req, _ := http.NewRequest("DELETE", "/xianyu/cards/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_GetByID_Success 测试根据 ID 获取卡片成功
func TestXianyuCardController_GetByID_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	})

	router.GET("/xianyu/cards/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/xianyu/cards/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_GetByID_NotFound 测试卡片不存在
func TestXianyuCardController_GetByID_NotFound(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.GET("/xianyu/cards/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/xianyu/cards/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Expected status Internal Server Error or Not Found, got %d", w.Code)
	}
}

// TestXianyuCardController_GetByID_InvalidID 测试无效 ID 格式
func TestXianyuCardController_GetByID_InvalidID(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.GET("/xianyu/cards/:id", ctrl.GetByID)

	req, _ := http.NewRequest("GET", "/xianyu/cards/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_GetList_Success 测试获取卡片列表成功
func TestXianyuCardController_GetList_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	for i := 1; i <= 5; i++ {
		svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
			Title:        "Card " + string(rune('0'+i)),
			Description:  "Description " + string(rune('0'+i)),
			ImageURL:     "https://example.com/image" + string(rune('0'+i)) + ".jpg",
			DomainPoolID: 1,
			IsActive:     boolPtr(true),
		})
	}

	router.GET("/xianyu/cards", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/xianyu/cards?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_GetList_DefaultPagination 测试默认分页
func TestXianyuCardController_GetList_DefaultPagination(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:        "Card",
		Description:  "Description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	})

	router.GET("/xianyu/cards", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/xianyu/cards?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

// TestXianyuCardController_GetList_InvalidPage 测试无效分页参数
func TestXianyuCardController_GetList_InvalidPage(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.GET("/xianyu/cards", ctrl.GetList)

	req, _ := http.NewRequest("GET", "/xianyu/cards?page=invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_GenerateShortLink_Success 测试生成短链成功
func TestXianyuCardController_GenerateShortLink_Success(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	// D-44：没有 redirect_url 时 GenerateShortLink 会拒绝生成（避免删旧链后建空链），
	// 本用例验证「有跳转地址」的成功路径，因此显式给 redirect_url。
	svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:       "Card with Short Link",
		Description: "This card will have a short link",
		ImageURL:    "https://example.com/image.jpg",
		RedirectURL: "https://example.com/target",
		IsActive:    boolPtr(true),
	})

	router.POST("/xianyu/cards/:id/shortlink", ctrl.GenerateShortLink)

	req, _ := http.NewRequest("POST", "/xianyu/cards/1/shortlink", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_GenerateShortLink_RequiresRedirectURL 验证 D-44：
// 卡片没配 redirect_url 时必须 400，而不是「删掉旧短链 + 200 空 code」。
func TestXianyuCardController_GenerateShortLink_RequiresRedirectURL(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	if _, err := svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:    "Card without redirect",
		ImageURL: "https://example.com/image.jpg",
		IsActive: boolPtr(true),
	}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	router.POST("/xianyu/cards/:id/shortlink", ctrl.GenerateShortLink)

	req, _ := http.NewRequest("POST", "/xianyu/cards/1/shortlink", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 redirect_url 时应 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "redirect_url") {
		t.Errorf("错误文案应指出 redirect_url，实际: %s", w.Body.String())
	}
}

// TestXianyuCardController_GenerateShortLink_InvalidID 测试无效 ID
func TestXianyuCardController_GenerateShortLink_InvalidID(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards/:id/shortlink", ctrl.GenerateShortLink)

	req, _ := http.NewRequest("POST", "/xianyu/cards/invalid/shortlink", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestXianyuCardController_GenerateShortLink_NotFound 测试卡片不存在
func TestXianyuCardController_GenerateShortLink_NotFound(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards/:id/shortlink", ctrl.GenerateShortLink)

	req, _ := http.NewRequest("POST", "/xianyu/cards/999/shortlink", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Expected status Internal Server Error or Not Found, got %d", w.Code)
	}
}

// TestXianyuCardController_Create_WithTags 测试创建带标签的卡片
func TestXianyuCardController_Create_WithTags(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:        "Card with Tags",
		Description:  "This card has tags",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		Tags:         "tag1,tag2,tag3",
		IsActive:     boolPtr(true),
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Create_InactiveCard 测试创建非激活卡片
func TestXianyuCardController_Create_InactiveCard(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	ctrl := NewXianyuCardController(service.NewXianyuCardService(database), service.NewXianyuCardStatsService(database))
	router := setupGinEngine()
	router.POST("/xianyu/cards", ctrl.Create)

	createReq := dto.XianyuCardCreateRequest{
		Title:        "Inactive Card",
		Description:  "This card is inactive",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(false),
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/xianyu/cards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestXianyuCardController_Update_ViewCount 测试更新浏览次数
func TestXianyuCardController_Update_ViewCount(t *testing.T) {
	database := setupXianyuCardTestDB(t)
	svc := service.NewXianyuCardService(database)
	ctrl := NewXianyuCardController(svc, service.NewXianyuCardStatsService(database))
	router := setupGinEngine()

	svc.Create(context.Background(), &dto.XianyuCardCreateRequest{
		Title:        "Card",
		Description:  "Description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	})

	router.PUT("/xianyu/cards/:id", ctrl.Update)

	updateReq := dto.XianyuCardUpdateRequest{
		ID:          1,
		Title:       "Card",
		Description: "Description",
		ImageURL:    "https://example.com/image.jpg",
		ViewCount:   1000,
		IsActive:    boolPtr(true),
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/xianyu/cards/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

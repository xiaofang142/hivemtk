package cards

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"

	"hivemtk-user/internal/repository"
)

func setupXiaohongshuCardServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.XiaohongshuCard{},
		&model.ShortLink{},
		&model.DomainPool{},
		&model.ShortLinkAccess{},
		&model.XiaohongshuCardActivity{},
	)
	db.SetTestDB(database)
	if err := database.Create(&model.DomainPool{
		ID:      1,
		Domain:  "example.com",
		Port:    80,
		Purpose: "测试域名",
		Status:  1,
	}).Error; err != nil {
		t.Fatalf("预创建 DomainPool 记录失败: %v", err)
	}
	return database
}

// TestNewXiaohongshuCardService 测试创建小红书卡片服务
func TestNewXiaohongshuCardService(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestXiaohongshuCardService_Create_Success 测试创建小红书卡片成功
func TestXiaohongshuCardService_Create_Success(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	req := &dto.XiaohongshuCardCreateRequest{
		Title:        "Test Card",
		Description:  "This is a test card",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xiaohongshu.com",
		DomainPoolID: uintPtr(1),
		Tags:         "test,card",
		IsActive:     boolPtr(true),
	}

	card, err := service.Create(context.Background(), req)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if card.ID == 0 {
		t.Error("Expected non-zero ID")
	}
	if card.Title != req.Title {
		t.Errorf("Expected title %s, got %s", req.Title, card.Title)
	}
	if card.Description != req.Description {
		t.Errorf("Expected description %s, got %s", req.Description, card.Description)
	}
}

// TestXiaohongshuCardService_Create_EmptyTitle 测试创建空标题卡片
func TestXiaohongshuCardService_Create_EmptyTitle(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	req := &dto.XiaohongshuCardCreateRequest{
		Title:       "",
		Description: "Test description",
		ImageURL:    "https://example.com/image.jpg",
	}

	_, err := service.Create(context.Background(), req)

	if err != nil {
		t.Logf("Create with empty title failed (expected): %v", err)
	}
}

// TestXiaohongshuCardService_Create_EmptyRedirectURL 测试创建无跳转链接卡片
func TestXiaohongshuCardService_Create_EmptyRedirectURL(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	req := &dto.XiaohongshuCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: uintPtr(1),
	}

	card, err := service.Create(context.Background(), req)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if card.ID == 0 {
		t.Error("Expected non-zero ID")
	}
}

// TestXiaohongshuCardService_Update_Success 测试更新小红书卡片成功
func TestXiaohongshuCardService_Update_Success(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	createReq := &dto.XiaohongshuCardCreateRequest{
		Title:        "Original Card",
		Description:  "Original description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xiaohongshu.com",
		DomainPoolID: uintPtr(1),
		Tags:         "original",
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.XiaohongshuCardUpdateRequest{
		ID:          createdCard.ID,
		Title:       "Updated Card",
		Description: "Updated description",
		ImageURL:    "https://example.com/new-image.jpg",
		RedirectURL: "https://www.xiaohongshu.com/new",
		Tags:        "updated,card",
		IsActive:    boolPtr(true),
	}

	updatedCard, err := service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updatedCard.Title != updateReq.Title {
		t.Errorf("Expected title %s, got %s", updateReq.Title, updatedCard.Title)
	}
	if updatedCard.Description != updateReq.Description {
		t.Errorf("Expected description %s, got %s", updateReq.Description, updatedCard.Description)
	}
}

// TestXiaohongshuCardService_Update_NotFound 测试更新不存在的卡片
func TestXiaohongshuCardService_Update_NotFound(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	updateReq := &dto.XiaohongshuCardUpdateRequest{
		ID:    999,
		Title: "Non-existent Card",
	}

	_, err := service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for updating non-existent card")
	}
}

// TestXiaohongshuCardService_Delete_Success 测试删除小红书卡片成功
func TestXiaohongshuCardService_Delete_Success(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	createReq := &dto.XiaohongshuCardCreateRequest{
		Title:        "Card to Delete",
		Description:  "This card will be deleted",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: uintPtr(1),
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = service.Delete(context.Background(), createdCard.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = service.GetByID(context.Background(), createdCard.ID)
	if err == nil {
		t.Error("Expected error when getting deleted card")
	}
}

// TestXiaohongshuCardService_Delete_NotFound 测试删除不存在的卡片
func TestXiaohongshuCardService_Delete_NotFound(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	err := service.Delete(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for deleting non-existent card")
	}
}

// TestXiaohongshuCardService_GetByID_Success 测试根据 ID 获取卡片成功
func TestXiaohongshuCardService_GetByID_Success(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	createReq := &dto.XiaohongshuCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xiaohongshu.com",
		DomainPoolID: uintPtr(1),
		Tags:         "test",
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetchedCard, err := service.GetByID(context.Background(), createdCard.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if fetchedCard.ID != createdCard.ID {
		t.Errorf("Expected ID %d, got %d", createdCard.ID, fetchedCard.ID)
	}
	if fetchedCard.Title != createdCard.Title {
		t.Errorf("Expected title %s, got %s", createdCard.Title, fetchedCard.Title)
	}
}

// TestXiaohongshuCardService_GetByID_NotFound 测试获取不存在的卡片
func TestXiaohongshuCardService_GetByID_NotFound(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	_, err := service.GetByID(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for getting non-existent card")
	}
}

// TestXiaohongshuCardService_GetList_Success 测试获取卡片列表成功
func TestXiaohongshuCardService_GetList_Success(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	for i := 1; i <= 5; i++ {
		createReq := &dto.XiaohongshuCardCreateRequest{
			Title:        "Card " + string(rune('0'+i)),
			Description:  "Description " + string(rune('0'+i)),
			ImageURL:     "https://example.com/image" + string(rune('0'+i)) + ".jpg",
			DomainPoolID: uintPtr(1),
			IsActive:     boolPtr(true),
		}
		_, err := service.Create(context.Background(), createReq)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq := &dto.XiaohongshuCardListRequest{
		Page:     1,
		PageSize: 10,
	}
	listResp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if listResp.Total != 5 {
		t.Errorf("Expected total 5, got %d", listResp.Total)
	}
	if len(listResp.List) != 5 {
		t.Errorf("Expected 5 cards, got %d", len(listResp.List))
	}
}

// TestXiaohongshuCardService_GetList_Pagination 测试分页获取卡片列表
func TestXiaohongshuCardService_GetList_Pagination(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	for i := 1; i <= 15; i++ {
		createReq := &dto.XiaohongshuCardCreateRequest{
			Title:        "Card " + string(rune('0'+i)),
			Description:  "Description " + string(rune('0'+i)),
			ImageURL:     "https://example.com/image" + string(rune('0'+i)) + ".jpg",
			DomainPoolID: uintPtr(1),
			IsActive:     boolPtr(true),
		}
		_, err := service.Create(context.Background(), createReq)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq := &dto.XiaohongshuCardListRequest{
		Page:     1,
		PageSize: 10,
	}
	listResp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}

	if len(listResp.List) != 10 {
		t.Errorf("Expected 10 cards on page 1, got %d", len(listResp.List))
	}

	listReq.Page = 2
	listResp2, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}

	if len(listResp2.List) != 5 {
		t.Errorf("Expected 5 cards on page 2, got %d", len(listResp2.List))
	}
}

// TestXiaohongshuCardService_GetList_EmptyList 测试获取空列表
func TestXiaohongshuCardService_GetList_EmptyList(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	listReq := &dto.XiaohongshuCardListRequest{
		Page:     1,
		PageSize: 10,
	}
	listResp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if listResp.Total != 0 {
		t.Errorf("Expected total 0, got %d", listResp.Total)
	}
	if len(listResp.List) != 0 {
		t.Errorf("Expected 0 cards, got %d", len(listResp.List))
	}
}

// TestXiaohongshuCardService_Create_WithTags 测试创建带标签的卡片
func TestXiaohongshuCardService_Create_WithTags(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	req := &dto.XiaohongshuCardCreateRequest{
		Title:        "Card with Tags",
		Description:  "This card has tags",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: uintPtr(1),
		Tags:         "tag1,tag2,tag3",
		IsActive:     boolPtr(true),
	}

	card, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if card.Tags != req.Tags {
		t.Errorf("Expected tags %s, got %s", req.Tags, card.Tags)
	}
}

// TestXiaohongshuCardService_Create_InactiveCard 测试创建非激活卡片
func TestXiaohongshuCardService_Create_InactiveCard(t *testing.T) {
	database := setupXiaohongshuCardServiceTestDB(t)
	service := NewXiaohongshuCardService(repository.NewXiaohongshuCardRepository(database), NewShortLinkService(repository.NewShortLinkRepository(database), repository.NewDomainPoolRepository(database), repository.NewShortLinkAccessRepository(database)), newRepoDomainPoolResolver(repository.NewDomainPoolRepository(database)))

	req := &dto.XiaohongshuCardCreateRequest{
		Title:        "Inactive Card",
		Description:  "This card is inactive",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: uintPtr(1),
		IsActive:     boolPtr(false),
	}

	card, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if card.ID == 0 {
		t.Error("Expected non-zero ID")
	}
}

func uintPtr(i uint) *uint {
	return &i
}

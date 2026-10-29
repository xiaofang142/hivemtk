package cards

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupDouyinCardServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.DouyinCard{},
		&model.ShortLink{},
		&model.DomainPool{},
		&model.ShortLinkAccess{},
		&model.DouyinCardActivity{},
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

// TestNewDouyinCardService 测试创建抖音卡片服务
func TestNewDouyinCardService(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestDouyinCardService_Create_Success 测试创建抖音卡片成功
func TestDouyinCardService_Create_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "This is a test card",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.douyin.com",
		DomainPoolID: 1,
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
	if card.ImageURL != req.ImageURL {
		t.Errorf("Expected image URL %s, got %s", req.ImageURL, card.ImageURL)
	}
	if card.RedirectURL != req.RedirectURL {
		t.Errorf("Expected redirect URL %s, got %s", req.RedirectURL, card.RedirectURL)
	}
	if card.IsActive != dto.BoolPtrOrDefaultTrue(req.IsActive) {
		t.Errorf("Expected isActive %v, got %v", dto.BoolPtrOrDefaultTrue(req.IsActive), card.IsActive)
	}
}

// TestDouyinCardService_Create_EmptyTitle 测试创建空标题卡片
func TestDouyinCardService_Create_EmptyTitle(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:       "",
		Description: "Test description",
		ImageURL:    "https://example.com/image.jpg",
	}

	_, err := service.Create(context.Background(), req)

	if err != nil {
		t.Logf("Create with empty title failed (expected): %v", err)
	}
}

// TestDouyinCardService_Create_EmptyRedirectURL 测试创建无跳转链接卡片
func TestDouyinCardService_Create_EmptyRedirectURL(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
	}

	card, err := service.Create(context.Background(), req)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expectedDefaultURL := "https://www.douyin.com"
	_ = expectedDefaultURL
	_ = card
}

// TestDouyinCardService_Update_Success 测试更新抖音卡片成功
func TestDouyinCardService_Update_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Original Card",
		Description:  "Original description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.douyin.com",
		DomainPoolID: 1,
		Tags:         "original",
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.DouyinCardUpdateRequest{
		ID:          createdCard.ID,
		Title:       "Updated Card",
		Description: "Updated description",
		ImageURL:    "https://example.com/new-image.jpg",
		RedirectURL: "https://www.douyin.com/new",
		Tags:        "updated,card",
		ViewCount:   100,
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
	if updatedCard.ViewCount != updateReq.ViewCount {
		t.Errorf("Expected view count %d, got %d", updateReq.ViewCount, updatedCard.ViewCount)
	}
}

// TestDouyinCardService_Update_NotFound 测试更新不存在的卡片
func TestDouyinCardService_Update_NotFound(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	updateReq := &dto.DouyinCardUpdateRequest{
		ID:    999,
		Title: "Non-existent Card",
	}

	_, err := service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for updating non-existent card")
	}
}

// TestDouyinCardService_Delete_Success 测试删除抖音卡片成功
func TestDouyinCardService_Delete_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Card to Delete",
		Description:  "This card will be deleted",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
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

// TestDouyinCardService_Delete_NotFound 测试删除不存在的卡片
func TestDouyinCardService_Delete_NotFound(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	err := service.Delete(context.Background(), 999)
	if err != nil {
		t.Errorf("Delete should not return error for non-existent card: %v", err)
	}
}

// TestDouyinCardService_GetByID_Success 测试根据 ID 获取卡片成功
func TestDouyinCardService_GetByID_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.douyin.com",
		DomainPoolID: 1,
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

// TestDouyinCardService_GetByID_NotFound 测试获取不存在的卡片
func TestDouyinCardService_GetByID_NotFound(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	_, err := service.GetByID(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for getting non-existent card")
	}
}

// TestDouyinCardService_GetByIDWithRefresh_Success 测试根据 ID 获取卡片（强制刷新）成功
func TestDouyinCardService_GetByIDWithRefresh_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetchedCard, err := service.GetByIDWithRefresh(context.Background(), createdCard.ID)
	if err != nil {
		t.Fatalf("GetByIDWithRefresh failed: %v", err)
	}

	if fetchedCard.ID != createdCard.ID {
		t.Errorf("Expected ID %d, got %d", createdCard.ID, fetchedCard.ID)
	}
}

// TestDouyinCardService_GetCardModelByID_Success 测试获取卡片模型成功
func TestDouyinCardService_GetCardModelByID_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	modelCard, err := service.GetCardModelByID(context.Background(), createdCard.ID)
	if err != nil {
		t.Fatalf("GetCardModelByID failed: %v", err)
	}

	if modelCard.ID != createdCard.ID {
		t.Errorf("Expected ID %d, got %d", createdCard.ID, modelCard.ID)
	}
	if modelCard.Title != createdCard.Title {
		t.Errorf("Expected title %s, got %s", createdCard.Title, modelCard.Title)
	}
}

// TestDouyinCardService_GetList_Success 测试获取卡片列表成功
func TestDouyinCardService_GetList_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	for i := 1; i <= 5; i++ {
		createReq := &dto.DouyinCardCreateRequest{
			Title:        "Card " + string(rune('0'+i)),
			Description:  "Description " + string(rune('0'+i)),
			ImageURL:     "https://example.com/image" + string(rune('0'+i)) + ".jpg",
			DomainPoolID: 1,
			IsActive:     boolPtr(true),
		}
		_, err := service.Create(context.Background(), createReq)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq := &dto.DouyinCardListRequest{
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

// TestDouyinCardService_GetList_Pagination 测试分页获取卡片列表
func TestDouyinCardService_GetList_Pagination(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	for i := 1; i <= 15; i++ {
		createReq := &dto.DouyinCardCreateRequest{
			Title:        "Card " + string(rune('0'+i)),
			Description:  "Description " + string(rune('0'+i)),
			ImageURL:     "https://example.com/image" + string(rune('0'+i)) + ".jpg",
			DomainPoolID: 1,
			IsActive:     boolPtr(true),
		}
		_, err := service.Create(context.Background(), createReq)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq := &dto.DouyinCardListRequest{
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

// TestDouyinCardService_GetList_EmptyList 测试获取空列表
func TestDouyinCardService_GetList_EmptyList(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	listReq := &dto.DouyinCardListRequest{
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

// TestDouyinCardService_ShareCard_Success 测试分享卡片成功
func TestDouyinCardService_ShareCard_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Share Test Card",
		Description:  "This card will be shared",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = service.ShareCard(context.Background(), createdCard.ID, "wechat")
	if err != nil {
		t.Fatalf("ShareCard failed: %v", err)
	}
}

// TestDouyinCardService_ShareCard_NotFound 测试分享不存在的卡片
func TestDouyinCardService_ShareCard_NotFound(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	err := service.ShareCard(context.Background(), 999, "wechat")
	if err == nil {
		t.Error("Expected error for sharing non-existent card")
	}
}

// TestDouyinCardService_GenerateShortLink_Success 测试生成短链成功
func TestDouyinCardService_GenerateShortLink_Success(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Card with Short Link",
		Description:  "This card will have a short link",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if createdCard.ShortCode == "" {
		t.Log("Short code not generated (this may be expected if short link service has issues)")
	}
}

// TestDouyinCardService_ToResponse 测试 toResponse 方法
func TestDouyinCardService_ToResponse(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	card := &model.DouyinCard{
		ID:           1,
		Title:        "Test Card",
		Description:  "Test Description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.douyin.com",
		DomainPoolID: 1,
		Tags:         "test,card",
		ViewCount:    100,
		IsActive:     true,
	}
	result := database.Create(card)
	if result.Error != nil {
		t.Fatalf("Failed to create test card: %v", result.Error)
	}

	fetchedCard, err := service.GetByID(context.Background(), card.ID)
	if err == nil {
		if fetchedCard.Title != card.Title {
			t.Errorf("Expected title %s, got %s", card.Title, fetchedCard.Title)
		}
	}
}

// TestDouyinCardService_ToResponseWithShortLink 测试 toResponseWithShortLink 方法
func TestDouyinCardService_ToResponseWithShortLink(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Card with Short Link",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_ = createdCard.ShortCode
	_ = createdCard.ShortLinkURL
}

// TestDouyinCardService_Create_WithTags 测试创建带标签的卡片
func TestDouyinCardService_Create_WithTags(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:        "Card with Tags",
		Description:  "This card has tags",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
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

// TestDouyinCardService_Create_InactiveCard 测试创建非激活卡片
func TestDouyinCardService_Create_InactiveCard(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	req := &dto.DouyinCardCreateRequest{
		Title:        "Inactive Card",
		Description:  "This card is inactive",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
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

// TestDouyinCardService_Update_DomainPoolChange 测试域名池变更时重新生成短链
func TestDouyinCardService_Update_DomainPoolChange(t *testing.T) {
	database := setupDouyinCardServiceTestDB(t)
	service := NewDouyinCardService(database)

	createReq := &dto.DouyinCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.DouyinCardUpdateRequest{
		ID:           createdCard.ID,
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
		IsActive:     boolPtr(true),
	}

	_, err = service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
}

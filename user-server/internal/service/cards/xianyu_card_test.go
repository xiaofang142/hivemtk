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

func setupXianyuCardServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.XianyuCard{},
		&model.ShortLink{},
		&model.DomainPool{},
		&model.ShortLinkAccess{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewXianyuCardService 测试创建闲鱼卡片服务
func TestNewXianyuCardService(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestXianyuCardService_Create_Success 测试创建闲鱼卡片成功
func TestXianyuCardService_Create_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	req := &dto.XianyuCardCreateRequest{
		Title:        "Test Card",
		Description:  "This is a test card",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xianyu.cn",
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
}

// TestXianyuCardService_Create_EmptyTitle 测试创建空标题卡片
func TestXianyuCardService_Create_EmptyTitle(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	req := &dto.XianyuCardCreateRequest{
		Title:       "",
		Description: "Test description",
		ImageURL:    "https://example.com/image.jpg",
	}

	_, err := service.Create(context.Background(), req)

	if err != nil {
		t.Logf("Create with empty title failed (expected): %v", err)
	}
}

// TestXianyuCardService_Create_EmptyRedirectURL 测试创建无跳转链接卡片
func TestXianyuCardService_Create_EmptyRedirectURL(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	req := &dto.XianyuCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		DomainPoolID: 1,
	}

	card, err := service.Create(context.Background(), req)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if card.ID == 0 {
		t.Error("Expected non-zero ID")
	}
}

// TestXianyuCardService_Update_Success 测试更新闲鱼卡片成功
func TestXianyuCardService_Update_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	createReq := &dto.XianyuCardCreateRequest{
		Title:        "Original Card",
		Description:  "Original description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xianyu.cn",
		DomainPoolID: 1,
		Tags:         "original",
		IsActive:     boolPtr(true),
	}
	createdCard, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.XianyuCardUpdateRequest{
		ID:          createdCard.ID,
		Title:       "Updated Card",
		Description: "Updated description",
		ImageURL:    "https://example.com/new-image.jpg",
		RedirectURL: "https://www.xianyu.cn/new",
		Tags:        "updated,card",
		LikeCount:   50,
		ShareCount:  25,
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
	if updatedCard.LikeCount != updateReq.LikeCount {
		t.Errorf("Expected like count %d, got %d", updateReq.LikeCount, updatedCard.LikeCount)
	}
	if updatedCard.ShareCount != updateReq.ShareCount {
		t.Errorf("Expected share count %d, got %d", updateReq.ShareCount, updatedCard.ShareCount)
	}
	if updatedCard.ViewCount != updateReq.ViewCount {
		t.Errorf("Expected view count %d, got %d", updateReq.ViewCount, updatedCard.ViewCount)
	}
}

// TestXianyuCardService_Update_NotFound 测试更新不存在的卡片
func TestXianyuCardService_Update_NotFound(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	updateReq := &dto.XianyuCardUpdateRequest{
		ID:    999,
		Title: "Non-existent Card",
	}

	_, err := service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for updating non-existent card")
	}
}

// TestXianyuCardService_Delete_Success 测试删除闲鱼卡片成功
func TestXianyuCardService_Delete_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	createReq := &dto.XianyuCardCreateRequest{
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

// TestXianyuCardService_Delete_NotFound 测试删除不存在的卡片
func TestXianyuCardService_Delete_NotFound(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	err := service.Delete(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for deleting non-existent card")
	}
}

// TestXianyuCardService_GetByID_Success 测试根据 ID 获取卡片成功
func TestXianyuCardService_GetByID_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	createReq := &dto.XianyuCardCreateRequest{
		Title:        "Test Card",
		Description:  "Test description",
		ImageURL:     "https://example.com/image.jpg",
		RedirectURL:  "https://www.xianyu.cn",
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

// TestXianyuCardService_GetByID_NotFound 测试获取不存在的卡片
func TestXianyuCardService_GetByID_NotFound(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	_, err := service.GetByID(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for getting non-existent card")
	}
}

// TestXianyuCardService_GetByIDWithRefresh_Success 测试根据 ID 获取卡片（强制刷新）成功
func TestXianyuCardService_GetByIDWithRefresh_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	createReq := &dto.XianyuCardCreateRequest{
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

// TestXianyuCardService_GetList_Success 测试获取卡片列表成功
func TestXianyuCardService_GetList_Success(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	for i := 1; i <= 5; i++ {
		createReq := &dto.XianyuCardCreateRequest{
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

	listReq := &dto.XianyuCardListRequest{
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

// TestXianyuCardService_GetList_Pagination 测试分页获取卡片列表
func TestXianyuCardService_GetList_Pagination(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	for i := 1; i <= 15; i++ {
		createReq := &dto.XianyuCardCreateRequest{
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

	listReq := &dto.XianyuCardListRequest{
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

// TestXianyuCardService_GetList_EmptyList 测试获取空列表
func TestXianyuCardService_GetList_EmptyList(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	listReq := &dto.XianyuCardListRequest{
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

// TestXianyuCardService_Create_WithTags 测试创建带标签的卡片
func TestXianyuCardService_Create_WithTags(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	req := &dto.XianyuCardCreateRequest{
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

// TestXianyuCardService_Create_InactiveCard 测试创建非激活卡片
func TestXianyuCardService_Create_InactiveCard(t *testing.T) {
	database := setupXianyuCardServiceTestDB(t)
	service := NewXianyuCardService(database)

	req := &dto.XianyuCardCreateRequest{
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

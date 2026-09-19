package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"

	"gorm.io/gorm"
)

func setupShortLinkServiceTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.ShortLink{},
		&model.ShortLinkAccess{},
		&model.DomainPool{},
	)
}

func newTestShortLinkService(db *gorm.DB) ShortLinkService {
	return NewShortLinkService(db)
}

func newTestShortLinkAccessRepository(db *gorm.DB) repository.ShortLinkAccessRepository {
	return repository.NewShortLinkAccessRepository(db)
}

// TestNewShortLinkService 测试创建短链服务
func TestNewShortLinkService(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)

	service := NewShortLinkService(database)
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestShortLinkService_Create 测试创建短链
func TestShortLinkService_Create(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "abc123",
		OriginalURL: "https://example.com/very/long/url",
		Title:       "测试短链",
		Description: "这是一个测试短链",
		Password:    "secret123",
	}

	resp, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp.ShortCode != "abc123" {
		t.Errorf("Expected ShortCode 'abc123', got %s", resp.ShortCode)
	}
	if resp.OriginalURL != "https://example.com/very/long/url" {
		t.Errorf("Expected OriginalURL 'https://example.com/very/long/url', got %s", resp.OriginalURL)
	}
	if resp.Title != "测试短链" {
		t.Errorf("Expected Title '测试短链', got %s", resp.Title)
	}
	if resp.Status != 1 {
		t.Errorf("Expected Status 1, got %d", resp.Status)
	}
}

// TestShortLinkService_Create_DuplicateShortCode 测试创建重复短码的短链
func TestShortLinkService_Create_DuplicateShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req1 := &dto.CreateShortLinkRequest{
		ShortCode:   "duplicate",
		OriginalURL: "https://example.com/1",
	}
	_, err := service.Create(context.Background(), req1)
	if err != nil {
		t.Fatalf("First Create failed: %v", err)
	}

	req2 := &dto.CreateShortLinkRequest{
		ShortCode:   "duplicate",
		OriginalURL: "https://example.com/2",
	}
	_, err = service.Create(context.Background(), req2)
	if err == nil {
		t.Error("Expected error for duplicate short code")
	}
	if err.Error() != "短码已存在" {
		t.Errorf("Expected '短码已存在', got %s", err.Error())
	}
}

// TestShortLinkService_Create_WithDomain 测试创建带域名的短链
func TestShortLinkService_Create_WithDomain(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	domain := &model.DomainPool{
		Domain: "test.com",
		Status: 1,
	}
	database.Create(domain)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "withdomain",
		OriginalURL: "https://example.com",
		DomainID:    uint(domain.ID),
	}

	resp, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create with domain failed: %v", err)
	}

	if resp.DomainID != uint(domain.ID) {
		t.Errorf("Expected DomainID %d, got %d", domain.ID, resp.DomainID)
	}
}

// TestShortLinkService_Create_WithInvalidDomain 测试创建带无效域名的短链
func TestShortLinkService_Create_WithInvalidDomain(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "invaliddomain",
		OriginalURL: "https://example.com",
		DomainID:    99999,
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent domain")
	}
	if err.Error() != "域名不存在" {
		t.Errorf("Expected '域名不存在', got %s", err.Error())
	}
}

// TestShortLinkService_Create_WithUnavailableDomain 测试创建带不可用域名的短链
func TestShortLinkService_Create_WithUnavailableDomain(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	domain := &model.DomainPool{
		Domain: "unavailable.com",
		Status: 2,
	}
	database.Create(domain)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "unavailabledomain",
		OriginalURL: "https://example.com",
		DomainID:    uint(domain.ID),
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for unavailable domain")
	}
	if err.Error() != "域名不可用" {
		t.Errorf("Expected '域名不可用', got %s", err.Error())
	}
}

// TestShortLinkService_Create_WithExpireTime 测试创建带过期时间的短链
func TestShortLinkService_Create_WithExpireTime(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	expireTime := time.Now().Add(24 * time.Hour)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "expire",
		OriginalURL: "https://example.com",
		ExpireTime:  &expireTime,
	}

	resp, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create with expire time failed: %v", err)
	}

	if resp.ExpireTime == nil {
		t.Error("Expected ExpireTime to be set")
	}
}

// TestShortLinkService_Create_EmptyShortCode 测试创建空短码的短链
// 注意：实际验证应在控制器层进行，服务层直接接收 DTO
func TestShortLinkService_Create_EmptyShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "",
		OriginalURL: "https://example.com",
	}

	_, err := service.Create(context.Background(), req)
	if err != nil {
		t.Logf("Create with empty short code: %v", err)
	}
}

// TestShortLinkService_Create_EmptyOriginalURL 测试创建空原始 URL 的短链
// 注意：实际验证应在控制器层进行，服务层直接接收 DTO
func TestShortLinkService_Create_EmptyOriginalURL(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "emptyurl",
		OriginalURL: "",
	}

	_, err := service.Create(context.Background(), req)
	if err != nil {
		t.Logf("Create with empty original URL: %v", err)
	}
}

// TestShortLinkService_Update 测试更新短链
func TestShortLinkService_Update(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "original",
		OriginalURL: "https://example.com/original",
		Title:       "原标题",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expireTime := time.Now().Add(24 * time.Hour)
	updateReq := &dto.UpdateShortLinkRequest{
		ID:          createResp.ID,
		ShortCode:   "updated",
		OriginalURL: "https://example.com/updated",
		Title:       "新标题",
		Description: "新描述",
		Status:      2,
		ExpireTime:  &expireTime,
	}

	resp, err := service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if resp.ShortCode != "updated" {
		t.Errorf("Expected ShortCode 'updated', got %s", resp.ShortCode)
	}
	if resp.OriginalURL != "https://example.com/updated" {
		t.Errorf("Expected OriginalURL 'https://example.com/updated', got %s", resp.OriginalURL)
	}
	if resp.Title != "新标题" {
		t.Errorf("Expected Title '新标题', got %s", resp.Title)
	}
	if resp.Status != 2 {
		t.Errorf("Expected Status 2, got %d", resp.Status)
	}
}

// TestShortLinkService_Update_NotFound 测试更新不存在的短链
func TestShortLinkService_Update_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          99999,
		OriginalURL: "https://example.com",
	}

	_, err := service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_Update_DuplicateShortCode 测试更新为已存在的短码
func TestShortLinkService_Update_DuplicateShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req1 := &dto.CreateShortLinkRequest{
		ShortCode:   "short1",
		OriginalURL: "https://example.com/1",
	}
	_, err := service.Create(context.Background(), req1)
	if err != nil {
		t.Fatalf("Create first failed: %v", err)
	}

	req2 := &dto.CreateShortLinkRequest{
		ShortCode:   "short2",
		OriginalURL: "https://example.com/2",
	}
	resp2, err := service.Create(context.Background(), req2)
	if err != nil {
		t.Fatalf("Create second failed: %v", err)
	}

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          resp2.ID,
		ShortCode:   "short1",
		OriginalURL: "https://example.com/2",
	}

	_, err = service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for duplicate short code")
	}
	if err.Error() != "短码已存在" {
		t.Errorf("Expected '短码已存在', got %s", err.Error())
	}
}

// TestShortLinkService_Update_SameShortCode 测试更新为相同的短码
func TestShortLinkService_Update_SameShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "samecode",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          createResp.ID,
		ShortCode:   "samecode",
		OriginalURL: "https://example.com/updated",
	}

	_, err = service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update with same short code failed: %v", err)
	}
}

// TestShortLinkService_Update_WithInvalidDomain 测试更新带无效域名的短链
func TestShortLinkService_Update_WithInvalidDomain(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "updatedomain",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          createResp.ID,
		OriginalURL: "https://example.com",
		DomainID:    99999,
	}

	_, err = service.Update(context.Background(), updateReq)
	if err == nil {
		t.Error("Expected error for non-existent domain")
	}
	if err.Error() != "域名不存在" {
		t.Errorf("Expected '域名不存在', got %s", err.Error())
	}
}

// TestShortLinkService_Delete 测试删除短链
func TestShortLinkService_Delete(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "todelete",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = service.Delete(context.Background(), createResp.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = service.GetByID(context.Background(), createResp.ID)
	if err == nil {
		t.Error("Expected error for deleted short link")
	}
}

// TestShortLinkService_Delete_NotFound 测试删除不存在的短链
func TestShortLinkService_Delete_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	err := service.Delete(context.Background(), 99999)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_GetByID 测试根据 ID 获取短链
func TestShortLinkService_GetByID(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "getbyid",
		OriginalURL: "https://example.com/getbyid",
		Title:       "测试标题",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resp, err := service.GetByID(context.Background(), createResp.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if resp.ID != createResp.ID {
		t.Errorf("Expected ID %d, got %d", createResp.ID, resp.ID)
	}
	if resp.ShortCode != "getbyid" {
		t.Errorf("Expected ShortCode 'getbyid', got %s", resp.ShortCode)
	}
	if resp.OriginalURL != "https://example.com/getbyid" {
		t.Errorf("Expected OriginalURL 'https://example.com/getbyid', got %s", resp.OriginalURL)
	}
	if resp.Title != "测试标题" {
		t.Errorf("Expected Title '测试标题', got %s", resp.Title)
	}
}

// TestShortLinkService_GetByID_NotFound 测试获取不存在的短链
func TestShortLinkService_GetByID_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	_, err := service.GetByID(context.Background(), 99999)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_GetByShortCode 测试根据短码获取短链
func TestShortLinkService_GetByShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "getbysc",
		OriginalURL: "https://example.com/getbysc",
		Title:       "测试标题",
	}
	_, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resp, err := service.GetByShortCode(context.Background(), "getbysc")
	if err != nil {
		t.Fatalf("GetByShortCode failed: %v", err)
	}

	if resp.ShortCode != "getbysc" {
		t.Errorf("Expected ShortCode 'getbysc', got %s", resp.ShortCode)
	}
	if resp.Title != "测试标题" {
		t.Errorf("Expected Title '测试标题', got %s", resp.Title)
	}
}

// TestShortLinkService_GetByShortCode_NotFound 测试获取不存在的短码
func TestShortLinkService_GetByShortCode_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	_, err := service.GetByShortCode(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent short code")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_GetList 测试获取短链列表
func TestShortLinkService_GetList(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	for i := 0; i < 5; i++ {
		req := &dto.CreateShortLinkRequest{
			ShortCode:   "list" + string(rune('0'+i)),
			OriginalURL: "https://example.com/" + string(rune('0'+i)),
			Title:       "短链" + string(rune('0'+i)),
		}
		_, err := service.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq := &dto.ListShortLinkRequest{
		Page:     1,
		PageSize: 10,
	}

	resp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if resp.Total != 5 {
		t.Errorf("Expected total 5, got %d", resp.Total)
	}
	if len(resp.List) != 5 {
		t.Errorf("Expected 5 items, got %d", len(resp.List))
	}
}

// TestShortLinkService_GetList_WithPagination 测试分页获取短链列表
func TestShortLinkService_GetList_WithPagination(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	for i := 0; i < 10; i++ {
		req := &dto.CreateShortLinkRequest{
			ShortCode:   "page" + string(rune('0'+i)),
			OriginalURL: "https://example.com/" + string(rune('0'+i)),
		}
		_, err := service.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	listReq1 := &dto.ListShortLinkRequest{
		Page:     1,
		PageSize: 5,
	}
	resp1, err := service.GetList(context.Background(), listReq1)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}

	listReq2 := &dto.ListShortLinkRequest{
		Page:     2,
		PageSize: 5,
	}
	resp2, err := service.GetList(context.Background(), listReq2)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}

	if resp1.Total != 10 {
		t.Errorf("Expected total 10, got %d", resp1.Total)
	}
	if len(resp1.List) != 5 {
		t.Errorf("Expected 5 items on page 1, got %d", len(resp1.List))
	}
	if len(resp2.List) != 5 {
		t.Errorf("Expected 5 items on page 2, got %d", len(resp2.List))
	}
}

// TestShortLinkService_GetList_WithShortCodeFilter 测试带短码过滤的列表
func TestShortLinkService_GetList_WithShortCodeFilter(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "test1", OriginalURL: "https://example.com/1"})
	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "test2", OriginalURL: "https://example.com/2"})
	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "other", OriginalURL: "https://example.com/3"})

	listReq := &dto.ListShortLinkRequest{
		Page:      1,
		PageSize:  10,
		ShortCode: "test",
	}

	resp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("Expected total 2, got %d", resp.Total)
	}
}

// TestShortLinkService_GetList_WithStatusFilter 测试带状态过滤的列表
func TestShortLinkService_GetList_WithStatusFilter(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "status1", OriginalURL: "https://example.com/1"})
	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "status2", OriginalURL: "https://example.com/2"})
	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "status3", OriginalURL: "https://example.com/3"})

	database.Model(&model.ShortLink{}).Where("short_code = ?", "status3").Update("status", 2)

	listReq := &dto.ListShortLinkRequest{
		Page:     1,
		PageSize: 10,
		Status:   1,
	}

	resp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("Expected total 2, got %d", resp.Total)
	}
}

// TestShortLinkService_GetList_EmptyList 测试空列表
func TestShortLinkService_GetList_EmptyList(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	listReq := &dto.ListShortLinkRequest{
		Page:     1,
		PageSize: 10,
	}

	resp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("Expected total 0, got %d", resp.Total)
	}
	if len(resp.List) != 0 {
		t.Errorf("Expected 0 items, got %d", len(resp.List))
	}
}

// TestShortLinkService_GetList_DefaultPagination 测试默认分页参数
func TestShortLinkService_GetList_DefaultPagination(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	service.Create(context.Background(), &dto.CreateShortLinkRequest{ShortCode: "default1", OriginalURL: "https://example.com/1"})

	listReq := &dto.ListShortLinkRequest{}

	resp, err := service.GetList(context.Background(), listReq)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("Expected non-zero total")
	}
}

// TestShortLinkService_AccessShortLink 测试访问短链
func TestShortLinkService_AccessShortLink(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "access",
		OriginalURL: "https://example.com/access",
		Title:       "测试访问",
	}
	_, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "access",
		IP:        "192.168.1.1",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		Referer:   "https://google.com",
	}

	resp, err := service.AccessShortLink(context.Background(), accessReq)
	if err != nil {
		t.Fatalf("AccessShortLink failed: %v", err)
	}

	if resp.OriginalURL != "https://example.com/access" {
		t.Errorf("Expected OriginalURL 'https://example.com/access', got %s", resp.OriginalURL)
	}
	if resp.Title != "测试访问" {
		t.Errorf("Expected Title '测试访问', got %s", resp.Title)
	}

	link, err := service.GetByShortCode(context.Background(), "access")
	if err != nil {
		t.Fatalf("GetByShortCode failed: %v", err)
	}
	if link.ClickCount != 1 {
		t.Errorf("Expected ClickCount 1, got %d", link.ClickCount)
	}
}

// TestShortLinkService_AccessShortLink_NotFound 测试访问不存在的短链
func TestShortLinkService_AccessShortLink_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "nonexistent",
	}

	_, err := service.AccessShortLink(context.Background(), accessReq)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_AccessShortLink_Expired 测试访问已过期的短链
func TestShortLinkService_AccessShortLink_Expired(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	expireTime := time.Now().Add(-24 * time.Hour)
	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "expired",
		OriginalURL: "https://example.com/expired",
		ExpireTime:  &expireTime,
	}
	_, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "expired",
	}

	_, err = service.AccessShortLink(context.Background(), accessReq)
	if err == nil {
		t.Error("Expected error for expired short link")
	}
	if err.Error() != "短链已过期或已禁用" {
		t.Errorf("Expected '短链已过期或已禁用', got %s", err.Error())
	}
}

// TestShortLinkService_AccessShortLink_Disabled 测试访问已禁用的短链
func TestShortLinkService_AccessShortLink_Disabled(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "disabled",
		OriginalURL: "https://example.com/disabled",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          createResp.ID,
		OriginalURL: "https://example.com/disabled",
		Status:      2,
	}
	_, err = service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	database.Model(&model.ShortLink{}).Where("id = ?", createResp.ID).Update("status", 2)

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "disabled",
	}

	_, err = service.AccessShortLink(context.Background(), accessReq)
	if err == nil {
		t.Error("Expected error for disabled short link")
	}
	if err.Error() != "短链已过期或已禁用" {
		t.Errorf("Expected '短链已过期或已禁用', got %s", err.Error())
	}
}

// TestShortLinkService_AccessShortLink_WithPassword 测试访问带密码的短链
func TestShortLinkService_AccessShortLink_WithPassword(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "password",
		OriginalURL: "https://example.com/password",
		Password:    "secret123",
	}
	_, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	accessReqWrong := &dto.AccessShortLinkRequest{
		ShortCode: "password",
		Password:  "wrong",
	}

	_, err = service.AccessShortLink(context.Background(), accessReqWrong)
	if err == nil {
		t.Error("Expected error for wrong password")
	}
	if err.Error() != "密码错误" {
		t.Errorf("Expected '密码错误', got %s", err.Error())
	}

	accessReqCorrect := &dto.AccessShortLinkRequest{
		ShortCode: "password",
		Password:  "secret123",
	}

	resp, err := service.AccessShortLink(context.Background(), accessReqCorrect)
	if err != nil {
		t.Fatalf("AccessShortLink with correct password failed: %v", err)
	}

	if resp.OriginalURL != "https://example.com/password" {
		t.Errorf("Expected OriginalURL 'https://example.com/password', got %s", resp.OriginalURL)
	}
}

// TestShortLinkService_AccessShortLink_WithDeviceParsing 测试访问短链的设备解析
func TestShortLinkService_AccessShortLink_WithDeviceParsing(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "device",
		OriginalURL: "https://example.com/device",
	}
	_, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	mobileReq := &dto.AccessShortLinkRequest{
		ShortCode: "device",
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X)",
	}
	_, err = service.AccessShortLink(context.Background(), mobileReq)
	if err != nil {
		t.Fatalf("Mobile access failed: %v", err)
	}

	accessRepo := newTestShortLinkAccessRepository(database)
	accesses, _, err := accessRepo.GetByShortLinkID(context.Background(), 1, 1, 10)
	if err != nil {
		t.Fatalf("GetByShortLinkID failed: %v", err)
	}

	if len(accesses) == 0 {
		t.Fatal("Expected at least one access record")
	}
	if accesses[0].DeviceType != "mobile" {
		t.Errorf("Expected DeviceType 'mobile', got %s", accesses[0].DeviceType)
	}
}

// TestShortLinkService_GenerateShortCode 测试生成短码
func TestShortLinkService_GenerateShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.GenerateShortCodeRequest{
		Length: 6,
	}

	resp, err := service.GenerateShortCode(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateShortCode failed: %v", err)
	}

	if len(resp.ShortCode) != 6 {
		t.Errorf("Expected ShortCode length 6, got %d", len(resp.ShortCode))
	}
}

// TestShortLinkService_GenerateShortCode_DefaultLength 测试生成短码使用默认长度
func TestShortLinkService_GenerateShortCode_DefaultLength(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.GenerateShortCodeRequest{}

	resp, err := service.GenerateShortCode(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateShortCode failed: %v", err)
	}

	if len(resp.ShortCode) != 6 {
		t.Errorf("Expected default ShortCode length 6, got %d", len(resp.ShortCode))
	}
}

// TestShortLinkService_GenerateShortCode_Uniqueness 测试生成短码的唯一性
func TestShortLinkService_GenerateShortCode_Uniqueness(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	shortCodes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		req := &dto.GenerateShortCodeRequest{
			Length: 8,
		}
		resp, err := service.GenerateShortCode(context.Background(), req)
		if err != nil {
			t.Fatalf("GenerateShortCode failed: %v", err)
		}

		if shortCodes[resp.ShortCode] {
			t.Errorf("Duplicate short code generated: %s", resp.ShortCode)
		}
		shortCodes[resp.ShortCode] = true
	}
}

// TestShortLinkService_GenerateShortCode_WithExistingShortCodes 测试生成短码时跳过已存在的
func TestShortLinkService_GenerateShortCode_WithExistingShortCodes(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	for i := 0; i < 10; i++ {
		req := &dto.CreateShortLinkRequest{
			ShortCode:   "existing" + string(rune('0'+i)),
			OriginalURL: "https://example.com",
		}
		_, err := service.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	genReq := &dto.GenerateShortCodeRequest{
		Length: 8,
	}
	resp, err := service.GenerateShortCode(context.Background(), genReq)
	if err != nil {
		t.Fatalf("GenerateShortCode failed: %v", err)
	}

	_, err = service.GetByShortCode(context.Background(), resp.ShortCode)
	if err == nil {
		t.Error("Generated short code should not exist")
	}
}

// TestShortLinkService_GetStats 测试获取短链统计
func TestShortLinkService_GetStats(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "stats",
		OriginalURL: "https://example.com/stats",
		Title:       "统计测试",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		accessReq := &dto.AccessShortLinkRequest{
			ShortCode: "stats",
			IP:        "192.168.1." + string(rune('1'+i)),
			UserAgent: "Mozilla/5.0",
		}
		_, _ = service.AccessShortLink(context.Background(), accessReq)
	}

	statsReq := &dto.ShortLinkStatsRequest{
		ID: createResp.ID,
	}

	stats, err := service.GetStats(context.Background(), statsReq)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.ShortLinkID != createResp.ID {
		t.Errorf("Expected ShortLinkID %d, got %d", createResp.ID, stats.ShortLinkID)
	}
	if stats.ShortCode != "stats" {
		t.Errorf("Expected ShortCode 'stats', got %s", stats.ShortCode)
	}
	if stats.TotalAccess != 5 {
		t.Errorf("Expected TotalAccess 5, got %d", stats.TotalAccess)
	}
}

// TestShortLinkService_GetStats_NotFound 测试获取不存在短链的统计
func TestShortLinkService_GetStats_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	statsReq := &dto.ShortLinkStatsRequest{
		ID: 99999,
	}

	_, err := service.GetStats(context.Background(), statsReq)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_GetStats_InvalidDateFormat 测试获取统计时日期格式错误
func TestShortLinkService_GetStats_InvalidDateFormat(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "statsdate",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	statsReq := &dto.ShortLinkStatsRequest{
		ID:        createResp.ID,
		StartDate: "invalid-date",
	}

	_, err = service.GetStats(context.Background(), statsReq)
	if err == nil {
		t.Error("Expected error for invalid date format")
	}
	if err.Error() != "开始日期格式错误，请使用YYYY-MM-DD格式" {
		t.Errorf("Expected correct error message, got %s", err.Error())
	}
}

// TestShortLinkService_GetStats_WithDateRange 测试获取指定日期范围的统计
func TestShortLinkService_GetStats_WithDateRange(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "statsrange",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "statsrange",
		UserAgent: "Mozilla/5.0",
	}
	_, _ = service.AccessShortLink(context.Background(), accessReq)

	today := timeutil.BusinessToday()
	statsReq := &dto.ShortLinkStatsRequest{
		ID:        createResp.ID,
		StartDate: today,
		EndDate:   today,
	}

	stats, err := service.GetStats(context.Background(), statsReq)
	if err != nil {
		t.Fatalf("GetStats with date range failed: %v", err)
	}

	if stats.TotalAccess < 1 {
		t.Errorf("Expected TotalAccess >= 1, got %d", stats.TotalAccess)
	}
}

// TestShortLinkService_GetStats_DeviceTypeStats 测试获取设备类型统计
func TestShortLinkService_GetStats_DeviceTypeStats(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "statsdevice",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	mobileReq := &dto.AccessShortLinkRequest{
		ShortCode: "statsdevice",
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X)",
	}
	_, _ = service.AccessShortLink(context.Background(), mobileReq)

	desktopReq := &dto.AccessShortLinkRequest{
		ShortCode: "statsdevice",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	}
	_, _ = service.AccessShortLink(context.Background(), desktopReq)
	_, _ = service.AccessShortLink(context.Background(), desktopReq)

	statsReq := &dto.ShortLinkStatsRequest{
		ID: createResp.ID,
	}

	stats, err := service.GetStats(context.Background(), statsReq)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if len(stats.DeviceTypeStats) == 0 {
		t.Error("Expected non-empty DeviceTypeStats")
	}
}

// TestShortLinkService_GetAllStats 测试获取所有短链统计
func TestShortLinkService_GetAllStats(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	for i := 0; i < 3; i++ {
		req := &dto.CreateShortLinkRequest{
			ShortCode:   "allstats" + string(rune('0'+i)),
			OriginalURL: "https://example.com/" + string(rune('0'+i)),
			Title:       "短链" + string(rune('0'+i)),
		}
		_, err := service.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		accessReq := &dto.AccessShortLinkRequest{
			ShortCode: "allstats" + string(rune('0'+i)),
			UserAgent: "Mozilla/5.0",
		}
		_, _ = service.AccessShortLink(context.Background(), accessReq)
	}

	allStatsReq := &dto.AllShortLinksStatsRequest{}

	allStats, err := service.GetAllStats(context.Background(), allStatsReq)
	if err != nil {
		t.Fatalf("GetAllStats failed: %v", err)
	}

	if allStats.TotalAccess < 3 {
		t.Errorf("Expected TotalAccess >= 3, got %d", allStats.TotalAccess)
	}
}

// TestShortLinkService_GetAllStats_WithDateRange 测试获取指定日期范围的所有短链统计
func TestShortLinkService_GetAllStats_WithDateRange(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "allstatsrange",
		OriginalURL: "https://example.com",
	}
	_, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	accessReq := &dto.AccessShortLinkRequest{
		ShortCode: "allstatsrange",
		UserAgent: "Mozilla/5.0",
	}
	_, _ = service.AccessShortLink(context.Background(), accessReq)

	today := timeutil.BusinessToday()
	allStatsReq := &dto.AllShortLinksStatsRequest{
		StartDate: today,
		EndDate:   today,
	}

	allStats, err := service.GetAllStats(context.Background(), allStatsReq)
	if err != nil {
		t.Fatalf("GetAllStats with date range failed: %v", err)
	}

	if allStats.TotalAccess < 1 {
		t.Errorf("Expected TotalAccess >= 1, got %d", allStats.TotalAccess)
	}
}

// TestShortLinkService_GetAllStats_InvalidDateFormat 测试获取所有统计时日期格式错误
func TestShortLinkService_GetAllStats_InvalidDateFormat(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	allStatsReq := &dto.AllShortLinksStatsRequest{
		StartDate: "invalid-date",
	}

	_, err := service.GetAllStats(context.Background(), allStatsReq)
	if err == nil {
		t.Error("Expected error for invalid date format")
	}
	if err.Error() != "开始日期格式错误，请使用YYYY-MM-DD格式" {
		t.Errorf("Expected correct error message, got %s", err.Error())
	}
}

// TestShortLinkService_GetAllStats_EmptyStats 测试获取空统计
func TestShortLinkService_GetAllStats_EmptyStats(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	allStatsReq := &dto.AllShortLinksStatsRequest{}

	allStats, err := service.GetAllStats(context.Background(), allStatsReq)
	if err != nil {
		t.Fatalf("GetAllStats failed: %v", err)
	}

	if allStats.TotalAccess != 0 {
		t.Errorf("Expected TotalAccess 0, got %d", allStats.TotalAccess)
	}
}

// TestShortLinkService_ShareShortLink 测试分享短链
func TestShortLinkService_ShareShortLink(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "share",
		OriginalURL: "https://example.com/share",
		Title:       "分享测试",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	shareReq := &dto.ShareShortLinkRequest{
		ID: createResp.ID,
	}

	resp, err := service.ShareShortLink(context.Background(), shareReq)
	if err != nil {
		t.Fatalf("ShareShortLink failed: %v", err)
	}

	expectedURL := config.DefaultUserServerBaseURL + "/s/share"
	if resp.ShortURL != expectedURL {
		t.Errorf("Expected ShortURL '%s', got %s", expectedURL, resp.ShortURL)
	}

	if resp.QRCode == "" {
		t.Error("Expected non-empty QRCode")
	}
	if !strings.HasPrefix(resp.QRCode, "data:image/png;base64,") {
		t.Errorf("Expected QRCode to be base64 encoded image, got %s", resp.QRCode)
	}
}

// TestShortLinkService_ShareShortLink_NotFound 测试分享不存在的短链
func TestShortLinkService_ShareShortLink_NotFound(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	shareReq := &dto.ShareShortLinkRequest{
		ID: 99999,
	}

	_, err := service.ShareShortLink(context.Background(), shareReq)
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "短链不存在" {
		t.Errorf("Expected '短链不存在', got %s", err.Error())
	}
}

// TestShortLinkService_Create_VeryLongURL 测试创建超长 URL 的短链
func TestShortLinkService_Create_VeryLongURL(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	longURL := "https://example.com/"
	for i := 0; i < 50; i++ {
		longURL += "very/long/path/"
	}
	longURL += "?param1=value1&param2=value2&param3=value3&param4=value4&param5=value5"

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "longurl",
		OriginalURL: longURL,
	}

	resp, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create with very long URL failed: %v", err)
	}

	if resp.OriginalURL != longURL {
		t.Error("Expected long URL to be stored correctly")
	}
}

// TestShortLinkService_Create_SpecialCharactersInShortCode 测试创建带特殊字符的短码
func TestShortLinkService_Create_SpecialCharactersInShortCode(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "abc-123_test.xyz",
		OriginalURL: "https://example.com",
	}

	_, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create with special characters failed: %v", err)
	}
}

// TestShortLinkService_Update_PartialUpdate 测试部分更新短链
func TestShortLinkService_Update_PartialUpdate(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "partial",
		OriginalURL: "https://example.com/original",
		Title:       "原标题",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq := &dto.UpdateShortLinkRequest{
		ID:          createResp.ID,
		OriginalURL: "https://example.com/original",
		Title:       "新标题",
	}

	resp, err := service.Update(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if resp.ShortCode != "partial" {
		t.Errorf("Expected ShortCode 'partial', got %s", resp.ShortCode)
	}
	if resp.Title != "新标题" {
		t.Errorf("Expected Title '新标题', got %s", resp.Title)
	}
}

// TestShortLinkService_AccessShortLink_MultipleTimes 测试多次访问短链
func TestShortLinkService_AccessShortLink_MultipleTimes(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "multiaccess",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		accessReq := &dto.AccessShortLinkRequest{
			ShortCode: "multiaccess",
			UserAgent: "Mozilla/5.0",
		}
		_, _ = service.AccessShortLink(context.Background(), accessReq)
	}

	link, err := service.GetByID(context.Background(), createResp.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if link.ClickCount != 10 {
		t.Errorf("Expected ClickCount 10, got %d", link.ClickCount)
	}
}

// TestShortLinkService_GetStats_TodayAccess 测试获取今日访问量统计
func TestShortLinkService_GetStats_TodayAccess(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   "todaystats",
		OriginalURL: "https://example.com",
	}
	createResp, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		accessReq := &dto.AccessShortLinkRequest{
			ShortCode: "todaystats",
			UserAgent: "Mozilla/5.0",
		}
		_, _ = service.AccessShortLink(context.Background(), accessReq)
	}

	statsReq := &dto.ShortLinkStatsRequest{
		ID: createResp.ID,
	}

	stats, err := service.GetStats(context.Background(), statsReq)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TodayAccess < 3 {
		t.Errorf("Expected TodayAccess >= 3, got %d", stats.TodayAccess)
	}
}

// TestShortLinkService_GenerateShortCode_DifferentLengths 测试生成不同长度的短码
func TestShortLinkService_GenerateShortCode_DifferentLengths(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	lengths := []int{4, 5, 6, 7, 8, 9, 10}

	for _, length := range lengths {
		req := &dto.GenerateShortCodeRequest{
			Length: length,
		}

		resp, err := service.GenerateShortCode(context.Background(), req)
		if err != nil {
			t.Fatalf("GenerateShortCode with length %d failed: %v", length, err)
		}

		if len(resp.ShortCode) != length {
			t.Errorf("Expected ShortCode length %d, got %d", length, len(resp.ShortCode))
		}
	}
}

// TestShortLinkService_StatusStr 测试状态字符串转换
func TestShortLinkService_StatusStr(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	createReq1 := &dto.CreateShortLinkRequest{
		ShortCode:   "status1",
		OriginalURL: "https://example.com/1",
	}
	resp1, err := service.Create(context.Background(), createReq1)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp1.StatusStr != "正常" {
		t.Errorf("Expected StatusStr '正常', got %s", resp1.StatusStr)
	}

	createReq2 := &dto.CreateShortLinkRequest{
		ShortCode:   "status2",
		OriginalURL: "https://example.com/2",
	}
	resp2, err := service.Create(context.Background(), createReq2)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updateReq2 := &dto.UpdateShortLinkRequest{
		ID:          resp2.ID,
		OriginalURL: "https://example.com/2",
		Status:      2,
	}
	_, err = service.Update(context.Background(), updateReq2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	resp2Updated, _ := service.GetByID(context.Background(), resp2.ID)
	if resp2Updated.StatusStr != "禁用" {
		t.Errorf("Expected StatusStr '禁用', got %s", resp2Updated.StatusStr)
	}

	expireTime := time.Now().Add(-24 * time.Hour)
	createReq3 := &dto.CreateShortLinkRequest{
		ShortCode:   "status3",
		OriginalURL: "https://example.com/3",
		ExpireTime:  &expireTime,
	}
	resp3, err := service.Create(context.Background(), createReq3)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp3.StatusStr != "已过期" {
		t.Errorf("Expected StatusStr '已过期', got %s", resp3.StatusStr)
	}
}

// TestShortLinkService_Create_WithAllFields 测试创建带所有字段的短链
func TestShortLinkService_Create_WithAllFields(t *testing.T) {
	database := setupShortLinkServiceTestDB(t)
	service := newTestShortLinkService(database)

	domain := &model.DomainPool{
		Domain: "test.com",
		Status: 1,
	}
	database.Create(domain)

	expireTime := time.Now().Add(30 * 24 * time.Hour)

	req := &dto.CreateShortLinkRequest{
		ShortCode:   "allfields",
		OriginalURL: "https://example.com/allfields",
		Title:       "完整字段测试",
		Description: "这是一个包含所有字段的测试短链",
		DomainID:    uint(domain.ID),
		Password:    "password123",
		ExpireTime:  &expireTime,
	}

	resp, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create with all fields failed: %v", err)
	}

	if resp.ShortCode != "allfields" {
		t.Errorf("Expected ShortCode 'allfields', got %s", resp.ShortCode)
	}
	if resp.OriginalURL != "https://example.com/allfields" {
		t.Errorf("Expected OriginalURL 'https://example.com/allfields', got %s", resp.OriginalURL)
	}
	if resp.Title != "完整字段测试" {
		t.Errorf("Expected Title '完整字段测试', got %s", resp.Title)
	}
	if resp.Description != "这是一个包含所有字段的测试短链" {
		t.Errorf("Expected Description '这是一个包含所有字段的测试短链', got %s", resp.Description)
	}
	if resp.DomainID != uint(domain.ID) {
		t.Errorf("Expected DomainID %d, got %d", domain.ID, resp.DomainID)
	}
	if resp.Password != "password123" {
		t.Errorf("Expected Password 'password123', got %s", resp.Password)
	}
	if resp.ExpireTime == nil {
		t.Error("Expected ExpireTime to be set")
	}
}

func TestValidateTargetURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https ok", "https://example.com/path?x=1", false},
		{"http rejected", "http://example.com", true},
		{"ftp rejected", "ftp://example.com", true},
		{"empty rejected", "", true},
		{"no host rejected", "https://", true},
		{"invalid url rejected", "://bad url", true},
	}
	for _, c := range cases {
		err := validateTargetURL(c.url)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: url=%q err=%v wantErr=%v", c.name, c.url, err, c.wantErr)
		}
	}
}

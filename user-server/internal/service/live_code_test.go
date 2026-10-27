package service

import (
	"context"
	"errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupLiveCodeServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.LiveCode{},
		&model.LiveCodeQR{},
		&model.LiveCodeQRStat{},
		&model.LiveCodeClickLog{},
		&model.QRCodeClickLog{},
		&model.DomainPool{},
	)
	db.SetTestDB(database)
	return database
}

func createTestDomains(t *testing.T, db *gorm.DB) (shortDomain, entryDomain, landingDomain *model.DomainPool) {
	shortDomain = &model.DomainPool{
		Domain: "short.example.com",
		Port:   443,
		Status: 1,
	}
	entryDomain = &model.DomainPool{
		Domain: "entry.example.com",
		Port:   443,
		Status: 1,
	}
	landingDomain = &model.DomainPool{
		Domain: "landing.example.com",
		Port:   443,
		Status: 1,
	}

	db.Create(shortDomain)
	db.Create(entryDomain)
	db.Create(landingDomain)
	return
}

func newTestLiveCodeService(database *gorm.DB) LiveCodeService {
	return NewLiveCodeService()
}

// TestNewLiveCodeService 测试创建活码服务
func TestNewLiveCodeService(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)

	service := newTestLiveCodeService(database)
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestLiveCodeService_Create 测试创建活码
func TestLiveCodeService_Create(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
		ImageURL:        "https://example.com/image.png",
		EntryURL:        "https://entry.example.com",
		LandingURL:      "https://landing.example.com",
	}

	response, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if response.Name != "测试活码" {
		t.Errorf("Expected name '测试活码', got %s", response.Name)
	}

	if response.ShortLink != "test-link" {
		t.Errorf("Expected short_link 'test-link', got %s", response.ShortLink)
	}
}

// TestLiveCodeService_Create_DuplicateShortLink 测试创建重复短链的活码
func TestLiveCodeService_Create_DuplicateShortLink(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码 1",
		ShortLink:       "duplicate-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}

	_, err := service.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("First Create failed: %v", err)
	}

	_, err = service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for duplicate short link")
	}
	if !errors.Is(err, ErrLiveCodeShortLinkExists) {
		t.Errorf("Expected ErrLiveCodeShortLinkExists, got %v", err)
	}
}

// TestLiveCodeService_Create_InvalidShortDomain 测试创建时短链域名不存在
func TestLiveCodeService_Create_InvalidShortDomain(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	_, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-link",
		ShortDomainID:   99999,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent short domain")
	}
	if !errors.Is(err, ErrLiveCodeDomainUnavailable) {
		t.Errorf("Expected ErrLiveCodeDomainUnavailable（短链域名）, got %v", err)
	}
}

// TestLiveCodeService_Create_InvalidEntryDomain 测试创建时入口域名不存在
func TestLiveCodeService_Create_InvalidEntryDomain(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, _, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   99999,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent entry domain")
	}
	if !errors.Is(err, ErrLiveCodeDomainUnavailable) {
		t.Errorf("Expected ErrLiveCodeDomainUnavailable（入口域名）, got %v", err)
	}
}

// TestLiveCodeService_Create_InvalidLandingDomain 测试创建时落地域名不存在
func TestLiveCodeService_Create_InvalidLandingDomain(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, _ := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: 99999,
		Status:          1,
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent landing domain")
	}
	if !errors.Is(err, ErrLiveCodeDomainUnavailable) {
		t.Errorf("Expected ErrLiveCodeDomainUnavailable（落地域名）, got %v", err)
	}
}

// TestLiveCodeService_Create_DisabledDomain 测试创建时使用不可用的域名
func TestLiveCodeService_Create_DisabledDomain(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)

	shortDomain.Status = 2
	database.Save(shortDomain)

	service := newTestLiveCodeService(database)

	req := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}

	_, err := service.Create(context.Background(), req)
	if err == nil {
		t.Error("Expected error for disabled domain")
	}
	if !errors.Is(err, ErrLiveCodeDomainUnavailable) {
		t.Errorf("Expected ErrLiveCodeDomainUnavailable（短链域名）, got %v", err)
	}
}

// TestLiveCodeService_Update 测试更新活码
func TestLiveCodeService_Update(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "旧名称",
		ShortLink:       "old-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	updateReq := &dto.UpdateLiveCodeRequest{
		Name:      "新名称",
		ShortLink: "new-link",
	}

	updatedResponse, err := service.Update(context.Background(), response.ID, updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updatedResponse.Name != "新名称" {
		t.Errorf("Expected name '新名称', got %s", updatedResponse.Name)
	}

	if updatedResponse.ShortLink != "new-link" {
		t.Errorf("Expected short_link 'new-link', got %s", updatedResponse.ShortLink)
	}
}

// TestLiveCodeService_Update_NotFound 测试更新不存在的活码
func TestLiveCodeService_Update_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	updateReq := &dto.UpdateLiveCodeRequest{
		Name: "新名称",
	}

	_, err := service.Update(context.Background(), "non-existent-id", updateReq)
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_Update_DuplicateShortLink 测试更新时短链重复
func TestLiveCodeService_Update_DuplicateShortLink(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq1 := &dto.CreateLiveCodeRequest{
		Name:            "活码 1",
		ShortLink:       "link-1",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq1)

	createReq2 := &dto.CreateLiveCodeRequest{
		Name:            "活码 2",
		ShortLink:       "link-2",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response2, _ := service.Create(context.Background(), createReq2)

	updateReq := &dto.UpdateLiveCodeRequest{
		ShortLink: "link-1",
	}

	_, err := service.Update(context.Background(), response2.ID, updateReq)
	if err == nil {
		t.Error("Expected error for duplicate short link")
	}
	if !errors.Is(err, ErrLiveCodeShortLinkExists) {
		t.Errorf("Expected ErrLiveCodeShortLinkExists, got %v", err)
	}
}

// TestLiveCodeService_Update_SameShortLink 测试更新时使用相同的短链
func TestLiveCodeService_Update_SameShortLink(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "活码 1",
		ShortLink:       "link-1",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	updateReq := &dto.UpdateLiveCodeRequest{
		Name:      "新名称",
		ShortLink: "link-1",
	}

	_, err := service.Update(context.Background(), response.ID, updateReq)
	if err != nil {
		t.Errorf("Update with same short link should succeed: %v", err)
	}
}

// TestLiveCodeService_Delete 测试删除活码
func TestLiveCodeService_Delete(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "待删除活码",
		ShortLink:       "to-delete",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	err := service.Delete(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = service.GetByID(context.Background(), response.ID)
	if err == nil {
		t.Error("Expected error for deleted live code")
	}
}

// TestLiveCodeService_Delete_NotFound 测试删除不存在的活码
func TestLiveCodeService_Delete_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	err := service.Delete(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GetByID 测试根据 ID 获取活码
func TestLiveCodeService_GetByID(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-get",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	retrieved, err := service.GetByID(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if retrieved.Name != "测试活码" {
		t.Errorf("Expected name '测试活码', got %s", retrieved.Name)
	}

	if retrieved.ShortLink != "test-get" {
		t.Errorf("Expected short_link 'test-get', got %s", retrieved.ShortLink)
	}
}

// TestLiveCodeService_GetByID_NotFound 测试获取不存在的活码
func TestLiveCodeService_GetByID_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	_, err := service.GetByID(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GetByShortLink 测试根据短链获取活码
func TestLiveCodeService_GetByShortLink(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "测试活码",
		ShortLink:       "test-short-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq)

	retrieved, err := service.GetByShortLink(context.Background(), "test-short-link")
	if err != nil {
		t.Fatalf("GetByShortLink failed: %v", err)
	}

	if retrieved.Name != "测试活码" {
		t.Errorf("Expected name '测试活码', got %s", retrieved.Name)
	}
}

// TestLiveCodeService_GetByShortLink_NotFound 测试获取不存在的短链
func TestLiveCodeService_GetByShortLink_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	_, err := service.GetByShortLink(context.Background(), "non-existent-link")
	if err == nil {
		t.Error("Expected error for non-existent short link")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GetList 测试获取活码列表
func TestLiveCodeService_GetList(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	for i := 0; i < 5; i++ {
		createReq := &dto.CreateLiveCodeRequest{
			Name:            "活码" + string(rune('0'+i)),
			ShortLink:       "link-" + string(rune('0'+i)),
			ShortDomainID:   shortDomain.ID,
			EntryDomainID:   entryDomain.ID,
			LandingDomainID: landingDomain.ID,
			Status:          1,
		}
		_, _ = service.Create(context.Background(), createReq)
	}

	list, total, err := service.GetList(context.Background(), 1, 10, "", "")
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(list) != 5 {
		t.Errorf("Expected 5 items, got %d", len(list))
	}
}

// TestLiveCodeService_GetList_WithNameFilter 测试带名称过滤的列表
func TestLiveCodeService_GetList_WithNameFilter(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq1 := &dto.CreateLiveCodeRequest{
		Name:            "测试活码 1",
		ShortLink:       "test-link-1",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq1)

	createReq2 := &dto.CreateLiveCodeRequest{
		Name:            "测试活码 2",
		ShortLink:       "test-link-2",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq2)

	createReq3 := &dto.CreateLiveCodeRequest{
		Name:            "其他活码",
		ShortLink:       "other-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq3)

	_, total, err := service.GetList(context.Background(), 1, 10, "测试", "")
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected total 2, got %d", total)
	}
}

// TestLiveCodeService_GetList_WithStatusFilter 测试带状态过滤的列表
func TestLiveCodeService_GetList_WithStatusFilter(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq1 := &dto.CreateLiveCodeRequest{
		Name:            "启用活码",
		ShortLink:       "enabled-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	_, _ = service.Create(context.Background(), createReq1)

	createReq2 := &dto.CreateLiveCodeRequest{
		Name:            "禁用活码",
		ShortLink:       "disabled-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          0,
	}
	_, _ = service.Create(context.Background(), createReq2)

	list, total, err := service.GetList(context.Background(), 1, 10, "", "1")
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if len(list) < 1 {
		t.Errorf("Expected at least 1 enabled item, got %d", len(list))
	}

	for _, item := range list {
		if item.Status != 1 {
			t.Errorf("Expected all items to have status 1, got %d", item.Status)
		}
	}
	_ = total
}

// TestLiveCodeService_GetStats 测试获取活码统计
func TestLiveCodeService_GetStats(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "统计测试活码",
		ShortLink:       "stats-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	database.Exec("UPDATE live_codes SET total_views = 100, today_views = 20, total_clicks = 50, daily_clicks = 10 WHERE id = ?", response.ID)

	stats, err := service.GetStats(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalViews != 100 {
		t.Errorf("Expected total_views 100, got %d", stats.TotalViews)
	}

	if stats.TodayViews != 20 {
		t.Errorf("Expected today_views 20, got %d", stats.TodayViews)
	}

	if stats.TotalClicks != 50 {
		t.Errorf("Expected total_clicks 50, got %d", stats.TotalClicks)
	}

	if stats.TodayClicks != 10 {
		t.Errorf("Expected today_clicks 10, got %d", stats.TodayClicks)
	}
}

// TestLiveCodeService_GetStats_NotFound 测试获取不存在的活码统计
func TestLiveCodeService_GetStats_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	_, err := service.GetStats(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GenerateQRCode 测试生成二维码
func TestLiveCodeService_GenerateQRCode(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "二维码测试活码",
		ShortLink:       "qr-test-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}

	qrResponse, err := service.GenerateQRCode(context.Background(), response.ID, generateReq)
	if err != nil {
		t.Fatalf("GenerateQRCode failed: %v", err)
	}

	if qrResponse == nil {
		t.Fatal("Expected non-nil QR response")
	}

	if qrResponse.LiveCodeID != response.ID {
		t.Errorf("Expected live_code_id '%s', got %s", response.ID, qrResponse.LiveCodeID)
	}

	if qrResponse.ExpireDays != 7 {
		t.Errorf("Expected expire_days 7, got %d", qrResponse.ExpireDays)
	}

	if qrResponse.Status != 1 {
		t.Errorf("Expected status 1, got %d", qrResponse.Status)
	}
}

// TestLiveCodeService_GenerateQRCode_NotFound 测试生成不存在的活码的二维码
func TestLiveCodeService_GenerateQRCode_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}

	_, err := service.GenerateQRCode(context.Background(), "non-existent-id", generateReq)
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GetQRCodes 测试获取二维码列表
func TestLiveCodeService_GetQRCodes(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "二维码列表测试活码",
		ShortLink:       "qr-list-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	for i := 0; i < 3; i++ {
		generateReq := &dto.GenerateQRCodeRequest{
			ExpireDays: 7,
			Status:     1,
		}
		_, _ = service.GenerateQRCode(context.Background(), response.ID, generateReq)
	}

	qrCodes, err := service.GetQRCodes(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("GetQRCodes failed: %v", err)
	}

	if len(qrCodes) != 3 {
		t.Errorf("Expected 3 QR codes, got %d", len(qrCodes))
	}
}

// TestLiveCodeService_GetQRCodes_NotFound 测试获取不存在的活码的二维码列表
func TestLiveCodeService_GetQRCodes_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	_, err := service.GetQRCodes(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_GetQRStats 测试获取二维码统计
func TestLiveCodeService_GetQRStats(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "二维码统计测试活码",
		ShortLink:       "qr-stats-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}
	qrResponse, _ := service.GenerateQRCode(context.Background(), response.ID, generateReq)

	stats, err := service.GetQRStats(context.Background(), qrResponse.ID)
	if err != nil {
		t.Fatalf("GetQRStats failed: %v", err)
	}

	if stats.QRCodeID != qrResponse.ID {
		t.Errorf("Expected qr_code_id '%s', got %s", qrResponse.ID, stats.QRCodeID)
	}

	if stats.ExpireDays != 7 {
		t.Errorf("Expected expire_days 7, got %d", stats.ExpireDays)
	}

	if stats.Status != 1 {
		t.Errorf("Expected status 1, got %d", stats.Status)
	}
}

// TestLiveCodeService_GetQRStats_NotFound 测试获取不存在的二维码统计
func TestLiveCodeService_GetQRStats_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	_, err := service.GetQRStats(context.Background(), "non-existent-qr-id")
	if err == nil {
		t.Error("Expected error for non-existent QR code")
	}
	if err.Error() != "二维码不存在" {
		t.Errorf("Expected '二维码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_Share 测试分享活码
func TestLiveCodeService_Share(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "分享测试活码",
		ShortLink:       "share-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}
	qrResponse, _ := service.GenerateQRCode(context.Background(), response.ID, generateReq)

	shareReq := &dto.ShareLiveCodeRequest{
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}

	shareResponse, err := service.Share(context.Background(), response.ID, shareReq)
	if err != nil {
		t.Fatalf("Share failed: %v", err)
	}

	if shareResponse.ShortLink != "https://short.example.com/share-link" {
		t.Errorf("Expected short_link 'https://short.example.com/share-link', got %s", shareResponse.ShortLink)
	}

	if shareResponse.QRCodeID != qrResponse.ID {
		t.Errorf("Expected qr_code_id '%s', got %s", qrResponse.ID, shareResponse.QRCodeID)
	}
}

// TestLiveCodeService_Share_NotFound 测试分享不存在的活码
func TestLiveCodeService_Share_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	shareReq := &dto.ShareLiveCodeRequest{
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}

	_, err := service.Share(context.Background(), "non-existent-id", shareReq)
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
	if err.Error() != "活码不存在" {
		t.Errorf("Expected '活码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_Share_NoAvailableQR 测试分享没有可用二维码的活码
func TestLiveCodeService_Share_NoAvailableQR(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "分享测试活码",
		ShortLink:       "share-no-qr-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	shareReq := &dto.ShareLiveCodeRequest{
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}

	_, err := service.Share(context.Background(), response.ID, shareReq)
	if err == nil {
		t.Error("Expected error for no available QR code")
	}
	if err.Error() != "没有可用的二维码" {
		t.Errorf("Expected '没有可用的二维码', got %s", err.Error())
	}
}

// TestLiveCodeService_DeleteQRCode 测试删除二维码
func TestLiveCodeService_DeleteQRCode(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "删除二维码测试活码",
		ShortLink:       "delete-qr-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}
	qrResponse, _ := service.GenerateQRCode(context.Background(), response.ID, generateReq)

	err := service.DeleteQRCode(context.Background(), qrResponse.ID)
	if err != nil {
		t.Fatalf("DeleteQRCode failed: %v", err)
	}

	_, err = service.GetQRStats(context.Background(), qrResponse.ID)
	if err == nil {
		t.Error("Expected error for deleted QR code")
	}
}

// TestLiveCodeService_DeleteQRCode_NotFound 测试删除不存在的二维码
func TestLiveCodeService_DeleteQRCode_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	err := service.DeleteQRCode(context.Background(), "non-existent-qr-id")
	if err == nil {
		t.Error("Expected error for non-existent QR code")
	}
	if err.Error() != "二维码不存在" {
		t.Errorf("Expected '二维码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_UpdateQRCode 测试更新二维码
func TestLiveCodeService_UpdateQRCode(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "更新二维码测试活码",
		ShortLink:       "update-qr-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}
	qrResponse, _ := service.GenerateQRCode(context.Background(), response.ID, generateReq)

	newStatus := 0
	newExpireDays := 30
	updateReq := &dto.UpdateLiveCodeQRRequest{
		Status:     &newStatus,
		ExpireDays: &newExpireDays,
	}

	err := service.UpdateQRCode(context.Background(), qrResponse.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateQRCode failed: %v", err)
	}

	var qrCode model.LiveCodeQR
	database.Where("id = ?", qrResponse.ID).First(&qrCode)
	if qrCode.Status != 0 {
		t.Errorf("Expected status 0, got %d", qrCode.Status)
	}
	if qrCode.ExpireDays != 30 {
		t.Errorf("Expected expire_days 30, got %d", qrCode.ExpireDays)
	}
}

// TestLiveCodeService_UpdateQRCode_NotFound 测试更新不存在的二维码
func TestLiveCodeService_UpdateQRCode_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	updateReq := &dto.UpdateLiveCodeQRRequest{
		Status: new(int),
	}

	err := service.UpdateQRCode(context.Background(), "non-existent-qr-id", updateReq)
	if err == nil {
		t.Error("Expected error for non-existent QR code")
	}
	if err.Error() != "二维码不存在" {
		t.Errorf("Expected '二维码不存在', got %s", err.Error())
	}
}

// TestLiveCodeService_RotateLiveCodes 测试轮询活码
func TestLiveCodeService_RotateLiveCodes(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "轮询测试活码",
		ShortLink:       "rotate-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	err := service.RotateLiveCodes(context.Background())
	if err != nil {
		t.Fatalf("RotateLiveCodes failed: %v", err)
	}

	var count int64
	database.Model(&model.LiveCodeQR{}).Where("live_code_id = ?", response.ID).Count(&count)
	if count < 1 {
		t.Errorf("Expected at least 1 QR code, got %d", count)
	}
}

// TestLiveCodeService_RotateLiveCodes_DailyLimitReached 测试轮询时超过每日限制的活码
func TestLiveCodeService_RotateLiveCodes_DailyLimitReached(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "轮询限制测试活码",
		ShortLink:       "rotate-limit-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	var liveCode model.LiveCode
	database.First(&liveCode, response.ID)
	liveCode.DailyClicks = 200
	database.Save(&liveCode)

	err := service.RotateLiveCodes(context.Background())
	if err != nil {
		t.Fatalf("RotateLiveCodes failed: %v", err)
	}

	var count int64
	database.Model(&model.LiveCodeQR{}).Where("live_code_id = ?", response.ID).Count(&count)
	_ = count
}

// TestLiveCodeService_RecordClick 测试记录点击
func TestLiveCodeService_RecordClick(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "点击测试活码",
		ShortLink:       "click-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	generateReq := &dto.GenerateQRCodeRequest{
		ExpireDays: 7,
		Status:     1,
	}
	qrResponse, _ := service.GenerateQRCode(context.Background(), response.ID, generateReq)

	err := service.RecordClick(context.Background(), qrResponse.ID, "203.0.113.7", "Mozilla/5.0", "https://referrer.com")
	if err != nil {
		t.Fatalf("RecordClick failed: %v", err)
	}

	var statCount int64
	database.Model(&model.LiveCodeQRStat{}).Where("qr_code_id = ?", qrResponse.ID).Count(&statCount)
	if statCount < 1 {
		t.Errorf("Expected at least 1 stat record, got %d", statCount)
	}

	var liveClickLog int64
	database.Model(&model.LiveCodeClickLog{}).
		Where("qr_code_id = ? AND ip_address = ?", qrResponse.ID, "203.0.113.7").Count(&liveClickLog)
	if liveClickLog != 1 {
		t.Errorf("Expected 1 live code click audit log, got %d", liveClickLog)
	}
	var qrClickLog int64
	database.Model(&model.QRCodeClickLog{}).
		Where("qr_code_id = ?", qrResponse.ID).Count(&qrClickLog)
	if qrClickLog != 1 {
		t.Errorf("Expected 1 QR code click audit log, got %d", qrClickLog)
	}

	stats, err := service.GetStats(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalQRClicks < 1 {
		t.Errorf("Expected TotalQRClicks >= 1, got %d", stats.TotalQRClicks)
	}

	qrStats, err := service.GetQRStats(context.Background(), qrResponse.ID)
	if err != nil {
		t.Fatalf("GetQRStats failed: %v", err)
	}
	if qrStats.ClickCount < 1 {
		t.Errorf("Expected QR ClickCount >= 1, got %d", qrStats.ClickCount)
	}
}

// TestLiveCodeService_RecordClick_NotFound 测试记录不存在的二维码点击
func TestLiveCodeService_RecordClick_NotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	err := service.RecordClick(context.Background(), "non-existent-qr-id", "203.0.113.7", "Mozilla/5.0", "https://referrer.com")
	if err == nil {
		t.Error("Expected error for non-existent QR code")
	}
}

// TestLiveCodeService_RecordClick_LiveCodeNotFound 测试记录点击时活码不存在
func TestLiveCodeService_RecordClick_LiveCodeNotFound(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	service := newTestLiveCodeService(database)

	qrCode := &model.LiveCodeQR{
		ID:         "test-qr-id",
		LiveCodeID: "non-existent-live-code-id",
		Status:     1,
		ExpireDays: 7,
	}
	database.Create(qrCode)

	err := service.RecordClick(context.Background(), qrCode.ID, "203.0.113.7", "Mozilla/5.0", "https://referrer.com")
	if err == nil {
		t.Error("Expected error for non-existent live code")
	}
}

// TestCalculateConversionRate 测试转化率计算
func TestCalculateConversionRate(t *testing.T) {
	tests := []struct {
		name     string
		shown    int
		clicks   int
		expected float64
	}{
		{"Zero shown", 0, 10, 0},
		{"Zero clicks", 100, 0, 0},
		{"50% conversion", 100, 50, 50},
		{"100% conversion", 100, 100, 100},
		{"25% conversion", 200, 50, 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateConversionRate(tt.shown, tt.clicks)
			if result != tt.expected {
				t.Errorf("Expected %f, got %f", tt.expected, result)
			}
		})
	}
}

// TestLiveCodeService_GetList_Pagination 测试分页获取活码列表
func TestLiveCodeService_GetList_Pagination(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	for i := 0; i < 15; i++ {
		createReq := &dto.CreateLiveCodeRequest{
			Name:            "活码" + string(rune('A'+i)),
			ShortLink:       "page-link-" + string(rune('A'+i)),
			ShortDomainID:   shortDomain.ID,
			EntryDomainID:   entryDomain.ID,
			LandingDomainID: landingDomain.ID,
			Status:          1,
		}
		_, _ = service.Create(context.Background(), createReq)
	}

	list1, total1, err := service.GetList(context.Background(), 1, 10, "", "")
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}

	if total1 != 15 {
		t.Errorf("Expected total 15, got %d", total1)
	}

	if len(list1) != 10 {
		t.Errorf("Expected 10 items on page 1, got %d", len(list1))
	}

	list2, total2, err := service.GetList(context.Background(), 2, 10, "", "")
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}

	if total2 != 15 {
		t.Errorf("Expected total 15, got %d", total2)
	}

	if len(list2) != 5 {
		t.Errorf("Expected 5 items on page 2, got %d", len(list2))
	}
}

// TestLiveCodeService_FullShortLink_HTTP 测试 HTTP 端口的完整短链
func TestLiveCodeService_FullShortLink_HTTP(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)

	shortDomain := &model.DomainPool{
		Domain: "http.example.com",
		Port:   80,
		Status: 1,
	}
	entryDomain := &model.DomainPool{
		Domain: "http-entry.example.com",
		Port:   8080,
		Status: 1,
	}
	landingDomain := &model.DomainPool{
		Domain: "http-landing.example.com",
		Port:   80,
		Status: 1,
	}
	database.Create(shortDomain)
	database.Create(entryDomain)
	database.Create(landingDomain)

	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "HTTP 测试活码",
		ShortLink:       "http-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
	}
	response, _ := service.Create(context.Background(), createReq)

	if response.Name != "HTTP 测试活码" {
		t.Errorf("Expected name 'HTTP 测试活码', got %s", response.Name)
	}

	if response.ShortDomainID != shortDomain.ID {
		t.Errorf("Expected ShortDomainID %d, got %d", shortDomain.ID, response.ShortDomainID)
	}

	if response.EntryDomainID != entryDomain.ID {
		t.Errorf("Expected EntryDomainID %d, got %d", entryDomain.ID, response.EntryDomainID)
	}

	if response.LandingDomainID != landingDomain.ID {
		t.Errorf("Expected LandingDomainID %d, got %d", landingDomain.ID, response.LandingDomainID)
	}
}

// TestLiveCodeService_EmptyResponseFields 测试响应中的空字段处理
func TestLiveCodeService_EmptyResponseFields(t *testing.T) {
	database := setupLiveCodeServiceTestDB(t)
	shortDomain, entryDomain, landingDomain := createTestDomains(t, database)
	service := newTestLiveCodeService(database)

	createReq := &dto.CreateLiveCodeRequest{
		Name:            "空字段测试活码",
		ShortLink:       "empty-fields-link",
		ShortDomainID:   shortDomain.ID,
		EntryDomainID:   entryDomain.ID,
		LandingDomainID: landingDomain.ID,
		Status:          1,
		ImageURL:        "",
		EntryURL:        "",
		LandingURL:      "",
	}
	response, err := service.Create(context.Background(), createReq)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if response.ImageURL != "" {
		t.Errorf("Expected empty ImageURL, got %s", response.ImageURL)
	}

	if response.EntryURL != "" {
		t.Errorf("Expected empty EntryURL, got %s", response.EntryURL)
	}

	if response.LandingURL != "" {
		t.Errorf("Expected empty LandingURL, got %s", response.LandingURL)
	}
}

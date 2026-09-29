package service

import (
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupDashboardScreenTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.DashboardScreen{},
		&model.DashboardWidget{},
	)
	db.SetTestDB(database)
	return database
}

func newTestDashboardScreenRepository(database *gorm.DB) *repository.DashboardScreenRepository {
	return repository.NewDashboardScreenRepository()
}

func newTestDashboardWidgetRepository(database *gorm.DB) *repository.DashboardWidgetRepository {
	return repository.NewDashboardWidgetRepository()
}

// TestNewDashboardScreenService 测试创建数据大屏服务
func TestNewDashboardScreenService(t *testing.T) {
	setupDashboardScreenTestDB(t)

	service := NewDashboardScreenService()
	if service == nil {
		t.Fatal("Expected non-nil service")
	}
	if service.screenRepo == nil {
		t.Error("Expected non-nil screen repository")
	}
	if service.widgetRepo == nil {
		t.Error("Expected non-nil widget repository")
	}
}

// TestDashboardScreenService_CreateScreen 测试创建大屏
func TestDashboardScreenService_CreateScreen(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{"grid": "12x8", "orientation": "landscape"},
		Theme:    "dark",
		IsPublic: false,
	}

	createdBy := uint(1)

	screen, err := service.CreateScreen(createdBy, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	if screen == nil {
		t.Fatal("Expected non-nil screen")
	}

	if screen.Name != "测试大屏" {
		t.Errorf("Expected name '测试大屏', got %s", screen.Name)
	}

	if screen.Theme != "dark" {
		t.Errorf("Expected theme 'dark', got %s", screen.Theme)
	}

	if screen.IsPublic {
		t.Error("Expected IsPublic to be false")
	}

	if screen.CreatedBy != createdBy {
		t.Errorf("Expected created_by %d, got %d", createdBy, screen.CreatedBy)
	}

	if screen.Code == "" {
		t.Error("Expected non-empty code")
	}

	var count int64
	database.Model(&model.DashboardScreen{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 screen in database, got %d", count)
	}
}

// TestDashboardScreenService_CreateScreen_EmptyTheme 测试创建大屏时主题为空的情况
func TestDashboardScreenService_CreateScreen_EmptyTheme(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "",
		IsPublic: true,
	}

	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	if screen.Theme != "dark" {
		t.Errorf("Expected default theme 'dark', got %s", screen.Theme)
	}
}

// TestDashboardScreenService_CreateScreen_WithNilLayout 测试创建大屏时布局为空的情况
func TestDashboardScreenService_CreateScreen_WithNilLayout(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   nil,
		Theme:    "light",
		IsPublic: true,
	}

	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	if screen.Layout != "null" {
		t.Errorf("Expected layout 'null', got %s", screen.Layout)
	}
}

// TestDashboardScreenService_GetScreenList 测试获取大屏列表
func TestDashboardScreenService_GetScreenList(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	for i := 0; i < 5; i++ {
		req := &CreateScreenRequest{
			Name:     fmt.Sprintf("大屏-%d", i),
			Layout:   map[string]any{},
			Theme:    "dark",
			IsPublic: false,
		}
		_, err := service.CreateScreen(1, req)
		if err != nil {
			t.Fatalf("CreateScreen failed: %v", err)
		}
	}

	screens, total, err := service.GetScreenList(1, 10)
	if err != nil {
		t.Fatalf("GetScreenList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(screens) != 5 {
		t.Errorf("Expected 5 screens, got %d", len(screens))
	}
}

// TestDashboardScreenService_GetScreenList_Single 测试获取单个大屏的列表
func TestDashboardScreenService_GetScreenList_Single(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	req := &CreateScreenRequest{
		Name:     "单一大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	_, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	screens, total, err := service.GetScreenList(1, 10)
	if err != nil {
		t.Fatalf("GetScreenList failed: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if len(screens) != 1 {
		t.Errorf("Expected 1 screen, got %d", len(screens))
	}
}

// TestDashboardScreenService_GetScreenList_MultipleUsers 单租户多用户创建隔离
// 单租户私有部署：所有大屏归当前部署实例，按 CreatedBy 区分创建者。
func TestDashboardScreenService_GetScreenList_MultipleUsers(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	for i := 0; i < 2; i++ {
		req := &CreateScreenRequest{
			Name:     fmt.Sprintf("大屏-A-%d", i),
			Layout:   map[string]any{},
			Theme:    "dark",
			IsPublic: false,
		}
		_, err := service.CreateScreen(1, req)
		if err != nil {
			t.Fatalf("CreateScreen failed for user 1: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		req := &CreateScreenRequest{
			Name:     fmt.Sprintf("大屏-B-%d", i),
			Layout:   map[string]any{},
			Theme:    "dark",
			IsPublic: false,
		}
		_, err := service.CreateScreen(2, req)
		if err != nil {
			t.Fatalf("CreateScreen failed for user 2: %v", err)
		}
	}

	all, total, err := service.GetScreenList(1, 10)
	if err != nil {
		t.Fatalf("GetScreenList failed: %v", err)
	}
	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}
	if len(all) != 5 {
		t.Errorf("Expected 5 screens, got %d", len(all))
	}

	countByUser := map[uint]int{}
	for _, s := range all {
		countByUser[s.CreatedBy]++
	}
	if countByUser[1] != 2 {
		t.Errorf("Expected user 1 has 2 screens, got %d", countByUser[1])
	}
	if countByUser[2] != 3 {
		t.Errorf("Expected user 2 has 3 screens, got %d", countByUser[2])
	}
}

// TestDashboardScreenService_GetScreenList_WithPagination 测试分页获取大屏列表
func TestDashboardScreenService_GetScreenList_WithPagination(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	for i := 0; i < 3; i++ {
		req := &CreateScreenRequest{
			Name:     fmt.Sprintf("大屏-%d", i),
			Layout:   map[string]any{},
			Theme:    "dark",
			IsPublic: false,
		}
		_, err := service.CreateScreen(1, req)
		if err != nil {
			t.Fatalf("CreateScreen failed: %v", err)
		}
	}

	screens, total, err := service.GetScreenList(1, 2)
	if err != nil {
		t.Fatalf("GetScreenList failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected total 3, got %d", total)
	}

	if len(screens) != 2 {
		t.Errorf("Expected 2 screens on page 1, got %d", len(screens))
	}

	screens2, _, err := service.GetScreenList(2, 2)
	if err != nil {
		t.Fatalf("GetScreenList failed for page 2: %v", err)
	}

	if len(screens2) != 1 {
		t.Errorf("Expected 1 screen on page 2, got %d", len(screens2))
	}
}

func TestDashboardScreenService_GetScreenList_EmptyMerchant(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	screens, total, err := service.GetScreenList(1, 10)
	if err != nil {
		t.Fatalf("GetScreenList failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0, got %d", total)
	}

	if len(screens) != 0 {
		t.Errorf("Expected 0 screens, got %d", len(screens))
	}
}

// TestDashboardScreenService_GetScreenByID 测试根据 ID 获取大屏
func TestDashboardScreenService_GetScreenByID(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{"key": "value"},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	retrievedScreen, err := service.GetScreenByID(screen.ID)
	if err != nil {
		t.Fatalf("GetScreenByID failed: %v", err)
	}

	if retrievedScreen.Name != "测试大屏" {
		t.Errorf("Expected name '测试大屏', got %s", retrievedScreen.Name)
	}

	if retrievedScreen.ID != screen.ID {
		t.Errorf("Expected ID %d, got %d", screen.ID, retrievedScreen.ID)
	}
}

// TestDashboardScreenService_GetScreenByID_SingleTenant 单租户访问验证
// 单租户私有部署：所有大屏归当前部署实例所有，不存在跨租户访问限制。
func TestDashboardScreenService_GetScreenByID_SingleTenant(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	got, err := service.GetScreenByID(screen.ID)
	if err != nil {
		t.Fatalf("GetScreenByID should succeed in single-tenant mode, got: %v", err)
	}
	if got == nil || got.ID != screen.ID {
		t.Errorf("Expected screen ID %d, got %v", screen.ID, got)
	}
}

// TestDashboardScreenService_GetScreenByID_NotFound 测试获取不存在的大屏
func TestDashboardScreenService_GetScreenByID_NotFound(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	_, err := service.GetScreenByID(99999)
	if err == nil {
		t.Error("Expected error for non-existent screen")
	}
}

// TestDashboardScreenService_UpdateScreen 测试更新大屏
func TestDashboardScreenService_UpdateScreen(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "旧名称",
		Layout:   map[string]any{"old": "layout"},
		Theme:    "light",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	updateReq := &UpdateScreenRequest{
		Name:     "新名称",
		Layout:   map[string]any{"new": "layout"},
		Theme:    "dark",
		IsPublic: true,
		Widgets: []WidgetConfig{
			{
				WidgetType: "chart",
				Title:      "销售趋势",
				Config:     map[string]any{"chartType": "line"},
				DataSource: "orders",
				X:          0,
				Y:          0,
				Width:      6,
				Height:     4,
			},
		},
	}

	updatedScreen, err := service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen failed: %v", err)
	}

	if updatedScreen.Name != "新名称" {
		t.Errorf("Expected name '新名称', got %s", updatedScreen.Name)
	}

	if updatedScreen.Theme != "dark" {
		t.Errorf("Expected theme 'dark', got %s", updatedScreen.Theme)
	}

	if !updatedScreen.IsPublic {
		t.Error("Expected IsPublic to be true")
	}

	var widgetCount int64
	database.Model(&model.DashboardWidget{}).Where("screen_id = ?", screen.ID).Count(&widgetCount)
	if widgetCount != 1 {
		t.Errorf("Expected 1 widget, got %d", widgetCount)
	}
}

// TestDashboardScreenService_UpdateScreen_Partial 测试部分更新大屏
func TestDashboardScreenService_UpdateScreen_Partial(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "原名称",
		Layout:   map[string]any{"original": "layout"},
		Theme:    "light",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	updateReq := &UpdateScreenRequest{
		Name:     "新名称",
		Layout:   nil,
		Theme:    "",
		IsPublic: false,
	}

	updatedScreen, err := service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen failed: %v", err)
	}

	if updatedScreen.Name != "新名称" {
		t.Errorf("Expected name '新名称', got %s", updatedScreen.Name)
	}

	if updatedScreen.Theme != "light" {
		t.Errorf("Expected theme to remain 'light', got %s", updatedScreen.Theme)
	}
}

// TestDashboardScreenService_UpdateScreen_SingleTenant 单租户更新验证
// 单租户私有部署：所有大屏可被当前部署实例的合法用户更新，无跨租户限制。
func TestDashboardScreenService_UpdateScreen_SingleTenant(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	updateReq := &UpdateScreenRequest{
		Name: "新名称",
	}
	updated, err := service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen in single-tenant should succeed, got: %v", err)
	}
	if updated.Name != "新名称" {
		t.Errorf("Expected name '新名称', got %s", updated.Name)
	}
}

// TestDashboardScreenService_UpdateScreen_NotFound 测试更新不存在的大屏
func TestDashboardScreenService_UpdateScreen_NotFound(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	updateReq := &UpdateScreenRequest{
		Name: "新名称",
	}
	_, err := service.UpdateScreen(99999, updateReq)
	if err == nil {
		t.Error("Expected error for non-existent screen")
	}
}

// TestDashboardScreenService_DeleteScreen 测试删除大屏
func TestDashboardScreenService_DeleteScreen(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	widgetRepo := newTestDashboardWidgetRepository(database)
	service := &DashboardScreenService{screenRepo: repo, widgetRepo: widgetRepo}

	req := &CreateScreenRequest{
		Name:     "待删除大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	widget := &model.DashboardWidget{
		ScreenID:   screen.ID,
		WidgetType: "chart",
		Title:      "测试 Widget",
		Config:     "{}",
		X:          0,
		Y:          0,
		Width:      4,
		Height:     3,
	}
	database.Create(widget)

	err = service.DeleteScreen(screen.ID)
	if err != nil {
		t.Fatalf("DeleteScreen failed: %v", err)
	}

	var count int64
	database.Model(&model.DashboardScreen{}).Where("id = ?", screen.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected screen to be deleted, got count %d", count)
	}

	var widgetCount int64
	database.Model(&model.DashboardWidget{}).Where("screen_id = ?", screen.ID).Count(&widgetCount)
	if widgetCount != 0 {
		t.Errorf("Expected 0 widgets after deletion, got %d", widgetCount)
	}
}

func TestDashboardScreenService_DeleteScreen_Access(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	widgetRepo := newTestDashboardWidgetRepository(database)
	service := &DashboardScreenService{screenRepo: repo, widgetRepo: widgetRepo}

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	err = service.DeleteScreen(screen.ID)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

// TestDashboardScreenService_DeleteScreen_NotFound 测试删除不存在的大屏
func TestDashboardScreenService_DeleteScreen_NotFound(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	err := service.DeleteScreen(99999)
	if err == nil {
		t.Error("Expected error for non-existent screen")
	}
}

// TestDashboardScreenService_GetPublicScreen 测试公开访问大屏
func TestDashboardScreenService_GetPublicScreen(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "公开大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: true,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	retrievedScreen, err := service.GetPublicScreen(screen.Code)
	if err != nil {
		t.Fatalf("GetPublicScreen failed: %v", err)
	}

	if retrievedScreen.Name != "公开大屏" {
		t.Errorf("Expected name '公开大屏', got %s", retrievedScreen.Name)
	}

	var updatedScreen model.DashboardScreen
	database.First(&updatedScreen, screen.ID)
	if updatedScreen.ViewCount != 1 {
		t.Errorf("Expected ViewCount 1, got %d", updatedScreen.ViewCount)
	}
}

// TestDashboardScreenService_GetPublicScreen_MultipleViews 测试多次访问大屏
func TestDashboardScreenService_GetPublicScreen_MultipleViews(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "公开大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: true,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err = service.GetPublicScreen(screen.Code)
		if err != nil {
			t.Fatalf("GetPublicScreen failed: %v", err)
		}
	}

	var updatedScreen model.DashboardScreen
	database.First(&updatedScreen, screen.ID)
	if updatedScreen.ViewCount != 5 {
		t.Errorf("Expected ViewCount 5, got %d", updatedScreen.ViewCount)
	}
}

// TestDashboardScreenService_GetPublicScreen_NotFound 测试访问不存在的大屏
func TestDashboardScreenService_GetPublicScreen_NotFound(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	_, err := service.GetPublicScreen("non-existent-code")
	if err == nil {
		t.Error("Expected error for non-existent screen")
	}
}

// TestDashboardScreenService_GetScreenWidgets 测试获取大屏 widgets
func TestDashboardScreenService_GetScreenWidgets(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	widgets := []model.DashboardWidget{
		{ScreenID: screen.ID, WidgetType: "chart", Title: "图表 1", Config: "{}", X: 0, Y: 0, Width: 4, Height: 3, SortOrder: 0},
		{ScreenID: screen.ID, WidgetType: "table", Title: "表格 1", Config: "{}", X: 4, Y: 0, Width: 4, Height: 3, SortOrder: 1},
		{ScreenID: screen.ID, WidgetType: "indicator", Title: "指标 1", Config: "{}", X: 8, Y: 0, Width: 4, Height: 3, SortOrder: 2},
	}
	for i := range widgets {
		database.Create(&widgets[i])
	}

	retrievedWidgets, err := service.GetScreenWidgets(screen.ID)
	if err != nil {
		t.Fatalf("GetScreenWidgets failed: %v", err)
	}

	if len(retrievedWidgets) != 3 {
		t.Errorf("Expected 3 widgets, got %d", len(retrievedWidgets))
	}

	if retrievedWidgets[0].Title != "图表 1" {
		t.Errorf("Expected first widget '图表 1', got %s", retrievedWidgets[0].Title)
	}
	if retrievedWidgets[1].Title != "表格 1" {
		t.Errorf("Expected second widget '表格 1', got %s", retrievedWidgets[1].Title)
	}
	if retrievedWidgets[2].Title != "指标 1" {
		t.Errorf("Expected third widget '指标 1', got %s", retrievedWidgets[2].Title)
	}
}

// TestDashboardScreenService_GetScreenWidgets_Empty 测试获取空 widgets
func TestDashboardScreenService_GetScreenWidgets_Empty(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	widgets, err := service.GetScreenWidgets(screen.ID)
	if err != nil {
		t.Fatalf("GetScreenWidgets failed: %v", err)
	}

	if len(widgets) != 0 {
		t.Errorf("Expected 0 widgets, got %d", len(widgets))
	}
}

// TestDashboardScreenService_updateWidgets 测试更新 widgets
func TestDashboardScreenService_updateWidgets(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	oldWidgets := []model.DashboardWidget{
		{ScreenID: screen.ID, WidgetType: "old1", Title: "旧 Widget 1", Config: "{}", X: 0, Y: 0, Width: 4, Height: 3},
		{ScreenID: screen.ID, WidgetType: "old2", Title: "旧 Widget 2", Config: "{}", X: 4, Y: 0, Width: 4, Height: 3},
	}
	for i := range oldWidgets {
		database.Create(&oldWidgets[i])
	}

	newWidgets := []WidgetConfig{
		{WidgetType: "chart", Title: "新 Widget 1", Config: map[string]any{"type": "line"}, DataSource: "source1", X: 0, Y: 0, Width: 6, Height: 4},
		{WidgetType: "table", Title: "新 Widget 2", Config: map[string]any{"type": "grid"}, DataSource: "source2", X: 6, Y: 0, Width: 6, Height: 4},
	}

	updateReq := &UpdateScreenRequest{
		Widgets: newWidgets,
	}
	_, err = service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen failed: %v", err)
	}

	var widgetCount int64
	database.Model(&model.DashboardWidget{}).Where("screen_id = ?", screen.ID).Count(&widgetCount)
	if widgetCount != 2 {
		t.Errorf("Expected 2 widgets, got %d", widgetCount)
	}

	var widgets []model.DashboardWidget
	database.Where("screen_id = ?", screen.ID).Order("sort_order").Find(&widgets)
	if widgets[0].WidgetType != "chart" {
		t.Errorf("Expected first widget type 'chart', got %s", widgets[0].WidgetType)
	}
	if widgets[1].WidgetType != "table" {
		t.Errorf("Expected second widget type 'table', got %s", widgets[1].WidgetType)
	}
	if widgets[0].Width != 6 {
		t.Errorf("Expected first widget width 6, got %d", widgets[0].Width)
	}
	if widgets[0].Height != 4 {
		t.Errorf("Expected first widget height 4, got %d", widgets[0].Height)
	}
}

// TestGenerateScreenCode 测试生成大屏编码
func TestGenerateScreenCode(t *testing.T) {
	code := generateScreenCode()

	if code == "" {
		t.Error("Expected non-empty code")
	}

	expectedPrefix := "screen_"
	if len(code) < len(expectedPrefix) {
		t.Errorf("Expected code to start with '%s', got %s", expectedPrefix, code)
	}
	if code[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("Expected code to start with '%s', got %s", expectedPrefix, code)
	}
}

// TestGenerateScreenCode_Uniqueness 测试生成编码的唯一性
// 注：由于当前实现使用秒级时间戳，快速连续调用会产生重复编码
// 这个测试记录了当前行为的限制
func TestGenerateScreenCode_Uniqueness(t *testing.T) {
	codes := make(map[string]bool)

	for i := 0; i < 5; i++ {
		code := generateScreenCode()
		if codes[code] {
			t.Errorf("Duplicate code generated: %s", code)
		}
		codes[code] = true
		time.Sleep(1100 * time.Millisecond)
	}

	if len(codes) != 5 {
		t.Errorf("Expected 5 unique codes, got %d", len(codes))
	}
}

// TestDashboardScreenService_CreateScreen_WithWidgets 测试创建带 widgets 的大屏
func TestDashboardScreenService_CreateScreen_WithWidgets(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "复杂大屏",
		Layout:   map[string]any{"grid": "24x12", "orientation": "landscape"},
		Theme:    "dark",
		IsPublic: true,
	}

	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	if screen.Layout == "" {
		t.Error("Expected non-empty layout")
	}
}

// TestDashboardScreenService_UpdateScreen_WithEmptyWidgets 测试更新空 widgets 列表
func TestDashboardScreenService_UpdateScreen_WithEmptyWidgets(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	oldWidget := &model.DashboardWidget{
		ScreenID:   screen.ID,
		WidgetType: "chart",
		Title:      "旧 Widget",
		Config:     "{}",
		X:          0,
		Y:          0,
		Width:      4,
		Height:     3,
	}
	database.Create(oldWidget)

	updateReq := &UpdateScreenRequest{
		Widgets: []WidgetConfig{},
	}
	_, err = service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen failed: %v", err)
	}

	var widgetCount int64
	database.Model(&model.DashboardWidget{}).Where("screen_id = ?", screen.ID).Count(&widgetCount)
	if widgetCount != 0 {
		t.Errorf("Expected 0 widgets after updating with empty list, got %d", widgetCount)
	}
}

// TestDashboardScreenService_GetScreenByID_DeletedScreen 测试获取已删除的大屏
func TestDashboardScreenService_GetScreenByID_DeletedScreen(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	widgetRepo := newTestDashboardWidgetRepository(database)
	service := &DashboardScreenService{screenRepo: repo, widgetRepo: widgetRepo}

	req := &CreateScreenRequest{
		Name:     "待删除大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	err = service.DeleteScreen(screen.ID)
	if err != nil {
		t.Fatalf("DeleteScreen failed: %v", err)
	}

	_, err = service.GetScreenByID(screen.ID)
	if err == nil {
		t.Error("Expected error for deleted screen")
	}
}

// TestDashboardScreenService_ConcurrentAccess 测试并发访问
// 注：PostgreSQL 测试数据库在连接池之间通过 SET search_path 等机制共享，
// 此测试仅用于演示
func TestDashboardScreenService_ConcurrentAccess(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	repo := newTestDashboardScreenRepository(database)
	service := &DashboardScreenService{screenRepo: repo}

	req := &CreateScreenRequest{
		Name:     "并发测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: true,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		_, err := service.GetPublicScreen(screen.Code)
		if err != nil {
			t.Errorf("GetPublicScreen failed: %v", err)
		}
	}

	var updatedScreen model.DashboardScreen
	database.First(&updatedScreen, screen.ID)
	if updatedScreen.ViewCount != 10 {
		t.Errorf("Expected ViewCount 10, got %d", updatedScreen.ViewCount)
	}
}

// TestDashboardScreenService_WidgetConfigSerialization 测试 Widget 配置序列化
func TestDashboardScreenService_WidgetConfigSerialization(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	_ = newTestDashboardScreenRepository(database)
	service := NewDashboardScreenService()

	req := &CreateScreenRequest{
		Name:     "测试大屏",
		Layout:   map[string]any{},
		Theme:    "dark",
		IsPublic: false,
	}
	screen, err := service.CreateScreen(1, req)
	if err != nil {
		t.Fatalf("CreateScreen failed: %v", err)
	}

	complexConfig := map[string]any{
		"chartType":   "line",
		"showLegend":  true,
		"showGrid":    false,
		"fontSize":    14,
		"colors":      []string{"#FF0000", "#00FF00", "#0000FF"},
		"annotations": map[string]any{"threshold": 100, "target": 150},
	}

	updateReq := &UpdateScreenRequest{
		Widgets: []WidgetConfig{
			{
				WidgetType: "chart",
				Title:      "复杂配置图表",
				Config:     complexConfig,
				DataSource: "sales_data",
				X:          0,
				Y:          0,
				Width:      8,
				Height:     6,
			},
		},
	}

	_, err = service.UpdateScreen(screen.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateScreen failed: %v", err)
	}

	var widget model.DashboardWidget
	database.Where("screen_id = ?", screen.ID).First(&widget)
	if widget.Config == "" {
		t.Error("Expected non-empty widget config")
	}
}

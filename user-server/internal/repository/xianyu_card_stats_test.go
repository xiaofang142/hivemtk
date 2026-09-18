package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupXianyuCardStatsTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.XianyuCard{},
		&model.XianyuCardActivity{},
	)
	db.SetTestDB(database)
	return database
}

func setupXianyuCardStatsRepository(t *testing.T) XianyuCardStatsRepository {
	setupXianyuCardStatsTestDB(t)
	return NewXianyuCardStatsRepository(db.GetDB())
}

// TestXianyuCardStatsRepository_RecordActivity 测试记录活动
func TestXianyuCardStatsRepository_RecordActivity(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		cardID       uint
		activityType string
		ip           string
		userAgent    string
		referer      string
		wantErr      bool
	}{
		{
			name:         "record view activity",
			cardID:       1,
			activityType: "view",
			ip:           "192.168.1.1",
			userAgent:    "Mozilla/5.0",
			referer:      "https://example.com",
			wantErr:      false,
		},
		{
			name:         "record click activity",
			cardID:       1,
			activityType: "click",
			ip:           "192.168.1.2",
			userAgent:    "Chrome/90.0",
			referer:      "https://google.com",
			wantErr:      false,
		},
		{
			name:         "record share activity",
			cardID:       1,
			activityType: "share",
			ip:           "192.168.1.3",
			userAgent:    "Safari/14.0",
			referer:      "",
			wantErr:      false,
		},
		{
			name:         "record activity without referer",
			cardID:       2,
			activityType: "view",
			ip:           "10.0.0.1",
			userAgent:    "iPhone Safari",
			referer:      "",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.RecordActivity(ctx, tt.cardID, tt.activityType, tt.ip, tt.userAgent, tt.referer)

			if (err != nil) != tt.wantErr {
				t.Errorf("RecordActivity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestXianyuCardStatsRepository_GetCardStats 测试获取卡片统计数据
func TestXianyuCardStatsRepository_GetCardStats(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card := &model.XianyuCard{
		Title:       "Test Card",
		Description: "Test description",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	for i := 1; i <= 5; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "view",
			IP:           "192.168.1." + string(rune('0'+i)),
			UserAgent:    "Mozilla/5.0",
		})
	}

	for i := 1; i <= 3; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "click",
			IP:           "192.168.1." + string(rune('0'+i)),
			UserAgent:    "Chrome/90.0",
		})
	}

	for i := 1; i <= 2; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "share",
			IP:           "192.168.1." + string(rune('0'+i)),
			UserAgent:    "Safari/14.0",
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetCardStats(ctx, card.ID, startDate, endDate)
	if err != nil {
		t.Errorf("GetCardStats() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}

	if stats.Views != 5 {
		t.Errorf("Expected 5 views, got %d", stats.Views)
	}

	if stats.Clicks != 3 {
		t.Errorf("Expected 3 clicks, got %d", stats.Clicks)
	}

	if stats.Shares != 2 {
		t.Errorf("Expected 2 shares, got %d", stats.Shares)
	}
}

// TestXianyuCardStatsRepository_GetCardStats_WithDateRange 测试日期范围筛选
func TestXianyuCardStatsRepository_GetCardStats_WithDateRange(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card := &model.XianyuCard{
		Title:       "Date Range Card",
		Description: "Test with date range",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	for i := 1; i <= 3; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "view",
			IP:           "192.168.1." + string(rune('0'+i)),
			CreatedAt:    time.Now(),
		})
	}

	oldDate := time.Now().AddDate(0, 0, -30)
	for i := 1; i <= 10; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "view",
			IP:           "192.168.2." + string(rune('0'+i)),
			CreatedAt:    oldDate,
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetCardStats(ctx, card.ID, startDate, endDate)
	if err != nil {
		t.Errorf("GetCardStats() error = %v", err)
	}

	if stats.Views != 3 {
		t.Errorf("Expected 3 views (recent only), got %d", stats.Views)
	}
}

// TestXianyuCardStatsRepository_GetCardStats_EmptyResult 测试空结果
func TestXianyuCardStatsRepository_GetCardStats_EmptyResult(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card := &model.XianyuCard{
		Title:       "Empty Stats Card",
		Description: "No activities",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetCardStats(ctx, card.ID, startDate, endDate)
	if err != nil {
		t.Errorf("GetCardStats() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected empty stats to be returned")
	}

	if stats.Views != 0 {
		t.Errorf("Expected 0 views, got %d", stats.Views)
	}
}

// TestXianyuCardStatsRepository_GetOverallStats 测试获取整体统计数据
func TestXianyuCardStatsRepository_GetOverallStats(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card1 := &model.XianyuCard{
		Title:       "Popular Card",
		Description: "Most viewed card",
		IsActive:    true,
	}
	db.GetDB().Create(card1)

	card2 := &model.XianyuCard{
		Title:       "Normal Card",
		Description: "Regular card",
		IsActive:    true,
	}
	db.GetDB().Create(card2)

	for i := 1; i <= 10; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card1.ID,
			ActivityType: "view",
			IP:           "192.168.1." + string(rune('0'+i%10)),
		})
	}

	for i := 1; i <= 5; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card1.ID,
			ActivityType: "click",
			IP:           "192.168.1." + string(rune('0'+i)),
		})
	}

	for i := 1; i <= 5; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card2.ID,
			ActivityType: "view",
			IP:           "192.168.2." + string(rune('0'+i)),
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetOverallStats(ctx, startDate, endDate)
	if err != nil {
		t.Errorf("GetOverallStats() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected overall stats to be returned")
	}

	if stats.TotalViewCount != 15 {
		t.Errorf("Expected 15 total views, got %d", stats.TotalViewCount)
	}

	if stats.TotalClickCount != 5 {
		t.Errorf("Expected 5 total clicks, got %d", stats.TotalClickCount)
	}

	if stats.TotalCards != 2 {
		t.Errorf("Expected 2 total cards, got %d", stats.TotalCards)
	}

	if stats.ActiveCards != 2 {
		t.Errorf("Expected 2 active cards, got %d", stats.ActiveCards)
	}
}

// TestXianyuCardStatsRepository_GetOverallStats_WithTopCards 测试获取热门卡片
func TestXianyuCardStatsRepository_GetOverallStats_WithTopCards(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		db.GetDB().Create(&model.XianyuCard{
			Title:       "Card " + string(rune('0'+i)),
			Description: "Description " + string(rune('0'+i)),
			IsActive:    true,
		})
	}

	for i := 1; i <= 100; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       1,
			ActivityType: "view",
			IP:           "192.168.1." + string(rune('0'+i%10)),
		})
	}

	for i := 1; i <= 50; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       2,
			ActivityType: "view",
			IP:           "192.168.2." + string(rune('0'+i%10)),
		})
	}

	for i := 1; i <= 10; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       3,
			ActivityType: "view",
			IP:           "192.168.3." + string(rune('0'+i%10)),
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetOverallStats(ctx, startDate, endDate)
	if err != nil {
		t.Errorf("GetOverallStats() error = %v", err)
	}

	if len(stats.TopCards) == 0 {
		t.Error("Expected top cards to be returned")
	}

	if len(stats.TopCards) > 0 && stats.TopCards[0].ID != 1 {
		t.Errorf("Expected most popular card to be card 1, got card %d", stats.TopCards[0].ID)
	}
}

// TestXianyuCardStatsRepository_GetOverallStats_EmptyResult 测试空整体统计
func TestXianyuCardStatsRepository_GetOverallStats_EmptyResult(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetOverallStats(ctx, startDate, endDate)
	if err != nil {
		t.Errorf("GetOverallStats() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected empty overall stats to be returned")
	}

	if stats.TotalViewCount != 0 {
		t.Errorf("Expected 0 total views, got %d", stats.TotalViewCount)
	}

	if stats.TotalCards != 0 {
		t.Errorf("Expected 0 total cards, got %d", stats.TotalCards)
	}
}

// TestXianyuCardStatsRepository_RecordActivity_WithDeviceType 测试记录活动包含设备信息
func TestXianyuCardStatsRepository_RecordActivity_WithDeviceType(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card := &model.XianyuCard{
		Title:       "Device Test Card",
		Description: "Test device tracking",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	repo.RecordActivity(ctx, card.ID, "view", "192.168.1.1", "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X)", "")
	repo.RecordActivity(ctx, card.ID, "view", "192.168.1.2", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "")
	repo.RecordActivity(ctx, card.ID, "view", "192.168.1.3", "Mozilla/5.0 (iPad; CPU OS 14_0 like Mac OS X)", "")

	stats, err := repo.GetCardStats(ctx, card.ID, time.Now().AddDate(0, 0, -7), time.Now())
	if err != nil {
		t.Errorf("GetCardStats() error = %v", err)
	}

	if stats.Views != 3 {
		t.Errorf("Expected 3 views, got %d", stats.Views)
	}
}

// TestXianyuCardStatsRepository_GetCardStats_StatsByDate 测试按日期统计
func TestXianyuCardStatsRepository_GetCardStats_StatsByDate(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)
	ctx := context.Background()

	card := &model.XianyuCard{
		Title:       "Daily Stats Card",
		Description: "Test daily stats",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	for i := 1; i <= 3; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "view",
			IP:           "192.168.1." + string(rune('0'+i)),
			CreatedAt:    time.Now(),
		})
	}

	yesterday := time.Now().AddDate(0, 0, -1)
	for i := 1; i <= 5; i++ {
		db.GetDB().Create(&model.XianyuCardActivity{
			CardID:       card.ID,
			ActivityType: "view",
			IP:           "192.168.2." + string(rune('0'+i)),
			CreatedAt:    yesterday,
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetCardStats(ctx, card.ID, startDate, endDate)
	if err != nil {
		t.Errorf("GetCardStats() error = %v", err)
	}

	if len(stats.StatsByDate) == 0 {
		t.Error("Expected daily stats to be returned")
	}

	if stats.Views != 8 {
		t.Errorf("Expected 8 total views, got %d", stats.Views)
	}
}

// TestXianyuCardStatsRepository_RecordActivity_WithContext 测试使用 Context
func TestXianyuCardStatsRepository_RecordActivity_WithContext(t *testing.T) {
	repo := setupXianyuCardStatsRepository(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	card := &model.XianyuCard{
		Title:       "Context Test Card",
		Description: "Test context cancellation",
		IsActive:    true,
	}
	db.GetDB().Create(card)

	err := repo.RecordActivity(ctx, card.ID, "view", "192.168.1.1", "Mozilla/5.0", "")
	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
}

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

func setupShortLinkAccessTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.ShortLinkAccess{},
	)
	db.SetTestDB(database)
	return database
}

func setupShortLinkAccessRepository(t *testing.T) ShortLinkAccessRepository {
	setupShortLinkAccessTestDB(t)
	return NewShortLinkAccessRepository(db.GetDB())
}

// TestShortLinkAccessRepository_Create 测试创建短链访问记录
func TestShortLinkAccessRepository_Create(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		access  *model.ShortLinkAccess
		wantErr bool
	}{
		{
			name: "create access record success",
			access: &model.ShortLinkAccess{
				ShortLinkID: 1,
				IP:          "192.168.1.1",
				UserAgent:   "Mozilla/5.0",
				DeviceType:  "desktop",
				Browser:     "Chrome",
				OS:          "Windows 10",
			},
			wantErr: false,
		},
		{
			name: "create access record with mobile device",
			access: &model.ShortLinkAccess{
				ShortLinkID: 2,
				IP:          "192.168.1.2",
				UserAgent:   "iPhone Safari",
				DeviceType:  "mobile",
				Browser:     "Safari",
				OS:          "iOS",
			},
			wantErr: false,
		},
		{
			name: "create access record with referer",
			access: &model.ShortLinkAccess{
				ShortLinkID: 1,
				IP:          "192.168.1.3",
				Referer:     "https://google.com",
				DeviceType:  "tablet",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.access)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.access.ID == 0 {
				t.Error("Expected access ID to be set after creation")
			}
		})
	}
}

// TestShortLinkAccessRepository_GetByID 测试根据 ID 获取访问记录
func TestShortLinkAccessRepository_GetByID(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	access := &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.100",
		DeviceType:  "desktop",
	}
	repo.Create(ctx, access)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing access",
			id:      access.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing access",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ShortLinkID != 1 {
					t.Errorf("Expected ShortLinkID 1, got %d", result.ShortLinkID)
				}
			}
		})
	}
}

// TestShortLinkAccessRepository_GetByShortLinkID 测试根据短链 ID 获取访问记录
func TestShortLinkAccessRepository_GetByShortLinkID(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		repo.Create(ctx, &model.ShortLinkAccess{
			ShortLinkID: 1,
			IP:          "192.168.1." + string(rune('0'+i)),
			DeviceType:  "desktop",
		})
	}

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 99,
		IP:          "192.168.2.1",
		DeviceType:  "mobile",
	})

	tests := []struct {
		name        string
		shortLinkID uint
		page        int
		pageSize    int
		wantCount   int
		wantTotal   int64
	}{
		{
			name:        "get all accesses for short link 1",
			shortLinkID: 1,
			page:        1,
			pageSize:    10,
			wantCount:   5,
			wantTotal:   5,
		},
		{
			name:        "get first page",
			shortLinkID: 1,
			page:        1,
			pageSize:    3,
			wantCount:   3,
			wantTotal:   5,
		},
		{
			name:        "get second page",
			shortLinkID: 1,
			page:        2,
			pageSize:    3,
			wantCount:   2,
			wantTotal:   5,
		},
		{
			name:        "get accesses for short link 99",
			shortLinkID: 99,
			page:        1,
			pageSize:    10,
			wantCount:   1,
			wantTotal:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetByShortLinkID(context.Background(), tt.shortLinkID, tt.page, tt.pageSize)

			if err != nil {
				t.Errorf("GetByShortLinkID() error = %v", err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if total != tt.wantTotal {
				t.Errorf("Expected total %d, got %d", tt.wantTotal, total)
			}
		})
	}
}

// TestShortLinkAccessRepository_GetStatsByShortLinkID 测试获取统计信息
func TestShortLinkAccessRepository_GetStatsByShortLinkID(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		repo.Create(ctx, &model.ShortLinkAccess{
			ShortLinkID: 1,
			IP:          "192.168.1." + string(rune('0'+i)),
		})
	}

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	stats, err := repo.GetStatsByShortLinkID(context.Background(), 1, startDate, endDate)
	if err != nil {
		t.Errorf("GetStatsByShortLinkID() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}
	if stats.ShortLinkID != 1 {
		t.Errorf("Expected ShortLinkID 1, got %d", stats.ShortLinkID)
	}
}

// TestShortLinkAccessRepository_GetDailyStatsByShortLinkID 测试获取每日统计
func TestShortLinkAccessRepository_GetDailyStatsByShortLinkID(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.1",
	})

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	results, err := repo.GetDailyStatsByShortLinkID(context.Background(), 1, startDate, endDate)
	if err != nil {
		t.Errorf("GetDailyStatsByShortLinkID() error = %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected daily stats to be returned")
	}
}

// TestShortLinkAccessRepository_GetDeviceTypeStatsByShortLinkID 测试获取设备类型统计
func TestShortLinkAccessRepository_GetDeviceTypeStatsByShortLinkID(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.1",
		DeviceType:  "desktop",
	})

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.2",
		DeviceType:  "mobile",
	})

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.3",
		DeviceType:  "mobile",
	})

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	results, err := repo.GetDeviceTypeStatsByShortLinkID(context.Background(), 1, startDate, endDate)
	if err != nil {
		t.Errorf("GetDeviceTypeStatsByShortLinkID() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 device types, got %d", len(results))
	}
}

// TestShortLinkAccessRepository_GetAllDailyStats 测试获取所有短链的每日统计
func TestShortLinkAccessRepository_GetAllDailyStats(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.1",
	})

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 2,
		IP:          "192.168.1.2",
	})

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	results, err := repo.GetAllDailyStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Errorf("GetAllDailyStats() error = %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected daily stats to be returned")
	}
}

// TestShortLinkAccessRepository_GetAllDeviceTypeStats 测试获取所有短链的设备类型统计
func TestShortLinkAccessRepository_GetAllDeviceTypeStats(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 1,
		IP:          "192.168.1.1",
		DeviceType:  "desktop",
	})

	repo.Create(ctx, &model.ShortLinkAccess{
		ShortLinkID: 2,
		IP:          "192.168.1.2",
		DeviceType:  "mobile",
	})

	startDate := time.Now().AddDate(0, 0, -7)
	endDate := time.Now()

	results, err := repo.GetAllDeviceTypeStats(context.Background(), startDate, endDate)
	if err != nil {
		t.Errorf("GetAllDeviceTypeStats() error = %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected device type stats to be returned")
	}
}

// TestShortLinkAccessRepository_GetByID_NotFound 测试获取不存在的访问记录
func TestShortLinkAccessRepository_GetByID_NotFound(t *testing.T) {
	repo := setupShortLinkAccessRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 99999)
	if err == nil {
		t.Error("Expected error when getting non-existing access")
	}
}

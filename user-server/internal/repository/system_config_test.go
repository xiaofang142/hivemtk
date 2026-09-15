package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupSystemConfigTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SystemConfig{},
	)
	db.SetTestDB(database)
	return database
}

func setupSystemConfigRepository(t *testing.T) SystemConfigRepository {
	setupSystemConfigTestDB(t)
	return NewSystemConfigRepository()
}

// TestSystemConfigRepository_SaveConfig 测试保存系统配置
func TestSystemConfigRepository_SaveConfig(t *testing.T) {
	repo := setupSystemConfigRepository(t)

	tests := []struct {
		name    string
		config  *model.SystemConfig
		wantErr bool
	}{
		{
			name: "create new config",
			config: &model.SystemConfig{
				Name:       "app_config",
				WebsiteURL: "https://example.com",
			},
			wantErr: false,
		},
		{
			name: "create config with minimal fields",
			config: &model.SystemConfig{
				Name: "minimal_config",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.SaveConfig(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("SaveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Name != tt.config.Name {
					t.Errorf("Expected name '%s', got '%s'", tt.config.Name, result.Name)
				}
			}
		})
	}
}

// TestSystemConfigRepository_GetConfig 测试获取系统配置
func TestSystemConfigRepository_GetConfig(t *testing.T) {
	repo := setupSystemConfigRepository(t)

	expectedConfig := &model.SystemConfig{
		Name:       "test_config",
		WebsiteURL: "https://test.example.com",
	}
	repo.SaveConfig(context.Background(), expectedConfig)

	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "get existing config",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetConfig(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("GetConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Name != "test_config" {
					t.Errorf("Expected name 'test_config', got '%s'", result.Name)
				}
				if result.WebsiteURL != "https://test.example.com" {
					t.Errorf("Expected website URL 'https://test.example.com', got '%s'", result.WebsiteURL)
				}
			}
		})
	}
}

// TestSystemConfigRepository_SaveConfig_Update 测试 SaveConfig 不会更新现有配置
func TestSystemConfigRepository_SaveConfig_Update(t *testing.T) {
	repo := setupSystemConfigRepository(t)

	config := &model.SystemConfig{
		Name:       "update_test",
		WebsiteURL: "https://original.example.com",
	}
	if _, err := repo.SaveConfig(context.Background(), config); err != nil {
		t.Fatalf("SaveConfig() 首次创建失败: %v", err)
	}

	// 单例配置：第二次保存应**覆盖**已有值。
	// 管理端 SystemConfigController.SaveConfig（internal/controller/system_config.go:34）
	// 正是靠这个语义让管理员改动能生效；原断言写成"保留原值"，
	// 既与本用例名 _Update 自相矛盾，也与产品行为相反。
	config.WebsiteURL = "https://updated.example.com"
	resultConfig, err := repo.SaveConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("SaveConfig() 更新失败: %v", err)
	}
	if resultConfig.WebsiteURL != "https://updated.example.com" {
		t.Errorf("第二次保存应覆盖为 'https://updated.example.com'，实际 '%s'", resultConfig.WebsiteURL)
	}

	// 必须真正落库，而不只是返回值变了
	var persisted model.SystemConfig
	if err := db.GetDB().First(&persisted).Error; err != nil {
		t.Fatalf("回查配置失败: %v", err)
	}
	if persisted.WebsiteURL != "https://updated.example.com" {
		t.Errorf("库中 WebsiteURL 未被更新，实际 '%s'", persisted.WebsiteURL)
	}

	// 单例约束：不应产生第二行
	var n int64
	if err := db.GetDB().Model(&model.SystemConfig{}).Count(&n).Error; err != nil {
		t.Fatalf("统计配置行数失败: %v", err)
	}
	if n != 1 {
		t.Errorf("system_config 应为单例（1 行），实际 %d 行", n)
	}
}

// TestSystemConfigRepository_GetConfig_Empty 测试获取空配置
func TestSystemConfigRepository_GetConfig_Empty(t *testing.T) {
	setupSystemConfigTestDB(t)

	repo := NewSystemConfigRepository()
	_, err := repo.GetConfig(context.Background())
	if err == nil {
		t.Error("Expected error for empty config")
	}
}

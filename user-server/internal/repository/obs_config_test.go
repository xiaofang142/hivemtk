package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupObsConfigTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.ObsConfig{},
	)
	db.SetTestDB(database)
	return database
}

func setupObsConfigRepository(t *testing.T) ObsConfigRepository {
	setupObsConfigTestDB(t)
	return NewObsConfigRepository()
}

// TestObsConfigRepository_Create 测试创建 OBS 配置
func TestObsConfigRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	tests := []struct {
		name    string
		config  *model.ObsConfig
		wantErr bool
	}{
		{
			name: "create obs config success",
			config: &model.ObsConfig{
				Name:      "Test Storage",
				Provider:  model.ObsProviderAliyun,
				AccessKey: "test_access_key",
				SecretKey: "test_secret_key",
				Bucket:    "test-bucket",
				Region:    "cn-hangzhou",
				Status:    model.ObsStatusActive,
				MaxSize:   104857600,
				MaxCount:  1000,
				IsDefault: false,
			},
			wantErr: false,
		},
		{
			name: "create qiniu config",
			config: &model.ObsConfig{
				Name:      "Qiniu Storage",
				Provider:  model.ObsProviderQiniu,
				AccessKey: "qiniu_access_key",
				SecretKey: "qiniu_secret_key",
				Bucket:    "qiniu-bucket",
				Status:    model.ObsStatusActive,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.config.ID == "" {
				t.Error("Expected config ID to be set after creation")
			}
		})
	}
}

// TestObsConfigRepository_GetByID 测试根据 ID 获取配置
func TestObsConfigRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	config := &model.ObsConfig{
		Name:     "GetByID Config",
		Provider: model.ObsProviderAliyun,
		Bucket:   "getbyid-bucket",
		Status:   model.ObsStatusActive,
	}
	repo.Create(ctx, config)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "get existing config",
			id:      config.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing config",
			id:      "non-existing-id",
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
				if result == nil {
					t.Fatalf("GetByID 既没报错也没给行（`result.Name` 会 nil 解引用带走整包测试二进制）")
				}
				if result.Name != "GetByID Config" {
					t.Errorf("Expected name 'GetByID Config', got '%s'", result.Name)
				}
			}
		})
	}
}

// TestObsConfigRepository_GetList 测试获取配置列表
func TestObsConfigRepository_GetList(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	for i := 1; i <= 5; i++ {
		repo.Create(ctx, &model.ObsConfig{
			Name:     "Config " + string(rune('0'+i)),
			Provider: model.ObsProviderAliyun,
			Bucket:   "bucket-" + string(rune('0'+i)),
			Status:   model.ObsStatusActive,
		})
	}

	repo.Create(ctx, &model.ObsConfig{
		Name:     "Qiniu Config",
		Provider: model.ObsProviderQiniu,
		Bucket:   "qiniu-bucket",
		Status:   model.ObsStatusActive,
	})

	repo.Create(ctx, &model.ObsConfig{
		Name:     "Inactive Config",
		Provider: model.ObsProviderTencent,
		Bucket:   "tencent-bucket",
		Status:   model.ObsStatusInactive,
	})

	tests := []struct {
		name      string
		provider  string
		status    string
		page      int
		limit     int
		wantCount int
		wantTotal int64
	}{
		{
			name:      "get all configs",
			provider:  "",
			status:    "",
			page:      1,
			limit:     10,
			wantCount: 7,
			wantTotal: 7,
		},
		{
			name:      "filter by provider",
			provider:  "aliyun",
			status:    "",
			page:      1,
			limit:     10,
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name:      "filter by status",
			provider:  "",
			status:    "active",
			page:      1,
			limit:     10,
			wantCount: 6,
			wantTotal: 6,
		},
		{
			name:      "filter by provider and status",
			provider:  "aliyun",
			status:    "active",
			page:      1,
			limit:     10,
			wantCount: 5,
			wantTotal: 5,
		},
		{
			name:      "pagination first page",
			provider:  "",
			status:    "",
			page:      1,
			limit:     3,
			wantCount: 3,
			wantTotal: 7,
		},
		{
			name:      "pagination second page",
			provider:  "",
			status:    "",
			page:      2,
			limit:     3,
			wantCount: 3,
			wantTotal: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := repo.GetList(ctx, tt.page, tt.limit, tt.provider, tt.status)

			if err != nil {
				t.Errorf("GetList() error = %v", err)
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

// TestObsConfigRepository_Update 测试更新配置
func TestObsConfigRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	config := &model.ObsConfig{
		Name:     "Original Name",
		Provider: model.ObsProviderAliyun,
		Bucket:   "original-bucket",
		Status:   model.ObsStatusActive,
	}
	repo.Create(ctx, config)

	config.Name = "Updated Name"
	config.Bucket = "updated-bucket"

	err := repo.Update(ctx, config)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, config.ID)
	if updated == nil {
		t.Fatalf("GetByID 既没报错也没给行（`updated.Name` 会 nil 解引用带走整包测试二进制）")
	}
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.Bucket != "updated-bucket" {
		t.Errorf("Expected bucket 'updated-bucket', got '%s'", updated.Bucket)
	}
}

// TestObsConfigRepository_Delete 测试删除配置
func TestObsConfigRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	config := &model.ObsConfig{
		Name:     "To Delete",
		Provider: model.ObsProviderAliyun,
		Bucket:   "delete-bucket",
		Status:   model.ObsStatusActive,
	}
	repo.Create(ctx, config)

	err := repo.DeleteNonDefault(ctx, config.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, config.ID)
	if err == nil {
		t.Error("Expected config to be deleted")
	}
}

// TestObsConfigRepository_GetDefault 测试获取默认配置
func TestObsConfigRepository_GetDefault(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	repo.Create(ctx, &model.ObsConfig{
		Name:      "Non-default Config",
		Provider:  model.ObsProviderAliyun,
		IsDefault: false,
		Status:    model.ObsStatusActive,
	})

	defaultConfig := &model.ObsConfig{
		Name:      "Default Config",
		Provider:  model.ObsProviderQiniu,
		IsDefault: true,
		Status:    model.ObsStatusActive,
	}
	repo.Create(ctx, defaultConfig)

	result, err := repo.GetDefault(context.Background())
	if err != nil {
		t.Errorf("GetDefault() error = %v", err)
	}
	if result == nil {
		t.Fatalf("GetDefault 既没报错也没给行（`result.Name` 会 nil 解引用带走整包测试二进制）")
	}

	if result.Name != "Default Config" {
		t.Errorf("Expected name 'Default Config', got '%s'", result.Name)
	}
}

// TestObsConfigRepository_SetDefault 测试设置默认配置
func TestObsConfigRepository_SetDefault(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	config1 := &model.ObsConfig{
		Name:      "Config 1",
		Provider:  model.ObsProviderAliyun,
		IsDefault: false,
		Status:    model.ObsStatusActive,
	}
	repo.Create(ctx, config1)

	config2 := &model.ObsConfig{
		Name:      "Config 2",
		Provider:  model.ObsProviderQiniu,
		IsDefault: false,
		Status:    model.ObsStatusActive,
	}
	repo.Create(ctx, config2)

	err := repo.SetDefault(context.Background(), config1.ID)
	if err != nil {
		t.Errorf("SetDefault() error = %v", err)
	}

	config1Updated, _ := repo.GetByID(ctx, config1.ID)
	if config1Updated == nil {
		t.Fatalf("GetByID 既没报错也没给行（`config1Updated.IsDefault` 会 nil 解引用带走整包测试二进制）")
	}
	if !config1Updated.IsDefault {
		t.Error("Expected config1 to be default")
	}

	err = repo.SetDefault(context.Background(), config2.ID)
	if err != nil {
		t.Errorf("SetDefault() error = %v", err)
	}

	config1Updated2, _ := repo.GetByID(ctx, config1.ID)
	config2Updated, _ := repo.GetByID(ctx, config2.ID)
	if config1Updated2 == nil || config2Updated == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面两句解引用会带走整包测试二进制）")
	}

	if config1Updated2.IsDefault {
		t.Error("Expected config1 to not be default after setting config2")
	}
	if !config2Updated.IsDefault {
		t.Error("Expected config2 to be default")
	}
}

// TestObsConfigRepository_ClearDefault 随 ClearDefault 一起删除（批M / N-26）。
//
// 删的是入口而不是判据：旧 API 把"清掉所有默认"做成一个可单独调用的方法，
// service 于是能在事务外先清后设，留下"零条默认"的库状态；本批把切换收进
// SetDefault 一个事务之后，这个单独入口没有任何合法用法。
// 它原来断言的性质（切换之后旧行不再是默认）由
// obs_config_default_batchm_test.go 的 TestBatchM_SetDefaultLeavesExactlyOneDefault 接管。

// TestObsConfigRepository_UpdateStatus 测试更新状态
func TestObsConfigRepository_UpdateStatus(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	config := &model.ObsConfig{
		Name:     "Status Update",
		Provider: model.ObsProviderAliyun,
		Status:   model.ObsStatusActive,
	}
	repo.Create(ctx, config)

	err := repo.UpdateStatus(context.Background(), config.ID, model.ObsStatusInactive)
	if err != nil {
		t.Errorf("UpdateStatus() error = %v", err)
	}

	updated, _ := repo.GetByID(ctx, config.ID)
	if updated == nil {
		t.Fatalf("GetByID 既没报错也没给行（`updated.Status` 会 nil 解引用带走整包测试二进制）")
	}
	if updated.Status != model.ObsStatusInactive {
		t.Errorf("Expected status 'inactive', got '%s'", updated.Status)
	}
}

// TestObsConfigRepository_CountByStatus 测试按状态统计数量
func TestObsConfigRepository_CountByStatus(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	repo.Create(ctx, &model.ObsConfig{
		Name:     "Active 1",
		Provider: model.ObsProviderAliyun,
		Status:   model.ObsStatusActive,
	})

	repo.Create(ctx, &model.ObsConfig{
		Name:     "Active 2",
		Provider: model.ObsProviderQiniu,
		Status:   model.ObsStatusActive,
	})

	repo.Create(ctx, &model.ObsConfig{
		Name:     "Inactive 1",
		Provider: model.ObsProviderTencent,
		Status:   model.ObsStatusInactive,
	})

	activeCount, err := repo.CountByStatus(context.Background(), model.ObsStatusActive)
	if err != nil {
		t.Errorf("CountByStatus() error = %v", err)
	}
	if activeCount != 2 {
		t.Errorf("Expected 2 active configs, got %d", activeCount)
	}

	inactiveCount, err := repo.CountByStatus(context.Background(), model.ObsStatusInactive)
	if err != nil {
		t.Errorf("CountByStatus() error = %v", err)
	}
	if inactiveCount != 1 {
		t.Errorf("Expected 1 inactive config, got %d", inactiveCount)
	}
}

// TestObsConfigRepository_GetByID_NotFound 测试获取不存在的配置
func TestObsConfigRepository_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := setupObsConfigRepository(t)

	_, err := repo.GetByID(ctx, "non-existing-id")
	if err == nil {
		t.Error("Expected error when getting non-existing config")
	}
}

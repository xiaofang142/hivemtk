package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"

	"gorm.io/gorm"
)

func setupRagConfigTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.RagProduct{},
		&model.PlatformAccountConfig{},
	)
	testmigrate.RunTestMigrations(t, database)
	return database
}

func setupRagConfigRepository(t *testing.T) (*RagConfigRepository, *gorm.DB) {
	database := setupRagConfigTestDB(t)
	return NewRagConfigRepository(database), database
}

// TestRagConfigRepository_CreateRagProduct 测试创建 RAG 产品
func TestRagConfigRepository_CreateRagProduct(t *testing.T) {
	repo, _ := setupRagConfigRepository(t)

	tests := []struct {
		name    string
		product *model.RagProduct
		wantErr bool
	}{
		{
			name: "create rag product success",
			product: &model.RagProduct{
				Name:        "Test RAG Product",
				Description: "Test description",
				Category:    "customer-service",
				VectorTable: "vec_test_rag_product",
				LLMModel:    "gpt-3.5-turbo",
				Temperature: 0.7,
				MaxTokens:   1000,
				IsActive:    true,
			},
			wantErr: false,
		},
		{
			name: "create rag product with custom config",
			product: &model.RagProduct{
				Name:        "Custom RAG Product",
				Category:    "sales",
				VectorTable: "vec_custom_rag_product",
				LLMModel:    "gpt-4",
				LLMProviderConfig: model.LLMProviderConfig{
					APIKey:         "sk-t" + "est123",
					BaseURL:        "https://api.openai.com/v1",
					APIType:        "openai",
					Model:          "gpt-4",
					MaxRetries:     3,
					RequestTimeout: 60,
				},
				Temperature: 0.5,
				MaxTokens:   2000,
				IsActive:    true,
			},
			wantErr: false,
		},
		{
			name: "create inactive rag product",
			product: &model.RagProduct{
				Name:        "Inactive Product",
				Category:    "support",
				VectorTable: "vec_inactive_product",
				LLMModel:    "gpt-3.5-turbo",
				IsActive:    false,
			},
			wantErr: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateRagProduct(ctx, tt.product)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRagProduct() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.product.ID == "" {
				t.Error("Expected product ID to be set after creation")
			}
		})
	}
}

// TestRagConfigRepository_GetRagProductByID 测试根据 ID 获取 RAG 产品
func TestRagConfigRepository_GetRagProductByID(t *testing.T) {
	repo, database := setupRagConfigRepository(t)
	ctx := context.Background()

	product := &model.RagProduct{
		Name:        "GetByID Product",
		Description: "Test description",
		Category:    "customer-service",
		VectorTable: "vec_getbyid_product",
		LLMModel:    "gpt-3.5-turbo",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, product)

	inactiveProduct := &model.RagProduct{
		Name:        "Inactive Product",
		Category:    "sales",
		VectorTable: "vec_inactive_product_getbyid",
		LLMModel:    "gpt-4",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, inactiveProduct)
	inactiveProduct.IsActive = false
	database.Save(inactiveProduct)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "get existing active product",
			id:      product.ID,
			wantErr: false,
		},
		{
			name:    "get inactive product (should not find)",
			id:      inactiveProduct.ID,
			wantErr: true,
		},
		{
			name:    "get non-existing product",
			id:      "non-existing-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetRagProductByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetRagProductByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Name != "GetByID Product" {
					t.Errorf("Expected name 'GetByID Product', got '%s'", result.Name)
				}
				if !result.IsActive {
					t.Error("Expected product to be active")
				}
			}
		})
	}
}

// TestRagConfigRepository_GetAccountConfig 测试获取账户配置
func TestRagConfigRepository_GetAccountConfig(t *testing.T) {
	repo, database := setupRagConfigRepository(t)
	ctx := context.Background()

	product := &model.RagProduct{
		Name:        "Linked Product",
		Category:    "customer-service",
		VectorTable: "vec_linked_product",
		LLMModel:    "gpt-3.5-turbo",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, product)

	config := &model.PlatformAccountConfig{
		ID:                 "config-123",
		AccountID:          "account-123",
		Platform:           "douyin",
		RagProductID:       &product.ID,
		IsAutoReplyEnabled: true,
		IsRagEnabled:       true,
		MaxDailyQueries:    1000,
	}
	database.Create(config)

	tests := []struct {
		name      string
		accountID string
		platform  string
		wantErr   bool
	}{
		{
			name:      "get existing config",
			accountID: "account-123",
			platform:  "douyin",
			wantErr:   false,
		},
		{
			name:      "get non-existing config (returns default)",
			accountID: "non-existing",
			platform:  "douyin",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetAccountConfig(ctx, tt.accountID, tt.platform)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetAccountConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.AccountID != tt.accountID && tt.accountID != "non-existing" {
					t.Errorf("Expected account_id '%s', got '%s'", tt.accountID, result.AccountID)
				}
			}
		})
	}
}

// TestRagConfigRepository_UpsertAccountConfig 测试创建或更新账户配置
func TestRagConfigRepository_UpsertAccountConfig(t *testing.T) {
	repo, _ := setupRagConfigRepository(t)
	ctx := context.Background()

	newConfig := &model.PlatformAccountConfig{
		AccountID:          "account-456",
		Platform:           "xiaohongshu",
		IsAutoReplyEnabled: false,
		IsRagEnabled:       true,
		MaxDailyQueries:    500,
	}

	err := repo.UpsertAccountConfig(ctx, newConfig)
	if err != nil {
		t.Errorf("UpsertAccountConfig() create error = %v", err)
	}

	if newConfig.ID == "" {
		t.Error("Expected config ID to be set after creation")
	}

	newConfig.IsAutoReplyEnabled = true
	newConfig.MaxDailyQueries = 1000

	err = repo.UpsertAccountConfig(ctx, newConfig)
	if err != nil {
		t.Errorf("UpsertAccountConfig() update error = %v", err)
	}

	updated, _ := repo.GetAccountConfig(ctx, "account-456", "xiaohongshu")
	if !updated.IsAutoReplyEnabled {
		t.Error("Expected IsAutoReplyEnabled to be true after update")
	}
	if updated.MaxDailyQueries != 1000 {
		t.Errorf("Expected MaxDailyQueries 1000, got %d", updated.MaxDailyQueries)
	}
}

// TestRagConfigRepository_ListRagProducts 测试获取 RAG 产品列表
func TestRagConfigRepository_ListRagProducts(t *testing.T) {
	repo, database := setupRagConfigRepository(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		repo.CreateRagProduct(ctx, &model.RagProduct{
			Name:        "ListTest Product " + string(rune('0'+i)),
			Category:    "customer-service",
			VectorTable: "vec_listtest_product_" + string(rune('0'+i)),
			LLMModel:    "gpt-3.5-turbo",
			IsActive:    true,
		})
	}

	inactiveProduct := &model.RagProduct{
		Name:        "ListTest Inactive Product",
		Category:    "sales",
		VectorTable: "vec_listtest_inactive_product",
		LLMModel:    "gpt-4",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, inactiveProduct)
	inactiveProduct.IsActive = false
	database.Save(inactiveProduct)

	results, err := repo.ListRagProducts(ctx)
	if err != nil {
		t.Errorf("ListRagProducts() error = %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 active products, got %d", len(results))
	}
}

// TestRagConfigRepository_UpdateRagProduct 测试更新 RAG 产品
func TestRagConfigRepository_UpdateRagProduct(t *testing.T) {
	repo, _ := setupRagConfigRepository(t)
	ctx := context.Background()

	product := &model.RagProduct{
		Name:        "Original Name",
		Description: "Original description",
		Category:    "customer-service",
		VectorTable: "vec_original_name",
		LLMModel:    "gpt-3.5-turbo",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, product)

	product.Name = "Updated Name"
	product.Description = "Updated description"
	product.LLMModel = "gpt-4"

	err := repo.UpdateRagProduct(ctx, product)
	if err != nil {
		t.Errorf("UpdateRagProduct() error = %v", err)
	}

	updated, _ := repo.GetRagProductByID(ctx, product.ID)
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.LLMModel != "gpt-4" {
		t.Errorf("Expected LLMModel 'gpt-4', got '%s'", updated.LLMModel)
	}
}

// TestRagConfigRepository_DeleteRagProduct 测试删除 RAG 产品
func TestRagConfigRepository_DeleteRagProduct(t *testing.T) {
	repo, _ := setupRagConfigRepository(t)
	ctx := context.Background()

	product := &model.RagProduct{
		Name:        "To Delete",
		Category:    "customer-service",
		VectorTable: "vec_to_delete",
		LLMModel:    "gpt-3.5-turbo",
		IsActive:    true,
	}
	repo.CreateRagProduct(ctx, product)

	err := repo.DeleteRagProduct(ctx, product.ID)
	if err != nil {
		t.Errorf("DeleteRagProduct() error = %v", err)
	}

	_, err = repo.GetRagProductByID(ctx, product.ID)
	if err == nil {
		t.Error("Expected product to be deleted")
	}
}

// TestRagConfigRepository_GetRagProductByID_NotFound 测试获取不存在的 RAG 产品
func TestRagConfigRepository_GetRagProductByID_NotFound(t *testing.T) {
	repo, _ := setupRagConfigRepository(t)
	ctx := context.Background()

	_, err := repo.GetRagProductByID(ctx, "non-existing-id")
	if err == nil {
		t.Error("Expected error when getting non-existing product")
	}
}

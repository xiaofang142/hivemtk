package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/aiagent/knowledge/model"
	sysmodel "hivemtk-user/internal/model"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RagConfigRepository struct {
	db *gorm.DB
}

func NewRagConfigRepository(db *gorm.DB) *RagConfigRepository {
	return &RagConfigRepository{db: db}
}

func (r *RagConfigRepository) CreateRagProduct(ctx context.Context, product *model.RagProduct) error {
	product.ID = uuid.New().String()
	return r.db.WithContext(ctx).Create(product).Error
}

func (r *RagConfigRepository) GetRagProductByID(ctx context.Context, id string) (*model.RagProduct, error) {
	var product model.RagProduct
	err := r.db.WithContext(ctx).Where("id = ? AND is_active = ?", id, true).First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("rag product not found")
		}
		return nil, err
	}
	return &product, nil
}

// FindRagProductByIDOnly 仅按 ID 查询 RagProduct（不过滤 is_active）
// 用于内部反查 UUID 等场景
func (r *RagConfigRepository) FindRagProductByIDOnly(ctx context.Context, id string) (*model.RagProduct, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("rag product repository not initialized")
	}
	var product model.RagProduct
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &product, nil
}

func (r *RagConfigRepository) GetAccountConfig(ctx context.Context, accountID, platform string) (*sysmodel.PlatformAccountConfig, error) {
	var config sysmodel.PlatformAccountConfig
	err := r.db.WithContext(ctx).
		Preload("RagProduct").
		Where("account_id = ? AND platform = ?", accountID, platform).
		First(&config).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &sysmodel.PlatformAccountConfig{
				AccountID:          accountID,
				Platform:           platform,
				IsAutoReplyEnabled: false,
				IsRagEnabled:       false,
				MaxDailyQueries:    1000,
			}, nil
		}
		return nil, err
	}
	return &config, nil
}

func (r *RagConfigRepository) UpsertAccountConfig(ctx context.Context, config *sysmodel.PlatformAccountConfig) error {
	existingConfig := &sysmodel.PlatformAccountConfig{}
	result := r.db.WithContext(ctx).
		Where("account_id = ? AND platform = ?", config.AccountID, config.Platform).
		First(existingConfig)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		config.ID = uuid.New().String()
		return r.db.WithContext(ctx).Create(config).Error
	} else if result.Error != nil {
		return result.Error
	}

	config.ID = existingConfig.ID
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *RagConfigRepository) ListRagProducts(ctx context.Context) ([]*model.RagProduct, error) {
	var products []*model.RagProduct
	err := r.db.WithContext(ctx).Where("is_active = ?", true).Find(&products).Error
	return products, err
}

// UpdateRagProduct 精准更新 RAG 产品（白名单字段，绝不触碰 vector_table / id / created_at）
//
// 使用 map + Updates，仅更新 service 层明确赋值的字段，避开 VectorTable。
// 若 service 层没有提供任何可更新字段，直接返回 nil（幂等 no-op）。
func (r *RagConfigRepository) UpdateRagProduct(ctx context.Context, product *model.RagProduct) error {
	updates := map[string]any{}
	if product.Name != "" {
		updates["name"] = product.Name
	}
	if product.Description != "" {
		updates["description"] = product.Description
	}
	if product.Category != "" {
		updates["category"] = product.Category
	}
	if product.EmbeddingModel != "" {
		updates["embedding_model"] = product.EmbeddingModel
	}
	if product.EmbeddingDim != 0 {
		updates["embedding_dim"] = product.EmbeddingDim
	}
	if product.LLMModel != "" {
		updates["llm_model"] = product.LLMModel
	}
	if product.LLMProviderConfig.APIType != "" {
		updates["llm_api_type"] = product.LLMProviderConfig.APIType
	}
	if product.LLMProviderConfig.Model != "" {
		updates["llm_model_detail"] = product.LLMProviderConfig.Model
	}
	if product.LLMProviderConfig.APIKey != "" {
		updates["llm_api_key"] = product.LLMProviderConfig.APIKey
	}
	if product.LLMProviderConfig.BaseURL != "" {
		updates["llm_base_url"] = product.LLMProviderConfig.BaseURL
	}
	if product.LLMProviderConfig.MaxRetries != 0 {
		updates["llm_max_retries"] = product.LLMProviderConfig.MaxRetries
	}
	if product.LLMProviderConfig.RequestTimeout != 0 {
		updates["llm_request_timeout"] = product.LLMProviderConfig.RequestTimeout
	}
	if product.EmbeddingProviderConfig.APIType != "" {
		updates["emb_api_type"] = product.EmbeddingProviderConfig.APIType
	}
	if product.EmbeddingProviderConfig.Model != "" {
		updates["emb_model"] = product.EmbeddingProviderConfig.Model
	}
	if product.EmbeddingProviderConfig.APIKey != "" {
		updates["emb_api_key"] = product.EmbeddingProviderConfig.APIKey
	}
	if product.EmbeddingProviderConfig.BaseURL != "" {
		updates["emb_base_url"] = product.EmbeddingProviderConfig.BaseURL
	}
	if product.EmbeddingProviderConfig.Dimension != 0 {
		updates["emb_dimension"] = product.EmbeddingProviderConfig.Dimension
	}
	updates["emb_enabled"] = product.EmbeddingProviderConfig.Enabled
	if product.RerankProviderConfig.APIType != "" {
		updates["rerank_api_type"] = product.RerankProviderConfig.APIType
	}
	if product.RerankProviderConfig.Model != "" {
		updates["rerank_model"] = product.RerankProviderConfig.Model
	}
	if product.RerankProviderConfig.APIKey != "" {
		updates["rerank_api_key"] = product.RerankProviderConfig.APIKey
	}
	if product.RerankProviderConfig.BaseURL != "" {
		updates["rerank_base_url"] = product.RerankProviderConfig.BaseURL
	}
	updates["rerank_enabled"] = product.RerankProviderConfig.Enabled
	if product.Temperature != 0 {
		updates["temperature"] = product.Temperature
	}
	if product.MaxTokens != 0 {
		updates["max_tokens"] = product.MaxTokens
	}
	if product.TopP != 0 {
		updates["top_p"] = product.TopP
	}
	if product.FrequencyPenalty != 0 {
		updates["frequency_penalty"] = product.FrequencyPenalty
	}
	if product.PresencePenalty != 0 {
		updates["presence_penalty"] = product.PresencePenalty
	}
	if product.ResponseFormat != "" {
		updates["response_format"] = product.ResponseFormat
	}
	if product.SystemPrompt != "" {
		updates["system_prompt"] = product.SystemPrompt
	}
	if product.TopK != 0 {
		updates["top_k"] = product.TopK
	}
	if product.ChunkSize != 0 {
		updates["chunk_size"] = product.ChunkSize
	}
	if product.ChunkOverlap != 0 {
		updates["chunk_overlap"] = product.ChunkOverlap
	}
	if product.SimilarityThreshold != 0 {
		updates["similarity_threshold"] = product.SimilarityThreshold
	}
	if product.IsActive {
		updates["is_active"] = product.IsActive
	}
	if product.Status != 0 {
		updates["status"] = product.Status
	}

	updates["updated_at"] = time.Now()

	if len(updates) == 1 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.RagProduct{}).
		Where("id = ?", product.ID).
		Updates(updates).Error
}

func (r *RagConfigRepository) DeleteRagProduct(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.RagProduct{}).Error
}

// UpdateRagProductStats 更新 RAG 产品统计(独立部署模式:存根实现)
func (r *RagConfigRepository) UpdateRagProductStats(ctx context.Context, productID string, docCount int, chunkCount int64, lastSyncAt any) error {
	return nil
}

// ListAllRagProducts 全量产品列表（含未激活，供 Stats 汇总）
func (r *RagConfigRepository) ListAllRagProducts(ctx context.Context) ([]*model.RagProduct, error) {
	var products []*model.RagProduct
	err := r.db.WithContext(ctx).Find(&products).Error
	return products, err
}

// GetRagProductForUpdate 按 ID 取产品（含未激活）
func (r *RagConfigRepository) GetRagProductForUpdate(ctx context.Context, id string) (*model.RagProduct, error) {
	var p model.RagProduct
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteRagProductByID 按 ID 硬删产品
func (r *RagConfigRepository) DeleteRagProductByID(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.RagProduct{}).Error
}

// CreateRagProductWithVectorTable 创建产品（service 层已补齐 vector_table 默认值）
func (r *RagConfigRepository) CreateRagProductWithVectorTable(ctx context.Context, p *model.RagProduct) error {
	return r.db.WithContext(ctx).Create(p).Error
}

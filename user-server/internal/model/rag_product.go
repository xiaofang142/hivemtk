package model

import (
	"time"
)

// LLMProviderConfig LLM 提供商配置(嵌入式)
// 列前缀 llm_（由 embeddedPrefix 自动添加）。
// 注意：Model 字段 column 标签为 model_detail（而非 model），否则会与
// RagProduct.LLMModel（列 llm_model）冲突。最终列名 = llm_model_detail，
// 与 repository UpdateRagProduct 中 updates["llm_model_detail"] 一致。
type LLMProviderConfig struct {
	APIKey         string `json:"api_key" gorm:"column:api_key"`
	BaseURL        string `json:"base_url" gorm:"column:base_url"`
	APIType        string `json:"api_type" gorm:"column:api_type"`
	Model          string `json:"model" gorm:"column:model_detail"`
	MaxRetries     int    `json:"max_retries" gorm:"column:max_retries;default:3"`
	RequestTimeout int    `json:"request_timeout" gorm:"column:request_timeout;default:60"`
}

// EmbeddingProviderConfig 文本向量(text-embedding)供应商配置(嵌入式)
// 列前缀 emb_（由 embeddedPrefix 自动添加），per 知识库覆盖全局 config.yaml inference.embedding
// 注意：字段的 column 标签须为裸名（api_key 而非 emb_api_key），否则会与 embeddedPrefix 叠加成 emb_emb_api_key
type EmbeddingProviderConfig struct {
	APIKey    string `json:"api_key" gorm:"column:api_key"`
	BaseURL   string `json:"base_url" gorm:"column:base_url"`
	APIType   string `json:"api_type" gorm:"column:api_type"`
	Model     string `json:"model" gorm:"column:model"`
	Dimension int    `json:"dimension" gorm:"column:dimension;default:1024"`
	Enabled   bool   `json:"enabled" gorm:"column:enabled;default:true"`
}

// RerankProviderConfig 重排(rerank)供应商配置(嵌入式)
// 列前缀 rerank_（由 embeddedPrefix 自动添加），per 知识库覆盖全局 config.yaml inference.rerank
// 注意：字段的 column 标签须为裸名（api_key 而非 rerank_api_key），否则会与 embeddedPrefix 叠加成 rerank_rerank_api_key
type RerankProviderConfig struct {
	APIKey  string `json:"api_key" gorm:"column:api_key"`
	BaseURL string `json:"base_url" gorm:"column:base_url"`
	APIType string `json:"api_type" gorm:"column:api_type"`
	Model   string `json:"model" gorm:"column:model"`
	Enabled bool   `json:"enabled" gorm:"column:enabled;default:true"`
}

// RagProduct RAG 产品配置 - RAG V2.0 权威基线
// 一个产品 = 一个 pgvector 集合 + 完整 LLM/Embedding 参数
type RagProduct struct {
	ID                      string                  `json:"id" gorm:"primaryKey;size:64"`
	Name                    string                  `json:"name" gorm:"size:255;not null"`
	Description             string                  `json:"description"`
	Category                string                  `json:"category" gorm:"size:100"`
	VectorTable             string                  `json:"vector_table" gorm:"size:128;uniqueIndex"`
	EmbeddingModel          string                  `json:"embedding_model" gorm:"size:64;default:bge-m3"`
	EmbeddingDim            int                     `json:"embedding_dim" gorm:"default:1024"`
	LLMModel                string                  `json:"llm_model" gorm:"size:100;default:'Qwen2.5-3B-Instruct'"`
	LLMProviderConfig       LLMProviderConfig       `json:"llm_provider_config" gorm:"embedded;embeddedPrefix:llm_"`
	EmbeddingProviderConfig EmbeddingProviderConfig `json:"embedding_provider_config" gorm:"embedded;embeddedPrefix:emb_"`
	RerankProviderConfig    RerankProviderConfig    `json:"rerank_provider_config" gorm:"embedded;embeddedPrefix:rerank_"`
	Temperature             float64                 `json:"temperature" gorm:"default:0.7"`
	MaxTokens               int                     `json:"max_tokens" gorm:"default:1000"`
	TopP                    float64                 `json:"top_p" gorm:"default:0.9"`
	FrequencyPenalty        float64                 `json:"frequency_penalty" gorm:"default:0.5"`
	PresencePenalty         float64                 `json:"presence_penalty" gorm:"default:0.5"`
	ResponseFormat          string                  `json:"response_format" gorm:"size:50;default:json_object"`
	SystemPrompt            string                  `json:"system_prompt" gorm:"type:text"`
	TopK                    int                     `json:"top_k" gorm:"default:5"`
	ChunkSize               int                     `json:"chunk_size" gorm:"default:800"`
	ChunkOverlap            int                     `json:"chunk_overlap" gorm:"default:100"`
	SimilarityThreshold     float64                 `json:"similarity_threshold" gorm:"default:0.6"`
	IsActive                bool                    `json:"is_active" gorm:"default:true"`
	Status                  int                     `json:"status" gorm:"default:1"`
	DocCount                int                     `json:"doc_count" gorm:"default:0"`
	ChunkCount              int64                   `json:"chunk_count" gorm:"default:0"`
	LastImportAt            *time.Time              `json:"last_import_at"`
	LastSearchAt            *time.Time              `json:"last_search_at"`
	SearchCount             int64                   `json:"search_count" gorm:"default:0"`
	IntentClassification    string                  `json:"intent_classification" gorm:"type:text;column:intent_classification"`
	EnabledIntents          string                  `json:"enabled_intents" gorm:"type:text;column:enabled_intents"`
	IntentSOPMap            string                  `json:"intent_sop_map" gorm:"type:text;column:intent_sop_map"`
	CreatedAt               time.Time               `json:"created_at"`
	UpdatedAt               time.Time               `json:"updated_at"`
}

// TableName 指定表名
func (RagProduct) TableName() string {
	return "rag_products"
}

// IntentConfig 精细意图识别配置（运行时反序列化 IntentClassification）
type IntentConfig struct {
	Mode               string            `json:"mode"`
	EnabledIntents     []string          `json:"enabled_intents,omitempty"`
	KeyIntentThreshold float64           `json:"key_intent_threshold"`
	EnableLLMFallback  bool              `json:"enable_llm_fallback"`
	KeyIntentSOPMap    map[string]string `json:"key_intent_sop_map,omitempty"`
}

// DefaultIntentConfig 默认意图配置
func DefaultIntentConfig() IntentConfig {
	return IntentConfig{
		Mode:               "all",
		EnabledIntents:     []string{},
		KeyIntentThreshold: 0.6,
		EnableLLMFallback:  true,
		KeyIntentSOPMap: map[string]string{
			"price_objection":      "objection_price_v1",
			"quality_objection":    "objection_quality_v1",
			"purchase_intent":      "purchase_intent_v1",
			"trust_objection":      "objection_trust_v1",
			"competitor_objection": "objection_competitor_v1",
			"discount_request":     "price_discount_v1",
			"refund_request":       "after_sale_refund_v1",
		},
	}
}

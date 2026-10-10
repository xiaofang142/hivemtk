package llm

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// =============================================================================
// 全局 Embedding 提供商手动配置
//
// 背景：本地推理栈（llama.cpp bge-m3）在 2GB 小内存服务器上跑不动，而
// DefaultConfig 只认 config.yaml / 环境变量——改提供商要登服务器改 .env 重启。
// 本文件把「全局 embedding 指向」写入 system_config_kv（key = embedding_global，经 SystemConfigKVRepository），
// 管理端 API 在线改，DefaultConfig 在内置默认之前读取覆盖。
//
// 优先级（高→低）：
//  1. 每知识库覆盖（rag_products 的 EmbeddingProviderConfig，已有机制）
//  2. 本文件的全局手动配置（enabled=true 且 base_url 非空时生效）
//  3. config.yaml embedding 段
//  4. 环境变量 EMBEDDING_*
//  5. 内置默认（127.0.0.1:8208 本地栈）
//
// 维度仍强制 1024：pgvector vector(1024) 兼容是硬约束，换云端提供商必须选
// 1024 维模型（bge-m3 / text-embedding-v3 等），其余维度一律强制回落。
// =============================================================================

const embeddingGlobalKVKey = "embedding_global"

// GlobalEmbeddingOverride 全局 embedding 手动配置
type GlobalEmbeddingOverride struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

var (
	embeddingOverrideMu       sync.RWMutex
	embeddingOverrideCache    *GlobalEmbeddingOverride
	embeddingOverrideCacheAt  time.Time
	embeddingOverrideCacheTTL = 60 * time.Second
)

// GetGlobalEmbeddingOverride 读全局手动配置（60s 进程内缓存；DB 未就绪返回 nil）。
func GetGlobalEmbeddingOverride() *GlobalEmbeddingOverride {
	embeddingOverrideMu.RLock()
	cached, at := embeddingOverrideCache, embeddingOverrideCacheAt
	embeddingOverrideMu.RUnlock()
	if cached != nil && time.Since(at) < embeddingOverrideCacheTTL {
		return cached
	}
	kv := repository.NewSystemConfigKVRepository()
	raw, err := kv.Get(context.Background(), embeddingGlobalKVKey)
	if err != nil || raw == "" {
		return nil
	}
	var out GlobalEmbeddingOverride
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[embedding] 全局手动配置解析失败 key=%s: %v", embeddingGlobalKVKey, err)
		return nil
	}
	embeddingOverrideMu.Lock()
	embeddingOverrideCache = &out
	embeddingOverrideCacheAt = time.Now()
	embeddingOverrideMu.Unlock()
	out.APIKey = decryptAPIKeyForUse(out.APIKey)
	return &out
}

// SetGlobalEmbeddingOverride 写全局手动配置（API key 落库前走统一加密）。
func SetGlobalEmbeddingOverride(o *GlobalEmbeddingOverride) error {
	if o == nil {
		return nil
	}
	stored := *o
	if stored.APIKey != "" {
		stored.APIKey = encryptAPIKeyForStorage(stored.APIKey)
	}
	blob, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	kv := repository.NewSystemConfigKVRepository()
	if _, err = kv.Upsert(context.Background(), embeddingGlobalKVKey, string(blob)); err != nil {
		return err
	}
	embeddingOverrideMu.Lock()
	embeddingOverrideCache = nil
	embeddingOverrideMu.Unlock()
	return nil
}

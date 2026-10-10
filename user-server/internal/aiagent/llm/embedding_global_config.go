package llm

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/pkg/utils/logger"
)

// =============================================================================
// 全局 Embedding 提供商手动配置
//
// 背景：本地推理栈（llama.cpp bge-m3）在 2GB 小内存服务器上跑不动，而
// DefaultConfig 只认 config.yaml / 环境变量——改提供商要登服务器改 .env 重启。
// 本文件把「全局 embedding 指向」存进参数中心（config_params 的
// embedding.global_override，整份 JSON），管理端 API 在线改，DefaultConfig
// 在内置默认之前读取覆盖。
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
//
// 为什么是两条注入接缝而不是直接调参数中心：本包不能 import internal/service
// （service 依赖本包取 LLM，反向依赖会成环），所以读写口由装配层注入闭包。
// 读口每次调用都回源参数中心，本层**不再自建缓存**：上一版在这里另有一份 60s
// 进程内缓存，叠在参数中心那份 60s 之上，最坏情况改完配置要等两分钟才生效，
// 而写侧刚刚把参数中心的缓存失效掉——第二层缓存把"在线改、免重启"这句话打了对折。
// =============================================================================

// GlobalEmbeddingOverride 全局 embedding 手动配置。
//
// 落库的是这份结构的 JSON；api_key 那一格在 marshal 之前已经过 encryptAPIKeyForStorage，
// 所以参数中心页面上看到的是密文（secrets 未就绪时按既有语义原样存明文，
// 与 llm_providers 那套是同一把尺子）。
type GlobalEmbeddingOverride struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

var (
	embeddingOverrideProvider func(context.Context) string
	embeddingOverrideWriter   func(context.Context, string, uint) error
)

// ErrEmbeddingOverrideUnwired 写口没注入时 SetGlobalEmbeddingOverride 的报错。
//
// 只在装配缺失时出现。这里不回落"直接写库"：那正是本文件刚拆掉的裸 SQL 直查，
// 留着它等于给旧路径留一条后门。
var ErrEmbeddingOverrideUnwired = errors.New("embedding 全局配置: 参数中心写口未注入（装配缺失），本次未保存")

// SetEmbeddingOverrideProvider 注入读取口（参数中心 embedding.global_override）。
// 传 nil 视为不注入，GetGlobalEmbeddingOverride 回 nil。
func SetEmbeddingOverrideProvider(fn func(context.Context) string) {
	embeddingOverrideProvider = fn
}

// SetEmbeddingOverrideWriter 注入写入口。第三个参数是操作者 id，
// 由参数中心那一侧写进变更审计——本层原来那条 INSERT 没有任何人来路记录，
// "是谁把向量服务指到外部端点的"这件事今天才查得到。
func SetEmbeddingOverrideWriter(fn func(context.Context, string, uint) error) {
	embeddingOverrideWriter = fn
}

// GetGlobalEmbeddingOverride 读全局手动配置；没接线、没配置或 JSON 读不出时回 nil。
//
// 回 nil 而不是回一个零值结构：调用侧判的是 `o != nil && o.Enabled`，
// 零值结构在语义上等于"有一份配置、它是关的"，与"没有配置"不是一件事。
func GetGlobalEmbeddingOverride() *GlobalEmbeddingOverride {
	p := embeddingOverrideProvider
	if p == nil {
		return nil
	}
	raw := p(context.Background())
	if raw == "" {
		return nil
	}
	var out GlobalEmbeddingOverride
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[embedding] 参数中心 embedding.global_override 解析失败: %v", err)
		return nil
	}
	out.APIKey = decryptAPIKeyForUse(out.APIKey)
	return &out
}

// SetGlobalEmbeddingOverride 写全局手动配置（API key 落库前走统一加密）。
//
// actorID 为 0 表示"没有会话可归属"（例如装配期自检），此时仍然落库、
// 审计里那条记成系统来路，而不是静默丢掉这次写入。
func SetGlobalEmbeddingOverride(o *GlobalEmbeddingOverride, actorID uint) error {
	if o == nil {
		return nil
	}
	w := embeddingOverrideWriter
	if w == nil {
		return ErrEmbeddingOverrideUnwired
	}
	stored := *o
	if stored.APIKey != "" {
		stored.APIKey = encryptAPIKeyForStorage(stored.APIKey)
	}
	blob, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return w(context.Background(), string(blob), actorID)
}

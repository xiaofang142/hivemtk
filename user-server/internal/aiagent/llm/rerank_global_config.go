package llm

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/pkg/utils/logger"
)

// =============================================================================
// 全局 Rerank 提供商手动配置
//
// 背景：与 embedding.global_override 同构——SaaS 兼容要求 rerank API 连接信息
// 禁止走环境变量（多租户各自配置、平台端在线改）。本文件把「全局 rerank
// 指向」存进参数中心（config_params 的 rerank.global_override，整份 JSON），
// 管理端 API 在线改，DefaultRerankConfig 在内置默认之前读取覆盖。
//
// 优先级（高→低，env 层已移除）：
//  1. 每知识库覆盖（rag_products 的 rerank_* 列，已有机制）
//  2. 本文件的全局手动配置（enabled=true 且 base_url 非空时生效）
//  3. config.yaml inference.rerank 段
//  4. 内置默认
// =============================================================================

// GlobalRerankOverride 全局 rerank 手动配置。
//
// 落库的是这份结构的 JSON；api_key 那一格在 marshal 之前已经过 encryptAPIKeyForStorage，
// 所以参数中心页面上看到的是密文（secrets 未就绪时按既有语义原样存明文，
// 与 llm_providers 那套是同一把尺子）。
type GlobalRerankOverride struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	// Protocol 重排端点协议：空/openai={base}/v1/rerank，tei={base}/rerank，
	// dashscope=DashScope 原生 text-rerank 端点。与 config.yaml inference.rerank.protocol 同义。
	Protocol string `json:"protocol,omitempty"`
}

var (
	rerankOverrideProvider func(context.Context) string
	rerankOverrideWriter   func(context.Context, string, uint) error
)

// ErrRerankOverrideUnwired 写口没注入时 SetGlobalRerankOverride 的报错（仅在装配缺失时出现）。
// 这里不回落"直接写库"：留后门等于绕过参数中心与审计。
var ErrRerankOverrideUnwired = errors.New("rerank 全局配置: 参数中心写口未注入（装配缺失），本次未保存")

// SetRerankOverrideProvider 注入读取口（参数中心 rerank.global_override）。
// 传 nil 视为不注入，GetGlobalRerankOverride 回 nil。
func SetRerankOverrideProvider(fn func(context.Context) string) {
	rerankOverrideProvider = fn
}

// SetRerankOverrideWriter 注入写入口。第三个参数是操作者 id，
// 由参数中心那一侧写进变更审计（记录"是谁把重排服务指到外部端点的"）。
func SetRerankOverrideWriter(fn func(context.Context, string, uint) error) {
	rerankOverrideWriter = fn
}

// GetGlobalRerankOverride 读全局手动配置；没接线、没配置或 JSON 读不出时回 nil。
//
// 回 nil 而不是回一个零值结构：调用侧判的是 `o != nil && o.Enabled`，
// 零值结构在语义上等于"有一份配置、它是关的"，与"没有配置"不是一件事。
func GetGlobalRerankOverride() *GlobalRerankOverride {
	p := rerankOverrideProvider
	if p == nil {
		return nil
	}
	raw := p(context.Background())
	if raw == "" {
		return nil
	}
	var out GlobalRerankOverride
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[rerank] 参数中心 rerank.global_override 解析失败: %v", err)
		return nil
	}
	out.APIKey = decryptAPIKeyForUse(out.APIKey)
	return &out
}

// SetGlobalRerankOverride 写全局手动配置（API key 落库前走统一加密）。
//
// actorID 为 0 表示"没有会话可归属"（例如装配期自检），此时仍然落库、
// 审计里那条记成系统来路，而不是静默丢掉这次写入。
func SetGlobalRerankOverride(o *GlobalRerankOverride, actorID uint) error {
	if o == nil {
		return nil
	}
	w := rerankOverrideWriter
	if w == nil {
		return ErrRerankOverrideUnwired
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

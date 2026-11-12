package llm

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/pkg/utils/logger"
)

// =============================================================================
// 全局 LLM 提供商手动配置
//
// 背景：SaaS 兼容要求 LLM API 连接信息禁止走环境变量（多租户各自配置、
// 平台端在线改，env 重启才生效且无法按租户隔离）。本文件把「全局 LLM
// 指向」存进参数中心（config_params 的 llm.global_override，整份 JSON），
// 管理端 API 在线改，GetDefaultConfig 在内置默认之前读取覆盖。
//
// 优先级（高→低，env 层已移除）：
//  1. 每知识库 / 每租户覆盖（rag_products / ai_agents 的 llm_* 列，已有机制）
//  2. 本文件的全局手动配置（enabled=true 且 base_url 非空时生效）
//  3. config.yaml inference.llm 段
//  4. 内置默认
//
// 为什么是两条注入接缝而不是直接调参数中心：本包不能 import internal/service
// （service 依赖本包取 LLM，反向依赖会成环），所以读写口由装配层注入闭包。
// 读口每次调用都回源参数中心，本层不再自建缓存（叠缓存会让改配置的生效时间翻倍）。
// =============================================================================

// GlobalLLMOverride 全局 LLM 手动配置。
//
// 落库的是这份结构的 JSON；api_key 那一格在 marshal 之前已经过 encryptAPIKeyForStorage，
// 所以参数中心页面上看到的是密文（secrets 未就绪时按既有语义原样存明文，
// 与 llm_providers 那套是同一把尺子）。
type GlobalLLMOverride struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	// APIType 协议：openai/azure/anthropic（与 llm_providers.api_type 同义），
	// 决定 endpoint 构造方式（llm.go 的 switch）。
	APIType string `json:"api_type,omitempty"`
}

var (
	llmOverrideProvider func(context.Context) string
	llmOverrideWriter   func(context.Context, string, uint) error
)

// ErrLLMOverrideUnwired 写口没注入时 SetGlobalLLMOverride 的报错（仅在装配缺失时出现）。
// 这里不回落"直接写库"：留后门等于绕过参数中心与审计。
var ErrLLMOverrideUnwired = errors.New("llm 全局配置: 参数中心写口未注入（装配缺失），本次未保存")

// SetLLMOverrideProvider 注入读取口（参数中心 llm.global_override）。
// 传 nil 视为不注入，GetGlobalLLMOverride 回 nil。
func SetLLMOverrideProvider(fn func(context.Context) string) {
	llmOverrideProvider = fn
}

// SetLLMOverrideWriter 注入写入口。第三个参数是操作者 id，
// 由参数中心那一侧写进变更审计（记录"是谁把 LLM 指向外部端点的"）。
func SetLLMOverrideWriter(fn func(context.Context, string, uint) error) {
	llmOverrideWriter = fn
}

// GetGlobalLLMOverride 读全局手动配置；没接线、没配置或 JSON 读不出时回 nil。
//
// 回 nil 而不是回一个零值结构：调用侧判的是 `o != nil && o.Enabled`，
// 零值结构在语义上等于"有一份配置、它是关的"，与"没有配置"不是一件事。
func GetGlobalLLMOverride() *GlobalLLMOverride {
	p := llmOverrideProvider
	if p == nil {
		return nil
	}
	raw := p(context.Background())
	if raw == "" {
		return nil
	}
	var out GlobalLLMOverride
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[llm] 参数中心 llm.global_override 解析失败: %v", err)
		return nil
	}
	out.APIKey = decryptAPIKeyForUse(out.APIKey)
	return &out
}

// SetGlobalLLMOverride 写全局手动配置（API key 落库前走统一加密）。
//
// actorID 为 0 表示"没有会话可归属"（例如装配期自检），此时仍然落库、
// 审计里那条记成系统来路，而不是静默丢掉这次写入。
func SetGlobalLLMOverride(o *GlobalLLMOverride, actorID uint) error {
	if o == nil {
		return nil
	}
	w := llmOverrideWriter
	if w == nil {
		return ErrLLMOverrideUnwired
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

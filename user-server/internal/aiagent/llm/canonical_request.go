package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// CanonicalMessage 是 adapter 渲染后的协议中立消息；顺序由调用方确定性给出。
type CanonicalMessage struct {
	Role     string            `json:"role"`
	Content  string            `json:"content"`
	Name     string            `json:"name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CanonicalTool 保留工具定义顺序；顺序变化会改变供应商缓存前缀。
type CanonicalTool struct {
	Name       string `json:"name"`
	SchemaJSON string `json:"schema_json,omitempty"`
}

// CanonicalPromptRequest 把可缓存前缀与每轮动态尾部拆开，供 prompt cache 稳定性测试。
type CanonicalPromptRequest struct {
	Model           string             `json:"model"`
	SystemPrompt    string             `json:"system_prompt"`
	Tools           []CanonicalTool    `json:"tools,omitempty"`
	StableMessages  []CanonicalMessage `json:"stable_messages,omitempty"`
	DynamicMessages []CanonicalMessage `json:"dynamic_messages,omitempty"`
	Metadata        map[string]string  `json:"metadata,omitempty"`
	ToolChoice      string             `json:"tool_choice,omitempty"`
	ResponseFormat  string             `json:"response_format,omitempty"`
}

type canonicalPrefixPayload struct {
	Model          string             `json:"model"`
	SystemPrompt   string             `json:"system_prompt"`
	Tools          []CanonicalTool    `json:"tools,omitempty"`
	StableMessages []CanonicalMessage `json:"stable_messages,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	ToolChoice     string             `json:"tool_choice,omitempty"`
	ResponseFormat string             `json:"response_format,omitempty"`
}

func hashCanonicalValue(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}

// PrefixHashBytes 返回 adapter 完成重排与元数据渲染后的实际稳定前缀 hash。
func (r *CanonicalPromptRequest) PrefixHashBytes() ([]byte, error) {
	if r == nil || r.Model == "" {
		return nil, errors.New("canonical request 缺少模型")
	}
	payload := canonicalPrefixPayload{
		Model: r.Model, SystemPrompt: r.SystemPrompt, Tools: r.Tools,
		StableMessages: r.StableMessages, Metadata: r.Metadata,
		ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat,
	}
	return hashCanonicalValue(payload)
}

// PrefixHash 给日志与观测使用；非法请求返回空串，调用方必须先用 PrefixHashBytes 校验。
func (r *CanonicalPromptRequest) PrefixHash() string {
	raw, err := r.PrefixHashBytes()
	if err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}

// FullRequestHashBytes 覆盖稳定前缀与动态尾部，用于幂等和 transcript 去重。
func (r *CanonicalPromptRequest) FullRequestHashBytes() ([]byte, error) {
	if r == nil || r.Model == "" {
		return nil, errors.New("canonical request 缺少模型")
	}
	return hashCanonicalValue(struct {
		Prefix  canonicalPrefixPayload `json:"prefix"`
		Dynamic []CanonicalMessage     `json:"dynamic_messages,omitempty"`
	}{
		Prefix: canonicalPrefixPayload{
			Model: r.Model, SystemPrompt: r.SystemPrompt, Tools: r.Tools,
			StableMessages: r.StableMessages, Metadata: r.Metadata,
			ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat,
		},
		Dynamic: r.DynamicMessages,
	})
}

// FullRequestHash 返回完整请求 hash 的十六进制形式，用于观测与幂等键。
func (r *CanonicalPromptRequest) FullRequestHash() string {
	raw, err := r.FullRequestHashBytes()
	if err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}

// PromptCachePolicy 是供应商 prompt cache 策略，不代表答案缓存或供应商 native session。
type PromptCachePolicy struct {
	Mode       string `json:"mode"`
	Enabled    bool   `json:"enabled"`
	MinTokens  int    `json:"min_tokens,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// PromptCachePolicyFromCapabilities 保守识别 implicit/explicit；其他模式（含 provider_session_cache）不伪造 prompt cache 收益。
func PromptCachePolicyFromCapabilities(caps ProviderConfig) PromptCachePolicy {
	policy := PromptCachePolicy{Mode: caps.CacheMode}
	switch caps.CacheMode {
	case "implicit", "explicit":
		policy.Enabled = caps.CacheMinTokens > 0 && caps.CacheTTLSeconds > 0
		policy.MinTokens = caps.CacheMinTokens
		policy.TTLSeconds = caps.CacheTTLSeconds
	}
	return policy
}

package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrUnsupportedSession 表示供应商/协议不支持原生 session，不能静默续接。
	ErrUnsupportedSession = errors.New("provider does not support native session")
	// ErrUnsupportedProviderField 表示请求携带了当前能力不支持的字段。
	ErrUnsupportedProviderField = errors.New("provider capability does not support requested field")
	// ErrProviderResponseMalformed 表示供应商响应无法解析。
	ErrProviderResponseMalformed = errors.New("provider response malformed")
)

// ProviderRequestShape 是发给供应商之前的规范化请求形状；不伪装厂商不支持的字段。
type ProviderRequestShape struct {
	Model             string
	Messages          []ChatMessage
	MaxTokens         int
	HasSession        bool
	HasIdempotencyKey bool
	CacheMode         string
	UnsupportedFields []string
}

// BuildProviderRequestShape 把通用调用转换为当前 provider 能力允许的请求形状。
// 原生 session 不被支持时直接拒绝，避免 DeepSeek 之类 stateless 协议静默忽略字段。
func BuildProviderRequestShape(cfg LLMConfig, caps ProviderCapabilities) (ProviderRequestShape, error) {
	return buildProviderRequestShape(cfg, caps, true)
}

// BuildProviderRequestShapeForDispatch 用于生产调度：canonical session key 在
// stateless 协议中仅作本地幂等/路由/transcript 关联，不伪称供应商原生续接，
// 因此不支持的 session 记为报告字段而不是硬失败。
func BuildProviderRequestShapeForDispatch(cfg LLMConfig, caps ProviderCapabilities) (ProviderRequestShape, error) {
	return buildProviderRequestShape(cfg, caps, false)
}

func buildProviderRequestShape(cfg LLMConfig, caps ProviderCapabilities, strictSession bool) (ProviderRequestShape, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return ProviderRequestShape{}, fmt.Errorf("%w: model is empty", ErrUnsupportedProviderField)
	}
	shape := ProviderRequestShape{
		Model:             cfg.Model,
		Messages:          append([]ChatMessage{}, cfg.Messages...),
		MaxTokens:         cfg.MaxTokens,
		HasSession:        strings.TrimSpace(cfg.LLMSessionRef) != "",
		HasIdempotencyKey: strings.TrimSpace(cfg.RequestIdempotencyKey) != "",
		CacheMode:         caps.CacheMode,
		UnsupportedFields: []string{},
	}
	if shape.HasSession && (!caps.NativeSessionAllowed || caps.SessionMode == SessionModeNone) {
		if strictSession {
			return ProviderRequestShape{}, fmt.Errorf("%w: session ref for model %s", ErrUnsupportedSession, cfg.Model)
		}
		shape.UnsupportedFields = append(shape.UnsupportedFields, "native_session")
	}
	if shape.HasIdempotencyKey && !caps.SupportsIdempotencyKey {
		shape.UnsupportedFields = append(shape.UnsupportedFields, "request_idempotency_key")
	}
	if strings.TrimSpace(cfg.PromptPrefixHash) != "" && caps.CacheMode == CacheModeNone {
		shape.UnsupportedFields = append(shape.UnsupportedFields, "prompt_prefix_hash")
	}
	if shape.MaxTokens > 0 && caps.MaxOutputTokens > 0 && shape.MaxTokens > caps.MaxOutputTokens {
		shape.MaxTokens = caps.MaxOutputTokens
	}
	return shape, nil
}

// ProviderUsage 归一化不同厂商的 token 用量；cached tokens 仍占上下文窗口。
type ProviderUsage struct {
	PromptTokens        int
	CompletionTokens    int
	TotalTokens         int
	CachedInputTokens   int
	CacheCreationTokens int
}

// NormalizedTotal 在无 total 字段时退回输入+输出。
func (u ProviderUsage) NormalizedTotal() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// NormalizeProviderUsage 解析 OpenAI/Anthropic/Gemini 风格的 usage。
func NormalizeProviderUsage(raw []byte) (ProviderUsage, error) {
	var envelope struct {
		Usage         json.RawMessage `json:"usage"`
		UsageMetadata json.RawMessage `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ProviderUsage{}, fmt.Errorf("%w: %v", ErrProviderResponseMalformed, err)
	}
	usageBytes := envelope.Usage
	if len(usageBytes) == 0 {
		usageBytes = envelope.UsageMetadata
	}
	if len(usageBytes) == 0 {
		return ProviderUsage{}, nil
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(usageBytes, &usage); err != nil {
		return ProviderUsage{}, fmt.Errorf("%w: usage: %v", ErrProviderResponseMalformed, err)
	}
	out := ProviderUsage{
		PromptTokens:        intField(usage, "prompt_tokens", "input_tokens", "promptTokenCount"),
		CompletionTokens:    intField(usage, "completion_tokens", "output_tokens", "candidatesTokenCount"),
		TotalTokens:         intField(usage, "total_tokens", "totalTokenCount"),
		CachedInputTokens:   intField(usage, "cached_tokens", "cached_content_token_count", "cache_read_input_tokens"),
		CacheCreationTokens: intField(usage, "cache_creation_input_tokens"),
	}
	for _, key := range []string{"prompt_tokens_details", "input_tokens_details"} {
		var details map[string]json.RawMessage
		if err := json.Unmarshal(usage[key], &details); err == nil {
			out.CachedInputTokens += intField(details, "cached_tokens")
		}
	}
	return out, nil
}

func intField(fields map[string]json.RawMessage, keys ...string) int {
	for _, key := range keys {
		raw := fields[key]
		if len(raw) == 0 {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return 0
}

// NormalizeFinishReason 把厂商 finish/stop 原因归一到内部有限集合。
func NormalizeFinishReason(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "stop", "end_turn", "eos", "complete":
		return "stop"
	case "length", "max_tokens", "max_tokens_reached", "max_output_tokens":
		return "length"
	case "tool_calls", "tool_use", "function_call":
		return "tool_calls"
	case "content_filter", "safety", "recitation":
		return "content_filter"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// ProviderResponse 是被归一化后的供应商响应，保留真实 request id 与 continuation ref。
type ProviderResponse struct {
	Content         string
	FinishReason    string
	Usage           ProviderUsage
	RequestID       string
	ContinuationRef string
}

// ParseProviderResponse 解析 OpenAI/Anthropic/Gemini 响应；未知协议按 malformed 拒绝。
func ParseProviderResponse(raw []byte) (ProviderResponse, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: %v", ErrProviderResponseMalformed, err)
	}
	usage, err := NormalizeProviderUsage(raw)
	if err != nil {
		return ProviderResponse{}, err
	}
	out := ProviderResponse{
		Usage:           usage,
		RequestID:       stringField(envelope, "id", "request_id"),
		ContinuationRef: stringField(envelope, "id", "response_id", "last_response_id"),
	}

	if choicesRaw := envelope["choices"]; len(choicesRaw) > 0 {
		var choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}
		if err := json.Unmarshal(choicesRaw, &choices); err != nil || len(choices) == 0 {
			return ProviderResponse{}, fmt.Errorf("%w: choices", ErrProviderResponseMalformed)
		}
		out.Content = extractText(choices[0].Message.Content)
		out.FinishReason = NormalizeFinishReason(choices[0].FinishReason)
		return out, nil
	}
	if contentRaw := envelope["content"]; len(contentRaw) > 0 {
		out.Content = extractText(contentRaw)
		out.FinishReason = NormalizeFinishReason(stringField(envelope, "stop_reason", "stop_reason"))
		return out, nil
	}
	if candidatesRaw := envelope["candidates"]; len(candidatesRaw) > 0 {
		var candidates []struct {
			Content      json.RawMessage `json:"content"`
			FinishReason string          `json:"finishReason"`
		}
		if err := json.Unmarshal(candidatesRaw, &candidates); err != nil || len(candidates) == 0 {
			return ProviderResponse{}, fmt.Errorf("%w: candidates", ErrProviderResponseMalformed)
		}
		out.Content = extractGeminiText(candidates[0].Content)
		out.FinishReason = NormalizeFinishReason(candidates[0].FinishReason)
		return out, nil
	}
	return ProviderResponse{}, fmt.Errorf("%w: no recognized content", ErrProviderResponseMalformed)
}

func stringField(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw := fields[key]
		if len(raw) == 0 {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && value != "" {
			return value
		}
	}
	return ""
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type string          `json:"type"`
		Text json.RawMessage `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var builder strings.Builder
		for _, block := range blocks {
			if block.Type == "text" || block.Type == "" {
				builder.WriteString(extractText(block.Text))
			}
		}
		return builder.String()
	}
	return ""
}

func extractGeminiText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var object struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range object.Parts {
		builder.WriteString(part.Text)
	}
	return builder.String()
}

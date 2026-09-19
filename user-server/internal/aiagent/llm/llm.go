package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
)

// LLMConfig LLM配置
type LLMConfig struct {
	APIKey           string
	BaseURL          string
	APIType          string
	Model            string
	MaxRetries       int
	RequestTimeout   int
	Temperature      float64
	MaxTokens        int
	TopP             float64
	FrequencyPenalty float64
	PresencePenalty  float64
	ResponseFormat   string
	SystemPrompt     string
	Logprobs         bool
	TopLogprobs      int
	Tools            []ToolDefinition
	ToolChoice       string
	Messages         []ChatMessage
}

// ToolDefinition OpenAI 兼容的工具定义
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

// ToolFunctionSchema 工具函数 schema
type ToolFunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatMessage 通用对话消息（支持 user/assistant/tool/system 角色）
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall OpenAI 兼容的工具调用结构
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 工具调用 function 部分
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LLMServiceInterface LLM服务接口
type LLMServiceInterface interface {
	Generate(ctx context.Context, config *LLMConfig, prompt string) (string, error)
	GenerateWithTools(ctx context.Context, config *LLMConfig, prompt string) (*GenerateResult, error)
	GenerateStructured(ctx context.Context, config *LLMConfig, prompt string, responseSchema any) (any, error)
	ValidateConfig(config *LLMConfig) error
	GetDefaultConfig() *LLMConfig
}

// LLMService LLM服务实现
// 支持 OpenAI 兼容协议（OpenAI/Azure OpenAI/DeepSeek/通义千问/Moonshot 等）
type LLMService struct {
	httpClient *http.Client
}

var defaultHTTPTimeout = 180 * time.Second

func setDefaultHTTPTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	defaultHTTPTimeout = d
}

// NewLLMService 创建新的LLM服务
func NewLLMService() *LLMService {
	return &LLMService{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

type chatRequest struct {
	Model            string           `json:"model"`
	Messages         []chatMessage    `json:"messages"`
	Temperature      float64          `json:"temperature,omitempty"`
	MaxTokens        int              `json:"max_tokens,omitempty"`
	TopP             float64          `json:"top_p,omitempty"`
	FrequencyPenalty float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64          `json:"presence_penalty,omitempty"`
	ResponseFormat   map[string]any   `json:"response_format,omitempty"`
	Stream           bool             `json:"stream"`
	Tools            []map[string]any `json:"tools,omitempty"`
	ToolChoice       any              `json:"tool_choice,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role              string         `json:"role"`
			Content           string         `json:"content"`
			ReasoningContent  string         `json:"reasoning,omitempty"`
			ReasoningContent2 string         `json:"reasoning_content,omitempty"`
			ToolCalls         []chatToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func applyEnvDefaults(cfg *LLMConfig) {
	if cfg == nil {
		return
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("LLM_API_KEY")
	}
	if cfg.BaseURL == "" {
		if v := os.Getenv("LLM_BASE_URL"); v != "" {
			cfg.BaseURL = v
		}
	}
	if cfg.Model == "" || cfg.Model == "gpt-3.5-turbo" {
		if v := os.Getenv("LLM_MODEL"); v != "" {
			cfg.Model = v
		}
	}
}

// GenerateResult LLM 调用结果（智能体支持）
//
// 字段含义：
//   - Content：文本内容（finish_reason=stop/length 时为最终回复；
//     finish_reason=tool_calls 时可能为空或仅是 LLM 的中间说明）
//   - ToolCalls：LLM 决定调用的工具列表（finish_reason=tool_calls 时非空）
//   - FinishReason：stop / length / tool_calls / content_filter
//   - Usage：token 用量统计
type GenerateResult struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        TokenUsage
}

// Generate 生成文本回复（向后兼容）。
//
// 注意：当 config.Tools 非空时，LLM 可能返回 tool_calls 而非文本内容；
// 此时本方法只返回 content（可能为空字符串）。需要获取 tool_calls 的调用方
// 请使用 GenerateWithTools。
func (s *LLMService) Generate(ctx context.Context, config *LLMConfig, prompt string) (string, error) {
	result, err := s.GenerateWithTools(ctx, config, prompt)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func sanitizeToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (s *LLMService) GenerateWithTools(ctx context.Context, config *LLMConfig, prompt string) (*GenerateResult, error) {
	applyEnvDefaults(config)
	if err := s.ValidateConfig(config); err != nil {
		return nil, err
	}

	var messages []chatMessage
	if len(config.Messages) > 0 {
		messages = make([]chatMessage, 0, len(config.Messages))
		for _, m := range config.Messages {
			nm := m.Name
			if nm != "" {
				nm = sanitizeToolName(nm)
			}
			messages = append(messages, chatMessage{
				Role:       m.Role,
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
				Name:       nm,
				ToolCalls:  toChatToolCalls(m.ToolCalls),
			})
		}
	} else {
		messages = []chatMessage{}
		if config.SystemPrompt != "" {
			messages = append(messages, chatMessage{Role: "system", Content: config.SystemPrompt})
		}
		messages = append(messages, chatMessage{Role: "user", Content: prompt})
	}

	reqBody := chatRequest{
		Model:            config.Model,
		Messages:         messages,
		Temperature:      config.Temperature,
		MaxTokens:        config.MaxTokens,
		TopP:             config.TopP,
		FrequencyPenalty: config.FrequencyPenalty,
		PresencePenalty:  config.PresencePenalty,
		Stream:           false,
	}
	if config.ResponseFormat == "json_object" {
		reqBody.ResponseFormat = map[string]any{"type": "json_object"}
	}

	toolNameMap := make(map[string]string)
	if len(config.Tools) > 0 {
		reqBody.Tools = make([]map[string]any, 0, len(config.Tools))
		for _, t := range config.Tools {
			fnType := t.Type
			if fnType == "" {
				fnType = "function"
			}
			safe := sanitizeToolName(t.Function.Name)
			toolNameMap[safe] = t.Function.Name
			logger.Infof("[LLM] tool: name=%s desc_len=%d params=%v",
				t.Function.Name, len(t.Function.Description), t.Function.Parameters != nil)
			reqBody.Tools = append(reqBody.Tools, map[string]any{
				"type": fnType,
				"function": map[string]any{
					"name":        safe,
					"description": t.Function.Description,
					"parameters":  t.Function.Parameters,
				},
			})
		}
		choice := config.ToolChoice
		if choice == "" {
			choice = "auto"
		}
		if choice == "auto" || choice == "none" || choice == "required" {
			reqBody.ToolChoice = choice
		} else if strings.HasPrefix(choice, "{") {
			var obj map[string]any
			if err := json.Unmarshal([]byte(choice), &obj); err == nil {
				if fn, ok := obj["function"].(map[string]any); ok {
					if nm, ok := fn["name"].(string); ok && nm != "" {
						fn["name"] = sanitizeToolName(nm)
					}
				}
				reqBody.ToolChoice = obj
			} else {
				reqBody.ToolChoice = "auto"
			}
		} else {
			reqBody.ToolChoice = "auto"
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	logger.Infof("[LLM] request: model=%s tools=%d tool_choice=%v messages=%d body_len=%d config_tools=%d config_toolchoice=%q",
		config.Model, len(reqBody.Tools), reqBody.ToolChoice, len(messages), len(bodyBytes), len(config.Tools), config.ToolChoice)

	if debugDir := os.Getenv("LLM_DEBUG_DUMP_DIR"); debugDir != "" && len(reqBody.Tools) > 0 {
		if err := os.MkdirAll(debugDir, 0700); err == nil {
			_ = os.WriteFile(filepath.Join(debugDir, "llm_with_tools.json"), bodyBytes, 0600)
		}
	}

	resp, err := s.callProvider(ctx, config, bodyBytes)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM: model=%s", config.Model)
	}

	choice := resp.Choices[0]
	content := choice.Message.Content
	if content == "" {
		// 绝不回退取 reasoning/reasoning_content：那是模型思考过程，不是给客户看的回复。
		// 曾经回退导致「用户要求简单回复一个字。但根据系统指令…」这类推理文本经 polish 后
		// 原样投递给真实客户（message_hub id=432）。保持为空，由上游空回复兜底处理。
		if r := choice.Message.ReasoningContent + choice.Message.ReasoningContent2; r != "" {
			runes := []rune(r)
			head := runes
			if len(head) > 40 {
				head = head[:40]
			}
			logger.Warnf("[LLM] 模型只返回思考文本无正式内容，按空回复处理（不外发推理文本）model=%s reasoning_len=%d head=%q",
				config.Model, len(runes), string(head))
		}
	}
	result := &GenerateResult{
		Content:      content,
		FinishReason: choice.FinishReason,
		Usage: TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			name := tc.Function.Name
			if orig, ok := toolNameMap[name]; ok {
				name = orig
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: ToolCallFunction{
					Name:      name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return result, nil
}

func toChatToolCalls(tcs []ToolCall) []chatToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]chatToolCall, 0, len(tcs))
	for _, tc := range tcs {
		out = append(out, chatToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: chatToolCallFunc{
				Name:      sanitizeToolName(tc.Function.Name),
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return out
}

// GenerateStructured 生成结构化回复
func (s *LLMService) GenerateStructured(ctx context.Context, config *LLMConfig, prompt string, responseSchema any) (any, error) {
	schemaJSON, _ := json.Marshal(responseSchema)
	structuredPrompt := fmt.Sprintf("%s\n\n请严格按照以下 JSON Schema 输出响应（仅输出合法 JSON，不要其他说明文字）：\n%s", prompt, string(schemaJSON))

	cfg := *config
	if cfg.ResponseFormat != "json_object" {
		cfg.ResponseFormat = "json_object"
	}

	rawJSON, err := s.Generate(ctx, &cfg, structuredPrompt)
	if err != nil {
		return nil, err
	}

	rawJSON = extractJSON(rawJSON)
	if rawJSON == "" {
		return nil, fmt.Errorf("no JSON content in LLM response")
	}

	result := responseSchema
	if result == nil {
		var generic map[string]any
		if err := json.Unmarshal([]byte(rawJSON), &generic); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		return generic, nil
	}
	if err := json.Unmarshal([]byte(rawJSON), result); err != nil {
		return nil, fmt.Errorf("parse JSON to schema: %w", err)
	}
	return result, nil
}

func (s *LLMService) callProvider(ctx context.Context, config *LLMConfig, body []byte) (*chatResponse, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		switch config.APIType {
		case "azure":
			baseURL = "https://api.openai.azure.com"
		case "anthropic":
			baseURL = "https://api.anthropic.com"
		default:
			baseURL = "https://api.openai.com"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")

	endpoint := baseURL + "/v1/chat/completions"
	if config.APIType == "azure" {
		endpoint = fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=2024-02-01", baseURL, config.Model)
	} else if config.APIType == "anthropic" {
		endpoint = baseURL + "/v1/messages"
	}

	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		switch config.APIType {
		case "anthropic":
			req.Header.Set("x-api-key", config.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		default:
			req.Header.Set("Authorization", "Bearer "+config.APIKey)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			logger.Errorf("[LLM] request error (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: status=%d body=%s", resp.StatusCode, string(respBody))
			logger.Errorf("[LLM] server error (attempt %d/%d): %s", attempt+1, maxRetries, string(respBody))
			continue
		}

		if resp.StatusCode != http.StatusOK {

			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, &RateLimitError{
					StatusCode: resp.StatusCode,
					RetryAfter: parseRetryAfterSeconds(resp.Header.Get("Retry-After")),
					Body:       string(respBody),
				}
			}
			return nil, fmt.Errorf("LLM API error: status=%d body=%s", resp.StatusCode, string(respBody))
		}

		var chatResp chatResponse
		if err := json.Unmarshal(respBody, &chatResp); err != nil {
			return nil, fmt.Errorf("decode response: %w (body=%s)", err, string(respBody))
		}

		if chatResp.Error != nil {
			return nil, fmt.Errorf("LLM API error: %s", chatResp.Error.Message)
		}

		return &chatResp, nil
	}

	return nil, fmt.Errorf("LLM request failed after %d attempts: %w", maxRetries, lastErr)
}

// ValidateConfig 验证配置
func (s *LLMService) ValidateConfig(config *LLMConfig) error {
	if config == nil {
		return fmt.Errorf("config is required")
	}
	if config.APIKey == "" {
		logger.Warnf("[llm] WARN: APIKey empty for base_url=%s (local model is fine; cloud will 401)", config.BaseURL)
	}
	if config.Model == "" {
		return fmt.Errorf("model is required")
	}

	if config.MaxRetries <= 0 {
		config.MaxRetries = 1
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60
	}
	if config.Temperature < 0 || config.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if config.MaxTokens <= 0 {
		config.MaxTokens = 2048
	}
	return nil
}

// GetDefaultConfig 获取默认配置（本地优先）
//
// 优先级：config.yaml inference.llm > 环境变量 LLM_* > 内置本地默认（127.0.0.1:8207）。
// 默认即本地 mtk-llm（llama.cpp，OpenAI 兼容）；用户配置线上 base_url+api_key 即切线上。
func (s *LLMService) GetDefaultConfig() *LLMConfig {
	inf := config.GetAppConfig().Inference.LLM
	baseURL := inf.BaseURL
	model := inf.Model
	apiKey := inf.APIKey
	if baseURL == "" {
		baseURL = os.Getenv("LLM_BASE_URL")
	}
	if baseURL == "" {
		baseURL = config.DefaultLLMBaseURLDev
	}
	if model == "" {
		if v := os.Getenv("LLM_MODEL"); v != "" {
			model = v
		}
	}
	if model == "" {
		model = config.DefaultLLMModel()
	}
	if apiKey == "" {
		apiKey = os.Getenv("LLM_API_KEY")
	}
	return &LLMConfig{
		Model:            model,
		APIType:          "openai",
		BaseURL:          baseURL,
		APIKey:           apiKey,
		MaxRetries:       3,
		RequestTimeout:   60,
		Temperature:      0.7,
		MaxTokens:        2048,
		TopP:             0.9,
		FrequencyPenalty: 0.5,
		PresencePenalty:  0.5,
		ResponseFormat:   "json_object",
	}
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	var start int
	if startObj == -1 && startArr == -1 {
		return ""
	}
	if startObj == -1 {
		start = startArr
	} else if startArr == -1 {
		start = startObj
	} else if startObj < startArr {
		start = startObj
	} else {
		start = startArr
	}

	openCount := 0
	openCh := byte(s[start])
	var closeCh byte
	if openCh == '{' {
		closeCh = '}'
	} else {
		closeCh = ']'
	}

	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == openCh {
			openCount++
		} else if c == closeCh {
			openCount--
			if openCount == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

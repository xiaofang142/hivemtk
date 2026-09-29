package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/service"
)

// LLMAdapter 为 GEO 模块提供 LLM 访问，复用 hivemtk 全局 Dispatcher
// （DB 配置 + 场景路由 + 缓存 + 故障转移 + 可观测性）
type LLMAdapter struct {
	dispatcher *llm.Dispatcher
}

// NewLLMAdapter 创建 GEO LLM 适配器
func NewLLMAdapter() *LLMAdapter {
	return &LLMAdapter{dispatcher: llm.GetGlobalDispatcher()}
}

// LLMResult LLM 调用结果
type LLMResult struct {
	Content      string
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
}

type modelPrice struct {
	aliases []string
	input   float64
	output  float64
}

var modelPrices = []modelPrice{
	{[]string{"gpt-4o-mini"}, 0.15, 0.6},
	{[]string{"gpt-4o"}, 2.5, 10},
	{[]string{"gpt-4.1-nano"}, 0.1, 0.4},
	{[]string{"gpt-4.1-mini"}, 0.4, 1.6},
	{[]string{"gpt-4.1"}, 2, 8},
	{[]string{"gpt-4-turbo"}, 10, 30},
	{[]string{"gpt-4"}, 30, 60},
	{[]string{"gpt-3.5"}, 0.5, 1.5},
	{[]string{"o1-preview", "o1-2024"}, 15, 60},
	{[]string{"o1-mini"}, 1.1, 4.4},
	{[]string{"o1"}, 15, 60},
	{[]string{"o3-mini", "o4-mini"}, 1.1, 4.4},
	{[]string{"o3"}, 10, 40},
	{[]string{"claude-3-opus", "claude-opus"}, 15, 75},
	{[]string{"claude-3-haiku"}, 0.25, 1.25},
	{[]string{"claude-3-5-sonnet", "claude-sonnet", "claude-3-7"}, 3, 15},
	{[]string{"claude-3-5-haiku", "claude-haiku"}, 0.8, 4},
	{[]string{"claude-sonnet-4"}, 3, 15},
	{[]string{"claude-opus-4"}, 15, 75},
	{[]string{"claude-haiku-4"}, 0.8, 4},
	{[]string{"deepseek-reasoner", "deepseek-r1"}, 0.55, 2.19},
	{[]string{"deepseek-chat", "deepseek-v3"}, 0.27, 1.1},
	{[]string{"deepseek-v2"}, 0.14, 0.28},
	{[]string{"qwen-max"}, 1.6, 6.4},
	{[]string{"qwen-plus"}, 0.4, 1.2},
	{[]string{"qwen-turbo", "qwen-flash"}, 0.05, 0.2},
	{[]string{"qwen2.5-72b", "qwen2.5-32b", "qwen2.5-14b", "qwen2.5-7b"}, 0.5, 1.5},
	{[]string{"qwen2.5-1.5b", "qwen2.5-0.5b"}, 0.03, 0.06},
	{[]string{"glm-4-plus", "glm-4-0520"}, 7, 7},
	{[]string{"glm-4"}, 0.7, 0.7},
	{[]string{"glm-4-air", "glm-4-airx"}, 0.1, 0.1},
	{[]string{"glm-4-flash"}, 0, 0},
	{[]string{"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"}, 1, 2},
	{[]string{"moonshot-v1-8k-vision", "moonshot-v1-32k-vision"}, 2, 4},
	{[]string{"ernie-4.0", "ernie-4.0-turbo"}, 4, 16},
	{[]string{"ernie-3.5", "ernie-3.5-turbo"}, 1.2, 1.2},
	{[]string{"ernie-speed", "ernie-lite"}, 0.3, 0.3},
	{[]string{"doubao-pro", "doubao-pro-32k"}, 0.8, 2},
	{[]string{"doubao-lite", "doubao-lite-32k"}, 0.3, 0.6},
	{[]string{"doubao-embedding"}, 0.5, 0.5},
	{[]string{"kimi-k2", "moonshot-k2"}, 0.6, 2.5},
	{[]string{"kimi-k1.5", "moonshot-k1.5"}, 0.4, 1.6},
	{[]string{"gemini-2.0-flash"}, 0.1, 0.4},
	{[]string{"gemini-2.5-flash"}, 0.3, 2.5},
	{[]string{"gemini-2.5-pro"}, 1.25, 10},
	{[]string{"gemini-1.5-flash"}, 0.075, 0.3},
	{[]string{"gemini-1.5-pro"}, 1.25, 5},
	{[]string{"llama-3.1-405b"}, 2.5, 2.5},
	{[]string{"llama-3.1-70b"}, 0.5, 0.7},
	{[]string{"llama-3.1-8b"}, 0.05, 0.05},
	{[]string{"llama-3-70b"}, 0.59, 0.79},
	{[]string{"llama-3-8b"}, 0.06, 0.06},
	{[]string{"mistral-large"}, 2, 6},
	{[]string{"mistral-medium"}, 2.7, 5.1},
	{[]string{"mistral-small"}, 0.1, 0.3},
	{[]string{"mixtral-8x7b"}, 0.24, 0.24},
	{[]string{"mixtral-8x22b"}, 0.6, 0.6},
	{[]string{"command-r-plus"}, 2.5, 10},
	{[]string{"command-r"}, 0.15, 0.6},
	{[]string{"yi-large"}, 2, 2},
	{[]string{"yi-medium"}, 0.5, 0.5},
	{[]string{"yi-lightning"}, 0.14, 0.14},
}

const (
	fallbackPriceIn  = 1.0
	fallbackPriceOut = 2.0

	usdCnyRate = 7.2
)

// EstimateCostUSD 估算单次调用的美元成本（按内置定价表，未识别模型走兜底价）
func EstimateCostUSD(modelName string, inputTokens, outputTokens int) (float64, float64) {
	in, out := fallbackPriceIn, fallbackPriceOut
	name := strings.ToLower(modelName)
	for _, p := range modelPrices {
		matched := false
		for _, a := range p.aliases {
			if strings.Contains(name, a) {
				matched = true
				break
			}
		}
		if matched {
			in, out = p.input, p.output
			break
		}
	}
	cost := float64(inputTokens)/1e6*in + float64(outputTokens)/1e6*out
	return cost, cost * usdCnyRate
}

func geoMaxTokensPerCall() int {
	return service.GlobalConfigParam().GetInt(context.Background(), "agent_llm", "geo_max_tokens_per_call", 4000)
}

func (a *LLMAdapter) Generate(ctx context.Context, systemPrompt, prompt string, temperature float64, maxTokens int) (*LLMResult, error) {
	if a.dispatcher == nil {
		return nil, fmt.Errorf("LLM dispatcher 未初始化")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if maxTokens <= 0 || maxTokens > geoMaxTokensPerCall() {
		maxTokens = geoMaxTokensPerCall()
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		Temperature:  temperature,
		MaxTokens:    maxTokens,
	}
	resp, err := a.dispatcher.Dispatch(ctx, req)
	if err != nil {
		return nil, err
	}
	return &LLMResult{
		Content:      resp.Content,
		Provider:     resp.Provider,
		Model:        resp.Model,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

// GenerateJSON 生成 JSON 格式内容
func (a *LLMAdapter) GenerateJSON(ctx context.Context, systemPrompt, prompt string, maxTokens int) (*LLMResult, error) {
	if a.dispatcher == nil {
		return nil, fmt.Errorf("LLM dispatcher 未初始化")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if maxTokens <= 0 || maxTokens > geoMaxTokensPerCall() {
		maxTokens = geoMaxTokensPerCall()
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		Temperature:  0.3,
		MaxTokens:    maxTokens,
		JSONMode:     true,
	}
	resp, err := a.dispatcher.Dispatch(ctx, req)
	if err != nil {
		return nil, err
	}
	return &LLMResult{
		Content:      resp.Content,
		Provider:     resp.Provider,
		Model:        resp.Model,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

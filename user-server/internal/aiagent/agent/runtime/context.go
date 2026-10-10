package agent_runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ToolResult / ToolTiming 引用 portcontract 的唯一定义。这里曾各自定义过一份并漂移
// （少 error_code/card），同一份工具结果 JSON 在两条链路上读出不同字段集。
type ToolResult = portcontract.ToolResult

// ToolTiming 执行耗时统计
type ToolTiming = portcontract.ToolTiming

// Message 消息类型
type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// ContextCompressor 上下文压缩器接口
type ContextCompressor interface {
	Compress(messages []Message, maxTokens int) ([]Message, error)
	ShouldCompress(messages []Message, maxTokens int) bool
	EstimateTokens(messages []Message) int
}

// TokenEstimator Token估算器
type TokenEstimator interface {
	Estimate(text string) int
}

// SimpleTokenEstimator 简单Token估算器
// 基于经验规则：1个中文字符约2个token，1个英文单词约1.3个token
type SimpleTokenEstimator struct{}

// Estimate 估算token数量
func (e *SimpleTokenEstimator) Estimate(text string) int {
	if text == "" {
		return 0
	}

	runeCount := 0
	wordCount := 0
	inWord := false

	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			runeCount++
			inWord = false
		} else if r == ' ' || r == '\t' || r == '\n' {
			if inWord {
				wordCount++
				inWord = false
			}
		} else {
			inWord = true
		}
	}

	if inWord {
		wordCount++
	}

	return runeCount*2 + int(float64(wordCount)*1.3)
}

// LLMClient LLM客户端接口
type LLMClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// SummarizationCompressor 摘要压缩器
type SummarizationCompressor struct {
	llmClient LLMClient
	estimator TokenEstimator
	threshold float64
}

// NewSummarizationCompressor 创建摘要压缩器
func NewSummarizationCompressor(llmClient LLMClient, threshold float64) *SummarizationCompressor {
	if threshold <= 0 || threshold >= 1 {
		threshold = 0.8
	}
	return &SummarizationCompressor{
		llmClient: llmClient,
		estimator: &SimpleTokenEstimator{},
		threshold: threshold,
	}
}

// ShouldCompress 判断是否需要压缩
func (c *SummarizationCompressor) ShouldCompress(messages []Message, maxTokens int) bool {
	currentTokens := c.EstimateTokens(messages)
	thresholdTokens := int(float64(maxTokens) * c.threshold)
	return currentTokens > thresholdTokens
}

// EstimateTokens 估算消息的token数量
func (c *SummarizationCompressor) EstimateTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += c.estimator.Estimate(msg.Content)
		if msg.ToolResult != nil {
			total += c.estimator.Estimate(msg.ToolResult.Error)
			if data, ok := msg.ToolResult.Data.(string); ok {
				total += c.estimator.Estimate(data)
			}
		}
	}
	return total
}

// Compress 压缩历史消息
func (c *SummarizationCompressor) Compress(messages []Message, maxTokens int) ([]Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	if !c.ShouldCompress(messages, maxTokens) {
		return messages, nil
	}

	keepCount := len(messages) / 3
	if keepCount < 2 {
		keepCount = 2
	}
	if keepCount > len(messages) {
		keepCount = len(messages)
	}

	earlyMessages := messages[:len(messages)-keepCount]
	lateMessages := messages[len(messages)-keepCount:]

	summary, err := c.generateSummary(context.Background(), earlyMessages)
	if err != nil {
		logger.Warnf("[context_compressor] generate summary failed: %v", err)
		return c.truncateCompress(messages, maxTokens), nil
	}

	compressed := make([]Message, 0, keepCount+1)
	compressed = append(compressed, Message{
		Role:      "system",
		Content:   summary,
		Timestamp: time.Now(),
	})
	compressed = append(compressed, lateMessages...)

	return compressed, nil
}

func (c *SummarizationCompressor) generateSummary(ctx context.Context, messages []Message) (string, error) {
	if c.llmClient == nil {
		return "", fmt.Errorf("llm client is nil")
	}

	prompt := "请总结以下对话历史的关键信息：\n\n"
	for _, msg := range messages {
		role := msg.Role
		if role == "tool" {
			role = "assistant"
		}
		prompt += fmt.Sprintf("[%s] %s\n", role, msg.Content)
	}
	prompt += "\n请用简洁的中文总结上述对话的关键信息，包括：\n1. 用户的主要需求\n2. 已完成的操作\n3. 重要的上下文信息"

	summary, err := c.llmClient.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}

	return summary, nil
}

func (c *SummarizationCompressor) truncateCompress(messages []Message, maxTokens int) []Message {
	if len(messages) == 0 {
		return messages
	}

	keepCount := len(messages) / 3
	if keepCount < 2 {
		keepCount = 2
	}
	if keepCount > len(messages) {
		keepCount = len(messages)
	}

	return messages[len(messages)-keepCount:]
}

// ToolCallRecord 工具调用记录
type ToolCallRecord struct {
	CallID     string         `json:"call_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args"`
	Result     *ToolResult    `json:"result,omitempty"`
	ExecutedAt time.Time      `json:"executed_at"`
	Duration   time.Duration  `json:"duration"`
}

// ToolCallHistory 工具调用历史
type ToolCallHistory struct {
	Calls   []ToolCallRecord      `json:"calls"`
	Results map[string]ToolResult `json:"results"`
	mu      sync.RWMutex
}

// NewToolCallHistory 创建工具调用历史
func NewToolCallHistory() *ToolCallHistory {
	return &ToolCallHistory{
		Calls:   make([]ToolCallRecord, 0),
		Results: make(map[string]ToolResult),
	}
}

// AddCall 添加工具调用记录
func (h *ToolCallHistory) AddCall(record ToolCallRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.Calls = append(h.Calls, record)
	if record.Result != nil {
		h.Results[record.CallID] = *record.Result
	}
}

// GetCall 获取工具调用记录
func (h *ToolCallHistory) GetCall(callID string) (*ToolCallRecord, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for i := range h.Calls {
		if h.Calls[i].CallID == callID {
			return &h.Calls[i], true
		}
	}
	return nil, false
}

// GetResult 获取工具调用结果
func (h *ToolCallHistory) GetResult(callID string) (*ToolResult, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result, exists := h.Results[callID]
	if !exists {
		return nil, false
	}
	return &result, true
}

// GetCallsByTool 按工具名获取调用记录
func (h *ToolCallHistory) GetCallsByTool(toolName string) []ToolCallRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()

	records := make([]ToolCallRecord, 0)
	for _, record := range h.Calls {
		if record.ToolName == toolName {
			records = append(records, record)
		}
	}
	return records
}

// ToMessages 转换为消息列表
func (h *ToolCallHistory) ToMessages() []Message {
	h.mu.RLock()
	defer h.mu.RUnlock()

	messages := make([]Message, 0, len(h.Calls)*2)
	for _, call := range h.Calls {
		argsJSON, _ := json.Marshal(call.Args)
		messages = append(messages, Message{
			Role:    "assistant",
			Content: fmt.Sprintf("调用工具 %s: %s", call.ToolName, string(argsJSON)),
			ToolCalls: []ToolCall{
				{
					ID:   call.CallID,
					Name: call.ToolName,
					Args: call.Args,
				},
			},
			Timestamp: call.ExecutedAt,
		})

		if call.Result != nil {
			messages = append(messages, Message{
				Role:       "tool",
				Content:    call.Result.ToJSON(),
				ToolResult: call.Result,
				Timestamp:  call.ExecutedAt.Add(call.Duration),
			})
		}
	}

	return messages
}

// Clear 清空历史
func (h *ToolCallHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.Calls = make([]ToolCallRecord, 0)
	h.Results = make(map[string]ToolResult)
}

// Count 返回调用记录数量
func (h *ToolCallHistory) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.Calls)
}

// EnhancedInferenceContext 增强的推理上下文
type EnhancedInferenceContext struct {
	*InferenceContext
	ToolHistory *ToolCallHistory  `json:"tool_history"`
	Compressor  ContextCompressor `json:"-"`
	MaxTokens   int               `json:"max_tokens"`
}

// NewEnhancedInferenceContext 创建增强的推理上下文
func NewEnhancedInferenceContext(ic *InferenceContext, compressor ContextCompressor, maxTokens int) *EnhancedInferenceContext {
	return &EnhancedInferenceContext{
		InferenceContext: ic,
		ToolHistory:      NewToolCallHistory(),
		Compressor:       compressor,
		MaxTokens:        maxTokens,
	}
}

// AddToolCall 添加工具调用记录
func (c *EnhancedInferenceContext) AddToolCall(record ToolCallRecord) {
	c.ToolHistory.AddCall(record)
}

// GetCompressedHistory 获取压缩后的历史消息
func (c *EnhancedInferenceContext) GetCompressedHistory() ([]Message, error) {
	messages := c.ToolHistory.ToMessages()

	if c.Compressor != nil && c.Compressor.ShouldCompress(messages, c.MaxTokens) {
		return c.Compressor.Compress(messages, c.MaxTokens)
	}

	return messages, nil
}

package llm

import (
	"errors"
	"fmt"
)

// 未知 provider 能力时的保守窗口；不把“百万上下文”当作所有模型的共同属性。
const (
	defaultProviderContextWindow = 8192
	defaultProviderOutputReserve = 2048
	contextBudgetSafetyMargin    = 512
	minUsableContextBudget       = 256
)

// ContextBudget 把供应商登记能力收敛为一次调用可用的输入预算。
// 本地 token 估算只用于裁剪，不替代供应商的真实计量。
type ContextBudget struct {
	TotalContext       int
	MaxInputTokens     int
	ProtocolInputLimit int
	OutputReserve      int
	ReasoningReserve   int
	SafetyMargin       int
	EffectiveInput     int
	MaxOutputTokens    int
}

// NewContextBudget 按 ContextWindow - 输出预留 - reasoning - 安全余量，
// 再与模型输入上限、协议输入上限取最小值。任一项无法形成有效预算时 fail-closed。
func NewContextBudget(cfg ProviderConfig, outputReserve int) (*ContextBudget, error) {
	window := cfg.ContextWindow
	if window <= 0 {
		window = defaultProviderContextWindow
	}
	if outputReserve <= 0 {
		outputReserve = cfg.MaxOutputTokens
	}
	if outputReserve <= 0 {
		outputReserve = defaultProviderOutputReserve
	}
	if cfg.MaxOutputTokens > 0 && outputReserve > cfg.MaxOutputTokens {
		outputReserve = cfg.MaxOutputTokens
	}

	b := &ContextBudget{
		TotalContext:       window,
		MaxInputTokens:     cfg.MaxInputTokens,
		ProtocolInputLimit: cfg.ProtocolInputLimit,
		OutputReserve:      outputReserve,
		ReasoningReserve:   cfg.ReasoningReserveTokens,
		SafetyMargin:       contextBudgetSafetyMargin,
		MaxOutputTokens:    cfg.MaxOutputTokens,
	}
	if b.ReasoningReserve < 0 {
		return nil, errors.New("reasoning 预留不能为负")
	}

	available := window - outputReserve - b.ReasoningReserve - b.SafetyMargin
	if available < minUsableContextBudget {
		return nil, fmt.Errorf("上下文预算不可用：window=%d output=%d reasoning=%d", window, outputReserve, b.ReasoningReserve)
	}
	if b.MaxInputTokens > 0 && available > b.MaxInputTokens {
		available = b.MaxInputTokens
	}
	if b.ProtocolInputLimit > 0 && available > b.ProtocolInputLimit {
		available = b.ProtocolInputLimit
	}
	if available < minUsableContextBudget {
		return nil, fmt.Errorf("供应商输入上限过低：input=%d", available)
	}
	b.EffectiveInput = available
	return b, nil
}

// TotalInputBudget 返回裁剪后的实际输入预算。
func (b *ContextBudget) TotalInputBudget() int {
	if b == nil {
		return 0
	}
	return b.EffectiveInput
}

// ClampOutput 按供应商登记的最大输出和输入侧剩余空间夹取输出预留。
func (b *ContextBudget) ClampOutput(requested int) int {
	if b == nil || requested <= 0 {
		return 0
	}
	if b.MaxOutputTokens > 0 && requested > b.MaxOutputTokens {
		requested = b.MaxOutputTokens
	}
	if requested > b.OutputReserve {
		requested = b.OutputReserve
	}
	return requested
}

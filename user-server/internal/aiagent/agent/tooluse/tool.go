package tooluse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
)

// ToolCategory 工具分类
type ToolCategory string

const (
	CategoryCustomer       ToolCategory = "customer"
	CategoryReach          ToolCategory = "reach"
	CategoryPrivateMessage ToolCategory = "private_message"
	CategoryKnowledge      ToolCategory = "knowledge"
	CategoryBusiness       ToolCategory = "business"
)

// Tool 工具接口（所有工具必须实现）
type Tool interface {
	Name() string
	Category() ToolCategory
	Description() string
	Parameters() ToolParameters
	Execute(ctx context.Context, args map[string]any) (ToolResult, error)
}

// ToolParameters 工具参数 JSON Schema
type ToolParameters struct {
	Type        string               `json:"type"`
	Properties  map[string]ToolParam `json:"properties"`
	Required    []string             `json:"required,omitempty"`
	Definitions map[string]ToolParam `json:"definitions,omitempty"`
}

// ToolParam 单个参数定义
type ToolParam struct {
	Type        string               `json:"type"`
	Description string               `json:"description"`
	Enum        []string             `json:"enum,omitempty"`
	Default     any                  `json:"default,omitempty"`
	Items       *ToolParam           `json:"items,omitempty"`
	Properties  map[string]ToolParam `json:"properties,omitempty"`
	Ref         string               `json:"$ref,omitempty"`

	Required  []string `json:"required,omitempty"`
	MinLength int      `json:"minLength,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
}

// ToolResult 工具执行结果
type ToolResult struct {
	Success    bool            `json:"success"`
	Data       any             `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
	Timing     ToolTiming      `json:"timing"`
	ToolName   string          `json:"tool_name"`
	ExecutedAt time.Time       `json:"executed_at"`
	AuditTrace string          `json:"audit_trace,omitempty"`
	Card       *model.RichCard `json:"card,omitempty"`
}

// 工具失败分类枚举（D08）：随 ToolResult.error_code 回灌给 LLM。
// 语义约定：INVALID_PARAMS→修参重试；RATE_LIMITED/CIRCUIT_OPEN→等待或降级；
// PERMISSION_DENIED/APPROVAL_DENIED/DNC_BLOCKED→放弃该路径；其余→INTERNAL。
const (
	ToolErrInvalidParams    = "TOOL_INVALID_PARAMS"
	ToolErrRateLimited      = "TOOL_RATE_LIMITED"
	ToolErrPermissionDenied = "TOOL_PERMISSION_DENIED"
	ToolErrTimeout          = "TOOL_TIMEOUT"
	ToolErrPanic            = "TOOL_PANIC"
	ToolErrApprovalDenied   = "TOOL_APPROVAL_DENIED"
	ToolErrDNCBlocked       = "TOOL_DNC_BLOCKED"
	ToolErrCircuitOpen      = "TOOL_CIRCUIT_OPEN"
	ToolErrNotFound         = "TOOL_NOT_FOUND"
	ToolErrInternal         = "TOOL_INTERNAL"
)

// ClassifyToolError 将错误映射为机器可读分类（sentinel errors.Is 穿透 %w 包装链）。
// 是失败分类的唯一映射表：isNonRetryableError 基于其返回值判定，避免双套分类漂移。
func ClassifyToolError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrParamValidationFailed):
		return ToolErrInvalidParams
	case errors.Is(err, ErrRateLimited):
		return ToolErrRateLimited
	case errors.Is(err, ErrPermissionDenied):
		return ToolErrPermissionDenied
	case errors.Is(err, ErrToolTimeout), errors.Is(err, context.DeadlineExceeded):
		return ToolErrTimeout
	case errors.Is(err, ErrToolPanic):
		return ToolErrPanic
	case errors.Is(err, ErrApprovalDenied):
		return ToolErrApprovalDenied
	case errors.Is(err, ErrDNCBlocked):
		return ToolErrDNCBlocked
	case errors.Is(err, ErrCircuitOpen):
		return ToolErrCircuitOpen
	case errors.Is(err, ErrLoopDetected):
		return ToolErrInternal
	case errors.Is(err, context.Canceled):
		return ToolErrInternal
	}
	return ToolErrInternal
}

// ToolTiming 执行耗时统计
type ToolTiming struct {
	DurationMs int64 `json:"duration_ms"`
	RetryCount int   `json:"retry_count"`
}

// ToJSON 将 ToolResult 序列化为 JSON 字符串
func (r ToolResult) ToJSON() string {
	data, _ := json.Marshal(r)
	return string(data)
}

// BaseTool 工具基类（简化工具实现）
// 工具实现者嵌入 BaseTool 后只需实现 Execute 方法即可
type BaseTool struct {
	NameVal        string
	CategoryVal    ToolCategory
	DescriptionVal string
	ParamsVal      ToolParameters

	// RiskVal 是本工具的后果分级（见 tool_risk.go）。
	// 留空 == 未声明 == 按 high_write 处理，不会因嵌入了 BaseTool 就自动获得放行。
	RiskVal ToolRiskLevel
}

func (b *BaseTool) Name() string               { return b.NameVal }
func (b *BaseTool) Category() ToolCategory     { return b.CategoryVal }
func (b *BaseTool) Description() string        { return b.DescriptionVal }
func (b *BaseTool) Parameters() ToolParameters { return b.ParamsVal }

// RiskLevel 让嵌入了 BaseTool 的工具天然实现 RiskDeclared。
//
// 注意这与"实现了就等于声明了"是两件事：判据是返回值的字面量合不合法，
// 不是方法在不在（EffectiveRisk）。
func (b *BaseTool) RiskLevel() ToolRiskLevel { return b.RiskVal }

// LLMFunction LLM Function Calling 格式（OpenAI 兼容）
type LLMFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

// ToLLMFunction 将 Tool 转换为 LLM Function Calling 格式
func ToLLMFunction(t Tool) LLMFunction {
	return LLMFunction{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
	}
}

// ErrorResult 快速构造错误结果（ErrorCode 由 ClassifyToolError 统一判定）
func ErrorResult(toolName string, err error) ToolResult {
	return ToolResult{
		Success:    false,
		Error:      err.Error(),
		ErrorCode:  ClassifyToolError(err),
		ToolName:   toolName,
		ExecutedAt: time.Now(),
	}
}

// SuccessResult 快速构造成功结果
func SuccessResult(toolName string, data any) ToolResult {
	return ToolResult{
		Success:    true,
		Data:       data,
		ToolName:   toolName,
		ExecutedAt: time.Now(),
	}
}

func (r ToolResult) withTiming(toolName string, start time.Time) ToolResult {
	r.ToolName = toolName
	r.ExecutedAt = time.Now()
	r.Timing = ToolTiming{DurationMs: time.Since(start).Milliseconds()}
	return r
}

// ValidateRequired 校验必填参数
func ValidateRequired(args map[string]any, required []string) error {
	for _, k := range required {
		v, ok := args[k]
		if !ok {
			return fmt.Errorf("缺少必填参数：%s", k)
		}
		if v == nil {
			return fmt.Errorf("参数 %s 不能为空", k)
		}
		if s, ok := v.(string); ok && s == "" {
			return fmt.Errorf("参数 %s 不能为空字符串", k)
		}
	}
	return nil
}

// GetStringArg 安全获取 string 参数
func GetStringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("参数 %s 不存在", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("参数 %s 类型错误，期望 string，实际 %T", key, v)
	}
	return s, nil
}

// GetIntArg 安全获取 int 参数（兼容 float64 JSON 数字）
func GetIntArg(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("参数 %s 不存在", key)
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("参数 %s 类型错误，期望 int，实际 %T", key, v)
	}
}

// GetFloatArg 安全获取 float64 参数
func GetFloatArg(args map[string]any, key string) (float64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("参数 %s 不存在", key)
	}
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("参数 %s 类型错误，期望 float，实际 %T", key, v)
	}
}

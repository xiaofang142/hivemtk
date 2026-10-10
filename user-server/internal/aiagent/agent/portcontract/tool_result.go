package portcontract

import (
	"encoding/json"
	"time"

	"hivemtk-user/internal/model"
)

// ToolResult 工具执行结果的唯一形状。定义在叶子包而不是 tooluse：agent_runtime 也要用它，
// 而 tooluse 经 knowledge/service 间接依赖 agent_runtime；两侧各自定义过一次并漂移（runtime
// 那份缺 error_code/card），方法也只能定义在这份定义上，消费方以类型别名引用。
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

// WithTiming 盖上工具名与本次执行耗时（值语义，返回副本）
func (r ToolResult) WithTiming(toolName string, start time.Time) ToolResult {
	r.ToolName = toolName
	r.ExecutedAt = time.Now()
	r.Timing = ToolTiming{DurationMs: time.Since(start).Milliseconds()}
	return r
}

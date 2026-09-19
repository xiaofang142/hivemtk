package tooluse

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/pkg/metrics"
)

var (
	toolCallTotal = metrics.NewCounter(
		"hivemtk_tool_calls_total",
		"Total tool calls",
		[]string{"tool_name", "success"},
	)

	toolCallLatencyMs = metrics.NewHistogram(
		"hivemtk_tool_call_latency_ms",
		"Tool call latency in ms",
		[]string{"tool_name"},
		[]float64{1, 5, 10, 50, 100, 500, 1000, 3000, 5000},
	)
)

type AuditLogger interface {
	Log(ctx context.Context, entry AuditEntry)
}

type AuditEntry struct {
	TraceID       string        `json:"trace_id,omitempty"`
	ToolName      string        `json:"tool_name"`
	CallerID      string        `json:"caller_id"`
	AgentID       string        `json:"agent_id,omitempty"`
	CustomerID    string        `json:"customer_id,omitempty"`
	SessionID     string        `json:"session_id,omitempty"`
	Success       bool          `json:"success"`
	Error         string        `json:"error,omitempty"`
	Duration      time.Duration `json:"duration_ms"`
	RetryCount    int           `json:"retry_count"`
	AuditTrace    string        `json:"audit_trace,omitempty"`
	ArgsSummary   string        `json:"args_summary,omitempty"`
	ResultSummary string        `json:"result_summary,omitempty"`
	ExecutedAt    time.Time     `json:"executed_at"`
}

type CostTracker interface {
	Record(ctx context.Context, toolName string, success bool, duration time.Duration) error
}

func AuditDecorator(logger AuditLogger, costTracker CostTracker) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			toolName := GetToolName(ctx)
			tc := GetToolContext(ctx)
			start := time.Now()

			result, err := next(ctx, args)
			duration := time.Since(start)

			if logger != nil {
				entry := AuditEntry{
					ToolName:   toolName,
					TraceID:    GetTraceID(ctx),
					CallerID:   "",
					AgentID:    "",
					CustomerID: "",
					SessionID:  "",
					Success:    err == nil && result.Success,
					Duration:   duration,
					RetryCount: result.Timing.RetryCount,
					ExecutedAt: start,
				}
				if err != nil {
					entry.Error = err.Error()
				} else if !result.Success {
					entry.Error = result.Error
				}
				if tc != nil {
					entry.CallerID = tc.CallerID
					entry.AgentID = tc.AgentID
					entry.CustomerID = tc.CustomerID
					entry.SessionID = tc.SessionID
					entry.AuditTrace = tc.AuditTrace
				}
				entry.ArgsSummary = summarizeArgs(args)

				entry.ResultSummary = summarizeResult(result.Data)

				logger.Log(ctx, entry)
			}

			if costTracker != nil {
				_ = costTracker.Record(ctx, toolName, err == nil && result.Success, duration)
			}

			recordToolCallMetrics(toolName, err, result, duration)

			return result, err
		}
	}
}

func recordToolCallMetrics(toolName string, err error, result ToolResult, duration time.Duration) {
	success := err == nil
	toolCallTotal.WithLabel(toolName, strconv.FormatBool(success)).Inc()
	toolCallLatencyMs.WithLabel(toolName).Observe(float64(duration.Milliseconds()))
}

func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	sensitiveKeys := map[string]bool{
		"password": true, "token": true, "secret": true,
		"api_key": true, "apikey": true, "phone": true,
		"id_card": true, "bank_card": true,
	}
	out := ""
	for k, v := range args {
		if sensitiveKeys[k] {
			out += fmt.Sprintf("%s=***,", k)
			continue
		}
		s := fmt.Sprintf("%v", v)
		if len(s) > 50 {
			s = cutBytes(s, 50) + "..."
		}
		out += fmt.Sprintf("%s=%s,", k, s)
	}
	if len(out) > 200 {
		out = cutBytes(out, 200) + "..."
	}
	return out
}

func summarizeResult(data any) string {
	if data == nil {
		return ""
	}
	s := fmt.Sprintf("%v", data)
	if len(s) > 200 {
		return cutBytes(s, 200) + "..."
	}
	return s
}

// cutBytes 按字节上限截断，并回退到最近的合法 rune 边界。
//
// 原来这里是 `s[:50]` / `s[:200]` 的裸字节切片。内存里这么砍没有任何后果，但本包现在
// 会把摘要**落库**（T-P1-08）：砍在多字节字符中间会产出非法 UTF-8，PG 的 TEXT 列直接
// 报 `invalid byte sequence for encoding "UTF8"`（实测），而 flushBatch 是整批
// CreateInBatches ⇒ 一条坏数据带走 100 条好审计。本项目参数与话术大量是中文，
// 命中概率不是尾数。
func cutBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

type NoOpAuditLogger struct{}

type NoOpCostTracker struct{}

// MemoryAuditLogger 内存审计日志（用于测试 / 单机审计）
type MemoryAuditLogger struct {
	mu      sync.Mutex
	entries []AuditEntry
	maxSize int
}

// NewMemoryAuditLogger 创建内存审计日志
// maxSize: 最大保留条数（超出后滚动覆盖最旧条目）
func NewMemoryAuditLogger(maxSize int) *MemoryAuditLogger {
	if maxSize < 1 {
		maxSize = 1000
	}
	return &MemoryAuditLogger{
		entries: make([]AuditEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// MemoryCostTracker 内存计费统计
type MemoryCostTracker struct {
	mu      sync.Mutex
	records map[string]*costRecord
}

type costRecord struct {
	TotalCalls      int64
	SuccessCalls    int64
	FailedCalls     int64
	TotalDurationMs int64
}

// NewMemoryCostTracker 创建内存计费统计
func NewMemoryCostTracker() *MemoryCostTracker {
	return &MemoryCostTracker{
		records: make(map[string]*costRecord),
	}
}

// CostStats 计费统计快照
type CostStats struct {
	ToolName        string  `json:"tool_name"`
	TotalCalls      int64   `json:"total_calls"`
	SuccessCalls    int64   `json:"success_calls"`
	FailedCalls     int64   `json:"failed_calls"`
	TotalDurationMs int64   `json:"total_duration_ms"`
	SuccessRate     float64 `json:"success_rate"`
	AvgDurationMs   float64 `json:"avg_duration_ms"`
}

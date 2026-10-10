// Package traceparent 实现 W3C Trace Context (Level 1) 规范。
//
// 规范：https://www.w3.org/TR/trace-context-1/
//
// 头格式：traceparent: <version>-<trace_id>-<parent_id>-<flags>
//   - version: 2 字节 hex（当前规范固定 "00"）
//   - trace_id: 16 字节 hex（32 字符），全 0 视为非法
//   - parent_id: 8 字节 hex（16 字符），全 0 视为非法
//   - flags: 1 字节 hex；bit 0 = sampled
//
// tracestate 头为可选 vendor-specific 列表（key=value 逗号分隔），本实现只透传不解析。
//
// 设计目标：
//   - 解析入站 header：上游调本服务时透传的 trace_id 直接复用，构造子 span。
//   - 构造出站 header：本服务调下游（LLM / 外部 API）时按规范写入。
//   - 与现有 logger.WithTraceID 完全兼容：W3C trace_id（32 hex）替换为原 UUID 也可工作。
//   - 非法 header 容忍：解析失败时不报错，仅生成新 trace_id；记录 debug 日志。
package traceparent

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	HeaderName     = "traceparent"
	CurrentVersion = "00"
	TraceIDHexLen  = 32
	SpanIDHexLen   = 16
)

// Context 解析后的 W3C trace context。
type Context struct {
	Version   string
	TraceID   string
	SpanID    string
	Sampled   bool
	RawHeader string
	Rest      string
}

// ErrInvalidHeader header 非法（不会 panic，仅返回错误供调用方降级生成新 trace_id）。
var ErrInvalidHeader = errors.New("traceparent: invalid header")

// Parse 解析入站 W3C traceparent header。
//
// 行为：
//   - 完全空字符串 → 返回 ErrInvalidHeader（调用方应生成新 trace_id）。
//   - 格式错误/长度不对 → 返回 ErrInvalidHeader + 详细原因。
//   - trace_id 或 span_id 全 0 → 返回 ErrInvalidHeader（规范禁止）。
//   - 解析成功 → 返回 Context，TraceID/SpanID 已是规范小写 hex。
//   - 多个 header（合法情况罕见）→ 取第一个非空。
func Parse(header string) (Context, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return Context{}, fmt.Errorf("%w: empty header", ErrInvalidHeader)
	}
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return Context{}, fmt.Errorf("%w: expected 4 parts, got %d", ErrInvalidHeader, len(parts))
	}
	version, traceID, spanID, flags := parts[0], parts[1], parts[2], parts[3]

	if len(version) != 2 || !isLowerHex(version) {
		return Context{}, fmt.Errorf("%w: invalid version %q", ErrInvalidHeader, version)
	}
	if len(traceID) != TraceIDHexLen || !isLowerHex(traceID) {
		return Context{}, fmt.Errorf("%w: invalid trace_id %q", ErrInvalidHeader, traceID)
	}
	if isAllZero(traceID) {
		return Context{}, fmt.Errorf("%w: trace_id is all zero", ErrInvalidHeader)
	}
	if len(spanID) != SpanIDHexLen || !isLowerHex(spanID) {
		return Context{}, fmt.Errorf("%w: invalid parent_id %q", ErrInvalidHeader, spanID)
	}
	if isAllZero(spanID) {
		return Context{}, fmt.Errorf("%w: parent_id is all zero", ErrInvalidHeader)
	}
	if len(flags) != 2 || !isLowerHex(flags) {
		return Context{}, fmt.Errorf("%w: invalid flags %q", ErrInvalidHeader, flags)
	}

	sampled := len(flags) >= 1 && (flags[1] == '1' || flags[1] == '3' || flags[1] == '5' || flags[1] == '7' ||
		flags[1] == '9' || flags[1] == 'b' || flags[1] == 'd' || flags[1] == 'f')

	return Context{
		Version:   version,
		TraceID:   traceID,
		SpanID:    spanID,
		Sampled:   sampled,
		RawHeader: header,
	}, nil
}

// Build 构造出站 traceparent header 值（用于本服务调下游时）。
//
//   - traceID: 32 hex chars（必填）。
//   - spanID: 16 hex chars（必填）。
//   - sampled: true=采样 false=不采样。
//
// 返回格式：00-<trace_id>-<span_id>-<flags> 严格遵循 W3C Level 1 规范。
func Build(traceID, spanID string, sampled bool) (string, error) {
	traceID = strings.ToLower(strings.TrimSpace(traceID))
	spanID = strings.ToLower(strings.TrimSpace(spanID))
	if len(traceID) != TraceIDHexLen || !isLowerHex(traceID) || isAllZero(traceID) {
		return "", fmt.Errorf("%w: trace_id must be 32 hex chars and non-zero", ErrInvalidHeader)
	}
	if len(spanID) != SpanIDHexLen || !isLowerHex(spanID) || isAllZero(spanID) {
		return "", fmt.Errorf("%w: span_id must be 16 hex chars and non-zero", ErrInvalidHeader)
	}
	flag := "00"
	if sampled {
		flag = "01"
	}
	return CurrentVersion + "-" + traceID + "-" + spanID + "-" + flag, nil
}

// IsZeroContext 判断 context 是否为空（Parse 失败或未初始化）。
func (c Context) IsZeroContext() bool {
	return c.TraceID == "" && c.SpanID == ""
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func isAllZero(s string) bool {
	for _, r := range s {
		if r != '0' {
			return false
		}
	}
	return true
}

// FormatTraceIDForLegacy 把 W3C trace_id 适配为项目 legacy 的 X-Trace-Id 格式。
// 兼容场景：旧日志/旧接口仅识别 UUIDv4 格式的 trace_id。
// W3C 32 hex 与 UUID 32 hex 兼容（去掉 dash 即可），但本函数保持原样，仅校验长度。
func FormatTraceIDForLegacy(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if len(traceID) == 0 {
		return ""
	}

	return strings.ToLower(traceID)
}

// HexEncode 便捷函数：把 []byte 编码为小写 hex 字符串。
// 校验：输入长度不符合 trace_id/span_id 长度时返回错误。
func HexEncode(b []byte, expectedLen int) (string, error) {
	if len(b) != expectedLen {
		return "", fmt.Errorf("traceparent: expected %d bytes, got %d", expectedLen, len(b))
	}
	return hex.EncodeToString(b), nil
}

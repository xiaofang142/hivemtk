package tooluse

import (
	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/pkg/tracing"
)

// redactToolTraceEvent 在写入 message_trace 前脱敏工具参数、输出与错误，不把原始内容变成第二份日志存储。
func redactToolTraceEvent(ev tracing.ToolTraceEvent) tracing.ToolTraceEvent {
	ev.Input = redactTraceValue(ev.Input)
	ev.Output = redactTraceValue(ev.Output)
	ev.Error = agentcontext.RedactForLog(ev.Error)
	return ev
}

func redactTraceValue(v any) any {
	switch typed := v.(type) {
	case nil:
		return nil
	case string:
		return agentcontext.RedactForLog(typed)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = redactTraceValue(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = redactTraceValue(value)
		}
		return out
	default:
		return v
	}
}

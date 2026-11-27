package service

import (
	agentcontext "hivemtk-user/internal/aiagent/context"
)

// redactSharedNotesForLog 在写日志前脱敏共享备注；完整内容只允许留在加密限权账本中。
func redactSharedNotesForLog(notes []string) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		out = append(out, agentcontext.RedactForLog(note))
	}
	return out
}

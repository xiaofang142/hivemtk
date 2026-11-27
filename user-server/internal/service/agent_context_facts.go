package service

import "strings"

// BusinessNoteLegacyUnverified 表示缺少明确来源、时间或确认状态的存量备注。
// 它只能作为待核实背景，绝不能升级为既成事实。
const BusinessNoteLegacyUnverified = "legacy_unverified"

// NormalizeBusinessNoteAttribute 对备注属性做 fail-closed 分类；未知属性按待核实处理。
func NormalizeBusinessNoteAttribute(attribute string) string {
	switch strings.TrimSpace(attribute) {
	case BusinessNoteFact:
		return BusinessNoteFact
	case BusinessNoteSuggestion:
		return BusinessNoteSuggestion
	default:
		return BusinessNoteLegacyUnverified
	}
}

// CanStateBusinessNoteAsFact 只有显式 fact 备注可被陈述为既成事实。
func CanStateBusinessNoteAsFact(note BusinessNote) bool {
	return NormalizeBusinessNoteAttribute(note.Attribute) == BusinessNoteFact
}

func businessNoteAttributeLabel(attribute string) string {
	switch NormalizeBusinessNoteAttribute(attribute) {
	case BusinessNoteFact:
		return "既成事实"
	case BusinessNoteSuggestion:
		return "建议（非已发生事实，不得当作既成事实）"
	default:
		return "待核实历史备注（非既成事实，不得当作已确认事实）"
	}
}

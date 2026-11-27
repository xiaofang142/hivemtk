package service

// aiGenerationFallbackDispositionReason 返回 AI 链路提前退出时必须落到逐消息台账的
// 终止原因。空串表示本次入站已经由投递出口作出明确处置，defer 不得再补写。
func aiGenerationFallbackDispositionReason(closed bool, err error, result *HandleResult) string {
	if closed {
		return ""
	}
	if err != nil {
		return "ai_failed"
	}
	if result == nil {
		return "ai_nil_result"
	}
	return ""
}

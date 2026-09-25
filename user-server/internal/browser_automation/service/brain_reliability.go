package service

import (
	"strings"

	"hivemtk-user/internal/browser_automation/dto"
)

// isRetryableLLMError LLM 调用错误分类（P0-3）：
// 可重试 = 限流/服务端错/网络/超时/JSON 抖动（有恢复提示救回）；
// 不可重试 = 鉴权/权限类（重试无意义，快败省预算）。
func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	nonRetryable := []string{"401", "403", "unauthorized", "invalid_api_key", "forbidden", "permission denied"}
	for _, k := range nonRetryable {
		if strings.Contains(msg, k) {
			return false
		}
	}
	retryable := []string{
		"429", "rate limit", "too many requests",
		"500", "502", "503", "504", "internal server error", "bad gateway", "service unavailable",
		"timeout", "deadline", "connection", "eof", "reset by peer",
		"parse json", "invalid character", "no json content",
	}
	for _, k := range retryable {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false // 未知错误保守不重试（快败）
}

// clampBrainStepParams Brain 下发步骤参数的服务端钳位（P0-4）：
// LLM 幻觉 retry_count=100 会被指数退避放大成数小时——服务端说了算。
func clampBrainStepParams(retryCount, backoffMs int) (int, int) {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 3 {
		retryCount = 3
	}
	if backoffMs <= 0 {
		backoffMs = 1000
	}
	if backoffMs > 10000 {
		backoffMs = 10000
	}
	return retryCount, backoffMs
}

// brainPlanStepRejection Brain 轮内步骤的服务端闸门（批19h）：返回非空=本步不执行、不落库，
// 文案原样进历史回喂下一轮；返回空串=放行。
//
// 判据来自 dto 的 oneof（REST 与 Brain 同源，见 dto/step_action.go）。旧写法只有两条腿：
// 空 action 静默跳过（模型收不到反馈，下轮照吐）、screenshot 单独硬闸（G17）——其余幻觉动作名
// 一路畅通到 dispatchStep 的 default，先建一条注定失败的 browser_steps 行、白烧一次 CDP 往返，
// 前台渲染成用户看不懂的红灯。拒它是信息：LLM 下一轮看见「这个动作名服务端不认识」才会换。
func brainPlanStepRejection(action string) string {
	if action == "screenshot" {
		// G17：captureVisibleTab 必然把 tab 切到用户前台抢焦点，Brain 轮内一律拒绝（文案逐字锁定）。
		return "screenshot → 被拒绝（Brain 模式禁止抢焦点截图，请用 snapshot/markdown 观察）"
	}
	if dto.IsKnownStepAction(action) {
		return ""
	}
	// 回显的是模型原文，长度不受我们控制：historyWindow 只按条数折叠不按字数，滑窗溢出的行
	// 还会拿首词当折叠台账的键名——几千字的幻觉串会同时撑大 prompt 和台账。
	label := truncateRunes(strings.TrimSpace(action), 40, "…")
	if label == "" {
		label = "<空 action>"
	}
	return label + " → 被拒绝（动作名不在可用 action 表内，服务端不认识；只允许 action 表里列出的动作）"
}

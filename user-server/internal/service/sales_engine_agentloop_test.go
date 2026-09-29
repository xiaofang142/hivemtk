package service

import (
	"strings"
	"testing"
)

// TestEmptyReplyFallback 守护铁律「LLM 返回空必须兜底」：
// runAgentLoop 在 LLM 返回空内容（无工具分支 / 最终文本为空 / 循环退出 lastResult 为空）时，
// 必须通过 emptyReplyFallback 返回非空友好提示，绝对禁止向用户展示空白回复。
// 纯函数、无 DB 依赖，作为空回复降级守卫的回归锚点。
func TestEmptyReplyFallback(t *testing.T) {
	se := &SalesEngine{}
	fallback := se.emptyReplyFallback()

	if strings.TrimSpace(fallback) == "" {
		t.Fatalf("emptyReplyFallback 返回了空白，违反「LLM 返回空必须兜底」铁律")
	}
	if !strings.Contains(fallback, "无法处理") {
		t.Fatalf("emptyReplyFallback 文案异常: %q", fallback)
	}
	// "请稍后再试" 是 scripts/simulate 认出"这条是降级而非真实应答"的识别锚
	// （simulate.py 的 AI_DEGRADED 判定、ai_quality.py 的 DEGRADED_MARKERS）：
	// 文案去掉它，压测会把降级回复统计成正常应答（假绿）。
	if !strings.Contains(fallback, "请稍后再试") {
		t.Fatalf("emptyReplyFallback 丢了压测降级识别锚: %q", fallback)
	}
	if !strings.Contains(fallback, "人工") {
		t.Fatalf("emptyReplyFallback 未给出人工出路，客户被关在死循环里: %q", fallback)
	}
	if se.emptyReplyFallback() != fallback {
		t.Fatal("emptyReplyFallback 非确定性")
	}
}

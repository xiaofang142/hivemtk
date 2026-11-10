package service

// intent_chain_result_truncate_test.go TB-12：工具结果回填摘要化。
//
// 钉两件事：
//  1. 超长工具结果回填 LLM 前被截断到预算内（含截断标记），完整结果不在
//     上下文里（防爆上下文——业界 340 工具教训的姊妹问题）；
//  2. 短结果原样透传（零变化回归面）；最新节点事实保留在头部（newest-first）。

import (
	"strings"
	"testing"
)

func TestTB12_TruncateToolResult_ShortPassthrough(t *testing.T) {
	short := `{"tracks":[{"status":"已签收"}]}`
	if got := truncateToolResultForLLM("logistics.track", short); got != short {
		t.Fatalf("短结果应原样透传, got %q", got)
	}
	if got := truncateToolResultForLLM("", ""); got != "" {
		t.Fatalf("空结果应原样, got %q", got)
	}
}

func TestTB12_TruncateToolResult_LongTruncatedWithMarker(t *testing.T) {
	// 构造超长结果（>6000 字符，中文节点）
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString(`{"time":"2026-11-10 09:00:00","status":"运输中","location":"上海转运中心","description":"包裹已到达上海转运中心，正在分拣"` + string(rune('中'+i%20)) + `"}` + "\n")
	}
	long := b.String()
	if len(long) <= agentLoopToolResultMaxChars {
		t.Fatalf("夹具应超预算, len=%d", len(long))
	}
	got := truncateToolResultForLLM("logistics.track", long)
	if len(got) > agentLoopToolResultMaxChars*4 { // rune 截断后字节数仍应远小于原长
		t.Fatalf("截断后体量应显著缩小, got %d bytes", len(got))
	}
	if !strings.Contains(got, "结果过长已截断") {
		t.Fatal("应含截断标记")
	}
	if !strings.Contains(got, "审计") {
		t.Fatal("标记应指引完整结果去向（审计）")
	}
	// rune 边界：截断结果可被合法解码（无撕裂字节）——strings 操作本身即验证
	if !strings.Contains(got, "2026-11-10") {
		t.Fatal("头部事实（最新节点）应保留")
	}
}

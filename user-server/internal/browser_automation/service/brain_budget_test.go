package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateRunes_UTF8Safety P0-1/V1：中文多字节截断不产生乱码
func TestTruncateRunes_UTF8Safety(t *testing.T) {
	s := strings.Repeat("成都火锅真好吃", 2000) // 10000 rune
	out := truncateRunes(s, 100, "…[cut]")
	if !utf8.ValidString(out) {
		t.Fatalf("截断后产生非法 UTF-8")
	}
	if got := utf8.RuneCountInString(out); got > 100+utf8.RuneCountInString("…[cut]") {
		t.Fatalf("超出预算: %d", got)
	}
	if !utf8.ValidString(truncateRunes("习惯一个繁体字與emoji😀混合", 5, "")) {
		t.Fatalf("混合字符截断非法")
	}
}

// TestBudgetInput_SnapshotCapped P0-1：大快照被裁剪并带标注
func TestBudgetInput_SnapshotCapped(t *testing.T) {
	big := strings.Repeat("菜单项 ", 20000) // 80k rune
	snap, stBudgeted := budgetInput(big, &reflectState{Memory: strings.Repeat("记", 1000)})
	if utf8.RuneCountInString(snap) > snapshotBudgetRunes+50 {
		t.Fatalf("快照未按预算裁剪: %d", utf8.RuneCountInString(snap))
	}
	if !strings.Contains(snap, "[快照过长已截断") {
		t.Fatalf("缺少截断标注")
	}
	// budgetInput 返回裁剪副本，不改动入参 st；marker "…" 记 1 rune，故上限 600+1
	if stBudgeted == nil || utf8.RuneCountInString(stBudgeted.Memory) > memoryBudgetRunes+utf8.RuneCountInString("…") {
		t.Fatalf("memory 未限长: %d", utf8.RuneCountInString(stBudgeted.Memory))
	}
}

// TestIsRetryableLLMError P0-3/V3：错误分类
func TestIsRetryableLLMError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"LLM 编排失败: Post \"http://x\": context deadline exceeded", true},
		{"LLM 编排失败: HTTP request failed: 429 Too Many Requests", true},
		{"LLM 编排失败: HTTP 500 Internal Server Error", true},
		{"LLM 编排失败: HTTP 502 Bad Gateway", true},
		{"LLM 编排失败: connection refused", true},
		{"LLM 编排失败: HTTP 401 Unauthorized: invalid_api_key", false},
		{"LLM 编排失败: HTTP 403 Forbidden", false},
		{"parse JSON: invalid character", true}, // JSON 抖动可重试（有恢复提示路径）
	}
	for _, c := range cases {
		if got := isRetryableLLMError(&testErr{c.msg}); got != c.want {
			t.Errorf("isRetryable(%q)=%v want %v", c.msg, got, c.want)
		}
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

// TestClampBrainStepParams P0-4/V4：LLM 下发参数钳位
func TestClampBrainStepParams(t *testing.T) {
	if rc, b := clampBrainStepParams(100, 999999); rc != 3 || b != 10000 {
		t.Fatalf("上限钳位失败: rc=%d b=%d", rc, b)
	}
	if rc, b := clampBrainStepParams(-5, 0); rc != 0 || b != 1000 {
		t.Fatalf("下限钳位失败: rc=%d b=%d", rc, b)
	}
	if rc, b := clampBrainStepParams(2, 500); rc != 2 || b != 500 {
		t.Fatalf("合法值被改动: rc=%d b=%d", rc, b)
	}
}

// TestLoopFingerprint P0-4 配套：循环指纹检测
func TestLoopFingerprint(t *testing.T) {
	stuck, _ := loopFingerprint([]string{"extract a", "extract b", "extract c", "extract a", "extract b", "extract c"})
	if !stuck {
		t.Fatalf("应检测到循环")
	}
	if stuck, _ := loopFingerprint([]string{"a", "b", "c", "a", "b", "d"}); stuck {
		t.Fatalf("不应误报")
	}
}

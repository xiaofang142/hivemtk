package service

import (
	"testing"
	"time"
)

// D5 补测：humanizedDelay 抖动分布与边界（铁律 2 拟人节奏的最低契约）。

func TestHumanizedDelay(t *testing.T) {
	if d := humanizedDelay(0); d != 0 {
		t.Errorf("base=0 应为 0，got %v", d)
	}
	if d := humanizedDelay(-5); d != 0 {
		t.Errorf("base<0 应为 0，got %v", d)
	}
	// base=1ms 时 span=0 → 恒等
	if d := humanizedDelay(1); d != time.Millisecond {
		t.Errorf("base=1ms got %v", d)
	}
	// 统计性检验：base=1000 的 200 次采样应全部落在 [700,1300]，且存在离散（不是恒定值）
	base := 1000
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		d := humanizedDelay(base)
		if d < 700*time.Millisecond || d > 1300*time.Millisecond {
			t.Fatalf("抖动越界 ±30%%: %v", d)
		}
		seen[d] = true
	}
	if len(seen) < 10 {
		t.Errorf("200 次采样仅 %d 个不同值，抖动退化为恒定", len(seen))
	}
}

func TestIsRetryableLLMErrorTable(t *testing.T) {
	// 补强 P0-3 分类表的模糊地带：eof/reset 属网络瞬断可重试；未知错误保守快败
	cases := map[string]bool{
		"read tcp: i/o timeout":    true,
		"EOF":                      true,
		"connection reset by peer": true,
		"429 Too Many Requests":    true,
		"503 service unavailable":  true,
		"unauthorized":             false,
		"invalid_api_key":          false,
		"some brand new error":     false,
	}
	for msg, want := range cases {
		got := isRetryableLLMError(errString(msg))
		if got != want {
			t.Errorf("isRetryableLLMError(%q)=%v want %v", msg, got, want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

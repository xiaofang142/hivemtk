package textutil

import (
	"strings"
	"testing"
)

// TestMaxBytesSeam 截断上限的接线三格。
func TestMaxBytesSeam(t *testing.T) {
	restore := maxBytesProvider
	t.Cleanup(func() { maxBytesProvider = restore })

	if got, want := DefaultMaxBytes, 8192; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.text_truncate_max_bytes 的 DefaultValue（%d）不一致", got, want)
	}

	SetMaxBytesProvider(nil)
	if got := DefaultTruncateMaxBytes(); got != DefaultMaxBytes {
		t.Fatalf("nil 注入时 = %v，期望回落 %d", got, DefaultMaxBytes)
	}

	SetMaxBytesProvider(func() int { return 64 })
	if got := DefaultTruncateMaxBytes(); got != 64 {
		t.Fatalf("注入后 = %v，期望 64", got)
	}
	// 非正数回落：0 会让 TruncateText 把每条都截成空串，比不接线更糟。
	SetMaxBytesProvider(func() int { return 0 })
	if got := DefaultTruncateMaxBytes(); got != DefaultMaxBytes {
		t.Fatalf("注入 0 时 = %v，期望回落 %d（0 会把文本全截没）", got, DefaultMaxBytes)
	}
}

// TestTruncateTextHonoursSeam 真正的读取点是截断本身。
func TestTruncateTextHonoursSeam(t *testing.T) {
	restore := maxBytesProvider
	t.Cleanup(func() { maxBytesProvider = restore })
	SetMaxBytesProvider(func() int { return 8 })

	in := "一二三四五六七八九十"
	// 截断后会补一段 "[truncated N bytes]" 后缀，所以结果可能比原文还长，
	// 判据只能是"尾注出现了"而不是"变短了"。
	if got := TruncateText(in, 0); !strings.Contains(got, "[truncated") {
		t.Fatalf("上限压到 8 字节后未走截断：%q", got)
	}
	if got := TruncateText(in, len(in)); strings.Contains(got, "[truncated") {
		t.Fatalf("显式传了足够大的上限时不该截断：%q", got)
	}
	// 上限取自接线时，尾注里的字节数必须按 8 算，不是按调用方传的 0。
	if got := TruncateText(in, 0); !strings.Contains(got, "[truncated 24 bytes]") {
		t.Fatalf("尾注字节数应按接线上限 8 计算，实际 %q", got)
	}
}

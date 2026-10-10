package service

import (
	"testing"
)

// positionDedupWindow 管的是「相邻两处品牌词命中的字符间隔」，不是条数、不是时间窗。
// 这个用例直接把去重行为钉死：窗口调大，「Apple 发布会」这种紧邻的两次提及就并成一次；
// 窗口调小，每次提及都被独立计数，品牌提及量成倍虚高。
func TestPositionDedupWindowSeam(t *testing.T) {
	reset := func() { SetPositionDedupWindowProvider(nil) }
	t.Cleanup(reset)
	reset()

	if got := positionDedupWindow(); got != DefaultPositionDedupWindow {
		t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, DefaultPositionDedupWindow)
	}

	SetPositionDedupWindowProvider(func() int { return 50 })
	if got := positionDedupWindow(); got != 50 {
		t.Errorf("注入 50 未生效：got %v", got)
	}

	SetPositionDedupWindowProvider(func() int { return 0 })
	if got := positionDedupWindow(); got != DefaultPositionDedupWindow {
		t.Errorf("注入 0 未回落兜底：got %v，期望 %v（0 会让每次命中都算独立提及）", got, DefaultPositionDedupWindow)
	}
	SetPositionDedupWindowProvider(func() int { return -5 })
	if got := positionDedupWindow(); got != DefaultPositionDedupWindow {
		t.Errorf("注入 -5 未回落兜底：got %v，期望 %v", got, DefaultPositionDedupWindow)
	}

	reset()
	if got := positionDedupWindow(); got != DefaultPositionDedupWindow {
		t.Errorf("传 nil 撤销注入后 = %v，期望 %v", got, DefaultPositionDedupWindow)
	}
}

func TestDedupMatchesRespectsPositionDedupWindow(t *testing.T) {
	reset := func() { SetPositionDedupWindowProvider(nil) }
	t.Cleanup(reset)
	reset()

	// 三处提及各自相隔 3 字符（{0,5} {8,13} {16,21}）。
	// 去重规则是「本次命中的起点 - 上一条被计入命中的终点 > 窗口」，
	// 所以间隔 3 在窗口 20 / 100 下都并成 1 次，在窗口 1 下则各算 1 次。
	spaced := []matchPos{{0, 5}, {8, 13}, {16, 21}}
	if got := dedupMatches(spaced); got != 1 {
		t.Fatalf("兜底窗口 20 下相隔 3 字符的三段应算 1 次提及，实际 %d", got)
	}

	// 窗口调到 100：最大间隔 11 < 100，仍并成 1 次——证明读点真的走了 seam。
	SetPositionDedupWindowProvider(func() int { return 100 })
	if got := dedupMatches(spaced); got != 1 {
		t.Errorf("窗口 100 下应仍为 1 次，实际 %d", got)
	}

	// 窗口调到 1：间隔 3 > 1，三段应各算 1 次。
	SetPositionDedupWindowProvider(func() int { return 1 })
	if got := dedupMatches(spaced); got != 3 {
		t.Errorf("窗口 1 下应算 3 次，实际 %d", got)
	}

	// 空输入不该因为窗口读点被调用而出问题。
	if got := dedupMatches(nil); got != 0 {
		t.Errorf("空输入应算 0 次，实际 %d", got)
	}
}

func TestProbePositionDedupWindowMatchesReader(t *testing.T) {
	reset := func() { SetPositionDedupWindowProvider(nil) }
	t.Cleanup(reset)
	reset()

	SetPositionDedupWindowProvider(func() int { return 13 })
	if positionDedupWindow() != 13 || ProbePositionDedupWindow() != 13 {
		t.Errorf("positionDedupWindow=%v ProbePositionDedupWindow=%v，应同为 13", positionDedupWindow(), ProbePositionDedupWindow())
	}
}

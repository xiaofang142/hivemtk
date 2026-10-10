package qq

import (
	"testing"
)

// TestQQMessageMaxLenSeam 覆盖 QQ 单条消息长度上限的 seam 四格。
// 第四格「setter 传 nil 是空操作」是本仓 seam 的统一语义：nil 表示"不注入"，
// 防的是装配顺序出错时把兜底值顶掉，不是"复位"。
// 复位只能由同包测试直接写包内变量完成，生产 setter 做不到。
func TestQQMessageMaxLenSeam(t *testing.T) {
	t.Cleanup(func() { qqMessageMaxLenProvider = nil })

	if got := QQMessageMaxLenEffective(); got != QQMessageMaxLen {
		t.Errorf("未注入时应等于兜底常量 %d, got %d", QQMessageMaxLen, got)
	}

	SetQQMessageMaxLenProvider(func() int { return 500 })
	if got := QQMessageMaxLenEffective(); got != 500 {
		t.Errorf("注入 500 后应读到 500, got %d", got)
	}

	SetQQMessageMaxLenProvider(func() int { return 0 })
	if got := QQMessageMaxLenEffective(); got != QQMessageMaxLen {
		t.Errorf("注入 0 应回落兜底 %d（0 会让每条消息都超限）, got %d", QQMessageMaxLen, got)
	}

	SetQQMessageMaxLenProvider(func() int { return -7 })
	if got := QQMessageMaxLenEffective(); got != QQMessageMaxLen {
		t.Errorf("注入负值应回落兜底 %d, got %d", QQMessageMaxLen, got)
	}

	SetQQMessageMaxLenProvider(func() int { return 777 })
	SetQQMessageMaxLenProvider(nil)
	if got := QQMessageMaxLenEffective(); got != 777 {
		t.Errorf("setter 传 nil 应是空操作，应保持 777, got %d", got)
	}
}

// TestSplitQQMessageRespectsSeam 证明读取点真的走 seam，而不是只读常量。
func TestSplitQQMessageRespectsSeam(t *testing.T) {
	t.Cleanup(func() { qqMessageMaxLenProvider = nil })

	text := "abcdefghij"
	want := func(limit int) []string { return splitQQMessage(text, limit) }

	def := want(QQMessageMaxLenEffective())
	if len(def) != 1 {
		t.Fatalf("兜底上限 2000 下不应分段, got %d 段", len(def))
	}

	SetQQMessageMaxLenProvider(func() int { return 4 })
	got := want(QQMessageMaxLenEffective())
	if len(got) != 3 {
		t.Errorf("上限 4 时应分成 3 段（4/4/2）, got %d 段: %v", len(got), got)
	}
	if len(got) > 0 && got[0] != "abcd" {
		t.Errorf("首段应为 abcd, got %q", got[0])
	}
}

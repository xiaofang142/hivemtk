package telegram

import (
	"testing"
)

// TestTGChannelSeams 覆盖 TG 三个上限参数的 seam 四格。
// 三者都是"超过即截断"的语义：注入 0/负值一律回落兜底，否则整条消息/整个键盘
// 会被截成空，比"上限写大了"更难排查。
// 复位只能由同包测试直接写包内变量完成——生产 setter 传 nil 是「不注入」的空操作。
func TestTGChannelSeams(t *testing.T) {
	t.Cleanup(func() {
		tgMessageMaxLenProvider = nil
		tgInlineRowsMaxProvider = nil
		tgInlineButtonsPerRowMaxProvider = nil
	})

	if got := TGMessageMaxLengthEffective(); got != TGMessageMaxLength {
		t.Errorf("未注入时应等于兜底常量 %d, got %d", TGMessageMaxLength, got)
	}
	if got := TGInlineRowsMaxEffective(); got != TGInlineRowsMax {
		t.Errorf("未注入时应等于兜底常量 %d, got %d", TGInlineRowsMax, got)
	}
	if got := TGInlineButtonsPerRowMaxEffective(); got != TGInlineButtonsPerRowMax {
		t.Errorf("未注入时应等于兜底常量 %d, got %d", TGInlineButtonsPerRowMax, got)
	}

	SetTGMessageMaxLenProvider(func() int { return 100 })
	SetTGInlineRowsMaxProvider(func() int { return 3 })
	SetTGInlineButtonsPerRowMaxProvider(func() int { return 2 })
	if got := TGMessageMaxLengthEffective(); got != 100 {
		t.Errorf("长度注入 100 后应读到 100, got %d", got)
	}
	if got := TGInlineRowsMaxEffective(); got != 3 {
		t.Errorf("行数注入 3 后应读到 3, got %d", got)
	}
	if got := TGInlineButtonsPerRowMaxEffective(); got != 2 {
		t.Errorf("每行钮数注入 2 后应读到 2, got %d", got)
	}

	SetTGMessageMaxLenProvider(func() int { return 0 })
	SetTGInlineRowsMaxProvider(func() int { return -1 })
	SetTGInlineButtonsPerRowMaxProvider(func() int { return 0 })
	if got := TGMessageMaxLengthEffective(); got != TGMessageMaxLength {
		t.Errorf("长度注入 0 应回落兜底, got %d", got)
	}
	if got := TGInlineRowsMaxEffective(); got != TGInlineRowsMax {
		t.Errorf("行数注入负值应回落兜底, got %d", got)
	}
	if got := TGInlineButtonsPerRowMaxEffective(); got != TGInlineButtonsPerRowMax {
		t.Errorf("每行钮数注入 0 应回落兜底, got %d", got)
	}

	// setter 传 nil 是空操作，不是复位 —— 先把三个值都改成非兜底值再传 nil。
	SetTGMessageMaxLenProvider(func() int { return 111 })
	SetTGInlineRowsMaxProvider(func() int { return 7 })
	SetTGInlineButtonsPerRowMaxProvider(func() int { return 5 })
	SetTGMessageMaxLenProvider(nil)
	SetTGInlineRowsMaxProvider(nil)
	SetTGInlineButtonsPerRowMaxProvider(nil)
	if got := TGMessageMaxLengthEffective(); got != 111 {
		t.Errorf("setter 传 nil 后应保持 111, got %d", got)
	}
	if got := TGInlineRowsMaxEffective(); got != 7 {
		t.Errorf("setter 传 nil 后应保持 7, got %d", got)
	}
	if got := TGInlineButtonsPerRowMaxEffective(); got != 5 {
		t.Errorf("setter 传 nil 后应保持 5, got %d", got)
	}
}

// TestSplitMessageRespectsSeam 证明发送侧的读取点真走 seam。
func TestSplitMessageRespectsSeam(t *testing.T) {
	t.Cleanup(func() { tgMessageMaxLenProvider = nil })

	text := "0123456789"
	SetTGMessageMaxLenProvider(func() int { return 4 })
	got := splitMessage(text, 0) // 传 0 走 seam 兜底链
	if len(got) != 3 {
		t.Errorf("上限 4 时应分成 3 段（4/4/2）, got %d 段: %v", len(got), got)
	}

	SetTGMessageMaxLenProvider(func() int { return 100 })
	if got := splitMessage(text, 0); len(got) != 1 {
		t.Errorf("上限 100 时不应分段, got %d 段: %v", len(got), got)
	}
}

// TestBuildInlineKeyboardRespectsSeams 证明 keyboard 的两级截断真按配置值走。
func TestBuildInlineKeyboardRespectsSeams(t *testing.T) {
	t.Cleanup(func() {
		tgInlineRowsMaxProvider = nil
		tgInlineButtonsPerRowMaxProvider = nil
	})

	rows := [][]InlineButton{
		{{Text: "a1", CallbackData: "a1"}, {Text: "a2", CallbackData: "a2"}, {Text: "a3", CallbackData: "a3"}},
		{{Text: "b1", CallbackData: "b1"}},
		{{Text: "c1", CallbackData: "c1"}},
		{{Text: "d1", CallbackData: "d1"}},
	}
	countButtons := func(kb map[string]any) (int, int) {
		raw, ok := kb["inline_keyboard"].([][]map[string]string)
		if !ok {
			t.Fatalf("inline_keyboard 形态不对: %T", kb["inline_keyboard"])
		}
		btns := 0
		for _, r := range raw {
			btns += len(r)
		}
		return len(raw), btns
	}

	// 默认 100 行 8 钮：4 行 3/1/1/1 个钮，全留。
	nRows, nBtns := countButtons(buildInlineKeyboard(rows))
	if nRows != 4 || nBtns != 6 {
		t.Errorf("兜底下应保留 4 行 6 钮, got %d 行 %d 钮", nRows, nBtns)
	}

	SetTGInlineRowsMaxProvider(func() int { return 2 })
	nRows, nBtns = countButtons(buildInlineKeyboard(rows))
	if nRows != 2 || nBtns != 4 {
		t.Errorf("行数上限 2 时应保留 2 行 4 钮, got %d 行 %d 钮", nRows, nBtns)
	}

	SetTGInlineButtonsPerRowMaxProvider(func() int { return 1 })
	nRows, nBtns = countButtons(buildInlineKeyboard(rows))
	if nRows != 2 || nBtns != 2 {
		t.Errorf("每行 1 钮 + 行数 2 时应保留 2 行 2 钮, got %d 行 %d 钮", nRows, nBtns)
	}

	// 空键盘与全空按钮仍应返回 nil（调用侧据此省略 reply_markup）。
	if kb := buildInlineKeyboard(nil); kb != nil {
		t.Errorf("空 rows 应返回 nil, got %v", kb)
	}
	empty := [][]InlineButton{{InlineButton{Text: "", CallbackData: ""}}}
	if kb := buildInlineKeyboard(empty); kb != nil {
		t.Errorf("全空按钮应返回 nil, got %v", kb)
	}
}

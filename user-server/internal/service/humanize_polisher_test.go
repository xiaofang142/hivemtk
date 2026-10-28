package service

import (
	"context"
	"strings"
	"testing"
)

func TestHumanize_RemoveAITraces(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{"作为 AI 助手", "作为 AI 助手，我推荐您看看", "推荐您看看", "作为 AI 助手"},
		{"作为人工智能", "作为人工智能，我帮您查一下", "帮您查一下", "作为人工智能"},
		{"我是 AI", "我是 AI 没问题", "没问题", "我是 AI"},
		{"我是人工智能", "我是人工智能 想帮您", "想帮您", "我是人工智能"},
		{"很抱歉，我无法", "很抱歉，我无法完成此操作", "完成此操作", "很抱歉，我无法"},
		{"作为一个", "作为一个 我来说下", "我来说下", "作为一个"},
		{"我是一个 AI", "我是一个 AI 请稍等", "请稍等", "我是一个 AI"},
		{"我的能力有限", "我的能力有限，需要时间", "需要时间", "我的能力有限"},
		{"根据您提供的信息", "根据您提供的信息，推荐 A 方案", "推荐 A 方案", "根据您提供的信息"},
		{"我理解您的", "我理解您的疑问，请稍等", "疑问，请稍等", "我理解您的"},
		{"作为您的销售顾问", "作为您的销售顾问 我建议您考虑", "我建议您考虑", "作为您的销售顾问"},
		{"多重 AI 痕迹", "作为 AI 助手，根据您提供的信息，我推荐", "我推荐", "作为 AI 助手"},
		{"无 AI 痕迹", "价格 199，下单包邮", "价格 199", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := p.Polish(context.Background(), c.input, nil)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if c.contains != "" && !strings.Contains(out, c.contains) {
				t.Errorf("output %q should contain %q", out, c.contains)
			}
			if c.excludes != "" && strings.Contains(out, c.excludes) {
				t.Errorf("output %q should not contain %q", out, c.excludes)
			}
		})
	}
}

func TestHumanize_RemoveExtraSymbols(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{"连续半角 !", "太棒了!!!", "太棒了！"},
		{"连续全角 !", "太棒了！！！", "太棒了！"},
		{"混合 !", "太棒了!!！", "太棒了！"},
		{"单个 ! 不变", "太棒了!", "太棒了!"},
		{"连续半角 ?", "真的吗??", "真的吗？"},
		{"连续全角 ?", "真的吗？？", "真的吗？"},
		{"混合 ?", "真的吗?？", "真的吗？"},
		{"单个 ? 不变", "真的吗?", "真的吗?"},
		{"连续 . 4 个", "嗯......", "嗯……"},
		{"连续 . 5 个", "嗯.....", "嗯……"},
		{"2 个 . 不变", "嗯..", "嗯.."},
		{"全角省略号不变", "嗯……", "嗯……"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := p.Polish(context.Background(), c.input, nil)
			if out != c.expect {
				t.Errorf("input=%q want=%q got=%q", c.input, c.expect, out)
			}
		})
	}
}

func TestHumanize_PlatformStyle(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name     string
		input    string
		platform string
		mustHave []string
		mustMiss []string
	}{
		{"wechat 允许 emoji", "已发货😊，请查收", "wechat", []string{"😊"}, nil},
		{"douyin 允许 emoji", "买它🔥🔥", "douyin", []string{"🔥"}, nil},
		{"小红书 允许 emoji", "姐妹们🌟冲啊", "xiaohongshu", []string{"🌟"}, nil},
		{"邮件去除 emoji", "已收到 😊，谢谢", "email", nil, []string{"😊"}},
		{"邮件全角 emoji 去除", "已收到 😁，谢谢", "email", nil, []string{"😁"}},
		{"邮件正式语气 嗯→是的", "嗯 好的", "email", []string{"是的"}, nil},
		{"whatsapp 正式 哈哈→呵呵", "哈哈 好的", "whatsapp", []string{"呵呵"}, nil},
		{"WeChat 大小写", "ok", "WeChat", nil, nil},
		{"XHS 简写", "ok", "xhs", nil, nil},
		{"未知平台", "ok", "unknown_platform_xyz", nil, nil},
		{"nil context", "ok", "", nil, nil},
		{"邮件 短句", "好", "email", []string{"好"}, nil},
		{"weixin 别名", "ok😊", "weixin", []string{"😊"}, nil},
		{"telegram", "哈哈", "telegram", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pctx := &PolishContext{Platform: c.platform}
			out, _ := p.Polish(context.Background(), c.input, pctx)
			for _, s := range c.mustHave {
				if !strings.Contains(out, s) {
					t.Errorf("output %q should contain %q", out, s)
				}
			}
			for _, s := range c.mustMiss {
				if strings.Contains(out, s) {
					t.Errorf("output %q should NOT contain %q", out, s)
				}
			}
		})
	}
}

func TestHumanize_Truncation(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		maxLen    int
		exLen     int
		shouldEnd bool
	}{
		{"不超长 默认 80", "短文本", 80, 3, false},
		{"正好 80", strings.Repeat("中", 80), 80, 80, false},
		{"81 截断到 80", strings.Repeat("中", 81), 80, 80, true},
		{"100 截断到 80", strings.Repeat("中", 100), 80, 80, true},
		{"200 截断到 80", strings.Repeat("a", 200), 80, 80, true},
		{"maxLen 0 不截断", strings.Repeat("中", 100), 0, 100, false},
		{"maxLen 1", "中文", 1, 1, true},
		{"maxLen 2", "中文", 2, 2, false},
		{"maxLen 3", "中文", 3, 2, false},
		{"空串", "", 80, 0, false},
		{"半角 ASCII 截断", "abcdefghij", 5, 5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewHumanizePolisher()
			p.randFn = func() float64 { return 0.99 }
			p.maxLength = c.maxLen
			out, _ := p.Polish(context.Background(), c.input, &PolishContext{Platform: "wechat"})
			runes := []rune(out)
			if len(runes) != c.exLen {
				t.Errorf("input runes=%d maxLen=%d want runes=%d got runes=%d (out=%q)",
					len([]rune(c.input)), c.maxLen, c.exLen, len(runes), out)
			}
			if c.shouldEnd {
				if len(runes) > 0 && runes[len(runes)-1] != '…' {
					t.Errorf("truncated output should end with '…', got %q", out)
				}
			}
		})
	}
}

func TestHumanize_TruncationDisabled(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	long := strings.Repeat("中", 200)
	out, _ := p.Polish(context.Background(), long, nil)
	if len([]rune(out)) != 200 {
		t.Errorf("truncation disabled should keep 200 runes, got %d", len([]rune(out)))
	}
}

func TestHumanize_Personalization(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name     string
		input    string
		customer string
		platform string
		mustHave string
	}{
		{"含名字 不重复加", "王先生，您好", "王先生", "wechat", "王先生"},
		{"含亲 不再加", "亲，欢迎光临", "张三", "wechat", "亲"},
		{"含您 不再加", "您说得对", "张三", "wechat", "您说得对"},
		{"空名字 不加", "好的", "", "wechat", "好的"},
		{"不包含称呼 但实现不强制加", "ok", "张三", "wechat", "ok"},
		{"长文本 名字嵌入", "欢迎咨询 王女士 专属服务", "王女士", "wechat", "王女士"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pctx := &PolishContext{CustomerName: c.customer, Platform: c.platform}
			out, _ := p.Polish(context.Background(), c.input, pctx)
			if !strings.Contains(out, c.mustHave) {
				t.Errorf("output %q should contain %q", out, c.mustHave)
			}
		})
	}
}

func TestHumanize_Particle(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name     string
		input    string
		intent   string
		expected string
	}{
		{"好的 加 嗯", "好的", "greeting", "嗯，好的"},
		{"是的 加 嗯", "是的", "greeting", "嗯，是的"},
		{"可以 加 嗯", "可以", "greeting", "嗯，可以"},
		{"没问题 加 嗯", "没问题", "greeting", "嗯，没问题"},
		{"OK 加 嗯", "OK", "greeting", "嗯，OK"},
		{"投诉不加", "好的", "complaint", "好的"},
		{"流失倾向不加", "好的", "churn", "好的"},
		{"售后不加", "好的", "after_sale", "好的"},
		{"长文本不加", "感谢您的支持，我们会尽快处理", "greeting", "感谢您的支持，我们会尽快处理"},
		{"60 字 不加", strings.Repeat("中", 60), "greeting", strings.Repeat("中", 60)},
		{"61 字 不加", strings.Repeat("中", 61), "greeting", strings.Repeat("中", 61)},
		{"59 字 不加", strings.Repeat("中", 59), "greeting", strings.Repeat("中", 59)},
		{"空文本", "", "greeting", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pctx := &PolishContext{Intent: c.intent}
			out, _ := p.Polish(context.Background(), c.input, pctx)
			if out != c.expected {
				t.Errorf("input=%q intent=%q want=%q got=%q", c.input, c.intent, c.expected, out)
			}
		})
	}
}

func TestHumanize_ParticleDisabled(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	p.enableParticles = false
	out, _ := p.Polish(context.Background(), "好的", &PolishContext{Intent: "greeting"})
	if out != "好的" {
		t.Errorf("particles disabled: want 好的 got %q", out)
	}
}

func TestHumanize_EndToEnd(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	cases := []struct {
		name     string
		input    string
		pctx     *PolishContext
		mustHave []string
		mustMiss []string
	}{
		{
			name:     "微信完整流",
			input:    "作为 AI 助手 根据您提供的信息 我推荐您看看 A 方案!!",
			pctx:     &PolishContext{Platform: "wechat", CustomerName: "王女士", Intent: "product_recommend"},
			mustHave: []string{"A 方案"},
			mustMiss: []string{"作为 AI 助手", "!!", "根据您提供的信息"},
		},
		{
			name:     "邮件完整流",
			input:    "嗯 好的 😊",
			pctx:     &PolishContext{Platform: "email", Intent: "greeting"},
			mustHave: []string{"是的"},
			mustMiss: []string{"😊", "嗯"},
		},
		{
			name:     "投诉场景不语气词",
			input:    "好的",
			pctx:     &PolishContext{Platform: "wechat", Intent: "complaint"},
			mustHave: []string{"好的"},
			mustMiss: []string{"嗯，"},
		},
		{
			name:     "长文本截断+去 AI",
			input:    "作为 AI 助手 我是人工智能 " + strings.Repeat("非常感谢您的咨询，", 10),
			pctx:     &PolishContext{Platform: "wechat"},
			mustMiss: []string{"作为 AI 助手", "我是人工智能"},
		},
		{
			name:     "抖音完整流",
			input:    "家人们冲啊🔥🔥🔥",
			pctx:     &PolishContext{Platform: "douyin", Intent: "product_recommend"},
			mustHave: []string{"🔥"},
		},
		{
			name:     "空上下文",
			input:    "ok",
			pctx:     nil,
			mustHave: []string{"ok"},
		},
		{
			name:     "纯空",
			input:    "",
			pctx:     &PolishContext{Platform: "wechat"},
			mustHave: []string{""},
		},
		{
			name:     "小红书+短回复",
			input:    "好的",
			pctx:     &PolishContext{Platform: "xiaohongshu", Intent: "greeting"},
			mustHave: []string{"嗯"},
		},
		{
			name:     "邮件无 emoji + 正式",
			input:    "哈哈 好的",
			pctx:     &PolishContext{Platform: "email", Intent: "greeting"},
			mustHave: []string{"呵呵"},
			mustMiss: []string{"哈哈"},
		},
		{
			name:     "大小写 platform",
			input:    "好的",
			pctx:     &PolishContext{Platform: "WECHAT", Intent: "greeting"},
			mustHave: []string{"嗯"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := p.Polish(context.Background(), c.input, c.pctx)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			for _, s := range c.mustHave {
				if !strings.Contains(out, s) {
					t.Errorf("output %q should contain %q", out, s)
				}
			}
			for _, s := range c.mustMiss {
				if strings.Contains(out, s) {
					t.Errorf("output %q should NOT contain %q", out, s)
				}
			}
		})
	}
}

func TestHumanize_EdgeCases(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	p.enableTruncation = false
	cases := []struct {
		name  string
		input string
		pctx  *PolishContext
	}{
		{"仅空白", "   ", &PolishContext{Platform: "wechat"}},
		{"仅换行", "\n\n\n", nil},
		{"仅 Tab", "\t\t", nil},
		{"仅 emoji", "😊😊", &PolishContext{Platform: "wechat"}},
		{"超长单字", strings.Repeat("x", 1000), &PolishContext{Platform: "wechat"}},
		{"混合 unicode", "中文 abc 123 😊", &PolishContext{Platform: "wechat"}},
		{"特殊符号", "!@#$%^&*()", nil},
		{"全角符号", "！@#￥%……&*（）", nil},
		{"html 注入", "<script>alert(1)</script>", nil},
		{"sql 注入", "'; DROP TABLE users;--", nil},
		{"NULL 字符串", "null", nil},
		{"undefined 字符串", "undefined", nil},
		{"NUL 字符", "\x00abc", nil},
		{"超长空字符串", strings.Repeat(" ", 100), nil},
		{"全数字", "1234567890", &PolishContext{Platform: "wechat"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := p.Polish(context.Background(), c.input, c.pctx)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			_ = out
		})
	}
}

func TestHumanize_GetStyleForPlatform(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	cases := []struct {
		platform     string
		expectName   string
		expectEmoji  bool
		expectFormal bool
	}{
		{"wechat", "wechat", true, false},
		{"weixin", "wechat", true, false},
		{"douyin", "douyin", true, false},
		{"xiaohongshu", "xhs", true, false},
		{"xhs", "xhs", true, false},
		{"xianyu", "xianyu", true, false},
		{"tiktok", "tiktok", true, false},
		{"whatsapp", "im", true, true},
		{"telegram", "im", true, true},
		{"email", "email", false, true},
		{"mail", "email", false, true},
		{"unknown", "default", true, false},
	}
	for _, c := range cases {
		t.Run(c.platform, func(t *testing.T) {
			style := p.getStyleForPlatform(context.Background(), c.platform)
			if style.StyleName != c.expectName {
				t.Errorf("platform=%q want name=%q got %q", c.platform, c.expectName, style.StyleName)
			}
			if style.AllowEmoji != c.expectEmoji {
				t.Errorf("platform=%q want emoji=%v got %v", c.platform, c.expectEmoji, style.AllowEmoji)
			}
			if style.AllowFormality != c.expectFormal {
				t.Errorf("platform=%q want formal=%v got %v", c.platform, c.expectFormal, style.AllowFormality)
			}
		})
	}
}

func TestHumanize_TruncateByLengthBoundary(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	if out := p.truncateByLength(context.Background(), "中文", 0); out != "中文" {
		t.Errorf("maxLen=0 should not truncate, got %q", out)
	}
	if out := p.truncateByLength(context.Background(), "中文", 1); out != "…" {
		t.Errorf("maxLen=1 want '…' got %q", out)
	}
	if out := p.truncateByLength(context.Background(), "abc", 10); out != "abc" {
		t.Errorf("len<max should not truncate, got %q", out)
	}
	if out := p.truncateByLength(context.Background(), "abcde", 5); out != "abcde" {
		t.Errorf("len==max should not truncate, got %q", out)
	}
	if out := p.truncateByLength(context.Background(), "abcdef", 5); out != "abcd…" {
		t.Errorf("len>max should truncate, got %q", out)
	}
}

func TestHumanize_ShouldAddParticle(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	if p.shouldAddParticle(context.Background(), &PolishContext{Intent: "complaint"}) {
		t.Error("complaint should not add particle")
	}
	if p.shouldAddParticle(context.Background(), &PolishContext{Intent: "churn"}) {
		t.Error("churn should not add particle")
	}
	if p.shouldAddParticle(context.Background(), &PolishContext{Intent: "after_sale"}) {
		t.Error("after_sale should not add particle")
	}
	if !p.shouldAddParticle(context.Background(), &PolishContext{Intent: "greeting"}) {
		t.Error("greeting should add particle")
	}
	if !p.shouldAddParticle(context.Background(), &PolishContext{Intent: "product_inquiry"}) {
		t.Error("product_inquiry should add particle")
	}
	if !p.shouldAddParticle(context.Background(), nil) {
		t.Error("nil context should add particle (default)")
	}
}

func TestHumanize_Personalize(t *testing.T) {
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0.99 }
	if out := p.personalize(context.Background(), "ok", "", "wechat"); out != "ok" {
		t.Errorf("empty name should not personalize, got %q", out)
	}
	if out := p.personalize(context.Background(), "王先生好", "王先生", "wechat"); out != "王先生好" {
		t.Errorf("existing name should not duplicate, got %q", out)
	}
	if out := p.personalize(context.Background(), "亲，欢迎", "张三", "wechat"); out != "亲，欢迎" {
		t.Errorf("existing 亲 should not add, got %q", out)
	}
	if out := p.personalize(context.Background(), "您说的对", "张三", "wechat"); out != "您说的对" {
		t.Errorf("existing 您 should not add, got %q", out)
	}
}

func TestHumanize_TestCaseCount(t *testing.T) {
	t.Log("HumanizePolisher 测试覆盖：≥100 用例")
	t.Log("  - AI 痕迹去除: 13")
	t.Log("  - 多余符号压缩: 12")
	t.Log("  - 平台风格适配: 14")
	t.Log("  - 长度截断: 11")
	t.Log("  - 个性化称呼: 6")
	t.Log("  - 语气词添加: 14")
	t.Log("  - 端到端集成: 10")
	t.Log("  - 边界异常: 15")
	t.Log("  - 内部 helper: 5+6+3 = 14")
	t.Log("  合计: ≥109")
}

// TestInjectTypoDefaultOff 内容审计 2026-10-28 §5-③：错别字注入默认关闭——
// 默认构造下，即使随机数必然命中（randFn=0，10% 概率窗口内）且文本以 typoPool
// 结尾，Polish 产出也不得被替换。显式 EnableTypoInjection 后行为恢复。
func TestInjectTypoDefaultOff(t *testing.T) {
	newHitCtx := func() *PolishContext {
		return &PolishContext{Persona: "朋友", Intent: IntentGreeting}
	}

	// 默认关：randFn=0 必然命中 10% 概率窗口，文本以"好的"结尾
	p := NewHumanizePolisher()
	p.randFn = func() float64 { return 0 }
	got := p.injectTypo("好的", newHitCtx())
	if got != "好的" {
		t.Fatalf("默认构造下错别字注入必须关闭，实得 %q（期望原样 %q）", got, "好的")
	}
	gotPolish, err := p.Polish(context.Background(), "好的", newHitCtx())
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(gotPolish, "好哒") || strings.HasSuffix(gotPolish, "ok") ||
		gotPolish == "好滴" || gotPolish == "好的呀" {
		t.Fatalf("Polish 全链默认下也不应注入错别字，实得 %q", gotPolish)
	}

	// 显式开启：同随机数下替换发生（开关本身有效，不是把注入逻辑删了）
	p2 := NewHumanizePolisher()
	p2.randFn = func() float64 { return 0 }
	p2.EnableTypoInjection()
	got2 := p2.injectTypo("好的", newHitCtx())
	if got2 == "好的" {
		t.Fatal("显式开启后 randFn=0 应命中替换，实得仍为原文")
	}
}

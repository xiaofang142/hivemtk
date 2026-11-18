package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"hivemtk-user/internal/model"
)

func TestRender_PlainStripsMarkdown(t *testing.T) {
	in := "**重点** 与 [官网](https://example.com/detail)\n# 标题"
	out, err := RenderForChannel("douyin", model.MsgTypeText, in, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out.Text, "**") || strings.Contains(out.Text, "# ") {
		t.Fatalf("plain 渠道必须剥掉 Markdown 标记，实际 %q", out.Text)
	}
	if !strings.Contains(out.Text, "重点") || !strings.Contains(out.Text, "https://example.com/detail") {
		t.Fatalf("剥标记不能丢内容，实际 %q", out.Text)
	}
	if out.Overflow != 0 {
		t.Fatalf("未超上限不应报溢出，实际 %d", out.Overflow)
	}
}

func TestRender_PlainLeavesPlainTextIntact(t *testing.T) {
	in := "您好，我们支持私有化部署，价格 3*4 元起。"
	out, err := RenderForChannel("xianyu", model.MsgTypeText, in, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Text != in {
		t.Fatalf("纯文本不应被改写：\nwant %q\ngot  %q", in, out.Text)
	}
}

func TestRender_CardFoldedWhenUnsupported(t *testing.T) {
	card := model.RichCard{
		Title:       "订单确认",
		Description: "预计明天发货",
		Fields:      map[string]string{"订单号": "A-1"},
	}
	out, err := RenderForChannel("whatsapp", model.MsgTypeText, "请确认订单。", []model.RichCard{card})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Cards) != 0 {
		t.Fatal("无卡片载体的渠道不得保留卡片")
	}
	for _, want := range []string{"订单确认", "预计明天发货", "订单号：A-1", "请确认订单。"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("折叠文本缺 %q：%q", want, out.Text)
		}
	}
}

func TestRender_CardKeptWhenSupported(t *testing.T) {
	card := model.RichCard{Title: "t"}
	out, err := RenderForChannel("telegram", model.MsgTypeText, "正文", []model.RichCard{card})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out.Cards) != 1 {
		t.Fatalf("telegram 有卡片载体，卡片应保留，实际 %d", len(out.Cards))
	}
	if out.Text != "正文" {
		t.Fatalf("HTML 渠道不应剥 Markdown/改写正文，实际 %q", out.Text)
	}
}

func TestRender_OverflowCountsWithoutSplitting(t *testing.T) {
	in := strings.Repeat("字", 600)
	out, err := RenderForChannel("sms", model.MsgTypeText, in, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Overflow != 100 {
		t.Fatalf("600 字超出 500 上限应报 100，实际 %d", out.Overflow)
	}
	if utf8.RuneCountInString(out.Text) != 600 {
		t.Fatalf("溢出只计数不切分：正文应原样 600 字，实际 %d", utf8.RuneCountInString(out.Text))
	}
}

func TestRender_UnknownChannelErrors(t *testing.T) {
	if _, err := RenderForChannel("pigeon_post", model.MsgTypeText, "hi", nil); err == nil {
		t.Fatal("未登记渠道必须报错，不能静默透传")
	}
}

// TestRender_BridgeFamilyAllMarkdownStripped 九个桥接渠道全部按纯文本渲染：
// 少登记一个，该渠道客户就会看到 **粗体** 原样漏出。
func TestRender_BridgeFamilyAllMarkdownStripped(t *testing.T) {
	for _, ch := range []string{"douyin", "xiaohongshu", "kuaishou", "tiktok", "xianyu", "jd", "bilibili", "weibo", "personal_wx", "pdd"} {
		out, err := RenderForChannel(ch, model.MsgTypeText, "**限时**", nil)
		if err != nil {
			t.Fatalf("%s render: %v", ch, err)
		}
		if strings.Contains(out.Text, "**") {
			t.Fatalf("%s 未剥 Markdown：%q", ch, out.Text)
		}
	}
}

// TestRender_NormalizesCommonMarkHardBreaks 行尾硬换行反斜杠不能漏进纯文本输入框。
func TestRender_NormalizesCommonMarkHardBreaks(t *testing.T) {
	in := "第一行\\\n第二行"
	out, err := RenderForChannel("douyin", model.MsgTypeText, in, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out.Text, "\\") {
		t.Fatalf("硬换行反斜杠必须清掉，实际 %q", out.Text)
	}
	if out.Text != "第一行\n第二行" {
		t.Fatalf("换行结构应保留：%q", out.Text)
	}
	// 成对反斜杠是字面内容（Windows 路径），不能当成标记删半个。
	if out2, _ := RenderForChannel("web", model.MsgTypeText, `C:\\tmp`, nil); out2.Text != `C:\\tmp` {
		t.Fatalf("成对反斜杠应原样保留，实际 %q", out2.Text)
	}
}

// TestRender_RestoresEscapedThematicBreak 模型为防止误渲染写的 \--- 在消息载体上应还原为 ---。
func TestRender_RestoresEscapedThematicBreak(t *testing.T) {
	out, err := RenderForChannel("web", model.MsgTypeText, "上文\n\\---\n下文", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Text != "上文\n---\n下文" {
		t.Fatalf("转义分隔线应还原，实际 %q", out.Text)
	}
}

// TestRender_KeepsCodeFenceVerbatim 代码围栏内的反斜杠是内容，逐字保留；围栏外照常清理。
func TestRender_KeepsCodeFenceVerbatim(t *testing.T) {
	in := "```bash\necho a \\\n\\---\n```\n结束\\"
	out, err := RenderForChannel("web", model.MsgTypeText, in, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out.Text, "echo a \\\n\\---\n") {
		t.Fatalf("代码围栏内容被改写：%q", out.Text)
	}
	if !strings.HasSuffix(out.Text, "结束") {
		t.Fatalf("围栏外硬换行反斜杠应清掉，实际 %q", out.Text)
	}
}

// TestRender_TrimsTrailingBlankLines 尾随空行不进入气泡。
func TestRender_TrimsTrailingBlankLines(t *testing.T) {
	out, err := RenderForChannel("web", model.MsgTypeText, "正文\n\n\n", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Text != "正文" {
		t.Fatalf("尾随空行应清掉，实际 %q", out.Text)
	}
}

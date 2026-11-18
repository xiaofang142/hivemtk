package service

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"hivemtk-user/internal/channelcaps"
	"hivemtk-user/internal/model"
)

// outbound_render.go 出站渲染层（specs/024 T401–T404）。
//
// 一句话：**出站内容在 sendOutbound 里只被转换一次**，之后所有 sender 都拿「已经是目标格式」的
// 正文，不再各自转义/清洗——否则同一个渠道多一条发送路径，就多一处格式漂移。
//
// 三件事在这里收口：
//  1. 格式转换：plain 渠道剥 Markdown 语法（网页私信输入框是纯文本，语法符号会原样暴露）；
//  2. 富卡决策：没有卡片载体的渠道把卡片折叠成完整文字（不能让客户看到「已展示卡片」的谎话）；
//  3. 溢出计数：超过平台单条上限只**计数上报**，不切分（切分在渠道自己手里，切两次会把话劈成两半）。
//
// 事实来源一律是 channelcaps（渠道能力清单），本文件不维护渠道白名单。

// RenderedOutbound 渲染结果。
type RenderedOutbound struct {
	// Text 已是目标格式的正文（sender 不得再次转义/清洗）。
	Text string
	// Cards 仍可发送的卡片；没有卡片载体的渠道为 nil（内容已折进 Text）。
	Cards []model.RichCard
	// Overflow 超出平台单条上限的字数（0 = 未超）。只用于上报，不参与切分。
	Overflow int
}

// RenderForChannel 按渠道能力把「AI 原始内容 + 卡片」渲染成可直接发送的载荷。
//
// 未知渠道返回错误：静默透传会让新渠道在没有任何能力声明的情况下上线，
// 出问题时没人知道它到底支持什么（宁可发失败也不发错格式）。
func RenderForChannel(channel, msgType, content string, cards []model.RichCard) (RenderedOutbound, error) {
	format := channelcaps.OutboundFormatOf(channel)
	if format == "" {
		// 文案与 sender 缺失路径同口径：调用方/告警按「出站不支持该渠道」聚合，
		// 换个说法会让同一种故障在两个指标里各算一次。
		return RenderedOutbound{}, fmt.Errorf("出站不支持该渠道(%s)：channelcaps 未登记出站格式", channel)
	}
	out := RenderedOutbound{Text: content, Cards: cards}

	// 1. 富卡决策：没有真载体就折叠成文字，且失败必须显式报错——
	//    「卡片发不出去但正文已经声称展示」比发送失败更糟。
	if len(cards) > 0 && !channelcaps.SupportsRichCards(channel) {
		text, err := foldCardsToText(content, cards)
		if err != nil {
			return out, err
		}
		out.Text = text
		out.Cards = nil
	}

	// 2. 格式转换：只对正文类消息做（卡片/图片等形态的 content 是说明文字，不剥语法）。
	if msgType == "" || msgType == model.MsgTypeText {
		// CommonMark 记账记号（硬换行反斜杠、转义分隔线）在任何载体上都是噪音，
		// 先统一清掉，再按目标格式剥语法——顺序反了会把刚还原的 --- 又当成列表标记处理。
		out.Text = normalizeOutboundText(out.Text)
		if format == channelcaps.OutboundFormatPlain {
			out.Text = StripMarkdownForDM(out.Text)
		}
	}

	// 3. 溢出计数（不切分）。
	if max := channelcaps.MaxTextLenOf(channel); max > 0 {
		if n := utf8.RuneCountInString(out.Text); n > max {
			out.Overflow = n - max
		}
	}
	return out, nil
}

// foldCardsToText 把卡片压成一段完整可读文字（原 cardTextDelivery 的实现，specs/024 T403 平迁）。
//
// 与发送解耦：这是纯函数，同样的输入永远得到同样的输出，所以延迟重放不会二次追加事实。
func foldCardsToText(content string, cards []model.RichCard) (string, error) {
	blocks := make([]string, 0, len(cards))
	for _, card := range cards {
		lines := make([]string, 0, 4+len(card.Fields)+len(card.Buttons))
		for _, fact := range []string{card.Title, card.Subtitle, card.Description} {
			if fact = strings.TrimSpace(fact); fact != "" {
				lines = append(lines, fact)
			}
		}
		keys := make([]string, 0, len(card.Fields))
		for key := range card.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := strings.TrimSpace(card.Fields[key])
			if value != "" {
				lines = append(lines, strings.TrimSpace(key)+"："+value)
			}
		}
		for _, button := range card.Buttons {
			label := strings.TrimSpace(button.Text)
			if label == "" {
				label = "详情"
			}
			if link := cardPublicHTTPSLink(button.URL); link != "" {
				lines = append(lines, label+"："+link)
			} else if button.URL != "" || button.Action != "" {
				lines = append(lines, label+"（当前渠道无法执行此操作，请联系人工确认）")
			} else if button.Text != "" {
				lines = append(lines, label)
			}
		}
		for _, image := range []string{card.ImageURL, card.ThumbURL} {
			if link := cardPublicHTTPSLink(image); link != "" {
				lines = append(lines, "图片链接："+link)
			}
		}
		if len(lines) > 0 {
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
	}
	if len(blocks) == 0 {
		return "", errors.New("卡片无法形成可读文字载荷")
	}
	// Neutralize only known presentation claims; preserve the rest of the answer's facts.
	text := strings.NewReplacer(
		"我已经为你展示了卡片", "以下是文字版信息",
		"已经为您展示了卡片", "以下是文字版信息",
		"请点击卡片查看详情", "请查看下方文字详情",
		"点击卡片查看详情", "查看下方文字详情",
		"请点击卡片", "请查看下方文字信息",
		"请查看卡片", "请查看下方文字信息",
	).Replace(strings.TrimSpace(content))
	for _, block := range blocks {
		if strings.Contains(text, block) {
			continue
		}
		if text != "" {
			text += "\n\n"
		}
		text += block
	}
	return text, nil
}

// mdEscapedThematicBreak 行首被反斜杠转义的主题分隔线（\--- / \*** / \___）。
var mdEscapedThematicBreak = regexp.MustCompile(`^([ \t]*)\\(-{3,}|\*{3,}|_{3,})([ \t]*)$`)

// normalizeOutboundText 清掉「CommonMark 硬换行 / 转义分隔线」在消息载体上的残留（specs/025 M-3）。
//
// 模型按 CommonMark 写作时会留下两类只在 Markdown 渲染器里有意义的记号：
//   - 行尾反斜杠（硬换行）：纯文本载体（网页私信输入框、短信…）会原样显示 "\"；
//   - 用作分隔线的 \--- / \*** / \___：反斜杠同样会被客户看见。
//
// 代码围栏内的内容逐字保留：那里可能是要复制的命令/代码，改一个字符就是错。
// 尾随空行也不留（DM 气泡会把它们渲染成多余空白）。
func normalizeOutboundText(content string) string {
	if content == "" || !strings.ContainsAny(content, "\\\n\r") {
		return content
	}
	s := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = unescapeThematicBreak(line)
		lines[i] = trimHardBreakBackslash(line)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// trimHardBreakBackslash 去掉行尾作为硬换行标记的反斜杠。
// 连续反斜杠为奇数才说明最后一个是标记（偶数时是成对的字面反斜杠）。
func trimHardBreakBackslash(line string) string {
	if !strings.HasSuffix(line, "\\") {
		return line
	}
	n := len(line) - len(strings.TrimRight(line, "\\"))
	if n%2 == 0 {
		return line
	}
	return line[:len(line)-1]
}

func unescapeThematicBreak(line string) string {
	if !strings.Contains(line, "\\") {
		return line
	}
	return mdEscapedThematicBreak.ReplaceAllString(line, "$1$2$3")
}

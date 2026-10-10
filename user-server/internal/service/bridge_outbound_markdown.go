package service

import (
	"regexp"
	"strings"
)

// =============================================================================
// 私聊出站 Markdown 清洗
//
// 背景：LLM 回复自带 Markdown 语法（**粗体**、`代码`、[链接](url)、# 标题…），
// 抖音/小红书等网页私信输入框是纯文本，语法符号原样暴露给客户。
// 在 DeliverBridgeOutbound 单一收口处清洗，覆盖 AI 回复 / SOP / 触达管线 /
// 主动私信 / CSAT / 离线重放全部出站路径。
//
// 清洗原则（防误清洗，见 review 约定）：
//   - 只剥「成对语法标记」，保留内容本体；
//   - 不动不成对的星号/下划线（3*4、snake_case、脚注星号）；
//   - 链接 [text](url) → text（url）：URL 是营销触达的落点，必须保留可读；
//   - 行首标记（# 标题、> 引用、- 列表）只剥标记符，文本与换行结构原样保留；
//   - 代码围栏 ``` 只删围栏行，围栏内代码本体保留；
//   - 表格不做处理（人设化短回复里几乎不出现，激进变换反而伤内容）。
// =============================================================================

var (
	mdLinkRe      = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\s]+)\)`)
	mdBoldRe      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdItalicRe    = regexp.MustCompile(`\*([^*\n]+)\*`)
	mdUnderscoreB = regexp.MustCompile(`__([^_\n]+)__`)
	mdStrikeRe    = regexp.MustCompile(`~~([^~\n]+)~~`)
	mdInlineCode  = regexp.MustCompile("`([^`\n]+)`")
	mdHeadingRe   = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	mdQuoteRe     = regexp.MustCompile(`(?m)^[ \t]{0,3}>[ \t]?`)
	mdListDashRe  = regexp.MustCompile(`(?m)^[ \t]{0,3}-[ \t]+`)
	mdListStarRe  = regexp.MustCompile(`(?m)^[ \t]{0,3}\*[ \t]+`)
	mdFenceRe     = regexp.MustCompile("(?m)^```[a-zA-Z0-9]*[ \\t]*\\r?\\n?|```")
)

// markdownDMChannels 需要 Markdown 清洗的网页私聊渠道集合。
// 这些渠道的输入框是纯文本，Markdown 语法符号会原样暴露。
var markdownDMChannels = map[string]bool{
	"douyin":      true,
	"xiaohongshu": true,
	"kuaishou":    true,
	"tiktok":      true,
	"xianyu":      true,
}

// StripMarkdownForDM 把 Markdown 语法标记剥成纯文本，供网页私信输入框直发。
// 仅对文本做保守变换；空串原样返回。
func StripMarkdownForDM(content string) string {
	gate := "*_`~#>[\n-"
	if content == "" || !strings.ContainsAny(content, gate) {
		return content
	}
	s := content
	// 代码围栏先剥（围栏行整体删除，内部代码不再被行内规则二次命中）
	s = mdFenceRe.ReplaceAllString(s, "")
	// 链接：[text](url) → text（url）；text 为空或与 url 相同 → url
	s = mdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		g := mdLinkRe.FindStringSubmatch(m)
		text, url := g[1], g[2]
		switch {
		case strings.TrimSpace(text) == "" || text == url:
			return url
		default:
			return text + "（" + url + "）"
		}
	})
	s = mdBoldRe.ReplaceAllString(s, "$1")
	s = mdUnderscoreB.ReplaceAllString(s, "$1")
	s = mdStrikeRe.ReplaceAllString(s, "$1")
	s = mdInlineCode.ReplaceAllString(s, "$1")
	s = mdItalicRe.ReplaceAllString(s, "$1")
	// 单下划线斜体不剥：Go RE2 无环视断言，且误伤 snake_case 的风险大于收益
	// 行首标记：标题/引用/无序列表（有序列表 "1. " 是纯文本习惯，保留）
	s = mdHeadingRe.ReplaceAllString(s, "")
	s = mdQuoteRe.ReplaceAllString(s, "")
	s = mdListDashRe.ReplaceAllString(s, "· ")
	s = mdListStarRe.ReplaceAllString(s, "· ")
	// DM 文本不需要尾随空行
	return strings.TrimRight(s, "\n")
}

// stripMarkdownForBridgeChannel 判定该渠道出站是否需要清洗（仅文本消息）。
func stripMarkdownForBridgeChannel(channel, msgType string) bool {
	if msgType != "" && msgType != "text" {
		return false
	}
	return markdownDMChannels[channel]
}

// Package xiaohongshu — 小红书平台适配器四件套。
// DOM 知识来源（调研已核实的公开实现）：
//   - xpzouying/xiaohongshu-mcp comment_feed.go：输入框 div.input-box p.content-input /
//     div.content-edit p.content-textarea；发送 button.submit；验证 .comments-container
//   - 本机真机实测（R17）：输入框 p.content-textarea；发送 button（文本「发送」）；
//     详情页 URL /search_result/{noteId}?xsec_token=&xsec_source= （token 必须从列表透传）
package xiaohongshu

import (
	"strings"

	"hivemtk-user/internal/browser_automation/platform"
)

// init 注册进基座注册表（新平台接入样板：只新增目录+init，基座零改动）
func init() {
	platform.Register(&Platform{})
}

// Platform 小红书适配器
type Platform struct{}

func (p *Platform) Identifier() string { return "xiaohongshu" }

func (p *Platform) Capabilities() []platform.Capability {
	return []platform.Capability{
		platform.CapSearch,
		platform.CapOpenDetail,
		platform.CapPostComment,
		platform.CapReadComment,
	}
}

func (p *Platform) MaxConcurrentJobs() int { return 1 } // 写操作串行（风控要求）

// Locators 平台元素定位表（DOM 知识唯一存放处，扩展原语不再硬编码；
// A1 收口：CommentLocators 从这里派生，同串 CSS 只写一遍；拦截文案移出 → BlockMarkers）
func (p *Platform) Locators() map[string]string {
	return map[string]string{
		"note_link":        "a[href*='/search_result/'], a[href*='/explore/']",
		"comment_input":    ".content-textarea, p.content-input, div.content-edit p",
		"send_button":      "button.submit, div.bottom button",
		"comment_list":     ".comments-container, .comments-el, [class*=comment-list]",
		"comment_item":     ".parent-comment .note-text, .comment-item .note-text",
		"note_title":       ".note-content .title, #detail-title",
		"note_author":      ".author-wrapper .name, .username",
		"send_button_text": "发送",
	}
}

// BlockMarkers 风控文案判据（A2：只匹配快照结构行，评论正文含「验证码」不再误报；
// 「验证码登录」类正常交互按钮也不列入——结构层只收拦截页专属文案）
func (p *Platform) BlockMarkers() []string {
	return []string{"IP存在风险", "当前环境异常"}
}

// BlockURLPatterns 拦截 URL 判据（实测形态：302→/website-login/error?error_code=300012）
func (p *Platform) BlockURLPatterns() []string {
	return []string{"website-login/error", "error_code=300012", "/captcha"}
}

// DetectBlock 拦截页识别（A2 结构化两层：URL 层 + 结构层；旧全文子串已废弃）
func (p *Platform) DetectBlock(pageURL, pageSnapshot string) bool {
	return platform.URLHits(pageURL, p.BlockURLPatterns()) ||
		platform.StructuralMarkerHits(pageSnapshot, p.BlockMarkers())
}

// ClassifyError 平台错误归因（Postiz handleErrors 语义）
func (p *Platform) ClassifyError(errText string) platform.ErrType {
	switch {
	case strings.Contains(errText, "登录"), strings.Contains(errText, "401"), strings.Contains(errText, "web_session"):
		return platform.ErrRefreshToken
	case strings.Contains(errText, "违规"), strings.Contains(errText, "敏感"), strings.Contains(errText, "invalid"):
		return platform.ErrBadBody
	case strings.Contains(errText, "验证码"), strings.Contains(errText, "风险"),
		strings.Contains(errText, "blocked"), strings.Contains(errText, "300012"), strings.Contains(errText, "461"):
		// 验证码/IP 风控：连续出现应 retire 账号——归 disconnect 由上层判定
		return platform.ErrDisconnect
	case strings.Contains(errText, "timeout"), strings.Contains(errText, "超时"),
		strings.Contains(errText, "未就绪"), strings.Contains(errText, "element_not_found"):
		return platform.ErrRetry
	default:
		// A3 fail-closed：判据未命中不再默认 retry（见 platform.ErrUnknown 注释）
		return platform.ErrUnknown
	}
}

// CommentLocators 发评论选择器四元组（基座把这套下发给扩展 post_comment 原语）。
// A1 单一来源：全部取自 Locators() 表，杜绝同串 CSS 写两遍的漂移（旧实现两处字面量重复）。
func (p *Platform) CommentLocators() platform.CommentLocators {
	l := p.Locators()
	return platform.CommentLocators{
		InputSelector:    l["comment_input"],
		SendButtonText:   l["send_button_text"],
		CommentContainer: l["comment_list"],
		CommentItemText:  l["comment_item"],
	}
}

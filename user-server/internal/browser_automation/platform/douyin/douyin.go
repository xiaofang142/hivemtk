// Package douyin — 抖音网页版适配器四件套。
// 调研依据（PLATFORM_BASE_DESIGN.md §1 能力矩阵）：
//   - 详情页 URL /video/{aweme_id}；全 CSR + jvm 混淆，读接口需 a_bogus+msToken 签名
//   - 发评论公开 API：POST /aweme/v1/web/comment/publish（M3 做）；DOM 选择器无公开资料
//   - 全线风控高：验证码中间页 rmc.bytedance.com（本机 2026-09-09 实测）
//
// M2 期先落「读链路」（DOM 路线：标题/作者/评论文本可见即可 extract）+ DetectBlock 判据。
package douyin

import (
	"strings"

	"hivemtk-user/internal/browser_automation/platform"
)

// init 注册进基座注册表（新平台接入样板：只新增目录+init，基座零改动）
func init() {
	platform.Register(&Platform{})
}

// Platform 抖音适配器
type Platform struct{}

func (p *Platform) Identifier() string { return "douyin" }

func (p *Platform) Capabilities() []platform.Capability {
	// 发评论（签名 API）列 M3；M2 只声明读能力
	return []platform.Capability{
		platform.CapSearch,
		platform.CapOpenDetail,
		platform.CapReadComment,
	}
}

func (p *Platform) MaxConcurrentJobs() int { return 1 } // 写操作串行（风控要求）

// Locators 平台元素定位表（DOM 知识唯一存放处；R20 真机校准 session117/119 实测；
// A1：拦截文案移出 → BlockMarkers）
func (p *Platform) Locators() map[string]string {
	return map[string]string{
		// R20 实测：搜索结果页 a[href*='/video/'] 卡片整体含标题+作者+时长+点赞（无独立 title 节点）
		"video_card":    "a[href*='/video/'], a[href*='/note/'], a[href*='modal_id']",
		"video_link":    "a[href*='/video/'], a[href*='/note/']",
		"video_title":   "a[href*='/video/']", // 卡片整体文本（标题混在卡片文本里）
		"video_author":  "[class*=author] a, [class*=author-name]",
		"comment_input": "[class*=comment-input] [contenteditable], [class*=pub] [contenteditable]",
		"comment_item":  "[class*=comment-item], [class*=CommentItem]",
		"comment_text":  "[class*=comment-item] span:not([class*=time]), p",
		"like_count":    "[class*=like-wrapper], [data-e2e=video-player-digg]",
		"search_input":  "input[placeholder*=搜索]",
	}
}

// BlockMarkers 风控文案判据（A2 结构层：只匹配交互/标题节点名。「验证码」不收——
// 登录按钮「手机验证码登录」是正常页面常驻文案，收即误报）
func (p *Platform) BlockMarkers() []string {
	return []string{"安全验证", "拖动上方滑块", "滑块验证"}
}

// BlockURLPatterns 拦截 URL 判据（本机实测 2026-09-09：无头/自动化特征 → 验证码中间页
// 跳转 rmc.bytedance.com；verifycenter 为该域验证流路径）
func (p *Platform) BlockURLPatterns() []string {
	return []string{"rmc.bytedance.com", "verifycenter", "/captcha"}
}

// DetectBlock 拦截页识别（A2 结构化两层：URL 层 + 结构层；旧全文子串已废弃）
func (p *Platform) DetectBlock(pageURL, pageSnapshot string) bool {
	return platform.URLHits(pageURL, p.BlockURLPatterns()) ||
		platform.StructuralMarkerHits(pageSnapshot, p.BlockMarkers())
}

// ClassifyError 平台错误归因（Postiz handleErrors 语义）
func (p *Platform) ClassifyError(errText string) platform.ErrType {
	switch {
	case strings.Contains(errText, "登录"), strings.Contains(errText, "401"), strings.Contains(errText, "sessionid"):
		return platform.ErrRefreshToken
	case strings.Contains(errText, "违规"), strings.Contains(errText, "敏感"), strings.Contains(errText, "filtered"):
		return platform.ErrBadBody
	case strings.Contains(errText, "验证码"), strings.Contains(errText, "rmc.bytedance"),
		strings.Contains(errText, "a_bogus"), strings.Contains(errText, "risk"):
		// 签名/风控：连续出现应 retire 账号——归 disconnect 由上层判定
		return platform.ErrDisconnect
	case strings.Contains(errText, "timeout"), strings.Contains(errText, "超时"),
		strings.Contains(errText, "未就绪"), strings.Contains(errText, "element_not_found"):
		return platform.ErrRetry
	default:
		// A3 fail-closed：判据未命中不再默认 retry（见 platform.ErrUnknown 注释）
		return platform.ErrUnknown
	}
}

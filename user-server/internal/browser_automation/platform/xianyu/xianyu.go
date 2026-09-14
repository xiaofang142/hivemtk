// Package xianyu — 闲鱼（goofish.com）适配器四件套。
// 调研依据（PLATFORM_BASE_DESIGN.md §1 能力矩阵）：
//   - 详情页 /item?id={item_id}；全 CSR，数据走 mtop（sign=MD5(token&t&appKey&data)，appKey=34839810）
//   - 评论发布接口无公开资料（调研明确标注未找到）→ 不声明 post_comment 能力
//   - 环境检测狠（baxia 滑块 + RGV587 实弹响应，本机 2026-09-09 实测）
//
// M2 期先落「读链路」（商品标题/价格/描述/卖家，DOM 可见即 extract）+ DetectBlock 判据。
package xianyu

import (
	"strings"

	"hivemtk-user/internal/browser_automation/platform"
)

// init 注册进基座注册表（新平台接入样板：只新增目录+init，基座零改动）
func init() {
	platform.Register(&Platform{})
}

// Platform 闲鱼适配器
type Platform struct{}

func (p *Platform) Identifier() string { return "xianyu" }

func (p *Platform) Capabilities() []platform.Capability {
	// 读能力 M2 先行；评论发布接口未公开，不声明（P4 fails-loudly 的反面：不吹不实现的能力）
	return []platform.Capability{
		platform.CapSearch,
		platform.CapOpenDetail,
	}
}

func (p *Platform) MaxConcurrentJobs() int { return 1 }

// Locators 平台元素定位表（DOM 知识唯一存放处；R20 真机校准 session118 实测）
func (p *Platform) Locators() map[string]string {
	return map[string]string{
		// R20 实测：商品卡片是 a[href*='/item?id='] 链接，长文本含标题+描述+价格+发货地
		"item_card":      "a[href*='/item?id=']",
		"item_title":     "a[href*='/item?id=']", // 卡片整体（标题混在卡片文本里，无独立节点）
		"item_price":     "[class*=price], [class*=Price]",
		"item_desc":      "[class*=desc], [class*=description]",
		"seller_name":    "[class*=seller-name], [class*=user-name]",
		"search_input":   "input[type=search], input[placeholder*=想要], input[placeholder*=搜索]",
		"item_link":      "a[href*='/item?id=']",
		"message_entry":  "[class*=message], [class*=chat]", // 私信（成熟生态走 WS 私信，M4+）
		"blocked_marker": "baxia, RGV587, 非法访问, 滑块",
	}
}

// DetectBlock 拦截页识别（本机实测：裸自动化 → 「非法访问」弹层；风控 → baxia 滑块 RGV587）
func (p *Platform) DetectBlock(pageSnapshot string) bool {
	for _, m := range []string{
		"baxia",  // 阿里风控域
		"RGV587", // 滑块验证错误码
		"非法访问",   // 弹层文案
		"滑动验证",   // 滑块文案
		"punish", // 阿里惩罚页路径
	} {
		if strings.Contains(pageSnapshot, m) {
			return true
		}
	}
	return false
}

// ClassifyError 平台错误归因（Postiz handleErrors 语义）
func (p *Platform) ClassifyError(errText string) platform.ErrType {
	switch {
	case strings.Contains(errText, "登录"), strings.Contains(errText, "401"), strings.Contains(errText, "cookie2"):
		return platform.ErrRefreshToken
	case strings.Contains(errText, "违规"), strings.Contains(errText, "敏感"), strings.Contains(errText, "FAIL_SYS_ILLEGAL_ACCESS"):
		return platform.ErrBadBody
	case strings.Contains(errText, "RGV587"), strings.Contains(errText, "baxia"),
		strings.Contains(errText, "滑块"), strings.Contains(errText, "punish"):
		// 风控惩罚：连续出现应 retire 账号——归 disconnect 由上层判定
		return platform.ErrDisconnect
	case strings.Contains(errText, "timeout"), strings.Contains(errText, "未就绪"),
		strings.Contains(errText, "element_not_found"):
		return platform.ErrRetry
	default:
		return platform.ErrRetry
	}
}

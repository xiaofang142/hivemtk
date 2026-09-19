// blockdetect.go — A2 结构化拦截判据（2026-09-19 批2）。
//
// 缺陷根因（旧实现）：文案 marker 对 snapshot 全文 Contains——评论/正文里出现
// 「验证码」三个字就误判拦截。业界调研（MediaCrawler 系风控判定）结论：拦截判定
// 必须分层——URL 重定向模式 / 风控专用 DOM 容器 / 结构化文案，全文子串是反模式。
//
// 分层判据（快照是扁平 a11y 行，无容器层级，故「专用容器」一层经 query 原语
// 由执行器下探，见 BlockSelectorDeclarer）：
//  1. URL 层：marker 只比对 pageURL（快照本身此前根本不含 URL——URL 型判据永不命中的第二缺陷）；
//  2. 结构层：文案 marker 只比对快照「结构行」——交互/标题角色节点行。
//     快照行格式 `[*]role "name" @eN`，正文节点（p/li/td/label…）role 恒为 text，
//     评论正文「讲讲验证码怎么过」是 text 行 → 不参与 marker 匹配。
package platform

import "strings"

// URLHits URL 层判据：pageURL 命中任一重定向/路径模式。
func URLHits(pageURL string, patterns []string) bool {
	if pageURL == "" {
		return false
	}
	low := strings.ToLower(pageURL)
	for _, p := range patterns {
		if p != "" && strings.Contains(low, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// StructuralMarkerHits 结构层判据：marker 只命中快照非 text 角色行的节点名称。
// 行解析失败（非标准格式，如空行）跳过——判据宁缺毋滥，闸门语义交给 URL 层。
func StructuralMarkerHits(snapshot string, markers []string) bool {
	if len(markers) == 0 {
		return false
	}
	for _, line := range strings.Split(snapshot, "\n") {
		l := strings.TrimPrefix(strings.TrimSpace(line), "*")
		q1 := strings.IndexByte(l, '"')
		if q1 < 0 {
			continue
		}
		role := strings.TrimSpace(l[:q1])
		if role == "" || role == "text" {
			continue // 正文内容节点：不参与拦截文案匹配（A2 误报根因）
		}
		rest := l[q1+1:]
		q2 := strings.LastIndexByte(rest, '"')
		if q2 < 0 {
			continue
		}
		name := rest[:q2]
		for _, m := range markers {
			if m != "" && strings.Contains(name, m) {
				return true
			}
		}
	}
	return false
}

// BlockSelectorDeclarer 风控专用 DOM 容器选择器（可选实现）。
// 只登记有公开调研实证的容器（如阿里 baxia 弹层），不猜——猜选择器是 B8 批判的形态。
// 执行器经 query exists 原语下探命中即判拦截（fail-soft：query 出错不作为证据）。
type BlockSelectorDeclarer interface {
	BlockSelectors() []string
}

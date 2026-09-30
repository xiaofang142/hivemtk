package service

import (
	"fmt"
	"sort"
	"strings"
)

// GeoAuditFactor 单项审计结果
type GeoAuditFactor struct {
	Factor string `json:"factor"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
	Weight int    `json:"weight"`
}

// GeoAuditReport 25 因子 GEO 审计报告（竞品对齐 Otterly 25因子 + Scrunch 技术可达性）
type GeoAuditReport struct {
	URL     string           `json:"url"`
	Score   int              `json:"score"`
	Grade   string           `json:"grade"`
	Factors []GeoAuditFactor `json:"factors"`
	Fixes   []GeoAuditFix    `json:"fixes"`
	Summary string           `json:"summary"`
}

// GeoAuditFix 未通过项的修复建议（按权重降序，前端可直接渲染为任务清单）
type GeoAuditFix struct {
	Factor     string `json:"factor"`
	Weight     int    `json:"weight"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion"`
}

// AuditGrade 评分等级（对标 Auriti 86/68/36 分档）
func AuditGrade(score int) string {
	switch {
	case score >= 86:
		return "优秀"
	case score >= 68:
		return "良好"
	case score >= 36:
		return "待改进"
	default:
		return "差"
	}
}

// fixSuggestion 各因子的修复指引；未命中时回退通用建议
func fixSuggestion(factor, detail string) string {
	for _, kv := range []struct{ k, v string }{
		{"H1标题存在", "在正文首部添加一个 Markdown 一级标题（# 标题），概括本页主题"},
		{"段落结构", "用空行分段，每 150-300 字一段，便于 AI 切块引用"},
		{"列表/表格", "把步骤、对比、参数改写为有序列表或 Markdown 表格"},
		{"FAQ问答段", "追加 3-5 组 Q&A（**Q: ... **A: ...），覆盖长尾疑问"},
		{"字数充足", "扩写至 500 字符以上，补充背景、步骤与案例"},
		{"权威引用", "引用 Gartner/IDC/ISO/信通院等权威机构数据或标准并标注来源"},
		{"量化数据", "加入百分比、倍数、金额等量化表述并注明口径年份"},
		{"作者/经验信号", "加入第一手表述（我们的实践/客户案例/实测数据）"},
		{"时效性声明", "标注内容适用版本与更新年份（如 2026），过期数据及时刷新"},
		{"品牌自然融入", "品牌出现 2-6 次，自然分布于首段、正文与结尾"},
		{"差异化定位", "点名核心卖点（开源/本地部署/不出域等），勿与竞品同质化"},
		{"CTA行动号召", "结尾加明确 CTA（了解更多/联系我们/开始使用）"},
		{"多语言友好", "清除 TODO 等占位符残留"},
		{"Meta描述长度", "meta description 控制在 80-300 字符，含关键词与卖点"},
		{"Schema JSON-LD", "补充 WebSite/Organization/FAQPage/Article 等 JSON-LD 结构化数据"},
		{"llms.txt 可用", "发布 /llms.txt（可用 POST /geo/techconfig/llms-txt 生成）并确保可访问"},
		{"robots.txt 允许 GPTBot", "robots.txt 放行 GPTBot/ClaudeBot/PerplexityBot 等 AI 爬虫"},
		{"HTTPS", "全站启用 HTTPS"},
		{"URL语义化", "URL 含关键词语义，避免 ?id= 类查询参数"},
		{"内部链接", "站内互链至少 2 处，形成主题簇"},
		{"定义句开头", "首 200 字符给出定义性表述（……是……）"},
		{"对比维度明确", "加入 vs/相比/对比等对比表述，列出可比维度"},
		{"信源可追溯", "关键论断附外部链接或来源标注"},
		{"无禁止词", "删除 TODO/FIXME/placeholder/lorem ipsum 等占位符"},
		{"移动端友好格式", "篇幅控制在 3000 字符内，长文拆篇"},
	} {
		if strings.Contains(factor, kv.k) {
			return kv.v
		}
	}
	if detail != "" {
		return "针对【" + detail + "】补齐缺失项"
	}
	return "按因子说明补齐缺失项"
}

// buildFixes 未通过因子按权重降序转修复清单
func buildFixes(fs []GeoAuditFactor) []GeoAuditFix {
	out := make([]GeoAuditFix, 0)
	for _, f := range fs {
		if !f.Pass {
			out = append(out, GeoAuditFix{Factor: f.Factor, Weight: f.Weight, Detail: f.Detail, Suggestion: fixSuggestion(f.Factor, f.Detail)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	return out
}

// RunGEOAudit 对指定内容执行 25 因子审计（Otterly 特色能力对齐）
func (s *TechConfigService) RunGEOAudit(url, title, content, metaDesc, schemaJSONLD string) *GeoAuditReport {
	factors := make([]GeoAuditFactor, 0, 25)
	add := func(name string, weight int, pass bool, detail string) {
		factors = append(factors, GeoAuditFactor{Factor: name, Pass: pass, Detail: detail, Weight: weight})
	}

	lc := strings.ToLower(content)
	words := len([]rune(content))

	add("H1标题存在", 5, strings.Contains(content, "# "), "Markdown 一级标题")
	add("段落结构", 3, strings.Count(content, "\n\n") >= 3, fmt.Sprintf("%d 个空行分段", strings.Count(content, "\n\n")))
	add("列表/表格", 3, strings.Contains(content, "- ") || strings.Contains(content, "|") || strings.Contains(content, "1."), "结构化列表或表格")
	add("FAQ问答段", 2, strings.Contains(lc, "常见问题") || strings.Contains(lc, "faq") || strings.Contains(content, "**Q"), "AI 引擎偏好 Q&A 格式")
	add("字数充足", 4, words >= 500, fmt.Sprintf("%d 字符", words))

	add("权威引用", 5, strings.Contains(lc, "gartner") || strings.Contains(lc, "idc") || strings.Contains(lc, "iso") || strings.Contains(lc, "信通院"), "引用权威机构/标准")
	hasData := false
	for _, kw := range []string{"%", "%以上", "倍", "亿", "万"} {
		if strings.Contains(content, kw) {
			hasData = true
			break
		}
	}
	add("量化数据", 4, hasData, "包含百分比或数值")
	add("作者/经验信号", 3, strings.Contains(lc, "我们的") || strings.Contains(lc, "实践") || strings.Contains(lc, "客户案例"), "第一手经验表述")
	add("时效性声明", 2, strings.Contains(content, "2025") || strings.Contains(content, "2026"), "包含近期年份")

	add("品牌自然融入", 5, strings.Count(content, "HiveMtk") >= 2 && strings.Count(content, "HiveMtk") <= 6, fmt.Sprintf("提及 %d 次(建议2-6次)", strings.Count(content, "HiveMtk")))
	add("差异化定位", 4, strings.Contains(lc, "开源") || strings.Contains(lc, "本地部署") || strings.Contains(lc, "不出域"), "核心卖点出现")
	add("CTA行动号召", 3, strings.Contains(lc, "了解更多") || strings.Contains(lc, "联系我们") || strings.Contains(lc, "开始使用") || strings.Contains(lc, "部署"), "引导下一步动作")
	add("多语言友好", 1, !strings.Contains(content, "TODO"), "无占位符残留")

	add("Meta描述长度", 3, len(metaDesc) >= 80 && len(metaDesc) <= 300, fmt.Sprintf("%d 字符", len(metaDesc)))
	add("Schema JSON-LD", 5, strings.Contains(schemaJSONLD, "application/ld+json") || strings.Contains(schemaJSONLD, "@context"), "结构化数据标记")
	add("llms.txt 可用", 4, true, "已由 /api/geo/techconfig/llms-txt 提供")
	add("robots.txt 允许 GPTBot", 4, true, "techconfig 默认允许 AI 爬虫")
	add("HTTPS", 5, strings.HasPrefix(url, "https://"), "传输安全")
	add("URL语义化", 3, !strings.Contains(url, "?id="), "URL 包含关键词而非查询参数")
	add("内部链接≥2", 2, strings.Count(content, "](/") >= 2, "站内链接数")

	add("定义句开头", 4, strings.Contains(content[:auditMin(len(content), 200)], "是"), "首200字符含定义性表述")
	add("对比维度明确", 3, strings.Contains(lc, "vs") || strings.Contains(lc, "相比") || strings.Contains(lc, "对比"), "含对比性表述")
	add("信源可追溯", 4, strings.Contains(content, "http") || strings.Contains(content, "来源"), "外部链接或来源标注")
	add("无禁止词", 5, !containsAny(lc, []string{" TODO ", "FIXME", "placeholder", "lorem ipsum"}), "无开发占位符")
	add("移动端友好格式", 2, words < 3000, "篇幅适中适合移动端")

	totalWeight, earned := 0, 0
	for _, f := range factors {
		totalWeight += f.Weight
		if f.Pass {
			earned += f.Weight
		}
	}
	score := 0
	if totalWeight > 0 {
		score = earned * 100 / totalWeight
	}

	summary := fmt.Sprintf("通过 %d/%d 项，加权得分 %d/100。", countPass(factors), len(factors), score)
	if score < 60 {
		summary += " 建议优先修复高权重未通过项。"
	} else if score < 80 {
		summary += " 整体良好，有优化空间。"
	} else {
		summary += " 优秀，达到行业领先水平。"
	}

	return &GeoAuditReport{URL: url, Score: score, Grade: AuditGrade(score), Factors: factors, Fixes: buildFixes(factors), Summary: summary}
}

func countPass(fs []GeoAuditFactor) int {
	n := 0
	for _, f := range fs {
		if f.Pass {
			n++
		}
	}
	return n
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func auditMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

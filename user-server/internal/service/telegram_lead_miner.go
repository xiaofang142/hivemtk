// Package service - Telegram 群发言意向识别（纯函数，P0-c 后）
//
// P0-c 收口：本文件原先只保留 mineTelegramGroupLead → MineUnifiedLead 的生产调用，
// 该双轨发掘已由 telegram dispatch 落 hub 后（InboxIngressService.persistMessage）
// 统一 LeadDiscoveryService.Process → Service.Enqueue 取代。这里只保留：
//   - 参数中心阈值 seam（装配层与测试仍在对账）；
//   - DetectTelegramIntent / DetectUnifiedIntent 共用的纯关键词打分词库，
//     TG 群"已验证成员发言"触发门控仍靠它判断是否有商业问话信号；
//   - 描述格式化 / 账号键 / 去重等纯工具（单测与工具链使用）。
//
// 不再持有任何线索库写入、主动触达或 WebhookService 依赖。
package service

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultTgLeadOpportunityThreshold Telegram 线索判为「商机」的最低意向分（参数中心未配置时的兜底）
const DefaultTgLeadOpportunityThreshold = 40

var tgLeadOpportunityThresholdProvider = func() int { return DefaultTgLeadOpportunityThreshold }

// SetTgLeadOpportunityThresholdProvider 由装配层注入参数中心读取口（nil 视为不注入）
func SetTgLeadOpportunityThresholdProvider(fn func() int) {
	if fn != nil {
		tgLeadOpportunityThresholdProvider = fn
	}
}

// tgLeadOpportunityThreshold 非正值一律回落兜底：阈值 ≤0 会让每条群发言都被判成商机，
// 线索库里全是噪声商机分，反而把真正的机会淹掉。
func tgLeadOpportunityThreshold() int {
	v := tgLeadOpportunityThresholdProvider()
	if v <= 0 {
		return DefaultTgLeadOpportunityThreshold
	}
	return v
}

var (
	tgEmailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	tgPhoneRe = regexp.MustCompile(`\+?\d[\d\-\s]{6,}\d`)
	tgLinkRe  = regexp.MustCompile(`(?i)(t\.me/|wa\.me/|https?://|whatsapp|wechat|微信|vx[:：]?|加我|私聊)`)
)

var tgHighIntentKeywords = []string{
	"多少钱", "价格", "报价", "怎么买", "购买", "下单", "批发", "代理", "加盟",
	"求购", "现货", "有货", "联系方式", "合作", "预算", "采购", "招商",
	"how much", "price", "cost", "quote", "quotation", "buy", "purchase",
	"order", "wholesale", "budget", "procurement", "distributor", "reseller",
}

var tgMediumIntentKeywords = []string{
	"咨询", "了解", "需要", "想要", "请问", "怎么卖", "优惠", "折扣", "试用",
	"方案", "效果", "质量", "发货", "物流", "功能", "多少",
	"interested", "need", "want", "looking for", "how to", "discount",
	"trial", "demo", "solution", "feature", "quality", "shipping",
}

// DetectTelegramIntent 对一条群发言做购买/合作意向打分。
// 返回：score(0-100)、命中的信号列表、是否达到「商机」阈值。
// 纯函数，便于单测。
func DetectTelegramIntent(text string) (score int, signals []string, isOpportunity bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, nil, false
	}
	lower := strings.ToLower(t)
	score = 8
	seen := map[string]bool{}
	add := func(sig string, w int) {
		if seen[sig] {
			return
		}
		seen[sig] = true
		signals = append(signals, sig)
		score += w
	}
	for _, kw := range tgHighIntentKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 25)
		}
	}
	for _, kw := range tgMediumIntentKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 12)
		}
	}
	if tgEmailRe.MatchString(t) || tgLinkRe.MatchString(t) || tgPhoneRe.MatchString(t) {
		add("联系方式", 35)
	}
	if score > 100 {
		score = 100
	}
	return score, signals, score >= tgLeadOpportunityThreshold()
}

func formatTelegramLeadDesc(groupTitle, snippet string, score int, signals []string, isOpportunity bool) string { //nolint:unused //// 仅被 *_test.go 引用，生产路径未用
	tag := "群发言线索"
	if isOpportunity {
		tag = "群发言商机"
	}
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		g = "未命名群组"
	}
	sig := "无"
	if len(signals) > 0 {
		sig = strings.Join(signals, ",")
	}
	snippet = trimRunes(strings.TrimSpace(snippet), 80)
	var b strings.Builder
	b.WriteString("[意向分:")
	b.WriteString(strconv.Itoa(score))
	b.WriteString("] [TG] ")
	b.WriteString(tag)
	b.WriteString(" | 群「")
	b.WriteString(g)
	b.WriteString("」 | 信号:")
	b.WriteString(sig)
	b.WriteString(" | 最近:「")
	b.WriteString(snippet)
	b.WriteString("」")
	return b.String()
}

func trimRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func telegramLeadAccountKey(username, fromID string) string {
	username = strings.TrimSpace(username)
	if username != "" {
		return "@" + strings.TrimPrefix(username, "@")
	}
	fromID = strings.TrimSpace(fromID)
	if fromID != "" && fromID != "0" {
		return "tg:" + fromID
	}
	return ""
}

// ProbeTgLeadOpportunityThreshold 暴露读取口当前值，供装配层测试断言接线确实生效。
func ProbeTgLeadOpportunityThreshold() int { return tgLeadOpportunityThreshold() }

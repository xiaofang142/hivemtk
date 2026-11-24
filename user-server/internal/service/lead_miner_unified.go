package service

// P0-c 后本文件只保留**无渠道写入副作用**的通用意向打分与参数中心阈值 seam：
//   - TG 群"已验证成员发言"触发门控（webhook_channel_telegram.go tgVerifiedSpeechWorthReply）与商机触发门控
//     仍靠本打分判断是否有商业问话信号；
//   - 参数中心 inbox_sales.unified_miner_lead_threshold 的装配/测试 seam 仍在。
//
// 原来的 MineUnifiedLead / MineUnifiedLeadDetailed / ChannelLeadAdapter /
// recordUnifiedLeadScore 双轨发掘实现已删除：所有渠道落 hub 后统一走
// LeadDiscoveryService.Process → Service.Enqueue → 主 Service.process。
// 渠道资格由 channelgw.Default 的 MineMode 投影提供（service.SetLeadMiningChannels）。

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// ChannelLeadAdapter 渠道线索适配器接口（P0-c 后仅用于 LeadAdapterForPlatform
// 工厂与 TriggerOutreach 主动触达能力；发掘写库已统一走主 Service.process，
// 本接口不再承载任何线索库写入）。保留它的原因是既有适配器与触达链路仍依赖此形状。
type ChannelLeadAdapter interface {
	Channel() string
	ClueType() int64
	AccountKey(fromID, fromName, username string) string
	DisplayName(fromName, username, accountKey string) string
	ChatLabel(groupTitle string) string
	DescPrefix() string
	LeadTag(isOpportunity bool) string
	ExtraKeywords() (high, medium []string)
	OutreachMinScore() int
	TriggerOutreach(ctx context.Context, s *WebhookService, accountID, fromID, groupID, groupTitle string, score int, originalText string)
}

// DefaultUnifiedMinerOpportunityThreshold 通用意向打分判为「有效线索」的最低分（参数中心未配置时的兜底）
const DefaultUnifiedMinerOpportunityThreshold = 40

var unifiedMinerOpportunityThresholdProvider = func() int { return DefaultUnifiedMinerOpportunityThreshold }

// SetUnifiedMinerOpportunityThresholdProvider 由装配层注入参数中心读取口（nil 视为不注入）
func SetUnifiedMinerOpportunityThresholdProvider(fn func() int) {
	if fn != nil {
		unifiedMinerOpportunityThresholdProvider = fn
	}
}

// unifiedMinerOpportunityThreshold 非正值一律回落兜底：阈值 ≤0 会让每条群发言都判成商机。
func unifiedMinerOpportunityThreshold() int {
	v := unifiedMinerOpportunityThresholdProvider()
	if v <= 0 {
		return DefaultUnifiedMinerOpportunityThreshold
	}
	return v
}

// unifiedIntentBaseScore 打分起点：正文里没有任何意向词时的分数。
const unifiedIntentBaseScore = 8

var unifiedMeaningfulRe = regexp.MustCompile(`[\p{L}\p{N}]`)

var (
	unifiedEmailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	unifiedPhoneRe = regexp.MustCompile(`\+?\d[\d\-\s]{6,}\d`)
	unifiedLinkRe  = regexp.MustCompile(`(?i)(t\.me/|wa\.me/|https?://|whatsapp|wechat|微信|vx[:：]?|加我|私聊)`)
)

var unifiedHighIntentKeywords = []string{
	"多少钱", "价格", "报价", "怎么买", "购买", "下单", "批发", "代理", "加盟",
	"求购", "现货", "有货", "联系方式", "合作", "预算", "采购", "招商",
	"how much", "price", "cost", "quote", "quotation", "buy", "purchase",
	"order", "wholesale", "budget", "procurement", "distributor", "reseller",
}

var unifiedMediumIntentKeywords = []string{
	"咨询", "了解", "需要", "想要", "请问", "怎么卖", "优惠", "折扣", "试用",
	"方案", "效果", "质量", "发货", "物流", "功能", "多少",
	"interested", "need", "want", "looking for", "how to", "discount",
	"trial", "demo", "solution", "feature", "quality", "shipping",
}

// DetectUnifiedIntent 通用意向打分（纯函数）：合并 TG/抖音两套关键词库，支持扩展词。
// 返回 score(0-100)、命中信号列表、是否达到「商机」阈值。
func DetectUnifiedIntent(text string, extraHigh, extraMedium []string) (score int, signals []string, isOpportunity bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, nil, false
	}
	lower := strings.ToLower(t)
	score = unifiedIntentBaseScore
	seen := map[string]bool{}
	add := func(sig string, w int) {
		if seen[sig] {
			return
		}
		seen[sig] = true
		signals = append(signals, sig)
		score += w
	}
	for _, kw := range unifiedHighIntentKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 25)
		}
	}
	for _, kw := range extraHigh {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 25)
		}
	}
	for _, kw := range unifiedMediumIntentKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 12)
		}
	}
	for _, kw := range extraMedium {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			add(kw, 12)
		}
	}
	if unifiedEmailRe.MatchString(t) || unifiedLinkRe.MatchString(t) || unifiedPhoneRe.MatchString(t) {
		add("联系方式", 35)
	}
	if score > 100 {
		score = 100
	}
	return score, signals, score >= unifiedMinerOpportunityThreshold()
}

// FormatUnifiedLeadDesc 通用线索描述生成（纯函数，内嵌 [意向分:NN] 供增量更新解析）。
func FormatUnifiedLeadDesc(prefix, leadTag, chatLabel, snippet string, score int, signals []string) string {
	sig := "无"
	if len(signals) > 0 {
		sig = strings.Join(signals, ",")
	}
	snippet = trimRunes(strings.TrimSpace(snippet), 80)
	var b strings.Builder
	b.WriteString("[意向分:")
	b.WriteString(strconv.Itoa(score))
	b.WriteString("] ")
	b.WriteString(prefix)
	b.WriteString(" ")
	b.WriteString(leadTag)
	b.WriteString(" | 会话「")
	b.WriteString(chatLabel)
	b.WriteString("」 | 信号:")
	b.WriteString(sig)
	b.WriteString(" | 最近:「")
	b.WriteString(snippet)
	b.WriteString("」")
	return b.String()
}

// ProbeUnifiedMinerOpportunityThreshold 暴露读取口当前值，供装配层测试断言接线确实生效。
func ProbeUnifiedMinerOpportunityThreshold() int { return unifiedMinerOpportunityThreshold() }

// Package service - Telegram 群发言 → 销售线索/商机 自动挖掘
//
// 业务背景：
//
//	Telegram 群里每个真实发言者都是潜在客户——发言即诉求，诉求即线索，线索即商机。
//	因此不能把 AI 触发收敛为"仅回复机器人 / @机器人"，而应把「群消息」当作线索来源全量捕获。
//
// 设计要点（与 AI 自动回复解耦，互不影响）：
//  1. 静默运行：只写线索库（clues）+ 线索评分（clue_scores），不向群里发任何消息，因此绝不刷屏。
//  2. 去重：按 (Type=Telegram, account) 去重，每个独立发言者仅生成一条线索；
//     后续更高意向的发言会「增量更新」该线索的意向分（取最高分），不会重复建条。
//  3. 意向识别：中英双语关键词 + 联系方式信号打分（0-100），score>=40 记为「商机」。
//  4. best-effort：任何 DB / 评分异常都不影响入站主链路（消息中台 / 收件箱 / AI 回复）。
package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
)

const tgLeadOpportunityThreshold = 40

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
	return score, signals, score >= tgLeadOpportunityThreshold
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

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "23505") ||
		strings.Contains(msg, "重复")
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

func (s *WebhookService) mineTelegramGroupLead(ctx context.Context, hub *model.MessageHub, accountID, groupID, groupTitle, fromID, username, fromName, text string) (newOpportunity bool) {
	return MineUnifiedLead(ctx, s, hub, TelegramLeadAdapter{}, accountID, groupID, groupTitle, fromID, fromName, username, text)
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

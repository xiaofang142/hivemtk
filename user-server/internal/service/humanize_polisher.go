package service

import (
	"context"
	"math/rand"
	"regexp"
	"strings"
)

// HumanizePolisher 拟人润色器
type HumanizePolisher struct {
	enableParticles  bool
	enableTruncation bool
	// enableTypo 错别字注入开关（内容审计 2026-10-28 §5-③）：与"错别字默认关闭"
	// 的产品口径对齐，默认 false——原实现默认生效（10% 概率替换"好的/是的"类结尾），
	// 属实现与口径打架。显式开启走 EnableTypoInjection。
	enableTypo    bool
	maxLength     int
	platformStyle PlatformStyle
	particlePool  []string
	emojiPool     map[string][]string
	randFn        func() float64
}

// PlatformStyle 平台风格
type PlatformStyle struct {
	AllowEmoji     bool
	AllowFormality bool
	StyleName      string
}

// PolishContext 润色上下文
type PolishContext struct {
	Persona      string
	Intent       string
	Platform     string
	Stage        string
	CustomerName string
}

// NewHumanizePolisher 创建拟人润色器
func NewHumanizePolisher() *HumanizePolisher {
	return &HumanizePolisher{
		enableParticles:  true,
		enableTruncation: true,
		maxLength:        200,
		platformStyle: PlatformStyle{
			AllowEmoji:     true,
			AllowFormality: false,
			StyleName:      "default",
		},
		particlePool: []string{"嗯", "那个", "对了", "哈哈", "呢", "哦", "啦", "呀"},
		emojiPool: map[string][]string{
			"wechat":      {"😊", "👍", "💪", "✨"},
			"douyin":      {"🎉", "🔥", "💯", "👍"},
			"xiaohongshu": {"🌟", "💕", "✨", "😊"},
			"xianyu":      {"😊", "👍", "🉑"},
			"tiktok":      {"🔥", "💯", "🎉"},
		},
	}
}

// Polish 拟人润色
func (p *HumanizePolisher) Polish(ctx context.Context, raw string, pctx *PolishContext) (string, error) {
	if raw == "" {
		return raw, nil
	}
	polished := raw

	polished = p.removeAITraces(ctx, polished)

	polished = p.removeExtraSymbols(ctx, polished)

	polished = filterExtremeClaims(polished)

	polished = p.applyPlatformStyle(ctx, polished, pctx)

	if p.enableTruncation {
		polished = p.truncateByLength(ctx, polished, p.maxLength)
	}

	if pctx != nil && pctx.CustomerName != "" {
		polished = p.personalize(ctx, polished, pctx.CustomerName, pctx.Platform)
	}

	if pctx == nil || !p.shouldPreserveLeadingAck(pctx) {
		polished = p.removeLeadingFlattery(polished)
	}

	if p.enableParticles && pctx != nil && p.shouldAddParticle(ctx, pctx) {
		polished = p.addNaturalParticle(ctx, polished, pctx)
	}

	polished = p.injectTypo(polished, pctx)

	return strings.TrimSpace(polished), nil
}

func (p *HumanizePolisher) shouldPreserveLeadingAck(pctx *PolishContext) bool {
	if pctx == nil {
		return false
	}
	switch pctx.Intent {
	case IntentComplaint, IntentChurn, IntentAfterSale:
		return true
	}
	return false
}

func (p *HumanizePolisher) removeLeadingFlattery(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	trimPrefixes := []string{"好的", "可以的", "没问题", "当然可以"}
	for _, prefix := range trimPrefixes {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		after := strings.TrimPrefix(trimmed, prefix)
		after = strings.TrimLeft(after, " 　\t")
		if after == "" {
			return text
		}
		first := []rune(after)[0]
		isSeparator := first == ',' || first == '，' || first == '.' || first == '。' ||
			first == '!' || first == '！' || first == '?' || first == '？' ||
			first == ':' || first == '：' || first == ';' || first == '；'
		isEmoji := first >= 0x1F000
		if isSeparator || isEmoji {
			return strings.TrimLeft(after, " 　\t")
		}
		return text
	}
	return text
}

func (p *HumanizePolisher) removeAITraces(ctx context.Context, text string) string {
	aiTraces := []string{
		"作为 AI 助手",
		"作为人工智能",
		"我是 AI",
		"我是人工智能",
		"我是一个 AI",
		"作为一个",
		"我的能力有限",
		"根据您提供的信息",
		"我理解您的",
		"作为您的销售顾问",
		"非常感谢您的咨询",
		"非常感谢您的提问",
		"感谢您的咨询",
		"感谢您的提问",
		"感谢您的关注",
		"很高兴为您服务",
		"很高兴能帮到您",
		"很高兴为您解答",
		"很荣幸为您服务",
		"非常荣幸为您介绍",
		"感谢您的信任",
		"非常感谢您的信任",
		"很乐意为您解答",
		"随时为您服务",
		"期待您的回复",
		"期待您的反馈",
		"可以的！",
		"可以的!",
		"好的！",
		"好的!",
		"没问题！",
		"没问题!",
		"当然可以！",
		"当然可以!",
		"很抱歉，我无法",
		"很抱歉，无法",
		"非常抱歉，给您带来不便",
		"造成不便深表歉意",
		"请您谅解",
		"请谅解",
	}
	for _, trace := range aiTraces {
		text = strings.ReplaceAll(text, trace, "")
	}
	return text
}

var adLawReplacements = []struct{ From, To string }{
	{"全网最低价", "价格很有优势"},
	{"全网最低", "价格很有优势"},
	{"史上最低", "很有竞争力的价格"},
	{"国家级", "高标准"},
	{"世界级", "高标准"},
	{"销量第一", "销量领先"},
	{"行业第一", "行业领先"},
	{"排名第一", "排名靠前"},
	{"第一品牌", "领先品牌"},
	{"百分百", "很大程度上"},
	{"100%", "很高比例的"},
	{"最低价", "实惠的价格"},
	{"最先进", "先进"},
	{"最好", "很好"},
	{"最优", "优秀"},
	{"最强", "很强"},
	{"最佳", "优选"},
	{"根治", "有效改善"},
	{"绝对", "确实"},
	{"顶级", "高端"},
	{"极致", "出色"},
}

func filterExtremeClaims(text string) string {
	for _, r := range adLawReplacements {
		text = strings.ReplaceAll(text, r.From, r.To)
	}
	return text
}

func (p *HumanizePolisher) removeExtraSymbols(ctx context.Context, text string) string {
	multiBang := regexp.MustCompile(`[!！]{2,}`)
	text = multiBang.ReplaceAllString(text, "！")

	multiQuestion := regexp.MustCompile(`[?？]{2,}`)
	text = multiQuestion.ReplaceAllString(text, "？")

	multiDot := regexp.MustCompile(`\.{3,}`)
	text = multiDot.ReplaceAllString(text, "……")

	return text
}

func (p *HumanizePolisher) applyPlatformStyle(ctx context.Context, text string, pctx *PolishContext) string {
	if pctx == nil || pctx.Platform == "" {
		return text
	}
	style := p.getStyleForPlatform(ctx, pctx.Platform)

	if !style.AllowEmoji {
		emojiRegex := regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]`)
		text = emojiRegex.ReplaceAllString(text, "")
	}

	if style.AllowFormality {
		text = strings.ReplaceAll(text, "嗯", "是的")
		text = strings.ReplaceAll(text, "哈哈", "呵呵")
	}

	return text
}

func (p *HumanizePolisher) getStyleForPlatform(ctx context.Context, platform string) PlatformStyle {
	switch strings.ToLower(platform) {
	case "wechat", "weixin":
		return PlatformStyle{AllowEmoji: true, AllowFormality: false, StyleName: "wechat"}
	case "douyin":
		return PlatformStyle{AllowEmoji: true, AllowFormality: false, StyleName: "douyin"}
	case "xiaohongshu", "xhs":
		return PlatformStyle{AllowEmoji: true, AllowFormality: false, StyleName: "xhs"}
	case "xianyu":
		return PlatformStyle{AllowEmoji: true, AllowFormality: false, StyleName: "xianyu"}
	case "tiktok":
		return PlatformStyle{AllowEmoji: true, AllowFormality: false, StyleName: "tiktok"}
	case "whatsapp", "telegram":
		return PlatformStyle{AllowEmoji: true, AllowFormality: true, StyleName: "im"}
	case "email", "mail":
		return PlatformStyle{AllowEmoji: false, AllowFormality: true, StyleName: "email"}
	default:
		return p.platformStyle
	}
}

func (p *HumanizePolisher) truncateByLength(ctx context.Context, text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	if maxLen > 0 {
		return string(runes[:maxLen-1]) + "…"
	}
	return text
}

func (p *HumanizePolisher) personalize(ctx context.Context, text, name, platform string) string {
	if name == "" {
		return text
	}
	if strings.Contains(text, name) || strings.Contains(text, "亲") || strings.Contains(text, "您") {
		return text
	}
	return name + "，" + text
}

func (p *HumanizePolisher) shouldAddParticle(ctx context.Context, pctx *PolishContext) bool {
	noParticleIntents := map[string]bool{
		IntentComplaint: true,
		IntentChurn:     true,
		IntentAfterSale: true,
	}
	if pctx != nil && noParticleIntents[pctx.Intent] {
		return false
	}
	return true
}

func (p *HumanizePolisher) addNaturalParticle(ctx context.Context, text string, pctx *PolishContext) string {
	if text == "" || len([]rune(text)) > 60 {
		return text
	}
	shortReply := map[string]bool{
		"好的": true, "是的": true, "可以": true, "没问题": true, "OK": true,
	}
	if shortReply[text] {
		return "嗯，" + text
	}
	return text
}

var typoPool = map[string][]string{
	"好的":  {"好哒", "ok", "好的呀", "好滴"},
	"好":   {"好的~", "ok", "好哒"},
	"是的":  {"对的", "嗯嗯", "是哒"},
	"好的呢": {"好的呀", "好的~"},
}

func (p *HumanizePolisher) randFloat() float64 {
	if p.randFn != nil {
		return p.randFn()
	}
	return rand.Float64()
}

func (p *HumanizePolisher) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(p.randFloat() * float64(n))
}

// EnableTypoInjection 显式开启错别字注入（默认关闭，见 enableTypo 字段注释）。
// 仅供明确要求"拟人化拉满"的 persona 场景调用；测试用 SetRandFn 控制命中。
func (p *HumanizePolisher) EnableTypoInjection() {
	p.enableTypo = true
}

func (p *HumanizePolisher) injectTypo(text string, pctx *PolishContext) string {
	// 默认关闭（enableTypo 注释）：口径与实现自洽的唯一闸口，调用点不再重复判
	if !p.enableTypo || pctx == nil {
		return text
	}
	persona := strings.ToLower(pctx.Persona)
	if strings.Contains(persona, "专业") || strings.Contains(persona, "客服") {
		return text
	}
	if p.randFloat() > 0.1 {
		return text
	}
	for orig, variants := range typoPool {
		if strings.HasSuffix(text, orig) {
			replacement := variants[p.randIntn(len(variants))]
			text = text[:len(text)-len(orig)] + replacement
			break
		}
	}
	return text
}

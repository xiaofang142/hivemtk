package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	dyLeadOpportunityThreshold = 50
	dyDMOutreachCooldown       = 60 * time.Minute
	dyDMOutreachMinScore       = 60
)

type DouyinIntegrationService struct {
	db any
}

func NewDouyinIntegrationService(db any) *DouyinIntegrationService {
	return &DouyinIntegrationService{db: db}
}

func (s *DouyinIntegrationService) SendMessage(ctx context.Context, accountID, conversationID, content string) error {
	if s == nil {
		return nil
	}

	if err := DeliverBridgeOutbound(ctx, "douyin", accountID, conversationID, "text", content, ""); err != nil {
		logger.Warnf("[Douyin] 私信发送失败 account=%s conv=%s: %v", accountID, conversationID, err)
		return fmt.Errorf("douyin deliver: %w", err)
	}

	logger.Infof("[Douyin] 私信已投递 account=%s conv=%s", accountID, conversationID)
	return nil
}

func (s *DouyinIntegrationService) SendCard(ctx context.Context, accountID, conversationID string, card *model.RichCard) error {
	if s == nil || card == nil {
		return nil
	}
	content := fmt.Sprintf("[卡片] %s", card.Title)
	if card.Description != "" {
		content += "\n" + card.Description
	}
	if url := card.Buttons; len(url) > 0 && url[0].URL != "" {
		content += "\n链接: " + url[0].URL
	}
	return s.SendMessage(ctx, accountID, conversationID, content)
}

// dmOutreachAllowed 私信触达冷却闸：同一 key 在 dyDMOutreachCooldown 内只放行一次。
func dmOutreachAllowed(ctx context.Context, key string) bool {
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", dyDMOutreachCooldown)
	if err != nil {
		return true
	}
	return set
}

func (s *DouyinIntegrationService) DMOutreachAllowed(ctx context.Context, accountID, userID string) bool {
	return dmOutreachAllowed(ctx, "mtk:dy:dm_outreach:"+accountID+":"+userID)
}

var (
	dyHighIntentKeywords = []string{
		"价格", "多少钱", "报价", "怎么买", "购买", "下单", "批发", "代理", "加盟",
		"求购", "现货", "有货", "联系方式", "合作", "预算", "采购", "招商",
		"直播间", "小黄车", "链接", "橱窗",
	}

	dyMediumIntentKeywords = []string{
		"咨询", "了解", "需要", "想要", "请问", "怎么卖", "优惠", "折扣", "试用",
		"方案", "效果", "质量", "发货", "物流", "功能", "多少",
		"推荐", "介绍", "分享",
	}

	dyContactRe = regexpMustCompile(`(微信|vx[:：]?|加我|私聊|联系|电话|手机|wx[:：]?)`)
	dyPhoneRe   = regexpMustCompile(`\+?\d[\d\-\s]{6,}\d`)
)

func regexpMustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func DetectDouyinIntent(text string) (score int, signals []string, isOpportunity bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, nil, false
	}
	lower := strings.ToLower(t)
	score = 5
	seen := map[string]bool{}
	add := func(sig string, w int) {
		if seen[sig] {
			return
		}
		seen[sig] = true
		signals = append(signals, sig)
		score += w
	}
	for _, kw := range dyHighIntentKeywords {
		if strings.Contains(lower, kw) {
			add(kw, 20)
		}
	}
	for _, kw := range dyMediumIntentKeywords {
		if strings.Contains(lower, kw) {
			add(kw, 10)
		}
	}
	if dyContactRe.MatchString(t) || dyPhoneRe.MatchString(t) {
		add("联系方式", 30)
	}
	if score > 100 {
		score = 100
	}
	return score, signals, score >= dyLeadOpportunityThreshold
}

func FormatDouyinLeadDesc(groupTitle, snippet string, score int, signals []string, isOpportunity bool) string {
	tag := "抖音线索"
	if isOpportunity {
		tag = "抖音商机"
	}
	g := strings.TrimSpace(groupTitle)
	if g == "" {
		g = "直播间/群组"
	}
	sig := "无"
	if len(signals) > 0 {
		sig = strings.Join(signals, ",")
	}
	snippet = trimRunes(strings.TrimSpace(snippet), 80)
	var b strings.Builder
	b.WriteString("[意向分:")
	b.WriteString(fmt.Sprintf("%d", score))
	b.WriteString("] [Douyin] ")
	b.WriteString(tag)
	b.WriteString(" | 群/直播间「")
	b.WriteString(g)
	b.WriteString("」 | 信号:")
	b.WriteString(sig)
	b.WriteString(" | 最近:「")
	b.WriteString(snippet)
	b.WriteString("」")
	return b.String()
}

func BuildDouyinDMWelcome(groupTitle, originalText string) string {
	groupLabel := strings.TrimSpace(groupTitle)
	if groupLabel == "" {
		groupLabel = "直播间"
	}
	templates := []string{
		fmt.Sprintf("你好！看到你在「%s」的发言，我是做这方面产品的，想了解一下你的具体需求。方便聊聊吗？", groupLabel),
		fmt.Sprintf("您好！关注到你对「%s」的内容感兴趣，我们提供相关产品/服务，是否方便进一步沟通？", groupLabel),
		fmt.Sprintf("哈喽！在「%s」看到你的留言，我这边正好有相关资源，可以聊聊你的需求吗？", groupLabel),
	}
	idx := 0
	if len(originalText) > 0 {
		for _, r := range originalText {
			if r >= 0x4E00 && r <= 0x9FFF {
				idx = 0
				break
			}
		}
		if idx == 0 {
			hasASCII := false
			for _, r := range originalText {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					hasASCII = true
					break
				}
			}
			if hasASCII {
				idx = 1
			}
		}
	}
	return templates[idx]
}

// Package service - 通用线索发掘器（所有渠道复用）
//
// 设计目标：
//  1. 统一所有渠道（TG/抖音/小红书/TikTok/快手/闲鱼/WhatsApp/企微/飞书/...）的线索发现逻辑。
//  2. 每个渠道只需实现 ChannelLeadAdapter 接口，提供账号键/线索类型/描述格式化/主动触达。
//  3. 关键词打分逻辑通用化（合并 DetectTelegramIntent / DetectDouyinIntent），支持渠道扩展词。
//  4. 幂等去重 + 意向分增量更新 + 评分记录全部复用同一套实现。
//  5. best-effort：任何异常都不影响入站主链路。
package service

import (
	"context"
	"log"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ChannelLeadAdapter 渠道线索适配器：每个渠道实现一份，封装渠道特定行为。
type ChannelLeadAdapter interface {
	// Channel 返回渠道标识（用于日志/评分事件），如 "telegram"、"douyin"、"xiaohongshu"。
	Channel() string
	// ClueType 返回该渠道对应的线索类型 ID。
	ClueType() int64
	// AccountKey 生成去重键（优先用可读用户名，否则回退 <channel>:<id>）。
	AccountKey(fromID, fromName, username string) string
	// DisplayName 返回线索展示名。
	DisplayName(fromName, username, accountKey string) string
	// ChatLabel 返回群/直播间/会话的展示标签（无群名时回退渠道默认）。
	ChatLabel(groupTitle string) string
	// DescPrefix 返回描述前缀标签，如 "[TG]"、"[Douyin]"、"[Xiaohongshu]"。
	DescPrefix() string
	// LeadTag 返回线索/商机标签文案，如 "群发言线索"/"群发言商机"。
	LeadTag(isOpportunity bool) string
	// ExtraKeywords 返回该渠道额外的关键词（合并到通用词库），可为空。
	ExtraKeywords() (high, medium []string)
	// OutreachMinScore 返回主动触达的最低意向分阈值；返回 0 表示该渠道不主动触达。
	OutreachMinScore() int
	// TriggerOutreach 主动私信触达；minScore=0 或 nil adapter 时跳过。
	TriggerOutreach(ctx context.Context, s *WebhookService, accountID, fromID, groupID, groupTitle string, score int, originalText string)
}

const unifiedMinerOpportunityThreshold = 40

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

// DetectUnifiedIntent 通用意向打分：合并 TG/抖音两套关键词库，并支持渠道扩展词。
// 返回 score(0-100)、命中的信号列表、是否达到「商机」阈值。
func DetectUnifiedIntent(text string, extraHigh, extraMedium []string) (score int, signals []string, isOpportunity bool) {
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
	return score, signals, score >= unifiedMinerOpportunityThreshold
}

// FormatUnifiedLeadDesc 通用线索描述生成（内嵌 [意向分:NN] 供增量更新解析）。
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

// MineUnifiedLead 通用线索挖掘入口：所有渠道复用。
// 幂等去重 + 意向分增量更新 + 评分记录 + 主动触达，全过程 best-effort。
// 返回 newOpportunity：本次是否让该发言者「新晋为商机」（首次达到阈值，或由非商机→商机）。
func MineUnifiedLead(ctx context.Context, s *WebhookService, hub *model.MessageHub, adapter ChannelLeadAdapter, accountID, groupID, groupTitle, fromID, fromName, username, text string) (newOpportunity bool) {
	if s == nil || adapter == nil {
		return false
	}
	s.ensureReposFromDB(ctx)
	if s.clueRepo == nil {
		return false
	}
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "/") {
		return false
	}
	if !unifiedMeaningfulRe.MatchString(t) {
		return false
	}

	account := adapter.AccountKey(fromID, fromName, username)
	if account == "" {
		return false
	}
	name := adapter.DisplayName(fromName, username, account)

	extraHigh, extraMedium := adapter.ExtraKeywords()
	score, signals, isOpp := DetectUnifiedIntent(t, extraHigh, extraMedium)
	chatLabel := adapter.ChatLabel(groupTitle)
	leadTag := adapter.LeadTag(isOpp)
	desc := FormatUnifiedLeadDesc(adapter.DescPrefix(), leadTag, chatLabel, t, score, signals)

	msgID, convID, oneID := "", "", ""
	if hub != nil {
		msgID = hub.MsgID
		convID = hub.ConversationID
		oneID = hub.SenderID
	}

	clueType := adapter.ClueType()
	existing, err := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account)
	if err != nil {
		logger.Warnf("[UnifiedLeadMiner] 查询线索失败 channel=%s account=%s: %v", adapter.Channel(), account, err)
		return false
	}

	isGroup := strings.TrimSpace(groupTitle) != "" || (hub != nil && hub.IsGroup)

	if existing == nil {
		clue := &model.Clue{
			SourceID:       groupID,
			Account:        account,
			Type:           clueType,
			Name:           name,
			Desc:           desc,
			IntentScore:    int64(score),
			IsOpportunity:  boolToInt64(isOpp),
			MessageID:      msgID,
			ConversationID: convID,
			OneID:          oneID,
			IsGroup:        isGroup,
			GroupID:        groupID,
			GroupName:      groupTitle,
		}
		if cerr := s.clueRepo.Create(ctx, clue); cerr != nil {
			if re, rerr := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account); rerr == nil && re != nil {
				existing = re
			} else {
				if !isDuplicateKeyError(cerr) {
					logger.Warnf("[UnifiedLeadMiner] 创建线索失败 channel=%s account=%s: %v", adapter.Channel(), account, cerr)
				}
				return false
			}
		} else {
			logger.Infof("[UnifiedLeadMiner] 新增线索 channel=%s account=%s 意向分=%d 商机=%v 会话=%s",
				adapter.Channel(), account, score, isOpp, chatLabel)
			newOpportunity = isOpp
			recordUnifiedLeadScore(ctx, s, clue, adapter.Channel(), isOpp)
			if isOpp && score >= adapter.OutreachMinScore() && adapter.OutreachMinScore() > 0 {
				adapter.TriggerOutreach(ctx, s, accountID, fromID, groupID, groupTitle, score, text)
			}
			return newOpportunity
		}
	}

	if existing != nil && existing.IsOpportunity == 0 && isOpp && score >= adapter.OutreachMinScore() && adapter.OutreachMinScore() > 0 {
		logger.Infof("[UnifiedLeadMiner] 线索首次升级为商机，触发触达 channel=%s account=%s", adapter.Channel(), account)
		newOpportunity = true
		adapter.TriggerOutreach(ctx, s, accountID, fromID, groupID, groupTitle, score, text)
	}

	if int64(score) > existing.IntentScore {
		wasOpp := existing.IsOpportunity != 0
		updates := map[string]any{
			"desc":            desc,
			"source_id":       groupID,
			"intent_score":    int64(score),
			"is_opportunity":  boolToInt64(isOpp),
			"message_id":      msgID,
			"conversation_id": convID,
			"one_id":          oneID,
			"is_group":        isGroup,
			"group_id":        groupID,
			"group_name":      groupTitle,
		}
		if uerr := s.clueRepo.UpdateByID(ctx, existing.ID, updates); uerr != nil {
			logger.Warnf("[UnifiedLeadMiner] 更新线索失败 channel=%s id=%s: %v", adapter.Channel(), existing.ID, uerr)
		} else {
			existing.IntentScore = int64(score)
			existing.IsOpportunity = boolToInt64(isOpp)
			existing.Desc = desc
			existing.SourceID = groupID
			logger.Infof("[UnifiedLeadMiner] 更新线索意向分 channel=%s account=%s → %d 商机=%v",
				adapter.Channel(), account, score, isOpp)
			newOpportunity = isOpp && !wasOpp
			if isOpp && score >= adapter.OutreachMinScore() && adapter.OutreachMinScore() > 0 && newOpportunity {
				adapter.TriggerOutreach(ctx, s, accountID, fromID, groupID, groupTitle, score, text)
			}
		}
	}
	recordUnifiedLeadScore(ctx, s, existing, adapter.Channel(), isOpp)
	return newOpportunity
}

func recordUnifiedLeadScore(ctx context.Context, s *WebhookService, clue *model.Clue, channel string, isOpp bool) {
	if s == nil || clue == nil || clue.ID == "" {
		return
	}
	db := s.lazyDB()
	if db == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()
	scoreSvc := NewClueScoreServiceWithRepos(
		repository.NewClueScoreRepositoryWithDB(db),
		repository.NewClueEngagementRepositoryWithDB(db),
		s.clueRepo,
	)
	_ = scoreSvc.RecordEngagement(context.Background(), clue.ID, "group_message", channel, map[string]any{"is_opportunity": isOpp})
	_, _ = scoreSvc.ScoreClue(context.Background(), clue)
}

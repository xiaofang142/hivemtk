package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// LeadJudgeContext 二次 LLM 汇总判断的富上下文（FR-013）。
//
// 相比旧的 Judge(cfg, history)，本上下文额外携带：三粒度规则、三路检测证据、
// ConversationContext 多轮/历史消息与成员角色分类。dispatcherJudge 新增
// JudgeWithContext 使用本结构；测试注入的 fake LLMJudge 未实现该可选方法，
// 走原有 Judge 路径，保持向后兼容。
type LeadJudgeContext struct {
	JudgementID      string
	Rule             *model.LeadMiningRule
	Messages         []DetectionMessage
	Hits             []model.LeadMiningHit
	Conversation     *model.ConversationContext
	ChannelAccountID uint
	AccountID        string
	Platform         string
	IsGroup          bool
	GroupID          string
}

// JudgeWithContext 二次 LLM 汇总判断：多轮 + 历史消息 + 三路证据 + 角色分类。
// 与 Judge 共用 llm.GetGlobalDispatcher() 单一路径，不新建 LLM 客户端。
func (j *dispatcherJudge) JudgeWithContext(ctx context.Context, jctx *LeadJudgeContext) (*LeadJudgement, error) {
	if jctx == nil {
		return nil, errors.New("二次判断上下文为空")
	}
	dispatcher := llm.GetGlobalDispatcher()
	if dispatcher == nil {
		return nil, fmt.Errorf("llm dispatcher 未初始化")
	}
	messages := make([]llm.ChatMessage, 0, len(jctx.Messages)+1)
	for _, m := range jctx.Messages {
		role := "user"
		if m.Role == "assistant" || m.Role == "self" {
			role = "assistant"
		}
		messages = append(messages, llm.ChatMessage{Role: role, Content: m.Content})
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioLowCost,
		SystemPrompt: buildLeadJudgeSystemPrompt(jctx),
		Messages:     messages,
		MaxTokens:    1024,
		Temperature:  0.2,
		JSONMode:     true,
	}
	var jd LeadJudgement
	if _, err := dispatcher.DispatchStructured(ctx, req, &jd); err != nil {
		return nil, err
	}
	jd.LeadRole = normalizeLeadRole(jd.LeadRole)
	if strings.TrimSpace(jd.ClueType) == "" {
		jd.ClueType = inferLeadClueType(jd.LeadRole, jd.IntentScore)
	}
	return &jd, nil
}

// buildLeadJudgeSystemPrompt 把多轮上下文、角色分类、三路证据写进系统提示。
func buildLeadJudgeSystemPrompt(jctx *LeadJudgeContext) string {
	var b strings.Builder
	b.WriteString("你是企业私域的「线索发掘」助手。基于提供的多轮客户对话与三路检测证据，判断是否构成值得跟进的销售线索。\n\n")
	if jctx.Rule != nil {
		if strings.TrimSpace(jctx.Rule.SystemDescription) != "" {
			b.WriteString("【发掘规则】" + jctx.Rule.SystemDescription + "\n")
		}
		if len(jctx.Rule.Keywords) > 0 {
			b.WriteString("【关键词】" + strings.Join([]string(jctx.Rule.Keywords), "、") + "\n")
		}
		if len(jctx.Rule.SemanticKeywords) > 0 {
			b.WriteString("【语义关键词】" + strings.Join([]string(jctx.Rule.SemanticKeywords), "、") + "\n")
		}
	}
	if jctx.Conversation != nil && len(jctx.Conversation.MemberRoles) > 0 {
		roles := make([]string, 0, len(jctx.Conversation.MemberRoles))
		for sender, role := range jctx.Conversation.MemberRoles {
			roles = append(roles, fmt.Sprintf("%s=%s", sender, role))
		}
		b.WriteString("【群成员角色分类】" + strings.Join(roles, "、") + "\n")
	}
	if jctx.IsGroup {
		b.WriteString("【会话类型】群聊，群ID=" + jctx.GroupID + "\n")
	}
	if len(jctx.Hits) > 0 {
		b.WriteString("【三路检测证据】\n")
		for _, h := range jctx.Hits {
			b.WriteString(fmt.Sprintf("- 类型=%s 完整=%t 降级=%t 证据消息=%v 载荷=%v 说明=%s\n",
				h.Type, h.Complete, h.Degraded, []string(h.EvidenceMessageIDs), map[string]any(h.Payload), h.Reason))
		}
		b.WriteString("注意：complete=false 或 degraded=true 的证据表示 LLM 未初始化/超时，结论必须标明证据不完整，不得当成完整证据。\n")
	}
	b.WriteString("\n请综合全部对话，输出严格 JSON：\n")
	b.WriteString("{\n  \"is_lead\": bool,\n  \"intent_score\": 0-100 整数,\n")
	b.WriteString("  \"clue_type\": \"需求线索|供货线索|资源线索|中介线索|待分类\",\n")
	b.WriteString("  \"lead_role\": \"demand|supply|resource|broker|unknown\",\n")
	b.WriteString("  \"matched_keywords\": [命中的关键词],\n  \"matched_tags\": [建议标签],\n")
	b.WriteString("  \"summary\": \"一句话线索摘要\",\n  \"followup_suggestion\": \"跟进建议\",\n")
	b.WriteString("  \"confidence\": 0-1 置信度,\n  \"reason\": \"判定理由\"\n}\n")
	b.WriteString("只输出 JSON，不要额外解释。")
	return b.String()
}

func inferLeadClueType(role string, score int) string {
	switch role {
	case "demand":
		return "需求线索"
	case "supply":
		return "供货线索"
	case "resource":
		return "资源线索"
	case "broker":
		return "中介线索"
	default:
		if score >= 40 {
			return "待分类"
		}
		return ""
	}
}

// buildLeadJudgeContext 组装富上下文并跑三路检测（T036-T038）。
// 规则解析失败/检测失败均 fail-visible：记日志并带着已拿到的证据继续判断，
// 不静默吞错、不返回零值规则。
func (s *Service) buildLeadJudgeContext(ctx context.Context, hub *model.MessageHub, history []llm.ChatMessage, judgementID string) *LeadJudgeContext {
	jctx := &LeadJudgeContext{
		JudgementID: judgementID,
		AccountID:   hub.AccountID,
		Platform:    hub.Platform,
		IsGroup:     hub.IsGroup,
		GroupID:     hub.GroupID,
	}

	target := LeadRuleTarget{}
	if resolved, err := NewChannelResolver().ResolveByIdentifyCode(ctx, hub.AccountID); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("account", hub.AccountID).Msg("[lead-mining] 渠道账号数值ID解析失败，回退 global 规则")
	} else if resolved != nil {
		target.ChannelAccountID = resolved.AccountID
		target.ChannelID = resolved.ChannelID
	}
	jctx.ChannelAccountID = target.ChannelAccountID
	rule, err := NewLeadRuleService().Resolve(ctx, target)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("account", hub.AccountID).Msg("[lead-mining] 三粒度规则解析失败，使用配置兜底")
		rule = &model.LeadMiningRule{
			Scope: "global", Enabled: true,
			Keywords:          []string(s.cfgRepoSafeKeywords(ctx)),
			SemanticKeywords:  model.JSONStrings{},
			SystemDescription: s.configRequirement(ctx),
		}
	}
	jctx.Rule = rule

	msgs := make([]DetectionMessage, 0, len(history)+1)
	for i, h := range history {
		msgs = append(msgs, DetectionMessage{
			MessageID: fmt.Sprintf("history_%d", i),
			Content:   h.Content,
			Role:      h.Role,
		})
	}
	msgs = append(msgs, DetectionMessage{
		MessageID: hub.MsgID,
		Content:   strings.TrimSpace(hub.Content),
		Role:      "user",
		SenderID:  hub.SenderID,
	})

	// 共享上下文：写入本轮消息并读取多轮历史 + 成员角色（T033/T039）。
	ctxRepo := repository.NewConversationContextRepository()
	if conv, cerr := ctxRepo.Get(ctx, target.ChannelAccountID, hub.ConversationID); cerr == nil && conv != nil {
		conv.Messages = append(conv.Messages, model.ConversationMessage{
			MessageID: hub.MsgID, Role: "user", SenderID: hub.SenderID,
			Content: strings.TrimSpace(hub.Content),
		})
		if len(conv.Messages) > leadMiningHistorySize {
			conv.Messages = conv.Messages[len(conv.Messages)-leadMiningHistorySize:]
		}
		if uerr := ctxRepo.Upsert(ctx, conv); uerr != nil {
			logger.Ctx(ctx).Warn().Err(uerr).Msg("[lead-mining] 共享对话上下文写入失败")
		}
		jctx.Conversation = conv
	}
	if jctx.Conversation != nil && len(jctx.Conversation.Messages) > 0 {
		msgs = make([]DetectionMessage, 0, len(jctx.Conversation.Messages))
		for _, m := range jctx.Conversation.Messages {
			msgs = append(msgs, DetectionMessage{
				MessageID: m.MessageID, Content: m.Content, Role: m.Role, SenderID: m.SenderID,
			})
		}
	}
	jctx.Messages = msgs

	hits, derr := NewLeadDetectionService().Run(ctx, DetectionInput{
		Rule: rule, Messages: msgs, JudgementID: judgementID,
	})
	if derr != nil {
		logger.Ctx(ctx).Warn().Err(derr).Msg("[lead-mining] 三路检测落库失败，二次判断标记证据不完整")
	} else {
		jctx.Hits = hits
	}
	return jctx
}

// cfgRepoSafeKeywords / configRequirement 仅在规则解析失败时提供旧配置兜底，
// 不新建规则行；关键词来自既有 lead_mining_configs，不是硬编码。
// 必须走 loadConfig 命中按租户隔离的 cfgCache，不得直调 cfgRepo.GetSingleton——
// 否则每次 process 都会绕过缓存重复读配置，破坏「每租户读一次」的隔离语义。
func (s *Service) cfgRepoSafeKeywords(ctx context.Context) []string {
	if s == nil || s.cfgRepo == nil {
		return nil
	}
	cfg := s.loadConfig(ctx)
	if cfg == nil {
		return nil
	}
	return []string(cfg.Keywords)
}

func (s *Service) configRequirement(ctx context.Context) string {
	if s == nil || s.cfgRepo == nil {
		return ""
	}
	cfg := s.loadConfig(ctx)
	if cfg == nil {
		return ""
	}
	return cfg.Requirement
}

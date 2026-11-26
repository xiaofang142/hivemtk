package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ---- LEAD-04：共享业务标签/备注输入（双方实际模型请求消费） ----

const (
	// BusinessNoteFact 表示已发生事实（来自转人工/情感/线索判定的既成事件）。
	BusinessNoteFact = "fact"
	// BusinessNoteSuggestion 表示模型或坐席给出的建议，不得被当成已发生事实。
	BusinessNoteSuggestion = "suggestion"
	// sharedBusinessInputBudget 单侧共享业务信息进入模型请求的长度预算（runes）。
	sharedBusinessInputBudget = 800
)

// BusinessNote 带来源/时间/属性的一条业务备注。
// Source 取 ContextNoteNamespaceAutoReply / ContextNoteNamespaceLeadMining；
// Attribute 取 BusinessNoteFact / BusinessNoteSuggestion。
type BusinessNote struct {
	Source    string `json:"source"`
	At        int64  `json:"at,omitempty"`
	Attribute string `json:"attribute"`
	Text      string `json:"text"`
}

// SharedBusinessInput 一方从共享上下文读到、准备送入模型的业务信息。
type SharedBusinessInput struct {
	Notes   []BusinessNote `json:"notes,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Summary string         `json:"summary,omitempty"`
}

// EncodeBusinessNote 把结构备注编码为 tags 列中的 note: 条目（不改 schema）。
func EncodeBusinessNote(n BusinessNote) string {
	attr := n.Attribute
	if attr != BusinessNoteSuggestion {
		attr = BusinessNoteFact
	}
	return contextNotePrefix + n.Source + ":" + attr + "|" + strconv.FormatInt(n.At, 10) + "|" + n.Text
}

// DecodeBusinessNote 解码备注；兼容存量 note:namespace:文本（视为事实）。
func DecodeBusinessNote(tag string) (BusinessNote, bool) {
	if !strings.HasPrefix(tag, contextNotePrefix) {
		return BusinessNote{}, false
	}
	raw := strings.TrimPrefix(tag, contextNotePrefix)
	idx := strings.Index(raw, ":")
	if idx <= 0 {
		return BusinessNote{}, false
	}
	note := BusinessNote{Source: raw[:idx]}
	payload := raw[idx+1:]
	if parts := strings.SplitN(payload, "|", 3); len(parts) == 3 {
		at, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil && (parts[0] == BusinessNoteFact || parts[0] == BusinessNoteSuggestion) {
			note.Attribute, note.At, note.Text = parts[0], at, parts[2]
			return note, note.Text != ""
		}
	}
	note.Attribute, note.Text = BusinessNoteFact, payload
	return note, note.Text != ""
}

// SharedBusinessInputFromContext 从共享上下文读取对方备注 + 纯业务标签 + 摘要。
// 传自己的 namespace，跳过自己写的条目；按长度预算裁剪，避免超长备注放大模型请求。
func SharedBusinessInputFromContext(conv *model.ConversationContext, ownNamespace string, budget int) SharedBusinessInput {
	out := SharedBusinessInput{}
	if conv == nil {
		return out
	}
	out.Summary = strings.TrimSpace(conv.Summary)
	for _, tag := range conv.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if strings.HasPrefix(tag, contextNotePrefix) {
			note, ok := DecodeBusinessNote(tag)
			if ok && note.Source != ownNamespace {
				out.Notes = append(out.Notes, note)
			}
			continue
		}
		out.Tags = append(out.Tags, tag)
	}
	if budget <= 0 {
		budget = sharedBusinessInputBudget
	}
	budgetRunes := []rune(strings.Join([]string{out.Summary, strings.Join(out.Tags, "")}, ""))
	for i := range out.Notes {
		budgetRunes = append(budgetRunes, []rune(out.Notes[i].Text)...)
	}
	if len(budgetRunes) > budget {
		// 超预算时按备注倒序（最近写入优先）裁剪，再裁标签/摘要。
		kept := make([]BusinessNote, 0, len(out.Notes))
		used := len([]rune(out.Summary))
		for i := len(out.Notes) - 1; i >= 0; i-- {
			n := len([]rune(out.Notes[i].Text))
			if used+n > budget {
				continue
			}
			used += n
			kept = append([]BusinessNote{out.Notes[i]}, kept...)
		}
		out.Notes = kept
	}
	return out
}

// PromptBlock 生成可直接放入双方模型请求的业务信息块。
// 建议属性明确标注为建议，禁止被当成已发生事实。
func (in SharedBusinessInput) PromptBlock() string {
	if in.empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("【共享业务信息（另一智能体写入）】\n")
	if in.Summary != "" {
		b.WriteString("- 会话摘要：" + in.Summary + "\n")
	}
	for _, tag := range in.Tags {
		b.WriteString("- 业务标签：" + tag + "\n")
	}
	for _, note := range in.Notes {
		attr := BusinessNoteFact
		if note.Attribute == BusinessNoteSuggestion {
			attr = BusinessNoteSuggestion
		}
		label := "既成事实"
		if attr == BusinessNoteSuggestion {
			label = "建议（非已发生事实，不得当作既成事实）"
		}
		b.WriteString(fmt.Sprintf("- 备注[来源=%s 时间=%d 属性=%s]：%s\n", note.Source, note.At, label, note.Text))
	}
	b.WriteString("以上仅为另一智能体写入的信息，不替代当前消息证据。\n")
	return b.String()
}

func (in SharedBusinessInput) empty() bool {
	return len(in.Notes) == 0 && len(in.Tags) == 0 && strings.TrimSpace(in.Summary) == ""
}

// LeadJudgeContext 二次 LLM 汇总判断的富上下文（FR-013）。
//
// 相比旧的 Judge(cfg, history)，本上下文额外携带：账号/渠道两级规则、三路检测证据、
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
	// GroupRoleCounts 群成员发言角色计数（由 AggregateGroupContext 聚合得到）。
	// 仅群聊场景填充；单聊为 nil，提示中不输出群成员统计行。
	GroupRoleCounts map[string]int
	// GroupAggregateFailed 群聊聚合失败：提示里标注证据不完整，不静默降级。
	GroupAggregateFailed bool
	// LM-12：三路检测落库失败时保留已计算结果与失败原因，但不把空 Hits 当完整证据。
	DetectionFailed bool
	// Evidence 本次授权的源消息级快照（真实 MsgID/成员/时间/覆盖水位）；
	// 合成 history_N 不属于快照，不得作为结论引用。
	Evidence *LeadEvidenceSnapshot
	// ReactEvidence D18 ReAct 工具取证收集到的证据（只读取证，失败带降级标记）。
	ReactEvidence []string
	ReactDegraded bool
	// SharedBusiness LEAD-04：自动回复侧写入的标签/备注/摘要，进入判定模型请求。
	SharedBusiness SharedBusinessInput
	// AgentSnapshot FR-026：本次冻结的线索发现智能体配置（与 gate/判定同版本）。
	// 智能体的 system_prompt / persona 据此进入判定提示，model 用于计量归因。
	AgentSnapshot *LeadMiningConfigSnapshot
}

// leadReactHook 由装配点注入 ReAct 取证循环；未注入时判定走既有主链（向后兼容）。
// 签名与 runLeadReactLoop 对齐，便于单测注入假实现。
// 并发安全：Set 与判定读写经 RWMutex 保护（多次 -race 审计的无锁全局变量缺陷）。
var (
	leadReactHookMu sync.RWMutex
	leadReactHook   func(context.Context, *LeadJudgeContext) ([]string, bool)
)

func getLeadReactHook() func(context.Context, *LeadJudgeContext) ([]string, bool) {
	leadReactHookMu.RLock()
	defer leadReactHookMu.RUnlock()
	return leadReactHook
}

// mergeJudgeMessages 合并(MessageHub 权威历史, 共享上下文补充)为判定消息列表。
//
// 关键契约（P0-b 假聚合修复）：MessageHub 的 history 来自真实双向/多成员消息，是权威源；
// 共享上下文只按真实 MessageID 补充缺失条目，绝不覆盖 history。旧实现用只含当前一条
// 的共享缓存整体替换 history，导致群聊多成员与单聊双向历史丢失（对抗审核 P0-2）。
// 伪造的 history_N 类 ID 不作为权威键——只有真实 hub.MsgID 才参与去重。
func mergeJudgeMessages(history []DetectionMessage, shared model.ConversationMessages, current DetectionMessage) []DetectionMessage {
	out := make([]DetectionMessage, 0, len(history)+len(shared)+1)
	seen := make(map[string]bool, len(history)+len(shared)+1)
	add := func(m DetectionMessage) {
		key := strings.TrimSpace(m.MessageID)
		if key == "" || strings.HasPrefix(key, "history_") {
			// 空 ID / 合成 history_N 不作为去重权威键，仍保留消息内容
			key = ""
		}
		if key != "" {
			if seen[key] {
				return
			}
			seen[key] = true
		}
		out = append(out, m)
	}
	for _, h := range history {
		add(h)
	}
	for _, m := range shared {
		if strings.TrimSpace(m.MessageID) == "" || strings.HasPrefix(m.MessageID, "history_") {
			continue // 共享侧的合成 ID 只做补充内容，不参与权威去重
		}
		add(DetectionMessage{MessageID: m.MessageID, Content: m.Content, Role: m.Role, SenderID: m.SenderID})
	}
	add(current)
	return out
}

// roleFromEnrichedContent 从 AggregateGroupContext 产出的 "[role:sender] content" 提取 role。
func roleFromEnrichedContent(content string) string {
	if !strings.HasPrefix(content, "[") {
		return ""
	}
	end := strings.Index(content, ":")
	if end <= 1 {
		return ""
	}
	role := content[1:end]
	switch role {
	case "demand", "supply", "resource", "broker", "member", "unknown":
		return role
	}
	return ""
}

// stripRolePrefix 去掉已有的 "[role:sender] " 前缀，避免重复标注叠加。
func stripRolePrefix(content string) string {
	if !strings.HasPrefix(content, "[") {
		return content
	}
	if close := strings.Index(content, "] "); close > 0 {
		return content[close+2:]
	}
	return content
}

// JudgeWithContext 二次 LLM 汇总判断：多轮 + 历史消息 + 三路证据 + 角色分类。
// 与 Judge 共用 llm.GetGlobalDispatcher() 单一路径，不新建 LLM 客户端。
func (j *dispatcherJudge) JudgeWithContext(ctx context.Context, jctx *LeadJudgeContext) (*LeadJudgement, error) {
	if jctx == nil {
		return nil, errors.New("二次判断上下文为空")
	}
	// D18：装配了 ReAct 取证钩子时先跑工具取证（只读、有步数上限、失败可见），
	// 证据进入系统提示；钩子未装配时行为与既有主链完全一致（向后兼容）。
	if hook := getLeadReactHook(); hook != nil {
		ev, degraded := hook(ctx, jctx)
		jctx.ReactEvidence = ev
		jctx.ReactDegraded = degraded
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
	// FR-026：账号级线索发现智能体的人设/系统提示必须真正进入判定模型请求。
	// 没有绑定智能体时不输出该段，保持存量租户行为不变。
	if jctx.AgentSnapshot != nil {
		if strings.TrimSpace(jctx.AgentSnapshot.LeadAgentPersona) != "" {
			b.WriteString("【线索发现智能体人设】" + strings.TrimSpace(jctx.AgentSnapshot.LeadAgentPersona) + "\n")
		}
		if strings.TrimSpace(jctx.AgentSnapshot.LeadAgentPrompt) != "" {
			b.WriteString("【线索发现智能体系统提示】" + strings.TrimSpace(jctx.AgentSnapshot.LeadAgentPrompt) + "\n")
		}
	}
	// P0-d / D8：历史摘要（ConversationContext.Summary）是 compaction 产物，
	// 判定前必须读入提示，让模型在长会话里也能看到被裁剪掉的早期诉求与结论。
	if jctx.Conversation != nil && strings.TrimSpace(jctx.Conversation.Summary) != "" {
		b.WriteString("【会话历史摘要】" + strings.TrimSpace(jctx.Conversation.Summary) + "\n")
		b.WriteString("以上为早期对话压缩摘要；与下方逐条消息冲突时以逐条消息为准。\n")
	}
	// LEAD-04：对方刚写的业务备注/标签/摘要进入真实判定模型请求（不是日志）。
	if block := jctx.SharedBusiness.PromptBlock(); block != "" {
		b.WriteString(block)
	}
	if jctx.Conversation != nil && len(jctx.Conversation.MemberRoles) > 0 {
		roles := make([]string, 0, len(jctx.Conversation.MemberRoles))
		for sender, role := range jctx.Conversation.MemberRoles {
			roles = append(roles, fmt.Sprintf("%s=%s", sender, role))
		}
		b.WriteString("【群成员角色分类】" + strings.Join(roles, "、") + "\n")
	}
	if len(jctx.ReactEvidence) > 0 {
		b.WriteString("【ReAct 取证证据】\n")
		for _, ev := range jctx.ReactEvidence {
			b.WriteString("- " + ev + "\n")
		}
	}
	if jctx.ReactDegraded {
		b.WriteString("【证据不完整】ReAct 取证失败或降级，结论需人工复核。\n")
	}
	if jctx.IsGroup {
		b.WriteString("【会话类型】群聊，群ID=" + jctx.GroupID + "\n")
		if len(jctx.GroupRoleCounts) > 0 {
			roles := make([]string, 0, len(jctx.GroupRoleCounts))
			for role, count := range jctx.GroupRoleCounts {
				roles = append(roles, fmt.Sprintf("%s=%d", role, count))
			}
			b.WriteString("【群成员发言统计】" + strings.Join(roles, "、") + "\n")
			b.WriteString("群聊消息已按 [角色:发送者] 标注，请区分求购方/供货方/资源方/撮合方的发言。\n")
		}
		if jctx.GroupAggregateFailed {
			b.WriteString("【证据不完整】群聊上下文聚合失败，角色标注不可用，不得据此确定成员角色。\n")
		}
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

	// 共享上下文/会话摘要必须写入可信作用域解析出的真实渠道账号。
	// ruleTargetFor 只是规则解析的可注入目标（测试替身），不是真实账号来源；
	// 把它当真实账号会让摘要写到错误账号下（T01 回归）。process 已在 ctx 冻结
	// historyScope，此处复用缓存不会重复查库；无真实账号时保持 0 不写共享上下文。
	var channelAccountID uint
	if scope, scopeErr := s.historyScope(ctx, hub); scopeErr == nil && scope != nil {
		channelAccountID = scope.accountID
	}
	jctx.ChannelAccountID = channelAccountID
	// 规则只读账号/渠道两级；解析失败不再合成 global 规则、也不拿 legacy 配置兜底。
	// jctx.Rule 留空 ⇒ 三路检测器按「规则为空」标记证据不完整（fail-visible）。
	rule, err := s.resolveProcessRule(ctx, hub)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("account", hub.AccountID).Msg("[lead-mining] 账号/渠道规则解析失败，不启用全局兜底")
	}
	jctx.Rule = rule

	// P0-b：history（MessageHub 真实双向/多成员数据）是权威上下文。
	// 注意 llm.ChatMessage 无 ID 字段，历史 ID 仍为合成键；mergeJudgeMessages
	// 会跳过 history_N 作为去重键，保证内容不被共享缓存覆盖。真实 MsgID 的
	// 证据引用改造需扩 fetchHistory 返回结构，列为后续独立任务。
	histMsgs := make([]DetectionMessage, 0, len(history))
	for i, h := range history {
		histMsgs = append(histMsgs, DetectionMessage{
			MessageID: fmt.Sprintf("history_%d", i), Content: h.Content, Role: h.Role,
		})
	}
	current := DetectionMessage{
		MessageID: hub.MsgID, Content: strings.TrimSpace(hub.Content),
		Role: "user", SenderID: hub.SenderID,
	}

	// 共享上下文：写入本轮消息并读取多轮历史 + 成员角色（T033/T039）。
	ctxRepo := repository.NewConversationContextRepository()
	conversationID := leadHistoryConversationID(hub)
	if conv, cerr := leadScopedConversation(ctx, ctxRepo, channelAccountID, conversationID); cerr == nil && conv != nil {
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
		// LEAD-04：把自动回复侧写入的备注/标签/摘要送进线索判定输入，
		// 与回复侧 SetSharedBusinessProvider 消费对方信息形成双向交叉引用。
		jctx.SharedBusiness = SharedBusinessInputFromContext(conv, ContextNoteNamespaceLeadMining, sharedBusinessInputBudget)
	}
	var shared model.ConversationMessages
	if jctx.Conversation != nil {
		shared = jctx.Conversation.Messages
	}
	// P0-b 核心：history 权威 + 共享上下文按真实 MsgID 补充，禁止整体覆盖。
	jctx.Messages = mergeJudgeMessages(histMsgs, shared, current)

	// 群聊富化：用 AggregateGroupContext 给每条历史消息补 [角色:发送者] 前缀，
	// 并把成员角色计数写进判定提示，让「谁求购/谁供货」在证据里可辨。
	// 失败时保留原始消息并标注证据不完整（fail-visible，不静默吞错）。
	if hub.IsGroup && channelAccountID > 0 && conversationID != "" {
		enriched, counts, aerr := NewConversationContextServiceWithRepo(ctxRepo).
			AggregateGroupContext(ctx, channelAccountID, hub.ConversationID, ConversationContextScope{Platform: hub.Platform, IsGroup: hub.IsGroup, GroupID: hub.GroupID})
		if aerr != nil {
			jctx.GroupAggregateFailed = true
			logger.Ctx(ctx).Warn().Err(aerr).Msg("[lead-mining] 群聊上下文聚合失败，判定提示标注证据不完整")
		} else if len(enriched) > 0 {
			// P0-b：只把角色标注应用到已合并的权威消息，不替换 jctx.Messages。
			// 用 SenderID 而非 MessageID 匹配：历史消息的 ID 是合成 history_N，
			// 与共享缓存的真实 MsgID 不同；按发送人匹配才能给真实多成员历史标注角色。
			roleBySender := make(map[string]string, len(enriched))
			for _, em := range enriched {
				if r := roleFromEnrichedContent(em.Content); r != "" && em.SenderID != "" {
					roleBySender[em.SenderID] = r
				}
			}
			for i := range jctx.Messages {
				sender := jctx.Messages[i].SenderID
				if sender == "" {
					continue
				}
				if role, ok := roleBySender[sender]; ok {
					jctx.Messages[i].Content = fmt.Sprintf("[%s:%s] %s", role, sender, stripRolePrefix(jctx.Messages[i].Content))
				}
			}
			jctx.GroupRoleCounts = counts
		}
	}

	// 用富化后的 jctx.Messages 跑三路检测，保证关键词/语义/LLM 检测器
	// 与二次判定看到同一份带 [角色:发送者] 的群聊上下文。
	detection := DetectionInput{Rule: rule, Messages: jctx.Messages, JudgementID: judgementID}
	var hits []model.LeadMiningHit
	var derr error
	if s.hitsProvider != nil {
		hits, derr = s.hitsProvider(ctx, detection)
	} else {
		hits, derr = NewLeadDetectionService().Run(ctx, detection)
	}
	if derr != nil {
		// LM-12：失败可见——结果不丢，但证据完整性关闭，禁止按完整证据自动放行。
		jctx.DetectionFailed = true
		logger.Ctx(ctx).Warn().Err(derr).Msg("[lead-mining] 三路检测落库失败，保留计算结果并标记证据不完整")
	} else {
		jctx.Hits = hits
	}
	return jctx
}

// EvidenceComplete 判定快照的必需证据是否齐全：三路命中完整、群聚合成功且 ReAct 未降级。
// 与 recordLeadMiningJudgement 的 EvidenceComplete 口径一致，供副作用准入闸复用。
func (c *LeadJudgeContext) EvidenceComplete() bool {
	if c == nil || len(c.Hits) == 0 || c.GroupAggregateFailed || c.ReactDegraded || c.DetectionFailed {
		return false
	}
	// 必需证据 = 至少一路完整命中；LLM/语义等可选路降级不构成挡单。
	// 与判定记录的 EvidenceComplete 口径一致：任一路上报 Complete=false 会在记录里留痕，
	// 但只有「全部证据路都降级」才让副作用闸判定证据不足。
	for _, h := range c.Hits {
		if h.Complete && !h.Degraded {
			return true
		}
	}
	return false
}

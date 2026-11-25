package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// AIEvidence 一条建议的事实依据（必须指向真实字段/记录，禁止凭空生成）。
//
// CRM-13：Time/Source 让结论可回查（什么时候、从哪张表/哪条消息来）；
// Stale 表示证据已被上游标记失效（越权/已删除/超出快照），不得当有效证据用。
type AIEvidence struct {
	Type       string `json:"type"`                  // customer_field / tag / rfm / order / clue / conversation / followup
	Ref        string `json:"ref"`                   // 字段名或记录 ID
	Snippet    string `json:"snippet"`               // 原始值片段（展示前仍需按渠道脱敏）
	Source     string `json:"source,omitempty"`      // 来源表名/渠道/文档，回查用
	OccurredAt string `json:"occurred_at,omitempty"` // 事实发生/更新时间（RFC3339）
	Stale      bool   `json:"stale,omitempty"`       // 失效证据，不得支撑结论
}

// AISuggestion 一条 AI 建议。每条都必须带 evidence；无有效证据时输出 degraded 记录。
type AISuggestion struct {
	Kind       string       `json:"kind"` // profile / segment / opportunity / followup / degraded
	Title      string       `json:"title"`
	Content    string       `json:"content"`
	Confidence float64      `json:"confidence"` // 模型自报把握（只读，不等于准确率）
	Evidence   []AIEvidence `json:"evidence,omitempty"`
	// ConfidenceSource 区分 model_estimate（模型自报）与 rule_score（证据覆盖度等可解释规则分）。
	ConfidenceSource string `json:"confidence_source,omitempty"`
	// RuleScore 是证据覆盖度等规则分数；0 表示没有可解释规则分数（不是 0% 准确率）。
	RuleScore float64 `json:"rule_score,omitempty"`
	// PromptVersion/Model 固定本次结论的提示词与模型版本，供 26 报告与 14 草稿引用。
	PromptVersion string `json:"prompt_version,omitempty"`
	Model         string `json:"model,omitempty"`
	// DegradeReason 非空说明为何不能作为已证实结论（无引用/伪造/越权/失效/模型失败）。
	DegradeReason string `json:"degrade_reason,omitempty"`
}

// CustomerAIResult 客户 AI 画像结果。
type CustomerAIResult struct {
	CustomerID  string         `json:"customer_id"`
	Suggestions []AISuggestion `json:"suggestions"`
	Provider    string         `json:"provider,omitempty"`
	Model       string         `json:"model,omitempty"`
	Degraded    bool           `json:"degraded,omitempty"`
	// EvidenceSnapshotVersion 本结果的证据快照版本（时间+来源摘要），供 14 草稿确认时比对。
	EvidenceSnapshotVersion string `json:"evidence_snapshot_version,omitempty"`
	PromptVersion           string `json:"prompt_version,omitempty"`
}

const customerAIPromptVersion = "crm-ai-evidence-v1"

// ErrAIDispatcherUnavailable LLM 调度器未初始化（fail-visible，不静默造结论）。
var ErrAIDispatcherUnavailable = errors.New("LLM dispatcher 未初始化")

// CustomerAIService 客户 AI 画像/分层/商机/跟进建议（FR-007）。
//
// 与 dispatcherJudge（service/lead_mining.go:772）共用同一套 LLM 调用能力：
// llm.GetGlobalDispatcher() + DispatchStructured + ScenarioLowCost。
// 不另起 provider 客户端，也不把密钥写进本模块（密钥仍只走 env/KV）。
type CustomerAIService struct {
	permission *CustomerPermissionService
	customers  repository.CRMCustomerRepository
	orderRepo  repository.OrderRepository
}

func NewCustomerAIService() *CustomerAIService {
	return &CustomerAIService{
		permission: NewCustomerPermissionService(),
		customers:  repository.NewCRMCustomerRepository(),
		orderRepo:  repository.NewOrderRepository(),
	}
}

type aiItem struct {
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Confidence   float64  `json:"confidence"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type aiCustomerAnalysis struct {
	Profile       string   `json:"profile"`
	Segment       string   `json:"segment"`
	Opportunities []aiItem `json:"opportunities"`
	FollowUps     []aiItem `json:"followups"`
}

// Analyze 生成建议。每条建议把模型给出的 evidence_refs 映射回本地事实证据；
// 模型引用了不存在的证据时，该条建议被丢弃（禁止无证据建议）。
func (s *CustomerAIService) Analyze(ctx context.Context, scope CustomerDataScope, customerID string) (*CustomerAIResult, error) {
	cust, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	if err != nil {
		return nil, err
	}
	evidence := buildCustomerEvidence(cust)
	if s.orderRepo != nil && cust.UnifiedID != "" {
		if n := s.orderRepo.CountByUnifiedID(ctx, cust.UnifiedID); n > 0 {
			evidence["order_count"] = AIEvidence{
				Type: "order", Ref: cust.UnifiedID,
				Snippet: fmt.Sprintf("历史订单数 %d", n),
			}
		}
	}

	result := &CustomerAIResult{CustomerID: cust.ID, Suggestions: []AISuggestion{},
		PromptVersion: customerAIPromptVersion}
	d := llm.GetGlobalDispatcher()
	if d == nil {
		result.Degraded = true
		result.Suggestions = append(result.Suggestions, aiDegradedSuggestion("analysis", "dispatcher_unavailable"))
		return result, nil
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioLowCost,
		SystemPrompt: customerAISystemPrompt(),
		Messages: []llm.ChatMessage{{
			Role: "user", Content: customerAIPrompt(evidence),
		}},
		MaxTokens:   1200,
		Temperature: 0.2,
	}
	var analysis aiCustomerAnalysis
	res, err := d.DispatchStructured(ctx, req, &analysis)
	if err != nil {
		result.Degraded = true
		result.Suggestions = append(result.Suggestions, aiDegradedSuggestion("analysis", "model_failed"))
		return result, nil // 模型失败显式降级，不伪造结论
	}
	if res != nil {
		result.Provider = res.Provider
		result.Model = res.Model
	}
	for _, sug := range buildCustomerAISuggestions(analysis, evidence, result.Model) {
		if strings.HasSuffix(sug.Kind, "_degraded") {
			result.Degraded = true
		}
		result.Suggestions = append(result.Suggestions, sug)
	}
	return result, nil
}

// buildCustomerAISuggestions 把模型输出逐条转成"有据结论或显式降级"，不回退全部 facts。
func buildCustomerAISuggestions(analysis aiCustomerAnalysis, evidence map[string]AIEvidence, model string) []AISuggestion {
	out := make([]AISuggestion, 0, 2+len(analysis.Opportunities)+len(analysis.FollowUps))
	add := func(kind, content string, item *aiItem) {
		refs := []string{}
		score := 0.0
		if item != nil {
			refs = item.EvidenceRefs
			score = item.Confidence
		}
		if strings.TrimSpace(content) == "" {
			return
		}
		out = append(out, resolveAISuggestions(kind, content, score, refs, nil, evidence, customerAIPromptVersion, model)...)
	}
	add("profile", analysis.Profile, nil)
	add("segment", analysis.Segment, nil)
	for i := range analysis.Opportunities {
		item := &analysis.Opportunities[i]
		add("opportunity", item.Content, item)
	}
	for i := range analysis.FollowUps {
		item := &analysis.FollowUps[i]
		add("followup", item.Content, item)
	}
	return out
}

func buildCustomerEvidence(c *model.Customer) map[string]AIEvidence {
	at := c.UpdatedAt
	if at.IsZero() {
		at = c.CreatedAt
	}
	occurredAt := at.Format(time.RFC3339)
	field := func(ref, snippet string) AIEvidence {
		return AIEvidence{Type: "customer_field", Ref: ref, Snippet: snippet, Source: "customers", OccurredAt: occurredAt}
	}
	ev := map[string]AIEvidence{
		"name":     field("name", c.Name),
		"source":   field("source_channel", c.SourceChannel),
		"account":  field("source_account_id", c.SourceAccountID),
		"industry": field("industry", c.Industry),
		"tags":     {Type: "tag", Ref: "tags", Snippet: c.Tags, Source: "customers.tags", OccurredAt: occurredAt},
		"rfm":      {Type: "rfm", Ref: "rfm_score", Snippet: fmt.Sprintf("%d", c.RFMScore), Source: "customers.rfm_score", OccurredAt: occurredAt},
		"churn":    field("churn_risk", c.ChurnRisk),
	}
	if c.CompanyID != nil {
		ev["company"] = field("company_id", fmt.Sprintf("%d", *c.CompanyID))
	}
	return ev
}

// resolveEvidence 只返回 refs 明列且在授权快照内有效的证据。
// 关键：refs 为空时返回空（不再回退全部 facts），失效/越权引用也不得混入。
func resolveEvidence(all map[string]AIEvidence, refs []string) []AIEvidence {
	out := make([]AIEvidence, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		ev, ok := all[ref]
		if !ok || ev.Stale || ev.Snippet == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ev)
	}
	return out
}

// resolveAISuggestions 把模型输出转成"有据结论或显式降级"，供画像结果/报告/草稿共用。
// modelScore 只作模型自报；ruleScore 由有效引用占比计算，绝不伪装成实测准确率。
func resolveAISuggestions(kind, content string, modelScore float64, refs []string, usedRefs []string,
	all map[string]AIEvidence, promptVersion, model string) []AISuggestion {
	evidence := resolveEvidence(all, refs)
	if len(evidence) == 0 || strings.TrimSpace(content) == "" {
		reason := "no_valid_evidence_ref"
		if len(refs) == 0 {
			reason = "missing_evidence_ref"
		} else if strings.TrimSpace(content) == "" {
			reason = "empty_content"
		}
		return []AISuggestion{{
			Kind: kind + "_degraded", Title: "结论降级", Content: "证据不足或引用无效，未生成已证实结论",
			Evidence: nil, Confidence: 0, ConfidenceSource: "none", RuleScore: 0,
			PromptVersion: promptVersion, Model: model, DegradeReason: reason,
		}}
	}
	if kind == "degraded" {
		kind = "suggestion"
	}
	valid := 0
	for _, ref := range refs {
		if ev, ok := all[ref]; ok && !ev.Stale && ev.Snippet != "" {
			valid++
		}
	}
	ruleScore := float64(valid) / float64(len(refs))
	if len(usedRefs) > 0 {
		// 授权检索阶段已标出不可引用项：按实际可用引用数重算覆盖度。
		ruleScore = float64(len(evidence)) / float64(len(usedRefs))
	}
	return []AISuggestion{{
		Kind: kind, Content: content, Confidence: modelScore, Evidence: evidence,
		ConfidenceSource: "model_estimate", RuleScore: ruleScore,
		PromptVersion: promptVersion, Model: model,
	}}
}

// aiDegradedSuggestion 构造显式降级结论（模型/检索失败时用，不伪造结论）。
func aiDegradedSuggestion(kind, reason string) AISuggestion {
	return AISuggestion{
		Kind: "degraded", Title: "结论降级", Content: "证据不足或模型不可用，未生成已证实结论",
		Confidence: 0, ConfidenceSource: "none", DegradeReason: reason,
	}
}

func customerAISystemPrompt() string {
	var b strings.Builder
	b.WriteString("你是跨境电商 CRM 的客户分析助手。基于提供的客户事实字段，输出严格 JSON：\n")
	b.WriteString("{\n  \"profile\": \"客户画像摘要\",\n  \"segment\": \"客户分层建议\",\n")
	b.WriteString("  \"opportunities\": [{\"title\":\"\",\"content\":\"\",\"confidence\":0-1,\"evidence_refs\":[\"字段名\"]}],\n")
	b.WriteString("  \"followups\": [{\"title\":\"\",\"content\":\"\",\"confidence\":0-1,\"evidence_refs\":[\"字段名\"]}]\n}\n")
	b.WriteString("evidence_refs 只能引用输入事实中出现的字段名，不得编造字段或数值。只输出 JSON。")
	return b.String()
}

func customerAIPrompt(ev map[string]AIEvidence) string {
	raw, _ := json.Marshal(ev)
	return "客户事实：" + string(raw)
}

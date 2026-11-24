package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// AIEvidence 一条建议的事实依据（必须指向真实字段/记录，禁止凭空生成）。
type AIEvidence struct {
	Type    string `json:"type"`    // customer_field / tag / rfm / order / clue
	Ref     string `json:"ref"`     // 字段名或记录 ID
	Snippet string `json:"snippet"` // 原始值片段
}

// AISuggestion 一条 AI 建议。每条都必须带 evidence。
type AISuggestion struct {
	Kind       string       `json:"kind"` // profile / segment / opportunity / followup
	Title      string       `json:"title"`
	Content    string       `json:"content"`
	Confidence float64      `json:"confidence"`
	Evidence   []AIEvidence `json:"evidence"`
}

// CustomerAIResult 客户 AI 画像结果。
type CustomerAIResult struct {
	CustomerID  string         `json:"customer_id"`
	Suggestions []AISuggestion `json:"suggestions"`
	Provider    string         `json:"provider,omitempty"`
	Degraded    bool           `json:"degraded,omitempty"`
}

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

	d := llm.GetGlobalDispatcher()
	if d == nil {
		return nil, ErrAIDispatcherUnavailable
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
		return nil, fmt.Errorf("客户 AI 分析失败: %w", err)
	}

	result := &CustomerAIResult{CustomerID: cust.ID, Suggestions: []AISuggestion{}}
	if refs := resolveEvidence(evidence, nil); strings.TrimSpace(analysis.Profile) != "" {
		result.Suggestions = append(result.Suggestions, AISuggestion{
			Kind: "profile", Title: "客户画像", Content: analysis.Profile,
			Confidence: 0.8, Evidence: refs,
		})
	}
	if strings.TrimSpace(analysis.Segment) != "" {
		result.Suggestions = append(result.Suggestions, AISuggestion{
			Kind: "segment", Title: "客户分层", Content: analysis.Segment,
			Confidence: 0.8, Evidence: resolveEvidence(evidence, nil),
		})
	}
	for _, item := range analysis.Opportunities {
		ev := resolveEvidence(evidence, item.EvidenceRefs)
		if len(ev) == 0 || strings.TrimSpace(item.Content) == "" {
			continue // 无证据不输出
		}
		result.Suggestions = append(result.Suggestions, AISuggestion{
			Kind: "opportunity", Title: firstNonEmptyCRMValue(item.Title, "商机建议"),
			Content: item.Content, Confidence: item.Confidence, Evidence: ev,
		})
	}
	for _, item := range analysis.FollowUps {
		ev := resolveEvidence(evidence, item.EvidenceRefs)
		if len(ev) == 0 || strings.TrimSpace(item.Content) == "" {
			continue
		}
		result.Suggestions = append(result.Suggestions, AISuggestion{
			Kind: "followup", Title: firstNonEmptyCRMValue(item.Title, "跟进建议"),
			Content: item.Content, Confidence: item.Confidence, Evidence: ev,
		})
	}
	if res != nil {
		result.Provider = res.Provider
	}
	return result, nil
}

func buildCustomerEvidence(c *model.Customer) map[string]AIEvidence {
	ev := map[string]AIEvidence{
		"name":     {Type: "customer_field", Ref: "name", Snippet: c.Name},
		"source":   {Type: "customer_field", Ref: "source_channel", Snippet: c.SourceChannel},
		"account":  {Type: "customer_field", Ref: "source_account_id", Snippet: c.SourceAccountID},
		"industry": {Type: "customer_field", Ref: "industry", Snippet: c.Industry},
		"tags":     {Type: "tag", Ref: "tags", Snippet: c.Tags},
		"rfm":      {Type: "rfm", Ref: "rfm_score", Snippet: fmt.Sprintf("%d", c.RFMScore)},
		"churn":    {Type: "customer_field", Ref: "churn_risk", Snippet: c.ChurnRisk},
	}
	if c.CompanyID != nil {
		ev["company"] = AIEvidence{Type: "customer_field", Ref: "company_id", Snippet: fmt.Sprintf("%d", *c.CompanyID)}
	}
	return ev
}

func resolveEvidence(all map[string]AIEvidence, refs []string) []AIEvidence {
	if len(refs) == 0 {
		// 画像/分层允许使用全部基础事实作为依据。
		out := make([]AIEvidence, 0, len(all))
		for k, v := range all {
			if v.Snippet != "" {
				out = append(out, v)
			}
			_ = k
		}
		return out
	}
	out := make([]AIEvidence, 0, len(refs))
	for _, ref := range refs {
		if ev, ok := all[ref]; ok && ev.Snippet != "" {
			out = append(out, ev)
		}
	}
	return out
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

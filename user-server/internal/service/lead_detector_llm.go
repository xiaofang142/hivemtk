package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
)

const leadLLMDetectDefaultTimeout = 8 * time.Second

// leadLLMDetectResult LLM 单路检测结果（结构化输出）。
type leadLLMDetectResult struct {
	IsLead             bool     `json:"is_lead"`
	Score              int      `json:"score"`
	Role               string   `json:"role"` // demand/supply/resource/broker/unknown
	Reasons            []string `json:"reasons"`
	EvidenceMessageIDs []string `json:"evidence_message_ids"`
}

// LeadLLMDetector LLM 检测（FR-012）。
//
// 与 Phase 3 customer_ai.go、既有 dispatcherJudge 走同一个
// llm.GetGlobalDispatcher() + DispatchStructured 路径，不新建 LLM 客户端。
// 异步由 LeadDetectionService 的 errgroup 编排；本检测器自身用 context.WithTimeout
// 限时，超时/未初始化一律返回 Complete=false + Degraded=true 的降级证据并写明原因，
// 绝不静默出结论（宪法 III）。
type LeadLLMDetector struct{}

func NewLeadLLMDetector() *LeadLLMDetector { return &LeadLLMDetector{} }

func (d *LeadLLMDetector) Detect(ctx context.Context, in DetectionInput) DetectionHit {
	timeout := leadLLMDetectTimeout()
	detectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	allIDs := make(model.JSONStrings, 0, len(in.Messages))
	for _, m := range in.Messages {
		if m.MessageID != "" {
			allIDs = append(allIDs, m.MessageID)
		}
	}
	hit := DetectionHit{
		Type:     "llm",
		Complete: true,
		Payload:  model.JSONPayload{"timeout_ms": timeout.Milliseconds()},
	}

	dispatcher := llm.GetGlobalDispatcher()
	if dispatcher == nil {
		hit.Complete = false
		hit.Degraded = true
		hit.Reason = "LLM dispatcher 未初始化，本次证据不完整"
		hit.Payload["error"] = hit.Reason
		hit.EvidenceMessageIDs = allIDs
		return hit
	}

	messages := make([]llm.ChatMessage, 0, len(in.Messages))
	for _, m := range in.Messages {
		role := "user"
		if m.Role == "assistant" || m.Role == "self" {
			role = "assistant"
		}
		messages = append(messages, llm.ChatMessage{Role: role, Content: m.Content})
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioLowCost,
		SystemPrompt: buildLeadLLMDetectPrompt(in.Rule),
		Messages:     messages,
		MaxTokens:    512,
		Temperature:  0.2,
		JSONMode:     true,
	}
	var result leadLLMDetectResult
	if _, err := dispatcher.DispatchStructured(detectCtx, req, &result); err != nil {
		hit.Complete = false
		hit.Degraded = true
		hit.Reason = fmt.Sprintf("LLM 检测超时或失败（%s）: %v", timeout, err)
		hit.Payload["error"] = err.Error()
		// 记不完整证据：把本次attempt覆盖的消息全部标为"未得到LLM结论"，
		// 二次汇总判断据此判定证据完整度，而不是把它当普通未命中。
		hit.EvidenceMessageIDs = allIDs
		return hit
	}

	ids := model.JSONStrings(result.EvidenceMessageIDs)
	if len(ids) == 0 {
		ids = allIDs
	}
	hit.EvidenceMessageIDs = ids
	hit.Payload["is_lead"] = result.IsLead
	hit.Payload["score"] = result.Score
	hit.Payload["role"] = normalizeLeadRole(result.Role)
	hit.Payload["reasons"] = result.Reasons
	return hit
}

func buildLeadLLMDetectPrompt(rule *model.LeadMiningRule) string {
	var b strings.Builder
	b.WriteString("你是线索发掘的「单路检测」助手。请基于对话判断本轮是否构成销售线索，并给出角色分类。\n")
	if rule != nil {
		if strings.TrimSpace(rule.SystemDescription) != "" {
			b.WriteString("【规则描述】" + rule.SystemDescription + "\n")
		}
		if len(rule.Keywords) > 0 {
			b.WriteString("【关键词参考】" + strings.Join([]string(rule.Keywords), "、") + "\n")
		}
		if len(rule.SemanticKeywords) > 0 {
			b.WriteString("【语义关键词参考】" + strings.Join([]string(rule.SemanticKeywords), "、") + "\n")
		}
	}
	b.WriteString("输出严格 JSON：{\"is_lead\":bool,\"score\":0-100整数,\"role\":\"demand|supply|resource|broker|unknown\",\"reasons\":[理由],\"evidence_message_ids\":[命中的消息ID]}\n")
	b.WriteString("只输出 JSON，不要额外解释。")
	return b.String()
}

// leadLLMDetectTimeout 超时可配置（env 仅作配置入口，不含密钥）。
func leadLLMDetectTimeout() time.Duration {
	ms := leadLLMDetectDefaultTimeout.Milliseconds()
	if v := os.Getenv("LEAD_LLM_DETECT_TIMEOUT_MS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			ms = n
		}
	}
	if ms < 500 {
		ms = 500
	}
	if ms > 30000 {
		ms = 30000
	}
	return time.Duration(ms) * time.Millisecond
}

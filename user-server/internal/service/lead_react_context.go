package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
)

const (
	leadReactTotalObservationRunes = 4000
	leadReactMaxObservations       = 5
)

// leadReactDispatchRequest 把每轮 ReAct 调用绑定到独立的 lead_react 执行链。
// 与路由用的 DispatchScenario 分离，避免低成本路由档位让 Detect/Judge/ReAct 串链。
func leadReactDispatchRequest(task LeadReactTask, system string, messages []llm.ChatMessage, step string) llm.DispatchRequest {
	sessionRef, idempotencyKey := intentDispatchCallContext(task.ContextView, string(agentcontext.ExecutionScenarioLeadReact), step)
	if sessionRef == "" && task.ContextView == nil && task.Scope != nil {
		sessionRef = validatedLLMSessionRef(task.ContextView, task.Scope.BusLLMSessionID)
	}
	if idempotencyKey == "" && task.CurrentMessage != "" {
		idempotencyKey = llm.BuildRequestIdempotencyKey(task.CurrentMessage, string(agentcontext.ExecutionScenarioLeadReact), step)
	}
	if block := BuildIntentContextBlock(task.ContextView); block != "" {
		system = strings.TrimSpace(system) + "\n" + block
	}
	return llm.DispatchRequest{
		Scenario:              llm.ScenarioLowCost,
		SystemPrompt:          system,
		Messages:              messages,
		MaxTokens:             4096,
		Temperature:           0.2,
		ExecutionScenarioID:   string(agentcontext.ExecutionScenarioLeadReact),
		LLMSessionRef:         sessionRef,
		RequestIdempotencyKey: idempotencyKey,
	}
}

// leadReactObservationBudget 从最新观察向旧方向夹取，保证跨轮总预算有界；
// 被省略的观察只记录索引原因，不把原文带入日志。
func leadReactObservationBudget(observations []string, maxRunes int) (kept []string, omitted []string) {
	if maxRunes <= 0 {
		maxRunes = leadReactTotalObservationRunes
	}
	kept = make([]string, 0, len(observations))
	omitted = make([]string, 0)
	used := 0
	count := 0
	for i := len(observations) - 1; i >= 0; i-- {
		obs := observations[i]
		runes := len([]rune(obs))
		if count >= leadReactMaxObservations || used+runes > maxRunes {
			omitted = append(omitted, fmt.Sprintf("observation_index=%d", i))
			continue
		}
		used += runes
		count++
		kept = append([]string{obs}, kept...)
	}
	return kept, omitted
}

// leadReactToolLedger 是 ReAct 工具调用的窄台账接口，生产实现包装
// repository.AgentTranscriptRepository，测试可用 fake 验证状态机。
type leadReactToolLedger interface {
	Plan(ctx context.Context, entry *model.ToolInvocation) error
	Settle(ctx context.Context, entry *model.ToolInvocation, status, resultHash string) error
}

func leadReactToolRisk(toolName string) string {
	name := strings.ToLower(toolName)
	for _, marker := range []string{"send", "create", "write", "update", "delete", "submit", "apply"} {
		if strings.Contains(name, marker) {
			return model.ToolRiskHighWrite
		}
	}
	return model.ToolRiskRead
}

func leadReactHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte(":"))
		h.Write([]byte(part))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func buildLeadReactToolInvocation(task LeadReactTask, toolName, query, step string) model.ToolInvocation {
	risk := leadReactToolRisk(toolName)
	authorization := leadReactHash(
		strconv.FormatUint(uint64(task.TenantID), 10), task.Platform, task.SourceRef,
		strconv.FormatBool(task.ScopeValid), task.ConversationID, task.GroupID, task.SenderID,
		strconv.FormatBool(task.IsGroup), strings.Join(task.AccountIDs, ","),
	)
	requestHash := leadReactHash(toolName, query, step)
	messageID := task.CurrentMessage
	if task.ContextView != nil {
		if task.ContextView.Current.MessageID != "" {
			messageID = task.ContextView.Current.MessageID
		}
	}
	idempotency := ""
	if messageID != "" {
		idempotency = llm.BuildRequestIdempotencyKey(messageID, string(agentcontext.ExecutionScenarioLeadReact), "tool:"+toolName+":"+step)
	}
	return model.ToolInvocation{
		TenantID:           task.TenantID,
		TranscriptID:       task.TranscriptID,
		ToolName:           toolName,
		ToolIdempotencyKey: idempotency,
		RiskClass:          risk,
		Status:             model.ToolInvocationPlanned,
		AuthorizationHash:  authorization,
		RequestHash:        requestHash,
		AttemptCount:       1,
	}
}

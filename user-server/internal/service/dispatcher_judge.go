package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
)

// leadReactDispatcherJudge 是生产判定入口：先执行服务自有文本 ReAct，再把 typed
// outcome 与证据交给 structured judge。dispatcher 可注入用于本地 fake HTTP 测试。
type leadReactDispatcherJudge struct {
	dispatcher *llm.Dispatcher
}

// leadReactJudgeError 在 structured judge 失败时保留已完成的 typed ReAct outcome，
// 供确定性 fallback 持久化；对外错误文本只暴露底层原因，不泄露机器状态。
type leadReactJudgeError struct {
	err   error
	react *LeadReactOutcome
}

func (e *leadReactJudgeError) Error() string { return e.err.Error() }
func (e *leadReactJudgeError) Unwrap() error { return e.err }

func (j *leadReactDispatcherJudge) Judge(ctx context.Context, cfg *model.LeadMiningConfig, history []llm.ChatMessage) (*LeadJudgement, error) {
	return (&dispatcherJudge{}).Judge(ctx, cfg, history)
}

func (j *leadReactDispatcherJudge) JudgeWithContext(ctx context.Context, jctx *LeadJudgeContext) (*LeadJudgement, error) {
	if jctx == nil {
		return nil, errors.New("二次判断上下文为空")
	}
	var capture *LeadReactOutcome
	if hook := getLeadReactHook(); hook != nil {
		capture = &LeadReactOutcome{}
		ctx = withLeadReactOutcomeCapture(ctx, capture)
		evidence, degraded := hook(ctx, jctx)
		jctx.ReactEvidence = evidence
		jctx.ReactDegraded = degraded
	}

	dispatcher := j.dispatcher
	if dispatcher == nil {
		dispatcher = llm.GetGlobalDispatcher()
	}
	if dispatcher == nil {
		return nil, fmt.Errorf("llm dispatcher 未初始化")
	}
	messages := make([]llm.ChatMessage, 0, len(jctx.Messages)+1)
	messages = append(messages, llm.ChatMessage{Role: "system", Content: buildLeadJudgeSystemPrompt(jctx)})
	for _, message := range jctx.Messages {
		role := "user"
		if message.Role == "assistant" || message.Role == "self" {
			role = "assistant"
		}
		messages = append(messages, llm.ChatMessage{Role: role, Content: message.Content})
	}
	req := llm.DispatchRequest{
		Scenario: llm.ScenarioLowCost, SystemPrompt: buildLeadJudgeSystemPrompt(jctx),
		Messages: messages, MaxTokens: 1024, Temperature: 0.2, JSONMode: true,
	}
	// FR-026：线索发现智能体 ID 进入用量归因，平台垫资模型能看清哪个账号的
	// 哪个线索智能体消耗了 token（自带 Key 路径不读该字段，存量零影响）。
	if jctx.AgentSnapshot != nil && jctx.AgentSnapshot.LeadAgentID > 0 {
		req.AgentID = strconv.FormatUint(uint64(jctx.AgentSnapshot.LeadAgentID), 10)
	}
	var jd LeadJudgement
	if _, err := dispatcher.DispatchStructured(ctx, req, &jd); err != nil {
		if capture != nil && capture.Status != "" {
			return nil, &leadReactJudgeError{err: err, react: capture}
		}
		return nil, err
	}
	if capture != nil && capture.Status != "" {
		jd.ReactOutcome = capture
		if capture.Degraded {
			jd.Degraded = true
		}
	}
	jd.LeadRole = normalizeLeadRole(jd.LeadRole)
	if strings.TrimSpace(jd.ClueType) == "" {
		jd.ClueType = inferLeadClueType(jd.LeadRole, jd.IntentScore)
	}
	return &jd, nil
}

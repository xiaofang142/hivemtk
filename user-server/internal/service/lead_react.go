package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
)

const (
	reactToolCustomer360     = "query_customer_360"
	reactToolHistoricalLeads = "query_historical_leads"
	reactToolDictionary      = "query_dictionary" // 显式不支持：本切片无安全词库来源
)

const (
	leadReactMaxRounds = 3
	leadReactTimeout   = 8 * time.Second
)

const (
	leadReactStatusOK          = "ok"
	leadReactStatusDegraded    = "degraded"
	leadReactStatusUnavailable = "unavailable"
)

const (
	leadReactReasonInvalidTask         = "invalid_task"
	leadReactReasonInvalidScope        = "invalid_scope"
	leadReactReasonRawAccountFallback  = "raw_account_fallback"
	leadReactReasonDispatchUnavailable = "dispatch_unavailable"
	leadReactReasonDispatchFailed      = "dispatch_failed"
	leadReactReasonInvalidResponse     = "invalid_response"
	leadReactReasonInvalidArgs         = "invalid_args"
	leadReactReasonUnknownTool         = "unknown_tool"
	leadReactReasonToolUnavailable     = "tool_unavailable"
	leadReactReasonToolFailed          = "tool_failed"
	leadReactReasonRoundLimit          = "round_limit"
	leadReactReasonTimeout             = "timeout"
)

// LeadReactOutcome 是可由结构化判定与持久化消费的机器状态。只保存工具观察与
// 固定原因，不保存模型 Thought 或内部错误原文。
type LeadReactOutcome struct {
	Evidence []string `json:"evidence,omitempty"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	Degraded bool     `json:"degraded"`
}

type LeadReactTool struct {
	Name        string
	Description string
	Run         func(ctx context.Context, task LeadReactTask, query string) (string, error)
}

type LeadReactResolver struct {
	Customer360     func(ctx context.Context, task LeadReactTask, query string) (string, error)
	HistoricalLeads func(ctx context.Context, task LeadReactTask, query string) (string, error)
}

var errLeadReactToolUnavailable = errors.New("ReAct 取证工具未装配")

func defaultLeadReactTools(resolver LeadReactResolver) []LeadReactTool {
	return []LeadReactTool{
		{
			Name:        reactToolCustomer360,
			Description: "按可信发送人查询当前来源账号下的最小会话摘要；不接受账号或租户参数",
			Run: func(ctx context.Context, task LeadReactTask, query string) (string, error) {
				if resolver.Customer360 == nil {
					return "", errLeadReactToolUnavailable
				}
				return resolver.Customer360(ctx, task, query)
			},
		},
		{
			Name:        reactToolHistoricalLeads,
			Description: "按可信平台/账号/会话或群/发送人查询历史线索；query 仅作范围内文本筛选",
			Run: func(ctx context.Context, task LeadReactTask, query string) (string, error) {
				if resolver.HistoricalLeads == nil {
					return "", errLeadReactToolUnavailable
				}
				return resolver.HistoricalLeads(ctx, task, query)
			},
		},
	}
}

type leadReactDispatch func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error)

type leadReactOutcomeCaptureContextKey struct{}

func withLeadReactOutcomeCapture(ctx context.Context, capture *LeadReactOutcome) context.Context {
	return context.WithValue(ctx, leadReactOutcomeCaptureContextKey{}, capture)
}

// SetLeadReactHook 装配服务自有文本 ReAct。任务从每次调用的 context 获取；旧
// JudgeWithContext 仍可调用该 hook，但生产路径由 leadReactDispatcherJudge 读取
// typed outcome 并写入判定机器状态。
func SetLeadReactHook(resolver LeadReactResolver, dispatch leadReactDispatch) {
	leadReactHookMu.Lock()
	defer leadReactHookMu.Unlock()
	if dispatch == nil {
		leadReactHook = nil
		return
	}
	tools := defaultLeadReactTools(resolver)
	leadReactHook = func(ctx context.Context, jctx *LeadJudgeContext) ([]string, bool) {
		outcome := LeadReactOutcome{Status: leadReactStatusDegraded, Degraded: true}
		task, ok := leadReactTaskFromContext(ctx)
		if !ok || !validLeadReactTaskContext(ctx, task) {
			outcome.Status = leadReactStatusUnavailable
			outcome.Reason = leadReactReasonInvalidScope
		} else if resolver.Customer360 == nil || resolver.HistoricalLeads == nil {
			outcome.Status = leadReactStatusUnavailable
			outcome.Reason = leadReactReasonToolUnavailable
		} else {
			outcome = runLeadReactLoop(ctx, task, tools, dispatch)
		}
		if capture, ok := ctx.Value(leadReactOutcomeCaptureContextKey{}).(*LeadReactOutcome); ok && capture != nil {
			*capture = outcome
		}
		return outcome.Evidence, outcome.Degraded
	}
}

// InstallLeadReactDispatcher 使用全局 dispatcher 装配文本 ReAct。缺少 resolver
// 或 dispatcher 时仍安装 fail-visible hook，并返回错误供 Router 记录。
func InstallLeadReactDispatcher(resolver LeadReactResolver) error {
	dispatcher := llm.GetGlobalDispatcher()
	if dispatcher == nil {
		SetLeadReactHook(resolver, func(context.Context, llm.DispatchRequest) (*llm.DispatchResult, error) {
			return nil, errLeadReactToolUnavailable
		})
		return errors.New("ReAct dispatcher 未初始化")
	}
	SetLeadReactHook(resolver, newLeadReactDispatch(dispatcher))
	if resolver.Customer360 == nil || resolver.HistoricalLeads == nil {
		return errors.New("ReAct resolver 依赖不完整")
	}
	return nil
}

// newLeadReactDispatch 强制文本协议：无论 provider.NoFC 为何值，都不发送原生
// Tools，也不依赖共享 ReActAdapter。完整 result 可保留，但循环只解析 Content。
func newLeadReactDispatch(dispatcher *llm.Dispatcher) leadReactDispatch {
	return func(ctx context.Context, req llm.DispatchRequest) (*llm.DispatchResult, error) {
		req.Tools = nil
		req.ToolChoice = ""
		return dispatcher.Dispatch(ctx, req)
	}
}

var (
	leadReactActionRE      = regexp.MustCompile(`(?mi)^\s*Action:\s*([a-zA-Z0-9_.-]+)\s*$`)
	leadReactActionInputRE = regexp.MustCompile(`(?ms)^\s*Action\s*Input:\s*(.+?)\s*$`)
	leadReactFinalRE       = regexp.MustCompile(`(?ms)^\s*Final\s*Answer:\s*(.+?)\s*$`)
)

type leadReactParsedResponse struct {
	Action      string
	ActionInput string
	Final       bool
}

func parseLeadReactResponse(content string) (leadReactParsedResponse, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return leadReactParsedResponse{}, errors.New("empty response")
	}
	action := leadReactActionRE.FindStringSubmatch(content)
	final := leadReactFinalRE.FindStringSubmatch(content)
	if len(action) > 0 && len(final) > 0 {
		return leadReactParsedResponse{}, errors.New("action and final answer both present")
	}
	if len(final) > 0 {
		return leadReactParsedResponse{Final: true}, nil
	}
	if len(action) == 0 {
		return leadReactParsedResponse{}, errors.New("missing action or final answer")
	}
	input := leadReactActionInputRE.FindStringSubmatch(content)
	if len(input) == 0 {
		return leadReactParsedResponse{}, errors.New("missing action input")
	}
	return leadReactParsedResponse{Action: strings.TrimSpace(action[1]), ActionInput: strings.TrimSpace(input[1])}, nil
}

func parseLeadReactQuery(raw string) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return "", err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("trailing JSON value")
	}
	query := strings.TrimSpace(args.Query)
	if query == "" || len([]rune(query)) > 256 {
		return "", errors.New("query empty or too long")
	}
	return query, nil
}

// runLeadReactLoop 执行严格文本协议。工具观察是唯一证据；Thought/Final Answer
// 不进入证据，未知工具、非法参数、超时与超轮数均返回 typed degraded outcome。
func runLeadReactLoop(ctx context.Context, task LeadReactTask, tools []LeadReactTool, dispatch leadReactDispatch) LeadReactOutcome {
	outcome := LeadReactOutcome{Status: leadReactStatusDegraded, Degraded: true}
	if !validLeadReactTaskContext(ctx, task) {
		outcome.Status = leadReactStatusUnavailable
		outcome.Reason = leadReactReasonInvalidScope
		return outcome
	}
	if task.ConversationID == "" && task.GroupID == "" && task.SenderID == "" {
		outcome.Reason = leadReactReasonInvalidTask
		return outcome
	}
	if dispatch == nil {
		outcome.Status = leadReactStatusUnavailable
		outcome.Reason = leadReactReasonDispatchUnavailable
		return outcome
	}

	loopCtx, cancel := context.WithTimeout(ctx, leadReactTimeout)
	defer cancel()
	byName := make(map[string]LeadReactTool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	messages := []llm.ChatMessage{
		{Role: "system", Content: buildLeadReactSystemPrompt(task, tools)},
		{Role: "user", Content: "请开始只读取证；证据足够时输出 Final Answer。"},
	}
	for round := 0; round < leadReactMaxRounds; round++ {
		if err := loopCtx.Err(); err != nil {
			outcome.Reason = leadReactReasonTimeout
			return outcome
		}
		result, err := dispatch(loopCtx, llm.DispatchRequest{
			Scenario: llm.ScenarioLowCost, Messages: messages,
			MaxTokens: 512, Temperature: 0.2,
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(loopCtx.Err(), context.DeadlineExceeded) {
				outcome.Reason = leadReactReasonTimeout
			} else {
				outcome.Reason = leadReactReasonDispatchFailed
			}
			return outcome
		}
		if result == nil {
			outcome.Reason = leadReactReasonInvalidResponse
			return outcome
		}
		parsed, err := parseLeadReactResponse(result.Content)
		if err != nil {
			outcome.Reason = leadReactReasonInvalidResponse
			return outcome
		}
		if parsed.Final {
			if task.RawFallback {
				outcome.Status = leadReactStatusDegraded
				outcome.Reason = leadReactReasonRawAccountFallback
				outcome.Degraded = true
			} else {
				outcome.Status = leadReactStatusOK
				outcome.Reason = ""
				outcome.Degraded = false
			}
			return outcome
		}
		query, err := parseLeadReactQuery(parsed.ActionInput)
		if err != nil {
			outcome.Reason = leadReactReasonInvalidArgs
			return outcome
		}
		tool, ok := byName[parsed.Action]
		if !ok {
			outcome.Evidence = append(outcome.Evidence, fmt.Sprintf("[工具拒绝] %s 不在白名单", parsed.Action))
			outcome.Reason = leadReactReasonUnknownTool
			return outcome
		}
		observation, err := tool.Run(loopCtx, task, query)
		if err != nil {
			if errors.Is(err, errLeadReactToolUnavailable) {
				outcome.Status = leadReactStatusUnavailable
				outcome.Reason = leadReactReasonToolUnavailable
			} else {
				outcome.Reason = leadReactReasonToolFailed
			}
			outcome.Evidence = append(outcome.Evidence, fmt.Sprintf("[工具失败] %s", tool.Name))
			return outcome
		}
		observation = truncateLeadReactRunes(observation, leadReactTaskMaxEvidenceRunes)
		outcome.Evidence = append(outcome.Evidence, fmt.Sprintf("[%s] query=%s\n%s", tool.Name, query, observation))
		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: fmt.Sprintf("Action: %s\nAction Input: %s", tool.Name, quoteLeadReactQuery(query))},
			llm.ChatMessage{Role: "user", Content: "Observation: " + observation},
		)
	}
	outcome.Reason = leadReactReasonRoundLimit
	return outcome
}

func quoteLeadReactQuery(query string) string {
	raw, _ := json.Marshal(map[string]string{"query": query})
	return string(raw)
}

func buildLeadReactSystemPrompt(task LeadReactTask, tools []LeadReactTool) string {
	var b strings.Builder
	b.WriteString("你是企业私域线索发掘的只读取证器。严格使用文本 ReAct 协议，不输出隐藏思考。\n")
	b.WriteString("需要工具时输出：\nAction: <工具名>\nAction Input: {\"query\":\"<范围内查询文本>\"}\n")
	b.WriteString("证据足够时输出：\nFinal Answer: <简短结论>\n")
	b.WriteString("不得编造工具结果；tenant/platform/account/conversation/group/sender 由服务端锁定，参数中不得尝试覆盖。\n\n")
	b.WriteString("【可用工具】\n")
	for _, tool := range tools {
		fmt.Fprintf(&b, "- %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Fprintf(&b, "\n【可信范围】platform=%s canonical_account_id=%d channel_id=%d raw_fallback=%t conversation=%s group=%s sender=%s is_group=%t\n",
		task.Platform, task.CanonicalAccountID, task.ChannelID, task.RawFallback,
		task.ConversationID, task.GroupID, task.SenderID, task.IsGroup)
	fmt.Fprintf(&b, "【当前消息】%s\n", task.CurrentMessage)
	if task.RuleDescription != "" || len(task.Keywords) > 0 || len(task.SemanticKeywords) > 0 {
		fmt.Fprintf(&b, "【规则】%s\n关键词=%v\n语义关键词=%v\n", task.RuleDescription, task.Keywords, task.SemanticKeywords)
	}
	if len(task.Messages) > 0 {
		b.WriteString("【有界历史】\n")
		for _, message := range task.Messages {
			fmt.Fprintf(&b, "- role=%s sender=%s content=%s\n", message.Role, message.SenderID, message.Content)
		}
	}
	if len(task.Hits) > 0 {
		b.WriteString("【三路检测证据】\n")
		for _, hit := range task.Hits {
			fmt.Fprintf(&b, "- type=%s complete=%t degraded=%t reason=%s messages=%v\n", hit.Type, hit.Complete, hit.Degraded, hit.Reason, hit.EvidenceMessageIDs)
		}
	}
	return b.String()
}

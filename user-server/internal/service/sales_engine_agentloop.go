package service

import (
	"context"

	"fmt"

	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/aiagent/llm"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	textutil "hivemtk-user/internal/pkg/utils/text"

	"encoding/json"
	"sort"
	"strings"
)

// SetAgentLoopMaxIterations 注入 Agent Loop 最大迭代次数
// 由 main.go 启动时调用；≤0 时保持默认 5
func SetAgentLoopMaxIterations(n int) {
	if n <= 0 {
		return
	}
	agentLoopMaxIterations = n
}

// SetAgentLoopMaxTools 注入 Agent Loop 工具数量上限
func SetAgentLoopMaxTools(n int) {
	if n <= 0 {
		return
	}
	agentLoopMaxTools = n
}

// SetAgentLoopTimeout 注入 Agent Loop 总超时（秒）
// 由 main.go 启动时调用：service.SetAgentLoopTimeout(cfg.Inference.LLM.TimeoutSeconds)
// ≤0 时保持默认 180s
func SetAgentLoopTimeout(seconds int) {
	if seconds <= 0 {
		return
	}
	agentLoopTotalTimeout = time.Duration(seconds) * time.Second
}

func (e *SalesEngine) runAgentLoop(
	ctx context.Context,
	scenario llm.DispatchScenario,
	prompt string,
	req *SalesRequest,
	intent *dto.RecognizeResult,
	mem *model.DialogueMemory,
	customer *model.Customer,
	availableTools []AgentToolDef,
	ragChunks []RAGChunk,
) (string, *llm.DispatchResult, []model.RichCard, error) {

	targetLang := e.resolveTargetLang(ctx, req.UserMessage)

	agentIDStr := fmt.Sprintf("%d", agentIDFromCtx(req))
	allowed := agentContextToolNames(req)

	if len(allowed) > 0 && e.permissionChecker != nil {
		e.permissionChecker.SetAgentWhitelist(agentIDStr, allowed)
	}

	maxTools, maxIter := resolveAgentSettings(ctx)

	availableTools = filterToolsForScenario(scenario, availableTools)
	filteredTools := limitToolsForAgent(availableTools, maxTools, allowed)
	toolDefs := make([]llm.ToolDefinition, 0, len(filteredTools))
	for _, fn := range filteredTools {
		params := fn.Parameters
		if params == nil {
			params = map[string]any{"type": "object"}
		}
		toolDefs = append(toolDefs, llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunctionSchema{
				Name:        fn.Name,
				Description: fn.Description,
				Parameters:  params,
			},
		})
	}
	if len(toolDefs) == 0 {

		result, err := e.dispatcher.Dispatch(ctx, llm.DispatchRequest{
			Scenario:     scenario,
			Prompt:       prompt,
			SystemPrompt: e.personaWithLang(ctx, req.Config.Persona, targetLang),
			MaxTokens:    req.Config.MaxTokens,
			Temperature:  req.Config.Temperature,
			CacheKey:     llm.CacheKey(scenario, prompt),
			CacheTTL:     3600,
		})
		if err != nil {
			return "", nil, nil, err
		}
		content := strings.TrimSpace(result.Content)
		if content == "" {

			content = e.emptyReplyFallback()
		}
		return e.calibrate(ctx, content, targetLang), result, nil, nil
	}

	guard := newAgentLoopGuard(agentLoopTotalTimeout, agentLoopMaxTotalTokens, agentLoopMaxTotalCostUSD)

	messages := make([]llm.ChatMessage, 0, 4+maxIter*2)
	messages = append(messages, llm.ChatMessage{
		Role:    "system",
		Content: buildAgentSystemPrompt(e.personaWithLang(ctx, req.Config.Persona, targetLang), intent, mem, customer, ragChunks, guard),
	})
	messages = append(messages, llm.ChatMessage{
		Role:    "user",
		Content: prompt,
	})

	if e.db != nil && req.SessionID != "" {
		var hist []model.SessionMessage
		if err := e.db.Where("session_id = ?", req.SessionID).
			Order("id desc").Limit(20).Find(&hist).Error; err == nil && len(hist) > 0 {

			if hist[0].Content == req.UserMessage {
				hist = hist[1:]
			}
			if len(hist) > 0 {

				for i, j := 0, len(hist)-1; i < j; i, j = i+1, j-1 {
					hist[i], hist[j] = hist[j], hist[i]
				}
				historyMsgs := make([]llm.ChatMessage, 0, len(hist))
				for _, m := range hist {
					role := "user"
					if m.SenderType == "ai" || m.SenderType == "agent" {
						role = "assistant"
					}
					historyMsgs = append(historyMsgs, llm.ChatMessage{Role: role, Content: m.Content})
				}

				newMessages := make([]llm.ChatMessage, 0, len(messages)+len(historyMsgs))
				newMessages = append(newMessages, messages[0])
				newMessages = append(newMessages, historyMsgs...)
				newMessages = append(newMessages, messages[1:]...)
				messages = newMessages
			}
		}
	}

	agentLoopCtx, agentLoopCancel := context.WithTimeout(ctx, agentLoopTotalTimeout)
	defer agentLoopCancel()

	stopReason := stopReasonNone
	totalToolCalls := 0

	loopGuard := tooluse.NewLoopGuard(tooluse.DefaultLoopGuardConfig())
	loopTraceID := req.SessionID
	if loopTraceID == "" {
		loopTraceID = "agentloop:" + agentIDStr
	}
	defer loopGuard.FinishTrace(loopTraceID)

	writeLoopSpan := func(output any, err error) {
		sr := tooluse.StopReasonOf(err)
		innerSR := stopReason

		sp := tracing.Start(ctx, tracing.NodeAgentTurn).
			Kind("agent_turn").
			Agent(agentIDStr)
		switch {
		case innerSR != stopReasonNone && innerSR != stopReasonCompleted && innerSR != "max_iterations_exhausted":
			sp = sp.Abnormal("stop_reason=" + string(innerSR))
		case innerSR == "max_iterations_exhausted":
			sp = sp.Abnormal("stop_reason=max_iterations_exhausted")
		case sr != tooluse.StopReasonCompleted:
			sp = sp.Abnormal("stop_reason=" + string(sr))
		}
		sp.End(output, err)
	}

	defer func() {
		logger.Infof("[AgentLoop] stop_reason=%s used_tokens=%d used_cost_usd=%.4f tool_calls=%d",
			stopReason, guard.usedTokens, guard.usedCost, totalToolCalls)
	}()

	defer func() {
		agentLoopStops.WithLabel(string(stopReason)).Inc()
	}()

	var lastResult *llm.DispatchResult
	totalTokensUsed := 0
	var firstLLMError error
	curMaxTokens := req.Config.MaxTokens
	if curMaxTokens <= 0 {

		curMaxTokens = 2048
	}
	curTools := toolDefs
	lengthRetryDone := false
	var collectedCards []model.RichCard
	for iter := 1; iter <= maxIter; iter++ {

		if iter > 1 && guard != nil {
			avgCost := guard.usedCost / float64(iter-1)
			if avgCost > 0 {
				guard.ChargeEstimated(0, avgCost)
			}
		}

		if r := guard.check(); r != stopReasonNone {
			stopReason = r
			logger.Warnf("[AgentLoop] guard tripped at iter=%d reason=%s tokens=%d cost_usd=%.4f, fallback to last content",
				iter, r, guard.usedTokens, guard.usedCost)
			break
		}

		iterCtx, iterCancel := context.WithTimeout(agentLoopCtx, agentLoopMaxPerIterTimeout)
		logger.Infof("[AgentLoop] iter=%d messages=%d tools=%d prompt_len=%d max_tokens=%d used_tokens=%d", iter, len(messages), len(curTools), len(prompt), curMaxTokens, totalTokensUsed)
		logger.Infof("[AgentLoop] prompt_preview=%s", truncate(prompt, 300))
		result, err := e.dispatcher.Dispatch(iterCtx, llm.DispatchRequest{
			Scenario:     scenario,
			Prompt:       prompt,
			SystemPrompt: e.personaWithLang(ctx, req.Config.Persona, targetLang),
			MaxTokens:    curMaxTokens,
			Temperature:  req.Config.Temperature,
			Tools:        curTools,
			ToolChoice:   "auto",
			Messages:     messages,
		})
		iterCancel()
		if err != nil {

			if iterCtx.Err() == context.DeadlineExceeded && agentLoopCtx.Err() == nil {
				logger.Warnf("[AgentLoop] iter=%d per-iter timeout (budget=%s), continue with next iter", iter, agentLoopMaxPerIterTimeout)
				continue
			}

			if firstLLMError == nil {
				firstLLMError = err
			}
			stopReason = stopReasonLLMError
			logger.Warnf("[AgentLoop] iter=%d LLM dispatch failed: %v, fallback to text response", iter, err)

			break
		}
		lastResult = result

		iterTokens := 0
		if result.Usage.TotalTokens > 0 {
			iterTokens = result.Usage.TotalTokens
		} else if result.TotalTokens > 0 {
			iterTokens = result.TotalTokens
		}
		totalTokensUsed += iterTokens
		guard.charge(iterTokens, result.Cost)

		if result.FinishReason != "tool_calls" || len(result.ToolCalls) == 0 {

			if result.FinishReason == "length" && strings.TrimSpace(result.Content) == "" && !lengthRetryDone {
				lengthRetryDone = true
				curMaxTokens = curMaxTokens * 2
				logger.Warnf("[AgentLoop] iter=%d 推理模型耗尽 token(content 为空)，重试: max_tokens=%d（保留工具）", iter, curMaxTokens)
				continue
			}

			content := strings.TrimSpace(result.Content)
			if content == "" {

				if firstLLMError == nil {
					firstLLMError = fmt.Errorf("LLM returned empty final content (finish_reason=%s)", result.FinishReason)
				}
				stopReason = stopReasonEmptyFinal
				logger.Warnf("[AgentLoop] iter=%d 最终文本回复为空(finish_reason=%s)，降级处理", iter, result.FinishReason)
				break
			}
			logger.Infof("[AgentLoop] iter=%d finish_reason=%s content_len=%d tools_called=%d",
				iter, result.FinishReason, len(content), totalToolCalls)
			stopReason = stopReasonCompleted
			finalContent := e.calibrate(ctx, content, targetLang)
			writeLoopSpan(finalContent, nil)
			return finalContent, result, collectedCards, nil
		}

		assistantMsg := llm.ChatMessage{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		toolCtx := AgentToolContext{
			AgentID:    agentIDStr,
			SessionID:  req.SessionID,
			CustomerID: req.CustomerID,
			Source:     "agent",
		}

		agentCalls := make([]AgentToolCall, 0, len(result.ToolCalls))
		for _, tc := range result.ToolCalls {
			agentCalls = append(agentCalls, AgentToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		toolResults := e.toolExecutor.DispatchToolCalls(ctx, agentCalls, toolCtx)
		for _, tr := range toolResults {
			if tr.Card != nil {
				collectedCards = append(collectedCards, *tr.Card)
			}
		}
		totalToolCalls += len(toolResults)

		for i, tr := range toolResults {

			var toolName string
			if i < len(result.ToolCalls) {
				toolName = result.ToolCalls[i].Function.Name
			}
			messages = append(messages, llm.ChatMessage{
				Role:       "tool",
				ToolCallID: tr.ToolCallID,
				Name:       toolName,
				Content:    tr.Content,
			})
		}

		toolContents := make([]string, 0, len(toolResults))
		for _, tr := range toolResults {
			toolContents = append(toolContents, tr.Content)
		}
		if r := guard.ObserveState(result.Content, toolContents); r != stopReasonNone {
			stopReason = r
			logger.Warnf("[AgentLoop] state loop detected at iter=%d (identical state x%d), fallback to last content", iter, stateLoopThreshold)
			break
		}

		logger.Infof("[AgentLoop] iter=%d finish_reason=%s tool_calls=%d total_calls=%d",
			iter, result.FinishReason, len(result.ToolCalls), totalToolCalls)
	}
	if stopReason == stopReasonNone {
		stopReason = "max_iterations_exhausted"
	}
	logger.Warnf("[AgentLoop] exited: stop_reason=%s total_tool_calls=%d, has_last_result=%v, llm_error=%v",
		stopReason, totalToolCalls, lastResult != nil, firstLLMError)
	if lastResult != nil {
		content := strings.TrimSpace(lastResult.Content)
		if content == "" {

			content = e.emptyReplyFallback()
		}
		writeLoopSpan(content, firstLLMError)
		return content, lastResult, collectedCards, nil
	}
	if firstLLMError != nil {

		fallback := "抱歉，AI 服务暂时不可用，请稍后再试或联系人工客服。"
		writeLoopSpan(fallback, firstLLMError)
		return fallback, nil, nil, nil
	}
	exhaustedErr := fmt.Errorf("agent loop exhausted with no final content")
	writeLoopSpan("", exhaustedErr)
	return "", nil, nil, exhaustedErr
}

func (e *SalesEngine) emptyReplyFallback() string {
	return "抱歉，我暂时无法处理您的请求，请稍后再试。"
}

func buildAgentSystemPrompt(persona string, intent *dto.RecognizeResult, mem *model.DialogueMemory, customer *model.Customer, ragChunks []RAGChunk, guard *agentLoopGuard) string {
	var sb strings.Builder
	sb.WriteString(persona)

	if customer != nil {
		name := customerNameOf(customer)
		if name != "" {
			sb.WriteString(fmt.Sprintf("\n[客户] %s", name))
		}
	}

	sb.WriteString("\n\n# 工具使用指引\n")
	sb.WriteString("- 当用户询问商品/产品推荐、清单、对比，或提到“有哪些产品/适合新手/推荐几款/给我看看”时，必须调用 card.show 工具返回结构化卡片，禁止只在文本里描述卡片。\n")

	sb.WriteString("- 系统已为你预检索相关知识库内容（见下方【知识库参考】），优先基于其回答，避免重复调用 rag.search；仅当用户问题明显超出已给范围、需要更多资料时，才调用 rag.search 补充检索。\n")

	if len(ragChunks) > 0 {
		sb.WriteString("\n【知识库参考】:\n")

		for i, chunk := range ragChunks {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, truncate(chunk.Content, 400)))
		}
	}

	if guard != nil {
		sb.WriteString("\n\n# 系统资源预算\n")
		remainTokens := guard.RemainingTokens()
		remainCost := guard.RemainingCostUSD()
		if remainTokens >= 0 {
			sb.WriteString(fmt.Sprintf("- 剩余 token 预算: ~%d\n", remainTokens))
		}
		if remainCost >= 0 {
			sb.WriteString(fmt.Sprintf("- 剩余成本预算: $%.2f\n", remainCost))
			if remainCost < 0.10 && remainCost >= 0 {
				sb.WriteString("- ⚠️ 成本即将耗尽，请尽量用已有工具数据回答，不要再调新的 LLM\n")
			}
		}
	}

	if intent != nil && len(intent.TopKExamples) > 0 {
		sb.WriteString("\n【意图参考示例】:\n")
		for i, ex := range intent.TopKExamples {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, ex))
		}
	}

	return sb.String()
}

func agentContextToolNames(req *SalesRequest) []string {
	if req == nil || req.AgentContext == nil {
		return nil
	}
	return req.AgentContext.Tools
}

func filterToolsForScenario(scenario llm.DispatchScenario, defs []AgentToolDef) []AgentToolDef {
	cats, restricted := tooluse.ScenarioAllowedCategories(string(scenario))
	if !restricted {
		return defs
	}
	allowed := make(map[tooluse.ToolCategory]bool, len(cats))
	for _, c := range cats {
		allowed[c] = true
	}
	reg := tooluse.GetGlobalRegistry()
	out := make([]AgentToolDef, 0, len(defs))
	for _, d := range defs {
		if t, err := reg.Get(d.Name); err == nil && allowed[t.Category()] {
			out = append(out, d)
		}
	}
	return out
}

func limitToolsForAgent(tools []AgentToolDef, maxTools int, allowed []string) []AgentToolDef {
	if maxTools <= 0 {
		maxTools = 18
	}

	priority := map[string]int{

		"rag.search": 1, "card.show": 2, "knowledge.feedback": 3, "knowledge.add_doc": 4, "knowledge.list_kb": 5,

		"customer.search": 6, "customer.get": 7, "customer.create": 8, "customer.update": 9,
		"customer.merge": 10, "customer.add_tag": 11, "customer.remove_tag": 12, "customer.segment": 13,

		"order.lookup": 14, "aftersale.create": 15, "aftersale.query": 16, "logistics.track": 17,

		"pm.session.open": 18, "pm.session.read": 19, "pm.message.send": 20,

		"follow_task.create": 21, "follow_task.update": 22,

		"reach.card.send": 23, "reach.sms.send": 24, "reach.web.send": 25,

		"reach.weixin.send": 26, "reach.wecom.send": 27, "reach.feishu.send": 28, "reach.dingtalk.send": 29,
		"reach.telegram.send": 30, "reach.whatsapp.send": 31, "reach.douyin.send": 32, "reach.kuaishou.send": 33,
		"reach.xhs.send": 34,

		"reach.batch": 35, "reach.schedule": 36, "reach.recall": 37, "reach.health": 38,
		"reach.history": 39, "reach.template.apply": 40, "reach.account.list": 41,
	}

	if len(allowed) > 0 {
		allowedWithCard := ensureToolInList(allowed, cardShowToolName)
		ordered := make([]AgentToolDef, 0, len(allowedWithCard))
		for _, n := range allowedWithCard {
			for _, t := range tools {
				if t.Name == n {
					ordered = append(ordered, t)
					break
				}
			}
		}
		if len(ordered) > 30 {
			return ordered[:30]
		}
		return ordered
	}

	if len(tools) <= maxTools {
		return ensureCardShowPresent(tools, tools, maxTools)
	}
	type scored struct {
		tool  AgentToolDef
		score int
	}
	scoredTools := make([]scored, 0, len(tools))
	for i, t := range tools {
		s, ok := priority[t.Name]
		if !ok {

			s = 100 + i
		}
		scoredTools = append(scoredTools, scored{tool: t, score: s})
	}
	sort.Slice(scoredTools, func(i, j int) bool {
		return scoredTools[i].score < scoredTools[j].score
	})
	result := make([]AgentToolDef, 0, maxTools)
	for i := 0; i < maxTools && i < len(scoredTools); i++ {
		result = append(result, scoredTools[i].tool)
	}
	return ensureCardShowPresent(tools, result, maxTools)
}

const cardShowToolName = "card.show"

func ensureToolInList(list []string, name string) []string {
	for _, l := range list {
		if l == name {
			return list
		}
	}
	return append(append([]string{}, list...), name)
}

func ensureCardShowPresent(all, selected []AgentToolDef, maxTools int) []AgentToolDef {
	var cardTool *AgentToolDef
	for i := range all {
		if all[i].Name == cardShowToolName {
			cardTool = &all[i]
			break
		}
	}
	if cardTool == nil {
		return selected
	}
	for _, t := range selected {
		if t.Name == cardShowToolName {
			return selected
		}
	}
	if len(selected) < maxTools {
		return append(selected, *cardTool)
	}
	res := make([]AgentToolDef, len(selected))
	copy(res, selected)
	res[len(res)-1] = *cardTool
	return res
}

func (e *SalesEngine) buildPrompt(
	req *SalesRequest,
	intent *dto.RecognizeResult,
	mem *model.DialogueMemory,
	sop *model.SOPAgent,
	stage string,
	ragChunks []RAGChunk,
	script *ScriptTemplate,
	customer *model.Customer,
) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("【客户消息】: %s\n\n", req.UserMessage))

	if req.EmotionHint != "" {
		sb.WriteString(fmt.Sprintf("【情绪应对策略】: %s\n\n", req.EmotionHint))
	}

	if customer != nil {
		name := customerNameOf(customer)
		if name != "" {
			sb.WriteString(fmt.Sprintf("【客户昵称】: %s\n", name))
		}
		if customer.Phone != "" {
			sb.WriteString(fmt.Sprintf("【客户手机】: %s\n", customer.Phone))
		}
	}

	if intent != nil {
		sb.WriteString(fmt.Sprintf("\n【识别意图】: %s (置信度 %.0f%%)\n",
			intent.IntentName, intent.Confidence*100))
	}

	if sop != nil {
		sb.WriteString(fmt.Sprintf("【适用 SOP】: %s\n", sop.Name))
	}
	if stage != "" && stage != "default" {
		sb.WriteString(fmt.Sprintf("【客户阶段】: %s\n", stage))
	}

	// RAG chunks 已在 buildAgentSystemPrompt (system message) 里拼一次，
	// 这里不再重复，避免占双倍 token（ContextBudget 统一管理）
	// 留 ragChunks 参数以便未来做按需注入

	if script != nil {
		sb.WriteString(fmt.Sprintf("\n【话术参考】: %s\n", truncate(script.Content, 150)))
	}

	if mem != nil && len(mem.KeyFacts) > 0 {
		sb.WriteString("\n【关键事实】:\n")
		factsJSON, _ := json.Marshal(mem.KeyFacts)
		sb.WriteString(string(factsJSON))
		sb.WriteString("\n")
	}

	if e.db != nil && req.SessionID != "" {
		hist := e.fetchHistoryWithinTokenBudget(req.SessionID, req.UserMessage)
		if len(hist) > 0 {
			var hb strings.Builder
			hb.WriteString(fmt.Sprintf("\n【对话历史（按时间顺序，共 %d 条）】\n", len(hist)))
			for _, m := range hist {
				role := "客户"
				if m.SenderType == "ai" || m.SenderType == "agent" {
					role = "AI"
				}
				hb.WriteString(fmt.Sprintf("%s：%s\n", role, m.Content))
			}
			sb.WriteString(hb.String())
		}
	}

	if intent != nil && len(intent.TopKExamples) > 0 {
		sb.WriteString("\n【意图参考示例】:\n")
		for i, ex := range intent.TopKExamples {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, ex))
		}
	}

	sb.WriteString("\n【回复要求】:\n")
	sb.WriteString("1. 基于上述信息（含对话历史）生成回复，不要编造事实\n")
	sb.WriteString("2. 简洁（≤80 字），分自然段\n")
	sb.WriteString("3. 语气亲切、像真人对话\n")
	sb.WriteString("4. 若客户异议，按话术/SOP 引导\n")

	return sb.String()
}

func safeID(c *model.Customer) string {
	if c == nil {
		return ""
	}
	return c.ID
}

const agentLoopHistoryTokenBudget = 4096

const agentLoopHistoryOutputReservePct = 30

const agentLoopHistoryMaxCandidates = 200

const historyMsgTokenOverhead = 6

func (e *SalesEngine) fetchHistoryWithinTokenBudget(sessionID, userMessage string) []model.SessionMessage {
	var hist []model.SessionMessage
	if err := e.db.Where("session_id = ?", sessionID).
		Order("id desc").Limit(agentLoopHistoryMaxCandidates).Find(&hist).Error; err != nil || len(hist) == 0 {
		return nil
	}

	if hist[0].Content == userMessage {
		hist = hist[1:]
	}
	if len(hist) == 0 {
		return nil
	}

	budget := agentLoopHistoryTokenBudget * (100 - agentLoopHistoryOutputReservePct) / 100
	used := 0
	keep := 0
	for i, m := range hist {
		cost := textutil.EstimateTokens(m.Content) + historyMsgTokenOverhead
		if used+cost > budget {
			keep = i
			break
		}
		used += cost
		keep = i + 1
	}
	truncated := keep < len(hist)
	hist = hist[:keep]

	if truncated {
		for len(hist) > 0 {
			last := hist[len(hist)-1]
			if last.SenderType != "ai" && last.SenderType != "agent" {
				break
			}
			hist = hist[:len(hist)-1]
		}
	}

	for i, j := 0, len(hist)-1; i < j; i, j = i+1, j-1 {
		hist[i], hist[j] = hist[j], hist[i]
	}
	return hist
}

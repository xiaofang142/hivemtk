package service

import (
	"context"
	"fmt"
	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"
	confidencesvc "hivemtk-user/internal/service/confidence"
	humanizesvc "hivemtk-user/internal/service/humanize"
	"strconv"
	"strings"
	"time"
)

type AgentToolExecutor interface {
	ListTools(ctx context.Context) []AgentToolDef
	DispatchToolCalls(ctx context.Context, calls []AgentToolCall, toolCtx AgentToolContext) []AgentToolResult
}

type AgentToolPermissionChecker interface {
	SetAgentWhitelist(agentID string, tools []string)
}

type AgentToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type AgentToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type AgentToolContext struct {
	AgentID    string
	SessionID  string
	CustomerID string
	Source     string
}

type AgentToolResult struct {
	ToolCallID string          `json:"tool_call_id"`
	Content    string          `json:"content"`
	Success    bool            `json:"success"`
	Card       *model.RichCard `json:"card,omitempty"`
}

type SalesEngine struct {
	sessionMsgRepo  *repository.SessionMessageRepository
	dispatcher      *llm.Dispatcher
	intent          IntentRecognizerInterface
	memory          DialogueMemoryInterface
	sop             SOPMatcherInterface
	polisher        PolisherInterface
	behaviorPl      *BehavioralPlanBuilder
	ragSearcher     RAGSearcher
	scriptLookup    ScriptLookup
	customerLookup  CustomerLookup
	playbook        PlaybookRecommenderInterface
	feedbackLearner FeedbackRecorderInterface

	confidenceAggregator *confidencesvc.ConfidenceAggregator

	humanizeEvaluator *humanizesvc.HumanizeEvalService

	toolExecutor AgentToolExecutor

	layerRouter *LayerRouter

	// faqSvc FAQ 快速通道的服务端（标准答案直答，零 LLM 调用）。
	// 为 nil 时本通道不生效，其余行为不变。
	faqSvc *FAQService

	permissionChecker AgentToolPermissionChecker

	// intentChain 意图→工具调用流绑定层（TB-03）；nil=未装配，行为与既有链路完全一致。
	// 命中绑定时：绑定工具晋升到可用清单前部（保证过 maxTools 截断）+ 系统提示
	// 注入"按链优先调用"指引；LLM 仍自主决策，绑定失效安全回落。
	intentChain agent_runtime.IntentChainLookup

	// intentRouteDaily 意图路由日聚合写侧（TB-11 刀2）；nil=未装配，loop 出口零变化。
	intentRouteDaily repository.IntentRouteDailyRepository

	glossary   GlossaryRenderer
	calibrator OutputCalibrator
}

// SetIntentChainLookup 注入意图→工具调用流绑定层（装配层调用；运行期只读）。
func (e *SalesEngine) SetIntentChainLookup(l agent_runtime.IntentChainLookup) {
	e.intentChain = l
}

// SetIntentRouteDailyRepository 注入路由观测聚合仓储（TB-11 刀2；装配层调用，运行期只读）。
func (e *SalesEngine) SetIntentRouteDailyRepository(repo repository.IntentRouteDailyRepository) {
	e.intentRouteDaily = repo
}

func (e *SalesEngine) SetPermissionChecker(pc AgentToolPermissionChecker) {
	e.permissionChecker = pc
}

type PolisherInterface interface {
	Polish(ctx context.Context, raw string, pctx *PolishContext) (string, error)
}

type SalesEngineConfig = dto.SalesEngineConfig

func DefaultSalesEngineConfig() *SalesEngineConfig {
	return dto.DefaultSalesEngineConfig()
}

func NewSalesEngine(
	dispatcher *llm.Dispatcher,

	intent IntentRecognizerInterface,

	memory DialogueMemoryInterface,

	sop SOPMatcherInterface,

	ragSearcher RAGSearcher,

	scriptLookup ScriptLookup,

	customerLookup CustomerLookup,

) *SalesEngine {
	polisher := NewHumanizePolisher()
	return &SalesEngine{
		sessionMsgRepo: repository.NewSessionMessageRepository(),
		dispatcher:     dispatcher,
		intent:         intent,
		memory:         memory,
		sop:            sop,
		polisher:       polisher,
		behaviorPl:     NewBehavioralPlanBuilder(),
		ragSearcher:    ragSearcher,
		scriptLookup:   scriptLookup,
		customerLookup: customerLookup,
	}
}

// SetBehavioralHumanize 注入行为层拟人开关
// 关闭时使用 noop（直接返回原文本，单条消息，0 延迟）
func (e *SalesEngine) SetBehavioralHumanize(enabled bool) {
	if e.behaviorPl == nil {
		e.behaviorPl = NewBehavioralPlanBuilder()
	}
	e.behaviorPl.SetEnabled(enabled)
}

func isFirstMessageOf(req *SalesRequest) bool {

	return req != nil && req.IsFirstTurn
}

func (e *SalesEngine) SetFeedbackLearner(ctx context.Context, fl FeedbackRecorderInterface) {
	e.feedbackLearner = fl
}

func (e *SalesEngine) SetConfidenceAggregator(ctx context.Context, agg *confidencesvc.ConfidenceAggregator) {
	e.confidenceAggregator = agg
}

// ConfidenceAggregator getter（D01：orchestrator 复用同实例，避免重复初始化）
func (e *SalesEngine) ConfidenceAggregator() *confidencesvc.ConfidenceAggregator {
	return e.confidenceAggregator
}

func (e *SalesEngine) SetHumanizeEvaluator(ctx context.Context, ev *humanizesvc.HumanizeEvalService) {
	e.humanizeEvaluator = ev
}

func (e *SalesEngine) SetToolExecutor(ctx context.Context, exec AgentToolExecutor) {
	e.toolExecutor = exec
}

func (e *SalesEngine) SetLayerRouter(ctx context.Context, lr *LayerRouter) {
	e.layerRouter = lr
}

// SetFAQService 注入 FAQ 服务（开启 FAQ 快速通道：命中标准答案直接作答）。
//
// 为 nil（不调用本方法）时快速通道不生效，引擎行为与挂载前逐字一致。
func (e *SalesEngine) SetFAQService(svc *FAQService) {
	e.faqSvc = svc
}

// tryFAQFastPath 尝试用 FAQ 标准答案直接回复。
//
// 第二个返回值表示"是否命中并已作答"。命中时：零 LLM 调用、零 Token、
// 答案确定（人工标注的标准答案，不是模型现场编的）。
//
// 与 LayerRouter.Route 的关系：Route 是**双层架构的实验通道**，受 FF_LAYER1 旗子
// 控制（默认关），且只在流式路径被调用。本方法是 FAQ 这一个能力的最小实现，
// 不受该旗子约束 —— 官网客服靠它才能把"介绍下项目"这类问题答对。
//
// 为什么不复用 Route：Route 的开关语义是"要不要开双层架构实验"，而这里是
// "有一条标准答案要不要直接用"。把后者绑在前者上，等于官网客服的能力取决于
// 一个默认关闭的实验旗子。
func (e *SalesEngine) tryFAQFastPath(ctx context.Context, req *SalesRequest) (*SalesResponse, bool) {
	if e.faqSvc == nil || req == nil || strings.TrimSpace(req.UserMessage) == "" {
		return nil, false
	}
	// NoFastpath（2026-11-14 生产实证）：同会话已投递过相同标准答案（客户追问、答案
	// 未解决）时编排器置位——固定文案再发一次只会撞已投递占位静默丢失，改走完整
	// LLM 链结合对话历史生成针对性新回答。
	if req.NoFastpath {
		return nil, false
	}
	agentID := agentIDFromCtx(req)
	if agentID == 0 {
		// 没有挂载智能体时不走本通道：没有智能体归属就没有 FAQ 归属，
		// 拿全局共享 FAQ 回答会串到别的租户/商户的话术上。
		return nil, false
	}
	matches, err := e.faqSvc.MatchByAgent(ctx, agentID, req.UserMessage, faqTopKDefault)
	if err != nil || len(matches) == 0 {
		return nil, false
	}
	top := matches[0]
	if top.Score < faqHitThresh {
		return nil, false
	}
	entry := top.Entry
	if entry == nil || strings.TrimSpace(entry.Answer) == "" {
		return nil, false
	}

	// T2 引用溯源：FAQ 命中即带出引用（来源卡片 + match_type + confidence 快照）。
	// 旁路字段，不影响命中判定；转人工/低分兜底轮次天然无引用。
	var citations []dto.Citation
	if e.faqSvc != nil {
		citations = e.faqSvc.MatchCitationsForAgent(ctx, agentID, req.UserMessage)
	}

	resp := &SalesResponse{
		Reply: entry.Answer,
		// top.Score 是关键词/向量加权分，可能 > 1（多词叠加）；置信度对外契约是 0~1，
		// 原样透出会让前端进度条/百分比出现 103% 这类非法值。
		Intent: &dto.RecognizeResult{IntentType: entry.Intent, Confidence: clamp01(top.Score)},
		Confidence: &dto.ConfidenceDecision{
			AggregatedConf:   clamp01(top.Score),
			DynamicThreshold: faqHitThresh,
			DecisionBand:     dto.BandAuto,
		},
		// 标记为确定答案：编排器据此跳过置信度判据（标准答案不需要再判可不可信）。
		DefiniteAnswer: true,
		Citations:      citations,
		Steps: []dto.SalesStepLog{{
			Step:   "layer1_faq_fastpath",
			Status: "ok",
			Detail: fmt.Sprintf("faq_id=%d score=%.4f", entry.ID, top.Score),
		}},
	}
	return resp, true
}

type SalesRequest = dto.SalesRequest

// clamp01 把分值夹到 [0,1]，用于对外暴露置信度（契约要求 0~1）。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func agentIDFromCtx(req *SalesRequest) uint {
	if req == nil || req.AgentContext == nil {
		return 0
	}
	return req.AgentContext.AgentID
}

type SalesResponse = dto.SalesResponse

// salesEngineHandleFault 引擎故障注入接缝（仅测试装配；生产恒 nil，行为零差异）。
// CS-02 引擎降级链用例（cs02_degraded_caliber_and_engine_fault_test.go）用它让
// Handle 入口直接失败，从而真实驱动编排器的 L1 备用 Handle 重试 → L2 RuleEngine
// → L3 转人工全链——生产代码里 Handle 除空参数外不返回错误，没有这条接缝那条
// 降级链的中间两级对测试永远不可达。
var salesEngineHandleFault func(req *SalesRequest) error

// SetSalesEngineHandleFault 注入/清除引擎故障（fn=nil 恢复生产语义）。仅测试调用。
func SetSalesEngineHandleFault(fn func(req *SalesRequest) error) {
	salesEngineHandleFault = fn
}

func (e *SalesEngine) Handle(ctx context.Context, req *SalesRequest) (*SalesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.UserMessage == "" {
		return nil, fmt.Errorf("user_message is empty")
	}
	if req.Config == nil {
		req.Config = DefaultSalesEngineConfig()
	}
	if salesEngineHandleFault != nil {
		if err := salesEngineHandleFault(req); err != nil {
			return nil, err
		}
	}

	// Layer1 FAQ 快速通道：命中就用标准答案直接回复，跳过 LLM 与 RAG。
	//
	// 为什么非流式路径也要走这一段：官网网页客服（HTTP /api/chat/public/*）走的正是
	// 本方法，而它早先只在 HandleStream 里查 FAQ —— 于是网页客服永远走 LLM 慢路径：
	// RAG 弱召回 → 置信度不足 → 转人工 → 「暂无坐席在线」。
	// FAQ 里明明有「HiveMTK 是什么？」这种标准答案，却一次都没被用上。
	if resp, hit := e.tryFAQFastPath(ctx, req); hit {
		return resp, nil
	}

	if e.shouldUseParallel() {
		return e.HandleParallel(ctx, req)
	}

	start := time.Now()
	resp := &SalesResponse{
		Steps: make([]dto.SalesStepLog, 0, 9),
	}
	defer func() {
		resp.LatencyMs = int(time.Since(start).Milliseconds())

		e.recordFeedback(ctx, req, resp)

	}()

	stepStart := time.Now()
	customer, err := e.resolveCustomer(ctx, req)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "1_resolve_customer", Status: "fail", Error: err.Error(),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "1_resolve_customer", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("customer_id=%s", safeID(customer)),
		})
	}

	stepStart = time.Now()
	memCtx, err := e.recallMemory(ctx, req)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "2_recall_memory", Status: "fail", Error: err.Error(),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "2_recall_memory", Status: "ok", LatencyMs: ms(stepStart),
			Extra: map[string]any{"key_facts": memCtx.KeyFacts},
		})
	}
	resp.Memory = DialogueMemoryToDTO(memCtx)

	stepStart = time.Now()
	var intentResult *dto.RecognizeResult
	if e.intent != nil {
		intentResult, err = e.intent.Recognize(ctx, req.SessionID, req.CustomerID, req.UserMessage)
		if err != nil {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "3_recognize_intent", Status: "fail", Error: err.Error(),
			})
			intentResult = &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0.3}
		} else {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "3_recognize_intent", Status: "ok", LatencyMs: ms(stepStart),
				Detail: fmt.Sprintf("%s (conf=%.2f)", intentResult.IntentType, intentResult.Confidence),
			})
		}
	} else {
		intentResult = &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0.3, Method: "fallback"}
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "3_recognize_intent", Status: "skip", LatencyMs: ms(stepStart),
			Detail: "intent_recognizer not injected",
		})
	}
	resp.Intent = intentResult

	if intentResult.IntentType == IntentClarify {
		reply := BuildClarifyReply(intentResult)
		resp.Reply = reply
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "3.5_clarify", Status: "ok", LatencyMs: 0, Detail: reply,
		})
		return resp, nil
	}

	transfer, reason := e.shouldTransferToHuman(ctx, intentResult, memCtx, req)
	if transfer {
		resp.TransferredToHuman = true
		resp.TransferReason = reason
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "3.5_transfer_check", Status: "ok",
			Detail: "transferred: " + resp.TransferReason,
		})
		resp.Reply = "[系统自动转人工] " + resp.TransferReason
		return resp, nil
	}

	stepStart = time.Now()
	matchedSOP, sopStage, err := e.matchSOP(ctx, intentResult, customer)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "4_match_sop", Status: "fail", Error: err.Error(),
		})
	} else if matchedSOP != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "4_match_sop", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("sop=%s stage=%s", matchedSOP.Name, sopStage),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "4_match_sop", Status: "skip", LatencyMs: ms(stepStart),
		})
	}
	resp.MatchedSOP = SOPAgentToDTO(matchedSOP)

	stepStart = time.Now()
	ragChunks, err := e.recallRAG(ctx, req, intentResult)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5_recall_rag", Status: "fail", Error: err.Error(),
		})
	} else {
		// ContextBudget 动态裁剪：按 relevance score 排序后塞入 token 预算
		budget := NewContextBudget(defaultContextWindow)
		ragChunks, dropped := budget.TruncateRAGChunks(ragChunks)

		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5_recall_rag", Status: "ok", LatencyMs: ms(stepStart),
			Extra: map[string]any{
				"chunk_count":    len(ragChunks),
				"chunks_dropped": dropped,
				"rag_budget":     budget.RAGTokenBudget(),
				"total_context":  budget.TotalContext,
			},
		})
	}
	resp.RAGChunks = ragChunks

	stepStart = time.Now()
	script, err := e.matchScript(ctx, intentResult)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.5_match_script", Status: "fail", Error: err.Error(),
		})
	} else if script != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.5_match_script", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("script_id=%s rate=%.2f", script.ID, script.MatchRate),

			Extra: map[string]any{"script_id": script.ID, "objection_category": intentResult.IntentType},
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.5_match_script", Status: "skip", LatencyMs: ms(stepStart),
		})
	}
	resp.ScriptTemplate = script

	stepStart = time.Now()
	if e.playbook != nil && intentResult != nil {

		industry := Industry("")
		productID := ""
		stage := stageToJourneyStage(sopStage)
		resp.PlaybookSuggestions = e.fetchPlaybookSuggestions(ctx, industry, productID, stage, intentResult.IntentType)
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.6_playbook_suggest", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("count=%d stage=%s intent=%s", len(resp.PlaybookSuggestions), stage, intentResult.IntentType),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.6_playbook_suggest", Status: "skip", LatencyMs: ms(stepStart),
		})
	}

	stepStart = time.Now()
	candidate, llmResult, cards, err := e.generateCandidate(ctx, req, intentResult, memCtx, matchedSOP, sopStage, ragChunks, script, customer)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "6_generate_candidate", Status: "fail", Error: err.Error(),
		})

		switch {
		case script != nil && script.Content != "":
			candidate = script.Content
		case len(ragChunks) > 0 && ragChunks[0].Content != "":

			candidate = "根据知识库：\n" + truncateForReply(ragChunks[0].Content, 280)
		default:
			candidate = "您好，请问有什么可以帮您？"
		}
	} else {

		if llmResult != nil {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "6_generate_candidate", Status: "ok", LatencyMs: ms(stepStart),
				Extra: map[string]any{
					"provider": llmResult.Provider, "model": llmResult.Model,
					"tokens": llmResult.TotalTokens, "cost": llmResult.Cost,
				},
			})
		} else {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "6_generate_candidate", Status: "ok", LatencyMs: ms(stepStart),
				Extra: map[string]any{
					"provider": "", "model": "",
					"tokens": 0, "cost": float64(0),
					"note": "candidate from script/rag fallback (no LLM call)",
				},
			})
		}
	}
	// 模型空输出的降级兜底（emptyReplyFallback）不是模型答案：打上 Degraded 标记，
	// 编排器透传到出站行 extra.degraded 并摘掉继承来的意图置信度（CS-02 口径）。
	// 判定点在 generateCandidate 返回处、polish 之前——humanize/语言校准层改写文本
	// 不影响标记；两条 fallback 注入点（plain dispatch 的空回复分支与 agent loop 的
	// 空输出分支）产出的都是同一个常量串，字节等值判定即覆盖。
	if candidate == e.emptyReplyFallback() {
		resp.Degraded = true
	}
	if llmResult != nil {
		resp.LLMProvider = llmResult.Provider
		resp.LLMModel = llmResult.Model
		resp.CostTokens = llmResult.TotalTokens
	}

	stepStart = time.Now()
	finalReply := candidate

	resp.Cards = RichCardsToDTO(cards)
	if req.Config.EnableHumanizePolish {
		polished, err := e.polisher.Polish(ctx, candidate, &PolishContext{
			Persona:      req.Config.Persona,
			Intent:       intentResult.IntentType,
			Platform:     req.Platform,
			Stage:        sopStage,
			CustomerName: customerNameOf(customer),
		})
		if err != nil {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "7_polish", Status: "fail", Error: err.Error(),
			})
		} else {
			finalReply = polished
			resp.Polished = true
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "7_polish", Status: "ok", LatencyMs: ms(stepStart),
			})
		}
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "7_polish", Status: "skip", LatencyMs: ms(stepStart),
		})
	}

	if e.humanizeEvaluator != nil && HumanizeEvaluatorEnabled {
		stepStart = time.Now()

		if e.behaviorPl != nil && e.behaviorPl.IsEnabled() {
			plan := e.behaviorPl.Build(finalReply, isFirstMessageOf(req))
			resp.SendPlan = &dto.SendPlanDTO{
				Messages:   plan.Messages,
				Intervals:  plan.Intervals,
				TotalDelay: plan.TotalDelay,
			}
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step:      "6.5_behavioral",
				Status:    "ok",
				LatencyMs: ms(stepStart),
				Detail:    "behavioral humanize: " + strconv.Itoa(len(plan.Messages)) + " segments",
			})
		}

		evalInput := &dto.HumanizeEvalInput{
			SessionID:       req.SessionID,
			CustomerID:      req.CustomerID,
			CustomerMessage: req.UserMessage,
			AIReply:         finalReply,
			Persona:         req.Config.Persona,
			Intent:          intentResult.IntentType,
			Platform:        req.Platform,
		}
		evalResult, err := e.humanizeEvaluator.Evaluate(ctx, evalInput)
		if err != nil {
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "7.5_humanize_eval", Status: "fail", LatencyMs: ms(stepStart),
				Error: err.Error(),
			})
		} else if evalResult != nil {
			resp.HumanizeScore = evalResult.TotalScore
			resp.HumanizePassed = evalResult.Passed
			resp.HumanizeAttempt = evalResult.AttemptCount
			if !evalResult.Passed {

				resp.HumanizeScore = evalResult.TotalScore
				resp.HumanizePassed = false
				resp.HumanizeAttempt = evalResult.AttemptCount
				if evalResult.FinalReply != "" {
					finalReply = evalResult.FinalReply
				}
				resp.Steps = append(resp.Steps, dto.SalesStepLog{
					Step: "7.5_humanize_eval", Status: "fail_soft", LatencyMs: ms(stepStart),
					Detail: fmt.Sprintf("拟人度未达标（%.2f < %.2f），保留 AI 回复下发（不转人工）", evalResult.TotalScore, humanizesvc.EffectiveThreshold()),
				})

			}

			if evalResult.FinalReply != "" && evalResult.FinalReply != finalReply {
				finalReply = evalResult.FinalReply
			}
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step: "7.5_humanize_eval", Status: "ok", LatencyMs: ms(stepStart),
				Extra: map[string]any{
					"score":    evalResult.TotalScore,
					"strategy": evalResult.SampleStrategy,
					"attempts": evalResult.AttemptCount,
				},
			})
		}
	}

	resp.Reply = finalReply

	if e.memory != nil {
		_ = e.memory.AppendMessage(ctx, req.SessionID, req.CustomerID, dto.Message{
			Role:      "ai",
			Content:   finalReply,
			Timestamp: time.Now(),
		})
	}

	return resp, nil
}

type StreamChunkCallback = func(chunk *dto.StreamChunk) bool

func (e *SalesEngine) HandleStream(ctx context.Context, req *SalesRequest, onChunk StreamChunkCallback) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.UserMessage == "" {
		return fmt.Errorf("user_message is empty")
	}
	if onChunk == nil {
		return fmt.Errorf("onChunk callback is nil")
	}
	if req.Config == nil {
		req.Config = DefaultSalesEngineConfig()
	}

	startChunk := &dto.StreamChunk{
		Type: dto.ChunkTypeStart,

		TraceID: func() string {
			if c := tracing.CarrierFromContext(ctx); c != nil {
				return c.TraceID
			}
			return tracing.TraceIDFromContext(ctx)
		}(),
		Step: "start",
	}
	if !onChunk(startChunk) {
		return ctx.Err()
	}

	if e.layerRouter != nil {

		decision := e.layerRouter.Route(ctx, &RouteRequest{
			SessionID:   req.SessionID,
			CustomerID:  req.CustomerID,
			UserMessage: req.UserMessage,
			Intent:      nil,
			RAGChunks:   nil,
			Stage:       "",

			AgentID: agentIDFromCtx(req),
		})
		if decision != nil && decision.SkipLLM && decision.Reply != "" {

			_ = onChunk(&dto.StreamChunk{
				Type:    dto.ChunkTypeDelta,
				TraceID: startChunk.TraceID,
				Text:    decision.Reply,
				Layer:   dto.Layer1,
				Step:    "layer1_delta",
			})
			finalChunk := &dto.StreamChunk{
				Type:    dto.ChunkTypeFinal,
				TraceID: startChunk.TraceID,
				Text:    decision.Reply,
				Layer:   dto.Layer1,
				Step:    "layer1_final",
				WallMs:  decision.WallMs,
				Steps: []dto.SalesStepLog{{
					Step: "layer1_fastpath", Status: "ok", LatencyMs: decision.WallMs,
					Detail: fmt.Sprintf("layer1 hit (reason=%s faq_id=%d sop_id=%d)",
						decision.Reason, decision.FAQID, decision.SOPID),
				}},
				Metadata: fmt.Sprintf(`{"layer1_reason":"%s","faq_id":%d,"sop_id":%d}`,
					decision.Reason, decision.FAQID, decision.SOPID),
			}
			if !onChunk(finalChunk) {
				return ctx.Err()
			}
			return nil
		}
	}

	placeholder := "正在为您查询…"
	_ = onChunk(&dto.StreamChunk{
		Type:    dto.ChunkTypeDelta,
		TraceID: startChunk.TraceID,
		Text:    placeholder,
		Layer:   dto.Layer2,
		Step:    "placeholder",
	})

	resp, err := e.Handle(ctx, req)
	if err != nil {
		_ = onChunk(&dto.StreamChunk{
			Type:    dto.ChunkTypeError,
			TraceID: startChunk.TraceID,
			Error:   err.Error(),
		})
		return fmt.Errorf("handle stream: %w", err)
	}

	reply := resp.Reply
	if reply == "" {

	} else {
		runes := []rune(reply)
		const batchSize = 4
		interval := 15 * time.Millisecond
		for i := 0; i < len(runes); i += batchSize {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			end := i + batchSize
			if end > len(runes) {
				end = len(runes)
			}
			delta := &dto.StreamChunk{
				Type:    dto.ChunkTypeDelta,
				TraceID: startChunk.TraceID,
				Text:    string(runes[i:end]),
				Layer:   dto.Layer2,
				Step:    "layer2_delta",
			}
			if !onChunk(delta) {
				return ctx.Err()
			}
			if end < len(runes) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(interval):
				}
			}
		}
	}

	finalChunk := &dto.StreamChunk{
		Type:    dto.ChunkTypeFinal,
		TraceID: startChunk.TraceID,
		Text:    resp.Reply,
		Steps:   resp.Steps,
		WallMs:  resp.LatencyMs,
		Model:   resp.LLMModel,
		Tokens:  resp.CostTokens,
		Layer:   dto.Layer2,
		Step:    "layer2_final",
	}
	if resp.TransferredToHuman {
		finalChunk.Metadata = `{"transferred_to_human":true,"reason":"` + resp.TransferReason + `"}`
	}
	if !onChunk(finalChunk) {
		return ctx.Err()
	}
	return nil
}

var agentLoopMaxIterations = 5

var agentLoopMaxTools = 18

func resolveAgentSettings(ctx context.Context) (maxTools, maxIter int, disabled []string) {
	maxTools, maxIter = agentLoopMaxTools, agentLoopMaxIterations
	if cfg, err := LoadAgentSettingsConfig(ctx); err == nil && cfg != nil {
		if cfg.MaxTools > 0 {
			maxTools = cfg.MaxTools
		}
		if cfg.MaxLoopIterations > 0 {
			maxIter = cfg.MaxLoopIterations
		}
		disabled = cfg.DisabledTools
	}
	return
}

var agentLoopTotalTimeout = 180 * time.Second

var agentLoopMaxTotalTokens = 50000

var agentLoopMaxPerIterTimeout = 60 * time.Second

// SetAgentLoopMaxTotalTokens 注入 Agent Loop 累计 token 预算
// 由 main.go 启动时调用；≤0 时保持默认 50000
func SetAgentLoopMaxTotalTokens(n int) {
	if n <= 0 {
		return
	}
	agentLoopMaxTotalTokens = n
}

// SetAgentLoopMaxPerIterTimeout 注入单次 LLM 调用超时（秒）
// 由 main.go 启动时调用；≤0 时保持默认 60s
func SetAgentLoopMaxPerIterTimeout(seconds int) {
	if seconds <= 0 {
		return
	}
	agentLoopMaxPerIterTimeout = time.Duration(seconds) * time.Second
}

func ms(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}

// ProcessIncomingMessage 多渠道统一入口
func (e *SalesEngine) ProcessIncomingMessage(ctx context.Context, msg *ChannelMessage) (*SalesResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}
	if msg.Content == "" && msg.MediaURL == "" {

		return &SalesResponse{}, nil
	}

	content, sessionID, customerID := e.normalizeChannelMessage(ctx, msg)

	req := &SalesRequest{
		SessionID:   sessionID,
		CustomerID:  customerID,
		OneID:       customerID,
		UserMessage: content,
		Platform:    msg.Channel,
		Config:      DefaultSalesEngineConfig(),
	}

	if msg.Channel == "feishu" {
		req.Config.Persona = "你是飞书上的企业助手，回复简洁专业。"
	} else if msg.Channel == "telegram" {
		req.Config.Persona = "你是 Telegram 上的销售助手，语气亲切。"
	}

	return e.Handle(ctx, req)
}

func stageToJourneyStage(stage string) JourneyStage {
	switch stage {
	case "churn_risk":
		return StageSleeping
	case "active":
		return StageContact
	case "default":
		return StageLead
	default:
		return StageLead
	}
}

package service

import (
	"context"
	"fmt"
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
	ListTools() []AgentToolDef
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

	glossary   GlossaryRenderer
	calibrator OutputCalibrator
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
		Reply:  entry.Answer,
		Intent: &dto.RecognizeResult{IntentType: entry.Intent, Confidence: top.Score},
		Confidence: &dto.ConfidenceDecision{
			AggregatedConf:   top.Score,
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

func agentIDFromCtx(req *SalesRequest) uint {
	if req == nil || req.AgentContext == nil {
		return 0
	}
	return req.AgentContext.AgentID
}

type SalesResponse = dto.SalesResponse

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

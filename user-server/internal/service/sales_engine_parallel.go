package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/featureflag"
)

// 5 阶段阶段名常量 (用于 Steps 日志)
const (
	PhaseParallel = "0_phase_parallel"
	PhaseSerial   = "1_phase_serial"
	PhaseAsync    = "2_phase_async"
)

type phase0Result struct {
	customer  *model.Customer
	memCtx    *model.DialogueMemory
	intent    *dto.RecognizeResult
	intentCh  <-chan *dto.RecognizeResult
	ragChunks []RAGChunk
}

// HandleParallel 5 阶段并行化版本入口
//
// 与 Handle 的区别:
//   - 步骤 1-3 + 5 (RAG) 并行执行, 节省 2-3x wall time
//   - 意图识别改用投机版 (RecognizeSpeculative)
//   - Phase 2 异步收割 LLM intent (10ms 超时, 不阻塞主流程)
//
// 兼容性: 通过 FF_PARALLEL 开关控制, 关闭时回退到原 Handle 行为
func (e *SalesEngine) HandleParallel(ctx context.Context, req *SalesRequest) (*SalesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.UserMessage == "" {
		return nil, fmt.Errorf("user_message is empty")
	}
	if req.Config == nil {
		req.Config = DefaultSalesEngineConfig()
	}

	start := time.Now()
	resp := &SalesResponse{
		Steps: make([]dto.SalesStepLog, 0, 12),
	}

	var phase0Ptr *phase0Result
	defer func() {
		resp.LatencyMs = int(time.Since(start).Milliseconds())
		e.recordFeedback(ctx, req, resp)
		_ = phase0Ptr
	}()

	phase0Start := time.Now()
	phase0 := e.runPhase0Parallel(ctx, req)
	phase0Ptr = phase0
	resp.Steps = append(resp.Steps, dto.SalesStepLog{
		Step:      PhaseParallel,
		Status:    "ok",
		LatencyMs: ms(phase0Start),
		Detail:    fmt.Sprintf("phase0 (customer+memory+intent+rag parallel) wall=%dms", ms(phase0Start)),
	})
	resp.Intent = phase0.intent
	resp.Memory = DialogueMemoryToDTO(phase0.memCtx)
	resp.RAGChunks = phase0.ragChunks

	if e.layerRouter != nil {
		phase05Start := time.Now()
		decision := e.layerRouter.Route(ctx, &RouteRequest{
			SessionID:   req.SessionID,
			CustomerID:  req.CustomerID,
			UserMessage: req.UserMessage,
			Intent:      phase0.intent,
			RAGChunks:   phase0.ragChunks,
			Stage:       "",
			AgentID:     agentIDFromCtx(req),
		})
		if decision != nil && decision.SkipLLM && decision.Reply != "" {
			resp.Reply = decision.Reply
			resp.Steps = append(resp.Steps, dto.SalesStepLog{
				Step:      "0.5_layer1_fastpath",
				Status:    "ok",
				LatencyMs: ms(phase05Start),
				Detail: fmt.Sprintf("layer1 hit (reason=%s faq_id=%d sop_id=%d) wall=%dms",
					decision.Reason, decision.FAQID, decision.SOPID, decision.WallMs),
			})
			return resp, nil
		}
	}

	phase1Start := time.Now()
	transferred := e.runPhase1Serial(ctx, req, resp, phase0)
	if transferred {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step:      PhaseSerial,
			Status:    "skip",
			LatencyMs: ms(phase1Start),
			Detail:    "transferred to human, phase 2/3 skipped",
		})
		return resp, nil
	}
	resp.Steps = append(resp.Steps, dto.SalesStepLog{
		Step:      PhaseSerial,
		Status:    "ok",
		LatencyMs: ms(phase1Start),
		Detail:    fmt.Sprintf("phase1 (3.5+4+5.5+5.6+6 serial) wall=%dms", ms(phase1Start)),
	})

	phase2Start := time.Now()
	if phase0.intentCh != nil {
		select {
		case llmIntent, ok := <-phase0.intentCh:
			if ok && llmIntent != nil && llmIntent.Confidence > phase0.intent.Confidence {
				if featureflag.Get("debug_log").Bool() {
					resp.Steps = append(resp.Steps, dto.SalesStepLog{
						Step: PhaseAsync, Status: "ok", LatencyMs: ms(phase2Start),
						Detail: fmt.Sprintf("intent upgraded: %s (%.2f) > %s (%.2f)",
							llmIntent.IntentType, llmIntent.Confidence,
							phase0.intent.IntentType, phase0.intent.Confidence),
					})
				}
			}
		case <-time.After(10 * time.Millisecond):
			if featureflag.Get("debug_log").Bool() {
				resp.Steps = append(resp.Steps, dto.SalesStepLog{
					Step: PhaseAsync, Status: "timeout", LatencyMs: ms(phase2Start),
					Detail: "LLM intent not received within 10ms, continue with rule result",
				})
			}
		}
	}

	return resp, nil
}

func (e *SalesEngine) runPhase0Parallel(ctx context.Context, req *SalesRequest) *phase0Result {
	out := &phase0Result{}

	speculative, hasSpeculative := e.intent.(interface {
		RecognizeSpeculative(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, <-chan *dto.RecognizeResult, error)
	})

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	g.Go(func() error {
		c, err := e.resolveCustomer(gctx, req)
		if err == nil {
			mu.Lock()
			out.customer = c
			mu.Unlock()
		}
		return nil
	})

	g.Go(func() error {
		m, err := e.recallMemory(gctx, req)
		if err == nil {
			mu.Lock()
			out.memCtx = m
			mu.Unlock()
		}
		return nil
	})

	g.Go(func() error {
		if hasSpeculative && speculative != nil {
			intent, ch, err := speculative.RecognizeSpeculative(gctx, req.SessionID, req.CustomerID, req.UserMessage)
			if err == nil {
				mu.Lock()
				out.intent = intent
				out.intentCh = ch
				mu.Unlock()
			}
		} else if e.intent != nil {
			intent, err := e.intent.Recognize(gctx, req.SessionID, req.CustomerID, req.UserMessage)
			if err == nil && intent != nil {
				mu.Lock()
				out.intent = intent
				mu.Unlock()
			}
		}
		if out.intent == nil {
			mu.Lock()
			out.intent = &dto.RecognizeResult{
				IntentType: IntentUnknown, Confidence: 0.3, Method: "fallback",
			}
			mu.Unlock()
		}
		return nil
	})

	g.Go(func() error {
		mu.Lock()
		intentReady := out.intent
		mu.Unlock()
		if intentReady == nil {
			r, err := e.recallRAG(gctx, req, &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0})
			if err == nil {
				mu.Lock()
				out.ragChunks = r
				mu.Unlock()
			}
		} else {
			r, err := e.recallRAG(gctx, req, intentReady)
			if err == nil {
				mu.Lock()
				out.ragChunks = r
				mu.Unlock()
			}
		}
		return nil
	})

	_ = g.Wait()
	return out
}

func (e *SalesEngine) runPhase1Serial(ctx context.Context, req *SalesRequest, resp *SalesResponse, phase0 *phase0Result) bool {
	intent := phase0.intent
	memCtx := phase0.memCtx
	customer := phase0.customer
	// 上下文透传：phase0 已取到 customer/memory，丢弃会导致转人工判断缺记忆
	// 依据、SOP 阶段路由恒 default、prompt 缺客户信息（parallel 模式静默退化）
	_ = memCtx
	_ = customer

	stepStart := time.Now()
	transfer, reason := e.shouldTransferToHuman(ctx, intent, memCtx, req)
	if transfer {
		resp.TransferredToHuman = true
		resp.TransferReason = reason
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "3.5_transfer_check", Status: "ok", LatencyMs: ms(stepStart),
			Detail: "transferred: " + reason,
		})
		resp.Reply = "[系统自动转人工] " + reason
		return true
	}

	stepStart = time.Now()
	sopAgent, stage, err := e.matchSOP(ctx, intent, customer)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "4_match_sop", Status: "fail", Error: err.Error(),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "4_match_sop", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("stage=%s", stage),
		})
	}

	stepStart = time.Now()
	scriptTpl, err := e.matchScript(ctx, intent)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.5_match_script", Status: "fail", Error: err.Error(),
		})
	} else {
		stepLog := dto.SalesStepLog{
			Step: "5.5_match_script", Status: "ok", LatencyMs: ms(stepStart),
		}
		if scriptTpl != nil {

			stepLog.Extra = map[string]any{"script_id": scriptTpl.ID, "objection_category": intent.IntentType}
			resp.ScriptTemplate = scriptTpl
		}
		resp.Steps = append(resp.Steps, stepLog)
	}

	stepStart = time.Now()
	if e.playbook != nil {
		_ = e.RecommendPlaybook(ctx, "", "", "", intent.IntentType)
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "5.6_playbook", Status: "ok", LatencyMs: ms(stepStart),
		})
	}

	stepStart = time.Now()
	reply, dispatchResult, cards, err := e.generateCandidate(ctx, req, intent, memCtx, sopAgent, stage, phase0.ragChunks, nil, customer)
	if err != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "6_generate_candidate", Status: "fail", Error: err.Error(),
		})
		resp.Reply = "抱歉, 服务暂时不可用, 请稍后重试。"
		return false
	}
	resp.Reply = reply
	resp.Cards = RichCardsToDTO(cards)
	if dispatchResult != nil {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "6_generate_candidate", Status: "ok", LatencyMs: ms(stepStart),
			Detail: fmt.Sprintf("model=%s tokens=%d", dispatchResult.Model, dispatchResult.TotalTokens),
		})
	} else {
		resp.Steps = append(resp.Steps, dto.SalesStepLog{
			Step: "6_generate_candidate", Status: "ok", LatencyMs: ms(stepStart),
			Detail: "sop_template_used",
		})
	}

	return false
}

func (e *SalesEngine) shouldUseParallel() bool {
	return featureflag.Get("parallel").Bool()
}

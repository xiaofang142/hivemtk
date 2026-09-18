package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	sopHitThresh = 0.65
)

// FAQMatcher FAQ 匹配接口 (DIP 重构, 便于 mock 单测)
//
// 满足接口的最小方法集: MatchByKeyword + MatchByAgent (Task 15 强 1对1) + IncrementHitCount
type FAQMatcher interface {
	MatchByKeyword(ctx context.Context, msg string, topK int) ([]model.FAQEntry, error)
	MatchByAgent(ctx context.Context, agentID uint, msg string, topK int) ([]model.FAQEntry, error)
	IncrementHitCount(ctx context.Context, id uint) error
}

// LayerRouter 双层路由决策器
type LayerRouter struct {
	faqRepo   FAQMatcher
	sopRepo   *repository.SOPTemplateRepository
	logRepo   *repository.LayerDecisionLogRepository
	faqSvc    *FAQService
	sopSvc    *SOPTemplateService
	traceFunc func() string
}

// NewLayerRouter 创建 LayerRouter
func NewLayerRouter(
	db *gorm.DB,
	faqRepo FAQMatcher,
	sopRepo *repository.SOPTemplateRepository,
	logRepo *repository.LayerDecisionLogRepository,
	faqSvc *FAQService,
	sopSvc *SOPTemplateService,
) *LayerRouter {
	if faqRepo == nil && db != nil {
		faqRepo = repository.NewFAQRepository(db)
	}
	if sopRepo == nil && db != nil {
		sopRepo = repository.NewSOPTemplateRepository(db)
	}
	if logRepo == nil && db != nil {
		logRepo = repository.NewLayerDecisionLogRepository(db)
	}
	return &LayerRouter{
		faqRepo: faqRepo,
		sopRepo: sopRepo,
		logRepo: logRepo,
		faqSvc:  faqSvc,
		sopSvc:  sopSvc,
	}
}

// RouteRequest 路由请求参数
//
// Task 15 变更:
//   - AgentFAQIDs (旧字段) 被弃用, 改用 AgentID 强 1对1
//   - AgentSOPTemplateIDs (旧字段) 同样, 后续 Task 16 改造 SOP
type RouteRequest struct {
	SessionID           string
	CustomerID          string
	UserMessage         string
	Intent              *dto.RecognizeResult
	RAGChunks           []RAGChunk
	Stage               string
	AgentID             uint
	AgentFAQIDs         []string
	AgentSOPTemplateIDs []string
}

// Route 路由决策 (核心入口)
func (r *LayerRouter) Route(ctx context.Context, req *RouteRequest) *dto.LayerDecision {
	start := time.Now()
	decision := &dto.LayerDecision{
		Layer:   dto.Layer2,
		SkipLLM: false,
		Reason:  dto.ReasonFallback,
		Intent:  intentType(req.Intent),
		WallMs:  0,
	}
	defer func() {
		decision.WallMs = int(time.Since(start).Milliseconds())
		r.record(ctx, req, decision)
		_ = decision.Layer
		_ = decision.Reason
	}()

	if !featureflag.Get("layer1").Bool() {
		decision.Layer = dto.Layer2
		decision.Reason = dto.ReasonLayer1Disabled
		return decision
	}

	if req.AgentID == 0 {
	} else if r.faqSvc != nil && strings.TrimSpace(req.UserMessage) != "" {
		matches, err := r.faqSvc.MatchByAgent(ctx, req.AgentID, req.UserMessage, 3)
		if err == nil && len(matches) > 0 {
			top := matches[0]
			if top.Score >= faqHitThresh {
				decision.Layer = dto.Layer1
				decision.SkipLLM = true
				if top.Entry != nil {
					decision.Reply = top.Entry.Answer
					decision.FAQID = top.Entry.ID
				}
				decision.Reason = dto.ReasonFAQHit
				decision.Confidence = top.Score
				if top.Entry != nil {
					decision.Intent = top.Entry.Intent
				}
				if intentType(req.Intent) == "" && top.Entry != nil {
					decision.Intent = top.Entry.Intent
				}
				if top.Entry != nil {

					entryID := top.Entry.ID
					utils.SafeGo(context.Background(), "layer.faq_hit_count", func(_ context.Context) {
						_ = r.faqRepo.IncrementHitCount(context.Background(), entryID)
					})
				}
				return decision
			}
			decision.Reason = dto.ReasonLowConfidenceSkip
		}
	} else if r.faqRepo != nil && strings.TrimSpace(req.UserMessage) != "" {
		if req.AgentID > 0 {
			entries, err := r.faqRepo.MatchByAgent(ctx, req.AgentID, req.UserMessage, 3)
			if err == nil && len(entries) > 0 {
				top := entries[0]
				if top.Confidence >= faqHitThresh {
					decision.Layer = dto.Layer1
					decision.SkipLLM = true
					decision.Reply = top.Answer
					decision.FAQID = top.ID
					decision.Reason = dto.ReasonFAQHit
					decision.Confidence = top.Confidence
					decision.Intent = top.Intent
					if intentType(req.Intent) == "" {
						decision.Intent = top.Intent
					}

					faqID := top.ID
					utils.SafeGo(context.Background(), "layer.faq_hit_count", func(_ context.Context) {
						_ = r.faqRepo.IncrementHitCount(context.Background(), faqID)
					})
					return decision
				}
				decision.Reason = dto.ReasonLowConfidenceSkip
			}
		}
	}

	if r.sopSvc != nil && req.AgentID > 0 && req.Intent != nil && req.Intent.IntentType != "" && req.Intent.IntentType != IntentUnknown {
		tpls, err := r.sopSvc.MatchByAgent(ctx, req.AgentID, req.Intent.IntentType, req.Stage, sopTopK)
		if err == nil && len(tpls) > 0 {
			top := tpls[0]
			if top.Confidence >= sopHitThresh {
				decision.Layer = dto.Layer1
				decision.SkipLLM = true
				decision.Reason = dto.ReasonSOPHit
				decision.Confidence = top.Confidence
				decision.SOPID = top.ID
				decision.Intent = top.Intent
				rendered, rErr := r.sopSvc.BuildLayer1Reply(&top, map[string]any{
					"customer_id":  req.CustomerID,
					"intent":       req.Intent.IntentType,
					"intent_name":  req.Intent.IntentName,
					"stage":        req.Stage,
					"user_message": req.UserMessage,
				})
				if rErr == nil && rendered != "" {
					decision.Reply = rendered
				} else {
					decision.Layer = dto.Layer2
					decision.SkipLLM = false
					decision.Reason = dto.ReasonFallback
					if featureflag.Get("debug_log").Bool() {
						logger.Warnf("[LayerRouter] SOP render failed: %v", rErr)
					}
					return decision
				}

				sopID := top.ID
				utils.SafeGo(context.Background(), "layer.sop_hit_count", func(_ context.Context) {
					_ = r.sopRepo.IncrementHitCount(context.Background(), sopID)
				})
				return decision
			}
		}
	}

	decision.Layer = dto.Layer2
	decision.SkipLLM = false
	if decision.Reason == "" {
		decision.Reason = dto.ReasonFallback
	}
	return decision
}

func (r *LayerRouter) record(ctx context.Context, req *RouteRequest, d *dto.LayerDecision) {
	if r.logRepo == nil {
		return
	}
	confIn := 0.0
	if req.Intent != nil {
		confIn = req.Intent.Confidence
	}
	llmSkipped := d.SkipLLM
	log := &model.LayerDecisionLog{
		TraceID:    r.traceID(),
		SessionID:  req.SessionID,
		CustomerID: req.CustomerID,
		Layer:      d.Layer,
		Reason:     d.Reason,
		Intent:     d.Intent,
		ConfIn:     confIn,
		ConfOut:    d.Confidence,
		WallMs:     d.WallMs,
		LLMSkipped: &llmSkipped,
		Extra:      fmt.Sprintf("faq_id=%d sop_id=%d", d.FAQID, d.SOPID),
	}

	utils.SafeGo(context.Background(), "layer.record", func(_ context.Context) {
		bgCtx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
		defer cancel()
		if err := r.logRepo.Record(bgCtx, log); err != nil {
			if featureflag.Get("debug_log").Bool() {
				logger.Warnf("[LayerRouter] record failed: %v", err)
			}
		}
	})
}

func (r *LayerRouter) traceID() string {
	if r.traceFunc != nil {
		return r.traceFunc()
	}
	return fmt.Sprintf("lr-%d", time.Now().UnixNano())
}

func intentType(r *dto.RecognizeResult) string {
	if r == nil {
		return ""
	}
	return r.IntentType
}

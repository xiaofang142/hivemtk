package service

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"golang.org/x/sync/errgroup"
)

// DetectionMessage 参与检测的一条消息（来自 ConversationContext 的多轮 + 历史）。
type DetectionMessage struct {
	MessageID  string                  `json:"message_id"`
	Content    string                  `json:"content"`
	Role       string                  `json:"role"`
	SenderID   string                  `json:"sender_id"`
	SenderName string                  `json:"sender_name,omitempty"`
	Visibility agentcontext.Visibility `json:"visibility,omitempty"`
	SentAt     int64                   `json:"sent_at,omitempty"`
}

// DetectionHit 单路检测结果。三路各自独立，由 LeadDetectionService 并行收集。
type DetectionHit struct {
	Type               string            `json:"type"` // keyword / semantic / llm
	Payload            model.JSONPayload `json:"payload"`
	EvidenceMessageIDs model.JSONStrings `json:"evidence_message_ids"`
	Complete           bool              `json:"complete"`
	Degraded           bool              `json:"degraded"`
	Reason             string            `json:"reason"`
}

// DetectionInput 检测输入：规则 + 上下文消息 + 归属判定 ID。
type DetectionInput struct {
	Rule        *model.LeadMiningRule
	Messages    []DetectionMessage
	JudgementID string
	Scope       *agentcontext.ConversationScope
	ContextView *agentcontext.AgentConversationContext
}

// LeadDetectionService 三路并行检测编排（FR-012）。
//
// 关键词与语义关键词为纯本地计算，LLM 检测走全局 Dispatcher（与 dispatcherJudge
// 同一路径），三路用 errgroup 并行；任一路 panic 都被 recover 成一条降级证据，
// 绝不击穿进程或静默丢弃——这与宪法 III「不许吹大 / fail-visible」一致。
type LeadDetectionService struct {
	keyword  *LeadKeywordDetector
	semantic *LeadSemanticDetector
	llm      *LeadLLMDetector
	hitRepo  repository.LeadMiningHitRepository
}

func NewLeadDetectionService() *LeadDetectionService {
	return &LeadDetectionService{
		keyword:  NewLeadKeywordDetector(),
		semantic: NewLeadSemanticDetector(),
		llm:      NewLeadLLMDetector(),
		hitRepo:  repository.NewLeadMiningHitRepository(),
	}
}

// Run 并行执行三路检测并把每路结果独立落库，返回落库后的证据。
// 规则为空或消息为空时 fail-closed 返回错误，不产出"空命中"伪证据。
func (s *LeadDetectionService) Run(ctx context.Context, in DetectionInput) ([]model.LeadMiningHit, error) {
	if in.Rule == nil {
		return nil, fmt.Errorf("检测缺少账号/渠道规则")
	}
	if len(in.Messages) == 0 {
		return nil, fmt.Errorf("检测缺少消息上下文")
	}
	if in.JudgementID == "" {
		return nil, fmt.Errorf("检测缺少 judgement_id")
	}
	if s == nil {
		return nil, fmt.Errorf("检测服务未初始化")
	}

	var mu sync.Mutex
	hits := make([]DetectionHit, 0, 3)
	collect := func(h DetectionHit) {
		mu.Lock()
		hits = append(hits, h)
		mu.Unlock()
	}

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		collect(runLeadDetectorSafe(ctx, "keyword", func() DetectionHit {
			return s.keyword.Detect(in)
		}))
		return nil
	})
	g.Go(func() error {
		collect(runLeadDetectorSafe(ctx, "semantic", func() DetectionHit {
			return s.semantic.Detect(in)
		}))
		return nil
	})
	g.Go(func() error {
		collect(runLeadDetectorSafe(ctx, "llm", func() DetectionHit {
			return s.llm.Detect(ctx, in)
		}))
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	records := make([]*model.LeadMiningHit, 0, len(hits))
	for _, h := range hits {
		r := h
		records = append(records, &model.LeadMiningHit{
			JudgementID:        in.JudgementID,
			Type:               r.Type,
			Payload:            r.Payload,
			EvidenceMessageIDs: r.EvidenceMessageIDs,
			Complete:           r.Complete,
			Degraded:           r.Degraded,
			Reason:             r.Reason,
		})
	}
	if s.hitRepo != nil {
		if err := s.hitRepo.CreateBatch(ctx, records); err != nil {
			return nil, fmt.Errorf("落库三路检测证据失败: %w", err)
		}
	}
	return derefLeadHits(records), nil
}

func derefLeadHits(in []*model.LeadMiningHit) []model.LeadMiningHit {
	out := make([]model.LeadMiningHit, 0, len(in))
	for _, h := range in {
		if h != nil {
			out = append(out, *h)
		}
	}
	return out
}

func runLeadDetectorSafe(ctx context.Context, name string, fn func() DetectionHit) (h DetectionHit) {
	defer func() {
		if r := recover(); r != nil {
			logger.Ctx(ctx).Error().
				Str("detector", name).
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("[lead-detect] 检测器 panic 已收敛为降级证据")
			h = DetectionHit{
				Type:     name,
				Payload:  model.JSONPayload{"panic": fmt.Sprint(r)},
				Complete: false,
				Degraded: true,
				Reason:   fmt.Sprintf("检测器 %s panic，证据不完整", name),
			}
		}
	}()
	h = fn()
	if h.Type == "" {
		h.Type = name
	}
	return h
}

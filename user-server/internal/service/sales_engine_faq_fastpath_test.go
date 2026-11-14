package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 回归：官网右下角网页客服问「介绍下项目」，AI 走 RAG 弱召回 → 置信度不足 → 转人工
// → 30 秒后前端提示「暂无坐席在线」。
//
// FAQ 里明明有「HiveMTK 是什么？」这条标准答案，但网页客服走的非流式
// SalesEngine.Handle 路径**从不查 FAQ**（Layer1 路由只在 HandleStream 里被调用，
// 且装配层从未 SetLayerRouter）⇒ FAQ 形同虚设。
//
// 这一组用例锁住 FAQ 快速通道：命中标准答案就直接作答，零 LLM 调用。

// newFAQServiceForFastPath 造一个只带候选集的 FAQService。
//
// 打分走 repository.FAQRepository.ScoreCandidates（真实打分口径：jaccard +
// 关键词加分），不另写一份 —— 另写一份就等于让用例锁住一份与生产不同的算法。
func newFAQServiceForFastPath(entries []model.FAQEntry) *FAQService {
	repo := &repository.FAQRepository{}
	svc := NewFAQService()
	svc.repo = &faqFastPathRepo{base: repo, entries: entries}
	return svc
}

// faqFastPathRepo 只覆写"取候选集"与"打分"两个方法，其余走基类的空实现。
type faqFastPathRepo struct {
	base    *repository.FAQRepository
	entries []model.FAQEntry
}

func (r *faqFastPathRepo) ListCandidates(_ context.Context, agentID uint, _ int) ([]model.FAQEntry, error) {
	if agentID == 0 {
		return nil, nil
	}
	return r.entries, nil
}

func (r *faqFastPathRepo) ScoreCandidates(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	return repository.ScoreFAQCandidates(r.base, entries, msg, topK)
}

func (r *faqFastPathRepo) MatchByKeyword(context.Context, string, int) ([]model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) MatchByAgent(context.Context, uint, string, int) ([]model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) ListByAgent(context.Context, uint, int) ([]model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) IncrementHitCount(context.Context, uint) error { return nil }
func (r *faqFastPathRepo) DecayQuality(context.Context, uint, float64) error {
	return nil
}
func (r *faqFastPathRepo) ListDecayCandidates(context.Context, time.Time, int) ([]model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) IncrementNegativeHit(context.Context, uint) error { return nil }
func (r *faqFastPathRepo) ListWithFilter(context.Context, repository.FAQListParams) ([]model.FAQEntry, int64, error) {
	return nil, 0, nil
}
func (r *faqFastPathRepo) GetByID(context.Context, uint) (*model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) ListEnabled(context.Context, int) ([]model.FAQEntry, error) {
	return nil, nil
}
func (r *faqFastPathRepo) Create(context.Context, *model.FAQEntry) error { return nil }
func (r *faqFastPathRepo) Update(context.Context, uint, *model.FAQEntry) error {
	return nil
}
func (r *faqFastPathRepo) SetEnabled(context.Context, uint, bool) error { return nil }
func (r *faqFastPathRepo) Delete(context.Context, uint) error           { return nil }

// TestTryFAQFastPath_HitsStandardAnswer FAQ 命中时直接返回标准答案。
func TestTryFAQFastPath_HitsStandardAnswer(t *testing.T) {
	enabled := true
	agentID := uint(1)
	engine := &SalesEngine{faqSvc: newFAQServiceForFastPath([]model.FAQEntry{
		{ID: 1, Question: "HiveMTK 是什么？", Answer: "HiveMTK 是一款开源的智能营销系统…",
			Keywords:   []string{"HiveMTK", "是什么", "介绍", "产品", "项目", "营销系统", "开源"},
			Confidence: 0.95, Enabled: &enabled, AgentID: &agentID},
	})}

	req := &SalesRequest{
		UserMessage: "介绍下项目",
		AgentContext: &dto.AgentContext{
			AgentID: 1,
		},
	}
	resp, hit := engine.tryFAQFastPath(context.Background(), req)
	if !hit {
		t.Fatal("「介绍下项目」应命中 FAQ 标准答案（线上 FAQ 有「HiveMTK 是什么？」）")
	}
	if resp == nil || resp.Reply == "" {
		t.Fatal("命中 FAQ 时必须给出标准答案")
	}
	if resp.TransferredToHuman {
		t.Error("命中 FAQ 不应转人工")
	}
	if resp.Confidence == nil || resp.Confidence.AggregatedConf < faqHitThresh {
		t.Errorf("命中 FAQ 的置信度应 ≥ 门槛 %v, got %v", faqHitThresh, resp.Confidence)
	}
	if !resp.DefiniteAnswer {
		t.Error("FAQ 直答应标记为确定答案（编排器据此跳过置信度判据）")
	}
}

// TestTryFAQFastPath_NoAgentNoHit 没有挂载智能体时不走本通道。
//
// 没有智能体就没有 FAQ 归属：拿全局共享 FAQ 回答会串到别人的话术上。
func TestTryFAQFastPath_NoAgentNoHit(t *testing.T) {
	enabled := true
	agentID := uint(1)
	engine := &SalesEngine{faqSvc: newFAQServiceForFastPath([]model.FAQEntry{
		{ID: 1, Question: "HiveMTK 是什么？", Answer: "答案", Keywords: []string{"介绍", "项目"},
			Confidence: 0.95, Enabled: &enabled, AgentID: &agentID},
	})}

	_, hit := engine.tryFAQFastPath(context.Background(), &SalesRequest{UserMessage: "介绍下项目"})
	if hit {
		t.Error("没有挂载智能体时不应走 FAQ 快速通道（会串到别的商户的话术）")
	}
}

// TestTryFAQFastPath_NoServiceNoHit 没注入 FAQ 服务时本通道不生效。
func TestTryFAQFastPath_NoServiceNoHit(t *testing.T) {
	engine := &SalesEngine{}
	_, hit := engine.tryFAQFastPath(context.Background(), &SalesRequest{UserMessage: "介绍下项目"})
	if hit {
		t.Error("未注入 FAQ 服务时不应命中")
	}
}

// ScoreCandidatesScored T2 引用溯源：复用真实打分口径并按层还原 MatchType。
func (r *faqFastPathRepo) ScoreCandidatesScored(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]repository.FAQScoredEntry, error) {
	// 走真实 repo 的 ScoreCandidatesScored（真实打分口径 + 层信息）
	return r.base.ScoreCandidatesScored(ctx, entries, msg, topK)
}

// NoFastpath 置位时 fastpath 必须让路（2026-11-14 生产实证回归钉）：
// 同会话已投递过相同标准答案后客户追问，编排器置位重跑完整 LLM——固定文案
// 再发一次只会撞已投递占位静默丢失。
func TestTryFAQFastPath_NoFastpathBypassesHit(t *testing.T) {
	enabled := true
	agentID := uint(1)
	engine := &SalesEngine{faqSvc: newFAQServiceForFastPath([]model.FAQEntry{
		{ID: 1, Question: "HiveMTK 是什么？", Answer: "HiveMTK 是一款开源的智能营销系统…",
			Keywords:   []string{"HiveMTK", "是什么", "介绍", "产品", "项目", "营销系统", "开源"},
			Confidence: 0.95, Enabled: &enabled, AgentID: &agentID},
	})}
	req := &SalesRequest{
		UserMessage: "介绍下项目",
		AgentContext: &dto.AgentContext{
			AgentID: 1,
		},
		NoFastpath: true,
	}
	if resp, hit := engine.tryFAQFastPath(context.Background(), req); hit {
		t.Fatalf("NoFastpath 置位时不应命中 fastpath，实际返回 %+v", resp)
	}
}

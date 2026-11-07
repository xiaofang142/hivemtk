// reflux adopter：审核采纳动作与写回 faq_entries（T1）。
//
// 状态机：draft→pending_review→adopted|ignored|merged（非法迁移拒绝）。
// adopt 时经 keyworder 去泛化硬校验，经 FAQService.Create 写回（复用 Layer1
// 关键词索引维护），全簇成员置 adopted 并回填 adopted_faq_id，写 review_log。
// 2026-10-25：DB 访问整体下沉 repository/reflux.go（句柄下沉台账 reflux 域）；
// 构造器保持 NewRefluxService(db) 签名，db 为瞬时参量（即建 repo，不存字段）。
package reflux

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ErrIllegalTransition 非法状态迁移
var ErrIllegalTransition = errors.New("非法状态迁移")

// ErrNotFound 目标不存在
var ErrNotFound = errors.New("目标不存在")

// ReviewAction 审核动作
type ReviewAction string

const (
	ActionSubmit ReviewAction = "submit"
	ActionAdopt  ReviewAction = "adopt"
	ActionIgnore ReviewAction = "ignore"
	ActionMerge  ReviewAction = "merge"
)

// AdoptRequest 审核请求
type AdoptRequest struct {
	Action         ReviewAction `json:"action" binding:"required"`
	MergedTargetID int64        `json:"merged_target_id"`
	Answer         string       `json:"answer"`
	Category       string       `json:"category"`
}

// RefluxService 审核/采纳服务（controller 唯一入口）
type RefluxService struct {
	repo *repository.RefluxRepository
	// FAQCreate 复用 FAQService.Create 的函数钩子（装配层注入，避免包依赖环）。
	// 签名：create(ctx, req) → faq_id
	FAQCreate func(ctx context.Context, req FAQCreateReq) (uint, error)
}

// FAQCreateReq 写回 FAQ 的参数（字段映射见设计文档 §6.1）
type FAQCreateReq struct {
	Question  string
	Answer    string
	Keywords  []string
	Category  string
	AgentID   uint
	Intent    string
	Enabled   bool
	SourceTag string
}

// NewRefluxService 构造（db 为瞬时参量：即建 repo，service 不存句柄——
// 保持既有签名，router/service_routes.go 装配与集成测试零改动）
func NewRefluxService(repo *repository.RefluxRepository) *RefluxService {
	return &RefluxService{repo: repo}
}

// SetFAQCreate 装配层注入 FAQService.Create 钩子
func (s *RefluxService) SetFAQCreate(fn func(ctx context.Context, req FAQCreateReq) (uint, error)) {
	s.FAQCreate = fn
}

// allowedTransitions 状态机白名单
var allowedTransitions = map[string]map[ReviewAction]bool{
	model.UQStatusDraft:         {ActionSubmit: true},
	model.UQStatusPendingReview: {ActionAdopt: true, ActionIgnore: true, ActionMerge: true},
}

// SubmitQuestion 单条 draft→pending_review
func (s *RefluxService) SubmitQuestion(ctx context.Context, id int64, operatorID string) error {
	return s.transition(ctx, "question", id, model.UQStatusDraft, model.UQStatusPendingReview,
		ActionSubmit, operatorID, "")
}

// ReviewQuestion 单条审核
func (s *RefluxService) ReviewQuestion(ctx context.Context, id int64, req *AdoptRequest, operatorID string) (map[string]any, error) {
	uq, err := s.repo.FindQuestion(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	switch req.Action {
	case ActionIgnore:
		if err := s.transition(ctx, "question", id, uq.Status, model.UQStatusIgnored, ActionIgnore, operatorID, ""); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "status": model.UQStatusIgnored}, nil
	case ActionMerge:
		if req.MergedTargetID == 0 {
			return nil, fmt.Errorf("merged_target_id 必填")
		}
		if err := s.repo.UpdateQuestion(ctx, id, map[string]any{
			"status": model.UQStatusMerged, "merged_target_id": req.MergedTargetID,
			"reviewed_by": operatorID, "reviewed_at": time.Now(),
		}); err != nil {
			return nil, err
		}
		s.log(ctx, "question", id, ActionMerge, uq.Status, model.UQStatusMerged, operatorID,
			fmt.Sprintf("merged_target_id=%d", req.MergedTargetID))
		return map[string]any{"id": id, "status": model.UQStatusMerged}, nil
	case ActionAdopt:
		return s.adoptSingle(ctx, uq, req, operatorID)
	default:
		// 未知动作单独给文案：此前一律回「非法状态迁移」，运营照抄这句话排查
		// 会往状态机上找，而实际是 body 里的 action 拼错/未注册（D-39 附带项）。
		return nil, fmt.Errorf("action 必须是 adopt/ignore/merge（收到 %q）", req.Action)
	}
}

// adoptSingle 单条采纳（非簇成员）
func (s *RefluxService) adoptSingle(ctx context.Context, uq *model.UnknownQuestion, req *AdoptRequest, operatorID string) (map[string]any, error) {
	if uq.Status != model.UQStatusPendingReview {
		return nil, ErrIllegalTransition
	}
	if req.Answer == "" {
		return nil, fmt.Errorf("answer 必填")
	}
	faqID, err := s.writeFAQ(ctx, uq.Question, req, uq.AgentID, uq.IntentType)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateQuestion(ctx, int64(uq.ID), map[string]any{
		"status": model.UQStatusAdopted, "adopted_faq_id": faqID,
		"reviewed_by": operatorID, "reviewed_at": time.Now(),
	}); err != nil {
		return nil, err
	}
	s.log(ctx, "question", uq.ID, ActionAdopt, uq.Status, model.UQStatusAdopted, operatorID,
		fmt.Sprintf("faq_id=%d", faqID))
	return map[string]any{"id": uq.ID, "faq_id": faqID, "status": model.UQStatusAdopted}, nil
}

// ReviewCluster 簇级审核（adopt 时对全簇生效，生成一条 FAQ）
func (s *RefluxService) ReviewCluster(ctx context.Context, clusterID int64, req *AdoptRequest, operatorID string) (map[string]any, error) {
	cluster, err := s.repo.FindCluster(ctx, clusterID)
	if err != nil {
		return nil, ErrNotFound
	}
	switch req.Action {
	case ActionIgnore:
		if err := s.repo.UpdateCluster(ctx, clusterID, map[string]any{
			"status": model.UQStatusIgnored, "reviewed_by": operatorID, "reviewed_at": time.Now(),
		}); err != nil {
			return nil, err
		}
		s.log(ctx, "cluster", clusterID, ActionIgnore, cluster.Status, model.UQStatusIgnored, operatorID, "")
		return map[string]any{"cluster_id": clusterID, "status": model.UQStatusIgnored}, nil
	case ActionAdopt:
		if cluster.Status != model.UQStatusPendingReview {
			return nil, ErrIllegalTransition
		}
		if req.Answer == "" {
			return nil, fmt.Errorf("answer 必填")
		}
		faqID, err := s.writeFAQ(ctx, cluster.Title, req, cluster.AgentID, "")
		if err != nil {
			return nil, err
		}
		adoptedCount, err := s.repo.AdoptClusterMembers(ctx, clusterID, faqID, operatorID)
		if err != nil {
			return nil, err
		}
		s.log(ctx, "cluster", clusterID, ActionAdopt, cluster.Status, model.UQStatusAdopted, operatorID,
			fmt.Sprintf("faq_id=%d adopted_count=%d total_frequency=%d", faqID, adoptedCount, cluster.TotalFrequency))
		return map[string]any{"cluster_id": clusterID, "faq_id": faqID, "adopted_count": adoptedCount}, nil
	default:
		return nil, ErrIllegalTransition
	}
}

// writeFAQ 关键词去泛化硬校验 + 经 FAQCreate 钩子写回
func (s *RefluxService) writeFAQ(ctx context.Context, question string, req *AdoptRequest, agentID uint, intent string) (uint, error) {
	keywords := GenerateKeywords(question)
	// 既有 FAQ 关键词全集（区分性校验用；取不到按空集处理，与原实现一致）
	existing := map[string]int{}
	if m, err := s.repo.ExistingFAQKeywords(ctx); err == nil {
		existing = m
	}
	if err := ValidateKeywords(keywords, existing); err != nil {
		return 0, err
	}
	if s.FAQCreate == nil {
		return 0, fmt.Errorf("FAQCreate 未装配")
	}
	category := req.Category
	if category == "" {
		category = "reflux"
	}
	return s.FAQCreate(ctx, FAQCreateReq{
		Question: question, Answer: req.Answer, Keywords: keywords,
		Category: category, AgentID: agentID, Intent: intent, Enabled: true, SourceTag: "reflux",
	})
}

// transition 通用状态迁移（白名单校验 + 流水）
func (s *RefluxService) transition(ctx context.Context, targetType string, id int64, from, to string, action ReviewAction, operatorID, detail string) error {
	allowed, ok := allowedTransitions[from][action]
	if !ok || !allowed {
		return ErrIllegalTransition
	}
	var err error
	if targetType == "cluster" {
		err = s.repo.TransitionCluster(ctx, id, from, to)
	} else {
		err = s.repo.TransitionQuestion(ctx, id, from, to)
	}
	if err != nil {
		return err
	}
	s.log(ctx, targetType, id, action, from, to, operatorID, detail)
	return nil
}

func (s *RefluxService) log(ctx context.Context, targetType string, targetID int64, action ReviewAction, from, to, operatorID, detail string) {
	_ = s.repo.InsertReviewLog(ctx, &model.UnknownQuestionReviewLog{
		TargetType: targetType, TargetID: targetID, Action: string(action),
		FromStatus: from, ToStatus: to, OperatorID: operatorID, Detail: detail,
	})
}

// ============================================================
// 查询侧（运营列表 / 详情 / 统计）
// ============================================================

// ListQuestions 未知问题分页（filter: status/source/agent_id/question_like）
func (s *RefluxService) ListQuestions(ctx context.Context, filter map[string]any, page, pageSize int) ([]model.UnknownQuestion, int64, error) {
	return s.repo.ListQuestions(ctx, filter, page, pageSize)
}

// ListClusters 簇分页（按 total_frequency 降序）
func (s *RefluxService) ListClusters(ctx context.Context, filter map[string]any, page, pageSize int) ([]model.UnknownQuestionCluster, int64, error) {
	return s.repo.ListClusters(ctx, filter, page, pageSize)
}

// Stats 统计：总数 / 待审 / 已采纳 / 采纳率
func (s *RefluxService) Stats(ctx context.Context) (map[string]any, error) {
	total, pending, adopted, err := s.repo.QuestionStats(ctx)
	if err != nil {
		return nil, err
	}
	rate := 0.0
	if total > 0 {
		rate = float64(adopted) / float64(total)
	}
	return map[string]any{
		"total": total, "pending": pending,
		"adopted": adopted, "adopt_rate": rate,
	}, nil
}

// QuestionDetail 详情：问题 + 审核流水 + 簇成员
func (s *RefluxService) QuestionDetail(ctx context.Context, id int64) (map[string]any, error) {
	uq, err := s.repo.FindQuestion(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	logs, _ := s.repo.QuestionReviewLogs(ctx, id)
	var cluster *model.UnknownQuestionCluster
	if uq.ClusterID > 0 {
		if c, err := s.repo.FindCluster(ctx, int64(uq.ClusterID)); err == nil {
			cluster = c
		}
	}
	return map[string]any{"question": uq, "review_logs": logs, "cluster": cluster}, nil
}

// ClusterDetail 簇详情：簇 + 成员 + 流水
func (s *RefluxService) ClusterDetail(ctx context.Context, id int64) (map[string]any, error) {
	cluster, err := s.repo.FindCluster(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	members, err := s.repo.ClusterMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	logs, _ := s.repo.ClusterReviewLogs(ctx, id)
	return map[string]any{"cluster": cluster, "members": members, "review_logs": logs}, nil
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// CRM-14：把 CRM-13 的画像结论落成可确认草稿，确认时重验权限/版本并幂等调用既有跟进接口。
// 本文件不新建商机/提醒模型：apply 默认走既有 FollowUpService.Schedule，
// 商机侧仍由 OpportunityConvertService 承接（其 LTC 闸门不因 AI 建议而绕过）。

// crmAIDraftRepo 是 ai_insight_drafts 的最小读写口（由 repository.AIInsightDraftRepository 满足）。
type crmAIDraftRepo interface {
	Create(ctx context.Context, d *model.AIInsightDraft) error
	GetByDraftID(ctx context.Context, draftID string) (*model.AIInsightDraft, error)
	UpdateReview(ctx context.Context, draftID, status string, confirmedBy *uint, reviewNote, appliedActionJSON string) error
}

// crmAIScopeRepo 按数据权限范围读取单个客户（CustomerPermissionService 满足）。
type crmAIScopeRepo interface {
	GetByIDScoped(ctx context.Context, scope CustomerDataScope, id string) (*model.Customer, error)
}

// CRMAIDraftInput 从一次 Analyze 结果生成草稿的输入。
type CRMAIDraftInput struct {
	CustomerID string
	Scope      CustomerDataScope
	Result     *CustomerAIResult
	Operator   string
}

// CRMAIDraftConfirmInput 确认/编辑/拒绝时的受权输入。
type CRMAIDraftConfirmInput struct {
	Scope           CustomerDataScope
	ExpectedVersion string
	EditedContent   string
}

type crmAIApplyFn func(context.Context, uint, *model.AIInsightDraft) (map[string]any, error)
type crmAIAuditFn func(ctx context.Context, action, detail string, payload any) error

// CRMAISuggestionDraftService 画像建议草稿服务（不直接改客户、不外发）。
type CRMAISuggestionDraftService struct {
	drafts     crmAIDraftRepo
	permission crmAIScopeRepo
	apply      crmAIApplyFn
	audit      crmAIAuditFn
	now        func() time.Time
}

func NewCRMAISuggestionDraftService() *CRMAISuggestionDraftService {
	repo := repository.NewAIInsightDraftRepository()
	return &CRMAISuggestionDraftService{
		drafts:     repo,
		permission: NewCustomerPermissionService(),
		now:        time.Now,
	}
}

// CreateFromAnalysis 仅把有据且未降级的结论落成草稿；无证据/降级结论不生成可应用动作。
func (s *CRMAISuggestionDraftService) CreateFromAnalysis(ctx context.Context, in CRMAIDraftInput) (*model.AIInsightDraft, error) {
	if in.Result == nil || strings.TrimSpace(in.CustomerID) == "" {
		return nil, errors.New("分析结果或客户 ID 为空，不生成草稿")
	}
	draftTime := time.Now()
	if s.now != nil {
		draftTime = s.now()
	}
	evidence := make([]map[string]any, 0, 4)
	content := map[string]any{
		"customer_id":    in.CustomerID,
		"prompt_version": in.Result.PromptVersion,
		"model":          in.Result.Model,
		"generated_at":   draftTime.UTC().Format(time.RFC3339),
		"mode":           "draft_only_no_customer_write",
		"suggestions":    []map[string]any{},
	}
	action := map[string]any{"type": "none", "reason": "no_actionable_evidence"}
	for _, sug := range in.Result.Suggestions {
		if sug.Kind != "opportunity" && sug.Kind != "followup" {
			continue
		}
		if len(sug.Evidence) == 0 || sug.DegradeReason != "" {
			continue
		}
		for _, ev := range sug.Evidence {
			evidence = append(evidence, map[string]any{
				"type": ev.Type, "ref": ev.Ref, "source": ev.Source,
				"snippet": ev.Snippet, "occurred_at": ev.OccurredAt,
			})
		}
		content["suggestions"] = append(content["suggestions"].([]map[string]any), map[string]any{
			"kind": sug.Kind, "content": sug.Content, "confidence": sug.Confidence,
			"confidence_source": sug.ConfidenceSource, "rule_score": sug.RuleScore,
		})
		if action["type"] == "none" {
			action = map[string]any{
				"type": "followup_suggestion", "customer_id": in.CustomerID,
				"title": "AI建议：" + sug.Kind, "content": sug.Content, "due_in_hours": 24,
			}
		}
	}
	if len(evidence) == 0 {
		return nil, errors.New("无有效证据的结论不能生成可应用草稿")
	}
	contentJSON, _ := json.Marshal(content)
	evidenceJSON, _ := json.Marshal(evidence)
	actionJSON, _ := json.Marshal(action)
	d := &model.AIInsightDraft{
		TenantID:           0,
		DraftID:            fmt.Sprintf("crmai_%d", draftTime.UnixNano()),
		EntityType:         "customer",
		EntityID:           in.CustomerID,
		InsightType:        "profile_action",
		Title:              "AI 画像建议草稿",
		ContentJSON:        contentJSON,
		EvidenceJSON:       evidenceJSON,
		SourceRefsJSON:     json.RawMessage("[]"),
		ProposedActionJSON: actionJSON,
		Status:             "draft",
		GeneratedBy:        in.Operator,
		ModelName:          in.Result.Model,
		PromptVersion:      in.Result.PromptVersion,
		Confidence:         crmAIDraftConfidence(in.Result),
	}
	if s.drafts == nil {
		return nil, errors.New("草稿仓储未装配")
	}
	if err := s.drafts.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// Confirm 确认/编辑草稿：重验当前 scope 与对象版本，幂等写既有跟进接口，并显式回报审计状态。
func (s *CRMAISuggestionDraftService) Confirm(ctx context.Context, draftID string, confirmedBy uint, in CRMAIDraftConfirmInput) (*AIInsightConfirmResult, error) {
	d, err := s.loadDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if d.Status != "draft" {
		return nil, fmt.Errorf("草稿状态为%s，不可确认", d.Status)
	}
	if err := s.verifyScopeVersion(ctx, d, in); err != nil {
		return nil, err
	}
	if in.EditedContent != "" {
		var content map[string]any
		if json.Unmarshal(d.ContentJSON, &content) != nil {
			content = map[string]any{}
		}
		content["edited_content"] = in.EditedContent
		if raw, err := json.Marshal(content); err == nil {
			d.ContentJSON = raw
		}
	}
	apply := s.resolveApply()
	applied, err := apply(ctx, confirmedBy, d)
	if err != nil {
		return nil, fmt.Errorf("业务写入失败: %w", err)
	}
	status := "confirmed"
	if ok, _ := applied["applied"].(bool); ok {
		status = "applied"
	}
	appliedJSON, _ := json.Marshal(applied)
	if err := s.drafts.UpdateReview(ctx, draftID, status, &confirmedBy, in.EditedContent, string(appliedJSON)); err != nil {
		return nil, fmt.Errorf("草稿状态写回失败: %w", err)
	}
	if s.audit != nil {
		if err := s.audit(ctx, "crm_ai_draft_confirm", draftID, map[string]any{
			"status": status, "scope": in.Scope, "applied": applied,
		}); err != nil {
			return nil, fmt.Errorf("审计写失败（业务已落地）: %w", err)
		}
	}
	updated, err := s.drafts.GetByDraftID(ctx, draftID)
	if err != nil {
		return nil, err
	}
	return &AIInsightConfirmResult{Draft: updated, Applied: status == "applied", AppliedAction: applied}, nil
}

// Reject 只改草稿状态，不写任何业务对象。
func (s *CRMAISuggestionDraftService) Reject(ctx context.Context, draftID string, confirmedBy uint, _ string) error {
	d, err := s.loadDraft(ctx, draftID)
	if err != nil {
		return err
	}
	if d.Status != "draft" {
		return fmt.Errorf("草稿状态为%s，不可拒绝", d.Status)
	}
	return s.drafts.UpdateReview(ctx, draftID, "rejected", &confirmedBy, "人工拒绝", "")
}

func (s *CRMAISuggestionDraftService) loadDraft(ctx context.Context, draftID string) (*model.AIInsightDraft, error) {
	if s.drafts == nil {
		return nil, errors.New("草稿仓储未装配")
	}
	d, err := s.drafts.GetByDraftID(ctx, draftID)
	if err != nil || d == nil {
		return nil, errors.New("草稿不存在或不可访问")
	}
	return d, nil
}

func (s *CRMAISuggestionDraftService) verifyScopeVersion(ctx context.Context, d *model.AIInsightDraft, in CRMAIDraftConfirmInput) error {
	if s.permission == nil {
		return errors.New("权限服务未装配，拒绝确认")
	}
	customer, err := s.permission.GetByIDScoped(ctx, in.Scope, d.EntityID)
	if err != nil || customer == nil {
		return fmt.Errorf("确认时客户已越权或不存在: %w", err)
	}
	version := d.UpdatedAt.UTC().Format(time.RFC3339)
	if d.UpdatedAt.IsZero() {
		version = d.CreatedAt.UTC().Format(time.RFC3339)
	}
	if in.ExpectedVersion == "" || in.ExpectedVersion != version {
		return errors.New("草稿版本已变化，请重新读取后确认")
	}
	return nil
}

func (s *CRMAISuggestionDraftService) resolveApply() crmAIApplyFn {
	if s.apply != nil {
		return s.apply
	}
	return func(ctx context.Context, confirmedBy uint, d *model.AIInsightDraft) (map[string]any, error) {
		ownerID := fmt.Sprintf("%d", confirmedBy)
		rem, err := NewFollowUpServiceWithStore(nil, repository.NewFollowUpReminderRepository()).Schedule(ctx,
			d.EntityID, ownerID, ReminderCustom, 24*time.Hour, &ScheduleOptions{
				Title: "AI建议跟进：" + d.EntityID, Description: "来自已确认的 AI 画像建议草稿 " + d.DraftID,
				Priority: PriorityNormal,
			})
		if err != nil {
			return nil, err
		}
		return map[string]any{"applied": true, "type": "followup_suggestion", "reminder_id": rem.ID, "customer_id": d.EntityID}, nil
	}
}

// crmAIDraftConfidence 取第一条有据结论的模型自报分数（不回退固定 0.8 常量）。
func crmAIDraftConfidence(res *CustomerAIResult) float64 {
	for _, sug := range res.Suggestions {
		if len(sug.Evidence) > 0 && sug.DegradeReason == "" {
			return sug.Confidence
		}
	}
	return 0
}

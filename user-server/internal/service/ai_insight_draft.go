package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type AIInsightDraftService struct {
	repo repository.AIInsightDraftRepository
}

func NewAIInsightDraftService() *AIInsightDraftService {
	return &AIInsightDraftService{repo: repository.NewAIInsightDraftRepository()}
}

type aiInsightProposedAction struct {
	Type       string   `json:"type,omitempty"`        // add_customer_tags | followup_suggestion
	CustomerID string   `json:"customer_id,omitempty"` // target customer, defaults to entity_id
	Tags       []string `json:"tags,omitempty"`
	Title      string   `json:"title,omitempty"`
	Content    string   `json:"content,omitempty"`
	DueInHours int      `json:"due_in_hours,omitempty"`
}

type aiInsightEvidence struct {
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

func rawBytes(raw json.RawMessage) []byte {
	b := bytesTrimSpace(raw)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func hasEvidence(evidenceJSON json.RawMessage) bool {
	b := rawBytes(evidenceJSON)
	if len(b) == 0 || string(b) == "[]" || string(b) == "{}" {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err == nil {
		return len(arr) > 0
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err == nil {
		return len(obj) > 0
	}
	return false
}

func normalizeJSONArray(raw json.RawMessage) json.RawMessage {
	b := rawBytes(raw)
	if len(b) == 0 {
		return json.RawMessage("[]")
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err == nil {
		out, _ := json.Marshal(arr)
		return out
	}
	return json.RawMessage("[]")
}

func normalizeJSONObject(raw json.RawMessage) json.RawMessage {
	b := rawBytes(raw)
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err == nil {
		out, _ := json.Marshal(obj)
		return out
	}
	return json.RawMessage("{}")
}

func parseProposedAction(raw json.RawMessage) (aiInsightProposedAction, bool) {
	b := rawBytes(raw)
	if len(b) == 0 || string(b) == "{}" {
		return aiInsightProposedAction{}, false
	}
	var a aiInsightProposedAction
	if err := json.Unmarshal(b, &a); err != nil {
		return aiInsightProposedAction{}, false
	}
	if strings.TrimSpace(a.Type) == "" {
		return aiInsightProposedAction{}, false
	}
	return a, true
}

func (s *AIInsightDraftService) sanitizeForDisplay(d *model.AIInsightDraft) *model.AIInsightDraft {
	if d == nil {
		return nil
	}
	cp := *d
	if !hasEvidence(cp.EvidenceJSON) {
		// 验收：无证据不展示可应用动作。
		cp.ProposedActionJSON = json.RawMessage("{}")
	}
	cp.EvidenceJSON = normalizeJSONArray(cp.EvidenceJSON)
	cp.SourceRefsJSON = normalizeJSONArray(cp.SourceRefsJSON)
	cp.ContentJSON = normalizeJSONObject(cp.ContentJSON)
	cp.AppliedActionJSON = normalizeJSONObject(cp.AppliedActionJSON)
	return &cp
}

func (s *AIInsightDraftService) Create(ctx context.Context, d *model.AIInsightDraft) error {
	if d == nil {
		return errors.New("草稿不能为空")
	}
	if d.DraftID == "" {
		d.DraftID = fmt.Sprintf("aid_%d", time.Now().UnixNano())
	}
	if d.Status == "" {
		d.Status = "draft"
	}
	d.EvidenceJSON = normalizeJSONArray(d.EvidenceJSON)
	d.SourceRefsJSON = normalizeJSONArray(d.SourceRefsJSON)
	d.ProposedActionJSON = normalizeJSONObject(d.ProposedActionJSON)
	d.ContentJSON = normalizeJSONObject(d.ContentJSON)
	d.AppliedActionJSON = normalizeJSONObject(d.AppliedActionJSON)
	if action, ok := parseProposedAction(d.ProposedActionJSON); ok && !hasEvidence(d.EvidenceJSON) {
		_ = action
		return errors.New("无证据不允许创建可应用动作草稿")
	}
	if d.Confidence < 0 {
		d.Confidence = 0
	}
	if d.Confidence > 1 {
		d.Confidence = 1
	}
	return s.repo.Create(ctx, d)
}

func (s *AIInsightDraftService) List(ctx context.Context, entityType, entityID, status string, page, pageSize int) ([]model.AIInsightDraft, int64, error) {
	items, total, err := s.repo.List(ctx, entityType, entityID, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.AIInsightDraft, 0, len(items))
	for i := range items {
		out = append(out, *s.sanitizeForDisplay(&items[i]))
	}
	return out, total, nil
}

func (s *AIInsightDraftService) Get(ctx context.Context, draftID string) (*model.AIInsightDraft, error) {
	item, err := s.repo.GetByDraftID(ctx, draftID)
	if err != nil {
		return nil, err
	}
	return s.sanitizeForDisplay(item), nil
}

func (s *AIInsightDraftService) Reject(ctx context.Context, draftID string, confirmedBy uint, reviewNote string) error {
	d, err := s.repo.GetByDraftID(ctx, draftID)
	if err != nil {
		return err
	}
	if d.Status != "draft" {
		return fmt.Errorf("草稿状态为%s，不可拒绝", d.Status)
	}
	return s.repo.UpdateReview(ctx, draftID, "rejected", &confirmedBy, reviewNote, "")
}

type AIInsightConfirmResult struct {
	Draft         *model.AIInsightDraft `json:"draft"`
	Applied       bool                  `json:"applied"`
	AppliedAction map[string]any        `json:"applied_action,omitempty"`
}

func (s *AIInsightDraftService) Confirm(ctx context.Context, draftID string, confirmedBy uint, reviewNote string) (*AIInsightConfirmResult, error) {
	d, err := s.repo.GetByDraftID(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if d.Status != "draft" {
		return nil, fmt.Errorf("草稿状态为%s，不可确认", d.Status)
	}
	if !hasEvidence(d.EvidenceJSON) {
		return nil, errors.New("无证据不允许确认可应用动作")
	}
	action, hasAction := parseProposedAction(d.ProposedActionJSON)
	applied := map[string]any{"applied": false}
	status := "confirmed"
	if hasAction {
		res, applyErr := s.applyAction(ctx, d, action, confirmedBy)
		if applyErr != nil {
			return nil, applyErr
		}
		applied = res
		if v, ok := res["applied"].(bool); ok && v {
			status = "applied"
		}
	}
	appliedJSON, _ := json.Marshal(applied)
	if err := s.repo.UpdateReview(ctx, draftID, status, &confirmedBy, reviewNote, string(appliedJSON)); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetByDraftID(ctx, draftID)
	if err != nil {
		return nil, err
	}
	return &AIInsightConfirmResult{Draft: s.sanitizeForDisplay(updated), Applied: status == "applied", AppliedAction: applied}, nil
}

func (s *AIInsightDraftService) applyAction(ctx context.Context, d *model.AIInsightDraft, action aiInsightProposedAction, confirmedBy uint) (map[string]any, error) {
	switch strings.TrimSpace(action.Type) {
	case "add_customer_tags":
		customerID := strings.TrimSpace(action.CustomerID)
		if customerID == "" && d.EntityType == "customer" {
			customerID = d.EntityID
		}
		if customerID == "" {
			return nil, errors.New("add_customer_tags 缺少 customer_id")
		}
		tags := cleanStringSlice(action.Tags)
		if len(tags) == 0 {
			return nil, errors.New("add_customer_tags 缺少 tags")
		}
		if err := NewCustomerService().AddTags(ctx, customerID, tags); err != nil {
			return nil, fmt.Errorf("写回客户标签失败: %w", err)
		}
		return map[string]any{"applied": true, "type": "add_customer_tags", "customer_id": customerID, "tags": tags}, nil
	case "followup_suggestion":
		customerID := strings.TrimSpace(action.CustomerID)
		if customerID == "" && d.EntityType == "customer" {
			customerID = d.EntityID
		}
		if customerID == "" {
			return nil, errors.New("followup_suggestion 缺少 customer_id")
		}
		title := strings.TrimSpace(action.Title)
		if title == "" {
			title = strings.TrimSpace(d.Title)
		}
		if title == "" {
			title = "AI跟进建议"
		}
		content := strings.TrimSpace(action.Content)
		if content == "" {
			content = "来自AI洞察草稿：" + d.DraftID
		}
		dueHours := action.DueInHours
		if dueHours <= 0 {
			dueHours = 24
		}
		ownerID := fmt.Sprintf("%d", confirmedBy)
		rem, err := NewFollowUpServiceWithStore(nil, repository.NewFollowUpReminderRepository()).Schedule(ctx, customerID, ownerID, ReminderCustom, time.Duration(dueHours)*time.Hour, &ScheduleOptions{
			Title:       title,
			Description: content,
			Priority:    PriorityNormal,
		})
		if err != nil {
			return nil, fmt.Errorf("写回跟进建议失败: %w", err)
		}
		return map[string]any{"applied": true, "type": "followup_suggestion", "customer_id": customerID, "reminder_id": rem.ID, "title": title}, nil
	default:
		return map[string]any{"applied": false, "type": action.Type, "reason": "unsupported_action"}, nil
	}
}

func cleanStringSlice(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// GenerateStub 只读生成草稿结构（不接真实LLM），用于PR-7最小可用验收。
// 只读来源：当前仅从客户主档提取可审计证据；无证据时不生成可应用动作。
func (s *AIInsightDraftService) GenerateStub(ctx context.Context, req AIInsightGenerateRequest) (*model.AIInsightDraft, error) {
	entityType := strings.TrimSpace(req.EntityType)
	entityID := strings.TrimSpace(req.EntityID)
	if entityType == "" {
		entityType = "customer"
	}
	if entityID == "" {
		return nil, errors.New("entity_id 不能为空")
	}
	insightType := strings.TrimSpace(req.InsightType)
	if insightType == "" {
		insightType = "profile"
	}
	evidence := make([]aiInsightEvidence, 0, 4)
	sourceRefs := make([]map[string]any, 0, 4)
	content := map[string]any{
		"summary":       "只读草稿stub：基于现有只读数据生成，需人工确认后才可写回",
		"insightType":   insightType,
		"entityType":    entityType,
		"entityId":      entityID,
		"generatedMode": "readonly_stub",
	}

	if entityType == "customer" {
		customer, err := repository.NewCustomerRepository().GetByID(ctx, entityID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if customer != nil {
			snippet := fmt.Sprintf("客户%s", strings.TrimSpace(customer.Name))
			if p := strings.TrimSpace(customer.Phone); p != "" {
				snippet += " / " + p
			}
			occurredAt := customer.UpdatedAt
			if occurredAt.IsZero() {
				occurredAt = customer.CreatedAt
			}
			evidence = append(evidence, aiInsightEvidence{
				Type: "customer_profile", Source: "customers", Ref: customer.ID,
				Snippet: snippet, OccurredAt: occurredAt.Format(time.RFC3339),
			})
			sourceRefs = append(sourceRefs, map[string]any{
				"type": "customer", "source": "customers", "id": customer.ID,
				"path": "/api/customers/" + customer.ID, "fields": []string{"name", "phone", "tags", "source_channel", "source_account_id", "updated_at"},
			})
			tags := model.GetCustomerTags(customer)
			if len(tags) > 0 {
				evidence = append(evidence, aiInsightEvidence{
					Type: "customer_tags", Source: "customers.tags", Ref: customer.ID,
					Snippet: "标签: " + strings.Join(tags, ","), OccurredAt: occurredAt.Format(time.RFC3339),
				})
			}
			if ch := strings.TrimSpace(customer.SourceChannel); ch != "" {
				sourceRefs = append(sourceRefs, map[string]any{
					"type": "source_channel", "source": "customers.source_channel", "id": customer.ID, "value": ch,
				})
			}
			content["customer"] = map[string]any{
				"id": customer.ID, "name": customer.Name, "company_id": customer.CompanyID,
				"source_channel": customer.SourceChannel, "source_account_id": customer.SourceAccountID, "tags": tags,
			}
		}
	} else {
		sourceRefs = append(sourceRefs, map[string]any{"type": entityType, "id": entityID, "source": "readonly_stub"})
	}

	proposedAction := map[string]any{}
	status := "draft"
	if len(evidence) == 0 {
		content["warning"] = "未找到可用只读证据，不生成可应用动作"
		content["action_policy"] = "no_evidence_no_action"
	} else {
		actionType := "followup_suggestion"
		switch insightType {
		case "segment", "profile":
			actionType = "add_customer_tags"
		}
		if actionType == "add_customer_tags" && entityType == "customer" {
			proposedAction = map[string]any{
				"type": "add_customer_tags", "customer_id": entityID,
				"tags":   []string{"AI洞察-" + insightType},
				"reason": "基于只读证据的草稿建议，确认后写回",
			}
		} else {
			proposedAction = map[string]any{
				"type": "followup_suggestion", "customer_id": entityID,
				"title":        "跟进AI洞察：" + entityID,
				"content":      "请根据证据复核后执行跟进",
				"due_in_hours": 24,
				"reason":       "基于只读证据的草稿建议，确认后写回",
			}
		}
		content["action_policy"] = "evidence_required_before_apply"
	}
	evidenceJSON, _ := json.Marshal(evidence)
	sourceRefsJSON, _ := json.Marshal(sourceRefs)
	contentJSON, _ := json.Marshal(content)
	proposedJSON, _ := json.Marshal(proposedAction)
	d := &model.AIInsightDraft{
		TenantID:           tenant.WriteTenantID(ctx),
		DraftID:            fmt.Sprintf("aid_%d", time.Now().UnixNano()),
		EntityType:         entityType,
		EntityID:           entityID,
		InsightType:        insightType,
		Title:              strings.TrimSpace(req.Title),
		ContentJSON:        contentJSON,
		EvidenceJSON:       evidenceJSON,
		SourceRefsJSON:     sourceRefsJSON,
		ProposedActionJSON: proposedJSON,
		Confidence:         0.5,
		Status:             status,
		GeneratedBy:        "stub_readonly",
		ModelName:          "stub-readonly",
		PromptVersion:      "pr7-stub-v2",
	}
	if d.Title == "" {
		d.Title = fmt.Sprintf("%s洞察草稿-%s", insightType, entityID)
	}
	if err := s.Create(ctx, d); err != nil {
		return nil, err
	}
	return s.sanitizeForDisplay(d), nil
}

type AIInsightGenerateRequest struct {
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	InsightType string `json:"insight_type"`
	Title       string `json:"title"`
}

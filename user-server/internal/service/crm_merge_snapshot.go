package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 合并影响快照 schema 版本。低于此版本或字段缺失的存量记录一律不可自动恢复。
const mergeImpactSchemaVersion = 2

// MergeFieldChange 单字段合并前后值。
type MergeFieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// MergeImpactSnapshot 成功合并前持久化的可恢复影响清单（CRM-07）。
// 全部落在 customer_merges.original_values（JSONB），不需要新增列或迁移。
type MergeImpactSnapshot struct {
	SchemaVersion      int                `json:"schema_version"`
	Recoverable        bool               `json:"recoverable"`
	Reason             string             `json:"reason,omitempty"`
	MergeID            string             `json:"merge_id"`
	RuleVersion        int                `json:"rule_version"`
	Operator           Operator           `json:"operator"`
	ConcurrencyVersion string             `json:"concurrency_version"` // 主对象合并前 updated_at
	Primary            MergeCustomerState `json:"primary"`
	Secondary          MergeCustomerState `json:"secondary"`
	FieldChanges       []MergeFieldChange `json:"field_changes"`
	Relations          MergeRelations     `json:"relations"`
	Uncaptured         []string           `json:"uncaptured,omitempty"`
}

// MergeCustomerState 主/次对象合并前不可变字段副本。
type MergeCustomerState struct {
	ID              string `json:"id"`
	UnifiedID       string `json:"unified_id"`
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	SourceAccountID string `json:"source_account_id"`
	Industry        string `json:"industry"`
	MergedIntoID    string `json:"merged_into_id"`
}

// MergeRelations 关联归属计数与身份连通组（用于撤销前的新写入/链式合并判定）。
type MergeRelations struct {
	Events       int64    `json:"events"`
	Sessions     int64    `json:"sessions"`
	DNC          int64    `json:"dnc"`
	LinkedOneIDs []string `json:"linked_one_ids"`
}

// buildMergeImpactSnapshot 在合并前读取主/次对象与关联归属，生成快照。
// 读取失败直接报错，禁止用空快照冒充可恢复。
func (s *CustomerDedupeService) buildMergeImpactSnapshot(ctx context.Context, primary, secondary *model.Customer, mergeID string) (*MergeImpactSnapshot, error) {
	rules, err := s.ruleSvc.GetRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取规则版本失败: %w", err)
	}
	version := 0
	if rules != nil {
		version = rules.Version
	}
	snap := &MergeImpactSnapshot{
		SchemaVersion: mergeImpactSchemaVersion, Recoverable: true, MergeID: mergeID,
		RuleVersion: version, Operator: OperatorFromContext(ctx),
		Primary:   customerMergeState(primary),
		Secondary: customerMergeState(secondary),
		// 合并会 by design 更新主对象 updated_at；并发基线在合并事务内合并后回填
		// （见 Merge 中的 refreshMergeSnapshotVersion），否则撤销会被误判为「新写入冲突」。
		FieldChanges: plannedFieldChanges(primary, secondary),
	}
	eventRows, err := repository.NewCustomerEventRepository().GetByCustomerID(ctx, secondary.ID, 10000)
	if err != nil {
		return nil, fmt.Errorf("快照读取事件失败: %w", err)
	}
	snap.Relations.Events = int64(len(eventRows))
	sessionRepo := repository.NewCustomerSessionRepository()
	if sessionRepo.Available() {
		sessions, err := sessionRepo.GetByOneID(ctx, secondary.UnifiedID)
		if err != nil {
			return nil, fmt.Errorf("快照读取会话失败: %w", err)
		}
		snap.Relations.Sessions = int64(len(sessions))
	}
	dncRows, err := repository.NewCustomerDoNotContactRepository(nil).ListByOneID(ctx, secondary.UnifiedID)
	if err != nil {
		return nil, fmt.Errorf("快照读取退订标志失败: %w", err)
	}
	snap.Relations.DNC = int64(len(dncRows))
	linked, err := s.consentLinkedOneIDs(ctx, secondary.UnifiedID)
	if err != nil {
		return nil, fmt.Errorf("快照读取许可身份关系失败: %w", err)
	}
	snap.Relations.LinkedOneIDs = linked
	// 明确列出快照未覆盖的表，避免把部分快照冒充完整快照。
	snap.Uncaptured = []string{"csat_surveys", "clues", "script_exposure_logs", "customer_channels"}
	return snap, nil
}

func customerMergeState(c *model.Customer) MergeCustomerState {
	return MergeCustomerState{
		ID: c.ID, UnifiedID: c.UnifiedID, Name: c.Name, Phone: c.Phone, Email: c.Email,
		SourceAccountID: c.SourceAccountID, Industry: c.Industry, MergedIntoID: c.MergedIntoID,
	}
}

// plannedFieldChanges 按 mergeCustomerFields 的补全口径列出前后值，供权限预览与撤销核对。
func plannedFieldChanges(primary, secondary *model.Customer) []MergeFieldChange {
	out := make([]MergeFieldChange, 0, 6)
	add := func(field, before, after string) {
		if before != after {
			out = append(out, MergeFieldChange{Field: field, Before: before, After: after})
		}
	}
	if secondary.Phone != "" && primary.Phone == "" {
		add("phone", primary.Phone, secondary.Phone)
	} else {
		add("phone", primary.Phone, primary.Phone)
	}
	if primary.UnifiedID == "" {
		add("unified_id", primary.UnifiedID, secondary.UnifiedID)
	}
	if secondary.Email != "" && primary.Email == "" {
		add("email", primary.Email, secondary.Email)
	}
	if secondary.WechatOpenID != "" && primary.WechatOpenID == "" {
		add("wechat_open_id", primary.WechatOpenID, secondary.WechatOpenID)
	}
	if secondary.DouyinOpenID != "" && primary.DouyinOpenID == "" {
		add("douyin_open_id", primary.DouyinOpenID, secondary.DouyinOpenID)
	}
	if secondary.XiaohongshuID != "" && primary.XiaohongshuID == "" {
		add("xiaohongshu_id", primary.XiaohongshuID, secondary.XiaohongshuID)
	}
	return out
}

// parseMergeImpactSnapshot 解析快照；存量旧审计明确标记不可自动恢复，不伪造快照。
func parseMergeImpactSnapshot(rec *model.CustomerMerge) (*MergeImpactSnapshot, error) {
	if rec == nil {
		return nil, errors.New("合并记录不存在")
	}
	var snap MergeImpactSnapshot
	if err := json.Unmarshal([]byte(rec.OriginalValues), &snap); err != nil {
		return &MergeImpactSnapshot{SchemaVersion: 0, Recoverable: false, MergeID: rec.MergeID,
			Reason: "历史快照不是可解析的结构化影响清单，不可自动恢复"}, nil
	}
	if snap.SchemaVersion < mergeImpactSchemaVersion || snap.Primary.ID == "" || snap.Secondary.ID == "" {
		return &MergeImpactSnapshot{SchemaVersion: snap.SchemaVersion, Recoverable: false, MergeID: rec.MergeID,
			Reason: "存量合并无完整快照（缺少版本/主次对象），不可自动恢复"}, nil
	}
	return &snap, nil
}

// MergeUndoApplier 事务内的逆操作接缝。生产默认未装配可恢复仓储时拒绝撤销并给人工线索；
// 测试/后续获得仓储授权后可注入实现，避免此处越界直连 DB。
type MergeUndoApplier interface {
	Apply(ctx context.Context, snap *MergeImpactSnapshot) error
}

// SetMergeUndoApplier 注入逆操作实现（Router 装配点后续可用）。
func (s *CustomerDedupeService) SetMergeUndoApplier(applier MergeUndoApplier) {
	s.undoApplier = applier
}

// MergeUndoResult 撤销结果/冲突提示。
type MergeUndoResult struct {
	MergeID     string `json:"merge_id"`
	Undone      bool   `json:"undone"`
	Conflict    string `json:"conflict,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// PreviewUndo 按权限预览 07 快照影响与可恢复性，不写库。
func (s *CustomerDedupeService) PreviewUndo(ctx context.Context, scope CustomerDataScope, mergeID string) (*MergeUndoResult, *MergeImpactSnapshot, error) {
	rec, err := s.mergeRepo.GetByMergeID(ctx, mergeID)
	if err != nil || rec == nil {
		return nil, nil, fmt.Errorf("合并记录不存在: %w", err)
	}
	snap, err := parseMergeImpactSnapshot(rec)
	if err != nil {
		return nil, nil, err
	}
	// 权限校验只针对可恢复的新快照：存量记录同样受范围约束，但无完整快照时
	// 必须优先给出「不可自动恢复」的明确标记，而不是因历史对象已删除误报 403。
	if s.permission != nil && snap.Recoverable {
		cust, err := s.customers.GetByID(ctx, rec.PrimaryID)
		if err != nil || cust == nil || !s.permission.CanAccess(scope, cust) {
			return nil, nil, ErrCRMCustomerForbidden
		}
	}
	result := &MergeUndoResult{MergeID: mergeID, Recoverable: snap.Recoverable}
	if !snap.Recoverable {
		result.Conflict, result.Hint = "不可自动恢复", snap.Reason+"；请人工核对原始合并记录"
	}
	return result, snap, nil
}

// UndoMerge 带冲突检查的人工撤销入口（CRM-08）：先展示影响，再校验新写入/链式合并/版本冲突，
// 确认后在单一事务内执行逆操作并写撤销审计；重复撤销幂等；许可连通关系保留，绝不因拆回放宽。
func (s *CustomerDedupeService) UndoMerge(ctx context.Context, scope CustomerDataScope, mergeID, reason string) (*MergeUndoResult, error) {
	result, snap, err := s.PreviewUndo(ctx, scope, mergeID)
	if err != nil {
		return nil, err
	}
	if result.Conflict == "已撤销" {
		return result, nil
	}
	if !snap.Recoverable {
		return result, nil
	}
	// 幂等：已有撤销审计则直接返回原结果，不重复逆操作。
	audits, err := s.mergeRepo.ListByCustomer(ctx, snap.Primary.ID, 200)
	if err != nil {
		return nil, fmt.Errorf("读取撤销审计失败: %w", err)
	}
	for _, row := range audits {
		if row.Action == "undo" && stringsContainsUndoRef(row.OriginalValues, mergeID) {
			return &MergeUndoResult{MergeID: mergeID, Undone: true, Recoverable: true, Conflict: "已撤销", Hint: "重复撤销幂等返回"}, nil
		}
	}
	if conflict, hint := s.undoConflicts(ctx, snap); conflict != "" {
		return &MergeUndoResult{MergeID: mergeID, Recoverable: true, Conflict: conflict, Hint: hint}, nil
	}
	if s.undoApplier == nil {
		return &MergeUndoResult{MergeID: mergeID, Recoverable: true,
			Conflict: "缺少可恢复仓储实现", Hint: "未装配逆操作仓储，禁止伪造撤销；请人工按快照处置"}, nil
	}
	op := OperatorFromContext(ctx)
	err = s.customers.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.undoApplier.Apply(txCtx, snap); err != nil {
			return fmt.Errorf("执行撤销逆操作失败: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{
			"undo_of": mergeID, "reason": reason, "operator": op.Username,
			"preserved_consent_links": true,
		})
		return s.mergeRepo.Create(txCtx, &model.CustomerMerge{
			MergeID: fmt.Sprintf("cu_%d", time.Now().UnixNano()), PrimaryID: snap.Primary.ID,
			SecondaryID: snap.Secondary.ID, Action: "undo", OriginalValues: string(payload),
			Reason: reason, OperatorID: op.UserID, OperatorName: op.Username, RuleName: "undo",
		})
	})
	if err != nil {
		return nil, fmt.Errorf("撤销合并失败: %w", err)
	}
	s.writeAudit(ctx, snap.Primary.ID, snap.Secondary.ID, op.Username, "撤销合并:"+reason)
	return &MergeUndoResult{MergeID: mergeID, Undone: true, Recoverable: true}, nil
}

// undoConflicts 检查合并后新写入、链式合并与版本冲突；任一项命中都显式拒绝。
func (s *CustomerDedupeService) undoConflicts(ctx context.Context, snap *MergeImpactSnapshot) (string, string) {
	primary, err := s.customers.GetByID(ctx, snap.Primary.ID)
	if err != nil || primary == nil {
		return "主对象不存在或不可读", "请人工核对主客户是否已删除或迁移"
	}
	if primary.UpdatedAt.UTC().Format(time.RFC3339Nano) != snap.ConcurrencyVersion {
		return "合并后主对象有新写入（版本冲突）", "停止撤销并人工比对合并后的更新，避免覆盖新数据"
	}
	secondary, err := s.customers.GetByID(ctx, snap.Secondary.ID)
	if err != nil {
		return "次对象状态不可读", "请人工核对次客户状态"
	}
	if secondary != nil {
		return "次对象已重新出现", "请人工确认是否已存在同 ID 客户，避免重复恢复"
	}
	events, err := repository.NewCustomerEventRepository().GetByCustomerID(ctx, snap.Primary.ID, 10000)
	if err != nil {
		return "事件计数不可读", "请人工核对事件归属后处置"
	}
	if int64(len(events)) != snap.Relations.Events {
		return "合并后有新事件写入", "停止撤销并人工迁移合并后新增事件"
	}
	linked, err := s.consentLinkedOneIDs(ctx, snap.Primary.UnifiedID)
	if err != nil {
		return "许可关系不可读", "请人工核对 OneID 连通组"
	}
	allowed := map[string]bool{snap.Primary.UnifiedID: true, snap.Secondary.UnifiedID: true}
	for _, id := range linked {
		if !allowed[id] {
			return "存在链式合并或额外许可关联", fmt.Sprintf("连通 OneID 含第三方 %s，请人工拆分并保留否定许可", id)
		}
	}
	return "", ""
}

func stringsContainsUndoRef(raw, mergeID string) bool {
	var payload struct {
		UndoOf string `json:"undo_of"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return false
	}
	return payload.UndoOf == mergeID
}

// consentLinkedOneIDs 经 repository seam 读取许可身份连通组。
// 仓库构造在 Service 构造函数中完成，本文件不直接取 DB 句柄（遵守五层架构）。
func (s *CustomerDedupeService) consentLinkedOneIDs(ctx context.Context, oneID string) ([]string, error) {
	if s == nil || s.consentRepo == nil {
		return nil, errors.New("许可身份仓储未初始化")
	}
	return s.consentRepo.LinkedOneIDs(ctx, oneID)
}

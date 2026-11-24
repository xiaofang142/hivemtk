package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// LeadClueArchiveInput US2 归档输入：线索信息 + 用户信息 + 证据引用。
type LeadClueArchiveInput struct {
	ClueID           string
	MessageID        string
	JudgementID      string
	ChannelAccountID uint
	SourceChannel    string
	SourceAccountID  string
	CustomerID       string
	Identifiers      identity.Identifiers
	Name             string
	Industry         string
	OwnerUserID      uint
	UserInfo         model.JSONPayload
	SourceHitIDs     model.JSONStrings
	Role             string
	Summary          string
	EvidenceJSON     string
}

// LeadClueArchiveService 线索归档 Service（FR-015 / T041）。
//
// 严格对接 Phase 3 T031 已建的 ClueArchiveService：
//   - 客户关联/OneID/负责人推送全部走 ArchiveClueAndLinkCustomer；
//   - 通知出口由 Router 通过 SetNotifier 注入（T042 的 LeadNotifyService 实现）；
//   - 本服务只负责线索自身的归档字段（user_info/source_hit_ids/role/status/archived_at）。
//
// 绝不自建第二套客户关联或推送逻辑。
type LeadClueArchiveService struct {
	clues   repository.ClueRepository
	archive *ClueArchiveService
}

func NewLeadClueArchiveService() *LeadClueArchiveService {
	return &LeadClueArchiveService{
		clues:   repository.NewClueRepositoryDefault(),
		archive: NewClueArchiveService(),
	}
}

func NewLeadClueArchiveServiceWith(clues repository.ClueRepository, archive *ClueArchiveService) *LeadClueArchiveService {
	return &LeadClueArchiveService{clues: clues, archive: archive}
}

// Archive 归档线索并关联客户。顺序：先写线索归档字段，再调 Phase 3 客户侧关联；
// 客户侧失败返回错误（fail-visible），不把半成品标记为"已通知"。
func (s *LeadClueArchiveService) Archive(ctx context.Context, in LeadClueArchiveInput) (*ArchiveClueResult, error) {
	if s == nil || s.archive == nil || s.clues == nil {
		return nil, errors.New("归档服务未装配")
	}
	if in.ClueID == "" {
		return nil, errors.New("clue_id 不能为空")
	}
	clue, err := s.clues.GetByID(ctx, in.ClueID)
	if err != nil {
		return nil, fmt.Errorf("读取待归档线索失败: %w", err)
	}
	if clue == nil {
		return nil, fmt.Errorf("待归档线索不存在: %s", in.ClueID)
	}

	role := normalizeLeadRole(in.Role)
	userInfo, _ := json.Marshal(in.UserInfo)
	if len(userInfo) == 0 || string(userInfo) == "null" {
		userInfo = []byte("{}")
	}
	now := time.Now().Unix()
	updates := map[string]any{
		"user_info":      string(userInfo),
		"source_hit_ids": in.SourceHitIDs,
		"lead_role":      role,
		"status":         "archived",
		"archived_at":    now,
	}
	if in.MessageID != "" {
		updates["message_id"] = in.MessageID
	}
	if in.Summary != "" {
		updates["summary"] = in.Summary
	}
	if in.SourceChannel != "" {
		updates["source_channel"] = in.SourceChannel
	}
	if in.SourceAccountID != "" {
		updates["source_account_id"] = in.SourceAccountID
	}
	if err := s.clues.UpdateByID(ctx, in.ClueID, updates); err != nil {
		return nil, fmt.Errorf("写入线索归档字段失败: %w", err)
	}

	result, err := s.archive.ArchiveClueAndLinkCustomer(ctx, ArchiveClueInput{
		ClueID:          in.ClueID,
		CustomerID:      in.CustomerID,
		Identifiers:     in.Identifiers,
		Name:            in.Name,
		Industry:        in.Industry,
		SourceAccountID: in.SourceAccountID,
		SourceChannel:   in.SourceChannel,
		OwnerUserID:     in.OwnerUserID,
		Summary:         in.Summary,
		EvidenceJSON:    in.EvidenceJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("Phase 3 客户侧归档失败: %w", err)
	}

	// 客户侧新建/识别出的 OneID 回写线索，保证双向可追溯。
	if result != nil && result.CustomerID != "" && result.CustomerID != in.CustomerID {
		if err := s.clues.UpdateByID(ctx, in.ClueID, map[string]any{"customer_id": result.CustomerID}); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("clue_id", in.ClueID).
				Str("customer_id", result.CustomerID).Msg("[lead-archive] 客户ID回写线索失败")
		}
	}
	return result, nil
}

// SetNotifier 由 Router 装配点注入 T042 的负责人推送出口（无内联闭包）。
func (s *LeadClueArchiveService) SetNotifier(n CustomerOwnerNotifier) {
	if s != nil && s.archive != nil && n != nil {
		s.archive.SetNotifier(n)
	}
}

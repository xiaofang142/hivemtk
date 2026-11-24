package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ArchiveClueInput US2 归档时传给客户侧的对接入参（T031 ↔ T041 契约）。
// US2 自己负责线索状态/归档时间/通知路由；本服务只负责客户侧关联与负责人推送。
type ArchiveClueInput struct {
	ClueID          string               `json:"clue_id"`
	CustomerID      string               `json:"customer_id,omitempty"` // 已有关联时直接传入
	Identifiers     identity.Identifiers `json:"identifiers,omitempty"`
	Name            string               `json:"name,omitempty"`
	Industry        string               `json:"industry,omitempty"`
	SourceAccountID string               `json:"source_account_id,omitempty"`
	SourceChannel   string               `json:"source_channel,omitempty"`
	OwnerUserID     uint                 `json:"owner_user_id,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	EvidenceJSON    string               `json:"evidence_json,omitempty"`
}

// ArchiveClueResult 客户侧关联结果。
type ArchiveClueResult struct {
	CustomerID string `json:"customer_id"`
	Linked     bool   `json:"linked"`
	Notified   bool   `json:"notified"`
}

// CustomerOwnerNotifier 负责人推送出口。US2 的通知路由服务（T042）实现此接口，
// 由 Router 装配点注入；未注入时只记日志，不阻断归档主流程。
type CustomerOwnerNotifier interface {
	NotifyOwner(ctx context.Context, in ArchiveClueInput, customerID string) error
}

// ClueArchiveService 线索归档后关联客户并推送负责人（FR-008）。
//
// 本服务刻意不引用 clue 模块的仓储/服务：US2 的 T041 在归档成功事务之后调用
// ArchiveClueAndLinkCustomer，线索自身的 archived_at/status 由 US2 写。
type ClueArchiveService struct {
	identitySvc *CustomerIdentityService
	customers   repository.CustomerRepository
	auditRepo   repository.OperationLogRepository
	notifier    CustomerOwnerNotifier
}

func NewClueArchiveService() *ClueArchiveService {
	return &ClueArchiveService{
		identitySvc: NewCustomerIdentityService(),
		customers:   repository.NewCustomerRepository(),
		auditRepo:   repository.NewOperationLogRepository(),
	}
}

// SetNotifier setter 注入通知出口（Router 装配点调用，无内联闭包）。
func (s *ClueArchiveService) SetNotifier(n CustomerOwnerNotifier) {
	if n != nil {
		s.notifier = n
	}
}

// ArchiveClueAndLinkCustomer 客户侧关联 + 负责人推送桩。
// 无 customer_id 且无可用身份标识时返回明确错误（fail-visible），不猜测归属。
func (s *ClueArchiveService) ArchiveClueAndLinkCustomer(ctx context.Context, in ArchiveClueInput) (*ArchiveClueResult, error) {
	var customer *model.Customer
	var err error
	if in.CustomerID != "" {
		customer, err = s.customers.GetByID(ctx, in.CustomerID)
		if err != nil || customer == nil {
			return nil, fmt.Errorf("关联客户不存在: %w", err)
		}
	} else {
		if !HasAnyIdentifier(in.Identifiers) {
			return nil, errors.New("归档线索缺少 customer_id 或可用身份标识")
		}
		customer, err = s.identitySvc.IdentifyOrCreate(ctx, in.Identifiers)
		if err != nil {
			return nil, fmt.Errorf("识别/创建归档客户失败: %w", err)
		}
	}

	// 仅在客户侧补齐缺口字段，绝不覆盖已有主数据。
	changed := false
	if customer.Name == "" && in.Name != "" {
		customer.Name = in.Name
		changed = true
	}
	if customer.Industry == "" && in.Industry != "" {
		customer.Industry = in.Industry
		changed = true
	}
	if customer.SourceAccountID == "" && in.SourceAccountID != "" {
		customer.SourceAccountID = in.SourceAccountID
		changed = true
	}
	if customer.SourceChannel == "" && in.SourceChannel != "" {
		customer.SourceChannel = in.SourceChannel
		changed = true
	}
	if customer.OwnerUserID == nil && in.OwnerUserID > 0 {
		customer.OwnerUserID = &in.OwnerUserID
		changed = true
	}
	if changed {
		if err := s.customers.Update(ctx, customer); err != nil {
			return nil, fmt.Errorf("补齐归档客户字段失败: %w", err)
		}
	}

	result := &ArchiveClueResult{CustomerID: customer.ID, Linked: true}
	s.writeAudit(ctx, in, customer.ID)

	if s.notifier != nil {
		if err := s.notifier.NotifyOwner(ctx, in, customer.ID); err != nil {
			logger.Warnf("[clue-archive] 负责人推送失败 clue=%s customer=%s: %v", in.ClueID, customer.ID, err)
		} else {
			result.Notified = true
		}
	} else {
		logger.Infof("[clue-archive] 未装配负责人推送出口，仅完成客户关联 clue=%s customer=%s", in.ClueID, customer.ID)
	}
	return result, nil
}

func (s *ClueArchiveService) writeAudit(ctx context.Context, in ArchiveClueInput, customerID string) {
	if s.auditRepo == nil {
		return
	}
	detail, _ := json.Marshal(map[string]any{
		"clue_id": in.ClueID, "customer_id": customerID,
		"owner_user_id": in.OwnerUserID, "summary": in.Summary,
		"evidence": in.EvidenceJSON,
	})
	_ = s.auditRepo.Create(ctx, &model.OperationLog{
		UserID: in.OwnerUserID, Action: "clue_archive_link_customer",
		Module: "crm", Resource: "customer", ResourceID: customerID,
		Detail: string(detail),
	})
}

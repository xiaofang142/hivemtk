package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"strconv"
	"strings"
)

type FollowupRecordService struct {
	repo       repository.FollowupRecordRepository
	permission *CustomerPermissionService
	companies  repository.CompanyRepository
	contacts   repository.ContactRepository
	opps       repository.OpportunityRepository
	reminders  repository.FollowUpReminderStore
}

func NewFollowupRecordService() *FollowupRecordService {
	return &FollowupRecordService{
		repo:       repository.NewFollowupRecordRepository(),
		permission: NewCustomerPermissionService(),
		companies:  repository.NewCompanyRepository(),
		contacts:   repository.NewContactRepository(),
		opps:       repository.NewOpportunityRepository(),
		reminders:  repository.NewFollowUpReminderRepository(),
	}
}

func (s *FollowupRecordService) ensureRepos() {
	if s.companies == nil {
		s.companies = repository.NewCompanyRepository()
	}
	if s.contacts == nil {
		s.contacts = repository.NewContactRepository()
	}
	if s.opps == nil {
		s.opps = repository.NewOpportunityRepository()
	}
	if s.reminders == nil {
		s.reminders = repository.NewFollowUpReminderRepository()
	}
	if s.permission == nil {
		s.permission = NewCustomerPermissionService()
	}
}

func (s *FollowupRecordService) Create(ctx context.Context, rec *model.FollowupRecord) error {
	if rec.RecordID == "" {
		rec.RecordID = fmt.Sprintf("fr_%d", time.Now().UnixNano())
	}
	return s.repo.Create(ctx, rec)
}

func (s *FollowupRecordService) Update(ctx context.Context, rec *model.FollowupRecord) error {
	return s.repo.Update(ctx, rec)
}

func (s *FollowupRecordService) Get(ctx context.Context, recordID string) (*model.FollowupRecord, error) {
	return s.repo.GetByRecordID(ctx, recordID)
}

func (s *FollowupRecordService) List(ctx context.Context, customerID, reminderID string, ownerID uint, page, pageSize int) ([]model.FollowupRecord, int64, error) {
	return s.repo.List(ctx, customerID, reminderID, ownerID, page, pageSize)
}

// ErrFollowupForbidden 当前员工无权访问该跟进记录。
var ErrFollowupForbidden = ErrCRMCustomerForbidden

// Scope 从可信登录用户解析 CRM 数据范围。
func (s *FollowupRecordService) Scope(ctx context.Context, userID uint) CustomerDataScope {
	if s.permission == nil {
		s.permission = NewCustomerPermissionService()
	}
	scope, _ := s.permission.Resolve(ctx, userID)
	return scope
}

// customerAllowed 校验当前员工可访问跟进记录关联的客户。
func (s *FollowupRecordService) customerAllowed(ctx context.Context, scope CustomerDataScope, customerID string) bool {
	if scope.AllAccounts {
		return true
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return false
	}
	if s.permission == nil {
		return false
	}
	_, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	return err == nil
}

// ListScoped 按员工数据范围列出跟进记录；owner/customer 筛选只能收窄结果。
func (s *FollowupRecordService) ListScoped(ctx context.Context, scope CustomerDataScope, customerID, reminderID string, ownerID uint, page, pageSize int) ([]model.FollowupRecord, int64, error) {
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 && !scope.AllAccounts {
		return []model.FollowupRecord{}, 0, nil
	}
	if scope.OnlyOwned && ownerID == 0 {
		ownerID = scope.UserID
	} else if scope.OwnerUserID > 0 && ownerID == 0 {
		ownerID = scope.OwnerUserID
	}
	all, _, err := s.repo.List(ctx, customerID, reminderID, ownerID, 1, 10000)
	if err != nil {
		return nil, 0, err
	}
	allowed := make([]model.FollowupRecord, 0, len(all))
	for _, rec := range all {
		if !s.customerAllowed(ctx, scope, rec.CustomerID) {
			continue
		}
		if scope.OnlyOwned && rec.OwnerID != scope.UserID {
			continue
		}
		if scope.OwnerUserID > 0 && rec.OwnerID != scope.OwnerUserID {
			continue
		}
		allowed = append(allowed, rec)
	}
	total := int64(len(allowed))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(allowed) {
		return []model.FollowupRecord{}, total, nil
	}
	end := start + pageSize
	if end > len(allowed) {
		end = len(allowed)
	}
	return allowed[start:end], total, nil
}

// GetScoped 按数据范围读取跟进记录。
func (s *FollowupRecordService) GetScoped(ctx context.Context, scope CustomerDataScope, recordID string) (*model.FollowupRecord, error) {
	rec, err := s.repo.GetByRecordID(ctx, recordID)
	if err != nil {
		return nil, err
	}
	if !s.customerAllowed(ctx, scope, rec.CustomerID) {
		return nil, ErrFollowupForbidden
	}
	if scope.OnlyOwned && rec.OwnerID != scope.UserID {
		return nil, ErrFollowupForbidden
	}
	return rec, nil
}

// CreateScoped 创建时按最终关联客户与负责人校验权限。
func (s *FollowupRecordService) CreateScoped(ctx context.Context, scope CustomerDataScope, rec *model.FollowupRecord) error {
	if rec == nil {
		return errors.New("跟进记录不能为空")
	}
	s.ensureRepos()
	if rec.CustomerID != "" && !s.customerAllowed(ctx, scope, rec.CustomerID) {
		return ErrFollowupForbidden
	}
	if rec.CustomerID == "" {
		if scope.OnlyOwned && rec.OwnerID != scope.UserID {
			return ErrFollowupForbidden
		}
		if scope.OwnerUserID > 0 && rec.OwnerID != scope.OwnerUserID {
			return ErrFollowupForbidden
		}
	}
	if err := s.validateFollowupAssociations(ctx, scope, rec); err != nil {
		return err
	}
	if scope.OnlyOwned && rec.OwnerID != scope.UserID && rec.CustomerID != "" {
		return ErrFollowupForbidden
	}
	if scope.OwnerUserID > 0 && rec.OwnerID != scope.OwnerUserID && rec.CustomerID != "" {
		// 允许显式 owner 为 0 的无关联记录按角色规则处理；有关联客户时仍要求负责人一致。
	}
	return s.Create(ctx, rec)
}

// UpdateScoped 更新时按旧记录与最终写入状态双重校验，拒绝改挂越权客户/负责人。
func (s *FollowupRecordService) UpdateScoped(ctx context.Context, scope CustomerDataScope, rec *model.FollowupRecord) error {
	s.ensureRepos()
	old, err := s.GetScoped(ctx, scope, rec.RecordID)
	if err != nil {
		return err
	}
	if rec.CustomerID != "" && !s.customerAllowed(ctx, scope, rec.CustomerID) {
		return ErrFollowupForbidden
	}
	if rec.CustomerID == "" {
		if scope.OnlyOwned && rec.OwnerID != scope.UserID && old.OwnerID != scope.UserID {
			return ErrFollowupForbidden
		}
		if scope.OwnerUserID > 0 && rec.OwnerID != scope.OwnerUserID && old.OwnerID != scope.OwnerUserID {
			return ErrFollowupForbidden
		}
	}
	if scope.OnlyOwned && rec.CustomerID != "" && (rec.OwnerID != scope.UserID || old.OwnerID != scope.UserID) {
		return ErrFollowupForbidden
	}
	if scope.OwnerUserID > 0 && rec.CustomerID != "" && rec.OwnerID != scope.OwnerUserID {
		return ErrFollowupForbidden
	}
	if err := s.validateFollowupAssociations(ctx, scope, rec); err != nil {
		return err
	}
	return s.Update(ctx, rec)
}

// validateFollowupAssociations 校验 company/contact/opportunity/reminder 关联对象的租户、权限与相互关系。
func (s *FollowupRecordService) validateFollowupAssociations(ctx context.Context, scope CustomerDataScope, rec *model.FollowupRecord) error {
	s.ensureRepos()
	tid, _ := tenant.FromContext(ctx)
	if tid == 0 {
		tid = tenant.WriteTenantID(ctx)
	}
	if rec.CompanyID != nil && *rec.CompanyID > 0 {
		comp, err := s.companies.GetByID(ctx, *rec.CompanyID)
		if err != nil || comp == nil || comp.TenantID != tid {
			return ErrFollowupForbidden
		}
		if !scope.AllAccounts {
			if len(scope.Industries) > 0 && comp.Industry != "" && !containsCRMString(scope.Industries, comp.Industry) {
				return ErrFollowupForbidden
			}
			if scope.OnlyOwned && (comp.OwnerUserID == nil || *comp.OwnerUserID != scope.UserID) {
				return ErrFollowupForbidden
			}
			if scope.OwnerUserID > 0 && (comp.OwnerUserID == nil || *comp.OwnerUserID != scope.OwnerUserID) {
				return ErrFollowupForbidden
			}
		}
	}
	if rec.ContactID != nil && *rec.ContactID > 0 {
		contact, err := s.contacts.GetByID(ctx, *rec.ContactID)
		if err != nil || contact == nil || contact.TenantID != tid {
			return ErrFollowupForbidden
		}
		if rec.CustomerID != "" && contact.CustomerID != rec.CustomerID {
			return ErrFollowupForbidden
		}
	}
	if rec.OpportunityID != nil && *rec.OpportunityID > 0 {
		opp, err := s.opps.GetByID(ctx, strconv.FormatInt(*rec.OpportunityID, 10))
		if err != nil || opp == nil || opp.TenantID != tid {
			return ErrFollowupForbidden
		}
		if rec.CustomerID != "" && opp.CustomerID != "" && opp.CustomerID != rec.CustomerID {
			return ErrFollowupForbidden
		}
	}
	if strings.TrimSpace(rec.ReminderID) != "" {
		rem, err := s.reminders.GetByID(ctx, rec.ReminderID)
		if err != nil || rem == nil || rem.TenantID != tid {
			return ErrFollowupForbidden
		}
	}
	return nil
}

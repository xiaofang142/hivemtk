package service

import (
	"context"
	"errors"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// CRM360View 客户 360 视图（US1）：在既有 Customer360Service 的会话/消息/线索/
// 订单/公司关系之上，叠加补缺实体与合并审计。
type CRM360View struct {
	CustomerID       string                   `json:"customer_id"`
	Profile          *Customer360DTO          `json:"profile"`
	Contacts         []*model.Contact         `json:"contacts"`
	Suppliers        []*model.Supplier        `json:"suppliers"`
	ServiceProviders []*model.ServiceProvider `json:"service_providers"`
	MergeHistory     []*model.CustomerMerge   `json:"merge_history"`
}

// CRMIndustryCrossItem 跨账号行业交叉检索的一行（与 controller/cross_account_search.go
// 的出参口径一致：手机号/邮箱默认脱敏）。
type CRMIndustryCrossItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Phone           string `json:"phone,omitempty"`
	Email           string `json:"email,omitempty"`
	Industry        string `json:"industry,omitempty"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	OwnerUserID     string `json:"owner_user_id,omitempty"`
}

// CRMIndustryCrossRow 行业 × 账号交叉统计。
type CRMIndustryCrossRow struct {
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	Count           int64  `json:"count"`
}

// CRMIndustryCrossResult 跨账号行业交叉检索结果。
type CRMIndustryCrossResult struct {
	Items    []CRMIndustryCrossItem  `json:"items"`
	Total    int64                   `json:"total"`
	CrossTab []CRMIndustryCrossRow   `json:"cross_tab"`
	Scope    CrossAccountSearchScope `json:"scope"`
}

// CRM360Service 360 视图 + 跨账号行业交叉检索（FR-006）。
//
// 勘察结论：Customer360Service / CrossAccountSearchService 已存在。本服务是薄编排层，
// 复用二者的既有语义（含脱敏与 scope 出参），并叠加 T023 数据权限与 T018 行业字段。
type CRM360Service struct {
	profile    *Customer360Service
	permission *CustomerPermissionService
	contacts   repository.ContactRepository
	suppliers  repository.SupplierRepository
	providers  repository.ServiceProviderRepository
	merges     repository.CustomerMergeRepository
}

func NewCRM360Service() *CRM360Service {
	return &CRM360Service{
		profile:    nil, // 延迟构造：既有 Customer360Service 需要显式 clueRepo 装配
		permission: NewCustomerPermissionService(),
		contacts:   repository.NewContactRepository(),
		suppliers:  repository.NewSupplierRepository(),
		providers:  repository.NewServiceProviderRepository(),
		merges:     repository.NewCustomerMergeRepository(),
	}
}

// SetProfileService 由 Router 装配点注入既有 360 服务（setter 注入，无内联闭包）。
func (s *CRM360Service) SetProfileService(svc *Customer360Service) {
	if svc != nil {
		s.profile = svc
	}
}

// View 返回 360 视图，越权客户直接拒绝。
func (s *CRM360Service) View(ctx context.Context, scope CustomerDataScope, customerID string) (*CRM360View, error) {
	if s.permission == nil {
		return nil, errors.New("CRM 数据权限服务未装配")
	}
	cust, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	if err != nil {
		return nil, err
	}
	view := &CRM360View{CustomerID: cust.ID}
	if s.profile != nil {
		view.Profile, err = s.profile.GetCustomer360ByCustomerID(ctx, customerID)
		if err != nil {
			return nil, err
		}
	}
	view.Contacts, _, _ = s.contacts.ListByCustomer(ctx, customerID, 1, 50)
	view.Suppliers, _, _ = s.suppliers.ListByCustomer(ctx, customerID, 1, 50)
	view.ServiceProviders, _, _ = s.providers.ListByCustomer(ctx, customerID, 1, 50)
	view.MergeHistory, _ = s.merges.ListByCustomer(ctx, customerID, 50)
	return view, nil
}

// CrossIndustrySearch 按行业 + 关键词跨账号检索客户。
// 账号范围沿用 CrossAccountSearchService 的 scope 语义，并叠加 T023 的账号/行业/负责人权限。
func (s *CRM360Service) CrossIndustrySearch(ctx context.Context, tid uint, scope CustomerDataScope, keyword, industry string, page, pageSize int) (*CRMIndustryCrossResult, error) {
	if s.permission == nil {
		return nil, errors.New("CRM 数据权限服务未装配")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	filter := repository.CustomerScopeFilter{
		Keyword: keyword, Page: page, PageSize: pageSize, ExcludeMerged: true,
	}
	if industry != "" {
		filter.Industries = []string{industry}
	}
	rows, total, err := s.permission.ListScoped(ctx, scope, filter)
	if err != nil {
		return nil, err
	}

	// 与 controller/cross_account_search.go 对齐：scope 出参 + 敏感字段脱敏。
	scopeInfo := CrossAccountSearchScope{
		AllAccounts:            scope.AllAccounts || !scope.RestrictedAccounts,
		Restricted:             scope.RestrictedAccounts,
		AuthorizedAccountCount: len(scope.AccountIDs),
	}
	result := &CRMIndustryCrossResult{
		Items:    make([]CRMIndustryCrossItem, 0, len(rows)),
		Total:    total,
		Scope:    scopeInfo,
		CrossTab: make([]CRMIndustryCrossRow, 0),
	}
	crossTab := make(map[string]int64)
	for _, c := range rows {
		item := CRMIndustryCrossItem{
			ID: c.ID, Name: c.Name, Industry: c.Industry,
			SourceAccountID: c.SourceAccountID,
		}
		// 非全量账号用户一律脱敏（与既有跨账号检索一致）。
		if !scopeInfo.AllAccounts {
			item.Phone = maskPhone(c.Phone)
			item.Email = maskEmail(c.Email)
		} else {
			item.Phone = c.Phone
			item.Email = c.Email
		}
		if c.OwnerUserID != nil {
			item.OwnerUserID = crmUintToString(*c.OwnerUserID)
		}
		result.Items = append(result.Items, item)
		key := c.Industry + "\x00" + c.SourceAccountID
		crossTab[key]++
	}
	for key, count := range crossTab {
		parts := splitCrossTabKey(key)
		result.CrossTab = append(result.CrossTab, CRMIndustryCrossRow{
			Industry: parts[0], SourceAccountID: parts[1], Count: count,
		})
	}
	return result, nil
}

func crmUintToString(v uint) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}

func splitCrossTabKey(key string) [2]string {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}

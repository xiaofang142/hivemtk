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
	// SubStates 按子列表给出分页/总数/截断与依赖可用性；依赖失败不再静默吞掉。
	SubStates map[string]CRM360SubState `json:"sub_states"`
	// Source 是带账号作用域的来源引用及缺失/越权状态（CRM-20）。
	Source *CRM360SourceState `json:"source,omitempty"`
}

// CRM360SubState 一个 360 子查询的可核验分页与可用性状态。
type CRM360SubState struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Total     int64  `json:"total"`
	Truncated bool   `json:"truncated"` // 当前页之外的记录数 > 0
	Available bool   `json:"available"` // 依赖读取成功
	Error     string `json:"error,omitempty"`
}

// CRM360ViewOptions 360 子列表分页参数（变参传入，旧调用方无需改动）。
type CRM360ViewOptions struct {
	Page     int
	PageSize int
}

// CRM360SourceState 来源契约：账号 + 实际消息/会话，且区分可用/缺失/越权。
type CRM360SourceState struct {
	AccountID      string `json:"account_id"`
	Channel        string `json:"channel,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	Status         string `json:"status"` // available | missing | out_of_scope
}

// crm360SubState 把一次子查询的 (page,pageSize,total,err) 归一为对外状态。
// 独立成纯函数便于验证「全量与当前页分开」「依赖失败标不可用」。
func crm360SubState(page, pageSize int, total int64, err error) CRM360SubState {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	st := CRM360SubState{Page: page, PageSize: pageSize, Total: total, Available: err == nil}
	if err != nil {
		st.Error = err.Error()
	}
	st.Truncated = total > int64(page*pageSize)
	return st
}

// crm360SourceState 客户来源只在该账号在权限范围内时才回查；
// 无消息标识或账号越权都有明确状态，不伪装成"有来源"。
func crm360SourceState(scope CustomerDataScope, c *model.Customer, canAccess func(CustomerDataScope, *model.Customer) bool) *CRM360SourceState {
	if c == nil {
		return nil
	}
	st := &CRM360SourceState{
		AccountID: c.SourceAccountID, Channel: c.SourceChannel,
		ConversationID: c.SourceConversationID, MessageID: c.SourceMessageID,
	}
	ref := CRMSourceRef{AccountID: c.SourceAccountID, MessageID: c.SourceMessageID}
	if canAccess != nil && !canAccess(scope, c) {
		st.Status = "out_of_scope"
		return st
	}
	if ref.Valid() {
		st.Status = "available"
	} else {
		st.Status = "missing"
	}
	return st
}

// CRMIndustryCrossItem 跨账号行业交叉检索的一行（与 controller/cross_account_search.go
// 的出参口径一致：手机号/邮箱默认脱敏）。
type CRMIndustryCrossItem struct {
	EntityType      string `json:"entity_type,omitempty"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Phone           string `json:"phone,omitempty"`
	Email           string `json:"email,omitempty"`
	Industry        string `json:"industry,omitempty"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	CustomerID      string `json:"customer_id,omitempty"`
	SourceRef       string `json:"source_ref,omitempty"`
}

// CRMIndustryCrossRow 行业 × 账号交叉统计。
type CRMIndustryCrossRow struct {
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	Count           int64  `json:"count"`
}

// CRMIndustryCrossResult 跨账号行业交叉检索结果。Total 为全量计数，
// Items 只含当前页；PerType 逐类型标注全量/当前页与可用性。
type CRMIndustryCrossResult struct {
	Items       []CRMIndustryCrossItem  `json:"items"`
	Total       int64                   `json:"total"`
	CrossTab    []CRMIndustryCrossRow   `json:"cross_tab"`
	Scope       CrossAccountSearchScope `json:"scope"`
	EntityTypes []string                `json:"entity_types"`
	Page        int                     `json:"page"`
	PageSize    int                     `json:"page_size"`
	PerType     []CRMCrossEntityStat    `json:"per_type"`
}

type CRMCrossEntityStat struct {
	EntityType string `json:"entity_type"`
	Total      int64  `json:"total"`
	Items      int    `json:"items"`
	Available  bool   `json:"available"`
	Error      string `json:"error,omitempty"`
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
	cross      repository.CRMCrossSearchRepository
}

func NewCRM360Service() *CRM360Service {
	return &CRM360Service{
		profile:    nil, // 延迟构造：既有 Customer360Service 需要显式 clueRepo 装配
		permission: NewCustomerPermissionService(),
		contacts:   repository.NewContactRepository(),
		suppliers:  repository.NewSupplierRepository(),
		providers:  repository.NewServiceProviderRepository(),
		merges:     repository.NewCustomerMergeRepository(),
		cross:      repository.NewCRMCrossSearchRepository(),
	}
}

// SetCrossSearchRepository 由 Router/测试注入跨实体检索仓储（setter 注入，无内联闭包）。
func (s *CRM360Service) SetCrossSearchRepository(repo repository.CRMCrossSearchRepository) {
	if repo != nil {
		s.cross = repo
	}
}

// SetProfileService 由 Router 装配点注入既有 360 服务（setter 注入，无内联闭包）。
func (s *CRM360Service) SetProfileService(svc *Customer360Service) {
	if svc != nil {
		s.profile = svc
	}
}

// View 返回 360 视图，越权客户直接拒绝（fail-closed）。
// 子列表失败不再让整个视图失败：记为 available=false + error，调用方据实展示"不可用"。
func (s *CRM360Service) View(ctx context.Context, scope CustomerDataScope, customerID string, opts ...CRM360ViewOptions) (*CRM360View, error) {
	if s.permission == nil {
		return nil, errors.New("CRM 数据权限服务未装配")
	}
	cust, err := s.permission.GetByIDScoped(ctx, scope, customerID)
	if err != nil {
		return nil, err
	}
	page, pageSize := 1, 50
	if len(opts) > 0 {
		if opts[0].Page > 0 {
			page = opts[0].Page
		}
		if opts[0].PageSize > 0 {
			pageSize = opts[0].PageSize
		}
	}
	view := &CRM360View{CustomerID: cust.ID, SubStates: map[string]CRM360SubState{}}
	view.Source = crm360SourceState(scope, cust, s.permission.CanAccess)
	if s.profile != nil {
		view.Profile, err = s.profile.GetCustomer360ByCustomerID(ctx, customerID)
		if err != nil {
			view.SubStates["profile"] = crm360SubState(1, 1, 0, err)
		} else {
			view.SubStates["profile"] = crm360SubState(1, 1, 1, nil)
		}
	}
	filter := s.permission.EntityAccountFilter(scope, cust)

	var total int64
	view.Contacts, total, err = s.contacts.ListByCustomer(ctx, customerID, page, pageSize, filter)
	view.SubStates["contacts"] = crm360SubState(page, pageSize, total, err)
	view.Suppliers, total, err = s.suppliers.ListByCustomer(ctx, customerID, page, pageSize, filter)
	view.SubStates["suppliers"] = crm360SubState(page, pageSize, total, err)
	view.ServiceProviders, total, err = s.providers.ListByCustomer(ctx, customerID, page, pageSize, filter)
	view.SubStates["service_providers"] = crm360SubState(page, pageSize, total, err)
	merges, mErr := s.merges.ListByCustomer(ctx, customerID, pageSize)
	if mErr == nil {
		view.MergeHistory = merges
	}
	view.SubStates["merge_history"] = crm360SubState(1, pageSize, int64(len(merges)), mErr)
	return view, nil
}

// CrossIndustrySearch 兼容旧入口：只检索客户。
func (s *CRM360Service) CrossIndustrySearch(ctx context.Context, tid uint, scope CustomerDataScope, keyword, industry string, page, pageSize int) (*CRMIndustryCrossResult, error) {
	return s.CrossIndustrySearchWithEntities(ctx, tid, scope, keyword, industry, []string{"customer"}, page, pageSize)
}

// CrossIndustrySearchWithEntities 跨账号检索客户/公司/联系人/供应商/服务商。
// 全量统计与当前页严格分开：Total/PerType.Total/CrossTab 来自全部受权记录，
// Items 只含当前页；不支持的实体类型显式标注，不静默退化为客户全量。
func (s *CRM360Service) CrossIndustrySearchWithEntities(ctx context.Context, tid uint, scope CustomerDataScope, keyword, industry string, entityTypes []string, page, pageSize int) (*CRMIndustryCrossResult, error) {
	if s.permission == nil {
		return nil, errors.New("CRM 数据权限服务未装配")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	if len(entityTypes) == 0 {
		entityTypes = []string{"customer"}
	}
	scopeInfo := CrossAccountSearchScope{
		AllAccounts:            scope.AllAccounts || !scope.RestrictedAccounts,
		Restricted:             scope.RestrictedAccounts,
		AuthorizedAccountCount: len(scope.AccountIDs),
	}
	result := &CRMIndustryCrossResult{
		Items: make([]CRMIndustryCrossItem, 0, len(entityTypes)*pageSize),
		Scope: scopeInfo, EntityTypes: append([]string(nil), entityTypes...),
		Page: page, PageSize: pageSize,
		CrossTab: make([]CRMIndustryCrossRow, 0), PerType: make([]CRMCrossEntityStat, 0, len(entityTypes)),
	}
	crossTab := make(map[string]int64)

	// 未装配跨实体仓储时退回旧客户检索路径，保持既有装配/测试可用。
	if s.cross == nil {
		return s.crossIndustrySearchFallback(ctx, scope, keyword, industry, page, pageSize, scopeInfo, result, crossTab)
	}

	for _, et := range entityTypes {
		base := s.permission.ToFilter(scope, repository.CustomerScopeFilter{ExcludeMerged: true})
		base.Keyword = ""
		if industry != "" {
			base.Industries = []string{industry}
		}
		f := repository.CRMCrossSearchFilter{EntityType: et, CustomerScopeFilter: base}
		f.Keyword = keyword
		f.Page, f.PageSize = page, pageSize
		rows, total, err := s.cross.ListScoped(ctx, f)
		stat := CRMCrossEntityStat{EntityType: et, Total: total, Available: err == nil}
		if err != nil {
			stat.Error = err.Error()
			result.PerType = append(result.PerType, stat)
			continue
		}
		result.Total += total
		for _, row := range rows {
			item := CRMIndustryCrossItem{
				EntityType: et, ID: row.ID, Name: row.Name, Industry: row.Industry,
				SourceAccountID: row.SourceAccountID, OwnerUserID: row.OwnerUserID,
				CustomerID: row.CustomerID, SourceRef: row.SourceRef,
			}
			if !scopeInfo.AllAccounts {
				item.Phone = maskPhone(row.Phone)
				item.Email = maskEmail(row.Email)
			} else {
				item.Phone = row.Phone
				item.Email = row.Email
			}
			result.Items = append(result.Items, item)
		}
		stat.Items = len(rows)
		result.PerType = append(result.PerType, stat)

		// CrossTab 用全量聚合，不能从当前页累加（第 51 条后仍需正确统计）。
		counts, cErr := s.cross.CountIndustryAccount(ctx, f)
		if cErr != nil {
			for i := range result.PerType {
				if result.PerType[i].EntityType == et {
					result.PerType[i].Error += " 交叉统计失败: " + cErr.Error()
					result.PerType[i].Available = false
				}
			}
			continue
		}
		for _, c := range counts {
			crossTab[c.Industry+"\x00"+c.SourceAccountID] += c.Count
		}
	}
	for key, count := range crossTab {
		parts := splitCrossTabKey(key)
		result.CrossTab = append(result.CrossTab, CRMIndustryCrossRow{
			Industry: parts[0], SourceAccountID: parts[1], Count: count,
		})
	}
	return result, nil
}

func (s *CRM360Service) crossIndustrySearchFallback(ctx context.Context, scope CustomerDataScope, keyword, industry string, page, pageSize int, scopeInfo CrossAccountSearchScope, result *CRMIndustryCrossResult, crossTab map[string]int64) (*CRMIndustryCrossResult, error) {
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
	result.Total = total
	result.PerType = append(result.PerType, CRMCrossEntityStat{EntityType: "customer", Total: total, Items: len(rows), Available: true})
	for _, c := range rows {
		item := CRMIndustryCrossItem{
			EntityType: "customer", ID: c.ID, Name: c.Name, Industry: c.Industry,
			SourceAccountID: c.SourceAccountID,
		}
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

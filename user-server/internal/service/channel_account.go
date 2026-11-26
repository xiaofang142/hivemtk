package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	// ErrChannelAccountNotFound 渠道账号不存在（Controller 映射 404）。
	ErrChannelAccountNotFound = errors.New("渠道账号不存在")
	// ErrChannelAccountInvalid 渠道账号参数错误（Controller 映射 400）。
	ErrChannelAccountInvalid = errors.New("渠道账号参数错误")
	// ErrChannelNotFound 渠道不存在。
	ErrChannelNotFound = errors.New("渠道不存在")
	// ErrChannelAccountDataScopeInvalid 数据授权 scope 不是合法 JSON 对象。
	ErrChannelAccountDataScopeInvalid = errors.New("数据授权范围必须是 JSON 对象")
	// ErrAccountAuthorizationInvalid 账号授权输入非法（空授权、非法行业/角色）。
	ErrAccountAuthorizationInvalid = errors.New("账号授权参数错误")
	// ErrAccountAuthorizationTenantMismatch 用户/账号不属于当前租户。
	ErrAccountAuthorizationTenantMismatch = errors.New("账号或员工不属于当前租户")
	// ErrAccountAuthorizationStoreUnavailable 授权存储未就绪，fail-closed。
	ErrAccountAuthorizationStoreUnavailable = errors.New("账号授权存储不可用")
)

// allowedChannelAccountTypes 渠道账号类型白名单（单聊/群聊/客服）。
var allowedChannelAccountTypes = map[string]bool{"dm": true, "single": true, "group": true, "group_chat": true, "cs": true, "service": true}

// validateChannelAccountSave 校验渠道账号入参（FR-024 全字段完整化后的统一校验口）。
func validateChannelAccountSave(in *dto.ChannelAccountSave) error {
	if in == nil || in.ChannelID == 0 || strings.TrimSpace(in.Name) == "" {
		return ErrChannelAccountInvalid
	}
	if in.Type != "" && !allowedChannelAccountTypes[strings.ToLower(strings.TrimSpace(in.Type))] {
		return fmt.Errorf("%w: 不支持的账号类型 %q", ErrChannelAccountInvalid, in.Type)
	}
	if in.DataScope != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(in.DataScope), &obj); err != nil || obj == nil {
			return ErrChannelAccountDataScopeInvalid
		}
	}
	if in.Status != nil && *in.Status != 0 && *in.Status != 1 {
		return ErrChannelAccountInvalid
	}
	return nil
}

func toAccountDetail(acc *model.ChannelAccount) dto.ChannelAccountDetail {
	return dto.ChannelAccountDetail{
		ID: acc.ID, ChannelID: acc.ChannelID, Name: acc.Name, Remark: acc.Remark,
		SelfIdentifyName: acc.SelfIdentifyName, Type: acc.Type,
		AutoReplyAgentID: acc.AutoReplyAgentID, LeadMiningAgentID: acc.LeadMiningAgentID,
		OwnerUserIDs: []string(acc.OwnerUserIDs), DataScope: acc.DataScope,
		IdentifyCode: acc.IdentifyCode, Status: acc.Status,
		CreatedAt: acc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: acc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ChannelAccountService 渠道账号业务服务。
//
// ListOptions 是所有内部渠道账号选择（下拉、配置页、规则作用域选择、通知人配置等）
// 的唯一入口（FR-029）——任何调用方不得自己拼 repository 选项或直查表。
type ChannelAccountService interface {
	// ListAccounts 是内部调用（解析器/分发/租户绑定）的统一账号选择口，
	// 保留仓储选项的完整语义（含非启用账号），返回 model 行。
	ListAccounts(ctx context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error)
	ListOptions(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountOption, error)
	Get(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountOption, error)
	// GetDetail 返回全字段（含备注/负责人/数据授权），管理面编辑回填用。
	GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error)
	Create(ctx context.Context, tenantID uint, in *dto.ChannelAccountSave) (*dto.ChannelAccountOption, error)
	Update(ctx context.Context, tenantID, id uint, in *dto.ChannelAccountSave) error
	Delete(ctx context.Context, tenantID, id uint) error
}

type channelAccountService struct {
	repo repository.ChannelAccountRepository
}

// NewChannelAccountService 创建渠道账号服务。
func NewChannelAccountService() ChannelAccountService {
	return &channelAccountService{repo: repository.NewChannelAccountRepository()}
}

// NewChannelAccountServiceWithRepo 用显式仓储构造（装配点/测试用）。
func NewChannelAccountServiceWithRepo(repo repository.ChannelAccountRepository) ChannelAccountService {
	return &channelAccountService{repo: repo}
}

func toAccountOption(acc *model.ChannelAccount) dto.ChannelAccountOption {
	return dto.ChannelAccountOption{
		ID:                acc.ID,
		ChannelID:         acc.ChannelID,
		Name:              acc.Name,
		IdentifyCode:      acc.IdentifyCode,
		Type:              acc.Type,
		Status:            acc.Status,
		SelfIdentifyName:  acc.SelfIdentifyName,
		AutoReplyAgentID:  acc.AutoReplyAgentID,
		LeadMiningAgentID: acc.LeadMiningAgentID,
	}
}

// ListAccounts 内部统一账号选择口：所有仓库级账号列表必须经此方法，
// 避免各调用方自行拼选项造成口径漂移（FR-029/SC-006）。
func (s *channelAccountService) ListAccounts(ctx context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return listChannelAccounts(ctx, s.repo, opts)
}

// listChannelAccounts 是仓库级账号查询的唯一实现：解析器/分发/租户绑定/服务层
// 都必须经此函数，避免各自拼 ChannelAccountListOptions 造成口径漂移。
func listChannelAccounts(ctx context.Context, repo repository.ChannelAccountRepository, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	if repo == nil || !repo.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	return repo.List(ctx, opts)
}

// ListOptions 统一下拉：默认只返回启用中的账号，避免停用的账号被继续选中。
func (s *channelAccountService) ListOptions(ctx context.Context, tenantID uint, q dto.ChannelAccountListQuery) ([]dto.ChannelAccountOption, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	status := q.Status
	if status == nil {
		on := 1
		status = &on
	}
	rows, err := s.ListAccounts(ctx, repository.ChannelAccountListOptions{
		TenantID:  tenantID,
		ChannelID: q.ChannelID,
		Status:    status,
		Keyword:   q.Keyword,
		IDs:       q.IDs,
		Limit:     q.Limit,
		Offset:    q.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelAccountOption, 0, len(rows))
	for i := range rows {
		out = append(out, toAccountOption(&rows[i]))
	}
	return out, nil
}

func (s *channelAccountService) Get(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountOption, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelAccountNotFound
	}
	opt := toAccountOption(acc)
	return &opt, nil
}

func (s *channelAccountService) GetDetail(ctx context.Context, tenantID, id uint) (*dto.ChannelAccountDetail, error) {
	if tenantID == 0 || id == 0 {
		return nil, ErrChannelAccountInvalid
	}
	acc, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelAccountNotFound
	}
	detail := toAccountDetail(acc)
	return &detail, nil
}

func (s *channelAccountService) Create(ctx context.Context, tenantID uint, in *dto.ChannelAccountSave) (*dto.ChannelAccountOption, error) {
	if tenantID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if err := validateChannelAccountSave(in); err != nil {
		return nil, err
	}
	status := 1
	if in.Status != nil {
		status = *in.Status
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = "dm"
	}
	acc := &model.ChannelAccount{
		TenantID:          tenantID,
		ChannelID:         in.ChannelID,
		Name:              strings.TrimSpace(in.Name),
		Remark:            in.Remark,
		SelfIdentifyName:  in.SelfIdentifyName,
		Type:              typ,
		AutoReplyAgentID:  in.AutoReplyAgentID,
		LeadMiningAgentID: in.LeadMiningAgentID,
		OwnerUserIDs:      model.JSONStrings(in.OwnerUserIDs),
		DataScope:         "{}",
		Status:            status,
	}
	if in.DataScope != "" {
		acc.DataScope = in.DataScope
	}
	if err := s.repo.Create(ctx, acc); err != nil {
		return nil, err
	}
	opt := toAccountOption(acc)
	return &opt, nil
}

func (s *channelAccountService) Update(ctx context.Context, tenantID, id uint, in *dto.ChannelAccountSave) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	if err := validateChannelAccountSave(in); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrChannelAccountNotFound
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	updates := map[string]any{
		"channel_id":           in.ChannelID,
		"name":                 strings.TrimSpace(in.Name),
		"remark":               in.Remark,
		"self_identify_name":   in.SelfIdentifyName,
		"auto_reply_agent_id":  in.AutoReplyAgentID,
		"lead_mining_agent_id": in.LeadMiningAgentID,
		"owner_user_ids":       model.JSONStrings(in.OwnerUserIDs),
	}
	if typ != "" {
		updates["type"] = typ
	}
	if in.DataScope != "" {
		updates["data_scope"] = in.DataScope
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	return s.repo.Update(ctx, tenantID, id, updates)
}

func (s *channelAccountService) Delete(ctx context.Context, tenantID, id uint) error {
	if tenantID == 0 || id == 0 {
		return ErrChannelAccountInvalid
	}
	return s.repo.Delete(ctx, tenantID, id)
}

// AccountAuthorizationPair 一条账号授权配对：账号 + 可见行业 + 角色。
// 配对不可拆成全局行业集合，否则不同账号的行业会被拼成超集后交叉放行。
type AccountAuthorizationPair struct {
	AccountID   uint     `json:"account_id"`
	Industries  []string `json:"industries,omitempty"`
	Role        string   `json:"role,omitempty"`
	OwnerUserID uint     `json:"owner_user_id,omitempty"`
}

// AccountAuthorizationService 账号授权管理（LEAD-01）。
//
// 复用 092 channel_account_authorizations 表与 CustomerPermissionService 的读取口径；
// 不新建第二套授权表，不改 RBAC。选择项由各账号仓储提供，保存/回读/撤销经本服务收口。
// 用户/账号必须属于当前租户；空授权拒绝；每次读取即时反映撤销。
type authorizationAccountReader interface {
	Available() bool
	GetByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error)
	List(ctx context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error)
}

type authorizationUserReader interface {
	GetByID(ctx context.Context, id uint) (*model.SystemUser, error)
}

type AccountAuthorizationService struct {
	accounts authorizationAccountReader
	users    authorizationUserReader
	store    accountAuthorizationStore
}

type accountAuthorizationStore interface {
	ListByUser(ctx context.Context, userID uint) ([]model.ChannelAccountAuthorization, error)
	Replace(ctx context.Context, tenantID, userID uint, rows []model.ChannelAccountAuthorization) error
	Revoke(ctx context.Context, tenantID, userID, accountID uint) error
}

// accountAuthorizationStoreImpl 生产实现：复用 092 表与泛型仓储，不新增表/迁移。
type accountAuthorizationStoreImpl struct {
	rows repository.BaseRepository[model.ChannelAccountAuthorization]
}

func newAccountAuthorizationStore() accountAuthorizationStore {
	gdb := dbstate.Get()
	if gdb == nil {
		return nil
	}
	return &accountAuthorizationStoreImpl{rows: *repository.NewBaseRepository[model.ChannelAccountAuthorization](gdb)}
}

func (s *accountAuthorizationStoreImpl) ListByUser(ctx context.Context, userID uint) ([]model.ChannelAccountAuthorization, error) {
	if s == nil {
		return nil, ErrAccountAuthorizationStoreUnavailable
	}
	found, err := s.rows.FindByField(ctx, "user_id", userID)
	if err != nil {
		return nil, err
	}
	out := make([]model.ChannelAccountAuthorization, 0, len(found))
	for _, row := range found {
		out = append(out, *row)
	}
	return out, nil
}

func (s *accountAuthorizationStoreImpl) Replace(ctx context.Context, tenantID, userID uint, rows []model.ChannelAccountAuthorization) error {
	if s == nil {
		return ErrAccountAuthorizationStoreUnavailable
	}
	existing, err := s.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, row := range existing {
		if row.TenantID == tenantID && row.UserID == userID {
			if err := s.rows.Delete(ctx, row.ID); err != nil {
				return err
			}
		}
	}
	for i := range rows {
		if err := s.rows.Create(ctx, &rows[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *accountAuthorizationStoreImpl) Revoke(ctx context.Context, tenantID, userID, accountID uint) error {
	if s == nil {
		return ErrAccountAuthorizationStoreUnavailable
	}
	existing, err := s.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, row := range existing {
		if row.TenantID == tenantID && row.UserID == userID && row.ChannelAccountID == accountID {
			if err := s.rows.Delete(ctx, row.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// NewAccountAuthorizationService 生产构造：仓储均取当前 DB 装配。
func NewAccountAuthorizationService() *AccountAuthorizationService {
	return NewAccountAuthorizationServiceWithDeps(
		repository.NewChannelAccountRepository(),
		repository.NewSystemUserRepository(),
		newAccountAuthorizationStore(),
	)
}

// NewAccountAuthorizationServiceWithDeps 测试/装配显式注入。
func NewAccountAuthorizationServiceWithDeps(accounts authorizationAccountReader, users authorizationUserReader, store accountAuthorizationStore) *AccountAuthorizationService {
	return &AccountAuthorizationService{accounts: accounts, users: users, store: store}
}

func (s *AccountAuthorizationService) currentTenant(ctx context.Context) (uint, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
		if tenant.IsMultiTenant() {
			return 0, ErrAccountAuthorizationTenantMismatch
		}
		tid = tenant.DefaultTenantID
	}
	return tid, nil
}

// AvailableAccounts 返回当前租户可选择授权的账号（可读选择源）。
func (s *AccountAuthorizationService) AvailableAccounts(ctx context.Context, keyword string) ([]dto.ChannelAccountOption, error) {
	tid, err := s.currentTenant(ctx)
	if err != nil {
		return nil, err
	}
	if s.accounts == nil || !s.accounts.Available() {
		return nil, ErrAccountAuthorizationStoreUnavailable
	}
	rows, err := s.accounts.List(ctx, repository.ChannelAccountListOptions{TenantID: tid, Keyword: strings.TrimSpace(keyword)})
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelAccountOption, 0, len(rows))
	for i := range rows {
		out = append(out, toAccountOption(&rows[i]))
	}
	return out, nil
}

// Save 保存当前租户用户对一组账号的授权（覆盖式，保留逐条配对）。
func (s *AccountAuthorizationService) Save(ctx context.Context, tenantID, userID uint, pairs []AccountAuthorizationPair) ([]model.ChannelAccountAuthorization, error) {
	if s == nil || s.store == nil || s.accounts == nil || s.users == nil {
		return nil, ErrAccountAuthorizationStoreUnavailable
	}
	ctxTid, err := s.currentTenant(ctx)
	if err != nil {
		return nil, err
	}
	if tenantID == 0 || tenantID != ctxTid || userID == 0 || len(pairs) == 0 {
		return nil, ErrAccountAuthorizationInvalid
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil || u == nil || u.TenantID != tenantID {
		return nil, ErrAccountAuthorizationTenantMismatch
	}
	rows := make([]model.ChannelAccountAuthorization, 0, len(pairs))
	seen := make(map[uint]bool, len(pairs))
	for _, p := range pairs {
		if p.AccountID == 0 || seen[p.AccountID] {
			return nil, ErrAccountAuthorizationInvalid
		}
		seen[p.AccountID] = true
		role := strings.ToLower(strings.TrimSpace(p.Role))
		if role == "" {
			role = "viewer"
		}
		if role != "viewer" && role != "owner" {
			return nil, ErrAccountAuthorizationInvalid
		}
		acc, err := s.accounts.GetByID(ctx, tenantID, p.AccountID)
		if err != nil || acc == nil || acc.TenantID != tenantID {
			return nil, ErrAccountAuthorizationTenantMismatch
		}
		inds, err := encodeAuthorizationIndustries(p.Industries)
		if err != nil {
			return nil, ErrAccountAuthorizationInvalid
		}
		rows = append(rows, model.ChannelAccountAuthorization{
			TenantID: tenantID, UserID: userID, ChannelAccountID: p.AccountID,
			VisibleIndustryScope: inds, Role: role,
		})
	}
	if err := s.store.Replace(ctx, tenantID, userID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// List 回读当前租户用户已保存的配对（不做集合合并）。
func (s *AccountAuthorizationService) List(ctx context.Context, tenantID, userID uint) ([]model.ChannelAccountAuthorization, error) {
	if s == nil || s.store == nil {
		return nil, ErrAccountAuthorizationStoreUnavailable
	}
	ctxTid, err := s.currentTenant(ctx)
	if err != nil {
		return nil, err
	}
	if tenantID == 0 || tenantID != ctxTid || userID == 0 {
		return nil, ErrAccountAuthorizationInvalid
	}
	all, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]model.ChannelAccountAuthorization, 0, len(all))
	for _, row := range all {
		if row.TenantID == tenantID {
			out = append(out, row)
		}
	}
	return out, nil
}

// Revoke 撤销某账号授权；权限解析每次重新读表，撤销立即生效。
func (s *AccountAuthorizationService) Revoke(ctx context.Context, tenantID, userID, accountID uint) error {
	if s == nil || s.store == nil {
		return ErrAccountAuthorizationStoreUnavailable
	}
	ctxTid, err := s.currentTenant(ctx)
	if err != nil {
		return err
	}
	if tenantID == 0 || tenantID != ctxTid || userID == 0 || accountID == 0 {
		return ErrAccountAuthorizationInvalid
	}
	return s.store.Revoke(ctx, tenantID, userID, accountID)
}

// ---- LEAD-02：账号侧回复 Agent 绑定作为回复执行的唯一配置来源 ----

var (
	// ErrReplyAgentBindingInvalid 绑定目标不存在、停用或不属于当前租户。
	ErrReplyAgentBindingInvalid = errors.New("回复智能体绑定无效")
	// ErrReplyAgentBindingWrongType 绑定目标不是回复类智能体。
	ErrReplyAgentBindingWrongType = errors.New("回复智能体类型错误")
)

// ReplyAgentExecutionContext 一次回复执行实际采用的 Agent 配置证明。
// 捕获 ID/模型/提示词，供模型请求与测试核对真实执行配置，而非只看绑定 ID。
type ReplyAgentExecutionContext struct {
	AgentID      uint   `json:"agent_id"`
	AgentType    string `json:"agent_type"`
	LLMModel     string `json:"llm_model"`
	SystemPrompt string `json:"system_prompt"`
}

// ReplyAgentBindingService 账号侧回复绑定解析（LEAD-02）。
// 只接受 sales/customer_service/hybrid 三类回复 Agent；停用、缺失、跨租户一律拒绝。
type ReplyAgentBindingService struct {
	accounts authorizationAccountReader
	agents   replyAgentReader
}

type replyAgentReader interface {
	GetByID(ctx context.Context, id uint) (*model.AIAgent, error)
}

// NewReplyAgentBindingService 生产构造；accounts/agents 可为 nil（Resolve 时 fail-closed）。
func NewReplyAgentBindingService(accounts authorizationAccountReader, agents replyAgentReader) *ReplyAgentBindingService {
	return &ReplyAgentBindingService{accounts: accounts, agents: agents}
}

func (s *ReplyAgentBindingService) Resolve(ctx context.Context, tenantID, accountID uint, boundAgentID *uint) (*ReplyAgentExecutionContext, error) {
	if s == nil || s.accounts == nil || s.agents == nil || tenantID == 0 || accountID == 0 || boundAgentID == nil || *boundAgentID == 0 {
		return nil, ErrReplyAgentBindingInvalid
	}
	acc, err := s.accounts.GetByID(ctx, tenantID, accountID)
	if err != nil || acc == nil || acc.TenantID != tenantID {
		return nil, ErrReplyAgentBindingInvalid
	}
	agent, err := s.agents.GetByID(ctx, *boundAgentID)
	if err != nil || agent == nil || agent.TenantID != tenantID || agent.Status != 1 {
		return nil, ErrReplyAgentBindingInvalid
	}
	switch strings.ToLower(strings.TrimSpace(agent.AgentType)) {
	case string(model.AgentTypeSales), string(model.AgentTypeCustomerService), string(model.AgentTypeHybrid):
	default:
		return nil, ErrReplyAgentBindingWrongType
	}
	return s.ExecutionContext(agent), nil
}

// ExecutionContext 把真实 Agent 配置投影成执行配置证明（无 DB 依赖）。
func (s *ReplyAgentBindingService) ExecutionContext(agent *model.AIAgent) *ReplyAgentExecutionContext {
	if agent == nil {
		return nil
	}
	return &ReplyAgentExecutionContext{
		AgentID: agent.ID, AgentType: agent.AgentType,
		LLMModel: agent.LLMModel, SystemPrompt: agent.SystemPrompt,
	}
}

// LegacyReplyBindingMigration 旧 ChannelAgentBinding → 账号侧绑定的迁移预览项。
type LegacyReplyBindingMigration struct {
	LegacyBindingID             uint   `json:"legacy_binding_id"`
	ChannelType                 string `json:"channel_type"`
	AccountID                   string `json:"account_id"`
	ChatID                      string `json:"chat_id,omitempty"`
	AgentID                     uint   `json:"agent_id"`
	ConflictsWithAccountBinding bool   `json:"conflicts_with_account_binding"`
	RedirectToAccountBinding    bool   `json:"redirect_to_account_binding"`
}

// PreviewLegacyReplyBindings 先预览映射/冲突，再由管理面确认迁移或转向账号侧绑定。
// 只预览启用的主绑定；停用/辅绑定不进入迁移，避免两套权威并存。
func PreviewLegacyReplyBindings(rows []model.ChannelAgentBinding, accountAgentID *uint) []LegacyReplyBindingMigration {
	out := make([]LegacyReplyBindingMigration, 0, len(rows))
	for _, b := range rows {
		if b.Enabled != nil && !*b.Enabled {
			continue
		}
		if b.IsPrimary == nil || !*b.IsPrimary {
			continue
		}
		out = append(out, LegacyReplyBindingMigration{
			LegacyBindingID: b.ID, ChannelType: b.ChannelType, AccountID: b.AccountID,
			ChatID: b.ChatID, AgentID: b.AgentID,
			ConflictsWithAccountBinding: accountAgentID != nil && *accountAgentID != b.AgentID,
			RedirectToAccountBinding:    accountAgentID != nil && *accountAgentID == b.AgentID,
		})
	}
	return out
}

// LegacyReplyBindingNotice 旧入口统一提示：账号侧绑定是唯一权威，旧入口只预览/转向。
func LegacyReplyBindingNotice() string {
	return "账号侧回复智能体绑定是唯一权威来源；旧入口仅预览映射，确认后转向账号绑定，不再产生第二套配置。"
}

func encodeAuthorizationIndustries(inds []string) (string, error) {
	if len(inds) == 0 {
		return "[]", nil
	}
	seen := make(map[string]bool, len(inds))
	out := make([]string, 0, len(inds))
	for _, ind := range inds {
		ind = strings.TrimSpace(ind)
		if ind == "" || seen[ind] {
			continue
		}
		seen[ind] = true
		out = append(out, ind)
	}
	if len(out) == 0 {
		return "", ErrAccountAuthorizationInvalid
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", ErrAccountAuthorizationInvalid
	}
	return string(b), nil
}

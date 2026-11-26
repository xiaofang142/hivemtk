package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CRM-22/23：员工 CRM 助手的服务端受限会话与行动草稿审批闭环。
//
// 设计口径：
//   - 工具白名单是服务端常量，模型/客户端只能"请求"调用，不能自行扩张权限；
//   - 所有权限/账号/行业校验走接缝，未装配依赖一律 fail-closed；
//   - 无证据不生成结论；行动草稿必须审批后才执行，执行前再次复核对象版本与权限；
//   - 本文件不碰 DB/gin，持久化由 Router 注入实现（内存实现仅供测试与本地跑通）。

var (
	ErrAssistantForbidden       = errors.New("CRM 助手越权")
	ErrAssistantToolDenied      = errors.New("CRM 助手工具不在只读白名单")
	ErrAssistantVersionConflict = errors.New("行动草稿版本冲突")
	ErrAssistantDraftNotFound   = errors.New("行动草稿不存在")
)

// crmAssistantReadOnlyTools 服务端只读工具白名单：写操作/外发工具一律不在此列。
// 新增工具必须显式登记并经权限复核；未登记的工具不会被暴露给模型。
var crmAssistantReadOnlyTools = map[string]bool{
	"crm.query_customer":         true,
	"crm.query_contact":          true,
	"crm.query_company":          true,
	"crm.query_supplier":         true,
	"crm.query_service_provider": true,
	"crm.query_followup":         true,
	"crm.view_360":               true,
	"rag.search":                 true,
}

// CRMAssistantScope 员工会话作用域。账号/行业语义复用 CustomerDataScope；
// TenantID/EmployeeID 由服务端上下文注入，客户端不可自报。
type CRMAssistantScope struct {
	TenantID   uint
	EmployeeID uint
	SessionID  string
	CustomerDataScope
}

// CRMAssistantToolCall 一次模型/客户端的工具调用请求（含声明的检索边界）。
type CRMAssistantToolCall struct {
	Name      string
	AccountID string
	Industry  string
	Ref       string
}

// CRMAssistantEvidence 可回查引用；Ref 必须来自只读工具的实际检索结果。
type CRMAssistantEvidence struct {
	Ref       string `json:"ref"`
	Source    string `json:"source"`
	AccountID string `json:"account_id,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
}

// CRMAssistantToolRecord 工具调用审计记录：允许与否、拒绝原因、引用一并留存。
type CRMAssistantToolRecord struct {
	Name        string `json:"name"`
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason,omitempty"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// CRMAssistantAnswer 会话回答。Evidence 为空时 Text 必须为空（无证据不编造）。
type CRMAssistantAnswer struct {
	SessionID string                   `json:"session_id"`
	Text      string                   `json:"text"`
	Evidence  []CRMAssistantEvidence   `json:"evidence"`
	ToolCalls []CRMAssistantToolRecord `json:"tool_calls"`
	Status    string                   `json:"status"` // ok | degraded_no_evidence | model_unavailable
}

// CRMAssistantScopeGuard 服务端作用域校验接缝：会话归属、账号与行业边界。
type CRMAssistantScopeGuard interface {
	OwnsSession(ctx context.Context, scope CRMAssistantScope) error
	AllowsAccount(ctx context.Context, scope CRMAssistantScope, accountID string) error
	AllowsIndustry(ctx context.Context, scope CRMAssistantScope, industry string) error
}

// crmAssistantScopeGuard 默认守卫，基于 CustomerDataScope 的既有账号/行业语义。
type crmAssistantScopeGuard struct{}

func (crmAssistantScopeGuard) OwnsSession(_ context.Context, scope CRMAssistantScope) error {
	if scope.TenantID == 0 || scope.EmployeeID == 0 || scope.SessionID == "" {
		return ErrAssistantForbidden
	}
	if scope.UserID != 0 && scope.UserID != scope.EmployeeID {
		return ErrAssistantForbidden
	}
	return nil
}

func (crmAssistantScopeGuard) AllowsAccount(_ context.Context, scope CRMAssistantScope, accountID string) error {
	if accountID == "" {
		return nil
	}
	if scope.AllAccounts {
		return nil
	}
	if scope.RestrictedAccounts {
		if !crmContainsString(scope.AccountIDs, accountID) {
			return ErrAssistantForbidden
		}
	}
	// 逐账号授权：声明的账号必须命中一条允许该账号的规则。
	if len(scope.AccountRules) > 0 {
		for _, rule := range scope.AccountRules {
			if rule.AccountID == accountID || crmContainsString(rule.SourceAccountIDs, accountID) {
				return nil
			}
		}
		return ErrAssistantForbidden
	}
	return nil
}

func (crmAssistantScopeGuard) AllowsIndustry(_ context.Context, scope CRMAssistantScope, industry string) error {
	if industry == "" {
		return nil
	}
	if len(scope.Industries) > 0 && !crmContainsString(scope.Industries, industry) {
		return ErrAssistantForbidden
	}
	return nil
}

func crmContainsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// CRMAssistantToolExecutor 只读工具执行接缝，由 Router 注入带数据权限的检索实现。
type CRMAssistantToolExecutor interface {
	Execute(ctx context.Context, scope CRMAssistantScope, call CRMAssistantToolCall) ([]CRMAssistantEvidence, error)
}

// CRMAssistantModel 模型接缝：只把已授权检索到的证据喂给模型，并回收其引用。
type CRMAssistantModel interface {
	Answer(ctx context.Context, question string, evidence []CRMAssistantEvidence) (text string, citedRefs []string, err error)
}

// CRMAssistantAudit 工具调用与回答审计接缝。
type CRMAssistantAudit interface {
	RecordToolCall(ctx context.Context, scope CRMAssistantScope, record CRMAssistantToolRecord)
}

// CRMAssistantService 员工助手会话服务（CRM-22）。
type CRMAssistantService struct {
	guard    CRMAssistantScopeGuard
	executor CRMAssistantToolExecutor
	model    CRMAssistantModel
	audit    CRMAssistantAudit
}

func NewCRMAssistantService() *CRMAssistantService {
	return &CRMAssistantService{guard: crmAssistantScopeGuard{}}
}

func (s *CRMAssistantService) SetExecutor(e CRMAssistantToolExecutor) {
	if e != nil {
		s.executor = e
	}
}
func (s *CRMAssistantService) SetModel(m CRMAssistantModel) {
	if m != nil {
		s.model = m
	}
}
func (s *CRMAssistantService) SetAudit(a CRMAssistantAudit) {
	if a != nil {
		s.audit = a
	}
}
func (s *CRMAssistantService) SetGuard(g CRMAssistantScopeGuard) {
	if g != nil {
		s.guard = g
	}
}

// Ask 只执行服务端白名单内、且通过作用域复核的只读工具；模型引用必须命中实际证据，
// 否则降级为空结论。模型/检索不可用均显式标状态，不伪装成已证实结论。
func (s *CRMAssistantService) Ask(ctx context.Context, scope CRMAssistantScope, question string, calls []CRMAssistantToolCall) (*CRMAssistantAnswer, error) {
	if s.guard == nil {
		return nil, ErrAssistantForbidden
	}
	if err := s.guard.OwnsSession(ctx, scope); err != nil {
		return nil, err
	}
	answer := &CRMAssistantAnswer{
		SessionID: scope.SessionID, Status: "ok",
		Evidence: make([]CRMAssistantEvidence, 0), ToolCalls: make([]CRMAssistantToolRecord, 0, len(calls)),
	}
	seen := make(map[string]bool)
	for _, call := range calls {
		record := CRMAssistantToolRecord{Name: call.Name}
		if !crmAssistantReadOnlyTools[call.Name] {
			record.Reason = "工具不在服务端只读白名单"
		} else if err := s.guard.AllowsAccount(ctx, scope, call.AccountID); err != nil {
			record.Reason = "账号超出员工授权范围"
		} else if err := s.guard.AllowsIndustry(ctx, scope, call.Industry); err != nil {
			record.Reason = "行业超出员工授权范围"
		} else if s.executor == nil {
			record.Reason = "未装配只读检索出口"
		} else {
			rows, err := s.executor.Execute(ctx, scope, call)
			if err != nil {
				record.Reason = "检索失败: " + err.Error()
			} else {
				record.Allowed = true
				for _, ev := range rows {
					if ev.Ref == "" || seen[ev.Ref] {
						continue
					}
					seen[ev.Ref] = true
					answer.Evidence = append(answer.Evidence, ev)
					if record.EvidenceRef == "" {
						record.EvidenceRef = ev.Ref
					}
				}
			}
		}
		answer.ToolCalls = append(answer.ToolCalls, record)
		if s.audit != nil {
			s.audit.RecordToolCall(ctx, scope, record)
		}
	}

	if s.model == nil {
		answer.Status = "model_unavailable"
		return answer, nil
	}
	text, refs, err := s.model.Answer(ctx, question, answer.Evidence)
	if err != nil {
		answer.Status = "model_unavailable"
		return answer, nil
	}
	// 引用必须逐条命中实际证据；伪造/越权引用一律不计入，全部失效则不给结论。
	valid := make([]string, 0, len(refs))
	for _, ref := range refs {
		if seen[ref] {
			valid = append(valid, ref)
		}
	}
	if len(valid) == 0 || len(answer.Evidence) == 0 {
		answer.Status = "degraded_no_evidence"
		return answer, nil
	}
	answer.Text = text
	return answer, nil
}

// ---- CRM-23：行动草稿审批闭环 ----

const (
	CRMActionApprovalDraftPending  = "pending"
	CRMActionApprovalDraftApproved = "approved"
	CRMActionApprovalDraftRejected = "rejected"
	CRMActionApprovalDraftExecuted = "executed"
	CRMActionApprovalDraftExpired  = "expired"
)

var crmActionBusinessTypes = map[string]bool{"opportunity": true, "followup": true}

// CRMActionApprovalDraft 由助手建议生成的待审批业务动作（商机/跟进），不直接落地。
type CRMActionApprovalDraft struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	TenantID      uint      `json:"tenant_id"`
	EmployeeID    uint      `json:"employee_id"`
	BusinessType  string    `json:"business_type"`
	PayloadJSON   string    `json:"payload_json"`
	Evidence      []string  `json:"evidence"`
	ObjectVersion string    `json:"object_version"` // 审批/执行时复核的业务对象版本
	Status        string    `json:"status"`
	ReceiptID     string    `json:"receipt_id,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	Version       uint64    `json:"version"` // 草稿自身 CAS 版本
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (d *CRMActionApprovalDraft) scope() CRMAssistantScope {
	return CRMAssistantScope{
		TenantID: d.TenantID, EmployeeID: d.EmployeeID, SessionID: d.SessionID,
		CustomerDataScope: CustomerDataScope{UserID: d.EmployeeID},
	}
}

// CRMActionApprovalDraftStore 草稿存储接缝（生产注入耐久实现；内存实现仅供测试）。
type CRMActionApprovalDraftStore interface {
	Create(ctx context.Context, d *CRMActionApprovalDraft) error
	Get(ctx context.Context, id string) (*CRMActionApprovalDraft, error)
	UpdateIfVersion(ctx context.Context, d *CRMActionApprovalDraft, expect uint64) error
}

// inMemoryCRMActionApprovalDraftStore 线程安全的内存草稿仓；返回副本，杜绝外部绕过 CAS 改动。
type inMemoryCRMActionApprovalDraftStore struct {
	mu   sync.Mutex
	rows map[string]*CRMActionApprovalDraft
}

func NewInMemoryCRMActionApprovalDraftStore() CRMActionApprovalDraftStore {
	return &inMemoryCRMActionApprovalDraftStore{rows: map[string]*CRMActionApprovalDraft{}}
}

func (s *inMemoryCRMActionApprovalDraftStore) Create(_ context.Context, d *CRMActionApprovalDraft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d == nil || d.ID == "" {
		return errors.New("草稿 ID 不能为空")
	}
	if _, exists := s.rows[d.ID]; exists {
		return fmt.Errorf("草稿已存在: %s", d.ID)
	}
	cp := *d
	cp.Version = 1
	s.rows[d.ID] = &cp
	return nil
}

func (s *inMemoryCRMActionApprovalDraftStore) Get(_ context.Context, id string) (*CRMActionApprovalDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return nil, ErrAssistantDraftNotFound
	}
	cp := *row
	return &cp, nil
}

func (s *inMemoryCRMActionApprovalDraftStore) UpdateIfVersion(_ context.Context, d *CRMActionApprovalDraft, expect uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[d.ID]
	if !ok {
		return ErrAssistantDraftNotFound
	}
	if cur.Version != expect {
		return ErrAssistantVersionConflict
	}
	cp := *d
	cp.Version = expect + 1
	s.rows[d.ID] = &cp
	return nil
}

// CRMActionObjectChecker 审批与执行前的对象权限/版本复核接缝。
type CRMActionObjectChecker interface {
	Verify(ctx context.Context, scope CRMAssistantScope, draft *CRMActionApprovalDraft) error
}

type crmActionObjectChecker struct{}

func (crmActionObjectChecker) Verify(_ context.Context, scope CRMAssistantScope, draft *CRMActionApprovalDraft) error {
	if len(draft.Evidence) == 0 || strings.TrimSpace(draft.ObjectVersion) == "" {
		return errors.New("行动草稿缺少证据或对象版本，禁止执行")
	}
	if scope.TenantID != draft.TenantID || scope.EmployeeID != draft.EmployeeID {
		return ErrAssistantForbidden
	}
	return nil
}

// CRMActionApprovalExecutor 幂等业务执行接缝（复用既有商机/跟进接口，返回业务对象回执 ID）。
type CRMActionApprovalExecutor interface {
	Execute(ctx context.Context, scope CRMAssistantScope, draft *CRMActionApprovalDraft) (receiptID string, err error)
}

// CRMActionAudit 审批/执行审计接缝。
type CRMActionAudit interface {
	Record(ctx context.Context, draftID, action, detail string)
}

// CRMActionApprovalService 行动草稿服务（CRM-23）。
type CRMActionApprovalService struct {
	guard    CRMAssistantScopeGuard
	store    CRMActionApprovalDraftStore
	checker  CRMActionObjectChecker
	executor CRMActionApprovalExecutor
	audit    CRMActionAudit
	now      func() time.Time
}

func NewCRMActionApprovalService(store CRMActionApprovalDraftStore) *CRMActionApprovalService {
	if store == nil {
		store = NewInMemoryCRMActionApprovalDraftStore()
	}
	return &CRMActionApprovalService{
		guard: crmAssistantScopeGuard{}, store: store,
		checker: crmActionObjectChecker{}, now: time.Now,
	}
}

func (s *CRMActionApprovalService) SetExecutor(e CRMActionApprovalExecutor) {
	if e != nil {
		s.executor = e
	}
}
func (s *CRMActionApprovalService) SetChecker(c CRMActionObjectChecker) {
	if c != nil {
		s.checker = c
	}
}
func (s *CRMActionApprovalService) SetAudit(a CRMActionAudit) {
	if a != nil {
		s.audit = a
	}
}
func (s *CRMActionApprovalService) SetGuard(g CRMAssistantScopeGuard) {
	if g != nil {
		s.guard = g
	}
}

// CreateDraft 从助手建议生成待审批草稿；不写任何业务对象。
func (s *CRMActionApprovalService) CreateDraft(ctx context.Context, scope CRMAssistantScope, businessType, payload string, evidence []string, objectVersion string) (*CRMActionApprovalDraft, error) {
	if err := s.guard.OwnsSession(ctx, scope); err != nil {
		return nil, err
	}
	if !crmActionBusinessTypes[businessType] {
		return nil, fmt.Errorf("不支持的行动业务类型: %s", businessType)
	}
	now := s.now()
	d := &CRMActionApprovalDraft{
		ID: fmt.Sprintf("draft_%d", now.UnixNano()), SessionID: scope.SessionID,
		TenantID: scope.TenantID, EmployeeID: scope.EmployeeID,
		BusinessType: businessType, PayloadJSON: payload,
		Evidence: append([]string(nil), evidence...), ObjectVersion: objectVersion,
		Status: CRMActionApprovalDraftPending, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := s.store.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// loadOwned 读取草稿并复核会话归属；过期草稿惰性标记为 expired 并返回冲突。
func (s *CRMActionApprovalService) loadOwned(ctx context.Context, scope CRMAssistantScope, id string) (*CRMActionApprovalDraft, error) {
	d, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.guard.OwnsSession(ctx, d.scope()); err != nil || scope.EmployeeID != d.EmployeeID || scope.TenantID != d.TenantID {
		return nil, ErrAssistantForbidden
	}
	if s.now().After(d.ExpiresAt) && (d.Status == CRMActionApprovalDraftPending || d.Status == CRMActionApprovalDraftApproved) {
		_ = s.store.UpdateIfVersion(ctx, &CRMActionApprovalDraft{ID: d.ID, Status: CRMActionApprovalDraftExpired, Version: d.Version + 1}, d.Version)
		return nil, fmt.Errorf("%w: 草稿已过期", ErrAssistantVersionConflict)
	}
	return d, nil
}

// Approve 审批：重验权限与对象版本，CAS 改状态；重复审批幂等。
// expectedVersion 可选，调用方持有旧版本时可显式要求 CAS，防止并发覆盖。
func (s *CRMActionApprovalService) Approve(ctx context.Context, scope CRMAssistantScope, id string, expectedVersion ...uint64) (*CRMActionApprovalDraft, error) {
	d, err := s.loadOwned(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if len(expectedVersion) > 0 && d.Version != expectedVersion[0] {
		return nil, ErrAssistantVersionConflict
	}
	if d.Status == CRMActionApprovalDraftApproved || d.Status == CRMActionApprovalDraftExecuted {
		return d, nil
	}
	if d.Status != CRMActionApprovalDraftPending {
		return nil, fmt.Errorf("草稿状态 %s 不可审批", d.Status)
	}
	if s.checker == nil || s.checker.Verify(ctx, scope, d) != nil {
		return nil, ErrAssistantForbidden
	}
	next := *d
	next.Status = CRMActionApprovalDraftApproved
	next.Version = d.Version + 1
	if err := s.store.UpdateIfVersion(ctx, &next, d.Version); err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Record(ctx, id, "approve", "行动草稿审批通过")
	}
	return &next, nil
}

// Reject 拒绝草稿（不写业务）；重复拒绝幂等。
func (s *CRMActionApprovalService) Reject(ctx context.Context, scope CRMAssistantScope, id, reason string) (*CRMActionApprovalDraft, error) {
	d, err := s.loadOwned(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if d.Status == CRMActionApprovalDraftRejected {
		return d, nil
	}
	next := *d
	next.Status, next.Reason, next.Version = CRMActionApprovalDraftRejected, reason, d.Version+1
	if err := s.store.UpdateIfVersion(ctx, &next, d.Version); err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Record(ctx, id, "reject", reason)
	}
	return &next, nil
}

// Execute 执行已审批草稿：执行前再次复核权限/版本，幂等返回业务回执。
// expectedVersion 可选，传入陈旧版本时直接冲突，防止审批后被并发改动仍执行。
func (s *CRMActionApprovalService) Execute(ctx context.Context, scope CRMAssistantScope, id string, expectedVersion ...uint64) (*CRMActionApprovalDraft, error) {
	d, err := s.loadOwned(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if d.Status == CRMActionApprovalDraftExecuted {
		return d, nil
	}
	if len(expectedVersion) > 0 && d.Version != expectedVersion[0] {
		return nil, ErrAssistantVersionConflict
	}
	if d.Status != CRMActionApprovalDraftApproved {
		return nil, fmt.Errorf("草稿未审批或已失效（%s），禁止执行", d.Status)
	}
	if s.checker == nil || s.checker.Verify(ctx, scope, d) != nil {
		return nil, ErrAssistantForbidden
	}
	if s.executor == nil {
		return nil, errors.New("未装配业务执行出口，禁止执行")
	}
	receipt, err := s.executor.Execute(ctx, scope, d)
	if err != nil {
		return nil, fmt.Errorf("执行行动草稿失败: %w", err)
	}
	next := *d
	next.Status, next.ReceiptID, next.Version = CRMActionApprovalDraftExecuted, receipt, d.Version+1
	if err := s.store.UpdateIfVersion(ctx, &next, d.Version); err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Record(ctx, id, "execute", "业务回执: "+receipt)
	}
	return &next, nil
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	ErrCommerceInput    = errors.New("commerce invalid input")
	ErrCommerceDenied   = errors.New("commerce permission denied")
	ErrCommerceConflict = errors.New("commerce revision conflict")
	ErrCommerceProvider = errors.New("commerce pending live validation")
	ErrCommerceBusy     = errors.New("commerce resource busy")
)

type CommerceService interface {
	Profiles(context.Context, uint) ([]dto.CommerceProfileResponse, error)
	Read(context.Context, uint, dto.CommerceTarget) (dto.CommerceOrderResponse, error)
	Propose(context.Context, uint, dto.CommerceProposeRequest) (dto.CommerceOperationResponse, error)
	List(context.Context, uint, string, string, string) (dto.CommerceOperationListResponse, error)
	Get(context.Context, uint, string) (dto.CommerceOperationResponse, error)
	Transition(context.Context, uint, string, string, dto.CommerceTransitionRequest) (dto.CommerceOperationResponse, error)
	Evidence(context.Context, uint, string, bool, dto.CommerceEvidenceRequest) (dto.CommerceOperationResponse, error)
	ConfigurePolicy(context.Context, uint, dto.CommercePolicyRequest) (map[string]any, error)
	ConfigureMember(context.Context, uint, dto.CommerceMemberRequest) (map[string]any, error)
	RunDue(context.Context, int) (int, error)
}
type commerceService struct {
	repo     repository.CommerceRepository
	adapters map[string]CommerceAdapter
	now      func() time.Time
}

func NewCommerceService(repo repository.CommerceRepository, adapters map[string]CommerceAdapter) CommerceService {
	cloned := make(map[string]CommerceAdapter, len(adapters))
	for k, v := range adapters {
		if v != nil {
			cloned[k] = v
		}
	}
	return &commerceService{repo: repo, adapters: cloned, now: time.Now}
}
func commerceTenant(ctx context.Context) (uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return 0, ErrCommerceDenied
	}
	return id, nil
}
func commerceDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func commerceEncode(v any) string                    { b, _ := json.Marshal(v); return string(b) }
func commerceMutate(a *repository.CommerceAggregate) { a.Operation.Version++; a.Dirty = true }
func commerceActive(u model.SystemUser) bool         { return u.ID != 0 && u.Enabled && u.Status == 1 }
func commerceReadGuard(a *repository.CommerceAggregate, p dto.CommerceProfileResponse) error {
	if !commerceActive(a.Actor) || !a.Member.CanRead || a.Member.ID == "" || a.Order.ID == 0 || a.Order.Platform != p.Product || a.Binding.ID == 0 || a.Binding.Revoked || a.Binding.ShopID != a.Policy.ShopID || a.Binding.ConnectionID != a.Connection.ID || a.Binding.ConsentVersion != a.Connection.ConsentVersion {
		return ErrCommerceDenied
	}
	return nil
}
func (s *commerceService) Profiles(ctx context.Context, actor uint) ([]dto.CommerceProfileResponse, error) {
	if _, e := commerceTenant(ctx); e != nil || actor == 0 {
		return nil, ErrCommerceDenied
	}
	p := commerceProfiles()
	for i := range p {
		p[i].ProviderConfigured = s.adapters[p[i].ID] != nil
	}
	return p, nil
}
func commerceTargetKey(actor uint, t dto.CommerceTarget, p dto.CommerceProfileResponse) repository.CommerceKey {
	return repository.CommerceKey{ActorID: actor, Profile: p.ID, Product: p.Product, ShopID: t.ShopID, OrderID: t.OrderID}
}
func (s *commerceService) Read(ctx context.Context, actor uint, t dto.CommerceTarget) (dto.CommerceOrderResponse, error) {
	var out dto.CommerceOrderResponse
	p, ok := commerceProfile(t.Profile)
	if !ok || t.OrderID == 0 || t.ShopID == "" {
		return out, ErrCommerceInput
	}
	var request CommerceAdapterRequest
	e := s.repo.Transact(ctx, commerceTargetKey(actor, t, p), func(a *repository.CommerceAggregate) error {
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		out = dto.CommerceOrderResponse{CommerceTarget: t, ExternalOrderID: a.Order.OrderID, Status: a.Order.Status, SourceVersion: a.Order.UpdatedAt.UTC().Format(time.RFC3339Nano), MirrorFetchedAt: a.Order.UpdatedAt, MaskedFields: []string{"address", "phone", "buyer"}, Source: "external_orders", ReadRestriction: "mirror_only"}
		request = commerceRequest(a, p, dto.CommerceParameters{})
		return nil
	})
	if e != nil {
		return out, e
	}
	if provider := s.adapters[p.ID]; provider != nil {
		snap, err := provider.Read(ctx, request)
		if err == nil {
			out.Status = snap.NativeState
			out.SourceVersion = snap.Version
			out.Currency = snap.Currency
			out.RefundableMinor = snap.RefundableMinor
			out.LiveFetchedAt = &snap.FetchedAt
			out.Source = snap.Source
			out.ReadRestriction = ""
		}
	}
	return out, nil
}
func commerceMissing(action string, p dto.CommerceParameters) []string {
	m := []string{}
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			m = append(m, k)
		}
	}
	switch action {
	case "address.change":
		add("recipient", p.Recipient)
		add("phone", p.Phone)
		add("address", p.Address)
		add("province", p.Province)
		add("city", p.City)
		add("district", p.District)
	case "refund", "refund.partial", "refund.difference":
		if p.AmountMinor <= 0 {
			m = append(m, "amount_minor")
		}
		add("currency", p.Currency)
		add("case_ref", p.CaseRef)
		add("payment_path", p.PaymentPath)
	case "return.approve":
		add("case_ref", p.CaseRef)
		add("warehouse_ref", p.WarehouseRef)
		add("line_id", p.LineID)
		if p.Quantity <= 0 {
			m = append(m, "quantity")
		}
	case "exchange.ship", "reship":
		add("case_ref", p.CaseRef)
		add("sku", p.SKU)
		add("logistics_ref", p.LogisticsRef)
		add("line_id", p.LineID)
		if p.Quantity <= 0 {
			m = append(m, "quantity")
		}
	case "return.solution":
		add("case_ref", p.CaseRef)
		add("reason", p.Reason)
	case "cancel":
		add("reason", p.Reason)
	case "managed.observe":
		add("case_ref", p.CaseRef)
	}
	return m
}
func commerceParametersValid(p dto.CommerceParameters) bool {
	if p.AmountMinor < 0 || p.Quantity < 0 || p.AmountMinor > 1_000_000_000_000 || p.Quantity > 1_000_000 {
		return false
	}
	if p.Currency != "" && (len(p.Currency) != 3 || p.Currency != strings.ToUpper(p.Currency)) {
		return false
	}
	if p.PaymentPath != "" && !slices.Contains([]string{"cash", "store_credit", "manual"}, p.PaymentPath) {
		return false
	}
	for _, v := range []string{p.Recipient, p.Phone, p.Address, p.Province, p.City, p.District, p.LineID, p.CaseRef, p.Reason, p.SKU, p.LogisticsRef, p.WarehouseRef} {
		if len(v) > 1024 || strings.ContainsRune(v, 0) {
			return false
		}
	}
	return len(p.Acknowledgements) <= 32
}
func (s *commerceService) Propose(ctx context.Context, actor uint, in dto.CommerceProposeRequest) (dto.CommerceOperationResponse, error) {
	var out dto.CommerceOperationResponse
	p, ok := commerceProfile(in.Profile)
	if !ok || in.ShopID == "" || in.OrderID == 0 || in.IntentID == "" || len(in.IntentID) > 120 || in.Revision < 1 || !commerceParametersValid(in.Parameters) {
		return out, ErrCommerceInput
	}
	if _, ok = commerceAction(p, in.Action); !ok {
		return out, ErrCommerceInput
	}
	sortAck := slices.Clone(in.Parameters.Acknowledgements)
	slices.Sort(sortAck)
	in.Parameters.Acknowledgements = slices.Compact(sortAck)
	hash := commerceDigest(struct {
		Action            string
		Order             uint
		Expected          string
		AfterSale, Change uint
		Parent            string
		Parameters        dto.CommerceParameters
	}{in.Action, in.OrderID, in.ExpectedVersion, in.AfterSaleID, in.OrderChangeID, in.ParentID, in.Parameters})
	key := commerceTargetKey(actor, in.CommerceTarget, p)
	key.AfterSaleID, key.OrderChangeID, key.ParentID = in.AfterSaleID, in.OrderChangeID, in.ParentID
	key.Action, key.IntentID, key.Revision = in.Action, in.IntentID, in.Revision
	e := s.repo.Transact(ctx, key, func(a *repository.CommerceAggregate) error {
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		if a.Exists {
			if a.Operation.ParameterHash != hash {
				return ErrCommerceConflict
			}
			out = commerceOperationDTO(a.Operation, a.Attempts, a.Approval)
			return nil
		}
		if in.AfterSaleID != 0 && (a.AfterSale.ID != in.AfterSaleID || a.AfterSale.Platform != p.Product || a.AfterSale.OrderID != a.Order.OrderID) {
			return ErrCommerceDenied
		}
		if in.OrderChangeID != 0 && (a.OrderChange.ID != in.OrderChangeID || a.OrderChange.Platform != p.Product || a.OrderChange.OrderID != a.Order.OrderID) {
			return ErrCommerceDenied
		}
		if in.ParentID != "" && (a.Parent.ID == "" || a.Parent.OrderID != in.OrderID || a.Parent.ShopID != in.ShopID || a.Parent.State != "succeeded") {
			return ErrCommerceConflict
		}
		state := "draft"
		if len(commerceMissing(in.Action, in.Parameters)) > 0 {
			state = "needs_information"
		}
		a.Operation = model.CommerceOperation{ID: uuid.NewString(), Profile: p.ID, Product: p.Product, ShopID: in.ShopID, Action: in.Action, IntentID: in.IntentID, Revision: in.Revision, OrderID: in.OrderID, ExternalOrderID: a.Order.OrderID, ParameterHash: hash, Parameters: commerceEncode(in.Parameters), Snapshot: commerceEncode(CommerceSnapshot{Version: in.ExpectedVersion}), State: state, Mode: p.Mode, WriteOwner: a.Policy.WriteOwner, ActorID: actor, OwnerID: actor, PolicyVersion: a.Policy.Version, MemberVersion: a.Member.Version, ConnectionID: a.Connection.ID, ConnectionVersion: a.Connection.Version, CredentialVersion: a.Connection.CredentialVersion, RecoveryEpoch: a.Policy.RecoveryEpoch, Version: 1, DueAt: s.now(), ExpiresAt: s.now().Add(15 * time.Minute), EvidenceLevel: "none", FundsState: "pending", AmountMinor: in.Parameters.AmountMinor, Currency: in.Parameters.Currency, Quantity: in.Parameters.Quantity, ParentID: in.ParentID, AfterSaleID: in.AfterSaleID, OrderChangeID: in.OrderChangeID}
		a.Dirty = true
		out = commerceOperationDTO(a.Operation, nil, model.CommerceApproval{})
		return nil
	})
	return out, e
}
func commerceOperationDTO(o model.CommerceOperation, attempts []model.CommerceAttempt, approval model.CommerceApproval) dto.CommerceOperationResponse {
	var p dto.CommerceParameters
	_ = json.Unmarshal([]byte(o.Parameters), &p)
	var snap CommerceSnapshot
	_ = json.Unmarshal([]byte(o.Snapshot), &snap)
	summary := "尚未执行"
	switch o.State {
	case "queued", "executing":
		summary = "已提交本地命令，结果未定"
	case "accepted", "processing":
		summary = "已受理，尚待核验"
	case "awaiting_customer":
		summary = "待买家确认"
	case "outcome_unknown", "reconciling", "manual_review":
		summary = "结果待核明，请勿重复执行"
	case "succeeded":
		summary = "平台动作后置条件已核验"
	case "failed":
		summary = "已核明未完成"
	}
	actual := int64(0)
	for _, a := range attempts {
		if a.Kind == "submit" && a.DispatchCommitted {
			actual++
		}
	}
	return dto.CommerceOperationResponse{ID: o.ID, CommerceTarget: dto.CommerceTarget{Profile: o.Profile, ShopID: o.ShopID, OrderID: o.OrderID}, Action: o.Action, IntentID: o.IntentID, Revision: o.Revision, Version: o.Version, State: o.State, NativeState: o.NativeState, Mode: o.Mode, WriteOwner: o.WriteOwner, ParameterHash: o.ParameterHash, PreflightHash: o.PreflightHash, MissingFields: commerceMissing(o.Action, p), Obligations: snap.Obligations, AmountMinor: o.AmountMinor, Currency: o.Currency, Quantity: o.Quantity, ExpiresAt: o.ExpiresAt, DueAt: o.DueAt, UnknownSince: o.UnknownSince, OwnerID: o.OwnerID, ApproverID: approval.ActorID, EvidenceLevel: o.EvidenceLevel, EvidenceRef: o.EvidenceRef, ExternalRef: o.ExternalRef, FundsState: o.FundsState, ManualState: o.ManualState, FailureReason: o.FailureReason, ActualAttempts: actual, ResultSummary: summary, LiveStatus: "pending_live_validation", AfterSaleID: o.AfterSaleID, OrderChangeID: o.OrderChangeID}
}
func (s *commerceService) List(ctx context.Context, actor uint, profile, shop, state string) (dto.CommerceOperationListResponse, error) {
	out := dto.CommerceOperationListResponse{List: []dto.CommerceOperationResponse{}}
	rows, e := s.repo.List(ctx, actor, profile, shop, state, 100)
	for _, r := range rows {
		out.List = append(out.List, commerceOperationDTO(r, nil, model.CommerceApproval{}))
	}
	out.Total = int64(len(rows))
	return out, e
}
func (s *commerceService) Get(ctx context.Context, actor uint, id string) (dto.CommerceOperationResponse, error) {
	var out dto.CommerceOperationResponse
	e := s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		p, ok := commerceProfile(a.Operation.Profile)
		if !ok {
			return ErrCommerceInput
		}
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		out = commerceOperationDTO(a.Operation, a.Attempts, a.Approval)
		return nil
	})
	return out, e
}
func commerceRequest(a *repository.CommerceAggregate, p dto.CommerceProfileResponse, params dto.CommerceParameters) CommerceAdapterRequest {
	return CommerceAdapterRequest{TenantID: a.Actor.TenantID, Profile: p.ID, Product: p.Product, Region: a.Binding.Region, ShopID: a.Policy.ShopID, ExternalOrderID: a.Order.OrderID, Action: a.Operation.Action, OperationID: a.Operation.ID, BusinessKey: a.Operation.ID, ExpectedVersion: commerceSnapshot(a.Operation).Version, ExternalRef: a.Operation.ExternalRef, CredentialRef: a.Connection.ID, CredentialVersion: a.Connection.CredentialVersion, WriteOwner: a.Policy.WriteOwner, Parameters: params}
}
func commerceSnapshot(o model.CommerceOperation) CommerceSnapshot {
	var x CommerceSnapshot
	_ = json.Unmarshal([]byte(o.Snapshot), &x)
	return x
}
func commerceParams(o model.CommerceOperation) dto.CommerceParameters {
	var x dto.CommerceParameters
	_ = json.Unmarshal([]byte(o.Parameters), &x)
	return x
}
func (s *commerceService) ConfigurePolicy(ctx context.Context, actor uint, in dto.CommercePolicyRequest) (map[string]any, error) {
	tid, e := commerceTenant(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := commerceProfile(in.Profile); !ok || in.ShopID == "" || in.OwnerID == 0 || in.RecoveryEpoch < 1 || !slices.Contains([]string{"dual", "single_owner"}, in.ApprovalMode) || !(in.WriteOwner == "native" || in.WriteOwner == "official_ui" || in.WriteOwner == "platform_managed" || strings.HasPrefix(in.WriteOwner, "erp:")) {
		return nil, ErrCommerceInput
	}
	row, e := s.repo.ConfigurePolicy(ctx, actor, in.ExpectedVersion, model.CommercePolicy{ID: commerceDigest([]any{tid, in.Profile, in.ShopID}), Profile: in.Profile, ShopID: in.ShopID, WriteEnabled: in.WriteEnabled, WriteOwner: in.WriteOwner, ApprovalMode: in.ApprovalMode, OwnerID: in.OwnerID, RecoveryEpoch: in.RecoveryEpoch})
	return map[string]any{"version": row.Version, "write_enabled": row.WriteEnabled, "approval_mode": row.ApprovalMode, "live_status": "pending_live_validation"}, e
}
func (s *commerceService) ConfigureMember(ctx context.Context, actor uint, in dto.CommerceMemberRequest) (map[string]any, error) {
	tid, e := commerceTenant(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := commerceProfile(in.Profile); !ok || in.ShopID == "" || in.ActorID == 0 || in.LimitMinor < 0 || (in.LimitMinor > 0 && len(in.Currency) != 3) {
		return nil, ErrCommerceInput
	}
	row, e := s.repo.ConfigureMember(ctx, actor, in.ExpectedVersion, model.CommerceMember{ID: commerceDigest([]any{tid, in.Profile, in.ShopID, in.ActorID}), Profile: in.Profile, ShopID: in.ShopID, ActorID: in.ActorID, CanRead: in.CanRead, CanExecute: in.CanExecute, CanApprove: in.CanApprove, CanReview: in.CanReview, LimitMinor: in.LimitMinor, Currency: in.Currency})
	return map[string]any{"version": row.Version, "actor_id": row.ActorID}, e
}

func commerceWriteGuard(a *repository.CommerceAggregate, now time.Time) error {
	o := a.Operation
	if !commerceActive(a.Actor) || !a.Member.CanExecute || !a.Policy.WriteEnabled || a.Policy.Version != o.PolicyVersion || a.Policy.RecoveryEpoch != o.RecoveryEpoch || a.Policy.WriteOwner != o.WriteOwner {
		return ErrCommerceDenied
	}
	c := a.Connection
	if c.ID == 0 || c.Revoked || c.CredentialVersion == 0 || c.Version != o.ConnectionVersion || c.CredentialVersion != o.CredentialVersion || c.RefreshFailed || c.RefreshUntil != nil || c.AttemptExpiresAt != nil || c.ExpiresAt == nil || !c.ExpiresAt.After(now) {
		return ErrCommerceDenied
	}
	actionScope := "commerce." + o.Action
	if !slices.Contains(c.Scopes, actionScope) && !slices.Contains(c.Scopes, o.Action) {
		return ErrCommerceDenied
	}
	if o.Mode == commerceAPI {
		e := a.Capability
		if e.ID == 0 || !e.Documentation || !e.Implementation || e.Simulated || e.CredentialVersion != c.CredentialVersion || e.ConnectionVersion != c.Version || e.ConnectionID != c.ID || !e.ExpiresAt.After(now) || e.Carrier != commerceAPI {
			return ErrCommerceDenied
		}
	}
	return nil
}
func commerceApprovalGuard(a *repository.CommerceAggregate, now time.Time) error {
	o := a.Operation
	ap := a.Approval
	if ap.ID == "" || ap.Digest != o.PreflightHash || !ap.ExpiresAt.After(now) || ap.PolicyVersion != a.Policy.Version || ap.MemberVersion != a.Approver.Version || !a.Approver.CanApprove || !commerceActive(a.ApproverUser) {
		return ErrCommerceDenied
	}
	if o.AmountMinor > 0 && (a.Approver.LimitMinor < o.AmountMinor || a.Approver.Currency != o.Currency) {
		return ErrCommerceDenied
	}
	if a.Policy.ApprovalMode == "dual" && ap.ActorID == o.ActorID {
		return ErrCommerceDenied
	}
	if a.Policy.ApprovalMode == "single_owner" && ap.ActorID != a.Policy.OwnerID {
		return ErrCommerceDenied
	}
	return nil
}

func (s *commerceService) Transition(ctx context.Context, actor uint, id, verb string, in dto.CommerceTransitionRequest) (dto.CommerceOperationResponse, error) {
	if verb == "preflight" {
		return s.preflight(ctx, actor, id, in)
	}
	if verb == "observe" {
		return s.execute(ctx, actor, id, true)
	}
	var out dto.CommerceOperationResponse
	e := s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		p, _ := commerceProfile(a.Operation.Profile)
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		o := &a.Operation
		if o.Version != in.ExpectedVersion {
			return ErrCommerceConflict
		}
		switch verb {
		case "approve":
			if o.State != "awaiting_approval" || in.PreflightHash != o.PreflightHash || !o.ExpiresAt.After(s.now()) || !a.Member.CanApprove {
				return ErrCommerceDenied
			}
			if o.AmountMinor > 0 && (a.Member.LimitMinor < o.AmountMinor || a.Member.Currency != o.Currency) {
				return ErrCommerceDenied
			}
			if a.Policy.ApprovalMode == "dual" && actor == o.ActorID {
				return ErrCommerceDenied
			}
			if a.Policy.ApprovalMode == "single_owner" && actor != a.Policy.OwnerID {
				return ErrCommerceDenied
			}
			ap := model.CommerceApproval{ID: uuid.NewString(), OperationID: o.ID, ActorID: actor, Digest: o.PreflightHash, PolicyVersion: a.Policy.Version, MemberVersion: a.Member.Version, ExpiresAt: s.now().Add(15 * time.Minute)}
			a.NewApproval = &ap
			a.Approval = ap
			o.ApprovalID = ap.ID
			o.State = "ready"
		case "submit":
			if o.State != "ready" || in.PreflightHash != o.PreflightHash {
				return ErrCommerceConflict
			}
			if e := commerceWriteGuard(a, s.now()); e != nil {
				return e
			}
			if e := commerceApprovalGuard(a, s.now()); e != nil {
				return e
			}
			if a.Resource.ActiveOperation != "" && a.Resource.ActiveOperation != o.ID {
				return ErrCommerceBusy
			}
			snap := commerceSnapshot(*o)
			if o.Mode == commerceAPI && (o.AmountMinor > snap.RefundableMinor || o.Quantity > snap.AvailableQuantity) {
				return ErrCommerceConflict
			}
			a.Resource.ActiveOperation = o.ID
			a.Resource.AmountMinor = o.AmountMinor
			a.Resource.Currency = o.Currency
			a.Resource.Quantity = o.Quantity
			a.Resource.Revision++
			o.State = "queued"
			o.DueAt = s.now()
		case "cancel":
			if o.DispatchedAt != nil || !slices.Contains([]string{"draft", "needs_information", "awaiting_approval", "ready", "queued"}, o.State) {
				return ErrCommerceConflict
			}
			if actor != o.ActorID && !a.Member.CanApprove {
				return ErrCommerceDenied
			}
			o.State = "cancelled"
			if a.Resource.ActiveOperation == o.ID {
				a.Resource.ActiveOperation = ""
				a.Resource.AmountMinor = 0
				a.Resource.Quantity = 0
			}
		case "claim":
			if o.Mode != commerceWorkbench || o.DispatchedAt != nil || o.State != "queued" {
				return ErrCommerceConflict
			}
			if e := commerceWriteGuard(a, s.now()); e != nil {
				return e
			}
			if e := commerceApprovalGuard(a, s.now()); e != nil {
				return e
			}
			if a.Resource.ActiveOperation != o.ID {
				return ErrCommerceBusy
			}
			o.OwnerID = actor
			o.ManualState = "manual_dispatched"
			o.State = "processing"
			now := s.now()
			o.DispatchedAt = &now
			a.NewAttempt = &model.CommerceAttempt{ID: uuid.NewString(), OperationID: o.ID, Kind: "manual", Fence: o.Fence, DispatchCommitted: true, State: "processing"}
		default:
			return ErrCommerceInput
		}
		commerceMutate(a)
		out = commerceOperationDTO(*o, a.Attempts, a.Approval)
		return nil
	})
	if e == nil && verb == "submit" && out.Mode != commerceWorkbench {
		return s.execute(ctx, actor, id, false)
	}
	return out, e
}
func (s *commerceService) preflight(ctx context.Context, actor uint, id string, in dto.CommerceTransitionRequest) (dto.CommerceOperationResponse, error) {
	var out dto.CommerceOperationResponse
	var req CommerceAdapterRequest
	var p dto.CommerceProfileResponse
	var baseline model.CommerceOperation
	e := s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		p, _ = commerceProfile(a.Operation.Profile)
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		if a.Operation.Version != in.ExpectedVersion || a.Operation.DispatchedAt != nil || !slices.Contains([]string{"draft", "needs_information", "awaiting_approval", "ready"}, a.Operation.State) {
			return ErrCommerceConflict
		}
		if len(commerceMissing(a.Operation.Action, commerceParams(a.Operation))) > 0 {
			return ErrCommerceInput
		}
		req = commerceRequest(a, p, commerceParams(a.Operation))
		baseline = a.Operation
		return nil
	})
	if e != nil {
		return out, e
	}
	var quote CommerceQuote
	if provider := s.adapters[p.ID]; provider != nil {
		callctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		quote, e = provider.Preflight(callctx, req)
		if e != nil {
			return out, ErrCommerceProvider
		}
		if !quote.FeesKnown {
			return out, ErrCommerceConflict
		}
	} else if p.Mode == commerceAPI {
		return out, ErrCommerceProvider
	} else {
		quote = CommerceQuote{Snapshot: CommerceSnapshot{Version: commerceSnapshot(baseline).Version, FetchedAt: s.now(), Source: "manual_preflight"}, FeesKnown: true}
	}
	contract, _ := commerceAction(p, baseline.Action)
	quote.Snapshot.Obligations = append(slices.Clone(contract.Obligations), quote.Obligations...)
	params := commerceParams(baseline)
	for _, ob := range quote.Snapshot.Obligations {
		if !slices.Contains(params.Acknowledgements, ob) {
			return out, ErrCommerceInput
		}
	}
	if p.Mode == commerceAPI && (!slices.Contains(quote.Snapshot.AllowedActions, baseline.Action) || quote.Snapshot.Version == "" || s.now().Sub(quote.Snapshot.FetchedAt) > 30*time.Second) {
		return out, ErrCommerceConflict
	}
	if expected := commerceSnapshot(baseline).Version; expected != "" && p.Mode == commerceAPI && expected != quote.Snapshot.Version {
		return out, ErrCommerceConflict
	}
	e = s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		if a.Operation.Version != baseline.Version {
			return ErrCommerceConflict
		}
		if e := commerceWriteGuard(a, s.now()); e != nil {
			return e
		}
		a.Operation.Snapshot = commerceEncode(quote.Snapshot)
		a.Operation.State = "awaiting_approval"
		a.Operation.ApprovalID = ""
		a.Operation.ExpiresAt = s.now().Add(15 * time.Minute)
		a.Operation.PreflightHash = commerceDigest([]any{a.Operation.ParameterHash, quote, a.Policy.Version, a.Connection.Version, a.Connection.CredentialVersion, a.Policy.WriteOwner, a.Policy.RecoveryEpoch})
		commerceMutate(a)
		out = commerceOperationDTO(a.Operation, a.Attempts, a.Approval)
		return nil
	})
	return out, e
}

func (s *commerceService) Evidence(ctx context.Context, actor uint, id string, review bool, in dto.CommerceEvidenceRequest) (dto.CommerceOperationResponse, error) {
	var out dto.CommerceOperationResponse
	e := s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		p, _ := commerceProfile(a.Operation.Profile)
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		o := &a.Operation
		if o.Version != in.ExpectedVersion || o.Mode != commerceWorkbench || o.DispatchedAt == nil || o.State == "succeeded" {
			return ErrCommerceConflict
		}
		if !review {
			if !a.Member.CanExecute || actor != o.OwnerID || in.Reference == "" || len(in.Reference) > 160 || !slices.Contains([]string{"completed", "rejected", "unknown"}, in.Outcome) || !slices.Contains([]string{"pending", "succeeded", "failed", "unknown", ""}, in.FundsState) {
				return ErrCommerceDenied
			}
			ev := model.CommerceEvidence{ID: uuid.NewString(), OperationID: id, ActorID: actor, Source: "manual_attestation", Reference: in.Reference, NativeState: in.NativeState, FundsState: in.FundsState, Outcome: in.Outcome}
			a.NewEvidence = &ev
			o.ManualState = "manual_attested"
			o.EvidenceLevel = "manual_attested"
			o.EvidenceRef = in.Reference
			o.NativeState = in.NativeState
			o.State = "manual_review"
		} else {
			if !a.Member.CanReview {
				return ErrCommerceDenied
			}
			var chosen *model.CommerceEvidence
			for i := range a.Evidences {
				if a.Evidences[i].ID == in.EvidenceID {
					chosen = &a.Evidences[i]
				}
			}
			if chosen == nil || chosen.ReviewerID != 0 || (a.Policy.ApprovalMode == "dual" && chosen.ActorID == actor) {
				return ErrCommerceDenied
			}
			chosen.ReviewerID = actor
			now := s.now()
			chosen.ReviewedAt = &now
			a.UpdateEvidence = chosen
			o.EvidenceLevel = "manual_reviewed"
			o.FundsState = chosen.FundsState
			// A human statement remains review evidence; it cannot release an uncertain financial resource.
			if chosen.Outcome == "rejected" {
				o.FailureReason = "manual_reported_rejection"
			}
			o.State = "manual_review"
		}
		commerceMutate(a)
		out = commerceOperationDTO(*o, a.Attempts, a.Approval)
		return nil
	})
	return out, e
}

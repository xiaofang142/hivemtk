package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/system/install"
)

const PilotContractVersion = "pilot-contract.v1"

var (
	ErrPilotTenant      = errors.New("pilot contract requires matching tenant context")
	ErrPilotInvalid     = errors.New("invalid pilot contract boundary")
	ErrPilotUnavailable = errors.New("pilot contract unavailable")
	ErrPilotConflict    = repository.ErrPilotContractConflict
)

type pilotContractDocument struct {
	Contract  dto.PilotContract `json:"contract"`
	UpdatedBy uint              `json:"updated_by"`
	UpdatedAt string            `json:"updated_at"`
}

type PilotContractService interface {
	Load(ctx context.Context) (*dto.PilotContractResponse, error)
	Validate(ctx context.Context, in dto.PilotContract) (*dto.PilotContractResponse, error)
	Save(ctx context.Context, in dto.PilotContract, actor uint) (*dto.PilotContractResponse, error)
}

type pilotContractService struct {
	repo             repository.PilotContractRepository
	installationLoad func() (*install.Lock, error)
}

func NewPilotContractService(repo repository.PilotContractRepository, load func() (*install.Lock, error)) PilotContractService {
	if load == nil {
		load = install.Load
	}
	return &pilotContractService{repo: repo, installationLoad: load}
}

// identity 只读现有安装身份，不调用 GetStatus 的自动修复路径，不重铸身份。
func (s *pilotContractService) identity(ctx context.Context) (dto.PilotInstallation, uint, error) {
	id, bound := tenant.FromContext(ctx)
	if !bound {
		return dto.PilotInstallation{}, 0, ErrPilotTenant
	}
	if s == nil || s.installationLoad == nil {
		return dto.PilotInstallation{}, id, ErrPilotUnavailable
	}
	lock, err := s.installationLoad()
	if err != nil {
		return dto.PilotInstallation{}, id, ErrPilotUnavailable
	}
	if lock == nil {
		return dto.PilotInstallation{}, id, nil
	}
	return dto.PilotInstallation{InstallID: lock.InstallID, Initialized: lock.Initialized, Version: lock.Version}, id, nil
}

func (s *pilotContractService) Load(ctx context.Context) (*dto.PilotContractResponse, error) {
	identity, id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, ErrPilotUnavailable
	}
	raw, err := s.repo.Load(tenant.OnlyTenant(ctx, id))
	if err != nil {
		return nil, err
	}
	// 未配置时仅回传真实安装/租户基线；不预置 fake 草稿，避免样例信号冒充业务配置。
	doc := pilotContractDocument{Contract: dto.PilotContract{Version: PilotContractVersion, InstallID: identity.InstallID, TenantID: id}}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, ErrPilotUnavailable
		}
	}
	out := evaluatePilotContract(doc.Contract, identity, id)
	out.Configured = raw != ""
	out.UpdatedAt, out.UpdatedBy = doc.UpdatedAt, doc.UpdatedBy
	return out, nil
}

func (s *pilotContractService) Validate(ctx context.Context, in dto.PilotContract) (*dto.PilotContractResponse, error) {
	identity, id, err := s.identity(ctx)
	if err != nil {
		return nil, err
	}
	if in.TenantID != id {
		return nil, ErrPilotTenant
	}
	if in.Version != PilotContractVersion || in.InstallID == "" || in.InstallID != identity.InstallID ||
		(in.ProviderMode != "fake" && in.ProviderMode != "simulated") || in.EngineeringMinutes < 0 ||
		(in.FundsLimit != nil && in.FundsLimit.AmountMinor != nil && *in.FundsLimit.AmountMinor < 0) || in.ObservationMinutes < 0 {
		return nil, ErrPilotInvalid
	}
	return evaluatePilotContract(in, identity, id), nil
}

func (s *pilotContractService) Save(ctx context.Context, in dto.PilotContract, actor uint) (*dto.PilotContractResponse, error) {
	if actor == 0 {
		return nil, ErrPilotInvalid
	}
	out, err := s.Validate(ctx, in)
	if err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, ErrPilotUnavailable
	}
	scoped := tenant.OnlyTenant(ctx, out.TenantID)
	previous, err := s.repo.Load(scoped)
	if err != nil {
		return nil, err
	}
	current := pilotContractDocument{}
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &current); err != nil {
			return nil, ErrPilotUnavailable
		}
	}
	if in.Revision != current.Contract.Revision || in.Revision == ^uint64(0) {
		return nil, ErrPilotConflict
	}
	in.Revision++
	doc := pilotContractDocument{Contract: in, UpdatedBy: actor, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, ErrPilotInvalid
	}
	if err := s.repo.Save(scoped, previous, string(raw)); err != nil {
		return nil, err
	}
	out.Contract, out.UpdatedBy, out.UpdatedAt, out.Configured = in, doc.UpdatedBy, doc.UpdatedAt, true
	return out, nil
}

func evaluatePilotContract(in dto.PilotContract, identity dto.PilotInstallation, id uint) *dto.PilotContractResponse {
	out := &dto.PilotContractResponse{
		Contract: in, Installation: identity, TenantID: id,
		BaselineAvailable: identity.Initialized && identity.InstallID != "",
		Readiness:         "blocked", ProductionGate: "blocked", ValidationSignal: "pending_live_validation", Simulated: true,
		Blockers: []string{}, ProductionBlockers: []string{"pending_live_validation", "order_action_capability_unverified"},
		Capabilities: []dto.PilotCapabilityDeclaration{},
	}
	add := func(missing bool, reason string) {
		if missing {
			out.Blockers = append(out.Blockers, reason)
		}
	}
	blank := func(value string) bool { return strings.TrimSpace(value) == "" }
	add(!out.BaselineAvailable, "installation_not_initialized")
	add(in.Version != PilotContractVersion, "unsupported_contract_version")
	add(in.TenantID != id, "tenant_mapping_mismatch")
	add(in.InstallID == "" || in.InstallID != identity.InstallID, "installation_mapping_mismatch")
	add(in.ProviderMode != "fake" && in.ProviderMode != "simulated", "provider_mode_invalid")
	add(blank(in.MerchantType), "merchant_type_missing")
	add(blank(in.Shop.ShopID) || blank(in.Shop.Platform) || blank(in.Shop.OwnerRef), "shop_mapping_missing")
	add(blank(in.Shop.AuthorizationRef), "shop_authorization_missing")
	add(!pilotScopePresent(in.ReadScope), "read_scope_missing")
	add(!pilotScopePresent(in.WriteScope), "write_scope_missing")
	add(in.FundsLimit == nil || (in.FundsLimit != nil && (blank(in.FundsLimit.Currency) || in.FundsLimit.AmountMinor == nil || *in.FundsLimit.AmountMinor < 0)), "funds_limit_missing")
	add(blank(in.TestAuthorizer), "test_authorizer_missing")
	add(in.ObservationMinutes <= 0, "observation_window_missing")
	add(blank(in.RollbackOwner), "rollback_owner_missing")
	add(blank(in.PlatformApprovalWait), "platform_approval_wait_missing")
	add(in.Action.Name != "single_order_address_change" || blank(in.Action.OrderID), "verifiable_action_missing")
	add(!pilotScopePresent(in.Action.Postconditions) || blank(in.Action.ResponsibleOwner), "postconditions_or_owner_missing")
	add(in.EngineeringMinutes < 0, "engineering_effort_invalid")
	for _, required := range []string{"FA", "AR", "CH"} {
		closed, count := false, 0
		for _, dependency := range in.Dependencies {
			if dependency.ID == required {
				count++
				closed = !blank(dependency.CloseCommit) && !blank(dependency.TestEvidence)
			}
		}
		add(!closed || count != 1, "dependency_"+required+"_closure_missing")
	}
	// 复用注册声明只供核对，不将消息收发声明解释成订单写权限。
	for _, entry := range capregistry.All() {
		if entry.Platform == in.Shop.Platform {
			out.Capabilities = append(out.Capabilities, dto.PilotCapabilityDeclaration{Platform: entry.Platform, Capability: string(entry.Cap), Via: entry.Via})
		}
	}
	if len(out.Blockers) == 0 {
		out.Readiness = "ready_for_simulated_validation"
	}
	return out
}

func pilotScopePresent(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

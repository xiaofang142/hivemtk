package service

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
)

var ErrCapabilityCellNotRegistered = errors.New("capability cell not registered or shop missing")

const capabilitySummaryLifetime = 5 * time.Minute

func capabilityCellOption(cell capregistry.CellDefinition) dto.CapabilityCellOption {
	return dto.CapabilityCellOption{Product: cell.Product, Region: cell.Region, Action: cell.Action, Carrier: cell.Carrier}
}

func (s *CapabilityRuntimeService) ListCellOptions() []dto.CapabilityCellOption {
	cells := capregistry.CellDefinitions()
	options := make([]dto.CapabilityCellOption, 0, len(cells))
	for _, cell := range cells {
		options = append(options, capabilityCellOption(cell))
	}
	return options
}

func (s *CapabilityRuntimeService) ResolveCellSummary(ctx context.Context, req dto.CapabilityCellResolveRequest) (dto.CapabilityCellSummary, error) {
	now := time.Now().UTC()
	decision, err := s.resolveCellAt(ctx, req.ShopID, req.Product, req.Region, req.Action, req.Carrier, now)
	if err != nil {
		return dto.CapabilityCellSummary{}, err
	}
	cell, found := capregistry.FindCellDefinition(decision.Product, decision.Region, decision.Action, decision.Carrier)
	if !found || decision.ShopID == "" {
		return dto.CapabilityCellSummary{}, ErrCapabilityCellNotRegistered
	}
	owner, next := cellResponsibility(decision)
	validUntil := now.Add(capabilitySummaryLifetime)
	if expires := decision.ExpiresAt; expires != nil && expires.After(now) && expires.Before(validUntil) {
		validUntil = expires.UTC()
	}
	return dto.CapabilityCellSummary{
		CapabilityCellOption: capabilityCellOption(cell), TenantID: decision.TenantID, ShopID: decision.ShopID,
		ExecutionMode: decision.ExecutionMode, ConnectionState: string(decision.ConnectionState), Status: decision.Status,
		ProductionAllowed: decision.ProductionAllowed, ReasonCode: decision.ReasonCode,
		RequiredScopes: append([]string{}, cell.RequiredScopes...), ContractVersion: cell.ContractVersion,
		DocumentationVersion: cell.DocumentationVersion, ObservedScopes: decision.Scopes,
		ObservedVersion: decision.Version, ObservedDocumentationVersion: decision.DocumentationVersion,
		StateLimits: decision.StateLimits, Owner: owner, NextStep: next, ValidUntil: validUntil,
	}, nil
}

func cellResponsibility(decision capregistry.CellResolution) (owner, next string) {
	switch decision.ExecutionMode {
	case capregistry.ModeOfficialUI:
		return "merchant_operator", "use_official_workbench"
	case capregistry.ModePlatformManaged:
		return "platform", "observe_platform_result"
	}
	if decision.ProductionAllowed {
		return "system", "run_preflight"
	}
	if decision.ReasonCode == "capability_storage_unavailable" {
		return "tenant_admin", "verify_live_evidence"
	}
	switch decision.ConnectionState {
	case capregistry.ConnectionUnconfigured:
		return "tenant_admin", "configure_connection"
	case capregistry.ConnectionConnecting:
		return "tenant_admin", "complete_connection"
	case capregistry.ConnectionReauth:
		return "tenant_admin", "reconnect"
	case capregistry.ConnectionDisabled:
		return "tenant_admin", "review_disabled_connection"
	}
	switch decision.ReasonCode {
	case "required_scope_missing":
		return "tenant_admin", "review_permissions"
	case "state_preflight_required":
		return "merchant_operator", "review_action_state"
	default:
		return "tenant_admin", "verify_live_evidence"
	}
}

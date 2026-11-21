package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/secrets"
)

type MerchantRefreshPolicy string

const (
	MerchantRefreshRotate          MerchantRefreshPolicy = "rotate"
	MerchantRefreshRetainIfOmitted MerchantRefreshPolicy = "retain_if_omitted"
)

type MerchantOAuthRefresh struct {
	ShopID          string `json:"-"`
	ContractVersion string `json:"-"`
	RefreshToken    string `json:"-"`
}

// MerchantConnectionRefreshProvider must verify the actual shop and effective scopes
// server-side. It must honor ctx and make at most one refresh request per invocation.
// Contract must be a local configuration lookup. Its version covers all security settings.
type MerchantConnectionRefreshProvider interface {
	MerchantConnectionProvider
	Refresh(ctx context.Context, input MerchantOAuthRefresh) (MerchantOAuthGrant, error)
}

// Preserve the persisted field names from the original credential envelope.
type merchantTokens struct{ AccessToken, RefreshToken string }

func merchantTokenValid(token string) bool {
	return strings.TrimSpace(token) != "" && !strings.ContainsFunc(token, unicode.IsControl)
}

func sealMerchantTokens(tokens merchantTokens) (string, error) {
	plain, err := json.Marshal(tokens)
	if err != nil {
		return "", ErrMerchantConnectionStorage
	}
	return secrets.EncryptString(string(plain))
}

func openMerchantTokens(ciphertext string) (merchantTokens, error) {
	var tokens merchantTokens
	plain, err := openMerchantSecret(ciphertext)
	if err != nil {
		return tokens, err
	}
	if json.Unmarshal([]byte(plain), &tokens) != nil || !merchantTokenValid(tokens.AccessToken) ||
		(tokens.RefreshToken != "" && !merchantTokenValid(tokens.RefreshToken)) {
		return merchantTokens{}, ErrMerchantConnectionStorage
	}
	// Only this service writes the envelope. A round trip rejects unknown and duplicate
	// keys, null fields, or alternate shapes that the standard decoder accepts.
	canonical, err := json.Marshal(tokens)
	if err != nil || string(canonical) != plain {
		return merchantTokens{}, ErrMerchantConnectionStorage
	}
	return tokens, nil
}

func merchantRefreshAvailable(contract MerchantOAuthContract, row model.MerchantConnection) bool {
	if contract.RefreshPolicy == "" {
		return false
	}
	tokens, err := openMerchantTokens(row.CredentialCiphertext)
	return err == nil && tokens.RefreshToken != ""
}

func (s *merchantConnectionService) Refresh(ctx context.Context, actor uint, input dto.MerchantConnectionRefreshRequest) (dto.MerchantConnectionResponse, error) {
	var out dto.MerchantConnectionResponse
	ctx, err := merchantConnectionScope(ctx, input.MerchantConnectionTarget)
	if err != nil {
		return out, err
	}
	if actor == 0 {
		return out, ErrMerchantConnectionTenant
	}
	if input.CredentialVersion <= 0 {
		return out, ErrMerchantConnectionInput
	}
	provider, contract, err := s.contract(ctx, input.Provider)
	if err != nil {
		return out, err
	}
	refresher, ok := provider.(MerchantConnectionRefreshProvider)
	if !ok || contract.RefreshPolicy == "" {
		return out, ErrMerchantConnectionState
	}
	row, err := s.repo.FindStatus(ctx, input.Provider, input.ShopID)
	if err != nil {
		return out, ErrMerchantConnectionStorage
	}
	key := merchantContractKey(contract)
	if row.CredentialVersion != input.CredentialVersion || row.CredentialContract != key || row.ExpiresAt == nil ||
		!slices.Equal(row.Scopes, contract.Scopes) || row.Revoked || row.AttemptExpiresAt != nil || row.AttemptFailed || row.RefreshUntil != nil || row.RefreshFailed {
		return out, ErrMerchantConnectionState
	}
	tokens, err := openMerchantTokens(row.CredentialCiphertext)
	if err != nil {
		return out, err
	}
	if tokens.RefreshToken == "" {
		return out, ErrMerchantConnectionState
	}
	bounded, cancel := context.WithTimeout(ctx, merchantExchangeTimeout)
	defer cancel()
	if err := s.repo.ClaimRefresh(bounded, row, merchantExchangeTimeout); err != nil {
		return out, merchantConnectionWriteError(err)
	}
	grant, err := exchangeMerchantRefresh(bounded, refresher, contract, row, tokens.RefreshToken)
	if err != nil {
		// A canceled client must not prevent recording the uncertain outcome. The
		// persisted deadline still recovers status if this write fails or the process exits.
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), merchantExchangeTimeout)
		defer stop()
		if finishErr := s.repo.FinishRefresh(cleanup, row, nil); finishErr != nil {
			return out, merchantConnectionWriteError(finishErr)
		}
		return out, err
	}
	grant.CredentialContract = key
	if err := s.repo.FinishRefresh(bounded, row, &grant); err != nil {
		return out, merchantConnectionWriteError(err)
	}
	grant.ID, grant.CredentialVersion = row.ID, row.Version+1
	return merchantConnectionReady(grant, true), nil
}

func exchangeMerchantRefresh(ctx context.Context, provider MerchantConnectionRefreshProvider, contract MerchantOAuthContract, row model.MerchantConnection, token string) (model.MerchantConnection, error) {
	if ctx.Err() != nil {
		return model.MerchantConnection{}, ErrMerchantConnectionExchange
	}
	grant, err := provider.Refresh(ctx, MerchantOAuthRefresh{ShopID: row.ShopID, ContractVersion: contract.Version, RefreshToken: token})
	if err != nil || ctx.Err() != nil {
		return model.MerchantConnection{}, ErrMerchantConnectionExchange
	}
	if grant.RefreshToken == token || (grant.RefreshToken == "" && contract.RefreshPolicy == MerchantRefreshRotate) {
		return model.MerchantConnection{}, ErrMerchantConnectionExchange
	}
	if grant.RefreshToken == "" && contract.RefreshPolicy == MerchantRefreshRetainIfOmitted {
		grant.RefreshToken = token
	}
	return sealMerchantGrant(grant, contract, row)
}

func (s *merchantConnectionService) Revoke(ctx context.Context, actor uint, target dto.MerchantConnectionTarget) (dto.MerchantConnectionResponse, error) {
	ctx, err := merchantConnectionScope(ctx, target)
	if err != nil {
		return dto.MerchantConnectionResponse{}, err
	}
	if actor == 0 {
		return dto.MerchantConnectionResponse{}, ErrMerchantConnectionTenant
	}
	if err := s.repo.Revoke(ctx, target.Provider, target.ShopID); err != nil {
		return dto.MerchantConnectionResponse{}, ErrMerchantConnectionStorage
	}
	return merchantConnectionRevoked(target.Provider), nil
}

func merchantConnectionRevoked(provider string) dto.MerchantConnectionResponse {
	return dto.MerchantConnectionResponse{Provider: provider, ConnectionState: "revoked", ReasonCode: "provider_revocation_unconfirmed"}
}

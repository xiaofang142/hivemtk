package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/secrets"
)

var (
	ErrMerchantConnectionTenant    = errors.New("merchant connection requires an authenticated tenant")
	ErrMerchantConnectionInput     = errors.New("invalid merchant connection input")
	ErrMerchantProviderUnavailable = errors.New("merchant provider pending live validation")
	ErrMerchantConnectionStorage   = errors.New("merchant connection storage unavailable")
	ErrMerchantConnectionState     = errors.New("merchant connection state invalid or expired")
	ErrMerchantConnectionExchange  = errors.New("merchant authorization exchange failed")
)

const (
	merchantAttemptTTL      = 10 * time.Minute
	merchantExchangeTimeout = 30 * time.Second
	merchantCodeMaxBytes    = 4096
)

type MerchantPKCE string

const (
	MerchantPKCENone MerchantPKCE = "none"
	MerchantPKCES256 MerchantPKCE = "S256"
)

// MerchantOAuthContract comes from trusted connector configuration in the caller's tenant context.
// Version must change when a security-relevant adapter setting changes, including client secrets.
// Scopes are the exact grant this adapter may request and store.
type MerchantOAuthContract struct {
	Provider, Version                            string
	AuthorizationEndpoint, RedirectURI, ClientID string
	PKCE                                         MerchantPKCE
	Scopes                                       []string
	RefreshPolicy                                MerchantRefreshPolicy
}

type MerchantOAuthExchange struct {
	Code            string `json:"-"`
	Verifier        string `json:"-"`
	RedirectURI     string `json:"-"`
	ContractVersion string `json:"-"`
}

// MerchantOAuthGrant must contain the shop and scopes verified through server-side provider calls.
// OAuth request data or unverified callback parameters do not prove either field.
type MerchantOAuthGrant struct {
	Provider, ShopID string    `json:"-"`
	Scopes           []string  `json:"-"`
	AccessToken      string    `json:"-"`
	RefreshToken     string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
}

type MerchantConnectionProvider interface {
	Contract(ctx context.Context) (MerchantOAuthContract, error)
	Exchange(ctx context.Context, input MerchantOAuthExchange) (MerchantOAuthGrant, error)
}

type MerchantConnectionService interface {
	Status(ctx context.Context, target dto.MerchantConnectionTarget) (dto.MerchantConnectionResponse, error)
	Start(ctx context.Context, actor uint, target dto.MerchantConnectionTarget) (dto.MerchantConnectionStartResponse, error)
	Complete(ctx context.Context, actor uint, input dto.MerchantConnectionCompleteRequest) (dto.MerchantConnectionResponse, error)
	Refresh(ctx context.Context, actor uint, input dto.MerchantConnectionRefreshRequest) (dto.MerchantConnectionResponse, error)
	Revoke(ctx context.Context, actor uint, target dto.MerchantConnectionTarget) (dto.MerchantConnectionResponse, error)
}

type merchantConnectionService struct {
	repo      repository.MerchantConnectionRepository
	providers map[string]MerchantConnectionProvider
}

func NewMerchantConnectionService(repo repository.MerchantConnectionRepository, providers map[string]MerchantConnectionProvider) MerchantConnectionService {
	registered := make(map[string]MerchantConnectionProvider, len(providers))
	for key, provider := range providers {
		registered[key] = provider
	}
	return &merchantConnectionService{repo: repo, providers: registered}
}

func merchantConnectionScope(ctx context.Context, target dto.MerchantConnectionTarget) (context.Context, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return nil, ErrMerchantConnectionTenant
	}
	if !merchantOAuthIdentifier(target.Provider, 64) || !merchantOAuthIdentifier(target.ShopID, 160) {
		return nil, ErrMerchantConnectionInput
	}
	return tenant.OnlyTenant(ctx, id), nil
}

func merchantOAuthIdentifier(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._:-", r)
	})
}

func merchantHTTPSURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func (s *merchantConnectionService) contract(ctx context.Context, name string) (MerchantConnectionProvider, MerchantOAuthContract, error) {
	provider := s.providers[name]
	if provider == nil {
		return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
	}
	contract, err := provider.Contract(ctx)
	if err != nil || contract.Provider != name || !merchantOAuthIdentifier(contract.Version, 64) || contract.ClientID == "" ||
		(contract.PKCE != MerchantPKCENone && contract.PKCE != MerchantPKCES256) {
		return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
	}
	if endpoint, ok := merchantHTTPSURL(contract.AuthorizationEndpoint); !ok || endpoint.RawQuery != "" {
		return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
	}
	if _, ok := merchantHTTPSURL(contract.RedirectURI); !ok {
		return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
	}
	switch contract.RefreshPolicy {
	case "":
	case MerchantRefreshRotate, MerchantRefreshRetainIfOmitted:
		if _, ok := provider.(MerchantConnectionRefreshProvider); !ok {
			return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
		}
	default:
		return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
	}
	contract.Scopes = slices.Clone(contract.Scopes)
	slices.Sort(contract.Scopes)
	for _, scope := range contract.Scopes {
		if !merchantOAuthIdentifier(scope, 160) {
			return nil, MerchantOAuthContract{}, ErrMerchantProviderUnavailable
		}
	}
	contract.Scopes = slices.Compact(contract.Scopes)
	return provider, contract, nil
}

func merchantDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func merchantContractKey(c MerchantOAuthContract) string {
	parts := []string{c.Provider, c.Version, c.AuthorizationEndpoint, c.RedirectURI, c.ClientID, string(c.PKCE), strings.Join(c.Scopes, " ")}
	if c.RefreshPolicy != "" {
		parts = append(parts, string(c.RefreshPolicy))
	}
	return merchantDigest(strings.Join(parts, "\x00"))
}

func merchantRandom() string {
	var nonce [32]byte
	_, _ = rand.Read(nonce[:]) // Go guarantees a complete fill or process termination.
	return base64.RawURLEncoding.EncodeToString(nonce[:])
}

func (s *merchantConnectionService) Start(ctx context.Context, actor uint, target dto.MerchantConnectionTarget) (dto.MerchantConnectionStartResponse, error) {
	var out dto.MerchantConnectionStartResponse
	ctx, err := merchantConnectionScope(ctx, target)
	if err != nil {
		return out, err
	}
	if actor == 0 {
		return out, ErrMerchantConnectionTenant
	}
	_, contract, err := s.contract(ctx, target.Provider)
	if err != nil {
		return out, err
	}
	if !secrets.Ready() {
		return out, ErrMerchantConnectionStorage
	}
	state := merchantRandom()
	expires := time.Now().UTC().Add(merchantAttemptTTL)
	attempt := model.MerchantConnection{Provider: target.Provider, ShopID: target.ShopID, AttemptActor: actor,
		AttemptExpiresAt: &expires, AttemptContract: merchantContractKey(contract), StateDigest: merchantDigest(state)}
	query := url.Values{"state": {state}, "response_type": {"code"}, "client_id": {contract.ClientID}, "redirect_uri": {contract.RedirectURI}}
	if len(contract.Scopes) != 0 {
		query.Set("scope", strings.Join(contract.Scopes, " "))
	}
	if contract.PKCE == MerchantPKCES256 {
		verifier := merchantRandom()
		attempt.VerifierCiphertext, err = secrets.EncryptString(verifier)
		if err != nil {
			return out, ErrMerchantConnectionStorage
		}
		challenge := sha256.Sum256([]byte(verifier))
		query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		query.Set("code_challenge_method", string(MerchantPKCES256))
	}
	if err := s.repo.Begin(ctx, attempt); err != nil {
		return out, ErrMerchantConnectionStorage
	}
	endpoint, _ := url.Parse(contract.AuthorizationEndpoint)
	endpoint.RawQuery = query.Encode()
	return dto.MerchantConnectionStartResponse{AuthorizationURL: endpoint.String(), ExpiresAt: expires}, nil
}

func openMerchantSecret(ciphertext string) (string, error) {
	if !secrets.IsCiphertextFormat(ciphertext) {
		return "", ErrMerchantConnectionStorage
	}
	plain, err := secrets.DecryptString(ciphertext)
	if err != nil || plain == "" {
		return "", ErrMerchantConnectionStorage
	}
	return plain, nil
}

func (s *merchantConnectionService) Complete(ctx context.Context, actor uint, input dto.MerchantConnectionCompleteRequest) (dto.MerchantConnectionResponse, error) {
	var out dto.MerchantConnectionResponse
	ctx, err := merchantConnectionScope(ctx, input.MerchantConnectionTarget)
	if err != nil {
		return out, err
	}
	if actor == 0 {
		return out, ErrMerchantConnectionTenant
	}
	if len(input.State) != base64.RawURLEncoding.EncodedLen(32) {
		return out, ErrMerchantConnectionState
	}
	if input.Code == "" || len(input.Code) > merchantCodeMaxBytes || strings.ContainsFunc(input.Code, unicode.IsControl) {
		return out, ErrMerchantConnectionInput
	}
	provider, contract, err := s.contract(ctx, input.Provider)
	if err != nil {
		return out, err
	}
	if !secrets.Ready() {
		return out, ErrMerchantConnectionStorage
	}
	key := merchantContractKey(contract)
	claim, err := s.repo.Claim(ctx, input.Provider, input.ShopID, merchantDigest(input.State), key, actor)
	if err != nil {
		return out, merchantConnectionWriteError(err)
	}
	grant, err := exchangeMerchantCredential(ctx, provider, contract, input.Code, claim)
	if err != nil {
		// Keep the one-time claim consumed. Attempt failure is best-effort on request cancellation;
		// the persisted expiry still makes the attempt unusable and visible as expired.
		_ = s.repo.Finish(ctx, claim, nil)
		return out, err
	}
	grant.CredentialContract = key
	if err := s.repo.Finish(ctx, claim, &grant); err != nil {
		return out, merchantConnectionWriteError(err)
	}
	grant.ID = claim.ID
	grant.CredentialVersion = claim.Version
	return merchantConnectionReady(grant, merchantRefreshAvailable(contract, grant)), nil
}

func exchangeMerchantCredential(ctx context.Context, provider MerchantConnectionProvider, contract MerchantOAuthContract, code string, claim model.MerchantConnection) (model.MerchantConnection, error) {
	var row model.MerchantConnection
	var verifier string
	if contract.PKCE == MerchantPKCES256 {
		var err error
		verifier, err = openMerchantSecret(claim.VerifierCiphertext)
		if err != nil {
			return row, err
		}
	}
	bounded, cancel := context.WithTimeout(ctx, merchantExchangeTimeout)
	defer cancel()
	grant, err := provider.Exchange(bounded, MerchantOAuthExchange{Code: code, Verifier: verifier, RedirectURI: contract.RedirectURI, ContractVersion: contract.Version})
	if err != nil || bounded.Err() != nil {
		return row, ErrMerchantConnectionExchange
	}
	return sealMerchantGrant(grant, contract, claim)
}

func sealMerchantGrant(grant MerchantOAuthGrant, contract MerchantOAuthContract, claim model.MerchantConnection) (model.MerchantConnection, error) {
	var row model.MerchantConnection
	if grant.Provider != claim.Provider || grant.ShopID != claim.ShopID || !merchantTokenValid(grant.AccessToken) ||
		(grant.RefreshToken != "" && !merchantTokenValid(grant.RefreshToken)) || !grant.ExpiresAt.After(time.Now()) {
		return row, ErrMerchantConnectionExchange
	}
	scopes := slices.Clone(grant.Scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	if !slices.Equal(contract.Scopes, scopes) {
		return row, ErrMerchantConnectionExchange
	}
	ciphertext, err := sealMerchantTokens(merchantTokens{AccessToken: grant.AccessToken, RefreshToken: grant.RefreshToken})
	if err != nil {
		return row, ErrMerchantConnectionStorage
	}
	expires := grant.ExpiresAt.UTC()
	return model.MerchantConnection{Provider: grant.Provider, ShopID: grant.ShopID, CredentialCiphertext: ciphertext, Scopes: scopes, ExpiresAt: &expires}, nil
}

func merchantConnectionWriteError(err error) error {
	if errors.Is(err, repository.ErrMerchantConnectionStale) {
		return ErrMerchantConnectionState
	}
	return ErrMerchantConnectionStorage
}

func (s *merchantConnectionService) Status(ctx context.Context, target dto.MerchantConnectionTarget) (dto.MerchantConnectionResponse, error) {
	var out dto.MerchantConnectionResponse
	ctx, err := merchantConnectionScope(ctx, target)
	if err != nil {
		return out, err
	}
	out.Provider, out.ConnectionState = target.Provider, "unconfigured"
	_, contract, contractErr := s.contract(ctx, target.Provider)
	row, err := s.repo.FindStatus(ctx, target.Provider, target.ShopID)
	if row.Revoked && row.AttemptExpiresAt == nil && !row.AttemptFailed {
		return merchantConnectionRevoked(target.Provider), nil
	}
	if contractErr != nil {
		out.ReasonCode = "pending_live_validation"
		return out, nil
	}
	if err != nil {
		return dto.MerchantConnectionResponse{}, ErrMerchantConnectionStorage
	}
	key, now := merchantContractKey(contract), time.Now()
	switch {
	case row.AttemptExpiresAt != nil && row.AttemptContract == key && row.AttemptExpiresAt.After(now):
		out.ConnectionState = "connecting"
	case row.AttemptExpiresAt != nil:
		out.ConnectionState, out.ReasonCode = "reauth", "authorization_attempt_expired_or_changed"
	case row.AttemptFailed:
		out.ConnectionState, out.ReasonCode = "reauth", "authorization_exchange_failed"
	case row.RefreshUntil != nil && row.CredentialContract == key && row.RefreshUntil.After(now):
		out.ConnectionState, out.ReasonCode = "refreshing", "refresh_in_progress"
	case row.RefreshUntil != nil:
		out.ConnectionState, out.ReasonCode = "reauth", "refresh_outcome_unknown"
	case row.RefreshFailed:
		out.ConnectionState, out.ReasonCode = "reauth", "refresh_exchange_failed"
	case row.CredentialVersion == 0:
		return out, nil
	case row.CredentialContract != key || !slices.Equal(contract.Scopes, row.Scopes) || row.ExpiresAt == nil:
		out.ConnectionState, out.ReasonCode = "reauth", "credential_expired_or_changed"
	default:
		tokens, err := openMerchantTokens(row.CredentialCiphertext)
		if err != nil {
			out.ConnectionState, out.ReasonCode = "reauth", "credential_storage_unavailable"
			return out, nil
		}
		refreshAvailable := contract.RefreshPolicy != "" && tokens.RefreshToken != ""
		if row.ExpiresAt.After(now) {
			return merchantConnectionReady(row, refreshAvailable), nil
		}
		out.ConnectionState, out.ReasonCode = "reauth", "credential_expired_or_changed"
		if refreshAvailable {
			out = merchantConnectionReady(row, true)
			out.ConnectionState, out.ReasonCode = "reauth", "credential_expired_refresh_available"
		}
	}
	return out, nil
}

func merchantConnectionReady(row model.MerchantConnection, refreshAvailable bool) dto.MerchantConnectionResponse {
	return dto.MerchantConnectionResponse{Provider: row.Provider, ShopID: row.ShopID, ConnectionState: "ready",
		CredentialRef: row.ID, CredentialVersion: row.CredentialVersion, Scopes: row.Scopes, ExpiresAt: row.ExpiresAt,
		ReasonCode: "action_preflight_required", RefreshAvailable: refreshAvailable}
}

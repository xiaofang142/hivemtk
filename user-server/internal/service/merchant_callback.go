package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"golang.org/x/net/http/httpguts"
)

const (
	MerchantCallbackBodyLimit   = 64 << 10
	merchantCallbackHeaderLimit = 8 << 10
	merchantCallbackTimeout     = 5 * time.Second
)

type MerchantCallbackInput struct {
	Body    []byte      `json:"-"`
	Headers http.Header `json:"-"`
}

// VerifiedMerchantEvent is the only projection retained from an authenticated callback.
// ResourceID is a provider-verified business identifier, not an arbitrary payload field.
type VerifiedMerchantEvent struct {
	Provider, SubscriptionKey, ShopID, EventKey string
	Kind, Scope, ResourceID                     string
	SignedAt, ValidUntil                        time.Time
}

type MerchantCallbackAck struct {
	Status      int
	Body        string
	ContentType string
}

type MerchantCallbackContract struct {
	Enabled        bool
	Version        string
	Headers        []string
	Success, Retry MerchantCallbackAck
}

// MerchantCallbackVerifier uses trusted local configuration. Verify must authenticate
// the exact body and the necessary header values before yielding identifiers, enforce
// provider-specific time/replay rules, and cryptographically bind all returned fields
// (or derive them through an independently authenticated provider check). Verify runs
// before tenant lookup and must honor its context. It never supplies an ACK or tenant.
type MerchantCallbackVerifier interface {
	Contract() MerchantCallbackContract
	Verify(ctx context.Context, in MerchantCallbackInput) (VerifiedMerchantEvent, error)
}

type MerchantCallbackResult struct {
	Ack        MerchantCallbackAck
	EventID    uint
	Duplicate  bool
	TrustedAck bool
}

type MerchantCallbackService interface {
	Ingest(ctx context.Context, provider string, body []byte, headers http.Header) MerchantCallbackResult
	// ReadCompensationPage / AcknowledgeCompensationPage 是 EC-02 面向未来 EC-03 consumer 的
	// 内部薄方法：只经 repository 契约委托，不暴露 HTTP 路由、凭证、原始报文或 provider 客户端。
	// 分页 fence、租户/系统 scope 与连接版本校验由 repository 统一 fail-closed 执行。
	ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (repository.MerchantCallbackCompensationPage, error)
	AcknowledgeCompensationPage(ctx context.Context, page repository.MerchantCallbackCompensationPage, appliedEventIDs []uint) error
}

type merchantCallbackService struct {
	repo        repository.MerchantCallbackRepository
	connections repository.MerchantConnectionRepository
	verifiers   map[string]MerchantCallbackVerifier
}

func NewMerchantCallbackService(repo repository.MerchantCallbackRepository, connections repository.MerchantConnectionRepository, verifiers map[string]MerchantCallbackVerifier) MerchantCallbackService {
	registered := make(map[string]MerchantCallbackVerifier, len(verifiers))
	for name, verifier := range verifiers {
		registered[name] = verifier
	}
	return &merchantCallbackService{repo: repo, connections: connections, verifiers: registered}
}

func (s *merchantCallbackService) Ingest(ctx context.Context, provider string, body []byte, headers http.Header) MerchantCallbackResult {
	if !merchantOAuthIdentifier(provider, 64) || len(body) == 0 || len(body) > MerchantCallbackBodyLimit {
		return merchantCallbackFailure(http.StatusBadRequest)
	}
	v := s.verifiers[provider]
	if v == nil {
		return merchantCallbackFailure(http.StatusServiceUnavailable)
	}
	contract := v.Contract()
	if !contract.Enabled || !merchantOAuthIdentifier(contract.Version, 64) || !merchantCallbackAckValid(contract.Success, false) || !merchantCallbackAckValid(contract.Retry, true) {
		return merchantCallbackFailure(http.StatusServiceUnavailable)
	}
	selected, ok := merchantCallbackHeaders(contract.Headers, headers)
	if !ok {
		return merchantCallbackFailure(http.StatusBadRequest)
	}
	ctx, cancel := context.WithTimeout(tenant.OnlyTenant(ctx, 0), merchantCallbackTimeout)
	defer cancel()
	bodyDigest := sha256.Sum256(body)
	verified, err := v.Verify(ctx, MerchantCallbackInput{Body: body, Headers: selected})
	if err != nil || ctx.Err() != nil || !merchantCallbackEventValid(provider, verified) {
		return merchantCallbackFailure(http.StatusForbidden)
	}
	bind, err := s.repo.LookupSubscription(ctx, provider, verified.SubscriptionKey)
	if err != nil {
		return merchantCallbackStoreResult(contract, err)
	}
	if bind.ID == 0 || bind.TenantID == 0 || bind.ConnectionID == 0 || bind.ConsentVersion <= 0 || !bind.Enabled ||
		bind.Provider != provider || bind.SubscriptionKey != verified.SubscriptionKey || bind.ShopID != verified.ShopID ||
		bind.RequiredScope != verified.Scope || bind.EventKind != verified.Kind || bind.VerifierVersion != contract.Version {
		return merchantCallbackFailure(http.StatusForbidden)
	}
	ctx = tenant.OnlyTenant(ctx, bind.TenantID)
	row, err := s.connections.FindStatus(ctx, provider, verified.ShopID)
	if err != nil {
		return MerchantCallbackResult{Ack: contract.Retry, TrustedAck: true}
	}
	if row.ID != bind.ConnectionID || row.ConsentVersion != bind.ConsentVersion || row.CredentialVersion <= 0 ||
		row.Revoked || row.AttemptExpiresAt != nil || row.AttemptFailed || row.RefreshFailed || row.CredentialContract != bind.CredentialContract ||
		!slices.Contains(row.Scopes, bind.RequiredScope) {
		return merchantCallbackFailure(http.StatusForbidden)
	}
	if row.RefreshUntil != nil {
		if row.RefreshUntil.After(time.Now()) {
			return MerchantCallbackResult{Ack: contract.Retry, TrustedAck: true}
		}
		return merchantCallbackFailure(http.StatusForbidden)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.After(time.Now()) {
		return merchantCallbackFailure(http.StatusForbidden)
	}
	if _, err := openMerchantTokens(row.CredentialCiphertext); err != nil {
		return merchantCallbackFailure(http.StatusForbidden)
	}
	event := model.MerchantCallbackEvent{TenantID: bind.TenantID, Provider: provider, SubscriptionID: bind.ID,
		EventKey: verified.EventKey, ShopID: bind.ShopID, ConsentVersion: bind.ConsentVersion,
		ConnectionVersion: row.Version, CredentialVersion: row.CredentialVersion, Kind: verified.Kind, Scope: verified.Scope,
		ResourceID: verified.ResourceID, BodyDigest: hex.EncodeToString(bodyDigest[:]), VerifierVersion: contract.Version,
		SignedAt: verified.SignedAt.UTC(), ValidUntil: verified.ValidUntil.UTC()}
	id, duplicate, err := s.repo.Record(ctx, bind, row, event)
	if err != nil {
		return merchantCallbackStoreResult(contract, err)
	}
	if id == 0 {
		return MerchantCallbackResult{Ack: contract.Retry, TrustedAck: true}
	}
	return MerchantCallbackResult{Ack: contract.Success, EventID: id, Duplicate: duplicate, TrustedAck: true}
}

// ReadCompensationPage 只做边界守卫与委托：租户上下文必须存在且非系统 scope，
// 其余（订阅/连接/同意/凭证/fence/分页钳制）由 repository 统一 fail-closed 校验。
func (s *merchantCallbackService) ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (repository.MerchantCallbackCompensationPage, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) || subscriptionID == 0 {
		return repository.MerchantCallbackCompensationPage{}, repository.ErrMerchantCallbackBinding
	}
	if s.repo == nil {
		return repository.MerchantCallbackCompensationPage{}, repository.ErrMerchantCallbackStorage
	}
	return s.repo.ReadCompensationPage(ctx, subscriptionID, limit)
}

// AcknowledgeCompensationPage 同样只做边界守卫与委托，精确页面 fence 与集合校验在 repository 内完成。
func (s *merchantCallbackService) AcknowledgeCompensationPage(ctx context.Context, page repository.MerchantCallbackCompensationPage, appliedEventIDs []uint) error {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) {
		return repository.ErrMerchantCallbackBinding
	}
	if s.repo == nil {
		return repository.ErrMerchantCallbackStorage
	}
	return s.repo.AcknowledgeCompensationPage(ctx, page, appliedEventIDs)
}

func merchantCallbackEventValid(provider string, e VerifiedMerchantEvent) bool {
	return e.Provider == provider && merchantOAuthIdentifier(e.SubscriptionKey, 160) && merchantOAuthIdentifier(e.ShopID, 160) &&
		merchantOAuthIdentifier(e.EventKey, 160) && merchantOAuthIdentifier(e.Kind, 64) && merchantOAuthIdentifier(e.Scope, 160) &&
		merchantOAuthIdentifier(e.ResourceID, 160) && !e.SignedAt.IsZero() && e.ValidUntil.After(e.SignedAt) && e.ValidUntil.After(time.Now())
}

func merchantCallbackHeaders(names []string, source http.Header) (http.Header, bool) {
	selected := make(http.Header, len(names))
	budget := merchantCallbackHeaderLimit
	for _, name := range names {
		if !httpguts.ValidHeaderFieldName(name) {
			return nil, false
		}
		name = http.CanonicalHeaderKey(name)
		values := source.Values(name)
		budget -= len(name)
		for _, value := range values {
			budget -= len(value) + 1
			if budget < 0 || !httpguts.ValidHeaderFieldValue(value) {
				return nil, false
			}
		}
		if budget < 0 {
			return nil, false
		}
		selected[name] = slices.Clone(values)
	}
	return selected, true
}

func merchantCallbackAckValid(ack MerchantCallbackAck, retry bool) bool {
	if retry {
		if ack.Status != http.StatusTooManyRequests && (ack.Status < 500 || ack.Status > 599) {
			return false
		}
	} else if ack.Status < 200 || ack.Status > 299 {
		return false
	}
	return len(ack.Body) <= 1024 && ((ack.Status != http.StatusNoContent && ack.Status != http.StatusResetContent) || ack.Body == "") &&
		(ack.ContentType == "" || ack.ContentType == "text/plain" || ack.ContentType == "application/json")
}

func merchantCallbackFailure(status int) MerchantCallbackResult {
	return MerchantCallbackResult{Ack: MerchantCallbackAck{Status: status}}
}

func merchantCallbackStoreResult(contract MerchantCallbackContract, err error) MerchantCallbackResult {
	switch {
	case errors.Is(err, repository.ErrMerchantCallbackBinding):
		return merchantCallbackFailure(http.StatusForbidden)
	case errors.Is(err, repository.ErrMerchantCallbackConflict):
		return merchantCallbackFailure(http.StatusConflict)
	default:
		return MerchantCallbackResult{Ack: contract.Retry, TrustedAck: true}
	}
}

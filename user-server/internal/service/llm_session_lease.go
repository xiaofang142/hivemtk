package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	ErrLLMSessionLeaseUnavailable = errors.New("LLM 会话租约后端不可用")
	ErrLLMSessionLeaseBusy        = errors.New("LLM 会话已被其他执行占用")
	ErrLLMSessionLeaseLost        = errors.New("LLM 会话租约或 fencing 已失效")
	ErrLLMSessionLeaseNotFound    = errors.New("LLM 会话状态不存在")
	ErrLLMSessionLeaseScope       = errors.New("LLM 会话租约范围不匹配")
	ErrLLMSessionLeaseConflict    = errors.New("LLM 会话租约 CAS 冲突")
	ErrLLMSessionLeaseInvalid     = errors.New("LLM 会话租约参数无效")
)

type llmSessionLockBackend interface {
	SetNX(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	RefreshLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key, token string) error
}

type llmSessionStateStore interface {
	GetLatest(ctx context.Context, query repository.LLMSessionStateQuery) (*model.LLMSessionState, error)
	Update(ctx context.Context, item *model.LLMSessionState) error
}

type LLMSessionLease struct {
	Token          string
	Key            string
	StateID        uint
	FencingVersion int64
	ExpiresAt      time.Time
	query          repository.LLMSessionStateQuery
}

type LLMSessionLeaseService struct {
	locks  llmSessionLockBackend
	states llmSessionStateStore
	now    func() time.Time
}

func NewLLMSessionLeaseService(locks llmSessionLockBackend, states llmSessionStateStore) *LLMSessionLeaseService {
	return &LLMSessionLeaseService{
		locks: locks, states: states, now: time.Now,
	}
}

func (s *LLMSessionLeaseService) Acquire(ctx context.Context, query repository.LLMSessionStateQuery, ttl time.Duration) (*LLMSessionLease, error) {
	if s == nil || s.locks == nil || s.states == nil || ttl <= 0 {
		return nil, ErrLLMSessionLeaseInvalid
	}
	if err := validateLLMSessionLeaseQuery(query); err != nil {
		return nil, err
	}
	tid, err := llmSessionLeaseTenantID(ctx)
	if err != nil {
		return nil, err
	}
	key := llmSessionLeaseKey(ctx, tid, query)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMSessionLeaseUnavailable, err)
	}
	token := hex.EncodeToString(tokenBytes)
	acquired, err := s.locks.SetNX(ctx, key, token, ttl)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMSessionLeaseUnavailable, err)
	}
	if !acquired {
		return nil, ErrLLMSessionLeaseBusy
	}
	releaseOnFailure := func() {
		_ = s.locks.ReleaseLock(ctx, key, token)
	}

	state, err := s.states.GetLatest(ctx, query)
	if err != nil {
		releaseOnFailure()
		return nil, err
	}
	if state == nil || state.ID == 0 {
		releaseOnFailure()
		return nil, ErrLLMSessionLeaseNotFound
	}
	if !llmSessionStateMatchesQuery(state, query) {
		releaseOnFailure()
		return nil, ErrLLMSessionLeaseScope
	}
	if state.RetentionMode != model.LLMSessionRetentionNative {
		releaseOnFailure()
		return nil, ErrLLMSessionLeaseInvalid
	}
	now := s.now()
	if !state.ExpiresAt.IsZero() && !state.ExpiresAt.After(now) {
		releaseOnFailure()
		return nil, ErrLLMSessionLeaseNotFound
	}
	if err := s.states.Update(ctx, state); err != nil {
		releaseOnFailure()
		if errors.Is(err, repository.ErrLLMSessionStateConflict) {
			return nil, fmt.Errorf("%w: %v", ErrLLMSessionLeaseConflict, err)
		}
		return nil, err
	}
	return &LLMSessionLease{
		Token: token, Key: key, StateID: state.ID,
		FencingVersion: state.Version, ExpiresAt: now.Add(ttl), query: query,
	}, nil
}

func (s *LLMSessionLeaseService) Renew(ctx context.Context, lease *LLMSessionLease, ttl time.Duration) error {
	if s == nil || s.locks == nil || lease == nil || lease.Token == "" || lease.Key == "" || ttl <= 0 {
		return ErrLLMSessionLeaseInvalid
	}
	ok, err := s.locks.RefreshLock(ctx, lease.Key, lease.Token, ttl)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLLMSessionLeaseUnavailable, err)
	}
	if !ok {
		return ErrLLMSessionLeaseLost
	}
	lease.ExpiresAt = s.now().Add(ttl)
	return nil
}

func (s *LLMSessionLeaseService) AssertOwned(ctx context.Context, lease *LLMSessionLease) error {
	if s == nil || s.locks == nil || s.states == nil || lease == nil || lease.Token == "" || lease.Key == "" {
		return ErrLLMSessionLeaseInvalid
	}
	remaining := time.Until(lease.ExpiresAt)
	if remaining <= 0 {
		return ErrLLMSessionLeaseLost
	}
	ok, err := s.locks.RefreshLock(ctx, lease.Key, lease.Token, remaining)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLLMSessionLeaseUnavailable, err)
	}
	if !ok {
		return ErrLLMSessionLeaseLost
	}
	state, err := s.states.GetLatest(ctx, lease.query)
	if err != nil {
		return err
	}
	if state == nil || state.ID != lease.StateID || state.Version != lease.FencingVersion {
		return ErrLLMSessionLeaseLost
	}
	return nil
}

func (s *LLMSessionLeaseService) Commit(ctx context.Context, lease *LLMSessionLease, mutate func(*model.LLMSessionState) error) error {
	if s == nil || s.states == nil || lease == nil || mutate == nil {
		return ErrLLMSessionLeaseInvalid
	}
	if err := s.AssertOwned(ctx, lease); err != nil {
		return err
	}
	state, err := s.states.GetLatest(ctx, lease.query)
	if err != nil {
		return err
	}
	if state == nil || state.ID != lease.StateID || state.Version != lease.FencingVersion {
		return ErrLLMSessionLeaseLost
	}
	id, version := state.ID, state.Version
	if err := mutate(state); err != nil {
		return err
	}
	if state.ID != id || state.Version != version {
		return ErrLLMSessionLeaseInvalid
	}
	if err := s.states.Update(ctx, state); err != nil {
		if errors.Is(err, repository.ErrLLMSessionStateConflict) {
			return fmt.Errorf("%w: %v", ErrLLMSessionLeaseConflict, err)
		}
		return err
	}
	return nil
}

func (s *LLMSessionLeaseService) Release(ctx context.Context, lease *LLMSessionLease) error {
	if s == nil || s.locks == nil || lease == nil || lease.Token == "" || lease.Key == "" {
		return ErrLLMSessionLeaseInvalid
	}
	if err := s.locks.ReleaseLock(ctx, lease.Key, lease.Token); err != nil {
		return fmt.Errorf("%w: %v", ErrLLMSessionLeaseUnavailable, err)
	}
	return nil
}

func validateLLMSessionLeaseQuery(query repository.LLMSessionStateQuery) error {
	if strings.TrimSpace(query.Platform) == "" || query.ChannelAccountID == 0 ||
		strings.TrimSpace(query.LLMConversationID) == "" || strings.TrimSpace(query.ScopeHash) == "" ||
		len(query.BlindIndexes) == 0 || strings.TrimSpace(query.ExecutionScenarioID) == "" ||
		strings.TrimSpace(query.Provider) == "" || strings.TrimSpace(query.Model) == "" ||
		strings.TrimSpace(query.Protocol) == "" || strings.TrimSpace(query.Region) == "" {
		return ErrLLMSessionLeaseScope
	}
	return nil
}

func llmSessionStateMatchesQuery(state *model.LLMSessionState, query repository.LLMSessionStateQuery) bool {
	if state == nil || state.Platform != query.Platform || state.ChannelAccountID != query.ChannelAccountID ||
		state.LLMConversationID != query.LLMConversationID || state.ScopeHash != query.ScopeHash ||
		state.ExecutionScenarioID != query.ExecutionScenarioID || state.Provider != query.Provider ||
		state.Model != query.Model || state.Protocol != query.Protocol || state.Region != query.Region {
		return false
	}
	for _, blind := range query.BlindIndexes {
		if state.BusLLMSessionIDBlind == blind {
			return true
		}
	}
	return false
}

func llmSessionLeaseTenantID(ctx context.Context) (uint, error) {
	tid, ok := tenant.FromContext(ctx)
	if ok && !tenant.IsSystemScope(ctx) && tid > 0 {
		return tid, nil
	}
	if !tenant.IsMultiTenant() && !tenant.IsSystemScope(ctx) {
		return tenant.DefaultTenantID, nil
	}
	return 0, ErrLLMSessionLeaseScope
}

func llmSessionLeaseKey(ctx context.Context, tenantID uint, query repository.LLMSessionStateQuery) string {
	parts := []string{
		fmt.Sprint(tenantID), query.Platform, fmt.Sprint(query.ChannelAccountID),
		query.LLMConversationID, query.ScopeHash, query.ExecutionScenarioID,
		query.Provider, query.Model, query.Protocol, query.Region,
	}
	var builder strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&builder, "%d:%s|", len(part), part)
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return tenant.RedisKey(ctx, "llm-session-lease:v1:"+hex.EncodeToString(sum[:]))
}

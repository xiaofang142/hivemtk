package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type fakeLLMSessionLock struct {
	values     map[string]string
	expires    map[string]time.Time
	setErr     error
	refreshErr error
	releaseErr error
}

func (f *fakeLLMSessionLock) SetNX(_ context.Context, key, token string, ttl time.Duration) (bool, error) {
	if f.setErr != nil {
		return false, f.setErr
	}
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = token
	f.expires[key] = time.Now().Add(ttl)
	return true, nil
}

func (f *fakeLLMSessionLock) RefreshLock(_ context.Context, key, token string, ttl time.Duration) (bool, error) {
	if f.refreshErr != nil {
		return false, f.refreshErr
	}
	if f.values[key] != token {
		return false, nil
	}
	if !f.expires[key].After(time.Now()) {
		return false, nil
	}
	f.expires[key] = time.Now().Add(ttl)
	return true, nil
}

func (f *fakeLLMSessionLock) ReleaseLock(_ context.Context, key, token string) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	if f.values[key] == token {
		delete(f.values, key)
		delete(f.expires, key)
	}
	return nil
}

type fakeLLMSessionStateStore struct {
	item      *model.LLMSessionState
	updateErr error
	updates   int
}

func (f *fakeLLMSessionStateStore) GetLatest(_ context.Context, _ repository.LLMSessionStateQuery) (*model.LLMSessionState, error) {
	if f.item == nil {
		return nil, nil
	}
	copy := *f.item
	return &copy, nil
}

func (f *fakeLLMSessionStateStore) Update(_ context.Context, item *model.LLMSessionState) error {
	f.updates++
	if f.updateErr != nil {
		return f.updateErr
	}
	item.Version++
	copy := *item
	f.item = &copy
	return nil
}

func newLeaseFixture(t *testing.T) (*LLMSessionLeaseService, *fakeLLMSessionLock, *fakeLLMSessionStateStore, repository.LLMSessionStateQuery) {
	t.Helper()
	locks := &fakeLLMSessionLock{values: map[string]string{}, expires: map[string]time.Time{}}
	state := &fakeLLMSessionStateStore{item: &model.LLMSessionState{
		ID: 42, TenantID: 7, Version: 2, RetentionMode: model.LLMSessionRetentionNative,
		Platform: "telegram", ChannelAccountID: 3, LLMConversationID: "llmconv-1", ScopeHash: "scope-1", BusLLMSessionIDBlind: "blind-1",
		ExecutionScenarioID: "sales_generate", Provider: "openai", Model: "gpt-test", Protocol: "responses", Region: "global",
	}}
	query := repository.LLMSessionStateQuery{
		Platform: "telegram", ChannelAccountID: 3, LLMConversationID: "llmconv-1", ScopeHash: "scope-1",
		BlindIndexes: []string{"blind-1"}, ExecutionScenarioID: "sales_generate",
		Provider: "openai", Model: "gpt-test", Protocol: "responses", Region: "global",
	}
	return NewLLMSessionLeaseService(locks, state), locks, state, query
}

func TestLLMSessionLeaseAcquireClaimsFencingVersion(t *testing.T) {
	svc, locks, state, query := newLeaseFixture(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	lease, err := svc.Acquire(ctx, query, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Token == "" || lease.FencingVersion != 3 || lease.StateID != 42 {
		t.Fatalf("租约 fencing 错误：%+v", lease)
	}
	if state.item.Version != 3 || state.updates != 1 {
		t.Fatalf("获取租约必须 CAS claim：version=%d updates=%d", state.item.Version, state.updates)
	}
	if strings.Contains(lease.Key, "blind-1") || strings.Contains(lease.Key, "llmconv-1") {
		t.Fatalf("租约键不得包含原始/盲索引标识：%q", lease.Key)
	}
	if _, err := svc.Acquire(ctx, query, time.Minute); !errors.Is(err, ErrLLMSessionLeaseBusy) {
		t.Fatalf("重复获取必须 busy，实际 %v", err)
	}
	if len(locks.values) != 1 {
		t.Fatalf("busy 不得覆盖原锁：%#v", locks.values)
	}
}

func TestLLMSessionLeaseRenewAndAssertOwned(t *testing.T) {
	svc, _, _, query := newLeaseFixture(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	lease, err := svc.Acquire(ctx, query, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	before := lease.ExpiresAt
	if err := svc.Renew(ctx, lease, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if !lease.ExpiresAt.After(before) {
		t.Fatalf("续期未延长过期时间：before=%v after=%v", before, lease.ExpiresAt)
	}
	if err := svc.AssertOwned(ctx, lease); err != nil {
		t.Fatal(err)
	}
}

func TestLLMSessionLeaseRejectsStaleFencing(t *testing.T) {
	svc, _, state, query := newLeaseFixture(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	lease, err := svc.Acquire(ctx, query, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	state.item.Version++
	if err := svc.AssertOwned(ctx, lease); !errors.Is(err, ErrLLMSessionLeaseLost) {
		t.Fatalf("旧 fencing 必须失效，实际 %v", err)
	}
}

func TestLLMSessionLeaseCommitRequiresOwnershipAndCAS(t *testing.T) {
	svc, _, state, query := newLeaseFixture(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	lease, err := svc.Acquire(ctx, query, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Commit(ctx, lease, func(item *model.LLMSessionState) error {
		item.ProviderContinuationRefEnc = []byte("enc-ref")
		item.LastMessageCursor = "hub:9"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if state.item.Version != 4 || string(state.item.ProviderContinuationRefEnc) != "enc-ref" || state.item.LastMessageCursor != "hub:9" {
		t.Fatalf("提交未持久化：%+v", state.item)
	}
	if err := svc.AssertOwned(ctx, lease); !errors.Is(err, ErrLLMSessionLeaseLost) {
		t.Fatalf("提交后旧租约必须失效，实际 %v", err)
	}
}

func TestLLMSessionLeaseBackendFailureIsFailClosed(t *testing.T) {
	svc, locks, _, query := newLeaseFixture(t)
	locks.setErr = errors.New("redis unavailable")
	ctx := tenant.WithTenantID(context.Background(), 7)
	if _, err := svc.Acquire(ctx, query, time.Minute); !errors.Is(err, ErrLLMSessionLeaseUnavailable) {
		t.Fatalf("锁后端故障必须 fail-closed，实际 %v", err)
	}
}

func TestLLMSessionLeaseReleaseOnlyOwnsToken(t *testing.T) {
	svc, locks, _, query := newLeaseFixture(t)
	ctx := tenant.WithTenantID(context.Background(), 7)
	lease, err := svc.Acquire(ctx, query, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wrong := *lease
	wrong.Token = "wrong"
	if err := svc.Release(ctx, &wrong); err != nil {
		t.Fatal(err)
	}
	if locks.values[lease.Key] != lease.Token {
		t.Fatal("错误 token 不得释放他人锁")
	}
	if err := svc.Release(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, exists := locks.values[lease.Key]; exists {
		t.Fatal("正确 token 应释放锁")
	}
}

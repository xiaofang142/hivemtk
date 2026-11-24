package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

type replayTenantRepoStub struct {
	tenantID uint
	err      error
	calls    int
}

func (s *replayTenantRepoStub) ResolveTenantID(context.Context, string, string) (uint, error) {
	s.calls++
	return s.tenantID, s.err
}

func TestResolveDelayedReplayTenant_Arbitration(t *testing.T) {
	t.Run("existing-context-wins", func(t *testing.T) {
		repo := &replayTenantRepoStub{tenantID: 9}
		svc := &WebhookService{bridgeAccountTenantRepo: repo}
		ctx := tenant.WithTenantID(context.Background(), 7)
		got, err := svc.resolveDelayedReplayTenant(ctx, &model.DelayedOutboundReply{TenantID: 18})
		if err != nil {
			t.Fatalf("已有租户不应反查: %v", err)
		}
		if id, _ := tenant.FromContext(got); id != 7 || repo.calls != 0 {
			t.Fatalf("已有租户被改写: id=%d calls=%d", id, repo.calls)
		}
	})

	t.Run("no-row-falls-back-to-row", func(t *testing.T) {
		repo := &replayTenantRepoStub{}
		svc := &WebhookService{bridgeAccountTenantRepo: repo}
		got, err := svc.resolveDelayedReplayTenant(context.Background(), &model.DelayedOutboundReply{TenantID: 18})
		if err != nil {
			t.Fatalf("未命中应回退行值: %v", err)
		}
		if id, _ := tenant.FromContext(got); id != 18 || repo.calls != 1 {
			t.Fatalf("未命中回退错误: id=%d calls=%d", id, repo.calls)
		}
	})

	t.Run("query-error-does-not-fallback", func(t *testing.T) {
		queryErr := errors.New("replay tenant query failed")
		repo := &replayTenantRepoStub{err: queryErr}
		svc := &WebhookService{bridgeAccountTenantRepo: repo}
		got, err := svc.resolveDelayedReplayTenant(context.Background(), &model.DelayedOutboundReply{TenantID: 18})
		if !errors.Is(err, queryErr) {
			t.Fatalf("运行错误必须可见且不得回退行值: %v", err)
		}
		if _, bound := tenant.FromContext(got); bound {
			t.Fatal("运行错误时不得绑定 rec.TenantID")
		}
	})

	t.Run("ambiguous-does-not-fallback", func(t *testing.T) {
		repo := &replayTenantRepoStub{err: tenant.ErrAmbiguousTenantKey}
		svc := &WebhookService{bridgeAccountTenantRepo: repo}
		got, err := svc.resolveDelayedReplayTenant(context.Background(), &model.DelayedOutboundReply{TenantID: 18})
		if !errors.Is(err, tenant.ErrAmbiguousTenantKey) {
			t.Fatalf("歧义必须可见且不得回退行值: %v", err)
		}
		if _, bound := tenant.FromContext(got); bound {
			t.Fatal("歧义时不得绑定 rec.TenantID")
		}
	})

	t.Run("no-row-and-no-row-tenant", func(t *testing.T) {
		repo := &replayTenantRepoStub{}
		svc := &WebhookService{bridgeAccountTenantRepo: repo}
		got, err := svc.resolveDelayedReplayTenant(context.Background(), &model.DelayedOutboundReply{})
		if err == nil {
			t.Fatal("反查未命中且行值无租户必须拒绝")
		}
		if _, bound := tenant.FromContext(got); bound {
			t.Fatal("无归属时不得绑定默认租户")
		}
	})
}

func TestReplayDelayedOutbound_TenantLookupFailureAbandons(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "query-error", err: errors.New("replay tenant query failed")},
		{name: "ambiguous", err: tenant.ErrAmbiguousTenantKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
			svc := NewWebhookService()
			t.Cleanup(func() { svc.Stop(context.Background()) })
			svc.bridgeAccountTenantRepo = &replayTenantRepoStub{err: tc.err}
			now := time.Now()
			until := now.Add(time.Minute)
			rec := &model.DelayedOutboundReply{
				ID: uint(800_000_000 + time.Now().UnixNano()%19_000_000), TenantID: 1,
				Platform: "douyin", AccountID: "replay-tenant-failure", Content: "待投回复",
				Kind: model.DelayedKindQuietHours, Status: model.DelayedStatusSending,
				ClaimToken: fmt.Sprintf("replay-tenant-token-%d", time.Now().UnixNano()),
				ClaimedAt:  &now, LeaseUntil: &until,
			}
			if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(rec).Error; err != nil {
				t.Fatalf("写入重放夹具失败: %v", err)
			}
			svc.replayDelayedOutbound(context.Background(), rec)
			var got model.DelayedOutboundReply
			if err := db.First(&got, rec.ID).Error; err != nil {
				t.Fatalf("读回重放行失败: %v", err)
			}
			if got.Status != model.DelayedStatusFailed {
				t.Fatalf("租户反查失败必须立即 abandon: status=%s last_error=%s", got.Status, got.LastError)
			}
			if !strings.Contains(got.LastError, tc.err.Error()) {
				t.Fatalf("abandon 必须保留反查错误: %q", got.LastError)
			}
		})
	}
}

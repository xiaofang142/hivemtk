package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type failingReplayCandidateStore struct {
	repository.WebhookDeliveryRepository
	err error
}

func (f failingReplayCandidateStore) ListReplayCandidates(context.Context, uint, int, time.Time, int) ([]model.WebhookDelivery, error) {
	return nil, f.err
}

func TestReplayDeadWebhookDeliveries_CandidateDBErrorVisible(t *testing.T) {
	svc := NewWebhookSubService()
	svc.deliveries = failingReplayCandidateStore{err: errors.New("synthetic candidate query failure")}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err == nil || n != 0 {
		t.Fatalf("候选查询 DB 错误必须可见且不得报成功: n=%d err=%v", n, err)
	}
}

type failingReplayCompletionStore struct {
	repository.WebhookDeliveryRepository
	err error
}

func (f failingReplayCompletionStore) CompleteReplay(context.Context, repository.WebhookDeliveryClaim, string, int, error, time.Time) (bool, error) {
	return false, f.err
}

func TestReplayDeadWebhookDeliveries_CompletionDBErrorVisible(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, _ := seedWebhookDeadLetter(t, srv.URL, 3)
	svc.deliveries = failingReplayCompletionStore{WebhookDeliveryRepository: svc.deliveries, err: errors.New("synthetic completion failure")}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err == nil || n != 0 || hits.Load() != 1 {
		t.Fatalf("完成 DB 错误必须可见且不得报成功: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
}

func TestReplayDeadWebhookDeliveries_OnModeRequiresTenant(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, _ := seedWebhookDeadLetter(t, srv.URL, 3)
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err == nil || n != 0 || hits.Load() != 0 {
		t.Fatalf("on 模式无租户上下文必须 fail-closed: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
}

func TestReplayDeadWebhookDeliveries_DisabledSubscriptionVisibleFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)
	if err := dbstate.Get().Model(&model.WebhookSubscription{}).Where("id = ?", row.SubscriptionID).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable subscription: %v", err)
	}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 1 || hits.Load() != 0 {
		t.Fatalf("禁用订阅应作为可见失败完成且不外发: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
	var got model.WebhookDelivery
	if err := dbstate.Get().First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "dead_letter" || got.Attempts != 4 || got.LastError == "" {
		t.Fatalf("禁用订阅失败应可观测且 attempts 仅 +1: %+v", got)
	}
}

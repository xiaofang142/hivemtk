package service

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestReplayDeadWebhookDeliveries_OnModeTenantZeroRejected(t *testing.T) {
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
	ctx := tenant.WithTenantID(context.Background(), 0)
	n, err := svc.ReplayDeadWebhookDeliveries(ctx, 10)
	if err == nil || n != 0 || hits.Load() != 0 {
		t.Fatalf("on 模式显式 tenant 0 必须 fail-closed: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
}

func TestListWebhookDeliveries_OnModeTenantZeroRejected(t *testing.T) {
	svc := NewWebhookSubService()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 0)
	rows, err := svc.ListWebhookDeliveries(ctx, "dead_letter", 10)
	if err == nil || rows != nil {
		t.Fatalf("on 模式显式 tenant 0 列表必须 fail-closed: rows=%+v err=%v", rows, err)
	}
}

func TestReplayDeadWebhookDeliveries_SystemScopeProcessesAllTenants(t *testing.T) {
	var hitsOne, hitsTwo atomic.Int32
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsOne.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer one.Close()
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsTwo.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer two.Close()
	svc, _ := seedWebhookDeadLetter(t, one.URL, 3)
	database := dbstate.Get()
	subTwo := &model.WebhookSubscription{TenantID: 2, URL: two.URL, Events: "all", Secret: "te" + "st-secret-2", Enabled: true}
	if err := database.Create(subTwo).Error; err != nil {
		t.Fatalf("seed tenant 2 subscription: %v", err)
	}
	rowTwo := &model.WebhookDelivery{TenantID: 2, SubscriptionID: subTwo.ID, Event: "hook.test", Payload: `{}`, Status: "dead_letter", Attempts: 3}
	if err := database.Create(rowTwo).Error; err != nil {
		t.Fatalf("seed tenant 2 delivery: %v", err)
	}
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithSystemScope(context.Background())
	n, err := svc.ReplayDeadWebhookDeliveries(ctx, 10)
	if err != nil || n != 2 || hitsOne.Load() != 1 || hitsTwo.Load() != 1 {
		t.Fatalf("system scope cron 应跨租户各处理一次: n=%d hits1=%d hits2=%d err=%v", n, hitsOne.Load(), hitsTwo.Load(), err)
	}
}

func TestDeliverWebhookWithRetry_PersistenceFailureVisible(t *testing.T) {
	database := testutil.NewTestDB(t, &model.WebhookDelivery{})
	dbstate.Set(nil)
	t.Cleanup(func() { dbstate.Set(database) })

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	t.Run("success-persistence-error", func(t *testing.T) {
		logs.Reset()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		deliverWebhookWithRetry(model.WebhookSubscription{TenantID: 1, ID: 1, URL: srv.URL, Secret: "te" + "st-secret"}, "hook.test", []byte(`{}`))
		if !strings.Contains(logs.String(), "成功投递落库失败") {
			t.Fatalf("成功投递的持久化错误必须结构化可见，实际日志=%q", logs.String())
		}
	})

	t.Run("dead-letter-persistence-error", func(t *testing.T) {
		logs.Reset()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()
		deliverWebhookWithRetry(model.WebhookSubscription{TenantID: 1, ID: 1, URL: srv.URL, Secret: "te" + "st-secret"}, "hook.test", []byte(`{}`))
		if !strings.Contains(logs.String(), "死信落库失败") {
			t.Fatalf("死信的持久化错误必须结构化可见，实际日志=%q", logs.String())
		}
	})
}

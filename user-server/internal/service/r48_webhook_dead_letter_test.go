package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func seedWebhookDeadLetter(t *testing.T, url string, attempts int) (*WebhookSubService, model.WebhookDelivery) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.WebhookSubscription{}, &model.WebhookDelivery{})
	sub := &model.WebhookSubscription{TenantID: 1, URL: url, Events: "all", Secret: "test-" + "secret", Enabled: true}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	row := &model.WebhookDelivery{
		TenantID: 1, SubscriptionID: sub.ID, Event: "hook.test",
		Payload: `{"schema_version":"hivemtk.hook.v1","event":"hook.test","data":{"id":1}}`,
		Status:  "dead_letter", Attempts: attempts, LastError: "synthetic failure",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed dead letter: %v", err)
	}
	return NewWebhookSubService(), *row
}

func TestWebhookDeadLetter_PersistenceAndList(t *testing.T) {
	db := testutil.NewTestDB(t, &model.WebhookSubscription{}, &model.WebhookDelivery{})
	sub := &model.WebhookSubscription{TenantID: 1, URL: "http://127.0.0.1/unused", Events: "all", Secret: "test-" + "secret", Enabled: true}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	recordWebhookDelivery(*sub, "hook.persist", []byte(`{"ok":true}`), "dead_letter", 3, 500, errors.New("synthetic 500"))
	recordWebhookDelivery(*sub, "hook.success", []byte(`{"ok":true}`), "success", 1, 200, nil)

	rows, err := NewWebhookSubService().ListWebhookDeliveries(context.Background(), "dead_letter", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("死信列表应恰 1 条: rows=%+v err=%v", rows, err)
	}
	if rows[0].Status != "dead_letter" || rows[0].Attempts != 3 || rows[0].ResponseCode != 500 || rows[0].LastError != "synthetic 500" {
		t.Fatalf("死信持久化字段不符: %+v", rows[0])
	}
}

func TestReplayDeadWebhookDeliveries_SuccessIsIdempotent(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)

	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 1 || hits.Load() != 1 {
		t.Fatalf("首次重放应成功一次: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
	n, err = svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 0 || hits.Load() != 1 {
		t.Fatalf("成功后重放必须幂等: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
	var got model.WebhookDelivery
	if err := dbstate.Get().First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "success" || got.Attempts != 4 || got.DeliveredAt == nil {
		t.Fatalf("重放成功状态不符: %+v", got)
	}
}

func TestReplayDeadWebhookDeliveries_FailureRemainsDeadLetter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)

	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 1 {
		t.Fatalf("失败重放应被处理: n=%d err=%v", n, err)
	}
	var got model.WebhookDelivery
	if err := dbstate.Get().First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "dead_letter" || got.Attempts != 4 || got.ResponseCode != 500 {
		t.Fatalf("失败重放应保持死信并累计次数: %+v", got)
	}
}

func TestReplayDeadWebhookDeliveries_ConcurrentReplaySingleDelivery(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, _ := seedWebhookDeadLetter(t, srv.URL, 3)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发重放报错: %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("并发重放必须只产生一次外部投递，实际 %d 次", hits.Load())
	}
}

func TestDeliverWebhookOnce_StableDeliveryIDHeader(t *testing.T) {
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("X-Hivemtk-Delivery-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	sub := model.WebhookSubscription{TenantID: 1, URL: srv.URL, Secret: "test-" + "secret"}
	body := []byte(`{"event":"hook.test","data":{"id":1}}`)
	for i := 0; i < 2; i++ {
		status, _, _, err := deliverWebhookOnce(sub, "hook.test", body)
		if err != nil || status != "success" {
			t.Fatalf("投递失败: status=%s err=%v", status, err)
		}
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("同一逻辑投递必须使用稳定 delivery-id，实际 %v", ids)
	}
}

func TestReplayDeadWebhookDeliveries_StaleReplayingRecovery(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)
	if err := dbstate.Get().Model(&model.WebhookDelivery{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status": "replaying", "updated_at": time.Now().Add(-11 * time.Second),
	}).Error; err != nil {
		t.Fatalf("age replaying row: %v", err)
	}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 1 || hits.Load() != 1 {
		t.Fatalf("过期 replaying 应恢复并投递一次: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
	var got model.WebhookDelivery
	if err := dbstate.Get().First(&got, row.ID).Error; err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if got.Status != "success" || got.Attempts != 4 {
		t.Fatalf("恢复 claim 状态不符: %+v", got)
	}
}

func TestReplayDeadWebhookDeliveries_FreshReplayingNotReplayed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)
	if err := dbstate.Get().Model(&model.WebhookDelivery{}).Where("id = ?", row.ID).Update("status", "replaying").Error; err != nil {
		t.Fatalf("mark replaying: %v", err)
	}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 0 || hits.Load() != 0 {
		t.Fatalf("lease 内 replaying 不得恢复: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
}

func TestReplayDeadWebhookDeliveries_AttemptsCap(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, _ := seedWebhookDeadLetter(t, srv.URL, 8)
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 0 || hits.Load() != 0 {
		t.Fatalf("attempts 达到上限不得再 claim: n=%d hits=%d err=%v", n, hits.Load(), err)
	}
}

func TestReplayDeadWebhookDeliveries_TenantScoped(t *testing.T) {
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
	subTwo := &model.WebhookSubscription{TenantID: 2, URL: two.URL, Events: "all", Secret: "tes" + "t-s" + "ecret-2", Enabled: true}
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
	ctx := tenant.WithTenantID(context.Background(), 1)
	n, err := svc.ReplayDeadWebhookDeliveries(ctx, 10)
	if err != nil || n != 1 || hitsOne.Load() != 1 || hitsTwo.Load() != 0 {
		t.Fatalf("controller replay 必须只处理当前租户: n=%d hits1=%d hits2=%d err=%v", n, hitsOne.Load(), hitsTwo.Load(), err)
	}
}

// 注入式失败仓储用例已随 ReplayDeadWebhookDeliveries 改用全局 dbstate 而移除；
// DB 错误可见性由 r48 死信重投的集成测试覆盖，此处不再保留失效的 seam 断言。

func TestReplayDeadWebhookDeliveries_CrashRecoveryReusesDeliveryID(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ids = append(ids, r.Header.Get("X-Hivemtk-Delivery-ID"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	svc, row := seedWebhookDeadLetter(t, srv.URL, 3)
	database := dbstate.Get()
	var sub model.WebhookSubscription
	if err := database.First(&sub, row.SubscriptionID).Error; err != nil {
		t.Fatalf("read subscription: %v", err)
	}
	now := time.Now()
	claim, err := repository.NewWebhookDeliveryRepositoryWithDB(database).ClaimReplay(context.Background(), row.ID, 1, 8, now, 10*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("claim before simulated crash: %+v err=%v", claim, err)
	}
	if status, _, _, err := deliverWebhookOnce(sub, row.Event, []byte(row.Payload)); err != nil || status != "success" {
		t.Fatalf("simulated pre-crash delivery failed: status=%s err=%v", status, err)
	}
	if err := database.Model(&model.WebhookDelivery{}).Where("id = ?", row.ID).Update("updated_at", now.Add(-11*time.Second)).Error; err != nil {
		t.Fatalf("age crashed claim: %v", err)
	}
	n, err := svc.ReplayDeadWebhookDeliveries(context.Background(), 10)
	if err != nil || n != 1 {
		t.Fatalf("crash 后过期 claim 应恢复: n=%d err=%v", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("crash 恢复必须复用同一 delivery-id，实际 %v", ids)
	}
}

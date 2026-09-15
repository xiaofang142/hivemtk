package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func newRecoveryFixture(t *testing.T) (*webhookRecoveryScanner, *repository.WebhookEventRepository) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.WebhookEvent{})
	if db == nil {
		t.Skip("test db unavailable")
	}
	repo := repository.NewWebhookEventRepository()
	repository.SetWebhookEventRepoDB(repo, db)
	svc := &WebhookService{eventRepo: repo}
	sc := newWebhookRecoveryScanner(svc)
	if sc == nil {
		t.Fatal("scanner should not be nil with db")
	}
	return sc, repo
}

func backdateEvent(t *testing.T, repo *repository.WebhookEventRepository, evt *model.WebhookEvent, d time.Duration) {
	t.Helper()
	if err := repo.GetDB().Model(&model.WebhookEvent{}).Where("id = ?", evt.ID).
		Update("created_at", time.Now().Add(-d)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

func TestRecoveryListStaleUnprocessed(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()

	stale := &model.WebhookEvent{Platform: "whatsapp", EventID: "evt_stale_1", AccountID: "1", RawData: "{}", Processed: false}
	fresh := &model.WebhookEvent{Platform: "whatsapp", EventID: "evt_fresh_1", AccountID: "1", RawData: "{}", Processed: false}
	done := &model.WebhookEvent{Platform: "whatsapp", EventID: "evt_done_1", AccountID: "1", RawData: "{}", Processed: true}
	for _, e := range []*model.WebhookEvent{stale, fresh, done} {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("create %s: %v", e.EventID, err)
		}
	}
	backdateEvent(t, repo, stale, sc.cooldown+time.Minute)
	backdateEvent(t, repo, done, sc.cooldown+time.Minute)

	events, err := repo.ListStaleUnprocessed(ctx, time.Now().Add(-sc.cooldown), 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[string]bool{}
	for _, e := range events {
		ids[e.EventID] = true
	}
	if !ids["evt_stale_1"] {
		t.Fatalf("stale unprocessed event should be listed")
	}
	if ids["evt_fresh_1"] {
		t.Fatalf("fresh event within cooldown should not be listed")
	}
	if ids["evt_done_1"] {
		t.Fatalf("processed event should not be listed")
	}
}

func TestReplayLegacyRowWithoutAccountTerminates(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()
	evt := &model.WebhookEvent{Platform: "whatsapp", EventID: "evt_legacy_1", AccountID: "", RawData: "{}", Processed: false}
	if err := repo.Create(ctx, evt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := sc.replay(ctx, evt); got {
		t.Fatalf("legacy row should not be replayable")
	}
	fresh, err := repo.GetByID(ctx, evt.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !fresh.Processed {
		t.Fatalf("legacy row should be marked processed to stop infinite replay")
	}
}

func TestReplayTruncatedPayloadTerminates(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()
	evt := &model.WebhookEvent{Platform: "whatsapp", EventID: "evt_trunc_1", AccountID: "1", RawData: `{"a":1` + webhookRawTruncatedSuffix, Processed: false}
	if err := repo.Create(ctx, evt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := sc.replay(ctx, evt); got {
		t.Fatalf("truncated payload should not be replayable")
	}
	fresh, _ := repo.GetByID(ctx, evt.ID)
	if !fresh.Processed {
		t.Fatalf("truncated row should be marked processed")
	}
}

func TestReplayPoisonEventGivesUp(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()
	evt := &model.WebhookEvent{Platform: "custom", EventID: "evt_poison_" + time.Now().Format("150405.000000000"), AccountID: "1", RawData: "not-json", Processed: false}
	if err := repo.Create(ctx, evt); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < webhookRecoveryMaxRetry; i++ {
		sc.incrRetry(ctx, evt.EventID)
	}
	if got := sc.replay(ctx, evt); got {
		t.Fatalf("poison event should give up")
	}
	fresh, _ := repo.GetByID(ctx, evt.ID)
	if !fresh.Processed {
		t.Fatalf("poison event should be marked processed")
	}
}

func TestRecoveryScannerDisabled(t *testing.T) {
	db := testutil.NewTestDB(t, &model.WebhookEvent{})
	if db == nil {
		t.Skip("test db unavailable")
	}
	t.Setenv("WEBHOOK_RECOVERY_ENABLED", "false")
	repo := repository.NewWebhookEventRepository()
	repository.SetWebhookEventRepoDB(repo, db)
	svc := &WebhookService{eventRepo: repo}
	svc.startRecoveryScanner()
}

func TestReplayMarksProcessedOnUnknownPlatform(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()
	called := 0
	sc.handleFn = func(ctx context.Context, job *webhookJob) {
		called++
		job.event.Processed = true
		_ = repo.Update(ctx, job.event)
	}
	raw := `{"event_id":"evt_replay_ok","event_type":"message","content":"hi","from":"u1"}`
	evt := &model.WebhookEvent{Platform: "custom", EventID: "evt_replay_ok", AccountID: "1", RawData: raw, Processed: false}
	if err := repo.Create(ctx, evt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := sc.replay(ctx, evt); !got {
		t.Fatalf("replay should succeed")
	}
	if called != 1 {
		t.Fatalf("handleFn should be called once, got %d", called)
	}
	fresh, _ := repo.GetByID(ctx, evt.ID)
	if !fresh.Processed {
		t.Fatalf("event should be marked processed after replay")
	}
}

func TestScanOnceReplaysStaleBatch(t *testing.T) {
	sc, repo := newRecoveryFixture(t)
	ctx := context.Background()
	sc.handleFn = func(ctx context.Context, job *webhookJob) {
		job.event.Processed = true
		_ = repo.Update(ctx, job.event)
	}
	for i := 0; i < 3; i++ {
		evt := &model.WebhookEvent{Platform: "custom", EventID: "evt_batch_" + time.Now().Format("150405.000000000"), AccountID: "1", RawData: "{}", Processed: false}
		if err := repo.Create(ctx, evt); err != nil {
			t.Fatalf("create: %v", err)
		}
		backdateEvent(t, repo, evt, sc.cooldown+time.Minute)
		time.Sleep(2 * time.Millisecond)
	}
	n := sc.scanOnce(ctx)
	if n != 3 {
		t.Fatalf("expected 3 replayed, got %d", n)
	}
}

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func webhookAdmissionRequest(eventID string) *ReceiveRequest {
	return &ReceiveRequest{
		Channel:   ChannelDouyin,
		AccountID: "channel04-account",
		Body:      []byte(`{"event_id":"` + eventID + `","content":"channel-04"}`),
	}
}

func TestWebhookReceive_RateLimitDoesNotReserveDedupKey(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	const eventID = "channel04-rate-limited"
	cleanupWebhookDedupKeysForTest(t, eventID)

	key := string(ChannelDouyin) + ":channel04-account"
	bucket := &tokenBucket{
		capacity:   1,
		refillRate: 0,
		tokens:     0,
		lastRefill: time.Now(),
	}
	svc.rlMu.Lock()
	svc.rlBuckets[key] = bucket
	svc.rlMu.Unlock()

	result, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if result.Accepted || !result.RateLimit {
		t.Fatalf("expected rate-limited rejection, got %+v", result)
	}

	exists, err := cache.GetGlobalCache().Exists(context.Background(), "mtk:webhook:dedup:"+eventID)
	if err != nil {
		t.Fatalf("check dedup key: %v", err)
	}
	if exists {
		t.Fatal("rate-limited admission must not reserve a deduplication key")
	}

	bucket.mu.Lock()
	bucket.tokens = 1
	bucket.lastRefill = time.Now()
	bucket.mu.Unlock()
	retry, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("retry after rate limit: %v", err)
	}
	if !retry.Accepted || retry.Duplicate {
		t.Fatalf("retry after rate limit should admit the event, got %+v", retry)
	}

	var count int64
	if err := db.Model(&model.WebhookEvent{}).Where("event_id = ?", eventID).Count(&count).Error; err != nil {
		t.Fatalf("count durable events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one durable event after retry, got %d", count)
	}
}

func TestWebhookReceive_PersistFailureCanRetry(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	const eventID = "channel04-persist-retry"
	cleanupWebhookDedupKeysForTest(t, eventID)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	first, err := svc.Receive(cancelled, webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("cancelled Receive: %v", err)
	}
	if first.Accepted || first.Duplicate {
		t.Fatalf("failed persistence must not be acknowledged, got %+v", first)
	}
	if !strings.HasPrefix(first.Reason, "persist error:") {
		t.Fatalf("expected persistence failure, got %+v", first)
	}

	exists, err := cache.GetGlobalCache().Exists(context.Background(), "mtk:webhook:dedup:"+eventID)
	if err != nil {
		t.Fatalf("check dedup key after failed persistence: %v", err)
	}
	if exists {
		t.Fatal("failed persistence must not leave a deduplication reservation")
	}

	retry, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("retry Receive: %v", err)
	}
	if !retry.Accepted || retry.Duplicate {
		t.Fatalf("retry should durably admit the event, got %+v", retry)
	}

	var count int64
	if err := db.Model(&model.WebhookEvent{}).Where("event_id = ?", eventID).Count(&count).Error; err != nil {
		t.Fatalf("count durable events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one durable event after retry, got %d", count)
	}
}

func TestWebhookReceive_StaleReservationDoesNotBlockDurableAdmission(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	const eventID = "channel04-stale-reservation"
	cleanupWebhookDedupKeysForTest(t, eventID)
	if err := cache.GetGlobalCache().Set(context.Background(), "mtk:webhook:dedup:"+eventID, "stale-owner", time.Minute); err != nil {
		t.Fatalf("seed stale reservation: %v", err)
	}

	result, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !result.Accepted || result.Duplicate {
		t.Fatalf("stale reservation must not be treated as durable duplicate, got %+v", result)
	}

	var count int64
	if err := db.Model(&model.WebhookEvent{}).Where("event_id = ?", eventID).Count(&count).Error; err != nil {
		t.Fatalf("count durable events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one durable event, got %d", count)
	}
}

func TestWebhookReceive_ConcurrentReplayCreatesOneRowAndOneJob(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())
	// Stop workers before the requests so the test can count every enqueue without
	// racing a worker that may immediately consume the channel.
	svc.Stop(context.Background())
	svc.queue = make(chan *webhookJob, 32)

	const eventID = "channel04-concurrent-replay"
	cleanupWebhookDedupKeysForTest(t, eventID)

	const attempts = 8
	results := make(chan *ReceiveResult, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
			if err != nil {
				t.Errorf("concurrent Receive: %v", err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)

	accepted, duplicates := 0, 0
	for result := range results {
		if !result.Accepted {
			t.Errorf("concurrent replay must not reject: %+v", result)
			continue
		}
		if result.Duplicate {
			duplicates++
		} else {
			accepted++
		}
	}
	if accepted != 1 || duplicates != attempts-1 {
		t.Fatalf("expected one admission and %d duplicate responses, got accepted=%d duplicates=%d", attempts-1, accepted, duplicates)
	}

	var count int64
	if err := db.Model(&model.WebhookEvent{}).Where("event_id = ?", eventID).Count(&count).Error; err != nil {
		t.Fatalf("count durable events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one durable event, got %d", count)
	}
	if got := len(svc.queue); got != 1 {
		t.Fatalf("expected one queued job, got %d", got)
	}
}

func TestWebhookReceive_DurableEventRecoversWhenQueueUnavailable(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	db := setupWebhookTestDB(t)
	svc := NewWebhookService()
	svc.Stop(context.Background())
	// Model an unavailable in-memory queue after durable admission: the persisted
	// row remains eligible for the recovery scanner.
	svc.queue = nil

	eventID := fmt.Sprintf("channel04-recovery-%d", time.Now().UnixNano())
	retryKey := "mtk:webhook:retry:" + eventID
	if err := cache.GetGlobalCache().Delete(context.Background(), retryKey); err != nil {
		t.Fatalf("clear retry counter: %v", err)
	}
	t.Cleanup(func() { _ = cache.GetGlobalCache().Delete(context.Background(), retryKey) })

	result, err := svc.Receive(context.Background(), webhookAdmissionRequest(eventID))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !result.Accepted || result.Duplicate {
		t.Fatalf("durable admission must be accepted before recovery, got %+v", result)
	}
	if exists, err := cache.GetGlobalCache().Exists(context.Background(), "mtk:webhook:dedup:"+eventID); err != nil {
		t.Fatalf("check dedup key: %v", err)
	} else if exists {
		t.Fatal("durable admission must not depend on a reservation-only dedup key")
	}

	var event model.WebhookEvent
	if err := db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		t.Fatalf("load durable event: %v", err)
	}
	if err := db.Model(&model.WebhookEvent{}).Where("id = ?", event.ID).
		Update("created_at", time.Now().Add(-2*time.Minute)).Error; err != nil {
		t.Fatalf("age event for recovery scan: %v", err)
	}

	processed := 0
	scanner := &webhookRecoveryScanner{
		eventRepo: svc.eventRepo,
		cooldown:  time.Minute,
		batchSize: 10,
		handleFn: func(ctx context.Context, job *webhookJob) {
			processed++
			job.event.Processed = true
			if err := svc.eventRepo.Update(ctx, job.event); err != nil {
				t.Errorf("mark recovered event processed: %v", err)
			}
		},
	}
	ctx := tenant.ContextWithDefault(context.Background())
	if got := scanner.scanTenant(ctx, tenant.DefaultTenantID); got != 1 {
		t.Fatalf("expected one recovered durable event, got %d", got)
	}
	if processed != 1 {
		t.Fatalf("expected one recovery dispatch, got %d", processed)
	}
	if got := scanner.scanTenant(ctx, tenant.DefaultTenantID); got != 0 || processed != 1 {
		t.Fatalf("processed event must not replay again: replayed=%d dispatches=%d", got, processed)
	}

	var count int64
	if err := db.Model(&model.WebhookEvent{}).Where("event_id = ? AND processed = ?", eventID, true).Count(&count).Error; err != nil {
		t.Fatalf("verify processed durable event: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one processed durable event after recovery, got %d", count)
	}
}

// 验证进程在事件提交后、入队前退出时，新进程能从持久化记录恢复且不重复处理。
func TestWebhookReceive_ProcessRestartRecovery(t *testing.T) {
	if stage := os.Getenv("CHANNEL04_RESTART_STAGE"); stage != "" {
		runWebhookRestartChild(t, stage)
		return
	}
	db := setupWebhookTestDB(t)
	dialect, ok := db.Dialector.(*postgres.Dialector)
	if !ok || dialect.DSN == "" {
		t.Fatal("期望使用专用 PostgreSQL 测试连接")
	}
	// Reuse the parent's existing isolated slot. Child processes must never call
	// NewTestDB, which would reset a database instead of reopening this fixture.
	dsnFile := filepath.Join(t.TempDir(), "connection")
	if err := os.WriteFile(dsnFile, []byte(dialect.DSN), 0600); err != nil {
		t.Fatal("写入子进程专用连接文件失败")
	}
	eventID := fmt.Sprintf("channel04-restart-%d", time.Now().UnixNano())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runChild := func(stage string, exitCode int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWebhookReceive_ProcessRestartRecovery$", "-test.timeout=25s")
		cmd.Env = append(os.Environ(), "CHANNEL04_RESTART_STAGE="+stage,
			"CHANNEL04_RESTART_CONNECTION="+dsnFile, "CHANNEL04_RESTART_EVENT="+eventID)
		// Child output may contain connection diagnostics; report only exit status.
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("子进程阶段 %s 超时", stage)
		}
		if cmd.ProcessState == nil {
			t.Fatalf("无法启动子进程阶段 %s", stage)
		}
		if got := cmd.ProcessState.ExitCode(); got != exitCode || (exitCode == 0 && err != nil) {
			t.Fatalf("子进程阶段 %s 退出码 = %d，期望 %d", stage, got, exitCode)
		}
	}

	runChild("admit", 86)
	var event model.WebhookEvent
	if err := db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		t.Fatalf("接入进程退出后应保留持久化事件：%v", err)
	}
	if event.Processed {
		t.Fatal("入队前进程退出后事件应保持未处理")
	}
	if err := db.Model(&event).Update("created_at", time.Now().Add(-2*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	runChild("recover", 0)
	var count int64
	if err := db.Model(&model.WebhookEvent{}).
		Where("event_id = ? AND processed = ?", eventID, true).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("进程重启恢复后应有一条已处理事件，实际 %d", count)
	}
}

func runWebhookRestartChild(t *testing.T, stage string) {
	t.Helper()
	if stage != "admit" && stage != "recover" {
		t.Fatal("未知的重启测试阶段")
	}
	eventID := os.Getenv("CHANNEL04_RESTART_EVENT")
	if eventID == "" {
		t.Fatal("缺少重启测试事件 ID")
	}
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	dsn, err := os.ReadFile(os.Getenv("CHANNEL04_RESTART_CONNECTION"))
	if err != nil {
		t.Fatal("读取子进程专用连接文件失败")
	}
	database, err := gorm.Open(postgres.Open(string(dsn)), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatal("重新连接父进程已建立的测试数据库失败")
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatal("获取子进程数据库连接池失败")
	}
	t.Cleanup(func() { _ = pool.Close() })
	tenancyidx.RegisterTenantCallbacks(database)
	dbstate.Set(database)
	t.Cleanup(func() { dbstate.Set(nil) })
	repo := repository.NewWebhookEventRepository()
	ctx := tenant.ContextWithDefault(context.Background())
	if stage == "admit" {
		// Exit after the actual insert transaction commits, before Receive can
		// enqueue or acknowledge. No service cleanup or memory state survives.
		if err := database.Callback().Create().After("gorm:commit_or_rollback_transaction").
			Register("channel04:exit_after_commit", func(tx *gorm.DB) {
				if tx.Error == nil && tx.Statement.Table == "webhook_events" {
					os.Exit(86)
				}
			}); err != nil {
			t.Fatal(err)
		}
		svc := NewWebhookService()
		svc.Stop(context.Background())
		_, _ = svc.Receive(ctx, webhookAdmissionRequest(eventID))
		t.Fatal("事件提交后应立即退出进程")
	}
	processed := 0
	scanner := &webhookRecoveryScanner{
		eventRepo: repo, cooldown: time.Minute, batchSize: 10,
		handleFn: func(ctx context.Context, job *webhookJob) {
			processed++
			if job.event.EventID != eventID || string(job.raw) != string(webhookAdmissionRequest(eventID).Body) {
				t.Error("跨进程恢复必须保留事件身份和载荷")
			}
			job.event.Processed = true
			if err := repo.Update(ctx, job.event); err != nil {
				t.Error("标记重启恢复事件为已处理失败")
			}
		},
	}
	if got := scanner.scanTenant(ctx, tenant.DefaultTenantID); got != 1 || processed != 1 {
		t.Fatalf("新进程应仅恢复一次：重放=%d 已处理=%d", got, processed)
	}
	if got := scanner.scanTenant(ctx, tenant.DefaultTenantID); got != 0 || processed != 1 {
		t.Fatalf("已处理事件不应再次重放：重放=%d 已处理=%d", got, processed)
	}
}

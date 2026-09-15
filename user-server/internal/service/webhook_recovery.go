package service

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	webhookRecoveryCooldownDefault = 60 * time.Second
	webhookRecoveryIntervalDefault = 30 * time.Second
	webhookRecoveryBatchDefault    = 100
	webhookRecoveryMaxRetry        = 5
	webhookRecoveryGateTTL         = 5 * time.Minute
	webhookRecoveryRetryTTL        = 24 * time.Hour

	webhookRawTruncatedSuffix = "...[truncated]"
)

type webhookRecoveryScanner struct {
	svc       *WebhookService
	eventRepo *repository.WebhookEventRepository

	handleFn func(ctx context.Context, job *webhookJob)

	interval  time.Duration
	cooldown  time.Duration
	batchSize int
	enabled   bool

	stopCh chan struct{}
	mu     sync.Mutex

	lastID uint64
}

func newWebhookRecoveryScanner(svc *WebhookService) *webhookRecoveryScanner {
	if svc == nil {
		return nil
	}
	db := svc.lazyDB()
	if db == nil {
		return nil
	}
	eventRepo := svc.eventRepo
	if eventRepo == nil {
		eventRepo = repository.NewWebhookEventRepository()
		repository.SetWebhookEventRepoDB(eventRepo, db)
	}
	return &webhookRecoveryScanner{
		svc:       svc,
		eventRepo: eventRepo,
		handleFn:  svc.handleJob,
		interval:  webhookEnvSeconds("WEBHOOK_RECOVERY_INTERVAL_SECONDS", webhookRecoveryIntervalDefault),
		cooldown:  webhookEnvSeconds("WEBHOOK_RECOVERY_COOLDOWN_SECONDS", webhookRecoveryCooldownDefault),
		batchSize: webhookEnvInt("WEBHOOK_RECOVERY_BATCH", webhookRecoveryBatchDefault),
		enabled:   os.Getenv("WEBHOOK_RECOVERY_ENABLED") != "false",
		stopCh:    make(chan struct{}),
	}
}

func (sc *webhookRecoveryScanner) markProcessed(ctx context.Context, evt *model.WebhookEvent) {
	now := time.Now()
	evt.Processed = true
	evt.ProcessedAt = &now
	if sc.eventRepo != nil {
		_ = sc.eventRepo.Update(ctx, evt)
	}
}

func (s *WebhookService) startRecoveryScanner() {
	sc := newWebhookRecoveryScanner(s)
	if sc == nil || !sc.enabled {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()

	ctx := context.Background()
	utils.SafeGo(ctx, "webhook.recovery_scanner", func(ctx context.Context) {
		defer s.wg.Done()
		ticker := time.NewTicker(sc.interval)
		defer ticker.Stop()
		logger.Infof("[WebhookRecovery] scanner started interval=%s cooldown=%s batch=%d", sc.interval, sc.cooldown, sc.batchSize)
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				sc.scanOnce(ctx)
			}
		}
	})
}

func (sc *webhookRecoveryScanner) scanOnce(ctx context.Context) int {
	if sc.eventRepo == nil {
		return 0
	}
	cutoff := time.Now().Add(-sc.cooldown)
	events, err := sc.eventRepo.ListStaleUnprocessedAfter(ctx, cutoff, sc.lastID, sc.batchSize)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[WebhookRecovery] scan query failed")
		return 0
	}
	if len(events) == 0 {
		sc.lastID = 0
		return 0
	}
	if len(events) < sc.batchSize {
		sc.lastID = 0
	} else {
		sc.lastID = uint64(events[len(events)-1].ID)
	}
	replayed := 0
	for _, evt := range events {
		token, claimed := sc.claim(ctx, evt)
		if !claimed {
			continue
		}
		sc.replay(ctx, evt)

		_, _ = cache.GetGlobalCache().ReleaseLock(context.Background(),
			"mtk:webhook:recovering:"+strconv.FormatUint(uint64(evt.ID), 10), token)
		replayed++
	}
	if replayed > 0 {
		logger.Infof("[WebhookRecovery] replayed %d/%d stale events", replayed, len(events))
	}
	return replayed
}

func (sc *webhookRecoveryScanner) claim(ctx context.Context, evt *model.WebhookEvent) (string, bool) {
	key := "mtk:webhook:recovering:" + strconv.FormatUint(uint64(evt.ID), 10)
	token := "recovery-" + strconv.FormatUint(uint64(evt.ID), 10) + "-" + time.Now().Format("150405.000000000")
	ok, err := cache.GetGlobalCache().SetNX(context.Background(), key, token, webhookRecoveryGateTTL)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("event_id", evt.ID).Msg("[WebhookRecovery] claim backend error, proceeding (fail-open)")
		return "", true
	}
	if !ok {
		return "", false
	}
	return token, true
}

func (sc *webhookRecoveryScanner) replay(ctx context.Context, evt *model.WebhookEvent) bool {

	if n := sc.incrRetry(ctx, evt.EventID); n > webhookRecoveryMaxRetry {
		logger.Ctx(ctx).Warn().Uint("event_id", evt.ID).Str("event", evt.EventID).Int64("attempts", n).Msg("[WebhookRecovery] poison event, giving up")
		sc.markProcessed(ctx, evt)
		return false
	}

	if evt.RawData == "" || strings.HasSuffix(evt.RawData, webhookRawTruncatedSuffix) {
		logger.Ctx(ctx).Warn().Uint("event_id", evt.ID).Msg("[WebhookRecovery] raw payload missing/truncated, cannot replay")
		sc.markProcessed(ctx, evt)
		return false
	}

	if evt.AccountID == "" {
		logger.Ctx(ctx).Warn().Uint("event_id", evt.ID).Str("platform", evt.Platform).Msg("[WebhookRecovery] legacy row without account_id, cannot replay")
		sc.markProcessed(ctx, evt)
		return false
	}

	job := &webhookJob{
		event:   evt,
		raw:     []byte(evt.RawData),
		header:  nil,
		channel: WebhookChannel(evt.Platform),
		account: evt.AccountID,
		payload: nil,
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[WebhookRecovery] replay panic event=%s: %v", evt.EventID, r)
			sc.markProcessed(ctx, evt)
		}
	}()
	latest, err := sc.eventRepo.GetByID(ctx, evt.ID)
	if err == nil && latest != nil && latest.Processed {
		return false
	}
	sc.handleFn(ctx, job)
	if fresh, _ := sc.eventRepo.GetByID(ctx, evt.ID); fresh != nil && !fresh.Processed {

		return false
	}
	return true
}

func (sc *webhookRecoveryScanner) incrRetry(ctx context.Context, eventID string) int64 {
	if eventID == "" {
		return 0
	}
	key := "mtk:webhook:retry:" + eventID
	n, err := cache.GetGlobalCache().Incr(context.Background(), key, webhookRecoveryRetryTTL)
	if err != nil {

		return 0
	}
	return n
}

func webhookEnvSeconds(name string, def time.Duration) time.Duration {
	n := webhookEnvInt(name, int(def/time.Second))
	if n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}

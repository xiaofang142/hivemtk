package service

import (
	"context"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
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

	// lastIDByTenant 各租户自己的 id 游标（防头部反复失败的事件占满批次窗口饿死后续积压）。
	//
	// 必须按租户分账：扇出之后拿租户 A 的 id 去当租户 B 的下界，会把 B 更早的积压整段跳过，
	// 而"跳过"在恢复扫描器这里的形态是**这批事件永远不重放**（游标只有在批次不满时才归零）。
	lastIDByTenant map[uint]uint64
}

func newWebhookRecoveryScanner(svc *WebhookService) *webhookRecoveryScanner {
	if svc == nil {
		return nil
	}
	// E-4 下沉：装配判据走 connected（eventRepo 有 DB），不再拿句柄比 nil。
	// 恢复扫描器的职责是重放 webhook_events：eventRepo 没连上 ⇒ 没有可扫的表。
	if !svc.connected() {
		return nil
	}
	eventRepo := svc.eventRepo
	if eventRepo == nil {
		eventRepo = repository.NewWebhookEventRepository()
	}
	return &webhookRecoveryScanner{
		svc:       svc,
		eventRepo: eventRepo,
		handleFn:  svc.handleJob,
		// SaaS 配置面改造（B8，2026-11-17）：WEBHOOK_RECOVERY_* 环境变量退役，
		// 旋钮入参数中心 webhook 组（recovery_enabled/interval/cooldown/batch）。
		// 构造期读，与 worker_count/queue_size 同批：改参数对下次启动的扫描器生效。
		interval:       webhookRecoveryInterval(context.Background()),
		cooldown:       webhookRecoveryCooldown(context.Background()),
		batchSize:      webhookRecoveryBatch(context.Background()),
		enabled:        webhookRecoveryEnabledRaw(context.Background()),
		stopCh:         make(chan struct{}),
		lastIDByTenant: map[uint]uint64{},
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

// scanOnce 扫一轮：**按租户扇出**（specs/001-saas-multi-tenant T020）。
//
// 为什么必须 Fanout 而不是 system scope：TENANT_MODE=on 时 webhook_events 是租户表，扫描器由
// startRecoveryScanner 用 context.Background() 起、没有 HTTP 请求 ctx 可绑，无租户 ctx 的
// ListStaleUnprocessedAfter（query）会被隔离 callback fail-closed 拒绝（生产日志实证：
// "[WebhookRecovery] scan query failed: [tenant] 表 webhook_events 需要租户上下文但 ctx
// 未绑定（query）"）—— 症状是积压事件一轮都不重放。
//
// 扫描放行只是半件事：往下 replay → handleJob 是**跨模块执行路径**（落 unified_messages、
// 触发 AI、回写事件行），全靠 ctx 注入租户。走 system scope 虽能取到全量行，但执行链的
// Create 会被 fillCreateForTenant 放行到列默认租户 1（串写默认租户），Query 也会摊成跨租户
// 全量 —— 与 sop_scheduler.tick 同一结论：执行链吃 ctx 租户 ⇒ 逐租户注入（Fanout）。
// off 模式退化为单默认租户，等价既有行为。
func (sc *webhookRecoveryScanner) scanOnce(ctx context.Context) int {
	if sc.eventRepo == nil {
		return 0
	}
	total, err := tenant.Fanout(ctx, nil, func(tctx context.Context, tenantID uint) (int64, error) {
		return sc.scanTenant(tctx, tenantID), nil
	})
	if err != nil {
		// 只有 Fanout 的结构性失败会走到这里（活跃租户枚举器未装配 / ctx 已取消）：
		// 照样出声 —— 这里静默的形态就是"积压事件再也不重放，且一行日志都没有"。
		logger.Ctx(ctx).Warn().Err(err).Msg("[WebhookRecovery] scan fanout failed")
		return 0
	}
	return int(total)
}

// scanTenant 单租户的一轮扫描（scanOnce 的扇出回调体），返回本租户重放成功的事件数。
//
// 查询失败就地出声并返回 0：Fanout 只在**全部**租户都失败时才把错误汇总回外层，
// 单租户失败若不就地报，就会被它吞掉 —— 原实现那句 "[WebhookRecovery] scan query failed"
// 因此原样保留（多带一枚 tenant_id，好定位是哪个租户的积压没动）。
func (sc *webhookRecoveryScanner) scanTenant(ctx context.Context, tenantID uint) int64 {
	if sc.lastIDByTenant == nil { // 防手工构造的 scanner（测试替身）踩 nil map 写
		sc.lastIDByTenant = map[uint]uint64{}
	}
	cutoff := time.Now().Add(-sc.cooldown)
	events, err := sc.eventRepo.ListStaleUnprocessedAfter(ctx, cutoff, sc.lastIDByTenant[tenantID], sc.batchSize)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("tenant_id", tenantID).Msg("[WebhookRecovery] scan query failed")
		return 0
	}
	if len(events) == 0 {
		sc.lastIDByTenant[tenantID] = 0
		return 0
	}
	if len(events) < sc.batchSize {
		sc.lastIDByTenant[tenantID] = 0
	} else {
		sc.lastIDByTenant[tenantID] = uint64(events[len(events)-1].ID)
	}
	replayed := 0
	for _, evt := range events {
		token, claimed := sc.claim(ctx, evt)
		if !claimed {
			continue
		}
		sc.replay(ctx, evt)

		_, _ = cache.GetGlobalCache().ReleaseLock(ctx,
			"mtk:webhook:recovering:"+strconv.FormatUint(uint64(evt.ID), 10), token)
		replayed++
	}
	if replayed > 0 {
		logger.Infof("[WebhookRecovery] replayed %d/%d stale events (tenant=%d)", replayed, len(events), tenantID)
	}
	return int64(replayed)
}

func (sc *webhookRecoveryScanner) claim(ctx context.Context, evt *model.WebhookEvent) (string, bool) {
	key := "mtk:webhook:recovering:" + strconv.FormatUint(uint64(evt.ID), 10)
	token := "recovery-" + strconv.FormatUint(uint64(evt.ID), 10) + "-" + time.Now().Format("150405.000000000")
	ok, err := cache.GetGlobalCache().SetNX(ctx, key, token, webhookRecoveryGateTTL)
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
			logger.Errorf("[WebhookRecovery] replay panic event=%s: %v\n%s", evt.EventID, r, debug.Stack())
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
	n, err := cache.GetGlobalCache().Incr(ctx, key, webhookRecoveryRetryTTL)
	if err != nil {

		return 0
	}
	return n
}

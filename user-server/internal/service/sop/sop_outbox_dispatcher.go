package sop

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// SOPDispatchSender 调度任务派发接口
//
// 抽象自 SOPExecutionDispatcher.DispatchOrLog，便于测试注入 mock，
// 同时解耦 Outbox/StuckDetector 与具体调度器实现。
type SOPDispatchSender interface {
	DispatchOrLog(task *dispatchTask)
}

// SOPOutboxDispatcher Outbox 调度器
//
// 独立 goroutine 轮询 sop_timers 表，扫描到期的 timer 后派发任务给 SOPExecutionDispatcher。
// 与 SOPScheduler 解耦：SOPScheduler 触发新执行，OutboxDispatcher 处理执行内事件。
//
// S1-2（Wait 语义）：扫描 max_wait_at 已过期的 pending timer → 转 skipped + 事件记录，
// 并以 SkipWait 任务通知 dispatcher "已过期视为满足立即跳过"。
// S1-5（Outbox 死信）：认领失败累计 claim_count≥5 → status=dead_letter + 告警日志。
type SOPOutboxDispatcher struct {
	timerRepo      *repository.SOPTimerRepository
	eventRepo      *repository.SOPExecEventRepository
	execDispatcher SOPDispatchSender
	tickInterval   time.Duration
	batchSize      int
	stopCh         chan struct{}
	wg             sync.WaitGroup
	runMu          sync.Mutex
	running        bool
}

const (
	sopTimerStatusPending    = "pending"
	sopTimerStatusSkipped    = "skipped"
	sopTimerStatusDeadLetter = "dead_letter"

	sopTimerMaxClaims = 5
)

// NewSOPOutboxDispatcher 创建 Outbox 调度器
//
// 句柄由 repository 自取全局库。
func NewSOPOutboxDispatcher(execDispatcher SOPDispatchSender) *SOPOutboxDispatcher {
	return &SOPOutboxDispatcher{
		timerRepo:      repository.NewSOPTimerRepositoryDefault(),
		eventRepo:      repository.NewSOPExecEventRepositoryDefault(),
		execDispatcher: execDispatcher,
		tickInterval:   5 * time.Second,
		batchSize:      100,
		stopCh:         make(chan struct{}),
	}
}

// SetTickInterval 设置轮询周期（用于测试）
func (o *SOPOutboxDispatcher) SetTickInterval(ctx context.Context, d time.Duration) {
	o.tickInterval = d
}

// Start 启动 Outbox 调度器
func (o *SOPOutboxDispatcher) Start(ctx context.Context) {
	o.runMu.Lock()
	defer o.runMu.Unlock()
	if o.running {
		return
	}
	o.running = true
	o.stopCh = make(chan struct{})
	o.wg.Add(1)
	go o.loop(context.Background())
	logger.GetLogger().Info().
		Dur("tick_interval", o.tickInterval).
		Int("batch_size", o.batchSize).
		Msg("[SOPOutboxDispatcher] started")
}

// Stop 停止 Outbox 调度器
func (o *SOPOutboxDispatcher) Stop(ctx context.Context) {
	o.runMu.Lock()
	if !o.running {
		o.runMu.Unlock()
		return
	}
	o.running = false
	close(o.stopCh)
	o.runMu.Unlock()
	o.wg.Wait()
	logger.GetLogger().Info().Msg("[SOPOutboxDispatcher] stopped")
}

func (o *SOPOutboxDispatcher) loop(ctx context.Context) {
	defer o.wg.Done()
	ticker := time.NewTicker(o.tickInterval)
	defer ticker.Stop()

	o.processDueTimers(ctx)

	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.processDueTimers(ctx)
		}
	}
}

func (o *SOPOutboxDispatcher) processDueTimers(ctx context.Context) {
	if o.timerRepo == nil || o.execDispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, utils.DefaultHTTPTimeout)
	defer cancel()
	ctx = logger.WithModule(ctx, "sop_outbox")

	// 后台任务租户扇出（specs/001-saas-multi-tenant T020）。
	//
	// 选 Fanout 而不是 WithSystemScope 的理由：这一 tick 不是"纯平台级扫描"。
	// FindDueForUpdate 只是取到期行，但后续每一行都要 MarkFired 改 sop_timers、
	// DispatchOrLog 把任务推进 sop_executions / 发消息（sweep 还会 Create
	// sop_exec_events）—— 全是带租户业务语义的写动作。system scope 只豁免查询：
	// Create 会落列默认值 1，于是租户 N 的 timer 点火后事件/执行被串写到默认租户。
	// Fanout 逐活跃租户注入 ctx ⇒ 查询自动带 WHERE tenant_id=N（只看本租户的行）、
	// 点火与派发也拿到已绑定租户的 ctx，不产生任何跨租户读写；off 模式退化为单
	// 默认租户（等价既有行为）。与 sop_scheduler.tick / churn_score_job 同一形态。
	// 行上 sop_timers.tenant_id（T027 行自证）在下面仍再兜一层（timerRowTenant）。
	if _, err := tenant.Fanout(ctx, nil, func(tctx context.Context, _ uint) (int64, error) {
		o.processDueTimersForTenant(tctx)
		return 0, nil
	}); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[outbox] 租户扇出执行失败")
	}
}

// processDueTimersForTenant 单租户视角的到期 timer 处理（由 Fanout 调用，
// 入口 ctx 必定已绑定租户；off 模式绑定默认租户，回调直通）。
func (o *SOPOutboxDispatcher) processDueTimersForTenant(ctx context.Context) {
	now := time.Now()
	timers, err := o.timerRepo.FindDueForUpdate(ctx, now, o.batchSize)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[outbox] query due timers failed")
		return
	}

	o.sweepPendingTimers(ctx, now)

	if len(timers) == 0 {
		return
	}

	firedCount := 0
	for _, t := range timers {
		// 租户上下文（specs/001-saas-multi-tenant T027 行自证）：timer 行自带
		// tenant_id（写入时 callback 已填），派发与下游执行按行租户恢复上下文。
		// Fanout 已注入的 ctx 与行租户必然一致，这里再取一次是双保险：
		// 即便行值与扇出租户对不上，也只会落成"0 行受影响→跳过"，绝不跨租户写。
		// off 模式行值恒为默认租户，等价现状。
		rowTenant := timerRowTenant(&t)
		taskCtx := tenant.WithTenantID(ctx, rowTenant)

		rowsAffected, err := o.timerRepo.MarkFired(taskCtx, t.ID, now)
		if err != nil {
			logger.Ctx(taskCtx).Error().Err(err).
				Uint("timer_id", t.ID).
				Msg("[outbox] mark timer fired failed")

			o.bumpTimerClaimOrDeadLetter(taskCtx, &t, now)
			continue
		}
		if rowsAffected == 0 {
			continue
		}

		traceID := fmt.Sprintf("timer-%d", t.ID)
		o.execDispatcher.DispatchOrLog(&dispatchTask{
			ExecutionID: t.ExecutionID,
			NodeID:      t.NodeID,
			Attempt:     0,
			TraceID:     traceID,
			TimerFired:  true,
			WaitEvent:   t.WaitEvent,
			WaitPayload: t.Payload,
			TenantID:    rowTenant,
		})
		firedCount++

		logger.Ctx(ctx).Info().
			Uint("timer_id", t.ID).
			Uint("execution_id", t.ExecutionID).
			Str("node_id", t.NodeID).
			Str("wait_event", t.WaitEvent).
			Msg("[outbox] timer fired, dispatched task")
	}

	if firedCount > 0 {
		logger.Ctx(ctx).Info().
			Int("fired_count", firedCount).
			Int("scanned_count", len(timers)).
			Msg("[outbox] processed due timers")
	}
}

func (o *SOPOutboxDispatcher) sweepPendingTimers(ctx context.Context, now time.Time) {
	if o.timerRepo == nil {
		return
	}

	deadCandidates, err := o.timerRepo.FindClaimExhaustedPendingTimers(ctx, sopTimerMaxClaims, o.batchSize)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[outbox] sweep claim-exhausted timers failed")
	}
	for i := range deadCandidates {
		t := &deadCandidates[i]
		rows, txErr := o.timerRepo.TransitionPendingStatus(ctx, t.ID, sopTimerStatusDeadLetter, now)
		if txErr != nil {
			logger.Ctx(ctx).Error().Err(txErr).
				Uint("timer_id", t.ID).
				Msg("[outbox] mark dead_letter failed")
			continue
		}
		if rows == 0 {
			continue
		}
		logger.Ctx(ctx).Error().
			Uint("timer_id", t.ID).
			Uint("execution_id", t.ExecutionID).
			Str("node_id", t.NodeID).
			Int("claim_count", timerClaimCount(t)).
			Msg("[outbox][ALERT] timer moved to dead_letter: claim_count exhausted")
	}

	overdue, err := o.timerRepo.FindMaxWaitOverduePendingTimers(ctx, now, o.batchSize)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[outbox] sweep max-wait-overdue timers failed")
		return
	}

	for i := range overdue {
		t := &overdue[i]
		maxWaitAt := timerMaxWaitAt(t)
		rows, txErr := o.timerRepo.TransitionPendingStatus(ctx, t.ID, sopTimerStatusSkipped, now)
		if txErr != nil {
			logger.Ctx(ctx).Error().Err(txErr).
				Uint("timer_id", t.ID).
				Msg("[outbox] mark skipped failed")
			continue
		}
		if rows == 0 {
			continue
		}
		o.writeTimerSkippedEvent(ctx, t, now)
		logger.Ctx(ctx).Warn().
			Uint("timer_id", t.ID).
			Uint("execution_id", t.ExecutionID).
			Str("node_id", t.NodeID).
			Time("max_wait_at", maxWaitAt).
			Msg("[outbox] timer exceeded max_wait, marked skipped and dispatching skip task")

		if o.execDispatcher != nil {
			o.execDispatcher.DispatchOrLog(&dispatchTask{
				ExecutionID: t.ExecutionID,
				NodeID:      t.NodeID,
				Attempt:     0,
				SkipWait:    true,
				TraceID:     fmt.Sprintf("maxwait-%d", t.ID),
				// 行自证（T027）：跳过等待同样是业务派发，worker 靠它恢复租户
				// ctx；漏填在 on 模式下会让这次唤醒被 fail-closed 吞掉。
				TenantID: timerRowTenant(t),
			})
		}
	}
}

func (o *SOPOutboxDispatcher) bumpTimerClaimOrDeadLetter(ctx context.Context, t *model.SOPTimer, now time.Time) {
	if o.timerRepo == nil || t == nil {
		return
	}
	claims := timerClaimCount(t) + 1
	payload := model.JSONMap{}
	for k, v := range t.Payload {
		payload[k] = v
	}
	payload["claim_count"] = claims

	var rows int64
	var err error
	if claims >= sopTimerMaxClaims {
		rows, err = o.timerRepo.BumpClaimCountAndDeadLetter(ctx, t.ID, claims, payload, now)
	} else {
		rows, err = o.timerRepo.BumpClaimCount(ctx, t.ID, claims, payload)
	}
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Uint("timer_id", t.ID).
			Msg("[outbox] bump claim count failed")
		return
	}
	if rows == 0 {
		return
	}
	if claims >= sopTimerMaxClaims {
		logger.Ctx(ctx).Error().
			Uint("timer_id", t.ID).
			Uint("execution_id", t.ExecutionID).
			Str("node_id", t.NodeID).
			Int("claim_count", claims).
			Msg("[outbox][ALERT] timer moved to dead_letter: claim_count exhausted")
	} else {
		logger.Ctx(ctx).Warn().
			Uint("timer_id", t.ID).
			Int("claim_count", claims).
			Msg("[outbox] timer claim failed, claim_count incremented")
	}
}

func (o *SOPOutboxDispatcher) writeTimerSkippedEvent(ctx context.Context, t *model.SOPTimer, now time.Time) {
	if o.eventRepo == nil || o.timerRepo == nil {
		return
	}
	execRow, err := o.timerRepo.GetExecutionSummary(ctx, t.ExecutionID)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Uint("execution_id", t.ExecutionID).
			Msg("[outbox] write skipped event aborted: execution not found")
		return
	}
	event := &model.SOPExecEvent{
		ExecutionID:  execRow.ID,
		SOPID:        execRow.SOPID,
		NodeID:       t.NodeID,
		NodeType:     SOPNodeTypeWait,
		EventType:    NodeEventSkipped,
		Status:       NodeStatusSkipped,
		ErrorMessage: fmt.Sprintf("wait exceeded max_wait_at=%s, treated as satisfied and skipped", timerMaxWaitAt(t).Format(time.RFC3339)),
	}
	if err := o.eventRepo.Create(ctx, event); err != nil {
		logger.Ctx(ctx).Debug().Err(err).
			Uint("execution_id", execRow.ID).
			Str("node_id", t.NodeID).
			Msg("[outbox] write skipped event failed")
	}
}

// timerRowTenant 取 timer 行归属租户（T027 行自证），0 视为未填回落默认租户。
//
// 为什么必须回填进 dispatchTask.TenantID：队列 worker 只认 task.TenantID 来
// 恢复租户上下文（sop_dispatcher.worker），漏填 ⇒ on 模式下 loadExecution 被
// 隔离 callback fail-closed 吞掉，唤醒任务永远推不动。
func timerRowTenant(t *model.SOPTimer) uint {
	if t != nil && t.TenantID != 0 {
		return t.TenantID
	}
	return tenant.DefaultTenantID
}

func timerClaimCount(t *model.SOPTimer) int {
	if t != nil && t.ClaimCount > 0 {
		return t.ClaimCount
	}
	switch v := t.Payload["claim_count"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func timerMaxWaitAt(t *model.SOPTimer) time.Time {
	if t == nil {
		return time.Time{}
	}
	if t.MaxWaitAt != nil && !t.MaxWaitAt.IsZero() {
		return *t.MaxWaitAt
	}
	return parseSOPTimePayload(t.Payload, "max_wait_at")
}

func parseSOPTimePayload(payload model.JSONMap, key string) time.Time {
	s, _ := payload[key].(string)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// SOPStuckDetector 卡死执行检测器
//
// 独立 goroutine 周期扫描（默认 60s），检测卡死的 Execution：
//   - status='running' AND started_at < now()-24h（执行超时）
//   - 最近 30min 无 sop_exec_events（节点级卡死）
//   - 且无 pending timer（wait 节点不算卡死）
//
// 恢复策略：
//   - 找到当前节点重新派发任务（attempt=0）
//   - 若仍失败，标记 Execution 为 failed
//
// 去重保障：recentlyRecovered 记录最近恢复过的 execution ID，
// 在 recoveredCooldown 期间内跳过重复恢复，避免同一执行被多次恢复。
type SOPStuckDetector struct {
	execRepo       *repository.SopExecutionRepository
	timerRepo      *repository.SOPTimerRepository
	eventRepo      *repository.SOPExecEventRepository
	execDispatcher SOPDispatchSender
	maxIdleTime    time.Duration
	maxExecTime    time.Duration
	tickInterval   time.Duration
	stopCh         chan struct{}
	wg             sync.WaitGroup
	runMu          sync.Mutex
	running        bool

	recentlyRecovered map[uint]time.Time
	recoveredMu       sync.RWMutex
	recoveredCooldown time.Duration

	// recoveryAttempts 每执行累计恢复次数（内容审计 2026-10-28 §5-②：原实现
	// 只有重投无上限，同卡死点理论可无限 5min 一轮）。达 maxRecoverAttempts
	// 后不再重投，判 failed 收口（注释与代码自此一致）。
	recoveryAttempts   map[uint]int
	maxRecoverAttempts int
}

// NewSOPStuckDetector 创建卡死检测器
//
// 句柄由 repository 自取全局库。
func NewSOPStuckDetector(execDispatcher SOPDispatchSender) *SOPStuckDetector {
	return &SOPStuckDetector{
		execRepo:           repository.NewSopExecutionRepositoryDefault(),
		timerRepo:          repository.NewSOPTimerRepositoryDefault(),
		eventRepo:          repository.NewSOPExecEventRepositoryDefault(),
		execDispatcher:     execDispatcher,
		maxIdleTime:        30 * time.Minute,
		maxExecTime:        24 * time.Hour,
		tickInterval:       60 * time.Second,
		recentlyRecovered:  make(map[uint]time.Time),
		recoveryAttempts:   make(map[uint]int),
		maxRecoverAttempts: 3,
		recoveredCooldown:  5 * time.Minute,
		stopCh:             make(chan struct{}),
	}
}

// SetTickInterval 设置扫描周期（用于测试）
func (d *SOPStuckDetector) SetTickInterval(ctx context.Context, interval time.Duration) {
	d.tickInterval = interval
}

// SetMaxIdleTime 设置节点级卡死阈值（用于测试）
func (d *SOPStuckDetector) SetMaxIdleTime(ctx context.Context, t time.Duration) {
	d.maxIdleTime = t
}

// Start 启动卡死检测器
func (d *SOPStuckDetector) Start(ctx context.Context) {
	d.runMu.Lock()
	defer d.runMu.Unlock()
	if d.running {
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})
	d.wg.Add(1)
	go d.loop(context.Background())
	logger.GetLogger().Info().
		Dur("tick_interval", d.tickInterval).
		Dur("max_idle_time", d.maxIdleTime).
		Dur("max_exec_time", d.maxExecTime).
		Msg("[SOPStuckDetector] started")
}

// Stop 停止卡死检测器
func (d *SOPStuckDetector) Stop(ctx context.Context) {
	d.runMu.Lock()
	if !d.running {
		d.runMu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	d.runMu.Unlock()
	d.wg.Wait()
	logger.GetLogger().Info().Msg("[SOPStuckDetector] stopped")
}

func (d *SOPStuckDetector) loop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(d.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.scanStuckExecutions(ctx)
			d.cleanupRecovered()
		}
	}
}

func (d *SOPStuckDetector) cleanupRecovered() {
	d.recoveredMu.Lock()
	defer d.recoveredMu.Unlock()
	threshold := time.Now().Add(-d.recoveredCooldown)
	for id, recoveredAt := range d.recentlyRecovered {
		if recoveredAt.Before(threshold) {
			delete(d.recentlyRecovered, id)
			delete(d.recoveryAttempts, id) // 计数随冷却一起失效（执行恢复推进后重新起算）
		}
	}
}

func (d *SOPStuckDetector) scanStuckExecutions(ctx context.Context) {
	if d.execRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, utils.DefaultHTTPTimeout)
	defer cancel()
	ctx = logger.WithModule(ctx, "sop_stuck_detector")

	// 后台任务租户扇出（specs/001-saas-multi-tenant T020）。
	//
	// 选 Fanout 而不是 WithSystemScope 的理由：FindStuck 后半段是**业务动作** ——
	// 判定卡死后 DispatchOrLog 会重新执行当前节点，写 sop_executions /
	// sop_exec_events，还可能发消息。system scope 只豁免查询，派发链下游的 Create
	// 会落列默认值 1，把租户 N 的恢复写到默认租户（串租户）。Fanout 逐活跃租户注入
	// ctx ⇒ 查询自动带 WHERE tenant_id=N、派发也带着租户上下文；off 模式退化为单
	// 默认租户，等价既有行为。与 sop_scheduler.tick / churn_score_job 同一形态。
	// 行上 sop_executions.tenant_id（T027 行自证）在循环里再兜一层。
	if _, err := tenant.Fanout(ctx, nil, func(tctx context.Context, _ uint) (int64, error) {
		d.scanStuckExecutionsForTenant(tctx)
		return 0, nil
	}); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[stuck] 租户扇出执行失败")
	}
}

// scanStuckExecutionsForTenant 单租户视角的卡死扫描（由 Fanout 调用，
// 入口 ctx 必定已绑定租户；off 模式绑定默认租户，回调直通）。
func (d *SOPStuckDetector) scanStuckExecutionsForTenant(ctx context.Context) {
	now := time.Now()
	idleThreshold := now.Add(-d.maxIdleTime)
	execThreshold := now.Add(-d.maxExecTime)

	execs, err := d.execRepo.FindStuck(ctx, SOPStatusRunning, execThreshold, idleThreshold, 50)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[stuck] query stuck executions failed")
		return
	}

	if len(execs) == 0 {
		return
	}

	recoveredCount := 0
	for _, exec := range execs {
		// 行自证（specs/001-saas-multi-tenant T027）：sop_executions 行自带
		// tenant_id，后续计数与派发按行租户恢复上下文。Fanout 已注入的 ctx 与
		// 行租户必然一致，这里再取一次是双保险——对不上时也只会变成"计数 0 行/
		// 派发后 load 不到行"，绝不跨租户读写。off 模式行值恒为默认租户。
		rowTenant := tenant.DefaultTenantID
		if exec.TenantID != 0 {
			rowTenant = exec.TenantID
		}
		execCtx := tenant.WithTenantID(ctx, rowTenant)

		d.recoveredMu.Lock()
		recoveredAt, found := d.recentlyRecovered[exec.ID]
		if found && time.Since(recoveredAt) < d.recoveredCooldown {
			d.recoveredMu.Unlock()
			logger.Ctx(execCtx).Debug().
				Uint("execution_id", exec.ID).
				Time("recovered_at", recoveredAt).
				Msg("[stuck] skip recently recovered execution")
			continue
		}

		d.recentlyRecovered[exec.ID] = time.Now()
		d.recoveredMu.Unlock()

		pendingTimerCount, err := d.timerRepo.CountPendingByExecutionID(execCtx, exec.ID)
		if err != nil {
			logger.Ctx(execCtx).Warn().Err(err).
				Uint("execution_id", exec.ID).
				Msg("[stuck] count pending timers failed, skip")
			continue
		}
		if pendingTimerCount > 0 {
			continue
		}

		recentEventCount, err := d.eventRepo.CountRecentByExecutionID(execCtx, exec.ID, idleThreshold)
		if err != nil {
			logger.Ctx(execCtx).Warn().Err(err).
				Uint("execution_id", exec.ID).
				Msg("[stuck] count recent events failed, skip")
			continue
		}
		if recentEventCount > 0 {
			continue
		}

		logger.Ctx(execCtx).Warn().
			Uint("execution_id", exec.ID).
			Str("current_node", exec.CurrentNode).
			Time("started_at", exec.StartedAt).
			Msg("[stuck] detected stuck execution, attempting recovery")

		// 恢复上限（内容审计 2026-10-28 §5-②）：同一执行累计恢复达上限后不再重投，
		// 判 failed 收口——无限 5min 一轮的重投既不解决问题也会一直占用扫描窗口。
		// 计数随冷却清理失效（执行真正推进 >5min 后再卡死 ⇒ 新一轮恢复预算）。
		d.recoveredMu.Lock()
		d.recoveryAttempts[exec.ID]++
		attempts := d.recoveryAttempts[exec.ID]
		d.recoveredMu.Unlock()
		if attempts > d.maxRecoverAttempts {
			logger.Ctx(execCtx).Error().
				Uint("execution_id", exec.ID).
				Int("recovery_attempts", attempts).
				Str("current_node", exec.CurrentNode).
				Msg("[stuck] recovery attempts exhausted, marking execution failed")
			if err := d.execRepo.UpdateFields(execCtx, exec.ID, map[string]any{
				"status":        SOPStatusFailed,
				"error_message": fmt.Sprintf("卡死恢复重投 %d 次仍未推进（当前节点 %s），按失败收口", d.maxRecoverAttempts, exec.CurrentNode),
			}); err != nil {
				logger.Ctx(execCtx).Warn().Err(err).
					Uint("execution_id", exec.ID).
					Msg("[stuck] mark failed after exhausted recoveries failed")
			}
			continue
		}

		if d.execDispatcher != nil && exec.CurrentNode != "" {
			traceID := fmt.Sprintf("stuck-recovery-%d-%d", exec.ID, now.Unix())
			d.execDispatcher.DispatchOrLog(&dispatchTask{
				ExecutionID: exec.ID,
				NodeID:      exec.CurrentNode,
				Attempt:     0,
				TraceID:     traceID,
				// 行自证（T027）：恢复派发是业务动作，队列 worker 只认
				// task.TenantID 恢复租户 ctx；漏填在 on 模式下 load execution
				// 会被隔离 callback fail-closed 吞掉，卡死恢复永远失败。
				TenantID: rowTenant,
			})
			recoveredCount++
		}
	}

	if recoveredCount > 0 {
		logger.Ctx(ctx).Info().
			Int("recovered_count", recoveredCount).
			Int("scanned_count", len(execs)).
			Msg("[stuck] recovered stuck executions")
	}
}

var (
	globalOutboxDispatcher *SOPOutboxDispatcher
	globalStuckDetector    *SOPStuckDetector
	outboxOnce             sync.Once
	stuckOnce              sync.Once
)

// InitSOPOutboxDispatcher 初始化全局 Outbox 调度器
func InitSOPOutboxDispatcher(execDispatcher *SOPExecutionDispatcher) *SOPOutboxDispatcher {
	outboxOnce.Do(func() {
		globalOutboxDispatcher = NewSOPOutboxDispatcher(execDispatcher)
		globalOutboxDispatcher.Start(context.Background())
	})
	return globalOutboxDispatcher
}

// GetSOPOutboxDispatcher 获取全局 Outbox 调度器
func GetSOPOutboxDispatcher() *SOPOutboxDispatcher {
	return globalOutboxDispatcher
}

// InitSOPStuckDetector 初始化全局卡死检测器
func InitSOPStuckDetector(execDispatcher *SOPExecutionDispatcher) *SOPStuckDetector {
	stuckOnce.Do(func() {
		globalStuckDetector = NewSOPStuckDetector(execDispatcher)
		globalStuckDetector.Start(context.Background())
	})
	return globalStuckDetector
}

// GetSOPStuckDetector 获取全局卡死检测器
func GetSOPStuckDetector() *SOPStuckDetector {
	return globalStuckDetector
}

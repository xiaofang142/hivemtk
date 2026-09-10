package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
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
// 构造函数签名保持 db *gorm.DB 不变以兼容调用方，内部用 db 创建 repository。
func NewSOPOutboxDispatcher(db *gorm.DB, execDispatcher SOPDispatchSender) *SOPOutboxDispatcher {
	return &SOPOutboxDispatcher{
		timerRepo:      repository.NewSOPTimerRepository(db),
		eventRepo:      repository.NewSOPExecEventRepository(db),
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

	o.processDueTimers(context.Background())

	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.processDueTimers(context.Background())
		}
	}
}

func (o *SOPOutboxDispatcher) processDueTimers(ctx context.Context) {
	if o.timerRepo == nil || o.execDispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
	defer cancel()
	ctx = logger.WithModule(ctx, "sop_outbox")

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
		rowsAffected, err := o.timerRepo.MarkFired(ctx, t.ID, now)
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).
				Uint("timer_id", t.ID).
				Msg("[outbox] mark timer fired failed")

			o.bumpTimerClaimOrDeadLetter(ctx, &t, now)
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
}

// NewSOPStuckDetector 创建卡死检测器
//
// 构造函数签名保持 db *gorm.DB 不变以兼容调用方，内部用 db 创建 repository。
func NewSOPStuckDetector(db *gorm.DB, execDispatcher SOPDispatchSender) *SOPStuckDetector {
	return &SOPStuckDetector{
		execRepo:          repository.NewSopExecutionRepository(db),
		timerRepo:         repository.NewSOPTimerRepository(db),
		eventRepo:         repository.NewSOPExecEventRepository(db),
		execDispatcher:    execDispatcher,
		maxIdleTime:       30 * time.Minute,
		maxExecTime:       24 * time.Hour,
		tickInterval:      60 * time.Second,
		recentlyRecovered: make(map[uint]time.Time),
		recoveredCooldown: 5 * time.Minute,
		stopCh:            make(chan struct{}),
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
			d.scanStuckExecutions(context.Background())
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
		}
	}
}

func (d *SOPStuckDetector) scanStuckExecutions(ctx context.Context) {
	if d.execRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
	defer cancel()
	ctx = logger.WithModule(ctx, "sop_stuck_detector")

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

		d.recoveredMu.Lock()
		recoveredAt, found := d.recentlyRecovered[exec.ID]
		if found && time.Since(recoveredAt) < d.recoveredCooldown {
			d.recoveredMu.Unlock()
			logger.Ctx(ctx).Debug().
				Uint("execution_id", exec.ID).
				Time("recovered_at", recoveredAt).
				Msg("[stuck] skip recently recovered execution")
			continue
		}

		d.recentlyRecovered[exec.ID] = time.Now()
		d.recoveredMu.Unlock()

		pendingTimerCount, err := d.timerRepo.CountPendingByExecutionID(ctx, exec.ID)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Uint("execution_id", exec.ID).
				Msg("[stuck] count pending timers failed, skip")
			continue
		}
		if pendingTimerCount > 0 {
			continue
		}

		recentEventCount, err := d.eventRepo.CountRecentByExecutionID(ctx, exec.ID, idleThreshold)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Uint("execution_id", exec.ID).
				Msg("[stuck] count recent events failed, skip")
			continue
		}
		if recentEventCount > 0 {
			continue
		}

		logger.Ctx(ctx).Warn().
			Uint("execution_id", exec.ID).
			Str("current_node", exec.CurrentNode).
			Time("started_at", exec.StartedAt).
			Msg("[stuck] detected stuck execution, attempting recovery")

		if d.execDispatcher != nil && exec.CurrentNode != "" {
			traceID := fmt.Sprintf("stuck-recovery-%d-%d", exec.ID, now.Unix())
			d.execDispatcher.DispatchOrLog(&dispatchTask{
				ExecutionID: exec.ID,
				NodeID:      exec.CurrentNode,
				Attempt:     0,
				TraceID:     traceID,
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
func InitSOPOutboxDispatcher(db *gorm.DB, execDispatcher *SOPExecutionDispatcher) *SOPOutboxDispatcher {
	outboxOnce.Do(func() {
		globalOutboxDispatcher = NewSOPOutboxDispatcher(db, execDispatcher)
		globalOutboxDispatcher.Start(context.Background())
	})
	return globalOutboxDispatcher
}

// GetSOPOutboxDispatcher 获取全局 Outbox 调度器
func GetSOPOutboxDispatcher() *SOPOutboxDispatcher {
	return globalOutboxDispatcher
}

// InitSOPStuckDetector 初始化全局卡死检测器
func InitSOPStuckDetector(db *gorm.DB, execDispatcher *SOPExecutionDispatcher) *SOPStuckDetector {
	stuckOnce.Do(func() {
		globalStuckDetector = NewSOPStuckDetector(db, execDispatcher)
		globalStuckDetector.Start(context.Background())
	})
	return globalStuckDetector
}

// GetSOPStuckDetector 获取全局卡死检测器
func GetSOPStuckDetector() *SOPStuckDetector {
	return globalStuckDetector
}

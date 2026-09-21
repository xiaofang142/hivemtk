package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"
)

// SOPDispatcherConfig 调度器配置
type SOPDispatcherConfig struct {
	WorkerCount       int
	QueueCapacity     int
	LLMConcurrency    int
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

// DefaultSOPDispatcherConfig 默认配置
func DefaultSOPDispatcherConfig() *SOPDispatcherConfig {
	return &SOPDispatcherConfig{
		WorkerCount:       16,
		QueueCapacity:     1000,
		LLMConcurrency:    4,
		MaxAttempts:       3,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

type dispatchTask struct {
	ExecutionID uint
	NodeID      string
	Attempt     int
	TraceID     string

	SkipWait bool

	// TimerFired 标记本任务由 wait timer 到期触发（outbox dispatcher 派发）。
	// wait 节点的等待条件已满足，processTask 应视为"等待完成、推进下一节点"，
	// 而不是重新执行 WaitExecutor——否则 waitUntil 被重算、timer 无限再生，
	// 执行永久卡 running（审计R49 修复的 SOP wait 死循环）。
	TimerFired bool

	// WaitEvent / WaitPayload 触发本次点火的那枚定时器自带的信息
	// （仅 TimerFired 任务非空；SkipWait 任务不填，见 sop_outbox_dispatcher 的注释）。
	//
	// 为什么要在这里捎带：定时器是**唯一**知道自己在等什么的东西，而点火之后那一行
	// 状态已经变成 fired，再想读就得按 (execution,node) 反查一遍——多一次读、多一个
	// 读错行的机会（同一节点重跑过就有好几行）。payload 里的恢复凭证由审批等待档写入
	// （T-P3-02），调度器本身不认识它，只负责原样递到回读方手里。
	WaitEvent   string
	WaitPayload model.JSONMap
}

// SOPExecutionDispatcher SOP 执行调度器
//
// 通过 Worker Pool 并发执行 SOP 节点，全局唯一实例。
// 由 SOPService.Execute / Step 派发任务，由 OutboxDispatcher 派发 timer 唤醒任务。
type SOPExecutionDispatcher struct {
	registry    *NodeExecutorRegistry
	execRepo    *repository.SopExecutionRepository
	agentRepo   *repository.SopAgentRepository
	eventRepo   *repository.SOPExecEventRepository
	msgRepo     *repository.SessionMessageRepository
	sessionRepo *repository.CustomerSessionRepository
	sopService  *SOPService

	dispatchQueue chan *dispatchTask
	workerCount   int
	llmSem        chan struct{}

	retryPolicy *SOPRetryPolicy

	wg      sync.WaitGroup
	stopCh  chan struct{}
	runMu   sync.Mutex
	running bool

	workerCancel context.CancelFunc

	retryTimersMu sync.Mutex
	retryTimers   map[*time.Timer]struct{}

	// compensationMu 保护 compensationMgr：装配发生在 InitSOPExecutionDispatcher 之后
	// （那时 Start() 已把 worker 跑起来），而 worker 的失败路径会读这个字段。
	// 无锁写=数据竞争（-race 可复现），故读写都过这里。
	compensationMu  sync.RWMutex
	compensationMgr *CompensationManager
}

// SetCompensationManager 注入 Saga 补偿管理器
//
// 启动期由 service.InitSOPCompensation 调用（见 T-P1-02）；不注入时 failExecution
// 走原路径（仅标记失败，不补偿）。
func (d *SOPExecutionDispatcher) SetCompensationManager(m *CompensationManager) {
	if d == nil {
		return
	}
	d.compensationMu.Lock()
	d.compensationMgr = m
	d.compensationMu.Unlock()
}

// compensationManager 取当前补偿管理器（未装配返回 nil）。
func (d *SOPExecutionDispatcher) compensationManager() *CompensationManager {
	d.compensationMu.RLock()
	defer d.compensationMu.RUnlock()
	return d.compensationMgr
}

func (d *SOPExecutionDispatcher) registerRetryTimer(t *time.Timer) {
	d.retryTimersMu.Lock()
	defer d.retryTimersMu.Unlock()
	if d.retryTimers == nil {
		d.retryTimers = make(map[*time.Timer]struct{})
	}
	d.retryTimers[t] = struct{}{}
}

func (d *SOPExecutionDispatcher) unregisterRetryTimer(t *time.Timer) {
	d.retryTimersMu.Lock()
	defer d.retryTimersMu.Unlock()
	if d.retryTimers != nil {
		delete(d.retryTimers, t)
	}
}

func (d *SOPExecutionDispatcher) stopAllRetryTimers() {
	d.retryTimersMu.Lock()
	defer d.retryTimersMu.Unlock()
	for t := range d.retryTimers {
		t.Stop()
	}
	d.retryTimers = make(map[*time.Timer]struct{})
}

// SOPRetryPolicy SOP 节点执行指数退避重试策略
//
// 注意：reach_pipeline.go 已有同名 RetryPolicy 类型（用于触达 pipeline），
// 本结构体专为 SOP 节点执行器设计，故加 SOP 前缀避免冲突。
type SOPRetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

// DefaultSOPRetryPolicy 默认 SOP 重试策略
func DefaultSOPRetryPolicy() *SOPRetryPolicy {
	return &SOPRetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
	}
}

// Backoff 计算第 attempt 次重试的退避时间（attempt 从 1 开始）
//
// 标准指数退避：Backoff(N) = InitialBackoff * Multiplier^(N-1)，被 MaxBackoff 封顶。
//   - attempt=1 → InitialBackoff
//   - attempt=2 → InitialBackoff * Multiplier
//   - attempt=3 → InitialBackoff * Multiplier^2
//   - attempt<=0 视为 1
func (p *SOPRetryPolicy) Backoff(ctx context.Context, attempt int) time.Duration {
	if attempt <= 1 {
		return p.InitialBackoff
	}
	d := p.InitialBackoff
	for i := 1; i < attempt; i++ {
		d = time.Duration(float64(d) * p.Multiplier)
		if d >= p.MaxBackoff {
			return p.MaxBackoff
		}
	}
	if d > p.MaxBackoff {
		return p.MaxBackoff
	}
	return d
}

// NewSOPExecutionDispatcher 创建调度器
//
// 不会自动启动 Worker，需调用 Start 启动。
func NewSOPExecutionDispatcher(db *gorm.DB, sopSvc *SOPService, registry *NodeExecutorRegistry, cfg *SOPDispatcherConfig) *SOPExecutionDispatcher {
	if cfg == nil {
		cfg = DefaultSOPDispatcherConfig()
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 16
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 1000
	}
	if cfg.LLMConcurrency <= 0 {
		cfg.LLMConcurrency = 4
	}

	d := &SOPExecutionDispatcher{
		registry:      registry,
		execRepo:      repository.NewSopExecutionRepository(db),
		agentRepo:     repository.NewSopAgentRepository(db),
		eventRepo:     repository.NewSOPExecEventRepository(db),
		msgRepo:       repository.NewSessionMessageRepository(),
		sessionRepo:   repository.NewCustomerSessionRepository(),
		sopService:    sopSvc,
		dispatchQueue: make(chan *dispatchTask, cfg.QueueCapacity),
		workerCount:   cfg.WorkerCount,
		llmSem:        make(chan struct{}, cfg.LLMConcurrency),
		retryPolicy: &SOPRetryPolicy{
			MaxAttempts:    cfg.MaxAttempts,
			InitialBackoff: cfg.InitialBackoff,
			MaxBackoff:     cfg.MaxBackoff,
			Multiplier:     cfg.BackoffMultiplier,
		},
		stopCh: make(chan struct{}),
	}

	deps := &SOPNodeExecutorDeps{
		DB:          db,
		WSHub:       nil,
		MsgRepo:     d.msgRepo,
		SessionRepo: d.sessionRepo,
		LLMSem:      d.llmSem,
	}
	if sopSvc != nil {
		deps.Dispatcher = sopSvc.dispatcher
	}
	RegisterAllNodeExecutors(registry, deps)

	return d
}

func (d *SOPExecutionDispatcher) SetWSHub(ctx context.Context, hub *websocket.Hub) {
	if d == nil || hub == nil {
		return
	}
	d.replaceMessageExecutorHub(ctx, hub)
}

func (d *SOPExecutionDispatcher) replaceMessageExecutorHub(ctx context.Context, hub *websocket.Hub) {
	if d == nil || d.registry == nil {
		return
	}
	for _, exec := range d.registry.AllExecutors(ctx) {
		if mb, ok := exec.(*MessageNodeBase); ok {
			mb.SetWSHub(ctx, hub)
		}
	}
}

// Start 启动 Worker Pool
func (d *SOPExecutionDispatcher) Start(ctx context.Context) {
	d.runMu.Lock()
	defer d.runMu.Unlock()
	if d.running {
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})

	workerCtx, workerCancel := context.WithCancel(ctx)
	d.workerCancel = workerCancel

	for i := 0; i < d.workerCount; i++ {
		d.wg.Add(1)
		go d.worker(workerCtx, i)
	}
	logger.GetLogger().Info().
		Int("worker_count", d.workerCount).
		Int("queue_capacity", cap(d.dispatchQueue)).
		Int("llm_concurrency", cap(d.llmSem)).
		Msg("[SOPExecutionDispatcher] started")
}

// Stop 停止 Worker Pool（等待所有任务完成）
func (d *SOPExecutionDispatcher) Stop(ctx context.Context) {
	d.runMu.Lock()
	if !d.running {
		d.runMu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	if d.workerCancel != nil {
		d.workerCancel()
	}
	d.runMu.Unlock()

	d.stopAllRetryTimers()

	d.wg.Wait()
	logger.GetLogger().Info().Msg("[SOPExecutionDispatcher] stopped")
}

// Dispatch 派发任务到调度队列
//
// 队列满时返回错误（背压），调用方应处理（如重试或记录日志）。
// 停止信号（stopCh 关闭）优先于入队，确保停止语义明确。
func (d *SOPExecutionDispatcher) Dispatch(ctx context.Context, task *dispatchTask) error {
	select {
	case <-d.stopCh:
		return fmt.Errorf("dispatcher stopped")
	default:
	}
	select {
	case d.dispatchQueue <- task:
		return nil
	case <-d.stopCh:
		return fmt.Errorf("dispatcher stopped")
	default:
		return fmt.Errorf("dispatch queue full (capacity=%d)", cap(d.dispatchQueue))
	}
}

// DispatchOrLog 派发任务，失败时记录日志（不阻塞调用方）
func (d *SOPExecutionDispatcher) DispatchOrLog(task *dispatchTask) {
	if err := d.Dispatch(context.Background(), task); err != nil {
		logger.GetLogger().Error().Err(err).
			Uint("execution_id", task.ExecutionID).
			Str("node_id", task.NodeID).
			Msg("dispatch task failed")
	}
}

func (d *SOPExecutionDispatcher) worker(ctx context.Context, id int) {
	defer d.wg.Done()
	logger.GetLogger().Debug().Int("worker_id", id).Msg("[worker] started")
	for {
		select {
		case <-d.stopCh:
			logger.GetLogger().Debug().Int("worker_id", id).Msg("[worker] stopped")
			return
		case <-ctx.Done():
			logger.GetLogger().Debug().Int("worker_id", id).Msg("[worker] ctx cancelled, stopped")
			return
		case task := <-d.dispatchQueue:

			taskCtx, cancel := context.WithTimeout(ctx, utils.CronShortTimeout)
			d.processTask(taskCtx, id, task)
			cancel()
		}
	}
}

func (d *SOPExecutionDispatcher) processTask(ctx context.Context, workerID int, task *dispatchTask) {
	ctx = logger.WithTraceID(ctx, task.TraceID)
	ctx = logger.WithModule(ctx, "sop_dispatcher")

	start := time.Now()
	logger.Ctx(ctx).Info().
		Int("worker_id", workerID).
		Uint("execution_id", task.ExecutionID).
		Str("node_id", task.NodeID).
		Int("attempt", task.Attempt).
		Msg("[worker] processing task")

	exec, err := d.loadExecution(ctx, task.ExecutionID)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Uint("execution_id", task.ExecutionID).
			Msg("[worker] load execution failed")
		return
	}
	if exec.Status != SOPStatusRunning {
		logger.Ctx(ctx).Info().
			Str("status", exec.Status).
			Msg("[worker] execution not running, skip")
		return
	}

	graph, err := d.loadGraph(ctx, exec)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[worker] load sop graph failed")
		d.handleExecutionError(ctx, exec, err, task)
		return
	}
	node := findNodeByID(graph, task.NodeID)
	if node == nil {
		logger.Ctx(ctx).Error().Str("node_id", task.NodeID).Msg("[worker] node not found in graph")
		d.handleExecutionError(ctx, exec, fmt.Errorf("node not found: %s", task.NodeID), task)
		return
	}

	entryPolicy := DefaultSOPEntryPolicy()
	if d.sopService != nil {
		if agent, aerr := d.sopService.Get(ctx, exec.SOPID); aerr == nil && agent != nil {
			entryPolicy = ParseSOPEntryPolicy(agent.TriggerConfig)
		}
	}

	latencyMs := time.Since(start).Milliseconds()

	var result *NodeExecResult
	if (task.SkipWait || task.TimerFired) && node.Type == SOPNodeTypeWait {
		// SkipWait：max_wait 超期，视为满足；TimerFired：timer 正常到期，等待条件已满足。
		// 两者都直接推进下一节点，不得重新执行 WaitExecutor（重入会重算 waitUntil
		// 并再建 timer，形成死循环）。
		if task.SkipWait {
			logger.Ctx(ctx).Info().
				Str("node_id", node.ID).
				Msg("[worker] skip wait node (max_wait exceeded, treated as satisfied)")
		} else {
			logger.Ctx(ctx).Info().
				Str("node_id", node.ID).
				Msg("[worker] wait node satisfied (timer fired), advance to next")
		}
		result = &NodeExecResult{Status: NodeStatusSkipped}
		// 审批等待档（T-P3-02）：等待条件是"有人裁决了"，而这件事只有库里那条审批记录知道。
		// 点火这一刻才回读，把结论并进 Output —— handleNodeSuccess 会先把它写进
		// ExecutionData 再算下一跳，于是后面的 condition 节点能按结论分支。
		// 非审批等待返回 nil，这一行等价于改动前。
		result.Output = GetApprovalResumeBridge().ResolveOnFire(ctx, task)
	} else {
		d.writeExecEvent(ctx, exec, node, NodeEventStarted, task.Attempt, nil, nil, "")

		// 审批定时器点火后重入的**非 wait** 节点（T-P5-03 的 reach_send）：结论必须在这里
		// 递回执行器手里。不递的话它读不到裁决，只会再入队一次审批 —— 待办中心被同一件事
		// 刷屏，而那条外发永远发不出去。
		// 非审批等待（timer / customer_reply）回读返回 nil ⇒ 这一格恒空 ⇒ 对既有节点逐字等价于改动前。
		var approvalOutcome model.JSONMap
		if task.TimerFired && task.WaitEvent == WaitEventApproval {
			approvalOutcome = GetApprovalResumeBridge().ResolveOnFire(ctx, task)
		}

		execCtx := &ExecutionContext{
			Execution:       exec,
			Node:            node,
			Graph:           graph,
			CustomerID:      exec.CustomerID,
			SessionID:       exec.SessionID,
			Variant:         exec.Variant,
			Input:           exec.ExecutionData,
			ExecutionData:   exec.ExecutionData,
			TraceID:         task.TraceID,
			StartedAt:       start,
			Attempt:         task.Attempt,
			ApprovalOutcome: approvalOutcome,
		}

		executor := d.registry.MustGet(ctx, node.Type)
		var err error
		result, err = executor.Execute(ctx, execCtx)
		if err != nil || result == nil {

			d.handleNodeFailure(ctx, exec, node, task, err, true, latencyMs)
			return
		}
	}

	d.writeExecEvent(ctx, exec, node, NodeEventExecuted, task.Attempt, result.Output, result.SideEffects, "")

	switch result.Status {
	case NodeStatusCompleted, NodeStatusSkipped:

		appendExecutedNode(exec, node, task.Attempt, "")
		d.handleNodeSuccess(ctx, exec, node, graph, result, task, entryPolicy, latencyMs)
	case NodeStatusWaiting:
		d.handleNodeWaiting(ctx, exec, node, result, latencyMs)
	case NodeStatusFailed:
		err := fmt.Errorf("%s", result.ErrorMessage)
		d.handleNodeFailure(ctx, exec, node, task, err, result.Retryable, latencyMs)
	default:
		logger.Ctx(ctx).Warn().
			Str("status", result.Status).
			Msg("[worker] unknown node status, treating as completed")
		appendExecutedNode(exec, node, task.Attempt, "")
		d.handleNodeSuccess(ctx, exec, node, graph, result, task, entryPolicy, latencyMs)
	}
}

func (d *SOPExecutionDispatcher) loadExecution(ctx context.Context, execID uint) (*model.SOPExecution, error) {
	return d.execRepo.GetByID(ctx, execID)
}

func (d *SOPExecutionDispatcher) loadGraph(ctx context.Context, exec *model.SOPExecution) (*dto.SOPGraph, error) {
	if d.sopService == nil {
		return nil, fmt.Errorf("sop service not configured")
	}
	agent, err := d.sopService.Get(ctx, exec.SOPID)
	if err != nil {
		return nil, err
	}

	var variantGraphID uint
	if exec.Variant != "" {
		cfg := ParseSOPABTestConfig(agent.ABTestConfig)
		if cfg.Enabled {
			for _, v := range cfg.Variants {
				if v.Name == exec.Variant {
					variantGraphID = v.SOPGraphID
					break
				}
			}
		}
	}
	graph, err := d.sopService.loadSOPGraph(ctx, agent, variantGraphID)
	if err != nil {
		return nil, err
	}
	return &graph, nil
}

func (d *SOPExecutionDispatcher) handleNodeSuccess(ctx context.Context, exec *model.SOPExecution, node *dto.SOPNode, graph *dto.SOPGraph, result *NodeExecResult, task *dispatchTask, policy SOPEntryPolicy, latencyMs int64) {
	if exec.ExecutionData == nil {
		exec.ExecutionData = model.JSONMap{}
	}
	for k, v := range result.Output {
		exec.ExecutionData[k] = v
	}
	for _, effect := range result.SideEffects {
		exec.ExecutionData = appendSideEffect(exec.ExecutionData, effect)
	}

	if policy.GoalExit != "" && goalExitAchieved(policy.GoalExit, exec.ExecutionData) {
		d.writeExecEvent(ctx, exec, node, NodeEventGoalAchieved, task.Attempt, result.Output, result.SideEffects, "")
		logger.Ctx(ctx).Info().
			Uint("execution_id", exec.ID).
			Str("goal_exit", policy.GoalExit).
			Msg("[worker] goal_exit achieved, completing execution early")
		d.completeExecution(ctx, exec)
		return
	}

	nextNodeID := result.NextNodeID
	if nextNodeID == "" {
		nextNode := nextNode(graph, node, exec.ExecutionData)
		if nextNode == nil {
			d.completeExecution(ctx, exec)
			return
		}
		nextNodeID = nextNode.ID
	} else if nextNodeID == "_end_" {
		d.completeExecution(ctx, exec)
		return
	}

	now := time.Now()
	exec.CurrentNode = nextNodeID
	exec.LastEventAt = &now
	exec.AttemptCount = 0
	exec.WaitEvent = ""
	for i, n := range graph.Nodes {
		if n.ID == nextNodeID {
			exec.CurrentNodeIdx = i
			break
		}
	}
	if err := d.execRepo.Save(ctx, exec); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[worker] save execution failed")
		return
	}

	d.writeExecEvent(ctx, exec, node, NodeEventCompleted, task.Attempt, result.Output, result.SideEffects, "")

	d.DispatchOrLog(&dispatchTask{
		ExecutionID: exec.ID,
		NodeID:      nextNodeID,
		Attempt:     0,
		TraceID:     task.TraceID,
	})

	logger.Ctx(ctx).Info().
		Str("node_id", node.ID).
		Str("next_node_id", nextNodeID).
		Int64("latency_ms", latencyMs).
		Msg("[worker] node completed, dispatched next")
}

func (d *SOPExecutionDispatcher) handleNodeWaiting(ctx context.Context, exec *model.SOPExecution, node *dto.SOPNode, result *NodeExecResult, latencyMs int64) {
	now := time.Now()
	exec.LastEventAt = &now
	exec.WaitEvent = result.WaitEvent
	exec.AttemptCount = 0
	if err := d.execRepo.Save(ctx, exec); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[worker] save execution (waiting) failed")
		return
	}

	d.writeExecEvent(ctx, exec, node, NodeEventWaiting, 0, result.Output, nil, "")

	logger.Ctx(ctx).Info().
		Str("node_id", node.ID).
		Str("wait_event", result.WaitEvent).
		Int64("latency_ms", latencyMs).
		Msg("[worker] node waiting")
}

func (d *SOPExecutionDispatcher) handleNodeFailure(ctx context.Context, exec *model.SOPExecution, node *dto.SOPNode, task *dispatchTask, err error, retryable bool, latencyMs int64) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	d.writeExecEvent(ctx, exec, node, NodeEventFailed, task.Attempt, nil, nil, errMsg)

	if task.Attempt+1 < d.retryPolicy.MaxAttempts {
		backoff := d.retryPolicy.Backoff(ctx, task.Attempt+1)
		logger.Ctx(ctx).Warn().
			Str("node_id", node.ID).
			Int("attempt", task.Attempt).
			Int("next_attempt", task.Attempt+1).
			Dur("backoff", backoff).
			Err(err).
			Msg("[worker] node failed, will retry")

		exec.AttemptCount = task.Attempt + 1
		if err := d.execRepo.UpdateAttemptCount(ctx, exec.ID, exec.AttemptCount); err != nil {
			logger.Ctx(ctx).Warn().
				Uint("exec_id", exec.ID).
				Int("attempt_count", exec.AttemptCount).
				Err(err).
				Msg("[worker] update attempt_count failed")
		}

		d.writeExecEvent(ctx, exec, node, NodeEventRetried, task.Attempt+1, nil, nil, errMsg)

		retryTimer := time.NewTimer(backoff)
		d.registerRetryTimer(retryTimer)

		utils.SafeGo(ctx, "sop_dispatcher.retry_timer", func(_ context.Context) {
			defer d.unregisterRetryTimer(retryTimer)
			select {
			case <-retryTimer.C:
				d.DispatchOrLog(&dispatchTask{
					ExecutionID: exec.ID,
					NodeID:      node.ID,
					Attempt:     task.Attempt + 1,
					TraceID:     task.TraceID,
				})
			case <-d.stopCh:
				retryTimer.Stop()
				return
			}
		})
		return
	}

	logger.Ctx(ctx).Error().
		Str("node_id", node.ID).
		Int("attempts", task.Attempt+1).
		Err(err).
		Msg("[worker] node failed after max attempts, marking execution as failed")

	errClass := SOPErrorClassTransient
	if !retryable {
		errClass = SOPErrorClassPermanent
	}
	appendExecutedNodeWithStatus(exec, node, task.Attempt, "failed", errMsg, errClass)

	d.failExecution(ctx, exec, fmt.Sprintf("node %s failed after %d attempts: %s", node.ID, task.Attempt+1, errMsg))
}

func (d *SOPExecutionDispatcher) handleExecutionError(ctx context.Context, exec *model.SOPExecution, err error, task *dispatchTask) {
	d.failExecution(ctx, exec, fmt.Sprintf("execution error: %v", err))
}

func (d *SOPExecutionDispatcher) completeExecution(ctx context.Context, exec *model.SOPExecution) {
	now := time.Now()
	exec.Status = SOPStatusSuccess
	exec.CompletedAt = &now
	exec.LastEventAt = &now
	exec.WaitEvent = ""
	if err := d.execRepo.Save(ctx, exec); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[worker] mark execution success failed")
		return
	}
	_ = d.agentRepo.IncrementSuccessCount(ctx, exec.SOPID)

	logger.Ctx(ctx).Info().
		Uint("execution_id", exec.ID).
		Msg("[worker] execution completed successfully")
}

func (d *SOPExecutionDispatcher) failExecution(ctx context.Context, exec *model.SOPExecution, errMsg string) {
	now := time.Now()
	exec.Status = SOPStatusFailed
	exec.CompletedAt = &now
	exec.LastEventAt = &now
	exec.ErrorMessage = errMsg
	exec.WaitEvent = ""
	_ = d.execRepo.Save(ctx, exec)
	logger.Ctx(ctx).Error().
		Uint("execution_id", exec.ID).
		Str("error", errMsg).
		Msg("[worker] execution marked as failed")

	d.tryCompensate(ctx, exec)
}

const maxExecutedNodeTrace = 200

func appendExecutedNode(exec *model.SOPExecution, node *dto.SOPNode, attempt int, errMsg string) {
	appendExecutedNodeWithStatus(exec, node, attempt, "completed", errMsg, "")
}

func appendExecutedNodeWithStatus(exec *model.SOPExecution, node *dto.SOPNode, attempt int, status, errMsg, errClass string) {
	if exec == nil || node == nil {
		return
	}
	if len(exec.ExecutedNodes) >= maxExecutedNodeTrace {
		return
	}
	rec := map[string]any{
		"node_id":   node.ID,
		"node_type": node.Type,
		"status":    status,
		"attempt":   attempt,
	}
	if errMsg != "" {
		rec["error"] = errMsg
	}
	if errClass != "" {
		rec["error_class"] = errClass
	}
	exec.ExecutedNodes = append(exec.ExecutedNodes, rec)
}

func (d *SOPExecutionDispatcher) tryCompensate(_ context.Context, exec *model.SOPExecution) {
	if d == nil {
		return
	}
	mgr := d.compensationManager() // 快照一次：整条补偿用同一个管理器，避免装配与执行交错
	if mgr == nil {
		return
	}
	if exec == nil || exec.ID == 0 {
		return
	}

	utils.SafeGo(context.Background(), "sop_dispatcher.compensate", func(ctx context.Context) {
		bgCtx, cancel := context.WithTimeout(context.Background(), utils.CronShortTimeout)
		defer cancel()

		var executed []compensationTraceEntry
		fresh, err := d.execRepo.GetByID(bgCtx, exec.ID)
		if err == nil && fresh != nil && len(fresh.ExecutedNodes) > 0 {
			raw, _ := json.Marshal(fresh.ExecutedNodes)
			_ = json.Unmarshal(raw, &executed)
		} else if err == nil && fresh != nil && len(fresh.ExecutedNodes) == 0 && len(exec.ExecutedNodes) > 0 {
			raw2, _ := json.Marshal(exec.ExecutedNodes)
			_ = json.Unmarshal(raw2, &executed)
		}

		planRecords := make([]CompensationRecord, 0, len(executed))
		for _, e := range executed {

			if e.Status == "failed" {
				continue
			}
			planRecords = append(planRecords, CompensationRecord{
				NodeID:   e.NodeID,
				NodeType: e.NodeType,
				Status:   e.Status,
				Attempt:  e.Attempt,
			})
		}

		plan := mgr.Plan(planRecords)
		if len(plan) == 0 {
			logger.GetLogger().Debug().
				Uint("execution_id", exec.ID).
				Msg("[SOP] no executed nodes to compensate")
			return
		}

		graph, err := d.loadGraph(bgCtx, exec)
		if err != nil {
			logger.GetLogger().Warn().
				Uint("execution_id", exec.ID).
				Err(err).
				Msg("[SOP] compensation aborted: graph load failed")
			return
		}
		nodeByID := make(map[string]*dto.SOPNode, len(graph.Nodes))
		for i := range graph.Nodes {
			nodeByID[graph.Nodes[i].ID] = &graph.Nodes[i]
		}

		result := mgr.Run(bgCtx, exec.ID, plan,
			func(nodeType string) NodeExecutor {
				return d.registry.MustGet(bgCtx, nodeType)
			},
			func(nodeID string) *ExecutionContext {
				n := nodeByID[nodeID]
				if n == nil {
					return nil
				}
				return &ExecutionContext{
					Execution:     exec,
					Node:          n,
					Graph:         graph,
					CustomerID:    exec.CustomerID,
					SessionID:     exec.SessionID,
					Variant:       exec.Variant,
					Input:         exec.ExecutionData,
					ExecutionData: exec.ExecutionData,
					TraceID:       exec.TraceID,
					StartedAt:     time.Now(),
					Attempt:       0,
				}
			},
		)
		logger.GetLogger().Info().
			Uint("execution_id", exec.ID).
			Int("planned", len(plan)).
			Str("status", result.Status).
			Msg("[SOP] SAGA compensation finished")
	})
}

type compensationTraceEntry struct {
	NodeID     string `json:"node_id"`
	NodeType   string `json:"node_type"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	Error      string `json:"error,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
}

func (d *SOPExecutionDispatcher) writeExecEvent(ctx context.Context, exec *model.SOPExecution, node *dto.SOPNode, eventType string, attempt int, output model.JSONMap, sideEffects []string, errMsg string) {
	if d.eventRepo == nil {
		return
	}
	event := &model.SOPExecEvent{
		ExecutionID:  exec.ID,
		SOPID:        exec.SOPID,
		NodeID:       node.ID,
		NodeType:     node.Type,
		EventType:    eventType,
		Attempt:      attempt,
		Status:       eventType,
		Input:        exec.ExecutionData,
		Output:       output,
		SideEffects:  sopToJSONArray(sideEffects),
		ErrorMessage: errMsg,
		TraceID:      tracing.TraceIDFromContext(ctx),
	}
	if err := d.eventRepo.Create(ctx, event); err != nil {
		logger.Ctx(ctx).Debug().Err(err).
			Str("node_id", node.ID).
			Str("event_type", eventType).
			Msg("[worker] write exec event failed (may be duplicate)")
	}
}

func sopToJSONArray(s []string) model.JSONArray {
	if len(s) == 0 {
		return nil
	}
	out := make(model.JSONArray, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}

var (
	globalSOPDispatcher *SOPExecutionDispatcher
	sopDispatcherOnce   sync.Once
)

// InitSOPExecutionDispatcher 初始化全局调度器
func InitSOPExecutionDispatcher(db *gorm.DB, sopSvc *SOPService, cfg *SOPDispatcherConfig) *SOPExecutionDispatcher {
	sopDispatcherOnce.Do(func() {
		registry := NewNodeExecutorRegistry()
		globalSOPDispatcher = NewSOPExecutionDispatcher(db, sopSvc, registry, cfg)
		globalSOPDispatcher.Start(context.Background())
	})
	return globalSOPDispatcher
}

// GetSOPExecutionDispatcher 获取全局调度器
func GetSOPExecutionDispatcher() *SOPExecutionDispatcher {
	return globalSOPDispatcher
}

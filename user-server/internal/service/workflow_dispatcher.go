package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// WorkflowRetryPolicy 工作流重试策略
type WorkflowRetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

func DefaultWorkflowRetryPolicy() *WorkflowRetryPolicy {
	return &WorkflowRetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
	}
}

func (p *WorkflowRetryPolicy) Backoff(attempt int) time.Duration {
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

// WorkflowDispatcher 工作流执行调度器
type WorkflowDispatcher struct {
	registry     *WorkflowNodeExecutorRegistry
	versionRepo  *repository.WorkflowVersionRepository
	execRepo     *repository.WorkflowExecutionRepository
	nodeExecRepo *repository.WorkflowNodeExecutionRepository
	retryPolicy  *WorkflowRetryPolicy
	stopCh       chan struct{}
	runMu        sync.Mutex
	running      bool
	wg           sync.WaitGroup
}

func NewWorkflowDispatcher(
	versionRepo *repository.WorkflowVersionRepository,
	execRepo *repository.WorkflowExecutionRepository,
	nodeExecRepo *repository.WorkflowNodeExecutionRepository,
	registry *WorkflowNodeExecutorRegistry,
	retryPolicy *WorkflowRetryPolicy,
) *WorkflowDispatcher {
	if retryPolicy == nil {
		retryPolicy = DefaultWorkflowRetryPolicy()
	}
	return &WorkflowDispatcher{
		registry:     registry,
		versionRepo:  versionRepo,
		execRepo:     execRepo,
		nodeExecRepo: nodeExecRepo,
		retryPolicy:  retryPolicy,
		stopCh:       make(chan struct{}),
	}
}

func (d *WorkflowDispatcher) Start(ctx context.Context) {
	d.runMu.Lock()
	defer d.runMu.Unlock()
	if d.running {
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})
	logger.GetLogger().Info().Msg("[WorkflowDispatcher] started")
}

func (d *WorkflowDispatcher) Stop(ctx context.Context) {
	d.runMu.Lock()
	if !d.running {
		d.runMu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	d.runMu.Unlock()

	d.wg.Wait()
	logger.GetLogger().Info().Msg("[WorkflowDispatcher] stopped")
}

// Run 异步触发执行，立即返回
func (d *WorkflowDispatcher) Run(ctx context.Context, executionID uint, traceID string) {
	d.wg.Add(1)

	utils.SafeGo(ctx, "workflow_dispatcher.run", func(ctx context.Context) {
		defer d.wg.Done()
		d.runExecution(ctx, executionID, traceID)
	})
}

func (d *WorkflowDispatcher) runExecution(ctx context.Context, executionID uint, traceID string) {
	ctx = logger.WithTraceID(ctx, traceID)
	ctx = logger.WithModule(ctx, "workflow_dispatcher")

	exec, err := d.execRepo.GetByID(ctx, executionID)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Uint("execution_id", executionID).Msg("load execution failed")
		return
	}
	if exec == nil || exec.Status != model.WorkflowExecRunning {
		logger.Ctx(ctx).Info().Str("status", safeStatus(exec)).Msg("execution not running, skip")
		return
	}

	version, err := d.versionRepo.GetByWorkflowIDAndVersion(ctx, exec.WorkflowID, exec.Version)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("load workflow version failed")
		d.failExecution(ctx, exec, fmt.Sprintf("load version failed: %v", err))
		return
	}

	def, err := ParseWorkflowDefinition(version.Definition)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("parse workflow definition failed")
		d.failExecution(ctx, exec, fmt.Sprintf("parse definition failed: %v", err))
		return
	}

	currentNode := d.findFirstNode(def)
	if currentNode == nil {
		logger.Ctx(ctx).Error().Msg("no start node found in workflow definition")
		d.failExecution(ctx, exec, "no start node found")
		return
	}

	steps := 0

	for {
		steps++
		if steps > maxWorkflowSteps() {
			logger.Ctx(ctx).Error().Int("steps", steps).Msg("workflow step limit exceeded (cycle?)")
			d.failExecution(ctx, exec, "workflow step limit exceeded; check definition for cycles")
			return
		}

		select {
		case <-d.stopCh:
			logger.Ctx(ctx).Info().Msg("dispatcher stopped, terminating execution")
			d.terminateExecution(ctx, exec)
			return
		default:
		}

		exec, err = d.execRepo.GetByID(ctx, executionID)
		if err != nil || exec == nil {
			logger.Ctx(ctx).Error().Err(err).Msg("execution disappeared")
			return
		}
		if exec.Status != model.WorkflowExecRunning {
			logger.Ctx(ctx).Info().Str("status", exec.Status).Msg("execution no longer running, exit loop")
			return
		}

		// FA-WF-01：预算以节点为键——前一个节点耗掉的预算不再压给后一个节点。
		attempt := readAttemptFor(exec, currentNode)
		nextNodeID, completed, retryScheduled := d.executeNode(ctx, exec, def, currentNode, attempt)
		if retryScheduled {
			return
		}
		if completed {
			d.completeExecution(ctx, exec)
			return
		}

		if nextNodeID == "" || nextNodeID == "_end_" {
			d.completeExecution(ctx, exec)
			return
		}

		currentNode = FindWorkflowNode(def, nextNodeID)
		if currentNode == nil {
			logger.Ctx(ctx).Error().Str("next_node_id", nextNodeID).Msg("next node not found in definition")
			d.failExecution(ctx, exec, fmt.Sprintf("next node not found: %s", nextNodeID))
			return
		}
	}
}

func (d *WorkflowDispatcher) findFirstNode(def *WorkflowDefinition) *WorkflowDefNode {
	if def == nil || len(def.Nodes) == 0 {
		return nil
	}
	for i := range def.Nodes {
		if def.Nodes[i].Type == WorkflowNodeTypeTrigger {
			return &def.Nodes[i]
		}
	}
	return &def.Nodes[0]
}

func (d *WorkflowDispatcher) executeNode(
	ctx context.Context,
	exec *model.WorkflowExecution,
	def *WorkflowDefinition,
	node *WorkflowDefNode,
	attempt int,
) (nextNodeID string, completed bool, retryScheduled bool) {

	nodeExec := &model.WorkflowNodeExecution{
		ExecutionID: exec.ID,
		NodeID:      node.ID,
		NodeType:    node.Type,
		NodeName:    node.Name,
		InputData:   exec.Context,
		Status:      model.WorkflowNodeRunning,
		StartedAt:   wfTimePtr(time.Now()),
	}
	if err := d.nodeExecRepo.Create(ctx, nodeExec); err != nil {

		logger.Ctx(ctx).Error().Err(err).Msg("create node execution record failed")
		d.failExecution(ctx, exec, fmt.Sprintf("create node execution failed: %v", err))
		return "", false, true
	}

	start := time.Now()
	if exec.Context == nil {
		exec.Context = model.JSONMap{}
	}
	wctx := &WorkflowExecContext{
		Execution:  exec,
		NodeID:     node.ID,
		NodeType:   node.Type,
		NodeConfig: node.Config,
		Graph:      def,
		Input:      exec.TriggerPayload,
		Context:    exec.Context,
		TraceID:    tracing.TraceIDFromContext(ctx),
		StartedAt:  start,
		Attempt:    attempt,
	}

	executor, execErr := d.registry.Get(ctx, node.Type)
	if execErr != nil {
		// FA-02：未知节点类型不再 noop 冒充完成——节点记失败、执行整体失败。
		// 配置错误重试无意义，不走 handleNodeFailure 的重试排程。
		now2 := time.Now()
		nodeExec.FinishedAt = &now2
		nodeExec.Status = model.WorkflowNodeFailed
		nodeExec.Error = fmt.Sprintf("unknown node type %q: %v", node.Type, execErr)
		_ = d.nodeExecRepo.Update(ctx, nodeExec)
		d.failExecution(ctx, exec, nodeExec.Error)
		return "", false, true
	}
	result, err := executor.Execute(ctx, wctx)
	durationMs := time.Since(start).Milliseconds()

	now := time.Now()
	nodeExec.FinishedAt = &now
	nodeExec.DurationMs = int(durationMs)

	if err != nil || result == nil {
		nodeExec.Status = model.WorkflowNodeFailed
		nodeExec.Error = fmt.Sprintf("%v", err)
		_ = d.nodeExecRepo.Update(ctx, nodeExec)
		d.handleNodeFailure(ctx, exec, node, result, err, attempt)
		return "", false, true
	}

	nodeExec.OutputData = result.Output
	nodeExec.Status = d.mapNodeStatus(result.Status)
	// FA-WF-01：节点成功（含 skipped/completed 终态）⇒ 该节点预算收束清零，
	// 并清理历史全局键残留，避免旧 _wf_attempt 干扰新键口径。
	if result.Status != "running" {
		clearAttemptFor(exec, node.ID)
	}
	if result.ErrorMessage != "" {
		nodeExec.Error = result.ErrorMessage
	}
	// FA-WF-02：节点证据（结果/状态/耗时）写失败 = 持久化未确认——节点的外部
	// 副作用可能已发生，此时继续按成功推进会让"完成"建立在丢失的证据上。
	// 处置：执行标为明确 failed（带可诊断原因），恢复语义由 FA-WF-06 恢复扫描
	// 与副作用幂等（FA-WF-03）承载；不盲目重放。
	if err := d.nodeExecRepo.Update(ctx, nodeExec); err != nil {
		d.failExecution(ctx, exec, fmt.Sprintf("node %s evidence write failed (persistence unconfirmed): %v", node.ID, err))
		return "", false, true
	}

	for k, v := range result.Output {
		if exec.Context == nil {
			exec.Context = model.JSONMap{}
		}
		exec.Context[k] = v
	}
	for _, effect := range result.SideEffects {
		AppendWorkflowSideEffect(wctx, effect)
	}
	// FA-WF-02：上下文/进度写失败同样不得报成功（CurrentNode 断点丢失 = 恢复
	// 协议失效，下一步推进建立在不可恢复的假断点上）。
	if err := d.execRepo.UpdateFields(ctx, exec.ID, map[string]any{
		"context":         exec.Context,
		"current_node_id": node.ID,
		"updated_at":      time.Now(),
	}); err != nil {
		d.failExecution(ctx, exec, fmt.Sprintf("node %s context write failed (persistence unconfirmed): %v", node.ID, err))
		return "", false, true
	}

	switch result.Status {
	case NodeStatusCompleted, NodeStatusSkipped:
		return result.NextNodeID, false, false
	case NodeStatusWaiting:

		if err := d.execRepo.UpdateFields(ctx, exec.ID, map[string]any{
			"status":     model.WorkflowExecWaiting,
			"updated_at": time.Now(),
		}); err != nil {
			logger.Ctx(ctx).Error().Err(err).Msg("mark execution waiting failed")
		}
		return "", false, true
	case NodeStatusFailed:
		d.handleNodeFailure(ctx, exec, node, result, fmt.Errorf("%s", result.ErrorMessage), attempt)
		return "", false, true
	default:
		logger.Ctx(ctx).Warn().Str("status", result.Status).Msg("unknown node status, treating as completed")
		return result.NextNodeID, false, false
	}
}

func (d *WorkflowDispatcher) mapNodeStatus(s string) string {
	switch s {
	case NodeStatusCompleted:
		return model.WorkflowNodeCompleted
	case NodeStatusWaiting:
		return model.WorkflowNodeRunning
	case NodeStatusFailed:
		return model.WorkflowNodeFailed
	case NodeStatusSkipped:
		return model.WorkflowNodeSkipped
	default:
		return model.WorkflowNodeCompleted
	}
}

func (d *WorkflowDispatcher) handleNodeFailure(
	ctx context.Context,
	exec *model.WorkflowExecution,
	node *WorkflowDefNode,
	result *WorkflowNodeExecResult,
	err error,
	attempt int,
) bool {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if result != nil {
		errMsg = result.ErrorMessage
	}

	// FA-WF-01：执行器显式判定不可重试（Retryable=false 的 failed 结果）⇒ 不排重试，
	// 直接终态 failed——HTTP 确定拒绝、取消或配置错误重复执行等于重复副作用。
	if result != nil && result.Status == NodeStatusFailed && !result.Retryable {
		logger.Ctx(ctx).Warn().
			Str("node_id", node.ID).
			Str("error", errMsg).
			Msg("node failed with Retryable=false, marking execution as failed (no retry)")
		d.failExecution(ctx, exec, fmt.Sprintf("node %s failed (not retryable): %s", node.ID, errMsg))
		return false
	}

	if attempt < d.retryPolicy.MaxAttempts {
		backoff := d.retryPolicy.Backoff(attempt + 1)
		logger.Ctx(ctx).Warn().
			Str("node_id", node.ID).
			Int("attempt", attempt).
			Int("next_attempt", attempt+1).
			Dur("backoff", backoff).
			Err(err).
			Msg("node failed, scheduling retry")

		writeAttemptFor(exec, node.ID, attempt+1)
		_ = d.execRepo.UpdateFields(ctx, exec.ID, map[string]any{
			"context": exec.Context,
			"error":   errMsg,
		})

		d.wg.Add(1)

		utils.SafeGo(context.Background(), "workflow_dispatcher.retry", func(_ context.Context) {
			defer d.wg.Done()
			select {
			case <-time.After(backoff):
				// 行自证：重试协程脱离原请求 ctx，必须按执行单自带租户恢复绑定，
				// 否则 on 模式下 GetByID 被隔离 callback fail-closed，执行永久停在 retrying。
				d.Run(tenant.WithTenantID(context.Background(), exec.TenantID), exec.ID, tracing.TraceIDFromContext(ctx))
			case <-d.stopCh:
				return
			}
		})
		return true
	}

	logger.Ctx(ctx).Error().
		Str("node_id", node.ID).
		Int("attempts", attempt+1).
		Err(err).
		Msg("node failed after max attempts, marking execution as failed")
	d.failExecution(ctx, exec, fmt.Sprintf("node %s failed after %d attempts: %s", node.ID, attempt+1, errMsg))
	return false
}

func (d *WorkflowDispatcher) completeExecution(ctx context.Context, exec *model.WorkflowExecution) {
	now := time.Now()
	// FA-WF-04：完成写带栅栏——已确认停止（terminated）的执行不得被在途节点
	// 的完成写复活/改写；内存对象照常更新供日志与后续逻辑使用。
	exec.Status = model.WorkflowExecCompleted
	exec.FinishedAt = &now
	exec.CurrentNodeID = ""
	applied, err := d.execRepo.UpdateStatusFenced(ctx, exec.ID, map[string]any{
		"status":          model.WorkflowExecCompleted,
		"finished_at":     now,
		"current_node_id": "",
	}, nil)
	if err != nil {
		logger.Ctx(ctx).Error().Uint("execution_id", exec.ID).Err(err).Msg("execution completed write failed")
	}
	if !applied {
		logger.Ctx(ctx).Warn().Uint("execution_id", exec.ID).
			Msg("execution was confirmed terminated; completion write fenced (stop reason preserved)")
		return
	}
	logger.Ctx(ctx).Info().Uint("execution_id", exec.ID).Msg("execution completed successfully")
}

func (d *WorkflowDispatcher) failExecution(ctx context.Context, exec *model.WorkflowExecution, errMsg string) {
	now := time.Now()
	// FA-WF-04：失败写与完成写同一栅栏——已确认停止不被失败写覆盖。
	exec.Status = model.WorkflowExecFailed
	exec.FinishedAt = &now
	exec.Error = errMsg
	applied, err := d.execRepo.UpdateStatusFenced(ctx, exec.ID, map[string]any{
		"status":      model.WorkflowExecFailed,
		"finished_at": now,
		"error":       errMsg,
	}, nil)
	if err != nil {
		logger.Ctx(ctx).Error().Uint("execution_id", exec.ID).Err(err).Msg("execution failed write failed")
	}
	if !applied {
		logger.Ctx(ctx).Warn().Uint("execution_id", exec.ID).
			Msg("execution was confirmed terminated; failure write fenced (stop reason preserved)")
		return
	}
	logger.Ctx(ctx).Error().Uint("execution_id", exec.ID).Str("error", errMsg).Msg("execution marked as failed")
}

func (d *WorkflowDispatcher) terminateExecution(ctx context.Context, exec *model.WorkflowExecution) {
	now := time.Now()
	// FA-WF-04：dispatcher 停机终止同样 CAS（仅 running 可被终止）。
	exec.Status = model.WorkflowExecTerminated
	exec.FinishedAt = &now
	exec.Error = "dispatcher stopped"
	applied, err := d.execRepo.UpdateStatusFenced(ctx, exec.ID, map[string]any{
		"status":      model.WorkflowExecTerminated,
		"finished_at": now,
		"error":       "dispatcher stopped",
	}, []string{model.WorkflowExecRunning})
	if err != nil {
		logger.Ctx(ctx).Error().Uint("execution_id", exec.ID).Err(err).Msg("execution terminate write failed")
	}
	if !applied {
		logger.Ctx(ctx).Info().Uint("execution_id", exec.ID).Msg("execution already terminal; dispatcher stop write fenced")
		return
	}
	logger.Ctx(ctx).Info().Uint("execution_id", exec.ID).Msg("execution terminated by dispatcher stop")
}

func wfTimePtr(t time.Time) *time.Time { return &t }

func safeStatus(e *model.WorkflowExecution) string {
	if e == nil {
		return ""
	}
	return e.Status
}

// nodeAttemptKey FA-WF-01：预算按节点键控（节点成功即清零，重试只累计本节点）。
func nodeAttemptKey(nodeID string) string { return "_wf_attempt_" + nodeID }

func readAttemptFor(exec *model.WorkflowExecution, node *WorkflowDefNode) int {
	if exec == nil || exec.Context == nil {
		return 0
	}
	key := nodeAttemptKey(node.ID)
	if v, ok := exec.Context[key].(float64); ok {
		return int(v)
	}
	if v, ok := exec.Context[key].(int); ok {
		return v
	}
	// 历史全局键兼容读：旧执行的在途预算不丢（写路径已全部改节点键）。
	if v, ok := exec.Context["_wf_attempt"].(float64); ok {
		return int(v)
	}
	if v, ok := exec.Context["_wf_attempt"].(int); ok {
		return v
	}
	return 0
}

func writeAttemptFor(exec *model.WorkflowExecution, nodeID string, attempt int) {
	if exec == nil {
		return
	}
	if exec.Context == nil {
		exec.Context = model.JSONMap{}
	}
	exec.Context[nodeAttemptKey(nodeID)] = attempt
	// 旧全局键退役：防止后续节点误读前一节点的残留预算。
	delete(exec.Context, "_wf_attempt")
}

// clearAttemptFor 节点终态（完成/跳过）时收束该节点预算。
func clearAttemptFor(exec *model.WorkflowExecution, nodeID string) {
	if exec == nil || exec.Context == nil {
		return
	}
	delete(exec.Context, nodeAttemptKey(nodeID))
	delete(exec.Context, "_wf_attempt")
}

// DefaultMaxWorkflowSteps 单个工作流可执行的最大节点数的代码兜底
// （参数中心 workflow.max_workflow_steps 未配置时用）。
// 它是死循环的最后一道闸：定义里出现环时靠这个把执行打断，而不是无限跑下去。
const DefaultMaxWorkflowSteps = 1000

var maxWorkflowStepsProvider func() int

// SetMaxWorkflowStepsProvider 注入工作流最大步数；传 nil 视为不注入。
func SetMaxWorkflowStepsProvider(fn func() int) { maxWorkflowStepsProvider = fn }

// maxWorkflowSteps 生效的最大步数。非正值一律回落兜底——0 会在第一步就判超限，
// 所有工作流全部失败。
func maxWorkflowSteps() int {
	if p := maxWorkflowStepsProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultMaxWorkflowSteps
}

// ProbeMaxWorkflowSteps 导出当前生效的工作流最大步数（仅供装配层测试断言读取口）。
func ProbeMaxWorkflowSteps() int { return maxWorkflowSteps() }

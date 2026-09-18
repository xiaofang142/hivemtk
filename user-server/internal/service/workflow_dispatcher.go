package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
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

	const maxWorkflowSteps = 1000
	steps := 0

	for {
		steps++
		if steps > maxWorkflowSteps {
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

		attempt := readAttempt(exec)
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

	executor := d.registry.MustGet(ctx, node.Type)
	result, err := executor.Execute(ctx, wctx)
	durationMs := time.Since(start).Milliseconds()

	now := time.Now()
	nodeExec.FinishedAt = &now
	nodeExec.DurationMs = int(durationMs)

	if err != nil || result == nil {
		nodeExec.Status = model.WorkflowNodeFailed
		nodeExec.Error = fmt.Sprintf("%v", err)
		_ = d.nodeExecRepo.Update(ctx, nodeExec)
		return "", false, d.handleNodeFailure(ctx, exec, node, result, err, attempt)
	}

	nodeExec.OutputData = result.Output
	nodeExec.Status = d.mapNodeStatus(result.Status)
	if result.ErrorMessage != "" {
		nodeExec.Error = result.ErrorMessage
	}
	_ = d.nodeExecRepo.Update(ctx, nodeExec)

	for k, v := range result.Output {
		if exec.Context == nil {
			exec.Context = model.JSONMap{}
		}
		exec.Context[k] = v
	}
	for _, effect := range result.SideEffects {
		AppendWorkflowSideEffect(wctx, effect)
	}
	_ = d.execRepo.UpdateFields(ctx, exec.ID, map[string]any{
		"context":         exec.Context,
		"current_node_id": node.ID,
		"updated_at":      time.Now(),
	})

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
		return "", false, d.handleNodeFailure(ctx, exec, node, result, fmt.Errorf("%s", result.ErrorMessage), attempt)
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

	if attempt < d.retryPolicy.MaxAttempts {
		backoff := d.retryPolicy.Backoff(attempt + 1)
		logger.Ctx(ctx).Warn().
			Str("node_id", node.ID).
			Int("attempt", attempt).
			Int("next_attempt", attempt+1).
			Dur("backoff", backoff).
			Err(err).
			Msg("node failed, scheduling retry")

		writeAttempt(exec, attempt+1)
		_ = d.execRepo.UpdateFields(ctx, exec.ID, map[string]any{
			"context": exec.Context,
			"error":   errMsg,
		})

		d.wg.Add(1)

		utils.SafeGo(context.Background(), "workflow_dispatcher.retry", func(_ context.Context) {
			defer d.wg.Done()
			select {
			case <-time.After(backoff):
				d.Run(context.Background(), exec.ID, tracing.TraceIDFromContext(ctx))
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
	exec.Status = model.WorkflowExecCompleted
	exec.FinishedAt = &now
	exec.CurrentNodeID = ""
	_ = d.execRepo.Update(ctx, exec)
	logger.Ctx(ctx).Info().Uint("execution_id", exec.ID).Msg("execution completed successfully")
}

func (d *WorkflowDispatcher) failExecution(ctx context.Context, exec *model.WorkflowExecution, errMsg string) {
	now := time.Now()
	exec.Status = model.WorkflowExecFailed
	exec.FinishedAt = &now
	exec.Error = errMsg
	_ = d.execRepo.Update(ctx, exec)
	logger.Ctx(ctx).Error().Uint("execution_id", exec.ID).Str("error", errMsg).Msg("execution marked as failed")
}

func (d *WorkflowDispatcher) terminateExecution(ctx context.Context, exec *model.WorkflowExecution) {
	now := time.Now()
	exec.Status = model.WorkflowExecTerminated
	exec.FinishedAt = &now
	exec.Error = "dispatcher stopped"
	_ = d.execRepo.Update(ctx, exec)
	logger.Ctx(ctx).Info().Uint("execution_id", exec.ID).Msg("execution terminated by dispatcher stop")
}

func wfTimePtr(t time.Time) *time.Time { return &t }

func safeStatus(e *model.WorkflowExecution) string {
	if e == nil {
		return ""
	}
	return e.Status
}

func readAttempt(exec *model.WorkflowExecution) int {
	if exec == nil || exec.Context == nil {
		return 0
	}
	if v, ok := exec.Context["_wf_attempt"].(float64); ok {
		return int(v)
	}
	if v, ok := exec.Context["_wf_attempt"].(int); ok {
		return v
	}
	return 0
}

func writeAttempt(exec *model.WorkflowExecution, attempt int) {
	if exec == nil {
		return
	}
	if exec.Context == nil {
		exec.Context = model.JSONMap{}
	}
	exec.Context["_wf_attempt"] = attempt
}

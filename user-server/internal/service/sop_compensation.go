package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 补偿状态常量
const (
	CompensationStatusPending   = "pending"
	CompensationStatusRunning   = "running"
	CompensationStatusCompleted = "completed"
	CompensationStatusFailed    = "failed"
	CompensationStatusSkipped   = "skipped"
)

// Compensable 节点支持 Saga 补偿的可选接口
//
// 实现者：需要"撤销/回滚"的节点（如发短信 → 补偿删除消息；写 DB → 补偿删除记录；
// 调外部 API → 补偿取消订单）。
//
// Compensate 设计原则：
//   - 必须幂等（可能重试）
//   - 必须有界（不能无限阻塞）
//   - 失败允许（失败不阻断其他补偿，但记日志）
//
// 与 Execute 的区别：Execute 是"做"，Compensate 是"撤销"。
// 业界 SAGA 经典：每个 Activity 都有 Compensation，定义在同一个接口里。
type Compensable interface {
	// Compensate 执行补偿
	// 返回 nil 表示补偿成功；返回 error 表示失败（可重试）
	// ctx 可能已被 cancel（worker 关闭时）
	Compensate(ctx context.Context, execCtx *ExecutionContext) error
}

// CompensationRecord 单个节点的补偿记录
type CompensationRecord struct {
	NodeID     string    `json:"node_id"`
	NodeType   string    `json:"node_type"`
	Status     string    `json:"status"`
	Attempt    int       `json:"attempt"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
}

// CompensationPlan 一次完整补偿计划
type CompensationPlan struct {
	ExecutionID uint                 `json:"execution_id"`
	StartedAt   time.Time            `json:"started_at"`
	FinishedAt  time.Time            `json:"finished_at,omitempty"`
	Records     []CompensationRecord `json:"records"`
	Status      string               `json:"status"`
}

// CompensationManager Saga 补偿管理器
//
// 线程安全：支持多 Execution 并发补偿
//
// 计划保留有上界（config.MaxPlansKept）：本管理器在 T-P1-02 后是**进程级单例**，
// 原先 plans 只增不减 ⇒ 每次失败执行泄漏一条计划（含全部节点记录），
// 故障风暴（如 LLM 集群不可用致大量 SOP 失败）下会无界增长。现按写入顺序淘汰最旧的，
// 因此 Summary()/GetPlan() 只覆盖"最近 N 次"，不是全量史册；全量留档属 DB 侧（T-P1-08）。
type CompensationManager struct {
	mu     sync.RWMutex
	plans  map[uint]*CompensationPlan
	order  []uint // plans 的写入顺序，用于有界淘汰
	config CompensationConfig
}

// CompensationConfig 补偿配置
type CompensationConfig struct {
	MaxAttempts        int
	PerCompensationTTL time.Duration
	TotalTimeout       time.Duration
	MaxPlansKept       int // 内存中保留的补偿计划条数上界（<=0 取默认 512）
}

// DefaultCompensationConfig 默认配置
//
// 业界依据：
//   - 单节点补偿应比原 Activity 执行时间短（撤销操作通常更快）
//   - 总超时留 5 分钟（与 execute 总超时同数量级）
func DefaultCompensationConfig() CompensationConfig {
	return CompensationConfig{
		MaxAttempts:        3,
		PerCompensationTTL: 30 * time.Second,
		TotalTimeout:       5 * time.Minute,
		MaxPlansKept:       512,
	}
}

// NewCompensationManager 构造补偿管理器
func NewCompensationManager(cfg CompensationConfig) *CompensationManager {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.PerCompensationTTL <= 0 {
		cfg.PerCompensationTTL = 30 * time.Second
	}
	if cfg.TotalTimeout <= 0 {
		cfg.TotalTimeout = 5 * time.Minute
	}
	if cfg.MaxPlansKept <= 0 {
		cfg.MaxPlansKept = 512
	}
	return &CompensationManager{
		plans:  make(map[uint]*CompensationPlan),
		config: cfg,
	}
}

// retainPlan 记录本次补偿计划并按上界淘汰最旧者（调用方须已持写锁）。
func (m *CompensationManager) retainPlan(executionID uint, plan *CompensationPlan) {
	if _, exists := m.plans[executionID]; !exists {
		m.order = append(m.order, executionID)
	}
	m.plans[executionID] = plan
	for len(m.plans) > m.config.MaxPlansKept && len(m.order) > 0 {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.plans, oldest)
	}
}

// appendRecord 向已发布的计划追加一条节点补偿记录。
//
// result 自 retainPlan 起就对 GetPlan/Summary 的任意 goroutine 可见，因此对它每次改写
// 都必须持写锁（接线前无生产读者，-race 从未覆盖这条路径；T-P1-02 装配后立刻暴露）。
// 临界区只包住 append 本身，绝不跨 CompensateNode（单次最长 PerCompensationTTL）。
func (m *CompensationManager) appendRecord(result *CompensationPlan, rec CompensationRecord) {
	m.mu.Lock()
	result.Records = append(result.Records, rec)
	m.mu.Unlock()
}

// Plan 构造补偿计划：按 executed 节点的反向顺序构造
//
// inputs:
//   - executedNodes: 按执行顺序的节点列表（成功完成或被跳过）
//   - failedNode: 失败节点（不参与补偿，但其前序节点要补偿）
//
// 返回：反向顺序的节点列表（先补偿最后执行的，最后补偿最早执行的）
func (m *CompensationManager) Plan(executedNodes []CompensationRecord) []CompensationRecord {

	reversed := make([]CompensationRecord, len(executedNodes))
	for i, n := range executedNodes {
		reversed[len(executedNodes)-1-i] = n
	}
	return reversed
}

// CompensateNode 补偿单个节点（带重试和超时）
//
// 业界 pattern（来自 Temporal / Cadence）：补偿是 activity-level 操作，
// 失败可重试；总超时防止补偿卡死整个 execution。
func (m *CompensationManager) CompensateNode(
	ctx context.Context,
	execCtx *ExecutionContext,
	executor NodeExecutor,
) CompensationRecord {
	rec := CompensationRecord{
		NodeID:    execCtx.Node.ID,
		NodeType:  execCtx.Node.Type,
		Status:    CompensationStatusRunning,
		StartedAt: time.Now(),
		TraceID:   execCtx.TraceID,
	}

	comp, ok := executor.(Compensable)
	if !ok {
		rec.Status = CompensationStatusSkipped
		rec.FinishedAt = time.Now()
		logger.Ctx(ctx).Info().
			Str("node_id", rec.NodeID).
			Str("node_type", rec.NodeType).
			Msg("[Compensation] node not compensable, skipped")
		return rec
	}

	var lastErr error
	for attempt := 1; attempt <= m.config.MaxAttempts; attempt++ {
		rec.Attempt = attempt

		compCtx, cancel := context.WithTimeout(ctx, m.config.PerCompensationTTL)
		err := comp.Compensate(compCtx, execCtx)
		cancel()

		if err == nil {
			rec.Status = CompensationStatusCompleted
			rec.FinishedAt = time.Now()
			logger.Ctx(ctx).Info().
				Str("node_id", rec.NodeID).
				Str("node_type", rec.NodeType).
				Int("attempt", attempt).
				Dur("duration", time.Since(rec.StartedAt)).
				Msg("[Compensation] node compensated")
			return rec
		}

		lastErr = err
		logger.Ctx(ctx).Warn().
			Err(err).
			Str("node_id", rec.NodeID).
			Int("attempt", attempt).
			Int("max_attempts", m.config.MaxAttempts).
			Msg("[Compensation] node compensate failed, will retry")

		if attempt < m.config.MaxAttempts {
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				rec.Status = CompensationStatusFailed
				rec.Error = "context cancelled"
				rec.FinishedAt = time.Now()
				return rec
			}
		}
	}

	rec.Status = CompensationStatusFailed
	rec.Error = lastErr.Error()
	rec.FinishedAt = time.Now()
	return rec
}

// Run 启动一次完整补偿流程
//
// inputs:
//   - executionID: SOP execution ID
//   - plan: 补偿节点列表（应已按反向排序）
//   - getExecutor: 通过 nodeType 获取 executor（与 NodeExecutorRegistry 兼容）
//   - execCtxFor: 通过 nodeID 构造 ExecutionContext
//
// 返回：完成的 CompensationPlan
//
// 业界特性：
//   - 单节点失败不阻断其他补偿（best-effort）
//   - 总超时：超时强制结束，防止永久卡死
//   - 终态记录：所有尝试都可在 plan 中回放
func (m *CompensationManager) Run(
	ctx context.Context,
	executionID uint,
	plan []CompensationRecord,
	getExecutor func(nodeType string) NodeExecutor,
	execCtxFor func(nodeID string) *ExecutionContext,
) *CompensationPlan {
	totalCtx, cancel := context.WithTimeout(ctx, m.config.TotalTimeout)
	defer cancel()

	result := &CompensationPlan{
		ExecutionID: executionID,
		StartedAt:   time.Now(),
		Records:     make([]CompensationRecord, 0, len(plan)),
		Status:      CompensationStatusRunning,
	}

	m.mu.Lock()
	m.retainPlan(executionID, result)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		hasFailed := false
		hasCompleted := false
		for _, r := range result.Records {
			if r.Status == CompensationStatusFailed {
				hasFailed = true
			}
			if r.Status == CompensationStatusCompleted {
				hasCompleted = true
			}
		}
		if hasFailed && !hasCompleted {
			result.Status = CompensationStatusFailed
		} else if hasFailed {
			result.Status = "partial"
		} else {
			result.Status = CompensationStatusCompleted
		}
		result.FinishedAt = time.Now()
		status, records := result.Status, len(result.Records)
		m.mu.Unlock()

		logger.Ctx(ctx).Info().
			Uint("execution_id", executionID).
			Str("status", status).
			Int("records", records).
			Msg("[Compensation] run finished")
	}()

	for _, planned := range plan {
		if totalCtx.Err() != nil {
			logger.Ctx(ctx).Warn().Msg("[Compensation] total timeout, abort remaining")

			m.appendRecord(result, CompensationRecord{
				NodeID:     planned.NodeID,
				NodeType:   planned.NodeType,
				Status:     CompensationStatusSkipped,
				Error:      "aborted: " + totalCtx.Err().Error(),
				FinishedAt: time.Now(),
			})
			continue
		}

		execCtx := execCtxFor(planned.NodeID)
		if execCtx == nil {
			m.appendRecord(result, CompensationRecord{
				NodeID:     planned.NodeID,
				Status:     CompensationStatusFailed,
				Error:      "no execution context available",
				FinishedAt: time.Now(),
			})
			continue
		}

		executor := getExecutor(planned.NodeType)
		if executor == nil {
			m.appendRecord(result, CompensationRecord{
				NodeID:     planned.NodeID,
				NodeType:   planned.NodeType,
				Status:     CompensationStatusSkipped,
				FinishedAt: time.Now(),
			})
			continue
		}

		rec := m.CompensateNode(totalCtx, execCtx, executor)
		m.appendRecord(result, rec)
	}

	return result
}

// GetPlan 查询补偿计划。
//
// 返回**快照副本**而非共享指针：Run 会在补偿过程中持续改写 Records/Status，
// 把活指针交给调用方等于把未同步字段暴露给外部 goroutine（-race 实测到会报数据竞争）。
// 若在读取时被 MaxPlansKept 淘汰，返回那一刻的内容仍有效。
func (m *CompensationManager) GetPlan(executionID uint) *CompensationPlan {
	m.mu.RLock()
	defer m.mu.RUnlock()
	plan := m.plans[executionID]
	if plan == nil {
		return nil
	}
	snapshot := *plan
	snapshot.Records = append([]CompensationRecord(nil), plan.Records...)
	return &snapshot
}

// Summary 输出补偿计划摘要（用于监控/调试）
type CompensationSummary struct {
	TotalPlans     int `json:"total_plans"`
	CompletedPlans int `json:"completed_plans"`
	FailedPlans    int `json:"failed_plans"`
	PartialPlans   int `json:"partial_plans"`
	TotalNodes     int `json:"total_nodes"`
	FailedNodes    int `json:"failed_nodes"`
}

// Summary 全局补偿摘要
//
// 口径：只覆盖内存中仍保留的最近 MaxPlansKept 条计划（见 CompensationManager 注释），
// 不是历史全量。用于灰度期"这一轮跑了多少补偿、失败多少"的即时观测。
func (m *CompensationManager) Summary() CompensationSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	summary := CompensationSummary{TotalPlans: len(m.plans)}
	for _, p := range m.plans {
		summary.TotalNodes += len(p.Records)
		switch p.Status {
		case CompensationStatusCompleted:
			summary.CompletedPlans++
		case CompensationStatusFailed:
			summary.FailedPlans++
		case "partial":
			summary.PartialPlans++
		}
		for _, r := range p.Records {
			if r.Status == CompensationStatusFailed {
				summary.FailedNodes++
			}
		}
	}
	return summary
}

// compensationEnvVar Saga 补偿挂载开关（T-P1-02）。默认关闭 = 现网行为零变化。
const compensationEnvVar = "FF_LTC_SAGA_COMPENSATION"

// compensationEnabledFn 判定入口，测试可替换（先例：checkpointEnabledFn）。
var compensationEnabledFn = func() bool { return envFlagEnabled(compensationEnvVar) }

// CompensationEnabled 报告 Saga 补偿挂载是否开启。
func CompensationEnabled() bool { return compensationEnabledFn() }

// InitSOPCompensation 把 Saga 补偿管理器装配到 SOP 调度器（T-P1-02 接线）。
//
// 调用方：cmd/api/main.go（紧随 InitSOPExecutionDispatcher / SetWSHub 之后）。
// 开关关闭时**不注入**，d.compensationMgr 保持 nil ⇒ tryCompensate 首行早退，
// 失败路径与接线前逐字节一致；返回 nil 即表示未挂载。
//
// 为何整进程可共用一个实例：plans 以 executionID 分键、条目互相独立，
// MaxPlansKept 上界防住无界增长；PerCompensationTTL/TotalTimeout 是每次补偿的
// 上下文超时，不是跨执行的可变状态。
func InitSOPCompensation(d *SOPExecutionDispatcher) *CompensationManager {
	if d == nil {
		logger.GetLogger().Warn().Msg("[saga] ⚠️ dispatcher 为 nil，补偿管理器未装配")
		return nil
	}
	if !CompensationEnabled() {
		logger.GetLogger().Info().
			Str("flag", compensationEnvVar).
			Msg("[saga] 补偿未启用（失败执行仍不补偿，与接线前一致）")
		return nil
	}
	mgr := NewCompensationManager(DefaultCompensationConfig())
	d.SetCompensationManager(mgr)
	logger.GetLogger().Info().
		Str("flag", compensationEnvVar).
		Int("max_plans_kept", mgr.config.MaxPlansKept).
		Msg("[saga] ✅ 补偿管理器已装配")
	return mgr
}

var _ = func() *dto.SOPNode {
	return &dto.SOPNode{Type: "compensable_test"}
}

var _ = fmt.Sprintf

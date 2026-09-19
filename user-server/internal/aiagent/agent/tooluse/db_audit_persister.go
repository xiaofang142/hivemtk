package tooluse

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// ToolCallAuditRecord 工具调用审计记录（DB 模型别名）
//
// 实体已收敛到 internal/model.ToolCallAudit（表名 tool_call_audits 不变）。
// 为什么要收敛：本仓建表由 internal/pkg/db 的 AutoMigrate() 清单驱动，模型留在本包
// 时它只在 AutoMigrateAuditTable 里出现 —— 而那个函数**从未被任何生产代码调用过**，
// 于是 check_model_migration.py 的 "文件里含 .AutoMigrate( 就算已登记" 启发式把它
// 误判成已登记（实测库内根本没有 tool_call_audits 表）。现在登记走 allModels()，
// 本包只留别名，写入口径逐字段不变。
type ToolCallAuditRecord = model.ToolCallAudit

// DBAuditLogger DB 持久化 AuditLogger
//
// 实现 AuditLogger 接口，将审计日志写入 PostgreSQL
// 写入异步执行（避免阻塞主流程）
type DBAuditLogger struct {
	db       *gorm.DB
	queue    chan AuditEntry
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
	fallback AuditLogger

	// mu 只保护计数与 lastWarn，不跨 DB 写持有（写慢时不能把计数也堵住）。
	mu          sync.Mutex
	enqueued    int64
	fellBack    int64 // 因队列满或 DB 写失败而降级到 fallback 的条数
	dbRows      int64 // 成功落库的条数
	failBatches int64 // 失败的批次（CreateInBatches 一次整批算一次）
	lastWarn    time.Time
}

// DBAuditStats DB 审计写入的自报口径（供 /agent/tools/audit 与装配层观测）
//
// 为什么必须把这些数摊开：本 logger 的失败方向是"降级到内存"，它对调用方永远返回
// 成功 —— 没有计数的话，"全部落库"与"一条都没写进去、全在内存里等着重启即丢"
// 在外面上看一模一样。
type DBAuditStats struct {
	Enqueued     int64 `json:"enqueued"`
	DBRows       int64 `json:"db_rows"`
	FellBack     int64 `json:"fell_back"`    // 没走正常落库的条数：有 fallback 时改走它，没有则该条未持久化
	FailBatches  int64 `json:"fail_batches"` // CreateInBatches 一次整批算一次
	QueueLen     int   `json:"queue_len"`
	QueueCap     int   `json:"queue_cap"`
	DegradedOnly bool  `json:"degraded_only"` // db 为 nil ⇒ 天生只能走内存
}

// Stats 返回写入计数快照。
func (l *DBAuditLogger) Stats() DBAuditStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return DBAuditStats{
		Enqueued:     l.enqueued,
		DBRows:       l.dbRows,
		FellBack:     l.fellBack,
		FailBatches:  l.failBatches,
		QueueLen:     len(l.queue),
		QueueCap:     cap(l.queue),
		DegradedOnly: l.db == nil,
	}
}

// dbWarnInterval DB 写失败告警的最小间隔。
//
// 刷屏上限：flushBatch 最快 1s 一次（batchSize 100 或 flushInterval），不设间隔时
// 一次 DB 故障能稳定产出 8.6 万行日志/天。
const dbWarnInterval = 60 * time.Second

// NewDBAuditLogger 创建 DB 持久化 AuditLogger
//
// 参数：
//   - db: PostgreSQL 连接
//   - queueSize: 异步队列大小（默认 10000）
//   - fallback: DB 写入失败时的降级 logger（可为 nil）
func NewDBAuditLogger(db *gorm.DB, queueSize int, fallback AuditLogger) *DBAuditLogger {
	if queueSize <= 0 {
		queueSize = 10000
	}
	l := &DBAuditLogger{
		db:       db,
		queue:    make(chan AuditEntry, queueSize),
		stopCh:   make(chan struct{}),
		fallback: fallback,
	}
	l.wg.Add(1)
	go l.consume()
	return l
}

// Log 实现 AuditLogger 接口
//
// 异步写入：将 entry 推入队列，由后台 goroutine 批量写入 DB
// 队列满时降级到 fallback logger（避免丢失审计日志）
func (l *DBAuditLogger) Log(ctx context.Context, entry AuditEntry) {
	select {
	case l.queue <- entry:
		l.mu.Lock()
		l.enqueued++
		l.mu.Unlock()
	default:
		l.useFallback(ctx, entry, "queue_full")
	}
}

// useFallback 把一条审计交给降级通道并计数。
//
// fallback 为 nil 时的措辞刻意只说"持久化副本没落库"，不说"审计丢了"：
// 单独挂本 logger 时确实一条都不剩，但生产形态是 CompositeAuditLogger(内存, 本器)，
// 内存那一腿在进到这里之前就已经收下这条了。把它写成"丢失"会让排障的人去查一条
// 其实看得见的记录。是否真有其它通道由装配方决定，本器无从得知。
func (l *DBAuditLogger) useFallback(ctx context.Context, entry AuditEntry, reason string) {
	l.mu.Lock()
	l.fellBack++
	warn := l.shouldWarnLocked()
	l.mu.Unlock()
	if l.fallback != nil {
		l.fallback.Log(ctx, entry)
		return
	}
	if warn {
		logger.Warnf("[ToolAudit] ⚠️ 审计未落库（本器无降级通道）reason=%s tool=%s", reason, entry.ToolName)
	}
}

// shouldWarnLocked 判定此刻是否该打告警；调用方须持锁。
func (l *DBAuditLogger) shouldWarnLocked() bool {
	if time.Since(l.lastWarn) < dbWarnInterval {
		return false
	}
	l.lastWarn = time.Now()
	return true
}

func (l *DBAuditLogger) consume() {
	defer l.wg.Done()

	const batchSize = 100
	const flushInterval = time.Second
	batch := make([]AuditEntry, 0, batchSize)
	timer := time.NewTimer(flushInterval)
	defer timer.Stop()

	for {
		select {
		case entry := <-l.queue:
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				l.flushBatch(batch)
				batch = batch[:0]
			}
		case <-timer.C:
			if len(batch) > 0 {
				l.flushBatch(batch)
				batch = batch[:0]
			}
			timer.Reset(flushInterval)
		case <-l.stopCh:
			drained := 0
			for {
				select {
				case entry := <-l.queue:
					batch = append(batch, entry)
					drained++
					if len(batch) >= batchSize {
						l.flushBatch(batch)
						batch = batch[:0]
					}
				default:
					goto flushAndExit
				}
			}
		flushAndExit:
			if len(batch) > 0 {
				l.flushBatch(batch)
			}
			if drained > 0 {
				_ = drained
			}
			return
		}
	}
}

func (l *DBAuditLogger) flushBatch(batch []AuditEntry) {
	if len(batch) == 0 {
		return
	}
	ctx := context.Background()
	if l.db == nil {
		// 没有 DB 句柄时整批走降级 —— 这条路径在装配层"db 为 nil"时会长期命中，
		// 所以只按 dbWarnInterval 限速告警一次，不逐批刷。
		l.markDegraded(len(batch), "nil_db")
		if l.fallback != nil {
			for _, e := range batch {
				l.fallback.Log(ctx, e)
			}
		}
		return
	}
	records := make([]ToolCallAuditRecord, 0, len(batch))
	for _, e := range batch {
		records = append(records, auditEntryToRecord(e))
	}
	if err := l.db.CreateInBatches(records, 100).Error; err != nil {
		l.markDegraded(len(batch), "write_failed")
		if l.fallback != nil {
			for _, e := range batch {
				l.fallback.Log(ctx, e)
			}
		}
		return
	}
	l.mu.Lock()
	l.dbRows += int64(len(records))
	l.mu.Unlock()
}

// markDegraded 记一次整批降级并按限速打告警。
func (l *DBAuditLogger) markDegraded(n int, reason string) {
	l.mu.Lock()
	l.fellBack += int64(n)
	l.failBatches++
	warn := l.shouldWarnLocked()
	l.mu.Unlock()
	if !warn {
		return
	}
	if l.fallback != nil {
		logger.Warnf("[ToolAudit] ⚠️ 审计批量落库失败 ⇒ 整批改走降级通道（不再耐久）reason=%s rows=%d", reason, n)
		return
	}
	logger.Warnf("[ToolAudit] ⚠️ 审计批量落库失败 ⇒ 整批未持久化（本器无降级通道）reason=%s rows=%d", reason, n)
}

// Close 优雅关闭：等待队列消费完毕
func (l *DBAuditLogger) Close() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
		l.wg.Wait()
	})
}

// 定长列宽（与 model.ToolCallAudit 的 size tag 同源，改列宽时两处一起改）。
const (
	auditColTraceID     = 64
	auditColToolName    = 128
	auditColCallerID    = 64
	auditColAgentID     = 64
	auditColCustomerID  = 64
	auditColSessionID   = 64
	auditColAuditTrace  = 128
	auditColErrorBudget = 2000
)

// auditEntryToRecord 把内存态审计映射成落库行。
//
// 每个 varchar 列都先按列宽裁一刀，不是防御性冗余而是**批写语义**决定的：
// trace_id 由 middleware/trace.go 从上游 X-Trace-Id 透传（长度不受本地约束），
// 一旦超长，PG 报 `value too long for type character varying(64)`，而 flushBatch 用
// CreateInBatches 整批提交 ⇒ 一行超长会带走同批最多 100 条正常审计，且失败方向是
// 静默降级到内存。截断的代价（同一批里 trace_id 前缀相同的行难以区分）明显更小。
func auditEntryToRecord(e AuditEntry) ToolCallAuditRecord {
	return ToolCallAuditRecord{
		TraceID:       cutBytes(e.TraceID, auditColTraceID),
		ToolName:      cutBytes(e.ToolName, auditColToolName),
		CallerID:      cutBytes(e.CallerID, auditColCallerID),
		AgentID:       cutBytes(e.AgentID, auditColAgentID),
		CustomerID:    cutBytes(e.CustomerID, auditColCustomerID),
		SessionID:     cutBytes(e.SessionID, auditColSessionID),
		Success:       e.Success,
		Error:         cutBytes(e.Error, auditColErrorBudget),
		DurationMs:    int64(e.Duration / time.Millisecond),
		RetryCount:    e.RetryCount,
		AuditTrace:    cutBytes(e.AuditTrace, auditColAuditTrace),
		ArgsSummary:   e.ArgsSummary,
		ResultSummary: e.ResultSummary,
		ExecutedAt:    e.ExecutedAt,
	}
}

// AlertLevel 告警级别
type AlertLevel string

const (
	AlertInfo     AlertLevel = "info"
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

// AlertEvent 告警事件
type AlertEvent struct {
	Level     AlertLevel     `json:"level"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	ToolName  string         `json:"tool_name,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// AlertHandler 告警处理回调
//
// 接收告警事件，由调用方实现具体通知逻辑
//   - 应用层日志
//   - 钉钉机器人
//   - 飞书群机器人
//   - Slack
type AlertHandler interface {
	OnAlert(event AlertEvent)
}

// AlertHandlerFunc 函数式 AlertHandler
type AlertHandlerFunc func(event AlertEvent)

func (f AlertHandlerFunc) OnAlert(event AlertEvent) { f(event) }

// ToolAlertManager 工具调用告警管理器
//
// 基于规则触发告警：
//  1. 工具失败率超过阈值
//  2. 熔断器开启
//  3. 死信队列堆积
//  4. 单次工具调用耗时过长
type ToolAlertManager struct {
	mu             sync.Mutex
	handlers       []AlertHandler
	failureRateMap map[string]*failureRateTracker
}

type failureRateTracker struct {
	total       int
	failed      int
	windowStart time.Time
}

// NewToolAlertManager 创建工具调用告警管理器
func NewToolAlertManager() *ToolAlertManager {
	return &ToolAlertManager{
		failureRateMap: make(map[string]*failureRateTracker),
	}
}

// AddHandler 添加告警处理回调
func (a *ToolAlertManager) AddHandler(handler AlertHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers = append(a.handlers, handler)
}

// OnToolCall 工具调用后回调（由 AuditDecorator 调用）
//
// 触发条件：
//   - 失败率超过 50%（窗口 1 分钟，至少 10 次调用）
//   - 熔断器开启（由 CircuitBreakerDecorator 直接触发）
//   - 单次调用耗时 > 5s
func (a *ToolAlertManager) OnToolCall(entry AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	toolName := entry.ToolName
	tracker, ok := a.failureRateMap[toolName]
	if !ok {
		tracker = &failureRateTracker{windowStart: time.Now()}
		a.failureRateMap[toolName] = tracker
	}

	if time.Since(tracker.windowStart) > time.Minute {
		tracker.total = 0
		tracker.failed = 0
		tracker.windowStart = time.Now()
	}

	tracker.total++
	if !entry.Success {
		tracker.failed++
	}

	if tracker.total >= 10 {
		rate := float64(tracker.failed) / float64(tracker.total)
		if rate > 0.5 {
			a.emitAlert(AlertEvent{
				Level:    AlertWarning,
				Title:    fmt.Sprintf("工具 %s 失败率过高", toolName),
				Message:  fmt.Sprintf("失败率 %.2f%%（%d/%d），最近 1 分钟", rate*100, tracker.failed, tracker.total),
				ToolName: toolName,
				TraceID:  entry.TraceID,
				Extra: map[string]any{
					"failure_rate": rate,
					"total_calls":  tracker.total,
					"failed_calls": tracker.failed,
				},
			})
		}
	}

	if entry.Duration > 5*time.Second {
		a.emitAlert(AlertEvent{
			Level:    AlertWarning,
			Title:    fmt.Sprintf("工具 %s 调用耗时过长", toolName),
			Message:  fmt.Sprintf("耗时 %v，超过 5s 阈值", entry.Duration),
			ToolName: toolName,
			TraceID:  entry.TraceID,
			Extra: map[string]any{
				"duration_ms": entry.Duration.Milliseconds(),
			},
		})
	}
}

// AlertCircuitOpen 触发熔断器开启告警（由 CircuitBreakerDecorator 调用）
func (a *ToolAlertManager) AlertCircuitOpen(toolName string, state CircuitState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emitAlert(AlertEvent{
		Level:    AlertCritical,
		Title:    fmt.Sprintf("工具 %s 熔断器开启", toolName),
		Message:  fmt.Sprintf("熔断器状态：%s，连续失败达到阈值，请检查下游服务", state),
		ToolName: toolName,
		Extra: map[string]any{
			"circuit_state": state.String(),
		},
	})
}

// AlertDeadLetterBacklog 触发死信队列堆积告警
func (a *ToolAlertManager) AlertDeadLetterBacklog(toolName string, backlogCount int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	level := AlertWarning
	if backlogCount > 100 {
		level = AlertCritical
	}
	a.emitAlert(AlertEvent{
		Level:    level,
		Title:    fmt.Sprintf("工具 %s 死信队列堆积", toolName),
		Message:  fmt.Sprintf("待处理死信 %d 条，请及时排查", backlogCount),
		ToolName: toolName,
		Extra: map[string]any{
			"backlog_count": backlogCount,
		},
	})
}

func (a *ToolAlertManager) emitAlert(event AlertEvent) {
	event.Timestamp = time.Now()
	for _, handler := range a.handlers {
		go func(h AlertHandler) {
			defer func() {
				_ = recover()
			}()
			h.OnAlert(event)
		}(handler)
	}
}

// Stats 返回各工具的失败率统计（用于 /metrics endpoint）
func (a *ToolAlertManager) Stats() map[string]map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]map[string]any, len(a.failureRateMap))
	for tool, tracker := range a.failureRateMap {
		rate := 0.0
		if tracker.total > 0 {
			rate = float64(tracker.failed) / float64(tracker.total)
		}
		out[tool] = map[string]any{
			"total_calls":  tracker.total,
			"failed_calls": tracker.failed,
			"failure_rate": rate,
			"window_start": tracker.windowStart,
		}
	}
	return out
}

// CompositeAuditLogger 复合 AuditLogger
//
// 同时将审计日志写入多个目标：
//   - 内存（NewMemoryAuditLogger，便于即时查询）
//   - DB（DBAuditLogger，便于持久化）
//   - 告警管理器（ToolAlertManager，触发告警）
type CompositeAuditLogger struct {
	loggers []AuditLogger
	alert   *ToolAlertManager
}

// NewCompositeAuditLogger 创建复合 AuditLogger
func NewCompositeAuditLogger(memory AuditLogger, dbLogger *DBAuditLogger, alert *ToolAlertManager) *CompositeAuditLogger {
	c := &CompositeAuditLogger{alert: alert}
	if memory != nil {
		c.loggers = append(c.loggers, memory)
	}
	if dbLogger != nil {
		c.loggers = append(c.loggers, dbLogger)
	}
	return c
}

// Log 同时写入所有 logger + 触发告警
func (c *CompositeAuditLogger) Log(ctx context.Context, entry AuditEntry) {
	for _, l := range c.loggers {
		l.Log(ctx, entry)
	}
	if c.alert != nil {
		c.alert.OnToolCall(entry)
	}
}

// MarshalJSON 兼容 JSON 序列化（用于调试）
func (e AlertEvent) MarshalJSON() ([]byte, error) {
	type alias AlertEvent
	return json.Marshal(alias(e))
}

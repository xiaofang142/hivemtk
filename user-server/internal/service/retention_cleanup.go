package service

// retention_cleanup.go 高写入自观测表通用保留清理器（CC-04 刀1）。
//
// 形态对齐两个先例的合体：
//   - geo job_run（DeleteBefore + 保留窗口常量）：user 仓自己的清理先例；
//   - platform heartbeat DeleteHeartbeatsBefore（PLT-04 剩余半边）：参数中心
//     热读保留天数 + 单条 DELETE 行数上限（子查询 LIMIT 圈批，PG 无 DELETE LIMIT）
//     + 循环删至收敛 + 幂等（窗外删空后重跑 0 行）。
//
// 与 message_trace_cleanup_cron 的关系：同级看门狗（retention_cleanup_cron.go），
// 不改 trace cron 的既有语义。八表接入规格（表/时间列/窗口参数键）在刀2 的
// retention_cleanup_tables.go；本文件只提供机制，specs 为空时 RunOnce 是 no-op。
//
// tenant 作用域口径：逐租户清理（tenant.Fanout，对齐 message trace 掩码 cron）。
// DELETE 经 Model 的 schema 拿隔离 callback 自动注入 tenant_id 条件 ——
// 本租户窗外行删、他租户行结构上够不到（on 模式），off 模式退化默认租户单次。

import (
	"context"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// RetentionSpec 一张表的保留策略。
//
// Days 必须是以字面键调 GetInt 的闭包（如 func(ctx) int { return
// GlobalConfigParam().GetInt(ctx, "retention", "api_logs_days", 30) }）——
// scripts/check-config-param-readpoints.py 的读取点门只认「同一行既有字面键
// 又有 Get*(」的形状，经变量拼装的键名门视野外会判 UNDECLARED。
type RetentionSpec struct {
	// Table 表名，只用于结果标识与日志；删除走 Model 的 schema。
	Table string
	// Model gorm 模型；隔离 callback 靠它认出租户表并注入 tenant_id 条件。
	Model any
	// TimeCol 保留窗口度量的时间列：绝大多数表 created_at；trace_events
	// 没有 CreatedAt 字段，度量列为 timestamp（写入时刻即事件时刻）。
	TimeCol string
	// Days 本表保留天数热读（config_param「retention.<表名>_days」）。
	Days func(ctx context.Context) int
	// DefaultDays 代码兜底天数，与种子 DefaultValue 同值（改一处要改两处）。
	DefaultDays int
}

// RetentionTableResult 单表一轮清理结果。
type RetentionTableResult struct {
	Table string
	// Deleted 本轮物理删除行数（跨租户求和）。
	Deleted int64
	// Skipped 跳过原因（空串=已执行）；规格非法等配置错误走这里不走 Err。
	Skipped string
	// Err 本表清理错误（不阻断他表，见 RunOnce）。
	Err error
}

// RetentionCleaner 通用保留清理器：逐表读窗口、逐租户批删窗外行。
type RetentionCleaner struct {
	repo       *repository.RetentionCleanupRepo
	specs      []RetentionSpec
	nowFn      func() time.Time
	batchLimit int
}

// retentionBatchLimit 单条 DELETE 语句行数上限（防长事务锁）。
//
// 取 1000：对齐 platform heartbeat.retention 清理的 heartbeatRetentionBatchLimit
// （3min 心跳节奏下单日新增行数最坏量级远小于它×语句数；首启清理面对存量
// 洪峰时分批收益覆盖，单批仍足够小）。每条 DELETE 最多删这么多行，清理方
// 循环调用直到删完，单语句粒度始终有界。
const retentionBatchLimit = 1000

// NewRetentionCleaner 构造。repo 为 nil 时 RunOnce 是 no-op（与
// MessageTraceCleanupTask 的 nil-repo 口径一致：句柄未装配宁可什么都不做，
// 不让后台任务带无效句柄执行）。
func NewRetentionCleaner(repo *repository.RetentionCleanupRepo, specs []RetentionSpec) *RetentionCleaner {
	return &RetentionCleaner{
		repo:       repo,
		specs:      specs,
		nowFn:      time.Now,
		batchLimit: retentionBatchLimit,
	}
}

// RunOnce 对每张表执行一轮清理，返回逐表结果。
//
// 错误语义：单表错误记入该表结果的 Err，不阻断他表（与 Fanout「单租户失败
// 不阻断其余租户」同口径）；空 specs / 构造缺失返回 nil（no-op，幂等）。
func (c *RetentionCleaner) RunOnce(ctx context.Context) []RetentionTableResult {
	if c == nil || c.repo == nil || len(c.specs) == 0 {
		return nil
	}
	out := make([]RetentionTableResult, 0, len(c.specs))
	for _, spec := range c.specs {
		out = append(out, c.cleanOne(ctx, spec))
	}
	return out
}

func (c *RetentionCleaner) cleanOne(ctx context.Context, spec RetentionSpec) RetentionTableResult {
	res := RetentionTableResult{Table: spec.Table}
	if spec.Model == nil || spec.TimeCol == "" {
		res.Skipped = "spec 缺 Model/TimeCol，跳过"
		return res
	}
	if spec.Days == nil {
		res.Skipped = "spec 缺 Days 读取函数，跳过"
		return res
	}
	days := spec.Days(ctx)
	if days <= 0 {
		// ≤0 不解释为「永不清理」而是回落兜底（对齐 platform heartbeat.retention_days
		// 口径）：静默关掉清理会让表无限增长，要停清理应该摘调度而不是配 0。
		days = spec.DefaultDays
		logger.Warnf("[retention_cleanup] %s 保留天数参数非法(<=0)，回落兜底 %d 天",
			spec.Table, spec.DefaultDays)
	}
	cutoff := c.nowFn().AddDate(0, 0, -days)

	deleted, err := tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
		var total int64
		for {
			n, derr := c.repo.DeleteBeforeBatch(ctx, spec.Model, spec.TimeCol, cutoff, c.batchLimit)
			if derr != nil {
				return total, derr
			}
			total += n
			if n < int64(c.batchLimit) {
				break // 本批不足上限 = 窗口内已删空，幂等收敛
			}
		}
		return total, nil
	})
	if err != nil {
		res.Err = err
		return res
	}
	res.Deleted = deleted
	if deleted > 0 {
		logger.Infof("[retention_cleanup] %s 删除 %d 行（保留 %d 天）", spec.Table, deleted, days)
	}
	return res
}

package repository

// retention_cleanup.go 高写入自观测表通用保留清理仓储（CC-04 刀1）。
//
// 为什么是通用原语而不是每表一个 Delete 方法：api_logs / llm_routing_logs /
// tool_call_audits / feature_flag_eval_logs / layer_decision_logs /
// handoff_decisions / trace_events / platform_llm_usage_records 八张表写入频率
// 从「每 HTTP 请求」到「每 LLM 调用」不等，此前全部零清理路径（仓内四先例
// geo job_run DeleteBefore、message trace 掩码 cron、password_reset 7 天、
// egolite retention sweep 都定了窗口，这批新表未跟进=先例漂移）。清理语义
// 八表完全同构（按时间列删窗外行），只有时间列名与窗口天数不同，参数化后
// 同一原语服务全部接入表，operation_logs 亦同（对齐 platform heartbeat
// DeleteHeartbeatsBefore 与 geo job_run DeleteBefore 的形态）。

import (
	"context"
	"errors"
	"time"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// RetentionCleanupRepo 通用保留清理仓储
type RetentionCleanupRepo struct {
	db *gorm.DB
}

// NewRetentionCleanupRepo 构造（无参，内部取全局 DB）
func NewRetentionCleanupRepo() *RetentionCleanupRepo {
	return NewRetentionCleanupRepoWithDB(_db.GetDB())
}

// NewRetentionCleanupRepoWithDB 显式注入 DB（测试/装配用）
func NewRetentionCleanupRepoWithDB(db *gorm.DB) *RetentionCleanupRepo {
	return &RetentionCleanupRepo{db: db}
}

// conn 返回当前可用的句柄：显式注入的优先，否则每次现取全局句柄。
// 与 MessageTraceCleanupRepo 同口径：本仓储可能在 service 包 init() 时构造，
// 那一刻全局 DB 尚未就绪，缓存 nil 句柄会让清理任务每次都以无效句柄执行。
func (r *RetentionCleanupRepo) conn() (*gorm.DB, error) {
	if r.db != nil {
		return r.db, nil
	}
	if g := _db.GetDB(); g != nil {
		return g, nil
	}
	return nil, gorm.ErrInvalidDB
}

// DeleteBeforeBatch 删除 m 表中 timeCol 早于 before 的行，单条 DELETE 语句
// 最多删 batchLimit 行，返回本语句实际删除行数。
//
// 为什么是子查询而不是 DELETE ... LIMIT：PostgreSQL 不支持 DELETE 直接带
// LIMIT，用 `id IN (SELECT id ... LIMIT ?)` 圈定本批 —— 外层按主键删除，内层
// 走时间列索引，单语句行数始终有界（防长事务锁）。幂等性来自谓词本身：
// 窗外行删空后重跑命中 0 行。
//
// batchLimit<=0 不解释为「全删」：调用方配置错误时宁可不删，也不无界删除。
// timeCol 由调用方以固定规格表传入（非用户输入），不存在注入面；m 必须是
// 已注册租户表的模型，on 模式下隔离 callback 会自动注入 tenant_id 条件。
//
// Unscoped：保留清理的目的是回收空间，带 DeletedAt 的表（如 layer_decision_logs）
// 软删残留同样占空间，一律物理删除；无软删列的表上 Unscoped 是 no-op。
func (r *RetentionCleanupRepo) DeleteBeforeBatch(ctx context.Context, m any, timeCol string, before time.Time, batchLimit int) (int64, error) {
	if batchLimit <= 0 {
		return 0, nil
	}
	if m == nil {
		return 0, errors.New("retention: model must not be nil")
	}
	if timeCol == "" {
		return 0, errors.New("retention: time column must not be empty")
	}
	db, err := r.conn()
	if err != nil {
		return 0, err
	}
	batch := db.WithContext(ctx).Model(m).
		Select("id").
		Where(timeCol+" < ?", before).
		Limit(batchLimit)
	res := db.WithContext(ctx).Unscoped().Model(m).
		Where("id IN (?)", batch).
		Delete(m)
	return res.RowsAffected, res.Error
}

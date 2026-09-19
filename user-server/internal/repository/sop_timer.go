package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SOPTimerRepository SOP 定时器仓储
//
// 负责 sop_timers 表的读写，服务于 OutboxDispatcher 与 WaitExecutor。
type SOPTimerRepository struct {
	db *gorm.DB
}

// NewSOPTimerRepository 创建 SOP 定时器仓储
func NewSOPTimerRepository(db *gorm.DB) *SOPTimerRepository {
	return &SOPTimerRepository{db: db}
}

// Create 创建 SOP 定时器
func (r *SOPTimerRepository) Create(ctx context.Context, timer *model.SOPTimer) error {
	if r == nil || r.db == nil {
		return errors.New("sop timer repository not initialized")
	}
	return r.db.WithContext(ctx).Create(timer).Error
}

// FindDueForUpdate 查询到期 timer（FOR UPDATE SKIP LOCKED，多实例并发安全）
//
// 查询条件：status='pending' AND wait_until <= now
// 排序：wait_until ASC
// 调用方应在事务外使用本方法配合 MarkFired 实现幂等抢占。
func (r *SOPTimerRepository) FindDueForUpdate(ctx context.Context, now time.Time, limit int) ([]model.SOPTimer, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop timer repository not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	var timers []model.SOPTimer
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ? AND wait_until <= ?", "pending", now).
		Order("wait_until ASC").
		Limit(limit).
		Find(&timers).Error
	if err != nil {
		return nil, err
	}
	return timers, nil
}

// MarkFired 原子标记 timer 为 fired（防多实例重复处理），返回受影响行数
//
// 条件：id 匹配且 status='pending'，更新 status='fired' 与 fired_at=now
// RowsAffected=0 表示已被其他实例处理，调用方应跳过。
func (r *SOPTimerRepository) MarkFired(ctx context.Context, id uint, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	res := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]any{
			"status":   "fired",
			"fired_at": &now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// FindPendingByExecutionAndNode 取某执行某节点上仍处于 pending 的定时器（按 id ASC，可多条）。
//
// 为什么需要它（T-P3-02 审批唤醒）：裁决落在**任意时刻**，而等待那一刻写下的定时器只认
// `wait_until`。要提前推进流程，就得从"哪一行在等这件事"反查到定时器，再把它的
// `wait_until` 提前（见 MarkDueNow）—— 反查到多条是合法的（wait 节点重跑过一次 attempt），
// 所以返回切片而不是 First：漏掉第二条 = 那一轮的流程永远醒不过来。
//
// 刻意不带 LIMIT：一个 (execution,node) 上的 pending 定时器天然只有个位数，
// 加上限只会让"漏唤醒"变成静默行为。
func (r *SOPTimerRepository) FindPendingByExecutionAndNode(ctx context.Context, executionID uint, nodeID string) ([]model.SOPTimer, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop timer repository not initialized")
	}
	var timers []model.SOPTimer
	err := r.db.WithContext(ctx).
		Where("execution_id = ? AND node_id = ? AND status = ?", executionID, nodeID, "pending").
		Order("id ASC").
		Find(&timers).Error
	if err != nil {
		return nil, err
	}
	return timers, nil
}

// MarkDueNow 把一枚仍在等的定时器提前到"此刻即可点火"，返回受影响行数。
//
// 为什么改 wait_until 而不是直接 MarkFired + 自己派发（T-P3-02 的推送侧选择）：
// 派发走的是有缓冲的队列，队列满时 DispatchOrLog 只记日志不阻塞 —— 于是"行已 fired、
// 任务没进队"成为一个新的挂死面：状态已不是 pending，轮询器再也不会看它第二眼，
// 流程再也叫不醒。提前到期把点火与派发**留在 outbox 轮询器那条已有路径上**：
// 抢占仍由 MarkFired 的 CAS 提供，裁决那一刻进程正好不在也不影响（新值已落库，
// 下一轮、甚至下一个进程照样点火）⇒ AC② 可证。
// 代价是唤醒延迟从 0 变成一个轮询周期（默认 5s），而推送这条股本就只买"及时性"。
//
// WHERE 里两个条件各挡一类并发：
//   - status='pending'：已 fired/skipped/dead_letter 的行不动（与轮询器抢同一行时返回 0，
//     调用方按"已经有人在处理了"处理）；
//   - wait_until > now：重复通知不会把同一行反复改写，于是 RowsAffected 顺带回答了
//     "这一次是不是我提前到的"。
func (r *SOPTimerRepository) MarkDueNow(ctx context.Context, id uint, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	res := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("id = ? AND status = ? AND wait_until > ?", id, "pending", now).
		Update("wait_until", now)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// CountPendingByExecutionID 统计指定执行 ID 的 pending timer 数
//
// 用于卡死检测：有 pending timer 表示 wait 节点正在等待，不算卡死。
func (r *SOPTimerRepository) CountPendingByExecutionID(ctx context.Context, executionID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("execution_id = ? AND status = ?", executionID, "pending").
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// FindClaimExhaustedPendingTimers S1-5：扫描 claim_count ≥ maxClaims 的 pending timer，
// 用于死信迁移兜底。兼容旧数据：payload->>'claim_count' 回退整数解析。
func (r *SOPTimerRepository) FindClaimExhaustedPendingTimers(ctx context.Context, maxClaims, limit int) ([]model.SOPTimer, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop timer repository not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	const claimExpr = `(CASE WHEN payload->>'claim_count' ~ '^[0-9]+$' THEN (payload->>'claim_count')::int ELSE 0 END)`
	var list []model.SOPTimer
	err := r.db.WithContext(ctx).
		Where("status = ? AND (claim_count >= ? OR "+claimExpr+" >= ?)",
			"pending", maxClaims, maxClaims).
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// FindMaxWaitOverduePendingTimers S1-2：扫描 max_wait_at 已过期的 pending timer。
// 兼容旧数据：payload->>'max_wait_at' 回退 RFC3339 timestamptz 解析。
func (r *SOPTimerRepository) FindMaxWaitOverduePendingTimers(ctx context.Context, now time.Time, limit int) ([]model.SOPTimer, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop timer repository not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	const maxWaitExpr = `(CASE WHEN payload->>'max_wait_at' ~ '^\d{4}-\d{2}-\d{2}T' THEN (payload->>'max_wait_at')::timestamptz ELSE NULL END)`
	var list []model.SOPTimer
	err := r.db.WithContext(ctx).
		Where("status = ? AND ((max_wait_at IS NOT NULL AND max_wait_at <= ?) OR (max_wait_at IS NULL AND "+maxWaitExpr+" IS NOT NULL AND "+maxWaitExpr+" <= ?))",
			"pending", now, now).
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// TransitionPendingStatus 原子把指定 timer 从 pending 转为新状态，返回受影响行数。
//
// 用于死信 / 跳过：WHERE id = ? AND status = 'pending' 限定避免抢占失败时误改。
func (r *SOPTimerRepository) TransitionPendingStatus(ctx context.Context, id uint, newStatus string, firedAt time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	res := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]any{
			"status":   newStatus,
			"fired_at": firedAt,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// BumpClaimCount 累计 claim_count（payload claim_count 同步更新），返回受影响行数
//
// 用于 S1-5：认领失败累计，达到阈值再调用 TransitionPendingStatus 转 dead_letter。
func (r *SOPTimerRepository) BumpClaimCount(ctx context.Context, id uint, claims int, payload model.JSONMap) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	updates := map[string]any{
		"claim_count": claims,
		"payload":     payload,
	}
	res := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// BumpClaimCountAndDeadLetter 累计 claim_count 并原子转 dead_letter，返回受影响行数
func (r *SOPTimerRepository) BumpClaimCountAndDeadLetter(ctx context.Context, id uint, claims int, payload model.JSONMap, firedAt time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	updates := map[string]any{
		"status":      "dead_letter",
		"claim_count": claims,
		"payload":     payload,
		"fired_at":    firedAt,
	}
	res := r.db.WithContext(ctx).Model(&model.SOPTimer{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// GetExecutionSummary 按 ID 获取执行记录的 id+sop_id 摘要，用于写 skipped 事件前置校验
func (r *SOPTimerRepository) GetExecutionSummary(ctx context.Context, executionID uint) (*model.SOPExecution, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("sop timer repository not initialized")
	}
	var row model.SOPExecution
	if err := r.db.WithContext(ctx).Select("id, sop_id").
		Where("id = ?", executionID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeletePendingByExecutionAndNode 删除指定执行+节点的 pending 定时器（D09/D03 补偿配套）。
// 带 status='pending' 守卫：与 MarkFired 的原子条件更新互斥——先 fired 则删不掉（不打断已完成节点），
// 先删则 FindDueForUpdate 不再可见。返回删除行数。
func (r *SOPTimerRepository) DeletePendingByExecutionAndNode(ctx context.Context, executionID uint, nodeID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("sop timer repository not initialized")
	}
	res := r.db.WithContext(ctx).
		Where("execution_id = ? AND node_id = ? AND status = 'pending'", executionID, nodeID).
		Delete(&model.SOPTimer{})
	return res.RowsAffected, res.Error
}

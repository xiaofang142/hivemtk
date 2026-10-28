package egolite

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// egolite 存储层（设计文档 §一 表模型：物理新表、列形状独立、禁跨域外键）。
//
// 与 browser_automation 域的关系纪律：**复制模式不复制实现**——本文件方法集刻意
// 只覆盖 ego 腿自己的读写路径，不 import browser_automation 任何类型；
// write_claim 的「占坑语义」以独立表 + 独立方法落在本域（决策 8：禁共享 seq
// 生成器/断连钩子/外键，跨腿只允许同形语义）。

var (
	// ErrExecutionNotFound 执行不存在或不属于该用户（越权与不存在同响应，防枚举）
	ErrExecutionNotFound = errors.New("执行不存在")
	// ErrExecStateConflict 状态机非法迁移（如对终态执行 confirm/abort）
	ErrExecStateConflict = errors.New("执行状态不允许该操作")
	// ErrConcurrencyGate 并发闸（每用户同时在途执行上限，task.go CountRunningByUser 同纪律）
	ErrConcurrencyGate = errors.New("在途执行已达上限，请等待当前执行结束或停止它")
	// ErrAssetNotFound 资产不存在/已下线/未过扫（三者同响应，不暴露上下线状态）
	ErrAssetNotFound = errors.New("资产不存在或已下线")
	// ErrAssetUnavailable 装配缺陷：market 通道没有资产目录可校验（fail-closed）
	ErrAssetUnavailable = errors.New("资产目录不可用（服务装配异常）")
	// ErrAssetVersionConflict 客户端报的资产版本 ≠ 服务端当前批准版本（版本钉扎，
	// specs/017①：旧橱窗快照/拉取滞后不能换来按旧脚本组装的执行）
	ErrAssetVersionConflict = errors.New("资产版本已更新，请刷新橱窗后重试")
	// ErrFlowScanGate draft 直发通道的扫描门（specs/017④：Start 与 DryRun/Publish/cron 同门）
	ErrFlowScanGate = errors.New("扫描门未通过（存在 block 级命中），先修正脚本再执行")
)

// ---------- 执行 ----------

// EgoExecutionRepository egolite_executions 读写
type EgoExecutionRepository interface {
	Create(ctx context.Context, e *model.EgoliteExecution) error
	GetByID(ctx context.Context, id uint) (*model.EgoliteExecution, error)
	// GetForUser 归属校验版（userID 不符返回 ErrExecutionNotFound——不区分不存在/越权）
	GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteExecution, error)
	// TransitionState 乐观状态迁移：UPDATE ... WHERE id=? AND state IN (from...)，
	// 返回 gorm.ErrRecordNotFound 表示当前状态不在 from 集合（非法迁移）。
	// 单语句原子迁移是并发闸与看门狗不打架的根基（禁止读-判-写三段式）。
	TransitionState(ctx context.Context, id uint, from []string, to string, sets map[string]any) error
	// CountActiveByUser 在途计数（并发闸；口径=非终态全计——
	// dispatching/waiting_confirm 同样占闸，"卡在中间态"和"正在跑"对用户都是资源占用）
	CountActiveByUser(ctx context.Context, userID uint) (int64, error)
	ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteExecution, int64, error)
	// FailActiveByUser 断连收口：该用户全部非终态执行判 failed（FailRunningByUser 同纪律；
	// 判据由 hooks 层给 reason，本方法只做批迁移）
	FailActiveByUser(ctx context.Context, userID uint, reason string) (int64, error)
	// ListConfirmOverdue confirm TTL 看门狗扫描：waiting_confirm 且挂起超过 TTL
	ListConfirmOverdue(ctx context.Context, now time.Time) ([]*model.EgoliteExecution, error)
	// ListTimeoutOverdue 硬超时看门狗：running/dispatching 且超 wall-clock 上限
	ListTimeoutOverdue(ctx context.Context, now time.Time) ([]*model.EgoliteExecution, error)
}

type egoExecutionRepo struct{ db *gorm.DB }

// NewEgoExecutionRepositoryWithDB 显式注入（路由装配与测试用）
func NewEgoExecutionRepositoryWithDB(db *gorm.DB) EgoExecutionRepository {
	return &egoExecutionRepo{db: db}
}

func (r *egoExecutionRepo) Create(ctx context.Context, e *model.EgoliteExecution) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *egoExecutionRepo) GetByID(ctx context.Context, id uint) (*model.EgoliteExecution, error) {
	var e model.EgoliteExecution
	if err := r.db.WithContext(ctx).First(&e, id).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *egoExecutionRepo) GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteExecution, error) {
	var e model.EgoliteExecution
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *egoExecutionRepo) TransitionState(ctx context.Context, id uint, from []string, to string, sets map[string]any) error {
	q := r.db.WithContext(ctx).Model(&model.EgoliteExecution{}).
		Where("id = ? AND state IN ?", id, from)
	updates := map[string]any{"state": to, "updated_at": time.Now()}
	for k, v := range sets {
		updates[k] = v
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound // 当前状态不在 from 集合
	}
	return nil
}

func (r *egoExecutionRepo) CountActiveByUser(ctx context.Context, userID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EgoliteExecution{}).
		Where("user_id = ? AND state NOT IN ?", userID, terminalStatesSlice()).
		Count(&n).Error
	return n, err
}

func (r *egoExecutionRepo) ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteExecution, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.EgoliteExecution{}).
		Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.EgoliteExecution
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *egoExecutionRepo) FailActiveByUser(ctx context.Context, userID uint, reason string) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.EgoliteExecution{}).
		Where("user_id = ? AND state NOT IN ?", userID, terminalStatesSlice()).
		Updates(map[string]any{
			"state": model.EgoExecFailed, "fail_reason": reason,
			"ended_at": time.Now(), "updated_at": time.Now(),
		})
	return res.RowsAffected, res.Error
}

func (r *egoExecutionRepo) ListConfirmOverdue(ctx context.Context, now time.Time) ([]*model.EgoliteExecution, error) {
	// 挂起点没有独立列——用 updated_at 作为进入 waiting_confirm 的近似时刻。
	// 误差可接受：waiting_confirm 态内任何事件行都会刷 updated_at，只会把超时判得
	// 更晚（偏保守方向，不会误杀）。全量扫走 updated_at 索引。
	var list []*model.EgoliteExecution
	err := r.db.WithContext(ctx).
		Where("state = ?", model.EgoExecWaitingConfirm).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, e := range list {
		ttl := e.ConfirmTTLSec
		if ttl <= 0 {
			ttl = 300
		}
		if now.Sub(e.UpdatedAt) > time.Duration(ttl)*time.Second {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *egoExecutionRepo) ListTimeoutOverdue(ctx context.Context, now time.Time) ([]*model.EgoliteExecution, error) {
	var list []*model.EgoliteExecution
	err := r.db.WithContext(ctx).
		Where("state IN ?", []string{model.EgoExecRunning, model.EgoExecDispatching}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, e := range list {
		limit := e.TimeoutSec
		if limit <= 0 {
			limit = 1800
		}
		anchor := e.UpdatedAt
		if e.DispatchedAt != nil {
			anchor = *e.DispatchedAt
		}
		if now.Sub(anchor) > time.Duration(limit)*time.Second {
			out = append(out, e)
		}
	}
	return out, nil
}

func terminalStatesSlice() []string {
	return []string{model.EgoExecCompleted, model.EgoExecFailed, model.EgoExecStopped, model.EgoExecTimeout}
}

// ---------- 事件日志 ----------

// EgoEventRepository ego_event_log（append-only：只有 Append 与 List，无 Update/Delete）
type EgoEventRepository interface {
	Append(ctx context.Context, ev *model.EgoEventLog) error
	ListByExecution(ctx context.Context, executionID uint, limit int) ([]*model.EgoEventLog, error)
	// ListByExecutionAfter Monitor 增量游标（after_id 之后的新增帧）。游标用 id 不用
	// seq：seq 只在轮内单调（重发/续轮会把低 seq 再写进同一 execution），id 才全表单调。
	ListByExecutionAfter(ctx context.Context, executionID uint, afterID uint, limit int) ([]*model.EgoEventLog, error)
}

type egoEventRepo struct{ db *gorm.DB }

func NewEgoEventRepositoryWithDB(db *gorm.DB) EgoEventRepository {
	return &egoEventRepo{db: db}
}

func (r *egoEventRepo) Append(ctx context.Context, ev *model.EgoEventLog) error {
	return r.db.WithContext(ctx).Create(ev).Error
}

func (r *egoEventRepo) ListByExecution(ctx context.Context, executionID uint, limit int) ([]*model.EgoEventLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var list []*model.EgoEventLog
	err := r.db.WithContext(ctx).Where("execution_id = ?", executionID).
		Order("seq ASC, id ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *egoEventRepo) ListByExecutionAfter(ctx context.Context, executionID uint, afterID uint, limit int) ([]*model.EgoEventLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var list []*model.EgoEventLog
	err := r.db.WithContext(ctx).
		Where("execution_id = ? AND id > ?", executionID, afterID).
		Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// ---------- 流程 ----------

// EgoFlowRepository egolite_flows 读写
type EgoFlowRepository interface {
	Create(ctx context.Context, f *model.EgoliteFlow) error
	GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteFlow, error)
	// GetByID 管理端审核读（跨用户；审核工作台按流程号取，不看归属）
	GetByID(ctx context.Context, id uint) (*model.EgoliteFlow, error)
	Update(ctx context.Context, id, userID uint, sets map[string]any) error
	// UpdateAdmin 管理端跨用户更新（审核工作台驳回；与 Update 的 user_id 闸互补）
	UpdateAdmin(ctx context.Context, id uint, sets map[string]any) error
	ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteFlow, int64, error)
	// ListByStatus 管理端按状态读（审核工作台：submitted 清单；空状态 = 全量）
	ListByStatus(ctx context.Context, status string, page, pageSize int) ([]*model.EgoliteFlow, int64, error)
	Delete(ctx context.Context, id, userID uint) error
}

type egoFlowRepo struct{ db *gorm.DB }

func NewEgoFlowRepositoryWithDB(db *gorm.DB) EgoFlowRepository {
	return &egoFlowRepo{db: db}
}

func (r *egoFlowRepo) Create(ctx context.Context, f *model.EgoliteFlow) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *egoFlowRepo) GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteFlow, error) {
	var f model.EgoliteFlow
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound // 同响应防枚举
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *egoFlowRepo) GetByID(ctx context.Context, id uint) (*model.EgoliteFlow, error) {
	var f model.EgoliteFlow
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *egoFlowRepo) Update(ctx context.Context, id, userID uint, sets map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.EgoliteFlow{}).
		Where("id = ? AND user_id = ?", id, userID).Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

func (r *egoFlowRepo) UpdateAdmin(ctx context.Context, id uint, sets map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.EgoliteFlow{}).
		Where("id = ?", id).Updates(sets)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

func (r *egoFlowRepo) ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteFlow, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.EgoliteFlow{}).
		Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.EgoliteFlow
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// ListByStatus 管理端按状态读（审核工作台：submitted 清单；空状态 = 全量）。
// 刻意不带 user_id 过滤：审核的对象就是"别人的流程"，带归属过滤等于看不见任何东西。
func (r *egoFlowRepo) ListByStatus(ctx context.Context, status string, page, pageSize int) ([]*model.EgoliteFlow, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	q := r.db.WithContext(ctx).Model(&model.EgoliteFlow{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.EgoliteFlow
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *egoFlowRepo) Delete(ctx context.Context, id, userID uint) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.EgoliteFlow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

// ---------- 写台账 ----------

// EgoWriteClaimRepository 写台账（browser write_claim 语义的 ego 域独立实现）：
// 占坑（ON CONFLICT DO NOTHING）→ sent → verified / unattributed。
type EgoWriteClaimRepository interface {
	// Claim 占坑：返回 true=拿到；false=已被占（同 execution+action 重复声明幂等——
	// 与 browser 版差异：ego 腿的坑键是 execution+action，同一步重试不是新坑）
	Claim(ctx context.Context, c *model.EgoliteWriteClaim) (bool, error)
	MarkSent(ctx context.Context, id uint) error
	MarkVerified(ctx context.Context, id uint) error
	MarkUnattributed(ctx context.Context, id uint) error
	ListByExecution(ctx context.Context, executionID uint) ([]*model.EgoliteWriteClaim, error)
}

type egoWriteClaimRepo struct{ db *gorm.DB }

func NewEgoWriteClaimRepositoryWithDB(db *gorm.DB) EgoWriteClaimRepository {
	return &egoWriteClaimRepo{db: db}
}

func (r *egoWriteClaimRepo) Claim(ctx context.Context, c *model.EgoliteWriteClaim) (bool, error) {
	// ON CONFLICT DO NOTHING：占坑与「已被占」在同一条语句原子判定。
	// 坑键 (tenant_id, execution_id, action) 需要唯一索引——由模型 uniqueIndex 标签
	// + tenancyidx.Normalize 复合化保证。
	res := r.db.WithContext(ctx).Table("egolite_write_claims").
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "execution_id"}, {Name: "action"}},
			DoNothing: true,
		}).
		Create(map[string]any{
			"tenant_id":    c.TenantID,
			"execution_id": c.ExecutionID,
			"user_id":      c.UserID,
			"asset_id":     c.AssetID,
			"action":       c.Action,
			"claim_state":  model.EgoWriteClaimed,
			"claimed_at":   time.Now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *egoWriteClaimRepo) MarkSent(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.EgoliteWriteClaim{}).
		Where("id = ? AND claim_state = ?", id, model.EgoWriteClaimed).
		Update("claim_state", model.EgoWriteSent).Error
}

func (r *egoWriteClaimRepo) MarkVerified(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.EgoliteWriteClaim{}).
		Where("id = ? AND claim_state = ?", id, model.EgoWriteSent).
		Updates(map[string]any{"claim_state": model.EgoWriteVerified, "verified_at": &now}).Error
}

func (r *egoWriteClaimRepo) MarkUnattributed(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.EgoliteWriteClaim{}).
		Where("id = ? AND claim_state = ?", id, model.EgoWriteSent).
		Update("claim_state", model.EgoWriteUnattributed).Error
}

func (r *egoWriteClaimRepo) ListByExecution(ctx context.Context, executionID uint) ([]*model.EgoliteWriteClaim, error) {
	var list []*model.EgoliteWriteClaim
	err := r.db.WithContext(ctx).Where("execution_id = ?", executionID).
		Order("id ASC").Find(&list).Error
	return list, err
}

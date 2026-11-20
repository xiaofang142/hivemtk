// Package repository 内容安全闸（ContentGuard）的 GORM 仓储实现。
//
// 租户隔离不在本包实现：所有查询都走 db.WithContext(ctx)，租户由 GORM 隔离
// callback 从 ctx 注入（见 internal/pkg/tenancyidx 与 specs/001-saas-multi-tenant）。
// 这里只写 SQL 形状，不做业务判断（五层架构：Repository 不包含业务逻辑）。
package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/contentguard/model"
)

// ── 词库 ───────────────────────────────────────────────────────────────

// GuardWordRepository 词库仓储。
type GuardWordRepository struct {
	db *gorm.DB
}

// NewGuardWordRepository 构造。
func NewGuardWordRepository(db *gorm.DB) *GuardWordRepository {
	return &GuardWordRepository{db: db}
}

// ListEnabled 取当前租户全部启用词条。
func (r *GuardWordRepository) ListEnabled(ctx context.Context) ([]model.GuardWord, error) {
	var rows []model.GuardWord
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("id asc").Find(&rows).Error
	return rows, err
}

// List 分页查询。
func (r *GuardWordRepository) List(ctx context.Context, q model.WordQuery) ([]model.GuardWord, int64, error) {
	tx := r.db.WithContext(ctx).Model(&model.GuardWord{})
	if q.ListKind != "" {
		tx = tx.Where("list_kind = ?", q.ListKind)
	}
	if q.Category != "" {
		tx = tx.Where("category = ?", q.Category)
	}
	if q.Enabled != nil {
		tx = tx.Where("enabled = ?", *q.Enabled)
	}
	if q.Keyword != "" {
		tx = tx.Where("word LIKE ?", "%"+q.Keyword+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(q.Page, q.PageSize)
	var rows []model.GuardWord
	if err := tx.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Get 按主键取词条。
func (r *GuardWordRepository) Get(ctx context.Context, id uint) (*model.GuardWord, error) {
	var row model.GuardWord
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Upsert 按 (list_kind, word) 幂等写入。
//
// 为什么不直接 Create：词库维护是"改词"而不是"加行"，重复录入同一个词
// 应当更新其级别/分类，而不是被唯一索引打回 500。
func (r *GuardWordRepository) Upsert(ctx context.Context, w *model.GuardWord) (*model.GuardWord, error) {
	var cur model.GuardWord
	err := r.db.WithContext(ctx).
		Where("list_kind = ? AND word = ?", w.ListKind, w.Word).
		First(&cur).Error
	switch {
	case err == nil:
		cur.Category = w.Category
		cur.Severity = w.Severity
		cur.Enabled = w.Enabled
		cur.Source = w.Source
		cur.Remark = w.Remark
		if err := r.db.WithContext(ctx).Save(&cur).Error; err != nil {
			return nil, err
		}
		return &cur, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := r.db.WithContext(ctx).Create(w).Error; err != nil {
			return nil, err
		}
		return w, nil
	default:
		return nil, err
	}
}

// BulkUpsert 批量写入（冲突按 (tenant_id, list_kind, word) 更新）。
func (r *GuardWordRepository) BulkUpsert(ctx context.Context, words []model.GuardWord) (int, error) {
	if len(words) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "list_kind"}, {Name: "word"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"category", "severity", "enabled", "source", "remark", "updated_at",
		}),
	}).CreateInBatches(&words, 200)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// Delete 删除词条。
func (r *GuardWordRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.GuardWord{}, id).Error
}

// ── 策略 ───────────────────────────────────────────────────────────────

// GuardPolicyRepository 策略仓储。
type GuardPolicyRepository struct {
	db *gorm.DB
}

// NewGuardPolicyRepository 构造。
func NewGuardPolicyRepository(db *gorm.DB) *GuardPolicyRepository {
	return &GuardPolicyRepository{db: db}
}

// Get 取指定主体类型的策略（不存在返回 nil, nil）。
func (r *GuardPolicyRepository) Get(ctx context.Context, subjectType string) (*model.GuardPolicy, error) {
	var row model.GuardPolicy
	err := r.db.WithContext(ctx).Where("subject_type = ?", subjectType).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// List 全部策略。
func (r *GuardPolicyRepository) List(ctx context.Context) ([]model.GuardPolicy, error) {
	var rows []model.GuardPolicy
	err := r.db.WithContext(ctx).Order("subject_type asc").Find(&rows).Error
	return rows, err
}

// Save 按 subject_type 幂等写入。
func (r *GuardPolicyRepository) Save(ctx context.Context, p *model.GuardPolicy) (*model.GuardPolicy, error) {
	var cur model.GuardPolicy
	err := r.db.WithContext(ctx).Where("subject_type = ?", p.SubjectType).First(&cur).Error
	switch {
	case err == nil:
		p.ID = cur.ID
		p.CreatedAt = cur.CreatedAt
		if err := r.db.WithContext(ctx).Save(p).Error; err != nil {
			return nil, err
		}
		return p, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, err
	}
}

// ── 任务队列 ───────────────────────────────────────────────────────────

// GuardTaskRepository 审查任务仓储。
type GuardTaskRepository struct {
	db *gorm.DB
}

// NewGuardTaskRepository 构造。
func NewGuardTaskRepository(db *gorm.DB) *GuardTaskRepository {
	return &GuardTaskRepository{db: db}
}

// Enqueue 入队（幂等键冲突则返回 false）。
func (r *GuardTaskRepository) Enqueue(ctx context.Context, t *model.GuardTask) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "task_key"}},
		DoNothing: true,
	}).Create(t)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ClaimDue 认领到期任务：FOR UPDATE SKIP LOCKED 保证多副本各拿一份不相交的集合。
func (r *GuardTaskRepository) ClaimDue(ctx context.Context, limit int, now time.Time) ([]model.GuardTask, error) {
	if limit <= 0 {
		limit = 32
	}
	var out []model.GuardTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.GuardTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_run_at <= ?", model.TaskStatusPending, now).
			Order("next_run_at asc, id asc").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uint64, 0, len(rows))
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
		if err := tx.Model(&model.GuardTask{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":     model.TaskStatusRunning,
				"attempts":   gorm.Expr("attempts + 1"),
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].Status = model.TaskStatusRunning
			rows[i].Attempts++
		}
		out = rows
		return nil
	})
	return out, err
}

// MarkDone 标记完成。
func (r *GuardTaskRepository) MarkDone(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.GuardTask{}).Where("id = ?", id).
		Updates(map[string]any{"status": model.TaskStatusDone, "last_error": ""}).Error
}

// MarkFailed 记录失败：retryAt 非零回到 pending 待重试，零值置 failed 死信。
func (r *GuardTaskRepository) MarkFailed(ctx context.Context, id uint64, errMsg string, retryAt time.Time) error {
	if len([]rune(errMsg)) > 500 {
		errMsg = string([]rune(errMsg)[:500])
	}
	updates := map[string]any{
		"status":     model.TaskStatusFailed,
		"last_error": errMsg,
	}
	if !retryAt.IsZero() {
		updates["status"] = model.TaskStatusPending
		updates["next_run_at"] = retryAt
	}
	return r.db.WithContext(ctx).Model(&model.GuardTask{}).Where("id = ?", id).
		Updates(updates).Error
}

// CountPending 待处理任务数（健康检查/积压告警）。
func (r *GuardTaskRepository) CountPending(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GuardTask{}).
		Where("status = ?", model.TaskStatusPending).Count(&n).Error
	return n, err
}

// ── 审查记录 ───────────────────────────────────────────────────────────

// GuardRecordRepository 审查记录仓储。
type GuardRecordRepository struct {
	db *gorm.DB
}

// NewGuardRecordRepository 构造。
func NewGuardRecordRepository(db *gorm.DB) *GuardRecordRepository {
	return &GuardRecordRepository{db: db}
}

// Create 写入记录。
func (r *GuardRecordRepository) Create(ctx context.Context, rec *model.GuardRecord) error {
	return r.db.WithContext(ctx).Create(rec).Error
}

// Get 取记录。
func (r *GuardRecordRepository) Get(ctx context.Context, id uint64) (*model.GuardRecord, error) {
	var row model.GuardRecord
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// List 分页查询。
func (r *GuardRecordRepository) List(ctx context.Context, q model.RecordQuery) ([]model.GuardRecord, int64, error) {
	tx := r.db.WithContext(ctx).Model(&model.GuardRecord{})
	if q.SubjectType != "" {
		tx = tx.Where("subject_type = ?", q.SubjectType)
	}
	if q.SubjectID != "" {
		tx = tx.Where("subject_id = ?", q.SubjectID)
	}
	if q.Scene != "" {
		tx = tx.Where("scene = ?", q.Scene)
	}
	if q.Verdict != "" {
		tx = tx.Where("verdict = ?", q.Verdict)
	}
	if q.ManualStatus != "" {
		tx = tx.Where("manual_status = ?", q.ManualStatus)
	}
	if q.TraceID != "" {
		tx = tx.Where("trace_id = ?", q.TraceID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(q.Page, q.PageSize)
	var rows []model.GuardRecord
	if err := tx.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// CountBlocksSince 窗口内该主体被拦截次数（升级判据）。
func (r *GuardRecordRepository) CountBlocksSince(ctx context.Context, subjectType, subjectID string, since time.Time) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GuardRecord{}).
		Where("subject_type = ? AND subject_id = ? AND verdict = ? AND created_at >= ?",
			subjectType, subjectID, model.VerdictBlock, since).
		Count(&n).Error
	return int(n), err
}

// UpdateManual 更新人工复核状态。
func (r *GuardRecordRepository) UpdateManual(ctx context.Context, id uint64, status string, operatorID uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.GuardRecord{}).Where("id = ?", id).
		Updates(map[string]any{
			"manual_status": status,
			"operator_id":   operatorID,
			"reviewed_at":   at,
		}).Error
}

// SetSanctionID 回填处置号。
func (r *GuardRecordRepository) SetSanctionID(ctx context.Context, id, sanctionID uint64) error {
	return r.db.WithContext(ctx).Model(&model.GuardRecord{}).Where("id = ?", id).
		Update("sanction_id", sanctionID).Error
}

// ── 处置 ───────────────────────────────────────────────────────────────

// GuardSanctionRepository 处置仓储。
type GuardSanctionRepository struct {
	db *gorm.DB
}

// NewGuardSanctionRepository 构造。
func NewGuardSanctionRepository(db *gorm.DB) *GuardSanctionRepository {
	return &GuardSanctionRepository{db: db}
}

// FindActive 取该主体当前生效的处置。
//
// 生效判据写在 SQL 里而不是交给调用方过滤：worker 与热路径都要这一条，
// 两处各写一遍必然漂移（一处忘了判过期 ⇒ 到期冻结永不解除）。
func (r *GuardSanctionRepository) FindActive(ctx context.Context, subjectType, subjectID string, now time.Time) (*model.GuardSanction, error) {
	var row model.GuardSanction
	err := r.db.WithContext(ctx).
		Where("subject_type = ? AND subject_id = ? AND active = ?", subjectType, subjectID, true).
		Where("(level = ? OR expires_at IS NULL OR expires_at > ?)", model.SanctionLevelPermanent, now).
		Order("id desc").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Get 按主键取处置。
func (r *GuardSanctionRepository) Get(ctx context.Context, id uint64) (*model.GuardSanction, error) {
	var row model.GuardSanction
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Create 新建处置。
func (r *GuardSanctionRepository) Create(ctx context.Context, s *model.GuardSanction) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// Update 更新处置。
func (r *GuardSanctionRepository) Update(ctx context.Context, s *model.GuardSanction) error {
	return r.db.WithContext(ctx).Save(s).Error
}

// List 分页查询处置。
func (r *GuardSanctionRepository) List(ctx context.Context, q model.SanctionQuery) ([]model.GuardSanction, int64, error) {
	tx := r.db.WithContext(ctx).Model(&model.GuardSanction{})
	if q.SubjectType != "" {
		tx = tx.Where("subject_type = ?", q.SubjectType)
	}
	if q.SubjectID != "" {
		tx = tx.Where("subject_id = ?", q.SubjectID)
	}
	if q.Kind != "" {
		tx = tx.Where("kind = ?", q.Kind)
	}
	if q.Level != "" {
		tx = tx.Where("level = ?", q.Level)
	}
	if q.ActiveOnly {
		tx = tx.Where("active = ?", true)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(q.Page, q.PageSize)
	var rows []model.GuardSanction
	if err := tx.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// normalizePage 分页参数夹取（page>=1，1<=size<=200）。
func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

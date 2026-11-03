// Package repository - RAG 自动评测仓库
package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// RagEvalRepository RAG 评测仓库
type RagEvalRepository struct {
	db *gorm.DB
}

// NewRagEvalRepository 创建实例
func NewRagEvalRepository() *RagEvalRepository {
	return &RagEvalRepository{db: _db.GetDB()}
}

// NewRagEvalRepositoryWithDB 测试注入
func NewRagEvalRepositoryWithDB(db *gorm.DB) *RagEvalRepository {
	return &RagEvalRepository{db: db}
}

// CreateRun 创建评测运行
func (r *RagEvalRepository) CreateRun(ctx context.Context, run *model.RagEvalRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// GetRun 按 ID 查询
func (r *RagEvalRepository) GetRun(ctx context.Context, id uint) (*model.RagEvalRun, error) {
	var run model.RagEvalRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns 列出评测运行记录（limit 夹取 1..200：controller 的 strconv.Atoi
// 只挡了 <=0，没挡大数）。
func (r *RagEvalRepository) ListRuns(ctx context.Context, limit int) ([]*model.RagEvalRun, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	var runs []*model.RagEvalRun
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// CreateQuestion 批量写入评测问题
func (r *RagEvalRepository) CreateQuestions(ctx context.Context, questions []*model.RagEvalQuestion) error {
	if len(questions) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&questions).Error
}

// ListQuestionsByRun 列出某次运行的所有问题
func (r *RagEvalRepository) ListQuestionsByRun(ctx context.Context, runID uint) ([]*model.RagEvalQuestion, error) {
	var questions []*model.RagEvalQuestion
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&questions).Error
	return questions, err
}

// CompleteRun 更新运行状态为 completed 并聚合指标
// 使用 Raw SQL 直接指定列名，绕过 GORM map key→column 映射的诡异行为
func (r *RagEvalRepository) CompleteRun(ctx context.Context, runID uint) error {

	var result struct {
		Total        int64   `gorm:"column:total"`
		HitCount     int64   `gorm:"column:hit_count"`
		AvgRecall    float64 `gorm:"column:avg_recall"`
		AvgPrecision float64 `gorm:"column:avg_precision"`
	}
	// 聚合查询的错误必须透传：吞掉的话 Scan 落零值，run 会带着
	// total=0 / avg_recall=0 的假指标落成 completed。
	if err := r.db.WithContext(ctx).Model(&model.RagEvalQuestion{}).
		Select(`
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE hit = true) as hit_count,
			AVG(recall) as avg_recall,
			AVG(precision) as avg_precision
		`).
		Where("run_id = ?", runID).
		Scan(&result).Error; err != nil {
		return err
	}

	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).
		Updates(map[string]any{
			"status":        "completed",
			"completed_at":  now,
			"total":         result.Total,
			"hit":           result.HitCount,
			"avg_recall":    result.AvgRecall,
			"avg_precision": result.AvgPrecision,
		}).Error
}

// FailRun 标记运行失败
func (r *RagEvalRepository) FailRun(ctx context.Context, runID uint, errMsg string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).
		Updates(map[string]any{"status": "failed", "completed_at": now, "error_msg": errMsg}).Error
}

// ListRecentQueryLogs 取最近 RAG 查询日志（去重取样源，供自动评测出题）。
func (r *RagEvalRepository) ListRecentQueryLogs(ctx context.Context, since time.Time, limit int) ([]*model.RagQueryLog, error) {
	var logs []*model.RagQueryLog
	err := r.db.WithContext(ctx).
		Model(&model.RagQueryLog{}).
		Where("created_at >= ?", since).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// ListIndexedDocuments 取已索引状态的 KB 文档（评测出题兜底源）。
func (r *RagEvalRepository) ListIndexedDocuments(ctx context.Context, limit int) ([]*model.KBDocument, error) {
	var docs []*model.KBDocument
	err := r.db.WithContext(ctx).
		Model(&model.KBDocument{}).
		Where("status = ?", model.KBDocumentStatusIndexed).
		Limit(limit).
		Find(&docs).Error
	return docs, err
}

// LatestRun 取最新一次「有结果」的评测 run：优先 status=completed 且 total>0，
// 否则退回最新一条记录（首次运行中/全部失败时仍能拿到最近状态）。
// 原来直接取 id DESC 第一条，评测进行期间 KPI（Recall/MRR/评估集）会被那条
// total=-1、eval_set_size=0 的占位行覆盖，页面还会误判「未上传评估集」。
func (r *RagEvalRepository) LatestRun(ctx context.Context) (*model.RagEvalRun, error) {
	var run model.RagEvalRun
	err := r.db.WithContext(ctx).
		Where("status = ? AND total > 0", "completed").
		Order("id DESC").First(&run).Error
	if err == nil {
		return &run, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	var latest model.RagEvalRun
	if err := r.db.WithContext(ctx).Order("id DESC").First(&latest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &model.RagEvalRun{}, nil
		}
		return nil, err
	}
	return &latest, nil
}

// ListRunsDesc 按 id 降序列出 run（limit<=0 或 >100 归一为 20）。
func (r *RagEvalRepository) ListRunsDesc(ctx context.Context, limit int) ([]*model.RagEvalRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var runs []*model.RagEvalRun
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// GetRunRaw 按 ID 取 run（含不存在错误透传）。
func (r *RagEvalRepository) GetRunRaw(ctx context.Context, id uint) (*model.RagEvalRun, error) {
	var run model.RagEvalRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

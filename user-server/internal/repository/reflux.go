// reflux.go 未知问题回流（审核/采纳/簇）仓储——2026-10-25 自 service/reflux/adopter.go
// 下沉（句柄下沉台账 reflux 域，L3 铁律）。状态机与写回 FAQ 的业务留在 service。
package repository

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RefluxRepository unknown_questions / unknown_question_clusters / 审核流水访问。
type RefluxRepository struct {
	db *gorm.DB
}

// NewRefluxRepository 用装配点注入的连接构造（wiring 与集成测试传测试库同源）。
func NewRefluxRepository(db *gorm.DB) *RefluxRepository {
	return &RefluxRepository{db: db}
}

// FindQuestion 单条未知问题；不存在返回 gorm.ErrRecordNotFound（service 侧翻译）。
func (r *RefluxRepository) FindQuestion(ctx context.Context, id int64) (*model.UnknownQuestion, error) {
	var uq model.UnknownQuestion
	if err := r.db.WithContext(ctx).First(&uq, id).Error; err != nil {
		return nil, err
	}
	return &uq, nil
}

// FindCluster 单条簇；不存在返回 gorm.ErrRecordNotFound。
func (r *RefluxRepository) FindCluster(ctx context.Context, id int64) (*model.UnknownQuestionCluster, error) {
	var c model.UnknownQuestionCluster
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateQuestion 按 id 更新未知问题列（updates 由 service 组装业务戳）。
func (r *RefluxRepository) UpdateQuestion(ctx context.Context, id int64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.UnknownQuestion{}).
		Where("id = ?", id).Updates(updates).Error
}

// UpdateCluster 按 id 更新簇列。
func (r *RefluxRepository) UpdateCluster(ctx context.Context, id int64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.UnknownQuestionCluster{}).
		Where("id = ?", id).Updates(updates).Error
}

// TransitionQuestion CAS 式状态迁移：仅当当前状态等于 from 才写 to。
// 与原实现一致：命中零行不算错误（RowsAffected 由调用方按需判读）。
//
// 0 行必须报错：CAS 条件下 0 行说明"这条不存在"或"状态已被别人改走"，
// 直接当成功会让 POST /api/unknown-questions/{id}/submit 对不存在的 id 回 code=0
// （实测 999999 → 200「ok, pending_review」，前端乐观更新出一个幽灵记录）。
// 用 EnsureAffected ⇒ ErrRecordNotFound，由 controller 翻成 404。
func (r *RefluxRepository) TransitionQuestion(ctx context.Context, id int64, from, to string) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Table("unknown_questions").
		Where("id = ? AND status = ?", id, from).Update("status", to))
}

// TransitionCluster 簇的 CAS 式状态迁移（口径同 TransitionQuestion）。
func (r *RefluxRepository) TransitionCluster(ctx context.Context, id int64, from, to string) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Table("unknown_question_clusters").
		Where("id = ? AND status = ?", id, from).Update("status", to))
}

// AdoptClusterMembers 采纳整簇：事务内先把待审成员全部置 adopted 并回填 faq_id，
// 再更新簇本身；返回采纳成员数（事务边界留在仓储——半套采纳比失败更难排查）。
func (r *RefluxRepository) AdoptClusterMembers(ctx context.Context, clusterID int64, faqID uint, operatorID string) (int64, error) {
	var adoptedCount int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.UnknownQuestion{}).
			Where("cluster_id = ? AND status = ?", clusterID, model.UQStatusPendingReview).
			Updates(map[string]any{
				"status": model.UQStatusAdopted, "adopted_faq_id": faqID,
				"reviewed_by": operatorID, "reviewed_at": time.Now(),
			}).Error; err != nil {
			return err
		}
		adoptedCount = tx.Statement.RowsAffected
		return tx.Model(&model.UnknownQuestionCluster{}).Where("id = ?", clusterID).Updates(map[string]any{
			"status": model.UQStatusAdopted, "adopted_faq_id": faqID,
			"reviewed_by": operatorID, "reviewed_at": time.Now(),
		}).Error
	})
	return adoptedCount, err
}

// InsertReviewLog 落一条审核流水（原实现吞错留痕，调用方保持同口径）。
func (r *RefluxRepository) InsertReviewLog(ctx context.Context, l *model.UnknownQuestionReviewLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// ExistingFAQKeywords 既有 FAQ 关键词全集（区分性校验用，上限 5000 行尽力取）。
func (r *RefluxRepository) ExistingFAQKeywords(ctx context.Context) (map[string]int, error) {
	rows := []struct{ Keywords []string }{}
	if err := r.db.WithContext(ctx).Table("faq_entries").
		Select("keywords").Where("keywords IS NOT NULL").Limit(5000).Scan(&rows).Error; err != nil {
		return nil, err
	}
	existing := map[string]int{}
	for _, row := range rows {
		for _, k := range row.Keywords {
			existing[k]++
		}
	}
	return existing, nil
}

// ListQuestions 未知问题分页（filter: status/source/agent_id/question_like）。
func (r *RefluxRepository) ListQuestions(ctx context.Context, filter map[string]any, page, pageSize int) ([]model.UnknownQuestion, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.UnknownQuestion{})
	if v, ok := filter["status"]; ok {
		q = q.Where("status = ?", v)
	}
	if v, ok := filter["source"]; ok {
		q = q.Where("source = ?", v)
	}
	if v, ok := filter["agent_id"]; ok {
		q = q.Where("agent_id = ?", v)
	}
	if v, ok := filter["question_like"]; ok {
		q = q.Where("question ILIKE ?", fmt.Sprintf("%%%v%%", v))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.UnknownQuestion{}
	if err := q.Order("last_seen_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListClusters 簇分页（按 total_frequency 降序）。
func (r *RefluxRepository) ListClusters(ctx context.Context, filter map[string]any, page, pageSize int) ([]model.UnknownQuestionCluster, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.UnknownQuestionCluster{})
	if v, ok := filter["status"]; ok {
		q = q.Where("status = ?", v)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.UnknownQuestionCluster{}
	if err := q.Order("total_frequency DESC").Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// QuestionStats 统计总数 / 待审 / 已采纳（采纳率由 service 计算）。
func (r *RefluxRepository) QuestionStats(ctx context.Context) (total, pending, adopted int64, err error) {
	row := struct {
		Total   int64 `gorm:"column:total"`
		Pending int64 `gorm:"column:pending"`
		Adopted int64 `gorm:"column:adopted"`
	}{}
	if err := r.db.WithContext(ctx).Table("unknown_questions").
		Select(`COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'pending_review') AS pending,
			COUNT(*) FILTER (WHERE status = 'adopted') AS adopted`).
		Scan(&row).Error; err != nil {
		return 0, 0, 0, err
	}
	return row.Total, row.Pending, row.Adopted, nil
}

// QuestionReviewLogs 某问题的审核流水（按时间升序）。
func (r *RefluxRepository) QuestionReviewLogs(ctx context.Context, questionID int64) ([]model.UnknownQuestionReviewLog, error) {
	logs := []model.UnknownQuestionReviewLog{}
	err := r.db.WithContext(ctx).Where("target_type = 'question' AND target_id = ?", questionID).
		Order("created_at ASC").Find(&logs).Error
	return logs, err
}

// ClusterReviewLogs 某簇的审核流水（按时间升序）。
func (r *RefluxRepository) ClusterReviewLogs(ctx context.Context, clusterID int64) ([]model.UnknownQuestionReviewLog, error) {
	logs := []model.UnknownQuestionReviewLog{}
	err := r.db.WithContext(ctx).Where("target_type = 'cluster' AND target_id = ?", clusterID).
		Order("created_at ASC").Find(&logs).Error
	return logs, err
}

// ClusterMembers 簇成员（按出现频次降序）。
func (r *RefluxRepository) ClusterMembers(ctx context.Context, clusterID int64) ([]model.UnknownQuestion, error) {
	members := []model.UnknownQuestion{}
	err := r.db.WithContext(ctx).Where("cluster_id = ?", clusterID).
		Order("frequency DESC").Find(&members).Error
	return members, err
}

// ── collector / clusterer 侧（2026-10-25 二批下沉，原 service/reflux 直查） ──

// UpsertUnknownQuestion 指纹 upsert：命中则频次+1 并刷新 last_seen/source_id。
func (r *RefluxRepository) UpsertUnknownQuestion(ctx context.Context, row *model.UnknownQuestion) error {
	return r.db.WithContext(ctx).Select(
		"TenantID", "QuestionHash", "Question", "Status", "Source", "SourceID",
		"SessionID", "CustomerID", "AgentID", "IntentType", "LowConfScore",
		"Frequency", "FirstSeenAt", "LastSeenAt", "CreatedAt", "UpdatedAt",
	).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "question_hash"}},
		DoUpdates: clause.Assignments(map[string]any{
			"frequency":    gorm.Expr("unknown_questions.frequency + 1"),
			"last_seen_at": gorm.Expr("EXCLUDED.last_seen_at"),
			"source_id":    gorm.Expr("EXCLUDED.source_id"),
		}),
	}).Create(row).Error
}

// FindClusterIDByHash 按指纹取已归属簇 id（未归属返回 0）。
func (r *RefluxRepository) FindClusterIDByHash(ctx context.Context, hash string) (int64, error) {
	var clusterID int64
	err := r.db.WithContext(ctx).Model(&model.UnknownQuestion{}).
		Select("COALESCE(cluster_id, 0)").
		Where("question_hash = ?", hash).
		Limit(1).Scan(&clusterID).Error
	return clusterID, err
}

// RefreshClusterFrequency 重算簇频次合计（成员问题频次可能已 +1）。
func (r *RefluxRepository) RefreshClusterFrequency(ctx context.Context, clusterID int64) error {
	return r.db.WithContext(ctx).Model(&model.UnknownQuestionCluster{}).
		Where("id = ?", clusterID).
		Update("total_frequency", gorm.Expr(`(SELECT COALESCE(SUM(frequency), 0)
			FROM unknown_questions q
			WHERE q.cluster_id = unknown_question_clusters.id
			  AND q.tenant_id = unknown_question_clusters.tenant_id)`)).Error
}

// FindSimilarPendingCluster 在待审簇里按 trgm 相似度找最像的簇（阈值由业务层给定）。
// 未命中返回 0。
func (r *RefluxRepository) FindSimilarPendingCluster(ctx context.Context, question string, threshold float64) (int64, error) {
	var hit struct {
		ID int64 `gorm:"column:id"`
	}
	err := r.db.WithContext(ctx).Model(&model.UnknownQuestionCluster{}).
		Select("id").
		Where("status = ?", model.UQStatusPendingReview).
		Where("similarity(title, ?) > ?", question, threshold).
		Order(clause.OrderBy{
			Expression: clause.Expr{SQL: "similarity(title, ?) DESC", Vars: []interface{}{question}},
		}).
		Limit(1).Scan(&hit).Error
	return hit.ID, err
}

// JoinCluster 事务：把指纹未归属的问题挂进簇，并重算簇三统计（成员数/频次/样本问句）。
func (r *RefluxRepository) JoinCluster(ctx context.Context, clusterID int64, hash string, maxSamples int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.UnknownQuestion{}).
			Where("question_hash = ? AND cluster_id = 0", hash).
			Update("cluster_id", clusterID).Error; err != nil {
			return err
		}
		return tx.Model(&model.UnknownQuestionCluster{}).
			Where("id = ?", clusterID).
			Updates(map[string]any{
				// 三个子查询都按 unknown_questions 计数/取样。cluster_id 是全局主键、
				// 正常情况下不跨租户，但子查询写在 SET 表达式里，隔离 callback 注入
				// 不到（只管外层表）——与 RefreshClusterFrequency 同一防御：显式带上
				// tenant_id 相关条件，把"成员行永远与簇同租户"钉死在 SQL 里。
				// off 模式两侧恒为 1，相关条件恒真，行为逐字不变。
				"question_count": gorm.Expr(
					`(SELECT COUNT(*) FROM unknown_questions q
						WHERE q.cluster_id = ?
						  AND q.tenant_id = unknown_question_clusters.tenant_id)`, clusterID),
				"total_frequency": gorm.Expr(
					`(SELECT COALESCE(SUM(frequency), 0) FROM unknown_questions q
						WHERE q.cluster_id = ?
						  AND q.tenant_id = unknown_question_clusters.tenant_id)`, clusterID),
				"sample_questions": gorm.Expr(
					`(SELECT COALESCE(array_agg(q), unknown_question_clusters.sample_questions) FROM (
						SELECT question AS q FROM unknown_questions
						WHERE cluster_id = ?
						  AND tenant_id = unknown_question_clusters.tenant_id
						ORDER BY last_seen_at DESC LIMIT ?
					) s)`, clusterID, maxSamples),
			}).Error
	})
}

// CreateClusterForHash 事务：按指纹对应的问题新建簇（冲突即已存在，幂等），
// 并把该问题归属到簇。簇初始计数/样本由调用方按业务规则给定。
func (r *RefluxRepository) CreateClusterForHash(ctx context.Context, hash, title string, samples []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var q struct {
			TenantID uint  `gorm:"column:tenant_id"`
			AgentID  uint  `gorm:"column:agent_id"`
			ID       int64 `gorm:"column:id"`
		}
		if err := tx.Model(&model.UnknownQuestion{}).
			Select("tenant_id", "COALESCE(agent_id, 0) AS agent_id", "id").
			Where("question_hash = ?", hash).
			Limit(1).Scan(&q).Error; err != nil {
			return err
		}
		now := time.Now()
		cluster := model.UnknownQuestionCluster{
			TenantID:        q.TenantID,
			ClusterHash:     hash,
			Title:           title,
			AgentID:         q.AgentID,
			Status:          model.UQStatusPendingReview,
			QuestionCount:   1,
			TotalFrequency:  1,
			SampleQuestions: samples,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Select(
			"TenantID", "ClusterHash", "Title", "AgentID", "Status", "QuestionCount",
			"TotalFrequency", "SampleQuestions", "CreatedAt", "UpdatedAt",
		).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "cluster_hash"}},
			DoNothing: true,
		}).Create(&cluster).Error; err != nil {
			return err
		}
		var clusterID int64
		if err := tx.Model(&model.UnknownQuestionCluster{}).
			Select("id").
			Where("cluster_hash = ?", hash).
			Limit(1).Scan(&clusterID).Error; err != nil {
			return err
		}
		return tx.Model(&model.UnknownQuestion{}).
			Where("question_hash = ? AND cluster_id = 0", hash).
			Update("cluster_id", clusterID).Error
	})
}

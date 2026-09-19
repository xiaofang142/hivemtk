// csat.go CSAT 满意度调查仓储（五层 L4）
package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CSATSurveyRepository CSAT 仓储
type CSATSurveyRepository struct {
	db *gorm.DB
}

// NewCSATSurveyRepository 构造
func NewCSATSurveyRepository() *CSATSurveyRepository {
	return &CSATSurveyRepository{db: _db.GetDB()}
}

// UpsertBySession 一会话一调查（幂等创建）
func (r *CSATSurveyRepository) UpsertBySession(ctx context.Context, s *model.CSATSurvey) (*model.CSATSurvey, error) {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoNothing: true,
	}).Create(s).Error
	if err != nil {
		return nil, err
	}
	if s.ID == 0 {
		var existing model.CSATSurvey
		if err := r.db.WithContext(ctx).Where("session_id = ?", s.SessionID).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	return s, nil
}

// SubmitResponse 提交评分
func (r *CSATSurveyRepository) SubmitResponse(ctx context.Context, sessionID string, score int, comment string) (*model.CSATSurvey, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&model.CSATSurvey{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"score":        score,
			"comment":      comment,
			"status":       model.CSATStatusResponded,
			"responded_at": now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var out model.CSATSurvey
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkSent 标记已发送
func (r *CSATSurveyRepository) MarkSent(ctx context.Context, sessionID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.CSATSurvey{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{"status": model.CSATStatusSent, "sent_at": now}).Error
}

// Stats 总体统计（均值/总数/分布）
func (r *CSATSurveyRepository) Stats(ctx context.Context) (map[string]any, error) {
	var total, responded int64
	var avgScore *float64
	if err := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).Count(&total).Error; err != nil {
		return nil, err
	}
	q := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).Where("status = ?", model.CSATStatusResponded)
	if err := q.Count(&responded).Error; err != nil {
		return nil, err
	}
	if err := q.Select("COALESCE(AVG(score), 0)").Scan(&avgScore).Error; err != nil {
		return nil, err
	}
	type distRow struct {
		Score int   `json:"score"`
		Count int64 `json:"count"`
	}
	var dist []distRow
	if err := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).
		Select("score, COUNT(*) AS count").
		Where("status = ?", model.CSATStatusResponded).
		Group("score").Order("score ASC").
		Scan(&dist).Error; err != nil {
		return nil, err
	}
	avg := 0.0
	if avgScore != nil {
		avg = *avgScore
	}
	return map[string]any{
		"total":        total,
		"responded":    responded,
		"avg_score":    avg,
		"distribution": dist,
	}, nil
}

// Trend 按日趋势（近 N 天）
func (r *CSATSurveyRepository) Trend(ctx context.Context, days int) ([]map[string]any, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	type row struct {
		Date string  `json:"date"`
		Avg  float64 `json:"avg_score"`
		Cnt  int64   `json:"count"`
	}
	var rows []row
	// since 下推给 `responded_at >= ?`，而 PG 会话时区钉在 CST ⇒ 边界按业务日算
	since := timeutil.BusinessDate(time.Now().AddDate(0, 0, -days))
	err := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).
		Select("DATE(responded_at) AS date, AVG(score) AS avg, COUNT(*) AS cnt").
		Where("status = ? AND responded_at >= ?", model.CSATStatusResponded, since).
		Group("DATE(responded_at)").Order("date ASC").
		Scan(&rows).Error
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"date": r.Date, "avg_score": r.Avg, "count": r.Cnt})
	}
	return out, err
}

// ListNegative 差评列表（score <= threshold）
func (r *CSATSurveyRepository) ListNegative(ctx context.Context, threshold int, limit int) ([]*model.CSATSurvey, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var list []*model.CSATSurvey
	err := r.db.WithContext(ctx).
		Where("status = ? AND score <= ?", model.CSATStatusResponded, threshold).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

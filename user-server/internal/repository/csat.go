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
//
// 已回收评分（status=responded）的调查单**不得**被推回 sent：会话再次关闭/手动重触发
// 会再走一次 Trigger→MarkSent，原实现无条件覆写状态，于是 score/responded_at 还留着、
// status 回到 sent——而 Stats 与差评列表都按 status='responded' 过滤，
// 结果是客户明明打了分，看板上那一条凭空消失（库里实测到 2 条这种行）。
// 评分一旦回收就是既成事实，重触发只补发邀请，不改回收状态。
func (r *CSATSurveyRepository) MarkSent(ctx context.Context, sessionID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.CSATSurvey{}).
		Where("session_id = ? AND status <> ?", sessionID, model.CSATStatusResponded).
		Updates(map[string]any{"status": model.CSATStatusSent, "sent_at": now}).Error
}

// Stats 总体统计（均值/总数/分布）
//
// since 为 nil 表示全量；非 nil 时按业务日边界下推：调查单总数看 created_at，
// 已回收/均值/分布看 responded_at（"本月收到的评分"），两者口径不同属刻意。
func (r *CSATSurveyRepository) Stats(ctx context.Context, since *time.Time) (map[string]any, error) {
	var total, responded int64
	var avgScore *float64
	countQ := r.db.WithContext(ctx).Model(&model.CSATSurvey{})
	respondedQ := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).Where("status = ?", model.CSATStatusResponded)
	distQ := r.db.WithContext(ctx).Model(&model.CSATSurvey{}).Where("status = ?", model.CSATStatusResponded)
	if since != nil {
		countQ = countQ.Where("created_at >= ?", *since)
		respondedQ = respondedQ.Where("responded_at >= ?", *since)
		distQ = distQ.Where("responded_at >= ?", *since)
	}
	if err := countQ.Count(&total).Error; err != nil {
		return nil, err
	}
	if err := respondedQ.Count(&responded).Error; err != nil {
		return nil, err
	}
	if err := respondedQ.Select("COALESCE(AVG(score), 0)").Scan(&avgScore).Error; err != nil {
		return nil, err
	}
	var dist []CSATDistRow
	if err := distQ.
		Select("score, COUNT(*) AS count").
		Group("score").Order("score ASC").
		Scan(&dist).Error; err != nil {
		return nil, err
	}
	if dist == nil {
		dist = []CSATDistRow{}
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

// CSATDistRow 评分分布的一档（星级 → 回收数）。
// 导出是为了让服务层能在 Stats 的 map 载荷里按类型取回它、派生好评率与差评数。
type CSATDistRow struct {
	Score int   `json:"score"`
	Count int64 `json:"count"`
}

// NegativeRow 差评行：调查单本体 + 会话上的坐席/客户名。
// 管理端差评列表要回答"这个差评是谁接的、哪个客户给的"，
// 而名字只存在于 customer_sessions，所以一次 LEFT JOIN 取回，不留给前端二跳。
type NegativeRow struct {
	model.CSATSurvey
	AgentName string `json:"agent_name"`
	UserName  string `json:"user_name"`
}

// ListNegative 差评列表（score <= threshold）
func (r *CSATSurveyRepository) ListNegative(ctx context.Context, threshold int, limit int) ([]NegativeRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var list []NegativeRow
	err := r.db.WithContext(ctx).
		Table("csat_surveys cs").
		Select("cs.*, COALESCE(sess.agent_name, '') AS agent_name, COALESCE(sess.user_name, '') AS user_name").
		Joins("LEFT JOIN customer_sessions sess ON sess.session_id = cs.session_id").
		Where("cs.status = ? AND cs.score <= ?", model.CSATStatusResponded, threshold).
		Order("cs.responded_at DESC").
		Limit(limit).
		Scan(&list).Error
	return list, err
}

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrLeadScopeDenied 空授权/无可见账号时拒绝统计。
var ErrLeadScopeDenied = errors.New("无权访问该客户")

type LeadMiningJudgementRepository interface {
	Available() bool
	Create(ctx context.Context, rec *model.LeadMiningJudgement) error
	List(ctx context.Context, page, limit int, platform, accountID, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error)
	GetByID(ctx context.Context, judgementID string) (*model.LeadMiningJudgement, error)
	UpdateFeedback(ctx context.Context, judgementID, status, reason, by string) error
	GetQualityStats(ctx context.Context, days int) (map[string]any, error)
	// GetQualityStatsScoped 按员工可见账号集合统计；accounts=nil 表示管理员全量，空切片表示受限拒绝。
	GetQualityStatsScoped(ctx context.Context, accounts []string, days int) (map[string]any, error)
	// ListScoped 按配置人员可见的账号集合过滤判定记录（FR-010）；空集合返回空列表，不为全量。
	ListScoped(ctx context.Context, accounts []string, page, limit int, platform, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error)
	// ListRecentForReAct 按可信来源账号集合与会话/群/发送人读取有界历史判定。
	ListRecentForReAct(ctx context.Context, platform string, accountIDs []string, conversationID, groupID, senderID string, isGroup bool, query string, limit int) ([]model.LeadMiningJudgement, error)
}

type leadMiningJudgementRepo struct {
	db *gorm.DB
}

func NewLeadMiningJudgementRepository() LeadMiningJudgementRepository {
	return &leadMiningJudgementRepo{db: _db.GetDB()}
}

func NewLeadMiningJudgementRepositoryWithDB(db *gorm.DB) LeadMiningJudgementRepository {
	return &leadMiningJudgementRepo{db: db}
}

func (r *leadMiningJudgementRepo) Available() bool { return r != nil && r.db != nil }

func (r *leadMiningJudgementRepo) Create(ctx context.Context, rec *model.LeadMiningJudgement) error {
	db := r.db
	if db == nil {
		db = _db.GetDB()
	}
	if db == nil {
		return nil
	}
	evidenceIncomplete := !rec.EvidenceComplete
	if err := db.WithContext(ctx).Create(rec).Error; err != nil {
		return err
	}
	// EvidenceComplete 的模型默认值是 true；GORM 会把显式 false 当零值回填。
	// 对降级判定显式补写 false，避免持久化机器状态与 typed outcome 不一致。
	if evidenceIncomplete {
		return db.WithContext(ctx).Model(&model.LeadMiningJudgement{}).
			Where("judgement_id = ?", rec.JudgementID).
			UpdateColumn("evidence_complete", false).Error
	}
	return nil
}

func (r *leadMiningJudgementRepo) List(ctx context.Context, page, limit int, platform, accountID, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	q := dbFromCtx(ctx).Model(&model.LeadMiningJudgement{})
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	if leadRole != "" {
		q = q.Where("lead_role = ?", leadRole)
	}
	if matched != nil {
		q = q.Where("matched = ?", *matched)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.LeadMiningJudgement
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *leadMiningJudgementRepo) ListScoped(ctx context.Context, accounts []string, page, limit int, platform, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	empty := make([]model.LeadMiningJudgement, 0)
	if len(accounts) == 0 {
		return empty, 0, nil
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, 0, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	q := r.db.WithContext(ctx).Model(&model.LeadMiningJudgement{}).
		Where("tenant_id = ?", tid).Where("account_id IN ?", accounts)
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}
	if leadRole != "" {
		q = q.Where("lead_role = ?", leadRole)
	}
	if matched != nil {
		q = q.Where("matched = ?", *matched)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.LeadMiningJudgement
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *leadMiningJudgementRepo) ListRecentForReAct(ctx context.Context, platform string, accountIDs []string, conversationID, groupID, senderID string, isGroup bool, query string, limit int) ([]model.LeadMiningJudgement, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
		return nil, ErrLeadTenantRequired
	}
	platform = strings.TrimSpace(platform)
	conversationID = strings.TrimSpace(conversationID)
	groupID = strings.TrimSpace(groupID)
	senderID = strings.TrimSpace(senderID)
	aliases := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID = strings.TrimSpace(accountID); accountID != "" {
			aliases = append(aliases, accountID)
		}
	}
	if platform == "" || len(aliases) == 0 {
		return nil, errors.New("ReAct 历史线索查询缺少可信范围")
	}
	if isGroup {
		if groupID == "" {
			return nil, errors.New("ReAct 群历史查询缺少群标识")
		}
	} else if conversationID == "" && senderID == "" {
		return nil, errors.New("ReAct 历史查询缺少会话或发送人")
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	q := r.db.WithContext(ctx).Model(&model.LeadMiningJudgement{}).
		Where("tenant_id = ? AND platform = ? AND account_id IN ? AND is_group = ?", tid, platform, aliases, isGroup)
	if isGroup {
		q = q.Where("group_id = ?", groupID)
	} else if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	} else {
		q = q.Where("conversation_id = ? AND group_id = ? AND sender_id = ?", "", "", senderID)
	}
	if query = strings.TrimSpace(query); query != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
		q = q.Where(`(summary ILIKE ? ESCAPE '\' OR reason ILIKE ? ESCAPE '\')`, like, like)
	}
	var items []model.LeadMiningJudgement
	if err := q.Select("judgement_id", "created_at", "platform", "account_id", "conversation_id", "group_id", "sender_id",
		"matched", "lead_role", "intent_score", "clue_type", "summary", "reason", "degraded", "evidence_complete").
		Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *leadMiningJudgementRepo) GetByID(ctx context.Context, judgementID string) (*model.LeadMiningJudgement, error) {
	var rec model.LeadMiningJudgement
	err := dbFromCtx(ctx).Where("judgement_id = ?", judgementID).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *leadMiningJudgementRepo) UpdateFeedback(ctx context.Context, judgementID, status, reason, by string) error {
	now := time.Now()
	res := dbFromCtx(ctx).Model(&model.LeadMiningJudgement{}).
		Where("judgement_id = ?", judgementID).
		Updates(map[string]any{
			"feedback_status": status,
			"feedback_reason": reason,
			"feedback_by":     by,
			"feedback_at":     &now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *leadMiningJudgementRepo) GetQualityStats(ctx context.Context, days int) (map[string]any, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	db := dbFromCtx(ctx).Model(&model.LeadMiningJudgement{})

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	var matched int64
	if err := db.Session(&gorm.Session{}).Where("matched = ?", true).Count(&matched).Error; err != nil {
		return nil, err
	}
	var falsePositive int64
	if err := db.Session(&gorm.Session{}).Where("feedback_status = ?", "false_positive").Count(&falsePositive).Error; err != nil {
		return nil, err
	}
	var validFeedback int64
	if err := db.Session(&gorm.Session{}).Where("feedback_status = ?", "valid").Count(&validFeedback).Error; err != nil {
		return nil, err
	}

	var byRole []map[string]any
	if err := db.Session(&gorm.Session{}).
		Select("COALESCE(NULLIF(lead_role,''),'unknown') AS lead_role, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total").
		Group("COALESCE(NULLIF(lead_role,''),'unknown')").Scan(&byRole).Error; err != nil {
		return nil, err
	}

	var byChannel []map[string]any
	if err := db.Session(&gorm.Session{}).
		Select("COALESCE(NULLIF(platform,''),'unknown') AS platform, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total").
		Group("COALESCE(NULLIF(platform,''),'unknown')").Order("total DESC").Limit(20).Scan(&byChannel).Error; err != nil {
		return nil, err
	}

	var trend []map[string]any
	trendDB := db.Session(&gorm.Session{}).
		Select("DATE(created_at) AS day, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total, SUM(CASE WHEN feedback_status='false_positive' THEN 1 ELSE 0 END) AS false_positive_total").
		Where("created_at >= ?", since).
		Group("DATE(created_at)").Order("day ASC")
	if err := trendDB.Scan(&trend).Error; err != nil {
		return nil, err
	}

	return map[string]any{
		"total":          total,
		"matched_total":  matched,
		"false_positive": falsePositive,
		"valid_feedback": validFeedback,
		"by_role":        byRole,
		"by_channel":     byChannel,
		"trend":          trend,
		"days":           days,
	}, nil
}

func (r *leadMiningJudgementRepo) GetQualityStatsScoped(ctx context.Context, accounts []string, days int) (map[string]any, error) {
	if accounts != nil && len(accounts) == 0 {
		return nil, ErrLeadScopeDenied
	}
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	db := dbFromCtx(ctx).Model(&model.LeadMiningJudgement{})
	if accounts != nil {
		db = db.Where("account_id IN ?", accounts)
	}

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	var matched int64
	if err := db.Session(&gorm.Session{}).Where("matched = ?", true).Count(&matched).Error; err != nil {
		return nil, err
	}
	var falsePositive int64
	if err := db.Session(&gorm.Session{}).Where("feedback_status = ?", "false_positive").Count(&falsePositive).Error; err != nil {
		return nil, err
	}
	var validFeedback int64
	if err := db.Session(&gorm.Session{}).Where("feedback_status = ?", "valid").Count(&validFeedback).Error; err != nil {
		return nil, err
	}

	var byRole []map[string]any
	if err := db.Session(&gorm.Session{}).
		Select("COALESCE(NULLIF(lead_role,''),'unknown') AS lead_role, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total").
		Group("COALESCE(NULLIF(lead_role,''),'unknown')").Scan(&byRole).Error; err != nil {
		return nil, err
	}

	var byChannel []map[string]any
	if err := db.Session(&gorm.Session{}).
		Select("COALESCE(NULLIF(platform,''),'unknown') AS platform, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total").
		Group("COALESCE(NULLIF(platform,''),'unknown')").Order("total DESC").Limit(20).Scan(&byChannel).Error; err != nil {
		return nil, err
	}

	var trend []map[string]any
	trendDB := db.Session(&gorm.Session{}).
		Select("DATE(created_at) AS day, COUNT(*) AS total, SUM(CASE WHEN matched THEN 1 ELSE 0 END) AS matched_total, SUM(CASE WHEN feedback_status='false_positive' THEN 1 ELSE 0 END) AS false_positive_total").
		Where("created_at >= ?", since).
		Group("DATE(created_at)").Order("day ASC")
	if err := trendDB.Scan(&trend).Error; err != nil {
		return nil, err
	}

	return map[string]any{
		"total":          total,
		"matched_total":  matched,
		"false_positive": falsePositive,
		"valid_feedback": validFeedback,
		"by_role":        byRole,
		"by_channel":     byChannel,
		"trend":          trend,
		"days":           days,
	}, nil
}

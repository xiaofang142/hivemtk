package repository

import (
	"context"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"
)

// ClueQuery 线索查询条件。
// Type / IsVerify 用指针，nil 表示不参与过滤（区别于值为 0）。
type ClueQuery struct {
	Keyword          string
	Type             *int64
	IsVerify         *int64
	OwnerAccount     string
	IsOpportunity    *int64
	IsGroup          *bool
	GroupID          string
	StartUnix        int64
	EndUnix          int64
	LeadRole         string
	Status           string
	SourceChannel    string
	SourceAccountID  string
	ConversationType string
}

func applyClueQuery(db *gorm.DB, q ClueQuery) *gorm.DB {
	tx := db
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + utils.EscapeLike(kw) + "%"
		tx = tx.Where(
			"(name ILIKE ? ESCAPE '\\' OR account ILIKE ? ESCAPE '\\' OR city ILIKE ? ESCAPE '\\' OR address ILIKE ? ESCAPE '\\' OR COALESCE(owner_account,'') ILIKE ? ESCAPE '\\')",
			like, like, like, like, like,
		)
	}
	if q.Type != nil {
		tx = tx.Where("type = ?", *q.Type)
	}
	if q.IsVerify != nil {
		if *q.IsVerify >= 1 {
			tx = tx.Where("COALESCE(is_verify, 0) >= 1")
		} else {
			tx = tx.Where("COALESCE(is_verify, 0) < 1")
		}
	}
	if q.OwnerAccount != "" {
		tx = tx.Where("owner_account = ?", q.OwnerAccount)
	}
	if q.IsOpportunity != nil {
		if *q.IsOpportunity >= 1 {
			tx = tx.Where("COALESCE(is_opportunity, 0) >= 1")
		} else {
			tx = tx.Where("COALESCE(is_opportunity, 0) < 1")
		}
	}
	if q.IsGroup != nil {
		tx = tx.Where("is_group = ?", *q.IsGroup)
	}
	if q.GroupID != "" {
		tx = tx.Where("group_id = ?", q.GroupID)
	}
	if q.LeadRole != "" {
		tx = tx.Where("lead_role = ?", q.LeadRole)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.SourceChannel != "" {
		tx = tx.Where("source_channel = ?", q.SourceChannel)
	}
	if q.SourceAccountID != "" {
		tx = tx.Where("source_account_id = ?", q.SourceAccountID)
	}
	if q.ConversationType != "" {
		tx = tx.Where("conversation_type = ?", q.ConversationType)
	}
	if q.StartUnix > 0 {
		tx = tx.Where("create_time >= ?", q.StartUnix)
	}
	if q.EndUnix > 0 {
		tx = tx.Where("create_time <= ?", q.EndUnix)
	}
	return tx
}

func (r *clueRepo) ListWithQuery(ctx context.Context, q ClueQuery, page, limit int) ([]*model.Clue, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var total int64
	countTx := applyClueQuery(r.db.WithContext(ctx).Model(&model.Clue{}), q)
	if err := countTx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var clues []*model.Clue
	listTx := applyClueQuery(r.db.WithContext(ctx).Model(&model.Clue{}), q)
	if err := listTx.Order("create_time DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&clues).Error; err != nil {
		return nil, 0, err
	}
	return clues, total, nil
}

func (r *clueRepo) CountWithQuery(ctx context.Context, q ClueQuery) (int64, error) {
	var total int64
	tx := applyClueQuery(r.db.WithContext(ctx).Model(&model.Clue{}), q)
	err := tx.Count(&total).Error
	return total, err
}

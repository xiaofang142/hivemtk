package repository

import (
	"hivemtk-user/internal/model"

	"context"
	"time"

	"gorm.io/gorm"
)

// TikTokCardRepository TikTok 卡片仓储接口
type TikTokCardRepository interface {
	Create(ctx context.Context, card *model.TikTokCard) (*model.TikTokCard, error)
	Update(ctx context.Context, card *model.TikTokCard) (*model.TikTokCard, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.TikTokCard, error)
	GetList(ctx context.Context, req CardListFilter) ([]model.TikTokCard, int64, error)
	IncrementViewCount(ctx context.Context, id uint) error
	IncrementLikeCount(ctx context.Context, id uint) error
	IncrementShareCount(ctx context.Context, id uint) error
	CreateActivity(ctx context.Context, activity *model.TikTokCardActivity) error
	GetOverallStats(ctx context.Context) (int64, int64, int64, []model.TikTokCard, error)
	GetCardStats(ctx context.Context, id uint, days int) (*model.TikTokCard, []model.TikTokCardActivity, error)
	ListAll(ctx context.Context) ([]model.TikTokCard, error)
	CountDailyView(ctx context.Context, day string) (int64, error)
	CountCardDailyView(ctx context.Context, cardID uint, day string) (int64, error)
	ListRecentActivities(ctx context.Context, limit int) ([]model.TikTokCardActivity, error)
}

type tiktokCardRepository struct {
	db *gorm.DB
}

// NewTikTokCardRepository 创建 TikTok 卡片仓储实例
func NewTikTokCardRepository(db *gorm.DB) TikTokCardRepository {
	return &tiktokCardRepository{db: db}
}

func (r *tiktokCardRepository) Create(ctx context.Context, card *model.TikTokCard) (*model.TikTokCard, error) {
	if err := r.db.Create(card).Error; err != nil {
		return nil, err
	}
	return card, nil
}

func (r *tiktokCardRepository) Update(ctx context.Context, card *model.TikTokCard) (*model.TikTokCard, error) {
	if err := r.db.Save(card).Error; err != nil {
		return nil, err
	}
	return card, nil
}

func (r *tiktokCardRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Where("id = ?", id).Delete(&model.TikTokCard{}).Error
}

func (r *tiktokCardRepository) GetByID(ctx context.Context, id uint) (*model.TikTokCard, error) {
	var card model.TikTokCard
	if err := r.db.Where("id = ?", id).First(&card).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *tiktokCardRepository) GetList(ctx context.Context, req CardListFilter) ([]model.TikTokCard, int64, error) {
	var cards []model.TikTokCard
	var total int64

	query := r.db.Model(&model.TikTokCard{})

	if req.Keyword != "" {
		like := "%" + req.Keyword + "%"
		query = query.Where("title LIKE ? OR description LIKE ? OR tags LIKE ?", like, like, like)
	}
	if req.IsActive != nil {
		query = query.Where("is_active = ?", *req.IsActive)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := 1
	pageSize := 20
	if req.Page > 0 {
		page = req.Page
	}
	if req.PageSize > 0 {
		pageSize = req.PageSize
	}
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&cards).Error; err != nil {
		return nil, 0, err
	}

	return cards, total, nil
}

func (r *tiktokCardRepository) IncrementViewCount(ctx context.Context, id uint) error {
	return r.db.Model(&model.TikTokCard{}).
		Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}

func (r *tiktokCardRepository) IncrementLikeCount(ctx context.Context, id uint) error {
	return r.db.Model(&model.TikTokCard{}).
		Where("id = ?", id).
		UpdateColumn("like_count", gorm.Expr("like_count + 1")).Error
}

func (r *tiktokCardRepository) IncrementShareCount(ctx context.Context, id uint) error {
	return r.db.Model(&model.TikTokCard{}).
		Where("id = ?", id).
		UpdateColumn("share_count", gorm.Expr("share_count + 1")).Error
}

func (r *tiktokCardRepository) CreateActivity(ctx context.Context, activity *model.TikTokCardActivity) error {
	return r.db.Create(activity).Error
}

func (r *tiktokCardRepository) GetOverallStats(ctx context.Context) (int64, int64, int64, []model.TikTokCard, error) {
	var totalCards, activeCards, totalViews int64

	if err := r.db.Model(&model.TikTokCard{}).Count(&totalCards).Error; err != nil {
		return 0, 0, 0, nil, err
	}
	if err := r.db.Model(&model.TikTokCard{}).Where("is_active = ?", true).Count(&activeCards).Error; err != nil {
		return 0, 0, 0, nil, err
	}
	if err := r.db.Model(&model.TikTokCard{}).Select("COALESCE(SUM(view_count), 0)").Row().Scan(&totalViews); err != nil {
		return 0, 0, 0, nil, err
	}

	var popular []model.TikTokCard
	if err := r.db.Order("view_count DESC").Limit(5).Find(&popular).Error; err != nil {
		return 0, 0, 0, nil, err
	}

	return totalCards, activeCards, totalViews, popular, nil
}

func (r *tiktokCardRepository) GetCardStats(ctx context.Context, id uint, days int) (*model.TikTokCard, []model.TikTokCardActivity, error) {
	var card model.TikTokCard
	if err := r.db.Where("id = ?", id).First(&card).Error; err != nil {
		return nil, nil, err
	}
	var activities []model.TikTokCardActivity
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)
	if err := r.db.Where("card_id = ? AND created_at >= ?", id, since).Order("created_at DESC").Limit(50).Find(&activities).Error; err != nil {
		return nil, nil, err
	}
	return &card, activities, nil
}

func (r *tiktokCardRepository) ListAll(ctx context.Context) ([]model.TikTokCard, error) {
	var cards []model.TikTokCard
	if err := r.db.Find(&cards).Error; err != nil {
		return nil, err
	}
	return cards, nil
}

func (r *tiktokCardRepository) CountDailyView(ctx context.Context, day string) (int64, error) {
	var n int64
	if err := r.db.Model(&model.TikTokCardActivity{}).
		Where("activity_type = ? AND DATE(created_at) = ?", "view", day).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *tiktokCardRepository) CountCardDailyView(ctx context.Context, cardID uint, day string) (int64, error) {
	var n int64
	if err := r.db.Model(&model.TikTokCardActivity{}).
		Where("card_id = ? AND activity_type = ? AND DATE(created_at) = ?", cardID, "view", day).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *tiktokCardRepository) ListRecentActivities(ctx context.Context, limit int) ([]model.TikTokCardActivity, error) {
	var list []model.TikTokCardActivity
	if err := r.db.Order("created_at DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

package repository

import (
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"context"

	"gorm.io/gorm"
)

// CardListFilter 卡片列表查询过滤条件（repository 层本地定义，不依赖 dto）
// 所有平台卡片仓储统一使用此过滤结构，service 层负责 dto → CardListFilter 转换
type CardListFilter struct {
	Page     int
	PageSize int
	Keyword  string
	IsActive *bool
}

// DouyinCardRepository 抖音卡片仓储接口
type DouyinCardRepository interface {
	Create(ctx context.Context, card *model.DouyinCard) (*model.DouyinCard, error)
	Update(ctx context.Context, card *model.DouyinCard) (*model.DouyinCard, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.DouyinCard, error)
	GetList(ctx context.Context, req CardListFilter) ([]model.DouyinCard, int64, error)
	IncrementViewCount(ctx context.Context, id uint) (*model.DouyinCard, error)
	IncrementLikeCount(ctx context.Context, id uint) error
	IncrementShareCount(ctx context.Context, id uint) error
}

type douyinCardRepository struct {
	db *gorm.DB
}

func NewDouyinCardRepository(db *gorm.DB) DouyinCardRepository {
	return &douyinCardRepository{
		db: db,
	}
}

func (r *douyinCardRepository) Create(ctx context.Context, card *model.DouyinCard) (*model.DouyinCard, error) {
	if err := r.db.WithContext(ctx).Create(card).Error; err != nil {
		return nil, err
	}
	return card, nil
}

func (r *douyinCardRepository) Update(ctx context.Context, card *model.DouyinCard) (*model.DouyinCard, error) {
	logger.Infof("更新抖音卡片，ID: %d, ShortLinkID: %d", card.ID, card.ShortLinkID)

	if err := r.db.WithContext(ctx).Model(&model.DouyinCard{}).Where("id = ?", card.ID).Update("short_link_id", card.ShortLinkID).Error; err != nil {
		return nil, fmt.Errorf("更新short_link_id失败: %v", err)
	}

	if err := r.db.WithContext(ctx).Model(&model.DouyinCard{}).Where("id = ?", card.ID).Updates(map[string]any{
		"title":          card.Title,
		"description":    card.Description,
		"image_url":      card.ImageURL,
		"redirect_url":   card.RedirectURL,
		"domain_pool_id": card.DomainPoolID,
		"tags":           card.Tags,
		"view_count":     card.ViewCount,
		"is_active":      card.IsActive,
	}).Error; err != nil {
		return nil, fmt.Errorf("更新其他字段失败: %v", err)
	}

	logger.Infof("更新抖音卡片成功")

	var updatedCard model.DouyinCard
	if err := r.db.WithContext(ctx).First(&updatedCard, card.ID).Error; err != nil {
		return nil, err
	}

	return &updatedCard, nil
}

func (r *douyinCardRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&model.DouyinCard{}, id)
	if result.Error != nil {
		return result.Error
	}
	// 0 行=不存在：DELETE 幂等成功会让前端把「已删过的卡片」当成功，与其它模块 404 语义不一致
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *douyinCardRepository) GetByID(ctx context.Context, id uint) (*model.DouyinCard, error) {
	var card model.DouyinCard
	if err := r.db.WithContext(ctx).First(&card, id).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *douyinCardRepository) GetList(ctx context.Context, req CardListFilter) ([]model.DouyinCard, int64, error) {
	var cards []model.DouyinCard
	var total int64

	query := r.db.WithContext(ctx).Model(&model.DouyinCard{})

	if req.Keyword != "" {
		query = query.Where("title LIKE ? OR description LIKE ? OR tags LIKE ?",
			"%"+req.Keyword+"%", "%"+req.Keyword+"%", "%"+req.Keyword+"%")
	}

	if req.IsActive != nil {
		query = query.Where("is_active = ?", *req.IsActive)
	}

	if req.Page > 0 && req.PageSize > 0 {
		offset := (req.Page - 1) * req.PageSize
		query = query.Offset(offset).Limit(req.PageSize)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Find(&cards).Error; err != nil {
		return nil, 0, err
	}

	return cards, total, nil
}

// IncrementViewCount 卡片浏览量 +1 并返回最新行。
// 必须是原子 UPDATE 再读回：读整行回写会丢并发增量、且会覆盖期间其他写口改的字段。
func (r *douyinCardRepository) IncrementViewCount(ctx context.Context, id uint) (*model.DouyinCard, error) {
	res := r.db.WithContext(ctx).Model(&model.DouyinCard{}).
		Where("id = ?", id).
		Update("view_count", gorm.Expr("view_count + ?", 1))
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var card model.DouyinCard
	if err := r.db.WithContext(ctx).First(&card, id).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *douyinCardRepository) IncrementLikeCount(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.DouyinCard{}).Where("id = ?", id).Update("like_count", gorm.Expr("like_count + ?", 1)).Error
}

func (r *douyinCardRepository) IncrementShareCount(ctx context.Context, id uint) error {

	var card model.DouyinCard
	if err := r.db.WithContext(ctx).First(&card, id).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.DouyinCard{}).Where("id = ?", id).Update("share_count", gorm.Expr("share_count + ?", 1)).Error
}

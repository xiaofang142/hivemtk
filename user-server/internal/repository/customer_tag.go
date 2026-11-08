package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// CustomerTagRepository defines the interface for customer tag data access
type CustomerTagRepository interface {
	Create(ctx context.Context, tag *model.CustomerTag) error
	GetByID(ctx context.Context, id string) (*model.CustomerTag, error)
	ListByMerchant(ctx context.Context) ([]*model.CustomerTag, error)
	ListAutoTags(ctx context.Context) ([]*model.CustomerTag, error)
	Update(ctx context.Context, tag *model.CustomerTag) error
	Delete(ctx context.Context, id string) error
}

type customerTagRepository struct{}

// NewCustomerTagRepository creates a new CustomerTagRepository instance
func NewCustomerTagRepository() CustomerTagRepository {
	return &customerTagRepository{}
}

func (r *customerTagRepository) Create(ctx context.Context, tag *model.CustomerTag) error {
	return _db.GetDB().WithContext(ctx).Create(tag).Error
}

func (r *customerTagRepository) GetByID(ctx context.Context, id string) (*model.CustomerTag, error) {
	var tag model.CustomerTag
	if err := _db.GetDB().WithContext(ctx).First(&tag, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tag, nil
}

func (r *customerTagRepository) ListByMerchant(ctx context.Context) ([]*model.CustomerTag, error) {
	var tags []*model.CustomerTag

	if err := _db.GetDB().WithContext(ctx).
		Order("category, name").
		Find(&tags).Error; err != nil {
		return nil, err
	}

	return tags, nil
}

func (r *customerTagRepository) ListAutoTags(ctx context.Context) ([]*model.CustomerTag, error) {
	var tags []*model.CustomerTag

	if err := _db.GetDB().WithContext(ctx).
		Where("source = ?", model.TagSourceAuto).
		Order("category, name").
		Find(&tags).Error; err != nil {
		return nil, err
	}

	return tags, nil
}

func (r *customerTagRepository) Delete(ctx context.Context, id string) error {
	// 与下方 Update 同一口径：命中 0 行（不存在/别的租户）不许当成功——
	// 管理端会显示「删除成功」而规则原样还在（#981，D-35 同型）。
	res := _db.GetDB().WithContext(ctx).Delete(&model.CustomerTag{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Update 按主键白名单更新标签规则（name/category/rule）。
//
// 不用 Save 整行回写：会把主键与只读列（source/created_at）一起交给 GORM，
// 且命中 0 行也返回成功——记录已删或 id 属于别的租户时，管理端照旧显示"已保存"。
func (r *customerTagRepository) Update(ctx context.Context, tag *model.CustomerTag) error {
	res := _db.GetDB().WithContext(ctx).
		Model(&model.CustomerTag{}).
		Where("id = ?", tag.ID).
		Updates(map[string]any{
			"name":     tag.Name,
			"category": tag.Category,
			"rule":     tag.Rule,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// macro_repo.go 宏 CRUD 仓储（五层 L5）
package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// MacroRepository macros 表 CRUD
type MacroRepository struct {
	db *gorm.DB
}

// NewMacroRepository 构造
func NewMacroRepository(db *gorm.DB) *MacroRepository {
	return &MacroRepository{db: db}
}

// Create 创建宏
func (r *MacroRepository) Create(ctx context.Context, m *model.Macro) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// List 宏列表（id 升序）
func (r *MacroRepository) List(ctx context.Context) ([]*model.Macro, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var list []*model.Macro
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// Delete 删除宏
func (r *MacroRepository) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Delete(&model.Macro{}, id).Error
}

// GetByID 按 ID 取宏
func (r *MacroRepository) GetByID(ctx context.Context, id uint) (*model.Macro, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var m model.Macro
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

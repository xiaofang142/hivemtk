package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CustomerTagAssignmentRepository interface {
	GetByCustomerAndTag(ctx context.Context, customerID, tag string) (*model.CustomerTagAssignment, error)
	ListByCustomerID(ctx context.Context, customerID string) ([]*model.CustomerTagAssignment, error)
	Create(ctx context.Context, assignment *model.CustomerTagAssignment) error
	Update(ctx context.Context, assignment *model.CustomerTagAssignment) error
	DeleteByCustomerAndTag(ctx context.Context, customerID, tag string) error
	Upsert(ctx context.Context, assignment *model.CustomerTagAssignment) error
	ListCustomerIDsByTag(ctx context.Context, tag string, limit int) ([]string, int64, error)
}

type customerTagAssignmentRepository struct {
	db *gorm.DB
}

func NewCustomerTagAssignmentRepository() CustomerTagAssignmentRepository {
	return &customerTagAssignmentRepository{}
}

// NewCustomerTagAssignmentRepositoryWithDB 注入指定库（测试与圈选侧使用）；
// 不带 db 的构造函数保持原样走全局句柄，既有调用方零变化。
func NewCustomerTagAssignmentRepositoryWithDB(database *gorm.DB) CustomerTagAssignmentRepository {
	return &customerTagAssignmentRepository{db: database}
}

func assignmentDB() (*gorm.DB, error) {
	database := _db.GetDB()
	if database == nil {
		return nil, gorm.ErrInvalidDB
	}
	return database, nil
}

// database 取本实例应使用的句柄：注入了用注入的，否则回落到全局。
func (r *customerTagAssignmentRepository) database() (*gorm.DB, error) {
	if r.db != nil {
		return r.db, nil
	}
	return assignmentDB()
}

func (r *customerTagAssignmentRepository) GetByCustomerAndTag(ctx context.Context, customerID, tag string) (*model.CustomerTagAssignment, error) {
	database, err := r.database()
	if err != nil {
		return nil, err
	}
	var assignment model.CustomerTagAssignment
	if err := database.First(&assignment, "customer_id = ? AND tag = ?", customerID, tag).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &assignment, nil
}

func (r *customerTagAssignmentRepository) ListByCustomerID(ctx context.Context, customerID string) ([]*model.CustomerTagAssignment, error) {
	database, err := r.database()
	if err != nil {
		return nil, err
	}
	var assignments []*model.CustomerTagAssignment
	if err := database.
		Where("customer_id = ?", customerID).
		Order("created_at").
		Find(&assignments).Error; err != nil {
		return nil, err
	}
	return assignments, nil
}

func (r *customerTagAssignmentRepository) Create(ctx context.Context, assignment *model.CustomerTagAssignment) error {
	database, err := r.database()
	if err != nil {
		return err
	}
	return database.Create(assignment).Error
}

func (r *customerTagAssignmentRepository) Update(ctx context.Context, assignment *model.CustomerTagAssignment) error {
	database, err := r.database()
	if err != nil {
		return err
	}
	return database.Save(assignment).Error
}

func (r *customerTagAssignmentRepository) DeleteByCustomerAndTag(ctx context.Context, customerID, tag string) error {
	database, err := r.database()
	if err != nil {
		return err
	}
	return database.Delete(&model.CustomerTagAssignment{}, "customer_id = ? AND tag = ?", customerID, tag).Error
}

func (r *customerTagAssignmentRepository) Upsert(ctx context.Context, assignment *model.CustomerTagAssignment) error {
	database, err := r.database()
	if err != nil {
		return err
	}
	return database.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "customer_id"},
			{Name: "tag"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"category": assignment.Category,
			"source":   assignment.Source,

			"confidence": gorm.Expr("GREATEST(customer_tag_assignments.confidence, ?)", assignment.Confidence),
		}),
	}).Create(assignment).Error
}

// ListCustomerIDsByTag 按标签取客户 ID（T-P5-01 圈选侧消费），按最近打标时间倒序。
//
// total 是"打过该标的总行数"，与 ids 是否被 limit 截断无关 —— 调用方要据此区分
// "这个标签根本没人打"（total=0）和"有人但一轮取不完"（len(ids)<total）。
func (r *customerTagAssignmentRepository) ListCustomerIDsByTag(ctx context.Context, tag string, limit int) ([]string, int64, error) {
	database, err := r.database()
	if err != nil {
		return nil, 0, err
	}
	if limit < 1 {
		limit = 100
	}
	var total int64
	if err := database.Model(&model.CustomerTagAssignment{}).
		Where("tag = ?", tag).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	if err := database.Model(&model.CustomerTagAssignment{}).
		Where("tag = ?", tag).
		Order("created_at DESC").
		Limit(limit).
		Pluck("customer_id", &ids).Error; err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}

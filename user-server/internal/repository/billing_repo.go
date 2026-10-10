package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/rbac"

	"gorm.io/gorm"
)

// billing_repo.go 计费三件套 + 角色-权限覆盖层仓储（L5）。

// BillingPlanRepo 计费方案仓储。
type BillingPlanRepo struct{ db *gorm.DB }

func NewBillingPlanRepo() *BillingPlanRepo { return &BillingPlanRepo{db: db.GetDB()} }

func (r *BillingPlanRepo) Available() bool { return r != nil && r.db != nil }

func (r *BillingPlanRepo) Create(ctx context.Context, p *model.BillingPlan) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *BillingPlanRepo) Update(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.BillingPlan{}).Where("id = ?", id).Updates(updates).Error
}

func (r *BillingPlanRepo) Delete(ctx context.Context, id uint) error {
	// 有订阅引用的方案不允许物理删（订阅还指着它）：调用方先查引用。
	return r.db.WithContext(ctx).Delete(&model.BillingPlan{}, id).Error
}

func (r *BillingPlanRepo) GetByID(ctx context.Context, id uint) (*model.BillingPlan, error) {
	var row model.BillingPlan
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *BillingPlanRepo) GetByCode(ctx context.Context, code string) (*model.BillingPlan, error) {
	var row model.BillingPlan
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *BillingPlanRepo) List(ctx context.Context) ([]model.BillingPlan, error) {
	var rows []model.BillingPlan
	err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// CountSubscriptions 方案被订阅数（删除前置校验）。
func (r *BillingPlanRepo) CountSubscriptions(ctx context.Context, planID uint) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.TeamSubscription{}).Where("plan_id = ?", planID).Count(&cnt).Error
	return cnt, err
}

// TeamSubscriptionRepo 团队订阅仓储。
type TeamSubscriptionRepo struct{ db *gorm.DB }

func NewTeamSubscriptionRepo() *TeamSubscriptionRepo {
	return &TeamSubscriptionRepo{db: db.GetDB()}
}

func (r *TeamSubscriptionRepo) Available() bool { return r != nil && r.db != nil }

func (r *TeamSubscriptionRepo) Upsert(ctx context.Context, sub *model.TeamSubscription) error {
	// 一团队一行：先查后插（量小，不做 ON CONFLICT 以保持 GORM 钩子语义简单）
	var existing model.TeamSubscription
	err := r.db.WithContext(ctx).Where("team_id = ?", sub.TeamID).First(&existing).Error
	if err == nil {
		return r.db.WithContext(ctx).Model(&model.TeamSubscription{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"plan_id": sub.PlanID, "plan_code": sub.PlanCode, "status": sub.Status,
			"starts_at": sub.StartsAt, "ends_at": sub.EndsAt, "auto_renew": sub.AutoRenew,
		}).Error
	}
	return r.db.WithContext(ctx).Create(sub).Error
}

func (r *TeamSubscriptionRepo) GetByTeam(ctx context.Context, teamID uint) (*model.TeamSubscription, error) {
	var row model.TeamSubscription
	if err := r.db.WithContext(ctx).Where("team_id = ?", teamID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *TeamSubscriptionRepo) List(ctx context.Context) ([]model.TeamSubscription, error) {
	var rows []model.TeamSubscription
	err := r.db.WithContext(ctx).Order("id DESC").Find(&rows).Error
	return rows, err
}

// CountActiveByPlan 用于方案删除前置校验的别名（语义同 CountSubscriptions）。
func (r *TeamSubscriptionRepo) CountActiveByPlan(ctx context.Context, planID uint) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.TeamSubscription{}).
		Where("plan_id = ? AND status = ?", planID, model.TeamSubStatusActive).Count(&cnt).Error
	return cnt, err
}

// BillingRecordRepo 账单仓储。
type BillingRecordRepo struct{ db *gorm.DB }

func NewBillingRecordRepo() *BillingRecordRepo { return &BillingRecordRepo{db: db.GetDB()} }

func (r *BillingRecordRepo) Available() bool { return r != nil && r.db != nil }

func (r *BillingRecordRepo) Create(ctx context.Context, row *model.BillingRecord) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *BillingRecordRepo) MarkPaid(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.BillingRecord{}).Where("id = ?", id).
		Updates(map[string]any{"status": model.BillingStatusPaid, "paid_at": at}).Error
}

func (r *BillingRecordRepo) Void(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.BillingRecord{}).Where("id = ?", id).
		Update("status", model.BillingStatusVoid).Error
}

// List 分页（teamID=0 全量；status 空=全部）。
func (r *BillingRecordRepo) List(ctx context.Context, teamID uint, status string, page, size int) ([]model.BillingRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.db.WithContext(ctx).Model(&model.BillingRecord{})
	if teamID > 0 {
		q = q.Where("team_id = ?", teamID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.BillingRecord
	err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// RolePermissionRepo 角色-权限覆盖层仓储（结构定义在 rbac 包：读写同表同语义）。
type RolePermissionRepo struct{ db *gorm.DB }

func NewRolePermissionRepo() *RolePermissionRepo { return &RolePermissionRepo{db: db.GetDB()} }

func (r *RolePermissionRepo) Available() bool { return r != nil && r.db != nil }

// ListByRole 角色的覆盖行（空 = 无覆盖，回落生成矩阵）。
func (r *RolePermissionRepo) ListByRole(ctx context.Context, roleCode string) ([]string, error) {
	var rows []rbac.RolePermission
	if err := r.db.WithContext(ctx).Where("role_code = ?", roleCode).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.PermissionCode)
	}
	return out, nil
}

// HasAnyOverride 角色是否存在覆盖行（执法层判"DB 优先还是生成为准"）。
func (r *RolePermissionRepo) HasAnyOverride(ctx context.Context, roleCode string) (bool, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&rbac.RolePermission{}).
		Where("role_code = ?", roleCode).Limit(1).Count(&cnt).Error
	return cnt > 0, err
}

// Replace 覆盖式保存角色的权限集合（先删后插，一次事务语义）。
func (r *RolePermissionRepo) Replace(ctx context.Context, roleCode string, permissionCodes []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_code = ?", roleCode).Delete(&rbac.RolePermission{}).Error; err != nil {
			return err
		}
		for _, code := range permissionCodes {
			row := rbac.RolePermission{RoleCode: roleCode, PermissionCode: code}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

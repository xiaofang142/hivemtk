package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	dbpkg "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// FollowUpReminderStore 跟进提醒持久仓储接口（FA-26/27）。
//
// 定义在 repository 包、服务包以接口消费：所有生产入口（HTTP 今日跟进、回款钩子、
// AI 工具）共享同一权威存储；重启/双副本后行仍在。租户隔离在仓储层按 ctx 过滤，
// 授权（操作者=负责人或管理员）在服务层判定。
type FollowUpReminderStore interface {
	Create(ctx context.Context, r *model.FollowUpReminder) error
	GetByID(ctx context.Context, id string) (*model.FollowUpReminder, error)
	Update(ctx context.Context, r *model.FollowUpReminder) error
	ListPending(ctx context.Context, ownerID string, limit int) ([]*model.FollowUpReminder, error)
	ListOverdue(ctx context.Context, ownerID string, now time.Time) ([]*model.FollowUpReminder, error)
	ListRange(ctx context.Context, ownerID string, start, end time.Time) ([]*model.FollowUpReminder, error)
}

// FollowUpReminderRepository gorm 实现；租户隔离口径：
//   - ctx 带租户（TENANT_MODE=on 的请求链/租户绑定的后台链）⇒ 读写都限定该租户；
//   - ctx 无租户（单租户默认档/系统内部链）⇒ 行按 default:1 落库、读不过滤
//     （与全仓 default:1 兜底口径一致，不因系统链缺租户而 fail 整条业务）。
type FollowUpReminderRepository struct{ db *gorm.DB }

func NewFollowUpReminderRepositoryWithDB(db *gorm.DB) *FollowUpReminderRepository {
	if db == nil {
		db = dbpkg.GetDB()
	}
	return &FollowUpReminderRepository{db: db}
}

func NewFollowUpReminderRepository() *FollowUpReminderRepository {
	return NewFollowUpReminderRepositoryWithDB(nil)
}

func (r *FollowUpReminderRepository) tenantID(ctx context.Context) (uint, bool) {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id, true
	}
	return 0, false
}

func (r *FollowUpReminderRepository) scoped(ctx context.Context) (*gorm.DB, uint, bool) {
	q := r.db.WithContext(ctx).Model(&model.FollowUpReminder{})
	if id, ok := r.tenantID(ctx); ok {
		return q.Where("tenant_id = ?", id), id, true
	}
	return q, 0, false
}

func (r *FollowUpReminderRepository) Create(ctx context.Context, row *model.FollowUpReminder) error {
	if row == nil {
		return errors.New("提醒为空")
	}
	if id, ok := r.tenantID(ctx); ok {
		row.TenantID = id
	}
	if row.Status == "" {
		row.Status = "pending"
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *FollowUpReminderRepository) GetByID(ctx context.Context, id string) (*model.FollowUpReminder, error) {
	q, _, _ := r.scoped(ctx)
	var row model.FollowUpReminder
	if err := q.Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *FollowUpReminderRepository) Update(ctx context.Context, row *model.FollowUpReminder) error {
	q, _, _ := r.scoped(ctx)
	// Update 走全字段 Save 前先确认行在当前租户范围内：scoped 已带租户条件，
	// 拿不到行 = 跨租户改写被这里拦下（返回 not found，不泄露存在性）。
	res := q.Where("id = ?", row.ID).Save(row)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *FollowUpReminderRepository) ListPending(ctx context.Context, ownerID string, limit int) ([]*model.FollowUpReminder, error) {
	q, _, _ := r.scoped(ctx)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	rows := []*model.FollowUpReminder{}
	err := q.Where("status = ?", "pending").Order("priority DESC, due_at ASC").Find(&rows).Error
	return rows, err
}

func (r *FollowUpReminderRepository) ListOverdue(ctx context.Context, ownerID string, now time.Time) ([]*model.FollowUpReminder, error) {
	q, _, _ := r.scoped(ctx)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	rows := []*model.FollowUpReminder{}
	err := q.Where("status = ? AND due_at < ?", "pending", now).Order("due_at ASC").Find(&rows).Error
	return rows, err
}

func (r *FollowUpReminderRepository) ListRange(ctx context.Context, ownerID string, start, end time.Time) ([]*model.FollowUpReminder, error) {
	q, _, _ := r.scoped(ctx)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	rows := []*model.FollowUpReminder{}
	err := q.Where("status = ? AND due_at >= ? AND due_at <= ?", "pending", start, end).
		Order("due_at ASC").Find(&rows).Error
	return rows, err
}

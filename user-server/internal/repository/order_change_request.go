package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// OrderChangeRequestRepository 改单申请单仓储（TB-05）。
// 租户作用域显式过滤（与 intent_tool_chain/custom_intent 同口径）。
type OrderChangeRequestRepository interface {
	Create(ctx context.Context, rec *model.OrderChangeRequest) error
	UpdateStatus(ctx context.Context, id uint, patch OrderChangeStatusPatch) error
	GetByID(ctx context.Context, id uint) (*model.OrderChangeRequest, error)
	GetByChangeNo(ctx context.Context, changeNo string) (*model.OrderChangeRequest, error)
	List(ctx context.Context, status string, page, pageSize int) ([]model.OrderChangeRequest, int64, error)
}

type orderChangeRequestRepo struct {
	db *gorm.DB
}

func NewOrderChangeRequestRepositoryWithDB(db *gorm.DB) OrderChangeRequestRepository {
	return &orderChangeRequestRepo{db: db}
}

// OrderChangeStatusPatch 状态迁移补丁（白名单字段，身份字段物理写不到）。
// OrderChangeStatusPatch 状态迁移补丁（白名单字段，身份字段物理写不到）。
// ExpectStatus 非空时启用 CAS（FA-09）：UPDATE 附加 `status = ExpectStatus` 条件，
// 并发迁移只有旧状态持有者能赢，输家拿到 ErrOrderChangeStatusConflict。
type OrderChangeStatusPatch struct {
	Status       string
	ApprovedBy   string
	ApprovedAt   *time.Time
	ExecutorNote string
	ExecutedBy   string
	ExecutedAt   *time.Time
	RejectedBy   string
	RejectReason string
	RejectedAt   *time.Time
	CancelledAt  *time.Time
	DispatchedBy string
	DispatchedAt *time.Time
	ExpectStatus string
}

// ErrOrderChangeStatusConflict CAS 落空：记录仍在但状态已被并发迁移。
var ErrOrderChangeStatusConflict = errors.New("order_change_request: 状态已被并发迁移（CAS 落空）")

func (p OrderChangeStatusPatch) toMap() map[string]any {
	m := map[string]any{"status": p.Status}
	if p.ApprovedBy != "" {
		m["approved_by"] = p.ApprovedBy
	}
	if p.ApprovedAt != nil {
		m["approved_at"] = p.ApprovedAt
	}
	if p.ExecutorNote != "" {
		m["executor_note"] = p.ExecutorNote
	}
	if p.ExecutedBy != "" {
		m["executed_by"] = p.ExecutedBy
	}
	if p.ExecutedAt != nil {
		m["executed_at"] = p.ExecutedAt
	}
	if p.RejectedBy != "" {
		m["rejected_by"] = p.RejectedBy
	}
	if p.RejectReason != "" {
		m["reject_reason"] = p.RejectReason
	}
	if p.RejectedAt != nil {
		m["rejected_at"] = p.RejectedAt
	}
	if p.CancelledAt != nil {
		m["cancelled_at"] = p.CancelledAt
	}
	if p.DispatchedBy != "" {
		m["dispatched_by"] = p.DispatchedBy
	}
	if p.DispatchedAt != nil {
		m["dispatched_at"] = p.DispatchedAt
	}
	return m
}

func (r *orderChangeRequestRepo) tenantScope(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (r *orderChangeRequestRepo) dbw(ctx context.Context) *gorm.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.OrderChangeRequest{}).
		Where("tenant_id = ?", r.tenantScope(ctx))
}

var errOrderChangeDBNotReady = errors.New("order_change_request repository: db not ready")

func (r *orderChangeRequestRepo) Create(ctx context.Context, rec *model.OrderChangeRequest) error {
	if r.db == nil {
		return errOrderChangeDBNotReady
	}
	if rec.TenantID == 0 {
		rec.TenantID = r.tenantScope(ctx)
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *orderChangeRequestRepo) UpdateStatus(ctx context.Context, id uint, patch OrderChangeStatusPatch) error {
	db := r.dbw(ctx)
	if db == nil {
		return errOrderChangeDBNotReady
	}
	if patch.ExpectStatus != "" {
		db = db.Where("status = ?", patch.ExpectStatus)
	}
	res := db.Where("id = ?", id).Updates(patch.toMap())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		if patch.ExpectStatus != "" {
			// 区分「记录不存在」与「状态已被并发迁移」
			var n int64
			if e := r.db.WithContext(ctx).Model(&model.OrderChangeRequest{}).
				Where("id = ?", id).Count(&n).Error; e == nil && n > 0 {
				return ErrOrderChangeStatusConflict
			}
		}
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *orderChangeRequestRepo) GetByID(ctx context.Context, id uint) (*model.OrderChangeRequest, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errOrderChangeDBNotReady
	}
	var rec model.OrderChangeRequest
	if err := db.Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *orderChangeRequestRepo) GetByChangeNo(ctx context.Context, changeNo string) (*model.OrderChangeRequest, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errOrderChangeDBNotReady
	}
	var rec model.OrderChangeRequest
	err := db.Where("change_no = ?", changeNo).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *orderChangeRequestRepo) List(ctx context.Context, status string, page, pageSize int) ([]model.OrderChangeRequest, int64, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, 0, errOrderChangeDBNotReady
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	var rows []model.OrderChangeRequest
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type CustomerImportJobRepository interface {
	Create(ctx context.Context, j *model.CustomerImportJob) error
	GetByJobID(ctx context.Context, jobID string) (*model.CustomerImportJob, error)
	List(ctx context.Context, page, pageSize int) ([]model.CustomerImportJob, int64, error)
	UpdateStatus(ctx context.Context, jobID, status string, total, success, failed, skipped, updated int, errReport string) error
	Update(ctx context.Context, j *model.CustomerImportJob) error
}

type customerImportJobRepository struct{}

func NewCustomerImportJobRepository() CustomerImportJobRepository {
	return &customerImportJobRepository{}
}
func (r *customerImportJobRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *customerImportJobRepository) Create(ctx context.Context, j *model.CustomerImportJob) error {
	if j.TenantID == 0 {
		j.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(j).Error
}

func (r *customerImportJobRepository) GetByJobID(ctx context.Context, jobID string) (*model.CustomerImportJob, error) {
	tid := tenant.WriteTenantID(ctx)
	var j model.CustomerImportJob
	err := r.db(ctx).Where("tenant_id=? AND job_id=?", tid, jobID).First(&j).Error
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *customerImportJobRepository) List(ctx context.Context, page, pageSize int) ([]model.CustomerImportJob, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	tid := tenant.WriteTenantID(ctx)
	q := r.db(ctx).Model(&model.CustomerImportJob{}).Where("tenant_id=?", tid)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.CustomerImportJob
	err := q.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *customerImportJobRepository) UpdateStatus(ctx context.Context, jobID, status string, total, success, failed, skipped, updated int, errReport string) error {
	tid := tenant.WriteTenantID(ctx)
	return r.db(ctx).Model(&model.CustomerImportJob{}).Where("tenant_id=? AND job_id=?", tid, jobID).
		Updates(map[string]any{
			"status":       status,
			"total_rows":   total,
			"success_rows": success,
			"failed_rows":  failed,
			"skipped_rows": skipped,
			"updated_rows": updated,
			"error_report": errReport,
		}).Error
}

func (r *customerImportJobRepository) Update(ctx context.Context, j *model.CustomerImportJob) error {
	tid := tenant.WriteTenantID(ctx)
	if j.TenantID == 0 {
		j.TenantID = tid
	}
	return r.db(ctx).Model(&model.CustomerImportJob{}).
		Where("tenant_id=? AND job_id=?", j.TenantID, j.JobID).
		Updates(map[string]any{
			"entity_type":     j.EntityType,
			"file_name":       j.FileName,
			"file_path":       j.FilePath,
			"status":          j.Status,
			"total_rows":      j.TotalRows,
			"success_rows":    j.SuccessRows,
			"failed_rows":     j.FailedRows,
			"skipped_rows":    j.SkippedRows,
			"updated_rows":    j.UpdatedRows,
			"dedup_strategy":  j.DedupStrategy,
			"mapping_json":    j.MappingJSON,
			"precheck_report": j.PrecheckReport,
			"error_report":    j.ErrorReport,
		}).Error
}

package model

import "time"

// CustomerImportJob 客户/联系人导入任务（PR-6/R-05）。
// Status: pending/prechecked/running/completed/failed
// DedupStrategy: skip/update/error
type CustomerImportJob struct {
	TenantID       uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID          string     `gorm:"type:varchar(32);not null;index" json:"job_id"`
	EntityType     string     `gorm:"type:varchar(32);not null;default:'customer'" json:"entity_type"`
	FileName       string     `gorm:"type:varchar(255);not null;default:''" json:"file_name"`
	FilePath       string     `gorm:"type:varchar(512);not null;default:''" json:"-"`
	Status         string     `gorm:"type:varchar(32);not null;default:'pending';index" json:"status"`
	TotalRows      int        `gorm:"not null;default:0" json:"total_rows"`
	SuccessRows    int        `gorm:"not null;default:0" json:"success_rows"`
	FailedRows     int        `gorm:"not null;default:0" json:"failed_rows"`
	SkippedRows    int        `gorm:"not null;default:0" json:"skipped_rows"`
	UpdatedRows    int        `gorm:"not null;default:0" json:"updated_rows"`
	DedupStrategy  string     `gorm:"type:varchar(32);not null;default:'skip'" json:"dedup_strategy"`
	MappingJSON    string     `gorm:"type:jsonb;not null;default:'{}'" json:"mapping_json"`
	PrecheckReport string     `gorm:"type:text;not null;default:''" json:"precheck_report,omitempty"`
	ErrorReport    string     `gorm:"type:text;not null;default:''" json:"error_report,omitempty"`
	CreatedBy      *uint      `json:"created_by,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (CustomerImportJob) TableName() string { return "customer_import_jobs" }

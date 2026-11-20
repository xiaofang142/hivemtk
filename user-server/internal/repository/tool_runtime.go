package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/toolruntime"

	"gorm.io/gorm"
)

var (
	ErrToolRuntimeUnavailable = toolruntime.ErrUnavailable
	ErrToolRuntimeDisabled    = toolruntime.ErrDisabled
	ErrToolRuntimeRevoked     = toolruntime.ErrRevoked
	ErrToolRuntimeChanged     = toolruntime.ErrChanged
)

// ToolRuntimeRepository 每次准入直接读取权威状态，不缓存撤销结果或凭据。
// 准入后已进入工具体的调用允许完成；管理提交后发起的新准入必须看到新状态。
type ToolRuntimeRepository struct{ db *gorm.DB }

func NewToolRuntimeRepository() *ToolRuntimeRepository {
	return NewToolRuntimeRepositoryWithDB(GetDB())
}

func NewToolRuntimeRepositoryWithDB(database *gorm.DB) *ToolRuntimeRepository {
	return &ToolRuntimeRepository{db: database}
}

// CheckToolAdmission 未配置的内置工具保留默认策略，缺表和查询失败绝不冒充未配置。
func (r *ToolRuntimeRepository) CheckToolAdmission(ctx context.Context, name string) error {
	if r == nil || r.db == nil || ctx == nil || tenant.WriteTenantID(ctx) == 0 || strings.TrimSpace(name) == "" {
		return ErrToolRuntimeUnavailable
	}
	var row model.AIToolConfig
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND tool_name = ?", tenant.WriteTenantID(ctx), name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return ErrToolRuntimeUnavailable
	}
	if !row.IsEnabled {
		return ErrToolRuntimeDisabled
	}
	return nil
}

// CheckCustomTool 验证来源身份及捕获的执行配置，防止同名重建复活旧实例。
func (r *ToolRuntimeRepository) CheckCustomTool(ctx context.Context, expected model.CustomTool, name string) error {
	if err := r.CheckToolAdmission(ctx, name); err != nil {
		return err
	}
	if expected.ID == 0 || expected.TenantID != tenant.WriteTenantID(ctx) {
		return ErrToolRuntimeRevoked
	}
	var row model.CustomTool
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", expected.TenantID, expected.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrToolRuntimeRevoked
	}
	if err != nil {
		return ErrToolRuntimeUnavailable
	}
	if !row.IsActive {
		return ErrToolRuntimeRevoked
	}
	if row.Name != expected.Name || !row.CreatedAt.Equal(expected.CreatedAt) || row.BaseURL != expected.BaseURL || row.SchemaJSON != expected.SchemaJSON || row.AuthHeader != expected.AuthHeader || row.AuthValueEnc != expected.AuthValueEnc || row.TimeoutMS != expected.TimeoutMS {
		return ErrToolRuntimeChanged
	}
	return nil
}

// CheckMCPServer 排除统计回写字段，只比较会改变实际远程执行的配置。
func (r *ToolRuntimeRepository) CheckMCPServer(ctx context.Context, expected model.MCPServer, name string) error {
	if err := r.CheckToolAdmission(ctx, name); err != nil {
		return err
	}
	if expected.ID == 0 || expected.TenantID != tenant.WriteTenantID(ctx) {
		return ErrToolRuntimeRevoked
	}
	var row model.MCPServer
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", expected.TenantID, expected.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrToolRuntimeRevoked
	}
	if err != nil {
		return ErrToolRuntimeUnavailable
	}
	if !row.Enabled {
		return ErrToolRuntimeRevoked
	}
	if row.Name != expected.Name || !row.CreatedAt.Equal(expected.CreatedAt) || row.Endpoint != expected.Endpoint || row.APIKeyEnc != expected.APIKeyEnc || row.TimeoutSeconds != expected.TimeoutSeconds || row.FailLimit != expected.FailLimit || row.CooldownSeconds != expected.CooldownSeconds {
		return ErrToolRuntimeChanged
	}
	return nil
}

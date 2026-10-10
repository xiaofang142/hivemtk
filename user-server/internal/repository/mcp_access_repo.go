package repository

import (
	"context"

	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// mcp_access_repo.go MCP 调用审计 / Skill 注册表仓储（L4，controller 禁直连 DB）
//
// 2026-10-10 新增：MCP 入口此前只有"签发了哪些凭证"，没有调用记录与 Skill 登记面。
// 这两张表把"谁用哪把凭证调了什么工具、成没成"与"有哪些 Skill、绑定哪把凭证、允许哪些工具"
// 收敛成可管理的记录（配合 mcp_guard 的凭证级 scope 过滤 + controller 层的审计写入）。

// MCPCallAuditRepo MCP 调用审计仓储。
type MCPCallAuditRepo struct{ db *gorm.DB }

func NewMCPCallAuditRepo() *MCPCallAuditRepo {
	return &MCPCallAuditRepo{db: db.GetDB()}
}

// Available 报告底层库句柄是否就绪。
// 分诊纪律（与 sales_workbench 503 三件套同源）："没装配"（503）与"装配了但查询报错"（500）
// 不是一件事。构造出的 repo 恒非 nil，判可用性必须下探到 db 句柄。
func (r *MCPCallAuditRepo) Available() bool { return r != nil && r.db != nil }

func (r *MCPCallAuditRepo) Create(ctx context.Context, row *model.MCPCallAudit) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(row).Error
}

// List 分页查询。过滤参数均可为空（不限定）；event 空串=不限（call/list/auth）。
func (r *MCPCallAuditRepo) List(ctx context.Context, clientID, tool string, allowed *bool, event string, page, size int) ([]model.MCPCallAudit, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.db.WithContext(ctx).Model(&model.MCPCallAudit{})
	if clientID != "" {
		q = q.Where("client_id = ?", clientID)
	}
	if tool != "" {
		q = q.Where("tool = ?", tool)
	}
	if allowed != nil {
		q = q.Where("allowed = ?", *allowed)
	}
	if event != "" {
		q = q.Where("event = ?", event)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.MCPCallAudit
	err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// MCPSkillRepo Skill 注册表仓储。
type MCPSkillRepo struct{ db *gorm.DB }

func NewMCPSkillRepo() *MCPSkillRepo {
	return &MCPSkillRepo{db: db.GetDB()}
}

// Available 见 MCPCallAuditRepo.Available（同一判据：db 句柄即服务就绪）。
func (r *MCPSkillRepo) Available() bool { return r != nil && r.db != nil }

func (r *MCPSkillRepo) List(ctx context.Context, limit int) ([]model.MCPSkill, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []model.MCPSkill
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *MCPSkillRepo) GetByID(ctx context.Context, id uint) (*model.MCPSkill, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var row model.MCPSkill
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *MCPSkillRepo) Create(ctx context.Context, row *model.MCPSkill) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *MCPSkillRepo) Update(ctx context.Context, id uint, updates map[string]any) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Model(&model.MCPSkill{}).Where("id = ?", id).Updates(updates).Error
}

func (r *MCPSkillRepo) Delete(ctx context.Context, id uint) error {
	if r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Delete(&model.MCPSkill{}, id).Error
}

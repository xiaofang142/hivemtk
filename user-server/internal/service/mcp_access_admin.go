package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// mcp_access_admin.go MCP 调用审计 / Skill 注册表管理服务（L4）
//
// 与 McpCredentialAdminService 同层：controller → service → repository。

// MCPCallAuditAdminService MCP 调用审计管理。
type MCPCallAuditAdminService struct {
	repo *repository.MCPCallAuditRepo
}

func NewMCPCallAuditAdminService() *MCPCallAuditAdminService {
	r := repository.NewMCPCallAuditRepo()
	if r == nil || !r.Available() {
		// 未装配（db 句柄缺失）：返回空服务，控制器判 Ready()=false 回 503，
		// 与"装配了但查询报错"（500）分开——见 repository.Available 注释的分诊纪律。
		return &MCPCallAuditAdminService{}
	}
	return &MCPCallAuditAdminService{repo: r}
}

func (s *MCPCallAuditAdminService) Ready() bool { return s.repo != nil && s.repo.Available() }

func (s *MCPCallAuditAdminService) List(ctx context.Context, clientID, tool string, allowed *bool, page, size int) ([]model.MCPCallAudit, int64, error) {
	return s.repo.List(ctx, clientID, tool, allowed, page, size)
}

// Record 追加一条审计（MCP 入口调用路径使用；失败只记日志不断工具调用）。
func (s *MCPCallAuditAdminService) Record(ctx context.Context, row *model.MCPCallAudit) {
	if s.repo == nil {
		return
	}
	_ = s.repo.Create(ctx, row) // best-effort：审计写失败不阻断 MCP 工具调用
}

// MCPSkillAdminService Skill 注册表管理。
type MCPSkillAdminService struct {
	repo *repository.MCPSkillRepo
}

func NewMCPSkillAdminService() *MCPSkillAdminService {
	r := repository.NewMCPSkillRepo()
	if r == nil || !r.Available() {
		return &MCPSkillAdminService{}
	}
	return &MCPSkillAdminService{repo: r}
}

func (s *MCPSkillAdminService) Ready() bool { return s.repo != nil && s.repo.Available() }

func (s *MCPSkillAdminService) List(ctx context.Context, limit int) ([]model.MCPSkill, error) {
	return s.repo.List(ctx, limit)
}

func (s *MCPSkillAdminService) GetByID(ctx context.Context, id uint) (*model.MCPSkill, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *MCPSkillAdminService) Create(ctx context.Context, row *model.MCPSkill) error {
	return s.repo.Create(ctx, row)
}

func (s *MCPSkillAdminService) Update(ctx context.Context, id uint, updates map[string]any) error {
	return s.repo.Update(ctx, id, updates)
}

func (s *MCPSkillAdminService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

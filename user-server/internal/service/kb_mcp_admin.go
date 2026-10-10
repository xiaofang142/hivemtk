package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// kb_mcp_admin.go 连接器/MCP 凭证管理服务（L3 controller → L4 service → L5 repository）

// ExternalKBConnectorAdminService 第三方知识库连接器管理。
type ExternalKBConnectorAdminService struct {
	repo *repository.ExternalKBConnectorRepo
}

func NewExternalKBConnectorAdminService() *ExternalKBConnectorAdminService {
	return &ExternalKBConnectorAdminService{repo: repository.NewExternalKBConnectorRepo()}
}

func (s *ExternalKBConnectorAdminService) Ready() bool { return s.repo != nil }

func (s *ExternalKBConnectorAdminService) List(ctx context.Context, limit int) ([]model.ExternalKBConnector, error) {
	return s.repo.List(ctx, limit)
}

func (s *ExternalKBConnectorAdminService) GetByID(ctx context.Context, id uint) (*model.ExternalKBConnector, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *ExternalKBConnectorAdminService) Create(ctx context.Context, row *model.ExternalKBConnector) error {
	return s.repo.Create(ctx, row)
}

func (s *ExternalKBConnectorAdminService) Update(ctx context.Context, id uint, updates map[string]any) error {
	return s.repo.Update(ctx, id, updates)
}

func (s *ExternalKBConnectorAdminService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// McpCredentialAdminService MCP 凭证对管理。
type McpCredentialAdminService struct {
	repo *repository.MCPCredentialRepo
}

func NewMcpCredentialAdminService() *McpCredentialAdminService {
	return &McpCredentialAdminService{repo: repository.NewMCPCredentialRepo()}
}

func (s *McpCredentialAdminService) Ready() bool { return s.repo != nil }

func (s *McpCredentialAdminService) List(ctx context.Context, limit int) ([]model.MCPCredential, error) {
	return s.repo.List(ctx, limit)
}

func (s *McpCredentialAdminService) Create(ctx context.Context, row *model.MCPCredential) error {
	return s.repo.Create(ctx, row)
}

func (s *McpCredentialAdminService) Update(ctx context.Context, id uint, updates map[string]any) error {
	return s.repo.Update(ctx, id, updates)
}

func (s *McpCredentialAdminService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

var mcpTouchMu sync.Mutex

// TouchLastUsed 回写最近使用（节流：每凭证 1 次/分钟）。
func (s *McpCredentialAdminService) TouchLastUsed(ctx context.Context, clientID string) {
	mcpTouchMu.Lock()
	defer mcpTouchMu.Unlock()
	_ = s.repo.TouchLastUsed(ctx, clientID, time.Now())
}

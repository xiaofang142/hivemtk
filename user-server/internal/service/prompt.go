package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// PromptService Prompt 版本管理 + A/B 实验业务层
type PromptService struct {
	repo *repository.PromptRepo
}

// NewPromptService 构造
func NewPromptService() *PromptService {
	return &PromptService{repo: repository.NewPromptRepo()}
}

// NewPromptServiceWithRepo 注入 repo（测试用）
func NewPromptServiceWithRepo(repo *repository.PromptRepo) *PromptService {
	return &PromptService{repo: repo}
}

// ListVersions 获取某个 SOP Node / Prompt ID 的所有历史版本
func (s *PromptService) ListVersions(ctx context.Context, idStr string, sopID uint, sopNodeID string, status string) ([]model.PromptCandidate, error) {
	return s.repo.ListVersions(ctx, idStr, sopID, sopNodeID, status)
}

// ListABTests 获取所有 Prompt A/B 实验列表
func (s *PromptService) ListABTests(ctx context.Context, status string) ([]model.PromptABTest, error) {
	return s.repo.ListABTests(ctx, status)
}

// Create 创建 Prompt 候选（模板 CRUD）
func (s *PromptService) Create(ctx context.Context, p *model.PromptCandidate) error {
	return s.repo.Create(ctx, p)
}

// GetByID 按 ID 查询 Prompt 候选
func (s *PromptService) GetByID(ctx context.Context, id uint) (*model.PromptCandidate, error) {
	return s.repo.GetByID(ctx, id)
}

// List 分页查询 Prompt 候选（支持 sop_node_id / sop_id / status 过滤）
func (s *PromptService) List(ctx context.Context, page, pageSize int, status, sopNodeID string, sopID uint) ([]model.PromptCandidate, int64, error) {
	return s.repo.List(ctx, page, pageSize, status, sopNodeID, sopID)
}

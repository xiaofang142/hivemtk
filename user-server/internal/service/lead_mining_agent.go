package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LeadMiningAgentService 「线索发现智能体」独立注册服务（FR-026，T047）。
//
// 设计口径（R8.3-B）：
//   - 不新建表；复用既有 ai_agents 表 / AIAgentRepository，以 agent_type='lead_mining' 子类型区分。
//   - 对外只暴露线索发现智能体的 CRUD，所有写操作强制把 agent_type 固定为 lead_mining，
//     避免调用方把销售型智能体挂进线索发现注册表。
//   - 列表/详情以 Status=-1 取全量（管理端要能看到停用项），关键字沿用仓储既有口径。
type LeadMiningAgentService struct {
	repo *repository.AIAgentRepository
}

// NewLeadMiningAgentServiceWithRepo 用显式仓储构造（装配点/测试用）；repo 为 nil 时方法走 fail-closed。
func NewLeadMiningAgentServiceWithRepo(repo *repository.AIAgentRepository) *LeadMiningAgentService {
	return &LeadMiningAgentService{repo: repo}
}

var errLeadMiningAgentStoreUnavailable = errors.New("线索发现智能体存储不可用")

// List 线索发现智能体列表（FR-026 列表）。
func (s *LeadMiningAgentService) List(ctx context.Context, keyword string) ([]*model.AIAgent, error) {
	if s == nil || s.repo == nil {
		return nil, errLeadMiningAgentStoreUnavailable
	}
	return s.repo.List(ctx, string(model.AgentTypeLeadMining), -1, strings.TrimSpace(keyword))
}

// Get 线索发现智能体详情；非本子类型的智能体返回 nil（fail-closed，不串给别类智能体）。
func (s *LeadMiningAgentService) Get(ctx context.Context, id uint) (*model.AIAgent, error) {
	if s == nil || s.repo == nil || id == 0 {
		return nil, errLeadMiningAgentStoreUnavailable
	}
	a, err := s.repo.GetByID(ctx, id)
	if err != nil || a == nil {
		return a, err
	}
	if !strings.EqualFold(a.AgentType, string(model.AgentTypeLeadMining)) {
		return nil, nil
	}
	return a, nil
}

// Create 新增线索发现智能体（FR-026 新增）。强制子类型 + 生成 agent_code + 必填兜底。
func (s *LeadMiningAgentService) Create(ctx context.Context, a *model.AIAgent) error {
	if s == nil || s.repo == nil {
		return errLeadMiningAgentStoreUnavailable
	}
	if a == nil || strings.TrimSpace(a.Name) == "" {
		return errors.New("线索发现智能体名称不能为空")
	}
	a.AgentType = string(model.AgentTypeLeadMining)
	a.AgentMode = normalizeAgentMode(a.AgentMode)
	a.AgentCode = strings.TrimSpace(a.AgentCode)
	if a.AgentCode == "" {
		a.AgentCode = fmt.Sprintf("lm_%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(a.Persona) == "" {
		a.Persona = "你是线索发现智能体：只从双方对话中识别商机、判定类型并归档，不参与自动回复。"
	}
	if a.Status == 0 {
		a.Status = 1
	}
	return s.repo.Create(ctx, a)
}

// Update 修改线索发现智能体（FR-026 修改）。强制子类型，不允许把本类改成别类。
func (s *LeadMiningAgentService) Update(ctx context.Context, a *model.AIAgent) error {
	if s == nil || s.repo == nil || a == nil || a.ID == 0 {
		return errLeadMiningAgentStoreUnavailable
	}
	existing, err := s.repo.GetByID(ctx, a.ID)
	if err != nil {
		return err
	}
	if existing == nil || !strings.EqualFold(existing.AgentType, string(model.AgentTypeLeadMining)) {
		return errors.New("线索发现智能体不存在")
	}
	a.AgentType = string(model.AgentTypeLeadMining)
	a.AgentCode = existing.AgentCode
	return s.repo.Update(ctx, a)
}

// Delete 删除线索发现智能体（FR-026 删除）。仅允许删本子类型。
func (s *LeadMiningAgentService) Delete(ctx context.Context, id uint) error {
	if s == nil || s.repo == nil || id == 0 {
		return errLeadMiningAgentStoreUnavailable
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil || !strings.EqualFold(existing.AgentType, string(model.AgentTypeLeadMining)) {
		return errors.New("线索发现智能体不存在")
	}
	return s.repo.Delete(ctx, id)
}

func normalizeAgentMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(model.AgentModeActive):
		return string(model.AgentModeActive)
	default:
		return string(model.AgentModePassive)
	}
}

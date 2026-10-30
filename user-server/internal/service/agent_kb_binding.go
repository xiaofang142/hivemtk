package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// AgentKBBindingService 智能体知识库绑定服务
//
// 写事务（覆盖 / 全量替换 / 批量覆盖）全部落在仓储：service 只做入参校验与错误文案，
// 不再持有 *gorm.DB，也就不存在"有没有开事务取决于调用方传了什么"这种二义性。
type AgentKBBindingService struct {
	bindingRepo *repository.AgentKBBindingRepository
}

// NewAgentKBBindingService 创建绑定服务（句柄由 repository 自取全局库）
func NewAgentKBBindingService() *AgentKBBindingService {
	return &AgentKBBindingService{bindingRepo: repository.NewAgentKBBindingRepositoryDefault()}
}

// NewAgentKBBindingServiceWithRepos 通过 repo 注入 (供测试和知识库 service 内部调用)
func NewAgentKBBindingServiceWithRepos(bindRepo *repository.AgentKBBindingRepository) *AgentKBBindingService {
	return &AgentKBBindingService{bindingRepo: bindRepo}
}

// Bind 单个绑定 (重复自动覆盖)
//
// 业务规则:
//   - agentID 必填
//   - kbID    必填且必须存在
//   - 同一 (agent_id, kb_id) 已存在则覆盖 priority / enabled
//   - 默认 enabled = true
func (s *AgentKBBindingService) Bind(ctx context.Context, agentID, kbID uint, priority int) error {
	if s.bindingRepo == nil {
		return errors.New("binding repo not initialized")
	}
	if agentID == 0 {
		return errors.New("agent_id 不能为空")
	}
	if kbID == 0 {
		return errors.New("knowledge_base_id 不能为空")
	}
	if err := s.bindingRepo.BindOverwrite(ctx, agentID, kbID, priority); err != nil {
		// 仓储已返回带 kb_id 的可读文案（"知识库 123 不存在"），再包一层只会变成
		// "知识库不存在: 知识库 123 不存在"，故原样透出。
		return err
	}
	return nil
}

// Unbind 单个解绑
func (s *AgentKBBindingService) Unbind(ctx context.Context, agentID, kbID uint) error {
	if s.bindingRepo == nil {
		return errors.New("binding repo not initialized")
	}
	exists, err := s.bindingRepo.CheckExists(ctx, agentID, kbID)
	if err != nil {
		return err
	}
	if !exists {
		// 隔离条件挡掉 0 行时若不探测，跨租户解绑会返回「解绑成功」而实际未动任何数据。
		return gorm.ErrRecordNotFound
	}
	return s.bindingRepo.DeleteByAgentAndKB(ctx, agentID, kbID)
}

// ListByAgent 查某智能体的所有绑定
func (s *AgentKBBindingService) ListByAgent(ctx context.Context, agentID uint) ([]*model.AgentKBBinding, error) {
	if s.bindingRepo == nil {
		return nil, nil
	}
	bindings, err := s.bindingRepo.ListByAgent(ctx, agentID, "")
	if err != nil {
		return nil, err
	}
	out := make([]*model.AgentKBBinding, len(bindings))
	for i := range bindings {
		b := bindings[i]
		out[i] = &b
	}
	return out, nil
}

// ListByKB 查某知识库被哪些智能体绑定
func (s *AgentKBBindingService) ListByKB(ctx context.Context, kbID uint) ([]*model.AgentKBBinding, error) {
	if s.bindingRepo == nil {
		return nil, nil
	}
	bindings, err := s.bindingRepo.ListByKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	out := make([]*model.AgentKBBinding, len(bindings))
	for i := range bindings {
		b := bindings[i]
		out[i] = &b
	}
	return out, nil
}

// ReplaceByAgent 全量替换某智能体的知识库挂载（编辑页保存场景）
//
// 行为:
//   - 先删除该智能体全部已有绑定, 再按 kbIDs 重新绑定 (replace 语义)
//   - 任一 kbID 对应的知识库不存在则整体失败并回滚
//   - kbIDs 为空: 等价于清空该智能体的所有挂载
func (s *AgentKBBindingService) ReplaceByAgent(ctx context.Context, agentID uint, kbIDs []uint) error {
	if s.bindingRepo == nil {
		return errors.New("binding repo not initialized")
	}
	if agentID == 0 {
		return errors.New("agent_id 不能为空")
	}
	// kbID=0 视为"空槽"丢弃（前端未选中时会回传 0），其余原样交给仓储
	ids := make([]uint, 0, len(kbIDs))
	for _, kbID := range kbIDs {
		if kbID != 0 {
			ids = append(ids, kbID)
		}
	}
	return s.bindingRepo.ReplaceByAgent(ctx, agentID, ids)
}

// BatchBindItem 批量绑定参数项
type BatchBindItem struct {
	AgentID  uint `json:"agent_id"`
	KBID     uint `json:"knowledge_base_id"`
	Priority int  `json:"priority"`
}

// BatchBind 批量绑定 (事务; 全部成功或全部失败)
//
// 行为:
//   - 同一 (agent_id, kb_id) 重复: 覆盖
//   - 任一条失败: 整体回滚
//   - items 为空: 直接返回 nil (不视为错误)
func (s *AgentKBBindingService) BatchBind(ctx context.Context, items []BatchBindItem) error {
	if len(items) == 0 {
		return nil
	}
	if s.bindingRepo == nil {
		return errors.New("binding repo not initialized")
	}
	for i, it := range items {
		if it.AgentID == 0 {
			return fmt.Errorf("items[%d]: agent_id 不能为空", i)
		}
		if it.KBID == 0 {
			return fmt.Errorf("items[%d]: knowledge_base_id 不能为空", i)
		}
	}
	targets := make([]repository.BindingTarget, 0, len(items))
	for _, it := range items {
		targets = append(targets, repository.BindingTarget{
			AgentID:  it.AgentID,
			KBID:     it.KBID,
			Priority: it.Priority,
		})
	}
	return s.bindingRepo.BatchBindOverwrite(ctx, targets)
}

func boolPtr(b bool) *bool { return &b }

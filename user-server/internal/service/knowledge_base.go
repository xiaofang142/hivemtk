package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// KnowledgeBaseService 知识库业务服务
type KnowledgeBaseService struct {
	repo        *repository.KnowledgeBaseRepository
	bindingRepo *repository.AgentKBBindingRepository
}

// IsValidKBType 校验知识库类型字段是否合法 (faq / rag / sop)
//
// 工具方法: 不依赖 repo/db, 业务层可直接调用. 抽离出 Repository 以避免
// 架构检查 (check-architecture.sh) 对"无 ctx 工具方法"的误报.
func IsValidKBType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t == model.KnowledgeBaseTypeFAQ ||
		t == model.KnowledgeBaseTypeRAG ||
		t == model.KnowledgeBaseTypeSOP
}

// NewKnowledgeBaseServiceDefault 使用全局 DB 创建知识库服务（controller 层入口，避免 controller 持有 gorm.DB）。
func NewKnowledgeBaseServiceDefault() *KnowledgeBaseService {
	return NewKnowledgeBaseService(dbUtil.GetDB())
}

// NewKnowledgeBaseService 创建知识库服务
func NewKnowledgeBaseService(db *gorm.DB) *KnowledgeBaseService {
	return &KnowledgeBaseService{
		repo:        repository.NewKnowledgeBaseRepository(db),
		bindingRepo: repository.NewAgentKBBindingRepository(db),
	}
}

// SetRepositories 注入 repository (用于测试)
func (s *KnowledgeBaseService) SetRepositories(kbRepo *repository.KnowledgeBaseRepository, bindRepo *repository.AgentKBBindingRepository) {
	if kbRepo != nil {
		s.repo = kbRepo
	}
	if bindRepo != nil {
		s.bindingRepo = bindRepo
	}
}

// CreateKB 创建知识库
//
// 业务规则:
//   - name 必填
//   - type 必填且为 faq/rag/sop
//   - owner_type=private 时 owner_agent_id 必填, 自动创建 agent_kb_bindings 绑定
//   - owner_type=shared  时 owner_agent_id 必为空
func (s *KnowledgeBaseService) CreateKB(ctx context.Context, kb *model.KnowledgeBase) error {
	if s.repo == nil {
		return errors.New("repo not initialized")
	}
	if kb == nil {
		return errors.New("kb is nil")
	}
	kb.Name = strings.TrimSpace(kb.Name)
	if kb.Name == "" {
		return fmt.Errorf("%w: name 不能为空", utils.ErrInvalidInput)
	}
	kb.Type = strings.ToLower(strings.TrimSpace(kb.Type))
	if !IsValidKBType(kb.Type) {
		return fmt.Errorf("type 非法: %s (faq/rag/sop)", kb.Type)
	}
	kb.OwnerType = strings.ToLower(strings.TrimSpace(kb.OwnerType))
	if kb.OwnerType == "" {
		kb.OwnerType = model.KnowledgeBaseOwnerPrivate
	}
	var ownerAgentID uint
	switch kb.OwnerType {
	case model.KnowledgeBaseOwnerPrivate:
		if kb.OwnerAgentID == nil || *kb.OwnerAgentID == 0 {
			return errors.New("owner_type=private 时 owner_agent_id 必填")
		}
		ownerAgentID = *kb.OwnerAgentID
	case model.KnowledgeBaseOwnerShared:
		if kb.OwnerAgentID != nil && *kb.OwnerAgentID != 0 {
			return errors.New("owner_type=shared 时 owner_agent_id 必为空")
		}
		kb.OwnerAgentID = nil
	default:
		return fmt.Errorf("owner_type 非法: %s (private/shared)", kb.OwnerType)
	}
	if kb.Enabled == nil {
		t := true
		kb.Enabled = &t
	}
	// 新 KB 从 1 号起。实测这条对 Create 路径是**冗余**的：GORM 会按 `gorm:"default:1"` 标签
	// 在客户端填值（把列默认值 DROP 掉之后只走仓储 Create 仍落 1，三条探针见
	// knowledge_base_version_start_test.go 文件头）。保留它的理由是："零值→1"这条业务规则
	// 不该寄望在某个库的内部行为上，也不该让 version=0 出现在管理端（读起来像"这库被切过版本"）。
	// 只管零值：调用方显式给的版本号一律不动。
	if kb.Version == 0 {
		kb.Version = 1
	}

	if ownerAgentID > 0 {
		binding := &model.AgentKBBinding{
			AgentID: ownerAgentID,
			KBID:    kb.ID,
			KBType:  kb.Type,
			Role:    model.AgentKBBindingRolePrimary,
			Enabled: boolPtr(true),
		}
		return s.repo.CreateWithBinding(ctx, kb, binding)
	}
	return s.repo.Create(ctx, kb)
}

// GetKB 按 ID 查询
func (s *KnowledgeBaseService) GetKB(ctx context.Context, id uint) (*model.KnowledgeBase, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.GetByID(ctx, id)
}

// GetKBStats 知识库统计（前端 KBDrawer 抽屉: item_count/agent_count/hit_count）
func (s *KnowledgeBaseService) GetKBStats(ctx context.Context, id uint) (map[string]int64, error) {
	kb, err := s.GetKB(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, nil
	}
	stats := map[string]int64{
		"item_count":  int64(kb.DocCount),
		"agent_count": 0,
		"hit_count":   0,
	}
	if s.bindingRepo != nil {
		bindings, err := s.bindingRepo.ListByKB(ctx, id)
		if err != nil {
			return nil, err
		}
		stats["agent_count"] = int64(len(bindings))
	}
	return stats, nil
}

// ListKBs 列表查询
func (s *KnowledgeBaseService) ListKBs(ctx context.Context, kbType, ownerType string, agentID uint, keyword string) ([]*model.KnowledgeBase, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	filter := repository.KBListFilter{
		Type:      kbType,
		OwnerType: ownerType,
	}
	if agentID > 0 {
		aid := agentID
		filter.OwnerAgentID = &aid
	}
	kbs, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	ptrs := make([]*model.KnowledgeBase, len(kbs))
	for i := range kbs {
		ptrs[i] = &kbs[i]
	}
	if keyword != "" {
		like := strings.ToLower(strings.TrimSpace(keyword))
		filtered := make([]*model.KnowledgeBase, 0, len(ptrs))
		for _, kb := range ptrs {
			if strings.Contains(strings.ToLower(kb.Name), like) || strings.Contains(strings.ToLower(kb.Description), like) {
				filtered = append(filtered, kb)
			}
		}
		return filtered, int64(len(filtered)), nil
	}
	return ptrs, total, nil
}

// ListByType 按类型查询
func (s *KnowledgeBaseService) ListByType(ctx context.Context, kbType string) ([]*model.KnowledgeBase, error) {
	if s.repo == nil {
		return nil, nil
	}
	kbs, err := s.repo.ListByType(ctx, kbType, 0)
	if err != nil {
		return nil, err
	}
	out := make([]*model.KnowledgeBase, len(kbs))
	for i := range kbs {
		out[i] = &kbs[i]
	}
	return out, nil
}

// ListByAgent 查某智能体可用的知识库 (shared + agent 私有)
func (s *KnowledgeBaseService) ListByAgent(ctx context.Context, agentID uint) ([]*model.KnowledgeBase, error) {
	if s.repo == nil {
		return nil, nil
	}
	kbs, err := s.repo.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	out := make([]*model.KnowledgeBase, len(kbs))
	for i := range kbs {
		out[i] = &kbs[i]
	}
	return out, nil
}

// UpdateKB 更新知识库 (支持部分更新)
//
// 业务规则:
//   - 先加载现有记录, 仅覆盖请求中显式提供的字段
//   - owner_type=private 时 owner_agent_id 必填 (最终校验)
//   - owner_type=shared  时 owner_agent_id 必为空 (最终校验)
func (s *KnowledgeBaseService) UpdateKB(ctx context.Context, id uint, kb *model.KnowledgeBase) error {
	if s.repo == nil {
		return errors.New("repo not initialized")
	}
	if id == 0 {
		return errors.New("id 不能为空")
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return errors.New("知识库不存在")
	}

	merged := mergeKBForUpdate(existing, kb)

	if merged.Name == "" {
		return fmt.Errorf("%w: name 不能为空", utils.ErrInvalidInput)
	}
	if merged.Type != "" && !IsValidKBType(merged.Type) {
		return fmt.Errorf("%w: type 非法: %s", utils.ErrInvalidInput, merged.Type)
	}

	prevOwnerID := uint(0)
	if existing.OwnerAgentID != nil {
		prevOwnerID = *existing.OwnerAgentID
	}
	if strings.ToLower(strings.TrimSpace(kb.OwnerType)) == model.KnowledgeBaseOwnerShared {
		merged.OwnerAgentID = nil
	}

	ot := strings.ToLower(strings.TrimSpace(merged.OwnerType))
	switch ot {
	case model.KnowledgeBaseOwnerPrivate:
		if merged.OwnerAgentID == nil || *merged.OwnerAgentID == 0 {
			return errors.New("owner_type=private 时 owner_agent_id 必填")
		}
	case model.KnowledgeBaseOwnerShared:
		if merged.OwnerAgentID != nil && *merged.OwnerAgentID != 0 {
			return errors.New("owner_type=shared 时 owner_agent_id 必为空")
		}
		merged.OwnerAgentID = nil
	}

	if err := s.repo.Update(ctx, id, merged); err != nil {
		return err
	}

	if ot == model.KnowledgeBaseOwnerShared && prevOwnerID > 0 && s.bindingRepo != nil {
		if err := s.bindingRepo.DeleteByAgentAndKB(ctx, prevOwnerID, id); err != nil {
			return fmt.Errorf("移除原 owner 绑定失败: %w", err)
		}
	}
	return nil
}

func mergeKBForUpdate(dst *model.KnowledgeBase, src *model.KnowledgeBase) *model.KnowledgeBase {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Name != "" {
		dst.Name = src.Name
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.OwnerType != "" {
		dst.OwnerType = src.OwnerType
	}
	if src.OwnerAgentID != nil {
		dst.OwnerAgentID = src.OwnerAgentID
	}
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if src.MemberCount != 0 {
		dst.MemberCount = src.MemberCount
	}
	if src.DocCount != 0 {
		dst.DocCount = src.DocCount
	}
	return dst
}

// DeleteKB 删除知识库 (业务级联: 同步删除所有 agent_kb_bindings)
func (s *KnowledgeBaseService) DeleteKB(ctx context.Context, id uint) error {
	if s.repo == nil {
		return errors.New("repo not initialized")
	}
	if err := s.bindingRepo.DeleteByKB(ctx, id); err != nil {
		return fmt.Errorf("删除知识库绑定失败: %w", err)
	}
	return s.repo.Delete(ctx, id)
}

// BindToAgent 入口 (供控制器调用), 内部委托 AgentKBBindingService
func (s *KnowledgeBaseService) BindToAgent(ctx context.Context, kbID, agentID uint) error {
	bindingSvc := NewAgentKBBindingServiceWithRepos(s.repo, s.bindingRepo, dbUtil.GetDB())
	return bindingSvc.Bind(ctx, agentID, kbID, 0)
}

// UnbindFromAgent 从智能体解绑知识库
func (s *KnowledgeBaseService) UnbindFromAgent(ctx context.Context, kbID, agentID uint) error {
	if s.bindingRepo == nil {
		return nil
	}
	return s.bindingRepo.DeleteByAgentAndKB(ctx, agentID, kbID)
}

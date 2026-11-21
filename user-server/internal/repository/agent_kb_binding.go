package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// bindingListLimit 绑定列表返回上限：单个智能体/知识库的绑定数量远小于此，
// 但不封顶就是无界查询。
const bindingListLimit = 500

// AgentKBBindingRepository 智能体知识库绑定仓储
type AgentKBBindingRepository struct {
	db *gorm.DB
}

// NewAgentKBBindingRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
func NewAgentKBBindingRepositoryDefault() *AgentKBBindingRepository {
	return NewAgentKBBindingRepository(GetDB())
}

func NewAgentKBBindingRepository(db *gorm.DB) *AgentKBBindingRepository {
	return &AgentKBBindingRepository{db: db}
}

// Create 新增绑定；UNIQUE(tenant_id, agent_id, kb_id) 约束下重复绑定会返回错误。
//
// 不用 Select("*")：它会把 nil 的 Enabled(*bool) 当 NULL 写进 NOT NULL 列，
// 而普通 Create 会省略 nil 指针、让列默认值 true 生效。
func (r *AgentKBBindingRepository) Create(ctx context.Context, b *model.AgentKBBinding) error {
	return r.db.WithContext(ctx).Create(b).Error
}

// DeleteByAgentKB 按 (agent_id, kb_id) 删除
func (r *AgentKBBindingRepository) DeleteByAgentKB(ctx context.Context, agentID, kbID uint) error {
	return r.db.WithContext(ctx).
		Where("agent_id = ? AND kb_id = ?", agentID, kbID).
		Delete(&model.AgentKBBinding{}).Error
}

// DeleteByAgentAndKB 按 (agent_id, kb_id) 删除 (DeleteByAgentKB 的别名, 兼容老 service 调用)
func (r *AgentKBBindingRepository) DeleteByAgentAndKB(ctx context.Context, agentID, kbID uint) error {
	return r.DeleteByAgentKB(ctx, agentID, kbID)
}

// DeleteByAgent 删除某智能体的所有绑定 (业务级联: 智能体删除时调用)
func (r *AgentKBBindingRepository) DeleteByAgent(ctx context.Context, agentID uint) error {
	if agentID == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Delete(&model.AgentKBBinding{}).Error
}

// DeleteByKB 删除某知识库的所有绑定 (业务级联: 知识库删除时调用)
func (r *AgentKBBindingRepository) DeleteByKB(ctx context.Context, kbID uint) error {
	if kbID == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("kb_id = ?", kbID).
		Delete(&model.AgentKBBinding{}).Error
}

// ListByAgent 列出某智能体的全部绑定 (按 priority DESC 排序)
// kbType 为空时不过滤类型
func (r *AgentKBBindingRepository) ListByAgent(ctx context.Context, agentID uint, kbType string) ([]model.AgentKBBinding, error) {
	q := r.db.WithContext(ctx).Where("agent_id = ? AND enabled = ?", agentID, true)
	if kbType != "" {
		q = q.Where("kb_type = ?", kbType)
	}
	var bindings []model.AgentKBBinding
	err := q.Order("priority DESC, id DESC").Limit(bindingListLimit).Find(&bindings).Error
	return bindings, err
}

// ListByAgentAll 列出某智能体的全部绑定 (不限制 enabled, 不过滤 kb_type, 业务级联删除用)
func (r *AgentKBBindingRepository) ListByAgentAll(ctx context.Context, agentID uint) ([]model.AgentKBBinding, error) {
	var bindings []model.AgentKBBinding
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("priority DESC, id DESC").
		Limit(bindingListLimit).
		Find(&bindings).Error
	return bindings, err
}

// ListByKB 列出引用某 KB 的全部智能体绑定
func (r *AgentKBBindingRepository) ListByKB(ctx context.Context, kbID uint) ([]model.AgentKBBinding, error) {
	var bindings []model.AgentKBBinding
	err := r.db.WithContext(ctx).
		Where("kb_id = ? AND enabled = ?", kbID, true).
		Order("priority DESC, id DESC").
		Limit(bindingListLimit).
		Find(&bindings).Error
	return bindings, err
}

// CheckExists 检查 (agent, kb) 是否已绑定
func (r *AgentKBBindingRepository) CheckExists(ctx context.Context, agentID, kbID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AgentKBBinding{}).
		Where("agent_id = ? AND kb_id = ?", agentID, kbID).
		Count(&count).Error
	return count > 0, err
}

// 绑定域的写事务（覆盖 / 全量替换 / 批量覆盖）
//
// 事务边界落在仓储而不是 service：这三个动作都是"先删后建"，中途失败必须整体回滚，
// 否则绑定会凭空消失。三个动作永远在事务里执行。
//
// kb_type 一律读知识库的真实类型再落冗余列：写死 faq 会让按类型过滤（ListByAgent 的
// kbType 参数）把 rag/sop 库整批漏掉。

// ErrKnowledgeBaseNotFound 绑定时引用的知识库不存在。
//
// 用独立哨兵而不是回 gorm.ErrRecordNotFound：调用方要把它翻成"知识库不存在"这类
// 用户可见文案，而不是把"记录没找到"透出去。
var ErrKnowledgeBaseNotFound = errors.New("知识库不存在")

// KnowledgeBaseNotFoundError 带上 kb_id 的"知识库不存在"。
//
// 用自定义类型而不是 fmt.Errorf 拼 id：既保证 errors.Is(ErrKnowledgeBaseNotFound)
// 可分支处理，又不让文案里同一句话说两遍。
type KnowledgeBaseNotFoundError struct {
	ID uint
}

func (e *KnowledgeBaseNotFoundError) Error() string {
	return fmt.Sprintf("知识库 %d 不存在", e.ID)
}

func (e *KnowledgeBaseNotFoundError) Is(target error) bool {
	return target == ErrKnowledgeBaseNotFound
}

// BindingTarget 批量绑定的单条入参：(agent_id, kb_id) 定位，priority 为覆盖后的优先级。
type BindingTarget struct {
	AgentID  uint
	KBID     uint
	Priority int
}

// ErrAgentNotFound 绑定时引用的智能体不存在（含跨租户不可见，隔离闸吞行后同样查无）。
//
// 与 ErrKnowledgeBaseNotFound 同款哨兵：调用方要"智能体 %d 不存在"的用户可见文案，
// 而不是把 gorm.ErrRecordNotFound 透出去。
var ErrAgentNotFound = errors.New("智能体不存在")

// AgentNotFoundError 带上 agent_id 的"智能体不存在"。
type AgentNotFoundError struct {
	ID uint
}

func (e *AgentNotFoundError) Error() string {
	return fmt.Sprintf("智能体 %d 不存在", e.ID)
}

func (e *AgentNotFoundError) Is(target error) bool {
	return target == ErrAgentNotFound
}

// resolveAgentExists 与 resolveKBType 同层同风格：写事务开头校验智能体对当前租户可见。
//
// 为什么必须有：写路径此前只校验 KB，跨租户会话可对任意 agent_id 播种本租户绑定行
// （Replace 还会先 DeleteByAgent——租户过滤保住别人数据，但幽灵行照落），违反
// 多租户 fail-closed（缺归属校验不得放行写）。软删（DeletedAt）与租户隔离 callback
// 与 KB 校验同口径生效。
func resolveAgentExists(ctx context.Context, tx *gorm.DB, agentID uint) error {
	var agent model.AIAgent
	if err := tx.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &AgentNotFoundError{ID: agentID}
		}
		return err
	}
	return nil
}

// resolveKBType 读知识库并返回其类型；不存在返回 ErrKnowledgeBaseNotFound。
func resolveKBType(ctx context.Context, kbs *KnowledgeBaseRepository, kbID uint) (string, error) {
	kb, err := kbs.GetByID(ctx, kbID)
	if err != nil {
		return "", err
	}
	if kb == nil {
		return "", &KnowledgeBaseNotFoundError{ID: kbID}
	}
	return kb.Type, nil
}

// newBinding 组装一条绑定行（enabled 默认 true；nil 指针会被 Create 省略、走列默认值）。
func newBinding(agentID, kbID uint, kbType string, priority int) *model.AgentKBBinding {
	enabled := true
	return &model.AgentKBBinding{
		AgentID:  agentID,
		KBID:     kbID,
		KBType:   kbType,
		Role:     model.AgentKBBindingRolePrimary,
		Priority: priority,
		Enabled:  &enabled,
	}
}

// BindOverwrite 覆盖式绑定单个 (agent_id, kb_id)。
func (r *AgentKBBindingRepository) BindOverwrite(ctx context.Context, agentID, kbID uint, priority int) error {
	if r.db == nil {
		return errors.New("binding repo not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewAgentKBBindingRepository(tx)
		if err := resolveAgentExists(ctx, tx, agentID); err != nil {
			return err
		}
		kbType, err := resolveKBType(ctx, NewKnowledgeBaseRepository(tx), kbID)
		if err != nil {
			return err
		}
		if err := txRepo.DeleteByAgentAndKB(ctx, agentID, kbID); err != nil {
			return fmt.Errorf("清除旧绑定失败: %w", err)
		}
		if err := txRepo.Create(ctx, newBinding(agentID, kbID, kbType, priority)); err != nil {
			return fmt.Errorf("创建绑定失败: %w", err)
		}
		return nil
	})
}

// ReplaceByAgent 全量替换某智能体的知识库挂载（编辑页保存场景）。
//
// kbIDs 为空等价于清空；其中任一知识库不存在则整体回滚——半套挂载比没有更难排查。
func (r *AgentKBBindingRepository) ReplaceByAgent(ctx context.Context, agentID uint, kbIDs []uint) error {
	if r.db == nil {
		return errors.New("binding repo not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewAgentKBBindingRepository(tx)
		txKB := NewKnowledgeBaseRepository(tx)
		if err := resolveAgentExists(ctx, tx, agentID); err != nil {
			return err
		}
		if err := txRepo.DeleteByAgent(ctx, agentID); err != nil {
			return fmt.Errorf("清除旧绑定失败: %w", err)
		}
		for _, kbID := range kbIDs {
			if kbID == 0 {
				continue
			}
			kbType, err := resolveKBType(ctx, txKB, kbID)
			if err != nil {
				return err
			}
			if err := txRepo.Create(ctx, newBinding(agentID, kbID, kbType, 0)); err != nil {
				return fmt.Errorf("创建绑定失败: %w", err)
			}
		}
		return nil
	})
}

// BatchBindOverwrite 批量覆盖绑定（全部成功或全部失败）。
func (r *AgentKBBindingRepository) BatchBindOverwrite(ctx context.Context, targets []BindingTarget) error {
	if r.db == nil {
		return errors.New("binding repo not initialized")
	}
	if len(targets) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewAgentKBBindingRepository(tx)
		txKB := NewKnowledgeBaseRepository(tx)
		checked := make(map[uint]struct{}, len(targets))
		for _, t := range targets {
			if _, done := checked[t.AgentID]; !done {
				if err := resolveAgentExists(ctx, tx, t.AgentID); err != nil {
					return fmt.Errorf("items[agent=%d]: %w", t.AgentID, err)
				}
				checked[t.AgentID] = struct{}{}
			}
			kbType, err := resolveKBType(ctx, txKB, t.KBID)
			if err != nil {
				return fmt.Errorf("items[agent=%d]: %w", t.AgentID, err)
			}
			if err := txRepo.DeleteByAgentAndKB(ctx, t.AgentID, t.KBID); err != nil {
				return fmt.Errorf("items[agent=%d kb=%d]: 清除旧绑定失败: %w", t.AgentID, t.KBID, err)
			}
			if err := txRepo.Create(ctx, newBinding(t.AgentID, t.KBID, kbType, t.Priority)); err != nil {
				return fmt.Errorf("items[agent=%d kb=%d]: 创建绑定失败: %w", t.AgentID, t.KBID, err)
			}
		}
		return nil
	})
}

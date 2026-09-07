package repository

import (
	"context"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// AIAgentRepository AI 智能体仓库
type AIAgentRepository struct {
	db *gorm.DB
}

// NewAIAgentRepository 创建智能体仓库
func NewAIAgentRepository() *AIAgentRepository {
	return &AIAgentRepository{}
}

// SetDB 注入 db（用于测试和外部依赖注入）
func (r *AIAgentRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// GetDB 获取 db（内部使用）
func (r *AIAgentRepository) GetDB(ctx context.Context) *gorm.DB {
	return r.db
}

// Create 创建智能体
func (r *AIAgentRepository) Create(ctx context.Context, agent *model.AIAgent) error {
	return r.db.Create(agent).Error
}

// CountChannelBindingsByAgent 统计智能体被渠道账号绑定的数量
func (r *AIAgentRepository) CountChannelBindingsByAgent(ctx context.Context, agentID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ChannelAgentBinding{}).
		Where("agent_id = ?", agentID).Count(&n).Error
	return n, err
}

// CountServiceMountsByAgent 统计智能体被客服座席挂载的数量
func (r *AIAgentRepository) CountServiceMountsByAgent(ctx context.Context, agentID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerServiceAgent{}).
		Where("ai_agent_id = ?", agentID).Count(&n).Error
	return n, err
}

// GetByID 根据 ID 获取
func (r *AIAgentRepository) GetByID(ctx context.Context, id uint) (*model.AIAgent, error) {
	var a model.AIAgent
	if err := r.db.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// GetByCode 根据 agent_code 获取
func (r *AIAgentRepository) GetByCode(ctx context.Context, code string) (*model.AIAgent, error) {
	var a model.AIAgent
	if err := r.db.Where("agent_code = ?", code).First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// List 列表查询（支持类型/状态/关键字筛选）
func (r *AIAgentRepository) List(ctx context.Context, agentType string, status int, keyword string) ([]*model.AIAgent, error) {
	q := r.db.Model(&model.AIAgent{})
	if agentType != "" {
		q = q.Where("agent_type = ?", agentType)
	}
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR agent_code LIKE ? OR description LIKE ?", like, like, like)
	}
	var list []*model.AIAgent
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListEnabled 获取所有启用的智能体
func (r *AIAgentRepository) ListEnabled(ctx context.Context) ([]*model.AIAgent, error) {
	var list []*model.AIAgent
	if err := r.db.Where("status = ?", 1).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Update 更新智能体
func (r *AIAgentRepository) Update(ctx context.Context, agent *model.AIAgent) error {
	return r.db.Save(agent).Error
}

// UpdateStatus 更新状态
func (r *AIAgentRepository) UpdateStatus(ctx context.Context, id uint, status int) error {
	return r.db.Model(&model.AIAgent{}).Where("id = ?", id).Update("status", status).Error
}

// Delete 删除智能体
// 注意：关联的 channel_agent_bindings 和 customer_service_agents 由数据库外键 ON DELETE CASCADE 自动级联删除
func (r *AIAgentRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.AIAgent{}, id).Error
}

// CountByIDs 按ID集合统计数量（用于校验绑定挂载合法性）
func (r *AIAgentRepository) CountByIDs(ctx context.Context, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var count int64
	if err := r.db.Model(&model.AIAgent{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ChannelAgentBindingRepository 渠道绑定仓库
type ChannelAgentBindingRepository struct {
	db *gorm.DB
}

// NewChannelAgentBindingRepository 创建渠道绑定仓库
func NewChannelAgentBindingRepository() *ChannelAgentBindingRepository {
	return &ChannelAgentBindingRepository{}
}

// SetDB 注入 db
func (r *ChannelAgentBindingRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建绑定
func (r *ChannelAgentBindingRepository) Create(ctx context.Context, b *model.ChannelAgentBinding) error {
	return r.db.Create(b).Error
}

// ReplacePrimaryBinding 事务替换渠道账号主绑定: 清除旧主绑定 + 创建新主绑定
//
// 任一步骤失败整体回滚; 与 channel_agent_bindings 的 uq_channel_account_primary
// 部分唯一索引配合, 即使并发也只有一条主绑定能落地。
func (r *ChannelAgentBindingRepository) ReplacePrimaryBinding(ctx context.Context, channelType, accountID string, agentID uint) (*model.ChannelAgentBinding, error) {
	var created *model.ChannelAgentBinding
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tmpRepo := NewChannelAgentBindingRepository()
		tmpRepo.SetDB(ctx, tx)
		if err := tmpRepo.ClearPrimaryByChannelAccount(ctx, channelType, accountID); err != nil {
			return err
		}
		b := &model.ChannelAgentBinding{
			ChannelType: channelType,
			AccountID:   accountID,
			AgentID:     agentID,
			IsPrimary:   true,
			Enabled:     true,
		}
		if err := tmpRepo.Create(ctx, b); err != nil {
			return err
		}
		created = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetByID 根据 ID 获取
func (r *ChannelAgentBindingRepository) GetByID(ctx context.Context, id uint) (*model.ChannelAgentBinding, error) {
	var b model.ChannelAgentBinding
	if err := r.db.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// ListByChannelAccount 按渠道+账号ID查询所有绑定
func (r *ChannelAgentBindingRepository) ListByChannelAccount(ctx context.Context, channelType, accountID string) ([]*model.ChannelAgentBinding, error) {
	var list []*model.ChannelAgentBinding
	if err := r.db.Where("channel_type = ? AND account_id = ?", channelType, accountID).
		Order("is_primary DESC, id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetPrimaryByChannelAccount 按渠道+账号ID查询主绑定
// 返回 nil, nil 表示未绑定（不视为错误）
func (r *ChannelAgentBindingRepository) GetPrimaryByChannelAccount(ctx context.Context, channelType, accountID string) (*model.ChannelAgentBinding, error) {
	var b model.ChannelAgentBinding
	err := r.db.WithContext(ctx).Where("channel_type = ? AND account_id = ? AND is_primary = ? AND enabled = ?",
		channelType, accountID, true, true).First(&b).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// ListByAgentID 反查智能体被哪些渠道账号使用
func (r *ChannelAgentBindingRepository) ListByAgentID(ctx context.Context, agentID uint) ([]*model.ChannelAgentBinding, error) {
	var list []*model.ChannelAgentBinding
	if err := r.db.Where("agent_id = ?", agentID).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ResolveBinding 按优先级路由查找渠道+账号+会话的最佳绑定
//
// 匹配优先级（从高到低）：
//  1. chat_id 精确匹配  + priority DESC（最具体）
//  2. chat_id IS NULL（account 级默认）+ priority DESC
//  3. is_primary=true 兜底
//
// 返回 nil, nil 表示未绑定
//
// 实现策略：两次参数化查询（先精确后默认），避免 OR 条件导致 Seq Scan + 消除 SQL 注入风险。
func (r *ChannelAgentBindingRepository) ResolveBinding(ctx context.Context, channelType, accountID, chatID string) (*model.ChannelAgentBinding, error) {
	// Step 1: chat_id 精确匹配 + 高 priority
	if chatID != "" {
		var b model.ChannelAgentBinding
		err := r.db.WithContext(ctx).
			Where("channel_type = ? AND account_id = ? AND chat_id = ? AND enabled = ?",
				channelType, accountID, chatID, true).
			Order("priority DESC").
			Order("is_primary DESC").
			Order("id DESC").
			Limit(1).
			First(&b).Error
		if err == nil {
			return &b, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	// Step 2: chat_id IS NULL 的 account 级默认绑定
	{
		var b model.ChannelAgentBinding
		err := r.db.WithContext(ctx).
			Where("channel_type = ? AND account_id = ? AND chat_id IS NULL AND enabled = ?",
				channelType, accountID, true).
			Order("priority DESC").
			Order("is_primary DESC").
			Order("id DESC").
			Limit(1).
			First(&b).Error
		if err == nil {
			return &b, nil
		}
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
}

// GetBindingByChatID 按渠道+账号+chat_id 精确查询（不含默认绑定）
func (r *ChannelAgentBindingRepository) GetBindingByChatID(ctx context.Context, channelType, accountID, chatID string) (*model.ChannelAgentBinding, error) {
	var b model.ChannelAgentBinding
	err := r.db.WithContext(ctx).Where(
		"channel_type = ? AND account_id = ? AND chat_id = ? AND enabled = ?",
		channelType, accountID, chatID, true,
	).First(&b).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// Update 更新绑定
func (r *ChannelAgentBindingRepository) Update(ctx context.Context, b *model.ChannelAgentBinding) error {
	return r.db.Save(b).Error
}

// Delete 删除绑定
func (r *ChannelAgentBindingRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.ChannelAgentBinding{}, id).Error
}

// ClearPrimaryByChannelAccount 清除指定渠道账号下的所有主绑定标记
// 用于切换主智能体时先清空再设置
func (r *ChannelAgentBindingRepository) ClearPrimaryByChannelAccount(ctx context.Context, channelType, accountID string) error {
	return r.db.Model(&model.ChannelAgentBinding{}).
		Where("channel_type = ? AND account_id = ?", channelType, accountID).
		Update("is_primary", false).Error
}

// DeleteByChannelAccount 删除指定渠道账号下的所有绑定
func (r *ChannelAgentBindingRepository) DeleteByChannelAccount(ctx context.Context, channelType, accountID string) error {
	return r.db.Where("channel_type = ? AND account_id = ?", channelType, accountID).
		Delete(&model.ChannelAgentBinding{}).Error
}

// CustomerServiceAgentRepository 客服挂载仓库
type CustomerServiceAgentRepository struct {
	db *gorm.DB
}

// NewCustomerServiceAgentRepository 创建客服挂载仓库
func NewCustomerServiceAgentRepository() *CustomerServiceAgentRepository {
	return &CustomerServiceAgentRepository{}
}

// SetDB 注入 db
func (r *CustomerServiceAgentRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建挂载
func (r *CustomerServiceAgentRepository) Create(ctx context.Context, c *model.CustomerServiceAgent) error {
	return r.db.Create(c).Error
}

// GetByID 根据 ID 获取
func (r *CustomerServiceAgentRepository) GetByID(ctx context.Context, id uint) (*model.CustomerServiceAgent, error) {
	var c model.CustomerServiceAgent
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByAgentStatusID 按客服座席ID查询所有挂载
func (r *CustomerServiceAgentRepository) ListByAgentStatusID(ctx context.Context, agentStatusID uint) ([]*model.CustomerServiceAgent, error) {
	var list []*model.CustomerServiceAgent
	if err := r.db.Where("agent_status_id = ?", agentStatusID).
		Order("is_primary DESC, id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetPrimaryByAgentStatusID 按座席ID查询主挂载
// 返回 nil, nil 表示未挂载
func (r *CustomerServiceAgentRepository) GetPrimaryByAgentStatusID(ctx context.Context, agentStatusID uint) (*model.CustomerServiceAgent, error) {
	var c model.CustomerServiceAgent
	err := r.db.Where("agent_status_id = ? AND is_primary = ? AND enabled = ?",
		agentStatusID, true, true).First(&c).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// ListByAIAgentID 反查智能体被哪些客服使用
func (r *CustomerServiceAgentRepository) ListByAIAgentID(ctx context.Context, aiAgentID uint) ([]*model.CustomerServiceAgent, error) {
	var list []*model.CustomerServiceAgent
	if err := r.db.Where("ai_agent_id = ?", aiAgentID).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Update 更新挂载
func (r *CustomerServiceAgentRepository) Update(ctx context.Context, c *model.CustomerServiceAgent) error {
	return r.db.Save(c).Error
}

// Delete 删除挂载
func (r *CustomerServiceAgentRepository) Delete(ctx context.Context, id uint) error {
	return r.db.Delete(&model.CustomerServiceAgent{}, id).Error
}

// ClearPrimaryByAgentStatusID 清除指定座席下所有主挂载标记
func (r *CustomerServiceAgentRepository) ClearPrimaryByAgentStatusID(ctx context.Context, agentStatusID uint) error {
	return r.db.Model(&model.CustomerServiceAgent{}).
		Where("agent_status_id = ?", agentStatusID).
		Update("is_primary", false).Error
}

// DeleteByAgentStatusID 删除指定座席下的所有挂载
func (r *CustomerServiceAgentRepository) DeleteByAgentStatusID(ctx context.Context, agentStatusID uint) error {
	return r.db.Where("agent_status_id = ?", agentStatusID).
		Delete(&model.CustomerServiceAgent{}).Error
}

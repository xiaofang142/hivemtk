package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// AgentContext 智能体执行上下文
// 已迁移至 dto 包，此处保留类型别名以维持向后兼容
// 注意：Go 不允许为类型别名（type X = Y）定义方法，相关方法必须挂在 dto 包中
type AgentContext = dto.AgentContext

// AIAgentService AI 智能体服务
type AIAgentService struct {
	repo *repository.AIAgentRepository
	db   *gorm.DB

	cacheMu  sync.RWMutex
	cache    map[uint]*agentCacheEntry
	cacheTTL time.Duration
}

type agentCacheEntry struct {
	ctx       *AgentContext
	expiresAt time.Time
}

// NewAIAgentService 创建智能体服务(无参,内部用 dbUtil.GetDB())
func NewAIAgentService() *AIAgentService {
	return NewAIAgentServiceWithDB(dbUtil.GetDB())
}

// NewAIAgentServiceWithDB 创建带 DB 的智能体服务(显式注入 db,兼容旧调用)
func NewAIAgentServiceWithDB(db *gorm.DB) *AIAgentService {
	repo := repository.NewAIAgentRepository()
	repo.SetDB(context.Background(), db)
	return &AIAgentService{
		repo:     repo,
		db:       db,
		cache:    make(map[uint]*agentCacheEntry),
		cacheTTL: 30 * time.Second,
	}
}

// SetRepository 注入 repository（用于测试）
func (s *AIAgentService) SetRepository(ctx context.Context, repo *repository.AIAgentRepository) {
	if repo != nil {
		s.repo = repo
	}
}

// Create 创建智能体
func (s *AIAgentService) Create(ctx context.Context, a *model.AIAgent) error {
	if a.AgentCode == "" {
		return errors.New("agent_code 不能为空")
	}
	if a.Name == "" {
		return errors.New("name 不能为空")
	}
	if a.Persona == "" {
		return errors.New("persona 不能为空")
	}
	if a.AgentType == "" {
		a.AgentType = string(model.AgentTypeSales)
	}
	if existing, _ := s.repo.GetByCode(ctx, a.AgentCode); existing != nil {
		return fmt.Errorf("agent_code %s 已存在", a.AgentCode)
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return err
	}
	s.invalidateCache(ctx, a.ID)
	return nil
}

// GetByID 获取智能体详情
func (s *AIAgentService) GetByID(ctx context.Context, id uint) (*model.AIAgent, error) {
	return s.repo.GetByID(ctx, id)
}

// List 列表查询
func (s *AIAgentService) List(ctx context.Context, agentType string, status int, keyword string) ([]*model.AIAgent, error) {
	return s.repo.List(ctx, agentType, status, keyword)
}

// ListEnabled 获取所有启用的智能体（供下拉选择）
func (s *AIAgentService) ListEnabled(ctx context.Context) ([]*model.AIAgent, error) {
	return s.repo.ListEnabled(ctx)
}

// Update 更新智能体
func (s *AIAgentService) Update(ctx context.Context, a *model.AIAgent) error {
	if a.ID == 0 {
		return errors.New("id 不能为空")
	}
	if a.Name == "" {
		return errors.New("name 不能为空")
	}
	if a.Persona == "" {
		return errors.New("persona 不能为空")
	}
	a.Version = a.Version + 1
	if err := s.repo.Update(ctx, a); err != nil {
		return err
	}
	s.invalidateCache(ctx, a.ID)
	return nil
}

// UpdateStatus 更新状态
func (s *AIAgentService) UpdateStatus(ctx context.Context, id uint, status int) error {
	if err := s.repo.UpdateStatus(ctx, id, status); err != nil {
		return err
	}
	s.invalidateCache(ctx, id)
	return nil
}

// Delete 删除智能体
// 业务约束：若智能体被渠道绑定或客服挂载引用，应先解绑
func (s *AIAgentService) Delete(ctx context.Context, id uint) error {

	bindings, _ := s.repo.CountChannelBindingsByAgent(ctx, id)
	if bindings > 0 {
		return fmt.Errorf("智能体被 %d 个渠道账号绑定，请先解绑", bindings)
	}
	mounts, _ := s.repo.CountServiceMountsByAgent(ctx, id)
	if mounts > 0 {
		return fmt.Errorf("智能体被 %d 个客服座席挂载，请先解挂", mounts)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateCache(ctx, id)
	return nil
}

// LoadContext 加载智能体执行上下文（带缓存）
// 返回 nil, nil 表示未找到（不视为错误，调用方回退默认配置）
func (s *AIAgentService) LoadContext(ctx context.Context, agentID uint) (*AgentContext, error) {
	if agentID == 0 {
		return nil, nil
	}
	s.cacheMu.RLock()
	if entry, ok := s.cache[agentID]; ok && time.Now().Before(entry.expiresAt) {
		s.cacheMu.RUnlock()
		return entry.ctx, nil
	}
	s.cacheMu.RUnlock()

	agent, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	if agent.Status != 1 {
		return nil, nil
	}

	agentCtx := &AgentContext{
		AgentID:              agent.ID,
		AgentCode:            agent.AgentCode,
		Name:                 agent.Name,
		AgentType:            agent.AgentType,
		Persona:              agent.Persona,
		SystemPrompt:         agent.SystemPrompt,
		Greeting:             agent.Greeting,
		RagProductIDs:        []string(agent.RagProductIDs),
		FAQEntryIDs:          []string(agent.FAQEntryIDs),
		SOPTemplateIDs:       []string(agent.SOPTemplateIDs),
		SOPIDs:               []string(agent.SOPIDs),
		ScriptLibraryIDs:     []string(agent.ScriptLibraryIDs),
		LLMModel:             agent.LLMModel,
		LLMProviderConfig:    LLMProviderConfigToDTO(agent.LLMProviderConfig),
		Temperature:          agent.Temperature,
		MaxTokens:            agent.MaxTokens,
		TopP:                 agent.TopP,
		FrequencyPenalty:     agent.FrequencyPenalty,
		PresencePenalty:      agent.PresencePenalty,
		EnableRAG:            agent.EnableRAG,
		EnableScriptMatch:    agent.EnableScriptMatch,
		EnableHumanizePolish: agent.EnableHumanizePolish,
		EnableContentAudit:   agent.EnableContentAudit,
		EnablePlaybook:       agent.EnablePlaybook,
		RAGTopK:              agent.RAGTopK,
		ConfidenceThreshold:  agent.ConfidenceThreshold,
		MaxAIConsecutive:     agent.MaxAIConsecutive,
		AssetBundleID:        agent.AssetBundleID,
	}

	s.cacheMu.Lock()
	s.cache[agentID] = &agentCacheEntry{
		ctx:       agentCtx,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()

	return agentCtx, nil
}

func (s *AIAgentService) invalidateCache(ctx context.Context, agentID uint) {
	s.cacheMu.Lock()
	delete(s.cache, agentID)
	s.cacheMu.Unlock()
}

// BindAssetBundle 将资产包（含平台同步的 local_assets.AssetID）绑定到智能体。
//
// 闭合「平台下发资产包 → 商户同步落库 → 绑定到智能体 → 运行时消费」全链路：
// 绑定后运行期 ResolveSystemPrompt(asset_bundle_id) 会回退解析该本地资产，
// 像 seed/自建资产包一样覆盖智能体 Persona。
func (s *AIAgentService) BindAssetBundle(ctx context.Context, agentID uint, assetBundleID string) error {
	agent, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	agent.AssetBundleID = assetBundleID
	if err := s.repo.Update(ctx, agent); err != nil {
		return err
	}
	s.invalidateCache(ctx, agentID)
	return nil
}

// TestAgent 测试智能体执行
// 输入消息和客户ID，返回 SalesEngine 完整链路日志
// 此方法在 Controller 层调用，由 Controller 注入 SalesEngine
func (s *AIAgentService) TestAgent(ctx context.Context, agentID uint, customerID, message string, engine *SalesEngine) (*SalesResponse, error) {
	agentCtx, err := s.LoadContext(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("加载智能体上下文失败: %w", err)
	}
	if agentCtx == nil {
		return nil, errors.New("智能体不存在或已禁用")
	}
	if engine == nil {
		return nil, errors.New("SalesEngine 未注入")
	}
	req := &SalesRequest{
		CustomerID:  customerID,
		UserMessage: message,
		AutoExecute: true,
	}
	return engine.HandleWithAgent(ctx, req, agentCtx)
}

// ChannelAgentBindingService 渠道绑定服务
type ChannelAgentBindingService struct {
	repo      *repository.ChannelAgentBindingRepository
	agentRepo *repository.AIAgentRepository
	agentSvc  *AIAgentService
	db        *gorm.DB
}

// NewChannelAgentBindingService 创建渠道绑定服务(无参,内部用 dbUtil.GetDB())
func NewChannelAgentBindingService() *ChannelAgentBindingService {
	return NewChannelAgentBindingServiceWithDB(dbUtil.GetDB(), NewAIAgentService())
}

// NewChannelAgentBindingServiceWithDB 创建带 DB 的渠道绑定服务(显式注入 db,兼容旧调用)
func NewChannelAgentBindingServiceWithDB(db *gorm.DB, agentSvc *AIAgentService) *ChannelAgentBindingService {
	repo := repository.NewChannelAgentBindingRepository()
	repo.SetDB(context.Background(), db)
	agentRepo := repository.NewAIAgentRepository()
	agentRepo.SetDB(context.Background(), db)
	return &ChannelAgentBindingService{
		repo:      repo,
		agentRepo: agentRepo,
		agentSvc:  agentSvc,
		db:        db,
	}
}

// Create 创建绑定
// 业务约束：agentID 必须存在且启用；若 is_primary=true，需先清除同账号其他主绑定
func (s *ChannelAgentBindingService) Create(ctx context.Context, b *model.ChannelAgentBinding) error {
	if b.ChannelType == "" {
		return errors.New("channel_type 不能为空")
	}
	if b.AccountID == "" {
		return errors.New("account_id 不能为空")
	}
	if b.AgentID == 0 {
		return errors.New("agent_id 不能为空")
	}
	agent, err := s.agentRepo.GetByID(ctx, b.AgentID)
	if err != nil {
		return fmt.Errorf("智能体不存在: %w", err)
	}
	if agent.Status != 1 {
		return errors.New("智能体已禁用，无法绑定")
	}
	if b.IsPrimary {
		if err := s.repo.ClearPrimaryByChannelAccount(ctx, b.ChannelType, b.AccountID); err != nil {
			return fmt.Errorf("清除旧主绑定失败: %w", err)
		}
	}
	return s.repo.Create(ctx, b)
}

// ListByChannelAccount 按渠道账号查询所有绑定
func (s *ChannelAgentBindingService) ListByChannelAccount(ctx context.Context, channelType, accountID string) ([]*model.ChannelAgentBinding, error) {
	return s.repo.ListByChannelAccount(ctx, channelType, accountID)
}

// ListByAgentID 反查智能体被哪些渠道使用
func (s *ChannelAgentBindingService) ListByAgentID(ctx context.Context, agentID uint) ([]*model.ChannelAgentBinding, error) {
	return s.repo.ListByAgentID(ctx, agentID)
}

// GetByID 获取绑定详情
func (s *ChannelAgentBindingService) GetByID(ctx context.Context, id uint) (*model.ChannelAgentBinding, error) {
	return s.repo.GetByID(ctx, id)
}

// Update 更新绑定
func (s *ChannelAgentBindingService) Update(ctx context.Context, b *model.ChannelAgentBinding) error {
	if b.IsPrimary {
		existing, _ := s.repo.GetByID(ctx, b.ID)
		if existing != nil && (existing.ChannelType != b.ChannelType || existing.AccountID != b.AccountID) {
			if err := s.repo.ClearPrimaryByChannelAccount(ctx, b.ChannelType, b.AccountID); err != nil {
				return err
			}
		} else if existing != nil {
			if err := s.repo.ClearPrimaryByChannelAccount(ctx, existing.ChannelType, existing.AccountID); err != nil {
				return err
			}
		}
	}
	if err := s.repo.Update(ctx, b); err != nil {
		return err
	}
	return nil
}

// Delete 删除绑定
func (s *ChannelAgentBindingService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// ReplaceBinding 强 1对1 替换主绑定 (Task 21)
//
// 业务语义: "把渠道账号 (channel_type, account_id) 当前的主绑定智能体换成 agentID"
//
// 行为:
//   - 同一事务内: 清除该 (channel_type, account_id) 下所有 is_primary=true 记录
//   - 创建新主绑定 (is_primary=true, enabled=true, agent_id=agentID)
//   - 任一步骤失败: 整体回滚, 数据库状态保持不变
//
// 适用场景:
//   - 渠道账号第一次绑定智能体
//   - 渠道账号切换绑定的智能体
//   - 与 channel_agent_bindings 表的 uq_channel_account_primary 部分唯一索引
//     配合, 即使并发也只有一条主绑定能落地
func (s *ChannelAgentBindingService) ReplaceBinding(ctx context.Context, channelType, accountID string, agentID uint) (*model.ChannelAgentBinding, error) {
	if channelType == "" {
		return nil, errors.New("channel_type 不能为空")
	}
	if accountID == "" {
		return nil, errors.New("account_id 不能为空")
	}
	if agentID == 0 {
		return nil, errors.New("agent_id 不能为空")
	}
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("智能体不存在: %w", err)
	}
	if agent.Status != 1 {
		return nil, errors.New("智能体已禁用, 无法绑定")
	}
	if s.db == nil {
		return nil, errors.New("db 未初始化, 事务不可用")
	}
	return s.repo.ReplacePrimaryBinding(ctx, channelType, accountID, agentID)
}

// LoadAgentForChannel 加载渠道账号绑定的主智能体上下文
// WebhookService.triggerSalesEngine 调用此方法
// 返回 nil, nil 表示未绑定（调用方回退默认配置）
func (s *ChannelAgentBindingService) LoadAgentForChannel(ctx context.Context, channelType, accountID string) (*AgentContext, error) {
	binding, err := s.repo.GetPrimaryByChannelAccount(ctx, channelType, accountID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, nil
	}
	return s.agentSvc.LoadContext(ctx, binding.AgentID)
}

func NormalizeChannelType(ch string) string {
	ch = strings.ToLower(strings.TrimSpace(ch))
	switch ch {
	case "telegram", "tg":
		return string(model.ChannelTypeTelegram)
	case "qq", "qqbot":
		return string(model.ChannelTypeQQ)
	case "wecom", "wechat_work", "wechatwork":
		return string(model.ChannelTypeWeCom)
	case "feishu", "lark":
		return string(model.ChannelTypeFeishu)
	case "whatsapp", "wa":
		return string(model.ChannelTypeWhatsApp)
	case "dingtalk", "dt":
		return string(model.ChannelTypeDingTalk)
	case "douyin", "douyin_web":
		return string(model.ChannelTypeDouyin)
	case "xiaohongshu", "xhs", "xhs_web":
		return string(model.ChannelTypeXiaohongshu)
	case "kuaishou", "ks", "kuaishou_web":
		return string(model.ChannelTypeKuaishou)
	case "xianyu", "xianyu_web":
		return string(model.ChannelTypeXianyu)
	case "tiktok", "tiktok_web":
		return string(model.ChannelTypeTikTok)
	case "web_embed", "webembed":
		return string(model.ChannelTypeWebEmbed)
	default:
		return ch
	}
}

// CustomerServiceAgentService 客服挂载服务
type CustomerServiceAgentService struct {
	repo            *repository.CustomerServiceAgentRepository
	agentRepo       *repository.AIAgentRepository
	agentSvc        *AIAgentService
	agentStatusRepo *repository.AgentStatusRepository
}

// NewCustomerServiceAgentService 创建客服挂载服务(无参,内部用 dbUtil.GetDB())
func NewCustomerServiceAgentService() *CustomerServiceAgentService {
	return NewCustomerServiceAgentServiceWithDB(dbUtil.GetDB(), NewAIAgentService())
}

// NewCustomerServiceAgentServiceWithDB 创建带 DB 的客服挂载服务(显式注入 db,兼容旧调用)
func NewCustomerServiceAgentServiceWithDB(db *gorm.DB, agentSvc *AIAgentService) *CustomerServiceAgentService {
	repo := repository.NewCustomerServiceAgentRepository()
	repo.SetDB(context.Background(), db)
	agentRepo := repository.NewAIAgentRepository()
	agentRepo.SetDB(context.Background(), db)
	agentStatusRepo := repository.NewAgentStatusRepositoryWithDB(db)
	return &CustomerServiceAgentService{
		repo:            repo,
		agentRepo:       agentRepo,
		agentSvc:        agentSvc,
		agentStatusRepo: agentStatusRepo,
	}
}

// NewCustomerServiceAgentServiceViaPort 通过 Port 模式创建客服挂载服务
//
// 注意：所有持久化操作走 repo（含 agentStatusRepo）。
func NewCustomerServiceAgentServiceViaPort(agentSvc *AIAgentService) *CustomerServiceAgentService {
	return NewCustomerServiceAgentServiceWithDB(dbUtil.GetDB(), agentSvc)
}

// Create 创建挂载
func (s *CustomerServiceAgentService) Create(ctx context.Context, c *model.CustomerServiceAgent) error {
	if c.AgentStatusID == 0 {
		return errors.New("agent_status_id 不能为空")
	}
	if c.AIAgentID == 0 {
		return errors.New("ai_agent_id 不能为空")
	}
	agent, err := s.agentRepo.GetByID(ctx, c.AIAgentID)
	if err != nil {
		return fmt.Errorf("智能体不存在: %w", err)
	}
	if agent.Status != 1 {
		return errors.New("智能体已禁用，无法挂载")
	}
	if c.IsPrimary {
		if err := s.repo.ClearPrimaryByAgentStatusID(ctx, c.AgentStatusID); err != nil {
			return fmt.Errorf("清除旧主挂载失败: %w", err)
		}
	}
	return s.repo.Create(ctx, c)
}

// ListByAgentStatusID 按座席查询所有挂载
func (s *CustomerServiceAgentService) ListByAgentStatusID(ctx context.Context, agentStatusID uint) ([]*model.CustomerServiceAgent, error) {
	return s.repo.ListByAgentStatusID(ctx, agentStatusID)
}

// ListByAIAgentID 反查智能体被哪些客服使用
func (s *CustomerServiceAgentService) ListByAIAgentID(ctx context.Context, aiAgentID uint) ([]*model.CustomerServiceAgent, error) {
	return s.repo.ListByAIAgentID(ctx, aiAgentID)
}

// GetByID 获取挂载详情
func (s *CustomerServiceAgentService) GetByID(ctx context.Context, id uint) (*model.CustomerServiceAgent, error) {
	return s.repo.GetByID(ctx, id)
}

// Update 更新挂载
func (s *CustomerServiceAgentService) Update(ctx context.Context, c *model.CustomerServiceAgent) error {
	if c.IsPrimary {
		existing, _ := s.repo.GetByID(ctx, c.ID)
		if existing != nil {
			if err := s.repo.ClearPrimaryByAgentStatusID(ctx, existing.AgentStatusID); err != nil {
				return err
			}
		}
	}
	return s.repo.Update(ctx, c)
}

// Delete 删除挂载
func (s *CustomerServiceAgentService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// LoadAgentForSeat 加载客服座席挂载的主智能体上下文
// SmartCSOrchestrator 处理消息时调用此方法
// 返回 nil, nil 表示未挂载
func (s *CustomerServiceAgentService) LoadAgentForSeat(ctx context.Context, agentStatusID uint) (*AgentContext, error) {
	mount, err := s.repo.GetPrimaryByAgentStatusID(ctx, agentStatusID)
	if err != nil {
		return nil, err
	}
	if mount == nil {
		return nil, nil
	}
	return s.agentSvc.LoadContext(ctx, mount.AIAgentID)
}

// GetOrCreateAgentStatusByUserID 按用户ID查找座席状态，不存在则创建
// 用于前端"为团队成员挂载AI智能体"场景：团队成员即座席，AgentStatus.AgentID = 用户ID
func (s *CustomerServiceAgentService) GetOrCreateAgentStatusByUserID(ctx context.Context, userID uint, name string) (*model.AgentStatus, error) {
	if userID == 0 {
		return nil, errors.New("user_id 不能为空")
	}
	st, err := s.agentStatusRepo.GetByAgentID(ctx, userID)
	if err == nil {
		return st, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询座席状态失败: %w", err)
	}
	newStatus := &model.AgentStatus{
		AgentID:     userID,
		AgentName:   name,
		Status:      "offline",
		MaxSessions: 5,
	}
	if err := s.agentStatusRepo.Create(ctx, newStatus); err != nil {
		return nil, fmt.Errorf("创建座席状态失败: %w", err)
	}
	return newStatus, nil
}

// ListByUserID 按用户ID查询挂载（先查 AgentStatus，再查挂载）
func (s *CustomerServiceAgentService) ListByUserID(ctx context.Context, userID uint) ([]*model.CustomerServiceAgent, error) {
	if userID == 0 {
		return nil, errors.New("user_id 不能为空")
	}
	st, err := s.agentStatusRepo.GetByAgentID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []*model.CustomerServiceAgent{}, nil
		}
		return nil, fmt.Errorf("查询座席状态失败: %w", err)
	}
	return s.repo.ListByAgentStatusID(ctx, st.ID)
}

// CreateByUserID 按用户ID创建挂载（自动创建 AgentStatus）
func (s *CustomerServiceAgentService) CreateByUserID(ctx context.Context, userID uint, userName string, aiAgentID uint, isPrimary bool) (*model.CustomerServiceAgent, error) {
	if aiAgentID == 0 {
		return nil, errors.New("ai_agent_id 不能为空")
	}
	st, err := s.GetOrCreateAgentStatusByUserID(ctx, userID, userName)
	if err != nil {
		return nil, err
	}
	m := &model.CustomerServiceAgent{
		AgentStatusID: st.ID,
		AIAgentID:     aiAgentID,
		IsPrimary:     isPrimary,
		Enabled:       true,
	}
	if err := s.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

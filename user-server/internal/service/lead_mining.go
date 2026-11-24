package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// IsRecordNotFound reports whether err represents a "record not found" condition.
// Controller 层通过此函数判断，避免直接引用 gorm 包（分层约束）。
func IsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

const (
	leadMiningQueueSize       = 8192
	leadMiningWorkers         = 4
	leadMiningDebounce        = 60 * time.Second
	leadMiningHistorySize     = 20
	leadMiningConfigTTL       = 30 * time.Second
	leadMiningHighOpportunity = 70
)

// LeadJudgement LLM 结构化判定结果
type LeadJudgement struct {
	IsLead             bool     `json:"is_lead"`
	IntentScore        int      `json:"intent_score"`
	ClueType           string   `json:"clue_type"` // 需求/供货/资源/中介线索（T039 角色分类）
	MatchedKeywords    []string `json:"matched_keywords"`
	MatchedTags        []string `json:"matched_tags"`
	Summary            string   `json:"summary"`
	Confidence         float64  `json:"confidence"`
	Reason             string   `json:"reason"`
	LeadRole           string   `json:"lead_role"`           // demand/supply/resource/broker/unknown
	FollowupSuggestion string   `json:"followup_suggestion"` // 给维护人的跟进建议
}

// LLMJudge 判定接口（便于测试注入 fake）
type LLMJudge interface {
	Judge(ctx context.Context, cfg *model.LeadMiningConfig, history []llm.ChatMessage) (*LeadJudgement, error)
}

type leadMiningConfigCacheEntry struct {
	config            *model.LeadMiningConfig
	loadedAt          time.Time
	serviceGeneration uint64
	tenantGeneration  uint64
}

// Service 线索发掘服务（非侵入异步）
type Service struct {
	queue              chan *model.MessageHub
	workers            int
	judge              LLMJudge
	historyFetcher     func(ctx context.Context, hub *model.MessageHub) []llm.ChatMessage
	custRepo           repository.CustomerRepository
	clueRepo           repository.ClueRepository
	cfgRepo            repository.LeadMiningConfigRepository
	hubRepo            *repository.MessageHubRepository
	mu                 sync.Mutex
	lastJudge          map[string]time.Time
	persistLocks       sync.Map
	cfgCache           map[uint]leadMiningConfigCacheEntry
	cfgGeneration      map[uint]uint64
	cfgCacheGeneration uint64
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
}

var singleton *Service

// NewLeadMiningService 构造并启动线索发掘服务（后台 worker，非阻塞）。
// db 用于线索仓储的显式注入（不摸全局句柄）。
func NewLeadMiningService(clueRepo repository.ClueRepository) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		queue:         make(chan *model.MessageHub, leadMiningQueueSize),
		workers:       leadMiningWorkers,
		judge:         &dispatcherJudge{},
		custRepo:      repository.NewCustomerRepository(),
		clueRepo:      clueRepo,
		cfgRepo:       repository.NewLeadMiningConfigRepository(),
		hubRepo:       repository.NewMessageHubRepository(),
		lastJudge:     make(map[string]time.Time),
		cfgCache:      make(map[uint]leadMiningConfigCacheEntry),
		cfgGeneration: make(map[uint]uint64),
		cancel:        cancel,
	}
	singleton = s
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
	logger.Infof("[lead-mining] 服务已启动，worker=%d queue=%d", s.workers, leadMiningQueueSize)
	return s
}

// ReloadConfigCache 热更新配置缓存（控制器保存配置后调用）
func ReloadConfigCache() {
	if singleton == nil {
		return
	}
	singleton.mu.Lock()
	singleton.invalidateConfigCacheLocked(nil)
	singleton.mu.Unlock()
}

// ReloadConfigCacheForTenant 热更新指定租户的配置缓存。
func ReloadConfigCacheForTenant(tenantID uint) {
	if singleton == nil || tenantID == 0 {
		return
	}
	singleton.mu.Lock()
	singleton.invalidateConfigCacheLocked(&tenantID)
	singleton.mu.Unlock()
}

func (s *Service) invalidateConfigCacheLocked(tenantID *uint) {
	if s.cfgCache == nil {
		s.cfgCache = make(map[uint]leadMiningConfigCacheEntry)
	}
	if tenantID == nil {
		s.cfgCacheGeneration++
		s.cfgCache = make(map[uint]leadMiningConfigCacheEntry)
		return
	}
	if s.cfgGeneration == nil {
		s.cfgGeneration = make(map[uint]uint64)
	}
	delete(s.cfgCache, *tenantID)
	s.cfgGeneration[*tenantID]++
}

// GetLeadMiningConfig 读取线索发掘全局配置（controller 层入口，避免 controller 直连 repository/model）
func GetLeadMiningConfig(ctx context.Context) (*dto.LeadMiningConfig, error) {
	cfg, err := repository.NewLeadMiningConfigRepository().GetSingleton(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &dto.LeadMiningConfig{}, nil
	}
	return leadMiningConfigToDTO(cfg), nil
}

// GetStatus 返回线索挖掘服务运行时状态
func GetStatus() map[string]any {
	s := singleton
	if s == nil {
		return map[string]any{"running": false, "message": "service not initialized"}
	}
	s.mu.Lock()
	recent := len(s.lastJudge)
	configLoaded := len(s.cfgCache) > 0
	s.mu.Unlock()
	return map[string]any{
		"running":          true,
		"workers":          s.workers,
		"queue_depth":      len(s.queue),
		"recent_processed": recent,
		"cfg_loaded":       configLoaded,
	}
}

func SaveLeadMiningConfig(ctx context.Context, in *dto.LeadMiningConfig) error {
	if err := repository.NewLeadMiningConfigRepository().Save(ctx, leadMiningConfigFromDTO(in)); err != nil {
		return err
	}
	if tenantID, ok := tenant.FromContext(ctx); ok {
		ReloadConfigCacheForTenant(tenantID)
	} else {
		ReloadConfigCache()
	}
	return nil
}

// ListLeadMiningJudgements 返回判定记录列表（最小只读 API，LM-PR5）。
func ListLeadMiningJudgements(ctx context.Context, page, limit int, platform, accountID, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	return repository.NewLeadMiningJudgementRepository().List(ctx, page, limit, platform, accountID, leadRole, matched)
}

func leadMiningConfigToDTO(m *model.LeadMiningConfig) *dto.LeadMiningConfig {
	return &dto.LeadMiningConfig{
		ID:             m.ID,
		Enabled:        m.Enabled,
		Keywords:       []string(m.Keywords),
		Tags:           []string(m.Tags),
		Requirement:    m.Requirement,
		Channels:       []string(m.Channels),
		MinIntentScore: m.MinIntentScore,
		Model:          m.Model,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func leadMiningConfigFromDTO(d *dto.LeadMiningConfig) *model.LeadMiningConfig {
	return &model.LeadMiningConfig{
		ID:             d.ID,
		Enabled:        d.Enabled,
		Keywords:       model.JSONStrings(d.Keywords),
		Tags:           model.JSONStrings(d.Tags),
		Requirement:    d.Requirement,
		Channels:       model.JSONStrings(d.Channels),
		MinIntentScore: d.MinIntentScore,
		Model:          d.Model,
	}
}

// Stop 停止服务（进程退出时）
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// Enqueue 非阻塞入队：核心业务只在消息落库后调用一次，绝不阻塞/入侵
func (s *Service) Enqueue(hub *model.MessageHub) {
	if s == nil || hub == nil {
		return
	}
	select {
	case s.queue <- hub:
	default:
		logger.Warnf("[lead-mining] 队列已满，丢弃消息 msgID=%s", hub.MsgID)
	}
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case hub := <-s.queue:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[lead-mining] worker panic recovered: %v\n%s", r, debug.Stack())
					}
				}()
				s.process(ctx, hub)
			}()
		}
	}
}

func (s *Service) process(ctx context.Context, hub *model.MessageHub) {
	if hub == nil || hub.Direction != "inbound" {
		return
	}
	if hub.TenantID == 0 && tenant.IsMultiTenant() {
		logger.Warnf("[lead-mining] 拒绝无租户归属的消息 msgID=%s", hub.MsgID)
		return
	}
	if hub.TenantID != 0 {
		ctx = tenant.OnlyTenant(ctx, hub.TenantID)
	} else {
		ctx = tenant.OnlyTenant(ctx, tenant.DefaultTenantID)
	}
	content := strings.TrimSpace(hub.Content)
	if content == "" {
		return
	}

	// T040：复用 ai_debounce.go 的窗口配置做线索去重；无 dedup_hash 的测试/历史
	// 消息不参与，避免把不同来源的消息误合并。
	if hub.DedupHash != "" {
		evt := &model.MessageEvent{Content: content, SessionID: hub.ConversationID}
		if !LeadDebounce().TryClaim(ctx, evt, hub.Platform, hub.AccountID, hub.SenderID, hub.DedupHash) {
			logger.Infof("[lead-mining] 防抖命中，跳过重复消息 msgID=%s hash=%s", hub.MsgID, hub.DedupHash)
			return
		}
	}

	cfg := s.loadConfig(ctx)
	if cfg == nil || !cfg.Enabled {
		return
	}
	if !channelEnabled(cfg, hub.Platform) {
		return
	}

	account := fmt.Sprintf("%s:%s", hub.Platform, hub.SenderID)
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			logger.Warnf("[lead-mining] 拒绝无租户归属的消息 msgID=%s", hub.MsgID)
			return
		}
		tenantID = tenant.DefaultTenantID
	}
	debounceKey := leadMiningDebounceKey(tenantID, hub.Platform, hub.ConversationID, hub.SenderID, hub.IsGroup)
	s.mu.Lock()
	if last, ok := s.lastJudge[debounceKey]; ok && time.Since(last) < leadMiningDebounce {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	history := s.fetchHistory(ctx, hub)
	if len(history) == 0 {
		return
	}

	// T039：组装多轮/历史 + 三路证据富上下文；fake judge（未实现可选方法）
	// 仍走原 Judge 路径，不改变既有测试契约。
	judgementID := fmt.Sprintf("lmj_%d", time.Now().UnixNano())
	jctx := s.buildLeadJudgeContext(ctx, hub, history, judgementID)
	var jd *LeadJudgement
	var err error
	if rich, ok := s.judge.(interface {
		JudgeWithContext(context.Context, *LeadJudgeContext) (*LeadJudgement, error)
	}); ok {
		jd, err = rich.JudgeWithContext(ctx, jctx)
	} else {
		jd, err = s.judge.Judge(ctx, cfg, history)
	}
	if err != nil {
		logger.Warnf("[lead-mining] 判定失败 account=%s: %v", account, err)
		return
	}
	// 仅在判定成功后占用防抖窗口，避免失败消息阻塞后续判定（D-03）。
	s.mu.Lock()
	s.lastJudge[debounceKey] = time.Now()
	s.mu.Unlock()
	s.recordLeadMiningJudgement(ctx, hub, jd, jctx)
	if !jd.IsLead || jd.IntentScore < cfg.MinIntentScore {
		return
	}

	s.persistLead(ctx, cfg, hub, account, jd)
}

func leadMiningDebounceKey(tenantID uint, platform, conversationID, senderID string, isGroup bool) string {
	key := fmt.Sprintf("%s:%s", platform, senderID)
	if isGroup && conversationID != "" {
		key = fmt.Sprintf("%s:%s:%s", platform, conversationID, senderID)
	}
	if tenantID == tenant.DefaultTenantID {
		return key
	}
	return fmt.Sprintf("%d:%s", tenantID, key)
}

func (s *Service) loadConfig(ctx context.Context) *model.LeadMiningConfig {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() || tenant.IsSystemScope(ctx) {
			return nil
		}
		tenantID = tenant.DefaultTenantID
		ctx = tenant.OnlyTenant(ctx, tenantID)
	}

	now := time.Now()
	s.mu.Lock()
	if s.cfgCache == nil {
		s.cfgCache = make(map[uint]leadMiningConfigCacheEntry)
	}
	if s.cfgGeneration == nil {
		s.cfgGeneration = make(map[uint]uint64)
	}
	if cached, found := s.cfgCache[tenantID]; found && cached.serviceGeneration == s.cfgCacheGeneration && cached.tenantGeneration == s.cfgGeneration[tenantID] && now.Sub(cached.loadedAt) < leadMiningConfigTTL {
		cfg := cloneLeadMiningConfig(cached.config)
		s.mu.Unlock()
		return cfg
	}
	generation := s.cfgCacheGeneration
	tenantGeneration := s.cfgGeneration[tenantID]
	s.mu.Unlock()

	cfg, err := s.cfgRepo.GetSingleton(ctx)
	if err != nil {
		logger.Warnf("[lead-mining] 读取配置失败: %v", err)
		return nil
	}
	if cfg == nil {
		return nil
	}

	cachedConfig := cloneLeadMiningConfig(cfg)
	s.mu.Lock()
	if generation == s.cfgCacheGeneration && tenantGeneration == s.cfgGeneration[tenantID] {
		s.cfgCache[tenantID] = leadMiningConfigCacheEntry{
			config:            cachedConfig,
			loadedAt:          time.Now(),
			serviceGeneration: generation,
			tenantGeneration:  tenantGeneration,
		}
	}
	s.mu.Unlock()
	return cloneLeadMiningConfig(cachedConfig)
}

func cloneLeadMiningConfig(cfg *model.LeadMiningConfig) *model.LeadMiningConfig {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	cloned.Keywords = append(model.JSONStrings(nil), cfg.Keywords...)
	cloned.Tags = append(model.JSONStrings(nil), cfg.Tags...)
	cloned.Channels = append(model.JSONStrings(nil), cfg.Channels...)
	return &cloned
}

func (s *Service) fetchHistory(ctx context.Context, hub *model.MessageHub) []llm.ChatMessage {
	if s.historyFetcher != nil {
		return s.historyFetcher(ctx, hub)
	}
	return s.fetchHistoryDB(ctx, hub)
}

func (s *Service) fetchHistoryDB(ctx context.Context, hub *model.MessageHub) []llm.ChatMessage {
	var (
		hubs []model.MessageHub
		err  error
	)
	// LM-PR1: 单聊结合两人历史（同 conversation 双向）；群聊结合群内大家发言。
	if hub.IsGroup && hub.GroupID != "" {
		hubs, err = s.hubRepo.ListRecentByGroup(ctx, hub.Platform, hub.GroupID, leadMiningHistorySize)
	} else if hub.ConversationID != "" {
		hubs, err = s.hubRepo.ListRecentByConversation(ctx, hub.ConversationID, leadMiningHistorySize)
	} else {
		hubs, err = s.hubRepo.ListRecentInboundBySender(ctx, hub.Platform, hub.SenderID, leadMiningHistorySize)
	}
	if err != nil {
		logger.Warnf("[lead-mining] 读取会话历史失败: %v", err)
		return nil
	}
	// DESC -> ASC
	for i, j := 0, len(hubs)-1; i < j; i, j = i+1, j-1 {
		hubs[i], hubs[j] = hubs[j], hubs[i]
	}
	msgs := make([]llm.ChatMessage, 0, len(hubs))
	for _, h := range hubs {
		c := strings.TrimSpace(h.Content)
		if c == "" {
			continue
		}
		role := "user"
		speaker := strings.TrimSpace(h.SenderName)
		if speaker == "" {
			speaker = strings.TrimSpace(h.SenderID)
		}
		if h.Direction == "outbound" {
			role = "assistant"
		}
		// 群聊保留说话人，避免多成员上下文丢失。
		if hub.IsGroup {
			if speaker != "" {
				c = fmt.Sprintf("[%s] %s", speaker, c)
			}
			role = "user"
		}
		msgs = append(msgs, llm.ChatMessage{Role: role, Content: c})
	}
	return msgs
}

func (s *Service) persistLead(ctx context.Context, cfg *model.LeadMiningConfig, hub *model.MessageHub, account string, jd *LeadJudgement) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			logger.Warnf("[lead-mining] 拒绝无租户归属的线索写入 account=%s", account)
			return
		}
		tenantID = tenant.DefaultTenantID
	}
	lockKey := fmt.Sprintf("%d:%d:%s", tenantID, ClueTypeLeadMining, account)
	if v, _ := s.persistLocks.LoadOrStore(lockKey, &sync.Mutex{}); v != nil {
		mu := v.(*sync.Mutex)
		mu.Lock()
		defer mu.Unlock()
	}
	customer, err := s.resolveCustomer(ctx, hub.Platform, hub.SenderID, hub.SenderName)
	if err != nil || customer == nil {
		logger.Warnf("[lead-mining] 解析客户失败 account=%s: %v", account, err)
		return
	}

	tags := mergeTags(cfg.Tags, jd.MatchedTags)
	if len(tags) > 0 {
		if err := s.tagCustomer(ctx, customer, tags); err != nil {
			logger.Warnf("[lead-mining] 打标签失败 customer=%s: %v", customer.ID, err)
		}
	}

	clueType := ClueTypeLeadMining

	nowJudged := time.Now().Unix()
	evidenceJSON, _ := json.Marshal([]map[string]any{{
		"message_id": hub.MsgID, "conversation_id": hub.ConversationID,
		"sender_id": hub.SenderID, "platform": hub.Platform, "reason": jd.Reason,
	}})
	leadRole := normalizeLeadRole(jd.LeadRole)
	conversationType := "private"
	if hub.IsGroup {
		conversationType = "group"
	}
	clue := &model.Clue{
		Type:               clueType,
		Account:            account,
		Name:               displayName(customer, hub),
		Desc:               jd.Summary,
		IntentScore:        int64(jd.IntentScore),
		IsOpportunity:      boolToInt(jd.IntentScore >= leadMiningHighOpportunity),
		ConversationID:     hub.ConversationID,
		OneID:              customer.UnifiedID,
		SourceID:           "lead_mining",
		MessageID:          fmt.Sprintf("%d", hub.ID),
		OwnerAccount:       hub.AccountID,
		IsGroup:            hub.IsGroup,
		GroupID:            hub.GroupID,
		LeadRole:           leadRole,
		Status:             "new",
		EvidenceJSON:       string(evidenceJSON),
		Summary:            jd.Summary,
		FollowupSuggestion: jd.FollowupSuggestion,
		LastJudgedAt:       &nowJudged,
		SourceChannel:      hub.Platform,
		SourceAccountID:    hub.AccountID,
		ConversationType:   conversationType,
	}

	existing, gerr := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account)
	if gerr == nil && existing != nil {
		nowJudged := time.Now().Unix()
		evidenceJSON, _ := json.Marshal([]map[string]any{{
			"message_id": hub.MsgID, "conversation_id": hub.ConversationID,
			"sender_id": hub.SenderID, "platform": hub.Platform, "reason": jd.Reason,
		}})
		if err := s.clueRepo.UpdateByID(ctx, existing.ID, map[string]any{
			"intent_score":        int64(jd.IntentScore),
			"desc":                jd.Summary,
			"is_opportunity":      boolToInt(jd.IntentScore >= leadMiningHighOpportunity),
			"conversation_id":     hub.ConversationID,
			"message_id":          fmt.Sprintf("%d", hub.ID),
			"is_group":            hub.IsGroup,
			"group_id":            hub.GroupID,
			"lead_role":           normalizeLeadRole(jd.LeadRole),
			"evidence_json":       string(evidenceJSON),
			"summary":             jd.Summary,
			"followup_suggestion": jd.FollowupSuggestion,
			"last_judged_at":      nowJudged,
			"source_channel":      hub.Platform,
			"source_account_id":   hub.AccountID,
			"conversation_type":   map[bool]string{true: "group", false: "private"}[hub.IsGroup],
		}); err != nil {
			logger.Warnf("[lead-mining] 更新线索失败 account=%s: %v", account, err)
		}
		// 更新分支到此为止没有 clueRepo.Create，线索号是查出来的那一条 ——
		// 本分支的 return 早于新建那一支，是这条接缝最容易只接一半的地方（判据见用例）。
		s.tryConvertToOpportunity(ctx, existing.ID, customer, jd)
		return
	}
	if err := s.clueRepo.Create(ctx, clue); err != nil {
		// TOCTOU 兜底：并发插入冲突时回查已有记录并转为更新（D-01）。
		if existing2, gerr := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account); gerr == nil && existing2 != nil {
			nowJudged2 := time.Now().Unix()
			if uerr := s.clueRepo.UpdateByID(ctx, existing2.ID, map[string]any{
				"intent_score":   int64(jd.IntentScore),
				"desc":           jd.Summary,
				"last_judged_at": nowJudged2,
			}); uerr != nil {
				logger.Warnf("[lead-mining] TOCTOU 兜底更新线索失败 account=%s: %v", account, uerr)
			}
			s.tryConvertToOpportunity(ctx, existing2.ID, customer, jd)
			return
		}
		logger.Warnf("[lead-mining] 写入线索失败 account=%s: %v", account, err)
		return
	}
	s.notifyLeadDiscovery(ctx, clue, jd)
	s.tryConvertToOpportunity(ctx, clue.ID, customer, jd)
}

// tryConvertToOpportunity 线索写完之后的那一跳（T-P4-05：达标的线索自动变成有人推进的商机）。
//
// 三条口径：
//  1. 整条路径**可选**：商机竖没装配（无 DB 句柄 ⇒ 全局为 nil）时这里直接返回，
//     线索写入那一步的既有行为一个字节都不变。这条服务一直是"非侵入异步"的。
//  2. 拒签不记日志：闸门关着或未达阈值是**运营的选择**，而默认配置就是关 ——
//     每条线索打一行日志会把挖掘这条路径的日志刷成噪声。转换那一步成功的详单
//     由转换器自己打（含归属与规则），加上 operation_logs 那一行，两处都在说同一件事。
//  3. 失败只 Warn，不重试、不回滚线索：判定/写入故障修好之后下一条消息自然再走一遍，
//     而把一条已落库的线索因为"没转成商机"一起撤掉，是拿既有功能给新功能陪绑。
//     越界入参（模型给出 0–1 之外的 confidence）也落在这里 —— 它报错，但没人替它归一。
func (s *Service) tryConvertToOpportunity(ctx context.Context, clueID string, customer *model.Customer, jd *LeadJudgement) {
	conv := GlobalOpportunityConverter()
	if !conv.Available() {
		logger.Debugf("[lead-mining] 商机竖未装配，线索 %s 不转商机（检查 app.InitOpportunityRuntime）", clueID)
		return
	}
	res, err := conv.ConvertFromClue(ctx, OpportunityConversion{
		ClueID:     clueID,
		CustomerID: customer.ID,
		OneID:      customer.UnifiedID,
		LeadScore:  jd.IntentScore,
		Confidence: jd.Confidence,
	})
	if err != nil {
		logger.Warnf("[lead-mining] 线索 %s 转商机未完成: %v", clueID, err)
		return
	}
	if res.Created && res.AuditError != "" {
		// 商机已经落库，缺的是来源记录 —— 这一条必须冒出来：它意味着事后没人答得出
		// "这单凭什么占我的工作台"，而那一格正是本处承诺要能回答的问题。
		logger.Warnf("[lead-mining] 商机 %s 已建但审计缺失: %s", res.Opportunity.ID, res.AuditError)
	}
}

func (s *Service) notifyLeadDiscovery(ctx context.Context, clue *model.Clue, jd *LeadJudgement) {
	if clue == nil || jd == nil {
		return
	}
	roleLabel := map[string]string{"demand": "需求方", "supply": "供货方", "resource": "资源方", "broker": "中介", "unknown": "待识别"}[clue.LeadRole]
	if roleLabel == "" {
		roleLabel = "待识别"
	}
	title := fmt.Sprintf("发现新线索：%s", clue.Name)
	content := fmt.Sprintf("渠道=%s 角色=%s 意向=%d 摘要=%s", clue.SourceChannel, roleLabel, clue.IntentScore, clue.Summary)
	if clue.FollowupSuggestion != "" {
		content += "\n建议：" + clue.FollowupSuggestion
	}
	_ = NotifyBusiness(ctx, &model.Notification{
		UserID:  0,
		Type:    model.NotificationTypeInfo,
		Title:   title,
		Content: content,
		Link:    "/lead-mining",
		Source:  model.NotificationSourceLocal,
		Level:   model.NotificationLevelInfo,
	})
}

func (s *Service) resolveCustomer(ctx context.Context, platform, senderID, name string) (*model.Customer, error) {
	if platform == "" || senderID == "" {
		return nil, fmt.Errorf("无客户身份标识")
	}
	// LM-PR3: 优先复用已有 Customer 身份，避免 lm:platform:sender 平行主身份长期膨胀。
	legacyKey := "lm:" + platform + ":" + senderID
	if c, gerr := s.custRepo.GetByUnifiedID(ctx, legacyKey); gerr == nil && c != nil {
		if name != "" && (c.Name == "" || c.Name == legacyKey) {
			c.Name = name
			_ = s.custRepo.Update(ctx, c)
		}
		return c, nil
	}
	candidate := &model.Customer{Name: name}
	applyLeadMiningIdentity(candidate, platform, senderID)
	unifiedID := model.GenerateCustomerUnifiedID(candidate)
	if unifiedID == "" {
		unifiedID = legacyKey
	}
	if c, gerr := s.custRepo.GetByUnifiedID(ctx, unifiedID); gerr == nil && c != nil {
		if name != "" && c.Name == "" {
			c.Name = name
			_ = s.custRepo.Update(ctx, c)
		}
		return c, nil
	}
	candidate.UnifiedID = unifiedID
	candidate.SourceChannel = platform
	candidate.SourceAccountID = senderID
	if cerr := s.custRepo.Create(ctx, candidate); cerr != nil {
		if c2, g2 := s.custRepo.GetByUnifiedID(ctx, unifiedID); g2 == nil && c2 != nil {
			return c2, nil
		}
		if unifiedID != legacyKey {
			if c2, g2 := s.custRepo.GetByUnifiedID(ctx, legacyKey); g2 == nil && c2 != nil {
				return c2, nil
			}
		}
		return nil, cerr
	}
	return candidate, nil
}

func applyLeadMiningIdentity(c *model.Customer, platform, senderID string) {
	p := strings.ToLower(strings.TrimSpace(platform))
	id := strings.TrimSpace(senderID)
	if c == nil || id == "" {
		return
	}
	switch p {
	case "wechat", "weixin", "individual_wechat":
		c.WechatOpenID = id
	case "wecom", "qywx", "wework":
		c.WeComExternalID = id
	case "feishu", "lark":
		c.FeishuOpenID = id
	case "douyin":
		c.DouyinOpenID = id
	case "tiktok":
		c.TikTokOpenID = id
	case "kuaishou":
		c.KuaishouOpenID = id
	case "xiaohongshu", "xhs":
		c.XiaohongshuID = id
	case "xianyu":
		c.XianyuID = id
	case "telegram":
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			c.TelegramChatID = n
		}
	case "whatsapp":
		c.WhatsAppPhone = id
	case "email":
		c.Email = id
	default:
		// qq/sms/web/dingtalk 等暂无专用字段，回退 legacy lm: key
	}
}

func (s *Service) tagCustomer(ctx context.Context, customer *model.Customer, newTags []string) error {
	var cur []string
	_ = json.Unmarshal([]byte(customer.Tags), &cur)
	merged := mergeTags(cur, newTags)
	b, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	customer.Tags = string(b)
	return s.custRepo.Update(ctx, customer)
}

func channelEnabled(cfg *model.LeadMiningConfig, platform string) bool {
	if len(cfg.Channels) == 0 {
		return true
	}
	for _, c := range cfg.Channels {
		if strings.EqualFold(strings.TrimSpace(c), strings.TrimSpace(platform)) {
			return true
		}
	}
	return false
}

func mergeTags(a, b []string) []string {
	set := map[string]string{}
	add := func(t string) {
		if v := strings.TrimSpace(t); v != "" {
			k := strings.ToLower(v)
			if _, ok := set[k]; !ok {
				set[k] = v
			}
		}
	}
	for _, t := range a {
		add(t)
	}
	for _, t := range b {
		add(t)
	}
	out := make([]string, 0, len(set))
	for _, v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func displayName(c *model.Customer, hub *model.MessageHub) string {
	if hub != nil && hub.SenderName != "" {
		return hub.SenderName
	}
	if c != nil && c.ID != "" {
		return c.ID
	}
	if hub != nil {
		return hub.SenderID
	}
	return "未知"
}

type dispatcherJudge struct{}

func (j *dispatcherJudge) Judge(ctx context.Context, cfg *model.LeadMiningConfig, history []llm.ChatMessage) (*LeadJudgement, error) {
	d := llm.GetGlobalDispatcher()
	if d == nil {
		return nil, fmt.Errorf("llm dispatcher 未初始化")
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioLowCost,
		SystemPrompt: buildSystemPrompt(cfg),
		Messages:     history,
		MaxTokens:    1024,
		Temperature:  0.2,
	}
	var jd LeadJudgement
	if _, err := d.DispatchStructured(ctx, req, &jd); err != nil {
		return nil, err
	}
	return &jd, nil
}

func buildSystemPrompt(cfg *model.LeadMiningConfig) string {
	var b strings.Builder
	b.WriteString("你是企业私域的「线索发掘」助手。下面是运营设置的发掘规则，请你基于提供的多轮客户对话，判断是否构成值得跟进的销售线索。\n\n")
	if len(cfg.Keywords) > 0 {
		b.WriteString("【关键词】命中以下任一关键词应加分（仅参考，不是唯一标准）：" + strings.Join(cfg.Keywords, "、") + "\n")
	}
	if len(cfg.Tags) > 0 {
		b.WriteString("【期望标签】命中线索后应给客户打的标签：" + strings.Join(cfg.Tags, "、") + "\n")
	}
	if strings.TrimSpace(cfg.Requirement) != "" {
		b.WriteString("【线索要求】运营对「什么是线索」的明确要求：\n" + cfg.Requirement + "\n")
	}
	b.WriteString("\n请综合全部对话，输出严格 JSON：\n")
	b.WriteString("{\n  \"is_lead\": bool,\n  \"intent_score\": 0-100 的整数（>=70 强意向，40-69 中等，<40 弱）,\n")
	b.WriteString("  \"matched_keywords\": [命中的关键词],\n  \"matched_tags\": [建议追加的标签],\n")
	b.WriteString("  \"lead_role\": \"demand|supply|resource|broker|unknown\",\n")
	b.WriteString("  \"summary\": \"一句话线索摘要（含客户诉求/场景/预算等要点）\",\n  \"followup_suggestion\": \"给维护人的下一步跟进建议\",\n")
	b.WriteString("  \"confidence\": 0-1 的置信度,\n  \"reason\": \"判定理由\"\n}\n")
	b.WriteString("只输出 JSON，不要额外解释。")
	return b.String()
}

func normalizeLeadRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "demand", "buyer", "需求方":
		return "demand"
	case "supply", "supplier", "seller", "供货方":
		return "supply"
	case "resource", "partner", "上下游", "资源方":
		return "resource"
	case "broker", "middleman", "中介":
		return "broker"
	default:
		return "unknown"
	}
}

func (s *Service) recordLeadMiningJudgement(ctx context.Context, hub *model.MessageHub, jd *LeadJudgement, jctxs ...*LeadJudgeContext) {
	if hub == nil || jd == nil {
		return
	}
	judgementRepo := repository.NewLeadMiningJudgementRepository()
	if judgementRepo == nil {
		return
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			return
		}
		tid = tenant.DefaultTenantID
	}
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	resultJSON, _ := json.Marshal(jd)
	ctxJSON, _ := json.Marshal(map[string]any{
		"conversation_id": hub.ConversationID, "is_group": hub.IsGroup, "group_id": hub.GroupID,
		"platform": hub.Platform, "account_id": hub.AccountID, "sender_id": hub.SenderID,
	})
	judgementID := fmt.Sprintf("lmj_%d", time.Now().UnixNano())
	if len(jctxs) > 0 && jctxs[0] != nil && jctxs[0].JudgementID != "" {
		judgementID = jctxs[0].JudgementID
	}
	rec := &model.LeadMiningJudgement{
		TenantID:       tid,
		JudgementID:    judgementID,
		MessageID:      hub.MsgID,
		ConversationID: hub.ConversationID,
		Platform:       hub.Platform,
		AccountID:      hub.AccountID,
		IsGroup:        hub.IsGroup,
		GroupID:        hub.GroupID,
		SenderID:       hub.SenderID,
		ContentHash:    fmt.Sprintf("%d", hub.ID),
		ContextJSON:    string(ctxJSON),
		ResultJSON:     string(resultJSON),
		Matched:        jd.IsLead,
		LeadRole:       normalizeLeadRole(jd.LeadRole),
		IntentScore:    jd.IntentScore,
		Reason:         jd.Reason,
	}
	// T039：把二次汇总的角色分类、上下文引用与三路证据完整度一并落库，
	// 供判定详情回放与归档/通知链路判读（证据不完整必须可见）。
	rec.IsClue = jd.IsLead
	rec.ClueType = jd.ClueType
	rec.Summary = jd.Summary
	if len(jctxs) > 0 && jctxs[0] != nil {
		c := jctxs[0]
		rec.ChannelAccountID = targetChannelAccountOf(c)
		rec.ContextMessageIDs = make(model.JSONStrings, 0, len(c.Messages))
		for _, m := range c.Messages {
			if m.MessageID != "" {
				rec.ContextMessageIDs = append(rec.ContextMessageIDs, m.MessageID)
			}
		}
		rec.SourceHitIDs = make(model.JSONStrings, 0, len(c.Hits))
		rec.EvidenceComplete = len(c.Hits) > 0
		for _, h := range c.Hits {
			rec.SourceHitIDs = append(rec.SourceHitIDs, strconv.FormatInt(h.ID, 10))
			if !h.Complete || h.Degraded {
				rec.EvidenceComplete = false
				rec.Degraded = true
			}
		}
	}
	if err := judgementRepo.Create(ctx, rec); err != nil {
		logger.Warnf("[lead-mining] 记录判定失败 msg=%s: %v", hub.MsgID, err)
	}
}

func targetChannelAccountOf(c *LeadJudgeContext) uint {
	if c == nil {
		return 0
	}
	return c.ChannelAccountID
}

// GetLeadMiningJudgementDetail 返回判定记录回放详情（LM-PR6）。
func GetLeadMiningJudgementDetail(ctx context.Context, judgementID string) (*model.LeadMiningJudgement, error) {
	if strings.TrimSpace(judgementID) == "" {
		return nil, fmt.Errorf("judgement_id 不能为空")
	}
	return repository.NewLeadMiningJudgementRepository().GetByID(ctx, judgementID)
}

// SubmitLeadMiningJudgementFeedback 标记判定误报/有效（LM-PR6）。
func SubmitLeadMiningJudgementFeedback(ctx context.Context, judgementID, status, reason, by string) error {
	if strings.TrimSpace(judgementID) == "" {
		return fmt.Errorf("judgement_id 不能为空")
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "false_positive" && status != "valid" && status != "none" {
		return fmt.Errorf("feedback_status 仅支持 none/false_positive/valid")
	}
	return repository.NewLeadMiningJudgementRepository().UpdateFeedback(ctx, judgementID, status, strings.TrimSpace(reason), strings.TrimSpace(by))
}

// GetLeadMiningQualityStats 返回线索发掘质量看板统计（LM-PR6）。
func GetLeadMiningQualityStats(ctx context.Context, days int) (map[string]any, error) {
	return repository.NewLeadMiningJudgementRepository().GetQualityStats(ctx, days)
}

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
	"sync/atomic"
	"time"

	"hivemtk-user/internal/aiagent/llm"
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
	// Degraded 表示二次 LLM 不可用，结论由关键词/语义命中确定性兜底得出。
	Degraded bool `json:"degraded,omitempty"`
	// ReactOutcome 是文本 ReAct 取证机器状态；只含工具观察与固定原因，不含模型思考。
	ReactOutcome *LeadReactOutcome `json:"react_outcome,omitempty"`
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
	channelResolver    *ChannelResolver
	mu                 sync.Mutex
	lastJudge          map[string]time.Time
	persistLocks       sync.Map
	cfgCache           map[uint]leadMiningConfigCacheEntry
	cfgGeneration      map[uint]uint64
	cfgCacheGeneration uint64
	// notifier 由 Router 装配点注入；单测构造 Service 时可不注入，此时
	// notifyLeadDiscovery 只记日志跳过，不再在 Service 内部隐式造带 DB 的服务。
	notifier CustomerOwnerNotifier
	// ruleService 账号/渠道两级规则解析（T01：发掘开关与关键词的唯一来源）。
	// 不注入时按全局 DB 构造；单测可注入替身。
	ruleService *LeadRuleService
	// ruleTargetFor 把消息解析为规则目标（account > channel，无 global 兜底）。
	// 不注入时走 ChannelResolver.ResolveByAccountRef；单测可注入替身避免依赖 DB。
	ruleTargetFor func(context.Context, *model.MessageHub) LeadRuleTarget
	// hitsProvider 仅供纯单测注入三路证据；生产为 nil，走真实 LeadDetectionService。
	// 副作用闸依赖 EvidenceComplete，租户隔离等不涉及证据完整性的测试用它声明
	// “本场景证据完整”，避免用空 Hits 误判为降级。
	hitsProvider func(context.Context, DetectionInput) ([]model.LeadMiningHit, error)
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	// LM-09：按账号/会话的尾判水位；内存只做唤醒，耐久所有权仍归 BridgeLeadTask/MessageHub。
	// 「仅入队」不算完成，只有判定耐久落库后才 NoteJudged 移除待处理。
	tail        *LeadTailWatermark
	tailSeq     atomic.Uint64
	tailHubs    sync.Map // key -> leadTailCandidate，每个水位保留最新一条用于尾判重放
	pending     chan *model.MessageHub
	tailMaxWait time.Duration

	// LM-03：判定记录仓储注入口。生产构造时装配并开启严格模式（落库失败即挡副作用）；
	// 未装配的单测保留旧的 fail-visible 兼容行为，不因此改变既有断言。
	judgementRepo   repository.LeadMiningJudgementRepository
	strictJudgement bool

	// LM-07：一次分析冻结一份有效配置快照。nil 时保持 legacy 判据（单测兼容）。
	configResolver *LeadMiningConfigResolver

	// H6：统一 ContextView 提供方（由装配点注入）；nil 时线索链保持既有有限历史路径。
	// 装配期写一次、worker 期只读，用 RWMutex 防并发读写竞态。
	contextViewProviderMu sync.RWMutex
	contextViewProvider   LeadContextViewProvider
}

// leadTailCandidate 水位对应的最新待判消息与序号；迟到旧任务不得覆盖新水位。
type leadTailCandidate struct {
	hub *model.MessageHub
	seq uint64
}

// 尾判重放时经 context 传递，避免为 force 改坏 s.process 的既有调用面。
type leadTailForceKey struct{}
type leadTailSeqKey struct{}

// leadTailKeyFor 与 process 的防抖键同账号边界，确保跨账号同发送人不互相抑制。
func leadTailKeyFor(hub *model.MessageHub) string {
	if hub == nil {
		return ""
	}
	key := fmt.Sprintf("%d|%s|%s|", hub.TenantID, strings.TrimSpace(hub.Platform), strings.TrimSpace(hub.AccountID))
	if hub.IsGroup {
		key += "g:" + strings.TrimSpace(hub.GroupID)
	} else {
		key += "c:" + strings.TrimSpace(hub.ConversationID)
	}
	return key
}

var singleton *Service

// 线索发掘渠道资格集（P0-c）：由 channelgw.Default 注册表在 init() 经
// SetLeadMiningChannels 一次性投影。只有 MineMode != none 的渠道才在默认准入集合内；
// 这是发掘资格的唯一真值源，service.ValidPlatform 只做 MessageHub 落库兜底校验。
var (
	leadMiningMu              sync.RWMutex
	leadMiningCapableChannels = map[string]struct{}{}
)

// SetLeadMiningChannels 由 channelgw.init() 注入发掘资格集合（仿 SetBridgeOutboxChannels）。
// 空注入忽略（防误清空：装配时序异常时宁可维持上一份集合，也不悄悄断掉全渠道发掘）。
func SetLeadMiningChannels(names []string) {
	if len(names) == 0 {
		logger.Errorf("[lead-mining] SetLeadMiningChannels called with empty names; keep previous set")
		return
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = struct{}{}
		}
	}
	if len(m) == 0 {
		return
	}
	leadMiningMu.Lock()
	defer leadMiningMu.Unlock()
	leadMiningCapableChannels = m
}

// LeadMiningChannels 返回当前发掘资格集合（排序副本；parity/诊断用）。
func LeadMiningChannels() []string {
	leadMiningMu.RLock()
	defer leadMiningMu.RUnlock()
	names := make([]string, 0, len(leadMiningCapableChannels))
	for n := range leadMiningCapableChannels {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// leadMiningChannelCapable 渠道是否在注册表投影的发掘资格集合内。
func leadMiningChannelCapable(platform string) bool {
	leadMiningMu.RLock()
	defer leadMiningMu.RUnlock()
	_, ok := leadMiningCapableChannels[strings.ToLower(strings.TrimSpace(platform))]
	return ok
}

// NewLeadMiningService 构造并启动线索发掘服务（后台 worker，非阻塞）。
// db 用于线索仓储的显式注入（不摸全局句柄）。
func NewLeadMiningService(clueRepo repository.ClueRepository) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		queue:         make(chan *model.MessageHub, leadMiningQueueSize),
		workers:       leadMiningWorkers,
		judge:         &leadReactDispatcherJudge{},
		custRepo:      repository.NewCustomerRepository(),
		clueRepo:      clueRepo,
		cfgRepo:       repository.NewLeadMiningConfigRepository(),
		hubRepo:       repository.NewMessageHubRepository(),
		ruleService:   NewLeadRuleService(),
		lastJudge:     make(map[string]time.Time),
		cfgCache:      make(map[uint]leadMiningConfigCacheEntry),
		cfgGeneration: make(map[uint]uint64),
		tail:          NewLeadTailWatermark(),
		pending:       make(chan *model.MessageHub, 1024),
		tailMaxWait:   leadMiningDebounce,
		judgementRepo: repository.NewLeadMiningJudgementRepository(),
		// 生产 fail-closed：判定记录落库失败时不得进入标签/通知/商机等确定性副作用。
		strictJudgement: true,
		configResolver:  NewLeadMiningConfigResolver(),
		cancel:          cancel,
	}
	singleton = s
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
	logger.Infof("[lead-mining] 服务已启动，worker=%d queue=%d", s.workers, leadMiningQueueSize)
	return s
}

// SetNotifier 注入线索负责人推送出口（Router 装配点调用，无内联闭包）。
func (s *Service) SetNotifier(n CustomerOwnerNotifier) {
	if s != nil && n != nil {
		s.notifier = n
	}
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

// ListLeadMiningJudgements 返回判定记录列表（最小只读 API，LM-PR5）。
func ListLeadMiningJudgements(ctx context.Context, page, limit int, platform, accountID, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	return repository.NewLeadMiningJudgementRepository().List(ctx, page, limit, platform, accountID, leadRole, matched)
}

// ListLeadMiningJudgementsScoped 按员工可见账号集合过滤判定记录（LM-13）。
// accounts=nil 表示管理员全量；受限员工传入可见账号集合，空集合拒绝/空列表由仓储处理。
func ListLeadMiningJudgementsScoped(ctx context.Context, accounts []string, page, limit int, platform, accountID, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	repo := repository.NewLeadMiningJudgementRepository()
	if accounts == nil {
		return repo.List(ctx, page, limit, platform, accountID, leadRole, matched)
	}
	if accountID != "" {
		allowed := false
		for _, a := range accounts {
			if strings.TrimSpace(a) == strings.TrimSpace(accountID) {
				allowed = true
				break
			}
		}
		if !allowed {
			return []model.LeadMiningJudgement{}, 0, nil
		}
		accounts = []string{accountID}
	}
	return repo.ListScoped(ctx, accounts, page, limit, platform, leadRole, matched)
}

// GetLeadMiningJudgementDetailScoped 按员工可见账号集合读取判定详情（LM-13）。
func GetLeadMiningJudgementDetailScoped(ctx context.Context, judgementID string, accounts []string) (*model.LeadMiningJudgement, error) {
	if strings.TrimSpace(judgementID) == "" {
		return nil, fmt.Errorf("judgement_id 不能为空")
	}
	repo := repository.NewLeadMiningJudgementRepository()
	if accounts == nil {
		return repo.GetByID(ctx, judgementID)
	}
	return repo.GetByIDScoped(ctx, judgementID, accounts)
}

// Stop 停止服务（进程退出时）
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// SetConfigResolver 注入一次分析冻结的有效配置快照解析器（装配点/测试用）。
func (s *Service) SetConfigResolver(r *LeadMiningConfigResolver) {
	if s != nil && r != nil {
		s.configResolver = r
	}
}

// Enqueue 非阻塞入队：核心业务只在消息落库后调用一次，绝不阻塞/入侵。
// LM-09：队列满不再静默丢弃——保留水位并转待处理缓冲，由 worker 补扫；
// 缓冲也满时记显式错误而非无声丢消息，水位保留供按 MessageHub 补扫恢复。
func (s *Service) Enqueue(hub *model.MessageHub) {
	if s == nil || hub == nil {
		return
	}
	if s.tail == nil {
		select {
		case s.queue <- hub:
		default:
			logger.Warnf("[lead-mining] 队列已满，丢弃消息 msgID=%s", hub.MsgID)
		}
		return
	}
	seq := s.tailSeq.Add(1)
	key := leadTailKeyFor(hub)
	at := time.Now()
	s.tailHubs.Store(key, leadTailCandidate{hub: hub, seq: seq})
	select {
	case s.queue <- hub:
		s.tail.NoteEnqueued(key, seq, at)
	default:
		s.tail.NoteQueueFull(key, seq, at)
		select {
		case s.pending <- hub:
			logger.Warnf("[lead-mining] 主队列已满，消息转入待处理缓冲 msgID=%s", hub.MsgID)
		default:
			logger.Errorf("[lead-mining] 待处理缓冲已满，水位保留待补扫 key=%s msgID=%s", key, hub.MsgID)
		}
	}
}

// tailFlushInterval 尾判扫描周期：不超过最大等待的一半，最低 200ms，避免忙轮询。
func (s *Service) tailFlushInterval() time.Duration {
	wait := s.tailMaxWait
	if wait <= 0 {
		wait = leadMiningDebounce
	}
	interval := wait / 2
	if interval < 200*time.Millisecond {
		interval = 200 * time.Millisecond
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.tailFlushInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.drainPending()
			s.flushDueTails(ctx)
		case hub := <-s.pending:
			select {
			case s.queue <- hub:
			default:
				// 主队列仍满：水位未清，下个周期继续补扫，不丢消息。
			}
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

// drainPending 把待处理缓冲里的消息重新投入主队列；投不进去的保持水位待下轮。
func (s *Service) drainPending() {
	if s.tail == nil {
		return
	}
	for {
		select {
		case hub := <-s.pending:
			select {
			case s.queue <- hub:
			default:
				s.pending <- hub
				return
			}
		default:
			return
		}
	}
}

// flushDueTails 最后一条入队后超过最大等待仍无判定结果时，强制重放最新消息做尾判。
// 尾判只在仍有 unfinished 水位时触发；判定落库成功后水位已被 NoteJudged 清除。
func (s *Service) flushDueTails(ctx context.Context) {
	if s.tail == nil {
		return
	}
	now := time.Now()
	s.tailHubs.Range(func(key, value any) bool {
		k, ok := key.(string)
		if !ok || !s.tail.ShouldFlush(k, now, s.tailMaxWait) {
			return true
		}
		cand, ok := value.(leadTailCandidate)
		if !ok || cand.hub == nil {
			return true
		}
		forced := context.WithValue(ctx, leadTailForceKey{}, true)
		forced = context.WithValue(forced, leadTailSeqKey{}, cand.seq)
		select {
		case s.queue <- cand.hub:
			logger.Infof("[lead-mining] 尾判重放 key=%s msgID=%s", k, cand.hub.MsgID)
		default:
			select {
			case s.pending <- cand.hub:
			default:
			}
		}
		return true
	})
}

// leadSideEffectsEligible 是业务副作用的统一准入闸：
// 标签、线索、通知、自动商机必须共用同一判定。降级或必需证据不全的高分结论
// 只能作为判定记录/候选保留，不得触发任何确定性业务副作用。
func leadSideEffectsEligible(jd *LeadJudgement, evidenceComplete bool, minScore int) bool {
	if jd == nil || !jd.IsLead || jd.IntentScore < minScore {
		return false
	}
	if jd.Degraded || !evidenceComplete {
		return false
	}
	if jd.ReactOutcome != nil && jd.ReactOutcome.Degraded {
		return false
	}
	return true
}

func (s *Service) process(ctx context.Context, hub *model.MessageHub, force ...bool) {
	if hub == nil || hub.Direction != "inbound" {
		return
	}
	// LM-09：尾判重放通过 variadic / context 传入，跳过 60 秒防抖直接分析末条。
	forced := len(force) > 0 && force[0]
	if v, ok := ctx.Value(leadTailForceKey{}).(bool); ok && v {
		forced = true
	}
	seq, _ := ctx.Value(leadTailSeqKey{}).(uint64)
	tailKey := leadTailKeyFor(hub)
	if hub.TenantID == 0 && tenant.IsMultiTenant() {
		logger.Warnf("[lead-mining] 拒绝无租户归属的消息 msgID=%s", hub.MsgID)
		return
	}
	if tid, ok := tenant.FromContext(ctx); ok && hub.TenantID != 0 && tid != hub.TenantID {
		logger.Warnf("[lead-mining] 拒绝上下文与消息租户不一致 msgID=%s", hub.MsgID)
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
	// 非文字占位符（[图片]/[表情]/[视频] 且无真实媒体）没有可判定的正文，
	// 不得触发线索发掘；否则关键词会命中占位文本，产出 degraded 的假线索。
	if IsNonTextPlaceholderHub(hub) {
		return
	}
	scope, scopeErr := s.historyScope(ctx, hub)
	if scopeErr != nil {
		logger.Warnf("[lead-mining] 拒绝不明确的历史作用域 msgID=%s: %v", hub.MsgID, scopeErr)
		return
	}
	ctx = context.WithValue(ctx, leadHistoryScopeKey{}, scope)
	debounceKey := leadMiningDebounceKey(scope.tenantID, scope.Platform, scope.accountKey, hub.ConversationID, hub.GroupID, hub.SenderID, hub.IsGroup)
	if debounceKey == "" {
		return
	}

	// T040：复用 ai_debounce.go 的窗口配置做线索去重；无 dedup_hash 的测试/历史
	// 消息不参与，避免把不同来源的消息误合并。
	if hub.DedupHash != "" {
		evt := &model.MessageEvent{Content: content, SessionID: hub.ConversationID}
		if !LeadDebounce().TryClaim(ctx, evt, debounceKey, hub.DedupHash) {
			logger.Infof("[lead-mining] 防抖命中，跳过重复消息 msgID=%s hash=%s", hub.MsgID, hub.DedupHash)
			return
		}
	}

	cfg := s.loadConfig(ctx)
	// 账号/渠道级规则是发掘开关的唯一权威源；legacy 单例配置只保留阈值/模型等参数默认值。
	// 规则缺失或查询失败一律 fail-closed：不发掘、不回退全局默认（T01）。
	rule, ruleErr := s.resolveProcessRule(ctx, hub)
	if ruleErr != nil {
		logger.Debugf("[lead-mining] 账号/渠道规则缺失或解析失败，不发掘 account=%s: %v", hub.AccountID, ruleErr)
		return
	}
	gate := leadMiningGateFor(cfg, rule, hub.Platform)
	if !gate.enabled {
		return
	}

	// LM-07：同一分析冻结一份有效配置快照，gate/判定/持久化共用；
	// 解析/查询故障 fail-closed，不再静默放大到全局启用或混用新旧版本。
	// FR-026：把本账号绑定的线索发现智能体传给解析器，让智能体的
	// model/persona/system_prompt 真正进入本次快照与判定提示。
	var agentSnapshot *LeadMiningConfigSnapshot
	if s.configResolver != nil {
		target := s.processRuleTarget(ctx, hub)
		snap, snapErr := s.configResolver.Resolve(ctx, target, s.processLeadAgentID(ctx, hub))
		if snapErr != nil || snap == nil {
			logger.Warnf("[lead-mining] 有效配置解析失败，fail-closed 不发掘 account=%s: %v", hub.AccountID, snapErr)
			return
		}
		// 上面已保证规则存在；快照是本次分析的唯一权威，规则关闭或智能体失效立即生效。
		if !snap.Enabled {
			return
		}
		if snap.MinIntentScore > gate.minIntentScore {
			gate.minIntentScore = snap.MinIntentScore
		}
		agentSnapshot = snap
		cfg = applyLeadMiningSnapshotToConfig(cfg, snap)
	}

	// 日志用粗粒度 account；LM-10 需求分桶键依赖 jd，在 persistLead 内计算。
	account := fmt.Sprintf("%s:%s", hub.Platform, hub.SenderID)
	if hub.IsGroup && strings.TrimSpace(hub.GroupID) != "" {
		account = fmt.Sprintf("%s:%s:%s", hub.Platform, hub.AccountID, hub.GroupID)
	}
	s.mu.Lock()
	if last, ok := s.lastJudge[debounceKey]; ok && time.Since(last) < leadMiningDebounce && !forced {
		s.mu.Unlock()
		// 防抖命中不等于已完成：水位由 Enqueue 记录，超时后尾判重放末条。
		return
	}
	s.mu.Unlock()

	history := s.fetchHistory(ctx, hub)
	if len(history) == 0 {
		return
	}
	historyDetection := s.fetchHistoryDetectionMessages(ctx, hub)

	// T039：组装多轮/历史 + 三路证据富上下文；fake judge（未实现可选方法）
	// 仍走原 Judge 路径，不改变既有测试契约。
	judgementID := fmt.Sprintf("lmj_%d", time.Now().UnixNano())
	jctx := s.buildLeadJudgeContext(ctx, hub, historyDetection, judgementID)
	// FR-026：冻结的线索智能体快照与判定共用同一版本，防止配置切换时
	// 网关走了新智能体、判定提示却还是旧智能体的 system_prompt。
	jctx.AgentSnapshot = agentSnapshot
	// LM-12：真实消息级快照与提示词共用同一批 hub 行；伪造/合成 ID 不在快照内。
	jctx.Evidence = NewLeadEvidenceSnapshot(s.fetchHistoryEvidence(ctx, hub))
	ctx = withLeadReactTask(ctx, buildLeadReactTask(ctx, hub, jctx, scope))
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
		logger.Warnf("[lead-mining] 二次 LLM 判定失败 account=%s: %v", account, err)
		// FR-012/Edge Case：二次 LLM 不可用时不得静默丢弃。
		// 用关键词/语义三路证据做确定性降级判定；无本地命中则确无信号，正常返回。
		jd = buildDegradedJudgement(jctx, cfg, err)
		if jd == nil {
			return
		}
		logger.Warnf("[lead-mining] 已降级判定 account=%s score=%d（证据不完整，前端需显示 degraded）",
			account, jd.IntentScore)
	}
	// 仅在判定成功后占用防抖窗口，避免失败消息阻塞后续判定（D-03）。
	s.mu.Lock()
	s.lastJudge[debounceKey] = time.Now()
	s.mu.Unlock()
	// LM-03：判定记录落库是副作用的前置检查点。失败时保留本次计算结果与失败原因，
	// 但不把它当作完整证据放行标签/通知/商机；水位保留，尾判可重试。
	if !s.recordLeadMiningJudgement(ctx, hub, jd, jctx) {
		logger.Warnf("[lead-mining] 判定落库失败，仅保留计算结果不进入副作用 msgID=%s", hub.MsgID)
		return
	}
	// LM-09：判定耐久保存后才清除待处理水位；迟到旧任务不会回退新水位。
	if s.tail != nil && tailKey != "" {
		s.tail.NoteJudged(tailKey, seq, time.Now())
	}
	// D8：判定成功后把结论写成会话历史摘要（compaction），供多轮/长会话复用。
	saveCompactSummary(ctx, hub, jctx.ChannelAccountID, jd)
	// 统一业务副作用准入：判定记录/候选已落库，判定自身降级（含 React 降级）不得触发标签/通知/商机。
	// 证据完整性统一取 jctx.EvidenceComplete()，与判定记录同一口径；三路全降级、
	// 群聚合失败或无完整命中时只保留候选/回放，不写线索标签、通知或商机。
	if !leadSideEffectsEligible(jd, jctx.EvidenceComplete(), gate.minIntentScore) {
		return
	}

	s.persistLead(ctx, cfg, hub, account, jd, jctx)
}

// applyLeadMiningSnapshotToConfig 把冻结快照的执行字段并入本次分析使用的配置副本，
// 不写回缓存/DB（快照只在本次分析生命周期内有效），避免改配置时前后消费不同版本。
func applyLeadMiningSnapshotToConfig(cfg *model.LeadMiningConfig, snap *LeadMiningConfigSnapshot) *model.LeadMiningConfig {
	out := cloneLeadMiningConfig(cfg)
	if out == nil {
		out = &model.LeadMiningConfig{}
	}
	if snap == nil {
		return out
	}
	if snap.MinIntentScore > 0 {
		out.MinIntentScore = snap.MinIntentScore
	}
	if strings.TrimSpace(snap.Model) != "" {
		out.Model = snap.Model
	}
	if strings.TrimSpace(snap.Requirement) != "" {
		out.Requirement = snap.Requirement
	}
	if len(snap.Keywords) > 0 {
		out.Keywords = append(model.JSONStrings(nil), snap.Keywords...)
	}
	return out
}

// leadMiningGate 发掘准入判据：rule 是唯一开关来源，cfg 只提供默认阈值。
type leadMiningGate struct {
	enabled        bool
	minIntentScore int
}

// leadMiningGateFor 计算一条消息是否进入发掘链路。
//
// 无 rule ⇒ disabled（fail-closed，不再按 legacy cfg.Enabled/channels 放行）；
// rule.enabled 是唯一开关；阈值仍从 legacy 配置取参数默认值（非规则兜底）。
func leadMiningGateFor(cfg *model.LeadMiningConfig, rule *model.LeadMiningRule, platform string) leadMiningGate {
	score := 50
	if cfg != nil && cfg.MinIntentScore > 0 {
		score = cfg.MinIntentScore
	}
	if rule == nil {
		return leadMiningGate{minIntentScore: score}
	}
	return leadMiningGate{enabled: rule.Enabled, minIntentScore: score}
}

// resolveProcessRule 用账号/渠道两级规则解析本条消息的发掘规则。
// ruleTargetFor 是可注入的消息→规则目标解析通道（测试替身用）；生产走 ChannelResolver。
func (s *Service) resolveProcessRule(ctx context.Context, hub *model.MessageHub) (*model.LeadMiningRule, error) {
	rs := s.ruleService
	if rs == nil {
		rs = NewLeadRuleService()
	}
	return rs.Resolve(ctx, s.processRuleTarget(ctx, hub))
}

func (s *Service) processRuleTarget(ctx context.Context, hub *model.MessageHub) LeadRuleTarget {
	if s.ruleTargetFor != nil {
		return s.ruleTargetFor(ctx, hub)
	}
	var target LeadRuleTarget
	if hub == nil {
		return target
	}
	if scope, err := s.historyScope(ctx, hub); err == nil {
		target.ChannelAccountID = scope.accountID
		target.ChannelID = scope.channelID
	}
	return target
}

// processLeadAgentID 取本条消息所属渠道账号绑定的线索发现智能体 ID（FR-026）。
// 账号解析失败/歧义时返回 0：解析器退化为仅用账号/渠道规则字段，不阻断发掘主链。
// historyScope 在同一次分析内按 context 缓存，重复调用不会重复查库。
func (s *Service) processLeadAgentID(ctx context.Context, hub *model.MessageHub) uint {
	if s == nil || hub == nil {
		return 0
	}
	scope, err := s.historyScope(ctx, hub)
	if err != nil || scope == nil {
		return 0
	}
	return scope.leadMiningAgentID
}

// buildDegradedJudgement 二次 LLM 不可用时的确定性降级判定。
// 只认关键词/语义两路完整且确实命中的证据；LLM 未初始化/超时的 hit 不算正向信号。
// 评分保守取 60（高于默认阈值 50 但低于高意向 70），并明确标注 Degraded/证据不完整。
func buildDegradedJudgement(jctx *LeadJudgeContext, cfg *model.LeadMiningConfig, judgeErr error) *LeadJudgement {
	if jctx == nil || cfg == nil {
		return nil
	}
	signals := 0
	keywords := make([]string, 0)
	for _, h := range jctx.Hits {
		if h.Type != "keyword" && h.Type != "semantic" {
			continue
		}
		if !h.Complete || h.Degraded {
			continue
		}
		if n, _ := h.Payload["match_count"].(int); n > 0 {
			signals += n
			if matched, ok := h.Payload["matched"].([]map[string]any); ok {
				for _, m := range matched {
					if kw, _ := m["keyword"].(string); kw != "" {
						keywords = append(keywords, kw)
					}
				}
			}
		}
	}
	if signals == 0 {
		return nil // 无本地正向证据：确无线索信号，不是被 LLM 故障吞掉
	}
	var reactOutcome *LeadReactOutcome
	var reactErr *leadReactJudgeError
	if errors.As(judgeErr, &reactErr) && reactErr.react != nil {
		reactOutcome = reactErr.react
	}
	score := 60
	if cfg.MinIntentScore > score {
		score = cfg.MinIntentScore
	}
	if score > leadMiningHighOpportunity-1 {
		score = leadMiningHighOpportunity - 1 // 降级判定不得自动进「高意向商机」
	}
	reason := "二次 LLM 不可用，基于关键词/语义命中降级判定；证据不完整，需人工复核"
	if judgeErr != nil {
		reason += fmt.Sprintf("（%v）", judgeErr)
	}
	return &LeadJudgement{
		IsLead:          true,
		IntentScore:     score,
		ClueType:        "待分类",
		MatchedKeywords: dedupeLeadStrings(keywords),
		MatchedTags:     []string{"LLM降级判定"},
		Summary:         "LLM 不可用时由本地命中生成的降级线索",
		Confidence:      0.4,
		Reason:          reason,
		LeadRole:        "unknown",
		Degraded:        true,
		ReactOutcome:    reactOutcome,
	}
}

func dedupeLeadStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
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

// loadHistoryHubs 是唯一的历史读取口：LM-12 的证据快照与 llm 历史共用同一批真实 hub 行，
// 避免提示词与证据引用来自不同窗口。
func (s *Service) loadHistoryHubs(ctx context.Context, hub *model.MessageHub) []model.MessageHub {
	if s == nil || s.hubRepo == nil || hub == nil {
		return nil
	}
	scope, scopeErr := s.historyScope(ctx, hub)
	if scopeErr != nil {
		logger.Warnf("[lead-mining] 拒绝不明确的历史作用域: %v", scopeErr)
		return nil
	}
	var (
		hubs []model.MessageHub
		err  error
	)
	// LM-PR1: 单聊结合两人历史（同 conversation 双向）；群聊结合群内大家发言。
	if hub.IsGroup && hub.GroupID != "" {
		hubs, err = s.hubRepo.ListRecentByGroup(ctx, scope.MessageHubHistoryScope, hub.GroupID, leadMiningHistorySize)
	} else if hub.ConversationID != "" {
		hubs, err = s.hubRepo.ListRecentByConversation(ctx, scope.MessageHubHistoryScope, hub.ConversationID, leadMiningHistorySize)
	} else {
		hubs, err = s.hubRepo.ListRecentBySender(ctx, scope.MessageHubHistoryScope, hub.SenderID, leadMiningHistorySize)
	}
	if err != nil {
		logger.Warnf("[lead-mining] 读取会话历史失败: %v", err)
		return nil
	}
	// DESC -> ASC
	for i, j := 0, len(hubs)-1; i < j; i, j = i+1, j-1 {
		hubs[i], hubs[j] = hubs[j], hubs[i]
	}
	// 非文字占位符不是可用判定上下文：历史里大量 [表情]/[图片] 会稀释真实正文，
	// 并让关键词命中占位文本。证据快照 fetchHistoryEvidence 复用本函数，口径一致。
	filtered := make([]model.MessageHub, 0, len(hubs))
	for _, h := range hubs {
		if IsNonTextPlaceholderHub(&h) {
			continue
		}
		filtered = append(filtered, h)
	}
	return filtered
}

// fetchHistoryEvidence 取本次授权快照的真实消息行；合成 history_N 不进入快照。
func (s *Service) fetchHistoryEvidence(ctx context.Context, hub *model.MessageHub) []model.MessageHub {
	hubs := s.loadHistoryHubs(ctx, hub)
	return append(hubs, *hub)
}

func (s *Service) fetchHistoryDB(ctx context.Context, hub *model.MessageHub) []llm.ChatMessage {
	hubs := s.loadHistoryHubs(ctx, hub)
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

func (s *Service) persistLead(ctx context.Context, cfg *model.LeadMiningConfig, hub *model.MessageHub, account string, jd *LeadJudgement, jctxs ...*LeadJudgeContext) {
	// LM-07：配置解析失败/未装配时不得因 cfg==nil 在 cfg.Tags 处 panic。
	if cfg == nil {
		cfg = &model.LeadMiningConfig{}
	}
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			logger.Warnf("[lead-mining] 拒绝无租户归属的线索写入 account=%s", account)
			return
		}
		tenantID = tenant.DefaultTenantID
	}
	// LM-10：分桶键用于并发锁隔离（同群不同主体/项目/方向互不阻塞）与后续去重升级；
	// clue.Account 保留既有 "platform:sender" 语义，避免破坏下游查询与历史验收口径。
	demandKey := leadDemandKeyFor(*hub, hub.SenderID, leadDemandProject(jd.Summary), leadRoleOf(jd))
	lockKey := fmt.Sprintf("%d:%d:%s", tenantID, ClueTypeLeadMining, demandKey)
	if v, _ := s.persistLocks.LoadOrStore(lockKey, &sync.Mutex{}); v != nil {
		mu := v.(*sync.Mutex)
		mu.Lock()
		defer mu.Unlock()
	}
	customer, err := s.resolveCustomerForHub(ctx, hub)
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

	var jctx *LeadJudgeContext
	if len(jctxs) > 0 {
		jctx = jctxs[0]
	}
	clueType := ClueTypeLeadMining
	nowJudged := time.Now().Unix()
	evidenceJSON := leadMiningEvidenceJSON(hub, jd, jctx)
	sourceHitIDs := leadMiningSourceHitIDs(jctx)
	status := classifyLeadStatus(jd, cfg) // D11/D12：低分/证据不完整进 candidate 待审
	conversationType := "private"
	if hub.IsGroup {
		conversationType = "group"
	}
	// LM-14：归档时间只有归档真正成功才写；候选/降级结论不伪填。
	archiveEligible := leadArchiveAllowed(status == "candidate", jd.Degraded)
	var archivedAt *int64
	if archiveEligible {
		at := nowJudged
		archivedAt = &at
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
		LeadRole:           normalizeLeadRole(jd.LeadRole),
		Status:             status,
		EvidenceJSON:       evidenceJSON,
		Summary:            jd.Summary,
		FollowupSuggestion: jd.FollowupSuggestion,
		LastJudgedAt:       &nowJudged,
		SourceChannel:      hub.Platform,
		SourceAccountID:    hub.AccountID,
		ConversationType:   conversationType,
		// LM-14：四条写入路径共用的归档关联字段，使 360/证据回放可回查真实主体与来源。
		CustomerID:   customer.ID,
		UserInfo:     leadMiningUserInfo(customer, hub),
		SourceHitIDs: sourceHitIDs,
		ArchivedAt:   archivedAt,
	}

	updates := func(statusValue string) map[string]any {
		m := map[string]any{
			"status":              statusValue,
			"intent_score":        int64(jd.IntentScore),
			"desc":                jd.Summary,
			"is_opportunity":      boolToInt(jd.IntentScore >= leadMiningHighOpportunity),
			"conversation_id":     hub.ConversationID,
			"message_id":          fmt.Sprintf("%d", hub.ID),
			"is_group":            hub.IsGroup,
			"group_id":            hub.GroupID,
			"lead_role":           normalizeLeadRole(jd.LeadRole),
			"evidence_json":       evidenceJSON,
			"summary":             jd.Summary,
			"followup_suggestion": jd.FollowupSuggestion,
			"last_judged_at":      nowJudged,
			"source_channel":      hub.Platform,
			"source_account_id":   hub.AccountID,
			"conversation_type":   conversationType,
			"customer_id":         customer.ID,
			"user_info":           leadMiningUserInfo(customer, hub),
			"source_hit_ids":      sourceHitIDs,
		}
		if archiveEligible {
			m["archived_at"] = archivedAt
		}
		return m
	}

	existing, gerr := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account)
	if gerr == nil && existing != nil {
		promoted := promoteStatus(existing.Status, jd, cfg) // D14：高分自动转正，只升不降
		if err := s.clueRepo.UpdateByID(ctx, existing.ID, updates(promoted)); err != nil {
			// LM-14：归档写入失败即停止，不得继续通知/转商机造成假成功。
			logger.Warnf("[lead-mining] 更新线索失败，停止后续副作用 account=%s: %v", account, err)
			return
		}
		// D14：candidate 被后续高分判定自动转正时补发通知（此前 candidate 被抑制）。
		if existing.Status == "candidate" && promoted != "candidate" {
			existing.Status = promoted
			s.notifyLeadDiscovery(ctx, existing, jd)
		}
		s.tryConvertToOpportunity(ctx, existing.ID, customer, jd)
		return
	}
	if err := s.clueRepo.Create(ctx, clue); err != nil {
		// TOCTOU 兜底：并发插入冲突时回查已有记录并转为更新（D-01）。
		if existing2, gerr := s.clueRepo.FindByTypeAndAccount(ctx, clueType, account); gerr == nil && existing2 != nil {
			// 兜底更新与新建写入同字段，不能只写 score/desc/last_judged_at 三个字段。
			if uerr := s.clueRepo.UpdateByID(ctx, existing2.ID, updates(promoteStatus(existing2.Status, jd, cfg))); uerr != nil {
				logger.Warnf("[lead-mining] TOCTOU 兜底更新线索失败，停止后续副作用 account=%s: %v", account, uerr)
				return
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

// leadMiningEvidenceJSON 把真实消息引用（MsgID/说话人/平台/原文/方向）冻结进证据 JSON。
// 合成 history_N 不属于精确证据，只有 LeadJudgeContext 的真实消息才参与。
func leadMiningEvidenceJSON(hub *model.MessageHub, jd *LeadJudgement, jctx *LeadJudgeContext) string {
	rows := make([]map[string]any, 0, 4)
	if jctx != nil && jctx.Evidence != nil {
		for _, id := range jctx.Evidence.RealMessageIDs() {
			rows = append(rows, map[string]any{
				"message_id": id, "conversation_id": hub.ConversationID,
				"platform": hub.Platform, "reason": jd.Reason, "coverage": "snapshot",
			})
		}
	}
	rows = append(rows, map[string]any{
		"message_id": hub.MsgID, "conversation_id": hub.ConversationID,
		"sender_id": hub.SenderID, "platform": hub.Platform, "reason": jd.Reason, "coverage": "current",
	})
	b, _ := json.Marshal(rows)
	return string(b)
}

func leadMiningSourceHitIDs(jctx *LeadJudgeContext) model.JSONStrings {
	if jctx == nil {
		return model.JSONStrings{}
	}
	out := make(model.JSONStrings, 0, len(jctx.Hits))
	for _, h := range jctx.Hits {
		out = append(out, strconv.FormatInt(h.ID, 10))
	}
	return out
}

// leadMiningUserInfo 保留归档时的客户画像与成员身份快照，不凭昵称强并身份。
func leadMiningUserInfo(c *model.Customer, hub *model.MessageHub) string {
	if c == nil {
		return "{}"
	}
	b, _ := json.Marshal(map[string]any{
		"customer_id": c.ID, "unified_id": c.UnifiedID, "name": c.Name,
		"platform": hub.Platform, "sender_id": hub.SenderID, "sender_name": hub.SenderName,
		"conversation_id": hub.ConversationID, "is_group": hub.IsGroup, "group_id": hub.GroupID,
	})
	return string(b)
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
	// D11/P3：candidate 待审线索不触发外部通知，避免骚扰；人工确认或高分自动转正后再通知。
	if clue.Status == "candidate" {
		logger.Debugf("[lead-mining] 待审线索 %s 跳过负责人通知", clue.ID)
		return
	}
	// FR-015：自动化发掘链路也要按配置路由推送负责人，而不是只发一条 user_id=0
	// 的全局广播。手动归档端点（/api/manage/leads/archive）已走 LeadNotifyService；
	// 这里补上同一出口，确保 account > channel > tenant 三级路由与送达记录都生效。
	if s.notifier == nil {
		logger.Debugf("[lead-mining] 通知出口未装配，线索 %s 跳过负责人推送", clue.ID)
	} else if err := s.notifier.NotifyOwner(ctx, ArchiveClueInput{
		ClueID:          clue.ID,
		CustomerID:      clue.CustomerID,
		SourceChannel:   clue.SourceChannel,
		SourceAccountID: clue.SourceAccountID,
		Summary:         clue.Summary,
	}, clue.CustomerID); err != nil {
		logger.Warnf("[lead-mining] 配置路由推送负责人失败 clue=%s: %v", clue.ID, err)
	}
	// P5：去掉 user_id=0 全局广播，收敛为单一 NotifyOwner 出口（按账号→渠道→租户路由）。
	// 文案（群名/角色/建议）由 ArchiveClueInput 承载，在下游统一构造。
}

// leadMiningUnifiedIDFor 为线索入口生成稳定且带账号作用域的 OneID。
// 有专用身份列的渠道使用 GenerateCustomerUnifiedID（按真实平台身份归一）；
// 无专用列的渠道使用 tenant/account 作用域的确定性键，不再依赖随机 UUID。
func leadMiningUnifiedIDFor(platform, sourceAccountID, senderID string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	id := strings.TrimSpace(senderID)
	if p == "" || id == "" {
		return ""
	}
	candidate := &model.Customer{Name: id}
	applyLeadMiningIdentity(candidate, p, id)
	// 只有写入了专用身份列才交给 OneID 归一；GenerateCustomerUnifiedID 对无身份列
	// 渠道返回随机 UUID，不能作为稳定客户键。
	hasDedicated := candidate.WechatOpenID != "" || candidate.PersonalWXOpenID != "" || candidate.QQOpenID != "" ||
		candidate.WeComExternalID != "" || candidate.FeishuOpenID != "" || candidate.DouyinOpenID != "" ||
		candidate.TikTokOpenID != "" || candidate.KuaishouOpenID != "" || candidate.XiaohongshuID != "" ||
		candidate.XianyuID != "" || candidate.TelegramChatID != 0 || candidate.WhatsAppPhone != "" ||
		candidate.Email != "" || candidate.Phone != ""
	if hasDedicated {
		if unified := model.GenerateCustomerUnifiedID(candidate); unified != "" {
			return unified
		}
	}
	scope := strings.TrimSpace(sourceAccountID)
	if scope == "" {
		scope = "unknown"
	}
	return fmt.Sprintf("lm:%s:%s:%s", p, scope, id)
}

func (s *Service) resolveCustomer(ctx context.Context, platform, sourceAccountID, senderID, name string) (*model.Customer, error) {
	if platform == "" || senderID == "" {
		return nil, fmt.Errorf("无客户身份标识")
	}
	// LM-04：来源账号作用域优先；带来源账号时不得先回退到无作用域的 legacy
	// lm:platform:sender 键，避免不同接入账号的同名联系人被合并。legacy 键仅在
	// 缺少可信来源账号（历史/迁移场景）时作为兼容回退。
	legacyKey := "lm:" + platform + ":" + senderID
	scoped := strings.TrimSpace(sourceAccountID) != ""
	unifiedID := leadMiningUnifiedIDFor(platform, sourceAccountID, senderID)
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
	if !scoped {
		if c, gerr := s.custRepo.GetByUnifiedID(ctx, legacyKey); gerr == nil && c != nil {
			if name != "" && (c.Name == "" || c.Name == legacyKey) {
				c.Name = name
				_ = s.custRepo.Update(ctx, c)
			}
			return c, nil
		}
	}
	candidate := &model.Customer{Name: name}
	applyLeadMiningIdentity(candidate, platform, senderID)
	candidate.UnifiedID = unifiedID
	candidate.SourceChannel = platform
	candidate.SourceAccountID = sourceAccountID
	if cerr := s.custRepo.Create(ctx, candidate); cerr != nil {
		if c2, g2 := s.custRepo.GetByUnifiedID(ctx, unifiedID); g2 == nil && c2 != nil {
			return c2, nil
		}
		if !scoped && unifiedID != legacyKey {
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
	case "personal_wx", "wx_personal", "wechat_personal":
		// D15：个微独立身份列，同 sender 跨消息 OneID 稳定（不再回退随机 UUID）。
		c.PersonalWXOpenID = id
	case "qq":
		// D15：QQ 独立身份列。
		c.QQOpenID = id
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
		// sms/web/dingtalk 等暂无专用身份列，回退 legacy lm: key
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

// channelEnabled 判一个渠道是否参与发掘（P0-c）。
//
// 语义改为**默认全纳入、显式关闭**：未配置 / Channels 为空 ⇒ 所有具备发掘能力
// （channelgw 投影的 MineMode != none 集合）的渠道都参与；租户配置只表达显式关闭
// 某渠道（cfg.Channels 作为关闭名单），不再把「名单非空但漏一个」解读成静默全关。
// capability 集合为空（装配异常/单测）时不阻断——保留 legacy 行为，避免断链。
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

// recordLeadMiningJudgement 把判定结果耐久落库，返回是否成功。
// 生产构造开启 strictJudgement：落库失败必须挡住下游副作用；未装配 DB 的单测
// 保留 fail-visible 兼容行为（只记日志），不改变既有断言。
func (s *Service) recordLeadMiningJudgement(ctx context.Context, hub *model.MessageHub, jd *LeadJudgement, jctxs ...*LeadJudgeContext) bool {
	if hub == nil || jd == nil {
		return false
	}
	judgementRepo := s.judgementRepo
	if judgementRepo == nil {
		judgementRepo = repository.NewLeadMiningJudgementRepository()
	}
	if judgementRepo == nil {
		return false
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			return false
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
		// LM-03：证据完整性与副作用准入闸共用 LeadJudgeContext.EvidenceComplete() 口径：
		// 必需证据至少一路完整命中即可，可选路/局部覆盖降级不一概挡单；落库与闸门不再分叉。
		rec.EvidenceComplete = c.EvidenceComplete()
		evidenceDegraded := c.GroupAggregateFailed || c.ReactDegraded
		for _, h := range c.Hits {
			rec.SourceHitIDs = append(rec.SourceHitIDs, strconv.FormatInt(h.ID, 10))
			if h.Degraded {
				evidenceDegraded = true
			}
		}
		if evidenceDegraded {
			rec.Degraded = true
		}
		// LM-03：群聚合失败 / ReAct 降级必须落库可见，不能只藏在 reason 文本。
		if c.GroupAggregateFailed || c.ReactDegraded || c.DetectionFailed {
			rec.EvidenceComplete = false
			rec.Degraded = true
		}
		// LM-12：引用的原文/说话人/方向/时间由上下文快照保留；落库失败原因不能
		// 被当成空证据——结果仍在 ResultJSON 里，只是 EvidenceComplete 关闭。
		if c.Evidence != nil {
			rec.ContextMessageIDs = c.Evidence.RealMessageIDs()
		}
	}
	// 二次 LLM 降级判定必须让前端/回放看到 degraded，不能只藏在 reason 文本里。
	if jd.Degraded {
		rec.Degraded = true
		rec.EvidenceComplete = false
	}
	if jd.ReactOutcome != nil && jd.ReactOutcome.Degraded {
		rec.Degraded = true
		rec.EvidenceComplete = false
	}
	if err := judgementRepo.Create(ctx, rec); err != nil {
		logger.Warnf("[lead-mining] 记录判定失败 msg=%s: %v", hub.MsgID, err)
		// 生产 fail-closed；未装配仓储的单测只记日志（既有契约）。
		return !s.strictJudgement
	}
	return true
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

// GetLeadMiningQualityStatsScoped 按员工可见账号统计判定质量（LM-13）。
// accounts=nil 表示管理员全量；空切片表示受限无账号，repository 会拒绝。
func GetLeadMiningQualityStatsScoped(ctx context.Context, accounts []string, days int) (map[string]any, error) {
	return repository.NewLeadMiningJudgementRepository().GetQualityStatsScoped(ctx, accounts, days)
}

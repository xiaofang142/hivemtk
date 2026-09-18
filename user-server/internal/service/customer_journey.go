package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	journeyStateKeyPrefix = "journey:state:"

	journeyL1TTL = 60 * time.Second

	journeyTTLAcquisition   = 30 * 24 * time.Hour
	journeyTTLTransactional = 90 * 24 * time.Hour
	journeyTTLSleeping      = 180 * 24 * time.Hour
	journeyTTLLost          = 365 * 24 * time.Hour
	journeyTTLDefault       = 30 * 24 * time.Hour
)

func journeyStateKey(customerID string) string {
	return journeyStateKeyPrefix + customerID
}

func journeyStateTTL(stage JourneyStage) time.Duration {
	switch stage {
	case StageStranger, StageLead, StageContact, StageInterested:
		return journeyTTLAcquisition
	case StageQuoted, StageWon, StageAfterSale, StageRepurchase:
		return journeyTTLTransactional
	case StageSleeping:
		return journeyTTLSleeping
	case StageLost:
		return journeyTTLLost
	default:
		return journeyTTLDefault
	}
}

func cloneJourneyState(src *JourneyState) JourneyState {
	if src == nil {
		return JourneyState{}
	}
	dst := *src
	if src.StageHistory != nil {
		dst.StageHistory = append([]JourneyEvent(nil), src.StageHistory...)
	}
	if src.AutoTags != nil {
		dst.AutoTags = append([]string(nil), src.AutoTags...)
	}
	if src.Metadata != nil {
		dst.Metadata = make(map[string]string, len(src.Metadata))
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	return dst
}

// JourneyStage 客户旅程阶段
// 已迁移至 dto 包，此处保留类型别名以维持向后兼容
type JourneyStage = dto.JourneyStage

// StageXxx 常量别名（与 dto.StageXxx 一致）
const (
	StageStranger   = dto.StageStranger
	StageLead       = dto.StageLead
	StageContact    = dto.StageContact
	StageInterested = dto.StageInterested
	StageQuoted     = dto.StageQuoted
	StageWon        = dto.StageWon
	StageAfterSale  = dto.StageAfterSale
	StageRepurchase = dto.StageRepurchase
	StageSleeping   = dto.StageSleeping
	StageLost       = dto.StageLost
)

// AllStages 所有阶段
// 已迁移至 dto 包，此处保留变量别名以维持向后兼容
var AllStages = dto.AllStages

// StageMeta 阶段元信息
type StageMeta struct {
	Stage           JourneyStage  `json:"stage"`
	Label           string        `json:"label"`
	Description     string        `json:"description"`
	DefaultFollowup time.Duration `json:"default_followup"`
	RecommendedSOP  string        `json:"recommended_sop"`
	OwnerRole       string        `json:"owner_role"`
	AllowAIHandle   bool          `json:"allow_ai_handle"`
	AutoNextStage   JourneyStage  `json:"auto_next_stage,omitempty"`
}

// StageMetas 阶段配置
var StageMetas = map[JourneyStage]*StageMeta{
	StageStranger: {
		Stage: StageStranger, Label: "陌生客户", Description: "首次接触，未留资",
		DefaultFollowup: 0, RecommendedSOP: "welcome_greeting", OwnerRole: "ai",
		AllowAIHandle: true,
	},
	StageLead: {
		Stage: StageLead, Label: "已留资", Description: "留下联系方式，待分配",
		DefaultFollowup: 30 * time.Minute, RecommendedSOP: "first_contact", OwnerRole: "ai",
		AllowAIHandle: true,
	},
	StageContact: {
		Stage: StageContact, Label: "已加微", Description: "添加企微/微信成功",
		DefaultFollowup: 24 * time.Hour, RecommendedSOP: "value_proposition", OwnerRole: "ai",
		AllowAIHandle: true,
	},
	StageInterested: {
		Stage: StageInterested, Label: "有意向", Description: "主动咨询产品/价格",
		DefaultFollowup: 4 * time.Hour, RecommendedSOP: "product_intro", OwnerRole: "ai+sales",
		AllowAIHandle: true,
	},
	StageQuoted: {
		Stage: StageQuoted, Label: "已报价", Description: "已发送价格/方案，等待决策",
		DefaultFollowup: 24 * time.Hour, RecommendedSOP: "objection_handling", OwnerRole: "sales",
		AllowAIHandle: true, AutoNextStage: StageInterested,
	},
	StageWon: {
		Stage: StageWon, Label: "已成交", Description: "已下单/已付款",
		DefaultFollowup: 0, RecommendedSOP: "thank_you", OwnerRole: "sales",
		AllowAIHandle: true, AutoNextStage: StageAfterSale,
	},
	StageAfterSale: {
		Stage: StageAfterSale, Label: "售后中", Description: "服务履约/已交付",
		DefaultFollowup: 7 * 24 * time.Hour, RecommendedSOP: "after_sale_care", OwnerRole: "cs",
		AllowAIHandle: true, AutoNextStage: StageRepurchase,
	},
	StageRepurchase: {
		Stage: StageRepurchase, Label: "复购期", Description: "服务完成后 30 天内",
		DefaultFollowup: 15 * 24 * time.Hour, RecommendedSOP: "repurchase_reminder", OwnerRole: "ai",
		AllowAIHandle: true, AutoNextStage: StageSleeping,
	},
	StageSleeping: {
		Stage: StageSleeping, Label: "沉睡", Description: "30-90 天无互动",
		DefaultFollowup: 7 * 24 * time.Hour, RecommendedSOP: "reactivation", OwnerRole: "ai",
		AllowAIHandle: true, AutoNextStage: StageLost,
	},
	StageLost: {
		Stage: StageLost, Label: "已流失", Description: "明确拒绝/拉黑/90天以上未互动",
		DefaultFollowup: 0, RecommendedSOP: "win_back", OwnerRole: "sales",
		AllowAIHandle: false,
	},
}

// JourneyEvent 旅程事件
type JourneyEvent struct {
	Type       string         `json:"type"`
	FromStage  JourneyStage   `json:"from_stage"`
	ToStage    JourneyStage   `json:"to_stage"`
	Reason     string         `json:"reason"`
	Source     string         `json:"source"`
	OperatorID string         `json:"operator_id"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
}

// JourneyState 旅程状态
type JourneyState struct {
	CustomerID   string            `json:"customer_id"`
	OneID        string            `json:"one_id"`
	CurrentStage JourneyStage      `json:"current_stage"`
	StageSince   time.Time         `json:"stage_since"`
	StageHistory []JourneyEvent    `json:"stage_history"`
	LastTouchAt  time.Time         `json:"last_touch_at"`
	TotalTouches int               `json:"total_touches"`
	AutoTags     []string          `json:"auto_tags"`
	Metadata     map[string]string `json:"metadata"`
}

// CustomerJourneyService 客户旅程服务
//
// P-5 多实例权威化：读路径以 Redis（L2）为权威源，内存（L1）仅作 60s 读缓存；
// 写路径双写（L1 + L2）保持兼容。
type CustomerJourneyService struct {
	mu          sync.RWMutex
	states      map[string]*JourneyState
	l1ExpiresAt map[string]time.Time
	cache       cache.Cache
	subscribers []JourneySubscriber
}

// JourneySubscriber 旅程订阅者
type JourneySubscriber interface {
	OnJourneyEvent(ctx context.Context, customerID string, event *JourneyEvent)
}

// NewCustomerJourneyService 创建客户旅程服务（默认注入全局缓存后端）
func NewCustomerJourneyService() *CustomerJourneyService {
	return &CustomerJourneyService{
		states: make(map[string]*JourneyState), l1ExpiresAt: make(map[string]time.Time),
		cache: cache.GetGlobalCache(),
	}
}

// NewCustomerJourneyServiceWithCache 使用指定缓存后端创建服务（依赖注入用）
func NewCustomerJourneyServiceWithCache(c cache.Cache) *CustomerJourneyService {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	return &CustomerJourneyService{
		states: make(map[string]*JourneyState), l1ExpiresAt: make(map[string]time.Time),
		cache: c,
	}
}

// SetCache 注入/替换缓存后端（main 装配或测试场景）
func (s *CustomerJourneyService) SetCache(c cache.Cache) {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	s.mu.Lock()
	s.cache = c
	s.mu.Unlock()
}

func (s *CustomerJourneyService) persistState(ctx context.Context, state *JourneyState) {
	if state == nil || state.CustomerID == "" {
		return
	}
	if s.cache == nil {
		return
	}
	ttl := journeyStateTTL(state.CurrentStage)
	if err := s.cache.SetJSON(ctx, journeyStateKey(state.CustomerID), state, ttl); err != nil {
		logger.Warnf("journey.persistState(%s) stage=%s error: %v",
			state.CustomerID, state.CurrentStage, err)
	}
}

func (s *CustomerJourneyService) getLiveL1(customerID string) (*JourneyState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[customerID]
	if !ok {
		return nil, false
	}
	if exp, ok := s.l1ExpiresAt[customerID]; ok && time.Now().After(exp) {
		delete(s.states, customerID)
		delete(s.l1ExpiresAt, customerID)
		return nil, false
	}
	c := cloneJourneyState(state)
	return &c, true
}

func (s *CustomerJourneyService) putL1(customerID string, state *JourneyState) {
	s.mu.Lock()
	stored := cloneJourneyState(state)
	s.states[customerID] = &stored
	s.l1ExpiresAt[customerID] = time.Now().Add(journeyL1TTL)
	s.mu.Unlock()
}

func (s *CustomerJourneyService) loadAuthoritative(ctx context.Context, customerID string) (*JourneyState, bool) {
	if st, ok := s.getLiveL1(customerID); ok {
		return st, true
	}
	if s.cache != nil {
		var loaded JourneyState
		if err := s.cache.GetJSON(ctx, journeyStateKey(customerID), &loaded); err == nil && loaded.CustomerID != "" {
			stored := loaded
			s.putL1(customerID, &stored)
			out := loaded
			return &out, true
		}
	}
	return nil, false
}

// GetState 获取客户旅程状态（L1 读缓存 → L2 Redis 权威 → 默认陌生客户）
func (s *CustomerJourneyService) GetState(ctx context.Context, customerID string) *JourneyState {
	if state, ok := s.loadAuthoritative(ctx, customerID); ok {
		return state
	}

	return &JourneyState{
		CustomerID:   customerID,
		CurrentStage: StageStranger,
		StageSince:   time.Now(),
		StageHistory: []JourneyEvent{},
		TotalTouches: 0,
		AutoTags:     []string{},
		Metadata:     make(map[string]string),
	}
}

// Touch 记录互动（不改变阶段）
func (s *CustomerJourneyService) Touch(ctx context.Context, customerID, source string) {

	base, found := s.loadAuthoritative(ctx, customerID)
	if !found {
		now := time.Now()
		base = &JourneyState{
			CustomerID:   customerID,
			CurrentStage: StageContact,
			StageSince:   now,
			StageHistory: []JourneyEvent{},
			AutoTags:     []string{},
			Metadata:     make(map[string]string),
		}
	}
	state := cloneJourneyState(base)
	state.CustomerID = customerID
	state.LastTouchAt = time.Now()
	state.TotalTouches++
	s.putL1(customerID, &state)
	s.persistState(ctx, &state)
}

// Transition 迁移阶段
func (s *CustomerJourneyService) Transition(ctx context.Context, customerID string, toStage JourneyStage, source, operatorID, reason string, metadata map[string]any) (*JourneyEvent, error) {
	if !s.isValidStage(ctx, toStage) {
		return nil, fmt.Errorf("无效的阶段: %s", toStage)
	}

	base, found := s.loadAuthoritative(ctx, customerID)
	if !found {
		now := time.Now()
		base = &JourneyState{
			CustomerID:   customerID,
			CurrentStage: StageStranger,
			StageSince:   now,
			StageHistory: []JourneyEvent{},
			AutoTags:     []string{},
			Metadata:     make(map[string]string),
		}
	}
	state := cloneJourneyState(base)
	state.CustomerID = customerID
	fromStage := state.CurrentStage
	if fromStage == toStage {
		s.putL1(customerID, &state)
		return nil, nil
	}
	event := &JourneyEvent{
		Type:       "stage_transition",
		FromStage:  fromStage,
		ToStage:    toStage,
		Reason:     reason,
		Source:     source,
		OperatorID: operatorID,
		Metadata:   metadata,
		Timestamp:  time.Now(),
	}
	state.CurrentStage = toStage
	state.StageSince = time.Now()
	state.StageHistory = append(state.StageHistory, *event)
	s.applyStageSideEffects(ctx, &state, toStage)
	s.putL1(customerID, &state)
	s.persistState(ctx, &state)
	for _, sub := range s.subscribers {
		go sub.OnJourneyEvent(ctx, customerID, event)
	}
	return event, nil
}

func (s *CustomerJourneyService) applyStageSideEffects(ctx context.Context, state *JourneyState, stage JourneyStage) {
	meta := StageMetas[stage]
	if meta == nil {
		return
	}
	tag := "stage:" + string(stage)
	hasTag := false
	for _, t := range state.AutoTags {
		if t == tag {
			hasTag = true
			break
		}
	}
	if !hasTag {
		state.AutoTags = append(state.AutoTags, tag)
	}
}

func (s *CustomerJourneyService) isValidStage(ctx context.Context, stage JourneyStage) bool {
	for _, s := range AllStages {
		if s == stage {
			return true
		}
	}
	return false
}

// AddSubscriber 添加订阅者
func (s *CustomerJourneyService) AddSubscriber(ctx context.Context, sub JourneySubscriber) {
	s.subscribers = append(s.subscribers, sub)
}

// ListByStage 按阶段列出客户（本地 L1 视图）
func (s *CustomerJourneyService) ListByStage(ctx context.Context, stage JourneyStage) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	customerIDs := []string{}
	now := time.Now()
	for cid, state := range s.states {
		if exp, ok := s.l1ExpiresAt[cid]; ok && now.After(exp) {
			continue
		}
		if state.CurrentStage == stage {
			customerIDs = append(customerIDs, cid)
		}
	}
	return customerIDs
}

// AutoDetectSleeping 自动检测沉睡客户
// 商业逻辑：成交后 30 天未互动 → 复购期；复购期 60 天未互动 → 沉睡
// DefaultFollowup=0 的阶段（如 won）也需处理，否则成交客户无法沉淀
// 商业产品级要求：所有"持续型"阶段（contact → repurchase）必须可检测沉睡
func (s *CustomerJourneyService) AutoDetectSleeping(ctx context.Context) []string {
	s.mu.Lock()
	wokeUp := []string{}
	now := time.Now()
	snapshots := make([]JourneyState, 0)
	for cid, state := range s.states {
		if exp, ok := s.l1ExpiresAt[cid]; ok && now.After(exp) {
			delete(s.states, cid)
			delete(s.l1ExpiresAt, cid)
			continue
		}
		meta := StageMetas[state.CurrentStage]
		if meta == nil {
			continue
		}
		threshold := meta.DefaultFollowup * 3
		if threshold == 0 {
			threshold = stageDefaultSleepThreshold(state.CurrentStage)
		}
		if threshold == 0 {
			continue
		}
		ref := state.StageSince
		if !state.LastTouchAt.IsZero() && state.LastTouchAt.Before(ref) {
			ref = state.LastTouchAt
		}
		if state.LastTouchAt.IsZero() {
			ref = state.StageSince
		}
		if now.Sub(ref) > threshold {
			event := &JourneyEvent{
				Type:       "auto_sleep",
				FromStage:  state.CurrentStage,
				ToStage:    StageSleeping,
				Reason:     fmt.Sprintf("超 %v 未互动（阶段=%s）", threshold, state.CurrentStage),
				Source:     "system",
				OperatorID: "system",
				Timestamp:  now,
			}
			state.CurrentStage = StageSleeping
			state.StageSince = now
			state.StageHistory = append(state.StageHistory, *event)
			s.applyStageSideEffects(ctx, state, StageSleeping)
			wokeUp = append(wokeUp, cid)
			s.l1ExpiresAt[cid] = now.Add(journeyL1TTL)
			snapshots = append(snapshots, cloneJourneyState(state))
		}
	}
	s.mu.Unlock()
	for i := range snapshots {
		s.persistState(ctx, &snapshots[i])
	}
	return wokeUp
}

// JourneySleepCron 客户旅程沉睡检测定时任务。
//
// H4 债务修复：AutoDetectSleeping 此前零调用（无任何 cron 注册），沉睡检测实际永不运行。
// 现每日 03:30（CST，早于 CustomerRFMCron 的 04:00 重算）执行一次，
// 将超过阶段阈值未互动的客户自动迁移至 sleeping 阶段。
type JourneySleepCron struct {
	svc       *CustomerJourneyService
	stop      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
}

// NewJourneySleepCron 构造（svc 为 nil 时使用默认构造）
func NewJourneySleepCron(svc *CustomerJourneyService) *JourneySleepCron {
	if svc == nil {
		svc = NewCustomerJourneyService()
	}
	return &JourneySleepCron{svc: svc, stop: make(chan struct{})}
}

// Start 启动每日调度（幂等：重复调用仅启动一次）
func (c *JourneySleepCron) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		c.wg.Add(1)
		go c.loop(ctx)
		logger.Info("[JourneySleepCron] 已启动（每日 03:30 CST 沉睡客户自动检测）")
	})
}

// Stop 停止（幂等：重复调用安全返回）
func (c *JourneySleepCron) Stop(_ context.Context) {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
	c.wg.Wait()
	logger.Info("[JourneySleepCron] 已停止")
}

func (c *JourneySleepCron) loop(ctx context.Context) {
	defer c.wg.Done()
	cst := time.FixedZone("CST", 8*3600)
	for {
		next := time.Now().In(cst).Add(24 * time.Hour)
		next = time.Date(next.Year(), next.Month(), next.Day(), 3, 30, 0, 0, cst)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-c.stop:
			timer.Stop()
			return
		case <-timer.C:
			detected := c.runOnce(ctx)
			logger.Ctx(ctx).Info().Int("detected", len(detected)).
				Msg("[JourneySleepCron] 沉睡客户自动检测完成")
		}
	}
}

func (c *JourneySleepCron) runOnce(ctx context.Context) (detected []string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Ctx(ctx).Error().Msgf("[JourneySleepCron] 沉睡检测 panic: %v", r)
			detected = nil
		}
	}()
	return c.svc.AutoDetectSleeping(ctx)
}

func stageDefaultSleepThreshold(stage JourneyStage) time.Duration {
	switch stage {
	case StageWon, StageAfterSale:
		return 90 * 24 * time.Hour
	case StageRepurchase:
		return 60 * 24 * time.Hour
	case StageQuoted:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// JourneyStageOverview 阶段总览
type JourneyStageOverview struct {
	Stage        JourneyStage `json:"stage"`
	Label        string       `json:"label"`
	Count        int          `json:"count"`
	Rate         float64      `json:"rate"`
	AvgStayHours float64      `json:"avg_stay_hours"`
}

// JourneyOverview 客户旅程总览
type JourneyOverview struct {
	TotalCustomers int                    `json:"total_customers"`
	Stages         []JourneyStageOverview `json:"stages"`
	TotalEvents    int                    `json:"total_events"`
	GeneratedAt    time.Time              `json:"generated_at"`
}

// GetOverview 获取全旅程总览（各阶段客户数 + 转化率）
func (s *CustomerJourneyService) GetOverview(ctx context.Context) *JourneyOverview {
	s.mu.RLock()
	defer s.mu.RUnlock()

	overview := &JourneyOverview{
		Stages:      make([]JourneyStageOverview, 0, len(AllStages)),
		GeneratedAt: time.Now(),
	}
	overview.TotalCustomers = 0

	stageCounts := make(map[JourneyStage]int, len(AllStages))
	stageStaySum := make(map[JourneyStage]float64, len(AllStages))
	now := time.Now()
	for cid, state := range s.states {
		if exp, ok := s.l1ExpiresAt[cid]; ok && now.After(exp) {
			continue
		}
		overview.TotalCustomers++
		stageCounts[state.CurrentStage]++
		stay := now.Sub(state.StageSince).Hours()
		stageStaySum[state.CurrentStage] += stay
		overview.TotalEvents += len(state.StageHistory)
	}

	for _, st := range AllStages {
		count := stageCounts[st]
		var rate float64
		if overview.TotalCustomers > 0 {
			rate = float64(count) / float64(overview.TotalCustomers) * 100
		}
		var avgStay float64
		if count > 0 {
			avgStay = stageStaySum[st] / float64(count)
		}
		meta := StageMetas[st]
		label := string(st)
		if meta != nil {
			label = meta.Label
		}
		overview.Stages = append(overview.Stages, JourneyStageOverview{
			Stage:        st,
			Label:        label,
			Count:        count,
			Rate:         rate,
			AvgStayHours: avgStay,
		})
	}
	return overview
}

// Funnel 构建基于客户旅程的转化漏斗（H2：自 sales_dashboard.FunnelByJourney 迁移）
// 商业逻辑：每阶段的客户数 + 阶段间转化率 + 端到端转化率
func (s *CustomerJourneyService) Funnel(ctx context.Context) *JourneyFunnel {
	funnel := &JourneyFunnel{
		Stages:      make([]JourneyFunnelStage, 0, len(AllStages)),
		GeneratedAt: time.Now(),
	}

	stageCounts := make(map[JourneyStage]int)
	stageDwell := make(map[JourneyStage][]float64)
	ownerLoad := make(map[JourneyStage]map[string]int)

	s.mu.RLock()
	for _, state := range s.states {
		stageCounts[state.CurrentStage]++
		dwell := time.Since(state.StageSince).Hours() / 24
		stageDwell[state.CurrentStage] = append(stageDwell[state.CurrentStage], dwell)
		if ownerLoad[state.CurrentStage] == nil {
			ownerLoad[state.CurrentStage] = make(map[string]int)
		}
		owner := state.Metadata["owner_id"]
		if owner == "" {
			owner = "unassigned"
		}
		ownerLoad[state.CurrentStage][owner]++
	}
	s.mu.RUnlock()

	funnel.TotalEntered = stageCounts[StageStranger] + stageCounts[StageLead] + stageCounts[StageContact]
	funnel.TotalWon = stageCounts[StageWon]
	if funnel.TotalEntered > 0 {
		funnel.EndToEndRate = float64(funnel.TotalWon) / float64(funnel.TotalEntered)
	}

	orderedStages := []JourneyStage{
		StageStranger, StageLead, StageContact, StageInterested,
		StageQuoted, StageWon,
	}
	for _, st := range orderedStages {
		count := stageCounts[st]
		dwells := stageDwell[st]
		avgDwell := 0.0
		if len(dwells) > 0 {
			sum := 0.0
			for _, d := range dwells {
				sum += d
			}
			avgDwell = sum / float64(len(dwells))
		}
		meta := StageMetas[st]
		label := string(st)
		if meta != nil && meta.Label != "" {
			label = meta.Label
		}
		funnel.Stages = append(funnel.Stages, JourneyFunnelStage{
			Stage:        st,
			Label:        label,
			Customers:    count,
			AvgDwellDays: avgDwell,
			OwnerLoad:    ownerLoad[st],
		})
	}
	for i := range funnel.Stages {
		funnel.Stages[i].StageRate = float64(funnel.Stages[i].Customers) / float64(funnel.TotalEntered) * 100
		if i == 0 {
			funnel.Stages[i].StepRate = 100
		} else if funnel.Stages[i-1].Customers > 0 {
			funnel.Stages[i].StepRate = float64(funnel.Stages[i].Customers) / float64(funnel.Stages[i-1].Customers) * 100
		}
	}
	return funnel
}

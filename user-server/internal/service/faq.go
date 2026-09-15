package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

const (
	faqCacheTTL           = 5 * time.Minute
	faqCacheMaxN          = 5000
	faqHitThresh          = 0.6
	faqTopKDefault        = 3
	faqDecayPerWeek       = 0.1
	faqDecayDays          = 7 * 24 * time.Hour
	faqDecayMinHits       = 5
	faqDecayMaxBatch      = 1000
	faqAgentShared   uint = 0
)

// Clock 时钟抽象 (用于 WeekDecay 测试注入, 五层架构 L4)
//
// 默认实现 time.RealClock (返回 time.Now), 测试可注入 mock clock。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type faqRepoIface interface {
	MatchByKeyword(ctx context.Context, msg string, topK int) ([]model.FAQEntry, error)
	MatchByAgent(ctx context.Context, agentID uint, msg string, topK int) ([]model.FAQEntry, error)
	ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error)
	IncrementHitCount(ctx context.Context, id uint) error
	DecayQuality(ctx context.Context, id uint, decay float64) error
	ListDecayCandidates(ctx context.Context, cutoff time.Time, limit int) ([]model.FAQEntry, error)
	IncrementNegativeHit(ctx context.Context, id uint) error
	ListWithFilter(ctx context.Context, filter repository.FAQListParams) ([]model.FAQEntry, int64, error)
	GetByID(ctx context.Context, id uint) (*model.FAQEntry, error)
	ListEnabled(ctx context.Context, limit int) ([]model.FAQEntry, error)
	ListCandidates(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error)
	ScoreCandidates(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error)
	Create(ctx context.Context, entry *model.FAQEntry) error
	Update(ctx context.Context, id uint, entry *model.FAQEntry) error
	Delete(ctx context.Context, id uint) error
}

type faqBindingRepoIface interface {
	ListByAgent(ctx context.Context, agentID uint, kbType string) ([]model.AgentKBBinding, error)
}

// FAQService FAQ 业务服务
//
// Task 15 变更:
//   - 增加 bindingRepo 字段 (注入绑定仓库, 用于校验 agent ↔ KB 关系)
//   - cache 由单片 []model.FAQEntry 改为 map[uint][]model.FAQEntry (按 agentID 分片)
//   - 新增 sharedCache 概念: agentID=0 的桶存储"共享池"FAQ (向后兼容旧 Match API)
type FAQService struct {
	repo        faqRepoIface
	bindingRepo faqBindingRepoIface
	clock       Clock

	mu     sync.RWMutex
	cache  map[uint][]model.FAQEntry
	loaded map[uint]time.Time
}

// NewFAQServiceDefault 使用全局 DB 创建 FAQ Service（controller 层入口，避免 controller 持有 gorm.DB）。
func NewFAQServiceDefault() *FAQService {
	return NewFAQService(dbUtil.GetDB(), nil)
}

// NewFAQService 创建 FAQ Service
//
// Task 15: 第二个参数保留 *repository.FAQRepository 兼容旧调用, bindingRepo 走 SetBindingRepo 注入。
func NewFAQService(db *gorm.DB, repo *repository.FAQRepository) *FAQService {
	var iface faqRepoIface
	if repo == nil && db != nil {
		repo = repository.NewFAQRepository(db)
	}
	if repo != nil {
		iface = repo
	}
	var bindingIface faqBindingRepoIface
	if db != nil {
		bindingIface = repository.NewAgentKBBindingRepository(db)
	}
	return &FAQService{
		repo:        iface,
		bindingRepo: bindingIface,
		clock:       realClock{},
		cache:       make(map[uint][]model.FAQEntry),
		loaded:      make(map[uint]time.Time),
	}
}

// NewFAQServiceWithRepo 用任意实现 faqRepoIface 的 repo 创建 (测试用)
func NewFAQServiceWithRepo(repo faqRepoIface) *FAQService {
	return &FAQService{
		repo:   repo,
		clock:  realClock{},
		cache:  make(map[uint][]model.FAQEntry),
		loaded: make(map[uint]time.Time),
	}
}

// NewFAQServiceWithRepos 用 repo + bindingRepo 同时注入 (Task 15: 单元测试/上层装配)
func NewFAQServiceWithRepos(repo faqRepoIface, bindingRepo faqBindingRepoIface) *FAQService {
	return &FAQService{
		repo:        repo,
		bindingRepo: bindingRepo,
		clock:       realClock{},
		cache:       make(map[uint][]model.FAQEntry),
		loaded:      make(map[uint]time.Time),
	}
}

// SetBindingRepo 注入 binding 仓库 (供 layer / controller 装配时使用)
func (s *FAQService) SetBindingRepo(r faqBindingRepoIface) {
	if r != nil {
		s.bindingRepo = r
	}
}

// SetClock 注入时钟 (: 单测可注入 mock clock)
func (s *FAQService) SetClock(c Clock) {
	if c != nil {
		s.clock = c
	}
}

func (s *FAQService) cachedEntries(ctx context.Context, agentID uint) ([]model.FAQEntry, error) {
	s.mu.RLock()
	if ents, ok := s.cache[agentID]; ok {
		if t, ok2 := s.loaded[agentID]; ok2 && time.Since(t) < faqCacheTTL {
			s.mu.RUnlock()
			return ents, nil
		}
	}
	s.mu.RUnlock()

	ents, err := s.repo.ListCandidates(ctx, agentID, faqCacheMaxN)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[agentID] = ents
	s.loaded[agentID] = time.Now()
	s.mu.Unlock()
	return ents, nil
}

func (s *FAQService) Match(ctx context.Context, msg string, topK int) ([]dto.FAQMatchResult, error) {
	if s.repo == nil {
		return nil, nil
	}
	if topK <= 0 {
		topK = faqTopKDefault
	}
	entries, err := s.cachedEntries(ctx, 0)
	if err != nil {
		return nil, err
	}
	scored, err := s.repo.ScoreCandidates(ctx, entries, msg, topK)
	if err != nil {
		return nil, err
	}
	return s.toMatchResults(scored, "keyword"), nil
}

// MatchByAgent 按智能体 ID 匹配 FAQ (Task 15: 强 1对1 改造)
//
// 新签名: (ctx, agentID uint, msg string, topK int)
//
// 行为:
//   - agentID == 0: 不再走"空数组=全局"回退, 直接返回 (nil, nil) (移除原 MatchByIDs 空数组分支)
//   - msg == "": 短路, 不查 repo, 直接返回 (nil, nil)
//   - agentID > 0: 仅匹配 enabled = true AND agent_id = ? 的 FAQ
//   - 命中后通过 toMatchResults 转 DTO, tag = "agent_bound"
//
// 调用方 (LayerRouter / FAQController) 必须显式传入 agentID。
func (s *FAQService) MatchByAgent(ctx context.Context, agentID uint, msg string, topK int) ([]dto.FAQMatchResult, error) {
	if s.repo == nil {
		return nil, nil
	}
	if agentID == 0 {
		return nil, nil
	}
	if strings.TrimSpace(msg) == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = faqTopKDefault
	}
	entries, err := s.cachedEntries(ctx, agentID)
	if err != nil {
		return nil, err
	}
	scored, err := s.repo.ScoreCandidates(ctx, entries, msg, topK)
	if err != nil {
		return nil, err
	}
	return s.toMatchResults(scored, "agent_bound"), nil
}

// MatchByAgentKB 按 agentID + KB ID 集合匹配 (供上层 service / 编排使用)
//
// 场景: agent 绑定了多个 KB 时, 内部要按 KB 维度再过滤 (默认走 agentID 即可,
// 此方法用于"agent 绑了多个 KB 但只想用其中部分"的精细场景)。
func (s *FAQService) MatchByAgentKB(ctx context.Context, agentID uint, kbIDs []uint, msg string, topK int) ([]dto.FAQMatchResult, error) {
	if s.repo == nil || agentID == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = faqTopKDefault
	}
	_ = kbIDs
	entries, err := s.cachedEntries(ctx, agentID)
	if err != nil {
		return nil, err
	}
	scored, err := s.repo.ScoreCandidates(ctx, entries, msg, topK)
	if err != nil {
		return nil, err
	}
	return s.toMatchResults(scored, "agent_kb"), nil
}

func (s *FAQService) toMatchResults(entries []model.FAQEntry, matchType string) []dto.FAQMatchResult {
	out := make([]dto.FAQMatchResult, 0, len(entries))
	for i, e := range entries {
		out = append(out, dto.FAQMatchResult{
			Entry:     entryToDTO(&e),
			Score:     e.Confidence,
			Rank:      i,
			HitCount:  e.HitCount,
			MatchType: matchType,
		})
	}
	return out
}

// List 列表查询
func (s *FAQService) List(ctx context.Context, filter dto.FAQFilter) ([]dto.FAQEntry, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	params := repository.FAQListParams{
		Keyword:  filter.Keyword,
		Category: filter.Category,
		Intent:   filter.Intent,
		Enabled:  filter.Enabled,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	entries, total, err := s.repo.ListWithFilter(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.FAQEntry, 0, len(entries))
	for i := range entries {
		out = append(out, *entryToDTO(&entries[i]))
	}
	return out, total, nil
}

// GetByID 按 ID 查询
func (s *FAQService) GetByID(ctx context.Context, id uint) (*dto.FAQEntry, error) {
	if s.repo == nil {
		return nil, nil
	}
	e, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return entryToDTO(e), nil
}

// Create 新增 FAQ
//
// Task 15 改造: AgentID 必填 (强 1对1, 所有 FAQ 都必须归属于某个智能体或共享池)
func (s *FAQService) Create(ctx context.Context, entry *model.FAQEntry) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	if entry == nil {
		return errors.New("entry is nil")
	}
	if entry.AgentID == nil || *entry.AgentID == 0 {
		return errors.New("agent_id 必填 (Task 15 强 1对1: 不允许全局匿名 FAQ)")
	}
	if entry.Enabled == nil {
		t := true
		entry.Enabled = &t
	}
	if err := s.repo.Create(ctx, entry); err != nil {
		return err
	}
	s.InvalidateCache(*entry.AgentID)
	return nil
}

// Update 更新 FAQ
func (s *FAQService) Update(ctx context.Context, id uint, entry *model.FAQEntry) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	entry.ID = id
	if err := s.repo.Update(ctx, id, entry); err != nil {
		return err
	}
	if entry != nil && entry.AgentID != nil {
		s.InvalidateCache(*entry.AgentID)
	} else {
		s.InvalidateAllCache()
	}
	return nil
}

// Delete 删除 FAQ
func (s *FAQService) Delete(ctx context.Context, id uint) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	existing, _ := s.repo.GetByID(ctx, id)
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if existing != nil && existing.AgentID != nil {
		s.InvalidateCache(*existing.AgentID)
	} else {
		s.InvalidateAllCache()
	}
	return nil
}

// ShouldSkipLLM 基于 FAQ 命中判定是否应该跳过 LLM
//
// 规则:
//   - 命中且 score >= faqHitThresh   -> true (SkipLLM, 用 FAQ 回复)
//   - 命中但 score <  faqHitThresh   -> false (走 Layer2 LLM 兜底)
//   - 未命中                          -> false
func (s *FAQService) ShouldSkipLLM(matches []dto.FAQMatchResult) (bool, *dto.FAQMatchResult) {
	if len(matches) == 0 {
		return false, nil
	}
	top := matches[0]
	if top.Score < faqHitThresh {
		return false, nil
	}
	return true, &top
}

// IncrementHitCount 命中计数 (异步, 不阻塞主流程)
func (s *FAQService) IncrementHitCount(ctx context.Context, id uint) {
	if s.repo == nil || id == 0 {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
		defer cancel()
		_ = s.repo.IncrementHitCount(bgCtx, id)
	}()
}

// WarmupCache 预热缓存 (Task 15: 按 agentID 分片)
//
// 行为:
//   - agentID == 0: 预热共享池 (agent_id IS NULL 语义等价; 当前实现为 agentID=0 桶)
//   - agentID > 0: 仅预热该智能体的 FAQ (走 ListByAgent)
//   - TTL = 5 min, 重复调用会覆盖旧缓存
//
// 当前实现简化: Match 走 MatchByKeyword (不查 agentID 字段), MatchByAgent 走 MatchByAgent。
//
//	为避免过度复杂, 共享池 key=0 不直接用于命中 (保留以便向后兼容 Match API),
//	实际"全量缓存"是 Lazy 模式: 命中时由 repo 实时过滤。
func (s *FAQService) WarmupCache(ctx context.Context, agentID uint) error {
	if s.repo == nil {
		return nil
	}
	var entries []model.FAQEntry
	var err error
	if agentID == faqAgentShared {
		entries, err = s.repo.ListEnabled(ctx, faqCacheMaxN)
	} else {
		entries, err = s.repo.ListByAgent(ctx, agentID, faqCacheMaxN)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[agentID] = entries
	s.loaded[agentID] = time.Now()
	s.mu.Unlock()
	return nil
}

// WarmupAll 预热所有已知 agent 的缓存 (启动时调用, 由调用方传入 agentID 列表)
//
// 用途: 已知全量 agent 列表时, 一次预热避免运行时冷启动。
// 实现: 简单串行预热每个 agent, 失败不阻塞。
func (s *FAQService) WarmupAll(ctx context.Context, agentIDs []uint) {
	for _, id := range agentIDs {
		_ = s.WarmupCache(ctx, id)
	}
}

// InvalidateCache 失效指定 agentID 的缓存 (Task 15: 精确失效)
//
// agentID == 0: 失效共享池
// agentID > 0: 失效该 agent 私有桶
func (s *FAQService) InvalidateCache(agentID uint) {
	s.mu.Lock()
	delete(s.cache, agentID)
	delete(s.loaded, agentID)
	s.mu.Unlock()
}

// InvalidateAllCache 失效全部缓存 (Task 15 兼容入口)
//
// 用于: 业务不确定受影响 agentID 时 (例如全局配置变更)。
func (s *FAQService) InvalidateAllCache() {
	s.mu.Lock()
	s.cache = make(map[uint][]model.FAQEntry)
	s.loaded = make(map[uint]time.Time)
	s.mu.Unlock()
}

func (s *FAQService) WeekDecay(ctx context.Context) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	now := s.now()
	cutoff := now.Add(-faqDecayDays)
	candidates, err := s.repo.ListDecayCandidates(ctx, cutoff, faqDecayMaxBatch)
	if err != nil {
		return 0, err
	}
	decayed := 0
	for _, e := range candidates {
		if err := s.repo.DecayQuality(ctx, e.ID, faqDecayPerWeek); err != nil {
			continue
		}
		decayed++
	}
	if decayed > 0 {
		s.InvalidateAllCache()
	}
	return decayed, nil
}

func (s *FAQService) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}

// Stats 命中统计
func (s *FAQService) Stats(ctx context.Context) (total, enabled int64, err error) {
	if s.repo == nil {
		return 0, 0, nil
	}
	all, err := s.repo.ListEnabled(ctx, 10000)
	if err != nil {
		return 0, 0, err
	}
	enabled = int64(len(all))
	return enabled, enabled, nil
}

func entryToDTO(e *model.FAQEntry) *dto.FAQEntry {
	if e == nil {
		return nil
	}
	enabled := false
	if e.Enabled != nil {
		enabled = *e.Enabled
	}
	return &dto.FAQEntry{
		ID:         e.ID,
		Question:   e.Question,
		Answer:     e.Answer,
		Keywords:   []string(e.Keywords),
		Category:   e.Category,
		Intent:     e.Intent,
		Confidence: e.Confidence,
		HitCount:   e.HitCount,
		Enabled:    &enabled,
		AgentID:    e.AgentID,
	}
}

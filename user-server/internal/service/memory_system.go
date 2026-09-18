package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type MemorySystem struct {
	memoryRepo   repository.MemoryRepository
	embeddingSvc llm.EmbeddingServiceInterface
	mu           sync.Mutex
}

const (
	L1WindowSize = 10
	L1TTLHours   = 24 * time.Hour
	L4MaxPerCust = 500
	defaultImp   = 5

	longTermMemoryRecallMultiplier = 3
	longTermMemoryMaxFetch         = 50
	longTermMemoryDecayDuration    = 30 * 24 * time.Hour

	longTermDedupThreshold    = 0.92
	longTermDedupFallbackJacc = 0.72
	longTermDedupScanLimit    = 50
	l4EvictLowImp             = 3
	l4EvictProtectedImp       = 8
	l4EvictScanLimit          = 1000
)

var (
	memorySystemOnce sync.Once
	memorySystem     *MemorySystem
)

// GetMemorySystem 获取全局 4 层记忆系统
func GetMemorySystem() *MemorySystem {
	return memorySystem
}

// InitMemorySystem 初始化 4 层记忆系统
// 默认注入 llm.NewEmbeddingService()（本地 TEI 真实 bge-m3，1024 维）
// 测试场景可用 SetEmbeddingService 替换为 HashEmbeddingService
//
// 注意：测试模式下（IS_TEST_MODE=1）跳过 sync.Once 缓存，每次返回新实例，
// 避免测试间状态污染（不同测试用不同 DB 时全局缓存会导致数据写到错误 DB）。
//
// 五层架构修复：service 层不再持有 *gorm.DB，由 repository 层封装所有 DB 操作。
// 保留 db 参数以兼容调用方（main.go），内部转换为 MemoryRepository。
func InitMemorySystem(db *gorm.DB) *MemorySystem {
	repo := repository.NewMemoryRepositoryWithDB(db)
	if os.Getenv("IS_TEST_MODE") == "1" {
		return &MemorySystem{
			memoryRepo:   repo,
			embeddingSvc: llm.NewEmbeddingService(),
		}
	}
	memorySystemOnce.Do(func() {
		memorySystem = &MemorySystem{
			memoryRepo:   repo,
			embeddingSvc: llm.NewEmbeddingService(),
		}
	})
	return memorySystem
}

// SetEmbeddingService 替换 Embedding 服务（用于测试注入 HashEmbeddingService）
// 非并发安全，应在初始化阶段调用
func (m *MemorySystem) SetEmbeddingService(ctx context.Context, svc llm.EmbeddingServiceInterface) {
	m.embeddingSvc = svc
}

// WithEmbeddingService 链式调用注入 Embedding 服务
func (m *MemorySystem) WithEmbeddingService(ctx context.Context, svc llm.EmbeddingServiceInterface) *MemorySystem {
	m.embeddingSvc = svc
	return m
}

// L1Append 追加一条短期消息
func (m *MemorySystem) L1Append(ctx context.Context, sessionID, customerID, role, content string) error {
	if m.memoryRepo == nil {
		return nil
	}
	exp := time.Now().Add(L1TTLHours)
	item := &model.MemoryItem{
		Layer:      model.MemoryLayerShortTerm,
		SessionID:  sessionID,
		CustomerID: customerID,
		ItemType:   "message",
		Role:       role,
		Content:    content,
		ExpiresAt:  &exp,
	}
	if err := m.memoryRepo.CreateMemoryItem(ctx, item); err != nil {
		return err
	}
	m.l1Trim(ctx, sessionID)
	return nil
}

// L1List 获取会话最近 N 条消息
func (m *MemorySystem) L1List(ctx context.Context, sessionID string, limit int) ([]model.MemoryItem, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = L1WindowSize
	}
	return m.memoryRepo.ListShortTermMemoryBySession(ctx, sessionID, limit)
}

// L1Clear 清空某会话短期记忆
func (m *MemorySystem) L1Clear(ctx context.Context, sessionID string) error {
	if m.memoryRepo == nil {
		return nil
	}
	return m.memoryRepo.DeleteShortTermMemoryBySession(ctx, sessionID)
}

func (m *MemorySystem) l1Trim(ctx context.Context, sessionID string) {
	if m.memoryRepo == nil {
		return
	}
	count, err := m.memoryRepo.CountShortTermMemoryBySession(ctx, sessionID)
	if err != nil {
		return
	}
	if count <= int64(L1WindowSize) {
		return
	}
	exceed := count - int64(L1WindowSize)
	oldIDs, err := m.memoryRepo.PluckOldestShortTermMemoryIDs(ctx, sessionID, int(exceed))
	if err != nil {
		return
	}
	if len(oldIDs) > 0 {
		utils.WarnErrKV("memory.L1Trim.DeleteOldItems",
			m.memoryRepo.DeleteMemoryItemsByIDs(ctx, oldIDs),
			"session_id", sessionID,
			"old_count", strconv.Itoa(len(oldIDs)))
	}
}

// L2SaveFact 保存一条长期事实（事件时间缺省为 now，见 L2SaveFactAt）
func (m *MemorySystem) L2SaveFact(ctx context.Context, customerID, key, value string, importance int) error {
	return m.L2SaveFactAt(ctx, customerID, key, value, importance, time.Time{})
}

func (m *MemorySystem) L2SaveFactAt(ctx context.Context, customerID, key, value string, importance int, eventAt time.Time) error {
	if m.memoryRepo == nil {
		return nil
	}
	if importance <= 0 || importance > 10 {
		importance = defaultImp
	}
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	now := time.Now()

	var staleIDs []uint
	if old, err := m.memoryRepo.ListFactsByKey(ctx, customerID, key, 50); err != nil {
		logger.Warnf("[MemorySystem] 同键事实扫描失败，降级追加式写入 customer=%s key=%s err=%v", customerID, key, err)
	} else {
		for i := range old {
			it := old[i]
			if !validAtAsOf(timePtrValue(it.ValidFrom), it.CreatedAt, timePtrValue(it.InvalidAt), now) {
				continue
			}
			if it.Content == value {
				return nil
			}
			staleIDs = append(staleIDs, it.ID)
		}
	}
	if err := m.memoryRepo.SoftInvalidateMemoryItemsByIDs(ctx, staleIDs, now); err != nil {
		utils.WarnErrKV("memory.L2SaveFact.SoftInvalidate",
			err, "customer_id", customerID, "key", key, "stale_count", strconv.Itoa(len(staleIDs)))
	}
	vf := eventAt
	item := &model.MemoryItem{
		Layer:      model.MemoryLayerLongTerm,
		CustomerID: customerID,
		ItemType:   "fact:" + key,
		Content:    value,
		Importance: importance,
		Metadata:   model.JSONMap{"key": key},
		ValidFrom:  &vf,
	}
	return m.memoryRepo.CreateMemoryItem(ctx, item)
}

// L2SaveSummary 保存长期摘要
func (m *MemorySystem) L2SaveSummary(ctx context.Context, customerID, summary string) error {
	if m.memoryRepo == nil {
		return nil
	}
	item := &model.MemoryItem{
		Layer:      model.MemoryLayerLongTerm,
		CustomerID: customerID,
		ItemType:   "summary",
		Content:    summary,
		Importance: 8,
	}
	return m.memoryRepo.CreateMemoryItem(ctx, item)
}

// L2ListFacts 获取客户的长期事实
func (m *MemorySystem) L2ListFacts(ctx context.Context, customerID string, limit int) ([]model.MemoryItem, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	return m.memoryRepo.ListFacts(ctx, customerID, limit)
}

func (m *MemorySystem) L2ListFactsAsOf(ctx context.Context, customerID string, asOf time.Time, limit int) ([]model.MemoryItem, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if limit <= 0 {
		limit = 50
	}
	return m.memoryRepo.ListFactsAsOf(ctx, customerID, asOf, limit)
}

func (m *MemorySystem) ListValidFacts(ctx context.Context, customerID string, asOf time.Time, limit int) ([]model.MemoryItem, error) {
	return m.L2ListFactsAsOf(ctx, customerID, asOf, limit)
}

// L2GetLatestSummary 获取客户最新长期摘要
func (m *MemorySystem) L2GetLatestSummary(ctx context.Context, customerID string) (string, error) {
	if m.memoryRepo == nil {
		return "", nil
	}
	item, err := m.memoryRepo.GetLatestSummary(ctx, customerID)
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", nil
	}
	return item.Content, nil
}

// L3SaveSOPState 保存 SOP 状态
func (m *MemorySystem) L3SaveSOPState(ctx context.Context, state *model.SOPStateMemory) error {
	if m.memoryRepo == nil || state == nil {
		return nil
	}
	state.LastStepAt = time.Now()
	return m.memoryRepo.SaveSOPState(ctx, state)
}

// L3GetSOPState 获取会话当前 SOP 状态
func (m *MemorySystem) L3GetSOPState(ctx context.Context, sessionID string) (*model.SOPStateMemory, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	return m.memoryRepo.GetSOPStateBySession(ctx, sessionID)
}

// L3ListByCustomer 获取客户的所有 SOP 状态
func (m *MemorySystem) L3ListByCustomer(ctx context.Context, customerID string, limit int) ([]model.SOPStateMemory, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	return m.memoryRepo.ListSOPStatesByCustomer(ctx, customerID, limit)
}

// L4Record 记录业务记忆
func (m *MemorySystem) L4Record(ctx context.Context, customerID, memoryType, content, relatedID string, importance int, metadata map[string]any) error {
	if m.memoryRepo == nil {
		return nil
	}
	if importance <= 0 || importance > 10 {
		importance = defaultImp
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	count, _ := m.memoryRepo.CountBusinessMemoriesByCustomer(ctx, customerID)
	if count+1 > int64(L4MaxPerCust) {
		m.l4EvictImportanceAware(ctx, customerID, int(count+1-int64(L4MaxPerCust)))
	}

	meta := model.JSONMap{}
	for k, v := range metadata {
		meta[k] = v
	}
	item := &model.BusinessMemory{
		CustomerID: customerID,
		MemoryType: memoryType,
		Content:    content,
		RelatedID:  relatedID,
		Importance: importance,
		Metadata:   meta,
	}
	return m.memoryRepo.CreateBusinessMemory(ctx, item)
}

// L4ListByCustomer 获取客户业务记忆
func (m *MemorySystem) L4ListByCustomer(ctx context.Context, customerID string, memoryType string, limit int) ([]model.BusinessMemory, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	return m.memoryRepo.ListBusinessMemories(ctx, customerID, memoryType, limit)
}

func l4EvictPriority(it model.BusinessMemory) int {
	if it.Importance <= l4EvictLowImp {
		return 0
	}
	return 1
}

func (m *MemorySystem) l4EvictImportanceAware(ctx context.Context, customerID string, n int) {
	if n <= 0 {
		return
	}
	items, err := m.memoryRepo.ListBusinessMemories(ctx, customerID, "", l4EvictScanLimit)
	if err != nil || len(items) == 0 {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		pi, pj := l4EvictPriority(items[i]), l4EvictPriority(items[j])
		if pi != pj {
			return pi < pj
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	ids := make([]uint, 0, n)
	for _, it := range items {
		if len(ids) >= n {
			break
		}
		if it.Importance >= l4EvictProtectedImp {
			continue
		}
		ids = append(ids, it.ID)
	}
	if len(ids) > 0 {
		utils.WarnErrKV("memory.L4Trim.DeleteBusiness",
			m.memoryRepo.DeleteBusinessMemoriesByIDs(ctx, ids),
			"customer_id", customerID,
			"delete_count", strconv.Itoa(len(ids)))
	}
	if remaining := len(items) - len(ids); remaining > L4MaxPerCust {
		logger.Warnf("[MemorySystem] L4 记忆超限且剩余均为 importance>=%d 受保护记忆，暂缓淘汰 customer=%s remaining=%d cap=%d",
			l4EvictProtectedImp, customerID, remaining, L4MaxPerCust)
	}
}

// BuildFullContext 构造 4 层汇总上下文（用于 LLM 提示）
func (m *MemorySystem) BuildFullContext(ctx context.Context, sessionID, customerID string) (string, error) {
	if m.memoryRepo == nil {
		return "", nil
	}
	var sb strings.Builder

	l1, _ := m.L1List(ctx, sessionID, L1WindowSize)
	if len(l1) > 0 {
		sb.WriteString("【L1 短期记忆（最近对话）】\n")
		for i := len(l1) - 1; i >= 0; i-- {
			role := "客户"
			if l1[i].Role == "ai" || l1[i].Role == "agent" {
				role = "我"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", role, l1[i].Content))
		}
		sb.WriteString("\n")
	}

	if customerID != "" {
		if summary, _ := m.L2GetLatestSummary(ctx, customerID); summary != "" {
			sb.WriteString("【L2 长期摘要】\n")
			sb.WriteString(summary + "\n\n")
		}
		facts, _ := m.L2ListFacts(ctx, customerID, 20)
		if len(facts) > 0 {
			sb.WriteString("【L2 关键事实】\n")
			for _, f := range facts {
				sb.WriteString(fmt.Sprintf("- %s\n", f.Content))
			}
			sb.WriteString("\n")
		}
	}

	if sessionID != "" {
		state, _ := m.L3GetSOPState(ctx, sessionID)
		if state != nil {
			sb.WriteString("【L3 SOP 状态】\n")
			sb.WriteString(fmt.Sprintf("当前流程=%d, 节点=%s, 步骤=%d, 状态=%s\n\n",
				state.SOPID, state.CurrentNode, state.StepIndex, state.Status))
		}
	}

	if customerID != "" {
		biz, _ := m.L4ListByCustomer(ctx, customerID, "", 10)
		if len(biz) > 0 {
			sb.WriteString("【L4 业务记忆】\n")
			for _, b := range biz {
				sb.WriteString(fmt.Sprintf("[%s] %s\n", b.MemoryType, b.Content))
			}
		}
	}

	return sb.String(), nil
}

// SyncFromDialogueMemory 从老的 DialogueMemory 同步到 4 层结构
// 兼容层：保证重启后老数据也能被 4 层系统看到
func (m *MemorySystem) SyncFromDialogueMemory(ctx context.Context, mem *model.DialogueMemory) {
	if m.memoryRepo == nil || mem == nil {
		return
	}
	if mem.CustomerID == "" {
		return
	}
	if len(mem.KeyFacts) > 0 {
		for k, v := range mem.KeyFacts {
			if s, ok := v.(string); ok && s != "" {
				m.L2SaveFact(ctx, mem.CustomerID, k, s, 7)
			}
		}
	}
	if mem.Summary != "" {
		m.L2SaveSummary(ctx, mem.CustomerID, mem.Summary)
	}
	if len(mem.Objections) > 0 {
		objs, _ := json.Marshal(mem.Objections)
		m.L4Record(ctx, mem.CustomerID, "objection", string(objs), "", 7, nil)
	}
	if mem.PurchaseIntent != "" {
		m.L4Record(ctx, mem.CustomerID, "intent", "购买意向="+mem.PurchaseIntent, "", 8, nil)
	}
	if mem.Budget != "" {
		m.L4Record(ctx, mem.CustomerID, "preference", "预算="+mem.Budget, "", 6, nil)
	}
	if mem.Demand != "" {
		m.L4Record(ctx, mem.CustomerID, "preference", "需求="+mem.Demand, "", 6, nil)
	}
	logger.Infof("[MemorySystem] 同步 DialogueMemory customer=%s 完成", mem.CustomerID)
}

// LongTermMemoryRecallResult Recall 结果
type LongTermMemoryRecallResult struct {
	Memory     *model.CustomerLongTermMemory
	Similarity float64
	Score      float64
}

func (m *MemorySystem) Remember(ctx context.Context, customerID string, memType model.LongTermMemoryType, content string, importance int) (*model.CustomerLongTermMemory, error) {
	if m.memoryRepo == nil {
		return nil, fmt.Errorf("memory system db not initialized")
	}
	if customerID == "" {
		return nil, fmt.Errorf("customer_id cannot be empty")
	}
	if content == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}
	if m.embeddingSvc == nil {
		return nil, fmt.Errorf("embedding service not configured")
	}
	if importance <= 0 || importance > 10 {
		importance = defaultImp
	}
	switch memType {
	case model.LongTermMemoryPreference, model.LongTermMemoryHabit,
		model.LongTermMemoryFeedback, model.LongTermMemoryEvent, model.LongTermMemoryFact:
	default:
		return nil, fmt.Errorf("invalid memory_type: %s", memType)
	}

	cfg := m.embeddingSvc.DefaultConfig()
	vec, err := m.embeddingSvc.EmbedOne(ctx, cfg, content)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	dupItem, dedupErr := m.dedupLongTermMemory(ctx, customerID, memType, content, importance, vec)
	if dedupErr != nil {
		return nil, dedupErr
	}
	if dupItem != nil {
		return dupItem, nil
	}

	item := &model.CustomerLongTermMemory{
		CustomerID: customerID,
		MemoryType: memType,
		Content:    content,
		Importance: importance,
		Source:     model.LongTermMemorySourceConversation,
		Embedding:  embeddingToString(vec),
		Metadata:   model.JSONMap{},
	}

	vf := time.Now()
	item.ValidFrom = &vf
	if err := m.memoryRepo.CreateLongTermMemory(ctx, item); err != nil {
		return nil, fmt.Errorf("save long term memory: %w", err)
	}
	return item, nil
}

func (m *MemorySystem) dedupLongTermMemory(ctx context.Context, customerID string, memType model.LongTermMemoryType, content string, importance int, vec []float32) (*model.CustomerLongTermMemory, error) {
	existing, err := m.memoryRepo.ListLongTermMemories(ctx, customerID, string(memType), longTermDedupScanLimit)
	if err != nil {
		logger.Warnf("[MemorySystem] L2 去重扫描失败，降级追加式写入 customer=%s type=%s err=%v", customerID, memType, err)
		return nil, nil
	}
	for i := range existing {
		old := existing[i]
		var sim float64
		var threshold float64
		if old.Embedding != "" {
			sim = cosineSimilarity(vec, bytesToFloat32Slice([]byte(old.Embedding)))
			threshold = longTermDedupThreshold
		} else {
			sim = keywordJaccard(content, old.Content)
			threshold = longTermDedupFallbackJacc
		}
		if sim < threshold {
			continue
		}
		if old.Content == content && old.Importance == importance {
			logger.Infof("[MemorySystem] L2 语义等价跳过 customer=%s type=%s id=%d sim=%.3f", customerID, memType, old.ID, sim)
			cp := old
			return &cp, nil
		}
		old.Content = content
		old.Embedding = embeddingToString(vec)
		old.Importance = importance
		if err := m.memoryRepo.SaveLongTermMemory(ctx, &old); err != nil {
			return nil, fmt.Errorf("update dedup long term memory: %w", err)
		}
		logger.Infof("[MemorySystem] L2 去重合并(保 ID=%d) customer=%s type=%s sim=%.3f", old.ID, customerID, memType, sim)
		cp := old
		return &cp, nil
	}
	return nil, nil
}

// RememberWithSource 记录长期记忆（带来源 + 元信息）
func (m *MemorySystem) RememberWithSource(ctx context.Context, customerID string, memType model.LongTermMemoryType, content string, importance int, source model.LongTermMemorySource, metadata map[string]any) (*model.CustomerLongTermMemory, error) {
	if m.memoryRepo == nil {
		return nil, fmt.Errorf("memory system db not initialized")
	}
	if m.embeddingSvc == nil {
		return nil, fmt.Errorf("embedding service not configured")
	}
	item, err := m.Remember(ctx, customerID, memType, content, importance)
	if err != nil {
		return nil, err
	}
	item.Source = source
	if metadata != nil {
		meta := model.JSONMap{}
		for k, v := range metadata {
			meta[k] = v
		}
		item.Metadata = meta
	}
	if err := m.memoryRepo.SaveLongTermMemory(ctx, item); err != nil {
		return nil, fmt.Errorf("update long term memory meta: %w", err)
	}
	return item, nil
}

// Recall 召回与 query 最相关的长期记忆
// 对应 PRD §5.2 G5：MemorySystem.Recall(ctx, customerID, query, limit)
// 算法：向量检索（PG pgvector）+ 重排序（importance + 时间衰减）
//   - PG 环境：使用 pgvector 索引召回
//   - 无 pgvector（如未初始化 embedding）：降级为扫表 + 内存计算余弦相似度 + 重排序
func (m *MemorySystem) Recall(ctx context.Context, customerID, query string, limit int) ([]LongTermMemoryRecallResult, error) {
	if m.memoryRepo == nil {
		return nil, fmt.Errorf("memory system db not initialized")
	}
	if customerID == "" {
		return nil, fmt.Errorf("customer_id cannot be empty")
	}
	if limit <= 0 {
		limit = 5
	}
	if m.embeddingSvc == nil {
		return nil, fmt.Errorf("embedding service not configured")
	}

	cfg := m.embeddingSvc.DefaultConfig()
	queryVec, err := m.embeddingSvc.EmbedOne(ctx, cfg, query)
	if err != nil {
		return nil, fmt.Errorf("query embedding failed: %w", err)
	}

	dialect := m.memoryRepo.DialectName(ctx)
	if dialect == "postgres" {
		return m.recallPostgres(ctx, customerID, queryVec, limit)
	}
	return m.recallFallback(ctx, customerID, queryVec, limit)
}

func (m *MemorySystem) recallPostgres(ctx context.Context, customerID string, queryVec []float32, limit int) ([]LongTermMemoryRecallResult, error) {
	fetchN := limit * longTermMemoryRecallMultiplier
	if fetchN > longTermMemoryMaxFetch {
		fetchN = longTermMemoryMaxFetch
	}
	queryVecStr := embeddingToString(queryVec)
	rows, err := m.memoryRepo.SearchLongTermMemoriesByVector(ctx, queryVecStr, customerID, fetchN)
	if err != nil {
		return nil, fmt.Errorf("pgvector search: %w", err)
	}
	return m.rerank(ctx, rows, limit), nil
}

func (m *MemorySystem) recallFallback(ctx context.Context, customerID string, queryVec []float32, limit int) ([]LongTermMemoryRecallResult, error) {
	items, err := m.memoryRepo.ListLongTermMemoriesForFallback(ctx, customerID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("fallback fetch: %w", err)
	}

	rows := make([]repository.LongTermMemoryVectorRow, 0, len(items))
	for _, it := range items {
		vec := bytesToFloat32Slice([]byte(it.Embedding))
		sim := cosineSimilarity(queryVec, vec)
		meta := ""
		if it.Metadata != nil {
			if b, err := json.Marshal(it.Metadata); err == nil {
				meta = string(b)
			}
		}
		rows = append(rows, repository.LongTermMemoryVectorRow{
			ID:         it.ID,
			CustomerID: it.CustomerID,
			MemoryType: string(it.MemoryType),
			Content:    it.Content,
			Importance: it.Importance,
			Source:     string(it.Source),
			Metadata:   meta,
			CreatedAt:  it.CreatedAt,
			ExpiresAt:  it.ExpiresAt,
			Similarity: sim,
		})
	}
	return m.rerank(ctx, rows, limit), nil
}

func (m *MemorySystem) rerank(ctx context.Context, rows []repository.LongTermMemoryVectorRow, limit int) []LongTermMemoryRecallResult {
	now := time.Now()
	results := make([]LongTermMemoryRecallResult, 0, len(rows))
	for _, r := range rows {
		importanceScore := float64(r.Importance) / 10.0
		recencyScore := 1.0 - float64(now.Sub(r.CreatedAt))/float64(longTermMemoryDecayDuration)
		if recencyScore < 0 {
			recencyScore = 0
		}
		if recencyScore > 1 {
			recencyScore = 1
		}
		score := r.Similarity*0.6 + importanceScore*0.3 + recencyScore*0.1

		var meta model.JSONMap
		if r.Metadata != "" {
			_ = json.Unmarshal([]byte(r.Metadata), &meta)
		}
		if meta == nil {
			meta = model.JSONMap{}
		}
		results = append(results, LongTermMemoryRecallResult{
			Memory: &model.CustomerLongTermMemory{
				ID:         r.ID,
				CustomerID: r.CustomerID,
				MemoryType: model.LongTermMemoryType(r.MemoryType),
				Content:    r.Content,
				Importance: r.Importance,
				Source:     model.LongTermMemorySource(r.Source),
				Metadata:   meta,
				CreatedAt:  r.CreatedAt,
				ExpiresAt:  r.ExpiresAt,
			},
			Similarity: r.Similarity,
			Score:      score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// ListLongTermMemories 列出客户长期记忆（按 importance + created_at 降序，不走向量检索）
// 用于 UI 展示 / 调试
func (m *MemorySystem) ListLongTermMemories(ctx context.Context, customerID string, memType string, limit int) ([]model.CustomerLongTermMemory, error) {
	if m.memoryRepo == nil {
		return nil, nil
	}
	if customerID == "" {
		return nil, fmt.Errorf("customer_id cannot be empty")
	}
	if limit <= 0 {
		limit = 50
	}
	return m.memoryRepo.ListLongTermMemories(ctx, customerID, memType, limit)
}

// DeleteLongTermMemory 删除指定 ID 的长期记忆
func (m *MemorySystem) DeleteLongTermMemory(ctx context.Context, id uint64) error {
	if m.memoryRepo == nil {
		return nil
	}
	return m.memoryRepo.DeleteLongTermMemoryByID(ctx, id)
}

func float32SliceToBytes(vec []float32) []byte {
	data, _ := json.Marshal(vec)
	return data
}

func bytesToFloat32Slice(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	var vec []float32
	_ = json.Unmarshal(data, &vec)
	return vec
}

func embeddingToString(vec []float32) string {
	if len(vec) == 0 {
		return ""
	}
	return string(float32SliceToBytes(vec))
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func timePtrValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func effectiveValidFrom(validFrom, createdAt time.Time) time.Time {
	if validFrom.IsZero() {
		return createdAt
	}
	return validFrom
}

func validAtAsOf(validFrom, createdAt, invalidAt, asOf time.Time) bool {
	if effectiveValidFrom(validFrom, createdAt).After(asOf) {
		return false
	}
	return invalidAt.IsZero() || invalidAt.After(asOf)
}

package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/metrics"
)

// MemoryScope L-3 统一门面的读写范围
type MemoryScope string

const (
	MemoryScopeDialogue MemoryScope = "dialogue"
	MemoryScopeFact     MemoryScope = "fact"
)

var facadeWriteTotal = metrics.NewCounter(
	"hivemtk_memory_facade_write_total",
	"Memory facade write total",
	[]string{"scope"},
)

// MemoryWrite 统一写入请求
type MemoryWrite struct {
	Scope      MemoryScope
	SessionID  string
	CustomerID string
	Role       string
	Content    string
	Key        string
	Value      string
	Importance int
	EventAt    time.Time
}

// MemoryQuery 统一读取请求
type MemoryQuery struct {
	CustomerID string
	AsOf       time.Time
	Scope      MemoryScope
	Limit      int
}

// MemoryFact 读取结果中的单条事实（跨库归一化）
type MemoryFact struct {
	Key        string
	Value      string
	Source     string
	Confidence float64
	ValidFrom  time.Time
	InvalidAt  *time.Time
}

type MemoryFacade struct {
	ms *MemorySystem
	dm *DialogueMemoryService
}

// NewMemoryFacade 从已存在对象注入构造（不重复初始化底层系统）
func NewMemoryFacade(db *gorm.DB, ms *MemorySystem, dm *DialogueMemoryService) *MemoryFacade {
	return &MemoryFacade{ms: ms, dm: dm}
}

// Write 统一写入入口：dialogue→DialogueMemory.AppendMessage；fact→MemorySystem.L2SaveFactAt
// 底层实例缺失时静默跳过（与既有 memory 方法 nil 防御语义一致）
func (f *MemoryFacade) Write(ctx context.Context, w MemoryWrite) error {
	if f == nil {
		return nil
	}
	switch w.Scope {
	case MemoryScopeDialogue:
		if f.dm == nil {
			return nil
		}
		facadeWriteTotal.WithLabel(string(MemoryScopeDialogue)).Inc()
		return f.dm.AppendMessage(ctx, w.SessionID, w.CustomerID, dto.Message{Role: w.Role, Content: w.Content})
	case MemoryScopeFact:
		if f.ms == nil {
			return nil
		}
		facadeWriteTotal.WithLabel(string(MemoryScopeFact)).Inc()
		return f.ms.L2SaveFactAt(ctx, w.CustomerID, w.Key, w.Value, w.Importance, w.EventAt)
	default:
		return fmt.Errorf("memory facade: unknown scope %q", w.Scope)
	}
}

// Read 统一读取：合并 memory_items(L2 事实) 与 customer_long_term_memory(L2 向量) 双库结果
// 同键事实取 ValidFrom 最新且 InvalidAt 为空者（pickLatestValid 冲突合并验证门），
// 同键多来源佐证时 Confidence 相加封顶 1.0
func (f *MemoryFacade) Read(ctx context.Context, q MemoryQuery) ([]MemoryFact, error) {
	if f == nil || f.ms == nil {
		return nil, nil
	}
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	byKey := map[string][]MemoryFact{}

	items, err := f.ms.L2ListFactsAsOf(ctx, q.CustomerID, asOf, limit)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		key := factKeyOfItem(it)
		byKey[key] = append(byKey[key], MemoryFact{
			Key:        key,
			Value:      it.Content,
			Source:     "L2_item",
			Confidence: itemDecayConfidence(it),
			ValidFrom:  effectiveValidFrom(timePtrValue(it.ValidFrom), it.CreatedAt),
			InvalidAt:  it.InvalidAt,
		})
	}

	l2v, err := f.ms.ListLongTermMemories(ctx, q.CustomerID, string(model.LongTermMemoryFact), limit)
	if err != nil {
		return nil, err
	}
	for _, it := range l2v {
		vf := timePtrValue(it.ValidFrom)
		if !validAtAsOf(vf, it.CreatedAt, timePtrValue(it.InvalidAt), asOf) {
			continue
		}
		key, val := splitFactKV(it.Content)
		if key == "" {
			key = string(it.MemoryType)
		}
		byKey[key] = append(byKey[key], MemoryFact{
			Key:        key,
			Value:      val,
			Source:     "L2_vector",
			Confidence: float64(it.Importance) / 10.0,
			ValidFrom:  effectiveValidFrom(vf, it.CreatedAt),
			InvalidAt:  it.InvalidAt,
		})
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]MemoryFact, 0, len(keys))
	for _, k := range keys {
		if mf, ok := pickLatestValid(byKey[k], asOf); ok {
			out = append(out, mf)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func factKeyOfItem(it model.MemoryItem) string {
	if it.Metadata != nil {
		if k, ok := it.Metadata["key"].(string); ok && k != "" {
			return k
		}
	}
	return strings.TrimPrefix(it.ItemType, "fact:")
}

func splitFactKV(content string) (string, string) {
	if i := strings.Index(content, "="); i > 0 {
		return content[:i], content[i+1:]
	}
	return "", content
}

func pickLatestValid(cands []MemoryFact, asOf time.Time) (MemoryFact, bool) {
	var best MemoryFact
	found := false
	sum := 0.0
	for _, c := range cands {
		if !validAtAsOf(c.ValidFrom, c.ValidFrom, timePtrValue(c.InvalidAt), asOf) {
			continue
		}
		sum += c.Confidence
		if !found || c.ValidFrom.After(best.ValidFrom) {
			best = c
			found = true
		}
	}
	if !found {
		return MemoryFact{}, false
	}
	if sum > 1.0 {
		sum = 1.0
	}
	best.Confidence = sum
	return best, true
}

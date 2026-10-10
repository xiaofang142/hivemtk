package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
)

const fewShotLoadCooldown = 10 * time.Minute

// FewShotMinCosDefault Few-Shot 检索的最小余弦相似度（编译期兜底）
const FewShotMinCosDefault = 0.7

// fewShotMinCosProvider 由装配层（internal/app/confidence_params_wiring.go）注入，
// 数据源是 config_params 的 confidence.intent_fewshot_min_cos。
var fewShotMinCosProvider = func() float64 { return FewShotMinCosDefault }

// SetFewShotMinCosProvider 注入 Few-Shot 最小余弦读取口；传 nil 视为不注入。
func SetFewShotMinCosProvider(fn func() float64) {
	if fn != nil {
		fewShotMinCosProvider = fn
	}
}

// fewShotMinCos 当前生效的最小余弦相似度。
// 非 (0,1] 的值一律回落兜底：取到 0 会把语义完全不相关的样本也当成命中，
// Few-Shot 反而污染意图判定。
func fewShotMinCos() float64 {
	if c := fewShotMinCosProvider(); c > 0 && c <= 1 {
		return c
	}
	return FewShotMinCosDefault
}

// ProbeFewShotMinCos 导出当前生效的 Few-Shot 最小余弦。
//
// 只给装配层测试用：它要证明「参数中心改了值 → 读取口真的变了」，
// 而 fewShotMinCos 是包内私有、无法从 app 包断言。与其断言 setter 收下了
// 函数（那证明不了行为变化），不如把读取口露出来。
func ProbeFewShotMinCos() float64 { return fewShotMinCos() }

type fewShotCandidate struct {
	Intent string
	Text   string
	Vec    []float32
}

// FewShotStore 全量示例向量缓存 + KATE 语义 kNN 选择器
type FewShotStore struct {
	embedder llm.EmbeddingServiceInterface
	cooldown time.Duration

	mu       sync.Mutex
	loadOnce sync.Once
	cache    map[string][]fewShotCandidate
	loaded   bool
	failAt   time.Time
}

// NewFewShotStore 构造 few-shot 示例库（懒加载，构造不触发向量化）
func NewFewShotStore(embedder llm.EmbeddingServiceInterface) *FewShotStore {
	return &FewShotStore{embedder: embedder, cooldown: fewShotLoadCooldown}
}

// EnsureReady 预热示例向量缓存（外部可用请求 ctx 触发；失败静默进入冷却）
func (f *FewShotStore) EnsureReady(ctx context.Context) {
	f.ensureLoaded(ctx)
}

// SelectExamples 语义 kNN：从缓存示例中选取与查询向量余弦相似度最高的 k 条文本。
// 未加载则先懒加载；选中数不足 k（含全部低于 minCos）返回 nil，由调用方回退静态示例。
func (f *FewShotStore) SelectExamples(queryEmb []float32, k int, minCos float64) []string {
	if f == nil || f.embedder == nil || len(queryEmb) == 0 || k <= 0 {
		return nil
	}
	f.ensureLoaded(context.Background())
	f.mu.Lock()
	size := 0
	for _, cs := range f.cache {
		size += len(cs)
	}
	cands := make([]fewShotCandidate, 0, size)
	for _, cs := range f.cache {
		cands = append(cands, cs...)
	}
	f.mu.Unlock()
	picked := topKByCosine(queryEmb, cands, k, minCos)
	if len(picked) == 0 {
		return nil
	}
	out := make([]string, 0, len(picked))
	for _, c := range picked {
		out = append(out, c.Text)
	}
	return out
}

func (f *FewShotStore) ensureLoaded(ctx context.Context) {
	if f == nil || f.embedder == nil {
		return
	}
	f.loadOnce.Do(func() { f.load(ctx) })
	f.mu.Lock()
	retry := !f.loaded && !f.failAt.IsZero() && time.Since(f.failAt) >= f.cooldown
	f.mu.Unlock()
	if retry {
		f.load(ctx)
	}
}

func (f *FewShotStore) load(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loaded {
		return
	}
	if !f.failAt.IsZero() && time.Since(f.failAt) < f.cooldown {
		return
	}
	cfg := f.embedder.DefaultConfig()
	cache := make(map[string][]fewShotCandidate, len(DefaultIntents))
	failed := 0
	for _, def := range DefaultIntents {
		for _, ex := range def.Examples {
			v, err := f.embedder.EmbedOne(ctx, cfg, ex)
			if err != nil || len(v) == 0 {
				failed++
				continue
			}
			cache[def.Type] = append(cache[def.Type], fewShotCandidate{Intent: def.Type, Text: ex, Vec: v})
		}
	}
	total := 0
	for _, cs := range cache {
		total += len(cs)
	}
	if total == 0 {
		f.failAt = time.Now()
		logger.Warnf("[Intent][FewShot] 示例向量化全部失败 failed=%d，%.0fmin 冷却后重试", failed, f.cooldown.Minutes())
		return
	}
	f.cache = cache
	f.loaded = true
	if failed > 0 {
		logger.Warnf("[Intent][FewShot] 示例向量化部分失败: failed=%d cached=%d", failed, total)
	}
	logger.Infof("[Intent][FewShot] 动态 few-shot 就绪: intents=%d examples=%d", len(cache), total)
}

func topKByCosine(query []float32, cands []fewShotCandidate, k int, minCos float64) []fewShotCandidate {
	if len(query) == 0 || k <= 0 {
		return nil
	}
	type scored struct {
		c   fewShotCandidate
		sim float64
	}
	all := make([]scored, 0, len(cands))
	for _, c := range cands {
		sim := cosineSimilarity(query, c.Vec)
		if sim < minCos {
			continue
		}
		all = append(all, scored{c: c, sim: sim})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	if len(all) > k {
		all = all[:k]
	}
	out := make([]fewShotCandidate, 0, len(all))
	for _, s := range all {
		out = append(out, s.c)
	}
	return out
}

func shouldFallbackStatic(picked, k int) bool {
	return picked < k
}

var (
	fewShotMu     sync.Mutex
	fewShotShared *FewShotStore
)

func recognizerFewShotStore(embedder llm.EmbeddingServiceInterface) *FewShotStore {
	if embedder == nil {
		return nil
	}
	fewShotMu.Lock()
	defer fewShotMu.Unlock()
	if fewShotShared == nil {
		fewShotShared = NewFewShotStore(embedder)
	}
	return fewShotShared
}

func (s *IntentRecognizer) fillTopKExamplesDynamic(ctx context.Context, text string, r *dto.RecognizeResult) {
	if r == nil || r.IntentType == "" || r.IntentType == IntentUnknown || r.IntentType == IntentClarify {
		return
	}
	fallback := func() { fillTopKExamples(r) }
	store := recognizerFewShotStore(s.embedSvc)
	if store == nil {
		fallback()
		return
	}

	defer func() {
		if rec := recover(); rec != nil {
			logger.Warnf("[Intent][FewShot] 动态选择 panic 已隔离，回退静态示例: %v", rec)
			fallback()
		}
	}()
	qvec, err := s.embedSvc.EmbedOne(ctx, s.embedSvc.DefaultConfig(), text)
	if err != nil || len(qvec) == 0 {
		fallback()
		return
	}
	store.EnsureReady(ctx)
	picked := store.SelectExamples(qvec, intentTopKExamples, fewShotMinCos())
	if shouldFallbackStatic(len(picked), intentTopKExamples) {
		fallback()
		return
	}
	r.TopKExamples = picked
}

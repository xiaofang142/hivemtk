package service

import (
	"math"
	"sort"
	"strings"
	"sync"

	"hivemtk-user/internal/dto"
)

// WeakTruthMinConfidence 进入混淆矩阵的最低置信度阈值（编译期兜底，等于阈值即入，之下进低置信桶）
const WeakTruthMinConfidence = 0.9

// weakTruthMinConfidenceProvider 由装配层（internal/app/confidence_params_wiring.go）注入，
// 数据源是 config_params 的 confidence.weak_truth_min_confidence。
var weakTruthMinConfidenceProvider = func() float64 { return WeakTruthMinConfidence }

// SetWeakTruthMinConfidenceProvider 注入弱真值置信度阈值读取口；传 nil 视为不注入。
func SetWeakTruthMinConfidenceProvider(fn func() float64) {
	if fn != nil {
		weakTruthMinConfidenceProvider = fn
	}
}

// weakTruthMinConfidence 当前生效的最低置信度阈值。
// 非 (0,1] 的值一律回落兜底：取到 0 会把所有低置信预测都算进混淆矩阵，
// 矩阵分母被噪声灌满，PR 指标随之失真。
func weakTruthMinConfidence() float64 {
	if t := weakTruthMinConfidenceProvider(); t > 0 && t <= 1 {
		return t
	}
	return WeakTruthMinConfidence
}

// ProbeWeakTruthMinConfidence 导出当前生效的弱真值阈值（仅供装配层测试断言读取口）。
func ProbeWeakTruthMinConfidence() float64 { return weakTruthMinConfidence() }

var fallbackClassSet = map[string]bool{
	IntentUnknown:  true,
	IntentClarify:  true,
	"fallback":     true,
	"out_of_scope": true,
}

// IntentPR 单个意图类别的弱标签指标
type IntentPR struct {
	Precision float64
	Recall    float64
	F1        float64
	TP        int
	FP        int
	FN        int
}

// IntentMetricsSnapshot 混淆矩阵快照；MacroF1 不含兜底/超范围类
type IntentMetricsSnapshot struct {
	PerClass     map[string]IntentPR
	MacroF1      float64
	Total        int64
	LowConf      map[string]int64
	LowConfTotal int64
}

// ConfusionStore 弱标签混淆矩阵：map[predicted]map[weakTruth]int + 低置信桶独立计数
type ConfusionStore struct {
	mu       sync.Mutex
	matrix   map[string]map[string]int
	lowConf  map[string]int64
	total    int64
	lowTotal int64
}

// NewConfusionStore 构造空混淆矩阵
func NewConfusionStore() *ConfusionStore {
	return &ConfusionStore{
		matrix:  make(map[string]map[string]int),
		lowConf: make(map[string]int64),
	}
}

// Reset 清空全部统计（运维/测试用）
func (c *ConfusionStore) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.matrix = make(map[string]map[string]int)
	c.lowConf = make(map[string]int64)
	c.total = 0
	c.lowTotal = 0
}

// RecordPrediction 规格入口（predicted, gold, confidence）：
//   - predicted 或 gold 为空直接忽略（防御式，保证打点永不干扰主链路）；
//   - confidence >= WeakTruthMinConfidence 时入矩阵 matrix[predicted][gold]++；
//   - 低于阈值进低置信桶（键格式 "predicted|gold"），不影响指标分子分母。
func (c *ConfusionStore) RecordPrediction(predicted string, gold string, confidence float64) {
	if predicted == "" || gold == "" {
		return
	}
	if confidence < weakTruthMinConfidence() {
		c.mu.Lock()
		c.lowConf[predicted+"|"+gold]++
		c.lowTotal++
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.matrix[predicted] == nil {
		c.matrix[predicted] = make(map[string]int)
	}
	c.matrix[predicted][gold]++
	c.total++
}

// Snapshot 导出快照（深拷贝，返回后与内部状态解耦）。
// PerClass 覆盖矩阵中出现过的所有类（含兜底类）；MacroF1 仅对非兜底类求均值。
func (c *ConfusionStore) Snapshot() IntentMetricsSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := IntentMetricsSnapshot{
		PerClass:     make(map[string]IntentPR, len(c.matrix)),
		Total:        c.total,
		LowConf:      make(map[string]int64, len(c.lowConf)),
		LowConfTotal: c.lowTotal,
	}
	for k, v := range c.lowConf {
		out.LowConf[k] = v
	}

	classSet := make(map[string]bool)
	for predicted, row := range c.matrix {
		classSet[predicted] = true
		for weak := range row {
			classSet[weak] = true
		}
	}

	macroSum, macroN := 0.0, 0
	for _, cls := range sortedKeys(classSet) {
		row := c.matrix[cls]
		var fp, fn int
		for w, n := range row {
			if w != cls {
				fp += n
			}
		}
		for p, otherRow := range c.matrix {
			if p != cls {
				fn += otherRow[cls]
			}
		}
		tp := row[cls]
		pr := IntentPR{TP: tp, FP: fp, FN: fn}
		pr.Precision = ratio(tp, tp+fp)
		pr.Recall = ratio(tp, tp+fn)
		pr.F1 = f1Of(pr.Precision, pr.Recall)
		out.PerClass[cls] = pr
		if !fallbackClassSet[cls] && tp+fp+fn > 0 {
			macroSum += pr.F1
			macroN++
		}
	}
	if macroN > 0 {
		out.MacroF1 = macroSum / float64(macroN)
	}
	return out
}

func ratio(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func f1Of(p, r float64) float64 {
	if p+r <= 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var globalIntentMetrics = NewConfusionStore()

// RecordIntentWeakLabel 意图识别汇聚点的零阻塞打点入口：
// 仅当规则引擎真实命中时，以命中意图同时充当 predicted 与 weakTruth（伪真值）。
// 兜底结果（Method="rule" 但 IntentType=unknown）、Embedding/LLM/disabled 结果一律跳过。
func RecordIntentWeakLabel(res *dto.RecognizeResult) {
	if res == nil || res.Method != "rule" || res.IntentType == "" || res.IntentType == IntentUnknown {
		return
	}
	globalIntentMetrics.RecordPrediction(res.IntentType, res.IntentType, res.Confidence)
}

// IntentClassCounters 单意图混淆计数
type IntentClassCounters struct {
	TP int
	FP int
	FN int
}

// IntentClassScore 单意图 P/R/F1（快照中已四舍五入到 4 位小数）
type IntentClassScore struct {
	Precision float64
	Recall    float64
	F1        float64
}

// IntentMetricsRegistrySnapshot 快照：per-intent 指标 + 宏平均 + 兜底计数
type IntentMetricsRegistrySnapshot struct {
	PerIntent map[string]IntentClassScore
	MacroAvg  float64
	Fallback  int64
	Total     int64
}

// IntentMetricsRegistry per-intent 混淆矩阵注册表（mutex + map[intent]counters）
type IntentMetricsRegistry struct {
	mu       sync.Mutex
	counters map[string]*IntentClassCounters
	fallback int64
	total    int64
}

// NewIntentMetricsRegistry 构造空注册表
func NewIntentMetricsRegistry() *IntentMetricsRegistry {
	return &IntentMetricsRegistry{counters: make(map[string]*IntentClassCounters)}
}

// Reset 清空全部统计（测试/运维用）
func (r *IntentMetricsRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = make(map[string]*IntentClassCounters)
	r.fallback = 0
	r.total = 0
}

const fallbackPredictedClass = "fallback"

// RecordPrediction 记一条 (gold, predicted) 样本。confidence 当前口径不参与判定
// （gold 非空即记），保留参数以便后续按置信度分桶扩展。
func (r *IntentMetricsRegistry) RecordPrediction(gold string, predicted string, confidence float64) {
	_ = confidence
	g := strings.TrimSpace(gold)
	if g == "" {
		return
	}
	p := strings.TrimSpace(predicted)
	if p == fallbackPredictedClass {
		r.mu.Lock()
		r.fallback++
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p == "" {

		r.fnc(g).FN++
		r.total++
		return
	}
	if p == g {
		r.countersOf(g).TP++
	} else {
		r.countersOf(p).FP++
		r.fnc(g).FN++
	}
	r.total++
}

func (r *IntentMetricsRegistry) countersOf(intent string) *IntentClassCounters {
	c, ok := r.counters[intent]
	if !ok {
		c = &IntentClassCounters{}
		r.counters[intent] = c
	}
	return c
}

func (r *IntentMetricsRegistry) fnc(gold string) *IntentClassCounters {
	return r.countersOf(gold)
}

// Snapshot 导出 per-intent P/R/F1（保留 4 位小数）+ 宏平均；深拷贝与内部状态解耦
func (r *IntentMetricsRegistry) Snapshot() IntentMetricsRegistrySnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := IntentMetricsRegistrySnapshot{
		PerIntent: make(map[string]IntentClassScore, len(r.counters)),
		Fallback:  r.fallback,
		Total:     r.total,
	}
	macroSum, macroN := 0.0, 0
	for _, intent := range sortedKeysOf(r.counters) {
		c := r.counters[intent]
		score := IntentClassScore{
			Precision: ratio(c.TP, c.TP+c.FP),
			Recall:    ratio(c.TP, c.TP+c.FN),
		}
		score.F1 = f1Of(score.Precision, score.Recall)
		score.Precision = roundTo4(score.Precision)
		score.Recall = roundTo4(score.Recall)
		score.F1 = roundTo4(score.F1)
		out.PerIntent[intent] = score
		if !fallbackClassSet[intent] {
			macroSum += score.F1
			macroN++
		}
	}
	if macroN > 0 {
		out.MacroAvg = roundTo4(macroSum / float64(macroN))
	}
	return out
}

func sortedKeysOf(m map[string]*IntentClassCounters) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func roundTo4(v float64) float64 {
	return math.Round(v*1e4) / 1e4
}

var defaultIntentMetricsRegistry = NewIntentMetricsRegistry()

// DefaultIntentMetricsRegistry 进程级默认注册表
func DefaultIntentMetricsRegistry() *IntentMetricsRegistry { return defaultIntentMetricsRegistry }

// ResetIntentMetrics 重置全局注册表（测试用）
func ResetIntentMetrics() { defaultIntentMetricsRegistry.Reset() }

// Package metrics 提供轻量级指标采集（应用层巡检用）。
//
// 设计目标：
//   - 零外部依赖（不引入外部监控库）
//   - 支持 Counter / Gauge / Histogram 三种基础类型
//   - 文本格式通过 /metrics 端点暴露（用于内部调试/巡检）
//   - 线程安全（atomic + mutex）
//   - 标签（labels）支持
//
// 用法：
//
//	metrics.NewCounter("bridge_ingest_total", "Total ingest requests", []string{"channel", "agent_id"})
//	metrics.GetCounter("bridge_ingest_total").WithLabel("xhs", "agent-1").Inc()
//
//	metrics.NewHistogram("bridge_request_duration_ms", "Request duration in ms", []string{"channel"}, []float64{1, 5, 10, 50, 100, 500, 1000, 5000})
//	metrics.GetHistogram("bridge_request_duration_ms").WithLabel("xhs").Observe(123.4)
//
// 暴露端点：
//
//	http.HandleFunc("/metrics", metrics.Handler())
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Metric 指标接口
type Metric interface {
	Name() string
	Help() string
	Type() string
	Write(w io.Writer)
}

func labelsKey(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, "\x00")
}

func writeLabels(sb *strings.Builder, labelNames, values []string) {
	if len(labelNames) == 0 {
		return
	}
	sb.WriteByte('{')
	for i, n := range labelNames {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(n)
		sb.WriteString(`="`)
		sb.WriteString(escapeLabelValue(values[i]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
}

func writeLabelsWithExtra(sb *strings.Builder, labelNames, values []string, extraName, extraValue string) {
	sb.WriteByte('{')
	first := true
	for i, n := range labelNames {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(n)
		sb.WriteString(`="`)
		sb.WriteString(escapeLabelValue(values[i]))
		sb.WriteByte('"')
	}
	if !first {
		sb.WriteByte(',')
	}
	sb.WriteString(extraName)
	sb.WriteString(`="`)
	sb.WriteString(escapeLabelValue(extraValue))
	sb.WriteByte('"')
	sb.WriteByte('}')
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}

// CounterVec 计数器集合
type CounterVec struct {
	name      string
	help      string
	labelKeys []string
	mu        sync.RWMutex
	values    map[string]*counterCell
}

type counterCell struct {
	v      atomic.Uint64
	values []string
}

// NewCounterVec 创建计数器
func NewCounterVec(name, help string, labelKeys []string) *CounterVec {
	return &CounterVec{
		name:      name,
		help:      help,
		labelKeys: append([]string{}, labelKeys...),
		values:    make(map[string]*counterCell),
	}
}

// WithLabel 获取指定标签的计数器
func (c *CounterVec) WithLabel(values ...string) *Counter {
	if len(values) != len(c.labelKeys) {
		panic(fmt.Sprintf("metrics: counter %q expects %d labels, got %d", c.name, len(c.labelKeys), len(values)))
	}
	key := labelsKey(values)
	c.mu.RLock()
	if v, ok := c.values[key]; ok {
		c.mu.RUnlock()
		return &Counter{cell: v}
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.values[key]; ok {
		return &Counter{cell: v}
	}
	v := &counterCell{values: append([]string{}, values...)}
	c.values[key] = v
	return &Counter{cell: v}
}

// Counter 单个计数器
type Counter struct {
	cell *counterCell
}

// Inc 自增 1
func (c *Counter) Inc() { c.cell.v.Add(1) }

// Add 增加 n（n>=0）
func (c *Counter) Add(n uint64) { c.cell.v.Add(n) }

// Value 当前值
func (c *Counter) Value() uint64 { return c.cell.v.Load() }

// Name 指标名
func (c *CounterVec) Name() string { return c.name }

// Help 指标帮助信息
func (c *CounterVec) Help() string { return c.help }

// Type 指标类型
func (c *CounterVec) Type() string { return "counter" }

// Write 写指标文本格式（counter）
func (c *CounterVec) Write(w io.Writer) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help)
	_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", c.name)

	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.values) == 0 {
		return
	}
	type entry struct {
		key  string
		cell *counterCell
	}
	entries := make([]entry, 0, len(c.values))
	for k, v := range c.values {
		entries = append(entries, entry{key: k, cell: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	var sb strings.Builder
	for _, e := range entries {
		sb.Reset()
		sb.WriteString(c.name)
		writeLabels(&sb, c.labelKeys, e.cell.values)
		_, _ = fmt.Fprintf(w, "%s %d\n", sb.String(), e.cell.v.Load())
	}
}

// GaugeVec 仪表集合
type GaugeVec struct {
	name      string
	help      string
	labelKeys []string
	mu        sync.RWMutex
	values    map[string]*gaugeCell
}

type gaugeCell struct {
	v      atomic.Int64
	values []string
}

// NewGaugeVec 创建仪表
func NewGaugeVec(name, help string, labelKeys []string) *GaugeVec {
	return &GaugeVec{
		name:      name,
		help:      help,
		labelKeys: append([]string{}, labelKeys...),
		values:    make(map[string]*gaugeCell),
	}
}

// WithLabel 获取指定标签的 gauge
func (g *GaugeVec) WithLabel(values ...string) *Gauge {
	if len(values) != len(g.labelKeys) {
		panic(fmt.Sprintf("metrics: gauge %q expects %d labels, got %d", g.name, len(g.labelKeys), len(values)))
	}
	key := labelsKey(values)
	g.mu.RLock()
	if v, ok := g.values[key]; ok {
		g.mu.RUnlock()
		return &Gauge{cell: v}
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	if v, ok := g.values[key]; ok {
		return &Gauge{cell: v}
	}
	v := &gaugeCell{values: append([]string{}, values...)}
	g.values[key] = v
	return &Gauge{cell: v}
}

// Gauge 单个仪表
type Gauge struct {
	cell *gaugeCell
}

// Set 设定值
func (g *Gauge) Set(v int64) { g.cell.v.Store(v) }

// SetFloat 设定浮点值（转为 int64 bits 存储；读取时再转换）
// 当前用最简实现：乘以 1000 保留 3 位小数（适用于 ratio 0-1 类指标）
// 真正的 SLO 跟踪应使用专用 float64 gauge；此处为简化版。
func (g *Gauge) SetFloat(v float64) {
	g.cell.v.Store(int64(v * 1000))
}

// Float 当前浮点值（与 SetFloat 配对）
func (g *Gauge) Float() float64 {
	return float64(g.cell.v.Load()) / 1000.0
}

// Inc 自增 1
func (g *Gauge) Inc() { g.cell.v.Add(1) }

// Dec 自减 1
func (g *Gauge) Dec() { g.cell.v.Add(-1) }

// Add 增加值（可负）
func (g *Gauge) Add(n int64) { g.cell.v.Add(n) }

// Value 当前值
func (g *Gauge) Value() int64 { return g.cell.v.Load() }

// Name 指标名
func (g *GaugeVec) Name() string { return g.name }

// Help 指标帮助信息
func (g *GaugeVec) Help() string { return g.help }

// Type 指标类型
func (g *GaugeVec) Type() string { return "gauge" }

// Write 写指标文本格式（gauge）
func (g *GaugeVec) Write(w io.Writer) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help)
	_, _ = fmt.Fprintf(w, "# TYPE %s gauge\n", g.name)

	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.values) == 0 {
		return
	}
	type entry struct {
		key  string
		cell *gaugeCell
	}
	entries := make([]entry, 0, len(g.values))
	for k, v := range g.values {
		entries = append(entries, entry{key: k, cell: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	var sb strings.Builder
	for _, e := range entries {
		sb.Reset()
		sb.WriteString(g.name)
		writeLabels(&sb, g.labelKeys, e.cell.values)
		_, _ = fmt.Fprintf(w, "%s %d\n", sb.String(), e.cell.v.Load())
	}
}

// HistogramVec 直方图集合
type HistogramVec struct {
	name      string
	help      string
	labelKeys []string
	buckets   []float64
	mu        sync.RWMutex
	values    map[string]*histogramCell
}

type histogramCell struct {
	mu     sync.Mutex
	count  atomic.Uint64
	sumPtr atomic.Pointer[float64]
	counts []atomic.Uint64
	values []string
}

// NewHistogramVec 创建直方图
func NewHistogramVec(name, help string, labelKeys []string, buckets []float64) *HistogramVec {
	if len(buckets) == 0 {
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	}
	sorted := append([]float64{}, buckets...)
	sort.Float64s(sorted)

	return &HistogramVec{
		name:      name,
		help:      help,
		labelKeys: append([]string{}, labelKeys...),
		buckets:   sorted,
		values:    make(map[string]*histogramCell),
	}
}

// WithLabel 获取指定标签的直方图
func (h *HistogramVec) WithLabel(values ...string) *Histogram {
	if len(values) != len(h.labelKeys) {
		panic(fmt.Sprintf("metrics: histogram %q expects %d labels, got %d", h.name, len(h.labelKeys), len(values)))
	}
	key := labelsKey(values)
	h.mu.RLock()
	if v, ok := h.values[key]; ok {
		h.mu.RUnlock()
		return &Histogram{cell: v, buckets: h.buckets}
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	if v, ok := h.values[key]; ok {
		return &Histogram{cell: v, buckets: h.buckets}
	}
	v := &histogramCell{
		counts: make([]atomic.Uint64, len(h.buckets)+1),
		values: append([]string{}, values...),
	}
	zero := 0.0
	v.sumPtr.Store(&zero)
	h.values[key] = v
	return &Histogram{cell: v, buckets: h.buckets}
}

// Histogram 单个直方图
type Histogram struct {
	cell    *histogramCell
	buckets []float64
}

// Observe 观测一个值
func (h *Histogram) Observe(v float64) {
	h.cell.mu.Lock()
	defer h.cell.mu.Unlock()
	h.cell.count.Add(1)
	oldPtr := h.cell.sumPtr.Load()
	old := *oldPtr
	newVal := old + v
	h.cell.sumPtr.Store(&newVal)
	for i, b := range h.buckets {
		if v <= b {
			h.cell.counts[i].Add(1)
		}
	}
	h.cell.counts[len(h.buckets)].Add(1)
}

// Name 指标名
func (h *HistogramVec) Name() string { return h.name }

// Help 指标帮助信息
func (h *HistogramVec) Help() string { return h.help }

// Type 指标类型
func (h *HistogramVec) Type() string { return "histogram" }

// Write 写指标文本格式（histogram）
func (h *HistogramVec) Write(w io.Writer) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help)
	_, _ = fmt.Fprintf(w, "# TYPE %s histogram\n", h.name)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.values) == 0 {
		return
	}
	type entry struct {
		key  string
		cell *histogramCell
	}
	entries := make([]entry, 0, len(h.values))
	for k, v := range h.values {
		entries = append(entries, entry{key: k, cell: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	var sb strings.Builder
	for _, e := range entries {
		for i, b := range h.buckets {
			sb.Reset()
			sb.WriteString(h.name)
			sb.WriteString("_bucket")
			writeLabelsWithExtra(&sb, h.labelKeys, e.cell.values, "le", formatFloat(b))
			_, _ = fmt.Fprintf(w, "%s %d\n", sb.String(), e.cell.counts[i].Load())
		}
		sb.Reset()
		sb.WriteString(h.name)
		sb.WriteString("_bucket")
		writeLabelsWithExtra(&sb, h.labelKeys, e.cell.values, "le", "+Inf")
		_, _ = fmt.Fprintf(w, "%s %d\n", sb.String(), e.cell.counts[len(h.buckets)].Load())
		sumPtr := e.cell.sumPtr.Load()
		sum := 0.0
		if sumPtr != nil {
			sum = *sumPtr
		}
		sb.Reset()
		sb.WriteString(h.name)
		sb.WriteString("_sum")
		writeLabels(&sb, h.labelKeys, e.cell.values)
		_, _ = fmt.Fprintf(w, "%s %s\n", sb.String(), formatFloat(sum))
		sb.Reset()
		sb.WriteString(h.name)
		sb.WriteString("_count")
		writeLabels(&sb, h.labelKeys, e.cell.values)
		_, _ = fmt.Fprintf(w, "%s %d\n", sb.String(), e.cell.count.Load())
	}
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Metric)
)

// Register 注册指标
func Register(m Metric) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[m.Name()] = m
}

// NewCounter 创建并注册 Counter
func NewCounter(name, help string, labelKeys []string) *CounterVec {
	c := NewCounterVec(name, help, labelKeys)
	Register(c)
	return c
}

// NewGauge 创建并注册 Gauge
func NewGauge(name, help string, labelKeys []string) *GaugeVec {
	g := NewGaugeVec(name, help, labelKeys)
	Register(g)
	return g
}

// NewHistogram 创建并注册 Histogram
func NewHistogram(name, help string, labelKeys []string, buckets []float64) *HistogramVec {
	h := NewHistogramVec(name, help, labelKeys, buckets)
	Register(h)
	return h
}

// Snapshot 返回所有注册指标的当前数值快照
//
// 用于告警检查器（AlertChecker）按指标名匹配阈值：
//   - Counter：返回累计值（按所有 label 聚合）
//   - Gauge：返回最后一个写入的值（按所有 label 聚合，取 max）
//   - Histogram：返回 _count（样本总数）
//
// 返回 map[metricName]value。同名带 label 的指标会被聚合为单值。
func Snapshot() map[string]float64 {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]float64, len(registry))
	for name, m := range registry {
		var samples []Sample
		switch t := m.(type) {
		case *CounterVec:
			samples = t.samples()
			for _, s := range samples {
				out[name] += s.Value
			}
		case *GaugeVec:
			samples = t.samples()
			for _, s := range samples {
				if s.Value > out[name] {
					out[name] = s.Value
				}
			}
		case *HistogramVec:
			samples = t.samples()
			for _, s := range samples {
				if s.Agg == "count" {
					out[name+"_count"] += s.Value
				}
			}
		}
	}
	return out
}

// MustGetCounter 取出已注册的 Counter
func MustGetCounter(name string) *CounterVec {
	m := Get(name)
	if m == nil {
		panic(fmt.Sprintf("metrics: counter %q not registered", name))
	}
	if c, ok := m.(*CounterVec); ok {
		return c
	}
	panic(fmt.Sprintf("metrics: %q is not a counter", name))
}

// MustGetHistogram 取出已注册的 Histogram
func MustGetHistogram(name string) *HistogramVec {
	m := Get(name)
	if m == nil {
		panic(fmt.Sprintf("metrics: histogram %q not registered", name))
	}
	if h, ok := m.(*HistogramVec); ok {
		return h
	}
	panic(fmt.Sprintf("metrics: %q is not a histogram", name))
}

// MustGetGauge 取出已注册的 Gauge
func MustGetGauge(name string) *GaugeVec {
	m := Get(name)
	if m == nil {
		panic(fmt.Sprintf("metrics: gauge %q not registered", name))
	}
	if g, ok := m.(*GaugeVec); ok {
		return g
	}
	panic(fmt.Sprintf("metrics: %q is not a gauge", name))
}

// Get 按名称取出指标
func Get(name string) Metric {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}

// Gather 收集所有指标并写入 w
func Gather(w io.Writer) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		registry[n].Write(w)
		_, _ = fmt.Fprintln(w)
	}
}

// Handler 返回 /metrics 端点 handler（用于内部调试/巡检）
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		Gather(w)
	}
}

type Sample struct {
	Name      string
	Type      string
	LabelKeys []string
	Labels    []string
	Value     float64
	Agg       string
}

func (c *CounterVec) samples() []Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Sample, 0, len(c.values))
	for _, cell := range c.values {
		out = append(out, Sample{
			Name:      c.name,
			Type:      c.Type(),
			LabelKeys: append([]string{}, c.labelKeys...),
			Labels:    append([]string{}, cell.values...),
			Value:     float64(cell.v.Load()),
		})
	}
	return out
}

func (g *GaugeVec) samples() []Sample {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Sample, 0, len(g.values))
	for _, cell := range g.values {
		out = append(out, Sample{
			Name:      g.name,
			Type:      g.Type(),
			LabelKeys: append([]string{}, g.labelKeys...),
			Labels:    append([]string{}, cell.values...),
			Value:     float64(cell.v.Load()),
		})
	}
	return out
}

func (h *HistogramVec) samples() []Sample {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Sample, 0, len(h.values)*2)
	for _, cell := range h.values {
		sum := 0.0
		if ptr := cell.sumPtr.Load(); ptr != nil {
			sum = *ptr
		}
		labels := append([]string{}, cell.values...)
		out = append(out,
			Sample{Name: h.name, Type: h.Type(), LabelKeys: append([]string{}, h.labelKeys...), Labels: labels, Value: float64(cell.count.Load()), Agg: "count"},
			Sample{Name: h.name, Type: h.Type(), LabelKeys: append([]string{}, h.labelKeys...), Labels: labels, Value: sum, Agg: "sum"},
		)
	}
	return out
}

// CollectSamples 遍历全局注册表，收集所有已注册指标（Counter/Gauge/Histogram）的当前值快照。
// 线程安全：返回的是复制后的切片，不持有注册表锁。
// 供 metrics 落库 sink（bridge_sink.go）与 SQL 巡检使用。
func CollectSamples() []Sample {
	registryMu.RLock()
	ms := make([]Metric, 0, len(registry))
	for _, m := range registry {
		ms = append(ms, m)
	}
	registryMu.RUnlock()

	out := make([]Sample, 0, len(ms)*2)
	for _, m := range ms {
		switch v := m.(type) {
		case *CounterVec:
			out = append(out, v.samples()...)
		case *GaugeVec:
			out = append(out, v.samples()...)
		case *HistogramVec:
			out = append(out, v.samples()...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Agg != out[j].Agg {
			return out[i].Agg < out[j].Agg
		}
		return strings.Join(out[i].Labels, "\x00") < strings.Join(out[j].Labels, "\x00")
	})
	return out
}

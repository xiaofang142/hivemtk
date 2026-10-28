package browsercore

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// 观测契约（解决「整页 snapshot 装不进 4000 字节工具结果」的问题）：
//   - Targets 只保留可交互/有语义节点，带 loc 供跨轮重新解析；
//   - Text 裁剪到预算内；
//   - 全量 snapshot 由执行侧落盘 / 由 handle 指向，模型按需再取。
const (
	DefaultMaxTargets = 40
	DefaultMaxText    = 1200
)

var snapshotNodeRE = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*\[(.*)\]\s*$`)
var snapshotRefRE = regexp.MustCompile(`ref=(\d+)`)
var snapshotLocRE = regexp.MustCompile(`loc=([^,\]]+)`)
var snapshotTextRE = regexp.MustCompile(`^\s*text\s+"(.*)"\s*$`)

// ParseSnapshot 解析 ego-browser 的 snapshot 文本（E16 实测形状）。
// 仅用于服务端 golden test / script 模式兜底；ops 模式的解析在 runner 脚本内完成
// （同一套规则，双端以样例集钉死）。
func ParseSnapshot(snapshot string) ([]Target, string) {
	lines := strings.Split(snapshot, "\n")
	out := make([]Target, 0, DefaultMaxTargets)
	var texts []string
	var pendingRef = -1
	flushName := func(name string) {
		if pendingRef >= 0 && pendingRef < len(out) && out[pendingRef].Name == "" {
			out[pendingRef].Name = name
		}
		pendingRef = -1
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if m := snapshotNodeRE.FindStringSubmatch(line); m != nil {
			attrs := m[2]
			refm := snapshotRefRE.FindStringSubmatch(attrs)
			if refm == nil {
				pendingRef = -1
				continue
			}
			loc := ""
			if lm := snapshotLocRE.FindStringSubmatch(attrs); lm != nil {
				loc = lm[1]
			}
			out = append(out, Target{Ref: "e" + refm[1], Role: m[1], Loc: loc})
			pendingRef = len(out) - 1
			continue
		}
		if m := snapshotTextRE.FindStringSubmatch(line); m != nil {
			texts = append(texts, m[1])
			flushName(m[1])
		}
	}
	return out, strings.Join(texts, "\n")
}

// Sanitize 裁剪 + 脱敏（服务端兜底：即使 runner/脚本回传超量，也保证入库与回灌有界）。
func (o *Observation) Sanitize(maxTargets, maxText int, extraPatterns []string) {
	if maxTargets <= 0 {
		maxTargets = DefaultMaxTargets
	}
	if maxText <= 0 {
		maxText = DefaultMaxText
	}
	if len(o.Targets) > maxTargets {
		o.Targets = o.Targets[:maxTargets]
	}
	for i := range o.Targets {
		o.Targets[i].Name = Redact(o.Targets[i].Name, extraPatterns)
	}
	if utf8.RuneCountInString(o.Text) > maxText {
		r := []rune(o.Text)
		o.Text = string(r[:maxText])
		o.TextTruncated = true
	}
	o.Text = Redact(o.Text, extraPatterns)
	o.Title = Redact(o.Title, extraPatterns)
}

var defaultRedactors = []*regexp.Regexp{
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	regexp.MustCompile(`\b1[3-9]\d{9}\b`),
	regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|api[_-]?key)\b\s*[:=]\s*[^\s,;]+`),
}

// Redact 默认脱敏（邮箱/手机号/凭据），再套自定义正则。自定义正则非法则忽略。
func Redact(s string, extraPatterns []string) string {
	if s == "" {
		return s
	}
	for _, re := range defaultRedactors {
		s = re.ReplaceAllString(s, "***")
	}
	for _, p := range extraPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		s = re.ReplaceAllString(s, "***")
	}
	return s
}

// DefaultObserveSpec 观测缺省：视口快照、40 个目标、1200 字符、出错截图。
func DefaultObserveSpec() ObserveSpec {
	return ObserveSpec{
		Snapshot:   true,
		Scope:      "only_within_viewport",
		MaxTargets: DefaultMaxTargets,
		MaxText:    DefaultMaxText,
		Screenshot: "on_error",
	}
}

// NormalizeObserve 填缺省 + 上限收敛。
func (s *ObserveSpec) Normalize() {
	d := DefaultObserveSpec()
	// ego-browser 的合法 scope 只有 full_page / only_within_viewport / subtree
	// （实测：传 "viewport" 会抛 "scope must be one of ..."）。兼容旧调用别名。
	if s.Scope == "viewport" {
		s.Scope = "only_within_viewport"
	}
	if s.Scope == "" {
		s.Scope = d.Scope
	}
	if s.Scope != "only_within_viewport" && s.Scope != "full_page" && s.Scope != "subtree" {
		s.Scope = d.Scope
	}
	if s.MaxTargets <= 0 {
		s.MaxTargets = d.MaxTargets
	}
	if s.MaxTargets > 100 {
		s.MaxTargets = 100
	}
	if s.MaxText <= 0 {
		s.MaxText = d.MaxText
	}
	if s.MaxText > 4000 {
		s.MaxText = 4000
	}
	if s.Screenshot != "" && s.Screenshot != "on_error" && s.Screenshot != "always" {
		s.Screenshot = d.Screenshot
	}
}

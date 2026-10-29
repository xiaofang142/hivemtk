package egolite

// telemetry.go —— S-9 遥测口径（隐私优先：**永不收页面内容**）。
//
// 口径不是"过滤掉敏感字段"，而是**默认拒绝**：只有本文件白名单里的键能进遥测，
// 其余一律丢弃并计入 Dropped。这样新增的 runner 字段不会因为"忘了加过滤"而泄露。
//
// 三条硬约束（评审口径，改动前先读）：
//  1. 不收任何自由文本：错误只收**分类码**（error_class），不收 message/stack；
//  2. 不收任何 URL/选择器/DOM/截图：这些天然带页面内容或可反推内容；
//  3. 数值字段只收聚合量（耗时、条数、状态），单条记录不落原文。
//
// 为什么用白名单而不是黑名单：黑名单永远落后于新增字段（"page_html_v2" 会漏），
// 而遥测是**可观测性**需求，不是业务数据需求——少一个字段只影响看板，多一个字段是事故。

import (
	"math"
	"sort"
	"strings"
	"time"
)

// TelemetryEvent 一条遥测样本（最终落库/上报的形状，全部是低基数枚举或数值）。
type TelemetryEvent struct {
	Kind       string    `json:"kind"`        // run / install / upgrade / sync
	At         time.Time `json:"at"`          // 采样时刻（UTC）
	DurationMS int64     `json:"duration_ms"` // 耗时；未知为 0
	OK         bool      `json:"ok"`
	SkillID    string    `json:"skill_id"`    // slug 形状（[a-z0-9_-]），非 slug 一律丢弃
	ErrorClass string    `json:"error_class"` // 分类码（timeout/network/auth/scan/unknown）
	RunnerID   string    `json:"runner_id"`   // device_id（安装时生成，非客户标识）
}

// telemetryStringKeys 允许进遥测的字符串键（值另有形状校验）。
var telemetryStringKeys = map[string]bool{
	"kind":        true,
	"skill_id":    true,
	"error_class": true,
	"runner_id":   true,
}

// telemetryIntKeys 允许进遥测的数值键。
var telemetryIntKeys = map[string]bool{
	"duration_ms": true,
}

// telemetryBoolKeys 允许进遥测的布尔键。
var telemetryBoolKeys = map[string]bool{
	"ok": true,
}

// maxTelemetryStringLen 字符串上限：超长一律丢弃（防把页面片段塞进 error_class）。
const maxTelemetryStringLen = 64

// SanitizeTelemetry 把一条来路不明的样本收敛成 TelemetryEvent。
//
// 返回的 dropped 是"被丢弃的键数"：>0 表示上游送来了不该送的字段（含页面内容键），
// 调用方应把它计入指标（这是唯一能发现"采集面写歪了"的信号）。
func SanitizeTelemetry(raw map[string]any) (TelemetryEvent, int, error) {
	ev := TelemetryEvent{}
	dropped := 0
	for k, v := range raw {
		switch {
		case telemetryStringKeys[k]:
			s, ok := v.(string)
			if !ok || len(s) > maxTelemetryStringLen || !safeTelemetryToken(s) {
				dropped++
				continue
			}
			switch k {
			case "kind":
				ev.Kind = s
			case "skill_id":
				ev.SkillID = s
			case "error_class":
				ev.ErrorClass = s
			case "runner_id":
				ev.RunnerID = s
			}
		case telemetryIntKeys[k]:
			switch n := v.(type) {
			case int:
				ev.DurationMS = int64(n)
			case int64:
				ev.DurationMS = n
			case float64:
				ev.DurationMS = int64(n)
			default:
				dropped++
			}
		case k == "at":
			tm, ok := v.(time.Time)
			if !ok {
				dropped++
				continue
			}
			ev.At = tm.UTC()
		case telemetryBoolKeys[k]:
			b, ok := v.(bool)
			if !ok {
				dropped++
				continue
			}
			ev.OK = b
		default:
			// 白名单之外的一切（含 page_html / url / text / screenshot…）直接丢。
			dropped++
		}
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	if ev.Kind == "" {
		return TelemetryEvent{}, dropped, errTelemetryKindRequired
	}
	return ev, dropped, nil
}

type telemetryError string

func (e telemetryError) Error() string { return string(e) }

const errTelemetryKindRequired = telemetryError("telemetry: 缺 kind（遥测必须以事件类型为维度）")

// safeTelemetryToken 只允许标识符形状：字母数字与 _-.:@（device_id、slug、分类码）。
// 空格/引号/尖括号/斜杠一律拒——这些是"混进自由文本"的最短路径。
func safeTelemetryToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == ':', r == '@':
		default:
			return false
		}
	}
	return true
}

// TelemetryBucket 一组样本的聚合（看板直接渲染这一层，不暴露单条明细）。
type TelemetryBucket struct {
	Kind       string  `json:"kind"`
	Count      int     `json:"count"`
	OKCount    int     `json:"ok_count"`
	FailCount  int     `json:"fail_count"`
	P95MS      int64   `json:"p95_ms"`
	SuccessPct float64 `json:"success_pct"`
}

// AggregateTelemetry 按 kind 聚合（样本量十位~千位级，内存排序足够）。
func AggregateTelemetry(events []TelemetryEvent) []TelemetryBucket {
	byKind := map[string][]TelemetryEvent{}
	for _, e := range events {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	out := make([]TelemetryBucket, 0, len(byKind))
	for kind, list := range byKind {
		b := TelemetryBucket{Kind: kind, Count: len(list)}
		durations := make([]int64, 0, len(list))
		for _, e := range list {
			if e.OK {
				b.OKCount++
			} else {
				b.FailCount++
			}
			if e.DurationMS > 0 {
				durations = append(durations, e.DurationMS)
			}
		}
		if len(durations) > 0 {
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			idx := int(math.Ceil(0.95*float64(len(durations)))) - 1
			if idx < 0 {
				idx = 0
			}
			b.P95MS = durations[idx]
		}
		if b.Count > 0 {
			b.SuccessPct = float64(b.OKCount) * 100 / float64(b.Count)
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// TelemetryForbiddenKeys 显式列出"永远不得进遥测"的键前缀，供守卫测试与代码审查引用。
var TelemetryForbiddenKeys = []string{
	"page", "html", "dom", "text", "content", "screenshot", "image",
	"url", "href", "selector", "cookie", "token", "password", "message",
	"stack", "body", "answer", "prompt",
}

// IsForbiddenTelemetryKey 键名命中禁止面即 true（大小写不敏感，前缀匹配）。
func IsForbiddenTelemetryKey(k string) bool {
	lk := strings.ToLower(k)
	for _, p := range TelemetryForbiddenKeys {
		if strings.HasPrefix(lk, p) {
			return true
		}
	}
	return false
}

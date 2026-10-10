// Package safeprompt 落地 K-1 决策：提示词越狱修复 + 敏感词扫描。
//
// 关键设计：
//   - 输入侧：拦截 "ignore previous" / "system prompt" / "DAN" 等已知越狱前缀，
//     命中即替换为脱敏说明，并在 metadata 标记 remediation_applied=true。
//   - 输出侧：扫描生成文本是否包含 PII（手机号/邮箱/身份证）、违规词、绕过指令。
//     命中即按策略返回 (RedactedText, Violation{Kind, Hit})。
//   - 内置敏感词词典来自 ./sensitive_default.go（可热更新由管理后台注入）。
//
// 该包不引入第三方依赖，纯字符串/Aho-Corasick 由调用方注入；默认实现用朴素子串扫描，
// 命中量级 < 1k 条时性能足够，> 1k 词建议替换为 AC 自动机。
package safeprompt

import (
	"regexp"
	"strings"
	"sync"
)

// ViolationKind 命中类型。
type ViolationKind string

const (
	KindJailbreakPrefix ViolationKind = "jailbreak_prefix"
	KindPII             ViolationKind = "pii"
	KindForbiddenWord   ViolationKind = "forbidden_word"
)

// Violation 单条命中。
type Violation struct {
	Kind     ViolationKind
	Hit      string
	Severity int
}

// RemediationResult 输入侧修复结果。
type RemediationResult struct {
	Text               string
	Original           string
	RemediationApplied bool
	Hits               []Violation
}

var (
	jailbreakPrefixes = []string{
		"ignore previous instructions",
		"ignore all previous",
		"disregard previous",
		"forget previous",
		"ignore the above",
		"system prompt",
		"you are dan",
		"jailbreak",
		"do anything now",
		"绕过",
		"忽略之前",
		"忽略以上",
		"无视之前的",
	}

	piiPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b1[3-9]\d{9}\b`),
		regexp.MustCompile(`\b\d{17}[\dXx]\b`),
		regexp.MustCompile(`[\w._%+-]+@[\w.-]+\.[A-Za-z]{2,}`),
		regexp.MustCompile(`\b\d{16,19}\b`),
	}

	mu        sync.RWMutex
	forbidden = map[string]int{}
)

// RegisterForbiddenWords 注入/覆盖敏感词词典。
func RegisterForbiddenWords(words map[string]int) {
	mu.Lock()
	defer mu.Unlock()
	for k, v := range words {
		if k == "" {
			continue
		}
		if v < 1 {
			v = 1
		}
		if v > 3 {
			v = 3
		}
		forbidden[strings.ToLower(k)] = v
	}
}

// AddForbidden 运行时追加敏感词。
func AddForbidden(word string, severity int) {
	mu.Lock()
	defer mu.Unlock()
	if severity < 1 {
		severity = 1
	}
	if severity > 3 {
		severity = 3
	}
	forbidden[strings.ToLower(word)] = severity
}

// Remediate 输入侧提示词修复。
func Remediate(prompt string) RemediationResult {
	if prompt == "" {
		return RemediationResult{Text: "", Original: ""}
	}
	lower := strings.ToLower(prompt)
	hits := make([]Violation, 0, 2)
	fixed := prompt
	for _, p := range jailbreakPrefixes {
		if strings.Contains(lower, p) {
			hits = append(hits, Violation{Kind: KindJailbreakPrefix, Hit: p, Severity: 3})

			fixed = maskOccurrences(fixed, p, "[sanitized: jailbreak attempt]")
		}
	}
	return RemediationResult{
		Text:               fixed,
		Original:           prompt,
		RemediationApplied: len(hits) > 0,
		Hits:               hits,
	}
}

// ScanOutput 输出侧扫描生成内容。
func ScanOutput(text string) []Violation {
	if text == "" {
		return nil
	}
	hits := make([]Violation, 0, 4)

	for _, re := range piiPatterns {
		if m := re.FindString(text); m != "" {
			hits = append(hits, Violation{Kind: KindPII, Hit: m, Severity: 2})
		}
	}

	lower := strings.ToLower(text)
	mu.RLock()
	for w, sev := range forbidden {
		if strings.Contains(lower, w) {
			hits = append(hits, Violation{Kind: KindForbiddenWord, Hit: w, Severity: sev})
		}
	}
	mu.RUnlock()
	return hits
}

// MaskPII 替换文本中的 PII 为占位符，便于日志/审计。
func MaskPII(text string) string {
	if text == "" {
		return text
	}
	out := text
	for _, re := range piiPatterns {
		out = re.ReplaceAllString(out, "[REDACTED:PII]")
	}
	return out
}

// HasCriticalViolation 判断命中是否达到关键级别（severity >= 3）。
func HasCriticalViolation(vs []Violation) bool {
	for _, v := range vs {
		if v.Severity >= 3 {
			return true
		}
	}
	return false
}

// MaxSeverity 返回最高严重等级。
func MaxSeverity(vs []Violation) int {
	m := 0
	for _, v := range vs {
		if v.Severity > m {
			m = v.Severity
		}
	}
	return m
}

func maskOccurrences(s, needle, replacement string) string {
	if needle == "" {
		return s
	}

	for {
		idx := strings.Index(strings.ToLower(s), needle)
		if idx < 0 {
			return s
		}
		s = s[:idx] + replacement + s[idx+len(needle):]
	}
}

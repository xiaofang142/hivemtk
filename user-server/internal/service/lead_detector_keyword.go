package service

import (
	"strings"

	"hivemtk-user/internal/model"
)

// LeadKeywordDetector 字面关键词检测（FR-012）。
// 大小写不敏感的子串匹配；命中消息 ID 作为证据，payload 保留命中的关键词与片段。
type LeadKeywordDetector struct{}

func NewLeadKeywordDetector() *LeadKeywordDetector { return &LeadKeywordDetector{} }

func (d *LeadKeywordDetector) Detect(in DetectionInput) DetectionHit {
	hit := DetectionHit{Type: "keyword", Complete: true, Payload: model.JSONPayload{}}
	if in.Rule == nil {
		hit.Complete = false
		hit.Reason = "规则为空，关键词证据不完整"
		return hit
	}
	keywords := normalizeLeadKeywords([]string(in.Rule.Keywords))
	if len(keywords) == 0 {
		hit.Reason = "未配置关键词"
		return hit
	}
	matched := make([]map[string]any, 0)
	evidence := make([]string, 0)
	seen := make(map[string]bool)
	for _, msg := range in.Messages {
		content := leadDetectionResolvedContent(in, msg)
		lower := strings.ToLower(content)
		for _, kw := range keywords {
			idx := strings.Index(lower, strings.ToLower(kw))
			if idx < 0 {
				continue
			}
			matched = append(matched, map[string]any{
				"keyword":    kw,
				"message_id": msg.MessageID,
				"snippet":    leadSnippet(content, idx, len(kw)),
			})
			if !seen[msg.MessageID] {
				seen[msg.MessageID] = true
				evidence = append(evidence, msg.MessageID)
			}
		}
	}
	hit.EvidenceMessageIDs = model.JSONStrings(evidence)
	hit.Payload["matched"] = matched
	hit.Payload["match_count"] = len(matched)
	return hit
}

func normalizeLeadKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool)
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func leadSnippet(content string, idx, length int) string {
	start := idx - 20
	if start < 0 {
		start = 0
	}
	end := idx + length + 20
	if end > len(content) {
		end = len(content)
	}
	if start >= end {
		return ""
	}
	return content[start:end]
}

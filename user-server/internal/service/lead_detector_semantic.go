package service

import (
	"strings"

	"hivemtk-user/internal/model"
)

// LeadSemanticDetector 语义关键词检测（FR-012）。
//
// 不另起向量/LLM 客户端：对配置的语义关键词做「同义/相关词扩展 + 双向包含匹配」，
// 命中即给出语义等价证据。与字面关键词并行执行，互补覆盖「我想找货源」这类
// 不出现字面关键词但语义明确的表述。内置词表是公开的同义词映射，不含业务密钥。
var leadSemanticSynonyms = map[string][]string{
	"需求": {"求购", "采购", "想买", "需要", "要一批", "寻找", "询价", "多少钱", "报价"},
	"采购": {"求购", "想买", "需要", "下单", "询价", "要货"},
	"供货": {"货源", "供应", "工厂", "厂家", "现货", "有货", "批发"},
	"货源": {"供货", "供应", "厂家", "现货", "批发"},
	"合作": {"代理", "加盟", "对接", "一起做", "渠道合作"},
	"资源": {"渠道", "上下游", "人脉", "客户资源"},
	"中介": {"介绍", "牵线", "对接", "佣金"},
	"价格": {"报价", "多少钱", "价位", "成本", "预算"},
}

// LeadSemanticDetector 语义关键词检测器。
type LeadSemanticDetector struct{}

func NewLeadSemanticDetector() *LeadSemanticDetector { return &LeadSemanticDetector{} }

func (d *LeadSemanticDetector) Detect(in DetectionInput) DetectionHit {
	hit := DetectionHit{Type: "semantic", Complete: true, Payload: model.JSONPayload{}}
	if in.Rule == nil {
		hit.Complete = false
		hit.Reason = "规则为空，语义关键词证据不完整"
		return hit
	}
	keywords := normalizeLeadKeywords([]string(in.Rule.SemanticKeywords))
	if len(keywords) == 0 {
		hit.Reason = "未配置语义关键词"
		return hit
	}
	matched := make([]map[string]any, 0)
	evidence := make([]string, 0)
	seen := make(map[string]bool)
	for _, msg := range in.Messages {
		lower := strings.ToLower(msg.Content)
		for _, kw := range keywords {
			terms := leadSemanticTerms(kw)
			found := ""
			for _, term := range terms {
				if strings.Contains(lower, strings.ToLower(term)) {
					found = term
					break
				}
			}
			if found == "" {
				continue
			}
			matched = append(matched, map[string]any{
				"keyword":    kw,
				"term":       found,
				"message_id": msg.MessageID,
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

// leadSemanticTerms 返回关键词自身 + 同义扩展词（去重）。
func leadSemanticTerms(keyword string) []string {
	kw := strings.TrimSpace(keyword)
	out := []string{kw}
	if extras, ok := leadSemanticSynonyms[kw]; ok {
		out = append(out, extras...)
	}
	// 反向扩展：同义词表里某个词的近义词列表包含本关键词时，取该组。
	for k, extras := range leadSemanticSynonyms {
		if k == kw {
			continue
		}
		for _, e := range extras {
			if e == kw {
				out = append(out, k)
				for _, other := range extras {
					out = append(out, other)
				}
				break
			}
		}
	}
	seen := make(map[string]bool, len(out))
	uniq := make([]string, 0, len(out))
	for _, t := range out {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		uniq = append(uniq, t)
	}
	return uniq
}

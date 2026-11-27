package service

import (
	"strings"

	"hivemtk-user/internal/model"
)

// LM-08：供需意图 / 相对产业链关系 / 经营身份三轴分离。
// 每轴保留多值与 unknown，不把角色简化成一个显示名；结论附说话人/时间/消息证据。
type LeadAxisEvidence struct {
	MessageID string `json:"message_id"`
	Speaker   string `json:"speaker"`
	At        int64  `json:"at"`
	Axis      string `json:"axis"`
	Value     string `json:"value"`
}

type LeadSemanticAxes struct {
	Intent   map[string][]string           `json:"intent"`   // demand/supply/broker/unknown
	Chain    map[string][]string           `json:"chain"`    // upstream/downstream/peer/unknown
	Identity map[string][]string           `json:"identity"` // direct/broker/unknown
	Evidence map[string][]LeadAxisEvidence `json:"evidence"`
}

// ClassifyLeadSemanticAxes 对消息逐条做有界分类；否定/引用/转发不生成真实意图证据。
func ClassifyLeadSemanticAxes(msgs []DetectionMessage) LeadSemanticAxes {
	out := LeadSemanticAxes{
		Intent: make(map[string][]string), Chain: make(map[string][]string),
		Identity: make(map[string][]string), Evidence: make(map[string][]LeadAxisEvidence),
	}
	add := func(m DetectionMessage, axis, value string) {
		out.IntentValue(axis, value)
		out.Evidence[m.MessageID] = append(out.Evidence[m.MessageID], LeadAxisEvidence{
			MessageID: m.MessageID, Speaker: m.SenderID, Axis: axis, Value: value,
		})
	}
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if strings.Contains(content, "不需要") || strings.Contains(content, "已买到") ||
			strings.Contains(content, "转述") || strings.Contains(content, "转发") ||
			strings.Contains(content, "引用") || strings.Contains(content, "‘") {
			continue
		}
		intent := ""
		switch {
		case strings.Contains(content, "撮合") || strings.Contains(content, "佣金") || strings.Contains(content, "牵线"):
			intent = "broker"
		case strings.Contains(content, "我有货") || strings.Contains(content, "现货") || strings.Contains(content, "厂价直销"):
			intent = "supply"
		case strings.Contains(content, "需要货") || strings.Contains(content, "求购") || strings.Contains(content, "找原料"):
			intent = "demand"
		}
		chain := ""
		switch {
		case strings.Contains(content, "上下游") || strings.Contains(content, "撮合"):
			chain = "broker"
		case strings.Contains(content, "原料"):
			chain = "upstream"
		case strings.Contains(content, "成品"):
			chain = "downstream"
		}
		identity := ""
		switch {
		case strings.Contains(content, "佣金") || strings.Contains(content, "牵线") || strings.Contains(content, "中间商"):
			identity = "broker"
		case strings.Contains(content, "直销") || strings.Contains(content, "工厂") || strings.Contains(content, "厂价"):
			identity = "direct"
		}
		if intent == "" && chain == "" && identity == "" {
			add(m, "intent", "unknown")
			continue
		}
		if intent != "" {
			add(m, "intent", intent)
		}
		if chain != "" {
			add(m, "chain", chain)
		}
		if identity != "" {
			add(m, "identity", identity)
		}
	}
	return out
}

// IntentValue 把值追加到对应轴集合（去重，保持首次出现顺序）。
func (a *LeadSemanticAxes) IntentValue(axis, value string) {
	var target *map[string][]string
	switch axis {
	case "intent":
		target = &a.Intent
	case "chain":
		target = &a.Chain
	case "identity":
		target = &a.Identity
	default:
		return
	}
	if value == "" {
		value = "unknown"
	}
	if len((*target)[value]) > 0 {
		return
	}
	(*target)[value] = append((*target)[value], value)
}

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
		lower := strings.ToLower(leadDetectionResolvedContent(in, msg))
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
	// LM-08：三轴结论随语义证据一起输出，不改变既有 matched/count 契约。
	hit.Payload["semantic_axes"] = ClassifyLeadSemanticAxes(in.Messages)
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
				out = append(out, extras...)
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

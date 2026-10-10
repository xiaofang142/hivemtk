package confidence

import (
	"hivemtk-user/internal/dto"
)

// VetoRule 一票否决规则接口
type VetoRule interface {
	Check(signals *dto.FiveSignals, ctx *VetoContext) (triggered bool, reason string)
}

// VetoContext 否决上下文
type VetoContext struct {
	IntentType        string
	CustomerMessage   string
	LastNTurns        []string
	ExpectedEntities  map[string]any
	ExtractedEntities map[string]any
}

// VetoComplaint 投诉/流失意图直接否决
type VetoComplaint struct{}

// Check 实现 VetoRule
func (r *VetoComplaint) Check(_ *dto.FiveSignals, ctx *VetoContext) (bool, string) {
	switch ctx.IntentType {
	case "complaint", "churn":
		return true, "veto_complaint"
	}
	return false, ""
}

// VetoLowEntity 实体完整性过低
type VetoLowEntity struct {
	Threshold float64
}

// Check 实现 VetoRule
//
// 仅当 expected 非空（即 intent 需要实体）时才检查
func (r *VetoLowEntity) Check(signals *dto.FiveSignals, ctx *VetoContext) (bool, string) {
	if len(ctx.ExpectedEntities) == 0 {
		return false, ""
	}
	threshold := r.Threshold
	if threshold <= 0 {
		threshold = 0.2
	}
	if signals.EntityComp < threshold {
		return true, "veto_low_entity"
	}
	return false, ""
}

// VetoLowRAG RAG 覆盖过低
//
// 默认阈值 0.1：RAGQual < 0.1 视为知识库无覆盖，必须转人工/兜底。
// 业务规则：当 Threshold=0 时自动用默认值 0.1（而非禁用）。
//
//	禁用本规则请用负数（如 Threshold=-1）显式声明。
type VetoLowRAG struct {
	Threshold float64
}

// DefaultVetoLowRAGThreshold 否决低 RAG 的编译期兜底阈值（RAGQual 低于它视为知识库无覆盖）
const DefaultVetoLowRAGThreshold = 0.1

// vetoLowRAGThresholdProvider 由装配层（internal/app/confidence_params_wiring.go）注入，
// 数据源是 config_params 的 confidence.veto_low_rag_threshold。
var vetoLowRAGThresholdProvider = func() float64 { return DefaultVetoLowRAGThreshold }

// SetVetoLowRAGThresholdProvider 注入否决阈值读取口；传 nil 视为不注入。
func SetVetoLowRAGThresholdProvider(fn func() float64) {
	if fn != nil {
		vetoLowRAGThresholdProvider = fn
	}
}

// vetoLowRAGThreshold 当前生效的否决阈值。
// 非 (0,1] 的值一律回落兜底：取到 0 会让任何 RAG 得分都触发否决（含满分），
// 一票否决规则会变成「全部转人工」。
func vetoLowRAGThreshold() float64 {
	if t := vetoLowRAGThresholdProvider(); t > 0 && t <= 1 {
		return t
	}
	return DefaultVetoLowRAGThreshold
}

// ProbeVetoLowRAGThreshold 导出当前生效的否决阈值（仅供装配层测试断言读取口）。
func ProbeVetoLowRAGThreshold() float64 { return vetoLowRAGThreshold() }

// Check 实现 VetoRule
//
// 仅当 intent 标记为需要 RAG 时才检查（这里简化为：所有 intent 都检查）
func (r *VetoLowRAG) Check(signals *dto.FiveSignals, _ *VetoContext) (bool, string) {
	threshold := r.Threshold
	if threshold == 0 {
		threshold = vetoLowRAGThreshold()
	}
	if threshold < 0 {
		return false, ""
	}
	if signals.RAGQual < threshold {
		return true, "veto_low_rag"
	}
	return false, ""
}

// VetoHighEntropy LLM 熵过低（高不确定）
//
// 注意：LLMEntropy 信号定义为 1 - normalized_entropy
// 所以"高不确定"对应 LLMEntropy < threshold
type VetoHighEntropy struct {
	Threshold float64
}

// Check 实现 VetoRule
func (r *VetoHighEntropy) Check(signals *dto.FiveSignals, _ *VetoContext) (bool, string) {
	threshold := r.Threshold
	if threshold <= 0 {
		threshold = 0.2
	}
	if signals.LLMEntropy < threshold {
		return true, "veto_high_entropy"
	}
	return false, ""
}

// VetoLoop 同一问题连续 3 轮
//
// 检测规则：最近 3 条用户消息完全相同
// LastNTurns 长度 < 6（即 < 3 轮 user+ai 交替）时返回 false
type VetoLoop struct{}

// Check 实现 VetoRule
func (r *VetoLoop) Check(_ *dto.FiveSignals, ctx *VetoContext) (bool, string) {
	if len(ctx.LastNTurns) < 6 {
		return false, ""
	}
	last3 := ctx.LastNTurns[len(ctx.LastNTurns)-3:]
	if last3[0] == last3[1] && last3[1] == last3[2] && last3[0] != "" {
		return true, "veto_loop"
	}
	return false, ""
}

// VetoExplicit 客户显式请求转人工
type VetoExplicit struct{}

var explicitKeywordMatcher func(string) bool

// SetExplicitKeywordMatcher 注入显式转人工关键词匹配函数（S-5 运行时单一来源）。
// 由 service 包 init 调用；fn 为 nil 时忽略。
func SetExplicitKeywordMatcher(fn func(string) bool) {
	if fn != nil {
		explicitKeywordMatcher = fn
	}
}

// Check 实现 VetoRule
func (r *VetoExplicit) Check(_ *dto.FiveSignals, ctx *VetoContext) (bool, string) {
	if ctx.CustomerMessage == "" {
		return false, ""
	}
	if explicitKeywordMatcher != nil && explicitKeywordMatcher(ctx.CustomerMessage) {
		return true, "veto_explicit"
	}
	return false, ""
}

// VetoChain 一票否决链（按顺序检查，返回第一个触发的规则）
type VetoChain struct {
	rules []VetoRule
}

// NewVetoChain 默认否决链
//
// 顺序：
//  1. VetoExplicit   （客户意愿最优先）
//  2. VetoComplaint  （投诉必须人工）
//  3. VetoLoop       （循环检测）
//  4. VetoLowEntity  （实体缺失）
//  5. VetoLowRAG     （知识库无覆盖）
//  6. VetoHighEntropy（LLM 不确定）
func NewVetoChain() *VetoChain {
	return &VetoChain{
		rules: []VetoRule{
			&VetoExplicit{},
			&VetoComplaint{},
			&VetoLoop{},
			&VetoLowEntity{Threshold: 0.2},
			&VetoLowRAG{Threshold: 0},
			&VetoHighEntropy{Threshold: 0.2},
		},
	}
}

// NewVetoChainWithRules 自定义规则的否决链
func NewVetoChainWithRules(rules []VetoRule) *VetoChain {
	return &VetoChain{rules: rules}
}

// Check 顺序检查所有规则，返回第一个触发的规则
func (c *VetoChain) Check(signals *dto.FiveSignals, ctx *VetoContext) (bool, string) {
	for _, r := range c.rules {
		if triggered, reason := r.Check(signals, ctx); triggered {
			return true, reason
		}
	}
	return false, ""
}

// Rules 返回规则列表（只读）
func (c *VetoChain) Rules() []VetoRule {
	return c.rules
}

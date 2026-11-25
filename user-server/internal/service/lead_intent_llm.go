package service

// lead_intent_llm.go 保留线索 LLM 参数的默认值、装配注入接口与验证探针。
// 原关键词评分后的复核实现没有生产调用，已随旧矿工链路清理。

// DefaultLeadLLMRefineEnabled / DefaultLeadIndustryProfile 是参数中心不可用时的兜底。
// 两处都必须与 config_param_seeds.go 里 lead 组的 DefaultValue 逐字一致：
// scripts/check-config-param-defaults.py 会比对这两个常量与种子行，不一致即判红。
const (
	DefaultLeadLLMRefineEnabled = true
	DefaultLeadIndustryProfile  = "通用零售/服务咨询：客户咨询产品或服务的价格、功能与购买流程"
)

// LLM 复核开关与行业画像都走参数中心（ConfigParam），这里用 seam 注入，
// 避免本包直接依赖装配层。初始值取上面的常量，这样「没接参数中心」和
// 「参数中心还没读到值」两种情况下，行为都和种子默认一致。
var (
	leadLLMRefineEnabledProvider = func() bool { return DefaultLeadLLMRefineEnabled }
	leadIndustryProfileProvider  = func() string { return DefaultLeadIndustryProfile }
)

// SetLeadLLMRefineProviders 由装配层注入参数读取口（nil 忽略）。
func SetLeadLLMRefineProviders(enabled func() bool, profile func() string) {
	if enabled != nil {
		leadLLMRefineEnabledProvider = enabled
	}
	if profile != nil {
		leadIndustryProfileProvider = profile
	}
}

// ProbeLeadLLMRefineEnabled 暴露开关读取口当前值，供装配层测试断言接线确实生效
// （与 ProbeUnifiedMinerOpportunityThreshold 同用途）。
func ProbeLeadLLMRefineEnabled() bool { return leadLLMRefineEnabledProvider() }

// ProbeLeadIndustryProfile 暴露行业画像读取口当前值。
func ProbeLeadIndustryProfile() string { return leadIndustryProfileProvider() }

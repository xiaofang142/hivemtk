package model

import "time"

// SecurityRules 安全规则配置（敏感词过滤 / 输出内容护栏 / 数据脱敏规则）。
// 持久化到 system_config_kv（key = security.rules），保存时热更新 safeprompt 词典。
type SecurityRules struct {
	// SensitiveWords 敏感词 → 严重级别（数字越大越严重）。
	SensitiveWords map[string]int `json:"sensitive_words"`
	// OutputGuardEnabled 是否启用输出内容护栏（越狱/敏感词拦截）。
	OutputGuardEnabled bool `json:"output_guard_enabled"`
	// PIIMaskEnabled 是否启用数据脱敏（手机号/邮箱/身份证/银行卡/令牌）。
	PIIMaskEnabled bool `json:"pii_mask_enabled"`
	// UpdatedAt 最近一次写入时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// SecurityRulesConfigKey system_config_kv 中的存储键。
const SecurityRulesConfigKey = "security.rules"

package model

import "time"

// LLMProvider 持久化 LLM 模型/厂商定义。
//
// 此前 provider 定义只存在于内存（config 构建 + 后台「LLM路由」页面 AddProvider），
// 容器重启即丢失。改造后：后台页面 / 脚本 / config 引导注册的 provider 均落库到此表；
// user-server 启动时由 dispatcher.LoadProvidersFromDB 加载回内存路由表，
// 实现「可视化/脚本添加 → 落库 → 容器重启不丢」的闭环。
//
// Name 为全局唯一标识，与 dispatcher.ProviderConfig.Name 一一对应。
type LLMProvider struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           uint    `gorm:"primaryKey" json:"id"`
	Name         string  `gorm:"column:name;uniqueIndex;size:128" json:"name"`
	DisplayName  string  `gorm:"column:display_name;size:255" json:"display_name"`
	BaseURL      string  `gorm:"column:base_url;size:512" json:"base_url"`
	Model        string  `gorm:"column:model;size:128" json:"model"`
	APIKey       string  `gorm:"column:api_key;type:text" json:"-"`
	APIType      string  `gorm:"column:api_type;size:32;default:'openai'" json:"api_type"`
	Enabled      bool    `gorm:"column:enabled;default:false" json:"enabled"`
	QualityScore float64 `gorm:"column:quality_score;default:0.8" json:"quality_score"`
	MaxRPM       int     `gorm:"column:max_rpm;default:60" json:"max_rpm"`
	MaxTPM       int     `gorm:"column:max_tpm;default:0" json:"max_tpm"`
	// CostPer1k 单一单价（元/1k token），2026-10 之前的口径。保留作回落：
	// 只填了它而没填下面两列的 provider，两部分按同价计算。
	CostPer1k float64 `gorm:"column:cost_per_1k;default:0" json:"cost_per_1k"`
	// CostPer1kInput / CostPer1kOutput 输入输出分列单价。
	// 需求原文：「输入和输出分别计价以及扣费」—— 只有这两个列并存且不相等时，
	// 成本口径才是真正的「分别计价」（见 ProviderConfig.HasSeparatePricing）。
	CostPer1kInput  float64 `gorm:"column:cost_per_1k_input;default:0" json:"cost_per_1k_input"`
	CostPer1kOutput float64 `gorm:"column:cost_per_1k_output;default:0" json:"cost_per_1k_output"`
	// Source self=商户自带 Key（不计费）/ platform=平台垫资（走平台代理 + 计量扣费）
	//
	// 判空回落 self 而不是 platform：漏填的绝大多数是用户自己加的 provider，
	// 落成 platform 会凭空记一笔欠平台的账。
	Source   string `gorm:"column:source;not null;default:'self';index" json:"source"`
	PlanCode string `gorm:"column:plan_code;type:varchar(32)" json:"plan_code"`
	// InputPricePer1kCents / OutputPricePer1kCents 平台套餐单价快照（分/1k token）。
	//
	// 来源唯一：sync 时 Providers()×Plans() 按 plan_code 关联（平台下发的
	// provider 视图不带价，套餐接口带）。join 不上（套餐下架/不可见）= 恒 0 =
	// 调用侧如实降级 cost 0，不编价。self provider 恒 0（自带 Key 不按平台价计）。
	//
	// **写入时点按当次快照落列，读侧不重算不回填** —— 平台改价后历史明细不漂移
	// （对齐平台 RecordUsage 的写时计算口径）。落库写权限只归平台 sync 一条链路
	// （repository.UpsertProviders），admin 后台的 provider 保存路径不得触碰。
	InputPricePer1kCents  int64     `gorm:"column:input_price_per_1k_cents;not null;default:0" json:"input_price_per_1k_cents"`
	OutputPricePer1kCents int64     `gorm:"column:output_price_per_1k_cents;not null;default:0" json:"output_price_per_1k_cents"`
	AvgLatencyMs          int       `gorm:"column:avg_latency_ms;default:0" json:"avg_latency_ms"`
	NoFC                  bool      `gorm:"column:no_fc;default:false" json:"no_fc"`
	Vendor                string    `gorm:"column:vendor;size:64" json:"vendor"`
	Tags                  string    `gorm:"column:tags;type:text" json:"-"`
	SortOrder             int       `gorm:"column:sort_order;default:0" json:"sort_order"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// TableName 指定表名
func (LLMProvider) TableName() string { return "llm_providers" }

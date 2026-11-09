// Package model 提供 LLM 路由日志（LLMRoutingLog）的 GORM 实体定义。
//
// v3.6.0 / v3.7.0：基础字段 + 扩展字段（model_type / vendor / base_url / cost_split / token_source 等）
// v3.12.0：多语言扩展字段（internal_lang / target_lang / cross_lingual / glossary_version /
//
//	cache_hit / quality_score / validation_issues）
//
// 表结构由 migrations/llm_routing_logs_migration.go 与 llm_routing_logs_extend_migration.go
// 通过原生 SQL 创建；本结构体作为 Go 层映射，供 Service / Repository 使用。
//
// 私域独立部署：无 merchant_id
// 五层架构：仅定义数据结构，业务逻辑在 Service 层
package model

import (
	"time"
)

// LLMRoutingLog LLM 路由调用日志（每次 Dispatch 落一条）
//
// 字段说明：
//   - 基础计量：prompt_tokens / completion_tokens / total_tokens / cost / latency_ms
//   - 路由元信息：scenario / provider / model / trace_id
//   - 厂商归集：model_type(local/cloud) / vendor / base_url
//   - 降级审计：is_fallback / source(dispatch/cache/fallback/null)
//   - 成本拆分：prompt_cost / completion_cost
//   - Token 来源：token_source(actual/estimated/missing) / estimator
//   - 聚合键：scenario_provider
//   - 多语言扩展：internal_lang / target_lang / cross_lingual / glossary_version /
//     cache_hit / quality_score / validation_issues
type LLMRoutingLog struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID          string  `gorm:"type:varchar(64);column:trace_id" json:"trace_id"`
	Scenario         string  `gorm:"type:varchar(64);column:scenario;not null" json:"scenario"`
	Provider         string  `gorm:"type:varchar(64);column:provider;not null" json:"provider"`
	Model            string  `gorm:"type:varchar(128);column:model" json:"model"`
	PromptTokens     int     `gorm:"column:prompt_tokens;not null;default:0" json:"prompt_tokens"`
	CompletionTokens int     `gorm:"column:completion_tokens;not null;default:0" json:"completion_tokens"`
	TotalTokens      int     `gorm:"column:total_tokens;not null;default:0" json:"total_tokens"`
	Cost             float64 `gorm:"type:numeric(14,6);column:cost;not null;default:0" json:"cost"`
	LatencyMs        int     `gorm:"column:latency_ms;not null;default:0" json:"latency_ms"`
	Success          bool    `gorm:"column:success;not null;default:true" json:"success"`
	ErrorMsg         string  `gorm:"type:text;column:error_msg" json:"error_msg"`
	// ErrorClass 错误分类（timeout/network/auth/rate_limit/upstream/unknown）。
	// 历史上无写入方（报表读取侧用 errorClassExpr 按 error_msg 回推），但
	// llm_usage_report 的聚合/明细 SQL 引用了该列——模型缺字段时 AutoMigrate
	// 不会建列，全新 AutoMigrate 库上 LLM 用量报表 500（2026-11-10 回归实测）。
	// 先补列对齐读取侧契约；写入侧标注留后续（表达式对空值回推分类）。
	ErrorClass       string  `gorm:"type:varchar(32);not null;default:''" json:"error_class"`
	FromCache        bool    `gorm:"column:from_cache;not null;default:false" json:"from_cache"`

	ModelType        string  `gorm:"type:varchar(16);column:model_type;not null;default:'local'" json:"model_type"`
	Vendor           string  `gorm:"type:varchar(64);column:vendor;not null;default:'unknown'" json:"vendor"`
	BaseURL          string  `gorm:"type:varchar(512);column:base_url;not null;default:''" json:"base_url"`
	IsFallback       bool    `gorm:"column:is_fallback;not null;default:false" json:"is_fallback"`
	PromptCost       float64 `gorm:"type:numeric(14,6);column:prompt_cost;not null;default:0" json:"prompt_cost"`
	CompletionCost   float64 `gorm:"type:numeric(14,6);column:completion_cost;not null;default:0" json:"completion_cost"`
	TokenSource      string  `gorm:"type:varchar(16);column:token_source;not null;default:'missing'" json:"token_source"`
	Estimator        string  `gorm:"type:varchar(32);column:estimator;not null;default:''" json:"estimator"`
	Source           string  `gorm:"type:varchar(32);column:source;not null;default:'dispatch'" json:"source"`
	ScenarioProvider string  `gorm:"type:varchar(160);column:scenario_provider;not null;default:''" json:"scenario_provider"`

	InternalLang     string  `gorm:"type:varchar(8);column:internal_lang" json:"internal_lang"`
	TargetLang       string  `gorm:"type:varchar(8);column:target_lang" json:"target_lang"`
	CrossLingual     bool    `gorm:"column:cross_lingual;default:false" json:"cross_lingual"`
	GlossaryVersion  string  `gorm:"type:varchar(32);column:glossary_version" json:"glossary_version"`
	CacheHit         bool    `gorm:"column:cache_hit;default:false" json:"cache_hit"`
	QualityScore     float64 `gorm:"type:decimal(4,3);column:quality_score" json:"quality_score"`
	ValidationIssues JSONMap `gorm:"type:jsonb;column:validation_issues" json:"validation_issues"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名
func (LLMRoutingLog) TableName() string { return "llm_routing_logs" }

// LLMRoutingAudit 路由变更审计（scenario 路由升级/灰度/回滚留痕）。
//
// 历史上只有原始 SQL 迁移建表（llm_routing_logs_migration.go），无 Go 模型 ⇒
// AutoMigrate 主导的库上该表不存在，/api/llm/audit 稳定 500（2026-11-10 回归实测）。
// 按「AutoMigrate 单一事实源」补模型进 allModels；列定义与迁移 SQL 逐列对齐
// （含 tenant_id：审计行归属租户，on 模式行自证）。
type LLMRoutingAudit struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Scenario     string    `gorm:"type:varchar(64);column:scenario;not null" json:"scenario"`
	Version      int       `gorm:"column:version;not null;default:1" json:"version"`
	PrevProvider string    `gorm:"type:varchar(64);column:prev_provider" json:"prev_provider"`
	NewProvider  string    `gorm:"type:varchar(64);column:new_provider;not null" json:"new_provider"`
	PrevFallback string    `gorm:"type:text;column:prev_fallbacks" json:"prev_fallbacks"`
	NewFallback  string    `gorm:"type:text;column:new_fallbacks" json:"new_fallbacks"`
	Action       string    `gorm:"type:varchar(32);column:action;not null" json:"action"`
	Operator     string    `gorm:"type:varchar(64);column:operator" json:"operator"`
	TraceID      string    `gorm:"type:varchar(64);column:trace_id" json:"trace_id"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名
func (LLMRoutingAudit) TableName() string { return "llm_routing_audit" }

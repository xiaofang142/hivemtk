package model

import (
	"time"
)

// IntentRouteDaily 意图路由日聚合（TB-11 观测面数据面）。
//
// 一句话职责：把「意图识别→链内工具命中/跳过」路由行为按
// 租户×日期×意图×链×工具 聚成计数行，供管理端观测页
// （意图分布/链命中率/工具成功率三图 + 北极星三指标）消费。
//
// 冻结契约（TB-11，2026-11-10）：
//   - 复合 PK = tenant_id/date/intent_type/chain_name/tool_name，
//     无 ID/DeletedAt（聚合表按日覆盖写，软删无意义）；
//   - date = 应用层日切 yyyy-mm-dd（varchar(10)），按写入时刻本地日；
//   - 链级行约定：chain_name = 绑定链 IntentCode 且 tool_name 为空串；
//     chain_name 与 tool_name 均为空串的组合不作为链级行使用；
//   - tool_name = '__correction__' 为纠错事件行（TB-09 CorrectIntent
//     写侧：intent_type = 原错误意图，tool_calls + 1）；
//   - 计数语义：chain_hits/chain_misses = 链级命中/跳过；
//     tool_hits/tool_skips = 工具级命中/跳过；tool_calls = 工具调用总数；
//     tool_success/tool_fail = 工具调用成功/失败数；
//   - UpsertDiff 走 ON CONFLICT 增量累加（先例 msg_hourly_summary），
//     写侧显式 stamps TenantID，读侧不拼 tenant_id 由租户 callback 盖章。
//
// 北极星口径（消费方遵守，2026-11-10 冻结）：
//   - 转人工率 = handoff_at 非空会话 / 窗口会话（customer_sessions）；
//   - 解决率 = closed_at/resolved_at 有值且 handoff_at 空 / 窗口会话；
//   - 意图准确率（纠错采样口径）= 1 − 窗口纠错数 / 窗口 intent_records 识别量。
type IntentRouteDaily struct {
	TenantID uint `gorm:"not null;default:1;primaryKey" json:"tenant_id"`
	// Date 应用层日切 yyyy-mm-dd。
	Date string `gorm:"type:varchar(10);not null;primaryKey" json:"date"`
	// IntentType 意图类型（agent runtime Intent 词表值 / 家族常量）。
	IntentType string `gorm:"type:varchar(50);not null;primaryKey" json:"intent_type"`
	// ChainName 链名（绑定链 IntentCode；链级行 tool_name 为空串）。
	ChainName string `gorm:"type:varchar(50);not null;default:'';primaryKey" json:"chain_name"`
	// ToolName 工具名（'__correction__' = 纠错事件行）。
	ToolName string `gorm:"type:varchar(100);not null;default:'';primaryKey" json:"tool_name"`

	ChainHits   int64 `gorm:"not null;default:0" json:"chain_hits"`
	ChainMisses int64 `gorm:"not null;default:0" json:"chain_misses"`
	ToolHits    int64 `gorm:"not null;default:0" json:"tool_hits"`
	ToolSkips   int64 `gorm:"not null;default:0" json:"tool_skips"`
	ToolCalls   int64 `gorm:"not null;default:0" json:"tool_calls"`
	ToolSuccess int64 `gorm:"not null;default:0" json:"tool_success"`
	ToolFail    int64 `gorm:"not null;default:0" json:"tool_fail"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (IntentRouteDaily) TableName() string { return "intent_route_daily" }

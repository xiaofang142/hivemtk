package model

import "time"

// ChannelRouteRule 入站事件路由规则（specs/024 T301）。
//
// 解决的问题：今天「入站即触发 AI」写在代码里（webhook_ai.go 的 shouldTriggerAI），
// 想改成「某类事件只归档」「某账号交给别的管线」只能改代码发版。
// 这张表把「什么事件 + 什么条件 → 谁处理」变成数据，且**无规则时行为与今天完全一致**（引擎无命中即放行）。
//
// 语义：
//   - Channel/AccountID 空 = 不限（全渠道/全账号）；
//   - EventType 支持通配：`*`（全部）、`message.*`（前缀）、精确值；
//   - Match 是 JSON 对象，键支持 `extra.<k>` 嵌套取值，值支持标量与 {"$in":[...]}；
//   - Priority 升序，**首个命中即生效**（不叠加，避免规则组合爆炸）；
//   - Target=discard 表示「这条事件不落库、不回复，但必须留审计」。
type ChannelRouteRule struct {
	TenantID  uint   `gorm:"column:tenant_id;not null;default:1;index:idx_channel_route_lookup,priority:1" json:"tenant_id"`
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel   string `gorm:"column:channel;type:varchar(32);not null;default:'';index:idx_channel_route_lookup,priority:2" json:"channel"`
	AccountID string `gorm:"column:account_id;type:varchar(64);not null;default:'';index:idx_channel_route_lookup,priority:3" json:"account_id"`
	// EventType 事件类型（model.EventType* 词表或通配）。空串按 `*` 处理，兼容只填渠道的旧式规则。
	EventType string `gorm:"column:event_type;type:varchar(64);not null;default:''" json:"event_type"`
	// Match 条件 JSON（文本列而非 jsonb：规则量小、只在读时解析，jsonb 的写入校验会挡住
	// 「先存后改」的编辑流程，而非法 JSON 在引擎侧本来就被跳过并计数）。
	Match     string    `gorm:"column:match;type:text;not null;default:''" json:"match"`
	Target    string    `gorm:"column:target;type:varchar(32);not null;default:'pipeline'" json:"target"`
	TargetRef string    `gorm:"column:target_ref;type:varchar(128);not null;default:''" json:"target_ref"`
	Priority  int       `gorm:"column:priority;not null;default:100;index:idx_channel_route_lookup,priority:4" json:"priority"`
	Enabled   bool      `gorm:"column:enabled;not null;default:true;index:idx_channel_route_lookup,priority:5" json:"enabled"`
	Note      string    `gorm:"column:note;type:varchar(255);not null;default:''" json:"note"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// 路由目标词表（T303/T304 共用；写错目标名会让规则静默失效，故集中定义 + 启动期校验）。
const (
	ChannelRouteTargetDiscard  = "discard"
	ChannelRouteTargetPipeline = "pipeline"
	ChannelRouteTargetAgent    = "agent"
	ChannelRouteTargetPlugin   = "plugin"
)

// IsKnownChannelRouteTarget 判定目标词是否在册。
func IsKnownChannelRouteTarget(t string) bool {
	switch t {
	case ChannelRouteTargetDiscard, ChannelRouteTargetPipeline, ChannelRouteTargetAgent, ChannelRouteTargetPlugin:
		return true
	}
	return false
}

func (ChannelRouteRule) TableName() string { return "channel_route_rules" }

package model

import (
	"time"

	"gorm.io/gorm"
)

// IntentToolChain 意图→工具链绑定（TB-01 绑定层数据面）。
//
// 一句话职责：把「识别到某意图后规划器该调哪些工具」从代码硬编码
// （agent/runtime/planner_stage.go 的 case 分支）变成租户级数据配置，
// planner 未命中绑定时回落既有硬编码，行为兼容。
//
// 冻结契约（TB-01，2026-11-09）：
//   - intent_code 取值 = agent runtime Intent 词表（service 层写侧校验，编译期
//     引用 agent_runtime 常量，漂移即编译失败）；
//   - steps_json = []IntentChainStepConfig 的 JSON 数组文本，1~8 步；
//   - args 值仅允许 string/number/bool，字符串里的 {{var}} 变量白名单在
//     agent/runtime 渲染侧执行（写侧只做预检，白名单外变量写侧即拒）；
//   - 同租户同 intent_code 唯一（软删行不占用唯一索引：唯一性由 service 写侧
//     显式查重保证，不用 DB 部分唯一索引，保持与仓内软删表一致的做法）。
type IntentToolChain struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"type:varchar(100);not null;default:''" json:"name"`
	// IntentCode 绑定的意图（agent runtime Intent 词表值，如 order_status/after_sales）。
	IntentCode string `gorm:"type:varchar(50);not null;index" json:"intent_code"`
	// StepsJSON 步骤数组 JSON 文本：[{"tool_name":"order.lookup","args":{"customer_id":"{{customer_id}}"},"priority":1,"enabled":true}]
	StepsJSON string `gorm:"type:text;not null" json:"steps_json"`
	// ReplyHint 绑定层自定义回复提示（空 = planner 用默认提示兜底）。
	ReplyHint string `gorm:"type:text" json:"reply_hint"`
	Enabled   bool   `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (IntentToolChain) TableName() string { return "intent_tool_chains" }

// IntentChainStepConfig steps_json 数组元素（纯数据结构，无外部依赖）。
// 与 agent/runtime/planner_chain.go 的 IntentChainStep 一一对应（service 层映射）。
type IntentChainStepConfig struct {
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args,omitempty"`
	Priority int            `json:"priority"`
	Enabled  bool           `json:"enabled"`
}

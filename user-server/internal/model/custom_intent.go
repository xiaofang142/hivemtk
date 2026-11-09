package model

import (
	"time"

	"gorm.io/gorm"
)

// CustomIntent 自定义意图词条（T-09 意图目录数据化）。
//
// 冻结契约（2026-11-10）：
//   - type 取值与 DefaultIntents 或已存在词条冲突 → service 写侧 4009 拒绝
//     （默认词典不可被覆盖，避免隐式行为变化）；
//   - 消费点：词面规则匹配（intentsForRule 合并）+ LLM 候选清单 + 词典展示；
//     embedding 锚点 v1 不合并（自定义意图走规则+LLM 两级，不动 IntentExample
//     落库/预计算通路——边界显式声明，不是遗漏）；
//   - 启停：enabled=false 不进合并结果，但占位（同意图不可重建）；
//   - 挂载：enabled 的自定义意图 type 进入意图→工具调用流绑定校验放行面
//     （IntentToolChainService 经动态词表依赖放行）。
type CustomIntent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Type string `gorm:"type:varchar(50);not null;index" json:"type"`
	Name string `gorm:"type:varchar(100);not null" json:"name"`
	// KeywordsJSON 命中词面（JSON 字符串数组；词面匹配沿 intentsForRule 既有计分语义）
	KeywordsJSON string `gorm:"type:text;not null" json:"keywords_json"`
	// ExamplesJSON 示例问句（JSON 字符串数组；v1 不进 embedding 锚点）
	ExamplesJSON string `gorm:"type:text" json:"examples_json"`
	Description  string `gorm:"type:varchar(500)" json:"description"`
	Enabled      bool   `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (CustomIntent) TableName() string { return "custom_intents" }

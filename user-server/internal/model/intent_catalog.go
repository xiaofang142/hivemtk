package model

import (
	"time"

	"gorm.io/gorm"
)

// IntentCatalog 统一意图目录（TB-10 刀1）：把 runtime Intent 词表与 service
// 家族词表的双家族债收敛为一张租户级数据表，绑定/识别/编排/LLM 候选
// 全部消费同一目录；service/runtime Intent 常量作为目录的「内置种子」
// （SeedBuiltinDefaults 幂等写入，source=default）。
//
// 冻结契约（TB-10，2026-11-10）：
//   - source 取值 default/custom：default=内置种子（含 TB-09 纠错覆盖语义
//     的载体），custom=自定义词条同步行（兼容映射视图消费迁移第一站）；
//   - family 为兼容视图标注：runtime/service/custom 三值，绑定侧
//     intent_families 映射由它派生（刀3），不参与唯一性；
//   - 唯一性 service 写侧保证（同 IntentToolChain 软删不占部分唯一索引
//     先例）：同租户同 intent_key 只一行启用态，冲突 4009 拒绝；
//   - keywords_json/examples_json 为 JSON 数组文本，识别侧消费形态与
//     IntentDef{Type,Name,Keywords,Examples,Description} 一一对应；
//   - enabled=false 行对识别/绑定消费方不可见（ListEnabled 过滤），
//     但保留目录记录（禁用不删除）。
type IntentCatalog struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	IntentKey string `gorm:"type:varchar(50);not null;index" json:"intent_key"`
	// Source 来源：default 内置种子 / custom 自定义词条同步。
	Source string `gorm:"type:varchar(20);not null;default:'default'" json:"source"`
	// Family 兼容视图标注：runtime/service/custom（刀3 映射 intent_families）。
	Family       string `gorm:"type:varchar(20);not null;default:'service'" json:"family"`
	Name         string `gorm:"type:varchar(100);not null" json:"name"`
	KeywordsJSON string `gorm:"type:text" json:"keywords_json"`
	ExamplesJSON string `gorm:"type:text" json:"examples_json"`
	Description  string `gorm:"type:varchar(500)" json:"description"`
	Enabled      bool   `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (IntentCatalog) TableName() string { return "intent_catalog" }

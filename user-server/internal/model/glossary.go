// Package model 提供多语言术语表（Glossary）的 GORM 实体定义。
//
// v1.2 出海多语言方案 - 保护 SKU / 价格 / 品牌名不被 LLM 翻译
// 文档依据：docs/企业级架构优化/多语言方案.md
//
// 设计原则：
//  1. TermID 全局唯一（uniqueIndex），用于跨模块引用
//  2. Category 维度归类（brand/sku/logistic/policy/other），便于按维度加载
//  3. Preserve=true 表示全语种不翻译（如品牌名、SKU 编号、价格数字）
//  4. Translations 用 JSONB 存储 {lang: text} 映射（如 {"en":"Apple","zh":"苹果"}）
//  5. Pattern 正则保护模式，命中即跳过翻译
//
// 命名规范遵循五层架构：
//   - 此文件属于 model 层：仅含实体定义，禁止业务方法
//   - CRUD 操作 → repository/glossary_repo.go
//   - 业务逻辑 → service/glossary.go
//   - API DTO → dto/glossary.go
//   - HTTP 路由 → controller/glossary.go
//
// 私域独立部署：无 merchant_id 字段
package model

import (
	"time"

	"gorm.io/gorm"
)

// GlossaryCategory 术语分类
type GlossaryCategory string

// GlossaryStatus 术语状态
type GlossaryStatus string

// Glossary 多语言术语表（保护 SKU/价格/品牌名不被 LLM 翻译）
//
// 私域独立部署：无 merchant_id
// 五层架构：仅定义数据结构，业务逻辑在 Service 层
type Glossary struct {
	ID           int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TermID       string         `gorm:"type:varchar(64);uniqueIndex:idx_term_id;not null" json:"term_id"`
	Category     string         `gorm:"type:varchar(32);index" json:"category"`
	Preserve     bool           `gorm:"default:false" json:"preserve"`
	Translations JSONMap        `gorm:"type:jsonb;column:translations;default:'{}'" json:"translations"`
	Pattern      string         `gorm:"type:varchar(256)" json:"pattern"`
	Status       string         `gorm:"type:varchar(16);default:'active'" json:"status"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (Glossary) TableName() string { return "glossaries" }

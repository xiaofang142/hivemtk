/*
 * @Author: xiaofang
 * @Date:
 * @LastEditors: xiaofang
 * @LastEditTime:
 * @FilePath: user-server/internal/model/faq_entry.go
 * @Description: 这是默认设置,请设置`customMade`, 打开koroFileHeader查看配置 进行设置: https://github.com/OBKoro1/koro1FileHeader/wiki/%E9%85%8D%E7%BD%AE
 */
package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/lib/pq"
)

// FAQEntry FAQ 知识库条目 (Layer1 快速匹配)
//
// 五层架构归属: L5 数据层 (横向)
// 设计依据: AI 智能体性能优化
//   - Layer1 路由决策依赖 FAQ 命中 (零 LLM, <100ms)
//   - 双层架构: Layer1 FAQ 命中 -> SkipLLM; Layer2 LLM 兜底
//
// 表: faq_entries
// 索引:
//   - idx_faq_enabled  (enabled 过滤, ListEnabled 快速取全部)
//   - idx_faq_intent   (intent 维度, MatchByIntent)
//   - idx_faq_gin      (question 全文检索, MatchByKeyword 兜底)
//
// 字段说明:
//   - Question: 用户问句 (用于关键词匹配 / 全文检索)
//   - Answer:   标准答案 (Layer1 命中后直接返回)
//   - Keywords: 中文分词后的关键词数组 (PG text[])
//   - Category: 业务分类 (logistics / pricing / aftersales / general)
//   - Intent:   关联意图 (与 IntentLog.IntentMajor 对齐)
//   - Confidence: 人工标注的基准置信度 (0-1, 用于动态阈值)
//   - HitCount: 命中次数 (用于优化排序 + 报表)
//
// QualityScore: 动态质量分 0-1, 默认 0.5 (: 用于周期衰减/正负反馈)
// LastHitAt: 最近一次命中时间 (: 用于 7 天未命中判定)
// NegativeHitCount: 用户负反馈次数 (: 用于快速降权)
type FAQEntry struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID               uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Question         string         `gorm:"type:text;not null" json:"question"`
	Answer           string         `gorm:"type:text;not null" json:"answer"`
	Keywords         pq.StringArray `gorm:"type:text[];default:'{}'" json:"keywords"`
	Category         string         `gorm:"type:varchar(64);default:''" json:"category"`
	Intent           string         `gorm:"type:varchar(64);default:''" json:"intent"`
	Confidence       float64        `gorm:"type:decimal(5,4);default:0" json:"confidence"`
	HitCount         int64          `gorm:"type:bigint;default:0" json:"hit_count"`
	QualityScore     float64        `gorm:"type:decimal(5,4);default:0.5" json:"quality_score"`
	LastHitAt        *time.Time     `gorm:"type:timestamptz" json:"last_hit_at,omitempty"`
	NegativeHitCount int            `gorm:"type:integer;default:0" json:"negative_hit_count"`
	AgentID          *uint          `gorm:"index" json:"agent_id,omitempty"`
	Enabled          *bool          `gorm:"type:boolean;default:true;not null" json:"enabled"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName GORM 表名
func (FAQEntry) TableName() string { return "faq_entries" }

// FAQ 匹配方式常量（2026-10-25 自 dto/faq_citation.go 下沉：repository 不得引用 dto，
// 架构文档 L5；dto 侧同名常量与其保持同值，收敛后由 dto 别名引用）。
const (
	// FAQMatchExact 问题串/关键词原样命中（强证据，可作高置信引用）
	FAQMatchExact = "exact"
	// FAQMatchToken 词元命中（兜底）
	FAQMatchToken = "token"
	// FAQMatchKeywordFallback 泛词兜底命中（前端折叠展示，低分直接丢弃）
	FAQMatchKeywordFallback = "keyword_fallback"
)

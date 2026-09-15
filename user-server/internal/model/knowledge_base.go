package model

import (
	"gorm.io/gorm"
	"time"
)

// knowledge_base.go 智能体知识库主表 (智能体 1:N 知识库)
//
// 五层架构归属: L5 数据层 (横向)
// 设计依据: 知识库隔离架构
//   - 渠道 1:1 智能体 → 智能体 1:N 知识库(RAG/FAQ/SOP) → 知识库 1:N 条目
//   - 默认严格隔离, 共享 = 显式白名单
//   - knowledge_bases 表: 记录每个知识库元数据(类型/所有者/可见性)
//   - agent_kb_bindings 表: 记录智能体 ↔ 知识库的多对多绑定关系
//   - 三个内容表 (faq_entries / sop_templates / knowledge_documents) 加 agent_id 字段
//     实现"按智能体隔离"的核心数据隔离
//
// 字段说明:
//   - KBCode: 业务唯一码 (KB-FAQ-001, KB-RAG-PROD-001 等)
//   - Type:   知识库类型 (faq / rag / sop)
//   - OwnerType: 所有者类型 (private=智能体私有, shared=共享)
//   - OwnerAgentID: 当 OwnerType=private 时, 指向所属智能体 (外键 ai_agents.id, 可空)
//   - MemberCount / DocCount: 冗余统计字段 (用于列表展示, 实际从内容表 COUNT)
//   - Enabled: 用 *bool 避免 GORM v2 零值问题
//
// 表: knowledge_bases
// 索引:
//   - UNIQUE(kb_code)            业务唯一
//   - INDEX(type)                按类型查询
//   - INDEX(owner_agent_id)      按智能体查询其私有 KB
//   - INDEX(enabled)             过滤禁用
type KnowledgeBase struct {
	ID           uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	KBCode       string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"kb_code"`
	Type         string         `gorm:"type:varchar(16);not null;index" json:"type"`
	Name         string         `gorm:"type:varchar(128);not null" json:"name"`
	Description  string         `gorm:"type:text" json:"description"`
	OwnerType    string         `gorm:"type:varchar(16);not null;default:private" json:"owner_type"`
	OwnerAgentID *uint          `gorm:"index" json:"owner_agent_id,omitempty"`
	MemberCount  int            `gorm:"type:integer;default:0" json:"member_count"`
	DocCount     int            `gorm:"type:integer;default:0" json:"doc_count"`
	Enabled      *bool          `gorm:"type:boolean;default:true;not null;index" json:"enabled"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName GORM 表名
func (KnowledgeBase) TableName() string { return "knowledge_bases" }

// KnowledgeBaseType 知识库类型枚举
const (
	KnowledgeBaseTypeFAQ = "faq"
	KnowledgeBaseTypeRAG = "rag"
	KnowledgeBaseTypeSOP = "sop"
)

// KnowledgeBaseOwnerType 知识库所有者类型
const (
	KnowledgeBaseOwnerPrivate = "private"
	KnowledgeBaseOwnerShared  = "shared"
)

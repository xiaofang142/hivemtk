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
//   - Version / CanaryEnabled / CanaryPercent: 答案缓存的版本与灰度路由，见下方"版本口径"
//
// 表: knowledge_bases
// 索引:
//   - UNIQUE(kb_code)            业务唯一
//   - INDEX(type)                按类型查询
//   - INDEX(owner_agent_id)      按智能体查询其私有 KB
//   - INDEX(enabled)             过滤禁用
//
// 版本口径（G-1 / T-P2-05）——"版本"这三列到底管什么、不管什么：
//
// a) 生效面只有 rag_answer_cache 一层。该表唯一索引键是 (kb_id, prompt_version)，
// 而 prompt_version 在挂载前是常量字面量 "v1"（service/smart_cs_orchestrator.go
// 的 faqPromptVersion），全仓无第二个写入值。本列的作用就是把那个常量变成可路由的
// 命名空间；除此之外**不改变任何检索结果**：knowledge_chunks 按 product_id 归属、
// faq_entries 按 agent_id 归属，两者与 knowledge_bases 行都没有外键关系
// （实测见 docs/architecture/AI_CORE_FEATURE_INVENTORY.md 短板 G18）。
// b) 键折算规则只有一条：Version <= 1 一律折算成 "v1"。存量行经 AutoMigrate 补列后
// 读到 0 或 1 都命中该规则 ⇒ 升级后不改任何缓存键，这是"未开灰度时逐条一致"
// （AC③）成立的原因，不是巧合。改这条规则等于清空全量答案缓存。
// c) 灰度组读写的命名空间 = Version+1 那一个号，因此"转正"就是把 Version 加一，
// 流量落在灰度组已经焐热的同一批行上；回滚就是退回去。两个版本的行同时存在于
// 同一张表 ⇒ 转正/回滚都不需要重灌数据（AC②）。前提在仓储层：
// Repository.UpdateVersionCanary 必须走 UpdateColumns 不 bump updated_at，
// 否则 cache/service.go 的 fresh() 会把另一个版本的行整批删掉。
// d) 分桶沿用仓内既有口径（flagBucketHash，FNV-1a，key = "kb.{id}"），不新造第三套；
// 话术（ScriptLibrary/ScriptVersion/script_ab.go）早有自己的一套版本+分桶，
// 报价话术要挂版本请用那套，本表三列不覆盖话术。
// e) 三态旗子 FF_LTC_KB_CANARY 与行上的 CanaryPercent 是两道独立的锁：旗子 off（默认）
// 或 shadow 时**恒**用 "v1"（shadow 只把"本会落到哪"写日志），只有 on 才允许换命名空间
// ⇒ 本卡上线当天对线上缓存零影响，运维抬旗才生效。
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

	// Version / CanaryEnabled / CanaryPercent 追加在结构体末尾，而不是插在 Enabled 之后：
	// 三列会随 model 一起进 JSON 响应，插在中间等于把已有消费方看到的键序挪了位。
	// default 让 AutoMigrate 补列时存量行直接落 1（PG 对常量默认值不重写表），
	// not null 是必需的而不是修饰：可空的话，一旦有行把 version 存成 NULL，
	// GORM 把它读回 int 时是转换错误而不是零值 ⇒ 整行连管理端列表都打不开。
	// 实测补充（探针见 service/knowledge_base_version_start_test.go 文件头）：这个
	// `default:1` 今天**由 GORM 在客户端填值**，不依赖 DB 默认值——把列默认值 DROP 掉之后
	// 只走仓储 Create 仍然落 1。所以 service 侧那次归一是冗余保险，两者都不是唯一来源。
	// 折算规则仍要认 0 与负数（见 b）：今天仓内**没有**会把 version 写成 0 的 KB 通路
	// （仓储 Update 的列 map 不含这三列），所以"认 0"是防御性的——它让"缓存键不漂移"这条
	// 性质不依赖"永远没人写出 0"（裸 SQL 补数、绕过 service 的写入都能造出 0）。
	Version       int   `gorm:"not null;default:1" json:"version"`
	CanaryEnabled *bool `gorm:"type:boolean;default:false" json:"canary_enabled"`
	CanaryPercent int   `gorm:"not null;default:0" json:"canary_percent"`
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

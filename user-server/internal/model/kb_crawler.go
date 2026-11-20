// Package model T4 知识库冷启动：爬取源 / 爬取批次 / QA 候选对三张表。
//
// 设计文档：docs/architecture/design/T4-kb-coldstart.md
//
// 建表登记：走 internal/pkg/db 的 RegisterExtraModels（kb_crawler_register.go）——
// 生产建表只跑 AutoMigrate，同 kb_release.go 文件头 T-P9-02 实测口径。
// 所有表带 TenantID（隔离判据 = 有无 tenant_id 列，见 tenancyidx）。
package model

import (
	"time"

	"gorm.io/gorm"
)

// —— 爬取源状态机 ——

const (
	KBCrawlerSourceStatusActive = "active"
	KBCrawlerSourceStatusPaused = "paused"
	KBCrawlerSourceStatusError  = "error"
)

var kbCrawlerSourceLegalTransitions = map[string]map[string]bool{
	KBCrawlerSourceStatusActive: {
		KBCrawlerSourceStatusPaused: true,
		KBCrawlerSourceStatusError:  true,
	},
	KBCrawlerSourceStatusPaused: {
		KBCrawlerSourceStatusActive: true,
	},
	KBCrawlerSourceStatusError: {
		KBCrawlerSourceStatusActive: true,
	},
}

// KBCrawlerSourceTransitionAllowed 判 from→to 是否合法跃迁（唯一判据，读写两侧都问它）。
// 跃迁表写法对齐 kb_release.go 的 kbChangeLegalTransitions。
func KBCrawlerSourceTransitionAllowed(from, to string) bool {
	return kbCrawlerSourceLegalTransitions[from][to]
}

// KBCrawlerSource 爬取源配置（表 kb_crawler_sources）
type KBCrawlerSource struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`
	ID       uint `gorm:"primaryKey;autoIncrement" json:"id"`

	// KBID 目标知识库 knowledge_bases.id（逻辑外键，不建 FK——KB 删除不能牵连源历史）
	KBID uint `gorm:"index;not null" json:"kb_id"`
	// SourceType url / sitemap
	SourceType string `gorm:"type:varchar(16);not null" json:"source_type"`
	// StartURL 页面 URL 或 sitemap.xml
	StartURL string `gorm:"type:text;not null" json:"start_url"`
	// IncludePatterns 逗号分隔 URL 前缀白名单（空 = 同域全量）
	IncludePatterns string `gorm:"type:text" json:"include_patterns"`
	// MaxPages 单批次页面上限（默认 200，配额见设计文档 §8.2）
	MaxPages int `gorm:"not null;default:200" json:"max_pages"`
	// IntervalHours resync 周期（默认 168h = 7 天，对齐 Chatbase auto-retrain）
	IntervalHours int `gorm:"not null;default:168" json:"interval_hours"`
	// AutoResync *bool 防 GORM 零值覆盖 default:true
	AutoResync *bool `gorm:"type:boolean" json:"auto_resync"`
	// QASplitEnabled *bool：是否对爬得的文档跑 QA 拆分（默认开）
	QASplitEnabled *bool `gorm:"type:boolean" json:"qa_split_enabled"`
	// SiteVerifyToken 域名验证 token（首次爬取前校验通过才发请求，§8.1 硬门槛）
	SiteVerifyToken string `gorm:"type:varchar(64);default:''" json:"site_verify_token"`
	SiteVerified    bool   `gorm:"not null;default:false" json:"site_verified"`

	LastCrawlAt *time.Time `json:"last_crawl_at"`
	LastBatchID string     `gorm:"type:varchar(64);default:''" json:"last_batch_id"`
	// 状态机：active ⇄ paused（人工）；error（连续 3 批失败自动置入，人工恢复回 active）
	Status   string `gorm:"type:varchar(16);not null;default:'active';index" json:"status"`
	ErrorMsg string `gorm:"type:text" json:"error_msg"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (KBCrawlerSource) TableName() string { return "kb_crawler_sources" }

// —— 爬取批次 ——

const (
	KBCrawlBatchStatusPending         = "pending"
	KBCrawlBatchStatusRunning         = "running"
	KBCrawlBatchStatusSucceeded       = "succeeded"
	KBCrawlBatchStatusFailed          = "failed"
	KBCrawlBatchStatusPartiallyFailed = "partially_failed"

	KBCrawlTriggerManual = "manual"
	KBCrawlTriggerCron   = "cron"
)

// KBCrawlBatch 爬取批次（表 kb_crawl_batches）——只增的事件流：
// 重试新建行、旧行保持 failed 不覆盖（对齐 kb_change_audit_logs 只增语义）
type KBCrawlBatch struct {
	TenantID uint   `gorm:"not null;default:1;index" json:"tenant_id"`
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	SourceID uint   `gorm:"index;not null" json:"source_id"`
	BatchNo  string `gorm:"type:varchar(64);index;not null" json:"batch_no"` // crawl_<unixnano>
	// Trigger manual / cron
	Trigger string `gorm:"type:varchar(16);not null" json:"trigger"`
	// Status: pending → running → succeeded / failed / partially_failed
	Status   string `gorm:"type:varchar(16);not null;default:'pending';index" json:"status"`
	ErrorMsg string `gorm:"type:text" json:"error_msg"`

	PagesFound     int        `gorm:"not null;default:0" json:"pages_found"`
	PagesNew       int        `gorm:"not null;default:0" json:"pages_new"`
	PagesChanged   int        `gorm:"not null;default:0" json:"pages_changed"`
	PagesUnchanged int        `gorm:"not null;default:0" json:"pages_unchanged"`
	PagesFailed    int        `gorm:"not null;default:0" json:"pages_failed"`
	RetryCount     int        `gorm:"not null;default:0" json:"retry_count"`
	LeaseToken     string     `gorm:"type:varchar(64);not null;default:''" json:"-"`
	LeaseUntil     *time.Time `gorm:"index" json:"-"`

	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (KBCrawlBatch) TableName() string { return "kb_crawl_batches" }

// KBCrawlPage binds a source URL to its document and durable completion checkpoint.
type KBCrawlPage struct {
	ID            uint64 `gorm:"primaryKey;autoIncrement"`
	TenantID      uint   `gorm:"not null;uniqueIndex:uidx_crawl_page,priority:1"`
	SourceID      uint   `gorm:"not null;uniqueIndex:uidx_crawl_page,priority:2"`
	URLHash       string `gorm:"type:varchar(64);not null;uniqueIndex:uidx_crawl_page,priority:3"`
	URL           string `gorm:"type:text;not null"`
	KBID          uint   `gorm:"not null;index"`
	DocumentID    uint   `gorm:"not null;index"`
	ContentHash   string `gorm:"type:varchar(64);not null"`
	CompletedHash string `gorm:"type:varchar(64);not null;default:''"`
	QAHash        string `gorm:"type:varchar(64);not null;default:''"`
	ErrorMsg      string `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (KBCrawlPage) TableName() string { return "kb_crawl_pages" }

// —— QA 候选对（与 T1 审核队列共用状态机）——

const (
	QACandidateSourceTypeCrawl    = "crawl"
	QACandidateSourceTypeFeedback = "feedback"
	QACandidateSourceTypeManual   = "manual"

	QACandidateStatusPendingReview = "pending_review"
	QACandidateStatusApproved      = "approved"
	QACandidateStatusRejected      = "rejected"
	QACandidateStatusIndexed       = "indexed"
)

var qaCandidateLegalTransitions = map[string]map[string]bool{
	QACandidateStatusPendingReview: {
		QACandidateStatusApproved: true,
		QACandidateStatusRejected: true,
	},
	QACandidateStatusApproved: {
		QACandidateStatusIndexed: true,
	},
	QACandidateStatusRejected: {},
	QACandidateStatusIndexed:  {},
}

// QACandidateTransitionAllowed 判 from→to 是否合法跃迁（T4 爬取拆分与 T1 反馈回流共用）。
func QACandidateTransitionAllowed(from, to string) bool {
	return qaCandidateLegalTransitions[from][to]
}

// QACandidate QA 候选对（表 qa_candidates）——LLM 拆分/反馈回流产生的待审核问答对。
// 状态机与 T1 审核队列共用一份；唯一差别是 source_type。
type QACandidate struct {
	TenantID uint   `gorm:"not null;default:1;index" json:"tenant_id"`
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// SourceType crawl / feedback(T1) / manual
	SourceType string `gorm:"type:varchar(16);not null;index" json:"source_type"`
	// SourceRef crawl: doc:<id>；feedback: session:<id>
	SourceRef string `gorm:"type:varchar(128);index" json:"source_ref"`
	KBID      uint   `gorm:"index" json:"kb_id"`

	Question string `gorm:"type:text;not null" json:"question"`
	Answer   string `gorm:"type:text;not null" json:"answer"`
	// AnswerAnchor 答案锚定的原文片段（防 LLM 编造事实，§1.2）
	AnswerAnchor string `gorm:"type:text" json:"answer_anchor"`

	// Status: pending_review → approved → indexed(终态)；pending_review → rejected(终态)
	Status   string `gorm:"type:varchar(16);not null;default:'pending_review';index" json:"status"`
	Reviewer string `gorm:"type:text" json:"reviewer"`
	// TaskID 所属 QA 拆分任务（一篇文档一个任务，task_id 聚合判断完成度）
	TaskID   string `gorm:"type:varchar(64);index" json:"task_id"`
	LLMModel string `gorm:"type:varchar(64)" json:"llm_model"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (QACandidate) TableName() string { return "qa_candidates" }

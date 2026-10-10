package model

import (
	"time"
)

// EmbedStatus 文档嵌入状态
type EmbedStatus string

const (
	EmbedStatusPending    EmbedStatus = "pending"
	EmbedStatusProcessing EmbedStatus = "processing"
	EmbedStatusIndexed    EmbedStatus = "indexed"
	EmbedStatusFailed     EmbedStatus = "failed"
)

// SourceType 文档来源类型
type SourceType string

const (
	SourceTypeUpload  SourceType = "upload"
	SourceTypeText    SourceType = "text"
	SourceTypeURL     SourceType = "url"
	SourceTypeBatch   SourceType = "batch"
	SourceTypeOpenAPI SourceType = "openapi"
)

// KnowledgeDocument 知识库文档(产品维度)
type KnowledgeDocument struct {
	ID            uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID     string      `gorm:"index;not null;default:''" json:"product_id"`
	SourceType    SourceType  `gorm:"size:16;default:'upload';index" json:"source_type"`
	SourceRef     string      `gorm:"size:512" json:"source_ref"`
	Title         string      `gorm:"size:256;not null" json:"title"`
	FileName      string      `gorm:"size:256" json:"file_name"`
	FilePath      string      `gorm:"size:512" json:"file_path"`
	FileURL       string      `gorm:"size:512" json:"file_url"`
	FileType      string      `gorm:"size:16" json:"file_type"`
	FileSize      int64       `gorm:"default:0" json:"file_size"`
	MimeType      string      `gorm:"size:64" json:"mime_type"`
	ChunkCount    int         `gorm:"default:0" json:"chunk_count"`
	TotalTokens   int         `gorm:"default:0" json:"total_tokens"`
	EmbedStatus   EmbedStatus `gorm:"size:16;default:'pending';index" json:"embed_status"`
	EmbedProgress int         `gorm:"default:0" json:"embed_progress"`
	ErrorMsg      string      `gorm:"type:text" json:"error_msg"`
	Tags          string      `gorm:"type:jsonb;default:'[]'" json:"tags"`
	Category      string      `gorm:"size:64;index" json:"category"`
	PublicVisible bool        `gorm:"default:false;index" json:"public_visible"`
	HCStatus      string      `gorm:"size:20;default:'';index" json:"help_center_status"`
	HCViews       int64       `gorm:"default:0" json:"help_center_views"`
	Priority      int         `gorm:"default:0" json:"priority"`
	Metadata      string      `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	ImportedBy    string      `gorm:"size:64" json:"imported_by"`
	AgentID       *uint       `gorm:"index" json:"agent_id,omitempty"`
	LastIndexAt   *time.Time  `json:"last_index_at"`
	// SearchCount/HitCount 文档级检索计数：全仓无写入路径，恒 0（I31 核实）。
	// 真统计需检索命中时回写文档级计数或从 KnowledgeChunk.HitCount 聚合，
	// 属产品口径决策；当前前端详情/热门文档已诚实化为「未接入」占位。
	SearchCount int64     `gorm:"default:0" json:"search_count"`
	HitCount    int64     `gorm:"default:0" json:"hit_count"`
	Status      int       `gorm:"default:1" json:"status"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (KnowledgeDocument) TableName() string {
	return "knowledge_documents"
}

// KnowledgeChunk 知识库分段详情
type KnowledgeChunk struct {
	ID              uint64  `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID      uint64  `gorm:"index;not null" json:"document_id"`
	ProductID       string  `gorm:"index;not null;default:''" json:"product_id"`
	ChunkIndex      int     `gorm:"not null" json:"chunk_index"`
	Content         string  `gorm:"type:text;not null" json:"content"`
	ContentHash     string  `gorm:"size:64;index" json:"content_hash"`
	TokenCount      int     `gorm:"default:0" json:"token_count"`
	CharCount       int     `gorm:"default:0" json:"char_count"`
	EmbeddingID     string  `gorm:"size:64" json:"embedding_id"`
	SimilarityScore float64 `gorm:"default:0" json:"similarity_score"`
	HitCount        int     `gorm:"default:0" json:"hit_count"`
	Weight          float64 `gorm:"type:double precision;not null;default:1" json:"weight"`
	Metadata        string  `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	SourceLanguage  string  `gorm:"type:varchar(8);default:'zh'" json:"source_language"`
	// D16: 向量来源（'tei'=真实模型 / 'hash'=FNV 兜底）；读路径按 'tei' 过滤
	EmbeddingSource string `gorm:"type:varchar(16);not null;default:'tei'" json:"embedding_source"`
	// T-P9-02 发布制三列。可见性判据只有一处（kbrelease.hiddenPredicate），这里只声明形状。
	//
	// 三列都**不带索引**，两个理由：
	//   - 读路径不按它们过滤单行，而是在已命中的行上做一次相关子查询判定；
	//   - AutoMigrate 给一张有存量的热表建索引会取 SHARE 锁（阻塞写入），而这三列
	//     在升级后的一段时间里全是默认值 0，建了也没人走。
	//
	// 默认 0 的语义是"升级前的存量行"，它保证两件事：启用发布制不会让已上线内容
	// 当场消失（0 > effective 恒假），以及任何忘记打戳的新写入退化成"按存量对待"
	// 而不是"凭空不可见"—— 少打一个戳的表现是多放一条可见，不是少一条，
	// 这个失败方向是刻意选的（同审批闸门"没拿到批准"与"再开一条待办"的分开法）。
	KBVersion          int       `gorm:"column:kb_version;not null;default:0" json:"kb_version"`
	RetiredVersion     int       `gorm:"column:retired_version;not null;default:0" json:"retired_version"`
	ChangeID           string    `gorm:"type:varchar(40);column:change_id;not null;default:''" json:"change_id"`
	TranslatedVersions JSONMap   `gorm:"type:jsonb;column:translated_versions" json:"translated_versions,omitempty"`
	CreatedAt          time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 表名
func (KnowledgeChunk) TableName() string {
	return "knowledge_chunks"
}

// KnowledgeImportLog 知识库导入审计日志
type KnowledgeImportLog struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID   string    `gorm:"index;not null;default:''" json:"product_id"`
	DocumentID  *uint64   `gorm:"index" json:"document_id"`
	SourceType  string    `gorm:"size:16" json:"source_type"`
	BatchNo     string    `gorm:"size:32;index" json:"batch_no"`
	Status      string    `gorm:"size:16;default:'success'" json:"status"`
	Operator    string    `gorm:"size:64" json:"operator"`
	IP          string    `gorm:"size:64" json:"ip"`
	UserAgent   string    `gorm:"size:256" json:"user_agent"`
	DurationMs  int       `json:"duration_ms"`
	ErrorDetail string    `gorm:"type:jsonb" json:"error_detail"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (KnowledgeImportLog) TableName() string {
	return "knowledge_import_logs"
}

// KnowledgeSearchLog 知识库检索统计日志
type KnowledgeSearchLog struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID           string    `gorm:"index;default:''" json:"product_id"`
	Query               string    `gorm:"type:text" json:"query"`
	QueryHash           string    `gorm:"size:64;index" json:"query_hash"`
	TopK                int       `json:"top_k"`
	SimilarityThreshold float64   `json:"similarity_threshold"`
	ResultCount         int       `json:"result_count"`
	MaxScore            float64   `json:"max_score"`
	MinScore            float64   `json:"min_score"`
	AvgScore            float64   `json:"avg_score"`
	LatencyMs           int       `json:"latency_ms"`
	Hit                 int       `gorm:"default:0" json:"hit"`
	Source              string    `gorm:"size:32" json:"source"`
	SessionID           string    `gorm:"size:120" json:"session_id"`
	CreatedAt           time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (KnowledgeSearchLog) TableName() string {
	return "knowledge_search_logs"
}

// KnowledgeOpenAPISource 知识库 OpenAPI 数据源
type KnowledgeOpenAPISource struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID       string     `gorm:"index;not null;default:''" json:"product_id"`
	Name            string     `gorm:"size:128;not null" json:"name"`
	Type            string     `gorm:"size:16;not null;default:'rest'" json:"type"`
	Endpoint        string     `gorm:"size:512;not null" json:"endpoint"`
	Method          string     `gorm:"size:8;default:'GET'" json:"method"`
	AuthType        string     `gorm:"size:16;default:'none'" json:"auth_type"`
	AuthConfig      string     `gorm:"type:jsonb" json:"auth_config"`
	RequestTemplate string     `gorm:"type:text" json:"request_template"`
	ResponsePath    string     `gorm:"size:256" json:"response_path"`
	FieldMapping    string     `gorm:"type:jsonb;default:'{}'" json:"field_mapping"`
	Schedule        string     `gorm:"size:32" json:"schedule"`
	Enabled         int        `gorm:"default:1" json:"enabled"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	LastStatus      string     `gorm:"size:16;default:'never'" json:"last_status"`
	LastError       string     `gorm:"type:text" json:"last_error"`
	TotalSynced     int64      `gorm:"default:0" json:"total_synced"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (KnowledgeOpenAPISource) TableName() string {
	return "knowledge_openapi_sources"
}

type HelpCenterTestRecord struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID string    `gorm:"type:varchar(64);index" json:"product_id"`
	Query     string    `gorm:"type:varchar(300);not null" json:"query"`
	TopK      int       `json:"top_k"`
	Hits      int       `json:"hits"`
	Results   string    `gorm:"type:text" json:"results"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (HelpCenterTestRecord) TableName() string { return "help_center_test_records" }

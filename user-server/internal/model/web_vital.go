package model

import "time"

// WebVitalRecord 前端性能指标（Web Vitals: CLS/FID/LCP/FCP/TTFB）
type WebVitalRecord struct {
	ID     uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	Metric string  `gorm:"type:varchar(16);index;not null" json:"metric"`
	Value  float64 `json:"value"`
	// web-vitals v4 的 rating 枚举是 good / needs-improvement / poor，
	// 其中 "needs-improvement" 是 17 字符 —— 原先的 varchar(16) 一遇到中等评分就撞
	// SQLSTATE 22001，整个上报 500 且前端 .catch(()=>{}) 静默吞掉，性能数据永久丢失。
	// 32 是按规范最长值留的余量。
	Rating    string    `gorm:"type:varchar(32)" json:"rating"`
	Page      string    `gorm:"type:varchar(300)" json:"page"`
	SessionID string    `gorm:"type:varchar(64);index" json:"session_id"`
	UserAgent string    `gorm:"type:varchar(300)" json:"user_agent"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (WebVitalRecord) TableName() string { return "web_vital_records" }

type RagEvalQuestion struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	RunID     uint   `gorm:"index" json:"run_id"`
	ProductID string `gorm:"type:varchar(64);index" json:"product_id"`
	Question  string `gorm:"type:text;not null" json:"question"`
	Answer    string `gorm:"type:text" json:"answer"`

	SourceDocID     string  `gorm:"type:varchar(128);index" json:"source_doc_id"`
	SourceChunkIdx  int     `gorm:"default:0" json:"source_chunk_idx"`
	RelevantDocIDs  string  `gorm:"type:text" json:"relevant_doc_ids"`
	RetrievedDocIDs string  `gorm:"type:text" json:"retrieved_doc_ids"`
	Hit             bool    `gorm:"default:false" json:"hit"`
	Recall          float64 `gorm:"type:decimal(6,4);default:0" json:"recall"`
	Precision       float64 `gorm:"type:decimal(6,4);default:0" json:"precision"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (RagEvalQuestion) TableName() string { return "rag_eval_questions" }

type RagEvalRun struct {
	ID          uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	Total       int     `json:"total"`
	Hit         int     `json:"hit"`
	Recall5     float64 `json:"recall5"`
	MRR         float64 `json:"mrr"`
	NDCG5       float64 `json:"ndcg5"`
	EvalSetSize int     `json:"eval_set_size"`

	Name         string     `gorm:"type:varchar(128)" json:"name"`
	Status       string     `gorm:"type:varchar(20);default:'completed';index" json:"status"`
	AvgRecall    float64    `gorm:"type:decimal(6,4);default:0" json:"avg_recall"`
	AvgPrecision float64    `gorm:"type:decimal(6,4);default:0" json:"avg_precision"`
	ErrorMsg     string     `gorm:"type:text" json:"error_msg"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (RagEvalRun) TableName() string { return "rag_eval_runs" }

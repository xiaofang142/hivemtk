package controller

import (
	"context"

	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/model"
)

type OpenAPISyncResult struct {
	SourceID    uint64 `json:"source_id"`
	TotalItems  int    `json:"total_items"`
	ImportedNum int    `json:"imported_num"`
	SkippedNum  int    `json:"skipped_num"`
	FailedNum   int    `json:"failed_num"`
	DurationMs  int64  `json:"duration_ms"`
	Status      string `json:"status"`
	ErrorMsg    string `json:"error_msg"`
}

type OpenAPISourcePort interface {
	ListSources(ctx context.Context, productID string) ([]knowledgemodel.KnowledgeOpenAPISource, error)
	CreateSource(ctx context.Context, src *knowledgemodel.KnowledgeOpenAPISource) error
	GetSource(ctx context.Context, productID string, id int64) (*knowledgemodel.KnowledgeOpenAPISource, error)
	UpdateSource(ctx context.Context, id uint64, patch model.OpenAPISourcePatch) (*knowledgemodel.KnowledgeOpenAPISource, error)
	DeleteSource(ctx context.Context, productID string, id int64) error
	ToggleEnabled(ctx context.Context, productID string, id int64, enabled bool) error
	SyncSource(ctx context.Context, productID string, sourceID int64) (*OpenAPISyncResult, error)
	TestConnection(ctx context.Context, src *knowledgemodel.KnowledgeOpenAPISource) (map[string]any, error)
}

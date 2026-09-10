// kb_document_chunk_repo.go 知识库文档切片仓储（五层 L5）
//
// KBDocumentChunk 是 service 域内私有 gorm 行模型（kb_document_chunks 表），
// 全部 SQL 收口到本仓储，service 只做差分编排。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// KBDocumentChunkRow kb_document_chunks 行
type KBDocumentChunkRow struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID    uint      `gorm:"index;not null" json:"document_id"`
	ChunkIndex    int       `gorm:"not null;default:0" json:"chunk_index"`
	ContentHash   string    `gorm:"type:varchar(64);index;not null" json:"content_hash"`
	ChunkContent  string    `gorm:"type:text" json:"chunk_content"`
	EmbeddingHash string    `gorm:"type:varchar(64);default:''" json:"embedding_hash"`
	Status        string    `gorm:"type:varchar(20);default:'active';index" json:"status"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (KBDocumentChunkRow) TableName() string { return "kb_document_chunks" }

// KBDocumentChunkRepository 文档切片仓储
type KBDocumentChunkRepository struct {
	db *gorm.DB
}

// NewKBDocumentChunkRepository 构造
func NewKBDocumentChunkRepository(db *gorm.DB) *KBDocumentChunkRepository {
	return &KBDocumentChunkRepository{db: db}
}

// ListActiveByDocument 取文档全部 active 切片（按 chunk_index 升序）
func (r *KBDocumentChunkRepository) ListActiveByDocument(ctx context.Context, documentID uint) ([]KBDocumentChunkRow, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var chunks []KBDocumentChunkRow
	err := r.db.WithContext(ctx).
		Where("document_id = ? AND status = ?", documentID, "active").
		Order("chunk_index ASC").
		Find(&chunks).Error
	return chunks, err
}

// Create 新增切片
func (r *KBDocumentChunkRepository) Create(ctx context.Context, chunk *KBDocumentChunkRow) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(chunk).Error
}

// MarkSuperseded 批量标记切片为 superseded
func (r *KBDocumentChunkRepository) MarkSuperseded(ctx context.Context, ids []uint64) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).
		Exec("UPDATE kb_document_chunks SET status = ? WHERE id IN ?", "superseded", ids).Error
}

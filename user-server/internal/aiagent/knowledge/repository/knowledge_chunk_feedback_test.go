package repository

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestChunkUpdateFieldsInvalidateStaleEmbedding(t *testing.T) {
	fields := chunkUpdateFields(&model.KnowledgeChunk{
		Content:     "updated body",
		CharCount:   12,
		TokenCount:  2,
		ContentHash: "new-hash",
	})
	if value, ok := fields["embedding"]; !ok || value != nil {
		t.Fatalf("embedding update = %#v, want explicit NULL to invalidate old vector", value)
	}
	if fields["embed_status"] != "pending" {
		t.Fatalf("embed_status = %#v, want pending", fields["embed_status"])
	}
	if fields["content"] != "updated body" || fields["content_hash"] != "new-hash" {
		t.Fatalf("content update fields = %#v", fields)
	}
}

func TestChunkEmbeddingFailedFieldsKeepVectorUnavailable(t *testing.T) {
	fields := chunkEmbeddingFailedFields()
	if value, ok := fields["embedding"]; !ok || value != nil {
		t.Fatalf("embedding failure update = %#v, want explicit NULL", value)
	}
	if fields["embed_status"] != "failed" {
		t.Fatalf("embed_status = %#v, want failed", fields["embed_status"])
	}
}

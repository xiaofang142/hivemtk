package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/testutil"
)

func TestKnowledgeChunkRepository_ReplaceSplitRollsBackOnCreateFailure(t *testing.T) {
	t.Setenv(kbrelease.FlagEnv, "off")
	db := testutil.NewTestDBOrSkip(t, &model.KnowledgeChunk{})
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_test_split_document_chunk
		ON knowledge_chunks (document_id, chunk_index)`).Error; err != nil {
		t.Fatalf("创建拆分故障注入索引失败：%v", err)
	}
	defer func() {
		if err := db.Exec(`DROP INDEX IF EXISTS idx_test_split_document_chunk`).Error; err != nil {
			t.Errorf("清理拆分故障注入索引失败：%v", err)
		}
	}()

	original := model.KnowledgeChunk{DocumentID: 73001, ProductID: "split-test", ChunkIndex: 4, Content: "原分段"}
	blocker := model.KnowledgeChunk{DocumentID: 73001, ProductID: "split-test", ChunkIndex: 6, Content: "冲突分段"}
	if err := db.Create(&original).Error; err != nil {
		t.Fatalf("创建原分段失败：%v", err)
	}
	if err := db.Create(&blocker).Error; err != nil {
		t.Fatalf("创建冲突夹具失败：%v", err)
	}

	replacements := []model.KnowledgeChunk{
		{DocumentID: original.DocumentID, ProductID: original.ProductID, ChunkIndex: 5, Content: "替换一"},
		{DocumentID: original.DocumentID, ProductID: original.ProductID, ChunkIndex: 6, Content: "替换二"},
	}
	err := NewKnowledgeChunkRepository(db).ReplaceSplit(context.Background(), original.ID, replacements)
	if err == nil {
		t.Fatal("第二条替换分段冲突时应返回创建错误")
	}

	var rows []model.KnowledgeChunk
	if err := db.Where("document_id = ?", original.DocumentID).Order("chunk_index ASC").Find(&rows).Error; err != nil {
		t.Fatalf("回读拆分结果失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("失败事务应保留原分段和冲突夹具，实际行数 %d", len(rows))
	}
	if rows[0].ID != original.ID || rows[0].Content != "原分段" {
		t.Fatalf("原分段未在回滚后保留：%+v", rows[0])
	}
	if rows[1].ID != blocker.ID || rows[1].Content != "冲突分段" {
		t.Fatalf("失败事务留下了部分替换分段：%+v", rows[1])
	}
}

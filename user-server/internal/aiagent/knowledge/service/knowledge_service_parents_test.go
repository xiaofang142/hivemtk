package service

// knowledge_service_parents_test.go 父子分段的生产侧（P1-1 前置小刀）。
//
// 三格：
//  1. 长文档（多块）→ 建父块、子块全部带 parent_chunk_id 且指向正确窗口；
//  2. 短文档（单块）→ 不建父块、parent_chunk_id 全 0（行为闸：与现状逐字节一致）；
//  3. 窗口超字符上限 → 该窗口不建父块，其余窗口照建（不因一条超长窗口整篇放弃）。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"

	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/testutil"
)

// seedParentDoc 种一篇文档并跑流水线；返回服务与其 DB。
func runPipelineForParents(t *testing.T, docID uint64, content string) (*KnowledgeService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KnowledgeDocument{}, &model.KnowledgeChunk{}, &model.KnowledgeChunkParent{})
	// embedding/embed_status 不在 gorm model（走裸 SQL），与 G13 测试同口径手补。
	if err := db.Exec(`ALTER TABLE knowledge_chunks
		ADD COLUMN IF NOT EXISTS embedding vector,
		ADD COLUMN IF NOT EXISTS embed_status varchar(16) NOT NULL DEFAULT 'pending'`).Error; err != nil {
		t.Fatalf("补 chunk 向量列失败: %v", err)
	}
	svc := NewKnowledgeService()
	if err := db.Create(&model.KnowledgeDocument{
		ID: docID, ProductID: "p-parent", Title: "父子分段", EmbedStatus: model.EmbedStatusPending,
	}).Error; err != nil {
		t.Fatalf("种文档失败: %v", err)
	}
	svc.processDocumentAsync(context.Background(), docID, "p-parent",
		"", "inline.txt", content, "text/plain", "父子分段", model.SourceTypeText, nil)
	return svc, db
}

func TestProcessDocumentAsync_BuildsParentWindows(t *testing.T) {
	// 每块目标 ~500 token，写足够长保证 >3 块。
	content := strings.Repeat("这是一段用于父子分段验证的知识库内容，需要足够长以便切出多个分块。", 120)
	docID := uint64(11)
	_, db := runPipelineForParents(t, docID, content)

	var children []model.KnowledgeChunk
	if err := db.Where("document_id = ?", docID).Order("chunk_index").Find(&children).Error; err != nil {
		t.Fatalf("读子块失败: %v", err)
	}
	if len(children) < 2 {
		t.Fatalf("子块数 = %d，用例需要多块文档", len(children))
	}
	var parents []model.KnowledgeChunkParent
	if err := db.Where("document_id = ?", docID).Order("id").Find(&parents).Error; err != nil {
		t.Fatalf("读父块失败: %v", err)
	}
	wantParents := (len(children) + parentWindowSize - 1) / parentWindowSize
	if len(parents) != wantParents {
		t.Fatalf("父块数 = %d want %d（子块 %d / 窗口 %d）", len(parents), wantParents, len(children), parentWindowSize)
	}
	for i, c := range children {
		if c.ParentChunkID == 0 {
			t.Errorf("子块 #%d parent_chunk_id=0，应指向父块", i)
			continue
		}
		want := parents[i/parentWindowSize].ID
		if c.ParentChunkID != want {
			t.Errorf("子块 #%d parent=%d want %d（窗口映射错位）", i, c.ParentChunkID, want)
		}
	}
	// 父块内容 = 窗口内子块拼接（子块文本必须整段出现在父块里，否则回拼会把上下文拼丢）
	for w, p := range parents {
		start := w * parentWindowSize
		end := start + parentWindowSize
		if end > len(children) {
			end = len(children)
		}
		for i := start; i < end; i++ {
			if !strings.Contains(p.Content, children[i].Content) {
				t.Errorf("父块 #%d 内容缺子块 #%d 的文本", w, i)
			}
		}
	}
}

func TestProcessDocumentAsync_SingleChunkHasNoParent(t *testing.T) {
	docID := uint64(12)
	_, db := runPipelineForParents(t, docID, "只有一句话的短文档。")

	var parents int64
	db.Model(&model.KnowledgeChunkParent{}).Where("document_id = ?", docID).Count(&parents)
	if parents != 0 {
		t.Errorf("短文档不应建父块，实际 %d 行", parents)
	}
	var children []model.KnowledgeChunk
	db.Where("document_id = ?", docID).Find(&children)
	if len(children) != 1 {
		t.Fatalf("短文档子块数 = %d want 1", len(children))
	}
	if children[0].ParentChunkID != 0 {
		t.Errorf("短文档 parent_chunk_id = %d want 0", children[0].ParentChunkID)
	}
}

func TestProcessDocumentAsync_OversizeWindowSkipped(t *testing.T) {
	old := parentMaxChars
	parentMaxChars = 10 // 任何窗口都超限
	t.Cleanup(func() { parentMaxChars = old })

	docID := uint64(13)
	_, db := runPipelineForParents(t, docID,
		strings.Repeat("超长窗口场景的知识库内容，重复以切出多块。", 60))

	var parents int64
	db.Model(&model.KnowledgeChunkParent{}).Where("document_id = ?", docID).Count(&parents)
	if parents != 0 {
		t.Errorf("全部窗口超上限时不应建父块，实际 %d 行", parents)
	}
	var children []model.KnowledgeChunk
	db.Where("document_id = ?", docID).Find(&children)
	if len(children) < 2 {
		t.Fatalf("用例需要多块文档，实际 %d 块", len(children))
	}
	for i, c := range children {
		if c.ParentChunkID != 0 {
			t.Errorf("子块 #%d parent_chunk_id = %d want 0（超限窗口应跳过）", i, c.ParentChunkID)
		}
	}
}

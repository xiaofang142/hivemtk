package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.93.0（K-4 API 台账轮）验收判据：
//
//	① 存量库模拟：AutoMigrate 表上手工补回旧 bootstrap 的 filename NOT NULL 列后，
//	   Up 能把 NOT NULL 放宽为可空；
//	② 回填：file_name 为空的行取 filename 历史值；
//	③ 放宽后：不带 filename 的插入（= 模型路径）不再 23502；
//	④ 幂等重入：第二次 Up 不报错；
//	⑤ 全新库（无 filename 列）Up 空操作不报错；
//	⑥ Down 拒绝恢复 NOT NULL（遗留列降级不重紧）。
func TestKnowledgeDocumentsLegacyFilenameMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDBOrSkip(t, &model.KnowledgeDocument{})
	ctx := context.Background()

	// 存量库模拟：AutoMigrate 建出的新形表上补回旧 bootstrap 的 filename NOT NULL 列
	if err := database.Exec(
		`ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS filename VARCHAR(256) NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatalf("预置加 filename NOT NULL 列失败: %v", err)
	}

	// 存量行：filename 有历史值、file_name 空（bootstrap 时代写入形态）
	if err := database.Model(&model.KnowledgeDocument{}).Create(map[string]any{
		"product_id":  "k4-legacy-prod",
		"title":       "legacy bootstrap doc",
		"source_type": model.SourceTypeUpload,
		"filename":    "old-report.pdf",
		"file_type":   ".pdf",
	}).Error; err != nil {
		t.Fatalf("预置存量行失败: %v", err)
	}

	m := NewKnowledgeDocumentsLegacyFilenameMigration(database)
	// ① 放宽 NOT NULL + ② 回填
	if err := m.Up(ctx); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// ④ 幂等重入
	if err := m.Up(ctx); err != nil {
		t.Fatalf("幂等重入失败: %v", err)
	}

	// ② 回填：file_name 取得 filename 历史值
	var fileName string
	if err := database.Raw(
		`SELECT file_name FROM knowledge_documents WHERE product_id = ?`, "k4-legacy-prod").
		Row().Scan(&fileName); err != nil {
		t.Fatalf("读回填结果: %v", err)
	}
	if fileName != "old-report.pdf" {
		t.Fatalf("② file_name 应回填为 filename 历史值 old-report.pdf，got %q", fileName)
	}

	// ① 列已可空
	var isNullable string
	if err := database.Raw(
		`SELECT is_nullable FROM information_schema.columns WHERE table_name = 'knowledge_documents' AND column_name = 'filename'`).
		Row().Scan(&isNullable); err != nil {
		t.Fatalf("读 filename 列元数据: %v", err)
	}
	if isNullable != "YES" {
		t.Fatalf("① filename 应可空，got is_nullable=%s", isNullable)
	}

	// ③ 模型路径插入（不带 filename）不再 23502
	doc := &model.KnowledgeDocument{
		ProductID:  "k4-legacy-prod",
		Title:      "fresh model-path doc",
		FileName:   "new.txt",
		SourceType: model.SourceTypeText,
	}
	if err := database.Create(doc).Error; err != nil {
		t.Fatalf("③ 模型路径插入失败（filename 放宽后应可插入）: %v", err)
	}

	// ⑤ 全新库（无 filename 列）Up 空操作
	if err := database.Migrator().DropColumn(&model.KnowledgeDocument{}, "filename"); err != nil {
		t.Fatalf("预置删 filename 列失败: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("⑤ 无 filename 列时 Up 应空操作，got err: %v", err)
	}

	// ⑥ Down 不恢复 NOT NULL（也不删列）
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down 失败: %v", err)
	}
}

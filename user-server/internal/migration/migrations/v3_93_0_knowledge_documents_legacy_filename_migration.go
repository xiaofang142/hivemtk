package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// KnowledgeDocumentsLegacyFilenameMigration v3.93.0（K-4 API 台账轮，2026-11-12）：
// 旧 bootstrap 库 knowledge_documents.filename NOT NULL 残留放宽为可空。
//
// 背景：init-user-db.sql 时代的 knowledge_documents 只有 filename NOT NULL 等
// 9 列；模型（model.KnowledgeDocument）此后演进为 file_name/file_path/
// embed_status/... 富表，而 AutoMigrate 只「加列不降约束」⇒ 存量库上 filename
// 恒无写入者又恒 NOT NULL，任何文档插入都报 23502「null value in column
// filename」——POST /api/knowledge/import/text 等全部导入路径 400「保存文档
// 记录失败」（2026-11-12 本地 8401 实测）。全新部署（纯 AutoMigrate 建表）
// 没有 filename 列，不受影响。
//
// 处理：① 老列里可能有 bootstrap 时代的历史文件名 → 先回填 file_name
// （file_name 为空的行取 filename）；② filename DROP NOT NULL 放宽为可空
// （列保留不销毁历史数据；模型无此字段，后续写入恒 NULL，无害）。
//
// 幂等可重入：HasColumn 判存在 + 回填带 WHERE file_name 空守卫，重复执行空操作。
type KnowledgeDocumentsLegacyFilenameMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*KnowledgeDocumentsLegacyFilenameMigration)(nil)

// NewKnowledgeDocumentsLegacyFilenameMigration 构造
func NewKnowledgeDocumentsLegacyFilenameMigration(db *gorm.DB) *KnowledgeDocumentsLegacyFilenameMigration {
	return &KnowledgeDocumentsLegacyFilenameMigration{db: db}
}

func (m *KnowledgeDocumentsLegacyFilenameMigration) Version() string { return "v3.93.0" }

func (m *KnowledgeDocumentsLegacyFilenameMigration) Name() string {
	return "knowledge_documents.filename legacy not-null relaxation"
}

func (m *KnowledgeDocumentsLegacyFilenameMigration) Description() string {
	return "旧 bootstrap 库 knowledge_documents.filename NOT NULL 残留放宽为可空（file_name 空行先回填），修复存量库上任何文档插入 23502「保存文档记录失败」"
}

func (m *KnowledgeDocumentsLegacyFilenameMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	probe := knowledgeDocumentFilenameProbe{}
	if !db.Migrator().HasTable(probe) || !db.Migrator().HasColumn(probe, "filename") {
		// 全新部署（纯 AutoMigrate 建表）没有 filename 列：空操作
		return nil
	}
	if db.Migrator().HasColumn(probe, "file_name") {
		if err := db.Exec(
			`UPDATE knowledge_documents SET file_name = filename ` +
				`WHERE COALESCE(file_name, '') = '' AND filename IS NOT NULL AND filename <> ''`).Error; err != nil {
			return fmt.Errorf("回填 file_name ← filename 失败: %w", err)
		}
	}
	if err := db.Exec(
		`ALTER TABLE knowledge_documents ALTER COLUMN filename DROP NOT NULL`).Error; err != nil {
		return fmt.Errorf("放宽 knowledge_documents.filename NOT NULL 失败: %w", err)
	}
	return nil
}

func (m *KnowledgeDocumentsLegacyFilenameMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// filename 是 bootstrap 时代的遗留列，不在模型声明内（AutoMigrate 不持有）：
	// 重收紧约束会把存量库上所有后续插入重新打回 23502，故降级不恢复 NOT NULL。
	declineColumnDrop(m.Version(), "knowledge_documents.filename")
	return nil
}

// knowledgeDocumentFilenameProbe 只为 Migrator().HasColumn 服务（按 TableName 判列存在性）。
type knowledgeDocumentFilenameProbe struct{}

func (knowledgeDocumentFilenameProbe) TableName() string { return "knowledge_documents" }

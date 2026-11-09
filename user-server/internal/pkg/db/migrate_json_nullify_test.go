package db

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestNullifyEmptyJSONTextColumns 钉住 text→jsonb 迁移前的空串归一：
// 只把空串改成 NULL，已有文案与 NULL 都不得触碰，重复执行幂等。
func TestNullifyEmptyJSONTextColumns(t *testing.T) {
	database := testutil.NewTestDB(t, &model.KnowledgeImportLog{})
	if err := database.Exec(`INSERT INTO knowledge_import_logs (tenant_id, product_id, error_detail)
		VALUES (1, 'p-empty', ''), (1, 'p-msg', 'dial tcp failed'), (1, 'p-null', NULL)`).Error; err != nil {
		t.Fatalf("seed knowledge_import_logs: %v", err)
	}

	assertRows := func(stage string) {
		t.Helper()
		var rows []struct {
			ProductID   string
			ErrorDetail *string
		}
		if err := database.Raw(`SELECT product_id, error_detail FROM knowledge_import_logs ORDER BY product_id`).
			Scan(&rows).Error; err != nil {
			t.Fatalf("%s: query rows: %v", stage, err)
		}
		if len(rows) != 3 {
			t.Fatalf("%s: rows = %d, want 3", stage, len(rows))
		}
		got := make(map[string]*string, len(rows))
		for _, r := range rows {
			got[r.ProductID] = r.ErrorDetail
		}
		if got["p-empty"] != nil {
			t.Fatalf("%s: p-empty error_detail = %q, want NULL", stage, *got["p-empty"])
		}
		if got["p-null"] != nil {
			t.Fatalf("%s: p-null should stay NULL", stage)
		}
		if got["p-msg"] == nil || *got["p-msg"] != "dial tcp failed" {
			t.Fatalf("%s: p-msg error_detail changed: %v", stage, got["p-msg"])
		}
	}

	nullifyEmptyJSONTextColumns(database)
	assertRows("first run")
	nullifyEmptyJSONTextColumns(database)
	assertRows("idempotent second run")
}

// TestNullifyEmptyJSONTextColumnsSkipsMissingTable 表不存在时必须静默跳过，
// 不能在启动迁移链上制造错误。
func TestNullifyEmptyJSONTextColumnsSkipsMissingTable(t *testing.T) {
	database := testutil.NewTestDB(t)
	database.Exec(`DROP TABLE IF EXISTS knowledge_import_logs`)
	nullifyEmptyJSONTextColumns(database)
}

package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestRetrievalObservation_MigrationPreservesOldIdentity(t *testing.T) {
	database := testutil.NewTestDB(t)
	for _, statement := range []string{
		`DROP TABLE IF EXISTS knowledge_search_logs`,
		`CREATE TABLE knowledge_search_logs (id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL DEFAULT 1, product_id BIGINT DEFAULT 0, vector_latency_ms BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO knowledge_search_logs (tenant_id, product_id) VALUES (18, 42), (19, NULL)`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	m := NewKnowledgeSearchObservationMigration(database)
	for i := 0; i < 2; i++ {
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("迁移应兼容并幂等保留旧记录：%v", err)
		}
	}
	var dataType string
	if err := database.Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='knowledge_search_logs' AND column_name='product_id'`).Scan(&dataType).Error; err != nil {
		t.Fatal(err)
	}
	if dataType != "text" {
		t.Fatalf("知识库身份应为字符串，而不是%s", dataType)
	}
	var old []struct {
		TenantID  uint
		ProductID *string
	}
	if err := database.Table("knowledge_search_logs").Select("tenant_id, product_id").Order("id").Find(&old).Error; err != nil {
		t.Fatal(err)
	}
	if len(old) != 2 || old[0].TenantID != 18 || old[0].ProductID == nil || *old[0].ProductID != "42" || old[1].TenantID != 19 || old[1].ProductID != nil {
		t.Fatal("迁移不得改变历史租户、数字身份或NULL")
	}
	if err := database.Create(&model.KnowledgeSearchLog{TenantID: 18, ProductID: "cb35812c-cd41-4e23-b4a8-1d28929291b7"}).Error; err != nil {
		t.Fatalf("新UUID日志应可写：%v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Model(&model.KnowledgeSearchLog{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("降级不能删除日志或把UUID强制转换为数字")
	}
}

func TestRetrievalObservation_MigrationIsRegistered(t *testing.T) {
	source, err := os.ReadFile("initial_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "register(NewKnowledgeSearchObservationMigration(db))") {
		t.Fatal("兼容迁移必须进入正式迁移链")
	}
}

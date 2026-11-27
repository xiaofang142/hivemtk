package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubVisibilityBackfillMigrationContract(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "104_backfill_message_hub_visibility.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取可见性回填迁移失败: %v", err)
	}
	sql := string(data)
	for _, want := range []string{
		"UPDATE message_hub",
		"'member'",
		"'public'",
		"jsonb_build_array(group_id)",
		"backfill_channel_ingress",
		"correlation_id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("迁移缺少关键片段 %q", want)
		}
	}
}

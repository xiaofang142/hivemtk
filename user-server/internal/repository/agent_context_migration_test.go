package repository

import (
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestAgentContextMigration101SchemaContract(t *testing.T) {
	path := repoMigrationPath(t, "101_agent_context_session_cache.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移 101 失败：%v", err)
	}
	sql := strings.Join(strings.Fields(string(raw)), " ")

	required := []string{
		"ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS visibility varchar(32) NOT NULL DEFAULT 'unknown'",
		"ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS audience_json jsonb",
		"ALTER TABLE message_hub ADD COLUMN IF NOT EXISTS visibility_evidence jsonb",
		"ALTER TABLE session_messages ADD COLUMN IF NOT EXISTS correlation_id varchar(100)",
		"idx_session_messages_correlation",
		"ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1",
		"ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS source_cursor varchar(191) NOT NULL DEFAULT ''",
		"ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS source_cursor_hash char(64) NOT NULL DEFAULT ''",
		"ALTER TABLE conversation_contexts ADD COLUMN IF NOT EXISTS summary_version bigint NOT NULL DEFAULT 1",
		"CREATE TABLE IF NOT EXISTS conversation_memberships",
		"CREATE TABLE IF NOT EXISTS conversation_message_dispositions",
		"CREATE TABLE IF NOT EXISTS message_tombstones",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_conv_membership_active",
		"WHERE left_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_agent_conv_disposition",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_agent_msg_tombstone",
		"CHECK (status IN ('pending', 'processing', 'replied', 'skipped', 'merged'))",
		"CHECK (tombstone_type IN ('edited', 'deleted', 'recalled', 'redacted'))",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("迁移 101 缺少契约片段：%s", token)
		}
	}

	for _, table := range []string{
		model.ConversationMembership{}.TableName(),
		model.ConversationMessageDisposition{}.TableName(),
		model.MessageTombstone{}.TableName(),
	} {
		if !strings.Contains(sql, table) {
			t.Fatalf("迁移 101 未覆盖模型表 %q", table)
		}
	}
}

func TestAgentContextMigration101RunsTwice(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.MessageHub{},
		&model.SessionMessage{},
		&model.ConversationContext{},
		&model.ConversationMembership{},
		&model.ConversationMessageDisposition{},
		&model.MessageTombstone{},
	)
	for i := 0; i < 2; i++ {
		execMigrationFile(t, database, "101_agent_context_session_cache.sql")
	}

	for _, column := range []string{"visibility", "audience_json", "visibility_evidence"} {
		var count int64
		if err := database.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name = 'message_hub' AND column_name = ?`, column).
			Scan(&count).Error; err != nil {
			t.Fatalf("查询 message_hub.%s 失败：%v", column, err)
		}
		if count != 1 {
			t.Fatalf("message_hub.%s 应存在且唯一，实际 %d 列", column, count)
		}
	}
	for _, table := range []string{"conversation_memberships", "conversation_message_dispositions", "message_tombstones"} {
		var count int64
		if err := database.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_name = ?`, table).
			Scan(&count).Error; err != nil {
			t.Fatalf("查询表 %s 失败：%v", table, err)
		}
		if count != 1 {
			t.Fatalf("表 %s 应存在且唯一，实际 %d 张", table, count)
		}
	}
}

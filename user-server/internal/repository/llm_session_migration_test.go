package repository

import (
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestLLMSessionMigration102SchemaContract(t *testing.T) {
	path := repoMigrationPath(t, "102_llm_session_ledger.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移 102 失败：%v", err)
	}
	sql := strings.Join(strings.Fields(string(raw)), " ")
	required := []string{
		"CREATE TABLE IF NOT EXISTS llm_session_states",
		"CREATE TABLE IF NOT EXISTS scenario_transcripts",
		"CREATE TABLE IF NOT EXISTS tool_invocations",
		"bus_llm_session_id_enc bytea NOT NULL",
		"bus_llm_session_id_blind char(64) NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_llm_session_state",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_scenario_transcript_sequence",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_scenario_transcript_idempotency",
		"CREATE UNIQUE INDEX IF NOT EXISTS uni_tool_invocation_idempotency",
		"WHERE tool_idempotency_key <> ''",
		"CHECK (retention_mode IN ('stateless', 'native'))",
		"CHECK (entry_type IN ('model_step', 'tool_call', 'tool_result', 'handoff', 'compaction'))",
		"CHECK (risk_class IN ('read', 'write', 'high_write'))",
		"CHECK (status IN ('planned', 'authorized', 'dispatched', 'unknown', 'confirmed'))",
		"ON DELETE CASCADE",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("迁移 102 缺少契约片段：%s", token)
		}
	}
	for _, table := range []string{
		model.LLMSessionState{}.TableName(),
		model.ScenarioTranscript{}.TableName(),
		model.ToolInvocation{}.TableName(),
	} {
		if !strings.Contains(sql, table) {
			t.Fatalf("迁移 102 未覆盖模型表 %q", table)
		}
	}
}

func TestLLMSessionMigration102RunsTwice(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMSessionState{}, &model.ScenarioTranscript{}, &model.ToolInvocation{})
	for i := 0; i < 2; i++ {
		execMigrationFile(t, database, "102_llm_session_ledger.sql")
	}
	for _, table := range []string{"llm_session_states", "scenario_transcripts", "tool_invocations"} {
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

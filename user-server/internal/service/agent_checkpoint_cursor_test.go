// agent_checkpoint_cursor_test.go T-P1-01：恢复游标的判定依据与开关解析。
package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

const checkpointTableDDL = `CREATE TABLE IF NOT EXISTS agent_checkpoints (
	id BIGSERIAL PRIMARY KEY,
	thread_id VARCHAR(120) NOT NULL,
	stage VARCHAR(40) NOT NULL,
	state JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT uk_agent_ckpt UNIQUE (thread_id, stage)
)`

// FF_LTC_CHECKPOINT 默认关闭；显式真值才开。反向：任何未识别值一律视为关（safe-by-default）。
func TestCheckpointEnabled_EnvParsing(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"off", false},
		{"maybe", false},
		{"1", true},
		{"true", true},
		{"ON", true},
		{" yes ", true},
	} {
		t.Setenv("FF_LTC_CHECKPOINT", tc.value)
		if got := CheckpointEnabled(); got != tc.want {
			t.Errorf("FF_LTC_CHECKPOINT=%q 应为 %v，got %v", tc.value, tc.want, got)
		}
	}

	// 真正 unset（不是空串）也必须为关
	t.Setenv("FF_LTC_CHECKPOINT", "1")
	if err := os.Unsetenv("FF_LTC_CHECKPOINT"); err != nil {
		t.Fatal(err)
	}
	if CheckpointEnabled() {
		t.Error("env 未设置时必须为关（默认安全侧）")
	}
}

// 游标 = 阶段序号最大的一行，与写入顺序、未知阶段名无关。
// 行序故意排成"末行不是最远阶段"，以杀死 rows[len-1] / rows[0] 这类偷懒实现。
func TestLatestByStageOrder_PicksFurthestStage(t *testing.T) {
	rows := []AgentCheckpoint{
		{Stage: "gatekeeper"},
		{Stage: "perception"},
		{Stage: "legacy_stage_not_in_list"},
		{Stage: "planner"},
		{Stage: "alignment"},
	}
	latest := LatestByStageOrder(rows)
	if latest == nil || latest.Stage != "planner" {
		t.Fatalf("游标应为 planner，got %+v", latest)
	}
	if got := ResumeStage(latest); got != "reviewer" {
		t.Errorf("planner 之后应从 reviewer 续跑，got %q", got)
	}

	if LatestByStageOrder(nil) != nil {
		t.Error("空记录应返回 nil")
	}
	only := LatestByStageOrder([]AgentCheckpoint{{Stage: "typo_stage"}})
	if only != nil {
		t.Errorf("全未知阶段应视为无游标（回退整体重跑），got %+v", only)
	}
	if got := ResumeStage(&AgentCheckpoint{Stage: "reviewer"}); got != "reviewer" {
		t.Errorf("末阶段续跑点应仍是 reviewer（整阶段重跑），got %q", got)
	}
}

// 端到端（真库）：早期阶段被重跑、时间戳与自增 id 反而最新时，游标不得倒退。
// 这正是旧实现（ORDER BY updated_at DESC LIMIT 1）会错的地方，也是 rows 末行
// 这类"按取回顺序取最后一条"的实现会错的地方。
func TestLoadLatestCheckpoint_CursorNeverGoesBackwards(t *testing.T) {
	db := testutil.NewTestDB(t)
	if err := db.Exec(checkpointTableDDL).Error; err != nil {
		t.Skipf("建表失败（环境限制）: %v", err)
	}
	repo := NewAgentCheckpointRepository(db)
	ctx := context.Background()

	const thread = "thr-cursor-1"
	state, _ := json.Marshal(map[string]any{"v": 1})
	for _, stage := range []string{"perception", "alignment", "gatekeeper", "planner"} {
		if err := repo.Save(ctx, thread, stage, state); err != nil {
			t.Fatal(err)
		}
	}
	// 模拟"感知阶段整阶段重跑"：upsert 把 perception 的 updated_at 顶到最新
	if err := repo.Save(ctx, thread, "perception", state); err != nil {
		t.Fatal(err)
	}
	// 再加一刀：perception 被删除后重建 → 它的自增 id 也变成最大
	if err := db.Exec("DELETE FROM agent_checkpoints WHERE thread_id = ? AND stage = 'perception'", thread).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, thread, "perception", state); err != nil {
		t.Fatal(err)
	}
	var newest string
	if err := db.Table("agent_checkpoints").Select("stage").
		Where("thread_id = ?", thread).Order("id DESC").Limit(1).Scan(&newest).Error; err != nil {
		t.Fatal(err)
	}
	if newest != "perception" {
		t.Fatalf("前置条件不成立：期望 perception 成为 id 最大行，got %q", newest)
	}

	latest, err := LoadLatestCheckpoint(ctx, repo, thread)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if latest == nil || latest.Stage != "planner" {
		t.Fatalf("游标必须是 planner（id/时间戳最新的 perception 不得倒退游标），got %+v", latest)
	}
	if got := ResumeStage(latest); got != "reviewer" {
		t.Errorf("续跑点应为 reviewer，got %q", got)
	}
}

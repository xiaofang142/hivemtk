package service

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// D06: 阶段边界 checkpoint 落库 + 恢复游标
func TestD06_CheckpointSaveResume(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	_ = db

	if err := db.Exec(checkpointTableDDL).Error; err != nil {
		t.Skipf("建表失败（环境限制）: %v", err)
	}
	repo := NewAgentCheckpointRepository(db)
	ctx := context.Background()

	if stage := ResumeStage(nil); stage != "perception" {
		t.Fatalf("无 checkpoint 应从 perception, got %s", stage)
	}

	state, _ := json.Marshal(map[string]any{"reply": "draft"})
	for _, st := range []string{"perception", "alignment", "gatekeeper", "planner"} {
		if err := repo.Save(ctx, "thr-1", st, state); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := LoadLatestCheckpoint(ctx, repo, "thr-1")
	if err != nil || latest == nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	if latest.Stage != "planner" {
		t.Errorf("最新 stage 应 planner, got %s", latest.Stage)
	}
	if stage := ResumeStage(latest); stage != "reviewer" {
		t.Errorf("应从 reviewer 续跑, got %s", stage)
	}

	var cnt int64
	db.Table("agent_checkpoints").Where("thread_id = ?", "thr-1").Count(&cnt)
	if cnt != 4 {
		t.Errorf("4 阶段应 4 行, got %d", cnt)
	}
}

// 未知 stage 回退第一阶段
func TestD06_ResumeUnknownStage(t *testing.T) {
	if stage := ResumeStage(&AgentCheckpoint{Stage: "unknown_stage"}); stage != "perception" {
		t.Errorf("未知 stage 应回退 perception, got %s", stage)
	}
}

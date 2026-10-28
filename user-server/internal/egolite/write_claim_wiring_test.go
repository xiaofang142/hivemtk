package egolite

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 写台账接线（落地指南红线 2：声明写的执行必须留台账）：
//   - dispatch 时按 writes 声明占坑（幂等）；
//   - confirm 放行 → claimed→sent（动作名取最近一条 confirm 事件）；
//   - 刻意不标 verified（自述级审计没有独立证据，G-E5）。
func TestWriteClaimWiring(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.EgoliteExecution{}, &model.EgoEventLog{}, &model.EgoliteWriteClaim{},
	)
	if db == nil {
		t.Fatal("测试库不可用")
	}
	ctx := context.Background()
	execRepo := NewEgoExecutionRepositoryWithDB(db)
	evRepo := NewEgoEventRepositoryWithDB(db)
	wcRepo := NewEgoWriteClaimRepositoryWithDB(db)
	svc := NewEgoExecutionService(execRepo, evRepo, nil, nil, wcRepo, NewEgoRunnerRegistry())

	e := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecDispatching}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}

	// ① 占坑 + 幂等（redispatch 重复 dispatch 不重复占坑）
	svc.claimWrites(ctx, e, []string{"post_comment", " ", "follow"})
	svc.claimWrites(ctx, e, []string{"post_comment"})
	claims, err := wcRepo.ListByExecution(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("应占 2 个坑（空声明跳过、重复幂等）, got %d", len(claims))
	}
	for _, c := range claims {
		if c.ClaimState != model.EgoWriteClaimed {
			t.Fatalf("初始态应为 claimed, got %s", c.ClaimState)
		}
	}

	// ② 放行推进：最近一条 confirm 事件的 action → sent
	ok := true
	confirmPayload, _ := json.Marshal(map[string]any{"type": "confirm", "action": "post_comment"})
	if err := evRepo.Append(ctx, &model.EgoEventLog{ExecutionID: e.ID, Seq: 1, Type: "action", Ok: &ok}); err != nil {
		t.Fatal(err)
	}
	if err := evRepo.Append(ctx, &model.EgoEventLog{ExecutionID: e.ID, Seq: 2, Type: "confirm", Payload: confirmPayload}); err != nil {
		t.Fatal(err)
	}
	svc.markSentOnAllow(ctx, e)
	claims, _ = wcRepo.ListByExecution(ctx, e.ID)
	byAction := map[string]string{}
	for _, c := range claims {
		byAction[c.Action] = c.ClaimState
	}
	if byAction["post_comment"] != model.EgoWriteSent {
		t.Fatalf("放行的写应推进 sent, got %s", byAction["post_comment"])
	}
	if byAction["follow"] != model.EgoWriteClaimed {
		t.Fatalf("未放行的写应保持 claimed, got %s", byAction["follow"])
	}

	// ③ confirm 事件不带 action：如实不动
	e2 := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecDispatching}
	if err := execRepo.Create(ctx, e2); err != nil {
		t.Fatal(err)
	}
	svc.claimWrites(ctx, e2, []string{"post_comment"})
	barePayload, _ := json.Marshal(map[string]any{"type": "confirm"})
	if err := evRepo.Append(ctx, &model.EgoEventLog{ExecutionID: e2.ID, Seq: 1, Type: "confirm", Payload: barePayload}); err != nil {
		t.Fatal(err)
	}
	svc.markSentOnAllow(ctx, e2)
	claims2, _ := wcRepo.ListByExecution(ctx, e2.ID)
	if len(claims2) != 1 || claims2[0].ClaimState != model.EgoWriteClaimed {
		t.Fatalf("无 action 的 confirm 不应推进台账, got %+v", claims2)
	}
}

package egolite

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// after_id 增量游标（Monitor 轮询）：id 全表单调。重发/续轮会把低 seq 再写进同一
// execution——按 seq 当游标会漏掉第二轮的帧，这里钉住「游标后新增一条不少、之前不回」。
func TestEventCursorAfterID(t *testing.T) {
	execRepo, evRepo, _, _, _, _ := newExecTestEnv(t)
	ctx := context.Background()

	e := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecCreated}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	appendEvent := func(seq int, typ string) *model.EgoEventLog {
		ev := &model.EgoEventLog{ExecutionID: e.ID, Seq: seq, Type: typ}
		if err := evRepo.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	appendEvent(1, "action")
	appendEvent(2, "extract")
	last := appendEvent(1, "action") // 第二轮重发：seq 回到 1，id 继续单调

	// 全量游标（after_id=0）：三帧都回，按 id 升序
	all, err := evRepo.ListByExecutionAfter(ctx, e.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("after_id=0 应返回全部 3 帧, got %d", len(all))
	}
	if all[2].ID != last.ID || all[2].Seq != 1 {
		t.Fatalf("低 seq 的新帧不能被游标吞掉: got id=%d seq=%d", all[2].ID, all[2].Seq)
	}

	// 游标推进到最后一帧 id：不再回任何帧
	tail, err := evRepo.ListByExecutionAfter(ctx, e.ID, last.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 {
		t.Fatalf("游标之后不应有帧, got %d", len(tail))
	}

	// 游标停在第一帧：恰好回剩余两帧
	rest, err := evRepo.ListByExecutionAfter(ctx, e.ID, all[0].ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 2 {
		t.Fatalf("游标之后应恰有 2 帧, got %d", len(rest))
	}
}

// Redispatch 状态语义：只认 created（runner 上线前的重发口）；其他状态一律冲突。
// runner 在线的真下发路径需要 WS 对端，dispatch 内部的状态迁移由 dispatch 自身单测面覆盖。
func TestRedispatchOnlyFromCreated(t *testing.T) {
	execRepo, _, _, _, _, svc := newExecTestEnv(t)
	ctx := context.Background()

	e := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecCreated}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}

	// runner 不在线：保留 created（可等上线后再发），不报错
	got, err := svc.Redispatch(ctx, 7, e.ID)
	if err != nil {
		t.Fatalf("offline redispatch 不应报错: %v", err)
	}
	if got.State != model.EgoExecCreated {
		t.Fatalf("offline redispatch 应保留 created, got %s", got.State)
	}

	// 非 created：拒绝重发
	other := &model.EgoliteExecution{UserID: 7, Origin: "draft", State: model.EgoExecRunning}
	if err := execRepo.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redispatch(ctx, 7, other.ID); !errors.Is(err, ErrExecStateConflict) {
		t.Fatalf("running 态重发应返回 ErrExecStateConflict, got %v", err)
	}
}

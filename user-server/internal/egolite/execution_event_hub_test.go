package egolite

// execution_event_hub_test.go —— S-13 接线：IngestEvent 是唯一写入口，广播跟着它走。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func TestIngestEventPublishesToHub(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)
	hub := NewEgoEventHub()
	svc.AttachEventHub(hub)

	e := &model.EgoliteExecution{UserID: 5, Origin: "draft", State: model.EgoExecDispatching, TimeoutSec: 1800}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	signal, cancel := hub.Subscribe(e.ID)
	defer cancel()

	if err := svc.IngestEvent(ctx, 5, RunnerEvent{
		Seq: 1, Type: "action", ExecutionID: EgoExecKey(e.ID),
		Payload: []byte(`{"action":"goto","ok":true}`),
	}); err != nil {
		t.Fatalf("IngestEvent err: %v", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("事件落库后必须广播（否则 SSE 只能靠心跳，等于轮询）")
	}
}

// done 帧进终态也要广播（前端靠它收流，不该等 15s 心跳才发现结束）
func TestIngestDonePublishesToHub(t *testing.T) {
	ctx := context.Background()
	execRepo, _, _, _, _, svc := newExecTestEnv(t)
	hub := NewEgoEventHub()
	svc.AttachEventHub(hub)

	e := &model.EgoliteExecution{UserID: 6, Origin: "draft", State: model.EgoExecRunning, TimeoutSec: 1800}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	signal, cancel := hub.Subscribe(e.ID)
	defer cancel()

	if err := svc.IngestEvent(ctx, 6, RunnerEvent{
		Seq: 9, Type: "done", ExecutionID: EgoExecKey(e.ID),
		Payload: []byte(`{"message":"finished"}`), ExitCode: 0,
	}); err != nil {
		t.Fatalf("IngestEvent err: %v", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("终态事件必须广播")
	}
	got, err := execRepo.GetByID(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !model.EgoExecTerminalStates[got.State] {
		t.Fatalf("done 帧后应为终态，got %s", got.State)
	}
}

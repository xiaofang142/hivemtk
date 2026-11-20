package agent_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type outcomeStage struct {
	name string
	run  func(context.Context, *InferenceContext) StageResult
}

func (s outcomeStage) Name() string { return s.name }
func (s outcomeStage) Execute(ctx context.Context, ic *InferenceContext) StageResult {
	return s.run(ctx, ic)
}

func outcomeCycle(fault string, timeout bool, cause error, calls *[]string) *InferenceCycle {
	stages := make([]InferenceStage, 0, 5)
	for _, name := range testCheckpointOrder {
		stages = append(stages, outcomeStage{name, func(ctx context.Context, ic *InferenceContext) StageResult {
			*calls = append(*calls, name)
			if name == fault {
				ic.Plan = &ActionPlan{ReplyHint: "must not publish"}
				ic.Decision.Reply = "must not publish"
				if timeout {
					<-ctx.Done()
					return ContinueResult()
				}
				return FailResult(cause)
			}
			if name == "planner" {
				ic.Plan = &ActionPlan{PlanType: "reply", ReplyHint: "reviewed reply", SkipLLM: true, Confidence: 1}
			}
			return ContinueResult()
		}})
	}
	c := NewInferenceCycleWithStages(stages[0], stages[1], stages[2], stages[3])
	c.SetReviewerStage(stages[4])
	c.StageTimeout = 5 * time.Millisecond
	return c
}

func outcomeJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	blob, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(blob, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInferenceOutcomeStageFailures(t *testing.T) {
	for index, name := range testCheckpointOrder {
		for _, timeout := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/error", true: "/timeout"}[timeout], func(t *testing.T) {
				cause := errors.New("private provider failure")
				var calls []string
				cycle := outcomeCycle(name, timeout, cause, &calls)
				decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
				if err == nil {
					t.Fatalf("%s failure was acknowledged as success: %+v", name, decision)
				}
				if !timeout && !errors.Is(err, cause) {
					t.Fatalf("cause lost: %v", err)
				}
				if timeout && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline lost: %v", err)
				}
				if len(calls) != index+1 {
					t.Fatalf("later stage executed: %v", calls)
				}
				if decision == nil || decision.Plan != nil || decision.Reply != "" {
					t.Fatalf("unsafe partial result: %+v", decision)
				}
				view := outcomeJSON(t, decision)
				if view["outcome"] != "failed" || view["failed_stage"] != name || view["publishable"] != false {
					t.Fatalf("missing failure outcome: %v", view)
				}
				stages, ok := view["stages"].([]any)
				if !ok || len(stages) != index+1 {
					t.Fatalf("missing trace: %v", view)
				}
				stats := cycle.GetStats()
				if stats.TotalRuns != 1 || stats.FailureRuns != 1 || stats.SuccessRuns != 0 {
					t.Fatalf("wrong failure stats: %+v", stats)
				}
			})
		}
	}
}

func TestInferenceOutcomeCheckpointRetriesFailedStage(t *testing.T) {
	store := newFakeCheckpointStore(true)
	var calls []string
	cycle := outcomeCycle("reviewer", false, errors.New("review unavailable"), &calls)
	cycle.SetCheckpointStore(store)
	if _, err := cycle.RunOnce(checkpointTestContext(), checkpointPayload(), nil); err == nil {
		t.Fatal("expected reviewer error")
	}
	if store.doneAt(checkpointThread(t, checkpointPayload()), "planner") {
		t.Fatal("failed run marked completed")
	}
	calls = nil
	recovered := outcomeCycle("", false, nil, &calls)
	recovered.SetCheckpointStore(store)
	decision, err := recovered.RunOnce(checkpointTestContext(), checkpointPayload(), nil)
	if err != nil || len(calls) != 2 || calls[0] != "gatekeeper" || calls[1] != "reviewer" || decision == nil || decision.Reply != "reviewed reply" {
		t.Fatalf("bad recovery calls=%v decision=%+v err=%v", calls, decision, err)
	}
}

func TestInferenceOutcomeStreamRunsReviewAndDoesNotCompleteFailure(t *testing.T) {
	var calls []string
	cycle := outcomeCycle("reviewer", false, errors.New("review unavailable"), &calls)
	handler := NewBufferedStreamHandler(nil)
	_, err := cycle.RunOnceStream(context.Background(), testPayload(), nil, handler)
	if err == nil || len(calls) != 5 {
		t.Fatalf("stream omitted review failure calls=%v err=%v", calls, err)
	}
	errorsSeen := 0
	for _, event := range handler.Events() {
		if event.Type == StreamEventComplete {
			t.Fatal("failed stream emitted success complete")
		}
		if event.Type == StreamEventError {
			errorsSeen++
		}
	}
	if errorsSeen != 1 {
		t.Fatalf("error events=%d", errorsSeen)
	}
}

func TestInferenceOutcomeReviewRejectsPublication(t *testing.T) {
	var calls []string
	cycle := outcomeCycle("", false, nil, &calls)
	cycle.SetReviewerStage(outcomeStage{"reviewer", func(_ context.Context, ic *InferenceContext) StageResult {
		ic.Decision.Review = &ReviewResult{Passed: false}
		return ContinueResult()
	}})
	publisher := &capturePublisher{ch: make(chan string, 1)}
	orch := NewCoreDataFlowOrchestrator(cycle, nil)
	orch.SetPublisher(publisher)
	result, err := orch.Process(context.Background(), testPayload(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.FinalReply != "" || len(publisher.ch) != 0 {
		t.Fatalf("rejected reply published: %+v publisher=%+v", result, publisher)
	}
	if outcomeJSON(t, result)["outcome"] != "rejected" || cycle.GetStats().SuccessRuns != 0 {
		t.Fatalf("rejection reported as success: %+v", result)
	}
}

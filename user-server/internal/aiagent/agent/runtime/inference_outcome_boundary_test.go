package agent_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 拒绝必须在阶段事件、终态和检查点中保持一致。
func TestInferenceOutcomeStreamRejectionMatchesTrace(t *testing.T) {
	var calls []string
	cycle := outcomeCycle("", false, nil, &calls)
	cycle.SetReviewerStage(outcomeStage{"reviewer", func(_ context.Context, ic *InferenceContext) StageResult {
		ic.Decision.Review = &ReviewResult{Passed: false, AdjustedReply: "仍未通过的候选"}
		return ContinueResult()
	}})
	store := newFakeCheckpointStore(true)
	cycle.SetCheckpointStore(store)
	channel := NewChannelStreamHandler(32)
	buffer := NewBufferedStreamHandler(nil)
	decision, err := cycle.RunOnceStream(context.Background(), testPayload(), nil, NewCompositeStreamHandler(channel, buffer))
	if err != nil || decision.Outcome != InferenceRejected || decision.Publishable {
		t.Fatalf("拒绝终态错误：decision=%+v err=%v", decision, err)
	}
	select {
	case <-channel.Done():
	default:
		t.Fatal("拒绝后通道未关闭")
	}
	stageEnd, terminal := false, false
	for _, event := range buffer.Events() {
		if event.Type == StreamEventComplete || event.Type == StreamEventError {
			t.Fatalf("业务拒绝被标记成成功或技术错误：%+v", event)
		}
		if event.Type == StreamEventStageEnd && event.StageName == "reviewer" {
			stageEnd = true
			if event.Outcome != InferenceRejected {
				t.Fatalf("阶段事件与拒绝终态不一致：%+v", event)
			}
		}
		if event.Type == StreamEventOutcome {
			terminal = event.Outcome == InferenceRejected && !event.Publishable
		}
	}
	if !stageEnd || !terminal {
		t.Fatal("缺少复核阶段或拒绝终态事件")
	}
	_, blob, err := store.LoadResume(context.Background(), CheckpointThreadID(testPayload()))
	var saved checkpointState
	if err != nil || json.Unmarshal(blob, &saved) != nil || !saved.Done || saved.Snapshot.Decision.Outcome != InferenceRejected {
		t.Fatalf("拒绝检查点与返回不一致：%s err=%v", blob, err)
	}
	if cycle.GetStats().RejectedRuns != 1 || cycle.GetStats().SuccessRuns != 0 {
		t.Fatal("拒绝计入成功")
	}
}

// 最后阶段正常提前结束，不得误报为转人工。
func TestInferenceOutcomeReviewerStopIsSuccessful(t *testing.T) {
	var calls []string
	cycle := outcomeCycle("", false, nil, &calls)
	cycle.SetReviewerStage(outcomeStage{"reviewer", func(_ context.Context, ic *InferenceContext) StageResult {
		ic.Decision.Reply = "复核后的最终回复"
		return StopResult(&ic.Decision)
	}})
	decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
	if err != nil || decision.Outcome != InferenceSucceeded || !decision.Publishable || decision.HandoffToHuman || decision.Reply != "复核后的最终回复" {
		t.Fatalf("正常结束被错误分类：%+v err=%v", decision, err)
	}
	if cycle.GetStats().SuccessRuns != 1 || cycle.GetStats().EscalationRuns != 0 {
		t.Fatal("正常复核被计入转人工")
	}
}

func TestInferenceOutcomeFailureCheckpointAuditAndRecovery(t *testing.T) {
	for index, stage := range testCheckpointOrder {
		for _, timeout := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/error", true: "/timeout"}[timeout], func(t *testing.T) {
				var calls []string
				store := newFakeCheckpointStore(true)
				cycle := outcomeCycle(stage, timeout, errors.New("故障信号"), &calls)
				cycle.SetCheckpointStore(store)
				decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
				if err == nil {
					t.Fatal("故障未返回错误")
				}
				if got := store.stageCount(CheckpointThreadID(testPayload())); got != index {
					t.Fatalf("失败推进游标：%d，期望 %d", got, index)
				}
				if index > 0 {
					_, blob, loadErr := store.LoadResume(context.Background(), CheckpointThreadID(testPayload()))
					var saved checkpointState
					if loadErr != nil || json.Unmarshal(blob, &saved) != nil || saved.Done || saved.LastAttempt == nil {
						t.Fatalf("失败审计缺失：%s %v", blob, loadErr)
					}
					if !reflect.DeepEqual(saved.LastAttempt, decision) {
						t.Fatalf("失败审计与返回不同：saved=%+v decision=%+v", saved.LastAttempt, decision)
					}
					if len(saved.Snapshot.Stages) != index || saved.Snapshot.Decision.Outcome == InferenceFailed {
						t.Fatal("失败产出污染成功恢复快照")
					}
				}
				calls = nil
				recovered := outcomeCycle("", false, nil, &calls)
				recovered.SetCheckpointStore(store)
				reply, runErr := recovered.RunOnce(context.Background(), testPayload(), nil)
				if runErr != nil || reply.Outcome != InferenceSucceeded || reply.Reply != "reviewed reply" || !reflect.DeepEqual(calls, testCheckpointOrder[index:]) {
					t.Fatalf("恢复跳过失败阶段或重复成功阶段：%v %+v %v", calls, reply, runErr)
				}
			})
		}
	}
}

// 可选基础设施故障显式降级；真实门禁错误仍须关闭发布。
type outcomeMemoryFailure struct{}

func (outcomeMemoryFailure) LoadEpisodicMemory(context.Context, string, string) (string, error) {
	return "", errors.New("private memory fault")
}

type outcomeStoreFailure struct {
	*fakeCheckpointStore
	load, save bool
}

func (s outcomeStoreFailure) LoadResume(ctx context.Context, thread string) (string, json.RawMessage, error) {
	if s.load {
		return "", nil, errors.New("private checkpoint read fault")
	}
	return s.fakeCheckpointStore.LoadResume(ctx, thread)
}
func (s outcomeStoreFailure) Save(ctx context.Context, thread, stage string, blob json.RawMessage) error {
	if s.save {
		return errors.New("private checkpoint write fault")
	}
	return s.fakeCheckpointStore.Save(ctx, thread, stage, blob)
}

func TestInferenceOutcomeOptionalFailuresAreDegraded(t *testing.T) {
	for _, kind := range []string{"memory", "checkpoint_load", "checkpoint_save"} {
		t.Run(kind, func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle("", false, nil, &calls)
			if kind == "memory" {
				cycle.SetMemoryProvider(outcomeMemoryFailure{})
			} else {
				cycle.SetCheckpointStore(outcomeStoreFailure{newFakeCheckpointStore(true), kind == "checkpoint_load", kind == "checkpoint_save"})
			}
			decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
			if err != nil || decision.Outcome != InferenceDegraded || !decision.Publishable || len(decision.Degradations) != 1 || cycle.GetStats().DegradedRuns != 1 || cycle.GetStats().SuccessRuns != 0 {
				t.Fatalf("可选故障被吞或错误拒绝：%+v stats=%+v err=%v", decision, cycle.GetStats(), err)
			}
			blob, _ := json.Marshal(decision)
			if strings.Contains(string(blob), "private") {
				t.Fatal("内部故障泄漏到返回体")
			}
		})
	}
}

func TestInferenceOutcomeProtocolBoundaries(t *testing.T) {
	for _, kind := range []string{"missing_gate", "missing_reviewer", "invalid_result", "early_reply", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle("", false, nil, &calls)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "missing_gate":
				cycle.GatekeeperStage = nil
			case "missing_reviewer":
				cycle.SetReviewerStage(nil)
			case "invalid_result":
				cycle.PlannerStage = outcomeStage{"planner", func(context.Context, *InferenceContext) StageResult { return StageResult{} }}
			case "early_reply":
				cycle.AlignmentStage = outcomeStage{"alignment", func(_ context.Context, ic *InferenceContext) StageResult {
					ic.Decision.Reply = "未过门禁回复"
					return StopResult(&ic.Decision)
				}}
			case "canceled":
				cancel()
			}
			handler := NewChannelStreamHandler(32)
			decision, err := cycle.RunOnceStream(ctx, testPayload(), nil, handler)
			if err == nil || decision.Outcome != InferenceFailed || decision.Publishable || decision.Reply != "" || decision.Plan != nil {
				t.Fatalf("失败未关闭发布：%+v %v", decision, err)
			}
			select {
			case <-handler.Done():
			default:
				t.Fatal("技术失败通道未关闭")
			}
			for event := range handler.Events() {
				if event.Type == StreamEventComplete {
					t.Fatal("失败发送成功完成事件")
				}
			}
		})
	}
}

func TestInferenceOutcomeOrchestratorFaultHasNoPublication(t *testing.T) {
	for index, stage := range testCheckpointOrder {
		t.Run(stage, func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle(stage, false, errors.New("private provider failure"), &calls)
			publisher := &capturePublisher{ch: make(chan string, 1)}
			orch := NewCoreDataFlowOrchestrator(cycle, nil)
			orch.SetPublisher(publisher)
			result, err := orch.Process(context.Background(), testPayload(), nil)
			stats := orch.GetStats()
			if err == nil || result.Outcome != InferenceFailed || result.FinalReply != "" || result.Publishable || result.ToolCallCount != 0 || result.HandoffToHuman || len(publisher.ch) != 0 || len(result.Stages) != index+1 || stats.TotalTasks != 1 || stats.ErrorCount != 1 || stats.DirectReplyCount != 0 || stats.ToolCallCount != 0 || stats.FastHandoffCount != 0 {
				t.Fatalf("编排器故障仍有成功或副作用：result=%+v stats=%+v err=%v", result, stats, err)
			}
		})
	}
}

// 安全转人工保留提前结束；通过复核的修改稿不可被规划旧稿覆盖。
func TestInferenceOutcomeHandoffAndReviewedReply(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "reviewed_reply", true: "safe_handoff"}[handoff], func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle("", false, nil, &calls)
			if handoff {
				cycle.GatekeeperStage = outcomeStage{"gatekeeper", func(_ context.Context, ic *InferenceContext) StageResult {
					calls = append(calls, "gatekeeper")
					ic.Decision.HandoffToHuman, ic.Decision.HandoffReason = true, "安全转人工"
					return StopResult(&ic.Decision)
				}}
			} else {
				cycle.SetReviewerStage(outcomeStage{"reviewer", func(_ context.Context, ic *InferenceContext) StageResult {
					ic.Decision.Review = &ReviewResult{Passed: true, AdjustedReply: "复核后的最终版本"}
					ic.Decision.Reply = ic.Decision.Review.AdjustedReply
					return ContinueResult()
				}})
			}
			decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if handoff {
				if decision.Outcome != InferenceHandoff || decision.Publishable || len(calls) != 3 || cycle.GetStats().EscalationRuns != 1 {
					t.Fatalf("安全转人工异常：%+v calls=%v", decision, calls)
				}
			} else if decision.Outcome != InferenceSucceeded || decision.Reply != "复核后的最终版本" || !decision.Publishable {
				t.Fatalf("复核稿被覆盖：%+v", decision)
			}
		})
	}
}

func TestInferenceOutcomeOverallDeadlineAndContinuedError(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "continued_error", true: "total_deadline"}[deadline], func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle("", false, nil, &calls)
			cycle.TotalTimeout, cycle.StageTimeout = 5*time.Millisecond, time.Second
			cause := errors.New("内部故障与继续标志冲突")
			cycle.GatekeeperStage = outcomeStage{"gatekeeper", func(ctx context.Context, _ *InferenceContext) StageResult {
				if deadline {
					<-ctx.Done()
					return ContinueResult()
				}
				return StageResult{Continue: true, Error: cause}
			}}
			decision, err := cycle.RunOnce(context.Background(), testPayload(), nil)
			if err == nil || decision.Outcome != InferenceFailed || decision.Publishable || len(calls) != 2 {
				t.Fatalf("截止或冲突错误被忽略：%+v calls=%v err=%v", decision, calls, err)
			}
			if deadline && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("整体截止原因丢失")
			}
			if !deadline && !errors.Is(err, cause) {
				t.Fatal("阶段原始错误链丢失")
			}
		})
	}
}

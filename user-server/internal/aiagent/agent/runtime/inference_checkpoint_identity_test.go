package agent_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// 原实现已通过独立 overlay 复现 RED；此处保留同一真实恢复调度场景。
func TestCheckpointIdentityIsolationRed(t *testing.T) {
	for _, changed := range []string{"tenant", "account", "message", "agent", "version", "prompt", "model"} {
		t.Run(changed, func(t *testing.T) {
			store := newFakeCheckpointStore(true)
			payload := testPayload()
			payload.MessageID, payload.AccountID = "stable-message", "account-one"
			agent := AgentContext{AgentID: 7, AgentCode: "agent-one", Version: 1, SystemPrompt: "original policy", LLMModel: "model-one"}
			ctx := tenant.WithTenantID(context.Background(), 773)
			if !runRecovering(func() {
				_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, &agent)
			}) {
				t.Fatal("未模拟到阶段边界中断")
			}
			switch changed {
			case "tenant":
				ctx = tenant.WithTenantID(context.Background(), 774)
			case "account":
				payload.AccountID = "account-two"
			case "message":
				payload.MessageID = "another-stable-message"
			case "agent":
				agent.AgentID = 8
			case "version":
				agent.Version = 2
			case "prompt":
				agent.SystemPrompt = "updated policy"
			case "model":
				agent.LLMModel = "model-two"
			}
			resumed := newCounts()
			if _, err := newStageSet(store, resumed, map[string]string{}, "").cycle.RunOnce(ctx, payload, &agent); err != nil {
				t.Fatal(err)
			}
			if *resumed["perception"] != 1 || *resumed["gatekeeper"] != 1 {
				t.Fatalf("身份或配置改变后仍沿用旧恢复游标：perception=%d gatekeeper=%d", *resumed["perception"], *resumed["gatekeeper"])
			}
		})
	}
}

func checkpointScopedInputs() (context.Context, CustomerMessagePayload, *AgentContext) {
	payload := testPayload()
	payload.MessageID, payload.AccountID = "stable-message", "account-one"
	return tenant.WithTenantID(context.Background(), 773), payload, &AgentContext{AgentID: 7, AgentCode: "agent-one", Version: 1}
}

func TestCheckpointScopedFingerprintAllConfiguration(t *testing.T) {
	ctx, payload, original := checkpointScopedInputs()
	key, err := ScopedCheckpointThreadID(ctx, payload, original)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < reflect.TypeOf(*original).NumField(); index++ {
		field := reflect.TypeOf(*original).Field(index)
		if field.Name == "LoadedAt" {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			changed := *original
			value := reflect.ValueOf(&changed).Elem().Field(index)
			switch value.Kind() {
			case reflect.String:
				value.SetString(value.String() + "-changed")
			case reflect.Uint:
				value.SetUint(value.Uint() + 1)
			case reflect.Int:
				value.SetInt(value.Int() + 1)
			case reflect.Float64:
				value.SetFloat(value.Float() + 0.25)
			case reflect.Bool:
				value.SetBool(!value.Bool())
			case reflect.Slice:
				value.Set(reflect.ValueOf([]string{"changed"}))
			default:
				t.Fatalf("新增配置类型必须显式纳入测试：%s", field.Name)
			}
			other, err := ScopedCheckpointThreadID(ctx, payload, &changed)
			if err != nil || key == other {
				t.Fatalf("配置字段未隔离：%s key=%q err=%v", field.Name, other, err)
			}
		})
	}
	changed := payload
	changed.Raw = map[string]any{"policy": "changed"}
	other, err := ScopedCheckpointThreadID(ctx, changed, original)
	if err != nil || key == other {
		t.Fatal("原始消息语义改变后仍复用")
	}
}

func TestCheckpointScopedIdentityStableRecovery(t *testing.T) {
	ctx, payload, agent := checkpointScopedInputs()
	store := newFakeCheckpointStore(true)
	if !runRecovering(func() {
		_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, agent)
	}) {
		t.Fatal("未模拟阶段中断")
	}
	before, err := ScopedCheckpointThreadID(ctx, payload, agent)
	if err != nil {
		t.Fatal(err)
	}
	payload.TraceID, payload.Timestamp, agent.LoadedAt = "new-transport-trace", time.Unix(999, 0), time.Now()
	after, err := ScopedCheckpointThreadID(ctx, payload, agent)
	if err != nil || before != after {
		t.Fatal("加载时间或传输元数据改变了恢复身份")
	}
	counts := newCounts()
	decision, err := newStageSet(store, counts, map[string]string{}, "").cycle.RunOnce(ctx, payload, agent)
	if err != nil || decision.Outcome != InferenceSucceeded || *counts["perception"] != 0 || *counts["planner"] != 0 || *counts["gatekeeper"] != 1 || *counts["reviewer"] != 1 {
		t.Fatalf("同身份未安全恢复：counts=%v decision=%+v err=%v", counts, decision, err)
	}
}

func TestCheckpointInvalidScopeNeverTouchesStore(t *testing.T) {
	for _, invalid := range []string{"tenant", "message", "account", "channel", "customer", "session", "agent_code", "agent_nil", "version", "raw"} {
		t.Run(invalid, func(t *testing.T) {
			ctx, payload, agent := checkpointScopedInputs()
			switch invalid {
			case "tenant":
				ctx = context.Background()
			case "message":
				payload.MessageID = ""
			case "account":
				payload.AccountID = ""
			case "channel":
				payload.ChannelType = ""
			case "customer":
				payload.CustomerID = ""
			case "session":
				payload.SessionID = ""
			case "agent_code":
				agent.AgentCode = ""
			case "agent_nil":
				agent = nil
			case "version":
				agent.Version = -1
			case "raw":
				payload.Raw = map[string]any{"unsupported": func() {}}
			}
			if _, err := ScopedCheckpointThreadID(ctx, payload, agent); err == nil {
				t.Fatal("无效恢复身份被接受")
			}
			store, counts := newFakeCheckpointStore(true), newCounts()
			decision, err := newStageSet(store, counts, map[string]string{}, "").cycle.RunOnce(ctx, payload, agent)
			// nil agent 由调度器装配有效默认配置；其余无效身份禁止存储。
			if invalid == "agent_nil" {
				if err != nil || decision.Outcome != InferenceSucceeded || store.loadCalls != 1 || store.saves == 0 {
					t.Fatalf("默认智能体未正常完整运行：%+v %v", decision, err)
				}
				return
			}
			if err != nil || decision.Outcome != InferenceDegraded || store.loadCalls != 0 || store.saves != 0 {
				t.Fatalf("无可信身份仍触碰检查点：load=%d save=%d decision=%+v err=%v", store.loadCalls, store.saves, decision, err)
			}
			for _, name := range testCheckpointOrder {
				if *counts[name] != 1 {
					t.Fatalf("降级没有完整执行当前阶段：%s", name)
				}
			}
		})
	}
}

func TestCheckpointV3RejectsOldOrMismatchedSnapshot(t *testing.T) {
	for _, invalid := range []string{"v2", "fingerprint", "malformed", "missing_snapshot", "unknown_stage"} {
		t.Run(invalid, func(t *testing.T) {
			ctx, payload, agent := checkpointScopedInputs()
			store := newFakeCheckpointStore(true)
			if !runRecovering(func() {
				_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, agent)
			}) {
				t.Fatal("未模拟到中断")
			}
			thread, err := ScopedCheckpointThreadID(ctx, payload, agent)
			if err != nil {
				t.Fatal(err)
			}
			var saved checkpointState
			if err := json.Unmarshal(store.rows[thread]["planner"], &saved); err != nil {
				t.Fatal(err)
			}
			switch invalid {
			case "v2":
				saved.Version = 2
			case "fingerprint":
				saved.Fingerprint = "another-identity"
			case "missing_snapshot":
				saved.Snapshot = nil
			}
			blob, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if invalid == "malformed" {
				blob = []byte("invalid-json")
			}
			store.rows[thread]["planner"] = blob
			var port StageCheckpointStore = store
			if invalid == "unknown_stage" {
				port = checkpointResumeOverride{StageCheckpointStore: store, stage: "unregistered-stage"}
			}
			counts := newCounts()
			cycle := newStageSet(store, counts, map[string]string{}, "").cycle
			cycle.SetCheckpointStore(port)
			decision, err := cycle.RunOnce(ctx, payload, agent)
			if err != nil || decision == nil || decision.Outcome == InferenceFailed {
				t.Fatalf("不兼容快照阻断安全全跑：%+v %v", decision, err)
			}
			for _, stage := range testCheckpointOrder {
				if *counts[stage] != 1 {
					t.Fatalf("旧版或无效快照跳过当前阶段：%s %d", stage, *counts[stage])
				}
			}
		})
	}
}

type checkpointResumeOverride struct {
	StageCheckpointStore
	stage string
}

func (s checkpointResumeOverride) LoadResume(ctx context.Context, thread string) (string, json.RawMessage, error) {
	_, blob, err := s.StageCheckpointStore.LoadResume(ctx, thread)
	return s.stage, blob, err
}

func TestCheckpointRecoveredGateFailurePreservesSuccess(t *testing.T) {
	for _, failure := range []string{"error", "timeout", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			ctx, payload, agent := checkpointScopedInputs()
			store := newFakeCheckpointStore(true)
			if !runRecovering(func() {
				_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, agent)
			}) {
				t.Fatal("未模拟中断")
			}
			thread, err := ScopedCheckpointThreadID(ctx, payload, agent)
			if err != nil {
				t.Fatal(err)
			}
			var before checkpointState
			if err := json.Unmarshal(store.rows[thread]["planner"], &before); err != nil {
				t.Fatal(err)
			}
			beforeSnapshot, _ := json.Marshal(before.Snapshot)
			gateBefore := append([]byte(nil), store.rows[thread]["gatekeeper"]...)
			counts := newCounts()
			cycle := newStageSet(store, counts, map[string]string{}, "").cycle
			cause := errors.New("current gate rejected execution")
			if failure == "timeout" {
				cause = context.DeadlineExceeded
			}
			if failure == "canceled" {
				cause = context.Canceled
			}
			cycle.GatekeeperStage = outcomeStage{"gatekeeper", func(context.Context, *InferenceContext) StageResult {
				*counts["gatekeeper"]++
				return StageResult{Error: cause}
			}}
			decision, err := cycle.RunOnce(ctx, payload, agent)
			if !errors.Is(err, cause) || decision.Outcome != InferenceFailed || decision.FailedStage != "gatekeeper" || *counts["gatekeeper"] != 1 || *counts["reviewer"] != 0 || decision.Publishable || decision.Plan != nil {
				t.Fatalf("恢复安全门失败仍继续：decision=%+v err=%v", decision, err)
			}
			var after checkpointState
			if err := json.Unmarshal(store.rows[thread]["planner"], &after); err != nil {
				t.Fatal(err)
			}
			afterSnapshot, _ := json.Marshal(after.Snapshot)
			if string(beforeSnapshot) != string(afterSnapshot) || after.Done || after.LastAttempt == nil || after.LastAttempt.FailedStage != "gatekeeper" || string(store.rows[thread]["gatekeeper"]) != string(gateBefore) {
				t.Fatalf("安全门重检污染成功快照或倒退游标：%+v", after)
			}
		})
	}
}

func TestCheckpointCurrentGateMustRunRed(t *testing.T) {
	ctx, payload, agent := checkpointScopedInputs()
	store := newFakeCheckpointStore(true)
	if !runRecovering(func() {
		_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, agent)
	}) {
		t.Fatal("未模拟中断")
	}
	counts := newCounts()
	cycle := newStageSet(store, counts, map[string]string{}, "").cycle
	cycle.GatekeeperStage = &countingStage{name: "gatekeeper", hits: counts["gatekeeper"], onRun: func(ic *InferenceContext) {
		ic.Decision.HandoffToHuman, ic.Decision.HandoffReason = true, "current safety gate"
	}}
	decision, err := cycle.RunOnce(ctx, payload, agent)
	if err != nil {
		t.Fatal(err)
	}
	if *counts["gatekeeper"] != 1 || !decision.HandoffToHuman || *counts["reviewer"] != 0 {
		t.Fatalf("恢复绕过当前安全门：gatekeeper=%d reviewer=%d handoff=%v", *counts["gatekeeper"], *counts["reviewer"], decision.HandoffToHuman)
	}
}

func TestCheckpointScopedStreamRechecksCurrentPolicy(t *testing.T) {
	for _, mode := range []string{"success", "handoff", "gate_error", "review_error", "review_rejected"} {
		t.Run(mode, func(t *testing.T) {
			ctx, payload, agent := checkpointScopedInputs()
			store := newFakeCheckpointStore(true)
			if !runRecovering(func() {
				_, _ = newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle.RunOnce(ctx, payload, agent)
			}) {
				t.Fatal("未模拟中断")
			}
			counts := newCounts()
			cycle := newStageSet(store, counts, map[string]string{}, "").cycle
			cause := errors.New("current stream safety failure")
			switch mode {
			case "handoff", "gate_error":
				cycle.GatekeeperStage = outcomeStage{"gatekeeper", func(_ context.Context, ic *InferenceContext) StageResult {
					*counts["gatekeeper"]++
					if mode == "gate_error" {
						return StageResult{Error: cause}
					}
					ic.Decision.HandoffToHuman, ic.Decision.HandoffReason = true, "current stream policy"
					return StopResult(&ic.Decision)
				}}
			case "review_error", "review_rejected":
				cycle.ReviewerStage = outcomeStage{"reviewer", func(_ context.Context, ic *InferenceContext) StageResult {
					*counts["reviewer"]++
					if mode == "review_error" {
						return StageResult{Error: cause}
					}
					ic.Decision.Review = &ReviewResult{Passed: false}
					return ContinueResult()
				}}
			}
			handler := NewBufferedStreamHandler(nil)
			decision, err := cycle.RunOnceStream(ctx, payload, agent, handler)
			if *counts["perception"] != 0 || *counts["planner"] != 0 || *counts["gatekeeper"] != 1 {
				t.Fatalf("流式恢复绕过当轮策略或重做成功阶段：%v", counts)
			}
			completes, failures, outcomes := 0, 0, 0
			for _, event := range handler.Events() {
				switch event.Type {
				case StreamEventComplete:
					completes++
				case StreamEventError:
					failures++
				case StreamEventOutcome:
					outcomes++
				}
			}
			if outcomes != 1 {
				t.Fatalf("缺少唯一终态事件：%v", handler.Events())
			}
			switch mode {
			case "success":
				if err != nil || decision.Outcome != InferenceSucceeded || *counts["reviewer"] != 1 || completes != 1 || failures != 0 {
					t.Fatalf("流式安全恢复失败：%+v %v", decision, err)
				}
			case "handoff":
				if err != nil || !decision.HandoffToHuman || *counts["reviewer"] != 0 || completes != 1 {
					t.Fatalf("流式交接绕过当前策略：%+v %v", decision, err)
				}
			case "gate_error", "review_error":
				if !errors.Is(err, cause) || decision.Outcome != InferenceFailed || decision.Publishable || completes != 0 || failures != 1 {
					t.Fatalf("流式故障仍成功完成：%+v %v", decision, err)
				}
			case "review_rejected":
				if err != nil || decision.Outcome != InferenceRejected || decision.Publishable || completes != 0 {
					t.Fatalf("流式复核拒绝仍成功完成：%+v %v", decision, err)
				}
			}
		})
	}
}

// 安全重检由调度器中的门禁/复核位置决定，不能依赖可替换阶段的自报名称。
func TestCheckpointRenamedSafetyStageStillRechecks(t *testing.T) {
	ctx, payload, agent := checkpointScopedInputs()
	store := newFakeCheckpointStore(true)
	cycle := newStageSet(store, newCounts(), map[string]string{}, "reviewer").cycle
	cycle.GatekeeperStage = &countingStage{name: "configured_safety_gate", hits: new(int)}
	if !runRecovering(func() { _, _ = cycle.RunOnce(ctx, payload, agent) }) {
		t.Fatal("未模拟到复核前中断")
	}
	gateHits, reviewerHits := 0, 0
	recovered := newStageSet(store, newCounts(), map[string]string{}, "").cycle
	recovered.GatekeeperStage = &countingStage{name: "configured_safety_gate", hits: &gateHits, onRun: func(ic *InferenceContext) { ic.Decision.HandoffToHuman = true }}
	recovered.ReviewerStage = &countingStage{name: "reviewer", hits: &reviewerHits}
	decision, err := recovered.RunOnce(ctx, payload, agent)
	if err != nil || gateHits != 1 || !decision.HandoffToHuman || reviewerHits != 0 {
		t.Fatalf("自定义安全门名称绕过重检：gate=%d reviewer=%d decision=%+v err=%v", gateHits, reviewerHits, decision, err)
	}
}

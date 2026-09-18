// inference_checkpoint_test.go T-P1-01 阶段边界断点续跑挂载的行为锁定。
//
// 三条主断言对应卡片验收：
//
//	①进程被杀后重启（这里 = 换一个 InferenceCycle 实例、同一 store、同一载荷重试）
//	  从阶段边界续跑，已完成阶段不再执行、其产出从快照回填；
//	②开关关闭时执行序列与"未挂载"完全一致，且不产生任何一次存储调用；
//	③已完整结束的运行不参与续跑（否则会把上一轮的阶段产出喂给新一次推理）。
package agent_runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// testCheckpointOrder 与 service.AgentStageNames 同序（测试内不跨包引，避免 service↔runtime 环）
var testCheckpointOrder = []string{"perception", "alignment", "gatekeeper", "planner", "reviewer"}

type countingStage struct {
	name  string
	hits  *int
	see   func(ic *InferenceContext)
	onRun func(ic *InferenceContext)
	boom  bool
}

func (s *countingStage) Name() string { return s.name }

func (s *countingStage) Execute(_ context.Context, ic *InferenceContext) StageResult {
	*s.hits++
	if s.see != nil {
		s.see(ic)
	}
	if s.onRun != nil {
		s.onRun(ic)
	}
	if s.boom {
		panic("simulated kill during " + s.name)
	}
	return ContinueResult()
}

type fakeCheckpointStore struct {
	mu        sync.Mutex
	enabled   bool
	rows      map[string]map[string][]byte
	saves     int
	loadCalls int
}

func newFakeCheckpointStore(enabled bool) *fakeCheckpointStore {
	return &fakeCheckpointStore{enabled: enabled, rows: map[string]map[string][]byte{}}
}

func (f *fakeCheckpointStore) Enabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled
}

func (f *fakeCheckpointStore) Save(_ context.Context, threadID, stage string, state json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.rows[threadID] == nil {
		f.rows[threadID] = map[string][]byte{}
	}
	f.rows[threadID][stage] = append([]byte(nil), state...)
	return nil
}

// LoadResume 复刻生产语义：游标 = 阶段序号最大的一行，续跑点 = 其下一个阶段。
func (f *fakeCheckpointStore) LoadResume(_ context.Context, threadID string) (string, json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCalls++
	best, bestIdx := "", -1
	for stage := range f.rows[threadID] {
		for i, name := range testCheckpointOrder {
			if name == stage && i > bestIdx {
				best, bestIdx = stage, i
			}
		}
	}
	if best == "" {
		return "", nil, nil
	}
	next := best
	if bestIdx+1 < len(testCheckpointOrder) {
		next = testCheckpointOrder[bestIdx+1]
	}
	return next, json.RawMessage(f.rows[threadID][best]), nil
}

func (f *fakeCheckpointStore) doneAt(threadID, stage string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	blob, ok := f.rows[threadID][stage]
	if !ok {
		return false
	}
	var st checkpointState
	return json.Unmarshal(blob, &st) == nil && st.Done
}

func (f *fakeCheckpointStore) stageCount(threadID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows[threadID])
}

func newCounts() map[string]*int {
	m := make(map[string]*int, len(testCheckpointOrder))
	for _, n := range testCheckpointOrder {
		v := 0
		m[n] = &v
	}
	return m
}

// stageSet 五阶段计数桩。crashIn 指定的阶段执行时 panic（模拟进程被杀）。
type stageSet struct {
	cycle *InferenceCycle
}

func newStageSet(store StageCheckpointStore, counts map[string]*int, seen map[string]string, crashIn string) *stageSet {
	mk := func(name string, onRun func(*InferenceContext)) *countingStage {
		return &countingStage{
			name: name,
			hits: counts[name],
			see: func(ic *InferenceContext) {
				seen[name] = fmt.Sprintf("%v/%v", ic.Sentiment.Label, ic.Decision.StopReason)
			},
			onRun: onRun,
			boom:  crashIn == name,
		}
	}
	perception := mk("perception", func(ic *InferenceContext) {
		ic.Sentiment = SentimentScore{Label: SentimentAngry, Score: 0.9}
		ic.Intent = IntentResult{Primary: IntentRefund, Score: 0.8}
	})
	alignment := mk("alignment", func(ic *InferenceContext) {
		ic.Alignment = AlignmentScore{Empathy: 4, Enthusiasm: 3, Expertise: 5, Patience: 4, Clarity: 3, Politeness: 4}
	})
	gatekeeper := mk("gatekeeper", func(ic *InferenceContext) {
		ic.Crisis = CrisisSignal{Level: CrisisMedium, Reason: "refund-delay"}
		// 只有走快照回填才可能在 planner 入口看到这个值
		ic.Decision.StopReason = "from-gatekeeper"
	})
	planner := mk("planner", func(ic *InferenceContext) {
		ic.Plan = &ActionPlan{PlanType: "reply", Confidence: 0.7}
	})
	reviewer := mk("reviewer", func(ic *InferenceContext) {
		ic.Decision.Reply = "已为您加急核查退款进度"
	})

	c := NewInferenceCycleWithStages(perception, alignment, gatekeeper, planner)
	c.SetReviewerStage(reviewer)
	if store != nil {
		c.SetCheckpointStore(store)
	}
	return &stageSet{cycle: c}
}

func testPayload() CustomerMessagePayload {
	return CustomerMessagePayload{
		ChannelType: "xiaohongshu",
		CustomerID:  "cust-ckpt",
		SessionID:   "sess-ckpt",
		Content:     "退款怎么还没到账",
		MessageType: "text",
		TraceID:     "trace-run-1",
	}
}

func runRecovering(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return
}

// ①崩溃重启：已完成阶段不重跑，产出从快照回填
func TestInferenceCycle_CheckpointResumesAfterCrash(t *testing.T) {
	store := newFakeCheckpointStore(true)
	payload := testPayload()

	crashed := newCounts()
	crashSeen := map[string]string{}
	if !runRecovering(func() {
		_, _ = newStageSet(store, crashed, crashSeen, "planner").cycle.RunOnce(context.Background(), payload, nil)
	}) {
		t.Fatal("planner 桩应触发 panic（否则本用例没有模拟到进程被杀）")
	}
	for _, done := range []string{"perception", "alignment", "gatekeeper"} {
		if *crashed[done] != 1 {
			t.Fatalf("崩溃前 %s 应执行 1 次，got %d", done, *crashed[done])
		}
	}
	if *crashed["reviewer"] != 0 {
		t.Fatalf("崩溃点之后的阶段不应执行，reviewer=%d", *crashed["reviewer"])
	}
	thread := CheckpointThreadID(payload)
	if store.stageCount(thread) != 3 {
		t.Fatalf("崩溃前应落 3 个阶段边界点，got %d", store.stageCount(thread))
	}
	if store.doneAt(thread, "gatekeeper") {
		t.Fatal("被中断的运行不得标 done")
	}

	// 重启后的新一轮：全新 cycle 实例 + 同一 store + 同一载荷
	resumed := newCounts()
	resumeSeen := map[string]string{}
	decision, err := newStageSet(store, resumed, resumeSeen, "").cycle.RunOnce(context.Background(), payload, nil)
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	for _, done := range []string{"perception", "alignment", "gatekeeper"} {
		if *resumed[done] != 0 {
			t.Errorf("已完成阶段 %s 被重复执行（%d 次）—— 违反阶段边界恢复", done, *resumed[done])
		}
	}
	if *resumed["planner"] != 1 || *resumed["reviewer"] != 1 {
		t.Errorf("应从 planner 起续跑：planner=%d reviewer=%d", *resumed["planner"], *resumed["reviewer"])
	}
	if !store.doneAt(thread, "reviewer") {
		t.Error("续跑完成后应把最后一个阶段改写为 done")
	}
	// 快照回填实证：planner 入口看到被杀那一轮感知/门禁的产出（本轮这些阶段没跑过）
	if want := "angry/from-gatekeeper"; resumeSeen["planner"] != want {
		t.Errorf("planner 入口状态应为 %q，got %q（说明快照未回填 Sentiment 或 Decision）", want, resumeSeen["planner"])
	}
	if decision.Reply != "已为您加急核查退款进度" || decision.Sentiment.Label != SentimentAngry {
		t.Errorf("最终决策不完整: %+v", decision)
	}
}

// ②开关关闭：执行序列与未挂载一致，且零存储调用
func TestInferenceCycle_CheckpointDisabledTouchesNoStore(t *testing.T) {
	store := newFakeCheckpointStore(false)
	payload := testPayload()

	baseline := newCounts()
	baseSeen := map[string]string{}
	if _, err := newStageSet(nil, baseline, baseSeen, "").cycle.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("baseline run failed: %v", err)
	}

	gated := newCounts()
	gatedSeen := map[string]string{}
	if _, err := newStageSet(store, gated, gatedSeen, "").cycle.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("gated run failed: %v", err)
	}
	if store.saves != 0 || store.loadCalls != 0 {
		t.Fatalf("开关关闭时不得有任何存储调用：saves=%d loads=%d", store.saves, store.loadCalls)
	}
	for _, n := range testCheckpointOrder {
		if *gated[n] != *baseline[n] {
			t.Errorf("阶段 %s 执行次数与未挂载基线不一致：%d vs %d", n, *gated[n], *baseline[n])
		}
	}
	if gatedSeen["reviewer"] != baseSeen["reviewer"] {
		t.Errorf("阶段产出序列与基线不一致：%q vs %q", gatedSeen["reviewer"], baseSeen["reviewer"])
	}
}

// ③已完整结束的运行不续跑：同一载荷重试必须整体重跑
func TestInferenceCycle_CompletedRunIsNotResumed(t *testing.T) {
	store := newFakeCheckpointStore(true)
	payload := testPayload()

	first := newCounts()
	if _, err := newStageSet(store, first, map[string]string{}, "").cycle.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	second := newCounts()
	if _, err := newStageSet(store, second, map[string]string{}, "").cycle.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	for _, n := range testCheckpointOrder {
		if *first[n] != 1 || *second[n] != 1 {
			t.Errorf("done 运行后的重试应整体重跑（每阶段每轮 1 次），%s first=%d second=%d", n, *first[n], *second[n])
		}
	}
}

// 同会话不同消息不得命中彼此的游标（内容寻址 thread 的意义所在）
func TestInferenceCycle_DifferentMessageUsesFreshThread(t *testing.T) {
	store := newFakeCheckpointStore(true)
	payload := testPayload()

	crashed := newCounts()
	runRecovering(func() {
		_, _ = newStageSet(store, crashed, map[string]string{}, "planner").cycle.RunOnce(context.Background(), payload, nil)
	})

	other := testPayload()
	other.Content = "换个问题：你们支持开发票吗"
	other.TraceID = "trace-run-2"
	fresh := newCounts()
	if _, err := newStageSet(store, fresh, map[string]string{}, "").cycle.RunOnce(context.Background(), other, nil); err != nil {
		t.Fatalf("fresh run failed: %v", err)
	}
	for _, n := range testCheckpointOrder {
		if *fresh[n] != 1 {
			t.Errorf("不同载荷应全新执行，%s got %d 次（0 = 被上一条消息的游标污染）", n, *fresh[n])
		}
	}
	if CheckpointThreadID(payload) == CheckpointThreadID(other) {
		t.Error("不同内容应得到不同 thread")
	}
	if CheckpointThreadID(payload) != CheckpointThreadID(testPayload()) {
		t.Error("同内容应得到同一 thread（重试才能续跑）；trace 不同也不应改变")
	}
}

// 提前返回（门禁转人工）也算运行结束：留 done，后续重试不续跑到半程
func TestInferenceCycle_EarlyReturnMarksDone(t *testing.T) {
	store := newFakeCheckpointStore(true)
	payload := testPayload()

	counts := newCounts()
	perception := &countingStage{name: "perception", hits: counts["perception"]}
	early := &earlyReturnStage{name: "alignment", hits: counts["alignment"]}
	gatekeeper := &countingStage{name: "gatekeeper", hits: counts["gatekeeper"]}
	planner := &countingStage{name: "planner", hits: counts["planner"]}
	reviewer := &countingStage{name: "reviewer", hits: counts["reviewer"]}

	c := NewInferenceCycleWithStages(perception, early, gatekeeper, planner)
	c.SetReviewerStage(reviewer)
	c.SetCheckpointStore(store)
	if _, err := c.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("early return run failed: %v", err)
	}
	thread := CheckpointThreadID(payload)
	if !store.doneAt(thread, "perception") {
		t.Fatal("在 perception 之后提前结束，应把 perception 标 done")
	}
	if store.stageCount(thread) != 1 {
		t.Fatalf("只应落 1 个点（perception），got %d", store.stageCount(thread))
	}
	if *counts["gatekeeper"] != 0 || *counts["planner"] != 0 {
		t.Error("提前返回后不得继续执行后续阶段")
	}
}

type earlyReturnStage struct {
	name string
	hits *int
}

func (s *earlyReturnStage) Name() string { return s.name }

func (s *earlyReturnStage) Execute(_ context.Context, ic *InferenceContext) StageResult {
	*s.hits++
	ic.Decision.HandoffToHuman = true
	ic.Decision.StopReason = "manual"
	return StopResult(&ic.Decision)
}

// 快照只装阶段产出：EpisodicMemory（prompt 级体积）与 Payload 不入库
func TestCheckpointSnapshotExcludesVolatileFields(t *testing.T) {
	ic := &InferenceContext{
		Payload:        testPayload(),
		EpisodicMemory: "一大段情境记忆 prompt",
		Sentiment:      SentimentScore{Label: SentimentAngry},
		Stages:         []StageDecision{{Stage: "perception"}},
	}
	blob, err := json.Marshal(snapshotOf(ic))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	for _, unwanted := range []string{"episodic_memory", "一大段情境记忆", "cust-ckpt", "payload"} {
		if strings.Contains(string(blob), unwanted) {
			t.Errorf("快照不应包含 %q，got %s", unwanted, blob)
		}
	}

	restored := &InferenceContext{}
	applySnapshot(restored, snapshotOf(ic))
	if restored.Sentiment.Label != SentimentAngry || len(restored.Stages) != 1 {
		t.Errorf("回填不完整: %+v", restored)
	}
	if restored.EpisodicMemory != "" {
		t.Error("EpisodicMemory 不应由快照提供")
	}
}

// 快照版本不符（老版本遗留行）时不得半程续跑
func TestCheckpointSnapshotVersionMismatchFallsBackToFullRun(t *testing.T) {
	store := newFakeCheckpointStore(true)
	payload := testPayload()
	thread := CheckpointThreadID(payload)

	stale, err := json.Marshal(checkpointState{Version: 0, Fingerprint: "nope", Snapshot: &checkpointContext{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), thread, "gatekeeper", stale); err != nil {
		t.Fatal(err)
	}

	counts := newCounts()
	if _, err := newStageSet(store, counts, map[string]string{}, "").cycle.RunOnce(context.Background(), payload, nil); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	for _, n := range testCheckpointOrder {
		if *counts[n] != 1 {
			t.Errorf("指纹/版本不匹配应整体重跑，%s got %d", n, *counts[n])
		}
	}
}

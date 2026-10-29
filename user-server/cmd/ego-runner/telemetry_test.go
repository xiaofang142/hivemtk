package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// S-9 runner 侧遥测：环形缓冲纪律 + 上报语义。
//
// 口径红线（与 internal/egolite/telemetry.go 双端同源，样例钉死）：
//   - 样本字段是**闭集**——遥测样本序列化后不得出现页面内容类键；
//   - 环形有界：满丢最旧，长期断网最多少记，绝不影响执行主流程；
//   - 上报失败整批放回（保序），成功清空。

func TestTelemetryRingFIFOAndDrain(t *testing.T) {
	r := newTelemetryRing(4)
	for i := 0; i < 3; i++ {
		if n := r.record(telemetrySample{Kind: "run"}); n != 0 {
			t.Fatalf("未满环不应逐出，实得 %d", n)
		}
	}
	got := r.drain()
	if len(got) != 3 {
		t.Fatalf("drain 应取出全部 3 条，实得 %d", len(got))
	}
	if r.len() != 0 {
		t.Fatal("drain 后缓冲应清空")
	}
	// drain 后可继续入队（复用而非报废）
	r.record(telemetrySample{Kind: "sync"})
	if r.len() != 1 {
		t.Fatalf("drain 后继续入队，实长 %d", r.len())
	}
}

func TestTelemetryRingEvictsOldestWhenFull(t *testing.T) {
	r := newTelemetryRing(2)
	r.record(telemetrySample{Kind: "run", SkillID: "a"})
	r.record(telemetrySample{Kind: "run", SkillID: "b"})
	if n := r.record(telemetrySample{Kind: "run", SkillID: "c"}); n != 1 {
		t.Fatalf("环满再入队应逐出 1 条，实得 %d", n)
	}
	got := r.drain()
	if len(got) != 2 || got[0].SkillID != "b" || got[1].SkillID != "c" {
		t.Fatalf("应保序只剩 b,c（最旧 a 被丢），实得 %+v", got)
	}
}

func TestTelemetrySampleShapeIsClosedSet(t *testing.T) {
	// 口径红线：样本序列化后的键集必须 ⊆ 服务端白名单——任何"顺手多带一个字段"
	// 都会让页面内容类数据有通道。这里是结构级守卫，不是靠自觉。
	s := telemetrySample{Kind: "run", At: "2026-10-29T00:00:00Z", DurationMS: 12,
		OK: true, SkillID: "my-flow", ErrorClass: "", RunnerID: "dev-uuid"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"kind": true, "at": true, "duration_ms": true, "ok": true,
		"skill_id": true, "error_class": true, "runner_id": true,
	}
	for k := range m {
		if !allowed[k] {
			t.Errorf("遥测样本出现白名单外的键 %q（采集面必须闭集）", k)
		}
	}
	// 禁止面自检：页面内容类键永远不该出现在样本结构里（编译期就该不可能，
	// 这里再从序列化面钉一次）
	for _, forbidden := range []string{"page", "html", "text", "url", "content"} {
		if _, ok := m[forbidden]; ok {
			t.Errorf("禁止键 %q 出现在遥测样本", forbidden)
		}
	}
}

func TestFlushTelemetryBatchingAndRetry(t *testing.T) {
	telRing = newTelemetryRing(telemetryRingCap) // 隔离全局态
	for i := 0; i < 3; i++ {
		telRing.record(telemetrySample{Kind: "run"})
	}

	var mu sync.Mutex
	var posts int
	prev := flushTelemetryPost
	defer func() { flushTelemetryPost = prev }()

	// 先失败：整批放回，缓冲不丢
	flushTelemetryPost = func(c runnerConf, body []byte) error {
		mu.Lock()
		posts++
		mu.Unlock()
		return errors.New("server down")
	}
	flushTelemetry(runnerConf{ServerURL: "ws://127.0.0.1:1"})
	if got := telRing.len(); got != 3 {
		t.Fatalf("上报失败应整批放回，缓冲实长 %d", got)
	}

	// 再成功：清空
	flushTelemetryPost = func(c runnerConf, body []byte) error {
		mu.Lock()
		posts++
		mu.Unlock()
		// 上报体形状自检：{"events":[...]} 且 events 是数组
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil || m["events"] == nil {
			t.Errorf("上报体形状错误: %s", body)
		}
		return nil
	}
	flushTelemetry(runnerConf{ServerURL: "ws://127.0.0.1:1"})
	if got := telRing.len(); got != 0 {
		t.Fatalf("上报成功后缓冲应清空，实长 %d", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts < 2 {
		t.Fatalf("应至少两次 POST（一次失败一次成功），实得 %d", posts)
	}
}

func TestRecordTelemetryDefaults(t *testing.T) {
	telRing = newTelemetryRing(telemetryRingCap)
	confSnapshot = runnerConf{DeviceID: "dev-x"}
	recordTelemetry("run", 1500000, false, "", "") // 1.5s 失败、无错误码
	got := telRing.drain()
	if len(got) != 1 {
		t.Fatalf("应记一条，实得 %d", len(got))
	}
	s := got[0]
	if s.ErrorClass != "unknown" {
		t.Errorf("失败样本无错误码应归 unknown，实得 %q", s.ErrorClass)
	}
	if s.DurationMS != 1 {
		t.Errorf("耗时毫秒取整应=1，实得 %d", s.DurationMS)
	}
	if s.RunnerID != "dev-x" || s.At == "" {
		t.Errorf("runner_id/at 必须自动补全: %+v", s)
	}
	if strings.Contains(s.At, "local") {
		t.Errorf("at 必须 UTC RFC3339，实得 %q", s.At)
	}
}

func TestFlushTelemetryChunksOverMaxPerPost(t *testing.T) {
	telRing = newTelemetryRing(telemetryRingCap)
	// 环容量 512 > 单次上限 500：满环一次 flush 至少拆两包
	for i := 0; i < telemetryRingCap; i++ {
		telRing.record(telemetrySample{Kind: "sync"})
	}
	var sizes []int
	prev := flushTelemetryPost
	defer func() { flushTelemetryPost = prev }()
	flushTelemetryPost = func(c runnerConf, body []byte) error {
		var m struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Errorf("坏上报体: %v", err)
		}
		sizes = append(sizes, len(m.Events))
		return nil
	}
	flushTelemetry(runnerConf{ServerURL: "ws://127.0.0.1:1"})
	if len(sizes) < 2 {
		t.Fatalf("512 条应拆 ≥2 包，实得 %v", sizes)
	}
	for _, n := range sizes {
		if n > telemetryMaxPerPost {
			t.Errorf("单包 %d 条超过上限 %d", n, telemetryMaxPerPost)
		}
	}
	if got := telRing.len(); got != 0 {
		t.Fatalf("全部成功后缓冲应清空，实长 %d", got)
	}
}

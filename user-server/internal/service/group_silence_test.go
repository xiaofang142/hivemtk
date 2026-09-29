package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/event"
)

// TestDetectGroupSilence_Boundaries 三档判定边界：47/71/73/167/169 小时 × 最后发言人是否 bot
func TestDetectGroupSilence_Boundaries(t *testing.T) {
	base := time.Now()
	at := func(hours float64) time.Time { return base.Add(-time.Duration(hours * float64(time.Hour))) }

	cases := []struct {
		name       string
		lastNonBot time.Time
		lastIsBot  bool
		want       string
	}{
		{"47h_非bot_空", at(47), false, ""},
		{"47h_末bot_空", at(47), true, ""},
		{"71h_非bot_空", at(71), false, ""},
		{"71h_末bot_空", at(71), true, ""},
		{"73h_非bot_空", at(73), false, ""},
		{"73h_末bot_revive", at(73), true, GroupReviveCandidate},
		{"167h_非bot_空", at(167), false, ""},
		{"167h_末bot_revive", at(167), true, GroupReviveCandidate},
		{"169h_非bot_dead", at(169), false, GroupDeadVerdict},
		{"169h_末bot_dead优先", at(169), true, GroupDeadVerdict},
	}
	for _, c := range cases {
		if got := DetectGroupSilence(c.lastNonBot, c.lastIsBot, base); got != c.want {
			t.Errorf("%s: DetectGroupSilence = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDetectGroupSilence_ThresholdEdge 恰好等于阈值时不算（严格大于）
func TestDetectGroupSilence_ThresholdEdge(t *testing.T) {
	now := time.Now()
	if got := DetectGroupSilence(now.Add(-GroupSilenceReviveAfter), true, now); got != "" {
		t.Errorf("恰好 72h 应返回空档, got %q", got)
	}
	if got := DetectGroupSilence(now.Add(-GroupSilenceDeadAfter), false, now); got != "" {
		t.Errorf("恰好 168h 未超过阈值（严格大于）应返回空档, got %q", got)
	}
}

// TestBuildReviveVerdict 建议字段为枚举而非营销话术；仅 revive_candidate 档给建议
func TestBuildReviveVerdict(t *testing.T) {
	now := time.Now()

	v := BuildReviveVerdict("grp_001", GroupReviveCandidate, 80.5, now)
	if v.SignalKey != GroupReviveSignal || GroupReviveSignal != "group_revive" {
		t.Fatalf("SignalKey = %s, want group_revive", v.SignalKey)
	}
	if v.GroupID != "grp_001" || v.Verdict != GroupReviveCandidate || v.SilenceHours != 80.5 {
		t.Fatalf("字段回填不符: %+v", v)
	}
	want := []string{"value_content", "poll", "help_recap"}
	if len(v.Suggestions) != len(want) {
		t.Fatalf("revive_candidate 建议数 = %d, want %d", len(v.Suggestions), len(want))
	}
	for i, s := range v.Suggestions {
		if s != want[i] {
			t.Fatalf("建议[%d] = %s, want %s", i, s, want[i])
		}
	}

	d := BuildReviveVerdict("grp_002", GroupDeadVerdict, 100, now)
	if len(d.Suggestions) != 0 {
		t.Fatal("dead 档不应给复活建议")
	}
	e := BuildReviveVerdict("grp_003", "", 10, now)
	if len(e.Suggestions) != 0 {
		t.Fatal("空档不应给复活建议")
	}
}

// TestEmitGroupRevive_NoBusNoPanic 总线未初始化时发布为 no-op 不 panic
func TestEmitGroupRevive_NoBusNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("未初始化总线时不应 panic: %v", r)
		}
	}()
	EmitGroupRevive(context.Background(), "grp_test", BuildReviveVerdict("grp_test", GroupReviveCandidate, 73, time.Now()))
}

// TestEmitGroupRevive_PublishesToBus 挂接既有事件总线：注册订阅后可收到 group_revive 信号
func TestEmitGroupRevive_PublishesToBus(t *testing.T) {
	bus := event.New(1, 16)
	received := make(chan GroupRevivePayload, 1)
	bus.Subscribe(TopicGroupRevive, func(evt event.Event) error {
		if p, ok := evt.Payload.(GroupRevivePayload); ok {
			received <- p
		}
		return nil
	})
	event.SetGlobalBus(bus)
	t.Cleanup(event.StopGlobal)

	EmitGroupRevive(context.Background(), "grp_x", BuildReviveVerdict("grp_x", GroupReviveCandidate, 80, time.Now()))
	select {
	case p := <-received:
		if p.SignalKey != GroupReviveSignal || p.GroupID != "grp_x" {
			t.Fatalf("载荷不符: %+v", p)
		}
		if p.Verdict != GroupReviveCandidate || p.Detail.Verdict != GroupReviveCandidate {
			t.Fatalf("Verdict = %s, want revive_candidate", p.Verdict)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到群复活事件")
	}
}

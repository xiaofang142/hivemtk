// 批14：写/交互步骤必须把「这次动作走的是哪条通道」落进审计面。
// 立项依据：真机 session 536/537 的取证显示，扩展侧早就回了 channel:cdp / dom_fallback，
// 但服务端 click 只挑走 navigated、type 与 click_near 整个回包直接丢弃（hand 层签名只留 error）。
// 结果： trusted 通道全程失效、每一次点击都在降级 DOM 兜底，而步骤台账一片绿——
// 「降级不可见」和「假绿」在这条链路上是同一件事。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// b14RunSteps 跑一条编排并回读步骤行
func b14RunSteps(t *testing.T, stepsJSON string, reply func(string, map[string]any) (map[string]any, string)) []map[string]any {
	t.Helper()
	exec, _, bundle := newWSE2E(t, reply)
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)
	rows := bundle.steps(t, session.ID)
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		m := map[string]any{}
		if len(rows[i].Result) > 0 {
			if err := json.Unmarshal(rows[i].Result, &m); err != nil {
				t.Fatalf("步 %d result 解析失败: %v（raw=%s）", rows[i].StepIndex, err, string(rows[i].Result))
			}
		}
		out = append(out, m)
	}
	return out
}

func TestB14_ClickStepRecordsChannel(t *testing.T) {
	const stepsJSON = `[{"action":"click","target":"#send"}]`
	res := b14RunSteps(t, stepsJSON, func(action string, _ map[string]any) (map[string]any, string) {
		if action == "click" {
			return map[string]any{"ok": true, "navigated": false, "channel": "cdp"}, ""
		}
		return map[string]any{}, ""
	})
	if len(res) != 1 {
		t.Fatalf("步骤行数=%d want 1", len(res))
	}
	if res[0]["channel"] != "cdp" {
		t.Errorf("click 步未记下 channel=%v（trusted/兜底必须可审）", res[0]["channel"])
	}
}

// 老扩展不回 channel 时只能记 null：把「不知道」写成 cdp 等于给降级兜底发合格证。
func TestB14_UnknownChannelStaysNull(t *testing.T) {
	const stepsJSON = `[{"action":"click","target":"#send"}]`
	res := b14RunSteps(t, stepsJSON, func(action string, _ map[string]any) (map[string]any, string) {
		return map[string]any{"ok": true, "navigated": false}, ""
	})
	v, ok := res[0]["channel"]
	if !ok {
		t.Fatal("channel 键缺失：老扩展也要留下这一问，不能整字段消失")
	}
	if v != nil {
		t.Errorf("channel=%v，未知必须是 null", v)
	}
}

// DOM 兜底必须在台账上看得见——它只该出现在调试器被占的降级场景，
// 一旦成片出现就是 trusted 通道死了（批14 实证正是这个形态）。
func TestB14_DomFallbackChannelVisible(t *testing.T) {
	const stepsJSON = `[{"action":"click","target":"#send"}]`
	res := b14RunSteps(t, stepsJSON, func(action string, _ map[string]any) (map[string]any, string) {
		return map[string]any{"ok": true, "navigated": false, "channel": "dom_fallback"}, ""
	})
	if res[0]["channel"] != "dom_fallback" {
		t.Errorf("channel=%v want dom_fallback", res[0]["channel"])
	}
}

func TestB14_TypeAndClickNearRecordChannel(t *testing.T) {
	const stepsJSON = `[{"action":"type","target":"#input","value":"你好"},{"action":"click_near","anchor":".box","button_text":"发送"}]`
	res := b14RunSteps(t, stepsJSON, func(action string, _ map[string]any) (map[string]any, string) {
		switch action {
		case "type":
			return map[string]any{"ok": true, "editable": true, "channel": "cdp"}, ""
		case "click_near":
			return map[string]any{"ok": true, "clicked": true, "channel": "dom_fallback"}, ""
		}
		return map[string]any{}, ""
	})
	if len(res) != 2 {
		t.Fatalf("步骤行数=%d want 2", len(res))
	}
	if res[0]["channel"] != "cdp" {
		t.Errorf("type 步 channel=%v want cdp（回包被整包丢弃过，这条是回归闸）", res[0]["channel"])
	}
	if res[1]["channel"] != "dom_fallback" {
		t.Errorf("click_near 步 channel=%v want dom_fallback", res[1]["channel"])
	}
}

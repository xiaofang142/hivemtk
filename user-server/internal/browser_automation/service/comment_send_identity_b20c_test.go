package service

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
)

// comment_send_identity_b20c_test.go — 三条 trusted 写通道的最后一处点后身份复核。
//
// 为什么开关/结论都要腿（与 verify_identity_b17_test.go 同理由，此处是第三个消费点）：
// 复核长在 primitives.js 里，Go 只发一个布尔、只读一个字段。没有断言的话，
// ①「Go 从没发过开关」（扩展再健壮也一次都不跑）②「扩展复核了但结论没进审计面」
// 两种世界在库里与「真的复核过」完全同形。comment_send 的代价比 click 高一档：
// 点错一次是一条撤不回的公开评论。
//
// 手法沿用 executor_ws_e2e_test.go：真 WS 帧 + 真测试库，唯一替身是扩展侧。

const commentSendSteps = `[` + openXHS + `,
{"action":"post_comment","value":"测试评论正文"}]`

// runCommentSend 跑一次两段式任务（open_tab → post_comment），返回抓到的帧、步骤行与 result 面。
func runCommentSend(t *testing.T, reply func(action string, frame map[string]any) (map[string]any, string)) (*capturedFrames, model.BrowserStep, map[string]json.RawMessage) {
	t.Helper()
	frames := newCapturedFrames()
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		frames.note(action, frame)
		return reply(action, frame)
	})
	task, session := bundle.seedTask(t, commentSendSteps, false)
	steps, err := ParseSteps([]byte(commentSendSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 1)
	var got map[string]json.RawMessage
	if len(row.Result) > 0 {
		if err := json.Unmarshal(row.Result, &got); err != nil {
			t.Fatalf("step result 不是 JSON 对象: %v（raw=%s）", err, row.Result)
		}
	}
	return frames, row, got
}

// ① 写步的 comment_send 帧必须请求复核：post_comment 恒为写步，这一格没有「读步所以不付」的余地
func TestCommentSendFrameRequestsIdentityRecheck(t *testing.T) {
	frames, row, _ := runCommentSend(t, happyReply)
	if !row.IsWrite {
		t.Fatal("前置不成立：post_comment 没被判成写步，本条成了自证")
	}
	frame, ok := frames.lastOf("comment_send")
	if !ok {
		t.Fatal("扩展侧没收到 comment_send 帧（无法判定复核开关）")
	}
	if got, present := identityFlag(frame); !present || !got {
		t.Fatalf("comment_send 帧 verify_identity=%v present=%v want true——那道闸门的执行侧在扩展，"+
			"请求侧就是这一个布尔：不发出去，真机上一次都不会跑", frame["verify_identity"], present)
	}
}

// ② 复核结论必须进审计面：扩展回 identity_checked=true 就得在落库 result 里看得见；
// 旧扩展不回这个字段时如实记 null，不许洗成 true（click/click_near 同口径）。
func TestCommentSendResultRecordsIdentityChecked(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{"扩展报复核过", map[string]any{"sent": true, "identity_checked": true}, `true`},
		{"旧扩展没这个字段", map[string]any{"sent": true}, `null`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, row, got := runCommentSend(t, func(action string, frame map[string]any) (map[string]any, string) {
				if action == "comment_send" {
					return c.data, ""
				}
				return happyReply(action, frame)
			})
			if row.Status != "success" {
				t.Fatalf("status=%s want success（error=%s）——前置不成立，本条判不了审计面", row.Status, row.ErrorMsg)
			}
			v, ok := got["identity_checked"]
			if !ok {
				t.Fatalf("post_comment 的 result 里没有 identity_checked 这一列（result=%s）——复核跑没跑过在库里无从分辨", row.Result)
			}
			if string(v) != c.want {
				t.Fatalf("identity_checked=%s want %s", v, c.want)
			}
		})
	}
}

// ③ element_moved 从 comment_send 回来时是「点了，但点到的可能不是发送按钮」：
// 动作已经发生，台账必须留下这次尝试，且绝不重发（重发=双发，公开内容不可撤回）。
// 这里让 comment_verify 仍然回 verified=true（评论确实上去了），判据取的是
// 「台账记的是已跨越的态」而不是「步判绿不绿」——绿与不绿由回查裁决，跨没跨由台账裁决，两件事各留痕。
func TestCommentSendElementMovedRecordsCrossedAttempt(t *testing.T) {
	frames, row, got := runCommentSend(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_send" {
			return nil, "element_moved: 落点已被 overlay 接管"
		}
		return happyReply(action, frame)
	})
	attempted := false
	for _, s := range model.StepSubmitAttemptedStates() {
		if row.SubmitState == s {
			attempted = true
		}
	}
	if !attempted {
		t.Fatalf("submit_state=%q 不在「已尝试」集合里：点后身份不过被归因成了「从未发生」，"+
			"下一轮同文本就会绕过双发闸再提交一次", row.SubmitState)
	}
	if n := frames.countOf("comment_send"); n != 1 {
		t.Fatalf("comment_send 到线 %d want 1（未知态绝不重发）", n)
	}
	// 复核没给结论，就不许在审计面上冒充「复核过了」
	if v, ok := got["identity_checked"]; !ok || string(v) != `null` {
		t.Fatalf("identity_checked=%v present=%v want null（element_moved 那次复核给的是结论不是通过）", got["identity_checked"], ok)
	}
}

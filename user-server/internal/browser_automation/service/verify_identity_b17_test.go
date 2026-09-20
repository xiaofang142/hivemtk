package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
)

// verify_identity_b17_test.go — 批17(c)：点后身份复核的**请求侧**在 Go，执行侧在扩展。
//
// 为什么开关也要腿（批14 的 channel 列同一形状）：复核长在 primitives.js 里，Go 只发一个布尔。
// 如果「写步=true / 读步≠true」这两格都没有断言，那么下面三种世界在审计面上完全一样：
// ① 写步真的复核了；② Go 从没发过开关（扩展再健壮也不会跑）；③ 读步也付了这次额外注入
// （每个展开页多一次 evaluate，白付的钱没人记账）。所以帧内容必须当场抓下来判。
//
// 手法沿用 executor_ws_e2e_test.go：真 WS 帧 + 真测试库，唯一替身是扩展侧。

type capturedFrames struct {
	mu    sync.Mutex
	items map[string][]map[string]any
}

func newCapturedFrames() *capturedFrames {
	return &capturedFrames{items: map[string][]map[string]any{}}
}

func (c *capturedFrames) note(action string, frame map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[action] = append(c.items[action], frame)
}

// lastOf 该 action 最后一帧（没有则返回 nil, false）。
func (c *capturedFrames) lastOf(action string) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.items[action]
	if len(list) == 0 {
		return nil, false
	}
	return list[len(list)-1], true
}

func (c *capturedFrames) countOf(action string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items[action])
}

// identityFlag 帧里的复核开关。缺失＝旧版 Go 没这个字段，按「未请求」处置（不是 nil 就当过）。
func identityFlag(frame map[string]any) (got bool, present bool) {
	v, ok := frame["verify_identity"]
	if !ok {
		return false, false
	}
	b, isBool := v.(bool)
	if !isBool {
		// JSON 解出来必是 bool；出现别的类型说明有人往里塞了字符串，判「未请求」比判「猜对」诚实
		return false, true
	}
	return b, true
}

const openXHS = `{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"}`

// placeholderStepTask 两步任务模板：第二步用 "C_ITEM" 占位，由表驱动用例换成被测步骤 JSON。
const placeholderStepTask = `[` + openXHS + `,"C_ITEM"]`

// ① 写步（click 命中平台 send_button 定位）必须请求复核
func TestWriteClickFrameRequestsIdentityRecheck(t *testing.T) {
	frames := newCapturedFrames()
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		frames.note(action, frame)
		return happyReply(action, frame)
	})
	const stepsJSON = `[` + openXHS + `,
{"action":"click","target":"button.submit"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	frame, ok := frames.lastOf("click")
	if !ok {
		t.Fatal("扩展侧没收到 click 帧（无法判定复核开关）")
	}
	if got, present := identityFlag(frame); !present || !got {
		t.Fatalf("写步的 click 帧 verify_identity=%v（present=%v）want true——复核执行侧在扩展，"+
			"请求侧就是这一个布尔：不发出去，批17(b) 那道闸门在真机上一次都不会跑", frame["verify_identity"], present)
	}
}

// ② 读步（同平台、target 不命中发送位）不许请求复核：每次点击多一发注入不是免费的
func TestReadClickFrameDoesNotRequestIdentityRecheck(t *testing.T) {
	frames := newCapturedFrames()
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		frames.note(action, frame)
		return happyReply(action, frame)
	})
	const stepsJSON = `[` + openXHS + `,
{"action":"click","target":"div.like"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 1)
	if row.IsWrite {
		t.Fatal("前置条件不成立：div.like 这一步被判成了写步，②就成了自证")
	}
	frame, ok := frames.lastOf("click")
	if !ok {
		t.Fatal("扩展侧没收到 click 帧")
	}
	if got, _ := identityFlag(frame); got {
		t.Fatal("只读步请求了点后复核——写步专属的开销漏到了读路径，读步的 retries 也会被一次误判的 element_moved 打掉")
	}
}

// ③④ click_near 同样两处消费：写步（button_text 命中平台 send_button_text）请求、读步不请求
func TestClickNearIdentityRecheckFollowsWriteGate(t *testing.T) {
	cases := []struct {
		name    string
		item    string
		want    bool
		isWrite bool
	}{
		{"带平台发送文案=写步要复核", `{"action":"click_near","anchor":"div.comment-box","button_text":"发送"}`, true, true},
		{"无文案=只读步不复核", `{"action":"click_near","anchor":"div.comment-box"}`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frames := newCapturedFrames()
			exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
				frames.note(action, frame)
				return happyReply(action, frame)
			})
			body := strings.Replace(placeholderStepTask, `"C_ITEM"`, c.item, 1)
			task, session := bundle.seedTask(t, body, false)
			steps, err := ParseSteps([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
			defer cancel()
			exec.ExecuteSession(ctx, task, session, steps)

			if row := readStepState(t, bundle, session.ID, 1); row.IsWrite != c.isWrite {
				t.Fatalf("is_write=%v want %v（写闸门判定本身变了 = 这两格不再是对照，断言失去意义）", row.IsWrite, c.isWrite)
			}
			frame, ok := frames.lastOf("click_near")
			if !ok {
				t.Fatal("扩展侧没收到 click_near 帧")
			}
			if got, present := identityFlag(frame); got != c.want || !present {
				t.Fatalf("click_near 帧 verify_identity=%v present=%v want %v", frame["verify_identity"], present, c.want)
			}
		})
	}
}

// ⑤ 复核结论必须进审计面：扩展回了 identity_checked=true，落库那一步的 result 里就得看得见；
// 旧扩展不回这个字段时如实记 null，不能洗成 true（open_tab 的 page_loaded 同口径）。
func TestClickResultRecordsIdentityChecked(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{"扩展报复核过", map[string]any{"navigated": false, "channel": "cdp", "identity_checked": true}, `true`},
		{"旧扩展没这个字段", map[string]any{"navigated": false, "channel": "cdp"}, `null`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
				if action == "click" {
					return c.data, ""
				}
				return happyReply(action, frame)
			})
			const stepsJSON = `[` + openXHS + `,
{"action":"click","target":"button.submit"}]`
			task, session := bundle.seedTask(t, stepsJSON, false)
			steps, err := ParseSteps([]byte(stepsJSON))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
			defer cancel()
			exec.ExecuteSession(ctx, task, session, steps)

			row := readStepState(t, bundle, session.ID, 1)
			var got map[string]json.RawMessage
			if err := json.Unmarshal(row.Result, &got); err != nil {
				t.Fatalf("step result 不是 JSON 对象: %v（raw=%s）", err, row.Result)
			}
			v, ok := got["identity_checked"]
			if !ok {
				t.Fatal("result 里没有 identity_checked 这一列——复核跑没跑过在库里无从分辨")
			}
			if string(v) != c.want {
				t.Fatalf("identity_checked=%s want %s", v, c.want)
			}
		})
	}
}

// ⑥ element_moved 是「点了，但点到的可能不是那个元素」——动作已发生，台账必须留下尝试，
// 且绝不能被 isNeverExecuted 那族（*_not_found / *_inject_timeout_）当成「从未发生」而放行重发。
func TestElementMovedWriteClickRecordsAttempt(t *testing.T) {
	frames := newCapturedFrames()
	exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		frames.note(action, frame)
		if action == "click" {
			return nil, "element_moved: 中心点从 150,220 挪到 230,220"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[` + openXHS + `,
{"action":"click","target":"button.submit"}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 1)
	if row.Status != "failed" {
		t.Fatalf("status=%s want failed", row.Status)
	}
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Fatalf("submit_state=%q want %q——点后身份不过=动作已发生但结果未知，记成「从未发生」就是把重发（双发）留给了下一轮",
			row.SubmitState, model.StepSubmitUnattributed)
	}
	if n := frames.countOf("click"); n != 1 {
		t.Fatalf("click 到线 %d want 1（写步禁重试，未知态不重发）", n)
	}
}

// ⑦ *_not_interactable* 与 element_moved 恰好相反：它是**坐标还没离开扩展**时给出的结论
// （probe / settle / hit-target 全在派发之前），所以台账不许记成提交尝试。
//
// 立项证据（批17 真机腿，DB 实读）：夹具页上那个一直在挪的按钮被 stable 拦下，步 2287
// 落成 submit_state=unattributed、error=「element_not_interactable: unstable」。
// 而 unattributed 在 StepSubmitAttemptedStates() 里（write_ledger.go:169「归因不到不等于没发生」）
// ⇒ 下一轮同文本直接被双发闸拦死。可这次「没发生」是**查明的**，不是归因不到：
// 把零副作用的拒绝记成待判，等于让闸门误伤掉唯一正确的处置（等页面停下再跑一次）。
// 同形状的浮层遮挡（covered）在批14 起就是这个落点，只是当时没有腿盯着——本条一起钉住。
func TestPreDispatchRefusalRecordsNoAttempt(t *testing.T) {
	cases := []struct{ name, err string }{
		{"stable 拦下（批17 新增）", "element_not_interactable: unstable"},
		{"浮层遮挡（批14 起就有）", "element_not_interactable: covered"},
		{"发送按钮抖动", "send_button_not_interactable: unstable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frames := newCapturedFrames()
			exec, _, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
				frames.note(action, frame)
				if action == "click" {
					return nil, c.err
				}
				return happyReply(action, frame)
			})
			const stepsJSON = `[` + openXHS + `,
{"action":"click","target":"button.submit"}]`
			task, session := bundle.seedTask(t, stepsJSON, false)
			steps, err := ParseSteps([]byte(stepsJSON))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
			defer cancel()
			exec.ExecuteSession(ctx, task, session, steps)

			row := readStepState(t, bundle, session.ID, 1)
			if row.Status != "failed" {
				t.Fatalf("status=%s want failed（这一步确实是失败的，不许为了放行重发把它洗成绿）", row.Status)
			}
			for _, s := range model.StepSubmitAttemptedStates() {
				if row.SubmitState == s {
					t.Fatalf("submit_state=%q 落在「已尝试」集合里：零副作用的派发前拒绝被记成了提交尝试，"+
						"下一轮同文本会被双发闸拦死（%s）", row.SubmitState, c.err)
				}
			}
			if n := frames.countOf("click"); n != 1 {
				t.Fatalf("click 到线 %d want 1", n)
			}
		})
	}
}

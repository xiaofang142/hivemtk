package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

// 扩展侧「提交键派发失败」上抛后，Go 台账必须记成**提交尝试**。
//
// 立项缺陷（第三轮深查）：primitives.js 的 type 分支原先给 pressEnter 挂了空回调吞错，
// Enter 没发出去整步仍回 ok=true。type+submit_on_enter 命中注册评论框时是写步
// （write_ledger.go:279），于是库里是 status=success / submit_state=sent——
// 「评论根本没发出去」这一步既报了绿，又被双发闸永久拦死（唯一正确处置=看清页面再跑一次）。
// 修复面在扩展侧（改为上抛 submit_key_not_dispatched）；这里锁两半：
//   - 行为半：这串错误文案进 isNeverExecuted 必须是 false，且步行落成 unattributed；
//   - 契约半：断言的字符串从扩展源码里读，不写死在本文件里——两边改名时必须有一边红，
//     否则这条腿只是两段各自自证的平行文字。

const submitKeyPrefix = "submit_key_not_dispatched"

// jsPrimitivesPath 扩展源码相对本测试包的位置。仓库根同时含 user-server 与 user-web，
// 门禁的 --shared 克隆也是这个形状；读不到就直接 Fatal，不 skip——静默 skip 的锁等于没锁。
func jsPrimitivesPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "..", "user-web", "browser_automation", "src", "core", "primitives.js")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读扩展源码失败 %s：%v", p, err)
	}
	return string(b)
}

func TestSubmitKeyFailureTokenIsNotNeverExecuted(t *testing.T) {
	src := jsPrimitivesPath(t)
	// 契约半之一：扩展确实还在抛这个前缀（删掉/改名都会在这里红）。
	if !strings.Contains(src, "'"+submitKeyPrefix+": '") {
		t.Fatalf("扩展源码里找不到 %q 的抛出点——两语言契约的另一边断了", submitKeyPrefix)
	}
	// 真机/单测里 pressEnter 的实际失败文案长这样，拼成扩展上抛后的完整错误：
	err := errors.New(submitKeyPrefix + ": Debugger is not attached to the target")
	if isNeverExecuted(err) {
		t.Fatalf("isNeverExecuted(%v)=true：提交键失败被判成「从未发生」⇒ 台账留空 ⇒ 下一轮同文本直接双发", err)
	}
}

func TestWSE2E_SubmitKeyFailureRecordsAttempt(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "type" {
			return nil, submitKeyPrefix + ": Debugger is not attached to the target"
		}
		return happyReply(action, frame)
	})
	const stepsJSON = `[{"action":"open_tab","target":"https://www.xiaohongshu.com/explore"},
{"action":"type","target":".content-textarea","value":"提交键没发出去的评论","submit_on_enter":true}]`
	task, session := bundle.seedTask(t, stepsJSON, false)
	steps, err := ParseSteps([]byte(stepsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	if n := ext.countOf("type"); n != 1 {
		t.Fatalf("type 到线 %d want 1（写步 retries 钳 0，失败也不许自己再来一次）", n)
	}
	row := readStepState(t, bundle, session.ID, 1)
	if !row.IsWrite {
		t.Fatal("is_write 未落库：这一整条台账腿的前提就不成立")
	}
	if row.Status != "failed" {
		t.Errorf("status=%s want failed——提交键没发出去不许报绿（本批要消灭的假绿）", row.Status)
	}
	// 提交键可能已部分送达页面，判不出结局 ⇒ 记尝试、拦重发、交人工核。
	if row.SubmitState != model.StepSubmitUnattributed {
		t.Errorf("submit_state=%s want unattributed（got=%q）", row.SubmitState, model.StepSubmitUnattributed)
	}
	if _, err := bundle.stepRepo.FindSubmitAttempt(ctx, task.ID, row.TextHash, 0); err != nil {
		t.Errorf("这一形状必须算提交尝试（否则下一轮同文本会双发），got err=%v", err)
	}
}

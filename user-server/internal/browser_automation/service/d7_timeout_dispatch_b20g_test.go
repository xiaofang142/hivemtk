package service

// d7_timeout_dispatch_b20g_test.go — §8.2-2 D7 确认超时支路：闸门算了结论，调用方必须听。
//
// 立项理由（注码实证过的失效形态）：把 executor.go dispatchStep 里确认闸门的 default 支路
// 从「返回超时错误」注成「Warnf 一句后继续往下走到 sendOnce」，本包原有腿全套照样绿——
//   - 源码静态锁只钉 "confirmWaitTimedOut：" 与 "case confirmStoppedByUser:" 两处字面量，注码原样保留；
//   - 超时的用例全部直调 awaitConfirmGate 只校验返回值，穿不到 dispatchStep；
//   - 两条 WSE2E 只覆盖 granted 与 stop 两分支。
// 即「判据算得对、消费点没人管」在现有断言下不可见。本腿经真执行入口跑一遍，把确认预算
// 调到 1s 自然到期（执行预算仍宽裕），断三件事：
// ① comment_send 一帧都不下发；② 步/会话判 failed 且错误文本透出 why（是哪条预算到头，
// 否则运维调错旋钮）；③ 台账停在 prepared（可安全重下发）。
// ③ 是 ①② 的反向腿：摘掉 ① 后，注码形态在 ②（status/文案）与 ③（submit_state）上照样红。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
)

func TestWSE2E_D7ConfirmTimeoutVerdictConsumedByCaller(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, happyReply)
	task, session := bundle.seedTask(t, commentSendSteps, true)
	// 确认预算 1s 自己到期：走的就是 confirmWaitTimedOut 那条 default 支路，
	// 且 why 必须是「人工确认等待」而不是「任务执行预算」（两条旋钮各管各的）。
	task.ConfirmWaitSec = 1

	steps, err := ParseSteps([]byte(commentSendSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eExecBudget)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	// ① 闸门判了超时，一帧 comment_send 都不许到线
	if n := ext.countOf("comment_send"); n != 0 {
		t.Fatalf("comment_send 到线 %d want 0——闸门判超时、调用方却照常走完 sendOnce：人工未放行，不可逆提交已跨出", n)
	}
	// ② 步判 failed，且红因透出 why（点名是确认预算到头）
	row := readStepState(t, bundle, session.ID, 1)
	if row.Status != "failed" {
		t.Errorf("步 status=%s want failed——确认超时必须判红，不许静默跳过或判绿", row.Status)
	}
	if !strings.Contains(row.ErrorMsg, "超时") || !strings.Contains(row.ErrorMsg, "人工确认等待") {
		t.Errorf("步红因必须透出超时归因与是哪条预算（why），got %q", row.ErrorMsg)
	}
	got := bundle.reloadSession(t, session.ID)
	if got.Status != "failed" {
		t.Errorf("会话终态=%s（%s）want failed", got.Status, got.ErrorMsg)
	}
	// ③ 台账停在 prepared：prep 是唯一发生过的事 ⇒ 可安全重下发
	if row.SubmitState != model.StepSubmitPrepared {
		t.Errorf("submit_state=%s want %s——「闸门/超时腿从未提交」的正面证据就是台账停在 prepared；"+
			"摘掉 ① 后这一格也必须为注码形态变红（越过提交点必进拦阻集合）",
			row.SubmitState, model.StepSubmitPrepared)
	}
}

package service

// 批14：comment_send 页面侧闸门（send_button_not_interactable）必须归到「点击从未发生」这一类。
// 与注入超时同一待遇的理由：扩展在把坐标交给 CDP 之前就被自己的可点性检查拦下，
// 页面没有收到任何输入 → 台账留在 prepared 才是事实；记成 sent 会把一次「根本没点」
// 说成「可能已发」，从此这条评论既不能安全重下发，也说不清到底发生过什么。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

const sendGateErr = "send_button_not_interactable: covered"

// 1) 谓词本体：只有该 token 命中；定位失效（可自愈）与 WS 超时（结局未知）都不许命中
func TestIsSendGateReject(t *testing.T) {
	if !isSendGateReject(errors.New(sendGateErr)) {
		t.Error("闸门 token 必须识别为「点击从未发生」")
	}
	if isSendGateReject(nil) {
		t.Error("nil 不得命中")
	}
	if isSendGateReject(errors.New("send_button_not_found")) {
		t.Error("按钮没找到是另一类（走自愈重发），不得混进闸门早返")
	}
	if isSendGateReject(errors.New("Host 命令超时（45s，action=comment_send）")) {
		t.Error("WS 超时=结局未知，必须继续走 finalize 回查")
	}
}

// 2) 自愈闸门：A1 不得对这个 token 起效（按钮已找到、只是不可点，换文本重选没有依据）
func TestSendGateNotHealable(t *testing.T) {
	if isSelectorMiss(errors.New(sendGateErr), "send_button_not_found") {
		t.Error("闸门 token 里不含 send_button_not_found，命中即为自愈重发开了口子")
	}
	if !isSelectorMiss(errors.New("send_button_not_found"), "send_button_not_found") {
		t.Error("前提不成立：原有定位失效自愈必须仍然工作")
	}
}

// 3) 端到端：真 WS 帧 + 真库，断的是台账那一行与到线帧数，不是错误字符串
func TestWSE2E_SendGateRejectStaysPrepared(t *testing.T) {
	exec, ext, bundle := newWSE2E(t, func(action string, frame map[string]any) (map[string]any, string) {
		if action == "comment_send" {
			return nil, sendGateErr
		}
		return happyReply(action, frame)
	})
	task, session := bundle.seedTask(t, threeStageSteps, false)
	steps, err := ParseSteps([]byte(threeStageSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec.ExecuteSession(ctx, task, session, steps)

	row := readStepState(t, bundle, session.ID, 3)
	if row.SubmitState != model.StepSubmitPrepared {
		t.Errorf("submit_state=%s want prepared——点击从未发生却记 sent，等于给这条评论永久锁死重下发", row.SubmitState)
	}
	if row.Status != "failed" {
		t.Errorf("status=%s want failed", row.Status)
	}
	if n := ext.countOf("comment_send"); n != 1 {
		t.Errorf("comment_send 到线 %d 次 want 1（闸门腿不得自愈重发）", n)
	}
	if n := ext.countOf("comment_verify"); n != 0 {
		t.Errorf("comment_verify 到线 %d 次 want 0——既然确定没点过，就不该再白轮 finalize", n)
	}
	if !strings.Contains(row.ErrorMsg, "send_button_not_interactable") {
		t.Errorf("失败原因必须带上页面侧 token，got %q", row.ErrorMsg)
	}
	// prepared 不在拦阻集合内：这条腿的重下发合法（点击从未发生），这是早返的全部意义
	if _, err := bundle.stepRepo.FindSubmitAttempt(context.Background(), task.ID, row.TextHash, 999999); !isNotFound(err) {
		t.Errorf("prepared 不该算提交尝试，got err=%v", err)
	}
}

// 4) 静态契约：闸门早返必须排在落 sent 台账之前（顺序错一处，上面的断言就全是摆设）
func TestSendGateOrderedBeforeSentLedger(t *testing.T) {
	src := readSrc(t, "executor.go")
	iSend := strings.Index(src, "e.hand.commentSend(")
	iGate := strings.Index(src, "isSendGateReject(sendErr)")
	// 只锁状态 token、不锁参数尾巴：recordSubmitState 的签名会变（批16 加了 crossed），
	// 锁尾巴等于把断言绑在参数列表上，改签名就假红，而本测试要断的从来只有顺序。
	iSent := strings.Index(src, "model.StepSubmitSent")
	iHeal := strings.Index(src, "e.healCommentSendButton(")
	if !(0 <= iSend && iSend < iGate && iGate < iSent) {
		t.Errorf("顺序必须 send→闸门早返→落 sent，got send=%d gate=%d sent=%d", iSend, iGate, iSent)
	}
	if !(iGate < iHeal) {
		t.Error("闸门早返必须先于自愈调用（否则闸门 token 会走成第二次提交）")
	}
}

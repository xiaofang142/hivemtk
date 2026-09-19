package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

// D7 写操作人工确认闸门契约锁：
// 运行时语义（挂起/放行/中止）用真通道测，编排位置（必须前置于不可逆提交点）用源码静态锁。

func newConfirmExecutor() *Executor {
	return &Executor{
		stopRegistry:    make(map[uint]chan struct{}),
		confirmRegistry: make(map[uint]chan struct{}),
	}
}

// startWaitConfirm 后台发起等待，阻塞到确认已进入挂起态，返回结果通道。
// ctx 由调用方给出（预算解耦用例需要「ctx 活着、确认预算先到」这一支）。
func startWaitConfirm(t *testing.T, e *Executor, ctx context.Context, sessionID uint, wait time.Duration) chan confirmVerdict {
	t.Helper()
	done := make(chan confirmVerdict, 1)
	go func() {
		out, why := e.waitForConfirm(ctx, sessionID, wait)
		done <- confirmVerdict{out, why}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !e.ConfirmPending(sessionID) {
		if time.Now().After(deadline) {
			t.Fatal("waitForConfirm 未进入挂起态")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return done
}

// confirmVerdict 把 waitForConfirm 的双返回值打包进通道。
type confirmVerdict struct {
	outcome confirmOutcome
	why     string
}

// 1) 放行链路：未挂起时 ConfirmPending=false、SignalConfirm=false（不误报命中）；
// 挂起后可放行一次，等待方返回 true，通道随即注销；重复放行返回 false（不 double close）。
func TestSignalConfirmLifecycle(t *testing.T) {
	e := newConfirmExecutor()
	if e.ConfirmPending(7) {
		t.Error("初始不应有待确认")
	}
	if e.SignalConfirm(7) {
		t.Error("无挂起点时放行应返回 false")
	}

	done := startWaitConfirm(t, e, context.Background(), 7, time.Minute)
	if !e.SignalConfirm(7) {
		t.Fatal("挂起中放行应命中")
	}
	select {
	case v := <-done:
		if v.outcome != confirmGranted {
			t.Errorf("放行后出路=%v want confirmGranted", v.outcome)
		}
		if v.why != "" {
			t.Errorf("放行不该带超时归因，got %q", v.why)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("放行后等待方未收敛")
	}
	if e.ConfirmPending(7) {
		t.Error("放行后应注销挂起通道")
	}
	if e.SignalConfirm(7) {
		t.Error("已注销后再次放行应返回 false（且不得 panic）")
	}
}

// 2) 中止链路 A：手动中断（stopCh 关闭）必须立即解除挂起并单独报明「是用户叫的停」——
// 确认等待不能变成「停不下来的死等」，而终态收口要靠这个出路把会话记成 stopped（F4）。
func TestWaitForConfirmAbortedByStop(t *testing.T) {
	e := newConfirmExecutor()
	stopCh := e.registerStop(3)
	done := startWaitConfirm(t, e, context.Background(), 3, time.Minute)
	e.SignalStop(3)
	select {
	case v := <-done:
		if v.outcome != confirmStoppedByUser {
			t.Errorf("被中断出路=%d want confirmStoppedByUser（不得与超时同一值）", v.outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop 未能解除确认挂起")
	}
	if !e.stopFired(stopCh) {
		t.Error("stop 通道应已关闭")
	}
}

// 3) 中止链路 B（批8 解耦契约）：确认预算与执行预算是两条独立计时器，谁先到谁说话，
// 但出路同为 confirmWaitTimedOut（都不是用户主动否决），且 why 必须点明是哪条到头——
// 否则运维会去调错旋钮（该调 confirm_wait_sec 却调了 timeout_sec）。
// 两支都不得放行；确认等待吃满即失败收口，不会把 session 吊成永久 active。
func TestWaitForConfirmBudgetDecoupled(t *testing.T) {
	// 3a 确认预算先到：execCtx 还很宽裕（1min），wait=40ms 就必须自己掐断
	e := newConfirmExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := startWaitConfirm(t, e, ctx, 5, 40*time.Millisecond)
	select {
	case v := <-done:
		if v.outcome != confirmWaitTimedOut {
			t.Errorf("确认预算到头出路=%d want confirmWaitTimedOut", v.outcome)
		}
		if !strings.Contains(v.why, "人工确认等待") {
			t.Errorf("归因必须写明是确认预算到期，got %q", v.why)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("确认预算未掐断等待（仍被 execCtx 兼职？解耦失效）")
	}
	if e.ConfirmPending(5) {
		t.Error("退出后应注销挂起通道（defer 清理）")
	}

	// 3b 执行预算先到：确认预算给了 1min（不可能自己到期），ctx 取消必须立刻收敛，
	// 且归因是「任务执行预算」而不是「人工确认等待」
	e2 := newConfirmExecutor()
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := startWaitConfirm(t, e2, ctx2, 6, time.Minute)
	cancel2()
	select {
	case v := <-done2:
		if v.outcome != confirmWaitTimedOut {
			t.Errorf("ctx 取消出路=%d want confirmWaitTimedOut", v.outcome)
		}
		if !strings.Contains(v.why, "任务执行预算") {
			t.Errorf("ctx 取消归因必须是执行预算，got %q", v.why)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消未能解除确认挂起")
	}
	if e2.ConfirmPending(6) {
		t.Error("退出后应注销挂起通道（defer 清理）")
	}
}

// 4) 编排位置契约（源码静态锁）：post_comment 的确认闸门必须落在 prep 之后、
// 不可逆 send 之前；派生写步（type+回车 / click 发送）的闸门必须落在任何命令帧下发之前。
// 挪到 send/命令帧之后 = 确认形同虚设；默认 false 若被绕过 = 破坏铁律 4。
func TestConfirmGatePrecedesIrreversibleSend(t *testing.T) {
	src := readSrc(t, "executor.go")
	iPrep := strings.Index(src, "e.hand.commentPrep(")
	// 派生写闸门在文件里排在 post_comment 原语之前，所以 prep→gate 的定序必须取最后一个调用点
	iGate := strings.LastIndex(src, "e.waitForConfirm(")
	iSend := strings.Index(src, "e.hand.commentSend(")
	if iGate < 0 {
		t.Fatal("executor.go 无 waitForConfirm 调用点（D7 闸门未接入分发路径）")
	}
	if !(iPrep < iGate && iGate < iSend) {
		t.Errorf("post_comment 闸门必须位于 prep→send 之间，got prep=%d gate=%d send=%d", iPrep, iGate, iSend)
	}
	if !strings.Contains(src, "if task.RequireConfirm {") {
		t.Error("闸门必须以 task.RequireConfirm 为条件（默认 false 时不得挂起）")
	}
	// F4：三种出路必须分流——放行继续 / 用户中止（→stopped）/ 确认超时（→failed）
	if !strings.Contains(src, "case confirmStoppedByUser:") || !strings.Contains(src, "confirmWaitTimedOut：") {
		t.Error("确认闸门未对「用户中止」与「确认超时」分流归因")
	}
	// 批8：闸门恰为两处——post_comment 原语内（先 prep 再问）+ 派生写步分发路径（问都没问就下发=漏闸）
	if got := strings.Count(src, "e.waitForConfirm("); got != 2 {
		t.Errorf("waitForConfirm 调用点应为 2（post_comment + 派生写步），got %d", got)
	}
	derived := strings.Index(src, `if writeStep && step.Action != "post_comment" && task.RequireConfirm {`)
	firstCommand := strings.Index(src, `, "command", `)
	if derived < 0 {
		t.Error("派生写步未接入 D7 闸门（批7 已把它们认成写步，闸门却仍在 post_comment 里）")
	}
	if firstCommand < 0 {
		t.Fatal("找不到命令帧下发点，无法定序")
	}
	if !(derived < firstCommand) {
		t.Errorf("派生写闸门必须先于任何命令帧下发，got gate=%d firstCommand=%d", derived, firstCommand)
	}
}

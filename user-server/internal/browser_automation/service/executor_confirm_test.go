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
		confirmRegistry: make(map[uint]*confirmGate),
	}
}

// gateHandle 一次挂起的测试句柄：结果通道 + 登记上去的那个闸门。
// 放行现在必须带载荷哈希，句柄把哈希的唯一真相交给用例——测试里再抄一份字符串常量，
// 改生产代码时就不会红，而是两份常量各自漂移。
type gateHandle struct {
	done chan confirmVerdict
	gate *confirmGate
}

// startWaitConfirm 登记闸门并后台发起等待，阻塞到确认已进入挂起态。
// ctx 由调用方给出（预算解耦用例需要「ctx 活着、确认预算先到」这一支）。
// 登记放在这里而不是 waitForConfirm 内部，与生产同序（awaitConfirmGate 先登记再等待）。
func startWaitConfirm(t *testing.T, e *Executor, ctx context.Context, sessionID uint, wait time.Duration) gateHandle {
	t.Helper()
	gate := e.registerConfirmGate(&confirmGate{
		ch: make(chan struct{}), sessionID: sessionID, payloadHash: "b20-wait-hash",
		expiresAt: time.Now().Add(wait), budget: wait,
	})
	done := make(chan confirmVerdict, 1)
	go func() {
		out, why := e.waitForConfirm(ctx, gate)
		done <- confirmVerdict{out, why}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !e.ConfirmPending(sessionID) {
		if time.Now().After(deadline) {
			t.Fatal("waitForConfirm 未进入挂起态")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return gateHandle{done: done, gate: gate}
}

// confirmVerdict 把 waitForConfirm 的双返回值打包进通道。
type confirmVerdict struct {
	outcome confirmOutcome
	why     string
}

// 1) 放行链路：未挂起时 ConfirmPending=false、SignalConfirm=VerdictNoGate（不误报命中）；
// 挂起后可放行一次，等待方返回 granted，通道随即注销；重复放行返回 NoGate（不 double close）。
func TestSignalConfirmLifecycle(t *testing.T) {
	e := newConfirmExecutor()
	if e.ConfirmPending(7) {
		t.Error("初始不应有待确认")
	}
	if e.SignalConfirm(7, "b20-wait-hash") != VerdictNoGate {
		t.Error("无挂起点时放行应返回 VerdictNoGate")
	}

	h := startWaitConfirm(t, e, context.Background(), 7, time.Minute)
	if e.SignalConfirm(7, h.gate.payloadHash) != VerdictGranted {
		t.Fatal("挂起中放行应命中")
	}
	select {
	case v := <-h.done:
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
	if e.SignalConfirm(7, h.gate.payloadHash) != VerdictNoGate {
		t.Error("已注销后再次放行应返回 VerdictNoGate（且不得 panic）")
	}
}

// 2) 中止链路 A：手动中断（stopCh 关闭）必须立即解除挂起并单独报明「是用户叫的停」——
// 确认等待不能变成「停不下来的死等」，而终态收口要靠这个出路把会话记成 stopped（F4）。
func TestWaitForConfirmAbortedByStop(t *testing.T) {
	e := newConfirmExecutor()
	stopCh := e.registerStop(3)
	h := startWaitConfirm(t, e, context.Background(), 3, time.Minute)
	e.SignalStop(3)
	select {
	case v := <-h.done:
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

// 3) 中止链路 B（解耦契约）：确认预算与执行预算是两条独立计时器，谁先到谁说话，
// 但出路同为 confirmWaitTimedOut（都不是用户主动否决），且 why 必须点明是哪条到头——
// 否则运维会去调错旋钮（该调 confirm_wait_sec 却调了 timeout_sec）。
// 两支都不得放行；确认等待吃满即失败收口，不会把 session 吊成永久 active。
func TestWaitForConfirmBudgetDecoupled(t *testing.T) {
	// 3a 确认预算先到：execCtx 还很宽裕（1min），wait=40ms 就必须自己掐断
	e := newConfirmExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h := startWaitConfirm(t, e, ctx, 5, 40*time.Millisecond)
	select {
	case v := <-h.done:
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
	h2 := startWaitConfirm(t, e2, ctx2, 6, time.Minute)
	cancel2()
	select {
	case v := <-h2.done:
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
//
// 起闸门收成 awaitConfirmGate 一个入口：静态锁因此多锁两件事——
// ① 调用点仍恰为两处（漏一处=有一种写步问都没问就下发）；
// ② waitForConfirm 只能被 awaitConfirmGate 调起（绕过它=挂起不留审计痕迹、不绑载荷）；
// ③ 登记先于落帧（顺序反了会有一个「帧已写、注册表还空着」的窗口把真放行答成「没有闸门」）。
func TestConfirmGatePrecedesIrreversibleSend(t *testing.T) {
	src := readSrc(t, "executor.go")
	iPrep := strings.Index(src, "e.hand.commentPrep(")
	// 派生写闸门在文件里排在 post_comment 原语之前，所以 prep→gate 的定序必须取最后一个调用点
	iGate := strings.LastIndex(src, "e.awaitConfirmGate(")
	iSend := strings.Index(src, "e.hand.commentSend(")
	if iGate < 0 {
		t.Fatal("executor.go 无 awaitConfirmGate 调用点（D7 闸门未接入分发路径）")
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
	// ① 闸门恰为两处——post_comment 原语内（先 prep 再问）+ 派生写步分发路径（问都没问就下发=漏闸）
	if got := strings.Count(src, "e.awaitConfirmGate("); got != 2 {
		t.Errorf("awaitConfirmGate 调用点应为 2（post_comment + 派生写步），got %d", got)
	}
	// ② 等待原语只允许被唯一入口调用（自己开一个 waitForConfirm 就等于开一个不落帧的闸门）
	if got := strings.Count(src, "e.waitForConfirm("); got != 1 {
		t.Errorf("waitForConfirm 调用点应为 1（只在 awaitConfirmGate 内），got %d", got)
	}
	// ③ 登记 → 落 d7_wait 帧 → 等待，三步定序（三处都只出现一次，全局索引即函数内索引）
	iReg := strings.Index(src, "e.registerConfirmGate(gate)")
	iFrame := strings.Index(src, `, "event", gateWaitFrame`)
	iWait := strings.Index(src, "e.waitForConfirm(ctx, gate)")
	if iReg < 0 || iFrame < 0 || iWait < 0 {
		t.Fatalf("闸门三步缺一：register=%d wait帧=%d wait=%d", iReg, iFrame, iWait)
	}
	if !(iReg < iFrame && iFrame < iWait) {
		t.Errorf("闸门定序必须是 登记→落帧→等待，got register=%d frame=%d wait=%d", iReg, iFrame, iWait)
	}
	derived := strings.Index(src, `if writeStep && step.Action != "post_comment" && task.RequireConfirm {`)
	firstCommand := strings.Index(src, `, "command", `)
	if derived < 0 {
		t.Error("派生写步未接入 D7 闸门（它们已被认成写步，闸门却仍在 post_comment 里）")
	}
	if firstCommand < 0 {
		t.Fatal("找不到命令帧下发点，无法定序")
	}
	if !(derived < firstCommand) {
		t.Errorf("派生写闸门必须先于任何命令帧下发，got gate=%d firstCommand=%d", derived, firstCommand)
	}
}

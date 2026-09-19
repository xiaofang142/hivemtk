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
// ctx 由调用方给出（超时中止用例需要可取消 ctx）。
func startWaitConfirm(t *testing.T, e *Executor, ctx context.Context, sessionID uint) chan confirmOutcome {
	t.Helper()
	done := make(chan confirmOutcome, 1)
	go func() { done <- e.waitForConfirm(ctx, sessionID) }()
	deadline := time.Now().Add(2 * time.Second)
	for !e.ConfirmPending(sessionID) {
		if time.Now().After(deadline) {
			t.Fatal("waitForConfirm 未进入挂起态")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return done
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

	done := startWaitConfirm(t, e, context.Background(), 7)
	if !e.SignalConfirm(7) {
		t.Fatal("挂起中放行应命中")
	}
	select {
	case out := <-done:
		if out != confirmGranted {
			t.Errorf("放行后出路=%v want confirmGranted", out)
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
	done := startWaitConfirm(t, e, context.Background(), 3)
	e.SignalStop(3)
	select {
	case out := <-done:
		if out != confirmStoppedByUser {
			t.Errorf("被中断出路=%d want confirmStoppedByUser（不得与超时同一值）", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop 未能解除确认挂起")
	}
	if !e.stopFired(stopCh) {
		t.Error("stop 通道应已关闭")
	}
}

// 3) 中止链路 B：ctx 超时（task.TimeoutSec 预算耗尽）出路=confirmWaitTimedOut，
// 与「用户主动中止」必须是两个值（否则人工否决被统计成系统故障）；
// 两者都不得放行，确认等待吃满预算即失败收口，不会把 session 吊成永久 active。
func TestWaitForConfirmAbortedByContext(t *testing.T) {
	e := newConfirmExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startWaitConfirm(t, e, ctx, 5)
	cancel()
	select {
	case out := <-done:
		if out != confirmWaitTimedOut {
			t.Errorf("ctx 取消出路=%d want confirmWaitTimedOut", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消未能解除确认挂起")
	}
	if e.ConfirmPending(5) {
		t.Error("退出后应注销挂起通道（defer 清理）")
	}
}

// 4) 编排位置契约（源码静态锁）：确认闸门必须落在 post_comment 的 prep 之后、
// 不可逆 send 之前，且仅在 task.RequireConfirm 为真时生效。
// 挪到 send 之后 = 确认形同虚设；默认 false 若被绕过 = 破坏铁律 4。
func TestConfirmGatePrecedesIrreversibleSend(t *testing.T) {
	src := readSrc(t, "executor.go")
	iPrep := strings.Index(src, "e.hand.commentPrep(")
	iGate := strings.Index(src, "e.waitForConfirm(")
	iSend := strings.Index(src, "e.hand.commentSend(")
	if iGate < 0 {
		t.Fatal("executor.go 无 waitForConfirm 调用点（D7 闸门未接入分发路径）")
	}
	if !(iPrep < iGate && iGate < iSend) {
		t.Errorf("闸门必须位于 prep→send 之间，got prep=%d gate=%d send=%d", iPrep, iGate, iSend)
	}
	if !strings.Contains(src, "if task.RequireConfirm {") {
		t.Error("闸门必须以 task.RequireConfirm 为条件（默认 false 时不得挂起）")
	}
	// F4：三种出路必须分流——放行继续 / 用户中止（→stopped）/ 确认超时（→failed）
	if !strings.Contains(src, "case confirmStoppedByUser:") || !strings.Contains(src, "confirmWaitTimedOut：") {
		t.Error("确认闸门未对「用户中止」与「确认超时」分流归因")
	}
	// 闸门只准出现在写原语路径：全文件 waitForConfirm 调用点恰为一处（读原语无须确认）
	if got := strings.Count(src, "e.waitForConfirm("); got != 1 {
		t.Errorf("waitForConfirm 调用点应唯一，got %d", got)
	}
}

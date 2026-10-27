package app

import (
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
)

// E-6a 验收用例（规划 §4.6）：熔断打开 ⇒ 告警器被调用；shadow 态只记录不告警。
//
// observeToolCircuitDecision 是生产装配的观察者：WouldBlock 且非 shadow 时
// 必须调 AlertCircuitOpen 恰好一次；shadow 判定（WouldBlock=true、Shadow=true）
// 不得触发告警——那是"只记录不告警"口径的验收点。
func TestObserveToolCircuitDecision_AlertsOnBlockNotShadow(t *testing.T) {
	prev := toolAlertManager
	t.Cleanup(func() { toolAlertManager = prev })

	var mu sync.Mutex
	var alerts []tooluse.AlertEvent
	mgr := tooluse.NewToolAlertManager()
	mgr.AddHandler(tooluse.AlertHandlerFunc(func(e tooluse.AlertEvent) {
		mu.Lock()
		defer mu.Unlock()
		alerts = append(alerts, e)
	}))
	toolAlertManager = mgr

	// ① enforce 态 would_block ⇒ 告警 1 条
	observeToolCircuitDecision(t.Context(), tooluse.CircuitDecision{
		ToolName: "search_kb", State: tooluse.CircuitOpen, WouldBlock: true, Shadow: false,
	})
	// emitAlert 的 handler 是 go 协程派发（生产语义：告警通道不能拖慢主链），
	// 所以这里必须等它落地，不能 observe 返回后立刻读计数——全量跑时机器一忙就假红。
	if n := waitAlertCount(&mu, &alerts, 1); n != 1 {
		t.Fatalf("enforce would_block 应恰好 1 条告警，实际 %d", n)
	}
	mu.Lock()
	if alerts[0].ToolName != "search_kb" {
		t.Fatalf("告警应点名工具名，实际 %q", alerts[0].ToolName)
	}
	mu.Unlock()

	// ② shadow 判定 ⇒ 只记录不告警（计数不变）
	observeToolCircuitDecision(t.Context(), tooluse.CircuitDecision{
		ToolName: "search_kb", State: tooluse.CircuitOpen, WouldBlock: true, Shadow: true,
	})
	// 给异步派发留出足够时间：若 shadow 也告警，这段时间内必然能看到 >1。
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(alerts) != 1 {
		t.Fatalf("shadow 态不得触发告警（只记录），实际告警数 %d", len(alerts))
	}
}

// waitAlertCount 轮询等待异步 handler 落地，避免把调度延迟当成功能缺失。
func waitAlertCount(mu *sync.Mutex, alerts *[]tooluse.AlertEvent, want int) int {
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(*alerts)
		mu.Unlock()
		if n >= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ③ 放行判定（WouldBlock=false）不告警——绝大多数调用没有信息量。
func TestObserveToolCircuitDecision_PassDoesNotAlert(t *testing.T) {
	prev := toolAlertManager
	t.Cleanup(func() { toolAlertManager = prev })

	var alerts []tooluse.AlertEvent
	mgr := tooluse.NewToolAlertManager()
	mgr.AddHandler(tooluse.AlertHandlerFunc(func(e tooluse.AlertEvent) { alerts = append(alerts, e) }))
	toolAlertManager = mgr

	observeToolCircuitDecision(t.Context(), tooluse.CircuitDecision{
		ToolName: "search_kb", State: tooluse.CircuitClosed, WouldBlock: false, Shadow: false,
	})
	if len(alerts) != 0 {
		t.Fatalf("放行判定不得告警，实际 %d 条", len(alerts))
	}
}

// ④ off 态（toolAlertManager=nil）判定不 panic——装配降级不炸主链。
func TestObserveToolCircuitDecision_NilManagerNoPanic(t *testing.T) {
	prev := toolAlertManager
	t.Cleanup(func() { toolAlertManager = prev })
	toolAlertManager = nil
	observeToolCircuitDecision(t.Context(), tooluse.CircuitDecision{
		ToolName: "search_kb", State: tooluse.CircuitOpen, WouldBlock: true, Shadow: false,
	})
}

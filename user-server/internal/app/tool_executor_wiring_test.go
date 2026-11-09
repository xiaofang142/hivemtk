// tool_executor_wiring_test.go MD-02：工具执行器的 Construct→Ready 可观测。
package app

import (
	"testing"
)

// InitGlobalToolExecutor 是 Construct+Ready 一体（executor 不持协程，注册即就绪，
// 无独立 Start/Stop）。本用例钉住两件事：装配后就绪口为真；重复装配不翻转语义
// （全局件换新、不报错）。审批门固定 off 档，只验执行器本身，不改审批语义；
// 包级全局由 restoreApprovalState 在 cleanup 还原。
func TestGlobalToolExecutorReadyAfterConstruct(t *testing.T) {
	restoreApprovalState(t)
	t.Setenv(ApprovalGateFlagEnv, "off")

	InitGlobalToolExecutor()
	if !GlobalToolExecutorReady() {
		t.Fatal("InitGlobalToolExecutor 后 GlobalToolExecutorReady 应为真（注册即就绪）")
	}
	InitGlobalToolExecutor() // 重复 Construct：全局件换新，不报错、不留双份
	if !GlobalToolExecutorReady() {
		t.Fatal("重复 Init 后就绪口仍应为真")
	}
}

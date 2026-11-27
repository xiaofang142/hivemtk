package main

import "testing"

// TestRunOutboundReclaimForTenantsNilTask 验证生产装配的巡检循环在 task 未就绪
// 或租户清单含 0 值时不会 panic；OutboundReclaimTask.RunOnce 对 nil 接收者静默返回。
func TestRunOutboundReclaimForTenantsNilTask(t *testing.T) {
	runOutboundReclaimForTenants(nil, []uint{0, 1, 2})
}

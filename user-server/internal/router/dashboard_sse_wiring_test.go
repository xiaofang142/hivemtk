package router

// 契约锁：DashboardSSEController 的三个 handler 必须真的挂在路由上。
//
// 立项依据：StreamEventStream / Snapshot / Metrics 三个 handler 与 collectSnapshot /
// collectLLMMetrics 两个采集函数都在盘上，但全仓没有任何装配点——实时驾驶舱的
// SSE 长连接从未上线，运行时一律 404。这类失效形态最难被发现：编译通过、vet 通过、
// 单测全绿，因为没有任何一处代码声称它可达过；只有真正去点那个页面才知道是 404。
//
// 判据取自装配函数源码本身（本仓对「接线是否在位」的既有做法，见
// browser_trigger_cascade_wiring_test.go）：断言三个 handler 都被映射到具体 URL 上，
// 而不是断言某个构造函数被调用过——后者在路由被改名后依然会绿。
import (
	"os"
	"strings"
	"testing"
)

func TestSSEDashboardRoutesWireDashboardSSEHandlers(t *testing.T) {
	src, err := os.ReadFile("service_routes.go")
	if err != nil {
		t.Fatalf("读取 service_routes.go 失败: %v", err)
	}
	text := string(src)

	wired := map[string]string{
		"StreamEventStream": "/dashboards/stream",
		"Snapshot":          "/dashboards/snapshot",
		"Metrics":           "/dashboards/metrics",
	}
	for method, path := range wired {
		if !strings.Contains(text, method) {
			t.Errorf("%s handler 未出现在装配处：实时驾驶舱 %s 是 404", method, path)
			continue
		}
		if !strings.Contains(text, path) {
			t.Errorf("%s 的路由 %s 未出现在 service_routes.go", method, path)
		}
	}
	// 控制器本身必须被构造出来（handler 名字出现在文件里 ≠ 实例存在）
	if !strings.Contains(text, "controller.NewDashboardSSEController(") {
		t.Error("DashboardSSEController 未被实例化")
	}
	// 统计服务必须由 router 注入：不注入就退化成零值快照，「实时」二字落空
	if !strings.Contains(text, "service.NewDashboardStatsService(db)") {
		t.Error("DashboardStatsService 未注入：SSE 会恒推零值快照")
	}
}

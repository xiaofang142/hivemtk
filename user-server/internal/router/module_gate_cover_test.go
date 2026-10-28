package router

// TestModuleRoutePatternsCoverShippedRoutes_BrowserLegs —— module_gate.go 承诺的
// 「TestModuleRoutePatternsCoverShippedRoutes」兜底测试的浏览器双腿落地版。
//
// /api/egolite 与 /api/browser-automation 的形状不在生成件（能力面表按"菜单页消费的口"
// 收录，天然收不到这两条腿），全靠 module_route_supplement.go 的手写条目拦住
// ——漏了它们，多租户模式下 ¥99 买断模块就是免费的。
//
// 本测试把双腿实际注册的路径形状逐一钉进 ModuleOwnerOf：谁删了补充件、改了
// 匹配语义、或新增 authed 路由没被模式盖住，这里红。免 JWT 通道（runner-ws、
// assets/pull、host-ws）不在 GuardModulePath 射程，由 eg_/bh_ token 门负责，
// 不在本表（route_policy_gate_test.go 已另行钉住）。

import (
	"testing"

	"hivemtk-user/internal/middleware"
)

func TestModuleRoutePatternsCoverShippedRoutes_BrowserLegs(t *testing.T) {
	egolitePaths := []string{
		// runner 域
		"/api/egolite/runner/status",
		"/api/egolite/runner/tokens",
		"/api/egolite/runner/tokens/123/revoke",
		"/api/egolite/runner/token/reset",
		"/api/egolite/admin/runner/upgrade",
		// flow 域
		"/api/egolite/flows",
		"/api/egolite/flows/123",
		"/api/egolite/flows/123/rescan",
		"/api/egolite/flows/123/publish",
		"/api/egolite/flows/123/dry-run",
		"/api/egolite/flows/123/submit",
		"/api/egolite/flows/123/cron",
		"/api/egolite/admin/flows",
		// 执行域
		"/api/egolite/executions",
		"/api/egolite/executions/123",
		"/api/egolite/executions/123/events",
		"/api/egolite/executions/123/redispatch",
		"/api/egolite/executions/123/abort",
		"/api/egolite/executions/123/confirm",
		// 会话域
		"/api/egolite/sessions",
		"/api/egolite/sessions/123",
		"/api/egolite/sessions/123/round",
		"/api/egolite/sessions/123/observe",
		"/api/egolite/sessions/123/rounds",
		"/api/egolite/sessions/123/confirm",
		"/api/egolite/sessions/123/abort",
		"/api/egolite/sessions/123/save-as-flow",
		"/api/egolite/sessions/123/finish",
		// 资产域
		"/api/egolite/assets",
		"/api/egolite/admin/assets",
		"/api/egolite/admin/assets/publish",
		"/api/egolite/admin/assets/pack",
	}
	browserPaths := []string{
		"/api/browser-automation/tasks",
		"/api/browser-automation/tasks/123",
		"/api/browser-automation/tasks/123/publish",
		"/api/browser-automation/tasks/123/run",
		"/api/browser-automation/tasks/123/pause",
		"/api/browser-automation/tasks/123/resume",
		"/api/browser-automation/tasks/123/archive",
		"/api/browser-automation/tasks/123/dependency",
		"/api/browser-automation/tasks/123/sessions",
		"/api/browser-automation/tasks/123/receipts",
		"/api/browser-automation/sessions",
		"/api/browser-automation/sessions/123",
		"/api/browser-automation/sessions/123/steps",
		"/api/browser-automation/sessions/123/logs",
		"/api/browser-automation/sessions/123/export",
		"/api/browser-automation/sessions/123/stop",
		"/api/browser-automation/sessions/123/confirm-gate",
		"/api/browser-automation/sessions/123/confirm",
		"/api/browser-automation/cron",
		"/api/browser-automation/cron/123",
		"/api/browser-automation/cron/123/enable",
		"/api/browser-automation/cron/123/disable",
		"/api/browser-automation/host/status",
		"/api/browser-automation/platforms",
		"/api/browser-automation/platforms/123/locators",
		"/api/browser-automation/host/token/reset",
		"/api/browser-automation/profile/recover",
	}
	for _, p := range egolitePaths {
		if got := middleware.ModuleOwnerOf(p); got != "browser-automation" {
			t.Errorf("egolite 腿 %q 模块归属=%q，应为 browser-automation（补充件缺失或匹配语义漂移）", p, got)
		}
	}
	for _, p := range browserPaths {
		if got := middleware.ModuleOwnerOf(p); got != "browser-automation" {
			t.Errorf("browser 腿 %q 模块归属=%q，应为 browser-automation（补充件缺失或匹配语义漂移）", p, got)
		}
	}

	// 负向：段级匹配不能被前缀相似的路径误命中
	for _, p := range []string{"/api/egolite2/x", "/api/browser-automationX", "/api/egolitefake/y"} {
		if got := middleware.ModuleOwnerOf(p); got == "browser-automation" {
			t.Errorf("负向路径 %q 不应命中 browser-automation（前缀误匹配）", p)
		}
	}
}

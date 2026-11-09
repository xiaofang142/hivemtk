package router

import (
	"os"
	"strings"
	"testing"
)

// MD-04 S1：setupMessageRoutes 的 InboxIngressService 第二构造已收敛为参数注入。
// 改前红（service_routes.go 仍自带 NewInboxIngressService），改后绿。
func TestSetupMessageRoutes_UsesSharedIngressServiceInstance(t *testing.T) {
	src, err := os.ReadFile("service_routes.go")
	if err != nil {
		t.Fatalf("读 service_routes.go 失败: %v", err)
	}
	if n := strings.Count(string(src), "service.NewInboxIngressService("); n != 0 {
		t.Errorf("service_routes.go 里 service.NewInboxIngressService( 出现 %d 次，期望 0：第二构造已收敛为参数", n)
	}
	if !strings.Contains(string(src), "ingressSvc *service.InboxIngressService") {
		t.Error("setupMessageRoutes 未声明 ingressSvc *service.InboxIngressService 参数")
	}
}

// router.go 只保留唯一生产构造点（admin_routes.go:131 公开入口 aiTrigger=nil
// 是产品语义，不在本刀范围，单独文件另行计数）。
func TestRouterAssemblesExactlyOneInboxIngressService(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读 router.go 失败: %v", err)
	}
	const ctor = "service." + "NewInboxIngressService("
	if n := strings.Count(string(src), ctor); n != 1 {
		t.Errorf("router.go 里 %s 出现 %d 次，期望 1 次：装配期唯一实例", ctor, n)
	}
}

// S3：桥接全局注册副作用已迁出 router.Setup（迁入 app/bridge_wiring.go），
// Router 不再留 Repository/Service 构造与全局注册。
func TestRouterSetup_NoBridgeRepoConstructionLeft(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读 router.go 失败: %v", err)
	}
	for _, ctor := range []string{
		"bridge.NewBridgeIngestHandler(",
		"repository.NewMessageHubRepositoryWithDB(",
		"bridge.NewBridgeAccountRepository(",
		"service.SetGlobalSSEPublisher(",
		"bridge.RegisterOwnershipChecker(",
		"bridge.RegisterBridgeAccountRepo(",
		"app.SetBridgeIngressSvc(",
	} {
		if n := strings.Count(string(src), ctor); n != 0 {
			t.Errorf("router.go 里 %s 残留 %d 次，期望 0：已迁 app/bridge_wiring.go", ctor, n)
		}
	}
}

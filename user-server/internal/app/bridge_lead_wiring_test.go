package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func TestBridgeLeadRuntime_RequiresReadyDependencies(t *testing.T) {
	if _, err := InitBridgeLeadRuntime(nil, nil); err == nil {
		t.Fatal("缺少依赖不能启动后台执行")
	}
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	ingress := service.NewInboxIngressService()
	ingress.SetBridgeLeadProcessor(func(context.Context, *model.MessageHub) error { return nil })
	worker, err := InitBridgeLeadRuntime(database, ingress)
	if err != nil || worker == nil {
		t.Fatalf("已装配依赖必须生成可关闭实例：%v", err)
	}
	if err := StopBridgeLeadRuntime(worker); err != nil {
		t.Fatal("装配实例必须正常关闭：", err)
	}
}

func TestBridgeLeadRuntime_IsWiredAfterRouterAndStopsBeforeDBClose(t *testing.T) {
	routerSrc, err := os.ReadFile("../router/router.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSrc, err := os.ReadFile("../../cmd/api/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(routerSrc), "SetLeadMiner(") || !strings.Contains(string(routerSrc), "SetBridgeLeadProcessor(") {
		t.Fatal("生产必须装配持久矿工处理器而非HTTP回调")
	}
	body := string(mainSrc)
	setup, init := strings.Index(body, "router.Setup(r, db.GetDB())"), strings.Index(body, "app.InitBridgeLeadRuntime(")
	if setup < 0 || init <= setup || !strings.Contains(body, "defer func()") || !strings.Contains(body, "if err := app.StopBridgeLeadRuntime(leadWorker); err != nil") {
		t.Fatal("后台实例必须在依赖装配之后启动并显式登记关闭")
	}
}

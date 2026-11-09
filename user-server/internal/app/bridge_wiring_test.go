package app

import (
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/service"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// MD-04 S3：桥接全局注册副作用收进 WireBridgeGlobals，ingress 实例由调用方
// 显式传入（不再依赖 Router 初始化副作用）。
func TestWireBridgeGlobals_SetsIngressGlobalAndReturnsHandler(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	ingressSvc := service.NewInboxIngressService()
	t.Cleanup(func() { SetBridgeIngressSvc(nil) })

	h := WireBridgeGlobals(database, ingressSvc)
	if h == nil {
		t.Fatal("WireBridgeGlobals 返回 nil handler")
	}
	if got := GetBridgeIngressSvc(); got != ingressSvc {
		t.Errorf("全局桥接入站实例指针漂移：期望装配入参实例 %p，实际 %p", ingressSvc, got)
	}
}

// 源形状守卫：全局注册三件套（SSE publisher/账号仓储/归属 checker）在
// app/bridge_wiring.go 各一次，且 R-B1 msg_id=ContentHashMsgID 与
// PublishCtx 租户口径逐字节随迁；本文件不得新造 InboxIngressService 实例。
func TestWireBridgeGlobals_ClosuresMovedByteForByte(t *testing.T) {
	src, err := os.ReadFile("bridge_wiring.go")
	if err != nil {
		t.Fatalf("读 bridge_wiring.go 失败: %v", err)
	}
	body := string(src)
	for _, want := range []string{
		"service.SetGlobalSSEPublisher(",
		"bridge.RegisterBridgeAccountRepo(",
		"bridge.RegisterOwnershipChecker(",
		"service.ContentHashMsgID(",
		"bridge.GlobalSSEBus.PublishCtx(",
		"acc.UserID == 0 || acc.UserID == userID",
		"msg_id|conversation_id",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bridge_wiring.go 缺少随迁片段 %q", want)
		}
	}
	if strings.Contains(body, "service.NewInboxIngressService(") {
		t.Error("bridge_wiring.go 不得新造 InboxIngressService 实例（唯一构造点仍在 router.Setup）")
	}
}

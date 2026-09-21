package router

import (
	"os"
	"strings"
	"testing"
)

// T-P5-03：SOP 的 `reach_send` 节点靠一个全局 sender 出域，而那个 sender 必须是
// **装了 T-P3-07 闸门的那一个实例**。这条用源码形状守，理由是运行时没有任何可观测差异：
//
//   - 装配点漏掉这一行 ⇒ 图上的外发节点全部 fail-closed（"外发服务未装配"），
//     而 internal/service 的用例带着自己的夹具跑，一条都不会红；
//   - 装配点改成 `SetSOPReachSender(service.NewProactiveReachService(db, nil))`（新建一个没装
//     闸门的实例）⇒ 编译过、路由通、发得出短信，只是 W-1 那道发送前门从此不在 Active 这条路上。
//
// 所以这里断的有两件：**存在**（恰好一处）与**次序**（在 AttachReachGate 之后，且交的是同一个
// 变量名）。次序不是洁癖 —— AttachReachGate 是在 proactiveSvc 上就地装门，先借后装就等于
// 把一个没装门的实例交给了 SOP 这一侧，而 W-1 的门只在发送时才有声音。
//
// 与既有口径同源：TestRouterAssemblesExactlyOneWebhookService 也用源码形状守"装配期唯一实例"。
func TestRouterWiresGatedSenderToSOPLane(t *testing.T) {
	src, err := os.ReadFile("service_routes.go")
	if err != nil {
		t.Fatalf("读 service_routes.go 失败（门扫空就等于没测）: %v", err)
	}
	text := string(src)

	// 控制组：锚点所在的那个装配函数还在。函数改名时下面几条都会"零命中"，
	// 没有这一句就分不出"装配被拆了"和"文件长变了"。
	if !strings.Contains(text, "func setupProactiveReachRoutes(") {
		t.Fatal("找不到 setupProactiveReachRoutes（本用例的锚点前提已不成立，须同步改这里）")
	}

	const gate = "AttachReachGate(proactiveSvc)"
	const wire = "service.SetSOPReachSender(proactiveSvc)"
	if n := strings.Count(text, wire); n != 1 {
		t.Errorf("SOP 侧 sender 装配点出现 %d 次，期望恰好 1 次（0 次=图上的外发永不装配；"+
			"多次=两个实例在抢同一个全局）", n)
	}
	gi, wi := strings.Index(text, gate), strings.Index(text, wire)
	if gi < 0 {
		t.Fatal("装配点读不到 AttachReachGate(proactiveSvc)：闸门那一侧的写法变了，本用例的次序断言失去对象")
	}
	if wi >= 0 && wi < gi {
		t.Error("sender 装在了闸门之前：借出去的是没装 W-1 门的那个实例")
	}
	// 交出去的必须是**同一个变量**：出现第二个 NewProactiveReachService 就等于 SOP 这一侧
	// 走的是另一套（看不见退订表、看不见冷却窗的手工装配路径）。
	if n := strings.Count(text, "service.NewProactiveReachService("); n != 1 {
		t.Errorf("setupProactiveReachRoutes 所在文件里外发服务构造点 %d 处，期望 1 处（多一处就有一条未装门的实例）", n)
	}
}

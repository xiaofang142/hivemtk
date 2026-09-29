// kb_release_routes_test.go T-P9-02：G-5 变更/发布这组端点的**挂载层**判据。
//
// 本包只测三件在 controller 用例里测不到的事（那层已经用替身把状态码、入参翻译、
// 留痕形状各判过一遍，这里一行都不重复）：
//  1. 挂载取的是**全局登记处**那一份，而不是就地 new 的（两处各一份就会漂移：装配点
//     换了构造参数而路由还在用旧形状）；
//  2. 静态段（/gate、/taxonomy）与 :product / :id 共存时谁先匹配 —— 只在真挂一次时才暴露，
//     gin 的路由树对这种共存有历史包袱；
//  3. **装配次序**：InitApprovalRuntime → InitKBReleaseRuntime → setupKBReleaseRoutes。
//     次序不在用例里钉住的话，破坏它的一次改动编译照过、测试照绿（本竖没有可观测的
//     运行时差异 —— 路由只是在挂载那一刻取了一次全局快照），只有线上会露出来。
package router

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// kbRoutesSetupWithIdentity 挂一次路由并带回身份的中间件。
//
// operator 传 nil 表示"这一趟没有登录态"，与 controller 用例里同一夹具形状。
func kbRoutesSetupWithIdentity(t *testing.T, operator any) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	if operator != nil {
		auth.Use(func(ctx *gin.Context) {
			ctx.Set("user_id", operator)
			ctx.Next()
		})
	}
	setupKBReleaseRoutes(auth)
	return engine
}

// kbRoutesDB 建本竖四张表 + 语料表 + 审批表（服务构造里会引用后两者；缺表不会在挂载期报错，
// 但发布那一条会，所以夹具一次给齐）。
func kbRoutesDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.KBRelease{}, &model.KBChangeRequest{}, &model.KBChangeAuditLog{},
		&model.KnowledgeChunk{}, &model.ApprovalRequest{})
}

// 未装配时每个依赖底座的出口都回 503，且响应体里不许带任何数据字段；
// 两条静态出口（档位、值域表）必须照常 200 —— 它们读的是 env 与常量，
// 跟着底座一起 503 会让运维在"底座坏了"之外再多查一层。
func TestKBReleaseRoutes_UnassembledAnswers503WithoutData(t *testing.T) {
	// 前置自己钉：整包跑时别的用例会走 router.Setup，把全局设成"服务在、库已关"的形状，
	// 那一路读到的是 500（sql: database is closed）而不是 503，判据就成了测别人的收尾顺序。
	t.Cleanup(func() { service.SetGlobalKBReleaseService(nil) })
	service.SetGlobalKBReleaseService(nil)

	h := kbRoutesSetupWithIdentity(t, uint(7))
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/kb-changes", ""},
		{http.MethodPost, "/api/kb-changes", `{"product_id":"p1","op":"add","content":"x","reason":"y"}`},
		{http.MethodGet, "/api/kb-changes/kbc_1", ""},
		{http.MethodPost, "/api/kb-changes/kbc_1/withdraw", ""},
		{http.MethodGet, "/api/kb-changes/kbc_1/audit", ""},
		{http.MethodGet, "/api/kb-releases", ""},
		{http.MethodGet, "/api/kb-releases/p1", ""},
		{http.MethodGet, "/api/kb-releases/p1/stats", ""},
		{http.MethodGet, "/api/kb-releases/p1/audit", ""},
		{http.MethodPost, "/api/kb-releases/p1/publish", ""},
		{http.MethodPost, "/api/kb-releases/p1/rollback", ""},
		{http.MethodPost, "/api/kb-releases/p1/restore", ""},
		{http.MethodPatch, "/api/kb-releases/p1/governed", `{"governed":true}`},
	} {
		// 复用坏例那一套响应探针（同包、同一 {code,message,data} 契约，不再抄第三份）。
		got := doBadCase(t, h, c.method, c.path, c.body)
		if got.Status != http.StatusServiceUnavailable {
			t.Errorf("%s %s 未装配时应 503，实得 %d：%s", c.method, c.path, got.Status, got.Body)
		}
		for _, frag := range []string{`"list"`, `"total"`, `"result"`, `"verdicts"`, `"governed"`, `"pending_changes"`} {
			if strings.Contains(got.Body, frag) {
				t.Errorf("503 响应里带了数据字段 %s（会把\"一次都没读到\"渲染成业务结论）：%s", frag, got.Body)
			}
		}
	}

	// 两条静态出口：不碰底座，所以不该 503。
	if got := doBadCase(t, h, http.MethodGet, "/api/kb-releases/gate", ""); got.Status != http.StatusOK {
		t.Errorf("/gate 不该依赖底座，实得 %d：%s", got.Status, got.Body)
	} else if got.Data["mode"] == nil {
		t.Errorf("/gate 没回档位：%s", got.Body)
	}
	if got := doBadCase(t, h, http.MethodGet, "/api/kb-changes/taxonomy", ""); got.Status != http.StatusOK {
		t.Errorf("taxonomy 不该依赖底座，实得 %d：%s", got.Status, got.Body)
	} else if ops, ok := got.Data["ops"].([]any); !ok || len(ops) != len(model.KBChangeOps) {
		t.Errorf("taxonomy 的 ops 与值域表不一致：%v", got.Data["ops"])
	}
}

// 静态段抢在参数段之前：/kb-releases/gate 若被 /:product 抢走，档位读数的 product_id 会
// 回显成 "gate"（把"闸门在哪一档"变成一个库的状态）；/kb-changes/taxonomy 同理会被当成
// 变更号去查、回 404，前端那份值域表就永远拿不到。
func TestKBReleaseRoutes_StaticSegmentsWin(t *testing.T) {
	t.Cleanup(func() { service.SetGlobalKBReleaseService(nil) })
	db := kbRoutesDB(t)
	if svc := app.InitKBReleaseRuntime(db); svc == nil {
		t.Fatal("InitKBReleaseRuntime(真句柄) 回了 nil")
	}
	h := kbRoutesSetupWithIdentity(t, uint(7))

	gate := doBadCase(t, h, http.MethodGet, "/api/kb-releases/gate", "")
	if gate.Status != http.StatusOK {
		t.Fatalf("/gate 应 200，实得 %d：%s", gate.Status, gate.Body)
	}
	if pid, _ := gate.Data["product_id"].(string); pid != "" {
		t.Errorf("/gate 被 :product 抢走了（回显库号 %q）：%s", pid, gate.Body)
	}

	tax := doBadCase(t, h, http.MethodGet, "/api/kb-changes/taxonomy", "")
	if tax.Status != http.StatusOK {
		t.Fatalf("taxonomy 应 200，实得 %d：%s", tax.Status, tax.Body)
	}

	// 反向控制：换一个不叫 gate 的库号打同一条参数路由，它必须 200 并把库号回显出来。
	// 有了这一格，上面那条 "/gate 没回显 product_id" 才证明的是"走了档位出口"，
	// 而不是"这条参数路由压根没挂、谁都匹配不上"。
	probe := doBadCase(t, h, http.MethodGet, "/api/kb-releases/not-a-gate", "")
	if probe.Status != http.StatusOK || probe.Data["product_id"] != "not-a-gate" || probe.Data["exists"] != false {
		t.Errorf("/:product 这条参数路由本身该通（证明静态段那条走的是另一出口）：%d %v",
			probe.Status, probe.Data)
	}
}

// 装配与撤销在全局登记处上必须对称：Init(nil) 清空、Stop 清空，撤完之后新挂的路由回 503。
// 留着一个已经不该服务的全局实例，比从来没装配过更坏 —— 日志会说"未装配"而端点回 200。
func TestKBReleaseRoutes_AssemblyAndStopAreSymmetric(t *testing.T) {
	t.Cleanup(func() { service.SetGlobalKBReleaseService(nil) })

	db := kbRoutesDB(t)
	if app.InitKBReleaseRuntime(nil) != nil {
		t.Fatal("InitKBReleaseRuntime(nil) 该回 nil")
	}
	if service.GlobalKBReleaseService() != nil {
		t.Fatal("Init(nil) 之后全局仍有实例：路由会对着\"未装配\"的告警继续回 200")
	}
	if got := doBadCase(t, kbRoutesSetupWithIdentity(t, uint(7)), http.MethodGet, "/api/kb-changes", ""); got.Status != http.StatusServiceUnavailable {
		t.Errorf("清空后新挂的路由应 503，实得 %d：%s", got.Status, got.Body)
	}

	svc := app.InitKBReleaseRuntime(db)
	if svc == nil || !svc.Available() {
		t.Fatal("装好句柄后服务不可用")
	}
	if got := doBadCase(t, kbRoutesSetupWithIdentity(t, uint(7)), http.MethodGet, "/api/kb-changes", ""); got.Status != http.StatusOK {
		t.Errorf("装配后新挂的路由应 200，实得 %d：%s", got.Status, got.Body)
	}

	app.StopKBReleaseRuntime()
	if service.GlobalKBReleaseService() != nil {
		t.Error("StopKBReleaseRuntime 没清全局")
	}
	if got := doBadCase(t, kbRoutesSetupWithIdentity(t, uint(7)), http.MethodGet, "/api/kb-changes", ""); got.Status != http.StatusServiceUnavailable {
		t.Errorf("撤销后应回 503，实得 %d：%s", got.Status, got.Body)
	}
}

// 装配次序门（源码形状）。
//
// 三条次序各有各的破坏形态，且全都编译照过：
//   - InitKBReleaseRuntime 早于 InitApprovalRuntime ⇒ 变更底座自己 new 一份审批服务，
//     于是"入队用的那份"与"裁决入口读的那份"不是同一个实例：变更提交得出去、
//     审批页面上查得到，但裁决写进另一份的库里，发布永远读到 pending；
//   - setupKBReleaseRoutes 早于 InitKBReleaseRuntime ⇒ 路由挂的是 nil 快照，
//     六个写入口全 503，而日志那句"底座可用=true"是装配点出的，两边对不上；
//   - setupKBReleaseRoutes 挂到 auth 组之外 ⇒ 写入口的 operator 取不到，全 401。
//
// 与 TestRouterWiresGatedSenderToSOPLane 同一口径：运行时没有可观测差异的装配约束，
// 只能用源码形状守。
func TestKBReleaseRoutes_AssemblyOrderInRouter(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读 router.go 失败（门扫空就等于没测）: %v", err)
	}
	text := string(src)

	// 控制组：三个锚点都还在。任一处改名都会让下面的次序断言失去对象，先在这里出声。
	// 挂载那条只锚到"函数名 + 左括号"：把实参一起写进锚点会让下面那道"实参必须是 auth"
	// 的断言永远轮不到开口（实参一改，控制组先按"锚点没了"红，读起来像是函数被搬走）。
	for _, anchor := range []string{"app.InitApprovalRuntime(", "app.InitKBReleaseRuntime(", "setupKBReleaseRoutes("} {
		if n := strings.Count(text, anchor); n != 1 {
			t.Fatalf("装配锚点 %q 在 router.go 里出现 %d 次，期望恰好 1 次（0=被搬走/改名，多次=两个实例抢同一个全局）", anchor, n)
		}
	}
	approvalAt := strings.Index(text, "app.InitApprovalRuntime(")
	kbAt := strings.Index(text, "app.InitKBReleaseRuntime(")
	mountAt := strings.Index(text, "setupKBReleaseRoutes(")
	if kbAt < approvalAt {
		t.Error("KB 变更底座装在了审批运行时之前：它会自带一份审批服务，与裁决入口读的那份分家")
	}
	if mountAt < kbAt {
		t.Error("路由挂在了底座装配之前：路由拿到的是 nil 快照，六个写入口全 503")
	}

	// 必须挂在鉴权组里：六个写入口的操作者身份来自令牌，挂在组外等于匿名可写，
	// 而 requested_by / changed_by 是 AC③ 留痕的唯一出处。
	authLine := strings.Count(text, "setupKBReleaseRoutes(auth)")
	if authLine != 1 {
		t.Errorf("setupKBReleaseRoutes 的实参形状不再是 (auth)：%d 处", authLine)
	}
}

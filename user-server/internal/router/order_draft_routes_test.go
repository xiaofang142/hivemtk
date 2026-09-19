// order_draft_routes_test.go T-P2-06 ④/⑤：观察端点的渲染与状态码。
//
// 分两层测，理由与 tool_debug_routes_test.go 里那组 approvalStatePayload 用例相同：
//   - payload 是纯函数，五个分支（off / shadow / db / memory / 计数读不动）都能直接喂
//     快照出来测，不依赖装配；
//   - handler 只测两条真正决定可用性的出口：off 档要 200 且 counts 为 null，
//     装配在位但底座读不动要 503（AC⑤）。503 这条走**真的把连接关掉**来触发，
//     而不是造假快照 —— 造假的那条测的是"我写的桩会返回错误"，不是端点会回 503。
package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestSetupOrderDraftRoutes_Registered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api")
	setupOrderDraftRoutes(auth)

	var found []string
	for _, r := range engine.Routes() {
		found = append(found, r.Method+" "+r.Path)
	}
	want := "GET /api/agent/order-drafts/stats"
	for _, line := range found {
		if line == want {
			return
		}
	}
	t.Fatalf("路由未注册 %s，实际 %v", want, found)
}

func TestOrderDraftStatsPayload_NotAssembled(t *testing.T) {
	p := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode:      "off",
		Assembled: false,
		Store:     app.OrderDraftStoreNone,
	})
	if p["counts"] != nil {
		t.Errorf("未装配时 counts 必须渲染成 null，实际 %#v", p["counts"])
	}
	if _, ok := p["sweep"]; ok {
		t.Error("未装配时不该渲染 sweep 字段（interval=\"\" 会被读成\"有清扫器但没跑过\"）")
	}
	note, _ := p["note"].(string)
	if !strings.Contains(note, "没有写入方") {
		t.Errorf("note 应说明关旗的后果，实际 %q", note)
	}
	hint, _ := p["env_hint"].(string)
	if !strings.Contains(hint, app.OrderDraftFlagEnv) || !strings.Contains(hint, "off|shadow|on") {
		t.Errorf("env_hint 应带上开关名与三档取值，实际 %q", hint)
	}
	if p["store"] != app.OrderDraftStoreNone {
		t.Errorf("store=%v，期望 none（空串会被读成字段没填）", p["store"])
	}
	if p["assembled"] != false {
		t.Error("assembled 应为 false")
	}
}

func TestOrderDraftStatsPayload_Shadow(t *testing.T) {
	snap := app.OrderDraftSnapshot{
		Mode: "shadow", Assembled: true, Store: service.DraftStoreKindShadow,
		Durable: false, ProducerAttached: true,
		Counts:        map[string]int64{"pending": 4},
		SweepInterval: "6h0m0s", SweepRunning: true, SweepRounds: 2,
		Mirror: &service.OrderDraftMirrorStatus{
			Available: true, RowCounts: map[string]int64{"pending": 3}, Failures: 0,
		},
	}
	p := orderDraftStatsPayload(snap)
	if p["durable"] != false {
		t.Error("影子档必须回显 durable=false：库里那三份是镜像不是权威")
	}
	counts, ok := p["counts"].(map[string]int64)
	if !ok || counts["pending"] != 4 {
		t.Errorf("counts 应是内存侧读数 4，实际 %#v", p["counts"])
	}
	mirror, ok := p["mirror"].(gin.H)
	if !ok {
		t.Fatalf("影子档应渲染 mirror，实际 %#v", p["mirror"])
	}
	if mirror["row_counts"] == nil {
		t.Error("mirror.row_counts 应与 counts 并排给出（这两个数对不上正是灰度期要看的东西）")
	}
	sweep, ok := p["sweep"].(gin.H)
	if !ok || sweep["interval"] != "6h0m0s" || sweep["rounds"] != int64(2) {
		t.Errorf("sweep 应回显生效节拍与轮次，实际 %#v", p["sweep"])
	}
	if _, hasWarn := p["warning"]; hasWarn {
		t.Errorf("生产者已挂、镜像无故障 ⇒ 不该有 warning，实际 %#v", p["warning"])
	}
	if _, has := p["invariant_violation"]; has {
		t.Error("durable=false 时不该报不变量冲突")
	}
}

// 影子档 + Durable=true 是自相矛盾的实现，端点必须把它喊出来而不是顺着说"已持久化"。
func TestOrderDraftStatsPayload_ShadowInvariantViolation(t *testing.T) {
	p := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode: "shadow", Assembled: true, Store: service.DraftStoreKindShadow,
		Durable: true, ProducerAttached: true,
		Mirror: &service.OrderDraftMirrorStatus{Available: true},
	})
	v, ok := p["invariant_violation"].(string)
	if !ok || !strings.Contains(v, "Durable") {
		t.Fatalf("应报不变量冲突，实际 %#v", p["invariant_violation"])
	}
}

// 镜像三种故障各报各的（不混成一句"有点问题"）。
func TestOrderDraftStatsPayload_ShadowMirrorWarnings(t *testing.T) {
	cases := []struct {
		name   string
		mirror *service.OrderDraftMirrorStatus
		want   string
	}{
		{"句柄不可用", &service.OrderDraftMirrorStatus{Available: false}, "退回纯内存"},
		{"写失败累计", &service.OrderDraftMirrorStatus{Available: true, Failures: 5, LastError: "x"}, "镜像写有失败计数"},
		{"计数读不动", &service.OrderDraftMirrorStatus{Available: true, ReadError: "permission denied"}, "库侧计数读不动"},
	}
	for _, c := range cases {
		p := orderDraftStatsPayload(app.OrderDraftSnapshot{
			Mode: "shadow", Assembled: true, Store: service.DraftStoreKindShadow,
			ProducerAttached: true, Mirror: c.mirror,
		})
		got, _ := p["mirror_warning"].(string)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s：mirror_warning 应含 %q，实际 %q", c.name, c.want, got)
		}
	}
	// Mirror 为 nil（不是影子底座）时整块不渲染
	if _, ok := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode: "shadow", Assembled: true, Store: service.DraftStoreKindShadow, ProducerAttached: true,
	})["mirror"]; ok {
		t.Error("读不到镜像状态时不该渲染半个 mirror 字段")
	}
}

func TestOrderDraftStatsPayload_DBAndMemoryBranches(t *testing.T) {
	p := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode: "on", Assembled: true, Store: service.DraftStoreKindDB,
		Durable: true, ProducerAttached: true, Counts: map[string]int64{},
	})
	if _, has := p["warning_db_unavailable"]; has {
		t.Error("durable=true 时不该报句柄不可用")
	}
	if _, has := p["warning"]; has {
		t.Errorf("正常 on 态不该有 warning，实际 %#v", p["warning"])
	}
	if c, ok := p["counts"].(map[string]int64); !ok || len(c) != 0 {
		t.Errorf("空 map 应原样渲染成 {}（真的一条都没有），实际 %#v", p["counts"])
	}

	// store=db 但句柄不可用：读到的是空库，不能与"没有草稿"混为一谈
	p2 := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode: "on", Assembled: true, Store: service.DraftStoreKindDB,
		Durable: false, ProducerAttached: true, Counts: map[string]int64{},
	})
	if _, has := p2["warning_db_unavailable"]; !has {
		t.Error("store=db 且 durable=false 必须报 warning_db_unavailable")
	}

	// mode=on 却退回内存：两条故障（无句柄 + 没挂生产者）都要在 warning 里看得见
	p3 := orderDraftStatsPayload(app.OrderDraftSnapshot{
		Mode: "on", Assembled: true, Store: service.DraftStoreKindMemory, Durable: false,
	})
	warning, _ := p3["warning"].(string)
	if !strings.Contains(warning, "memory") || !strings.Contains(warning, "生产者") {
		t.Errorf("两条告警都该保留（后写的不能顶掉先写的），实际 %q", warning)
	}
}

// ------------------------------------------------------------------ handler ----

// 读一次 handler，返回 (HTTP 状态码, 解出来的 body)
func doGetDraftStats(t *testing.T) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agent/order-drafts/stats", nil)
	handleOrderDraftStats(c)

	body := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v —— %s", err, w.Body.String())
	}
	return w.Code, body
}

// AC③/④（off 侧）：关旗时端点要能答上话 —— 200 + assembled=false + counts 为 null，
// 且 JSON 里就是 null 而不是 {}（后者会被读成"库里零张草稿"）。
func TestHandleOrderDraftStats_OffReturnsNullCounts(t *testing.T) {
	t.Cleanup(app.StopOrderDraftRuntime)
	t.Setenv(app.OrderDraftFlagEnv, "off")
	if rt := app.InitOrderDraftRuntime(nil); rt != nil {
		t.Fatal("前置条件破了：off 档不应装配运行时")
	}

	code, body := doGetDraftStats(t)
	if code != http.StatusOK {
		t.Fatalf("off 档应回 200（端点在关旗时也要能答话），实际 %d：%v", code, body)
	}
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), `"counts":null`) {
		t.Errorf("counts 必须是 JSON null，实际 %s", raw)
	}
	data, _ := body["data"].(map[string]any)
	if data == nil {
		t.Fatalf("标准响应体应含 data，实际 %s", raw)
	}
	if data["assembled"] != false {
		t.Errorf("assembled=%v，期望 false", data["assembled"])
	}
	if data["store"] != app.OrderDraftStoreNone {
		t.Errorf("store=%v，期望 none", data["store"])
	}
	if _, has := data["sweep"]; has {
		t.Error("未装配时不该有 sweep")
	}
}

// AC⑤：运行时在位、但底座读不动 ⇒ 503，不回一份看起来正常的空计数。
//
// 触发方式是把真测试库的连接关掉，而不是伪造快照：这样"计数读失败"这条错误
// 是从 repository 一路穿过 service/app 到 handler 的，任一环把它吞掉本用例就会红。
func TestHandleOrderDraftStats_CountReadFailureIs503(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	t.Cleanup(app.StopOrderDraftRuntime)
	t.Setenv(app.OrderDraftFlagEnv, "on")

	rt := app.InitOrderDraftRuntime(database)
	if rt == nil {
		t.Fatal("前置条件破了：on 档应装配")
	}
	if !app.GetOrderDraftSnapshot(context.Background()).Durable {
		t.Fatal("前置条件破了：本用例要跑在 durable 底座上")
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭连接失败: %v", err)
	}

	snap := app.GetOrderDraftSnapshot(context.Background())
	if snap.CountsError == "" {
		t.Fatal("前置条件破了：连接已关，快照应带计数错误")
	}

	code, body := doGetDraftStats(t)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("计数读不动应回 503，实际 %d：%v", code, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), `"counts"`) {
		t.Errorf("503 出口不该附带任何计数（会被读成\"0 张草稿\"），实际 %s", raw)
	}
	if !strings.Contains(string(raw), "读取失败") {
		t.Errorf("错误信息要说清是哪一段读不动，实际 %s", raw)
	}
}

// 装配成功 + 底座健康 ⇒ 200，且 durable/producer 两件事都回显出来。
func TestHandleOrderDraftStats_AssembledReturns200(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	t.Cleanup(app.StopOrderDraftRuntime)
	t.Setenv(app.OrderDraftFlagEnv, "on")
	if app.InitOrderDraftRuntime(database) == nil {
		t.Fatal("on 档应装配")
	}

	code, body := doGetDraftStats(t)
	if code != http.StatusOK {
		t.Fatalf("健康态应回 200，实际 %d：%v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["durable"] != true {
		t.Errorf("durable=%v，期望 true（on 档 + 可用句柄）", data["durable"])
	}
	if data["store"] != service.DraftStoreKindDB {
		t.Errorf("store=%v，期望 db", data["store"])
	}
	if _, has := data["counts"]; !has {
		t.Error("已装配且读得到 ⇒ counts 必须存在（哪怕是 {}）")
	}
	if _, has := data["sweep"]; !has {
		t.Error("已装配 ⇒ sweep 必须回显（PurgeTerminal 的定时调用方要有可见性）")
	}
}

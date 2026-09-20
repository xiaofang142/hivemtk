package controller

// `GET /conversion-funnel` 的 HTTP 边界锁（T-P4-06 AC①/②/③）。
//
// 本卡之前这条端点上**一条 controller 用例都没有**：AC① 说的是"接口返回含商机阶段"，
// 而"接口"包括 gin 的路由、`response.Success` 那层 `{code,message,data}` 外壳、
// 以及 `parseTimeRange` 对 RFC3339 的解析。service 层的黄金用例证明了 report 的内容，
// 证不了这三处中的任何一处。
//
// 与 service 那侧的分工：那边钉逐字节响应体，这边钉"从 HTTP 进来拿到的是什么"；
// 三条 AC 各有一条落在这里，且都不依赖演示表。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	httpFunnelFrom = time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	httpFunnelTo   = time.Date(2019, 3, 31, 23, 59, 59, 0, time.UTC)
	httpFunnelIn   = time.Date(2019, 3, 10, 8, 0, 0, 0, time.UTC)
	httpFunnelOut  = time.Date(2019, 4, 20, 8, 0, 0, 0, time.UTC)
)

// setupFunnelHTTPDB 建五张真实源表 + 那张演示表（后者只为证明 AC② 的"零写入"）。
// intent_records 照 service 侧同一处理：它由迁移建表、禁止进 AutoMigrate 清单，
// 这里手写最小列集，并且**先 DROP**（测试库是进程级的，不重建就会跨用例累加）。
func setupFunnelHTTPDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&sysmodel.CustomerEvent{},
		&sysmodel.Clue{},
		&sysmodel.CustomerSession{},
		&sysmodel.Opportunity{},
		&sysmodel.ConversionFunnel{},
	)
	if err := database.Exec(`DROP TABLE IF EXISTS intent_records`).Error; err != nil {
		t.Fatalf("清理 intent_records 失败：%v", err)
	}
	if err := database.Exec(`CREATE TABLE intent_records (
		id SERIAL PRIMARY KEY,
		customer_id VARCHAR(64),
		intent_type VARCHAR(50),
		raw_text TEXT,
		confidence NUMERIC(5,4) DEFAULT 0,
		source VARCHAR(32) DEFAULT 'fine_grained',
		created_at TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("建 intent_records 沙箱表失败：%v", err)
	}
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	return database
}

// seedFunnelHTTPRows 每段窗口内各 1 条，商机段窗口内 2 条、窗口外 1 条。
// 商机是唯一"窗口内 ≠ 1"的一段，这样阶段顺序一旦错位就会体现在数字上而不只是名字上。
func seedFunnelHTTPRows(t *testing.T, database *gorm.DB) {
	t.Helper()
	must := func(table string, row map[string]any) {
		t.Helper()
		if err := database.Table(table).Create(row).Error; err != nil {
			t.Fatalf("写入 %s 失败：%v", table, err)
		}
	}
	must("customer_events", map[string]any{
		"id": "p406http-ev", "customer_id": "p406http-cust", "event_type": "page_view",
		"event_source": "web", "occurred_at": httpFunnelIn, "created_at": httpFunnelIn,
	})
	must("clues", map[string]any{
		"id": "p406http-clue", "account": "p406http-acc", "name": "p406http-线索",
		"create_time": httpFunnelIn.Unix(), "updated_at": httpFunnelIn.Unix(),
	})
	must("intent_records", map[string]any{
		"customer_id": "p406http-cust", "intent_type": "buy", "raw_text": "p406http",
		"confidence": 0.9, "created_at": httpFunnelIn,
	})
	must("customer_sessions", map[string]any{
		"session_id": "p406http-sess", "user_id": "p406http-u", "status": "closed",
		"created_at": httpFunnelIn, "updated_at": httpFunnelIn, "resolved_at": httpFunnelIn,
	})
	for i, at := range []time.Time{httpFunnelIn, httpFunnelIn, httpFunnelOut} {
		must("opportunities", map[string]any{
			"id":            "p406http-opp-" + string(rune('a'+i)),
			"code":          "P406HTTP-" + string(rune('A'+i)),
			"customer_id":   "p406http-cust",
			"stage":         "qualification",
			"status":        "open",
			"amount":        1000,
			"currency":      "CNY",
			"owner_user_id": "p406http-sales",
			"created_at":    at,
			"updated_at":    at,
		})
	}
}

func funnelGet(t *testing.T, router *gin.Engine, target string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s 应回 200，实际 %d，body=%s", target, w.Code, w.Body.String())
	}
	var envelope struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（body=%s）", err, w.Body.String())
	}
	if envelope.Code != 0 {
		t.Fatalf("外壳 code 应为 0，实际 %d（message=%s）", envelope.Code, envelope.Message)
	}
	if envelope.Data == nil {
		t.Fatalf("响应没有 data 字段（body=%s）", w.Body.String())
	}
	return envelope.Data
}

func funnelRoutes() *gin.Engine {
	ctrl := NewConversionFunnelController()
	router := gin.New()
	router.GET("/conversion-funnels", ctrl.GetFunnel)
	router.GET("/conversion-funnels/stage", ctrl.GetStageDetails)
	return router
}

// TestConversionFunnelHTTP_FunnelContainsOpportunityStage 就是 AC①：
// 从 HTTP 进来的响应里，stages[] 必须是五段、顺序不变、末位是商机。
// 顺序单独断言一次是因为前端看板按下标绘制，商机段插到中间等于挪走别人的柱子。
func TestConversionFunnelHTTP_FunnelContainsOpportunityStage(t *testing.T) {
	database := setupFunnelHTTPDB(t)
	seedFunnelHTTPRows(t, database)

	data := funnelGet(t, funnelRoutes(), "/conversion-funnels?start_time="+
		httpFunnelFrom.Format(time.RFC3339)+"&end_time="+httpFunnelTo.Format(time.RFC3339))

	raw, err := json.Marshal(data["stages"])
	if err != nil {
		t.Fatalf("stages 序列化失败：%v", err)
	}
	var stages []struct {
		Stage    string  `json:"stage"`
		Name     string  `json:"name"`
		Count    int64   `json:"count"`
		Rate     float64 `json:"rate"`
		DropRate float64 `json:"drop_rate"`
	}
	if err := json.Unmarshal(raw, &stages); err != nil {
		t.Fatalf("stages 反序列化失败：%v", err)
	}
	want := []struct {
		stage string
		name  string
		count int64
	}{
		{"visit", "访问", 1}, {"clue", "线索", 1}, {"intent", "意向", 1},
		{"session", "会话", 1}, {"opportunity", "商机", 2},
	}
	if len(stages) != len(want) {
		t.Fatalf("阶段数应为 %d，实际 %d（%s）", len(want), len(stages), raw)
	}
	for i, w := range want {
		if stages[i].Stage != w.stage || stages[i].Name != w.name || stages[i].Count != w.count {
			t.Fatalf("第 %d 段应为 %s/%s/%d，实际 %s/%s/%d",
				i, w.stage, w.name, w.count, stages[i].Stage, stages[i].Name, stages[i].Count)
		}
	}

	// 详情侧同理（前端别名 `/conversion-funnel/stage` 走的就是这条）。
	det := funnelGet(t, funnelRoutes(), "/conversion-funnels/stage?stage=opportunity&start_time="+
		httpFunnelFrom.Format(time.RFC3339)+"&end_time="+httpFunnelTo.Format(time.RFC3339))
	if det["name"] != "商机" || det["count"] != float64(2) {
		t.Fatalf("stage=opportunity 详情应为 商机/2，实际 %v/%v", det["name"], det["count"])
	}
}

// TestConversionFunnelHTTP_DemoTableStaysEmpty 是 AC② 在 HTTP 边界的版本：
// 两条读请求打完，演示表仍须 0 行。摘掉路由或把取数改成写那张表，这条都会红。
func TestConversionFunnelHTTP_DemoTableStaysEmpty(t *testing.T) {
	database := setupFunnelHTTPDB(t)
	seedFunnelHTTPRows(t, database)

	count := func() int64 {
		var n int64
		if err := database.Table("conversion_funnels").Count(&n).Error; err != nil {
			t.Fatalf("统计演示表失败：%v", err)
		}
		return n
	}
	if before := count(); before != 0 {
		t.Fatalf("用例自己把演示表弄脏了：请求前有 %d 行", before)
	}

	router := funnelRoutes()
	q := "?start_time=" + httpFunnelFrom.Format(time.RFC3339) + "&end_time=" + httpFunnelTo.Format(time.RFC3339)
	funnelGet(t, router, "/conversion-funnels"+q)
	funnelGet(t, router, "/conversion-funnels/stage?stage=opportunity"+q)

	if after := count(); after != 0 {
		t.Fatalf("读漏斗往演示表里写了 %d 行（AC② 破了，R-4 收口失效）", after)
	}
}

// TestConversionFunnelHTTP_DemoRowsDoNotLeakIntoResponse 是 AC③ 的 HTTP 版本：
// 演示表里灌 99999 之后，接口读数一字不变。
func TestConversionFunnelHTTP_DemoRowsDoNotLeakIntoResponse(t *testing.T) {
	database := setupFunnelHTTPDB(t)
	seedFunnelHTTPRows(t, database)

	router := funnelRoutes()
	q := "?start_time=" + httpFunnelFrom.Format(time.RFC3339) + "&end_time=" + httpFunnelTo.Format(time.RFC3339)
	before, err := json.Marshal(funnelGet(t, router, "/conversion-funnels"+q))
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}

	for i, st := range []string{"visit", "clue", "intent", "session", "opportunity", "deal"} {
		row := &sysmodel.ConversionFunnel{
			StatDate: "2019-03-10", FunnelType: "sales", Stage: st, StageOrder: i,
			Count: 99999, ConversionRate: 99.99, DropOffRate: 99.99,
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("写演示表行 %s 失败：%v", st, err)
		}
	}

	after, err := json.Marshal(funnelGet(t, router, "/conversion-funnels"+q))
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	// generated_at 每次都不同 ⇒ 先确认差异只可能来自它，再逐字段比 stages。
	var b, a map[string]any
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	if string(mustJSON(t, b["stages"])) != string(mustJSON(t, a["stages"])) {
		t.Fatalf("演示表灌了 6×99999 行之后接口读数变了（AC③ 破了）\n前：%s\n后：%s",
			string(mustJSON(t, b["stages"])), string(mustJSON(t, a["stages"])))
	}
	if string(mustJSON(t, b["stages"])) == "null" {
		t.Fatal("stages 字段没取到，本条比较是空的")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	return raw
}

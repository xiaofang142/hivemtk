package service

// 转化漏斗真实源的行为锁（T-P2-03 / R-4）。
//
// 本卡对 `BuildFunnel` / `GetStageDetails` 只做了"字面量换成词表常量"的机械改写，
// 卡面 AC① 要求"返回值不变"。这类"我保证没改行为"的口头承诺不值钱，所以这里
// 把响应按**具体数值**钉住：窗口固定、行由本文件自己种，因此计数、比率、顺序、
// 字段名全都是确定的。同一份文件在改动前的基线树上跑一遍、改动后跑一遍，
// 两边输出逐字相等才算 AC① 成立（执行结果里报的正是这个差分）。
//
// 因此本文件**刻意不引用** `repository.FunnelStageKey` 等新符号 —— 引了就编译不到基线树。

import (
	"encoding/json"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 窗口刻意选在 2019 年 3 月：远离"最近 30 天"的默认窗口，不与任何按 now 取数的用例
// 重叠；窗口外还各种一行，用来证明"时间过滤真的在过滤"。
var (
	funnelFrom  = time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC)
	funnelTo    = time.Date(2019, 3, 31, 23, 59, 59, 0, time.UTC)
	funnelIn1   = time.Date(2019, 3, 5, 10, 0, 0, 0, time.UTC)
	funnelIn2   = time.Date(2019, 3, 20, 12, 0, 0, 0, time.UTC)
	funnelOut   = time.Date(2019, 2, 1, 8, 0, 0, 0, time.UTC)
	funnelOut2  = time.Date(2019, 4, 15, 8, 0, 0, 0, time.UTC)
	funnelLabel = "tp203"
)

func setupFunnelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&sysmodel.CustomerEvent{},
		&sysmodel.Clue{},
		&sysmodel.CustomerSession{},
	)
	// intent_records 由迁移建表、被明确禁止进 AutoMigrate 清单（见 model/intent_log.go 头部
	// 那句"否则会重建 intent_logs 造成双表回潮"）⇒ 这里自建一个最小列集，
	// 只覆盖漏斗真正用到的 created_at 与 intent_type 两列。
	// 先 DROP 再建：测试库是**进程级**的（同包多个用例共用），而 NewTestDB 只 DROP
	// 它自己 models 参数里列出的那几张表 —— intent_records 不在其中，不重建就会跨用例累加
	// （首轮实跑即因此把"全年窗口 intent=2"读成 6）。
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

// seedFunnelRows 种下窗口内 5 访问 / 3 线索 / 2 意向（其中只有 1 条命中购买意向词表）
// / 4 会话，窗口外各 1 条。这组数字是下面所有断言的分母，改一行就要一起改期望值。
func seedFunnelRows(t *testing.T, database *gorm.DB) {
	t.Helper()

	mustCreate := func(table string, row map[string]any) {
		t.Helper()
		if err := database.Table(table).Create(row).Error; err != nil {
			t.Fatalf("写入 %s 失败：%v", table, err)
		}
	}

	// 窗口内 4 条 + 窗口外 1 条的访问事件（用同秒不同分钟区分，避免任何"取整后重叠"）
	for i, at := range []time.Time{funnelIn1, funnelIn1, funnelIn2, funnelIn2, funnelOut} {
		mustCreate("customer_events", map[string]any{
			"id":           funnelLabel + "-ev-" + string(rune('a'+i)),
			"customer_id":  funnelLabel + "-cust",
			"event_type":   "page_view",
			"event_source": "web",
			"occurred_at":  at,
			"created_at":   at,
		})
	}

	// 线索按 account 分组用于 TopSources：accA 两条、accB 一条，窗口外再一条 accC
	for i, at := range []time.Time{funnelIn1, funnelIn2, funnelIn2, funnelOut} {
		account := funnelLabel + "-accA"
		if i == 1 {
			account = funnelLabel + "-accB"
		}
		if i == 2 {
			account = funnelLabel + "-accA"
		}
		mustCreate("clues", map[string]any{
			"id":          funnelLabel + "-clue-" + string(rune('a'+i)),
			"account":     account,
			"name":        funnelLabel + "-线索",
			"create_time": at.Unix(),
			"updated_at":  at.Unix(),
		})
	}

	// 意向：1 条命中 BuildFunnel 用的词表（buy），1 条不命中（question）；窗口外再 1 条 buy
	mustCreateIntent := func(intentType string, at time.Time) {
		mustCreate("intent_records", map[string]any{
			"customer_id": funnelLabel + "-cust",
			"intent_type": intentType,
			"raw_text":    funnelLabel + "-原文",
			"confidence":  0.9,
			"created_at":  at,
		})
	}
	mustCreateIntent("buy", funnelIn1)
	mustCreateIntent("question", funnelIn2)
	mustCreateIntent("buy", funnelOut)

	// 会话：窗口内 4 条（session_id 唯一）+ 窗口外 1 条
	for i, at := range []time.Time{funnelIn1, funnelIn1, funnelIn2, funnelIn2, funnelOut2} {
		mustCreate("customer_sessions", map[string]any{
			"session_id":  funnelLabel + "-sess-" + string(rune('a'+i)),
			"user_id":     funnelLabel + "-u",
			"status":      "closed",
			"created_at":  at,
			"updated_at":  at,
			"resolved_at": at,
		})
	}
}

func TestConversionFunnel_BuildFunnel_GoldenContract(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)

	svc := NewConversionFunnelService()
	report, err := svc.BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}

	got := stripGeneratedAt(t, report)
	want := `{"start_time":"2019-03-01T00:00:00Z","end_time":"2019-03-31T23:59:59Z",` +
		`"stages":[` +
		`{"stage":"visit","name":"访问","count":4,"rate":100,"drop_rate":0},` +
		`{"stage":"clue","name":"线索","count":3,"rate":75,"drop_rate":25},` +
		`{"stage":"intent","name":"意向","count":1,"rate":33.33333333333333,"drop_rate":66.66666666666667},` +
		`{"stage":"session","name":"会话","count":4,"rate":400,"drop_rate":-300}],` +
		`"total":4,"conversion":100}`
	if got != want {
		t.Fatalf("漏斗响应与基线不一致\n实际：%s\n期望：%s", got, want)
	}
}

// TestConversionFunnel_BuildFunnel_NonMonotonicStagesBaseline 把一处真实存在的口径问题钉住：
// 阶段之间不保证单调（会话数可以大于意向数），于是 Rate 可以 >100、DropRate 可以为负
// —— 上面那条黄金期望里的 400 / -300 就是它。本卡不改（改了就是改现网读数），
// 登记见 docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md §4.11 与短板 G16。
func TestConversionFunnel_BuildFunnel_NonMonotonicStagesBaseline(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)

	report, err := NewConversionFunnelService().BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}
	last := report.Stages[len(report.Stages)-1]
	if last.Rate <= 100 || last.DropRate >= 0 {
		t.Fatalf("期望登记住「比率超 100 / 跌幅为负」的现状，实际 rate=%v drop=%v ⇒ 口径已变，回灌本卡",
			last.Rate, last.DropRate)
	}
}

// TestConversionFunnel_BuildFunnel_WindowFilter 单独证明"窗口外的那一行没被算进来"。
// 黄金期望里已经隐含了这一点，但一旦有人把窗口改成"全表 count"，那条断言只会说
// "数字不对"，而这条会直接说出是谁漏了。
//
// 关键是**两个窗口都要查**：只查全年窗口等于把"漏了过滤"和"过滤正确"混成同一个读数
// （变异实测：只查全年时把 visit 的时间窗整个删掉，这条仍然绿）。
// 每个阶段各埋一条窗口外数据，所以 全年 − 当月 必须恰好为 1。
func TestConversionFunnel_BuildFunnel_WindowFilter(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)

	svc := NewConversionFunnelService()
	all, err := svc.BuildFunnel(
		time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}
	narrow, err := svc.BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("BuildFunnel（当月窗口）出错：%v", err)
	}
	counts := func(rep *FunnelReport) map[string]int64 {
		m := map[string]int64{}
		for _, s := range rep.Stages {
			m[s.Stage] = s.Count
		}
		return m
	}
	allCount, monthCount := counts(all), counts(narrow)

	// 全年窗口 = 窗口内 + 各自那一条窗口外数据。
	// intent 走 BuildFunnel 自己的词表（buy/purchase/order/interested），
	// 故全年 = 当月 1 条 buy + 窗口外 1 条 buy = 2，那条 question 两边都不算。
	wantAll := map[string]int64{"visit": 5, "clue": 4, "intent": 2, "session": 5}
	wantMonth := map[string]int64{"visit": 4, "clue": 3, "intent": 1, "session": 4}
	for stage, want := range wantAll {
		if allCount[stage] != want {
			t.Errorf("全年窗口下 %s 应为 %d（含窗口外那条），实际 %d", stage, want, allCount[stage])
		}
		if monthCount[stage] != wantMonth[stage] {
			t.Errorf("当月窗口下 %s 应为 %d，实际 %d ⇒ 窗口过滤漏了", stage, wantMonth[stage], monthCount[stage])
		}
		if diff := allCount[stage] - monthCount[stage]; diff != 1 {
			t.Errorf("%s 全年与当月只差 1 条窗口外数据，实际差 %d ⇒ 时间窗未生效", stage, diff)
		}
	}
}

// TestConversionFunnel_GetStageDetails_EachStage 覆盖详情的四个分支：
// 意向分支刻意传 nil 词表 ⇒ 它是 2（不筛 intent_type），与漏斗里的 1 形成对照；
// 线索分支还要看 TopSources 的排序（按 count 降序）。
func TestConversionFunnel_GetStageDetails_EachStage(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)

	svc := NewConversionFunnelService()
	cases := []struct {
		stage   string
		name    string
		count   int64
		sources int
	}{
		{"visit", "访问", 4, 0},
		{"clue", "线索", 3, 2},
		{"intent", "意向", 2, 0},
		{"session", "会话", 4, 0},
	}
	for _, c := range cases {
		det, err := svc.GetStageDetails(c.stage, funnelFrom, funnelTo)
		if err != nil {
			t.Fatalf("%s 详情出错：%v", c.stage, err)
		}
		if det.Name != c.name || det.Count != c.count {
			t.Errorf("%s 详情：期望 name=%s count=%d，实际 name=%s count=%d",
				c.stage, c.name, c.count, det.Name, det.Count)
		}
		if len(det.TopSources) != c.sources {
			t.Errorf("%s 详情：期望 TopSources %d 条，实际 %d 条（%+v）",
				c.stage, c.sources, len(det.TopSources), det.TopSources)
		}
	}

	det, err := svc.GetStageDetails("clue", funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("clue 详情出错：%v", err)
	}
	if len(det.TopSources) == 2 &&
		(det.TopSources[0].Source != funnelLabel+"-accA" || det.TopSources[0].Count != 2) {
		t.Errorf("TopSources 应按 count 降序，实际 %+v", det.TopSources)
	}
}

// TestConversionFunnel_GetStageDetails_UnknownStageIsBaseline 未知阶段名（含演示表那套
// exposure/click、以及只定名未产出的 opportunity）保持现状：200 + 空名字 + 0 计数，
// **不判 404**。本卡若改成报错就是行为变更，故把现状锁在这里，
// 要动它得先有一次"前端能否接受报错"的判定（短板 G16 一并登记）。
func TestConversionFunnel_GetStageDetails_UnknownStageIsBaseline(t *testing.T) {
	setupFunnelTestDB(t)

	for _, stage := range []string{"exposure", "click", "opportunity", "", "VISIT"} {
		det, err := NewConversionFunnelService().GetStageDetails(stage, funnelFrom, funnelTo)
		if err != nil {
			t.Fatalf("未知阶段 %q 竟返回错误：%v（现状应是 200 + 空详情）", stage, err)
		}
		if det.Stage != stage || det.Name != "" || det.Count != 0 {
			t.Fatalf("未知阶段 %q 应回空详情，实际 %+v", stage, det)
		}
	}
}

// TestConversionFunnel_BuildFunnel_DefaultWindow 不传时间时取"最近 30 天"：
// 只断言跨度（30 天 ±1 分钟），不断言墙钟，免得跨时区/闰秒抖。
func TestConversionFunnel_BuildFunnel_DefaultWindow(t *testing.T) {
	setupFunnelTestDB(t)

	report, err := NewConversionFunnelService().BuildFunnel(time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}
	span := report.EndTime.Sub(report.StartTime)
	if d := time.Duration(30) * 24 * time.Hour; span < d-time.Minute || span > d+time.Minute {
		t.Fatalf("默认窗口应约 30 天，实际 %s", span)
	}
}

// stripGeneratedAt 把报告序列化成 JSON 后摘掉 generated_at（每次调用都不同，留着就是噪声），
// 再按 Go 的 map 序列化顺序无关的比较口径返回**原字段顺序**的字符串。
func stripGeneratedAt(t *testing.T, report *FunnelReport) string {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if _, ok := obj["generated_at"]; !ok {
		t.Fatalf("响应里少了 generated_at 字段 ⇒ 契约漂移（key: %v）", keysOf(obj))
	}
	delete(obj, "generated_at")

	// 逐字段按固定顺序拼回来，避免依赖 map 的随机序。
	out := "{"
	for i, k := range []string{"start_time", "end_time", "stages", "total", "conversion"} {
		v, ok := obj[k]
		if !ok {
			t.Fatalf("响应里少了 %q 字段 ⇒ 契约漂移（key: %v）", k, keysOf(obj))
		}
		out += `"` + k + `":` + string(v)
		if i < 4 {
			out += ","
		}
	}
	for k := range obj {
		if k != "start_time" && k != "end_time" && k != "stages" && k != "total" && k != "conversion" {
			t.Fatalf("响应里多出字段 %q ⇒ 契约漂移（key: %v）", k, keysOf(obj))
		}
	}
	return out + "}"
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

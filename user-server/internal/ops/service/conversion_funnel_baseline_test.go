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
//
// 【T-P4-06 起本文件的判据换了一次】上面那句"两边输出逐字相等才算 AC① 成立"是 R-4 那张卡
// 的 AC，它的验收对象是"机械改写不改行为"。本卡的 AC① 要的是**响应必须多出一段**，
// 那条差分从此不再成立（黄金期望里第五段就是它变更的地方）。不引用新符号这条**保留**，
// 但理由换了：期望值写**字面值**才钉得住契约 —— 引用常量的话，有人把常量值从
// "opportunity" 改成 "opp"，用例仍会跟着一起绿，而那已经是改契约了。

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
		&sysmodel.Opportunity{},
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
// / 4 会话 / 3 商机，窗口外各 1 条。这组数字是下面所有断言的分母，改一行就要一起改期望值。
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

	// 商机（T-P4-06 加的第五段）：窗口内 3 条 + 窗口外 1 条。
	// 时间戳必须显式给：这一列是本段的**切窗依据**，交给 GORM 的 autoCreateTime 就等于
	// 用"这条用例什么时候跑"当期望值。
	for i, at := range []time.Time{funnelIn1, funnelIn2, funnelIn2, funnelOut} {
		mustCreate("opportunities", map[string]any{
			"id":            funnelLabel + "-opp-" + string(rune('a'+i)),
			"code":          funnelLabel + "-CODE-" + string(rune('a'+i)),
			"customer_id":   funnelLabel + "-cust",
			"stage":         "qualification",
			"status":        "open",
			"amount":        1000,
			"currency":      "CNY",
			"owner_user_id": funnelLabel + "-sales",
			"created_at":    at,
			"updated_at":    at,
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
		`{"stage":"session","name":"会话","count":4,"rate":400,"drop_rate":-300},` +
		`{"stage":"opportunity","name":"商机","count":3,"rate":75,"drop_rate":25}],` +
		`"total":4,"conversion":100}`
	if got != want {
		t.Fatalf("漏斗响应与基线不一致\n实际：%s\n期望：%s", got, want)
	}
	// 第五段**只加段、不改旧账**：total 仍是访问量、conversion 仍是"访问→会话"。
	// 这两格不是顺带断言 —— 前端摘要区今日读的就是它们（见 user-web 那个 view 里的
	// stages[0] / stages[len-1]），服务端口径若跟着末段挪，"端到端转化率"会在无人改
	// 前端的那个发版里从"访问→会话"悄悄变成"访问→商机"。
	if report.Total != 4 {
		t.Errorf("total 应仍是访问量 4，实际 %d ⇒ 汇总口径跟着新段漂移了", report.Total)
	}
	if report.Conversion != 100 {
		t.Errorf("conversion 应仍是 访问→会话 = 100，实际 %v ⇒ 末段变更把 KPI 换了定义", report.Conversion)
	}
}

// TestConversionFunnel_BuildFunnel_NonMonotonicStagesBaseline 把一处真实存在的口径问题钉住：
// 阶段之间不保证单调（会话数可以大于意向数），于是 Rate 可以 >100、DropRate 可以为负
// —— 上面那条黄金期望里的 400 / -300 就是它。本卡不改（改了就是改现网读数），
// 登记见 docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md §4.11 与短板 G16。
//
// T-P4-06 把"取最后一段"改成"按键名取会话段"，这不是风格调整：加了第五段之后
// `Stages[len-1]` 会从会话变成商机（rate 75、drop 25），这条登记现状的用例会
// **因为绿着而失去意义** —— 它断的已经不是它说的那件事了。同一把刀也砍在前端摘要区，
// 那里原先同样按下标取首末段（`stages[0]` / `stages[stages.length-1]`），本卡一并改成
// 按阶段名取，见 user-web/src/views/conversionFunnel/List.vue。
func TestConversionFunnel_BuildFunnel_NonMonotonicStagesBaseline(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)

	report, err := NewConversionFunnelService().BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}
	var last *FunnelStage
	for i := range report.Stages {
		if report.Stages[i].Stage == "session" {
			last = &report.Stages[i]
		}
	}
	if last == nil {
		t.Fatal("响应里没有会话段，本条断言无从落点（阶段名漂移了？）")
	}
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
	wantAll := map[string]int64{"visit": 5, "clue": 4, "intent": 2, "session": 5, "opportunity": 4}
	wantMonth := map[string]int64{"visit": 4, "clue": 3, "intent": 1, "session": 4, "opportunity": 3}
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

// TestConversionFunnel_GetStageDetails_EachStage 覆盖详情的五个分支：
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
		{"opportunity", "商机", 3, 0},
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

// TestConversionFunnel_GetStageDetails_UnknownStageIsBaseline 未知阶段名（演示表那套
// exposure/click 等）保持现状：200 + 空名字 + 0 计数，**不判 404**。本卡若改成报错就是
// 行为变更，故把现状锁在这里，要动它得先有一次"前端能否接受报错"的判定（短板 G16 一并登记）。
//
// T-P4-06 之前这份名单里还有 "opportunity"：那时它**只是定名未产出**，回空详情是正确现状。
// 现在它已经有取数腿（见上面 EachStage 那条的第五行），留在未登记名单里会让这条用例把
// "商机详情回空"重新扶成期望 —— 那是拿一条过期判据去否决本卡的 AC①，所以是**移出去**
// 而不是把断言改松。
func TestConversionFunnel_GetStageDetails_UnknownStageIsBaseline(t *testing.T) {
	setupFunnelTestDB(t)

	for _, stage := range []string{"exposure", "click", "consult", "add_wecom", "deal", "", "VISIT"} {
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

// TestConversionFunnel_DemoTableRowsNeverMoveTheRealView 是 AC③ 的**视图层**落点。
// 取数层那条同名用例只证明"计数来自真表"，这一条要证明的是整份响应：
// 演示表里那些 stage='opportunity'、count=99999 的行就算存在（`cmd/seed` 天天在造），
// 真实视图也一个字都不许变 —— 包括别把已经存在的那几个演示段名扶成"看得见的阶段"。
//
// 与取数层那条**不能合并成一条**：这里的失败面不是"读错表"，是"读对了表但把演示表当补充"
// （比如有人加一段 `if 真表为 0 { 回退读演示表 }`）—— 那条在取数层用例里种的是假行，
// 真表恰好也有数，照样绿。
func TestConversionFunnel_DemoTableRowsNeverMoveTheRealView(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)
	if err := database.AutoMigrate(&sysmodel.ConversionFunnel{}); err != nil {
		t.Fatalf("建演示表失败：%v", err)
	}
	for i, st := range []string{"opportunity", "deal", "visit", "clue"} {
		row := &sysmodel.ConversionFunnel{
			StatDate: "2019-03-05", FunnelType: "sales", Stage: st, StageOrder: i,
			Count: 99999, ConversionRate: 99.99, DropOffRate: 99.99,
		}
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("写演示表行 %s 失败：%v", st, err)
		}
	}

	report, err := NewConversionFunnelService().BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("BuildFunnel 出错：%v", err)
	}
	got := stripGeneratedAt(t, report)
	want := `{"start_time":"2019-03-01T00:00:00Z","end_time":"2019-03-31T23:59:59Z",` +
		`"stages":[` +
		`{"stage":"visit","name":"访问","count":4,"rate":100,"drop_rate":0},` +
		`{"stage":"clue","name":"线索","count":3,"rate":75,"drop_rate":25},` +
		`{"stage":"intent","name":"意向","count":1,"rate":33.33333333333333,"drop_rate":66.66666666666667},` +
		`{"stage":"session","name":"会话","count":4,"rate":400,"drop_rate":-300},` +
		`{"stage":"opportunity","name":"商机","count":3,"rate":75,"drop_rate":25}],` +
		`"total":4,"conversion":100}`
	if got != want {
		t.Fatalf("演示表里灌了 4×99999 行之后真实视图变了（AC③ 破了）\n实际：%s\n期望：%s", got, want)
	}
}

// TestConversionFunnel_OpportunityLegFailureKeepsTheOtherFour 定住本卡的失败面形状：
// 商机段取数失败（表没建 / 列漂移 / 连接断）时，响应**照样 200、照样五段**，那一段读 0。
//
// 为什么不是 500：四段既有腿今日就是同一个口径（错误在 service 层被吞，短板 G16 已登记），
// 把第五段做成"它挂了整页挂"会让一个刚接上的读方拥有比四个老的更大的爆炸半径，
// 而看板的代价是不对称的 —— 报错是**四段都看不见**，回 0 只是**一段读成零**。
// 两边的分工因此是：取数层必须报错（见 ops/repository 那条同名判据），
// 服务层把"报出来的错"变成可观测的一条 Warn 而不是一个静默零。
// 这条用例正是那把变异（把错误 `continue` 掉、连 Warn 都不打）与"错误直接上抛"两种改法共同的绊线。
func TestConversionFunnel_OpportunityLegFailureKeepsTheOtherFour(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)
	if err := database.Exec(`DROP TABLE IF EXISTS opportunities`).Error; err != nil {
		t.Fatalf("移除 opportunities 失败：%v", err)
	}

	report, err := NewConversionFunnelService().BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("商机段取数失败被上抛成整份报告的错误：%v ⇒ 一段故障挪用了四段的可见性", err)
	}
	if len(report.Stages) != 5 {
		t.Fatalf("失败时段的段数变了：%d 段（%v）⇒ 少一段会把前端按下标取的末段整体挪位", len(report.Stages), stageKeys(report))
	}
	counts := map[string]int64{}
	for _, s := range report.Stages {
		counts[s.Stage] = s.Count
	}
	if counts["opportunity"] != 0 {
		t.Errorf("表不存在却报出 %d 条商机", counts["opportunity"])
	}
	// 其余四段必须**照旧有数**：这条断言防的是"一遇到错就把整份报告清零"那种修法，
	// 它能让上面两条都绿，代价是把四个真数换成四个假零。
	for stage, want := range map[string]int64{"visit": 4, "clue": 3, "intent": 1, "session": 4} {
		if counts[stage] != want {
			t.Errorf("%s 段应为 %d，实际 %d ⇒ 一段故障波及到了别的段", stage, want, counts[stage])
		}
	}
}

func stageKeys(report *FunnelReport) []string {
	out := make([]string, 0, len(report.Stages))
	for _, s := range report.Stages {
		out = append(out, s.Stage)
	}
	return out
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

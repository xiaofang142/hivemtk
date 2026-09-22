// quote_test.go T-P6-02：报价生成服务（模板 + 行项目 + 话术分片）。
//
// 本文件只测**这一层才成立**的事。列的形状与复合唯一索引在 T-P6-01 的
// repository / pkg-db 两层已经钉过，这里一律不重测；这里钉的是三条 AC：
//
//	AC① 话术必经版本/灰度：正文只能来自 `script_versions` 的**生效版本快照**，
//	     既不是 `script_library.content` 那个可变列，也不是调用方递进来的字符串
//	     （入参结构体里压根没有这个字段，由反射用例钉住"以后也不许加"）。
//	AC② 生成出来的是草稿：本层的依赖面里**没有**状态跃迁方法（quoteStore 是
//	     QuoteRepository 去掉 UpdateStatus 的那一份），所以"未过闸门就发出去"
//	     不是被约定挡住，是被类型挡住。
//	AC③ 行项目金额合计与落库一致：每行先舍到分再落库，合计 = 库里那些行相加；
//	     "先把未舍的行加起来再舍一次"会得出另一个数，用例里把它算出来放在旁边。
//
// 另有一处刻意的失败面：**闸门关着 / 模板没配 / 话术没注册 ⇒ 一行都不写**。
// 这三条各自的退路都是往下游发一份没有依据的价格文件，代价不对等。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 夹具 ——————————————————————————————————————————————————————————————

// qsClockBase 显式时钟：valid_days 推出来的有效期、生成键里的时间戳都从它算。
// 依赖 time.Now 的用例在跨 UTC 小时的机器上会换答案（日期边界裂脑的老账）。
var qsClockBase = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

const (
	qsTemplateKey = QuoteTemplateKVPrefix + "std_annual"
	// 模板两份内容：一份完整（含 valid_days 与两条行），一份留给各用例改写。
	qsTemplateJSON = `{"name":"标准版年费","currency":"CNY","valid_days":30,"lines":[` +
		`{"product_id":"p_seat","title":"标准版席位","quantity":10,"unit_price":199.00,"discount_percent":10},` +
		`{"product_id":"p_impl","title":"实施服务","quantity":1,"unit_price":8000.00,"discount_percent":5}]}`
	// 无币种、无有效期的最小模板。
	qsTemplateBare = `{"lines":[{"product_id":"p_seat","title":"标准版席位","quantity":10,` +
		`"unit_price":199.00,"discount_percent":10},` +
		`{"product_id":"p_impl","title":"实施服务","quantity":1,"unit_price":8000.00,"discount_percent":5}]}`
)

type qsLineWant struct {
	productID string
	title     string
	qty       float64
	unit      float64
	disc      float64
	gross     float64
	amount    float64
}

func qsF(v float64) *float64 { return &v }
func qsS(v string) *string   { return &v }

func qsSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ScriptLibrary{}, &model.ScriptVersion{},
	)
}

// qsConfig 是 system_config_kv 的替身：只给一张表，缺键回空串（与真仓储的
// RecordNotFound ⇒ ("", nil) 同一口径 —— 服务必须分得开"没配"与"读失败"，
// 后一种由 qsBrokenConfig 单独造）。
type qsConfig map[string]string

func (c qsConfig) Get(_ context.Context, key string) (string, error) { return c[key], nil }

type qsBrokenConfig struct{ err error }

func (b qsBrokenConfig) Get(context.Context, string) (string, error) { return "", b.err }

// qsKeyReadFailure 只让指定的那几个键读失败，其余照常返回。
//
// 整份存储一起坏的话，"失败发生在哪一格"就读不出来了 —— 而本层要证的正是
// 两格各自的读失败都不许被报成那一格的「没配」。
type qsKeyReadFailure struct {
	qsConfig
	bad map[string]bool
}

func (k qsKeyReadFailure) Get(ctx context.Context, key string) (string, error) {
	if k.bad[key] {
		return "", errors.New("connection reset by peer")
	}
	return k.qsConfig.Get(ctx, key)
}

// qsScripts 话术端口的替身：记录被问到的 (scriptID, oneID)，返回固定正文。
type qsScripts struct {
	gotID    uint
	gotOneID string
	script   QuoteScript
	err      error
}

func (s *qsScripts) ActiveQuoteScript(_ context.Context, id uint, oneID string) (QuoteScript, error) {
	s.gotID, s.gotOneID = id, oneID
	if s.err != nil {
		return QuoteScript{}, s.err
	}
	return s.script, nil
}

// qsGate 直接递一份 *LTCConfig（同 convGate 的口径：不经存储、不经缓存）。
type qsGate struct{ cfg *LTCConfig }

func (g qsGate) Config(context.Context) *LTCConfig { return g.cfg }

func qsGateOn() *LTCConfig {
	return &LTCConfig{
		Enabled:       true,
		StagesEnabled: LTCStages{}.With(LTCStageQuote),
		Thresholds:    DefaultLTCConfig().Thresholds,
	}
}

// qsSvc 装配一台"什么都能干"的服务：真库 + 固定时钟 + 模板 + 一条已生效的话术指针。
// 各用例按需把某个端口换成坏的那一份 —— 好的一侧必须先齐，否则"坏了才报"这件事
// 无从判起（全坏的服务对任何输入都报错，那样的绿不值钱）。
func qsSvc(t *testing.T, db *gorm.DB) (*QuoteService, qsConfig, *qsScripts) {
	t.Helper()
	cfg := qsConfig{qsTemplateKey: qsTemplateJSON, QuoteScriptIDKVKey: "77"}
	scripts := &qsScripts{script: QuoteScript{ScriptID: 77, Version: 3, Bucket: "A", Content: "这是第三版生效话术"}}
	svc := NewQuoteService(
		repository.NewQuoteRepositoryWithDB(db),
		repository.NewOpportunityRepositoryWithDB(db),
		qsGate{qsGateOn()},
	)
	svc.SetClock(func() time.Time { return qsClockBase })
	svc.SetConfigStore(cfg)
	svc.SetScriptSource(scripts)
	return svc, cfg, scripts
}

func qsSeedOpportunity(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	repo := repository.NewOpportunityRepositoryWithDB(db)
	row := &model.Opportunity{
		ID: id, Code: "OPP-QS-" + id, CustomerID: "cus_qs", OneID: "one_1",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_a",
		CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
	}
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
}

func qsInput(opp string) QuoteGenerateInput {
	return QuoteGenerateInput{OpportunityID: opp, OneID: "one_1", TemplateCode: "std_annual"}
}

// qsRow / qsLines 读回一律用**独立零值 struct**：复用已填充的 struct 再 First()
// 会把旧字段并进 WHERE，那是本仓 gorm dest 复用坑的假红形状。
func qsRow(t *testing.T, db *gorm.DB, id string) *model.Quote {
	t.Helper()
	var got model.Quote
	if err := db.WithContext(context.Background()).First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("按 %s 读报价版本行失败：%v", id, err)
	}
	return &got
}

func qsLines(t *testing.T, db *gorm.DB, rowID string) []model.QuoteLineItem {
	t.Helper()
	var rows []model.QuoteLineItem
	if err := db.WithContext(context.Background()).
		Where("quote_row_id = ?", rowID).Order("line_no ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读 %s 的行项目失败：%v", rowID, err)
	}
	return rows
}

func qsCountAll(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(context.Background()).Table(table).Count(&n).Error; err != nil {
		t.Fatalf("数 %s 失败：%v", table, err)
	}
	return n
}

// qsSumAmount 合计的**库侧真值**：直接 SUM 那一版的行。
// AC③ 的判据是"服务报的数 == 这个数"，而不是"服务报的数 == 服务自己算的数"。
func qsSumAmount(t *testing.T, db *gorm.DB, rowID string) float64 {
	t.Helper()
	var sum *float64
	if err := db.WithContext(context.Background()).
		Raw(`SELECT SUM(amount) FROM quote_line_items WHERE quote_row_id = ?`, rowID).
		Scan(&sum).Error; err != nil {
		t.Fatalf("SUM(amount) 失败：%v", err)
	}
	if sum == nil {
		return 0
	}
	return *sum
}

func qsSame(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// qsSameLineContent 比两行的**内容列**。
//
// 刻意不比 QuoteRowID（第二版那行当然挂在第二版上）也不比 CreatedAt
// （这一行是这次新建的，它的时刻就是此刻）—— 要比的是"抄过来的行还是那行"。
func qsSameLineContent(a, b model.QuoteLineItem) bool {
	return a.LineNo == b.LineNo && a.ProductID == b.ProductID && a.Title == b.Title &&
		a.Quantity == b.Quantity && a.UnitPrice == b.UnitPrice &&
		a.DiscountPercent == b.DiscountPercent && a.Amount == b.Amount
}

// —— AC②：本层的形状 ————————————————————————————————————————————————

// TestQuoteServiceStoreSurfaceHasNoStatusWriter AC② 的**类型层**判据。
//
// 仓储那边有 UpdateStatus（T-P6-01 交付的三条写路径之一），本层若把它接进来，
// "生成即草稿、发送必经闸门"（C1 的定级）就只剩一句注释守着。所以本层依赖的是一个
// **刻意窄掉**的接口，这里点名列集合：以后有人往 quoteStore 里加状态相关方法，
// 这一条先红，逼他去看 P6-03 那条闸门该长在哪。
func TestQuoteServiceStoreSurfaceHasNoStatusWriter(t *testing.T) {
	typ := reflect.TypeOf((*quoteStore)(nil)).Elem()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		names = append(names, name)
		if strings.Contains(name, "Status") || strings.HasPrefix(name, "Update") ||
			strings.HasPrefix(name, "Delete") || strings.HasPrefix(name, "Save") {
			t.Errorf("quoteStore 上有 %s：本层不许改写报价状态或删行（跃迁归 T-P6-03 的闸门）", name)
		}
	}
	want := map[string]bool{
		"Available": true, "Create": true, "Append": true, "AddLines": true,
		"GetByID": true, "GetVersion": true, "Latest": true, "ListVersions": true, "ListLines": true,
	}
	if len(names) != len(want) {
		t.Errorf("quoteStore 方法数 %d，期望 %d：%v", len(names), len(want), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("quoteStore 多出未登记的方法 %s", n)
		}
	}
}

// TestQuoteGenerateInputHasNoScriptField 「必经」的另一半：没有旁路可递正文。
//
// 只要入参里能塞话术，就一定有人塞 —— 那时版本/灰度/过期三判据全被绕过，
// 而库里那行看起来与走了正路的一模一样。字段名集合按白名单逐字比。
func TestQuoteGenerateInputHasNoScriptField(t *testing.T) {
	typ := reflect.TypeOf(QuoteGenerateInput{})
	want := map[string]bool{
		"OpportunityID": true, "OneID": true, "TemplateCode": true,
		"Currency": true, "ValidUntil": true, "Lines": true,
	}
	if typ.NumField() != len(want) {
		t.Errorf("入参字段数 %d，期望 %d", typ.NumField(), len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !want[name] {
			t.Errorf("入参出现未登记字段 %s", name)
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "script") || strings.Contains(lower, "content") ||
			strings.Contains(lower, "version") || strings.Contains(lower, "text") {
			t.Errorf("字段 %s 读起来像「话术可以自己给」，这与 AC① 冲突", name)
		}
	}
}

// —— AC①：话术必经版本 ————————————————————————————————————————————————

// TestQuoteService_ScriptResolvedByConfiguredIDAndOneID 服务只递「哪个分片 + 给谁」，
// 版本与正文由端口决定；端口不接 DB 句柄，也就无从绕过快照。
func TestQuoteService_ScriptResolvedByConfiguredIDAndOneID(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_svc_1")
	svc, _, scripts := qsSvc(t, db)

	view, err := svc.Generate(context.Background(), qsInput("opp_svc_1"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if scripts.gotID != 77 || scripts.gotOneID != "one_1" {
		t.Errorf("端口收到的是 (script=%d, one=%q)，期望 (77, one_1)", scripts.gotID, scripts.gotOneID)
	}
	if view.Script.Version != 3 || view.Script.Content != "这是第三版生效话术" {
		t.Errorf("视图里的话术不是端口返回的那一份：%+v", view.Script)
	}
}

// TestQuoteService_WithoutScriptPointerWritesNothing 话术分片没注册 ⇒ 不生成。
//
// 反面是"先生成、发送时再补话术"，那会让一张没有依据的价格文件先进库、
// 再被 P6-03 的读侧当成可发对象捞走 —— 失败必须发生在写之前。
func TestQuoteService_WithoutScriptPointerWritesNothing(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_svc_2")
	ctx := context.Background()
	svc, cfg, _ := qsSvc(t, db)
	delete(cfg, QuoteScriptIDKVKey)

	if _, err := svc.Generate(ctx, qsInput("opp_svc_2")); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Fatalf("话术未注册应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	if n := qsCountAll(t, db, "quotes"); n != 0 {
		t.Errorf("话术不可用时写了 %d 行报价，期望 0", n)
	}

	// 指针配成了一个不像 ID 的值：也是「不可用」，不是「当成 0 号话术」。
	cfg[QuoteScriptIDKVKey] = "七十七"
	if _, err := svc.Generate(ctx, qsInput("opp_svc_2")); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("话术指针非数字应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	// 端口本身报错（分片被删 / 已过期）同样不许落行。
	cfg[QuoteScriptIDKVKey] = "77"
	svc2, _, _ := qsSvc(t, db)
	svc2.SetScriptSource(&qsScripts{err: ErrQuoteScriptUnavailable})
	if _, err := svc2.Generate(ctx, qsInput("opp_svc_2")); err == nil {
		t.Error("端口报错时 Generate 竟然成功")
	}
	if n := qsCountAll(t, db, "quotes"); n != 0 {
		t.Errorf("端口报错后又写了 %d 行，期望仍是 0", n)
	}
}

// TestQuoteScriptSource_ActiveSnapshotIsTheOnlyTextSource AC① 的**真库**判据，四臂。
//
//  1. 正文取自 script_versions 的快照 ⇒ 改可变列不影响已生效版本；
//  2. status=expired / expires_at 已过 ⇒ 明确不可用（不是回退到 content 列）；
//  3. 生效指针指向一个没有快照的版本 ⇒ 不可用，而不是「那就先用可变列那份」。
//     这一臂最值钱：CreateVersion 之前的日常编辑恰好是这种状态，
//     而「先用着、等有快照再切」等于把未经版本化的正文发给客户。
//  4. 分片行不存在 ⇒ 同一类错（回 nil 会让上层写出一行带空话术的报价）。
func TestQuoteScriptSource_ActiveSnapshotIsTheOnlyTextSource(t *testing.T) {
	db := qsSetupDB(t)
	ctx := context.Background()
	repo := repository.NewScriptLibraryRepository(db)
	src := NewQuoteScriptSource(repo, NewScriptABService(repo), func() time.Time { return qsClockBase })

	seed := func(id uint, status string, version int, expires *time.Time, content string) {
		row := &model.ScriptLibrary{
			ID: id, Category: "quote", Subcategory: "cover", Title: "报价封面话术",
			Content: content, Version: version, Status: status, ExpiresAt: expires,
			CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
		}
		if err := db.WithContext(ctx).Create(row).Error; err != nil {
			t.Fatalf("造话术行失败：%v", err)
		}
	}
	// seedVersion 给生效指针配一份快照：让"闸门在拦"成为报错的**唯一**原因。
	seedVersion := func(scriptID uint, version int, content string) {
		if err := db.WithContext(ctx).Create(&model.ScriptVersion{
			ScriptID: scriptID, Version: version, Title: "报价封面话术", Content: content,
			Status: "active", CreatedAt: qsClockBase,
		}).Error; err != nil {
			t.Fatalf("造版本快照失败：%v", err)
		}
	}
	// ① active 且指针 3 有快照。
	seed(1, "active", 3, nil, "可变列当前值")
	seedVersion(1, 3, "快照里的第三版")
	got, err := src.ActiveQuoteScript(ctx, 1, "one_1")
	if err != nil {
		t.Fatalf("读生效话术失败：%v", err)
	}
	if got.Content != "快照里的第三版" {
		t.Errorf("正文取自 %q，期望取版本快照里的文本", got.Content)
	}
	if got.Version != 3 {
		t.Errorf("版本号 %d，期望跟着生效指针 3", got.Version)
	}
	if got.Bucket != "A" && got.Bucket != "B" {
		t.Errorf("分桶 %q 不是 A/B：分桶必须复用 script_ab 那一个函数", got.Bucket)
	}
	if err := db.WithContext(ctx).Model(&model.ScriptLibrary{}).
		Where("id = ?", 1).Update("content", "有人手改了可变列").Error; err != nil {
		t.Fatalf("改可变列失败：%v", err)
	}
	again, err := src.ActiveQuoteScript(ctx, 1, "one_1")
	if err != nil || again.Content != "快照里的第三版" {
		t.Errorf("改了可变列后正文跟着变了：content=%q err=%v", again.Content, err)
	}

	// ②两种过期形状都要拒：状态位是运营手动下线，expires_at 是定时到点。
	//
	// 两种都**必须自带生效快照**：没有快照的话，读它会在"指针找不到快照"那一步就报
	// 同一个错，本步想断言的那道闸（状态位 / 到期时刻）坏掉了也照样绿 —— 变异实测：
	// 把 expires_at 那判整条摘掉，本用例原来一样过。闸门要单独能被证明在拦。
	seedVersion(2, 1, "已下线的旧话术也有正文")
	seed(2, "expired", 1, nil, "已下线的旧话术")
	if _, err := src.ActiveQuoteScript(ctx, 2, "one_1"); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("status=expired 应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	past := qsClockBase.Add(-time.Hour)
	seedVersion(3, 1, "到期话术也有正文")
	seed(3, "active", 1, &past, "到期话术")
	if _, err := src.ActiveQuoteScript(ctx, 3, "one_1"); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("expires_at 已过应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	// ③指针没有对应快照。
	future := qsClockBase.Add(time.Hour)
	seed(4, "active", 1, &future, "快照还没打")
	if _, err := src.ActiveQuoteScript(ctx, 4, "one_1"); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("生效指针无快照应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	// ④分片不存在。
	if _, err := src.ActiveQuoteScript(ctx, 999, "one_1"); !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("分片不存在应报 ErrQuoteScriptUnavailable，实际 %v", err)
	}
	// 空 one_id：分桶无从谈起，但正文照样可用（报价可以先挂在商机上、还没定位到人）。
	noBucket, err := src.ActiveQuoteScript(ctx, 1, "")
	if err != nil || noBucket.Bucket != "" {
		t.Errorf("one_id 为空时应回空桶且不报错：bucket=%q err=%v", noBucket.Bucket, err)
	}
}

// TestQuoteScriptSource_BucketIsTheSameFunctionAsAB 分桶不许有第二份实现。
//
// script_ab.go 的 FNV 判据是曝光归因的分桶依据；报价侧若自己算一遍（哪怕今天
// 结果相同），明天改了 SplitA 语义就会分成两拨话术人群，而 AB 报表读不出差异来源。
func TestQuoteScriptSource_BucketIsTheSameFunctionAsAB(t *testing.T) {
	db := qsSetupDB(t)
	ctx := context.Background()
	repo := repository.NewScriptLibraryRepository(db)
	if err := db.WithContext(ctx).Create(&model.ScriptLibrary{
		ID: 5, Category: "quote", Title: "封面", Content: "x", Version: 1, Status: "active",
	}).Error; err != nil {
		t.Fatalf("造话术行失败：%v", err)
	}
	if err := db.WithContext(ctx).Create(&model.ScriptVersion{
		ScriptID: 5, Version: 1, Title: "封面", Content: "生效正文", Status: "active",
	}).Error; err != nil {
		t.Fatalf("造快照失败：%v", err)
	}
	ab := NewScriptABService(repo)
	cfg := ab.GetConfig(ctx, 5)
	src := NewQuoteScriptSource(repo, ab, func() time.Time { return qsClockBase })
	for _, oneID := range []string{"one_a", "one_b", "one_c", "one_d", "one_e"} {
		got, err := src.ActiveQuoteScript(ctx, 5, oneID)
		if err != nil {
			t.Fatalf("读 %s 的话术失败：%v", oneID, err)
		}
		if want := ab.AssignBucket(5, oneID, cfg); got.Bucket != want {
			t.Errorf("one_id=%s 分桶 %q 与 AssignBucket 的 %q 不符", oneID, got.Bucket, want)
		}
	}
}

// —— AC③：金额与合计 ————————————————————————————————————————————————

// TestQuoteService_LineArithmeticIsCentRoundedPerRow 每行各自舍到分，且落的就是那个数。
//
// 期望值全部手工算好、且刻意选二进制能精确表示的数（100.25 / 0.875 / 0.125），
// 否则用例测的是 float64 的表示误差而不是本层的算法。
func TestQuoteService_LineArithmeticIsCentRoundedPerRow(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_math")
	svc, cfg, _ := qsSvc(t, db)
	cfg[qsTemplateKey] = `{"currency":"CNY","lines":[` +
		`{"product_id":"a","title":"甲","quantity":3,"unit_price":100.25,"discount_percent":12.5},` +
		`{"product_id":"b","title":"乙","quantity":0.5,"unit_price":0.25,"discount_percent":0},` +
		`{"product_id":"c","title":"丙","quantity":1,"unit_price":12345678.90,"discount_percent":100}]}`

	view, err := svc.Generate(context.Background(), qsInput("opp_math"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	// 甲：3×100.25 = 300.75；×(1−0.125) = 263.15625 → 263.16（半分向上）
	// 乙：0.5×0.25 = 0.125 → 0.13（恰好压在半分上，这是本用例的命门）
	// 丙：折扣 100% ⇒ 净额 0（合法，赠送行；库里必须写 0.00 而不是 NULL）
	want := []qsLineWant{
		{"a", "甲", 3, 100.25, 12.5, 300.75, 263.16},
		{"b", "乙", 0.5, 0.25, 0, 0.13, 0.13},
		{"c", "丙", 1, 12345678.90, 100, 12345678.90, 0},
	}
	if len(view.Lines) != len(want) {
		t.Fatalf("行数 %d，期望 %d", len(view.Lines), len(want))
	}
	rows := qsLines(t, db, view.ID)
	if len(rows) != len(want) {
		t.Fatalf("库里的行数 %d 与视图不一致", len(rows))
	}
	for i, w := range want {
		got, row := view.Lines[i], rows[i]
		if got.ProductID != w.productID || got.Title != w.title {
			t.Errorf("第 %d 行身份不对：%+v", i+1, got)
		}
		if row.LineNo != int64(i+1) {
			t.Errorf("第 %d 行的 line_no 是 %d（行号必须从 1 连续）", i+1, row.LineNo)
		}
		if got.Gross != w.gross || row.Quantity != w.qty || row.UnitPrice != w.unit {
			t.Errorf("第 %d 行输入项不符：view.gross=%v row.qty=%v row.unit=%v（期望 gross=%v）",
				i+1, got.Gross, row.Quantity, row.UnitPrice, w.gross)
		}
		if got.Amount != w.amount || row.Amount != w.amount {
			t.Errorf("第 %d 行净额 view=%v 库里=%v，期望 %v", i+1, got.Amount, row.Amount, w.amount)
		}
		if row.DiscountPercent != w.disc {
			t.Errorf("第 %d 行折扣 %v，期望 %v", i+1, row.DiscountPercent, w.disc)
		}
	}
	if !qsSame(view.GrossTotal, 12345979.78) || !qsSame(view.Total, 263.29) {
		t.Errorf("合计 gross=%v total=%v，期望 12345979.78 / 263.29（每行舍完再相加）",
			view.GrossTotal, view.Total)
	}
	if !qsSame(view.Total, qsSumAmount(t, db, view.ID)) {
		t.Errorf("服务报的合计 %v 与库侧 SUM(amount) %v 不一致", view.Total, qsSumAmount(t, db, view.ID))
	}
}

// TestQuoteService_TotalRoundsAfterPerLineNotOnce AC③ 的反面数值。
//
// 三行真值各 0.004：逐行舍 ⇒ 0.00 + 0.00 + 0.00 = 0.00；
// 先把未舍的值加起来再舍 ⇒ round2(0.012) = 0.01。两个数差一分，
// 而「这张报价的总价是多少」必须只有一个答案 —— 答案是库里那三行相加。
// 用例把另一个数**当场算出来**放在旁边，比写一句「另一种做法会不同」可靠。
func TestQuoteService_TotalRoundsAfterPerLineNotOnce(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_round")
	svc, cfg, _ := qsSvc(t, db)
	line := `{"product_id":"micro","title":"零头","quantity":0.4,"unit_price":0.01,"discount_percent":0}`
	cfg[qsTemplateKey] = fmt.Sprintf(`{"currency":"CNY","lines":[%s,%s,%s]}`, line, line, line)

	view, err := svc.Generate(context.Background(), qsInput("opp_round"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	sumThenRound := math.Round(3*0.4*0.01*100) / 100
	if qsSame(view.Total, sumThenRound) {
		t.Fatalf("合计成了「先加总再舍」的结果 %v，本层要的是逐行舍完再相加", view.Total)
	}
	if view.Total != 0 {
		t.Errorf("合计 %v，期望 0（三行各自都是 0.00）", view.Total)
	}
	if got := qsSumAmount(t, db, view.ID); !qsSame(got, view.Total) {
		t.Errorf("库侧 SUM=%v 与服务合计 %v 不一致", got, view.Total)
	}
	for i, l := range view.Lines {
		if l.Amount != 0 {
			t.Errorf("第 %d 行净额 %v，期望 0.00", i+1, l.Amount)
		}
	}
}

// TestQuoteService_HalfCentLineRoundsHalfUpInDecimal 半分那一格要向上，而不是被二进制拽下来。
//
// 0.5 × 1.15 的真值是 0.575 —— 两个输入都是本列宽内的常规价（半个月、半只席位），
// 落在半分上不是巧合而是常态。它在 float64 里是 0.57499999999999995551，
// 于是 `math.Round(v*100)/100` 得到 0.57，而同一张单子在库里 numeric(14,2) 那一侧
// 是 0.58（PG 按**十进制**半分向上；实测见下）。差的那一分会印到客户手里那张纸上。
//
// 期望值写 0.58 而不是"跟库里一样"：库里那份是这道题的**答案**，不是**判据** ——
// 服务先把 0.57 递进去，列就存 0.57，两边自洽而都错。所以这里把十进制真值算出来钉住。
func TestQuoteService_HalfCentLineRoundsHalfUpInDecimal(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_half")
	svc, cfg, _ := qsSvc(t, db)
	cfg[qsTemplateKey] = `{"currency":"CNY","lines":[` +
		`{"product_id":"p_half","title":"半月席位","quantity":0.5,"unit_price":1.15,"discount_percent":0},` +
		`{"product_id":"p_quarter","title":"四分之三折行","quantity":1,"unit_price":0.145,"discount_percent":0}]}`

	view, err := svc.Generate(context.Background(), qsInput("opp_half"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	// 十进制真值：0.5×1.15 = 0.575 → 0.58；1×0.145 = 0.145 → 0.15（都是半分向上）。
	want := []qsLineWant{{"p_half", "半月席位", 0.5, 1.15, 0, 0.58, 0.58}, {"p_quarter", "四分之三折行", 1, 0.15, 0, 0.15, 0.15}}
	rows := qsLines(t, db, view.ID)
	if len(rows) != len(want) {
		t.Fatalf("库里行数 %d，期望 %d", len(rows), len(want))
	}
	for i, w := range want {
		if view.Lines[i].Gross != w.gross {
			t.Errorf("第 %d 行毛额 view=%v，期望 %v（半分向上按十进制取）", i+1, view.Lines[i].Gross, w.gross)
		}
		if view.Lines[i].Amount != w.amount {
			t.Errorf("第 %d 行净额 view=%v，期望 %v", i+1, view.Lines[i].Amount, w.amount)
		}
		if rows[i].Amount != w.amount {
			t.Errorf("第 %d 行落库净额=%v，期望 %v（这一列是 numeric(14,2)，存进去的就是那位数）",
				i+1, rows[i].Amount, w.amount)
		}
	}
	if !qsSame(view.Total, 0.73) {
		t.Errorf("合计 %v，期望 0.73（0.58 + 0.15）", view.Total)
	}
	if !qsSame(view.Total, qsSumAmount(t, db, view.ID)) {
		t.Errorf("服务合计 %v 与库侧 SUM %v 不一致", view.Total, qsSumAmount(t, db, view.ID))
	}
}

// TestQuoteService_ViewRecomputesFromPersistedRows 读侧的合计必须来自库里的行。
//
// 判据不是"再调一次 View 得到同一个数"（两份内存视图自洽就能骗过去），
// 而是往库里直接塞一行：读侧必须把它并进合计。
func TestQuoteService_ViewRecomputesFromPersistedRows(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_view")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)
	view, err := svc.Generate(ctx, qsInput("opp_view"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	before := view.Total
	if err := db.WithContext(ctx).Create(&model.QuoteLineItem{
		QuoteRowID: view.ID, LineNo: 3, Title: "手工加的行", Quantity: 1, UnitPrice: 500, Amount: 500,
	}).Error; err != nil {
		t.Fatalf("插行失败：%v", err)
	}
	late, err := svc.View(ctx, view.ID)
	if err != nil {
		t.Fatalf("读视图失败：%v", err)
	}
	if !qsSame(late.Total, before+500) {
		t.Errorf("读侧合计 %v，期望 %v（库里多了一行 500 而视图没跟着动 = 合计另有来源）",
			late.Total, before+500)
	}
	if len(late.Lines) != 3 {
		t.Errorf("读侧行数 %d，期望 3：%+v", len(late.Lines), late.Lines)
	}
	if late.Lines[2].Title != "手工加的行" {
		t.Errorf("手工行的行序不对：%+v", late.Lines[2])
	}
}

// —— 模板 / 入参 ————————————————————————————————————————————————

// TestQuoteService_TemplateMergeZeroOverridesAndAddedLines 模板行的三条合并规则各一臂。
//
// ①只改数量：单价与折扣仍取模板那份；
// ②**显式 0 折扣**把模板里的 5% 抹掉 —— 这一臂是本用例的全部价值。
// 按「非零才覆盖」实现的合并会把 0 读成「没给」，于是「客户还价、回到原价」
// 这个最常见的动作做不出来，而且不报错，只是折扣悄悄还在。
// ③模板外的行追加在尾部，必须自带品名与单价（无目录可查，静默补空标题会让
// 报价单上出现一行不知道是什么的东西）。
func TestQuoteService_TemplateMergeZeroOverridesAndAddedLines(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_merge")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	view, err := svc.Generate(ctx, QuoteGenerateInput{
		OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
		Lines: []QuoteLineInput{
			{ProductID: "p_seat", Quantity: qsF(20)},
			{ProductID: "p_impl", DiscountPercent: qsF(0)},
			{ProductID: "p_extra", Title: qsS("第三方数据源"), Quantity: qsF(2), UnitPrice: qsF(1500)},
		},
	})
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if len(view.Lines) != 3 {
		t.Fatalf("行数 %d，期望 3：%+v", len(view.Lines), view.Lines)
	}
	seat, impl, extra := view.Lines[0], view.Lines[1], view.Lines[2]
	if seat.Quantity != 20 || seat.UnitPrice != 199 || seat.DiscountPercent != 10 {
		t.Errorf("席位行合并错了：%+v（只改数量，单价与折扣该留模板那份）", seat)
	}
	if impl.DiscountPercent != 0 || impl.Amount != 8000 {
		t.Errorf("实施行的显式 0 折扣没生效：%+v", impl)
	}
	if extra.LineNo != 3 || extra.Title != "第三方数据源" || extra.Amount != 3000 {
		t.Errorf("模板外的行没追加到尾部：%+v", extra)
	}
	if seat.Gross != 3980 || impl.Gross != 8000 || extra.Gross != 3000 {
		t.Errorf("折前小计不对：seat=%v impl=%v extra=%v", seat.Gross, impl.Gross, extra.Gross)
	}
	if !qsSame(view.Total, 14582) {
		t.Errorf("合计 %v，期望 14582（3582 + 8000 + 3000）", view.Total)
	}

	// 六种越界入参各拒一次。
	for name, in := range map[string]QuoteGenerateInput{
		"缺品名": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_new", Quantity: qsF(1), UnitPrice: qsF(10)}}},
		"缺单价": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_new", Title: qsS("新行"), Quantity: qsF(1)}}},
		"负数量": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_seat", Quantity: qsF(-1)}}},
		"负折扣": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_seat", DiscountPercent: qsF(-0.01)}}},
		"折扣超界": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_seat", DiscountPercent: qsF(100.01)}}},
		"折扣 NaN": {OpportunityID: "opp_merge", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_seat", DiscountPercent: qsF(math.NaN())}}},
	} {
		if _, err := svc.Generate(ctx, in); !errors.Is(err, ErrQuoteInputInvalid) {
			t.Errorf("%s 应报 ErrQuoteInputInvalid，实际 %v", name, err)
		}
	}
	// 越界的折扣不许「悄悄夹到 100」，也不许留下半截行项目。
	if n := len(qsLines(t, db, view.ID)); n != 3 {
		t.Errorf("被拒的六次生成里有人写了行项目（现 %d 行）", n)
	}
	if n := qsCountAll(t, db, "quotes"); n != 1 {
		t.Errorf("被拒的六次生成写出了 %d 行报价，期望仍是 1", n)
	}
}

// TestQuoteService_TemplateMissingOrBroken 模板这一格的三种坏法分得开。
//
// 「没配」（去配）、「配坏了」（去改配置）、「读不到」（去看库）是三种处置动作，
// 合成一个 error 就只能三选一地标错。
func TestQuoteService_TemplateMissingOrBroken(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_tpl")
	ctx := context.Background()

	svc, cfg, _ := qsSvc(t, db)
	delete(cfg, qsTemplateKey)
	if _, err := svc.Generate(ctx, qsInput("opp_tpl")); !errors.Is(err, ErrQuoteTemplateMissing) {
		t.Errorf("模板未配置应报 ErrQuoteTemplateMissing，实际 %v", err)
	}

	broken := map[string]string{
		"不是 JSON": `{`,
		"零行":      `{"currency":"CNY","lines":[]}`,
		"行缺标题":    `{"currency":"CNY","lines":[{"product_id":"p","quantity":1,"unit_price":1}]}`,
		"行单价为负":   `{"currency":"CNY","lines":[{"product_id":"p","title":"x","quantity":1,"unit_price":-1}]}`,
		"币种列放不下":  `{"currency":"人民币","lines":[{"product_id":"p","title":"x","quantity":1,"unit_price":1}]}`,
		"有效期为负天数": `{"currency":"CNY","valid_days":-5,"lines":[{"product_id":"p","title":"x","quantity":1,"unit_price":1}]}`,
		"行数量为零":   `{"currency":"CNY","lines":[{"product_id":"p","title":"x","quantity":0,"unit_price":1}]}`,
	}
	for name, raw := range broken {
		s2, c2, _ := qsSvc(t, db)
		c2[qsTemplateKey] = raw
		if _, err := s2.Generate(ctx, qsInput("opp_tpl")); !errors.Is(err, ErrQuoteTemplateInvalid) {
			t.Errorf("%s 应报 ErrQuoteTemplateInvalid，实际 %v", name, err)
		}
	}
	if n := qsCountAll(t, db, "quotes"); n != 0 {
		t.Errorf("模板坏这一路写了 %d 行报价，期望 0", n)
	}

	s3, _, _ := qsSvc(t, db)
	s3.SetConfigStore(qsBrokenConfig{err: errors.New("connection reset by peer")})
	_, err := s3.Generate(ctx, qsInput("opp_tpl"))
	if err == nil {
		t.Fatal("配置读失败被当成了成功")
	}
	if errors.Is(err, ErrQuoteTemplateMissing) {
		t.Errorf("读失败报成了配置缺失（处置动作完全不同）：%v", err)
	}
	if errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Errorf("整份配置读不通报成了「话术没注册」（那是去配，这是去修存储）：%v", err)
	}

	// 单键读失败：两格各自都必须落在「读不到」这一类，而不是那一格的「没配」。
	// 整份坏掉时失败发生在哪一格读不出来，所以这里逐键造。
	for _, key := range []string{QuoteScriptIDKVKey, qsTemplateKey} {
		s4, c4, _ := qsSvc(t, db)
		s4.SetConfigStore(qsKeyReadFailure{qsConfig: c4, bad: map[string]bool{key: true}})
		if _, err := s4.Generate(ctx, qsInput("opp_tpl")); err == nil {
			t.Fatalf("键 %s 读失败被当成了成功", key)
		} else if errors.Is(err, ErrQuoteTemplateMissing) || errors.Is(err, ErrQuoteScriptUnavailable) {
			t.Errorf("键 %s 读失败报成了配置缺失（去配 ≠ 去修存储）：%v", key, err)
		}
	}
}

// TestQuoteService_CurrencyAndExpiryResolution 币种与有效期的各级兜底。
func TestQuoteService_CurrencyAndExpiryResolution(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_cur")
	ctx := context.Background()

	// 请求没给币种 ⇒ 取模板那份。
	svc, _, _ := qsSvc(t, db)
	view, err := svc.Generate(ctx, qsInput("opp_cur"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if view.Currency != "CNY" || qsRow(t, db, view.ID).Currency != "CNY" {
		t.Errorf("币种没取模板那份：view=%q", view.Currency)
	}
	// 模板也没给 ⇒ 取建表默认（两处必须同源，否则就是两个默认值）。
	s2, c2, _ := qsSvc(t, db)
	c2[qsTemplateKey] = qsTemplateBare
	v2, err := s2.Generate(ctx, qsInput("opp_cur"))
	if err != nil {
		t.Fatalf("模板没写币种时生成失败：%v", err)
	}
	if v2.Currency != model.QuoteCurrencyDefault {
		t.Errorf("模板没写币种时应落默认币种：cur=%q", v2.Currency)
	}

	// 请求给的币种：三位小写码规范化（它是对外码位不是自由文本），其余形状拒。
	s3, _, _ := qsSvc(t, db)
	in := qsInput("opp_cur")
	in.Currency = "usd"
	v3, err := s3.Generate(ctx, in)
	if err != nil {
		t.Fatalf("小写三位码规范化那条生成失败：%v", err)
	}
	if v3.Currency != "USD" {
		t.Errorf("币种规范化失败：got=%q，期望 USD", v3.Currency)
	}
	if qsRow(t, db, v3.ID).Currency != "USD" {
		t.Error("规范化后的币种没落进库")
	}
	for _, bad := range []string{"USDT", "cn", "人民币", "C N", "CNY1"} {
		inB := qsInput("opp_cur")
		inB.Currency = bad
		if _, err := s3.Generate(ctx, inB); !errors.Is(err, ErrQuoteInputInvalid) {
			t.Errorf("币种 %q 越界应报 ErrQuoteInputInvalid，实际 %v", bad, err)
		}
	}

	// 有效期三级：请求显式 > 模板 valid_days > 不设（NULL）。
	if view.ValidUntil == nil || !view.ValidUntil.Equal(qsClockBase.AddDate(0, 0, 30)) {
		t.Errorf("valid_days=30 没从固定时钟推出有效期：%v", view.ValidUntil)
	}
	manual := qsClockBase.Add(72 * time.Hour)
	inU := qsInput("opp_cur")
	inU.ValidUntil = &manual
	v4, err := s3.Generate(ctx, inU)
	if err != nil {
		t.Fatalf("显式有效期那条生成失败：%v", err)
	}
	if v4.ValidUntil == nil || !v4.ValidUntil.Equal(manual) {
		t.Errorf("请求显式有效期没赢过模板推导：%v，期望 %v", v4.ValidUntil, manual)
	}
	s5, c5, _ := qsSvc(t, db)
	c5[qsTemplateKey] = qsTemplateBare
	v5, err := s5.Generate(ctx, qsInput("opp_cur"))
	if err != nil {
		t.Fatalf("无有效期模板生成失败：%v", err)
	}
	if v5.ValidUntil != nil {
		t.Errorf("模板没给天数时应回 nil，实际 %v", *v5.ValidUntil)
	}
	var nulls int64
	if err := db.WithContext(ctx).
		Raw(`SELECT COUNT(*) FROM quotes WHERE id = ? AND valid_until IS NULL`, v5.ID).
		Scan(&nulls).Error; err != nil || nulls != 1 {
		t.Errorf("未设有效期必须是 NULL（零值时间会被「早已过期」的读侧吞掉）：err=%v n=%d", err, nulls)
	}
}

// —— 版本链 / 闸门 / 装配 ————————————————————————————————————————————————

// TestQuoteService_GenerateFirstVersionShape AC② 的落库形状：一版、草稿、无来路。
func TestQuoteService_GenerateFirstVersionShape(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_shape")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	view, err := svc.Generate(ctx, qsInput("opp_shape"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if view.Version != model.QuoteVersionFirst || view.Status != model.QuoteStatusDraft {
		t.Errorf("第一版该是 version=1 status=draft，实际 %d %q", view.Version, view.Status)
	}
	row := qsRow(t, db, view.ID)
	if row.SourceID != "" || row.Status != model.QuoteStatusDraft || row.OpportunityID != "opp_shape" {
		t.Errorf("库里那行不对：source=%q status=%q opp=%q", row.SourceID, row.Status, row.OpportunityID)
	}
	if row.QuoteID != view.QuoteID || !strings.HasPrefix(view.QuoteID, "QT-") {
		t.Errorf("报价编号 %q 不以 QT- 开头（人要在电话里念它）", view.QuoteID)
	}
	if !strings.HasPrefix(view.ID, "q_") {
		t.Errorf("版本行 ID %q 不以 q_ 开头（model 注释里承诺的形状）", view.ID)
	}
	if len(view.ID) > 64 || len(view.QuoteID) > 64 {
		t.Errorf("键长超列宽：id=%d quote_id=%d", len(view.ID), len(view.QuoteID))
	}
	// 同一时刻两次生成也不许撞键（同一纳秒靠 seq 分开）。
	again, err := svc.Generate(ctx, qsInput("opp_shape"))
	if err != nil {
		t.Fatalf("第二次生成失败：%v", err)
	}
	if again.ID == view.ID || again.QuoteID == view.QuoteID {
		t.Errorf("两次生成撞键：%q/%q 与 %q/%q", view.ID, view.QuoteID, again.ID, again.QuoteID)
	}
}

// TestQuoteService_GateClosedWritesNothing 域开关（ltc.config 的 quote 阶段）关 ⇒ 一行不写。
//
// 三种关法各报各的原因：总闸关、这一档没开、配置降级 —— 运营看到「没生成」时
// 要能分清是哪一个，否则第一个动作就是去翻代码。
func TestQuoteService_GateClosedWritesNothing(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_gate")
	ctx := context.Background()

	cases := map[string]*LTCConfig{
		"这一档没开": {Enabled: true, StagesEnabled: LTCStages{}.With(LTCStageBill),
			Thresholds: DefaultLTCConfig().Thresholds},
		"总闸关": {Enabled: false, StagesEnabled: LTCStages{}.With(LTCStageQuote),
			Thresholds: DefaultLTCConfig().Thresholds},
		"配置降级": {Enabled: true, Degraded: true, DegradeReason: "读不到 ltc.config",
			StagesEnabled: LTCStages{}.With(LTCStageQuote), Thresholds: DefaultLTCConfig().Thresholds},
		"配置为 nil": nil,
	}
	for name, cfg := range cases {
		svc, _, _ := qsSvc(t, db)
		svc.gate = qsGate{cfg}
		_, err := svc.Generate(ctx, qsInput("opp_gate"))
		if !errors.Is(err, ErrQuoteGateClosed) {
			t.Errorf("%s：应报 ErrQuoteGateClosed，实际 %v", name, err)
			continue
		}
		if !strings.Contains(err.Error(), "阶段") {
			t.Errorf("%s：报错文案没带上可处置的原因：%v", name, err)
		}
	}
	if n := qsCountAll(t, db, "quotes"); n != 0 {
		t.Errorf("闸门关着时写了 %d 行报价，期望 0", n)
	}
	// 闸门只挡写路径：已生成的那一版照常读得到，否则关一次开关就把
	// 历史报价从运营眼前抹掉了。
	on, _, _ := qsSvc(t, db)
	created, err := on.Generate(ctx, qsInput("opp_gate"))
	if err != nil {
		t.Fatalf("开闸后生成失败：%v", err)
	}
	on.gate = qsGate{nil}
	if got, err := on.View(ctx, created.ID); err != nil || got == nil || got.ID != created.ID {
		t.Errorf("关闸后读侧应照常：%+v err=%v", got, err)
	}
	if _, err := on.Revise(ctx, created.QuoteID, QuoteGenerateInput{OneID: "one_1", TemplateCode: "std_annual"}); !errors.Is(err, ErrQuoteGateClosed) {
		t.Errorf("关闸后还价也应被挡住，实际 %v", err)
	}
}

// TestQuoteService_OpportunityMustExist 报价挂在商机上：商机不存在就没什么可挂。
//
// 报错而不是照写 —— 挂空的行在漏斗与闭环率里读不出来，而它长得完全正常。
// 「已生成的报价被改了归属」这一臂也在下面：还价不许顺手把单子挪到别的商机名下。
func TestQuoteService_OpportunityMustExist(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_here")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	in := qsInput("opp_missing")
	if _, err := svc.Generate(ctx, in); !errors.Is(err, ErrQuoteOpportunityMissing) {
		t.Errorf("商机不存在应报 ErrQuoteOpportunityMissing，实际 %v", err)
	}
	if _, err := svc.Generate(ctx, qsInput("   ")); !errors.Is(err, ErrQuoteInputInvalid) {
		t.Errorf("空商机号应报 ErrQuoteInputInvalid，实际 %v", err)
	}
	if n := qsCountAll(t, db, "quotes"); n != 0 {
		t.Errorf("商机不存在时写了 %d 行", n)
	}

	created, err := svc.Generate(ctx, qsInput("opp_here"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if _, err := svc.Revise(ctx, created.QuoteID, QuoteGenerateInput{
		OpportunityID: "opp_other", TemplateCode: "std_annual",
	}); !errors.Is(err, ErrQuoteInputInvalid) {
		t.Errorf("还价时改商机归属应报 ErrQuoteInputInvalid，实际 %v", err)
	}
	// 空商机号在还价侧是「继承基准那份」，不是「挪到无主」。
	got, err := svc.Revise(ctx, created.QuoteID, QuoteGenerateInput{TemplateCode: "std_annual"})
	if err != nil {
		t.Fatalf("空商机号的还价（继承基准那份）失败：%v", err)
	}
	if got.OpportunityID != "opp_here" {
		t.Errorf("还价应继承基准的商机归属：opp=%q，期望 opp_here", got.OpportunityID)
	}
}

// TestQuoteService_ReviseAppendsAndKeepsBaseImmutable LTC-12 的落库判据。
//
// 关键不是「有了 v2」，而是「v1 一个字都没动」：读回基准行与它的每一行逐项比对。
// 另一臂是「还价不改行」—— Lines 为空时以基准版那些行为起点（仓储刻意不抄，
// 那是本层的判据），只改一个折扣后其余行该与基准逐字段相同。
func TestQuoteService_ReviseAppendsAndKeepsBaseImmutable(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_rev")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	v1, err := svc.Generate(ctx, qsInput("opp_rev"))
	if err != nil {
		t.Fatalf("生成第一版失败：%v", err)
	}
	baseRow := qsRow(t, db, v1.ID)
	baseLines := qsLines(t, db, v1.ID)

	v2, err := svc.Revise(ctx, v1.QuoteID, QuoteGenerateInput{
		OneID: "one_1", TemplateCode: "std_annual",
		Lines: []QuoteLineInput{{ProductID: "p_seat", DiscountPercent: qsF(25)}},
	})
	if err != nil {
		t.Fatalf("追加第二版失败：%v", err)
	}
	if v2.Version != 2 || v2.Status != model.QuoteStatusDraft {
		t.Errorf("第二版该是 version=2 draft，实际 %d %q", v2.Version, v2.Status)
	}
	if v2.QuoteID != v1.QuoteID {
		t.Errorf("还价另起了一条链：quote_id %q → %q", v1.QuoteID, v2.QuoteID)
	}
	if v2.ID == v1.ID {
		t.Error("新版本复用了对内行键（旧版就被覆盖了）")
	}
	newRow := qsRow(t, db, v2.ID)
	if newRow.SourceID != v1.ID || newRow.OpportunityID != "opp_rev" {
		t.Errorf("新版本行不对：source=%q opp=%q", newRow.SourceID, newRow.OpportunityID)
	}

	// 基准行逐项比回原值：任何一列漂了都说明「版本链」被写成了「就地改价」。
	// 用 DeepEqual 而不是 !=：Quote 有 ValidUntil 这个指针列，两次读回的地址必然不同，
	// == 会比成"永远不相等"，那条判据就成了恒红的摆设。
	if after := qsRow(t, db, v1.ID); !reflect.DeepEqual(*after, *baseRow) {
		t.Errorf("基准行被改写：\n  前 %+v\n  后 %+v", *baseRow, *after)
	}
	still := qsLines(t, db, v1.ID)
	if len(still) != len(baseLines) {
		t.Fatalf("基准行数从 %d 变成 %d", len(baseLines), len(still))
	}
	for i := range baseLines {
		if still[i] != baseLines[i] {
			t.Errorf("基准第 %d 行被改写：\n  前 %+v\n  后 %+v", i+1, baseLines[i], still[i])
		}
	}

	// 未提及的行从基准抄过来；提及的行只改折扣。199×10 = 1990，×0.75 = 1492.50。
	revLines := qsLines(t, db, v2.ID)
	if len(revLines) != len(baseLines) {
		t.Fatalf("第二版行数 %d，期望 %d（行项目该以基准行为起点）", len(revLines), len(baseLines))
	}
	if !qsSame(revLines[0].Amount, 1492.50) {
		t.Errorf("席位行 25%% 折后 %v，期望 1492.50", revLines[0].Amount)
	}
	if !qsSameLineContent(revLines[1], baseLines[1]) {
		t.Errorf("实施行该原样带到第二版：%+v vs %+v", revLines[1], baseLines[1])
	}

	latest, err := svc.LatestView(ctx, v1.QuoteID)
	if err != nil || latest.Version != 2 {
		t.Errorf("LatestView 没取到最新版：v=%+v err=%v", latest, err)
	}
	// 第三版接着追加（版本号继续单调，来路指向第二版）。
	v3, err := svc.Revise(ctx, v1.QuoteID, QuoteGenerateInput{OneID: "one_1", TemplateCode: "std_annual"})
	if err != nil {
		t.Fatalf("追加第三版失败：%v", err)
	}
	if v3.Version != 3 || qsRow(t, db, v3.ID).SourceID != v2.ID {
		t.Errorf("第三版链断了：version=%d source=%q（期望 3 / %q）", v3.Version, qsRow(t, db, v3.ID).SourceID, v2.ID)
	}
	// 「读某一版」读到的是当时那一版，不是最新版 —— 旧版不可变唯一能被证明的地方。
	back, err := svc.ViewAt(ctx, v1.QuoteID, 1)
	if err != nil || back.ID != v1.ID || !qsSame(back.Total, v1.Total) {
		t.Errorf("按 (quote_id, version) 读第一版没读回来：%+v err=%v", back, err)
	}
	if _, err := svc.ViewAt(ctx, v1.QuoteID, 9); !errors.Is(err, ErrQuoteVersionMissing) {
		t.Errorf("读一个不存在的版本应报 ErrQuoteVersionMissing，实际 %v", err)
	}
}

// TestQuoteService_ReviseConcurrentSecondLoser 两个人同时还价的结局。
//
// 与 T-P6-01 那条并发用例同一取向：**判不变量，不判赢输比例**。「基于哪一版」这次
// 由服务自己读（Latest），所以两个协程读到什么取决于调度 —— 把"恰好一个赢家"写进
// 期望值，就成了一条看机器脸色的用例（本仓拒绝这类假红）。
//
// 调度能造出**三种**正当结局，不止两种：版本行与它的行项目是两次写（Append 之后
// 才 AddLines），所以协程 B 完全可能读到 A 刚 Append、行项目还没落的那一版 —— 这一版
// 是空基准，本层当场拒掉（ErrQuoteVersionMissing）。把它算成"第三种失败"就是用例
// 在骗自己：真出现的第三种结局是**接受了空基准**并往下追加，那由末尾的落库断言兜。
// 与调度无关的只有三条：①结局只可能是这三类，第四类即失败；②成功几笔链上就多几版，
// 且版本号 1..n 连续不复；③链上每一版在终态都有行项目，没有哪一版停在"空基准"上。
// 裁决仍然只在库：本层不重读、不重试、不加进程内锁。
func TestQuoteService_ReviseConcurrentSecondLoser(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_race")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)
	first, err := svc.Generate(ctx, qsInput("opp_race"))
	if err != nil {
		t.Fatalf("生成第一版失败：%v", err)
	}

	const writers = 4
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, conflict, emptyBase, other := 0, 0, 0, 0
	outcomes := make([]string, 0, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			in := QuoteGenerateInput{OneID: fmt.Sprintf("one_%d", n), TemplateCode: "std_annual"}
			_, err := svc.Revise(ctx, first.QuoteID, in)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
				outcomes = append(outcomes, "成功")
			case errors.Is(err, repository.ErrQuoteVersionConflict):
				conflict++
				outcomes = append(outcomes, "版本冲突")
			case errors.Is(err, ErrQuoteVersionMissing):
				emptyBase++
				outcomes = append(outcomes, "空基准被拒")
			default:
				other++
				outcomes = append(outcomes, "第四类: "+err.Error())
				t.Log("未预期的失败：", err)
			}
		}(i)
	}
	wg.Wait()
	if other != 0 {
		t.Errorf("出现第四类结局 %d 次：%v", other, outcomes)
	}
	if won+conflict+emptyBase != writers {
		t.Errorf("成功 %d + 冲突 %d + 空基准 %d ≠ %d 次尝试（结局：%v）",
			won, conflict, emptyBase, writers, outcomes)
	}
	if won == 0 {
		t.Fatal("四次还价一笔都没落库")
	}
	chain, err := repository.NewQuoteRepositoryWithDB(db).ListVersions(ctx, first.QuoteID)
	if err != nil {
		t.Fatalf("读链失败：%v", err)
	}
	if len(chain) != won+1 {
		t.Errorf("链长 %d，期望 %d（成功几笔就该多几版；多出来的那一版是幽灵）", len(chain), won+1)
	}
	for i, row := range chain {
		if row.Version != int64(i+1) || row.QuoteID != first.QuoteID {
			t.Errorf("链上第 %d 行的版本号是 %d、链号 %q（期望 %d / %q）：不连续或重复 = 库里的裁决失守",
				i+1, row.Version, row.QuoteID, i+1, first.QuoteID)
		}
		// 终态判据：链上不许停在半写的那一版。落库顺序是"版本行 → 行项目"，
		// 中间那一瞬读到空基准是允许的，但**没有任何一版可以永远停在那里** ——
		// 否则"报价单上合计 0"这份数就会被下一次还价当成有依据的起点。
		if n := len(qsLines(t, db, row.ID)); n == 0 {
			t.Errorf("链上 v%d（%s）落库后一行项目都没有：空基准被接受了，本层要在追加前拒掉它",
				row.Version, row.ID)
		}
	}
}

// TestQuoteService_ReviseRejectsEmptyBaseVersion 空基准在**单跑**下也要被拒。
//
// 上面那条并发用例只在调度凑巧时才造得出空基准（读到别人刚 Append、行项目还没落的
// 那一版），所以那条判据不能作为它的唯一覆盖 —— 这里由测试直接种一个幽灵版本行：
// 走仓储的 Append（合法的写入路径），故意不写行项目。Revise 对着这种起点必须
// **不产出第三版**：继承一份空的行项目、报一个看起来合理的合计 0，比报错难查得多。
func TestQuoteService_ReviseRejectsEmptyBaseVersion(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_ghost")
	ctx := context.Background()
	repo := repository.NewQuoteRepositoryWithDB(db)
	svc, _, _ := qsSvc(t, db)
	first, err := svc.Generate(ctx, qsInput("opp_ghost"))
	if err != nil {
		t.Fatalf("生成第一版失败：%v", err)
	}
	ghost := &model.Quote{
		ID: "qs_ghost_second_version", QuoteID: first.QuoteID, OpportunityID: first.OpportunityID,
		Status: model.QuoteStatusDraft, Currency: first.Currency,
		CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
	}
	if err := repo.Append(ctx, first.ID, ghost); err != nil {
		t.Fatalf("种空基准版本行失败：%v", err)
	}

	_, err = svc.Revise(ctx, first.QuoteID, QuoteGenerateInput{OneID: "one_1"})
	if !errors.Is(err, ErrQuoteVersionMissing) {
		t.Errorf("对着空基准还价的结局是 %v，期望 ErrQuoteVersionMissing", err)
	}
	chain, err := repo.ListVersions(ctx, first.QuoteID)
	if err != nil {
		t.Fatalf("读链失败：%v", err)
	}
	if len(chain) != 2 {
		t.Errorf("链长 %d，期望仍是 2（被拒的那次一笔都不许写）：%v", len(chain), qsChainIDs(chain))
	}
}

func qsChainIDs(rows []*model.Quote) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Version)
	}
	return out
}

// qsConflictStore 只盯一件事：拿到版本冲突后本层有没有换一版再来一次。
//
// 计数的是**插入尝试次数**，不是"mock 被调了几次"这种自洽断言：
// Append 的契约是一次调用一次 INSERT，所以"重试过"的唯一可观测签名就是这里 >1。
type qsConflictStore struct {
	quoteStore
	calls int
}

func (c *qsConflictStore) Append(_ context.Context, _ string, _ *model.Quote) error {
	c.calls++
	return repository.ErrQuoteVersionConflict
}

// TestQuoteService_VersionConflictIsNotRetried 输家的结局必须原样上抛。
//
// "重读最新版再试一次"看起来是对用户友好，实际是把"客户还了三次价"里的第三版
// 变成一次网络抖动自动补出来的行 —— 链上多出一版没人见过的报价。
// 上面的并发用例对这一点不敏感（重试与不重试都满足它的不变量），所以这里单独钉。
func TestQuoteService_VersionConflictIsNotRetried(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_conflict")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)
	first, err := svc.Generate(ctx, qsInput("opp_conflict"))
	if err != nil {
		t.Fatalf("生成第一版失败：%v", err)
	}
	spy := &qsConflictStore{quoteStore: svc.store}
	svc.store = spy

	_, err = svc.Revise(ctx, first.QuoteID, QuoteGenerateInput{OneID: "one_1"})
	if !errors.Is(err, repository.ErrQuoteVersionConflict) {
		t.Fatalf("应原样收到版本冲突，实际 %v", err)
	}
	if spy.calls != 1 {
		t.Errorf("Append 被调用 %d 次（期望 1 次：冲突不许在本层重试）", spy.calls)
	}
	if n := qsCountAll(t, db, "quotes"); n != 1 {
		t.Errorf("冲突这一路链上现有 %d 行，期望仍是 1（没落库才是对的）", n)
	}
}

// TestQuoteService_UnassembledAndNilHandles 每个依赖缺件都单独可辨。
//
// 尤其不许「缺件 ⇒ 回空视图」：那会被 P6-03 的读侧当成「这单没报过价」。
func TestQuoteService_UnassembledAndNilHandles(t *testing.T) {
	ctx := context.Background()
	in := qsInput("opp_x")

	var nilSvc *QuoteService
	if _, err := nilSvc.Generate(ctx, in); err == nil {
		t.Error("nil 服务应报错")
	}
	if _, err := nilSvc.View(ctx, "q_1"); err == nil {
		t.Error("nil 服务读侧应报错")
	}
	if nilSvc.Available() {
		t.Error("nil 服务 Available 应为 false")
	}

	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_nil")
	for name, mut := range map[string]func(*QuoteService){
		"缺配置端口": func(s *QuoteService) { s.SetConfigStore(nil) },
		"缺话术端口": func(s *QuoteService) { s.SetScriptSource(nil) },
		"缺闸门":   func(s *QuoteService) { s.gate = nil },
	} {
		svc, _, _ := qsSvc(t, db)
		mut(svc)
		if svc.Available() {
			t.Errorf("%s：Available 仍报 true", name)
		}
		if _, err := svc.Generate(ctx, in); !errors.Is(err, ErrQuoteServiceUnavailable) {
			t.Errorf("%s：应报 ErrQuoteServiceUnavailable，实际 %v", name, err)
		}
		if _, err := svc.Revise(ctx, "QT-1", in); !errors.Is(err, ErrQuoteServiceUnavailable) {
			t.Errorf("%s：Revise 应报 ErrQuoteServiceUnavailable，实际 %v", name, err)
		}
	}
	// 仓储句柄没了：Available 要报得出来，读侧不许回空视图冒充「没有报价」。
	svc, _, _ := qsSvc(t, db)
	svc.store = repository.NewQuoteRepositoryWithDB(nil)
	if svc.Available() {
		t.Error("nil 句柄时 Available 应为 false")
	}
	if _, err := svc.Generate(ctx, in); err == nil {
		t.Error("nil 句柄时 Generate 应报错")
	}
	if _, err := svc.LatestView(ctx, "QT-none"); err == nil {
		t.Error("nil 句柄时读侧应报错（回空会被读成「这单没报过价」）")
	}
}

// TestQuoteKeysAreStableAndBounded 生成器是纯函数：同一时刻 + 同一 seq ⇒ 同一对键。
//
// 刻意不含日期串（带日期就要选一个时区来格式化，与本仓的日期边界裂脑同源）；
// 长度上限按最坏情况（int64 上限的 base36）核过列宽 64。
func TestQuoteKeysAreStableAndBounded(t *testing.T) {
	id, code := newQuoteKeys(qsClockBase, 1)
	if id2, code2 := newQuoteKeys(qsClockBase, 1); id2 != id || code2 != code {
		t.Error("纯函数版本不确定，用例无法复算")
	}
	if !strings.HasPrefix(id, "q_") || !strings.HasPrefix(code, "QT-") {
		t.Errorf("键形状不对：%q / %q", id, code)
	}
	for _, sep := range []string{"/", ":", " "} {
		if strings.Contains(id, sep) || strings.Contains(code, sep) {
			t.Errorf("键 %q / %q 含分隔符 %q（编号要在电话里念、键要被别的表抄走）", id, code, sep)
		}
	}
	if len(id) > 64 || len(code) > 64 {
		t.Errorf("键长超列宽：%d / %d", len(id), len(code))
	}
	_, worst := newQuoteKeys(qsClockBase, math.MaxInt64)
	if len(worst) > 64 {
		t.Errorf("最坏情况编号长 %d：%q", len(worst), worst)
	}
	if _, next := newQuoteKeys(qsClockBase, 2); next == code {
		t.Error("seq 没参与编号（同一秒内两条会撞）")
	}
}

// TestQuoteService_EveryVersionStaysDraft 本层产出的每一版都是 draft。
//
// 这条是 AC② 的行为面（形状面在 quoteStore 那条）：跑完生成 + 两次还价之后，
// 链上不许出现任何非 draft 状态 —— 出现即说明本层多了一条能改状态的路。
func TestQuoteService_EveryVersionStaysDraft(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_draft")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	first, err := svc.Generate(ctx, qsInput("opp_draft"))
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Revise(ctx, first.QuoteID, QuoteGenerateInput{
			OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{{ProductID: "p_seat", Quantity: qsF(float64(10 + i))}},
		}); err != nil {
			t.Fatalf("第 %d 次还价失败：%v", i+2, err)
		}
	}
	var statuses []string
	if err := db.WithContext(ctx).Model(&model.Quote{}).Pluck("status", &statuses).Error; err != nil {
		t.Fatalf("读状态集合失败：%v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("链上 %d 行，期望 3", len(statuses))
	}
	for _, s := range statuses {
		if s != model.QuoteStatusDraft {
			t.Errorf("本层写出了状态 %q 的版本（sent/accepted 只能由 P6-03 的闸门落）", s)
		}
	}
}

// —— 逐分支点名补出来的三格 ——————————————————————————————————————————
//
// 电池 23 刀全杀之后按分支点名（else / 半写 / 早退臂 / 有注释无语义的语义）余下这三格：
// 每格的牙由电池里对应的那一刀证明（M24 摘掉 AddLines 的错误上抛、M25–M30 把六条早退臂
// 各改成空视图、M31 去掉「命中第一条」的 break）。

// qsFailingLines 只坏 AddLines 这一格：版本行照常落库，明细写不进去。
//
// 整块 store 一起坏的话，"半写"这个状态根本造不出来（Create 就先炸），
// 而这一格要的正是「Create 成功、AddLines 失败」那个夹缝。
type qsFailingLines struct {
	quoteStore
	err error
}

func (f qsFailingLines) AddLines(context.Context, string, []*model.QuoteLineItem) error {
	return f.err
}

// TestQuoteService_LineWriteFailureIsReportedNotSwallowed 明细写失败必须报得难听，
// 且底层错因要能被调用方分诊。
//
// 这一格是生成路径上唯一的半写状态：那一版在库里读出来是「0 行、合计 0」——一个
// 完全合理而全是错的数字。所以除了"要报错"，还判两件事：报错文本点名人工处置
// （运维看得见该怎么收），以及 errors.Is 能拿回原始 DB 错因（调用方分得出网络抖动
// 还是列宽炸了）。同文件别处一律 %w，只有这一句 %v 是漏写：它让一次可重试的抖动
// 与一次数据事故在对外侧长得一模一样。
func TestQuoteService_LineWriteFailureIsReportedNotSwallowed(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_halfwrite")
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)
	boom := errors.New("connection reset by peer")
	svc.store = qsFailingLines{quoteStore: svc.store, err: boom}

	view, err := svc.Generate(ctx, qsInput("opp_halfwrite"))
	if err == nil {
		t.Fatalf("明细写不进去必须报错，实际回了视图 %+v（那会被 P6-03 读成一张 0 元的可发报价）", view)
	}
	if view != nil {
		t.Errorf("报错时不许带视图，实际 %+v", view)
	}
	if !errors.Is(err, boom) {
		t.Errorf("底层错因没包上：%v（调用方分不出抖动与数据事故，重试策略无从决定）", err)
	}
	for _, want := range []string{"行项目没跟上", "人工处置"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错文本缺 %q：%s", want, err)
		}
	}

	var rows []*model.Quote
	if err := db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读 quotes 失败：%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("链上 %d 行，期望 1（Create 已落的那一版）", len(rows))
	}
	if n := len(qsLines(t, db, rows[0].ID)); n != 0 {
		t.Errorf("半写那一版有 %d 行明细，期望 0", n)
	}
	// 闭环：这一版虽然是本层自己写坏的，还价路径也不许把它当成可继承的起点
	// （判据与 TestQuoteService_ReviseRejectsEmptyBaseVersion 同源，一头一尾各钉一次）。
	svc.store = repository.NewQuoteRepositoryWithDB(db)
	if _, err := svc.Revise(ctx, rows[0].QuoteID, QuoteGenerateInput{OneID: "one_1"}); !errors.Is(err,
		ErrQuoteVersionMissing) {
		t.Errorf("对 0 明细那一版还价应报 ErrQuoteVersionMissing，实际 %v", err)
	}
}

// TestQuoteService_ReadEntriesGuardBlankKeyAndMissingRow 三条读入口各自的早退臂。
//
// 三条路（按行键 / 按 (编号,版号) / 取最新）的空白判据与"不存在"判据各写一遍：
// 三处是同形不同格的代码，一处的绿不替另外两处说话。空白那侧刻意用带空白的串
// （"   " / "\t"），这样"先 TrimSpace 再判空"这件事本身也在判据里。
func TestQuoteService_ReadEntriesGuardBlankKeyAndMissingRow(t *testing.T) {
	db := qsSetupDB(t)
	ctx := context.Background()
	svc, _, _ := qsSvc(t, db)

	cases := []struct {
		name string
		call func() (*QuoteView, error)
		want error
	}{
		{"View 空行键", func() (*QuoteView, error) { return svc.View(ctx, "   ") }, ErrQuoteInputInvalid},
		{"ViewAt 空编号", func() (*QuoteView, error) { return svc.ViewAt(ctx, "", 1) }, ErrQuoteInputInvalid},
		{"LatestView 空编号", func() (*QuoteView, error) { return svc.LatestView(ctx, "\t") }, ErrQuoteInputInvalid},
		{"View 不存在的行键", func() (*QuoteView, error) { return svc.View(ctx, "q_not_a_row") }, ErrQuoteVersionMissing},
		{"ViewAt 不存在的版号", func() (*QuoteView, error) { return svc.ViewAt(ctx, "QT-none", 9) }, ErrQuoteVersionMissing},
		{"LatestView 不存在的链", func() (*QuoteView, error) { return svc.LatestView(ctx, "QT-none") }, ErrQuoteVersionMissing},
	}
	for _, c := range cases {
		view, err := c.call()
		if !errors.Is(err, c.want) {
			t.Errorf("%s：应报 %v，实际 %v", c.name, c.want, err)
		}
		if view != nil {
			t.Errorf("%s：报错却带了视图 %+v（空视图会被读成「这单没报过价」）", c.name, view)
		}
	}
}

// TestQuoteService_DuplicateSkuOverrideHitsOnlyFirstLine 把「只改第一条」那句注释变成判据。
//
// 同一 SKU 出现两次是真实价目表（validateTemplate 刻意不查重），此时"改 p_seat 的数量"
// 落在哪一行完全由 mergeQuoteLines 的命中规则决定。规则今天取第一条 —— 那必须钉住，
// 否则哪天有人去掉 break 就变成悄悄改最后一行，而两份价目表都能通过全部既有断言
// （各自只有一行同 SKU 的用例命中同一格）。
func TestQuoteService_DuplicateSkuOverrideHitsOnlyFirstLine(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_dupslice")
	ctx := context.Background()
	svc, cfg, _ := qsSvc(t, db)
	cfg[qsTemplateKey] = `{"currency":"CNY","lines":[` +
		`{"product_id":"p_seat","title":"席位·十人","quantity":10,"unit_price":100},` +
		`{"product_id":"p_seat","title":"席位·两人","quantity":2,"unit_price":100}]}`

	view, err := svc.Generate(ctx, QuoteGenerateInput{
		OpportunityID: "opp_dupslice", OneID: "one_1", TemplateCode: "std_annual",
		Lines: []QuoteLineInput{{ProductID: "p_seat", Quantity: qsF(3)}},
	})
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if len(view.Lines) != 2 {
		t.Fatalf("行数 %d，期望 2：%+v", len(view.Lines), view.Lines)
	}
	if got := view.Lines[0]; got.Quantity != 3 || got.Title != "席位·十人" || got.LineNo != 1 {
		t.Errorf("第一条行 = 数量 %v / 品名 %q / 行号 %d，期望 3 / 席位·十人 / 1（覆盖只落第一条）",
			got.Quantity, got.Title, got.LineNo)
	}
	if got := view.Lines[1]; got.Quantity != 2 || got.Title != "席位·两人" || got.LineNo != 2 {
		t.Errorf("第二条行 = 数量 %v / 品名 %q / 行号 %d，期望原样 2 / 席位·两人 / 2",
			got.Quantity, got.Title, got.LineNo)
	}
	if !qsSame(view.Total, 3*100+2*100) {
		t.Errorf("合计 %v，期望 500", view.Total)
	}
	if sum := qsSumAmount(t, db, view.ID); !qsSame(sum, view.Total) {
		t.Errorf("库侧 SUM(amount)=%v 与服务报的 %v 不一致", sum, view.Total)
	}
}

// TestQuoteService_LineValuesMustStayInsideColumnDomain 三列各有上界，越界在入参侧挡。
//
// 列宽写在 model 上：quantity numeric(12,2)（上限 9.99e9）、unit_price 与 amount
// numeric(14,2)（上限 9.99e12）。改之前 checkQuoteLineShape 只有下界与折扣区间：
// +Inf 走得过 `!(unit >= 0)`（+Inf ≥ 0 为真），天文数字走得过所有臂 —— 于是"越界"
// 由 PG 在写库时才报，红因是一句 numeric field overflow 而不是「哪一格超出哪一列」。
// 最要紧的是**乘积**那一格：数量与单价各自都合规（1e7 < 9.99e9、1e6 < 9.99e12），
// 行净额 1e13 却已经出列 —— 没有乘积判据时这一档完全看不见。
// NaN 与 ±Inf 一并由 `!(x < 上界)` 这个形状挡下（它们与任何数比较都是 false）。
func TestQuoteService_LineValuesMustStayInsideColumnDomain(t *testing.T) {
	db := qsSetupDB(t)
	qsSeedOpportunity(t, db, "opp_domain")
	ctx := context.Background()

	// contains 点的是**哪一臂报的**，不是哪一列：三臂里有两臂都提到 numeric(14,2)，
	// 只断言列名的话，摘掉单价上界会被乘积臂救回来（+Inf×10 折 10% 仍是 +Inf，
	// 落到「行净额超出 numeric(14,2)」），看着红不了 ⇒ 单价臂无牙。
	cases := []struct {
		name     string
		line     QuoteLineInput
		contains string
	}{
		{"单价 +Inf", QuoteLineInput{ProductID: "p_seat", UnitPrice: qsF(math.Inf(1))}, "不在 numeric(14,2)"},
		{"数量 +Inf", QuoteLineInput{ProductID: "p_seat", Quantity: qsF(math.Inf(1))}, "不在 numeric(12,2)"},
		{"数量出列", QuoteLineInput{ProductID: "p_seat", Quantity: qsF(1e11)}, "不在 numeric(12,2)"},
		{"单价出列", QuoteLineInput{ProductID: "p_seat", UnitPrice: qsF(1e13)}, "不在 numeric(14,2)"},
		{"行净额乘积出列", QuoteLineInput{ProductID: "p_seat", Quantity: qsF(1e7), UnitPrice: qsF(1e6)}, "行净额超出"},
	}
	for _, c := range cases {
		svc, _, _ := qsSvc(t, db)
		_, err := svc.Generate(ctx, QuoteGenerateInput{
			OpportunityID: "opp_domain", OneID: "one_1", TemplateCode: "std_annual",
			Lines: []QuoteLineInput{c.line},
		})
		if !errors.Is(err, ErrQuoteInputInvalid) {
			t.Errorf("%s：应报 ErrQuoteInputInvalid，实际 %v", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.contains) {
			t.Errorf("%s：报错没点名 %q（运维要能一眼看出是哪一列）：%s", c.name, c.contains, err)
		}
	}

	// 反向一票：贴着界内侧的大数必须过 —— 否则上界掐错了位，把能报的价也堵死了。
	// 1e6 × 1e5 折去模板那 10% = 9e10（在 numeric(14,2) 内），加第二行的 7600。
	svc, _, _ := qsSvc(t, db)
	view, err := svc.Generate(ctx, QuoteGenerateInput{
		OpportunityID: "opp_domain", OneID: "one_1", TemplateCode: "std_annual",
		Lines: []QuoteLineInput{{ProductID: "p_seat", Quantity: qsF(1e6), UnitPrice: qsF(1e5)}},
	})
	if err != nil {
		t.Fatalf("界内的大数应能生成，实际 %v", err)
	}
	const want = 90000007600 // 两个加数与和都在 2^53 内 ⇒ float64 与 numeric 两侧精确，可比 ==
	if view.Total != want {
		t.Errorf("合计 %v，期望 %v", view.Total, want)
	}
	if sum := qsSumAmount(t, db, view.ID); sum != want {
		t.Errorf("库侧 SUM(amount)=%v，期望 %v", sum, want)
	}
}

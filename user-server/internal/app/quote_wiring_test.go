// quote_wiring_test.go T-P6-03：报价两条腿的装配点，以及"接的是哪一把句柄"。
//
// 为什么单独立一个文件（service 侧 9+16 条判据已经全绿）：本仓反复出现的形状是
// "服务层单测全绿、生产装配点没人调用"（M33 那一课，商机竖的 T-P4-05 收的就是它）。
// 报价这条更具体：发送腿有九个依赖，缺任何一个的表现都是 Available() 为假 ⇒ 503，
// 而 503 在测试里长得和"没装配"一模一样，只有直接调 InitQuoteRuntime 才分得开。
//
// 本文件的前两条只断装配点独有的三件事（缺件的表现与"没装配"同为 503）：
// ① 闸门句柄是活的（关着时 Send 连版本行都不读）；
// ② store 句柄指向这把库（闸门打开后读到"行不存在"而不是"没接库"）；
// ③ open 句柄指向这把库（库里的 pending 读得出来）。
// 只断 Available() 的话，把任何一个句柄换成另一把库都照样绿。
//
// 第三条用例（不依赖全局句柄）跑一次真生成 —— 它是对上面那句"换成另一把库"的补盲：
// 前两条都在"全局已指向同一把库"的状态下跑，分不清参数句柄与全局句柄。
package app

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

func quoteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{},
		&model.ScriptLibrary{}, &model.ScriptVersion{})
}

// restoreQuoteGlobals 成对还原：本包的用例在同一个进程里跑，
// 只清不还原会把下一格的"未装配"判据变成"上一格留的那一份"。
func restoreQuoteGlobals(t *testing.T) {
	t.Helper()
	prevGen := service.GlobalQuoteService()
	prevSend := service.GlobalQuoteSendService()
	prevLTC := service.GlobalLTCConfig()
	t.Cleanup(func() {
		service.SetGlobalQuoteService(prevGen)
		service.SetGlobalQuoteSendService(prevSend)
		service.SetGlobalLTCConfig(prevLTC)
	})
}

func TestInitQuoteRuntimeAssemblesBothLegs(t *testing.T) {
	restoreQuoteGlobals(t)
	database := quoteTestDB(t)

	// 闸门：装配点取的是全局实例，所以这里把全局换成"读这把库"的那一份。
	// 不这么做的话测的就是缓存里的默认值，而默认值恰好也是"关"——关着的闸门
	// 证不了句柄是活的（见下面第二格把它打开）。
	ltc := service.NewLTCConfigServiceWithStore(repository.NewSystemConfigKVRepositoryWithDB(database))
	service.SetGlobalLTCConfig(ltc)

	if !InitQuoteRuntime(database) {
		t.Fatal("有 DB 句柄却报「装配不齐」：两条腿必有缺件")
	}
	gen := service.GlobalQuoteService()
	if gen == nil || !gen.Available() {
		t.Fatal("生成腿没进全局或 Available() 为假：/api/quote 会一直回 503")
	}
	send := service.GlobalQuoteSendService()
	if send == nil || !send.Available() {
		t.Fatal("发送腿没进全局或 Available() 为假：九个句柄少一个就在这里红")
	}

	ctx := context.Background()

	// ① 默认配置是"全关"：闸门关着时 Send 必须**先**撞上闸门，连版本行都不去读。
	// 这一条同时钉住"句柄活着"和"顺序在装配之后仍然成立"。
	if _, err := send.Send(ctx, service.QuoteSendInput{QuoteRowID: "q_absent", Operator: "op_1"}); !errors.Is(err, service.ErrQuoteGateClosed) {
		t.Errorf("闸门关着时 Send 回 %v，期望 ErrQuoteGateClosed —— 说明装配点给的闸门句柄没读 ltc.config", err)
	}

	if _, err := ltc.Save(ctx, &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
	}, 0); err != nil {
		t.Fatalf("打开报价阶段失败: %v", err)
	}
	ltc.InvalidateCache()

	// ② 闸门开了之后才轮到读那一版：报"行不存在"才是"读的是这把库"的证据。
	// 若 store 句柄落成了 nil/别的库，这里会是未装配或读失败。
	if _, err := send.Send(ctx, service.QuoteSendInput{QuoteRowID: "q_absent", Operator: "op_1"}); !errors.Is(err, service.ErrQuoteVersionMissing) {
		t.Errorf("Send 回 %v，期望 ErrQuoteVersionMissing（%v）", err, service.ErrQuoteVersionMissing)
	}

	// ③ open 句柄：往这把库里塞一条 pending，装配出来的实例必须读得到它。
	probeID := "q_wiring_probe"
	seed := &model.ApprovalRequest{
		ID: "apr_wiring_probe", SubjectType: service.QuoteApprovalSubjectType,
		SubjectID: probeID, PolicyKey: service.QuoteSendPolicyKey,
		Status: model.ApprovalStatusPending, ResumeToken: "rt_probe",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := database.Create(seed).Error; err != nil {
		t.Fatalf("写入探针审批行失败: %v", err)
	}
	got, err := send.OpenApproval(ctx, probeID)
	if err != nil {
		t.Fatalf("OpenApproval 失败: %v", err)
	}
	if got == nil || got.ID != seed.ID {
		t.Errorf("读到的开放审批是 %v，期望 %s：open 句柄接的不是这把库", got, seed.ID)
	}
	if got != nil && got.ResumeToken == "" {
		t.Error("探针行自带的凭证丢了：读侧拿到的是裁剪过的对象，OpenApproval 应当原样返回")
	}
}

// TestInitQuoteRuntimeDoesNotDependOnTheGlobalHandle 装配点读的是**传进来的那把**句柄。
//
// 为什么单独一格（上一条用例已经把三个句柄钉过了）：上一条跑在"全局句柄已指好"的状态下，
// 而那正是生产启动时的状态 —— 于是它分不清"用了参数 db"与"用了 db.GetDB()"。
// 两种写法在测试里同解，在生产里差一整个装配顺序问题：
// `NewSystemConfigKVRepository()` 捕获的是**构造那一刻**的全局句柄，本卡量过它的坏法 ——
// router.Setup 里那条 agent 工具链就因为它拿到 nil 而**panic**（nil *gorm.DB 在 Get 里
// 不是 error 是崩，见 repository/system_config_kv.go 的注释）。报价这一族若走同一条路，
// 坏法不是报错而是"永远读不到模板"：模板与生效话术指针都在 system_config_kv 里，
// 全局句柄为 nil 时生成腿当场 panic，全局句柄指向别的库时**照出价，只是取的是别处的价目表**。
//
// 所以这里刻意把全局句柄置空再装配：走对了（用参数）⇒ 一次完整的生成落在那把库上；
// 走错了 ⇒ 读配置时撞 nil。nil 的来路是 panic 而不是 error，而一条 panic 会带走整个测试
// 二进制（剩下的用例一条不报），所以这里自己接住并判成本格红。
func TestInitQuoteRuntimeDoesNotDependOnTheGlobalHandle(t *testing.T) {
	restoreQuoteGlobals(t)
	database := quoteTestDB(t)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("装配点把某个句柄挂到了全局 DB 上（全局为空时它不是报错而是 panic）：%v", r)
		}
	}()

	prevDB := dbutil.GetDB()
	t.Cleanup(func() { dbutil.SetTestDB(prevDB) })
	dbutil.SetTestDB(nil)

	// 闸门服务必须显式建在这把库上：GlobalLTCConfig() 在为空时会就地用全局句柄造一份，
	// 而全局此刻是 nil —— 不指这一把的话，红因会落在闸门上而不是配置读口上。
	ltc := service.NewLTCConfigServiceWithStore(repository.NewSystemConfigKVRepositoryWithDB(database))
	service.SetGlobalLTCConfig(ltc)
	if _, err := ltc.Save(context.Background(), &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
	}, 0); err != nil {
		t.Fatalf("打开报价阶段失败: %v", err)
	}
	ltc.InvalidateCache()

	if !InitQuoteRuntime(database) {
		t.Fatal("全局句柄为空时装配失败：装配点显然还在从别处取库")
	}
	quoteSeedForWiring(t, database)

	view, err := service.GlobalQuoteService().Generate(context.Background(), service.QuoteGenerateInput{
		OpportunityID: quoteWiringOppID, TemplateCode: "cfg",
	})
	if err != nil {
		t.Fatalf("在全局句柄为空的进程状态下生成失败: %v", err)
	}
	// 读回必须打这把库：生成的那一版若写进了别的库，这一句会是 record not found。
	var stored model.Quote
	if err := database.Where("id = ?", view.ID).First(&stored).Error; err != nil {
		t.Fatalf("生成的那一版不在装配时给的那把库里: %v", err)
	}
	if stored.Status != model.QuoteStatusDraft {
		t.Errorf("库里那一版是 %s，期望 %s", stored.Status, model.QuoteStatusDraft)
	}
}

const quoteWiringOppID = "opp_wiring_handle"

// quoteSeedForWiring 一套最小可生成的夹具：商机 + 生效话术快照 + 模板。
// 全走运营写得到的入口（kv 与内容行），不改任何全局变量。
func quoteSeedForWiring(t *testing.T, database *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(ctx, &model.Opportunity{
		ID: quoteWiringOppID, Code: "OPP-WIRING", CustomerID: "cus_wiring", OneID: "one_wiring",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 500, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_wiring",
	}); err != nil {
		t.Fatalf("造商机行失败: %v", err)
	}
	sc := &model.ScriptLibrary{
		Category: "quote", Subcategory: "quote_cover", Title: "报价封面",
		Content: "编辑区草稿", Version: 1, Status: "active",
	}
	if err := database.Create(sc).Error; err != nil {
		t.Fatalf("写话术分片失败: %v", err)
	}
	if err := database.Create(&model.ScriptVersion{
		ScriptID: sc.ID, Version: 1, Title: "报价封面",
		Content: "您好，以下是本次报价。", Status: "published",
	}).Error; err != nil {
		t.Fatalf("写话术快照失败: %v", err)
	}
	kv := repository.NewSystemConfigKVRepositoryWithDB(database)
	if _, err := kv.Upsert(ctx, service.QuoteScriptIDKVKey, strconv.FormatUint(uint64(sc.ID), 10)); err != nil {
		t.Fatalf("写话术指针失败: %v", err)
	}
	if _, err := kv.Upsert(ctx, service.QuoteTemplateKVPrefix+"cfg",
		`{"currency":"CNY","valid_days":7,"lines":[`+
			`{"product_id":"p_seat","title":"席位","quantity":2,"unit_price":100.00,"discount_percent":0}]}`); err != nil {
		t.Fatalf("写模板失败: %v", err)
	}
}

func TestInitQuoteRuntimeClearsBothLegsWithoutDB(t *testing.T) {
	restoreQuoteGlobals(t)
	database := quoteTestDB(t)
	if !InitQuoteRuntime(database) {
		t.Fatal("前置条件不成立：带库装配没成功")
	}
	if service.GlobalQuoteService() == nil || service.GlobalQuoteSendService() == nil {
		t.Fatal("前置条件不成立：带库装配之后全局是空的")
	}

	if InitQuoteRuntime(nil) {
		t.Error("无 DB 句柄却回了 true")
	}
	// 两条腿都要清：只清一半的话，路由里那一半继续对着上一份实例回 200，
	// 而日志同时写着"未装配"——那是本函数唯一会骗人的方式。
	if svc := service.GlobalQuoteService(); svc != nil {
		t.Error("无库装配没清生成腿全局实例")
	}
	if svc := service.GlobalQuoteSendService(); svc != nil {
		t.Error("无库装配没清发送腿全局实例")
	}
}

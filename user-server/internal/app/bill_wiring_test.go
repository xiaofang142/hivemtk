// bill_wiring_test.go T-P7-01：账单派生腿的装配点，以及"接的是哪一把句柄"。
//
// 为什么单独立一个文件（服务层那 18 条已经全绿）：本仓反复出现的形状是
// "服务层单测全绿、生产装配点没人调用"（M33 那一课）。账单这一族尤其致命：
// accepted 的**唯一**生产写入口就是这条腿，装配点缺席时库里永远不会有账单，
// 而表现是 /api/bill 恒 503 —— 那在测试里与"没装配"长得一模一样。
//
// 三条用例各钉一件别人钉不住的：
// ① 装配齐全时 Available() 为真，且真能在**这把**库上派生出一行；
// ② 用的是参数句柄而不是全局句柄（全局为空时仍要能派生 —— 走错的那一路在生产里
//
//	是"装配顺序决定一切"，nil 全局则当场 panic）；
//
// ③ 无库时必须把全局**清空**：Init 可重复调用，只登记不清会把上一份实例继续对着流量回 200。
package app

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func billTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 四张表一起建：派生这条腿的因果链横跨"报价行 → 行项目 → 账单行"，
	// 少任何一张都会让用例红在"表不存在"上而不是红在装配上。
	return testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{}, &model.Bill{})
}

// restoreBillGlobal 成对还原：本包的用例在同一进程里跑，只清不还原会把下一格的
// "未装配"判据变成"上一格留的那一份"。
func restoreBillGlobal(t *testing.T) {
	t.Helper()
	prev := service.GlobalBillService()
	t.Cleanup(func() { service.SetGlobalBillService(prev) })
	service.SetGlobalBillService(nil)
}

// billSeedDerivableQuote 落一版"已发出、有行项目"的报价，全走真仓储的写路径。
func billSeedDerivableQuote(t *testing.T, database *gorm.DB, rowID string) {
	t.Helper()
	ctx := context.Background()
	if err := repository.NewOpportunityRepositoryWithDB(database).Insert(ctx, &model.Opportunity{
		ID: "opp_" + rowID, Code: "OPP-" + rowID, CustomerID: "cus_b", OneID: "one_b",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_b",
	}); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
	repo := repository.NewQuoteRepositoryWithDB(database)
	row := &model.Quote{
		ID: rowID, QuoteID: "QT-" + rowID, OpportunityID: "opp_" + rowID,
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault,
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("落报价第一版失败：%v", err)
	}
	lines := []*model.QuoteLineItem{
		{LineNo: 1, ProductID: "p-a", Title: "坐席", Quantity: 2, UnitPrice: 50, DiscountPercent: 0, Amount: 100},
	}
	if err := repo.AddLines(ctx, rowID, lines); err != nil {
		t.Fatalf("落行项目失败：%v", err)
	}
	if err := repo.UpdateStatus(ctx, rowID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("把报价推到 sent 失败：%v", err)
	}
}

func TestInitBillRuntimeAssemblesTheDeriveLeg(t *testing.T) {
	restoreBillGlobal(t)
	database := billTestDB(t)

	if !InitBillRuntime(database) {
		t.Fatal("有 DB 句柄却报「装配不齐」：两把句柄必有缺件")
	}
	svc := service.GlobalBillService()
	if svc == nil {
		t.Fatal("派生腿没进全局登记处：/api/bill 会一直回 503")
	}
	if !svc.Available() {
		t.Fatal("派生腿 Available() 为假：账单存储与报价存储缺一即在这里红")
	}

	billSeedDerivableQuote(t, database, "q_wiring_bill")
	view, err := svc.DeriveFromQuote(context.Background(), service.BillDeriveInput{QuoteRowID: "q_wiring_bill"})
	if err != nil {
		t.Fatalf("装配出来的实例派生失败：%v", err)
	}
	// 断言打在**落库那一行**：只信返回值的话，"写进了别的库"这一种坏法读不出来。
	var stored model.Bill
	if err := database.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("派生出来的账单不在装配时给的那把库里：%v", err)
	}
	if stored.Amount != 100 || stored.Status != model.BillStatusOpen {
		t.Errorf("库里那一行是 %v / %s，期望 100 与 %s", stored.Amount, stored.Status, model.BillStatusOpen)
	}
}

// TestInitBillRuntimeDoesNotDependOnTheGlobalHandle 装配点读的是**传进来的那把**句柄。
//
// 上一格跑在"全局句柄已指好"的状态下，而那正是生产启动时的状态 ——
// 于是它分不清"用了参数 db"与"用了 db.GetDB()"。两种写法在上一格里同解，
// 在生产里差一整个装配顺序问题：NewXxxRepository()（无 WithDB 的那一版）捕获的是
// **构造那一刻**的全局句柄，为 nil 时不是报错而是 panic，一条 panic 会带走整个测试二进制
// （剩下的用例一条不报），所以这里自己接住并判成本格红。
func TestInitBillRuntimeDoesNotDependOnTheGlobalHandle(t *testing.T) {
	restoreBillGlobal(t)
	database := billTestDB(t)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("装配点把某把句柄挂到了全局 DB 上（全局为空时它不是报错而是 panic）：%v", r)
		}
	}()

	prevDB := dbutil.GetDB()
	t.Cleanup(func() { dbutil.SetTestDB(prevDB) })
	dbutil.SetTestDB(nil)

	if !InitBillRuntime(database) {
		t.Fatal("全局句柄为空时装配失败：装配点显然还在从别处取库")
	}
	billSeedDerivableQuote(t, database, "q_wiring_bill_handle")
	if _, err := service.GlobalBillService().DeriveFromQuote(context.Background(),
		service.BillDeriveInput{QuoteRowID: "q_wiring_bill_handle"}); err != nil {
		t.Fatalf("全局句柄为空的进程状态下派生失败：%v", err)
	}
	var n int64
	if err := database.Model(&model.Bill{}).Count(&n).Error; err != nil {
		t.Fatalf("数账单失败：%v", err)
	}
	if n != 1 {
		t.Errorf("装配时给的那把库里有 %d 行账单，期望 1（写成 0 就是派生落到了别的句柄上）", n)
	}
}

func TestInitBillRuntimeClearsGlobalWithoutDB(t *testing.T) {
	restoreBillGlobal(t)
	database := billTestDB(t)
	if !InitBillRuntime(database) {
		t.Fatal("前置条件不成立：带库装配没成功")
	}
	if service.GlobalBillService() == nil {
		t.Fatal("前置条件不成立：带库装配之后全局是空的")
	}

	if InitBillRuntime(nil) {
		t.Error("无 DB 句柄却回了 true")
	}
	// 不清就等于"路由挂了、全局里还是上一份实例"，而它对着的可能是已经关掉的库连接；
	// 日志同时写着"未装配"——那是本函数唯一会骗人的方式。
	if svc := service.GlobalBillService(); svc != nil {
		t.Error("无库装配没清全局派生腿实例")
	}
}

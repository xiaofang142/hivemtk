// payment_wiring_test.go T-P7-02：回款腿的装配点，以及"两条腿接的是不是同一把库"。
//
// 为什么单独立一个文件（service/payment.go 那十几条已经全绿）：本仓反复出现的形状是
// "服务层单测全绿、生产装配点没人调用"（M33 那一课，账单侧的 bill_wiring_test.go 为同一条而立）。
// 回款这一族更致命：入账的**唯一**生产通路是订单 webhook，读侧的唯一通路是 /api/bill 那两条 GET，
// 两条都从这一个全局实例出发。装配点缺席时的症状是"webhook 收了钱但说回款腿没装"
// 与"账单读口恒 503"——而在测试里这两种与"故意没装配"长得一模一样。
//
// 三条用例各钉一件别人钉不住的：
// ① 装配齐全时 Available() 为真，且真能在**这把**库上记一行回款、读回那个和；
// ② 用的是参数句柄而不是全局句柄（全局为空时仍要能入账）；
// ③ 无库时必须把全局**清空**：Init 可重复调用，只登记不清会让 webhook 继续对着
//
//	上一份实例（可能已关连接）回 200，而账单状态那一格再没人折算。
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

func paymentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 两张表一起建：入账这条腿的因果链横跨"回款行 → 账单状态"，
	// 少任何一张都会让用例红在"表不存在"上而不是红在装配上。
	return testutil.NewTestDB(t, &model.Bill{}, &model.Payment{})
}

// restorePaymentGlobal 成对还原：本包的用例在同一进程里跑，只清不还原会把下一格的
// "未装配"判据变成"上一格留的那一份"。
func restorePaymentGlobal(t *testing.T) {
	t.Helper()
	prev := service.GlobalPaymentService()
	t.Cleanup(func() { service.SetGlobalPaymentService(prev) })
	service.SetGlobalPaymentService(nil)
}

// paymentSeedBill 落一张"已开账、还没收钱"的应收，走真仓储的写路径（不直插）。
func paymentSeedBill(t *testing.T, database *gorm.DB, billID string, amount float64) {
	t.Helper()
	err := repository.NewBillRepositoryWithDB(database).Create(context.Background(), &model.Bill{
		ID: billID, QuoteID: "QT-" + billID, QuoteRowID: billID + "_1", OpportunityID: "opp_" + billID,
		Status: model.BillStatusOpen, Amount: amount, Currency: model.BillCurrencyDefault,
	})
	if err != nil {
		t.Fatalf("造账单行失败：%v", err)
	}
}

func TestInitPaymentRuntimeAssemblesBothLegs(t *testing.T) {
	restorePaymentGlobal(t)
	database := paymentTestDB(t)

	if !InitPaymentRuntime(database) {
		t.Fatal("有 DB 句柄却报「装配不齐」：两把句柄必有缺件")
	}
	svc := service.GlobalPaymentService()
	if svc == nil {
		t.Fatal("回款腿没进全局登记处：webhook 入账与 /api/bill 读侧会一直回 503")
	}
	if !svc.Available() {
		t.Fatal("回款腿 Available() 为假：回款存储与账单存储缺一即在这里红")
	}

	paymentSeedBill(t, database, "b_wiring_pay", 300)
	if _, err := svc.RecordPayment(context.Background(), service.RecordPaymentInput{
		BillID: "b_wiring_pay", ChannelRef: "ref_wiring_1", Amount: 120,
		Platform: "taobao", OrderID: "ord_wiring_1",
	}); err != nil {
		t.Fatalf("装配出来的实例入账失败：%v", err)
	}
	// 断言打在**落库那一行**：只信返回值的话，"写进了别的库"这一种坏法读不出来。
	var n int64
	if err := database.Model(&model.Payment{}).Where("bill_id = ?", "b_wiring_pay").Count(&n).Error; err != nil {
		t.Fatalf("数回款行失败：%v", err)
	}
	if n != 1 {
		t.Fatalf("装配时给的那把库里有 %d 行回款，期望 1（写成 0 就是钱落到了别的句柄上）", n)
	}
	var stored model.Bill
	if err := database.First(&stored, "id = ?", "b_wiring_pay").Error; err != nil {
		t.Fatalf("读回账单失败：%v", err)
	}
	if stored.Status != model.BillStatusPartial {
		t.Errorf("库里账单状态=%s，期望 %s —— 两把句柄接的不是同一把库时才会这样", stored.Status, model.BillStatusPartial)
	}

	st, err := svc.Statement(context.Background(), "b_wiring_pay")
	if err != nil {
		t.Fatalf("读对账视图失败：%v", err)
	}
	if st.Settled != 120 || st.Outstanding != 180 {
		t.Errorf("settled/outstanding = %v/%v，期望 120/180", st.Settled, st.Outstanding)
	}
}

// TestInitPaymentRuntimeDoesNotDependOnTheGlobalHandle 装配点读的是**传进来的那把**句柄。
//
// 判据与账单侧同一条：上一格跑在"全局句柄已指好"的状态下，而那正是生产启动时的状态，
// 于是它分不清"用了参数 db"与"用了 db.GetDB()"。写成后者时全局为空不是报错而是 panic，
// 一条 panic 会带走整个测试二进制（剩下的用例一条不报），所以这里自己接住并判成本格红。
func TestInitPaymentRuntimeDoesNotDependOnTheGlobalHandle(t *testing.T) {
	restorePaymentGlobal(t)
	database := paymentTestDB(t)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("装配点把某把句柄挂到了全局 DB 上（全局为空时它不是报错而是 panic）：%v", r)
		}
	}()

	prevDB := dbutil.GetDB()
	t.Cleanup(func() { dbutil.SetTestDB(prevDB) })
	dbutil.SetTestDB(nil)

	if !InitPaymentRuntime(database) {
		t.Fatal("全局句柄为空时装配失败：装配点显然还在从别处取库")
	}
	paymentSeedBill(t, database, "b_wiring_pay_handle", 100)
	if _, err := service.GlobalPaymentService().RecordPayment(context.Background(), service.RecordPaymentInput{
		BillID: "b_wiring_pay_handle", ChannelRef: "ref_wiring_2", Amount: 100,
		Platform: "jd", OrderID: "ord_wiring_2",
	}); err != nil {
		t.Fatalf("全局句柄为空的进程状态下入账失败：%v", err)
	}
	var m int64
	if err := database.Model(&model.Payment{}).Count(&m).Error; err != nil {
		t.Fatalf("数回款失败：%v", err)
	}
	if m != 1 {
		t.Errorf("装配时给的那把库里有 %d 行回款，期望 1", m)
	}
}

func TestInitPaymentRuntimeClearsGlobalWithoutDB(t *testing.T) {
	restorePaymentGlobal(t)
	database := paymentTestDB(t)
	if !InitPaymentRuntime(database) {
		t.Fatal("前置条件不成立：带库装配没成功")
	}
	if service.GlobalPaymentService() == nil {
		t.Fatal("前置条件不成立：带库装配之后全局是空的")
	}

	if InitPaymentRuntime(nil) {
		t.Error("无 DB 句柄却回了 true")
	}
	// 不清就等于"webhook 照收、读口照答，而它对着的是上一份实例（可能是已关的连接）"，
	// 日志同时写着"未装配"——那是本函数唯一会骗人的方式。
	if svc := service.GlobalPaymentService(); svc != nil {
		t.Error("无库装配没清全局回款腿实例")
	}
}

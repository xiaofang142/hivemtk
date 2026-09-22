// bill_test.go T-P7-01：账单派生层（报价接受 → 一张应收）。
//
// 本层的四件事，逐件独立成例（摘掉任何一件不会让别件变红）：
//
//	AC① 「账单由已成交报价自动派生」= 从**库里那一版 sent 的报价**出发，一次调用产出一行账单，
//	     且同一版被确认两次仍然只有一行（幂等，判据打在 Count 上而不是返回值上）。
//	AC② 「金额与报价合计对账一致」= 期望值是**手算写死的 369.99**，不是 quoteSumAmount 的返回值。
//	     两边同源于一个函数时，那条"对账一致"就退化成"这个函数等于它自己"（假绿的一种标准形态）。
//	边界 「什么算成交」= 只有 sent 那一版能被确认，且必须是链上最新的一版；
//	     一条链上已经有被接受过的版本时，第二版不接受（否则同一商机凭空多出一张应收）。
//	顺序 「只读判据全在第一次写之前」= 行项目缺失这种"读出来就知道会失败"的形状，
//	     必须留下"报价仍是 sent"的现场；反过来 CAS 成功而账单写入失败时，
//	     留下的是"已接受但没账单"，那条靠幂等重入修得回来（见 TestBillDeriveRacesIntoExisting）。
//
// 替身只在真库造不出那一格时才加（沿用 quote_send_test.go 的口径）：
// 存储层是真仓储 + 真库，只有"跃迁写不进去"、"Create 报已派生"、"读回坏掉"三格需要开关。
package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 夹具 ——————————————————————————————————————————————————————————————

func billSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 四张表一起建：派生腿的因果链横跨"报价行 → 行项目 → 账单行"，
	// 分库建会让"从行项目算出来的合计落进账单"这一段断成两次互不相干的测试。
	return testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{}, &model.Bill{},
	)
}

// billSeedQuote 经真仓储落一版报价（v1），并把它推进到 wantStatus。
//
// 走仓储而不是 db.Create 直插：报价行的 status 值是仓储跃迁口写进去的，
// 直插会替仓储兜住"这一格根本写不进去"那类错误。
// 链上第二版由用例自己 Append（只有两例需要，见 TestBillDeriveRejectsSupersededVersion），
// 夹具里先做一条通用分支出来，等于写一段没人走的准备路。
func billSeedQuote(t *testing.T, db *gorm.DB, rowID, quoteID, oppID, wantStatus string) string {
	t.Helper()
	ctx := context.Background()
	repo := repository.NewQuoteRepositoryWithDB(db)
	row := &model.Quote{
		ID: rowID, QuoteID: quoteID, OpportunityID: oppID,
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault,
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("落报价第一版失败: %v", err)
	}
	if wantStatus != model.QuoteStatusDraft {
		if err := repo.UpdateStatus(ctx, row.ID, model.QuoteStatusDraft, wantStatus); err != nil {
			t.Fatalf("把报价 %s 推到 %s 失败: %v", row.ID, wantStatus, err)
		}
	}
	return row.ID
}

// billSeedLines 落两份行项目，净额**手算**：3×100.00×(1−10%) = 270.00，1×99.99×(1−0%) = 99.99。
// 合计 369.99 —— AC② 的期望值就是这个字面量，它不来自被测代码。
func billSeedLines(t *testing.T, db *gorm.DB, rowID string) {
	t.Helper()
	repo := repository.NewQuoteRepositoryWithDB(db)
	rows := []*model.QuoteLineItem{
		{LineNo: 1, ProductID: "p-a", Title: "标准版坐席 ×3", Quantity: 3, UnitPrice: 100, DiscountPercent: 10, Amount: 270},
		{LineNo: 2, ProductID: "p-b", Title: "实施包", Quantity: 1, UnitPrice: 99.99, DiscountPercent: 0, Amount: 99.99},
	}
	if err := repo.AddLines(context.Background(), rowID, rows); err != nil {
		t.Fatalf("落行项目失败: %v", err)
	}
}

// billSeedOpp 派生腿不读商机表（opportunity_id 从报价行原样抄），但报价的生成前提是那条商机存在；
// 建出来只为了让夹具与真路一致，不为了让服务去查它。
func billSeedOpp(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	repo := repository.NewOpportunityRepositoryWithDB(db)
	row := &model.Opportunity{
		ID: id, Code: "OPP-BILL-" + id, CustomerID: "cus_bill", OneID: "one_bill",
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_a",
		CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
	}
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("造商机行失败: %v", err)
	}
}

// billSvc 装一台真库上的派生服务。返回的 parts 里带真仓储句柄，便于用例直接读库核对。
func billSvc(t *testing.T, db *gorm.DB) *BillService {
	t.Helper()
	svc := NewBillService(
		repository.NewBillRepositoryWithDB(db),
		repository.NewQuoteRepositoryWithDB(db),
	)
	svc.SetClock(func() time.Time { return qsClockBase })
	return svc
}

// billRow 从库里读回那张账单（断言打在落库那一行，不是返回值）。
func billRow(t *testing.T, db *gorm.DB, id string) *model.Bill {
	t.Helper()
	var row model.Bill
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("读回账单 %s 失败: %v", id, err)
	}
	return &row
}

func billCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.Bill{}).Count(&n).Error; err != nil {
		t.Fatalf("清点账单失败: %v", err)
	}
	return n
}

// —— 替身（只为真库造不出的那一格）————————————————————————————————————————

// errCASBoom / errReadBackBoom 是替身吐出的原始错误。
//
// 用例断它们能被 errors.Is 分到，是在断服务侧没把底层错因丢掉：
// "状态写不进去"与"账单写进去了但没读回来"的处置动作完全不同（前者重试即可，
// 后者要去库里确认那一张到底有没有），只留一句哨兵文本就等于让人去猜。
var (
	errCASBoom      = errors.New("boom: 状态写不进去")
	errReadBackBoom = errors.New("boom: 账单读不回来")
)

// billQuoteSpy 包住真报价仓储，只加两个开关：
// failCAS = 跃迁写不进去（并发对手先动了那一版 / 库抖动）；
// casCalls 数跃迁尝试次数，用来证"行项目缺失时一次都没动过状态列"。
type billQuoteSpy struct {
	repository.QuoteRepository
	failCAS  bool
	casCalls int
}

func (s *billQuoteSpy) UpdateStatus(ctx context.Context, id, from, to string) error {
	s.casCalls++
	if s.failCAS {
		return errCASBoom
	}
	return s.QuoteRepository.UpdateStatus(ctx, id, from, to)
}

// billStoreSpy 包住真账单仓储：
// failDerivedAlways = Create 永远报"已派生过"（并发里另一路刚插进去的那个窗口）；
// failReadBack = 写得进去但读不回来（存储抖动的另一格）。
type billStoreSpy struct {
	repository.BillRepository
	failDerivedAlways bool
	failReadBack      bool
}

func (s *billStoreSpy) Create(ctx context.Context, b *model.Bill) error {
	if s.failDerivedAlways {
		return repository.ErrBillAlreadyDerived
	}
	return s.BillRepository.Create(ctx, b)
}

func (s *billStoreSpy) GetByID(ctx context.Context, id string) (*model.Bill, error) {
	if s.failReadBack {
		return nil, errReadBackBoom
	}
	return s.BillRepository.GetByID(ctx, id)
}

// —— AC① ————————————————————————————————————————————————————————————————

// TestBillDeriveFromSentQuoteWritesOneRow 成交确认落到库里那一行账单。
//
// 断言全部打在**读回来的那一行**上：返回值是服务自己拼的，它说金额多少不算数。
func TestBillDeriveFromSentQuoteWritesOneRow(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-b1")
	rowID := billSeedQuote(t, db, "q_b1", "QT-B1", "opp-b1", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)

	view, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}
	if view == nil || view.ID == "" {
		t.Fatalf("返回了空视图: %+v", view)
	}
	stored := billRow(t, db, view.ID)
	if stored.QuoteRowID != rowID {
		t.Errorf("账单钉在报价行 %q，期望 %q", stored.QuoteRowID, rowID)
	}
	if stored.QuoteID != "QT-B1" || stored.OpportunityID != "opp-b1" {
		t.Errorf("来路两把键没从报价行抄下来: quote_id=%q opportunity_id=%q", stored.QuoteID, stored.OpportunityID)
	}
	if stored.Status != model.BillStatusOpen {
		t.Errorf("新派生的账单状态 %q，期望 open", stored.Status)
	}
	if stored.Currency != model.QuoteCurrencyDefault {
		t.Errorf("币种 %q 没跟随报价", stored.Currency)
	}
	// 报价那一版本地也要真的翻成 accepted：账单派生了而报价还是 sent，
	// 下一次确认会再走一遍全路（虽然幂等挡住，但"成交"这件事在两处各说一半）。
	var quote model.Quote
	if err := db.First(&quote, "id = ?", rowID).Error; err != nil {
		t.Fatalf("读回报价失败: %v", err)
	}
	if quote.Status != model.QuoteStatusAccepted {
		t.Errorf("报价状态还是 %q：账单说成交了而报价域不认", quote.Status)
	}
}

// TestBillAmountEqualsHandComputedTotal AC②：账单金额 = 那一版行项目的手算合计。
//
// 期望值是夹具上面注释里那两个式子的结果，**不调用** quoteSumAmount / quoteRound2。
// 两边同源于一个函数时，这条用例在"函数整体写错"下照样绿。
func TestBillAmountEqualsHandComputedTotal(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-b2")
	rowID := billSeedQuote(t, db, "q_b2", "QT-B2", "opp-b2", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)

	view, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}
	const want = 369.99
	if stored := billRow(t, db, view.ID); stored.Amount != want {
		t.Errorf("账单金额 %v，手算合计 %v：AC② 的对账就是这两个数必须逐分相等", stored.Amount, want)
	}
}

// TestBillDeriveIsIdempotentAcrossCalls 同一版确认两次只有一张账单。
//
// 判据是 Count 而不是返回值：返回值漂亮而库里两行，才是财务上最贵的那种形状。
// 第二次的返回必须标明"这是复用"（Reused），否则调用方会把"我刚开了一张应收"
// 这句话在第二次也说不出口 / 或者说错。
func TestBillDeriveIsIdempotentAcrossCalls(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-b3")
	rowID := billSeedQuote(t, db, "q_b3", "QT-B3", "opp-b3", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	svc := billSvc(t, db)
	ctx := context.Background()

	first, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("第一次派生失败: %v", err)
	}
	if first.Reused {
		t.Error("首次派生就报告 Reused：调用方会以为这张应收早就存在")
	}
	second, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("第二次派生失败（幂等重入没走通）: %v", err)
	}
	if !second.Reused {
		t.Error("第二次没报 Reused")
	}
	if second.ID != first.ID {
		t.Errorf("两次返回不同账单号 %q / %q", first.ID, second.ID)
	}
	if n := billCount(t, db); n != 1 {
		t.Errorf("同一版确认两次之后库里有 %d 张账单，期望 1", n)
	}
}

// TestBillDeriveCurrencyFollowsQuote 币种跟报价走，报价那格空着才落到默认值。
//
// 抄而不是各写各的：账单币种与报价币种不同的那一刻起，AC② 的"金额相等"
// 就变成两个不同量纲的数字相等。
func TestBillDeriveCurrencyFollowsQuote(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	billSeedOpp(t, db, "opp-cur")
	repo := repository.NewQuoteRepositoryWithDB(db)

	// 一版明确是 EUR 的报价（列默认值只补 NULL，不补空串 —— 空串那格另测）。
	eur := &model.Quote{ID: "q_cur_eur", QuoteID: "QT-CUR-EUR", OpportunityID: "opp-cur",
		Status: model.QuoteStatusDraft, Currency: "EUR"}
	if err := repo.Create(ctx, eur); err != nil {
		t.Fatalf("落 EUR 报价失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, eur.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("推 EUR 报送到 sent 失败: %v", err)
	}
	billSeedLines(t, db, eur.ID)
	first, err := billSvc(t, db).DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: eur.ID})
	if err != nil {
		t.Fatalf("EUR 那版派生失败: %v", err)
	}
	if got := billRow(t, db, first.ID); got.Currency != "EUR" {
		t.Errorf("账单币种 %q，报价是 EUR：跟着抄，不许换成默认值", got.Currency)
	}

	// 报价那格空串 ⇒ 落到 CNY 默认值（账单列不能空：空币种的金额不可算）。
	bare := &model.Quote{ID: "q_cur_bare", QuoteID: "QT-CUR-BARE", OpportunityID: "opp-cur",
		Status: model.QuoteStatusDraft, Currency: ""}
	if err := repo.Create(ctx, bare); err != nil {
		t.Fatalf("落空币种报价失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, bare.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("推空币种报送到 sent 失败: %v", err)
	}
	billSeedLines(t, db, bare.ID)
	second, err := billSvc(t, db).DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: bare.ID})
	if err != nil {
		t.Fatalf("空币种那版派生失败: %v", err)
	}
	if got := billRow(t, db, second.ID); got.Currency != model.BillCurrencyDefault {
		t.Errorf("空币种报价派生出的账单币种是 %q，期望默认 %q", got.Currency, model.BillCurrencyDefault)
	}
}

// TestBillDeriveLeavesDueAtNull 账期不凭空补。
//
// 报价模板里的 valid_days 是**报价有效期**，不是"多少天内付款"。
// 在这里给它写个默认 30 天，运营就会把它读成合同条款（判据见 model/bill.go 的 DueAt 注释）。
func TestBillDeriveLeavesDueAtNull(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-due")
	rowID := billSeedQuote(t, db, "q_due", "QT-DUE", "opp-due", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	view, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}
	if view.DueAt != nil {
		t.Errorf("派生出的账单带了账期 %v：本卡没有任何一处定义过账期", *view.DueAt)
	}
	// 库里那一格也要真是 NULL（视图判空而库里落了零值，是另一件事）。
	var n int64
	if err := db.Model(&model.Bill{}).Where("id = ? AND due_at IS NULL", view.ID).Count(&n).Error; err != nil {
		t.Fatalf("查 due_at 失败: %v", err)
	}
	if n != 1 {
		t.Error("库里那一行的 due_at 不是 NULL")
	}
}

// —— 边界：什么算成交 ——————————————————————————————————————————————————

// TestBillDeriveRequiresSentVersion 只有 sent 那一版能被确认成交。
//
// draft 是"还没给客户看过"，rejected 是"客户回了不要"，expired 是"那版已经作废"。
// 三格都派不出应收；其中 expired 那一格最容易被"先接单后补流程"的人顺手打开。
func TestBillDeriveRequiresSentVersion(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	svc := billSvc(t, db)
	for i, st := range []string{model.QuoteStatusDraft, model.QuoteStatusRejected, model.QuoteStatusExpired} {
		opp := fmt.Sprintf("opp-ns%d", i)
		billSeedOpp(t, db, opp)
		rowID := billSeedQuote(t, db, fmt.Sprintf("q_ns%d", i), fmt.Sprintf("QT-NS%d", i), opp, st)
		billSeedLines(t, db, rowID)
		_, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
		if !errors.Is(err, ErrBillQuoteNotSent) {
			t.Errorf("%s 的报价竟然能派生账单，err=%v", st, err)
			continue
		}
		var quote model.Quote
		if err := db.First(&quote, "id = ?", rowID).Error; err != nil {
			t.Fatalf("读回报价失败: %v", err)
		}
		if quote.Status != st {
			t.Errorf("被拒的确认把报价状态从 %q 改成了 %q", st, quote.Status)
		}
	}
	if n := billCount(t, db); n != 0 {
		t.Errorf("三笔被拒的确认之后库里有 %d 张账单", n)
	}
}

// TestBillDeriveAcceptsAlreadyAcceptedRowAgain 已 accepted 的那一版再确认一次 = 幂等复用。
//
// 它不是"边界放行"：那一版确实成交过，账单也确实该有。重入这条路是给
// "CAS 成功但账单没落下去"那个崩溃窗口留的（见文件头 顺序 那一段）。
func TestBillDeriveAcceptsAlreadyAcceptedRowAgain(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	billSeedOpp(t, db, "opp-acc")
	rowID := billSeedQuote(t, db, "q_acc", "QT-ACC", "opp-acc", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	svc := billSvc(t, db)
	first, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("派生失败: %v", err)
	}
	second, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("已 accepted 的那版重入失败: %v", err)
	}
	if second.ID != first.ID || !second.Reused {
		t.Errorf("重入没复用到那张: %q/%v vs %q", second.ID, second.Reused, first.ID)
	}
	if n := billCount(t, db); n != 1 {
		t.Errorf("重入之后库里有 %d 张账单", n)
	}
}

// TestBillDeriveRejectsSupersededVersion 不能拿链上被取代的旧版确认成交。
//
// 客户手上看到的是最新那一版。按旧版开应收，开出的是**另一份价格**——
// 金额不同、且库里两处都说得通（旧版确实 sent 过），对账时才发现两张单子讲不同价钱。
func TestBillDeriveRejectsSupersededVersion(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-sup")
	v1 := billSeedQuote(t, db, "q_sup", "QT-SUP", "opp-sup", model.QuoteStatusSent)
	billSeedLines(t, db, v1)
	repo := repository.NewQuoteRepositoryWithDB(db)
	v2 := &model.Quote{ID: "q_sup_v2", QuoteID: "QT-SUP", OpportunityID: "opp-sup",
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault}
	if err := repo.Append(context.Background(), v1, v2); err != nil {
		t.Fatalf("追加第二版失败: %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), v2.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("推第二版失败: %v", err)
	}
	billSeedLines(t, db, v2.ID)

	_, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: v1})
	if !errors.Is(err, ErrBillNotLatestVersion) {
		t.Errorf("按被取代的旧版确认成交没报 ErrBillNotLatestVersion：%v", err)
	}
	if n := billCount(t, db); n != 0 {
		t.Errorf("被拒的确认还是开出了 %d 张账单", n)
	}
	// 最新那一版照样能确认（守卫不能把正路一起堵死）。
	if _, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: v2.ID}); err != nil {
		t.Errorf("确认链上最新那一版被挡了：%v", err)
	}
}

// TestBillDeriveRejectsSecondAcceptedVersionInChain 一条链上只留一次成交。
//
// v1 已被接受（账单也开出来了）之后，有人又改出一版让客户签 —— 那是**重新成交**，
// 原账单一式两份会变成两张应收。本卡不自动作废原来那张（作废的判据要看回款，
// 而回款行在 T-P7-02），所以宁可把第二次确认挡在门外，让人先去处理那一张。
func TestBillDeriveRejectsSecondAcceptedVersionInChain(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	billSeedOpp(t, db, "opp-twice")
	v1 := billSeedQuote(t, db, "q_tw", "QT-TW", "opp-twice", model.QuoteStatusSent)
	billSeedLines(t, db, v1)
	svc := billSvc(t, db)
	if _, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: v1}); err != nil {
		t.Fatalf("第一次确认失败: %v", err)
	}
	// 把 v1 拉回"链上不是最新"的形状，再让 v2 走到 sent。
	repo := repository.NewQuoteRepositoryWithDB(db)
	v2 := &model.Quote{ID: "q_tw_v2", QuoteID: "QT-TW", OpportunityID: "opp-twice",
		Status: model.QuoteStatusDraft, Currency: model.QuoteCurrencyDefault}
	if err := repo.Append(ctx, v1, v2); err != nil {
		t.Fatalf("追加第二版失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, v2.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("推第二版失败: %v", err)
	}
	billSeedLines(t, db, v2.ID)

	_, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: v2.ID})
	if !errors.Is(err, ErrBillChainAlreadyAccepted) {
		t.Errorf("一条链上第二次确认成交没报 ErrBillChainAlreadyAccepted：%v", err)
	}
	if n := billCount(t, db); n != 1 {
		t.Errorf("这条链上现在有 %d 张应收，期望仍是 1", n)
	}
}

// TestBillDeriveNeedsLines 没有行项目的版本派不出账单，且**不动报价状态**。
//
// 合计 0.00 的应收与"这一版根本没行"是两回事：前者会进对账，后者连派生的输入都没有。
// 更要紧的是顺序判据：这条只读检查必须在第一次写之前跑完 ——
// 否则库里留下一版 accepted 而永远派不出账单的报价（行项目不会自己长回来）。
func TestBillDeriveNeedsLines(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-nl")
	rowID := billSeedQuote(t, db, "q_nl", "QT-NL", "opp-nl", model.QuoteStatusSent)
	spy := &billQuoteSpy{QuoteRepository: repository.NewQuoteRepositoryWithDB(db)}
	svc := NewBillService(repository.NewBillRepositoryWithDB(db), spy)
	svc.SetClock(func() time.Time { return qsClockBase })

	_, err := svc.DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: rowID})
	if !errors.Is(err, ErrBillLinesMissing) {
		t.Errorf("没有行项目的版本没报 ErrBillLinesMissing：%v", err)
	}
	if spy.casCalls != 0 {
		t.Errorf("只读判据失败前已经尝试了 %d 次状态跃迁：所有只读检查必须在第一次写之前", spy.casCalls)
	}
	var quote model.Quote
	if err := db.First(&quote, "id = ?", rowID).Error; err != nil {
		t.Fatalf("读回报价失败: %v", err)
	}
	if quote.Status != model.QuoteStatusSent {
		t.Errorf("报价被推成了 %q：行项目缺失的版不该留下成交现场", quote.Status)
	}
	if n := billCount(t, db); n != 0 {
		t.Errorf("库里多出 %d 张账单", n)
	}
}

// —— 失败路径 ——————————————————————————————————————————————————————————

// TestBillDeriveCASFailureLeavesNoBill 跃迁写不进去时不派生。
//
// 反过来（先派账单再改报价状态）会产出"报价没成交而库里有一张应收"——
// 那是要人去删的数据，而本表没有删除口。
func TestBillDeriveCASFailureLeavesNoBill(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	billSeedOpp(t, db, "opp-cas")
	rowID := billSeedQuote(t, db, "q_cas", "QT-CAS", "opp-cas", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	spy := &billQuoteSpy{QuoteRepository: repository.NewQuoteRepositoryWithDB(db), failCAS: true}
	svc := NewBillService(repository.NewBillRepositoryWithDB(db), spy)

	_, err := svc.DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: rowID})
	if !errors.Is(err, ErrBillStatusStuck) {
		t.Errorf("跃迁失败没报 ErrBillStatusStuck：%v", err)
	}
	if !errors.Is(err, errCASBoom) {
		t.Errorf("跃迁的底层错因被丢掉了：%v", err)
	}
	if spy.casCalls != 1 {
		t.Errorf("跃迁尝试了 %d 次，期望恰好 1 次（失败后不重试：重试会把并发对手那一版再推一次）", spy.casCalls)
	}
	if n := billCount(t, db); n != 0 {
		t.Errorf("报价状态没落成而库里已经有 %d 张账单", n)
	}
}

// TestBillDeriveRacesIntoExisting Create 撞到"已派生过"时复用那一行。
//
// 这一格在真库里造不出"只失败一次"的并发，所以包一层开关。
// 判据是：报这条错的路径**不再往上抛**，而是回读已有那一行并标 Reused；
// 反过来（把 ErrBillAlreadyDerived 直接抛给调用方）的症状是
// 销售点第二次确认就看到一句"这张报价已经开过账单了"，而账单在他眼前凭空消失。
func TestBillDeriveRacesIntoExisting(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	billSeedOpp(t, db, "opp-race")
	rowID := billSeedQuote(t, db, "q_race", "QT-RACE", "opp-race", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	realRepo := repository.NewBillRepositoryWithDB(db)
	existing := &model.Bill{ID: "b_existing_race", QuoteID: "QT-RACE", QuoteRowID: rowID,
		OpportunityID: "opp-race", Amount: 369.99, Currency: model.BillCurrencyDefault,
		Status: model.BillStatusOpen, CreatedAt: qsClockBase, UpdatedAt: qsClockBase}
	if err := realRepo.Create(ctx, existing); err != nil {
		t.Fatalf("预置那张账单失败: %v", err)
	}
	spy := &billStoreSpy{BillRepository: realRepo, failDerivedAlways: true}
	svc := NewBillService(spy, repository.NewQuoteRepositoryWithDB(db))

	view, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("并发窗口里的确认失败了（该复用那一行）: %v", err)
	}
	if view.ID != "b_existing_race" || !view.Reused {
		t.Errorf("复用到的是 %q(Reused=%v)，期望库里那一行 b_existing_race", view.ID, view.Reused)
	}
}

// TestBillDeriveReportsReadBackFailure 读不回那一行时明确报错，不把内存里那份当结论返回。
//
// 返回值会被写进 API 响应。库里到底有没有、金额是多少，只能以读回来的为准。
// 后半段断的是这一格的**可恢复性**：账单其实落库了，只是这一次没读回来 ——
// 换一台正常服务再确认一次，应当复用那一行而不是报"已存在"或再插一张。
func TestBillDeriveReportsReadBackFailure(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	billSeedOpp(t, db, "opp-rb")
	rowID := billSeedQuote(t, db, "q_rb", "QT-RB", "opp-rb", model.QuoteStatusSent)
	billSeedLines(t, db, rowID)
	spy := &billStoreSpy{BillRepository: repository.NewBillRepositoryWithDB(db), failReadBack: true}
	svc := NewBillService(spy, repository.NewQuoteRepositoryWithDB(db))

	_, err := svc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if !errors.Is(err, errReadBackBoom) {
		t.Errorf("底层错因没被带出来（调用方只能猜是读失败还是写失败）：%v", err)
	}
	if errors.Is(err, repository.ErrBillAlreadyDerived) {
		t.Errorf("读失败被报成了幂等冲突：%v", err)
	}
	recovered, err := billSvc(t, db).DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: rowID})
	if err != nil {
		t.Fatalf("换正常服务再确认一次失败（这一格本该可恢复）: %v", err)
	}
	if !recovered.Reused {
		t.Error("恢复那一次没报 Reused：调用方会以为自己又开了一张应收")
	}
	if n := billCount(t, db); n != 1 {
		t.Errorf("折腾两轮之后库里有 %d 张账单，期望 1", n)
	}
}

// TestBillDeriveQuoteNotFound 版本行根本不存在时，报"不存在"而不是"没装配"。
//
// 分诊码不同：前者是调用方拿错了号（前端刷新就好），后者是部署事故。
func TestBillDeriveQuoteNotFound(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	_, err := billSvc(t, db).DeriveFromQuote(context.Background(), BillDeriveInput{QuoteRowID: "q_nope"})
	if !errors.Is(err, ErrBillQuoteNotFound) {
		t.Errorf("%v", err)
	}
}

// —— 契约形状 ——————————————————————————————————————————————————————————

// TestBillDeriveInputHasOnlyTheRowKey 入参白名单：一格里只有版本行号。
//
// 多开一格 amount / status / currency，就是把"账单是报价的派生物"改成
// "账单是人填的表"——AC② 从此没有对账对象，而这是本卡存在的全部理由。
func TestBillDeriveInputHasOnlyTheRowKey(t *testing.T) {
	st := reflect.TypeOf(BillDeriveInput{})
	if st.NumField() != 1 {
		t.Fatalf("BillDeriveInput 有 %d 个字段，期望恰好 1 个（版本行号）", st.NumField())
	}
	if f := st.Field(0); f.Name != "QuoteRowID" || f.Type.Kind() != reflect.String {
		t.Errorf("唯一那一格是 %s %s，期望 QuoteRowID string", f.Name, f.Type.Kind())
	}
}

// TestBillServicePortSurfacesAreNarrow 两条接缝的方法集合逐字钉住（反射比，多一个少一个都红）。
//
// 账单接缝里**没有**状态写口：派生这一条腿不跃迁账单状态，接口里没有，
// 将来就写不出"派生时顺手标成 paid"——而那一格真正的调用方在 T-P7-02（回款累计到位）。
// 报价接缝里**没有** Create / Append：确认成交的腿不许顺手造报价，
// 否则"账单由已成交报价派生"会退化成"账单由这条腿自己编出来的报价派生"。
//
// 两边各四格、且 reflect 按字典序返回，所以期望清单也按字典序写。
func TestBillServicePortSurfacesAreNarrow(t *testing.T) {
	bills := portMethodNames((*billStore)(nil))
	wantBills := []string{"Available", "Create", "GetByID", "GetByQuoteRowID"}
	if got := strings.Join(bills, ","); got != strings.Join(wantBills, ",") {
		t.Errorf("billStore 方法集=%v，期望恰好 %v", bills, wantBills)
	}
	quotes := portMethodNames((*billQuoteStore)(nil))
	wantQuotes := []string{"GetByID", "ListLines", "ListVersions", "UpdateStatus"}
	if got := strings.Join(quotes, ","); got != strings.Join(wantQuotes, ",") {
		t.Errorf("billQuoteStore 方法集=%v，期望恰好 %v", quotes, wantQuotes)
	}
}

func portMethodNames(ptr any) []string {
	st := reflect.TypeOf(ptr).Elem()
	out := make([]string, st.NumMethod())
	for i := 0; i < st.NumMethod(); i++ {
		out[i] = st.Method(i).Name
	}
	return out
}

// TestBillKeyGeneratorIsDeterministic 账单号生成器：同一时刻同一 seq ⇒ 同一个号。
//
// 与 newQuoteKeys 同一口径：纳秒 + 进程内单调计数，**不含日期串**（含日期就要选时区，
// 而本仓 PG 会话钉 CST、Go 侧按宿主机时区读，同一时刻能生成两个"当天序号"）。
func TestBillKeyGeneratorIsDeterministic(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	a, b := newBillKey(at, 7), newBillKey(at, 7)
	if a != b {
		t.Errorf("同一时刻同一 seq 生成 %q / %q", a, b)
	}
	if !strings.HasPrefix(a, "b_") {
		t.Errorf("账单号 %q 不以 b_ 开头（与 b36 编号前缀同族，运营在库里肉眼分域）", a)
	}
	if strings.Contains(a, "2026") || strings.Contains(a, "-") {
		t.Errorf("账单号 %q 里带了日期形状：时区裂脑那条老账会在这把键上重演", a)
	}
	if newBillKey(at, 8) == a {
		t.Error("seq 参与生成之后仍然同号")
	}
}

// TestBillServiceAvailabilityAndNilSafety 半装配与 nil 接收者都要报得出来、不许 panic。
func TestBillServiceAvailabilityAndNilSafety(t *testing.T) {
	db := billSetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	if NewBillService(nil, repository.NewQuoteRepositoryWithDB(db)).Available() {
		t.Error("缺账单存储还报 Available")
	}
	if NewBillService(repository.NewBillRepositoryWithDB(db), nil).Available() {
		t.Error("缺报价存储还报 Available")
	}
	if _, err := NewBillService(nil, nil).DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: "q_x"}); !errors.Is(err, ErrBillServiceUnavailable) {
		t.Errorf("未装配时没报 ErrBillServiceUnavailable：%v", err)
	}
	var nilSvc *BillService
	if nilSvc.Available() {
		t.Error("nil 服务报告可用")
	}
	if _, err := nilSvc.DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: "q_x"}); !errors.Is(err, ErrBillServiceUnavailable) {
		t.Errorf("nil 服务没报未装配：%v", err)
	}
	if _, err := billSvc(t, db).DeriveFromQuote(ctx, BillDeriveInput{QuoteRowID: "   "}); !errors.Is(err, ErrBillInputInvalid) {
		t.Errorf("空白行号没报 ErrBillInputInvalid：%v", err)
	}
}

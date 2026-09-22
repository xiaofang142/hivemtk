// payment_test.go T-P7-02：回款入账与结清层（一笔到账 → payments 一行 → bills.status 跟着钱动）。
//
// 本层要证的六件事，逐件独立成例（摘掉任何一件不会让别件变红）：
//
//	AC② 「同 channel_ref 不重复入账」= 重复回调在库里**只留一行**，判据打在 Count 上，
//	     而"内容不同的同一流水号"不许被幂等通路吞掉（吞掉的后果是把两笔钱记成一笔）。
//	结清 「bills.status 是 Σ 计入结清的函数」= 全额 ⇒ paid、部分 ⇒ partial、
//	     被冲销回零 ⇒ 退回 open；三条边都由真库读回那一行来断，不看返回值。
//	冲销 「reversed 不是删掉」= 那一行还在、金额与来路列逐字不动，只有 status 与 updated_at 动。
//	值域 「币种不符的钱不进同一个和」= 账单 CNY 而这笔 USD ⇒ 拒，且不写任何一行。
//	来路 「账单不存在 / 账单已作废」⇒ 拒，钱不落账（作废的凭据不再收钱）。
//	顺序 「判据全在第一次写之前」= 上面几类拒写之后库里 payments 与 bills 两边都是原样。
//
// 替身只在真库造不出那一格时才加（与 bill_test.go 同一口径）：存储层是真仓储 + 真库，
// 只有"跃迁写不进去"这一格需要一个包住真仓储的开关（payStuckBillStore）。
package service

import (
	"context"
	"errors"
	"math"
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

func paySetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 只建 bills + payments：入账腿不读报价、不读商机（结清的判据只在"这张应收"与
	// "名下的回款行"之间，多建一张表就遮住了一次偷偷回查）。
	return testutil.NewTestDB(t, &model.Bill{}, &model.Payment{})
}

// payBill 落一张应收。amount 由用例给出（**手写的**，不从被测代码算出来）。
func payBill(t *testing.T, db *gorm.DB, id, quoteID, rowID string, amount float64) *model.Bill {
	t.Helper()
	repo := repository.NewBillRepositoryWithDB(db)
	row := &model.Bill{
		ID: id, QuoteID: quoteID, QuoteRowID: rowID, OpportunityID: "opp-" + quoteID,
		Amount: amount, Currency: model.BillCurrencyDefault, Status: model.BillStatusOpen,
		CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
	}
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatalf("落账单 %s 失败: %v", id, err)
	}
	return row
}

// paySvc 装一台真库上的入账服务。
func paySvc(t *testing.T, db *gorm.DB) *PaymentService {
	t.Helper()
	svc := NewPaymentService(
		repository.NewPaymentRepositoryWithDB(db),
		repository.NewBillRepositoryWithDB(db),
	)
	svc.SetClock(func() time.Time { return qsClockBase })
	return svc
}

// payIn 造一次入账入参（渠道三件套给全：来路列不许空，理由见 model/payment.go）。
func payIn(billID, ref string, amount float64) RecordPaymentInput {
	return RecordPaymentInput{
		BillID: billID, ChannelRef: ref, Amount: amount,
		Platform: "taobao", OrderID: "ord-" + ref,
	}
}

// payRows 数 payments 里有几行（幂等判据打在行数上，不是返回值）。
func payRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.Payment{}).Count(&n).Error; err != nil {
		t.Fatalf("清点回款行失败: %v", err)
	}
	return n
}

// payBillRow 从库里读回那张账单（断言打在落库那一行）。
func payBillRow(t *testing.T, db *gorm.DB, id string) *model.Bill {
	t.Helper()
	var row model.Bill
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("读回账单 %s 失败: %v", id, err)
	}
	return &row
}

// payPaymentRow 从库里读回那一笔回款。
func payPaymentRow(t *testing.T, db *gorm.DB, ref string) *model.Payment {
	t.Helper()
	var row model.Payment
	if err := db.First(&row, "channel_ref = ?", ref).Error; err != nil {
		t.Fatalf("读回回款行 %s 失败: %v", ref, err)
	}
	return &row
}

// payStuckBillStore 只把"跃迁"这一格换成失败：真库能让 CAS 命不中，
// 但让**任何**跃迁都失败（模拟库抖 / 约束没建出来）只能包一层。
type payStuckBillStore struct {
	repository.BillRepository
	err error
}

func (s payStuckBillStore) UpdateStatus(ctx context.Context, id, from, to string) error {
	return s.err
}

// —— ① 入账与结清 ——————————————————————————————————————————————

// TestPaymentServiceSettlesBySum 账单状态是那笔和的函数：部分 ⇒ partial，全额 ⇒ paid。
//
// 金额 400.00 是手写的，两笔回款 150.00 + 250.00 也是手写的：
// 期望值不来自被测代码里的任何一次加法（否则 AC② 就成了"这个函数等于它自己"）。
func TestPaymentServiceSettlesBySum(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_settle", "QT-SETTLE", "q_settle_v1", 400.00)
	svc := paySvc(t, db)

	rec, err := svc.RecordPayment(ctx, payIn("b_settle", "ref-settle-1", 150.00))
	if err != nil {
		t.Fatalf("首笔入账失败: %v", err)
	}
	if rec.Reused {
		t.Error("第一次入账被报成复用")
	}
	if got := payBillRow(t, db, "b_settle").Status; got != model.BillStatusPartial {
		t.Errorf("收到 150/400 后账单状态 %q，期望 %q", got, model.BillStatusPartial)
	}
	if rec.Settlement.Settled != 150.00 || rec.Settlement.Outstanding != 250.00 {
		t.Errorf("结算视图 = %+v，期望 settled=150 outstanding=250", rec.Settlement)
	}

	if _, err := svc.RecordPayment(ctx, payIn("b_settle", "ref-settle-2", 250.00)); err != nil {
		t.Fatalf("第二笔入账失败: %v", err)
	}
	back := payBillRow(t, db, "b_settle")
	if back.Status != model.BillStatusPaid {
		t.Errorf("累计到位后账单状态 %q，期望 %q", back.Status, model.BillStatusPaid)
	}
	if back.Amount != 400.00 {
		t.Errorf("入账顺手把应收改成了 %v：amount 是凭证，只有派生那一次写它", back.Amount)
	}
	if n := payRows(t, db); n != 2 {
		t.Errorf("库里 %d 行回款，期望 2", n)
	}
}

// TestPaymentServiceOverpaymentIsVisibleNotRejected 多收的那一口不拒、但必须看得见。
//
// 方向选"收下 + 标注"而不是"拒掉"：拒掉的后果是渠道已经收到的钱在我方账上**没有记录**，
// 那是财务上最贵的一种错（少记）；收下的后果是 settled>amount，对账两侧一比就看得见，
// 且视图上有 oversettled 这一格替它发声。
func TestPaymentServiceOverpaymentIsVisibleNotRejected(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_over", "QT-OVER", "q_over_v1", 100.00)
	svc := paySvc(t, db)

	rec, err := svc.RecordPayment(ctx, payIn("b_over", "ref-over-1", 180.00))
	if err != nil {
		t.Fatalf("多收的一笔被拒了（少记一笔钱比多记一列贵）: %v", err)
	}
	if !rec.Settlement.Oversettled {
		t.Errorf("视图没有标出超收：%+v", rec.Settlement)
	}
	if rec.Settlement.Outstanding != 0 {
		t.Errorf("欠额给成了 %v，期望 0（欠额不许为负：那一格读的是「还该去催收多少」）", rec.Settlement.Outstanding)
	}
	if got := payBillRow(t, db, "b_over").Status; got != model.BillStatusPaid {
		t.Errorf("超收后账单状态 %q，期望 %q", got, model.BillStatusPaid)
	}
}

// —— ② AC② 幂等 ——————————————————————————————————————————————

// TestPaymentServiceSameChannelRefBooksOnce 同一渠道流水号重投三次只入账一次（AC②）。
func TestPaymentServiceSameChannelRefBooksOnce(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_idem", "QT-IDEM", "q_idem_v1", 500.00)
	svc := paySvc(t, db)

	first, err := svc.RecordPayment(ctx, payIn("b_idem", "ref-idem", 200.00))
	if err != nil {
		t.Fatalf("首投失败: %v", err)
	}
	if first.Reused {
		t.Error("首投被报成复用")
	}
	for i := 0; i < 2; i++ {
		again, err := svc.RecordPayment(ctx, payIn("b_idem", "ref-idem", 200.00))
		if err != nil {
			t.Fatalf("重投第 %d 次失败: %v", i+1, err)
		}
		if !again.Reused {
			t.Errorf("重投第 %d 次没被认成复用：%+v", i+1, again.Payment)
		}
		if again.Payment.ID != first.Payment.ID {
			t.Errorf("复用回的不是同一行：%s vs %s", again.Payment.ID, first.Payment.ID)
		}
	}
	if n := payRows(t, db); n != 1 {
		t.Fatalf("三次回调之后库里 %d 行回款，期望 1（多出来的每一行都会把欠额记成已收）", n)
	}
	if got := payBillRow(t, db, "b_idem").Status; got != model.BillStatusPartial {
		t.Errorf("账单状态 %q，期望 %q（重投把 200/500 推成了别的数）", got, model.BillStatusPartial)
	}
}

// TestPaymentServiceRejectsReusedRefWithDifferentContent 同一流水号、不同的钱 ⇒ 拒。
//
// 幂等通路只认"同一笔"：bill_id / 金额 / 币种任一不同就不是同一笔，
// 此时复用的后果是把第二笔真钱吞掉 —— 渠道那边两笔都在，我方只有一行。
func TestPaymentServiceRejectsReusedRefWithDifferentContent(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_reuse_1", "QT-R1", "q_r1_v1", 500.00)
	payBill(t, db, "b_reuse_2", "QT-R2", "q_r2_v1", 500.00)
	svc := paySvc(t, db)

	if _, err := svc.RecordPayment(ctx, payIn("b_reuse_1", "ref-reuse", 200.00)); err != nil {
		t.Fatalf("首笔失败: %v", err)
	}
	for _, tc := range []struct {
		name string
		in   RecordPaymentInput
	}{
		{"换金额", payIn("b_reuse_1", "ref-reuse", 200.01)},
		{"换账单", payIn("b_reuse_2", "ref-reuse", 200.00)},
	} {
		_, err := svc.RecordPayment(ctx, tc.in)
		if !errors.Is(err, ErrPaymentRefReuse) {
			t.Errorf("%s：期望 ErrPaymentRefReuse，实际 %v", tc.name, err)
		}
	}
	if n := payRows(t, db); n != 1 {
		t.Errorf("被拒的两笔之后库里 %d 行，期望仍是 1", n)
	}
	if got := payBillRow(t, db, "b_reuse_2").Status; got != model.BillStatusOpen {
		t.Errorf("被拒之后 b_reuse_2 状态 %q，期望仍是 %q", got, model.BillStatusOpen)
	}
}

// —— ③ 冲销 ——————————————————————————————————————————————

// TestPaymentServiceReversalMovesStatusBack 渠道把这笔钱冲掉时，欠额必须回来。
//
// 这一条是 T-P7-02 放宽 bills 跃迁表的全部理由（见 model/bill.go 那段）：
// 全额到账 ⇒ paid，同一 channel_ref 再以 reversed 推一次 ⇒ 那一行不删、status 转 reversed，
// 账单从 paid 退回 open，settled 归零。
func TestPaymentServiceReversalMovesStatusBack(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_rev", "QT-REV", "q_rev_v1", 300.00)
	svc := paySvc(t, db)

	if _, err := svc.RecordPayment(ctx, payIn("b_rev", "ref-rev", 300.00)); err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if got := payBillRow(t, db, "b_rev").Status; got != model.BillStatusPaid {
		t.Fatalf("全额之后账单是 %q，夹具没铺对（后面那条断言就没意义了）", got)
	}

	revIn := payIn("b_rev", "ref-rev", 300.00)
	revIn.Status = model.PaymentStatusReversed
	rec, err := svc.RecordPayment(ctx, revIn)
	if err != nil {
		t.Fatalf("冲销失败: %v", err)
	}
	if rec.Payment.Status != model.PaymentStatusReversed {
		t.Errorf("回款行状态 %q，期望 %q", rec.Payment.Status, model.PaymentStatusReversed)
	}
	row := payPaymentRow(t, db, "ref-rev")
	if row.Status != model.PaymentStatusReversed {
		t.Errorf("库里那一行是 %q：冲销没落库，视图说了假话", row.Status)
	}
	if row.Amount != 300.00 || row.BillID != "b_rev" || row.Platform != "taobao" {
		t.Errorf("冲销改写了凭据列: %+v", row)
	}
	if got := payBillRow(t, db, "b_rev").Status; got != model.BillStatusOpen {
		t.Errorf("钱被冲掉而账单还停在 %q：那正是要挡的「两头对不上」", got)
	}
	if rec.Settlement.Settled != 0 {
		t.Errorf("settled = %v，期望 0（求和侧必须只认计入结清的那些状态）", rec.Settlement.Settled)
	}

	// 同一笔钱再冲一次：幂等，不报错也不产生第二行。
	if _, err := svc.RecordPayment(ctx, revIn); err != nil {
		t.Errorf("重复冲销报错了: %v", err)
	}
	// 冲销过的行不许被一次普通重投复活。
	if _, err := svc.RecordPayment(ctx, payIn("b_rev", "ref-rev", 300.00)); errors.Is(err, nil) {
		t.Error("已冲销的回款被一次重投复活了：那笔钱会重新计进结清")
	} else if !errors.Is(err, ErrPaymentAlreadyReversed) {
		t.Errorf("复活失败报的不是 ErrPaymentAlreadyReversed：%v", err)
	}
	if n := payRows(t, db); n != 1 {
		t.Errorf("冲销与重投之后库里 %d 行，期望仍是 1（reversed 不是再插一行）", n)
	}
}

// TestPaymentServiceReversalOfUnknownRefIsRejected 冲销一笔从没记过的钱 ⇒ 拒。
//
// 不"顺手补一行 reversed"：那会让库里出现一笔从没到账过的钱的记录，
// 而 reversed 那一格的全部含义是"这一笔曾经算数过"。
func TestPaymentServiceReversalOfUnknownRefIsRejected(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_norev", "QT-NOREV", "q_norev_v1", 100.00)
	svc := paySvc(t, db)

	in := payIn("b_norev", "ref-nobody", 100.00)
	in.Status = model.PaymentStatusReversed
	if _, err := svc.RecordPayment(ctx, in); !errors.Is(err, ErrPaymentNothingToReverse) {
		t.Errorf("期望 ErrPaymentNothingToReverse，实际 %v", err)
	}
	if n := payRows(t, db); n != 0 {
		t.Errorf("被拒的冲销留下了 %d 行", n)
	}
}

// —— ④ 来路与值域 ——————————————————————————————————————————————

func TestPaymentServiceRejectsBadProvenanceAndCurrency(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_prov", "QT-PROV", "q_prov_v1", 100.00)
	// 已作废的那张：作废 = 不再主张，钱不该落在它身上（落上去会把它从 voided 里拽出来）。
	voided := payBill(t, db, "b_void", "QT-VOID", "q_void_v1", 100.00)
	if err := repository.NewBillRepositoryWithDB(db).
		UpdateStatus(ctx, voided.ID, model.BillStatusOpen, model.BillStatusVoided); err != nil {
		t.Fatalf("把夹具推到 voided 失败: %v", err)
	}
	svc := paySvc(t, db)

	missingBill := payIn("b_不存在", "ref-prov-1", 50.00)
	noRef := payIn("b_prov", "  ", 50.00)
	negAmount := payIn("b_prov", "ref-prov-2", -50.00)
	badStatus := payIn("b_prov", "ref-prov-3", 50.00)
	badStatus.Status = "refunded"
	mismatch := payIn("b_prov", "ref-prov-4", 50.00)
	mismatch.Currency = "USD"
	onVoided := payIn("b_void", "ref-prov-5", 50.00)
	noPlatform := payIn("b_prov", "ref-prov-6", 50.00)
	noPlatform.Platform = ""

	for _, tc := range []struct {
		name string
		in   RecordPaymentInput
		want error
	}{
		{"账单不存在", missingBill, ErrPaymentBillNotFound},
		{"渠道流水号为空", noRef, ErrPaymentInputInvalid},
		{"金额为负", negAmount, ErrPaymentInputInvalid},
		{"载荷自述状态越域", badStatus, ErrPaymentInputInvalid},
		{"币种与账单不符", mismatch, ErrPaymentCurrencyMismatch},
		{"账单已作废", onVoided, ErrPaymentBillVoided},
		{"没有平台来路", noPlatform, ErrPaymentInputInvalid},
	} {
		_, err := svc.RecordPayment(ctx, tc.in)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s：期望 %v，实际 %v", tc.name, tc.want, err)
		}
	}
	if n := payRows(t, db); n != 0 {
		t.Errorf("七笔全被拒之后库里仍有 %d 行回款", n)
	}
	if got := payBillRow(t, db, "b_prov").Status; got != model.BillStatusOpen {
		t.Errorf("被拒的入账动了账单状态: %q", got)
	}
}

// TestPaymentServiceFillsPaidAtAndSaysSo 载荷没给到账时间：填接收时刻，并在视图上承认是填的。
func TestPaymentServiceFillsPaidAtAndSaysSo(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_time", "QT-TIME", "q_time_v1", 100.00)
	svc := paySvc(t, db)

	given := payIn("b_time", "ref-time-1", 10.00)
	stamp := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	given.PaidAt = &stamp
	rec, err := svc.RecordPayment(ctx, given)
	if err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	if rec.Payment.FilledPaidAt {
		t.Error("渠道给了到账时间却在视图上标成「系统填的」")
	}
	if !rec.Payment.PaidAt.Equal(stamp) {
		t.Errorf("paid_at = %v，期望渠道给的那个 %v", rec.Payment.PaidAt, stamp)
	}

	filled := payIn("b_time", "ref-time-2", 10.00)
	rec2, err := svc.RecordPayment(ctx, filled)
	if err != nil {
		t.Fatalf("第二笔入账失败: %v", err)
	}
	if !rec2.Payment.FilledPaidAt {
		t.Error("载荷没给 paid_at 却没标出来：运营会把它读成渠道给的时间")
	}
	if !rec2.Payment.PaidAt.Equal(qsClockBase) {
		t.Errorf("填充值 = %v，期望接收时刻 %v", rec2.Payment.PaidAt, qsClockBase)
	}
	if row := payPaymentRow(t, db, "ref-time-2"); !row.PaidAt.Equal(qsClockBase) {
		t.Errorf("库里那一行的 paid_at = %v，与视图不一致", row.PaidAt)
	}
}

// —— ⑤ 装配与故障 ——————————————————————————————————————————————

func TestPaymentServiceAvailabilityAndFailures(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_stuck", "QT-STUCK", "q_stuck_v1", 100.00)

	// 半装配：两把句柄缺一即不能入账（与账单派生腿同一条判据）。
	if NewPaymentService(nil, nil).Available() {
		t.Error("两把句柄都是 nil 却报告可用")
	}
	half := NewPaymentService(repository.NewPaymentRepositoryWithDB(db), nil)
	if half.Available() {
		t.Error("只有回款句柄也报告可用")
	}
	if _, err := half.RecordPayment(ctx, payIn("b_stuck", "ref-half", 10.00)); !errors.Is(err, ErrPaymentServiceUnavailable) {
		t.Errorf("半装配下的入账报的不是 ErrPaymentServiceUnavailable：%v", err)
	}
	if _, err := half.Statement(ctx, "b_stuck"); !errors.Is(err, ErrPaymentServiceUnavailable) {
		t.Errorf("半装配下的读口报的不是 ErrPaymentServiceUnavailable：%v", err)
	}

	// 跃迁写不进去：那一笔钱**已经**落库了，报错必须把这件事说清楚。
	svc := NewPaymentService(repository.NewPaymentRepositoryWithDB(db),
		payStuckBillStore{BillRepository: repository.NewBillRepositoryWithDB(db), err: errors.New("boom")})
	_, err := svc.RecordPayment(ctx, payIn("b_stuck", "ref-stuck", 100.00))
	if !errors.Is(err, ErrPaymentStatusStuck) {
		t.Fatalf("期望 ErrPaymentStatusStuck，实际 %v", err)
	}
	if n := payRows(t, db); n != 1 {
		t.Errorf("跃迁失败之后回款行数 %d，期望 1（钱记下了就是记下了，回滚它等于少记一笔）", n)
	}
	if !strings.Contains(err.Error(), "已入账") {
		t.Errorf("错误文案没交代「钱已经落库」这件事：%v", err)
	}
	if got := payBillRow(t, db, "b_stuck").Status; got != model.BillStatusOpen {
		t.Errorf("跃迁失败而账单变成了 %q", got)
	}
	// 换回真句柄再走一次同一笔：钱不重复记，状态补得上（幂等通路自愈）。
	real := paySvc(t, db)
	rec, err := real.RecordPayment(ctx, payIn("b_stuck", "ref-stuck", 100.00))
	if err != nil {
		t.Fatalf("重放同一笔失败: %v", err)
	}
	if !rec.Reused {
		t.Error("重放同一 channel_ref 没走复用通路")
	}
	if got := payBillRow(t, db, "b_stuck").Status; got != model.BillStatusPaid {
		t.Errorf("重放之后账单 %q，期望 %q", got, model.BillStatusPaid)
	}
	if n := payRows(t, db); n != 1 {
		t.Errorf("重放之后回款行数 %d，期望 1", n)
	}
}

// —— ⑥ 读侧视图 ——————————————————————————————————————————————

// TestPaymentServiceStatementAnswersTheReconciliationPair 读侧答的就是结转遗留①那两问：
// 一张账单收了多少、还欠多少，名下行按流水顺序列出来。
func TestPaymentServiceStatement(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	payBill(t, db, "b_stmt", "QT-STMT", "q_stmt_v1", 1000.00)
	svc := paySvc(t, db)

	for i, tc := range []struct {
		ref    string
		amount float64
	}{{"ref-stmt-a", 300.00}, {"ref-stmt-b", 200.00}} {
		if _, err := svc.RecordPayment(ctx, payIn("b_stmt", tc.ref, tc.amount)); err != nil {
			t.Fatalf("入账 %d 失败: %v", i+1, err)
		}
	}
	// 第三笔被渠道冲掉：它仍在名单里（流水顺序），只是不计入和。
	rev := payIn("b_stmt", "ref-stmt-c", 50.00)
	if _, err := svc.RecordPayment(ctx, rev); err != nil {
		t.Fatalf("入账第三笔失败: %v", err)
	}
	rev.Status = model.PaymentStatusReversed
	if _, err := svc.RecordPayment(ctx, rev); err != nil {
		t.Fatalf("冲销第三笔失败: %v", err)
	}

	st, err := svc.Statement(ctx, "b_stmt")
	if err != nil {
		t.Fatalf("读账单对账视图失败: %v", err)
	}
	if st.Amount != 1000.00 || st.Settled != 500.00 || st.Outstanding != 500.00 {
		t.Errorf("对账三格 = amount %v / settled %v / outstanding %v，期望 1000/500/500",
			st.Amount, st.Settled, st.Outstanding)
	}
	if st.Status != model.BillStatusPartial {
		t.Errorf("状态 %q，期望 %q", st.Status, model.BillStatusPartial)
	}
	if len(st.Payments) != 3 {
		t.Fatalf("名单里 %d 行，期望 3（被冲掉的那一笔也要在名单上）：%+v", len(st.Payments), st.Payments)
	}
	if st.Payments[2].ChannelRef != "ref-stmt-c" || st.Payments[2].Status != model.PaymentStatusReversed {
		t.Errorf("名单第三行不对：%+v", st.Payments[2])
	}
	for _, p := range st.Payments {
		if p.Status == model.PaymentStatusReversed {
			continue
		}
		if math.Abs(p.Amount) <= 0 {
			t.Errorf("名单里有零金额的行：%+v", p)
		}
	}
	if st.Oversettled {
		t.Error("500/1000 被标成超收")
	}

	// 复读一次必须给出同一个数（视图里不许有随调用时刻变的东西）。
	again, err := svc.Statement(ctx, "b_stmt")
	if err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if again.Amount != st.Amount || again.Settled != st.Settled || len(again.Payments) != len(st.Payments) {
		t.Errorf("两次读到的对账数不同：%+v vs %+v", again, st)
	}
	if _, err := svc.Statement(ctx, "b_不存在"); !errors.Is(err, ErrPaymentBillNotFound) {
		t.Errorf("读不存在的账单报的不是 ErrPaymentBillNotFound：%v", err)
	}
	if _, err := svc.Statement(ctx, " "); !errors.Is(err, ErrPaymentInputInvalid) {
		t.Errorf("空账单号被放行：%v", err)
	}
}

// TestPaymentServiceStatementsOfQuote 那张报价开了几张应收 —— 逐张带上收了多少钱。
//
// 这一条就是结转遗留①的兑现点：一张报价单的多个版本各自成交过一次时（改价后又接受），
// 对账读的是"这一串里哪几张还欠着"，而它没有库的读口。
func TestPaymentServiceStatementsOfQuote(t *testing.T) {
	db := paySetupDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	ctx := context.Background()
	// 同一张报价（QT-CHAIN）的两个版本行各开一张应收。
	payBill(t, db, "b_chain_1", "QT-CHAIN", "q_chain_v1", 700.00)
	payBill(t, db, "b_chain_2", "QT-CHAIN", "q_chain_v2", 800.00)
	payBill(t, db, "b_other", "QT-OTHER", "q_other_v1", 900.00)
	svc := paySvc(t, db)

	if _, err := svc.RecordPayment(ctx, payIn("b_chain_2", "ref-chain-2", 800.00)); err != nil {
		t.Fatalf("结清 b_chain_2 失败: %v", err)
	}
	if _, err := svc.RecordPayment(ctx, payIn("b_chain_1", "ref-chain-1", 100.00)); err != nil {
		t.Fatalf("部分回款失败: %v", err)
	}

	list, err := svc.StatementsOfQuote(ctx, "QT-CHAIN")
	if err != nil {
		t.Fatalf("按报价号读失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("捞回 %d 张，期望 2（多一张就是把别家的应收算进这份对账）", len(list))
	}
	if list[0].BillID != "b_chain_1" || list[1].BillID != "b_chain_2" {
		t.Errorf("顺序不是派生早晚：%s, %s", list[0].BillID, list[1].BillID)
	}
	if list[0].Settled != 100.00 || list[0].Outstanding != 600.00 {
		t.Errorf("第一张的对账数不对：%+v", list[0])
	}
	if list[1].Settled != 800.00 || list[1].Outstanding != 0 || list[1].Status != model.BillStatusPaid {
		t.Errorf("第二张的对账数不对：%+v", list[1])
	}

	// 零命中：空列表 + 无错。
	none, err := svc.StatementsOfQuote(ctx, "QT-没开过单")
	if err != nil {
		t.Errorf("零命中报成了错误: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("零命中捞回 %d 张", len(none))
	}
	if _, err := svc.StatementsOfQuote(ctx, ""); !errors.Is(err, ErrPaymentInputInvalid) {
		t.Errorf("空报价号被放行：%v", err)
	}
}

// —— ⑦ 接口形状 ——————————————————————————————————————————————

// TestPaymentServicePortSurfacesAreNarrow 两条入边的方法集合用反射钉住。
//
// 判据与账单派生腿那两条同一条：接口里没有的写口，这条腿就写不出来。
//   - paymentStore **没有** 改金额 / 改账单号 / Delete：回款行是凭据；
//   - settlementBillStore **没有** Create：入账这条腿不许凭空造一张应收；
//     也没有 ListByQuoteRowID 之类的读口，本层只按 bill_id 读。
func TestPaymentServicePortSurfacesAreNarrow(t *testing.T) {
	for _, tc := range []struct {
		name string
		port any
		want []string
	}{
		{"paymentStore", (*paymentStore)(nil),
			[]string{"Available", "Create", "GetByChannelRef", "ListByBillID", "MarkReversed", "SumSettledByBill"}},
		{"settlementBillStore", (*settlementBillStore)(nil),
			[]string{"Available", "GetByID", "ListByQuoteID", "UpdateStatus"}},
	} {
		st := reflect.TypeOf(tc.port).Elem()
		if st.NumMethod() != len(tc.want) {
			t.Fatalf("%s 方法数 %d ≠ %d", tc.name, st.NumMethod(), len(tc.want))
		}
		for _, name := range tc.want {
			if _, ok := st.MethodByName(name); !ok {
				t.Errorf("%s 缺方法 %s", tc.name, name)
			}
		}
		for _, banned := range []string{"Delete", "Save", "Upsert", "UpdateAmount", "Create"} {
			if _, ok := st.MethodByName(banned); ok && !(banned == "Create" && tc.name == "paymentStore") {
				t.Errorf("%s 里有 %s：这条腿的权限面比判据需要的大", tc.name, banned)
			}
		}
	}
}

// TestRecordPaymentInputIsWhitelistShaped 入参结构里没有账单号以外的"来路"格。
//
// 尤其没有 amount 之外的第二个金额、没有 status 之外的第二个方向位、
// 没有 quote_id / opportunity_id（副本会漂，来路只有 bill_id 一条 —— 见 model/payment.go）。
func TestRecordPaymentInputIsWhitelistShaped(t *testing.T) {
	st := reflect.TypeOf(RecordPaymentInput{})
	want := []string{"BillID", "ChannelRef", "Amount", "Currency", "PaidAt", "Platform", "OrderID", "Status"}
	if st.NumField() != len(want) {
		t.Fatalf("RecordPaymentInput 字段数 %d ≠ %d（%v）", st.NumField(), len(want), st)
	}
	for _, name := range want {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在", name)
		}
	}
	for _, banned := range []string{"QuoteID", "QuoteRowID", "OpportunityID", "BillAmount", "RefundedAmount"} {
		if _, ok := st.FieldByName(banned); ok {
			t.Errorf("入参里有 %s：那一格从调用方递进来就是第二个事实源", banned)
		}
	}
}

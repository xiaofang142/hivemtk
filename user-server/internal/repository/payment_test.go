// payment_test.go T-P7-02：回款仓储（表 payments）。
//
// 与 bills 那一族同一个分工：本文件只测**只有真库才能证明**的事，八条：
//  1. AC② 的幂等键：同 channel_ref 的第二笔报的是**那条约束**（ErrPaymentAlreadyRecorded），
//     而主键撞车一类的别的 23505 不许被认成"这笔已经记过了"—— 认错的方向是把一次
//     本该失败的写入吞成一次成功复用，而这一格吞掉的是钱；
//  2. 身份列守卫：空 bill_id 的回款行不知道冲的是哪张应收，空 channel_ref 更糟 ——
//     它在库里是一个**永远撞不上的幂等键**（PG 的 unique 不约束 NULL，但约束空串），
//     两条合起来 = 每一笔都能插进去、且再没有一条判据能挡住重复；
//  3. 金额与状态的值域守在这里：库里没有 CHECK，一列拼错的状态会静默改变结清金额；
//  4. 结清求和只算"计入结清"的那几格，且用 SQL 侧的 SUM/ROUND 而不是逐行搬到 Go 里加；
//  5. 冲销是 CAS：起点不是 confirmed 就不生效，且写集合只有 status + updated_at；
//  6. 接口形状即"回款行不可改写"：没有 Delete、没有改金额/账单号的方法；
//  7. "没有这笔"与"读不到"分得开（同 bills 的第 7 条判据）；
//  8. 缺句柄时 Available 为假、每笔读写明确报错而不是 panic。
//
// 刻意**没有内存版底座**：AC② 的实现位置是库上的唯一索引，不是 Go 里的 map ——
// 有影子实现时这条 AC 永远测不出来（与 bills / quotes 同一取向）。
package repository

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
)

func setupPaymentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 只建 payments：回款仓储不碰账单表（读账单是 service 的活）。
	// 把 bills 一起建反而会遮住"仓储偷偷去查账单"这种越界。
	return testutil.NewTestDB(t, &model.Payment{})
}

// newPaymentRow 造一笔回款。paid_at 显式给出（本文件的排序判据读的就是它，靠"默认零值"
// 的话两条流水谁在前是随机的）；created_at / updated_at **刻意留零**，交给 gorm 打真实时间戳：
// 夹具一旦把 updated_at 写成固定值，"本次调用有没有推进 updated_at"就只能拿一个
// 与墙钟无关的数当基准（第一版红在这里 —— 写死的基准在将来，真实时间戳永远追不上）。
func newPaymentRow(id, billID, ref string, amount float64) *model.Payment {
	base := time.Date(2026, 3, 4, 2, 3, 4, 0, time.UTC)
	return &model.Payment{
		ID: id, BillID: billID, Amount: amount, Currency: model.PaymentCurrencyDefault,
		PaidAt: base, ChannelRef: ref, Status: model.PaymentStatusConfirmed,
		Platform: "taobao", OrderID: "ord-" + ref,
	}
}

func TestPaymentRepository_CreateRejectsMissingIdentity(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*model.Payment)
	}{
		{"空行号", func(p *model.Payment) { p.ID = "" }},
		{"空账单号", func(p *model.Payment) { p.BillID = "" }},
		{"空渠道流水号", func(p *model.Payment) { p.ChannelRef = "" }},
		{"只有空白的渠道流水号", func(p *model.Payment) { p.ChannelRef = "   " }},
		{"超长渠道流水号", func(p *model.Payment) { p.ChannelRef = strings.Repeat("x", 129) }},
		{"零金额", func(p *model.Payment) { p.Amount = 0 }},
		{"负金额", func(p *model.Payment) { p.Amount = -12.5 }},
		{"超出列量程的金额", func(p *model.Payment) { p.Amount = model.PaymentAmountLimit * 2 }},
		{"空平台", func(p *model.Payment) { p.Platform = "" }},
		{"空外部订单号", func(p *model.Payment) { p.OrderID = "" }},
		{"空币种", func(p *model.Payment) { p.Currency = "" }},
		{"状态不在值域", func(p *model.Payment) { p.Status = "paid" }},
	}
	for i, tc := range cases {
		row := newPaymentRow(fmt.Sprintf("p_guard_%d", i), "b_guard", "ref-guard-"+tc.name, 100.00)
		tc.mutate(row)
		err := repo.Create(ctx, row)
		if err == nil {
			t.Errorf("%s 被收下：payments 是资金凭据表，缺来路或缺方向的行会把结清算错", tc.name)
			continue
		}
		if errors.Is(err, ErrPaymentAlreadyRecorded) {
			t.Errorf("%s 被报成\"这笔已入账\"：%v", tc.name, err)
		}
	}
}

// TestPaymentRepository_DuplicateChannelRefIsItsOwnError AC② 的物理形态。
//
// 两臂都要：① 同 channel_ref（不同行号、不同账单）⇒ 必须是 ErrPaymentAlreadyRecorded，
// service 据此走"回读那一行并比对内容"这条路；② 撞主键（同行号、不同流水号）⇒
// **不许**是那个 sentinel —— 把它读成"已入账"会让一次坏写入被静默吞成一次成功复用，
// 而调用方随后回读到的那一行与它以为写进去的那一行不是同一笔钱。
func TestPaymentRepository_DuplicateChannelRefIsItsOwnError(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	first := newPaymentRow("p_dup_1", "b_dup", "ref-dup-1", 500.00)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("首笔入账失败: %v", err)
	}

	second := newPaymentRow("p_dup_2", "b_other", "ref-dup-1", 600.00)
	err := repo.Create(ctx, second)
	if !errors.Is(err, ErrPaymentAlreadyRecorded) {
		t.Errorf("重复 channel_ref 应报 ErrPaymentAlreadyRecorded，实际 %v", err)
	}

	pk := newPaymentRow("p_dup_1", "b_dup", "ref-dup-pk", 700.00)
	err = repo.Create(ctx, pk)
	if errors.Is(err, ErrPaymentAlreadyRecorded) {
		t.Error("撞主键被读成\"这笔已入账\"：幂等键与主键必须分得开")
	}
	if err == nil {
		t.Fatal("撞主键的写入被收下")
	}

	// 库里到底几行：首笔 + 撞主键那笔都没进来，只有 ref-dup-1 一行。
	rows, err := repo.ListByBillID(ctx, "b_dup")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Amount != 500.00 {
		t.Errorf("期望 b_dup 上只有 500.00 那笔，实际 %+v", rows)
	}
}

// TestPaymentRepository_SumCountsOnlySettledStatuses 结清金额的加法面。
//
// 用 SQL 侧的 SUM 而不是把行搬到 Go 里逐笔累加：一次分期能有几十笔，
// 逐笔 float64 相加的舍入轨迹与列上的 numeric(14,2) 不同源，
// 结清判据（Σ ≥ amount）就会在刚好相等的那一单上翻脸。
func TestPaymentRepository_SumCountsOnlySettledStatuses(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	seed := []struct {
		id, ref, bill string
		amount        float64
		status        string
	}{
		{"p_sum_1", "ref-sum-1", "b_sum", 100.10, model.PaymentStatusConfirmed},
		{"p_sum_2", "ref-sum-2", "b_sum", 200.20, model.PaymentStatusConfirmed},
		{"p_sum_3", "ref-sum-3", "b_sum", 999.99, model.PaymentStatusReversed},
		{"p_sum_4", "ref-sum-4", "b_other", 50.00, model.PaymentStatusConfirmed},
	}
	for _, s := range seed {
		row := newPaymentRow(s.id, s.bill, s.ref, s.amount)
		row.Status = s.status
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("种 %s 失败: %v", s.id, err)
		}
	}

	got, err := repo.SumSettledByBill(ctx, "b_sum")
	if err != nil {
		t.Fatalf("求和失败: %v", err)
	}
	if got != 300.30 {
		t.Errorf("已收合计 = %v，期望 300.30（两笔 confirmed；reversed 那一笔不计、别单的也不计）", got)
	}

	// 没有回款的账单：0 而不是错误（"这张还没收到钱"是合法答案）。
	zero, err := repo.SumSettledByBill(ctx, "b_none")
	if err != nil {
		t.Errorf("无回款账单求和报错: %v", err)
	}
	if zero != 0 {
		t.Errorf("无回款账单求和 = %v，期望 0", zero)
	}

	if _, err := repo.SumSettledByBill(ctx, "  "); err == nil {
		t.Error("空账单号的求和被放行：它会退化成\"全表求和\"，结清判据当场变成另一个数")
	}
}

// TestPaymentRepository_MarkReversedIsCompareAndSet 冲销只走 CAS，且写集合只有两列。
func TestPaymentRepository_MarkReversedIsCompareAndSet(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	row := newPaymentRow("p_cas_1", "b_cas", "ref-cas-1", 321.45)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("入账失败: %v", err)
	}
	// 先取"这一行自己"的冲销前快照，再冲：updated_at / paid_at 两条判据比的都是它，
	// 而不是夹具里的某个固定值。
	before, err := repo.GetByID(ctx, "p_cas_1")
	if err != nil || before == nil {
		t.Fatalf("冲销前读回失败: %v", err)
	}
	if err := repo.MarkReversed(ctx, "p_cas_1"); err != nil {
		t.Fatalf("首次冲销失败: %v", err)
	}
	// 第二次：已经是 reversed ⇒ 冲突，不是"又冲掉一次"（幂等由 service 在写之前判，
	// 这里守的是"重复调用不会把别的东西改掉"）。
	if err := repo.MarkReversed(ctx, "p_cas_1"); !errors.Is(err, ErrPaymentNotConfirmed) {
		t.Errorf("重复冲销应报 ErrPaymentNotConfirmed，实际 %v", err)
	}
	if err := repo.MarkReversed(ctx, "p_missing"); !errors.Is(err, ErrPaymentNotFound) {
		t.Errorf("冲销不存在的行应报 ErrPaymentNotFound，实际 %v", err)
	}
	if err := repo.MarkReversed(ctx, "  "); err == nil {
		t.Error("空行号被放行")
	}
	back, err := repo.GetByID(ctx, "p_cas_1")
	if err != nil || back == nil {
		t.Fatalf("读回失败: %v row=%v", err, back)
	}
	if back.Status != model.PaymentStatusReversed {
		t.Errorf("状态未落到 reversed：%s", back.Status)
	}
	if back.Amount != 321.45 || back.BillID != "b_cas" || back.ChannelRef != "ref-cas-1" {
		t.Errorf("冲销顺手改了内容列：%+v", back)
	}
	// 与上面那份 before 比：只有本次调用写过这一列，它才会前进。
	if !back.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("updated_at 没被推进：before=%v after=%v", before.UpdatedAt, back.UpdatedAt)
	}
	// paid_at 是"钱哪天到过账"的历史事实，被冲掉不等于它没发生过。
	if !back.PaidAt.Equal(before.PaidAt) {
		t.Errorf("冲销改了 paid_at：before=%v after=%v", before.PaidAt, back.PaidAt)
	}
}

// TestPaymentRepository_ReadsDistinguishMissingFromFailure 三条读路径的 (nil,nil) 与 error 分家。
func TestPaymentRepository_ReadsDistinguishMissingFromFailure(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	row := newPaymentRow("p_get_1", "b_get", "ref-get-1", 10.00)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("入账失败: %v", err)
	}

	got, err := repo.GetByChannelRef(ctx, "ref-get-1")
	if err != nil || got == nil || got.ID != "p_get_1" {
		t.Fatalf("按渠道号读不回来: err=%v row=%+v", err, got)
	}
	missing, err := repo.GetByChannelRef(ctx, "ref-none")
	if err != nil {
		t.Errorf("不存在的渠道号报了错误: %v", err)
	}
	if missing != nil {
		t.Errorf("不存在的渠道号带回了一行: %+v", missing)
	}

	deadCtx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		read func(context.Context) (*model.Payment, error)
	}{
		{"GetByID", func(ctx context.Context) (*model.Payment, error) { return repo.GetByID(ctx, "p_get_1") }},
		{"GetByChannelRef", func(ctx context.Context) (*model.Payment, error) { return repo.GetByChannelRef(ctx, "ref-get-1") }},
	} {
		got, err := tc.read(deadCtx)
		if err == nil {
			t.Errorf("%s 在库故障时回了 nil 错误（got=%v）：故障被读成\"没有这笔\"，service 会据此再插一笔", tc.name, got)
			continue
		}
		if got != nil {
			t.Errorf("%s 故障时既报错又带回行：%+v", tc.name, got)
		}
	}
	if _, err := repo.SumSettledByBill(deadCtx, "b_get"); err == nil {
		t.Error("求和在库故障时静默回 0：账单会被读成\"一分没收\"并退回到 open")
	}
	rows, err := repo.ListByBillID(deadCtx, "b_get")
	if err == nil {
		t.Error("列表在库故障时静默回空：同上")
	}
	if rows != nil {
		t.Errorf("列表故障时既报错又带回数据：%+v", rows)
	}
}

// TestPaymentRepository_ListByBillIDIsScopedAndOrdered 只回本单、按到账时点排。
func TestPaymentRepository_ListByBillIDIsScopedAndOrdered(t *testing.T) {
	db := setupPaymentTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewPaymentRepositoryWithDB(db)
	ctx := context.Background()

	base := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)
	seed := []struct {
		id, ref, bill string
		paid          time.Time
	}{
		{"p_lst_1", "ref-lst-1", "b_lst", base.Add(2 * time.Hour)},
		{"p_lst_2", "ref-lst-2", "b_lst", base.Add(1 * time.Hour)},
		{"p_lst_3", "ref-lst-3", "b_else", base},
	}
	for _, s := range seed {
		row := newPaymentRow(s.id, s.bill, s.ref, 20.00)
		row.PaidAt = s.paid
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("种 %s 失败: %v", s.id, err)
		}
	}
	rows, err := repo.ListByBillID(ctx, "b_lst")
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 行，实际 %d（别单的行漏进来了？）", len(rows))
	}
	if rows[0].ID != "p_lst_2" || rows[1].ID != "p_lst_1" {
		t.Errorf("未按 paid_at 升序：%s, %s", rows[0].ID, rows[1].ID)
	}
	if _, err := repo.ListByBillID(ctx, " "); err == nil {
		t.Error("空账单号的列表被放行：它会退化成全表扫，而这是凭证表")
	}
}

// TestPaymentRepository_MethodSetIsExactlyTheDocumentedSix 接口形状即"回款行不可改写"。
func TestPaymentRepository_MethodSetIsExactlyTheDocumentedSix(t *testing.T) {
	want := []string{"Available", "Create", "GetByID", "GetByChannelRef", "ListByBillID", "MarkReversed", "SumSettledByBill"}
	st := reflect.TypeOf((*PaymentRepository)(nil)).Elem()
	if st.NumMethod() != len(want) {
		t.Fatalf("PaymentRepository 方法数 %d ≠ %d", st.NumMethod(), len(want))
	}
	for _, name := range want {
		if _, ok := st.MethodByName(name); !ok {
			t.Errorf("方法 %s 不存在", name)
		}
	}
	for i := 0; i < st.NumMethod(); i++ {
		name := st.Method(i).Name
		for _, banned := range []string{"Save", "Delete", "Upsert", "Find", "First", "All", "Update"} {
			if strings.HasPrefix(name, banned) {
				t.Errorf("接口里出现了 %s：回款行建后只能被冲销，改金额/改账单号/抹行都不在本卡的接口形状里", name)
			}
		}
	}
}

// TestPaymentRepository_ConstraintNameIsTheOneOnTheModel 幂等键的名字与模型标签同源。
//
// 认错约束名的后果与 bills 同一条：别的 23505 被读成"这笔已入账"，
// service 于是回读一条从没写过的行 —— 一路静默到对账那天。
func TestPaymentRepository_ConstraintNameIsTheOneOnTheModel(t *testing.T) {
	f, ok := reflect.TypeOf(model.Payment{}).FieldByName("ChannelRef")
	if !ok {
		t.Fatal("模型里没有 ChannelRef 这一格，幂等键无从谈起")
	}
	tag := f.Tag.Get("gorm")
	if !strings.Contains(tag, paymentChannelRefConstraint) {
		t.Errorf("仓储用的约束名 %q 不在模型标签里：%s", paymentChannelRefConstraint, tag)
	}
	if paymentChannelRefConstraint != "uq_payments_channel_ref" {
		t.Errorf("约束名漂了：%s", paymentChannelRefConstraint)
	}
}

// TestPaymentRepository_NilHandleFailsLoudly 半装配的失败面。
func TestPaymentRepository_NilHandleFailsLoudly(t *testing.T) {
	repo := NewPaymentRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄报告 Available=true")
	}
	ctx := context.Background()
	if err := repo.Create(ctx, newPaymentRow("p_nil", "b_nil", "ref-nil", 1)); err == nil {
		t.Error("nil 句柄下的 Create 没报错")
	}
	if _, err := repo.GetByID(ctx, "p_nil"); err == nil {
		t.Error("nil 句柄下的 GetByID 没报错")
	}
	if _, err := repo.GetByChannelRef(ctx, "ref-nil"); err == nil {
		t.Error("nil 句柄下的 GetByChannelRef 没报错")
	}
	if _, err := repo.ListByBillID(ctx, "b_nil"); err == nil {
		t.Error("nil 句柄下的列表没报错")
	}
	if _, err := repo.SumSettledByBill(ctx, "b_nil"); err == nil {
		t.Error("nil 句柄下的求和没报错")
	}
	if err := repo.MarkReversed(ctx, "p_nil"); err == nil {
		t.Error("nil 句柄下的冲销没报错")
	}
	var typed *paymentRepo
	if typed.Available() {
		t.Error("nil 接收者报告可用")
	}
	_ = gorm.ErrRecordNotFound
}

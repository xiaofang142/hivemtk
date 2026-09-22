// bill_test.go T-P7-01：账单仓储（表 bills）。
//
// 本文件只测**只有真库才能证明**的事，六条：
//  1. AC① 的幂等键：同一版的第二张账单报的是**那条约束**（ErrBillAlreadyDerived），
//     而主键撞车一类的别的 23505 不许被认成"已经派生过了"—— 认错的方向是
//     "把一次本该失败的写入静默吞成一次成功复用"，账单是钱，这一条不能让；
//  2. 身份列非空守卫：空 quote_row_id 的账单既引用不了报价、也排除不了重复，
//     它在库里就是一张谁也追不回来源的应收；
//  3. 状态值域守在这里：bills.status 是 varchar(16)，库里没有 CHECK，
//     一格拼错的状态既扫不到也改不动，两种坏法都静默；
//  4. 跃迁是 CAS：`WHERE id = ? AND status = ?` 起点过期不生效，且写集合只有
//     status + updated_at —— 金额列如果能被这条路径顺手改到，对账就没主体了；
//  5. 接口形状即"账单不可改写"：没有 Delete、没有改内容列的方法；
//  6. 缺句柄时 Available 为假、写入明确报错而不是 panic。
//
// 与 quotes / opportunities 同一取向：本卡刻意**没有内存版底座** ——
// 一旦有影子实现，"同一版被派生过两次"就永远测不出来，而那正是 AC① 的全部内容。
//
// 方法集与 model 标签的对应关系：
//   - Create / GetByID / GetByQuoteRowID / UpdateStatus 四条对应 bills 的四处列；
//   - Available 与 require 是装配回显与半装配守卫（同 quote 仓储的形状）。
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

func setupBillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 只建 bills：账单仓储不碰报价表（读报价是 service 的活）。
	// 把 quotes 一起建反而会遮住"仓储偷偷去查报价"这种越界。
	return testutil.NewTestDB(t, &model.Bill{})
}

// newBillRow 造一张账单。两个时间戳显式给出：updated_at "有没有被本次调用推进"
// 是本文件的判据之一，靠 autoUpdateTime 的话基准值本身就是随机的。
func newBillRow(id, quoteID, rowID, oppID string) *model.Bill {
	base := time.Date(2026, 10, 12, 8, 30, 0, 0, time.UTC)
	return &model.Bill{
		ID: id, QuoteID: quoteID, QuoteRowID: rowID, OpportunityID: oppID,
		Amount: 1890.50, Currency: model.BillCurrencyDefault,
		Status: model.BillStatusOpen, CreatedAt: base, UpdatedAt: base,
	}
}

func TestBillRepository_CreateRejectsMissingIdentity(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()

	for _, tc := range []struct{ field, id, quoteID, rowID, oppID string }{
		{"ID", "", "QT-1", "q-1", "opp-1"},
		{"quote_id", "b-1", "", "q-1", "opp-1"},
		{"quote_row_id", "b-1", "QT-1", "", "opp-1"},
		{"opportunity_id", "b-1", "QT-1", "q-1", ""},
	} {
		b := &model.Bill{ID: tc.id, QuoteID: tc.quoteID, QuoteRowID: tc.rowID, OpportunityID: tc.oppID,
			Amount: 10, Currency: model.BillCurrencyDefault, Status: model.BillStatusOpen}
		err := repo.Create(ctx, b)
		if err == nil {
			t.Errorf("%s 为空竟然写得进去：库里多了一张追不回来源的应收", tc.field)
			continue
		}
		if errors.Is(err, ErrBillAlreadyDerived) {
			t.Errorf("%s 为空报成了\"已派生过\"：%v", tc.field, err)
		}
	}
	var n int64
	if err := db.Model(&model.Bill{}).Count(&n).Error; err != nil {
		t.Fatalf("清点失败: %v", err)
	}
	if n != 0 {
		t.Errorf("四笔被拒的写入之后库里应有 0 行，实际 %d", n)
	}
}

// TestBillRepository_DuplicateQuoteRowIsItsOwnError 两种 23505 必须分得开。
//
// ① 同一 quote_row_id 的第二张 ⇒ ErrBillAlreadyDerived（service 据此走"复用已有那张"，
//
//	这是 AC① 的幂等通路）；
//
// ② 同一个主键 ID 但**不同**的报价行 ⇒ 那是另一次调用把自己的行键写重了，
//
//	不许被 ① 的通路吞掉 —— 吞掉的后果是"以为这张账单已经存在"，
//	而它其实一次都没写过。
func TestBillRepository_DuplicateQuoteRowIsItsOwnError(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()

	if err := repo.Create(ctx, newBillRow("b_dup_1", "QT-A", "q_a_v1", "opp-a")); err != nil {
		t.Fatalf("首张账单写入失败: %v", err)
	}
	err := repo.Create(ctx, newBillRow("b_dup_2", "QT-A", "q_a_v1", "opp-a"))
	if !errors.Is(err, ErrBillAlreadyDerived) {
		t.Errorf("同一版重复派生报的不是 ErrBillAlreadyDerived：%v", err)
	}
	if err := repo.Create(ctx, newBillRow("b_dup_3", "QT-A", "q_a_v2", "opp-a")); err != nil {
		t.Errorf("同一报价单的**另一版**被拒了（多版并存不成立）: %v", err)
	}
	// 主键撞车：同 ID、不同报价行。
	if err := repo.Create(ctx, newBillRow("b_dup_1", "QT-B", "q_b_v1", "opp-b")); err == nil {
		t.Error("重复主键写得进去了：行键生成器重号将无人发现")
	} else if errors.Is(err, ErrBillAlreadyDerived) {
		t.Errorf("重复主键被认成\"已派生过\"：幂等复用一个从没写过的行 ⇒ %v", err)
	}
}

// TestBillRepository_StatusMustBeInTheDomain 值域守卫住在离库最近的那一层。
//
// bills.status 是 varchar(16)，本仓不建 CHECK 约束（与 quotes / approval_requests 同源），
// 所以一格拼错的状态会安然落库，然后**同时**坏两件事，且两种坏法都是静默的：
//   - T-P7-03 的逾期扫描条件是 status ∈ {open, partial}，这行从此扫不到 ——
//     运营读到的是"这张单没有逾期"这个假事实；
//   - 跃迁的 WHERE status = ? 再也命不中它，这行从此改不动 —— 一张其实收清了的账
//     永远停在 "paid_" 上，对账两边都读不到它。
//
// 守放在 Create 与 UpdateStatus 两侧：只守入口的话，未来第一个跃迁方（T-P7-02 的
// 回款累计）传错值照样落库；只守跃迁的话，派生那一步就漏了。
func TestBillRepository_StatusMustBeInTheDomain(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()

	// 反面清单挑的都是"看着像对"的形状：全大写 = 值域里逐字比的对应词，
	// 带下划线/尾空格 = 复制粘贴的产物，"sent" = 隔壁报价表的词，
	// "overdue" = 本卡刻意**不**做成状态的那个词（理由见 model/bill.go 的 BillStatuses 注释）。
	for i, bad := range []string{"", "OPEN", "open ", "overdue", "sent", "unpaid"} {
		row := newBillRow(fmt.Sprintf("b_dom_%d", i), fmt.Sprintf("QT-D%d", i), fmt.Sprintf("q_d_v%d", i), "opp-d")
		row.Status = bad
		err := repo.Create(ctx, row)
		if err == nil {
			t.Errorf("派生时状态 %q 落库成功了：它从此既扫不到也改不动", bad)
			continue
		}
		if errors.Is(err, ErrBillAlreadyDerived) {
			t.Errorf("状态 %q 报成了幂等冲突：%v（会把没写成功的行读成已存在）", bad, err)
		}
	}
	var n int64
	if err := db.Model(&model.Bill{}).Count(&n).Error; err != nil {
		t.Fatalf("清点失败: %v", err)
	}
	if n != 0 {
		t.Errorf("六笔状态非法的派生之后库里应有 0 行，实际 %d", n)
	}

	seed := newBillRow("b_dom_seed", "QT-DS", "q_ds_v1", "opp-ds")
	if err := repo.Create(ctx, seed); err != nil {
		t.Fatalf("合法状态的种子写入失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, "b_dom_seed", model.BillStatusOpen, "paid_"); err == nil {
		t.Error("跃迁到了值域外的状态：库里现在有一张谁也读不懂的应收")
	}
	if err := repo.UpdateStatus(ctx, "b_dom_seed", "opne", model.BillStatusPaid); err == nil {
		t.Error("起点是值域外的拼错词也返回了成功")
	}
	var after model.Bill
	if err := db.First(&after, "id = ?", "b_dom_seed").Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if after.Status != model.BillStatusOpen {
		t.Errorf("两次被拒的跃迁改动了状态：%q", after.Status)
	}

	// 合法跃迁照旧放行：守卫不能把值域内的边也堵死。
	if err := repo.UpdateStatus(ctx, "b_dom_seed", model.BillStatusOpen, model.BillStatusPaid); err != nil {
		t.Errorf("open→paid 被值域守卫挡了：%v", err)
	}
}

func TestBillRepository_ReadsDistinguishMissingFromFailure(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()
	seed := newBillRow("b_read_1", "QT-R", "q_r_v1", "opp-r")
	if err := repo.Create(ctx, seed); err != nil {
		t.Fatalf("种子写入失败: %v", err)
	}

	got, err := repo.GetByID(ctx, "b_read_1")
	if err != nil || got == nil {
		t.Fatalf("按 ID 读回失败: got=%v err=%v", got, err)
	}
	if got.Amount != 1890.50 || got.QuoteRowID != "q_r_v1" || got.Status != model.BillStatusOpen {
		t.Errorf("读回的账单不是写进去的那张: %+v", got)
	}
	missing, err := repo.GetByID(ctx, "b_nope")
	if err != nil || missing != nil {
		t.Errorf("不存在的行应回 (nil, nil)，实际 got=%v err=%v", missing, err)
	}
	byRow, err := repo.GetByQuoteRowID(ctx, "q_r_v1")
	if err != nil || byRow == nil || byRow.ID != "b_read_1" {
		t.Errorf("按报价行读回失败（幂等复用要走这条路）: got=%v err=%v", byRow, err)
	}
	missingRow, err := repo.GetByQuoteRowID(ctx, "q_nope")
	if err != nil || missingRow != nil {
		t.Errorf("不存在的报价行应回 (nil, nil)，实际 got=%v err=%v", missingRow, err)
	}
}

// TestBillRepository_ReadFailureIsNotMissingRead 查不了 ≠ 没有这张单。
//
// 上面那条只走了"确实没有这一行"那一支（record not found ⇒ (nil, nil)），
// 于是 billOrNil 里 `err != nil` 那一支**一条用例都不经过**：把它改成也回 (nil, nil)，
// 全文件照样绿（电池格 K38 实测抓到的就是这个缺口）。
//
// 缺这一支的代价不在读侧，在写侧：派生路径把"还没有账单"当作继续建单的依据，
// 一次库抖动被读成 (nil, nil) ⇒ 同一版报价长出两张应收，而 uq_bills_quote_row
// 只在第二张**真的写进去**时才拦得住 —— 读错了的那一次连撞键的机会都没有。
//
// 故障用取消的 context 注入，不用"表没建"那种搭法：后者取决于同进程里
// 前一条用例建没建过 bills（testutil 的影子库按进程命名，不是按用例），
// 那会把判据挂在文件顺序上。
func TestBillRepository_ReadFailureIsNotMissingRead(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)

	deadCtx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		name string
		read func(context.Context) (*model.Bill, error)
	}{
		{"GetByID", func(ctx context.Context) (*model.Bill, error) {
			return repo.GetByID(ctx, "b_read_1")
		}},
		{"GetByQuoteRowID", func(ctx context.Context) (*model.Bill, error) {
			return repo.GetByQuoteRowID(ctx, "q_r_v1")
		}},
	} {
		got, err := tc.read(deadCtx)
		if err == nil {
			t.Errorf("%s 在库故障时回了 nil 错误（got=%v）：故障被读成\"没有这张单\"，派生路径会据此再开一张", tc.name, got)
			continue
		}
		if got != nil {
			t.Errorf("%s 故障时既报错又带回行（got=%+v）：调用方就没法只按 err 分支了", tc.name, got)
		}
	}
}

// TestBillRepository_UpdateStatusIsCompareAndSet 跃迁三条路分开 + 写集合只有两列。
//
// "金额没被顺手改掉"这一臂是本用例的全部价值：如果 UpdateStatus 用的是 Save(整行)，
// 一次带旧金额的结构体就能把应收改回上一个数，而 AC② 的对账恰好读这一列。
func TestBillRepository_UpdateStatusIsCompareAndSet(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()
	seed := newBillRow("b_cas_1", "QT-C", "q_c_v1", "opp-c")
	if err := repo.Create(ctx, seed); err != nil {
		t.Fatalf("种子写入失败: %v", err)
	}
	if err := db.Model(&model.Bill{}).Where("id = ?", "b_cas_1").
		Update("updated_at", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatalf("基准时间戳位移失败: %v", err)
	}

	if err := repo.UpdateStatus(ctx, "b_cas_1", model.BillStatusOpen, model.BillStatusVoided); err != nil {
		t.Fatalf("起点正确的跃迁失败: %v", err)
	}
	// 过期起点（库里已是 voided）⇒ 冲突，且**没有**改成 partial。
	if err := repo.UpdateStatus(ctx, "b_cas_1", model.BillStatusOpen, model.BillStatusPartial); !errors.Is(err, ErrBillStatusConflict) {
		t.Errorf("过期起点报的不是 ErrBillStatusConflict：%v", err)
	}
	if err := repo.UpdateStatus(ctx, "b_nope", model.BillStatusOpen, model.BillStatusPaid); !errors.Is(err, ErrBillNotFound) {
		t.Errorf("行不存在报的不是 ErrBillNotFound：%v", err)
	}
	if err := repo.UpdateStatus(ctx, "b_cas_1", model.BillStatusVoided, model.BillStatusVoided); err == nil {
		t.Error("零位移跃迁返回了成功：调用方从此分不清\"跃迁完成\"和\"什么都没做\"")
	}

	var after model.Bill
	if err := db.First(&after, "id = ?", "b_cas_1").Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if after.Status != model.BillStatusVoided {
		t.Errorf("状态 %q，期望 voided", after.Status)
	}
	if after.Amount != 1890.50 {
		t.Errorf("跃迁把金额改成了 %v：写集合里不许有 amount（对账读的就是这一列）", after.Amount)
	}
	if after.UpdatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("updated_at 没被推进：跃迁不留时间痕迹，账龄读起来就没有第二种解释")
	}
	// 其余列逐字不动（来路两把键 + 币种 + 创建时间）。
	if after.QuoteID != "QT-C" || after.QuoteRowID != "q_c_v1" || after.OpportunityID != "opp-c" ||
		after.Currency != model.BillCurrencyDefault || !after.CreatedAt.Equal(time.Date(2026, 10, 12, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("跃迁动了别的列: %+v", after)
	}
}

// TestBillRepository_MethodSetIsExactlyTheDocumentedFive 接口形状即"账单不可改写"。
//
// 反射比方法名清单，多一个少一个都红：
//   - 多出 Save/Update/Delete 的那一天，"这一张应收金额从没变过"这句话就作废了；
//   - 少了 GetByQuoteRowID 的那一天，重复派生的幂等通路就没脚可走；
//   - 刻意**没有**任何 List/分页方法：账单的第一个批量读方是 T-P7-03 的逾期扫描，
//     那一条查询的形状（status IN (…) ∧ due_at）会决定要哪个索引、要不要游标。
//     在这里先建一个 ListByOpportunity，就是把一个还没有读方的形状钉成契约。
func TestBillRepository_MethodSetIsExactlyTheDocumentedFive(t *testing.T) {
	want := []string{"Available", "Create", "GetByID", "GetByQuoteRowID", "UpdateStatus"}
	st := reflect.TypeOf((*BillRepository)(nil)).Elem()
	if st.NumMethod() != len(want) {
		t.Fatalf("BillRepository 方法数 %d ≠ %d（清单见用例注释）", st.NumMethod(), len(want))
	}
	for _, name := range want {
		if _, ok := st.MethodByName(name); !ok {
			t.Errorf("方法 %s 不存在", name)
		}
	}
	for i := 0; i < st.NumMethod(); i++ {
		name := st.Method(i).Name
		for _, banned := range []string{"Save", "Delete", "Upsert", "List", "Find", "First", "All"} {
			if strings.HasPrefix(name, banned) {
				t.Errorf("接口里出现了 %s：账单是凭证表，改写与抹除都不在本卡的接口形状里", name)
			}
		}
	}
}

// TestBillRepository_NilHandleFailsLoudly 半装配的失败面：Available 为假、每笔读写明确报错。
func TestBillRepository_NilHandleFailsLoudly(t *testing.T) {
	repo := NewBillRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄报告 Available()=true")
	}
	ctx := context.Background()
	if err := repo.Create(ctx, newBillRow("b_nil", "QT-N", "q_n_v1", "opp-n")); err == nil {
		t.Error("nil 句柄下的 Create 竟然成功")
	}
	if _, err := repo.GetByID(ctx, "b_nil"); err == nil {
		t.Error("nil 句柄下的 GetByID 返回了 nil error（读方会把\"没句柄\"读成\"没有这张单\"）")
	}
	if _, err := repo.GetByQuoteRowID(ctx, "q_n_v1"); err == nil {
		t.Error("nil 句柄下的 GetByQuoteRowID 返回了 nil error")
	}
	if err := repo.UpdateStatus(ctx, "b_nil", model.BillStatusOpen, model.BillStatusPaid); err == nil {
		t.Error("nil 句柄下的 UpdateStatus 竟然成功")
	}
}

// TestBillRepository_ConstraintNameIsTheOneOnTheModel 约束名与模型标签同源。
//
// 这一条是"防漂"而不是"防错"：uq_bills_quote_row 这个字面值同时出现在
// model 的标签、仓储的 23505 判据与 internal/pkg/db 的真库用例里，
// 三处写歪任何一处，症状都是"重复派生被报成一次普通写入失败"（红在真库用例之外）。
func TestBillRepository_ConstraintNameIsTheOneOnTheModel(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if err := db.AutoMigrate(&model.Bill{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	var names []string
	if err := db.Raw(`SELECT i.relname FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = ANY(x.indkey)
		WHERE x.indrelid = 'bills'::regclass AND x.indisunique AND a.attname = 'quote_row_id'`).
		Scan(&names).Error; err != nil {
		t.Fatalf("查唯一约束名失败: %v", err)
	}
	if len(names) != 1 || names[0] != billQuoteRowConstraint {
		t.Errorf("quote_row_id 上的唯一索引名 %v，与仓储判据 %q 不同源：23505 那一步会认不出幂等键",
			names, billQuoteRowConstraint)
	}
}

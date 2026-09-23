// bill_overdue_scan_test.go T-P7-03：逾期扫描查询的判据。
//
// 这条查询是"谁该被催"这件事在库里的**唯一**表达，所以本用例的四条断言各自挡的
// 都是一种"静默少催"：少一格状态、把 NULL 当成早就过期、封顶把最欠久的那批挤掉、
// 以及把"读失败"读成"没有逾期"。最后一条与 billOrNil 那条判据同源（T-P7-01 的 K38
// 就是为它补的刀），这里必须在新读口上重讲一遍：扫描查询返回空切片在语义上就是
// "今天没有人逾期"，它会直接写进日志与报表，而一次失败与零行在这里的后果完全不同。
package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// newOverdueBillRow 造一张可催收的应收行：与 newBillRow 同一批身份列，
// 只多带 due_at 与 status（这两格正是本用例的被测对象，所以在此处逐条显式给）。
func newOverdueBillRow(t *testing.T, db *gorm.DB, id string, status string, dueAt *time.Time) {
	t.Helper()
	row := newBillRow(id, "QT-"+id, "qr-"+id, "opp-"+id)
	row.Status = status
	row.DueAt = dueAt
	if err := db.WithContext(context.Background()).Create(row).Error; err != nil {
		t.Fatalf("造 %s 失败：%v", id, err)
	}
}

func TestBillRepository_ScanOverdueSelectsByCutoffAndStatus(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()

	// 一把 cutoff，两侧各摆一行：晚于它的必须不被选中（比较发生在 SQL 侧）。
	cutoff := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	early := cutoff.Add(-72 * time.Hour)
	late := cutoff.Add(72 * time.Hour)

	newOverdueBillRow(t, db, "b_open_early", model.BillStatusOpen, &early)
	newOverdueBillRow(t, db, "b_partial_early", model.BillStatusPartial, &early)
	newOverdueBillRow(t, db, "b_open_late", model.BillStatusOpen, &late)
	newOverdueBillRow(t, db, "b_open_equal", model.BillStatusOpen, &cutoff)
	newOverdueBillRow(t, db, "b_paid_early", model.BillStatusPaid, &early)
	newOverdueBillRow(t, db, "b_voided_early", model.BillStatusVoided, &early)
	newOverdueBillRow(t, db, "b_open_nodate", model.BillStatusOpen, nil)

	res, err := repo.ScanOverdue(ctx, cutoff, 50)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	got := map[string]bool{}
	for _, b := range res.Overdue {
		got[b.ID] = true
	}
	for _, id := range []string{"b_open_early", "b_partial_early"} {
		if !got[id] {
			t.Errorf("%s 没被扫到：open/partial 且已过账期的两张都必须进集合", id)
		}
	}
	for _, id := range []string{"b_open_late", "b_open_equal", "b_paid_early", "b_voided_early", "b_open_nodate"} {
		if got[id] {
			t.Errorf("%s 被扫到了：不该进集合（晚于/等于 cutoff、已结清、已作废、账期未定四类各挡一种坏法）", id)
		}
	}
	// 账期未定的那一行**必须被数出来**（T-P7-01 在 DueAt 那格上许下的判据：
	// "没有逾期"与"没法定逾期"是两件事，合成一件的那天催收就看不见这批单）。
	if res.Undated != 1 {
		t.Errorf("Undated = %d，期望 1（那张 due_at IS NULL 的可催收单要单独计数）", res.Undated)
	}
}

// TestBillRepository_ScanOverdueOrdersOldestFirst 顺序是契约而不是偏好：
// 一轮有封顶（防止某天积压时把整个进程的出站队列打满），被截掉的就只剩
// "最不急的那批"才有资格被砍 —— 反过来按 due_at 降序排，封顶就会精确地砍掉
// 欠得最久的那几张，而那正是该先催的人。
func TestBillRepository_ScanOverdueOrdersOldestFirst(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()
	cutoff := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	for i, id := range []string{"b_o_1", "b_o_2", "b_o_3"} {
		due := cutoff.Add(-time.Duration(24-i) * time.Hour)
		newOverdueBillRow(t, db, id, model.BillStatusOpen, &due)
	}
	res, err := repo.ScanOverdue(ctx, cutoff, 2)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(res.Overdue) != 2 {
		t.Fatalf("返回 %d 行，期望被 limit 截成 2", len(res.Overdue))
	}
	if res.Overdue[0].ID != "b_o_1" || res.Overdue[1].ID != "b_o_2" {
		t.Errorf("截断后留下的是 %s,%s，期望最欠久的 b_o_1,b_o_2（顺序错一天，封顶就会砍掉最该催的那批）",
			res.Overdue[0].ID, res.Overdue[1].ID)
	}
	// 到这里三行的 due_at 互不相同，二级排序键还没被走到（下面那一格才走）。
	// Truncated 必须说实话，否则调用方会把"这一轮只看到 2 张"读成"只有 2 张逾期"。
	if !res.Truncated {
		t.Error("Truncated=false：库里 3 张而封顶取了 2 张，这一轮的读数是不完整的")
	}
	full, err := repo.ScanOverdue(ctx, cutoff, 50)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(full.Overdue) != 3 || full.Truncated {
		t.Errorf("未触顶时 %d 行 / truncated=%v，期望 3 行 / false", len(full.Overdue), full.Truncated)
	}
	// 库里 3 张而封顶也是 3 张：这一格把 `len(rows) > limit` 与 `>=` 分开。
	// 只测"3 张 / 封顶 2"的话，把 > 写成 >= 也照杀不到（那一趟多取的那一行本来就越界），
	// 而 >= 的坏法是"每一轮都谎报还有没看完的"——运营会以为积压在涨。
	exact, err := repo.ScanOverdue(ctx, cutoff, 3)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(exact.Overdue) != 3 || exact.Truncated {
		t.Errorf("库里 3 张、封顶 3 时 %d 行 / truncated=%v，期望 3 行 / false",
			len(exact.Overdue), exact.Truncated)
	}

	// 二级排序键 `id ASC`：两张**同一时刻到期**的应收压在封顶线上。
	//
	// 为什么这一天会真的发生：同一张报价派生多张应收、或者批量导入按同一账期落行时，
	// due_at 是同一个值（同一秒写进去的），而封顶位只有一个是留给它们的。
	// 摘掉 `id ASC` 之后留下哪一张由存储顺序决定 ⇒ 同一批数据在两次运行/两个副本上
	// 催的人不一样，而两边都"正常"。测试里刻意**先插 id 大的那张**：只有真按 id 升序排，
	// 第 4 位才是 b_o_t_1；若退化成物理序，第 4 位会是 b_o_t_2，这一格就红。
	tie := cutoff.Add(-21 * time.Hour)
	newOverdueBillRow(t, db, "b_o_t_2", model.BillStatusOpen, &tie)
	newOverdueBillRow(t, db, "b_o_t_1", model.BillStatusOpen, &tie)
	tied, err := repo.ScanOverdue(ctx, cutoff, 4)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(tied.Overdue) != 4 || !tied.Truncated {
		t.Fatalf("5 张 / 封顶 4 时 %d 行 truncated=%v，期望 4 行 / true", len(tied.Overdue), tied.Truncated)
	}
	if got := tied.Overdue[3].ID; got != "b_o_t_1" {
		t.Errorf("封顶位上留下的是 %q，期望 b_o_t_1 ⇒ due_at 相同时没有稳定的第二排序键", got)
	}
	for i, want := range []string{"b_o_1", "b_o_2", "b_o_3"} {
		if tied.Overdue[i].ID != want {
			t.Errorf("第 %d 位是 %q，期望 %q（前缀顺序被同级行打乱）", i, tied.Overdue[i].ID, want)
		}
	}
}

// TestBillRepository_ScanOverdueRejectsNonPositiveLimit limit<=0 不能读成"不限"，
// 也不能读成"零行"：前者会让一次本应封顶的扫描变成全表捞，后者会让日志写下
// "今天没有人逾期"。两边都是错的，所以只能拒。
func TestBillRepository_ScanOverdueRejectsNonPositiveLimit(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	for _, limit := range []int{0, -1} {
		if _, err := repo.ScanOverdue(context.Background(), time.Now(), limit); err == nil {
			t.Errorf("limit=%d 竟然成功了：封顶值非法时必须出声而不是给一个读数", limit)
		}
	}
}

// TestBillRepository_ScanOverdueFailureIsNotEmptyResult 库故障（这里用已取消的 context
// 造）必须回 error 且**不回一份空结果**。判据与 T-P7-01 的 K38 完全同族，只是这一侧
// 更贵：空结果不只是一张单读不到，它会变成日志里那句"本轮扫到 0 张逾期"。
func TestBillRepository_ScanOverdueFailureIsNotEmptyResult(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := repo.ScanOverdue(cancelled, time.Now(), 10)
	if err == nil {
		t.Fatal("取消 context 后竟然回了一份结果")
	}
	if res != nil {
		t.Errorf("失败时仍带回结果对象 %+v：调用方会拿着它去数逾期张数", res)
	}
	// 同一条判据在空句柄上也要成立（装配半边失败时扫描必须拒绝而不是给"零逾期"）。
	empty := NewBillRepositoryWithDB(nil)
	if res, err := empty.ScanOverdue(context.Background(), time.Now(), 10); err == nil || res != nil {
		t.Errorf("空句柄：err=%v res=%v，期望报错且不给结果", err, res)
	}
}

// TestBillRepository_ScanOverdueFollowsTheChasedDomain 状态集合由 model 那一格决定，
// 不由本用例也不由 SQL 里抄来的字面量决定。
//
// 形状与 T-P7-01 的 K08 同一课（判据不许用例自己重算）：这里**不读源文件**，而是把
// `model.BillStatuses` 全值域各造一张同样逾期的单，再按 `BillChasedStatusKnown` 分边断言。
// 于是"有人往催收集里加了一格"或"从里面删掉一格"都会当场改变本用例的期望集合 ——
// 抄一份字面量的那种写法在这里必然对不上，而不需要任何人去 grep。
func TestBillRepository_ScanOverdueFollowsTheChasedDomain(t *testing.T) {
	db := setupBillTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	repo := NewBillRepositoryWithDB(db)
	ctx := context.Background()
	cutoff := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	due := cutoff.Add(-time.Hour)

	wantChased := 0
	for i, status := range model.BillStatuses {
		id := "b_dom_" + strconv.Itoa(i) + "_" + status
		newOverdueBillRow(t, db, id, status, &due)
		if model.BillChasedStatusKnown(status) {
			wantChased++
		}
	}
	res, err := repo.ScanOverdue(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	got := map[string]bool{}
	for _, b := range res.Overdue {
		got[b.ID] = true
	}
	if len(res.Overdue) != wantChased {
		t.Errorf("扫到 %d 张，按催收集应为 %d 张（两边必须同源于 model.BillStatusesChased）",
			len(res.Overdue), wantChased)
	}
	for i, status := range model.BillStatuses {
		id := "b_dom_" + strconv.Itoa(i) + "_" + status
		if model.BillChasedStatusKnown(status) != got[id] {
			t.Errorf("%s：在催收集里=%v 而被扫到=%v，两边不一致", status, model.BillChasedStatusKnown(status), got[id])
		}
	}
}

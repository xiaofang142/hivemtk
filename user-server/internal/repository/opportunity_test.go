// opportunity_test.go T-P4-02：商机仓储（表 opportunities）的并发写与读侧形状。
//
// 本文件只测**只有真库才能证明**的事：
//  1. AC①「并发更新同一商机有版本控制，后写不静默覆盖」—— CAS 的三种结果
//     （应用 / 版本过期 / 行不存在）各自可辨，且 8 协程只有一个赢家；
//  2. 写集合是一份白名单：身份列（id/code/customer_id/one_id/clue_id/created_at）
//     改不动，version 只能由 +1 走 —— 改得动就等于把一条商机挪到另一个客户名下、
//     或把版本号写成任意值从而使 CAS 失去意义；
//  3. 零值与 NULL 写得进去（GORM 默认跳过零值字段，"把金额改成 0"会静默失效）；
//  4. 三个查询维度的过滤、排序与分页边界；
//  5. AC② 铁律的反面证明：仓储**不做**值域校验（越界的 stage/status 原样落库），
//     因为跃迁与值域是 service（T-P4-03）的判据，落在这层等于把业务规则抄两份。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupOpportunityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.Opportunity{})
}

// newOpportunityRow 造一行商机。时间戳显式给出：分页与排序断言一半在 created_at 上，
// 依赖 GORM 的 autoCreateTime 会让"同一秒插入的五行谁在前"变成随机答案。
func newOpportunityRow(id, code, customerID, stage, status string) *model.Opportunity {
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	return &model.Opportunity{
		ID:          id,
		Code:        code,
		CustomerID:  customerID,
		Stage:       stage,
		Status:      status,
		Amount:      1000,
		Currency:    model.OpportunityCurrencyDefault,
		OwnerUserID: "sales_a",
		CreatedAt:   base,
		UpdatedAt:   base,
	}
}

func TestOpportunityRepo_NilHandle(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要**报错**，不是回空集合。商机表没有内存版底座：
	// "句柄没了 ⇒ 列表回空"会被上层读成"这个客户没有商机"，继而把在跑的商机重建一遍。
	if err := repo.Insert(ctx, newOpportunityRow("o1", "C1", "cus_1", model.OpportunityStageQualification, model.OpportunityStatusOpen)); err == nil {
		t.Error("Insert 应报错")
	}
	if _, err := repo.GetByID(ctx, "o1"); err == nil {
		t.Error("GetByID 应报错")
	}
	if err := repo.Update(ctx, newOpportunityRow("o1", "C1", "cus_1", model.OpportunityStageProposal, model.OpportunityStatusOpen)); err == nil {
		t.Error("Update 应报错")
	}
	for name, err := range map[string]error{
		"ListByCustomer": firstErr(repo.ListByCustomer(ctx, "cus_1", []string{model.OpportunityStatusOpen}, 10, 0)),
		"ListByStage":    firstErr(repo.ListByStage(ctx, model.OpportunityStageQualification, []string{model.OpportunityStatusOpen}, 10, 0)),
		"ListByOwner":    firstErr(repo.ListByOwner(ctx, "sales_a", []string{model.OpportunityStatusOpen}, 10, 0)),
	} {
		if err == nil {
			t.Errorf("%s 应报错", name)
		}
	}
}

func firstErr(_ []*model.Opportunity, err error) error { return err }

func TestOpportunityRepo_InsertAndReadBack(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	row := newOpportunityRow("opp_ins", "C-INS", "cus_ins", model.OpportunityStageNeedsConfirmed, model.OpportunityStatusOpen)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := repo.GetByID(ctx, "opp_ins")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("刚插入的行读不到")
	}
	if got.Code != "C-INS" || got.Stage != model.OpportunityStageNeedsConfirmed || got.Version != 0 {
		t.Errorf("读回形状不对：code=%q stage=%q version=%d", got.Code, got.Stage, got.Version)
	}
	// currency 靠 DB 默认值兜住：Insert 时显式写了 CNY，这里再验一次"不写也会落 CNY"。
	bare := newOpportunityRow("opp_bare", "C-BARE", "cus_ins", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	bare.Currency = ""
	if err := repo.Insert(ctx, bare); err != nil {
		t.Fatalf("Insert(空币种): %v", err)
	}
	back, err := repo.GetByID(ctx, "opp_bare")
	if err != nil || back == nil {
		t.Fatalf("读回空币种行: %v nil=%v", err, back == nil)
	}
	if back.Currency != model.OpportunityCurrencyDefault {
		t.Errorf("空币种没落到默认值：%q", back.Currency)
	}

	// 查不到 = (nil, nil)，读失败 = error。本卡能证明的是前者；后者由 nil 句柄用例覆盖。
	missing, err := repo.GetByID(ctx, "opp_none")
	if err != nil || missing != nil {
		t.Errorf("不存在的 ID 应回 (nil, nil)，得到 (%v, %v)", missing, err)
	}
}

func TestOpportunityRepo_InsertRejections(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()

	if err := repo.Insert(ctx, nil); err == nil {
		t.Error("Insert(nil) 应报错")
	}
	blank := newOpportunityRow("", "C-BLANK", "cus_x", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	if err := repo.Insert(ctx, blank); err == nil {
		t.Error("空主键应报错：ID 是被别的表抄走的键，空串会让行无法引用也无法删")
	}

	// 对外编号撞了必须报**可辨的**哨兵，而不是让调用方去匹配 PG 约束名。
	first := newOpportunityRow("opp_c1", "SAME-CODE", "cus_1", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	second := newOpportunityRow("opp_c2", "SAME-CODE", "cus_2", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("首次 Insert: %v", err)
	}
	if err := repo.Insert(ctx, second); !errors.Is(err, ErrOpportunityCodeConflict) {
		t.Errorf("重复编号应回 ErrOpportunityCodeConflict，得到 %v", err)
	}
	// 别的唯一冲突（主键）不得被读成"编号撞了"：service 对两者的反应完全不同
	// （前者改编号重试，后者是重复提交直接放弃）。
	if err := repo.Insert(ctx, newOpportunityRow("opp_c1", "OTHER-CODE", "cus_3", model.OpportunityStageQualification, model.OpportunityStatusOpen)); errors.Is(err, ErrOpportunityCodeConflict) {
		t.Errorf("主键冲突被误报成编号冲突: %v", err)
	} else if err == nil {
		t.Error("主键冲突应报错")
	}

	// 新建行的版本必须是 0：这一列的语义是"被成功改过几次"，
	// 出生即 v≥1 会让第一次 CAS 的期望值无定义（谁也无法说明它从哪一版改来）。
	born := newOpportunityRow("opp_v", "C-V", "cus_v", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	born.Version = 7
	if err := repo.Insert(ctx, born); err == nil {
		t.Error("Insert 一份 Version=7 的新行应报错")
	}
}

// TestOpportunityRepo_UpdateIsCAS AC① 的第一半：期望版本对了才写得进去，
// 而"后写不静默覆盖"这件事必须能被单独证明 —— 所以这里用同一行两次改写，
// 第二次拿的是**第一次改写之前**读到的那份。
func TestOpportunityRepo_UpdateIsCAS(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	if err := repo.Insert(ctx, newOpportunityRow("opp_cas", "C-CAS", "cus_cas", model.OpportunityStageQualification, model.OpportunityStatusOpen)); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	v0, err := repo.GetByID(ctx, "opp_cas")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	// 两个人同时从 v0 出发。
	stale := *v0

	v0.Stage = model.OpportunityStageProposal
	if err := repo.Update(ctx, v0); err != nil {
		t.Fatalf("第一次 Update 应成功: %v", err)
	}

	stale.Stage = model.OpportunityStageNegotiation
	stale.Amount = 999999
	if err := repo.Update(ctx, &stale); !errors.Is(err, ErrOpportunityStaleVersion) {
		t.Errorf("拿过期版本改写应回 ErrOpportunityStaleVersion，得到 %v", err)
	}

	after, err := repo.GetByID(ctx, "opp_cas")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	// 这一句就是 AC① 的全部内容：第二个人的金额**没有**进库。
	if after.Stage != model.OpportunityStageProposal || after.Amount != 1000 {
		t.Errorf("后写覆盖了先写：stage=%q amount=%v", after.Stage, after.Amount)
	}
	if after.Version != 1 {
		t.Errorf("version=%d，期望 1（成功改写一次 +1，被拒的那次不加）", after.Version)
	}

	// 用最新那份再改一次：应该成功，且版本继续单调前进。
	after.Stage = model.OpportunityStageNegotiation
	if err := repo.Update(ctx, after); err != nil {
		t.Fatalf("用最新版本改写应成功: %v", err)
	}
	third, err := repo.GetByID(ctx, "opp_cas")
	if err != nil || third.Version != 2 {
		t.Fatalf("第二次成功改写后 version 应为 2，得到 %+v err=%v", third, err)
	}
}

func TestOpportunityRepo_UpdateMissingRow(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	ghost := newOpportunityRow("opp_ghost", "C-GHOST", "cus_g", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	if err := repo.Update(ctx, ghost); !errors.Is(err, ErrOpportunityNotFound) {
		t.Errorf("改不存在的行应回 ErrOpportunityNotFound，得到 %v", err)
	}
	// 两种"没写成"必须分得开：409（别人先改了，重新读一次再来）与 404（这条没了，
	// 别再重试）在端点上是两个码，混成一个就只能要么无限重试要么谎报。
	if errors.Is(ErrOpportunityNotFound, ErrOpportunityStaleVersion) || errors.Is(ErrOpportunityStaleVersion, ErrOpportunityNotFound) {
		t.Error("两个哨兵错误不能互相包裹")
	}
}

// TestOpportunityRepo_ConcurrentUpdateSingleWinner AC① 的并发面：8 个协程拿同一份
// v0 各写自己的金额，只允许一个成功。
//
// 为什么不是"各写各的行"：那种测法测不到覆盖。这里所有协程抢的是**同一行**，
// 七个失败者是本卡的交付物而不是副作用。
func TestOpportunityRepo_ConcurrentUpdateSingleWinner(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	if err := repo.Insert(ctx, newOpportunityRow("opp_race", "C-RACE", "cus_race", model.OpportunityStageQualification, model.OpportunityStatusOpen)); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	base, err := repo.GetByID(ctx, "opp_race")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mine := *base
			mine.Amount = float64(100 + i)
			<-start
			errs[i] = repo.Update(ctx, &mine)
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	stale := 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, ErrOpportunityStaleVersion):
			stale++
		default:
			t.Errorf("出现预期外的错误: %v", e)
		}
	}
	if ok != 1 {
		t.Errorf("成功数 %d，期望恰好 1（%d 个写者把同一行各改了一遍）", ok, n)
	}
	if stale != n-1 {
		t.Errorf("过期数 %d，期望 %d", stale, n-1)
	}
	after, err := repo.GetByID(ctx, "opp_race")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if after.Version != 1 {
		t.Errorf("version=%d，期望 1：只允许赢的那一次加成 1", after.Version)
	}
	if after.Amount < 100 || after.Amount > 107 {
		t.Errorf("库里金额 %v 不是任何一个写者写过的值", after.Amount)
	}
}

// TestOpportunityRepo_UpdateCannotTouchIdentityColumns 写集合白名单的反面：
// 白名单之外的一切，即便调用方的 struct 里写了新值，也不许进 UPDATE 语句。
//
// 两条后果各对应一处真实破坏：
//   - 改 customer_id ⇒ 事件流里已抄走的副本还挂在老客户上，同一条商机在两个客户
//     名下各出现一次（按客户聚合的报表当场双计）；
//   - 把 version 写成任意值 ⇒ CAS 判据失效，下一次覆盖又变回静默。
func TestOpportunityRepo_UpdateCannotTouchIdentityColumns(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	if err := repo.Insert(ctx, newOpportunityRow("opp_id", "C-ID", "cus_orig", model.OpportunityStageQualification, model.OpportunityStatusOpen)); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	row, err := repo.GetByID(ctx, "opp_id")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	// 第一刀只伪造版本号：期望版本取自**调用方那份**，不是"读库里的当前值再写回"
	// （后者等于每次都比得上，CAS 就此变成常真）。
	faked := *row
	faked.Version = 999
	faked.Stage = model.OpportunityStageProposal
	if err := repo.Update(ctx, &faked); !errors.Is(err, ErrOpportunityStaleVersion) {
		t.Fatalf("带伪造版本号的改写应被拒成 StaleVersion，得到 %v", err)
	}

	// 第二刀把身份列一起改了（含主键）：改不动才是重点。
	row.ID = "opp_hijack"
	row.Code = "C-HIJACK"
	row.CustomerID = "cus_other"
	row.OneID = "phone:13800000000"
	row.ClueID = "clue-999"
	row.CreatedAt = row.CreatedAt.Add(-72 * time.Hour)
	row.Version = 999
	row.Stage = model.OpportunityStageProposal
	if err := repo.Update(ctx, row); !errors.Is(err, ErrOpportunityNotFound) {
		// 主键换了 ⇒ WHERE 打不到任何行，回 NotFound 是**正确**的：
		// 这条断言守的是"挪行走不通"，而不是"必须报 StaleVersion"。
		t.Fatalf("换主键的改写应被拒成 NotFound，得到 %v", err)
	}
	unchanged, err := repo.GetByID(ctx, "opp_id")
	if err != nil || unchanged == nil {
		t.Fatalf("行不该被挪走：(%v, %v)", unchanged, err)
	}
	if hijacked, err := repo.GetByID(ctx, "opp_hijack"); err != nil || hijacked != nil {
		t.Errorf("冒出了第二条主键：(%v, %v)（改写不能变成插入）", hijacked, err)
	}
	if unchanged.Code != "C-ID" || unchanged.CustomerID != "cus_orig" || unchanged.OneID != "" || unchanged.ClueID != "" {
		t.Errorf("身份列被改写了：code=%q customer=%q one=%q clue=%q",
			unchanged.Code, unchanged.CustomerID, unchanged.OneID, unchanged.ClueID)
	}
	if unchanged.Version != 0 {
		t.Errorf("version 被写成了 %d（只许 +1）", unchanged.Version)
	}
	if unchanged.Stage != model.OpportunityStageQualification {
		t.Errorf("被拒的改写把 stage 也带进去了：%q", unchanged.Stage)
	}
	if !unchanged.CreatedAt.Equal(row.CreatedAt.Add(72 * time.Hour)) {
		t.Errorf("created_at 被改动：入队时刻改了等于把商机挪到另一个时间窗")
	}

	// 反过来也要成立：白名单内的列改得动，否则这条用例只是在证明"Update 是空操作"。
	fresh, err := repo.GetByID(ctx, "opp_id")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	fresh.OwnerUserID = "sales_b"
	if err := repo.Update(ctx, fresh); err != nil {
		t.Fatalf("改负责人应成功: %v", err)
	}
	moved, err := repo.GetByID(ctx, "opp_id")
	if err != nil || moved.OwnerUserID != "sales_b" {
		t.Fatalf("负责人没改到：%+v err=%v", moved, err)
	}
}

// TestOpportunityRepo_UpdateWritesZeroAndNull 零值与 NULL 必须写得进去。
//
// GORM 用 struct 做 Updates 时**默认跳过零值字段**，于是"把金额改成 0"、
// "把预计关单日清空"会当场静默失效 —— 函数返回成功，库里一切照旧。
// 这类失效没有报错、没有日志，只有下一次读回来才发现。
func TestOpportunityRepo_UpdateWritesZeroAndNull(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	row := newOpportunityRow("opp_zero", "C-ZERO", "cus_zero", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	closeAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	row.ExpectedCloseAt = &closeAt
	row.WinProbability = 0.7
	// 输单原因先写一个非空值：它后面要被清空。若用例从"本来就是空串"出发，
	// "清空写不进 map"这种缺失会和"其实写进去了"给出同样的读后值，断言等于没断。
	row.LostReason = "客户预算撤回"
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	loaded, err := repo.GetByID(ctx, "opp_zero")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	loaded.Amount = 0
	loaded.WinProbability = 0
	loaded.ExpectedCloseAt = nil
	loaded.LostReason = ""
	// 币种留空：一次"没人碰币种"的改写不许把老行的 CNY 冲成空串（金额不带币种不可算）。
	loaded.Currency = ""
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, err := repo.GetByID(ctx, "opp_zero")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if after.Amount != 0 || after.WinProbability != 0 {
		t.Errorf("零值没写进去：amount=%v win=%v", after.Amount, after.WinProbability)
	}
	if after.ExpectedCloseAt != nil {
		t.Errorf("预计关单日没被清空：%v", *after.ExpectedCloseAt)
	}
	if after.Currency != model.OpportunityCurrencyDefault {
		t.Errorf("改写把币种冲成了 %q：白名单强制写这一列，兜底漏了就是静默错账", after.Currency)
	}
	if after.LostReason != "" {
		t.Errorf("输单原因没被清空：%q", after.LostReason)
	}

	// 兜底只在**空值**上生效：非空币种必须原样写回。把兜底写成"永远回默认币种"
	// 会让一次正常的改币种（客户换成美元结算）静默失败并返回成功。
	withCurrency, err := repo.GetByID(ctx, "opp_zero")
	if err != nil {
		t.Fatalf("GetByID(第二次): %v", err)
	}
	withCurrency.Currency = "USD"
	if err := repo.Update(ctx, withCurrency); err != nil {
		t.Fatalf("改币种应成功: %v", err)
	}
	inUSD, err := repo.GetByID(ctx, "opp_zero")
	if err != nil || inUSD == nil {
		t.Fatalf("读回改过币种的行: (%+v, %v)", inUSD, err)
	}
	if inUSD.Currency != "USD" {
		t.Errorf("币种改成 USD 没落库：%q", inUSD.Currency)
	}

	// 库侧再确认一次"是 NULL"而不是"零值时间"：两者在 P8 的逾期聚合里是两个答案。
	var isNull bool
	if err := repo.(*opportunityRepo).db.Raw(
		`SELECT expected_close_at IS NULL FROM opportunities WHERE id = ?`, "opp_zero").Scan(&isNull).Error; err != nil {
		t.Fatalf("查 NULL: %v", err)
	}
	if !isNull {
		t.Error("expected_close_at 不是 NULL：清空写成了写零值")
	}
}

// TestOpportunityRepo_Lists 三个查询维度（卡面：按客户/阶段/负责人）+ 排序分页边界。
func TestOpportunityRepo_Lists(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()

	stages := model.OpportunityStages
	base := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		row := newOpportunityRow(
			fmt.Sprintf("opp_l%d", i), fmt.Sprintf("C-L%d", i), "cus_list",
			stages[i%len(stages)], model.OpportunityStatusOpen)
		// 第 5 行给另一个负责人，另有一行属于别的客户。
		if i == 4 {
			row.OwnerUserID = "sales_other"
		}
		row.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}
	noise := newOpportunityRow("opp_noise", "C-NOISE", "cus_other_owner", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	noise.OwnerUserID = "sales_third"
	noise.CreatedAt = base.Add(10 * time.Hour)
	if err := repo.Insert(ctx, noise); err != nil {
		t.Fatalf("Insert noise: %v", err)
	}

	byCustomer, err := repo.ListByCustomer(ctx, "cus_list", model.OpportunityStatuses, 100, 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(byCustomer) != 5 {
		t.Fatalf("按客户应拿到 5 行，得到 %d", len(byCustomer))
	}
	// 倒序：列表页第一屏要看见最新的。
	if byCustomer[0].ID != "opp_l4" || byCustomer[4].ID != "opp_l0" {
		t.Errorf("排序不是 created_at 倒序：%s … %s", byCustomer[0].ID, byCustomer[4].ID)
	}
	// 分页：2+2+1 的并集必须等于全量、且不重不漏（偏移分页最常见的缺陷是末页重复）。
	seen := map[string]bool{}
	for page := 0; page < 3; page++ {
		rows, err := repo.ListByCustomer(ctx, "cus_list", model.OpportunityStatuses, 2, page*2)
		if err != nil {
			t.Fatalf("分页 %d: %v", page, err)
		}
		for _, r := range rows {
			if seen[r.ID] {
				t.Errorf("第 %d 页重复返回 %s", page, r.ID)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 5 {
		t.Errorf("三页并集 %d 行，期望 5", len(seen))
	}

	// 同一时刻建的两行（批量导入商机是真实路径）：排序必须有 id 兜底，否则
	// ORDER BY created_at DESC 下并列行的次序由扫描顺序决定，翻页会重复一行、漏一行。
	// 断言写成**精确次序**而不是只查"不重不漏"，理由是变异实测：摘掉 `id DESC` 后三行按
	// 插入顺序（a,b,c）返回，limit=1 翻三页**照样不重不漏**（并集计数那条断言仍绿），
	// 只有逐位比对把这份错序抓出来了 —— "漏行"要页间计划不一致才现形，次序判据当场就现形。
	const tieCustomer = "cus_tie"
	tieAt := base.Add(48 * time.Hour)
	for _, tag := range []string{"a", "b", "c"} {
		// 阶段与负责人刻意避开下面的断言口径（proposal / sales_tie 只在并列行这段里出现），
		// 免得"新加的三行"把按阶段、按负责人的计数解释成两处耦合的数。
		row := newOpportunityRow("opp_tie_"+tag, "C-TIE"+strings.ToUpper(tag), tieCustomer,
			model.OpportunityStageProposal, model.OpportunityStatusOpen)
		row.OwnerUserID = "sales_tie"
		row.CreatedAt = tieAt
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("Insert tie %s: %v", tag, err)
		}
	}
	wantTie := []string{"opp_tie_c", "opp_tie_b", "opp_tie_a"}
	all, err := repo.ListByCustomer(ctx, tieCustomer, model.OpportunityStatuses, 10, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("并列行应一次拿全 3 行，得到 %d err=%v", len(all), err)
	}
	for i, r := range all {
		if r.ID != wantTie[i] {
			t.Errorf("并列行第 %d 位是 %s，期望 %s（排序缺 id 兜底）", i, r.ID, wantTie[i])
		}
	}
	tieSeen := map[string]int{}
	for page := 0; page < 3; page++ {
		rows, err := repo.ListByCustomer(ctx, tieCustomer, model.OpportunityStatuses, 1, page)
		if err != nil || len(rows) != 1 {
			t.Fatalf("并列行第 %d 页应恰好 1 行，得到 %d err=%v", page, len(rows), err)
		}
		if rows[0].ID != wantTie[page] {
			t.Errorf("并列行第 %d 页给的是 %s，期望 %s：页间次序不唯一，翻页迟早重复或漏行", page, rows[0].ID, wantTie[page])
		}
		tieSeen[rows[0].ID]++
	}
	if len(tieSeen) != 3 {
		t.Errorf("三页并集只有 %d 行（应 3）", len(tieSeen))
	}

	byStage, err := repo.ListByStage(ctx, model.OpportunityStageQualification, model.OpportunityStatuses, 100, 0)
	if err != nil {
		t.Fatalf("ListByStage: %v", err)
	}
	for _, r := range byStage {
		if r.Stage != model.OpportunityStageQualification {
			t.Errorf("按阶段查拿回了别的阶段：%s/%q", r.ID, r.Stage)
		}
	}
	// 3 = l0 与 l4（stages 取模回到 qualification）+ noise（它也是 qualification，
	// 只是客户不同：这条用例按阶段筛，不看客户）。
	if len(byStage) != 3 {
		t.Errorf("qualification 应有 3 行，得到 %d", len(byStage))
	}

	byOwner, err := repo.ListByOwner(ctx, "sales_a", []string{model.OpportunityStatusOpen}, 100, 0)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(byOwner) != 4 { // l0..l3（l4 属于 sales_other）
		t.Errorf("sales_a 名下 open 商机应 4 行，得到 %d", len(byOwner))
	}
	// 显式状态集生效：换成一个没有人的状态 ⇒ 空集，而不是"忘了过滤"的全部。
	none, err := repo.ListByOwner(ctx, "sales_a", []string{model.OpportunityStatusWon}, 100, 0)
	if err != nil || len(none) != 0 {
		t.Errorf("按 won 过滤应为空，得到 %d 行 err=%v", len(none), err)
	}
	// statuses 传空切片必须**报错**而不是回全部：调用方想说的是"只看还在跑的"，
	// 一旦参数算错成空集，"回全部"会把已赢单的行混进待推进列表 —— 静默且难查。
	if _, err := repo.ListByOwner(ctx, "sales_a", nil, 100, 0); !errors.Is(err, ErrOpportunityStatusFilterEmpty) {
		t.Errorf("空状态集应回 ErrOpportunityStatusFilterEmpty，得到 %v", err)
	}
	if _, err := repo.ListByOwner(ctx, "sales_a", []string{}, 100, 0); !errors.Is(err, ErrOpportunityStatusFilterEmpty) {
		t.Errorf("长度为 0 的状态集也应报错，得到 %v", err)
	}
	// 空维度参数同样不接受：按 "" 客户查会拿到"所有 customer_id 为空的行"，
	// 而那在本表里是一个合法的、可诊断的形状（值域校验在 service），不是查询条件。
	if _, err := repo.ListByCustomer(ctx, "", nil, 100, 0); err == nil {
		t.Error("空 customerID 应报错")
	}
	// limit<=0 不接受：与"不限"混在一层会让上层少写一个判断就拖全表。
	if _, err := repo.ListByStage(ctx, model.OpportunityStageProposal, []string{model.OpportunityStatusOpen}, 0, 0); err == nil {
		t.Error("limit=0 应报错，而不是回全表")
	}
	// 负 offset 同样当场拒，且**判据是报错来自本层**：漏了这道守卫的话它会被拼进 SQL，
	// 报错来自 PG（`OFFSET must not be negative`）—— 那照样是 err != nil，只查"有没有报错"
	// 会绿，而调用方拿到的是一个听不懂的驱动错。变异实测见脚本 r38_mutate.sh 的 M11。
	if _, err := repo.ListByCustomer(ctx, "cus_list", model.OpportunityStatuses, 2, -1); err == nil ||
		!strings.Contains(err.Error(), "offset 不能为负") {
		t.Errorf("offset=-1 应由本层拒掉，得到 %v", err)
	}
}

// TestOpportunityRepo_DoesNotValidate 铁律 AC② 的反面证明：这层不做值域判断。
//
// 看着像"该修"的缺陷：越界的 stage/status、负金额照样落库。但**这正是要钉住的形状** ——
// 跃迁合法性、阈值、赢率算法都是 T-P4-03 的判据，如果这层也写一份，两份判据迟早分家，
// 而分家之后"哪一份说了算"取决于请求先撞上哪个方法（同 approval 侧把状态机写在
// service 一处的理由）。
//
// 也就是说：本用例红了的含义是"有人在仓储层加了业务校验"，修法是把校验搬去 service，
// 不是把用例改掉。
func TestOpportunityRepo_DoesNotValidate(t *testing.T) {
	repo := NewOpportunityRepositoryWithDB(setupOpportunityTestDB(t))
	ctx := context.Background()
	row := newOpportunityRow("opp_raw", "C-RAW", "cus_raw", "sideways_stage", "maybe")
	row.Amount = -5000
	row.WinProbability = 42
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("仓储层不该拦越界值，Insert 却报错了: %v", err)
	}
	back, err := repo.GetByID(ctx, "opp_raw")
	if err != nil || back == nil {
		t.Fatalf("读回: (%v, %v)", back, err)
	}
	if back.Stage != "sideways_stage" || back.Status != "maybe" {
		t.Errorf("值被改写了：stage=%q status=%q（期望原样）", back.Stage, back.Status)
	}
	if back.Amount != -5000 || back.WinProbability != 42 {
		t.Errorf("数值列被夹过：amount=%v win=%v", back.Amount, back.WinProbability)
	}
	if model.OpportunityStageIndex(back.Stage) != -1 {
		t.Error("用例前提变了：这个 stage 值成了合法阶段，本用例就不再证明\"不做校验\"")
	}
}

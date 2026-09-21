// quote_test.go T-P6-01：报价版本链仓储（表 quotes / quote_line_items）。
//
// 本文件只测**只有真库才能证明**的事，四条：
//  1. AC②「客户还价 → 新版本由系统生成而非覆盖」：版本号只能由 Append 从基准行
//     +1 走出来，调用方自己填版本号会被拒 —— 这一条松掉， LTC-12 的谈判过程就
//     重新变成"谁最后写谁赢"；
//  2. AC①「同一 quote_id 多版本共存、旧版不可变」：以同一基准并发追加，八个里只有
//     一个赢（库级复合唯一索引兜底，见 internal/pkg/db 那侧）；且本层**没有任何一个
//     方法**能改写已存在版本行的内容列 —— 不可变是接口形状的结果，不是靠约定；
//  3. 行项目的存在性守卫与批次原子性：明细指向不存在的版本行 ⇒ 报表上是一张空报价单，
//     半截批次 ⇒ 合计是一个**看起来合理**的错误数字；
//  4. AC② 铁律的反面证明：仓储不做值域与跃迁判断（那是 T-P6-02/03 的判据）。
//
// 本卡刻意**没有内存版底座**（同 T-P4-02 的商机表）：一旦有内存影子，
// "这一版已经被别人追加过了"就永远测不出来，而 AC①②要的都是它。
package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupQuoteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 两张表一起建：分两次建会漏掉"明细表没建、版本行建了"这种半状态，
	// 而那正是 AddLines 会报"表不存在"、上层读成"写不进去"的形状。
	return testutil.NewTestDB(t, &model.Quote{}, &model.QuoteLineItem{})
}

// newQuoteRow 造一个版本行。时间戳显式给出：updated_at 的"有没有被本次调用推进"
// 是本文件的判据之一，靠 autoUpdateTime 的话基准值本身就是随机的。
func newQuoteRow(id, quoteID, oppID, status string) *model.Quote {
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	return &model.Quote{
		ID:            id,
		QuoteID:       quoteID,
		OpportunityID: oppID,
		Status:        status,
		Currency:      model.QuoteCurrencyDefault,
		CreatedAt:     base,
		UpdatedAt:     base,
	}
}

func newQuoteLine(no int64, title string, qty, unit, amount float64) *model.QuoteLineItem {
	return &model.QuoteLineItem{
		LineNo: no, Title: title, ProductID: "p_" + title,
		Quantity: qty, UnitPrice: unit, Amount: amount,
	}
}

func TestQuoteRepo_NilHandle(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要**报错**，不是回空集合：报价表没有内存影子，
	// "句柄没了 ⇒ 版本链回空"会被上层读成"这张报价单还没有版本"，然后从 v1 重建一遍。
	if err := repo.Create(ctx, newQuoteRow("q1", "QT-1", "opp_1", model.QuoteStatusDraft)); err == nil {
		t.Error("Create 应报错")
	}
	if err := repo.Append(ctx, "q1", newQuoteRow("q2", "QT-1", "opp_1", model.QuoteStatusDraft)); err == nil {
		t.Error("Append 应报错")
	}
	if err := repo.UpdateStatus(ctx, "q1", model.QuoteStatusDraft, model.QuoteStatusSent); err == nil {
		t.Error("UpdateStatus 应报错")
	}
	if err := repo.AddLines(ctx, "q1", []*model.QuoteLineItem{newQuoteLine(1, "a", 1, 1, 1)}); err == nil {
		t.Error("AddLines 应报错")
	}
	for name, err := range map[string]error{
		"GetByID":      firstQuoteErr(repo.GetByID(ctx, "q1")),
		"GetVersion":   firstQuoteErr(repo.GetVersion(ctx, "QT-1", 1)),
		"Latest":       firstQuoteErr(repo.Latest(ctx, "QT-1")),
		"ListVersions": firstQuoteChainErr(repo.ListVersions(ctx, "QT-1")),
		"ListLines":    firstQuoteLineErr(repo.ListLines(ctx, "q1")),
	} {
		if err == nil {
			t.Errorf("%s 应报错", name)
		}
	}
}

func firstQuoteErr(_ *model.Quote, err error) error { return err }

func firstQuoteLineErr(_ []*model.QuoteLineItem, err error) error { return err }

func firstQuoteChainErr(_ []*model.Quote, err error) error { return err }

func TestQuoteRepo_CreateIsFirstVersion(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	row := newQuoteRow("q_c_1", "QT-C", "opp_c", model.QuoteStatusDraft)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 调用方留空版本号，落到第一版：本卡的"从 1 起"只许有一个出处。
	if row.Version != model.QuoteVersionFirst {
		t.Errorf("Create 应把 Version 补成 %d，实际 %d", model.QuoteVersionFirst, row.Version)
	}
	got, err := repo.GetByID(ctx, "q_c_1")
	if err != nil || got == nil {
		t.Fatalf("读回刚建的行: %v nil=%v", err, got == nil)
	}
	if got.QuoteID != "QT-C" || got.Status != model.QuoteStatusDraft || got.Version != model.QuoteVersionFirst {
		t.Errorf("读回形状不对：quote_id=%q status=%q version=%d", got.QuoteID, got.Status, got.Version)
	}
	if got.SourceID != "" {
		t.Errorf("第一版没有来路，source_id 应为空串，实际 %q", got.SourceID)
	}

	// 版本由 Append 走，不由 Create 表达：填了 >1 的版本号说明有人想绕过链。
	if err := repo.Create(ctx, &model.Quote{
		ID: "q_c_skip", QuoteID: "QT-SKIP", OpportunityID: "opp_c",
		Version: 7, Status: model.QuoteStatusDraft,
	}); err == nil {
		t.Error("Create 带着 version=7 竟然成功了：版本号只能由 Append 递增")
	}
	// 同一个 (quote_id, version) 第二次 Create 必须报"版本冲突"而不是裸的 23505：
	// service 对前者的反应是"这条链已经存在，别再建"，对后者只能整个失败。
	dup := newQuoteRow("q_c_1b", "QT-C", "opp_c", model.QuoteStatusDraft)
	if err := repo.Create(ctx, dup); !errors.Is(err, ErrQuoteVersionConflict) {
		t.Errorf("重复第一版应回 ErrQuoteVersionConflict，实际 %v", err)
	}
	// 查不到 = (nil, nil)，读失败 = error（后者由 nil 句柄用例覆盖）。
	missing, err := repo.GetByID(ctx, "q_c_none")
	if err != nil || missing != nil {
		t.Errorf("不存在的行 ID 应回 (nil, nil)，得到 (%v, %v)", missing, err)
	}
}

func TestQuoteRepo_CreateKeyGuards(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	cases := map[string]*model.Quote{
		"nil 行":      nil,
		"空行 ID":      newQuoteRow("", "QT-K", "opp_k", model.QuoteStatusDraft),
		"空 quote_id": newQuoteRow("q_k1", "", "opp_k", model.QuoteStatusDraft),
		"空商机":        newQuoteRow("q_k2", "QT-K", "", model.QuoteStatusDraft),
		"显式高版本号":     {ID: "q_k3", QuoteID: "QT-K", OpportunityID: "opp_k", Version: 3},
	}
	for name, row := range cases {
		if err := repo.Create(ctx, row); err == nil {
			t.Errorf("%s 竟然建成功了", name)
		}
	}
}

// TestQuoteRepo_AppendBuildsChain AC② 的正身：还价是**追加**，不是覆盖。
//
// 五臂各挡一种坏法：
//  1. 新版本号 = 基准行 +1，且 source_id 指向基准行 —— 链的"来自哪一版"必须由系统写下，
//     调用方填的版本号一律拒（那是 AC② 的正面：谁也不能自己声明"我是第三版"）。
//  2. 基准行原封不动（含它的行项目）——"旧版不可变"在本层的可执行形式。
//     若 Append 顺手把基准行的 status 改成"已被取代"，AC① 读到的就不是当时那一版了。
//  3. ListVersions 按版本升序：谈判过程要能按当时顺序重放。
//  4. Latest 取最大版本号而不是最新创建时间 —— 补录一版旧内容时两者会分家，
//     而"当前报给客户的是哪一版"只能有一个答案。
//  5. GetVersion 精确取某一版：这是"旧版不可变"唯一能被**读**出来的地方。
func TestQuoteRepo_AppendBuildsChain(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	v1 := newQuoteRow("qt_chain_v1", "QT-CHAIN", "opp_chain", model.QuoteStatusDraft)
	if err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("Create v1: %v", err)
	}
	if err := repo.AddLines(ctx, v1.ID, []*model.QuoteLineItem{
		newQuoteLine(1, "seat", 10, 199, 1990),
		newQuoteLine(2, "impl", 1, 5000, 5000),
	}); err != nil {
		t.Fatalf("AddLines v1: %v", err)
	}

	// ①追加，且调用方自作主张填了版本号 ⇒ 拒。
	bad := newQuoteRow("qt_chain_bad", "QT-CHAIN", "opp_chain", model.QuoteStatusDraft)
	bad.Version = 9
	if err := repo.Append(ctx, v1.ID, bad); !errors.Is(err, ErrQuoteVersionReserved) {
		t.Errorf("Append 时调用方自填版本号应回 ErrQuoteVersionReserved，实际 %v", err)
	}

	v2 := newQuoteRow("qt_chain_v2", "QT-CHAIN", "opp_chain", model.QuoteStatusDraft)
	if err := repo.Append(ctx, v1.ID, v2); err != nil {
		t.Fatalf("Append v2: %v", err)
	}
	if v2.Version != 2 || v2.SourceID != v1.ID {
		t.Errorf("v2 的链字段不对：version=%d source_id=%q（期望 2 / %q）", v2.Version, v2.SourceID, v1.ID)
	}
	v3 := newQuoteRow("qt_chain_v3", "QT-CHAIN", "opp_chain", model.QuoteStatusDraft)
	if err := repo.Append(ctx, v2.ID, v3); err != nil {
		t.Fatalf("Append v3: %v", err)
	}
	if v3.Version != 3 || v3.SourceID != v2.ID {
		t.Errorf("v3 的链字段不对：version=%d source_id=%q", v3.Version, v3.SourceID)
	}

	// ②基准行分毫未动。用独立零值 struct 读回（复用 v1 会把已填字段并进 WHERE）。
	backV1, err := repo.GetVersion(ctx, "QT-CHAIN", 1)
	if err != nil || backV1 == nil {
		t.Fatalf("读回 v1: %v nil=%v", err, backV1 == nil)
	}
	if backV1.Status != model.QuoteStatusDraft || backV1.SourceID != "" || !backV1.UpdatedAt.Equal(v1.UpdatedAt) {
		t.Errorf("追加新版本改动了基准行：status=%q source=%q updated=%v",
			backV1.Status, backV1.SourceID, backV1.UpdatedAt)
	}
	v1Lines, err := repo.ListLines(ctx, v1.ID)
	if err != nil {
		t.Fatalf("读回 v1 行项目: %v", err)
	}
	if len(v1Lines) != 2 {
		t.Errorf("v1 的行项目应当还是 2 条，实际 %d 条（Append 动了旧版）", len(v1Lines))
	}

	// ③升序重放。
	chain, err := repo.ListVersions(ctx, "QT-CHAIN")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	var versions []int64
	for _, q := range chain {
		versions = append(versions, q.Version)
	}
	if !reflect.DeepEqual(versions, []int64{1, 2, 3}) {
		t.Errorf("版本链应升序，实际 %v", versions)
	}

	// ④Latest：往回插一行"看起来最新创建、版本号最小"的行，Latest 仍须回 v3。
	// 这一臂证明选版凭的是版本号而不是时间。
	lateOld := newQuoteRow("qt_chain_v0_late", "QT-CHAIN", "opp_chain", model.QuoteStatusDraft)
	if err := repo.Create(ctx, lateOld); !errors.Is(err, ErrQuoteVersionConflict) {
		t.Errorf("补录第一版与已有 v1 撞号，应回 ErrQuoteVersionConflict，实际 %v", err)
	}
	latest, err := repo.Latest(ctx, "QT-CHAIN")
	if err != nil || latest == nil {
		t.Fatalf("Latest: %v nil=%v", err, latest == nil)
	}
	if latest.ID != "qt_chain_v3" {
		t.Errorf("Latest 回的是 %q，期望 qt_chain_v3", latest.ID)
	}

	// ⑤精确取某一版。
	only2, err := repo.GetVersion(ctx, "QT-CHAIN", 2)
	if err != nil || only2 == nil {
		t.Fatalf("GetVersion(2): %v nil=%v", err, only2 == nil)
	}
	if only2.ID != "qt_chain_v2" {
		t.Errorf("GetVersion(2) 回的是 %q", only2.ID)
	}
	none, err := repo.GetVersion(ctx, "QT-CHAIN", 99)
	if err != nil || none != nil {
		t.Errorf("不存在的版本应回 (nil, nil)，得到 (%v, %v)", none, err)
	}
}

// TestQuoteRepo_AppendRejectsCrossQuoteAndMissingBase 追加的两条越界。
//
// 两者若放行，坏法都是**静默**的：
//   - 基准行与目标行 quote_id 不同 ⇒ 一张报价单的第二版挂到另一张的链上，
//     两张单的版本历史各自都"看起来完整"（各自的链里少一格，但没人知道少了哪一格）；
//   - 基准行不存在 ⇒ Append 会退化成"凭空建一个 v1"，同一 quote_id 于是有两行 v1，
//     而唯一索引只在**列值相同**时拦得住，这里连号都没得上。
func TestQuoteRepo_AppendRejectsCrossQuoteAndMissingBase(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	a := newQuoteRow("qt_x_a1", "QT-X", "opp_x", model.QuoteStatusDraft)
	b := newQuoteRow("qt_x_b1", "QT-Y", "opp_x", model.QuoteStatusDraft)
	for _, q := range []*model.Quote{a, b} {
		if err := repo.Create(ctx, q); err != nil {
			t.Fatalf("Create %s: %v", q.ID, err)
		}
	}

	cross := newQuoteRow("qt_x_cross", "QT-Y", "opp_x", model.QuoteStatusDraft)
	if err := repo.Append(ctx, a.ID, cross); err == nil {
		t.Fatal("以 QT-X 的行为基准追加给 QT-Y 竟然成功了：两条链会互相吃掉版本号")
	}
	// 跨单追加必须**一行未落**。
	if got, err := repo.GetByID(ctx, "qt_x_cross"); err != nil || got != nil {
		t.Errorf("被拒的跨单追加留下了行：%+v err=%v", got, err)
	}

	orphan := newQuoteRow("qt_x_orphan", "QT-X", "opp_x", model.QuoteStatusDraft)
	if err := repo.Append(ctx, "qt_x_missing", orphan); !errors.Is(err, ErrQuoteNotFound) {
		t.Errorf("基准行不存在应回 ErrQuoteNotFound，实际 %v", err)
	}
	if got, err := repo.GetByID(ctx, "qt_x_orphan"); err != nil || got != nil {
		t.Errorf("被拒的追加留下了孤儿行（它带着一个没人认领的版本号）：%+v err=%v", got, err)
	}

	// 商机换了也不行：报价单不跟随商机改嫁（同 quote_id 的链必须同一下家）。
	switched := newQuoteRow("qt_x_switch", "QT-X", "opp_other", model.QuoteStatusDraft)
	if err := repo.Append(ctx, a.ID, switched); err == nil {
		t.Error("换商机的追加成功了：同一条链的商机归属应当恒定")
	}
}

// TestQuoteRepo_ConcurrentAppendSingleWinner AC① 在本层的形状。
//
// 八个写者从**同一个基准行**各追加一版：它们算出的目标版本号全是同一个（base+1），
// 所以裁决权在库级复合唯一索引，本层的职责只有一条 —— 把 23505 翻成
// ErrQuoteVersionConflict，而不是原样抛出一个没人能判断的驱动错误。
// 为什么不在本层加锁：那会把"谁赢了"变成进程内的运气，多实例部署时八个写者
// 分布在两个进程里，锁就没了；索引是哪都拦得住的那一层。
func TestQuoteRepo_ConcurrentAppendSingleWinner(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	base := newQuoteRow("qt_race_v1", "QT-RACE", "opp_race", model.QuoteStatusDraft)
	if err := repo.Create(ctx, base); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for k := 0; k < writers; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			next := newQuoteRow(
				fmt.Sprintf("qt_race_v2_%d", k), "QT-RACE", "opp_race", model.QuoteStatusDraft)
			errs[k] = repo.Append(ctx, base.ID, next)
		}(k)
	}
	wg.Wait()

	var won, conflicted, other int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrQuoteVersionConflict):
			conflicted++
		default:
			other++
			t.Logf("意外错误：%v", err)
		}
	}
	if won != 1 {
		t.Errorf("八个并发追加应当恰好一个成功，实际 %d 个", won)
	}
	if conflicted != writers-1 {
		t.Errorf("其余 %d 个应全部收到 ErrQuoteVersionConflict，实际收到 %d 个", writers-1, conflicted)
	}
	if other != 0 {
		t.Errorf("%d 个写者拿到了既不是成功也不是版本冲突的错误", other)
	}
	chain, err := repo.ListVersions(ctx, "QT-RACE")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(chain) != 2 {
		t.Errorf("链上应当只有 v1 与赢的那个 v2，实际 %d 行", len(chain))
	}
}

// TestQuoteRepo_UpdateStatusIsCAS 生命周期列的改写：唯一的一个写方法，写集合只有两列。
//
// 四臂：
//  1. from 匹配才生效，生效后 status 变、updated_at 变、**version 不变**
//     （version 标识的是内容那一版；改状态不是产生新报价，两者不能混）。
//  2. from 不匹配回 ErrQuoteStatusConflict 且什么都没改 —— 这是"客户已经接受了，
//     你却把它标成已发送"的防线。
//  3. 行不存在回 ErrQuoteNotFound：409 该重试、404 不该，混成一个调用方就分不开。
//  4. 两个并发写者从 draft 各自跃迁 ⇒ 恰好一个赢（CAS 的读-改-写间隙由同一个判据守）。
//
// 为什么这里用 CAS 而不是 Append 那种"库级唯一索引兜底"：状态列没有唯一性可依，
// 只能靠 `WHERE status = ?` 把"我以为它是什么"写进条件里。
func TestQuoteRepo_UpdateStatusIsCAS(t *testing.T) {
	db := setupQuoteTestDB(t)
	repo := NewQuoteRepositoryWithDB(db)
	ctx := context.Background()

	created := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	row := newQuoteRow("qt_st_1", "QT-ST", "opp_st", model.QuoteStatusDraft)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	until := created.AddDate(0, 0, 30)
	if err := db.Model(&model.Quote{}).Where("id = ?", row.ID).
		Updates(map[string]any{"valid_until": until, "currency": "USD"}).Error; err != nil {
		t.Fatalf("夹具写列失败: %v", err)
	}

	if err := repo.UpdateStatus(ctx, row.ID, model.QuoteStatusDraft, model.QuoteStatusSent); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("读回: %v nil=%v", err, got == nil)
	}
	if got.Status != model.QuoteStatusSent {
		t.Errorf("status 没改过来：%q", got.Status)
	}
	if !got.UpdatedAt.After(created) {
		t.Errorf("updated_at 没推进（%v）：它是「多久没动过」的唯一凭据", got.UpdatedAt)
	}
	if got.Version != model.QuoteVersionFirst {
		t.Errorf("改状态把 version 从 %d 改成了 %d", model.QuoteVersionFirst, got.Version)
	}
	// 白名单的正面：本方法只该动 status + updated_at，其余列一格都不该碰。
	if got.Currency != "USD" || got.ValidUntil == nil || !got.ValidUntil.Equal(until) {
		t.Errorf("UpdateStatus 碰了白名单之外的列：currency=%q valid_until=%v", got.Currency, got.ValidUntil)
	}

	// from 不匹配：已经 sent 的行，再拿 draft 去跃迁必须被拒。
	if err := repo.UpdateStatus(ctx, row.ID, model.QuoteStatusDraft, model.QuoteStatusAccepted); !errors.Is(err, ErrQuoteStatusConflict) {
		t.Errorf("过期跃迁应回 ErrQuoteStatusConflict，实际 %v", err)
	}
	if got, err := repo.GetByID(ctx, row.ID); err != nil || got.Status != model.QuoteStatusSent {
		t.Errorf("被拒的跃迁改了行：%+v err=%v", got, err)
	}
	if err := repo.UpdateStatus(ctx, "qt_st_none", model.QuoteStatusDraft, model.QuoteStatusSent); !errors.Is(err, ErrQuoteNotFound) {
		t.Errorf("不存在的行应回 ErrQuoteNotFound，实际 %v", err)
	}

	// 两个写者从同一 from 出发。
	race := newQuoteRow("qt_st_race", "QT-ST2", "opp_st", model.QuoteStatusDraft)
	if err := repo.Create(ctx, race); err != nil {
		t.Fatalf("Create race: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	tos := []string{model.QuoteStatusAccepted, model.QuoteStatusRejected}
	for k := 0; k < 2; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			errs[k] = repo.UpdateStatus(ctx, race.ID, model.QuoteStatusDraft, tos[k])
		}(k)
	}
	wg.Wait()
	won, conflicted := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrQuoteStatusConflict):
			conflicted++
		}
	}
	if won != 1 || conflicted != 1 {
		t.Errorf("并发跃迁应一赢一冲突，实际 赢=%d 冲突=%d", won, conflicted)
	}
	after, err := repo.GetByID(ctx, race.ID)
	if err != nil || after == nil {
		t.Fatalf("读回 race 行: %v", err)
	}
	if after.Status != model.QuoteStatusAccepted && after.Status != model.QuoteStatusRejected {
		t.Errorf("两个跃迁都没落地：%q", after.Status)
	}
}

func TestQuoteRepo_UpdateStatusInputGuards(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()
	if err := repo.UpdateStatus(ctx, "", model.QuoteStatusDraft, model.QuoteStatusSent); err == nil {
		t.Error("空行 ID 应报错：空串不是主键，它会把「查不到的那一行」当成目标")
	}
	if err := repo.UpdateStatus(ctx, "q", model.QuoteStatusDraft, model.QuoteStatusDraft); err == nil {
		t.Error("from == to 应报错：零位移跃迁返回成功，调用方就再也分不清「没人改」与「改成了同一个值」")
	}
}

// TestQuoteRepo_LinesGuards 行项目的两条守卫。
//
//  1. 存在性探针在写入**之前**：明细指向不存在的版本行时，它是一块永远读不到的孤儿，
//     而报价单在报表上是一张"没有行项目"的空单 —— 探针写在后面就等于先留下脏数据再报错。
//  2. 一批要么全进要么全不进：半截批次的合计是一个**看起来合理**的错误数字，
//     比一个明显的失败难查得多（P6-02 AC③ 要的正是"合计与落库一致"）。
//  3. 行项目上的 quote_row_id 由这层落，不信任调用方那一份；给了别的行 ID 直接拒 ——
//     静默写到另一版的名下，本用例第 2 臂的批次原子性就全白搭了。
func TestQuoteRepo_LinesGuards(t *testing.T) {
	db := setupQuoteTestDB(t)
	repo := NewQuoteRepositoryWithDB(db)
	ctx := context.Background()

	row := newQuoteRow("qt_ln_1", "QT-LN", "opp_ln", model.QuoteStatusDraft)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.AddLines(ctx, "qt_ln_missing", []*model.QuoteLineItem{newQuoteLine(1, "a", 1, 1, 1)}); !errors.Is(err, ErrQuoteNotFound) {
		t.Errorf("往不存在的版本行写明细应回 ErrQuoteNotFound，实际 %v", err)
	}
	var n int64
	if err := db.Model(&model.QuoteLineItem{}).Where("quote_row_id = ?", "qt_ln_missing").
		Count(&n).Error; err != nil {
		t.Fatalf("数孤儿行失败: %v", err)
	}
	if n != 0 {
		t.Errorf("存在性探针没起作用：%d 行明细指向了不存在的版本行", n)
	}

	// 空批次：一条都不写、且必须报错——静默成功会让上层以为"这份报价有 0 个行项目"。
	if err := repo.AddLines(ctx, row.ID, nil); err == nil {
		t.Error("AddLines 空批次应报错")
	}
	if lines, err := repo.ListLines(ctx, row.ID); err != nil || len(lines) != 0 {
		t.Errorf("空批次之后不该有行：n=%d err=%v", len(lines), err)
	}

	// 批内重复行号 ⇒ 整批回滚（第一行本身是合法的，它也不许留下）。
	if err := repo.AddLines(ctx, row.ID, []*model.QuoteLineItem{
		newQuoteLine(1, "good", 1, 100, 100),
		newQuoteLine(2, "bad", 1, 200, 200),
		newQuoteLine(2, "dupe", 1, 300, 300),
	}); err == nil {
		t.Fatal("批内重复行号竟然成功了")
	}
	lines, err := repo.ListLines(ctx, row.ID)
	if err != nil {
		t.Fatalf("ListLines: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("失败批次应当一条未落，实际落了 %d 条：半截批次的合计是个错数字", len(lines))
	}

	// 跨行 ID 的明细：调用方在 line 上写了别人的 ID，也必须整批拒。
	other := newQuoteRow("qt_ln_2", "QT-LN2", "opp_ln", model.QuoteStatusDraft)
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("Create other: %v", err)
	}
	cross := newQuoteLine(1, "cross", 1, 1, 1)
	cross.QuoteRowID = other.ID
	if err := repo.AddLines(ctx, row.ID, []*model.QuoteLineItem{cross}); err == nil {
		t.Error("明细自带的 quote_row_id 与目标行不符却写成功了")
	}
	if got, err := repo.ListLines(ctx, other.ID); err != nil || len(got) != 0 {
		t.Errorf("被拒的跨行明细落在了别的版本名下：n=%d err=%v", len(got), err)
	}

	// 行号必须为正：0 与负数会被前端"按行号定位"读成不存在或第一行。
	if err := repo.AddLines(ctx, row.ID, []*model.QuoteLineItem{newQuoteLine(0, "zero", 1, 1, 1)}); err == nil {
		t.Error("行号 0 写成功了")
	}

	// 正常批次落地，且**按行号升序**读回（合计与前端行序都靠它）。
	if err := repo.AddLines(ctx, row.ID, []*model.QuoteLineItem{
		newQuoteLine(3, "c", 1, 300, 300),
		newQuoteLine(1, "a", 1, 100, 100),
		newQuoteLine(2, "b", 1, 200, 200),
	}); err != nil {
		t.Fatalf("AddLines 正常批次: %v", err)
	}
	lines, err = repo.ListLines(ctx, row.ID)
	if err != nil {
		t.Fatalf("ListLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("期望 3 行，实际 %d", len(lines))
	}
	for k, want := range []int64{1, 2, 3} {
		if lines[k].LineNo != want {
			t.Errorf("第 %d 位的行号是 %d，期望 %d（不排序时计划器给什么读什么）", k, lines[k].LineNo, want)
		}
	}
	if lines[0].QuoteRowID != row.ID {
		t.Errorf("quote_row_id 没由这层落：%q", lines[0].QuoteRowID)
	}
	// 空 rowID 不是查询条件：它会命中「所有 rowID 为空的明细里的第一条」，
	// 而那是个看着完全合理的错误答案。
	if _, err := repo.ListLines(ctx, "  "); err == nil {
		t.Error("空 quote_row_id 应报错")
	}
}

// TestQuoteRepo_DoesNotValidate AC②（仓储无业务判断）的反面证明。
//
// 本用例红了意味着有人在这层加了校验，修法是把校验搬去 service，不是改掉用例。
// 判据不在洁癖：值域在 model（QuoteStatusKnown）、跃迁合法性与折扣阈值在
// T-P6-03/04 的 service —— 这里再写一份，两份迟早分家，
// 而分家之后"哪一份说了算"取决于请求先撞上哪个方法。
func TestQuoteRepo_DoesNotValidate(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()

	if model.QuoteStatusKnown("香蕉") {
		t.Fatal("夹具前提不成立：model 层已认得「香蕉」，本用例就测不到「仓储不校验」了")
	}
	weird := newQuoteRow("qt_nv_1", "QT-NV", "opp_nv", "香蕉")
	if err := repo.Create(ctx, weird); err != nil {
		t.Fatalf("仓储不该校验 status 值域，却拒了：%v", err)
	}
	nv2 := newQuoteRow("qt_nv_2", "QT-NV", "opp_nv", "已收回")
	if err := repo.Append(ctx, weird.ID, nv2); err != nil {
		t.Fatalf("仓储不该判断跃迁合法性，却拒了：%v", err)
	}
	if err := repo.AddLines(ctx, weird.ID, []*model.QuoteLineItem{
		newQuoteLine(1, "negative", -5, -1, -99999.99),
	}); err != nil {
		t.Fatalf("仓储不该看金额与数量的正负，却拒了：%v", err)
	}
	// 值域校验该在的地方确实存在（否则本用例是在证明"整仓都没人管"）。
	if err := repo.UpdateStatus(ctx, weird.ID, "香蕉", "西瓜"); err != nil {
		t.Errorf("from 用了未知状态也应原样跃迁（跃迁表不在本层）：%v", err)
	}
	back, err := repo.GetByID(ctx, weird.ID)
	if err != nil || back == nil {
		t.Fatalf("读回: %v", err)
	}
	if back.Status != "西瓜" {
		t.Errorf("status 应为西瓜，实际 %q", back.Status)
	}
}

// TestQuoteRepo_BlankKeyIsNotAQuery 三个按 quote_id 的读：空串一律报错，不回空集合。
//
// 判据不是洁癖：空串在这三处都能"合理地"返回 (nil, nil) / 空链，
// 于是一次漏传参数会被上层读成"这条报价链不存在"，继而从 v1 重建一遍。
// 与 ListLines 那条（在行项目用例里）同源，四把钥匙一起钉。
func TestQuoteRepo_BlankKeyIsNotAQuery(t *testing.T) {
	repo := NewQuoteRepositoryWithDB(setupQuoteTestDB(t))
	ctx := context.Background()
	for name, err := range map[string]error{
		"GetVersion":   firstQuoteErr(repo.GetVersion(ctx, " ", 1)),
		"Latest":       firstQuoteErr(repo.Latest(ctx, "")),
		"ListVersions": firstQuoteChainErr(repo.ListVersions(ctx, "")),
		"ListLines":    firstQuoteLineErr(repo.ListLines(ctx, "")),
	} {
		if err == nil {
			t.Errorf("%s 收到空键却返回了结果：漏传参数会被读成「这条链不存在」", name)
		}
	}
}

// TestQuoteRepo_InterfaceHasNoContentRewriter 「旧版不可变」的接口层证明。
//
// 判据取方法名而不是列清单：本层唯一的写方法是 UpdateStatus，它的写集合只有
// status + updated_at（由上面的白名单臂钉住）。一旦有人加一个能改内容列的方法
// （Save / SetLines / ReplaceLines / UpdateAmount / Delete……），AC① 的"旧版不可变"
// 就不是形状的结果而变成约定了 —— 这条用例红的时候，正确反应是改回去、
// 或者把它和那张卡一起想清楚，不是把期望名单加长。
func TestQuoteRepo_InterfaceHasNoContentRewriter(t *testing.T) {
	it := reflect.TypeOf((*QuoteRepository)(nil)).Elem()
	var names []string
	for k := 0; k < it.NumMethod(); k++ {
		names = append(names, it.Method(k).Name)
	}
	for _, n := range names {
		if strings.HasPrefix(n, "Delete") || strings.HasPrefix(n, "Remove") ||
			strings.HasPrefix(n, "Save") || strings.HasPrefix(n, "Patch") ||
			strings.HasPrefix(n, "Set") || strings.HasPrefix(n, "Replace") {
			t.Errorf("接口出现了 %s：版本行的内容与身份列一律不可改写，作废只能靠追加与状态", n)
		}
		if strings.HasPrefix(n, "Update") && n != "UpdateStatus" {
			t.Errorf("接口出现了 %s：本层唯一的 Update 是 UpdateStatus（写集合只有 status + updated_at）", n)
		}
	}
}

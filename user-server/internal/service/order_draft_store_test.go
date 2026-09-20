// order_draft_store_test.go T-P2-01：两副草稿底座（内存 / DB）的等价性与 durable 语义。
//
// 为什么"等价性"要单独钉：service 只依赖 draftStore，两条实现一旦判据漂移
// （排序、到期边界、pending 优先、清理集合），表现就是"接了 DB 之后销售工作台的
// 列表顺序变了"这种没人会归因到底座的缺陷。这里用**同一份观测序列**跑两副底座，
// 逐行比对输出，而不是各写一套断言再"看起来一样"。
//
// 已知且刻意保留的两处差异单独列成测试（TestOrderDraftStore_KnownDivergences），
// 差异登记在案比伪装没有差异更安全。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// scenarioNow 场景里所有"当前时间"的唯一来源，取"进程启动那一刻"而不是某个历史时刻。
//
// 两副底座必须用**同一个** now 去过期/改动，否则 expireOverdue 写进去的 updated_at
// 只差几纳秒也会被比对成不等 —— 那种差异是测试自己的噪声，不是被测行为；截到秒同理。
// 而这个 now 绝不能写死：夹具里满是 expires_at = scenarioNow+24h，写死的话 24 小时
// 之后整批用例集体过期（2026-09-20 实测 8 个并发 Confirm 全判"草稿已过期"、pending
// 归零），用例从"被测行为红"退化成"日历红"，且此后每天必红。
var scenarioNow = time.Now().UTC().Truncate(time.Second)

func seedDraft(id, customer, owner, product string, status DraftStatus, conf, amount float64, createdAt, expiresAt time.Time) *OrderDraft {
	return &OrderDraft{
		ID:          id,
		CustomerID:  customer,
		OwnerID:     owner,
		ProductName: product,
		Quantity:    1,
		UnitPrice:   amount,
		TotalAmount: amount,
		Confidence:  conf,
		Source:      "ai_chat",
		Status:      status,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
		ExpiresAt:   expiresAt,
		Metadata:    map[string]any{"seed": id},
	}
}

// seedStoreScenario 往一副空底座里写入固定六条草稿（覆盖 4 种状态 × 2 个销售 × 3 个客户，
// 其中两条 created_at 完全相同，用来逼出排序的 id 兜底位）。
func seedStoreScenario(t *testing.T, st draftStore) {
	t.Helper()
	ctx := context.Background()
	base := scenarioNow.Add(-5 * time.Hour)
	rows := []*OrderDraft{
		seedDraft("s-high-big", "cust-a", "sales-a", "热玛吉", DraftStatusPending, 0.9, 5000, base, scenarioNow.Add(48*time.Hour)),
		seedDraft("s-high-small", "cust-a", "sales-a", "光子嫩肤", DraftStatusPending, 0.9, 1000, base, scenarioNow.Add(24*time.Hour)),
		// 与 s-low 同一创建时间 ⇒ 只有 id 兜底能让两条路给出同一顺序
		seedDraft("s-low", "cust-b", "sales-b", "水光针", DraftStatusPending, 0.5, 9999, base.Add(time.Hour), scenarioNow.Add(72*time.Hour)),
		seedDraft("s-tie", "cust-b", "sales-b", "脱毛套餐", DraftStatusPending, 0.5, 9999, base.Add(time.Hour), scenarioNow.Add(72*time.Hour)),
		seedDraft("s-expired", "cust-c", "sales-a", "小气泡", DraftStatusExpired, 0.7, 300, base.Add(2*time.Hour), scenarioNow.Add(-time.Hour)),
		seedDraft("s-cancelled", "cust-c", "sales-a", "刷酸", DraftStatusCancelled, 0.6, 200, base.Add(3*time.Hour), scenarioNow.Add(-time.Hour)),
	}
	for _, r := range rows {
		if err := st.put(ctx, r); err != nil {
			t.Fatalf("seed %s 失败：%v", r.ID, err)
		}
	}
}

// observeStore 对一副已播种底座跑一遍固定操作序列，把可观察结果压成字符串切片。
//
// 顺序即契约：任何一步在两副底座上给出不同字符串，本函数的比对就会指出来。
func observeStore(t *testing.T, st draftStore) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	log := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	ids := func(list []*OrderDraft) string {
		parts := make([]string, 0, len(list))
		for _, d := range list {
			parts = append(parts, d.ID)
		}
		return strings.Join(parts, "|")
	}

	pending, err := st.listPending(ctx, "", scenarioNow, 0)
	log("listPending.all=%s err=%v", ids(pending), err)
	byOwner, err := st.listPending(ctx, "sales-b", scenarioNow, 1)
	log("listPending.ownerB.limit1=%s err=%v", ids(byOwner), err)
	// 到期边界：now 恰好等于 s-high-small 的 expires_at 前 1 秒 / 后 1 秒
	soon, err := st.listPending(ctx, "", scenarioNow.Add(time.Hour), 0)
	log("listPending.later=%s err=%v", ids(soon), err)

	cust, err := st.listByCustomer(ctx, "cust-a")
	log("byCustomer.a=%s err=%v", ids(cust), err)
	own, err := st.listByOwner(ctx, "sales-a")
	log("byOwner.a=%s err=%v", ids(own), err)
	pbc, err := st.pendingByCustomer(ctx, "cust-b")
	log("pendingByCustomer.b=%s err=%v", ids(pbc), err)

	got, err := st.get(ctx, "s-high-big")
	log("get.hit=%v status=%s meta=%v err=%v", got != nil, got.Status, got.Metadata["seed"], err)
	miss, err := st.get(ctx, "查无此稿")
	log("get.miss=%v err=%v", miss == nil, err)

	// mutateIfPending：pending 生效、终态落败、不存在落败
	_, applied, err := st.mutateIfPending(ctx, "s-high-big", func(d *OrderDraft) {
		d.Quantity = 7
		d.TotalAmount = 35000
		d.UpdatedAt = scenarioNow
	})
	log("mutate.pending=%v err=%v", applied, err)
	_, applied, err = st.mutateIfPending(ctx, "s-expired", func(d *OrderDraft) { d.Quantity = 99 })
	log("mutate.terminal=%v err=%v", applied, err)
	_, applied, err = st.mutateIfPending(ctx, "不存在的草稿", func(d *OrderDraft) { d.Quantity = 99 })
	log("mutate.missing=%v err=%v", applied, err)
	after, _ := st.get(ctx, "s-high-big")
	log("mutate.readback=%d/%.0f", after.Quantity, after.TotalAmount)

	// 再建一条已到期的 pending，让过期扫描有活干
	if err := st.put(ctx, seedDraft("s-due", "cust-d", "sales-b", "点阵激光", DraftStatusPending, 0.8, 800,
		scenarioNow.Add(-time.Hour), scenarioNow.Add(-time.Minute))); err != nil {
		t.Fatalf("seed s-due 失败：%v", err)
	}
	var evicted []string
	n, err := st.expireOverdue(ctx, scenarioNow, func(d *OrderDraft) { evicted = append(evicted, d.ID) })
	log("expire.count=%d err=%v", n, err)
	log("expire.events=%s", strings.Join(sortedStrings(evicted), "|"))
	// 幂等：第二次必须是 0 条（不翻状态的实现会反复数同一条）
	n2, err := st.expireOverdue(ctx, scenarioNow, nil)
	log("expire.again=%d err=%v", n2, err)

	counts, err := st.statusCounts(ctx)
	logPairs := make([]string, 0, len(counts))
	for k, v := range counts {
		logPairs = append(logPairs, fmt.Sprintf("%s=%d", k, v))
	}
	log("counts=[%s] err=%v", strings.Join(sortedStrings(logPairs), ","), err)

	// 清理：只应带走终态且足够老的行（s-cancelled / s-expired；s-due 刚被翻成 expired 但 updated_at=now）
	deleted, err := st.purgeTerminal(ctx, scenarioNow.Add(-time.Hour))
	log("purge=%d err=%v", deleted, err)
	counts, err = st.statusCounts(ctx)
	logPairs = logPairs[:0]
	for k, v := range counts {
		logPairs = append(logPairs, fmt.Sprintf("%s=%d", k, v))
	}
	log("afterPurge=[%s] err=%v", strings.Join(sortedStrings(logPairs), ","), err)
	after2, err := st.get(ctx, "s-cancelled")
	log("purge.readback=%v err=%v", after2 == nil, err)
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestOrderDraftStore_Parity(t *testing.T) {
	mem := newMemoryDraftStore()
	seedStoreScenario(t, mem)
	memObs := observeStore(t, mem)

	database := testutil.NewTestDB(t, &model.OrderDraft{})
	durable := newDBDraftStore(repository.NewOrderDraftRepositoryWithDB(database))
	seedStoreScenario(t, durable)
	dbObs := observeStore(t, durable)

	if len(memObs) != len(dbObs) {
		t.Fatalf("两副底座的观测步数都不同（%d vs %d），比对无意义", len(memObs), len(dbObs))
	}
	for i := range memObs {
		if memObs[i] != dbObs[i] {
			t.Errorf("第 %d 步不等价：\n  内存=%s\n  DB  =%s", i, memObs[i], dbObs[i])
		}
	}
	// 空跑防护：如果两副底座都返回了空观测（比如测试库被跳过），上面的循环会静默通过。
	if len(memObs) < 15 {
		t.Fatalf("观测步数异常少（%d），比对形同虚设", len(memObs))
	}
}

// KnownDivergences 两处**刻意保留**的差异，写成测试而不是注释：
// 谁哪天"顺手统一一下"，这两条会红，那时再判断该不该统一。
func TestOrderDraftStore_KnownDivergences(t *testing.T) {
	ctx := context.Background()
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	mem := newMemoryDraftStore()
	durable := newDBDraftStore(repository.NewOrderDraftRepositoryWithDB(database))

	// 差异 1：返回对象是否为存储里的那一份。
	// 内存副保留别名（25 个存量用例依赖 `draft.ExpiresAt = ...` 之后直接调 ExpireOverdue），
	// DB 副必然是副本 —— 所以"durable 模式下改返回对象再断言列表"是不成立的写法。
	for _, st := range []draftStore{mem, durable} {
		original := seedDraft("div-1", "cust-div", "sales-div", "产品", DraftStatusPending, 0.9, 100,
			scenarioNow.Add(-time.Hour), scenarioNow.Add(time.Hour))
		if err := st.put(ctx, original); err != nil {
			t.Fatalf("put 失败：%v", err)
		}
		back, err := st.get(ctx, "div-1")
		if err != nil {
			t.Fatalf("get 失败：%v", err)
		}
		back.Quantity = 42
		reread, err := st.get(ctx, "div-1")
		if err != nil {
			t.Fatalf("重读失败：%v", err)
		}
		isMem := st == draftStore(mem)
		if isMem && reread.Quantity != 42 {
			t.Errorf("内存副必须保持指针别名语义，实际 %d", reread.Quantity)
		}
		if !isMem && reread.Quantity == 42 {
			t.Error("DB 副不该把'改副本'当成已保存，否则等于把内存语义伪装成持久化")
		}
	}

	// 差异 2：metadata 为 nil 时，DB 副回读成空 map（jsonb 列不该回 nil）。
	nilMeta := seedDraft("div-2", "cust-div2", "sales-div2", "产品", DraftStatusPending, 0.9, 100,
		scenarioNow, scenarioNow.Add(time.Hour))
	nilMeta.Metadata = nil
	if err := durable.put(ctx, nilMeta); err != nil {
		t.Fatalf("put 失败：%v", err)
	}
	got, err := durable.get(ctx, "div-2")
	if err != nil || got == nil {
		t.Fatalf("get 失败：(%v,%v)", got, err)
	}
	if got.Metadata == nil {
		t.Error("DB 副的 metadata 应回非 nil map，让上层可以直接写入而不必每处判空")
	}
	if err := mem.put(ctx, nilMeta); err != nil {
		t.Fatalf("内存 put 失败：%v", err)
	}
	gotMem, err := mem.get(ctx, "div-2")
	if err != nil || gotMem == nil {
		t.Fatalf("内存 get 失败：(%v,%v)", gotMem, err)
	}
	if gotMem.Metadata != nil {
		t.Errorf("内存副保留调用方给的 nil（别名语义的一部分），实际 %+v", gotMem.Metadata)
	}
}

// ---------------------------------------------------------------- durable 侧 ----

func durableDraftService(t *testing.T, database *gorm.DB) *OrderDraftService {
	t.Helper()
	return NewOrderDraftServiceWithRepo(nil, repository.NewOrderDraftRepositoryWithDB(database))
}

// AC①：重启后草稿不丢。
//
// "重启"在这里的等价动作是**换一整个 service 实例**（新 store、新 mutex、新一切内存态），
// 只保留 DB 句柄 —— 因为进程重启真正丢掉的就是前者。
func TestOrderDraftService_Durable_RestartSurvival(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{}, &model.SalesEvent{})
	db.SetTestDB(database)
	ctx := context.Background()

	first := durableDraftService(t, database)
	if !first.Durable() {
		t.Fatal("durable 底座应报告 Durable()=true")
	}
	draft := first.CreateFromIntent(ctx, &OrderIntent{
		CustomerID:  "cust-restart",
		OneID:       "one-restart",
		ProductName: "光子嫩肤 3 次",
		Quantity:    3,
		UnitPrice:   760,
		Confidence:  0.8,
		RawText:     "我想做三次光子嫩肤",
	}, "sales-restart")
	if draft == nil {
		t.Fatal("CreateFromIntent 应返回草稿")
	}

	// "重启"
	second := durableDraftService(t, database)
	after := draftByID(t, ctx, second, draft.ID)
	if after == nil {
		t.Fatalf("重启后草稿 %s 应仍在", draft.ID)
	}
	if after.Quantity != 3 || after.TotalAmount != 2280 || after.ProductName != "光子嫩肤 3 次" {
		t.Errorf("重启后字段应原样，实际 %+v", after)
	}
	if after.Status != DraftStatusPending {
		t.Errorf("重启后状态应仍是 pending，实际 %s", after.Status)
	}
	list := draftPending(t, ctx, second, "sales-restart", 10)
	if len(list) != 1 || list[0].ID != draft.ID {
		t.Fatalf("重启后待确认列表应含该草稿，实际 %+v", list)
	}

	// 重启后仍能完成确认流程（成单链路依赖的不只是"读得到"，还有"能翻状态"）
	res, err := second.Confirm(ctx, draft.ID, "sales-restart")
	if err != nil {
		t.Fatalf("重启后 Confirm 失败：%v", err)
	}
	if res.OrderID == "" {
		t.Error("Confirm 应给出订单 ID")
	}
	confirmed, err := second.store.get(ctx, draft.ID)
	if err != nil || confirmed == nil {
		t.Fatalf("回读失败：(%v,%v)", confirmed, err)
	}
	if confirmed.Status != DraftStatusConfirmed {
		t.Errorf("确认后状态应落库为 confirmed，实际 %s", confirmed.Status)
	}
	if confirmed.OrderID != res.OrderID {
		t.Errorf("草稿应回写订单关联，实际 %q vs %q", confirmed.OrderID, res.OrderID)
	}
	if confirmed.ConfirmedAt == nil {
		t.Error("confirmed_at 应落库")
	}
}

// AC②：ExpireOverdue 真正落库，而不是只数一遍。
//
// 开工前的实现改的是内存 map 里的状态、返回一个计数；同一条草稿在下一轮会被再数一次。
// 这里用"第二轮必须是 0 + 库里状态确已翻转 + 事件只记一次"三条一起钉。
func TestOrderDraftService_Durable_ExpireOverdueLandsInDB(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{}, &model.SalesEvent{})
	db.SetTestDB(database)
	ctx := context.Background()

	stats := NewSalesEventStatsServiceWithRepo(repository.NewSalesEventRepositoryWithDB(database))
	svc := durableDraftService(t, database)
	svc.SetStats(ctx, stats)

	repo := repository.NewOrderDraftRepositoryWithDB(database)
	// ExpireOverdue 的判据用的是真实时钟（签名沿用存量，不接 now 参数），
	// 所以本用例的到期时间必须以 time.Now() 为基准，scenarioNow 在这里只会测出"没到期"。
	realNow := time.Now()
	for _, id := range []string{"due-1", "due-2"} {
		if err := svc.store.put(ctx, seedDraft(id, "cust-"+id, "sales-x", "产品"+id, DraftStatusPending,
			0.8, 500, realNow.Add(-8*24*time.Hour), realNow.Add(-time.Hour))); err != nil {
			t.Fatalf("seed %s 失败：%v", id, err)
		}
	}
	// 一条未到期的，用来证明扫描不是"把所有 pending 都刷一遍"
	if err := svc.store.put(ctx, seedDraft("fresh-1", "cust-fresh", "sales-x", "产品F", DraftStatusPending,
		0.8, 500, realNow.Add(-time.Hour), realNow.Add(24*time.Hour))); err != nil {
		t.Fatalf("seed fresh-1 失败：%v", err)
	}

	if n := draftExpireOverdue(t, ctx, svc); n != 2 {
		t.Fatalf("首轮期望过期 2 条，实际 %d", n)
	}
	for _, id := range []string{"due-1", "due-2"} {
		m, err := repo.GetByID(ctx, id)
		if err != nil || m == nil {
			t.Fatalf("回读 %s 失败：(%v,%v)", id, m, err)
		}
		if m.Status != model.OrderDraftStatusExpired {
			t.Errorf("%s 应已在库里翻成 expired，实际 %s", id, m.Status)
		}
	}
	fresh, err := repo.GetByID(ctx, "fresh-1")
	if err != nil || fresh == nil {
		t.Fatalf("回读 fresh-1 失败：(%v,%v)", fresh, err)
	}
	if fresh.Status != model.OrderDraftStatusPending {
		t.Errorf("未到期草稿不该被动过，实际 %s", fresh.Status)
	}

	if n := draftExpireOverdue(t, ctx, svc); n != 0 {
		t.Errorf("第二轮期望 0 条，实际 %d（说明上一轮没真落库）", n)
	}

	var expiredEvents int64
	if err := database.Model(&model.SalesEvent{}).
		Where("action = ? AND event_type = ?", "expired", model.SalesEventTypeOrderDraft).
		Count(&expiredEvents).Error; err != nil {
		t.Fatalf("统计事件行数失败：%v", err)
	}
	if expiredEvents != 2 {
		t.Errorf("expired 事件应各记一次（2 条），实际 %d", expiredEvents)
	}

	counts, err := svc.DraftStatusCounts(ctx)
	if err != nil {
		t.Fatalf("DraftStatusCounts 失败：%v", err)
	}
	if counts[string(DraftStatusExpired)] != 2 || counts[string(DraftStatusPending)] != 1 {
		t.Errorf("状态分布异常，实际 %+v", counts)
	}
}

// AC②的另一半：终态行不会无限堆积。
func TestOrderDraftService_Durable_PurgeTerminalBounds(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	ctx := context.Background()
	svc := durableDraftService(t, database)
	repo := repository.NewOrderDraftRepositoryWithDB(database)

	old := scenarioNow.Add(-200 * 24 * time.Hour)
	seed := func(id string, status DraftStatus, updatedAt time.Time) {
		d := seedDraft(id, "cust-"+id, "sales-p", "产品"+id, status, 0.8, 100, updatedAt.Add(-time.Hour), updatedAt)
		d.UpdatedAt = updatedAt
		if err := svc.store.put(ctx, d); err != nil {
			t.Fatalf("seed %s 失败：%v", id, err)
		}
	}
	seed("gone-expired", DraftStatusExpired, old)
	seed("gone-cancelled", DraftStatusCancelled, old)
	seed("kept-pending", DraftStatusPending, old)                        // 非终态：永不清理
	seed("kept-confirmed", DraftStatusConfirmed, old)                    // 成单证据链：永不清理
	seed("kept-recent", DraftStatusExpired, scenarioNow.Add(-time.Hour)) // 在保留期内

	if n := draftPurgeTerminal(t, ctx, svc, 90*24*time.Hour); n != 2 {
		t.Fatalf("期望清掉 2 行，实际 %d", n)
	}
	for id, wantGone := range map[string]bool{
		"gone-expired":   true,
		"gone-cancelled": true,
		"kept-pending":   false,
	} {
		m, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("回读 %s 失败：%v", id, err)
		}
		if wantGone && m != nil {
			t.Errorf("%s 应已被清理", id)
		}
		if !wantGone && m == nil {
			t.Errorf("%s 不该被清理", id)
		}
	}
	if n := draftPurgeTerminal(t, ctx, svc, 0); n != 0 {
		// 传 0 走默认保留期（90 天）：kept-recent 在期内、kept-pending/confirmed 非终态
		t.Errorf("第二轮期望 0 行（默认保留期），实际 %d", n)
	}
	counts, err := svc.DraftStatusCounts(ctx)
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	var total int64
	for _, v := range counts {
		total += v
	}
	if total != 3 {
		t.Errorf("清理后应剩 3 行，实际 %+v", counts)
	}
}

// 无 DB 句柄时必须"退回内存 + 出声"，而不是 panic 也不是假装持久化。
func TestOrderDraftService_NilDBFallsBackToMemory(t *testing.T) {
	svc := NewOrderDraftServiceWithDB(nil, nil)
	if svc.Durable() {
		t.Error("nil 句柄时 Durable 应为 false（否则端点会谎报）")
	}
	draft := svc.CreateFromIntent(context.Background(), &OrderIntent{
		CustomerID: "c", ProductName: "P", Quantity: 1, UnitPrice: 10, Confidence: 0.5,
	}, "owner")
	if draft == nil {
		t.Fatal("回退路径应仍可工作")
	}
	if got := draftByID(t, context.Background(), svc, draft.ID); got == nil {
		t.Error("回退到内存后仍应读得到")
	}
}

// Confirm 的并发对手只有一个：草稿状态翻转必须互斥。
//
// 这是本卡修掉的第二个真缺陷 —— 原实现"读判写"三步无锁，两个销售同时点确认会各建一张订单。
func TestOrderDraftService_Durable_ConfirmConcurrentSingleWinner(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{}, &model.SalesEvent{})
	db.SetTestDB(database)
	ctx := context.Background()
	svc := durableDraftService(t, database)

	d := seedDraft("race-1", "cust-race", "sales-race", "热玛吉", DraftStatusPending, 0.9, 8000,
		scenarioNow.Add(-time.Hour), scenarioNow.Add(24*time.Hour))
	if err := svc.store.put(ctx, d); err != nil {
		t.Fatalf("seed 失败：%v", err)
	}

	const racers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		orderID string
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := svc.Confirm(ctx, "race-1", "sales-race")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && res != nil:
				wins++
				if orderID == "" {
					orderID = res.OrderID
				}
			case err != nil && strings.Contains(err.Error(), "不可确认"):
				// 落败方的正确回答：状态已被别人改掉
			default:
				t.Errorf("意外的 Confirm 结果：res=%+v err=%v", res, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("8 个并发 Confirm 应恰好 1 人成功，实际 %d 人", wins)
	}
	if orderID == "" {
		t.Error("胜出方应拿到订单 ID")
	}
	counts, err := svc.DraftStatusCounts(ctx)
	if err != nil {
		t.Fatalf("失败：%v", err)
	}
	if counts[string(DraftStatusConfirmed)] != 1 {
		t.Errorf("confirmed 应恰为 1 行，实际 %+v", counts)
	}
}

// 跨进程重复建草稿：部分唯一索引报错后必须重查并按合并处理。
//
// 进程内跑不到这个分支（CreateFromIntent 全程持 mu，查—建是原子的），所以要一副
// 能复现"另一个副本恰好在两次调用之间插入"的假底座把它逼出来：
// 第一次去重查询故意看不见 → put 撞唯一索引 → 重查才看见。
// 不这么做的话，那段冲突合并代码就是永不执行的死代码，写它的人以为它有用。
type conflictOnceStore struct {
	draftStore
	mu          sync.Mutex
	blindLookup int // 前 N 次 pendingByCustomer 返回空（模拟对手进程尚未可见）
	fired       bool
	conflic     int
}

func (c *conflictOnceStore) pendingByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blindLookup > 0 {
		c.blindLookup--
		return nil, nil
	}
	return c.draftStore.pendingByCustomer(ctx, customerID)
}

func (c *conflictOnceStore) put(ctx context.Context, d *OrderDraft) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fired {
		c.fired = true
		c.conflic++
		return repository.ErrOrderDraftPendingConflict
	}
	return c.draftStore.put(ctx, d)
}

func TestOrderDraftService_PendingConflictMerges(t *testing.T) {
	ctx := context.Background()
	mem := newMemoryDraftStore()
	// 另一进程已建的同客户同产品 pending 草稿（数量 2）
	other := seedDraft("other-proc", "cust-x", "sales-x", "光子嫩肤", DraftStatusPending, 0.7, 800,
		scenarioNow.Add(-time.Hour), scenarioNow.Add(24*time.Hour))
	other.Quantity = 2
	other.TotalAmount = 1600
	other.UnitPrice = 800
	if err := mem.put(ctx, other); err != nil {
		t.Fatalf("seed 失败：%v", err)
	}
	fake := &conflictOnceStore{draftStore: mem, blindLookup: 1}
	svc := newOrderDraftService(nil, fake)

	got := svc.CreateFromIntent(ctx, &OrderIntent{
		CustomerID:  "cust-x",
		ProductName: "光子嫩肤",
		Quantity:    2,
		UnitPrice:   800,
		Confidence:  0.9,
		RawText:     "再来两次光子嫩肤",
	}, "sales-x")
	if got == nil {
		t.Fatal("冲突后应重查并合并，而不是丢弃本次意向")
	}
	if got.ID != "other-proc" {
		t.Errorf("应合并进已存在的那条，实际新建了 %s（会产生重复草稿）", got.ID)
	}
	if got.Quantity != 4 {
		t.Errorf("数量应累加为 2+2，实际 %d", got.Quantity)
	}
	if got.TotalAmount != 3200 {
		t.Errorf("总价应随之重算，实际 %.0f", got.TotalAmount)
	}
	if fake.conflic != 1 {
		t.Errorf("冲突分支应恰好走一次，实际 %d", fake.conflic)
	}
	if list := draftPending(t, ctx, svc, "", 10); len(list) != 1 {
		t.Errorf("全程只该有一条 pending 草稿，实际 %d 条", len(list))
	}
}

// 内存底座下"必冲突"的另一种结局：重查也查不到时，明确报错并丢弃本次意向。
//
// 这条测的是"宁可少一条草稿，也不给销售两条重复的"这个取舍是否真的成立。
type alwaysConflictStore struct {
	draftStore
}

func (a *alwaysConflictStore) put(context.Context, *OrderDraft) error {
	return repository.ErrOrderDraftPendingConflict
}

func TestOrderDraftService_ConflictWithoutCandidateDropsIntent(t *testing.T) {
	svc := newOrderDraftService(nil, &alwaysConflictStore{draftStore: newMemoryDraftStore()})
	got := svc.CreateFromIntent(context.Background(), &OrderIntent{
		CustomerID: "cust-y", ProductName: "无人认领的产品", Quantity: 1, UnitPrice: 10, Confidence: 0.5,
	}, "sales-y")
	if got != nil {
		t.Fatalf("重查无候选时应返回 nil，实际 %+v", got)
	}
	if list := draftPending(t, context.Background(), svc, "sales-y", 10); len(list) != 0 {
		t.Errorf("不应有任何草稿落地，实际 %+v", list)
	}
}

// 存储层真失败（非冲突）时必须回错，不能返回一份"没存下的草稿"。
type failingStore struct {
	draftStore
	err error
}

func (f *failingStore) put(context.Context, *OrderDraft) error { return f.err }

func TestOrderDraftService_StorePutFailureIsVisible(t *testing.T) {
	boom := errors.New("connection reset by peer")
	svc := newOrderDraftService(nil, &failingStore{draftStore: newMemoryDraftStore(), err: boom})

	if got := svc.CreateFromIntent(context.Background(), &OrderIntent{
		CustomerID: "c", ProductName: "P", Quantity: 1, UnitPrice: 10, Confidence: 0.5,
	}, "o"); got != nil {
		t.Errorf("落库失败时 CreateFromIntent 应返回 nil，实际 %+v", got)
	}
	draft, err := svc.CreateManual(context.Background(), &CreateDraftRequest{
		CustomerID: "c", OwnerID: "o", ProductName: "P", Quantity: 1, UnitPrice: 10,
	})
	if err == nil {
		t.Fatal("CreateManual 必须把存储错误回给调用方")
	}
	if !errors.Is(err, boom) {
		t.Errorf("错误应可被 errors.Is 追到底层原因，实际 %v", err)
	}
	if draft != nil {
		t.Errorf("回错时不该附带一份草稿，实际 %+v", draft)
	}
}

// ---------------------------------------------------- T-P2-06 ③：读失败可区分 ----

// readFailRepo 只把"读"这一段弄坏，写保持成功。
//
// 为什么必须只坏读：本卡 ③ 的病灶是"读库失败被吞成 nil/空列表"，写路径的失败
// 上面几个用例已经钉过（failingStore）。混在一起的话，"CreateManual 成功 + 读回失败"
// 这个最容易骗过人的组合就测不到 —— 而销售工作台正是这个形状。
type readFailRepo struct {
	repository.OrderDraftRepository
	err error
}

func (r *readFailRepo) Available() bool { return true }

func (r *readFailRepo) Upsert(context.Context, *model.OrderDraft) error { return nil }

func (r *readFailRepo) GetByID(context.Context, string) (*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) ListPendingByCustomer(context.Context, string) ([]*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) ListPending(context.Context, string, time.Time, int) ([]*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) ListByCustomer(context.Context, string) ([]*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) ListByOwner(context.Context, string) ([]*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) CountByStatus(context.Context) (map[string]int64, error) {
	return nil, r.err
}

func (r *readFailRepo) MutatePending(context.Context, string, func(*model.OrderDraft)) (bool, error) {
	return false, r.err
}

func (r *readFailRepo) ExpirePendingBatch(context.Context, time.Time, int) ([]*model.OrderDraft, error) {
	return nil, r.err
}

func (r *readFailRepo) PurgeTerminal(context.Context, time.Time, int) (int64, error) {
	return 0, r.err
}

// AC③：读不动 ≠ 没有。四种读一律把错误带到调用方，且**不附带**一个看起来像"空"的返回值。
//
// 改签名之前这些方法在失败时 logger 一句就回 nil/空，于是"库挂了"在调用方与
// "这个销售今天没有待确认草稿"逐字相同 —— 后者是一句业务结论，会被写进首页、
// 被读成"不用跟"。
func TestOrderDraftService_ReadFailureIsDistinctFromEmpty(t *testing.T) {
	boom := errors.New("connection refused")
	svc := NewOrderDraftServiceWithRepo(nil, &readFailRepo{err: boom})
	ctx := context.Background()

	draft, err := svc.GetByID(ctx, "draft_any")
	if draft != nil {
		t.Errorf("读失败时不该返回草稿对象，实际 %+v", draft)
	}
	if !errors.Is(err, boom) {
		t.Errorf("GetByID 必须把底层错误原样带回（可被 errors.Is 追到），实际 %v", err)
	}

	list, err := svc.ListPending(ctx, "sales_1", 10)
	if list != nil {
		t.Errorf("ListPending 失败时应回 nil 而非空切片（空切片会被 len()==0 读成「没有草稿」），实际 %v", list)
	}
	if !errors.Is(err, boom) {
		t.Errorf("ListPending 应回错，实际 %v", err)
	}

	if l, e := svc.ListByCustomer(ctx, "c"); l != nil || !errors.Is(e, boom) {
		t.Errorf("ListByCustomer 口径应一致，实际 list=%v err=%v", l, e)
	}
	if l, e := svc.ListByOwner(ctx, "o"); l != nil || !errors.Is(e, boom) {
		t.Errorf("ListByOwner 口径应一致，实际 list=%v err=%v", l, e)
	}
	if c, e := svc.DraftStatusCounts(ctx); c != nil || !errors.Is(e, boom) {
		t.Errorf("DraftStatusCounts 失败时不该回空 map（那是「库里零张草稿」），实际 counts=%v err=%v", c, e)
	}

	// Confirm 的读也在锁内，失败必须报"读不动"而不是"草稿不存在"
	if _, e := svc.Confirm(ctx, "draft_any", "sales_1"); !errors.Is(e, boom) {
		t.Errorf("Confirm 遇到读失败应回底层错误，实际 %v", e)
	}
	// 去重查询失败时按"宁缺勿重"丢弃本次意向，且不建草稿
	if got := svc.CreateFromIntent(ctx, &OrderIntent{
		CustomerID: "c", ProductName: "P", Quantity: 1, UnitPrice: 10, Confidence: 0.5,
	}, "o"); got != nil {
		t.Errorf("去重读失败时不应新建草稿（可能重复），实际 %+v", got)
	}
	// 清扫两段同理：出错时不能回 (0, nil)，那会被记成"这一轮扫得很干净"
	if n, e := svc.ExpireOverdue(ctx); n != 0 || !errors.Is(e, boom) {
		t.Errorf("ExpireOverdue 应回错，实际 n=%d err=%v", n, e)
	}
	if n, e := svc.PurgeTerminal(ctx, time.Hour); n != 0 || !errors.Is(e, boom) {
		t.Errorf("PurgeTerminal 应回错，实际 n=%d err=%v", n, e)
	}
}

// 反方向也要钉：真的没有 ≠ 读失败。这条防止实现被改成"只要不是命中就回错"。
// 用真库跑，因为假句柄证不出"没命中"这个业务事实来自数据而不是来自桩。
func TestOrderDraftService_MissingIsNotError(t *testing.T) {
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	db.SetTestDB(database)
	ctx := context.Background()
	svc := NewOrderDraftServiceWithDB(nil, database)

	got, err := svc.GetByID(ctx, "draft_definitely_not_exists")
	if err != nil {
		t.Fatalf("查不到不该回错（那是读失败的事），实际 %v", err)
	}
	if got != nil {
		t.Fatalf("查不到应回 (nil, nil)，实际 %+v", got)
	}
	list, err := svc.ListPending(ctx, "owner_with_no_drafts", 10)
	if err != nil {
		t.Fatalf("空结果不该回错: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("该销售应无待确认草稿，实际 %d 条", len(list))
	}
	counts, err := svc.DraftStatusCounts(ctx)
	if err != nil {
		t.Fatalf("计数为空不该回错: %v", err)
	}
	for k, v := range counts {
		if v != 0 {
			t.Errorf("刚迁移完的表不该有 %s=%d（测试库之间串了）", k, v)
		}
	}
}

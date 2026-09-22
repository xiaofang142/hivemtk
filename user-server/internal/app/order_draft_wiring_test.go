// order_draft_wiring_test.go T-P2-06：草稿竖装配层的实跑。
//
// 装配层单独设测的理由与 T-P0-07 / T-P1-07 同一教训：判定 A 的病灶是"service 侧单测
// 全绿、生产装配点却根本没人调用"。只测 OrderDraftService 自身的逻辑测不到这一层——
// 必须有人真的调 InitOrderDraftRuntime 并断言它装/不装、装的是哪副底座。
//
// 本文件里 AC② 的口径要特别注意：写库走的是 orderDraftProduceFunc(rt) 这个**生产注入
// 用的同一个闭包**，不是直接调 store 或 service 方法。"整条草稿竖在生产路径上一行都不写"
// 是本卡开工前的实测结论，所以这里的断言对象必须是"装配之后的写入通路"。
package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// draftTestDB 建含 order_drafts 的测试库，并把全局 DB 指过去
// （service 里若干构造器读的是全局句柄）。
func draftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	db.SetTestDB(database)
	return database
}

// 每份用例结束都要清全局：Init 会留一个在跑的清扫协程，
// 不清就会串到同包后续用例（以及它们对 currentOrderDraftRuntime() 的断言）上。
func installDraftRuntimeCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(StopOrderDraftRuntime)
}

func TestParseOrderDraftMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "off"},
		{"off", "off"},
		{"false", "off"},
		{"0", "off"},
		{"no", "off"},
		{"shadow", "shadow"},
		{"mirror", "shadow"},
		{"TRUE", "shadow"},
		{"1", "shadow"},
		{"yes", "shadow"},
		{"on", "on"},
		{"enforce", "on"},
		{"durable", "on"},
		{"  ON  ", "on"},
		{"banana", "off"},
	}
	for _, c := range cases {
		if got := parseOrderDraftMode(c.raw); string(got) != c.want {
			t.Errorf("parseOrderDraftMode(%q)=%s，期望 %s", c.raw, got, c.want)
		}
	}
}

// 真值只到 shadow：`FF_LTC_ORDER_DRAFT_DB=true` 不能让权威副本进库。
// 这一条单独立用例，因为它是"按习惯写 true 就换存储介质"这个事故的唯一下防线。
func TestParseOrderDraftMode_BoolTruthDoesNotReachOn(t *testing.T) {
	for _, raw := range []string{"true", "True", "1", "yes", "y"} {
		if parseOrderDraftMode(raw) != orderDraftShadow {
			t.Errorf("%q 应判 shadow，实际 %s", raw, parseOrderDraftMode(raw))
		}
	}
}

// AC③（off 侧）：关旗时不装配，且快照要能说清"没装"而不是"装了但是空的"。
func TestInitOrderDraftRuntime_OffDoesNotAssemble(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "off")

	if rt := InitOrderDraftRuntime(database); rt != nil {
		t.Fatal("off 档不应装配运行时")
	}
	if currentOrderDraftRuntime() != nil {
		t.Fatal("off 档全局运行时应为 nil")
	}
	snap := GetOrderDraftSnapshot(context.Background())
	if snap.Assembled {
		t.Error("快照 Assembled 应为 false")
	}
	if snap.Store != OrderDraftStoreNone {
		t.Errorf("Store=%q，期望 %q", snap.Store, OrderDraftStoreNone)
	}
	if snap.Counts != nil {
		t.Errorf("未装配时 Counts 必须是 nil（回空 map 会被读成 0 张草稿），实际 %v", snap.Counts)
	}
	if snap.Mode != "off" {
		t.Errorf("Mode=%q，期望 off", snap.Mode)
	}
}

// 重复 Init 必须先停上一份的协程：否则测试/多轮 Setup 会攒出并发清扫器，
// 每一轮都只看到部分到期草稿（SKIP LOCKED 让它们各拿一份）却各自自称扫完。
func TestInitOrderDraftRuntime_ReInitStopsPreviousSweeper(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "shadow")

	first := InitOrderDraftRuntime(database)
	if first == nil {
		t.Fatal("shadow 档应装配")
	}
	if !first.sweeper.Running() {
		t.Fatal("shadow 档清扫协程应在跑")
	}
	second := InitOrderDraftRuntime(database)
	if second == nil {
		t.Fatal("第二次 Init 也应装配")
	}
	if first.sweeper.Running() {
		t.Error("重复 Init 后上一份的清扫协程仍在跑（会与新的一份抢同一批草稿）")
	}
	if !second.sweeper.Running() {
		t.Error("新的一份应已启动")
	}
	if first == second {
		t.Error("两次 Init 返回了同一个实例")
	}
}

// db 为 nil + on：退回内存底座，但档位仍是 on —— 这个错配必须能在快照里看见
// （Init 会打 ❌ 告警，快照把同一件事交给端点）。
func TestInitOrderDraftRuntime_OnWithNilDBFallsBackToMemory(t *testing.T) {
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")

	rt := InitOrderDraftRuntime(nil)
	if rt == nil {
		t.Fatal("on 档即使没句柄也要装（回退到内存底座），否则端点无从报告错配")
	}
	if got := rt.svc.StoreKind(); got != service.DraftStoreKindMemory {
		t.Errorf("Store=%s，期望 memory（nil 句柄的回退）", got)
	}
	if rt.svc.Durable() {
		t.Error("内存底座 Durable 必须为 false")
	}
	snap := GetOrderDraftSnapshot(context.Background())
	if snap.Store != service.DraftStoreKindMemory || snap.Durable {
		t.Errorf("快照应回显错配：store=memory durable=false，实际 store=%s durable=%t",
			snap.Store, snap.Durable)
	}
}

// AC①+②：on 档下走生产注入闭包建草稿 ⇒ order_drafts 里能查到真实行，且 Durable=true。
func TestInitOrderDraftRuntime_OnWritesRealRowsThroughProducer(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")

	rt := InitOrderDraftRuntime(database)
	if rt == nil {
		t.Fatal("on 档应装配")
	}
	if !rt.svc.Durable() {
		t.Fatal("on 档 + 有句柄 ⇒ Durable 必须为 true")
	}
	if got := rt.svc.StoreKind(); got != service.DraftStoreKindDB {
		t.Fatalf("Store=%s，期望 db", got)
	}

	o := newBareOrchestrator(t)
	if !attachOrderDraftProducer(o, rt) {
		t.Fatal("attach 应成功")
	}
	if !GetOrderDraftSnapshot(context.Background()).ProducerAttached {
		t.Error("attach 后快照应记 ProducerAttached=true")
	}

	customerID := "ac2_on_producer_cust"
	orderDraftProduceFunc(rt)(context.Background(), customerID, "sales_ac2", &service.SalesResponse{
		Reply: "好的，光子嫩肤 3 次套餐 2280 元，这就给您下单",
	})

	repo := repository.NewOrderDraftRepositoryWithDB(database)
	rows, err := repo.ListByCustomer(context.Background(), customerID)
	if err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("生产装配路径应写入 1 行，实际 %d", len(rows))
	}
	if rows[0].ProductName != "光子嫩肤" || rows[0].Quantity != 3 || rows[0].Status != model.OrderDraftStatusPending {
		t.Errorf("落库字段不符: product=%s qty=%d status=%s",
			rows[0].ProductName, rows[0].Quantity, rows[0].Status)
	}
	if rows[0].OwnerID != "sales_ac2" {
		t.Errorf("OwnerID=%q，期望透传 sales_ac2", rows[0].OwnerID)
	}

	snap := GetOrderDraftSnapshot(context.Background())
	if snap.Counts[model.OrderDraftStatusPending] < 1 {
		t.Errorf("快照计数应含本次 pending，实际 %v", snap.Counts)
	}
}

// ownerID 传空串时由 CreateFromIntent 落到 "system"（生产调用点就是这么传的：
// 会话侧没有稳定的销售归属）。
func TestOrderDraftProduceFunc_EmptyOwnerFallsBackToSystem(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")

	rt := InitOrderDraftRuntime(database)
	orderDraftProduceFunc(rt)(context.Background(), "ac2_empty_owner", "",
		&service.SalesResponse{Reply: "水光针 1 次 980 元"})

	rows, err := repository.NewOrderDraftRepositoryWithDB(database).
		ListByCustomer(context.Background(), "ac2_empty_owner")
	if err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应写入 1 行，实际 %d", len(rows))
	}
	if rows[0].OwnerID != "system" {
		t.Errorf("OwnerID=%q，期望 system", rows[0].OwnerID)
	}
}

// AC①：shadow 档库里要有行（对照用），但 Durable 必须仍是 false。
// 这两件事同时成立才是影子的定义；任何一侧写错，端点就会让人以为"重启不丢了"。
func TestInitOrderDraftRuntime_ShadowMirrorsButStaysNotDurable(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "shadow")

	rt := InitOrderDraftRuntime(database)
	if rt == nil {
		t.Fatal("shadow 档应装配")
	}
	if got := rt.svc.StoreKind(); got != service.DraftStoreKindShadow {
		t.Fatalf("Store=%s，期望 shadow", got)
	}
	if rt.svc.Durable() {
		t.Error("shadow 档 Durable 必须为 false：权威那份是内存")
	}

	customerID := "ac1_shadow_cust"
	orderDraftProduceFunc(rt)(context.Background(), customerID, "sales_shadow",
		&service.SalesResponse{Reply: "玻尿酸 2 支 1980 元"})

	// 库侧（镜像）
	rows, err := repository.NewOrderDraftRepositoryWithDB(database).ListByCustomer(context.Background(), customerID)
	if err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("影子镜像应写 1 行进库，实际 %d", len(rows))
	}

	mirror, ok := rt.svc.MirrorStatus(context.Background())
	if !ok {
		t.Fatal("shadow 底座应报 MirrorStatus")
	}
	if !mirror.Available {
		t.Error("镜像句柄应在位")
	}
	if mirror.Failures != 0 {
		t.Errorf("镜像失败计数=%d，期望 0（LastError=%s）", mirror.Failures, mirror.LastError)
	}
	if mirror.RowCounts[model.OrderDraftStatusPending] < 1 {
		t.Errorf("库侧计数应含 pending，实际 %v", mirror.RowCounts)
	}
	// 内存侧权威读数也在（读走内存 ⇒ 调用方零行为变化）
	if snap := GetOrderDraftSnapshot(context.Background()); snap.Counts[model.OrderDraftStatusPending] < 1 {
		t.Errorf("内存侧计数应含 pending，实际 %v", snap.Counts)
	}
}

// 非影子底座不报 MirrorStatus：端点据此决定 mirror 字段出不出现。
func TestMirrorStatus_NotReportedForNonShadowStores(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")
	rt := InitOrderDraftRuntime(database)
	if _, ok := rt.svc.MirrorStatus(context.Background()); ok {
		t.Error("db 底座不应报 MirrorStatus")
	}
	mem := service.NewOrderDraftService(nil)
	if _, ok := mem.MirrorStatus(context.Background()); ok {
		t.Error("内存底座不应报 MirrorStatus")
	}
}

func TestAttachOrderDraftProducer_NilGuards(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")
	rt := InitOrderDraftRuntime(database)

	if attachOrderDraftProducer(nil, rt) {
		t.Error("编排器为 nil 时 attach 不应报成功")
	}
	if attachOrderDraftProducer(newBareOrchestrator(t), nil) {
		t.Error("运行时为 nil（旗子 off）时 attach 不应报成功")
	}
}

// Stop 幂等 + 之后快照回到未装配态（优雅关停接上时的行为契约）。
func TestStopOrderDraftRuntime_IdempotentAndClears(t *testing.T) {
	database := draftTestDB(t)
	t.Setenv(OrderDraftFlagEnv, "shadow")
	rt := InitOrderDraftRuntime(database)
	if rt == nil {
		t.Fatal("应装配")
	}
	StopOrderDraftRuntime()
	StopOrderDraftRuntime()
	if currentOrderDraftRuntime() != nil {
		t.Error("Stop 后全局运行时应为 nil")
	}
	if rt.sweeper.Running() {
		t.Error("Stop 后清扫协程应已退出")
	}
	if GetOrderDraftSnapshot(context.Background()).Assembled {
		t.Error("Stop 后快照应回到未装配态")
	}
}

// T-P2-06 遗留的那一环：worker 侧有了跨轮累计计数（ExpiredTotal/PurgedTotal），
// 但"快照从 sweeper 取这两个值"这一跳此前没有任何用例盯着。装配层少写一行
// `snap.SweepExpiredTotal = ...`，运维端点就会永久回 0 而全绿 —— 这正是本文件开头
// 那条教训（"service 侧全绿、生产装配点没人调用"）的同一个病灶换个层。
func TestGetOrderDraftSnapshot_CarriesSweepTotals(t *testing.T) {
	database := draftTestDB(t)
	installDraftRuntimeCleanup(t)
	t.Setenv(OrderDraftFlagEnv, "on")
	rt := InitOrderDraftRuntime(database)
	if rt == nil {
		t.Fatal("on 档应装配")
	}
	ctx := context.Background()
	repo := repository.NewOrderDraftRepositoryWithDB(database)

	// 两段各自预置，且**条数故意不等**（2 张到期 pending / 3 行过保留期终态）：
	// 相等时把 expired 填成 purged、或两个都填成 rounds，断言都抓不到。
	for i, id := range []string{"snap_pending_a", "snap_pending_b"} {
		if err := repo.Upsert(ctx, &model.OrderDraft{
			ID: id, CustomerID: "snap_totals_cust_" + string(rune('a'+i)), OwnerID: "snap_totals_sales",
			ProductName: "热玛吉", Quantity: 1, UnitPrice: 1200, TotalAmount: 1200,
			Status: model.OrderDraftStatusPending, Source: "ai_chat",
			CreatedAt: time.Now().Add(-2 * time.Hour), UpdatedAt: time.Now().Add(-2 * time.Hour),
			ExpiresAt: time.Now().Add(-time.Hour),
		}); err != nil {
			t.Fatalf("预置到期草稿 %s 失败: %v", id, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := repo.Upsert(ctx, &model.OrderDraft{
			ID: "snap_stale_" + string(rune('a'+i)), CustomerID: "snap_totals_stale_" + string(rune('a'+i)),
			OwnerID: "snap_totals_sales", ProductName: "水光针", Quantity: 1, UnitPrice: 980, TotalAmount: 980,
			Status: model.OrderDraftStatusExpired, Source: "ai_chat",
			CreatedAt: time.Now().Add(-100 * 24 * time.Hour), UpdatedAt: time.Now().Add(-100 * 24 * time.Hour),
			ExpiresAt: time.Now().Add(-99 * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("预置终态行失败: %v", err)
		}
	}

	// 一轮扫完再读数：节拍是 DefaultOrderDraftSweepInterval（小时级），本轮之后不会再有
	// 后台轮次插进来，所以"快照值 == worker 自己那份累计值"是可以在同一时刻成立的硬等式。
	if r := rt.sweeper.RunOnce(ctx); r.ExpireError != "" || r.PurgeError != "" {
		t.Fatalf("清扫轮报错：expire=%q purge=%q", r.ExpireError, r.PurgeError)
	}
	wantExpired, wantPurged := rt.sweeper.ExpiredTotal(), rt.sweeper.PurgedTotal()
	if wantExpired < 1 || wantPurged < 1 || wantExpired == wantPurged {
		t.Fatalf("夹具没给出不等的两段累计数（expired=%d purged=%d）⇒ 后面的等式抓不到填错列", wantExpired, wantPurged)
	}

	snap := GetOrderDraftSnapshot(ctx)
	if snap.SweepExpiredTotal != wantExpired || snap.SweepPurgedTotal != wantPurged {
		t.Errorf("快照应带上清扫累计数 expired=%d purged=%d，实际 expired_total=%d purged_total=%d",
			wantExpired, wantPurged, snap.SweepExpiredTotal, snap.SweepPurgedTotal)
	}

	// 第二段盯的是字段名承诺的那件事：**跨轮累加**，不是"最近一轮的值"。
	// 只跑一轮时 `+=` 写成 `=` 读数一模一样（本轮实测：那条变异在单轮夹具下存活），
	// 所以这里再补一张到期行、再扫一轮，要求累计数恰好 +1。
	if err := repo.Upsert(ctx, &model.OrderDraft{
		ID: "snap_pending_c", CustomerID: "snap_totals_cust_c", OwnerID: "snap_totals_sales",
		ProductName: "热玛吉", Quantity: 1, UnitPrice: 1200, TotalAmount: 1200,
		Status: model.OrderDraftStatusPending, Source: "ai_chat",
		CreatedAt: time.Now().Add(-2 * time.Hour), UpdatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("预置第二轮到期草稿失败: %v", err)
	}
	rt.sweeper.RunOnce(ctx)
	snap2 := GetOrderDraftSnapshot(ctx)
	if snap2.SweepExpiredTotal != wantExpired+1 {
		t.Errorf("第二轮应把累计 expired 从 %d 累到 %d，实际 %d（=上一轮的值 ⇒ 计数被覆盖而非累加）",
			wantExpired, wantExpired+1, snap2.SweepExpiredTotal)
	}
	if snap2.SweepPurgedTotal != wantPurged {
		t.Errorf("第二轮没有新到期的终态行，累计 purged 应仍是 %d，实际 %d", wantPurged, snap2.SweepPurgedTotal)
	}
}

// newBareOrchestrator 造一个只用于验证注入面的编排器（不跑会话链）。
//
// HandleIncomingWithAgent 需要完整引擎 + 会话仓储，为了断言"setter 挂得上"去搭那套
// 不值当；生产者被真正触发的形状由 service 侧的 TestRunOrderDraftProduce_* 覆盖
// （那边同包可驱动 runOrderDraftProduce）。两边合起来才是"注入 + 调用"两段都有人盯。
func newBareOrchestrator(t *testing.T) *service.SmartCSOrchestrator {
	t.Helper()
	return service.NewSmartCSOrchestrator(nil, &service.OrchestratorConfig{
		ConfidenceThreshold: 0.5,
		EnableAutoReply:     false,
		MaxAIConsecutive:    1,
	}, nil)
}

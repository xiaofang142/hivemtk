// order_draft_shadow_test.go T-P2-06 ①/③：影子底座的对照语义 + 读失败与读为空的区分。
//
// 影子档存在的唯一理由是"换底座前后读到的东西对不对得上"这件事得有对照，
// 所以本文件的断言全部围绕三组区分：
//   - 写两遍、但**读只读内存**（否则灰度期就把行为换了）；
//   - 镜像失败不阻断业务、但必须数得出来（否则对照数据会静默少一半）；
//   - "库里没有" 与 "库读不动" 是两种返回（AC③，本卡开工前两者逐字相同）。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// stubDraftRepo 只实现影子底座会调到的那几个方法，其余留给内嵌接口（调到即 panic，
// 那本身就是"影子多依赖了一条路径"的信号）。
type stubDraftRepo struct {
	repository.OrderDraftRepository

	available bool
	upsertErr error
	countErr  error
	counts    map[string]int64

	upserts    int
	upserted   []*model.OrderDraft
	expireArgs []time.Time
	purgeArgs  []time.Time
	purgeN     int64
	purgeErr   error
	expireRows []*model.OrderDraft
}

func (s *stubDraftRepo) Available() bool { return s.available }

func (s *stubDraftRepo) Upsert(_ context.Context, d *model.OrderDraft) error {
	s.upserts++
	if s.upsertErr == nil {
		s.upserted = append(s.upserted, d)
	}
	return s.upsertErr
}

func (s *stubDraftRepo) CountByStatus(context.Context) (map[string]int64, error) {
	if s.countErr != nil {
		return nil, s.countErr
	}
	return s.counts, nil
}

func (s *stubDraftRepo) ExpirePendingBatch(_ context.Context, now time.Time, _ int) ([]*model.OrderDraft, error) {
	s.expireArgs = append(s.expireArgs, now)
	return s.expireRows, nil
}

func (s *stubDraftRepo) PurgeTerminal(_ context.Context, before time.Time, _ int) (int64, error) {
	s.purgeArgs = append(s.purgeArgs, before)
	return s.purgeN, s.purgeErr
}

func newStubShadow(stub *stubDraftRepo) *shadowDraftStore {
	return &shadowDraftStore{primary: newMemoryDraftStore(), mirror: stub}
}

// 影子的定义性属性：库里有真实行、权威读数仍是内存、Durable 必须是 false。
func TestShadowDraftStore_WritesBothReadsMemory(t *testing.T) {
	database := draftSweepTestDB(t)
	ctx := context.Background()
	st := &shadowDraftStore{
		primary: newMemoryDraftStore(),
		mirror:  repository.NewOrderDraftRepositoryWithDB(database),
	}
	svc := newOrderDraftService(nil, st)

	if svc.Durable() {
		t.Error("影子档 Durable 必须为 false：权威那份是内存，重启即丢")
	}
	if got := svc.StoreKind(); got != DraftStoreKindShadow {
		t.Errorf("StoreKind=%s，期望 %s", got, DraftStoreKindShadow)
	}

	draft, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "shadow_wbr", OwnerID: "sales_shadow_wbr",
		ProductName: "光子嫩肤", Quantity: 2, UnitPrice: 880,
	})
	if err != nil {
		t.Fatalf("影子档建草稿失败: %v", err)
	}

	// 内存侧（权威读）
	got, err := svc.GetByID(ctx, draft.ID)
	if err != nil || got == nil {
		t.Fatalf("内存侧应读得到刚建的草稿: got=%v err=%v", got, err)
	}
	// 库侧（镜像写）
	row, err := repository.NewOrderDraftRepositoryWithDB(database).GetByID(ctx, draft.ID)
	if err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if row == nil {
		t.Fatal("影子档应把新建草稿镜像进库（否则灰度期没有对照数据）")
	}
	if row.CustomerID != "shadow_wbr" || row.Quantity != 2 || row.Status != model.OrderDraftStatusPending {
		t.Errorf("镜像行字段与内存那份不符: %+v", row)
	}

	// Edit 走 mutateIfPending：内存改完必须再镜像一次 Upsert，两边才不会各说各话
	qty := 5
	if err := svc.Edit(ctx, draft.ID, DraftUpdates{Quantity: &qty}); err != nil {
		t.Fatalf("Edit 失败: %v", err)
	}
	after, err := repository.NewOrderDraftRepositoryWithDB(database).GetByID(ctx, draft.ID)
	if err != nil || after == nil {
		t.Fatalf("读镜像失败: %v", err)
	}
	if after.Quantity != 5 {
		t.Errorf("库侧应跟随内存侧的编辑（数量 5），实际 %d —— 影子期的对照会因此失真", after.Quantity)
	}

	// 两个计数口径必须各归各：svc 的读走内存，MirrorStatus 才给库侧
	memCounts, err := svc.DraftStatusCounts(ctx)
	if err != nil {
		t.Fatalf("内存计数失败: %v", err)
	}
	if memCounts[string(DraftStatusPending)] != 1 {
		t.Errorf("内存侧计数应含 1 张 pending，实际 %v", memCounts)
	}
	mirror, ok := svc.MirrorStatus(ctx)
	if !ok {
		t.Fatal("影子底座应报 MirrorStatus")
	}
	if !mirror.Available {
		t.Error("镜像句柄在位时 Available 应为 true")
	}
	if mirror.ReadError != "" || mirror.Failures != 0 {
		t.Errorf("无故障时不该带错: read_err=%q failures=%d", mirror.ReadError, mirror.Failures)
	}
	if mirror.RowCounts[model.OrderDraftStatusPending] != 1 {
		t.Errorf("库侧计数应为 1 张 pending，实际 %v", mirror.RowCounts)
	}
}

// 镜像写失败：业务照旧（草稿仍在内存里建成），但失败必须累计可查。
//
// 这条是"影子期对照数据静默少一半"这个事故的唯一下防线 —— 原实现若把 err 丢掉，
// 端点上 failures 永远是 0，运维会以为两边已对齐。
func TestShadowDraftStore_MirrorFailureCountedButNotFatal(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("dial tcp 127.0.0.1: order_drafts connection refused")
	stub := &stubDraftRepo{available: true, upsertErr: boom}
	st := newStubShadow(stub)
	svc := newOrderDraftService(nil, st)

	draft, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "shadow_fail", OwnerID: "s", ProductName: "P", Quantity: 1, UnitPrice: 10,
	})
	if err != nil {
		t.Fatalf("镜像失败不该让建草稿失败: %v", err)
	}
	if draft == nil {
		t.Fatal("草稿仍应建成（权威在内存）")
	}
	if stub.upserts != 1 {
		t.Errorf("应尝试镜像一次，实际 %d 次", stub.upserts)
	}
	mirror, _ := svc.MirrorStatus(ctx)
	if mirror.Failures != 1 {
		t.Errorf("镜像失败计数应为 1，实际 %d", mirror.Failures)
	}
	if !strings.Contains(mirror.LastError, "connection refused") {
		t.Errorf("LastError 应带上底层原因，实际 %q", mirror.LastError)
	}
	if len(mirror.RowCounts) != 0 {
		t.Errorf("镜像写失败时不该谎报行数，实际 %v", mirror.RowCounts)
	}

	// 第二次失败要累计，不是覆盖
	if _, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "shadow_fail", OwnerID: "s", ProductName: "Q", Quantity: 1, UnitPrice: 10,
	}); err != nil {
		t.Fatalf("第二次建草稿也应成功: %v", err)
	}
	mirror2, _ := svc.MirrorStatus(ctx)
	if mirror2.Failures != 2 {
		t.Errorf("镜像失败应累计为 2，实际 %d", mirror2.Failures)
	}
}

// 镜像句柄在位但库侧计数读不动：回 ReadError 且 RowCounts 为 nil，不回空 map。
//
// 空 map 在端点上会被读成"库里一行都没有"，那是另一句假话。
func TestShadowDraftStore_MirrorCountReadErrorDistinct(t *testing.T) {
	ctx := context.Background()
	stub := &stubDraftRepo{
		available: true,
		countErr:  errors.New("permission denied for table order_drafts"),
	}
	svc := newOrderDraftService(nil, newStubShadow(stub))
	mirror, ok := svc.MirrorStatus(ctx)
	if !ok {
		t.Fatal("影子底座应报 MirrorStatus")
	}
	if mirror.ReadError == "" {
		t.Error("计数读失败必须带 ReadError")
	}
	if mirror.RowCounts != nil {
		t.Errorf("读失败时 RowCounts 应为 nil（空 map 会被当成库是空的），实际 %v", mirror.RowCounts)
	}
	if mirror.Failures != 0 {
		t.Errorf("读失败不是写失败，不该记进 Failures，实际 %d", mirror.Failures)
	}
}

// 句柄可用性要单独报：影子档但没地方写 ≠ 有地方写但还没写东西。
func TestShadowDraftStore_AvailableFalseReported(t *testing.T) {
	svc := newOrderDraftService(nil, newStubShadow(&stubDraftRepo{available: false}))
	mirror, ok := svc.MirrorStatus(context.Background())
	if !ok {
		t.Fatal("影子底座应报 MirrorStatus")
	}
	if mirror.Available {
		t.Error("句柄不可用时 Available 应为 false")
	}
	if mirror.RowCounts != nil {
		t.Error("句柄不可用时不该去读行数")
	}
}

// 过期事件只能发一遍：内存侧回调，库侧只翻状态不再回调，否则"过期草稿数"直接翻倍。
func TestShadowDraftStore_ExpireEmitsEventsOnce(t *testing.T) {
	ctx := context.Background()
	stub := &stubDraftRepo{
		available:  true,
		expireRows: []*model.OrderDraft{{ID: "db-side-1"}, {ID: "db-side-2"}},
	}
	st := newStubShadow(stub)
	now := scenarioNow
	if err := st.put(ctx, seedDraft("shadow-exp-1", "c", "o", "P", DraftStatusPending, 0.5, 10,
		now.Add(-time.Hour), now.Add(-time.Minute))); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	if err := st.put(ctx, seedDraft("shadow-exp-2", "c", "o", "Q", DraftStatusPending, 0.5, 10,
		now.Add(-time.Hour), now.Add(-time.Minute))); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	var emitted []string
	n, err := st.expireOverdue(ctx, now, func(d *OrderDraft) { emitted = append(emitted, d.ID) })
	if err != nil {
		t.Fatalf("过期失败: %v", err)
	}
	if n != 2 {
		t.Errorf("应翻 2 条，实际 %d", n)
	}
	if len(emitted) != 2 {
		t.Errorf("expired 事件应恰好每条一次（2 次），实际 %d：%v", len(emitted), emitted)
	}
	if len(stub.expireArgs) != 1 || !stub.expireArgs[0].Equal(now) {
		t.Errorf("库侧应跟着翻一批（一次、同一个 now），实际 %v", stub.expireArgs)
	}
}

// 清理同理：内存删完也要把库侧终态行删掉，否则库里只涨不删，而端点看着"已清理"。
func TestShadowDraftStore_PurgeCleansMirrorToo(t *testing.T) {
	ctx := context.Background()
	stub := &stubDraftRepo{available: true, purgeN: 3}
	st := newStubShadow(stub)
	before := scenarioNow.Add(-90 * 24 * time.Hour)
	if err := st.put(ctx, seedDraft("shadow-purge", "c", "o", "P", DraftStatusExpired, 0.5, 10,
		before.Add(-time.Hour), before.Add(-time.Hour))); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	n, err := st.purgeTerminal(ctx, before)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 1 {
		t.Errorf("内存侧应删 1 条，实际 %d", n)
	}
	if len(stub.purgeArgs) == 0 || !stub.purgeArgs[0].Equal(before) {
		t.Errorf("库侧应以同一 before 清理，实际 %v", stub.purgeArgs)
	}

	// 库侧清理失败：内存结果照回，不连带报错（两段各自成败）
	stub2 := &stubDraftRepo{available: true, purgeErr: errors.New("deadlock")}
	st2 := newStubShadow(stub2)
	if err := st2.put(ctx, seedDraft("shadow-purge-2", "c", "o", "P", DraftStatusCancelled, 0.5, 10,
		before.Add(-time.Hour), before.Add(-time.Hour))); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	n2, err := st2.purgeTerminal(ctx, before)
	if err != nil {
		t.Errorf("镜像清理失败不该让业务清理回错: %v", err)
	}
	if n2 != 1 {
		t.Errorf("内存侧删除数仍应回 1，实际 %d", n2)
	}
	if failures, lastErr := st2.MirrorStats(); failures != 1 || !strings.Contains(lastErr, "deadlock") {
		t.Errorf("镜像清理失败要计一次数，实际 failures=%d last=%q", failures, lastErr)
	}
}

// 影子档没拿到句柄 ⇒ 退回**纯内存**，而不是"影子但没有镜像"。
// 退错的后果是端点上 available=false 与"今天就是没写进库"两种情况再也分不开。
func TestNewShadowDraftStoreForDB_NilHandleIsPlainMemory(t *testing.T) {
	st := newShadowDraftStoreForDB(nil)
	if _, ok := st.(*memoryDraftStore); !ok {
		t.Fatalf("nil 句柄应退回 memoryDraftStore，实际 %T", st)
	}
	svc := newOrderDraftService(nil, st)
	if got := svc.StoreKind(); got != DraftStoreKindMemory {
		t.Errorf("StoreKind=%s，期望 memory（连影子都不是，端点要能看出来）", got)
	}
	if _, ok := svc.MirrorStatus(context.Background()); ok {
		t.Error("已退回内存 ⇒ 不该报 MirrorStatus")
	}
}

// 有句柄时才真是影子
func TestNewShadowDraftStoreForDB_WithHandle(t *testing.T) {
	database := draftSweepTestDB(t)
	st := newShadowDraftStoreForDB(database)
	shadow, ok := st.(*shadowDraftStore)
	if !ok {
		t.Fatalf("有句柄应是 shadowDraftStore，实际 %T", st)
	}
	if !shadow.mirrorAvailable() {
		t.Error("真句柄下镜像应可用")
	}
	if shadow.durable() {
		t.Error("影子底座 durable 必须为 false")
	}
}

// 影子档的过期要真的落到库里那行（镜像批次带的是真行）。
func TestShadowDraftStore_EndToEndExpiresMemoryAndDB(t *testing.T) {
	database := draftSweepTestDB(t)
	ctx := context.Background()
	svc := NewOrderDraftServiceShadow(&OrderDraftConfig{DefaultExpiry: time.Millisecond}, database)
	draft, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "shadow_e2e", OwnerID: "s", ProductName: "水光针", Quantity: 1, UnitPrice: 980,
	})
	if err != nil {
		t.Fatalf("建草稿失败: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	n, err := svc.ExpireOverdue(ctx)
	if err != nil {
		t.Fatalf("过期失败: %v", err)
	}
	if n < 1 {
		t.Fatalf("应至少翻 1 条，实际 %d", n)
	}
	memSide, err := svc.GetByID(ctx, draft.ID)
	if err != nil || memSide == nil {
		t.Fatalf("内存侧读回应命中: %v", err)
	}
	if memSide.Status != DraftStatusExpired {
		t.Errorf("内存侧状态=%s，期望 expired", memSide.Status)
	}
	row, err := repository.NewOrderDraftRepositoryWithDB(database).GetByID(ctx, draft.ID)
	if err != nil || row == nil {
		t.Fatalf("查库失败: %v", err)
	}
	if row.Status != model.OrderDraftStatusExpired {
		t.Errorf("库侧那行状态=%s，期望 expired（影子期两边状态漂移就没法对照了）", row.Status)
	}
}

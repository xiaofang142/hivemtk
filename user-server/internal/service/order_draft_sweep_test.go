// order_draft_sweep_test.go T-P2-06 ②/AC④：到期与终态清理**按节拍真的被调用**。
//
// 卡面原话是"ExpireOverdue/PurgeTerminal 有调用方且有测试证明按节拍触发"。这里刻意
// 分成两层来测，因为这两个失败面不一样：
//   - 节拍层（recording store）：证明 timer 到点就会调这两段、两段的**先后顺序**固定、
//     且过期段失败不会连带跳过清理段 —— 这三件事都跟库里有什么数据无关，
//     用真库测反而测不到（数据不足时两段都返回 0，看不出谁被调了）；
//   - 落库层（真 PG）：证明这条节拍最终改写的是**表里的行**，不是只改内存对象。
//     只有前者会漏掉的病灶是"worker 在跑、方法在调、但底座是内存所以重启就回到原样"。
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// draftSweepTestDB 建含 order_drafts 的真库并把全局句柄指过去（service 内若干
// 构造器读全局，指过去是为了别的应用路径不会拿到没迁移的库）。
func draftSweepTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.OrderDraft{})
	db.SetTestDB(database)
	return database
}

// recordingDraftStore 只记"哪一段被调、按什么顺序"，行为可注入失败。
//
// 其余方法透传给内存底座：Confirm/去重那套逻辑在清扫里用不到，但接口得完整实现，
// 所以嵌一个真 store 而不是手写七个空方法（手写的话哪天 draftStore 加方法这里会静默失配）。
type recordingDraftStore struct {
	draftStore
	mu       sync.Mutex
	calls    []string
	purgeArg []time.Time

	expireErr error
	expireN   int
	purgeErr  error
	purgeN    int
	// purgeStub 置位则不真删、固定回 purgeN 行 —— 用来断"过期段失败时清理段确实跑了"，
	// 否则内存底座里没播种，purge 永远回 0，那条断言就是空的。
	purgeStub bool
}

func newRecordingStore() *recordingDraftStore {
	return &recordingDraftStore{draftStore: newMemoryDraftStore()}
}

func (r *recordingDraftStore) record(call string) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
}

func (r *recordingDraftStore) expireOverdue(ctx context.Context, now time.Time, ev func(*OrderDraft)) (int, error) {
	r.record("expire")
	if r.expireErr != nil {
		return r.expireN, r.expireErr
	}
	return r.draftStore.expireOverdue(ctx, now, ev)
}

func (r *recordingDraftStore) purgeTerminal(ctx context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	r.calls = append(r.calls, "purge")
	r.purgeArg = append(r.purgeArg, before)
	r.mu.Unlock()
	if r.purgeErr != nil {
		return r.purgeN, r.purgeErr
	}
	if r.purgeStub {
		return r.purgeN, nil
	}
	return r.draftStore.purgeTerminal(ctx, before)
}

func (r *recordingDraftStore) snapshotCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *recordingDraftStore) lastPurgeBefore() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.purgeArg) == 0 {
		return time.Time{}, false
	}
	return r.purgeArg[len(r.purgeArg)-1], true
}

// waitUntil 轮询到 cond 为真，超时返回 false（不给固定 sleep：CI 上慢机器会假红，
// 快机器会白等）。
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// AC④（节拍层）：间隔到了就各跑一段，顺序固定"先过期后清理"。
func TestOrderDraftSweepWorker_RunsBothPhasesOnCadence(t *testing.T) {
	store := newRecordingStore()
	svc := newOrderDraftService(nil, store)
	w := NewOrderDraftSweepWorker(svc, 15*time.Millisecond, 0)
	ctx := context.Background()
	w.Start(ctx)
	t.Cleanup(func() { w.Stop(context.Background()) })

	if !w.Running() {
		t.Fatal("Start 后 worker 应在跑")
	}
	if !waitUntil(t, 3*time.Second, func() bool { return w.Rounds() >= 3 }) {
		t.Fatalf("3 秒内没跑到 3 轮，实际轮次=%d（节拍没转起来 = PurgeTerminal 依然没有定时调用方）", w.Rounds())
	}
	calls := store.snapshotCalls()
	if len(calls) < 6 {
		t.Fatalf("3 轮应至少 6 次调用，实际 %v", calls)
	}
	for i := 0; i+1 < len(calls); i += 2 {
		if calls[i] != "expire" || calls[i+1] != "purge" {
			t.Fatalf("第 %d/%d 次调用顺序应为 expire→purge，实际 %s→%s（先清理会让到期 pending 永远留在库里）",
				i, i+1, calls[i], calls[i+1])
		}
	}

	// Stop 之后轮次不再增长：否则"关掉的 worker 还在扫"会变成测试之间互相踩的源头。
	w.Stop(context.Background())
	if w.Running() {
		t.Error("Stop 后 Running 应为 false")
	}
	stoppedAt := w.Rounds()
	time.Sleep(80 * time.Millisecond)
	if got := w.Rounds(); got != stoppedAt {
		t.Errorf("Stop 后轮次仍在增长：%d → %d", stoppedAt, got)
	}
	// 幂等：第二次 Stop 不能 panic / 不能卡住
	w.Stop(context.Background())
}

// 过期段失败**不**跳过清理段（一次 UPDATE 失败不该放大成两件都不做）。
func TestOrderDraftSweepWorker_ExpireFailureDoesNotSkipPurge(t *testing.T) {
	store := newRecordingStore()
	store.expireErr = errors.New("deadlock detected")
	store.purgeStub = true
	store.purgeN = 7
	svc := newOrderDraftService(nil, store)
	w := NewOrderDraftSweepWorker(svc, time.Hour, 0)

	report := w.RunOnce(context.Background())
	if report.ExpireError == "" {
		t.Error("过期段失败必须写进 ExpireError")
	}
	if report.PurgeError != "" {
		t.Errorf("清理段本身没失败，不该带错：%q", report.PurgeError)
	}
	if report.Purged != 7 {
		t.Errorf("过期失败后清理段仍应执行并回行数，实际 purged=%d", report.Purged)
	}
	if !report.Failed() {
		t.Error("有任一半失败时 Failed() 必须为 true")
	}
	calls := store.snapshotCalls()
	if len(calls) != 2 || calls[0] != "expire" || calls[1] != "purge" {
		t.Errorf("调用序列应为 [expire purge]，实际 %v", calls)
	}
	if got := w.Rounds(); got != 1 {
		t.Errorf("一轮结束应记 1 轮，实际 %d", got)
	}
	if w.LastReport() == nil {
		t.Fatal("LastReport 应可读到刚才那轮")
	}
	if w.LastReport().ExpireError == "" {
		t.Error("LastReport 应保留失败原因（端点要据此报警）")
	}
	// 复制语义：改返回值不该污染 worker 内部那份
	lr := w.LastReport()
	lr.ExpireError = "hacked"
	if w.LastReport().ExpireError == "hacked" {
		t.Error("LastReport 返回了内部指针（并发读会被改写）")
	}
}

// 清理段失败同样单独可见（两段各自成报错字段，端点才分得清"没做成"和"做了一半"）。
func TestOrderDraftSweepWorker_PurgeFailureReported(t *testing.T) {
	store := newRecordingStore()
	store.purgeErr = errors.New("relation order_drafts does not exist")
	svc := newOrderDraftService(nil, store)
	w := NewOrderDraftSweepWorker(svc, time.Hour, 0)
	report := w.RunOnce(context.Background())
	if report.PurgeError == "" {
		t.Fatal("清理段失败必须单独报出来（表会继续无界增长）")
	}
	if report.ExpireError != "" {
		t.Errorf("过期段没失败，不该被连带记错：%q", report.ExpireError)
	}
}

// 保留期参数：<=0 回落到默认 90 天，且传进 store 的 before = now-90d。
// 这条测的是"默认值只有一份"：worker 与 service 各自写一份 90 天的话，改一处就分叉。
func TestOrderDraftSweepWorker_RetentionFallsBackToSingleDefault(t *testing.T) {
	store := newRecordingStore()
	svc := newOrderDraftService(nil, store)

	w := NewOrderDraftSweepWorker(svc, time.Hour, 0)
	before, ok := func() (time.Time, bool) {
		w.RunOnce(context.Background())
		return store.lastPurgeBefore()
	}()
	if !ok {
		t.Fatal("purgeTerminal 应被调用并收到 before")
	}
	want := time.Now().Add(-defaultDraftRetention)
	if diff := before.Sub(want); diff > time.Second || diff < -time.Second {
		t.Errorf("retention=0 时 before 应为 now-%s，实际偏 %s", defaultDraftRetention, diff)
	}
	if got := w.retentionString(); got != defaultDraftRetention.String()+"（默认）" {
		t.Errorf("retentionString=%q，期望标注为默认值", got)
	}

	custom := 3 * time.Hour
	w2 := NewOrderDraftSweepWorker(svc, time.Hour, custom)
	w2.RunOnce(context.Background())
	b2, _ := store.lastPurgeBefore()
	if diff := b2.Sub(time.Now().Add(-custom)); diff > time.Second || diff < -time.Second {
		t.Errorf("显式保留期 %s 未透传，before=%s", custom, b2)
	}
	if got := w2.retentionString(); got != custom.String() {
		t.Errorf("retentionString=%q，期望 %s", got, custom)
	}
}

// 默认节拍常量与"首轮延后一个间隔"：首轮延后与 RecoveryQueueWorker 同口径
// （启动瞬间正有进程在重启，那一次全表 UPDATE 会与恢复期读请求抢锁）。
func TestOrderDraftSweepWorker_FirstRoundDeferred(t *testing.T) {
	if DefaultOrderDraftSweepInterval != 6*time.Hour {
		t.Errorf("默认节拍=%s，与卡面登记（6h，对齐天级过期判据）不符", DefaultOrderDraftSweepInterval)
	}
	store := newRecordingStore()
	w := NewOrderDraftSweepWorker(newOrderDraftService(nil, store), 50*time.Millisecond, 0)
	w.Start(context.Background())
	t.Cleanup(func() { w.Stop(context.Background()) })
	if n := len(store.snapshotCalls()); n != 0 {
		t.Errorf("Start 瞬间不应立刻扫一轮（重启风暴期抢锁），实际已有 %d 次调用", n)
	}
	if w.Interval() != 50*time.Millisecond {
		t.Errorf("Interval 回显=%s，期望 50ms（端点要显示生效节拍而非默认值）", w.Interval())
	}
}

// svc 为 nil：不启动并出声；RunOnce 两段都记错而不是 panic 或静默成功。
func TestOrderDraftSweepWorker_NilServiceSafe(t *testing.T) {
	w := NewOrderDraftSweepWorker(nil, 10*time.Millisecond, 0)
	w.Start(context.Background())
	if w.Running() {
		t.Error("没有草稿服务时 worker 不应自称在跑")
	}
	report := w.RunOnce(context.Background())
	if !report.Failed() {
		t.Error("svc=nil 时 RunOnce 必须报错（静默成功会让监控以为在正常清理）")
	}
	var nilWorker *OrderDraftSweepWorker
	nilWorker.Start(context.Background())
	nilWorker.Stop(context.Background())
	if r := nilWorker.RunOnce(context.Background()); r == nil || !r.Failed() {
		t.Error("nil worker 调用 Start/Stop/RunOnce 应回失败报告而非 panic")
	}
	// Running/Rounds/LastReport 刻意不承诺 nil 接收者：唯一的调用方（app 层快照）在调之前
	// 就判了 rt.sweeper != nil，为到不了的分支加空判只会让"没启动"和"指针没了"分不开。
	if w.Interval() != 10*time.Millisecond {
		t.Errorf("Interval 回显=%s，期望 10ms", w.Interval())
	}
}

// Start 幂等：重复调用只起一个协程（Init 可被重复调用，见 app 层；但同一份 worker
// 被 Start 两次会攒出两个 loop，两份都会扫同一批行）。
func TestOrderDraftSweepWorker_StartIdempotent(t *testing.T) {
	store := newRecordingStore()
	w := NewOrderDraftSweepWorker(newOrderDraftService(nil, store), 10*time.Millisecond, 0)
	w.Start(context.Background())
	w.Start(context.Background())
	t.Cleanup(func() { w.Stop(context.Background()) })
	if !waitUntil(t, 3*time.Second, func() bool { return w.Rounds() >= 2 }) {
		t.Fatalf("两轮未跑完，轮次=%d", w.Rounds())
	}
	// 一轮 = expire+purge 两次调用；两个 loop 会让两次计数几乎同时增长，
	// 这里用"停止后再观察到的调用数与轮次同阶"这条硬判据反证只有一个协程在跑。
	w.Stop(context.Background())
	rounds := w.Rounds()
	calls := len(store.snapshotCalls())
	if calls > int(rounds)*2+2 {
		t.Errorf("调用数 %d 远超轮数 %d 的两倍 ⇒ 疑似起了多个清扫协程", calls, rounds)
	}
}

// ---------------------------------------------------------------- 落库层 ----

// AC④（落库层）：到期草稿由**定时节拍**在真库里翻成 expired，终态行按保留期被删。
//
// 这条是本卡的关键反假绿：节拍层测过了不等于底座收到了 —— 装配到内存底座上的 worker
// 一样能跑满轮次，但重启后库里那行仍是 pending。所以这里必须过 repository 读回来断言。
func TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows(t *testing.T) {
	database := draftSweepTestDB(t)
	repo := repository.NewOrderDraftRepositoryWithDB(database)
	ctx := context.Background()

	svc := NewOrderDraftServiceWithDB(nil, database)
	if !svc.Durable() {
		t.Fatal("本用例必须跑在 durable 底座上，否则测的是内存")
	}

	// ① 一张已到期的 pending：把默认到期时长设成极短，走的是生产同一条建草稿路径
	expiring := newOrderDraftService(&OrderDraftConfig{DefaultExpiry: time.Millisecond},
		newDraftStoreForDB(database))
	created, err := expiring.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "sweep_e2e_cust", OwnerID: "sweep_e2e_sales",
		ProductName: "光子嫩肤", Quantity: 1, UnitPrice: 880,
	})
	if err != nil {
		t.Fatalf("建草稿失败: %v", err)
	}

	// ② 一张已过保留期的终态行：直接按 model 写，UpdatedAt 挪到 100 天前
	stale := &model.OrderDraft{
		ID: "draft_sweep_e2e_stale", CustomerID: "sweep_e2e_cust", OwnerID: "sweep_e2e_sales",
		ProductName: "水光针", Quantity: 1, UnitPrice: 980, TotalAmount: 980,
		Status: model.OrderDraftStatusExpired, Source: "ai_chat",
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour),
		UpdatedAt: time.Now().Add(-100 * 24 * time.Hour),
		ExpiresAt: time.Now().Add(-99 * 24 * time.Hour),
	}
	if err := repo.Upsert(ctx, stale); err != nil {
		t.Fatalf("预置终态行失败: %v", err)
	}

	w := NewOrderDraftSweepWorker(svc, 20*time.Millisecond, time.Second)
	w.Start(ctx)
	t.Cleanup(func() { w.Stop(context.Background()) })

	// 到期翻转
	if !waitUntil(t, 5*time.Second, func() bool {
		m, err := repo.GetByID(ctx, created.ID)
		return err == nil && m != nil && m.Status == model.OrderDraftStatusExpired
	}) {
		m, err := repo.GetByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("读回草稿失败: %v", err)
		}
		t.Fatalf("定时节拍没把库里到期草稿翻成 expired，实际状态=%v 轮次=%d", m.Status, w.Rounds())
	}

	// 终态清理（保留期 1s，那行的 updated_at 在 100 天前 ⇒ 第一轮就该删掉）
	if !waitUntil(t, 5*time.Second, func() bool {
		m, err := repo.GetByID(ctx, stale.ID)
		return err == nil && m == nil
	}) {
		t.Fatalf("定时节拍没删掉过保留期的终态行（表仍在无界增长）；轮次=%d", w.Rounds())
	}

	lr := w.LastReport()
	if lr == nil {
		t.Fatal("至少跑过一轮，LastReport 不该为 nil")
	}
	if lr.Expired < 1 || lr.Purged < 1 {
		t.Errorf("末轮报告应记到 expired>=1 且 purged>=1，实际 %+v", lr)
	}
	if lr.Store != DraftStoreKindDB || !lr.Durable {
		t.Errorf("末轮报告应回显 db/durable=true，实际 store=%s durable=%t", lr.Store, lr.Durable)
	}
	if lr.ExpireError != "" || lr.PurgeError != "" {
		t.Errorf("端到端清扫不该有失败：expire=%q purge=%q", lr.ExpireError, lr.PurgeError)
	}
}

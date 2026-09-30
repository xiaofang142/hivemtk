package service

// 门控清扫器的跨进程租约（多实例共库时只有一台动手）。
//
// 为什么要它：同一套库上可以同时住着多个进程（本地开发常见的"正式实例 + air 实例 +
// 手工起的旧二进制"），每个实例的清扫协程各自每分钟广播一次，入群提示的重复量就是
// 实例数的整数倍——线上 219 条重复里有相当一部分是这么来的。更要紧的是超时处置会对
// 真人动手（移出群），双跑＝两个人被同一个已死进程的判断各踢一遍。
//
// 判据分两层：sweepOnce 这一层管"抢不到租约就一次 TG 调用都不发"（同步可测）；
// tickLoop 这一层管"节拍跑起来后真会续约、关停真会释放"（把节拍压成毫秒才进得了用例）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// r66WithLease 补建租约表并把清扫器那一行的历史持有者清掉。
//
// 租约行是跨用例残留的（共库、共进程）：上一格留下的"worker-A + 新鲜心跳"会让这一格的
// Hold 直接判负，看上去像"租约互斥生效了"，其实测的是别人的数据。清完再登记 cleanup，
// 免得本格的持有者变成下一格的假阳性。
func r66WithLease(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&model.CronJobLease{}); err != nil {
		t.Fatalf("补建 cron_job_leases: %v", err)
	}
	r66PurgeLease := func() error {
		return db.Where("job_name = ?", tgGateSweeperLeaseJob).Delete(&model.CronJobLease{}).Error
	}
	if err := r66PurgeLease(); err != nil {
		t.Fatalf("清理清扫器租约: %v", err)
	}
	t.Cleanup(func() {
		if err := r66PurgeLease(); err != nil {
			t.Errorf("用例结束后未清掉租约行: %v", err)
		}
	})
}

// 持有者跑、非持有者零动作。非持有者那一格必须用"台账没变 + 一次 TG 调用都没有"来判，
// 只判返回值 true/false 的话，"抢约失败但照样清扫"这种最贵的写法能全绿通过。
func TestSweepOnceRunsOnlyForLeaseHolder(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	r66WithLease(t, db)
	ctx := context.Background()

	// 送达过、宽限期也走完 ⇒ 持有者这一轮应该把他移出
	r66Seed(t, db, accID, "8001", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), false)

	swept, ran, err := sweepOnce(ctx, svc, "worker-A")
	if err != nil {
		t.Fatalf("持有者 sweepOnce: %v", err)
	}
	if !ran {
		t.Fatal("首次抢占租约应成功并由本进程执行")
	}
	if swept != 1 {
		t.Fatalf("持有者应处置 1 人, 实际=%d, 调用=%v", swept, stub.calls)
	}
	if got := r65ReadMember(t, db, accID, "8001"); got.JoinStatus != model.TGMemberKicked {
		t.Fatalf("持有者应完成处置, 实际=%s, 调用=%v", got.JoinStatus, stub.calls)
	}

	// 换一个 worker 来：租约仍在 A 名下且心跳新鲜 ⇒ 本轮零动作
	r66Seed(t, db, accID, "8002", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), false)
	callsBefore := stub.total()

	swept2, ran2, err := sweepOnce(ctx, svc, "worker-B")
	if err != nil {
		t.Fatalf("非持有者 sweepOnce: %v", err)
	}
	if ran2 || swept2 != 0 {
		t.Fatalf("租约在别人名下时不该返回「本轮由我执行」, swept=%d ran=%v", swept2, ran2)
	}
	if got := r65ReadMember(t, db, accID, "8002"); got.JoinStatus != model.TGMemberRestricted {
		t.Fatalf("非持有者不该动台账, 实际=%s", got.JoinStatus)
	}
	if stub.total() != callsBefore {
		t.Fatalf("非持有者应零 TG 调用, 前后=%d→%d, 新增=%v", callsBefore, stub.total(), stub.calls[callsBefore:])
	}

	owner, _, found, err := svc.leaseRepo.Holder(ctx, tgGateSweeperLeaseJob)
	if err != nil || !found {
		t.Fatalf("读回租约: found=%v err=%v", found, err)
	}
	if owner != "worker-A" {
		t.Fatalf("租约应仍由 A 持有, 实际=%s", owner)
	}
}

// 节拍跑起来后的三件事：抢到租约（写进库）→ 真跑一轮清扫（能看到移人）→ Stop 后把租约交回。
//
// 这里走的是登记的启动入口 startGateSweeperAt + 真的 StopGateSweeper，不是手工拼 runner：
// 生产路径的"注册 ⇒ 自己续约 ⇒ 停机释放"必须整条被跑到，否则漏掉任何一环（比如 Stop 只
// cancel 不 Release）都还是线上那句"下一个实例等 3 分钟才敢接手"。
func TestGateSweeperTickLoopTakesAndReleasesLease(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	r66WithLease(t, db)
	ctx := context.Background()

	r66Seed(t, db, accID, "8101", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), false)

	startGateSweeperAt(svc, "worker-tick", 10*time.Millisecond)
	t.Cleanup(func() { StopGateSweeper(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	var owner string
	for time.Now().Before(deadline) {
		if o, _, found, err := svc.leaseRepo.Holder(ctx, tgGateSweeperLeaseJob); err == nil && found {
			owner = o
			if owner == "worker-tick" {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if owner != "worker-tick" {
		t.Fatalf("节拍跑起来后租约应在本 worker 名下, 实际=%q", owner)
	}
	// 处置落库在这一拍的 TG 调用之后，读早了会拿到旧值 ⇒ 轮询到窗口尽头再判
	got := r65ReadMember(t, db, accID, "8101")
	writeDeadline := time.Now().Add(5 * time.Second)
	for got.JoinStatus != model.TGMemberKicked && time.Now().Before(writeDeadline) {
		time.Sleep(20 * time.Millisecond)
		got = r65ReadMember(t, db, accID, "8101")
	}
	if got.JoinStatus != model.TGMemberKicked {
		t.Fatalf("节拍应真跑一轮清扫, 实际台账=%s, 调用=%v", got.JoinStatus, stub.calls)
	}

	StopGateSweeper(ctx)
	if got := currentGateSweeperWorkerID(); got != "" {
		t.Fatalf("Stop 后不该留有登记, 实际=%q", got)
	}
	released, _, _, err := svc.leaseRepo.Holder(ctx, tgGateSweeperLeaseJob)
	if err != nil {
		t.Fatalf("读回租约: %v", err)
	}
	if released != "" {
		t.Fatalf("退出必须交回租约（否则下一个实例最多等 3 分钟才接手）, 实际仍为 %q", released)
	}
}

// 启动幂等 + 停止安全：重复 Start 不换实例（两台在扫同一批台账＝双份播报），
// 未运行时 Stop 不炸也不碰别人的租约。
func TestStartGateSweeperIdempotentAndStopSafe(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	r66WithLease(t, db)
	ctx := context.Background()

	// 先让别的进程名下的租约存在：Stop 在"本进程没跑清扫器"时必须原样留着它
	if _, err := svc.leaseRepo.Hold(ctx, tgGateSweeperLeaseJob, "other-process"); err != nil {
		t.Fatalf("预置他人租约: %v", err)
	}
	StopGateSweeper(ctx) // 无实例可停：不该 panic，更不该把别人的租约清了
	owner, _, found, err := svc.leaseRepo.Holder(ctx, tgGateSweeperLeaseJob)
	if err != nil || !found || owner != "other-process" {
		t.Fatalf("空停动别人的租约了: owner=%q found=%v err=%v", owner, found, err)
	}

	startGateSweeper(svc, "worker-first")
	if got := currentGateSweeperWorkerID(); got != "worker-first" {
		t.Fatalf("首次启动应登记 worker-first, 实际=%q", got)
	}
	startGateSweeper(svc, "worker-second")
	if got := currentGateSweeperWorkerID(); got != "worker-first" {
		t.Fatalf("重复启动不该换实例（旧 ticker 会泄漏成两条清扫流、同一批人被扫两遍）, 实际=%q", got)
	}

	StopGateSweeper(ctx)
	if got := currentGateSweeperWorkerID(); got != "" {
		t.Fatalf("Stop 后不该留有登记, 实际=%q", got)
	}
	// 真跑过一拍的租约已交回；这一台从没抢到，"other-process" 那一行必须原样还在
	owner, _, _, err = svc.leaseRepo.Holder(ctx, tgGateSweeperLeaseJob)
	if err != nil || owner != "other-process" {
		t.Fatalf("未持有租约的实例停机的 Release 走错了行: owner=%q err=%v", owner, err)
	}
	_ = accID
}

// currentGateSweeperWorkerID 读包级登记。同包用例直接拿锁读，不必为此在生产代码里
// 开一个只有测试会调的导出方法。
func currentGateSweeperWorkerID() string {
	gateSweeperMu.Lock()
	defer gateSweeperMu.Unlock()
	if gateSweeper == nil {
		return ""
	}
	return gateSweeper.workerID
}

// currentGateSweeperRunner 读登记的实例**身份**：workerID 在自愈重启前后是同一个值
// （沿用原 worker），要判"换了一台"只能比指针。
func currentGateSweeperRunner() *gateSweeperRunner {
	gateSweeperMu.Lock()
	defer gateSweeperMu.Unlock()
	return gateSweeper
}

// drainGateSweeper 把登记清干净为止。
//
// 只在"panic 与重启之间"那种纳秒级窗口才会需要：Stop 摘走全局到 cancel 之间若恰好
// 落地一次复活，登记会剩一台还在打拍的实例，而它会把下一格的 startGateSweeperAt
// 幂等地顶回去（表现为别人用例的假红）。循环几次直到登记为空，判据仍由调用方持有。
func drainGateSweeper(t *testing.T, attempts int) {
	t.Helper()
	for i := 0; i < attempts; i++ {
		if currentGateSweeperRunner() == nil {
			return
		}
		StopGateSweeper(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	if got := currentGateSweeperRunner(); got != nil {
		t.Errorf("清扫器登记没能清干净（残留实例会让后续用例的启动被幂等地顶掉）, worker=%s", got.workerID)
	}
}

// tick 里 panic 之后清扫器必须自己复活。
//
// 判这条的理由不是"panic 会发生"，而是它发生的形态：**台账从此不再清理，而日志里
// 只留下一行 panic**——没有崩溃、没有告警端点、没有失败计数，下一次有人发现"群里
// 一堆禁言成员没被处理"已经是几小时后。静默失效必须由用例钉住"会复活"。
//
// 制造 panic 的形状：wired() 判的是 gateRepo，所以摘掉 memberRepo 后端面仍算装配完成，
// 而补偿循环一碰台账就 nil 解引用——panic 真发生在 tick 体内（不是取证夹具里）。
func TestGateSweeperSelfHealsAfterTickPanic(t *testing.T) {
	stub := &r65GateStub{}
	svc, _, _ := r65SetupGate(t, stub)
	t.Cleanup(func() { drainGateSweeper(t, 5) })

	panicky := &TelegramGateService{gateRepo: svc.gateRepo}
	startGateSweeperAt(panicky, "worker-panic", 10*time.Millisecond)
	first := currentGateSweeperRunner()
	if first == nil {
		t.Fatal("清扫器没登记起来，后面的「复活」无从判起")
	}

	deadline := time.Now().Add(5 * time.Second)
	var healed *gateSweeperRunner
	for time.Now().Before(deadline) {
		if cur := currentGateSweeperRunner(); cur != nil && cur != first {
			healed = cur
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if healed == nil {
		t.Fatal("tick 里 panic 之后清扫器没有复活：清扫从此静默停摆，台账里的禁言成员再没人处理")
	}
	// 正证据：出事那台自己退出了。done 关闭才证明崩溃真发生在 tick 体内；否则"复活"
	// 可能只是别处又 Start 了一台，而原协程还活着 ⇒ 两条清扫流对同一批台账动手。
	select {
	case <-first.done:
	case <-time.After(2 * time.Second):
		t.Fatal("制造 panic 的那台清扫器没退出：done 未关闭＝tick 里根本没崩，复活判据是空的")
	}
	// 复活的那台沿用原节拍与原 worker，否则自愈等于换了个身份去抢租约
	if healed.workerID != "worker-panic" || healed.interval != 10*time.Millisecond {
		t.Fatalf("复活实例应沿用原 worker 与节拍, 实际 worker=%s interval=%v", healed.workerID, healed.interval)
	}
	drainGateSweeper(t, 5)
}

// 关停窗口里的 panic 不许复活。
//
// 走 ticker 判不了这一条——它要 panic 恰好落在 cancel 之前，那是运气。所以直接调
// restartAfterPanic：这台实例已经退出，StopGateSweeper 等着它的 done、随后还要交回租约，
// 此时复活出一台新实例＝"进程已经停了却还在踢人"，而且新协程用的还是同一个 workerID，
// 租约会被立刻抢回去。
func TestSweeperPanicRestartSkippedDuringShutdown(t *testing.T) {
	stub := &r65GateStub{}
	svc, _, _ := r65SetupGate(t, stub)
	t.Cleanup(func() { drainGateSweeper(t, 5) })

	// 摘掉 leaseRepo：这一格不判租约，而节拍是一小时、根本不会打拍，
	// 留着 leaseRepo 只会让收尾的 Stop 去 Release 一张本格没建过的表（一行无害的告警，
	// 但用例的噪声就是下一位用来找红因的信号）。
	noLease := &TelegramGateService{gateRepo: svc.gateRepo, memberRepo: svc.memberRepo}
	startGateSweeperAt(noLease, "worker-shutdown", time.Hour)
	r := currentGateSweeperRunner()
	if r == nil {
		t.Fatal("清扫器没登记起来")
	}
	if got := currentGateSweeperWorkerID(); got != "worker-shutdown" {
		t.Fatalf("登记的是别的实例（上一格没清干净）, 实际=%q", got)
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	r.restartAfterPanic(shutdownCtx)

	if currentGateSweeperRunner() != r {
		t.Fatal("关停窗口里 panic 之后又复活了一台：Stop 已准备交回租约，新协程会立刻把它抢回去继续处置真人")
	}
	StopGateSweeper(context.Background())
	if got := currentGateSweeperRunner(); got != nil {
		t.Fatalf("正常停机后不该留有登记, worker=%s", got.workerID)
	}
}

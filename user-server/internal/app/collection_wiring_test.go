// collection_wiring_test.go T-P7-03：催收任务装配层的实跑用例。
//
// 为什么装配层必须单独设测（与本文件每一格的对象）：service 包那一族用例是自己 new
// 假件、自己跑 RunOnce，它们**看不见**"生产路径上有没有人把五条依赖接齐"。
// 少装一条的症状不是报错而是永久不跑（Available 为假 ⇒ Start 直接退出），
// 而"逾期单没人催"在日志之外没有任何读数 —— 判定 A 那一族病灶的原文形状。
package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// db 为 nil 时不装配，且**清掉全局那一份**：Init 可被重复调用（测试与灰度重启都是真实路径），
// 只"什么都不做"会让端点继续对上一份实例回读数，而日志同时写着"未装配"。
func TestInitCollectionRuntime_NilDBClearsRuntime(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "shadow")
	t.Setenv(service.CollectionJobIntervalEnv, "1h")
	if j := InitCollectionRuntime(db); j == nil {
		t.Fatal("shadow + 真句柄应装配出实例")
	}
	if !GetCollectionSnapshot(context.Background()).Assembled {
		t.Fatal("前置不成立：第一份实例没装上，后面的断言等于没测")
	}

	if j := InitCollectionRuntime(nil); j != nil {
		t.Errorf("db=nil 不应装配，got mode=%s", j.Mode())
	}
	snap := GetCollectionSnapshot(context.Background())
	if snap.Assembled {
		t.Errorf("db=nil 之后快照仍报已装配 ⇒ 端点会对着一句\"未装配\"的告警继续回旧读数")
	}
	if snap.Running {
		t.Error("db=nil 之后快照仍报运行中")
	}
}

// 默认档（off）：实例照装、协程不起。
//
// Available 必须为真 —— 这一格是整张卡最要紧的断言：off 只该关掉"跑不跑"，
// 不该顺手把装配做成缺件。若装配点少接一条依赖，off 档下这条断言会当场红，
// 而不是等到某人把旗子拧到 shadow 那天才发现"跑了但五条依赖缺三条"。
func TestInitCollectionRuntime_OffModeAssemblesWithoutStarting(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")
	j := InitCollectionRuntime(db)
	if j == nil {
		t.Fatal("off 档也要返回实例（观测面要知道当前配的是哪一档）")
	}
	if !j.Available() {
		t.Error("Available=false ⇒ 五条依赖没接齐，开旗那天催收会静默不跑")
	}
	if j.Running() {
		t.Error("off 档不应起协程")
	}
	if j.Mode() != service.RecoveryWorkerModeOff {
		t.Errorf("Mode=%s，期望 off", j.Mode())
	}

	snap := GetCollectionSnapshot(context.Background())
	if !snap.Assembled || snap.Mode != "off" || snap.Running {
		t.Errorf("快照=%+v，期望 assembled=true mode=off running=false", snap)
	}
	if !snap.Available {
		t.Error("快照没把 Available 报出来 ⇒ 运维在端点上分不清\"没开\"与\"开了但缺件\"")
	}
	if snap.FlagEnv != service.CollectionJobFlagEnv {
		t.Errorf("快照旗子名=%q，期望 %s（端点回显必须与实读变量同源）", snap.FlagEnv, service.CollectionJobFlagEnv)
	}
}

// shadow 档真的起协程，Stop 幂等（优雅关停会调两次）。
func TestInitCollectionRuntime_ShadowModeStartsAndStops(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "shadow")
	t.Setenv(service.CollectionJobIntervalEnv, "1h")
	j := InitCollectionRuntime(db)
	if j == nil {
		t.Fatal("shadow 档应装配")
	}
	if !j.Running() {
		t.Error("shadow 档应已起协程（首轮要等到间隔之后，不产生外发）")
	}
	j.Stop(context.Background())
	j.Stop(context.Background())
	if j.Running() {
		t.Error("Stop 之后不应仍在运行")
	}
}

// 重复装配必须把上一份的协程停掉。
//
// 不是防御性洁癖：多起的每一台都会各自扫一遍同一批逾期单。两把频控窗都在同一个
// Redis 里的确会拦住第二条消息，但 enforce 档下"谁抢到"变成协程调度顺序，
// 而报告里会出现一整轮 reminders_held —— 看起来像"这个客户刚被催过"，
// 其实是本进程自己在跟自己抢。
func TestInitCollectionRuntime_RepeatedInitStopsPreviousInstance(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "shadow")
	t.Setenv(service.CollectionJobIntervalEnv, "1h")
	first := InitCollectionRuntime(db)
	second := InitCollectionRuntime(db)
	if first == second {
		t.Fatal("前置不成立：两次装配返回了同一个实例，本格的断言没有对象")
	}
	if first.Running() {
		t.Error("第二次装配后，上一份实例的协程仍在跑 ⇒ 同一批逾期单被两台各自扫一遍")
	}
	if !second.Running() {
		t.Error("第二份实例应处于运行中")
	}
	second.Stop(context.Background())
}

// 快照里的"跑没跑""哪一档"必须读自实例，而不是就地再解析一次 env。
//
// 这一格盯的刀口是：把 GetCollectionSnapshot 写成 `parseRecoveryWorkerMode(os.Getenv(...))`
// 之后，端点在装配好的进程里照样回 shadow/running=false 的自洽读数（env 就是 shadow），
// 而真实那台可能已经被 Stop 或压根没启起来。两处各读一次 env 迟早会各说各话。
func TestGetCollectionSnapshot_ReadsTheMountedInstanceNotTheEnv(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "shadow")
	t.Setenv(service.CollectionJobIntervalEnv, "1h")
	j := InitCollectionRuntime(db)
	if !GetCollectionSnapshot(context.Background()).Running {
		t.Fatal("前置不成立：shadow 档下快照没报运行中")
	}

	j.Stop(context.Background())
	snap := GetCollectionSnapshot(context.Background())
	if snap.Running {
		t.Errorf("实例已停而快照仍报 running=true ⇒ 读数来自 env 不是实例（env 仍是 %s）",
			service.CollectionJobFlagEnv)
	}
	if snap.Mode != "shadow" {
		t.Errorf("Mode=%q，实例那一份是 shadow", snap.Mode)
	}

	// 换成 off 档重新装配：读数必须跟着实例换，env 全程没改。
	t.Setenv(service.CollectionJobFlagEnv, "off")
	InitCollectionRuntime(db)
	if got := GetCollectionSnapshot(context.Background()).Mode; got != "off" {
		t.Errorf("重新装配后快照 Mode=%q，期望 off ⇒ mode 读数没跟着实例走", got)
	}

	// 上面那一段里 env 与实例恰好同值（off/off），拿它当判据的话，把快照写成
	// `CollectionJobModeFromEnv()` 也照样绿 —— 而"读数来自 env"正是本条要杀的形状。
	// 所以这里造一次**故意不一致**：env 拧到 enforce，实例仍是上一台 off。
	t.Setenv(service.CollectionJobFlagEnv, "enforce")
	if got := GetCollectionSnapshot(context.Background()).Mode; got != "off" {
		t.Errorf("env=enforce 而实例=off 时快照 Mode=%q ⇒ 这一格读的是环境变量，"+
			"不是那台真的在跑的实例（两处各解析一次迟早各说各话）", got)
	}
}

// 累计读数必须来自那台活着的实例（与挽回 worker / 草稿清扫同一学费：
// LastReport 一轮覆盖一轮，跨轮要看的只有累计那两个数，而端点漏抄一行就永久回 0）。
func TestGetCollectionSnapshot_CarriesCumulativeTotals(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")
	j := InitCollectionRuntime(db)
	snap := GetCollectionSnapshot(context.Background())
	if snap.RemindedTotal != j.RemindedTotal() || snap.EscalatedTotal != j.EscalatedTotal() {
		t.Errorf("累计读数 reminded=%d escalated=%d，实例上是 %d/%d",
			snap.RemindedTotal, snap.EscalatedTotal, j.RemindedTotal(), j.EscalatedTotal())
	}

	// off 档跑一轮：只落"这一轮没跑业务"的读数，两处数字都不许动。
	if _, err := j.RunOnce(context.Background()); err != nil {
		t.Fatalf("off 档 RunOnce 应回 nil 错误（缺件与关档是两件事），实际 %v", err)
	}
	snap2 := GetCollectionSnapshot(context.Background())
	if snap2.Last == nil {
		t.Error("跑过一轮之后快照没有 Last ⇒ 端点读不到最近一轮")
	} else if snap2.Last.Mode != "off" {
		t.Errorf("Last.Mode=%q，期望 off（快照串到了别的实例）", snap2.Last.Mode)
	}
	if snap2.RemindedTotal != 0 || snap2.EscalatedTotal != 0 {
		t.Errorf("off 档跑一轮之后累计读数变成了 %d/%d，应为 0/0",
			snap2.RemindedTotal, snap2.EscalatedTotal)
	}
}

// 催收口径必须从端点读得出来，而不是只有读代码的人才知道。
//
// 盯的刀口：把这四个数写成端点上的另一份字面量（或者干脆不报）。宽限期/升级线是
// 财务口径、刻意不走 env，那它们就更需要一个"现网此刻是哪两个数"的读口 ——
// 否则运维只能从"这张单为什么还没催"倒推口径，而那就是拿客户当探针。
func TestGetCollectionSnapshot_CarriesTheDocumentedCalibre(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")
	// 两把子参数显式清空：不关宿主机环境的话，下面那四格字面量锁量的就是别人的部署，
	// 而不是"装配点把默认值抄进了快照"这句话。
	t.Setenv(service.CollectionJobBatchEnv, "")
	t.Setenv(service.CollectionJobIntervalEnv, "")
	j := InitCollectionRuntime(db)
	snap := GetCollectionSnapshot(context.Background())

	// 七格**字面量**锁。
	//
	// 为什么这一格不许借用常量（与 service 侧 TestCollectionDocumentedContractSurfaceIsExact
	// 是两份不同的锁）：那边锁的是"常量的值就是文档里那几个"，这边锁的是
	// "装配点把哪一个常量抄进了哪一个字段"。把 GraceDays 与 EscalateAfterDays 两格对调、
	// 或把 FlagEnv 抄成 batch 那把变量名，在那一族的比较里全都是自算的 ⇒ 全绿，
	// 而端点会指着运维说"去改 LTC_COLLECTION_JOB_BATCH 吧" —— 改了什么也不会发生。
	for _, tc := range []struct {
		name string
		got  any
		want any
	}{
		{"宽限期（天）", snap.GraceDays, 3},
		{"升级线（天）", snap.EscalateAfterDays, 14},
		{"提醒窗", snap.RemindWindow, "168h0m0s"},
		{"升级窗", snap.EscalateWindow, "720h0m0s"},
		{"主开关变量名", snap.FlagEnv, "FF_LTC_COLLECTION_JOB"},
		{"封顶变量名", snap.BatchEnv, "LTC_COLLECTION_JOB_BATCH"},
		{"间隔变量名", snap.IntervalEnv, "LTC_COLLECTION_JOB_INTERVAL"},
		{"默认单轮封顶", snap.Batch, 20},
		{"默认轮询间隔", snap.Interval, "6h0m0s"},
	} {
		if tc.got != tc.want {
			t.Errorf("快照里 %s = %v，期望 %v（端点回显的必须是这一格本身，抄错字段与抄错值同样致命）",
				tc.name, tc.got, tc.want)
		}
	}

	if snap.GraceDays != service.CollectionGraceDays {
		t.Errorf("宽限期=%d，期望常量 %d", snap.GraceDays, service.CollectionGraceDays)
	}
	if snap.EscalateAfterDays != service.CollectionEscalateAfterDays {
		t.Errorf("升级线=%d，期望常量 %d", snap.EscalateAfterDays, service.CollectionEscalateAfterDays)
	}
	if snap.RemindWindow != service.CollectionReminderWindow.String() {
		t.Errorf("提醒窗=%q，期望 %s", snap.RemindWindow, service.CollectionReminderWindow)
	}
	if snap.EscalateWindow != service.CollectionEscalateWindow.String() {
		t.Errorf("升级窗=%q，期望 %s", snap.EscalateWindow, service.CollectionEscalateWindow)
	}
	if snap.BatchEnv != service.CollectionJobBatchEnv || snap.IntervalEnv != service.CollectionJobIntervalEnv {
		t.Errorf("子参数名 batch=%q interval=%q，与实读变量不同源", snap.BatchEnv, snap.IntervalEnv)
	}
	if snap.Batch <= 0 || snap.Interval == "" {
		t.Errorf("节奏读数缺失 batch=%d interval=%q ⇒ 端点答不出\"多久跑一轮、一轮几条\"", snap.Batch, snap.Interval)
	}
	// 节奏两格还要**等于那台实例上的数**：只判"非空非零"的话，端点回显一个从没生效过的
	// 默认值也照样绿，而 `Start` 会把过低的间隔抬到下限 —— 现网真正在用的就是被改过的那一份。
	if snap.Batch != j.Batch() || snap.Interval != j.Interval().String() {
		t.Errorf("节奏读数 batch=%d interval=%q，实例上是 %d/%s ⇒ 端点回显的不是生效值",
			snap.Batch, snap.Interval, j.Batch(), j.Interval())
	}
}

// collection 阶段的开关状态必须在同一份快照里（第二把锁）。
//
// 为什么不能只报旗子：旗子三档全开而 ltc.config 的 collection 阶段没开，一条都不催；
// 反过来阶段开了而旗子 off，同样一条都不催。两把锁的读数分在两个端点上，
// 运维就要在两个页面之间自己对一遍 —— 这一族"看着都开了其实没开"的错位是明知道会发生的。
func TestGetCollectionSnapshot_CarriesStageGateState(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")
	InitCollectionRuntime(db)
	snap := GetCollectionSnapshot(context.Background())

	on, reason := service.GlobalLTCConfig().StageActive(context.Background(), service.LTCStageCollection)
	if snap.StageOn != on {
		t.Errorf("快照 StageOn=%t，阶段实读 %t（reason=%q）", snap.StageOn, on, reason)
	}
	if snap.StageReason != reason {
		t.Errorf("快照 StageReason=%q，实读 %q", snap.StageReason, reason)
	}
	if !on && snap.StageReason == "" {
		t.Error("阶段没开却没给出原因 ⇒ 读数退化成一个布尔，排障时等于没说")
	}
}

// 外发闸门必须装在这一条腿的触达服务上，而且两种"挂没挂"都要读得出来。
//
// 与挽回 worker 同一判据：cron 这条路径是"非工具外发"，闸门少装一边就是留一条盲区。
// 快照里的 ReachGated 就是用来发现"只装了一边"的那个数字（装配点计数在 reach-gate 端点上，
// 但那一处看不到的是"催收这一条到底挂没挂"）。
//
// 两格都**判确切布尔**而不判"没挂上就得有原因"那种复合式：后者在"挂了却报没挂"这一半上
// 没有牙 —— 挂上门时 note 非空是合法的，于是把 ReachGated 取反的写法能整格活下来。
// 裁决来源（approvalCheckerRef）在两种情形下分别是 nil 与非 nil，那是这一档唯一的开关，
// 所以这里显式把它摆到位置上，而不是依赖同包别的用例留下的状态（包级变量没有锁，
// 靠 save/restore 成对还原 —— 同 restoreApprovalState / restoreReachGateState 的理由）。
func TestInitCollectionRuntime_AttachesReachGate(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })
	restoreReachGateState(t)
	restoreApprovalState(t)

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")
	t.Setenv(ReachGateFlagEnv, "shadow")

	// ① 旗子在、裁决来源没接线 ⇒ 闸门**拒绝装门**，快照必须把这句话说出来。
	approvalCheckerRef = nil
	InitCollectionRuntime(db)
	snap := GetCollectionSnapshot(context.Background())
	if !snap.ReachGateChecked {
		t.Fatal("快照没报出闸门装配结果 ⇒ 这条腿挂没挂门在端点上是看不见的")
	}
	if snap.ReachGated {
		t.Error("没有裁决来源却报 ReachGated=true ⇒ 运维会以为这条外发受审批约束")
	}
	if !strings.Contains(snap.ReachGateNote, ApprovalGateFlagEnv) {
		t.Errorf("没装门却没点名缺哪把旗子：%q ⇒ 退化成\"就是没挂\"，看不出是运营选的还是配置顺序错",
			snap.ReachGateNote)
	}

	// ② 裁决来源接上 ⇒ 同一条装配路径必须报出借了门。
	approvalCheckerRef = approval.NewWhiteList(nil, nil)
	InitCollectionRuntime(db)
	if got := GetCollectionSnapshot(context.Background()); !got.ReachGated {
		t.Errorf("旗子=shadow 且裁决来源已接线，这一条腿却没挂门（note=%q）", got.ReachGateNote)
	}
}

// TestInitCollectionRuntime_ConcurrentReinitAndSnapshot 重复装配与端点读并发时不许有数据竞争。
//
// 装配层注释里那句"Init 可被重复调用（测试与灰度重启都是真实路径），重复调用时端点的读协程
// 已经在跑 ⇒ 不能靠约定先写后读"到目前为止只有锁的形状、没有证据。这一格就是那份证据：
// 八个协程一半写一半读，`-race` 下必须零竞争。
//
// 它同时是**闸门那三份全局**（reachGateModeValue / reachDecisions / reachAttached）的唯一证据：
// 第一次跑这一格就把它们报成了竞争 —— 那三份的注释写着"写入发生在 router.Setup 之前⇒读侧
// 无需加锁"，而催收这条腿的重复装配恰好把写发生了服务之后。读侧也一并跑到 GetReachGateSnapshot，
// 因为运维端点 `/agent/tools/reach-gate` 在装配之后仍每请求读一次。
//
// 它的牙齿靠反向测证明（不在电池里 —— 电池的 app runner 不带 `-race`，摘锁的单行 getter
// 还会被内联）：把 `collectionMu` 或 `reachGateMu` 的任一侧摘掉，这一格必须在 `-race` 下
// 报出竞争，实测见执行记录。收尾再断言"最后一位写者赢且读得动"，否则这一格只证明没崩、不证明状态可读。
func TestInitCollectionRuntime_ConcurrentReinitAndSnapshot(t *testing.T) {
	t.Cleanup(func() { InitCollectionRuntime(nil) })

	db := testutil.NewTestDB(t)
	t.Setenv(service.CollectionJobFlagEnv, "off")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch {
			case i%2 == 0:
				InitCollectionRuntime(db)
			default:
				// 一次读里不许看到劈开的事实：未装配那一臂按文档回读 env 并给出提示，
				// 装配那一臂必须带着实例自己的模式与空提示。两臂各自成立，混起来才是洞。
				snap := GetCollectionSnapshot(context.Background())
				if snap.Assembled && snap.UnassembledHint != "" {
					t.Errorf("assembled=true 的快照带着未装配提示 %q ⇒ 读到了两份实例拼出来的一格", snap.UnassembledHint)
				}
				if !snap.Assembled && snap.UnassembledHint == "" {
					t.Error("未装配的快照没给 UnassembledHint ⇒ 运维端点上\"没装\"与\"装了但缺件\"同形")
				}
				// 读侧还要碰到闸门那三份包级全局：重复装配会写它们，而 /agent/tools/reach-gate
				// 在装配之后仍然每请求读一次。这里不跨字段断言（attached>0 与 wired=false
				// 是合法的先后组合），判据是 `-race` 报不报竞争。
				if gate, _ := GetReachGateSnapshot(); gate.Mode == "" {
					t.Error("闸门快照 mode 为空 ⇒ 读到了一份还没写完的全局")
				}
			}
		}(i)
	}
	wg.Wait()

	InitCollectionRuntime(db)
	snap := GetCollectionSnapshot(context.Background())
	if !snap.Assembled || snap.Mode != "off" {
		t.Errorf("并发阶段之后 assembled=%v mode=%q，期望 true/off ⇒ 全局那份已经不可读",
			snap.Assembled, snap.Mode)
	}
	if !snap.Available {
		t.Error("并发阶段之后 Available=false ⇒ 五把依赖在重复装配中丢了一把")
	}
}

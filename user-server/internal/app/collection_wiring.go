// collection_wiring.go 催收腿的装配层（新规划任务清单 T-P7-03）。
//
// 一句话职责：把"有没有 DB 句柄"翻译成一台会自己跑的催收任务，并把它的状态登记成
// 一份可读的快照 —— 在此之前 `CollectionJob` 全仓非测试构造点为 0，那正是判定 A 的
// 原文形状（service 侧 27 条用例全绿，生产路径上没人把它接起来）。
//
// 为什么这一卡**必须**装而不是停在"服务层写完了"：催收是回款域里第一条会**主动打扰客户**
// 的腿。它不装，`bills` 里那些越过账期的行就永远停在 open，"逾期"这件事在系统里
// 只剩一句注释；升级待办也不会有人投。两种失效都不报错、不崩溃，只在客户那边表现为
// "欠了三个月没人催"，而在我们这边表现为"库里一切正常"。
//
// 为什么本竖不加第四把旗子：`FF_LTC_COLLECTION_JOB` 本身就是那把旗子（off|shadow|enforce，
// 默认 off ⇒ Start 直接不起协程），它守的是"这条腿今天跑不跑"，比"装不装"更细一档。
// 装配点无条件构造并登记，为的是 off 档下端点仍然答得出"现在配的是哪一档、
// 五条依赖齐不齐"—— 关档与缺件必须能在一个读面上分开（见快照用例）。
//
// 五把依赖的接法，逐条都有理由：
//   - 账单/商机两把仓储走**显式句柄**构造（全局句柄版刻意不产出），与账单/回款两条腿同一口径：
//     装配顺序决定的隐式句柄是本仓最难查的一类错因；
//   - 触达服务在这里**单独构造一份**（与 HTTP 侧那份互不影响），并且必须过 AttachReachGate：
//     cron 这条路径是"非工具外发"的第二条，闸门少装一边就是留一条盲区；
//   - 待办服务同样单独构造：`HumanTaskService` 只有 repo + 配置读取口两格，没有实例内状态，
//     幂等判据（"这一 subject 有没有开放待办"）在库里，所以各建一份不产生裂脑；
//     它换来的是"不依赖 InitHumanTaskRuntime 的装配顺序" —— 待办底座没装的那次启动，
//     催收仍然能投出待办（行是持久的，读侧装配好就能看见），而不是静默少投；
//   - 阶段开关**必须**用全局那份：ltc.config 是运营在 HTTP 侧改的，读全局才可能在下一轮
//     看到改动；自己 new 一份等于把"改配置不用重启"这半句做成了"要重启"。
//
// 与 T-P1-07（挽回 worker）的顺序关系：两把装配点都排在 router.Setup **之后**，
// 理由同一条 —— AttachReachGate 要读 W-1 的 checker，而那个 checker 是
// router.Setup 里的 InitGlobalToolExecutor 才建的。排在前面不 panic、不报错，
// 只留一条"依赖未成立 ⇒ 不装门"的告警，也就是这条外发路径静默地不受审批约束。
package app

import (
	"context"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// CollectionRuntime 一次装配留下的全部状态：那台任务，加上"外发闸门挂没挂上"这个事实。
//
// 闸门结果必须存而不是每次重算：AttachReachGate 是一次性装配动作（它改的是触达服务身上的
// 钩子），而快照是每次请求读一次。重算会得到一个"每次读都自增装配计数"的假读数。
type CollectionRuntime struct {
	job        *service.CollectionJob
	reachGated bool
	reachNote  string
}

// 锁口径与草稿运行时一致：写入发生在 main 里（HTTP 尚未开始收流量），但 Init 可被重复调用
// （测试与灰度重启都是真实路径），重复调用时端点的读协程已经在跑 ⇒ 不能靠"约定先写后读"。
var (
	collectionMu      sync.RWMutex
	collectionRuntime *CollectionRuntime
)

// InitCollectionRuntime 装配催收任务、起协程并登记为全局那一份，可重复调用（后写覆盖）。
//
// 返回实例只给调用方做 Stop 用（main 里 defer）；观测面一律从全局读，不在 main 里再抄一份，
// 免得"main 持有的那台"和"端点读的那台"有一天不是同一台。
//
// db == nil 时必须把全局**清空并停掉上一份**：不清的话，端点会继续对着上一份实例回
// "running=true、催过 N 条"，而日志同时写着"未装配"。
func InitCollectionRuntime(db *gorm.DB) *service.CollectionJob {
	collectionMu.Lock()
	prev := collectionRuntime
	collectionRuntime = nil
	collectionMu.Unlock()
	if prev != nil {
		prev.job.Stop(context.Background())
	}

	if db == nil {
		logger.Warnf("[collection] ⚠️ 无 DB 句柄 ⇒ 催收腿不装配：逾期应收不会被自动提醒、也不会升级人工" +
			"（上一份实例已停）")
		return nil
	}

	// 变量名带 collection 前缀是给台账门看的：项15 用 `AttachReachGate(reach)` 锁定
	// 挽回队列那一个装配点，这边若同名，两个装配点在 grep 里就分不开（"只装了一边"
	// 正是这一族最容易发生又最难发现的漏法）。
	collectionReach := service.NewProactiveReachService(db, nil)
	service.BindProactiveReachSenders(collectionReach, db)
	gated := AttachReachGate(collectionReach)
	if !gated {
		LogReachGateSkippedAssemblyPoint("app.InitCollectionRuntime")
	}

	job := service.NewCollectionJob(
		repository.NewBillRepositoryWithDB(db),
		repository.NewOpportunityRepositoryWithDB(db),
		collectionReach,
		service.NewHumanTaskService(
			repository.NewHumanTaskRepositoryWithDB(db),
			service.GlobalConfigParam(),
		),
		service.GlobalLTCConfig(),
	)

	rt := &CollectionRuntime{job: job, reachGated: gated, reachNote: collectionGateNote(gated)}
	job.Start(context.Background())

	collectionMu.Lock()
	collectionRuntime = rt
	collectionMu.Unlock()

	if !job.Available() {
		// 半装配比不装配更坏：不装配是"这条腿不存在"，半装配是"协程起了、每轮在第一格退出"。
		// 这里必须点名，否则排障时会从"旗子开了为什么没动静"一路猜到库上去。
		logger.Warnf("[collection] ❌ 催收任务已登记但 Available() 为假 ⇒ 每轮直接退出，检查五把依赖（mode=%s）",
			job.Mode())
		return job
	}
	logger.Infof("[collection] ✅ 催收腿已装配：mode=%s 间隔=%s 单轮上限=%d 宽限期=%d天 升级线=%d天 "+
		"（开关 %s；外发只走触达这一条出口，频控=触达冷却 + 单张应收提醒窗 %s）",
		job.Mode(), job.Interval(), job.Batch(), service.CollectionGraceDays,
		service.CollectionEscalateAfterDays, service.CollectionJobFlagEnv, service.CollectionReminderWindow)
	if rt.reachNote != "" {
		logger.Warnf("[collection] ⚠️ %s", rt.reachNote)
	}
	return job
}

// collectionGateNote 把"闸门挂上/没挂"翻译成一句有因有话的读数。
//
// 没挂上的两种成因给的行动不一样（off 是运营选的，依赖未满足是配置顺序错的），
// 所以不能塌成一句"未装门"。这一句同时进快照和装配日志：只在日志里的话，
// 事后查"那三个月的外发受不受审批约束"就只能翻启动日志，而启动日志会被重启冲掉。
func collectionGateNote(gated bool) string {
	snap, _ := GetReachGateSnapshot()
	if gated {
		return ReachRolloutCouplingNote(snap)
	}
	if snap.DependencyUnmet {
		return "外发闸门**没挂上**：" + ReachGateFlagEnv + "=" + snap.Mode + " 但 " + ApprovalGateFlagEnv +
			" 未接线 ⇒ 催收提醒这条外发路径不受审批约束。先开依赖旗子再重启"
	}
	return "外发闸门未挂（" + ReachGateFlagEnv + "=" + snap.Mode + "，默认 off）⇒ 催收提醒只受触达自身的" +
		"退订与频控约束，不受 W-1 审批白名单约束"
}

// GetCollectionSnapshot 读当前催收运行时状态。端点在 off 档与未装配时也要能答上话，
// 所以这里永不返回 nil，只返回 Assembled=false 的快照。
//
// mode/running/batch/interval 一律读**实例**而不是就地再解析一次 env：那份解析在
// 装配期已经做过一次（还可能被下限夹取修正过），再读一次就会在两处各说各话。
func GetCollectionSnapshot(ctx context.Context) CollectionSnapshot {
	collectionMu.RLock()
	rt := collectionRuntime
	collectionMu.RUnlock()

	snap := CollectionSnapshot{
		FlagEnv:           service.CollectionJobFlagEnv,
		BatchEnv:          service.CollectionJobBatchEnv,
		IntervalEnv:       service.CollectionJobIntervalEnv,
		GraceDays:         service.CollectionGraceDays,
		EscalateAfterDays: service.CollectionEscalateAfterDays,
	}
	// 两把窗与两个口径常量同源回显：它们在 service 里是不可改的财务口径，
	// 而端点是唯一能让运维"看见现网此刻是哪两个数"的地方。
	snap.RemindWindow = service.CollectionReminderWindow.String()
	snap.EscalateWindow = service.CollectionEscalateWindow.String()

	on, reason := service.GlobalLTCConfig().StageActive(ctx, service.LTCStageCollection)
	snap.StageOn, snap.StageReason = on, reason

	if rt == nil {
		// 未装配时只能回显 env 上此刻的值，并且必须说清这是"没装配所以只能读 env"，
		// 否则这一格与"装配了、跑着、恰好也是这个档"在响应里长得一模一样。
		snap.Mode = string(service.CollectionJobModeFromEnv())
		snap.UnassembledHint = "本进程未装配催收腿（无 DB 句柄或启动路径没调装配）⇒ mode 读自当前环境变量，" +
			"不是任何一台在跑的实例"
		return snap
	}

	snap.Assembled = true
	snap.Available = rt.job.Available()
	snap.Mode = string(rt.job.Mode())
	snap.Running = rt.job.Running()
	snap.Batch = rt.job.Batch()
	snap.Interval = rt.job.Interval().String()
	snap.RemindedTotal = rt.job.RemindedTotal()
	snap.EscalatedTotal = rt.job.EscalatedTotal()
	snap.Last = rt.job.LastReport()
	snap.ReachGateChecked = true
	snap.ReachGated = rt.reachGated
	snap.ReachGateNote = rt.reachNote
	return snap
}

// CollectionSnapshot 催收腿的观测面（app 层快照，router 只负责渲染）。
//
// 为什么一次读这么多件：端点要回答的不是"有没有催收任务"，而是"这一进程里这条腿现在
// 到底是什么状态、为什么没动"。少任何一格，运维就得靠"猜哪一把锁没开"来排障，
// 而这一条腿的失败形态恰恰全是静默的。
type CollectionSnapshot struct {
	Assembled bool   `json:"assembled"`
	Mode      string `json:"mode"`
	Running   bool   `json:"running"`
	Available bool   `json:"available"`

	FlagEnv     string `json:"flag_env"`
	BatchEnv    string `json:"batch_env"`
	IntervalEnv string `json:"interval_env"`
	Batch       int    `json:"batch"`
	Interval    string `json:"interval"`

	// 口径四格：宽限期与升级线不走 env（能在启动参数里改掉的逾期定义 = 每个副本各有一套），
	// 所以它们必须从这个端点读得出来。
	GraceDays         int    `json:"grace_days"`
	EscalateAfterDays int    `json:"escalate_after_days"`
	RemindWindow      string `json:"remind_window"`
	EscalateWindow    string `json:"escalate_window"`

	// 第二把锁：ltc.config 的 collection 阶段。旗子三档全开而这一格关着，一条都不催。
	StageOn     bool   `json:"stage_on"`
	StageReason string `json:"stage_reason,omitempty"`

	RemindedTotal  int64                          `json:"reminded_total"`
	EscalatedTotal int64                          `json:"escalated_total"`
	Last           *service.CollectionRoundReport `json:"last,omitempty"`

	ReachGateChecked bool   `json:"reach_gate_checked"`
	ReachGated       bool   `json:"reach_gated"`
	ReachGateNote    string `json:"reach_gate_note,omitempty"`

	UnassembledHint string `json:"unassembled_hint,omitempty"`
}

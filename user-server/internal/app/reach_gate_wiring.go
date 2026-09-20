// reach_gate_wiring.go T-P3-07：把 W-1 的审批裁决接到"非工具外发出口"上。
//
// 一句话职责：给 ProactiveReachService 装一把发送前的闸门，让 cron / 直接 API 这些
// **不经过 executor 工具链**的外发路径也被判一次 —— 在此之前审批门只存在于
// `ApprovalGateDecorator`（工具建链点），`ReachByCustomer` 是外发的唯一出口却是闸门的盲区。
//
// 为什么门装在 service 内部而不是每个调用方（本文件因此只是"装门的人"，不是"判门的人"）：
// 调用方今天有 HTTP 一个装配点 + cron 一个装配点（SOP 节点今天不经过本服务：它只写会话消息
// 与商家 WS；将来若有节点走外发，也是这一类）。每加一个调用方就要记得在这里多接一行，
// 等于把 G13 这个盲区复制一遍。
// service 侧的钩子（`SetPreSendApprovalChecker`）因此是唯一的判点，本文件是唯一的装法。
//
// 三把旗子的关系（少一把就装不上，必须写清楚）：
//   - `FF_LTC_APPROVAL_GATE`（W-1）：裁决来源（白名单 checker + 计数器）存不存在；
//   - `FF_LTC_REACH_GATE`（本文件）：外发出口挂不挂这道门、以哪种模式挂；
//   - `approval.FlagKey`（内层）：白名单生不生效。
//
// W-1 没接线时本文件**拒绝装门**（`DependencyUnmet`）。这不是保守，是防"假闸门"：
// 没有裁决来源的门只能恒放或恒拒，两种都长得像在拦 —— T-P1-05 的教训正是如此。
//
// 刹车与 W-1 同源（复用 `approvalDenialBlocks`，不另写一份）：block 态但白名单旗子没开时
// `disabled_by_flag` 不拦。两条路径共用一个判据，才不会出现在"工具侧放行、外发侧拦下"
// 这种两边都自洽、合起来互相矛盾的状态。
//
// 五层归属：判定与留痕的语义在 approval 包与 approval_wiring.go；"外发前问一次"在 service；
// 本文件只做装配（DI + 旗子 + 快照）。
package app

import (
	"context"
	"os"
	"strconv"
	"strings"

	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

const (
	// ReachGateFlagEnv 外发闸门开关。默认 off：不装门，外发路径与接线前逐字一致。
	ReachGateFlagEnv = "FF_LTC_REACH_GATE"
	// ReachApprovalToolKey 外发闸门在 W-1 白名单里的"入口名"。
	//
	// 借用 toolName 这一维而不是新造一张表：授权入口（ApprovalWhitelistMutate）、
	// 有效条目读数、过期语义全都复用 W-1 那份，两处各一份授权表迟早会出现
	// "这里批过、那里没批过"的裂脑。前缀 reach. 让它一眼看出不是工具名。
	ReachApprovalToolKey = "reach.proactive.send"
)

// reach 门的包级状态。与 approvalGateState 同一条装配前提：写入发生在 router.Setup() 里、
// HTTP 服务启动之前（cron 侧则在 main 把 InitRecoveryWorker 挪到 router.Setup 之后），
// 因此读侧无需加锁。reachAttached 计数是"几个装配点真的拿到了钩子"——
// 漏接一个装配点是这个功能最可能的失败形态，所以它必须是可观测的一个数字。
var (
	reachGateModeValue = string(approvalGateOff)
	reachDecisions     *approval.DecisionCounter
	reachAttached      int
)

// ReachGateSnapshot 外发闸门接线状态快照（值类型，运维端点直接序列化）。
//
// 刻意把"总白名单条数"和"reach 名下条数"两个数一起回显：block 态下运维要看的是
// "我现在放行了几个外发对象"，而白名单是两条路径共用的，只看总数会把工具侧的授权
// 误读成外发侧也已就绪（⇒ 一开旗子所有冷触达都被拒，却没人知道为什么）。
//
// DependencyUnmet 单独立一个字段：mode=block 而 wired=false 这个组合只有一种成因
// （W-1 没接线），若不点名，端点上看到的就是"旗子开了却没生效"这种查不出所以然的形状。
type ReachGateSnapshot struct {
	Mode                     string `json:"mode"`
	Wired                    bool   `json:"wired"`
	BlocksWhenDenied         bool   `json:"blocks_when_denied"`
	DependencyUnmet          bool   `json:"dependency_unmet"`
	GateFlagEnv              string `json:"gate_flag_env"`
	DependencyFlagEnv        string `json:"dependency_flag_env"`
	ReachToolKey             string `json:"reach_tool_key"`
	WhitelistFlagOn          bool   `json:"whitelist_flag_on"`
	WhitelistFlagEnv         string `json:"whitelist_flag_env"`
	WhitelistActiveEntries   int    `json:"whitelist_active_entries"`
	WhitelistEntriesForReach int    `json:"whitelist_entries_for_reach"`
	AttachedServices         int    `json:"attached_services"`
}

// parseReachGateMode 解析 FF_LTC_REACH_GATE。
//
// 与 parseApprovalGateMode 逐条同构（off|shadow|block，布尔真值降 shadow，认不出判 off），
// 但刻意不复用同一个函数：两者的告警说的是两件不同的后果 —— W-1 那句要提白名单内层旗子
// （它没开时 block 只剩无差别失败），这句要提"W-1 没接线时这里根本装不上"。
// 合成一个函数就得为两边各留一个措辞分支，读起来比这两段还长；且 AC④ 要求工具路径
// 的既有行为逐字不变，把它的解析器顺手改掉是超出本卡范围的改动。
func parseReachGateMode(raw string) approvalGateMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return approvalGateOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return approvalGateOff
	case "shadow", "observe", "watch", "log", "report":
		return approvalGateShadow
	case "block", "enforce", "active":
		return approvalGateBlock
	case "on", "yes", "y", "true", "1":
		logger.Warnf("[reach-gate] ⚠️ %s=%q 是布尔真值：语义不足以表达\"把客户的外发拒掉\"⇒ 按 shadow 处理。"+
			"要转阻断请显式写 block（白名单旗子 %s 也要开），可用值：off|shadow|block",
			ReachGateFlagEnv, raw, approval.FlagKey)
		return approvalGateShadow
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			logger.Warnf("[reach-gate] ⚠️ %s=%q 解析为布尔真值 ⇒ 按 shadow 处理（转阻断须显式写 block）",
				ReachGateFlagEnv, raw)
			return approvalGateShadow
		}
		return approvalGateOff
	}
	logger.Warnf("[reach-gate] %s=%q 无法识别 ⇒ 按 off 处理（外发闸门不装门）；可用值：off|shadow|block",
		ReachGateFlagEnv, raw)
	return approvalGateOff
}

// reachPreSendGate 是 service 侧钩子的实现：把 ReachSubject 翻译成一次 W-1 裁决。
//
// 判定键用 sub.Key（客户身份：one_id → customer_id → "渠道:收件人"），刻意不用
// sub.AccountID —— 短信/邮箱渠道的 accountID 在装配里恒为空，拿它当键得到的是一把
// 恒空的白名单，判了等于没判（AC② 要堵的就是 T-P1-05 那个形态）。
type reachPreSendGate struct {
	mode    approvalGateMode
	checker *approval.WhiteListApprovalChecker
}

// CheckReachPreSend 问一次 W-1，并把"这个拒绝在当下模式里拦不拦"折进返回值。
//
// 返回的是**闸门裁决**而不是"是否拒绝"：service 侧只看 allowed，reason 原样带出去，
// 由日志与计数器负责口径。刹车在这里生效而不是在 service 里，是因为刹车要看 mode，
// 而 mode 是装配期的事 —— 业务层不认识旗子。
func (g *reachPreSendGate) CheckReachPreSend(ctx context.Context, sub service.ReachSubject) (bool, string) {
	if g.checker == nil {
		// 与 W-1 的 blockApprovalChecker 同一口径：没有裁决来源就没有"拦"的依据，放行。
		// 正常装配路径到不了这里（AttachReachGate 先查过 approvalCheckerRef）。
		return true, ""
	}
	allowed, reason := g.checker.Verdict(ctx, ReachApprovalToolKey, sub.Key)
	if allowed {
		return true, reason
	}
	return !approvalDenialBlocks(g.mode, reason), reason
}

// AttachReachGate 给一个触达服务装上发送前闸门；返回是否装上。
//
// 三个"不装"的情形各有后果，所以分开处理、分开留痕：
//  1. 旗子 off（默认）⇒ 不装，且显式把钩子归零（同一个 service 被重复装配时，
//     上一轮的钩子不能留在身上）；
//  2. W-1 没接线 ⇒ 不装并告警。宁可不装，也不装一把没有裁决来源的门；
//  3. svc 为 nil ⇒ 不装（装配点写错了，让调用方的 nil 检查暴露它，而不是在这里吞掉）。
//
// 计数器只在真的装上门时才建：wired 的读数是 reachDecisions != nil，
// 若 off 态也建一个空计数器，端点就会把"没接线"显示成"接了但零流量"，这两件事必须能区分。
func AttachReachGate(svc *service.ProactiveReachService) bool {
	mode := parseReachGateMode(os.Getenv(ReachGateFlagEnv))
	reachGateModeValue = string(mode)

	if mode == approvalGateOff {
		if svc != nil {
			svc.SetPreSendApprovalChecker(nil)
		}
		reachDecisions = nil
		return false
	}
	if approvalCheckerRef == nil {
		logger.Warnf("[reach-gate] ⚠️ %s=%s 但 %s 未接线 ⇒ **不装门**：没有裁决来源的闸门只能恒放或恒拒，"+
			"两种都长得像在拦。先开 %s=shadow|block 再开这把",
			ReachGateFlagEnv, mode, ApprovalGateFlagEnv, ApprovalGateFlagEnv)
		reachDecisions = nil
		return false
	}
	if svc == nil {
		logger.Warnf("[reach-gate] ⚠️ %s=%s 但装配点传入的触达服务为 nil ⇒ 未装门（该装配点的外发不受闸门约束）",
			ReachGateFlagEnv, mode)
		return false
	}

	if reachDecisions == nil {
		reachDecisions = approval.NewDecisionCounter()
	}
	svc.SetPreSendApprovalChecker(&reachPreSendGate{mode: mode, checker: approvalCheckerRef})
	reachAttached++

	flagOn := featureflag.Get(approval.FlagKey).Bool()
	forReach := approvalCheckerRef.ActiveEntryCountFor(ReachApprovalToolKey)
	if mode == approvalGateShadow {
		logger.Infof("[reach-gate] ✅ 外发闸门已接线（第 %d 个装配点），模式=shadow：非工具外发路径只记 would_deny，不拦任何发送；"+
			"生效配置：reach_gate=%s 依赖 %s 白名单旗子 %s=%t reach 名下有效授权=%d",
			reachAttached, ReachGateFlagEnv, ApprovalGateFlagEnv, approval.FlagKey, flagOn, forReach)
		logger.Infof("[reach-gate] ⚠️ shadow 态：**cron 与直接 API 的外发照常出域**。转阻断前先看 /agent/tools/reach-gate 的 " +
			"would_deny 报告，并确认 reach 名下有效授权>0（授权只写进程内存、重启即空，撑不起阻断）")
		return true
	}

	logger.Infof("[reach-gate] 🚫 外发闸门已接线（第 %d 个装配点），模式=block：未获授权的对象在出口前被拒（ErrReachApprovalDenied），"+
		"零外发；生效配置：reach_gate=%s 白名单旗子 %s=%t reach 名下有效授权=%d（白名单总条目=%d）",
		reachAttached, ReachGateFlagEnv, approval.FlagKey, flagOn, forReach, approvalCheckerRef.ActiveEntryCount())
	if !flagOn {
		logger.Warnf("[reach-gate] ⚠️ block 态但白名单旗子 %s 未开 ⇒ 刹车生效：reason=%s 的拒绝**不拦**，"+
			"当前实际只等价于 shadow。要真阻断请同时开 %s",
			approval.FlagKey, approval.ReasonDisabledByFlag, featureflag.EnvNameOf(approval.FlagKey))
	}
	if forReach == 0 {
		logger.Warnf("[reach-gate] ⚠️ block 态且 reach 名下有效授权=0 ⇒ 白名单旗子一开，**所有**非工具外发都会被拒。"+
			"放量前用 /agent/tools/approval/whitelist 以 tool=%s 灌入授权；该表只在进程内存里，重启即空",
			ReachApprovalToolKey)
	}
	logger.Warnf("[reach-gate] 🚫 block 态是行为变更：被拒的外发不会重试（挽回队列把它记为 blocked_by_approval，不烧尝试次数），"+
		"退化回观察请把 %s 改回 shadow 并重启", ReachGateFlagEnv)
	return true
}

// LogReachGateSkippedAssemblyPoint 装配点没装上门时留痕（旗子 off 是预期默认，不喊）。
//
// 单独有这个入口而不是让每个装配点自己拼日志：装配点会随外发路径增加，
// "哪条路径没挂门"必须用同一种说法、同一个前缀打得出来，否则 grep 不到漏的那个。
func LogReachGateSkippedAssemblyPoint(site string) {
	if reachGateModeValue == string(approvalGateOff) {
		return
	}
	logger.Warnf("[reach-gate] ⚠️ 装配点 %s 未挂上外发闸门（mode=%s, wired=%t）：这条路径的外发不受审批约束",
		site, reachGateModeValue, reachDecisions != nil)
}

// GetReachGateSnapshot 返回外发闸门的接线状态与观察期累计，供运维端点读取。
func GetReachGateSnapshot() (ReachGateSnapshot, *approval.DecisionCounter) {
	mode := approvalGateMode(reachGateModeValue)
	snap := ReachGateSnapshot{
		Mode:              reachGateModeValue,
		Wired:             reachDecisions != nil,
		BlocksWhenDenied:  mode == approvalGateBlock,
		GateFlagEnv:       ReachGateFlagEnv,
		DependencyFlagEnv: ApprovalGateFlagEnv,
		ReachToolKey:      ReachApprovalToolKey,
		WhitelistFlagEnv:  featureflag.EnvNameOf(approval.FlagKey),
		WhitelistFlagOn:   featureflag.Get(approval.FlagKey).Bool(),
		AttachedServices:  reachAttached,
	}
	// 判定与读数同源：白名单在 W-1 那个 checker 手里，它没接线时两个分项都读 0
	// （而不是 -1 或"未知"——运维看到 0 与"旗子开了"合起来的含义就是"一条都不会放行"，
	// 这正是需要在放量前看见的那件事）。
	snap.WhitelistActiveEntries = approvalCheckerRef.ActiveEntryCount()
	snap.WhitelistEntriesForReach = approvalCheckerRef.ActiveEntryCountFor(ReachApprovalToolKey)
	if !snap.Wired && mode != approvalGateOff && approvalCheckerRef == nil {
		snap.DependencyUnmet = true
	}
	return snap, reachDecisions
}

package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/approval"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/utils/logger"
)

// W-1 冷触达审批门接线（新规划任务清单 T-P1-05 接线 + T-P1-06 三态 off|shadow|block）
//
// 未接线状态比"没人调 SetGlobalApprovalChecker"更深一层，是两处：
//  1. `SetGlobalApprovalChecker` 非测试调用点 0 ⇒ 全局 checker 恒为 nil；
//  2. `WithApproval` / `WithApprovalChecker` 非测试调用点同样 0 ⇒ **没有任何工具被审批门包过**。
//
// 所以只补第 1 处等于接了一根没人插的线：checker 装好了，工具侧永远不会去问它。
// 本文件因此改走装饰器链（`tooluse.ApprovalGateDecorator`），在 executor 唯一的
// 建链点 `buildHandler` 上按工具类型挂门，而不是去每个注册点各包一层。
//
// `WhiteListApprovalChecker` 是**默认拒绝**语义 —— `decide()` 在白名单旗子没开时返回
// `disabled_by_flag`（拒绝），开了但账号不在表里返回 `denied_default`（拒绝）。
// 这个"默认拒绝"决定了 block 态的边界，也决定了它为什么必须带一道刹车：
//
//	刹车：block 态下，若白名单旗子（`approval.FlagKey`）没开，判定理由为
//	`disabled_by_flag` 的调用**不拦**。因为此时 checker 根本没查过白名单，
//	"允许"这一侧无路径可达 —— 拦下去就是全量冷触达无差别失败，且运维在端点上
//	也放不进任何账号（放了也不生效）。这不是"把 block 降级成 shadow"：白名单旗子
//	一开，同一份代码立刻开始真拦。
//
//	不放刹车的情形：白名单旗子已开但表为空 ⇒ 全部 `denied_default` ⇒ 全拦。
//	这**是**白名单的预期语义（explicit allow），不做特殊处理，只在启动日志和快照里
//	把 `whitelist_active_entries=0` 明确报出来，让运维在放量前看见自己的配置。
//
// 还有一个必须写下来的坑：两把旗子不是一把 ——
//   - `FF_LTC_APPROVAL_GATE`（本文件）：审批门挂不挂链、以哪种模式挂；
//   - `approval.FlagKey` = `ai.safety.tool_approval_gate`（env 名
//     `FF_AI.SAFETY_TOOL_APPROVAL_GATE`，由 featureflag 读取，点号合法）：白名单生不生效。
//
// 因此**阻断需要两把旗子同时到位**（`block` + 白名单旗子为真），只开一把都不会拦人。
// 观察端点把两把旗子的当前值、有效白名单条数和"本模式是否真拦"一起回显，
// 否则"would_deny=100% 且全是 disabled_by_flag"会被误读成"账号都没被批准"。

// 环境变量名集中在此，避免"文档写一个、代码读另一个"。导出的那份给运维端点的 env_hint 用，
// 两处引用同一个常量 ⇒ 提示语和实际读取的变量名不可能再漂移。
const (
	ApprovalGateFlagEnv = "FF_LTC_APPROVAL_GATE"
)

type approvalGateMode string

const (
	approvalGateOff    approvalGateMode = "off"
	approvalGateShadow approvalGateMode = "shadow"
	approvalGateBlock  approvalGateMode = "block"
)

// approvalGateState 当前生效的审批门接线状态；由 applyApprovalGate 在装配期写一次。
//
// 读写无锁的前提与 circuitStateRef 相同：写入发生在 router.Setup() 里、
// HTTP 服务启动之前。热切三态需要连同 executor 的 handler 缓存一起失效，光换这几个字段不生效。
var (
	approvalModeValue  = string(approvalGateOff)
	approvalCheckerRef *approval.WhiteListApprovalChecker
	approvalDecisions  *approval.DecisionCounter
	approvalGlobalSet  bool
)

// ApprovalGateSnapshot 审批门接线状态快照（值类型，运维端点直接序列化）。
//
// 两把旗子分开回显是刻意的：Wired/Mode 说的是"闸门挂没挂上链"，
// WhitelistFlagOn 说的是"白名单生没生效"。只看前者会把
// "would_deny=100% 且全是 disabled_by_flag" 误读成"账号都没被批准"。
//
// BlocksWhenDenied 说的是"这个模式下拒绝会不会真的传下去"——只有 block 态为 true，
// 且它仍受上面那道刹车约束（白名单旗子没开时 disabled_by_flag 不拦）。
// WhitelistActiveEntries 说的是"现在放得行的账号有几个"：block 态 + 0 条
// = 所有冷触达都会被拒，这个数字必须在放量前读得到。
type ApprovalGateSnapshot struct {
	Mode                   string `json:"mode"`
	Wired                  bool   `json:"wired"`
	GlobalCheckerSet       bool   `json:"global_checker_set"`
	BlocksWhenDenied       bool   `json:"blocks_when_denied"`
	WhitelistFlagKey       string `json:"whitelist_flag_key"`
	WhitelistFlagEnv       string `json:"whitelist_flag_env"`
	WhitelistFlagOn        bool   `json:"whitelist_flag_on"`
	WhitelistActiveEntries int    `json:"whitelist_active_entries"`
	GateFlagEnv            string `json:"gate_flag_env"`
}

// parseApprovalGateMode 解析开关值。
//
// 三态口径：off 不挂链；shadow 挂链但绝不拦；block 挂链且拒绝真的传下去。
// 与熔断那张旗子的差别刻意保留：熔断的 `true` 可以直接等价 enforce（"下游坏了先短路"，
// 恢复即放行），审批的布尔真值只到 shadow —— 因为 block 会**把客户的冷触达永久拒掉**，
// 必须由字面量 `block|enforce|active` 显式表达，不能在 env 里写个 `true` 就生效。
// 认不出的值判 off 并告警。
func parseApprovalGateMode(raw string) approvalGateMode {
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
		logger.Warnf("[tool-approval] ⚠️ %s=%q 是布尔真值：语义不足以表达\"把冷触达拒掉\"⇒ 按 shadow 处理。"+
			"要转阻断请显式写 block（并按需开启白名单旗子 %s），可用值：off|shadow|block",
			ApprovalGateFlagEnv, raw, approval.FlagKey)
		return approvalGateShadow
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			logger.Warnf("[tool-approval] ⚠️ %s=%q 解析为布尔真值 ⇒ 按 shadow 处理（转阻断须显式写 block）",
				ApprovalGateFlagEnv, raw)
			return approvalGateShadow
		}
		return approvalGateOff
	}
	logger.Warnf("[tool-approval] %s=%q 无法识别 ⇒ 按 off 处理（审批门不挂链）；可用值：off|shadow|block", ApprovalGateFlagEnv, raw)
	return approvalGateOff
}

// approvalDenialBlocks 判定一个"拒绝"在给定模式下是否真的拦截。
//
// 单独抽成函数是因为它同时被三处用：装饰器侧的 checker 包装、留痕日志的 blocked 字段、
// 快照的 blocks_when_denied。三处口径必须一致，否则报告里"拦了多少"和日志里的
// blocked=true 会对不上。
//
// 刹车见文件头：block 态 + 白名单旗子没开时，`disabled_by_flag` 这一类拒绝不拦
// （此时"允许"无路径可达，拦下去只剩无差别失败）。`denied_default`/`denied_explicit`
// 不受刹车影响 —— 它们恰恰是"白名单查过了、这个账号没被批准"，正是阻断要拦的那批。
func approvalDenialBlocks(mode approvalGateMode, reason string) bool {
	if mode != approvalGateBlock {
		return false
	}
	return reason != approval.ReasonDisabledByFlag
}

// observeApprovalDecision 审批门留痕：计数 + 结构化日志。
//
// 作为 approval.NewWhiteList 的 OnDecision 回调传入，因此**每次判定都会到这里**，
// 包括放行的那些（AC① 要的是"冷触达调用产生 Decision 审计行"，不是只有拒绝才留痕）。
//
// would_deny 与 blocked 是两个字段，刻意不合并：前者是"切阻断后会被拦的量"（观察期的
// 核心指标，shadow 态也照样累加），后者是"这一次真的拦下来了"。只在 block 态且不被
// 刹车豁免时才为 true。合并成一个字段会让 T-P1-05 攒下的报告口径在转阻断后失效。
func observeApprovalDecision(ctx context.Context, toolName, accountID string, d approval.Decision) {
	mode := approvalGateMode(approvalModeValue)
	approvalDecisions.Observe(ctx, toolName, accountID, d)

	ev := logger.Ctx(ctx).Info()
	if !d.Allowed {
		ev = logger.Ctx(ctx).Warn()
	}
	ev.Str("event", "tool_approval_decision").
		Str("mode", approvalModeValue).
		Bool("allowed", d.Allowed).
		Bool("would_deny", !d.Allowed).
		Bool("blocked", !d.Allowed && approvalDenialBlocks(mode, d.Reason)).
		Str("tool_name", toolName).
		Str("account", accountID).
		Str("reason", d.Reason).
		Bool("whitelist_flag_on", featureflag.Get(approval.FlagKey).Bool()).
		Msg("冷触达审批门判定")
}

// shadowApprovalChecker 把 shadow 语义带到全局注入点。
//
// 必须包这一层：`WithApproval` 那条路（approvalTool）拿到 false 就硬拦，它不认识
// ApprovalShadow 字段。不包的话"不阻断"只对装饰器那条路成立——今天生产里没有
// WithApproval 调用点所以无感，等将来有人按工具粒度包一层时，它会绕过旗子直接拦人。
//
// 留痕照常：inner 仍然被问、OnDecision 仍然回调，所以观察期的数字不因包装而少一笔。
type shadowApprovalChecker struct{ inner tooluse.ApprovalChecker }

func (s shadowApprovalChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	if s.inner != nil {
		s.inner.IsApproved(ctx, toolName, accountIDorOwnerKey)
	}
	return true
}

// blockApprovalChecker 是 block 态的裁决收口：把"默认拒绝"里的两类分开处理。
//
// 它存在的唯一理由是文件头那道刹车——白名单旗子没开时（reason=disabled_by_flag）
// checker 压根没查过白名单，此时放行不是"放水"，而是"阻断无据可依"。
// 除此之外 inner 说不行，就是不行。
//
// inner 用具体类型 *approval.WhiteListApprovalChecker 而非接口：刹车要看判定**理由**，
// 而 tooluse.ApprovalChecker 只有 bool。改成调用 Verdict 而不是"再读一次旗子"，
// 是为了让理由与判定同源于一次 decide()——分两次读旗子会在翻转的瞬间给出
// "allowed=false 且 reason=whitelisted"这种自相矛盾的组合。
type blockApprovalChecker struct {
	inner *approval.WhiteListApprovalChecker
}

func (b blockApprovalChecker) IsApproved(ctx context.Context, toolName, accountID string) bool {
	if b.inner == nil {
		// 与"闸门未接线"逐字一致：没有 checker 就没有裁决来源，此时放行。
		return true
	}
	allowed, reason := b.inner.Verdict(ctx, toolName, accountID)
	if allowed {
		return true
	}
	return !approvalDenialBlocks(approvalGateBlock, reason)
}

// applyApprovalGate 把审批门挂进 executor 配置，由 InitGlobalToolExecutor 在
// NewToolExecutor 之前调用。返回最终生效的模式（供装配日志打印）。
//
// off 分支显式清空三个字段 + 把全局 checker 归零：旗子关掉时必须逐字回到
// "没接过线"的状态，而不是留着上一轮的 checker 继续判定。
func applyApprovalGate(config *tooluse.ToolExecutorConfig) approvalGateMode {
	mode := parseApprovalGateMode(os.Getenv(ApprovalGateFlagEnv))
	approvalModeValue = string(mode)
	if mode == approvalGateOff {
		config.ApprovalChecker = nil
		config.ApprovalShadow = false
		approvalCheckerRef, approvalDecisions, approvalGlobalSet = nil, nil, false
		tooluse.SetGlobalApprovalChecker(nil)
		return mode
	}

	counter := approval.NewDecisionCounter()
	checker := approval.NewWhiteList(observeApprovalDecision, nil)
	approvalCheckerRef, approvalDecisions = checker, counter

	// shadow 侧逐字保持 T-P1-05 的接线，不做"顺手加固"：装饰器拿**裸 checker**，
	// 不拦的保证只来自 ApprovalShadow=true 这一道。这个性质是被变异测试盯住的
	// （把 ApprovalShadow 改成 false ⇒ 冷触达立刻被拦 ⇒ 测试变红）；若这里换成
	// shadowApprovalChecker 包装版，同一改动就不再有任何后果，保证从"可判别"退化成"恒成立"。
	//
	// block 侧必须换：ApprovalShadow=false 已经会让装饰器拦人，此时交给它裸 checker
	// 等于把刹车（disabled_by_flag 不拦）丢掉，所以两条路共用同一个 blockApprovalChecker。
	gate := tooluse.ApprovalChecker(checker)
	config.ApprovalShadow = true
	globalGate := tooluse.ApprovalChecker(shadowApprovalChecker{inner: checker})
	if mode == approvalGateBlock {
		gate = blockApprovalChecker{inner: checker}
		config.ApprovalShadow = false
		globalGate = gate
	}
	config.ApprovalChecker = gate

	// AC③ 要求的全局注入点。交出去的权限与模式一致：
	// shadow ⇒ shadowApprovalChecker（恒放行，因为 WithApproval 那条路没有 shadow 字段，
	// 只能靠包装实现"不拦"）；block ⇒ 与装饰器侧同一个 gate，拦的是同一批人。
	//
	// 已知口径偏差（非缺陷，留此备忘）：若某个冷触达工具**同时**被 WithApproval 包过、
	// 又走 buildHandler 的装饰器链，它会各问一次 ⇒ 一次逻辑调用记两笔 Decision。
	// 今天 WithApproval 非测试调用点为 0，所以实际不发生；出现调用点时应在留痕侧去重，
	// 而不是去掉其中一层包装（两层各有各的入口）。
	tooluse.SetGlobalApprovalChecker(globalGate)
	approvalGlobalSet = true

	flagOn := featureflag.Get(approval.FlagKey).Bool()
	if mode == approvalGateShadow {
		logger.Infof("[tool-approval] ✅ 审批门已接线，模式=shadow（只记录判定，不拦任何冷触达）；生效配置：approval_gate=%s 白名单旗子 %s=%t",
			ApprovalGateFlagEnv, approval.FlagKey, flagOn)
		logger.Infof("[tool-approval] ⚠️ shadow 态：**冷触达照常外发**。转阻断前先看 /agent/tools/approval 的 would_deny 报告，" +
			"并确认授权来源已可运营（/agent/tools/approval/whitelist 只写进程内存、重启即空，撑不起阻断）")
		return mode
	}

	entryCount := checker.ActiveEntryCount()
	logger.Infof("[tool-approval] 🚫 审批门已接线，模式=block（拒绝会真的传下去：冷触达被拒并返回可读原因）；生效配置：approval_gate=%s 白名单旗子 %s=%t 有效白名单条目=%d",
		ApprovalGateFlagEnv, approval.FlagKey, flagOn, entryCount)
	if !flagOn {
		logger.Warnf("[tool-approval] ⚠️ block 态但白名单旗子 %s 未开 ⇒ 刹车生效：reason=%s 的拒绝**不拦**，"+
			"当前实际只等价于 shadow（白名单未启用，没有任何账号能被批准，拦下去只剩无差别失败）。"+
			"要真正阻断请同时开 %s；该旗子走 featureflag 热加载，不必重启",
			approval.FlagKey, approval.ReasonDisabledByFlag, featureflag.EnvNameOf(approval.FlagKey))
	}
	if entryCount == 0 {
		logger.Warnf("[tool-approval] ⚠️ block 态且有效白名单条目=0 ⇒ 白名单旗子一开，**所有**冷触达都会被拒（denied_default）。" +
			"放量前先用 /agent/tools/approval/whitelist 灌入授权；该表只在进程内存里，重启即空")
	}
	logger.Warnf("[tool-approval] 🚫 block 态是行为变更：被拒的外发不会重试（ErrApprovalDenied 在 retry/loop-guard 里都是终止语义），"+
		"退化回观察请把 %s 改回 shadow 并重启", ApprovalGateFlagEnv)
	return mode
}

// GetApprovalSnapshot 返回审批门的接线状态与观察期累计，供运维端点读取。
// 未接线时 Mode=off、计数器为 nil，端点据此报 wired=false。
func GetApprovalSnapshot() (ApprovalGateSnapshot, *approval.DecisionCounter) {
	snap := ApprovalGateSnapshot{
		Mode:             approvalModeValue,
		Wired:            approvalCheckerRef != nil,
		GlobalCheckerSet: approvalGlobalSet,
		// 只有 block 态会让拒绝传下去；off/shadow 恒 false，端点据此解释 would_deny 为什么没后果。
		BlocksWhenDenied:       approvalModeValue == string(approvalGateBlock),
		WhitelistFlagKey:       approval.FlagKey,
		WhitelistFlagEnv:       featureflag.EnvNameOf(approval.FlagKey),
		GateFlagEnv:            ApprovalGateFlagEnv,
		WhitelistActiveEntries: approvalCheckerRef.ActiveEntryCount(),
	}
	// 白名单旗子由 featureflag 每 5s 轮询 env 刷新；这里读的是缓存值，与判定时刻同源。
	snap.WhitelistFlagOn = featureflag.Get(approval.FlagKey).Bool()
	return snap, approvalDecisions
}

// ApprovalWhitelistMutate 在运行期改白名单（加/撤一个 (tool, account) 授权）。
//
// 有意只经这一个入口：白名单内容不落库，进程重启即空，若允许散点直接改 checker，
// 排障时会出现"这里为什么放行了"查不到出处。返回 false 表示审批门未接线。
// expiresAt 零值 = 永不过期（沿用 WhiteListApprovalChecker.Whitelist 的口径）。
//
// T-P1-06 起这个入口有了直接后果：block 态下 revoke 一个正在放行的账号，下一次冷触达
// 立刻被拒；且授权只在内存里，重启后 block 态会退化成"全拒"（有效条目=0）。
// 所以每次变更都把变更后的有效条目数写进日志，让"改完还剩多少可外发"是可回溯的。
func ApprovalWhitelistMutate(toolName, accountID string, expiresAt time.Time, revoke bool) bool {
	if approvalCheckerRef == nil {
		return false
	}
	action := "grant"
	if revoke {
		action = "revoke"
		approvalCheckerRef.Revoke(toolName, accountID)
	} else {
		approvalCheckerRef.Whitelist(toolName, accountID, expiresAt)
	}
	logger.Infof("[tool-approval] 白名单变更 %s：tool=%s account=%s mode=%s 有效条目=%d",
		action, toolName, accountID, approvalModeValue, approvalCheckerRef.ActiveEntryCount())
	return true
}

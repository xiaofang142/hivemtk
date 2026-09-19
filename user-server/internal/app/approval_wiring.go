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

// W-1 冷触达审批门接线（新规划任务清单 T-P1-05，**本卡不阻断**）
//
// 未接线状态比"没人调 SetGlobalApprovalChecker"更深一层，是两处：
//  1. `SetGlobalApprovalChecker` 非测试调用点 0 ⇒ 全局 checker 恒为 nil；
//  2. `WithApproval` / `WithApprovalChecker` 非测试调用点同样 0 ⇒ **没有任何工具被审批门包过**。
//
// 所以只补第 1 处等于接了一根没人插的线：checker 装好了，工具侧永远不会去问它。
// 本文件因此改走装饰器链（`tooluse.ApprovalGateDecorator`），在 executor 唯一的
// 建链点 `buildHandler` 上按工具类型挂门，而不是去每个注册点各包一层。
//
// 为什么这一卡绝不阻断：`WhiteListApprovalChecker` 是**默认拒绝**语义 ——
// `decide()` 在白名单旗子没开时返回 `disabled_by_flag`（拒绝），开了但账号不在表里
// 返回 `denied_default`（拒绝）。也就是说：今天直接把 checker 挂上去并放行拦截逻辑，
// 效果是**所有冷触达外发立刻全拒**——白名单唯一的灌入口是下面的 admin 端点，
// 手工、不落库、重启即空，撑不起"把客户的外发按规则放行"这件事。
// 因此 FF_LTC_APPROVAL_GATE 只提供 off|shadow 两态，本文件里**不存在**能把
// ApprovalShadow 置为 false 的分支；转阻断是 T-P1-06 的事，且它的准入条件是
// 本文件累计出的 shadow 报告（按 reason 拆开的那份）+ 一个可运营的授权来源。
//
// 还有一个必须写下来的坑：闸门自身带**第二把旗子** `approval.FlagKey`
// （`ai.safety.tool_approval_gate`，由 featureflag 读取，env 名是
// `FF_AI.SAFETY_TOOL_APPROVAL_GATE`）。它与本文件的 FF_LTC_APPROVAL_GATE 不是一把：
// 前者决定白名单是否生效，后者决定审批门是否挂上链。观察端点把两把旗子的当前值
// 一起回显，否则"would_deny=100% 且全是 disabled_by_flag"会被误读成"账号都没被批准"。

// 环境变量名集中在此，避免"文档写一个、代码读另一个"。导出的那份给运维端点的 env_hint 用，
// 两处引用同一个常量 ⇒ 提示语和实际读取的变量名不可能再漂移。
const (
	ApprovalGateFlagEnv = "FF_LTC_APPROVAL_GATE"
)

type approvalGateMode string

const (
	approvalGateOff    approvalGateMode = "off"
	approvalGateShadow approvalGateMode = "shadow"
)

// approvalGateState 当前生效的审批门接线状态；由 applyApprovalGate 在装配期写一次。
//
// 读写无锁的前提与 circuitStateRef 相同：写入发生在 router.Setup() 里、
// HTTP 服务启动之前。热切两态需要连同 executor 的 handler 缓存一起失效，光换这几个字段不生效。
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
type ApprovalGateSnapshot struct {
	Mode             string `json:"mode"`
	Wired            bool   `json:"wired"`
	GlobalCheckerSet bool   `json:"global_checker_set"`
	WhitelistFlagKey string `json:"whitelist_flag_key"`
	WhitelistFlagOn  bool   `json:"whitelist_flag_on"`
	GateFlagEnv      string `json:"gate_flag_env"`
}

// parseApprovalGateMode 解析开关值。
//
// 与熔断那张旗子同一口径：布尔式真值只到 shadow；**这里连 block/enforce 也只到 shadow**，
// 因为本卡不交付阻断能力（熔断的 enforce 是"下游坏了先短路"，审批的 enforce 是"把客户的
// 外发永久拒掉"，后者必须等白名单有灌入路径 + 一轮实测报告）。
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
	case "enforce", "block", "active", "on", "yes", "y", "true", "1":
		if v == "enforce" || v == "block" || v == "active" {
			logger.Warnf("[tool-approval] ⚠️ %s=%q 要求的阻断本版本不交付（转阻断是 T-P1-06）⇒ 按 shadow 处理：只记录判定，不拦任何冷触达",
				ApprovalGateFlagEnv, raw)
		}
		return approvalGateShadow
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return approvalGateShadow
		}
		return approvalGateOff
	}
	logger.Warnf("[tool-approval] %s=%q 无法识别 ⇒ 按 off 处理（审批门不挂链）；可用值：off|shadow", ApprovalGateFlagEnv, raw)
	return approvalGateOff
}

// observeApprovalDecision 审批门留痕：计数 + 结构化日志。
//
// 作为 approval.NewWhiteList 的 OnDecision 回调传入，因此**每次判定都会到这里**，
// 包括放行的那些（AC① 要的是"冷触达调用产生 Decision 审计行"，不是只有拒绝才留痕）。
func observeApprovalDecision(ctx context.Context, toolName, accountID string, d approval.Decision) {
	approvalDecisions.Observe(ctx, toolName, accountID, d)

	ev := logger.Ctx(ctx).Info()
	if !d.Allowed {
		ev = logger.Ctx(ctx).Warn()
	}
	ev.Str("event", "tool_approval_decision").
		Bool("shadow", approvalModeValue == string(approvalGateShadow)).
		Bool("would_deny", !d.Allowed).
		Bool("allowed", d.Allowed).
		Str("tool_name", toolName).
		Str("account", accountID).
		Str("reason", d.Reason).
		Bool("whitelist_flag_on", featureflag.Get(approval.FlagKey).Bool()).
		Msg("冷触达审批门判定：shadow 态不改变放行行为")
}

// shadowApprovalChecker 把 shadow 语义带到全局注入点。
//
// 必须包这一层：`WithApproval` 那条路（approvalTool）拿到 false 就硬拦，它不认识
// ApprovalShadow 字段。不包的话"本卡不阻断"只对装饰器那条路成立——今天生产里没有
// WithApproval 调用点所以无感，等将来有人按工具粒度包一层时，它会绕过旗子直接拦人。
//
// 留痕照常：inner 仍然被问、OnDecision 仍然回调，所以观察期的数字不因包装而少一笔。
// T-P1-06 加 enforce 时，这里改成按模式交出 inner 或包装版。
type shadowApprovalChecker struct{ inner tooluse.ApprovalChecker }

func (s shadowApprovalChecker) IsApproved(ctx context.Context, toolName, accountIDorOwnerKey string) bool {
	if s.inner != nil {
		s.inner.IsApproved(ctx, toolName, accountIDorOwnerKey)
	}
	return true
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

	config.ApprovalChecker = checker
	// 本函数不存在把 ApprovalShadow 置 false 的分支：见文件头。
	config.ApprovalShadow = true

	// AC③ 要求的全局注入点。交出去的是 shadow 包装版（见 shadowApprovalChecker），
	// 因此这一句**不改变行为**：它让"全局 checker 已装配"为真，将来任何按 Tool
	// 粒度包 WithApproval 的代码不必再等一次装配，且在中途也不会拿到拦人的权限。
	tooluse.SetGlobalApprovalChecker(shadowApprovalChecker{inner: checker})
	approvalGlobalSet = true

	logger.Infof("[tool-approval] ✅ 审批门已接线，模式=%s（shadow：只记录判定，不拦任何冷触达）；生效配置：approval_gate=%s 白名单旗子 %s=%t",
		mode, ApprovalGateFlagEnv, approval.FlagKey, featureflag.Get(approval.FlagKey).Bool())
	logger.Infof("[tool-approval] ⚠️ shadow 态：**冷触达照常外发**。转阻断前先看 /agent/tools/approval 的 would_deny 报告，" +
		"并确认授权来源已可运营（/agent/tools/approval/whitelist 只写进程内存、重启即空，撑不起阻断）")
	return mode
}

// GetApprovalSnapshot 返回审批门的接线状态与观察期累计，供运维端点读取。
// 未接线时 Mode=off、计数器为 nil，端点据此报 wired=false。
func GetApprovalSnapshot() (ApprovalGateSnapshot, *approval.DecisionCounter) {
	snap := ApprovalGateSnapshot{
		Mode:             approvalModeValue,
		Wired:            approvalCheckerRef != nil,
		GlobalCheckerSet: approvalGlobalSet,
		WhitelistFlagKey: approval.FlagKey,
		GateFlagEnv:      ApprovalGateFlagEnv,
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
func ApprovalWhitelistMutate(toolName, accountID string, expiresAt time.Time, revoke bool) bool {
	if approvalCheckerRef == nil {
		return false
	}
	if revoke {
		approvalCheckerRef.Revoke(toolName, accountID)
		return true
	}
	approvalCheckerRef.Whitelist(toolName, accountID, expiresAt)
	return true
}

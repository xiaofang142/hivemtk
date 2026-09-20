package app

import (
	"context"
	"os"
	"strings"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
)

// G-3 工具后果分级接线（新规划任务清单 T-P3-05）
//
// tooluse 侧此前只有一个**死掉的**分级词汇：`feedback_decorator.go` 里的
// ToolRiskLevel(read|write|admin) 有 3 个工具实现，而唯一读者 ToolCallEvent.RiskLevel
// 在 FeedbackCollectorDecorator 里从未被赋值 ⇒ 元数据写进 DB 后没有任何消费方
// （app/feedback_sink_adapter.go 只在非空时写 metadata）。那是一副"看着有、其实不通"
// 的假配置，与本卡要修的现象同源：`WhitelistPermissionChecker.defaultAllow=true`
// 让"白名单"名义上是授权、实现上是默认放行。
//
// 因此本卡先把词汇迁到 readonly|low_write|high_write（判据=副作用落在谁的系统、能否撤回），
// 把 45 个生产工具的声明补齐，再挂一个**只判定不拦截**的观察层。
//
// 两态而非三态（与熔断/审批门的刻意差别）：
//
//	off（默认） 不挂判定层，装饰链与接线前逐字节一致
//	shadow      每次调用算一次判定并留痕；**代码里不存在拒绝路径**
//
// 熔断有 enforce、审批门有 block，本卡却**没有**阻断态：卡面写着"只记录判定，
// 不改放行结果"，转阻断排在 P9。所以把 `FF_TOOL_PERMISSION_ENFORCE=enforce|block|true`
// 一律按 shadow 挂载并**显式告警"这个构建里它拦不住任何东西"** —— 运维写了 enforce
// 却以为在阻断，是比不接更糟的状态（defaultAllow=true 那种"名义与实现相反"正是本卡要清掉的）。
// P9 接阻断时改的是 RiskGateDecorator，不是这里的解析表；报告的 blocks_when_denied 恒 false，
// 就是为了让"当前有没有真拦"在数据上无法被误读。

const (
	// RiskGateFlagEnv 挂不挂判定层。名字里的 enforce 是卡面指定的（P9 会把它接成真拦截），
	// 今天它的合法值只有 off|shadow，其余真值形态一律降级为 shadow 并告警。
	RiskGateFlagEnv = "FF_TOOL_PERMISSION_ENFORCE"
	// riskRetainedEnv 保留窗内判定条数上限。计数不受它影响（见 MemoryRiskObserver）。
	riskRetainedEnv     = "TOOL_RISK_OBSERVED_RETAINED"
	riskRetainedDefault = 2000
)

type riskGateMode string

const (
	riskGateOff    riskGateMode = "off"
	riskGateShadow riskGateMode = "shadow"
)

// 装配期写一次、HTTP 读，无锁前提与 circuitStateRef / approvalModeValue 相同。
var (
	riskModeValue   = string(riskGateOff)
	riskObserverRef *tooluse.MemoryRiskObserver
)

// parseRiskGateMode 解析旗子值。
//
// 认不出的值判 off（不挂链、不留痕）：挂一层没有 observer 消费的门只会白烧判定开销。
// 但**阻断语气的值**（enforce/block/active/true/1/on/yes）判 shadow 并告警，
// 不判 off —— 写出这些值的人显然想要拦截，静默不挂链等于把他的意图丢掉。
func parseRiskGateMode(raw string) riskGateMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return riskGateOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return riskGateOff
	case "shadow", "observe", "watch", "log", "report":
		return riskGateShadow
	case "enforce", "block", "active", "on", "yes", "y", "true", "1":
		logger.Warnf("[tool-risk] ⚠️ %s=%q 是阻断语气：本构建的分级层**没有任何拒绝路径**（T-P3-05 只判定，转阻断在 P9）"+
			"⇒ 按 shadow 挂载，只会留下 would_deny 观察数据，不会拦下任何调用。"+
			"不要据此认为高危工具已被管控；可用值：off|shadow", RiskGateFlagEnv, raw)
		return riskGateShadow
	}
	logger.Warnf("[tool-risk] %s=%q 无法识别 ⇒ 按 off 处理（分级判定层不挂链）；可用值：off|shadow", RiskGateFlagEnv, raw)
	return riskGateOff
}

// riskSink 在内存计数之外补一条结构化日志。
//
// 只给 would_deny 的那批打日志：非高危调用占绝大多数，逐条打等于把日志刷满噪音，
// 而累计量由 MemoryRiskObserver 负责，不靠日志行数。
type riskSink struct{ obs *tooluse.MemoryRiskObserver }

func (s riskSink) Observe(ctx context.Context, d tooluse.RiskDecision) {
	s.obs.Observe(ctx, d)
	if !d.WouldDeny {
		return
	}
	logger.Ctx(ctx).Info().
		Str("event", "tool_risk_decision").
		Str("mode", riskModeValue).
		Str("tool_name", d.ToolName).
		Str("risk_level", string(d.Level)).
		Bool("risk_declared", d.Declared).
		Str("agent_id", d.AgentID).
		Str("caller_id", d.CallerID).
		Bool("allowed", d.Allowed).
		Bool("would_deny", true).
		Bool("agent_wildcard", d.AgentWildcard).
		Str("reason", d.Reason).
		Msg("工具后果分级判定：若转阻断会被拦下的调用")
}

// applyRiskGate 按旗子把分级判定挂进 executor 配置。
//
// 调用方：InitGlobalToolExecutor（NewToolExecutor 之前）。off 时显式清空两个字段 +
// 把包内引用归零，使"关旗 = 与接线前完全一致"成为可断言的性质。
//
// grants 取全局 WhitelistPermissionChecker：判定只读它的 agentWhitelist 一张表
// （见 tooluse.AgentGrantReader 的注释），不借它的 Check —— 后者有全局白名单、
// "*" 与 defaultAllow 三级兜底，会把"没人拦"读成"被授权"。
func applyRiskGate(config *tooluse.ToolExecutorConfig) riskGateMode {
	mode := parseRiskGateMode(os.Getenv(RiskGateFlagEnv))
	riskModeValue = string(mode)
	if mode == riskGateOff {
		config.RiskGrants = nil
		config.RiskObserver = nil
		riskObserverRef = nil
		return mode
	}

	retained := riskRetainedDefault
	if n, ok := envInt("[tool-risk]", riskRetainedEnv, 10, 100000); ok {
		retained = n
	}
	obs := tooluse.NewMemoryRiskObserver(retained)
	grants := tooluse.AgentGrantReader(GetGlobalPermissionChecker())

	config.RiskGrants = grants
	config.RiskObserver = riskSink{obs: obs}
	riskObserverRef = obs

	logger.Infof("[tool-risk] ✅ 工具后果分级判定已接线，模式=%s（保留窗 %d 条判定，累计计数不受上限影响；"+
		"本层无拒绝路径 ⇒ 放行结果与接线前一致）", mode, retained)
	// 这里不写"高危有几个、门内有几个"：那组数字随工具集合漂，抄进日志就会变成第二处事实源。
	// 启动日志只指路，数字由 /api/agent/tools/risk 每次实算。
	logger.Infof("[tool-risk] ⚠️ 观察数据只在进程内存里（重启归零）。转阻断前先看 /api/agent/tools/risk 的 " +
		"would_deny 与 high_write_in_approval_gate（后者给出审批门的实际覆盖面）")
	return mode
}

// GetToolRiskReport 组装 AC③ 的分级报告：静态声明面（每次实算，重启不丢）+
// 授权面（读当前 Agent 白名单）+ 动态观察面（进程内，未接线时计数全 0 且 mode=off）。
//
// 授权面**不看旗子**：读的是全局 WhitelistPermissionChecker，与判定层挂没挂无关。
// 若在 off 时把 grants 传 nil，报告里 "0 个 Agent 配过白名单" 和 "没读授权数据"
// 就长得一样，而 P9 评审恰恰要在旗子还关着的时候拿这份数据判断能不能开。
// mode/blocks_when_denied 因此才是"观察层与阻断能力"的口径，不是"这份报告可不可信"。
func GetToolRiskReport() tooluse.RiskReport {
	return tooluse.BuildRiskReport(
		tooluse.GetGlobalRegistry(),
		tooluse.AgentGrantReader(GetGlobalPermissionChecker()),
		riskObserverRef,
		riskModeValue,
		// 阻断能力恒 false：本构建的判定层没有拒绝路径，P9 接阻断时改这一处。
		false,
		RiskGateFlagEnv,
	)
}

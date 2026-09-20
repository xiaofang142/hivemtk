// 工具风险分级闸门（T-P3-05 / G-3）：**只判定、只留痕，不改放行结果**。
//
// 与已有的两层控制各自回答不同的问题，缺一层都会留下具体的盲区：
//
//	WhitelistPermissionChecker  「这个 Agent 能不能调这个工具」——授权。
//	                              但 defaultAllow=true，未配置白名单的 Agent 什么都能调。
//	ApprovalGateDecorator       「这次冷触达有没有被批准」——逐次授权。
//	                              但它只认 IsColdOutreachTool 的名字启发式，
//	                              knowledge.add_doc 改写 RAG 语料不在其内。
//	本文件                     「这个工具一旦跑起来的后果能不能撤回」——后果分级。
//	                              判据是声明出来的 RiskLevel，不靠工具名猜。
//
// 三档里只有 high_write 参与判定：readonly/low_write 一律 below_high 直接放过。
// 判据刻意收窄到"出域或不可逆"这一批，是因为这一层的产出要给 P9 决定要不要转阻断，
// 而"打开即改变现网行为"的面越小，评审越能看清单。
//
// 判定依据是 **Agent 自己的白名单**，不读全局白名单、不认 "*" 通配、不认 defaultAllow。
// 这不是把现有 checker 写错了重做一遍，而是问另一个问题：P9 要放行的高危工具，
// 依据必须是"这个 Agent 被逐条授权过它"。全局白名单里躺着 reach.sms.send、
// 或者某 Agent 配了 ["*"]，都不构成这种授权 —— 它们只说明今天没人拦。
package tooluse

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// riskModeOffLiteral 与 app 层的 riskGateOff 同值；在这里重述一份字面量，
// 只是为了让 tooluse 不必反向 import app（读法提示要按模式说话）。
const riskModeOffLiteral = "off"

// 判定理由。字符串是报告里可读的口径，P9 评审与观察端点共用这一份，不另写常量。
const (
	// RiskReasonBelowHigh 非 high_write，不参与判定。
	RiskReasonBelowHigh = "below_high"
	// RiskReasonGranted high_write 且在该 Agent 自己的白名单里。
	RiskReasonGranted = "agent_granted"
	// RiskReasonNotGranted high_write 且该 Agent 配了白名单、里面没有这个工具。
	RiskReasonNotGranted = "agent_whitelist_excluded"
	// RiskReasonWhitelistAbsent 该 Agent 一条白名单都没配 ⇒ 谈不上"被逐条授权过"。
	// 这是 would_deny 里的多数派，也是 P9 放量前必须先补的运营面。
	RiskReasonWhitelistAbsent = "agent_whitelist_absent"
	// RiskReasonNoAgent 连 AgentID 都取不到 ⇒ 判定无归因对象。与 absent 分开记，
	// 因为前者是"调用链没带上下文"（可能是接线漏洞），后者是"配置缺失"。
	RiskReasonNoAgent = "no_agent_id"
)

// AgentGrantReader 只读地回答"某个 Agent 自己被授权了哪些工具"。
//
// 由 *WhitelistPermissionChecker 实现。刻意不复用 PermissionChecker 接口：那个接口
// 的检查顺序里有全局白名单、"*" 通配和 defaultAllow 三级兜底，任何一级命中都返回 nil，
// 而分级要的恰恰是"这一条授权是不是这个 Agent 显式有的" —— 复用会把
// "全站默认放行"读成"这个 Agent 被批准发短信"。
type AgentGrantReader interface {
	AgentHasTool(agentID, toolName string) bool
	AgentWhitelistConfigured(agentID string) bool
	ListConfiguredAgents() []string
	ListAgentWhitelist(agentID string) []string
}

// RiskDecision 一次调用上的分级判定结果。
//
// Allowed 与 WouldDeny 必须同时存在且**不合并**：本卡 Allowed 恒真（无拒绝路径），
// WouldDeny 才是"若转阻断会被拦的量"。合并成一个字段的话，P9 手上就没有观察期数据了。
type RiskDecision struct {
	ToolName      string        `json:"tool_name"`
	Level         ToolRiskLevel `json:"level"`
	Declared      bool          `json:"declared"`
	AgentID       string        `json:"agent_id"`
	CallerID      string        `json:"caller_id"`
	SessionID     string        `json:"session_id"`
	Allowed       bool          `json:"allowed"`
	WouldDeny     bool          `json:"would_deny"`
	AgentWildcard bool          `json:"agent_wildcard"`
	Reason        string        `json:"reason"`
	At            time.Time     `json:"at"`
}

// RiskObserver 收集判定。实现方负责自己的并发与容量边界。
type RiskObserver interface {
	Observe(ctx context.Context, d RiskDecision)
}

// RiskVerdict 只做判定，不做拦截，也不填 Allowed/At（那是装饰器的事）。
//
// grants 为 nil 时不判：没有授权来源就没有结论，此时若照常返回 would_deny=true，
// 报告里会出现一批"没人配过白名单所以全都会拦"的假高地。
func RiskVerdict(t Tool, tc *ToolContext, grants AgentGrantReader) RiskDecision {
	d := RiskDecision{Level: RiskHighWrite, Declared: true}
	if t == nil || grants == nil {
		d.Reason = RiskReasonBelowHigh
		return d
	}
	d.ToolName = t.Name()
	level, declared := EffectiveRisk(t)
	d.Level, d.Declared = level, declared
	if tc != nil {
		d.AgentID, d.CallerID, d.SessionID = tc.AgentID, tc.CallerID, tc.SessionID
	}
	// 未声明按 high_write 参与判定（safe-by-default 的实际落点）：忘了分级不能成为免于评审的理由。
	if level != RiskHighWrite {
		d.Reason = RiskReasonBelowHigh
		return d
	}
	if d.AgentID == "" {
		d.WouldDeny = true
		d.Reason = RiskReasonNoAgent
		return d
	}
	d.AgentWildcard = grants.AgentHasTool(d.AgentID, "*")
	if !grants.AgentWhitelistConfigured(d.AgentID) {
		d.WouldDeny = true
		d.Reason = RiskReasonWhitelistAbsent
		return d
	}
	if grants.AgentHasTool(d.AgentID, d.ToolName) {
		d.Reason = RiskReasonGranted
		return d
	}
	d.WouldDeny = true
	d.Reason = RiskReasonNotGranted
	return d
}

// RiskGateDecorator 把判定挂到装饰器链上（位置与审批门同层：整条链之外）。
//
// **本函数不存在返回拒绝的路径**：结论只经 observer 流出，next 的结果原样透出。
// 这是 AC② 的结构保证，比"旗子现在是 shadow"强 —— 旗子能被运维改掉，代码里的路径不能。
// grants 或 observer 为 nil 时纯透传（= 未接线，行为与接线前逐字一致）。
func RiskGateDecorator(t Tool, grants AgentGrantReader, observer RiskObserver) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (ToolResult, error) {
			if grants == nil || observer == nil {
				return next(ctx, args)
			}
			j := RiskVerdict(t, GetToolContext(ctx), grants)
			j.Allowed = true
			j.At = time.Now()
			observer.Observe(ctx, j)
			return next(ctx, args)
		}
	}
}

// RiskToolCount 单个工具的观察累计。
type RiskToolCount struct {
	ToolName  string `json:"tool_name"`
	Level     string `json:"level"`
	Declared  bool   `json:"declared"`
	Calls     int64  `json:"calls"`
	WouldDeny int64  `json:"would_deny"`
	// WildcardDeny 是 would_deny 里"该 Agent 配了 ["*"]"的那一批：今天它被授权层放行，
	// 转阻断后会被本层拒掉 ⇒ 这个数是 P9 的破坏面下限。
	WildcardDeny int64 `json:"wildcard_deny"`
}

// MemoryRiskObserver 有界环形缓冲 + 无界计数。
//
// 快照有界是必须的：每条工具调用都留一行的话，不设上限就是一个跟着流量线性长的内存泄漏。
// 计数则相反，必须无界且不清零，否则攒不出"这批 Agent 里有多少会被拦"。
// 两者都不落库 ⇒ 进程重启即归零，报告里用 observations_persisted=false 显式说明。
type MemoryRiskObserver struct {
	mu       sync.Mutex
	max      int
	kept     []RiskDecision
	counters map[string]*RiskToolCount
}

func NewMemoryRiskObserver(max int) *MemoryRiskObserver {
	if max <= 0 {
		max = 1000
	}
	return &MemoryRiskObserver{max: max, counters: make(map[string]*RiskToolCount)}
}

func (o *MemoryRiskObserver) Observe(ctx context.Context, d RiskDecision) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.kept) >= o.max {
		copy(o.kept, o.kept[1:])
		o.kept = o.kept[:len(o.kept)-1]
	}
	o.kept = append(o.kept, d)
	if d.ToolName == "" {
		return
	}
	c, ok := o.counters[d.ToolName]
	if !ok {
		c = &RiskToolCount{ToolName: d.ToolName, Level: string(d.Level), Declared: d.Declared}
		o.counters[d.ToolName] = c
	}
	c.Calls++
	if d.WouldDeny {
		c.WouldDeny++
		if d.AgentWildcard {
			c.WildcardDeny++
		}
	}
}

// Snapshot 返回保留窗内的判定副本（ newest last）。
func (o *MemoryRiskObserver) Snapshot() []RiskDecision {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]RiskDecision, len(o.kept))
	copy(out, o.kept)
	return out
}

// Counts 返回按工具名累计的判定数（含已滚出保留窗的部分）。
func (o *MemoryRiskObserver) Counts() map[string]RiskToolCount {
	out := map[string]RiskToolCount{}
	if o == nil {
		return out
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for name, c := range o.counters {
		out[name] = *c
	}
	return out
}

// RiskReport 是 AC③ 的产出：P9 转阻断前的评审材料。
//
// 静态面（tools/by_level/undeclared）每次请求都从当前注册表实算，重启不丢；
// 动态面（counts）只有进程内的观察期数据，所以带 observations_persisted=false。
// 只看静态面会得出"45 个工具全声明了，可以阻断"的结论，而真正的阻断依据是
// "哪些 Agent 被逐条授权过哪些高危工具" —— 因此每个 high_write 工具都带授权 Agent 数，
// 且 agent_whitelist_configured=0 时 explicitly 报出来（那时 would_deny 必然是 100%）。
type RiskReport struct {
	Mode                 string `json:"mode"`
	BlocksWhenDenied     bool   `json:"blocks_when_denied"`
	EnforceFlagEnv       string `json:"enforce_flag_env"`
	AgentWhitelistConfig int    `json:"agent_whitelist_configured_agents"`

	Total      int            `json:"total_tools"`
	ByLevel    map[string]int `json:"by_level"`
	Undeclared []string       `json:"undeclared_tools"`
	HighWrite  []string       `json:"high_write_tools"`
	// HighWriteInGate 是 high_write 里被 IsColdOutreachTool 认成冷触达的个数，
	// 也就是今天审批门真的会去看的那一批。两个数一比就暴露分级要补的盲：
	// 实测 20 个 high_write 里只有 2 个在门内（reach.batch / reach.schedule），
	// 12 个 reach.*.send 单条外发工具全在门外 —— 它们出域、且不可撤回。
	HighWriteInGate int              `json:"high_write_in_approval_gate"`
	Tools           []RiskReportTool `json:"tools"`
	Counts          []RiskToolCount  `json:"counts"`

	// ReadingHint 把"这份数字容易被读错的地方"写在响应里（与 /agent/tools/approval
	// 的 reading_hint 同形）。灰度判定时最贵的错误不是数字缺失，而是把
	// "没在观察"读成"零次会被拦" —— 那句话必须由报告自己说出口。
	ReadingHint           []string `json:"reading_hint"`
	ObservationsPersisted bool     `json:"observations_persisted"`
	ObservationsRetained  int      `json:"observations_retained"`
}

// RiskReportTool 单个工具在报告里的一行。
type RiskReportTool struct {
	Name          string   `json:"name"`
	Category      string   `json:"category"`
	Level         string   `json:"level"`
	Declared      bool     `json:"declared"`
	ColdOutreach  bool     `json:"cold_outreach"`
	GrantedAgents int      `json:"granted_agents"`
	GrantedBy     []string `json:"granted_by,omitempty"`
	Calls         int64    `json:"calls"`
	WouldDeny     int64    `json:"would_deny"`
}

// BuildRiskReport 汇总静态声明 + 授权面 + 观察计数。
//
// grants 允许为 nil（未接权限检查器的退化场景），此时授权列全 0 且
// agent_whitelist_configured_agents=0，报告读起来就是"没有任何授权依据"。
func BuildRiskReport(registry *ToolRegistry, grants AgentGrantReader, obs *MemoryRiskObserver, mode string, blocksNow bool, flagEnv string) RiskReport {
	report := RiskReport{
		Mode:                  mode,
		BlocksWhenDenied:      blocksNow,
		EnforceFlagEnv:        flagEnv,
		ByLevel:               map[string]int{},
		Undeclared:            []string{},
		HighWrite:             []string{},
		Tools:                 []RiskReportTool{},
		Counts:                []RiskToolCount{},
		ObservationsPersisted: false,
	}
	if registry == nil {
		return report
	}
	grantedBy := map[string][]string{}
	if grants != nil {
		for _, agentID := range grants.ListConfiguredAgents() {
			report.AgentWhitelistConfig++
			for _, toolName := range grants.ListAgentWhitelist(agentID) {
				grantedBy[toolName] = append(grantedBy[toolName], agentID)
			}
		}
	}
	counts := obs.Counts()
	tools := registry.List()
	for _, t := range tools {
		level, declared := EffectiveRisk(t)
		row := RiskReportTool{
			Name:         t.Name(),
			Category:     string(t.Category()),
			Level:        string(level),
			Declared:     declared,
			ColdOutreach: IsColdOutreachTool(t),
		}
		report.Total++
		report.ByLevel[row.Level]++
		if !declared {
			report.Undeclared = append(report.Undeclared, row.Name)
		}
		if level == RiskHighWrite {
			report.HighWrite = append(report.HighWrite, row.Name)
			if row.ColdOutreach {
				report.HighWriteInGate++
			}
		}
		row.GrantedBy = grantedBy[row.Name]
		row.GrantedAgents = len(row.GrantedBy)
		if c, ok := counts[row.Name]; ok {
			row.Calls = c.Calls
			row.WouldDeny = c.WouldDeny
		}
		report.Tools = append(report.Tools, row)
	}
	for _, c := range counts {
		report.Counts = append(report.Counts, c)
	}
	// 报告要能两次阅读做 diff（观察期前后对比是 P9 的评审材料），所以所有数组都得是确定序。
	// registry.List() 自身已按名排序；这两处来自 map 遍历，不排序就会每次调用换顺序。
	sort.Slice(report.Counts, func(i, j int) bool { return report.Counts[i].ToolName < report.Counts[j].ToolName })
	for i := range report.Tools {
		sort.Strings(report.Tools[i].GrantedBy)
	}
	if snap := obs.Snapshot(); len(snap) > 0 {
		report.ObservationsRetained = len(snap)
	}
	report.ReadingHint = riskReadingHints(report)
	return report
}

// riskReadingHints 按当前这份报告的实数生成读法提示（不是固定文案表）。
//
// 每条都只在对应条件成立时出现：一份 mode=shadow、授权面齐备的报告不该被
// "未挂载观察层"这类提示淹没，否则提示本身会退化成人人略过的页脚。
func riskReadingHints(r RiskReport) []string {
	out := []string{}
	if r.Mode == riskModeOffLiteral {
		out = append(out, "mode=off：分级判定层未挂载，counts 为空是「没在观察」，不是「零次会被拦」")
	}
	if !r.ObservationsPersisted && r.Mode != riskModeOffLiteral {
		out = append(out, "观察计数只在进程内存里，重启归零：跨重启的对比不能靠这份报告")
	}
	if r.AgentWhitelistConfig == 0 {
		out = append(out, "没有任何 Agent 配置过白名单 ⇒ 转阻断后全部 high_write 调用都会被拒："+
			"would_deny=100% 是配置缺失的必然，不是流量异常")
	}
	if len(r.HighWrite) > 0 && r.HighWriteInGate < len(r.HighWrite) {
		out = append(out, fmt.Sprintf("high_write %d 个里只有 %d 个落在审批门的冷触达判据内："+
			"其余高危工具今天没有第二道闸门兜着，这正是 G-3 的补盲面", len(r.HighWrite), r.HighWriteInGate))
	}
	if len(r.Undeclared) > 0 {
		out = append(out, fmt.Sprintf("%d 个工具没有声明分级、按 high_write 兜底参与判定："+
			"它们是补声明的对象，不能当成「已评审过的高危」", len(r.Undeclared)))
	}
	return out
}

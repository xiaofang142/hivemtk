package approval

import (
	"context"
	"sort"
	"sync"
)

// decide() 返回的 Reason 取值口径。声明在本包（而非 whitelist.go）是为了让报表键名、
// 测试断言、运维端点提示语引用同一个符号——字面量分写两处时拼错的那一处不会编译失败，
// 只会让按 reason 拆开的报告少一类。
const (
	ReasonDisabledByFlag = "disabled_by_flag"
	ReasonDeniedDefault  = "denied_default"
	ReasonDeniedExplicit = "denied_explicit"
	ReasonWhitelisted    = "whitelisted"
)

// ToolStat 单个工具的审批门累计。
type ToolStat struct {
	ToolName   string
	Total      int64
	WouldDeny  int64
	LastReason string
}

// Report 是审批门观察期的汇总，供"切 block 前有多少调用会被拦"这类判定使用。
type Report struct {
	Total            int64
	WouldDeny        int64
	WouldDenyRatePct float64
	ByReason         map[string]int64
	PerTool          []ToolStat
}

// DecisionCounter 累计审批门判定结果，不改变任何判定。
//
// 为什么要单独有这个东西：T-P1-06 的准入条件是"先产出一轮 shadow 对比报告"。
// 只靠日志做这份报告要引入一个日志解析器（超出本次范围），且进程重启即失忆；
// 这个计数器把观察期的结论留在进程内，运维端点直接读得到。
//
// 口径：WouldDeny = !Decision.Allowed。**包含** reason=disabled_by_flag 的情况
// （即白名单旗子本身没开时的默认拒绝），因为那正是"切 block 后会立刻全拒"的量级；
// 报表按 reason 拆开，读的人不必猜这堆拒绝是"账号没被批准"还是"闸门没开"。
//
// 计数随进程重启清零，跨实例聚合不在本类型职责内。
type DecisionCounter struct {
	mu        sync.Mutex
	total     int64
	wouldDeny int64
	byReason  map[string]int64
	perTool   map[string]*ToolStat
}

// NewDecisionCounter 创建计数器。
func NewDecisionCounter() *DecisionCounter {
	return &DecisionCounter{
		byReason: make(map[string]int64),
		perTool:  make(map[string]*ToolStat),
	}
}

// Observe 记录一次判定，可用作 OnDecision 回调（签名一致）。
//
// ctx 目前不参与计算，保留形参是为了能直接当回调传进去、日后要按 trace 聚合不必改签名。
func (c *DecisionCounter) Observe(ctx context.Context, toolName, accountID string, d Decision) {
	_ = ctx
	_ = accountID
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byReason == nil {
		c.byReason = make(map[string]int64)
	}
	if c.perTool == nil {
		c.perTool = make(map[string]*ToolStat)
	}
	c.total++
	reason := d.Reason
	if reason == "" {
		reason = "unspecified"
	}
	c.byReason[reason]++
	st, ok := c.perTool[toolName]
	if !ok {
		st = &ToolStat{ToolName: toolName}
		c.perTool[toolName] = st
	}
	st.Total++
	st.LastReason = reason
	if !d.Allowed {
		c.wouldDeny++
		st.WouldDeny++
	}
}

// Report 返回当前累计的快照。nil 接收者返回零值报告（不 panic）。
func (c *DecisionCounter) Report() Report {
	if c == nil {
		return Report{ByReason: map[string]int64{}}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Report{
		Total:            c.total,
		WouldDeny:        c.wouldDeny,
		ByReason:         make(map[string]int64, len(c.byReason)),
		PerTool:          make([]ToolStat, 0, len(c.perTool)),
		WouldDenyRatePct: 0,
	}
	for k, v := range c.byReason {
		out.ByReason[k] = v
	}
	if c.total > 0 {
		out.WouldDenyRatePct = float64(c.wouldDeny) / float64(c.total) * 100
	}
	for _, st := range c.perTool {
		out.PerTool = append(out.PerTool, *st)
	}
	// 会被拦得最多的排前面——灰度时先处理头部工具，这一份顺序就是处置顺序。
	sort.Slice(out.PerTool, func(i, j int) bool {
		if out.PerTool[i].WouldDeny != out.PerTool[j].WouldDeny {
			return out.PerTool[i].WouldDeny > out.PerTool[j].WouldDeny
		}
		return out.PerTool[i].ToolName < out.PerTool[j].ToolName
	})
	return out
}

// Reset 清零累计（观察期结束后手工归零用）。
func (c *DecisionCounter) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total = 0
	c.wouldDeny = 0
	c.byReason = make(map[string]int64)
	c.perTool = make(map[string]*ToolStat)
}

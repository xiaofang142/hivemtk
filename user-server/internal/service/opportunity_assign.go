// opportunity_assign.go T-P4-05 AC③：自动分配销售，且**每一次分配都自带解释**。
//
// 为什么分配结果必须是返回值而不是日志行：本层的承诺是"事后答得出为什么是他"。
// 写在日志里的话，日志滚掉就没有了，而且它不会被任何契约测试钉住 ——
// 于是"改了规则但忘了改说明"这种分家在代码评审里完全看不见。
// OwnerAssignment 因此同时带出三样：命中的规则名、参与比较的候选、每人被比较时的负载数。
//
// 三条规则的**顺序**是判据不是偏好：
//  1. customer_owner —— 同一客户已有归属销售时沿用。换人 = 客户视角里之前的沟通作废，
//     这个代价远大于"负载不均"，所以它压过负载均衡，哪怕那位销售正忙到冒烟。
//  2. least_loaded —— 在册销售里在办商机最少者。**平票按 SalesID 字典序**，不随机：
//     随机会让"同参重放得到同一个人"失效，而重放正是唯一的排查手段。
//  3. no_roster —— 一个在册销售都没有：分不出人，但**这不是失败**。
//     该建的商机照样建，只是暂时无归属（赢率侧本就按未归属扣分）。
//
// 与规则三相对的是**故障**：名单读不到、负载读不到，一律原样上抛，绝不停到规则三去。
// 把"我们不知道有没有人"伪装成"确实没人"，后果是一批商机带着空归属与一条
// 从未发生过的"名单为空"记录落库 —— 运维查到的解释比故障本身更难排除。
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AssignRule 一次分配命中的规则名。值是**对外契约**（进响应体、进审计备注），
// 改名等于改一次前端判据，因此这里只列今日真实的三条。
type AssignRule string

const (
	// AssignRuleCustomerOwner 沿用该客户已有的归属销售。
	AssignRuleCustomerOwner AssignRule = "customer_owner"
	// AssignRuleLeastLoaded 在册销售里在办商机最少者（平票取 SalesID 字典序最小）。
	AssignRuleLeastLoaded AssignRule = "least_loaded"
	// AssignRuleNoRoster 在册销售为空：未分配，但本次分配成功返回。
	AssignRuleNoRoster AssignRule = "no_roster"
)

// ErrOwnerAssignerUnavailable 分配器未装配（名单或负载任一句柄为空）。
//
// 单独立一个 sentinel：它的修法是"补装配"，而下面两条依赖故障的修法是"查数据源"。
// 混在一起报，装配漏项就会伪装成一次数据库抖动。
var ErrOwnerAssignerUnavailable = errors.New("opportunity assign: 分配器未装配（在册销售或负载句柄缺失）")

// SalesRoster 在册销售名单。
//
// "在册"的判据由实现方定（本仓走 sales_events 里的 sales_profile 事件），
// 本层只要求它**分得清空名单与读不到**：返回 (nil, nil) 就是"确实没人"，
// 返回 error 就是"不知道有没有人"，两者在本层走的是完全不同的分支。
type SalesRoster interface {
	ActiveSalesIDs(ctx context.Context) ([]string, error)
}

// OwnerLoadReader 每个销售当前在办的商机数。
//
// 返回 map 里**缺席即为 0**（从没建过单的人不会出现在 GROUP BY 结果里），
// 本层不区分"缺席"与"0"，也不会因为某人缺席而把他排除出候选。
type OwnerLoadReader interface {
	OpenCountByOwner(ctx context.Context) (map[string]int, error)
}

// AssignRequest 一次分配请求。
//
// PreferredOwner 是"该客户已有的归属销售"，由调用方（转换那一步）从线索侧读来；
// ClueID 只在规则三的说明里出现，用于事后从"没分出去"回溯到具体那条线索。
type AssignRequest struct {
	PreferredOwner string `json:"preferred_owner,omitempty"`
	ClueID         string `json:"clue_id,omitempty"`
}

// OwnerCandidate 一个参与比较的销售，连同他被比较时使用的那个负载数。
type OwnerCandidate struct {
	SalesID   string `json:"sales_id"`
	OpenCount int    `json:"open_count"`
}

// OwnerAssignment 分配结论。四格缺一不可，理由见文件头。
type OwnerAssignment struct {
	OwnerUserID string           `json:"owner_user_id"`
	Rule        AssignRule       `json:"rule"`
	Reason      string           `json:"reason"`
	Candidates  []OwnerCandidate `json:"candidates"`
}

// OwnerAssigner 分配策略。无状态：每次 Assign 都现读名单与负载，
// 缓存它等于把"负载均衡"退化成"第一次读到的那份负载永远均衡"。
type OwnerAssigner struct {
	roster SalesRoster
	load   OwnerLoadReader
}

// NewOwnerAssigner 构造。两个句柄由装配侧注入，本层不自己去拿全局 DB。
func NewOwnerAssigner(roster SalesRoster, load OwnerLoadReader) *OwnerAssigner {
	return &OwnerAssigner{roster: roster, load: load}
}

// Available 报告是否可分配（装配回显用，不用于吞错）。
//
// 判据是**两个句柄都在**才算可分配：只给了名单的话，规则一会退化成"没有负载可读"，
// 而那条路径在本层是"上抛故障"——回显说"已装配"、第一次调用就报错，比直接说没装更难查。
func (a *OwnerAssigner) Available() bool {
	return a != nil && a.roster != nil && a.load != nil
}

// Assign 按三条规则定归属。详见文件头。
//
// 返回值是**值类型**而不是指针：规则三与出错时调用方都要读这个结构，
// 让调用方到处判 nil 换不来任何东西，只会漏判成 panic。
func (a *OwnerAssigner) Assign(ctx context.Context, req AssignRequest) (OwnerAssignment, error) {
	if !a.Available() {
		return OwnerAssignment{}, ErrOwnerAssignerUnavailable
	}
	ids, err := a.roster.ActiveSalesIDs(ctx)
	if err != nil {
		return OwnerAssignment{}, fmt.Errorf("opportunity assign: 读在册销售失败：%w", err)
	}
	sales := sanitizeSalesIDs(ids)
	if len(sales) == 0 {
		// 负载在这里不读：名单已经决定了结论，多一次查询只是多一个可能失败的分支。
		return OwnerAssignment{
			Rule:   AssignRuleNoRoster,
			Reason: fmt.Sprintf("在册销售为空，本单未分配归属（线索 %s）；商机照常落库，赢率按未归属扣减", req.ClueID),
		}, nil
	}
	loads, err := a.load.OpenCountByOwner(ctx)
	if err != nil {
		return OwnerAssignment{}, fmt.Errorf("opportunity assign: 读销售在办商机负载失败：%w", err)
	}
	candidates := make([]OwnerCandidate, len(sales))
	pos := make(map[string]int, len(sales))
	for i, id := range sales {
		candidates[i] = OwnerCandidate{SalesID: id, OpenCount: loads[id]}
		pos[id] = i
	}

	preferred := strings.TrimSpace(req.PreferredOwner)
	if preferred != "" {
		if idx, ok := pos[preferred]; ok {
			c := candidates[idx]
			return OwnerAssignment{
				OwnerUserID: c.SalesID,
				Rule:        AssignRuleCustomerOwner,
				Reason: fmt.Sprintf("沿用该客户已有归属销售 %s（在办商机 %d 个），未再走负载均衡",
					c.SalesID, c.OpenCount),
				Candidates: candidates,
			}, nil
		}
		picked := leastLoaded(candidates)
		return OwnerAssignment{
			OwnerUserID: picked.SalesID,
			Rule:        AssignRuleLeastLoaded,
			Reason: fmt.Sprintf("在册销售中查无 %s，回退负载均衡：选中 %s（在办商机 %d 个，候选 %d 人，平票取 SalesID 字典序最小）",
				preferred, picked.SalesID, picked.OpenCount, len(candidates)),
			Candidates: candidates,
		}, nil
	}

	picked := leastLoaded(candidates)
	return OwnerAssignment{
		OwnerUserID: picked.SalesID,
		Rule:        AssignRuleLeastLoaded,
		Reason: fmt.Sprintf("%d 名在册销售中选在办商机最少者：%s（%d 个），平票取 SalesID 字典序最小",
			len(candidates), picked.SalesID, picked.OpenCount),
		Candidates: candidates,
	}, nil
}

// sanitizeSalesIDs 洗名单：去空白项、去重复、按 ID 字典序排。
//
// 排序是**判据**而非美观：leastLoaded 靠"先遇到者胜"实现平票的字典序裁决，
// 那前提是输入已经有序。空白项尤其要挡 —— owner_user_id="" 在本表是"未归属"的
// 合法常态，一个"看起来分了、其实没分"的结论比"没分"更难查。
func sanitizeSalesIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	if len(raw) == 0 {
		return out
	}
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// leastLoaded 取负载最小者，平票取先到者；调用方保证 candidates 已按 SalesID 升序。
func leastLoaded(candidates []OwnerCandidate) OwnerCandidate {
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.OpenCount < best.OpenCount {
			best = c
		}
	}
	return best
}

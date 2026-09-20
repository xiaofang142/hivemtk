// opportunity_assign_test.go T-P4-05 AC③：分配结果的"可解释"不是日志里的一句话，
// 而是**返回值本身**必须带得动三样东西 —— 命中了哪条规则、参与比较的候选是谁、
// 每个人当时背了多少在办商机。少了任何一样，事后都答不出"为什么是他"。
//
// 这里全用假依赖（名单 / 负载各一条），真库那条腿在 opportunity_convert_test.go：
// 分配本身不落库，落库是转换那一步的事，把 DB 扯进来只会让"规则顺序"这五条判据
// 变成"规则顺序 + 迁移是否正确"的合取，红的时候分不清是哪一半。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// —— 夹具 ——————————————————————————————————————————————

type fakeRoster struct {
	ids []string
	err error
}

func (f fakeRoster) ActiveSalesIDs(context.Context) ([]string, error) { return f.ids, f.err }

type fakeLoad struct {
	counts map[string]int
	err    error
}

func (f fakeLoad) OpenCountByOwner(context.Context) (map[string]int, error) { return f.counts, f.err }

func newTestAssigner(roster SalesRoster, load OwnerLoadReader) *OwnerAssigner {
	return NewOwnerAssigner(roster, load)
}

// —— AC③：三条规则各自的形状 ————————————————————————————

// TestAssignOwnerPrefersCustomerPriorOwner 规则一：同客户同销售，**优先于**负载均衡。
//
// 反例是这里唯一有杀伤力的另一种实现：先按负载选，只有负载读不到时才退回沿用。
// 那样"客户的销售"会在每一次新商机时被换掉，而客户视角里换人 = 之前的沟通作废。
// 所以这一条断的不是"选了 sales_a"，而是"负载更低的那个人没被选"。
func TestAssignOwnerPrefersCustomerPriorOwner(t *testing.T) {
	a := newTestAssigner(
		fakeRoster{ids: []string{"sales_a", "sales_b"}},
		fakeLoad{counts: map[string]int{"sales_a": 9, "sales_b": 0}},
	)
	got, err := a.Assign(context.Background(), AssignRequest{PreferredOwner: "sales_a", ClueID: "clue_1"})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if got.OwnerUserID != "sales_a" {
		t.Errorf("沿用了别的销售：%+v —— 负载最低不等于客户最熟", got)
	}
	if got.Rule != AssignRuleCustomerOwner {
		t.Errorf("命中规则记成 %q，期望 %q", got.Rule, AssignRuleCustomerOwner)
	}
	if !strings.Contains(got.Reason, "sales_a") || !strings.Contains(got.Reason, "9") {
		t.Errorf("理由没把「为什么是他」说清楚：%q", got.Reason)
	}
}

// TestAssignOwnerChoosesLeastLoadedWithDeterministicTieBreak 规则二：负载最少者；
// 平票按 SalesID 字典序。
//
// 平票不随机是**判据**不是风格：随机选人时，AC③ 的"事后重放同一入参应当得到同一个人"
// 直接失效，而那条重放正是排查"这单为什么落在他手上"的唯一办法。
func TestAssignOwnerChoosesLeastLoadedWithDeterministicTieBreak(t *testing.T) {
	roster := fakeRoster{ids: []string{"zeta", "alpha", "mid"}}
	load := fakeLoad{counts: map[string]int{"zeta": 3, "alpha": 1, "mid": 1}}

	first, err := newTestAssigner(roster, load).Assign(context.Background(), AssignRequest{})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if first.OwnerUserID != "alpha" {
		t.Errorf("平票没按字典序： %+v", first)
	}
	if first.Rule != AssignRuleLeastLoaded {
		t.Errorf("命中规则记成 %q，期望 %q", first.Rule, AssignRuleLeastLoaded)
	}
	// 名单顺序换一下、结论必须不变：字典序判的是 ID，不是"谁先被列出来"。
	shuffled, err := newTestAssigner(fakeRoster{ids: []string{"mid", "zeta", "alpha"}}, load).
		Assign(context.Background(), AssignRequest{})
	if err != nil {
		t.Fatalf("打乱名单后报错：%v", err)
	}
	if shuffled.OwnerUserID != first.OwnerUserID {
		t.Errorf("同一家两个人、同一份负载，名单顺序却决定了归属：%q vs %q",
			first.OwnerUserID, shuffled.OwnerUserID)
	}

	// 负载真的更低时，字典序要让位。
	if heavier, err := newTestAssigner(roster, fakeLoad{counts: map[string]int{
		"zeta": 0, "alpha": 5, "mid": 5,
	}}).Assign(context.Background(), AssignRequest{}); err != nil {
		t.Fatalf("负载分配报错：%v", err)
	} else if heavier.OwnerUserID != "zeta" {
		t.Errorf("负载更低的人没被选上：%+v", heavier)
	}
}

// TestAssignOwnerEmptyRosterAssignsNobodyButSucceeds 名单为空 ⇒ 分不出人，但**不是失败**。
//
// 这一条刻意与"故障"分家：没人可分是一个事实（该建商机还是该建，缺的只是归属），
// 而"名单查询挂了"是另一个事实（我们根本不知道有没有人）。前者继续、后者停。
func TestAssignOwnerEmptyRosterAssignsNobodyButSucceeds(t *testing.T) {
	got, err := newTestAssigner(fakeRoster{ids: nil}, fakeLoad{counts: map[string]int{}}).
		Assign(context.Background(), AssignRequest{ClueID: "clue_9"})
	if err != nil {
		t.Fatalf("空名单被当成故障上抛了：%v —— 它会让一条合格线索因为排班问题直接消失", err)
	}
	if got.OwnerUserID != "" {
		t.Errorf("名单为空却分出了人：%q", got.OwnerUserID)
	}
	if got.Rule != AssignRuleNoRoster {
		t.Errorf("命中规则记成 %q，期望 %q", got.Rule, AssignRuleNoRoster)
	}
	if !strings.Contains(got.Reason, "clue_9") {
		t.Errorf("没分出去的理由里查不到这条线索：%q", got.Reason)
	}
	if len(got.Candidates) != 0 {
		t.Errorf("名单为空却有候选：%+v", got.Candidates)
	}
}

// TestAssignOwnerRosterFailureIsNotEmptyRoster 名单查询故障必须原样上抛。
//
// 判据与上一条同形：把故障读成"没人"，后果是一条条合格商机带着"无归属"的赢率
// 惩罚落库，而运维在日志里看见的是"名单为空"——一个从未发生过的事实。
func TestAssignOwnerRosterFailureIsNotEmptyRoster(t *testing.T) {
	want := errors.New("roster down")
	_, err := newTestAssigner(fakeRoster{err: want}, fakeLoad{counts: map[string]int{}}).
		Assign(context.Background(), AssignRequest{})
	if !errors.Is(err, want) {
		t.Errorf("名单故障被吞了或改写了：得到 %v，期望 %v", err, want)
	}
}

// TestAssignOwnerLoadFailureStops 负载读不到时**不改用别的规则**，直接停。
//
// 另一种实现是"退化成字典序分配"，它看起来更"可用"，代价是 AC③ 当场失效：
// 同一个入参会因为一次瞬时故障落到不同的人手上，而命中的规则名还会写成 least_loaded。
func TestAssignOwnerLoadFailureStops(t *testing.T) {
	want := errors.New("load down")
	got, err := newTestAssigner(
		fakeRoster{ids: []string{"alpha", "zeta"}},
		fakeLoad{err: want},
	).Assign(context.Background(), AssignRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("负载故障被降级处理了：err=%v", err)
	}
	if got.OwnerUserID != "" || got.Rule != "" {
		t.Errorf("停下来的那次仍然给出了归属：%+v", got)
	}
}

// TestAssignOwnerSanitizesRoster 名单里的空串与重复项不得成为归属。
//
// 空串尤其危险：owner_user_id="" 在本表是"未归属"的合法常态（赢率据此扣 0.10），
// 一个"看起来分了、其实没分"的结论比"没分"更难查。
func TestAssignOwnerSanitizesRoster(t *testing.T) {
	got, err := newTestAssigner(
		fakeRoster{ids: []string{"", "  ", "\t", "beta", "beta", "alpha"}},
		fakeLoad{counts: map[string]int{"beta": 0, "alpha": 0}},
	).Assign(context.Background(), AssignRequest{})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if got.OwnerUserID == "" {
		t.Error("名单里有两个有效销售却没分出去")
	}
	if strings.TrimSpace(got.OwnerUserID) != got.OwnerUserID || got.OwnerUserID == "" {
		t.Errorf("归属是个空白值：%q", got.OwnerUserID)
	}
	if len(got.Candidates) != 2 {
		t.Errorf("候选没去重/没去空：%+v，期望恰好 alpha 与 beta 两个", got.Candidates)
	}
	for _, c := range got.Candidates {
		if c.SalesID == "beta" && c.OpenCount != 0 {
			t.Errorf("beta 的负载读错了：%+v", c)
		}
	}
}

// TestAssignOwnerPreferredNotInRosterFallsThrough 沿用的对象**必须在册**才作数。
//
// 不查名单就照单全收是另一种写法，它更"贴心"（客户认识的人不会被换掉），
// 但代价是一行 owner_user_id 指向一个已经不在销售体系里的人 ——
// 本表里这个值不是历史字段而是工作台与赢率的输入，指向不在册的人等于这单没人推进，
// 而所有下游读到的都是"已归属"。
func TestAssignOwnerPreferredNotInRosterFallsThrough(t *testing.T) {
	got, err := newTestAssigner(
		fakeRoster{ids: []string{"alpha", "zeta"}},
		fakeLoad{counts: map[string]int{"alpha": 4, "zeta": 4}},
	).Assign(context.Background(), AssignRequest{PreferredOwner: "gone_sales", ClueID: "clue_7"})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if got.Rule == AssignRuleCustomerOwner {
		t.Errorf("把商机交给了不在册的销售：%+v", got)
	}
	if got.OwnerUserID != "alpha" {
		t.Errorf("回退后没走负载均衡：%+v（平票应选字典序最小的 alpha）", got)
	}
	if !strings.Contains(got.Reason, "gone_sales") {
		t.Errorf("理由里查不到那个被放弃的沿用对象，事后无法解释为什么换了人：%q", got.Reason)
	}
}

// TestAssignOwnerTrimsPreferredOwner 带空白的沿用对象要能洗成在册的那一个。
//
// 判据不是洁癖：preferred 来自上一行的 owner_user_id 或调用方传值，两处都可能带出空白；
// 不洗的话它就落进"不在册"那一支，一次本该沿用老销售的分单变成换人。
func TestAssignOwnerTrimsPreferredOwner(t *testing.T) {
	got, err := newTestAssigner(
		fakeRoster{ids: []string{"sales_a", "sales_b"}},
		fakeLoad{counts: map[string]int{"sales_a": 2, "sales_b": 0}},
	).Assign(context.Background(), AssignRequest{PreferredOwner: " sales_a "})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if got.OwnerUserID != "sales_a" || got.Rule != AssignRuleCustomerOwner {
		t.Errorf("带空白的沿用对象没洗成在册销售：%+v", got)
	}
}

// TestAssignOwnerCandidatesAreStableAndSorted AC③ 的落点在这一格：
// 可解释要求"参与比较的每个人都带着他被比较时使用的那个数"出现，且顺序稳定。
//
// 顺序不稳定的话，同一份结论两次渲染出的 JSON 不同，前端拿它做幂等渲染就会闪；
// 而少一个人不进候选，事后就答不出"为什么不是他"。
func TestAssignOwnerCandidatesAreStableAndSorted(t *testing.T) {
	roster := fakeRoster{ids: []string{"gamma", "alpha", "beta"}}
	load := fakeLoad{counts: map[string]int{"alpha": 2, "beta": 0}} // gamma 一个都没建过
	got, err := newTestAssigner(roster, load).Assign(context.Background(), AssignRequest{})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("候选少了人：%+v —— 缺席的人无法被事后解释", got.Candidates)
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, c := range got.Candidates {
		if c.SalesID != want[i] {
			t.Errorf("候选第 %d 位是 %q，期望 %q（按 ID 字典序）", i, c.SalesID, want[i])
		}
	}
	// 没有负载记录的那个人必须显式给出 0，而不是留一个读不出来的形状。
	if got.Candidates[2].OpenCount != 0 {
		t.Errorf("从未建过单的销售负载读成 %d，期望 0", got.Candidates[2].OpenCount)
	}
	if got.Candidates[0].OpenCount != 2 || got.Candidates[1].OpenCount != 0 {
		t.Errorf("负载数字错位：%+v", got.Candidates)
	}
}

// TestAssignOwnerUnavailableAssigner 未装配：报错而不是回一个"没名单"的结论。
//
// 与 T-P4-04 在 HTTP 侧立的同一条判据：「没装底座」与"底座说没人"是两件事，
// 混在一起就会让装配漏项伪装成一次合法的 no_roster。
func TestAssignOwnerUnavailableAssigner(t *testing.T) {
	if _, err := (&OwnerAssigner{}).Assign(context.Background(), AssignRequest{}); err == nil {
		t.Error("未装配的分配器给出了结论：装配漏项会被读成「这个商户没有销售」")
	}
	var nilA *OwnerAssigner
	if _, err := nilA.Assign(context.Background(), AssignRequest{}); err == nil {
		t.Error("nil 分配器回了 nil 错误：这道关的第二臂没人守")
	}
	if (&OwnerAssigner{}).Available() {
		t.Error("未装配的分配器自称 Available —— 装配回显会跟着说谎")
	}
}

// TestAssignRequestPreferredOwnerMustBeNonBlankBeforeItWins 规则一的入参也要洗：
// 一个空白的"沿用对象"不能把分配变成"归属是空白的商机"。
func TestAssignRequestPreferredOwnerMustBeNonBlankBeforeItWins(t *testing.T) {
	got, err := newTestAssigner(
		fakeRoster{ids: []string{"alpha"}},
		fakeLoad{counts: map[string]int{"alpha": 7}},
	).Assign(context.Background(), AssignRequest{PreferredOwner: "   "})
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	if got.Rule == AssignRuleCustomerOwner {
		t.Errorf("空白值被当成了「该客户已有的归属销售」：%+v", got)
	}
	if got.OwnerUserID != "alpha" {
		t.Errorf("空白沿用对象挤掉了正常分配：%+v", got)
	}
}

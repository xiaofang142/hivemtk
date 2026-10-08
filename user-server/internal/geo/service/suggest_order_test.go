package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// 下拉词抓取的确定性不变量。
//
// 两处随机源：
//  1. 抓取是并发的，results 的到达序＝各引擎完成序，每轮不同 ⇒ 同一个词被多引擎命中时，
//     「谁先到」决定了落库行的 source 和 suggest_engines 的元素序；
//  2. 去重后原本把 map 直接摊成切片返回，Go 每轮随机化 range 序 ⇒ 接口返回的词序也在抖。
//
// 修法是给每条产出带 (种子, 引擎, 引擎内序号) 三元组，先 sortSuggestResults 定序，
// 再由 mergeSuggestResults 按首次出现顺序追加。
// 改动前的实测：同一组种子连发三次，返回顺序与每行 source 都不一致。

func suggestRow(seedIdx, engIdx int, engine, kw string, kwIdx int) suggestResult {
	return suggestResult{
		keyword: kw, source: "suggest_" + engine, engine: engine,
		seed:    fmt.Sprintf("seed%d", seedIdx),
		seedIdx: seedIdx, engIdx: engIdx, kwIdx: kwIdx,
	}
}

// engineBatch 造一个引擎的连续产出（kwIdx 递增）。
func engineBatch(seedIdx, engIdx int, engine string, kws ...string) []suggestResult {
	out := make([]suggestResult, 0, len(kws))
	for i, kw := range kws {
		out = append(out, suggestRow(seedIdx, engIdx, engine, kw, i))
	}
	return out
}

// arriveReversed 模拟「最后派发的引擎最先返回」：把各批次按派发序倒过来拼接。
func arriveReversed(batches ...[]suggestResult) []suggestResult {
	var out []suggestResult
	for i := len(batches) - 1; i >= 0; i-- {
		out = append(out, batches[i]...)
	}
	return out
}

func sixBatches() [][]suggestResult {
	return [][]suggestResult{
		engineBatch(0, 0, "baidu", "s0-b0", "s0-b1"),
		engineBatch(0, 1, "bing", "s0-i0", "s0-i1"),
		engineBatch(0, 2, "google", "s0-g0", "s0-g1"),
		engineBatch(1, 0, "baidu", "s1-b0", "s1-b1"),
		engineBatch(1, 1, "bing", "s1-i0", "s1-i1"),
		engineBatch(1, 2, "google", "s1-g0", "s1-g1"),
	}
}

var wantSuggestOrder = []string{
	"s0-b0", "s0-b1", "s0-i0", "s0-i1", "s0-g0", "s0-g1",
	"s1-b0", "s1-b1", "s1-i0", "s1-i1", "s1-g0", "s1-g1",
}

func TestSortSuggestResults_OrderIgnoresArrivalSequence(t *testing.T) {
	arrived := arriveReversed(sixBatches()...)

	// 前提自证：不经过定序时，merge 只会照搬到达序（＝倒序），
	// 否则下面那条断言是在给 merge 记功，不是在测 sort。
	naive := keywordsOf(mergeSuggestResults(arrived).Keywords)
	if strings.Join(naive, ",") == strings.Join(wantSuggestOrder, ",") {
		t.Fatalf("乱序输入本该得到乱序输出，却已经是期望序 ⇒ 本用例的前置没成立，判据没牙")
	}

	sortSuggestResults(arrived)
	got := keywordsOf(mergeSuggestResults(arrived).Keywords)
	if strings.Join(got, ",") != strings.Join(wantSuggestOrder, ",") {
		t.Fatalf("定序后应是 种子→引擎→序号：\n  got =%v\n  want=%v", got, wantSuggestOrder)
	}
}

func TestMergeSuggestResults_EngineListIsEngineOrderNotArrival(t *testing.T) {
	// 同一个词被 google(engIdx=2)、bing(engIdx=1)、baidu(engIdx=0) 依次「先完成」，
	// 定序后 baidu 排最前 ⇒ 落库行的 source 必须是 baidu，引擎列表必须是 [baidu bing google]。
	// 改动前这里会随完成序写成 suggest_google / ["google","bing","baidu"]。
	results := []suggestResult{
		suggestRow(0, 2, "google", "共享词", 0),
		suggestRow(0, 1, "bing", "共享词", 0),
		suggestRow(0, 0, "baidu", "共享词", 0),
	}
	sortSuggestResults(results)
	out := mergeSuggestResults(results)

	if len(out.Keywords) != 1 {
		t.Fatalf("同词应合并成 1 行，实际 %d", len(out.Keywords))
	}
	row := out.Keywords[0]
	if row.Source != "suggest_baidu" {
		t.Fatalf("source 应是定序后的首个引擎 baidu，实际=%q（＝还在按到达序取第一个）", row.Source)
	}
	var engs []string
	if err := json.Unmarshal([]byte(row.SuggestEngines), &engs); err != nil {
		t.Fatalf("suggest_engines 解析失败：%v raw=%s", err, row.SuggestEngines)
	}
	if strings.Join(engs, ",") != "baidu,bing,google" {
		t.Fatalf("引擎列表应按引擎序追加，实际=%v", engs)
	}
	if row.SuggestCount != 3 {
		t.Fatalf("SuggestCount 应等于覆盖引擎数 3，实际=%d", row.SuggestCount)
	}
	if out.PerEngine["baidu"] != 1 || out.PerEngine["bing"] != 1 || out.PerEngine["google"] != 1 {
		t.Fatalf("PerEngine 应逐引擎各计一条，实际=%v", out.PerEngine)
	}
}

func TestSuggestMerge_StableAcrossRoundShuffles(t *testing.T) {
	// 每轮换一种「到达序」，定序 + 合并后的输出必须恒等（原缺陷：range map 每轮随机）。
	arrivals := [][]int{
		{0, 1, 2, 3, 4, 5},
		{5, 4, 3, 2, 1, 0},
		{3, 5, 0, 4, 1, 2},
		{2, 0, 5, 1, 4, 3},
	}
	for ai, perm := range arrivals {
		batches := sixBatches()
		var arrived []suggestResult
		for _, bi := range perm {
			arrived = append(arrived, batches[bi]...)
		}
		sortSuggestResults(arrived)
		got := keywordsOf(mergeSuggestResults(arrived).Keywords)
		if strings.Join(got, ",") != strings.Join(wantSuggestOrder, ",") {
			t.Fatalf("到达序 #%d（%v）下输出漂移：got=%v", ai, perm, got)
		}
	}
	assertStableAcrossRounds(t, "crawl-suggest 定序合并", func() string {
		arrived := arriveReversed(sixBatches()...)
		sortSuggestResults(arrived)
		return strings.Join(keywordsOf(mergeSuggestResults(arrived).Keywords), ",")
	})
}

func TestMergeSuggestResults_KeepsErrorsDiagnostics(t *testing.T) {
	// 撤掉 Errors/PerEngine 的诊断面就回到「count=0 分不清上游全挂还是真没词」的老口径，
	// 这里锁住两个字段在重构后仍被填充。
	results := []suggestResult{
		{err: fmt.Errorf("[baidu] seed0: boom"), seedIdx: 0, engIdx: 0},
		suggestRow(0, 1, "bing", "ok1", 0),
		{keyword: "   ", engine: "sogou", seedIdx: 0, engIdx: 4},
	}
	sortSuggestResults(results)
	out := mergeSuggestResults(results)
	if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], "boom") {
		t.Fatalf("抓取报错应逐条进 Errors，实际=%v", out.Errors)
	}
	if len(out.Keywords) != 1 || out.Keywords[0].Keyword != "ok1" {
		t.Fatalf("空词与报错行不该进结果集，实际=%v", keywordsOf(out.Keywords))
	}
	if out.PerEngine["bing"] != 1 || len(out.PerEngine) != 1 {
		t.Fatalf("PerEngine 只应计到 bing，实际=%v", out.PerEngine)
	}
}

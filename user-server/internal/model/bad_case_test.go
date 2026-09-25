// bad_case_test.go Bad Case 模型层的值域与状态机判据（T-P8-03 / AC③ 的"表本身可测"）。
//
// 这一层没有 DB，测的是三件"写歪了整条闭环就没人拦得住"的事：
// 状态迁移表、类目→责任层的映射、幂等键的形态。
package model

import "testing"

func TestBadCase_StatusesAreClosed(t *testing.T) {
	for _, s := range BadCaseStatuses {
		if !IsKnownBadCaseStatus(s) {
			t.Errorf("状态 %q 在 BadCaseStatuses 里却判未知", s)
		}
	}
	for _, bogus := range []string{"", "DONE", "labeled_", "Pending"} {
		if IsKnownBadCaseStatus(bogus) {
			t.Errorf("未知状态 %q 被判成已知", bogus)
		}
	}
	// 迁移表的键必须与状态全集**一模一样**：少一个键意味着那个状态没人能迁出去，
	// 而 CanBadCaseTransition 对未知态回 false，于是那条行永远卡在队列里不出货。
	for _, s := range BadCaseStatuses {
		if _, ok := BadCaseTransitions[s]; !ok {
			t.Errorf("状态 %q 在 BadCaseTransitions 里没有键", s)
		}
	}
	for from, targets := range BadCaseTransitions {
		if !IsKnownBadCaseStatus(from) {
			t.Errorf("迁移表出现未知起点 %q", from)
		}
		for _, to := range targets {
			if !IsKnownBadCaseStatus(to) {
				t.Errorf("%s → %q：目标态不在状态全集里", from, to)
			}
		}
	}
}

func TestBadCase_Transitions(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{BadCaseStatusPending, BadCaseStatusLabeled, true},
		{BadCaseStatusPending, BadCaseStatusDismissed, true},
		{BadCaseStatusLabeled, BadCaseStatusExported, true},
		{BadCaseStatusLabeled, BadCaseStatusDismissed, true},
		// "不许"的每一条都挡一类实际会发生的错：
		{BadCaseStatusPending, BadCaseStatusExported, false},  // 没判过的不能直接进评测集
		{BadCaseStatusExported, BadCaseStatusLabeled, false},  // 已交付的样本不许回炉改结论
		{BadCaseStatusDismissed, BadCaseStatusLabeled, false}, // 判过"不是坏例"不许重开
		{BadCaseStatusExported, BadCaseStatusExported, false},
		// 下面三条是补的：上一版只断了"往前的不许回头"，没断"原地不动"与"退回 pending"，
		// 于是把 BadCaseStatusDismissed 的键值写成 {pending}（= 撤销后重新排队）在这一层测不出来，
		// 而队列里就会同时出现"人判过了"和"还欠一次判定"两种读法。
		{BadCaseStatusDismissed, BadCaseStatusPending, false},
		{BadCaseStatusExported, BadCaseStatusPending, false},
		{BadCaseStatusLabeled, BadCaseStatusPending, false},
		{BadCaseStatusPending, BadCaseStatusPending, false},
		{BadCaseStatusLabeled, BadCaseStatusLabeled, false},
		{BadCaseStatusDismissed, BadCaseStatusDismissed, false},
		{"bogus", BadCaseStatusLabeled, false},
	}
	for _, c := range cases {
		if got := CanBadCaseTransition(c.from, c.to); got != c.want {
			t.Errorf("CanBadCaseTransition(%s, %s)=%v，期望 %v", c.from, c.to, got, c.want)
		}
	}
}

func TestBadCase_EveryLabelHasAFixLayerAndEveryLayerIsReachable(t *testing.T) {
	for _, label := range BadCaseLabels {
		layer := FixLayerOfLabel(label)
		if layer == "" {
			t.Errorf("类目 %q 没有责任层：导出评测集时这一行的 fix_layer 会是空串", label)
			continue
		}
		if !badCaseIn(BadCaseFixLayers, layer) {
			t.Errorf("类目 %q 指向未知责任层 %q", label, layer)
		}
	}
	// 反向：四层里如果有哪层没有任何类目指向它，看板上那一格就永远是 0，
	// 而"0"会被读成"这一层没问题"—— 它真正的问题是没人判得出来。
	reached := map[string]bool{}
	for _, layer := range BadCaseFixLayers {
		reached[layer] = false
	}
	for _, layer := range BadCaseLabelFixLayer {
		reached[layer] = true
	}
	for layer, ok := range reached {
		if !ok {
			t.Errorf("责任层 %q 没有任何类目指向它", layer)
		}
	}
	// 逐类目点名期望层，而不是只断"非空且在四层里"：把 retrieve_miss 接到 knowledge 上，
	// 上面两段循环照旧全绿，而这份映射正是本卡交付给 P8 的落点（找算法还是找知识库）。
	// 这张期望表是**独立抄的一份**，与 BadCaseLabelFixLayer 同义不同源：两表同时写歪的概率
	// 远低于一处写歪，改类目归属时必须两处一起改（这就是"改错了要有人拦"）。
	wantLayer := map[string]string{
		BadCaseLabelKBMissing:       BadCaseFixLayerKnowledge,
		BadCaseLabelKBStale:         BadCaseFixLayerKnowledge,
		BadCaseLabelRetrieveMiss:    BadCaseFixLayerRetrieval,
		BadCaseLabelRetrieveWrong:   BadCaseFixLayerRetrieval,
		BadCaseLabelGenerationWrong: BadCaseFixLayerGeneration,
		BadCaseLabelGenerationStyle: BadCaseFixLayerGeneration,
		BadCaseLabelIntentMisjudge:  BadCaseFixLayerIntent,
	}
	if len(wantLayer) != len(BadCaseLabels) {
		t.Fatalf("期望表 %d 条、类目全集 %d 条，两表规模已对不上", len(wantLayer), len(BadCaseLabels))
	}
	for label, want := range wantLayer {
		if got := FixLayerOfLabel(label); got != want {
			t.Errorf("类目 %q 的责任层是 %q，期望 %q", label, got, want)
		}
	}
	if FixLayerOfLabel("not_a_label") != "" {
		t.Error("未知类目必须回空串（回某个层等于把脏数据悄悄归进那一层）")
	}
}

func TestBadCase_SourcesAreClosed(t *testing.T) {
	for _, s := range BadCaseSources {
		if !IsKnownBadCaseSource(s) {
			t.Errorf("来源 %q 判未知", s)
		}
	}
	if IsKnownBadCaseSource("lowConfidence") {
		t.Error("驼峰写法不该被认成 low_confidence")
	}
}

func TestBadCaseDedupKey(t *testing.T) {
	key, ok := BadCaseDedupKey(BadCaseSourceLowConfidence, "sess_1", "msg_1")
	if !ok || key != "low_confidence|sess_1|msg_1" {
		t.Errorf("自动来源的键形态不对：%q ok=%v", key, ok)
	}
	// 同一轮不同来源必须给两条不同的键：一条既低置信又零命中的回答，
	// 若两判据抢同一键，第二条就被"已存在"吃掉，于是"零命中有多少条"永远少算。
	k2, _ := BadCaseDedupKey(BadCaseSourceZeroHit, "sess_1", "msg_1")
	if k2 == key {
		t.Error("两个来源共用了同一条幂等键")
	}
	// 自动来源缺 message_id ⇒ 拒绝（不是给一条空串键）：
	// 空串键会把所有"拿不到消息 id"的轮次挤成一行，第一条之后全被静默吞掉。
	if _, ok := BadCaseDedupKey(BadCaseSourceLowConfidence, "sess_1", ""); ok {
		t.Error("自动来源缺 message_id 必须判不可用")
	}
	if _, ok := BadCaseDedupKey(BadCaseSourceLowConfidence, "sess_1", "   "); ok {
		t.Error("全空格的 message_id 等同缺失，不许生成键")
	}
	// 手动来源可以没有 message_id：那是人补录的一条，键用行 id（调用方保证唯一）。
	manual, ok := BadCaseDedupKey(BadCaseSourceManual, "sess_1", "")
	if !ok || manual != "manual|sess_1|" {
		t.Errorf("手动来源允许空 message_id，得到 %q ok=%v", manual, ok)
	}
}

func TestBadCase_TableName(t *testing.T) {
	if got := (BadCase{}).TableName(); got != "bad_cases" {
		t.Errorf("表名 %q", got)
	}
}

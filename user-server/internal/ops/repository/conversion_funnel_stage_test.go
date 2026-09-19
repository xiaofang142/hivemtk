package repository

// 阶段词表的契约测试（T-P2-03 / R-4 AC③）。
//
// 这份测试的目的不是"覆盖代码"，而是把三条**只有在这里才守得住**的约束钉住：
// ① 对外契约的取值与顺序（改一个字母就是改 `GET /conversion-funnel` 的响应）；
// ② "已定名、未产出"的保留位必须真的不产出（否则平白多出一个恒为 0 的阶段）；
// ③ 演示表那套阶段名不得混进词表（混进来 = R-4 要收的双源又长回来了）。

import (
	"reflect"
	"testing"
)

func TestFunnelStageVocabulary_LiveOrderIsTheResponseContract(t *testing.T) {
	got := LiveFunnelStages()

	// 值与顺序逐位比对：响应里 stages[] 的次序就是这里的次序。
	wantKeys := []FunnelStageKey{StageVisit, StageClue, StageIntent, StageSession}
	if !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("产出阶段变了：%v ≠ %v", got, wantKeys)
	}
	if string(StageVisit) != "visit" || string(StageClue) != "clue" ||
		string(StageIntent) != "intent" || string(StageSession) != "session" {
		t.Fatalf("阶段键的**字面值**是对外契约，不得改：visit=%q clue=%q intent=%q session=%q",
			StageVisit, StageClue, StageIntent, StageSession)
	}
}

func TestFunnelStageVocabulary_Labels(t *testing.T) {
	wantLabels := map[FunnelStageKey]string{
		StageVisit:       "访问",
		StageClue:        "线索",
		StageIntent:      "意向",
		StageSession:     "会话",
		StageOpportunity: "商机",
	}
	for k, want := range wantLabels {
		if got := k.Label(); got != want {
			t.Errorf("阶段 %s 中文名应为 %q，实际 %q", k, want, got)
		}
	}
	// 中文名不得重复：看板图例里两个同名阶段无法区分。
	seen := map[string]FunnelStageKey{}
	for _, k := range append(LiveFunnelStages(), ReservedFunnelStages()...) {
		label := k.Label()
		if label == "" {
			t.Errorf("阶段 %s 没有中文名", k)
			continue
		}
		if prev, dup := seen[label]; dup {
			t.Errorf("阶段 %s 与 %s 共用中文名 %q", prev, k, label)
		}
		seen[label] = k
	}
	// 未知键不编名字（详情分支对"未登记阶段"就依赖这个空串口径）。
	if unknown := FunnelStageKey("exposure"); unknown.Label() != "" {
		t.Errorf("未知阶段不该有中文名，实际 %q", unknown.Label())
	}
}

func TestFunnelStageVocabulary_ReservedNeverProduced(t *testing.T) {
	reserved := ReservedFunnelStages()
	if len(reserved) == 0 {
		t.Fatal("词表应当留有商机位（T-P7-02 要引用 opportunity 这个键名）")
	}
	for _, r := range reserved {
		if r.IsLive() {
			t.Errorf("保留阶段 %s 同时出现在产出清单里 ⇒ 它会凭空多出一个恒为 0 的阶段", r)
		}
	}
	if !StageOpportunity.IsReserved() || StageOpportunity.IsLive() {
		t.Errorf("StageOpportunity 应是「已定名、未产出」，实际 reserved=%v live=%v",
			StageOpportunity.IsReserved(), StageOpportunity.IsLive())
	}
}

// TestFunnelStageVocabulary_DemoTableStaysOutOfVocabulary 是 R-4 的守门条：
// `conversion_funnels` 演示表那五个阶段名一旦被并进词表，就等于把僵尸表扶正。
func TestFunnelStageVocabulary_DemoTableStaysOutOfVocabulary(t *testing.T) {
	for _, demo := range []FunnelStageKey{"exposure", "click", "consult", "add_wecom", "deal"} {
		if demo.IsLive() || demo.IsReserved() {
			t.Errorf("演示表阶段名 %s 进了词表 ⇒ 双源又长回来了（真实源见 ops/service，"+
				"演示表标注见 model.ConversionFunnel）", demo)
		}
	}
}

// TestFunnelStageVocabulary_GettersReturnCopies 防的是"调用方 sort 一下就改了全进程词表"。
func TestFunnelStageVocabulary_GettersReturnCopies(t *testing.T) {
	first := LiveFunnelStages()
	first[0] = "tampered"
	if got := LiveFunnelStages(); !reflect.DeepEqual(got,
		[]FunnelStageKey{StageVisit, StageClue, StageIntent, StageSession}) {
		t.Fatalf("LiveFunnelStages 返回的不是副本，调用方改元素会污染词表：%v", got)
	}

	reserved := ReservedFunnelStages()
	reserved[0] = "tampered"
	if got := ReservedFunnelStages(); len(got) != 1 || got[0] != StageOpportunity {
		t.Fatalf("ReservedFunnelStages 返回的不是副本：%v", got)
	}
}

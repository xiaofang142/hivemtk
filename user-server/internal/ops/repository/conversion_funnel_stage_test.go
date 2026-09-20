package repository

// 阶段词表的契约测试（T-P2-03 / R-4 AC③）。
//
// 这份测试的目的不是"覆盖代码"，而是把三条**只有在这里才守得住**的约束钉住：
// ① 对外契约的取值与顺序（改一个字母就是改 `GET /conversion-funnel` 的响应）；
// ② 产出清单与保留清单互斥（同时出现 = 一个阶段既产出不产出，读侧无从判断该看哪个数）；
// ③ 演示表那套阶段名不得混进词表（混进来 = R-4 要收的双源又长回来了）。
//
// T-P4-06 起 ①的序列从四段变五段、②里的商机位从保留转产出。两处都不是"顺手改期望值"：
// 前者是 AC① 要的响应变更，后者是"取数已接上"这件事在词表层的兑现点。原来的
// "词表应当留有商机位"那条断言随之作废，替换成它的反面 —— 商机位**不许再留在保留清单**，
// 因为那会让 T-P7-02 引用同一个键名时读成两个状态。

import (
	"reflect"
	"testing"
)

func TestFunnelStageVocabulary_LiveOrderIsTheResponseContract(t *testing.T) {
	got := LiveFunnelStages()

	// 值与顺序逐位比对：响应里 stages[] 的次序就是这里的次序。
	wantKeys := []FunnelStageKey{StageVisit, StageClue, StageIntent, StageSession, StageOpportunity}
	if !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("产出阶段变了：%v ≠ %v", got, wantKeys)
	}
	if string(StageVisit) != "visit" || string(StageClue) != "clue" ||
		string(StageIntent) != "intent" || string(StageSession) != "session" ||
		string(StageOpportunity) != "opportunity" {
		t.Fatalf("阶段键的**字面值**是对外契约，不得改：visit=%q clue=%q intent=%q session=%q opportunity=%q",
			StageVisit, StageClue, StageIntent, StageSession, StageOpportunity)
	}
	// 商机段必须**排在末位**：前四段的下标是现网看板的绘图位置，追加才不动它们。
	if got[len(got)-1] != StageOpportunity {
		t.Errorf("商机段应在末位，实际 %v ⇒ 前四段的下标被挪动，看板按序绘制会错位", got)
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

// TestFunnelStageVocabulary_ReservedEmptiedAndDisjoint 商机位转产出之后，保留清单应当为空，
// 但**机制留着**（下次再有"键名已定、数据源未接"的阶段仍登记在这里）。
// 两条判据各守一种坏法：清单没清空 = 同一个键既产出又保留；互斥破了 = 读侧无从判断。
func TestFunnelStageVocabulary_ReservedEmptiedAndDisjoint(t *testing.T) {
	if got := ReservedFunnelStages(); len(got) != 0 {
		t.Errorf("商机段已接上取数，保留清单应已腾空，实际仍留着 %v ⇒ 同一个键既是产出又是保留", got)
	}
	if StageOpportunity.IsReserved() {
		t.Error("StageOpportunity 仍登记为「未产出」：T-P7-02 会引用同一个键名读到两个状态")
	}
	if !StageOpportunity.IsLive() {
		t.Error("StageOpportunity 应已在产出清单里（AC①）")
	}
	for _, live := range LiveFunnelStages() {
		if live.IsReserved() {
			t.Errorf("阶段 %s 同时出现在产出与保留两份清单里", live)
		}
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
// 保留清单今日为空，副本语义仍要守着：这里改成**先塞一个元素再改**，
// 这样"清单为空所以没东西可污染"不会被读成通过（那条断言本来就跳不过去）。
func TestFunnelStageVocabulary_GettersReturnCopies(t *testing.T) {
	first := LiveFunnelStages()
	first[0] = "tampered"
	if got := LiveFunnelStages(); !reflect.DeepEqual(got,
		[]FunnelStageKey{StageVisit, StageClue, StageIntent, StageSession, StageOpportunity}) {
		t.Fatalf("LiveFunnelStages 返回的不是副本，调用方改元素会污染词表：%v", got)
	}

	// 空清单本身也可能是共享的底层数组：写满它再看下一次读到什么。
	reserved := ReservedFunnelStages()
	reserved = append(reserved, StageVisit)
	reserved[0] = "tampered"
	if got := ReservedFunnelStages(); len(got) != 0 {
		t.Fatalf("ReservedFunnelStages 返回的不是副本，调用方 append/改元素污染了词表：%v", got)
	}
}

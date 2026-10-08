package ragretrieval

import (
	"strings"
	"testing"
)

// reciprocalRankFusion 把两路召回融合成一张分数表：分数累进 map[string]float64，
// 再摊成切片按分数降序排。只比分数时，同分块次的先后取自 map 迭代序（Go 每轮 range 随机），
// 而调用方紧接着就按顺序截 Top-K 当 LLM 上下文 ⇒ 同一个查询两次能拿到不同的召回片段。
//
// 同包的 rrf_fusion.go 早就写了 ID 兜底，这里是漏网的那条路径；本文件把它钉住。
// 兜底键用 scores 的 map key（makeRRFKey 是 sha256 摘要），所以这里断言的是
// 「分数降序、同分时 key 升序」这条可复现性本身，而不是任何人名可读的次序。

func rrfChunks(ids ...string) []Chunk {
	out := make([]Chunk, 0, len(ids))
	for _, id := range ids {
		out = append(out, Chunk{ID: id, DocumentID: "doc-" + id})
	}
	return out
}

func rrfIDs(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.ID)
	}
	return out
}

// assertScoreThenKeyOrder 断言返回序列满足「分数降序；同分则 key 升序」。
func assertScoreThenKeyOrder(t *testing.T, got []Chunk) {
	t.Helper()
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if cur.Score > prev.Score {
			t.Fatalf("第 %d 项分数 %v 高于前一项 %v：必须降序", i, cur.Score, prev.Score)
		}
		if cur.Score == prev.Score {
			pk, ck := makeRRFKey(prev.DocumentID, prev.ID), makeRRFKey(cur.DocumentID, cur.ID)
			if ck < pk {
				t.Fatalf("同分块次未按 key 升序：%q(%s) 排在 %q(%s) 之后", cur.ID, ck, prev.ID, pk)
			}
		}
	}
}

func TestReciprocalRankFusion_CrossListTiesAreStable(t *testing.T) {
	h := &HybridSearcher{config: DefaultHybridSearcherConfig()}
	// vec 第 1 名与 kw 第 1 名融合分相同（同为 w/(k+1)），第 2 名同理 ⇒ 两组各 2 个同分。
	vec := rrfChunks("zz-vec1", "zz-vec2")
	kw := rrfChunks("aa-kw1", "aa-kw2")

	produce := func() string {
		return strings.Join(rrfIDs(h.reciprocalRankFusion(vec, kw, 0.5, 0.5)), ",")
	}
	first := produce()
	if len(strings.Split(first, ",")) != 4 {
		t.Fatalf("夹具应有 4 个块进入融合结果，实际=%q（用例是空跑）", first)
	}
	for round := 2; round <= 200; round++ {
		if got := produce(); got != first {
			t.Fatalf("RRF 融合结果第 %d 轮与第 1 轮不一致（同分顺序随 map 迭代序变）：\n  第 1 轮=%q\n  第 %d 轮=%q",
				round, first, round, got)
		}
	}
	assertScoreThenKeyOrder(t, h.reciprocalRankFusion(vec, kw, 0.5, 0.5))
}

func TestReciprocalRankFusion_TieGroupCoversBothLists(t *testing.T) {
	// 同分组必须真的跨两路召回：否则上面的稳定性只证明了单路内部有序，判据没牙。
	h := &HybridSearcher{config: DefaultHybridSearcherConfig()}
	vec := rrfChunks("v1")
	kw := rrfChunks("k1")
	got := h.reciprocalRankFusion(vec, kw, 0.5, 0.5)
	if len(got) != 2 {
		t.Fatalf("两路各 1 块应融合出 2 块，实际 %d", len(got))
	}
	if got[0].Score != got[1].Score {
		t.Fatalf("vec/kw 首位分数应相等（同为 0.5/61），实际 %v vs %v", got[0].Score, got[1].Score)
	}
	assertScoreThenKeyOrder(t, got)

	// 换权重后不再是同分：顺序必须由分数决定，而不是兜底键。
	got2 := h.reciprocalRankFusion(vec, kw, 0.9, 0.1)
	if got2[0].ID != "v1" {
		t.Fatalf("vec 权重更高时 v1 应排首位，实际=%q", got2[0].ID)
	}
}

package humanize

import (
	"strings"
	"testing"
)

// TF-IDF 词组表把所有 n-gram 的分数累进 tfMap/dfMap，再摊成切片按分数降序、截前 topN。
// 只比分数时，同分词组先后随 map 迭代序变 ⇒ 同一段文案两次跑，被选中的「强调词组」可以不同。
// 同分词组在实际文案里非常常见（tf 与 df 都相同的词组一大把），所以这不是理论风险。

func TestTFIDFExtract_TieBrokenByPhrase(t *testing.T) {
	// 三条消息里的词组 tf 全为 1、df 全为 1 ⇒ 分数全等，整个榜单都在并列区。
	messages := []ChampionMessage{
		{Content: "蜂巢智营"},
		{Content: "蜜罐调度"},
		{Content: "灯塔看板"},
	}
	extractor := NewTFIDFPhraseExtractor()

	produce := func() string {
		out := []string{}
		for _, p := range extractor.Extract(messages, 3) {
			out = append(out, p.Phrase)
		}
		return strings.Join(out, ",")
	}
	first := produce()
	if first == "" {
		t.Fatal("夹具没抽出任何词组，用例是空跑")
	}
	for round := 2; round <= 200; round++ {
		if got := produce(); got != first {
			t.Fatalf("TF-IDF 前 3 词组第 %d 轮与第 1 轮不一致（同分顺序随 map 迭代序变）：\n  第 1 轮=%q\n  第 %d 轮=%q",
				round, first, round, got)
		}
	}
}

func TestTFIDFExtract_ParallelScoresArePhraseAscending(t *testing.T) {
	messages := []ChampionMessage{
		{Content: "这款产品的成分是烟酰胺，保湿效果好，补水效果好。"},
		{Content: "现在下单立享优惠，包邮活动，赠品丰富。"},
		{Content: "理解您的心情，抱歉给您带来困扰，马上处理。"},
	}
	phrases := NewTFIDFPhraseExtractor().Extract(messages, 12)
	if len(phrases) < 2 {
		t.Fatalf("夹具应抽出多条短语，实际 %d 条", len(phrases))
	}
	// 分数相等的相邻两项必须按词组升序；同时 Rank 连续。
	for i := 1; i < len(phrases); i++ {
		prev, cur := phrases[i-1], phrases[i]
		if cur.TFIDFScore > prev.TFIDFScore {
			t.Fatalf("phrases[%d].Score=%v 大于前一项 %v：分数必须降序", i, cur.TFIDFScore, prev.TFIDFScore)
		}
		if cur.TFIDFScore == prev.TFIDFScore && cur.Phrase < prev.Phrase {
			t.Fatalf("同分词组未按名字升序：phrases[%d]=%q 排在 phrases[%d]=%q 之后", i, cur.Phrase, i-1, prev.Phrase)
		}
		if cur.Rank != i+1 {
			t.Fatalf("phrases[%d].Rank=%d，应为 %d", i, cur.Rank, i+1)
		}
	}
}

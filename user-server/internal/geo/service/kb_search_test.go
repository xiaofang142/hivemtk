package service

import (
	"testing"

	"hivemtk-user/internal/geo/model"
)

func kbDoc(title, content string) *model.GeoKnowledgeDocument {
	return &model.GeoKnowledgeDocument{Title: title, Content: content}
}

func TestKBQueryTokens丢弃单字符与去重(t *testing.T) {
	toks := kbQueryTokens("hivemtk 是什么？ 是 HiveMTK")
	want := []string{"hivemtk", "是什么"}
	if len(toks) != len(want) {
		t.Fatalf("tokens = %v, want %v", toks, want)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Fatalf("tokens[%d] = %q, want %q (all=%v)", i, toks[i], want[i], toks)
		}
	}
}

// 自然语言问句整串匹配必然落空，search 必须退化为词元匹配，
// 否则 Ask 永远回答「暂无与问题相关的内容」。
func TestKBSearch整句落空时按词元兜底(t *testing.T) {
	svc := &KBService{}
	docs := []*model.GeoKnowledgeDocument{
		kbDoc("HiveMTK 核心功能总览", "智能客服与销售智能体"),
		kbDoc("退款政策", "七天内可退"),
	}
	if got := svc.matchDocs(docs, "hivemtk 是什么？"); len(got) != 0 {
		t.Fatalf("整串不应命中，实际 %d 条", len(got))
	}
	tokens := kbQueryTokens("hivemtk 是什么？")
	got := svc.matchDocsAnyToken(docs, tokens)
	if len(got) != 1 {
		t.Fatalf("词元兜底应命中 1 条，实际 %d 条", len(got))
	}
	if got[0].Title != "HiveMTK 核心功能总览" {
		t.Fatalf("命中标题 = %q", got[0].Title)
	}
	if got[0].Score <= 0 || got[0].Score > 0.95 {
		t.Fatalf("score = %v, want (0, 0.95]", got[0].Score)
	}
}

func TestKBMatchDocsAnyToken标题权重高于正文(t *testing.T) {
	svc := &KBService{}
	docs := []*model.GeoKnowledgeDocument{
		kbDoc("普通文档", "正文提到 hivemtk"),
		kbDoc("hivemtk 介绍", "无关正文"),
	}
	got := svc.matchDocsAnyToken(docs, []string{"hivemtk"})
	if len(got) != 2 {
		t.Fatalf("应命中 2 条，实际 %d", len(got))
	}
	var titleScore, contentScore float64
	for _, r := range got {
		switch r.Title {
		case "hivemtk 介绍":
			titleScore = r.Score
		case "普通文档":
			contentScore = r.Score
		}
	}
	if titleScore <= contentScore {
		t.Fatalf("标题命中分应更高: title=%v content=%v", titleScore, contentScore)
	}
}

package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
)

// TestShouldTransferToHuman_ExplicitRequestBeatsConfidentIntent 客户白纸黑字要人工时，
// 转人工不能取决于分类器当天心情：降级兜底文案里那句"回复「转人工」"是给客户的一条出路，
// 而旧序里 intent.Confidence>=0.7 的前置门会先 return false、连 veto 都不看
// （分类器完全可能把「转人工」标成 product/chat 且很自信）⇒ 客户照做只是再等一轮 AI。
func TestShouldTransferToHuman_ExplicitRequestBeatsConfidentIntent(t *testing.T) {
	e := &SalesEngine{}
	confident := &dto.RecognizeResult{IntentType: "product", Confidence: 0.9}

	cases := []struct {
		name    string
		msg     string
		want    bool
		wantRsn string
	}{
		{"中文裸暗号", "转人工", true, "客户显式要求转人工"},
		{"夹在句子里", "这个问题比较复杂，转人工处理", true, "客户显式要求转人工"},
		{"关键词表里的另一种说法", "给我安排人工客服", true, "客户显式要求转人工"},
		{"英文暗号", "I want a Human Agent", true, "客户显式要求转人工"},
		{"否定窗口内不转", "不用转人工，我自己弄", false, ""},
		{"无暗号时前置门照旧放行 AI", "这个多少钱", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := e.shouldTransferToHuman(context.Background(), confident, nil,
				&SalesRequest{UserMessage: tc.msg})
			if got != tc.want {
				t.Fatalf("消息 %q 在分类器自信(%v)的前提下 transfer=%v，期望 %v（reason=%q）",
					tc.msg, confident.Confidence, got, tc.want, reason)
			}
			if tc.want && reason != tc.wantRsn {
				t.Fatalf("命中显式人工诉求应给出可辨识的理由，得到 %q", reason)
			}
		})
	}
}

// TestShouldTransferToHuman_NilRequestAndNilIntent 新增的前置判据不得改变两个空值出口：
// req 为 nil 时无暗号可读，intent 为 nil 时仍是"不转"。
func TestShouldTransferToHuman_NilRequestAndNilIntent(t *testing.T) {
	e := &SalesEngine{}
	if got, reason := e.shouldTransferToHuman(context.Background(), nil, nil, nil); got {
		t.Fatalf("req/intent 双 nil 不应触发转人工: %v %q", got, reason)
	}
	if got, reason := e.shouldTransferToHuman(context.Background(), nil, nil, &SalesRequest{UserMessage: "转人工"}); !got {
		t.Fatalf("intent 为 nil 时客户的人工诉求仍必须被接住: %v %q", got, reason)
	}
}

package service

import (
	"testing"
)

// LM-05 正例：messages + historic_messages 转规范消息，重叠按源消息 ID 去重，
// 历史/实时分别标记，未知方向保持 unknown，不凭昵称补成 customer/self。
func TestNormalizeSageXNotificationMessages_DedupeAndProvenance(t *testing.T) {
	in := SageXNotificationUpload{
		EventID: "ev1",
		Messages: []SageXStyleMessage{
			{Text: "甲：我有货", Timestamp: 1000, SenderName: "甲", SenderKey: "member-a"},
			{Text: "乙：求购", Timestamp: 2000, SenderName: "乙"},
		},
		HistoricMessages: []SageXStyleMessage{
			{Text: "甲：我有货", Timestamp: 1000, SenderName: "甲", SenderKey: "member-a"},
			{Text: "丙：撮合", Timestamp: 900, SenderName: "丙"},
		},
		ParseStatus:     "partial",
		TruncatedFields: []string{"big_text"},
	}
	out := NormalizeSageXNotificationMessages(in)
	if len(out.Messages) != 3 {
		t.Fatalf("期望 3 条去重后消息，实际 %d", len(out.Messages))
	}
	byContent := map[string]NormalizedSageXMessage{}
	for _, m := range out.Messages {
		byContent[m.Content] = m
	}
	if byContent["甲：我有货"].SourceCoverage != "historic" || byContent["乙：求购"].SourceCoverage != "live" {
		t.Fatalf("历史/live 标记错误: %+v", out.Messages)
	}
	if byContent["甲：我有货"].Direction != "unknown" {
		t.Fatalf("未知方向不得猜成 customer/self，实际 %q", byContent["甲：我有货"].Direction)
	}
	if byContent["甲：我有货"].MemberID != "member-a" {
		t.Fatalf("成员稳定标识应保留 sender_key，实际 %q", byContent["甲：我有货"].MemberID)
	}
	if out.ParseStatus != "partial" || len(out.TruncatedFields) != 1 {
		t.Fatalf("解析/截断标记丢失: %+v", out)
	}
}

// LM-05 反例：空 sender、空文本不得伪造成员/内容；解析失败与无载荷需显式标记。
func TestNormalizeSageXNotificationMessages_EmptyAndFailed(t *testing.T) {
	out := NormalizeSageXNotificationMessages(SageXNotificationUpload{
		ParseStatus: "failed",
		Messages:    []SageXStyleMessage{{Text: "  "}, {Text: "真实内容"}},
	})
	if len(out.Messages) != 1 {
		t.Fatalf("空文本应丢弃，实际 %d 条", len(out.Messages))
	}
	if out.Messages[0].MemberID != "" || out.Messages[0].Direction != "unknown" {
		t.Fatalf("空 sender 不得伪造成员或方向: %+v", out.Messages[0])
	}
	if !out.CollectionIncomplete {
		t.Fatal("解析失败必须标记采集不完整")
	}
	if len(NormalizeSageXNotificationMessages(SageXNotificationUpload{}).Messages) != 0 {
		t.Fatal("无数组载荷时不得构造伪消息")
	}
}

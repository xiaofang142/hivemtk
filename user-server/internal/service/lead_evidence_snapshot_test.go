package service

import (
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// LM-12 正例：快照携带真实 message_id/成员/方向/时间，引用必须属于授权快照。
func TestLeadEvidenceSnapshot_ReferenceValidation(t *testing.T) {
	snap := NewLeadEvidenceSnapshot([]model.MessageHub{
		{ID: 1, MsgID: "real-1", SenderID: "甲", SenderName: "甲", Direction: "inbound", Content: "我有货", AccountID: "acc", Platform: "telegram", SentAt: timeZero()},
		{ID: 2, MsgID: "real-2", SenderID: "乙", Direction: "outbound", Content: " reply", AccountID: "acc", Platform: "telegram", SentAt: timeZero()},
	})
	if len(snap.Messages) != 2 {
		t.Fatalf("快照应保留全部真实消息，实际 %d", len(snap.Messages))
	}
	if !snap.ReferencesValid([]string{"real-1", "real-2"}) {
		t.Fatal("快照内引用应通过")
	}
	if snap.ReferencesValid([]string{"real-1", "fabricated"}) || snap.ReferencesValid([]string{"history_99"}) {
		t.Fatal("伪造/合成 ID 必须被拒绝")
	}
}

// LM-12 反例：按源消息去重、不放大证据；空引用不冒充精确证据。
func TestLeadEvidenceSnapshot_DedupAndEmpty(t *testing.T) {
	snap := NewLeadEvidenceSnapshot([]model.MessageHub{
		{MsgID: "m1", Content: "甲", SenderID: "甲", Direction: "inbound"},
		{MsgID: "m1", Content: "甲", SenderID: "甲", Direction: "inbound"},
	})
	if len(snap.Messages) != 1 {
		t.Fatalf("重复消息去重后应为 1 条，实际 %d", len(snap.Messages))
	}
	if snap.ReferencesValid(nil) || snap.ReferencesValid([]string{""}) {
		t.Fatal("空引用不得充当精确证据")
	}
}

func timeZero() time.Time { return time.Unix(100, 0) }

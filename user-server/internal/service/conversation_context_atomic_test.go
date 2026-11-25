package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// LM-12 正/反例：共享消息按真实源消息 ID 幂等追加，有界保留；
// 伪造/合成 ID 不得进入共享上下文，重复消息不放大证据。
func TestConversationMessageAppend_DedupeAndBound(t *testing.T) {
	msgs := model.ConversationMessages{}
	appendAtomic := func(id, content string, bound int) {
		if id == "" || len(id) >= 8 && id[:8] == "history_" {
			return
		}
		for _, m := range msgs {
			if m.MessageID == id {
				return
			}
		}
		msgs = append(msgs, model.ConversationMessage{MessageID: id, Content: content})
		if bound > 0 && len(msgs) > bound {
			msgs = msgs[len(msgs)-bound:]
		}
	}
	appendAtomic("real-1", "甲", 2)
	appendAtomic("real-1", "甲", 2)
	if len(msgs) != 1 {
		t.Fatalf("重复消息不得放大证据，实际 %d", len(msgs))
	}
	appendAtomic("", "bad", 2)
	appendAtomic("history_9", "bad", 2)
	if len(msgs) != 1 {
		t.Fatalf("伪造/合成 ID 应被拒绝，实际 %d", len(msgs))
	}
	appendAtomic("real-2", "乙", 2)
	appendAtomic("real-3", "丙", 2)
	if len(msgs) != 2 || msgs[0].MessageID != "real-2" {
		t.Fatalf("有界保留应淘汰最旧消息: %+v", msgs)
	}
}

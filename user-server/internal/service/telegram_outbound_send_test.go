package service

import "testing"

// TestTelegramOutboundHubMsgID 锁住出站 msg_id 的账号维度唯一性。
//
// 反例（2026-09-19 实测）：msg_id 只写 tg-<message_id> 时，账号 9 在会话 8608488936
// 已有的 tg-26/tg-27 会让账号 5 的同编号出站行撞唯一键 (platform,msg_id,conversation_id)：
// 消息真的发出去了，落库却失败，于是 recheck 判定「未回复」并再次生成、再次投递。
func TestTelegramOutboundHubMsgID(t *testing.T) {
	got := telegramOutboundHubMsgID(5, 26)
	if got != "tg-out-5-26" {
		t.Fatalf("msg_id 应为 tg-out-<account>-<message_id>，实际 %q", got)
	}
	if telegramOutboundHubMsgID(9, 26) == got {
		t.Fatal("不同账号的同一 message_id 必须不撞唯一键")
	}
	if id := telegramOutboundHubMsgID(5, 0); id == got || id == "" {
		t.Fatalf("message_id<=0 时应回退到唯一占位 ID，实际 %q", id)
	}
}

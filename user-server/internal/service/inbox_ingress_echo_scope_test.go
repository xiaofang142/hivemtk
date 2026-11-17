package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 历史消息哈希只在完整渠道/账号/会话范围内证明回声；不能跨范围吞客户消息。
// 夹具时间超出内容回声与错归窗口，隔离验证身份闸，不借助其他判据假绿。
func TestInboxIngress_BaseHashEcho_RequiresMatchingScope(t *testing.T) {
	tests := []struct {
		name          string
		channel       string
		account       string
		conversation  string
		eventID       string
		wantSelfEcho  bool
		shadowInbound bool
	}{
		{
			name: "同作用域裸哈希仍拦回声", channel: "douyin", account: "scope-account",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d", wantSelfEcho: true,
		},
		{
			name: "同作用域发生次数后缀仍拦回声", channel: "douyin", account: "scope-account",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d#1", wantSelfEcho: true,
		},
		{
			name: "跨渠道相同哈希不能判回声", channel: "xiaohongshu", account: "scope-account",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d",
		},
		{
			name: "跨账号相同哈希不能判回声", channel: "douyin", account: "other-account",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d",
		},
		{
			name: "跨会话相同哈希不能判回声", channel: "douyin", account: "scope-account",
			conversation: "other-conversation", eventID: "mh:1a2b3c4d#1",
		},
		{
			name: "缺账号不能退回全局哈希", channel: "douyin",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d",
		},
		{
			name: "缺会话不能退回全局哈希", channel: "douyin", account: "scope-account",
			eventID: "mh:1a2b3c4d",
		},
		{
			name: "异域首条入站不能遮蔽本域出站", channel: "douyin", account: "scope-account",
			conversation: "scope-conversation", eventID: "mh:1a2b3c4d#1",
			wantSelfEcho: true, shadowInbound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
			svc := NewInboxIngressService()
			// 只测试 DB 身份范围，不让跨子用例共享内容缓存抢占干扰判据。
			svc.cache = nil
			old := time.Now().Add(-30 * 24 * time.Hour)
			if tt.shadowInbound {
				if err := db.Create(&model.MessageHub{
					MsgID: "mh:1a2b3c4d", Platform: "douyin", AccountID: "other-account",
					ConversationID: "other-conversation", Direction: "inbound", Status: "delivered",
					MsgType: "text", Content: "其他会话的历史客户消息", SentAt: old, CreatedAt: old,
				}).Error; err != nil {
					t.Fatalf("预置异域入站失败：%v", err)
				}
			}
			if err := db.Create(&model.MessageHub{
				MsgID: "mh:1a2b3c4d", Platform: "douyin", AccountID: "scope-account",
				ConversationID: "scope-conversation", Direction: "outbound", Status: "delivered",
				MsgType: "text", SenderID: "scope-account", Content: "历史发送气泡",
				SentAt: old, CreatedAt: old,
			}).Error; err != nil {
				t.Fatalf("预置出站失败：%v", err)
			}
			event := &model.MessageEvent{
				Channel: tt.channel, ConversationID: tt.conversation,
				EventID: tt.eventID, SenderID: "customer-physical-id", SenderType: "customer",
				SenderName: "作用域回归客户", Content: "客户独立发言",
				Extra: map[string]interface{}{"account_id": tt.account},
			}
			decision, err := svc.interceptInbound(context.Background(), event)
			if err != nil {
				t.Fatalf("入口判定失败：%v", err)
			}
			if tt.wantSelfEcho {
				if decision == nil || !decision.Blocked || !decision.IsSelfEcho ||
					!strings.Contains(decision.Reason, "base hash") {
					t.Fatalf("同作用域哈希应判自回声，实际：%+v", decision)
				}
				return
			}
			if decision != nil && decision.Blocked {
				t.Fatalf("缺失或不同作用域不能仅凭历史哈希拦截，实际：%+v", decision)
			}
		})
	}
}

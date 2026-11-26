package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// T10 后端定向测试：自他判据、群聊只读归并、筛选、合并线程与数据权限。
// 全部走真实 PG 测试库（testutil.NewTestDBOrSkip），不写生产数据。

func TestInboxT10HubSelfJudgment(t *testing.T) {
	cases := []struct {
		name       string
		hub        *model.MessageHub
		wantSelf   bool
		wantSender string
	}{
		{
			name:       "outbound is platform even without sender_type",
			hub:        &model.MessageHub{Direction: "outbound", MsgType: "text", ConversationID: "c1"},
			wantSelf:   true,
			wantSender: "agent",
		},
		{
			name:       "outbound AI reply is ai sender",
			hub:        &model.MessageHub{Direction: "outbound", IsAIReply: true, MsgType: "text", ConversationID: "c1"},
			wantSelf:   true,
			wantSender: "ai",
		},
		{
			name:       "inbound customer stays left",
			hub:        &model.MessageHub{Direction: "inbound", SenderID: "u1", SenderName: "张三", MsgType: "text", ConversationID: "c1"},
			wantSelf:   false,
			wantSender: "customer",
		},
		{
			name: "sender_type from Extra wins",
			hub: &model.MessageHub{
				Direction: "inbound", SenderID: "staff-1", MsgType: "text", ConversationID: "c1",
				Extra: model.JSONMap{"sender_type": "staff", "group_name": "测试群"},
			},
			wantSelf:   true,
			wantSender: "staff",
		},
		{
			name: "extra sender_type cannot fake self when inbound contact",
			hub: &model.MessageHub{
				Direction: "inbound", SenderID: "u2", MsgType: "text", ConversationID: "c1",
				Extra: model.JSONMap{"sender_type": "contact"},
			},
			wantSelf:   false,
			wantSender: "contact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := inboxHubToMessage(tc.hub)
			if got, ok := data["is_self"].(bool); !ok || got != tc.wantSelf {
				t.Fatalf("is_self = %v, want %v", data["is_self"], tc.wantSelf)
			}
			if got := data["sender_type"]; got != tc.wantSender {
				t.Fatalf("sender_type = %v, want %v", got, tc.wantSender)
			}
			if got := data["source"]; got != "hub" {
				t.Fatalf("source = %v, want hub", got)
			}
		})
	}
}

func TestInboxT10HubProjectionKeepsAvatarAndGroup(t *testing.T) {
	hub := &model.MessageHub{
		Direction: "inbound", SenderID: "u1", SenderName: "张三",
		SenderAvatar: "https://example.com/a.png", ConversationID: "c1",
		MsgType: "text", GroupID: "gid-1", IsGroup: true, Content: "你好",
		Extra: model.JSONMap{"group_name": "相侵相碍一家人", "bridge": "wecom", "transport": "bot"},
	}
	data := inboxHubToMessage(hub)
	for key, want := range map[string]any{
		"sender_avatar": "https://example.com/a.png",
		"sender_type":   "customer",
		"group_name":    "相侵相碍一家人",
		"group_id":      "gid-1",
		"is_group":      true,
		"is_self":       false,
		"content":       "你好",
		"bridge":        "wecom",
		"transport":     "bot",
	} {
		if data[key] != want {
			t.Errorf("%s = %v, want %v", key, data[key], want)
		}
	}
	if _, ok := data["platform_msg_id"]; !ok {
		t.Error("platform_msg_id missing from projection")
	}
}

func t10Conv(platform, account, customer, convID, name string) *model.InboxConversation {
	now := time.Now()
	return &model.InboxConversation{
		TenantID: 1, Platform: platform, AccountID: account,
		CustomerID: customer, CustomerName: name, ConversationID: convID,
		Status: "open", LastMessagePreview: "hello", LastMessageAt: &now,
	}
}

func TestInboxT10ConversationMetaAndFilters(t *testing.T) {
	svc, db := newInboxService(t)
	ctx := context.Background()

	// 旧 APK 群聊：同 (platform,account,conversation_id) 下两个不同 customer_id。
	g1 := t10Conv("wecom", "acc-t10", "u-A", "相侵相碍一家人", "我爱腾讯")
	g2 := t10Conv("wecom", "acc-t10", "u-B", "相侵相碍一家人", "祎眠冬")
	// 单聊：conversation_id 唯一。
	s1 := t10Conv("wecom", "acc-t10", "u-C", "single-C", "张三")
	for _, c := range []*model.InboxConversation{g1, g2, s1} {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create conv: %v", err)
		}
	}

	list, total, err := svc.List(ctx, InboxQuery{Page: 1, PageSize: 20})
	if err != nil || total != 3 {
		t.Fatalf("List total=%d err=%v", total, err)
	}
	byID := map[uint]*model.InboxConversation{}
	for _, c := range list {
		byID[c.ID] = c
	}
	gc := byID[g1.ID]
	if gc == nil || !gc.IsGroup || gc.ConversationType != "group" {
		t.Fatalf("shared conversation_id not detected as group: %+v", gc)
	}
	if gc.GroupName != "相侵相碍一家人" || gc.GroupKey != "wecom|acc-t10|相侵相碍一家人" {
		t.Fatalf("group name/key wrong: %+v", gc)
	}
	if gc.MemberCount != 2 || len(gc.ConversationIDs) != 1 {
		t.Fatalf("member count/ids wrong: %+v", gc)
	}
	sc := byID[s1.ID]
	if sc == nil || sc.IsGroup || sc.ConversationType != "single" {
		t.Fatalf("unique conversation_id should stay single: %+v", sc)
	}

	groups, groupTotal, err := svc.List(ctx, InboxQuery{ConversationType: "group", Page: 1, PageSize: 20})
	if err != nil || groupTotal != 2 {
		t.Fatalf("group filter total=%d err=%v", groupTotal, err)
	}
	for _, c := range groups {
		if !c.IsGroup {
			t.Fatalf("group filter leaked single conv %d", c.ID)
		}
	}
	singles, singleTotal, err := svc.List(ctx, InboxQuery{ConversationType: "single", Page: 1, PageSize: 20})
	if err != nil || singleTotal != 1 || singles[0].ID != s1.ID {
		t.Fatalf("single filter total=%d err=%v", singleTotal, err)
	}
	nick, nickTotal, err := svc.List(ctx, InboxQuery{Nickname: "张三", Page: 1, PageSize: 20})
	if err != nil || nickTotal != 1 || nick[0].CustomerID != "u-C" {
		t.Fatalf("nickname filter total=%d err=%v", nickTotal, err)
	}
}

func TestInboxT10GroupFlagFromMessage(t *testing.T) {
	svc, db := newInboxService(t)
	ctx := context.Background()

	c := t10Conv("wecom", "acc-t10", "u-D", "grp-flag", "单成员群")
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("create conv: %v", err)
	}
	msg := mkMsg("t10-flag")
	msg.TenantID = 1
	msg.AccountID = "acc-t10"
	msg.ConversationID = "grp-flag"
	msg.IsGroup = true
	msg.GroupID = "gid-flag"
	msg.Extra = model.JSONMap{"group_name": "官方群"}
	if err := db.Create(msg).Error; err != nil {
		t.Fatalf("create msg: %v", err)
	}

	list, _, err := svc.List(ctx, InboxQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, got := range list {
		if got.ID == c.ID {
			if !got.IsGroup || got.GroupName != "官方群" || got.GroupKey != "wecom|acc-t10|官方群" {
				t.Fatalf("message group flag not projected: %+v", got)
			}
			return
		}
	}
	t.Fatal("created conversation missing from list")
}

func TestInboxT10GetGroupThreadMergesAndGuards(t *testing.T) {
	svc, db := newInboxService(t)
	ctx := context.Background()

	g1 := t10Conv("wecom", "acc-t10", "u-1", "群A", "甲")
	g1.AssignedTo = "agent-1"
	g2 := t10Conv("wecom", "acc-t10", "u-2", "群A", "乙")
	for _, c := range []*model.InboxConversation{g1, g2} {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create conv: %v", err)
		}
	}
	now := time.Now()
	mk := func(id, senderID, senderName, direction, avatar string, at time.Time) *model.MessageHub {
		return &model.MessageHub{
			TenantID: 1, Platform: "wecom", AccountID: "acc-t10",
			MsgID: id, Direction: direction, MsgType: "text",
			SenderID: senderID, SenderName: senderName, SenderAvatar: avatar,
			ReceiverID: "acc-t10", Content: id, ConversationID: "群A", SentAt: at,
		}
	}
	for _, m := range []*model.MessageHub{
		mk("m-1", "u-1", "甲", "inbound", "https://example.com/1.png", now.Add(-3*time.Minute)),
		mk("m-2", "u-2", "乙", "inbound", "", now.Add(-2*time.Minute)),
		mk("m-3", "acc-t10", "客服", "outbound", "", now.Add(-1*time.Minute)),
	} {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create msg: %v", err)
		}
	}

	q := InboxGroupThreadQuery{Platform: "wecom", AccountID: "acc-t10", GroupName: "群A", Page: 1, PageSize: 50}
	list, total, members, err := svc.GetGroupThread(ctx, q, InboxScope{})
	if err != nil || total != 3 || len(list) != 3 {
		t.Fatalf("merged thread total=%d len=%d err=%v", total, len(list), err)
	}
	selfCount := 0
	avatarCount := 0
	for _, m := range list {
		if m["is_self"] == true {
			selfCount++
		}
		if m["sender_avatar"] == "https://example.com/1.png" {
			avatarCount++
		}
	}
	if selfCount != 1 || avatarCount != 1 {
		t.Fatalf("self=%d avatar=%d, want 1/1", selfCount, avatarCount)
	}
	if len(members) != 2 {
		t.Fatalf("members=%d, want 2 user members (account excluded): %+v", len(members), members)
	}

	// 非超管未分配 → 拒绝；分配到群内任一会话 → 放行。
	if _, _, _, err := svc.GetGroupThread(ctx, q, InboxScope{Role: "customer_service", Username: "nobody"}); !errors.Is(err, ErrInboxForbidden) {
		t.Fatalf("unassigned scope err=%v, want ErrInboxForbidden", err)
	}
	if _, _, _, err := svc.GetGroupThread(ctx, q, InboxScope{Role: "customer_service", Username: "agent-1"}); err != nil {
		t.Fatalf("assigned scope err=%v", err)
	}
	// 空群名 fail-closed。
	empty, emptyTotal, emptyMembers, err := svc.GetGroupThread(ctx, InboxGroupThreadQuery{Platform: "wecom", AccountID: "acc-t10", GroupName: "  "}, InboxScope{})
	if err != nil || emptyTotal != 0 || len(empty) != 0 || len(emptyMembers) != 0 {
		t.Fatalf("empty group name should return empty, got %d/%d err=%v", emptyTotal, len(empty), err)
	}
}

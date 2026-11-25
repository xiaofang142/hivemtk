package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// 历史必须锁住租户、平台、来源账号，同时保留私聊双向和群内所有已采集成员。
func TestMessageHubHistory_AccountScope(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := NewMessageHubRepositoryWithDB(database)
	ctx := tenant.OnlyTenant(context.Background(), 51)
	now := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	for i, input := range []struct {
		tenant                               uint
		platform, account, direction, sender string
		group                                bool
	}{
		{51, "telegram", "11", "inbound", "peer", false},
		{51, "telegram", "11", "outbound", "self", false},
		{51, "telegram", "22", "inbound", "peer", false},
		{52, "telegram", "11", "inbound", "peer", false},
		{51, "qq", "11", "inbound", "peer", false},
		{51, "telegram", "11", "inbound", "member-a", true},
		{51, "telegram", "11", "inbound", "member-b", true},
		{51, "telegram", "11", "outbound", "self", true},
		{51, "telegram", "22", "inbound", "member-a", true},
		{52, "telegram", "11", "inbound", "member-a", true},
		{51, "qq", "11", "inbound", "member-a", true},
	} {
		row := model.MessageHub{TenantID: input.tenant, Platform: input.platform, AccountID: input.account,
			MsgID: fmt.Sprintf("scope-%d", i), ConversationID: "same", IsGroup: input.group,
			Direction: input.direction, SenderID: input.sender, ReceiverID: "peer", Content: fmt.Sprintf("content-%d", i), SentAt: now}
		if input.group {
			row.GroupID = "same-group"
		}
		if err := database.WithContext(tenant.OnlyTenant(context.Background(), input.tenant)).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Run("private", func(t *testing.T) {
		rows, err := repo.ListRecentByConversation(ctx, MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"11"}}, "same", 20)
		if err != nil || len(rows) != 2 {
			t.Fatalf("私聊应仅含 A 的双向两条，实际 %d err=%v", len(rows), err)
		}
		if rows[0].MsgID != "scope-1" || rows[1].MsgID != "scope-0" {
			t.Fatalf("同时间应按 ID 稳定倒序，实际 %+v", rows)
		}
	})
	t.Run("group", func(t *testing.T) {
		rows, err := repo.ListRecentByGroup(ctx, MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"11"}, IsGroup: true}, "same-group", 20)
		if err != nil || len(rows) != 3 {
			t.Fatalf("群历史应仅含 A 的全部三名成员，实际 %d err=%v", len(rows), err)
		}
	})
}

// 发送者兜底只能读同账号无明确会话的双向私聊，不能夹带其他会话或群。
func TestMessageHubHistory_SenderFallback(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := NewMessageHubRepositoryWithDB(database)
	ctx := tenant.OnlyTenant(context.Background(), 51)
	for i, input := range []struct {
		tid                                            uint
		account, conversation, group, sender, receiver string
	}{
		{51, "11", "", "", "peer", "self"},
		{51, "11", "", "", "self", "peer"},
		{51, "22", "", "", "peer", "self"},
		{51, "11", "other-conv", "", "peer", "self"},
		{51, "11", "", "group", "peer", "self"},
		{52, "11", "", "", "peer", "self"},
	} {
		row := model.MessageHub{TenantID: input.tid, Platform: "telegram", AccountID: input.account,
			MsgID: fmt.Sprintf("peer-%d", i), ConversationID: input.conversation, GroupID: input.group,
			IsGroup: input.group != "", SenderID: input.sender, ReceiverID: input.receiver, Direction: "inbound", SentAt: time.Now()}
		if i == 1 {
			row.Direction = "outbound"
		}
		if err := database.WithContext(tenant.OnlyTenant(context.Background(), input.tid)).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	scope := MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"11"}}
	rows, err := repo.ListRecentBySender(ctx, scope, "peer", 20)
	if err != nil || len(rows) != 2 || rows[0].Direction != "outbound" {
		t.Fatalf("兜底应只读同账号无会话的双向消息，实际 %+v err=%v", rows, err)
	}
	rows, err = repo.ListRecentBySender(ctx, scope, "peer", 1)
	if err != nil || len(rows) != 1 || rows[0].MsgID != "peer-1" {
		t.Fatalf("最近一条截取失败：%+v err=%v", rows, err)
	}
}

// 缺账号/平台/租户和系统作用域均须显式拒绝，不能省略查询谓词。
func TestMessageHubHistory_RejectIncompleteScope(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := NewMessageHubRepositoryWithDB(database)
	ctx := tenant.OnlyTenant(context.Background(), 51)
	valid := MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"11"}}
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		scope MessageHubHistoryScope
	}{
		{"no-tenant", context.Background(), valid},
		{"system", tenant.WithSystemScope(ctx), valid},
		{"no-platform", ctx, MessageHubHistoryScope{AccountIDs: []string{"11"}}},
		{"no-account", ctx, MessageHubHistoryScope{Platform: "telegram"}},
		{"blank-account", ctx, MessageHubHistoryScope{Platform: "telegram", AccountIDs: []string{"11", " "}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rows, err := repo.ListRecentByConversation(tc.ctx, tc.scope, "same", 20); !errors.Is(err, ErrMessageHubHistoryScope) || len(rows) != 0 {
				t.Fatalf("不完整作用域应拒绝，实际 %+v err=%v", rows, err)
			}
		})
	}
}

package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 真实 PG：自动回复、线索发现和旧管理端 Snapshot 必须读写同一份 scoped 上下文。
func TestConversationContextScope_SharedReaders(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{},
		&model.MessageHub{}, &model.ConversationContext{})
	if err := database.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id)").Error; err != nil {
		t.Fatal(err)
	}
	ctx := tenant.OnlyTenant(context.Background(), 51)
	channel := model.Channel{TenantID: 51, Code: "telegram", Type: "telegram", Name: "TG"}
	if err := database.WithContext(ctx).Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	agent := uint(1)
	account := model.ChannelAccount{TenantID: 51, ChannelID: channel.ID, Name: "shared-account",
		Status: 1, DataScope: "{}", LeadMiningAgentID: &agent, AutoReplyAgentID: &agent}
	if err := database.WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	hub := &model.MessageHub{TenantID: 51, Platform: "telegram", AccountID: account.Name,
		ConversationID: "same", GroupID: "same", IsGroup: true, SenderID: "peer", Direction: "inbound", Content: "采购", MsgID: "shared-msg"}
	if err := database.WithContext(ctx).Create(hub).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewConversationContextRepositoryWithDB(database)
	key := leadHistoryConversationID(hub)
	if err := repo.Upsert(ctx, &model.ConversationContext{ChannelAccountID: account.ID, ExternalConversationID: key,
		Summary: "隔离后的摘要", Tags: model.JSONStrings{"需求"}, MemberRoles: model.MemberRoles{"peer": "demand"},
		Messages: model.ConversationMessages{{MessageID: hub.MsgID, SenderID: "peer", Content: "采购"}}}); err != nil {
		t.Fatal(err)
	}
	svc := NewConversationContextServiceWithRepo(repo)
	auto := NewAutoReplyServiceWithCtx(svc)
	lead := NewLeadDiscoveryServiceWithCtx(nil, svc)
	if err := auto.RecordNote(ctx, account.ID, "same", "自动回复备注"); err != nil {
		t.Fatal(err)
	}
	if err := lead.RecordNote(ctx, account.ID, "same", "线索备注"); err != nil {
		t.Fatal(err)
	}
	snap, err := svc.Snapshot(ctx, account.ID, "same")
	if err != nil || snap.Summary != "隔离后的摘要" || len(snap.Messages) != 1 || snap.MemberRoles["peer"] != "demand" || len(snap.AutoReplyNotes) != 1 || len(snap.LeadMiningNotes) != 1 {
		t.Fatalf("双模块和管理端必须共享摘要、角色、消息和备注：%+v err=%v", snap, err)
	}
	if notes := auto.ReferLeadNotes(ctx, account.ID, "same"); len(notes) != 1 || !strings.Contains(notes[0], "线索备注") {
		t.Fatalf("自动回复应看到同群线索备注：%v", notes)
	}
	if notes := lead.ReferAutoReplyNotes(ctx, account.ID, "same"); len(notes) != 1 || !strings.Contains(notes[0], "自动回复备注") {
		t.Fatalf("线索发现应看到同群自动回复备注：%v", notes)
	}
	// Process 的原始 conversation_id 可与 group_id 不同，角色也必须写到群边界。
	hub.ConversationID, hub.GroupID, hub.SenderID = "transport-conv", "another-group", "member-b"
	lead.Process(ctx, hub)
	group, err := repo.Get(ctx, account.ID, leadHistoryConversationID(hub))
	if err != nil || group.MemberRoles["member-b"] != "member" || len(group.Tags) != 1 {
		t.Fatalf("Process 必须使用平台账号单群键：%+v err=%v", group, err)
	}
}

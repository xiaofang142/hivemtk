package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestChannelIdentity_ConversationTypeBelongsToMessage(t *testing.T) {
	for _, legacyType := range []string{"dm", "group"} {
		t.Run(legacyType, func(t *testing.T) {
			repo := &memAccountRepo{rows: []model.ChannelAccount{{ID: 1, TenantID: 18, Type: legacyType}}}
			resolver := NewChannelResolverWithRepos(repo, nil)
			for _, messageType := range []string{"dm", "group"} {
				identity, err := resolver.Identify(context.Background(), 18, 1, messageType, "member-1", "member")
				if err != nil || identity.ConversationType != messageType || identity.Self {
					t.Fatalf("同一账号应按每条消息识别 %s：identity=%+v err=%v", messageType, identity, err)
				}
			}
			for _, missingType := range []string{"", " ", "unknown"} {
				if _, err := resolver.Identify(context.Background(), 18, 1, missingType, "member-1", "member"); !errors.Is(err, ErrChannelIdentityUnresolved) {
					t.Fatalf("消息类型 %q 不得回退到账号类型 %q：%v", missingType, legacyType, err)
				}
			}
		})
	}
}

func TestChannelIdentitySpeaker_RequiresMessageConversationType(t *testing.T) {
	repo := &memAccountRepo{rows: []model.ChannelAccount{{ID: 1, TenantID: 18, Type: "group"}}}
	resolver := NewChannelIdentityResolver(NewChannelResolverWithRepos(repo, nil))
	for _, messageType := range []string{"dm", "group"} {
		identity, err := resolver.IdentifySpeaker(context.Background(), 18, 1, messageType, "self-1", "self-1", nil)
		if err != nil || identity.ConversationType != messageType || !identity.Self {
			t.Fatalf("统一入口应按消息类型 %s 识别：identity=%+v err=%v", messageType, identity, err)
		}
	}
	for _, missingType := range []string{"", " ", "unknown"} {
		if _, err := resolver.IdentifySpeaker(context.Background(), 18, 1, missingType, "self-1", "self-1", nil); !errors.Is(err, ErrChannelIdentityUnresolved) {
			t.Fatalf("统一入口不得将消息类型 %q 默认成单聊：%v", missingType, err)
		}
	}
}

func TestChannelInbound_ConversationTypeComesFromMessage(t *testing.T) {
	ctx := tenant.WithTenantID(customEditionTestCtx(), 18)
	channels := newMemChannelRepo()
	channel := &model.Channel{TenantID: 18, Code: "qq", Enabled: true}
	if err := channels.Create(ctx, channel); err != nil {
		t.Fatal(err)
	}
	accounts := &memAccountRepo{rows: []model.ChannelAccount{{ID: 1, TenantID: 18, ChannelID: channel.ID, IdentifyCode: "account-ref", Type: "dm", Status: 1}}}
	resolver := NewChannelIdentityResolver(NewChannelResolverWithRepos(accounts, channels))
	archive := &memInboundArchive{seen: map[string]bool{}}
	orchestrator := NewChannelInboundOrchestrator(archive, resolver, nil)
	for _, message := range []model.InboundMessage{
		{EventID: "direct", Source: "bridge_http", Channel: "qq", AccountRef: "account-ref", TenantID: 18, SenderID: "member-1"},
		{EventID: "group", Source: "bridge_http", Channel: "qq", AccountRef: "account-ref", TenantID: 18, SenderID: "member-1", IsGroup: true, GroupID: "group-1"},
	} {
		out, err := orchestrator.DeliverAndArchive(ctx, &message)
		if err != nil || out.Status != "archived" || out.Automation != "skipped_unknown" {
			t.Fatalf("同一 QQ 账号应接收消息自身的会话类型：isGroup=%v out=%+v err=%v", message.IsGroup, out, err)
		}
	}
	if len(archive.rows) != 2 {
		t.Fatalf("单聊和群聊均应归档，实际 %d 条", len(archive.rows))
	}
}

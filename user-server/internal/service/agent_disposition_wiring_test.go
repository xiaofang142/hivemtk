package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestEnsureIncomingDispositionUsesGroupRootAndMessageIdentity(t *testing.T) {
	store := &dispositionWiringStore{items: map[string]*model.ConversationMessageDisposition{}}
	svc := NewConversationDispositionService(store)
	in := &IncomingContext{
		Platform: model.Platform("telegram"), AccountID: "42", SenderID: "u1",
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-9",
		MessageID: "hub:101", BusLLMSessionID: "llmsess_bus_1", OriginalSentAt: time.Unix(100, 0),
	}
	item, err := ensureIncomingDisposition(
		tenant.WithTenantID(context.Background(), 7), svc, in, "scope-hash-1",
	)
	if err != nil {
		t.Fatalf("ensure 失败: %v", err)
	}
	if item.ConversationID != "group-1" || item.MessageID != "hub:101" {
		t.Fatalf("群聊必须以 group_id 为会话根、真实消息 ID 为身份: %+v", item)
	}
	if item.ChannelAccountID != 42 || item.Status != model.ConversationDispositionPending {
		t.Fatalf("账号或初始状态错误: %+v", item)
	}
	if len(store.items) != 1 {
		t.Fatalf("应写入一条逐消息处置记录: %d", len(store.items))
	}
}

func TestEnsureIncomingDispositionFailClosedOnMissingIdentity(t *testing.T) {
	store := &dispositionWiringStore{items: map[string]*model.ConversationMessageDisposition{}}
	svc := NewConversationDispositionService(store)
	_, err := ensureIncomingDisposition(
		tenant.WithTenantID(context.Background(), 7), svc,
		&IncomingContext{Platform: model.PlatformDouyin, AccountID: "42", MessageID: "hub:1"},
		"scope-hash-1",
	)
	if err == nil {
		t.Fatal("缺 conversation/group 身份时必须 fail-closed")
	}
}

type dispositionWiringStore struct {
	items map[string]*model.ConversationMessageDisposition
}

func keyOf(tenantID, accountID uint, platform, conversationID, messageID string) string {
	return platform + ":" + string(rune(tenantID)) + ":" + string(rune(accountID)) + ":" + conversationID + ":" + messageID
}

func (s *dispositionWiringStore) Get(_ context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error) {
	return s.items[keyOf(tenantID, accountID, platform, conversationID, messageID)], nil
}

func (s *dispositionWiringStore) Save(_ context.Context, item *model.ConversationMessageDisposition) error {
	copy := *item
	s.items[keyOf(item.TenantID, item.ChannelAccountID, item.Platform, item.ConversationID, item.MessageID)] = &copy
	return nil
}

func (s *dispositionWiringStore) List(context.Context, uint, string, uint, string) ([]model.ConversationMessageDisposition, error) {
	return nil, nil
}

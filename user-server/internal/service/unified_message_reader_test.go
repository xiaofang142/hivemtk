package service

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestMergeUnifiedMessagesPreservesSourcesAndOrder(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	messages := []UnifiedMessage{
		{
			ContextMessage: agentcontext.ContextMessage{
				MessageID: "session:2", Origin: agentcontext.OriginSessionMessage,
				Content: "AI 回复", SentAt: base.Add(time.Minute), Direction: "outbound",
			},
		},
		{
			ContextMessage: agentcontext.ContextMessage{
				MessageID: "hub:1", Origin: agentcontext.OriginMessageHub,
				Content: "客户消息", SentAt: base, Direction: "inbound", SenderID: "customer-1",
			},
		},
	}

	got, err := mergeUnifiedMessages(messages)
	if err != nil {
		t.Fatalf("合并消息失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("消息数量错误: %d", len(got))
	}
	if got[0].MessageID != "hub:1" || got[1].MessageID != "session:2" {
		t.Fatalf("消息顺序或来源错误: %+v", got)
	}
	if got[0].SenderID != "customer-1" || !got[0].SentAt.Equal(base) {
		t.Fatalf("消息元数据丢失: %+v", got[0])
	}
}

func TestMergeUnifiedMessagesDeduplicatesOutboundByCorrelation(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	messages := []UnifiedMessage{
		{
			ContextMessage: agentcontext.ContextMessage{
				MessageID: "hub:1", Origin: agentcontext.OriginMessageHub, CorrelationID: "corr-1",
				Content: "相同回复", SentAt: sentAt, Direction: "outbound",
			},
		},
		{
			ContextMessage: agentcontext.ContextMessage{
				MessageID: "session:2", Origin: agentcontext.OriginSessionMessage, CorrelationID: "corr-1",
				Content: "相同回复", SentAt: sentAt.Add(time.Second), Direction: "outbound",
			},
		},
	}

	got, err := mergeUnifiedMessages(messages)
	if err != nil {
		t.Fatalf("重复出站应合并: %v", err)
	}
	if len(got) != 1 || got[0].MessageID != "hub:1" {
		t.Fatalf("重复出站必须保留 MessageHub 权威行: %+v", got)
	}
}

func TestMergeUnifiedMessagesRejectsAmbiguousDuplicates(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		messages []UnifiedMessage
	}{
		{
			name: "cross_source_content_conflict",
			messages: []UnifiedMessage{
				{ContextMessage: agentcontext.ContextMessage{MessageID: "hub:1", Origin: agentcontext.OriginMessageHub, CorrelationID: "corr-1", Content: "回复 A", SentAt: sentAt, Direction: "outbound"}},
				{ContextMessage: agentcontext.ContextMessage{MessageID: "session:2", Origin: agentcontext.OriginSessionMessage, CorrelationID: "corr-1", Content: "回复 B", SentAt: sentAt, Direction: "outbound"}},
			},
		},
		{
			name: "cross_source_direction_conflict",
			messages: []UnifiedMessage{
				{ContextMessage: agentcontext.ContextMessage{MessageID: "hub:1", Origin: agentcontext.OriginMessageHub, CorrelationID: "corr-1", Content: "相同内容", SentAt: sentAt, Direction: "inbound"}},
				{ContextMessage: agentcontext.ContextMessage{MessageID: "session:2", Origin: agentcontext.OriginSessionMessage, CorrelationID: "corr-1", Content: "相同内容", SentAt: sentAt, Direction: "outbound"}},
			},
		},
		{
			name: "same_source_conflict",
			messages: []UnifiedMessage{
				{ContextMessage: agentcontext.ContextMessage{MessageID: "hub:1", Origin: agentcontext.OriginMessageHub, CorrelationID: "corr-1", Content: "回复 A", SentAt: sentAt, Direction: "outbound"}},
				{ContextMessage: agentcontext.ContextMessage{MessageID: "hub:2", Origin: agentcontext.OriginMessageHub, CorrelationID: "corr-1", Content: "回复 B", SentAt: sentAt, Direction: "outbound"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeUnifiedMessages(tt.messages)
			if !errors.Is(err, ErrUnifiedMessageAmbiguousDuplicate) {
				t.Fatalf("歧义重复必须 fail-closed, got %v", err)
			}
		})
	}
}

func TestMergeUnifiedMessagesWithoutCorrelationDoesNotDedupe(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	messages := []UnifiedMessage{
		{ContextMessage: agentcontext.ContextMessage{MessageID: "hub:1", Origin: agentcontext.OriginMessageHub, Content: "相同回复", SentAt: sentAt, Direction: "outbound"}},
		{ContextMessage: agentcontext.ContextMessage{MessageID: "session:2", Origin: agentcontext.OriginSessionMessage, Content: "相同回复", SentAt: sentAt, Direction: "outbound"}},
	}
	got, err := mergeUnifiedMessages(messages)
	if err != nil {
		t.Fatalf("无 correlation 的消息不应误合并: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("无 correlation 必须保留两条来源: %+v", got)
	}
}

func TestUnifiedMessageFromHubPreservesEvidence(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	hub := model.MessageHub{
		ID: 42, TenantID: 7, Platform: "telegram", MsgID: "platform-msg-42", PlatformMsgID: "platform-msg-42",
		AccountID: "account-1", Direction: "inbound", SenderID: "customer-1", SenderName: "客户",
		Content: "你好", ConversationID: "conv-1", SentAt: sentAt, Visibility: model.VisibilityMember,
		AudienceJSON: model.JSONStrings{"customer-1"}, Extra: model.JSONMap{"correlation_id": "corr-1", "channel_msg_id": "channel-42"},
	}

	got := unifiedMessageFromHub(hub)
	if got.MessageID != "hub:42" || got.SourceMessageID != "platform-msg-42" {
		t.Fatalf("MessageHub 消息 ID 丢失: %+v", got)
	}
	if got.CorrelationID != "corr-1" || got.SenderID != "customer-1" || got.Visibility != agentcontext.VisibilityMember {
		t.Fatalf("MessageHub 来源元数据丢失: %+v", got)
	}
	if len(got.Audience) != 1 || got.Audience[0] != "customer-1" {
		t.Fatalf("MessageHub 受众证据丢失: %+v", got)
	}
	if got.SourceMetadata["channel_msg_id"] != "channel-42" {
		t.Fatalf("MessageHub 白名单来源元数据丢失: %+v", got.SourceMetadata)
	}
}

func TestUnifiedMessageReaderRejectsOutOfScopeHubRows(t *testing.T) {
	hubRepo := &fakeUnifiedMessageHubRepo{
		conversationRows: []model.MessageHub{
			{ID: 1, TenantID: 7, Platform: "telegram", AccountID: "account-1", ConversationID: "conv-other", Direction: "inbound", Content: "越界消息"},
			{ID: 2, TenantID: 7, Platform: "telegram", AccountID: "account-1", ConversationID: "conv-1", Direction: "inbound", Content: "范围内消息"},
		},
	}
	reader := NewUnifiedMessageReader(hubRepo, &fakeUnifiedMessageSessionRepo{})
	ctx := tenant.WithTenantID(context.Background(), 7)

	got, err := reader.List(ctx, UnifiedMessageQuery{
		TenantID: 7, Platform: "telegram", AccountIDs: []string{"account-1"},
		ConversationID: "conv-1", Limit: 10,
	})
	if err != nil {
		t.Fatalf("读取消息失败: %v", err)
	}
	if len(got) != 1 || got[0].Content != "范围内消息" {
		t.Fatalf("仓储返回越界会话消息时必须 fail-closed 过滤, got %+v", got)
	}
}

type fakeUnifiedMessageHubRepo struct {
	conversationRows []model.MessageHub
	groupRows        []model.MessageHub
	senderRows       []model.MessageHub
	lastScope        repository.MessageHubHistoryScope
	lastConversation string
	lastGroup        string
	lastSender       string
	lastLimit        int
}

func (r *fakeUnifiedMessageHubRepo) ListRecentByConversation(_ context.Context, scope repository.MessageHubHistoryScope, conversationID string, limit int) ([]model.MessageHub, error) {
	r.lastScope = scope
	r.lastConversation = conversationID
	r.lastLimit = limit
	return r.conversationRows, nil
}

func (r *fakeUnifiedMessageHubRepo) ListRecentByGroup(_ context.Context, scope repository.MessageHubHistoryScope, groupID string, limit int) ([]model.MessageHub, error) {
	r.lastScope = scope
	r.lastGroup = groupID
	r.lastLimit = limit
	return r.groupRows, nil
}

func (r *fakeUnifiedMessageHubRepo) ListRecentBySender(_ context.Context, scope repository.MessageHubHistoryScope, senderID string, limit int) ([]model.MessageHub, error) {
	r.lastScope = scope
	r.lastSender = senderID
	r.lastLimit = limit
	return r.senderRows, nil
}

type fakeUnifiedMessageSessionRepo struct {
	rows      []model.SessionMessage
	lastID    string
	lastLimit int
}

func (r *fakeUnifiedMessageSessionRepo) ListRecentDescBySessionID(_ context.Context, sessionID string, limit int) ([]model.SessionMessage, error) {
	r.lastID = sessionID
	r.lastLimit = limit
	return r.rows, nil
}

func TestUnifiedMessageFromSessionPreservesEvidence(t *testing.T) {
	createdAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	deliveredAt := createdAt.Add(time.Second)
	row := model.SessionMessage{
		ID: 9, TenantID: 7, SessionID: "session-1", CorrelationID: "corr-1", Content: "AI 回复",
		SenderType: "ai", SenderID: "agent-1", SenderName: "客服", CreatedAt: createdAt, DeliveredAt: &deliveredAt,
	}

	got := unifiedMessageFromSession(row, "telegram", "account-1", "conv-1", false, "")
	if got.MessageID != "session:9" || got.SourceMessageID != "9" {
		t.Fatalf("SessionMessage 消息 ID 丢失: %+v", got)
	}
	if got.CorrelationID != "corr-1" || got.Direction != "outbound" || got.Role != "assistant" {
		t.Fatalf("SessionMessage 方向或角色丢失: %+v", got)
	}
	if got.SenderID != "agent-1" || got.DeliveredAt == nil || !got.DeliveredAt.Equal(deliveredAt) {
		t.Fatalf("SessionMessage 发送者或送达证据丢失: %+v", got)
	}
	if got.Visibility != agentcontext.VisibilityUnknown {
		t.Fatalf("SessionMessage 无受众证据时必须为 unknown, got %q", got.Visibility)
	}
}

func setupRealUnifiedMessageReader(
	t *testing.T,
) (*UnifiedMessageReader, context.Context, *repository.MessageHubRepository, *repository.SessionMessageRepository) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.SessionMessage{})
	hubRepo := repository.NewMessageHubRepositoryWithDB(db)
	sessionRepo := repository.NewSessionMessageRepositoryWithDB(db)
	reader := NewUnifiedMessageReader(hubRepo, sessionRepo)
	ctx := tenant.WithTenantID(context.Background(), 7)
	return reader, ctx, hubRepo, sessionRepo
}

func TestUnifiedMessageReaderWithRealDB(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	t.Run("hub_only", func(t *testing.T) {
		reader, ctx, hubRepo, _ := setupRealUnifiedMessageReader(t)
		row := &model.MessageHub{
			TenantID: 7, Platform: "telegram", AccountID: "account-1", MsgID: "hub-only-1",
			Direction: "inbound", Status: "delivered", MsgType: "text", Content: "hub-only-message",
			SenderID: "customer-1", ReceiverID: "bot-1", ConversationID: "conv-hub-only",
			Visibility: model.VisibilityPublic, SentAt: base,
		}
		if err := hubRepo.Create(ctx, row); err != nil {
			t.Fatalf("写入 MessageHub 失败: %v", err)
		}

		got, err := reader.List(ctx, UnifiedMessageQuery{
			TenantID: 7, Platform: "telegram", AccountIDs: []string{"account-1"},
			ConversationID: "conv-hub-only", Limit: 10,
		})
		if err != nil {
			t.Fatalf("读取 hub-only 消息失败: %v", err)
		}
		if len(got) != 1 || got[0].Origin != agentcontext.OriginMessageHub || got[0].Content != row.Content {
			t.Fatalf("hub-only 结果不正确: %+v", got)
		}
	})

	t.Run("session_only", func(t *testing.T) {
		reader, ctx, _, sessionRepo := setupRealUnifiedMessageReader(t)
		row := &model.SessionMessage{
			TenantID: 7, SessionID: "session-only", Content: "session-only-message",
			ContentType: model.MessageTypeText, SenderType: "customer", SenderID: "customer-1",
			CreatedAt: base,
		}
		if err := sessionRepo.Create(ctx, row); err != nil {
			t.Fatalf("写入 SessionMessage 失败: %v", err)
		}

		got, err := reader.List(ctx, UnifiedMessageQuery{
			TenantID: 7, Platform: "telegram", AccountIDs: []string{"account-1"},
			ConversationID: "conv-session-only", SessionID: "session-only", SessionAccountID: "account-1", Limit: 10,
		})
		if err != nil {
			t.Fatalf("读取 session-only 消息失败: %v", err)
		}
		if len(got) != 1 || got[0].Origin != agentcontext.OriginSessionMessage || got[0].Content != row.Content {
			t.Fatalf("session-only 结果不正确: %+v", got)
		}
	})

	t.Run("duplicate_outbound_prefers_hub", func(t *testing.T) {
		reader, ctx, hubRepo, sessionRepo := setupRealUnifiedMessageReader(t)
		content := "same-outbound-reply"
		hubRow := &model.MessageHub{
			TenantID: 7, Platform: "telegram", AccountID: "account-1", MsgID: "hub-dup-1",
			Direction: "outbound", Status: "sent", MsgType: "text", Content: content,
			SenderID: "bot-1", ReceiverID: "customer-1", ConversationID: "conv-dup",
			Visibility: model.VisibilityPublic, SentAt: base,
			Extra: model.JSONMap{"correlation_id": "corr-dup"},
		}
		sessionRow := &model.SessionMessage{
			TenantID: 7, SessionID: "session-dup", CorrelationID: "corr-dup", Content: content,
			ContentType: model.MessageTypeText, SenderType: "ai", SenderID: "bot-1", CreatedAt: base.Add(time.Second),
		}
		if err := hubRepo.Create(ctx, hubRow); err != nil {
			t.Fatalf("写入 MessageHub 出站失败: %v", err)
		}
		if err := sessionRepo.Create(ctx, sessionRow); err != nil {
			t.Fatalf("写入 SessionMessage 出站失败: %v", err)
		}

		got, err := reader.List(ctx, UnifiedMessageQuery{
			TenantID: 7, Platform: "telegram", AccountIDs: []string{"account-1"},
			ConversationID: "conv-dup", SessionID: "session-dup", SessionAccountID: "account-1", Limit: 10,
		})
		if err != nil {
			t.Fatalf("读取重复出站失败: %v", err)
		}
		if len(got) != 1 || got[0].Origin != agentcontext.OriginMessageHub || got[0].CorrelationID != "corr-dup" {
			t.Fatalf("重复出站必须保留 MessageHub 权威行: %+v", got)
		}
	})

	t.Run("cross_scope_isolation", func(t *testing.T) {
		reader, ctx, hubRepo, sessionRepo := setupRealUnifiedMessageReader(t)
		for _, seed := range []struct {
			tenantID uint
			ctx      context.Context
			suffix   string
		}{
			{tenantID: 7, ctx: ctx, suffix: "ours"},
			{tenantID: 8, ctx: tenant.WithTenantID(context.Background(), 8), suffix: "other"},
		} {
			hubRow := &model.MessageHub{
				TenantID: seed.tenantID, Platform: "telegram", AccountID: "account-1", MsgID: "hub-cross-" + seed.suffix,
				Direction: "inbound", Status: "delivered", MsgType: "text", Content: "hub-" + seed.suffix,
				SenderID: "customer-1", ReceiverID: "bot-1", ConversationID: "conv-cross",
				Visibility: model.VisibilityPublic, SentAt: base,
			}
			if err := hubRepo.Create(seed.ctx, hubRow); err != nil {
				t.Fatalf("写入租户 %d MessageHub 失败: %v", seed.tenantID, err)
			}
			sessionRow := &model.SessionMessage{
				TenantID: seed.tenantID, SessionID: "session-cross", Content: "session-" + seed.suffix,
				ContentType: model.MessageTypeText, SenderType: "customer", SenderID: "customer-1", CreatedAt: base.Add(time.Second),
			}
			if err := sessionRepo.Create(seed.ctx, sessionRow); err != nil {
				t.Fatalf("写入租户 %d SessionMessage 失败: %v", seed.tenantID, err)
			}
		}

		got, err := reader.List(ctx, UnifiedMessageQuery{
			TenantID: 7, Platform: "telegram", AccountIDs: []string{"account-1"},
			ConversationID: "conv-cross", SessionID: "session-cross", SessionAccountID: "account-1", Limit: 10,
		})
		if err != nil {
			t.Fatalf("读取跨租户隔离消息失败: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("跨租户消息数量错误: %+v", got)
		}
		for _, message := range got {
			if message.TenantID != 7 {
				t.Fatalf("跨租户消息泄露: %+v", message)
			}
		}
	})
}

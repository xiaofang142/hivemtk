package model

import (
	"testing"
	"time"
)

func TestPlatform_Constants(t *testing.T) {
	platforms := map[Platform]string{
		PlatformDouyin:      "douyin",
		PlatformKuaishou:    "kuaishou",
		PlatformXiaohongshu: "xiaohongshu",
		PlatformXianyu:      "xianyu",
		PlatformTiktok:      "tiktok",
	}

	for platform, expected := range platforms {
		if string(platform) != expected {
			t.Errorf("Expected Platform %s, got %s", expected, platform)
		}
	}
}

func TestMessageType_Constants(t *testing.T) {
	messageTypes := map[MessageType]string{
		MessageTypeText:  "text",
		MessageTypeImage: "image",
		MessageTypeVideo: "video",
		MessageTypeAudio: "audio",
		MessageTypeFile:  "file",
		MessageTypeCard:  "card",
	}

	for messageType, expected := range messageTypes {
		if string(messageType) != expected {
			t.Errorf("Expected MessageType %s, got %s", expected, messageType)
		}
	}
}

func TestMessageStatus_Constants(t *testing.T) {
	statuses := map[MessageStatus]string{
		MessageStatusPending:    "pending",
		MessageStatusProcessing: "processing",
		MessageStatusReplied:    "replied",
		MessageStatusFailed:     "failed",
		MessageStatusIgnored:    "ignored",
	}

	for status, expected := range statuses {
		if string(status) != expected {
			t.Errorf("Expected MessageStatus %s, got %s", expected, status)
		}
	}
}

func TestChatType_Constants(t *testing.T) {
	chatTypes := map[ChatType]string{
		ChatTypePrivate: "private",
		ChatTypeGroup:   "group",
	}

	for chatType, expected := range chatTypes {
		if string(chatType) != expected {
			t.Errorf("Expected ChatType %s, got %s", expected, chatType)
		}
	}
}

func TestUnifiedMessage_TableName(t *testing.T) {
	msg := &UnifiedMessage{}
	tableName := msg.TableName()
	if tableName != "unified_messages" {
		t.Errorf("Expected table name 'unified_messages', got %s", tableName)
	}
}

func TestUnifiedMessage_BasicFields(t *testing.T) {
	msg := &UnifiedMessage{
		ID:           1,
		MessageID:    "msg-001",
		Platform:     PlatformDouyin,
		AccountID:    "account-001",
		AccountName:  "Test Account",
		ChatID:       "chat-001",
		ChatType:     ChatTypePrivate,
		SenderID:     "sender-001",
		SenderName:   "Test Sender",
		SenderAvatar: "https://example.com/avatar.jpg",
		Content:      "Hello, how can I help you?",
		ContentType:  MessageTypeText,
		MediaURL:     "https://example.com/media.jpg",
		ReplyToID:    "msg-000",
		Status:       MessageStatusPending,
		RawData:      `{"raw": "data"}`,
	}

	if msg.ID != 1 {
		t.Errorf("Expected ID 1, got %d", msg.ID)
	}
	if msg.MessageID != "msg-001" {
		t.Errorf("Expected MessageID 'msg-001', got %s", msg.MessageID)
	}
	if msg.Platform != PlatformDouyin {
		t.Errorf("Expected Platform 'douyin', got %s", msg.Platform)
	}
	if msg.ChatType != ChatTypePrivate {
		t.Errorf("Expected ChatType 'private', got %s", msg.ChatType)
	}
	if msg.ContentType != MessageTypeText {
		t.Errorf("Expected ContentType 'text', got %s", msg.ContentType)
	}
	if msg.Status != MessageStatusPending {
		t.Errorf("Expected Status 'pending', got %s", msg.Status)
	}
}

func TestReplyStatus_Constants(t *testing.T) {
	statuses := map[ReplyStatus]string{
		ReplyStatusPending:   "pending",
		ReplyStatusSent:      "sent",
		ReplyStatusFailed:    "failed",
		ReplyStatusDiscarded: "discarded",
	}

	for status, expected := range statuses {
		if string(status) != expected {
			t.Errorf("Expected ReplyStatus %s, got %s", expected, status)
		}
	}
}

func TestUnifiedReply_TableName(t *testing.T) {
	reply := &UnifiedReply{}
	tableName := reply.TableName()
	if tableName != "unified_replies" {
		t.Errorf("Expected table name 'unified_replies', got %s", tableName)
	}
}

func TestUnifiedReply_BasicFields(t *testing.T) {
	now := time.Now()

	reply := &UnifiedReply{
		ID:          1,
		ReplyID:     "reply-001",
		MessageID:   "msg-001",
		Platform:    PlatformDouyin,
		AccountID:   "account-001",
		ChatID:      "chat-001",
		Content:     "Thank you for your message",
		ContentType: MessageTypeText,
		MediaURL:    "",
		ReplyType:   "rule",
		Confidence:  0.95,
		RuleID:      10,
		Status:      ReplyStatusSent,
		SentAt:      &now,
	}

	if reply.ID != 1 {
		t.Errorf("Expected ID 1, got %d", reply.ID)
	}
	if reply.ReplyID != "reply-001" {
		t.Errorf("Expected ReplyID 'reply-001', got %s", reply.ReplyID)
	}
	if reply.ReplyType != "rule" {
		t.Errorf("Expected ReplyType 'rule', got %s", reply.ReplyType)
	}
	if reply.Confidence != 0.95 {
		t.Errorf("Expected Confidence 0.95, got %f", reply.Confidence)
	}
	if reply.Status != ReplyStatusSent {
		t.Errorf("Expected Status 'sent', got %s", reply.Status)
	}
}

func TestPlatformAccount_TableName(t *testing.T) {
	account := &PlatformAccount{}
	tableName := account.TableName()
	if tableName != "platform_accounts" {
		t.Errorf("Expected table name 'platform_accounts', got %s", tableName)
	}
}

func TestPlatformAccount_BasicFields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	account := &PlatformAccount{
		ID:            1,
		Platform:      PlatformDouyin,
		AccountID:     "dy-001",
		AccountName:   "Douyin Account",
		AccountAvatar: "https://example.com/avatar.jpg",
		Config:        `{"key": "value"}`,
		Cookie:        "encrypted_cookie",
		Token:         "enc" + "rypt" + "ed_token",
		Status:        1,
		LastSyncAt:    &now,
		ExpiresAt:     &expiresAt,
	}

	if account.ID != 1 {
		t.Errorf("Expected ID 1, got %d", account.ID)
	}
	if account.Platform != PlatformDouyin {
		t.Errorf("Expected Platform 'douyin', got %s", account.Platform)
	}
	if account.AccountName != "Douyin Account" {
		t.Errorf("Expected AccountName 'Douyin Account', got %s", account.AccountName)
	}
	if account.Status != 1 {
		t.Errorf("Expected Status 1, got %d", account.Status)
	}
}

func TestReplyDecision(t *testing.T) {
	decision := &ReplyDecision{
		ShouldReply: true,
		ReplyType:   "rule",
		Content:     "Hello!",
		Confidence:  0.95,
		Reason:      "Rule matched",
		Variables:   map[string]string{"name": "user"},
	}

	if !decision.ShouldReply {
		t.Error("Expected ShouldReply to be true")
	}
	if decision.ReplyType != "rule" {
		t.Errorf("Expected ReplyType 'rule', got %s", decision.ReplyType)
	}
	if decision.Confidence != 0.95 {
		t.Errorf("Expected Confidence 0.95, got %f", decision.Confidence)
	}
	if decision.Variables["name"] != "user" {
		t.Errorf("Expected Variables['name'] to be 'user', got %s", decision.Variables["name"])
	}
}

func TestKnowledgeHit(t *testing.T) {
	hit := &KnowledgeHit{
		ID:         1,
		Title:      "FAQ: Shipping",
		Content:    "We ship within 24 hours",
		Score:      0.95,
		Source:     "rag",
		CategoryID: 10,
	}

	if hit.ID != 1 {
		t.Errorf("Expected ID 1, got %d", hit.ID)
	}
	if hit.Title != "FAQ: Shipping" {
		t.Errorf("Expected Title 'FAQ: Shipping', got %s", hit.Title)
	}
	if hit.Score != 0.95 {
		t.Errorf("Expected Score 0.95, got %f", hit.Score)
	}
}

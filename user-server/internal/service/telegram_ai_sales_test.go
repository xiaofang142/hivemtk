package service

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

func setupTelegramTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.TelegramAccount{},
		&model.WebhookEvent{},
		&model.Customer{},
		&model.IntegrationAccount{},
		&model.UnifiedMessage{},
		&model.Clue{},
		&model.ClueScore{},
		&model.ClueEngagementEvent{},
	)
}

// newTestWebhookService 构造仅携带测试本地库的 WebhookService：
// ARC-01 删除 db 字段后，经 eventRepo 注入使 lazyDB() 返回测试库，
// ensureReposFromDB 才能把 messageHubRepo/clueRepo 等惰性建到该库上。
func newTestWebhookService(db *gorm.DB) *WebhookService {
	eventRepo := repository.NewWebhookEventRepository()
	repository.SetWebhookEventRepoDB(eventRepo, db)
	return &WebhookService{eventRepo: eventRepo}
}

// TestDispatchTelegram_JoinEvent_NewChatMembers 验证 TG 入群事件被正确解析并写入消息中台
func TestDispatchTelegram_JoinEvent_NewChatMembers(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1001,
		"message": {
			"message_id": 5001,
			"from": {"id": 999, "first_name": "Inviter", "is_bot": false},
			"chat": {"id": -1001234567890, "type": "supergroup", "title": "销售交流群"},
			"date": 1700000000,
			"new_chat_members": [
				{"id": 8888, "first_name": "Alice", "username": "alice88", "is_bot": false},
				{"id": 8889, "first_name": "BobBot", "is_bot": true}
			]
		}
	}`)

	p := &ParsedPayload{
		EventID:   "tg_join_evt_1",
		EventType: "message",
	}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("dispatchTelegram join event failed: %v", err)
	}
	if hub == nil {
		t.Fatal("expected MessageHub entry for join event, got nil")
	}
	if hub.Platform != "telegram" {
		t.Errorf("expected platform=telegram, got %s", hub.Platform)
	}
	if hub.MsgType != "event" {
		t.Errorf("expected msg_type=event, got %s", hub.MsgType)
	}
	if hub.AccountID != "1" {
		t.Errorf("expected account_id=1, got %s", hub.AccountID)
	}
	expectedMsgID := fmt.Sprintf("tg_join_%d_%d", -1001234567890, 8888)
	if hub.MsgID != expectedMsgID {
		t.Errorf("expected msg_id=%s, got %s", expectedMsgID, hub.MsgID)
	}
	if hub.SenderID != "8888" {
		t.Errorf("expected sender_id=8888, got %s", hub.SenderID)
	}
	if !hub.IsGroup {
		t.Error("expected is_group=true for supergroup join event")
	}
	if hub.GroupID != "-1001234567890" {
		t.Errorf("expected group_id=-1001234567890, got %s", hub.GroupID)
	}
	if hub.Content == "" {
		t.Error("expected non-empty event content")
	}

	var count int64
	db.Model(&model.MessageHub{}).Where("platform = ? AND msg_type = ?", "telegram", "event").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 event row in DB, got %d", count)
	}
}

// TestDispatchTelegram_JoinEvent_OnlyBotsSkipped 验证入群成员全是 bot 时不触发入群事件流程
func TestDispatchTelegram_JoinEvent_OnlyBotsSkipped(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1002,
		"message": {
			"message_id": 5002,
			"from": {"id": 999, "first_name": "Inviter", "is_bot": false},
			"chat": {"id": -1001234567891, "type": "group", "title": "测试群"},
			"date": 1700000001,
			"new_chat_members": [
				{"id": 7001, "first_name": "GuardBot", "is_bot": true}
			]
		}
	}`)

	p := &ParsedPayload{EventID: "tg_join_evt_2", EventType: "message"}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if hub != nil && hub.MsgType == "event" {
		t.Errorf("expected no event-type hub when only bots joined, got event: %+v", hub)
	}
	var eventCount int64
	db.Model(&model.MessageHub{}).Where("msg_type = ?", "event").Count(&eventCount)
	if eventCount != 0 {
		t.Errorf("expected 0 event rows when only bots, got %d", eventCount)
	}
}

// TestDispatchTelegram_LeftEvent_RecordsToHub 验证退群事件被记录但不触发 AI
func TestDispatchTelegram_LeftEvent_RecordsToHub(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1003,
		"message": {
			"message_id": 5003,
			"from": {"id": 8888, "first_name": "Alice", "is_bot": false},
			"chat": {"id": -1001234567892, "type": "supergroup", "title": "退群测试"},
			"date": 1700000002,
			"left_chat_member": {"id": 8888, "first_name": "Alice", "username": "alice88", "is_bot": false}
		}
	}`)

	p := &ParsedPayload{EventID: "tg_left_evt_1", EventType: "message"}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("dispatchTelegram left event failed: %v", err)
	}
	if hub == nil {
		t.Fatal("expected MessageHub entry for left event, got nil")
	}
	if hub.MsgType != "event" {
		t.Errorf("expected msg_type=event, got %s", hub.MsgType)
	}
	expectedMsgID := fmt.Sprintf("tg_left_%d_%d", -1001234567892, 8888)
	if hub.MsgID != expectedMsgID {
		t.Errorf("expected msg_id=%s, got %s", expectedMsgID, hub.MsgID)
	}
}

// TestDispatchTelegram_RegularMessage_TextToHub 验证普通文本消息被正确写入消息中台
func TestDispatchTelegram_RegularMessage_TextToHub(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1004,
		"message": {
			"message_id": 5004,
			"from": {"id": 12345, "first_name": "Customer", "username": "customer1", "is_bot": false},
			"chat": {"id": 12345, "type": "private"},
			"date": 1700000003,
			"text": "你好，我想咨询产品价格"
		}
	}`)

	p := &ParsedPayload{EventID: "tg_msg_1", EventType: "message"}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("dispatchTelegram text message failed: %v", err)
	}
	if hub == nil {
		t.Fatal("expected MessageHub entry, got nil")
	}
	if hub.MsgType != "text" {
		t.Errorf("expected msg_type=text, got %s", hub.MsgType)
	}
	if hub.Content != "你好，我想咨询产品价格" {
		t.Errorf("expected content, got %s", hub.Content)
	}
	if hub.SenderID != "12345" {
		t.Errorf("expected sender_id=12345, got %s", hub.SenderID)
	}
	if hub.IsGroup {
		t.Error("private chat should not be marked as group")
	}
}

// TestDispatchTelegram_GroupMessage_IsGroupTrue 验证群组消息 IsGroup=true
func TestDispatchTelegram_GroupMessage_IsGroupTrue(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1005,
		"message": {
			"message_id": 5005,
			"from": {"id": 2222, "first_name": "GroupUser", "is_bot": false},
			"chat": {"id": -100999, "type": "group", "title": "产品咨询群"},
			"date": 1700000004,
			"text": "这个产品多少钱？"
		}
	}`)

	p := &ParsedPayload{EventID: "tg_group_msg_1", EventType: "message"}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("dispatchTelegram group message failed: %v", err)
	}
	if hub == nil {
		t.Fatal("expected MessageHub entry")
	}
	if !hub.IsGroup {
		t.Error("group message should have IsGroup=true")
	}
	if hub.GroupID != "-100999" {
		t.Errorf("expected group_id=-100999, got %s", hub.GroupID)
	}
}

// TestDispatchTelegram_SystemNotification_Skipped 验证系统通知消息（new_chat_title 等）被跳过
func TestDispatchTelegram_SystemNotification_Skipped(t *testing.T) {
	db := setupTelegramTestDB(t)
	svc := newTestWebhookService(db)

	payload := []byte(`{
		"update_id": 1006,
		"message": {
			"message_id": 5006,
			"from": {"id": 2222, "first_name": "Admin", "is_bot": false},
			"chat": {"id": -100999, "type": "group", "title": "新群名"},
			"date": 1700000005,
			"new_chat_title": "新群名"
		}
	}`)

	p := &ParsedPayload{EventID: "tg_sys_1", EventType: "message"}
	hub, _, err := svc.dispatchTelegram(context.Background(), "1", p, payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if hub != nil {
		t.Errorf("expected nil hub for system notification, got %+v", hub)
	}
}

// TestShouldTriggerAI_TelegramAccountStates 验证不同状态的 TG 账号触发判定
func TestShouldTriggerAI_TelegramAccountStates(t *testing.T) {
	db := setupTelegramTestDB(t)
	tgRepo := repository.NewTelegramAccountRepository()
	tgRepo.SetDB(context.Background(), db)

	cases := []struct {
		name            string
		account         *model.TelegramAccount
		accountID       string
		salesEngineNil  bool
		expectedTrigger bool
	}{
		{
			name: "enabled_and_active",
			account: &model.TelegramAccount{
				AccountName:    "active-bot",
				BotToken:       "token-1",
				AIAgentEnabled: true,
				Status:         1,
			},
			accountID:       "1",
			expectedTrigger: true,
		},
		{
			name: "ai_disabled",
			account: &model.TelegramAccount{
				AccountName:    "ai-off-bot",
				BotToken:       "token-2",
				AIAgentEnabled: false,
				Status:         1,
			},
			accountID:       "2",
			expectedTrigger: false,
		},
		{
			name: "account_disabled",
			account: &model.TelegramAccount{
				AccountName:    "disabled-bot",
				BotToken:       "token-3",
				AIAgentEnabled: true,
				Status:         2,
			},
			accountID:       "3",
			expectedTrigger: false,
		},
		{
			name: "nonexistent_account",
			account: &model.TelegramAccount{
				AccountName:    "will-not-be-used",
				BotToken:       "token-4",
				AIAgentEnabled: true,
				Status:         1,
			},
			accountID:       "999",
			expectedTrigger: false,
		},
	}

	for i, c := range cases {
		if i == 3 {
			break
		}
		if err := tgRepo.Create(context.Background(), c.account); err != nil {
			t.Fatalf("create account %d: %v", i, err)
		}
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &WebhookService{
				telegramRepo: tgRepo,
				salesEngine:  &SalesEngine{},
			}
			got := svc.shouldTriggerAI(context.Background(), ChannelTelegram, c.accountID)
			if got != c.expectedTrigger {
				t.Errorf("case=%s expected %v, got %v", c.name, c.expectedTrigger, got)
			}
		})
	}
}

// TestShouldTriggerAI_NilSalesEngineReturnsFalse 验证 salesEngine 为 nil 时不触发
func TestShouldTriggerAI_NilSalesEngineReturnsFalse(t *testing.T) {
	db := setupTelegramTestDB(t)
	tgRepo := repository.NewTelegramAccountRepository()
	tgRepo.SetDB(context.Background(), db)
	acc := &model.TelegramAccount{
		AccountName:    "test-bot",
		BotToken:       "tok",
		AIAgentEnabled: true,
		Status:         1,
	}
	if err := tgRepo.Create(context.Background(), acc); err != nil {
		t.Fatalf("create: %v", err)
	}

	svc := &WebhookService{
		telegramRepo: tgRepo,
		salesEngine:  nil,
	}
	if svc.shouldTriggerAI(context.Background(), ChannelTelegram, "1") {
		t.Error("expected false when salesEngine is nil")
	}
}

// TestShouldTriggerAI_InvalidAccountIDReturnsFalse 验证非法 accountID 不触发
func TestShouldTriggerAI_InvalidAccountIDReturnsFalse(t *testing.T) {
	db := setupTelegramTestDB(t)
	tgRepo := repository.NewTelegramAccountRepository()
	tgRepo.SetDB(context.Background(), db)

	svc := &WebhookService{
		telegramRepo: tgRepo,
		salesEngine:  &SalesEngine{},
	}
	if svc.shouldTriggerAI(context.Background(), ChannelTelegram, "abc") {
		t.Error("expected false for non-numeric accountID")
	}
	if svc.shouldTriggerAI(context.Background(), ChannelTelegram, "0") {
		t.Error("expected false for accountID=0")
	}
	if svc.shouldTriggerAI(context.Background(), ChannelTelegram, "") {
		t.Error("expected false for empty accountID")
	}
}

// TestTriggerTelegramJoinSales_NilSalesEngineNoCrash 验证 salesEngine 为 nil 时安全返回
func TestTriggerTelegramJoinSales_NilSalesEngineNoCrash(t *testing.T) {
	svc := &WebhookService{salesEngine: nil}
	svc.triggerTelegramJoinSales(context.Background(), "1", "-1001234567890", "8888", "新用户加入群组")
}

// TestTriggerTelegramJoinSales_ShouldNotTriggerWhenAIDisabled 验证 AI 关闭时不触发
func TestTriggerTelegramJoinSales_ShouldNotTriggerWhenAIDisabled(t *testing.T) {
	db := setupTelegramTestDB(t)
	tgRepo := repository.NewTelegramAccountRepository()
	tgRepo.SetDB(context.Background(), db)
	acc := &model.TelegramAccount{
		AccountName:    "ai-off",
		BotToken:       "tok",
		AIAgentEnabled: false,
		Status:         1,
	}
	if err := tgRepo.Create(context.Background(), acc); err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := &WebhookService{
		telegramRepo: tgRepo,
		salesEngine:  &SalesEngine{},
	}
	svc.triggerTelegramJoinSales(context.Background(), "1", "-1001234567890", "8888", "新用户加入群组")
}

// TestWebhookService_Receive_TelegramJoinEvent 验证完整 Receive 链路处理 TG 入群事件
// 跳过验签（无 secret 时直接通过）→ 解析 → 入队 → 处理
func TestWebhookService_Receive_TelegramJoinEvent(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	db := setupTelegramTestDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	payload := []byte(`{
		"update_id": 2001,
		"message": {
			"message_id": 7001,
			"from": {"id": 999, "first_name": "Admin", "is_bot": false},
			"chat": {"id": -100555, "type": "supergroup", "title": "智能体测试群"},
			"date": 1700000010,
			"new_chat_members": [
				{"id": 7777, "first_name": "NewCustomer", "username": "newcustomer", "is_bot": false}
			]
		}
	}`)

	result, err := svc.Receive(nil, &ReceiveRequest{
		Channel:   ChannelTelegram,
		AccountID: "1",
		Body:      payload,
		Headers:   map[string]string{},
	})
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}
	if result == nil || !result.Accepted {
		t.Fatalf("expected accepted result, got %+v", result)
	}
	if result.Duplicate {
		t.Error("should not be duplicate on first receive")
	}
}

// TestTelegramAccountRepository_CRUD 验证 TG 账号仓库 CRUD 操作
func TestTelegramAccountRepository_CRUD(t *testing.T) {
	db := setupTelegramTestDB(t)
	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), db)

	acc := &model.TelegramAccount{
		AccountName:    "crud-test-bot",
		BotToken:       "123456:ABC-DEF",
		WebhookURL:     "https://example.com/api/webhook/telegram/1",
		WebhookSecret:  "secret-x",
		WebhookEnabled: true,
		AIAgentEnabled: true,
		Status:         1,
	}
	if err := repo.Create(context.Background(), acc); err != nil {
		t.Fatalf("create: %v", err)
	}
	if acc.ID == 0 {
		t.Fatal("expected ID populated after create")
	}

	got, err := repo.GetByID(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AccountName != "crud-test-bot" || got.BotToken != "123456:ABC-DEF" {
		t.Errorf("unexpected account: %+v", got)
	}
	if !got.AIAgentEnabled || !got.WebhookEnabled {
		t.Error("expected flags preserved")
	}

	all, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 account, got %d", len(all))
	}

	got.AIAgentEnabled = false
	got.LastErrorMsg = "test error"
	if err := repo.Update(context.Background(), got); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := repo.GetByID(context.Background(), acc.ID)
	if updated.AIAgentEnabled {
		t.Error("expected AIAgentEnabled=false after update")
	}
	if updated.LastErrorMsg != "test error" {
		t.Errorf("expected last_error_msg=test error, got %s", updated.LastErrorMsg)
	}

	if err := repo.Delete(context.Background(), acc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all2, _ := repo.GetAll(context.Background())
	if len(all2) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(all2))
	}
}

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestSendTelegram_NoRegistry 验证未注册 sender 时返回错误
func TestSendTelegram_NoRegistry(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	req := &ProactiveReachRequest{Content: "hi"}
	if _, err := svc.sendTelegram(context.Background(), req, "12345", "1"); err == nil {
		t.Fatalf("未注册 sender 应返回错误")
	}
}

// TestSendTelegram_InvalidChatID 验证 chatID 解析失败
func TestSendTelegram_InvalidChatID(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	called := false
	svc.SetTelegramRegistry(func(_ context.Context, _ uint, _ int64, _ string) error {
		called = true
		return nil
	})
	req := &ProactiveReachRequest{Content: "hi"}
	if _, err := svc.sendTelegram(context.Background(), req, "abc", "1"); err == nil {
		t.Fatalf("非数字 chatID 应返回错误")
	}
	if called {
		t.Fatalf("chatID 无效不应调用 sender")
	}
	if _, err := svc.sendTelegram(context.Background(), req, "0", "1"); err == nil {
		t.Fatalf("chatID=0 应返回错误")
	}
}

// TestSendTelegram_SenderError 验证 sender 失败透传
func TestSendTelegram_SenderError(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	wantErr := errors.New("telegram api timeout")
	svc.SetTelegramRegistry(func(_ context.Context, _ uint, _ int64, _ string) error {
		return wantErr
	})
	req := &ProactiveReachRequest{Content: "hi"}
	_, err := svc.sendTelegram(context.Background(), req, "12345", "1")
	if err != wantErr {
		t.Fatalf("期望透传 %v 实际 %v", wantErr, err)
	}
}

// TestSendTelegram_Success 验证成功路径 response 字段
func TestSendTelegram_Success(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	var gotAccID uint
	var gotChatID int64
	var gotContent string
	svc.SetTelegramRegistry(func(_ context.Context, accID uint, chatID int64, content string) error {
		gotAccID, gotChatID, gotContent = accID, chatID, content
		return nil
	})
	req := &ProactiveReachRequest{Content: "你好"}
	resp, err := svc.sendTelegram(context.Background(), req, "-1001234567890", "42")
	if err != nil {
		t.Fatalf("成功路径不应报错: %v", err)
	}
	if gotAccID != 42 || gotChatID != -1001234567890 || gotContent != "你好" {
		t.Fatalf("sender 收参异常 acc=%d chat=%d content=%q", gotAccID, gotChatID, gotContent)
	}
	if resp.Channel != "telegram" || resp.RecipientID != "-1001234567890" || resp.AccountID != "42" || resp.Status != "sent" {
		t.Fatalf("response 字段异常: %+v", resp)
	}
	if resp.Strategy != "customer has telegram_chat_id" {
		t.Fatalf("strategy 异常: %s", resp.Strategy)
	}
	if !strings.HasPrefix(resp.MessageID, "tg_") {
		t.Fatalf("messageID 应以 tg_ 开头: %s", resp.MessageID)
	}
	if resp.SentAt.IsZero() {
		t.Fatalf("SentAt 不应为零值")
	}
}

// TestSendTelegram_LargeChatID 验证大 chatID（负数群组）正确解析
func TestSendTelegram_LargeChatID(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	var got int64
	svc.SetTelegramRegistry(func(_ context.Context, _ uint, chatID int64, _ string) error {
		got = chatID
		return nil
	})
	req := &ProactiveReachRequest{Content: "hi"}
	const bigID = -1001234567890123
	if _, err := svc.sendTelegram(context.Background(), req, "-1001234567890123", "1"); err != nil {
		t.Fatalf("大 chatID 应成功: %v", err)
	}
	if got != bigID {
		t.Fatalf("大 chatID 解析异常: got=%d want=%d", got, bigID)
	}
}

// TestSendTelegram_RegistriesIsolated 验证 TG registry 与其他渠道隔离
func TestSendTelegram_RegistriesIsolated(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	tgCalled := false
	svc.SetTelegramRegistry(func(_ context.Context, _ uint, _ int64, _ string) error {
		tgCalled = true
		return nil
	})
	req := &ProactiveReachRequest{Content: "hi"}
	if _, err := svc.sendWhatsApp(context.Background(), req, "13800138000", "1"); err == nil {
		t.Fatalf("未注册 WA sender 应返回错误")
	}
	if tgCalled {
		t.Fatalf("WA 未注册不应触发 TG sender")
	}
}

// TestTriggerDMOutreach_NilSafe 验证 nil svc / nil svc.svc 不 panic
func TestTriggerDMOutreach_NilSafe(t *testing.T) {
	var nilSvc *TelegramDMOutreachService
	nilSvc.TriggerDMOutreach(context.Background(), "1", 123, "-100", "群", 80, true, "你好")

	svc := &TelegramDMOutreachService{svc: nil}
	svc.TriggerDMOutreach(context.Background(), "1", 123, "-100", "群", 80, true, "你好")
}

// TestTriggerDMOutreach_ScoreBelowThreshold 验证意向分不足不发送
func TestTriggerDMOutreach_ScoreBelowThreshold(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	svc := NewTelegramDMOutreachService(ws)

	svc.TriggerDMOutreach(context.Background(), "1", 9999991, "-100", "群", 59, true, "hi")
}

// TestTriggerDMOutreach_NotOpportunity 验证非商机不发送
func TestTriggerDMOutreach_NotOpportunity(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	svc := NewTelegramDMOutreachService(ws)
	svc.TriggerDMOutreach(context.Background(), "1", 9999992, "-100", "群", 80, false, "hi")
}

// TestTriggerDMOutreach_CooldownBlocks 验证冷却期内不重复发送
// 注：首次调用会触发真实 tgIntegration.SendMessage（因无 bot 账号会失败 log warn return），
// 但冷却 key 已 SetNX，第二次同 user+group 会被冷却拦截。
func TestTriggerDMOutreach_CooldownBlocks(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	svc := NewTelegramDMOutreachService(ws)
	const uid int64 = 9999993

	svc.TriggerDMOutreach(context.Background(), "1", uid, "-100", "群", 80, true, "hi")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("冷却路径不应 panic: %v", r)
		}
	}()
	svc.TriggerDMOutreach(context.Background(), "1", uid, "-100", "群", 80, true, "hi")
}

// TestTriggerDMOutreach_DMCooldownBlocks 验证 DM 维度冷却
func TestTriggerDMOutreach_DMCooldownBlocks(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	svc := NewTelegramDMOutreachService(ws)
	const uid int64 = 9999994
	svc.TriggerDMOutreach(context.Background(), "1", uid, "-100A", "群A", 80, true, "hi")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DM 冷却路径不应 panic: %v", r)
		}
	}()
	svc.TriggerDMOutreach(context.Background(), "1", uid, "-100B", "群B", 80, true, "hi")
}

// TestBuildDMWelcomeTemplate_LanguageSelection 验证中英模板选择
func TestBuildDMWelcomeTemplate_LanguageSelection(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	svc := NewTelegramDMOutreachService(ws)

	zh := svc.buildDMWelcomeTemplate("销售群", "我想买产品")
	if !strings.Contains(zh, "销售群") || !strings.Contains(zh, "看到你的发言") {
		t.Fatalf("中文模板异常: %s", zh)
	}

	en := svc.buildDMWelcomeTemplate("SalesGroup", "I want to buy")
	if !strings.Contains(en, "SalesGroup") || !strings.Contains(en, "I noticed") {
		t.Fatalf("英文模板异常: %s", en)
	}

	def := svc.buildDMWelcomeTemplate("群", "")
	if !strings.Contains(def, "提到了产品需求") {
		t.Fatalf("默认模板异常: %s", def)
	}

	emptyGroup := svc.buildDMWelcomeTemplate("", "")
	if !strings.Contains(emptyGroup, "相关群组") {
		t.Fatalf("空群名应回退「相关群组」: %s", emptyGroup)
	}
}

// TestTriggerTGDMOutreach_InvalidFromID 验证 fromID 解析失败不 panic
func TestTriggerTGDMOutreach_InvalidFromID(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	ws.triggerTGDMOutreach(context.Background(), "1", "not-a-number", "-100", "群", 80, "hi")
	ws.triggerTGDMOutreach(context.Background(), "1", "0", "-100", "群", 80, "hi")
}

// TestParseAccountID 验证 accountID 解析
func TestParseAccountID(t *testing.T) {
	if got := parseAccountID("0"); got != 0 {
		t.Fatalf("0 应返回 0，实际 %d", got)
	}
	if got := parseAccountID("abc"); got != 0 {
		t.Fatalf("非数字应返回 0，实际 %d", got)
	}
	if got := parseAccountID("42"); got != 42 {
		t.Fatalf("42 应返回 42，实际 %d", got)
	}
}

// TestRecordDMOutreachEvent 验证记录 outreach 事件到 message_hub
func TestRecordDMOutreachEvent(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := newTestWebhookService(db)
	ws.ensureReposFromDB(context.Background())
	svc := NewTelegramDMOutreachService(ws)

	svc.recordDMOutreachEvent(context.Background(), "1", 8888888, "-100", 75)

	var count int64
	if err := db.Table("message_hub").Where("platform = ? AND direction = ? AND sender_id = ?",
		"telegram", "outbound", "1").Count(&count).Error; err != nil {
		t.Fatalf("查询 message_hub 失败: %v", err)
	}
	if count == 0 {
		t.Fatalf("应记录 outreach 事件到 message_hub")
	}

	var hubs []telegramOutreachHub
	if err := db.Table("message_hub").Where("platform = ? AND sender_id = ?", "telegram", "1").
		Order("created_at DESC").Limit(1).Scan(&hubs).Error; err != nil || len(hubs) == 0 {
		t.Fatalf("查询最新 outreach 记录失败: err=%v len=%d", err, len(hubs))
	}
}

type telegramOutreachHub struct {
	MsgID      string `gorm:"column:msg_id"`
	Direction  string `gorm:"column:direction"`
	MsgType    string `gorm:"column:msg_type"`
	SenderID   string `gorm:"column:sender_id"`
	ReceiverID string `gorm:"column:receiver_id"`
	Platform   string `gorm:"column:platform"`
}

// TestTriggerDMOutreach_DoesNotPanicOnMissingDB 验证 db 为 nil 时不 panic
func TestTriggerDMOutreach_DoesNotPanicOnMissingDB(t *testing.T) {
	ws := &WebhookService{}
	svc := NewTelegramDMOutreachService(ws)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不应 panic: %v", r)
		}
	}()
	svc.TriggerDMOutreach(context.Background(), "1", 9999996, "-100", "群", 80, true, "hi")
}

var _ = gorm.ErrRecordNotFound

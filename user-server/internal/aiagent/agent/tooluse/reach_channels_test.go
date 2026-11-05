package tooluse

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
)

type mockReachAdapter struct {
	sendTelegramCount   int32
	sendWhatsAppCount   int32
	sendFeishuCount     int32
	lastTelegramAccount string
	lastTelegramChatID  string
	lastTelegramContent string
	lastWhatsAppAccount string
	lastWhatsAppPhone   string
	lastWhatsAppContent string
	lastFeishuAccount   string
	lastFeishuOpenID    string
	lastFeishuContent   string
	lastFeishuMsgType   string
	telegramErr         error
	whatsappErr         error
	feishuErr           error
}

func (m *mockReachAdapter) SendTelegram(_ context.Context, accountID, chatID, content string) (string, error) {
	atomic.AddInt32(&m.sendTelegramCount, 1)
	m.lastTelegramAccount = accountID
	m.lastTelegramChatID = chatID
	m.lastTelegramContent = content
	if m.telegramErr != nil {
		return "", m.telegramErr
	}
	return "tg-msg-" + accountID, nil
}

func (m *mockReachAdapter) SendWhatsApp(_ context.Context, accountID, toPhone, content string) (string, error) {
	atomic.AddInt32(&m.sendWhatsAppCount, 1)
	m.lastWhatsAppAccount = accountID
	m.lastWhatsAppPhone = toPhone
	m.lastWhatsAppContent = content
	if m.whatsappErr != nil {
		return "", m.whatsappErr
	}
	return "wa-msg-" + accountID, nil
}

func (m *mockReachAdapter) SendFeishu(_ context.Context, accountID, openID, content string) (string, error) {
	atomic.AddInt32(&m.sendFeishuCount, 1)
	m.lastFeishuAccount = accountID
	m.lastFeishuOpenID = openID
	m.lastFeishuContent = content
	m.lastFeishuMsgType = "text"
	if m.feishuErr != nil {
		return "", m.feishuErr
	}
	return "feishu-msg-" + accountID, nil
}
func (m *mockReachAdapter) SendWeb(_ context.Context, sessionID, content string) (string, error) {
	return "web-msg-" + sessionID, nil
}

func (m *mockReachAdapter) SendSMS(_ context.Context, _, _, _ string, _ map[string]string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendEmail(_ context.Context, _, _, _ string, _ []string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendWeCom(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendWeixin(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendDouyin(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendKuaishou(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendXHS(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendTikTok(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendXianyu(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendJD(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendBilibili(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendWeibo(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendPersonalWX(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendDingTalk(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) SendCard(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockReachAdapter) Recall(_ context.Context, _, _ string) error { return nil }
func (m *mockReachAdapter) AccountHealth(_ context.Context, _, _ string) (*AccountHealthInfo, error) {
	return &AccountHealthInfo{}, nil
}
func (m *mockReachAdapter) ListAccounts(_ context.Context, _ string) ([]AccountInfo, error) {
	return nil, nil
}

func TestReachTelegramSendTool_Name(t *testing.T) {
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	if tool.Name() != "reach.telegram.send" {
		t.Errorf("expected name 'reach.telegram.send', got %q", tool.Name())
	}
	if tool.Category() != CategoryReach {
		t.Errorf("expected category CategoryReach, got %q", tool.Category())
	}
}

func TestReachTelegramSendTool_Success(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})

	result, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123456789",
		"content":    "hello tg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
	if atomic.LoadInt32(&mock.sendTelegramCount) != 1 {
		t.Errorf("expected 1 call, got %d", mock.sendTelegramCount)
	}
	if mock.lastTelegramAccount != "1" {
		t.Errorf("expected account '1', got %q", mock.lastTelegramAccount)
	}
	if mock.lastTelegramChatID != "123456789" {
		t.Errorf("expected chat_id '123456789', got %q", mock.lastTelegramChatID)
	}
	if mock.lastTelegramContent != "hello tg" {
		t.Errorf("expected content 'hello tg', got %q", mock.lastTelegramContent)
	}
}

func TestReachTelegramSendTool_GroupChatNegativeID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})

	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "-1001234567890",
		"content":    "群组消息",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastTelegramChatID != "-1001234567890" {
		t.Errorf("expected negative chat_id, got %q", mock.lastTelegramChatID)
	}
}

func TestReachTelegramSendTool_MissingAccountID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"chat_id": "123",
		"content": "x",
	})
	if err == nil {
		t.Error("expected error for missing account_id")
	}
	if atomic.LoadInt32(&mock.sendTelegramCount) != 0 {
		t.Error("adapter should not be called when params invalid")
	}
}

func TestReachTelegramSendTool_MissingChatID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error for missing chat_id")
	}
}

func TestReachTelegramSendTool_MissingContent(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123",
	})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestReachTelegramSendTool_EmptyContent(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123",
		"content":    "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestReachTelegramSendTool_AdapterError(t *testing.T) {
	mock := &mockReachAdapter{telegramErr: errors.New("rate limited")}
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error from adapter")
	}
}

func TestReachTelegramSendTool_NoOpBehavior(t *testing.T) {
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123",
		"content":    "x",
	})
	if err == nil || !errors.Is(err, ErrAdapterNotConfigured) {
		t.Errorf("expected ErrAdapterNotConfigured, got %v", err)
	}
}

func TestReachTelegramSendTool_ToLLMFunction(t *testing.T) {
	tool := NewReachTelegramSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	fn := ToLLMFunction(tool)
	if fn.Name != "reach.telegram.send" {
		t.Errorf("expected name 'reach.telegram.send', got %q", fn.Name)
	}
	if !strings.Contains(fn.Description, "Telegram") {
		t.Errorf("expected description to mention Telegram, got %q", fn.Description)
	}
	requiredFound := false
	for _, p := range fn.Parameters.Required {
		if p == "account_id" || p == "chat_id" || p == "content" {
			requiredFound = true
		}
	}
	if !requiredFound {
		t.Error("expected account_id/chat_id/content in required")
	}
}

func TestReachWhatsAppSendTool_Name(t *testing.T) {
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	if tool.Name() != "reach.whatsapp.send" {
		t.Errorf("expected name 'reach.whatsapp.send', got %q", tool.Name())
	}
}

func TestReachWhatsAppSendTool_Success(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})

	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "10",
		"to_phone":   "+8613800138000",
		"content":    "hello wa",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendWhatsAppCount) != 1 {
		t.Errorf("expected 1 call, got %d", mock.sendWhatsAppCount)
	}
	if mock.lastWhatsAppPhone != "+8613800138000" {
		t.Errorf("expected phone '+8613800138000', got %q", mock.lastWhatsAppPhone)
	}
}

func TestReachWhatsAppSendTool_WithTemplateID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})

	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id":  "10",
		"to_phone":    "+8613800138000",
		"content":     "template content",
		"template_id": "marketing_promo_v1",
		"params":      map[string]any{"name": "Alice", "code": "1234"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendWhatsAppCount) != 1 {
		t.Errorf("expected 1 call, got %d", mock.sendWhatsAppCount)
	}
}

func TestReachWhatsAppSendTool_MissingPhone(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "10",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error for missing to_phone")
	}
}

func TestReachWhatsAppSendTool_BothContentAndTemplateEmpty(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "10",
		"to_phone":   "+861",
	})
	if err == nil {
		t.Error("expected error when both content and template_id are empty")
	}
}

func TestReachWhatsAppSendTool_AdapterError(t *testing.T) {
	mock := &mockReachAdapter{whatsappErr: errors.New("template not approved")}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "10",
		"to_phone":   "+861",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error from adapter")
	}
}

func TestReachWhatsAppSendTool_E164Format(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: mock})
	for _, phone := range []string{"+8613800138000", "+14155552671", "+447911123456"} {
		_, err := tool.Execute(context.Background(), map[string]any{
			"account_id": "1",
			"to_phone":   phone,
			"content":    "test",
		})
		if err != nil {
			t.Errorf("phone %q failed: %v", phone, err)
		}
	}
}

func TestReachWhatsAppSendTool_ToLLMFunction(t *testing.T) {
	tool := NewReachWhatsAppSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	fn := ToLLMFunction(tool)
	if fn.Name != "reach.whatsapp.send" {
		t.Errorf("expected name 'reach.whatsapp.send', got %q", fn.Name)
	}
	if !strings.Contains(fn.Description, "WhatsApp") {
		t.Errorf("expected description to mention WhatsApp, got %q", fn.Description)
	}
}

func TestReachFeishuSendTool_Name(t *testing.T) {
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	if tool.Name() != "reach.feishu.send" {
		t.Errorf("expected name 'reach.feishu.send', got %q", tool.Name())
	}
}

func TestReachFeishuSendTool_Success(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})

	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"open_id":    "ou_abc123",
		"content":    "hello feishu",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendFeishuCount) != 1 {
		t.Errorf("expected 1 call, got %d", mock.sendFeishuCount)
	}
	if mock.lastFeishuOpenID != "ou_abc123" {
		t.Errorf("expected open_id 'ou_abc123', got %q", mock.lastFeishuOpenID)
	}
}

func TestReachFeishuSendTool_WithMsgType(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"open_id":    "oc_chat123",
		"content":    "hello",
		"msg_type":   "post",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReachFeishuSendTool_MissingOpenID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error for missing open_id")
	}
}

func TestReachFeishuSendTool_EmptyContent(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"open_id":    "ou_x",
		"content":    "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestReachFeishuSendTool_AdapterError(t *testing.T) {
	mock := &mockReachAdapter{feishuErr: errors.New("token expired")}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"open_id":    "ou_x",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error from adapter")
	}
}

func TestReachFeishuSendTool_OpenIDAndChatID(t *testing.T) {
	mock := &mockReachAdapter{}
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: mock})
	for _, id := range []string{"ou_user_1", "oc_chat_1", "user@email.com"} {
		_, err := tool.Execute(context.Background(), map[string]any{
			"account_id": "5",
			"open_id":    id,
			"content":    "test",
		})
		if err != nil {
			t.Errorf("id %q failed: %v", id, err)
		}
	}
}

func TestReachFeishuSendTool_ToLLMFunction(t *testing.T) {
	tool := NewReachFeishuSendTool(ReachToolDeps{Adapter: NoOpReachAdapter{}})
	fn := ToLLMFunction(tool)
	if fn.Name != "reach.feishu.send" {
		t.Errorf("expected name 'reach.feishu.send', got %q", fn.Name)
	}
	if !strings.Contains(fn.Description, "飞书") && !strings.Contains(fn.Description, "Feishu") && !strings.Contains(fn.Description, "feishu") {
		t.Errorf("expected description to mention feishu, got %q", fn.Description)
	}
}

type dispatchBridge struct{ adapter ReachAdapter }

func (b *dispatchBridge) dispatch(ctx context.Context, req *ReachSendRequest) (string, error) {
	return dispatchToAdapter(ctx, b.adapter, req)
}

func TestReachChannelAdapterBridge_DispatchesTelegram(t *testing.T) {
	mock := &mockReachAdapter{}
	bridge := &dispatchBridge{adapter: mock}
	_, err := bridge.dispatch(context.Background(), &ReachSendRequest{
		Channel:     "telegram",
		AccountID:   "1",
		RecipientID: "123",
		Content:     "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendTelegramCount) != 1 {
		t.Error("expected SendTelegram to be called")
	}
}

func TestReachChannelAdapterBridge_DispatchesWhatsApp(t *testing.T) {
	mock := &mockReachAdapter{}
	bridge := &dispatchBridge{adapter: mock}
	_, err := bridge.dispatch(context.Background(), &ReachSendRequest{
		Channel:     "whatsapp",
		AccountID:   "1",
		RecipientID: "+861",
		Content:     "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendWhatsAppCount) != 1 {
		t.Error("expected SendWhatsApp to be called")
	}
}

func TestReachChannelAdapterBridge_DispatchesFeishu(t *testing.T) {
	mock := &mockReachAdapter{}
	bridge := &dispatchBridge{adapter: mock}
	_, err := bridge.dispatch(context.Background(), &ReachSendRequest{
		Channel:     "feishu",
		AccountID:   "1",
		RecipientID: "ou_x",
		Content:     "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&mock.sendFeishuCount) != 1 {
		t.Error("expected SendFeishu to be called")
	}
}

func TestReachChannelAdapterBridge_UnknownChannel(t *testing.T) {
	mock := &mockReachAdapter{}
	bridge := &dispatchBridge{adapter: mock}
	_, err := bridge.dispatch(context.Background(), &ReachSendRequest{
		Channel:     "unknown_channel",
		AccountID:   "1",
		RecipientID: "x",
		Content:     "x",
	})
	if err == nil {
		t.Error("expected error for unknown channel")
	}
}

func TestReachNewChannelsTools_RegistrationWithDB(t *testing.T) {
	db := testutil.NewTestDB(t)
	registry := NewToolRegistry()

	reachDeps := ReachToolDeps{Adapter: &mockReachAdapter{}, DB: db}

	if err := RegisterReachTools(registry, reachDeps); err != nil {
		t.Fatalf("register reach tools failed: %v", err)
	}

	tools := registry.List()
	foundTelegram := false
	foundWhatsApp := false
	foundFeishu := false
	for _, tool := range tools {
		switch tool.Name() {
		case "reach.telegram.send":
			foundTelegram = true
		case "reach.whatsapp.send":
			foundWhatsApp = true
		case "reach.feishu.send":
			foundFeishu = true
		}
	}
	if !foundTelegram || !foundWhatsApp || !foundFeishu {
		t.Errorf("missing tools: telegram=%v whatsapp=%v feishu=%v", foundTelegram, foundWhatsApp, foundFeishu)
	}
}

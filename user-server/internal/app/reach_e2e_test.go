package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

type e2eMockSender struct {
	count int32
	err   error
}

func (m *e2eMockSender) SendMessage(_ context.Context, _ uint, _ int64, _ string) error {
	atomic.AddInt32(&m.count, 1)
	return m.err
}

type e2eMockReachAdapter struct {
	*e2eMockSender
	telegramCalls int32
	whatsappCalls int32
	feishuCalls   int32
	telegramErr   error
	whatsappErr   error
	feishuErr     error
}

func (m *e2eMockReachAdapter) SendTelegram(ctx context.Context, accountID, chatID, content string) (string, error) {
	atomic.AddInt32(&m.telegramCalls, 1)
	if m.telegramErr != nil {
		return "", m.telegramErr
	}
	return "e2e-tg-msg", nil
}
func (m *e2eMockReachAdapter) SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (string, error) {
	atomic.AddInt32(&m.whatsappCalls, 1)
	if m.whatsappErr != nil {
		return "", m.whatsappErr
	}
	return "e2e-wa-msg", nil
}
func (m *e2eMockReachAdapter) SendFeishu(ctx context.Context, accountID, openID, content string) (string, error) {
	atomic.AddInt32(&m.feishuCalls, 1)
	if m.feishuErr != nil {
		return "", m.feishuErr
	}
	return "e2e-feishu-msg", nil
}
func (m *e2eMockReachAdapter) SendWeb(_ context.Context, sessionID, content string) (string, error) {
	return "e2e-web-msg", nil
}

func (m *e2eMockReachAdapter) SendSMS(_ context.Context, _, _, _ string, _ map[string]string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendEmail(_ context.Context, _, _, _ string, _ []string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendWeCom(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendWeixin(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendDouyin(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendKuaishou(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendXHS(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendTikTok(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendXianyu(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendDingTalk(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendCard(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) Recall(_ context.Context, _, _ string) error { return nil }
func (m *e2eMockReachAdapter) AccountHealth(_ context.Context, _, _ string) (*tooluse.AccountHealthInfo, error) {
	return &tooluse.AccountHealthInfo{}, nil
}
func (m *e2eMockReachAdapter) ListAccounts(_ context.Context, _ string) ([]tooluse.AccountInfo, error) {
	return nil, nil
}

func TestE2E_ReachTelegram_FullPipeline(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}

	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachTelegramSendTool(deps)
	args := map[string]any{
		"account_id": "1",
		"chat_id":    "987654321",
		"content":    "E2E 测试消息",
	}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}

	if atomic.LoadInt32(&mock.telegramCalls) != 1 {
		t.Errorf("expected 1 telegram call, got %d", mock.telegramCalls)
	}

	data, _ := json.Marshal(result.Data)
	dataStr := string(data)
	for _, key := range []string{"message_id", "channel", "sent_at", "step_results", "retry_count", "fallback_used"} {
		if !strings.Contains(dataStr, key) {
			t.Errorf("result.Data missing key %q: %s", key, dataStr)
		}
	}
	if !strings.Contains(dataStr, "telegram") {
		t.Errorf("result.Data missing channel=telegram: %s", dataStr)
	}
}

func TestE2E_ReachTelegram_ErrorPropagation(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{telegramErr: context.DeadlineExceeded}
	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachTelegramSendTool(deps)
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123",
		"content":    "x",
	})
	if err == nil {
		t.Error("expected error from tool")
	}
}

func TestE2E_ReachWhatsApp_FullPipeline(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}

	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachWhatsAppSendTool(deps)
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id":  "10",
		"to_phone":    "+8613800138000",
		"content":     "E2E WhatsApp",
		"template_id": "marketing_promo",
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
	if atomic.LoadInt32(&mock.whatsappCalls) != 1 {
		t.Errorf("expected 1 whatsapp call, got %d", mock.whatsappCalls)
	}
}

func TestE2E_ReachWhatsApp_TemplateWithoutContent(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}

	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachWhatsAppSendTool(deps)
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id":  "10",
		"to_phone":    "+8613800138000",
		"template_id": "utility_v1",
		"content":     "x",
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
}

func TestE2E_ReachFeishu_FullPipeline(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}

	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachFeishuSendTool(deps)
	_, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "5",
		"open_id":    "ou_xyz",
		"content":    "E2E 飞书",
		"msg_type":   "text",
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
	if atomic.LoadInt32(&mock.feishuCalls) != 1 {
		t.Errorf("expected 1 feishu call, got %d", mock.feishuCalls)
	}
}

func TestE2E_ReachFeishu_AllMsgTypes(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}
	deps := NewReachToolDepsWithAdapter(db, mock)

	tool := tooluse.NewReachFeishuSendTool(deps)
	for _, mt := range []string{"text", "post", "image", "interactive"} {
		_, err := tool.Execute(context.Background(), map[string]any{
			"account_id": "5",
			"open_id":    "ou_xyz",
			"content":    "test " + mt,
			"msg_type":   mt,
		})
		if err != nil {
			t.Errorf("msg_type=%s failed: %v", mt, err)
		}
	}
	if atomic.LoadInt32(&mock.feishuCalls) != 4 {
		t.Errorf("expected 4 feishu calls, got %d", mock.feishuCalls)
	}
}

func TestE2E_ReachNewChannels_LLMFunctionSerialization(t *testing.T) {
	db := testutil.NewTestDB(t)
	registry := tooluse.NewToolRegistry()
	mock := &e2eMockReachAdapter{}
	deps := NewReachToolDepsWithAdapter(db, mock)
	if err := tooluse.RegisterReachTools(registry, deps); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	tools := registry.List()
	found := map[string]bool{
		"reach.telegram.send": false,
		"reach.whatsapp.send": false,
		"reach.feishu.send":   false,
	}
	for _, tool := range tools {
		if _, ok := found[tool.Name()]; ok {
			found[tool.Name()] = true
		}
	}
	for name, ok := range found {
		if !ok {
			t.Errorf("tool %s not registered", name)
		}
	}

	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{})
	fns := executor.ListAvailableLLMFunctions()
	data, err := json.Marshal(fns)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	jsonStr := string(data)
	for _, name := range []string{"reach.telegram.send", "reach.whatsapp.send", "reach.feishu.send"} {
		if !strings.Contains(jsonStr, name) {
			t.Errorf("LLM Function JSON missing %s", name)
		}
	}
}

func TestE2E_ReachFullLoop_SalesReply(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}
	deps := NewReachToolDepsWithAdapter(db, mock)

	replyContent := "您好！感谢咨询我们的产品。请问您对哪个产品感兴趣？"

	tool := tooluse.NewReachTelegramSendTool(deps)
	result, err := tool.Execute(context.Background(), map[string]any{
		"account_id": "1",
		"chat_id":    "123456789",
		"content":    replyContent,
	})
	if err != nil {
		t.Fatalf("智能体回复失败：%v", err)
	}

	if atomic.LoadInt32(&mock.telegramCalls) != 1 {
		t.Error("智能体回复应触发 1 次 telegram 调用")
	}

	data, _ := json.Marshal(result.Data)
	if !strings.Contains(string(data), "e2e-tg-msg") {
		t.Errorf("返回的 message_id 不正确：%s", string(data))
	}
}

func TestE2E_ReachNewChannels_BatchDispatch(t *testing.T) {
	db := testutil.NewTestDB(t)
	mock := &e2eMockReachAdapter{}
	deps := NewReachToolDepsWithAdapter(db, mock)

	tools := []tooluse.Tool{
		tooluse.NewReachTelegramSendTool(deps),
		tooluse.NewReachWhatsAppSendTool(deps),
		tooluse.NewReachFeishuSendTool(deps),
	}
	calls := []map[string]any{
		{"account_id": "1", "chat_id": "123", "content": "TG"},
		{"account_id": "10", "to_phone": "+861", "content": "WA"},
		{"account_id": "5", "open_id": "ou_x", "content": "FS"},
	}

	for i, tool := range tools {
		_, err := tool.Execute(context.Background(), calls[i])
		if err != nil {
			t.Errorf("工具 %d 执行失败：%v", i, err)
		}
	}

	if atomic.LoadInt32(&mock.telegramCalls) != 1 {
		t.Error("telegram 未被调用")
	}
	if atomic.LoadInt32(&mock.whatsappCalls) != 1 {
		t.Error("whatsapp 未被调用")
	}
	if atomic.LoadInt32(&mock.feishuCalls) != 1 {
		t.Error("feishu 未被调用")
	}
}

func TestE2E_ReachPipeline_ChannelDispatch(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	bridge := &reachChannelAdapterBridge{adapter: mock}

	for _, ch := range []string{"telegram", "whatsapp", "feishu"} {
		_, err := bridge.Send(context.Background(), &service.ReachSendRequest{
			Channel:     ch,
			AccountID:   "1",
			RecipientID: "x",
			Content:     "x",
		})
		if err != nil {
			t.Errorf("channel %s dispatch failed: %v", ch, err)
		}
	}
	if atomic.LoadInt32(&mock.telegramCalls) != 1 {
		t.Error("bridge should dispatch telegram")
	}
	if atomic.LoadInt32(&mock.whatsappCalls) != 1 {
		t.Error("bridge should dispatch whatsapp")
	}
	if atomic.LoadInt32(&mock.feishuCalls) != 1 {
		t.Error("bridge should dispatch feishu")
	}
}

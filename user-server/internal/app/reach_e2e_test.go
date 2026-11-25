package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
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
func (m *e2eMockReachAdapter) SendJD(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendBilibili(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendWeibo(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *e2eMockReachAdapter) SendPersonalWX(_ context.Context, _, _, _, _ string) (string, error) {
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

const (
	e2eReachCustomer = "e2e-reach-customer"
	e2eReachBrand    = "e2e-brand"
)

func newE2EReachDeps(t *testing.T, mock *e2eMockReachAdapter, channels ...string) (tooluse.ReachToolDeps, context.Context) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ContactConsentEvent{}, &model.ContactPreference{},
		&model.ContactConsentIdentityLink{}, &model.CustomerDoNotContact{}, &model.ConfigParam{})
	// 本组验证同意与发送接线，通过现有参数关闭静默窗口，避免依赖运行时刻。
	if err := database.Create(&model.ConfigParam{Group: "reach", Key: "quiet_hours_disabled",
		Name: "E2E quiet window", ValueType: "bool", Value: "true", DefaultValue: "false"}).Error; err != nil {
		t.Fatal(err)
	}
	prior := service.GlobalConfigParam()
	service.SetGlobalForTest(service.NewConfigParamService())
	t.Cleanup(func() { service.SetGlobalForTest(prior) })
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	consent := service.NewContactConsentService(repository.NewContactConsentRepository(database))
	for _, channel := range channels {
		if err := consent.Record(ctx, service.ConsentRecordInput{
			OneID: e2eReachCustomer, Purpose: model.ConsentPurposeMarketing, ChannelScope: channel,
			Decision: model.ConsentDecisionGrant, Source: model.ConsentSourceImport, Brand: e2eReachBrand,
			LegalBasis: "consent", StatementVersion: "v1", EvidenceRef: "e2e-signed-form", SourceRef: "e2e-official-export",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return NewReachToolDepsWithAdapter(database, mock), ctx
}

func TestE2E_ReachTelegram_FullPipeline(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "telegram")

	tool := tooluse.NewReachTelegramSendTool(deps)
	args := map[string]any{
		"account_id":  "1",
		"chat_id":     "987654321",
		"content":     "E2E 测试消息",
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
	}
	result, err := tool.Execute(ctx, args)
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
	mock := &e2eMockReachAdapter{telegramErr: context.DeadlineExceeded}
	deps, ctx := newE2EReachDeps(t, mock, "telegram")

	tool := tooluse.NewReachTelegramSendTool(deps)
	_, err := tool.Execute(ctx, map[string]any{
		"account_id":  "1",
		"chat_id":     "123",
		"content":     "x",
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
	})
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) || atomic.LoadInt32(&mock.telegramCalls) == 0 {
		t.Fatalf("适配器错误必须透传，不能被前置拒绝掩盖：calls=%d err=%v", mock.telegramCalls, err)
	}
}

func TestE2E_ReachWhatsApp_FullPipeline(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "whatsapp")

	tool := tooluse.NewReachWhatsAppSendTool(deps)
	_, err := tool.Execute(ctx, map[string]any{
		"account_id":  "10",
		"to_phone":    "+8613800138000",
		"content":     "E2E WhatsApp",
		"template_id": "marketing_promo",
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
	if atomic.LoadInt32(&mock.whatsappCalls) != 1 {
		t.Errorf("expected 1 whatsapp call, got %d", mock.whatsappCalls)
	}
}

func TestE2E_ReachWhatsApp_TemplateWithoutContent(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "whatsapp")

	tool := tooluse.NewReachWhatsAppSendTool(deps)
	_, err := tool.Execute(ctx, map[string]any{
		"account_id":  "10",
		"to_phone":    "+8613800138000",
		"template_id": "utility_v1",
		"content":     "x",
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
}

func TestE2E_ReachFeishu_FullPipeline(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "feishu")

	tool := tooluse.NewReachFeishuSendTool(deps)
	_, err := tool.Execute(ctx, map[string]any{
		"account_id":  "5",
		"open_id":     "ou_xyz",
		"content":     "E2E 飞书",
		"msg_type":    "text",
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}
	if atomic.LoadInt32(&mock.feishuCalls) != 1 {
		t.Errorf("expected 1 feishu call, got %d", mock.feishuCalls)
	}
}

func TestE2E_ReachFeishu_AllMsgTypes(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "feishu")

	tool := tooluse.NewReachFeishuSendTool(deps)
	for _, mt := range []string{"text", "post", "image", "interactive"} {
		_, err := tool.Execute(ctx, map[string]any{
			"account_id":  "5",
			"open_id":     "ou_xyz",
			"content":     "test " + mt,
			"msg_type":    mt,
			"customer_id": e2eReachCustomer,
			"brand":       e2eReachBrand,
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
			if param, ok := tool.Parameters().Properties["brand"]; !ok || param.Type != "string" {
				t.Errorf("tool %s 必须声明品牌范围参数", tool.Name())
			}
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

func TestE2E_ReachNewChannels_ScopedConsentDeniesBeforeAdapter(t *testing.T) {
	for _, channel := range []string{"telegram", "whatsapp", "feishu"} {
		t.Run(channel, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				brand   string
				revoked bool
				wantErr error
			}{
				{name: "缺少品牌", wantErr: service.ErrSendConsentRequired},
				{name: "品牌不匹配", brand: "other-brand", wantErr: service.ErrSendConsentRequired},
				{name: "已撤回同意", brand: e2eReachBrand, revoked: true, wantErr: service.ErrSendConsentRevoked},
			} {
				t.Run(tc.name, func(t *testing.T) {
					mock := &e2eMockReachAdapter{}
					deps, ctx := newE2EReachDeps(t, mock, channel)
					if tc.revoked {
						consent := service.NewContactConsentService(repository.NewContactConsentRepository(deps.DB))
						if err := consent.Record(ctx, service.ConsentRecordInput{
							OneID: e2eReachCustomer, Purpose: model.ConsentPurposeMarketing, ChannelScope: channel,
							Decision: model.ConsentDecisionRevoke, Source: model.ConsentSourceImport, Brand: e2eReachBrand,
						}); err != nil {
							t.Fatal(err)
						}
					}
					var tool tooluse.Tool
					switch channel {
					case "telegram":
						tool = tooluse.NewReachTelegramSendTool(deps)
					case "whatsapp":
						tool = tooluse.NewReachWhatsAppSendTool(deps)
					case "feishu":
						tool = tooluse.NewReachFeishuSendTool(deps)
					}
					_, err := tool.Execute(ctx, map[string]any{
						"account_id": "1", "chat_id": "123", "to_phone": "+8613800138000", "open_id": "ou_test",
						"content": "test", "customer_id": e2eReachCustomer, "brand": tc.brand,
					})
					if err == nil || !strings.Contains(err.Error(), tc.wantErr.Error()) {
						t.Fatalf("期望同意拒绝 %v，实际 %v", tc.wantErr, err)
					}
					if atomic.LoadInt32(&mock.telegramCalls)+atomic.LoadInt32(&mock.whatsappCalls)+atomic.LoadInt32(&mock.feishuCalls) != 0 {
						t.Fatal("同意未通过时不得调用发送适配器")
					}
				})
			}
		})
	}
}

func TestE2E_ReachFullLoop_SalesReply(t *testing.T) {
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "telegram")

	replyContent := "您好！感谢咨询我们的产品。请问您对哪个产品感兴趣？"

	tool := tooluse.NewReachTelegramSendTool(deps)
	result, err := tool.Execute(ctx, map[string]any{
		"account_id":  "1",
		"chat_id":     "123456789",
		"content":     replyContent,
		"customer_id": e2eReachCustomer,
		"brand":       e2eReachBrand,
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
	mock := &e2eMockReachAdapter{}
	deps, ctx := newE2EReachDeps(t, mock, "telegram", "whatsapp", "feishu")

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
		calls[i]["customer_id"] = e2eReachCustomer
		calls[i]["brand"] = e2eReachBrand
		_, err := tool.Execute(ctx, calls[i])
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

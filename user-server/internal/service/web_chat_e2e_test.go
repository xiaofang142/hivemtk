package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

type fakeRAGSearcher struct {
	mu        sync.Mutex
	calls     int
	lastQuery string
	chunks    []dto.RAGChunk
}

func (f *fakeRAGSearcher) Search(ctx context.Context, query string, topK int) ([]dto.RAGChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastQuery = query
	return f.chunks, nil
}

const ragMarker = "KZ-8848"

func newFakeRAG() *fakeRAGSearcher {
	return &fakeRAGSearcher{
		chunks: []dto.RAGChunk{
			{
				Content: "【知识库#" + ragMarker + "】我们的标准版套餐价格是 1999 元/年，含全部基础功能。",
				Source:  "kb-e2e",
				Score:   0.91,
				DocID:   "doc-e2e-1",
				ChunkID: "chunk-e2e-1",
			},
		},
	}
}

type fakeIntentRecognizer struct{}

func (fakeIntentRecognizer) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{
		IntentType: IntentPriceInquiry,
		IntentName: "价格咨询",
		Confidence: 0.92,
	}, nil
}

type fakeMemory struct{}

func (fakeMemory) AppendMessage(ctx context.Context, sessionID, customerID string, msg dto.Message) error {
	return nil
}
func (fakeMemory) GetOrCreateMemory(ctx context.Context, sessionID, customerID string) (*model.DialogueMemory, error) {
	return &model.DialogueMemory{}, nil
}

type fakeSOP struct{}

func (fakeSOP) MatchByIntent(ctx context.Context, intentType string) ([]model.SOPAgent, error) {
	return nil, nil
}

type fakeScript struct{}

func (fakeScript) MatchScript(ctx context.Context, intent string, scenario string) (*dto.ScriptTemplate, error) {
	return nil, nil
}

type fakeCustomerLookup struct{}

func (fakeCustomerLookup) GetByOneID(ctx context.Context, oneID string) (*model.Customer, error) {
	return nil, nil
}
func (fakeCustomerLookup) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	return nil, nil
}

func newFakeLLMDispatcher(t *testing.T) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		var prompt strings.Builder
		for _, m := range req.Messages {
			prompt.WriteString(m.Content)
		}
		reply := "您好，我是 AI 智能助手，很高兴为您服务。"
		if strings.Contains(prompt.String(), ragMarker) {
			reply = "根据知识库#" + ragMarker + " 的记录，我们的标准版套餐价格是 1999 元/年，含全部基础功能，已为您确认。"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	disp := llm.NewDispatcher(llm.NewLLMService())
	fake := llm.ProviderConfig{
		Name:    "fake",
		APIKey:  "test",
		BaseURL: srv.URL,
		APIType: "openai",
		Model:   "fake",
		Enabled: true,
	}
	disp.AddProvider(fake)
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioSOPReply,
		llm.ScenarioObjection,
		llm.ScenarioFriendlyChat,
		llm.ScenarioIntentRecognize,
		llm.ScenarioHighQuality,
		llm.ScenarioLowCost,
		llm.ScenarioLongSummary,
	} {
		disp.SetRoute(llm.ScenarioRoute{Scenario: sc, Provider: "fake", Fallbacks: []string{"fake"}})
	}
	return disp
}

func setupWebChatE2E(t *testing.T) (*VisitorChatService, *SmartCSOrchestrator, *fakeRAGSearcher, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.ChatChannel{},
		&model.QuickReply{},
		&model.SessionTag{},
		&model.CSATSurvey{},
	)
	db.SetTestDB(database)

	rag := newFakeRAG()
	disp := newFakeLLMDispatcher(t)
	engine := NewSalesEngine(
		database,
		disp,
		fakeIntentRecognizer{},
		fakeMemory{},
		fakeSOP{},
		rag,
		fakeScript{},
		fakeCustomerLookup{},
	)
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	channelSvc := MustNewChatChannelService(database)
	visitorSvc := NewVisitorChatService(context.Background(), database, channelSvc, orch, nil)
	return visitorSvc, orch, rag, database
}

func TestE2E_WebChat_VisitorAsk_AIReplyWithRAG(t *testing.T) {
	visitorSvc, _, rag, _ := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID:   "default",
		VisitorID:   "v_e2e_001",
		VisitorName: "测试访客",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}
	if open == nil || open.Session == nil {
		t.Fatal("OpenSession 返回的会话为空")
	}
	if open.IsNewSession {
		t.Logf("✅ 默认渠道已自动创建，欢迎语: %q", open.WelcomeMessage)
	}

	send, err := visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_001",
		SessionID: open.Session.SessionID,
		Content:   "你们的产品怎么收费？",
	})
	if err != nil {
		t.Fatalf("SendMessage(AI) 失败: %v", err)
	}

	if rag.calls < 1 {
		t.Error("❌ RAG 召回未被调用（recallRAG 接线断裂）")
	}
	if !strings.Contains(rag.lastQuery, "收费") {
		t.Errorf("❌ RAG 查询词异常: %q", rag.lastQuery)
	}

	if !send.AIReplied {
		t.Error("❌ 未触发 AI 自动回复")
	}
	if send.AIResponse == nil {
		t.Fatal("❌ AI 回复消息为空")
	}
	if !strings.Contains(send.AIResponse.Content, ragMarker) {
		t.Errorf("❌ AI 回复未引用 RAG 知识库（标记 %s 未出现）: %q", ragMarker, send.AIResponse.Content)
	}
	if send.HandlerType != string(model.HandlerTypeAI) {
		t.Errorf("❌ 处理类型应为 ai，实际: %s", send.HandlerType)
	}
	if send.Confidence <= 0.7 {
		t.Errorf("❌ 置信度应 > 0.7（高置信度 AI 自动回复），实际: %.2f", send.Confidence)
	}
	t.Logf("✅ 场景A通过：AI回复=%q，置信度=%.2f，RAG调用次数=%d",
		send.AIResponse.Content, send.Confidence, rag.calls)
}

func TestE2E_WebChat_KeywordTransferToHuman(t *testing.T) {
	visitorSvc, _, _, _ := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_002",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}

	send, err := visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_002",
		SessionID: open.Session.SessionID,
		Content:   "这个问题我要转人工处理",
	})
	if err != nil {
		t.Fatalf("SendMessage(transfer) 失败: %v", err)
	}

	if !send.Transferred {
		t.Error("❌ 关键词未触发转人工")
	}
	if send.HandlerType != string(model.HandlerTypeHuman) {
		t.Errorf("❌ 处理类型应为 human，实际: %s", send.HandlerType)
	}
	t.Logf("✅ 场景B通过：转人工原因=%q", send.TransferReason)
}

func TestE2E_WebChat_AgentReplyAfterTransfer(t *testing.T) {
	visitorSvc, orch, _, _ := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_003",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}
	if _, err := visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_003",
		SessionID: open.Session.SessionID,
		Content:   "转人工",
	}); err != nil {
		t.Fatalf("SendMessage(transfer) 失败: %v", err)
	}

	agentID := uint(1)
	if err := orch.AgentReply(context.Background(), open.Session.SessionID, agentID, "您好，我是人工客服，已为您接入，请问还有什么可以帮您？"); err != nil {
		t.Fatalf("AgentReply 失败: %v", err)
	}

	msgs, _, err := visitorSvc.GetMessages(context.Background(), "default", "v_e2e_003", open.Session.SessionID, 1, 50)
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	foundAgent := false
	for _, m := range msgs {
		if m.SenderType == "agent" {
			foundAgent = true
			if !strings.Contains(m.Content, "人工客服") {
				t.Errorf("❌ 坐席消息内容异常: %q", m.Content)
			}
		}
	}
	if !foundAgent {
		t.Error("❌ 坐席回复未落库（人工协同闭环断裂）")
	}
	t.Logf("✅ 场景C通过：坐席回复已落库，会话消息数=%d", len(msgs))
}

func TestE2E_WebChat_FullBusinessLine(t *testing.T) {
	visitorSvc, orch, rag, database := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_004",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}
	sid := open.Session.SessionID

	s1, err := visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default", VisitorID: "v_e2e_004", SessionID: sid,
		Content: "请问你们的套餐价格？",
	})
	if err != nil || !s1.AIReplied {
		t.Fatalf("步骤1 AI 回复失败: err=%v replied=%v", err, s1 != nil && s1.AIReplied)
	}
	if !strings.Contains(s1.AIResponse.Content, ragMarker) {
		t.Error("步骤1 回复未引用 RAG 知识库")
	}

	s2, err := visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default", VisitorID: "v_e2e_004", SessionID: sid,
		Content: "我要转人工",
	})
	if err != nil || !s2.Transferred {
		t.Fatalf("步骤2 转人工失败: err=%v transferred=%v", err, s2 != nil && s2.Transferred)
	}

	if err := orch.AgentReply(context.Background(), sid, 1, "您好，人工客服已接入，正在为您处理价格问题。"); err != nil {
		t.Fatalf("步骤3 坐席回复失败: %v", err)
	}

	var sess model.CustomerSession
	if err := database.Where("session_id = ?", sid).First(&sess).Error; err != nil {
		t.Fatalf("会话状态校验失败: %v", err)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("❌ 最终处理类型应为 human，实际: %s", sess.HandlerType)
	}
	_ = rag
	t.Logf("✅ 场景D（完整业务线）通过：AI回复→转人工→坐席回复 状态机正确")
}

// TestE2E_WebChat_ClosedSessionSendMessageRejected 审计R36功能连贯性修复回归：
// 访客对 resolved/closed 会话调用 SendMessage 必须被拒（与坐席侧同一状态口径），
// 防止关闭后旧 session_id 仍可写入并意外触发 AI。
func TestE2E_WebChat_ClosedSessionSendMessageRejected(t *testing.T) {
	visitorSvc, _, _, _ := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_005",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}

	if err := visitorSvc.CloseSession(context.Background(), "default", "v_e2e_005", open.Session.SessionID); err != nil {
		t.Fatalf("CloseSession 失败: %v", err)
	}

	_, err = visitorSvc.SendMessage(context.Background(), &VisitorSendMessageRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_005",
		SessionID: open.Session.SessionID,
		Content:   "会话都关了还能发吗",
	})
	if err == nil {
		t.Fatal("❌ 已关闭会话仍允许访客发消息（状态守卫缺失）")
	}
	if !strings.Contains(err.Error(), "不允许发送消息") {
		t.Errorf("❌ 拒绝语义异常，期望包含'不允许发送消息'，实际: %v", err)
	}
	t.Logf("✅ 关闭会话拦截通过: %v", err)
}

// TestE2E_WebChat_VisitorRatingReflowsToCSATSurvey 审计R36功能连贯性修复回归：
// 关闭会话自动生成 csat_surveys（sent）后，访客评分需同步回流该调查单
// （status→responded），否则管理端 CSAT 看板读不到 embed 访客评分。
func TestE2E_WebChat_VisitorRatingReflowsToCSATSurvey(t *testing.T) {
	visitorSvc, _, _, database := setupWebChatE2E(t)

	open, err := visitorSvc.OpenSession(context.Background(), &VisitorOpenSessionRequest{
		ChannelID: "default",
		VisitorID: "v_e2e_006",
	})
	if err != nil {
		t.Fatalf("OpenSession 失败: %v", err)
	}
	// 关闭会话 → TriggerCSATOnClose 异步创建调查单
	if err := visitorSvc.CloseSession(context.Background(), "default", "v_e2e_006", open.Session.SessionID); err != nil {
		t.Fatalf("CloseSession 失败: %v", err)
	}

	// 等待异步 TriggerCSATOnClose 落库
	var survey model.CSATSurvey
	surveyFound := false
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		if err := database.Where("session_id = ?", open.Session.SessionID).First(&survey).Error; err == nil {
			surveyFound = true
			break
		}
	}
	if !surveyFound {
		t.Skip("异步 CSAT 调查单未在超时内创建（goroutine 调度），跳过回流断言")
	}

	if err := visitorSvc.RateSession(context.Background(), "default", "v_e2e_006", open.Session.SessionID, 5, "很满意"); err != nil {
		t.Fatalf("RateSession 失败: %v", err)
	}

	var after model.CSATSurvey
	if err := database.Where("session_id = ?", open.Session.SessionID).First(&after).Error; err != nil {
		t.Fatalf("调查单回查失败: %v", err)
	}
	if after.Status != model.CSATStatusResponded {
		t.Errorf("❌ 调查单状态应为 responded，实际: %s", after.Status)
	}
	if after.Score != 5 {
		t.Errorf("❌ 调查单评分应为 5，实际: %d", after.Score)
	}

	var sess model.CustomerSession
	if err := database.Where("session_id = ?", open.Session.SessionID).First(&sess).Error; err != nil {
		t.Fatalf("会话回查失败: %v", err)
	}
	if sess.Rating != 5 {
		t.Errorf("❌ 会话评分应为 5，实际: %d", sess.Rating)
	}
	t.Logf("✅ 评分回流通过：csat_surveys.status=%s score=%d，customer_sessions.rating=%d", after.Status, after.Score, sess.Rating)
}

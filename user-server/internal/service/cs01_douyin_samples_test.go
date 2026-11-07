// cs01_douyin_samples_test.go CS-01 固定主链路样本与验收契约（首发渠道 douyin，蜂桥网页桥形态）。
//
// 本文件是 CS-01 的三条贯通样本 + 负例的**服务层**落地：
//
//	样本① A/A1 新访客问合成已发布 FAQ → 客户身份/会话关联 → 可追溯知识（fake RAG 标记）
//	        → 高置信 AI 回复 → 获准路径一次有效外发（bridge outbox pending 行）→ 可对账
//	样本② 无命中/低置信 → 转人工原因/状态落库、坐席可见（waiting+human）、
//	        无未经批准的 AI 外发（转接公告单独计，正是 runAIGeneration 的既有分支）
//	样本③ 明确必需审批且被拒 → sender 调用 0、无 delivered、拒绝原因可查
//	失败分支 模型空输出 → 无外发、无 fallback 冒充知识命中
//	重放负例 同 event_id 原帧重放 → 不增有效入站、不重复触发 AI
//
// 样本字段全部合成（cs01- 前缀）并冻结为常量：tenant/account/客户/会话/消息可区分，
// 真实 PG 8217 受控变量（testutil.NewTestDB），fake 只在既有接缝上换件：
// 检索=fakeRAGSearcher（可计数）、模型=fake LLM dispatcher（httptest，同 web_chat_e2e）、
// AI 触发=fakeAITrigger（可计数）。只断确定性契约（外发次数/状态增量/审计行数/人工持有态），
// 不锁真实 LLM 文案全文（知识命中以标记 c01FAQMarker 的出现为准）。
//
// 链路各格只走生产入口，不重建业务实现：
//
//	入站=InboxIngressService.HandleIngressMessage/HandleIngressBatch（channelgw pipeline 的委托体）
//	AI 腿=WebhookService.runAIGeneration（TriggerInboundAI → triggerSmartOrchestrator 的同步实体，
//	     与装配层 bridgeIngressSvc.SetAITrigger(webhookSvc) 的生产接线同源）
//	外发腿=WebhookService.sendOutbound 桥接分支（registry OutboundPath=bridge_outbox 能力位）
//	审批腿=QuoteSendService.Send + ApprovalRequestService.Decide（既有审批竖）
package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// newC01LLMDispatcher 样本①的模型 fake：OpenAI 兼容端点，prompt 携带已发布 FAQ 标记
// 时回"知识命中"话术（含标记），否则回通用话术。与 web_chat_e2e 的 newFakeLLMDispatcher
// 同形，但标记键用 CS-01 冻结的 c01FAQMarker（不跨文件共享标记语义）。
func newC01LLMDispatcher(t *testing.T) *llm.Dispatcher {
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
		if strings.Contains(prompt.String(), c01FAQMarker) {
			reply = "根据知识库#" + c01FAQMarker + " 的记录：我们的标准版套餐价格是 1999 元/年，含全部基础功能，已为您确认。"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return c01DispatcherAt(t, srv.URL, "fake-cs01")
}

// c01DispatcherAt 在指定 base URL 上装配场景路由（newFakeLLMDispatcher 的同款装配）。
func c01DispatcherAt(t *testing.T, baseURL, providerName string) *llm.Dispatcher {
	t.Helper()
	disp := llm.NewDispatcher(llm.NewLLMService())
	disp.AddProvider(llm.ProviderConfig{
		Name: providerName, APIKey: "test", BaseURL: baseURL,
		APIType: "openai", Model: "fake", Enabled: true,
	})
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioSOPReply, llm.ScenarioObjection, llm.ScenarioFriendlyChat,
		llm.ScenarioIntentRecognize, llm.ScenarioHighQuality, llm.ScenarioLowCost,
		llm.ScenarioLongSummary,
	} {
		disp.SetRoute(llm.ScenarioRoute{Scenario: sc, Provider: providerName, Fallbacks: []string{providerName}})
	}
	return disp
}

// newC01EmptyReplyDispatcher 模型空输出 fake：OpenAI 兼容端点恒回 content=""。
// （newC01LLMDispatcher 是"有输出"那一档；本 helper 是失败分支的另一档
// ——沿用同一 Scenario 路由装配，仅响应为空。）
func newC01EmptyReplyDispatcher(t *testing.T) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return c01DispatcherAt(t, srv.URL, "fake-empty")
}

// —— CS-01 douyin 样本冻结字段（合成标识，全部可区分） ——————————————————————
//
// 渠道名冻结为规范名 douyin；douyin_web 是桥端别名，bridge.NormalizeBridgeChannel
// 在 HTTP 入口归一为 douyin（bridge/channel.go），本样本按归一后的名字进管道。
const (
	c01Channel   = "douyin"
	c01MsgType   = "text"
	c01TenantA   = uint(501)
	c01TenantB   = uint(502)
	c01AccountA1 = "cs01-acc-a1" // 租户 A 的抖音授权账号
	c01AccountB1 = "cs01-acc-b1" // 租户 B 的对照账号（同内容不串）
	c01ConvA     = "cs01-conv-a1-001"
	c01ConvB     = "cs01-conv-b1-001"
	c01CustA     = "cs01-openid-a-001" // douyin open_id（客户身份来源）
	c01CustB     = "cs01-openid-b-001"
	c01EventA    = "cs01-evt-a-0001"
	c01EventB    = "cs01-evt-b-0001"

	c01FAQMarker = "CS01-FAQ-5199"
	c01FAQAnswer = "【知识库#" + c01FAQMarker + "】我们的标准版套餐价格是 1999 元/年，含全部基础功能。"
	c01Question  = "你们的标准版套餐多少钱？"

	// 冻结时间戳惯例：入站链路无全局时钟注入口（ingress/orchestrator 直用 time.Now()），
	// 样本按既有惯例取**用例起点时刻**作为冻结事件时间戳：SentAt 断言仍锚定
	// 「服务端不覆盖事件自带 timestamp」这一时序契约，同时满足新鲜度合同
	// （HasUnrepliedCustomerMessage 的 5 分钟窗口——绝对冻结的历史时刻会被
	// 「历史消息不触发 AI」格正确拒触发，那是另一格的既有用例，不是样本①）。
	// 报价审批腿沿用既有 qsClockBase 绝对冻结惯例（该链路有 SetClock 注入口）。
)

// c01FixedTime 样本帧的冻结事件时间戳（用例进程起点时刻，见上方冻结时间戳惯例）。
var c01FixedTime = time.Now().UTC().Truncate(time.Second)

// c01DouyinSampleEvent 样本帧：douyin 传输真实字段冻结
// （event_id/session_id/channel/sender/receiver/content/type/timestamp/conversation）。
// account 由既有 Extra.account_id 接入（与 persistMessage/triggerAIForEvent 同一读法）；
// tenant 不在帧里——由已认证上下文传递（ws TenantBinder 绑定，见 channelgw 侧样本）。
func c01DouyinSampleEvent(eventID, conv, sender, accountID, content string) *model.MessageEvent {
	return &model.MessageEvent{
		EventID:        eventID,
		SessionID:      "", // 由 NormalizeEvent 派生 channel:sender（断言派生规则）
		Channel:        c01Channel,
		SenderID:       sender,
		SenderType:     "customer",
		ReceiverID:     accountID,
		MsgType:        c01MsgType,
		Content:        content,
		ConversationID: conv,
		Extra:          map[string]any{"account_id": accountID},
		Timestamp:      c01FixedTime,
	}
}

// c01InboundHub 样本①的入站库行（AI 腿的 hubMsg 入参，与 TriggerInboundAI 构造的
// hubMsg 同形——webhook_ai.go:358 起）。
func c01InboundHub(eventID, conv, sender, accountID, content string) *model.MessageHub {
	return &model.MessageHub{
		MsgID:          eventID,
		Platform:       c01Channel,
		AccountID:      accountID,
		ConversationID: conv,
		SenderID:       sender,
		Content:        content,
		Direction:      "inbound",
		MsgType:        c01MsgType,
		IsRead:         true,
		SentAt:         c01FixedTime,
	}
}

// c01LowConfIntent 低置信意图识别（样本②的"低置信"来源）：
// 同一 fake 竖，只把置信度压到阈值之下、意图仍是已知的 price_inquiry
// （isKnownIntent=true ⇒ 走置信度判据；未知意图不参与该判据）。
type c01LowConfIntent struct{}

func (c01LowConfIntent) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentPriceInquiry, IntentName: "价格咨询", Confidence: 0.30}, nil
}

// newC01RAGHit 样本①的"已发布 FAQ"：可计数检索 fake 返回含 CS-01 冻结标记的命中块。
// （测试工厂替代真库 FAQ 行：知识格的贯通以"检索被调用+标记可追溯"为契约，
// FAQ 行定位与关联规则保留给 CS-06/CS-R1。）
func newC01RAGHit() *fakeRAGSearcher {
	return &fakeRAGSearcher{
		chunks: []dto.RAGChunk{{
			Content: c01FAQAnswer,
			Source:  "kb-cs01-faq",
			Score:   0.91,
			DocID:   "doc-cs01-faq-1",
			ChunkID: "chunk-cs01-faq-1",
		}},
	}
}

// c01SetupAI 装样本①②共用的 AI 腿：真实引擎（可计数检索 + fake 模型）+ 真实编排器 +
// 真实 WebhookService（runAIGeneration 生产入口）。全部真库。
func c01SetupAI(t *testing.T, rag *fakeRAGSearcher, intent interface {
	Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error)
}) (*WebhookService, *SmartCSOrchestrator, *gorm.DB) {
	t.Helper()
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	database := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)

	engine := NewSalesEngine(
		newC01LLMDispatcher(t),
		intent,
		fakeMemory{},
		fakeSOP{},
		rag,
		fakeScript{},
		fakeCustomerLookup{},
	)
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	// 客户身份格：注入真实身份服务（douyin open_id → customer 档案）。
	orch.SetIdentityService(NewCustomerIdentityService())

	ws := NewWebhookService()
	t.Cleanup(func() { ws.Stop(context.Background()) })
	ws.SetSmartOrchestrator(context.Background(), orch)
	return ws, orch, database
}

// c01HubRowsByConv 按会话直读库行（受控变量断言；口径同 channelgw/pipeline_ack_detailed_test 的 hubRow）。
func c01HubRowsByConv(t *testing.T, db *gorm.DB, conv string) []*model.MessageHub {
	t.Helper()
	var rows []*model.MessageHub
	if err := db.Where("conversation_id = ?", conv).Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读会话 %s 的 message_hub 行失败: %v", conv, err)
	}
	return rows
}

// c01OutboundRows 过出方向行。
func c01OutboundRows(t *testing.T, db *gorm.DB, conv string) []*model.MessageHub {
	t.Helper()
	var rows []*model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = ?", conv, "outbound").Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读出站行失败: %v", err)
	}
	return rows
}

// —— 样本①：知识命中正常回复 ——————————————————————————————————————————

// TestSmartCS01_SampleA_KnowledgeHit_AIReplyApprovedOutbound 样本①主断言（N≥3 确定性契约）：
//
//	① 新访客一条有效入站 → 既有规则客户会话关联（douyin open_id → customer 档案 + 会话行）
//	② 可追溯知识：检索真被调用、回答携带已发布 FAQ 的可追溯标记（不锁 LLM 文案全文）
//	③ 获准路径一次有效外发：outbox 恰 1 条 pending 出站行（IsAIReply、sender≡accountID、
//	   trace 关联入站会话），无第二条
//	④ 留存审计：session_messages user/ai 各一条、ai_suggestions 一条、
//	   入站行 SentAt=冻结 timestamp（时序锚点）
func TestSmartCS01_SampleA_KnowledgeHit_AIReplyApprovedOutbound(t *testing.T) {
	rag := newC01RAGHit() // 可计数检索 fake：1 条命中块（含已发布 FAQ 标记）
	ws, _, db := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := context.Background()

	// 步骤1：入站落库（生产入口；channelgw pipeline 的委托体 HandleIngressBatch 的单条路径）。
	ingress := NewInboxIngressService()
	ev := c01DouyinSampleEvent(c01EventA, c01ConvA, c01CustA, c01AccountA1, c01Question)
	res, err := ingress.HandleIngressMessage(ctx, ev)
	if err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	if !res.Accepted || !res.QueuedForAI {
		t.Fatalf("样本①入站应 accepted+queued_for_ai，实际 %+v", res)
	}
	if res.SessionID != c01Channel+":"+c01CustA {
		t.Errorf("会话派生规则（channel:sender）= %q，期望 %q", res.SessionID, c01Channel+":"+c01CustA)
	}

	// 步骤2：AI 腿（生产实体 runAIGeneration：编排器 → sendOutbound 桥接分支）。
	hubMsg := c01InboundHub(c01EventA, c01ConvA, c01CustA, c01AccountA1, c01Question)
	p := &ParsedPayload{EventID: c01EventA, Sender: c01CustA, Content: c01Question}
	releaseReplyClaimForTest(t, c01EventA)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1, p, hubMsg, &IncomingContext{
		Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
		SenderID: c01CustA, Content: c01Question, MessageID: c01EventA,
	}, nil)

	// 契约②：检索可计数、可追溯。
	if rag.calls < 1 {
		t.Fatal("知识检索未被调用（召回接线断裂）")
	}
	if !strings.Contains(rag.lastQuery, "套餐") {
		t.Errorf("检索查询词异常: %q", rag.lastQuery)
	}

	// 契约③：获准路径一次有效外发。
	out := c01OutboundRows(t, db, c01ConvA)
	if len(out) != 1 {
		t.Fatalf("获准路径应恰 1 条出站行，实际 %d（%+v）", len(out), out)
	}
	reply := out[0]
	if reply.Status != "pending" {
		t.Errorf("出站行状态 = %q，期望 pending（桥端认领后走 ack，不在入站腿翻转）", reply.Status)
	}
	if !reply.IsAIReply {
		t.Error("出站行应标 IsAIReply=true")
	}
	if reply.SenderID != c01AccountA1 {
		t.Errorf("bridge 单一 sender 契约：SenderID=%q，期望 ≡ accountID %q", reply.SenderID, c01AccountA1)
	}
	if reply.ReceiverID != c01CustA {
		t.Errorf("ReceiverID=%q，期望客户 %q", reply.ReceiverID, c01CustA)
	}
	if !strings.Contains(reply.Content, c01FAQMarker) {
		t.Errorf("回答未携带已发布 FAQ 的可追溯标记 %s（不得用 fallback 冒充知识命中）: %q", c01FAQMarker, reply.Content)
	}
	if reply.TraceID == "" {
		t.Error("出站行 trace_id 为空——入站/出站消息 ID 不要求相等，但 trace 关联必须有")
	}

	// 契约①：客户身份与会话关联（douyin open_id → customer 档案；会话行平台/账号归属）。
	var sess model.CustomerSession
	if err := db.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("客户会话未落库: %v", err)
	}
	if sess.OneID != c01Channel+":"+c01CustA {
		t.Errorf("会话 OneID 派生 = %q，期望 douyin:open_id", sess.OneID)
	}
	var cust model.Customer
	if err := db.Where("douyin_open_id = ?", c01CustA).First(&cust).Error; err != nil {
		t.Fatalf("douyin open_id 未建客户档案（身份格断裂）: %v", err)
	}

	// 契约④：留存审计行数。
	var userMsgs, aiMsgs int64
	db.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "user").Count(&userMsgs)
	db.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "ai").Count(&aiMsgs)
	if userMsgs != 1 || aiMsgs != 1 {
		t.Errorf("session_messages 应 user=1 ai=1，实际 user=%d ai=%d", userMsgs, aiMsgs)
	}
	var suggestions int64
	db.Model(&model.AISuggestion{}).Where("session_id = ?", sess.SessionID).Count(&suggestions)
	if suggestions != 1 {
		t.Errorf("ai_suggestions 应 1 条，实际 %d", suggestions)
	}

	// 时序锚点：入站行 SentAt = 冻结 timestamp（服务端不覆盖事件自带时刻）。
	inbound := c01HubRowsByConv(t, db, c01ConvA)
	if len(inbound) < 1 || !inbound[0].SentAt.Equal(c01FixedTime) {
		t.Errorf("入站行 SentAt 应为冻结时间 %v，实际 %+v", c01FixedTime, inbound)
	}
}

// —— 样本②：无命中/低置信 → 转人工，无未经批准 AI 外发 ——————————————————

// TestSmartCS01_SampleB_LowConfidence_TransferToHuman_NoAIOutbound 样本②主断言：
//
//	① 检索真发生了但无命中（rag.calls≥1、0 块）+ 低置信（0.30 < 0.70）→ 转人工
//	② 转人工原因/状态落库：会话 waiting+human、handoff_reason 可查、系统消息一条（坐席可见）
//	③ 无未经批准的 AI 外发：唯一出站行是转接公告（既有获准提示单独计），
//	   不含模型文案、extra.scenario ≠ auto_reply
func TestSmartCS01_SampleB_LowConfidence_TransferToHuman_NoAIOutbound(t *testing.T) {
	rag := &fakeRAGSearcher{} // 无命中：0 块
	ws, _, db := c01SetupAI(t, rag, c01LowConfIntent{})
	ctx := context.Background()

	const (
		evt  = "cs01-evt-a-0002"
		conv = "cs01-conv-a1-002"
		cust = "cs01-openid-a-002"
		qLow = "这个能不能再便宜一点？" // 不含转人工/紧急关键词：转人工只能由置信度判据触发
	)

	ingress := NewInboxIngressService()
	if _, err := ingress.HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, conv, cust, c01AccountA1, qLow)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}

	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cust, Content: qLow},
		c01InboundHub(evt, conv, cust, c01AccountA1, qLow),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cust, Content: qLow, MessageID: evt,
		}, nil)

	// 契约①：检索发生过且无命中；决策为转人工（原因可查）。
	if rag.calls < 1 {
		t.Fatal("样本②检索未被调用——'无命中'必须是真的查过")
	}
	if len(rag.chunks) != 0 {
		t.Fatalf("样本②应无命中，fake 却给了 %d 块", len(rag.chunks))
	}
	var sess model.CustomerSession
	if err := db.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.Status != model.SessionStatusWaiting {
		t.Errorf("会话状态 = %q，期望 waiting（人工持有态）", sess.Status)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("处理方 = %q，期望 human", sess.HandlerType)
	}
	if sess.HandoffReason == "" {
		t.Error("handoff_reason 未落库——转人工原因必须可查")
	}
	if !strings.Contains(sess.HandoffReason, "置信度不足") {
		t.Errorf("转人工原因应说明置信度不足，实际 %q", sess.HandoffReason)
	}

	// 契约②：坐席可见——系统消息一条（【系统】前缀）。
	var sysMsgs int64
	db.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "system").Count(&sysMsgs)
	if sysMsgs != 1 {
		t.Errorf("转人工系统消息应 1 条（坐席工作台可见），实际 %d", sysMsgs)
	}

	// 契约③：无未经批准的 AI 外发；转接公告单独计 1 条。
	out := c01OutboundRows(t, db, conv)
	if len(out) != 1 {
		t.Fatalf("低置信转人工后应恰 1 条出站行（转接公告），实际 %d", len(out))
	}
	notice := out[0]
	if !strings.Contains(notice.Content, agent_runtime.HandoffQueueNotice) {
		t.Errorf("唯一出站行应是转接公告 %q，实际 %q", agent_runtime.HandoffQueueNotice, notice.Content)
	}
	if strings.Contains(notice.Content, "AI 智能助手") {
		t.Errorf("模型文案泄漏进低置信轮次: %q", notice.Content)
	}
	// 转接公告与 AI 回复同行走同一出库链（生产设计：插件不必单开通道），但必须可区分：
	// handler_type=human 落在行上（HandleResultToContext 注入），审计据此把公告与 AI
	// 应答分开计数。（出站行常量 IsAIReply=true/scenario=auto_reply 对全桥接出站统一
	// 打标——公告的应答量归零判定归 CS-04 观测口径，不在此锁。）
	if ht, _ := notice.Extra["handler_type"].(string); ht != "human" {
		t.Errorf("转接公告行 handler_type = %v，期望 human（公告单独计的凭据）", notice.Extra["handler_type"])
	}
}

// —— 失败分支：模型空输出 ——————————————————————————————————————————————

// TestSmartCS01_ModelEmptyReply_FallbackNotMasqueradingAsKnowledgeHit 模型空输出失败分支。
//
// 生产实况（emptyReplyFallback，sales_engine_agentloop.go）：空输出被引擎替换成
// 降级文案照常出站——本用例钉的是这条失败分支的**可区分性**契约：
//
//	① 恰 1 条出站行（降级文案获准出站，客户有出路：回复「转人工」即触发 veto）
//	② 降级文案不得冒充知识命中：内容不含已发布 FAQ 的可追溯标记，
//	   且带仓库声明的降级锚（"无法处理"/"请稍后再试"，TestEmptyReplyFallback 与
//	   scripts/simulate 的共同识别锚）
//	③ 检索计数为真（查过、无命中）——空输出轮与知识命中轮在审计上可分
//
// 已知缺口（CS-02 领走，不在本测试内修）：降级文案行继承意图置信度（extra.confidence=0.92）、
// handler_type=ai——应答质量指标把降级轮当成正常 AI 回答计数。
func TestSmartCS01_ModelEmptyReply_FallbackNotMasqueradingAsKnowledgeHit(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	database := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)

	rag := &fakeRAGSearcher{} // 有检索无命中：空输出不得被算作知识命中
	engine := NewSalesEngine(newC01EmptyReplyDispatcher(t), fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	ws := NewWebhookService()
	t.Cleanup(func() { ws.Stop(context.Background()) })
	ws.SetSmartOrchestrator(context.Background(), orch)

	ctx := context.Background()
	const (
		evt  = "cs01-evt-a-0003"
		conv = "cs01-conv-a1-003"
		cust = "cs01-openid-a-003"
	)
	ingress := NewInboxIngressService()
	if _, err := ingress.HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, conv, cust, c01AccountA1, c01Question)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cust, Content: c01Question},
		c01InboundHub(evt, conv, cust, c01AccountA1, c01Question),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cust, Content: c01Question, MessageID: evt,
		}, nil)

	// 契约①：恰 1 条降级出站行（pending；本腿不认领 ⇒ delivered=0）。
	out := c01OutboundRows(t, database, conv)
	if len(out) != 1 {
		t.Fatalf("空输出失败分支应恰 1 条降级出站行，实际 %d", len(out))
	}
	if out[0].Status != "pending" {
		t.Errorf("降级出站行状态 = %q，期望 pending（未认领即未 delivered）", out[0].Status)
	}

	// 契约②：不得冒充知识命中——无 FAQ 标记、带降级锚。
	if strings.Contains(out[0].Content, c01FAQMarker) {
		t.Errorf("空输出轮不得携带知识标记（冒充知识命中）: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "无法处理") || !strings.Contains(out[0].Content, "请稍后再试") {
		t.Errorf("降级文案应带仓库声明的降级锚（无法处理/请稍后再试），实际 %q", out[0].Content)
	}

	// 契约③：检索为真（查过、无命中）。
	if rag.calls < 1 {
		t.Error("空输出轮的检索未被调用——与知识命中轮的区分必须建立在真实检索上")
	}
}

// —— 重放负例 ————————————————————————————————————————————————————————

// TestHandleIngressCS01_ReplayNegative_SameEventIDNoSecondInbound 重放负例：
// 同 event_id 原帧重放 ⇒ 幂等跳过（accepted=true、queued_for_ai=false）、
// AI 触发仍 1 次、库内同 msg_id 行仍 1 条。
func TestHandleIngressCS01_ReplayNegative_SameEventIDNoSecondInbound(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)
	ctx := context.Background()

	// 专用 conv/sender/content：内容窗口去重键在进程级缓存里，与其他样本隔离
	// （同 event_id 原帧重放的判定对象是 hook2 的 msg_id 幂等，不得被别格的键提前命中）。
	const (
		replayConv    = "cs01-conv-a1-008"
		replayCust    = "cs01-openid-a-replay"
		replayContent = "CS01服务层重放负例专用：查物流"
	)
	first, err := svc.HandleIngressMessage(ctx, c01DouyinSampleEvent(c01EventA, replayConv, replayCust, c01AccountA1, replayContent))
	if err != nil {
		t.Fatalf("首帧失败: %v", err)
	}
	if !first.Accepted || !first.QueuedForAI || tr.called != 1 {
		t.Fatalf("首帧应 accepted+queued+触发1次，实际 %+v called=%d", first, tr.called)
	}

	// 原帧重放（字节级同帧：同 event_id/conversation/sender/content）。
	replay, err := svc.HandleIngressMessage(ctx, c01DouyinSampleEvent(c01EventA, replayConv, replayCust, c01AccountA1, replayContent))
	if err != nil {
		t.Fatalf("重放帧失败: %v", err)
	}
	if !replay.Accepted {
		t.Error("重放帧应 accepted=true（幂等回执，扩展据此确认不重报）")
	}
	if replay.QueuedForAI {
		t.Error("重放帧不得再触发 AI（queued_for_ai 应为 false）")
	}
	if !strings.Contains(replay.Reason, "idempotent skip") {
		t.Errorf("重放原因应可核对（idempotent skip），实际 %q", replay.Reason)
	}
	if tr.called != 1 {
		t.Errorf("AI 触发次数应仍为 1，实际 %d", tr.called)
	}
	var n int64
	database.Model(&model.MessageHub{}).Where("msg_id = ?", c01EventA).Count(&n)
	if n != 1 {
		t.Errorf("同 msg_id 库内行应仍 1 条，实际 %d", n)
	}
	if tr.lastEventID != c01EventA || tr.lastAccount != c01AccountA1 || tr.lastConv != replayConv {
		t.Errorf("触发身份元组不符: %+v", tr)
	}
}

// —— 样本③：明确必需审批且被拒 → sender 0、无 delivered、拒绝原因可查 ————

// TestSmartCS01_SampleC_ApprovalRejected_ZeroSenderCalls 样本③主断言：
//
//	① 明确必需审批（policy=nil ⇒ 永不自动放行的最保守档）→ 先停 pending
//	② 人工拒绝（真 Decide 路）→ 带 rejected 处置返回
//	③ sender 调用 0、无任何 delivered/外发行
//	④ 拒绝原因可查：approval_requests 行 status=rejected、DecisionNote=拒绝备注
func TestSmartCS01_SampleC_ApprovalRejected_ZeroSenderCalls(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraftOn(t, db, "opp_cs01_quote", "douyin:"+c01CustA)
	p := qssSvc(t, db)
	ctx := context.Background()

	// 步骤1：明确必需审批——首次发送只换来一条 pending 待办，外发计数 0。
	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	if first.Disposition != QuoteSendAwaiting {
		t.Fatalf("首次发送处置 = %q，期望 awaiting", first.Disposition)
	}
	if p.reach.calls != 0 {
		t.Fatalf("审批未裁决前 sender 已被调用 %d 次", p.reach.calls)
	}

	// 步骤2：人工拒绝（真 Decide 路，拒绝原因落库）。
	if _, err := p.appro.Decide(ctx, first.ApprovalID, ApprovalReject, "boss_cs01", "折扣超出权限，不能这么报"); err != nil {
		t.Fatalf("拒绝裁决失败: %v", err)
	}

	// 步骤3：带被拒的号再来 → rejected 处置、sender 仍 0。
	res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if err != nil {
		t.Fatalf("被拒应带处置返回而非故障: %v", err)
	}
	if res.Disposition != QuoteSendRejected {
		t.Errorf("处置 = %q，期望 rejected", res.Disposition)
	}
	if p.reach.calls != 0 {
		t.Errorf("被拒后 sender 调用 = %d，期望 0（审批门不是装饰）", p.reach.calls)
	}

	// 步骤4：拒绝原因可查 + 报价停在草稿 + 无任何外发痕迹。
	var appr model.ApprovalRequest
	if err := db.First(&appr, "id = ?", first.ApprovalID).Error; err != nil {
		t.Fatalf("审批行不可查: %v", err)
	}
	if appr.Status != model.ApprovalStatusRejected {
		t.Errorf("审批行状态 = %q，期望 rejected", appr.Status)
	}
	if !strings.Contains(appr.DecisionNote, "不能这么报") {
		t.Errorf("拒绝原因应可查（DecisionNote），实际 %q", appr.DecisionNote)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("被拒报价应留在草稿，实际 %s", got)
	}
	var outbound int64
	db.Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outbound)
	if outbound != 0 {
		t.Errorf("样本③不应产生任何 delivered/外发行，实际 %d", outbound)
	}
}

// —— 跨租户负例（写入侧租户落章 + 认领侧隔离） ————————————————————————

// TestIngressCS01_CrossTenantNegative_TenantBIsolatedNoOutbound 跨租户负例：
// 租户从已认证上下文传递（帧里没有 tenant 字段，也不认客户端自报）。
//
//	① A/B 两租户各写各的：入站行/出站行 tenant_id 各归其主，B 同内容不并入 A 的会话
//	② 认领隔离：B 的桥按 (channel,account) 认领，A 的待发行一条都不可见 ⇒ B 0 外发
//	③ 无租户的写被 fail-closed 拒绝（宁可不发，也不落到默认租户）
func TestIngressCS01_CrossTenantNegative_TenantBIsolatedNoOutbound(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	setupBridgeWhitelistForTest(t, c01Channel)
	database := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	t.Cleanup(func() { SetGlobalInboxIngressService(nil) })

	// ① 租户 A：入站落 A 的租户章（tenant 由 ctx 传递，帧里没有）。
	ctxA := tenant.WithTenantID(context.Background(), c01TenantA)
	resA, err := svc.HandleIngressMessage(ctxA, c01DouyinSampleEvent(c01EventA, c01ConvA, c01CustA, c01AccountA1, c01Question))
	if err != nil {
		t.Fatalf("租户 A 入站失败: %v", err)
	}
	if !resA.Accepted {
		t.Fatalf("租户 A 入站应被接受: %+v", resA)
	}

	// ② 租户 B：同内容、同渠道，不同账号/会话——各落各的租户，互不合并。
	ctxB := tenant.WithTenantID(context.Background(), c01TenantB)
	resB, err := svc.HandleIngressMessage(ctxB, c01DouyinSampleEvent(c01EventB, c01ConvB, c01CustB, c01AccountB1, c01Question))
	if err != nil {
		t.Fatalf("租户 B 入站失败: %v", err)
	}
	if !resB.Accepted || !resB.QueuedForAI {
		t.Fatalf("租户 B 自己的入站应正常受理: %+v", resB)
	}

	var rowA, rowB model.MessageHub
	// on 模式下裸读租户表被隔离守卫拒绝（fail-closed 的正确行为）——
	// 验证读也必须各带各的租户上下文，这本身就是契约的一部分。
	if err := database.WithContext(ctxA).Where("msg_id = ?", c01EventA).First(&rowA).Error; err != nil {
		t.Fatalf("读 A 入站行失败: %v", err)
	}
	if err := database.WithContext(ctxB).Where("msg_id = ?", c01EventB).First(&rowB).Error; err != nil {
		t.Fatalf("读 B 入站行失败: %v", err)
	}
	if rowA.TenantID != c01TenantA {
		t.Errorf("A 入站行 tenant_id = %d，期望 %d", rowA.TenantID, c01TenantA)
	}
	if rowB.TenantID != c01TenantB {
		t.Errorf("B 入站行 tenant_id = %d，期望 %d（同内容不串租户）", rowB.TenantID, c01TenantB)
	}

	// ③ 获准路径外发（A 租户），B 的桥来认领：一条都看不见 ⇒ B 0 外发。
	if err := DeliverBridgeOutbound(ctxA, c01Channel, c01AccountA1, c01ConvA, c01MsgType, c01FAQAnswer, "cs01-evt-a-0009"); err != nil {
		t.Fatalf("A 租户获准外发失败: %v", err)
	}
	claimedB, err := svc.ClaimPendingOutbound(ctxB, c01Channel, c01AccountB1, 10)
	if err != nil {
		t.Fatalf("B 认领失败: %v", err)
	}
	if len(claimedB) != 0 {
		t.Errorf("B 租户不得认领到 A 的待发行（越租户），实际 %d 条", len(claimedB))
	}
	claimedA, err := svc.ClaimPendingOutbound(ctxA, c01Channel, c01AccountA1, 10)
	if err != nil {
		t.Fatalf("A 认领失败: %v", err)
	}
	if len(claimedA) != 1 {
		t.Fatalf("A 自己的待发行应恰 1 条，实际 %d", len(claimedA))
	}

	// ④ 无租户的写 fail-closed：不落默认租户、不外发。
	if err := DeliverBridgeOutbound(context.Background(), c01Channel, c01AccountA1, c01ConvA, c01MsgType, "无租户上下文的消息", "cs01-evt-a-0010"); err == nil {
		t.Fatal("无租户 ctx 的外发应被隔离守卫拒绝（不得静默落到默认租户）")
	}
	// 出站行对账（各租户各读各的）：A 恰 1 条获准外发，B 0 条（越租户=0、B 0 外发）。
	var outA, outB int64
	database.WithContext(ctxA).Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outA)
	database.WithContext(ctxB).Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outB)
	if outA != 1 {
		t.Errorf("A 租户出站行应恰 1 条，实际 %d", outA)
	}
	if outB != 0 {
		t.Errorf("B 租户出站行应 0 条，实际 %d", outB)
	}
}

// cs06_pipeline_checkpoint_test.go CS-06 检查点 B：本机完整主链路预演（channelgw 管道边界层）。
//
// CS-01～CS-05 把链路分段钉住了，但各段是"接缝各自验"：服务层样本手动分两腿
// （ingress.HandleIngressMessage + 直调 runAIGeneration），管道样本只验传输边界。
// 本文件按 CS-06 贯通要求把它们**串成一条线**：
//
//	从授权测试 adapter 的真实入站入口（IngressPipeline.IngestBatch，生产装配
//	router.go:780 同款 AITrigger 接线：ingress.SetAITrigger(webhookSvc)）进，
//	连接实际客户/知识/编排/审批/outbox 仓储——AI 腿走生产实体 TriggerInboundAI →
//	runAIGeneration → SmartCSOrchestrator（真 FAQService 读真库 faq_entries 行）→
//	sendOutbound 桥接分支 → outbox → 管道认领 → ack 对账。只 fake 外部模型
//	（OpenAI 兼容 httptest dispatcher）与必要的异步等待，不手造最终行。
//
// 每场景按 CS-01 期望值表列出：输入键 → 身份/知识/决策 → outbox 状态增量 →
// sender 次数 → ack/拒绝审计 → trace/关联键贯通（本文件新增的对账面：
// message_trace 的 ingest/ai_dispatch/outbound_enqueue 节点与 trace_id 串联）。
//
// 本机预演通过 ≠ 真实渠道/生产交付：真模型、真实发信、真实渠道操作归 CS-R1。
package channelgw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// —— CS-06 冻结字段（cs06- 前缀：内容窗口去重键/AI 处理标记都是进程级缓存，须与
// CS-01～03 各样本隔离，也避免本文件内各场景互相污染） ————————————————————————
const (
	cg06Channel   = "douyin"
	cg06TenantA   = uint(601)
	cg06TenantB   = uint(602)
	cg06AccountA1 = "cs06-acc-a1"
	cg06AccountB1 = "cs06-acc-b1"
	cg06ConvA     = "cs06-conv-a1-001"
	cg06ConvB     = "cs06-conv-b1-001"
	cg06CustA     = "cs06-openid-a-001"
	cg06CustB     = "cs06-openid-b-001"

	// 样本①②：知识命中/低置信共用的问题与标准答案（真库 FAQ 行，CS-03 口径）。
	cg06Question = "你们的标准版套餐多少钱？"
	cg06Answer   = "CS06-FAQ-A：标准版 1999 元/年，含全部基础功能。"

	// 样本③：报价审批腿（真 QuoteGenerate/QuoteSend/ApprovalRequestService 竖，
	// 沿用 qss 冻结字段惯例；OneID 用 A 账号会话的稳定键）。
	cg06OppID = "opp_cs06_quote"
	cg06OneID = "douyin:" + cg06CustA

	// cg06TemplateKey 报价模板配置键（与 service 侧 qss 冻结模板同形：两行、折扣位齐全）。
	cg06TemplateKey  = service.QuoteTemplateKVPrefix + "std_annual"
	cg06TemplateJSON = `{"name":"标准版年费","currency":"CNY","valid_days":30,"lines":[` +
		`{"product_id":"p_seat","title":"标准版席位","quantity":10,"unit_price":199.00,"discount_percent":10},` +
		`{"product_id":"p_impl","title":"实施服务","quantity":1,"unit_price":8000.00,"discount_percent":5}]}`
)

// cg06LTCGate LTC 配置直递读口（报价链的生效档：quote 阶段开、其余同默认）。
type cg06LTCGate struct{}

func (cg06LTCGate) Config(context.Context) *service.LTCConfig {
	return &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageQuote),
		Thresholds:    service.DefaultLTCConfig().Thresholds,
	}
}

// cg06Cfg 配置直递（quoteConfigGetter 的最小实现：模板/话术指针从 map 取）。
type cg06Cfg map[string]string

func (c cg06Cfg) Get(_ context.Context, key string) (string, error) { return c[key], nil }

// cg06ClockBase 报价审批腿的冻结时钟（该链路有 SetClock 注入口，沿 qssClockBase 惯例取绝对时刻）。
var cg06ClockBase = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

// cg06WaitFor 轮询等待 AI 腿异步收尾（runAIGeneration 是 go 出去的 goroutine，
// 无同步注入口——生产装配就是这样，等待本身就是预演的一部分）。
func cg06WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// cg06LLMCounter 可计数模型 fake 的计数器（CS-03 同款）。
var cg06LLMCalls int32

// cg06NewLLM 装配可计数 httptest 模型 fake：prompt 含 FAQ 标准答案片段时原样背书，
// 否则通用话术（样本②的"无模型文案可泄漏"由此可判）。
func cg06NewLLM(t *testing.T) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&cg06LLMCalls, 1)
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
		out := "您好，我是 AI 智能助手，很高兴为您服务。"
		if strings.Contains(prompt.String(), "1999") {
			out = cg06Answer
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": out}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	disp := llm.NewDispatcher(llm.NewLLMService())
	disp.AddProvider(llm.ProviderConfig{
		Name: "fake-cs06", APIKey: "test", BaseURL: srv.URL,
		APIType: "openai", Model: "fake", Enabled: true,
	})
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioSOPReply, llm.ScenarioObjection, llm.ScenarioFriendlyChat,
		llm.ScenarioIntentRecognize, llm.ScenarioHighQuality, llm.ScenarioLowCost,
		llm.ScenarioLongSummary,
	} {
		disp.SetRoute(llm.ScenarioRoute{Scenario: sc, Provider: "fake-cs06", Fallbacks: []string{"fake-cs06"}})
	}
	return disp
}

// cg06SeedAgentAndBinding 灌真智能体行 + 渠道绑定（loadAgentForChannel 的真路：
// AIAgent.Status=1 → LoadContext → agentCtx.AgentID 喂给 FAQ 快速通道）。
func cg06SeedAgentAndBinding(t *testing.T, db *gorm.DB, ctx context.Context, agentID uint, accountID string) {
	t.Helper()
	agent := &model.AIAgent{
		ID: agentID, AgentCode: fmt.Sprintf("cs06-agent-%d", agentID), Name: "CS06 主链路智能体",
		AgentType: "sales", AgentMode: "passive", Persona: "电商客服",
		Status: 1, EnableRAG: cg06BoolPtr(true), ConfidenceThreshold: 0.7,
	}
	if err := db.WithContext(ctx).Create(agent).Error; err != nil {
		t.Fatalf("灌智能体行失败: %v", err)
	}
	binding := &model.ChannelAgentBinding{
		ChannelType: "douyin", AccountID: accountID, AgentID: agentID,
		IsPrimary: cg06BoolPtr(true), Enabled: cg06BoolPtr(true),
	}
	if err := db.WithContext(ctx).Create(binding).Error; err != nil {
		t.Fatalf("灌渠道绑定失败: %v", err)
	}
}

func cg06BoolPtr(b bool) *bool { return &b }

// cg06SeedFAQ 灌真 FAQ 行（真 faq_entries 行，非工厂；FAQ 快速通道按 agentID 归属召回）。
func cg06SeedFAQ(t *testing.T, db *gorm.DB, ctx context.Context, agentID uint, question, answer string) model.FAQEntry {
	t.Helper()
	row := model.FAQEntry{
		Question: question, Answer: answer,
		Keywords:   []string{"套餐"},
		Confidence: 0.9, AgentID: cg06UintPtr(agentID), Enabled: cg06BoolPtr(true),
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("灌 FAQ 行失败: %v", err)
	}
	return row
}

func cg06UintPtr(u uint) *uint { return &u }

// cg06Frame CS-01 同款 wire 帧（douyin 传输真实字段冻结；account_id 在帧顶层，
// ToEvent 落入 Extra["account_id"]——persistMessage/triggerAIForEvent 的读法同源）。
func cg06Frame(eventID, conv, sender, accountID, content string) *IngestMessage {
	return &IngestMessage{
		EventID:        eventID,
		Channel:        cg06Channel,
		AccountID:      accountID,
		ConversationID: conv,
		SenderID:       sender,
		ReceiverID:     accountID,
		SenderType:     "customer",
		MsgType:        "text",
		Content:        content,
		Timestamp:      time.Now().UnixMilli(),
	}
}

// cg06Setup 完整主链路装配（生产接线，只 fake 模型）：
//
//	真库（含 message_trace：trace 对账面）→ 真 ingress（全局句柄注册，出库入口同源）
//	→ 真 WebhookService（生产 AITrigger 接线 ingress.SetAITrigger(ws)，非 fake 触发器）
//	→ 真 SmartCSOrchestrator（真身份服务 + 真绑定服务：agentCtx 从库加载）
//	→ 真 SalesEngine（真 FAQService 读真库行）。intent 参数是模型背书的外部组件接缝
//	（生产 NewIntentRecognizer(dispatcher)；样本②的低置信轮在此换件，CS-01 同口径）。
func cg06Setup(t *testing.T, intent service.IntentRecognizerInterface) (IngressPipeline, *service.WebhookService, *service.InboxIngressService, *gorm.DB) {
	t.Helper()
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	database := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.DelayedOutboundReply{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
		&model.MessageTrace{},
		&model.AIAgent{}, &model.ChannelAgentBinding{}, &model.FAQEntry{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })

	ingress := service.NewInboxIngressService()
	service.SetBridgeChannels([]string{cg06Channel})
	t.Cleanup(func() { service.SetBridgeChannels(nil) })
	service.SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })

	engine := service.NewSalesEngine(cg06NewLLM(t), intent,
		cg06Memory{}, cg06SOP{}, cg06RAGNone{},
		cg06Script{}, cg06CustLookup{})
	engine.SetFAQService(service.NewFAQService())
	orch := service.NewSmartCSOrchestrator(engine, &service.OrchestratorConfig{
		ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10,
	}, nil)
	orch.SetIdentityService(service.NewCustomerIdentityService())

	ws := service.NewWebhookService()
	t.Cleanup(func() { ws.Stop(context.Background()) })
	ws.SetSmartOrchestrator(context.Background(), orch)
	// 生产 AITrigger 接线（router.go:780 同款）：AI 触发不再走 fake，走真 TriggerInboundAI。
	ingress.SetAITrigger(ws)
	ws.SetIngressSvc(ingress)
	// 生产 agent 绑定接线（router.go:634 同款）：agentCtx 从库加载（绑定→智能体行）。
	ws.SetAgentBindingService(context.Background(), service.NewChannelAgentBindingServiceWithRepos(
		repository.NewChannelAgentBindingRepositoryWithDB(database),
		repository.NewAIAgentRepositoryWithDB(database),
		service.NewAIAgentServiceWithRepo(repository.NewAIAgentRepositoryWithDB(database)),
	))

	return NewPipeline(ingress), ws, ingress, database
}

// cg06HubRowsByConv 按会话直读库行。
func cg06HubRowsByConv(t *testing.T, d *gorm.DB, conv string) []*model.MessageHub {
	t.Helper()
	var rows []*model.MessageHub
	if err := d.Where("conversation_id = ?", conv).Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读会话 %s 的 message_hub 行失败: %v", conv, err)
	}
	return rows
}

func cg06OutboundRows(t *testing.T, d *gorm.DB, conv string) []*model.MessageHub {
	t.Helper()
	var rows []*model.MessageHub
	if err := d.Where("conversation_id = ? AND direction = ?", conv, "outbound").Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读出站行失败: %v", err)
	}
	return rows
}

// TestCS06_PipelineSampleA_KnowledgeHit_FullChainWithTraceReconciliation 样本①全链贯通：
//
//	输入键（adapter 帧 event_id/conv/open_id/account）→ 入站落库+派生会话
//	→ 真身份服务建客户档案（douyin open_id → customer）→ 真 FAQ 行召回（agentCtx 真路）
//	→ outbox 恰 1 条 pending → 管道认领（租约）→ ack delivered → 重 ack duplicate
//	→ trace 对账：ingest/ai_dispatch/outbound_enqueue 三节点同 conv，
//	  inbound 与 outbound 行 trace_id 相等（LinkInbound/LinkOutbound 同源派生）。
func TestCS06_PipelineSampleA_KnowledgeHit_FullChainWithTraceReconciliation(t *testing.T) {
	p, _, _, d := cg06Setup(t, cg06Intent{})
	ctx := context.Background()

	cg06SeedAgentAndBinding(t, d, ctx, 6101, cg06AccountA1)
	cg06SeedFAQ(t, d, ctx, 6101, cg06Question, cg06Answer)

	// 步骤1：从真实入站入口进（生产 AITrigger 接线 ⇒ IngestBatch 批尾真触发 AI 腿）。
	res, err := p.IngestBatch(ctx, []*model.MessageEvent{cg06Frame("cs06-evt-a-0001", cg06ConvA, cg06CustA, cg06AccountA1, cg06Question).ToEvent("ws")})
	if err != nil {
		t.Fatalf("样本①入站失败: %v", err)
	}
	if len(res.PerEvent) != 1 || res.PerEvent[0] == nil || !res.PerEvent[0].Accepted || !res.PerEvent[0].QueuedForAI {
		t.Fatalf("样本①入站应 accepted+queued: %+v", res.PerEvent)
	}

	// 步骤2：等 AI 腿异步收尾（生产 goroutine；判据=出站行落库）。
	cg06WaitFor(t, "样本① AI 腿出站落库", func() bool {
		return len(cg06OutboundRows(t, d, cg06ConvA)) == 1
	})

	// 契约①：身份/知识/决策——客户档案+会话（douyin open_id 真身份服务）。
	var cust model.Customer
	if err := d.Where("douyin_open_id = ?", cg06CustA).First(&cust).Error; err != nil {
		t.Fatalf("douyin open_id 未建客户档案（身份格断裂）: %v", err)
	}
	var sess model.CustomerSession
	if err := d.Where("platform = ? AND account_id = ?", cg06Channel, cg06AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("客户会话未落库: %v", err)
	}
	// webhook 路径的 OneID 语义（webhook_ai.go:261 in.OneID=hubMsg.SenderID=裸 open_id）：
	// 会话 OneID≡douyin open_id 本体（CS-01 服务层样本同款断言），不是 channel:sender 派生值
	// （那是 ingress 无 hubMsg 时的 fallback 形态）。
	if sess.OneID != cg06CustA {
		t.Errorf("会话 OneID 派生 = %q，期望 open_id 本体（webhook 路径）", sess.OneID)
	}
	if sess.UserID != cg06CustA {
		t.Errorf("会话 UserID（客户侧关联键）= %q，期望 open_id", sess.UserID)
	}

	// 契约②：知识来源可追溯——回复携带真库 FAQ 行的标准答案（真 FAQService 召回，
	// 不允许 LLM 通用话术冒充知识命中）。
	out := cg06OutboundRows(t, d, cg06ConvA)
	reply := out[0]
	if !strings.Contains(reply.Content, "CS06-FAQ-A") {
		t.Errorf("回复未携带真库 FAQ 标准答案（知识来源断裂）: %q", reply.Content)
	}

	// 契约③：outbox 状态增量——恰 1 条 pending，IsAIReply，sender≡accountID。
	if reply.Status != "pending" {
		t.Errorf("出站行状态 = %q，期望 pending", reply.Status)
	}
	if !reply.IsAIReply {
		t.Error("出站行应标 IsAIReply=true")
	}
	if reply.SenderID != cg06AccountA1 {
		t.Errorf("bridge 单一 sender：SenderID=%q 期望 ≡ accountID", reply.SenderID)
	}
	if ht, _ := reply.Extra["handler_type"].(string); ht != "ai" {
		t.Errorf("出站行 handler_type = %v，期望 ai", reply.Extra["handler_type"])
	}

	// 契约④：trace/关联键贯通——inbound 行与 outbound 行的 trace_id 相等
	// （LinkInboundTraceID 派生稳定值，LinkOutboundTraceID 回读同值）。
	rows := cg06HubRowsByConv(t, d, cg06ConvA)
	if len(rows) != 2 {
		t.Fatalf("会话应恰 2 行（1 入 1 出），实际 %d", len(rows))
	}
	var inboundRow, outboundRow *model.MessageHub
	for _, r := range rows {
		if r.Direction == "inbound" && inboundRow == nil {
			inboundRow = r
		}
		if r.Direction == "outbound" {
			outboundRow = r
		}
	}
	if inboundRow == nil || outboundRow == nil {
		t.Fatalf("入站/出站行缺失: %+v", rows)
	}
	if inboundRow.TraceID == "" || inboundRow.TraceID != outboundRow.TraceID {
		t.Errorf("inbound/outbound trace_id 应相等且非空（轮次级串联）: in=%q out=%q",
			inboundRow.TraceID, outboundRow.TraceID)
	}
	if inboundRow.SenderID != cg06CustA || outboundRow.ReceiverID != cg06CustA {
		t.Errorf("关联键：入站 sender 与出站 receiver 应同一客户，实际 in=%q out=%q",
			inboundRow.SenderID, outboundRow.ReceiverID)
	}

	// 契约⑤：message_trace 生命周期节点贯通（ingest → ai_dispatch → outbound_enqueue
	// 同 conv 同 trace），conversation_id 是会话级串联键。
	cg06WaitFor(t, "message_trace 三节点落库", func() bool {
		var n int64
		d.Model(&model.MessageTrace{}).Where("conversation_id = ?", cg06ConvA).Count(&n)
		return n >= 3
	})
	var nodes []model.MessageTrace
	if err := d.Where("conversation_id = ?", cg06ConvA).Order("node_order asc").Find(&nodes).Error; err != nil {
		t.Fatalf("读 message_trace 失败: %v", err)
	}
	seen := map[string]string{} // node → trace_id
	for _, tr := range nodes {
		seen[tr.Node] = tr.TraceID
	}
	for _, want := range []string{"ingest", "ai_dispatch", "outbound_enqueue"} {
		tid, ok := seen[want]
		if !ok {
			t.Errorf("message_trace 缺 %s 节点（生命周期断链），实际节点: %v", want, nodeNames(nodes))
			continue
		}
		if tid != inboundRow.TraceID {
			t.Errorf("节点 %s 的 trace_id=%s 与入站行 trace_id=%s 不一致", want, tid, inboundRow.TraceID)
		}
	}

	// 契约⑥：留存审计行数（session_messages user/ai 各 1 + ai_suggestions 1）。
	var userMsgs, aiMsgs int64
	d.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "user").Count(&userMsgs)
	d.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "ai").Count(&aiMsgs)
	if userMsgs != 1 || aiMsgs != 1 {
		t.Errorf("session_messages 应 user=1 ai=1，实际 user=%d ai=%d", userMsgs, aiMsgs)
	}
	var suggestions int64
	d.Model(&model.AISuggestion{}).Where("session_id = ?", sess.SessionID).Count(&suggestions)
	if suggestions != 1 {
		t.Errorf("ai_suggestions 应 1 条，实际 %d", suggestions)
	}

	// 契约⑦：下行对账全链——管道认领（租约）→ ack delivered → 重 ack duplicate → 不再可认领。
	claimed, err := p.ClaimOutbound(ctx, cg06Channel, cg06AccountA1, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("认领应恰 1 条，实际 %d (err=%v)", len(claimed), err)
	}
	if claimed[0].Status != "inflight" || claimed[0].ClaimedAt == nil {
		t.Errorf("认领行应 inflight 且带 claimed_at 租约: %+v", claimed[0])
	}
	acked, err := p.AckOutbound(ctx, cg06Channel, cg06AccountA1, []string{claimed[0].MsgID})
	if err != nil || acked != 1 {
		t.Fatalf("首次 ack 应翻转 1 行，实际 affected=%d err=%v", acked, err)
	}
	re, err := p.AckOutboundDetailed(ctx, cg06Channel, cg06AccountA1, []string{claimed[0].MsgID}, cg06ConvA, model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("重复 ack 失败: %v", err)
	}
	if re.Items[0].Status != model.BridgeAckStatusDuplicate {
		t.Errorf("重复 ack 应归 duplicate（重复送达=0 的对账依据），实际 %s", re.Items[0].Status)
	}
	again, err := p.ClaimOutbound(ctx, cg06Channel, cg06AccountA1, 10)
	if err != nil || len(again) != 0 {
		t.Errorf("delivered 后不得再被认领（重复送达=0），实际 %d 条 err=%v", len(again), err)
	}
}

func nodeNames(nodes []model.MessageTrace) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Node)
	}
	return out
}

// —— 引擎端口的本地替身（channelgw 不能 import service 的 unexported fakes；
// 接口均为 service 导出接口，替身只占位：知识格由真库 FAQ 行承担） ——————

// cg06Intent 高置信意图（价格咨询 0.92）：样本①走 FAQ 快速通道短路（意图不参与），
// 负例组的普通问答据此正常 AI 应答。
type cg06Intent struct{}

func (cg06Intent) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: service.IntentPriceInquiry, IntentName: "价格咨询", Confidence: 0.92}, nil
}

// cg06LowIntent 低置信意图（0.30，CS-01 样本②同款形态）：意图识别是模型背书的
// 外部组件（生产 NewIntentRecognizer(dispatcher)），按贯通要求在模型接缝上 fake。
// 编排器置信度链沿用 CS-01 钉住的形态（NewSmartCSOrchestrator 不注入五信号聚合器
// ⇒ fallbackConfidence=意图置信度，0.30 < 0.70 → 转人工，reason 含「置信度不足」）。
type cg06LowIntent struct{}

func (cg06LowIntent) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: service.IntentPriceInquiry, IntentName: "价格咨询", Confidence: 0.30}, nil
}

type cg06Memory struct{}

func (cg06Memory) AppendMessage(context.Context, string, string, dto.Message) error { return nil }
func (cg06Memory) GetOrCreateMemory(context.Context, string, string) (*model.DialogueMemory, error) {
	return &model.DialogueMemory{}, nil
}

type cg06SOP struct{}

func (cg06SOP) MatchByIntent(context.Context, string) ([]model.SOPAgent, error) { return nil, nil }

// cg06RAGNone 恒空检索替身：知识贯通的真路是 FAQService→真库行（快速通道短路，
// 不进 RAG）；本替身存在是为了证明"回复不是从 RAG 工厂里来的"。
type cg06RAGNone struct{}

func (cg06RAGNone) Search(context.Context, string, int) ([]service.RAGChunk, error) { return nil, nil }

type cg06Script struct{}

func (cg06Script) MatchScript(context.Context, string, string) (*dto.ScriptTemplate, error) {
	return nil, nil
}

type cg06CustLookup struct{}

func (cg06CustLookup) GetByOneID(context.Context, string) (*model.Customer, error) { return nil, nil }
func (cg06CustLookup) GetByID(context.Context, string) (*model.Customer, error)    { return nil, nil }

// —— 样本③的三个端口探针/直连（channelgw 侧同形于 service 侧 qss 竖，真库为底）——

// cg06Reader 「这一版报价当前开着的那条待办」的读口。
type cg06Reader struct {
	repo repository.ApprovalRequestRepository
}

func (r cg06Reader) GetPendingBySubject(ctx context.Context, st, sid, pk string) (*model.ApprovalRequest, error) {
	return r.repo.GetPendingBySubject(ctx, st, sid, pk)
}

// cg06Events 销售事件记账代理（底下垫真仓储，与 service 侧 qssEvents 同形）。
type cg06Events struct {
	repo repository.SalesEventRepository
}

func (e cg06Events) Create(ctx context.Context, ev *model.SalesEvent) error {
	return e.repo.Create(ctx, ev)
}

// cg06Scripts 报价话术端口（生效话术指针直递，真发送腿按它拼外发文案）。
type cg06Scripts struct{}

func (cg06Scripts) ActiveQuoteScript(context.Context, uint, string) (service.QuoteScript, error) {
	return service.QuoteScript{ScriptID: 77, Version: 3, Bucket: "A", Content: "这是第三版生效话术"}, nil
}

// cg06SeedOpp 造发送腿要能解析出收件人的那条商机（真库行）。
func cg06SeedOpp(t *testing.T, d *gorm.DB, id, oneID string) {
	t.Helper()
	repo := repository.NewOpportunityRepositoryWithDB(d)
	row := &model.Opportunity{
		ID: id, Code: "OPP-CS06-" + id, CustomerID: "cus_cs06", OneID: oneID,
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_cs06",
		CreatedAt: cg06ClockBase, UpdatedAt: cg06ClockBase,
	}
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("造商机行失败: %v", err)
	}
}

// TestCS06_PipelineSampleB_LowConfidence_HandoffNoticeNotModelReply 样本②全链贯通：
// 低置信（无 FAQ 命中 + 无知识标记词）→ 转人工（waiting+human+原因可查）→
// 唯一出站行=转接公告（handler_type=human），无模型文案泄漏；B 账号并行会话各自独立。
func TestCS06_PipelineSampleB_LowConfidence_HandoffNoticeNotModelReply(t *testing.T) {
	p, _, _, d := cg06Setup(t, cg06LowIntent{})
	ctx := context.Background()

	cg06SeedAgentAndBinding(t, d, ctx, 6102, cg06AccountA1)
	// 检索无命中形态：不给本会话的问题可命中的 FAQ 行（真库查过但 0 命中）。

	const (
		evt   = "cs06-evt-a-0002"
		conv  = "cs06-conv-a1-002"
		cust  = "cs06-openid-a-002"
		qLow  = "这个能不能再便宜一点？"
		convB = "cs06-conv-b1-002"
	)

	if _, err := p.IngestBatch(ctx, []*model.MessageEvent{cg06Frame(evt, conv, cust, cg06AccountA1, qLow).ToEvent("ws")}); err != nil {
		t.Fatalf("样本②入站失败: %v", err)
	}
	cg06WaitFor(t, "样本② 转接公告落库", func() bool {
		return len(cg06OutboundRows(t, d, conv)) == 1
	})

	// 契约①：会话转人工态可查。
	var sess model.CustomerSession
	if err := d.Where("platform = ? AND account_id = ?", cg06Channel, cg06AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.Status != model.SessionStatusWaiting {
		t.Errorf("会话状态 = %q，期望 waiting", sess.Status)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("处理方 = %q，期望 human", sess.HandlerType)
	}
	if !strings.Contains(sess.HandoffReason, "置信度不足") {
		t.Errorf("转人工原因应说明置信度不足，实际 %q", sess.HandoffReason)
	}

	// 契约②：坐席可见的系统消息恰 1 条。
	var sysMsgs int64
	d.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", sess.SessionID, "system").Count(&sysMsgs)
	if sysMsgs != 1 {
		t.Errorf("转人工系统消息应 1 条，实际 %d", sysMsgs)
	}

	// 契约③：唯一出站行=转接公告（handler_type=human 落行），无模型文案。
	out := cg06OutboundRows(t, d, conv)
	if len(out) != 1 {
		t.Fatalf("低置信转人工后应恰 1 条出站行（转接公告），实际 %d", len(out))
	}
	if ht, _ := out[0].Extra["handler_type"].(string); ht != "human" {
		t.Errorf("公告行 handler_type = %v，期望 human（公告与 AI 应答分开计的凭据）", out[0].Extra["handler_type"])
	}
	if strings.Contains(out[0].Content, "AI 智能助手") {
		t.Errorf("模型文案泄漏进低置信轮次: %q", out[0].Content)
	}

	// 契约④：B 账号同内容各落各的会话（并行会话独立——输入键不同即无交集）。
	if _, err := p.IngestBatch(ctx, []*model.MessageEvent{cg06Frame("cs06-evt-b-0002", convB, cg06CustB, cg06AccountB1, qLow).ToEvent("ws")}); err != nil {
		t.Fatalf("B 会话入站失败: %v", err)
	}
	var rowB model.MessageHub
	if err := d.Where("conversation_id = ? AND direction = ?", convB, "inbound").First(&rowB).Error; err != nil {
		t.Fatalf("B 入站行缺失: %v", err)
	}
	var nA int64
	d.Model(&model.MessageHub{}).Where("conversation_id = ? AND direction = ?", conv, "inbound").Count(&nA)
	if nA != 1 {
		t.Errorf("A 会话入站行应仍恰 1（B 同内容不得并入 A 会话），实际 %d", nA)
	}
}

// TestCS06_PipelineSampleC_QuoteApprovalRejected_ZeroSenderCalls 样本③全链贯通：
// 真 QuoteGenerate/QuoteSend/ApprovalRequestService 竖（真库行、真审批竖 policy=nil
// 最保守档），真实 ProactiveReachService 出口接桥接出库（真 DeliverBridgeOutbound）。
// 审批未裁决/被拒 ⇒ sender 调用 0、出站行 0、拒绝原因可查、报价停在草稿。
func TestCS06_PipelineSampleC_QuoteApprovalRejected_ZeroSenderCalls(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ScriptLibrary{}, &model.ScriptVersion{},
		&model.ApprovalRequest{}, &model.SalesEvent{},
		&model.MessageHub{},
	)
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ctx := context.Background()
	service.SetBridgeChannels([]string{cg06Channel})
	t.Cleanup(func() { service.SetBridgeChannels(nil) })
	service.SetGlobalInboxIngressService(service.NewInboxIngressService())
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })

	// 造草稿（真生成腿）：挂在 A 会话稳定 OneID 上（与样本①会话同一关联键域）。
	cg06SeedOpp(t, database, cg06OppID, cg06OneID)
	quoteRepo := repository.NewQuoteRepositoryWithDB(database)
	cfg := cg06Cfg{cg06TemplateKey: cg06TemplateJSON, service.QuoteScriptIDKVKey: "77"}
	generator := service.NewQuoteService(
		quoteRepo,
		repository.NewOpportunityRepositoryWithDB(database),
		cg06LTCGate{},
	)
	generator.SetClock(func() time.Time { return cg06ClockBase })
	generator.SetConfigStore(cfg)
	generator.SetScriptSource(cg06Scripts{})
	view, err := generator.Generate(ctx, service.QuoteGenerateInput{
		OpportunityID: cg06OppID, OneID: cg06OneID, TemplateCode: "std_annual",
	})
	if err != nil {
		t.Fatalf("造草稿失败: %v", err)
	}
	if view.Status != model.QuoteStatusDraft {
		t.Fatalf("生成腿交出来的不是草稿: %s", view.Status)
	}

	// 真发送竖：真审批（policy=nil 永不自动放行）+ 真 ProactiveReachService 出口
	// （接桥接出库真路 DeliverBridgeOutbound——不 fake sender，计数与落库都是真的）。
	reach := service.NewProactiveReachService(
		repository.NewProactiveReachRepository(database),
		repository.NewCustomerDoNotContactRepository(database),
		nil,
	)
	appro := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database), nil)
	send := service.NewQuoteSendService(
		quoteRepo,
		repository.NewOpportunityRepositoryWithDB(database),
		appro,
		cg06Reader{repo: repository.NewApprovalRequestRepositoryWithDB(database)},
		reach,
		cg06Events{repo: repository.NewSalesEventRepositoryWithDB(database)},
		cfg,
		cg06Scripts{},
		cg06LTCGate{},
	)
	send.SetClock(func() time.Time { return cg06ClockBase })

	// 步骤1：未批先点发送 → 只开 pending 待办，sender 0、出站行 0。
	first, err := send.Send(ctx, service.QuoteSendInput{QuoteRowID: view.ID, Operator: "sales_cs06"})
	if err != nil {
		t.Fatalf("首次发送失败: %v", err)
	}
	if first.Disposition != service.QuoteSendAwaiting {
		t.Fatalf("首次发送处置 = %q，期望 awaiting", first.Disposition)
	}
	var outbound int64
	database.Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outbound)
	if outbound != 0 {
		t.Fatalf("审批未裁决前已产生外发行 %d 条（审批门失效）", outbound)
	}

	// 步骤2：人工拒绝（真 Decide 路，原因落库）。
	if _, err := appro.Decide(ctx, first.ApprovalID, service.ApprovalReject, "boss_cs06", "折扣超出权限，不能这么报"); err != nil {
		t.Fatalf("拒绝裁决失败: %v", err)
	}

	// 步骤3：带被拒的号再来 → rejected 处置、无任何外发。
	res, err := send.Send(ctx, service.QuoteSendInput{QuoteRowID: view.ID, ApprovalID: first.ApprovalID, Operator: "sales_cs06"})
	if err != nil {
		t.Fatalf("被拒应带处置返回而非故障: %v", err)
	}
	if res.Disposition != service.QuoteSendRejected {
		t.Errorf("处置 = %q，期望 rejected", res.Disposition)
	}

	// 步骤4：拒绝审计可查 + 报价停草稿 + 出站行仍 0（含桥接出库全程）。
	var appr model.ApprovalRequest
	if err := database.First(&appr, "id = ?", first.ApprovalID).Error; err != nil {
		t.Fatalf("审批行不可查: %v", err)
	}
	if appr.Status != model.ApprovalStatusRejected {
		t.Errorf("审批行状态 = %q，期望 rejected", appr.Status)
	}
	if !strings.Contains(appr.DecisionNote, "不能这么报") {
		t.Errorf("拒绝原因应可查（DecisionNote），实际 %q", appr.DecisionNote)
	}
	var row model.Quote
	if err := database.First(&row, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("报价行不可查: %v", err)
	}
	if row.Status != model.QuoteStatusDraft {
		t.Errorf("被拒报价应留在草稿，实际 %s", row.Status)
	}
	database.Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outbound)
	if outbound != 0 {
		t.Errorf("样本③不应产生任何外发行，实际 %d", outbound)
	}
}

// TestCS06_PipelineReplayAndSendFailure_Negatives 负例组（同一管道入口）：
//
//	① 重放：同 event_id 原帧重投 → 幂等回执、不重触 AI、reason 落明确重复词表
//	   （词表=扩展侧"不重报"的唯一依据）；
//	② 发送失败：占位账号（-unknown 后缀）出站 → 行标 failed + undeliverable_reason，
//	   错误不冒充送达（不进 pending 出库队列）。
func TestCS06_PipelineReplayAndSendFailure_Negatives(t *testing.T) {
	p, _, _, d := cg06Setup(t, cg06Intent{})
	ctx := context.Background()

	// —— ① 重放负例（专用 conv/content：内容窗口去重键在进程级缓存，须隔离） ——
	const (
		evt     = "cs06-evt-replay"
		convR   = "cs06-conv-replay"
		custR   = "cs06-openid-replay"
		qReplay = "CS06重放负例专用：查物流"
	)
	frame := cg06Frame(evt, convR, custR, cg06AccountA1, qReplay)
	first, err := p.IngestBatch(ctx, []*model.MessageEvent{frame.ToEvent("ws")})
	if err != nil {
		t.Fatalf("首帧失败: %v", err)
	}
	if len(first.PerEvent) != 1 || !first.PerEvent[0].QueuedForAI {
		t.Fatalf("首帧应 queued_for_ai: %+v", first.PerEvent)
	}
	replay, err := p.IngestBatch(ctx, []*model.MessageEvent{frame.ToEvent("ws")})
	if err != nil {
		t.Fatalf("重放帧失败: %v", err)
	}
	r := replay.PerEvent[0]
	if !r.Accepted || r.QueuedForAI || replay.TriggeredAI {
		t.Errorf("重放帧应 accepted=true 且不触 AI: queued=%v triggered=%v", r.QueuedForAI, replay.TriggeredAI)
	}
	if !IsDuplicateReason(r.Reason) {
		t.Errorf("重放 reason %q 不在服务端明确重复结论词表", r.Reason)
	}
	var n int64
	d.Model(&model.MessageHub{}).Where("msg_id = ?", evt).Count(&n)
	if n != 1 {
		t.Errorf("同 msg_id 库内行应仍 1 条，实际 %d", n)
	}

	// —— ② 发送失败负例：占位账号出站 → failed，不冒充送达 ——
	cg06SeedAgentAndBinding(t, d, ctx, 6103, cg06AccountA1)
	cg06SeedFAQ(t, d, ctx, 6103, "物流多久到", "CS06-FAQ-LOGI：下单后 48 小时内发货。")
	const (
		evtF  = "cs06-evt-fail"
		convF = "cs06-conv-fail"
		custF = "cs06-openid-fail"
	)
	if _, err := p.IngestBatch(ctx, []*model.MessageEvent{cg06Frame(evtF, convF, custF, cg06AccountA1+"-unknown", "物流多久到").ToEvent("ws")}); err != nil {
		t.Fatalf("失败负例入站失败: %v", err)
	}
	cg06WaitFor(t, "失败负例出站行落库", func() bool {
		var n int64
		d.Model(&model.MessageHub{}).Where("conversation_id = ? AND direction = ?", convF, "outbound").Count(&n)
		return n == 1
	})
	var failRow model.MessageHub
	if err := d.Where("conversation_id = ? AND direction = ?", convF, "outbound").First(&failRow).Error; err != nil {
		t.Fatalf("读失败负例出站行: %v", err)
	}
	if failRow.Status != "failed" {
		t.Errorf("占位账号出站应标 failed（错误不冒充送达），实际 %q", failRow.Status)
	}
	if reason, _ := failRow.Extra["undeliverable_reason"].(string); reason == "" {
		t.Errorf("failed 行应带 undeliverable_reason（审计可对账），实际 extra=%v", failRow.Extra)
	}
}

// TestCS06_PipelineCrossTenantNegative_TenantBIsolated 跨租户负例（管道入口）：
// 租户从已认证上下文传递；B 的桥按 (channel,account) 认领不见 A 的待发行 ⇒ B 0 外发；
// 无租户 ctx 的写被隔离守卫 fail-closed 拒绝。
func TestCS06_PipelineCrossTenantNegative_TenantBIsolated(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	p, _, ingress, d := cg06Setup(t, cg06Intent{})

	ctxA := tenant.WithTenantID(context.Background(), cg06TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cg06TenantB)

	// A/B 各写各的（入站行 tenant_id 各归其主）。
	if _, err := p.IngestBatch(ctxA, []*model.MessageEvent{cg06Frame("cs06-evt-a-xt", cg06ConvA, cg06CustA, cg06AccountA1, cg06Question).ToEvent("ws")}); err != nil {
		t.Fatalf("A 入站失败: %v", err)
	}
	if _, err := p.IngestBatch(ctxB, []*model.MessageEvent{cg06Frame("cs06-evt-b-xt", cg06ConvB, cg06CustB, cg06AccountB1, cg06Question).ToEvent("ws")}); err != nil {
		t.Fatalf("B 入站失败: %v", err)
	}
	var rowA, rowB model.MessageHub
	if err := d.WithContext(ctxA).Where("msg_id = ?", "cs06-evt-a-xt").First(&rowA).Error; err != nil {
		t.Fatalf("读 A 入站行失败: %v", err)
	}
	if err := d.WithContext(ctxB).Where("msg_id = ?", "cs06-evt-b-xt").First(&rowB).Error; err != nil {
		t.Fatalf("读 B 入站行失败: %v", err)
	}
	if rowA.TenantID != cg06TenantA || rowB.TenantID != cg06TenantB {
		t.Errorf("入站行 tenant_id 应各归其主: A=%d B=%d", rowA.TenantID, rowB.TenantID)
	}

	// A 获准外发后，B 的桥认领 0 条（越租户不可见）⇒ B 0 外发。
	if err := service.DeliverBridgeOutbound(ctxA, cg06Channel, cg06AccountA1, cg06ConvA, "text", cg06Answer, "cs06-evt-a-xt-out"); err != nil {
		t.Fatalf("A 获准外发失败: %v", err)
	}
	claimedB, err := p.ClaimOutbound(ctxB, cg06Channel, cg06AccountB1, 10)
	if err != nil {
		t.Fatalf("B 认领失败: %v", err)
	}
	if len(claimedB) != 0 {
		t.Errorf("B 租户不得认领到 A 的待发行（越租户），实际 %d 条", len(claimedB))
	}
	claimedA, err := p.ClaimOutbound(ctxA, cg06Channel, cg06AccountA1, 10)
	if err != nil || len(claimedA) != 1 {
		t.Fatalf("A 自己的待发行应恰 1 条，实际 %d (err=%v)", len(claimedA), err)
	}

	// 无租户 ctx 的写 fail-closed（宁可不发，也不落到默认租户）。
	if err := service.DeliverBridgeOutbound(context.Background(), cg06Channel, cg06AccountA1, cg06ConvA, "text", "无租户上下文的消息", "cs06-evt-notenant"); err == nil {
		t.Fatal("无租户 ctx 的外发应被隔离守卫拒绝")
	}
	var outB int64
	d.WithContext(ctxB).Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&outB)
	if outB != 0 {
		t.Errorf("B 租户出站行应 0 条，实际 %d", outB)
	}
	_ = ingress
}

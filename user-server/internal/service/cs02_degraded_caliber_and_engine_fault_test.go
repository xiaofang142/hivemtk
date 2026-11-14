// cs02_degraded_caliber_and_engine_fault_test.go CS-02 领走的两个 CS-01 缺口
// （douyin 样本级，真实 PG 8217 受控变量；夹具与 cs01_douyin_samples_test.go 同源）：
//
//	① 模型空输出降级轮的外发行口径（emptyReplyFallback，sales_engine_agentloop.go）：
//	   TestSmartCS01_ModelEmptyReply 已钉「不冒充知识命中」；本文件钉**口径三件**——
//	     - extra.confidence：降级轮不得继承意图置信度（0.92 描述的是一条不存在的模型
//	       答案；应答质量指标据此把它当正常 AI 回答计数＝口径缺陷）；
//	     - extra.degraded：降级行必须带 "model_empty_reply" 标记，审计/指标按标记分轮；
//	     - extra.handler_type=ai、scenario=auto_reply：既有语义不变（降级轮仍是 AI 腿
//	       的应答行，放行语义也不变——文案自带「转人工」出路，置信度判据不受影响）。
//	② 引擎层降级链（smart_cs_orchestrator.HandleIncomingWithAgent）：
//	   L1 备用 Engine.Handle → L2 RuleEngine → L3 转人工。故障注入接缝为
//	   SetSalesEngineHandleFault（引擎入口测试钩子，生产恒 nil 语义零改动）：
//	     - 恒失败 → L1 同败 → L3 转人工（会话 waiting+human、公告出站恰 1 条、
//	       检索 0 次——降级不是靠模型兜出来的）；
//	     - 首败次成 → L1 备用 Handle 兜回 → 正常 AI 回复（无 degraded 标记）。
package service

import (
	"hivemtk-user/internal/pkg/tenant"
	"context"
	"errors"
	"strings"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// cs02Conv/cust 常量：cs02- 前缀隔离（内容窗口键、AI 处理标记都在进程级缓存）。
const (
	cs02ConvEmpty   = "cs02-conv-empty-1"
	cs02CustEmpty   = "cs02-openid-empty-1"
	cs02ConvFault   = "cs02-conv-fault-l3"
	cs02CustFault   = "cs02-openid-fault-l3"
	cs02ConvFaultL1 = "cs02-conv-fault-l1"
	cs02CustFaultL1 = "cs02-openid-fault-l1"
)

// cs02SetupEmptyEngine 空输出引擎夹具：与 TestSmartCS01_ModelEmptyReply 同构
// （模型恒回 content="" 的 dispatcher——c01SetupAI 装的是知识命中 dispatcher，
// 不是这条失败分支的件）。
func cs02SetupEmptyEngine(t *testing.T, rag *fakeRAGSearcher) (*WebhookService, *gorm.DB) {
	t.Helper()
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	database := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	engine := NewSalesEngine(newC01EmptyReplyDispatcher(t), fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)
	ws := NewWebhookService()
	t.Cleanup(func() { ws.Stop(context.Background()) })
	ws.SetSmartOrchestrator(context.Background(), orch)
	return ws, database
}

// TestCS02_ModelEmptyReply_DegradedRowCaliber 降级轮外发行口径（正确行为钉死）。
func TestCS02_ModelEmptyReply_DegradedRowCaliber(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, database := cs02SetupEmptyEngine(t, rag)
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "cs02-evt-empty-1"

	if _, err := NewInboxIngressService().HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, cs02ConvEmpty, cs02CustEmpty, c01AccountA1, c01Question)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cs02CustEmpty, Content: c01Question},
		c01InboundHub(evt, cs02ConvEmpty, cs02CustEmpty, c01AccountA1, c01Question),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cs02CustEmpty, Content: c01Question, MessageID: evt,
		}, nil)

	out := c01OutboundRows(t, database, cs02ConvEmpty)
	if len(out) != 1 {
		t.Fatalf("空输出失败分支应恰 1 条降级出站行，实际 %d", len(out))
	}
	row := out[0]

	// 口径①：降级行不得继承意图置信度（fakeIntentRecognizer 的 0.92 描述的是那条
	// 不存在的模型答案；把它写到出站行上＝应答质量指标把降级轮当正常 AI 回答计数）。
	if _, has := row.Extra["confidence"]; has {
		t.Errorf("降级轮 extra.confidence = %v，必须缺席——降级行没有模型答案，不得声称任何置信度",
			row.Extra["confidence"])
	}
	// 口径②：降级标记必须在行上，指标/审计据此分轮（与内容锚正交，锚会被文案/语言
	// 层改写，标记不会）。
	if v, _ := row.Extra["degraded"].(string); v != "model_empty_reply" {
		t.Errorf("降级行 extra.degraded = %v，期望 %q（可区分的降级轮标记）",
			row.Extra["degraded"], "model_empty_reply")
	}
	// 口径③：既有语义不变——handler_type=ai、scenario=auto_reply（放行语义与行归属
	// 都不动，只修可观测口径）。
	if ht, _ := row.Extra["handler_type"].(string); ht != "ai" {
		t.Errorf("handler_type = %v，期望 ai（降级轮仍是 AI 腿的应答行）", row.Extra["handler_type"])
	}
	if sc, _ := row.Extra["scenario"].(string); sc != "auto_reply" {
		t.Errorf("scenario = %v，期望 auto_reply（既有语义不变）", row.Extra["scenario"])
	}
	// 内容锚契约不变（TestSmartCS01_ModelEmptyReply 的既有断言口径）。
	if !strings.Contains(row.Content, "无法处理") || !strings.Contains(row.Content, "请稍后再试") {
		t.Errorf("降级文案锚丢失: %q", row.Content)
	}
}

// TestCS02_Douyin_EngineFault_L3FallbackTransferToHuman 引擎恒失败 → 降级链走满：
// L1 备用 Handle 同样失败 → L2 RuleEngine（无规则表，静默不炸主链）→ L3 转人工。
func TestCS02_Douyin_EngineFault_L3FallbackTransferToHuman(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "cs02-evt-fault-l3-1"

	SetSalesEngineHandleFault(func(*SalesRequest) error { return errors.New("cs02: engine fault injected (always)") })
	t.Cleanup(func() { SetSalesEngineHandleFault(nil) })

	if _, err := NewInboxIngressService().HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, cs02ConvFault, cs02CustFault, c01AccountA1, c01Question)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cs02CustFault, Content: c01Question},
		c01InboundHub(evt, cs02ConvFault, cs02CustFault, c01AccountA1, c01Question),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cs02CustFault, Content: c01Question, MessageID: evt,
		}, nil)

	// 降级不是靠模型兜出来的：故障注入在引擎入口，检索一次都没发生。
	if rag.calls != 0 {
		t.Errorf("引擎故障下检索应 0 次（L1/L2/L3 都不该再碰模型/知识库），实际 %d", rag.calls)
	}

	// L3 转人工落库：会话 waiting+human，原因可查。
	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.Status != model.SessionStatusWaiting {
		t.Errorf("会话状态 = %q，期望 waiting（L3 兜底转人工）", sess.Status)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("处理方 = %q，期望 human", sess.HandlerType)
	}
	if !strings.Contains(sess.HandoffReason, "降级链 Level 3") {
		t.Errorf("handoff_reason 应记录降级链 L3 兜底，实际 %q", sess.HandoffReason)
	}

	// 出站恰 1 条 = 转接公告（无降级文案、无模型话术）。
	out := c01OutboundRows(t, database, cs02ConvFault)
	if len(out) != 1 {
		t.Fatalf("引擎故障轮应恰 1 条出站行（转接公告），实际 %d", len(out))
	}
	if !strings.Contains(out[0].Content, agent_runtime.HandoffQueueNotice) {
		t.Errorf("唯一出站行应是转接公告，实际 %q", out[0].Content)
	}
	if strings.Contains(out[0].Content, "无法处理") || strings.Contains(out[0].Content, "AI 智能助手") {
		t.Errorf("降级文案/模型话术泄漏进引擎故障轮: %q", out[0].Content)
	}
	if ht, _ := out[0].Extra["handler_type"].(string); ht != "human" {
		t.Errorf("公告行 handler_type = %v，期望 human", out[0].Extra["handler_type"])
	}
}

// TestCS02_Douyin_EngineFault_L1BackupHandleRecovers 首败次成 → L1 备用 Handle 兜回，
// 正常 AI 回复（无 degraded 标记——这一轮不是空输出降级）。
func TestCS02_Douyin_EngineFault_L1BackupHandleRecovers(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "cs02-evt-fault-l1-1"

	calls := 0
	SetSalesEngineHandleFault(func(*SalesRequest) error {
		calls++
		if calls == 1 {
			return errors.New("cs02: first pass fault")
		}
		return nil
	})
	t.Cleanup(func() { SetSalesEngineHandleFault(nil) })

	if _, err := NewInboxIngressService().HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, cs02ConvFaultL1, cs02CustFaultL1, c01AccountA1, c01Question)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cs02CustFaultL1, Content: c01Question},
		c01InboundHub(evt, cs02ConvFaultL1, cs02CustFaultL1, c01AccountA1, c01Question),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cs02CustFaultL1, Content: c01Question, MessageID: evt,
		}, nil)

	// L1 兜回：备用 Handle 真跑了一轮（检索发生），回复照常出站。
	if rag.calls < 1 {
		t.Fatal("L1 备用 Handle 应真执行链路（检索未发生=兜回失败）")
	}
	out := c01OutboundRows(t, database, cs02ConvFaultL1)
	if len(out) != 1 {
		t.Fatalf("L1 兜回轮应恰 1 条 AI 出站行，实际 %d", len(out))
	}
	if !out[0].IsAIReply {
		t.Error("L1 兜回的回复应标 IsAIReply=true")
	}
	if _, has := out[0].Extra["degraded"]; has {
		t.Errorf("L1 兜回的模型回复不是降级轮，不得带 degraded 标记: %v", out[0].Extra["degraded"])
	}
	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.Status == model.SessionStatusWaiting || sess.HandlerType == model.HandlerTypeHuman {
		t.Errorf("L1 兜回后不得落入人工持有态: status=%q handler=%q", sess.Status, sess.HandlerType)
	}
}

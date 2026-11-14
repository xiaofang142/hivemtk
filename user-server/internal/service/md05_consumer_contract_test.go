// md05_consumer_contract_test.go MD-05：智能客服编排器对调用者的批准接口契约。
//
// 调用者（webhook/桥接出站侧）只需两类批准知识：IncomingContext（批准输入）
// 与 HandleResult 的批准输出字段；不需要知道 SalesResponse 的嵌套结构
// （意图类型已外置为 HandleResult.IntentType，出站侧不再拆 nil 链）。
//
// 本文件全部用例走**真实消费者接缝** WebhookService.runAIGeneration
// （webhook_ai.go 的 ctx/超时/重试装配层，夹具与 cs01/cs02 同源），
// 只断言批准输出（出站行 Extra 键、会话处理方、回复内容锚）与
// side effect 计数（出站行数、检索次数），不复制编排器内部步骤断言。
// 内部小 fake（intent/rag/dispatcher）仅用于受控变量与错误注入。
package service

import (
	"hivemtk-user/internal/pkg/tenant"
	"context"
	"errors"
	"strings"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// md05LowConfidenceIntent 低置信意图识别器：意图仍识别（price_inquiry），
// 但置信度低于编排器阈值 —— 用于钉「意图字段与置信度判据解耦」。
type md05LowConfidenceIntent struct{}

func (md05LowConfidenceIntent) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{
		IntentType: IntentPriceInquiry,
		IntentName: "价格咨询",
		Confidence: 0.05,
	}, nil
}

// md05RAGWithKB 返回带知识标记的检索结果（newC01LLMDispatcher 的 prompt
// 含 c01FAQMarker 时回知识命中文案）。
func md05RAGWithKB() *fakeRAGSearcher {
	return &fakeRAGSearcher{chunks: []dto.RAGChunk{{
		Content: "【知识库#" + c01FAQMarker + "】我们的标准版套餐价格是 1999 元/年，含全部基础功能。",
		Source:  "kb-md05",
		Score:   0.91,
		DocID:   "doc-md05-1",
		ChunkID: "chunk-md05-1",
	}}}
}

// md05DriveIngressAndAI 走真实消费者接缝：入站 → 释放认领 → runAIGeneration。
func md05DriveIngressAndAI(t *testing.T, ws *WebhookService, evt, conv, cust, content string, ctx context.Context) {
	t.Helper()
	if _, err := NewInboxIngressService().HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, conv, cust, c01AccountA1, content)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cust, Content: content},
		c01InboundHub(evt, conv, cust, c01AccountA1, content),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cust, Content: content, MessageID: evt,
		}, nil)
}

// TestMD05_ConsumerContract_KnowledgeHit_ApprovedOutputs 命中：调用者从出站行
// 即可拿到全部批准输出（意图/置信度/处理方/场景），无需碰 SalesResponse。
func TestMD05_ConsumerContract_KnowledgeHit_ApprovedOutputs(t *testing.T) {
	rag := md05RAGWithKB()
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-hit-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-hit", "md05-cust-hit", c01Question, ctx)

	out := c01OutboundRows(t, database, "md05-conv-hit")
	if len(out) != 1 {
		t.Fatalf("知识命中轮应恰 1 条 AI 出站行，实际 %d", len(out))
	}
	row := out[0]
	if !row.IsAIReply {
		t.Error("知识命中轮出站行应标 IsAIReply=true")
	}
	if v, _ := row.Extra["intent"].(string); v != IntentPriceInquiry {
		t.Errorf("extra.intent = %q，期望 %q（批准输出，不拆 SalesResponse 即可得）", v, IntentPriceInquiry)
	}
	if v, ok := row.Extra["confidence"].(float64); !ok || v <= 0 {
		t.Errorf("引擎应答轮应带置信度（>0），实际 %v（存在=%v）", row.Extra["confidence"], ok)
	}
	if ht, _ := row.Extra["handler_type"].(string); ht != "ai" {
		t.Errorf("handler_type = %v，期望 ai", row.Extra["handler_type"])
	}
	if sc, _ := row.Extra["scenario"].(string); sc != "auto_reply" {
		t.Errorf("scenario = %v，期望 auto_reply", row.Extra["scenario"])
	}
	if !strings.Contains(row.Content, c01FAQMarker) {
		t.Errorf("知识命中文案锚丢失: %q", row.Content)
	}
	if rag.calls < 1 {
		t.Error("知识命中轮检索应发生（rag.calls>=1）")
	}
}

// TestMD05_ConsumerContract_LowConfidence_TransferKeepsIntentSemantics 低置信转人工：
// 意图字段与置信度判据解耦——转人工轮的出站公告行仍带意图（与旧三条件语义同真同假）。
func TestMD05_ConsumerContract_LowConfidence_TransferKeepsIntentSemantics(t *testing.T) {
	rag := md05RAGWithKB()
	ws, _, database := c01SetupAI(t, rag, md05LowConfidenceIntent{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-lowconf-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-lowconf", "md05-cust-lowconf", c01Question, ctx)

	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("低置信轮处理方 = %q，期望 human", sess.HandlerType)
	}
	out := c01OutboundRows(t, database, "md05-conv-lowconf")
	if len(out) != 1 {
		t.Fatalf("低置信转人工轮应恰 1 条公告行，实际 %d", len(out))
	}
	row := out[0]
	if !strings.Contains(row.Content, agent_runtime.HandoffQueueNotice) {
		t.Errorf("低置信轮唯一出站行应是转接公告，实际 %q", row.Content)
	}
	if ht, _ := row.Extra["handler_type"].(string); ht != "human" {
		t.Errorf("公告行 handler_type = %v，期望 human", row.Extra["handler_type"])
	}
	// 等价钉：引擎已应答（意图已识别）⇒ 公告行带意图；旧行为 SalesResponse.Intent 非空同样写。
	if v, _ := row.Extra["intent"].(string); v != IntentPriceInquiry {
		t.Errorf("转人工轮 extra.intent = %q，期望 %q（契约收窄不得丢字段）", v, IntentPriceInquiry)
	}
	if rag.calls < 1 {
		t.Error("低置信轮引擎已跑（检索应发生）")
	}
}

// TestMD05_ConsumerContract_NoKnowledge_EngineStillAnswers 无知识：RAG 空，
// 引擎走模型兜底文案；批准输出语义不变（意图仍在、内容锚无知识标记）。
func TestMD05_ConsumerContract_NoKnowledge_EngineStillAnswers(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-nokb-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-nokb", "md05-cust-nokb", c01Question, ctx)

	out := c01OutboundRows(t, database, "md05-conv-nokb")
	if len(out) != 1 {
		t.Fatalf("无知识轮应恰 1 条 AI 出站行，实际 %d", len(out))
	}
	row := out[0]
	if !row.IsAIReply {
		t.Error("无知识轮引擎仍应答，出站行应标 IsAIReply=true")
	}
	if v, _ := row.Extra["intent"].(string); v != IntentPriceInquiry {
		t.Errorf("无知识轮 extra.intent = %q，期望 %q（意图不依赖知识命中）", v, IntentPriceInquiry)
	}
	if strings.Contains(row.Content, c01FAQMarker) {
		t.Errorf("无知识轮不得泄漏知识标记: %q", row.Content)
	}
}

// TestMD05_ConsumerContract_EngineFault_L3NoIntentLeak 模型错误：降级链 L3 转人工；
// 引擎未应答 ⇒ 意图字段恒空，出站公告行不得带 extra.intent。
func TestMD05_ConsumerContract_EngineFault_L3NoIntentLeak(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-fault-1"

	SetSalesEngineHandleFault(func(*SalesRequest) error { return errors.New("md05: engine fault injected (always)") })
	t.Cleanup(func() { SetSalesEngineHandleFault(nil) })

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-fault", "md05-cust-fault", c01Question, ctx)

	// 故障注入在引擎入口：检索一次都没发生（降级不是靠模型兜出来的）。
	if rag.calls != 0 {
		t.Errorf("引擎故障下检索应 0 次，实际 %d", rag.calls)
	}
	out := c01OutboundRows(t, database, "md05-conv-fault")
	if len(out) != 1 {
		t.Fatalf("引擎故障轮应恰 1 条转接公告行，实际 %d", len(out))
	}
	row := out[0]
	if !strings.Contains(row.Content, agent_runtime.HandoffQueueNotice) {
		t.Errorf("故障轮唯一出站行应是转接公告，实际 %q", row.Content)
	}
	if v, has := row.Extra["intent"]; has {
		t.Errorf("引擎未应答（降级链 L3）⇒ extra.intent 必须缺席，实际 %v", v)
	}
	if _, has := row.Extra["degraded"]; has {
		t.Errorf("L3 转人工轮不是模型空输出降级轮，不得带 degraded: %v", row.Extra["degraded"])
	}
	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.HandlerType != model.HandlerTypeHuman || sess.Status != model.SessionStatusWaiting {
		t.Errorf("L3 兜底应 waiting+human，实际 handler=%q status=%q", sess.HandlerType, sess.Status)
	}
}

// TestMD05_ConsumerContract_FAQCacheMountedUnresolved_NoContractChange 缓存装配
// 不改变批准接口：FAQ 缓存已挂载，但本接缝无 KB 解析（kbRepo 未装配 ⇒
// resolveFAQKB 恒 nil，缓存层不介入）⇒ 批准输出与未挂载时完全一致。
// （缓存层真未命中回落由 smart_cs_faq_cache_test.go 的冻结用例覆盖；这里只钉
// 「缓存装配」这个变量对消费者批准接口零影响。）
func TestMD05_ConsumerContract_FAQCacheMountedUnresolved_NoContractChange(t *testing.T) {
	rag := md05RAGWithKB()
	ws, orch, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	orch.SetFAQAnswerCache(
		ragcache.NewFAQAnswerCacheService(
			ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0, 0),
		llm.NewEmbeddingService())

	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-cachemount-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-cachemount", "md05-cust-cachemount", c01Question, ctx)

	out := c01OutboundRows(t, database, "md05-conv-cachemount")
	if len(out) != 1 {
		t.Fatalf("缓存未介入轮应恰 1 条 AI 出站行，实际 %d", len(out))
	}
	row := out[0]
	if !row.IsAIReply {
		t.Error("缓存未介入轮出站行应标 IsAIReply=true")
	}
	if v, _ := row.Extra["intent"].(string); v != IntentPriceInquiry {
		t.Errorf("缓存未介入轮 extra.intent = %q，期望 %q（缓存装配不改变批准输出）", v, IntentPriceInquiry)
	}
	if _, has := row.Extra["degraded"]; has {
		t.Errorf("缓存未介入不是降级轮，不得带 degraded: %v", row.Extra["degraded"])
	}
	if rag.calls < 1 {
		t.Error("缓存未介入应正常走引擎（检索应发生）")
	}
}

// TestMD05_ConsumerContract_HumanHeld_AIDoesNotSteal 人工并发：会话已被人工接管
// （坐席离线但有人工回复）⇒ AI 不抢（J-13），批准输出整体缺席、零 side effect。
func TestMD05_ConsumerContract_HumanHeld_AIDoesNotSteal(t *testing.T) {
	rag := md05RAGWithKB()
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-humanheld-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-humanheld", "md05-cust-humanheld", c01Question, ctx)

	// 入站建会话后，把会话置为人工持有（离线坐席已回复）。
	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if err := database.Model(&model.CustomerSession{}).Where("id = ?", sess.ID).Updates(map[string]any{
		"handler_type":      model.HandlerTypeHuman,
		"agent_id":          424242,
		"human_reply_count": 1,
	}).Error; err != nil {
		t.Fatalf("预置人工持有失败: %v", err)
	}

	outBefore := len(c01OutboundRows(t, database, "md05-conv-humanheld"))
	ragBefore := rag.calls
	const evt2 = "md05-evt-humanheld-2"
	md05DriveIngressAndAI(t, ws, evt2, "md05-conv-humanheld", "md05-cust-humanheld", c01Question, ctx)

	if got := rag.calls - ragBefore; got != 0 {
		t.Errorf("人工持有轮 AI 不得碰引擎/检索，增量应为 0，实际 +%d", got)
	}
	out := c01OutboundRows(t, database, "md05-conv-humanheld")
	if len(out) != outBefore {
		t.Errorf("人工持有轮不得新增出站行（AI 抢答），前=%d 后=%d", outBefore, len(out))
	}
	var held model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&held).Error; err != nil {
		t.Fatalf("会话读取失败: %v", err)
	}
	if held.HandlerType != model.HandlerTypeHuman {
		t.Errorf("人工持有轮后处理方仍应 human，实际 %q", held.HandlerType)
	}
}

// TestMD05_ConsumerContract_CtxCancelled_NoWrites ctx 取消：runAIGeneration 入口
// 的 ctx.Done 分支跳过本轮——入站已落库，但 AI 侧零写库（不产生半态）。
func TestMD05_ConsumerContract_CtxCancelled_NoWrites(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, fakeIntentRecognizer{})
	const evt = "md05-evt-ctxcancel-1"
	const conv = "md05-conv-ctxcancel"
	const cust = "md05-cust-ctxcancel"
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	// 入站用正常 ctx（持久化层用 ctx，取消态下写入本就该失败，不属本用例断言面）。
	if _, err := NewInboxIngressService().HandleIngressMessage(ctx, c01DouyinSampleEvent(evt, conv, cust, c01AccountA1, c01Question)); err != nil {
		t.Fatalf("入站失败: %v", err)
	}
	releaseReplyClaimForTest(t, evt)

	// AI 生成用已取消 ctx：runAIGeneration 入口 ctx.Done 分支跳过本轮。
	// 确定性：先填满 replySem，让 select 里「取信号量继续」这一路阻塞，
	// ctx.Done 成为唯一就绪 case（否则两路同时就绪时 select 随机取舍会假红；
	// 填满信号量的钉法同 webhook_wechat_fix_test.go 的 replySem 满用例）。
	for i := 0; i < cap(ws.replySem); i++ {
		ws.replySem <- struct{}{}
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	ws.runAIGeneration(canceledCtx, ChannelDouyin, c01AccountA1,
		&ParsedPayload{EventID: evt, Sender: cust, Content: c01Question},
		c01InboundHub(evt, conv, cust, c01AccountA1, c01Question),
		&IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: cust, Content: c01Question, MessageID: evt,
		}, nil)

	out := c01OutboundRows(t, database, conv)
	if len(out) != 0 {
		t.Errorf("ctx 取消轮不得有任何出站行，实际 %d", len(out))
	}
	if rag.calls != 0 {
		t.Errorf("ctx 取消轮检索应 0 次，实际 %d", rag.calls)
	}
	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err == nil {
		if sess.HandlerType == model.HandlerTypeAI {
			t.Error("ctx 取消轮不得把会话升级成 AI 处理态")
		}
	}
}

// TestMD05_ConsumerContract_LowConfidenceAndNoKnowledge 组合：低置信 + 无知识
// （RAG 空）⇒ 转人工成立，且意图字段与置信度判据解耦（意图仍在批准输出里）。
func TestMD05_ConsumerContract_LowConfidenceAndNoKnowledge(t *testing.T) {
	rag := &fakeRAGSearcher{}
	ws, _, database := c01SetupAI(t, rag, md05LowConfidenceIntent{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	const evt = "md05-evt-combo-1"

	md05DriveIngressAndAI(t, ws, evt, "md05-conv-combo", "md05-cust-combo", c01Question, ctx)

	var sess model.CustomerSession
	if err := database.Where("platform = ? AND account_id = ?", c01Channel, c01AccountA1).First(&sess).Error; err != nil {
		t.Fatalf("会话未落库: %v", err)
	}
	if sess.HandlerType != model.HandlerTypeHuman {
		t.Errorf("组合轮处理方 = %q，期望 human（低置信转人工不因无知识而改变）", sess.HandlerType)
	}
	out := c01OutboundRows(t, database, "md05-conv-combo")
	if len(out) != 1 {
		t.Fatalf("组合轮应恰 1 条公告行，实际 %d", len(out))
	}
	if !strings.Contains(out[0].Content, agent_runtime.HandoffQueueNotice) {
		t.Errorf("组合轮唯一出站行应是转接公告，实际 %q", out[0].Content)
	}
	if v, _ := out[0].Extra["intent"].(string); v != IntentPriceInquiry {
		t.Errorf("组合轮公告行 extra.intent = %q，期望 %q（意图与置信度解耦）", v, IntentPriceInquiry)
	}
}

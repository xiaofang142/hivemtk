// cs03_knowledge_recall_bridge_test.go CS-03 身份到知识检索贯通（服务层，真实 PG 8217 受控变量）。
//
// CS-01 领走的缺口：三样本的知识命中格现用测试工厂 newC01RAGHit 替代，本文件把
// 「真知识行发布后可被召回」的关联用真库行钉住（检索层数据矩阵见
// aiagent/knowledge/service/knowledge_release_tenant_recall_test.go，本文件钉编排侧）：
//
//	① 真 FAQ 行 → agentCtx → FAQ 快速通道召回（真 faq_entries 行 + 真 FAQService +
//	   真 FAQRepository，非工厂）：A 租户挂载智能体答 A 的标准答案并带真库 FAQEntryID
//	   引用；B 租户同关键词不同答案不串；未发布（enabled=false）行不可召回；
//	   阈值等号边界以当前比较符钉死（faqHitThresh=0.6，判据是 `<`，等于即命中）。
//	② agentCtx.RagProductIDs → 真 RagSearcher 分产品召回：召回引用含真库 chunk ID /
//	   文档 ID / 片段；未配置产品时回退全局检索（既有行为，不一律禁掉）。
//	③ FAQAnswerCache 语义层（orchestrator 接缝 SetFAQAnswerCache + 真 PG 存储）：
//	   写入守卫阈值等号边界（top1 >= threshold，等于即缓存、低于不缓存）；命中轮跳过
//	   引擎（检索/LLM 计数冻结、置信度=精确层 1.0）；KB updated_at 失效后回源。
//	④ 失败/边界：无候选诚实零、embedding 错误跳过缓存层、LLM 不可用降级如实带片段
//	   来源、模型空输出 Degraded 如实标记、ctx 取消不伪造成功、引擎故障 L3 转人工。
//
// fake 只在既有接缝上换件且全部可计数：检索=fakeRAGSearcher/计数包装、模型=httptest
// dispatcher（带调用计数）、embedding=桩。降级/命中来源逐格如实断言，不以任何一格
// 宣称模型效果全面达标（回答精准性/人工金标归 R10，见文件尾边界声明）。
package service

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

	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	kbservice "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// —— CS-03 冻结字段（cs03- 前缀：内容窗口去重键/AI 处理标记都在进程级缓存，须隔离）——
const (
	cs03TenantA   = uint(701)
	cs03TenantB   = uint(702)
	cs03AgentFAQ  = uint(9101) // 租户 A 的 FAQ 智能体
	cs03AgentFAQB = uint(9202) // 租户 B 的对照智能体（同关键词不同答案）
	cs03AgentEq   = uint(9303) // 阈值等号边界智能体（Confidence=0.6 → 命中）
	cs03AgentLt   = uint(9304) // 阈值下界智能体（Confidence=0.55 → 不命中）

	cs03FAQKeyword = "发票"
	cs03Question   = "发票怎么开具"
	// cs03QuestionEmptyOnly 空输出降级格专用问题：dispatch 响应缓存
	// （llm:dispatch:*，进程级全局、TTL 1h）以 scenario+systemPrompt+prompt 为键，
	// 与前序格共用 cs03Question 会命中前序格缓存的真实回复（空输出服务器根本
	// 不被调用），Degraded 断言被跨测试污染打红——批跑红、隔离绿正是该形态。
	// 空内容不入缓存（dispatcher_cache.go setCache 显式跳过），本键无前向污染。
	cs03QuestionEmptyOnly = "发票怎么开具（空输出降级格专用）"
	cs03FAQAnswerA        = "CS03-FAQ-A：电子发票可在订单页自助开具。"
	cs03FAQAnswerB        = "CS03-FAQ-B：专票需联系客服线下邮寄。"
	cs03FAQDisabld        = "CS03-FAQ-A-DISABLED：本行未发布不得外泄。"
	cs03FAQEqQ            = "售后申请流程细则说明" // 与 cs03Question 零公共词元（含单字）
	cs03FAQEqAns          = "CS03-FAQ-EQ：等号边界行。"
	cs03FAQLtAns          = "CS03-FAQ-LT：下界行。"

	cs03AccA  = "cs03-acc-a"
	cs03AccB  = "cs03-acc-b"
	cs03CustA = "cs03-openid-a"
	cs03CustB = "cs03-openid-b"
)

// cs03LLMCounter 可计数模型 fake 的计数器。
type cs03LLMCounter struct{ calls int32 }

// cs03NewLLM 装配可计数 dispatcher（Scenario 路由同 c01DispatcherAt）：
// prompt 含 marker 时回 reply，否则通用话术。
func cs03NewLLM(t *testing.T, counter *cs03LLMCounter, marker, reply string) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter.calls, 1)
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
		if marker != "" && strings.Contains(prompt.String(), marker) {
			out = reply
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": out}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return c01DispatcherAt(t, srv.URL, "fake-cs03")
}

// cs03EmptyLLM 恒空输出模型 fake（空输出降级档）。
func cs03EmptyLLM(t *testing.T, counter *cs03LLMCounter) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&counter.calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return c01DispatcherAt(t, srv.URL, "fake-cs03-empty")
}

// cs03ErrLLM 恒 500 模型 fake：generateCandidate 真调真败（计数>0），走既有降级出口。
func cs03ErrLLM(t *testing.T, counter *cs03LLMCounter) *llm.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&counter.calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return c01DispatcherAt(t, srv.URL, "fake-cs03-err")
}

// cs03DetEmbedder 确定性查询向量桩：同文本恒同向量（Tier1 精确层的位相等前提），
// 可注入错误（embedding 故障档）。维度对齐 vector(1024)。
type cs03DetEmbedder struct{ err error }

func (e cs03DetEmbedder) Embed(ctx context.Context, cfg *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, 1024)
		var h uint32 = 2166136261
		for _, r := range text {
			h = (h ^ uint32(r)) * 16777619
		}
		v[0] = float32(h%9000+500) / 10000
		v[1] = float32((h/7)%9000+500) / 10000
		out[i] = v
	}
	return out, nil
}

func (e cs03DetEmbedder) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	vecs, err := e.Embed(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e cs03DetEmbedder) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Model: "fake-cs03-det", Dimension: 1024}
}

// cs03BoolPtr / cs03UintPtr 小工具。
func cs03BoolPtr(b bool) *bool { return &b }
func cs03UintPtr(u uint) *uint { return &u }

// cs03SeedFAQ 灌真 FAQ 行（faq_entries 真库行，非测试工厂；on 模式下随 ctx 落租户章）。
func cs03SeedFAQ(t *testing.T, db *gorm.DB, ctx context.Context, agentID uint, question, answer string, conf float64, enabled bool) model.FAQEntry {
	t.Helper()
	row := model.FAQEntry{
		Question: question, Answer: answer,
		Keywords:   []string{cs03FAQKeyword},
		Confidence: conf, AgentID: cs03UintPtr(agentID), Enabled: cs03BoolPtr(enabled),
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("灌入 FAQ 行失败（agent=%d）: %v", agentID, err)
	}
	return row
}

// cs03WaitFor 轮询等待异步写（缓存 Store 走后台 goroutine）。
func cs03WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// TestSmartCS03_PublishedFAQRow_RecalledViaAgentCtx 真 FAQ 行 → agentCtx → 快速通道召回：
//
//	① A 租户智能体（真 faq_entries 行，已发布=enabled）命中 A 的标准答案，
//	   引用含真库 FAQEntryID（可追溯），外发恰 1 条 AI 消息；
//	② B 租户同关键词不同答案不串（B 智能体答 B 的答案；A 智能体的检索路径看不见 B 行）；
//	③ 未发布行（enabled=false）绝不可召回；
//	④ FAQ 命中轮不进 RAG（检索计数 0——快速通道短路是既有设计）。
func TestSmartCS03_PublishedFAQRow_RecalledViaAgentCtx(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	database := testutil.NewTestDB(t,
		&model.FAQEntry{}, &model.CustomerSession{}, &model.SessionMessage{},
		&model.AISuggestion{}, &model.AgentStatus{}, &model.Customer{},
	)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cs03TenantB)

	faqA := cs03SeedFAQ(t, database, ctxA, cs03AgentFAQ, "售后开票流程说明", cs03FAQAnswerA, 0.9, true)
	// 未发布（enabled=false）：同关键词、更高置信度——若被召回立即在答案里现形。
	cs03SeedFAQ(t, database, ctxA, cs03AgentFAQ, "未发布开票条目", cs03FAQDisabld, 0.99, false)
	faqB := cs03SeedFAQ(t, database, ctxB, cs03AgentFAQB, "售后开票流程说明", cs03FAQAnswerB, 0.9, true)

	rag := &fakeRAGSearcher{}
	llmCounter := &cs03LLMCounter{}
	engine := NewSalesEngine(cs03NewLLM(t, llmCounter, c01FAQMarker, "命中"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	engine.SetFAQService(NewFAQService()) // 真 FAQService → 真 FAQRepository → 真库行
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)

	// 格①：A 租户智能体命中 A 的标准答案，引用可追溯。
	res, err := orch.HandleIncomingWithAgent(ctxA, &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-faq-1",
	}, &dto.AgentContext{AgentID: cs03AgentFAQ, EnableRAG: true, RAGTopK: 3})
	if err != nil {
		t.Fatalf("编排失败: %v", err)
	}
	if !res.AIReplied {
		t.Fatalf("FAQ 命中轮应 AI 直接回复: %+v", res)
	}
	if !strings.Contains(res.Reply, "CS03-FAQ-A") {
		t.Errorf("回复应携带 A 租户已发布 FAQ 标准答案: %q", res.Reply)
	}
	if strings.Contains(res.Reply, "CS03-FAQ-B") || strings.Contains(res.Reply, "DISABLED") {
		t.Errorf("回复串入 B 租户答案或未发布行: %q", res.Reply)
	}
	if res.SalesResponse == nil || !res.SalesResponse.DefiniteAnswer {
		t.Errorf("FAQ 快速通道应标 DefiniteAnswer（标准答案不进置信度判据）: %+v", res.SalesResponse)
	}
	if len(res.Citations) == 0 || res.Citations[0].FAQ == nil || res.Citations[0].FAQ.FAQEntryID != faqA.ID {
		t.Errorf("引用应定位到真库 FAQ 行 id=%d: %+v", faqA.ID, res.Citations)
	}
	if rag.calls != 0 {
		t.Errorf("FAQ 快速通道命中后不应再进 RAG（既有短路设计），检索 %d 次", rag.calls)
	}
	var aiMsgs int64
	database.WithContext(ctxA).Model(&model.SessionMessage{}).
		Where("session_id = ? AND sender_type = ?", res.SessionID, "ai").Count(&aiMsgs)
	if aiMsgs != 1 {
		t.Errorf("外发应恰 1 条 AI 消息，实际 %d", aiMsgs)
	}

	// 格②：B 租户同关键词 → B 自己的答案；直接调检索层断言 A 智能体看不见 B 行。
	resB, err := orch.HandleIncomingWithAgent(ctxB, &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccB, SenderID: cs03CustB,
		Content: cs03Question, MessageID: "cs03-msg-faq-2",
	}, &dto.AgentContext{AgentID: cs03AgentFAQB, EnableRAG: true, RAGTopK: 3})
	if err != nil {
		t.Fatalf("B 租户编排失败: %v", err)
	}
	if !strings.Contains(resB.Reply, "CS03-FAQ-B") || strings.Contains(resB.Reply, "CS03-FAQ-A") {
		t.Errorf("B 租户应答 B 的标准答案（同关键词不同答案不串）: %q", resB.Reply)
	}
	if len(resB.Citations) == 0 || resB.Citations[0].FAQ == nil || resB.Citations[0].FAQ.FAQEntryID != faqB.ID {
		t.Errorf("B 引用应定位到 B 的真库行 id=%d: %+v", faqB.ID, resB.Citations)
	}
	aMatches, err := engine.faqSvc.MatchByAgent(ctxA, cs03AgentFAQ, cs03Question, 5)
	if err != nil {
		t.Fatalf("A 智能体检索失败: %v", err)
	}
	if len(aMatches) == 0 {
		t.Fatal("A 智能体检索应命中自己的 FAQ 行（前置不成立）")
	}
	for _, m := range aMatches {
		if strings.Contains(m.Entry.Answer, "CS03-FAQ-B") {
			t.Errorf("A 智能体的检索路径召回 B 租户 FAQ 行（越租户）: %q", m.Entry.Answer)
		}
	}
}

// TestSmartCS03_FAQThresholdBoundary_EqualityRecalls 阈值等号边界（以当前比较符为准，
// 不猜等号行为）：faqHitThresh=0.6，tryFAQFastPath 的判据是 `top.Score < faqHitThresh`
// ⇒ 等于阈值即命中、低于阈值不命中。夹具把 jaccard 压到 0（问法与用户句零公共词元、
// 关键词精确命中一次）使 score 恰为 (1.0×1+0.3×0)×Confidence，等号场景分数可精确构造。
func TestSmartCS03_FAQThresholdBoundary_EqualityRecalls(t *testing.T) {
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	database := testutil.NewTestDB(t,
		&model.FAQEntry{}, &model.CustomerSession{}, &model.SessionMessage{},
		&model.AISuggestion{}, &model.AgentStatus{}, &model.Customer{},
	)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	cs03SeedFAQ(t, database, ctxA, cs03AgentEq, cs03FAQEqQ, cs03FAQEqAns, 0.6, true)
	cs03SeedFAQ(t, database, ctxA, cs03AgentLt, cs03FAQEqQ, cs03FAQLtAns, 0.55, true)

	rag := &fakeRAGSearcher{}
	engine := NewSalesEngine(cs03NewLLM(t, &cs03LLMCounter{}, c01FAQMarker, "命中"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	engine.SetFAQService(NewFAQService())
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)

	// 等于阈值 → 命中（当前比较符 `<` 的等号语义：不跳过）。
	resEq, err := orch.HandleIncomingWithAgent(ctxA, &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-eq-1",
	}, &dto.AgentContext{AgentID: cs03AgentEq, EnableRAG: true})
	if err != nil {
		t.Fatalf("等号轮编排失败: %v", err)
	}
	if !resEq.AIReplied || !strings.Contains(resEq.Reply, "CS03-FAQ-EQ") {
		t.Errorf("score==faqHitThresh 应命中快速通道（当前比较符 `<`），实际 reply=%q aiReplied=%v", resEq.Reply, resEq.AIReplied)
	}

	// 低于阈值 → 不命中，落到正常链（本夹具 RAG 无命中 → 通用回复，无 FAQ 答案）。
	resLt, err := orch.HandleIncomingWithAgent(ctxA, &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-lt-1",
	}, &dto.AgentContext{AgentID: cs03AgentLt, EnableRAG: true})
	if err != nil {
		t.Fatalf("下界轮编排失败: %v", err)
	}
	if strings.Contains(resLt.Reply, "CS03-FAQ-LT") {
		t.Errorf("score<faqHitThresh 不得命中快速通道: %q", resLt.Reply)
	}
	if rag.calls < 1 {
		t.Error("下界轮应真走检索链（快速通道未命中 → RAG）")
	}
}

// TestSmartCS03_AgentCtxRagProductIDs_RealKBRecall agentCtx.RagProductIDs → 真 RagSearcher
// 分产品召回（真库 knowledge_chunks 行，非工厂）：召回引用含真库 chunk ID/文档 ID/片段；
// 未配置产品时回退全局检索（既有行为，不一律禁掉）。
//
// 检索器用真 RagSearcher（真库仓储 + 禁用 hybrid 走 legacy 直查）；查询向量由真实
// embedding 服务编码（测试环境受控变量允许 hash 兜底档，确定性向量，不依赖外部 TEI）。
func TestSmartCS03_AgentCtxRagProductIDs_RealKBRecall(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.KnowledgeChunk{}, &model.KnowledgeDocument{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	testmigrate.RunTestMigrations(t, database) // embedding 列由迁移补（模型只声明形状）
	const (
		prodA = "kb-cs03-rag-a"
		prodB = "kb-cs03-rag-b"
		markA = "CS03RAG-A"
		ansA  = markA + " 会员权益：年付赠三个月。"
		markB = "CS03RAG-B"
		ansB  = markB + " 物流时效：现货48小时内发货。"
	)
	ctx := context.Background()
	seed := func(product, content string) model.KnowledgeChunk {
		ch := model.KnowledgeChunk{ProductID: product, Content: content, EmbeddingSource: "tei"}
		if err := database.Create(&ch).Error; err != nil {
			t.Fatalf("灌入分段失败: %v", err)
		}
		// 夹具向量用全 1 单位向量（真检索 SQL 只要求向量合法，可见性/隔离断言不依赖语义）。
		vec := "[" + strings.TrimSuffix(strings.Repeat("1,", 1024), ",") + "]"
		if err := database.Model(&model.KnowledgeChunk{}).Where("id = ?", ch.ID).
			Update("embedding", gorm.Expr("?::vector", vec)).Error; err != nil {
			t.Fatalf("补向量失败: %v", err)
		}
		return ch
	}
	chA := seed(prodA, ansA)
	chB := seed(prodB, ansB)

	realSearcher := kbservice.NewRagSearcherWithRepo(knowledgerepo.NewKnowledgeChunkRepository(database))
	realSearcher.SetHybridSearcher(nil)

	ragCalls := 0
	counting := &cs03CountingRAG{inner: realSearcher, onCall: func() { ragCalls++ }}
	llmCounter := &cs03LLMCounter{}
	engine := NewSalesEngine(cs03NewLLM(t, llmCounter, "CS03RAG", "已为您核对知识库记录。"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, counting, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	ctxIn := func(msg, msgID string) *IncomingContext {
		return &IncomingContext{
			Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
			Content: msg, MessageID: msgID,
		}
	}

	// 格①：agentCtx 绑定 A 库 → 只召回 A 的真库行，引用定位到真 chunk（ID/文档/片段）。
	res, err := orch.HandleIncomingWithAgent(ctx, ctxIn("会员权益有哪些", "cs03-msg-rag-1"), &dto.AgentContext{
		AgentID: 9401, EnableRAG: true, RAGTopK: 3, RagProductIDs: []string{prodA},
	})
	if err != nil {
		t.Fatalf("编排失败: %v", err)
	}
	if ragCalls < 1 {
		t.Fatal("RAG 检索未被调用")
	}
	chunks := res.SalesResponse.RAGChunks
	if len(chunks) == 0 {
		t.Fatal("分产品召回为空")
	}
	located := false
	for _, c := range chunks {
		if strings.Contains(c.Content, markB) {
			t.Errorf("绑定 A 库却召回 B 库内容（产品隔离失效）: %q", c.Content)
		}
		if c.ChunkID == itoaUint64(chA.ID) && c.DocID == itoaUint64(chA.DocumentID) && strings.Contains(c.Content, markA) {
			located = true
		}
	}
	if !located {
		t.Fatalf("召回引用未定位到真库行 id=%d doc=%d: %+v", chA.ID, chA.DocumentID, chunks)
	}

	// 格②：换绑 B 库 → 只召回 B。
	ragCalls = 0
	resB, err := orch.HandleIncomingWithAgent(ctx, ctxIn("物流时效是多久", "cs03-msg-rag-2"), &dto.AgentContext{
		AgentID: 9401, EnableRAG: true, RAGTopK: 3, RagProductIDs: []string{prodB},
	})
	if err != nil {
		t.Fatalf("B 库编排失败: %v", err)
	}
	for _, c := range resB.SalesResponse.RAGChunks {
		if strings.Contains(c.Content, markA) {
			t.Errorf("绑定 B 库却召回 A 库内容: %q", c.Content)
		}
	}
	if len(resB.SalesResponse.RAGChunks) == 0 || !strings.Contains(resB.SalesResponse.RAGChunks[0].Content, markB) {
		t.Errorf("B 库召回异常: %+v", resB.SalesResponse.RAGChunks)
	}

	// 格③：未配置产品 → 回退全局检索（既有行为），两个库都在视野内。
	ragCalls = 0
	resG, err := orch.HandleIncomingWithAgent(ctx, ctxIn("会员和物流都说明一下", "cs03-msg-rag-3"), &dto.AgentContext{
		AgentID: 9401, EnableRAG: true, RAGTopK: 5,
	})
	if err != nil {
		t.Fatalf("全局回退编排失败: %v", err)
	}
	if ragCalls < 1 {
		t.Fatal("全局回退轮检索未被调用")
	}
	sawA, sawB := false, false
	for _, c := range resG.SalesResponse.RAGChunks {
		if strings.Contains(c.Content, markA) {
			sawA = true
		}
		if strings.Contains(c.Content, markB) {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Errorf("全局回退应可见两库（不一律禁掉），A=%v B=%v: %+v", sawA, sawB, resG.SalesResponse.RAGChunks)
	}
	_ = chB
}

func itoaUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// cs03CountingRAG 可计数包装（真检索器 + 调用钩子；fake 与真件都要求可统计）。
// 必须同时透传 SearchProducts 能力：recallRAG 以 RAGProductSearcher 类型断言
// 判定分产品召回路径，包装缺该方法会静默退化成全局检索（两库同分、Source 为空），
// 产品隔离断言就测不到真路径——这正是首跑失败的根因（RED→GREEN 修复）。
type cs03CountingRAG struct {
	inner  RAGSearcher
	onCall func()
}

func (c *cs03CountingRAG) Search(ctx context.Context, query string, topK int) ([]dto.RAGChunk, error) {
	c.onCall()
	return c.inner.Search(ctx, query, topK)
}

func (c *cs03CountingRAG) SearchProducts(ctx context.Context, productIDs []string, query string, topK int) ([]dto.RAGChunk, error) {
	c.onCall()
	if ps, ok := c.inner.(RAGProductSearcher); ok {
		return ps.SearchProducts(ctx, productIDs, query, topK)
	}
	return nil, nil
}

// cs03CacheFixture 缓存语义层夹具：真 PG 存储 + 真 KB 行 + 可计数引擎。
type cs03CacheFixture struct {
	db        *gorm.DB
	orch      *SmartCSOrchestrator
	rag       *fakeRAGSearcher
	llm       *cs03LLMCounter
	kb        model.KnowledgeBase
	agentID   uint
	top1Score float64
}

func cs03NewCacheFixture(t *testing.T, embedder cs03DetEmbedder, top1Score float64) *cs03CacheFixture {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{}, &model.KnowledgeBase{}, &model.RAGAnswerCache{},
	)
	f := &cs03CacheFixture{db: database, rag: &fakeRAGSearcher{}, llm: &cs03LLMCounter{}, top1Score: top1Score}
	f.agentID = 9501

	enabled := true
	f.kb = model.KnowledgeBase{
		KBCode: "kb-cs03-cache-" + itoaUint64(uint64(time.Now().UnixNano())), Type: model.KnowledgeBaseTypeFAQ,
		Name: "cs03-cache", OwnerAgentID: &f.agentID, Enabled: &enabled,
	}
	if err := database.Create(&f.kb).Error; err != nil {
		t.Fatalf("灌入 KB 行失败: %v", err)
	}

	engine := NewSalesEngine(cs03NewLLM(t, f.llm, "CS03CACHE", "CS03CACHE 已确认：标准版 1999 元/年。"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, f.rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10},
		repository.NewKnowledgeBaseRepository(database))
	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0, 0)
	orch.SetFAQAnswerCache(cacheSvc, embedder)
	f.orch = orch
	return f
}

func (f *cs03CacheFixture) send(t *testing.T, msgID string) *HandleResult {
	t.Helper()
	res, err := f.orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: msgID,
	}, &dto.AgentContext{AgentID: f.agentID, EnableRAG: true})
	if err != nil {
		t.Fatalf("编排失败: %v", err)
	}
	return res
}

func (f *cs03CacheFixture) cacheRows(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.RAGAnswerCache{}).Where("kb_id = ?", itoaUint64(uint64(f.kb.ID))).Count(&n).Error; err != nil {
		t.Fatalf("读缓存行失败: %v", err)
	}
	return n
}

// TestSmartCS03_FAQAnswerCache_StoreGuard_AtThresholdStores 写入守卫等号边界：
// top1 RAG 分数 == 置信度阈值（0.7）→ 允许缓存（当前比较符 `>=`）。
func TestSmartCS03_FAQAnswerCache_StoreGuard_AtThresholdStores(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, 0.7)
	f.rag.chunks = []dto.RAGChunk{{
		Content: "CS03CACHE 标准版 1999 元/年，含全部基础功能。", Score: f.top1Score,
		DocID: "cs03cache-doc", ChunkID: "cs03cache-chunk",
	}}
	res := f.send(t, "cs03-msg-cache-eq-1")
	if !res.AIReplied {
		t.Fatalf("应 AI 回复: %+v", res)
	}
	cs03WaitFor(t, "等于阈值的回答应写入缓存", func() bool { return f.cacheRows(t) == 1 })
}

// TestSmartCS03_FAQAnswerCache_StoreGuard_BelowThresholdSkips 写入守卫下界：
// top1 < 阈值 → 不写缓存（缓存投毒防线；无门槛写入会让低分召回长期留在缓存绕过判据）。
func TestSmartCS03_FAQAnswerCache_StoreGuard_BelowThresholdSkips(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, 0.6999)
	f.rag.chunks = []dto.RAGChunk{{
		Content: "CS03CACHE 标准版 1999 元/年，含全部基础功能。", Score: f.top1Score,
		DocID: "cs03cache-doc", ChunkID: "cs03cache-chunk",
	}}
	res := f.send(t, "cs03-msg-cache-lt-1")
	if !res.AIReplied {
		t.Fatalf("低分召回轮仍应走正常链回复: %+v", res)
	}
	time.Sleep(600 * time.Millisecond) // 给后台 goroutine 充分的"不该写"窗口
	if n := f.cacheRows(t); n != 0 {
		t.Errorf("top1 %.4f < 阈值 0.7 不得写缓存，实际 %d 行", f.top1Score, n)
	}
}

// TestSmartCS03_FAQAnswerCache_HitSkipsEngine_AndKBUpdateInvalidates 语义层读写闭环：
// 第一轮回答入缓存 → 第二轮同问命中精确层（Tier=exact、Similarity=1、引擎/检索零调用）
// → KB updated_at 变更后缓存失效回源（引擎重新被调用，不拿过期答案冒充命中）。
func TestSmartCS03_FAQAnswerCache_HitSkipsEngine_AndKBUpdateInvalidates(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, 0.9)
	f.rag.chunks = []dto.RAGChunk{{
		Content: "CS03CACHE 标准版 1999 元/年，含全部基础功能。", Score: f.top1Score,
		DocID: "cs03cache-doc", ChunkID: "cs03cache-chunk",
	}}

	res1 := f.send(t, "cs03-msg-cache-hit-1")
	if !res1.AIReplied || res1.Degraded {
		t.Fatalf("第一轮应正常 AI 回复（非降级）: %+v", res1)
	}
	if f.rag.calls != 1 {
		t.Fatalf("第一轮应恰 1 次检索，实际 %d", f.rag.calls)
	}
	llmAfterRound1 := atomic.LoadInt32(&f.llm.calls)
	cs03WaitFor(t, "第一轮回答入缓存", func() bool { return f.cacheRows(t) == 1 })

	// 第二轮：同问命中缓存 → 引擎零调用（检索/LLM 计数冻结）、置信度=精确层 1.0。
	res2 := f.send(t, "cs03-msg-cache-hit-2")
	if !res2.AIReplied {
		t.Fatalf("缓存命中轮应 AI 回复: %+v", res2)
	}
	if res2.Reply != res1.Reply {
		t.Errorf("缓存命中应返回缓存答案 %q，实际 %q", res1.Reply, res2.Reply)
	}
	if f.rag.calls != 1 {
		t.Errorf("缓存命中轮不得再检索（快速通道语义），实际 %d 次", f.rag.calls)
	}
	if got := atomic.LoadInt32(&f.llm.calls); got != llmAfterRound1 {
		t.Errorf("缓存命中轮不得再调模型：第一轮 %d 次，命中轮 %d 次", llmAfterRound1, got)
	}
	if res2.Confidence != 1 {
		t.Errorf("精确层命中置信度应为 1，实际 %v", res2.Confidence)
	}

	// 失效：KB updated_at 变更 → 命中前校验删行 → 第三轮回源（引擎重新被调用）。
	if err := f.db.Model(&model.KnowledgeBase{}).Where("id = ?", f.kb.ID).
		Update("updated_at", time.Now().Add(2*time.Hour)).Error; err != nil {
		t.Fatalf("更新 KB updated_at 失败: %v", err)
	}
	res3 := f.send(t, "cs03-msg-cache-hit-3")
	if !res3.AIReplied {
		t.Fatalf("失效回源轮应正常 AI 回复: %+v", res3)
	}
	if f.rag.calls != 2 {
		t.Errorf("失效后应重新检索回源（实际 %d 次）", f.rag.calls)
	}
	if got := atomic.LoadInt32(&f.llm.calls); got <= llmAfterRound1 {
		t.Errorf("失效后应重新调模型回源（第一轮 %d 次，回源轮 %d 次）", llmAfterRound1, got)
	}
}

// TestSmartCS03_Boundary_NoCandidate_EmbeddingError_HonestDegradation 失败/边界（一）：
// 无候选诚实零（查过、0 块、回复不带知识标记、不伪造命中）；embedding 服务错误 →
// 缓存层整段跳过（不 lookup 不 store），链路按既有配置走正常检索生成。
func TestSmartCS03_Boundary_NoCandidate_EmbeddingError_HonestDegradation(t *testing.T) {
	// 格①：无候选。
	database := testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	rag := &fakeRAGSearcher{} // 查过但 0 块
	llmCounter := &cs03LLMCounter{}
	engine := NewSalesEngine(cs03NewLLM(t, llmCounter, c01FAQMarker, "命中"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	res, err := orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-bnd-nocand",
	}, &dto.AgentContext{AgentID: 9601, EnableRAG: true})
	if err != nil {
		t.Fatalf("无候选轮编排失败: %v", err)
	}
	if rag.calls != 1 {
		t.Errorf("无候选格应真检索恰 1 次，实际 %d", rag.calls)
	}
	if !res.AIReplied || res.Transferred {
		t.Errorf("高置信意图 + 无候选应 AI 正常回复（不伪造知识命中）: %+v", res)
	}
	if strings.Contains(res.Reply, c01FAQMarker) {
		t.Errorf("无候选轮回复不得携带知识标记: %q", res.Reply)
	}
	if res.Degraded {
		t.Error("无候选不是降级轮，不得标 Degraded（来源字段如实）")
	}
	var aiMsgs int64
	database.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", res.SessionID, "ai").Count(&aiMsgs)
	if aiMsgs != 1 {
		t.Errorf("无候选轮外发应恰 1 条，实际 %d", aiMsgs)
	}

	// 格②：embedding 错误 → 缓存层跳过，正常链不受影响，不伪造缓存。
	database2 := testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{}, &model.KnowledgeBase{}, &model.RAGAnswerCache{},
	)
	agentID := uint(9502)
	enabled := true
	kb := model.KnowledgeBase{KBCode: "kb-cs03-emb-err", Type: model.KnowledgeBaseTypeFAQ, Name: "emb-err", OwnerAgentID: &agentID, Enabled: &enabled}
	if err := database2.Create(&kb).Error; err != nil {
		t.Fatalf("灌入 KB 行失败: %v", err)
	}
	rag2 := &fakeRAGSearcher{chunks: []dto.RAGChunk{{Content: "CS03CACHE 内容", Score: 0.9, DocID: "d", ChunkID: "c"}}}
	engine2 := NewSalesEngine(cs03NewLLM(t, &cs03LLMCounter{}, "CS03CACHE", "CS03CACHE 已确认。"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag2, fakeScript{}, fakeCustomerLookup{})
	orch2 := NewSmartCSOrchestrator(engine2, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10},
		repository.NewKnowledgeBaseRepository(database2))
	orch2.SetFAQAnswerCache(ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database2), ragcache.NewPGKBMetaReader(database2), 0, 0),
		cs03DetEmbedder{err: fmt.Errorf("cs03: embedding unavailable")})
	res2, err := orch2.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-bnd-emberr",
	}, &dto.AgentContext{AgentID: agentID, EnableRAG: true})
	if err != nil {
		t.Fatalf("embedding 故障轮编排失败: %v", err)
	}
	if !res2.AIReplied || rag2.calls != 1 {
		t.Errorf("embedding 故障应按既有配置走正常链（AI 回复 + 检索 1 次）: aiReplied=%v ragCalls=%d", res2.AIReplied, rag2.calls)
	}
	var n int64
	database2.Model(&model.RAGAnswerCache{}).Count(&n)
	if n != 0 {
		t.Errorf("embedding 故障下不得写缓存（无查询向量），实际 %d 行", n)
	}
}

// TestSmartCS03_Boundary_LLMUnavailable_RagFallbackProvenance LLM 不可用（500）：
// 引擎走既有降级出口——有 RAG 命中时回复如实带片段内容（不是编造、不是模型话术），
// 空输出降级标记不得滥用（这轮不是 emptyReplyFallback）。
func TestSmartCS03_Boundary_LLMUnavailable_RagFallbackProvenance(t *testing.T) {
	_ = testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	rag := &fakeRAGSearcher{chunks: []dto.RAGChunk{{
		Content: "CS03RAGPROV 会员权益：年付赠三个月。", Score: 0.91,
		DocID: "cs03prov-doc", ChunkID: "cs03prov-chunk",
	}}}
	llmCounter := &cs03LLMCounter{}
	engine := NewSalesEngine(cs03ErrLLM(t, llmCounter),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	res, err := orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03Question, MessageID: "cs03-msg-bnd-llmerr",
	}, &dto.AgentContext{AgentID: 9602, EnableRAG: true})
	if err != nil {
		t.Fatalf("LLM 故障轮编排失败: %v", err)
	}
	if atomic.LoadInt32(&llmCounter.calls) < 1 {
		t.Error("模型 fake 未被实际调用（降级前提不成立）")
	}
	if !res.AIReplied {
		t.Fatalf("LLM 故障 + RAG 命中应按既有降级出口给出片段兜底回复: %+v", res)
	}
	if !strings.Contains(res.Reply, "CS03RAGPROV") {
		t.Errorf("降级回复应如实携带真片段内容（来源可追溯）: %q", res.Reply)
	}
	if res.Degraded {
		t.Error("片段兜底不是空输出降级，不得标 Degraded（来源字段如实）")
	}
	if res.SalesResponse != nil && len(res.SalesResponse.RAGChunks) == 0 {
		t.Error("降级回复应有真实 RAG 召回记录（引用定位在 SalesResponse.RAGChunks）")
	}
}

// TestSmartCS03_Boundary_EmptyReply_Degraded_CtxCancel_EngineFault 失败/边界（二）：
// 模型空输出 → Degraded 如实标记（不冒充正常答案，CS-02 口径衔接）；ctx 取消 →
// 报错不伪造成功；引擎入口故障 → L3 转人工（决策/原因/外发次数逐项断言）。
func TestSmartCS03_Boundary_EmptyReply_Degraded_CtxCancel_EngineFault(t *testing.T) {
	// 格①：空输出 → Degraded=true，回复是兜底文案不是模型答案，查过无命中。
	database := testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	rag := &fakeRAGSearcher{}
	engine := NewSalesEngine(cs03EmptyLLM(t, &cs03LLMCounter{}),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag, fakeScript{}, fakeCustomerLookup{})
	orch := NewSmartCSOrchestrator(engine, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	res, err := orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA,
		Content: cs03QuestionEmptyOnly, MessageID: "cs03-msg-bnd-empty",
	}, &dto.AgentContext{AgentID: 9603, EnableRAG: true})
	if err != nil {
		t.Fatalf("空输出轮编排失败: %v", err)
	}
	if !res.Degraded {
		t.Error("模型空输出轮应标 Degraded（降级来源如实）")
	}
	if !strings.Contains(res.Reply, "无法处理") || !strings.Contains(res.Reply, "请稍后再试") {
		t.Errorf("空输出轮回复应是仓库声明的降级文案: %q", res.Reply)
	}
	if rag.calls != 1 || len(rag.chunks) != 0 {
		t.Errorf("空输出轮应查过且无命中（calls=%d chunks=%d）", rag.calls, len(rag.chunks))
	}
	var aiMsgs int64
	database.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", res.SessionID, "ai").Count(&aiMsgs)
	if aiMsgs != 1 {
		t.Errorf("空输出降级文案按既有获准语义外发恰 1 条，实际 %d", aiMsgs)
	}

	// 格②：ctx 取消 → 报错返回，不伪造成功（无会话、无消息、无外发）。
	database2 := testutil.NewTestDB(t, &model.CustomerSession{}, &model.SessionMessage{})
	rag2 := &fakeRAGSearcher{}
	engine2 := NewSalesEngine(cs03NewLLM(t, &cs03LLMCounter{}, c01FAQMarker, "命中"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag2, fakeScript{}, fakeCustomerLookup{})
	orch2 := NewSmartCSOrchestrator(engine2, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := orch2.HandleIncomingWithAgent(ctxCanceled, &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: "cs03-openid-cancel",
		Content: cs03Question, MessageID: "cs03-msg-bnd-cancel",
	}, &dto.AgentContext{AgentID: 9604, EnableRAG: true}); err == nil {
		t.Fatal("ctx 已取消应返回错误（不伪造成功）")
	}
	if rag2.calls != 0 {
		t.Errorf("ctx 取消轮不得进入检索，实际 %d 次", rag2.calls)
	}
	var sessN, msgN int64
	database2.Model(&model.CustomerSession{}).Where("user_id = ?", "cs03-openid-cancel").Count(&sessN)
	database2.Model(&model.SessionMessage{}).Where("sender_id = ?", "cs03-openid-cancel").Count(&msgN)
	if sessN != 0 || msgN != 0 {
		t.Errorf("ctx 取消轮不得落任何会话/消息行（sess=%d msg=%d）", sessN, msgN)
	}

	// 格③：引擎入口故障 → L1 同败 → L2 规则 → L3 转人工（决策/原因/外发次数）。
	database3 := testutil.NewTestDB(t,
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{},
	)
	rag3 := &fakeRAGSearcher{}
	engine3 := NewSalesEngine(cs03NewLLM(t, &cs03LLMCounter{}, c01FAQMarker, "命中"),
		fakeIntentRecognizer{}, fakeMemory{}, fakeSOP{}, rag3, fakeScript{}, fakeCustomerLookup{})
	orch3 := NewSmartCSOrchestrator(engine3, &OrchestratorConfig{ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10}, nil)
	SetSalesEngineHandleFault(func(*SalesRequest) error { return errCs03Fault })
	t.Cleanup(func() { SetSalesEngineHandleFault(nil) })
	res3, err := orch3.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: "cs03-openid-fault",
		Content: cs03Question, MessageID: "cs03-msg-bnd-fault",
	}, &dto.AgentContext{AgentID: 9605, EnableRAG: true})
	if err != nil {
		t.Fatalf("引擎故障轮不应向上抛错（L3 兜底）: %v", err)
	}
	if !res3.Transferred || res3.HandlerType != model.HandlerTypeHuman {
		t.Errorf("引擎故障应 L3 转人工: %+v", res3)
	}
	if !strings.Contains(res3.TransferReason, "降级链 Level 3") {
		t.Errorf("转人工原因应记录降级链 L3: %q", res3.TransferReason)
	}
	if rag3.calls != 0 {
		t.Errorf("引擎故障下检索应 0 次，实际 %d", rag3.calls)
	}
	var sysMsgs, aiMsgs3 int64
	database3.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", res3.SessionID, "system").Count(&sysMsgs)
	database3.Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", res3.SessionID, "ai").Count(&aiMsgs3)
	if sysMsgs != 1 || aiMsgs3 != 0 {
		t.Errorf("L3 轮应恰 1 条系统消息 0 条 AI 外发，实际 sys=%d ai=%d", sysMsgs, aiMsgs3)
	}
}

// errCs03Fault 引擎故障注入的固定错误。
var errCs03Fault = fmt.Errorf("cs03: engine fault injected")

// TestSmartCS03_LowConfidenceTransfer_OperatorPins 置信度判据比较符钉死（纯函数）：
// shouldTransferOnLowConfidence 的判据是 `conf >= threshold → false`（等于不转），
// band 为空回退旧口径（低于即转），review/fallback 档不转、handoff 档转。
func TestSmartCS03_LowConfidenceTransfer_OperatorPins(t *testing.T) {
	const threshold = 0.7
	if shouldTransferOnLowConfidence("", threshold, threshold) {
		t.Error("conf == threshold 不得转人工（判据 `>=` 的等号语义）")
	}
	if !shouldTransferOnLowConfidence("", 0.6999, threshold) {
		t.Error("conf < threshold 且 band 为空应转人工（旧口径回退）")
	}
	if shouldTransferOnLowConfidence(dto.BandReview, 0.6999, threshold) {
		t.Error("review 档语义是先别急着转，不得判转人工")
	}
	if !shouldTransferOnLowConfidence(dto.BandHandoff, 0.6999, threshold) {
		t.Error("handoff 档应转人工")
	}
	if shouldTransferOnLowConfidence(dto.BandHandoff, threshold, threshold) {
		t.Error("conf == threshold 时任何档都不得转人工")
	}
}

// —— R10 边界声明（照抄口径，防止被当模型效果证明）：———————————————
// 本文件的断言对象是「检索贯通、权限/发布过滤、缓存语义、降级来源」这些**确定性契约**；
// 回答精准性与人工金标评测归 R10，不以本文件任何一格通过宣称模型效果全面达标。

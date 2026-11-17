package service

import (
	"context"
	"strconv"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestFAQPolicy_OrchestratorHandlesHandoffBeforeFastAnswer(t *testing.T) {
	_, orch, _ := c01SetupAI(t, &fakeRAGSearcher{}, fakeIntentRecognizer{})
	enabled, agentID := true, uint(99)
	question := "介绍项目，转人工客服"
	orch.engine.SetFAQService(newFAQServiceForFastPath([]model.FAQEntry{{ID: 99, AgentID: &agentID, Enabled: &enabled,
		Question: question, Answer: "不应发出的产品介绍", Keywords: []string{"介绍", "项目", "人工", "客服"}, Confidence: 0.99}}))
	result, err := orch.HandleIncomingWithAgent(tenant.WithTenantID(context.Background(), c01TenantA), &IncomingContext{
		Platform: "douyin", AccountID: "faq-handoff-account", SenderID: "faq-handoff-customer", Content: question, MessageID: "faq-handoff-message",
	}, &AgentContext{AgentID: agentID})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Transferred || result.AIReplied || result.TransferNotice == "" {
		t.Fatal("显式人工诉求必须先处理，不能被FAQ整问命中盖掉")
	}
}

func TestFAQPolicy_OrchestratorDoesNotReplayAlreadyGeneratedStandardAnswer(t *testing.T) {
	intent := &resultExitIntent{run: func(context.Context, string) (*dto.RecognizeResult, error) {
		return &dto.RecognizeResult{IntentType: IntentGreeting, Confidence: 0.99}, nil
	}}
	_, orch, database := c01SetupAI(t, newC01RAGHit(), intent)
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)
	in := &IncomingContext{Platform: "douyin", AccountID: "faq-followup-account", SenderID: "faq-followup-customer", Content: "介绍下项目", MessageID: "faq-followup-message"}
	session, err := orch.findOrCreateSession(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(&model.SessionMessage{TenantID: c01TenantA, SessionID: session.SessionID, SenderType: "ai", SenderID: "ai_assistant", Content: "已生成过的标准介绍"}).Error; err != nil {
		t.Fatal(err)
	}
	enabled, agentID := true, uint(99)
	orch.engine.SetFAQService(newFAQServiceForFastPath([]model.FAQEntry{{ID: 99, AgentID: &agentID, Enabled: &enabled,
		Question: "HiveMTK是什么？", Answer: "已生成过的标准介绍", Keywords: []string{"介绍", "项目"}, Confidence: 0.99}}))
	result, err := orch.HandleIncomingWithAgent(ctx, in, &AgentContext{AgentID: agentID, MaxTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	if intent.calls != 1 {
		t.Fatal("有既往答案的客户新消息应进入完整链，不先重复固定话术")
	}
	if result == nil || result.Reply == "已生成过的标准介绍" {
		t.Fatal("不应把已答话术再次当本轮针对性回答")
	}
}

type faqPolicyCountingEmbedder struct {
	cs03DetEmbedder
	calls int
}

func (e *faqPolicyCountingEmbedder) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	e.calls++
	return e.cs03DetEmbedder.EmbedOne(ctx, cfg, text)
}

func TestFAQPolicy_VectorCacheHonorsGenerationContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noFast   bool
		prior    bool
		cacheHit bool
	}{
		{name: "新会话正常缓存命中", cacheHit: true},
		{name: "显式重生成旁路缓存", noFast: true},
		{name: "已答追问旁路缓存", prior: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := cs03NewCacheFixture(t, cs03DetEmbedder{}, 0.9)
			embedder := &faqPolicyCountingEmbedder{}
			fixture.orch.faqEmbedder = embedder
			ctx := context.Background()
			in := &IncomingContext{Platform: model.PlatformDouyin, AccountID: cs03AccA,
				SenderID: cs03CustA, Content: cs03Question, MessageID: "faq-vector-context", NoFastpath: tc.noFast}
			if !faqFastQuestionSafe(in.Content) {
				t.Fatal("正常缓存对照必须是无人工/安全诉求的完整问题")
			}
			if err := fixture.db.AutoMigrate(&model.AgentKBBinding{}); err != nil {
				t.Fatal(err)
			}
			agent := &AgentContext{AgentID: fixture.agentID, EnableRAG: true}
			if kb := fixture.orch.resolveFAQKB(ctx, agent); kb == nil || kb.ID != fixture.kb.ID {
				t.Fatal("正常缓存对照必须真实解析到准备的知识库")
			}
			session, err := fixture.orch.findOrCreateSession(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.orch.faqHasPriorAnswer(ctx, in, session) {
				t.Fatalf("新会话不应已有答复或缺范围：tenant=%d platform=%s account=%s", session.TenantID, session.Platform, session.AccountID)
			}
			in.SessionID = session.SessionID
			cacheNamespace := fixture.orch.answerCacheNamespace(ctx, in, session, agent, KBAnswerVersionFor(&fixture.kb, in.OneID))
			if cacheNamespace == "" {
				t.Fatal("正常对照必须先具备完整配置范围，再单独验证已答或重生成旁路")
			}
			if tc.prior {
				if err := fixture.db.Create(&model.SessionMessage{SessionID: session.SessionID,
					SenderType: "ai", SenderID: "ai_assistant", Content: "上一轮已经生成过的答案"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			vec, err := (cs03DetEmbedder{}).EmbedOne(ctx, nil, in.Content)
			if err != nil {
				t.Fatal(err)
			}
			const cached = "缓存标准答案，不应重放给追问客户。"
			if err := fixture.orch.faqCache.Store(ctx, ragcache.StoreRequest{
				KBID: strconv.FormatUint(uint64(fixture.kb.ID), 10), PromptVersion: cacheNamespace,
				QueryVector: vec, Answer: cached, FromKnowledgeBase: true,
			}); err != nil {
				t.Fatal(err)
			}
			if rows := fixture.cacheRows(t); rows != 1 {
				t.Fatalf("正常缓存对照必须已真实落库：实际=%d", rows)
			}
			result, err := fixture.orch.HandleIncomingWithAgent(ctx, in, agent)
			if err != nil || result == nil {
				t.Fatalf("编排应产生明确结果：%v", err)
			}
			if tc.cacheHit {
				if embedder.calls != 1 || result.Reply != cached {
					t.Fatal("正常新会话必须真的走向量服务和已有缓存，不能用空挂载伪造旁路测试")
				}
			} else if embedder.calls != 0 || result.Reply == cached {
				t.Fatal("重生成及已答追问不得再向量化/查旧标准答案")
			}
		})
	}
}

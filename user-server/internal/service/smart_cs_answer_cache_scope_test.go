package service

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestAnswerCacheScope_ProductionCannotBorrowLegacyOrOtherConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		legacy  bool
		mutate  func(*SmartCSOrchestrator, *AgentContext)
		wantHit bool
	}{
		{name: "正常纯知识配置命中新摘要", wantHit: true},
		{name: "旧未限定缓存不能借用", legacy: true},
		{name: "提示变化不能借旧答案", legacy: true, mutate: func(_ *SmartCSOrchestrator, a *AgentContext) { a.Persona = "新的回答规范" }},
		{name: "运行时工具不得绕过", legacy: true, mutate: func(o *SmartCSOrchestrator, _ *AgentContext) {
			o.engine.SetToolExecutor(context.Background(), answerScopeTools{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
			ctx := context.Background()
			in := &IncomingContext{Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: "scope-new-customer", Content: cs03Question, MessageID: "scope-pipeline"}
			agent := &AgentContext{AgentID: f.agentID, EnableRAG: true}
			session, err := f.orch.findOrCreateSession(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			in.SessionID = session.SessionID
			key := f.orch.answerCacheNamespace(ctx, in, session, agent, KBAnswerVersionFor(&f.kb, in.OneID))
			if key == "" {
				t.Fatal("正常对照必须具备完整配置证明")
			}
			if tc.legacy {
				key = faqPromptVersion
			}
			vec, err := (cs03DetEmbedder{}).EmbedOne(ctx, nil, in.Content)
			if err != nil {
				t.Fatal(err)
			}
			const cached = "只属于已确认配置的知识答案。"
			if err := f.orch.faqCache.Store(ctx, ragcache.StoreRequest{KBID: strconv.FormatUint(uint64(f.kb.ID), 10), PromptVersion: key, QueryVector: vec, Answer: cached, FromKnowledgeBase: true}); err != nil {
				t.Fatal(err)
			}
			if f.cacheRows(t) != 1 {
				t.Fatal("缓存对照必须真实落库")
			}
			if tc.mutate != nil {
				tc.mutate(f.orch, agent)
			}
			result, err := f.orch.HandleIncomingWithAgent(ctx, in, agent)
			if err != nil || result == nil {
				t.Fatalf("编排失败：%v", err)
			}
			if (result.Reply == cached) != tc.wantHit {
				t.Fatalf("缓存范围未落实到真实编排出口：命中=%v，预期=%v", result.Reply == cached, tc.wantHit)
			}
		})
	}
}

type answerScopeBlockingStore struct {
	ragcache.Store
	started  atomic.Int32
	finished atomic.Int32
	release  chan struct{}
}

func (s *answerScopeBlockingStore) Put(ctx context.Context, _ *ragcache.Entry) error {
	s.started.Add(1)
	defer s.finished.Add(1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		return nil
	}
}

func TestAnswerCacheScope_BackgroundWritesAreBounded(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
	f.rag.chunks = []dto.RAGChunk{{Content: "可核实的纯知识事实。", Score: .9, DocID: "scope-doc"}}
	store := &answerScopeBlockingStore{Store: ragcache.NewPGAnswerCacheStore(f.db), release: make(chan struct{})}
	f.orch.faqCache = ragcache.NewFAQAnswerCacheService(store, ragcache.NewPGKBMetaReader(f.db), 0, 0)
	defer func() {
		close(store.release)
		cs03WaitFor(t, "后台写入必须结束再关闭测试库", func() bool { return store.finished.Load() == store.started.Load() })
	}()
	for index := 0; index < 6; index++ {
		result := f.sendForCustomer(t, "scope-bounded-"+strconv.Itoa(index), "scope-bounded-customer-"+strconv.Itoa(index))
		if !result.AIReplied {
			t.Fatal("存储堵塞不能阻断正常回复")
		}
		if index < 4 {
			cs03WaitFor(t, "正常容量应真实开始写入", func() bool { return store.started.Load() >= int32(index+1) })
		}
	}
	// 有限观察窗用于发现不允许启动的第五次写入；不增加生产或旧测试预算。
	select {
	case <-time.After(100 * time.Millisecond):
	}
	if count := store.started.Load(); count > 4 {
		t.Fatalf("后台写入必须有固定并发上限：实际=%d", count)
	}
}

type answerScopeGreeting struct{ fakeIntentRecognizer }

func (answerScopeGreeting) PreviewIntent(context.Context, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentGreeting, Confidence: .99}, nil
}

func (answerScopeGreeting) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentGreeting, Confidence: .99}, nil
}

func TestAnswerCacheScope_StoreUsesActualThresholdAndStableGeneration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threshold float64
		change    bool
		wantRows  int64
	}{
		{name: "正常纯知识可以写入", threshold: .7, wantRows: 1},
		{name: "代理更高阈值不能沿用全局门槛", threshold: .95},
		{name: "生成期间配置变化不能回填旧答案", threshold: .7, change: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
			f.rag.chunks = []dto.RAGChunk{{Content: "明确的纯知识事实。", Score: .9, DocID: "doc-scope", ChunkID: "chunk-scope"}}
			// 正常回复出口不因低置信转人工而提前返回，才能测到写入门槛。
			f.orch.engine.intent = answerScopeGreeting{}
			cacheCreated := make(chan struct{}, 1)
			callbackName := "test:answer-scope-cache-created"
			if err := f.db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table == "rag_answer_cache" {
					select {
					case cacheCreated <- struct{}{}:
					default:
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callbackName) })
			agent := &AgentContext{AgentID: f.agentID, EnableRAG: true, ConfidenceThreshold: tc.threshold}
			if tc.change {
				f.orch.engine.ragSearcher = &cs03CountingRAG{inner: f.rag, onCall: func() { agent.Persona = "生成期间发生的配置更新" }}
			}
			result, err := f.orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: "scope-store-customer", Content: cs03Question, MessageID: "scope-store"}, agent)
			if err != nil || result == nil || !result.AIReplied {
				t.Fatalf("对照必须确实进入正常回复出口：%v", err)
			}
			if tc.wantRows > 0 {
				cs03WaitFor(t, "正常纯知识结果应缓存", func() bool { return f.cacheRows(t) == tc.wantRows })
			} else {
				select {
				case <-cacheCreated:
					t.Fatal("不合格结果不能进入缓存写入")
				case <-time.After(100 * time.Millisecond):
				}
			}
		})
	}
}

type answerScopeFaultStore struct {
	ragcache.Store
	mode  string
	calls atomic.Int32
}

func (s *answerScopeFaultStore) Put(ctx context.Context, _ *ragcache.Entry) error {
	s.calls.Add(1)
	if _, bounded := ctx.Deadline(); !bounded {
		return errors.New("后台存储缺少截止时间")
	}
	switch s.mode {
	case "panic":
		panic("合成存储异常")
	case "timeout":
		<-ctx.Done()
		return ctx.Err()
	default:
		return errors.New("合成存储故障")
	}
}

func TestAnswerCacheScope_WriteFaultDoesNotChangeNormalAnswer(t *testing.T) {
	for _, mode := range []string{"error", "panic", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
			f.rag.chunks = []dto.RAGChunk{{Content: "可核实的纯知识事实。", Score: .9}}
			store := &answerScopeFaultStore{Store: ragcache.NewPGAnswerCacheStore(f.db), mode: mode}
			f.orch.faqCache = ragcache.NewFAQAnswerCacheService(store, ragcache.NewPGKBMetaReader(f.db), 0, 0)
			result := f.send(t, "scope-fault-"+mode)
			if result == nil || !result.AIReplied || result.Transferred || result.Reply == "" {
				t.Fatal("可选存储失败不得改变正常回答或伪造转人工")
			}
			cs03WaitFor(t, "故障必须留计数并释放后台容量", func() bool {
				stats := f.orch.AnswerCacheWriteStats()
				return stats.Failed == 1 && stats.InFlight == 0
			})
			if stats := f.orch.AnswerCacheWriteStats(); stats.Completed != 0 || stats.Dropped != 0 || store.calls.Load() != 1 {
				t.Fatalf("故障不能冒充成功或无限重试：%+v，调用=%d", stats, store.calls.Load())
			}
		})
	}
}

func TestAnswerCacheScope_UnaccountedAnswerMustNotBeCached(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
	f.rag.chunks = []dto.RAGChunk{{Content: "可核实的纯知识事实。", Score: .9}}
	store := &answerScopeFaultStore{Store: ragcache.NewPGAnswerCacheStore(f.db), mode: "error"}
	f.orch.faqCache = ragcache.NewFAQAnswerCacheService(store, ragcache.NewPGKBMetaReader(f.db), 0, 0)
	const callbackName = "test:answer-scope-ledger-fault"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "session_messages" {
			if message, ok := tx.Statement.Dest.(*model.SessionMessage); ok && message.SenderType == "ai" {
				tx.AddError(errors.New("合成答案账本持久化故障"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callbackName) })
	result, err := f.orch.HandleIncomingWithAgent(context.Background(), &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: "scope-unaccounted",
		Content: cs03Question, MessageID: "scope-unaccounted",
	}, &AgentContext{AgentID: f.agentID, EnableRAG: true})
	if err == nil || result != nil {
		t.Fatal("答案账本失败必须明确失败，不能宣称回答已受理")
	}
	if store.calls.Load() != 0 || f.orch.AnswerCacheWriteStats() != (AnswerCacheWriteStats{}) {
		t.Fatal("尚未记账的答案不得提交可选缓存")
	}
}

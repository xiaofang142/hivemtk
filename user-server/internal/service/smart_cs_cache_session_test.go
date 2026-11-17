package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestCacheSession_AutomaticHitRequiresAtomicAccounting(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sessionTenant uint
		human         bool
		exhausted     bool
		failUpdate    bool
		wantHit       bool
	}{
		{name: "同租户正常命中并记账", sessionTenant: 18, wantHit: true},
		{name: "会话不存在不能交付缓存"},
		{name: "其他租户会话不能借用", sessionTenant: 19},
		{name: "人工会话不能被缓存抢回", sessionTenant: 18, human: true},
		{name: "连续回复预算耗尽", sessionTenant: 18, exhausted: true},
		{name: "更新故障回滚答案与计数", sessionTenant: 18, failUpdate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := tenant.CurrentMode()
			tenant.SetMode(tenant.ModeOn)
			t.Cleanup(func() { tenant.SetMode(previous) })
			database := testutil.NewTestDB(t, &model.CustomerSession{}, &model.SessionMessage{}, &model.KnowledgeBase{}, &model.RAGAnswerCache{})
			ctx := tenant.OnlyTenant(context.Background(), 18)
			kb := &model.KnowledgeBase{TenantID: 18, KBCode: "cache-session-proof", Name: "合成缓存知识", Type: model.KnowledgeBaseTypeFAQ}
			if err := database.WithContext(ctx).Create(kb).Error; err != nil {
				t.Fatal(err)
			}
			kbID := itoaUint64(uint64(kb.ID))
			cacheSvc := ragcache.NewFAQAnswerCacheService(ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0, 0)
			const answer = "合成知识答案：数据保留在企业服务器。"
			if err := cacheSvc.Store(ctx, ragcache.StoreRequest{KBID: kbID, PromptVersion: faqPromptVersion, QueryVector: vec1024Unit(), Answer: answer, FromKnowledgeBase: true}); err != nil {
				t.Fatal(err)
			}
			if tc.sessionTenant != 0 {
				session := &model.CustomerSession{TenantID: tc.sessionTenant, SessionID: "cache-session-proof", Platform: model.PlatformDouyin, AccountID: "cache-account", UserID: "cache-customer", HandlerType: model.HandlerTypeAI}
				if tc.human {
					session.HandlerType = model.HandlerTypeHuman
				}
				if tc.exhausted {
					session.AIReplyCount = 2
				}
				if err := database.WithContext(tenant.OnlyTenant(context.Background(), tc.sessionTenant)).Create(session).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.failUpdate {
				if err := database.Callback().Update().Before("gorm:update").Register("cache_session_test_fault", func(tx *gorm.DB) {
					if tx.Statement.Table == "customer_sessions" {
						tx.AddError(errors.New("合成会话更新故障"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { database.Callback().Update().Remove("cache_session_test_fault") })
			}
			orch := NewSmartCSOrchestrator(nil, &OrchestratorConfig{EnableAutoReply: true, ConfidenceThreshold: .7, MaxAIConsecutive: 2}, nil)
			orch.SetFAQAnswerCache(cacheSvc, llm.NewEmbeddingService())
			result := &HandleResult{SessionID: "cache-session-proof"}
			got, hit := orch.lookupFAQAnswerCache(ctx, kbID, faqPromptVersion, vec1024Unit(), result)
			if hit != tc.wantHit {
				t.Fatalf("缓存成功必须以完整记账为前提：实际=%v 预期=%v", hit, tc.wantHit)
			}
			if !tc.wantHit && (result.AIReplied || result.Reply != "" || got != nil) {
				t.Fatal("失败缓存出口不得污染正常生成结果")
			}
			var messages int64
			if err := database.WithContext(ctx).Model(&model.SessionMessage{}).Where("session_id = ? AND sender_type = ?", result.SessionID, "ai").Count(&messages).Error; err != nil {
				t.Fatal(err)
			}
			if tc.wantHit {
				var session model.CustomerSession
				if err := database.WithContext(ctx).Where("session_id = ?", result.SessionID).First(&session).Error; err != nil {
					t.Fatal(err)
				}
				if messages != 1 || session.AIReplyCount != 1 || session.MessageCount != 1 || session.LastMessage != answer || session.HandlerType != model.HandlerTypeAI || session.LastMessageAt == nil || time.Since(*session.LastMessageAt) > time.Minute {
					t.Fatal("正常缓存必须在同一事务中留痕、计数及更新最后消息")
				}
			} else if messages != 0 {
				t.Fatal("被拒绝或失败的缓存不得遗留半条答案")
			}
		})
	}
}

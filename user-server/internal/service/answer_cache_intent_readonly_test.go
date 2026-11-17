package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

func TestAnswerCacheIntent_NamespaceDoesNotRecordBusinessIntent(t *testing.T) {
	f := cs03NewCacheFixture(t, cs03DetEmbedder{}, .9)
	if err := f.db.AutoMigrate(&model.IntentRecord{}, &model.IntentExample{}); err != nil {
		t.Fatal(err)
	}
	recognizer := NewIntentRecognizer(nil, nil)
	f.orch.engine.intent = recognizer
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	in := &IncomingContext{Platform: model.PlatformDouyin, AccountID: cs03AccA, SenderID: cs03CustA, Content: "这个多少钱？"}
	session := &model.CustomerSession{TenantID: tenant.DefaultTenantID, SessionID: "cache-readonly-session",
		Platform: in.Platform, AccountID: in.AccountID, UserID: in.SenderID}
	agent := &AgentContext{AgentID: f.agentID, EnableRAG: true}
	var writes atomic.Int32
	written := make(chan struct{}, 8)
	callback := "test.cache.readonly.intent"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callback, func(database *gorm.DB) {
		if database.Statement.Table == "intent_records" {
			writes.Add(1)
			written <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callback) })
	for index := 0; index < 2; index++ {
		if key := f.orch.answerCacheNamespace(ctx, in, session, agent, "v1"); key == "" {
			t.Fatal("规则可确认的正常知识预判必须保留缓存资格")
		}
	}
	select {
	case <-written:
		t.Error("仅确认缓存资格不应写入意图业务记录")
	case <-time.After(100 * time.Millisecond):
	}
	if writes.Load() != 0 {
		t.Errorf("缓存资格产生了%d次业务写入", writes.Load())
	}
	before := writes.Load()
	if result, err := recognizer.Recognize(ctx, session.SessionID, session.UserID, in.Content); err != nil || result == nil {
		t.Fatalf("正常业务识别不能被改成全拒绝：%v", err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for writes.Load() == before {
		select {
		case <-written:
		case <-deadline.C:
			t.Fatal("正常Recognize必须仍有业务记录正向对照")
		}
	}
}

type cacheIntentWithoutPreview struct{ calls int }

func (r *cacheIntentWithoutPreview) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	r.calls++
	return &dto.RecognizeResult{IntentType: IntentPriceInquiry, Confidence: .99}, nil
}

func TestAnswerCacheIntent_UnknownCapabilityCannotCallBusinessRecognizer(t *testing.T) {
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	intent := &cacheIntentWithoutPreview{}
	engine := &SalesEngine{intent: intent}
	if engine.answerCacheIntentStable(ctx, &SalesRequest{UserMessage: "这个多少钱？"}, &model.DialogueMemory{}) {
		t.Error("未声明只读能力的识别器不能用于缓存资格")
	}
	if intent.calls != 0 {
		t.Fatal("缓存资格不应调用未知能力的业务识别方法")
	}
}

func TestAnswerCacheIntent_PreviewRequiresConfirmedScope(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	recognizer := &IntentRecognizer{}
	ctx := tenant.OnlyTenant(context.Background(), 18)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺上下文"},
		{name: "缺租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(ctx)},
		{name: "已取消", ctx: canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result, err := recognizer.PreviewIntent(tc.ctx, "这个多少钱？"); err == nil || result != nil {
				t.Fatal("未确认上下文不能产生缓存预判许可")
			}
		})
	}
}

func TestAnswerCacheIntent_UnknownRuleDoesNotEnableCache(t *testing.T) {
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	recognizer := &IntentRecognizer{}
	result, err := recognizer.PreviewIntent(ctx, "zqxv_unclassified_839271")
	if err != nil || result == nil || result.IntentType != IntentUnknown {
		t.Fatalf("缺少规则证据应返回unknown，不启动业务识别：%v", err)
	}
	engine := &SalesEngine{intent: recognizer}
	if engine.answerCacheIntentStable(ctx, &SalesRequest{UserMessage: "zqxv_unclassified_839271"}, &model.DialogueMemory{}) {
		t.Fatal("未知预判不能放行最终答案缓存")
	}
}

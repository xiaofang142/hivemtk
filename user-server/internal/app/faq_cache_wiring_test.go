package app

import (
	"context"
	"testing"
	"time"

	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// newFAQCacheFixture 建一次测试库（参数表 + 答案缓存表）、灌入种子参数，并把参数中心
// 换成"真读库"的实例；返回的 *gorm.DB 就是装配点要的那个句柄。
//
// 只能调一次 NewTestDB：它每次都会先 DropTable 再 AutoMigrate，第二次调用会把第一次
// 灌进去的种子参数行一起抹掉（实测报 record not found）。
//
// t.Cleanup 还原全局：globalConfigParam 是本进程共享的包级全局，留着带 DB 的实例会把
// 同包后续用例的 Get* 一并带走（它们期望的是"没建过参数表 ⇒ 恒回落"那个形状）。
func newFAQCacheFixture(t *testing.T) (*service.ConfigParamService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{}, &ragcache.RAGAnswerCache{})
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	return svc, database
}

// TestReadFAQAnswerCacheConfig_WithoutParamTableIsOff 参数表还没建出来时的回落形状。
//
// 装配早于 SeedConfigParams（或实例根本没跑过迁移）时，GlobalConfigParam() 回的是无 DB 的
// stub，三个值都落在 fallback 上 —— 开关的 fallback 是 false，所以那种进程一定不挂缓存。
// 这一格是下面"翻开关才挂"的正控制：它证明 true 那一格读到的不是同一个回落值。
func TestReadFAQAnswerCacheConfig_WithoutParamTableIsOff(t *testing.T) {
	service.SetGlobalForTest(nil)
	cfg := readFAQAnswerCacheConfig(context.Background())
	if cfg.enabled {
		t.Error("参数读不到时开关必须回落为 false（不挂缓存）")
	}
	if cfg.threshold != ragcache.DefaultSemanticThreshold {
		t.Errorf("阈值回落值 = %v，期望 %v（与构造函数同一条契约）", cfg.threshold, ragcache.DefaultSemanticThreshold)
	}
	if cfg.ttl != faqAnswerCacheDefaultTTL {
		t.Errorf("TTL 回落值 = %v，期望 %v", cfg.ttl, faqAnswerCacheDefaultTTL)
	}
}

// TestFAQAnswerCacheSeedDefaults 种子表里这三格的缺省值就是"挂载形状"的口径来源，
// 改任何一格都要先看这里：开关翻真、ttl 与装配回落同值、阈值落在契约允许的那一侧。
func TestFAQAnswerCacheSeedDefaults(t *testing.T) {
	svc, _ := newFAQCacheFixture(t)
	ctx := context.Background()
	if err := svc.UpdateValue(ctx, "cache", "faq_answer_enabled", "false", 1); err != nil {
		t.Fatalf("显式置关失败：%v", err)
	}
	cfg := readFAQAnswerCacheConfig(ctx)
	if cfg.enabled {
		t.Error("开关为 false 时不该挂：缓存命中会改客户收到的回复，默认停在改动前那一侧")
	}
	if cfg.ttl != faqAnswerCacheDefaultTTL {
		t.Errorf("cache.faq_ttl 缺省 %v，装配回落 %v：两处不同口径，改了参数中心也不等于生效值", cfg.ttl, faqAnswerCacheDefaultTTL)
	}
	if cfg.threshold != ragcache.DefaultSemanticThreshold {
		t.Errorf("agent_llm.default_semantic_threshold 缺省 %v，构造契约下限 %v", cfg.threshold, ragcache.DefaultSemanticThreshold)
	}
}

// TestAttachFAQAnswerCache_FollowsTheSwitch 开关是唯一决定挂不挂的东西：
// 关着时 attach 交回 false（编排器那两个字段保持零值 nil，读/写两段缓存分支都不进），
// 翻开后交回 true，且参数中心的阈值与 TTL 确实是被读走的那一份。
func TestAttachFAQAnswerCache_FollowsTheSwitch(t *testing.T) {
	svc, database := newFAQCacheFixture(t)
	ctx := context.Background()
	o := newBareOrchestrator(t)

	if attachFAQAnswerCache(o, database) {
		t.Fatal("开关关着（种子缺省 false）时不该挂缓存")
	}

	if err := svc.UpdateValue(ctx, "cache", "faq_answer_enabled", "true", 1); err != nil {
		t.Fatalf("翻开开关失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "cache", "faq_ttl", "3600", 1); err != nil {
		t.Fatalf("改 TTL 失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "agent_llm", "default_semantic_threshold", "0.97", 1); err != nil {
		t.Fatalf("改阈值失败：%v", err)
	}
	if !attachFAQAnswerCache(o, database) {
		t.Fatal("翻开开关后 attach 应返回 true（本装配点就是唯一注入口）")
	}
	if cfg := readFAQAnswerCacheConfig(ctx); !cfg.enabled || cfg.ttl != time.Hour || cfg.threshold != 0.97 {
		t.Fatalf("参数没被读走：enabled=%v ttl=%v threshold=%v", cfg.enabled, cfg.ttl, cfg.threshold)
	}
}

// TestAttachFAQAnswerCache_NilGuards 没有编排器或没有 DB 句柄时什么都不挂。
func TestAttachFAQAnswerCache_NilGuards(t *testing.T) {
	svc, database := newFAQCacheFixture(t)
	ctx := context.Background()
	if err := svc.UpdateValue(ctx, "cache", "faq_answer_enabled", "true", 1); err != nil {
		t.Fatalf("翻开开关失败：%v", err)
	}
	if attachFAQAnswerCache(nil, database) {
		t.Error("编排器为 nil 时不该返回挂载成功")
	}
	if attachFAQAnswerCache(newBareOrchestrator(t), nil) {
		t.Error("DB 句柄为 nil 时不该返回挂载成功（没有存储就别承诺缓存）")
	}
}

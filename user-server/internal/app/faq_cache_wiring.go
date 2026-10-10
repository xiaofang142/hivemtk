package app

import (
	"context"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// 答案缓存的三个参数中心键。写成常量而不是散在字符串字面量里，是为了让
// scripts/check-config-param-readpoints.py 能把"读取点"和种子表里的条目对上：
// 该门只认同一行里既有 `Get*(` 又出现键名（字面量，或本文件这样的标识符）这一种形状。
const (
	faqAnswerCacheEnabledParam   = "faq_answer_enabled"
	faqAnswerCacheTTLParam       = "faq_ttl"
	faqAnswerCacheThresholdParam = "default_semantic_threshold"
)

// faqAnswerCacheDefaultTTL 是参数读不到时的回落值，与种子表里 cache.faq_ttl 的
// DefaultValue（300 秒）同口径；两处不一致时以种子为准，回落只是防装配早于建参数表。
const faqAnswerCacheDefaultTTL = 300 * time.Second

// faqAnswerCacheConfig 挂载答案缓存要的三个值，一次读齐。
type faqAnswerCacheConfig struct {
	enabled   bool
	threshold float64
	ttl       time.Duration
}

// readFAQAnswerCacheConfig 读参数中心里的三个答案缓存参数。
//
// 单列出来是为了让"翻开关"这件事能被测到：GlobalConfigParam() 在没建过参数表的进程里
// 回落到 fallback 值（开关恒 false），所以装配早于 SeedConfigParams 也不会挂上半个缓存。
func readFAQAnswerCacheConfig(ctx context.Context) faqAnswerCacheConfig {
	cp := service.GlobalConfigParam()
	return faqAnswerCacheConfig{
		enabled:   cp.GetBool(ctx, "cache", faqAnswerCacheEnabledParam, false),
		threshold: cp.GetFloat(ctx, "agent_llm", faqAnswerCacheThresholdParam, ragcache.DefaultSemanticThreshold),
		ttl:       cp.GetDuration(ctx, "cache", faqAnswerCacheTTLParam, faqAnswerCacheDefaultTTL),
	}
}

// attachFAQAnswerCache 把语义答案缓存挂到编排器上：pgvector 存储、知识库更新时间读取器、
// 缓存服务、查询向量化服务四样一起给。返回是否真的挂了。
//
// 调用方：BuildSmartOrchestrator（本包内唯一的编排器构造点，router.Setup 调它）。
//
// 这条竖的零件早就齐了（表已进 allModels()、Store/KBMetaReader/Service 都有实现和用例），
// 缺的只有装配：过去唯一的注入口是包级全局 service.SetGlobalFAQAnswerCache，实测全仓（含测试）
// 零调用方，于是编排器恒拿 nil、三层查询（精确 / 语义 / 回源）在生产里从没跑过。这里把全局那对
// 变量删了，换成显式 setter + 本装配点，与其它几个生产者（订单草稿、人工待办、坏例留痕）同一分层
// 口径：拿不拿得到 DB 句柄、读不读参数是装配层的事，编排器只在有人给它时才会用到。
//
// 为什么留一个默认 false 的参数开关，而不是无条件打开：命中即直接返回缓存答案，等于把客户收到的
// 回复从"每次现生成"改成"语义相近就复用"，且每条入会话多一次 embedding 调用。这个决定归运营，
// 归代码的只有"翻一下就生效、不需要有人再写一遍构造代码"。开关开着的实例里仍在场的既有保护：
//   - 相似度阈值走 agent_llm.default_semantic_threshold（构造契约只允许比 0.95 更紧，松的值被抬回）；
//   - 写入侧要求本次回答真的取自知识库，且 RAG top1 分数过编排器的置信度阈值；
//   - 读取侧条目超过 cache.faq_ttl 即删该行并按未命中处理；知识库 updated_at 晚于入缓存时刻同样失效。
func attachFAQAnswerCache(o *service.SmartCSOrchestrator, gormDB *gorm.DB) bool {
	if o == nil || gormDB == nil {
		return false
	}
	cfg := readFAQAnswerCacheConfig(context.Background())
	if !cfg.enabled {
		return false
	}
	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(gormDB),
		ragcache.NewPGKBMetaReader(gormDB),
		cfg.threshold,
		cfg.ttl,
	)
	o.SetFAQAnswerCache(cacheSvc, llm.NewEmbeddingService())
	logger.Infof("[agent] ✅ 语义答案缓存已挂载（threshold=%.2f ttl=%s，表 rag_answer_cache）", cfg.threshold, cfg.ttl)
	return true
}

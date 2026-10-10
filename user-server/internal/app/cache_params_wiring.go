package app

import (
	"context"
	"time"

	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/system/install"
)

// WireCacheConfigParams 把 config_params 的 cache 组接进内存缓存、安装 memo、
// 平台信息缓存与翻译缓存四个点位。
//
// 接线理由同 misc / confidence / agent_llm 三批：这四个点位分别住在 internal/cache、
// internal/system/install、internal/controller、internal/aiagent/rag/retrieval 四个包里，
// 没有一个能反向 import service，只有装配层同时看得见它们。
//
// 分两类来源：
//   - cache.max_keys 的 seam（cache.SetMaxKeysProvider）本来就存在于本仓，接线只是补上注入；
//   - 其余四条是本次新加的 seam。
//
// Get* 调用与键名字面量必须写在同一行：scripts/check-config-param-readpoints.py
// 认的就是这个形状，把 Get* 藏进通用 helper 会让门禁判成「未接线」。
func WireCacheConfigParams() []string {
	bg := context.Background()
	cp := service.GlobalConfigParam()
	wired := make([]string, 0, 5)

	cache.SetMaxKeysProvider(func() int {
		return cp.GetInt(bg, "cache", "max_keys", cache.DefaultMaxKeys)
	})
	wired = append(wired, "cache.max_keys")

	install.SetMemoTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "cache", "memo_ttl", install.DefaultMemoTTL)
	})
	wired = append(wired, "cache.memo_ttl")

	controller.SetPlatformCacheTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "cache", "platform_cache_ttl", controller.DefaultPlatformCacheTTL)
	})
	wired = append(wired, "cache.platform_cache_ttl")

	ragretrieval.SetTranslationCacheMaxEntriesProvider(func() int {
		return cp.GetInt(bg, "cache", "translation_cache_max_entries", ragretrieval.TranslationCacheMaxEntriesDefault)
	})
	wired = append(wired, "cache.translation_cache_max_entries")

	service.SetFaqDecayMaxBatchProvider(func() int {
		return cp.GetInt(bg, "cache", "faq_decay_max_batch", service.DefaultFaqDecayMaxBatch)
	})
	wired = append(wired, "cache.faq_decay_max_batch")

	return wired
}

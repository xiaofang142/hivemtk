package app

import (
	"context"
	"testing"
	"time"

	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/system/install"
)

// wantCacheWired 是 cache 组本轮接线的 5 条，逐条写死而不是从 DefaultParamDefs()
// 过滤——理由同前几批：过滤只能钉住"接上的都在本组内"，拼错 key 时断言照样成立，
// 真正漏接的那条没人发现。
var wantCacheWired = []string{
	"cache.max_keys",
	"cache.memo_ttl",
	"cache.platform_cache_ttl",
	"cache.translation_cache_max_entries",
	"cache.faq_decay_max_batch",
}

// resetCacheProviders 把 5 个注入点摘回未装配状态。provider 是包级变量，
// 测试之间不还原会互相污染：上一格注入的值会让下一格"未注入"的断言假绿。
func resetCacheProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		cache.SetMaxKeysProvider(nil)
		install.SetMemoTTLProvider(nil)
		controller.SetPlatformCacheTTLProvider(nil)
		ragretrieval.SetTranslationCacheMaxEntriesProvider(nil)
		service.SetFaqDecayMaxBatchProvider(nil)
	})
}

func newCacheParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetCacheProviders(t)
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no DB")
	}
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	return svc
}

func TestWireCacheConfigParamsKeys(t *testing.T) {
	resetCacheProviders(t)
	got := WireCacheConfigParams()
	if len(got) != len(wantCacheWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantCacheWired), got, wantCacheWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantCacheWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantCacheWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次：同一个点位注入两次，后一次覆盖前一次，读点会跟着错位", k)
		}
		seen[k] = true
	}
}

func TestWiredCacheKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantCacheWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// TestWireCacheConfigParamsReachesReaders 装配完之后，参数中心的值必须真的
// 出现在读取点上。只断言 provider 被 setter 收下没有意义——那证明不了
// 「运维在页面上改值真的改变了行为」。
func TestWireCacheConfigParamsReachesReaders(t *testing.T) {
	svc := newCacheParamFixture(t)
	ctx := context.Background()

	beforeMaxKeys := cache.MaxKeys()
	beforeMemo := install.MemoTTL()
	beforePlatform := controller.PlatformCacheTTL()
	beforeMaxEntries := ragretrieval.TranslationCacheMaxEntries()
	beforeDecay := service.FaqDecayMaxBatch()

	WireCacheConfigParams()

	for _, tc := range []struct{ key, val string }{
		{"max_keys", "25000"},
		{"memo_ttl", "30"},
		{"platform_cache_ttl", "90"},
		{"translation_cache_max_entries", "50000"},
		{"faq_decay_max_batch", "300"},
	} {
		if err := svc.UpdateValue(ctx, "cache", tc.key, tc.val, 1); err != nil {
			t.Fatalf("改 cache.%s 失败：%v", tc.key, err)
		}
	}

	if got, want := cache.MaxKeys(), 25000; got != want {
		t.Errorf("max_keys 未被读走：got %v（原 %v）want %v", got, beforeMaxKeys, want)
	}
	if got, want := install.MemoTTL(), 30*time.Second; got != want {
		t.Errorf("memo_ttl 未被读走：got %v（原 %v）want %v", got, beforeMemo, want)
	}
	if got, want := controller.PlatformCacheTTL(), 90*time.Second; got != want {
		t.Errorf("platform_cache_ttl 未被读走：got %v（原 %v）want %v", got, beforePlatform, want)
	}
	if got, want := ragretrieval.TranslationCacheMaxEntries(), 50000; got != want {
		t.Errorf("translation_cache_max_entries 未被读走：got %v（原 %v）want %v", got, beforeMaxEntries, want)
	}
	if got, want := service.FaqDecayMaxBatch(), 300; got != want {
		t.Errorf("faq_decay_max_batch 未被读走：got %v（原 %v）want %v", got, beforeDecay, want)
	}
}

// TestWireCacheConfigParamsWithoutDB 参数表不存在时（没跑迁移、或装配早于建表），
// 接线必须仍把各点位托在代码兜底值上，而不是返回一个 nil 函数在首次请求时炸掉。
func TestWireCacheConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetCacheProviders(t)

	if got := WireCacheConfigParams(); len(got) != len(wantCacheWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantCacheWired))
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"memo_ttl", install.MemoTTL(), install.DefaultMemoTTL},
		{"platform_cache_ttl", controller.PlatformCacheTTL(), controller.DefaultPlatformCacheTTL},
	} {
		if tc.got != tc.want {
			t.Errorf("无库时 %s = %v，期望兜底 %v", tc.name, tc.got, tc.want)
		}
	}
	if got, want := cache.MaxKeys(), cache.DefaultMaxKeys; got != want {
		t.Errorf("无库时 max_keys = %v，期望兜底 %v", got, want)
	}
	if got, want := ragretrieval.TranslationCacheMaxEntries(), ragretrieval.TranslationCacheMaxEntriesDefault; got != want {
		t.Errorf("无库时 translation_cache_max_entries = %v，期望兜底 %v", got, want)
	}
	if got, want := service.FaqDecayMaxBatch(), service.DefaultFaqDecayMaxBatch; got != want {
		t.Errorf("无库时 faq_decay_max_batch = %v，期望兜底 %v", got, want)
	}
}

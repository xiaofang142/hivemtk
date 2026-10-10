package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// newWebhookParamFixture 起一个只带 config_params 的库，seed 后把全局参数服务换成
// 指向它的实例。第二个返回值是底层 *gorm.DB，供需要绕过 service 校验的用例直接写。
func newWebhookParamFixture(t *testing.T) (*ConfigParamService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no test DB")
	}
	if err := SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := NewConfigParamService(database)
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc, database
}

func setWebhookParam(t *testing.T, svc *ConfigParamService, key, val string) {
	t.Helper()
	if err := svc.UpdateValue(context.Background(), ParamGroupWebhook, key, val, 1); err != nil {
		t.Fatalf("UpdateValue(webhook.%s=%s) 失败：%v", key, val, err)
	}
}

// setWebhookParamRaw 绕过 service 的 min/max 守卫直接写库，用于构造"库里已有坏值"。
// 守卫（0.2 修的那个口子）要防的恰是这种情形：历史数据、直接改库、绕过 service 写。
func setWebhookParamRaw(t *testing.T, db *gorm.DB, key, val string) {
	t.Helper()
	if err := repository.NewConfigParamRepository(db).UpdateValue(context.Background(),
		ParamGroupWebhook, key, val, 1); err != nil {
		t.Fatalf("直接写库 webhook.%s=%s 失败：%v", key, val, err)
	}
}

// TestWebhookParamsReadParamCenter 证明七个参数真的从库里读，且改完立即生效。
// 每请求读的四条与构造期读的三条共用同一批 raw 读口，区别只在调用时机——
// 构造期那三条由 webhookIntAfterEnv 在 NewWebhookService 里一次性结算。
func TestWebhookParamsReadParamCenter(t *testing.T) {
	svc, _ := newWebhookParamFixture(t)
	ctx := context.Background()

	if got := webhookDedupTTL(ctx); got != WebhookDedupTTL {
		t.Errorf("未改参数时应等于兜底 %v, got %v", WebhookDedupTTL, got)
	}
	setWebhookParam(t, svc, WebhookDedupTTLParam, "120")
	if got := webhookDedupTTL(ctx); got != 2*time.Minute {
		t.Errorf("改 120s 后应读到 2m, got %v", got)
	}
	// duration 也接受 Go duration 字面量（0.2 起 parseDurationSeconds 的统一口径）。
	setWebhookParam(t, svc, WebhookDedupTTLParam, "10m")
	if got := webhookDedupTTL(ctx); got != 10*time.Minute {
		t.Errorf("改 10m 后应读到 10m, got %v", got)
	}

	intCases := []struct {
		name string
		def  int
		set  string
		want int
		read func(context.Context) int
	}{
		{"rate_limit", WebhookRateLimit, "7", 7, webhookRateLimit},
		{"rate_burst", WebhookRateBurst, "11", 11, webhookRateBurst},
		{"max_retries", WebhookMaxRetries, "5", 5, webhookMaxRetries},
		{"worker_count", WebhookWorkerCount, "9", 9, webhookWorkerCount},
		{"queue_size", WebhookQueueSize, "77", 77, webhookQueueSize},
		{"reply_concurrency", WebhookReplyConcurrency, "13", 13, webhookReplyConcurrency},
	}
	for _, c := range intCases {
		if got := c.read(ctx); got != c.def {
			t.Errorf("webhook.%s 未改参数时应等于兜底 %d, got %d", c.name, c.def, got)
		}
		setWebhookParam(t, svc, webhookParamKeyByName(c.name), c.set)
		if got := c.read(ctx); got != c.want {
			t.Errorf("webhook.%s 改成 %s 后应读到 %d, got %d", c.name, c.set, c.want, got)
		}
	}
}

// webhookParamKeyByName 只给上表用：把测试用的短名映射回键名常量。
func webhookParamKeyByName(name string) string {
	switch name {
	case "rate_limit":
		return WebhookRateLimitParam
	case "rate_burst":
		return WebhookRateBurstParam
	case "max_retries":
		return WebhookMaxRetriesParam
	case "worker_count":
		return WebhookWorkerCountParam
	case "queue_size":
		return WebhookQueueSizeParam
	case "reply_concurrency":
		return WebhookReplyConcurrencyParam
	}
	return "dedup_ttl"
}

// TestWebhookParamsFallBackOnNonPositive 非正数一律回落兜底。这一组全是数量/速率
// 语义：并发 0 = 一个 worker 都不起、队列 0 = 每条消息同步处理、速率 0 = 全拒、
// burst 0 = 一条都放不进去、去重 0 = 之后所有重试都被判重复而丢掉，没有一种是
// "配了就照做"的合法取值。
func TestWebhookParamsFallBackOnNonPositive(t *testing.T) {
	svc, db := newWebhookParamFixture(t)
	ctx := context.Background()
	badInt := []string{"0", "-1", "-512"}
	for _, v := range badInt {
		for _, key := range []string{WebhookRateLimitParam, WebhookRateBurstParam,
			WebhookMaxRetriesParam, WebhookWorkerCountParam, WebhookQueueSizeParam,
			WebhookReplyConcurrencyParam} {
			setWebhookParamRaw(t, db, key, v)
		}
	}
	for _, c := range []struct {
		name string
		def  int
		read func(context.Context) int
	}{
		{"rate_limit", WebhookRateLimit, webhookRateLimit},
		{"rate_burst", WebhookRateBurst, webhookRateBurst},
		{"max_retries", WebhookMaxRetries, webhookMaxRetries},
		{"worker_count", WebhookWorkerCount, webhookWorkerCount},
		{"queue_size", WebhookQueueSize, webhookQueueSize},
		{"reply_concurrency", WebhookReplyConcurrency, webhookReplyConcurrency},
	} {
		if got := c.read(ctx); got != c.def {
			t.Errorf("webhook.%s 坏值后应回落兜底 %d, got %d", c.name, c.def, got)
		}
	}
	for _, v := range []string{"0", "-60", "-1s"} {
		setWebhookParamRaw(t, db, WebhookDedupTTLParam, v)
		if got := webhookDedupTTL(ctx); got != WebhookDedupTTL {
			t.Errorf("dedup_ttl=%s 应回落兜底 %v, got %v", v, WebhookDedupTTL, got)
		}
	}
	_ = svc
}

// TestWebhookParamsWithoutParamCenter 无库时全部回落常量兜底——装配早于 DB 就绪、
// 或参数中心整体不可用时，webhook 链路仍按历史行为跑，不能把并发/限流打成 0。
func TestWebhookParamsWithoutParamCenter(t *testing.T) {
	SetGlobalForTest(nil)
	// 三个构造期读的口子还受 env 影响，先把 env 清干净才能证明回落的是常量。
	t.Setenv("WEBHOOK_WORKER_COUNT", "")
	t.Setenv("WEBHOOK_QUEUE_SIZE", "")
	t.Setenv("WEBHOOK_REPLY_CONCURRENCY", "")
	ctx := context.Background()

	if got := webhookDedupTTL(ctx); got != WebhookDedupTTL {
		t.Errorf("无库时 dedup_ttl 应回落 %v, got %v", WebhookDedupTTL, got)
	}
	if got := webhookRateLimit(ctx); got != WebhookRateLimit {
		t.Errorf("无库时 rate_limit 应回落 %d, got %d", WebhookRateLimit, got)
	}
	if got := webhookMaxRetries(ctx); got != WebhookMaxRetries {
		t.Errorf("无库时 max_retries 应回落 %d, got %d", WebhookMaxRetries, got)
	}
	if got := webhookWorkerCount(ctx); got != WebhookWorkerCount {
		t.Errorf("无库时 worker_count 应回落 %d, got %d", WebhookWorkerCount, got)
	}
	if got := webhookQueueSize(ctx); got != WebhookQueueSize {
		t.Errorf("无库时 queue_size 应回落 %d, got %d", WebhookQueueSize, got)
	}
	if got := webhookReplyConcurrency(ctx); got != WebhookReplyConcurrency {
		t.Errorf("无库时 reply_concurrency 应回落 %d, got %d", WebhookReplyConcurrency, got)
	}
}

// TestWebhookEnvWinsOverParamCenter env 仍是最高优先，且非法 env 不短路整条链：
// 一个残留的空/乱码 env 变量不能把运维在参数中心调好的值盖掉。
func TestWebhookEnvWinsOverParamCenter(t *testing.T) {
	svc, _ := newWebhookParamFixture(t)
	ctx := context.Background()
	setWebhookParam(t, svc, WebhookWorkerCountParam, "42")

	t.Setenv("WEBHOOK_WORKER_COUNT", "7")
	if got := webhookWorkerCount(ctx); got != 7 {
		t.Errorf("env 应压过参数中心: want 7, got %d", got)
	}
	for _, bad := range []string{"", "abc", "0", "-3"} {
		t.Setenv("WEBHOOK_WORKER_COUNT", bad)
		if got := webhookWorkerCount(ctx); got != 42 {
			t.Errorf("env=%q 非法时应放行参数中心的 42, got %d", bad, got)
		}
	}
}

// TestWebhookEnvWithoutParamCenterFallsBackToConst 有 env 但库也没有时，仍是
// env > 常量，与改造前逐字节一致。
func TestWebhookEnvWithoutParamCenterFallsBackToConst(t *testing.T) {
	SetGlobalForTest(nil)
	t.Setenv("WEBHOOK_QUEUE_SIZE", "1024")
	if got := webhookQueueSize(context.Background()); got != 1024 {
		t.Errorf("want 1024, got %d", got)
	}
}

// TestWebhookGroupSeedsMatchConstantDefaults 每条种子的默认值必须等于代码兜底常量。
// 参数中心默认值的语义是「没人配置时系统现在的行为」；两者不一致等于升级即改变行为。
// 与 config_param_validate_test.go 的 TestSeedDefaultsPassTheirOwnValidation 互补：
// 那条查种子能不能过自己的 min/max，本条目查种子与代码是不是同一个数。
func TestWebhookGroupSeedsMatchConstantDefaults(t *testing.T) {
	want := map[string]struct {
		raw string
		// constSecs 仅供 duration 型：常量折算成秒后必须与种子的秒数表述一致。
		constSecs int
	}{
		WebhookDedupTTLParam:         {"300", int(WebhookDedupTTL / time.Second)},
		WebhookRateLimitParam:        {"30", 0},
		WebhookRateBurstParam:        {"60", 0},
		WebhookMaxRetriesParam:       {"3", 0},
		WebhookWorkerCountParam:      {"4", 0},
		WebhookQueueSizeParam:        {"512", 0},
		WebhookReplyConcurrencyParam: {"32", 0},
	}
	defs := DefaultParamDefs()
	index := make(map[string]ParamDef, len(want))
	for _, d := range defs {
		if d.Group == ParamGroupWebhook {
			index[d.Key] = d
		}
	}
	if len(index) != len(want) {
		t.Fatalf("webhook 组应有 %d 条种子, 实际 %d 条: %v", len(want), len(index), index)
	}
	for key, d := range index {
		w, ok := want[key]
		if !ok {
			t.Errorf("webhook 组多出一条未登记的种子: %s", key)
			continue
		}
		if d.DefaultValue != w.raw {
			t.Errorf("webhook.%s 种子默认值 %q != 代码兜底 %q", key, d.DefaultValue, w.raw)
		}
		if w.constSecs > 0 && d.DefaultValue != strconv.Itoa(w.constSecs) {
			t.Errorf("webhook.%s 种子默认值 %q != 常量折算 %ds", key, d.DefaultValue, w.constSecs)
		}
	}
}

// TestWebhookConstructionTimeParamsAreMarkedRestart 三条构造期读的参数必须标
// Restart=true，否则运维改完以为生效了、实际只对下次启动生效——这正是
// message_hub_idem_ttl 在 2c 里踩过的坑。
func TestWebhookConstructionTimeParamsAreMarkedRestart(t *testing.T) {
	want := map[string]bool{
		WebhookRateLimitParam:        false,
		WebhookRateBurstParam:        false,
		WebhookMaxRetriesParam:       false,
		WebhookDedupTTLParam:         false,
		WebhookWorkerCountParam:      true,
		WebhookQueueSizeParam:        true,
		WebhookReplyConcurrencyParam: true,
	}
	seen := 0
	for _, d := range DefaultParamDefs() {
		if d.Group != ParamGroupWebhook {
			continue
		}
		seen++
		w, ok := want[d.Key]
		if !ok {
			t.Errorf("webhook 组多出一条未登记的种子: %s", d.Key)
			continue
		}
		if d.Restart != w {
			t.Errorf("webhook.%s Restart=%v, 预期 %v", d.Key, d.Restart, w)
		}
	}
	if seen != len(want) {
		t.Fatalf("webhook 组应恰好 %d 条, 实际 %d 条", len(want), seen)
	}
}

// TestWebhookParamKeysAreAllRead 反向钉死「登记了就必须有人读」：每条键都要被某个
// 带兜底的读口接过。读取点门禁只看 Get* 调用的形状，看不出某个键是不是压根没被用上。
func TestWebhookParamKeysAreAllRead(t *testing.T) {
	svc, _ := newWebhookParamFixture(t)
	ctx := context.Background()
	// 六个 int 键的 min 都是 1，统一写 1；dedup_ttl 是 duration 且 min 10s。
	for _, k := range []string{WebhookRateLimitParam, WebhookRateBurstParam,
		WebhookMaxRetriesParam, WebhookWorkerCountParam, WebhookQueueSizeParam,
		WebhookReplyConcurrencyParam} {
		setWebhookParam(t, svc, k, "1")
	}
	setWebhookParam(t, svc, WebhookDedupTTLParam, "10")

	if got := webhookDedupTTL(ctx); got != 10*time.Second {
		t.Errorf("dedup_ttl 应读到 10s, got %v", got)
	}
	if got := webhookRateLimit(ctx); got != 1 {
		t.Errorf("rate_limit 应读到 1, got %d", got)
	}
	if got := webhookRateBurst(ctx); got != 1 {
		t.Errorf("rate_burst 应读到 1, got %d", got)
	}
	if got := webhookMaxRetries(ctx); got != 1 {
		t.Errorf("max_retries 应读到 1, got %d", got)
	}
	t.Setenv("WEBHOOK_WORKER_COUNT", "")
	t.Setenv("WEBHOOK_QUEUE_SIZE", "")
	t.Setenv("WEBHOOK_REPLY_CONCURRENCY", "")
	if got := webhookWorkerCount(ctx); got != 1 {
		t.Errorf("worker_count 应读到 1, got %d", got)
	}
	if got := webhookQueueSize(ctx); got != 1 {
		t.Errorf("queue_size 应读到 1, got %d", got)
	}
	if got := webhookReplyConcurrency(ctx); got != 1 {
		t.Errorf("reply_concurrency 应读到 1, got %d", got)
	}
}

// TestWebhookTokenBucketPicksUpConfiguredRate 行为级：令牌桶真的按配置的速率与容量放行。
func TestWebhookTokenBucketPicksUpConfiguredRate(t *testing.T) {
	svc, _ := newWebhookParamFixture(t)
	setWebhookParam(t, svc, WebhookRateBurstParam, "2")
	setWebhookParam(t, svc, WebhookRateLimitParam, "1")

	s := &WebhookService{rlBuckets: make(map[string]*tokenBucket)}
	n := 0
	for i := 0; i < 5; i++ {
		if s.allowRate(context.Background(), "k") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("burst=2 时前 5 次只应放行 2 次, got %d", n)
	}

	// 换成 burst=4 后，新 key 的桶应放行 4 次。
	setWebhookParam(t, svc, WebhookRateBurstParam, "4")
	n = 0
	for i := 0; i < 5; i++ {
		if s.allowRate(context.Background(), "other") {
			n++
		}
	}
	if n != 4 {
		t.Errorf("burst=4 时新 key 的 5 次应放行 4 次, got %d", n)
	}
}

// TestWebhookDedupUsesConfiguredTTL 行为级：去重键真的按配置的 TTL 过期。
// 用桩替换全局缓存的 SetNX 代价太大，这里只验证读取口把值传到了位——
// 过期行为由 cache 层保证（2c 的 PopAll bug 已一并修掉）。
func TestWebhookDedupUsesConfiguredTTL(t *testing.T) {
	svc, _ := newWebhookParamFixture(t)
	setWebhookParam(t, svc, WebhookDedupTTLParam, "600")
	if got := webhookDedupTTL(context.Background()); got != 10*time.Minute {
		t.Errorf("应读到 10m, got %v", got)
	}
}

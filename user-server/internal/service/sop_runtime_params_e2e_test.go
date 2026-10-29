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
	"hivemtk-user/internal/service/sop"
)

func newSOPParamFixture(t *testing.T) (*ConfigParamService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no test DB")
	}
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := NewConfigParamService()
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc, database
}

func setSOPParam(t *testing.T, svc *ConfigParamService, key, val string) {
	t.Helper()
	if err := svc.UpdateValue(context.Background(), ParamGroupSOP, key, val, 1); err != nil {
		t.Fatalf("UpdateValue(sop.%s=%s) 失败：%v", key, val, err)
	}
}

func setSOPParamRaw(t *testing.T, db *gorm.DB, key, val string) {
	t.Helper()
	if err := repository.NewConfigParamRepository(db).UpdateValue(context.Background(),
		ParamGroupSOP, key, val, 1); err != nil {
		t.Fatalf("直接写库 sop.%s=%s 失败：%v", key, val, err)
	}
}

// TestSOPParamsReadParamCenter 六条参数真的从库里读。
func TestSOPParamsReadParamCenter(t *testing.T) {
	svc, _ := newSOPParamFixture(t)
	ctx := context.Background()

	intCases := []struct {
		name string
		def  int
		set  string
		want int
		read func(context.Context) int
	}{
		{"dispatcher_worker_count", DefaultSOPDispatcherWorkerCount, "32", 32, sop.DispatchWorkerCount},
		{"dispatcher_queue_capacity", DefaultSOPDispatcherQueueCapacity, "2048", 2048, sop.DispatchQueueCapacity},
		{"dispatcher_llm_concurrency", DefaultSOPDispatcherLLMConcurrency, "8", 8, sop.DispatchLLMConcurrency},
		{"compensation_max_attempts", DefaultCompensationMaxAttempts, "5", 5, sop.CompensationMaxAttempts},
		{"compensation_max_plans_kept", DefaultCompensationMaxPlansKept, "1024", 1024, sop.CompensationMaxPlansKept},
	}
	for _, c := range intCases {
		if got := c.read(ctx); got != c.def {
			t.Errorf("sop.%s 未改参数时应等于兜底 %d, got %d", c.name, c.def, got)
		}
		setSOPParam(t, svc, sopParamKey(c.name), c.set)
		if got := c.read(ctx); got != c.want {
			t.Errorf("sop.%s 改成 %s 后应读到 %d, got %d", c.name, c.set, c.want, got)
		}
	}

	if got := sop.CompensationTTL(ctx); got != DefaultCompensationPerTTL {
		t.Errorf("sop.compensation_ttl 未改参数时应等于兜底 %v, got %v", DefaultCompensationPerTTL, got)
	}
	setSOPParam(t, svc, SOPCompensationTTLParam, "90")
	if got := sop.CompensationTTL(ctx); got != 90*time.Second {
		t.Errorf("改 90s 后应读到 90s, got %v", got)
	}
	setSOPParam(t, svc, SOPCompensationTTLParam, "2m")
	if got := sop.CompensationTTL(ctx); got != 2*time.Minute {
		t.Errorf("改 2m 后应读到 2m, got %v", got)
	}
}

func sopParamKey(name string) string {
	switch name {
	case "dispatcher_worker_count":
		return SOPDispatcherWorkerCountParam
	case "dispatcher_queue_capacity":
		return SOPDispatcherQueueCapacityParam
	case "dispatcher_llm_concurrency":
		return SOPDispatcherLLMConcurrencyParam
	case "compensation_max_attempts":
		return SOPCompensationMaxAttemptsParam
	case "compensation_ttl":
		return SOPCompensationTTLParam
	case "compensation_max_plans_kept":
		return SOPCompensationMaxPlansKeptParam
	}
	return ""
}

// TestSOPParamsFallBackOnNonPositive 非正数一律回落常量。这一组没有「配 0 就照做」的
// 合法语义：worker 0 = 没人取任务、队列 0 = 每条节点同步执行、LLM 并发 0 = 一个都
// 不放行、补偿次数 0 = 一次都不重偿（compensation_max_attempts 的 Min 是 0，属于
// 「显式关掉重试」这一种合法取值，但读取侧同样把 0 当「没配过」回落 3——
// 二者只在「重启前已构造的管理器」这一层有区别，写进 Description 说明）。
func TestSOPParamsFallBackOnNonPositive(t *testing.T) {
	_, db := newSOPParamFixture(t)
	ctx := context.Background()
	for _, v := range []string{"0", "-1"} {
		setSOPParamRaw(t, db, SOPDispatcherWorkerCountParam, v)
		setSOPParamRaw(t, db, SOPDispatcherQueueCapacityParam, v)
		setSOPParamRaw(t, db, SOPDispatcherLLMConcurrencyParam, v)
		setSOPParamRaw(t, db, SOPCompensationMaxPlansKeptParam, v)
	}
	if got := sop.DispatchWorkerCount(ctx); got != DefaultSOPDispatcherWorkerCount {
		t.Errorf("worker_count 坏值后应回落 %d, got %d", DefaultSOPDispatcherWorkerCount, got)
	}
	if got := sop.DispatchQueueCapacity(ctx); got != DefaultSOPDispatcherQueueCapacity {
		t.Errorf("queue_capacity 坏值后应回落 %d, got %d", DefaultSOPDispatcherQueueCapacity, got)
	}
	if got := sop.DispatchLLMConcurrency(ctx); got != DefaultSOPDispatcherLLMConcurrency {
		t.Errorf("llm_concurrency 坏值后应回落 %d, got %d", DefaultSOPDispatcherLLMConcurrency, got)
	}
	if got := sop.CompensationMaxPlansKept(ctx); got != DefaultCompensationMaxPlansKept {
		t.Errorf("max_plans_kept 坏值后应回落 %d, got %d", DefaultCompensationMaxPlansKept, got)
	}
	for _, v := range []string{"0", "-30", "-1s"} {
		setSOPParamRaw(t, db, SOPCompensationTTLParam, v)
		if got := sop.CompensationTTL(ctx); got != DefaultCompensationPerTTL {
			t.Errorf("compensation_ttl=%s 应回落 %v, got %v", v, DefaultCompensationPerTTL, got)
		}
	}
}

// TestSOPParamsWithoutParamCenter 无库时全部回落常量。
func TestSOPParamsWithoutParamCenter(t *testing.T) {
	SetGlobalForTest(nil)
	ctx := context.Background()
	if got := sop.DispatchWorkerCount(ctx); got != DefaultSOPDispatcherWorkerCount {
		t.Errorf("want %d, got %d", DefaultSOPDispatcherWorkerCount, got)
	}
	if got := sop.DispatchQueueCapacity(ctx); got != DefaultSOPDispatcherQueueCapacity {
		t.Errorf("want %d, got %d", DefaultSOPDispatcherQueueCapacity, got)
	}
	if got := sop.DispatchLLMConcurrency(ctx); got != DefaultSOPDispatcherLLMConcurrency {
		t.Errorf("want %d, got %d", DefaultSOPDispatcherLLMConcurrency, got)
	}
	if got := sop.CompensationMaxAttempts(ctx); got != DefaultCompensationMaxAttempts {
		t.Errorf("want %d, got %d", DefaultCompensationMaxAttempts, got)
	}
	if got := sop.CompensationTTL(ctx); got != DefaultCompensationPerTTL {
		t.Errorf("want %v, got %v", DefaultCompensationPerTTL, got)
	}
	if got := sop.CompensationMaxPlansKept(ctx); got != DefaultCompensationMaxPlansKept {
		t.Errorf("want %d, got %d", DefaultCompensationMaxPlansKept, got)
	}
}

// TestSOPDispatcherConfigFromParams 调度器配置真的按参数组装，且没纳入参数中心的
// 四项仍取 DefaultSOPDispatcherConfig 的值。
func TestSOPDispatcherConfigFromParams(t *testing.T) {
	cfg := SOPDispatcherConfigFromParams(context.Background())
	want := DefaultSOPDispatcherConfig()
	if cfg.WorkerCount != want.WorkerCount || cfg.QueueCapacity != want.QueueCapacity ||
		cfg.LLMConcurrency != want.LLMConcurrency || cfg.MaxAttempts != want.MaxAttempts ||
		cfg.InitialBackoff != want.InitialBackoff || cfg.MaxBackoff != want.MaxBackoff ||
		cfg.BackoffMultiplier != want.BackoffMultiplier {
		t.Errorf("无库时应与默认配置逐字段一致:\n got %+v\nwant %+v", *cfg, *want)
	}
}

// TestNewSOPExecutionDispatcherNilConfigPicksUpParams 行为级：cfg 传 nil（生产路径
// cmd/api/main.go:380 就是这么调的）时读参数中心，而不是写死的 16/1000/4。
func TestNewSOPExecutionDispatcherNilConfigPicksUpParams(t *testing.T) {
	svc, _ := newSOPParamFixture(t)
	setSOPParam(t, svc, SOPDispatcherWorkerCountParam, "3")
	setSOPParam(t, svc, SOPDispatcherQueueCapacityParam, "7")
	setSOPParam(t, svc, SOPDispatcherLLMConcurrencyParam, "2")

	d := NewSOPExecutionDispatcher(nil, NewNodeExecutorRegistry(), nil)
	defer d.Stop(context.Background())
	if d.WorkerCountForTest() != 3 {
		t.Errorf("workerCount 应读到 3, got %d", d.WorkerCountForTest())
	}
	if d.QueueCapForTest() != 7 {
		t.Errorf("队列容量应读到 7, got %d", d.QueueCapForTest())
	}
	if d.LLMSemCapForTest() != 2 {
		t.Errorf("LLM 信号量应读到 2, got %d", d.LLMSemCapForTest())
	}
}

// TestCompensationConfigFromParams 补偿配置真的按参数组装，TotalTimeout 未参数化
// 仍取常量。
func TestCompensationConfigFromParams(t *testing.T) {
	cfg := sop.CompensationConfigFromParams(context.Background())
	want := sop.DefaultCompensationConfig()
	if cfg.MaxAttempts != want.MaxAttempts || cfg.PerCompensationTTL != want.PerCompensationTTL ||
		cfg.MaxPlansKept != want.MaxPlansKept || cfg.TotalTimeout != want.TotalTimeout {
		t.Errorf("无库时应与默认配置一致:\n got %+v\nwant %+v", cfg, want)
	}
}

// TestInitSOPCompensationPicksUpParams 行为级：开关开启时（测试里替换判定函数）
// 装配出来的管理器真的用参数中心的三个值。
func TestInitSOPCompensationPicksUpParams(t *testing.T) {
	svc, _ := newSOPParamFixture(t)
	setSOPParam(t, svc, SOPCompensationMaxAttemptsParam, "6")
	setSOPParam(t, svc, SOPCompensationTTLParam, "45")
	setSOPParam(t, svc, SOPCompensationMaxPlansKeptParam, "99")

	restore := sop.SwapCompensationEnabledFn(func() bool { return true })
	t.Cleanup(restore)

	d := NewSOPExecutionDispatcher(nil, NewNodeExecutorRegistry(), nil)
	defer d.Stop(context.Background())
	mgr := InitSOPCompensation(d)
	if mgr == nil {
		t.Fatal("开关开启时应装配补偿管理器")
	}
	if mgr.ConfigForTest().MaxAttempts != 6 {
		t.Errorf("MaxAttempts 应读到 6, got %d", mgr.ConfigForTest().MaxAttempts)
	}
	if mgr.ConfigForTest().PerCompensationTTL != 45*time.Second {
		t.Errorf("PerCompensationTTL 应读到 45s, got %v", mgr.ConfigForTest().PerCompensationTTL)
	}
	if mgr.ConfigForTest().MaxPlansKept != 99 {
		t.Errorf("MaxPlansKept 应读到 99, got %d", mgr.ConfigForTest().MaxPlansKept)
	}
	if mgr.ConfigForTest().TotalTimeout != 5*time.Minute {
		t.Errorf("TotalTimeout 未参数化应仍为 5m, got %v", mgr.ConfigForTest().TotalTimeout)
	}
}

// TestSOPGroupSeedsMatchConstantDefaults 种子默认值必须等于代码兜底常量。
func TestSOPGroupSeedsMatchConstantDefaults(t *testing.T) {
	want := map[string]struct {
		raw  string
		secs int
	}{
		SOPDispatcherWorkerCountParam:    {strconv.Itoa(DefaultSOPDispatcherWorkerCount), 0},
		SOPDispatcherQueueCapacityParam:  {strconv.Itoa(DefaultSOPDispatcherQueueCapacity), 0},
		SOPDispatcherLLMConcurrencyParam: {strconv.Itoa(DefaultSOPDispatcherLLMConcurrency), 0},
		SOPCompensationMaxAttemptsParam:  {strconv.Itoa(DefaultCompensationMaxAttempts), 0},
		SOPCompensationTTLParam:          {strconv.Itoa(int(DefaultCompensationPerTTL / time.Second)), int(DefaultCompensationPerTTL / time.Second)},
		SOPCompensationMaxPlansKeptParam: {strconv.Itoa(DefaultCompensationMaxPlansKept), 0},
	}
	index := make(map[string]ParamDef, len(want))
	for _, d := range DefaultParamDefs() {
		if d.Group == ParamGroupSOP {
			index[d.Key] = d
		}
	}
	if len(index) != len(want) {
		t.Fatalf("sop 组应有 %d 条种子, 实际 %d 条: %v", len(want), len(index), index)
	}
	for key, d := range index {
		w, ok := want[key]
		if !ok {
			t.Errorf("sop 组多出一条未登记的种子: %s", key)
			continue
		}
		if d.DefaultValue != w.raw {
			t.Errorf("sop.%s 种子默认值 %q != 代码兜底 %q", key, d.DefaultValue, w.raw)
		}
		if w.secs > 0 && d.DefaultValue != strconv.Itoa(w.secs) {
			t.Errorf("sop.%s 种子默认值 %q != 常量折算 %ds", key, d.DefaultValue, w.secs)
		}
		if !d.Restart {
			t.Errorf("sop.%s 是构造期读，必须标 Restart=true", key)
		}
	}
}

// TestSOPParamKeysAreAllRead 反向钉死「登记了必须有人读」。
func TestSOPParamKeysAreAllRead(t *testing.T) {
	svc, _ := newSOPParamFixture(t)
	ctx := context.Background()
	for _, k := range []string{SOPDispatcherWorkerCountParam, SOPDispatcherQueueCapacityParam,
		SOPDispatcherLLMConcurrencyParam, SOPCompensationMaxAttemptsParam,
		SOPCompensationMaxPlansKeptParam} {
		setSOPParam(t, svc, k, "1")
	}
	setSOPParam(t, svc, SOPCompensationTTLParam, "1")

	if got := sop.DispatchWorkerCount(ctx); got != 1 {
		t.Errorf("dispatcher_worker_count 应读到 1, got %d", got)
	}
	if got := sop.DispatchQueueCapacity(ctx); got != 1 {
		t.Errorf("dispatcher_queue_capacity 应读到 1, got %d", got)
	}
	if got := sop.DispatchLLMConcurrency(ctx); got != 1 {
		t.Errorf("dispatcher_llm_concurrency 应读到 1, got %d", got)
	}
	if got := sop.CompensationMaxAttempts(ctx); got != 1 {
		t.Errorf("compensation_max_attempts 应读到 1, got %d", got)
	}
	if got := sop.CompensationTTL(ctx); got != time.Second {
		t.Errorf("compensation_ttl 应读到 1s, got %v", got)
	}
	if got := sop.CompensationMaxPlansKept(ctx); got != 1 {
		t.Errorf("compensation_max_plans_kept 应读到 1, got %d", got)
	}
}

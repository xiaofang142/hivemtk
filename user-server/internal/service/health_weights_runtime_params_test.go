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

func newHealthWeightParamFixture(t *testing.T) (*ConfigParamService, *gorm.DB) {
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

func setDHParam(t *testing.T, svc *ConfigParamService, group, key, val string) {
	t.Helper()
	if err := svc.UpdateValue(context.Background(), group, key, val, 1); err != nil {
		t.Fatalf("UpdateValue(%s.%s=%s) 失败：%v", group, key, val, err)
	}
}

// refreshParamCache 换掉全局参数服务实例，把上一轮 UpdateValue 预热过的 group 缓存
// 清空。直接写库（setDHRaw）不会 invalidate——configParamService.invalidate 只在
// UpdateValue/Reset 路径上被调，绕开 service 写库后 60s 内读到的还是旧值。
// 这正是 service.UpdateValue 有 min/max 守卫、而守卫要防的"库里已有坏值"那类来路。
func refreshParamCache(t *testing.T, db *gorm.DB) {
	t.Helper()
	SetGlobalForTest(NewConfigParamService())
}

func setDHRaw(t *testing.T, db *gorm.DB, group, key, val string) {
	t.Helper()
	if err := repository.NewConfigParamRepository(db).UpdateValue(context.Background(),
		group, key, val, 1); err != nil {
		t.Fatalf("直接写库 %s.%s=%s 失败：%v", group, key, val, err)
	}
}

// TestDomainHealthParamsReadParamCenter 域名健康度四条（除构造期那条）每请求读。
func TestDomainHealthParamsReadParamCenter(t *testing.T) {
	svc, _ := newHealthWeightParamFixture(t)
	ctx := context.Background()

	if got := domainHealthScoreHealthy(ctx); got != HealthScoreHealthy {
		t.Errorf("未改参数时应等于兜底 %d, got %d", HealthScoreHealthy, got)
	}
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthScoreHealthyParam, "60")
	if got := domainHealthScoreHealthy(ctx); got != 60 {
		t.Errorf("改 60 后应读到 60, got %d", got)
	}

	if got := domainHealthScoreSwitchThreshold(ctx); got != HealthScoreSwitchThreshold {
		t.Errorf("未改参数时应等于兜底 %d, got %d", HealthScoreSwitchThreshold, got)
	}
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthScoreSwitchThresholdParam, "15")
	if got := domainHealthScoreSwitchThreshold(ctx); got != 15 {
		t.Errorf("改 15 后应读到 15, got %d", got)
	}

	if got := domainHealthConsecutiveFailureLimit(ctx); got != ConsecutiveFailureLimit {
		t.Errorf("未改参数时应等于兜底 %d, got %d", ConsecutiveFailureLimit, got)
	}
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthConsecutiveFailureLimitParam, "5")
	if got := domainHealthConsecutiveFailureLimit(ctx); got != 5 {
		t.Errorf("改 5 后应读到 5, got %d", got)
	}

	if got := domainHealthCheckTimeout(ctx); got != HealthCheckTimeout {
		t.Errorf("未改参数时应等于兜底 %v, got %v", HealthCheckTimeout, got)
	}
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthCheckTimeoutParam, "12")
	if got := domainHealthCheckTimeout(ctx); got != 12*time.Second {
		t.Errorf("改 12s 后应读到 12s, got %v", got)
	}
}

// TestDomainHealthNewServicePicksUpTimeout 行为级：新建服务实例时 http.Client 真的
// 拿到参数中心的超时（构造期读一次）。
func TestDomainHealthNewServicePicksUpTimeout(t *testing.T) {
	svc, _ := newHealthWeightParamFixture(t)
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthCheckTimeoutParam, "9")
	impl := NewDomainHealthServiceWithDeps(nil, nil, nil).(*domainHealthService)
	if impl.httpClient.Timeout != 9*time.Second {
		t.Errorf("httpClient.Timeout 应读到 9s, got %v", impl.httpClient.Timeout)
	}
}

// TestDomainHealthParamsFallBackOnNonPositive 非正数一律回落常量。
func TestDomainHealthParamsFallBackOnNonPositive(t *testing.T) {
	_, db := newHealthWeightParamFixture(t)
	ctx := context.Background()
	setDHRaw(t, db, ParamGroupDomainHealth, DomainHealthScoreHealthyParam, "0")
	setDHRaw(t, db, ParamGroupDomainHealth, DomainHealthConsecutiveFailureLimitParam, "-2")
	setDHRaw(t, db, ParamGroupDomainHealth, DomainHealthCheckTimeoutParam, "0")
	if got := domainHealthScoreHealthy(ctx); got != HealthScoreHealthy {
		t.Errorf("score_healthy 坏值后应回落 %d, got %d", HealthScoreHealthy, got)
	}
	if got := domainHealthConsecutiveFailureLimit(ctx); got != ConsecutiveFailureLimit {
		t.Errorf("consecutive_failure_limit 坏值后应回落 %d, got %d", ConsecutiveFailureLimit, got)
	}
	if got := domainHealthCheckTimeout(ctx); got != HealthCheckTimeout {
		t.Errorf("check_timeout 坏值后应回落 %v, got %v", HealthCheckTimeout, got)
	}
}

// TestRagHealthWeightsNormalizedToSumOne 六项权重按总和归一化——report.Score 是六个
// WeightedScore 直接相加（rag_health.go），不归一化的话把某一项调成 3 就会让总分超出
// 0-100、scoreToGrade 的 90/75/60 分档全部错位。
func TestRagHealthWeightsNormalizedToSumOne(t *testing.T) {
	svc, _ := newHealthWeightParamFixture(t)
	ctx := context.Background()

	r, rc, e, c, p, a := ragHealthWeights(ctx)
	if r != RagHealthWeightRetrieval || rc != RagHealthWeightRecall || e != RagHealthWeightEmbedding ||
		c != RagHealthWeightCoverage || p != RagHealthWeightPerformance || a != RagHealthWeightAlerts {
		t.Errorf("未改参数时应逐项等于常量兜底: got %v/%v/%v/%v/%v/%v", r, rc, e, c, p, a)
	}
	if sum := r + rc + e + c + p + a; sum < 0.9999 || sum > 1.0001 {
		t.Errorf("默认六项之和应为 1.0, got %v", sum)
	}

	// 把召回权重放大 10 倍，归一化后它应占主导，且总和仍是 1。
	setDHParam(t, svc, ParamGroupRagHealth, RagHealthWeightRecallParam, "3.0")
	r, rc, e, c, p, a = ragHealthWeights(ctx)
	sum := r + rc + e + c + p + a
	if sum < 0.9999 || sum > 1.0001 {
		t.Errorf("归一化后六项之和应为 1.0, got %v", sum)
	}
	if rc <= r || rc <= e || rc <= c || rc <= p || rc <= a {
		t.Errorf("召回权重调成 3.0 后应占主导, got %v", rc)
	}
	// 相对比例不变：3.0 : (其余之和) 应等于归一化后的比例关系
	others := r + e + c + p + a
	if want := 3.0 / (3.0 + 0.10 + 0.15*4); rc-want > 1e-9 || want-rc > 1e-9 {
		t.Errorf("归一化后 recall 应为 %v, got %v", want, rc)
	}
	if others <= 0 {
		t.Errorf("其余权重不应被清零, got %v", others)
	}
}

// TestRagHealthWeightsFallBackWhenAllZero 六项全被写成 0（或非正）时回落常量权重。
// 那时 report.Weight 会是 0、任何维度看起来都是 0 分，比百分比失准更难排查。
func TestRagHealthWeightsFallBackWhenAllZero(t *testing.T) {
	_, db := newHealthWeightParamFixture(t)
	ctx := context.Background()
	for _, k := range []string{RagHealthWeightRetrievalParam, RagHealthWeightRecallParam,
		RagHealthWeightEmbeddingParam, RagHealthWeightCoverageParam,
		RagHealthWeightPerformanceParam, RagHealthWeightAlertsParam} {
		setDHRaw(t, db, ParamGroupRagHealth, k, "0")
	}
	r, rc, e, c, p, a := ragHealthWeights(ctx)
	if r != RagHealthWeightRetrieval || rc != RagHealthWeightRecall || e != RagHealthWeightEmbedding ||
		c != RagHealthWeightCoverage || p != RagHealthWeightPerformance || a != RagHealthWeightAlerts {
		t.Errorf("全 0 时应回落常量权重: got %v/%v/%v/%v/%v/%v", r, rc, e, c, p, a)
	}
}

// TestRagHealthWeightIndividualFallback 单一项被写成 0 时只回落那一个，其余保持库值。
func TestRagHealthWeightIndividualFallback(t *testing.T) {
	svc, db := newHealthWeightParamFixture(t)
	ctx := context.Background()
	setDHParam(t, svc, ParamGroupRagHealth, RagHealthWeightRetrievalParam, "0.5")
	setDHRaw(t, db, ParamGroupRagHealth, RagHealthWeightRecallParam, "0")
	refreshParamCache(t, db)
	r, rc, _, _, _, _ := ragHealthWeights(ctx)
	// retrieval=0.5 / recall=0.30(回落) / 其余 0.15*4=0.6 → 总和 1.4
	if want := 0.5 / 1.4; r-want > 1e-9 || want-r > 1e-9 {
		t.Errorf("归一化后 retrieval 应为 %v, got %v", want, r)
	}
	if want := 0.30 / 1.4; rc-want > 1e-9 || want-rc > 1e-9 {
		t.Errorf("归一化后 recall 应为 %v, got %v", want, rc)
	}
}

// TestRagHealthDefaultWindowReadParamCenter RAG 健康度默认窗口每请求读。
func TestRagHealthDefaultWindowReadParamCenter(t *testing.T) {
	svc, db := newHealthWeightParamFixture(t)
	ctx := context.Background()
	if got := ragHealthDefaultWindow(ctx); got != RagHealthDefaultWindow {
		t.Errorf("未改参数时应等于兜底 %v, got %v", RagHealthDefaultWindow, got)
	}
	setDHParam(t, svc, ParamGroupRagHealth, RagHealthDefaultWindowParam, "7200")
	if got := ragHealthDefaultWindow(ctx); got != 2*time.Hour {
		t.Errorf("改 7200s 后应读到 2h, got %v", got)
	}
	setDHRaw(t, db, ParamGroupRagHealth, RagHealthDefaultWindowParam, "0")
	refreshParamCache(t, db)
	if got := ragHealthDefaultWindow(ctx); got != RagHealthDefaultWindow {
		t.Errorf("0 应回落 %v, got %v", RagHealthDefaultWindow, got)
	}
}

// TestHealthWeightGroupsSeedsMatchConstantDefaults 两组种子默认值必须等于代码兜底常量。
func TestHealthWeightGroupsSeedsMatchConstantDefaults(t *testing.T) {
	want := map[string]struct {
		raw  string
		secs int
	}{
		ParamGroupDomainHealth + "." + DomainHealthScoreHealthyParam:            {strconv.Itoa(HealthScoreHealthy), 0},
		ParamGroupDomainHealth + "." + DomainHealthScoreSwitchThresholdParam:    {strconv.Itoa(HealthScoreSwitchThreshold), 0},
		ParamGroupDomainHealth + "." + DomainHealthConsecutiveFailureLimitParam: {strconv.Itoa(ConsecutiveFailureLimit), 0},
		ParamGroupDomainHealth + "." + DomainHealthCheckTimeoutParam:            {strconv.Itoa(int(HealthCheckTimeout / time.Second)), int(HealthCheckTimeout / time.Second)},
		ParamGroupRagHealth + "." + RagHealthDefaultWindowParam:                 {strconv.Itoa(int(RagHealthDefaultWindow / time.Second)), int(RagHealthDefaultWindow / time.Second)},
		ParamGroupRagHealth + "." + RagHealthWeightRetrievalParam:               {"0.10", 0},
		ParamGroupRagHealth + "." + RagHealthWeightRecallParam:                  {"0.30", 0},
		ParamGroupRagHealth + "." + RagHealthWeightEmbeddingParam:               {"0.15", 0},
		ParamGroupRagHealth + "." + RagHealthWeightCoverageParam:                {"0.15", 0},
		ParamGroupRagHealth + "." + RagHealthWeightPerformanceParam:             {"0.15", 0},
		ParamGroupRagHealth + "." + RagHealthWeightAlertsParam:                  {"0.15", 0},
		// SaaS 配置面改造（B14，2026-11-17）：HEALTH_EMBEDDING_CRITICAL env 迁移批次。
		// bool 探活关键标记（默认 false），不走权重常量对照。
		ParamGroupRagHealth + "." + "embedding_critical": {"false", 0},
	}
	index := make(map[string]ParamDef, len(want))
	for _, d := range DefaultParamDefs() {
		if d.Group == ParamGroupDomainHealth || d.Group == ParamGroupRagHealth {
			index[d.Group+"."+d.Key] = d
		}
	}
	if len(index) != len(want) {
		t.Fatalf("两组应有 %d 条种子, 实际 %d 条: %v", len(want), len(index), index)
	}
	for key, d := range index {
		w, ok := want[key]
		if !ok {
			t.Errorf("多出一条未登记的种子: %s", key)
			continue
		}
		if d.DefaultValue != w.raw {
			t.Errorf("%s 种子默认值 %q != 代码兜底 %q", key, d.DefaultValue, w.raw)
		}
		if w.secs > 0 && d.DefaultValue != strconv.Itoa(w.secs) {
			t.Errorf("%s 种子默认值 %q != 常量折算 %ds", key, d.DefaultValue, w.secs)
		}
		if d.Group == ParamGroupDomainHealth && d.Key == DomainHealthCheckTimeoutParam && !d.Restart {
			t.Errorf("domain_health.check_timeout 是构造期读，必须标 Restart=true")
		}
	}
}

// TestHealthWeightParamKeysAreAllRead 反向钉死「登记了必须有人读」。
func TestHealthWeightParamKeysAreAllRead(t *testing.T) {
	svc, db := newHealthWeightParamFixture(t)
	ctx := context.Background()
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthScoreHealthyParam, "1")
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthScoreSwitchThresholdParam, "1")
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthConsecutiveFailureLimitParam, "1")
	setDHParam(t, svc, ParamGroupDomainHealth, DomainHealthCheckTimeoutParam, "1")
	setDHParam(t, svc, ParamGroupRagHealth, RagHealthDefaultWindowParam, "60")
	for _, k := range []string{RagHealthWeightRetrievalParam, RagHealthWeightRecallParam,
		RagHealthWeightEmbeddingParam, RagHealthWeightCoverageParam,
		RagHealthWeightPerformanceParam, RagHealthWeightAlertsParam} {
		setDHParam(t, svc, ParamGroupRagHealth, k, "0.02")
	}
	// domain_health 的 Max 是 100/60，写 1 都合法；rag_health 的 Max 是 10/604800 也合法。

	if got := domainHealthScoreHealthy(ctx); got != 1 {
		t.Errorf("score_healthy 应读到 1, got %d", got)
	}
	if got := domainHealthScoreSwitchThreshold(ctx); got != 1 {
		t.Errorf("score_switch_threshold 应读到 1, got %d", got)
	}
	if got := domainHealthConsecutiveFailureLimit(ctx); got != 1 {
		t.Errorf("consecutive_failure_limit 应读到 1, got %d", got)
	}
	if got := domainHealthCheckTimeout(ctx); got != time.Second {
		t.Errorf("check_timeout 应读到 1s, got %v", got)
	}
	if got := ragHealthDefaultWindow(ctx); got != time.Minute {
		t.Errorf("default_window 应读到 60s, got %v", got)
	}
	r, rc, e, c, p, a := ragHealthWeights(ctx)
	// 六项都写 0.02，归一化后各为 1/6
	const want = 1.0 / 6.0
	for i, got := range []float64{r, rc, e, c, p, a} {
		if got-want > 1e-9 || want-got > 1e-9 {
			t.Errorf("第 %d 项权重应为 %v, got %v", i, want, got)
		}
	}
	_ = db
}

package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/humanize"
)

// wantP1Wired 本批实际注入的 8 条。
//
// 逐条写死，不从 DefaultParamDefs() 过滤：过滤会让"拼错 key"也算通过，
// 而拼错 key 的表现是每次读都 miss、每次回落兜底——参数改了永远不生效，
// 且外表完全看不出来（这正是阶段 1.8 在 channelgw.ws_push_interval 上踩过的坑）。
var wantP1Wired = []string{
	"confidence.humanize_boundary_low",
	"confidence.humanize_boundary_high",
	"confidence.humanize_sample_rate",
	"confidence.humanize_max_retry",
	"sales.audience_default_limit",
	"sales.audience_max_limit",
	"misc.edit_lock_ttl",
	"memory.l1_ttl_hours",
}

// resetP1WiringProviders 复位。恢复「返回兜底常量的函数」而不是 nil：
// 置 nil 会让后面的读取空指针 panic；调 SetXxxProvider(nil) 则是空操作（不是复位）。
func resetP1WiringProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		humanize.SetBoundaryLowProvider(func() float64 { return humanize.DefaultBoundaryLow })
		humanize.SetBoundaryHighProvider(func() float64 { return humanize.DefaultBoundaryHigh })
		humanize.SetSampleRateProvider(func() float64 { return humanize.DefaultSampleRate })
		humanize.SetMaxRetryProvider(func() int { return humanize.DefaultMaxRetry })
		service.SetAudienceDefaultLimitProvider(func() int { return service.DefaultAudienceLimit })
		service.SetAudienceMaxLimitProvider(func() int { return service.MaxAudienceLimit })
		service.SetEditLockTTLProvider(func() time.Duration { return service.EditLockTTL })
		service.SetL1TTLHoursProvider(func() time.Duration { return service.L1TTLHours })
	})
}

func newP1ParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetP1WiringProviders(t)
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

func TestWireP1ConfigParamsKeys(t *testing.T) {
	resetP1WiringProviders(t)
	got := WireP1ConfigParams()
	if len(got) != len(wantP1Wired) {
		t.Fatalf("接线条数 = %d, 期望 %d（%v）", len(got), len(wantP1Wired), got)
	}
	seen := map[string]bool{}
	for i, want := range wantP1Wired {
		if got[i] != want {
			t.Errorf("第 %d 条 = %q, 期望 %q", i, got[i], want)
		}
		if seen[got[i]] {
			t.Errorf("第 %d 条 %q 重复注入", i, got[i])
		}
		seen[got[i]] = true
	}
}

// TestWiredP1KeysExistInSeed 接线键必须真实存在于种子表里。
// 反向钉住"加了 wiring 忘了加种子"（那种情况下读恒 miss，永远走兜底）。
func TestWiredP1KeysExistInSeed(t *testing.T) {
	resetP1WiringProviders(t)
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantP1Wired {
		if !index[k] {
			t.Errorf("%s 不在 DefaultParamDefs() 里", k)
		}
	}
}

// TestWireP1ConfigParamsReachesReaders 改库之后，8 个读取口必须真的被顶掉。
func TestWireP1ConfigParamsReachesReaders(t *testing.T) {
	svc := newP1ParamFixture(t)
	ctx := context.Background()

	// 接线前：8 个读取口都应等于各自兜底常量。
	if humanize.EffectiveBoundaryLow() != humanize.DefaultBoundaryLow {
		t.Fatalf("接线前 boundaryLow 已不等于兜底")
	}
	if service.EffectiveEditLockTTL() != service.EditLockTTL {
		t.Fatalf("接线前 editLockTTL 已不等于兜底")
	}

	WireP1ConfigParams()

	// 逐个改库并断言读取口跟着变。
	updates := []struct {
		group, key, value string
		check             func() bool
		desc              string
	}{
		{"confidence", "humanize_boundary_low", "0.31",
			func() bool { return humanize.EffectiveBoundaryLow() == 0.31 }, "boundaryLow"},
		{"confidence", "humanize_boundary_high", "0.62",
			func() bool { return humanize.EffectiveBoundaryHigh() == 0.62 }, "boundaryHigh"},
		{"confidence", "humanize_sample_rate", "0.44",
			func() bool { return humanize.EffectiveSampleRate() == 0.44 }, "sampleRate"},
		{"confidence", "humanize_max_retry", "6",
			func() bool { return humanize.EffectiveMaxRetry() == 6 }, "maxRetry"},
		{"sales", "audience_default_limit", "33",
			func() bool { return service.EffectiveAudienceDefaultLimit() == 33 }, "audienceDefault"},
		{"sales", "audience_max_limit", "66",
			func() bool { return service.EffectiveAudienceMaxLimit() == 66 }, "audienceMax"},
		{"misc", "edit_lock_ttl", "150",
			func() bool { return service.EffectiveEditLockTTL() == 150*time.Second }, "editLockTTL"},
		{"memory", "l1_ttl_hours", "3600",
			func() bool { return service.EffectiveL1TTLHours() == time.Hour }, "l1TTL"},
	}
	for _, u := range updates {
		if err := svc.UpdateValue(ctx, u.group, u.key, u.value, 1); err != nil {
			t.Fatalf("UpdateValue(%s.%s = %s) 失败：%v", u.group, u.key, u.value, err)
		}
		if !u.check() {
			t.Errorf("%s：改库为 %s 后读取口仍不认", u.desc, u.value)
		}
	}
}

// TestWireP1ConfigParamsWithoutDB 无库时接线不炸，且全部回落兜底。
func TestWireP1ConfigParamsWithoutDB(t *testing.T) {
	resetP1WiringProviders(t)
	service.SetGlobalForTest(nil)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	if got := WireP1ConfigParams(); len(got) != len(wantP1Wired) {
		t.Fatalf("无库时接线条数 = %d, 期望 %d", len(got), len(wantP1Wired))
	}
	if humanize.EffectiveBoundaryLow() != humanize.DefaultBoundaryLow ||
		humanize.EffectiveBoundaryHigh() != humanize.DefaultBoundaryHigh ||
		humanize.EffectiveSampleRate() != humanize.DefaultSampleRate ||
		humanize.EffectiveMaxRetry() != humanize.DefaultMaxRetry {
		t.Error("无库时 humanize 四个读取口应全部回落兜底")
	}
	if service.EffectiveAudienceDefaultLimit() != service.DefaultAudienceLimit ||
		service.EffectiveAudienceMaxLimit() != service.MaxAudienceLimit ||
		service.EffectiveEditLockTTL() != service.EditLockTTL ||
		service.EffectiveL1TTLHours() != service.L1TTLHours {
		t.Error("无库时 service 四个读取口应全部回落兜底")
	}
}

// TestP1SeedDefaultsMatchCodeFallbacks 种子默认值必须等于代码兜底常量。
// ResetToDefault / BulkResetGroup 会把 DefaultValue 写回库里，两边不一致
// 等于「点一下重置，系统行为就变」。
func TestP1SeedDefaultsMatchCodeFallbacks(t *testing.T) {
	want := map[string]string{
		"confidence.humanize_boundary_low":  "0.70",
		"confidence.humanize_boundary_high": "0.85",
		"confidence.humanize_sample_rate":   "0.10",
		"confidence.humanize_max_retry":     "3",
		"sales.audience_default_limit":      "200",
		"sales.audience_max_limit":          "500",
		"misc.edit_lock_ttl":                "300",
		"memory.l1_ttl_hours":               "86400",
	}
	index := map[string]service.ParamDef{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = d
	}
	for k, v := range want {
		d, ok := index[k]
		if !ok {
			t.Errorf("%s 不在 DefaultParamDefs() 里", k)
			continue
		}
		if d.DefaultValue != v {
			t.Errorf("%s 的种子默认值 = %q, 期望 %q", k, d.DefaultValue, v)
		}
	}
}

// TestP1SeedBoundsAreSane 逐条钉住 min/max 与 Restart 标记。
func TestP1SeedBoundsAreSane(t *testing.T) {
	index := map[string]service.ParamDef{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = d
	}
	// sample_rate 的 Min 必须含 0：0 是"一个都不送 LLM"的合法语义。
	if d := index["confidence.humanize_sample_rate"]; d.Min == nil || *d.Min != "0" {
		t.Errorf("humanize_sample_rate 的 Min 应为 0, 实为 %v", d.Min)
	}
	if d := index["confidence.humanize_max_retry"]; d.Min == nil || *d.Min != "1" {
		t.Errorf("humanize_max_retry 的 Min 应为 1, 实为 %v", d.Min)
	}
	// memory.l1_ttl_hours 的 Min 不能是 0：0 会让刚写入的短期消息立即过期。
	if d := index["memory.l1_ttl_hours"]; d.Min == nil || *d.Min != "60" {
		t.Errorf("memory.l1_ttl_hours 的 Min 应为 60, 实为 %v", d.Min)
	}
	// geo 组那条不在 wiring 里（走直连），但同样要钉住。
	if d, ok := index["geo.default_visibility_days"]; !ok {
		t.Error("geo.default_visibility_days 不在 DefaultParamDefs() 里")
	} else if d.DefaultValue != "30" || d.Min == nil || *d.Min != "1" || d.Max == nil || *d.Max != "365" {
		t.Errorf("geo.default_visibility_days 的默认/上下界不符：%+v", d)
	}
	// 本批 9 条都不需要重启：全是运行时读取。
	for _, k := range append(append([]string{}, wantP1Wired...), "geo.default_visibility_days") {
		d, ok := index[k]
		if !ok {
			continue
		}
		if d.Restart {
			t.Errorf("%s 不应标 Restart=true（本批全部是运行时读取）", k)
		}
	}
}

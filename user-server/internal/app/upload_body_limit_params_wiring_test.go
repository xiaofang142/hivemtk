package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantUploadBodyLimitWired 逐条写死，不从 DefaultParamDefs() 过滤 —— 理由同前八批：
// 过滤只能钉住"接上的都在本组内"，拼错 key 时断言照样成立，真正漏接的那条没人发现。
//
// ⚠️ 这里只有 middleware.max_json_body_mb 一条。misc.upload_max_size_mb 走「读取函数
// 自己读参数中心」的直连范式（读取点在 internal/controller/upload.go，那里能直接
// 依赖 service），不需要注入；它的接线正确性由 internal/controller/upload_param_limit_test.go
// 的真库行为测试盯住。把它塞进这个列表会是"看起来被接了、其实没走装配"的假声明。
var wantUploadBodyLimitWired = []string{
	"middleware.max_json_body_mb",
}

func resetUploadBodyLimitProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { middleware.SetMaxJSONBodyMBProvider(nil) })
}

func newUploadBodyLimitParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetUploadBodyLimitProviders(t)
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

func TestWireUploadBodyLimitConfigParamsKeys(t *testing.T) {
	resetUploadBodyLimitProviders(t)
	got := WireUploadBodyLimitConfigParams()
	if len(got) != len(wantUploadBodyLimitWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantUploadBodyLimitWired), got, wantUploadBodyLimitWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantUploadBodyLimitWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantUploadBodyLimitWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次：后一次覆盖前一次，读点会跟着错位", k)
		}
		seen[k] = true
	}
}

func TestWiredUploadBodyLimitKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantUploadBodyLimitWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// 装配完之后，参数中心的值必须真的出现在读取点上。只断言 setter 收下了 provider
// 没有意义 —— 那证明不了「运维在页面上改值真的改变了行为」。
func TestWireUploadBodyLimitConfigParamsReachesReader(t *testing.T) {
	svc := newUploadBodyLimitParamFixture(t)
	ctx := context.Background()

	t.Setenv("MAX_JSON_BODY_MB", "") // 让 env 不挡路
	before := middleware.BodyLimitFromEnv()
	WireUploadBodyLimitConfigParams()

	if err := svc.UpdateValue(ctx, "middleware", "max_json_body_mb", "32", 1); err != nil {
		t.Fatalf("改 middleware.max_json_body_mb 失败：%v", err)
	}
	got := middleware.BodyLimitFromEnv()
	want := int64(32 * 1024 * 1024)
	if got != want {
		t.Fatalf("未被读走：got %d（原 %d）want %d", got, before, want)
	}
}

// 参数表不存在时（没跑迁移、或装配早于建表），接线必须仍把点位托在代码兜底值上，
// 而不是返回一个 nil 函数在首个请求里炸掉。
func TestWireUploadBodyLimitConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)
	resetUploadBodyLimitProviders(t)
	t.Setenv("MAX_JSON_BODY_MB", "")

	if got := WireUploadBodyLimitConfigParams(); len(got) != len(wantUploadBodyLimitWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantUploadBodyLimitWired))
	}
	if got, want := middleware.BodyLimitFromEnv(), int64(middleware.DefaultMaxJSONBodyMB)*1024*1024; got != want {
		t.Fatalf("无库时应兜底 %d，got %d", want, got)
	}
}

// misc.upload_max_size_mb 的默认值必须是 0（不覆盖）。这是 2a 最容易写错的一处：
// 填 10 会把运维已在「系统配置」页调好的 200MB 又压回 10MB，等于把阶段 0.5
// 刚修好的 bug 原样退回；填 50 则让新装站点从 10MB 静默放宽 5 倍。
func TestUploadParamSeedDefaultIsNoOverride(t *testing.T) {
	for _, d := range service.DefaultParamDefs() {
		if d.Group != "misc" || d.Key != "upload_max_size_mb" {
			continue
		}
		if d.DefaultValue != "0" {
			t.Fatalf("misc.upload_max_size_mb 默认值 = %q，应为 \"0\"（不覆盖）", d.DefaultValue)
		}
		if d.Min == nil || *d.Min != "0" {
			t.Fatalf("Min 应为 0，否则运维无法把参数填回 0 来关闭覆盖：%v", d.Min)
		}
		return
	}
	t.Fatal("misc.upload_max_size_mb 不在 DefaultParamDefs() 里")
}

// body 上限在 router.Setup 里装配期读一次，所以 Restart 必须为 true；
// 标成 false 会让运维在页面上改了参数却看不到任何提示，直到下次重启才生效。
func TestMaxJSONBodyBodyLimitIsMarkedRestart(t *testing.T) {
	for _, d := range service.DefaultParamDefs() {
		if d.Group != "middleware" || d.Key != "max_json_body_mb" {
			continue
		}
		if !d.Restart {
			t.Fatal("middleware.max_json_body_mb 应标 Restart=true：router.Setup 装配期只读一次，不重启不生效")
		}
		if d.DefaultValue != "8" {
			t.Fatalf("默认值 = %q，应为 8：低于此值会把各端点既有上界（webhook 4MB）截断", d.DefaultValue)
		}
		if d.Min == nil || *d.Min != "1" {
			t.Fatalf("Min 应为 1，运营侧不提供「关掉全局 body 封顶」的口子：%v", d.Min)
		}
		return
	}
	t.Fatal("middleware.max_json_body_mb 不在 DefaultParamDefs() 里")
}

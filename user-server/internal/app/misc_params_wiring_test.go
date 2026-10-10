package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/security"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/textutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// wantMiscWired 是 misc 组本轮接线的 16 条，逐条写死而不是从 DefaultParamDefs() 过滤。
//
// 刻意不写成"扫出所有 misc 组的键然后断言接上了"：那样只钉住"接上的都在 misc 组里"，
// 拼错 key 时这个断言照样成立（错的 key 也算 misc 组），真正漏接的那条反而没人发现。
// 写死之后，少一条、多一条、改一个键名都会红。
var wantMiscWired = []string{
	"misc.visitor_token_ttl",
	"misc.sso_cookie_ttl",
	"misc.polling_lock_stale_threshold",
	"misc.domain_check_concurrency",
	"misc.sms_max_retry",
	"misc.csv_export_max_rows",
	"misc.text_truncate_max_bytes",
	"misc.preview_max_len",
	"misc.sop_scheduler_interval",
	"misc.sop_max_wait",
	"misc.deepl_timeout",
	"misc.reply_sem_timeout",
	"misc.ownership_cache_ttl",
	"misc.summary_stale_threshold",
	"misc.agentloop_history_max_candidates",
	"misc.backup_page_size",
}

// newMiscParamFixture 建测试库、灌种子、把参数中心换成"真读库"的实例。
//
// t.Cleanup 还原全局：globalConfigParam 是进程级包级变量，留着带 DB 的实例会把同包
// 后续用例的 Get* 一并带走。
func newMiscParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
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

// TestWireMiscConfigParamsKeys 装配点声明接线的键，必须与上面那 16 条逐条相等。
func TestWireMiscConfigParamsKeys(t *testing.T) {
	got := WireMiscConfigParams()
	if len(got) != len(wantMiscWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantMiscWired), got, wantMiscWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantMiscWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantMiscWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次：同一个点位注入两次，后一次覆盖前一次，读点会跟着错位", k)
		}
		seen[k] = true
	}
}

// TestWiredKeysExistInSeed 上面那 16 条必须在种子表里真实存在。
//
// 这一格是防"接线代码引用了一个没登记的键"：不存在的键在运行期永远读到兜底值，
// 页面上又看不见（管理台没有这一行），和当初那 72 条僵尸一模一样的病。
func TestWiredKeysExistInSeed(t *testing.T) {
	index := map[string]string{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = d.DefaultValue
	}
	for _, k := range wantMiscWired {
		if _, ok := index[k]; !ok {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// TestWireMiscConfigParamsReachesReaders 装配完之后，参数中心的值必须真的出现在读取点上。
//
// 这里只断言四个读取口是导出的（security / model / textutil / repository 的取值函数），
// 其余 12 条的"注入生效"由各自包内的用例覆盖——那 12 个取值函数刻意不导出，
// 导出来只为了在本文件断言，会把一包内部实现细节抬成公共 API。
func TestWireMiscConfigParamsReachesReaders(t *testing.T) {
	svc := newMiscParamFixture(t)
	ctx := context.Background()

	// 先在"没装配"的状态下把这四个读取口的原值记住：接上之后它们必须被参数中心的值
	// 顶掉，否则"接了线"只是多调了一个 setter。
	before := map[string]time.Duration{
		"visitor_token_ttl":            security.GetVisitorTokenTTL(),
		"polling_lock_stale_threshold": repository.GetPollingLockStaleThreshold(),
	}
	beforeInt := map[string]int{
		"sms_max_retry":           model.GetSmsMaxRetry(),
		"text_truncate_max_bytes": textutil.DefaultTruncateMaxBytes(),
	}

	if err := svc.UpdateValue(ctx, "misc", "visitor_token_ttl", "3600", 1); err != nil {
		t.Fatalf("改 visitor_token_ttl 失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "misc", "polling_lock_stale_threshold", "300", 1); err != nil {
		t.Fatalf("改 polling_lock_stale_threshold 失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "misc", "sms_max_retry", "6", 1); err != nil {
		t.Fatalf("改 sms_max_retry 失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "misc", "text_truncate_max_bytes", "1024", 1); err != nil {
		t.Fatalf("改 text_truncate_max_bytes 失败：%v", err)
	}

	WireMiscConfigParams()

	if got, want := security.GetVisitorTokenTTL(), 3600*time.Second; got != want {
		t.Errorf("visitor_token_ttl 未被读走：got %v（原 %v）want %v", got, before["visitor_token_ttl"], want)
	}
	if got, want := repository.GetPollingLockStaleThreshold(), 300*time.Second; got != want {
		t.Errorf("polling_lock_stale_threshold 未被读走：got %v（原 %v）want %v", got, before["polling_lock_stale_threshold"], want)
	}
	if got, want := model.GetSmsMaxRetry(), 6; got != want {
		t.Errorf("sms_max_retry 未被读走：got %v（原 %v）want %v", got, beforeInt["sms_max_retry"], want)
	}
	if got, want := textutil.DefaultTruncateMaxBytes(), 1024; got != want {
		t.Errorf("text_truncate_max_bytes 未被读走：got %v（原 %v）want %v", got, beforeInt["text_truncate_max_bytes"], want)
	}
}

// TestWireMiscConfigParamsWithoutDB 参数表根本不存在时（没跑迁移、或装配早于建表），
// 接线必须仍然把各点位托在代码兜底值上，而不是返回一个 nil 函数在首次请求时炸掉。
func TestWireMiscConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)

	if got := WireMiscConfigParams(); len(got) != len(wantMiscWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantMiscWired))
	}
	if got, want := security.GetVisitorTokenTTL(), security.DefaultVisitorTokenTTL; got != want {
		t.Errorf("无库时 visitor_token_ttl = %v，期望兜底 %v", got, want)
	}
	if got, want := model.GetSmsMaxRetry(), model.DefaultSmsMaxRetry; got != want {
		t.Errorf("无库时 sms_max_retry = %v，期望兜底 %v", got, want)
	}
	if got, want := textutil.DefaultTruncateMaxBytes(), textutil.DefaultMaxBytes; got != want {
		t.Errorf("无库时 text_truncate_max_bytes = %v，期望兜底 %v", got, want)
	}
	if got, want := repository.GetPollingLockStaleThreshold(), repository.PollingLockStaleThreshold; got != want {
		t.Errorf("无库时 polling_lock_stale_threshold = %v，期望兜底 %v", got, want)
	}
}

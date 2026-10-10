package app

import (
	"context"
	"testing"

	corebot "hivemtk-user/internal/channelbot/core"
	qqbot "hivemtk-user/internal/channelbot/qq"
	tgbot "hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantChannelBotWired 逐条写死，不从 DefaultParamDefs() 过滤：
// 过滤会让「key 拼错但恰好属于 channelbot 组」这种漏接悄悄通过。
var wantChannelBotWired = []string{
	"channelbot.qq_message_max_len",
	"channelbot.tg_message_max_length",
	"channelbot.tg_inline_rows_max",
	"channelbot.tg_inline_buttons_per_row_max",
	"channelbot.http_timeout",
}

func resetChannelBotProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		qqbot.SetQQMessageMaxLenProvider(nil)
		tgbot.SetTGMessageMaxLenProvider(nil)
		tgbot.SetTGInlineRowsMaxProvider(nil)
		tgbot.SetTGInlineButtonsPerRowMaxProvider(nil)
		corebot.SetHTTPTimeoutProvider(nil)
	})
}

func newChannelBotParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetChannelBotProviders(t)
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

func TestWireChannelBotConfigParamsKeys(t *testing.T) {
	got := WireChannelBotConfigParams()
	if len(got) != len(wantChannelBotWired) {
		t.Fatalf("接线条数不符：want %d (%v)，got %d (%v)",
			len(wantChannelBotWired), wantChannelBotWired, len(got), got)
	}
	seen := make(map[string]bool, len(got))
	for i, k := range wantChannelBotWired {
		if got[i] != k {
			t.Errorf("第 %d 条键不符：want %q，got %q", i, k, got[i])
		}
		if seen[k] {
			t.Errorf("键重复出现：%q", k)
		}
		seen[k] = true
	}
}

func TestWiredChannelBotKeysExistInSeed(t *testing.T) {
	index := make(map[string]bool)
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantChannelBotWired {
		if !index[k] {
			t.Errorf("%s 不在 DefaultParamDefs() 里", k)
		}
	}
}

func TestWireChannelBotConfigParamsReachesReaders(t *testing.T) {
	svc := newChannelBotParamFixture(t)

	// 注入前，五个读取口都应等于各自兜底常量。
	if got := qqbot.ProbeQQMessageMaxLen(); got != qqbot.QQMessageMaxLen {
		t.Fatalf("注入前 QQ 上限 = %d，want 兜底 %d", got, qqbot.QQMessageMaxLen)
	}
	if got := tgbot.ProbeTGMessageMaxLength(); got != tgbot.TGMessageMaxLength {
		t.Fatalf("注入前 TG 上限 = %d，want 兜底 %d", got, tgbot.TGMessageMaxLength)
	}
	if got := tgbot.ProbeTGInlineRowsMax(); got != tgbot.TGInlineRowsMax {
		t.Fatalf("注入前 TG 行数 = %d，want 兜底 %d", got, tgbot.TGInlineRowsMax)
	}
	if got := tgbot.ProbeTGInlineButtonsPerRowMax(); got != tgbot.TGInlineButtonsPerRowMax {
		t.Fatalf("注入前 TG 每行按钮 = %d，want 兜底 %d", got, tgbot.TGInlineButtonsPerRowMax)
	}
	if got := corebot.ProbeHTTPTimeout(); got != corebot.DefaultHTTPTimeout {
		t.Fatalf("注入前 HTTP 超时 = %v，want 兜底 %v", got, corebot.DefaultHTTPTimeout)
	}

	WireChannelBotConfigParams()

	ctx := context.Background()
	updates := []struct{ group, key, val string }{
		{"channelbot", "qq_message_max_len", "500"},
		{"channelbot", "tg_message_max_length", "1000"},
		{"channelbot", "tg_inline_rows_max", "42"},
		{"channelbot", "tg_inline_buttons_per_row_max", "6"},
		{"channelbot", "http_timeout", "45"},
	}
	for _, u := range updates {
		if err := svc.UpdateValue(ctx, u.group, u.key, u.val, 1); err != nil {
			t.Fatalf("UpdateValue(%s) 失败：%v", u.key, err)
		}
	}

	if got := qqbot.ProbeQQMessageMaxLen(); got != 500 {
		t.Errorf("QQ 上限未被顶掉：got %d，want 500", got)
	}
	if got := tgbot.ProbeTGMessageMaxLength(); got != 1000 {
		t.Errorf("TG 上限未被顶掉：got %d，want 1000", got)
	}
	if got := tgbot.ProbeTGInlineRowsMax(); got != 42 {
		t.Errorf("TG 行数未被顶掉：got %d，want 42", got)
	}
	if got := tgbot.ProbeTGInlineButtonsPerRowMax(); got != 6 {
		t.Errorf("TG 每行按钮未被顶掉：got %d，want 6", got)
	}
	if got := corebot.ProbeHTTPTimeout(); got != 45*1000*1000*1000 {
		t.Errorf("HTTP 超时未被顶掉：got %v，want 45s", got)
	}
}

func TestWireChannelBotConfigParamsWithoutDB(t *testing.T) {
	resetChannelBotProviders(t)
	service.SetGlobalForTest(nil)

	got := WireChannelBotConfigParams()
	if len(got) != len(wantChannelBotWired) {
		t.Fatalf("无库时接线条数变了：want %d，got %d", len(wantChannelBotWired), len(got))
	}
	// 无库（GlobalConfigParam() 返回无 repo 的 stub）时五个读取口必须回落兜底。
	if got := qqbot.ProbeQQMessageMaxLen(); got != qqbot.QQMessageMaxLen {
		t.Errorf("无库时 QQ 上限 = %d，want 兜底 %d", got, qqbot.QQMessageMaxLen)
	}
	if got := tgbot.ProbeTGMessageMaxLength(); got != tgbot.TGMessageMaxLength {
		t.Errorf("无库时 TG 上限 = %d，want 兜底 %d", got, tgbot.TGMessageMaxLength)
	}
	if got := tgbot.ProbeTGInlineRowsMax(); got != tgbot.TGInlineRowsMax {
		t.Errorf("无库时 TG 行数 = %d，want 兜底 %d", got, tgbot.TGInlineRowsMax)
	}
	if got := tgbot.ProbeTGInlineButtonsPerRowMax(); got != tgbot.TGInlineButtonsPerRowMax {
		t.Errorf("无库时 TG 每行按钮 = %d，want 兜底 %d", got, tgbot.TGInlineButtonsPerRowMax)
	}
	if got := corebot.ProbeHTTPTimeout(); got != corebot.DefaultHTTPTimeout {
		t.Errorf("无库时 HTTP 超时 = %v，want 兜底 %v", got, corebot.DefaultHTTPTimeout)
	}
}

// TestChannelBotSeedsStayWithinPlatformCaps 反向钉死种子默认值与平台硬上限对齐。
// 上限超过平台限制没有意义：平台侧会拒收，而本地分段逻辑永远走不到那一档。
func TestChannelBotSeedsStayWithinPlatformCaps(t *testing.T) {
	want := map[string]struct {
		def, max string
	}{
		"channelbot.qq_message_max_len":            {"2000", "2000"},
		"channelbot.tg_message_max_length":         {"4096", "4096"},
		"channelbot.tg_inline_rows_max":            {"100", "100"},
		"channelbot.tg_inline_buttons_per_row_max": {"8", "8"},
	}
	index := make(map[string]service.ParamDef)
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = d
	}
	for k, w := range want {
		d, ok := index[k]
		if !ok {
			t.Fatalf("%s 不在 DefaultParamDefs() 里", k)
		}
		if d.DefaultValue != w.def {
			t.Errorf("%s 默认值 = %q，want %q", k, d.DefaultValue, w.def)
		}
		if d.Max == nil || *d.Max != w.max {
			t.Errorf("%s 的 Max = %v，want %q（= 平台硬上限）", k, d.Max, w.max)
		}
	}
}

// TestConstructionTimeReadsAreMarkedRestart 钉住三个构造期读取点必须标 Restart=true。
// 漏标会让运维改完看不到任何生效提示，还以为是参数没保存。
func TestConstructionTimeReadsAreMarkedRestart(t *testing.T) {
	want := []string{"channelbot.http_timeout", "sse.client_buffer_size", "bridge.sse_bus_buffer_size"}
	index := make(map[string]service.ParamDef)
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = d
	}
	for _, k := range want {
		d, ok := index[k]
		if !ok {
			t.Fatalf("%s 不在 DefaultParamDefs() 里", k)
		}
		if !d.Restart {
			t.Errorf("%s 的读取点在构造函数里，却未标 Restart=true", k)
		}
	}
}

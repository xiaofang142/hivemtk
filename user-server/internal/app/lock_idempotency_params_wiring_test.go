package app

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// wantLockIdempotencyWired 逐条写死，刻意不从 DefaultParamDefs() 里按 group 过滤：
// 过滤法会把「拼错 group 前缀」「漏接某条」也一起算成已接，那种绿没有意义。
var wantLockIdempotencyWired = []string{
	"lock.inbox_human_ttl",
	"lock.inbox_pending_ttl",
	"lock.inbox_content_dedup_ttl",
	"lock.ingest_lock_ttl",
	"lock.ai_processing_ttl",
	"lock.message_hub_idem_ttl",
}

func resetLockIdempotencyProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		service.SetInboxHumanLockTTLProvider(nil)
		service.SetInboxPendingTTLProvider(nil)
		service.SetIngestLockTTLProvider(nil)
		service.SetAIProcessingTTLProvider(nil)
		service.SetMessageHubIdemTTLProvider(nil)
		service.SetInboxContentDedupTTLProvider(nil)
	})
}

func newLockIdempotencyParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	resetLockIdempotencyProviders(t)

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

func TestWireLockIdempotencyConfigParamsKeys(t *testing.T) {
	got := WireLockIdempotencyConfigParams()
	if len(got) != len(wantLockIdempotencyWired) {
		t.Fatalf("接线条数 = %d，期望 %d（%v）", len(got), len(wantLockIdempotencyWired), got)
	}
	for i := range wantLockIdempotencyWired {
		if got[i] != wantLockIdempotencyWired[i] {
			t.Errorf("第 %d 条 = %q，期望 %q", i, got[i], wantLockIdempotencyWired[i])
		}
	}
	seen := make(map[string]bool, len(got))
	for _, k := range got {
		if seen[k] {
			t.Errorf("接线列表里 %s 重复了", k)
		}
		seen[k] = true
	}
}

func TestWiredLockIdempotencyKeysExistInSeed(t *testing.T) {
	index := make(map[string]bool)
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantLockIdempotencyWired {
		if !index[k] {
			t.Errorf("%s 不在 DefaultParamDefs() 里：种子与装配脱节了", k)
		}
	}
}

func TestWireLockIdempotencyConfigParamsReachesReaders(t *testing.T) {
	svc := newLockIdempotencyParamFixture(t)
	ctx := context.Background()

	// 改库前的基线：六条都应落在各自兜底常量上。
	before := []struct {
		key  string
		want interface{}
		got  interface{}
	}{
		{"lock.inbox_human_ttl", service.InboxLockTTL, service.ProbeInboxHumanLockTTL()},
		{"lock.inbox_pending_ttl", service.InboxPendingTTL, service.ProbeInboxPendingTTL()},
		{"lock.inbox_content_dedup_ttl", service.InboxContentDedupTTL, service.ProbeInboxContentDedupTTL()},
		{"lock.ingest_lock_ttl", service.IngestLockTTL, service.ProbeIngestLockTTL()},
		{"lock.ai_processing_ttl", service.InboxAIProcessingTTL, service.ProbeAIProcessingTTL()},
		{"lock.message_hub_idem_ttl", service.MessageHubDefaultIdemTTL, service.ProbeMessageHubIdemTTL()},
	}
	for _, c := range before {
		if c.got != c.want {
			t.Fatalf("%s 未注入时应等于兜底常量 %v，实际 %v", c.key, c.want, c.got)
		}
	}

	wired := WireLockIdempotencyConfigParams()
	if len(wired) != len(wantLockIdempotencyWired) {
		t.Fatalf("接线条数 = %d，期望 %d", len(wired), len(wantLockIdempotencyWired))
	}

	// 每条改成与兜底不同的值，再断言读取口真的变了。
	overrides := []struct {
		group string
		key   string
		val   string
	}{
		{"lock", "inbox_human_ttl", "7200"},
		{"lock", "inbox_pending_ttl", "120"},
		{"lock", "ingest_lock_ttl", "10"},
		{"lock", "ai_processing_ttl", "45"},
		{"lock", "message_hub_idem_ttl", "43200"},
		{"lock", "inbox_content_dedup_ttl", "90"},
	}
	for _, o := range overrides {
		if err := svc.UpdateValue(ctx, o.group, o.key, o.val, 1); err != nil {
			t.Fatalf("UpdateValue(%s.%s) 失败：%v", o.group, o.key, err)
		}
	}

	// 装配层读的是 group.key，取值仍按秒折算。
	if got := service.ProbeInboxHumanLockTTL().Seconds(); got != 7200 {
		t.Errorf("inbox_human_ttl = %v 秒，期望 7200", got)
	}
	if got := service.ProbeInboxPendingTTL().Seconds(); got != 120 {
		t.Errorf("inbox_pending_ttl = %v 秒，期望 120", got)
	}
	if got := service.ProbeIngestLockTTL().Seconds(); got != 10 {
		t.Errorf("ingest_lock_ttl = %v 秒，期望 10", got)
	}
	if got := service.ProbeAIProcessingTTL().Seconds(); got != 45 {
		t.Errorf("ai_processing_ttl = %v 秒，期望 45", got)
	}
	if got := service.ProbeMessageHubIdemTTL().Seconds(); got != 43200 {
		t.Errorf("message_hub_idem_ttl = %v 秒，期望 43200", got)
	}
	if got := service.ProbeInboxContentDedupTTL().Seconds(); got != 90 {
		t.Errorf("inbox_content_dedup_ttl = %v 秒，期望 90", got)
	}
}

func TestWireLockIdempotencyConfigParamsWithoutDB(t *testing.T) {
	resetLockIdempotencyProviders(t)
	service.SetGlobalForTest(nil)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })

	wired := WireLockIdempotencyConfigParams()
	if len(wired) != len(wantLockIdempotencyWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d", len(wired), len(wantLockIdempotencyWired))
	}

	// 无库 = 参数中心读不到值，六条必须全部回落各自兜底常量。
	if got := service.ProbeInboxHumanLockTTL(); got != service.InboxLockTTL {
		t.Errorf("inbox_human_ttl = %v，期望兜底 %v", got, service.InboxLockTTL)
	}
	if got := service.ProbeInboxPendingTTL(); got != service.InboxPendingTTL {
		t.Errorf("inbox_pending_ttl = %v，期望兜底 %v", got, service.InboxPendingTTL)
	}
	if got := service.ProbeIngestLockTTL(); got != service.IngestLockTTL {
		t.Errorf("ingest_lock_ttl = %v，期望兜底 %v", got, service.IngestLockTTL)
	}
	if got := service.ProbeAIProcessingTTL(); got != service.InboxAIProcessingTTL {
		t.Errorf("ai_processing_ttl = %v，期望兜底 %v", got, service.InboxAIProcessingTTL)
	}
	if got := service.ProbeMessageHubIdemTTL(); got != service.MessageHubDefaultIdemTTL {
		t.Errorf("message_hub_idem_ttl = %v，期望兜底 %v", got, service.MessageHubDefaultIdemTTL)
	}
	if got := service.ProbeInboxContentDedupTTL(); got != service.InboxContentDedupTTL {
		t.Errorf("inbox_content_dedup_ttl = %v，期望兜底 %v", got, service.InboxContentDedupTTL)
	}
}

// TestMessageHubIdemTTLIsMarkedRestart 反向钉死 message_hub_idem_ttl 的 Restart 标记。
// 它由 NewMessageHubServiceWithDB 在账号服务构造期写入结构体字段，而账号服务
// （qq_account / wecom_integration / feishu / email）都在启动时一次性构造，
// 所以改了不重启对已启动的账号毫无意义 —— 不标 Restart 会让运维改完看不到任何效果。
func TestMessageHubIdemTTLIsMarkedRestart(t *testing.T) {
	for _, d := range service.DefaultParamDefs() {
		if d.Group != "lock" || d.Key != "message_hub_idem_ttl" {
			continue
		}
		if !d.Restart {
			t.Error("lock.message_hub_idem_ttl 必须标 Restart=true（账号服务构造期读取）")
		}
		if d.DefaultValue != "86400" {
			t.Errorf("lock.message_hub_idem_ttl 默认值 = %q，期望 86400（与 MessageHubDefaultIdemTTL 对齐）", d.DefaultValue)
		}
		return
	}
	t.Fatal("lock.message_hub_idem_ttl 不在 DefaultParamDefs() 里")
}

// TestAILockTTLStaysUnwired 反向钉死 ai_lock_ttl 的「未接线」标注。
// 它是刻意不接的：tryAcquireAILock / ReleaseAILock / IsSessionAIBusy 整条生产链路
// 没有调用方（源文件自带 //nolint:unused），真正在跑的并发闸是 ai_processing_ttl。
// 谁要是将来把标注去掉，这条测试先红，逼他把 callers 一起接上。
func TestAILockTTLStaysUnwired(t *testing.T) {
	for _, d := range service.DefaultParamDefs() {
		if d.Group != "lock" || d.Key != "ai_lock_ttl" {
			continue
		}
		if !strings.Contains(d.Name, "未接线") {
			t.Error("lock.ai_lock_ttl 的 Name 必须仍含「未接线」标注（readpoints 门禁依赖它）")
		}
		if !strings.Contains(d.Description, "当前不生效") {
			t.Error("lock.ai_lock_ttl 的 Description 必须仍含「当前不生效」标注（readpoints 门禁依赖它）")
		}
		return
	}
	t.Fatal("lock.ai_lock_ttl 不在 DefaultParamDefs() 里")
}

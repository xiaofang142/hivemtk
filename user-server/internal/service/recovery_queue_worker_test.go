package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------------------------------------------------------------- 测试替身

type fakeRecoveryReach struct {
	calls []*ProactiveReachRequest
	resp  *ProactiveReachResponse
	err   error
}

func (f *fakeRecoveryReach) ReachByCustomer(_ context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &ProactiveReachResponse{MessageID: "msg-1", Channel: "sms", Status: "sent"}, nil
}

func (f *fakeRecoveryReach) allDryRun() bool {
	if len(f.calls) == 0 {
		return false
	}
	for _, c := range f.calls {
		if !c.DryRun {
			return false
		}
	}
	return true
}

type fakeRecoveryClaim struct {
	held map[string]bool
	err  error
	keys []string
}

func newFakeRecoveryClaim() *fakeRecoveryClaim {
	return &fakeRecoveryClaim{held: make(map[string]bool)}
}

func (f *fakeRecoveryClaim) setNX(_ context.Context, key string, _ time.Duration) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.keys = append(f.keys, key)
	if f.held[key] {
		return false, nil
	}
	f.held[key] = true
	return true, nil
}

// newTestRecoveryWorker 直接构造（不经 env），只保留本文件关心的字段。
func newTestRecoveryWorker(mode RecoveryWorkerMode, queue *RecoveryQueueService, reach recoveryReachSender) (*RecoveryQueueWorker, *fakeRecoveryClaim) {
	claim := newFakeRecoveryClaim()
	return &RecoveryQueueWorker{
		queue:    queue,
		reach:    reach,
		mode:     mode,
		batch:    recoveryWorkerDefaultBatch,
		interval: time.Hour,
		backoff:  reachCooldownWindow + recoveryCooldownRetryGap,
		claimTTL: recoveryWorkerClaimTTL,
		nowFunc:  time.Now,
		claim:    claim.setNX,
		stop:     make(chan struct{}),
	}, claim
}

func newMockQueueService() (*RecoveryQueueService, *mockRecoveryRepo) {
	repo := newMockRecoveryRepo()
	return NewRecoveryQueueServiceWithRepo(repo), repo
}

func mustEnqueueWithContent(t *testing.T, svc *RecoveryQueueService, customerID, content string) *model.RecoveryQueue {
	t.Helper()
	item, err := svc.Enqueue(context.Background(), &RecoveryEnqueueInput{CustomerID: customerID, Content: content})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	return item
}

// ---------------------------------------------------------------- 开关与参数

func TestParseRecoveryWorkerMode(t *testing.T) {
	cases := []struct {
		raw  string
		want RecoveryWorkerMode
	}{
		{"", RecoveryWorkerModeOff},
		{"off", RecoveryWorkerModeOff},
		{"FALSE", RecoveryWorkerModeOff},
		{"0", RecoveryWorkerModeOff},
		{"shadow", RecoveryWorkerModeShadow},
		{" dry_run ", RecoveryWorkerModeShadow},
		{"enforce", RecoveryWorkerModeEnforce},
		{"SEND", RecoveryWorkerModeEnforce},
		// 布尔真值只到 shadow：给真人群发消息必须在 env 里写出 enforce 这个词
		{"true", RecoveryWorkerModeShadow},
		{"1", RecoveryWorkerModeShadow},
		{"yes", RecoveryWorkerModeShadow},
		// 认不得的值不能默认开
		{"yesplease", RecoveryWorkerModeOff},
		{"shadoww", RecoveryWorkerModeOff},
	}
	for _, c := range cases {
		if got := parseRecoveryWorkerMode(c.raw); got != c.want {
			t.Errorf("parseRecoveryWorkerMode(%q) = %s，期望 %s", c.raw, got, c.want)
		}
	}
}

func TestNewRecoveryQueueWorker_EnvKnobs(t *testing.T) {
	t.Run("未设置时取默认", func(t *testing.T) {
		t.Setenv(RecoveryWorkerFlagEnv, "shadow")
		w := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{})
		if w.batch != recoveryWorkerDefaultBatch || w.interval != recoveryWorkerDefaultInterval || w.backoff != recoveryWorkerDefaultBackoff {
			t.Fatalf("默认值异常: batch=%d interval=%s backoff=%s", w.batch, w.interval, w.backoff)
		}
	})

	t.Run("非法值回落默认", func(t *testing.T) {
		t.Setenv(RecoveryWorkerFlagEnv, "enforce")
		t.Setenv(RecoveryWorkerBatchEnv, "abc")
		t.Setenv(RecoveryWorkerIntervalEnv, "12")
		t.Setenv(RecoveryWorkerBackoffEnv, "sometime")
		w := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{})
		if w.batch != recoveryWorkerDefaultBatch || w.interval != recoveryWorkerDefaultInterval || w.backoff != recoveryWorkerDefaultBackoff {
			t.Fatalf("非法值应回落默认: batch=%d interval=%s backoff=%s", w.batch, w.interval, w.backoff)
		}
	})

	t.Run("越界值回落默认", func(t *testing.T) {
		t.Setenv(RecoveryWorkerFlagEnv, "enforce")
		t.Setenv(RecoveryWorkerBatchEnv, "0")
		if got := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{}).batch; got != recoveryWorkerDefaultBatch {
			t.Fatalf("batch=0 应回落默认，实际 %d", got)
		}
		t.Setenv(RecoveryWorkerBatchEnv, "5000")
		if got := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{}).batch; got != recoveryWorkerDefaultBatch {
			t.Fatalf("batch=5000（超过 repo 上限）应回落默认，实际 %d", got)
		}
	})

	// 退避基数 ≤ 冷却窗口时，每次到期都只会先撞冷却：白耗一轮，且从日志上看不出问题。
	t.Run("退避基数不得低于冷却窗口", func(t *testing.T) {
		t.Setenv(RecoveryWorkerFlagEnv, "enforce")
		t.Setenv(RecoveryWorkerBackoffEnv, "1s")
		w := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{})
		if w.backoff <= reachCooldownWindow {
			t.Fatalf("backoff=%s 仍不大于冷却窗口 %s ⇒ 到期即撞冷却", w.backoff, reachCooldownWindow)
		}
	})

	t.Run("轮询间隔不得低于单轮下限", func(t *testing.T) {
		t.Setenv(RecoveryWorkerFlagEnv, "enforce")
		t.Setenv(RecoveryWorkerIntervalEnv, "1ms")
		w := NewRecoveryQueueWorker(nil, &fakeRecoveryReach{})
		if w.interval < recoveryWorkerMinInterval {
			t.Fatalf("interval=%s 低于下限 %s", w.interval, recoveryWorkerMinInterval)
		}
	})
}

// 冷却窗口与退避的不变量：窗口改了，退避默认值和这里的断言必须一起复核。
func TestRecoveryWorkerCooldownWindowInvariant(t *testing.T) {
	if reachCooldownWindow != 60*time.Minute {
		t.Fatalf("冷却窗口变成了 %s：请同步复核退避默认值与本断言", reachCooldownWindow)
	}
	if recoveryWorkerDefaultBackoff <= reachCooldownWindow {
		t.Fatalf("默认退避 %s 未盖过冷却窗口 %s", recoveryWorkerDefaultBackoff, reachCooldownWindow)
	}
	// 机制本身可用：同一 oneID 第二次调用即被冷却挡住
	svc := &ProactiveReachService{}
	oneID := fmt.Sprintf("uid-cooldown-%d", time.Now().UnixNano())
	if !svc.checkCooldown(context.Background(), oneID) {
		t.Fatal("首次触达不应被冷却挡住")
	}
	if svc.checkCooldown(context.Background(), oneID) {
		t.Fatal("窗口内第二次触达应被冷却挡住（否则退避与冷却的先后关系不成立）")
	}
}

// max_attempts 的列默认值与 Go 常量必须同值：入队侧靠常量，直插侧（RFM 自动入队）靠列默认。
func TestRecoveryDefaultMaxAttemptsMatchesColumnTag(t *testing.T) {
	field, ok := reflect.TypeOf(model.RecoveryQueue{}).FieldByName("MaxAttempts")
	if !ok {
		t.Fatal("model.RecoveryQueue 没有 MaxAttempts 字段")
	}
	want := fmt.Sprintf("default:%d", model.RecoveryDefaultMaxAttempts)
	if got := field.Tag.Get("gorm"); !strings.Contains(got, want) {
		t.Fatalf("列默认值与常量漂移：tag=%q 应含 %q", got, want)
	}
}

// ---------------------------------------------------------------- AC③：flag 关时不启动

func TestRecoveryQueueWorker_OffDoesNotStart(t *testing.T) {
	queue, repo := newMockQueueService()
	reach := &fakeRecoveryReach{}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeOff, queue, reach)
	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop(ctx)

	if w.Running() {
		t.Fatal("off 模式下 worker 不应启动轮询协程")
	}
	mustEnqueueWithContent(t, queue, "cust-off-1", "好久不见，回来看看")
	if n := repo.countReady(ctx, t, time.Now(), 50); n != 1 {
		t.Fatalf("前置条件：应有一条到期项，实际 %d", n)
	}

	// 即便有人绕过 Start 直接调 RunOnce，off 也必须一步都不走
	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("off 模式 RunOnce 不应报错: %v", err)
	}
	if report.Scanned != 0 || len(reach.calls) != 0 {
		t.Fatalf("off 模式仍处理了队列: report=%+v calls=%d", report, len(reach.calls))
	}
}

func TestRecoveryQueueWorker_StartRunsAndStops(t *testing.T) {
	for _, mode := range []RecoveryWorkerMode{RecoveryWorkerModeShadow, RecoveryWorkerModeEnforce} {
		t.Run(string(mode), func(t *testing.T) {
			queue, _ := newMockQueueService()
			w, _ := newTestRecoveryWorker(mode, queue, &fakeRecoveryReach{})
			ctx := context.Background()
			w.Start(ctx)
			if !w.Running() {
				t.Fatalf("%s 模式应启动", mode)
			}
			w.Start(ctx) // 幂等：不重复起协程
			w.Stop(ctx)
			if w.Running() {
				t.Fatal("Stop 后不应仍在运行")
			}
			w.Stop(ctx) // 幂等
		})
	}
}

func TestRecoveryQueueWorker_StartWithoutReachDoesNotRun(t *testing.T) {
	queue, _ := newMockQueueService()
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, nil)
	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop(ctx)
	if w.Running() {
		t.Fatal("没有触达服务时不应启动（启动后每条都会失败，不如不启）")
	}
}

// ---------------------------------------------------------------- shadow：只看不发、不记账

func TestRecoveryQueueWorker_ShadowNeverSendsOrWritesLedger(t *testing.T) {
	queue, repo := newMockQueueService()
	item := mustEnqueueWithContent(t, queue, "cust-shadow-1", "老客回归立减 30")
	reach := &fakeRecoveryReach{resp: &ProactiveReachResponse{MessageID: "dry_run", Channel: "sms", Status: "dry_run"}}
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeShadow, queue, reach)

	report, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.WouldSend != 1 || report.Sent != 0 {
		t.Fatalf("shadow 应只计 would_send: %+v", report)
	}
	if len(reach.calls) != 1 || !reach.allDryRun() {
		t.Fatalf("shadow 必须全部带 DryRun: %d 次", len(reach.calls))
	}
	if len(claim.keys) != 0 {
		t.Fatalf("shadow 什么都没发，不该占用取锁: %v", claim.keys)
	}
	got := repo.items[item.ID]
	if got.Attempts != 0 || got.Stage != model.RecoveryStageQueued || got.NextAttemptAt != nil || got.LastChannel != "" {
		t.Fatalf("shadow 不该写任何台账: attempts=%d stage=%s next=%v channel=%q",
			got.Attempts, got.Stage, got.NextAttemptAt, got.LastChannel)
	}
}

func TestRecoveryQueueWorker_ShadowCountsWouldFail(t *testing.T) {
	queue, _ := newMockQueueService()
	mustEnqueueWithContent(t, queue, "cust-shadow-2", "老客回归立减 30")
	reach := &fakeRecoveryReach{err: fmt.Errorf("%w: customer u2 has opted out on all available channels", ErrDoNotContact)}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeShadow, queue, reach)

	report, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.WouldFail != 1 || report.WouldSend != 0 || report.BlockedByDNC != 1 {
		t.Fatalf("shadow 失败口径异常: %+v", report)
	}
	if report.Sent != 0 {
		t.Fatal("shadow 永不计 sent")
	}
}

// ---------------------------------------------------------------- AC①：到期项被消费并记账

func TestRecoveryQueueWorker_EnforceRecordsAttemptChannelAndResult(t *testing.T) {
	queue, repo := newMockQueueService()
	item := mustEnqueueWithContent(t, queue, "cust-send-1", "老客回归立减 30")
	reach := &fakeRecoveryReach{resp: &ProactiveReachResponse{MessageID: "sms_777", Channel: "sms", Status: "sent"}}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)

	report, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 1 {
		t.Fatalf("应发出 1 条: %+v", report)
	}
	if len(reach.calls) != 1 || reach.calls[0].DryRun {
		t.Fatal("enforce 必须真发（DryRun=false）")
	}
	if reach.calls[0].Content != "老客回归立减 30" || reach.calls[0].CustomerID != "cust-send-1" {
		t.Fatalf("文案/收件人未透传: %+v", reach.calls[0])
	}
	got := repo.items[item.ID]
	if got.Attempts != 1 {
		t.Errorf("attempts 应 +1，实际 %d", got.Attempts)
	}
	if got.LastChannel != "sms" {
		t.Errorf("last_channel 应为 sms，实际 %q", got.LastChannel)
	}
	if got.LastResult != "sent:sms_777" {
		t.Errorf("last_result 异常: %q", got.LastResult)
	}
	if got.Stage != model.RecoveryStageQueued {
		t.Errorf("还有下次机会时应留在 queued，实际 %s", got.Stage)
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.After(time.Now()) {
		t.Errorf("应排好下次触达时间，实际 %v", got.NextAttemptAt)
	}
}

func TestRecoveryQueueWorker_EnforceConsumesDueItemsOnly(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	due := mustEnqueueWithContent(t, queue, "cust-due-1", "文案一")
	later := mustEnqueueWithContent(t, queue, "cust-due-2", "文案二")
	future := time.Now().Add(2 * time.Hour)
	repo.items[later.ID].NextAttemptAt = &future

	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, &fakeRecoveryReach{})
	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Scanned != 1 || report.Sent != 1 {
		t.Fatalf("只应消费到期项: %+v", report)
	}
	if repo.items[due.ID].Attempts != 1 {
		t.Errorf("到期项应记 1 次: %d", repo.items[due.ID].Attempts)
	}
	if repo.items[later.ID].Attempts != 0 {
		t.Errorf("未到期项不该被碰: %d", repo.items[later.ID].Attempts)
	}
}

// ---------------------------------------------------------------- 文案：没有就不发，也不烧次数

func TestRecoveryQueueWorker_NoContentSkipsWithoutConsumingAttempt(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	silent, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{CustomerID: "cust-nocopy"})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	noisy := mustEnqueueWithContent(t, queue, "cust-copy", "有文案的那条")
	reach := &fakeRecoveryReach{}
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.SkippedNoContent != 1 || report.Sent != 1 {
		t.Fatalf("应跳过 1 条无文案、发出 1 条: %+v", report)
	}
	if len(reach.calls) != 1 || reach.calls[0].Content != "有文案的那条" {
		t.Fatalf("无文案项不该外发: %+v", reach.calls)
	}
	if len(claim.keys) != 1 {
		t.Fatalf("无文案项不该取锁（什么都没发）: %v", claim.keys)
	}
	got := repo.items[silent.ID]
	if got.Attempts != 0 {
		t.Errorf("跳过不该消耗尝试次数: %d", got.Attempts)
	}
	// 关键：无文案项必须被推离队首。ListReadyForAttempt 按 next_attempt_at ASC NULLS FIRST 排序，
	// 不推后它就永久压在有文案的项前面，配上单轮上限等于把整条队列饿死。
	if got.NextAttemptAt == nil || !got.NextAttemptAt.After(time.Now()) {
		t.Errorf("跳过项必须被推后，实际 next_attempt_at=%v", got.NextAttemptAt)
	}
	if repo.items[noisy.ID].Attempts != 1 {
		t.Errorf("有文案项应正常记账: %d", repo.items[noisy.ID].Attempts)
	}
}

func TestRecoveryQueueWorker_BadMetaJSONTreatedAsNoContent(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{CustomerID: "cust-badmeta"})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	repo.items[item.ID].MetaJSON = "{not json"

	reach := &fakeRecoveryReach{}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce 不该因单条坏 JSON 整体失败: %v", err)
	}
	if report.SkippedNoContent != 1 || len(reach.calls) != 0 {
		t.Fatalf("坏 JSON 应按无文案跳过: %+v calls=%d", report, len(reach.calls))
	}
}

func TestRecoveryEnqueue_MetaRoundTrip(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{
		CustomerID:        "cust-meta",
		Content:           "文案",
		Subject:           "主题",
		TemplateID:        "TPL-9",
		Params:            map[string]string{"name": "小王"},
		PreferredChannels: []string{"email"},
	})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if item.MaxAttempts != model.RecoveryDefaultMaxAttempts {
		t.Fatalf("MaxAttempts 应落到默认值，实际 %d", item.MaxAttempts)
	}
	raw := repo.items[item.ID].MetaJSON
	msg, err := parseRecoveryMeta(raw)
	if err != nil {
		t.Fatalf("解析入队写出的 meta_json 失败: %v (raw=%s)", err, raw)
	}
	if msg.Content != "文案" || msg.Subject != "主题" || msg.TemplateID != "TPL-9" ||
		msg.Params["name"] != "小王" || len(msg.PreferredChannels) != 1 || msg.PreferredChannels[0] != "email" {
		t.Fatalf("meta 往返丢字段: %+v (raw=%s)", msg, raw)
	}

	// 无文案 ⇒ meta_json 留空，由 GORM 省略该列、落到列默认值 '{}'
	bare, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{CustomerID: "cust-bare"})
	if err != nil {
		t.Fatalf("入队: %v", err)
	}
	if bare.MetaJSON != "" {
		t.Fatalf("无文案不该写 meta_json，实际 %q", bare.MetaJSON)
	}
	m, err := parseRecoveryMeta("")
	if err != nil || m.Content != "" {
		t.Fatalf("空 meta_json 应解析为空文案: %+v err=%v", m, err)
	}
}

// ---------------------------------------------------------------- AC④：单轮上限

func TestRecoveryQueueWorker_PerRoundCap(t *testing.T) {
	queue, _ := newMockQueueService()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{
			CustomerID: fmt.Sprintf("cust-cap-%d", i),
			Content:    "文案",
		}); err != nil {
			t.Fatalf("入队: %v", err)
		}
	}
	reach := &fakeRecoveryReach{}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	w.batch = 2

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Scanned != 2 || report.Sent != 2 {
		t.Fatalf("单轮上限未生效: %+v", report)
	}
	if len(reach.calls) != 2 {
		t.Fatalf("单轮外发次数应等于上限，实际 %d", len(reach.calls))
	}
}

// ---------------------------------------------------------------- 取锁

func TestRecoveryQueueWorker_ClaimHeldSkips(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item := mustEnqueueWithContent(t, queue, "cust-claim", "文案")
	reach := &fakeRecoveryReach{}
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	claim.held[recoveryClaimKey(item.ID)] = true

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.ClaimHeld != 1 || len(reach.calls) != 0 {
		t.Fatalf("锁被别的副本持有时不该发: %+v calls=%d", report, len(reach.calls))
	}
	if repo.items[item.ID].Attempts != 0 {
		t.Fatal("没抢到锁也不该记账")
	}
}

// 取锁失败时必须**不发**（fail-closed）。触达侧的冷却是 fail-open 的，
// 两处口径相反是刻意的：这里守的是"同一条别发两遍"。
func TestRecoveryQueueWorker_ClaimErrorFailsClosed(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item := mustEnqueueWithContent(t, queue, "cust-claim-err", "文案")
	reach := &fakeRecoveryReach{}
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	claim.err = errors.New("cache down")

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.ClaimUnavailable != 1 || len(reach.calls) != 0 {
		t.Fatalf("取锁出错时必须拒发: %+v calls=%d", report, len(reach.calls))
	}
	if repo.items[item.ID].Attempts != 0 {
		t.Fatal("取锁出错不该留下尝试记录")
	}
}

func TestRecoveryQueueWorker_ClaimKeyIsPerItem(t *testing.T) {
	queue, _ := newMockQueueService()
	ctx := context.Background()
	a := mustEnqueueWithContent(t, queue, "cust-key-a", "文案")
	b := mustEnqueueWithContent(t, queue, "cust-key-b", "文案")
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, &fakeRecoveryReach{})
	if _, err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := append([]string(nil), claim.keys...)
	sort.Strings(got)
	want := []string{recoveryClaimKey(a.ID), recoveryClaimKey(b.ID)}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("取锁键应按项隔离: got=%v want=%v", got, want)
	}
}

// ---------------------------------------------------------------- 失败分类

func TestRecoveryQueueWorker_DNCMarksCancelled(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item := mustEnqueueWithContent(t, queue, "cust-dnc", "文案")
	reach := &fakeRecoveryReach{err: fmt.Errorf("%w: customer x opted out on all available channels", ErrDoNotContact)}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.BlockedByDNC != 1 || report.Failed != 0 {
		t.Fatalf("退订不该记成发送失败: %+v", report)
	}
	got := repo.items[item.ID]
	if got.Stage != model.RecoveryStageCancelled {
		t.Errorf("退订应终止为 cancelled，实际 %s", got.Stage)
	}
	if got.LastResult != "blocked_do_not_contact" {
		t.Errorf("last_result 异常: %q", got.LastResult)
	}
	if got.Attempts != 1 {
		t.Errorf("仍应留下一次触达尝试记录: %d", got.Attempts)
	}
	if n := repo.countReady(ctx, t, time.Now(), 50); n != 0 {
		t.Fatalf("cancelled 项不该再进到期集，实际 %d", n)
	}
}

func TestRecoveryQueueWorker_CooldownDefersWithoutConsumingAttempt(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item := mustEnqueueWithContent(t, queue, "cust-cooldown", "文案")
	reach := &fakeRecoveryReach{err: fmt.Errorf("%w: customer x recently received a message, please wait", ErrReachCooldown)}
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.BlockedByCooldown != 1 || report.Failed != 0 || report.Sent != 0 {
		t.Fatalf("冷却口径异常: %+v", report)
	}
	got := repo.items[item.ID]
	if got.Attempts != 0 {
		t.Errorf("什么都没发出去，不该消耗尝试次数: %d", got.Attempts)
	}
	if got.NextAttemptAt == nil || got.NextAttemptAt.Before(time.Now().Add(reachCooldownWindow)) {
		t.Errorf("冷却重试必须排在冷却窗口之后，实际 %v", got.NextAttemptAt)
	}
	if got.LastResult != "" {
		t.Errorf("冷却不该写 last_result（什么都没发）: %q", got.LastResult)
	}
}

func TestRecoveryQueueWorker_SendErrorRetriesThenFails(t *testing.T) {
	cases := []struct {
		name      string
		attempts  int
		wantStage string
	}{
		{"尚有剩余", 0, model.RecoveryStageQueued},
		{"最后一次失败", 2, model.RecoveryStageFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			queue, repo := newMockQueueService()
			ctx := context.Background()
			item := mustEnqueueWithContent(t, queue, "cust-err-"+c.name, "文案")
			repo.items[item.ID].Attempts = c.attempts
			reach := &fakeRecoveryReach{err: errors.New("sms gateway 502")}
			w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)

			report, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if report.Failed != 1 {
				t.Fatalf("应记 1 次失败: %+v", report)
			}
			got := repo.items[item.ID]
			if got.Stage != c.wantStage {
				t.Errorf("stage=%s 期望 %s", got.Stage, c.wantStage)
			}
			if got.Attempts != c.attempts+1 {
				t.Errorf("attempts=%d 期望 %d", got.Attempts, c.attempts+1)
			}
			if c.wantStage == model.RecoveryStageQueued && (got.NextAttemptAt == nil || !got.NextAttemptAt.After(time.Now())) {
				t.Errorf("失败重试应排退避，实际 %v", got.NextAttemptAt)
			}
			if !strings.HasPrefix(got.LastResult, "error:") {
				t.Errorf("last_result 应带错误来源: %q", got.LastResult)
			}
		})
	}
}

func TestRecoveryQueueWorker_LastSuccessfulAttemptMarksRunning(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	item := mustEnqueueWithContent(t, queue, "cust-last", "文案")
	repo.items[item.ID].Attempts = item.MaxAttempts - 1
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, &fakeRecoveryReach{})

	if _, err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := repo.items[item.ID]
	// 发完最后一次 ⇒ running（已触达、等结果）：既不再重发，也不算"失败"或"已挽回"
	if got.Stage != model.RecoveryStageRunning {
		t.Fatalf("stage=%s 期望 running", got.Stage)
	}
	if got.NextAttemptAt != nil {
		t.Errorf("机会用尽后不该再排期: %v", got.NextAttemptAt)
	}
	if n := repo.countReady(ctx, t, time.Now(), 50); n != 0 {
		t.Fatalf("running 项不该再进到期集，实际 %d", n)
	}
}

func TestRecoveryQueueWorker_BackoffIsExponential(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	w, claim := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, &fakeRecoveryReach{err: errors.New("boom")})
	item := mustEnqueueWithContent(t, queue, "cust-backoff", "文案")
	seen := make([]time.Duration, 0, 3)
	for round := 0; round < 3; round++ {
		repo.items[item.ID].Stage = model.RecoveryStageQueued
		repo.items[item.ID].NextAttemptAt = nil
		// 每轮清空取锁表 = 模拟租约到期；不清的话第二轮会被"锁被占"挡掉，测的就不是退避了
		claim.held = make(map[string]bool)
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		next := repo.items[item.ID].NextAttemptAt
		if next == nil {
			t.Fatalf("round %d 未排期", round)
		}
		seen = append(seen, next.Sub(w.nowFunc()).Round(time.Minute))
	}
	if seen[1] <= seen[0] || seen[2] <= seen[1] {
		t.Fatalf("退避应逐轮变长: %v", seen)
	}
	if repo.items[item.ID].Stage != model.RecoveryStageFailed {
		t.Fatalf("3 次用尽后应为 failed，实际 %s", repo.items[item.ID].Stage)
	}
}

// last_result 列是 varchar(255)：超长会让"记一笔失败"这件事本身失败，这条项就再没人管了。
func TestRecoveryQueueWorker_TruncatesResultToFitColumn(t *testing.T) {
	long := strings.Repeat("失败原因", 300) // 3 字节/字：按字节砍会劈开字符
	got := truncateRecoveryResult("error:" + long)
	if len(got) > recoveryResultMaxLen+3 {
		t.Fatalf("截断后仍 %d 字节", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("截断劈开了 UTF-8 字符，PG 会报 invalid byte sequence")
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("应带省略号: %q", got[len(got)-8:])
	}
	if short := truncateRecoveryResult("sent:ok"); short != "sent:ok" {
		t.Fatalf("未超长不该改动: %q", short)
	}
}

// ---------------------------------------------------------------- 台账写失败不拖垮整轮

func TestRecoveryQueueWorker_LedgerWriteFailureDoesNotStopRound(t *testing.T) {
	queue, repo := newMockQueueService()
	ctx := context.Background()
	first := mustEnqueueWithContent(t, queue, "cust-ledger-1", "文案")
	second := mustEnqueueWithContent(t, queue, "cust-ledger-2", "文案")
	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, &fakeRecoveryReach{})
	repo.failMarkAttemptOn = first.ID

	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("单条台账写失败不该让整轮报错: %v", err)
	}
	if report.LedgerWriteFailures != 1 {
		t.Fatalf("应记 1 次台账写失败: %+v", report)
	}
	if report.Sent != 2 {
		t.Fatalf("两条都应发出（发都发了，记账失败不能把它回滚成没发）: %+v", report)
	}
	if repo.items[second.ID].Attempts != 1 {
		t.Fatal("第二条不应被第一条的失败带走")
	}
}

// ---------------------------------------------------------------- AC②：真触达服务下 DNC 零外发

func TestRecoveryQueueWorker_DNCZeroOutboundThroughRealReach(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.RecoveryQueue{},
		&model.CustomerDoNotContact{}, &model.CustomerChannel{})
	ctx := context.Background()

	seedCustomer := func(id, oneID, phone string) {
		t.Helper()
		if err := db.Create(&model.Customer{ID: id, UnifiedID: oneID, Phone: phone}).Error; err != nil {
			t.Fatalf("建客户失败: %v", err)
		}
	}
	seedCustomer("rc-dnc-1", "uid-dnc-1", "13900000001")
	seedCustomer("rc-ok-1", "uid-ok-1", "13900000002")

	dncSvc := NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db))
	if err := dncSvc.Block(ctx, "uid-dnc-1", model.DoNotContactChannelAll, "manual"); err != nil {
		t.Fatalf("写退订标志位失败: %v", err)
	}

	var outbound []string
	reach := NewProactiveReachService(db, &mockAccountLookup{})
	reach.SetDoNotContact(dncSvc)
	reach.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		return func(ctx context.Context, phone, _, _ string, _ map[string]string) (string, error) {
			outbound = append(outbound, phone)
			return "sms_test", nil
		}, nil
	})

	queue := NewRecoveryQueueServiceWithRepo(repository.NewRecoveryQueueRepositoryWithDB(db))
	for _, cid := range []string{"rc-dnc-1", "rc-ok-1"} {
		if _, err := queue.Enqueue(ctx, &RecoveryEnqueueInput{CustomerID: cid, Content: "老客回归立减 30"}); err != nil {
			t.Fatalf("入队: %v", err)
		}
	}

	w, _ := newTestRecoveryWorker(RecoveryWorkerModeEnforce, queue, reach)
	report, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// 反证：没退订的那条必须真的发出去了。少了这一句，"零外发"可能只是因为压根没接通。
	if len(outbound) != 1 || outbound[0] != "13900000002" {
		t.Fatalf("非退订客户应正常外发（否则本测试无法证明 DNC 起了作用）: %v", outbound)
	}
	if report.BlockedByDNC != 1 || report.Sent != 1 {
		t.Fatalf("处置统计异常: %+v", report)
	}

	var dncRow, okRow model.RecoveryQueue
	if err := db.Where("customer_id = ?", "rc-dnc-1").First(&dncRow).Error; err != nil {
		t.Fatalf("回读退订项: %v", err)
	}
	if dncRow.Stage != model.RecoveryStageCancelled {
		t.Errorf("退订项应终止为 cancelled，实际 %s（last_result=%q）", dncRow.Stage, dncRow.LastResult)
	}
	if dncRow.LastResult != "blocked_do_not_contact" {
		t.Errorf("退订项应留下原因: %q", dncRow.LastResult)
	}
	if err := db.Where("customer_id = ?", "rc-ok-1").First(&okRow).Error; err != nil {
		t.Fatalf("回读正常项: %v", err)
	}
	if okRow.LastChannel != "sms" || okRow.LastResult != "sent:sms_test" {
		t.Errorf("正常项应记下渠道与结果: channel=%q result=%q", okRow.LastChannel, okRow.LastResult)
	}
	// 真库口径：显式写入与列默认必须给出同一个尝试次数上限
	if okRow.MaxAttempts != model.RecoveryDefaultMaxAttempts {
		t.Errorf("max_attempts=%d 期望 %d", okRow.MaxAttempts, model.RecoveryDefaultMaxAttempts)
	}
}

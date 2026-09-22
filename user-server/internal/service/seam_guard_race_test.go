// seam_guard_race_test.go —— 传递层竞态的 accessor 腿（R28）。
//
// 这一批钉的不是"某个异步体别裸读包级全局"（那是 scripts/check-async-global-read.py 的口径，
// 已由 521e4f80 的协程前快照收口），而是**被异步链路经默认实现读到的那批全局本身**：
// 协程体调 seam 的默认实现（FetchTelegramMedia / FetchQQAttachment / sendOutbound / …），
// 默认实现里再读同文件的包级注入点。快照挡不住第二跳 —— 快照把 seam 函数本身冻进本地变量，
// 而函数体里那一句读的还是全局地址。
//
// 为什么钉 accessor 而不是补站点探针：站点探针（"跑一遍真实异步链看有没有红"）在锁被摘掉时
// 照样绿 —— 摘锁后异步体读的仍是那个全局，只是没有同步关系，而时序上未必撞上。
// 判据必须是"同一地址上的并发读写有没有被锁住"，所以每条腿直接把 load/store 同时放协程里打。
//
// 值断言只在**写方只写这一个值**、且 spawn 之前先由本协程落一次的前提下才成立（同 pkg/db 的
// TestConcurrentGetDBAndSetTestDBAreRaceFree 的教训）：否则"第一次读要看到还没被写过的值"是
// 断言自己造的时序，红的是腿不是锁。
//
// 摘掉某一家 accessor 的锁 ⇒ 静态门逐格红（16 格，见电池族 A）。`-race` 侧的证据是**一窄一全两格**
// 而不是逐格跑：第一版真按 16 格各跑一趟 `-race`，跑到第 6 格就把这台共享机器的数据盘写到 100%
// （只剩 1.7 GB）而中断，所以改成"摘一家看会不会外溢 + 全摘看有没有空腿"两格收口。
// 只摘 `tgMaxMediaBytes` 时**只有**它那条腿
// 报 DATA RACE、共锁邻居 `TelegramAPIBase` 仍绿；把 16 家全摘时 16 条腿各自报 DATA RACE，
// 且每个竞争块都只归属到自己那条腿、栈里点到本家文件。电池 `scripts/mut_seam_guard_r28.py`，
// 逐格计数见计划文档 ## R28。
// "栈里点到本家文件"这一条要靠电池对**被测包**关内联（`-gcflags=hivemtk-user/internal/service=-l`）：
// 摘锁后的 `return tgMaxMediaBytes` 默认会被内联进调用它的闭包，而写侧自 `734118d9` 起住在
// `seam_guard_setters_test.go`（golangci-lint 的 `run.tests:false` 不容 test-only 函数留在产码面），
// 两个文件都不是本家产码文件 ⇒ 不关内联就没有产码帧可点。
package service

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// hammerSeam 四条协程只写 probe、四条协程只读并校验，跑一小段。
//
// load 里可以 t.Errorf（并发用例不许 Fatalf），"这条腿到底跑没跑"由 loads 计数在末尾兜住：
// 少这一句，把腿改成空函数也能绿。
func hammerSeam(t *testing.T, name string, store, load func()) {
	t.Helper()
	store()
	var loads atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				store()
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				loads.Add(1)
				load()
			}
		}()
	}
	time.Sleep(120 * time.Millisecond)
	close(stop)
	wg.Wait()
	if loads.Load() == 0 {
		t.Fatalf("%s：读方一次都没跑到，这条腿没断言任何东西", name)
	}
}

func TestSeamGuard_TelegramAPIBaseIsLocked(t *testing.T) {
	const probe = "http://127.0.0.1:0/probe"
	prev := loadTGAPIBase()
	t.Cleanup(func() { storeTGAPIBase(prev) })
	hammerSeam(t, "tgAPIBaseOverride",
		func() { storeTGAPIBase(probe) },
		func() {
			if v := loadTGAPIBase(); v != probe {
				t.Errorf("loadTGAPIBase = %q, want 探针值（写方只写这一个值）", v)
			}
		})
}

func TestSeamGuard_TelegramMaxBytesIsLocked(t *testing.T) {
	const probe int64 = 4096
	prev := loadTGMaxMediaBytes()
	t.Cleanup(func() { storeTGMaxMediaBytes(prev) })
	hammerSeam(t, "tgMaxMediaBytes",
		func() { storeTGMaxMediaBytes(probe) },
		func() {
			if v := loadTGMaxMediaBytes(); v != probe {
				t.Errorf("loadTGMaxMediaBytes = %d, want %d", v, probe)
			}
		})
}

func TestSeamGuard_QQAttachmentGuardIsLocked(t *testing.T) {
	sentinel := errors.New("probe guard")
	probe := func(string) error { return sentinel }
	prev := loadQQAttachmentURLGuard()
	t.Cleanup(func() { storeQQAttachmentURLGuard(prev) })
	hammerSeam(t, "qqAttachmentURLGuard",
		func() { storeQQAttachmentURLGuard(probe) },
		func() {
			if err := loadQQAttachmentURLGuard()("https://example.com/a.jpg"); !errors.Is(err, sentinel) {
				t.Errorf("取到的不是探针策略：err = %v", err)
			}
		})
}

func TestSeamGuard_QQMaxBytesIsLocked(t *testing.T) {
	const probe int64 = 8192
	prev := loadQQMaxMediaBytes()
	t.Cleanup(func() { storeQQMaxMediaBytes(prev) })
	hammerSeam(t, "qqMaxMediaBytes",
		func() { storeQQMaxMediaBytes(probe) },
		func() {
			if v := loadQQMaxMediaBytes(); v != probe {
				t.Errorf("loadQQMaxMediaBytes = %d, want %d", v, probe)
			}
		})
}

func TestSeamGuard_DingTalkOpenAPIBaseIsLocked(t *testing.T) {
	const probe = "http://127.0.0.1:0/probe"
	prev := loadDingtalkOpenAPIBase()
	t.Cleanup(func() { storeDingtalkOpenAPIBase(prev) })
	hammerSeam(t, "dingtalkOpenAPIBase",
		func() { storeDingtalkOpenAPIBase(probe) },
		func() {
			if v := loadDingtalkOpenAPIBase(); v != probe {
				t.Errorf("loadDingtalkOpenAPIBase = %q, want 探针值", v)
			}
		})
}

func TestSeamGuard_DouyinAPIBaseIsLocked(t *testing.T) {
	const probe = "http://127.0.0.1:0/probe"
	prev := loadDyAPIBaseOverride()
	t.Cleanup(func() { storeDyAPIBaseOverride(prev) })
	hammerSeam(t, "dyAPIBaseOverride",
		func() { storeDyAPIBaseOverride(probe) },
		// 读方走生产真正用的那个 douyinAPIBase()，钉的是它内部的读，不是裸全局。
		func() {
			if v := douyinAPIBase(); v != probe {
				t.Errorf("douyinAPIBase = %q, want 探针值", v)
			}
		})
}

func TestSeamGuard_DouyinRetryBackoffIsLocked(t *testing.T) {
	probe := []time.Duration{3 * time.Millisecond}
	prev := loadDyMediaRetryBackoff()
	t.Cleanup(func() { storeDyMediaRetryBackoff(prev) })
	hammerSeam(t, "dyMediaRetryBackoff",
		func() { storeDyMediaRetryBackoff(probe) },
		func() {
			v := loadDyMediaRetryBackoff()
			if len(v) != 1 || v[0] != 3*time.Millisecond {
				t.Errorf("loadDyMediaRetryBackoff = %v, want [%v]", v, 3*time.Millisecond)
			}
		})
}

func TestSeamGuard_WeChatAPIBaseIsLocked(t *testing.T) {
	const probe = "http://127.0.0.1:0/probe"
	prev := loadWechatAPIBase()
	t.Cleanup(func() { storeWechatAPIBase(prev) })
	hammerSeam(t, "wechatAPIBase",
		func() { storeWechatAPIBase(probe) },
		func() {
			if v := loadWechatAPIBase(); v != probe {
				t.Errorf("loadWechatAPIBase = %q, want 探针值", v)
			}
		})
}

func TestSeamGuard_AIReplyQuietHoursFnIsLocked(t *testing.T) {
	probe := func(time.Time) bool { return true }
	prev := loadAIReplyQuietHoursFn()
	t.Cleanup(func() { storeAIReplyQuietHoursFn(prev) })
	hammerSeam(t, "aiReplyQuietHoursFn",
		func() { storeAIReplyQuietHoursFn(probe) },
		func() {
			if !loadAIReplyQuietHoursFn()(time.Now()) {
				t.Error("取到的不是探针策略（探针恒 true）")
			}
		})
}

func TestSeamGuard_DingTalkWebhookHostAllowedIsLocked(t *testing.T) {
	probe := func(*url.URL) bool { return true }
	prev := loadDingtalkWebhookHostAllowed()
	t.Cleanup(func() { storeDingtalkWebhookHostAllowed(prev) })
	hammerSeam(t, "dingtalkWebhookHostAllowed",
		func() { storeDingtalkWebhookHostAllowed(probe) },
		func() {
			if !loadDingtalkWebhookHostAllowed()(&url.URL{Host: "evil.example"}) {
				t.Error("取到的不是探针策略（探针恒 true）")
			}
		})
}

func TestSeamGuard_BridgeOnlineProbeIsLocked(t *testing.T) {
	probe := func(context.Context, string, string) bool { return true }
	prev := loadBridgeChannelOnlineProbe()
	t.Cleanup(func() { storeBridgeChannelOnlineProbe(prev) })
	hammerSeam(t, "bridgeChannelOnlineProbe",
		func() { storeBridgeChannelOnlineProbe(probe) },
		func() {
			fn := loadBridgeChannelOnlineProbe()
			if fn == nil || !fn(context.Background(), "qq", "1") {
				t.Error("取到的不是探针（探针恒 true）")
			}
		})
}

func TestSeamGuard_ApprovalNowFnIsLocked(t *testing.T) {
	probe := time.Unix(1_700_000_000, 0)
	fn := func() time.Time { return probe }
	prev := loadApprovalNowFn()
	t.Cleanup(func() { storeApprovalNowFn(prev) })
	hammerSeam(t, "approvalNowFn",
		func() { storeApprovalNowFn(fn) },
		func() {
			if got := loadApprovalNowFn()(); !got.Equal(probe) {
				t.Errorf("时钟 = %v, want %v", got, probe)
			}
		})
}

func TestSeamGuard_ApprovalResumeTokFnIsLocked(t *testing.T) {
	want := errors.New("probe rand")
	fn := func() (string, error) { return "", want }
	prev := loadApprovalResumeTokFn()
	t.Cleanup(func() { storeApprovalResumeTokFn(prev) })
	hammerSeam(t, "approvalResumeTokFn",
		func() { storeApprovalResumeTokFn(fn) },
		func() {
			if _, err := loadApprovalResumeTokFn()(); !errors.Is(err, want) {
				t.Errorf("随机源 = %v, want 探针错误", err)
			}
		})
}

func TestSeamGuard_HumanTaskNowFnIsLocked(t *testing.T) {
	probe := time.Unix(1_600_000_000, 0)
	fn := func() time.Time { return probe }
	prev := loadHumanTaskNowFn()
	t.Cleanup(func() { storeHumanTaskNowFn(prev) })
	hammerSeam(t, "humanTaskNowFn",
		func() { storeHumanTaskNowFn(fn) },
		func() {
			if got := loadHumanTaskNowFn()(); !got.Equal(probe) {
				t.Errorf("时钟 = %v, want %v", got, probe)
			}
		})
}

func TestSeamGuard_IntentEnabledIsLocked(t *testing.T) {
	prev := loadIntentEnabled()
	t.Cleanup(func() { storeIntentEnabled(prev) })
	hammerSeam(t, "IntentEnabled",
		func() { storeIntentEnabled(true) },
		func() {
			if !loadIntentEnabled() {
				t.Error("loadIntentEnabled = false, want true（写方只写 true）")
			}
		})
}

func TestSeamGuard_PollingLockRepoIsLocked(t *testing.T) {
	probe := repository.NewTelegramPollingLockRepositoryWithDB(&gorm.DB{})
	restore := resetPollingLockRepoForTest(probe)
	t.Cleanup(restore)
	hammerSeam(t, "pollingLockRepo/pollingLockRepoOnce",
		func() { resetPollingLockRepoForTest(probe)() },
		func() {
			if got := getPollingLockRepo(); got != probe {
				t.Error("取到的不是探针 repo")
			}
		})
}

// seam_guard_setters_test.go —— seam 全局的写侧 accessor，只给测试装桩用（R28 的补充收口）。
//
// 为什么不在各渠道的生产文件里：`user-server/.golangci.yml` 是 `run.tests: false` + 启用 `unused`，
// 于是「只被 `_test.go` 调用的函数」在生产面上就是死代码 —— 这 14 扇 setter 随 `734118d9` 落在
// 生产文件里时，CI 的 golangci-lint 步当场报 14 条 unused（父笔 1713110b 同一作业 0 条），
// 而本地 `make audit` 不含 golangci-lint，所以推上去之前没人看见。
// 本包已有的先例就是这个处置：`email_open_tracker_test.go` 的 `resetPixelCacheForTest`、
// `tool_integration_config_test.go` 的 `resetSecretsForTest` 都定义在测试侧。
//
// 锁仍在生产文件里（`tgSeamMu` 等）：竞态判据是「同一个全局的每一次读和每一次写都被同一把锁
// synchronize」，函数定义在哪个文件与这条不变量无关。`scripts/check-seam-guard.py` 按注册表
// setter 列的 `文件:` 前缀到这里找它，摘掉这里任何一把锁 ⇒ 门红 + 对应 `-race` 腿红
// （牙齿见 `scripts/mut_seam_guard_r28.py`）。
package service

import (
	"net/url"
	"sync"
	"time"

	"hivemtk-user/internal/repository"
)

func storeApprovalNowFn(fn func() time.Time) {
	approvalSeamMu.Lock()
	defer approvalSeamMu.Unlock()
	approvalNowFn = fn
}

func storeApprovalResumeTokFn(fn func() (string, error)) {
	approvalSeamMu.Lock()
	defer approvalSeamMu.Unlock()
	approvalResumeTokFn = fn
}

func storeDingtalkOpenAPIBase(base string) {
	dingtalkSeamMu.Lock()
	defer dingtalkSeamMu.Unlock()
	dingtalkOpenAPIBase = base
}

func storeDyMediaRetryBackoff(backoff []time.Duration) {
	dySeamMu.Lock()
	defer dySeamMu.Unlock()
	dyMediaRetryBackoff = backoff
}

func storeDyAPIBaseOverride(base string) {
	dySeamMu.Lock()
	defer dySeamMu.Unlock()
	dyAPIBaseOverride = base
}

func storeHumanTaskNowFn(fn func() time.Time) {
	humanTaskSeamMu.Lock()
	defer humanTaskSeamMu.Unlock()
	humanTaskNowFn = fn
}

func storeQQAttachmentURLGuard(guard func(string) error) {
	qqSeamMu.Lock()
	defer qqSeamMu.Unlock()
	qqAttachmentURLGuard = guard
}

func storeQQMaxMediaBytes(limit int64) {
	qqSeamMu.Lock()
	defer qqSeamMu.Unlock()
	qqMaxMediaBytes = limit
}

func storeTGAPIBase(base string) {
	tgSeamMu.Lock()
	defer tgSeamMu.Unlock()
	tgAPIBaseOverride = base
}

func storeTGMaxMediaBytes(limit int64) {
	tgSeamMu.Lock()
	defer tgSeamMu.Unlock()
	tgMaxMediaBytes = limit
}

// resetPollingLockRepoForTest 测试装桩：锁内一次完成「换仓储 + 让下一次取用看到它」，
// 返回还原函数（同 pkg/db 的 SetTestDB 一条路走 accessor 的口径）。
func resetPollingLockRepoForTest(repo *repository.TelegramPollingLockRepository) func() {
	pollingLockMu.Lock()
	defer pollingLockMu.Unlock()
	prev, prevOnce := pollingLockRepo, pollingLockRepoOnce
	pollingLockRepo = repo
	pollingLockRepoOnce = &sync.Once{}
	pollingLockRepoOnce.Do(func() { pollingLockRepo = repo })
	return func() {
		pollingLockMu.Lock()
		defer pollingLockMu.Unlock()
		pollingLockRepo, pollingLockRepoOnce = prev, prevOnce
	}
}

func storeAIReplyQuietHoursFn(fn func(time.Time) bool) {
	aiReplyQuietHoursMu.Lock()
	defer aiReplyQuietHoursMu.Unlock()
	aiReplyQuietHoursFn = fn
}

func storeDingtalkWebhookHostAllowed(fn func(*url.URL) bool) {
	dingtalkHostMu.Lock()
	defer dingtalkHostMu.Unlock()
	dingtalkWebhookHostAllowed = fn
}

func storeWechatAPIBase(base string) {
	wechatSeamMu.Lock()
	defer wechatSeamMu.Unlock()
	wechatAPIBase = base
}

package app

import (
	"context"
	"time"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	opssvc "hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/security"
	"hivemtk-user/internal/pkg/textutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/translation"
)

// WireMiscConfigParams 把 misc 组里「代码里已有常量、参数中心已登记」的 16 条接上线，
// 返回接上的参数键（"group.key"，按注入顺序）。
//
// 为什么集中在一个文件而不是就地塞进各包：这 16 个点位横跨 model / repository /
// middleware / pkg / ops 这些**不能反过来依赖 service** 的下层包（model 引 service
// 是跨层，middleware 被 service 引、pkg 是叶子包），就地接线只能靠每个包里各造一个
// 包级函数变量 + setter，那正是这 16 个点位各自的实现。装配层是唯一能同时看见
// 上下层的地方，读参数中心的动作就都落在这里，各包只留"读当前值"的函数。
//
// 每条的口径统一为：**参数读不到 / 没配 / 配了非法值 → 走该点位原有的代码兜底值**。
// 兜底值与种子 DefaultValue 逐条相等（见各点位注释），所以"有没有这条参数行"
// 不改变任何现有行为——这是让接线本身可回归的前提。
//
// 为什么键名要在下一行重复写一遍（wired）：各包自己的用例只能证明"注入了桩以后读取点
// 认不认"，证明不了这一层有没有把 group/key 写错——写错了照样注得进去，只是永远读到
// 兜底值。把键名交出去，启动日志与 internal/app 的用例就能逐条对账。
//
// 为什么每条都把 GetXxx 与键名写在同一行：scripts/check-config-param-readpoints.py
// 认的就是这个形状（同一行里既有 Get* 又有键名）。把 Get* 藏进一个通用闭包里，
// 16 条会一起变成"未接线但未声明"——门变红，而人看不出是哪一层出了问题。
//
// 这里不接 ctx 参数：各 provider 是**延迟**被读的（首次请求时），如果捕获装配时的
// ctx，那通常是个已经取消的请求上下文，所有点位会跟着失效。统一用
// context.Background()，与 SeedConfigParams 里 llmpkg/platform 两个 getter 同一写法。
func WireMiscConfigParams() []string {
	cp := service.GlobalConfigParam()
	bg := context.Background()
	var wired []string

	security.SetVisitorTokenTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "visitor_token_ttl", security.DefaultVisitorTokenTTL)
	})
	wired = append(wired, "misc.visitor_token_ttl")

	controller.SetSSOCookieTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "sso_cookie_ttl", controller.SSOCookieTTL)
	})
	wired = append(wired, "misc.sso_cookie_ttl")

	repository.SetPollingLockStaleThresholdProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "polling_lock_stale_threshold", repository.PollingLockStaleThreshold)
	})
	wired = append(wired, "misc.polling_lock_stale_threshold")

	service.SetDomainCheckConcurrencyProvider(func() int {
		return cp.GetInt(bg, "misc", "domain_check_concurrency", 16)
	})
	wired = append(wired, "misc.domain_check_concurrency")

	model.SetSmsMaxRetryProvider(func() int {
		return cp.GetInt(bg, "misc", "sms_max_retry", model.DefaultSmsMaxRetry)
	})
	wired = append(wired, "misc.sms_max_retry")

	opssvc.SetCSVExportMaxRowsProvider(func() int {
		return cp.GetInt(bg, "misc", "csv_export_max_rows", opssvc.CSVExportMaxRows)
	})
	wired = append(wired, "misc.csv_export_max_rows")

	textutil.SetMaxBytesProvider(func() int {
		return cp.GetInt(bg, "misc", "text_truncate_max_bytes", textutil.DefaultMaxBytes)
	})
	wired = append(wired, "misc.text_truncate_max_bytes")

	repository.SetPreviewMaxLenProvider(func() int {
		return cp.GetInt(bg, "misc", "preview_max_len", 500)
	})
	wired = append(wired, "misc.preview_max_len")

	service.SetSOPSchedulerScanIntervalProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "sop_scheduler_interval", 60*time.Second)
	})
	wired = append(wired, "misc.sop_scheduler_interval")

	service.SetSOPTimerMaxWaitProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "sop_max_wait", 24*time.Hour)
	})
	wired = append(wired, "misc.sop_max_wait")

	translation.SetDeeplTimeoutProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "deepl_timeout", 30*time.Second)
	})
	wired = append(wired, "misc.deepl_timeout")

	service.SetReplySemTimeoutProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "reply_sem_timeout", 5*time.Second)
	})
	wired = append(wired, "misc.reply_sem_timeout")

	middleware.SetOwnershipCacheTTLProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "ownership_cache_ttl", 5*time.Second)
	})
	wired = append(wired, "misc.ownership_cache_ttl")

	service.SetSummaryStaleThresholdProvider(func() time.Duration {
		return cp.GetDuration(bg, "misc", "summary_stale_threshold", 10*time.Minute)
	})
	wired = append(wired, "misc.summary_stale_threshold")

	service.SetAgentLoopHistoryMaxCandidatesProvider(func() int {
		return cp.GetInt(bg, "misc", "agentloop_history_max_candidates", 200)
	})
	wired = append(wired, "misc.agentloop_history_max_candidates")

	controller.SetBackupPageSizeCapProvider(func() int {
		return cp.GetInt(bg, "misc", "backup_page_size", controller.DefaultBackupPageSizeCap)
	})
	wired = append(wired, "misc.backup_page_size")

	return wired
}

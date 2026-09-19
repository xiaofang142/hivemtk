package service

import (
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

// A6（批3·2026-09-19）：browser_automation 服务端全部超时/时限预算收口单表。
// 纪律：不做配置化（YAGNI）——这些值要么是真机实测结论（session179/132/188），
// 要么是协议推导（客户端自计时 + 服务端宽限），配置化只会得到另一份没人校准的漂移源。
// 出处以注释钉死；改值必须同步真机验证，别在调用点就地加字面量。

// —— Hand → Host 命令帧往返超时（hand.go）——
const (
	// defaultCmdTimeout 常规命令（click/type/snapshot/query/scroll/extract…）默认预算；
	// 约束3 的基线值（D4 期定，无单命令 p99 实测，宽松侧）。
	defaultCmdTimeout = 30 * time.Second
	// handMarkdownTimeout 整页 Markdown 序列化：大 DOM 页遍历+拼接，取默认 2 倍。
	handMarkdownTimeout = 60 * time.Second
	// handConditionGrace 「客户端自计时」命令（wait_for_selector/assert/comment_verify）
	// 的服务端附加宽限：服务端预算必须大于扩展侧 timeout_ms，否则先于扩展回包假超时
	// （与 B4 假超时→重发→重复消息同源风险）。
	handConditionGrace = 10 * time.Second
	// handWaitGrace wait 固定等待的宽限（ms + 5s），同上防卡点假超时。
	handWaitGrace = 5 * time.Second
	// handCommentSendTimeout 三段式唯一不可逆提交点，45s 对齐旧一站式 post_comment 预算。
	// session179 真机实证：重页（小红书评论区渲染）CDP 事件逐条 round-trip 可达秒级，
	// 30s 实测触发假超时（发送实际成功但回包迟于超时）。发送结果未知时归因交
	// finalize 回查，绝不重发。
	handCommentSendTimeout = 45 * time.Second
)

// —— Host WS 连接生命周期（host_registry.go）——
// D4a/G4：半开 TCP（VPN 抖动/睡眠唤醒）无应用层探测时，Request 挂满命令超时且
// 断连清理钩子不触发。ping/pong + 读超时把半开变确定事件。
const (
	hostPingInterval  = 30 * time.Second
	hostReadTimeout   = 90 * time.Second // 容忍 2 个 ping 周期丢帧
	hostWriteDeadline = 10 * time.Second // 单帧写时限（ping/JSON 同值）
	// hostDisconnectHookTimeout 断连钩子（该用户 running session 置 failed）的独立 ctx 预算。
	hostDisconnectHookTimeout = 10 * time.Second
	// hostServProbeTimeout 注册期可服务探针（F3）的等回包时限。取值依据：探针帧在扩展侧是
	// 纯 chrome.tabs.get(0) 早退（不发 CDP、不等页面），正常耗时 <10ms；这里给的余量全部
	// 让给「SW 冷启/刚被 Chrome 唤醒」这类合法慢路径——探针误判的代价是白重启一次 host，
	// 而探针时限过短的代价是把可用 Host 拖进重启循环，故宁松不紧（远小于用户可见的 30s 命令超时）。
	hostServProbeTimeout = 10 * time.Second
)

// —— 会话收敛与看门狗（executor.go / task.go / executor_selfheal.go / cron.go）——
const (
	// sessionFinalWriteBudget 终态收口写库预算。R25/session188 实测：executeBrain 因
	// ctx 超时收敛返回后 ctx 已 Done，用原 ctx 写终态会被 DB 驱动取消，session 永久
	// 停留 active——终态必须走 WithoutCancel 的独立时限。
	sessionFinalWriteBudget = 120 * time.Second
	// taskWatchdogGrace wall-clock 兜底宽限：全链预算 = TimeoutSec + 30s。
	// R22/session132 实测：ctx 取消链在某些 LLM/DB 调用栈不生效（11min+ active），
	// 以真实时钟兜底保证会话必有终态。
	taskWatchdogGrace = 30 * time.Second
	// relocateLLMWatchdog A1 selector 自愈 LLM 调用看门狗：DispatchStructured 内部
	// 无硬超时，goroutine 悬挂会拖死整轮，独立 120s 判负（降级为原 selector 报错）。
	relocateLLMWatchdog = 120 * time.Second
	// cronTriggerOpTimeout cron 触发回调内「再校验 enabled + RunTask」的 DB 操作预算
	//（脱离请求 ctx，后台触发无上游超时可用）。
	cronTriggerOpTimeout = 30 * time.Second
	// sessionTabCleanupBudget 会话收口时回收自身 tab 的单条命令预算。执行 ctx 此时通常已
	// 取消（超时/中止腿必然如此），必须走 WithoutCancel；close_tab 在扩展侧是一次
	// chrome.tabs.remove，正常 <50ms，给 15s 全留给「SW 冷启 + WS 换发」。
	sessionTabCleanupBudget = 15 * time.Second
	// confirmWaitDefault / confirmWaitMaxSec 批8：D7 人工确认等待的独立预算与上限。
	// 解耦理由（真机实测形态）：确认等待此前兼职在 task.TimeoutSec 上——「人还没看到待确认，
	// 任务先被执行预算掐死」和「确认占用的时间把执行预算吃光」是同一枚硬币的两面。
	confirmWaitDefault = 600 * time.Second
	confirmWaitMaxSec  = 900
)

// confirmWaitBudget 任务生效的确认等待时长（0/负数=默认 600s；上限由 dto binding 与
// Publish 校验夹紧，这里不再二次夹紧——预算读不到夹紧值本身就是接线缺陷，宁可跑出可见的长等）。
func confirmWaitBudget(t *model.BrowserTask) time.Duration {
	if t != nil && t.ConfirmWaitSec > 0 {
		return time.Duration(t.ConfirmWaitSec) * time.Second
	}
	return confirmWaitDefault
}

// taskExecBudget 一次执行的 wall-clock 预算 = TimeoutSec（自动化本身）+ D7 确认等待（另计）。
// 三处消费必须同源：RunTask 的 execCtx、SafeGoDetached 的外层看门狗、Brain 循环的真实时钟兜底。
// 各写各的口径就是批8 要收的口子——曾经「确认中」的任务会被看门狗按 TimeoutSec 掐死。
func taskExecBudget(t *model.BrowserTask) time.Duration {
	d := time.Duration(t.TimeoutSec) * time.Second
	if t.RequireConfirm {
		d += confirmWaitBudget(t)
	}
	return d
}

// —— 写台账落库（write_ledger.go）——
// ledgerWriteBudget 单行 submit_state UPDATE 的预算。执行 ctx 此刻常已 Done
// （「send 刚跨越、execCtx 恰好到期」正是最需要留台账的一刻），故走 WithoutCancel；
// 单行更新正常 <5ms，给的 3s 全留给连接池重取，再长就不如让步自己失败。
const ledgerWriteBudget = 3 * time.Second

// —— 拦截页探测（executor.go detectBlockedIfFatal）——
// blockDetectBudget 一次拦截检测的整段预算（snapshot + 弹层选择器逐个 query 共用）。
// session236 实测：失败步本身已耗满 30s 命令超时，检测再叠一条满额 30s 命令，单步失败
// 拖到 60s+，且检测命令自身的超时会计入 A5「连续 2 条命令超时」僵尸判定，把步失败误升级
// 成 Host 假死自愈。检测是增强不是闸门，给短预算拿得到就算证据、拿不到就放行。
const blockDetectBudget = 8 * time.Second

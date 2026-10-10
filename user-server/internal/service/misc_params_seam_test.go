package service

import (
	"testing"
	"time"
)

// 这一组六条（域名检测并发 / SOP 扫描间隔 / SOP Timer 上限 / 回复语义超时 /
// Dashboard 摘要过期 / AgentLoop 历史候选）都落在 service 包内，共用同一套
// "provider + 非正数回落兜底" 口径，所以用两张表（duration / int）一次验完：
// ① 兜底值恒等于参数中心 DefaultValue；② nil 注入视为不注入；③ 注入生效；
// ④ 非法值（非正）回落兜底。

type durationSeam struct {
	name    string
	param   string
	def     time.Duration
	restore func() // 返回恢复 provider 的闭包，由 setup 记录
	read    func() time.Duration
	set     func(func() time.Duration)
}

type intSeam struct {
	name    string
	param   string
	def     int
	restore func()
	read    func() int
	set     func(func() int)
}

func TestMiscDurationSeams(t *testing.T) {
	seams := []durationSeam{
		{
			name:  "misc.sop_scheduler_interval",
			param: "sop_scheduler_interval",
			def:   60 * time.Second,
			restore: func() func() {
				old := sopSchedulerScanIntervalProvider
				return func() { sopSchedulerScanIntervalProvider = old }
			}(),
			read: soPSchedulerScanInterval,
			set:  SetSOPSchedulerScanIntervalProvider,
		},
		{
			name:  "misc.sop_max_wait",
			param: "sop_max_wait",
			def:   24 * time.Hour,
			restore: func() func() {
				old := sopTimerMaxWaitProvider
				return func() { sopTimerMaxWaitProvider = old }
			}(),
			read: sopTimerMaxWait,
			set:  SetSOPTimerMaxWaitProvider,
		},
		{
			name:  "misc.reply_sem_timeout",
			param: "reply_sem_timeout",
			def:   5 * time.Second,
			restore: func() func() {
				old := replySemTimeoutProvider
				return func() { replySemTimeoutProvider = old }
			}(),
			read: replySemTimeout,
			set:  SetReplySemTimeoutProvider,
		},
		{
			name:  "misc.summary_stale_threshold",
			param: "summary_stale_threshold",
			def:   10 * time.Minute,
			restore: func() func() {
				old := summaryStaleThresholdProvider
				return func() { summaryStaleThresholdProvider = old }
			}(),
			read: summaryStaleThreshold,
			set:  SetSummaryStaleThresholdProvider,
		},
	}
	for _, s := range seams {
		t.Run(s.name, func(t *testing.T) {
			t.Cleanup(s.restore)

			if got := s.read(); got != s.def {
				t.Fatalf("未注入时 = %v，与参数中心 %s 的 DefaultValue（%v）不一致", got, s.param, s.def)
			}

			s.set(nil)
			if got := s.read(); got != s.def {
				t.Fatalf("nil 注入时 = %v，期望回落 %v", got, s.def)
			}

			s.set(func() time.Duration { return 77 * time.Second })
			if got := s.read(); got != 77*time.Second {
				t.Fatalf("注入后 = %v，期望 77s", got)
			}

			s.set(func() time.Duration { return 0 })
			if got := s.read(); got != s.def {
				t.Fatalf("注入 0 时 = %v，期望回落 %v", got, s.def)
			}
		})
	}
}

func TestMiscIntSeams(t *testing.T) {
	seams := []intSeam{
		{
			name:  "misc.domain_check_concurrency",
			param: "domain_check_concurrency",
			def:   16,
			restore: func() func() {
				old := domainCheckConcurrencyProvider
				return func() { domainCheckConcurrencyProvider = old }
			}(),
			read: domainCheckConcurrency,
			set:  SetDomainCheckConcurrencyProvider,
		},
		{
			name:  "misc.agentloop_history_max_candidates",
			param: "agentloop_history_max_candidates",
			def:   200,
			restore: func() func() {
				old := agentLoopHistoryMaxCandidatesProvider
				return func() { agentLoopHistoryMaxCandidatesProvider = old }
			}(),
			read: agentLoopHistoryMaxCandidates,
			set:  SetAgentLoopHistoryMaxCandidatesProvider,
		},
	}
	for _, s := range seams {
		t.Run(s.name, func(t *testing.T) {
			t.Cleanup(s.restore)

			if got := s.read(); got != s.def {
				t.Fatalf("未注入时 = %v，与参数中心 %s 的 DefaultValue（%d）不一致", got, s.param, s.def)
			}

			s.set(nil)
			if got := s.read(); got != s.def {
				t.Fatalf("nil 注入时 = %v，期望回落 %d", got, s.def)
			}

			s.set(func() int { return 3 })
			if got := s.read(); got != 3 {
				t.Fatalf("注入后 = %v，期望 3", got)
			}

			// 0 是这几个点位里最危险的非法值：并发度 0 会让 make(chan, 0) 死锁，
			// 候选数 0 会让召回恒空。所以非正数必须回落，不能"照用"。
			s.set(func() int { return 0 })
			if got := s.read(); got != s.def {
				t.Fatalf("注入 0 时 = %v，期望回落 %d", got, s.def)
			}
		})
	}
}

// TestOpportunityReplySemTimeoutIsIndependent 商机分支的 30 秒是另一个口径，
// 不跟着 misc.reply_sem_timeout 走：那条参数描述的是"回复语义分析超时"，
// 商机链路要等的是另一段时间，混成一条会让改一个影响两个。
func TestOpportunityReplySemTimeoutIsIndependent(t *testing.T) {
	if opportunityReplySemTimeout == defaultReplySemTimeout {
		t.Fatal("商机分支的 30 秒与默认 5 秒是两条独立口径，不该相等后无法区分")
	}
	if opportunityReplySemTimeout != 30*time.Second {
		t.Fatalf("商机分支兜底 = %v，改动请连注释一起改", opportunityReplySemTimeout)
	}
}

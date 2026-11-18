package service

// outbound_reclaim_cron.go 出站回收巡检（2026-11-19 SSE 租约缺口修复，自立项巡检轮）。
//
// 形态与 channel_interaction_expiry_cron / retention_cleanup_cron 对齐：
// Ticker + utils.SafeGo + 成对 Start/Stop，过 check-naked-goroutine.sh。
// 为什么需要它：超时回收原本只活在拉取路径（ClaimPendingOutbound）里，桥接扩展切
// SSE 下发后不再调 GET /api/bridge/outbox——认领超时的行没有任何调用点回收，
// 生产实证出现过 inflight 挂 43 分钟（xiaohongshu）与自 10-22 挂起的存量行（douyin）。
// 认领必须有租约回收，无论下行走哪条通道：本任务周期跑 ReclaimOutboundSweep
// （到界升级终态 / 超时 inflight → pending / 陈旧非终态 → expired 三步纯回收）。

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type OutboundReclaimTask struct {
	repo         *repository.MessageHubRepository
	interval     time.Duration
	claimTimeout time.Duration
	staleAfter   time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
	nowFn        func() time.Time
}

// OutboundStaleAfterDefault 陈旧非终态老化阈值：超过该年龄仍 pending/inflight 的出站行
// 视为「会话已离开、回复永远送不出去」，巡检期升级 expired 终态（outbound_stale_expired）。
// 七天口径：桥端会话列表的可见窗口（页面/登录态存续）远短于此，能送达的早就送达了。
const OutboundStaleAfterDefault = 7 * 24 * time.Hour

func NewOutboundReclaimTask(repo *repository.MessageHubRepository) *OutboundReclaimTask {
	return &OutboundReclaimTask{
		repo:         repo,
		interval:     time.Minute,
		claimTimeout: InboxOutboundClaimTimeout,
		staleAfter:   OutboundStaleAfterDefault,
		stopCh:       make(chan struct{}),
		nowFn:        time.Now,
	}
}

// RunOnce 跑一轮三步回收，返回 (超时回收数, 到界升级数, 陈旧老化数)。
// 全表回收语义本就是跨租户的（租户绑死某一个会漏看别家的积压行）——
// 用 SystemScope 过隔离层（先例：telegram polling 锁/lookup；隔离层对
// system scope 直通放行，多租户 fail-closed 语义不变：业务路径仍必须带租户）。
func (t *OutboundReclaimTask) RunOnce(ctx context.Context) (reclaimed, exhausted, aged int64, err error) {
	if t == nil || t.repo == nil {
		return 0, 0, 0, nil
	}
	return t.repo.ReclaimOutboundSweep(tenant.WithSystemScope(ctx), t.claimTimeout, t.staleAfter)
}

func (t *OutboundReclaimTask) Start(ctx context.Context) {
	if t == nil || t.repo == nil {
		return
	}
	t.wg.Add(1)
	utils.SafeGo(ctx, "outbound_reclaim_cron.run", t.run)
}

func (t *OutboundReclaimTask) run(ctx context.Context) {
	defer t.wg.Done()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			r, e, a, err := t.RunOnce(ctx)
			if err != nil {
				logger.Errorf("[OutboundReclaim] 回收巡检失败（下轮重试）: %v", err)
				continue
			}
			// 每轮必打（2026-11-19 巡检批）：零命中轮也要留痕——ticker 存活性与
			// 「第二轮起零命中」的可观测性靠它，静默会让巡检死活无从判定。
			logger.Infof("[OutboundReclaim] 回收巡检: reclaimed=%d exhausted=%d aged=%d", r, e, a)
		}
	}
}

func (t *OutboundReclaimTask) Stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	t.wg.Wait()
}

var (
	outboundReclaimTask *OutboundReclaimTask
	outboundReclaimOnce sync.Once
)

// StartOutboundReclaimAfterDBInit DB 就绪后的装配点（cmd/api main.go 在 db.AutoMigrate
// 之后调用）。**不能学先例在 init() 里装配**：Go init 先于 main 的 db.InitDB，repo 拿到
// nil 句柄 → sweep 永远空转（2026-11-19 线上实证：巡检批上线后陈旧积压纹丝不动）。
// 幂等：Once 保证多次调用只启动一份。
func StartOutboundReclaimAfterDBInit() {
	outboundReclaimOnce.Do(func() {
		outboundReclaimTask = NewOutboundReclaimTask(repository.NewMessageHubRepository())
		outboundReclaimTask.Start(context.Background())
	})
}

// OutboundReclaimForTest 暴露当前全局任务（仅测试用）。
func OutboundReclaimForTest() *OutboundReclaimTask { return outboundReclaimTask }

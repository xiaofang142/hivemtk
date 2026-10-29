package service

// reach_send_pipeline_quiethours.go 全渠道静默时段守卫（R-4）：
// CST 22:00-8:00 窗口判断、次日开始时间计算与进程内延迟重发队列。

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

var globalQuietHoursOnce sync.Once

func GetGlobalQuietHoursQueue() *MemoryQuietHoursQueue {
	globalQuietHoursOnce.Do(func() {
		globalQuietHoursQueue = NewMemoryQuietHoursQueue()
	})
	return globalQuietHoursQueue
}

var globalQuietHoursQueue *MemoryQuietHoursQueue

// 默认窗口常量（R2 参数化后的代码兜底值）。种子 reach.quiet_hours_start/_end 的
// DefaultValue 与这里逐项相等，行为零变更。reach 管线（触达）与 webhook AI 回复免打扰
// （webhook_outbound.go 的 aiReplyQuiet*）是两条不同管道，各有一组键，禁止合并。
const (
	quietHoursStartHour = 22
	quietHoursEndHour   = 8
)

// reachQuietHoursStart / reachQuietHoursEnd 每请求读参数中心（60s TTL 缓存），
// 越界（<0 或 >23）回落代码默认。读取点形状：GetInt( 与 group/key 字面量同一行，
// 满足 check-config-param-readpoints.py 硬闸。scope=全局（config_params 无租户维）。
func reachQuietHoursStart(ctx context.Context) int {
	v := GlobalConfigParam().GetInt(ctx, "reach", "quiet_hours_start", quietHoursStartHour)
	if v < 0 || v > 23 {
		return quietHoursStartHour
	}
	return v
}

func reachQuietHoursEnd(ctx context.Context) int {
	v := GlobalConfigParam().GetInt(ctx, "reach", "quiet_hours_end", quietHoursEndHour)
	if v < 0 || v > 23 {
		return quietHoursEndHour
	}
	return v
}

// inQuietHoursWindow 判定 t 是否落在静默窗口。zone = 判定所用时区：
// 静默窗口按**收件客户当地**时区算（J-12 裁决），配额/合规域调用方传 cstZone。
func inQuietHoursWindow(t time.Time, startHour, endHour int, zone *time.Location) bool {
	if zone == nil {
		zone = cstZone
	}
	h := t.In(zone).Hour()
	if startHour > endHour {
		return h >= startHour || h < endHour
	}
	return h >= startHour && h < endHour
}

// nextQuietHoursRelease 下一次窗口放开的绝对时刻（按 zone 当地日历算，
// 跨日按 zone 当地的零点边界 +AddDate——两时区日期可能差一天，不能混算）。
func nextQuietHoursRelease(t time.Time, endHour int, zone *time.Location) time.Time {
	if zone == nil {
		zone = cstZone
	}
	local := t.In(zone)
	release := time.Date(local.Year(), local.Month(), local.Day(), endHour, 0, 0, 0, zone)
	if !local.Before(release) {
		release = release.AddDate(0, 0, 1)
	}
	return release
}

// SendQuietHoursDeferrer R-4：quiet hours 命中后的延迟入队接口
type SendQuietHoursDeferrer interface {
	Defer(ctx context.Context, req *ReachSendRequest, sendAt time.Time) error
}

// MemoryQuietHoursQueue 进程内 quiet hours 延迟队列（R-4 最小实现）。
// Defer 入队；Start 启动后台循环将到期消息经原 pipeline 重发。
// 注意：进程重启丢队（与 MemorySendAuditLogger 同级语义）；多实例共享需迁 Redis ZSET，量级未到不做。
type MemoryQuietHoursQueue struct {
	mu      sync.Mutex
	items   []deferredSendItem
	wake    chan struct{}
	started atomic.Bool
}

type deferredSendItem struct {
	req    *ReachSendRequest
	sendAt time.Time
}

func NewMemoryQuietHoursQueue() *MemoryQuietHoursQueue {
	return &MemoryQuietHoursQueue{wake: make(chan struct{}, 1)}
}

// Defer 实现 SendQuietHoursDeferrer
func (q *MemoryQuietHoursQueue) Defer(_ context.Context, req *ReachSendRequest, sendAt time.Time) error {
	q.mu.Lock()
	q.items = append(q.items, deferredSendItem{req: req, sendAt: sendAt})
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	logger.Warnf("[R-4] 触达命中全渠道 quiet hours，进入延迟队列 channel=%s recipient=%s send_at=%s",
		req.Channel, req.RecipientID, sendAt.Format(time.RFC3339))
	return nil
}

// Len 当前队列长度（测试/运维）
func (q *MemoryQuietHoursQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Start 启动到期重发循环；每秒扫描一次，到期项经 pipeline.Send 重发。
func (q *MemoryQuietHoursQueue) Start(ctx context.Context, pipeline SendPipeline) {
	if !q.started.CompareAndSwap(false, true) {
		return
	}

	utils.SafeGo(ctx, "reach_send_pipeline.quiet_hours_queue", func(ctx context.Context) {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-ticker.C:
			}
			due := time.Now()
			var pending []deferredSendItem
			q.mu.Lock()
			kept := q.items[:0]
			for _, it := range q.items {
				if it.sendAt.After(due) {
					kept = append(kept, it)
				} else {
					pending = append(pending, it)
				}
			}
			q.items = kept
			q.mu.Unlock()
			for _, it := range pending {
				pipeline.Send(ctx, it.req)
			}
		}
	})
}

package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// MessageHubSummaryAggregationService message_hub → msg_hourly_summary 小时聚合。
//
// 五层边界（架构 §3.2）：本服务只持仓储，不持 *gorm.DB。原构造器收 db 只为
// 建一个仓储，另外存下来供租户扇出用——而 tenant.Fanout 的 db 形参实际未被使用
// （扇出只认 ctx 里的租户绑定），所以句柄整体去掉，装配点给仓储即可。
type MessageHubSummaryAggregationService struct {
	repo      repository.MessageHubSummaryRepository
	batchSize int
}

// NewMessageHubSummaryAggregationService 由装配点注入仓储（句柄到仓储的转换在装配层完成）。
func NewMessageHubSummaryAggregationService(repo repository.MessageHubSummaryRepository) *MessageHubSummaryAggregationService {
	return &MessageHubSummaryAggregationService{
		repo:      repo,
		batchSize: 50000,
	}
}

// RunOnce 消费自上次水位线以来的全部新消息并累加进 summary 表。
// 返回本轮消费的消息行数。repo 未注入返回 (0, nil) 静默跳过。
func (s *MessageHubSummaryAggregationService) RunOnce(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, nil
	}
	wm, err := s.repo.LoadWatermark(ctx, model.SummarySourceMessageHub)
	if err != nil {
		return 0, err
	}
	var consumed int64
	for {
		rows, err := s.repo.LoadBatchSince(ctx, wm, s.batchSize)
		if err != nil {
			return consumed, err
		}
		if len(rows) == 0 {
			return consumed, nil
		}
		deltas, maxID := aggregateHubRows(rows)
		if err := s.repo.UpsertIncrementBatch(ctx, model.SummarySourceMessageHub, maxID, deltas); err != nil {
			return consumed, err
		}
		wm = maxID
		consumed += int64(len(rows))
		if len(rows) < s.batchSize {
			return consumed, nil
		}
	}
}

type summaryKey struct {
	bucket     time.Time
	merchantID uint
	platform   string
}

func aggregateHubRows(rows []model.MessageHub) ([]repository.MsgHourlyDelta, int64) {
	type acc struct {
		delta repository.MsgHourlyDelta
		sess  map[string]struct{}
	}
	accs := make(map[summaryKey]*acc)
	var maxID int64
	for _, h := range rows {
		if int64(h.ID) > maxID {
			maxID = int64(h.ID)
		}
		k := summaryKey{
			bucket:     h.CreatedAt.Truncate(time.Hour),
			merchantID: 0,
			platform:   h.Platform,
		}
		a := accs[k]
		if a == nil {
			a = &acc{delta: repository.MsgHourlyDelta{
				HourBucket: k.bucket,
				MerchantID: k.merchantID,
				Platform:   k.platform,
			}, sess: make(map[string]struct{})}
			accs[k] = a
		}
		a.delta.MessageCount++
		if h.IsAIReply {
			// CS-04 观测口径：转接公告与 AI 回复同行走同一条出库链，桥接出站行统一
			// 打 IsAIReply=true / scenario=auto_reply 常量——但公告不是 AI 应答
			// （SampleB 实测形态：低置信转人工轮的那条「已转人工」提示）。行上
			// handler_type=human（HandleResultToContext 注入，审计分流的凭据）即公告
			// /人工提示，不计 AICount；也不冒充 HumanCount（坐席没回话），只随
			// MessageCount 口径走。AI 回复行 handler_type=ai，照旧计数。
			if ht, _ := h.Extra["handler_type"].(string); ht != string(model.HandlerTypeHuman) {
				a.delta.AICount++
			}
		} else if h.Direction == "outbound" {
			a.delta.HumanCount++
		}
		if h.ConversationID != "" {
			a.sess[h.ConversationID] = struct{}{}
		}
	}
	deltas := make([]repository.MsgHourlyDelta, 0, len(accs))
	for _, a := range accs {
		a.delta.SessionCount = int64(len(a.sess))
		deltas = append(deltas, a.delta)
	}
	return deltas, maxID
}

type hubSummaryAggCron struct {
	svc      *MessageHubSummaryAggregationService
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

var hubSummaryAggCronInst *hubSummaryAggCron

// StartMessageHubSummaryAggCron 由 main 显式调用（原 init() 副作用装配的是 nil DB：
// repo 非空但句柄为空 ⇒ RunOnce 每 5 分钟报一次 invalid db，水位线永不推进，
// msg_hourly_summary 在任何真实进程里从未产出行）。
// repo 为 nil（装配点没拿到 DB）时不装配：宁可没有汇总，也不要再起一个只会报错的哑 cron。
func StartMessageHubSummaryAggCron(repo repository.MessageHubSummaryRepository) {
	if repo == nil || hubSummaryAggCronInst != nil {
		return
	}
	hubSummaryAggCronInst = startHubSummaryAggCron(NewMessageHubSummaryAggregationService(repo))
}

func startHubSummaryAggCron(svc *MessageHubSummaryAggregationService) *hubSummaryAggCron {
	if svc == nil {
		return nil
	}
	c := &hubSummaryAggCron{svc: svc, stopCh: make(chan struct{})}
	c.wg.Add(1)
	go c.run(context.Background())
	return c
}

func (c *hubSummaryAggCron) run(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			// 多租户模式下，需以租户扇出方式逐租户执行聚合。
			// Fanout 的 db 形参不参与任何查询（隔离靠 ctx 里的租户绑定，
			// 见 pkg/tenant/fanout.go），服务层因此没有理由再握一个句柄传进来。
			if tenant.IsMultiTenant() {
				_, _ = tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
					return c.svc.RunOnce(ctx)
				})
			} else {
				c.trigger(ctx)
			}
		}
	}
}

func (c *hubSummaryAggCron) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[hub_summary_agg_cron] panic: %v", r)
		}
	}()
	n, err := c.svc.RunOnce(ctx)
	if err != nil {
		logger.Errorf("[hub_summary_agg_cron] run failed: %v", err)
		return
	}
	if n > 0 {
		logger.Infof("[hub_summary_agg_cron] aggregated %d message_hub rows", n)
	}
}

// StopMessageHubSummaryAggCron 进程退出时由 main 调用（与 Start 成对，配合 defer）。
func StopMessageHubSummaryAggCron(ctx context.Context) {
	inst := hubSummaryAggCronInst
	if inst == nil {
		return
	}
	inst.stopOnce.Do(func() { close(inst.stopCh) })
	done := make(chan struct{})
	// 协程体只读这个本地快照：包级全局是测试会改写的注入点，
	// 若在这里仍写 hubSummaryAggCronInst，等的那把 wg 可能已不是刚关闭的那把。
	go func() { inst.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

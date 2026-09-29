package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type MessageHubSummaryAggregationService struct {
	repo repository.MessageHubSummaryRepository

	batchSize int
}

func NewMessageHubSummaryAggregationService(database *gorm.DB) *MessageHubSummaryAggregationService {
	return &MessageHubSummaryAggregationService{
		repo:      repository.NewMessageHubSummaryRepository(database),
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
			a.delta.AICount++
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
// database 为空时不装配：宁可没有汇总，也不要再起一个只会报错的哑 cron。
func StartMessageHubSummaryAggCron(database *gorm.DB) {
	if database == nil || hubSummaryAggCronInst != nil {
		return
	}
	hubSummaryAggCronInst = startHubSummaryAggCron(NewMessageHubSummaryAggregationService(database))
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
			c.trigger(ctx)
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

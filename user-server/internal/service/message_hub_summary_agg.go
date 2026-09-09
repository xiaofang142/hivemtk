package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type MessageHubSummaryAggregationService struct {
	repo repository.MessageHubSummaryRepository

	batchSize int
}

func NewMessageHubSummaryAggregationService(database *gorm.DB) *MessageHubSummaryAggregationService {
	if database == nil {
		database = db.GetDB()
	}
	return &MessageHubSummaryAggregationService{
		repo:      repository.NewMessageHubSummaryRepository(database),
		batchSize: 50000,
	}
}

// RunOnce 消费自上次水位线以来的全部新消息并累加进 summary 表。
// 返回本轮消费的消息行数。DB 未就绪返回 (0, nil) 静默跳过。
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

func init() {
	hubSummaryAggCronInst = startHubSummaryAggCron(NewMessageHubSummaryAggregationService(nil))
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

// StopMessageHubSummaryAggCron 进程退出时可调用（可选接线）。
func StopMessageHubSummaryAggCron(ctx context.Context) {
	if hubSummaryAggCronInst == nil {
		return
	}
	hubSummaryAggCronInst.stopOnce.Do(func() { close(hubSummaryAggCronInst.stopCh) })
	done := make(chan struct{})
	go func() { hubSummaryAggCronInst.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

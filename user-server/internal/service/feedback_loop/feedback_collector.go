package feedbackloop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// FeedbackCollector 反馈信号采集器
type FeedbackCollector struct {
	repo   *repository.FeedbackLoopRepository
	config FeedbackCollectorConfig
	queue  chan *dto.CollectRequest
	stopCh chan struct{}
	done   chan struct{}
}

// NewFeedbackCollector 创建采集器（启动后台 worker）
//
// 参数：
//
//	db     - GORM DB（写入 feedback_events / feedback_signals，由 repository 层使用）
//	cfg    - 配置（零值字段会用默认值填充）
func NewFeedbackCollector(db *gorm.DB, cfg FeedbackCollectorConfig) *FeedbackCollector {
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 1000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}
	if cfg.Weights == nil {
		cfg.Weights = DefaultSignalWeights
	}
	c := &FeedbackCollector{
		repo:   repository.NewFeedbackLoopRepositoryWithDB(db),
		config: cfg,
		queue:  make(chan *dto.CollectRequest, cfg.QueueSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go c.worker()
	return c
}

// Collect 异步采集（主链路调用，立即返回）
//
// 行为：
//   - 请求入队（队列满则返回 ErrQueueFull，事件丢弃但主链路不阻塞）
//   - worker 后台批量持久化 + 聚合
func (c *FeedbackCollector) Collect(ctx context.Context, req *dto.CollectRequest) error {
	if req == nil {
		return nil
	}
	if err := ValidateCollectRequest(req); err != nil {
		return err
	}
	select {
	case c.queue <- req:
		return nil
	default:
		return fmt.Errorf("%w: session=%s signal=%s", ErrQueueFull, req.SessionID, req.SignalKey)
	}
}

// CollectSync 同步采集（测试 / 关键场景使用）
//
// 立即持久化 + 聚合，不经过队列
func (c *FeedbackCollector) CollectSync(ctx context.Context, req *dto.CollectRequest) error {
	if req == nil {
		return nil
	}
	if err := ValidateCollectRequest(req); err != nil {
		return err
	}
	return c.persist(ctx, req)
}

// Stop 优雅关闭（等待 worker 刷盘剩余 batch 后退出）
func (c *FeedbackCollector) Stop() {
	close(c.stopCh)
	<-c.done
}

func (c *FeedbackCollector) worker() {
	defer close(c.done)

	batch := make([]*dto.CollectRequest, 0, c.config.BatchSize)
	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		c.flushBatch(ctx, batch)
		cancel()
		batch = batch[:0]
	}
	safeFlush := func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[feedback_collector] flush panic recovered: %v", r)
			}
		}()
		flush()
	}

	for {
		select {
		case req := <-c.queue:
			batch = append(batch, req)
			if len(batch) >= c.config.BatchSize {
				safeFlush()
			}
		case <-ticker.C:
			safeFlush()
		case <-c.stopCh:
			for {
				select {
				case req := <-c.queue:
					batch = append(batch, req)
				default:
					safeFlush()
					return
				}
			}
		}
	}
}

func (c *FeedbackCollector) flushBatch(ctx context.Context, batch []*dto.CollectRequest) {
	for _, req := range batch {
		if err := c.persist(ctx, req); err != nil {
			continue
		}
	}
}

func (c *FeedbackCollector) persist(ctx context.Context, req *dto.CollectRequest) error {
	if c.repo == nil {
		return fmt.Errorf("repo is nil")
	}
	weight := c.lookupWeight(req.SignalKey)
	reward := c.computeReward(req.SignalKey, req.SignalValue, weight)

	eventID := c.genEventID(req)
	signalValueJSON, err := json.Marshal(req.SignalValue)
	if err != nil {
		signalValueJSON = []byte("null")
	}

	event := &model.FeedbackEvent{
		EventID:           eventID,
		SessionID:         req.SessionID,
		CustomerID:        req.CustomerID,
		SOPID:             req.SOPID,
		ExecutionID:       req.ExecutionID,
		Variant:           req.Variant,
		PromptCandidateID: req.PromptCandidateID,
		EventType:         string(req.EventType),
		SignalKey:         string(req.SignalKey),
		SignalValue:       model.JSONMap{"v": json.RawMessage(signalValueJSON)},
		Weight:            weight,
		Reward:            reward,
		AIReply:           req.AIReply,
		CustomerMsg:       req.CustomerMsg,
		Metadata:          model.JSONMap(req.Metadata),
		CreatedBy:         req.CreatedBy,
	}

	breakdownJSON := fmt.Sprintf(`{"%s":1}`, string(req.SignalKey))
	sig := repository.FeedbackSignalUpsert{
		SessionID:         req.SessionID,
		CustomerID:        req.CustomerID,
		SOPID:             req.SOPID,
		Variant:           req.Variant,
		PromptCandidateID: req.PromptCandidateID,
		Reward:            reward,
		BreakdownJSON:     breakdownJSON,
	}
	return c.repo.PersistFeedback(ctx, event, sig)
}

func (c *FeedbackCollector) lookupWeight(key dto.FeedbackSignalKey) float64 {
	if w, ok := c.config.Weights[key]; ok {
		return w
	}
	return 0
}

func (c *FeedbackCollector) computeReward(key dto.FeedbackSignalKey, value any, weight float64) float64 {
	switch key {
	case dto.FBSignalRating:
		if v, ok := toFloat64(value); ok {
			return weight * (v / 5.0)
		}
	case dto.FBSignalReplyRate:
		if v, ok := toFloat64(value); ok {
			return weight * v
		}
	case dto.FBSignalDuration:
		if v, ok := toFloat64(value); ok {
			normalized := v / 300.0
			if normalized > 1 {
				normalized = 1
			}
			return weight * normalized
		}
	case dto.FBSignalToolCall:

		if v, ok := toFloat64(value); ok {
			if v > 0 {
				return weight
			}

			penalty := -weight * 1.5
			if weight < 0 {
				penalty = weight * 1.5
			}
			return penalty
		}
	case dto.FBSignalIntentMatch:

		if v, ok := toFloat64(value); ok {
			if v > 0 {
				return weight
			}
			return -weight * 1.6
		}
	}
	return weight
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1.0, true
		}
		return 0.0, true
	}
	return 0, false
}

func (c *FeedbackCollector) genEventID(req *dto.CollectRequest) string {
	now := time.Now().UnixNano()
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%d|", req.SessionID, req.SignalKey, req.CustomerMsg, now)
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		for i := 0; i < 8; i++ {
			nonce[i] = byte(now >> (i * 8))
		}
	}
	h.Write(nonce)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

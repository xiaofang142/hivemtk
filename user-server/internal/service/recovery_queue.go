package service

import (
	"errors"
	"time"

	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// RecoveryQueueService 流失挽回队列服务
type RecoveryQueueService struct {
	repo    repository.RecoveryQueueRepository
	nowFunc func() time.Time
}

// NewRecoveryQueueService 创建挽回队列服务
func NewRecoveryQueueService() *RecoveryQueueService {
	return &RecoveryQueueService{
		repo:    repository.NewRecoveryQueueRepository(),
		nowFunc: time.Now,
	}
}

// NewRecoveryQueueServiceWithRepo 测试用
func NewRecoveryQueueServiceWithRepo(r repository.RecoveryQueueRepository) *RecoveryQueueService {
	return &RecoveryQueueService{repo: r, nowFunc: time.Now}
}

// RecoveryEnqueueInput 入队参数。
//
// 为什么是结构体而不是继续加位置参数：原来 7 个参数里已有 4 个同类 string，
// 调用点写错顺序编译器不会拦（把 reason 填进 strategy 就是静默的错数据）；
// 再加文案相关字段要到 12 个，风险继续放大。
type RecoveryEnqueueInput struct {
	CustomerID string
	UnifiedID  string
	Account    string
	Reason     string
	Strategy   string
	Priority   int
	// Content 外发文案。留空 ⇒ 消费 worker 只登记不发送（见 recovery_queue_worker.go 文件头）。
	Content string
	// Subject 邮件主题（选发邮件渠道时用）。
	Subject string
	// TemplateID 渠道侧模板 ID（短信/邮件/WhatsApp 模板）。
	TemplateID string
	// Params 模板参数。
	Params map[string]string
	// PreferredChannels 期望渠道（按序）；留空则由 ProactiveReachService 自行选路。
	PreferredChannels []string
	// MaxAttempts 最大尝试次数；0 表示沿用列默认值 3。
	MaxAttempts int
}

// Enqueue 手动入队
func (s *RecoveryQueueService) Enqueue(ctx context.Context, in *RecoveryEnqueueInput) (*model.RecoveryQueue, error) {
	if in == nil {
		return nil, errors.New("入队参数不能为空")
	}
	if in.CustomerID == "" {
		return nil, errors.New("customer_id 不能为空")
	}
	priority := in.Priority
	if priority < 1 || priority > 10 {
		priority = 5
	}
	reason := in.Reason
	if reason == "" {
		reason = "churn"
	}
	strategy := in.Strategy
	if strategy == "" {
		strategy = "sms_coupon"
	}
	maxAttempts := in.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = model.RecoveryDefaultMaxAttempts
	}
	meta, err := encodeRecoveryMeta(&RecoveryMessage{
		Content:           in.Content,
		Subject:           in.Subject,
		TemplateID:        in.TemplateID,
		Params:            in.Params,
		PreferredChannels: in.PreferredChannels,
	})
	if err != nil {
		return nil, err
	}
	item := &model.RecoveryQueue{
		CustomerID:  in.CustomerID,
		UnifiedID:   in.UnifiedID,
		Account:     in.Account,
		Reason:      reason,
		Strategy:    strategy,
		Priority:    priority,
		Stage:       model.RecoveryStageQueued,
		MaxAttempts: maxAttempts,
		MetaJSON:    meta,
	}
	if err := s.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// MarkAttempt 记录一次触达尝试
//
//	stage: queued（还要再试）/ succeed / failed / cancelled
//	nextDelay: 下次重试延迟（0 表示不再排期）
func (s *RecoveryQueueService) MarkAttempt(ctx context.Context, id uint64, channel, result, stage string, nextDelay time.Duration) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}
	if stage == "" {
		stage = model.RecoveryStageFailed
	}
	if err := s.repo.MarkAttempt(ctx, id, channel, result, nextDelayPtr(s.nowFunc(), nextDelay)); err != nil {
		return err
	}
	return s.repo.MarkStage(ctx, id, stage)
}

// MarkRecovered 标记为已挽回
func (s *RecoveryQueueService) MarkRecovered(ctx context.Context, id uint64, value int64) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	now := s.nowFunc()
	item.RecoveredAt = &now
	item.RecoveryValue = value
	item.Stage = model.RecoveryStageSucceed
	return s.repo.Update(ctx, item)
}

// Cancel 取消入队
func (s *RecoveryQueueService) Cancel(ctx context.Context, id uint64) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}
	return s.repo.MarkStage(ctx, id, model.RecoveryStageCancelled)
}

// ListByStage 按阶段分页
func (s *RecoveryQueueService) ListByStage(ctx context.Context, stage string, page, pageSize int) ([]*model.RecoveryQueue, int64, error) {
	return s.repo.ListByStage(ctx, stage, page, pageSize)
}

// Distribution 阶段统计
func (s *RecoveryQueueService) Distribution(ctx context.Context) (map[string]int64, error) {
	return s.repo.CountByStage(ctx)
}

// ListReadyForAttempt 取出可触达任务
func (s *RecoveryQueueService) ListReadyForAttempt(ctx context.Context, limit int) ([]*model.RecoveryQueue, error) {
	return s.repo.ListReadyForAttempt(ctx, s.nowFunc(), limit)
}

// DeferAttempt 只把下次可触达时间推后，**不消耗** attempts。
//
// 为什么需要它：ListReadyForAttempt 的排序是 `priority ASC, next_attempt_at ASC NULLS FIRST`
// —— 没有文案、worker 只能跳过的那批项 next_attempt_at 恒为 NULL，会永久占住队首，
// 在单轮上限（AC④）下把有文案的项挤出去。跳过时必须把它们推离队首，
// 又不能记成"发送失败一次"（那会消耗掉三次机会里的一次，而什么都没发）。
func (s *RecoveryQueueService) DeferAttempt(ctx context.Context, id uint64, delay time.Duration) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}
	if delay <= 0 {
		return errors.New("delay 必须为正")
	}
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	next := s.nowFunc().Add(delay)
	item.NextAttemptAt = &next
	return s.repo.Update(ctx, item)
}

func nextDelayPtr(now time.Time, nextDelay time.Duration) *time.Time {
	if nextDelay <= 0 {
		return nil
	}
	t := now.Add(nextDelay)
	return &t
}

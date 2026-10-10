package service

// reach_pipeline.go 触达流水线核心：步骤/状态常量、错误定义、限流与重试配置类型、
// ReachPipelineService 服务结构与构造、Pipeline CRUD、步骤校验。
// 本服务没有全局单例：每个装配点各自 NewReachPipelineService(db)，
// 旧版这里的 Init/Get 一对函数在全仓没有任何调用点，Get 只会永远返回 nil，已删除。
// 按流水线阶段拆分的其余文件：
//   - reach_pipeline_alert.go     任务终态告警回调
//   - reach_pipeline_job.go       任务入队与生命周期管理、统计
//   - reach_pipeline_claim.go     任务领取、执行状态机与后台调度恢复
//   - reach_pipeline_steps.go     流水线步骤实现（内容/消息/报告/模板渲染）
//   - reach_pipeline_dispatch.go  渠道投递与发送结果跟踪
//   - reach_pipeline_ratelimit.go 频控、每日配额与全局单用户上限
//   - reach_pipeline_shared.go    触达域共享小工具（JSON 辅助/时区）

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	StepAudience = "audience"

	StepContentPrepare = "content_prepare"

	StepAccountSelect = "account_select"

	StepRateLimit = "rate_limit"

	StepMessageGen = "message_gen"

	StepSend = "send"

	StepTrackResult = "track_result"

	StepRetry = "retry"

	StepReport = "report"
)

var DefaultPipelineSteps = []string{
	StepAudience, StepContentPrepare, StepAccountSelect, StepRateLimit,
	StepMessageGen, StepSend, StepTrackResult, StepRetry, StepReport,
}

const (
	PipelineStatusActive = "active"

	PipelineStatusPaused = "paused"

	PipelineStatusArchived = "archived"
)

const (
	JobStatePending = "pending"

	JobStateRunning = "running"

	reachJobHeartbeatInterval = 1 * time.Minute

	JobStateSuccess = "success"

	JobStateFailed = "failed"

	JobStateCanceled = "canceled"

	JobStateRetrying = "retrying"

	JobStateRateLimited = "rate_limited"
)

var ReachChannels = map[string]bool{
	"wecom":       true,
	"sms":         true,
	"email":       true,
	"card":        true,
	"dingtalk":    true,
	"douyin":      true,
	"kuaishou":    true,
	"xiaohongshu": true,
	"telegram":    true,
	"whatsapp":    true,
	"feishu":      true,
}

var (
	ErrReachInvalidChannel = errors.New("invalid channel")

	ErrReachInvalidSteps = errors.New("invalid steps")

	ErrReachPipelineNotFound = errors.New("pipeline not found")

	ErrReachJobNotFound = errors.New("job not found")

	ErrReachRateLimited = errors.New("rate limit exceeded")

	ErrReachJobNotPending = errors.New("job is not pending/running")

	ErrReachInvalidPayload = errors.New("invalid payload")
)

type StepResult struct {
	Step       string         `json:"step"`
	Success    bool           `json:"success"`
	StartedAt  time.Time      `json:"started_at"`
	EndedAt    time.Time      `json:"ended_at"`
	DurationMs int            `json:"duration_ms"`
	Output     map[string]any `json:"output,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type RetryPolicy struct {
	MaxRetries      int      `json:"max_retries"`
	IntervalMs      int      `json:"interval_ms"`
	Backoff         string   `json:"backoff"`
	MaxIntervalMs   int      `json:"max_interval_ms"`
	RetryableErrors []string `json:"retryable_errors,omitempty"`
}

type RateLimitConfig struct {
	QPS          int `json:"qps"`
	Burst        int `json:"burst"`
	DailyQuota   int `json:"daily_quota"`
	PerUserLimit int `json:"per_user_limit"`
	CooldownSecs int `json:"cooldown_secs"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:    3,
		IntervalMs:    1000,
		Backoff:       "exponential",
		MaxIntervalMs: 60000,
	}
}

func DefaultRateLimit() RateLimitConfig {
	return RateLimitConfig{
		QPS:          10,
		Burst:        20,
		DailyQuota:   10000,
		PerUserLimit: 3,
		CooldownSecs: 60,
	}
}

type ReachPipelineService struct {
	repo *repository.ReachPipelineRepository

	rateMu    sync.RWMutex
	rateState map[string]*rateBucket

	globalMu      sync.Mutex
	globalHits    map[string]int
	globalLimitFn func(ctx context.Context) int
	dailyQuotaMu  sync.RWMutex
	dailyQuota    map[string]*dailyCounter
	perUserMu     sync.RWMutex
	perUserHits   map[string][]time.Time

	rateCache cache.Cache

	perUserLimitMu      sync.Mutex
	perUserLimitVal     int
	perUserLimitLoadAt  time.Time
	kvRepoOnce          sync.Once
	kvRepo              repository.SystemConfigKVRepository
	redisDegradedWarned sync.Map

	sender ReachSender

	alertHook ReachAlertHook
}

func NewReachPipelineService(db *gorm.DB) *ReachPipelineService {
	_ = db
	return &ReachPipelineService{
		repo:        repository.NewReachPipelineRepository(db),
		rateState:   make(map[string]*rateBucket),
		dailyQuota:  make(map[string]*dailyCounter),
		perUserHits: make(map[string][]time.Time),
		globalLimitFn: func(ctx context.Context) int {
			return GlobalConfigParam().GetInt(ctx, "reach", "global_per_user_daily_limit", 3)
		},
	}
}

type CreatePipelineRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Channel     string          `json:"channel" binding:"required"`
	Steps       []string        `json:"steps"`
	RetryPolicy RetryPolicy     `json:"retry_policy"`
	RateLimit   RateLimitConfig `json:"rate_limit"`
	Extra       map[string]any  `json:"extra,omitempty"`
}

func (s *ReachPipelineService) CreatePipeline(ctx context.Context, req *CreatePipelineRequest) (*model.ReachPipeline, error) {
	if s.repo == nil || !s.repo.Available() {
		return nil, fmt.Errorf("db is nil")
	}
	if !ReachChannels[req.Channel] {
		return nil, ErrReachInvalidChannel
	}
	steps := req.Steps
	if len(steps) == 0 {
		steps = DefaultPipelineSteps
	}
	if err := s.validateSteps(ctx, steps); err != nil {
		return nil, err
	}
	retryMap := model.JSONMap{}
	retryBytes, _ := json.Marshal(req.RetryPolicy)
	if err := json.Unmarshal(retryBytes, &retryMap); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RetryPolicy 失败: %v", err)
	}
	rateMap := model.JSONMap{}
	rateBytes, _ := json.Marshal(req.RateLimit)
	if err := json.Unmarshal(rateBytes, &rateMap); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RateLimit 失败: %v", err)
	}

	pipe := &model.ReachPipeline{

		Name:        req.Name,
		Description: req.Description,
		Channel:     req.Channel,
		Steps:       model.JSONArray(toIfaceSliceFromStrings(steps)),
		RetryPolicy: retryMap,
		RateLimit:   rateMap,
		Status:      PipelineStatusActive,
		Version:     1,
	}
	if pipe.Steps == nil {
		pipe.Steps = model.JSONArray{}
	}
	if pipe.RetryPolicy == nil {
		pipe.RetryPolicy = model.JSONMap{}
	}
	if pipe.RateLimit == nil {
		pipe.RateLimit = model.JSONMap{}
	}
	if err := s.repo.CreatePipeline(ctx, pipe); err != nil {
		return nil, err
	}
	return pipe, nil
}

func (s *ReachPipelineService) UpdatePipeline(ctx context.Context, id uint, req *CreatePipelineRequest) (*model.ReachPipeline, error) {
	if s.repo == nil || !s.repo.Available() {
		return nil, fmt.Errorf("db is nil")
	}
	if !ReachChannels[req.Channel] {
		return nil, ErrReachInvalidChannel
	}
	steps := req.Steps
	if len(steps) == 0 {
		steps = DefaultPipelineSteps
	}
	if err := s.validateSteps(ctx, steps); err != nil {
		return nil, err
	}
	pipe, err := s.repo.FindPipelineByID(ctx, id)
	if err != nil {
		return nil, ErrReachPipelineNotFound
	}
	retryMap := model.JSONMap{}
	retryBytes, _ := json.Marshal(req.RetryPolicy)
	if err := json.Unmarshal(retryBytes, &retryMap); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RetryPolicy 失败: %v", err)
	}
	rateMap := model.JSONMap{}
	rateBytes, _ := json.Marshal(req.RateLimit)
	if err := json.Unmarshal(rateBytes, &rateMap); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RateLimit 失败: %v", err)
	}
	pipe.Name = req.Name
	pipe.Description = req.Description
	pipe.Channel = req.Channel
	pipe.Steps = model.JSONArray(toIfaceSliceFromStrings(steps))
	pipe.RetryPolicy = retryMap
	pipe.RateLimit = rateMap
	pipe.Version++
	if err := s.repo.SavePipeline(ctx, pipe); err != nil {
		return nil, err
	}
	return pipe, nil
}

func (s *ReachPipelineService) GetPipeline(ctx context.Context, id uint) (*model.ReachPipeline, error) {
	pipe, err := s.repo.FindPipelineByID(ctx, id)
	if err != nil {
		return nil, ErrReachPipelineNotFound
	}
	return pipe, nil
}

func (s *ReachPipelineService) ListPipelines(ctx context.Context, channel, status string, page, pageSize int) ([]model.ReachPipeline, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return s.repo.ListPipelines(ctx, channel, status, page, pageSize)
}

func (s *ReachPipelineService) DeletePipeline(ctx context.Context, id uint) error {
	if _, err := s.repo.DeleteJobsByPipeline(ctx, id); err != nil {
		logger.Errorf("[reach_pipeline] 级联删除任务失败 pipeline=%d: %v", id, err)
	}
	rowsAffected, err := s.repo.DeletePipeline(ctx, id)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrReachPipelineNotFound
	}
	return nil
}

func (s *ReachPipelineService) PausePipeline(ctx context.Context, id uint) error {
	return s.repo.UpdatePipelineStatus(ctx, id, PipelineStatusPaused)
}

func (s *ReachPipelineService) ResumePipeline(ctx context.Context, id uint) error {
	return s.repo.UpdatePipelineStatus(ctx, id, PipelineStatusActive)
}

func (s *ReachPipelineService) ArchivePipeline(ctx context.Context, id uint) error {
	return s.repo.UpdatePipelineStatus(ctx, id, PipelineStatusArchived)
}

func (s *ReachPipelineService) validateSteps(ctx context.Context, steps []string) error {
	if len(steps) == 0 {
		return ErrReachInvalidSteps
	}
	allSteps := map[string]bool{}
	for _, s := range DefaultPipelineSteps {
		allSteps[s] = true
	}
	for _, st := range steps {
		if !allSteps[st] {
			return fmt.Errorf("%w: unknown step %s", ErrReachInvalidSteps, st)
		}
	}
	hasSend := false
	for _, st := range steps {
		if st == StepSend {
			hasSend = true
			break
		}
	}
	if !hasSend {
		return fmt.Errorf("%w: must include send step", ErrReachInvalidSteps)
	}
	return nil
}

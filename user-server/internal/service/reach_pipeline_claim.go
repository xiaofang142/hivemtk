package service

// reach_pipeline_claim.go 任务领取与执行状态机：ExecuteJob 抢占执行、
// executeJobCore 步骤编排与终态落库、后台调度循环（到期拉取 + stuck 恢复）、
// 自动重试的下一次执行时间计算。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *ReachPipelineService) ExecuteJob(ctx context.Context, id uint) (*model.ReachJob, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	claimed, err := s.repo.ClaimJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, ErrReachJobNotPending
	}
	return s.executeJobCore(ctx, job, false)
}

func (s *ReachPipelineService) executeJobCore(ctx context.Context, job *model.ReachJob, autoRetry bool) (*model.ReachJob, error) {
	pipe, err := s.GetPipeline(ctx, job.PipelineID)
	if err != nil {
		now := time.Now()
		job.State = JobStateFailed
		job.ErrorMessage = err.Error()
		job.CompletedAt = &now
		if e := s.repo.SaveJob(ctx, job); e != nil {
			logger.Errorf("[reach_pipeline] 失败状态落库失败 jobID=%d: %v", job.ID, e)
		}
		return job, err
	}
	steps := []string{}
	if pipe.Steps != nil {
		if err := json.Unmarshal(mustJSON(pipe.Steps), &steps); err != nil {
			logger.Errorf("[reach_pipeline] 解析 Steps 失败: %v", err)
		}
	}
	if len(steps) == 0 {
		steps = DefaultPipelineSteps
	}

	var rp RetryPolicy
	if err := json.Unmarshal(mustJSON(pipe.RetryPolicy), &rp); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RetryPolicy 失败: %v", err)
	}
	if rp.MaxRetries == 0 {
		rp = DefaultRetryPolicy()
	}
	var rl RateLimitConfig
	if err := json.Unmarshal(mustJSON(pipe.RateLimit), &rl); err != nil {
		logger.Errorf("[reach_pipeline] 解析 RateLimit 失败: %v", err)
	}

	now := time.Now()
	job.State = JobStateRunning
	job.StartedAt = &now
	if e := s.repo.SaveJob(ctx, job); e != nil {
		logger.Errorf("[reach_pipeline] 运行状态落库失败 jobID=%d: %v", job.ID, e)
	}

	hbStop := make(chan struct{})
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(reachJobHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.repo.TouchRunningJob(context.WithoutCancel(ctx), job.ID)
			}
		}
	}()
	defer func() {
		close(hbStop)
		<-hbDone
	}()

	if e := s.repo.IncrementPipelineField(ctx, pipe.ID, "total_runs", 1); e != nil {
		logger.Warnf("[reach_pipeline] total_runs 累加失败 pipeID=%d: %v", pipe.ID, e)
	}

	results := []StepResult{}
	job.StepResults = toJSONArray(mustJSON(results))

	success := true
	var firstErrStep string
	var firstErrMsg string
	for _, step := range steps {
		res := s.runStep(ctx, step, job, &rl)
		results = append(results, res)
		if !res.Success {
			if step == StepRateLimit && res.Error == ErrReachRateLimited.Error() {
				backoff := computeNextRunTime(rp, job.RetryCount+1)
				job.State = JobStateRateLimited
				job.ErrorMessage = res.Error
				job.NextRunAt = &backoff
				job.StepResults = toJSONArray(mustJSON(results))
				if e := s.repo.SaveJob(ctx, job); e != nil {
					logger.Errorf("[reach_pipeline] 限流状态落库失败 jobID=%d: %v", job.ID, e)
				}
				s.appendStepResult(ctx, job, res)
				return job, ErrReachRateLimited
			}
			if firstErrStep == "" {
				firstErrStep = step
				firstErrMsg = res.Error
			}
			success = false
			break
		}
	}

	job.StepResults = toJSONArray(mustJSON(results))

	finish := time.Now()
	job.CompletedAt = &finish
	job.DurationMs = int(finish.Sub(*job.StartedAt).Milliseconds())
	if success {
		job.State = JobStateSuccess
		job.ErrorMessage = ""
		if e := s.repo.IncrementPipelineField(ctx, pipe.ID, "total_success", 1); e != nil {
			logger.Warnf("[reach_pipeline] total_success 累加失败 pipeID=%d: %v", pipe.ID, e)
		}
	} else {
		if autoRetry && job.RetryCount < rp.MaxRetries {
			job.RetryCount++
			next := computeNextRunTime(rp, job.RetryCount)
			job.State = JobStateRetrying
			job.NextRunAt = &next
			job.ErrorMessage = fmt.Sprintf("[step=%s] %s（将自动重试 %d/%d）", firstErrStep, firstErrMsg, job.RetryCount, rp.MaxRetries)
			if e := s.repo.IncrementPipelineField(ctx, pipe.ID, "total_failure", 1); e != nil {
				logger.Warnf("[reach_pipeline] total_failure 累加失败 pipeID=%d: %v", pipe.ID, e)
			}
		} else {
			job.State = JobStateFailed
			job.ErrorMessage = fmt.Sprintf("[step=%s] %s", firstErrStep, firstErrMsg)
			if e := s.repo.IncrementPipelineField(ctx, pipe.ID, "total_failure", 1); e != nil {
				logger.Warnf("[reach_pipeline] total_failure 累加失败 pipeID=%d: %v", pipe.ID, e)
			}
			s.fireAlert(ctx, job, JobStateFailed, job.ErrorMessage)
		}
	}
	if err := s.repo.SaveJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

var reachDispatcherOnce sync.Once

func (s *ReachPipelineService) StartDispatcher(ctx context.Context, interval time.Duration) {
	reachDispatcherOnce.Do(func() {
		if interval <= 0 {
			interval = 15 * time.Second
		}
		logger.Infof("[reach_dispatcher] 启动后台任务调度器，间隔=%s", interval)
		go s.dispatchLoop(ctx, interval)
	})
}

func (s *ReachPipelineService) dispatchLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Infof("[reach_dispatcher] 调度器退出")
			return
		case <-ticker.C:

			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[reach_dispatcher] dispatchDueJobs panic recovered: %v", r)
					}
				}()
				s.dispatchDueJobs(ctx)
			}()
		}
	}
}

func (s *ReachPipelineService) dispatchDueJobs(ctx context.Context) {
	if _, err := s.repo.ResetStuckJobs(ctx, 10*time.Minute); err != nil {
		logger.Errorf("[reach_dispatcher] 恢复 stuck 任务失败: %v", err)
	}
	jobs, err := s.repo.ListDueJobs(ctx, time.Now())
	if err != nil {
		logger.Errorf("[reach_dispatcher] 拉取到期任务失败: %v", err)
		return
	}
	for i := range jobs {
		job := jobs[i]
		claimed, cerr := s.repo.ClaimJob(ctx, job.ID)
		if cerr != nil {
			logger.Errorf("[reach_dispatcher] 抢占任务 %d 失败: %v", job.ID, cerr)
			continue
		}
		if !claimed {
			continue
		}
		if _, err := s.executeJobCore(ctx, &job, true); err != nil {
			logger.Warnf("[reach_dispatcher] 执行任务 %d 失败: %v", job.ID, err)
		}
	}
}

func (s *ReachPipelineService) appendStepResult(ctx context.Context, job *model.ReachJob, res StepResult) {
	results := []StepResult{}
	if job.StepResults != nil {
		if err := json.Unmarshal(mustJSON(job.StepResults), &results); err != nil {
			logger.Errorf("[reach_pipeline] 解析 StepResults 失败: %v", err)
		}
	}
	results = append(results, res)
	data, _ := json.Marshal(results)
	job.StepResults = toJSONArray(data)
	if e := s.repo.SaveJob(ctx, job); e != nil {
		logger.Errorf("[reach_pipeline] StepResults 追加落库失败 jobID=%d: %v", job.ID, e)
	}
}

func computeNextRunTime(rp RetryPolicy, retryCount int) time.Time {
	interval := rp.IntervalMs
	if rp.Backoff == "exponential" {
		mult := 1
		for i := 0; i < retryCount; i++ {
			mult *= 2
		}
		interval = rp.IntervalMs * mult
		if rp.MaxIntervalMs > 0 && interval > rp.MaxIntervalMs {
			interval = rp.MaxIntervalMs
		}
	}
	return time.Now().Add(time.Duration(interval) * time.Millisecond)
}

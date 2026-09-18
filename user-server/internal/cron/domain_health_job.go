package cron

import (
	"context"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"time"
)

// DomainHealthCheckJob 域名健康度定时探测任务
// G 域 ：每 5 分钟探测一次所有域名，自动切换到评分最高的健康域名
type DomainHealthCheckJob struct {
	healthSvc service.DomainHealthService
	repo      repository.DomainPoolRepository
	interval  time.Duration
}

// NewDomainHealthCheckJob 创建健康度探测任务
func NewDomainHealthCheckJob(healthSvc service.DomainHealthService, repo repository.DomainPoolRepository) *DomainHealthCheckJob {
	return &DomainHealthCheckJob{
		healthSvc: healthSvc,
		repo:      repo,
		interval:  5 * time.Minute,
	}
}

// Start 启动探测循环（阻塞，应放入独立 goroutine）
func (j *DomainHealthCheckJob) Start() {
	logger.Info("[domain-health] 启动域名健康度定时探测任务")

	go j.runOnce()

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for range ticker.C {
		go j.runOnce()
	}
}

func (j *DomainHealthCheckJob) runOnce() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[domain-health] runOnce panic recovered: %v", r)
		}
	}()
	results, err := j.healthSvc.CheckAll(context.Background())
	if err != nil {
		logger.Errorf("[domain-health] 探测失败: %v", err)
		return
	}
	healthy := 0
	unhealthy := 0
	for _, r := range results {
		if r.HTTPOk && r.DNSOK && !r.OnBlacklist {
			healthy++
		} else {
			unhealthy++
		}
	}
	logger.Infof("[domain-health] 探测完成 total=%d healthy=%d unhealthy=%d", len(results), healthy, unhealthy)
}

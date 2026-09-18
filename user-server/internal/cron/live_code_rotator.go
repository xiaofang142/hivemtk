package cron

import (
	"context"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
	"time"
)

// LiveCodeRotator 活码轮询任务
type LiveCodeRotator struct {
	liveCodeService service.LiveCodeService
}

// NewLiveCodeRotator 创建活码轮询任务实例
func NewLiveCodeRotator(liveCodeService service.LiveCodeService) *LiveCodeRotator {
	return &LiveCodeRotator{
		liveCodeService: liveCodeService,
	}
}

// Start 启动活码轮询任务
func (r *LiveCodeRotator) Start() {
	go r.rotate()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		go r.rotate()
	}
}

func (r *LiveCodeRotator) rotate() {
	logger.Info("开始执行活码轮询任务...")

	err := r.liveCodeService.RotateLiveCodes(context.Background())
	if err != nil {
		logger.Errorf("活码轮询任务执行失败: %v", err)
		return
	}

	logger.Info("活码轮询任务执行完成")
}

// recovery_worker_wiring.go W-5 挽回队列消费者的装配层（新规划任务清单 T-P1-07）。
//
// 五层归属：取到期项、判触达、记台账的语义全在 service.RecoveryQueueWorker；
// 本文件只做 DI 装配（与 agent_checkpoint_wiring.go 同一口径），由 cmd/api/main.go 触发。
//
// 为什么装配放在 internal/app 而不是直接写在 main.go：
// scripts/check-unwired-assets.sh 项 6 的接线判定 scope 就是 internal/cron + internal/app
// （其余六组的装配点也都在那里）。判定 A 的原始病灶正是"能力实现完整、装配点却无人调用"，
// 所以"装配有没有落到装配层"本身就是这条基线要盯的东西——放 main.go 会让这条线一直显示
// UNWIRED，把门自己看瞎。
package app

import (
	"context"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitRecoveryWorker 装配并启动挽回队列消费 worker。
//
// 返回 nil 表示未启动（db 为 nil），调用方据此跳过 Stop。真正的开关在 worker 自己：
// FF_LTC_RECOVERY_WORKER=off（默认）时 Start 直接 no-op，队列只进不出、不产生任何外发。
//
// 触达服务在这里单独构造一份：它无状态（只有 sender 注册表 + repo），与 router 里那份
// 互不影响，避免与 HTTP 侧共享可变量。
func InitRecoveryWorker(db *gorm.DB) *service.RecoveryQueueWorker {
	if db == nil {
		logger.Warn("[RecoveryWorker] ⚠️ db 为 nil，挽回队列消费者未装配（入队照常，到期项无人消费）")
		return nil
	}
	reach := service.NewProactiveReachService(db, nil)
	service.BindProactiveReachSenders(reach, db)

	worker := service.NewRecoveryQueueWorker(service.NewRecoveryQueueService(), reach)
	worker.Start(context.Background())
	logger.Infof("[RecoveryWorker] ✅ 挽回队列消费者已装配：mode=%s（开关 %s；off 时不启动，"+
		"shadow 只试发不外发，enforce 才真发；外发受全局退订与触达频控约束）",
		worker.Mode(), service.RecoveryWorkerFlagEnv)
	return worker
}

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
	"hivemtk-user/internal/repository"
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
//
// 这里同时要装 T-P3-07 的外发闸门：worker 这条 cron 路径是"非工具外发"的第一条，
// 它与 HTTP 侧那份 service 各自独立构造，闸门少装一边就是留一条盲区
// （快照里的 attached_services 就是用来发现"只装了一边"的）。
//
// 装配顺序有硬依赖：AttachReachGate 要读 W-1 的 checker，而那个是在 router.Setup 里的
// InitGlobalToolExecutor 才建的 ⇒ 本函数**必须**在 router.Setup 之后调用（见 cmd/api/main.go
// 里那句注释）。顺序错了不会 panic，只会静默"不装门 + 一条告警"，这正是最难发现的那种失效。
//
// 生命周期（MD-02）：Construct（本函数装配）→ 依赖校验（AttachReachGate + 快照判
// DependencyUnmet）→ Start（worker.Start，旗子 off 时 service 内 no-op）→ Stop（main 的
// defer，幂等由 service.RecoveryQueueWorker.Stop 保证）。**配置关闭**（FF_LTC_REACH_GATE=off，
// 预期默认）与**必需依赖缺失**（旗子开着但 W-1 checker 没接线）不是同一失败：前者照常装配
// 只留痕，后者拒启 —— 拒启时返回 nil 且不起协程，外发恒为 0，绝不静默裸发。off 档 worker
// 本就不起协程（无裸发可言），其「依赖缺失」不拒启，装配与观测语义照旧。
func InitRecoveryWorker(db *gorm.DB) *service.RecoveryQueueWorker {
	if db == nil {
		logger.Warn("[RecoveryWorker] ⚠️ db 为 nil，挽回队列消费者未装配（入队照常，到期项无人消费）")
		return nil
	}
	reach := service.NewProactiveReachService(
		repository.NewProactiveReachRepository(db),
		repository.NewCustomerDoNotContactRepository(db),
		nil,
	)
	service.BindProactiveReachSenders(reach, service.ProactiveReachSenderSet{
		Telegram: service.NewTelegramIntegrationService(repository.NewTelegramAccountRepositoryWithDB(db)),
		WhatsApp: service.NewWhatsAppCloudIntegrationService(repository.NewWhatsAppCloudAccountRepositoryWithDB(db)),
		Feishu: service.NewFeishuIntegrationService(
			repository.NewFeishuAccountRepositoryWithDB(db),
			repository.NewFeishuMessageRepositoryWithDB(db),
		),
		WeCom:  service.NewWeComIntegrationService(),
		Wechat: service.NewWechatService(),
	})
	gated := AttachReachGate(reach)

	worker := service.NewRecoveryQueueWorker(service.NewRecoveryQueueService(), reach)
	if !gated {
		if snap, _ := GetReachGateSnapshot(); snap.DependencyUnmet && worker.Mode() != service.RecoveryWorkerModeOff {
			// 旗子开着但裁决来源（W-1 checker）没接线，而这台 worker 又不是 off 档
			// （off 档不起协程、不外发，无裸发可言）：装上也是恒放或恒拒的假闸门，
			// 不如拒启 —— 一旦起来每轮都会走裸外发路径。
			logger.Errorf("[RecoveryWorker] ❌ 拒启：%s=%s 但 %s 未接线（依赖缺失，非配置关闭）"+
				"⇒ 不装门、不起协程、不外发；先开 %s=shadow|block 再重启",
				ReachGateFlagEnv, snap.Mode, ApprovalGateFlagEnv, ApprovalGateFlagEnv)
			return nil
		}
		LogReachGateSkippedAssemblyPoint("app.InitRecoveryWorker")
	}

	worker.Start(context.Background())
	logger.Infof("[RecoveryWorker] ✅ 挽回队列消费者已装配：mode=%s（开关 %s；off 时不启动，"+
		"shadow 只试发不外发，enforce 才真发；外发受全局退订与触达频控约束）",
		worker.Mode(), service.RecoveryWorkerFlagEnv)
	return worker
}

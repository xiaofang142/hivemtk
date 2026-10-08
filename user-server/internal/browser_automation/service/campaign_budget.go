package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// campaign_budget.go — 触达 P0 Chunk 4：活动级预算（单次触达成本核算）。
//
// 「活动」在本仓没有独立实体：触达活动就是任务本体（cron/loop/retry 的所有 session 都累计
// 到同一行 browser_tasks）。所以预算是任务行上的持久累计额度，跨 session、跨重跑累计。
//
// **不落内存是硬约束，不是省事**：Executor 是进程级单例，任何进程内计数在重启、扩容、
// 多副本部署下都会归零或各算一份——而预算的全部意义就是「这一单活动最多打扰 N 次、
// 最多烧 M token」这条跨进程成立的上限。Chunk 2 的平台熔断同样如此。
//
// 三条门各自独立、互不替代：
//  ① TokenBudget      session 级 LLM 预算覆盖（0=用默认常量）
//  ② CampaignActBudget 活动触达条数上限（0=不限，存量行为不变）
//  ③ write_claims     同会话/跨任务的**双发**裁决（Chunk 3 的四元组去重 + 台账独占占坑）
// ②管「总量」，③管「同一件事不许发两遍」。只有 ③ 没有 ②，cron 能一直发到号被封；
// 只有 ② 没有 ③，一次预算内的重复打扰照样发生。

// taskTokenBudget 本任务实际生效的 session 级 token 预算。
// 口径与 taskExecBudget 同源要求：taskTokenBudget 是「写预算的地方」唯一的取数入口，
// 别处不许再直接调 brainTokenBudget()——两处各判一次 env/字段，迟早会有一处忘了看任务字段。
func taskTokenBudget(t *model.BrowserTask) int {
	if t != nil && t.TokenBudget > 0 {
		return t.TokenBudget
	}
	return brainTokenBudget()
}

// campaignActBudgetLimited 该任务是否受活动触达条数预算约束。
// 预算 0 与活动键空都算「不受限」：前者是「没设额度」，后者是「没编入任何活动」。
// 调用方据此跳过扣减查询——不限任务不该为每条写步白付一次 UPDATE。
func campaignActBudgetLimited(t *model.BrowserTask) bool {
	return t != nil && t.CampaignKey != "" && t.CampaignActBudget > 0
}

// campaignBudgetExhaustedMsg 预算耗尽的拒绝文案（面板与用户看的是同一句）。
// 刻意写成「配置事实」而非「异常」：预算耗尽是运营方主动设的上限被打到，
// 让用户以为任务出错并去重跑，只会让预算再次被打到。
func campaignBudgetExhaustedMsg(t *model.BrowserTask) string {
	return fmt.Sprintf("活动触达预算已耗尽（活动 %s：已用 %d/%d 条）——本步未下发，请提高活动预算或换活动键后重跑",
		t.CampaignKey, t.CampaignActUsed, t.CampaignActBudget)
}

// consumeCampaignActBudget 写步下发前的活动触达条数扣减，返回是否放行。
//
// 三条 fail-open 口径（都不是「闸门没接好就放行」的松散，而是有具体理由的）：
//  1. 未配置预算（campaignActBudgetLimited=false）⇒ 不查库、不扣减。存量任务与未编入活动的
//     任务行为逐字不变，且不该为每条写步白付一次 UPDATE。
//  2. taskRepo 未接线 ⇒ 放行。理由见 Executor.taskRepo 字段注释：预算的失败形态是多打扰几次，
//     锁死整条触达线的失败形态更大。
//  3. DB 报错 ⇒ 放行并告警。同上：读侧故障不该变成写侧的永久停摆；真要收紧该在启动门做。
//
// 扣减点刻意排在 write_claims 占坑**之后**：占坑失败/D7 确认超时的步最终没发出去，
// 先扣了额度就是「为没发生的触达收费」，用户的活动额度会被这些从不动用的失败步吃干。
func (e *Executor) consumeCampaignActBudget(ctx context.Context, task *model.BrowserTask) bool {
	if !campaignActBudgetLimited(task) || e.taskRepo == nil {
		return true
	}
	ok, err := e.taskRepo.TryConsumeCampaignActBudget(ctx, task.ID)
	if err != nil {
		logger.Warnf("[BrowserExec] 活动预算扣减失败，fail-open 放行 task=%d: %v", task.ID, err)
		return true
	}
	if !ok {
		logger.Warnf("[BrowserExec] %s", campaignBudgetExhaustedMsg(task))
	}
	return ok
}

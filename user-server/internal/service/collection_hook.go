// collection_hook.go 回款完成 → 赢单 → 复购跟进的默认钩子（T-P7-04）。
//
// 形状说明：组装函数 WireCollectionToRepurchase 落在 service 层而不是 app 层 ——
// 钩子要同时认识三台服务（商机 / 旅程 / 跟进），这份"认识"本身是可单测的业务逻辑，
// app/payment_wiring.go 只负责在生产装配时把三台真实例递进来（位置约束见该文件头）。

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
)

// ErrCollectionHookSkipped 钩子主动跳过的基错：调用方（fireCollectionCompleted）
// 只认这一把（errors.Is），跳过原因写在包错里，结论记 "skipped"。
var ErrCollectionHookSkipped = errors.New("collection hook: 跳过（幂等收口，不重排跟进）")

// ErrCollectionHookSkippedNoOpportunity 账单没关联商机：钱照记、单照结，赢单无从谈起。
var ErrCollectionHookSkippedNoOpportunity = fmt.Errorf("%w：账单未关联商机", ErrCollectionHookSkipped)

// ErrCollectionHookSkippedAlreadyWon 商机早已是 won：赢单是幂等收口，复购跟进不重排。
var ErrCollectionHookSkippedAlreadyWon = fmt.Errorf("%w：商机已赢单", ErrCollectionHookSkipped)

// WireCollectionToRepurchase 把"商机赢单 + 旅程切复购期 + 复购跟进排期"装成一个钩子。
//
// 三台服务任一为 nil / 不可用都不在装配时炸 —— 炸的时机是触发那一刻，且只变成
// receipt 上的 "error: …"，支付本身永远成功。nil 判据打在触发时而不是装配时，
// 理由：InitPaymentRuntime 可能跑在 InitOpportunityRuntime 之前（测试与灰度重启），
// 装配时取到的全局商机服务可能是上一份或 nil，那一刻的结论到触发时早已过期。
func WireCollectionToRepurchase(opps *OpportunityService, journey *CustomerJourneyService, followup *FollowUpService) CollectionCompletedHook {
	return func(ctx context.Context, bill *model.Bill) error {
		oppID := strings.TrimSpace(bill.OpportunityID)
		if oppID == "" {
			return fmt.Errorf("%w：账单 %s", ErrCollectionHookSkippedNoOpportunity, bill.ID)
		}
		if opps == nil || !opps.Available() {
			return fmt.Errorf("collection hook: 商机服务未装配，账单 %s 的赢单收口做不了（钱已记、单已结，跟进缺席）", bill.ID)
		}
		alreadyWon, opp, err := opps.CompleteCollection(ctx, oppID)
		if err != nil {
			return fmt.Errorf("collection hook: 商机 %s 赢单收口失败：%w", oppID, err)
		}
		if alreadyWon {
			return fmt.Errorf("%w：商机 %s", ErrCollectionHookSkippedAlreadyWon, oppID)
		}
		if journey == nil || followup == nil {
			return fmt.Errorf("collection hook: 旅程/跟进服务未装配，商机 %s 已赢单但复购跟进没排（需人工补排）", oppID)
		}
		// 机器路径的操作人：负责销售在先，无则 system（与 MarkWonByCollection 的
		// "事实不记人"不同，跟进提醒必须有个可 @ 的 owner，调度器按它派单）。
		operator := strings.TrimSpace(opp.OwnerUserID)
		if operator == "" {
			operator = "system"
		}
		if _, err := journey.Transition(ctx, opp.CustomerID, StageRepurchase,
			"collection", operator, "回款完成→赢单，复购期跟进", nil); err != nil {
			return fmt.Errorf("collection hook: 商机 %s 切复购期失败：%w", oppID, err)
		}
		if _, err := followup.ScheduleForStage(ctx, opp.CustomerID, operator, StageRepurchase); err != nil {
			return fmt.Errorf("collection hook: 商机 %s 复购跟进排期失败：%w", oppID, err)
		}
		return nil
	}
}

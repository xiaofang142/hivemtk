package service

// 回款金额 / 丢单负样本进 feedback_loop（T-P8-02，LTC-24）。
//
// 归因的三格约束（见定案）：
//   - payment 侧 Transited 的每次结算（含 partial）都触发 —— 用**累计 settled** 金额，
//     避免"分三次付只奖最后一次"的形状；重放 Transited=false 不触发，不重奖。
//   - SessionID 合成 "collection:<bill_id>"（支付域没有对话 session）：
//     同一账单多次 partial 走同一 session 做 upsert 累加；reflux 不按 session 分组，
//     前缀可排斥，误收也进不了 bandit。
//   - CustomerID 必须从商机解（账单行没有 customer 列）：解不到记 skipped，不伪造。
//   - reflux 四键过滤**不动**：回款事件无 PromptCandidateID/SOPID，加了键也必 skip；
//     事件入库供训练/离线消费，未来对话↔回款 join 通了再开 reflux 口。
//   - 钩子错一律 Warn + 记 outcome，不败支付（钱的事比 reward 大）。

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service/feedback_loop"
)

// CollectionRewardHook 回款奖励钩子：Transited 的每次结算触发（paid + partial）。
type CollectionRewardHook func(ctx context.Context, bill *model.Bill, settled SettlementView) error

// LostRewardHook 丢单负样本钩子：MarkLost 真跃迁后触发。
type LostRewardHook func(ctx context.Context, opp *model.Opportunity) error

var (
	// ErrRewardHookSkippedNoOpportunity 账单没挂商机（遗留脏行）：reward 缺席，不断支付。
	ErrRewardHookSkippedNoOpportunity = errors.New("reward hook skipped: 账单未关联商机")
	// ErrRewardHookSkippedNoCollector feedback 未装配：reward 缺席，不断支付。
	ErrRewardHookSkippedNoCollector = errors.New("reward hook skipped: 反馈采集器未装配")
)

// WireCollectionReward 把 payment 的奖励钩子接到商机 + 采集器。
//
// opps 负责 bill.OpportunityID → CustomerID；collector 负责入库。
// 任一缺席都返回 skipped 哨兵（traceSettlement 侧只记 Warn，不败支付）。
func WireCollectionReward(opps *OpportunityService, collector *feedbackloop.FeedbackCollector) CollectionRewardHook {
	return func(ctx context.Context, bill *model.Bill, settled SettlementView) error {
		if collector == nil {
			return ErrRewardHookSkippedNoCollector
		}
		oppID := strings.TrimSpace(bill.OpportunityID)
		if oppID == "" {
			return ErrRewardHookSkippedNoOpportunity
		}
		if opps == nil || !opps.Available() {
			return ErrRewardHookSkippedNoOpportunity
		}
		opp, gerr := opps.Get(ctx, oppID)
		if gerr != nil || opp == nil {
			return ErrRewardHookSkippedNoOpportunity
		}
		customerID := strings.TrimSpace(opp.CustomerID)
		if customerID == "" {
			return ErrRewardHookSkippedNoOpportunity
		}
		return collector.Collect(ctx, &dto.CollectRequest{
			SessionID:   "collection:" + strings.TrimSpace(bill.ID),
			CustomerID:  customerID,
			EventType:   dto.FBEventTypeImplicit,
			SignalKey:   dto.FBSignalCollection,
			SignalValue: settled.Settled,
			Metadata: map[string]any{
				"opportunity_id": oppID,
				"bill_id":        strings.TrimSpace(bill.ID),
				"quote_id":       strings.TrimSpace(bill.QuoteID),
				"bill_status":    settled.Status,
			},
		})
	}
}

// WireLostReward 把 opportunity 的丢单钩子接到采集器。
//
// MarkLost 成功才调（已 lost 的重放走 ErrOpportunityClosed，不到这里）。
// SessionID "collection-lost:<opp_id>" 与回款键同前缀，排斥策略一致。
func WireLostReward(collector *feedbackloop.FeedbackCollector) LostRewardHook {
	return func(ctx context.Context, opp *model.Opportunity) error {
		if collector == nil {
			return ErrRewardHookSkippedNoCollector
		}
		if opp == nil {
			return ErrRewardHookSkippedNoOpportunity
		}
		return collector.Collect(ctx, &dto.CollectRequest{
			SessionID:   "collection-lost:" + strings.TrimSpace(opp.ID),
			CustomerID:  strings.TrimSpace(opp.CustomerID),
			EventType:   dto.FBEventTypeImplicit,
			SignalKey:   dto.FBSignalCollectionLost,
			SignalValue: 0,
			Metadata: map[string]any{
				"opportunity_id": strings.TrimSpace(opp.ID),
				"lost_reason":    strings.TrimSpace(opp.LostReason),
			},
		})
	}
}

// fireRewardHook 在 traceSettlement 内调用：只 Warn，不断支付。
func (s *PaymentService) fireRewardHook(ctx context.Context, bill *model.Bill, settled SettlementView) {
	if !settled.Transited || s.onReward == nil {
		return
	}
	if err := s.onReward(ctx, bill, settled); err != nil {
		logger.Warnf("[payment] 回款奖励钩子缺席/失败（账单 %s）：%v —— 支付与结单不受影响", bill.ID, err)
	}
}

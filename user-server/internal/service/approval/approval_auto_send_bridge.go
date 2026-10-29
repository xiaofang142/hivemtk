// approval_auto_send_bridge.go E-10（2026-10-24，产品可复议）：审批通过后自动外发报价。
//
// 规划 §4.10 的原判据：
//   - 审批通过后只发一次（Send 自带幂等：approval_id 带结论去派发，重复调用
//     会撞 awaiting/approved 已跃迁的状态而不再外发）；
//   - 发送失败可重试（失败只出声，运营可重发——发送腿的状态机不变）；
//   - 关闭开关时行为与现在一致（nil 桥 = 无订阅者 = 纯手动档）。
//
// 为什么做成 ApprovalWaitNotifier 的追加订阅者而不是在 Send 里再校验一遍：
// 裁决端点（decide）是唯一知道"批了"的时刻，从这里驱动外发是事件驱动单入口；
// 在发送腿里再加一份"批了就发"的判定则是两处各写一遍同一结论（规划原话：
// 不要在两处重复校验）。
//
// 口径：只认 subject_type=quote、结论=approved 的裁决。rejected/expired/
// reach_plan/order_command 等一律不动——它们要么没有"发"这个动作，要么
// 外发口径未定义（等产品复议后扩展）。
package approval

import (
	"context"
	"os"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// approvalAutoSendOperator 自动外发的 operator 名（落 sales_events.owner_id）。
// 不装作真人：审计读这一列时必须能分辨"机器替人发的"与"人自己点的"。
const approvalAutoSendOperator = "approval-auto-send"

// QuoteApprovalSubjectType 报价审批的 subject_type（与根包 quote_send.go 同一词表）。
const QuoteApprovalSubjectType = "quote"

// QuoteSendRequest 自动外发所需的最小入参（与根包 QuoteSendInput 同形，根包适配器转换）。
type QuoteSendRequest struct {
	QuoteRowID string
	ApprovalID string
	Operator   string
}

// QuoteSendOutcome 自动外发结果的两列（日志用）。
type QuoteSendOutcome struct {
	Disposition string
	Status      string
}

// QuoteSendService 报价发送腿对审批桥暴露的窄接口（根包 *QuoteSendService 适配后满足）。
type QuoteSendService interface {
	Available() bool
	Send(ctx context.Context, in QuoteSendRequest) (*QuoteSendOutcome, error)
}

// ApprovalAutoSendBridge 审批通过 → 报价自动外发（E-10，产品可复议）。
//
// quoteSend 用全局实例（装配在 InitQuoteRuntime）：审批运行时与报价运行时的
// 装配顺序不固定，持全局句柄可以让"谁先装配都能接上"（nil 时本桥静默 = 手动档）。
type ApprovalAutoSendBridge struct {
	quoteSend func() QuoteSendService
	enabled   bool
}

// approvalAutoSendFlagEnv 自动外发开关。
//
// 规划 §4.10 判据"关闭开关时行为与现在一致"：此前桥装配后**无法单独关闭**
// （只能卸整个审批运行时），与 FF_LTC_APPROVAL_RESUME 开关家族不一致。
// 本开关是独立的一格：off=桥装着但静默（等价手动档），on=审批通过即外发。
//
// 与 ApprovalResumeFlagEnv 的分工（两把旗子不是一把）：那把管**审批运行时**
// 挂不挂（挂起/恢复/清扫），本把只管**通过之后要不要替人发**。运行时 off 时
// 本开关无意义（没有裁决就没有外发）；运行时 on 而本开关 off 是合法且有用的
// 组合——流程照样挂起等人工，批了以后外发仍是人工动作。
//
// 默认 on（最优默认实现，产品可复议）：桥的存在本身就是 E-10 的交付物，
// 默认关等于交付了但没人用得上；显式写 off 才降回手动档。
const approvalAutoSendFlagEnv = "FF_LTC_APPROVAL_AUTO_SEND"

// parseApprovalAutoSendFlag 开关值解析。与 parseApprovalRuntimeMode 同一取向：
// 认不清的值宁可降档（off）也不猜着开——错关一次外发，运营还能手动补发；
// 错开一次外发，报价已经发到客户手里了。
func parseApprovalAutoSendFlag(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return true // 缺省 on（见 approvalAutoSendFlagEnv 注释）
	case "on", "true", "1", "yes":
		return true
	case "off", "false", "0", "no":
		return false
	}
	logger.Warnf("[approval-auto-send] %s=%q 无法识别 ⇒ 按 off 处理（自动外发关闭，需人工发送）；可用值：on|off",
		approvalAutoSendFlagEnv, raw)
	return false
}

// NewApprovalAutoSendBridge 构造。quoteSend 允许回 nil（运行时未装配）。
// 开关读环境变量（见 approvalAutoSendFlagEnv）：off 时桥仍构造、仍订阅，
// 但 NotifyDecided 第一行就静默返回——不摘订阅者的原因是装配面的订阅清单
// 是 fan-out（AddWaitNotifier），摘装要过两把锁，静默判据一行就够。
func NewApprovalAutoSendBridge(quoteSend func() QuoteSendService) *ApprovalAutoSendBridge {
	return &ApprovalAutoSendBridge{quoteSend: quoteSend, enabled: parseApprovalAutoSendFlag(os.Getenv(approvalAutoSendFlagEnv))}
}

// NotifyDecided 实现 ApprovalWaitNotifier（E-10）。
//
// 无返回值是接口定的：人的裁决已经落库，外发失败不能回滚它——失败只出声，
// 运营可按原有手动路径重发（QuoteSendResult 的 awaiting/sent 语义不变）。
func (b *ApprovalAutoSendBridge) NotifyDecided(ctx context.Context, req *model.ApprovalRequest) {
	if b == nil || req == nil {
		return
	}
	if !b.enabled {
		// 开关 off（FF_LTC_APPROVAL_AUTO_SEND=off）：行为与装桥之前一致——
		// 审批照常裁决，外发留给人工。静默（不打日志）：off 是正常配置档，
		// 每次裁决都打一行"没发"的噪音会把真告警淹掉。
		return
	}
	if req.SubjectType != QuoteApprovalSubjectType {
		return
	}
	if req.Status != model.ApprovalStatusApproved {
		return
	}
	svc := b.quoteSend()
	if svc == nil || !svc.Available() {
		logger.Ctx(ctx).Warn().
			Str("approval_id", req.ID).
			Str("quote_row_id", req.SubjectID).
			Msg("[approval-auto-send] 报价发送腿未装配 ⇒ 审批已通过但未自动外发，需人工发送（手动档行为）")
		return
	}
	res, err := svc.Send(ctx, QuoteSendRequest{
		QuoteRowID: req.SubjectID,
		ApprovalID: req.ID,
		Operator:   approvalAutoSendOperator,
	})
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Str("approval_id", req.ID).
			Str("quote_row_id", req.SubjectID).
			Msg("[approval-auto-send] 审批通过后自动外发失败 ⇒ 需人工按原路径重发（裁决已生效，不受影响）")
		return
	}
	logger.Ctx(ctx).Info().
		Str("approval_id", req.ID).
		Str("quote_row_id", req.SubjectID).
		Str("disposition", res.Disposition).
		Str("status", res.Status).
		Msg("[approval-auto-send] 审批通过后自动外发完成")
}

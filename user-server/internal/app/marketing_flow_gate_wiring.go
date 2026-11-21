package app

// marketing_flow_gate_wiring.go FA-20：给营销流程的短信/邮件动作装上统一外发判定链。
//
// 一句话职责：构造营销流程判定专用的 ProactiveReachService（与催收/挽回/报价各腿
// 同构），先过 AttachReachGate 装上 W-1 审批门（旗子与裁决来源就绪时），再注入给
// internal/service 的营销流程发送器注册实现。
//
// 顺序约束与 recovery_worker_wiring.go / collection_wiring.go 同一条：必须在
// router.Setup 之后调用——W-1 的 checker 是 InitGlobalToolExecutor 才建的。
// 装在前面不会 panic，只会留下"依赖未成立 ⇒ 不装审批门"的告警，DNC/合规仍生效。
//
// fail-closed 语义：AttachMarketingFlowReachGate 未被调用（或传 nil db）时，营销流程
// 的 send_sms/send_email 动作一律拒发——不存在"没装配就裸发"的静默通道。

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// AttachMarketingFlowReachGate 装配营销流程外发判定链。
// db 为 nil 时只留痕不注入——注册的发送器随后 fail-closed，营销流程外发动作全部拒发。
func AttachMarketingFlowReachGate(db *gorm.DB) {
	if db == nil {
		logger.Warn("[marketing-flow-gate] ⚠️ db 为 nil，营销流程外发闸门未装配 ⇒ send_sms/send_email 动作将 fail-closed 拒发")
		return
	}
	reach := service.NewProactiveReachService(
		repository.NewProactiveReachRepository(db),
		repository.NewCustomerDoNotContactRepository(db),
		nil,
	)
	gated := AttachReachGate(reach)
	service.AttachMarketingFlowReachGate(reach)
	logger.Infof("[marketing-flow-gate] 营销流程外发判定链已装配 approval_gate=%v（DNC/合规恒生效）", gated)
}

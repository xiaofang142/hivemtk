// business_notifier_wiring.go 业务通知生产者的装配层（I8）。
//
// 职责与 bad_case_wiring.go 同一条：把"有没有 DB 句柄"翻译成一个可注入的服务、
// 登记到全局，让三个生产者（订单草稿 / 坏例 / 知识库变更）在请求期现取同一份。
//
// 与 InitHumanTaskSLA 各自构造一台 NotificationService，**不是重复**：
// SLA 那台活在三态旗 FF_LTC_HANDOFF_SLA_JOB 后面（off 时压根不构造，扫描器
// 不存在就没人需要它），这一台不受任何旗子控制 —— 底座拿到 DB 就装配，
// 三类生产者即刻生效。两件事的风险不同：SLA 按节拍反复外发（必须有档位），
// 这三类是新实体首建的一次性事件，铃铛响不响不影响任何业务正确性。
package app

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitBusinessNotifier 装配业务通知口并登记为全局实例，可重复调用（后写覆盖）。
//
// db == nil 时必须把全局**清空**而不是"什么都不做"：与 InitBadCaseRuntime 同理，
// 上一次装配成功的实例若留在全局里，生产者会继续对着已失效的连接写。
// 撤掉后生产者全部 no-op（只出一行 Warn），业务动作照常成功 —— 铃铛是旁路。
func InitBusinessNotifier(db *gorm.DB) {
	if db == nil {
		service.SetGlobalNotifier(nil)
		logger.Warnf("[business-notify] ⚠️ 无 DB 句柄 ⇒ 业务通知口不装配：草稿/坏例/知识库变更三类铃铛不会响" +
			"（业务动作照常成功，只是收不到提醒）")
		return
	}
	service.SetGlobalNotifier(service.NewNotificationService(db))
	logger.Infof("[business-notify] ✅ 业务通知口已装配：新草稿待确认 → owner、新坏例待归因 / 知识库变更待审核 → 广播")
}

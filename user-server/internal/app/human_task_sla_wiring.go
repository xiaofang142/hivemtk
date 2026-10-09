// human_task_sla_wiring.go 待办 SLA 超时闭环的装配层（I2）。
//
// 一句话职责：把 service.HumanTaskSLAWorker（human_task_sla_job.go）按一把三态旗子
// 挂到进程上。在此之前，human_tasks 的逾期只被 CountOverdueOpenByKind 数出来，
// 没有任何东西按节拍去处置它们 —— 本文件就是那个装配点。
//
// 三态语义（顺序 = 风险递增，默认停在第一档）：
//
//	off（默认）  什么都不装配。逾期待办只被数、不被办，与交付前逐字节一致。
//	shadow      只装扫描器、**只算不写**：每轮把"扫到几条逾期、几条该升级"打进日志。
//	            这一档回答的问题是"逾期判定那段 SQL 在真表上算对了没有"，答案不影响任何人。
//	enforce     真写：逾期即给责任人发站内提醒，拖过一整个 SLA 窗口再向管理员广播升级。
//
// 与 FF_LTC_APPROVAL_RESUME 同一取舍：布尔式真值（true/1/yes/on）只到 shadow。
// 一把能让坐席收到提醒、能让管理员被惊动的旗子，不该因为有人按习惯写了 `=true`
// 就拿到"真写通知"的能力；要那一档必须显式写 enforce。认不出的值判 off 并告警。
//
// 五层归属：旗子读取 + 依赖拼装在本文件；扫描/去重/升级判据在 human_task_sla_job.go。
package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// HumanTaskSLAJobFlagEnv 待办 SLA 超时闭环总开关。端点回显与运维口径都用这个常量。
const HumanTaskSLAJobFlagEnv = "FF_LTC_HANDOFF_SLA_JOB"

// parseHumanTaskSLAMode 解析开关值（三态 + 真值降档，见文件头）。
func parseHumanTaskSLAMode(raw string) service.HumanTaskSLAMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return service.HumanTaskSLAModeOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return service.HumanTaskSLAModeOff
	case "shadow", "observe", "watch", "sweep", "log":
		return service.HumanTaskSLAModeShadow
	case "enforce", "on", "active", "run":
		return service.HumanTaskSLAModeEnforce
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			logger.Warnf("[human-task-sla] ⚠️ %s=%q 是布尔真值：语义不足以表达\"真写提醒/升级\"⇒ 按 shadow 处理。"+
				"要真写请显式写 enforce，可用值：off|shadow|enforce", HumanTaskSLAJobFlagEnv, raw)
			return service.HumanTaskSLAModeShadow
		}
		return service.HumanTaskSLAModeOff
	}
	logger.Warnf("[human-task-sla] %s=%q 无法识别 ⇒ 按 off 处理（逾期扫描不装配）；可用值：off|shadow|enforce",
		HumanTaskSLAJobFlagEnv, raw)
	return service.HumanTaskSLAModeOff
}

// HumanTaskSLARuntime 本进程的待办 SLA 运行时（扫描 worker）。
type HumanTaskSLARuntime struct {
	worker *service.HumanTaskSLAWorker
	mode   service.HumanTaskSLAMode
}

// Mode 生效档位（off 时运行时根本不存在，故本方法只在已装配实例上调用）。
func (r *HumanTaskSLARuntime) Mode() string { return string(r.mode) }

var (
	humanTaskSLARuntimeMu  sync.RWMutex
	humanTaskSLARuntimeRef *HumanTaskSLARuntime
)

func currentHumanTaskSLARuntime() *HumanTaskSLARuntime {
	humanTaskSLARuntimeMu.RLock()
	defer humanTaskSLARuntimeMu.RUnlock()
	return humanTaskSLARuntimeRef
}

// InitHumanTaskSLA 按旗子装配待办 SLA 运行时；off / 无库两档返回 nil（并出声）。
//
// 先停旧的再装新的，可重复调用：测试进程里多个用例各自 Setup 时，只装不停会攒出
// 一堆并发扫描器；off 分支也必须走到"停旧的"这一步，否则用 t.Setenv 把旗子切回 off
// 后，上一份协程还留在进程里继续给坐席发提醒。
func InitHumanTaskSLA(db *gorm.DB) *HumanTaskSLARuntime {
	mode := parseHumanTaskSLAMode(os.Getenv(HumanTaskSLAJobFlagEnv))

	humanTaskSLARuntimeMu.Lock()
	prev := humanTaskSLARuntimeRef
	humanTaskSLARuntimeRef = nil
	humanTaskSLARuntimeMu.Unlock()
	if prev != nil && prev.worker != nil {
		prev.worker.Stop(context.Background())
	}

	if db == nil {
		logger.Warnf("[human-task-sla] ⚠️ 无 DB 句柄 ⇒ 待办 SLA 运行时不装配（开关 %s=%s）：逾期待办读不出来",
			HumanTaskSLAJobFlagEnv, mode)
		return nil
	}
	if mode == service.HumanTaskSLAModeOff {
		logger.Infof("[human-task-sla] %s=off ⇒ 不装配：待办逾期只被数、不被办（与交付前一致）", HumanTaskSLAJobFlagEnv)
		return nil
	}

	svc := service.NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(db), service.GlobalConfigParam())
	notifier := service.NewNotificationService(db)
	worker := service.NewHumanTaskSLAWorker(svc, notifier, mode, 0, 0)
	rt := &HumanTaskSLARuntime{worker: worker, mode: mode}
	worker.Start(context.Background())

	humanTaskSLARuntimeMu.Lock()
	humanTaskSLARuntimeRef = rt
	humanTaskSLARuntimeMu.Unlock()

	if mode == service.HumanTaskSLAModeShadow {
		logger.Infof("[human-task-sla] ✅ shadow 态：只扫只数、**一条不写** ⇒ 逾期待办不会收到任何提醒（开关 %s）",
			HumanTaskSLAJobFlagEnv)
		return rt
	}
	logger.Infof("[human-task-sla] ✅ 待办 SLA 超时闭环已装配：mode=enforce（开关 %s）。逾期即给责任人发站内提醒，"+
		"拖过一整个 SLA 窗口再向管理员广播升级；**不新增 human_task kind**（值域三类是硬约束）", HumanTaskSLAJobFlagEnv)
	return rt
}

// StopHumanTaskSLA 停掉扫描协程并清空全局引用（幂等）。
func StopHumanTaskSLA() {
	rt := currentHumanTaskSLARuntime()
	humanTaskSLARuntimeMu.Lock()
	humanTaskSLARuntimeRef = nil
	humanTaskSLARuntimeMu.Unlock()
	if rt == nil || rt.worker == nil {
		return
	}
	rt.worker.Stop(context.Background())
	logger.Infof("[human-task-sla] 扫描 worker 已停止（轮次=%d 累计逾期=%d 提醒=%d 升级=%d）",
		rt.worker.Rounds(), rt.worker.OverdueTotal(), rt.worker.RemindedTotal(), rt.worker.EscalatedTotal())
}

// approval_runtime_wiring.go 异步审批运行时的装配层（新规划任务清单 T-P3-02）。
//
// 一句话职责：把 T-P3-01 的审批服务、T-P3-02 的挂起/恢复桥、以及"到期没人裁决要落
// expired"这件事的节拍器，按一把旗子挂到进程上。在此之前 approval_requests 这张表
// 全仓非测试构造点为 0（T-P3-01 的项12a 登记的就是这件事）：服务写得再完整，
// 没有装配点就等于没有这个功能。
//
// 三态语义（顺序 = 风险递增，默认停在第一档）：
//
//	off（默认） 什么都不装配。图里的审批等待节点判**失败**（不是跳过、不是放行），
//	            到期 pending 无人清扫 —— 与 T-P3-01 交付态逐字节一致。
//	shadow    只装清扫器：审批行会按 expires_at 收口、待办会自己退场，但**没有桥**，
//	            于是没有任何流程会因为这次装配而停下来等人类。这一档回答的问题是
//	            "清扫那段 UPDATE 在真表上跑对了没有"，答案不影响任何客户会话。
//	on        桥也装配：SOP 的审批等待节点开始真的挂起、裁决之后真的被叫醒。
//
// 为什么 shadow 的刀口切在"装不装桥"而不是"挂不挂起"：挂起本身就是客户可感知的行为
// 变更（一条本来会立刻回复客户的流程从此停在等人点按钮），而清扫只是把已经发生的事实
// 写回库里。风险递增的顺序必须和"谁看得见"一致。
//
// 与 FF_LTC_APPROVAL_GATE / FF_LTC_ORDER_DRAFT_DB 同一取舍：布尔式真值（true/1/yes/on）
// 只到 shadow。一把能让流程停下来的旗子，不该因为有人按习惯写了 `=true` 就拿到
// "让客户会话挂起等人工"的能力；要那档必须显式写 on。认不出的值判 off 并告警。
//
// 与 FF_LTC_APPROVAL_GATE 的分工（两把旗子不是一把）：那把管**同步白名单审批门**
// （工具调用当场问"这个账号批了没有"，off|shadow|block），这把管**异步审批运行时**
// （流程挂起、人来裁决、之后续跑）。两把各自独立翻转，因为一个是"要不要拦这次调用"，
// 另一个是"要不要让流程停下来等人类"。
//
// 五层归属：旗子读取 + 依赖拼装在本文件；入队/裁决/到期的业务判据在 approval_request.go，
// 挂起与回读在 sop_approval_resume.go，节拍在 approval_sweep.go。
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

// ApprovalResumeFlagEnv 异步审批运行时总开关。端点回显与运维口径都用这个常量，
// 免得文档写一个名字、代码读另一个。
const ApprovalResumeFlagEnv = "FF_LTC_APPROVAL_RESUME"

type approvalRuntimeMode string

const (
	approvalRuntimeOff    approvalRuntimeMode = "off"
	approvalRuntimeShadow approvalRuntimeMode = "shadow"
	approvalRuntimeOn     approvalRuntimeMode = "on"
)

// parseApprovalRuntimeMode 解析开关值（三态 + 真值降档，见文件头）。
func parseApprovalRuntimeMode(raw string) approvalRuntimeMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return approvalRuntimeOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return approvalRuntimeOff
	case "shadow", "observe", "watch", "sweep", "log":
		return approvalRuntimeShadow
	case "on", "enforce", "active", "resume":
		return approvalRuntimeOn
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			logger.Warnf("[approval-runtime] ⚠️ %s=%q 是布尔真值：语义不足以表达\"让流程挂起等人工\"⇒ 按 shadow 处理。"+
				"要真挂起请显式写 on，可用值：off|shadow|on", ApprovalResumeFlagEnv, raw)
			return approvalRuntimeShadow
		}
		return approvalRuntimeOff
	}
	logger.Warnf("[approval-runtime] %s=%q 无法识别 ⇒ 按 off 处理（审批运行时不装配）；可用值：off|shadow|on",
		ApprovalResumeFlagEnv, raw)
	return approvalRuntimeOff
}

// ApprovalRuntime 本进程的审批运行时（服务 + 桥 + 清扫 worker）。
type ApprovalRuntime struct {
	svc     *service.ApprovalRequestService
	bridge  *service.ApprovalResumeBridge
	sweeper *service.ApprovalSweepWorker
	mode    approvalRuntimeMode
}

// Mode 生效档位（off 时运行时根本不存在，故本方法只在已装配实例上调用）。
func (r *ApprovalRuntime) Mode() string { return string(r.mode) }

// 全局运行时的锁口径与草稿侧相同：写入发生在 router.Setup()（HTTP 尚未开始收流量），
// 读发生在 worker 线程与测试里。用锁而不是"约定先写后读"，是因为 Init 可被重复调用
// （见其文档），而重复调用时上一份的协程与读侧还在跑。
var (
	approvalRuntimeMu  sync.RWMutex
	approvalRuntimeRef *ApprovalRuntime
)

func currentApprovalRuntime() *ApprovalRuntime {
	approvalRuntimeMu.RLock()
	defer approvalRuntimeMu.RUnlock()
	return approvalRuntimeRef
}

// publishApprovalRuntime 把这一份登记为"当前运行时"。
//
// 没有这一步，`approvalRuntimeRef` 恒为 nil，于是 Init 开头的"先停旧的"永远看到 prev==nil，
// StopApprovalRuntime 也永远无物可停 —— 表现是：旗子从 on 切回 off 后全局桥还挂着
// （流程照旧挂起），重复 Setup 会攒出多个并发清扫器。两者都是只在装配层才暴露的缺陷。
func publishApprovalRuntime(rt *ApprovalRuntime) {
	approvalRuntimeMu.Lock()
	approvalRuntimeRef = rt
	approvalRuntimeMu.Unlock()
}

// InitApprovalRuntime 按旗子装配审批运行时；off 档返回 nil（并出声）。
//
// 先停旧的再装新的，可重复调用。理由与草稿侧同源：测试进程里多个用例各自 Setup 的话，
// 只装不停会攒出一堆并发清扫器；而 off 分支也必须走到这一步 ——
// 测试用 t.Setenv 把旗子切回 off 时，上一份的协程不能留在进程里继续翻审批行的状态。
//
// policy 传 nil：C2 允许旧白名单 checker 作为 auto-approve 的判据来源之一，但把
// (subject_type, subject_id) 映射到 (tool_name, account_id) 是调用方的知识
// （T-P3-01 文件头同一条，接线卡是 T-P3-07 / T-P5-03）。nil 的含义是**全部走人工**，
// 那是最保守的一档，不是"没有策略所以没人被自动放行"的疏漏。
func InitApprovalRuntime(db *gorm.DB) *ApprovalRuntime {
	mode := parseApprovalRuntimeMode(os.Getenv(ApprovalResumeFlagEnv))

	approvalRuntimeMu.Lock()
	prev := approvalRuntimeRef
	approvalRuntimeRef = nil
	approvalRuntimeMu.Unlock()
	if prev != nil {
		if prev.bridge != nil {
			// 撤桥必须在停清扫之前：WaitExecutor 读的是全局桥，先撤掉它，
			// 之后即便还有任务在飞，也只会被判失败（fail-closed），不会走半套装配。
			service.SetApprovalResumeBridge(nil)
		}
		if prev.svc != nil {
			prev.svc.SetWaitNotifier(nil)
		}
		if prev.sweeper != nil {
			prev.sweeper.Stop(context.Background())
		}
	}

	if db == nil {
		logger.Warnf("[approval-runtime] ⚠️ 无 DB 句柄 ⇒ 审批运行时不装配（开关 %s=%s）：审批无处落库，"+
			"图里的审批等待节点会判失败", ApprovalResumeFlagEnv, mode)
		return nil
	}

	if mode == approvalRuntimeOff {
		logger.Infof("[approval-runtime] %s=off ⇒ 不装配审批运行时：approval_requests 无写入方、"+
			"到期 pending 无人清扫、审批等待节点判失败（与 T-P3-01 交付态一致）", ApprovalResumeFlagEnv)
		return nil
	}

	svc := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(db), nil)
	rt := &ApprovalRuntime{svc: svc, mode: mode}

	rt.sweeper = service.NewApprovalSweepWorker(svc, service.DefaultApprovalSweepInterval)
	rt.sweeper.Start(context.Background())

	if mode == approvalRuntimeShadow {
		publishApprovalRuntime(rt)
		logger.Infof("[approval-runtime] ✅ shadow 态：只清扫到期 pending，**不装配挂起/恢复桥** ⇒ 图里的审批等待节点"+
			"仍判失败，没有任何流程会因为这次装配而停下来等人工（开关 %s）", ApprovalResumeFlagEnv)
		return rt
	}

	bridge := service.NewApprovalResumeBridge(svc, db)
	rt.bridge = bridge
	service.SetApprovalResumeBridge(bridge)
	// 推送出口装在服务上：裁决与到期都经它叫醒等待中的流程（只省时间，正确性走点火回读）。
	svc.SetWaitNotifier(bridge)
	publishApprovalRuntime(rt)

	logger.Infof("[approval-runtime] ✅ 审批运行时已装配：mode=on（开关 %s）。SOP 的 wait 节点可用 "+
		"wait_event=approval 挂起，等待对象是一条 approval_requests 记录，"+
		"到期时刻取自该记录自己的 expires_at（同一事实源，不会两边各说各话）", ApprovalResumeFlagEnv)
	return rt
}

// StopApprovalRuntime 停掉清扫协程并清空全局引用（幂等）。
//
// 与 StopOrderDraftRuntime 同一个已知缺口：正解位置是 cmd/api/main.go 的退出序列，
// 而本卡的改动范围不含 main.go（它是并行会话正在改的文件）。入口先备好，
// 没有它，进程退出时清扫协程就是无人回收的泄漏。
func StopApprovalRuntime() {
	rt := currentApprovalRuntime()
	approvalRuntimeMu.Lock()
	approvalRuntimeRef = nil
	approvalRuntimeMu.Unlock()
	if rt == nil {
		return
	}
	if rt.bridge != nil {
		service.SetApprovalResumeBridge(nil)
	}
	if rt.svc != nil {
		rt.svc.SetWaitNotifier(nil)
	}
	if rt.sweeper != nil {
		rt.sweeper.Stop(context.Background())
		logger.Infof("[approval-runtime] 清扫 worker 已停止（轮次=%d 累计过期=%d）",
			rt.sweeper.Rounds(), rt.sweeper.ExpiredTotal())
	}
}

// email_runtime_wiring.go 排期邮件排水的装配层（R21）。
//
// 一句话职责：把"到期该发的邮件真的发出去"这件事挂到进程上。在此之前
// EmailSendService.ProcessPendingEmails 全仓非测试调用点为 0，而 dto.SendEmailRequest
// 同时暴露 sendTime 与 immediateSend ⇒ 用户在后台排一封 9 点的邮件会得到一条
// 合法入库、永不投递、永不报错、状态永不流转的记录。
//
// 为什么这里不加旗子（与审批运行时 FF_LTC_APPROVAL_RESUME 的三态不同）：
// 旗子的默认档决定"缺陷修没修"。审批那把旗子默认 off 改变的是"要不要让流程停下来等人工"
// 这种客户可感知的**新增**行为；这一格默认 off 等于排期邮件继续不发，
// 而它已经是产品对外承诺的能力（后台界面上就有那个时间选择器）。
// 真正的风险"停机一周后重启把老邮件一次性群发"由 EmailPendingTTL 那格挡掉，
// 不靠旗子挡。
//
// 合规判据（退订名单）走显式注入的句柄，其余读仍走包级单例：这与同日 SMS 侧
// SetSmsUnsubscribe 的取舍同源 —— 排水 worker 在独立协程里跑，
// "全局 DB 有没有被别人先设过"不该决定一封邮件该不该发给已退订的人。
package app

import (
	"context"
	"sync"

	emailsvc "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// EmailRuntime 本进程的邮件出站运行时（发送服务 + 排水节拍器）。
type EmailRuntime struct {
	svc    *emailsvc.EmailSendService
	worker *emailsvc.EmailDrainWorker
}

// DrainWorker 本进程的排水节拍器。运维手动催一轮走它的 RunOnce（导出的理由与
// 审批清扫同口径：测的那份就是生产跑的那份）。
func (r *EmailRuntime) DrainWorker() *emailsvc.EmailDrainWorker {
	if r == nil {
		return nil
	}
	return r.worker
}

// 全局登记的锁口径与审批侧相同：写入发生在 router.Setup()（HTTP 尚未开始收流量），
// 读发生在 worker 线程与测试里；Init 可被重复调用，故先停旧的再装新的。
var (
	emailRuntimeMu  sync.RWMutex
	emailRuntimeRef *EmailRuntime
)

// InitEmailRuntime 装配排期邮件排水；无库句柄时不装配并出声。
func InitEmailRuntime(gormDB *gorm.DB) *EmailRuntime {
	emailRuntimeMu.Lock()
	prev := emailRuntimeRef
	emailRuntimeRef = nil
	emailRuntimeMu.Unlock()
	if prev != nil && prev.worker != nil {
		prev.worker.Stop(context.Background())
	}

	if gormDB == nil {
		logger.Warnf("[email] ⚠️ 无 DB 句柄 ⇒ 不装配排期邮件排水：dto 的 sendTime 分支会落 pending 后永不投递、也不报错")
		return nil
	}

	svc := emailsvc.NewEmailSendService()
	svc.SetEmailUnsubscribeRepository(repository.NewEmailUnsubscribeRepository(gormDB))
	worker := emailsvc.NewEmailDrainWorker(svc, emailsvc.DefaultEmailDrainInterval)
	worker.Start(context.Background())

	rt := &EmailRuntime{svc: svc, worker: worker}
	emailRuntimeMu.Lock()
	emailRuntimeRef = rt
	emailRuntimeMu.Unlock()
	logger.Infof("[email] 排期邮件排水已装配：间隔=%s 龄上限=%s（超龄排期判过期不补发）",
		emailsvc.DefaultEmailDrainInterval, emailsvc.EmailPendingTTL)
	return rt
}

// StopEmailRuntime 停掉排水协程并清空全局引用（幂等；没装配过时也是安全的）。
//
// 与审批/草稿侧同一个已知缺口：正解位置是 cmd/api/main.go 的退出序列，而本卡改动范围
// 不含 main.go（它是并行会话正在改的文件）。入口先备好，没有它进程退出时
// 排水协程就是无人回收的泄漏。
func StopEmailRuntime() {
	emailRuntimeMu.Lock()
	rt := emailRuntimeRef
	emailRuntimeRef = nil
	emailRuntimeMu.Unlock()
	if rt == nil || rt.worker == nil {
		return
	}
	rt.worker.Stop(context.Background())
	logger.Infof("[email] 排期邮件排水 worker 已停止（轮次=%d）", rt.worker.Rounds())
}

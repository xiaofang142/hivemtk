package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	emailsvc "hivemtk-user/internal/email/service"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/mail"
	"hivemtk-user/internal/pkg/utils/logger"
)

// FeedbackService 反馈层：产物保存（LocalDriver）+ 通知（邮件）+ 失败重试调度。
// 通知渠道默认关闭（无 SMTP 配置即跳过），不影响主执行链路。
type FeedbackService struct {
	sessionRepo repository.BrowserSessionRepository
	taskRepo    repository.BrowserTaskRepository
	// hostUsersFn 批9 归属门：返回本进程当前持有 Host 连接的用户集（装配见 router 的
	// SetHostUsersProvider）。nil = 不参与过滤。
	hostUsersFn func() []uint
	// ledgerGapFn 批16b（B2）：该任务在本进程是否留有「提交尝试已跨越、写台账却没落成」的缺口。
	// 接线见 NewExecutor（单点，漏接线即整条抑制静默消失）。nil = 不参与判断。
	ledgerGapFn func(taskID uint) bool
}

func NewFeedbackService(sessionRepo repository.BrowserSessionRepository, taskRepo repository.BrowserTaskRepository) *FeedbackService {
	return &FeedbackService{sessionRepo: sessionRepo, taskRepo: taskRepo}
}

// SetHostUsersProvider 注入「本机 Host 连接归属」查询（HostRegistry.ConnectedUserIDs）。
func (f *FeedbackService) SetHostUsersProvider(fn func() []uint) { f.hostUsersFn = fn }

// SetLedgerGapProvider 注入「本机是否记着该任务的台账缺口」（Executor.HasCrossedLedgerGap）。
func (f *FeedbackService) SetLedgerGapProvider(fn func(taskID uint) bool) { f.ledgerGapFn = fn }

func (f *FeedbackService) ledgerGapHere(taskID uint) bool {
	return f.ledgerGapFn != nil && f.ledgerGapFn(taskID)
}

// ledgerGapNote 写在任务行上的原因（运维面板读不到日志，也读不到进程内存里那张兜底表）。
const ledgerGapNote = "\n写台账未落库、提交点已跨越：本任务未挂起自动重试——缺口只记在本进程内存里，" +
	"重启后没有任何东西能挡住重发，自动重跑即双发。请人工核对该内容是否已发布后改文本或换新任务重跑。"

// OnSessionFinished session 终态后的反馈动作（异步调用，勿阻塞 Executor）
//
// 批8：三步全部走 WithoutCancel 的独立预算 ctx。调用点在 ExecuteSession 收口末尾，
// 传进来的 execCtx 在超时/中止腿上必然已 Done——用它写 task 行会被 DB 驱动取消，
// 于是 session 已终态而 task 永久停在 running（任务砖化：既不能再下发，也显示不出结果）。
// R25 只对 session 行做了这个处理（executor.go 的 writeCtx），task 行是同一缺陷的第二半。
// 三步各自记可归因日志、互不牵连：快照写失败不该顺手取消挂起重试。
func (f *FeedbackService) OnSessionFinished(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, finalStatus string, success, total int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[BrowserFeedback] panic recovered: %v", r)
		}
	}()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionFinalWriteBudget)
	defer cancel()

	// 0. 先算「这次失败能不能交给自动重试」——批16b（B2）。必须在写任务快照之前定案：
	// 抑制的理由要写在同一行上，否则「为什么没重试」只剩日志里一句 Warn，面板看不到。
	retryDue := finalStatus == "failed" && task.RetryOnFail && task.RetryCount < task.MaxRetryTimes
	gapBlocked := retryDue && f.ledgerGapHere(task.ID)

	// 1. 更新任务快照（状态 + 结果摘要）
	taskStatus := "done"
	if finalStatus != "completed" {
		taskStatus = "failed"
	}
	lastResult := "steps " + itoa(success) + "/" + itoa(total) + " 成功"
	errMsg := ""
	if session.ErrorMsg != "" {
		errMsg = session.ErrorMsg
	}
	if gapBlocked {
		lastResult += ledgerGapNote
	}
	if err := f.taskRepo.UpdateRunResult(writeCtx, task.ID, taskStatus, lastResult, errMsg, task.RetryCount); err != nil {
		// 这一步失败就是砖化的起点：留给 stale_reconcile 的启动/周期对账收敛
		logger.Errorf("[BrowserFeedback] 更新任务快照失败 task=%d（待对账器收敛）: %v", task.ID, err)
	}

	// 2. 失败自动重试（session 级，一次性延迟任务）
	// 缺口拦一道：scheduleRetry 落库的挂起态是**持久化**的（重启不丢、任何持连接的实例都能认领），
	// 而兜底缺口表活在进程内存里、重启即空 ⇒ 挂上去的那次重试会在「没有闸门依据」的时刻被认领，
	// 库里那条凭据从未存在过 —— 这一跑就是双发。宁可停在这里让人重跑。
	if retryDue {
		if gapBlocked {
			logger.Warnf("[BrowserFeedback] task=%d 存在写台账缺口，自动重试已抑制（原因见任务行）", task.ID)
		} else {
			f.scheduleRetry(writeCtx, task)
		}
	}

	// 3. 失败通知（邮件；无 SMTP 配置静默跳过）
	if finalStatus == "failed" {
		f.notifyFailure(writeCtx, task, session, errMsg)
	}
}

// notifyFailure 任务失败时给归属用户发邮件（用户表无邮箱 / SMTP 未配置则静默跳过）
func (f *FeedbackService) notifyFailure(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, errMsg string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[BrowserFeedback] notify panic recovered: %v", r)
		}
	}()

	smtpSvc := emailsvc.NewEmailSmtpService()
	smtp, err := smtpSvc.GetRandEmailSmtp(ctx)
	if err != nil {
		return // 未配置 SMTP，静默跳过
	}

	var emails []string
	if err := _db.GetDB().WithContext(ctx).
		Raw(`SELECT email FROM system_users WHERE id = ? AND email <> ''`, fmt.Sprint(task.UserID)).
		Scan(&emails).Error; err != nil || len(emails) == 0 {
		return
	}

	subject := "【hivemtk】浏览器自动化任务失败 task#" + itoa(int(task.ID))
	body := fmt.Sprintf(
		"任务 %s（#%d）执行失败。\n\n会话 #%d\n起始 URL：%s\n错误：%s\n\n请登录管理端查看执行明细。",
		task.Name, task.ID, session.ID, task.Url, errMsg,
	)
	cfg := mail.Config{From: smtp.Username, Password: smtp.Password}
	if err := mail.SendMail(cfg, []string{emails[0]}, subject, body, false); err != nil {
		logger.Warnf("[BrowserFeedback] 失败通知邮件发送失败 task=%d to=%s: %v", task.ID, emails[0], err)
	} else {
		logger.Infof("[BrowserFeedback] 失败通知已发送 task=%d to=%s", task.ID, emails[0])
	}
}

// SaveFinalScreenshot 存 final_screenshot（base64 PNG → LocalDriver → URL）
func (f *FeedbackService) SaveFinalScreenshot(ctx context.Context, sessionID uint, b64PNG string) (string, error) {
	if b64PNG == "" {
		return "", nil
	}
	driver, err := newLocalDriverFromEnv()
	if err != nil {
		return "", err
	}
	raw, err := decodeBase64(b64PNG)
	if err != nil {
		return "", err
	}
	_, publicURL, err := driver.UploadReader(ctx, newBytesReader(raw), int64(len(raw)), "browser_automation", "session_"+itoa(int(sessionID))+".png")
	if err != nil {
		return "", err
	}
	return publicURL, nil
}

// scheduleRetry 失败自动重试（D4b/G5 持久化版）：原为内存 goroutine 定时器，进程重启即丢；
// 现在写 task.next_retry_at（DB 事实），由 StartRetryScanner 每分钟条件认领后触发——重启不丢。
func (f *FeedbackService) scheduleRetry(ctx context.Context, task *model.BrowserTask) {
	delay := task.RetryDelaySec
	if delay <= 0 {
		delay = 300
	}
	newCount := task.RetryCount + 1
	at := time.Now().Add(time.Duration(delay) * time.Second)
	if err := f.taskRepo.SetNextRetryAt(ctx, task.ID, &at); err != nil {
		logger.Warnf("[BrowserFeedback] 重试落库失败 task=%d: %v", task.ID, err)
		return
	}
	logger.Infof("[BrowserFeedback] 任务失败自动重试已挂起 task=%d 第 %d/%d 次，%ds 后（%s）执行（持久化，重启不丢）",
		task.ID, newCount, task.MaxRetryTimes, delay, at.Format(time.RFC3339))
}

// StartRetryScanner D4b：重试到期扫描器（每分钟）。ClaimDueRetries 条件认领（置 NULL）
// 保证多副本/双 tick 不双触发；认领后进程崩溃则该次重试放弃（与旧语义一致，但正常运行期重启不再丢）。
// 批9：认领前过归属门（见 scanDueRetries）。
func (f *FeedbackService) StartRetryScanner(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				f.scanDueRetries(ctx)
			}
		}
	}()
}

func (f *FeedbackService) scanDueRetries(ctx context.Context) {
	// 批9 归属门：Host 连接是进程内状态，挂起重试是库内共享队列。本进程没有该用户的
	// 连接却认领了它的重试，只会以「browser host 未连接」烧掉一次 MaxRetryTimes 额度
	// （真机实测 task=377 session=429）。provider 未装配时不过滤——宁可退化成改造前
	// 行为，也不让一次装配遗漏静默停掉全部重试。
	var owners []uint
	if f.hostUsersFn != nil {
		owners = f.hostUsersFn()
		if len(owners) == 0 {
			logger.Infof("[BrowserFeedback] 本轮重试扫描跳过：本机无 Host 连接（挂起重试留给持有连接的实例认领）")
			return
		}
	}
	due, err := f.taskRepo.ClaimDueRetries(ctx, time.Now(), 10, owners)
	if err != nil {
		logger.Warnf("[BrowserFeedback] 重试扫描失败: %v", err)
		return
	}
	for _, t := range due {
		newCount := t.RetryCount + 1
		logger.Infof("[BrowserFeedback] 认领到期重试 task=%d 第 %d/%d 次", t.ID, newCount, t.MaxRetryTimes)
		if err := f.runRetry(ctx, t, newCount); err != nil {
			logger.Warnf("[BrowserFeedback] 重试触发失败 task=%d: %v", t.ID, err)
		}
	}
}

// runRetry 由 TaskService 注入的重试执行器（避免 import cycle，见 SetRetryRunner）
type retryRunner func(ctx context.Context, taskID, userID uint, retryCount int) error

var retryRunnerFn retryRunner

// SetRetryRunner 路由装配时注入 taskSvc.RunTaskWithRetry
func SetRetryRunner(fn retryRunner) { retryRunnerFn = fn }

func (f *FeedbackService) runRetry(ctx context.Context, task *model.BrowserTask, newCount int) error {
	// 批16b（B2）第二道：OnSessionFinished 那道判定发生在「上一轮结束时」，而认领发生在
	// 几分钟后的扫描里——中间这段时间同一任务可能又被跑过一次（人工重跑/另一条会话），
	// 新缺口正是在那一刻留下的。只查一次就是拿旧结论放行新事实。
	if f.ledgerGapHere(task.ID) {
		return fmt.Errorf("重试拒绝起跑:该任务在本进程留有一条写台账缺口（提交尝试已跨越但未落库），自动重跑即双发，请人工核对内容是否已发布")
	}
	if retryRunnerFn == nil {
		return errors.New("retry runner 未装配（SetRetryRunner）")
	}
	return retryRunnerFn(ctx, task.ID, task.UserID, newCount)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

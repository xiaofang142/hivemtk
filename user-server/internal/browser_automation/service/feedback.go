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
}

func NewFeedbackService(sessionRepo repository.BrowserSessionRepository, taskRepo repository.BrowserTaskRepository) *FeedbackService {
	return &FeedbackService{sessionRepo: sessionRepo, taskRepo: taskRepo}
}

// OnSessionFinished session 终态后的反馈动作（异步调用，勿阻塞 Executor）
func (f *FeedbackService) OnSessionFinished(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, finalStatus string, success, total int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[BrowserFeedback] panic recovered: %v", r)
		}
	}()

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
	if err := f.taskRepo.UpdateRunResult(ctx, task.ID, taskStatus, lastResult, errMsg, task.RetryCount); err != nil {
		logger.Warnf("[BrowserFeedback] 更新任务快照失败 task=%d: %v", task.ID, err)
	}

	// 2. 失败自动重试（session 级，一次性延迟任务）
	if finalStatus == "failed" && task.RetryOnFail && task.RetryCount < task.MaxRetryTimes {
		f.scheduleRetry(ctx, task)
	}

	// 3. 失败通知（邮件；无 SMTP 配置静默跳过）
	if finalStatus == "failed" {
		f.notifyFailure(ctx, task, session, errMsg)
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

// scheduleRetry 延迟 retry_delay_sec 后重新触发 RunTask
func (f *FeedbackService) scheduleRetry(ctx context.Context, task *model.BrowserTask) {
	delay := task.RetryDelaySec
	if delay <= 0 {
		delay = 300
	}
	newCount := task.RetryCount + 1
	logger.Infof("[BrowserFeedback] 任务失败自动重试 task=%d 第 %d/%d 次，%ds 后执行", task.ID, newCount, task.MaxRetryTimes, delay)
	// 延迟执行：goroutine + timer（轻量，不占用 cron 槽位；进程重启则放弃本次重试，语义可接受）
	go func() {
		select {
		case <-time.After(time.Duration(delay) * time.Second):
		case <-ctx.Done():
			return
		}
		if err := f.runRetry(ctx, task, newCount); err != nil {
			logger.Warnf("[BrowserFeedback] 重试触发失败 task=%d: %v", task.ID, err)
		}
	}()
}

// runRetry 由 TaskService 注入的重试执行器（避免 import cycle，见 SetRetryRunner）
type retryRunner func(ctx context.Context, taskID, userID uint, retryCount int) error

var retryRunnerFn retryRunner

// SetRetryRunner 路由装配时注入 taskSvc.RunTaskWithRetry
func SetRetryRunner(fn retryRunner) { retryRunnerFn = fn }

func (f *FeedbackService) runRetry(ctx context.Context, task *model.BrowserTask, newCount int) error {
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

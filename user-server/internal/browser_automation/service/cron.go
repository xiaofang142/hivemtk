package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	pkgcron "hivemtk-user/internal/pkg/cron"
	"hivemtk-user/internal/pkg/utils/logger"

	cronv3 "github.com/robfig/cron/v3"
)

// CronService 定时触发器：CRUD + TaskManager 集成。
// CronExpr 存储 5 段（前端展示习惯），注册前补 "0 " 秒段转 6 段（项目 cron.WithSeconds）。
type CronService struct {
	cronRepo repository.BrowserCronTriggerRepository
	taskRepo repository.BrowserTaskRepository
	taskSvc  *TaskService

	mu       sync.Mutex
	entryIDs map[uint]cronv3.EntryID // taskID → cron entry
}

func NewCronService(cronRepo repository.BrowserCronTriggerRepository, taskRepo repository.BrowserTaskRepository, taskSvc *TaskService) *CronService {
	return &CronService{
		cronRepo: cronRepo,
		taskRepo: taskRepo,
		taskSvc:  taskSvc,
		entryIDs: make(map[uint]cronv3.EntryID),
	}
}

// toSixField 5 段 cron → 项目 TaskManager 的 6 段秒级；
// timeZone 非空时前缀 CRON_TZ=（robfig v3 原生支持，G20：调度不依赖服务器本地时区）。
func toSixField(expr, timeZone string) string {
	expr = strings.TrimSpace(expr)
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		expr = "0 " + expr
	}
	if tz := strings.TrimSpace(timeZone); tz != "" {
		return "CRON_TZ=" + tz + " " + expr
	}
	return expr
}

// cronParser 6 段秒级解析器（NewParser 原生支持 TZ=/CRON_TZ= 前缀，与 TaskManager 配置一致，用于提前校验）
var cronParser = cronv3.NewParser(cronv3.Second | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)

// ValidateCronExpr 校验 5 段 cron 表达式（转 6 段后解析）+ 时区合法性（G20）。timeZone 空=服务器本地。
func ValidateCronExpr(expr, timeZone string) error {
	tz := strings.TrimSpace(timeZone)
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("时区无效（需 IANA 名，如 Asia/Shanghai）: %w", err)
		}
	}
	if _, err := cronParser.Parse(toSixField(expr, tz)); err != nil {
		return fmt.Errorf("cron 表达式无效（需 5 段「分 时 日 月 周」或 6 段「秒 分 时 日 月 周」，如 */5 * * * *）: %w", err)
	}
	return nil
}

// nextRunAt from 之后的第一个触发时刻，判据与注册时交给 TaskManager 的 spec 同源
// （同一个 toSixField + cronParser，所以「展示的下一次」不会和「实际会触发的时刻」分家）。
// 解析不出来时返回 nil 而不是猜一个：调用方把它写成 NULL，界面显示「—」，
// 而回一个 now 之类的假数就是把这一列变成第二个缺陷（见 registerWithUser 的批注）。
func nextRunAt(expr, timeZone string, from time.Time) *time.Time {
	sched, err := cronParser.Parse(toSixField(expr, strings.TrimSpace(timeZone)))
	if err != nil {
		return nil
	}
	next := sched.Next(from)
	return &next
}

func (s *CronService) Create(ctx context.Context, userID uint, taskID uint, cronExpr, timeZone string, enabled bool) (*model.BrowserCronTrigger, error) {
	t, err := s.taskRepo.GetByID(ctx, taskID, userID)
	if err != nil {
		// 主语必须是「任务」：这一条 404 说的是「你要挂触发器的那条任务不归你/不存在」，
		// 回「触发器不存在」会把用户支到错误的排查方向。
		return nil, notFound("任务不存在")
	}
	if t.TaskType != "cron" {
		return nil, invalidInput("仅 cron 类型任务可配置定时触发器")
	}
	if err := ValidateCronExpr(cronExpr, timeZone); err != nil {
		return nil, invalidInput("%s", err)
	}
	if _, err := s.cronRepo.GetByTaskID(ctx, taskID); err == nil {
		return nil, stateConflict("该任务已存在触发器")
	}
	tr := &model.BrowserCronTrigger{TaskID: taskID, CronExpr: strings.TrimSpace(cronExpr), TimeZone: strings.TrimSpace(timeZone), Enabled: enabled}
	if err := s.cronRepo.Create(ctx, tr); err != nil {
		return nil, err
	}
	s.applySchedule(ctx, tr, userID)
	return tr, nil
}

func (s *CronService) Update(ctx context.Context, id, userID uint, cronExpr, timeZone string) (*model.BrowserCronTrigger, error) {
	tr, err := s.cronRepo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	// 校验结论必须是 InvalidInputError（=400），不能是裸 error。裸 error 会掉进
	// baErrToResponse 的 default → 500 INTERNAL_ERROR_6002：实测表达式写错的人收到
	// 「服务器内部错误 + 回显 robfig 原文」，既告诉他该改什么又告诉他这是服务端的锅。
	// 与 Create 同口径：同一条判据在两个入口上给出两种 HTTP 结论，等于没有判据。
	if err := ValidateCronExpr(cronExpr, timeZone); err != nil {
		return nil, invalidInput("%s", err)
	}
	tr.CronExpr = strings.TrimSpace(cronExpr)
	tr.TimeZone = strings.TrimSpace(timeZone)
	if err := s.cronRepo.Update(ctx, tr); err != nil {
		return nil, err
	}
	s.applySchedule(ctx, tr, userID)
	return tr, nil
}

func (s *CronService) List(ctx context.Context, userID uint) ([]*model.BrowserCronTrigger, error) {
	return s.cronRepo.ListByUser(ctx, userID)
}

func (s *CronService) Delete(ctx context.Context, id, userID uint) error {
	tr, err := s.cronRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	s.unregister(tr.TaskID)
	return s.cronRepo.Delete(ctx, id, userID)
}

// RemoveByTask 宿主任务侧的生命周期变化（任务被删、类型不再是 cron）时的回收入口：
// 先注销进程内的排程条目，再删库里的触发行。两步都不能少——
// 只删行会留下一个每次醒来都查不到行、于是静默 return 的僵尸条目（见 registerWithUser
// 触发时的 enabled 再校验）；只注销不删行则列表里留一条幽灵，重启后它又会被 RestoreAll 捞回来。
func (s *CronService) RemoveByTask(ctx context.Context, taskID uint) error {
	s.unregister(taskID)
	return s.cronRepo.DeleteByTaskID(ctx, taskID)
}

func (s *CronService) SetEnabled(ctx context.Context, id, userID uint, enabled bool) error {
	tr, err := s.cronRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if err := s.cronRepo.UpdateEnabled(ctx, tr.ID, enabled); err != nil {
		return err
	}
	tr.Enabled = enabled // applySchedule 以内存里的 enabled 决定注册与 next_run_at 的有无
	s.applySchedule(ctx, tr, userID)
	return nil
}

// applySchedule 触发器调度状态的唯一出口：按 enabled 注册/注销 TaskManager，并把 next_run_at
// 同步成「按当前表达式算出的下一次」（停用=NULL，读作「没有排定的下一次」）。
// 新建/改表达式/启停/重启恢复四个入口都走这里，这一列才只有一个写入方——
// 旧实现只在触发回调里写，且和 last_run_at 一起写成同一个 now，
// 结果是：第一次触发前恒 NULL，触发后恒等于「上一次触发的时刻」，两个读数都不是列名的意思。
func (s *CronService) applySchedule(ctx context.Context, tr *model.BrowserCronTrigger, userID uint) {
	s.unregister(tr.TaskID)
	if !tr.Enabled {
		s.storeNextRun(ctx, tr, nil)
		return
	}
	s.registerWithUser(tr, userID)
	s.storeNextRun(ctx, tr, nextRunAt(tr.CronExpr, tr.TimeZone, time.Now()))
}

// storeNextRun 只推进 next_run_at（last_run_at 原样回写：UpdateTimes 一次写这一对时刻）。
// 记账失败不改变本次请求的结论：调度已经注册好了，少一个展示用的时间戳不该让新建/启停报错。
func (s *CronService) storeNextRun(ctx context.Context, tr *model.BrowserCronTrigger, next *time.Time) {
	tr.NextRunAt = next
	if err := s.cronRepo.UpdateTimes(ctx, tr.ID, next, tr.LastRunAt); err != nil {
		logger.Warnf("[BrowserCron] 更新 next_run_at 失败 trigger=%d: %v", tr.ID, err)
	}
}

// registerWithUser 注册到项目 TaskManager（秒级 spec），触发时以归属用户身份 RunTask
func (s *CronService) registerWithUser(tr *model.BrowserCronTrigger, userID uint) {
	taskID := tr.TaskID
	mgr := pkgcron.GetTaskManager()
	entry, err := mgr.AddTask(toSixField(tr.CronExpr, tr.TimeZone), func() {
		ctx, cancel := context.WithTimeout(context.Background(), cronTriggerOpTimeout)
		defer cancel()
		// 触发时再次校验 enabled（Disable 与 fire 竞态窗口）
		cur, err := s.cronRepo.GetByTaskID(ctx, taskID)
		if err != nil || cur == nil || !cur.Enabled {
			return
		}
		now := time.Now()
		// last=本次触发时刻，next=本次之后的下一次（按 trigger 主键 cur.ID 更新）
		_ = s.cronRepo.UpdateTimes(ctx, cur.ID, nextRunAt(cur.CronExpr, cur.TimeZone, now), &now)
		if _, err := s.taskSvc.RunTask(ctx, taskID, userID, 0); err != nil {
			logger.Warnf("[BrowserCron] 触发执行失败 task=%d: %v", taskID, err)
		}
	})
	s.mu.Lock()
	if err == nil {
		s.entryIDs[taskID] = entry
	}
	s.mu.Unlock()
	if err != nil {
		logger.Warnf("[BrowserCron] 注册失败 task=%d expr=%s: %v", taskID, tr.CronExpr, err)
	}
}

func (s *CronService) unregister(taskID uint) {
	s.mu.Lock()
	entry, ok := s.entryIDs[taskID]
	if ok {
		delete(s.entryIDs, taskID)
	}
	s.mu.Unlock()
	if ok {
		pkgcron.GetTaskManager().RemoveTask(entry)
	}
}

// RestoreAll 进程启动后恢复已启用触发器（taskID → userID 反查）
func (s *CronService) RestoreAll(ctx context.Context) {
	list, err := s.cronRepo.ListAllEnabled(ctx)
	if err != nil {
		logger.Warnf("[BrowserCron] 恢复触发器失败: %v", err)
		return
	}
	for _, tr := range list {
		t, err := s.taskRepo.GetByIDAnyUser(ctx, tr.TaskID)
		if err != nil {
			continue
		}
		// 走 applySchedule 而不是裸注册：重启后 next_run_at 也要跟着推进
		// （进程停机期间错过的表达式若只补注册，这一列会一直停在停机前的旧值）
		s.applySchedule(ctx, tr, t.UserID)
	}
	if len(list) > 0 {
		logger.Infof("[BrowserCron] 已恢复 %d 个定时触发器", len(list))
	}
}

package service

import (
	"context"
	"errors"
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

// toSixField 5 段 cron → 项目 TaskManager 的 6 段秒级
func toSixField(expr string) string {
	expr = strings.TrimSpace(expr)
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		return "0 " + expr
	}
	return expr
}

// cronParser 6 段秒级解析器（与 TaskManager 内部配置一致，用于提前校验）
var cronParser = cronv3.NewParser(cronv3.Second | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)

// ValidateCronExpr 校验 5 段 cron 表达式（转 6 段后解析）
func ValidateCronExpr(expr string) error {
	spec := toSixField(expr)
	if _, err := cronParser.Parse(spec); err != nil {
		return fmt.Errorf("cron 表达式无效（需 5 段格式，如 */5 * * * *）: %w", err)
	}
	return nil
}

func (s *CronService) Create(ctx context.Context, userID uint, taskID uint, cronExpr string, enabled bool) (*model.BrowserCronTrigger, error) {
	t, err := s.taskRepo.GetByID(ctx, taskID, userID)
	if err != nil {
		return nil, errors.New("任务不存在")
	}
	if t.TaskType != "cron" {
		return nil, errors.New("仅 cron 类型任务可配置定时触发器")
	}
	if err := ValidateCronExpr(cronExpr); err != nil {
		return nil, err
	}
	if _, err := s.cronRepo.GetByTaskID(ctx, taskID); err == nil {
		return nil, errors.New("该任务已存在触发器")
	}
	tr := &model.BrowserCronTrigger{TaskID: taskID, CronExpr: strings.TrimSpace(cronExpr), Enabled: enabled}
	if err := s.cronRepo.Create(ctx, tr); err != nil {
		return nil, err
	}
	if tr.Enabled {
		s.registerWithUser(tr, userID)
	}
	return tr, nil
}

func (s *CronService) Update(ctx context.Context, id, userID uint, cronExpr string) (*model.BrowserCronTrigger, error) {
	tr, err := s.cronRepo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if err := ValidateCronExpr(cronExpr); err != nil {
		return nil, err
	}
	tr.CronExpr = strings.TrimSpace(cronExpr)
	if err := s.cronRepo.Update(ctx, tr); err != nil {
		return nil, err
	}
	s.unregister(tr.TaskID)
	if tr.Enabled {
		s.registerWithUser(tr, userID)
	}
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

func (s *CronService) SetEnabled(ctx context.Context, id, userID uint, enabled bool) error {
	tr, err := s.cronRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if err := s.cronRepo.UpdateEnabled(ctx, tr.ID, enabled); err != nil {
		return err
	}
	s.unregister(tr.TaskID)
	if enabled {
		tr.Enabled = true
		s.registerWithUser(tr, userID)
	}
	return nil
}

// registerWithUser 注册到项目 TaskManager（秒级 spec），触发时以归属用户身份 RunTask
func (s *CronService) registerWithUser(tr *model.BrowserCronTrigger, userID uint) {
	taskID := tr.TaskID
	mgr := pkgcron.GetTaskManager()
	entry, err := mgr.AddTask(toSixField(tr.CronExpr), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// 触发时再次校验 enabled（Disable 与 fire 竞态窗口）
		cur, err := s.cronRepo.GetByTaskID(ctx, taskID)
		if err != nil || cur == nil || !cur.Enabled {
			return
		}
		now := time.Now()
		_ = s.cronRepo.UpdateTimes(ctx, cur.ID, now, now) // next_run_at 由 cron 框架调度，此处记录 last（按 trigger 主键）
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
		s.registerWithUser(tr, t.UserID)
	}
	if len(list) > 0 {
		logger.Infof("[BrowserCron] 已恢复 %d 个定时触发器", len(list))
	}
}

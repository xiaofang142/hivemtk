package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// TaskService 任务服务：CRUD + publish/pause/resume/archive + RunTask（异步）
type TaskService struct {
	taskRepo    repository.BrowserTaskRepository
	sessionRepo repository.BrowserSessionRepository
	executor    *Executor
}

func NewTaskService(taskRepo repository.BrowserTaskRepository, sessionRepo repository.BrowserSessionRepository, executor *Executor) *TaskService {
	return &TaskService{taskRepo: taskRepo, sessionRepo: sessionRepo, executor: executor}
}

// ErrInvalidURL URL scheme 不在白名单（http/https）
var ErrInvalidURL = errors.New("url 仅支持 http/https 协议")

// ErrTaskRunning 同任务已有 running session（幂等拒绝）
var ErrTaskRunning = errors.New("任务正在执行中，请勿重复触发")

// ErrDependencyNotMet 前置依赖未满足
var ErrDependencyNotMet = errors.New("前置依赖任务未满足")

// ValidateURL scheme 白名单：仅 http/https（防 chrome://、file://、内网 file 探测）
func ValidateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ErrInvalidURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrInvalidURL
	}
	if u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}

func (s *TaskService) Create(ctx context.Context, userID uint, t *model.BrowserTask) error {
	if err := ValidateURL(t.Url); err != nil {
		return err
	}
	// 平台校验：必须是已注册适配器（P5 注册表唯一入口）；空值走缺省小红书
	t.Platform = strings.TrimSpace(t.Platform)
	if t.Platform == "" {
		t.Platform = "xiaohongshu"
	}
	if _, err := platform.Get(t.Platform); err != nil {
		return err
	}
	t.UserID = userID
	if t.TaskType == "" {
		t.TaskType = "one_shot"
	}
	if t.Status == "" {
		t.Status = "draft"
	}
	if t.LoopCount < 1 {
		t.LoopCount = 1
	}
	if t.TimeoutSec <= 0 {
		t.TimeoutSec = 120
	}
	if t.DependsOnTaskID != nil {
		if err := s.checkDependencyCycle(ctx, userID, t.ID, *t.DependsOnTaskID); err != nil {
			return err
		}
		if err := s.validateDependencyMode(t); err != nil {
			return err
		}
	}
	return s.taskRepo.Create(ctx, t)
}

func (s *TaskService) Get(ctx context.Context, id, userID uint) (*model.BrowserTask, error) {
	return s.taskRepo.GetByID(ctx, id, userID)
}

func (s *TaskService) List(ctx context.Context, userID uint, status, taskType string, page, limit int) ([]*model.BrowserTask, int64, error) {
	return s.taskRepo.List(ctx, userID, status, taskType, page, limit)
}

func (s *TaskService) Update(ctx context.Context, id, userID uint, mutator func(*model.BrowserTask) error) (*model.BrowserTask, error) {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if t.Status == "running" {
		return nil, errors.New("任务执行中，暂停后再编辑")
	}
	if err := mutator(t); err != nil {
		return nil, err
	}
	if err := ValidateURL(t.Url); err != nil {
		return nil, err
	}
	if err := s.taskRepo.Update(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TaskService) Delete(ctx context.Context, id, userID uint) error {
	return s.taskRepo.SoftDelete(ctx, id, userID)
}

// Publish draft → ready
func (s *TaskService) Publish(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status != "draft" {
		return fmt.Errorf("仅草稿可发布，当前状态: %s", t.Status)
	}
	if t.BrainMode && strings.TrimSpace(t.BrainGoal) == "" {
		return errors.New("Brain 模式必须填写 brain_goal")
	}
	if !t.BrainMode && len(t.Steps) == 0 {
		return errors.New("显式模式至少编排一个步骤")
	}
	return s.taskRepo.UpdateStatus(ctx, t.ID, "ready", "")
}

func (s *TaskService) Pause(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status != "running" {
		return fmt.Errorf("仅执行中任务可暂停，当前状态: %s", t.Status)
	}
	// pause 语义 = stop 当前 running session（见设计文档 §9）
	sessions, _, err := s.sessionRepo.ListByTaskID(ctx, t.ID, userID, 1, 10)
	if err == nil && len(sessions) > 0 {
		for _, sess := range sessions {
			if sess.Status == "created" || sess.Status == "active" {
				s.executor.SignalStop(sess.ID)
			}
		}
	}
	return s.taskRepo.UpdateStatus(ctx, t.ID, "paused", "")
}

func (s *TaskService) Resume(ctx context.Context, id, userID uint) (*model.BrowserSession, error) {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if t.Status != "paused" {
		return nil, fmt.Errorf("仅暂停任务可恢复，当前状态: %s", t.Status)
	}
	return s.RunTask(ctx, t.ID, userID, t.RetryCount)
}

func (s *TaskService) Archive(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status == "running" {
		return errors.New("任务执行中，不能归档")
	}
	return s.taskRepo.UpdateStatus(ctx, t.ID, "archived", "")
}

// SetDependency 设置/变更 workflow 依赖（DFS 检环）
func (s *TaskService) SetDependency(ctx context.Context, id, userID uint, dependsOnTaskID *uint, mode string) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if dependsOnTaskID != nil {
		if *dependsOnTaskID == t.ID {
			return errors.New("任务不能依赖自身")
		}
		dep, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOnTaskID)
		if err != nil {
			return errors.New("前置任务不存在")
		}
		if dep.UserID != userID {
			return errors.New("前置任务不存在")
		}
		if err := s.checkDependencyCycle(ctx, userID, t.ID, *dependsOnTaskID); err != nil {
			return err
		}
		t.DependsOnTaskID = dependsOnTaskID
		t.DependsOnMode = mode
		if t.DependsOnMode == "" {
			t.DependsOnMode = "all_done"
		}
	} else {
		t.DependsOnTaskID = nil
		t.DependsOnMode = ""
	}
	return s.taskRepo.Update(ctx, t)
}

// checkDependencyCycle DFS 检测 A→B→…→A 环
func (s *TaskService) checkDependencyCycle(ctx context.Context, userID, taskID, dependsOn uint) error {
	const maxDepth = 64
	visited := map[uint]bool{taskID: true}
	current := dependsOn
	for depth := 0; depth < maxDepth; depth++ {
		if current == taskID {
			return errors.New("检测到任务依赖环")
		}
		if visited[current] {
			return nil
		}
		visited[current] = true
		t, err := s.taskRepo.GetByIDAnyUser(ctx, current)
		if err != nil {
			return nil // 前置链断裂，无环
		}
		if t.DependsOnTaskID == nil {
			return nil
		}
		current = *t.DependsOnTaskID
	}
	return nil
}

func (s *TaskService) validateDependencyMode(t *model.BrowserTask) error {
	switch t.DependsOnMode {
	case "", "all_done", "any_success":
		return nil
	default:
		return fmt.Errorf("未知依赖模式: %s", t.DependsOnMode)
	}
}

// checkDependency 执行前置依赖校验
func (s *TaskService) checkDependency(ctx context.Context, t *model.BrowserTask) error {
	if t.DependsOnTaskID == nil {
		return nil
	}
	switch t.DependsOnMode {
	case "any_success":
		ok, err := s.sessionRepo.HasSuccess(ctx, *t.DependsOnTaskID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: 前置任务从未成功执行过", ErrDependencyNotMet)
		}
	default: // all_done
		last, err := s.sessionRepo.GetLatestByTaskID(ctx, *t.DependsOnTaskID)
		if err != nil {
			return fmt.Errorf("%w: 前置任务从未执行过", ErrDependencyNotMet)
		}
		if last.Status != "completed" {
			return fmt.Errorf("%w: 前置任务最近一次执行状态为 %s", ErrDependencyNotMet, last.Status)
		}
	}
	return nil
}

// RunTask 校验（归属/幂等/依赖）→ 创建 Session → goroutine 异步执行 → 立即返回。
// retryCount>0 时由 FeedbackService 重试链路调用（跳过 running 幂等检查，计数落库）。
func (s *TaskService) RunTask(ctx context.Context, taskID, userID uint, retryCount int) (*model.BrowserSession, error) {
	t, err := s.taskRepo.GetByID(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	if t.Status == "running" {
		return nil, ErrTaskRunning
	}
	if t.Status != "ready" && t.Status != "paused" && t.Status != "done" && t.Status != "failed" {
		return nil, fmt.Errorf("任务状态 %s 不可执行（需先 publish）", t.Status)
	}
	// 幂等：同任务已有运行中 session 则拒绝（并发触发防重，靠 DB 唯一性兜底竞态）
	runningByTask, err := s.sessionRepo.CountRunningByTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if runningByTask > 0 {
		return nil, ErrTaskRunning
	}

	if err := s.checkDependency(ctx, t); err != nil {
		return nil, err
	}

	// 解析 steps
	steps, err := ParseSteps(t.Steps)
	if err != nil {
		return nil, err
	}
	if !t.BrainMode && len(steps) == 0 {
		return nil, errors.New("任务未编排步骤")
	}

	session := &model.BrowserSession{
		TaskID: t.ID,
		UserID: userID,
		Url:    t.Url,
		Status: "created",
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}
	if retryCount > 0 {
		t.RetryCount = retryCount
	}
	// 任务快照：running
	if err := s.taskRepo.UpdateStatus(ctx, t.ID, "running", ""); err != nil {
		logger.Warnf("[BrowserTask] 更新任务 running 状态失败 task=%d: %v", t.ID, err)
	}

	// 异步执行：SafeGoDetached 剥除请求 ctx 的取消链（HTTP 响应返回即 cancel，
	// 普通 SafeGo 会让 Executor 在第一步就 ctx.Err() != nil 退出）；超时 = task.TimeoutSec
	utils.SafeGoDetached(ctx, "browser_automation.run", time.Duration(t.TimeoutSec)*time.Second+30*time.Second, func(runCtx context.Context) {
		execCtx, cancel := context.WithTimeout(runCtx, time.Duration(t.TimeoutSec)*time.Second)
		defer cancel()
		s.executor.ExecuteSession(execCtx, t, session, steps)
	})
	return session, nil
}

// MarshalSteps 编排步骤数组落库前序列化
func (s *TaskService) MarshalSteps(items []json.RawMessage) ([]byte, error) {
	return json.Marshal(items)
}

// RunTaskWithRetry FeedbackService 失败自动重试的入口（SetRetryRunner 装配）。
// 传 retryCount>0 使 RunTask 把计数落库；ctx 用 Background（重试链路无请求 ctx）。
func (s *TaskService) RunTaskWithRetry(ctx context.Context, taskID, userID uint, retryCount int) error {
	_, err := s.RunTask(ctx, taskID, userID, retryCount)
	return err
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

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
	// triggerRemover 由装配处注入（CronService）：nil = 本进程没有定时触发器面（测试装配常见）。
	triggerRemover TriggerRemover
	// profileHealthRepo 主 Profile 健康仓储（熔断门/人工恢复）：nil = 熔断面关闭（测试装配常见）。
	profileHealthRepo repository.BrowserProfileHealthRepository
	// outreachReceiptRepo 触达回执读侧（Chunk5 验收面）：nil = 回执列表恒空（装配常见于测试）。
	outreachReceiptRepo repository.BrowserOutreachReceiptRepository
}

// TriggerRemover 宿主任务生命周期变化时回收其定时触发器。
// 判据来自触发器自己的两条前置：Create 只认 task_type=cron 的任务、RunTask 只认已发布的任务；
// 任务被删除或类型被改走之后，这两条前置都不再由任务的编辑/删除入口负责，
// 所以回收必须发生在改动的同一处，否则就进入一个「新建时拒绝、改一改却能抵达」的状态。
type TriggerRemover interface {
	RemoveByTask(ctx context.Context, taskID uint) error
}

func (s *TaskService) SetTriggerRemover(r TriggerRemover) { s.triggerRemover = r }

// SetProfileHealthRepo 主 Profile 熔断面注入：nil = 熔断门关闭（测试装配常见）。
func (s *TaskService) SetProfileHealthRepo(r repository.BrowserProfileHealthRepository) {
	s.profileHealthRepo = r
}

// removeTriggers 回收失败不改写本次结论：任务已经删掉/改完了，此时回 500 只会让人
// 以为操作没发生而反复点。失败记进日志（列表侧也有「宿主已不可执行」的可见面兜底）。
func (s *TaskService) removeTriggers(ctx context.Context, taskID uint) {
	if s.triggerRemover == nil {
		return
	}
	if err := s.triggerRemover.RemoveByTask(ctx, taskID); err != nil {
		logger.Warnf("[BrowserTask] 回收任务 %d 的定时触发器失败: %v", taskID, err)
	}
}

func NewTaskService(taskRepo repository.BrowserTaskRepository, sessionRepo repository.BrowserSessionRepository, executor *Executor) *TaskService {
	return &TaskService{taskRepo: taskRepo, sessionRepo: sessionRepo, executor: executor}
}

// ErrInvalidURL URL scheme 不在白名单（http/https）
var ErrInvalidURL = errors.New("url 仅支持 http/https 协议")

// ErrTaskRunning 同任务已有 running session（幂等拒绝）
var ErrTaskRunning = errors.New("任务正在执行中，请勿重复触发")

// ErrUserBusy 并发闸（F8/G18）：同用户已有 running session。
// 根因：同 Host 连接上命令逻辑级交织（帧串行≠会话串行），平台「写操作串行」风控要求无闸兜底。
var ErrUserBusy = errors.New("已有浏览器任务执行中（同一 Host 串行），请等待其结束或先停止")

// ErrDependencyNotMet 前置依赖未满足
var ErrDependencyNotMet = errors.New("前置依赖任务未满足")

// StateConflictError 目标状态不满足这条操作的前置条件（草稿没发布就执行、非执行中却暂停…）。
// InvalidInputError 入参本身越界（数值范围、未知枚举、缺字段），与状态无关。
// 两类结论此前用裸 fmt.Errorf / errors.New 抛出 → controller default 分支 = HTTP 500：
// 真机 UI 腿点 draft 任务的「执行」，服务端把「还没发布」这种用户误操作记成内部错误。
// 给它们类型，错误码映射才有落脚点。
type StateConflictError struct{ Msg string }

func (e *StateConflictError) Error() string { return e.Msg }

type InvalidInputError struct{ Msg string }

func (e *InvalidInputError) Error() string { return e.Msg }

// NotFoundError 「哪样东西找不到」是结论的一部分：状态码一样（404）时，文案的主语
// 决定用户往哪儿排查（触发器挂在别人的任务上，回「触发器不存在」就是错方向）。
// 域内其余 to-be-typed 错误同构：类型给 controller 分流，Msg 给用户看原因。
type NotFoundError struct{ Msg string }

func (e *NotFoundError) Error() string { return e.Msg }

func notFound(format string, a ...any) error {
	return &NotFoundError{Msg: fmt.Sprintf(format, a...)}
}

func stateConflict(format string, a ...any) error {
	return &StateConflictError{Msg: fmt.Sprintf(format, a...)}
}

func invalidInput(format string, a ...any) error {
	return &InvalidInputError{Msg: fmt.Sprintf(format, a...)}
}

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
		if err := s.checkDependencyTarget(ctx, userID, t.ID, t.DependsOnTaskID); err != nil {
			return err
		}
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
		return nil, stateConflict("任务执行中，暂停后再编辑")
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
	// 类型改走 = 触发器的前置没了（Create 明确拒绝非 cron 任务挂触发器）。
	// 不在这里收就是「建的时候拦、改的时候放」：cron 任务编辑成 one_shot 之后，
	// 触发器仍按分钟唤起它，而 /cron 的读数（已启用/下次触发/上次触发）全部正常。
	if t.TaskType != "cron" {
		s.removeTriggers(ctx, t.ID)
	}
	return t, nil
}

func (s *TaskService) Delete(ctx context.Context, id, userID uint) error {
	if err := s.taskRepo.SoftDelete(ctx, id, userID); err != nil {
		return err
	}
	// 任务没了，挂在它上面的定时器必须跟着停。此前不级联时的实测形态：删除任务后
	// 触发器仍出现在列表里（列表的 JOIN 不过滤任务的 deleted_at），进程内条目照旧每次醒来、
	// last_run_at 与 next_run_at 照常推进，而每一次都在 RunTask 上拿到「任务不存在」——
	// 界面读起来一切正常，实际一场都没跑。
	s.removeTriggers(ctx, id)
	// 同一条纪律的另一个面：定时器之外，任务还挂着「在途会话」，而会话这条不收敛会锁死人。
	s.convergeInFlightSessions(ctx, id)
	return nil
}

// sessionDeleteReason 收口原文要自证「是删除动作终止的，不是平台跑失败」。
// 与 stopped 档位一起读才成立：failed 会把人引向「去查编排/选择器」，而这里没有任何执行出错。
const sessionDeleteReason = "任务已删除，执行中止"

// convergeInFlightSessions 删除任务时收敛其名下仍在途的会话。
//
// 不收敛的实测后果（活库：删除任务后 3 条会话一行未动，其中 1 条停在 active）：
// CountRunningByUser 按 created/active 计数，一条永不终态的会话把用户的并发闸永久占住
// ——此后每次下发都拿到 ErrUserBusy「已有浏览器任务执行中」，而那个任务在列表里已经不存在；
// 对账器本应兜这一格，但它按 running 的任务选行，任务被软删后永远扫不到，
// 等于删除动作亲手关掉了唯一的兜底路径。
//
// 分两半收敛不是偷懒，两半各自防一种危害：
//   - 执行协程在本进程（stopRegistry 有这一条）：只发中止信号，终态留给执行方写。
//     就地写终态会立刻放闸，用户随即能再发一个任务，而旧协程还在往同一条 Host 连接发命令
//     ——命令在一个浏览器上逻辑级交织，写操作不可撤回（这正是并发闸存在的理由）。
//   - 执行协程不在本进程（进程重启留下的僵尸会话）：没有人在跑，就地收口为 stopped。
//
// 已终态的会话不在清单里，一个都不会被改写：删除任务销毁的是任务，不是它真实跑过的凭据
// （command_log 的 append-only 立场在同一处）。
//
// 收口失败不改成删除失败：任务已经删掉了，此处回 500 只会让人以为没删而反复点（同 removeTriggers）。
// 日志必须带 task id 与会话 id：那一格的并发闸仍被占住，而它已不在对账器视野内，只能按日志人工补收。
func (s *TaskService) convergeInFlightSessions(ctx context.Context, taskID uint) {
	sessions, err := s.sessionRepo.ListInFlightByTask(ctx, taskID)
	if err != nil {
		logger.Warnf("[BrowserTask] 任务 %d 删除后查在途会话失败: %v", taskID, err)
		return
	}
	for _, sess := range sessions {
		if s.executor != nil && s.executor.SignalStop(sess.ID) {
			logger.Infof("[BrowserTask] 任务 %d 已删除，会话 %d 执行中 → 已发中止信号（终态由执行协程收口）", taskID, sess.ID)
			continue
		}
		if err := s.sessionRepo.UpdateStatus(ctx, sess.ID, "stopped", sessionDeleteReason); err != nil {
			logger.Warnf("[BrowserTask] 任务 %d 已删除，会话 %d 收口失败（该用户并发闸仍被占住）: %v", taskID, sess.ID, err)
		}
	}
}

// Publish draft → ready
func (s *TaskService) Publish(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status != "draft" {
		return stateConflict("仅草稿可发布，当前状态: %s", t.Status)
	}
	if t.BrainMode && strings.TrimSpace(t.BrainGoal) == "" {
		return invalidInput("brain 模式必须填写 brain_goal")
	}
	if !t.BrainMode && len(t.Steps) == 0 {
		return invalidInput("显式模式至少编排一个步骤")
	}
	// 确认等待预算夹紧 1..900s。dto binding 只管 HTTP 入口，这一列还有 cron/重试/
	// 直接落库三条来路；Publish 是进入可执行态的唯一门，值在这里失守就等于把
	// 「不可逆提交前挂起多久」交给一个没人校验的整数（0 会退化成默认 600s，负数直接不等待）。
	if t.ConfirmWaitSec < 0 || t.ConfirmWaitSec > confirmWaitMaxSec {
		return invalidInput("confirm_wait_sec 必须在 1..%d 秒之间（0=默认 600），当前: %d", confirmWaitMaxSec, t.ConfirmWaitSec)
	}
	return s.taskRepo.UpdateStatus(ctx, t.ID, "ready", "")
}

func (s *TaskService) Pause(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status != "running" {
		return stateConflict("仅执行中任务可暂停，当前状态: %s", t.Status)
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
		return nil, stateConflict("仅暂停任务可恢复，当前状态: %s", t.Status)
	}
	return s.RunTask(ctx, t.ID, userID, t.RetryCount)
}

func (s *TaskService) Archive(ctx context.Context, id, userID uint) error {
	t, err := s.taskRepo.GetByID(ctx, id, userID)
	if err != nil {
		return err
	}
	if t.Status == "running" {
		return stateConflict("任务执行中，不能归档")
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
		if err := s.checkDependencyTarget(ctx, userID, t.ID, dependsOnTaskID); err != nil {
			return err
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

// checkDependencyTarget 前置任务必须存在且属于当前用户。
// depends_on_task_id 由请求方任意指定，而 Create 与 SetDependency 是这条不变式的两条入口——
// 此前只有 SetDependency 查归属，Create 把请求里的 id 直接写库，于是任何人都能让自己的任务
// 挂在别人的任务上：执行期 checkDependency 按 task_id（不带 user 过滤）读那条会话，
// 并把它的状态原样写进本用户的 409 文案，等于一台可枚举 id 的跨用户状态探针。
// 判定只有一份，两条入口共用——「两处各写一遍」正是这次的漂移来源。
func (s *TaskService) checkDependencyTarget(ctx context.Context, userID, taskID uint, dependsOn *uint) error {
	if dependsOn == nil {
		return nil
	}
	if *dependsOn == taskID {
		return invalidInput("任务不能依赖自身")
	}
	dep, err := s.taskRepo.GetByIDAnyUser(ctx, *dependsOn)
	if err != nil || dep.UserID != userID {
		// 两类结论共用一句文案：分开写就等于回「这条 id 存在，但不是你的」
		return invalidInput("前置任务不存在")
	}
	return nil
}

// checkDependencyCycle DFS 检测 A→B→…→A 环
func (s *TaskService) checkDependencyCycle(ctx context.Context, userID, taskID, dependsOn uint) error {
	const maxDepth = 64
	visited := map[uint]bool{taskID: true}
	current := dependsOn
	for depth := 0; depth < maxDepth; depth++ {
		if current == taskID {
			return invalidInput("检测到任务依赖环")
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
		return invalidInput("未知依赖模式: %s", t.DependsOnMode)
	}
}

// checkDependency 执行前置依赖校验
func (s *TaskService) checkDependency(ctx context.Context, t *model.BrowserTask) error {
	if t.DependsOnTaskID == nil {
		return nil
	}
	// 执行期兜底（fail-closed）：会话查询只按 task_id 过滤，归属必须在这一刀上自己确认——
	// Create 修好之前写进库的越权行不会因代码更新而消失，不挡就是把别人会话的终态
	// 透进本用户的错误文案（且别人的执行结果真的参与了我的调度判定）。
	// 文案中性：跨用户与前置已删都只回「不可用」，不给出任何可比对的读数。
	dep, err := s.taskRepo.GetByIDAnyUser(ctx, *t.DependsOnTaskID)
	if err != nil || dep.UserID != t.UserID {
		return fmt.Errorf("%w: 前置任务不可用", ErrDependencyNotMet)
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
		return nil, stateConflict("任务状态 %s 不可执行（需先 publish）", t.Status)
	}
	// 幂等：同任务已有运行中 session 则拒绝（并发触发防重，靠 DB 唯一性兜底竞态）
	runningByTask, err := s.sessionRepo.CountRunningByTask(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if runningByTask > 0 {
		return nil, ErrTaskRunning
	}
	// F8 并发闸（G18）：消费 MaxConcurrentJobs 语义——单 Host 连接上多 session 是
	// 逻辑级并发（帧串行≠会话串行），写互踩且风控节奏被打乱。闸值=每用户同时 1 个
	// running session（v1 单租户单 Host，取平台声明下限）；cron 触发命中此闸=本轮自然跳过（调用方仅记日志）。
	runningByUser, err := s.sessionRepo.CountRunningByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if runningByUser > 0 {
		return nil, ErrUserBusy
	}

	if err := s.checkDependency(ctx, t); err != nil {
		return nil, err
	}

	// 主 Profile 熔断门：同平台被封后新任务拒绝，需人工恢复（单 Profile 无他号可换，
	// 熔断行由 Executor.detectBlockedIfFatal 命中时经 recordProfileBlocked 写入）。
	if s.profileHealthRepo != nil {
		platform := taskPlatformID(t)
		if h, err := s.profileHealthRepo.GetByPlatform(ctx, platform); err == nil && h != nil && h.Blocked {
			return nil, stateConflict("主 Profile 在 %s 被拦截（%s），已熔断暂停，需人工恢复后重试", platform, h.Reason)
		}
	}

	// 解析 steps
	steps, err := ParseSteps(t.Steps)
	if err != nil {
		return nil, err
	}
	if !t.BrainMode && len(steps) == 0 {
		return nil, stateConflict("任务未编排步骤")
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
	// 普通 SafeGo 会让 Executor 在第一步就 ctx.Err() != nil 退出）；超时预算见 taskExecBudget
	// （TimeoutSec 只管自动化，D7 确认等待另计，外层看门狗同步放宽，否则「确认中」必被掐死）
	utils.SafeGoDetached(ctx, "browser_automation.run", taskExecBudget(t)+taskWatchdogGrace, func(runCtx context.Context) {
		execCtx, cancel := context.WithTimeout(runCtx, taskExecBudget(t))
		defer cancel()
		s.executor.ExecuteSession(execCtx, t, session, steps)
	})
	return session, nil
}

// RecoverProfile 人工恢复：清指定平台的主 Profile 熔断标记（行保留作审计）。
// 仓储未装配时拒绝，避免"恢复成功"的假象。
func (s *TaskService) RecoverProfile(ctx context.Context, platform string) error {
	if s.profileHealthRepo == nil {
		return errors.New("profile 健康仓储未装配，无法恢复")
	}
	return s.profileHealthRepo.MarkRecovered(ctx, platform)
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

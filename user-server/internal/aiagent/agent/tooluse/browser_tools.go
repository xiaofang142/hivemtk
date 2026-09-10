package tooluse

// browser_tools.go 浏览器自动化 MCP 工具集
// 设计文档: BROWSER_AUTOMATION_TECH_DESIGN.md 阶段3 #19 (外部 Agent 调用面)
// 工具: browser_open_task / browser_task_status / browser_task_list
// 执行注入: SetBrowserTaskRunner(TaskService.RunTaskWithRetry) 由 router 装配层注入,
// 避免 tooluse → browser_automation/service 反向依赖。

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// BrowserToolDeps browser 工具依赖
type BrowserToolDeps struct {
	DB *gorm.DB
}

// NewBrowserToolDeps 创建 browser 工具依赖
func NewBrowserToolDeps(gdb *gorm.DB) BrowserToolDeps {
	return BrowserToolDeps{DB: gdb}
}

// browserTaskRunner 任务执行注入点（进程级，router 装配时注入）
var browserTaskRunner func(ctx context.Context, taskID, userID uint, retryCount int) error

// SetBrowserTaskRunner 注入任务执行函数（TaskService.RunTaskWithRetry）
func SetBrowserTaskRunner(fn func(ctx context.Context, taskID, userID uint, retryCount int) error) {
	browserTaskRunner = fn
}

// ---------- 查询行结构（直接查表，避免 import browser_automation/model） ----------

type browserTaskRow struct {
	ID         uint   `gorm:"column:id"`
	Name       string `gorm:"column:name"`
	TaskType   string `gorm:"column:task_type"`
	Status     string `gorm:"column:status"`
	Platform   string `gorm:"column:platform"`
	UserID     uint   `gorm:"column:user_id"`
	LastResult string `gorm:"column:last_result"`
	LastRunAt  *time.Time `gorm:"column:last_run_at"`
}

type browserSessionRow struct {
	ID                 uint       `gorm:"column:id"`
	TaskID             uint       `gorm:"column:task_id"`
	Status             string     `gorm:"column:status"`
	ErrorMsg           string     `gorm:"column:error_msg"`
	StartedAt          *time.Time `gorm:"column:started_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	DurationMs         int64      `gorm:"column:duration_ms"`
	TotalSteps         int        `gorm:"column:total_steps"`
	SuccessSteps       int        `gorm:"column:success_steps"`
	FailedSteps        int        `gorm:"column:failed_steps"`
	LlmSummary         string     `gorm:"column:llm_summary"`
	FinalScreenshotURL string     `gorm:"column:final_screenshot_url"`
}

// ---------- browser_open_task ----------

// BrowserOpenTaskTool 触发一个浏览器自动化任务执行
type BrowserOpenTaskTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserOpenTaskTool 创建工具
func NewBrowserOpenTaskTool(deps BrowserToolDeps) *BrowserOpenTaskTool {
	return &BrowserOpenTaskTool{
		BaseTool: BaseTool{
			NameVal:        "browser_open_task",
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "触发一个浏览器自动化任务在用户 Chrome 浏览器中执行（寄生式，复用已登录会话）。返回任务信息并异步派发执行；执行进度用 browser_task_status 查询。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"task_id": {Type: "integer", Description: "浏览器任务 ID（browser_tasks.id）"},
					"user_id": {Type: "integer", Description: "可选，覆盖任务归属用户（默认用任务创建者）"},
				},
				Required: []string{"task_id"},
			},
		},
		deps: deps,
	}
}

// RiskLevel 触发执行 = 写操作
func (t *BrowserOpenTaskTool) RiskLevel() ToolRiskLevel { return RiskLevelWrite }

// Execute 触发任务执行
func (t *BrowserOpenTaskTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"task_id"}); err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	taskID, err := GetIntArg(args, "task_id")
	if err != nil {
		return ErrorResult(t.Name(), fmt.Errorf("task_id 参数无效: %w", err)), nil
	}

	var task browserTaskRow
	if err := t.deps.DB.WithContext(ctx).
		Select("id, name, task_type, status, platform, user_id, last_result, last_run_at").
		Table("browser_tasks").
		Where("id = ? AND deleted_at IS NULL", taskID).
		Take(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrorResult(t.Name(), fmt.Errorf("browser task %d 不存在", taskID)), nil
		}
		return ErrorResult(t.Name(), fmt.Errorf("查询任务失败: %w", err)), nil
	}
	if task.Status == "archived" {
		return ErrorResult(t.Name(), fmt.Errorf("browser task %d 已归档，不能执行", taskID)), nil
	}

	userID := task.UserID
	if v, err := GetIntArg(args, "user_id"); err == nil && v > 0 {
		userID = uint(v)
	}

	if browserTaskRunner == nil {
		return ErrorResult(t.Name(), fmt.Errorf("browser task runner 未配置（进程未装配浏览器自动化执行器）")), nil
	}

	// 异步派发：执行可能持续数分钟，不阻塞工具调用；结果经 browser_task_status 轮询
	go func(taskID, userID uint) {
		defer func() { _ = recover() }()
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = browserTaskRunner(runCtx, taskID, userID, 0) // 失败落库 task.last_result / browser_sessions
	}(task.ID, userID)

	return SuccessResult(t.Name(), map[string]any{
		"task_id":    task.ID,
		"name":       task.Name,
		"status":     task.Status,
		"platform":   task.Platform,
		"user_id":    userID,
		"dispatched": true,
		"hint":       "任务已异步派发，用 browser_task_status(task_id) 查询执行进度",
	}), nil
}

// ---------- browser_task_status ----------

// BrowserTaskStatusTool 查询任务状态与最近一次执行 session
type BrowserTaskStatusTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserTaskStatusTool 创建工具
func NewBrowserTaskStatusTool(deps BrowserToolDeps) *BrowserTaskStatusTool {
	return &BrowserTaskStatusTool{
		BaseTool: BaseTool{
			NameVal:        "browser_task_status",
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "查询浏览器自动化任务状态及其最近一次执行的会话（session）详情：执行状态、步骤成败数、耗时、错误信息、AI 总结。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"task_id": {Type: "integer", Description: "浏览器任务 ID"},
				},
				Required: []string{"task_id"},
			},
		},
		deps: deps,
	}
}

// Execute 查询任务状态
func (t *BrowserTaskStatusTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"task_id"}); err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	taskID, err := GetIntArg(args, "task_id")
	if err != nil {
		return ErrorResult(t.Name(), fmt.Errorf("task_id 参数无效: %w", err)), nil
	}

	var task browserTaskRow
	if err := t.deps.DB.WithContext(ctx).
		Select("id, name, task_type, status, platform, user_id, last_result, last_run_at").
		Table("browser_tasks").
		Where("id = ? AND deleted_at IS NULL", taskID).
		Take(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrorResult(t.Name(), fmt.Errorf("browser task %d 不存在", taskID)), nil
		}
		return ErrorResult(t.Name(), fmt.Errorf("查询任务失败: %w", err)), nil
	}

	var sessions []browserSessionRow
	if err := t.deps.DB.WithContext(ctx).
		Select("id, task_id, status, error_msg, started_at, completed_at, duration_ms, total_steps, success_steps, failed_steps, llm_summary, final_screenshot_url").
		Table("browser_sessions").
		Where("task_id = ? AND deleted_at IS NULL", taskID).
		Order("id DESC").Limit(3).Find(&sessions).Error; err != nil {
		return ErrorResult(t.Name(), fmt.Errorf("查询会话失败: %w", err)), nil
	}

	sessionList := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		sessionList = append(sessionList, map[string]any{
			"session_id":           s.ID,
			"status":               s.Status,
			"error_msg":            s.ErrorMsg,
			"started_at":           s.StartedAt,
			"completed_at":         s.CompletedAt,
			"duration_ms":          s.DurationMs,
			"total_steps":          s.TotalSteps,
			"success_steps":        s.SuccessSteps,
			"failed_steps":         s.FailedSteps,
			"llm_summary":          s.LlmSummary,
			"final_screenshot_url": s.FinalScreenshotURL,
		})
	}

	return SuccessResult(t.Name(), map[string]any{
		"task": map[string]any{
			"task_id":     task.ID,
			"name":        task.Name,
			"task_type":   task.TaskType,
			"status":      task.Status,
			"platform":    task.Platform,
			"user_id":     task.UserID,
			"last_result": task.LastResult,
			"last_run_at": task.LastRunAt,
		},
		"recent_sessions": sessionList,
	}), nil
}

// ---------- browser_task_list ----------

// BrowserTaskListTool 列出浏览器自动化任务
type BrowserTaskListTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserTaskListTool 创建工具
func NewBrowserTaskListTool(deps BrowserToolDeps) *BrowserTaskListTool {
	return &BrowserTaskListTool{
		BaseTool: BaseTool{
			NameVal:        "browser_task_list",
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "列出浏览器自动化任务（可按 user_id/status/platform 过滤），用于发现可执行任务后配合 browser_open_task 触发。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"user_id":  {Type: "integer", Description: "可选，按归属用户过滤"},
					"status":   {Type: "string", Description: "可选，按状态过滤（draft/ready/running/paused/done/failed/archived）"},
					"platform": {Type: "string", Description: "可选，按平台过滤（xiaohongshu/douyin/xianyu）"},
					"limit":    {Type: "integer", Description: "可选，返回数量上限（默认 20，最大 100）"},
				},
			},
		},
		deps: deps,
	}
}

// Execute 列出任务
func (t *BrowserTaskListTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	q := t.deps.DB.WithContext(ctx).
		Select("id, name, task_type, status, platform, user_id, last_result, last_run_at").
		Table("browser_tasks").
		Where("deleted_at IS NULL")

	if v, err := GetIntArg(args, "user_id"); err == nil && v > 0 {
		q = q.Where("user_id = ?", v)
	}
	if v, err := GetStringArg(args, "status"); err == nil && v != "" {
		q = q.Where("status = ?", v)
	}
	if v, err := GetStringArg(args, "platform"); err == nil && v != "" {
		q = q.Where("platform = ?", v)
	}
	limit := 20
	if v, err := GetIntArg(args, "limit"); err == nil && v > 0 {
		limit = v
	}
	if limit > 100 {
		limit = 100
	}

	var tasks []browserTaskRow
	if err := q.Order("id DESC").Limit(limit).Find(&tasks).Error; err != nil {
		return ErrorResult(t.Name(), fmt.Errorf("查询任务列表失败: %w", err)), nil
	}

	list := make([]map[string]any, 0, len(tasks))
	for _, tk := range tasks {
		list = append(list, map[string]any{
			"task_id":     tk.ID,
			"name":        tk.Name,
			"task_type":   tk.TaskType,
			"status":      tk.Status,
			"platform":    tk.Platform,
			"user_id":     tk.UserID,
			"last_result": tk.LastResult,
			"last_run_at": tk.LastRunAt,
		})
	}

	return SuccessResult(t.Name(), map[string]any{
		"list":  list,
		"total": len(list),
	}), nil
}

// ---------- 注册 ----------

// BuildBrowserTools 构建 browser 自动化工具集
func BuildBrowserTools(deps BrowserToolDeps) []Tool {
	return []Tool{
		NewBrowserOpenTaskTool(deps),
		NewBrowserTaskStatusTool(deps),
		NewBrowserTaskListTool(deps),
	}
}

// RegisterBrowserTools 注册 browser 工具到 registry
func RegisterBrowserTools(registry *ToolRegistry, deps BrowserToolDeps) error {
	for _, t := range BuildBrowserTools(deps) {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("register browser tool %s: %w", t.Name(), err)
		}
	}
	return nil
}

// MustRegisterBrowserTools panic 版注册
func MustRegisterBrowserTools(registry *ToolRegistry, deps BrowserToolDeps) {
	if err := RegisterBrowserTools(registry, deps); err != nil {
		panic(err)
	}
}

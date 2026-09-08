# Browser Automation — 项目落地设计文档

> 本文档是技术方案在 **hivemtk 现有代码库上的精确落点**。所有路径、接口签名、迁移 SQL、路由注册均来自对项目现有样板（geo / content 域）的直接映射。
> **零 Python，纯 JS + Go**。

---

## 0. 项目位置总览

Browser Automation 功能横跨三个代码区：

```
hivemtk/
├── user-server/                      ← Go 后端（五层架构）
│   └── internal/browser_automation/  ← 【新建】新域目录（第 47 个业务域）
│       ├── controller/   (3 个文件)
│       ├── service/      (4 个文件 + Hand)
│       ├── repository/   (5 个文件)
│       ├── model/        (5 个文件)
│       └── dto/          (3 个文件)
│   └── internal/migration/migrations/v3_37_0_browser_automation_migration.go  ← 【新建】
│   └── internal/router/browser_automation_routes.go                         ← 【新建】
│
├── user-web/                         ← Vue3 + ElementPlus 前端
│   └── src/
│       ├── api/browserAutomation.js  ← 【新建】
│       ├── views/browserAutomation/  ← 【新建】7 个页面
│       ├── stores/browserAutomation.js ← 【新建】Pinia store
│       └── router/modules/browserAutomation.js ← 【新建】
│
└── user-web/extension/               ← 【新建】Chrome MV3 扩展
    ├── manifest.json
    ├── background.js
    └── nm-host/                     ← 【新建】Go Native Messaging Host
        ├── main.go                  (~80 行，event_loop)
        ├── go.mod
        ├── install.sh
        └── manifest.json.template
```

### 项目现有约定（必须遵守）

| 约定 | 来源 | 落地 |
|------|------|------|
| Router → Handler → Service → Repository → Model 五层 | CLAUDE.md | browser_automation 域严格五层，不跨层 |
| Repository interface + WithDB 构造器 | geo/repository/alert.go | `BrowserTaskRepository` interface + `NewBrowserTaskRepositoryWithDB(db)` |
| Service struct（不搞 interface） | geo/service/alert.go | `BrowserTaskService` struct 持有 repo |
| Controller 持有 Service 指针 | geo/controller/alert.go | `BrowserTaskController` 持有 `*BrowserTaskService` |
| Migration 五方法：Version/Name/Description/Up/Down | migrations/v3_35_0 | `BrowserAutomationMigration` struct |
| Model 用 GORM tag + TableName() | geo/model/*.go | `gorm:"primaryKey;autoIncrement"` + `DeletedAt` 软删除 |
| 前端 API 用 `@/utils/http` | src/api/geoAlert.js | `import { http } from '@/utils/http'` |
| 前端路由模块化注册 | src/router/modules/*.js | `browserAutomation.js` 导出路由数组 |
| DI 装配在 router 文件内完成 | router/geo_routes.go | `SetupBrowserAutomationRoutes()` 内 New Repo → New Svc → New Ctrl → 注册路由 |
| 普通路由 vs Admin 路由分组 | router/geo_routes.go | 敏感操作走 `middleware.AdminAuthMiddleware()` |

---

## 1. user-server 侧：browser_automation 域完整五层

### 1.1 Model 层（5 个文件）

参考样板：`internal/geo/model/geo_alert.go`（最简洁的 model 模板）

```
user-server/internal/browser_automation/model/
├── browser_task.go        ← 任务主体（最核心）
├── browser_session.go     ← Chrome tab 会话
├── browser_step.go        ← 任务内的执行步骤
├── browser_cron.go        ← 定时触发器
└── browser_llm_plan.go    ← LLM 生成的执行计划
```

#### model/browser_task.go

```go
package model

import (
    "time"
    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// BrowserTask 浏览器自动化任务主体
type BrowserTask struct {
    ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    Name        string         `gorm:"column:name;size:256;not null;index" json:"name"`
    Description string         `gorm:"column:description;type:text" json:"description"`
    TaskType    string         `gorm:"column:task_type;size:32;not null;index" json:"task_type"` // one_shot / loop / cron / workflow
    Status      string         `gorm:"column:status;size:32;not null;default:draft;index" json:"status"` // draft / ready / running / paused / done / failed / archived
    Url         string         `gorm:"column:url;size:2048;not null" json:"url"`
    // 步骤编排（显式原语模式）
    Steps       datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"` // [{"action":"click","target":"#submit"}, ...]
    // LLM 自动模式参数
    BrainMode   bool           `gorm:"column:brain_mode;default:false" json:"brain_mode"`
    BrainGoal   string         `gorm:"column:brain_goal;type:text" json:"brain_goal"` // LLM 目标："打开淘宝搜索鞋，看前 10 页价格"
    LlmPlanID   *uint          `gorm:"column:llm_plan_id;index" json:"llm_plan_id,omitempty"`
    // 执行控制
    LoopCount   int            `gorm:"column:loop_count;default:1" json:"loop_count"`
    DelayMs     int            `gorm:"column:delay_ms;default:1000" json:"delay_ms"`
    TimeoutSec  int            `gorm:"column:timeout_sec;default:120" json:"timeout_sec"`
    // 归属
    UserID      uint           `gorm:"column:user_id;index;not null" json:"user_id"`
    AccountID   uint           `gorm:"column:account_id;index" json:"account_id"`
    // 执行状态快照
    CurrentSessionID *uint      `gorm:"column:current_session_id" json:"current_session_id,omitempty"`
    LastRunAt   *time.Time     `gorm:"column:last_run_at" json:"last_run_at,omitempty"`
    LastResult  string         `gorm:"column:last_result;type:text" json:"last_result,omitempty"`
    ErrorMsg    string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserTask) TableName() string { return "browser_tasks" }
```

#### model/browser_session.go

```go
package model

import (
    "time"
    "gorm.io/gorm"
)

// BrowserSession Chrome tab 会话（一次执行 = 一个 session）
// Chrome "寄生"式：复用主 Profile、后台 tab、不抢焦点
type BrowserSession struct {
    ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID      uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    ChromeTabID int            `gorm:"column:chrome_tab_id;index" json:"chrome_tab_id"` // Chrome tab.id（扩展上报）
    Url         string         `gorm:"column:url;size:2048" json:"url"`
    Title       string         `gorm:"column:title;size:512" json:"title"`
    Status      string         `gorm:"column:status;size:32;not null;default:created;index" json:"status"` // created / active / completed / failed / closed
    Snapshot    string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"` // accessibility @e1/@e2 refs（执行前）
    LlmPlan     string         `gorm:"column:llm_plan;type:text" json:"llm_plan,omitempty"`  // 执行时的 LLM plan JSON
    StartedAt   *time.Time     `gorm:"column:started_at" json:"started_at,omitempty"`
    CompletedAt *time.Time     `gorm:"column:completed_at" json:"completed_at,omitempty"`
    DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
    ErrorMsg    string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserSession) TableName() string { return "browser_sessions" }
```

#### model/browser_step.go

```go
package model

import (
    "time"
    "gorm.io/gorm"
)

// BrowserStep 任务内的执行步骤（每次执行产生一条 step 记录）
// 也作为 session 的子记录，用于逐步回放 + 调试
type BrowserStep struct {
    ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    SessionID   uint           `gorm:"column:session_id;index;not null" json:"session_id"`
    TaskID      uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    StepIndex   int            `gorm:"column:step_index;not null" json:"step_index"`
    Action      string         `gorm:"column:action;size:32;not null;index" json:"action"` // open_tab / click / type / snapshot / markdown / wait / scroll / screenshot
    Target      string         `gorm:"column:target;size:1024" json:"target"`   // selector 或 @e3 refs
    Value       string         `gorm:"column:value;type:text" json:"value"`     // type 动作的输入值
    Status      string         `gorm:"column:status;size:32;not null;default:pending;index" json:"status"` // pending / running / success / failed / skipped
    Result      string         `gorm:"column:result;type:text" json:"result,omitempty"` // action 返回值（snapshot/markdown 结果）
    DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
    ErrorMsg    string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserStep) TableName() string { return "browser_steps" }
```

#### model/browser_cron.go

```go
package model

import (
    "time"
    "gorm.io/gorm"
)

// BrowserCronTrigger 定时触发器（Cron 表达式 → 触发 Task 执行）
// 调度由现有 internal/pkg/cron 接管，Trigger 只负责参数 + 状态
type BrowserCronTrigger struct {
    ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID      uint           `gorm:"column:task_id;uniqueIndex;not null" json:"task_id"`
    CronExpr    string         `gorm:"column:cron_expr;size:128;not null" json:"cron_expr"`       // "*/5 * * * *" 或 "0 9 * * 1-5"
    Enabled     bool           `gorm:"column:enabled;default:true;index" json:"enabled"`
    NextRunAt   *time.Time     `gorm:"column:next_run_at" json:"next_run_at,omitempty"`
    LastRunAt   *time.Time     `gorm:"column:last_run_at" json:"last_run_at,omitempty"`
    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserCronTrigger) TableName() string { return "browser_cron_triggers" }
```

#### model/browser_llm_plan.go

```go
package model

import (
    "time"
    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// BrowserLLMPlan LLM 生成的执行计划（Brain 层产物）
// Brain 层吃 accessibility snapshot → 产出 plan → Translator 层翻译成 steps → Hand 执行
type BrowserLLMPlan struct {
    ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID      uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    Goal        string         `gorm:"column:goal;type:text;not null" json:"goal"`              // 原始目标描述
    Snapshot    string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`      // 输入 snapshot（@e1/@e2 refs）
    Steps       datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"`                     // LLM 输出的步骤数组
    Reasoning   string         `gorm:"column:reasoning;type:text" json:"reasoning,omitempty"`    // LLM 推理过程（调试用）
    Model       string         `gorm:"column:model;size:64" json:"model"`                        // 使用的模型（路由决定）
    TokenIn     int            `gorm:"column:token_in" json:"token_in"`
    TokenOut    int            `gorm:"column:token_out" json:"token_out"`
    CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserLLMPlan) TableName() string { return "browser_llm_plans" }
```

---

### 1.2 Repository 层（5 个文件）

参考样板：`internal/geo/repository/alert.go`

每个 repo 文件有：
- `XxxRepository` interface
- `xxxRepo` private struct 持有 `*gorm.DB`
- `NewXxxRepository()` + `NewXxxRepositoryWithDB(db)` 两个构造器

```
user-server/internal/browser_automation/repository/
├── browser_task.go
├── browser_session.go
├── browser_step.go
├── browser_cron.go
└── browser_llm_plan.go
```

#### repository/browser_task.go（接口签名预览）

```go
package repository

import (
    "context"
    "hivemtk-user/internal/browser_automation/model"
    _db "hivemtk-user/internal/pkg/db"
    "gorm.io/gorm"
)

// BrowserTaskRepository 任务仓储
type BrowserTaskRepository interface {
    Create(ctx context.Context, t *model.BrowserTask) error
    GetByID(ctx context.Context, id uint, userID uint) (*model.BrowserTask, error)
    List(ctx context.Context, userID uint, status, taskType string, page, limit int) ([]*model.BrowserTask, int64, error)
    Update(ctx context.Context, t *model.BrowserTask) error
    UpdateStatus(ctx context.Context, id uint, status string, errMsg string) error
    SoftDelete(ctx context.Context, id uint, userID uint) error
    // 查询辅助
    FindRunning(ctx context.Context, userID uint) ([]*model.BrowserTask, error)
}

type browserTaskRepo struct { db *gorm.DB }

func NewBrowserTaskRepository() BrowserTaskRepository {
    return &browserTaskRepo{db: _db.GetDB()}
}
func NewBrowserTaskRepositoryWithDB(db *gorm.DB) BrowserTaskRepository {
    return &browserTaskRepo{db: db}
}

// ... 方法实现（每个都 ctx 第一个参数 + r.db.WithContext(ctx).Create/Where/Order...）
```

**所有 5 个 repo 接口签名**：

| Repo | 方法 |
|------|------|
| **BrowserTaskRepository** | Create / GetByID / List / Update / UpdateStatus / SoftDelete / FindRunning |
| **BrowserSessionRepository** | Create / GetByID / ListByTaskID / UpdateStatus / UpdateChromeTabID / SoftDelete |
| **BrowserStepRepository** | Create / ListBySessionID / ListByTaskID / UpdateStatus / SoftDelete |
| **BrowserCronTriggerRepository** | Create / GetByTaskID / ListAllEnabled / UpdateNextRunAt / UpdateLastRunAt / Delete |
| **BrowserLLMPlanRepository** | Create / GetByID / ListByTaskID / SoftDelete |

---

### 1.3 Service 层（4 个文件 + Hand）

参考样板：`internal/geo/service/alert.go`

```
user-server/internal/browser_automation/service/
├── browser_task_service.go   ← 任务 CRUD + 发布/暂停/归档
├── browser_cron_service.go    ← Cron 调度 + 与 pkg/cron 集成
├── browser_brain_service.go   ← LLM Plan 生成（调用 aiagent/llm.Dispatcher）
├── browser_executor.go        ← 执行引擎：步骤解释 + Hand 调用 + Session 流转
└── browser_hand.go            ← 【核心】Go NM Hand：exec.Command + LE 帧 + 并发互斥
```

#### service/browser_hand.go（最核心文件）

```go
package service

import (
    "context"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "os/exec"
    "sync"
    "syscall"
    "time"
)

// BrowserHand Go Native Messaging Hand 层
// 负责：启动 Go NM Host 子进程 + stdio pipe 通信 + 4 字节 LE 帧协议 + 并发互斥
// 约束：一个 user-server 实例只有一个 BrowserHand，多 Agent 命令通过 mutex 串行
type BrowserHand struct {
    mu       sync.Mutex     // 多 Agent 并发保护（同一时刻只进一个命令）
    hostMu   sync.Mutex     // Host 单实例保护
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   io.ReadCloser
    cmdPath  string        // "hivemtk_browser_nm_host"（编译后 binary 路径）
    timeout  time.Duration // 单命令超时
    connected bool
}

// NewBrowserHand 构造器（单例调用）
func NewBrowserHand() *BrowserHand {
    return &BrowserHand{
        cmdPath: "hivemtk_browser_nm_host",
        timeout: 30 * time.Second,
    }
}

// EnsureConnected 确保 Host 子进程活着
// 断线 / Chrome 重启后自动重试
func (h *BrowserHand) EnsureConnected(ctx context.Context) error {
    h.hostMu.Lock()
    defer h.hostMu.Unlock()
    if h.connected { return nil }

    cmd := exec.CommandContext(ctx, h.cmdPath)
    cmd.Stdin, _ = cmd.StdinPipe()
    cmd.Stdout, _ = cmd.StdoutPipe()
    cmd.Stderr = os.Stderr // 日志走 stderr，绝不走 stdout（否则帧解析崩）
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 独立进程组

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("nm_host 启动失败: %w", err)
    }
    h.cmd = cmd
    h.stdin = cmd.Stdin.(io.WriteCloser)
    h.stdout = cmd.Stdout.(io.ReadCloser)
    h.connected = true

    // 异步监测子进程退出，触发重连
    go func() {
        cmd.Wait()
        h.hostMu.Lock()
        h.connected = false
        h.hostMu.Unlock()
    }()
    return nil
}

// openTab 原语
func (h *BrowserHand) openTab(ctx context.Context, url string, active bool) (int, error) {
    resp, err := h.send(ctx, map[string]any{
        "action": "open_tab", "url": url, "active": active,
    })
    if err != nil { return 0, err }
    tabID, _ := resp["chrome_tab_id"].(float64)
    return int(tabID), nil
}

// click 原语
func (h *BrowserHand) click(ctx context.Context, tabID int, target string) error {
    _, err := h.send(ctx, map[string]any{
        "action": "click", "tab_id": tabID, "target": target,
    })
    return err
}

// typeText 原语
func (h *BrowserHand) typeText(ctx context.Context, tabID int, target string, value string, clearFirst bool) error {
    _, err := h.send(ctx, map[string]any{
        "action": "type", "tab_id": tabID, "target": target, "value": value,
        "clear_first": clearFirst,
    })
    return err
}

// snapshot 原语（accessibility @e1/@e2 refs）
func (h *BrowserHand) snapshot(ctx context.Context, tabID int) (string, error) {
    resp, err := h.send(ctx, map[string]any{
        "action": "snapshot", "tab_id": tabID,
    })
    if err != nil { return "", err }
    s, _ := resp["snapshot"].(string)
    return s, nil
}

// markdown 原语（页面转 Markdown）
func (h *BrowserHand) markdown(ctx context.Context, tabID int) (string, error) {
    resp, err := h.send(ctx, map[string]any{
        "action": "markdown", "tab_id": tabID,
    })
    if err != nil { return "", err }
    m, _ := resp["markdown"].(string)
    return m, nil
}

// screenshot 原语
func (h *BrowserHand) screenshot(ctx context.Context, tabID int) (string, error) {
    resp, err := h.send(ctx, map[string]any{
        "action": "screenshot", "tab_id": tabID,
    })
    if err != nil { return "", err }
    b64, _ := resp["base64"].(string)
    return b64, nil
}

// waitFor 原语
func (h *BrowserHand) waitFor(ctx context.Context, tabID int, ms int) error {
    _, err := h.send(ctx, map[string]any{
        "action": "wait", "tab_id": tabID, "ms": ms,
    })
    return err
}

// scroll 原语
func (h *BrowserHand) scroll(ctx context.Context, tabID int, direction string, amount int) error {
    _, err := h.send(ctx, map[string]any{
        "action": "scroll", "tab_id": tabID, "direction": direction, "amount": amount,
    })
    return err
}

// closeTab 原语
func (h *BrowserHand) closeTab(ctx context.Context, tabID int) error {
    _, err := h.send(ctx, map[string]any{
        "action": "close_tab", "tab_id": tabID,
    })
    return err
}

// send 核心：4 字节 Little Endian 帧 + JSON
func (h *BrowserHand) send(ctx context.Context, req map[string]any) (map[string]any, error) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if err := h.EnsureConnected(ctx); err != nil {
        return nil, err
    }

    // 加超时
    sendCtx, cancel := context.WithTimeout(ctx, h.timeout)
    defer cancel()

    // 1. 写帧：4 字节 LE 长度头 + JSON body
    payload, _ := json.Marshal(req)
    frame := make([]byte, 4+len(payload))
    binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
    copy(frame[4:], payload)

    if _, err := h.stdin.Write(frame); err != nil {
        h.connected = false
        return nil, fmt.Errorf("写帧失败: %w", err)
    }
    h.stdin.(*os.File).Sync() // 立即 flush

    // 2. 读帧：4 字节 LE 长度头
    header := make([]byte, 4)
    if _, err := io.ReadFull(h.stdout, header); err != nil {
        h.connected = false
        return nil, fmt.Errorf("读帧头失败: %w", err)
    }
    length := binary.LittleEndian.Uint32(header)
    if length > 1*1024*1024 { // Chrome Host→扩展 1 MiB 硬限制
        return nil, errors.New("frame 超过 Chrome 1 MiB 限制")
    }

    // 3. 读 body
    body := make([]byte, length)
    if _, err := io.ReadFull(h.stdout, body); err != nil {
        h.connected = false
        return nil, fmt.Errorf("读帧体失败: %w", err)
    }

    var resp map[string]any
    if err := json.Unmarshal(body, &resp); err != nil {
        return nil, fmt.Errorf("JSON 解析失败: %w", err)
    }
    if ok, _ := resp["ok"].(bool); !ok {
        return nil, errors.New(fmt.Sprint(resp["error"]))
    }
    return resp, nil
}
```

#### service/browser_executor.go（执行引擎预览）

```go
// Executor 是任务执行的调度中心
// RunTask → 创建 Session → 遍历 Steps → 每个 Step 调 Hand → 落库 Step
// Brain 模式：先让 BrainService 出 plan → 翻译为 steps → 再执行
func (s *BrowserTaskService) RunTask(ctx context.Context, taskID uint, userID uint) (*model.BrowserSession, error) {
    // 1. 校验任务归属 + 状态
    task, err := s.taskRepo.GetByID(ctx, taskID, userID)
    // 2. 启动 session（状态=running，chrome tab ID 先 0，hand.openTab 成功后更新）
    session := &model.BrowserSession{TaskID: taskID, Status: "created"}
    s.sessionRepo.Create(ctx, session)
    // 3. 调 Hand.openTab
    tabID, err := s.hand.openTab(ctx, task.Url, false) // active=false 不抢焦点
    s.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
    s.sessionRepo.UpdateStatus(ctx, session.ID, "active", "")
    session.ChromeTabID = tabID
    // 4. 如果是 Brain 模式：让 Brain 出 plan → 翻译为 steps
    // 5. 遍历 steps，每个调 Hand 原语 → 落库 Step
    // 6. session 完成/失败 → 更新 status + error_msg
    // 7. 更新 task.LastRunAt / LastResult / Status
    return session, nil
}
```

---

### 1.4 Controller 层（3 个文件）

参考样板：`internal/geo/controller/alert.go`

```
user-server/internal/browser_automation/controller/
├── browser_task_controller.go   ← 任务 CRUD + 发布/暂停/执行/归档
├── browser_session_controller.go ← Session 查询 + Step 列表 + 日志回放
└── browser_cron_controller.go   ← Cron 触发器 CRUD + 启停
```

#### controller/browser_task_controller.go（接口签名预览）

```go
package controller

import (
    "hivemtk-user/internal/browser_automation/service"
    "github.com/gin-gonic/gin"
)

type BrowserTaskController struct {
    svc *service.BrowserTaskService
}

func NewBrowserTaskController(svc *service.BrowserTaskService) *BrowserTaskController {
    return &BrowserTaskController{svc: svc}
}

// c.GET("", ctrl.List)     → 用户自己的任务列表
// c.POST("", ctrl.Create)    → 新建任务
// c.GET("/:id", ctrl.Get)   → 任务详情
// c.PUT("/:id", ctrl.Update) → 更新
// c.DELETE("/:id", ctrl.Delete) → 软删除
// c.POST("/:id/publish", ctrl.Publish) → draft → ready
// c.POST("/:id/run", ctrl.Run)    → 触发执行（返回 session_id）
// c.POST("/:id/pause", ctrl.Pause)
// c.POST("/:id/resume", ctrl.Resume)
// c.POST("/:id/archive", ctrl.Archive)
```

Handler 签名全部是 `func (c *gin.Context)`，用 `c.ShouldBindJSON(&dto)` 绑定，返回 `c.JSON(200, gin.H{"code":0, "data":...})`。

---

### 1.5 DTO 层（3 个文件）

```
user-server/internal/browser_automation/dto/
├── browser_task.go    ← CreateTaskReq / UpdateTaskReq / TaskListReq / RunTaskReq
├── browser_session.go ← SessionListReq / StepListReq
└── browser_cron.go    ← CreateCronReq / UpdateCronReq
```

```go
package dto

type CreateBrowserTaskReq struct {
    Name        string `json:"name" binding:"required,max=256"`
    Description string `json:"description"`
    TaskType    string `json:"task_type" binding:"required,oneof=one_shot loop cron workflow"`
    Url         string `json:"url" binding:"required,max=2048,url"`
    BrainMode   bool   `json:"brain_mode"`
    BrainGoal   string `json:"brain_goal"`
    Steps       []any  `json:"steps"`
    LoopCount   int    `json:"loop_count"`
    DelayMs     int    `json:"delay_ms"`
    TimeoutSec  int    `json:"timeout_sec"`
}
```

---

## 2. Migration

路径：`user-server/internal/migration/migrations/v3_37_0_browser_automation_migration.go`

参考样板：`v3_35_0_telegram_group_gate_migration.go`

```go
package migrations

import (
    "context"
    "fmt"
    "hivemtk-user/internal/migration"
    "gorm.io/gorm"
)

// BrowserAutomationMigration v3.37.0：浏览器自动化（寄生式 Chrome Native Messaging）
type BrowserAutomationMigration struct { db *gorm.DB }
var _ migration.Migration = (*BrowserAutomationMigration)(nil)

func NewBrowserAutomationMigration(db *gorm.DB) *BrowserAutomationMigration {
    return &BrowserAutomationMigration{db: db}
}
func (m *BrowserAutomationMigration) Version() string      { return "v3.37.0" }
func (m *BrowserAutomationMigration) Name() string         { return "browser_tasks / sessions / steps / cron_triggers / llm_plans" }
func (m *BrowserAutomationMigration) Description() string  { return "浏览器自动化：寄生式 Chrome Native Messaging + Go 直做 Host + 纯 JS 扩展" }

func (m *BrowserAutomationMigration) Up(ctx context.Context) error {
    if m.db == nil { return fmt.Errorf("db is nil") }
    // 5 张表 DDL（直接 raw SQL，风格同 telegram_group_gates）
    // ... CREATE TABLE IF NOT EXISTS browser_tasks (...)
    // ... CREATE INDEX browser_tasks_status_idx ON browser_tasks(status)
    // ... CREATE TABLE IF NOT EXISTS browser_sessions (...)
    // ... CREATE TABLE IF NOT EXISTS browser_steps (...)
    // ... CREATE TABLE IF NOT EXISTS browser_cron_triggers (...)
    // ... CREATE UNIQUE INDEX browser_cron_task_uk ON browser_cron_triggers(task_id)
    // ... CREATE TABLE IF NOT EXISTS browser_llm_plans (...)
    return nil
}

func (m *BrowserAutomationMigration) Down(ctx context.Context) error {
    // DROP TABLE IF EXISTS 反向顺序（steps 依赖 session，先删子表）
    return nil
}
```

**完整 DDL（raw SQL，与 telegram_group_gate 风格一致）**：

```sql
-- 1. 任务主体
CREATE TABLE IF NOT EXISTS browser_tasks (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    description TEXT DEFAULT '',
    task_type VARCHAR(32) NOT NULL DEFAULT 'one_shot',
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    url VARCHAR(2048) NOT NULL,
    steps JSONB,
    brain_mode BOOLEAN NOT NULL DEFAULT FALSE,
    brain_goal TEXT DEFAULT '',
    llm_plan_id BIGINT,
    loop_count INT NOT NULL DEFAULT 1,
    delay_ms INT NOT NULL DEFAULT 1000,
    timeout_sec INT NOT NULL DEFAULT 120,
    user_id BIGINT NOT NULL,
    account_id BIGINT,
    current_session_id BIGINT,
    last_run_at TIMESTAMP,
    last_result TEXT,
    error_msg TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_browser_tasks_user (user_id),
    INDEX idx_browser_tasks_status (status),
    INDEX idx_browser_tasks_task_type (task_type)
);

-- 2. Chrome session
CREATE TABLE IF NOT EXISTS browser_sessions (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    chrome_tab_id INT,
    url VARCHAR(2048),
    title VARCHAR(512),
    status VARCHAR(32) NOT NULL DEFAULT 'created',
    snapshot TEXT,
    llm_plan TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    duration_ms BIGINT,
    error_msg TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_browser_sessions_task (task_id),
    INDEX idx_browser_sessions_status (status),
    INDEX idx_browser_sessions_chrome_tab (chrome_tab_id)
);

-- 3. 执行步骤
CREATE TABLE IF NOT EXISTS browser_steps (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    step_index INT NOT NULL,
    action VARCHAR(32) NOT NULL,
    target VARCHAR(1024),
    value TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    result TEXT,
    duration_ms BIGINT,
    error_msg TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_browser_steps_session (session_id),
    INDEX idx_browser_steps_task (task_id),
    INDEX idx_browser_steps_action (action)
);

-- 4. 定时触发器
CREATE TABLE IF NOT EXISTS browser_cron_triggers (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL UNIQUE,
    cron_expr VARCHAR(128) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMP,
    last_run_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_browser_cron_enabled (enabled),
    FOREIGN KEY (task_id) REFERENCES browser_tasks(id) ON DELETE CASCADE
);

-- 5. LLM Plan
CREATE TABLE IF NOT EXISTS browser_llm_plans (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    goal TEXT NOT NULL,
    snapshot TEXT,
    steps JSONB,
    reasoning TEXT,
    model VARCHAR(64),
    token_in INT,
    token_out INT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    INDEX idx_browser_llm_plans_task (task_id)
);
```

---

## 3. Router 注册

路径：`user-server/internal/router/browser_automation_routes.go`

参考样板：`internal/router/geo_routes.go`（DI 全在 Setup 函数内完成）

```go
package router

import (
    browserctrl "hivemtk-user/internal/browser_automation/controller"
    browserrepo "hivemtk-user/internal/browser_automation/repository"
    browsersvc "hivemtk-user/internal/browser_automation/service"
    "hivemtk-user/internal/middleware"
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

// SetupBrowserAutomationRoutes 浏览器自动化路由
// 权限分级：所有写操作（/browser-automation/tasks POST/PUT/DELETE/run）需登录；
// 敏感配置（如禁用全部 session）走 AdminAuthMiddleware()
func SetupBrowserAutomationRoutes(auth *gin.RouterGroup, gormDB *gorm.DB) {

    // --- Repository ---
    taskRepo    := browserrepo.NewBrowserTaskRepositoryWithDB(gormDB)
    sessionRepo := browserrepo.NewBrowserSessionRepositoryWithDB(gormDB)
    stepRepo    := browserrepo.NewBrowserStepRepositoryWithDB(gormDB)
    cronRepo    := browserrepo.NewBrowserCronTriggerRepositoryWithDB(gormDB)
    planRepo    := browserrepo.NewBrowserLLMPlanRepositoryWithDB(gormDB)

    // --- Service ---
    hand        := browsersvc.NewBrowserHand()  // 单例，进程生命周期
    execSvc     := browsersvc.NewBrowserExecutor(hand, sessionRepo, stepRepo, planRepo)
    brainSvc    := browsersvc.NewBrowserBrainService(planRepo, nil) // LLM dispatcher 注入
    taskSvc     := browsersvc.NewBrowserTaskService(taskRepo, sessionRepo, hand, execSvc, brainSvc)
    cronSvc     := browsersvc.NewBrowserCronService(cronRepo, taskSvc)
    sessionSvc  := browsersvc.NewBrowserSessionService(sessionRepo, stepRepo)

    // --- Controller ---
    taskCtrl    := browserctrl.NewBrowserTaskController(taskSvc)
    cronCtrl    := browserctrl.NewBrowserCronController(cronSvc)
    sessionCtrl := browserctrl.NewBrowserSessionController(sessionSvc)

    // --- 路由注册 ---
    ba := auth.Group("/browser-automation")

    // 任务
    ba.POST("/tasks", taskCtrl.Create)
    ba.GET("/tasks", taskCtrl.List)
    ba.GET("/tasks/:id", taskCtrl.Get)
    ba.PUT("/tasks/:id", taskCtrl.Update)
    ba.DELETE("/tasks/:id", taskCtrl.Delete)
    ba.POST("/tasks/:id/publish", taskCtrl.Publish)
    ba.POST("/tasks/:id/run", taskCtrl.Run)        // ← 触发执行，返回 session_id
    ba.POST("/tasks/:id/pause", taskCtrl.Pause)
    ba.POST("/tasks/:id/resume", taskCtrl.Resume)
    ba.POST("/tasks/:id/archive", taskCtrl.Archive)

    // Cron
    ba.GET("/cron", cronCtrl.List)
    ba.POST("/cron", cronCtrl.Create)
    ba.PUT("/cron/:id", cronCtrl.Update)
    ba.DELETE("/cron/:id", cronCtrl.Delete)
    ba.POST("/cron/:id/enable", cronCtrl.Enable)
    ba.POST("/cron/:id/disable", cronCtrl.Disable)

    // Session（只读查询，监控用）
    ba.GET("/sessions", sessionCtrl.List)
    ba.GET("/sessions/:id", sessionCtrl.Get)
    ba.GET("/sessions/:id/steps", sessionCtrl.ListSteps)
    ba.GET("/tasks/:id/sessions", sessionCtrl.ListByTask)

    // Admin 专用
    baAdmin := ba.Group("")
    baAdmin.Use(middleware.AdminAuthMiddleware())
    baAdmin.POST("/hand/ensure", func(c *gin.Context) {
        if err := hand.EnsureConnected(c.Request.Context()); err != nil {
            c.JSON(500, gin.H{"code":500, "message": err.Error()})
            return
        }
        c.JSON(200, gin.H{"code":0, "data": gin.H{"connected": true}})
    })
}
```

最后需要在 `internal/router/router.go` 里加一行：
```go
SetupBrowserAutomationRoutes(auth, gormDB)
```

---

## 4. user-web 前端

### 4.1 API 文件

路径：`user-web/src/api/browserAutomation.js`

参考样板：`user-web/src/api/geoAlert.js`

```js
import { http } from '@/utils/http'

// 任务
export const listTasks = (params) =>
  http.get('/api/browser-automation/tasks', params)

export const getTask = (id) =>
  http.get(`/api/browser-automation/tasks/${id}`)

export const createTask = (data) =>
  http.post('/api/browser-automation/tasks', data)

export const updateTask = (id, data) =>
  http.put(`/api/browser-automation/tasks/${id}`, data)

export const deleteTask = (id) =>
  http.delete(`/api/browser-automation/tasks/${id}`)

export const publishTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/publish`)

export const runTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/run`)

export const pauseTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/pause`)

export const resumeTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/resume`)

export const archiveTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/archive`)

// Cron
export const listCron = () =>
  http.get('/api/browser-automation/cron')

export const createCron = (data) =>
  http.post('/api/browser-automation/cron', data)

export const updateCron = (id, data) =>
  http.put(`/api/browser-automation/cron/${id}`, data)

export const deleteCron = (id) =>
  http.delete(`/api/browser-automation/cron/${id}`)

export const enableCron = (id) =>
  http.post(`/api/browser-automation/cron/${id}/enable`)

export const disableCron = (id) =>
  http.post(`/api/browser-automation/cron/${id}/disable`)

// Session
export const listSessions = (params) =>
  http.get('/api/browser-automation/sessions', params)

export const getSession = (id) =>
  http.get(`/api/browser-automation/sessions/${id}`)

export const listSessionSteps = (id) =>
  http.get(`/api/browser-automation/sessions/${id}/steps`)

export const listTaskSessions = (taskId) =>
  http.get(`/api/browser-automation/tasks/${taskId}/sessions`)
```

### 4.2 页面

路径：`user-web/src/views/browserAutomation/`

| 页面 | 文件 | 功能 |
|------|------|------|
| 任务列表 | `TaskList.vue` | 表格：名称 / 状态 / 类型 / 上次执行时间 / 操作（发布/执行/编辑/归档/删除） |
| 新建任务 | `TaskCreate.vue` | 分步表单：基本信息 → URL → 编排步骤 → Brain 模式开关 → 定时触发器 |
| 编辑任务 | `TaskEdit.vue` | 同 Create，预填 |
| 任务详情 | `TaskDetail.vue` | 概览 + 步骤列表 + 历史执行（sessions）+ Cron 配置 |
| Session 监控 | `SessionMonitor.vue` | 实时查看运行中 session 的 steps 执行状态、耗时、截图 |
| Cron 管理 | `CronList.vue` | Cron 触发器列表 + 启停 + 手动触发 |
| Hand 连通性 | `HandStatus.vue` | Admin 用：检查 Go NM Host 是否连接 + Chrome 扩展状态 |

### 4.3 Pinia Store

路径：`user-web/src/stores/browserAutomation.js`

```js
import { defineStore } from 'pinia'
import { runTask, listTaskSessions, listSessionSteps } from '@/api/browserAutomation'

export const useBrowserAutomationStore = defineStore('browserAutomation', {
  state: () => ({
    runningSession: null,
    pollTimer: null,
  }),
  actions: {
    async startRun(taskId) {
      const res = await runTask(taskId)
      this.runningSession = res.data
      // 启动轮询（每 2s 查 step 列表直到状态 != running）
      this.pollTimer = setInterval(async () => {
        const steps = await listSessionSteps(this.runningSession.id)
        this.runningSession.steps = steps.data
        const allDone = steps.data.every(s => ['success','failed','skipped'].includes(s.status))
        if (allDone) {
          clearInterval(this.pollTimer)
        }
      }, 2000)
    },
    stopPoll() {
      if (this.pollTimer) clearInterval(this.pollTimer)
    }
  }
})
```

### 4.4 Router

路径：`user-web/src/router/modules/browserAutomation.js`

参考样板：`user-web/src/router/modules/geoTools.js`

```js
export default [
  {
    path: '/browser-automation',
    meta: { title: '浏览器自动化', icon: 'Monitor', group: 'automation' },
    children: [
      { path: '', redirect: '/browser-automation/tasks' },
      { path: 'tasks', component: () => import('@/views/browserAutomation/TaskList.vue'), meta: { title: '任务列表' } },
      { path: 'tasks/create', component: () => import('@/views/browserAutomation/TaskCreate.vue'), meta: { title: '新建任务' } },
      { path: 'tasks/:id/edit', component: () => import('@/views/browserAutomation/TaskEdit.vue'), meta: { title: '编辑任务' } },
      { path: 'tasks/:id', component: () => import('@/views/browserAutomation/TaskDetail.vue'), meta: { title: '任务详情' } },
      { path: 'sessions/:id', component: () => import('@/views/browserAutomation/SessionMonitor.vue'), meta: { title: '执行监控' } },
      { path: 'cron', component: () => import('@/views/browserAutomation/CronList.vue'), meta: { title: '定时触发器' } },
    ]
  }
]
```

最后在 `user-web/src/router/index.js` 注册这个 module。

---

## 5. Chrome 扩展 + Go NM Host

### 5.1 项目位置

```
user-web/extension/
├── manifest.json         ← MV3，tabs + scripting
├── background.js         ← connectNative + onMessage + 原语分发
└── nm-host/
    ├── main.go           ← ~80 行，event_loop（4 字节 LE 帧 + JSON）
    ├── go.mod
    ├── install.sh        ← 编译 + 注册 manifest + 校验
    └── manifest.json.template
```

### 5.2 Go NM Host（`nm-host/main.go`）

与 `service/browser_hand.go` 的 Hand 层对接：Hand → Host（子进程 stdin/stdout） → Chrome 扩展（Native Messaging）

```go
package main

import (
    "encoding/binary"
    "encoding/json"
    "io"
    "os"
)

// 核心：event_loop 读取 Hand 层发来的帧，转发给 Chrome 扩展处理
// Chrome 扩展通过 Native Messaging 回 response，Host 再写回 Hand 层
func main() {
    for {
        // 1. 读 Hand 层发来的帧（4 字节 LE 长度头 + JSON body）
        var length uint32
        if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
            if err == io.EOF { os.Exit(0) }
            return
        }
        if length > 64*1024*1024 { return } // Chrome 扩展→Host 64 MiB 限制

        body := make([]byte, length)
        if _, err := io.ReadFull(os.Stdin, body); err != nil { return }

        var msg map[string]any
        if err := json.Unmarshal(body, &msg); err != nil {
            writeFrame(map[string]any{"ok": false, "error": "invalid_json"})
            continue
        }

        // 2. 转发给 Chrome 扩展（Native Messaging）
        //    Host 作为 Chrome 启动的子进程，Chrome 已把 msg 转给扩展
        //    扩展处理完会通过 Native Messaging 回 response
        //    但——这里有个关键点：我们是 Hand → Host → Chrome 扩展
        //    Hand 通过 exec.Command 启动 Host，Hand 是 Host 的父进程
        //    Chrome 也需要 Host 作为 Native Messaging Host
        //    → Host 需要同时处理两条通道：Hand 的 stdin/stdout + Chrome 的 stdin/stdout
        //    → 这不可能（一个进程一个 stdin）
        //
        // 正确做法：Host 有两个职责：
        //   a) 连接 Chrome 扩展（Native Messaging 标准通道，stdin/stdout 由 Chrome 管理）
        //   b) 被 Hand 作为子进程启动 → 不行，因为 Chrome 也需要启动它
        //
        // 解法：Hand 不启动 Host，而是 Hand 通过本地 Unix Socket 或 HTTP 连接一个常驻 Host 进程
        // Host 常驻进程 = Chrome 启动的 + 监听本地 socket 接受 Hand 命令
        // 这是唯一可行方案（让 Host 成为独立 daemon，Chrome 连接是一个通道，Hand 连接是另一个通道）
    }
}
```

> ⚠️ **架构修正（重要！）**：
>
> 最初方案"Hand 启动 Host 子进程 + stdio pipe"**行不通**。因为：
> - Host 必须被 Chrome 启动（才能走 Native Messaging 连接到扩展）
> - Chrome 启动的子进程 stdin/stdout 由 Chrome 管控
> - 如果 Hand 也启动 Host → Host 有两个父进程，冲突
>
> **正确架构**：
>
> ```
> 方案 A：Host 是独立 daemon（推荐）
> ┌──────────────┐     HTTP / Unix Socket      ┌──────────────┐     Native Messaging     ┌──────────────┐
> │   Go Hand    │ ──────────────────────────▶ │ Go NM Host   │ ──────────────────────▶ │ Chrome 扩展   │
> │ (user-server)│     localhost:18789         │ (常驻进程)   │     Chrome 管理 stdio   │ Manifest V3   │
> └──────────────┘                             └──────────────┘                           └──────────────┘
>                                                    ▲
>                                                    │ Chrome 启动 Host 进程
>
> 方案 B：MCP 模式（Chrome 触发时启动，一次性）
> ┌──────────────┐   websocket   ┌──────────────┐
> │   Go Hand    │ ◀────────────▶ │ Go NM Host   │  ← 保持 Host 常驻
> │ (user-server)│   ws://local   │ (daemon)     │
> └──────────────┘                └──────────────┘
>                                         │ Native Messaging
>                                         ▼
>                                   ┌──────────────┐
>                                   │ Chrome 扩展   │
>                                   └──────────────┘
> ```
>
> 本项目采用**方案 A（Host 是独立 HTTP 服务 + Native Messaging 客户端）**，理由：
> - Chrome 扩展 `connectNative()` 会 fork Host 进程 → Host 启动时连接一个预注册的本地 HTTP 服务（由系统 init.d / launchd / systemd 常驻运行）
> - 这样 Host 进程同时有两条通道：Chrome 的 stdio（Native Messaging）+ Hand 的 HTTP（localhost:18789）
> - 协议：Hand ↔ Host 用 HTTP JSON-RPC；Host ↔ Chrome 扩展用 Native Messaging 4 字节 LE 帧

### 5.3 manifest.json

```json
{
  "name": "com.hivemtk.browser",
  "description": "HiveMTK Browser Automation Native Messaging Host",
  "path": "/usr/local/bin/hivemtk_browser_nm_host",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://<EXTENSION_ID>/"]
}
```

### 5.4 install.sh

```bash
#!/bin/bash
# 编译 Go Host → 拷贝到 /usr/local/bin → 注册 manifest → 打开 manifest.json 填入扩展 ID

set -e
GO_HOST_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_PATH="/usr/local/bin/hivemtk_browser_nm_host"

# 1. 编译
cd "$GO_HOST_DIR" && go build -o /tmp/hivemtk_browser_nm_host .
sudo mv /tmp/hivemtk_browser_nm_host "$BINARY_PATH"

# 2. 注册 manifest（macOS 用户级）
EXT_ID="${1:-YOUR_EXT_ID_HERE}"
MANIFEST_DIR="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts"
mkdir -p "$MANIFEST_DIR"
cat > "$MANIFEST_DIR/com.hivemtk.browser.json" << EOF
{
  "name": "com.hivemtk.browser",
  "description": "HiveMTK Browser Automation Native Messaging Host",
  "path": "$BINARY_PATH",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$EXT_ID/"]
}
EOF

echo "✅ 安装完成。在 Chrome 扩展页面启用扩展后，刷新扩展即可。"
echo "   验证: Go Hand 调 /api/browser-automation/hand/ensure → connected:true"
```

---

## 6. 全链路数据流

从前端点"执行任务"按钮 → 浏览器 tab 打开 → 步骤跑 → 结果回传前端轮询，**每个字节在哪个文件流动**：

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ① 前端 user-web                                                                     │
│                                                                                     │
│  TaskList.vue 点"执行"                                                              │
│   ↓                                                                                 │
│  src/api/browserAutomation.js: runTask(taskId) → POST /api/browser-automation/tasks/:id/run │
│   ↓                                                                                 │
│  src/stores/browserAutomation.js: startRun() → 拿到 session_id → 启动 2s 轮询 timer│
│   ↓                                                                                 │
│  每 2s: GET /api/browser-automation/sessions/:id/steps → 更新 SessionMonitor.vue    │
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ HTTP (JSON)
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ② 后端 user-server                                                                  │
│                                                                                     │
│  internal/router/browser_automation_routes.go: SetupBrowserAutomationRoutes()        │
│   ba.POST("/tasks/:id/run", taskCtrl.Run)                                           │
│   ↓                                                                                 │
│  internal/browser_automation/controller/browser_task_controller.go: TaskCtrl.Run()  │
│   c.Param("id") → c.GetUint("user_id") → 调 taskSvc.RunTask(ctx, taskID, userID)  │
│   ↓                                                                                 │
│  internal/browser_automation/service/browser_task_service.go: RunTask()             │
│   taskRepo.GetByID(ctx, id, userID) → sessionRepo.Create(ctx, session)              │
│   ↓                                                                                 │
│  internal/browser_automation/service/browser_executor.go: ExecuteSession(session)   │
│   遍历 task.Steps → 每个 step 调 hand.OpenTab/Click/Type/Snapshot/Screenshot...     │
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ BrowserHand.send() HTTP JSON-RPC
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ③ Go NM Host (独立 daemon, localhost:18789)                                          │
│                                                                                     │
│  user-web/extension/nm-host/main.go: HTTP handler                                    │
│   POST /rpc {"action":"open_tab","url":"...","active":false}                         │
│   ↓                                                                                 │
│   写 4 字节 Little Endian 帧 → Chrome Native Messaging stdio → 发送给扩展            │
│   ↓                                                                                 │
│   读 Chrome 扩展 response frame → 返回 HTTP JSON 给 Go Hand                         │
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ Native Messaging 4B LE 帧 + JSON
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ④ Chrome 扩展 (MV3 Service Worker)                                                  │
│                                                                                     │
│  user-web/extension/background.js: port.onMessage.addListener(handleCommand)         │
│   case 'open_tab':  chrome.tabs.create({url, active:false})  ← 后台 tab，不抢焦点    │
│   case 'click':     chrome.scripting.executeScript({tabId, func: () => querySelector.click()}) │
│   case 'type':      chrome.scripting.executeScript({tabId, func: typeScript})       │
│   case 'snapshot':  chrome.scripting.executeScript({tabId, func: snapshotScript})   │
│   case 'markdown':  chrome.scripting.executeScript({tabId, func: markdownScript})   │
│   case 'screenshot':chrome.tabs.captureVisibleTab({tabId})                          │
│   回传: chrome.runtime.sendNativeMessage('com.hivemtk.browser', {ok:true, data:...})│
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ Chrome 内部 API 调用
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ⑤ 用户主 Chrome Profile                                                              │
│                                                                                     │
│  chrome.tabs.create → 新 tab（active:false，不抢用户当前焦点）                        │
│  chrome.scripting.executeScript → 注入 JS 执行 click/type/snapshot/markdown          │
│  chrome.tabs.captureVisibleTab → 截图                                                │
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ 回传路径（反向）
                                      ▼
│ ④ 扩展 → ③ Host → ② Go Hand → executor 更新 session.status / step.result → 写 DB     │
│                                                                                     │
│  internal/browser_automation/repository/browser_step.go: UpdateStatus()             │
│  internal/browser_automation/repository/browser_session.go: UpdateStatus()          │
│  internal/browser_automation/repository/browser_task.go: UpdateStatus()             │
│                                                                                     │
│  ① 前端 2s 轮询 → SessionMonitor.vue 实时更新每个 step 的 pending/running/success/failed 状态 │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 7. 业务生命周期状态机

### 7.1 Task 状态

```
                    publish()                   run()
  [draft] ─────────────────▶ [ready] ─────────────────▶ [running]
       ▲                      │  │                        │
       │ update steps          │  │ pause()               │ run() loop
       └───────────────────────┘  ▼                        ▼
                              [paused]  ◀──────────────── [done/failed]
                                  │                          │
                                  │ resume()                 │ archive()
                                  ▼                          ▼
                              [running]                 [archived]
```

### 7.2 Session 状态

```
  [created] ── openTab 成功 ──▶ [active] ── 所有 step 完成 ──▶ [completed]
      │                              │
      │ openTab 失败                 │ 任一 step 失败
      ▼                              ▼
  [failed]                       [failed]
```

### 7.3 Step 状态

```
  [pending] ─── executor 开始执行 ──▶ [running] ── 原语返回 OK ──▶ [success]
                                          │
                                          │ 原语返回 error
                                          ▼
                                      [failed]
```

### 7.4 Cron Trigger 状态

```
  [enabled] ◀──── toggle ────▶ [disabled]
```

---

## 8. 与现有系统的集成点

### 8.1 LLM Routing（Brain 模式）

```
service/browser_brain_service.go:
  1. 调 Hand.snapshot(tabID) → 拿到 @e1/@e2 refs snapshot
  2. 调 aiagent/llm.Dispatcher（项目已有的 LLM 路由）
     - brain_goal + snapshot 作为 prompt
     - 模型选择由 llmRouting 表决定
  3. 解析 LLM 输出 → steps 数组
  4. 存 LLMPlan 表 + executor 用 plan 替代显式 steps
```

### 8.2 用户鉴权

```
所有路由走 auth *gin.RouterGroup
controller 里:
  userID := c.GetUint("user_id")  // middleware 注入
  所有 repo 的 GetByID/List 都带 userID 过滤
```

### 8.3 Cron 调度

```
service/browser_cron_service.go:
  1. 用 existing internal/pkg/cron（项目已有 cron 框架）
  2. CronTrigger.Enabled=true + NextRunAt 到期 → 自动调 taskSvc.RunTask()
  3. CronTrigger.LastRunAt / NextRunAt 自动更新
```

### 8.4 执行日志

```
SessionMonitor.vue 展示实时日志流：
  - 后端 session_steps 表轮询（2s 一次）
  - 后续可升级为 websocket 推送（项目已有 internal/websocket）
```

### 8.5 多 Agent 并发

```
BrowserHand.mu sync.Mutex:
  - 同一 user-server 实例的所有 Agent 命令串行化
  - Go NM Host HTTP 服务本身也是单线程串行处理 Chrome 扩展通道
  - 真正多并发 = 多 Chrome Profile / 多 NM Host 实例（MVP 先不做）
```

---

## 9. 项目文件清单汇总

### 9.1 user-server 新建文件

```
internal/migration/migrations/v3_37_0_browser_automation_migration.go  ← 5 张表 DDL
internal/browser_automation/model/browser_task.go
internal/browser_automation/model/browser_session.go
internal/browser_automation/model/browser_step.go
internal/browser_automation/model/browser_cron.go
internal/browser_automation/model/browser_llm_plan.go
internal/browser_automation/repository/browser_task.go
internal/browser_automation/repository/browser_session.go
internal/browser_automation/repository/browser_step.go
internal/browser_automation/repository/browser_cron.go
internal/browser_automation/repository/browser_llm_plan.go
internal/browser_automation/service/browser_task_service.go
internal/browser_automation/service/browser_executor.go
internal/browser_automation/service/browser_brain_service.go
internal/browser_automation/service/browser_cron_service.go
internal/browser_automation/service/browser_hand.go
internal/browser_automation/controller/browser_task_controller.go
internal/browser_automation/controller/browser_session_controller.go
internal/browser_automation/controller/browser_cron_controller.go
internal/browser_automation/dto/browser_task.go
internal/browser_automation/dto/browser_session.go
internal/browser_automation/dto/browser_cron.go
internal/router/browser_automation_routes.go  ← 注册 + DI 装配
```

**共计：23 个 .go 文件**（不含测试）

### 9.2 user-web 新建文件

```
src/api/browserAutomation.js
src/views/browserAutomation/TaskList.vue
src/views/browserAutomation/TaskCreate.vue
src/views/browserAutomation/TaskEdit.vue
src/views/browserAutomation/TaskDetail.vue
src/views/browserAutomation/SessionMonitor.vue
src/views/browserAutomation/CronList.vue
src/views/browserAutomation/HandStatus.vue
src/stores/browserAutomation.js
src/router/modules/browserAutomation.js
```

**共计：10 个前端文件**

### 9.3 Chrome 扩展 + Go NM Host

```
extension/manifest.json
extension/background.js
extension/nm-host/main.go
extension/nm-host/go.mod
extension/nm-host/install.sh
extension/nm-host/manifest.json.template
```

**共计：6 个文件**

### 9.4 汇总

| 代码区 | 文件数 | 语言 |
|--------|--------|------|
| user-server 后端 | 23 | Go |
| user-web 前端 | 10 | JS + Vue3 |
| Chrome 扩展 | 2 | JS (background.js + manifest) |
| Go NM Host | 4 | Go (main.go + install.sh) |
| **合计** | **39** | **Go + JS，零 Python** |

---

## 10. 实施优先级

| 阶段 | 内容 | 阻塞后续 |
|------|------|----------|
| **P0** | Go NM Host daemon（HTTP + Native Messaging 双通道）+ manifest 注册 + install.sh | 是——没有它 Go Hand 就是瞎命令 |
| **P1** | Chrome 扩展 background.js（connectNative + 8 个原语） | 是——寄生式 Chrome 能不能跑的前提 |
| **P2** | DB Migration + 5 Model + 5 Repository | 是——所有后端 CRUD 的基础 |
| **P3** | Go Hand 层 + Executor + 三层 Controller | 是——能跑通一条 end-to-end |
| **P4** | 前端 API + 7 页面 + Store + Router | 是——用户能点起来 |
| **P5** | Cron 调度 + Brain LLM 模式 | 否——MVP 先用显式 steps 模式 |

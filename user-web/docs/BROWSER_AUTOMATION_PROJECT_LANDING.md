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
└── user-web/extension/               ← 【新建】Chrome MV3 扩展（纯 JS，不含 Go 源码）
    ├── manifest.json
    ├── background.js
    └── icons/

└── user-server/cmd/nm-host/          ← 【新建】Go Native Messaging Host（独立 binary）
    ├── main.go                       (~80 行，event_loop)
    ├── install.sh                    ← 编译 + 注册 manifest + 校验
    └── manifest.json.template

    【说明】Go NM Host 放 cmd/ 下，与 api/、geo-run/、seed/ 等独立 binary 并列，
    共享 user-server/go.mod。Chrome 扩展目录里只有 JS/CSS，职责单一。
```

### 项目现有约定（必须遵守）

| 约定 | 来源 | 落地 |
|------|------|------|
| Router → Handler → Service → Repository → Model 五层 | CLAUDE.md | browser_automation 域严格五层，不跨层 |
| Repository interface + WithDB 构造器 | geo/repository/alert.go | `BrowserTaskRepository` interface + `NewBrowserTaskRepositoryWithDB(db)` |
| Service struct（不搞 interface） | geo/service/alert.go | `TaskService` struct 持有 repo |
| Controller 持有 Service 指针 | geo/controller/alert.go | `TaskController` 持有 `*TaskService` |
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
├── task.go           ← 任务主体（最核心）
├── session.go        ← Chrome tab 会话
├── step.go           ← 任务内的执行步骤
├── cron.go           ← 定时触发器
└── llm_plan.go       ← LLM 生成的执行计划
```

#### model/task.go

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

#### model/session.go

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

#### model/step.go

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

#### model/cron.go

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

#### model/llm_plan.go

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
├── task.go
├── session.go
├── step.go
├── cron.go
└── llm_plan.go
```

#### repository/task.go（接口签名预览）

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
├── task.go   ← 任务 CRUD + 发布/暂停/归档
├── cron.go    ← Cron 调度 + 与 pkg/cron 集成
├── brain.go   ← LLM Plan 生成（调用 aiagent/llm.Dispatcher）
├── executor.go        ← 执行引擎：步骤解释 + Hand 调用 + Session 流转
└── hand.go            ← 【核心】统一端口内部接口调用 + WebSocket 等待 Host 回传
└── host_conn.go        ← 【配套】WebSocket 管理 Host 连接池（/internal/browser/controller 内）
```

#### service/hand.go（统一端口版本，不启动任何子进程）

**核心改变**：BrowserHand 不 `exec.Command` 任何进程（那是 Chrome 的职责）。
它只往 user-server 统一端口的内部接口发请求，内部接口通过 WebSocket 推给 Host，Host 处理完回传。

```go
package service

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"
)

// Hand Go Native Messaging Hand 层
// 约束 1: Chrome 才是 Go NM Host 的父进程，user-server 不启动 Host
// 约束 2: 统一端口 → 往 http://127.0.0.1:<统一端口>/internal/browser/host 发请求
// 约束 3: 多 Agent 命令同一时刻只进一个（Host 单线程串行处理扩展通道）
type Hand struct {
    mu          sync.Mutex     // 多 Agent 并发保护
    serverURL   string         // "http://127.0.0.1" + config.DefaultListenPort
    httpClient  *http.Client   // 带超时
}

// NewHand 构造器（单例）
func NewHand() *Hand {
    return &Hand{
        serverURL:  fmt.Sprintf("http://127.0.0.1:%d", config.DefaultListenPort),
        httpClient: &http.Client{Timeout: 30 * time.Second},
    }
}

// EnsureHostReady 检查 Host 是否在线（通过统一端口内部接口）
// /internal/browser/host/status 返回 {"connected": bool}
func (h *Hand) EnsureHostReady(ctx context.Context) error {
    req, _ := http.NewRequestWithContext(ctx, "GET", h.serverURL+"/internal/browser/host/status", nil)
    resp, err := h.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("Host 状态检查失败（可能 Chrome 没启动或扩展未加载）: %w", err)
    }
    defer resp.Body.Close()
    var body map[string]any
    json.NewDecoder(resp.Body).Decode(&body)
    if ok, _ := body["connected"].(bool); !ok {
        return fmt.Errorf("Go NM Host 未连接，请确认 Chrome 已启动 + 扩展已加载")
    }
    return nil
}

// openTab 原语
func (h *Hand) openTab(ctx context.Context, url string, active bool) (int, error) {
    resp, err := h.send(ctx, map[string]any{
        "action": "open_tab", "url": url, "active": active,
    })
    if err != nil { return 0, err }
    tabID, _ := resp["chrome_tab_id"].(float64)
    return int(tabID), nil
}

// click 原语
func (h *Hand) click(ctx context.Context, tabID int, target string) error {
    _, err := h.send(ctx, map[string]any{
        "action": "click", "tab_id": tabID, "target": target,
    })
    return err
}

// typeText 原语
func (h *Hand) typeText(ctx context.Context, tabID int, target string, value string, clearFirst bool) error {
    _, err := h.send(ctx, map[string]any{
        "action": "type", "tab_id": tabID, "target": target, "value": value,
        "clear_first": clearFirst,
    })
    return err
}

// snapshot 原语（accessibility @e1/@e2 refs）
func (h *Hand) snapshot(ctx context.Context, tabID int) (string, error) {
    resp, err := h.send(ctx, map[string]any{"action": "snapshot", "tab_id": tabID})
    if err != nil { return "", err }
    s, _ := resp["snapshot"].(string)
    return s, nil
}

// screenshot 原语
func (h *Hand) screenshot(ctx context.Context, tabID int) (string, error) {
    resp, err := h.send(ctx, map[string]any{"action": "screenshot", "tab_id": tabID})
    if err != nil { return "", err }
    b64, _ := resp["base64"].(string)
    return b64, nil
}

// send 核心：POST 到统一端口的 /internal/browser/host
// 内部 Controller 收到后 → 通过 WebSocket 推给 Go NM Host → Host 写帧到 Chrome 扩展
// 扩展处理完 → Host 回传 → Controller HTTP response 返回 → Hand 拿到结果
func (h *Hand) send(ctx context.Context, req map[string]any) (map[string]any, error) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if err := h.EnsureHostReady(ctx); err != nil {
        return nil, err
    }

    body, _ := json.Marshal(req)
    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        h.serverURL+"/internal/browser/host", bytes.NewReader(body))
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("X-Internal-Token", internalToken()) // 预共享密钥

    resp, err := h.httpClient.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("Host 通信失败: %w", err)
    }
    defer resp.Body.Close()

    var result map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("response 解析失败: %w", err)
    }
    if ok, _ := result["ok"].(bool); !ok {
        return nil, fmt.Errorf("%v", result["error"])
    }
    return result, nil
}
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

#### service/executor.go（执行引擎预览）

```go
// Executor 是任务执行的调度中心
// RunTask → 创建 Session → 遍历 Steps → 每个 Step 调 Hand → 落库 Step
// Brain 模式：先让 BrainService 出 plan → 翻译为 steps → 再执行
func (s *TaskService) RunTask(ctx context.Context, taskID uint, userID uint) (*model.BrowserSession, error) {
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
├── task.go   ← 任务 CRUD + 发布/暂停/执行/归档
├── session.go ← Session 查询 + Step 列表 + 日志回放
└── cron.go   ← Cron 触发器 CRUD + 启停
```

#### controller/task.go（接口签名预览）

```go
package controller

import (
    "hivemtk-user/internal/browser_automation/service"
    "github.com/gin-gonic/gin"
)

type TaskController struct {
    svc *service.TaskService
}

func NewTaskController(svc *service.TaskService) *TaskController {
    return &TaskController{svc: svc}
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
├── task.go    ← CreateTaskReq / UpdateTaskReq / TaskListReq / RunTaskReq
├── session.go ← SessionListReq / StepListReq
└── cron.go    ← CreateCronReq / UpdateCronReq
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
    hand        := browsersvc.NewHand()  // 单例，进程生命周期
    execSvc     := browsersvc.NewExecutor(hand, sessionRepo, stepRepo, planRepo)
    brainSvc    := browsersvc.NewBrainService(planRepo, nil) // LLM dispatcher 注入
    taskSvc     := browsersvc.NewTaskService(taskRepo, sessionRepo, hand, execSvc, brainSvc)
    cronSvc     := browsersvc.NewCronService(cronRepo, taskSvc)
    sessionSvc  := browsersvc.NewSessionService(sessionRepo, stepRepo)

    // --- Controller ---
    taskCtrl    := browserctrl.NewTaskController(taskSvc)
    cronCtrl    := browserctrl.NewCronController(cronSvc)
    sessionCtrl := browserctrl.NewSessionController(sessionSvc)

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
└── icons/                ← 扩展图标

user-server/cmd/nm-host/
├── main.go               ← Go NM Host daemon（HTTP + Native Messaging 双通道）
├── install.sh            ← 编译 binary + 注册 manifest.json 到 Chrome 路径
└── manifest.json.template ← Chrome Native Messaging 清单模板
```

### 5.1.1 物理执行模型（关键！严格遵守项目"统一端口"定位）

**本项目核心事实**：`user-server/cmd/api/main.go` 启动一个 gin.Engine、**一个端口**（比如 8080），
前端静态资源 `/assets/*`、后端 API `/api/*`、Vue SPA `/`、WebSocket 全挂在这一个端口上。
**不能开第二个 HTTP server**。

```
┌───────────────────────────────────────────────────────────────────────┐
│ 项目统一端口架构（只有一个 HTTP server：gin.Engine on :8080）           │
│                                                                       │
│  user-server (api binary)                                             │
│  ┌─────────────────────────────────────────────────────────┐          │
│  │  gin.Engine  (一个端口，比如 http://host:8080)           │          │
│  │  ├── /api/browser-automation/*  ← 业务 API（前端 Vue 调用）│          │
│  │  ├── /internal/browser/host     ← NM Host 内部回调接口    │          │
│  │  │   （localhost only，不对外暴露，无 Auth 但 IP 白名单） │          │
│  │  ├── /assets/*                  ← Vue3 静态资源          │          │
│  │  ├── /                          ← Vue3 SPA              │          │
│  │  └── websocket                  ← 统一端口的 WS          │          │
│  └─────────────────────────────────────────────────────────┘          │
│                                                                       │
│  Chrome Native Messaging（独立进程，Chrome 管控启动）                    │
│  ┌─────────────────────────────────────────────────────────┐          │
│  │  Go NM Host (cmd/nm-host/main.go)                        │          │
│  │  ├── stdin/stdout  ← Chrome 管控的 Native Messaging      │          │
│  │  │   Channel A: 4B LE 帧 + JSON ↔ Chrome 扩展            │          │
│  │  │                                                       │          │
│  │  └── HTTP client → http://127.0.0.1:8080/internal/browser/host │    │
│  │       Channel B: Host 作为**客户端**连 user-server 统一端口│          │
│  │       Host 不开任何 HTTP server！它只是 HTTP client        │          │
│  └─────────────────────────────────────────────────────────┘          │
│                                                                       │
│  Chrome 扩展 (user-web/extension/)                                     │
│  ┌─────────────────────────────────────────────────────────┐          │
│  │  background.js                                           │          │
│  │  └── chrome.runtime.connectNative('com.hivemtk.browser') │          │
│  │      → Chrome 自动 fork Go NM Host binary                │          │
│  │      → Host stdin/stdout 由 Chrome 管控                  │          │
│  └─────────────────────────────────────────────────────────┘          │
└───────────────────────────────────────────────────────────────────────┘
```

#### 谁启动 Go NM Host？—— **Chrome，不是 user-server**

Chrome Native Messaging 协议硬性要求：
1. Host 进程必须由 Chrome 启动（Chrome fork manifest.json 里 `path` 指定的 binary）
2. Chrome 管控 Host 的 stdin/stdout
3. user-server **不能** fork Host（否则 stdin/stdout 冲突）

#### Host 怎么跟 user-server 通信？—— **HTTP client 连统一端口**

Go NM Host 自己**不开 HTTP server**，它只是 HTTP client：

```
请求方向：user-server → Host → Chrome 扩展 → Host → user-server

① user-server BrowserHand.Executor.RunTask()
   → POST http://127.0.0.1:8080/internal/browser/host
          body: {"action":"click","tab_id":1,"target":"#submit"}
   ↑ 这是 user-server 调自己的统一端口内部接口！

② /internal/browser/host Controller 收到请求
   → 把 command 写入共享 channel（或通过 Host 注册的长连接）

③ Go NM Host 轮询（或 WebSocket 长连接）从 user-server 拿 command
   → 写 4B LE 帧到 Chrome 扩展（通过 stdin）

④ Chrome 扩展处理原语：chrome.scripting.executeScript(...)
   → 回写 4B LE 帧到 Host（stdout）

⑤ Go NM Host 读响应帧
   → HTTP response 返回给 user-server 的内部 Controller
   → → Executor 拿到 result → 继续下一步
```

#### 两条通道的精确协议

**Channel A: Go NM Host ↔ Chrome 扩展**（Chrome 管控的 Native Messaging，不可绕过）
  - 帧格式：4 字节 Little Endian 长度头 + UTF-8 JSON body
  - 限制：扩展→Host 64 MiB / Host→扩展 1 MiB

**Channel B: Go NM Host ↔ user-server 统一端口**（Host 是 HTTP client，user-server 是 server）
  - 方式 1（推荐）：**WebSocket 长连接**
    - Host 启动时：`ws://127.0.0.1:8080/internal/browser/ws`
    - user-server 内部 Controller 把 WebSocket 挂到统一 gin.Engine 上
    - Host 保持连接，user-server 通过 WS 推 command 过来
    - 好处：不用轮询，实时性好，Host 状态天然知道
  - 方式 2（备选）：Host 轮询 `GET http://127.0.0.1:8080/internal/browser/poll?last_id=N`
    - Host 每 100ms 轮询一次，user-server 返回积压的 commands
    - 简单但延迟稍高、浪费端口请求
  - 安全：IP 白名单 127.0.0.1 / ::1，不走反代

#### 为什么不开第二个端口？

因为 `cmd/api/main.go` 只有一个 `gin.New()` 监听一个端口，这是项目定位。
`/internal/browser/*` 内部路由直接**挂在同一个 gin.Engine 上**，和 `/api/*`、`/assets/*`、`/`、WebSocket 并列：

```go
// cmd/api/main.go 里 SetupRoutes() 会调用
// 内部路由和外部路由都挂在同一个 r *gin.Engine 上
router.SetupBrowserAutomationRoutes(auth, r, gormDB)
// SetupBrowserAutomationRoutes 内部:
//   r.Group("/internal/browser")  ← 内部路由挂同一端口
//     .GET("/ws", ...)             ← WebSocket 长连接
//     .GET("/poll", ...)           ← 轮询备选
//     .POST("/host-register", ...)
//   auth.Group("/api/browser-automation")  ← 业务 API 挂同一端口
//     .POST("/tasks/:id/run", ...)
```

完全没有第二个端口，统一端口贯穿一切。

与 `service/hand.go` 的 Hand 层对接：**统一端口 WebSocket**

```go
// user-server/cmd/nm-host/main.go — Go NM Host（Chrome 启动的子进程）
// 职责：
//   1. stdin/stdout 走 Chrome Native Messaging（4 字节 LE 帧）
//   2. 同时作为 HTTP client 连 user-server 统一端口 /internal/browser/ws（WebSocket）
//   3. 从 WS 拿 command → 写帧给 Chrome 扩展 → 等扩展响应 → WS 回传 user-server

package main

import (
    "context"
    "encoding/binary"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "os/signal"
    "strconv"
    "syscall"

    "github.com/gorilla/websocket"
)

// 通过环境变量知道统一端口（install.sh 写进 Chrome Native Messaging manifest 的 args）
// 或者 Host 启动时读 ~/.hivemtk/nm_host.conf
var (
    serverURL = envOr("HIVE_MTK_SERVER_URL", "http://127.0.0.1:8080")
    wsURL     = envOr("HIVE_MTK_WS_URL", "ws://127.0.0.1:8080/internal/browser/ws")
    token     = envOr("HIVE_MTK_INTERNAL_TOKEN", "")
)

func main() {
    // 1. 连 user-server 统一端口的 WebSocket
    hdr := http.Header{"Authorization": []string{"Bearer " + token}}
    wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
    if err != nil {
        log.Fatalf("连 user-server /internal/browser/ws 失败: %v", err)
    }
    defer wsConn.Close()
    log.Println("✅ Go NM Host 已连接 user-server 统一端口 WebSocket")

    // 2. 发注册消息
    wsConn.WriteJSON(map[string]any{
        "type":    "register",
        "version": "1.0.0",
        "pid":     os.Getpid(),
    })

    // 3. 主循环：从 WebSocket 拿 command → 写帧给 Chrome 扩展 → 等响应 → WS 回传
    for {
        // 3a. 从 WebSocket 读 command
        _, raw, err := wsConn.ReadMessage()
        if err != nil {
            log.Printf("WebSocket 断开，重连中...: %v", err)
            reconnect(wsURL, token)
            continue
        }

        var cmd map[string]any
        if err := json.Unmarshal(raw, &cmd); err != nil {
            wsConn.WriteJSON(map[string]any{"ok": false, "error": "invalid_cmd_json"})
            continue
        }

        // 3b. 写 4 字节 LE 帧 + JSON body 给 Chrome 扩展（通过 Chrome 管控的 stdin）
        if err := writeNativeFrame(cmd); err != nil {
            wsConn.WriteJSON(map[string]any{"ok": false, "error": fmt.Sprintf("chrome_write: %v", err)})
            continue
        }

        // 3c. 等 Chrome 扩展的响应帧（从 Chrome 管控的 stdout 读）
        resp, err := readNativeFrame()
        if err != nil {
            wsConn.WriteJSON(map[string]any{"ok": false, "error": fmt.Sprintf("chrome_read: %v", err)})
            continue
        }

        // 3d. 通过 WebSocket 回传给 user-server
        wsConn.WriteJSON(resp)
    }
}

// writeNativeFrame 写 4 字节 LE 长度头 + JSON body 到 Chrome 管控的 stdin
func writeNativeFrame(msg map[string]any) error {
    body, _ := json.Marshal(msg)
    header := make([]byte, 4)
    binary.LittleEndian.PutUint32(header, uint32(len(body)))
    if _, err := os.Stdout.Write(header); err != nil { return err }
    _, err := os.Stdout.Write(body)
    return err
}

// readNativeFrame 从 Chrome 管控的 stdout 读 4 字节 LE 长度头 + JSON body
func readNativeFrame() (map[string]any, error) {
    header := make([]byte, 4)
    if _, err := io.ReadFull(os.Stdin, header); err != nil {
        return nil, err
    }
    length := binary.LittleEndian.Uint32(header)
    body := make([]byte, length)
    if _, err := io.ReadFull(os.Stdin, body); err != nil {
        return nil, err
    }
    var resp map[string]any
    return resp, json.Unmarshal(body, &resp)
}
```

### 5.3 manifest.json（Chrome Native Messaging 清单）

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
│  internal/browser_automation/controller/task.go: TaskCtrl.Run()  │
│   c.Param("id") → c.GetUint("user_id") → 调 taskSvc.RunTask(ctx, taskID, userID)  │
│   ↓                                                                                 │
│  internal/browser_automation/service/task.go: RunTask()             │
│   taskRepo.GetByID(ctx, id, userID) → sessionRepo.Create(ctx, session)              │
│   ↓                                                                                 │
│  internal/browser_automation/service/executor.go: ExecuteSession(session)   │
│   遍历 task.Steps → 每个 step 调 hand.OpenTab/Click/Type/Snapshot/Screenshot...     │
└─────────────────────────────────────────────────────────────────────────────────────┘
                                      │ BrowserHand.send() HTTP JSON-RPC
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│ ③ Go NM Host (Chrome fork 的独立进程, HTTP client 连统一端口)                       │
│                                                                                     │
│  user-server/cmd/nm-host/main.go:                                                   │
│   启动时 WebSocket 连接 ws://127.0.0.1:8080/internal/browser/ws                      │
│   user-server 统一端口收到 command → 通过 WS 推给 Host                              │
│   Host 写 4 字节 LE 帧 → Chrome Native Messaging stdio → 扩展                       │
│   Host 读扩展 response frame → WS 推回 user-server                                 │
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
│  internal/browser_automation/repository/step.go: UpdateStatus()             │
│  internal/browser_automation/repository/session.go: UpdateStatus()          │
│  internal/browser_automation/repository/task.go: UpdateStatus()             │
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
service/brain.go:
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
service/cron.go:
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
internal/browser_automation/model/task.go
internal/browser_automation/model/session.go
internal/browser_automation/model/step.go
internal/browser_automation/model/cron.go
internal/browser_automation/model/llm_plan.go
internal/browser_automation/repository/task.go
internal/browser_automation/repository/session.go
internal/browser_automation/repository/step.go
internal/browser_automation/repository/cron.go
internal/browser_automation/repository/llm_plan.go
internal/browser_automation/service/task.go
internal/browser_automation/service/executor.go
internal/browser_automation/service/brain.go
internal/browser_automation/service/cron.go
internal/browser_automation/service/hand.go
internal/browser_automation/controller/task.go
internal/browser_automation/controller/session.go
internal/browser_automation/controller/cron.go
internal/browser_automation/dto/task.go
internal/browser_automation/dto/session.go
internal/browser_automation/dto/cron.go
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

### 9.3 Chrome 扩展

```
user-web/extension/manifest.json
user-web/extension/background.js
user-web/extension/icons/          ← 扩展图标（可选 3 张 PNG）
```

**共计：3 项（核心 2 个文件）**

### 9.4 Go NM Host（user-server/cmd/nm-host/）

```
cmd/nm-host/main.go                 ← Go NM Host daemon，HTTP + Native Messaging 双通道
cmd/nm-host/install.sh              ← 编译 + 注册 manifest.json 到 Chrome 路径
cmd/nm-host/manifest.json.template  ← Chrome Native Messaging 清单模板
```

**注意：没有独立 go.mod**，因为放在 `user-server/cmd/` 下，共享 `user-server/go.mod`（module `hivemtk-user`）。与 `api/`、`geo-run/`、`seed/` 等独立 binary 并列。

**共计：3 个文件**

### 9.5 汇总

| 代码区 | 文件数 | 语言 | 位置 |
|--------|--------|------|------|
| user-server browser_automation 域 | 25 | Go | user-server/internal/browser_automation/ |
| user-server Migration + Router | 2 | Go | user-server/internal/migration/migrations/ + router/ |
| user-server Go NM Host | 3 | Go + Shell | user-server/cmd/nm-host/ |
| user-web 前端 | 10 | JS + Vue3 | user-web/src/ |
| Chrome 扩展 | 2 | JS + JSON | user-web/extension/ |
| **合计** | **42** | **Go + JS + Shell，零 Python** | |

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

---

## 11. 编排阶段：原语全集 + 错误处理策略 + Workflow 嵌套

### 11.1 原语全集（11 种，精确到参数）

编排页面让用户选原语 → 填参数 → 顺序排列 → 生成 JSON steps 数组。

| # | 原语 | Go Hand 方法 | 关键参数 | 说明 |
|---|------|-------------|----------|------|
| 1 | `open_tab` | `hand.openTab(ctx, url, active)` | `url: string` `active: bool` | **active 必须 false**（寄生式不抢焦点） |
| 2 | `click` | `hand.click(ctx, tabID, target)` | `target: selector 或 @e3 refs` | 扩展执行 `querySelector(target).click()` |
| 3 | `type` | `hand.typeText(ctx, tabID, target, value, clearFirst)` | `target` `value: string` `clear_first: bool` `submit_on_enter: bool` | clear_first=true 时先 `.value=''` |
| 4 | `snapshot` | `hand.snapshot(ctx, tabID)` | 无 | 返回 accessibility @e1/@e2 refs 表 |
| 5 | `markdown` | `hand.markdown(ctx, tabID)` | 无 | 返回页面 Markdown（给 LLM 吃） |
| 6 | `screenshot` | `hand.screenshot(ctx, tabID)` | `format: png\|jpeg` `full_page: bool` | 返回 base64，存 session.screenshot_b64 |
| 7 | `wait` | `hand.waitFor(ctx, tabID, ms)` | `ms: int` | 等待固定毫秒，或条件等待 |
| 8 | `wait_for_selector` | `hand.waitForSelector(ctx, tabID, selector, timeoutMs)` | `selector` `timeout_ms: int` | 扩展侧轮询 DOM，出现则返回 |
| 9 | `scroll` | `hand.scroll(ctx, tabID, direction, amount)` | `direction: up\|down\|left\|right` `amount: px` | `window.scrollBy` |
| 10 | `extract` | `hand.extract(ctx, tabID, schema)` | `schema: json` | 扩展侧按 schema 从 DOM 提取结构化数据 |
| 11 | `close_tab` | `hand.closeTab(ctx, tabID)` | 无 | 执行完成后关闭后台 tab |

### 11.2 Step DTO（精确字段）

```go
// dto/browser_task.go 里的 StepItem（前端编排 → 后端落库）
type StepItem struct {
    Action       string `json:"action" binding:"required,oneof=open_tab click type snapshot markdown screenshot wait wait_for_selector scroll extract close_tab"`
    Target       string `json:"target"`                       // click/type/scroll/extract 用
    Value        string `json:"value"`                        // type 用
    Ms           int    `json:"ms"`                           // wait 用
    ClearFirst   bool   `json:"clear_first"`                  // type 用
    SubmitOnEnter bool  `json:"submit_on_enter"`              // type 用
    Direction    string `json:"direction"`                    // scroll 用
    Amount       int    `json:"amount"`                       // scroll 用
    Selector     string `json:"selector"`                     // wait_for_selector 用
    TimeoutMs    int    `json:"timeout_ms"`                   // wait_for_selector 用
    Format       string `json:"format"`                       // screenshot 用
    FullPage     bool   `json:"full_page"`                    // screenshot 用
    Schema       string `json:"schema"`                       // extract 用（JSON schema）
    // 错误处理策略（**关键！之前没设计**）
    ContinueOnError bool `json:"continue_on_error"`           // 默认 false；true 则此步失败后继续下一步
    RetryCount      int  `json:"retry_count"`                 // 默认 0；失败后重试次数
    RetryBackoffMs  int  `json:"retry_backoff_ms"`            // 默认 1000；重试间隔指数增长
}
```

### 11.3 错误处理层级

```
Step 执行失败 → retry_count > 0 ? 重试（backoff * 2 递增）→ run out of retry ?
  → continue_on_error ? 标记 step.status=failed → 继续下一步
  → !continue_on_error ? 整个 Session 标记 failed → session.error_msg = step.error_msg
```

### 11.4 Workflow 嵌套（Task 间依赖）

```
BrowserTask model 新增字段：
  DependsOnTaskID *uint `json:"depends_on_task_id"` // 前置任务 ID
  DependsOnMode   string `json:"depends_on_mode"`   // all_done / any_success / step_count_match

Executor 启动前检查：
  if task.DependsOnTaskID != nil {
    depTask := taskRepo.GetByID(ctx, *task.DependsOnTaskID, userID)
    lastSession := sessionRepo.GetLatestByTaskID(ctx, *task.DependsOnTaskID)
    switch task.DependsOnMode {
    case "all_done":
      if lastSession == nil || lastSession.Status != "completed" { return error("前置任务未完成") }
    case "any_success":
      if !sessionRepo.HasSuccess(ctx, *task.DependsOnTaskID) { return error("前置任务从未成功过") }
    }
  }
```

前端：编排页面底部有"依赖前置任务"开关 → 选一个已发布任务 + 触发条件。

---

## 12. 执行阶段：控制流 + 中断 + 超时清理

### 12.1 Session 手动中断

```go
// TaskController 新增 endpoint：
// POST /api/browser-automation/sessions/:id/stop
func (c *SessionController) Stop(c *gin.Context) {
    // 1. sessionRepo.GetByID → 校验归属
    // 2. 往 executor 的 stopCh 发信号
    executor.SignalStop(sessionID)
    // 3. sessionRepo.UpdateStatus(id, "stopped", "用户手动中断")
    // 4. 关闭 Chrome tab
    hand.closeTab(ctx, session.ChromeTabID)
}

// Executor 内部：每步执行前检查 stopCh
func (e *Executor) ExecuteSession(ctx context.Context, session *model.BrowserSession) error {
    for i, step := range session.Steps {
        select {
        case <-e.stopCh:
            return errors.New("executor stopped by user")
        default:
        }
        // ... 执行 step
    }
}
```

### 12.2 Chrome 断开自动清理

```go
// Session 启动时：executor 启动 goroutine 监测 chrome tab 是否还活着
go func() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        // 调扩展原生 Chrome API：chrome.tabs.get(tabID)
        tab, err := hand.tabExists(ctx, session.ChromeTabID)
        if err != nil || tab == nil {
            sessionRepo.UpdateStatus(ctx, session.ID, "failed", "Chrome tab 被关闭")
            return
        }
    }
}()
```

### 12.3 超时自动终止

```go
// Session 超时 = task.TimeoutSec（默认 120s）
sessionTimer := time.AfterFunc(time.Duration(task.TimeoutSec)*time.Second, func() {
    sessionRepo.UpdateStatus(ctx, session.ID, "failed", fmt.Sprintf("执行超时（%ds）", task.TimeoutSec))
    hand.closeTab(context.Background(), session.ChromeTabID)
    // 同时发通知（Feedback 阶段）
    feedbackSvc.NotifySessionTimeout(ctx, session)
})
defer sessionTimer.Stop()
```

### 12.4 Brain 模式动态调整

```go
// Brain 模式执行流程（比显式 steps 多一步 snapshot + 重新 plan）
// 显式 steps:  [step1 → step2 → step3 → done]
// Brain 模式:  [snapshot → plan1 → step1 → snapshot → plan2 → step2 → ... → goal_reached]

func (s *Executor) RunBrainMode(ctx context.Context, session *model.BrowserSession, goal string) error {
    maxIterations := 10 // 防无限循环
    for i := 0; i < maxIterations; i++ {
        // 1. snapshot 当前页面
        snap, err := hand.snapshot(ctx, session.ChromeTabID)
        if err != nil { return err }
        // 2. 让 LLM 出 plan
        plan, err := brainSvc.GeneratePlan(ctx, session.TaskID, goal, snap)
        if err != nil { return err }
        // 3. 执行 plan 里的 steps
        done, err := s.ExecuteSteps(ctx, session, plan.Steps)
        if done || err != nil {
            // 4. done=true = LLM 判断 goal 达成了
            break
        }
    }
    return nil
}
```

---

## 13. 监控阶段：实时截图流 + 性能指标 + 回放

### 13.1 每步自动截图

```go
// Executor 配置：每个 step 执行完后自动截图
// step.auto_screenshot = true（默认）
func (e *Executor) executeStep(ctx context.Context, session *model.BrowserSession, step *model.BrowserStep) error {
    // ... 执行 step ...
    // 执行完后截图
    if step.Action != "screenshot" {
        b64, err := e.hand.screenshot(ctx, session.ChromeTabID)
        if err == nil {
            // 存到 step.result 里（作为 step 的附属数据）
            step.Result = map[string]any{
                "action_result": stepResult,
                "after_screenshot": b64,
            }
            stepRepo.UpdateResult(ctx, step.ID, step.Result)
        }
    }
    return nil
}
```

前端 SessionMonitor.vue 显示每个 step 的执行前/后对比截图（hover 或点击展开）。

### 13.2 性能指标

Session 模型新增：
```go
type BrowserSession struct {
    // ... 原有字段 ...
    TotalSteps     int            `json:"total_steps"`
    SuccessSteps   int            `json:"success_steps"`
    FailedSteps    int            `json:"failed_steps"`
    P50StepMs      int64          `json:"p50_step_ms"`    // step 耗时中位数
    P95StepMs      int64          `json:"p95_step_ms"`    // step 耗时 95 分位
    HandLatencyMs  int64          `json:"hand_latency_ms"` // Hand ↔ Host HTTP 延迟
}
```

前端 SessionMonitor.vue 顶部显示 Dashboard：总耗时、成功率、P50/P95 step 耗时、Hand 延迟。

### 13.3 Session 回放

```
SessionDetail.vue → "回放" 按钮
  → 前端逐步高亮每个 step 的 status
  → 同时显示该 step 的 before/after 截图对比
  → 可以"重跑单个 step"（手动修正后重新执行）
```

### 13.4 Console 错误捕获

```
Chrome 扩展 background.js:
  chrome.scripting.executeScript({
    tabId,
    func: () => {
      const errors = [];
      const origError = console.error;
      console.error = (...args) => { errors.push(args.map(a => String(a)).join(' ')); origError(...args); };
      return errors;
    }
  })
→ 返回错误数组
→ 存 browser_session.console_errors 字段
→ 前端 SessionMonitor.vue 底部有红色 Warning 区域展示
```

---

## 14. 反馈阶段：通知 + 结果保存 + 自动重试 + LLM 总结 + 导出

### 14.1 执行完成后自动通知

```go
// 新增 FeedbackService（service/feedback.go）
type FeedbackService struct {
    // 复用项目已有的通知渠道（channelbot 里的飞书/钉钉/企微）
    larkClient   *lark.BotClient
    dingClient   *dingtalk.BotClient
    emailSvc     *email.Service
}

func (f *FeedbackService) NotifySessionComplete(ctx context.Context, session *model.BrowserSession) {
    // 1. 生成通知消息（状态 + 耗时 + 步骤数）
    // 2. 查用户设置的通知渠道（user 表或 config）
    // 3. 发送
    switch session.Status {
    case "completed":
        msg := fmt.Sprintf("✅ 浏览器任务 [%s] 执行完成，耗时 %ds，成功率 %d/%d",
            session.Task.Name, session.DurationMs/1000, session.SuccessSteps, session.TotalSteps)
    case "failed", "stopped":
        msg := fmt.Sprintf("❌ 浏览器任务 [%s] %s：%s",
            session.Task.Name, session.Status, session.ErrorMsg)
    }
}
```

前端 TaskDetail.vue 有"通知设置"tab：飞书机器人 webhook / 钉钉机器人 webhook / 邮件地址。

### 14.2 结果自动保存

```go
// Session 完成后：
// 1. 所有 step.extract 原语的提取结果 → 写入 browser_session.extracted_data (JSONB)
// 2. 所有 step.screenshot + step.after_screenshot → 打包存 uploads/ 目录（项目已有 uploads）
// 3. session 主截图 → 存 browser_session.final_screenshot_url
func (e *Executor) saveArtifacts(ctx context.Context, session *model.BrowserSession) {
    // 提取所有 step 里的 extract 结果
    extracts := collectExtractsFromSteps(session.Steps)
    session.ExtractedData = datatypes.JSON(extracts)
    
    // 打包截图
    tarPath, _ := uploadScreenshots(session.Steps)
    session.ScreenshotsArchiveURL = tarPath
    
    sessionRepo.Update(ctx, session)
}
```

### 14.3 失败自动重试（不是 step 级，是 session 级）

```
BrowserTask 新增字段：
  RetryOnFail  bool  `json:"retry_on_fail"`  // 默认 false
  RetryDelaySec int  `json:"retry_delay_sec"` // 默认 300（5 分钟后重试）
  MaxRetryTimes int  `json:"max_retry_times"` // 默认 3

Executor 逻辑：
  if session.Status == "failed" && task.RetryOnFail && task.RetryCount < task.MaxRetryTimes {
    // 延迟 retry_delay_sec 后再触发 RunTask
    cronSvc.ScheduleDelayedExecution(ctx, task.ID, task.RetryDelaySec)
    taskRepo.IncrementRetryCount(ctx, task.ID)
  }
```

### 14.4 LLM 自动总结

```go
// FeedbackService 里调用 aiagent/llm.Dispatcher
func (f *FeedbackService) SummarizeSession(ctx context.Context, session *model.BrowserSession) string {
    // prompt: "以下是浏览器自动化任务执行结果，请总结关键发现：...\n" +
    //         "Goal: " + task.BrainGoal + "\n" +
    //         "Extracts: " + session.ExtractedData + "\n" +
    //         "Console Errors: " + session.ConsoleErrors + "\n" +
    //         "Success Rate: " + successRate
    summary, tokenUsed, err := llmDispatcher.ChatCompletion(ctx, prompt)
    sessionRepo.SaveSummary(ctx, session.ID, summary)
    return summary
}
```

前端 SessionDetail.vue 顶部显示 LLM 总结卡片（可隐藏）。

### 14.5 结果导出

```
SessionDetail.vue → "导出" 按钮
  → 导出格式选择：
     - 结构化数据（JSON / CSV，来自 extract 原语结果）
     - 截图打包（.tar.gz）
     - 完整报告（Markdown：目标 + 步骤 + 截图 + LLM 总结）

后端：
  GET /api/browser-automation/sessions/:id/export?format=json|csv|md|archive
  生成文件 → 返回 uploads URL（项目已有 upload 基础设施）
```

---

## 15. 新增表字段汇总（之前 5 张表需要补齐）

### browser_tasks 新增

```sql
ALTER TABLE browser_tasks ADD COLUMN depends_on_task_id BIGINT;
ALTER TABLE browser_tasks ADD COLUMN depends_on_mode VARCHAR(32) DEFAULT 'all_done';
ALTER TABLE browser_tasks ADD COLUMN retry_on_fail BOOLEAN DEFAULT FALSE;
ALTER TABLE browser_tasks ADD COLUMN retry_delay_sec INT DEFAULT 300;
ALTER TABLE browser_tasks ADD COLUMN max_retry_times INT DEFAULT 3;
ALTER TABLE browser_tasks ADD COLUMN retry_count INT DEFAULT 0;
```

### browser_sessions 新增

```sql
ALTER TABLE browser_sessions ADD COLUMN total_steps INT DEFAULT 0;
ALTER TABLE browser_sessions ADD COLUMN success_steps INT DEFAULT 0;
ALTER TABLE browser_sessions ADD COLUMN failed_steps INT DEFAULT 0;
ALTER TABLE browser_sessions ADD COLUMN p50_step_ms BIGINT;
ALTER TABLE browser_sessions ADD COLUMN p95_step_ms BIGINT;
ALTER TABLE browser_sessions ADD COLUMN hand_latency_ms BIGINT;
ALTER TABLE browser_sessions ADD COLUMN console_errors TEXT;
ALTER TABLE browser_sessions ADD COLUMN extracted_data JSONB;
ALTER TABLE browser_sessions ADD COLUMN final_screenshot_url VARCHAR(1024);
ALTER TABLE browser_sessions ADD COLUMN screenshots_archive_url VARCHAR(1024);
ALTER TABLE browser_sessions ADD COLUMN llm_summary TEXT;
```

### browser_steps 新增

```sql
ALTER TABLE browser_steps ADD COLUMN after_screenshot TEXT;  -- base64
ALTER TABLE browser_steps ADD COLUMN extract_data JSONB;
```

---

## 16. 新增文件清单（Feedback 层 + 补齐）

```
internal/browser_automation/service/feedback.go   ← 【新增】通知 + LLM 总结 + 导出
internal/browser_automation/service/browser_feedback_test.go
internal/browser_automation/dto/feedback.go       ← 【新增】NotifyConfigReq / ExportReq
```

**更新后总文件数**：
- user-server：25 个 .go（+2 feedback）
- user-web：10 个前端文件
- Chrome 扩展：6 个
- **合计：41 个文件**

---

## 17. 完整业务生命周期状态图（编排→执行→监控→反馈 全链路）

```
┌───────────────────── 编 排 阶 段 ─────────────────────┐
│                                                          │
│  TaskList.vue ──[+ 新建任务]──▶ TaskCreate.vue           │
│     │                                                      │
│     │ 填 name / url / task_type                            │
│     │ 选模式: ○ 显式 steps   ● Brain 目标驱动              │
│     │ 编排: [open_tab → click → type → snapshot → close] │
│     │   每个 step: target / retry / continue_on_error     │
│     │ 可选: 依赖前置任务 / Cron 表达式                      │
│     │ 可选: 失败自动重试 + 通知渠道                          │
│     │                                                      │
│     ├──▶ 存 browser_tasks (status=draft)                   │
│     └──▶ publish → status=ready                            │
│                                                          │
└──────────────────────────────────────────────────────────┘
                         │
                         ▼
┌───────────────────── 执 行 阶 段 ─────────────────────┐
│                                                          │
│  TaskList.vue ──[▶ 执行]──▶ POST :id/run                  │
│     │                                                      │
│     │ ① TaskController.Run()                               │
│     │ ② Executor.ExecuteSession()                          │
│     │    ├── 创建 BrowserSession (status=created)          │
│     │    ├── Hand.openTab() → Chrome 扩展                 │
│     │    │   └── chrome.tabs.create({active:false})        │
│     │    ├── Session 每 5s 监测 tab 存活                   │
│     │    ├── Session 超时定时器（默认 120s）               │
│     │    ├── 遍历 steps:                                   │
│     │    │   ├── Step 超时                                  │
│     │    │   ├── Step retry_count 重试                     │
│     │    │   ├── Step continue_on_error 跳过失败继续       │
│     │    │   └── Step 执行完 auto_screenshot               │
│     │    ├── Brain 模式: snapshot → LLM plan → 循环        │
│     │    └── Session 完成/失败                              │
│     │ ③ 用户可手动中断: POST sessions/:id/stop             │
│     │                                                      │
│     ├──▶ 每个 step → browser_steps (status=success/failed) │
│     ├──▶ session → browser_sessions (status=completed)    │
│     ├──▶ task → browser_tasks (status=running→done)        │
│     └──▶ 失败 → retry_on_fail ? schedule retry : done     │
│                                                          │
└──────────────────────────────────────────────────────────┘
                         │
                         ▼
┌───────────────────── 监 控 阶 段 ─────────────────────┐
│                                                          │
│  SessionMonitor.vue                                      │
│     │                                                      │
│     │ 2s 轮询 sessions/:id/steps                         │
│     │    └── 每个 step 显示 pending→running→success/failed │
│     │                                                      │
│     │ Session 顶部 Dashboard:                              │
│     │   ├── 总耗时 / P50 step 耗时 / P95 step 耗时         │
│     │   ├── 成功率（success/total）                        │
│     │   ├── Hand ↔ Host HTTP 延迟                          │
│     │   └── Console 错误（红色 Warning 区）                │
│     │                                                      │
│     │ Step 列表:                                           │
│     │   ├── 点击展开 → before/after 截图对比               │
│     │   ├── 耗时 bar                                       │
│     │   └── 重跑单个 step 按钮                             │
│     │                                                      │
│     │ SessionDetail.vue                                   │
│     │   ├── 历史执行列表（sessions table）                 │
│     │   ├── 回放模式（逐步高亮）                            │
│     │   └── 依赖关系图（Workflow 嵌套可视化）                │
│                                                          │
└──────────────────────────────────────────────────────────┘
                         │
                         ▼
┌───────────────────── 反 馈 阶 段 ─────────────────────┐
│                                                          │
│  Session 完成触发 FeedbackService:                        │
│     │                                                      │
│     │ ① 自动通知（飞书/钉钉/邮件）                          │
│     │     └── "✅ 任务 X 完成，3步成功/1步失败，耗时 23s"   │
│     │                                                      │
│     │ ② 自动保存 artifacts                                 │
│     │     ├── extract 原语 → extracted_data (JSONB)        │
│     │     ├── 所有截图 → screenshots_archive_url            │
│     │     └── 最终截图 → final_screenshot_url              │
│     │                                                      │
│     │ ③ LLM 自动总结                                       │
│     │     └── BrainService.Summarize() → llm_summary       │
│     │         "本次执行发现...价格区间 ... 竞品 A 排在首位"   │
│     │                                                      │
│     │ ④ 失败自动重试（session 级）                         │
│     │     └── schedule delayed execution                   │
│     │                                                      │
│     │ ⑤ 结果导出                                           │
│     │     ├── 结构化数据 → CSV/JSON                         │
│     │     ├── 截图打包 → .tar.gz                            │
│     │     └── 完整报告 → Markdown                           │
│     │                                                      │
│     │ 前端展示:                                             │
│     │   ├── TaskDetail.vue: 执行历史 + 通知设置 tab        │
│     │   ├── SessionDetail.vue: LLM 总结卡片 + 导出按钮     │
│     │   └── 飞书/钉钉机器人: 通知卡片                       │
│                                                          │
└──────────────────────────────────────────────────────────┘

# Browser Automation — 项目落地设计文档 v2

> 本文档是技术方案在 **hivemtk 现有代码库上的精确落点**。所有路径、接口签名、迁移 SQL、路由注册均来自对项目现有样板（geo / channelgw / bridge 域）的直接映射，并经 2026-09-08 全量核查修正。
> **零 Python，纯 JS + Go。**

## 修订记录（v2 相对 v1 的关键变更）

| # | 变更 | 原因 |
|---|------|------|
| A1 | 删除 v1 `service/hand.go` 中残留的 stdin/stdout 旧版 `send()` 代码块 | v1 文档拼接残片，无法编译 |
| A2 | 通信模型定稿：**WS 单长连接**（Host 主动连 server），删除 `POST /internal/browser/host` 同步接口与 `/poll` 轮询备选 | 三处自相矛盾；channelgw 已有现成 WS 样板 |
| A3 | 帧大小限制修正：扩展→Host **4GB**，Host→扩展 **1MB**（1MB 检查加在**写方向**） | v1 写反方向、写错数字 |
| A4 | 帧长度头改用 `binary.NativeEndian`（官方措辞 native order；arm64/x86 上等价 LE） | 官方规范 |
| A5 | Host 重新定性：**非 daemon**，生命周期随扩展 port 生灭；补 `port.onDisconnect` 重连 + **server 端 Host 断连清理钩子**（running session 置 failed） | MV3 SW 生命周期硬约束 |
| A6 | 截图方案定稿 **M3**：MVP 仅 session 结束对 active tab 截一次 final_screenshot；删除"每步自动截图/前后对比/全页截图" | `captureVisibleTab` 无 tabId 参数、不能截后台 tab、不能全页、限频 2 次/秒 |
| A7 | 新增 §10 多租户与产品边界：Host 注册必须携带用户身份（Bearer token），命令**只路由到归属 Host**；无 Host 用户明确降级引导 | v1 完全没回答"命令推给哪个 Host"，存在越权串号 |
| B1 | DDL 去掉内联 INDEX 与 FOREIGN KEY（Postgres 不支持内联；项目样板不用 FK，配合软删除） | 照 v1 抄建表直接报错 |
| B2 | 合并 v1 §15 的 ALTER 增量列进 v3.37.0 建表，一次建全；Model 同步补齐 | v1 三处字段集互不一致 |
| B3 | 通知渠道换成真实客户端：`FeishuIntegrationService` / `pkg/mail.SendMail` / `EmailSendService`；钉钉走通用 webhook | v1 假设的 `lark.BotClient` 等不存在 |
| B4 | 内部通道鉴权复用 **BridgeIngressGuard 模式**（KV `bridge_ingest_token`，fail-closed），废弃 v1 发明的 `X-Internal-Token` | 项目已有预共享密钥机制 |
| B5 | Cron 表达式：存储/前端展示 5 段，**注册到 TaskManager 前补 `"0 "` 前缀转 6 段秒级** | 项目 `cron.WithSeconds()`，5 段 spec 解析报错 |
| B6–B11 | 前端 API 重名函数、Pinia setup 风格、`res?.data || res` 拆包、路由 moduleNames 白名单、Layout.vue 手写菜单、Dispatcher 真实调用形态、LocalDriver 存产物、WS 挂载对齐 bridgeWS 样板 | 与现有代码不符 |
| P2 | Hand 全局锁改 **per-Host 串行 + req_id 异步关联**；`/run` 改异步返回 session_id；stopCh 注册表；依赖环检测；URL scheme 白名单；unpacked 扩展 manifest 写死 `key` 固定 ID | v1 并发模型会互相堵死/HTTP 必超时/refs 越权风险 |

---

## 0. 项目位置总览

```
hivemtk/
├── user-server/                      ← Go 后端（五层架构）
│   ├── internal/browser_automation/  ← 【新建】新业务域
│   │   ├── controller/   (4 个文件)
│   │   ├── service/      (8 个文件)
│   │   ├── repository/   (5 个文件)
│   │   ├── model/        (5 个文件)
│   │   └── dto/          (3 个文件)
│   ├── internal/migration/migrations/v3_37_0_browser_automation_migration.go  ← 【新建】
│   ├── internal/router/browser_automation_routes.go                           ← 【新建】
│   └── cmd/nm-host/                  ← 【新建】Go Native Messaging Host（独立 binary，共享 go.mod）
│       ├── main.go                   (~200 行：stdin/stdout 帧协议 + WS client)
│       ├── install.sh                ← 编译 + 注册 manifest + 固定扩展 ID 说明
│       └── manifest.json.template
│
└── user-web/                         ← Vue3 + ElementPlus 前端
    ├── src/api/browserAutomation.js      ← 【新建】
    ├── src/views/browserAutomation/      ← 【新建】6 个页面
    ├── src/stores/browserAutomation.js   ← 【新建】Pinia setup 风格
    ├── src/router/modules/browserAutomation.js ← 【新建】+ index.js moduleNames 注册
    └── browser_automation/           ← 【新建】Chrome MV3 扩展独立子项目（对齐 user-web/bridge/）
        ├── package.json              ← name: hivemtk-browser-automation, esbuild + vitest
        ├── manifest.json             ← MV3（项目根，build.mjs 复制到 dist/，写死 key 固定 ID）
        ├── scripts/build.mjs
        ├── src/
        │   ├── background/index.js   ← Service Worker：connectNative + 原语分发 + onDisconnect 重连
        │   ├── popup/index.js + popup.html
        │   └── core/
        │       ├── native-messaging.js
        │       ├── primitives.js     ← 9 种原语
        │       ├── tab-manager.js
        │       └── accessibility.js  ← snapshot 生成 @e{N} refs
        ├── test/                     ← vitest
        └── assets/icons/
```

### 项目现有约定（必须遵守，全部已核实）

| 约定 | 来源（已核实） | 落地 |
|------|------|------|
| 五层架构 Router→Handler→Service→Repository→Model | CLAUDE.md | browser_automation 域严格五层 |
| Repository interface + 双构造器 | `internal/geo/repository/alert.go` | `BrowserTaskRepository` + `New...WithDB(db)` |
| Service struct 不搞 interface | `internal/geo/service/alert.go` L14 | `TaskService` struct 持有 repo |
| Controller 持有 *Service 私有字段 | `internal/geo/controller/alert.go` L16 | 同款 |
| 响应 `response.Success/Error/SuccessWithList`（code 为 int 0） | `internal/pkg/utils/response/response.go` L48/85 | 禁手写 c.JSON |
| Migration 五方法 + `initial_schema.go` RegisterMigrations 注册 | `internal/migration/registry.go` L13、`migrations/initial_schema.go` L194 | v3.37.0 追加一行 |
| DDL raw SQL 分条 Exec、索引单独 CREATE INDEX、无 FK | `migrations/v3_35_0_telegram_group_gate_migration.go` | 同款 |
| 路由 Setup 函数签名 `func SetupXxxRoutes(auth *gin.RouterGroup, gormDB *gorm.DB)` | `internal/router/geo_routes.go` L24 | 同款，挂载点 `router.go` L321 附近 |
| 用户身份 `c.GetUint("user_id")`（JWTAuthMiddleware 注入，uint） | `internal/middleware/jwt.go` L71 | 所有 repo 按 userID 过滤 |
| Admin 路由 `middleware.AdminAuthMiddleware()` | `internal/middleware/jwt.go` L93 | Hand 状态/内部配置走 admin |
| Cron：`internal/pkg/cron` TaskManager，**6 段秒级 spec**，`AddTask(spec, fn)` | `internal/pkg/cron/cron.go` L30/49 | geo 已有 `SetupGeoJobs(mgr)` 先例 |
| LLM：`llm.NewDispatcher(llm.NewLLMService()).Dispatch(ctx, DispatchRequest{...})` | `internal/aiagent/llm/dispatcher.go` L76、`dispatcher_dispatch.go` L19/279 | brain 模式 + 总结 |
| 内部预共享密钥：KV `bridge_ingest_token`（fail-closed） | `internal/middleware/bridge_ingress_guard.go` | 复用该模式做 Host 通道 token |
| WS 挂统一端口：`bridgeWS := r.Group("/api"); bridgeWS.GET("/ws/channel", transport.HandleWS)` | `internal/router/router.go` L441-458、`internal/channelgw/ws.go` L101 | Host WS 同款挂法 |
| gorilla/websocket、gorm.io/datatypes v1.2.7 已在 go.mod | go.mod | 直接用 |
| 上传产物：`storage.NewLocalDriver(baseDir, publicBaseURL)` + `UploadReader` | `internal/storage/local.go` L26/64 | 截图/导出存 URL |
| 前端 http：`import { http } from '@/utils/http'`；拦截器**已拆到 data.data** | `src/api/geoAlert.js`、`src/utils/request.js` | 页面取数 `res?.data || res` |
| Pinia 全部 setup 风格 | `src/stores/user.js` | 同款 |
| 路由模块 export default 数组 + **index.js moduleNames 白名单** + **Layout.vue 手写菜单** | `src/router/index.js`、`src/layout/Layout.vue` | 三处都要注册 |
| 页面文案硬编码中文（项目惯例非 $t） | `src/views/geo/*` | 同款 |
| 扩展子项目样板：bridge/（esbuild IIFE 多入口 + vitest + 根 manifest） | `user-web/bridge/` | browser_automation 子项目对齐 |

---

## 1. 通信模型（定稿）

### 1.1 物理拓扑（统一端口，唯一 HTTP server :8204）

```
┌───────────────────────────────────────────────────────────────────────┐
│ user-server (api binary, gin.Engine on :8204 —— config.DefaultListenPort)│
│  /api/browser-automation/*   业务 API（JWT）                             │
│  /api/browser/host-ws        Host WebSocket 入口（BridgeGuard 模式 token）│
│  /api/browser-automation/*   其余业务路由……                              │
│  /files/*                    静态产物                                    │
└──────────────▲────────────────────────────────────────────────────────┘
               │ Channel B：WS 长连接（Host 是 ws client，帧=JSON+req_id）
┌──────────────┴────────────────────────────────────────────────────────┐
│ Go NM Host（cmd/nm-host，Chrome fork 的子进程，非 daemon）                │
│  Channel A：stdin/stdout 4B native-order 长度头 + JSON ↔ Chrome 扩展     │
│  写方向（Host→扩展）单帧 ≤1MB；读方向（扩展→Host）≤4GB                    │
└──────────────▲────────────────────────────────────────────────────────┘
               │ chrome.runtime.connectNative('com.hivemtk.browser')
┌──────────────┴────────────────────────────────────────────────────────┐
│ Chrome 扩展（MV3 SW）。Chrome 105+ connectNative 本身保活 SW，114+ port   │
│ 收发消息也保活 → port 不断则 SW 不死；port 断 → Host 进程被杀。            │
│ 扩展必须监听 port.onDisconnect 自动重连；server 侧断连即清理该 Host 会话。 │
└───────────────────────────────────────────────────────────────────────┘
```

### 1.2 定稿理由（为什么不是 HTTP 同步回调 / 轮询）

- HTTP 同步回调要求内部 Controller 挂起 gin worker 等 Chrome 执行完，Host 掉线时请求堆积不可控；
- 轮询 100ms 空转浪费且延迟高；
- 项目已有 `channelgw.WSTransport`（`/api/ws/channel`）成熟样板：升级 → 读 register 帧 → 校验 → 双泵循环。照抄结构即可。
- MV3 侧 connectNative port 是天然长连接，与 WS 对接形态一致。

### 1.3 请求-响应关联协议（WS 帧格式）

```jsonc
// server → Host：命令帧
{ "req_id": "b7f9…", "action": "click", "tab_id": 12, "target": "#submit" }
// Host → server：响应帧（req_id 原样带回）
{ "req_id": "b7f9…", "ok": true, "data": { ... } }
{ "req_id": "b7f9…", "ok": false, "error": "selector_not_found" }

// 注册帧（Host → server，连接后第一条）：
{ "type": "register", "version": "1.0.0", "pid": 1234 }
// 注册校验不靠 register 帧内容，靠 WS 握手时的 Authorization: Bearer <host token>，
// server 从 token 解出 user_id，绑定 连接↔用户。校验失败发 register_reject 并断开。
```

- Host 通道 token：复用 bridge 模式，KV key `browser_host_token`（admin 接口生成），`Authorization: Bearer <token>` + query `?token=`（JWT 中间件同款兼容，供 NM Host 无 header 环境使用）。
- **并发模型**：单个 Host 连接内命令**串行**（Host 单循环），不同用户的 Host 互不影响；server 端 Hand 不再加全局互斥锁，改为 per-连接发送锁 + `map[req_id]chan result` 异步等待（带超时）。同一用户多任务并发受 Host 串行天然排队。

### 1.4 安全边界

1. `/api/browser/host-ws` 双层防护：
   - **token 校验**（KV `browser_host_token`，fail-closed，参照 `BridgeIngressGuard`：无 token 配置时 503 拒绝，支持 `_prev` 双 token 轮换）；
   - **ClientIP 白名单**：仅 `127.0.0.1` / `::1`（frp 回源场景取 `X-Real-IP`，取不到且非本地直连即拒绝）。
2. nginx/frp 层：`/api/browser/host-ws` 不对外网暴露（本地部署本来走 127.0.0.1；文档部署清单需加一条 nginx location deny 兜底）。
3. 任务 URL 白名单：创建/执行任务时校验 `url` scheme ∈ {http, https}（防止 `chrome://`、`file://`、内网探测）。

---

## 2. user-server 侧：browser_automation 域五层

### 2.1 Model 层（5 个文件）

参考样板：`internal/geo/model/alert.go`

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
    ID          uint           `gorm:"primaryKey" json:"id"`
    Name        string         `gorm:"column:name;size:256;not null;index" json:"name"`
    Description string         `gorm:"column:description;type:text" json:"description"`
    TaskType    string         `gorm:"column:task_type;size:32;not null;default:one_shot;index" json:"task_type"` // one_shot / loop / cron / workflow
    Status      string         `gorm:"column:status;size:32;not null;default:draft;index" json:"status"` // draft / ready / running / paused / done / failed / archived
    Url         string         `gorm:"column:url;size:2048;not null" json:"url"`
    Steps       datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"`
    BrainMode   bool           `gorm:"column:brain_mode;default:false" json:"brain_mode"`
    BrainGoal   string         `gorm:"column:brain_goal;type:text" json:"brain_goal"`
    LlmPlanID   *uint          `gorm:"column:llm_plan_id;index" json:"llm_plan_id,omitempty"`
    LoopCount   int            `gorm:"column:loop_count;default:1" json:"loop_count"`
    DelayMs     int            `gorm:"column:delay_ms;default:1000" json:"delay_ms"`
    TimeoutSec  int            `gorm:"column:timeout_sec;default:120" json:"timeout_sec"`
    // workflow 依赖（依赖环在建依赖时 DFS 检测）
    DependsOnTaskID *uint      `gorm:"column:depends_on_task_id;index" json:"depends_on_task_id,omitempty"`
    DependsOnMode   string     `gorm:"column:depends_on_mode;size:32;default:all_done" json:"depends_on_mode"` // all_done / any_success
    // 失败自动重试（session 级）
    RetryOnFail    bool       `gorm:"column:retry_on_fail;default:false" json:"retry_on_fail"`
    RetryDelaySec  int        `gorm:"column:retry_delay_sec;default:300" json:"retry_delay_sec"`
    MaxRetryTimes  int        `gorm:"column:max_retry_times;default:3" json:"max_retry_times"`
    RetryCount     int        `gorm:"column:retry_count;default:0" json:"retry_count"`
    // 归属
    UserID      uint           `gorm:"column:user_id;index;not null" json:"user_id"`
    AccountID   uint           `gorm:"column:account_id;index" json:"account_id"`
    // 执行状态快照
    LastRunAt  *time.Time      `gorm:"column:last_run_at" json:"last_run_at,omitempty"`
    LastResult string          `gorm:"column:last_result;type:text" json:"last_result,omitempty"`
    ErrorMsg   string          `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserTask) TableName() string { return "browser_tasks" }
```

#### model/session.go

```go
package model

import (
    "time"

    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// BrowserSession Chrome tab 会话（一次执行 = 一个 session）
type BrowserSession struct {
    ID          uint           `gorm:"primaryKey" json:"id"`
    TaskID      uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    UserID      uint           `gorm:"column:user_id;index;not null" json:"user_id"`
    ChromeTabID int            `gorm:"column:chrome_tab_id;index" json:"chrome_tab_id"`
    Url         string         `gorm:"column:url;size:2048" json:"url"`
    Title       string         `gorm:"column:title;size:512" json:"title"`
    Status      string         `gorm:"column:status;size:32;not null;default:created;index" json:"status"` // created / active / completed / failed / stopped
    Snapshot    string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`
    LlmPlan     datatypes.JSON `gorm:"column:llm_plan;type:jsonb" json:"llm_plan,omitempty"`
    StartedAt   *time.Time     `gorm:"column:started_at" json:"started_at,omitempty"`
    CompletedAt *time.Time     `gorm:"column:completed_at" json:"completed_at,omitempty"`
    DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
    ErrorMsg    string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
    // 监控指标
    TotalSteps   int    `gorm:"column:total_steps;default:0" json:"total_steps"`
    SuccessSteps int    `gorm:"column:success_steps;default:0" json:"success_steps"`
    FailedSteps  int    `gorm:"column:failed_steps;default:0" json:"failed_steps"`
    HandLatencyMs int64 `gorm:"column:hand_latency_ms" json:"hand_latency_ms"`
    ConsoleErrors string `gorm:"column:console_errors;type:text" json:"console_errors,omitempty"`
    // 反馈产物（截图存 LocalDriver 的 URL，不落 base64）
    ExtractedData        datatypes.JSON `gorm:"column:extracted_data;type:jsonb" json:"extracted_data,omitempty"`
    FinalScreenshotURL   string         `gorm:"column:final_screenshot_url;size:1024" json:"final_screenshot_url,omitempty"`
    LlmSummary           string         `gorm:"column:llm_summary;type:text" json:"llm_summary,omitempty"`

    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserSession) TableName() string { return "browser_sessions" }
```

#### model/step.go

```go
package model

import (
    "time"

    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// BrowserStep 任务内的执行步骤（每次执行产生一条 step 记录，用于回放/调试）
type BrowserStep struct {
    ID         uint           `gorm:"primaryKey" json:"id"`
    SessionID  uint           `gorm:"column:session_id;index;not null" json:"session_id"`
    TaskID     uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    StepIndex  int            `gorm:"column:step_index;not null" json:"step_index"`
    Action     string         `gorm:"column:action;size:32;not null;index" json:"action"`
    Target     string         `gorm:"column:target;size:1024" json:"target"`
    Value      string         `gorm:"column:value;type:text" json:"value"`
    Params     datatypes.JSON `gorm:"column:params;type:jsonb" json:"params"` // wait/scroll/extract/screenshot 等扩展参数
    Status     string         `gorm:"column:status;size:32;not null;default:pending;index" json:"status"` // pending / running / success / failed / skipped
    Result     datatypes.JSON `gorm:"column:result;type:jsonb" json:"result,omitempty"`
    DurationMs int64          `gorm:"column:duration_ms" json:"duration_ms"`
    ErrorMsg   string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`

    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
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

// BrowserCronTrigger 定时触发器（CronExpr 存储 5 段表达式，注册前补秒段转 6 段）
type BrowserCronTrigger struct {
    ID        uint           `gorm:"primaryKey" json:"id"`
    TaskID    uint           `gorm:"column:task_id;uniqueIndex;not null" json:"task_id"`
    CronExpr  string         `gorm:"column:cron_expr;size:128;not null" json:"cron_expr"`
    Enabled   bool           `gorm:"column:enabled;default:true;index" json:"enabled"`
    NextRunAt *time.Time     `gorm:"column:next_run_at" json:"next_run_at,omitempty"`
    LastRunAt *time.Time     `gorm:"column:last_run_at" json:"last_run_at,omitempty"`

    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
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
type BrowserLLMPlan struct {
    ID        uint           `gorm:"primaryKey" json:"id"`
    TaskID    uint           `gorm:"column:task_id;index;not null" json:"task_id"`
    Goal      string         `gorm:"column:goal;type:text;not null" json:"goal"`
    Snapshot  string         `gorm:"column:snapshot;type:text" json:"snapshot,omitempty"`
    Steps     datatypes.JSON `gorm:"column:steps;type:jsonb" json:"steps"`
    Reasoning string         `gorm:"column:reasoning;type:text" json:"reasoning,omitempty"` // 调试用，注意脱敏
    Model     string         `gorm:"column:model;size:64" json:"model"`
    TokenIn   int            `gorm:"column:token_in" json:"token_in"`
    TokenOut  int            `gorm:"column:token_out" json:"token_out"`

    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserLLMPlan) TableName() string { return "browser_llm_plans" }
```

### 2.2 Repository 层（5 个文件）

参考样板：`internal/geo/repository/alert.go`（interface + 双构造器 + ctx 首参 + `r.db.WithContext(ctx)`）。

| Repo | 方法 |
|------|------|
| **BrowserTaskRepository** | Create / GetByID(id, userID) / List(userID, status, taskType, page, limit) / Update / UpdateStatus / SoftDelete / FindRunning(userID) / GetByIDAnyUser(id)（依赖检查用，仅同 owner 校验在 service 层做） |
| **BrowserSessionRepository** | Create / GetByID(id, userID) / ListByTaskID / ListByUser / UpdateStatus / UpdateChromeTabID / UpdateMetrics / FailRunningByUser(userID, reason)（Host 断连清理钩子）/ HasSuccess(taskID) / GetLatestByTaskID |
| **BrowserStepRepository** | BatchCreate / GetByID / ListBySessionID / UpdateStatus / UpdateResult |
| **BrowserCronTriggerRepository** | Create / GetByTaskID / GetByID / ListByUser / ListAllEnabled / Update / UpdateTimes / Delete |
| **BrowserLLMPlanRepository** | Create / GetByID / ListByTaskID |

全部带 `userID` 过滤（除 GetByIDAnyUser 明确注释用途），防越权串号。

### 2.3 Service 层（8 个文件）

```
service/
├── task.go       ← 任务 CRUD + publish/pause/archive + RunTask（异步）
├── executor.go   ← 执行引擎：steps 解释 + Hand 调用 + stopCh 注册表 + 超时
├── hand.go       ← 命令发送：per-连接锁 + req_id 异步等待（无全局锁）
├── host_registry.go ← 【核心】Host WS 注册表：userID→连接 映射 + 断连清理钩子
├── session.go    ← Session/Step 查询 + 手动中断
├── cron.go       ← Cron 管理 + TaskManager 集成（5 段→6 段转换）
├── brain.go      ← LLM plan 生成 + session 总结（Dispatcher）
└── feedback.go   ← 通知（Feishu/Mail）+ 产物保存（LocalDriver）+ 导出
```

#### service/host_registry.go（核心新增，v1 缺失）

```go
package service

// HostRegistry 管理在线 NM Host 连接：userID → *HostConn
// - Register(user_id, conn)：WS 注册成功后登记
// - Send(userID, cmd)：命令路由到**归属 Host**，无连接返回 ErrHostOffline
// - Unregister(user_id, conn)：断连时触发清理钩子——该用户所有 running session 置 failed
type HostRegistry struct {
    mu    sync.RWMutex
    conns map[uint]*HostConn // key = user_id
}

var ErrHostOffline = errors.New("browser host 未连接，请确认本机 Chrome 已启动且扩展已加载")
```

HostConn 持有 `*websocket.Conn`、写锁、`pending map[string]chan *CommandResult`、`closed chan struct{}`。读写泵：读循环收到帧按 `req_id` 投递到 pending channel；写循环串行发帧（`?token=` 或 Authorization 均在握手层校验完成）。

#### service/hand.go（定稿版，替换 v1 残片）

```go
package service

// Hand 命令出口：往 HostRegistry 归属连接发命令帧，等 req_id 回包。
// 约束：1) 不启动任何子进程（Chrome 才是 Host 父进程）
//       2) 单 Host 连接内串行（Host 单循环），跨用户天然隔离
//       3) 每命令带超时（默认 30s，wait_for_selector 类可更长）
func (h *Hand) send(ctx context.Context, userID uint, timeout time.Duration, cmd map[string]any) (map[string]any, error) {
    if err := h.registry.EnsureOnline(userID); err != nil {
        return nil, err
    }
    ctx2, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    return h.registry.Request(ctx2, userID, cmd) // 内部分配 req_id、登记 pending、写帧、等回包
}

// openTab / click / typeText / snapshot / markdown / waitFor /
// waitForSelector / scroll / extract / closeTab —— 一一映射 §6 原语表
```

#### service/task.go — RunTask（异步定稿）

```go
// RunTask 校验归属/状态/依赖/URL scheme → 创建 Session → goroutine 执行 → 立即返回 session
// - 幂等：同任务已有 running session 时返回 409（复用 FindRunning）
// - 依赖检查：all_done=前置最近 session completed；any_success=HasSuccess
// - 依赖环：SetDependsOn 时 DFS 检测（task.go 内 DetectDependencyCycle）
// - 执行体：utils.SafeGo(ctx, "browser_automation.run", func(ctx){ executor.ExecuteSession(...) })
func (s *TaskService) RunTask(ctx context.Context, taskID, userID uint) (*model.BrowserSession, error)
```

#### service/executor.go

- `stopRegistry map[uint]chan struct{}`（sessionID→stopCh），`SignalStop(sessionID)` 供手动中断；
- 每步执行前 `select stopCh`；
- 每步经 Hand 发命令，`step.status` pending→running→success/failed，逐条落库；
- 错误处理：`retry_count`（backoff×2 递增）→ `continue_on_error` 决定跳过或整 session failed；
- 超时：`context.WithTimeout(ctx, task.TimeoutSec)` 包住整个 session（替代 v1 的 AfterFunc 直改状态——状态只由 Executor 收口），到点 ctx 取消 → 当前命令失败 → session failed + 关 tab；
- 步间 `time.Sleep(task.DelayMs)` 可被 stopCh 打断；
- Host 断连（Hand 返回 ErrHostOffline / registry 广播）：session 置 failed "Host 掉线"。

#### service/brain.go（真实 Dispatcher 调用形态）

```go
dispatcher := llm.NewDispatcher(llm.NewLLMService())
result, err := dispatcher.Dispatch(ctx, llm.DispatchRequest{
    Scenario:    llm.ScenarioHighQuality,
    SystemPrompt: browserPlanSystemPrompt, // 输出 JSON steps 数组
    Prompt:      goal + "\n\n页面快照：\n" + snap,
    JSONMode:    true,
    MaxTokens:   4096,
})
// 结构化解析用 dispatcher.DispatchStructured(ctx, req, &planSchema)（dispatcher_dispatch.go L279）
```

Brain 模式循环（maxIterations=10）：snapshot → Dispatch 解析 steps → 执行 → done? 注意每轮 plan 落库（含 reasoning 脱敏截断 4KB）。

#### service/cron.go（6 段转换）

```go
// toSixField 把用户/前端 5 段 cron（"*/5 * * * *"）转项目 TaskManager 的 6 段秒级（"0 */5 * * * *"）
func toSixField(expr string) string { return "0 " + expr }
// 注册：mgr := cron.GetTaskManager(); mgr.AddTask(toSixField(t.CronExpr), func(){ ... RunTask ... })
// Enable/Disable/Delete 时 mgr.RemoveTask(entryID)；进程重启后 ListAllEnabled 重新注册
```

#### service/feedback.go（真实客户端）

- 通知：飞书 `service.NewFeishuIntegrationService(db).SendMessage(...)`；邮件 `pkg/mail.SendMail(cfg, to, subject, body, false)`；钉钉/企微走通用 webhook（`WebhookService`）。
- 产物：`storage.NewLocalDriver(uploadDir, publicBaseURL)` + `UploadReader` 存 final_screenshot（PNG bytes），session 只存 URL（`/files/...`）。
- session 完成/失败后异步触发：通知 + LLM 总结（可选开关）+ retry_on_fail 调度（`cron.AddTask` 一次性延迟任务 + `RemoveTask`）。

### 2.4 Controller 层（4 个文件）

签名对齐 `internal/geo/controller/alert.go`：`func (c *TaskController) Xxx(ctx *gin.Context)`，`response.Success/Error`，`c.GetUint("user_id")`。

- **task.go**：CRUD + publish/run/pause/resume/archive + SetDependsOn（检环）；
- **session.go**：List / Get / ListSteps / **Stop**（POST /sessions/:id/stop）；
- **cron.go**：CRUD + enable/disable；
- **host.go**：GetStatus（admin，registry 在线状态）/ ResetToken（admin，KV upsert `browser_host_token`）。

### 2.5 DTO 层（3 个文件）

```go
type CreateBrowserTaskReq struct {
    Name        string `json:"name" binding:"required,max=256"`
    Description string `json:"description"`
    TaskType    string `json:"task_type" binding:"required,oneof=one_shot loop cron workflow"`
    Url         string `json:"url" binding:"required,max=2048,url"`
    BrainMode   bool   `json:"brain_mode"`
    BrainGoal   string `json:"brain_goal"`
    Steps       []StepItem `json:"steps"`
    LoopCount   int    `json:"loop_count" binding:"omitempty,min=1,max=1000"`
    DelayMs     int    `json:"delay_ms" binding:"omitempty,min=0,max=60000"`
    TimeoutSec  int    `json:"timeout_sec" binding:"omitempty,min=10,max=3600"`
}

type StepItem struct {
    Action       string `json:"action" binding:"required,oneof=open_tab click type snapshot markdown screenshot wait wait_for_selector scroll extract close_tab"`
    Target       string `json:"target"`
    Value        string `json:"value"`
    Ms           int    `json:"ms"`
    ClearFirst   bool   `json:"clear_first"`
    SubmitOnEnter bool  `json:"submit_on_enter"`
    Direction    string `json:"direction" binding:"omitempty,oneof=up down left right"`
    Amount       int    `json:"amount"`
    Selector     string `json:"selector"`
    TimeoutMs    int    `json:"timeout_ms"`
    // 错误处理
    ContinueOnError bool `json:"continue_on_error"`
    RetryCount      int  `json:"retry_count" binding:"omitempty,min=0,max=10"`
    RetryBackoffMs  int  `json:"retry_backoff_ms" binding:"omitempty,min=100"`
}
```

> `extract` 原语 MVP 降级：`schema` 为 CSS selector 列表（`{"title": ".h1", "items": ".card"}`），复杂提取走 Brain 模式（markdown + LLM），不实现通用 schema 解释器。

---

## 3. Migration v3.37.0

路径：`user-server/internal/migration/migrations/v3_37_0_browser_automation_migration.go`
风格对齐 `v3_35_0_telegram_group_gate_migration.go`：raw SQL、分条 Exec、`CREATE INDEX IF NOT EXISTS` 单独语句、**无内联 INDEX、无 FOREIGN KEY**。

```sql
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
    depends_on_task_id BIGINT,
    depends_on_mode VARCHAR(32) DEFAULT 'all_done',
    retry_on_fail BOOLEAN NOT NULL DEFAULT FALSE,
    retry_delay_sec INT NOT NULL DEFAULT 300,
    max_retry_times INT NOT NULL DEFAULT 3,
    retry_count INT NOT NULL DEFAULT 0,
    user_id BIGINT NOT NULL,
    account_id BIGINT DEFAULT 0,
    last_run_at TIMESTAMP,
    last_result TEXT,
    error_msg TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_browser_tasks_user ON browser_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_browser_tasks_status ON browser_tasks(status);
CREATE INDEX IF NOT EXISTS idx_browser_tasks_type ON browser_tasks(task_type);

CREATE TABLE IF NOT EXISTS browser_sessions (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    chrome_tab_id INT DEFAULT 0,
    url VARCHAR(2048) DEFAULT '',
    title VARCHAR(512) DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'created',
    snapshot TEXT,
    llm_plan JSONB,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    duration_ms BIGINT DEFAULT 0,
    error_msg TEXT,
    total_steps INT NOT NULL DEFAULT 0,
    success_steps INT NOT NULL DEFAULT 0,
    failed_steps INT NOT NULL DEFAULT 0,
    hand_latency_ms BIGINT DEFAULT 0,
    console_errors TEXT,
    extracted_data JSONB,
    final_screenshot_url VARCHAR(1024) DEFAULT '',
    llm_summary TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_task ON browser_sessions(task_id);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_user ON browser_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_status ON browser_sessions(status);

CREATE TABLE IF NOT EXISTS browser_steps (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    step_index INT NOT NULL,
    action VARCHAR(32) NOT NULL,
    target VARCHAR(1024) DEFAULT '',
    value TEXT,
    params JSONB,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    result JSONB,
    duration_ms BIGINT DEFAULT 0,
    error_msg TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_browser_steps_session ON browser_steps(session_id);
CREATE INDEX IF NOT EXISTS idx_browser_steps_task ON browser_steps(task_id);
CREATE INDEX IF NOT EXISTS idx_browser_steps_status ON browser_steps(status);

CREATE TABLE IF NOT EXISTS browser_cron_triggers (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    cron_expr VARCHAR(128) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMP,
    last_run_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT uk_browser_cron_task UNIQUE (task_id)
);
CREATE INDEX IF NOT EXISTS idx_browser_cron_enabled ON browser_cron_triggers(enabled);

CREATE TABLE IF NOT EXISTS browser_llm_plans (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    goal TEXT NOT NULL,
    snapshot TEXT,
    steps JSONB,
    reasoning TEXT,
    model VARCHAR(64) DEFAULT '',
    token_in INT DEFAULT 0,
    token_out INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_browser_llm_plans_task ON browser_llm_plans(task_id);
```

Down：子表先删（steps → llm_plans → cron_triggers → sessions → tasks），全部 `DROP TABLE IF EXISTS`。
注册：`initial_schema.go` 的 `RegisterMigrations` 末尾追加 `register(NewBrowserAutomationMigration(db))`。

---

## 4. Router 注册

路径：`internal/router/browser_automation_routes.go`

```go
// SetupBrowserAutomationRoutes 浏览器自动化路由。
// 业务路由挂 auth（JWT）；Host WS 挂在 engine 上（独立于 auth，自带 token+IP 双层防护）。
func SetupBrowserAutomationRoutes(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
    // DI 装配（全部在函数内完成）
    hostRegistry := service.NewHostRegistry()                 // 进程级单例
    hand         := service.NewHand(hostRegistry)
    taskRepo     := repository.NewBrowserTaskRepositoryWithDB(gormDB)
    sessionRepo  := repository.NewBrowserSessionRepositoryWithDB(gormDB)
    stepRepo     := repository.NewBrowserStepRepositoryWithDB(gormDB)
    cronRepo     := repository.NewBrowserCronTriggerRepositoryWithDB(gormDB)
    planRepo     := repository.NewBrowserLLMPlanRepositoryWithDB(gormDB)
    brainSvc     := service.NewBrainService(planRepo)
    feedbackSvc  := service.NewFeedbackService(gormDB)
    executor     := service.NewExecutor(hand, sessionRepo, stepRepo, brainSvc, feedbackSvc)
    taskSvc      := service.NewTaskService(taskRepo, sessionRepo, executor)
    sessionSvc   := service.NewSessionService(sessionRepo, stepRepo, executor)
    cronSvc      := service.NewCronService(cronRepo, taskSvc)
    // 进程启动后恢复已启用 cron：utils.SafeGo 里 cronSvc.RestoreAll()

    taskCtrl    := controller.NewTaskController(taskSvc)
    sessionCtrl := controller.NewSessionController(sessionSvc)
    cronCtrl    := controller.NewCronController(cronSvc)
    hostCtrl    := controller.NewHostController(hostRegistry, taskSvc)

    ba := auth.Group("/browser-automation")
    ba.POST("/tasks", taskCtrl.Create)                 // + URL scheme 校验
    ba.GET("/tasks", taskCtrl.List)
    ba.GET("/tasks/:id", taskCtrl.Get)
    ba.PUT("/tasks/:id", taskCtrl.Update)
    ba.DELETE("/tasks/:id", taskCtrl.Delete)
    ba.POST("/tasks/:id/publish", taskCtrl.Publish)
    ba.POST("/tasks/:id/run", taskCtrl.Run)            // 异步，立即返回 session_id
    ba.POST("/tasks/:id/pause", taskCtrl.Pause)
    ba.POST("/tasks/:id/resume", taskCtrl.Resume)
    ba.POST("/tasks/:id/archive", taskCtrl.Archive)

    ba.GET("/sessions", sessionCtrl.List)
    ba.GET("/sessions/:id", sessionCtrl.Get)
    ba.GET("/sessions/:id/steps", sessionCtrl.ListSteps)
    ba.GET("/tasks/:id/sessions", sessionCtrl.ListByTask)
    ba.POST("/sessions/:id/stop", sessionCtrl.Stop)

    ba.GET("/cron", cronCtrl.List)
    ba.POST("/cron", cronCtrl.Create)
    ba.PUT("/cron/:id", cronCtrl.Update)
    ba.DELETE("/cron/:id", cronCtrl.Delete)
    ba.POST("/cron/:id/enable", cronCtrl.Enable)
    ba.POST("/cron/:id/disable", cronCtrl.Disable)

    baAdmin := ba.Group("")
    baAdmin.Use(middleware.AdminAuthMiddleware())
    baAdmin.GET("/host/status", hostCtrl.GetStatus)
    baAdmin.POST("/host/token/reset", hostCtrl.ResetToken)

    // Host WebSocket（双层防护：bridge 模式 token + 本地回环 IP 白名单）
    engine.GET("/api/browser/host-ws", NewHostWSHandler(hostRegistry, taskSvc, sessionRepo).Handle)
}
```

`router.go` 挂载（L321 `SetupGeoRoutes(auth, gormDB)` 旁）：

```go
SetupBrowserAutomationRoutes(auth, r, gormDB)
```

> `/api/browser/host-ws` 挂 engine 而非 auth 组：它不走 JWT（NM Host 无用户登录态），鉴权由 Handler 内 token+IP 双层完成；挂在 `/api` 前缀下便于 nginx/frp 一条规则封禁。**部署清单必须加：nginx `location /api/browser/ { deny all; }`（仅本机回环放行）**。

---

## 5. Go NM Host（cmd/nm-host）

```go
// main.go 主循环（~200 行）
// 1. env: HIVE_MTK_WS_URL(默认 ws://127.0.0.1:8204/api/browser/host-ws)
//         HIVE_MTK_HOST_TOKEN(必填，install.sh 时由 admin 生成写入 ~/.hivemtk/nm_host.conf)
// 2. websocket.DefaultDialer.Dial(wsURL+"?token="+token) —— 断线指数退避重连
// 3. 循环：读 WS 命令帧 → writeNativeFrame(stdout, 4B native-order 长度头 + JSON)
//          → readNativeFrame(stdin) → WS 回传 {req_id, ok, data|error}
// 4. 帧限制：写方向(Host→扩展)校验 ≤1MiB（官方 host→extension 限制）
// 5. stdin 关闭（Chrome 退出/port 断开）→ 进程自然退出（Chrome 负责杀）
```

- 字节序：`binary.NativeEndian.PutUint32`（官方措辞 native order；arm64/x86 等价 LE）。
- **没有独立 go.mod**（共享 `user-server/go.mod`，与 api/geo-run/seed 并列）。
- install.sh：编译 → `/usr/local/bin/hivemtk_browser_nm_host` → 写 `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.hivemtk.browser.json`（macOS 用户级；`allowed_origins` 填**固定扩展 ID**，见 §7.1 key 固定）→ 提示填 token。

### 5.1 扩展 ID 固定（v1 缺失）

`allowed_origins` **禁止通配符**，而 unpacked 扩展 ID 随目录路径派生。manifest.json 写死 `"key"`（公钥）固定 ID，install.sh 的 EXT_ID 即可写死，换目录/重装无需重跑注册。

---

## 6. 原语全集（9 种 MVP + 2 种 Brain 内部）

> 每个原语标注 MV3 约束。扩展侧统一 `chrome.scripting.executeScript({target:{tabId}, func, args})` —— **func 会被序列化，闭包变量全部丢失，外部值必须经 args 传入**（官方 scripting API 硬约束）。

| # | 原语 | 扩展实现 | 关键参数 / 约束 |
|---|------|---------|----------------|
| 1 | `open_tab` | `chrome.tabs.create({url, active:false})` | url 已在 service 层限 http/https；active 恒 false |
| 2 | `click` | executeScript `args:[target]` → `document.querySelector(t)?.click()` | target=CSS selector 或 `@e3` refs（经 accessibility.js 映射） |
| 3 | `type` | executeScript `args:[target, value, clearFirst, submit]` | 先 focus，`document.execCommand('insertText')` 触发输入事件 |
| 4 | `snapshot` | executeScript 遍历可交互 DOM 生成 `role [name] @eN` | refs 表由扩展持久缓存（常驻 map），跨命令有效；页面导航即失效重取 |
| 5 | `markdown` | executeScript 走 DOM → 粗粒度 Markdown | 供 LLM 吃 |
| 6 | `screenshot` | **MVP 仅限对 active tab**：`chrome.tabs.captureVisibleTab(windowId, {format:'png'})` | 无 tabId 参数；不能截后台 tab；不能全页；限频 2 次/秒 → 仅 session 完成后把 tab 激活截一次 final_screenshot（方案 M3）。全页/后台截图后续可选 chrome.debugger + CDP `captureBeyondViewport`（有"正在调试"横幅，本版不做） |
| 7 | `wait` | setTimeout | ms ∈ [0, 60000] |
| 8 | `wait_for_selector` | executeScript 轮询 querySelector（200ms 间隔） | timeout_ms ∈ [1000, 60000] |
| 9 | `scroll` | executeScript `window.scrollBy` | direction ∈ up/down/left/right |
| 10 | `extract` | executeScript 按 CSS selector 列表提取 textContent | MVP 降级为 selector 列表，不做通用 schema |
| 11 | `close_tab` | `chrome.tabs.remove(tabId)` | session 收尾调用 |

**截图说明（M3 定稿）**：session 执行结束后，扩展把 tab `chrome.tabs.update(tabId, {active:true})` 激活 → `captureVisibleTab` 截一次 → 传回 server 存 LocalDriver → `chrome.tabs.update(tabId, {active:false})`（若原非激活）。代价：执行结束瞬间 tab 会闪一下焦点；换取零 debugger 权限、无横幅。`after_screenshot` 每步截图从 Model/DDL 中**移除**。

---

## 7. Chrome 扩展（user-web/browser_automation/）

### 7.1 manifest.json

```json
{
  "manifest_version": 3,
  "name": "HiveMTK Browser Automation",
  "version": "1.0.0",
  "key": "<写死公钥，固定扩展 ID>",
  "permissions": ["nativeMessaging", "tabs", "scripting"],
  "host_permissions": ["<all_urls>"],
  "background": { "service_worker": "background.js" },
  "action": { "default_popup": "popup.html" },
  "icons": { "128": "icons/128.png" }
}
```

- `"nativeMessaging"` 权限**必须**声明（connectNative 前提）。
- `host_permissions` MVP 用 `<all_urls>`（通用自动化场景），上线前可按需收敛。
- background 只负责 connectNative port 生命周期 + 原语分发；sendNativeMessage **不可用**（每次新起 Host 进程、只认第一条回包，无法做长会话）。

### 7.2 background/index.js

```
port = chrome.runtime.connectNative('com.hivemtk.browser')
port.onMessage ← {req_id, action, ...} → primitives 分发 → port.postMessage({req_id, ok, data})
port.onDisconnect ← 自动重连（指数退避，上限 5 次）；重连失败置 popup 状态"Host 离线"
SW 保活：Chrome 105+ connectNative 保活 SW；114+ port 收发消息保活 → port 不断则 SW 不死。
```

### 7.3 core 模块

- `native-messaging.js`：port 封装 + req_id 关联（单 port 复用）。
- `primitives.js`：§6 原语实现（全部 executeScript func+args 形态）。
- `tab-manager.js`：open/close/activate、tab 存活探测（`chrome.tabs.get`）。
- `accessibility.js`：`@e{N}` refs 快照生成 + ref→selector 映射缓存（页面导航失效）。

### 7.4 popup

Host 连接状态 + 快捷"执行选中任务"入口 + 最近 session 列表。

---

## 8. 全链路数据流（定稿）

```
① 前端 List.vue 点"执行"
   api/browserAutomation.js runBrowserTask(id) → POST /api/browser-automation/tasks/:id/run
   → store.startRun() 拿 session_id → 跳 Monitor.vue，2s 轮询 steps
② TaskController.Run → taskSvc.RunTask：校验(归属/幂等/依赖/URL) → 建 Session → SafeGo 异步执行
③ Executor：hand.send(userID, cmd) → HostRegistry.Request 分配 req_id → 写 WS 帧
④ NM Host 读 WS 帧 → writeNativeFrame → 扩展 SW → primitives.executeScript
⑤ 扩展回帧 → Host 读 stdin → WS 回传 {req_id,...} → registry 投递 pending chan → Hand 返回
⑥ Executor 逐条落 step → session 完成 → feedback（通知/截图产物/总结/retry）
⑦ 前端轮询 sessions/:id/steps 渲染状态
```

---

## 9. 状态机

### Task
```
[publish] draft→ready    [run] ready→running    完成→done / 失败→failed(可 retry_on_fail)
[pause] running→paused   [resume] paused→running   [archive] 终态→archived
```
> pause 语义定稿：pause = stop 当前 session（保留 tab），resume = 重新 run。长等待原语执行中不可中断，pause 在步间生效。

### Session
```
created →(openTab ok)→ active →(全部 step 完)→ completed
                     ↘(任一 step 终止失败 / 超时 / stop / Host 掉线)→ failed|stopped
```

### Step
```
pending → running → success | failed | skipped(continue_on_error)
```

---

## 10. 多租户与产品边界（新增，v1 缺失）

1. **适用形态**：本功能仅对"本机部署 user-server + 本机 Chrome"的单机/自部署形态开放。云端多租户用户默认无 Host。
2. **无 Host 降级**：`POST /tasks/:id/run` 时 `ErrHostOffline` → 409 + message 引导："本机 Chrome 未连接。请在本机安装扩展（chrome://extensions 加载 user-web/browser_automation/dist）并运行 user-server/cmd/nm-host/install.sh"。前端 Detail/List 对 409 弹引导弹窗。
3. **命令路由**：Host WS 注册以 Bearer token 鉴权，token 与 user_id 绑定（admin 生成 token 时记录归属），registry 按 user_id 路由，**绝不跨用户投递命令**。
4. **数据归属**：tasks/sessions/steps 全表带 user_id 且 repo 层强制过滤；admin 可经 `/host/status` 看在线状态但看不到他人任务列表。

---

## 11. 前端（user-web）

### 11.1 API（src/api/browserAutomation.js）

对齐 geoAlert.js；**注意不要重名导出**（v1 `listBrowserSessions` 定义了两次）：

```js
import { http } from '@/utils/http'
export const listBrowserTasks = (params) => http.get('/api/browser-automation/tasks', params)
export const runBrowserTask = (id) => http.post(`/api/browser-automation/tasks/${id}/run`)
// ...任务/Session/Cron 全套；按任务查会话命名 listBrowserTaskSessions(taskId)
```

### 11.2 Store（setup 风格，注意拆包）

```js
import { defineStore } from 'pinia'
import { ref, onUnmounted } from 'vue'
export const useBrowserAutomationStore = defineStore('browserAutomation', () => {
  const runningSession = ref(null)
  let pollTimer = null
  const stopPoll = () => { if (pollTimer) { clearInterval(pollTimer); pollTimer = null } }
  async function pollSteps(sessionId) {
    // 拦截器已拆 data.data；res?.data || res 兜底
    const res = await getBrowserSessionSteps(sessionId)
    const steps = res?.data || res
    // ... all done → stopPoll
  }
  async function startRun(taskId) {
    const res = await runBrowserTask(taskId)
    runningSession.value = res?.data || res
    stopPoll()
    pollTimer = setInterval(() => pollSteps(runningSession.value.id), 2000)
  }
  return { runningSession, startRun, stopPoll }
})
```

### 11.3 路由 + 菜单（三处注册）

1. `src/router/modules/browserAutomation.js`：`export default [...]`，meta `{title:'浏览器自动化', icon:'Monitor', group:'automation', requiresAuth:true}`；页面 List / Create(合并 Editor，`:id` 可选) / Detail / Monitor / Cron。
2. `src/router/index.js`：`moduleNames` 数组加入 `'browserAutomation'` + `pathToModule` 映射。
3. `src/layout/Layout.vue`：手写菜单配置加入分组（icon 经 `utils/iconMap.js`）。

### 11.4 页面（6 个，页面文案硬编码中文）

| 页面 | 文件 | 功能 |
|------|------|------|
| 任务列表 | List.vue | 表格 + 发布/执行/暂停/归档/删除 + **409 无 Host 引导弹窗** |
| 任务编辑 | Editor.vue | Create+Edit 合一（`:id?`）；步骤编排（原语下拉+参数+错误策略）+ Brain 开关 + 依赖任务选择 |
| 任务详情 | Detail.vue | 概览 + 步骤列表 + sessions 历史 + Cron 配置 + 通知设置 |
| 执行监控 | Monitor.vue | 2s 轮询 steps；Dashboard（总耗时/成功率/P50 手算可省/Hand 延迟）+ LLM 总结卡 + 停止按钮 |
| 定时触发器 | Cron.vue | 列表 + 启停 + 表达式校验提示（5 段） |
| Host 状态 | Status.vue | admin：在线状态 + token 重置 + 安装引导 |

---

## 12. 文件清单

| 代码区 | 文件 |
|--------|------|
| user-server 域 | model×5 + repository×5 + service×8 + controller×4 + dto×3 = **25 个 .go** |
| migration + router | v3_37_0_browser_automation_migration.go + browser_automation_routes.go = **2** |
| NM Host | cmd/nm-host/{main.go, install.sh, manifest.json.template} = **3** |
| user-web | api×1 + views×6 + store×1 + router module×1 = **9** |
| 扩展 | manifest.json + package.json + build.mjs + background + core×4 + popup×2 + test×3 ≈ **12** |
| **合计** | **~51 个文件** |

---

## 13. 实施优先级

| 阶段 | 内容 | 验收 |
|------|------|------|
| P0 | migration + model + repository + dto | go build 通过 |
| P1 | host_registry + hand + executor + task/session/cron/brain/feedback service | 单测通过 |
| P2 | controller + 路由 + router.go 接线 | go build + 手工 curl |
| P3 | 扩展 + NM Host + install.sh | 扩展加载 + Host 注册成功（host/status connected:true） |
| P4 | 前端 9 文件 | vite build + 页面走查 |
| P5 | brain 模式打磨 + 通知渠道接入 | 端到端 |

---

## 14. 已核实的关键事实清单（防再错）

1. module 名 `hivemtk-user`；统一端口 `config.DefaultListenPort = "8204"`。
2. migration 最高版本 v3.36.0 → 本功能 **v3.37.0**。
3. `response.Success(ctx, data, msg)` / `response.Error(ctx, httpCode, msg)`（`utils/response`）。
4. `AdminAuthMiddleware` / `JWTAuthMiddleware` 在 `internal/middleware/jwt.go`；`c.GetUint("user_id")`。
5. Cron 是 6 段秒级 spec（`cron.WithSeconds()`）。
6. LLM 入口 `llm.NewDispatcher(llm.NewLLMService()).Dispatch(ctx, llm.DispatchRequest{...})`；场景常量如 `ScenarioHighQuality`；结构化用 `DispatchStructured`。
7. 内部 token 先例：`BridgeIngressGuard`（KV `bridge_ingest_token`，fail-closed + `_prev` 轮换 + 常量时间比较）。
8. WS 挂统一端口先例：`router.go` bridgeWS 组 `/api/ws/channel`（gorilla/websocket）。
9. Chrome NM 帧：4B **native-order** 长度头；host→extension 1MB / extension→host 4GB；`connectNative` 需 `nativeMessaging` 权限；`sendNativeMessage` 每次新起进程不可用于长会话。
10. MV3：SW 30s idle 被杀，但 connectNative（105+）与 port 消息（114+）保活；port 断 Host 被杀。
11. `captureVisibleTab(windowId?, options?)` 无 tabId、仅激活 tab、不可全页、2 次/秒。
12. `executeScript` 的 func 序列化注入，闭包丢失，必须 args 传参；默认仅顶层 frame。
13. `allowed_origins` 禁通配符；unpacked ID 用 manifest `"key"` 固定。
14. 前端拦截器返回 `data.data`；页面兜底 `res?.data || res`；Pinia setup 风格；菜单在 Layout.vue 手写 + router index moduleNames 白名单。

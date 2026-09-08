# 浏览器自动化产品完整设计方案

> 基于 Chrome MCP Native Messaging 方案，实现 **LLM 驱动的泛操作 + 浏览器原语执行** 的手脑分离架构

---

## 1. 产品定位与核心价值

### 1.1 是什么

浏览器自动化产品 = **自然语言驱动的 Chrome 操作编排引擎**

- 用户用一句话或结构化工作流描述"我要做什么"
- LLM 大脑把意图翻译成浏览器原语序列（click/type/scroll/screenshot/extract）
- Native Messaging Chrome 扩展在主 Profile 里后台 tab 执行原语
- 支持循环、定时、工作流子节点三种触发方式

### 1.2 手脑分离原则

```
┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐
│  大脑 Brain      │   │  翻译 Translator │   │  手 Hand          │
│  (LLM 决策层)    │──▶│  原语序列生成器   │──▶│  Chrome 执行器    │
│  吃自然语言       │   │  原语 Plan Schema │   │  Native Messaging │
│  产结构化计划     │   │  吃 DOM 产原语    │   │  只做浏览器原语    │
└──────────────────┘   └──────────────────┘   └──────────────────┘
```

- **大脑不直接操作浏览器**，只负责理解意图 + 生成结构化计划
- **翻译层** 是独立的 plan schema + DOM 分析器，纯函数可单测
- **手只做原语**：扩展只暴露 createTab / click / type / screenshot / getHTML 等基础动作
- 好处：LLM 升级不影响执行器、执行器换实现（CDP→Native Messaging）不影响上层

### 1.3 降低上手难度

| 场景 | 传统 Playwright/Selenium | 本产品 |
|------|-------------------------|--------|
| 登录某后台 | 写 30 行代码处理等待/重定向 | "登录 XXX 后台，账号 X 密码 Y" |
| 定期抓数据 | 写 cron + 脚本 + 维护 | "每天 9:00 登录后台抓订单列表导出 CSV" |
| 批量审核内容 | 写爬虫 + 规则引擎 | "遍历最近 50 条内容，违规的打标记" |
| 跨站操作 | 维护 cookie 池 + profile | 复用主 Chrome profile，登录态继承 |

### 1.4 三种触发方式

| 触发类型 | 说明 | 复用组件 |
|----------|------|----------|
| **循环任务** | forEach 遍历数据集，每条执行同一计划 | workflow_orchestrator + 本产品 step |
| **定时任务** | cron 表达式触发（每天 9:00 / 每小时） | 现有 `pkg/cron` |
| **工作流子节点** | 作为 workflow action 节点被编排器调度 | 现有 `WorkflowNodeExecution` |

---

## 2. 整体架构

### 2.1 子模块拆分

```
┌──────────────────────────────────────────────────────────────────────┐
│                        user-web (前端)                                 │
│  ┌─────────────────┐ ┌──────────┐ ┌────────────┐ ┌───────────────┐ │
│  │ 自动化枢纽 Hub   │ │ 任务编排  │ │ 工作流编辑  │ │ 执行监控+日志 │ │
│  │ LLM Playground  │ │ 原语步骤  │ │ 可视化DAG  │ │ 实时Trace     │ │
│  └─────────────────┘ └──────────┘ └────────────┘ └───────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
                              │ HTTP API
┌──────────────────────────────────────────────────────────────────────┐
│                     user-server (Go 后端)                              │
│                                                                      │
│  ┌─ 新域 browser_automation (五层架构) ──────────────────────────┐  │
│  │  Router → Controller → Service → Repository → Model           │  │
│  │     ↓            ↓                                        │  │
│  │              Service 内部：                                 │  │
│  │              ├─ Brain  (LLM Plan 生成)                    │  │
│  │              ├─ Translator (Plan → 原语序列)               │  │
│  │              ├─ Hand    (Native Messaging 客户端)          │  │
│  │              └─ Scheduler (循环/定时/工作流触发)           │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌─ 复用已有组件 ──────────────────────────────────────────────┐  │
│  │  pkg/cron          → 定时触发                                │  │
│  │  service/workflow_orchestrator → 工作流编排底座              │  │
│  │  aiagent/mcp/server → MCP 协议层（注册 browser_* tools）    │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
                              │ Native Messaging (stdio JSON)
┌──────────────────────────────────────────────────────────────────────┐
│                  Native Host (Python/Node) 本地进程                    │
│  stdin ←→ stdout JSON 协议，断线自动重连，多 Agent 消息队列           │
└──────────────────────────────────────────────────────────────────────┘
                              │ Native Messaging API
┌──────────────────────────────────────────────────────────────────────┐
│                Chrome 扩展 Manifest V3 (几百行 JS)                     │
│  权限: tabs + scripting，API: createBackgroundTab / click / type /     │
│  screenshot / getHTML / waitForLoad，维护标签页列表 + 消息路由         │
└──────────────────────────────────────────────────────────────────────┘
                              │
┌──────────────────────────────────────────────────────────────────────┐
│              用户主 Chrome Profile（完整 Cookie/登录态）                │
│              后台 tab 打开，不抢焦点                                    │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.2 与现有系统的集成点

| 现有组件 | 集成方式 | 改造量 |
|----------|----------|--------|
| `pkg/cron` | BrowserAutomationScheduler 注册定时浏览器任务 | 新增 ~100 行 |
| `workflow_orchestrator` | 新增 action 节点类型 `browser_task` | 新增 executor ~150 行 |
| `aiagent/mcp/server` | 注册 `browser_*` MCP tools | 新增 tools 注册 ~200 行 |
| FeatureFlag | 新增 `FF_BROWSER_AUTOMATION` 开关 | 1 行 config |
| `AutomationHub.vue` | 扩展为浏览器自动化入口页 | 改造 ~50% |

---

## 3. 数据库设计（5 张新表）

### 3.1 ER 图

```
browser_tasks 1─────N browser_sessions
      │                    │
      │                    N
      │              browser_steps
      │              (session执行明细)
      │
      N
browser_cron_triggers   (可选，定时触发)

browser_llm_plans  (可选，LLM 生成的计划存档)
```

### 3.2 browser_tasks — 任务定义

```sql
CREATE TABLE browser_tasks (
  id BIGSERIAL PRIMARY KEY,
  task_id VARCHAR(64) NOT NULL UNIQUE,        -- ULID
  name VARCHAR(200) NOT NULL,
  description TEXT,
  trigger_type VARCHAR(16) NOT NULL,          -- one_shot / cron / workflow
  -- 编排方式二选一：自然语言 OR 显式原语序列
  prompt TEXT,                                -- LLM 大脑吃的自然语言需求
  plan JSONB,                                 -- 显式原语序列（跳过 LLM）
  -- 浏览器配置
  target_url TEXT,                            -- 起始 URL（可选，prompt 中也可含）
  active_tab BOOLEAN DEFAULT false,           -- true=激活tab执行；默认false=后台
  -- 变量模板
  variables JSONB,                            -- 运行时变量模板 {{key}}
  -- 约束
  max_steps INT DEFAULT 50,                   -- 最多原语步骤，防失控
  timeout_sec INT DEFAULT 120,
  retry_on_step_fail BOOLEAN DEFAULT true,
  -- 状态
  status VARCHAR(16) NOT NULL DEFAULT 'draft', -- draft/active/paused/archived
  -- 关联 Chrome 扩展实例（多扩展场景）
  extension_instance_id VARCHAR(64),
  -- 可选：关联 workflow_orchestrator
  workflow_id VARCHAR(64),
  -- 审计
  created_by VARCHAR(64),
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_browser_tasks_status ON browser_tasks(status);
CREATE INDEX idx_browser_tasks_trigger ON browser_tasks(trigger_type);
```

### 3.3 browser_sessions — 执行会话（一次运行 = 一个 session）

```sql
CREATE TABLE browser_sessions (
  id BIGSERIAL PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL UNIQUE,
  task_id VARCHAR(64) NOT NULL REFERENCES browser_tasks(task_id),
  -- 触发来源
  trigger_source VARCHAR(16) NOT NULL,        -- manual / cron / workflow / api
  trigger_ref VARCHAR(64),                    -- cron_id / workflow_execution_id
  -- 上下文
  input_variables JSONB,                      -- 本次运行时变量
  llm_plan JSONB,                             -- 本次 LLM 生成的 plan 快照（若走大脑）
  -- 状态
  status VARCHAR(16) NOT NULL,                -- running/completed/failed/terminated
  current_step_index INT DEFAULT 0,
  -- 浏览器状态
  chrome_tab_id INT,                          -- Chrome 真实 tab id（调试用）
  chrome_window_id INT,
  -- 审计
  started_at TIMESTAMPTZ DEFAULT now(),
  finished_at TIMESTAMPTZ,
  duration_ms INT,
  error TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_browser_sessions_task ON browser_sessions(task_id);
CREATE INDEX idx_browser_sessions_status ON browser_sessions(status);
CREATE INDEX idx_browser_sessions_started ON browser_sessions(started_at DESC);
```

### 3.4 browser_steps — 原语步骤执行明细

```sql
CREATE TABLE browser_steps (
  id BIGSERIAL PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL REFERENCES browser_sessions(session_id),
  step_index INT NOT NULL,
  -- 原语类型
  action VARCHAR(32) NOT NULL,               -- open_tab / click / type / scroll / screenshot / get_html / wait / assert
  -- 原语参数（结构化 JSON）
  params JSONB NOT NULL,
  -- 结果
  input_snapshot JSONB,                       -- 执行前快照
  output_snapshot JSONB,                      -- 执行后快照（含 screenshot_path 等）
  status VARCHAR(16) NOT NULL,               -- running/completed/failed/skipped
  duration_ms INT,
  error TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  
  UNIQUE(session_id, step_index)
);

CREATE INDEX idx_browser_steps_session ON browser_steps(session_id);
```

### 3.5 browser_cron_triggers — 定时触发配置

```sql
CREATE TABLE browser_cron_triggers (
  id BIGSERIAL PRIMARY KEY,
  cron_id VARCHAR(64) NOT NULL UNIQUE,
  task_id VARCHAR(64) NOT NULL REFERENCES browser_tasks(task_id),
  expression VARCHAR(64) NOT NULL,            -- "0 9 * * *"
  variables JSONB,                           -- 定时触发时的变量覆盖
  enabled BOOLEAN DEFAULT true,
  last_run_at TIMESTAMPTZ,
  next_run_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_browser_cron_enabled ON browser_cron_triggers(enabled);
```

### 3.6 browser_llm_plans — LLM 计划缓存（可选）

```sql
CREATE TABLE browser_llm_plans (
  id BIGSERIAL PRIMARY KEY,
  plan_id VARCHAR(64) NOT NULL UNIQUE,
  -- 输入
  prompt_hash VARCHAR(64) NOT NULL,           -- SHA256(prompt + context_snapshot)
  prompt TEXT NOT NULL,
  context_snapshot JSONB,                     -- 当时的 DOM / 页面快照
  -- 输出
  plan JSONB NOT NULL,                        -- 原语序列
  model VARCHAR(64),
  tokens_used INT,
  -- 复用统计
  hit_count INT DEFAULT 0,
  created_at TIMESTAMPTZ DEFAULT now(),
  
  UNIQUE(prompt_hash)
);
```

> 缓存策略：相同 prompt + 相同页面结构 → 直接复用 plan，省 LLM 调用。

---

## 4. 原语 Schema（Hand 层 API 契约）

### 4.1 Plan Schema（大脑 → 翻译层）

```jsonc
{
  "$schema": "browser_plan_v1",
  "metadata": {
    "intent": "登录管理后台并抓取订单列表",
    "source_url": "https://admin.example.com",
    "llm_model": "qwen2.5-1.5b",
    "estimated_steps": 6
  },
  "steps": [
    {"action": "open_tab",    "params": {"url": "https://admin.example.com/login", "active": false}},
    {"action": "wait",         "params": {"load_state": "domcontentloaded"}},
    {"action": "type",        "params": {"selector": "#username", "value": "{{username}}"}},
    {"action": "type",        "params": {"selector": "#password", "value": "{{password}}"}},
    {"action": "click",       "params": {"selector": "button[type=submit]"}},
    {"action": "wait",        "params": {"element_visible": ".order-list"}},
    {"action": "screenshot",  "params": {"path": "orders.png"}},
    {"action": "extract",     "params": {"selector": ".order-item", "fields": ["order_id", "amount", "status"]}}
  ]
}
```

### 4.2 原语类型全集

| action | 必填 params | 可选 params | 说明 |
|--------|-------------|-------------|------|
| `open_tab` | `url` | `active`、`position` | 打开新标签页，默认后台 |
| `navigate` | `tab_id`, `url` | — | 在指定 tab 跳转 |
| `click` | `selector` | `wait_after_ms` | CSS selector 点击 |
| `type` | `selector`, `value` | `clear_first`, `submit_on_enter` | 输入，支持 `{{var}}` |
| `scroll` | `direction` | `amount`, `selector` | 滚动 |
| `screenshot` | — | `path`, `selector`, `full_page` | 截图存本地/OSS |
| `get_html` | — | `selector` | 获取页面 HTML |
| `extract` | `selector` | `fields` | 结构化提取 |
| `wait` | — | `load_state`, `element_visible`, `element_gone`, `ms` | 万能等待 |
| `assert` | `type`, `target`, `expected` | — | 断言失败触发 retry/fail |
| `evaluate` | `script` | — | 注入 JS 并执行 |
| `close_tab` | `tab_id` | — | 关闭 |
| `switch_tab` | `tab_id` | `activate` | 切换 tab |

### 4.3 翻译层职责

1. 变量替换：`{{username}}` → 运行时值
2. DOM 可用性检查（selector 合法性、页面是否就绪）
3. 超时/重试策略注入
4. 把 Plan 拆成 step 逐个执行，每步写入 `browser_steps`

---

## 5. LLM 大脑层设计

### 5.1 两种模式

| 模式 | 触发条件 | 流程 |
|------|----------|------|
| **显式 Plan 模式** | 用户直接写原语步骤（skill 模式） | 跳过 LLM → 直接 Hand |
| **LLM 驱动模式** | 任务只有 prompt（泛操作） | Brain → Translator → Hand |

### 5.2 Brain Prompt 模板

```
你是浏览器自动化规划器。用户给你一段自然语言需求，你需要输出一个浏览器原语执行计划。

【可用原语】
（枚举 4.2 的全部 action + params）

【当前页面上下文】
{context_snapshot: {url, title, main_selector_tree, forms_detected, buttons_detected}}

【用户需求】
{prompt}

【运行时变量】
{variables_schema: [{name, description}]}

请只输出 JSON（符合 browser_plan_v1 schema），不要解释。
```

### 5.3 反馈闭环（ReAct 风格）

```
步骤 N 执行完 → 抓取最新 DOM 快照 → 喂回 LLM → 判断下一步该干啥
```

- 单次执行最多 `max_steps`（默认 50）
- 每 5 步强制 LLM review（防止长链条偏离）
- 失败时允许 LLM 重新规划（retry_on_step_fail=true）

### 5.4 缓存命中流程

```
1. hash = SHA256(prompt + context_snapshot)
2. SELECT * FROM browser_llm_plans WHERE prompt_hash = ?
3. 命中 → plan hit_count++ → 直接 Hand
4. 未命中 → LLM 生成 → INSERT → Hand
```

---

## 6. 五层架构后端骨架

### 6.1 目录结构

```
user-server/internal/
├── model/browser_automation.go           ← 5 张表的 GORM 模型
├── repository/browser_automation.go      ← CRUD + 查询
├── service/
│   ├── browser_automation.go             ← 主编排服务（入口）
│   ├── browser_automation_brain.go       ← LLM Plan 生成
│   ├── browser_automation_translator.go  ← Plan → 原语序列 + 变量替换
│   ├── browser_automation_hand.go        ← Native Messaging 客户端（Hand）
│   └── browser_automation_scheduler.go   ← Cron + 循环 + 工作流触发
├── dto/browser_automation.go             ← 请求/响应 DTO
├── controller/browser_automation.go       ← HTTP handler
└── router/browser_automation_routes.go   ← 路由注册
```

### 6.2 路由设计

```
# 用户端
GET    /api/browser-automation/tasks
POST   /api/browser-automation/tasks
GET    /api/browser-automation/tasks/:task_id
PUT    /api/browser-automation/tasks/:task_id
DELETE /api/browser-automation/tasks/:task_id
POST   /api/browser-automation/tasks/:task_id/execute     ← 手动执行
GET    /api/browser-automation/tasks/:task_id/sessions

GET    /api/browser-automation/sessions/:session_id
GET    /api/browser-automation/sessions/:session_id/steps
POST   /api/browser-automation/sessions/:session_id/terminate

GET    /api/browser-automation/cron-triggers
POST   /api/browser-automation/cron-triggers
PUT    /api/browser-automation/cron-triggers/:cron_id
DELETE /api/browser-automation/cron-triggers/:cron_id

POST   /api/browser-automation/llm-playground            ← 快速生成 plan

# 管理端
GET    /api/manage/browser-automation/tasks
GET    /api/manage/browser-automation/sessions          ← 全量监控
GET    /api/manage/browser-automation/ext-health        ← 扩展实例健康
```

### 6.3 Service 关键方法签名

```go
// browser_automation.go
type BrowserAutomationService struct {
    taskRepo     *repository.BrowserTaskRepository
    sessionRepo  *repository.BrowserSessionRepository
    stepRepo     *repository.BrowserStepRepository
    cronRepo     *repository.BrowserCronRepository
    brain        *BrowserBrain
    translator   *BrowserTranslator
    hand         *BrowserHand           // Native Messaging 客户端
    scheduler    *BrowserScheduler
}

// 主入口：执行任务
func (s *BrowserAutomationService) ExecuteTask(ctx context.Context, taskID string, vars map[string]any) (*model.BrowserSession, error)

// 循环执行：对 datasource 遍历
func (s *BrowserAutomationService) ExecuteLoop(ctx context.Context, taskID string, datasource []map[string]any) ([]*model.BrowserSession, error)

// cron 触发入口（由 pkg/cron 回调）
func (s *BrowserAutomationService) OnCronFire(ctx context.Context, cronID string) error

// workflow 子节点入口
func (s *BrowserAutomationService) OnWorkflowAction(ctx context.Context, execID uint, nodeID string, params map[string]any) (map[string]any, error)
```

### 6.4 Brain 接口

```go
type BrowserBrain interface {
    GeneratePlan(ctx context.Context, prompt string, contextSnapshot map[string]any, varsSchema []VarDef) (*BrowserPlan, error)
    RePlan(ctx context.Context, prevPlan *BrowserPlan, currentDOM map[string]any, lastStepResult StepResult) (*BrowserPlan, error)
}
```

### 6.5 Hand 接口（Native Messaging 客户端）

```go
type BrowserHand interface {
    OpenBackgroundTab(ctx context.Context, url string, windowID int) (tabID int, err error)
    Click(ctx context.Context, tabID int, selector string) error
    Type(ctx context.Context, tabID int, selector string, value string) error
    Screenshot(ctx context.Context, tabID int) ([]byte, error)
    GetHTML(ctx context.Context, tabID int, selector string) (string, error)
    Wait(ctx context.Context, tabID int, cond WaitCondition) error
    Evaluate(ctx context.Context, tabID int, script string) (any, error)
    // 连接管理
    EnsureConnected(ctx context.Context) error
    Close() error
}
```

---

## 7. Native Messaging Chrome 扩展 + 本地 Host

### 7.1 Chrome 扩展（~300 行 JS）

```
chrome-extension/
├── manifest.json         ← manifest V3，tabs + scripting
├── background.js         ← Native Messaging 监听 + tabs API 封装
└── icons/
    ├── icon16.png
    └── icon48.png
```

**manifest.json 关键点**：
- `"permissions": ["tabs", "scripting"]` — 最小权限
- `"host_permissions": []` — 不需要全站点匹配
- `"background": {"service_worker": "background.js"}` — Manifest V3

**background.js 核心**：
```js
// 连接 Native Host
const port = chrome.runtime.connectNative('com.hivemtk.browser');
port.onMessage.addListener((msg) => handleCommand(msg));

async function handleCommand(cmd) {
  switch (cmd.action) {
    case 'open_tab':
      const tab = await chrome.tabs.create({url: cmd.url, active: false});
      return {ok: true, tab_id: tab.id};
    case 'click':
      await chrome.scripting.executeScript({
        target: {tabId: cmd.tab_id},
        func: (sel) => document.querySelector(sel).click(),
        args: [cmd.selector]
      });
      return {ok: true};
    // ... 其他原语
  }
}
```

### 7.2 Native Host（Python ~200 行）

```
native-host/
├── hivemtk_browser_host.py   ← stdio JSON 消息循环
├── install.sh                 ← 注册 Native Messaging 清单
└── manifest.json.template
```

**核心逻辑**：
- stdin/stdout JSON 行协议（Native Messaging 标准格式：4 字节大端长度 + JSON payload）
- 维持长连接，断线自动重连
- 多 Agent 并发队列（同一扩展实例同一时刻只处理一个命令）
- 把 Go 后端的 Hand 层命令翻译成扩展消息

### 7.3 安装（macOS）

```bash
# 1. 安装 Native Host
bash native-host/install.sh

# 2. Chrome 开发者模式加载扩展
#    chrome://extensions → 开发者模式 → 加载已解压扩展 → 选 chrome-extension/

# 3. 验证连接
#    扩展 icon 点击 → 显示 "HiveMTK Browser Connected"
```

### 7.4 Native Messaging 清单路径

macOS: `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.hivemtk.browser.json`

```json
{
  "name": "com.hivemtk.browser",
  "description": "HiveMTK Browser Automation Host",
  "path": "/Users/xxx/.local/bin/hivemtk_browser_host.py",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx/"]
}
```

---

## 8. MCP 协议注册

### 8.1 在 `aiagent/mcp/server.go` 注册 browser tools

```go
// 暴露给任何 MCP 客户端（Claude Code / Codex / 自研）
// 内部代理到 BrowserAutomationService

func (s *MCPServer) registerBrowserTools() {
    s.RegisterTool(mcp.Tool{
        Name: "browser_open_task",
        Description: "执行一个浏览器自动化任务（由 LLM 规划或显式步骤）",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "prompt":   map[string]any{"type": "string"},
                "plan":     map[string]any{"type": "object"},
                "variables":map[string]any{"type": "object"},
                "max_steps":map[string]any{"type": "integer"},
            },
        },
    }, s.handleBrowserOpenTask)

    s.RegisterTool(mcp.Tool{
        Name: "browser_list_tasks",
        Description: "列出所有浏览器自动化任务",
    }, s.handleBrowserListTasks)

    s.RegisterTool(mcp.Tool{
        Name: "browser_execution_log",
        Description: "查看某次执行的原语步骤日志",
        InputSchema: map[string]any{
            "session_id": map[string]any{"type": "string"},
        },
    }, s.handleBrowserExecutionLog)
}
```

### 8.2 MCP 调用流程

```
Agent → MCP tool call(browser_open_task)
     → user-server MCP handler
     → BrowserAutomationService.ExecuteTask(prompt=...)
     → Brain.GeneratePlan(prompt, context)  ← LLM 生成 plan
     → Translator.Validate(plan)
     → Hand.Execute(plan.steps[0])         ← Native Messaging → Chrome
     → 循环 step，写 browser_steps 表
     → 返回 session_id 给 Agent（可轮询状态）
```

---

## 9. 前端页面设计

### 9.1 页面清单

| 页面 | 路由 | 核心功能 |
|------|------|----------|
| 自动化枢纽 | `/system/automation-hub`（改造现有） | 浏览器自动化入口，任务统计，最近执行 |
| 任务编排器 | `/browser-automation/tasks/new` | 自然语言输入 OR 可视化原语步骤编辑 |
| 任务列表 | `/browser-automation/tasks` | 搜索/筛选/启停/删除 |
| 执行监控 | `/browser-automation/sessions/:session_id` | 实时 step 进度 + 截图缩略 + 错误定位 |
| Cron 配置 | `/browser-automation/cron` | 定时任务管理 |
| LLM Playground | `/browser-automation/playground` | 输入 prompt → 看 LLM 生成的 plan → 一键执行 |

### 9.2 任务编排器 UI

```
┌─────────────────────────────────────────────────┐
│ 浏览器自动化任务编排                                │
├─────────────────────────────────────────────────┤
│                                                   │
│ 【模式选择】  ○ LLM 驱动（写一句话）               │
│               ● 显式原语（拖拖拽）                  │
│                                                   │
│ 【LLM 驱动模式】                                   │
│ ┌─────────────────────────────────────────────┐ │
│ │ Prompt: 登录 admin.example.com               │ │
│ │   用账号 {{username}} 密码 {{password}}      │ │
│ │   进入订单页导出 CSV                          │ │
│ │                                              │ │
│ │ 变量定义:  username=admin  password=***      │ │
│ └─────────────────────────────────────────────┘ │
│ [生成 Plan]  → 预览原语步骤 → [执行] / [保存]      │
│                                                   │
│ 【显式原语模式】                                   │
│ ┌────────────┬────────────────────────────────┐  │
│ │ 原语工具箱  │ 步骤流                          │  │
│ │             │                                │  │
│ │ open_tab    │ ① open_tab → admin.example... │  │
│ │ click       │ ② wait                         │  │
│ │ type        │ ③ type #username {{username}} │  │
│ │ screenshot  │ ④ type #password {{password}} │  │
│ │ extract     │ ⑤ click submit                 │  │
│ │ ...         │ ⑥ wait .order-list             │  │
│ │             │ ⑦ extract .order-item          │  │
│ │             │ ⑧ screenshot orders.png        │  │
│ └────────────┴────────────────────────────────┘  │
│                                                   │
│ 【触发方式】                                       │
│  ○ 手动   ○ 定时 cron   ○ 工作流子节点              │
│                                                   │
│ 【执行配置】                                       │
│  最多步骤: [50]  超时: [120]s  失败重试: ✓         │
│                                                   │
│          [保存草稿]  [保存并执行]                    │
└─────────────────────────────────────────────────┘
```

### 9.3 执行监控页

```
┌──────────────────────────────────────────────────────┐
│ Session #sess_xxx  ● running  已执行 3/8 步 耗时 45s  │
├──────────────────────────────────────────────────────┤
│                                                        │
│ ┌─ Timeline ─────────┐  ┌─ 当前 Tab 状态 ─────────┐   │
│ │ ✅ open_tab   120ms │  │ URL: admin.example.com  │   │
│ │ ✅ wait       800ms │  │ Title: 后台 - 订单管理   │   │
│ │ ✅ type     1,200ms │  │                         │   │
│ │ ⏳ type     执行中  │  │ ┌─ 实时截图 ─────────┐ │   │
│ │ ○ click             │  │ │                    │ │   │
│ │ ○ wait              │  │ │                    │ │   │
│ │ ○ extract           │  │ │                    │ │   │
│ │ ○ screenshot        │  │ └────────────────────┘ │   │
│ └─────────────────────┘  └─────────────────────────┘   │
│                                                        │
│ ┌─ Step 3 详情 ─────────────────────────────────────┐ │
│ │ action: type                                        │ │
│ │ params: {selector: "#password", value: "********"} │ │
│ │ result: {ok: true, element_found: true}             │ │
│ └────────────────────────────────────────────────────┘ │
│                                                        │
│                          [终止执行]                     │
└──────────────────────────────────────────────────────┘
```

---

## 10. 调度引擎设计

### 10.1 循环任务

```go
// 伪代码：遍历 datasource，每条启动一个 session
for i, item := range datasource {
    vars := mergeTaskVars(task.Variables, item)  // item 覆盖同名 key
    session, err := svc.ExecuteTask(ctx, task.TaskID, vars)
    if err != nil {
        log.Errorf("loop iter %d failed: %v", i, err)
        if !task.RetryOnStepFail { continue }
    }
}
```

### 10.2 定时任务

```go
// 在 pkg/cron 扩展 BrowserCronAdapter
type BrowserCronAdapter struct {
    cron    *cron.Cron
    svc     *BrowserAutomationService
    cronRepo *repository.BrowserCronRepository
}

func (a *BrowserCronAdapter) LoadAll(ctx context.Context) error {
    triggers, _ := a.cronRepo.ListEnabled(ctx)
    for _, t := range triggers {
        a.cron.AddFunc(t.Expression, func() {
            a.svc.OnCronFire(ctx, t.CronID)
        })
    }
    return a.cron.Start()
}
```

### 10.3 工作流子节点

```go
// 在 workflow_node_executors.go 注册 browser_task 类型
registerExecutor("browser_task", func(ctx context.Context, node model.WorkflowNodeExecution, input map[string]any) (any, error) {
    taskID := node.NodeConfig["task_id"]
    vars   := node.NodeConfig["variables"]
    return browserSvc.OnWorkflowAction(ctx, node.ExecutionID, node.NodeID, map[string]any{
        "task_id": taskID, "variables": vars,
    })
})
```

---

## 11. FeatureFlag + 安全

### 11.1 FeatureFlag

| 开关 | 默认 | 说明 |
|------|------|------|
| `FF_BROWSER_AUTOMATION` | 0 | 总开关，关闭时所有 API 返回 disabled |
| `FF_BROWSER_LLM_PLAN_CACHE` | 1 | LLM plan 缓存 |
| `FF_BROWSER_REACT_LOOP` | 0 | ReAct 风格闭环（每 N 步 LLM review） |

### 11.2 安全边界

| 风险 | 对策 |
|------|------|
| 任务失控无限循环 | `max_steps` 硬限制 + `timeout_sec` |
| 敏感信息落 DB | `variables` 中 password 类字段自动 AES 加密存储 |
| 跨域自动登录窃取 Cookie | Native Messaging 权限已最小化；扩展不读写 Cookie |
| 多 Agent 并发冲突 | Hand 层单实例互斥队列 |
| LLM 注入风险 | Brain prompt 禁用直接 pass-through 用户输入为脚本；`evaluate` action 默认禁用 |

---

## 12. 实施优先级

### 阶段 1：MVP（可跑通一条链路）

1. DB Migration（5 张表）
2. Model + Repository
3. Hand（Native Messaging 客户端 + Chrome 扩展 + Python Host）
4. 后端 Service + Controller + Router（不含 LLM）
5. 前端任务编排（显式原语模式）+ 执行监控
6. 手动触发执行 end-to-end 跑通

### 阶段 2：LLM 大脑

7. Brain 层（GeneratePlan + ReAct 闭环）
8. Translator（Plan 校验 + 变量替换）
9. LLM Playground 页面
10. Plan 缓存

### 阶段 3：调度集成

11. Cron 定时触发 + 前端配置页
12. workflow_orchestrator 注册 browser_task executor
13. 循环任务（datasource 遍历）

### 阶段 4：生产加固

14. 多扩展实例管理（extension_instance_id 路由）
15. 执行日志 OSS 归档（截图/HTML 快照）
16. 健康检查（扩展心跳 + Native Host 状态）
17. 权限审计（谁在什么时候执行了什么任务）

---

## 13. 复用 vs 新建总结

| 能力 | 复用 | 新建 |
|------|------|------|
| 定时调度 | ✅ `pkg/cron` | BrowserCronAdapter（薄封装） |
| 工作流编排 | ✅ `service/workflow_orchestrator` | browser_task executor |
| MCP 协议 | ✅ `aiagent/mcp/server` | browser_* tools 注册 |
| 五层架构 | ✅ 现有分层模式 | 新域 browser_automation |
| DB 基础库 | ✅ GORM + 现有 migration 框架 | 5 张新表 |
| Chrome 扩展 | — | ✅ 新建 Manifest V3 |
| Native Host | — | ✅ 新建 Python 程序 |
| LLM Brain | — | ✅ 新建 + 复用现有 LLM routing |
| 前端框架 | ✅ Vue3 + ElementPlus + router | 新页面 6 个 |

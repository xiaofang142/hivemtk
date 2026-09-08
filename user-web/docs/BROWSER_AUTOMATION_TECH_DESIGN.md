# 浏览器自动化产品 — 完整技术方案

> 文档版本：v1.0 ｜ 2026-09-08 ｜ 手脑分离架构 · Native Messaging 路线 · LLM 驱动泛操作

---

## 0. 文档索引

| 章节 | 内容 | 决策者 |
|------|------|--------|
| **1 需求源** | 原始需求整理、硬约束、产品边界 | PM + 技术负责人 |
| **2 业界对比** | 9 梯队对比矩阵、关键方案分析 | 技术负责人 |
| **3 技术选型** | Native Messaging vs CDP 论证 | 架构师 |
| **4 架构设计** | 分层、集成点、数据流 | 架构师 |
| **5 数据库** | 5 张新表 ER + DDL | DBA |
| **6 原语 Schema** | Plan Schema + 13 种原语契约 | 前后端 + Chrome 扩展开发 |
| **7 LLM 大脑** | Brain 设计 + ReAct 闭环 + 缓存 | AI 工程师 |
| **8 Native Messaging** | Chrome 扩展 + Native Messaging Host (Go) 设计 | 前端 + Go 后端 |
| **9 调度引擎** | Cron + 循环 + 工作流子节点 | 后端 |
| **10 MCP 注册** | browser_* tools + Agent 调用流 | 后端 + Agent 工程 |
| **11 前端** | 6 个页面设计 + 编排器 UI | 前端 |
| **12 安全 + FeatureFlag** | 边界 + 开关 + 审计 | 安全负责人 |
| **13 风险评估** | 10 个风险点 + 对策 | 全员 |
| **14 实施路线** | 4 阶段 + 里程碑 | 项目经理 |
| **附录 A** | 复用 vs 新建清单 | 架构师 |
| **附录 B** | 已有相关文档索引 | — |

---

## 1. 需求源整理

### 1.1 用户原始需求浓缩

| 维度 | 内容 |
|------|------|
| **产品定位** | 浏览器自动化产品，文档/代码/前端/后端均需独立子模块 |
| **三模块协作** | ① user-web 编排自动化需求/任务/工作流；② Chrome MCP 浏览器自动化方案调度执行；③ 监控调度执行结果 |
| **LLM 定位** | 借助 LLM 实现**泛操作**而非精细操作，降低上手难度 |
| **手脑分离** | LLM 大脑（决策）和浏览器原语手（执行）分开 |
| **三种触发** | 循环任务、定时任务、工作流任务 |
| **核心硬约束** | 复用主 Chrome Profile、后台 tab 不抢焦点、免授权弹窗、多 Agent 共享会话 |

### 1.2 五条硬约束（不可妥协）

> 源自 CHROME_MCP_BROWSER_AUTOMATION.md 的约束，与业界调研交叉验证后确认。

| # | 约束 | 为什么 |
|---|------|--------|
| 1 | **复用主 Chrome Profile** | 私域部署场景，企业内网 SSO/证书/登录态必须继承，Playwright 隔离浏览器直接不可用 |
| 2 | **后台 tab 不抢焦点** | 不打断用户正常浏览，Chrome DevTools `activate()` 行为不可控 |
| 3 | **重启 Chrome 后自动连接、无弹窗** | CDP 远程调试每次重启必弹 "Allow"，无法绕过（Chrome 安全限制） |
| 4 | **多 Agent 共享会话** | Claude Code / Codex / 自研 Agent 需共用一个浏览器实例，互不冲突 |
| 5 | **不是无头、不搞隔离浏览器** | 目标是寄生式体验，接近 ChatGPT Chrome 扩展那种 "在你正在用的浏览器里工作" |

### 1.3 产品边界

| ✅ 在范围内 | ❌ 不在范围内 |
|------------|--------------|
| LLM 驱动泛操作（自然语言 → 原语序列） | Playwright/Puppeteer 级别的精细 selector 自动化（太脆） |
| 复用主 Chrome Profile 后台 tab | 启动独立浏览器实例 |
| 定时 / 循环 / 工作流子节点触发 | 分布式浏览器集群（单租户场景不需要） |
| MCP 协议暴露给外部 Agent | 自建大模型推理（复用现有 LLM routing） |
| 执行监控 + 截图 + Step 日志 | 浏览器反检测 / CAPTCHA 自动破解（Skyvern 路线） |

### 1.4 现有项目底座

调研发现项目已有 3 个关键底座可复用：

| 现有组件 | 文件位置 | 成熟度 | 本方案集成方式 |
|----------|----------|--------|----------------|
| `pkg/cron` | `user-server/internal/pkg/cron/cron.go` | ✅ 160 行，robfig/cron/v3 | BrowserCronAdapter 薄封装 |
| `workflow_orchestrator` | `user-server/internal/service/workflow_orchestrator.go` + `workflow_node_executors.go` | ✅ 326 + 359 行 | 新增 `browser_task` executor |
| `aiagent/mcp/server` | `user-server/internal/aiagent/mcp/server.go` | ✅ 435 行 | 注册 3 个 `browser_*` MCP tools |

结论：**新域 browser_automation 是增量开发，不需要推翻任何现有模块**。

---

## 2. 业界同类对比

### 2.1 浏览器自动化 9 梯队（2026 Q3 格局）

基于 HugoSkills / ChatForest / Skyvern 多份调研报告整理：

| 梯队 | 代表 | 架构 | AI 友好 | 复用主 Profile | 成熟度 | 适合场景 |
|------|------|------|---------|----------------|--------|----------|
| **1** | Playwright | Code + Selector | ❌ | ❌ 隔离浏览器 | ⭐⭐⭐⭐⭐ | 测试/CI，不适合 Agent |
| **2** | CDP | 远程调试协议 | ⚠️ 需封装 | ✅ 可连主浏览器 | ⭐⭐⭐⭐ | 协议层，弹窗枷锁 |
| **3** | Puppeteer | Google 出品 | ⚠️ | ❌ | ⭐⭐⭐⭐ | 新项目已选 Playwright |
| **4** | Browser Use (Agent-Use) | LLM Planner + Playwright | ✅ Agent Loop | ❌ 隔离浏览器 | ⭐⭐⭐ | Demo 惊艳，生产待打磨 |
| **5** | Stagehand | Playwright for AI，自然语言原语 | ✅ `act()`/`extract()` | ❌ 隔离浏览器 | ⭐⭐⭐⭐ | AI 原语但底层 Playwright |
| **6** | Skyvern | Vision + Playwright 混合 | ✅ 视觉理解 + Playwright 兜底 | ❌ 隔离浏览器 | ⭐⭐⭐⭐ | 复杂 Portal 场景，CAPTCHA |
| **7** | **MCP Browser DevTools (CDP 版)** | MCP + CDP WebSocket | ✅ 协议标准 | ✅ 可连 | ⭐⭐⭐ | 每次重连弹 Allow |
| **8** | **Native Messaging 路线** | Chrome 扩展 + stdio 通信 | ✅ 可暴露 MCP | ✅ 主 Profile | ⭐⭐ | 稀缺但方向正确 |
| **9** | 我们的方案 | 方案 A 自建极简网关 | ✅ 大脑/手分离 | ✅ 主 Profile | 🆕 自建 | **全部硬约束命中** |

### 2.2 关键方案横向对比

| 维度 | Playwright MCP | Stagehand | Browser Use | Skyvern | **Browser Agent Bridge** (参考) | **本方案** |
|------|---------------|-----------|-------------|---------|-------------------------------|-----------|
| 底层浏览器 | Playwright | Playwright | Playwright | Playwright | Chrome 扩展 + Native | Chrome 扩展 + Native |
| 复用主 Profile | ❌ 新实例 | ❌ | ❌ | ❌ | ✅ 主 Profile | ✅ 主 Profile |
| 后台 tab | ⚠️ 抢焦点 | ❌ 新浏览器 | ❌ | ❌ | ✅ `active:false` | ✅ `active:false` |
| 重启弹窗 | ❌ | ❌ | ❌ | ❌ | ✅ 无 | ✅ 无 |
| AI 驱动 | ✅ snapshot | ✅ `act()` | ✅ Agent Loop | ✅ Vision | ❌ 只暴露原语 | ✅ 大脑/手分离 |
| MCP 协议 | ✅ | ✅ | ✅ | ✅ | ⚠️ HTTP/WS JSON-RPC | ✅ 原生 MCP |
| 开源 | ✅ MIT | ✅ | ✅ | ✅ 商用 | ✅ MIT | ✅ 自建 |
| Stars | 36K | 23K | 108K | 13.6K | — | 🆕 |

### 2.3 关键发现

1. **没有任何成品项目完美命中全部硬约束**。业界热门方案（Playwright MCP / Stagehand / Browser Use / Skyvern）全部走 Playwright 隔离浏览器，**根本不尝试复用主 Profile**。
2. **Browser Agent Bridge**（https://github.com/TNJ2026/browser-agent-bridge）是最接近的参考实现——Native Messaging + Chrome 扩展 + Native Messaging Host (Go) + HTTP/WS JSON-RPC，**但它不做 LLM 大脑层**，只暴露原语。我们可以直接借鉴其 Native Messaging 通信协议和 Host 实现。
3. **CDP 路线的弹窗枷锁无解**。Chrome 的远程调试协议对外部调试器（任何走 `--remote-debugging-port` 的方案）有明确的用户确认保护，重启后首次连接必弹 "Allow"。这不是代码 bug，是 Chrome 安全模型。
4. **2025-2026 年业界范式转向 AI-First Browser Automation**（LLM 驱动 DOM 理解 + 原语执行），但几乎所有方案都在隔离浏览器里跑。我们的差异化在于：**把 AI-First 能力寄生到用户正在用的浏览器里**。

---

## 3. 技术选型论证

### 3.1 核心决策

> **选择：自建极简 Native Messaging MCP 网关（方案 A），不 fork 现有大项目。**

### 3.2 三条候选路线对比

| 路线 | 实现 | 优点 | 缺点 | 决策 |
|------|------|------|------|------|
| **方案 A：自建极简网关** | 剥离 Native Messaging 最小骨架，自己维护 | 全部硬约束命中、源码可控、代码量小（~500 行核心） | 需要少量 JS + Go | ✅ **选这个** |
| **方案 B：改造 Browser Agent Bridge** | fork 后替换 MCP 层，加 Brain 层 | 省掉扩展 + Host 编码 | 上游更新需 rebase，架构不完全契合（它用 HTTP/WS 而非直接 MCP） | ❌ 架构不契合 |
| **方案 C：继续用 CDP 路线 + 忍弹窗** | Chrome DevTools MCP / Playwright MCP + CDP 连接 | 零编码 | 重启必弹 Allow、抢焦点、硬约束全破 | ❌ 硬约束不满足 |

### 3.3 Native Messaging 为什么能解决弹窗问题

```
CDP 路线：
  Chrome (外部调试器) → 安全模型认为是外部 → 每次弹 Allow

Native Messaging + Chrome 扩展路线：
  扩展是浏览器内部组件 → 安装时一次性授予权限 → allowed_origins 白名单 → 永久有效
  Chrome 启动时自动加载扩展 → 扩展自动连 Native Host → 零交互
```

Chrome 官方文档确认：Native Messaging host 是通过 manifest 的 `allowed_origins` 白名单来授权，**一次安装永久有效，不涉及运行时弹窗**（https://developer.chrome.com/extensions/nativeMessaging）。

### 3.4 技术栈决策

| 组件 | 选型 | 理由 |
|------|------|------|
| Chrome 扩展 | Manifest V3 + 纯 JS | Chrome 官方标准，service worker 模型稳定；几百行足够 |
| Native Host | Go | 跨平台；stdio 消息循环实现简单；browser-agent-bridge 已验证 |
| Go Hand 层 | Go + `encoding/json` + 自定义 stdio | 后端本就是 Go，Native Messaging 协议（4 字节长度 + JSON payload）可直接实现 |
| LLM 大脑 | 复用现有 `aiagent/llm` + `llm_routing` | 不新建 LLM 基础设施，只写 prompt + plan 解析 |
| Plan 缓存 | PostgreSQL JSONB + SHA256 hash | 项目已有 PG 基础设施，不需要 Redis |

---

## 4. 完整架构设计

### 4.1 手脑分离三图层

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Brain (LLM 决策层)                                │
│                                                             │
│  输入: 自然语言 prompt + accessibility snapshot (@ref) + 变量 │
│  输出: browser_plan_v1 JSON (结构化原语序列)                 │
│  职责: 理解意图 + 生成计划 + ReAct 闭环 + 计划缓存           │
│  关键: snapshot 是 @e1/@e2 refs 表 (2-5 KB)，不是原始 HTML   │
│  不做: 不直接操作浏览器、不写脚本                             │
└─────────────────────────────────────────────────────────────┘
                              │ browser_plan_v1
┌─────────────────────────────────────────────────────────────┐
│  Layer 2: Translator (翻译/校验层)                           │
│                                                             │
│  输入: browser_plan_v1 + 运行时变量值                       │
│  输出: 逐原语执行指令 + 写 browser_steps 表                  │
│  职责: 变量替换 {{var}}、ref→DOM 元素定位、超时/重试注入      │
│  不做: 不调 LLM、不直接操作浏览器                             │
└─────────────────────────────────────────────────────────────┘
                              │ 逐原语指令
┌─────────────────────────────────────────────────────────────┐
│  Layer 3: Hand (浏览器原语执行层)                            │
│                                                             │
│  Go Hand (Go NM Host) ──(Native Messaging stdio)──▶ Chrome 扩展│
│                                                             │
│  Go 直接实现 Chrome Native Messaging 协议（小端 4B 长度帧） │
│  event_loop 模式：for { readFrame → dispatch → writeFrame } │
│                                                             │
│  输入: 单条原语 (action + params)                            │
│  输出: 执行结果 (ok/error + 快照/截图)                       │
│  职责: 维持长连接、多 Agent 互斥队列 (mutex)、断线重连       │
│  不做: 不理解业务逻辑、不调用 LLM                             │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  Chrome 扩展 (Manifest V3)                                 │
│                                                             │
│  权限: tabs + scripting （最小权限，不读写 Cookie）          │
│  API: createBackgroundTab / click / type / snapshot(@ref)  │
│       screenshot / markdown / getHTML / wait / scroll /    │
│       extract / assert / evaluate / close / switch_tab     │
│                                                             │
│  寄生在用户主 Chrome Profile，后台 tab (active:false) 不抢焦点│
│  Native Messaging port 天然保活 service worker（不需要 offscreen）│
└─────────────────────────────────────────────────────────────┘
```

### 4.2 子模块拆分

```
┌──────────────────────────────────────────────────────────────────┐
│                        user-web (前端)                            │
│                                                                  │
│  /browser-automation/tasks          任务列表                     │
│  /browser-automation/tasks/new      任务编排器 (LLM/原语双模式)   │
│  /browser-automation/sessions/:id   执行监控 (实时 step + 截图)    │
│  /browser-automation/cron           定时配置                     │
│  /browser-automation/playground     LLM Playground (prompt→plan) │
│  /system/automation-hub             自动化枢纽 (改造现有)         │
└──────────────────────────────────────────────────────────────────┘
                              │ HTTP API
┌──────────────────────────────────────────────────────────────────┐
│                     user-server (Go 后端)                          │
│                                                                  │
│  新域 browser_automation (五层架构)                                │
│  ┌ Router ──────────────────────────────────────────────────────┐│
│  │ browser_automation_routes.go                                  ││
│  ├ Controller ─────────────────────────────────────────────────┤│
│  │ browser_automation.go (HTTP handler)                          ││
│  ├ Service ─────────────────────────────────────────────────────┤│
│  │ browser_automation.go             主编排入口                   ││
│  │ browser_automation_brain.go       LLM Plan 生成                ││
│  │ browser_automation_translator.go  Plan → 原语 + 变量替换        ││
│  │ browser_automation_hand.go        Native Messaging 客户端       ││
│  │ browser_automation_scheduler.go   Cron/循环/工作流触发          ││
│  ├ Repository ──────────────────────────────────────────────────┤│
│  │ browser_automation.go (5 张表 CRUD)                           ││
│  ├ Model ────────────────────────────────────────────────────────┤│
│  │ browser_automation.go (5 表 GORM 模型 + 常量)                  ││
│  └ DTO ─────────────────────────────────────────────────────────┘│
│    browser_automation.go (请求/响应)                               │
│                                                                  │
│  复用已有：pkg/cron / workflow_orchestrator / aiagent/mcp/server  │
└──────────────────────────────────────────────────────────────────┘
                              │ Native Messaging (4 字节长度 + JSON)
┌──────────────────────────────────────────────────────────────────┐
│              Go Native Messaging Host (~80 行)                    │
│  4 字节 Little Endian 长度头 + UTF-8 JSON                        │
│  event_loop 模式，Chrome 管理生命周期                              │
│  位置:  ~/Library/Application Support/... NativeMessagingHosts/  │
│  关键: Go 直接实现，省掉 Python 中间层（见深度调研 §1）            │
└──────────────────────────────────────────────────────────────────┘
                              │ Native Messaging API
┌──────────────────────────────────────────────────────────────────┐
│            Chrome 扩展 Manifest V3 (~300 行 JS)                    │
│  chrome.tabs.create({active:false}) 后台 tab                     │
│  chrome.scripting.executeScript()   注入原语执行                 │
│  维护 tab 列表 + 消息路由                                         │
│  不读写 Cookie / Passwords 权限                                   │
└──────────────────────────────────────────────────────────────────┘
                              │
┌──────────────────────────────────────────────────────────────────┐
│          用户主 Chrome Profile (完整 Cookie/登录态)                │
│          后台 tab 打开，active:false，不抢焦点                     │
└──────────────────────────────────────────────────────────────────┘
```

### 4.3 与现有系统的集成清单

| 现有组件 | 集成点 | 改造文件 | 改造量 |
|----------|--------|----------|--------|
| `pkg/cron` | BrowserCronAdapter 注册定时浏览器任务 | 新建 `browser_automation_scheduler.go` | ~100 行 |
| `workflow_orchestrator` | 新增 action 节点 `browser_task` | 改造 `workflow_node_executors.go` | ~150 行 |
| `aiagent/mcp/server` | 注册 `browser_*` MCP tools | 改造 `server.go` 或新建 `browser_tools.go` | .~80 行 |
| FeatureFlag | 新增 `FF_BROWSER_AUTOMATION` 等开关 | `system_config` | 3 行 config |
| `AutomationHub.vue` | 扩展为浏览器自动化入口 | 改造现有 | ~50% |

### 4.4 数据流（手动执行场景）

```
1. 用户在前端点击 [执行]
   POST /api/browser-automation/tasks/:task_id/execute  {variables: {...}}

2. Controller → Service.ExecuteTask()

3. Service → Brain.GeneratePlan()（如果 task 只有 prompt）
   3a. 先查 browser_llm_plans 缓存 (SHA256 hash)
   3b. 命中 → 直接用
   3c. 未命中 → 调 LLM routing → 生成 browser_plan_v1 JSON → 存缓存

4. Service → Translator.Execute(plan, vars)
   4a. 变量替换 {{username}} → "admin"
   4b. 逐原语循环：
       - Hand.SendCommand(action, params)
       - Chrome 扩展执行 → 返回结果
       - 写 browser_steps 表 (status=completed/failed, duration_ms)
   4c. 每 5 步或失败时 ReAct review（若启用 FF_BROWSER_REACT_LOOP）

5. Service → 写 browser_sessions 表 (status=completed, duration_ms)

6. 前端轮询 GET /api/browser-automation/sessions/:session_id/steps
   实时展示 Timeline + 截图缩略
```

---

## 5. 数据库设计（5 张新表）

### 5.1 ER 图

```
browser_tasks ──1:N──▶ browser_sessions ──1:N──▶ browser_steps
      │                                          (session 执行明细)
      │
      └──1:N──▶ browser_cron_triggers           (定时触发配置，可选)

browser_llm_plans                               (LLM 计划缓存，独立)
```

### 5.2 表结构

见 `docs/BROWSER_AUTOMATION_PRODUCT_DESIGN.md` 第 3 章完整 DDL，此处不再重复。

关键字段：
- `browser_tasks`: `prompt` OR `plan` 二选一，`trigger_type` 标记触发方式，`max_steps` 硬限制防失控
- `browser_sessions`: `llm_plan` 快照记录本次 LLM 计划，`chrome_tab_id` 调试用
- `browser_steps`: `input_snapshot` / `output_snapshot` 存执行前后 DOM 片段和结果
- `browser_cron_triggers`: `expression` 标准 cron 5 字段格式，`next_run_at` 便于调度器预加载
- `browser_llm_plans`: `prompt_hash` (SHA256) 唯一索引，命中即复用 plan

---

## 6. 原语 Schema（Hand 层 API 契约）

### 6.1 browser_plan_v1 Schema（Brain → Translator）

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
    {"action": "open_tab", "params": {"url": "...", "active": false}},
    {"action": "wait",      "params": {"load_state": "domcontentloaded"}},
    {"action": "type",      "params": {"selector": "#username", "value": "{{username}}"}},
    {"action": "type",      "params": {"selector": "#password", "value": "{{password}}"}},
    {"action": "click",     "params": {"selector": "button[type=submit]"}},
    {"action": "wait",      "params": {"element_visible": ".order-list"}},
    {"action": "screenshot","params": {"path": "orders.png"}},
    {"action": "extract",   "params": {"selector": ".order-item", "fields": ["order_id","amount"]}}
  ]
}
```

### 6.2 13 种原语全集

| action | 必填 params | 可选 params | 说明 | Chrome API 映射 |
|--------|-------------|-------------|------|-----------------|
| `open_tab` | `url` | `active`、`position` | 打开新标签页，默认后台 | `chrome.tabs.create({active:false})` |
| `navigate` | `url` | `tab_id` | 跳转（缺省用第一 tab） | `chrome.tabs.update()` |
| `click` | `selector` | `wait_after_ms` | CSS selector 点击 | `chrome.scripting.executeScript()` |
| `type` | `selector`, `value` | `clear_first`, `submit_on_enter` | 输入，`{{var}}` 运行时替换 | `chrome.scripting.executeScript()` |
| `scroll` | `direction` | `amount`, `selector` | 滚动 | `chrome.scripting.executeScript()` |
| `screenshot` | — | `path`, `selector`, `full_page` | 截图 | `chrome.tabs.captureVisibleTab()` |
| `get_html` | — | `selector` | 获取 HTML | `chrome.scripting.executeScript()` |
| `extract` | `selector` | `fields` | 结构化提取 | `chrome.scripting.executeScript()` |
| `wait` | — | `load_state`, `element_visible`, `element_gone`, `ms` | 万能等待 | Content script poll |
| `assert` | `type`, `target`, `expected` | — | 断言，失败触发 retry/fail | Content script |
| `evaluate` | `script` | — | 注入 JS 并执行 | `chrome.scripting.executeScript()` |
| `close_tab` | `tab_id` | — | 关闭 | `chrome.tabs.remove()` |
| `switch_tab` | `tab_id` | `activate` | 切换 | `chrome.tabs.update()` |

### 6.3 Native Messaging 协议帧

```
┌───────────────────────────────┐
│ 4 字节小端长度 (uint32 LE)     │
├───────────────────────────────┤
│ JSON payload (长度字节)        │
│ {                             │
│   "id": "req_123",            │
│   "action": "click",          │
│   "tab_id": 42,               │
│   "params": {"selector":...}  │
│ }                             │
└───────────────────────────────┘
```

Chrome Native Messaging 标准格式，Go/扩展三方统一。

---

## 7. LLM 大脑层设计

### 7.1 两种模式

| 模式 | 触发条件 | 流程 | 适用场景 |
|------|----------|------|----------|
| **显式 Plan 模式** | 用户直接写原语步骤 | 跳过 LLM → 直接 Hand | 稳定流程、精确控制 |
| **LLM 驱动模式** | 任务只有 prompt | Brain → Translator → Hand | 泛操作、快速上手 |

### 7.2 Brain 职责

```go
type BrowserBrain interface {
    GeneratePlan(ctx context.Context,
        prompt string,
        contextSnapshot map[string]any,  // {url, title, selector_tree, forms_detected}
        varsSchema []VarDef,             // [{name, description}]
    ) (*BrowserPlan, error)

    RePlan(ctx context.Context,
        prevPlan *BrowserPlan,
        currentDOM map[string]any,
        lastStepResult StepResult,
    ) (*BrowserPlan, error)
}
```

### 7.3 Brain Prompt 模板

```
你是浏览器自动化规划器。用户给你一段自然语言需求，你需要输出一个 browser_plan_v1 JSON。

【可用原语】
open_tab / navigate / click / type / scroll / screenshot / get_html / extract / wait / assert / evaluate / close_tab / switch_tab
每种原语的 params 详见用户提供的 plan schema。

【当前页面上下文】
{url: "{{url}}", title: "{{title}}", interactive_elements: {{selector_tree}}, forms: {{forms_detected}}}

【用户需求】
{{prompt}}

【运行时变量定义】
{{variables_schema}}

【约束】
- 先 open_tab 打开目标 URL（或用 navigate 跳转现有 tab）
- 每步之间按需 wait，不要硬编码固定 sleep
- max_steps 不超过 {{max_steps}}
- 必须输出合法 JSON，不要解释文字

请只输出 JSON。
```

### 7.4 ReAct 闭环

```
Step 0: Brain.GeneratePlan(prompt, 初始页面快照)
Step 1..N: Translator 执行原语
         ├── 每 5 步 → Brain.RePlan(当前 plan, 最新 DOM, 最后一步结果)
         │   → 判断 plan 是否偏离 → 必要时重新规划剩余步骤
         └── 某步失败 → retry_on_step_fail=true 时 → Brain.RePlan(error context) → 新 plan
```

### 7.5 计划缓存

```
1. hash = SHA256(prompt + JSON.stringify(context_snapshot))
2. SELECT * FROM browser_llm_plans WHERE prompt_hash = hash
3. 命中 → UPDATE hit_count = hit_count + 1 → 返回缓存 plan
4. 未命中 → LLM 生成 → INSERT → 返回新 plan
```

缓存失效条件：用户手动点击 "Regenerate" 或 Brain 检测到 context 差异超过阈值。

---

## 8. Native Messaging 子系统

### 8.1 组件关系（**深度调研修正：Go 直做，无 Python**）

```
Go Hand (browser_automation_hand.go)
  │
  │ exec.Command("hivemtk_browser_nm_host")
  │ ── 启动 Go Native Messaging Host 子进程 ──
  │   │
  │   │ stdio pipe（进程内通信）
  │   │
  │   ▼
  │ Go NM Host (~80 行)
  │ 4 字节 Little Endian 长度帧 + UTF-8 JSON
  │ event_loop: for { readFrame → dispatch → writeFrame }
  │
  │   │ Chrome 管理的 stdio 通道（Native Messaging）
  │   ▼
Chrome MV3 扩展 ──▶ 用户主 Chrome Profile
```

**为什么不需要 Python**：
- Go 可以直接实现 Chrome Native Messaging Host — 已验证成熟库 `github.com/rickypc/native-messaging-host`
- 协议实现 ~60 行：读 4 字节 LE 长度头 + JSON，写 response
- Go binary 单文件部署，比 Python + 依赖包方便
- 架构层数减 1（Go → Extension，不是 Go → Python → Extension）

### 8.2 Chrome 扩展（~300 行 JS）

```
chrome-extension/
├── manifest.json         ← MV3，tabs + scripting + optional(offscreen)
├── background.js         ← Native Messaging 监听 + tabs API 封装 + accessibility snapshot
└── icons/
```

**manifest.json 关键点**：
- `"manifest_version": 3`
- `"permissions": ["tabs", "scripting"]` — 最小权限（不读 Cookie）
- `"host_permissions": []` — scripting.executeScript 可注入任何 tab
- `"background": {"service_worker": "background.js"}`
- Native Messaging 连接：`chrome.runtime.connectNative('com.hivemtk.browser')`

**background.js 核心**：
```js
// 1. 连接 Go Native Messaging Host
let port = chrome.runtime.connectNative('com.hivemtk.browser');
port.onMessage.addListener(handleCommand);
port.onDisconnect.addListener(() => {
  console.warn('NM Host disconnected, Go Hand will retry');
});
// port 保活 service worker — 不需要 offscreen document

// 2. handleCommand 分发
async function handleCommand(cmd) {
  switch (cmd.action) {
    case 'open_tab':
      return await chrome.tabs.create({url: cmd.url, active: cmd.active ?? false});
    case 'click':
      return await execScript(cmd.tab_id, `(sel) => document.querySelector(sel).click()`, [cmd.selector]);
    case 'snapshot':  // accessibility snapshot，生成 @e1/@e2 refs
      return await execScript(cmd.tab_id, snapshotScript);
    case 'markdown':  // 页面转 Markdown（给 LLM 吃）
      return await execScript(cmd.tab_id, markdownScript);
    // ... 其他原语
  }
}
```

### 8.3 Go Native Messaging Host（~80 行，event_loop 模式）

```go
// main.go — Chrome Native Messaging Host
// 编译: go build -o hivemtk_browser_nm_host .

package main

import (
    "encoding/binary"
    "encoding/json"
    "io"
    "os"
)

func main() {
    for {
        // 1. 读 4 字节 Little Endian 长度头
        var length uint32
        if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
            if err == io.EOF {
                return // Chrome 关闭通道，进程退出（Chrome 管理生命周期）
            }
            return
        }
        if length > 1*1024*1024 { // Chrome 限制 Host→扩展 1 MiB
            return
        }

        // 2. 读消息体
        buf := make([]byte, length)
        if _, err := io.ReadFull(os.Stdin, buf); err != nil {
            return
        }

        // 3. 解析 JSON 并处理
        var msg map[string]any
        if err := json.Unmarshal(buf, &msg); err != nil {
            writeFrame(map[string]any{"ok": false, "error": "invalid_json"})
            continue
        }
        response := dispatch(msg)  // 由 Go Hand 层业务逻辑注入

        // 4. 写 response
        writeFrame(response)
    }
}

func writeFrame(resp map[string]any) {
    payload, _ := json.Marshal(resp)
    binary.Write(os.Stdout, binary.LittleEndian, uint32(len(payload)))
    os.Stdout.Write(payload)
    os.Stdout.Flush()
}
```

**协议帧精确规范**：
- 4 字节 **Little Endian** uint32 长度头（之前误写为大端，已修正）
- 长度 = 后续 JSON payload 的字节数
- 扩展→Host 最大 64 MiB；Host→扩展 最大 1 MiB（Chrome 硬限制）
- **绝对不能往 stdout 写非帧数据**（如 println）——Chrome 会解析失败
- 日志走 stderr 或文件

**Chrome 生命周期管理**：
- 扩展 `connectNative()` → Chrome fork Host 进程 → Host 开始读 stdin
- 扩展 port 断开 / Chrome 重启 → Chrome 发送 EOF 关闭 Host 进程
- Go Host 不要自己 daemonize，让 Chrome 管理
- 崩溃后 Chrome 不自动重启，需要 Go Hand 层 `EnsureConnected()` 重试

### 8.4 Go Hand ↔ Go NM Host 通信

Go Hand 通过 `exec.Command` 启动 NM Host，用 `Cmd.Stdin` / `Cmd.Stdout` pipe：

```go
type BrowserHand struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.ReadCloser
    mutex  sync.Mutex  // 多 Agent 并发保护
    hostMu sync.Mutex  // Host 单实例保护
}

func NewBrowserHand() (*BrowserHand, error) {
    b := &BrowserHand{}
    if err := b.ensureHost(); err != nil {
        return nil, err
    }
    return b, nil
}

func (b *BrowserHand) ensureHost() error {
    b.hostMu.Lock()
    defer b.hostMu.Unlock()
    if b.cmd != nil {
        return nil  // 已启动
    }
    b.cmd = exec.Command("hivemtk_browser_nm_host")
    b.cmd.Stdin, _ = b.cmd.StdinPipe()
    b.cmd.Stdout, _ = b.cmd.StdoutPipe()
    b.cmd.Stderr = os.Stderr  // 日志走 stderr
    return b.cmd.Start()
}

func (b *BrowserHand) Click(tabID int, refOrSelector string) error {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    // ... 组装 frame → 写 stdin → 读 response
    return b.send(map[string]any{
        "action": "click",
        "tab_id": tabID,
        "target": refOrSelector,
    })
}
```

### 8.5 安装步骤

```bash
# macOS

# 1. 编译 Go Native Messaging Host
cd chrome-extension/nm-host/
go build -o ~/.local/bin/hivemtk_browser_nm_host .

# 2. 确保有执行权限 + 创建 manifest
chmod +x ~/.local/bin/hivemtk_browser_nm_host

mkdir -p ~/Library/Application\ Support/Google/Chrome/NativeMessagingHosts/
cat > ~/Library/Application\ Support/Google/Chrome/NativeMessagingHosts/com.hivemtk.browser.json << 'EOF'
{
  "name": "com.hivemtk.browser",
  "description": "HiveMTK Browser Automation Native Messaging Host",
  "path": "/Users/xxx/.local/bin/hivemtk_browser_nm_host",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://<扩展ID>/"]
}
EOF

# 3. Chrome 加载扩展
#    chrome://extensions → 开发者模式 → 加载已解压扩展 → 选 chrome-extension/

# 4. 验证
#    扩展 icon 点击 → popup 显示 "HiveMTK Browser Connected" = 成功
```

---

## 9. 调度引擎设计

### 9.1 三种触发方式

| 方式 | 实现 | 入口函数 |
|------|------|----------|
| **定时** | `pkg/cron` + `BrowserCronAdapter` | `OnCronFire(ctx, cronID)` |
| **循环** | 内存中 datasource 遍历 | `ExecuteLoop(ctx, taskID, datasource)` |
| **工作流** | `workflow_orchestrator` 注册 `browser_task` executor | `OnWorkflowAction(ctx, execID, nodeID, params)` |

### 9.2 定时任务

```
启动期：
  BrowserCronAdapter.LoadAll()
    → SELECT * FROM browser_cron_triggers WHERE enabled=true
    → 每个 cron.AddFunc(expr, func() { svc.OnCronFire(ctx, cronID) })
    → cron.Start()

运行期：
  cron 触发 → OnCronFire → ExecuteTask(taskID, cron.variables)
    → 更新 browser_cron_triggers.last_run_at
    → 计算并更新 next_run_at
```

### 9.3 循环任务

循环任务不是独立触发器，而是 Service 层一个便捷入口：

```go
// 伪代码
func (s *BrowserAutomationService) ExecuteLoop(ctx, taskID, datasource) {
    for i, item := range datasource {
        vars := mergeTaskVars(task.Variables, item)
        session, err := s.ExecuteTask(ctx, taskID, vars)
        if err != nil {
            log.Errorf("loop %d failed: %v", i, err)
            if !task.RetryOnStepFail { continue }
        }
        // 可选：节流 delay
    }
}
```

### 9.4 工作流子节点

```
workflow_node_executors.go 新增注册：
  registerExecutor("browser_task", func(ctx, node, input) (any, error) {
      taskID := input["task_id"].(string)
      vars   := input["variables"].(map[string]any)
      session, err := browserSvc.ExecuteTask(ctx, taskID, vars)
      return map[string]any{
          "session_id": session.SessionID,
          "llm_plan":   session.LLMPlan,
          "output":     session.OutputJSON,  // 最后一步的 extract/screenshot 结果
      }, err
  })
```

---

## 10. MCP 协议注册

### 10.1 暴露给外部 Agent 的 3 个 tools

在 `aiagent/mcp/server.go` 或独立 `browser_tools.go` 注册：

| Tool | 说明 |
|------|------|
| `browser_open_task` | 执行一个浏览器任务（prompt 或显式 plan），返回 session_id |
| `browser_list_tasks` | 列出所有任务 |
| `browser_execution_log` | 查看某次执行的步骤日志 |

### 10.2 调用流程

```
任何 MCP 客户端 (Claude Code / Codex / 自研 Agent)
    │
    │ mcp.tool_call("browser_open_task", {prompt: "...", variables: {...}})
    ▼
aiagent/mcp/server.go handleBrowserOpenTask()
    │
    │ Service.ExecuteTask()
    │   → Brain.GeneratePlan()
    │   → Translator + Hand 循环执行
    ▼
返回 session_id + 结果摘要
    │
    │ Agent 轮询 browser_execution_log(session_id) 获取进度
```

### 10.3 MCP 接入的价值

- 企业内外的任何 AI Agent（Trae / Cursor / Claude Code / Codex）都可以直接调浏览器自动化能力
- 不需要写代码，Agent 只需通过 MCP tool call 自然语言描述需求
- 与项目已有的 Agent 生态（aiagent 域）无缝融合

---

## 11. 前端页面设计

### 11.1 页面清单（6 个新页面 + 1 个改造）

| 页面 | 路由 | 状态 |
|------|------|------|
| 自动化枢纽 | `/system/automation-hub` | **改造现有**，扩展浏览器自动化入口 |
| 任务列表 | `/browser-automation/tasks` | 新建 |
| 任务编排器 | `/browser-automation/tasks/new` | 新建，核心页面 |
| 执行监控 | `/browser-automation/sessions/:session_id` | 新建 |
| Cron 配置 | `/browser-automation/cron` | 新建 |
| LLM Playground | `/browser-automation/playground` | 新建 |

### 11.2 任务编排器 UI 布局

```
┌─────────────────────────────────────────────────────────────┐
│ 浏览器自动化任务编排                                            │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│ 【模式选择】                                                  │
│  ○ LLM 驱动（自然语言）   ● 显式原语（可视化步骤）               │
│                                                              │
│ ┌── LLM 驱动模式 ─────────────────────────────────────┐    │
│ │ Prompt 输入:                                          │    │
│ │ ┌─────────────────────────────────────────────────┐ │    │
│ │ │ 登录 admin.example.com，用账号 {{user}}           │ │    │
│ │ │ 密码 {{pass}} 进入订单页，导出 CSV 到 OSS          │ │    │
│ │ └─────────────────────────────────────────────────┘ │    │
│ │ 变量定义:  user=[admin]  pass=[***]                  │    │
│ │                                              [生成 Plan] │    │
│ └────────────────────────────────────────────────────┘    │
│                                                              │
│ ┌── 显式原语模式 ─────────────────────────────────────┐    │
│ │ 工具箱 │ 步骤流                                     │    │
│ │        │ ① open_tab  admin.example.com/login        │    │
│ │ open   │ ② wait    load_state=domcontentloaded      │    │
│ │ click  │ ③ type    #username {{user}}               │    │
│ │ type   │ ④ type    #password {{pass}}               │    │
│ │ wait   │ ⑤ click   button[type=submit]              │    │
│ │ screenshot│ ⑥ wait   .order-list 可见                │    │
│ │ extract│ ⑦ extract .order-item fields=[order_id,amt]│    │
│ │        │ ⑧ screenshot orders.png                    │    │
│ └────────────────────────────────────────────────────┘    │
│                                                              │
│ 【触发方式】○ 手动   ○ 定时 cron   ○ 工作流子节点               │
│                                                              │
│ 【执行配置】最多步骤 [50]  超时 [120]s  失败重试 [✓]            │
│                                                              │
│                               [保存草稿]  [保存并执行]           │
└─────────────────────────────────────────────────────────────┘
```

### 11.3 执行监控页

```
┌──────────────────────────────────────────────────────────────┐
│ Session #sess_xxx  ● running  3/8 steps  耗时 45s              │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│ ┌─ Timeline ──────┐  ┌─ Tab 状态 ─────────────────────────┐ │
│ │ ✅ open_tab 120ms│  │ URL: admin.example.com/...         │ │
│ │ ✅ wait      800ms│  │ Title: 后台 - 订单管理             │ │
│ │ ✅ type    1,200ms│  │                                   │ │
│ │ ⏳ type    执行中 │  │ ┌─ 实时截图 ───────────────────┐ │ │
│ │ ○ click           │  │ │                              │ │ │
│ │ ○ wait            │  │ │                              │ │ │
│ │ ○ extract         │  │ │                              │ │ │
│ │ ○ screenshot      │  │ └──────────────────────────────┘ │ │
│ └──────────────────┘  └───────────────────────────────────┘ │
│                                                               │
│ ┌─ 当前 Step 详情 ──────────────────────────────────────────┐│
│ │ action:  type                                              ││
│ │ params:  {selector: "#password", value: "********"}       ││
│ │ browser: element_found=true, keystrokes_delivered=complete││
│ └───────────────────────────────────────────────────────────┘│
│                                               [终止执行]      │
└──────────────────────────────────────────────────────────────┘
```

---

## 12. 安全边界 + FeatureFlag

### 12.1 FeatureFlag

| 开关 | 默认 | 说明 |
|------|------|------|
| `FF_BROWSER_AUTOMATION` | 0 | 总开关，关闭时所有 browser_automation API 返回 disabled |
| `FF_BROWSER_LLM_PLAN_CACHE` | 1 | 计划缓存，相同 prompt+DOM 命中时跳过 LLM |
| `FF_BROWSER_REACT_LOOP` | 0 | ReAct 闭环，每 N 步 LLM review |
| `FF_BROWSER_EVALUATE` | 0 | 安全开关，是否允许 evaluate action（直接注入 JS） |

### 12.2 安全边界

| 风险 | 等级 | 对策 |
|------|------|------|
| 任务失控无限循环 | 🔴 高 | `max_steps` 硬限制（默认 50）+ `timeout_sec` |
| 敏感信息落 DB（密码等） | 🔴 高 | `variables` 中标记为 `secret` 的字段自动 AES 加密存储 |
| 跨域自动登录窃取 Cookie | 🟡 中 | 扩展不读写 `chrome.cookies` API；只控制 tab，不处理凭证 |
| 多 Agent 并发冲突 | 🟡 中 | Hand 层单实例互斥队列，同一时刻只有一个命令流入扩展 |
| LLM 注入 → 生成恶意 JS | 🟡 中 | Brain prompt 不允许 pass-through 用户输入为 script；`evaluate` 默认禁用 |
| Native Host 二进制被篡改 | 🟡 中 | install.sh 做 sha256 校验；manifest 放在用户级路径而非系统级 |
| 扩展被卸载后自动化静默失败 | 🟢 低 | 执行前 Hand.EnsureConnected() 做健康检查；失败立即终止并告警 |
| 截图/HTML 快照落盘敏感页面 | 🟡 中 | 敏感配置（`sensitive_url_patterns`）匹配时自动打码后再存 |

### 12.3 审计

| 审计项 | 方式 |
|--------|------|
| 谁在什么时候执行了什么 | `browser_sessions.created_by` + `started_at` + `operation_logs` 表 |
| 扩展健康状态 | `/api/manage/browser-automation/ext-health` 心跳接口 |
| LLM 调用记录 | 复用现有 `llm_routing_logs` |

---

## 13. 风险评估

| # | 风险 | 概率 | 影响 | 对策 |
|---|------|------|------|------|
| 1 | **Native Host 进程崩溃** → 整条链路断 | 中 | 高 | Go NM Host 做守护进程重启；Go Hand 层 `EnsureConnected()` 做 3 次重试 + 指数退避 |
| 2 | **Chrome 重启后扩展失活** → 需要手动 reload | 低 | 中 | Manifest V3 扩展随 Chrome 自动加载，不需要 reload；首次连接 Go 侧做健康检查 |
| 3 | **LLM 生成错误 selector** → 执行失败 | 中 | 中 | Translator 做 selector 预校验（尝试 `querySelector` 并看返回值）；失败时 Brain.RePlan 重新规划 |
| 4 | **SPA 页面等待永远不触发** → timeout | 中 | 中 | 内置默认等待策略（2s / 10s / 60s 三级）；超过 timeout 写 failed，不阻塞整个 session |
| 5 | **多 Chrome 窗口混乱** → tab 归属错 | 低 | 中 | open_tab 时指定 `windowId: chrome.windows.WINDOW_ID_CURRENT`；维护 tab→window 映射 |
| 6 | **Playwright 类方案后续又想换回** | 低 | 低 | 架构已分层（Brain → Translator → Hand），Hand 接口固定，换实现不影响上层 |
| 7 | **Go Hand ↔ Go NM Host stdio pipe 阻塞** | 低 | 中 | Go 用 `exec.CommandContext` + `io.Copy` goroutine 读 stdout，带超时；Go NM Host 端每个 command 必须在限定时间内写 response |
| 8 | **生产环境有人手动 kill Chrome → session 全断** | 低 | 高 | 这是硬约束——寄生式方案依赖 Chrome 在运行。Hand 层检测到端口断开时立即回写 session 为 terminated；调度器停止新的 cron 触发直到健康检查恢复 |
| 9 | **Manifest V3 background service worker 超时限制（30s）** | 中 | 中 | 长任务（screenshot/save 大文件）必须通过 `offscreen.html` 或拆成多步；单条原语执行时间严格控制在 5s 内 |
| 10 | **项目已验证成熟 Go 库：github.com/rickypc/native-messaging-host** | 中 | 低 | Browser Agent Bridge 有完整 Native Messaging Host (Go) 实现可参考；Go 端 stdio frame 协议实现约 50 行 |

---

## 14. 实施路线图

### 阶段 1：MVP（可跑通一条链路）— 预计 5-7 天

**目标**：手动触发一个显式原语任务，从打开后台 tab 到截图，全链路跑通

| # | 任务 | 产出 | 依赖 |
|---|------|------|------|
| 1 | DB Migration 5 张表 | SQL Migration + GORM Model | 无 |
| 2 | Repository 层（5 个 repo） | CRUD + 查询方法 | #1 |
| 3 | Chrome 扩展 MV3（核心 3 种原语） | manifest + background.js | 无 |
| 4 | Go NM Host | stdio 消息循环 | #3 |
| 5 | Go Hand 层（最小集） | open_tab / click / screenshot | #4 |
| 6 | Service + Controller + Router | 手动执行 API | #2 #5 |
| 7 | 前端任务编排（显式原语模式） | 列表页 + 编排器 | #6 |
| 8 | 执行监控页（基础版） | Timeline + 截图 | #6 |
| 9 | E2E 端到端验证 | 一条显式 plan 成功执行 | #1-#8 |

### 阶段 2：LLM 大脑 — 预计 4-6 天

| # | 任务 | 产出 | 依赖 |
|---|------|------|------|
| 10 | Brain 层（GeneratePlan） | LLM plan 生成 | 阶段 1 |
| 11 | Translator 层 | 变量替换 + Plan 校验 | #10 |
| 12 | Plan 缓存 + browser_llm_plans 表 | 缓存命中/存储 | #10 |
| 13 | ReAct 闭环 | 每 N 步 RePlan | #10 |
| 14 | LLM Playground 页面 | prompt → plan 预览 → 一键执行 | #10 |
| 15 | 全原语支持（补齐 wait/extract/evaluate 等） | 扩展 + Go Hand | 阶段 1 |

### 阶段 3：调度集成 — 预计 3-4 天

| # | 任务 | 产出 | 依赖 |
|---|------|------|------|
| 16 | BrowserCronAdapter + cron 配置页 | 定时触发 + 前端管理 | 阶段 2 |
| 17 | workflow_orchestrator browser_task executor | 工作流子节点调用 | 阶段 2 |
| 18 | ExecuteLoop 循环任务 | datasource 遍历 | 阶段 2 |
| 19 | MCP 注册 browser_* tools | Agent 可调用 | 阶段 2 |

### 阶段 4：生产加固 — 预计 4-5 天

| # | 任务 | 产出 |
|---|------|------|
| 20 | 多扩展实例路由 | extension_instance_id |
| 21 | 敏感信息 AES 加密 | variables secret 字段 |
| 22 | 健康检查 + 心跳 | /ext-health 接口 + 扩展状态显示 |
| 23 | 审计日志 | 谁在什么时候执行了什么 |
| 24 | 文档收尾 + 灰度启动脚本 | FeatureFlag 控制上线 |

### 里程碑

```
D1-D3: DB + 扩展 + Host + Go Hand  跑通 open_tab + click + screenshot
D4-D7: Service/Controller/Router + 前端基础页面  手动执行 E2E
D8-D13: Brain + Translator + 缓存 + Playground  LLM 驱动可用
D14-D16: Cron + 工作流 + MCP  三种触发齐全
D17-D20: 安全 + 审计 + 加固  生产就绪
```

---

## 附录 A：复用 vs 新建清单

| 能力 | 复用 | 新建 | 改造量 |
|------|------|------|--------|
| 定时调度 | ✅ `pkg/cron` | — | 新增 BrowserCronAdapter ~100 行 |
| 工作流编排 | ✅ `workflow_orchestrator` | — | 新增 browser_task executor ~150 行 |
| MCP 协议 | ✅ `aiagent/mcp/server` | — | 注册 3 个 tools .~80 行 |
| LLM routing | ✅ 现有 `llm_routing` | — | Brain prompt 模板 ~30 行 |
| 五层架构 | ✅ 现有分层模式 | — | 新域 browser_automation |
| GORM/PG | ✅ 现有 DB 基建 | — | 5 张新表 |
| Chrome 扩展 | — | ✅ MV3 ~300 行 | 全部新建 |
| Native Messaging Host (Go) | — | ✅ .~80 行 | 全部新建 |
| Go Hand（Native Messaging） | — | ✅ ~400 行 | 全部新建 |
| Brain/Translator/Service | — | ✅ 新域 Service 层 | 全部新建 |
| 前端页面 | ✅ Vue3 + ElementPlus | ✅ 6 个新页面 | 全新 + 1 个改造 |

**总新增代码量估算**：
- Chrome 扩展: ~300 行 JS
- Native Messaging Host (Go): .~80 行
- Go 后端: ~1,500 行（Model + Repository + Service + Controller + Router）
- DTO: .~80 行
- 前端: ~2,000 行（6 页面 + API 层）
- Migration: ~150 行 SQL
- **合计约 4,350 行**（不含测试）

---

## 附录 B：已有相关文档索引

| 文档 | 内容 | 路径 |
|------|------|------|
| Chrome MCP 技术选型 | 硬约束 + 路线对比 + 方案 A 推荐 | `docs/CHROME_MCP_BROWSER_AUTOMATION.md` |
| 本技术方案 | 完整产品设计（本文档） | `docs/BROWSER_AUTOMATION_TECH_DESIGN.md` |
| 产品设计 v1 初稿 | 之前产出的产品设计 | `docs/BROWSER_AUTOMATION_PRODUCT_DESIGN.md` |
| Browser Agent Bridge | 参考实现（Native Messaging + HTTP/WS JSON-RPC） | https://github.com/TNJ2026/browser-agent-bridge |
| Chrome Native Messaging 官方文档 | 协议规范 | https://developer.chrome.com/extensions/nativeMessaging |
| 九个梯队分析 | HugoSkills 业界全景 | https://hugozhu.site/post/2026/312-browser-automation-eight-tiers/ |
| 2025 浏览器自动化报告 | FigmaAI 韩文报告（含 Browser-Use 对比） | https://github.com/FigmaAI/KleverDesktop/blob/f8156a5688cb3e2ebd4d145220b96974c5b6df43/docs/BROWSER_AUTOMATION_2025.md |

# 浏览器自动化全链路技术论证文档

> 版本：v1.0 | 日期：2026-09-11
> 前置结论文档：`BROWSER_AUTOMATION_TECH_DECISION.md`（路线决策：维持 NM 桥 + 真机）
> 本文档：对全链路**每一步、每个技术栈、每段架构逻辑逐个调研论证**。
> 源码锚点均为本轮精读原文，非推测。

---

## 0. 全链路总览（9 步）

```
S0 前端控制台（Vue3 7 路由 + api + stores）
 → S1 Gin 业务路由（JWT）/tasks/sessions/cron/host/status/platforms
 → S2 TaskService/SessionService/CronService + Executor（steps 解释 / Brain 循环）
 → S3 Hand 命令出口（16 原语）→ HostRegistry req_id 回包
 → S4 WS /api/browser/host-ws（token + 回环 IP）→ nm-host（Go 子进程）
 → S5 NM 4 字节帧 → 扩展 SW connectNative（background/index.js）
 → S6 primitives.js dispatch（16/16 对齐）→ executeScript 注入 / CDP trusted / captureVisibleTab
 → S7 平台适配 L3（douyin/xiaohongshu/xianyu：Locators/DetectBlock/ClassifyError/CommentLocators）
 → S8 回包原路返回 → step.result/session 状态机/command_log 审计 → 前端 Monitor/Detail
```

---

## S0. 前端控制台（user-web）

**代码锚点**：`src/router/modules/browserAutomation.js`（7 路由）、`src/api/browserAutomation.js`（91 行，20+ 接口）、`src/stores/browserAutomation.js`、`src/views/browserAutomation/`（List/Editor/Detail/Monitor/Cron/Status.vue）。

| 路由 | 页面 | 调后端接口 |
|---|---|---|
| tasks / tasks/create / tasks/:id/edit | List / Editor | tasks CRUD + publish/run/pause/resume/archive/dependency |
| tasks/:id | Detail | getTask + listTaskSessions + steps |
| sessions/:id | Monitor | getSession + steps + stop |
| cron | Cron | cron CRUD + enable/disable |
| status | Status | host/status + token/reset（admin）+ platforms/locators |

**技术栈论证——Vue3 + Vite + Pinia + ElementPlus**：
- 结论：维持。与 manage 后台同栈（CLAUDE.md 前端规范），组件/状态管理模式复用，零学习成本。
- 备选被否：React 重写——沉没资产（7 页面已交付），且本模块前端只是控制台（无重交互），换栈零收益。

---

## S1. 服务端路由装配（Gin）

**代码锚点**：`user-server/internal/router/browser_automation_routes.go`（126 行）。

- 业务路由挂 `auth` 组（JWT）：`/browser-automation/tasks|/sessions|/cron|/host/status|/platforms`，共 25 个端点；`POST /host/token/reset` 再套 `AdminAuthMiddleware`。
- Host WS 挂 `engine` 独立：`GET /api/browser/host-ws`，**不走 JWT**，靠 token + 本地回环 IP 双层防护（fail-closed）。
- DI 装配：6 repository + registry/hand 单例 + brain/feedback/executor + task/session/cron service；三处外部注入（`tooluse` MCP browser 工具、`wfsvc` workflow browser_task 动作、disconnect 钩子 FailRunningByUser）。
- 启动后台：`EnsureHostTokenExists`（防首次部署 fail-closed 卡死）+ `cronSvc.RestoreAll`。

**技术栈论证——Gin**：
- 结论：维持。全仓统一框架，中间件（JWT/AdminAuth）复用；25 端点 CRUD 形态无特殊性能需求。
- **五层铁律合规**：Router 只做映射 + DI，零业务逻辑，合规。

---

## S2. 执行引擎 Executor（双模式）

**代码锚点**：`service/executor.go`（741 行）。

- **编排模式**：loop × steps，`executeStepWithRetry`（retry/backoff 指数退避）→ `dispatchStep` 按 action 分发 16 原语；步间 `humanizedDelay`（base ±30% 抖动，铁律 2 反检测）；步后 `detectBlockedIfFatal`（snapshot 命中平台 DetectBlock + ClassifyError=disconnect 即终止）。
- **Brain 模式**（`executeBrain`）：兜底 open_tab → snapshot → `GeneratePlanReflect` → 执行 → 独立 `JudgeDone` 验收 → reflect 状态回喂（history≤24条滑动窗口、prevEvaluation、memory）。五重熔断：maxBrainIterations=40、plan 连败 3、动作连败 5、token 预算 200k（env 可调）、wall-clock 看门狗 TimeoutSec+30s。循环指纹 nudge（连续 3 轮同序列强制换路径）。空 plan 计失败防 0 步死循环烧迭代。
- 状态收口：session/step 状态只由 Executor 写；command_log append-only（session 局部 seq，P0-2 跨 session 隔离）；失败仅告警不阻断（审计不能拖垮业务）。

**架构逻辑论证**：
- 双模式并存：编排模式给确定性任务（低成本），Brain 模式给开放式目标（LLM 编排）。`_ = steps // Brain 模式忽略显式编排` 显式声明，职责清晰。
- judge 独立于 plan（agent 自称完成 ≠ 真完成），fail-open 连续 2 次视为不通过——验收增强挂掉≠放行，这是对的（fail-closed 思想延续）。
- Watchdog 的存在说明 ctx 取消链在 LLM/DB 调用栈曾不生效（session132 实测 11min+ active）——真实时钟兜底是必要的。

---

## S3. Hand 命令出口 + HostRegistry + Token

**代码锚点**：`service/hand.go`（194 行，16 原语）、`service/host_registry.go`（247 行）、`service/host_token.go`（115 行）。

- Hand：open_tab/click/type/snapshot/markdown/screenshot/wait/wait_for_selector/scroll/click_near/assert/query/post_comment/extract/close_tab/tab_exists。约束三条：不启动子进程 / 单 Host 连接内串行（无全局锁，跨用户天然隔离）/ 每命令超时（默认 30s，markdown 60s、post_comment 45s、wait 类按参数放宽）。
- Registry：userID→*HostConn；写帧 writeMu 串行；读循环按 req_id 投递 pending chan（超时放弃的回包丢弃）；同用户旧连接顶掉（Chrome 重启）；断连钩子 running session 置 failed。`Request` 四路 select：ctx 取消 / 连接断 / 超时 / 回包（!ok 转 error）。
- Token：`bh_<userID>_<rand16>`，userID 内嵌使握手即绑定；Rotate 旧值滚入 `_prev` 平滑失效；Validate 常量时间比较双候选；KV 空时 fail-closed 拒绝；启动无 token 自动为 admin 生成。

**技术栈论证——Gorilla WebSocket + uuid req_id**：
- 结论：维持。Gorilla 是 Go 事实标准 WS 库；req_id 用 uuid 无碰撞；pending chan 模式是 Go 惯用法，并发约定注释清晰。
- 单 Host 串行 vs 多路复用：Host 单循环消费，服务端串行与之匹配——串行是**对端约束**，不是简化，论证成立。

---

## S4. nm-host（Go 子进程桥）

**代码锚点**：`user-server/cmd/nm-host/main.go`（211 行），hostVersion=`1.2.0`。

- 双泵循环：WS 命令帧→stdout（4 字节 native-order 头 + JSON，host→ext ≤1MiB 超限回 `chrome_write` 错误帧让 server 快速失败）；stdin 回帧→WS。stdin 关闭即退出（Chrome 连带杀掉，无孤儿进程）。
- 无 token 不立即退出（sleep 30s 等配置，防扩展报 disconnected）；WS 断线指数退避 2s→60s；注册帧带 version+pid。
- token 来源：env `HIVE_MTK_HOST_TOKEN` 优先，次选 `~/.hivemtk/nm_host.conf`。

**技术栈论证——Go 写 Host 而非 Python/Node**：
- 结论：维持。① 与服务端同语言同 go.mod（gorilla 复用）；② 单二进制分发（install.sh 无 runtime 依赖）；③ stdin/stdout 二进制帧操作 Go 标准库直接支持。
- 备选被否：Python——用户机需解释器环境；Node——与扩展 JS 同语言但分发体积大。Go 是最优解。

---

## S5. 扩展 SW 入口（MV3 + Native Messaging）

**代码锚点**：`browser_automation/src/background/index.js`（58 行）、`src/core/native-messaging.js`（66 行）、`src/core/tab-manager.js`（39 行）、`src/core/api-client.js`（199 行）、`src/core/constants.js`。

- `connectNative('com.hivemtk.browser')` 长连接（sendNativeMessage 只认第一条回包，不可用——官方文档事实）；收发消息保活 SW（Chrome 105+/114+）；指数退避重连 2s→30s 上限 5 次后 giveup；`onStartup/onInstalled` 空监听作 SW 冷启动锚点。
- 命令帧 `{req_id, action, ...}` → `dispatch(cmd, deps)` → 回 `{req_id, ok, data|error}`。
- tab 寄生式：`tabs.create({active:false})` 不抢焦点；close 容忍用户已关；activate 只为截图前置。
- api-client：`chrome.storage.local` 存 serverUrl（默认 `http://localhost:8204`）+ JWT；`{code,message,data}` 信封 code===0 拆包。

**技术栈论证——Chrome Extension MV3 Service Worker**：
- 结论：维持。MV2 已被 Chrome 下架通道淘汰，无备选。SW 不持久化是约束，但 connectNative 保活 + 事件锚点已对冲；SW 内存 refs（见 S6）导航即失效是已知代价，snapshot 每次重建可接受。
- **bridge/ 目录 41 文件是 DM 抓取通道扩展，与本链无关**——S1–S8 只走 `browser_automation/` 9 文件扩展，勿混淆。

---

## S6. 原语分发与可信输入（核心中的核心）

**代码锚点**：`src/core/primitives.js`（491 行）、`src/core/cdp/input.js`（164 行）、`src/core/accessibility.js`（125 行）。

- `dispatch` 16/16 对齐 Hand（open_tab URL 协议校验 → tabExists 预检 → `@eN`→CSS 解析 → 分发）。越界钳位：timeout 1s–60s、scroll 0–20000、wait ≤60s、extract 单 key ≤100 节点、markdown ≤64KiB。
- **序列化注入铁律**：executeScript func 被序列化，闭包全丢，外部值必须经 args——primitives.js 头注释明示，所有 inj* 函数零外部引用。
- 合成事件三层递进：`el.click()` → Pointer/Mouse 真实序列 → 外链/新标签 href 接管（保持 tab 稳定）。contenteditable 走 `execCommand('insertText')` 输入管线 + 逐字符 InputEvent 兜底（draft.js/ProseMirror 监听 beforeinput）。
- **CDP trusted 输入**（post_comment 必经）：ASCII 走 `Input.dispatchKeyEvent`（USKeyboardLayout 码表，Puppeteer 对齐），CJK/emoji 逐字 `Input.insertText`（= ImeCommitText，trusted）；鼠标 5 步线性轨迹 mouseMoved → settle（对数正态 ~300ms）→ press → hold（~84ms）→ release；attach 单例 + idle 3s detach（减横幅闪烁）+ infobar 挤压 56px 等 500ms；detach 类错误重 attach 重试一次。
- **Accessibility refs**：交互节点（a/button/input/textarea/select/contenteditable/role/click）+ 静态文本（h1-h6/p/li/label/th/td，供 LLM 判目标达成）；`role "name" @eN` 格式；SW 内存 Map，导航失效；MAX 400 节点。
- **post_comment 三阶段**：Prep（定位+聚焦，contenteditable 交 trusted）→ Send（按钮视口坐标 + CDP 真实点击）→ Verify（MutationObserver + 轮询，就地验证渲染，未见即抛错防静默假成功）。
- screenshot M3 定稿：`captureVisibleTab` 仅当前激活 tab，先激活 + 150ms 渲染一帧再截。

**技术栈论证**：
- chrome.debugger CDP：维持。trusted 事件是过合成事件检测的唯一手段（isTrusted=true）；代价是调试横幅 + 坐标视口系（infobar 对策已做）。备选（仅合成事件）被否：小红书 React 受控组件对 untrusted 输入不响应，实测结论。
- Accessibility snapshot：维持。对标 browser-use，先例充分；@eN 快照重排问题用 click_near（锚+文本 4 层上溯）对冲。
- humanize 对数正态时序：维持。参数表引自 xiaohongshu-mcp humanize/provider.go，有出处非拍脑袋。

---

## S7. 平台适配 L3

**代码锚点**：`platform/platform.go`（144 行）+ douyin/xiaohongshu/xianyu 三适配器。

- 接口：Identifier/Capabilities/MaxConcurrentJobs/Locators/DetectBlock/ClassifyError；CommentPoster 可选 + CommentLocators 四元组。新平台 = 四件套 + `init()` 自注册，基座/Brain/UI 零改动（P5）。
- ErrType 四分类：refresh_token（可刷新）/bad_body（不重试）/retry（瞬态）/disconnect（人工介入）。`CommentLocatorsFor` 契约默认失败（声明能力不实现→首次调用即报错，防静默假成功）。
- 写串行：MaxConcurrentJobs（Postiz 默认 1，风控要求）。

**技术栈论证——Go interface + init 自注册表**：
- 结论：维持。Go 惯用插件模式（database/sql 驱动注册同构）；重复注册 panic 防呆；Get 返回 error 而非 nil 防空指针。先例 Postiz/Mixpost/Apify 已在注释载明。

---

## S8. 回包与状态机

- 回包原路：扩展→NM 帧→nm-host→WS→Registry req_id 投递→Hand→Executor `recordResult`→step.result 落库；extract 合并入 `extracted_data`（追溯面板读）；screenshot 经 FeedbackService 落库 FinalScreenshotURL。
- 状态机：session active→completed/failed/stopped；断连钩子 running→failed；Brain 总结 completed 与 failed 都写 llm_summary（失败归因也是交付物）。

---

## 技术栈总表（一行一结论）

| 技术栈 | 结论 | 一句话论证 |
|---|---|---|
| Vue3+Vite+Pinia+ElementPlus | 维持 | 与 manage 同栈，控制台无重交互，换栈零收益 |
| Gin | 维持 | 全仓统一，25 端点 CRUD 无特殊需求；Router 零逻辑合规 |
| GORM（6 repository） | 维持 | 五层 Repository 层既定选型，task/session/step/cron/plan/cmdlog 六表够用 |
| Gorilla WebSocket | 维持 | Go 事实标准；req_id+pending chan 惯用法 |
| Go NM Host | 维持 | 单二进制分发，stdin/stdout 帧操作标准库直持 |
| MV3 SW + connectNative | 维持 | MV2 已淘汰无备选；保活+重连+冷启动锚点已对冲 SW 短命 |
| chrome.scripting.executeScript | 维持 | 官方唯一注入通道；序列化铁律已在代码明示 |
| chrome.debugger CDP | 维持 | trusted 事件唯一解；横幅/infobar 代价已对冲 |
| Accessibility snapshot @eN | 维持 | 对标 browser-use；重排问题 click_near 对冲 |
| LLM reflect+plan + judge | 维持 | MessageManager/evaluation/memory/judge 四件套对标齐；五重熔断齐 |
| L3 平台注册表 | 维持 | Postiz/Mixpost 先例；新平台零改动基座 |

**被否备选**：Playwright/chromedp/rod（服务端 headless——风控/内存/沉没资产三否，见 TECH_DECISION §4.2）；React 重写前端；Python/Node 写 Host。

---

## 版本锚点与风险（重申）

四处 `1.2.0` 必须同改：扩展 `src/manifest.json` + `dist/manifest.json` + nm-host `hostVersion` + `install.sh` 模板。Chrome SW ScriptCache 缓存陷阱下，`host/status` 版本号是新代码生效的排查锚点。

| 风险 | 现状 | 后续 |
|---|---|---|
| SW 空闲回收丢 tab | snapshot 失败→重开 tab 自愈（P2-1 审计） | I4 重连续跑（P2） |
| token 明文存 conf | 常量比较+回环 IP 双层 | I2 已有 Rotate；定期轮换（P1） |
| 无健康检查 | —— | I3 `scripts/check_browser_host.sh` 已交付（P1） |
| 单 Host 单用户 | Register 顶掉旧连 | I6 多 Host 预留（P3） |
| 审计 | command_log append-only | I5 审计落盘（P2） |

---

## 附录 A. 命令往返时序（源码级）

一次 `click` 从前端到回包的完整帧旅程（各段帧格式均为本轮精读原文）：

```
前端 Monitor.vue
 │ POST /browser-automation/tasks/:id/run（JWT）
 ▼
TaskController.Run (task.go:215) → TaskService.RunTaskWithRetry → Executor.executeBrain/executeSteps
 │ Hand.click(ctx, userID, tabID, "@e7")（hand.go:38）
 │   → cmd = {"action":"click","tab_id":7,"target":"@e7"}
 ▼
HostRegistry.Request (host_registry.go:216)：uuid req_id 注入 → registerPending → writeJSON（writeMu 串行，写 deadline 10s）
 │ WS 帧 {"action":"click","tab_id":7,"target":"@e7","req_id":"<uuid>"}
 ▼
nm-host pumpLoop (main.go:196)：conn.ReadJSON → writeNativeFrame（4 字节 native-endian 头 + JSON，≤1MiB）
 │ NM 帧 → Chrome → 扩展 background onMessage
 ▼
primitives.js dispatch(cmd)：@e7→CSS 解析 → 三层递进点击 → 回 {"req_id":"<uuid>","ok":true,"data":{...}}
 │ NM 回帧 → nm-host stdin 泵 → conn.WriteJSON → WS
 ▼
HostRegistry.readLoop (68-94)：按 req_id 投递 pending chan → Request select 第四分支收包
 → res.Data（!ok 则 errors.New透出 error 文）
 ▼
Executor.executeStepWithRetry (436-485)：step 落库 running → dispatchStep → UpdateResult(success/failed)
 + command_log append 两条（direction=command/event，seq session 局部单调，468-480 行）
```

超时/中断四分支（host_registry.go:231-246）：ctx 取消 / 连接关闭（→ErrHostOffline→controller 转 409 引导）
/ 超时（`Host 命令超时（%s，action=%v）`）/ 回包。超时后 late 回包因 pending 已删而被丢弃（89-91 行），无泄漏。

---

## 附录 B. 错误码表（API 信封 + 链路错误）

全仓统一信封（CLAUDE.md API 规范）：成功 `{"code":0,"data":{...},"message":"ok"}`；
失败 `code = 400 参数 / 401 认证 / 403 权限 / 404 不存在 / 409 冲突 / 500 服务端`。

| 错误 | 源码 | 含义与处理 |
|---|---|---|
| Host 未连接 | `ErrHostOffline`（host_registry.go:18）→ controller 转 **409** | 前端 Status 页引导：本机 Chrome 加载扩展 + 运行 install.sh |
| Host 命令超时 | `Host 命令超时（%s，action=%v）`（host_registry.go:237） | 按 Hand 超时表（§3.1/SOLUTION §3.1）；wait 类已按参数放宽 |
| 发送失败 | `发送命令到 Host 失败`（229 行） | WS 写坏，nm-host 侧通常已在重连（退避 2s→60s） |
| chrome 写坏 | `chrome_write: ...`（main.go:208） | NM 通道坏（扩展重载），server 侧命令快速失败，不悬挂 |
| 平台未注册 | `平台 %s 未注册（可用: %v）`（platform.go:95） | 返回 error 而非 nil，防下游空指针 |
| 能力缺实现 | `声明了 post_comment 但未实现 CommentPoster（fails-loudly）`（141 行） | 首次调用即报错，防静默假成功 |
| 账号风控 | ErrType `disconnect`（platform.go:36） | Executor 直接终止（重试无意义）+ 人工介入 |
| 登录态过期 | ErrType `refresh_token` | 可刷新后重试 |
| LLM 不可重试 | 401/403/unauthorized/invalid_api_key/forbidden（brain_reliability.go:15） | 立即快败，省 token 预算 |
| LLM 可重试 | 429/5xx/超时/网络/JSON 抖动（21-26 行） | 退避重试；未知错误保守不重试（32 行） |

---

## 附录 C. Controller 函数级签名（5 文件 27 方法）

| 文件 | 方法（行） | 路由 |
|---|---|---|
| task.go | Create(61)/List(99)/Get(114)/Update(128)/Delete(189)/Publish(202)/Run(215)/Pause(229)/Resume(242)/Archive(256)/SetDependency(269) | §3.4 任务 12 端点 |
| session.go | List(24)/Get(39)/ListSteps(54)/ListByTask(69)/Stop(85) | Session 5 端点 |
| cron.go | List(24)/Create(34)/Update(53)/Delete(73)/Enable(87)/Disable(92) | Cron 6 端点 |
| host.go | GetStatus(29，分流 admin 全量/普通只读自己)/ResetToken(42，admin)/HostWSHandler.Handle(75，token+回环) | Host + WS |
| platform.go | List(26)/Locators(44) | 平台 2 端点 |

五层铁律抽查：Controller 均为 `func (c *XController) M(ctx *gin.Context)` 薄封装（参数绑定→调 Service→信封返回），
repository 构造与 service 装配全部收敛在 routes.go 26-44 行（DI 不在 controller），合规。

---

## 附录 D. 六表 Schema（GORM model 原文）

| 表 | TableName | 关键列（节选） |
|---|---|---|
| BrowserTask | `browser_tasks` | task_type(one_shot/loop/cron/workflow，默认 one_shot)/status(draft/ready/running/paused/done/failed/archived，默认 draft)/steps jsonb/brain_mode+brain_goal/loop_count=1/delay_ms=**1000**/timeout_sec=**120**/depends_on(mode all_done/any_success)/retry(retry_on_fail=false/delay 300s/max 3)/platform(默认 xiaohongshu)/user_id+account_id |
| BrowserSession | `browser_sessions` | task_id/user_id/chrome_tab_id/status(**created**/active/completed/failed/stopped，默认 created)/snapshot text/llm_plan jsonb/total/success/failed_steps/hand_latency_ms/console_errors/extracted_data jsonb/final_screenshot_url/llm_summary（completed 与 failed 都写） |
| BrowserStep | `browser_steps` | session_id/task_id/step_index/action(32)/target(1024，selector 或 @eN)/value text/params jsonb/status(**pending**/running/success/failed/skipped，默认 pending)/result jsonb/duration_ms/error_msg |
| BrowserCommandLog | `browser_command_log` | session_id/task_id/step_id/**seq**(session 内单调)/direction(**command/event**；judge 验收记 direction=judge，executor.go:330)/action(32)/payload jsonb（命令帧或回包全文）/duration_ms/ok |
| BrowserCronTrigger | `browser_cron_triggers` | task_id(uniq)/cron_expr(128，如 */5 * * * *)/enabled=**true**/next_run_at/last_run_at |
| BrowserLLMPlan | `browser_llm_plans` | task_id/goal/snapshot/steps jsonb/reasoning（脱敏截断）/model(64)/token_in/token_out（P1-1 计量来源） |

step 表 action 注释列出的 11 个（open_tab/click/type/snapshot/markdown/screenshot/wait/
wait_for_selector/scroll/extract/close_tab）是 MVP 注释早于后加的 click_near/assert/query/
post_comment/tab_exists 5 个——注释滞后，行为以 dispatchStep + Hand 16 方法为准（已知文档债，不改代码）。

---

## 附录 E. 前端 api 20+ 接口清单（browserAutomation.js 91 行）

tasks（CRUD + publish/run/pause/resume/archive/dependency 12 个）/ sessions（list/get/steps/listByTask/stop 5 个）/
cron（CRUD + enable/disable 6 个）/ host（status + token/reset admin 2 个）/ platforms（list + :id/locators 2 个），
合计约 27 个，与后端 25 端点一一对应（前端 stop/run 等动词映射见 S0 表）。

---

## I4–I6 后续口径（v1 范围外，本轮不动代码）

- I4 重连续跑（P2）：Host 重连后 session 从最后成功 step 续跑，需 Executor 记 checkpoint。
- I5 审计（P2）：command_log 导出/追溯面板。
- I6 多 Host（P3）：Registry userID→conn 改为 userID→[]conn + 负载选择，预留即可。

---

## 附录 E：Planner 输入帧结构与提示注入面（本轮新增）

- 输入帧（`service/brain.go:148-173 planOnce`）：
  `系统prompt（BuildPlanSystemPrompt+平台知识） + "目标："+goal + "\n\n页面快照：\n"+snapshot
  [+上一步评估/累积记忆/动作历史] → JSONMode(MaxTokens 4096)`。
- 快照截断 64K（`brain.go:233` rune 截断，预算函数见 brain_budget.go，单测 5 组覆盖）。
- 风险点：snapshot 与 goal 之间仅一个换行分隔，无不可信数据标注（详见 SOLUTION §7.1）。
  现有防线：JudgeDone 独立验收 fail-closed + 动作白名单（非白名单动作 LLM 无法下发，
  最坏情况是误操作已有 15 原语之一，而非任意代码执行）—— blast radius 有界，
  这是"暂可接受、v2 再加固"的论证依据。

## 口径修正（与 PROOF §4-1 一致）
- 对外编排原语=15（oneof，`dto/task.go:5`），dispatch=15+2终态，扩展内部+1（tab_exists）。
  本文档 S2/S4 中"16 原语"表述统一修正为"对外 15 + 内部 1"。

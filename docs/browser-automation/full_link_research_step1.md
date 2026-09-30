# Browser-Automation 全链路调研（步骤一）

> 日期 2026-09-30 / 工作区 `hivemtk/` / 结论：本系统**无 JEV / 无 computer-use 像素级控制**，现行链路为「服务端 Brain 编排 → WS → Go NM-Host → 扩展 SW → 页面注入 + CDP trusted 输入」。

## 1. 总览链路

```
Vue user-web (api/browserAutomation.js → 任务/会话/步骤/确认/截图/导出)
  ↓ HTTP /api/browser/...
Go user-server: Router(browser_automation_routes.go) → Controller → Service → Repository → Model
  ├── TaskService / SessionService / Executor / BrainService / HandService / HostRegistry
  ├── CronService / FeedbackService / RetryScanner / Workflow / MCP / Retention / AuditDigest
  ↓ WS /api/browser/host-ws (HostRegistry.Request req_id 回包, ping/pong, 抖动顶替, 病连接自愈)
Go cmd/nm-host (NativeMessaging Host, Chrome 子进程: stdin 4字节头+JSON ↔ WS client)
  ↓ Native Messaging (≤1MiB 出 / 4MiB 入边缘上限)
Chrome 扩展 user-web/browser_automation/ (MV3 SW background/index.js)
  ├── core/native-messaging.js (connectNative 保活 SW, req_id 回帧)
  ├── core/tab-manager.js (tab 稳定, ensureActiveForInput)
  ├── core/accessibility.js (collectInteractiveNodes + assembleSnapshot, @eN refs, 基线去噪)
  ├── core/primitives.js (~1165行: injClick/injType/injPostComment 三段式, probe→CDP→DOM兜底)
  └── core/cdp/input.js (chrome.debugger trusted: attach/bezier轨迹/clickJitter/对数正态时序, deadline单表)
```

## 2. 后端五层（Controller 非 Handler）

- 路由：`user-server/internal/router/browser_automation_routes.go` — auth 业务路由 + engine Host WS；单例 registry/hand/brain/executor 装配，重试/MCP/workflow 注入，断连钩子，bootstrap 后台任务（RetryScanner/Cron Restore/Retention）。
- Controller：`internal/browser_automation/controller/` 仅绑参与调 Service。
- Service（核心）：
  - `brain.go` — GeneratePlanReflect / JudgeDone / SummarizeSession，对标 browser-use 大循环；plan 落库 `browser_llm_plans`，token 账 LastPlanTokens/LastAuxTokens。
  - `brain_prompts.go` — prompt 工厂：schema(reflect: thinking/evaluation/memory/next_goal/done/steps)+动作表+护栏(禁 open_tab、@eN优先、防呆重试、blocked判据、提示注入T1 `<page_snapshot>` 标签)+平台知识 L3 注入 + Recovery/LoopNudge。
  - `executor.go` — 显式 steps 与 Brain 双模式；D7 confirm 闸门（RequireConfirm+ConfirmWaitSec 独立预算）；写台账 submit_state/text_hash/is_write；双发闸 guardResubmit+claimWriteSlot；F2 写步禁重试；终态收口 sessionFinalWriteBudget(WithoutCancel)+wall-clock watchdog。
  - `executor_selfheal.go` — relocate/重定位自愈。
  - `hand.go` — 唯一命令出口，经 HostRegistry.Request：open_tab/click/type/snapshot/markdown/screenshot/query/scroll/extract/assert/wait/comment_prep_send_verify 三段式。
  - `host_registry.go` — WS 注册/同用户顶替/30s5次抖动拒绝/req_id pending 回包/ping30s+读90s/写10s/超时判病 forceSelfHeal/注册探针 probeServable(chrome.tabs.get早退)/servable+last_cmd_ok_at。
  - `timeouts.go` — 全预算单表：cmd30s/markdown60s/screenshot60s(真站29.95s实测)/comment_send45s/条件宽限10s/wait宽限5s；CDP attach8s/send5s/type总15s/cmd25s(扩展侧)。
  - `write_ledger.go` — HashWriteText(fnv32a去空白)+recordSubmitState(WithoutCancel+重试+ledgerGap兜底+降级哨兵)；SubmitState prepared/sent/verified/unattributed（model/step.go）。
  - `task.go / session.go / cron.go / feedback.go / workflow / mcp / retention / stale_reconcile / audit_digest` — 任务CRUD+依赖环检+重试落列 NextRetryAt+分钟扫描认领；会话导出；cron 恢复；终态反馈+终屏截图；审计摘要。
- Repository：GORM 数据访问，无业务判断；StepRepo 含 UpdateSubmitState/FindSubmitAttempt/claimWriteSlot。
- Model：`task.go(BrowserTask)` 任务主体 + `step.go(BrowserStep)` 步骤台账列 + session/plan/command_log/cron 等。
- DTO：`dto/step_action.go` 白名单由 StepItem oneof 反射解析（唯一事实源）。
- Platform L3：`platform/{platform.go,blockdetect.go,xiaohongshu,douyin,xianyu}` 平台知识片段+拦截判据注入 prompt。
- NM-Host：`cmd/nm-host/main.go` (~488行) — NM 帧协议 + WS client + server_url 解析(ENV>conf>默认8204) + 出1MiB/入4MiB/读8MiB + req_id 捞回 + 版本锚点 1.5.0。

## 3. 扩展侧（MV3）

- `background/index.js` — SW: connectNative + dispatch(req_id→dispatch→回帧) + status 查询 + onStartup/onInstalled 锚点保活。
- `primitives.js` — executeScript func 序列化铁律（闭包丢失）；injClick probe(actionability: visible/stable/enabled/hit-target/box, rAF双帧stable 500ms, 后台tab setTimeout兜底)→CDP 真实事件→DOM兜底(双click禁令 session536/537)；injType probe→CDP insertText→DOM管线；injPostComment 三段式 prep(定位输入框+聚焦)/send(发送按钮坐标+CDP轨迹点击)/verify(norm回查)；click_unacked 归因（已下发未ack≠未下发，禁DOM二次点击）；后台tab CDP ack 尖刺 3.1s/条 → 写前置定位后置。
- `cdp/input.js` — chrome.debugger trusted：withDebugger(attach1.3+idle detach+already-attached容忍)+withDeadline+bezierPoints/clickJitter/moveStartPoint/对数正态时序(@hivemtk/browser-core 同源)；USKeyboardLayout 子集 KEY_DEFS；deadline 常量见 §2。
- `accessibility.js / tab-manager.js / native-messaging.js / api-client.js / render.js / constants.js / popup/index.js` — 快照@eN装配、tab稳定、NM端口、API客户端、渲染、常量、popup 状态页。

## 4. 数据与观测

- 表：browser_tasks / sessions / steps(含台账三列) / llm_plans / command_logs / audit_digests / cron_triggers / write_claims；终态/台账/审计三线可查；SessionExport 打包。
- 超时/预算/帧限全部单表化（service/timeouts.go + 扩展 deadline + host 常量），改值需真机回归。
- 自愈/卫生：Host 病连接自愈、抖动互顶拒绝、SW冷启探针、重试落列、Retention 裁剪、ledgerGap 降级多拦。

## 5. 与 JEV / computer-use 的差距（给步骤4用）

- 现行观察 = 文本快照 snapshot/markdown（@eN refs）+ extract/query 结构化读；**无截图→坐标 grounding、无像素动作**。
- 现行动作 = CSS/@eN 选择器 + CDP trusted 鼠键（Input.dispatch 系列）；**无 OS 级 mouse/keyboard、无像素点击、无拖拽轨迹通用化**。
- 现行校验 = 文本回查 verify + judge LLM；**无视觉 diff、无执行前后截图对比**。
- JEV 缺口 = 需要新增「截图→VLM grounding→视口坐标→CDP 点击/输入→视觉复核」闭环（详见步骤四设计文档），现有 probe→CDP→verify 骨架可复用为 JEV 执行壳。
- 全库 grep `JEV|jev|ultrafast` 零命中（CDP 命中仅为现有 trusted 通道），确认步骤一基线无 JEV 残留。

## 6. 关键文件索引

- 后端路由/服务/模型/DTo/平台/NM-Host：见 §2。
- 扩展：`user-web/browser_automation/src/{background/index.js,core/{primitives.js,cdp/input.js,accessibility.js,tab-manager.js,native-messaging.js,api-client.js,render.js,constants.js},popup/index.js}`。
- 前端 API：`user-web/src/api/browserAutomation.js`。
- 设计史：`docs/` 下 2026-09-19 优化与 write-ledger 批1–批13。

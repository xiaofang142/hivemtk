# 本项目增加 JEV 逻辑的架构与开发设计（步骤四）

> 日期 2026-09-30 / 状态：已批准实施（v1 范围）/ 前置：步骤一（本系统无 JEV 基线）/ 步骤二（JEV 本质与 CUA 链路）/ 步骤三（jev-ultrafast 全链路）
> 铁律：① 默认关闭——未配置即零行为变化；② Jev selects, driver executes, app verifies；③ D7/写台账/审计/重试闸门包住 JEV，JEV 只提名、绝不直发不可逆动作。

## 1. 融合点（一句话）

Brain 轮内"整轮 plan（LLM 输出多步 JSON）"替换为"JEV 单步 choice（CLICK/SCROLL/WAIT/DONE/BLOCKED + 目标 @eN）"，
失败/未配置时回退原 Brain 路径。规划、字段文本、验收仍归大 LLM。

```
现行 Brain 轮： snapshot → GeneratePlanReflect(大LLM, 多步) → 逐 步 dispatchStep(probe→CDP→verify)
JEV 轮（新增）： snapshot → JevClient.Choose(毫秒 choice) → 单步 dispatchStep(同一条执行壳) → history → 下一轮 observe
                                                     ↓ 任何失败
                                              回退 GeneratePlanReflect（原路径逐字保留）
```

## 2. 架构增量（只加一文件 + 一处接线）

```
service/jev.go（新建，唯一新增生产文件）
├── JevConfig + loadJevConfig（env：BROWSER_JEV_ENABLED / BROWSER_JEV_ENDPOINT / BROWSER_JEV_API_KEY / BROWSER_JEV_MODEL）
├── parseSnapshotElements（解析现有 @eN 快照行 `role "name" @eN`，上限 250，只取可点击类 role）
├── buildJevState（page{url,title,text≤6000} + elements + recent_actions[-10:]）+ fingerprintState（sha256）
├── JevClient.Choose（POST {endpoint} {model,state,questions}，timeout 25s；429/529/503 退避 0.5*2^n×3，照抄 ultrafast）
├── validateChoice（5 项，照抄 model.py：choice∈ids / probs 键==ids / 值∈[0,1]有限 / sum≈1±0.02 / choice==argmax；
│   非法 → "invalid TypeSafe response; no action executed"，回退 Brain）
└── decisionToSteps（CLICK→click @eN / SCROLL_DOWN|UP→scroll / WAIT→wait 500ms / DONE→done=true / BLOCKED→中止；
    未知 operation → 拒绝回退；TYPE_TEXT/select 首版不支持——JEV 不产字段文本，表单填写仍走 Brain，见 §5）

executor.go（唯一改动点：executeBrain 轮内）
└── planRound(ctx, task, session, snap, pageURL, st/history, seq)：
    JEV enabled+configured → Choose → 审计帧 judge/jev_decision → 返回 stepsJSON/done
    否则/失败 → 原 GeneratePlanReflect 调用（逐字保留，含 token 预算累计）
```

不碰清单：dto 白名单、D7 闸门、写台账 guardResubmit/claimWriteSlot、hand、host_registry、扩展 JS、快照格式。
v1 复用现有 `@eN` 快照做元素表（accessibility.js 行协议已稳定：`button "登录" @e1` / `*button "发送" @e3`）。

## 3. 问题单（v1 实施项）

| # | 问题 | 方案 | 落点 |
|---|------|------|------|
| J1 | JEV 服务不可达/未配置 | fail-closed 回退 Brain；Choose 错误只 Warn | planRound |
| J2 | JEV 输出非法（幻觉 id/概率） | validateChoice 5 项全过才执行，否则回退 | jev.go |
| J3 | JEV 选了写步（CLICK 点到发送按钮） | 不特殊处理——现有写步判定（classifyStepEffect→is_write→禁重试+台账+D7）对 JEV 产出的步同样生效；JEV 提名≠放行 | executeStepWithRetry（零改动） |
| J4 | 快照过期（choose 与 act 之间页面变了） | v1：choose 紧邻本轮 snapshot 消费；decision 一次性消费；dispatch 既有 probe 失败→自愈/失败收口 | planRound + 既有链路 |
| J5 | 审计可追溯 | 成功 choice 落 judge 帧 `jev_decision{operation,target,confidence,latency_ms}`；回退落 event 帧 `jev_fallback{reason}` | planRound |
| J6 | 配置面 | env 四件套（沿用 llm.go 的 env 覆盖惯例）；默认关闭；key 不进 trace/command_log | jev.go |

## 4. 安全论证

- JEV 输出仅为 id（eN/controls），永不直变成 selector/坐标/shell/JS——decisionToSteps 是 white-list 映射，未知 operation 拒绝。
- TYPE_TEXT 不进 v1：执行器绝不编造字段文本（ultrafast field_text 教训），表单值仍由 Brain plan 的 value 提供。
- DONE 仍走独立 judge（JudgeDone 重拍快照验收），JEV 自称完成≠真完成。
- 循环上界沿用 maxBrainIterations=40 + loopFingerprint nudge。

## 5. 明确不做（v1）

- 扩展侧 snapshot.js 化改造（guards/page_key/marker/fingerprint 进扩展）——v1 在 Go 侧解析现有快照；视 v1 收益再定。
- JEV 选 fill/select 目标与小 LLM 写字段值（pending_text 复用）——留 v2。
- 后台 tab 保活（setFocusEmulationEnabled）与 2 帧等待优化——现行 CDP 链路不动。
- B 链路（bridge）不引入 JEV。

## 6. 验收口径

- `go vet ./internal/browser_automation/...` 零输出；`go test ./internal/browser_automation/service/ -count=1` 全绿。
- 新增单测：快照解析（含 `*` 新元素标记/畸形行跳过/250 截断）、validateChoice 正反 6 例、operation 映射 6 例、未配置回退（fake brain 断言走到 Brain）、fake HTTP server 端到端 Choose（含非法响应回退）。
- 默认关闭验证：无 env 时行为与改前一致（既有全量用例即回归网）。

## 7. v1.1 统一调度口径（2026-09-30，chunks 1–4；Q1 决议：JEV 走 LLM provider 统一配置）

- **传输迁移**：删除 TypeSafe HTTP 直调（chooseOnce/jevQuestion/jevMaxRetries/jevHTTPTimeout）；
  `JevClient` 改持 `jevDispatcher` 接口（默认 `llm.GetGlobalDispatcher`），`Choose` 经
  `DispatchStructured` 调 `jev_choice` scenario（5s ctx 超时 + `isRetryableLLMError` 口径 1 次重试，
  非法答案不重试）。prompt 文本化（`buildJevChoicePrompt`：目标 + 操作候选 + 可点候选 +
  `<page_snapshot>` 隔离 + recent_actions），同体解析 `jevChoiceAnswers→jevDecisionFromAnswers`
 （沿用 validateChoice 五项 + CLICK 无候选拒绝），tokens 实计返回。
- **Scenario 路由**：dispatcher 新增 `ScenarioJevChoice` + 双路径路由
 （MaxLatency 5000 / MinQuality 0.7；default 走 deepseek→qwen-turbo，localFirst 走 prim→fallback）。
- **planRound 新签名**：`(ctx, task, session, snap, pageURL, st, history, seq, js) (planOutcome, error)`；
  `planOutcome{stepsJSON, done, terminal, brainTokens, jevTokens, jevAttempted, jevOK}` 聚合双路径口径。
- **新鲜度**：Choose 后 `e.hand != nil` 时重拍 snapshot 比指纹（`fingerprintState`），stale 记
  `jev_stale` 帧回退 Brain；`hand == nil`（单测）跳过。`isJevStale` 纯函数，空指纹 fail-soft 放行。
- **Session 熔断/独立预算**：`jevSessionState{off, fails, tokensUsed}` 由执行循环持有（Executor 进程级
  单例不放计数，随 task 结束丢弃）；`attempted && !ok` 连续 ≥3（`jevMaxSessionFails`）或 JEV token
  超 20000（`jevSessionTokenBudget`，实测单轮 ~15）即 `off`，剩余轮次直走 Brain。
  执行器：`tokenUsed += brainTokens`（原 P1-1 总预算，JEV 不再记 0），`jevSess.tokensUsed += jevTokens`。
- **配置变更**：`loadJevConfig` 去 key 门槛（`Enabled` = 开关）；`Ready = Enabled && dispatcher != nil`；
  `BROWSER_JEV_ENDPOINT` 降级为可选 OpenAI-compatible 覆盖（幂等注册 `jev_env_override` + SetRoute，
  Model-only 告警忽略）；无可用 provider 时调度报错 → planRound 回退 Brain（fail-closed 在调度层）。
- **成本行**：`BrainService.recordJevPlan(kind=jev_choice)` 落库，不碰 `lastAuxTokens`
  （执行器用 `LastAuxTokens` 累 judge 预算，JEV 走独立口径）。
- **验收增量**：`TestIsJevStale`、`TestPlanRoundJevOffSkipsJev`；既有 6 处 planRound 调用方切新签名
  （末参 js，现有测试传 nil）；`go vet` 干净、`gofmt` 干净、service 整包回归绿
 （需 `POSTGRES_TEST_PORT=8232` + `.env` 的 `POSTGRES_PASSWORD`，264s）。

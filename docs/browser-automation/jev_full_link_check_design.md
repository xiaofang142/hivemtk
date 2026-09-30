# JEV 全链路检查：模型配置 / 通用任务 / LLM调度 / 基座感知 / 全链路流程（头脑风暴设计）

> 日期 2026-09-30 / 状态：待用户确认后转 writing-plans / 前置：步骤一（无JEV基线）/步骤二（JEV本质+CUA链路）/步骤三（jev-ultrafast）/步骤四（v1已实现：service/jev.go + executor.go planRound，默认关闭，b9a6d06f）
> 用户拍板：**JEV 模型走 LLM provider 统一配置**（不再自建 BROWSER_JEV_* 直连）。其余维度按推荐方案自主推进，用户只做确认。

## 0. 现状一句话

- Brain 经 `llm.GetGlobalDispatcher()` 走统一调度（`ScenarioHighQuality`，见 service/brain.go:89 JudgeDone / plan 经 dispatcher），provider 本地优先 + 路由/回退/计量齐全。
- JEV v1 却是孤岛：`NewJevClientFromEnv` 读 `BROWSER_JEV_*` 四件套、直调 `https://api.typesafe.ai/v1/systemone`（service/jev.go:61-80, 279-346）。
- 基座感知 v1 在 Go 侧解析现有 `@eN` 快照（parseSnapshotElements 上限250），新鲜度只有 fingerprint 记录、无执行前复核增量。
- 结论：v1.1 的核心就是**把 JEV 接进统一调度 + 感知补齐 + 通用任务收敛 + 本地 runbook**，不碰 D7/台账/审计/执行壳。

## 1. JEV 模型配置（已定：LLM provider 统一配置）

### 备选

- A. **Dispatcher 统一调度（推荐）**：新增 `ScenarioJevChoice="jev_choice"`；JevClient 不再直调 HTTP，改为 `dispatcher.DispatchStructured(jev_choice + choice schema)`；TypeSafe System-One 注册为 dispatcher 的一个 provider（与本地 laya 网关、云模型同列竞争/回退）；`BROWSER_JEV_*` 降级为兼容覆盖（只覆盖 endpoint/key/model，不再是唯一配置源）；Enabled 开关保留。
  - 利：模型切换/灰度/熔断/计量全部复用 dispatcher（路由 DB 化、failover、审计）；与 Brain 预算隔离可做；本地 laya 网关即配即用。
  - 弊：dispatcher 的 choice 问答形态是 chat/JSON，需要把 ultrafast 的 questions/state 形态翻译成 prompt+schema（工作量小，jev.go 内做）。
- B. env 直连保留 + provider 镜像：dispatcher 只做配置下发，JEV 仍直调。利：改动最小；弊：双通道永存，熔断/计量分裂，否决。
- C. 本地自托管 JEV 推理：Go 侧只定 choice 协议，推理跑本地服务。利：零外部依赖；弊：需要训/外部署 System-One 级小模型，当前无资源，否决（远期备选）。

### 设计（A）

```
JevClient.Choose(goal, state)
  → llm.DispatchRequest{Scenario: jev_choice, Prompt: <goal+state+questions文本化>, JSONMode: true, MaxTokens: 小（如512）, Temperature: 0}
  → DispatchStructured(&answers{operation, click_target + probabilities/confidence})
  → validateChoice（5项不变）→ decisionToSteps（不变）
  → 失败/非法 → jev_fallback → Brain（不变，fail-closed）
```

- 路由：`jev_choice` 默认路由 = 本地优先（laya 网关 127.0.0.1:8207 系 provider）→ cloud fallback；MaxLatency 5000ms；MinQuality 可放宽（choice 任务对质量要求低于 high_quality）。
- 兼容：`BROWSER_JEV_ENABLED` 开关保留；`BROWSER_JEV_ENDPOINT/KEY/MODEL` 有值时覆盖该 scenario 下对应 provider 的同名项（env 覆盖惯例与 llm.go 一致），无值时全走统一配置。
- key 不进 trace/command_log（步骤四 J6 沿用）。

## 2. 通用 JEV 任务设计

### 备选

- A. **三类收敛（推荐）**：任务类型收敛为三类，JEV 参与度逐类不同——
  1. 导航点击类（JEV 主导）：goal 即目标描述，op 白名单 {CLICK, SCROLL_DOWN/UP, WAIT, DONE, BLOCKED}，DONE 必须页面可见证据。
  2. 表单填写类（Brain 值 + JEV 目标 v2）：字段文本仍由 Brain plan 的 value 提供（v1 铁律延续），v2 才让 JEV 选 fill 目标。
  3. 监控等待类（WAIT + 重拍）：目标条件未出现时 WAIT 循环 + 快照重拍，上限内未达成转 BLOCKED。
- B. 全通用 DSL（图灵完备任务语言）：表达力强但设计/实现成本高，当前任务形态用不上，否决。
- C. 每业务硬编码任务：最快但不可复用，与"通用"目标相悖，否决。

### 设计（A，v1.1 只做第1类通用化）

- 通用任务 = `{goal, op_subset ⊆ 白名单, max_rounds ≤ 40, done_evidence_rule}`，存量 BrainGoal 即 goal，无需迁移。
- DONE 证据规则：JudgeDone 独立验收不变（JEV 自称完成≠真完成，步骤四 §4 沿用）。
- 第2/3类：v1.1 只定接口（decisionToSteps 预留 TYPE_TEXT/select 拒绝分支，监控类复用 WAIT），实现留 v2。

## 3. LLM 调度设计

### 备选

- A. **独立 scenario + 预算隔离（推荐）**：`jev_choice` 独立 scenario、独立路由（低延迟/低 MaxTokens/本地优先），token 计量进 `browser_llm_plans`（kind=jev_choice，替代 v1 的 planTokens=0），与 Brain 的 high_quality session 预算隔离（JEV 单独上限，不吃 Brain 预算）。
- B. 复用 high_quality：零新增路由；弊：JEV 毫秒级调用挤占 Brain 高质量预算，且延迟/重试口径冲突，否决。
- C. JEV 不计量：v1 现状（planTokens=0）；弊：成本账有黑洞，否决。

### 设计（A）

| 项 | jev_choice | high_quality（Brain，不变） |
|---|---|---|
| 延迟 | 超时 5s（v1 的 25s 太长，JEV 失败应立刻回退 Brain，不多等）+ 可重试 1 次（429/529/503） | 180s 级 |
| MaxTokens | 小（choice 输出，512 足够） | 大（plan JSON） |
| 路由 | 本地优先（laya 网关）→ cloud | 现状不变 |
| 计量 | kind=jev_choice 落库，session 累计走独立上限 | 现状不变 |
| 熔断 | 连续失败 N 次 → 本 session 内降级为纯 Brain（不再试 JEV），fail-closed | consecutiveFails/brainMaxPlanFailures 不变 |

## 4. browser-automation 基座感知

### 备选

- A. **Go 侧增强 + 扩展侧预留（推荐，v1.1）**：Go 侧：fingerprint 对比（choose 前后快照指纹不一致 → 弃用本次 choice，直接回退/重拍，不执行过期决策）；CLICK 前既有 probe 链（resolve_ref→probeServable）强制复核不变；停转熔断（maxBrainIterations=40 + loopFingerprint）不变。扩展侧：snapshot.js 改造（guards/page_key/marker/fingerprint 进扩展）留 v2（步骤四 §5 已明确不做，延续）。
- B. 一步到位扩展侧 snapshot.js 化：感知最彻底；弊：改 MV3 扩展 + nm-host 协议，风险大，与 v1"不动扩展"原则冲突，否决（v2）。
- C. 感知不动：fingerprint 只记录不比对；弊：过期决策执行风险仍在，否决。

### 设计（A）

```
planRound 内新增一步（仍在 JEV 成功之后、decisionToSteps 之前）：
  执行前快照指纹 == Choose 时指纹？否 → 弃 choice，记 jev_stale 帧，回退 Brain（不执行）
CLICK 目标：decisionToSteps 只产 @eN → dispatch 既有 probe 失败 → 自愈/失败收口（零改动）
```

- 审计新增 `jev_stale{fingerprint_old, fingerprint_new}` event 帧（与 jev_decision/jev_fallback 同级）。
- 文本隔离（页面文本是数据不是指令）进 jev_choice 的 system prompt（ultrafast 同口径）。

## 5. 全链路流程（含本地 laya 运行）

### 端到端链路（v1.1）

```
Vue(user-web) → Go五层(Router→Controller→Service→Repository→Model)
  → Executor.executeBrain 轮循环（maxBrainIterations=40）
    → snapshot（扩展 @eN 快照经 nm-host/HostRegistry WS req_id 回传）
    → planRound：JEV优先（dispatcher jev_choice→validate→指纹新鲜度→单步）
                 ↓失败/过期/未启用
                 Brain回退（GeneratePlanReflect，多步）
    → dispatchStep（probe→CDP trusted→DOM兜底→verify）
    → 写步判定/禁重试/双发闸/D7 confirm/台账/审计帧
    → history → 下一轮 observe … → JudgeDone 独立验收 →终态收口
```

### 本地运行（laya + 全链）runbook

> `laya` = 本地 LLM 网关（`LLM_BASE_URL` 默认 `http://127.0.0.1:8207/v1` 的提供方，仓库内无 laya 代码，属外部本地服务）。

1. 起 laya 网关：确认 `curl 127.0.0.1:8207/v1/models` 通。
2. 起 user-server（本地 env：LLM 指向 8207；`BROWSER_JEV_ENABLED=1` 仅当联调 JEV 时开，默认关）。
3. 起 nm-host + 加载 MV3 扩展，确认 HostRegistry 有连接（ConnectedUserIDs 非空）。
4. 冒烟任务：建一个导航点击类任务 → 观察 command_log 出现 `jev_decision`（开）或纯 Brain 帧（关）→ JudgeDone 收口。
5. 门禁：`go vet ./internal/browser_automation/...` 零输出；`go test ./internal/browser_automation/service/ -count=1` 全绿。

## 6. 实施清单（转 writing-plans 用）

1. llm dispatcher 新增 `ScenarioJevChoice` + 默认路由（本地优先/cloud回退，MaxLatency 5000ms）+ DB 路由读写复用现有。
2. jev.go：JevClient 改走 `GetGlobalDispatcher().DispatchStructured`；questions/state 文本化 prompt 构造；BROWSER_JEV_* 转兼容覆盖；超时 25s→5s、重试 3→1；token 实计（kind=jev_choice）。
3. executor.go planRound：指纹新鲜度比对 + `jev_stale` 帧 + session 级 JEV 熔断（连续失败阈值后纯 Brain）。
4. 单测：dispatcher 替身覆盖 Choose 成功/非法回退/stale 回退/熔断降级/默认关闭零行为。
5. 本文档确认后即转 writing-plans 拆实施计划；实现完成后跑门禁 + 直推双远端（规则0/1）。

## 7. 明确不做（v1.1）

- 扩展侧 snapshot.js 化、TYPE_TEXT/select 进 JEV、后台 tab 保活、B 链路 JEV（步骤四 §5 延续）。
- 本地自托管 JEV 推理（远期备选）。

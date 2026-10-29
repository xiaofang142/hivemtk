# 007 · D2 AI 全链路可观测性 + B2 渠道健康监控仪表盘

> 状态：调研完成 → 设计完成 → 实现完成
> 泳道：D2 + B2 合并交付（两者共享「运营看板」这一落点）
> 前置依赖：B1-a（选择器失效检测，`specs/006`）已交付，本规格直接消费其读数。

---

## 1. 目标与非目标

### 1.1 目标

给运营/运维一个**统一看板口径**的最小可用闭环（读路径，不写业务表）：

- **D2（AI 全链路可观测性）**：AI 成本 / 送达率 / 转化漏斗三块统一口径。
- **B2（渠道健康监控）**：渠道在线率 / 封禁信号两块统一口径。

### 1.2 非目标（明确不做）

- 不新写业务表：全部读既有表（见 §3），缺数据源的口径只做「定义 + 落点设计」。
- 不做告警推送 / 阈值配置化：阈值先用包级常量，接参数中心是后续增量（见 delivery）。
- 不做写路径：本泳道只交付读看板，扩展上报 / trace 采集等写路径由既有代码承担。
- 不碰菜单库（`menus` 表）：前端看板页挂载点写进 delivery，由主智能体统一接菜单。

---

## 2. 调研取证（回仓读码，截至 2026-10-24）

### 2.1 D2 现状：obs_config 不是观测配置，是对象存储

计划文档原文称「`internal/service/obs_config*.go`（3 个文件）+ `internal/monitor/` 已有观测底座」。
**取证结论：这句话的前半句是错的。**

- `internal/service/obs_config.go`（连同 `obs_config_batchm_test.go`、`obs_config_batchm3_test.go`）是
  **OBS（对象存储配置）**服务：GetConfigList / UploadFile / SetDefaultConfig / TestConnection，
  与「可观测性」无关。计划文档把 OBS 当成了 observability 的缩写。
- 真正的观测底座在 **`internal/monitor/`**（25KB `monitor.go` + `handler.go`），已具备：

| 能力 | 函数 | 数据源 | 缺口 |
|---|---|---|---|
| 业务健康概览 | `HealthOverview` | message_hub / message_trace | 只有**全局**计数，无按渠道拆分；无成本 |
| 异常分组 | `Anomalies` | message_hub / inbox_conversations / message_trace | 同上 |
| 节点健康窗口 | `NodeHealthByChannel` | message_trace | 有渠道维度，但粒度是「节点」不是「渠道运营态」 |
| 生命周期还原 | `Lifecycle` / `TraceTree` / `Traces` | message_trace | 单会话下钻，非聚合看板 |
| 端到端时延 | `LifecycleLatencyByChannel` | message_trace | 有渠道维度 |

- **缺的正是统一看板口径**：成本（谁花的、花在哪、多少 token）无聚合读数；
  送达率只在 `HealthOverview` 里有 delivered/failed 两个**全局**数字，没有按渠道的口径与分母定义；
  转化漏斗没有「阶段计数 + 阶段转化率」的聚合面。

### 2.2 B1-a 读数源（本规格要复用的底座）

`internal/service/selector_health.go` + `repository/bridge_selector_health.go` + `model/bridge_selector_health.go`
已交付「服务端判定面」，判据顺序（`ClassifySelectorHealth`）：

1. `attempts < 20` → `warming`（样本不足）
2. `hits == 0` → `broken`（选择器彻底失效：平台改版/封禁强信号）
3. `hitRate < 阈值(默认60)` → `degraded`
4. 否则 `ok`

- 阈值走参数中心 `bridge.selector_health_degraded_below`（`Threshold(ctx)` 读，代码兜底 60）。
- 读侧：`SelectorHealthService.List(ctx)` 返回 `[]SelectorHealthView`（channel / field / attempts / hits /
  hit_rate / status / last_report_at / stale_seconds）。
- 本规格直接复用 `ClassifySelectorHealth` 与 `bridge_selector_health` 表读数做渠道级聚合判定，
  **不新写任何 selector 判定逻辑**——判据与阈值已收敛在 B1-a 服务端，这里是消费方。

### 2.3 B2 现状：节点健康窗口 + 巡检脚本，缺运营看板

- `internal/monitor/monitor.go` 的 `NodeHealthByChannel` 是「每渠道每节点的响应时间/异常率」，
  数据源 `message_trace`，窗口走参数中心 `telemetry.node_health_window`（兜底 24h）。
- `scripts/bridge-monitor.sh` 是**命令行巡检脚本**（日志 + DB 两路判 FAIL），不是运营看板。
- **缺**：面向运营的「渠道在线率 / 封禁信号」看板。
  关键取证：渠道在线的正确口径**不是** `bridge_accounts.status='online'`（见下）。

### 2.4 渠道在线率的关键取证（必须读，否则量出错的在线数）

`internal/repository/bridge_online_predicate.go` 写明：

```
status <> 'offline' AND last_sync_at IS NOT NULL
  AND now() - last_sync_at < (grace * interval '1 second')
```

为什么不能按 `status='online'` 数：那一列只在 SSE **正常收尾**时才被 SetOffline 改回离线，
扩展崩溃/浏览器被杀/断网都不走那条路径 → 列粘在 online 上（实测某实例 155 行里 152 行标 online，
按 last_sync_at 判定的真值是 0）。宽限窗口来自 `config_params(bridge/online_grace_window)`
（默认 30s，见 `service/config_param.go` 的 `BridgeOnlineGraceWindow`）。
**本规格的在线率必须走同一条 predicate + 同一个参数**，否则与渠道总览/主动触达/离线回扫三个读侧
量出四个不同的「在线数」。

### 2.5 数据源盘点（本规格可消费的既有表）

| 口径 | 数据源 | 关键列 | 备注 |
|---|---|---|---|
| AI 成本 | `llm_routing_logs` | cost / prompt_tokens / completion_tokens / latency_ms / success / provider / model / scenario / created_at | 每次 Dispatch 落一条，已有聚合先例 `repository/ai_performance_repo.go` |
| 送达率 | `message_hub` | platform(=渠道) / direction / status(delivered·failed·pending·acked) / created_at | 出站行 `direction='outbound'` |
| 转化漏斗 | `message_trace` | node(ingest·ai_dispatch·outbound_enqueue·inbox_sync·downlink_fetch·delivered_ack) / trace_id / created_at | 生命周期 span；agent_turn/tool_call 挂在 ai_dispatch 下、node 值不同，天然被 node IN(...) 排除 |
| 渠道在线率 | `bridge_accounts` | channel / status / last_sync_at | 在线判据见 §2.4 |
| 选择器健康 | `bridge_selector_health` | channel / field / attempts / hits / last_report_at | B1-a 已交付 |
| 封禁信号（域名） | `domain_pool` | on_blacklist / health_score / is_active | G 域扩展已建 |

---

## 3. 统一看板口径（本规格的交付物定义）

一个聚合端点：`GET /api/observability/dashboard?window=7d`（`window ∈ 24h|7d|30d`，默认 7d）。

### 3.1 D2 · AI 成本口径

- **口径**：窗口内 `llm_routing_logs` 按 (provider, scenario) 聚合：
  调用数 `calls`、成功数 `success_calls`、prompt/completion token、`cost` 合计、`latency_ms` 合计。
- **派生**：`avg_latency_ms = latency_ms / calls`（calls=0 → 0）；按 provider、scenario 拆两组。
- **落点**：全部来自既有表 `llm_routing_logs`，**零新表**。成本为 0 是合法读数
  （本地模型 / 未接计费口径时 cost 列本就是 0），看板不把「0 成本」渲染成异常。

### 3.2 D2 · 送达率口径

- **分母**：窗口内出站「已了结」行 = `delivered + failed`（`direction='outbound'`）。
- **分子**：`delivered`。
- **在途**：`pending + acked`（未了结，不计入送达率分母，单列展示——把在途塞进分母会让送达率
  在消息发出瞬间被人为压低）。
- **送达率** = `delivered / (delivered + failed) × 100`，保留一位小数；分母为 0 → 0（不是 NaN）。
- 全局一条 + 按渠道各一条（`platform` 分组）。

### 3.3 D2 · 转化漏斗口径

- **阶段**（`message_trace` 生命周期节点，顺序固定）：`ingest → ai_dispatch → outbound_enqueue →
  inbox_sync → downlink_fetch → delivered_ack`。
- **阶段计数** = 窗口内该 node 的 `COUNT(DISTINCT trace_id)`（一轮 trace 只计一次，不按 span 行数）。
- **阶段转化率** = 本阶段 / 上一阶段 × 100，保留一位小数；上一阶段为 0 → 0。
- **落点**：全部来自 `message_trace`，零新表。

### 3.4 B2 · 渠道在线率口径

- **在线数** = 该渠道 `bridge_accounts` 里满足 §2.4 predicate 的行数（宽限窗口读
  `service.BridgeOnlineGraceWindow(ctx)`）。
- **在线率** = 在线数 / 账号总数 × 100，保留一位小数；账号总数为 0 → 0，渠道状态记 `empty`（未接入，不算异常）。

### 3.5 B2 · 封禁信号口径

两级信号，都来自既有表：

1. **渠道级（强信号）**：该渠道 `bridge_selector_health` 全部字段的「最差判定」为 `broken`
   （B1-a 口径：样本够了且一次都没命中 = 选择器彻底失效 = 平台改版/封禁改了页面结构）。
2. **域名级**：`domain_pool` 里 `on_blacklist=true` 的域名数（G 域黑名单检测已维护）。

### 3.6 B2 · 渠道状态判定（顺序有意义，同 B1-a 风格）

```
ClassifyChannelHealth(accounts, online, worstSelector, brokenFields):
  accounts == 0                        → empty    未接入渠道，不算异常
  worstSelector == broken              → banned   封禁/改版强信号（选择器全失效）
  online == 0                          → offline  全离线
  worstSelector == degraded            → degraded 选择器部分失效
  brokenFields > 0                     → degraded 存在 broken 字段（局部改版）
  其余                                → healthy
```

在线率只做**展示**，不进状态判定——避免「在线率低」与「选择器坏」两套判据打架：
在线率低的原因可能正是选择器坏了，再叠一条阈值会让运营看到两个互相矛盾的红。

---

## 4. 交付范围与验收口径

### 4.1 后端（user-server，新增 5 文件 + 1 行接线）

| 层 | 文件 | 内容 |
|---|---|---|
| repository | `internal/repository/observability_dashboard.go` | 只读仓储：LLMCost / Delivery / Funnel / ChannelAccounts / SelectorHealthRows / DomainBanSummary |
| service | `internal/service/observability_dashboard.go` | 口径聚合 + 状态判定 + 纯函数（DeliveryRate/OnlineRate/ClassifyChannelHealth/FunnelConversion/windowDuration） |
| service test | `internal/service/observability_dashboard_test.go` | 纯单测（不连库），表驱动 |
| controller | `internal/controller/observability_dashboard.go` | `GET /api/observability/dashboard`（带 @Router/@Tags 注解） |
| router | `internal/router/observability_dashboard_routes.go` | `RegisterObservabilityDashboardRoutes(auth, gormDB)` |
| 接线 | `internal/router/router.go` 在 `monitor.RegisterRoutes(auth)` 后加一行 | 只读路由，登录态 |

### 4.2 前端（user-web/src，新增 3 文件 + 1 行白名单）

| 文件 | 内容 |
|---|---|
| `src/api/observabilityDashboard.js` | API 调用层 |
| `src/views/observabilityDashboard/Index.vue` | 最小看板页（成本/送达/漏斗/渠道健康四卡 + 渠道表格） |
| `src/router/modules/observabilityDashboard.js` | 独立路由模块 |
| `src/router/index.js` 的 `moduleNames` 白名单 +1 项 | 挂载点（菜单树由后端 `menus` 表驱动，由主智能体接） |

### 4.3 验收口径

1. `go build ./...` 绿；`gofmt -l` 对新增文件干净；`go vet` 对新改包干净。
2. 纯单测覆盖：送达率/在线率/转化率/渠道状态判定的边界（分母 0、broken→banned、offline、
   未接入 empty、阈值边界、window 解析）。
3. 看板端点只读、不写任何业务表；复用 `bridgeOnlineSQLPredicate` 与
   `service.BridgeOnlineGraceWindow` 的同一口径。
4. 前端为独立页面 + 独立路由模块，不改动他人正在改的文件；挂载点写进 delivery。

---

## 5. 决策点（在 spec 里先钉住，delivery 里复核）

- **D1 不在本泳道**：转化漏斗只做到「AI 全链路（trace 生命周期节点）」，不延伸到
  opportunity→quote→bill（那是 D1 的 Lead-to-Cash，数据源不同、且正被另一 lane 改动）。
- **新表 = 0**：所有口径都有既有数据源，不需要新表 → 不需要动 `module_tables.go`。
- **阈值不接参数中心**（~~本轮~~ **2026-10-29 已按后续增量接上**，见 delivery D6）：
  `ChannelOnlineDegradedBelow`（本轮未使用，见 §3.6）与窗口默认值当初用包级常量；
  现为 `telemetry.dashboard_default_window_hours` 与 `telemetry.channel_online_degraded_below`
  两条种子，代码常量只做兜底（与种子 DefaultValue 一致，gate `check-config-param-defaults` 已验）。
- **domain_pool 封禁是全局计数**：域名到渠道的映射不在现有数据里，本轮不做「域名→渠道」归属推断，
  只在看板域名区展示全局封禁数；渠道级封禁信号用 selector broken（§3.5 口径 1）。

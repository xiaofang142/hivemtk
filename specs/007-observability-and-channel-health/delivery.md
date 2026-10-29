# Delivery: D2 AI 全链路可观测性 + B2 渠道健康监控仪表盘

**Feature**: `007-observability-and-channel-health` ｜ **完成**: 2026-10-24 ｜ **Spec**: `./spec.md`

## 1. 改动文件清单

### 后端（user-server，全部新增，除 1 行接线）

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/repository/observability_dashboard.go` | 新增 | 只读仓储 6 方法：LLMCost / Delivery / Funnel / ChannelAccounts / SelectorHealthRows / DomainBanSummary |
| `internal/service/observability_dashboard.go` | 新增 | 统一口径服务：Snapshot 聚合 + DeliveryRate / OnlineRate / FunnelConversion / ClassifyChannelHealth / windowDuration 纯函数 |
| `internal/service/observability_dashboard_test.go` | 新增 | 7 个测试函数 / 23 个表驱动用例（纯单测，不连库） |
| `internal/controller/observability_dashboard.go` | 新增 | `GET /api/observability/dashboard`（含 @Router/@Tags 注解） |
| `internal/router/observability_dashboard_routes.go` | 新增 | `RegisterObservabilityDashboardRoutes(auth, gormDB)` |
| `internal/router/router.go` | 修改 +4 行 | 在 `monitor.RegisterRoutes(auth)` 后加一行接线（写前查过 git status：当时 clean） |

### 前端（user-web，全部新增，除 1 行白名单）

| 文件 | 动作 | 说明 |
|---|---|---|
| `src/api/observabilityDashboard.js` | 新增 | API 调用层 |
| `src/views/observabilityDashboard/Index.vue` | 新增 | 最小看板页：成本/送达率/漏斗/渠道健康 + 渠道表 + 空态渲染 |
| `src/router/modules/observabilityDashboard.js` | 新增 | 独立路由模块 |
| `src/router/index.js` | 修改 +1 行 | `moduleNames` 白名单加 `"observabilityDashboard"` |

### 规格四件套

`specs/007-observability-and-channel-health/{spec,plan,tasks,delivery}.md`（新增）。

### 未触碰（铁律核对）

- 未改：`CHANGELOG.md`、计划文档、`scripts/check-*`、`payment*.go`、`module_tables.go`、`specs/006*/**`、`.github/workflows/**`。
- **新表 = 0**：全部口径都有既有数据源（llm_routing_logs / message_hub / message_trace / bridge_accounts /
  bridge_selector_health / domain_pool），**不需要在 `module_tables.go` 登记任何归属**。

## 2. 跑了哪些命令及结果（关键输出）

```text
$ cd user-server && go build ./...
# 无输出 = 成功（BUILD OK）

$ gofmt -l internal/service/observability_dashboard.go internal/service/observability_dashboard_test.go \
    internal/repository/observability_dashboard.go internal/controller/observability_dashboard.go \
    internal/router/observability_dashboard_routes.go internal/router/router.go
# 无输出 = 全部干净（GOFMT CLEAN）

$ go vet ./internal/service ./internal/repository ./internal/controller ./internal/router
# 无输出 = 0 问题（VET OK）
```

纯单测（不连库，测试库被并发 lane 占用，遵守铁律 4）：

```text
$ go test ./internal/service -run 'TestDeliveryRate|TestOnlineRate|TestFunnelConversion|TestClassifyChannelHealth|TestWindowDuration|TestSnapshot' -count=1
ok  	hivemtk-user/internal/service	0.735s
```

7 个测试函数全 PASS，关键断言：
- `TestDeliveryRate_ZeroDenominatorIsZeroNotNaN`（5 例）：分母 0 → 0 不是 NaN；100/50/66.7 边界。
- `TestOnlineRate_ZeroAccountsIsZero`、`TestFunnelConversion_ZeroPrevIsZero`：分母 0 口径。
- `TestClassifyChannelHealth_OrderMatters`（8 例）：empty → banned → offline → degraded → healthy 顺序；
  broken（选择器全失效）必须最先判 banned；warming/missing 不构成信号。
- `TestWindowDuration_FallsBackTo7d`（6 例）：24h/7d/30d + 非法（空/1d/7D）回落 7d。
- `TestSnapshot_AggregatesAllSections`：六读侧聚合进一份快照，字段不串位（成本按 provider 累加、
  token 180、送达率 80%、漏斗 6 阶段顺序、douyin banned / xhs empty、域名封禁 2/5）。
- `TestSnapshot_NilRepoIsExplicitErrorNotPanic`：nil 仓储 → 显式 sentinel 错误。

前端静态检查：

```text
$ cd user-web && npx eslint --fix src/views/observabilityDashboard/Index.vue src/api/observabilityDashboard.js src/router/modules/observabilityDashboard.js
# 无输出 = 0 errors 0 warnings（--fix 前 0 errors / 78 warnings，均为 vue 格式警告，已全部清掉）
```

## 3. 决策点 / 未实现清单

| # | 决策点 | 结论与理由 |
|---|---|---|
| D1 | 计划文档原文「`obs_config*.go` 是观测底座」是**错的** | 取证：`obs_config*.go` 是 **OBS 对象存储**服务，与可观测性无关。真正的观测底座是 `internal/monitor/`。证据已写进 spec §2.1 |
| D2 | 渠道在线率**不能**按 `status='online'` 数 | 复用 `bridgeOnlineSQLPredicate` + `service.BridgeOnlineGraceWindow`（默认 30s），与渠道总览/主动触达/离线回扫三个读侧同一条谓词。证据：`repository/bridge_online_predicate.go` |
| D3 | 转化漏斗范围只做到「AI 全链路」 | 用 `message_trace` 六节点（ingest→delivered_ack）。**不延伸到** opportunity→quote→bill（D1 Lead-to-Cash，数据源不同且正被另一 lane 改动）。注意：`dashboard_sse_stats.go` 已有**销售漏斗**（customer_journey 陌生人→成单），与本漏斗口径不同、不冲突 |
| D4 | 封禁信号两级 | 渠道级 = selector broken（B1-a 判定，平台改版/封禁强信号）；域名级 = `domain_pool.on_blacklist`。域名→渠道映射在现有数据里不存在，本轮**不做归属推断** |
| D5 | 在线率不进状态判定 | `ClassifyChannelHealth` 只用「选择器最差态 + broken 字段数 + 是否全离线」，在线率只做展示——避免「在线率低」与「选择器坏」两套判据打架（在线率低的原因可能正是选择器坏了）。**2026-10-29 补充**：新增在线率降级档时按同一顾虑排序——选择器面有任何信号（broken/banned、degraded、broken 字段 >0）时先返回那一档，**只有选择器面无信号才看在线率**，两套判据永远不同时发话 |
| D6 | 阈值不接参数中心 | ~~留作后续增量~~ **2026-10-29 已接上（S-20）**：`telemetry.dashboard_default_window_hours`（兜底 168h，窗串按配置显示 N d/N h）与 `telemetry.channel_online_degraded_below`（兜底 90，`OnlineRate < 阈值` 判 degraded，全离线仍走 offline 档，非法值回落兜底）；`SelectorHealthDegradedBelow` 本来就复用 B1-a 参数。兜底常量与种子 DefaultValue 由 `check-config-param-defaults` + 读取点门/键规范门三道 gate 校验 |
| D7 | 送达率分母 | = delivered + failed（已了结）；pending/acked 单列为「在途」。把在途塞进分母会让送达率在消息发出瞬间被人为压低 |
| D8 | 成本 0 是合法读数 | 本地模型 / 未接计费口径时 cost 列为 0，看板显式渲染「暂无数据」而非异常（前端空态 + 注释已写） |
| D9 | 未跑真库测试 | 测试库被并发 lane 占用（铁律 4）。仓储 SQL 落地无真库用例，已在 delivery 声明；SQL 全部遵循 `dashboard_stats.go`/`ai_performance_repo.go` 的既有写法 |

## 4. 建议的主智能体接线点（本泳道不代做）

1. **菜单（后端 `menus` 表 + 前端 menu 驱动）**：前端路由已可直达 `/observability-dashboard`，
   但侧栏菜单树由 `GET /api/menus` 驱动。建议补一条菜单行指向该路由（title「AI 观测与渠道健康」，
   requiresAdmin 语义由前端 meta 声明，后端按需挂 capability）。
2. **路由策略收紧**：当前 `GET /api/observability/dashboard` 挂 `auth` 组（JWT），
   `RoutePolicyGuard` 对未入表路径放行（`module_gate.go` `noteUncovered` 仅告警）——
   任何登录角色可读。若要收紧，二选一：① 加进 `middleware/route_policy.go` 的 adminOnlyRoutes；
   ② 加进 `module_route_patterns.go` + 能力面表。**本泳道不碰这两份生成件/占用文件**。
3. **e2e 冒烟清单**：`tests/e2e/routes_user.tsv` 建议补一行 `GET /api/observability/dashboard`
   （`route_surface_consistency_test.go` 的 minE2EInventoryCoverage=738 是下界棘轮，不加行不会红，
   但补行能把它纳入冒烟面）。
4. **路由守卫测试**：`route_surface_consistency_test.go` / `route_policy_gate_test.go` 需要真库，
   本泳道未跑；新增路由带注解、带 JWT 守卫，形状符合两道门的判据（注解数/路由数是下界棘轮，只升不降）。
5. **前端 build**：本泳道只做了 eslint + node --check（避免 `vite build` 写 dist 与无关改动），
   建议主智能体在统一 build 时验证 `npm run build` 含新页面。

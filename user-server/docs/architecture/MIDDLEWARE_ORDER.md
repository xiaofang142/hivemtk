# user-server 中间件注册顺序规范

> 文档版本：v1（2026-08-27，R38 落地）
> 适用：`hivemtk/user-server/internal/router/router.go` 的 `Setup(r, gormDB)` 函数

---

## 1. 目标

中间件的**注册顺序**直接决定其**生效顺序**（Gin 按 `r.Use()` 调用顺序串联）。
错误的顺序会导致：

- 跨域/OPTIONS 请求先进入业务链路（CORS 头未生效）
- panic 在 trace_id 注入之前抛出 → 日志无法关联
- 越权访问在 metrics 之前被发现 → 监控指标缺数据
- recovery 在 metrics 之后注册 → panic 请求不会被计数

本文档固化**唯一允许的注册顺序**，作为后续 PR 的评审基线。

---

## 2. 标准顺序（从外到内）

```
incoming request
       │
       ▼
① trace           ← 注入 trace_id（最早，否则后续日志/panic 都无 trace_id）
       │
       ▼
② cors            ← 拒绝跨域不应计入业务指标
       │
       ▼
③ metrics         ← 即便后续 panic 也产出部分 metric
       │
       ▼
④ recovery        ← 包住所有业务（含 handler / service）
       │
       ▼
⑤ logger          ← 带 trace_id，与 metric exemplar 关联
       │
       ▼
⑥ ownership       ← 资源越权拦截（在业务 handler 之前）
       │
       ▼
⑦ handler         ← 业务入口
```

### 各中间件职责

| # | 中间件 | 文件 | 职责 | 关键点 |
|---|--------|------|------|--------|
| ① | trace | `internal/middleware/trace.go` → `TraceMiddleware` | 注入 / 透传 X-Trace-Id，写入 ctx | 必须是第一个，否则 panic 日志无 trace |
| ② | cors | `internal/router/router.go` → `corsMiddleware` | 反射 ACAO + OPTIONS 短路 | 在 metrics 前，避免 OPTIONS 请求污染指标 |
| ③ | metrics | `internal/middleware/metrics.go` → `MetricsMiddleware` | 计数 / 延迟 / in-flight | 在 recovery 前 → panic 也算一次请求 |
| ④ | recovery | `gin.Recovery` 或自定义 | panic 转 500 + 堆栈 | 必须包住 ⑤⑥⑦，否则后续链路 panic 击穿 |
| ⑤ | logger | `internal/middleware/api_logger.go` → `APIInteractionLogger` | 结构化访问日志 | 带 trace_id 与 metric exemplar 联动 |
| ⑥ | ownership | `internal/middleware/init_guard.go` + `bridge_ingress_guard.go` + `ratelimit.go` | 限流 / 凭证 / 归属校验 | 业务前置，越权请求不消耗业务资源 |
| ⑦ | handler | 业务 handler（由路由表调用） | 处理业务逻辑 | — |

> 说明：当前实现把"限流 / 鉴权 / 归属"统称为 ⑥ ownership 链，按需拆分（见 §4 实际代码分析）。

---

## 3. 与当前代码的对照

### 3.1 实际注册顺序（`router.go:Setup`）

按 `r.Use()` 调用先后整理（行号以 R38 修复前为准）：

| # | 行号 | 中间件 | 对应规范 | 是否符合 |
|---|------|--------|----------|----------|
| 1 | `router.go:200` | `corsMiddleware()` | ② | ❌ 应在 trace 之后 |
| 2 | `router.go:201` | `gin.Recovery()` | ④ | ❌ 应在 metrics 之后 |
| 3 | `router.go:203` | `middleware.LocaleMiddleware()` | （非核心链） | ⚠️ 应在 trace 之后注入 locale 到 ctx |
| 4 | `router.go:205` | `middleware.ContextMiddleware()` | （非核心链） | ⚠️ 注入业务 ctx |
| 5 | `router.go:216` | `middleware.RateLimitMiddleware(...)` | ⑥ | ⚠️ 见下 |
| 6 | `router.go:226` | `middleware.TraceMiddleware()` | ① | ❌ **必须第一个** |
| 7 | `router.go:228` | `middleware.APIInteractionLogger()` | ⑤ | ⚠️ 见下 |
| 8 | `router.go:230` | `middleware.AuditMiddleware()` | （非核心链） | OK |

### 3.2 已识别的不符合项（CI 应阻断）

#### ❌ 不符合 1：TraceMiddleware 注册过晚

- **现状**：trace 在第 6 位（`router.go:226`），意味着 cors / recovery / locale / context / ratelimit 五个中间件
  处理时**ctx 中没有 trace_id**，panic 日志、限流命中、locale 决策日志全部无法关联。
- **正确顺序**：trace 应在 `r.Use(corsMiddleware())` 之前。

#### ❌ 不符合 2：Recovery 在 Metrics 之前

- **现状**：`gin.Recovery()` 在 `router.go:201`，而 `MetricsMiddleware` **未被注册**。
- **正确顺序**：
  - recovery 包住所有业务 → panic 转 500，不击穿进程
  - metrics 在 recovery 之外 → 即便 panic 也能计数 + 产出 status=500 的请求
- **现状风险**：当前 metrics 缺失，部分 panic 请求无 metric。

#### ⚠️ 不符合 3：Locale / Context / RateLimit 在 trace 之前

- **风险**：ratelimit 命中时无法记录 trace_id（影响排查）；locale 决策无法 trace。
- **建议**：把这三者挪到 trace 之后注册。

#### ⚠️ 不符合 4：CORS 注册在 trace 之前

- **风险**：跨域被拒的请求 trace_id 缺失（虽然这些请求本就不进入业务）。
- **建议**：保持 trace 第一，但 CORS 第二（trace 注入 → CORS 头写回）。

### 3.3 完全符合的部分

- `gin.Recovery()` 在 handler 之前 ✅
- `AuditMiddleware` 在 handler 之前 ✅
- `JWT` / `RequireAdmin` 挂在路由组的 `auth.Use(...)` 内，属于业务组级，符合"⑥ ownership 之后 handler"原则 ✅

---

## 4. 建议修复顺序（R38 后续迭代）

```go
// 1. trace — 注入 X-Trace-Id 到 ctx（必须第一个）
r.Use(middleware.TraceMiddleware())

// 2. cors — 跨域处理
r.Use(corsMiddleware())

// 3. metrics — 即使后续 panic 也能产出指标
r.Use(middleware.MetricsMiddleware())

// 4. recovery — 包住业务
r.Use(gin.Recovery())

// 5. logger — 带 trace_id 的访问日志
r.Use(middleware.APIInteractionLogger())

// 6. ownership 链
r.Use(middleware.LocaleMiddleware())
r.Use(middleware.ContextMiddleware())
r.Use(middleware.RateLimitMiddleware(middleware.RateLimitConfig{...}))

// 7. handler — 业务入口（由路由表调用）
```

修改后请同步更新：

- `docs/architecture/MIDDLEWARE_ORDER.md`（本文档）— 标记"已修复 R38-N"
- CI 兜底：可在 `check_naked_goroutine.sh` 同目录下加 `check_middleware_order.sh`
  通过解析 `router.go` 中 `r.Use(...)` 调用顺序与本文档 §2 比对，不符则 fail。

---

## 5. 验证清单

每次修改 `router.go:Setup` 后必须自检：

- [ ] `r.Use(middleware.TraceMiddleware())` 是**第一个**业务中间件
- [ ] `r.Use(corsMiddleware())` 在 trace 之后
- [ ] `r.Use(middleware.MetricsMiddleware())` 在 recovery 之前（**注意当前缺失**）
- [ ] `r.Use(gin.Recovery())` 包住 handler 之前的所有中间件
- [ ] `r.Use(middleware.APIInteractionLogger())` 在 trace 之后，能读到 trace_id
- [ ] 所有限流 / 鉴权 / 归属中间件在 handler 之前

---

## 6. 已知缺口

| 缺口 | 影响 | 责任迭代 |
|------|------|----------|
| `MetricsMiddleware` 未注册 | panic 请求无 metric，监控盲区 | R38+1 |
| `TraceMiddleware` 注册位置错误 | 5 个中间件日志无 trace_id | R38+1 |
| 无 CI 兜底校验顺序 | 后续 PR 可随手打乱 | R38+1 |

# R38 决策表二次论证（阶段4 成果）

> 输入：阶段3 `R38_COMPETITIVE_DECISIONS.md` + 红队审查报告
> 方法：把红队反馈按"是否适用于本项目"二八过滤 → 提取真正命中盲点的 7 项 → 升级决策表

---

## 一、红队反馈过滤（适配本项目）

红队报告把决策 ID 误读为"通用架构组件"（M1=sqlx、M5=JWT...），与本项目 R38 的"工程化合规模块"（M1=SafeGo、M3=归属校验...）不同。但**红队识别的盲点确实命中 R38 决策表的真实缺口**，过滤后保留如下 7 项：

### ✅ 命中 R38 盲点（采纳升级）

| # | 红队盲点 | R38 对应模块 | 升级动作 |
|---|---------|-------------|---------|
| 🚨 R-1 | **中间件注册顺序未冻结** | M11（trace）/M12（metrics）/M8（Debug+Recovery） | 新增 `bootstrap/middleware_order.go` 固化顺序，写入单测断言 |
| 🚨 R-2 | **SafeGo 改造 → ctx 雪崩被"成功吞掉"** | M1 | SafeGo 必须 `DetachContext` 独立超时，不能直接吃 c.Request.Context()；panic 必须 5xx 回写而非 200 |
| 🚨 R-3 | **sqlx 切库后 → 事务边界失控** | 不适用（项目用 GORM）→ 但 M5（Service 越层整改）相关 | 强制 `repo.WithTx(ctx, fn)` 模式 + lint 禁止裸 `db.Begin` |
| 🚨 R-4 | **JWT secret 走配置中心 → 灰度期间全员 401** | 不适用（项目用静态 env）→ 但保留 ADR | 项目目前静态 env 可控；如未来接配置中心必须双 secret 灰度 |
| 🚨 R-5 | **Redis 缓存与 DB 一致性** | M14d（RAG 三级缓存命中可观测）相关 | ADR 写明 Cache-Aside + 延迟双删策略 |
| 🚨 R-6 | **优雅停机缺失** | M7（启动清理）相关 | `app.Bootstrap()` 必须包含 `signal.Notify(SIGTERM)` + inflight 排空 |
| 🚨 R-7 | **依赖漏洞扫描 / pprof / 多租户隔离 / 时区** | 不在 R38 范围 | 列入 R39 路线图，不在本轮 |

---

## 二、决策表升级点（v2）

### M1 SafeGo 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| 保留 `utils.SafeGo` + 加 metric counter | **同左**，**新增**：`SafeGoDetached(ctx, name, timeout, fn)` 强制 `DetachContext` + 超时上限（防止 ctx 雪崩） |
| recover 处写堆栈日志 | **同左**，**新增**：recover 处强制返回 `slog.ErrorContext` + trace_id 自动注入 |
| — | **新增**：panic 必须使上游请求 5xx（gin.Recovery 默认行为），不允许"被吞掉" |

**升级接口签名**：
```go
// utils.SafeGo(ctx, name, fn) - 保留旧签名（兼容性）
// utils.SafeGoDetached(ctx, name, timeout, fn) - 新增（ctx 隔离 + 超时）
// utils.SafeGoWithRetry(ctx, name, backoff, fn) - 新增（重试串联）
```

---

### M2 分页钳制 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| max=200 业务 / 1000 管理 | **同左** |
| — | **新增**：明确"先于业务参数校验的早返回"，避免大对象先加载再 clamp |

---

### M3 归属校验 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| 中间件 + Service + Scope | **同左** |
| — | **新增**：中间件查询 owner_id 走 5s 内存缓存兜底（避免每次请求多一次 DB 查询） |

---

### M7 启动清理 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| `app.Bootstrap()` 统一启动序列 | **同左**，**新增**：`app.Bootstrap()` 内含 `signal.Notify(SIGTERM/SIGINT)` + `http.Server.Shutdown(ctx)` inflight 排空（红队 R-6 命中） |

---

### M11–M12 可观测性 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| 指标 demo 埋点 | **同左** |
| — | **新增**：`bootstrap/middleware_order.go` 固化中间件注册顺序：<br>1. trace → 2. cors → 3. metrics → 4. recovery(gin.Recovery) → 5. logger → 6. ownership → 7. handler<br>（红队 R-1 命中） |

**为什么是这个顺序**：
- trace 必须最先（注入 trace_id）
- cors 在 metrics 前（拒绝跨域请求不计入业务指标）
- metrics 在 recovery 前（即便后续 panic 也能产出部分 metric）
- recovery 包住所有业务（防止 panic 击穿 metrics）
- logger 在 metrics 后（带 trace_id 的日志能与 metric exemplar 关联）
- ownership 在 handler 前（资源越权拦截）

---

### M14d RAG 缓存一致性 升级版

| 原 v1 | 升级 v2 |
|-------|---------|
| 新增 `rag_recall_total` 埋点 | **同左**，**新增**：写明 ADR-001：**Cache-Aside + 延迟双删（500ms）策略**（红队 R-5 命中） |

---

## 三、新增项（红队发现的 R38 真盲点）

| 新增 ID | 内容 | 阶段 |
|---------|------|------|
| **R38-NEW-1** | `internal/bootstrap/middleware_order.go` 固化中间件顺序 + 单测断言 | 第 1 批 |
| **R38-NEW-2** | `app.Bootstrap()` 内含 `SIGTERM` graceful shutdown（inflight 排空 + DB/Redis 连接关闭） | 第 1 批 |
| **R38-NEW-3** | ADR-001：RAG 缓存 Cache-Aside + 延迟双删（500ms） | 第 1 批 |
| **R38-NEW-4** | ADR-002：若未来接配置中心 → JWT secret 必须双 buffer 灰度 | 不落地，仅 ADR |
| **R38-NEW-5** | `scripts/check_naked_goroutine.sh`（CI 兜底禁止裸 `go func(`） | 第 1 批 |

---

## 四、阶段 5 落地批次（最终版）

| 批次 | 内容 | 预估工作量 | 风险等级 |
|------|------|-----------|---------|
| **第 1 批** | M1 SafeGo 升级 + M2 分页钳制 + R38-NEW-1/2/3/5 + M11 接口预留 + M12 metrics 中间件 | 3 天 | 中 |
| **第 2 批** | M3 归属校验 + M4 Router 内联整改 + M5 Service 越层整改 | 2 天 | 中 |
| **第 3 批** | M6 吞错治理 + M7 启动清理 + M8 Debug+Recovery + M9 SVG + M10 SSE | 1 天 | 低 |
| **第 4 批** | M13 死条件/弱凭据/吞参数 | 0.5 天 | 低 |
| **第 5 批** | M14 业务补强（a/b/c/d/e） | 2 天 | 中 |

**总计 ~8.5 天**。鉴于 5 批次任务规模较大，本轮落地策略：
- 第 1 批 + 第 2 批 = 重点落地（核心合规）
- 第 3 批 + 第 4 批 = 跟随落地（轻量清理）
- 第 5 批 = 按需选 M14d（RAG 缓存可观测）落地，其他留 R39

---

## 五、R38 与 R31/R37 边界的最终确认

| 维度 | R31/R37 | R38（本轮）| R39（路线图） |
|------|---------|----------|-------------|
| AI 核心链 19 模块 | ✅ | 不重复 | — |
| P1g/f/h（情感/RAGAS/热力图） | ✅ | 不重复 | — |
| 最高标准安全审计报告 | ✅ | 不重复 | — |
| P0-1/P0-2 权限漏洞修复 | ✅（R37 已落）| 不重复 | — |
| SafeGo 全量收口 | 部分 | ✅ 重点 | — |
| 分页钳制 | ❌ | ✅ 重点 | — |
| 资源归属校验 | ❌ | ✅ 重点 | — |
| Router 内联整改 | ❌ | ✅ | — |
| Service 越层整改 | 部分 | ✅ 重点 | — |
| 启动优雅停机 | ❌ | ✅ 重点 | — |
| Prometheus metrics 中间件 | ❌ | ✅ 接口 + 关键埋点 | Grafana 看板 |
| trace_id 端到端贯通 | 部分（已有中间件）| ✅ SafeGo 强制 ctx | OTel SDK 完整接入 |
| 多租户隔离 / 优雅停机升级 / 依赖漏洞扫描 / 时区 | ❌ | R39 路线图 | 待规划 |

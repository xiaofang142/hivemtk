# R38 吸收/废弃决策表（阶段3 成果）

> 输入：阶段1 模块清单 M1-M14 + 阶段2 全网调研
> 评估维度：投入产出比 / 风险面 / 与现有架构一致性 / 与 R31 已落地的边界
> 结论：**全部 14 项均吸收**，无废弃项；2 项降级为「P3 留 R39」

---

## 决策矩阵（按 ROI 批次排序）

| ID | 模块 | ROI | 风险 | 决策 | 批次 | 落地摘要 |
|----|------|-----|------|------|------|---------|
| M1 | SafeGo 全量收口 | ★★★★★ | 中 | ✅ 吸收 | 第1批 | 升级内部加 metric + trace 自动注入；55 处裸 `go func` 改写 |
| M2 | 分页钳制 | ★★★★★ | 中 | ✅ 吸收 | 第1批 | `utils.ParsePagination()` 工具；200/1000 上限；前端默认 20 |
| M3 | 业务资源归属 | ★★★★ | 中 | ✅ 吸收 | 第2批 | 三段式纵深防御（中间件 + Service + Scope） |
| M4 | Router 内联整改 | ★★★★ | 低 | ✅ 吸收 | 第2批 | /bridge/capabilities、/mcp 等抽到 controller |
| M5 | Service 越层整改 | ★★★★ | 中 | ✅ 吸收 | 第2批 | 8 个违规 service 抽 Repo 封装 |
| M6 | `_ =` 吞错治理 | ★★★ | 低 | ✅ 吸收 | 第3批 | 25 处强制 warn 日志；目标下降 80% |
| M7 | 启动双调用清理 | ★★★ | 低 | ✅ 吸收 | 第3批 | `app.Bootstrap()` 统一启动序列 |
| M8 | Debug+Recovery 清理 | ★★★ | 低 | ✅ 吸收 | 第3批 | Release 模式 + Recovery 仅留 1 份 |
| M9 | SVG 上传收敛 | ★★ | 中 | ✅ 吸收 | 第3批 | 移除 svg 白名单；存量走 `?legacy=1` 兼容 |
| M10 | SSE CORS 收敛 | ★★ | 中 | ✅ 吸收 | 第3批 | 白名单 Origin，去 credentials |
| M11 | trace_id 贯通 | ★★★ | 低 | ✅ 吸收（接口预留）| 第4批 | SafeGo 必须保留 ctx；其他贯通 R39 |
| M12 | metrics 暴露 | ★★★ | 低 | ✅ 吸收（接口先行）| 第4批 | HTTP 中间件 + 关键路径 demo 埋点 |
| M13 | 死条件/弱凭据/吞参数 | ★★ | 低 | ✅ 吸收 | 第4批 | fail-fast、删除死条件、统一 ParseInt64 工具 |
| M14 | 业务模块补强 | ★★★ | 中 | ✅ 吸收 | 第5批 | a/orchestrator b/sop_heatmap c/bridge_account d/rag_cache e/ws_reconnect |

**统计**：吸收 14 / 废弃 0 / 降级 R39 0 / 阶段4 二次审查 0
**总代码变更预估**：~80 文件 / ~3000 行 / 影响域：controller、service、router、utils、middleware

---

## 每项决策详解

### M1 SafeGo 全量收口 ✅ 吸收

**采纳**（调研结论：保留升级，不引入 go-zero routine）

**落地步骤**：
1. `internal/utils/safe.go` 升级：
   - 增加 panic 计数器 `bg_panic_total{name}`（Prometheus Counter）
   - recover 处从 ctx 取 `trace.SpanContextFromContext(ctx)` 写入日志
   - 新增 `SafeGoWithRetry(ctx, name, backoff, fn)` 串联 cenkalti/backoff v5
2. 55 处裸 `go func(` 改写为 `utils.SafeGo(ctx, name, fn)`；ctx 必须来自 DetachContext 父级
3. 工具脚本 `scripts/check_naked_goroutine.sh`（CI 兜底）：grep 找裸 `go func(` → 非 0 报错
4. 风险点：异步链路 trace_id 丢失 → 必须保留 ctx，**禁用裸 `context.Background()`**

**验收**：`grep -rn "go func(" user-server --include="*.go" | grep -v _test.go | grep -v utils.SafeGo` → 0

---

### M2 分页钳制 ✅ 吸收

**采纳**（调研结论：pageSize=200/1000 上限）

**落地步骤**：
1. `internal/utils/pagination.go` 新增：
   ```go
   type PageOption func(*pageConfig)
   func WithDefaultSize(n int) PageOption
   func WithMaxSize(n int) PageOption
   func WithMinSize(n int) PageOption
   func ParsePagination(c *gin.Context, opts ...PageOption) (page, pageSize int, err error)
   ```
2. 业务 API 默认：max=200；管理 API：max=1000（`WithMaxSize(1000)`）
3. 全部 controller `DefaultQuery.*page` 替换为 `utils.ParsePagination`
4. 前端（user-web）默认 `page_size=20` 全仓统一；提供 `pageSize` 字段暴露给用户可改
5. 风险点：存量前端默认 page_size 透传不同值 → 钳制后截断 → 前端默认 20 解决

**验收**：`grep -rn "DefaultQuery.*page\|DefaultQuery.*Page" user-server/internal/controller --include="*.go" | grep -v ParsePagination | wc -l` → 较基线下降 ≥90%

---

### M3 业务资源归属校验 ✅ 吸收

**采纳**（调研结论：纵深防御三段式）

**落地步骤**：
1. `internal/middleware/ownership.go` 新增：
   - `RequireOwner(":id", "table_name")` 中间件（粗过滤）
   - `BuildOwnershipChecker(...)` 注册自定义表（资源 ID → owner_id 查询）
2. `internal/service/guard.go` 新增：
   - `func GuardOwnerID(ctx, resourceOwnerID uint) error` Service 层 Guard
   - 业务方法首行调用
3. `internal/repository/scope/tenant.go` 新增：
   - `func TenantScope(ctx) func(db *gorm.DB) *gorm.DB` GORM Scope
   - 所有 Repository 查询 `db.Scopes(TenantScope(ctx))`
4. 抽查 10 个高危 handler（ai_agent/tiktok_card/telegram/feishu/whatsapp/dingtalk 等）全量过审
5. 风险点：中间件需要查 DB 才能比对 owner_id → 引入一次额外查询 → 用 5s 缓存兜底

**验收**：10 个抽查 handler 至少 8 个过线；新增 P1-7 的 PermissionDecorator 接线（auto_register 链路）

---

### M4 Router 内联整改 ✅ 吸收

**采纳**（架构铁律违规）

**落地步骤**：
1. `/bridge/capabilities`、`/mcp`、`/agent/tools/*` 内联 handler 抽到 `internal/controller/{name}.go`
2. 路由文件 `internal/router/*.go` 仅保留路由映射 + 中间件装配
3. `card_routes.go` 多处内联包装 → 保留（属 adapter，不算违规）
4. 风险点：抽离后改动面广 → 用 PR 单文件增量落地，避免大爆炸

**验收**：`grep -rn "func(c \*gin.Context)" user-server/internal/router --include="*.go" | grep -v _test.go | grep -v health | grep -v license | grep -v swagger` → 0

---

### M5 Service 越层整改 ✅ 吸收

**采纳**（架构合规 B→A）

**落地步骤**：
1. 8 个违规 service（sop_outbox_dispatcher、password_reset、security_audit、agent_kb_binding、feedback_learner、reach_send_pipeline、sop、alert_rule）逐个抽 Repository 封装
2. Service 持 `*gorm.DB` 仅做 DI 传递，禁止直接执行查询
3. 风险点：抽离后 SQL 改动可能影响性能 → 跑 benchmark 对比

**验收**：抽样 40/238 service，违规率从 20% → 0%

---

### M6 `_ =` 吞错治理 ✅ 吸收

**采纳**

**落地步骤**：
1. 全量 grep 25 处 `_ =`
2. 全部替换为 `if err != nil { log.Warn().Err(err).Msg(...) }`
3. 旁路写允许 warn，但不静默吞错
4. 风险点：warn 日志可能暴增 → 设 sampling 兜底

**验收**：`_ = repo.*` 出现次数较基线下降 ≥80%

---

### M7 启动双调用清理 ✅ 吸收

**采纳**

**落地步骤**：
1. `cmd/api/main.go` 抽出 `app.Bootstrap()` 统一启动序列
2. 删除重复 `db.InitDB()/AutoMigrate()` 调用
3. 风险点：连接池配置可能在两处不一致 → diff 比对

---

### M8 Debug+Recovery 清理 ✅ 吸收

**采纳**

**落地步骤**：
1. `gin.SetMode(gin.ReleaseMode)` 默认；`GIN_MODE=debug` 留 dev 入口
2. 删除 `r.Use(gin.Recovery())` 中的一份（保留 `gin.Default()` 自带）
3. 风险点：Release 模式隐藏 dev 提示 → dev 通过 env 切换

---

### M9 SVG 上传收敛 ✅ 吸收

**采纳**

**落地步骤**：
1. `controller/upload.go` 移除 `.svg` 白名单
2. 出网关加 `Content-Security-Policy: default-src 'self'` + `nosniff`
3. 存量 svg 资源走 `?legacy=1` 兼容（限 30 天）
4. 风险点：影响设计/UI 类上传 → 通知存量用户重新导出 PNG

---

### M10 SSE CORS 收敛 ✅ 吸收

**采纳**

**落地步骤**：
1. SSE 中间件 Origin 白名单（配置文件 `cors.AllowedOrigins`）
2. 删除 `Access-Control-Allow-Credentials: true`
3. 风险点：第三方嵌入需求 → 配置项开关

---

### M11 trace_id 贯通 ✅ 吸收（接口预留）

**采纳**（R39 完整落地）

**落地步骤（本轮仅接口）**：
1. `SafeGo` 强制 ctx 入参（已落地）
2. 日志库 hook 接收 ctx 字段，写入 trace_id
3. R39 接入 OTel SDK

---

### M12 metrics 暴露 ✅ 吸收（接口先行）

**采纳**

**落地步骤（本轮）**：
1. `internal/middleware/metrics.go` 新增 Prometheus 中间件
2. 关键路径 demo 埋点：rag_recall_total / sop_node_exec_total / agent_runtime_inference_total / bg_panic_total
3. `/metrics` 端点暴露（仅内网访问）
4. R39 接 Grafana 看板

---

### M13 死条件/弱凭据/吞参数 ✅ 吸收

**采纳**

**落地步骤**：
1. `platform/contributor_client.go:59` 删除 `"mtk-default-secret"` fallback，fail-fast
2. `service/wecom_integration.go:142` 删除 `if false ||` 死条件
3. `utils.ParseInt64OrError(c, key)` 工具替换 4 处 `id, _ := strconv.ParseInt(...)`
4. 风险点：fail-fast 让既有"无 secret 仍可启动"崩溃 → 仅生产启用，dev 通过 env 跳过

---

### M14 业务模块补强 ✅ 吸收

**采纳**

**落地步骤**：

- **M14a WorkflowOrchestrator 健壮性**（R37 已有，本轮补 retry/timeout/cancel 全量测试）
- **M14b SOP_Heatmap 性能优化**（R37 P1h 已有，补 benchmark + PG 索引）
- **M14c Bridge Account 凭据托管**（新增，参考 Stripe Connect Access Key）
- **M14d RAG 三级缓存命中可观测**（新增埋点 `rag_recall_total{layer, hit}`）
- **M14e Channelgw WebSocket 优雅降级**（新增：断线重连 + 指数退避 + 熔断）

---

## 与 R31 边界的最终确认

- R31/R37 已落地 19 模块 AI 核心链（LLM/Agent/RAG/SOP/人味化等）+ P1g/f/h
- R38 **不重复**：仅做工程化合规（最高标准审计落地）+ 业务补强（M14）
- R38 **首次实现**：M1 SafeGo 升级 + M2 分页钳制 + M11 trace 接口预留 + M12 metrics 中间件
- R38 **R39 衔接**：完整 OTel SDK 接入 + SpiceDB 选型评估 + cursor-based pagination 可选

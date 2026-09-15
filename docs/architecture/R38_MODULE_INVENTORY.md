# R38 模块清单：全仓最高标准合规 + 全链路质量

> 审计基线：`user-server/docs/architecture/AUDIT_HIGHEST_STANDARD.md`（R37 后置，2026-08-26）
> 本轮（R38）聚焦**非 AI 核心链**的工程化合规与可观测性，对应"代码健康 + 系统可维护性"赛道。
> AI 核心链（19 模块）已在 R31/R37 完成，本轮不重复。

---

## 一、本轮主题与价值

R31/R37 解决了"功能维度"的竞品吸收（LLM 路由/Agent/ReAct/RAG/SOP/人味化等）；
R38 解决"代码维度"的最高标准合规：

| 维度 | 当前评级 | 目标评级 | 价值 |
|------|---------|---------|------|
| 安全 | B+ | A- | 收敛剩余 P0/P1 权限面，闭环 P0-2 修复后的二阶风险 |
| 数据完整性 | B | A- | 消除所有 `_ =` 吞错、规范化事务边界 |
| 五层架构合规 | B | A | 收口 Router 内联、Service 越层直访 DB |
| 资源泄漏 | B- | A | SafeGo 全量收口，进程级 panic 100% 可恢复 |
| 输入验证 | C+ | B+ | 全量分页钳制 + 通用 ParseIntOrZero 替换 |
| 可观测性 | C | B | 链路 trace_id 端到端贯通 + metrics 关键路径 |

---

## 二、模块清单（M1–M14）

### M1. 后台 goroutine SafeGo 全量收口（P1-3 落地）

- **现状**：全仓 164 处 `go func`，55 处无 recover，单点 panic → 整进程崩溃（可用性 P0 级连锁）
- **已具备工具**：`utils.SafeGo(ctx, name, fn)` / `utils.SafeGoWithRecover(...)` / `async.SafeGo(fn)` 三套
- **目标**：剩余 55 处 100% 改写为 SafeGo；新增 linter 规则禁止 `go func(` 裸用
- **验收**：`grep -rn "go func(" user-server --include="*.go" | grep -v _test.go | grep -v utils.SafeGo | grep -v async.SafeGo` → 0
- **风险**：异步链路日志上下文（trace_id）丢失 → 必须保留 ctx value，禁用 `context.Background()`

### M2. 分页参数全量钳制（P2-1 落地）

- **现状**：349 处分页参数引用，`page_size` 普遍未设上限（最极端默认 10000），管理端负数/超大值透传 → 单请求拖库/慢查询面
- **正面样板**：`chat_public.go:165-170` 已做 1≤page, 1≤pageSize≤200
- **目标**：抽 `utils.PaginationParams(page, size, maxSize)` 统一函数，所有 controller 入参必经；最大默认 maxSize=200（管理端可放宽到 1000）
- **验收**：`grep -rn "DefaultQuery.*page" user-server/internal/controller --include="*.go" | grep -v PaginationParams` → 0
- **风险**：存量前端默认 page_size 透传不同值，钳制后导致 list 截断 → 前端默认 page_size=20 全仓统一

### M3. 业务资源归属/角色校验补齐（P1-5 落地）

- **现状**：10 个抽查 handler（ai_agent/tiktok_card/telegram_account/feishu/whatsapp/dingtalk 等）的 `GetByID/Update/Delete` 不校验资源归属，staff A 可改 staff B 的渠道凭据
- **特殊高危**：渠道账号含 Bot Token / AppSecret / SMTP Password 等敏感凭据
- **目标**：抽 `service.OwnershipChecker.Check(ctx, ownerID)` 通用工具，所有敏感凭据类资源必须调用；非敏感资源至少加归属校验
- **验收**：抽 10 个抽查 handler 全量过审，至少 8 个过线

### M4. Router 内联业务逻辑整改（P2-8 落地）

- **现状**：`/bridge/capabilities`、`/mcp` 端点读 body+调 service+写响应全在 router 文件中；另有 `card_routes.go` 多处内联包装
- **违规点**：违反"Router 只做映射"铁律
- **目标**：所有 router 内联 handler 抽到 controller，业务逻辑 0 行进 router 文件
- **验收**：`grep -rn "func(c \*gin.Context)" user-server/internal/router --include="*.go" | grep -v _test.go` 仅保留 health/license/swagger 等纯转发

### M5. Service 越层直访 DB 整改（架构合规 B→A）

- **现状**：抽样 40/238 service 持有 `*gorm.DB`，8 个绕过 Repository 直接执行（sop_outbox_dispatcher、password_reset、security_audit、agent_kb_binding、feedback_learner、reach_send_pipeline、sop 等）
- **目标**：8 个违规 service 抽 Repository 层封装；其他 service 仅做 DI 传递
- **验收**：剩余持有 `*gorm.DB` 但**不直访**的 service 数量 ≥ 现状

### M6. `_ =` 吞错全量治理（P2-6 落地）

- **现状**：约 25 处 `_ = repo.Create/Update` 吞错（geo/service、knowledge reindex、dead_letter 等）
- **后果**：遥测/状态写失败不感知 → 状态漂移（看似"小事"但影响故障定位）
- **目标**：所有吞错处强制打 warn 日志；非主链路写失败用 `slog.Warn().Err(err).Msg(...)` 替代 `_ =`
- **验收**：`grep -rn "^[[:space:]]*_ = " user-server/internal --include="*.go" | grep -v _test.go | wc -l` 较基线下降 ≥80%

### M7. 启动配置双调用清理（P2-7 落地）

- **现状**：`cmd/api/main.go` 中 `db.InitDB()/AutoMigrate()` 调用两次（116-117 与 198-199）
- **目标**：抽出 `app.Bootstrap()` 统一启动序列
- **风险**：重复初始化可能引发连接池竞争 → 必须用 git diff 验证连接池配置一致性

### M8. 调试模式收紧 + Gin Recovery 双重注册清理（P2-2/P2-9 落地）

- **现状**：`cmd/api/main.go:180 gin.SetMode(gin.DebugMode)` 生产环境仍用 Debug；`gin.Default()` + `:145 gin.Recovery()` 双重注册
- **目标**：Debug 模式改为 `gin.SetMode(gin.ReleaseMode)`（或 env 切换）；Recovery 仅保留一份
- **风险**：Release 模式可能隐藏 dev 提示 → 通过 env `GIN_MODE=debug` 给 dev 留口子

### M9. SVG 上传收敛（P1-2 落地）

- **现状**：`controller/upload.go:31` 允许上传 `.svg`，存在存储型 XSS 面（反代/ 反向代理层 同源直出 + 无 CSP）
- **目标**：移除 svg 白名单，或出网关加 `nosniff` + CSP
- **风险**：存量素材库若含 svg 需兼容 → 加 `?legacy=1` query 参数临时放行 + 邮件通知存量用户

### M10. SSE CORS 通配收敛（P1-1 落地）

- **现状**：SSE 端点对任意 Origin 反射 ACAO + `Access-Control-Allow-Credentials: true`
- **现状依赖**：当前鉴权靠 X-Bridge-Token 头故可利用性低
- **目标**：SSE 白名单化 Origin，去掉 credentials 头
- **风险**：若 SSE 链路有第三方嵌入需求 → 通过 `cors.AllowedOrigins` 配置项开关

### M11. 链路 trace_id 端到端贯通（可观测性 B 起点）

- **现状**：`middleware/trace.go` 提供 trace_id 中间件，已注入到 ctx
- **缺口**：异步 goroutine（SafeGo 收口后）丢失 ctx value → 日志断链
- **目标**：所有 SafeGo 调用必须保留 `DetachContext(ctx)`，禁止直接 `context.Background()`
- **验收**：抽样 10 个 SafeGo 调用，全部使用 ctx 透传

### M12. 关键路径 metrics 暴露（可观测性 B 收口）

- **现状**：缺统一 metrics 中间件；RAG/SOP/Agent 等核心路径无可量化指标
- **目标**：抽 `middleware.Metrics()` Prometheus 中间件 + 关键路径埋点（http_request_duration_seconds / rag_recall_total / sop_node_exec_total / agent_runtime_inference_total）
- **取舍**：本轮仅实现**接口**与**demo 埋点**，全面推广留给 R39

### M13. 死条件/弱凭据回退/Panic 参数解析 一次性清零（P2-3/P2-4/P2-5 落地）

- **P2-3**：`platform/contributor_client.go:59` secret 为空回退 `"mtk-default-secret"` → fail-fast
- **P2-4**：`service/wecom_integration.go:142` `if false || req.AccountID == 0` 死条件残留 → 删
- **P2-5**：`controller/asset_market.go:119,158,182,192` `id, _ := strconv.ParseInt(...)` 吞错 → 用统一 `utils.ParseInt64OrError(c, key)` 替换
- **风险**：fail-fast 可能让既有"无 secret 仍可启动"的开发体验崩溃 → 仅生产环境启用

### M14. 业务模块新增（与 R31 互补）

R31 完成了 AI 核心链 19 模块；本轮新增 5 个工程化模块：

- **M14a. WorkflowOrchestrator 健壮性**：补 retry/timeout/cancel 全量测试（R37 已有基础）
- **M14b. SOP_Heatmap 性能优化**：从 PG 聚合改为异步物化（R37 P1h 已有，需补 benchmark）
- **M14c. Bridge Account 凭据托管**：参考 Stripe Connect / AWS IAM Access Key 模式
- **M14d. RAG 三级缓存命中可观测**（Redis Hit/Miss/L1/L2/L3 指标）
- **M14e. Channelgw WebSocket 优雅降级**（断线重连 + 指数退避 + 熔断）

---

## 三、与 R31 范围的边界

| 维度 | R31/R37 已做 | R38 本轮做 |
|------|-------------|-----------|
| LLM 路由/Agent/ReAct/RAG/SOP/人味化 | ✅ 19 模块全落地 | 不重复 |
| 情感分层响应/RAGAS/SOP 热力图 | ✅ P1g/f/h | 不重复 |
| 最高标准安全/架构审计 | ✅ 报告产出 | ✅ 落地修复（M1-M13） |
| 业务模块工程化补强 | 部分 | ✅ M14a–M14e |
| 五层架构合规 A 级 | ❌ 当前 B | ✅ 目标 A |
| 资源泄漏 0 panic 进程崩溃 | ❌ 当前 B- | ✅ 目标 A |

---

## 四、本轮 ROI 排序

按"投入产出比 × 风险"双轴，本轮落地优先级：

1. **M1 SafeGo 全量收口**（高 ROI/P0 级风险） — 第 1 批次
2. **M2 分页钳制**（高 ROI/中风险） — 第 1 批次
3. **M3 资源归属校验**（中 ROI/中风险） — 第 2 批次
4. **M4 Router 内联整改**（中 ROI/低风险） — 第 2 批次
5. **M5 Service 越层整改**（中 ROI/中风险） — 第 2 批次
6. **M6 吞错治理**（中 ROI/低风险） — 第 3 批次
7. **M7–M10 启动/SVG/SSE/Recovery 清理**（低 ROI/低风险） — 第 3 批次
8. **M11–M12 可观测性**（低 ROI/无风险） — 第 4 批次（接口先行）
9. **M13 死条件清零**（低 ROI/无风险） — 第 4 批次
10. **M14 业务模块工程化补强**（按需） — 第 5 批次

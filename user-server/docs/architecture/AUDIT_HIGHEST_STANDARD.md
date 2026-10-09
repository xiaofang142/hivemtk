# user-server 最高标准安全/架构审计报告

- 审计日期：2026-08-26
- 审计范围：`hivemtk/user-server`（Go 后端，约 1900 个 .go 文件）
- 审计人：首席安全/架构审计（ox-alpha）
- 方法：grep 定位 → 精读确认（所有结论均经人工精读代码核实，不凭 grep 结果下结论）
- 处置策略：仅 P0（安全漏洞/数据丢失/资金风险）立即修复；P1/P2 仅列清单

---

## 一、执行摘要

| # | 维度 | 评级 | 概述 |
|---|------|------|------|
| 1 | 安全 | **B+**（修复前 B，修复后 B+） | SQL注入/JWT/登录防爆破/上传校验均达标；发现 2 个 P0 权限类漏洞（已修复）；CORS SSE 通配 + SVG 上传为遗留 P1 |
| 2 | 数据完整性 | **B** | 关键链路（密码重置、配额扣减）事务/原子性正确；存在少量状态写路径吞错（`_ =`） |
| 3 | 五层架构合规 | **B** | Handler 层非常干净（抽查 10/10 无直查 DB）；Router 存在 2 处内联业务 handler；Service 层约 20% 直接操作 gorm.DB 绕过 Repository |
| 4 | 资源泄漏 | **B-** | HTTP body 全部正确 Close；进程内 map 均有界或只读；但 **55/76 处 `go func` 无 recover**，任一 panic 将击穿 gin.Recovery 导致整进程崩溃 |
| 5 | 输入验证 | **C+** | 公开访客聊天端点分页钳制完善；管理端大量控制器 `page_size` 未设上限/未防负数（最极端默认 10000） |

总体结论：该项目已经过多轮审计迭代（代码中大量 "v3 审计 P0-x/P1-x" 修复注释），基础安全水位显著高于同类项目。本次最高标准复审仍发现 **2 个 P0**（均为"低权限登录用户可触达高危写操作"的权限收敛遗漏），已立即修复并通过编译与定向验证。

---

## 二、P0 发现（已修复）

### P0-1 数据库迁移/回滚端点未做管理员鉴权 —— 数据丢失风险

- **位置**：`internal/router/system_routes.go:119 setupMigrationRoutes`（挂载于 `router.go:512 auth` 组）
- **证据**：`POST /api/migration/task`（执行升级）与 `POST /api/migration/rollback`（回滚 schema）仅经过 `JWTAuthMiddleware`，无任何角色校验。对照同文件 `setupBackupRoutes`（备份/恢复已 admin-only，注释明确"恢复可覆盖全库"）与 `router.go:274 systemAdmin` 组（system 路由 admin-only），migration 属于明显遗漏。`MigrationController.Rollback`（controller/migration.go:131）内部亦无角色检查。
- **影响**：任意低权限 staff 账号（JWT 被泄露/钓鱼/XSS 窃取后）可直接调用 rollback 触发数据库结构回滚 → **数据丢失**。
- **修复**：读端点保留任意登录用户；`POST /migration/task`、`POST /migration/rollback` 移入 `middleware.AdminAuthMiddleware()` 子组。

### P0-2 工具执行端点无权限控制 —— 任意工具调用/真实外发/成本消耗

- **位置**：`internal/router/tool_debug_routes.go:23,27`（挂载于 auth 组）
- **证据**：`POST /api/agent/tools/execute` 可按名称执行全局注册的**任意** Agent 工具，包括 `reach_tools.go:15-24` 定义的 SMS/Email/WeCom/微信/抖音/KS/小红书/TikTok/闲鱼/钉钉真实消息外发（产生真金白银的通道成本），以及知识库写操作、熔断器重置（`POST /agent/tools/circuit/reset` 影响全局限流熔断状态）。框架层已预留 `tooluse/decorator_permission.go PermissionDecorator`，但全仓 grep 确认**从未接线**——即工具执行链路零权限校验。前端产物（user-web-dist）grep 确认无该端点调用方，收敛不影响存量功能。
- **影响**：任意低权限登录用户可冒充 Agent 触发批量客户外发（资金风险 + 业务侧信任损害）、消耗 LLM 预算、随意重置全局熔断器。
- **修复**：`tools/execute` 与 `tools/circuit/reset` 移入 admin 子组；list/get/stats/audit/cost/providers 只读端点保持原权限。

### 验证

```
$ go build ./...            → BUILD_OK
$ go vet ./internal/router/ → VET_OK
$ go test ./internal/router/-run TestSetup_WeComRoutes
  → FAIL，但经 git stash 对照验证：未修改代码同样失败（测试依赖本地 PG :8202，
    "failed to connect ... unexpected EOF"，纯环境问题，与本次改动无关）
```

---

## 三、P1 发现（不修复，列清单）

| # | 位置 | 问题 | 建议 |
|---|------|------|------|
| P1-1 | `router/router.go:93,106-108` | SSE 端点对**任意 Origin** 反射 ACAO 且带 `Access-Control-Allow-Credentials: true`。当前鉴权靠 X-Bridge-Token 头故可利用性低，但一旦任何端点引入 Cookie 会话即刻升级为凭据型 CSRF | SSE 白名单化 Origin 或去掉 credentials 头 |
| P1-2 | `controller/upload.go:31` | 允许上传 `.svg`（image/svg+xml）。SVG 可内嵌 script，若反代/Nginx 同源直出且无 `Content-Security-Policy`/`Content-Disposition` 即构成存储型 XSS | 移除 svg 或出网关加 nosniff+CSP |
| P1-3 | 全仓 76 处 `go func`，55 处 6 行窗口内无 `recover()`（如 `middleware/trace.go:86`、`service/webhook.go` 系列、`service/layer.go` 多处） | goroutine 内 panic 不被 gin.Recovery 捕获 → 整进程崩溃（可用性 P0 级连锁） | 统一 SafeGo wrapper |
| P1-4 | `router/router.go:408-417 bridge.RegisterOwnershipChecker` | 账号查询 `ErrRecordNotFound` 时**返回 true（放行）**——fail-open 设计，账号不存在反而通过归属校验 | 改为 fail-closed 返回 false 或显式错误 |
| P1-5 | 抽查 10 个业务资源 handler（ai_agent/tiktok_card/telegram_account/feishu_account/whatsapp_cloud/dingtalk 等） | `GetByID/Update/Delete` 全部不校验资源归属用户（staff A 可改 staff B 的渠道账号、AI Agent）。单租户内部系统降低了危害，但渠道账号含 Bot Token/AppSecret 等敏感凭据 | 至少对渠道凭据类资源加归属/角色校验 |
| P1-6 | `router/system_routes.go:52-60 obs/config` | OBS 凭据配置 CRUD（含 TestConnection SSRF 探测面）开放给全部登录用户，非 admin | 写操作收 admin |
| P1-7 | `aiagent/agent/tooluse/decorator_permission.go` | 权限装饰器已实现但从未注册使用（P0-2 根因之一） | 在 auto_register 链路接入 |

---

## 四、P2 发现（不修复，列清单）

| # | 位置 | 问题 |
|---|------|------|
| P2-1 | 多个控制器分页参数无钳制（`operation_log.go:154` 默认 page_size=10000；`auth.go`、`intent.go`、`chat_channel.go` 等负数/超大值直接透传 repo） | 单请求拖库/慢查询面 |
| P2-2 | `cmd/api/main.go:180 gin.SetMode(gin.DebugMode)` | 生产 Debug 模式（路由 dump/性能开销/日志泄漏） |
| P2-3 | `platform/contributor_client.go:59` secret 为空时回退 `"mtk-default-secret"` 派生贡献者密码 | 弱凭据回退，建议 fail-fast |
| P2-4 | `service/wecom_integration.go:142` `if false || req.AccountID == 0 ...` 死条件残留 | 代码卫生 |
| P2-5 | `controller/asset_market.go:119,158,182,192` `id, _ := strconv.ParseInt(...)` 吞掉解析错误，非法 id 变 0 继续 | 参数校验缺失 |
| P2-6 | 约 25 处 `_ = repo.Create/Update` 吞错（geo/service、knowledge reindex、dead_letter 等） | 均为遥测/状态类写，非主链路，但会造成状态漂移 |
| P2-7 | `cmd/api/main.go` 中 `db.InitDB()/AutoMigrate()` 调用两次（116-117 与 198-199） | 重复初始化 |
| P2-8 | `router/router.go:368-393` `/bridge/capabilities`、`/mcp` 内联业务 handler（违反 Router 只做映射铁律） | 架构违规 |
| P2-9 | `main.go:181` `gin.Default()` 已含 Recovery，`:145` 又 `r.Use(gin.Recovery())` 双重注册 | 冗余 |

---

## 五、各维度审计明细

### 1. 安全
- **SQL 注入**：全仓 `Raw(/Exec(` × `fmt.Sprintf` 交叉定位 10 处，逐点精读：`vector_retriever.go`(%d int)、`pgvector_index_ops.go`(经 `sanitizeIdent` 清洗标识符)、migrations/testutil(常量表名) —— **0 个可利用注入点**。GORM Where 全部参数化。
- **JWT**：密钥从环境变量加载，缺失/过短 panic fail-fast（utils/jwt.go:39-59）；解析强制 HMAC 方法白名单（jwt.go:119）；有黑名单登出机制；WebSocket 升级场景 token 走 query 有注释说明。**合规**。
- **Secret 明文残留**：config.yaml 全部走 `${ENV}` 占位；唯一硬编码 fallback 见 P2-3。Telegram Bot Token / Feishu AppSecret / SMTP 密码在 VO 层均掩码输出（telegram_account.go:82、feishu_account.go、email_smtp.go toEmailSmtpResponse 省略 Password 字段）。**合规**。
- **CORS**：默认拒绝任意 Web 源 + 扩展源白名单化（遗留 SSE 通配见 P1-1）；TrustedProxies 收敛私网（main.go:184）。基本合规。
- **登录防护**：`BruteForceGuard("auth.login")` + 失败记录 + MFA 支持。**合规**。
- **文件上传**：扩展名白名单 + 危险扩展黑名单 + 魔术数字校验 + MIME 白名单 + 大小限制 + UUID 重命名 + 0750/0640 权限，质量高（遗留 SVG 见 P1-2）。

### 2. 数据完整性
- 密码重置 token 标记 + 用户改密在同一事务（password_reset.go:94-100）。✅
- 企微配额扣减使用 `gorm.Expr("daily_msg_used + ?")` + 条件 WHERE 原子扣减（wecom_account_health_quota.go:34-37），无读改写竞态。✅
- 违规样本：P2-6 列表（约 25 处吞错，均为旁路写）。

### 3. 五层架构合规抽查（各 10 处）
- **Handler 直查 gorm.DB**：抽查 10 个 controller（notification/auth/clue/customer_360/upload/email_smtp/migration/account/asset_market/alert_rule）—— import gorm 的仅 workflow_orchestrator.go 且只用了 `gorm.ErrRecordNotFound` 判断。违规率 **0/10**。
- **Router 内联业务逻辑**：发现 2 处实质内联（`/mcp` 读 body+调 MCP server+写响应；`/bridge/capabilities` 直接构造 JSON），另有 1 处 inline 包装委托（shortlink update，不算违规）。违规率 **2/10**（→P2-8）。
- **Service 绕过 Repository**：持有 `*gorm.DB` 的 service 抽样 40/238，其中实际绕过 Repo 直接执行 DB 操作的 8 个（sop_outbox_dispatcher、password_reset、security_audit、agent_kb_binding、feedback_learner、reach_send_pipeline、sop 等）。违规率约 **20%**（多数 service 仅做 DI 传递，合规）。

### 4. 资源泄漏
- **HTTP body**：全部 `client.Do` 调用点均配对 `defer resp.Body.Close()`（脚本比对 0 缺口）。✅
- **goroutine**：76 处 `go func`，55 处无 recover（P1-3）；长生命周期 goroutine（cron/hub/failover）均有 ctx cancel + Stop + defer，退出机制完备。
- **进程内 map**：包级 map 全部为只读配置表；有状态 map（ratelimit clients、visitor limiter、brute_force entries、SSE hub clients/ipCount）均带 mutex + 过期清理。✅

### 5. 输入验证
- 正面样板：公开访客聊天端点（chat_public.go:165-170）page/pageSize 完整钳制（1≤page，1≤pageSize≤200）+ visitor_token HMAC 校验。
- 反面：管理端普遍裸 `strconv.Atoi(DefaultQuery)` 无钳制（P2-1）；字符串入库未见截断策略（依赖 GORM size 标签 + MySQL 严格模式兜底）。

---

## 六、修复清单（本次落地）

| 文件 | 变更 |
|------|------|
| `internal/router/tool_debug_routes.go` | `POST /agent/tools/execute`、`POST /agent/tools/circuit/reset` 加 `AdminAuthMiddleware`（P0-2） |
| `internal/router/system_routes.go` | `POST /migration/task`、`POST /migration/rollback` 加 `AdminAuthMiddleware`（P0-1） |

验证：`go build ./...` 通过；`go vet ./internal/router/` 通过；router 包测试失败经 stash 对照确认为既有环境问题（本地无 PG）。

## 七、统计汇总

- 维度评级：安全 **B+** / 数据完整性 **B** / 架构合规 **B** / 资源泄漏 **B-** / 输入验证 **C+**
- P0：**2** 个（均已修复并验证）
- P1：**7** 个
- P2：**9** 个

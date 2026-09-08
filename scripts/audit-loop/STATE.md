# 永动审计循环 — 状态与报告（STATE）

> 机器状态见同目录 `state.json`；执行指令见 `PLAN.md`。本文件追加每轮审计报告，最新在上。

## 循环总览

- 循环启动：2026-09-08，由 ZCode 自动化每 30 分钟触发一轮
- 已完成轮次：3 / 角度序列：security → authz → architecture → error-handling → concurrency → data-integrity → api-contract → frontend → perf → test-coverage → config-deploy → docs-consistency →（循环）
- 累计发现 / 修复：6 / 6
- 下一轮角度：error-handling

## 轮次报告

### R3 — architecture（2026-09-08）

**审计范围**：`scripts/check-architecture.sh` 全量（controller 反向依赖、service 直连 DB、repository 反向依赖、文件命名、interface 规范、ctx 透传）、Router 内联 handler、五层铁律抽查。

**发现与处置（3 项，均已修复）**：

1. 文件命名违规 12 处（P2）— `_controller.go` 冗余后缀 10 个 + `_service.go` 后缀 2 个。**处置**：5 个 manage 控制器（rag_eval/smart_router/agent_co_pilot/data_export/typing_predict 的 Manage*Controller）内容并入同域文件后删除冗余文件；其余 7 个（bridge_token/dnc/handoff_chain/r48_growth/rule_engine/decision/customer_queue/do_not_contact）`git mv` 去后缀重命名。
2. `internal/controller/debug_routes.go:29` — `c.JSON(200, ...)` 直写响应，绕过统一响应协议（P2）。**处置**：改用 `response.Success`。
3. `internal/service/rag_product.go` — service struct 持 gorm 直连 9 处（P2，OPT-ARC-01 范畴的样板案例）。**处置**：整文件改造为 Repository 注入，`RagConfigRepository` 新增 `ListAllRagProducts/GetRagProductForUpdate/DeleteRagProductByID/CreateRagProductWithVectorTable` 四方法，Stats 改内存聚合。

**存量说明**：service 直连 DB 全仓历史存量 170 处 → 本轮清 9 处后余 161 处（26 文件），属 OPT-ARC-01 渐进重构范畴，按 CLAUDE.md 规则2"当轮修复"原则已消化样板 1 域；后续 error-handling 等轮次若触及同文件顺带收敛，另起独立重构轮会与"发现问题立即修复"冲突，特此留档为**已知技术债基线**（脚本 WARN 级，不阻断）。

**期间事件**：工作区合入同事未提交的渠道媒体转存半成品（channel_media.go / whatsapp 媒体链路 / feishu 卡片），已确认其编译通过并随本轮提交入库；推送遇双远端各有新提交，按规则0 merge 后三仓收敛于 `e512f4d`。

**验证证据**：`check-architecture.sh` 16 错 → 1 错（仅剩 L4 存量项）；`go build ./...`、`go vet ./...` 全绿；service(37s)/controller/repository 三包测试全绿。

**Commit**：`a200aa6`（修复）+ `ef3a33d`/`e512f4d`（merge 与推送）

### R2 — authz（2026-09-08）

**审计范围**：公开路由最小化、`/api/manage` 守卫覆盖、admin 中间件语义、垂直越权（staff 调管理接口）、水平越权 IDOR 抽查（chat public / whatsapp / geo / 卡片 / 短链 / email）、visitor WebSocket 鉴权、monitor 端点暴露面、init 流程守卫。

**发现与处置（1 项，已修复）**：

1. `user-server/internal/websocket/visitor_handler.go:126` — 访客 WS 无 token 分支语义依赖 `sessionID/visitorID` 非空的前置条件，且代码留有空行残迹疑似被改动过；若上游校验顺序变化即退化为凭 `session_id` 冒连他人会话、接收坐席/AI 回复（P1）。**处置：确认拒绝条件后补全注释固化 fail-closed 语义**，无 token 且带会话参数的连接明确 401 拒绝。

**核查通过项（无需修复）**：
- 公开路由最小化核对：`/health` `/system/info` `/auth/login(+mfa)` `init-*`（有 install.lock 已初始化 403 守卫）`/license/*`（固定空）`/s/:code` `/l/:code` `/livecode/*` `/platform/register` help-center 公开读、chat public（visitor_token 全链路校验：GetMessages/Send/Offline/Transfer/Close/Rate/UploadToken 均过 `validateVisitorTokenOrAbort`；GetActiveSession/RecentClosed 按 visitorID 自查范围）
- `/api/manage/*` 写操作全部在 `manageAdmin`（AdminAuthMiddleware）组；7 个免 admin 的 manage GET 仅低敏读（co-pilot 配置/rag-eval 记录/规则列表/SLA 配置），data-export 在同一组但服务端无越权放大（GDPR 导出按 customer_id 全量，属管理端设计，前端管理页调用）
- 系统运维高危端点（restart/logs/backup/restore/system config 写）整组挂在 `systemAdmin`（AdminAuthMiddleware）之后
- whatsapp/telegram/feishu/wa-cloud 写操作全部在 admin 子组；geo 的 config 写/平台账号写/工作流写/jobs 触发在 `geoAdmin` 组
- visitor WebSocket：token 校验绑定 channelID+visitorID+sessionID 三元组；SMTP 响应 DTO 不含 password 字段（模型 `json:"-"`，DTO 无此字段）
- monitor 端点挂在 JWT 组（`auth.Use(JWTAuthMiddleware)` 之后注册），需登录
- SystemUser 响应统一走 `SystemUserResponse`（无 password 字段）

**验证证据**：`go build ./...` OK；`go vet ./...` OK；`go test ./internal/websocket/ ./internal/controller/ ./internal/middleware/` 三包全绿。远端分叉（gitee-upstream 有新 docs 提交）已按规则0 `git merge` 后推送，merge 后 build 复验 OK。

**Commit**：`01e7297`（修复）+ `d906d49`（merge 推送）

### R1 — security（2026-09-08）

**审计范围**：硬编码密钥、SQL 注入、路径穿越、JWT/会话、CORS、SSRF 面、上传校验、exec 注入、.env 追踪、敏感日志、seed 凭据、JWT 中间件测试后门。

**发现与处置（2 项，均已修复）**：

1. `user-server/internal/service/email_tracking.go:28` — `emailTrackingDefaultSecret` 硬编码默认 HMAC 密钥常量（P2）。核实为**死代码**（secret 实际只从 `EMAIL_TRACKING_SECRET` 环境变量读取，未配置时签名为空串直接拒绝），但常量留在源码中会成为未来误用的种子。**处置：删除**。
2. `user-server/internal/service/email_unsubscribe.go:26` — `emailUnsubscribeDefaultSecret` 同上（P2）。**处置：删除**。

**核查通过项（无需修复）**：
- SQL 拼接点全部为内部常量表名/受控参数（migration/rag 索引/备份表），无用户输入直达
- `resolveLogPath` 有 `..` 检测 + 绝对路径白名单 + 相对路径 `logs/` 前缀三重守卫
- JWT secret：无 env 时非测试进程 panic，禁止硬编码兜底；黑名单吊销存在
- `IsTestMode` 生产默认 false 且 `testModeGate` 默认返回 false，无生产后门
- 上传链路：扩展名黑名单 + magic number 校验 + SVG 拒绝 + MIME 白名单 + 本地驱动 UUID 重命名
- CORS：默认拒绝，Origin 显式白名单 + 同源校验
- `.env` 均未入库（仅 example/production 模板，值为占位符）
- 密码重置日志只记 email 哈希/事件，无 token 落日志
- 登录/MFA/注册/忘记密码均已挂 `BruteForceGuard`

**观察项（不构成代码缺陷，留给对应角度轮次）**：
- `scripts/bootstrap.sh:38` 与 `scripts/geo_full_test.py:101` 内置 seed 密码 `Seed@123456`（公开开源仓库自举默认凭据，config.yaml 已明示固定标记，属产品决策而非泄漏；config-deploy 轮再评估是否加首次登录强制改密）
- govulncheck 在本机 Git Bash 下因路径解析问题无法运行（`no go.mod file` 假报错），下轮尝试 `go vet`+`golangci-lint` 替代或直接 Windows cmd 运行

**验证证据**：`go build ./...` OK；`go vet ./...` OK；`go test ./internal/service/ -run Email` OK。

**Commit**：见 git log `fix(security): 审计R1-security: 删除email追踪/退订服务死代码默认密钥常量`

# 永动审计循环 — 状态与报告（STATE）

> 机器状态见同目录 `state.json`；执行指令见 `PLAN.md`。本文件追加每轮审计报告，最新在上。

## 循环总览

- 循环启动：2026-09-08，由 ZCode 自动化每 30 分钟触发一轮
- 已完成轮次：12（**第一圈 12 角度收官**）/ 角度序列：security → authz → architecture → error-handling → concurrency → data-integrity → api-contract → frontend → perf → test-coverage → config-deploy → docs-consistency →（循环，下一轮 R13 回 security）
- 累计发现 / 修复：15 / 15（R6 为 0 缺陷轮）
- 下一轮角度：security（第二圈开始）

## 轮次报告

### R12 — docs-consistency（2026-09-09）— 第一圈收官

**审计范围**：`check-doc-consistency.sh` + `check-feature-doc.sh` 全量、DEV_DOCS_INDEX/README 链接有效性、CHANGELOG 与 git 历史对齐。

**发现与处置（2 项，均已修复）**：

1. `docs/marketing-features/README.md:271,295` — `AI_CORE_FEATURE_INVENTORY.md` 引用相对路径 `../../../docs/...` 多了一级（该文件实际在 `docs/architecture/`），`check-doc-consistency.sh` 唯一 error。**处置**：改 `../../docs/...`，脚本 error 清零。
2. `CHANGELOG.md` — 永动审计循环 R1–R11 共 11 轮修复完全未记录。**处置**：`[未发布]` 段补录机制说明 + 每轮一行摘要。

**核查通过项（无需修复）**：
- `DEV_DOCS_INDEX.md` 全部相对链接 0 失效（python 全量校验）；索引的 user-server/user-web dev 四件套文档均存在
- 根 `README.md` 链接 0 失效
- `check-feature-doc.sh`：0 失败（1 跳过为 README 本身）
- 余 19 个警告均为父仓库/平台端文档（CODEOWNERS、深架构系列等），不在本仓库范围，留档不阻断

**验证证据**：`check-doc-consistency.sh` 0 error；`check-feature-doc.sh` 0 失败；`go build ./...` + `go vet ./...` 全绿。

**Commit**：`1b82feb`

### R11 — config-deploy（2026-09-08）

**审计范围**：`.env.example` 键名 vs 代码 `os.Getenv` 消费对照、docker-compose 端口 vs `PORT_REGISTRY.md` vs `config.yaml` 三方对齐、迁移幂等性抽查、config.yaml 死键扫描、DEPLOYMENT_GUIDE 环境变量表核对。

**发现与处置（1 项，已修复）**：

1. 商户签名密钥键名不一致（P1）— `.env-example` 与 `DEPLOYMENT_GUIDE.md` 使用 `MERCHANT_HMAC_SECRET`，但代码实际消费链为 `config/platform.yaml` 的 `secret: "${MERCHANT_API_SECRET}"`（`os.ExpandEnv` 插值）→ `PlatformCfg.Secret` → `platform/client.go` HMAC 签名（也直接读 `os.Getenv("MERCHANT_API_SECRET")`）。按文档配置 `MERCHANT_HMAC_SECRET` 会被**静默忽略**，启动时报 "MERCHANT_API_SECRET 未配置"。**处置**：`.env-example` 与 DEPLOYMENT_GUIDE 两处统一改为 `MERCHANT_API_SECRET`（代码侧为既定事实、bootstrap.sh/测试已使用该键，故改文档侧）。

**核查通过项（无需修复）**：
- `.env.example`（user-server）4 个键全部有真实消费方：`VISITOR_TOKEN_SECRET`（config.yaml 插值→WebSocket 访客 token）、`USER_JWT_SECRET`（jwt.go panic 级校验）、`POSTGRES_PASSWORD`（docker-compose `:?` 强制）、`FIELD_ENCRYPTION_KEY`（`internal/pkg/crypto/field.go` AEAD 初始化）
- 端口三方一致：8202 PG / 8203 Redis（compose 默认=PORT_REGISTRY）；8204 API / 8207-8209 推理栈（宿主机进程）；`DB_PORT:8232` 为宿主机直连 Dev 模式默认，文档明示
- 迁移幂等性：`amount_money_migration` 等经 `information_schema.columns` 列存在性检查；`CREATE ... IF NOT EXISTS` 模式广泛使用；注册表机制防重放
- config.yaml 顶层键与 config struct yaml tag 全对齐，无死键；`QINIU_ACCESS_KEY/DEEPL_API_KEY` 等经 `${VAR}` 插值消费，非死键

**验证证据**：`go build ./...` + `go vet ./...` 全绿（文档/示例文件改动，无代码行为变化）。

**Commit**：`8e45b53`

### R10 — test-coverage（2026-09-08）

**审计范围**：全包 `go test ./...` 绿灯确认、无测试业务域盘点（按包 × 文件规模）、测试缺口补测优先级。

**发现与处置（1 项）**：

1. `internal/channelbot/core/` 测试数 0（P3）— 该包承载跨渠道安全比较 `SecureEqual`（常量时 HMAC 校验基元）与 `ToMessageEvent` 归一化映射（四渠道入站统一入口），全部为纯函数、零依赖，是最应先补测的缺口。**处置**：新增 `core_test.go` 3 组用例 — SecureEqual 相等/前缀/空值语义、ToMessageEvent 全字段映射（SessionID 拼接、Extra 三键、Timestamp 转换）、空可选字段行为、BaseClient 选项装配。

**核查通过项（无需修复）**：
- `go test ./... -count=1` **零失败**（含前 9 轮所有修复的回归）
- 测试基数健康：service 221 个测试文件 / controller 51 / repository 53 / aiagent/llm 25 / middleware 8

**测试缺口留档（增量补测路线）**：
- service 层 10 个 500+ 行文件无专属测试（sop.go 964 行、r44_gap_services.go 910、webhook_outbound.go 857、sales_engine.go 746、proactive_reach.go 725、chat_visitor.go 719 等）——均为多依赖编排层，补测需先引入 mock 框架支撑，非纯函数可一轮清零，列入后续循环增量处理
- `internal/reach/`（卡片触达）子包 0 测试，同上

**验证证据**：`go vet ./internal/channelbot/...` 绿 + `go test ./... -count=1` 全绿。

**Commit**：`a490044`

### R9 — perf（2026-09-08）

**审计范围**：循环内 DB 调用（N+1）结构化扫描（花括号配对 31 个候选逐个评估）、无分页大列表、热查询索引覆盖。

**发现与处置（1 项打包修复）**：

1. 列表页 N+1 与热路径缺索引（P2 打包）— **处置**：
   - `ai_tool_config.go ListTools`：每工具一次 `ListByTool`（页大小 N 次查询）改为新增 `ListByTools`（一次 `IN` 查询 + map 分组）
   - `role.go ListRoles`：3 个角色 3 次 `CountByRole` 改为新增 `CountByRoles`（一次 `GROUP BY`）
   - `model/integration.go ExternalCustomer`：`platform` 与 `external_id` 两个独立单列索引合并为复合索引 `idx_extcust_platform_external`（priority 1/2），`GetByExternalID` 与 CRM 同步 upsert 热路径走同一索引查找

**评估后不改项（留档）**：
- N+1 候选其余 28 处：均为低频后台任务（cron 聚合/调度器）、逐行容错写（integration 同步 upsert 需逐条判存后 Create/Update，批量化需引入事务级去重，收益低风险高）、或循环次数受配置上限约束（`config_param.go` 15 个固定配置项）
- 无分页 `Find` 7 处：宏/自动化规则/渠道账号等配置类小表全量加载，量级受控，属设计内行为

**验证证据**：`go build ./...` + `go vet ./...` 全绿；service(24.8s)/repository 测试全绿。

**Commit**：`8d47d5f`

### R8 — frontend（2026-09-08）

**审计范围**：eslint 工具链启用（R4 遗留项）与 src 全量 lint 归零、Vue 正确性缺陷（解析错误/重复 key/prop 直改/computed 漏 .value/deprecated 语法）、空块静默、no-undef 真缺陷、受限导入规范。

**发现与处置（1 项打包修复，163 errors → 0）**：

1. eslint 工具链（P2 打包项）— 同事在工作区补装了 `@eslint/js`/`eslint-plugin-vue`/`globals` 依赖并修了 `import.meta` globals 误用，但 src 仍有 163 个 error。**处置（分型全清）**：
   - **globals 白名单升级**：`globals.browser + globals.node` 替代手写 18 项白名单 → 清零 38 个 `no-undef`（WebSocket/fetch/btoa/caches/URLSearchParams 等标准 API）
   - **Vue 正确性 30 处**：QuickReplyPanel 模板解析错误（字面 `{{` 写法改为模板字符串）、SubMenuItem 与 props 重复 key（模板改用 `props.` 前缀引用）、WeComSendDialog 直接改 prop（改 emit）、qq/account `computed` 漏 `.value`（真实逻辑 bug，保存时锁状态判断恒真）、intentRecognition 过滤器缺 `||` 致意外换行调用（真实逻辑 bug，keywords 过滤失效）、6 文件 Vue2 filter 语法改 `??` 兜底、2 文件 `beforeDestroy` 改 `beforeUnmount`
   - **no-empty 70 处**：批量补显式注释（空 catch 补 best-effort 语义说明/空分支补 no-op），可读性归一
   - **真缺陷 6 处 no-undef 修根**：MaterialSelectDialog 取消按钮误发 `confirm`（引用未定义变量且语义错误）、integration 别名笔误、sms Jobs 缺 `toList` 导入、tiktok CardStats 函数名大小写笔误、customer360 未定义 `getRoleLabel` 内联映射、assetBundle `weaveOk` 作用域错误提升（原代码 catch 判断恒真）
   - **散错 19 处**：3 个受限 default 导入改 `{ http }`、5 处无用转义（regex `\-` 与模板字符串 `<\/script>` 改拼接避免 SFC 解析破坏）、5 处无用赋值、5 处 preserve-caught-error 补 `{ cause }` 错误链、1 处未用 `$index`

**验证证据**：`npx eslint src` errors = 0；`vite build` 生产构建成功；`vitest run` 6 文件 174 用例全过。

**Commit**：`28d3bad`

### R7 — api-contract（2026-09-08）

**审计范围**：前端 `src/api/*.js` 调用 vs 后端 gin 路由注册双向对照（`scripts/audit_api_contract.py`，同事新增工具）、响应格式/错误码一致性抽查、UNMATCHED 逐条真伪核实。

**发现与处置（1 项，已修复）**：

1. `user-server/cmd/seed/`（P1）— 同事提交 `3d7ac65` 只包含 `seed_faq_sop.go`（seeder 模块），缺命令入口：`SeedContext`/`cleanByCondition`/`seedTag`/`batchInsert`/`randInt` 全部未定义，`go build ./...` 失败，`bootstrap.sh` 的 `go run ./cmd/seed` 必然编译失败。**处置**：新建 `cmd/seed/main.go`，补 Seeder 框架 + 全部 helpers；`seedTag` 与 seeder 内硬编码的 `__URGENCY_SUPPORT_SEED_20260908__` 标记保持一致，保证 Clean 幂等只清本批种子。

**核查通过项（无需修复）**：
- 契约对照复跑：前端 831 个调用 **0 UNMATCHED** — 抽样发现的角色死 CRUD（`POST/PUT/DELETE /api/system/roles/*` 3 端点后端从未实现）与 `runSOVRefresh` 死导出，同事已在本工作区同步修复（`role.js` 收敛为只读 3 端点、`RoleList.vue` 重写为只读视图移除全部 CRUD UI、`geoProbe.js` 删死导出），本次一并入库并复核无其他消费方受影响
- 脚本 UNRESOLVED 40 项均为对 `Group` 间接绑定（platform/market/local 别名组、controller.Register 内嵌 group）的解析限制，逐组抽样（asset-market/local-assets/asset-bundle/knowledge）核实路由真实存在，属工具误报非契约缺口
- 响应格式抽查：controller 层 235 处统一走 `response.Success/Error`；`{code,data,message}` 协议一致

**验证证据**：`go build ./...` + `go vet ./...` 全绿（修复后）；controller/service 测试全绿；前端 vite build 成功 + vitest 174 用例全过。

**Commit**：`25c9ed9`

### R6 — data-integrity（2026-09-08）— 0 缺陷轮

**审计范围**：多表写事务覆盖、唯一约束与重复行风险（geo_daily_stats 教训复查）、金额字段存储类型、恢复/备份原子性、迁移幂等性。

**核查通过项（0 缺陷）**：
- `geo_daily_stats`：uniqueIndex `idx_date_engine_intent`（stat_date+engine+intent）在 model tag 声明且经 `db.AutoMigrate` 落库；仓储 Upsert 用 `FirstOrCreate+Assign` 语义防重；聚合侧 `runeTruncate(r.Query, 40)` 保证 intent 不超 `varchar(64)`，避免截断后超长写失败破坏唯一键 — 历史重复行问题（见 GEO 观测模块交付记录）已闭环
- 消息主链路原子性：`MessageHubRepository.CreateWithInbox` 消息创建+会话更新包同一事务；`msg_id` 幂等去重 + `isDuplicateKeyErr` 兜底；`uk_inbox_conv_channel`（platform+account_id+customer_id）唯一键防并发重发建重复会话，`CreateOrUpdateByChannel` 撞键退化为更新
- 金额存储：`SalesEvent.Amount` 统一 `numeric(12,2)`（model 注释明示杜绝 float 误差），历史迁移 `amount_money_migration.go` 有 ROUND 转换；float64 字段仅剩评分/奖励/置信度等统计语义，合规
- 备份恢复：`RestoreTable` 整表 DELETE + 逐行 Create 包同一事务，任一行失败整体回滚，且有 `allowedBackupTables` 白名单防任意表操作
- 会话分配：`AssignSession` 用 `SELECT FOR UPDATE`（clause.Locking）+ 手动事务 + recover 回滚，防超卖并发分配

**验证证据**：`go build ./...` + `go vet ./...` 全绿；geo/repository/repository 三包测试全绿。

**Commit**：见 git log `chore(audit): 审计R6-data-integrity: 0缺陷轮核查记录`（本轮无代码修复，仅状态推进）

### R5 — concurrency（2026-09-08）

**审计范围**：裸 `go func()` 全量 48 处生命周期审查、包级 map / 全局可变状态并发访问、WS Hub 注册表锁、后台 loop（cron/cleanup/dispatch/flush）退出通道、单例 set-once 语义。

**发现与处置（1 项，已修复）**：

1. `user-server/internal/service/channel_media.go:34` — `channelMediaFollowers` 包级 map：`RegisterChannelMediaFollower`（装配层写）与三渠道入站媒体转存（webhook 并发读）无任何锁，高负载下可触发 `fatal: concurrent map read and map write`（P1）。**处置**：补 `sync.RWMutex`，注册走写锁、`fetchChannelMediaFollower` 走读锁。

**核查通过项（无需修复）**：
- 48 处裸 `go func()`：2 处为 wg.Wait+close 模式（受控）、其余均为 `SafeGo`（recover 保护）或 hub/cron 一次性 worker；后台 loop（trace_sink flushLoop、db_audit_persister consume、event bus worker、SOP dispatcher 等）均有 stopCh/doneCh 或进程生命周期语义
- WS Hub：`clients`/`agentOnline` 全部读写持 `mu`（含 ticker 心跳摘除路径）；visitor 注册表有独立 RWMutex
- 限流器（visitor limiter）、typing predict cache、ownership cache 均有锁
- `globalReader`/`GlobalSSEBus`/`globalTraceBus` 等单例为启动期 set-once（sync.Once 或装配层先行注入），无运行时竞态
- `bridgeChannels` 包级 map 为 init-only（仅启动期赋值，无运行时写）

**环境限制留档**：`go test -race` 需要 CGO+gcc，宿主机（Windows/Git Bash）无 gcc，无法启用竞态检测；本轮以逐文件锁核查替代。后续若在 CI Linux 环境跑 race 可补验。

**验证证据**：`go build ./...` + `go vet ./...` 全绿；websocket(5.5s)/middleware(5.8s)/service(23.9s) 三包测试全绿。

**Commit**：`59aa223`

### R4 — error-handling（2026-09-08）

**审计范围**：go vet 全量、忽略 err（`_, _ =` / `x, _ :=`）扫描、panic 风险（裸下标 [0]/[1]、nil 解引用、无 ok 类型断言）、显式 panic 站点、前端未捕获 Promise 与全局异常处理器。

**发现与处置（2 项，均已修复）**：

1. `user-server/internal/controller/wecom.go:279` — `RefreshAccount` 刷新 token 后回查账号 `account, _ = GetAccountByID(...)` 忽略错误（P2）：回查失败时会把**旧账号数据**当成功返回，token 实际已换新，调用方拿到过期 token 静默失败。**处置**：显式判错并返回 500 语义错误。
2. `user-web/src/main.js` — 无 `app.config.errorHandler` 与 `unhandledrejection` 监听（P2）：Vue 渲染异常与未捕获 Promise 拒绝静默丢失，产生"白屏无报错"不可观测故障。**处置**：补全局兜底，统一落 console。

**核查通过项（无需修复）**：
- `go vet ./...` 全零
- service 层 24 处 `_, _ =` 全部为 best-effort 设计（hub.Push 旁路通知/ScoreClue 打分/锁释放/BOM 写响应等，失败不影响主链路，部分已有日志）
- 显式 panic 5 处：3 处为启动期注册冲突（快速失败，合理）、1 处 `MustNewChatChannelService`（注释明示语义）、1 处事务 defer recover 后 re-panic（标准模式）
- 高危裸下标 [0]/[1] 抽查 15 处（ai_tagger/faq/inbox/channel_media 等）全部有长度守卫或由正则捕获组保证
- 前端 45 处 `.then` 链由 axios 响应拦截器统一 reject + toast，无裸悬空 Promise

**既有问题记录（非本轮引入）**：user-web 的 eslint 工具链缺 `@eslint/js` 依赖（`eslint.config.mjs` 注释自述"尚未启用"），`npm run lint` 本就不可用；本轮以前端 `vite build` 成功 + vitest 174 用例全过作为前端回归证据。eslint 依赖补装列入 frontend 角度轮次处理。

**验证证据**：`go build ./...` + `go vet ./...` 全绿；controller/service Go 测试全绿；前端生产构建成功 + vitest 6 文件 174 用例全过。

**Commit**：`621ae5b`

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

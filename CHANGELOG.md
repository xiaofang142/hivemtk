# 更新日志 (Changelog)

本项目所有重要变更都会记录在此文件。版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/) 规范。

## [未发布] - 2026-Q3

### 新增 (Added)

#### 永动审计循环 R1–R11 (2026-09-08/09，12 角度循环第一轮，进行中)
- 机制：`scripts/audit-loop/`（PLAN.md 12 角度手册 + state.json 机器状态 + STATE.md 报告），ZCode 自动化每 30 分钟一轮，发现问题当轮修复、每轮提交推送
- **R1 security**：删除 email_tracking/email_unsubscribe 死代码默认 HMAC 密钥常量
- **R2 authz**：访客 WebSocket 无 token 连接 fail-closed 语义固化（防凭 session_id 冒连他人会话）
- **R3 architecture**：命名违规 12 处清零（5 个 manage 控制器并入同域文件 + 7 个去冗余后缀）、debug_routes 响应协议归一、rag_product 服务 Repository 化（9 处直连 DB）
- **R4 error-handling**：wecom RefreshAccount 忽略错误回查修复、前端补全局 errorHandler/unhandledrejection 兜底
- **R5 concurrency**：channelMediaFollowers 包级 map 并发读写补 RWMutex（三渠道媒体转存并发读）
- **R6 data-integrity**：0 缺陷轮（事务覆盖/唯一约束/金额存储/恢复原子性/迁移幂等全部核查通过）
- **R7 api-contract**：cmd/seed 缺命令入口致 go build 失败，补 main.go（Seeder 框架 + helpers）；前后端契约对照 831 调用 0 UNMATCHED
- **R8 frontend**：eslint 工具链启用收尾，src errors 163 → 0（含 2 个真逻辑 bug：computed 漏 .value、意图过滤缺 ||）
- **R9 perf**：ai_tool/role 列表页 N+1 批量化（IN 查询/GROUP BY），ExternalCustomer 补复合索引
- **R10 test-coverage**：channelbot/core 补测 0→3 用例；全包 go test 零失败
- **R11 config-deploy**：商户签名密钥键名统一 MERCHANT_API_SECRET（文档侧与代码不一致会致启动报错）

### 安全 (Security)

#### 用户端质量飞轮 (2026-08-17 多角度轰炸测试 + 修复)
- **客户创建输入校验** (`internal/service/customer.go` + `internal/controller/customer.go`)
  - 拒绝空 body（全部标识符为空）→ 400 `INVALID_PARAM_1001`，杜绝脏数据入库
  - 校验手机号格式（中国 11 位 `^1[3-9]\d{9}$`）→ 400
  - 校验邮箱格式 → 400
  - 已存在客户的更新只覆盖**非空**字段（避免部分更新把其它标识符清空）
  - **并发竞争幂等**：PG 23505 → 重新 FindByIdentity 返回已存在行，避免并发 10 个请求 → 4 个 500（10 并发测试已绿，DB 仍 1 行）
- **暴力守卫语义修正** (`internal/middleware/brute_force.go`)
  - 移除 `BruteForceGuard` 中重复自增的 `entry.failures++` 与 `failures = 0` 死代码（双计数导致 3 次失败即锁，与配置的 `MaxFailures=5` 不一致）
  - 守卫职责收敛为"判定是否已锁定"；真实计数/加锁由 `RecordBruteForceFailure` 单点维护
  - 删除无人调用的 `getBruteForceEntry`（返回临时 struct，自增后被丢弃，误导性强）
- **多角色越权修复**（二轮发现 5 类 P0 漏洞，全部 admin only）
  - **LLM 路由** (`internal/router/service_routes.go`)：Models/Strategies/SceneRouting/Fallback/Provider 写操作 admin only；staff 可注入恶意 base_url 重定向全公司 LLM 流量到 evil.com 窃取对话数据
  - **第三方集成** (`internal/router/business_routes.go`)：Create/Update/Delete/Test/Sync admin only；含 corp_secret 等敏感凭据
  - **企微账号** (`internal/router/chat_routes.go`)：Create/Update/Delete/Sync/Send/Refresh admin only
  - **chat-channels 渠道** (`internal/router/chat_routes.go`)：Create/Update/Delete/RotateKey/ResetSecret admin only；含 app_key/app_secret
  - **AB 实验** (`internal/router/business_routes.go`)：Create/Update/Delete/Start/Pause/Stop admin only
  - **WhatsApp** (`internal/router/platform_routes.go`)：CreateAccount/CreateJob/DeleteJob/StartLogin 等 admin only
  - **前端别名路由** (`internal/router/frontend_aliases.go`)：新增 `doRegAdmin` 工具，确保 alias 路径同样受保护
- **回归单测** (`internal/middleware/brute_force_test.go` + `internal/service/customer_test.go`)
  - `BruteForceGuard` 3 用例：纯请求不锁 / 5 次失败第 6 次 429 / Clear 循环不锁
  - `CustomerService.CreateOrUpdate` 4 用例：空 body 拒绝 / 5 种格式错误拒绝 / 纯第三方 ID 通过 / 部分更新保留其他字段
- **多角度负面测试脚本** (`tests/e2e/deep_security.sh`, 54 用例)
  - 鉴权 / Token / 登出 / 用户枚举 / 暴力 / CORS 反射 / IDOR / 客户创建校验 / 文件上传类型 + 路径穿越 + 魔术数字 / 边界 / 桥接端点 / **多角色越权 32 用例** 全部 PASS
- **SSE 端点编译占位** (`internal/bridge/sse_stub.go`)：另一迭代 v3 SSE 端点骨架引用了未定义类型（`SSEHandler`/`NewSSEHandler`/`SSEEvent`），本轮加 stub 让 `go build ./...` 恢复 EXIT=0

### 新增 (Added)

#### Bridge 桥接架构 (Chrome 扩展 + 三通道 HTTP 协议)
- **M2-P1 Popup 增强**:健康度面板/告警横幅/紧急停止/多账号/错误码友好化
  - `src/popup/health.js` 实时健康度面板（circuit-breaker 状态、延迟 P50/P95、错码分布、死开关）
  - `src/popup/alert-banner.js` 熔断/无响应时自动弹红色横幅
  - `src/popup/emergency-stop.js` 一键停所有桥接活动
  - `src/popup/error-messages.js` 18 种错误码中英文双语 + 解决建议
- **M1-P0 全面升级** (桥接代码质量 10 项 P0 修复):
  - 协议升级强制 `conversation_id` 入参（v1/v2 兼容）
  - `AckOutboundItem.Error` 字段透传
  - `req.Status` 决定终态（delivered/failed）
  - 单 SQL `UPDATE...RETURNING` 原子化合并 Get+Update
  - 500 条 IN 性能基准（< 200ms 阈值）
  - 跨账号探测安全测试（not_in_scope 状态）
  - PROTOCOL 常量共享单源化
  - `not_found` 区分 GC 回收 / 归属错
  - `_pendingAck` 最大重试次数 + 指数退避（10 次 / 1s→60s cap / 24h TTL）
- **M0 桥接架构重构**:
  - 三通道独立 HTTP（uplink / outbox / ack 相互独立）
  - Token 从 URL 迁移到 Authorization Header
  - 熔断器（Circuit Breaker）三态机
  - RateLimiter LRU+TTL 防内存泄漏
  - 死开关（Dead Man's Switch）健康度上报
  - 人类化模块（贝塞尔曲线鼠标轨迹/正态分布键入延迟）
  - 配置热更新（chrome.storage 监听）
  - 端到端请求 ID 追踪

#### 企业级合规
- 审计中间件（who/when/from_ip/action/resource/result/trace_id）
- 敏感数据脱敏中间件（手机/邮箱/身份证）

#### 文档
- `BRAINSTORM_2026-08-15_8D_REPORT.md` 8 维度报告（产品/企业/开源/代码质量）
- `bridge_10_of_10_task_list.md` 4 维度任务清单（44 项 / 283h）

### 变更 (Changed)

- **前端 popup** 全新 UI：紧急停止/多账号/健康度面板/告警横幅
- **后端 ack 协议** v1→v2 平滑升级（conversation_id 过滤/failed 终态/Error 字段）
- **巡检风控** 30-60s 轮间隔 + 6 个会话/轮 + 120s 会话冷却 + 3-5s 切换间隔
- **Token 传输** URL query → Authorization Header

### 修复 (Fixed)

- 双巡检冲突（删除 PollingLoop._patrol，保留 BaseAdapter._startPatrolAuto）
- `OutboxMessage.Extra` 字段缺失（前端主动私信路由失效）
- `account_id` 缺失时 default 兜底（垃圾数据风险）
- mergedEvent.EventID 使用新 UUID（幂等性丢失）
- ack 失败导致用户收到重复消息
- 高频巡检触发平台风控（30-60s 节奏调整）
- 16 个 `getPeerName FULL DIAG` 冗余打印删除

### 弃用 (Deprecated)

- PollingLoop 双巡检（保留 L1 巡检作唯一源）
- WebSocket 桥接协议（改为 HTTP 长轮询）
- Token in URL query（迁 Authorization Header）

### 安全 (Security)

- 桥接 Token 强制 Header 传输
- 跨账号探测返回 `not_in_scope`（不告知归属，防越权信息泄露）

---

## 历史版本 (Historical Releases)

> 历史版本见 [GitHub Releases](https://github.com/xiaofang142/hivemtk/releases) ·
> [Gitee Releases](https://gitee.com/xhpmayun/hivemtk/releases)

### 2026-Q2 早期迭代 (v0.1 - v0.4)

#### v0.4 (2026-Q2 末)
##### 新增
- **宿主机推理栈迁移方案**:llama.cpp (Qwen2.5) + TEI (bge-m3 + bge-reranker-v2-m3) 从容器化迁移到宿主机,节省 CPU/内存、提升推理性能
- 5 阶段并行 + 双层架构 + WebSocket 流式输出(FeatureFlag 灰度控制)
- 4 级 LLM 降级链(7B → 3B → 规则 → 兜底回复)
##### 变更
- 推理栈端口固定为 LLM :8207 / Embedding :8208 / Rerank :8209
- 支持 dev / prod 模型档切换(Qwen2.5-3B / 14B)

#### v0.3 (2026-Q2 中)
##### 新增
- 94 个营销业务模块(marketing-features 文档体系)
- 三级 RAG 检索:粗排(向量召回) + 精排(bge-reranker) + LLM 改写(HyDE / Query Rewriter)
- 多智能体协作:被动应答智能体 + 主动触达智能体(ADR-013)
- AI 销冠:话术模板 + RAG + 自动跟进
- 七端社媒触达:抖音/快手/小红书/闲鱼/TikTok/企业微信/邮件
##### 变更
- 后端 Go 五层架构(Controller → Service → Repository → Model → DTO)硬约束落地,`scripts/check-architecture.sh` 入库 CI

#### v0.2 (2026-Q2 初)
##### 新增
- 41 个 ReAct 智能体工具注册表
- 统一 CDP 客户视图 + 统一消息中心
- 营销自动化 SOP 可视化编排
- 企业微信 SCRM 多账号聚合 + 离职继承
##### 安全
- JWT 鉴权 + 角色权限 + 行级数据权限(data_scope)

#### v0.1 (2026-Q2 初,首个可运行版本)
##### 新增
- 初始 Go 后端(gin + GORM + pgvector)+ Vue 3 前端
- PostgreSQL 15 + pgvector(1024 维) + Redis 7 数据层
- 5 分钟一键部署(`make install`)
- AGPL-3.0 开源,发布 GitHub / Gitee 双仓库

> 📌 各版本具体 commit 记录见 [GitHub Releases](https://github.com/xiaofang142/hivemtk/releases) · [Gitee Releases](https://gitee.com/xhpmayun/hivemtk/releases)

---

**图例**:
- 🆕 新增
- 🔧 变更
- 🐛 修复
- ⚠️ 弃用
- 🔒 安全
- 📚 文档

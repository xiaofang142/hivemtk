# HiveMTK 完整功能清单（2026-11-16 全量盘点）

> 口径：后端 API 面 = `user-server/internal/router/*.go` 实际注册路由（2026-11-16 提取，
> 共 **1231** 条唯一 method+path）；前端页面 = `router-list.md`（**258** 条，2026-11-13 N2/O 轮
> 全量回归 0 缺陷）；渠道能力 = `docs/capabilities.md`（capregistry 注册表钉死）。
> 本清单是「逐域第一性原理审计」的骨架：每域一节，审计结论直接回填本文件对应小节。

## 审计执行摘要（2026-11-16 第一性原理逐域审计）

**验证证据（全部实跑）**：
- `go build ./...` ✅ exit 0；`go vet ./...` ✅ exit 0（无任何告警输出）
- `go test ./...` 全量：**非门禁真实失败 0**（6523 个失败事件全部为
  `POSTGRES_TEST_PASSWORD` 缺失的 fail-closed 门禁——未发起任何连接，AGENTS.md §3 明确
  非代码问题）；唯一真实失败 `TestPublishIsNonBlockingAndNeverDrops`（tracing）已定位修复
- 扩展 MV3 vitest：**102 文件 / 1249 通过 / 7 跳过 / 0 失败** ✅
- user-web eslint `--max-warnings 80`：✅ exit 0（门 80）
- user-web vitest：✅ exit 0
- embed-sdk node:test unit + boundary：✅ 全部通过（ws/e2e 为需活服务器的集成脚本，
  不在 `test:all` CI 门内，口径一致）

**系统性扫描结论（全库 grep 实证）**：
1. SQL 注入面：repository/service 内 `fmt.Sprintf` 拼 SQL 0 命中（全部参数化或 EscapeLike）
2. `exec.Command` 仅 geo/site.go 部署 hugo 用固定参数 2 处；`http.Client` 全部 12 处实例化均带
   Timeout（无裸 client）；请求路径 `filepath.Join`/`os.Open` 直拼请求参数 0 命中
3. git 追踪敏感文件 0（`.env` 未入库，仅 `.env-example`；pg_data/logs/uploads 均未追踪）
4. 订单 HMAC 回调：规范化签名串含 platform、恒时比较、时间窗+nonce 防重放、
   MaxBytesReader、双密钥轮换——fail-closed 无密钥 503
5. MCPGuard：凭证对 sha256 恒时比较 + 歧义即拒（同 client_id 多租户）+ 租户绑定不接受覆写
6. BridgeIngressGuard：恒时比较 + fail-closed（未配置拒绝，off 需显式逃生阀）+ query 凭证清痕
7. 租户隔离：GORM callback + 数据库内省登记（tenancyidx）+ 对抗性测试钉住；
   后台调度链走 WithSystemScope 拣选 → OnlyTenant 执行的显式租户钉回

**本轮修复（1 项真实缺陷）**：
- `internal/pkg/tracing`：`TestSinkNoDropsUnderNormalLoad` 向共享 sink 通道灌 5 万条后不排空
  即返回，先序残留使后续 `TestPublishIsNonBlockingAndNeverDrops` 撞满通道误报丢帧。
  修复：源头用例收尾排空（5s 超时兜底）+ 不变式用例测量前防御性等待。修复后两用例 PASS。

## A. 身份与访问（identity / middleware / platform）——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| A1 | 认证（登录/登出/刷新，SaaS+独立部署双形态） | `auth` ×25 | login/profile | ✅ 通过 |
| A2 | 用户/成员管理 | `user` `users` `members` | system | ✅ 通过（写口挂 AdminAuth） |
| A3 | 菜单/角色/授权 | `menu` `roles` `permissions` | setup | ✅ 通过 |
| A4 | 多租户上下文（TENANT_MODE fail-closed） | middleware 全链 | 全局 | ✅ 通过 |
| A5 | 审批流 | `approval` | approval | ✅ 通过（测试为 DB 门禁） |
| A6 | 数据范围门（data_scope） | data_scope_gate | 全局 | ✅ 通过（漏装配=拦，fail-closed） |
| A7 | MCP 凭证签发/吊销 | `mcp/credentials` | - | ✅ 通过 |

- 第一性判定：非公开路由全部 JWT+Tenant+RoutePolicyGuard+DataScopeGuard 链；
  初始化三入口 SetupOnlyGuard + service 层二次核对（装完即 403）；
  刷新令牌回源重签（角色/租户/数据范围取库内现值）+ 一次性轮换拉黑旧令牌；
  登录/注册/忘记密码均挂 BruteForceGuard；初始超管密码系统级保护。
- gin RouterGroup.Use 快照语义的装配顺序已被注释与测试钉死（monitor 组等）。

## B. 客户域（customer / inbox / clue）——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| B1 | 客户 CRUD/标签/合并 | `customer` ×20 | customer | ✅ 通过 |
| B2 | 客户 360（画像/标签规则） | `customer-360` ×14 | customer360 | ✅ 通过 |
| B3 | 会话收件箱（多渠道统一） | `inbox` ×23 `sessions` ×22 | chat/inbox | ✅ 通过 |
| B4 | 线索挖掘 | `clue` ×7 | leadMining | ✅ 通过 |
| B5 | 分群/标签圈人 | `user-segment` ×6 | userSegment/tagSegmentation | ✅ 通过 |
| B6 | 客户旅程/事件 | `events` ×10 | customerJourney | ✅ 通过 |
| B7 | 流失评分 | `churn` ×11 | churn | ✅ 通过 |
| B8 | OneID 识别 | oneid | - | ✅ 通过 |

- 标签查询 `tags::jsonb @>` 参数化 + escapeJSONString；LIKE 走 utils.EscapeLike。

## C. 触达与渠道（reach / bridge / channelgw）——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| C1 | 统一触达管道（8 渠道发送/召回/模板） | `reach` ×25 | reachPipeline | ✅ 通过 |
| C2 | 桥接入站/出站（蜂桥扩展） | `bridge` ×10 | bridge | ✅ 通过 |
| C3 | 渠道账号管理 | `platform-accounts` `channels` | 各渠道页 | ✅ 通过 |
| C4 | WhatsApp（Bot + Cloud API） | `whatsapp` ×12 | whatsapp* | ✅ 通过 |
| C5 | Email（SMTP/模板/任务） | `email` ×17 | email | ✅ 通过 |
| C6 | 企业微信 | `wecom` ×18 | wecomAccount | ✅ 通过 |
| C7 | Telegram/飞书/钉钉 | telegram/feishu/dingtalk | 同名页 | ✅ 通过 |
| C8 | SMS（阿里云） | sms | sms | ✅ 通过 |
| C9 | 渠道能力注册表（fail-closed 能力闸） | capregistry | capabilities.md | ✅ 通过 |

- 不可撤回外发的第一性保障链：ClaimJob 条件 UPDATE 原子抢占（RowsAffected 判定）→
  心跳 1min 保活 → stuck 10min 恢复 → GCRA 限流 → 合规判定器（地区×行业，失败语义
  显式：非法策略放行但 ERROR 留痕）→ 全局退订 DNC → 静默时段队列 → 日上限。
- 后台调度链租户钉回：WithSystemScope 拣选 + OnlyTenant(行租户) 执行，不落默认租户。

## D. AI 能力（aiagent / llm / knowledge）——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| D1 | LLM 路由（providers/models/failover/缓存） | `llm` ×30 | llmRouting | ✅ 通过 |
| D2 | 智能体运行时（ReAct 推理循环） | `agent` ×21 `agents` ×9 | aiAgent | ✅ 通过 |
| D3 | 意图识别/工具链 | `intent` ×21 | intentRecognition | ✅ 通过 |
| D4 | 知识库/RAG（向量检索+重排） | `kb*` `knowledge` ×N | knowledge* | ✅ 通过 |
| D5 | GEO 优化（生成引擎可见性） | `geo` | geo | ✅ 通过 |
| D6 | 拟人化/置信度/防抖 | humanize/confidence | 同名页 | ✅ 通过 |
| D7 | SOP/工作流编排 | `sop` `workflow` `flows` ×25 | sopAgent/workflowOrchestrator | ✅ 通过 |
| D8 | A/B 实验（含 CUPED） | `ab-experiments` ×10 | abExperiment | ✅ 通过 |
| D9 | 自动回复（闲鱼 WS/滑块/去重） | aiagent/agent/browser | - | ✅ 通过 |
| D10 | 对话评测 | `dialogue-eval` ×7 | dialogueEval | ✅ 通过 |
| D11 | AI 工具/MCP servers/自定义工具 | `ai-tools` `mcp-servers` `custom-tools` | - | ✅ 通过 |
| D12 | 反例库（bad case） | `bad_case` | - | ✅ 通过 |
| D13 | 智能体记忆 | `memory` ×10 | - | ✅ 通过 |

- RAG 检索租户隔离靠 Table 触点 + 隔离 callback（fail-closed），发布可见性闸
  kbrelease.WhereVisible 逐查询收口；向量召回 vecLiteral 走参数绑定。
- KB 连接器凭据读侧脱敏（kbConnectorSecretFields）+ 顺序固定（修过前端卡片跳位）。

## E. 营销资产——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| E1 | 多平台卡片（抖音/快手/TikTok/闲鱼/小红书） | `douyin` 等 ×45 | *Card | ✅ 通过 |
| E2 | 短链 | `shortlink` ×10 | shortlink | ✅ 通过 |
| E3 | 活码/二维码 | `live-codes` ×12 | livecode | ✅ 通过 |
| E4 | 素材库 | `material` ×13 | material | ✅ 通过 |
| E5 | 脚本库 | `script-library` `scripts` ×16 | scriptLibrary | ✅ 通过 |
| E6 | 模板/模块/资产市场 | `template-market` ×13 `module-market` ×8 | assetMarket | ✅ 通过 |
| E7 | 素材包（bundle） | `asset_bundle` | asset-bundle | ✅ 通过 |
| E8 | 竞品监控 | `competitors` | competitor | ✅ 通过 |

- 公开跳转 `/s/:code` `/l/:code` 挂 PublicCodeTenant 中间件；写口全部 AdminAuth。

## F. 数据与报表——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| F1 | 报表/自定义报表 | `reports` ×9 `custom-reports` ×7 | reports | ✅ 通过 |
| F2 | 仪表盘（含 SSE 推送） | `dashboards` ×9 | dashboard | ✅ 通过 |
| F3 | 可观测性（obs/probe/SLO） | `obs` ×8 `probe` ×9 | observability | ✅ 通过 |
| F4 | 备份/恢复 | `backup` ×8 | backup | ✅ 通过（systemAdmin 档） |
| F5 | 数据字典 | `dicts` | dictionary | ✅ 通过 |
| F6 | 配置参数 | `config_param` | system | ✅ 通过 |
| F7 | 账单 | `bill` | bill | ✅ 通过 |
| F8 | 数据迁移/ETL | `migration` ×7 | migration | ✅ 通过 |

- 报价/账单/回款竖切：金额 numeric(14,2)+float64 有量程护栏（PaymentAmountInRange），
  合计单一来源 quoteSumAmount（生成腿与发送腿共用，注释写明"两处各写一个循环迟早差一分"），
  行级状态机有测试钉住。回款回调与 HMAC 订单回调接线顺序由装配测试守着。

## G. 系统管理——✅ 审计通过

| # | 功能 | 后端路由域 | 前端 | 审计状态 |
|---|---|---|---|---|
| G1 | 系统初始化（init-admin） | `system` ×14 | setup | ✅ 通过 |
| G2 | 平台侧协作（心跳/授权/SLO） | `platform` `manage` ×23 | - | ✅ 通过 |
| G3 | 文件上传/下载（files_guard） | files | 各页上传 | ✅ 通过 |
| G4 | 浏览器自动化 | `browser-automation` | - | ✅ 通过 |
| G5 | EGO lite 工作流 | `egolite` | - | ✅ 通过 |
| G6 | 内容安全（contentguard） | `content` ×9 | - | ✅ 通过 |
| G7 | 集成/连接器 | `integrations` ×8 `kb-connectors` | integrations | ✅ 通过 |
| G8 | 订单 webhook/草稿 | `order_draft` | orders | ✅ 通过 |
| G9 | 人工任务/定时任务 | `tasks` ×11 `cron` | tasks | ✅ 通过 |
| G10 | WebSocket/事件总线 | ws/event | 全局 | ✅ 通过 |
| G11 | 健康检查 | `health` `/health` | - | ✅ 通过 |
| G12 | 静态资源/PWA/embed 托管 | embed_static | - | ✅ 通过 |

- files 守卫：同源可执行扩展名 403 + 签名凭证 + nosniff/sandbox（2026-11-05 二次审计收口）。
- /metrics、/__debug__/routes 均 JWT+Admin 双守卫；调试路由默认仅本地注册（空值=非本地）。

## H. 前端管理端（user-web/src，Vue3+ElementPlus+Pinia）——✅ 审计通过

- 258 条页面路由，85 个路由模块，80 个视图目录——**2026-11-13 N2/O 轮已完成双形态全量回归，0 缺陷**
- 本轮代码级抽查：token 刷新单飞锁 + 15 分钟预刷新（tokenRefresh.js 与后端一次性轮换对齐）；
  路由守卫双 401 竞态闸 + 403 降级 + 零菜单落地 /profile（2026-11-13 修复复验）
- eslint 门 `--max-warnings 80` exit 0 ✅；vitest exit 0 ✅

## I. Chrome MV3 扩展（user-web/bridge）——✅ 审计通过

- 渠道适配器 `src/channels/`（10 平台）+ 共享 `src/core/`（熔断器/限流/选择器健康/净化）
- 凭证一律走 `X-Bridge-Token` 头（不放 URL query，P0-A 口径）；租户短码 query 与服务端对齐
- vitest：**102 文件 / 1249 通过 / 7 跳过 / 0 失败** ✅

## J. embed-sdk 客服浮窗（/embed/* + chat/public 公开通道）——✅ 审计通过

- IIFE 产物由 user-server 托管；innerHTML 命中均为静态 SVG 常量，无动态插值
- node:test unit + boundary ✅ 全部通过；ws/e2e 为活服务器集成脚本（CI 门 test:all 不含，
  口径一致）；CORS 允许头含 X-Chat-* 四枚（ADR-011 跨域部署口径）

## K. 发布链（deploy/）——✅ 审计通过

- 链 A `deploy-api.sh`：`set -Eeuo pipefail`、镜像+二进制双回滚点、双健康检查（本地+公网）、
  状态台账落 `deploy/.api-deploy-state.json`、DRY-RUN 档
- 链 B 扩展发版双门（package.json + manifest.json 同时 bump + CHANGELOGS 条目）
- 端口注册表 PORT_REGISTRY.md；数据层 PG15+pgvector(8202) Redis(8203)；推理栈 LLM(8207)/Embedding(8208)/Rerank(8209)

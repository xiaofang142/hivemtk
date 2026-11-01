# API 全量清单（user-web/src/api → user-server）

> 生成：`node user-web/scripts/gen-api-list.mjs`　扫描文件：122 个　端点数：1148 条
> 状态图例：`☐ 待测` · `✅ 通过` · `🔧 已修复` · `⛔ 阻塞` · `➖ 不适用`（off=独立部署 / on=SaaS 双形态各记一次）
> 复核判据：HTTP 状态 + 响应体 `code` + 数据落库（写操作）+ 租户隔离（on 模式）
> 台账维护：生成器只出骨架；状态/备注列由逐条手工验证后回填（禁止重新生成覆盖，会清空全部测试记录）
> 2026-10-31 修复：orderDraft/whatsappCloud 行归位去重、whatsapp 补 #14、新增 chatPublic.js 12 条、未解析 `{base}` 路径按源码落成真路径
> 2026-11-01/02 全量回归完成：1148/1148 逐条 curl 验证（待测 0）。本轮修 8 个缺陷（openapi 部分更新数据丢失、短链删访问记录 FK、审计 module 误写资源 ID、QQ 编辑空密钥不可存、上游错误 400/500 误分类 ×2、删消息假成功、prompt 候选 SQL 原文泄漏、kbconnector .txt/错误回显），13 条阻塞均为环境依赖（LLM/外网/ego-runner）

## 汇总

| 模块 | 端点数 | 待测 | 通过 | 修复 | 阻塞 |
|---|---|---|---|---|---|
| users.js | 8 | 0 | 5 | 3 | 0 |
| tenant.js | 2 | 0 | 2 | 0 | 0 |
| permission.js | 3 | 0 | 2 | 1 | 0 |
| role.js | 3 | 0 | 3 | 0 | 0 |
| systemUser.js | 5 | 0 | 4 | 1 | 0 |
| system.js | 3 | 0 | 3 | 0 | 0 |
| menu.js | 4 | 0 | 4 | 0 | 0 |
| configParams.js | 8 | 0 | 8 | 0 | 0 |
| moduleMarket.js | 17 | 0 | 17 | 0 | 0 |
| platform.js | 2 | 0 | 2 | 0 | 0 |
| platformAccount.js | 8 | 0 | 6 | 2 | 0 |
| abExperiment.js | 11 | 0 | 10 | 1 | 0 |
| abExperimentPlus.js | 5 | 0 | 0 | 5 | 0 |
| agentKBBinding.js | 5 | 0 | 2 | 3 | 0 |
| aiAgent.js | 9 | 0 | 6 | 3 | 0 |
| aiProductivity.js | 2 | 0 | 1 | 1 | 0 |
| aiTool.js | 7 | 0 | 6 | 1 | 0 |
| approval.js | 2 | 0 | 2 | 0 | 0 |
| assetBundle.js | 16 | 0 | 6 | 10 | 0 |
| assetMarket.js | 12 | 0 | 8 | 4 | 0 |
| backup.js | 7 | 0 | 4 | 3 | 0 |
| badCase.js | 8 | 0 | 3 | 5 | 0 |
| batchOperation.js | 10 | 0 | 2 | 8 | 0 |
| bridgeExtension.js | 1 | 0 | 1 | 0 | 0 |
| bridgeToken.js | 2 | 0 | 2 | 0 | 0 |
| browserAutomation.js | 31 | 0 | 29 | 2 | 0 |
| bulkMessaging.js | 7 | 0 | 0 | 7 | 0 |
| capabilityDiagnostics.js | 1 | 0 | 1 | 0 | 0 |
| channelAgentBinding.js | 5 | 0 | 1 | 4 | 0 |
| chat.js | 1 | 0 | 0 | 1 | 0 |
| chatChannel.js | 7 | 0 | 2 | 5 | 0 |
| chatPublic.js | 12 | 0 | 12 | 0 | 0 |
| churnPrediction.js | 12 | 0 | 5 | 7 | 0 |
| clue.js | 4 | 0 | 0 | 4 | 0 |
| cockpit.js | 5 | 0 | 3 | 2 | 0 |
| collaboration.js | 9 | 0 | 4 | 5 | 0 |
| community.js | 10 | 0 | 1 | 9 | 0 |
| contactConsent.js | 4 | 0 | 0 | 4 | 0 |
| conversionFunnel.js | 2 | 0 | 0 | 2 | 0 |
| csat.js | 7 | 0 | 3 | 4 | 0 |
| customer360.js | 10 | 0 | 1 | 9 | 0 |
| customerEvent.js | 11 | 0 | 2 | 9 | 0 |
| customerJourney.js | 6 | 0 | 3 | 3 | 0 |
| customerService.js | 21 | 0 | 8 | 13 | 0 |
| customerServiceAgent.js | 7 | 0 | 1 | 6 | 0 |
| customerSession.js | 18 | 0 | 10 | 8 | 0 |
| customIntegration.js | 8 | 0 | 1 | 7 | 0 |
| customReport.js | 10 | 0 | 1 | 9 | 0 |
| dashboardScreen.js | 12 | 0 | 1 | 11 | 0 |
| dialogueMemory.js | 10 | 0 | 1 | 9 | 0 |
| dingtalkApp.js | 6 | 0 | 0 | 6 | 0 |
| domainPool.js | 12 | 0 | 6 | 6 | 0 |
| douyinCard.js | 9 | 0 | 9 | 0 | 0 |
| egolite.js | 39 | 0 | 28 | 2 | 9 |
| email.js | 17 | 0 | 6 | 11 | 0 |
| faq.js | 8 | 0 | 0 | 8 | 0 |
| featureFlags.js | 11 | 0 | 5 | 6 | 0 |
| feishu.js | 7 | 0 | 0 | 7 | 0 |
| followup.js | 5 | 0 | 0 | 5 | 0 |
| geo.js | 49 | 0 | 47 | 0 | 2 |
| geoAlert.js | 4 | 0 | 4 | 0 | 0 |
| geoAutopilot.js | 3 | 0 | 3 | 0 | 0 |
| geoDecision.js | 3 | 0 | 3 | 0 | 0 |
| geoEntity.js | 18 | 0 | 17 | 0 | 1 |
| geoPlatform.js | 6 | 0 | 6 | 0 | 0 |
| geoProbe.js | 18 | 0 | 17 | 0 | 1 |
| glossary.js | 6 | 0 | 3 | 3 | 0 |
| humanTask.js | 7 | 0 | 1 | 6 | 0 |
| i18nStats.js | 6 | 0 | 6 | 0 | 0 |
| inbox.js | 17 | 0 | 16 | 1 | 0 |
| integration.js | 13 | 0 | 11 | 2 | 0 |
| intentRecognition.js | 12 | 0 | 12 | 0 | 0 |
| kbConnector.js | 6 | 0 | 5 | 1 | 0 |
| kbCrawler.js | 14 | 0 | 14 | 0 | 0 |
| knowledge.js | 27 | 0 | 25 | 2 | 0 |
| knowledgeBase.js | 7 | 0 | 7 | 0 | 0 |
| knowledgeMerchant.js | 14 | 0 | 13 | 1 | 0 |
| kuaishouCard.js | 11 | 0 | 11 | 0 | 0 |
| leadMining.js | 2 | 0 | 2 | 0 | 0 |
| livecode.js | 12 | 0 | 12 | 0 | 0 |
| llmReport.js | 4 | 0 | 4 | 0 | 0 |
| llmRouting.js | 20 | 0 | 19 | 1 | 0 |
| ltcConfig.js | 2 | 0 | 2 | 0 | 0 |
| ltcRates.js | 1 | 0 | 1 | 0 | 0 |
| marketingFlow.js | 10 | 0 | 10 | 0 | 0 |
| material.js | 9 | 0 | 9 | 0 | 0 |
| mcpCredential.js | 4 | 0 | 4 | 0 | 0 |
| messageHub.js | 9 | 0 | 9 | 0 | 0 |
| monitor.js | 17 | 0 | 17 | 0 | 0 |
| notification.js | 4 | 0 | 4 | 0 | 0 |
| objection.js | 4 | 0 | 4 | 0 | 0 |
| obs.js | 8 | 0 | 8 | 0 | 0 |
| observabilityDashboard.js | 1 | 0 | 1 | 0 | 0 |
| oneid.js | 10 | 0 | 10 | 0 | 0 |
| operationLog.js | 5 | 0 | 4 | 1 | 0 |
| orderDraft.js | 6 | 0 | 5 | 1 | 0 |
| persona.js | 2 | 0 | 2 | 0 | 0 |
| qqBot.js | 7 | 0 | 6 | 1 | 0 |
| ragProductConfig.js | 8 | 0 | 8 | 0 | 0 |
| reachPipeline.js | 18 | 0 | 18 | 0 | 0 |
| runtimeConfig.js | 6 | 0 | 6 | 0 | 0 |
| salesWorkbench.js | 4 | 0 | 4 | 0 | 0 |
| scriptAb.js | 7 | 0 | 7 | 0 | 0 |
| scriptTemplate.js | 9 | 0 | 9 | 0 | 0 |
| securityAudit.js | 3 | 0 | 3 | 0 | 0 |
| shortLink.js | 10 | 0 | 9 | 1 | 0 |
| sms.js | 20 | 0 | 19 | 1 | 0 |
| sopAgent.js | 16 | 0 | 16 | 0 | 0 |
| sopTemplate.js | 7 | 0 | 7 | 0 | 0 |
| stats.js | 1 | 0 | 1 | 0 | 0 |
| tagSegmentation.js | 10 | 0 | 10 | 0 | 0 |
| telegram.js | 13 | 0 | 12 | 0 | 1 |
| templateMarket.js | 13 | 0 | 13 | 0 | 0 |
| tiktokCard.js | 8 | 0 | 8 | 0 | 0 |
| tuning.js | 16 | 0 | 15 | 1 | 0 |
| unifiedMessage.js | 2 | 0 | 2 | 0 | 0 |
| unknownQuestion.js | 8 | 0 | 8 | 0 | 0 |
| userSegment.js | 11 | 0 | 11 | 0 | 0 |
| wecomAccount.js | 24 | 0 | 24 | 0 | 0 |
| whatsapp.js | 14 | 0 | 14 | 0 | 0 |
| whatsappCloud.js | 6 | 0 | 5 | 1 | 0 |
| workflowOrchestrator.js | 14 | 0 | 14 | 0 | 0 |
| xianyuCard.js | 9 | 0 | 9 | 0 | 0 |
| xiaohongshuCard.js | 9 | 0 | 9 | 0 | 0 |
| **合计** | **1148** | **0** | **868** | **267** | **13** |

## 端点明细

### users.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | usersApi | list | GET | `/api/users` | `params` | - | params | JWT | 🔧 已修复 | L5 · off/on · 原忽略 keyword，前端搜索框失效 → 改走 SearchUsers |
| 2 | usersApi | get | GET | `/api/users/{id}` | `id` | - | - | JWT | ✅ 通过 | L9 · off/on |
| 3 | usersApi | create | POST | `/api/users` | `data` | data | - | JWT | 🔧 已修复 | L13 · 角色白名单只放 admin/user，staff/customer_service 被拒 → 对齐 model 常量 |
| 4 | usersApi | update | PUT | `/api/users/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | L17 · 同角色白名单问题，另修绑定层 |
| 5 | usersApi | delete | DELETE | `/api/users/{id}` | `id` | - | - | JWT | ✅ 通过 | L21 · off/on |
| 6 | usersApi | updatePassword | PUT | `/api/users/{id}/password` | `id, data` | data | - | JWT | ✅ 通过 | L25 · 字段名 `password`（非 new_password），重置后登录成功 |
| 7 | usersApi | login | POST | `/api/auth/login` | `data` | data | - | 公开 | ✅ 通过 | L29 · 错误口令 401 UNAUTHORIZED_2001 |
| 8 | usersApi | mfaVerify | POST | `/api/auth/mfa/verify` | `data` | data | - | 公开 | ✅ 通过 | L34 · 非法临时令牌 401「临时令牌无效」 |

### tenant.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | TenantApi | getInfo | GET | `/api/tenant/info` | - | - | - | JWT | ✅ 通过 | L8 · off 返回 tenant_id=1；on 返回 tenant_id=16 且跨租户不串 |
| 2 | TenantApi | isMultiTenant | GET | `/api/public/config` | - | undefined | - | 公开 | ✅ 通过 | L15 · off `tenant_mode:false`；on `true`+`edition:saas` |

### permission.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | PUT | `/api/system/permissions/{id}/enabled` | - | {enabled} | - | JWT | 🔧 已修复 | L4 · 功能正确（禁用后登录 401「用户已被禁用」，禁用自己 400）；修复错误文案泄漏 `: service: invalid input` |
| 2 | - | - | PUT | `/api/system/permissions/{id}/password` | - | {password} | - | JWT | ✅ 通过 | L8 · 重置后用新口令登录成功 |
| 3 | - | - | GET | `/api/system/permissions/audit-logs` | - | - | params | JWT | ✅ 通过 | L12 · 分页 + 审计字段完整 |

### role.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/system/roles` | - | - | - | JWT | ✅ 通过 | L10 · 返回 3 档角色 + member_count |
| 2 | - | - | GET | `/api/system/roles/{code}` | - | - | - | JWT | ✅ 通过 | L15 |
| 3 | - | - | GET | `/api/system/roles/{code}/members` | - | - | params | JWT | ✅ 通过 | L22 · 前端传 page/size，后端识别正常 |

### systemUser.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/system/users` | - | - | params | JWT | ✅ 通过 | L4 · 前端传 page/size，支持 keyword/role 筛选 |
| 2 | - | - | GET | `/api/system/users/{id}` | - | - | - | JWT | ✅ 通过 | L8 |
| 3 | - | - | POST | `/api/system/users` | - | data | - | JWT | 🔧 已修复 | L12 · 前端提交 name/phone/status，后端只认 name 且丢弃 phone/status → 补 real_name 别名+phone+status 同步 enabled |
| 4 | - | - | PUT | `/api/system/users/{id}` | - | data | - | JWT | 🔧 已修复 | L16 · 同上；另加"不能禁用自己""至少保留一个启用超管"守卫，禁用后登录 401 ✅ |
| 5 | - | - | DELETE | `/api/system/users/{id}` | - | - | - | JWT | ✅ 通过 | L20 |

### system.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | SystemApi | getInfo | GET | `/api/system/info` | `config` | config | - | JWT | ✅ 通过 | L6 · CPU/内存/磁盘/uptime/goroutine 齐全 |
| 2 | SystemApi | getConfig | GET | `/api/system/config` | - | - | - | JWT | ✅ 通过 | L9 · 按租户读取（tenant_id=1） |
| 3 | SystemApi | saveConfig | POST | `/api/system/config` | `data` | data | - | JWT | ✅ 通过 | L12 · 保存后 GET 与 /api/public/config 均同步 |

### menu.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/menus` | - | - | - | JWT | ✅ 通过 | L12 · 返回 {tree, all}，admin 全量 |
| 2 | - | - | GET | `/api/system/menus` | - | - | - | JWT | ✅ 通过 | L17 · 完整注册表 |
| 3 | - | - | GET | `/api/system/roles/{code}/menus` | - | - | - | JWT | ✅ 通过 | L22 |
| 4 | - | - | PUT | `/api/system/roles/{code}/menus` | - | {menu_keys} | - | JWT | ✅ 通过 | L27 · 覆盖保存生效（60→2→恢复60） |

### configParams.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/manage/config-params` | - | - | params | JWT | ✅ 通过 | L8 · 支持 group / include_deprecated |
| 2 | - | - | GET | `/api/manage/config-params/groups` | - | - | - | JWT | ✅ 通过 | L17 · 分组元数据（中文名/说明/排序） |
| 3 | - | - | GET | `/api/manage/config-params/audit-logs` | - | - | params | JWT | ✅ 通过 | L21 · update/reset/bulk_reset 均落审计 |
| 4 | - | - | PUT | `/api/manage/config-params/{group}/{key}` | - | {value} | - | JWT | ✅ 通过 | L25 · 类型校验生效；注意非法值走 HTTP 200 + 体内 `code:400`（本仓既有约定，前端按 code 判错） |
| 5 | - | - | POST | `/api/manage/config-params/{group}/{key}/reset` | - | - | - | JWT | ✅ 通过 | L29 · 恢复到 default_value |
| 6 | - | - | POST | `/api/manage/config-params/{group}/reset` | - | - | - | JWT | ✅ 通过 | L33 · 整组重置，19 条全部回到默认值，逐条留审计 |
| 7 | - | - | PUT | `/api/manage/platform-host` | - | {address} | - | JWT | ✅ 通过 | L47 · 写平台默认行，返回生效提示 |
| 8 | - | - | POST | `/api/manage/platform-host/connectivity` | - | {address} | - | JWT | ✅ 通过 | L56 · PLATFORM_ENABLED=false 时短路返回 `platform_enabled:false`/`not_configured`，语义正确 |

### moduleMarket.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listMarketModules | GET | `/api/module-market/list` | `params, config` | - | params | JWT | ✅ 通过 | L17 · 24 模块；off 全 owned / on 仅 browser-automation 未拥有 |
| 2 | - | marketModuleDetail | GET | `/api/module-market/detail/{expr}` | `key` | - | - | JWT | ✅ 通过 | L20 · 未知 key → 4302「模块不存在」 |
| 3 | - | paymentGuide | GET | `/api/module-market/payment-guide` | `config` | undefined | - | JWT | ✅ 通过 | L23 |
| 4 | - | regulatoryNotice | GET | `/api/tenant/regulatory-notice` | - | - | - | JWT | ✅ 通过 | L26 · 带协议版本号 |
| 5 | - | createModuleOrder | POST | `/api/module-market/order` | `data` | data | - | JWT | ✅ 通过 | L31 · PLATFORM_ENABLED=false → 4303「平台未连接」，符合预期（happy path 需 platform-server） |
| 6 | - | listModuleOrders | GET | `/api/module-market/orders` | `params, config` | - | params | JWT | ✅ 通过 | L36 · on 模式按租户隔离（租户16 见 0 条）；off 单租户不隔离为既定设计 |
| 7 | - | moduleOrderDetail | GET | `/api/module-market/orders/{expr}` | `orderNo` | - | - | JWT | ✅ 通过 | L39 · 不存在 → 4004；on 模式跨租户查询 → 4004 |
| 8 | - | orderPayStatus | GET | `/api/module-market/orders/{expr}/pay-status` | `orderNo, config` | undefined | - | JWT | ✅ 通过 | L43 · 平台未连接时 4303 |
| 9 | - | orderPayParams | POST | `/api/module-market/orders/{expr}/pay` | `orderNo` | - | - | JWT | ✅ 通过 | L48 · 同上 |
| 10 | - | subscriptionStatus | GET | `/api/subscription/status` | `config` | undefined | - | JWT | ✅ 通过 | L56 · trial 态 + stale 标记 |
| 11 | - | renewSubscription | POST | `/api/subscription/renew` | `data` | data | - | JWT | ✅ 通过 | L59 · 4303 守卫 |
| 12 | - | myModules | GET | `/api/modules/my` | - | - | - | JWT | ✅ 通过 | L64 |
| 13 | - | syncModules | POST | `/api/modules/sync` | `config` | undefined | - | JWT | ✅ 通过 | L67 · active_count 235、source=cache |
| 14 | - | moduleSyncLogs | GET | `/api/modules/sync-logs` | `params, config` | - | params | JWT | ✅ 通过 | L70 |
| 15 | - | llmWallet | GET | `/api/llm/wallet` | `config` | undefined | - | JWT | ✅ 通过 | L78 · platform_enabled=false 时返回 exists:false 而非报错 |
| 16 | - | llmPlans | GET | `/api/llm/plans` | `config` | undefined | - | JWT | ✅ 通过 | L81 · 空列表返回 {list:[],total:0} |
| 17 | - | createLLMOrder | POST | `/api/llm/recharge` | `data` | data | - | JWT | ✅ 通过 | L84 · 4303 守卫 |

### platform.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | platformAPI | getLatestMessage | GET | `/api/platform/message/latest` | - | {_silent} | - | JWT | ✅ 通过 | L5 · 平台未初始化时返回 code:0 + data:null（前端 `res?.data ?? res` 兼容） |
| 2 | platformAPI | markMessageRead | POST | `/api/platform/message/{messageId}/read` | `messageId` | - | - | JWT | ✅ 通过 | L9 · 未初始化时降级返回空数据，不报错 |

### platformAccount.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | platformAccountApi | getAccounts | GET | `/api/platform-accounts` | `params` | - | params | JWT | 🔧 已修复 | L5 · 原忽略 platform/account/status（页面靠本地过滤才看着正常）→ 后端补筛选；返回仍是裸数组 |
| 2 | platformAccountApi | getPlatforms | GET | `/api/platform-accounts/platforms` | - | - | - | JWT | ✅ 通过 | L9 |
| 3 | platformAccountApi | getAccountById | GET | `/api/platform-accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | L13 · 不存在 → 404 NOT_FOUND_1002 |
| 4 | platformAccountApi | createAccount | POST | `/api/platform-accounts` | `data` | data | - | JWT | ✅ 通过 | L17 |
| 5 | platformAccountApi | updateAccount | PUT | `/api/platform-accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | L21 |
| 6 | platformAccountApi | deleteAccount | DELETE | `/api/platform-accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | L25 |
| 7 | platformAccountApi | loginAccount | POST | `/api/platform-accounts/{id}/login` | `id, data` | data | - | JWT | 🔧 已修复 | L29 · 原返回 500 INTERNAL_ERROR_6002「登录平台账号失败」→ 改走 UnsupportedCapabilityError，400 + 可读原因 |
| 8 | platformAccountApi | checkStatus | GET | `/api/platform-accounts/{id}/status` | `id` | - | - | JWT | ✅ 通过 | L33 · 400 INVALID_PARAM_1001 + 明确原因 |

### abExperiment.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/ab-experiments` | - | - | params | JWT | ✅ 通过 | L4 · 分页返回 {list,total} |
| 2 | - | - | GET | `/api/ab-experiments/{id}` | - | - | - | JWT | ✅ 通过 | L7 · 不存在 → 404 NOT_FOUND_1002 |
| 3 | - | - | POST | `/api/ab-experiments` | - | data | - | JWT | 🔧 已修复 | L10 · created_by 恒为 0 → 控制器注入 user_id（实测 62） |
| 4 | - | - | PUT | `/api/ab-experiments/{id}` | - | data | - | JWT | ✅ 通过 | L13 · 不存在 → 404 |
| 5 | - | - | DELETE | `/api/ab-experiments/{id}` | - | - | - | JWT | ✅ 通过 | L16 · 删除后 GET 404；不存在 id → 404 |
| 6 | - | - | POST | `/api/ab-experiments/{id}/start` | - | - | - | JWT | ✅ 通过 | L19 · draft→running；stop 后可再 start（无状态机拦截，可接受） |
| 7 | - | - | POST | `/api/ab-experiments/{id}/pause` | - | - | - | JWT | ✅ 通过 | L22 · running→paused |
| 8 | - | - | POST | `/api/ab-experiments/{id}/stop` | - | - | - | JWT | ✅ 通过 | L25 · paused→stopped |
| 9 | - | - | GET | `/api/ab-experiments/{id}/results` | - | - | - | JWT | ✅ 通过 | L28 |
| 10 | - | - | GET | `/api/ab-experiments/{id}/conversion-events` | - | - | - | JWT | ✅ 通过 | L31 |
| 11 | - | - | GET | `/api/ab-experiments` | - | - | - | JWT | ✅ 通过 | L41 · 同上，参数透传正常 |

### abExperimentPlus.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getExperimentWithStats | GET | `/api/ab-experiments/{id}/stats` | `id, method` | - | params | JWT | 🔧 已修复 | L6 · 实验不存在时静默返回空统计 → 补存在性校验 404；非法 method 静默回落 → 400 |
| 2 | - | getExperimentDiagnostics | GET | `/api/ab-experiments/{id}/diagnostics` | `id` | - | - | JWT | 🔧 已修复 | L9 · 同上补 404 校验；返回 SRM/最小样本/多曝光诊断 |
| 3 | - | getExperimentWithCUPED | GET | `/api/ab-experiments/{id}/cuped` | `id` | - | - | JWT | 🔧 已修复 | L12 · 同上；无实验前数据时 available:false + 退化结果 |
| 4 | - | sequentialTest | POST | `/api/ab-experiments/{id}/sequential-test` | `id, alpha` | {alpha} | - | JWT | 🔧 已修复 | L15 · 同上；alpha 越界(≤0/≥1)回落 0.05，避免 NaN |
| 5 | - | bayesianTest | POST | `/api/ab-experiments/{id}/bayesian-test` | `id` | {} | - | JWT | 🔧 已修复 | L18 · 同上（存在性校验） |

### agentKBBinding.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/agent-kb-bindings` | - | data | - | JWT | 🔧 已修复 | L4 · 重复绑定覆盖 priority ✅；kb 不存在文案曾双前缀「知识库不存在: 知识库 999999 不存在」，且 404 被压成通用「记录不存在」→ 保留带 id 原因；参数校验不再泄漏 Go 结构体名 |
| 2 | - | - | DELETE | `/api/agent-kb-bindings/{agentId}/{kbId}` | - | - | - | JWT | 🔧 已修复 | L8 · 解绑成功；重复解绑原返回 `record not found` 英文原文 → 统一 404「记录不存在」 |
| 3 | - | - | GET | `/api/agent-kb-bindings/by-agent/{agentId}` | - | - | - | JWT | ✅ 通过 | L12 · off 返回绑定明细；on 跨租户查 53 → 空数组（隔离生效） |
| 4 | - | - | GET | `/api/agent-kb-bindings/by-kb/{kbId}` | - | - | - | JWT | ✅ 通过 | L16 · 未知 kb → 空数组 |
| 5 | - | - | PUT | `/api/agent-kb-bindings/by-agent/{agentId}` | - | {kb_ids} | - | JWT | 🔧 已修复 | L20 · replace 语义生效（5→6 只剩 6）；kb 不存在整体回滚 + 具体原因 |

### aiAgent.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/ai-agents` | - | - | params | JWT | ✅ 通过 | L4 分页{list,total}；**llm_provider_config.api_key 明文返回（当前库为空，待加固）** |
| 2 | - | - | GET | `/api/ai-agents-enabled` | - | - | - | JWT | ✅ 通过 | L8 只含 status=1，toggle 后即时生效 |
| 3 | - | - | GET | `/api/ai-agents/{id}` | - | - | - | JWT | ✅ 通过 | L12 含完整配置 |
| 4 | - | - | POST | `/api/ai-agents` | - | data | - | JWT | 🔧 已修复 | L16 缺 persona→400；**GORM default 标签吞零值（status=0/temperature=0 被写成 1/0.7），已改为创建后按需回写**；软删除残留 agent_code 撞唯一键时不再回传裸 SQL |
| 5 | - | - | PUT | `/api/ai-agents/{id}` | - | data | - | JWT | 🔧 已修复 | L20 局部更新（hasKey 语义）；temperature/status 改用指针入参，显式 0 生效 |
| 6 | - | - | DELETE | `/api/ai-agents/{id}` | - | - | - | JWT | ✅ 通过 | L24 软删除；重复删除/不存在→404 |
| 7 | - | - | POST | `/api/ai-agents/{id}/toggle` | - | {status} | - | JWT | ✅ 通过 | L28 0/1 往返正常 |
| 8 | - | - | POST | `/api/ai-agents/{id}/test` | - | data | - | JWT | 🔧 已修复 | L32 FAQ 快路径置信度 >1（1.0323）→ 新增 clamp01，现为 1 |
| 9 | - | - | GET | `/api/ai-agents/{id}/context` | - | - | - | JWT | ✅ 通过 | L36 返回 persona/知识库/FAQ 上下文 |

### aiProductivity.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | AIProductivityApi | getReport | GET | `/api/ai-productivity/overview` | `params` | - | params | JWT | ✅ 通过 | L5 时间区间用 `start_time/end_time`(RFC3339)，缺省近 30 天；on 模式租户隔离生效（tenant16 全 0） |
| 2 | AIProductivityApi | getTrend | GET | `/api/ai-productivity/trend` | `params` | - | params | JWT | 🔧 已修复 | L8 `days` 回显未归一化：days=0/-3/abc/200 时 trend 已是 30 天但响应 `days` 原样回显 → 改为与 service 同一归一化 |

### aiTool.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/ai-tools` | - | - | params | JWT | ✅ 通过 | L4 {list,total}，category/enabled/page_size 过滤正常 |
| 2 | - | - | GET | `/api/ai-tools/{name}` | - | - | - | JWT | ✅ 通过 | L8 不存在 → 404 NOT_FOUND_1002「工具不存在」 |
| 3 | - | - | PUT | `/api/ai-tools/{name}/status` | - | {is_enabled} | - | JWT | ✅ 通过 | L12 落库并即时反映到 GET；不存在 → 404 |
| 4 | - | - | POST | `/api/ai-tools/batch-status` | - | {tools,is_enabled} | - | JWT | ✅ 通过 | L16 空数组→400「工具列表不能为空」；全部不存在→404；**混合存在/不存在时静默成功（部分生效）** |
| 5 | - | - | GET | `/api/ai-tools/{toolName}/accounts` | - | - | - | JWT | ✅ 通过 | L20 返回裸数组；未知工具返回 `[]` |
| 6 | - | - | POST | `/api/ai-tools/{toolName}/accounts` | - | {account_type,account_id,is_primary} | - | JWT | 🔧 已修复 | L24 缺参→400；**原先可给不存在的工具建孤儿绑定并返回 success**，已加工具存在性校验 → 404「工具不存在」 |
| 7 | - | - | DELETE | `/api/ai-tools/{toolName}/accounts/{accountType}/{accountId}` | - | - | - | JWT | ✅ 通过 | L32 解绑成功；重复解绑 → 404「绑定不存在」 |

### approval.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | approvalApi | get | GET | `/api/approvals/{id}` | `id` | - | - | JWT | ✅ 通过 | L18 返回 allowed_transitions + decidable_by_human；不存在→404「审批不存在(id=…)」；**默认本地未开 `FF_LTC_APPROVAL_RESUME` 时整组回 503（设计如此）**，本回合以 `on` 启验 |
| 2 | approvalApi | decide | POST | `/api/approvals/{id}/decide` | `id, verdict, note` | {verdict,note} | - | JWT | ✅ 通过 | L27 approved 落 decided_by/note；重复裁决→409 DUPLICATE_ENTRY_3003（带最新行）；verdict=expired/bogus→400；`allowed_transitions` 含 expired 属状态机事实，人不可点（decidable_by_human 区分） |

### assetBundle.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | createBundle | POST | `/api/asset-bundle` | `data` | data | - | JWT | 🔧 已修复 | 校验失败原报 500+英文；改 bizerr 4xxx → 400 中文；重复 asset_id 提示「已存在，请更换后重试」（L4）|
| 2 | - | updateBundle | PUT | `/api/asset-bundle/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | 原 DTO 强制 body 回传 id（binding:required），REST 语义错误；改为路由参数注入（L7）|
| 3 | - | getBundle | GET | `/api/asset-bundle/{id}` | `id` | - | - | JWT | 🔧 已修复 | 不存在原回 404「record not found」→ 404「资产包不存在（id=）」（L10）|
| 4 | - | getBundleByAssetID | GET | `/api/asset-bundle/by-aid/{aid}` | `aid` | - | - | JWT | 🔧 已修复 | 同上，文案带 asset_id；跨租户（on）不可见（L13）|
| 5 | - | listBundles | POST | `/api/asset-bundle/list` | `data` | data | - | JWT | 🔧 已修复 | tags 过滤原展开成非法数组字面量 → SQLSTATE 22P02 500，改 pq.StringArray；page/size 回显原样（0/0）→ 归一化 1/20（L16）|
| 6 | - | publishBundle | POST | `/api/asset-bundle/{id}/publish` | `id` | - | - | JWT | ✅ 通过 | status→active，落库（L19）|
| 7 | - | submitToPlatform | POST | `/api/asset-bundle/{id}/submit-platform` | `id` | - | - | JWT | ✅ 通过 | PLATFORM_ENABLED=false → 403「平台集成未启用」，守卫正确（L22）|
| 8 | - | archiveBundle | POST | `/api/asset-bundle/{id}/archive` | `id` | - | - | JWT | ✅ 通过 | status→archived（L25）|
| 9 | - | deleteBundle | DELETE | `/api/asset-bundle/{id}` | `id` | - | - | JWT | 🔧 已修复 | 软删成功后 GET 404；不存在 id 文案统一为「资产包不存在」（L28）|
| 10 | - | weaveBundle | POST | `/api/asset-bundle/weave` | `data` | data | - | JWT | ✅ 通过 | 商户参数注入+RAG+历史+当前问，stats 正确（L31）|
| 11 | - | merchantSave | POST | `/api/asset-bundle/merchant-save` | `data` | data | - | JWT | 🔧 已修复 | qa_cards 只填 trigger 时原被静默丢弃；用 trigger 兜底并写回 JSON（L34）|
| 12 | - | merchantParse | POST | `/api/asset-bundle/merchant-parse/{aid}` | `aid` | - | - | JWT | 🔧 已修复 | round-trip 丢失 trigger；解析内联 JSON 回填；404 文案中文（L37）|
| 13 | - | enableBundle | POST | `/api/asset-bundle/{id}/enable` | `id` | - | - | JWT | 🔧 已修复 | 热启用写 Redis 索引；enabled/list 立即可见；404 文案中文（L40）|
| 14 | - | disableBundle | POST | `/api/asset-bundle/{id}/disable` | `id` | - | - | JWT | 🔧 已修复 | 热禁用即从 enabled/list 消失（L43）|
| 15 | - | listEnabledBundles | POST | `/api/asset-bundle/enabled/list` | - | - | - | JWT | ✅ 通过 | 返回 {list,total}（L46）|
| 16 | - | uploadCover | POST | `/api/upload` | `file` | fd | - | JWT | ✅ 通过 | multipart file；png 200 返回 /files/...；.exe 400 不支持类型（L51）|

### assetMarket.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listAssets | GET | `/api/v1/asset-market/list` | `params` | - | params | JWT | ✅ 通过 | 平台未接入时回落空列表 {list:[],total:0}，不报错（L4） |
| 2 | - | assetDetail | GET | `/api/v1/asset-market/detail/{id}` | `id` | - | - | JWT | 🔧 已修复 | 不存在/平台未接入 → 6001「资产不存在或不可用」（原 5000 内部错误）；有本地同 asset_id 时回落本地（L7） |
| 3 | - | purchaseAsset | POST | `/api/v1/asset-market/purchase` | `data` | data | - | JWT | ✅ 通过 | PLATFORM_ENABLED=false → 403 FORBIDDEN_2002 明确引导；happy path 需 platform-server（L10） |
| 4 | - | syncAsset | POST | `/api/v1/asset-market/sync` | `data` | data | - | JWT | ✅ 通过 | 同上 403 守卫（L13） |
| 5 | - | reportUsage | POST | `/api/v1/asset-market/report-usage` | `data` | data | - | JWT | ✅ 通过 | 同上 403 守卫（L16） |
| 6 | - | listLocalAssets | GET | `/api/v1/local-assets` | `params` | - | params | JWT | ✅ 通过 | {list,total}；page/size 归一化（size≤100）；asset_type/industry/source/keyword 过滤生效（L19） |
| 7 | - | getLocalAsset | GET | `/api/v1/local-assets/{id}` | `id` | - | - | JWT | 🔧 已修复 | 返回 {asset,data}；不存在 → 6001；非数字 id → 4000「无效的资产 ID」（原报「资产不存在」）（L22） |
| 8 | - | createLocalAsset | POST | `/api/v1/local-assets` | `data` | data | - | JWT | 🔧 已修复 | 自动生成 asset_id=manual-uuid；asset_type/industry 校验 4000；缺字段 → 6003 且带具体原因（原只回「资产 JSON 校验失败」）（L25） |
| 9 | - | updateLocalAsset | PUT | `/api/v1/local-assets/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | 补 industry 校验（原可写入任意行业）；校验失败带原因（L28） |
| 10 | - | deleteLocalAsset | DELETE | `/api/v1/local-assets/{id}` | `id` | - | - | JWT | ✅ 通过 | 软删；重复删 → 6001（L31） |
| 11 | - | toggleLocalAsset | PUT | `/api/v1/local-assets/{id}/toggle-active` | `id, active` | {active} | - | JWT | ✅ 通过 | {active} 落 is_active；无 body → 400；不存在 → 6001（L34） |
| 12 | - | syncLog | GET | `/api/v1/local-assets/sync-log` | `params` | - | params | JWT | ✅ 通过 | 裸数组；asset_id/limit 过滤（L37） |

### backup.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/backups` | - | - | params | JWT | ✅ 通过 | {list,total,page,page_size}；page_size 默认 10、上限 1000（L4） |
| 2 | - | - | GET | `/api/backups/{id}` | - | - | - | JWT | 🔧 已修复 | 不存在 → 404「备份不存在」（原回英文 record not found）；非数字 ID 400（L8） |
| 3 | - | - | POST | `/api/backups` | - | data | - | JWT | 🔧 已修复 | 建任务→后台异步打包（实测 completed，file_size 25035，created_by=62）；缺参 400「backup_name 必填」（原泄漏 Go 结构体名）；`../etc/passwd` → 400 非法字符（L12） |
| 4 | - | - | DELETE | `/api/backups/{id}` | - | - | - | JWT | ✅ 通过 | 删除记录 + 文件；重复删 → 404（L16） |
| 5 | - | - | POST | `/api/restore` | - | data | - | JWT | 🔧 已修复 | 自建备份 178 恢复成功（status completed）；缺参 400「backup_id 必填」；备份不存在 → 404「备份不存在，无法恢复」（L20） |
| 6 | - | - | GET | `/api/restore/list` | - | - | params | JWT | ✅ 通过 | {list,total,page,page_size}（L24） |
| 7 | - | - | GET | `/api/restore/last` | - | - | - | JWT | ✅ 通过 | 无记录时 code=0 + data:null；有记录返回最近一条（L28） |

### badCase.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | badCaseApi | list | GET | `/api/bad-cases` | `params` | - | params | JWT | 🔧 已修复 | 重复键多值过滤（source=zero_hit&source=manual）生效；不给 status 默认只列 pending；page=0 → 400；未知 label/fix_layer → 400，且已去掉「bad_case: 查询条件非法:」哨兵前缀 |
| 2 | badCaseApi | stats | GET | `/api/bad-cases/stats` | - | - | - | JWT | ✅ 通过 | by_status/by_fix_layer/labeled_ratio；on(8206) 只统计本租户（全 0） |
| 3 | badCaseApi | taxonomy | GET | `/api/bad-cases/taxonomy` | - | - | - | JWT | ✅ 通过 | sources/statuses/labels/fix_layers + label_layer 映射，全来自 model 常量 |
| 4 | badCaseApi | get | GET | `/api/bad-cases/{id}` | `id` | - | - | JWT | ✅ 通过 | 返回完整行（含 label/label_note/labeler_id/fix_layer）；不存在 → 404「坏例记录不存在（id=…)」 |
| 5 | badCaseApi | create | POST | `/api/bad-cases` | `input` | input | - | JWT | 🔧 已修复 | 手录 pending/manual、dedup_key 用自身 id（同 session 可补多条）；缺 query/answer → 400，文案已去哨兵前缀 |
| 6 | badCaseApi | label | POST | `/api/bad-cases/{id}/label` | `id, label, note` | {label,note} | - | JWT | 🔧 已修复 | kb_missing → labeled + fix_layer=knowledge、labeler_id=62；重复打标 409；非法类目/缺 note → 400（文案已清洗） |
| 7 | badCaseApi | dismiss | POST | `/api/bad-cases/{id}/dismiss` | `id, reason` | {reason} | - | JWT | 🔧 已修复 | 缺理由 400「撤销必须写明理由」；已导出/已裁决再撤 → 409「当前状态不允许该操作」 |
| 8 | badCaseApi | exportSet | POST | `/api/bad-cases/export` | `labels, limit` | {labels,limit} | - | JWT | 🔧 已修复 | 已打标行转 exported 并回 {eval_set_id,count,rows}；队列空 → 409「没有可导出的已打标样本」；limit<0 → 400 |

### batchOperation.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | resolveClueIds | GET | `/api/clues/list` | `data` | - | params | JWT | ✅ 通过 | 分页/过滤正常（resolveClueIds 依赖），total 221、limit 生效 | L83 |
| 2 | - | - | POST | `/api/batch/import` | - | fd | - | JWT | 🔧 已修复 | 缺文件文案改为「请选择要导入的文件」；type 非法 → 400（原 500）；导入完成写操作历史 | L112 |
| 3 | - | - | GET | `/api/batch/template` | - | - | params | JWT | 🔧 已修复 | format 只允许 csv（原 format=md 返回 CSV 内容却叫 .md）；type 非法 → 400 | L118 |
| 4 | - | - | POST | `/api/batch/delete` | - | {ids} | - | JWT | 🔧 已修复 | 绑定失败文案不再泄漏结构体名；一条未删 → 400（原「批量删除成功」）；写操作历史 | L131 |
| 5 | - | - | POST | `/api/batch/update` | - | {ids,fields} | - | JWT | 🔧 已修复 | 绑定失败文案清洗；字段全无效 → 400「无可更新字段」；一条未更新 → 400；写操作历史 | L156 |
| 6 | - | - | GET | `/api/batch/tools` | - | - | - | JWT | ✅ 通过 | 返回 4 个工具 import/export/delete/update | L163 |
| 7 | - | - | GET | `/api/batch/histories` | - | - | params | JWT | 🔧 已修复 | **历史表此前从未写入**（四个写端点都不落库）→ 前端「操作历史」恒空；已在四个写端点完成后写入 | L208 |
| 8 | - | - | POST | `/api/batch/histories/{id}/cancel` | - | - | - | JWT | 🔧 已修复 | 记录不存在 → 404「操作记录不存在」；状态不允许 → 409「当前状态为 completed…」（原 404） | L211 |
| 9 | - | - | GET | `/api/batch/histories/{id}` | - | - | - | JWT | 🔧 已修复 | 不存在 → 404「操作记录不存在」；有历史后可正常取详情 | L214 |
| 10 | - | - | POST | `/api/batch/preview` | - | {operation_type,data_type,ids} | - | JWT | 🔧 已修复 | 绑定失败文案清洗（原泄漏 PreviewRequest.OperationType）；非法操作/数据类型 → 400 | L223 |

### bridgeExtension.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getBridgeExtensionPackage | GET | `/api/bridge/extension/package` | - | undefined | - | JWT | ✅ 通过 | 平台关闭时按契约回 404 NOT_FOUND_1002「未接入平台或平台尚未上架」；off/on 双形态一致；200 分支需 platform-server | L11 |

### bridgeToken.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getBridgeTokenStatus | GET | `/api/bridge/token/status` | - | - | - | JWT | ✅ 通过 | 返回 {main_configured,prev_configured,source:"db"}；RequireAdminMiddleware；off/on 均 200 | L3 |
| 2 | - | resetBridgeToken | POST | `/api/bridge/token/reset` | - | - | - | JWT | ✅ 通过 | 返回新 token + 中文提示「旧凭据进入灰度窗口」；main/prev 均 configured（PREV 灰度生效）；已实测轮换后可继续用新 token | L5 |

### browserAutomation.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listBrowserTasks | GET | `/api/browser-automation/tasks` | `params` | - | params | JWT | ✅ 通过 | {list,total}；status/task_type/page/limit 过滤；limit 上限 200 | L6 |
| 2 | - | getBrowserTask | GET | `/api/browser-automation/tasks/{id}` | `id` | - | - | JWT | ✅ 通过 | 不存在 → 404「任务不存在」 | L9 |
| 3 | - | createBrowserTask | POST | `/api/browser-automation/tasks` | `data` | data | - | JWT | ✅ 通过 | steps 逐元素校验（dive）；缺 platform 服务层拒绝；成功 status=draft | L12 |
| 4 | - | updateBrowserTask | PUT | `/api/browser-automation/tasks/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 局部更新；platform 非法 → 400 | L15 |
| 5 | - | deleteBrowserTask | DELETE | `/api/browser-automation/tasks/{id}` | `id` | - | - | JWT | ✅ 通过 | 重复删 → 404 | L18 |
| 6 | - | publishBrowserTask | POST | `/api/browser-automation/tasks/{id}/publish` | `id` | - | - | JWT | ✅ 通过 | draft→ready | L21 |
| 7 | - | runBrowserTask | POST | `/api/browser-automation/tasks/{id}/run` | `id` | {} | - | JWT | ✅ 通过 | Host 离线 → 409 BROWSER_HOST_OFFLINE_8001 + 可操作中文指引 | L27 |
| 8 | - | pauseBrowserTask | POST | `/api/browser-automation/tasks/{id}/pause` | `id` | - | - | JWT | ✅ 通过 | 状态不符 → 409 BROWSER_STATE_CONFLICT_8003「当前状态: ready」 | L30 |
| 9 | - | resumeBrowserTask | POST | `/api/browser-automation/tasks/{id}/resume` | `id` | - | - | JWT | ✅ 通过 | 同上，非 paused → 409 | L33 |
| 10 | - | archiveBrowserTask | POST | `/api/browser-automation/tasks/{id}/archive` | `id` | - | - | JWT | ✅ 通过 | → archived | L36 |
| 11 | - | setBrowserTaskDependency | PUT | `/api/browser-automation/tasks/{id}/dependency` | `id, data` | data | - | JWT | 🔧 已修复 | 自依赖 → 400「任务不能依赖自身」；**修复：绑定错误回显 Go 字段名 DependsOnMode（匿名结构体无结构体前缀时 apiFieldPath 丢掉唯一 token）→ bind_err.go 改为单 token 不丢，实测回 depends_on_mode** | L39 |
| 12 | - | listBrowserSessions | GET | `/api/browser-automation/sessions` | `params` | - | params | JWT | ✅ 通过 | {list,total} | L44 |
| 13 | - | getBrowserSession | GET | `/api/browser-automation/sessions/{id}` | `id` | - | - | JWT | ✅ 通过 | 不存在 → 404「会话不存在」 | L47 |
| 14 | - | getBrowserSessionSteps | GET | `/api/browser-automation/sessions/{id}/steps` | `id` | - | - | JWT | ✅ 通过 | 会话不存在 → 404 | L50 |
| 15 | - | listBrowserTaskSessions | GET | `/api/browser-automation/tasks/{taskId}/sessions` | `taskId, params` | - | params | JWT | ✅ 通过 | {list,total} | L54 |
| 16 | - | stopBrowserSession | POST | `/api/browser-automation/sessions/{id}/stop` | `id, reason` | {reason} | - | JWT | ✅ 通过 | 不存在 → 404 | L57 |
| 17 | - | getBrowserConfirmGate | GET | `/api/browser-automation/sessions/{id}/confirm-gate` | `id` | - | - | JWT | ✅ 通过 | 不存在 → 404 | L62 |
| 18 | - | confirmBrowserSession | POST | `/api/browser-automation/sessions/{id}/confirm` | `id, payloadHash` | {payload_hash} | - | JWT | ✅ 通过 | 缺 hash → 400「放行必须指明批准的是哪份载荷」 | L72 |
| 19 | - | getBrowserSessionLogs | GET | `/api/browser-automation/sessions/{id}/logs` | `id, direction` | - | direction | JWT | ✅ 通过 | direction 白名单 command/event/judge，非法 → 400 | L93 |
| 20 | - | exportBrowserSessionAudit | GET | `/api/browser-automation/sessions/{id}/export` | `id` | - | - | JWT | ✅ 通过 | 会话不存在 → 404 | L97 |
| 21 | - | listBrowserTaskReceipts | GET | `/api/browser-automation/tasks/{taskId}/receipts` | `taskId` | - | - | JWT | ✅ 通过 | 返回 {list,total}；任务不存在 → 404「任务不存在」 | L102 |
| 22 | - | listBrowserCron | GET | `/api/browser-automation/cron` | - | - | - | JWT | ✅ 通过 | {list,total}（裸列表按用户） | L107 |
| 23 | - | createBrowserCron | POST | `/api/browser-automation/cron` | `data` | data | - | JWT | ✅ 通过 | 非 cron 任务 → 400「仅 cron 类型任务可配置定时触发器」；表达式/时区非法 → 400 中文前缀；任务不存在 → 404 | L110 |
| 24 | - | updateBrowserCron | PUT | `/api/browser-automation/cron/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | **修复：PUT 局部更新原无条件覆盖 time_zone（只改表达式会把 Asia/Shanghai 抹成空 → 服务器本地时区），改为空值即「不动」并校验合并后的组合；实测 expr-only / tz-only / 两者都发均正确，next_run_at 随 tz 换算** | L113 |
| 25 | - | deleteBrowserCron | DELETE | `/api/browser-automation/cron/{id}` | `id` | - | - | JWT | ✅ 通过 | 不存在 → 404「触发器不存在」 | L116 |
| 26 | - | enableBrowserCron | POST | `/api/browser-automation/cron/{id}/enable` | `id` | - | - | JWT | ✅ 通过 | 200「已启用」 | L119 |
| 27 | - | disableBrowserCron | POST | `/api/browser-automation/cron/{id}/disable` | `id` | - | - | JWT | ✅ 通过 | 200「已停用」 | L122 |
| 28 | - | getBrowserHostStatus | GET | `/api/browser-automation/host/status` | - | - | - | JWT | ✅ 通过 | admin 看全量 {count,hosts[]}，普通用户只看自己；online/servable/version/pid | L127 |
| 29 | - | resetBrowserHostToken | POST | `/api/browser-automation/host/token/reset` | - | - | - | JWT | ✅ 通过 | AdminAuthMiddleware；返回新 token + 「旧 token 仍可用，直到下一次重置」 | L130 |
| 30 | - | listPlatforms | GET | `/api/browser-automation/platforms` | - | - | - | JWT | ✅ 通过 | L3 注册表 3 个平台（douyin/xianyu/xiaohongshu）+ capabilities | L135 |
| 31 | - | getPlatformLocators | GET | `/api/browser-automation/platforms/{id}/locators` | `id` | - | - | JWT | ✅ 通过 | 返回选择器表；未知平台 → 404「平台 nope 未注册（可用: [...]）」 | L138 |

### bulkMessaging.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | getTemplates | - | GET | `/api/whatsapp/templates` | - | - | category/is_active | JWT | 🔧 已修复 | 返回 `{data:[…]}`；is_active 非法值静默忽略；on 未订阅 im-intl → 4301 | L4 |
| 2 | createTemplate | - | POST | `/api/whatsapp/templates` | - | data | - | JWT+Admin | 🔧 已修复 | 兼容前端 camelCase `isActive`；未传默认启用、显式 false 可停用（原 GORM default:true 吞零值）；空名/空内容 → 400 | L8 |
| 3 | updateTemplate | - | PUT | `/api/whatsapp/templates/{id}` | `id` | data | - | JWT+Admin | 🔧 已修复 | 兼容 `isActive`；is_active 按 key 存在与否决定是否改；不存在 → 404「模板不存在」 | L12 |
| 4 | deleteTemplate | - | DELETE | `/api/whatsapp/templates/{id}` | `id` | - | - | JWT+Admin | 🔧 已修复 | 硬删除；重复删 → 404「模板不存在」（原为通用「记录不存在」） | L16 |
| 5 | sendBulkMessage | - | POST | `/api/whatsapp/group-messaging/send` | - | data | - | JWT+Admin | 🔧 已修复 | 兼容 camelCase templateId/leadIds/scheduleAt；按选中 ID 精确取线索（原按 type=7 过滤，其它来源选中项被静默丢弃 → count=0 仍报成功）；零目标 → 400；返回 `{queue_id,count,skipped}`；on 未订阅 → 4301（新增门禁） | L20 |
| 6 | getMessageStatus | - | GET | `/api/whatsapp/group-messaging/status/{queueId}` | `queueId` | - | - | JWT | 🔧 已修复 | 内存态补 queue_id（原恒空）、DB 回源补 created/updated（原零值）；未订阅 im-intl → 4301 | L24 |
| 7 | getSendRecords | - | GET | `/api/whatsapp/group-messaging/records` | - | - | page/limit | JWT | 🔧 已修复 | `{data,total,page,limit}`；createdAt/updatedAt 原恒为 0001-01-01，现透传队列时间戳；templateName 固定「批量消息」 | L28 |

### capabilityDiagnostics.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | capabilityDiagnosticsApi | get | GET | `/api/capabilities` | - | - | - | JWT | ✅ 通过 | 返回 `{capabilities,total,ready_count}` 运行视图（22 条）；off 模式含 ready 项，on 租户 16 全为 not_configured_no_account（按租户账号判定，隔离生效） | L12 |

### channelAgentBinding.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/channel-agent-bindings` | - | - | channel_type/account_id（均必填） | JWT | 🔧 已修复 | 返回 `{list,total}`；缺参 400「参数缺失」；原把 err.Error() 塞进 data，改为统一错误出口 |
| 2 | - | - | GET | `/api/channel-agent-bindings/by-agent/{agentId}` | - | - | - | JWT | ✅ 通过 | 反查智能体被哪些渠道使用；`abc` → 400「无效的智能体ID」 |
| 3 | - | - | POST | `/api/channel-agent-bindings` | - | data | - | JWT | 🔧 已修复 | 原：不存在的 agent 报「智能体不存在: record not found」、重复绑定回 PG 原文 23505、缺参回 gin 结构体名。现：400「智能体 N 不存在/已停用，无法绑定」、409「渠道账号 x/y 已存在相同绑定…」、400 中文必填提示 |
| 4 | - | - | PUT | `/api/channel-agent-bindings/{id}` | - | data | - | JWT | 🔧 已修复 | 原换绑不校验目标智能体（可绑到 999999）→ 现仅在 agent_id 变化时校验存在且启用；不存在 id → 404「绑定不存在」 |
| 5 | - | - | DELETE | `/api/channel-agent-bindings/{id}` | - | - | - | JWT | 🔧 已修复 | 删除成功 200；重复删 → 404「绑定不存在」（原回「删除失败」） |

### chat.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/chat/public/upload-token` | - | - | params | JWT | 🔧 | L4 |

### chatChannel.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/chat-channels` | - | - | params | JWT | ✅ | L4 |
| 2 | - | - | GET | `/api/chat-channels/{channelId}` | - | - | - | JWT | ✅ | L8 |
| 3 | - | - | POST | `/api/chat-channels` | - | data | - | JWT | 🔧 | L12 |
| 4 | - | - | PUT | `/api/chat-channels/{channelId}` | - | data | - | JWT | 🔧 | L16 |
| 5 | - | - | DELETE | `/api/chat-channels/{channelId}` | - | - | - | JWT | 🔧 | L20 |
| 6 | - | - | POST | `/api/chat-channels/{channelId}/rotate-key` | - | - | - | JWT | 🔧 | L24 |
| 7 | - | - | POST | `/api/chat-channels/{channelId}/reset-secret` | - | - | - | JWT | 🔧 | L28 |

### chatPublic.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | openSession | POST | `/api/chat/public/sessions` | `data, channelId, visitorId` | data | - | 访客头 | ✅ 通过 | resume:true 续接同一访客会话；返回 {session,is_new_session,online_agent_num,welcome_message,visitor_token}；落库 customer_sessions |
| 2 | - | getActiveSession | GET | `/api/chat/public/sessions/active` | `channelId, visitorId` | - | - | 访客头 | ✅ 通过 | 返回该访客当前活跃会话全量行 |
| 3 | - | getRecentClosedSessions | GET | `/api/chat/public/sessions/recent-closed` | `channelId, visitorId, limit` | - | limit | 访客头 | ✅ 通过 | {list,total}；关闭后回归 total=1 一致 |
| 4 | - | getMessages | GET | `/api/chat/public/sessions/{sessionId}/messages` | `sessionId, page, pageSize, …` | - | page/page_size | 访客token | ✅ 通过 | {list,total}；无 token → 403（IDOR 防护生效） |
| 5 | - | getOfflineMessages | GET | `/api/chat/public/sessions/{sessionId}/offline-messages` | `sessionId, …` | - | - | 访客token | ✅ 通过 | {list,total}；先拉过 messages 已读后离线包为空（顺序语义非 bug） |
| 6 | - | sendMessage | POST | `/api/chat/public/sessions/{sessionId}/messages` | `sessionId, body, …` | body | - | 访客token | ✅ 通过 | {user_message} 落库 session_messages；AI 自动回复使首包 >8s（LLM 推理） |
| 7 | - | requestHumanTransfer | POST | `/api/chat/public/sessions/{sessionId}/transfer` | `sessionId, reason, …` | {reason} | - | 访客token | ✅ 通过 | 「已为您转接人工客服」 |
| 8 | - | closeSession | POST | `/api/chat/public/sessions/{sessionId}/close` | `sessionId, …` | {} | - | 访客token | ✅ 通过 | 落库 status=closed |
| 9 | - | rateSession | POST | `/api/chat/public/sessions/{sessionId}/rate` | `sessionId, rating, comment, …` | {rating,comment} | - | 访客token | ✅ 通过 | 落库 rating=5 |
| 10 | - | exchangeSessionToken | POST | `/api/chat/public/sessions/{sessionId}/token` | `sessionId, channelId, visitorId` | {} | - | 访客头 | ✅ 通过 | 换回会话自己的 visitor_token（旧 token 失效语义） |
| 11 | - | getAvailableAgents | GET | `/api/chat/public/agents/available` | `channelId` | - | - | 访客头 | ✅ 通过 | {available:6} |
| 12 | - | getUploadToken | GET | `/api/chat/public/upload-token` | `params, channelId, visitorId, visitorToken, sessionId` | - | session_id/visitor_id | 访客token | ✅ 通过 | QINIU_BUCKET 空 → 6003 SERVICE_UNAVAILABLE「对象存储未配置」（正确降级，非缺陷） |

### churnPrediction.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/churn/prediction` | - | - | params | JWT | ✅ 通过 | L4 缺 user_id→400；未知用户按低风险 0 分兜底（既定语义） |
| 2 | - | - | GET | `/api/churn-prediction` | - | - | params | JWT | 🔧 已修复 | L7 分页回显未归一化（page=0/size=999 实际 1/200 却回显 0/999） |
| 3 | - | - | GET | `/api/churn-prediction/users` | - | - | params | JWT | ✅ 通过 | L10 limit 生效，只回 high/critical |
| 4 | - | - | GET | `/api/churn-prediction/warnings` | - | - | params | JWT | 🔧 已修复 | L13 同 #2 分页回显 |
| 5 | - | - | GET | `/api/churn/unhandled-warnings` | - | - | params | JWT | 🔧 已修复 | L16 同 #2，且 is_handled=false 过滤生效 |
| 6 | - | - | POST | `/api/churn/warnings/{id}/handle` | - | data | - | JWT | 🔧 已修复 | L19 空 body 曾回「请求参数错误：EOF」；不存在 id 曾回「记录不存在」无 id |
| 7 | - | - | GET | `/api/churn-prediction/model-config` | - | - | - | JWT | ✅ 通过 | L22 无配置时回落默认值（tenant_id/id=0） |
| 8 | - | - | POST | `/api/churn-prediction/model-config` | - | data | - | JWT | 🔧 已修复 | L25 全 0/越界/临界<高阈曾静默入库（线上就有一行全 0）；保存回显曾把 tenant_id/时间戳写成零值 |
| 9 | - | - | GET | `/api/churn-prediction/statistics` | - | - | params | JWT | ✅ 通过 | L28 缺 start_date/end_date→400 |
| 10 | - | - | GET | `/api/churn/risk-distribution` | - | - | - | JWT | ✅ 通过 | L31 low/medium/high/critical 计数 |
| 11 | - | - | POST | `/api/churn/warnings/intervene` | - | data | - | JWT | 🔧 已修复 | L38 预测列表只有 user_id，前端却按 warning_id 传→恒 400；现支持 user_id 取最新未处理预警 |
| 12 | - | - | POST | `/api/user-segment/rfm/calculate` | - | data | - | JWT | 🔧 已修复 | L50 前端「运行流失预测」带 {"type":"churn"} 被忽略，只重算 RFM；现按 type 追加流失计算 |

### clue.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | clueApi | delete | DELETE | `/api/clues/delete/{id}` | `id` | - | - | JWT | 🔧 已修复 | L5 删除不存在线索原回笼统「删除线索失败」，现 404「线索不存在（id=…）」；0 行删除不假成功 |
| 2 | clueApi | list | GET | `/api/clues/list?{q}` | `page, limit, filters` | - | - | JWT | 🔧 已修复 | L12 page/limit/type/is_verify/start/end 全下推 DB，与 total 同口径；参数错误文案已具体化（page=abc → 400） |
| 3 | clueApi | statistics | GET | `/api/clues/statistics` | - | - | - | JWT | 🔧 已修复 | L15 按 type 分组回 total/verify_total；无数据时原回 `null`（前端按数组消费）→ now `[]` |
| 4 | clueApi | import | POST | `/api/clues/import` | `data` | data | - | JWT | 🔧 已修复 | L18 原接受未知 type（999）写脏数据、空数组静默成功、非数组回笼统「参数错误」、前端读 camelCase 得 undefined → 全部修复（含 5000 条上限） |

### cockpit.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/system/stats` | - | - | - | JWT | 🔧 已修复 | L4 实例级指标（CPU/磁盘/总用户数），后端按平台运营方档守卫：off 正常；on 租户管理员 403「仅平台运营方可执行」，而能力表把该路径挂在 opsOverview（admin）菜单 → 前端 OpsOverview 用 `Promise.all` 导致一条 403 整页停在「加载中」→ 改 `allSettled` + 403 降级为「仅平台运营可见」 |
| 2 | - | - | GET | `/api/monitor/health` | - | - | - | JWT | ✅ 通过 | L8 登录态可见；off 全量（total_traces 3712/abnormal 330），on 租户 16 全 0（隔离生效） |
| 3 | - | - | GET | `/api/monitor/node-health` | - | - | - | JWT | ✅ 通过 | L12 返回 `{nodes:[], window:"24h0m0s"}`（24h 内无可统计节点，空数组非缺陷） |
| 4 | - | - | GET | `/api/monitor/anomalies` | - | {limit} | - | JWT | 🔧 已修复 | L16 原完全忽略 `limit`（前端传 20 仍回全量 72 条），且项结构只有 `{conversation_id,channel,age_min}` → 前端「最近操作」表时间/用户两列恒 `-`。后端新增 `AnomalyGroups.Trim`（默认 50、上限 200、每组分别截断）；前端按 `age_min` 反推「N 分钟前」、用户列回落 conversation_id |
| 5 | - | - | GET | `/api/ai/sales-cockpit` | - | - | - | JWT | ✅ 通过 | L24 返回 react/sop/rag/reach/llmRoutes/channelHealth/intentDistribution/topTools，字段与 SalesCockpit/Index.vue 的 adaptCockpit 对齐；on 租户 16 全空（隔离生效） |

### collaboration.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | sendCollaborativeMessage | POST | `/api/customer-sessions/{sessionId}/messages` | `sessionId, data` | data | - | JWT | ✅ 通过 | L4 待发消息落库 sender_type=agent/sender_id=62；closed 会话回 400「会话状态 closed 不允许发送消息」 |
| 2 | - | addInternalNote | POST | `/api/customer-sessions/{sessionId}/internal-notes` | `sessionId, data` | data | - | JWT | 🔧 已修复 | L7 修：会话不存在时原可写入孤儿备注 → 现 404「会话不存在: xxx」；空内容回 INVALID_PARAM_1001「备注内容不能为空」 |
| 3 | - | searchMentionUsers | GET | `/api/users/search` | `params` | - | params | JWT | ✅ 通过 | L10 `?keyword=` 命中；不传 keyword 回前 20 个用户；on 模式只回本租户 |
| 4 | - | markMentionRead | POST | `/api/mentions/{mentionId}/read` | `mentionId` | {} | - | JWT | 🔧 已修复 | L13 该路径实为 NotificationController.MarkRead（通知而非 @提及）；修：不存在/越权原回 400 → 现 404「通知不存在或无权限」；非数字 id 400 |
| 5 | - | getMyMentions | GET | `/api/mentions/mine` | `params` | - | params | JWT | ✅ 通过 | L16 实为 `GET /api/auth/notifications` 同实现（page/page_size/type/is_read/keyword），返回通知列表；**观察：无独立 mention 表，无会话/消息引用，前端当前无调用方** |
| 6 | - | acquireEditLock | POST | `/api/customer-sessions/{sessionId}/edit-lock` | `sessionId` | {} | - | JWT | 🔧 已修复 | L21 修：原可锁任意 session id → 先校验会话存在/可见（404）；holder 缺省补 `user:<uid>`；他人持锁 409；过期锁惰性清理 |
| 7 | - | releaseEditLock | DELETE | `/api/customer-sessions/{sessionId}/edit-lock` | `sessionId` | {_silent} | - | JWT | ✅ 通过 | L24 不带 body 只解自己的锁；非持有人 404「无有效编辑锁或非持有人」 |
| 8 | - | getEditLock | GET | `/api/customer-sessions/{sessionId}/edit-lock` | `sessionId` | {_silent} | - | JWT | 🔧 已修复 | L27 修：不存在会话原回 `{locked:false}` 200 → 现 404；正常回 `{locked,lock{session_id,holder,expires_at}}` |
| 9 | - | listInternalNotes | GET | `/api/customer-sessions/{sessionId}/internal-notes` | `sessionId, params` | - | params | JWT | 🔧 已修复 | L30 修：不存在会话原回空列表 → 现 404；limit 缺省 50、>200 夹到 50；返回 `{list,total}`（含 tenant_id/created_at） |

### community.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | communityApi | getGroups | GET | `/api/community/groups` | `params` | - | params | JWT | 🔧 已修复 | L5 修：搜索参数 name/keyword 别名到 search（原只认 search，前端传 name 搜索框永远无效）；返回补 page/page_size 回显 |
| 2 | communityApi | createGroup | POST | `/api/community/groups` | `data` | data | - | JWT | 🔧 已修复 | L9 修：name 空/超 100 字符 → 400 中文（含 trim）；绑定失败不再泄漏 Go 结构体名 |
| 3 | communityApi | getGroupById | GET | `/api/community/groups/{id}` | `id` | - | - | JWT | 🔧 已修复 | L13 修：不存在 → 404「社群不存在: <id>」（原 404 文案笼统） |
| 4 | communityApi | updateGroup | PUT | `/api/community/groups/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | L17 修：同 404 文案带 id；绑定失败中文化 |
| 5 | communityApi | deleteGroup | DELETE | `/api/community/groups/{id}` | `id` | - | - | JWT | 🔧 已修复 | L21 修：同 404 文案带 id；语义为群软删 + 事务内硬删成员/消息 |
| 6 | communityApi | getMembers | GET | `/api/community/members` | `params` | - | params | JWT | 🔧 已修复 | L25 修：缺 group_id → 400 中文；返回补 page/page_size |
| 7 | communityApi | getMessages | GET | `/api/community/messages` | `params` | - | params | JWT | 🔧 已修复 | L29 修：缺 group_id → 400 中文；返回补 page/page_size |
| 8 | communityApi | getStats | GET | `/api/community/stats` | `params` | - | params | JWT | 🔧 已修复 | L33 修：带 group_id 时改为单群口径 {group_id,name,member_count,message_count,active_count}（原恒回全站统计，弹窗恒 0）；不存在群 → 404 |
| 9 | communityApi | importData | POST | `/api/community/import` | `data` | data | - | JWT | 🔧 已修复 | L37 修：逐条失败明细 {total,success_count,failed_count,errors[{index,name,message}]}（原静默吞错只回成功数）；请求体形状校验中文 |
| 10 | communityApi | exportData | POST | `/api/community/export` | `data` | data | - | JWT | ✅ 通过 | L41 实测 {groups,total} 正常；on 模式被 im-intl 模块门禁拦（4301，符合预期） |

### contactConsent.js

| # | 导出对象 | 函数/方法 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 行号 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | contactConsentApi | getTimeline | GET | `/api/contacts/{customerId}/consents` | `customerId, params` | - | params | JWT | 🔧 已修复 | L11 |
| 2 | contactConsentApi | record | POST | `/api/contacts/{customerId}/consents` | `customerId, data` | data | - | JWT | 🔧 已修复 | L15 |
| 3 | contactConsentApi | getPreferences | GET | `/api/contacts/{customerId}/preferences` | `customerId` | - | - | JWT | 🔧 已修复 | L19 |
| 4 | contactConsentApi | setPreference | PUT | `/api/contacts/{customerId}/preferences` | `customerId, data` | data | - | JWT | 🔧 已修复 | L23 |

> base = `/api/contacts`。本轮修复：#77 Record/SetPreference 严格校验 purpose（仅 marketing/support/transactional，未知值不再静默回落 marketing 污染事实账）、source 白名单（拒绝可伪造的 legacy_assertion）；#78 POST/PUT 绑定失败不再泄漏 Go 结构体名（`Key: 'consentRecordRequest.Decision' ...` → 中文提示）。实测 timeline/preferences 读写、grant/revoke、paused_until RFC3339 校验、on 模式租户隔离均通过；见 @b18@。

### conversionFunnel.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | ConversionFunnelApi | getFunnel | GET | `/api/conversion-funnels` | `params` | - | params(start_time,end_time) | JWT | 🔧 已修复 | L5：start_time/end_time 非法或倒置由静默回落改成 400；失败出口由 500+原始 err 改为 HandleDBError |
| 2 | ConversionFunnelApi | getStageDetails | GET | `/api/conversion-funnels/stage` | `stage, params` | - | stage,start_time,end_time | JWT | 🔧 已修复 | L8：rate 按漏斗同口径计算（原来恒 0）；新增 degraded / avg_duration_available；未知 stage 仍按既定契约回 200+空 |

> 控制器 `internal/ops/controller/conversion_funnel.go`、服务 `internal/ops/service/conversion_funnel.go`。实测（off 8204）：funnel 默认 30 天窗口、degraded=false；stage=clue/session/visit 正常（visit 100%、session 391.7% 属口径非单调的既定现象）；stage=bogus 回空对象；`start_time=yesterday` → 400「start_time 必须是 RFC3339 时刻」；起止倒置 → 400。on 8206（tenant 16）五段全 0、degraded=false（租户隔离）。`go test ./internal/ops/... -run Funnel` 全绿（含商机详情降级告警口径锁）。前端 `src/views/conversionFunnel/List.vue` 平均停留改为按 `avg_duration_available` 显示「暂无数据」，不再把 0.0 秒当实测值。

### csat.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | submitCSAT | POST | `/api/customer-sessions/{sessionId}/csat` | `sessionId, data` | data | - | JWT | 🔧 已修复 | L4：无调查单时 404 带 session_id（原通用「记录不存在」）；score 越界 400 |
| 2 | - | getCSATStats | GET | `/api/csat/stats` | `params(window)` | - | window=month/week/7d/30d | JWT | ✅ 通过 | L7：返回 avg_score/positive_rate/negative_count/threshold/window/distribution |
| 3 | - | getCSATTrend | GET | `/api/csat/trend` | `params(days)` | - | days | JWT | 🔧 已修复 | L10：date 由 `2026-10-31T00:00:00Z` 改为 `2026-10-31`（前端直接当 x 轴标签） |
| 4 | - | getNegativeCSAT | GET | `/api/csat/negative` | `params(limit)` | - | limit≤200 | JWT | ✅ 通过 | L13：回 `{list,total,threshold}`，limit 非法/超限夹到 50 |
| 5 | - | triggerCSATSurvey | POST | `/api/customer-sessions/{sessionId}/csat/trigger` | `sessionId` | {} | - | JWT | 🔧 已修复 | L16：会话不存在 404「会话不存在（id=…）」；已评分单重触发不退回 sent |
| 6 | - | getCSATTemplate | GET | `/api/csat/template` | - | - | - | JWT | ✅ 通过 | L18：未配置回落默认模板 |
| 7 | - | saveCSATTemplate | PUT | `/api/csat/template` | `data` | data | - | JWT+Admin | 🔧 已修复 | L19：坏 JSON 不再泄漏 Go 类型名；low_threshold 必须 1-5 数字、content 1-500 字符 |

> 控制器 `internal/controller/csat.go`、服务 `internal/service/csat.go`、仓储 `internal/repository/csat.go`；路由在 `internal/router/frontend_aliases.go:406-414`（PUT template 走 doRegAdmin）。实测（off 8204）：trigger→submit(5 分)→stats 立刻 responded=1/positive_rate=100/avg=5、trend 出当日行、negative 空；`nope-sess` 的 trigger/submit 均 404 且文案带 id；模板 PUT `low_threshold:9`/`""`/坏 JSON 均 400 中文，`"3"` 字符串被归一为数字 3。on 8206（tenant 16）stats/trend/negative 全空（租户隔离）。测试产生的调查单（id=44）与 tenant16 模板 KV 已删除，tenant1 模板恢复为 `{"content":"请给个评价","low_threshold":3}`。`go test ./internal/service/ -run CSAT` 绿。

### customer360.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getCustomerBasic | GET | `/api/customer-360/basic?user_id={id}` | `id, signal` | undefined | - | JWT | 🔧 | 错误出口统一：ErrCustomerNotFound→404「客户不存在」，其余走 HandleDBError（原来任何错误都回 404+英文原文） |
| 2 | - | getCustomerEvents | GET | `/api/customer-360/events` | `id, params, signal` | {user_id,} | - | JWT | 🔧 | 未知客户由「200 空数组」改为 404，与 basic/sessions 一致；DB 错误不再伪装成「客户不存在」 |
| 3 | - | getCustomerSessions | GET | `/api/customer-360/sessions` | `id, params, signal` | {user_id,} | - | JWT | 🔧 | 错误出口统一（404「客户不存在」/500 中文） |
| 4 | - | getCustomerTags | GET | `/api/customer-360/tags?user_id={id}` | `id, signal` | undefined | - | JWT | 🔧 | 错误文案不再拼接 DB 原文 |
| 5 | - | getCustomerOrders | GET | `/api/customer-360/orders` | `id, params, signal` | {user_id,} | - | JWT | 🔧 | 修复 ID 空间错配：前端传 basic_info.user_id（= customer_sessions.user_id），后端原来只认 customers.id → 现存客户恒 404；现已支持两套 ID 并回空列表 |
| 6 | - | getCustomerList | GET | `/api/customer-360/list` | `params` | - | params | JWT | 🔧 | 梳理 filters：platform / activity_level / purchase_power 原来被收集后丢弃 → 已在 DTO 层生效；带筛选时 total 回本页命中数 |
| 7 | - | getCustomerBasicById | GET | `/api/customer-360/basic?user_id={expr}` | `id` | - | - | JWT | 🔧 | 同 #1（encodeURIComponent 版本同一端点） |
| 8 | - | fetchTagsRaw | GET | `/api/customer-360/tags?user_id={expr}` | `id` | - | - | JWT | ✅ | GET tags 功能正常（返回 {user_id,tags}，前端 fetchTagsRaw 兼容数组与 .tags） |
| 9 | - | addCustomerTag | PUT | `/api/customer-360/tags?user_id={expr}` | `id, tag` | {tags} | - | JWT | 🔧 | 标签约束入库前校验：超 50 字符/超 100 个 → 400 中文（原来先删旧标签再插入，失败即数据丢失 + 500 泄漏 SQLSTATE 22001）；坏 body 文案中文化 |
| 10 | - | removeCustomerTag | PUT | `/api/customer-360/tags?user_id={expr}` | `id, tag` | {tags} | - | JWT | 🔧 | 同 #9（removeCustomerTag 传空数组可清空标签） |

### customerEvent.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/events/track` | - | data | - | JWT | 🔧 | 鉴权仅 JWT；#92 绑错文案中文化 + service.Track 新增 customer_id≤64/event_type≤32/event_source≤32 校验（原超长回 500 + SQLSTATE 22001 原文） |
| 2 | - | - | POST | `/api/events/pageview` | - | data | - | JWT | 🔧 | #92 缺参不再回 gin 结构体名 |
| 3 | - | - | POST | `/api/events/click` | - | data | - | JWT | 🔧 | #92 |
| 4 | - | - | POST | `/api/events/purchase` | - | data | - | JWT | 🔧 | #92（amount 为 0 会被 required 拦下，属既定语义） |
| 5 | - | - | POST | `/api/events/signup` | - | data | - | JWT | 🔧 | #92 |
| 6 | - | - | POST | `/api/events/login` | - | data | - | JWT | 🔧 | #92 |
| 7 | - | - | POST | `/api/events/add-to-cart` | - | data | - | JWT | 🔧 | #92 |
| 8 | - | - | GET | `/api/events/customer/{expr}` | - | - | params | JWT | ✅ | limit 非数字/越界静默夹取 50（未改） |
| 9 | - | - | GET | `/api/events/stats` | - | - | params | JWT | 🔧 | #93 start/end 现按业务日严格校验（非法/倒置回 400），不再静默回落默认窗口 |
| 10 | - | - | DELETE | `/api/events/customer/{expr}` | - | - | - | JWT | ✅ | 实际挂在 AdminAuthMiddleware 组，非普通 JWT |
| 11 | - | - | GET | `/api/customer-events/list` | - | - | params | JWT | 🔧 | #94 page_size 回显按仓储同规则夹取（0/999 → 50） |

### customerJourney.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | getJourneyOverview | - | GET | `/api/customer-journey/overview` | - | - | - | JWT | ✅ | 全旅程总览：total_customers + 10 阶段 count/rate/avg_stay_hours；私域 3 人（历史键回退可读） |
| 2 | getJourneyState | - | GET | `/api/customer-journey/overview` | - | - | customer_id | JWT | ✅ | 带 customer_id 走单客态（JourneyState）；未知客户合成 stranger（既定语义） |
| 3 | listJourneyStages | - | GET | `/api/customer-journey/stages` | - | - | - | JWT | ✅ | 10 个阶段元信息（label/description/default_followup/recommended_sop/owner_role/allow_ai_handle/auto_next_stage） |
| 4 | listByStage | - | GET | `/api/customer-journey/by-stage` | - | - | stage | JWT | 🔧 | #95 未知阶段不再静默回空表 → 400「未知的阶段: x（可用: stranger / … / lost）」；缺参同样带可用列表 |
| 5 | transitionJourney | - | POST | `/api/customer-journey/transition` | - | data | - | JWT | 🔧 | #95 操作人恒为 manual（JWT 存 uint，原代码断言 string）→ 现 `operator_id:"user:62"`；同阶段迁移回 `changed:false` 而非「迁移成功」+null；绑定错误去 gin 结构体名；#96 Redis 键不再重复拼租户前缀 |
| 6 | touchCustomer | - | POST | `/api/customer-journey/touch` | - | data | - | JWT | 🔧 | #95 响应改 `{customer_id,touched:true}`（原 data 为 null）；绑定错误中文化 |

### customerService.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/agents` | - | data | - | JWT | 🔧 | #99 建坐席：姓名/接待上限校验（1~50），重复坐席号 409「坐席 N 已存在」，绑定错误去 gin 结构体名（原 400 + PG duplicate key 原文） |
| 2 | - | - | GET | `/api/agents/{id}` | - | - | - | JWT | 🔧 | #99 不存在 → 404「坐席不存在: N」（原 404 + 仓储错误原文） |
| 3 | - | - | GET | `/api/agents/online` | - | - | - | JWT | ✅ | status IN (online,busy) 且 last_active_at > now-5min；兜底错误文案中文化 |
| 4 | - | - | GET | `/api/agents/all` | - | - | - | JWT | ✅ | admin 看全部（上限 1000），非 admin 只回自己那条；兜底错误文案中文化 |
| 5 | - | - | PUT | `/api/agents/{id}/status` | - | data | - | JWT | 🔧 | #99 状态白名单 online/busy/offline（任意串会落库成幽灵坐席）；max_sessions 原先被静默丢弃（前端弹窗同一表单提交）→ 现落库并校验 1~50；不存在 404 |
| 6 | - | - | POST | `/api/agents/{id}/online` | - | - | - | JWT | 🔧 | #99 不存在 → 404「坐席不存在: N」，离线分支错误语义化为 400 参数错误 |
| 7 | - | - | POST | `/api/agents/{id}/offline` | - | - | - | JWT | 🔧 | #99 有在办会话 → 400「还有 N 个未完成的会话…」；不存在 → 404 |
| 8 | - | - | GET | `/api/agents/{id}/sessions` | - | - | - | JWT | ✅ | 非 admin 仅可查自己（403），并顺带刷心跳 |
| 9 | - | - | GET | `/api/agents/me` | - | - | - | JWT | ✅ | 按登录 user_id 取/建坐席身份（on 模式 agent_id=52） |
| 10 | - | - | GET | `/api/quick-replies` | - | - | params | JWT | ✅ | 按 category 过滤；兜底错误文案中文化 |
| 11 | - | - | GET | `/api/quick-replies/categories` | - | - | - | JWT | ✅ | 返回分类名数组 |
| 12 | - | - | POST | `/api/quick-replies` | - | data | - | JWT | 🔧 | #100 绑定错误中文化（category/title/content 必填）；新建恒 is_public=true；错误出口改 ErrorFromDB |
| 13 | - | - | PUT | `/api/quick-replies/{id}` | - | data | - | JWT | 🔧 | #100 不存在 → 404「快捷回复不存在: N」（原 400 + gorm 原文）；is_public 未传保留原值 |
| 14 | - | - | DELETE | `/api/quick-replies/{id}` | - | - | - | JWT | 🔧 | #100 不存在 → 404「快捷回复不存在: N」（原 400） |
| 15 | - | - | GET | `/api/session-tags` | - | - | - | JWT | ✅ | 按 sort_order 升序、租户隔离；兜底错误文案中文化 |
| 16 | - | - | POST | `/api/session-tags` | - | data | - | JWT | 🔧 | #101 绑定错误中文化；编码重复 409（透明化 PG 原文泄漏问题）；创建走统一错误出口 |
| 17 | - | - | PUT | `/api/session-tags/{id}` | - | data | - | JWT | 🔧 | #101 不存在 → 404「会话标签不存在: N」；改编码撞唯一索引 → 409「标签编码已存在」 |
| 18 | - | - | DELETE | `/api/session-tags/{id}` | - | - | - | JWT | 🔧 | #101 不存在 → 404「会话标签不存在: N」 |
| 19 | - | - | GET | `/api/ai-suggestions/{sessionId}` | - | - | - | JWT | ✅ | 按 session_id 取最近 10 条；session_id 可由路径或 query 给 |
| 20 | - | - | POST | `/api/ai-suggestions/{id}/use` | - | - | - | JWT | 🔧 | #102 0 行更新 → 404「AI 建议不存在（id=N）」（原 404 + 装饰后的 gorm 原文） |
| 21 | - | - | POST | `/api/customer-sessions/{sessionId}/tags` | - | data | - | JWT | 🔧 | #103 路径参数是会话主键（customer_sessions.id 数字）而非 session_id 字符串，文案已点明；tags 绑定错误中文化；不存在 → 404「会话不存在（id=N）」（原笼统「记录不存在」） |

### customerServiceAgent.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | listMounts | - | GET | `/api/customer-service-agents` | - | - | params | JWT | 🔧 | #107 缺参/非数字不再泄漏 gin 结构体名与 strconv 原文 → 400「agent_status_id（坐席主键）必填」/「…必须是数字主键」 |
| 2 | listMountsByAIAgent | - | GET | `/api/customer-service-agents/by-ai-agent/{aiAgentId}` | - | - | - | JWT | ✅ | 返回 `{list,total}`；未知智能体回空列表 |
| 3 | createMount | - | POST | `/api/customer-service-agents` | - | data | - | JWT | 🔧 | #104 不再写出孤儿挂载（坐席不存在 → 404「坐席不存在: N」）；#107 重复对 → 409「该坐席已挂载此智能体（agent_status_id=…, ai_agent_id=…）」，绑定错误中文化 |
| 4 | updateMount | - | PUT | `/api/customer-service-agents/{id}` | - | data | - | JWT | 🔧 | #105 改到不存在的坐席/智能体 → 404（原可写成孤儿）；#107 不存在 → 404「客服挂载不存在: N」 |
| 5 | deleteMount | - | DELETE | `/api/customer-service-agents/{id}` | - | - | - | JWT | 🔧 | #107 不存在 → 404「客服挂载不存在: N」（原笼统「删除失败」+data record not found） |
| 6 | listMountsByUser | - | GET | `/api/customer-service-agents/by-user/{userId}` | - | - | - | JWT | 🔧 | #108 user_id 为 0/非数字前置 400；返回 `{list,total}`，无坐席回空 |
| 7 | createMountByUser | - | POST | `/api/customer-service-agents/by-user/{userId}` | - | data | - | JWT | 🔧 | #106 校验用户真实存在（不存在/跨租户 → 404「用户不存在: N」，原可凭空造坐席）；缺表类错误降级为仅告警（单测夹具） |

### customerSession.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | getSessions | - | GET | `/api/customer-sessions` | - | - | params | JWT | 🔧 | #109 page/page_size 回显按仓储规则归一化（0/999 → 1/100）；#110 status 白名单校验，未知值 400 并列出可用状态；兜底文案不再拼 DB 原文 |
| 2 | getSessionMessages | - | GET | `/api/customer-sessions/{id}/messages` | - | - | params | JWT | 🔧 | #109 分页归一化（默认 50、上限 200）；#111 会话不存在 → 404「会话不存在（id=…）」/「（session_id=…）」，原来笼统「记录不存在」 |
| 3 | sendMessage | - | POST | `/api/customer-sessions/{sessionId}/messages` | - | {content,sender_type,sender_name,sender_id} | - | JWT | 🔧 | #111 服务层新增哨兵 ErrCustomerSessionNotFound / SessionNotSendableError，控制器改 errors.Is/As（原靠 err.Error()=="会话不存在" 字符串匹配）；#112 绑定错误去 gin 结构体名；发送人身份仍由登录态强制 |
| 4 | createSession | - | POST | `/api/customer-sessions` | - | data | - | JWT | 🔧 | #112 缺参文案中文化（platform/account_id/user_id 必填）；命中黑名单会拒绝创建（既定语义） |
| 5 | closeSession | - | POST | `/api/customer-sessions/{id}/close` | - | - | - | JWT | ✅ | 关闭成功；重复关闭幂等 |
| 6 | transferSession | - | POST | `/api/customer-sessions/{id}/transfer` | - | data | - | JWT | 🔧 | #112 缺参文案中文化；#113 客服不存在改为「客服不存在（agent_id=N，请先在坐席管理中登记该账号）」 |
| 7 | takeover | - | POST | `/api/customer-sessions/{id}/takeover` | - | {reason} | - | JWT | ✅ | 接管成功返回 handler_type/session_id；坐席离线/未登记/会话他人名下均有明确原因 |
| 8 | releaseSession | - | POST | `/api/customer-sessions/{id}/release` | - | - | - | JWT | 🔧 | #113 AI 托管中（agent_id=0）不再报「无权操作」→「当前会话未由坐席持有…无需释放」；非持有人仍 400 并带 agent_id/session_id |
| 9 | switchHandler | - | POST | `/api/customer-sessions/{id}/switch-handler` | - | {handler_type,reason} | - | JWT | ✅ | 切 AI/人工成功返回 handler_type；非持有人 400 带 id |
| 10 | blacklistSession | - | POST | `/api/customer-sessions/{id}/blacklist` | - | {reason,ttl_hours} | - | JWT | ✅ | 拉黑成功；ttl_hours=0 为永久（落 user_blacklist，键 user_id+platform） |
| 11 | unblacklist | - | POST | `/api/customer-sessions/blacklist/remove` | - | {user_id,platform} | - | JWT | 🔧 | #112 缺参文案中文化（user_id 必填，platform 默认 web） |
| 12 | checkBlacklist | - | GET | `/api/customer-sessions/blacklist/check` | - | - | params | JWT | ✅ | 返回 {blacklisted:bool}；缺 user_id 400 |
| 13 | listBlacklist | - | GET | `/api/customer-sessions/blacklist` | - | - | params | JWT | 🔧 | #109 分页归一化（默认 20、上限 100）；兜底文案不再拼 DB 原文（#112） |
| 14 | - | - | GET | `/api/customer-360` | - | - | user_id | JWT | ✅ | 复用 customer360 模块结论（DTO 装配；未知客户 404） |
| 15 | - | - | GET | `/api/customer-360/basic` | - | - | user_id | JWT | ✅ | 复用 customer360 结论（缺参 400、未知 404） |
| 16 | - | - | GET | `/api/customer-360/stats` | - | - | user_id | JWT | ✅ | 复用 customer360 结论 |
| 17 | - | - | GET | `/api/customer-360/sessions` | - | - | user_id | JWT | ✅ | 复用 customer360 结论（返回 session_history/total） |
| 18 | - | - | GET | `/api/customer-360/tags` | - | - | user_id | JWT | ✅ | 复用 customer360 结论（返回 {user_id,tags}） |

### customIntegration.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/custom-tools` | - | - | - | JWT | 🔧 | #117 失败不再直出 err.Error() → 500「获取工具集列表失败」 |
| 2 | - | - | POST | `/api/custom-tools/import` | - | payload | - | JWT | 🔧 | #117 绑定错不再泄漏 gin 结构体名；同名/落库撞唯一键 → 409「工具集 \"x\" 已存在，请先删除再导入」；SSRF（环回/链路本地）→ 400；落库失败不再回传 SQLSTATE |
| 3 | - | - | DELETE | `/api/custom-tools/{id}` | - | - | - | JWT | 🔧 | #117 不存在/重复删 → 404「工具集不存在（id=N）」（原 500 + 原文）；id 非法 400「无效的 id」 |
| 4 | - | - | GET | `/api/mcp-servers` | - | - | - | JWT | 🔧 | #114 失败 → 500「获取 MCP 配置列表失败」（原直出 err.Error()）；响应不含 api_key（APIKeyEnc json:"-"） |
| 5 | - | - | POST | `/api/mcp-servers` | - | data | - | JWT | 🔧 | #114 缺 name/endpoint → 400 中文、同名 → 409「MCP server \"x\" 已存在，请先删除」、落库异常 → 500 泛化（原回传 PG 原文）；#118 显式 enabled:false 不再被 GORM default:true 覆盖（创建后 SetEnabled 回写） |
| 6 | - | - | DELETE | `/api/mcp-servers/{id}` | - | - | - | JWT | ✅ | 本来就正确：不存在/重复删 → 404「MCP 配置不存在」 |
| 7 | - | - | PUT | `/api/mcp-servers/{id}/enabled` | - | data | - | JWT | 🔧 | #115 配置不存在 → 404「MCP 配置不存在（id=N）」（原 400）；id 非法 400「无效的 id」；启用时会做工具发现（失败静默） |
| 8 | - | - | GET | `/api/mcp-servers/{id}/test` | - | - | - | JWT | 🔧 | #116 配置不存在 → 404（原 502）；端点不可达 → 502 UPSTREAM_FAILURE_5005「连通性测试失败: … connection refused」 |

### customReport.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | getReportList | - | GET | `/api/custom-reports` | params | - | params | JWT | 🔧 | #119 page/page_size 纠偏后再回显（0/0 → 1/20，>100 夹 100）；租户隔离生效 |
| 2 | getReport | - | GET | `/api/custom-reports/{expr}` | - | - | - | JWT | 🔧 | #123 不存在 → 404「报表不存在（id=N）」（原任何错误都 404 + DB 原文）；越权 → 403 |
| 3 | createReport | - | POST | `/api/custom-reports` | - | data | - | JWT | 🔧 | #120 绑定错改静态中文（原泄漏 Go 类型名）；#124 数据源/图表类型非法 → 400 INVALID_PARAM_1001（原 500）；dimensions/metrics 存 JSON 串，字段 field/label/data_type/group_by 与 field/label/agg_func/data_type |
| 4 | updateReport | - | PUT | `/api/custom-reports/{expr}` | - | data | - | JWT | 🔧 | #120/#123/#124 全量替换语义（只传 name 会因 data_source 为空 → 400「不支持的数据源类型」）；不存在 404 带 id；越权 403 |
| 5 | deleteReport | - | DELETE | `/api/custom-reports/{expr}` | - | - | - | JWT | 🔧 | #123 不存在 → 404「报表不存在（id=N）」 |
| 6 | getPublicTemplates | - | GET | `/api/custom-reports/templates` | - | - | - | JWT | ✅ | 静态路由优先于 `/:id`，无冲突；当前返回空数组 |
| 7 | useReportTemplate | - | POST | `/api/custom-reports/templates/{expr}/use` | - | - | - | JWT | 🔧 | #123 模板不存在 → 404「报表不存在（模板 id=N）」（原笼统「记录不存在」） |
| 8 | queryReportData | - | GET | `/api/custom-reports/{expr}/data` | params | - | params | JWT | 🔧 | #121 start_time/end_time 严格 YYYY-MM-DD（非法 → 400，原静默忽略查全量）；#123 不存在 404；返回 {dimensions,metrics,data,total} |
| 9 | exportReport | - | GET | `/api/custom-reports/{expr}/export` | params | - | params | JWT | 🔧 | #121 日期校验；#122 先取数再写 CSV 头（错误时回 JSON 400，不再把 JSON 混入 CSV 流）；超 50000 行 → 400 |
| 10 | runReport | - | GET | `/api/custom-reports/{expr}/export?format=run` | params | - | params | JWT | 🔧 | 前端「运行」实际复用 /data（format=run 仅前端标记），后端同 #8 结论 |

### dashboardScreen.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/dashboards` | - | - | params | JWT | 🔧 | #126 page=0/page_size=0 归一化为 1/10（原仓储按 (page-1)*size 算 offset，page=0 会负 offset 报错）；失败不再直出 err.Error() |
| 2 | - | - | GET | `/api/dashboards/{id}` | - | - | - | JWT | 🔧 | #127 不存在 → 404「大屏不存在（id=N）」，DB 故障 → 500「获取大屏详情失败」（原任何错都 404 + 原文） |
| 3 | - | - | POST | `/api/dashboards` | - | data | data | JWT | 🔧 | #125 绑定错误不再泄漏 gin 结构体名；#125 修 `user_id` 强制断言导致的 panic（改 dashboardUserID，缺失回 401） |
| 4 | - | - | PUT | `/api/dashboards/{id}` | - | data | data | JWT | 🔧 | #125 绑定文案中文化；#130 不存在 → 404「大屏不存在（id=N）」（原笼统「记录不存在」） |
| 5 | - | - | DELETE | `/api/dashboards/{id}` | - | - | - | JWT | 🔧 | #130 不存在/重复删 → 404「大屏不存在（id=N）」 |
| 6 | - | - | GET | `/api/dashboards/public/{code}` | - | - | - | JWT | ✅ | 公开路由无鉴权（public 组）；#128 已确认不存在 → 404「大屏不存在或未公开（code=X）」、其余错误 → 500（不再回 404 + 原文） |
| 7 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | 🔧 | #129 聚合失败 → 500「聚合大屏数据失败」（去掉 err.Error()）；isAdmin 口径补 super_admin |
| 8 | - | - | GET | `/api/dashboards/activities` | - | - | - | JWT | 🔧 | #129 新增 `?limit`（默认 20、上限 100，原硬编码 20）；失败文案去 err.Error() |
| 9 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | 🔧 | 同 #7（前端 getKPIs，重复端点） |
| 10 | - | - | GET | `/api/dashboards/data` | - | - | params | JWT | 🔧 | 同 #7（前端 getTrends，重复端点；params 由聚合层忽略） |
| 11 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | 🔧 | 同 #7（前端 getChannels，重复端点） |
| 12 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | 🔧 | 同 #7（前端 getRegions，重复端点） |

### dialogueMemory.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | memoryApi | appendMessage | POST | `/api/memory/messages` | `data` | data | - | JWT | 🔧 | #131/#132 错误出口统一 + 缺参 400 中文「session_id、role、content 均为必填」；会话/客户不存在时按设计「get-or-create」建记忆行；落库后 L1Append 到短期记忆 |
| 2 | memoryApi | getShortTerm | GET | `/api/memory/short` | `params` | - | params | JWT | ✅ | 读 message hub 会话消息（`ListMessageHubByConversation`）；对无 hub 记录的测试 session 返回 `[]` 属设计，不是缺陷；缺 session_id 400 中文（#132） |
| 3 | memoryApi | getLongTerm | GET | `/api/memory/long` | `params` | - | params | JWT | 🔧 | #131/#132 空 session_id 现在 400（原直接下渗服务）；返回记忆行（summary/key_facts/objections…） |
| 4 | memoryApi | updateKeyFacts | POST | `/api/memory/facts` | `data` | data | - | JWT | 🔧 | #132 缺参 400 中文「session_id 与 facts（字符串字典）均为必填」；key_facts 落库正常 |
| 5 | memoryApi | recordObjection | POST | `/api/memory/objections` | `data` | data | - | JWT | 🔧 | #132 缺参 400 中文；异议追加到 objections 数组 |
| 6 | memoryApi | updatePurchaseIntent | POST | `/api/memory/purchase-intent` | `data` | data | - | JWT | 🔧 | #131/#132 |
| 7 | memoryApi | recordIntent | POST | `/api/memory/intent-trail` | `data` | data | - | JWT | 🔧 | #131/#132 |
| 8 | memoryApi | recordSOP | POST | `/api/memory/sop-history` | `data` | data | - | JWT | 🔧 | #131/#132 |
| 9 | memoryApi | buildContext | GET | `/api/memory/context` | `params` | - | params | JWT | 🔧 | #131 返回 `{context:"【客户长期记忆】…【最近对话】…"}`；错误不再直出 err.Error() |
| 10 | memoryApi | list | GET | `/api/memory/list` | `params` | - | params | JWT | 🔧 | #133 原用 `pagination.Parse` 丢弃 page 且回显 page=1/page_size=limit；现 page_size 归一化（<1→20、>100→100）并回显 `{list,page:1,page_size,total}`（服务端无 offset，page 固定 1） |

### dingtalkApp.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/dingtalk-app/accounts` | - | - | params | JWT+Admin | 🔧 | #135 失败不再把 err 原文塞进 data → 500「查询钉钉应用账号列表失败」；响应中 app_secret/aes_key 只回 `*_masked`（app_key/token 明文） |
| 2 | - | - | GET | `/api/dingtalk-app/accounts/{id}` | - | - | - | JWT+Admin | 🔧 | #135 不存在 → 404「钉钉应用账号不存在（id=N）」，DB 故障不再伪装成 404（改 500）；id 非数字 → 400「无效的账号 ID」 |
| 3 | - | - | POST | `/api/dingtalk-app/accounts` | - | data | - | JWT+Admin | 🔧 | #134 新增必填校验（account_name/app_key/app_secret）→ 400 中文（原可建出全空孤儿账号，库里现存 8 条历史空行 id=2-10）；创建者取登录用户，user_id 字段被忽略 |
| 4 | - | - | PUT | `/api/dingtalk-app/accounts/{id}` | - | data | - | JWT+Admin | 🔧 | #135 错误文案统一；空 app_secret/token/aes_key = 保留原值，status=0 = 保留原值（实测改名后 token/secret/status 均未丢） |
| 5 | - | - | DELETE | `/api/dingtalk-app/accounts/{id}` | - | - | - | JWT+Admin | 🔧 | #135 成功 200；重复删 → 404「钉钉应用账号不存在（id=N）」；id 非数字 400 |
| 6 | - | - | POST | `/api/dingtalk-app/accounts/{id}/test` | - | - | - | JWT+Admin | 🔧 | #135 不存在 → 404 带 id；存在 → 200 `{ok:true,inbound_enabled}`。**语义提示**：只校验 app_key/app_secret/token 非空，不做真实连通性探测 |

### domainPool.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getDomainHealth | GET | `/api/domain-pool/health` | - | - | - | JWT | ✅ | 2026-10-31 实测 200「探测完成」，返回每个域名的 dns_resolved/不同状态 |
| 2 | - | checkBlacklist | GET | `/api/domain-pool/{domain}/blacklist` | `domain` | - | - | JWT | ✅ | 路径参数是**域名字符串**（非 id），`{blacklisted,domain}`；`/abc/blacklist` 也正常 200 |
| 3 | - | suspendDomain | POST | `/api/domain-pool/{id}/suspend` | `id` | {} | - | JWT | ✅ | 200「域名已停用」status=4；body 可空 |
| 4 | - | rotateToBackup | POST | `/api/domain-pool/{id}/rotate` | `id` | {} | - | JWT | ✅ | 已停用域名回 400「域名已停用，无法轮换激活」（守卫正确） |
| 5 | - | listAlerts | GET | `/api/domain-pool/alerts` | `params` | - | params | JWT | 🔧 | #137 失败不再直出 err.Error() → 500「获取域名告警失败」 |
| 6 | - | resolveAlert | POST | `/api/domain-pool/alerts/{id}/resolve` | `id` | {} | - | JWT | ✅ | 域名仍不健康 → 400「域名仍不健康，告警保持」（走 HandleServiceError） |
| 7 | domainPoolApi | getList | GET | `/api/domain-pool` | `params` | - | params | JWT | 🔧 | #137 查询失败 → 500「获取域名列表失败」；响应 `{list,total}`（不回显分页，前端未依赖） |
| 8 | domainPoolApi | create | POST | `/api/domain-pool` | `data` | data | - | JWT | 🔧 | #136 缺 domain → 400「请求参数错误：domain 必填，port / purpose 可选」（原泄漏 gin 结构体名）；#137 落库失败不再直出原文 |
| 9 | domainPoolApi | update | PUT | `/api/domain-pool/{id}` | `data` | data | - | JWT | 🔧 | #138 零值不覆盖：不传 port/purpose/status 时沿用原值（原实现会把 status 打成 0、port 改 80）；#136 绑定文案中文化；显式 status 生效 |
| 10 | domainPoolApi | delete | DELETE | `/api/domain-pool/{id}` | `id` | - | - | JWT | 🔧 | 不存在 → 404「域名不存在」；被活码引用 → 400 中文提示；#137 其余失败 → 500「删除域名失败」 |
| 11 | domainPoolApi | checkDomain | POST | `/api/domain-pool/{id}/check` | `id` | {} | - | JWT | ✅ | 200 `{healthy:false,id:133}`（域名不可解析时 healthy=false） |
| 12 | domainPoolApi | checkAllDomains | POST | `/api/domain-pool/check-all` | - | {} | - | JWT | 🔧 | #137 批量检查失败 → 500「批量检查域名失败」（原直出 err.Error()） |

### douyinCard.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/douyin/list` | - | - | - | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/douyin/{id}` | - | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/douyin/create` | - | - | - | JWT | ✅ 通过 | 创建成功（自动生成 /s/xxx 短码） |
| 4 | - | - | PUT | `/api/douyin/update` | - | - | - | JWT | ✅ 通过 | 全量语义（未传字段被清空，前端整表单提交） |
| 5 | - | - | DELETE | `/api/douyin/delete/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | GET | `/api/douyin/view/{id}` | - | - | - | JWT | ✅ 通过 | 落地页视图数据 |
| 7 | - | - | GET | `/api/douyin/stats/card/{id}` | - | - | - | JWT | ✅ 通过 | cardId/viewCount/dailyStats |
| 8 | - | - | GET | `/api/douyin/stats/overall` | - | - | - | JWT | ✅ 通过 | totalCards/activeCards/totalViews |
| 9 | - | - | POST | `/api/douyin/{id}/generate-short-link` | - | - | - | JWT | ✅ 通过 | 短链生成/复用成功 |

### egolite.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listEgoFlows | GET | `/api/egolite/flows` | - | - | - | JWT | ✅ | 读口；成功 `{code:0,data:{list,total}}` |
| 2 | - | getEgoFlow | GET | `/api/egolite/flows/{id}` | - | - | - | JWT | ✅ | 不存在 → HTTP 404 `{code:1,message:"流程不存在"}` |
| 3 | - | rescanEgoFlow | POST | `/api/egolite/flows/{id}/rescan` | - | - | - | JWT | ✅ | 返回 `{passed:true}`；未通过时附 scan_report |
| 4 | - | publishEgoFlow | POST | `/api/egolite/flows/{id}/publish` | - | - | - | JWT | ✅ | draft→published |
| 5 | - | submitEgoFlow | POST | `/api/egolite/flows/{id}/submit` | - | - | - | JWT | ✅ | published→submitted（进审核工作台） |
| 6 | - | dryRunEgoFlow | POST | `/api/egolite/flows/{id}/dry-run` | - | - | - | JWT | ✅ | 创建 origin=draft 执行；无 runner 时停在 created |
| 7 | - | deleteEgoFlow | DELETE | `/api/egolite/flows/{id}` | - | - | - | JWT | 🔧 | #144 删流程未摘定时触发器 → 现随流程一并 `cronSvc.Delete`（残留调度行会与「流程已删除」自相矛盾） |
| 8 | - | getEgoFlowCron | GET | `/api/egolite/flows/{id}/cron` | - | - | - | JWT | ✅ | 未挂载回 HTTP 404 + `{code:1}`；挂载后回完整行 |
| 9 | - | upsertEgoFlowCron | POST | `/api/egolite/flows/{id}/cron` | - | - | - | JWT | 🔧 | #142 软删撞唯一索引 `idx_egolite_flow_crons_flow_id`（摘除后再挂载必 23505 且回 SQL 原文）→ `DeleteByFlow` 改 `Unscoped()` 物理删；错误出口改 `egoErrorMessage` |
| 10 | - | deleteEgoFlowCron | DELETE | `/api/egolite/flows/{id}/cron` | - | - | - | JWT | ✅ | 修后回 `{code:0,message:"已摘除定时触发器"}` |
| 11 | - | listEgoExecutions | GET | `/api/egolite/executions` | - | - | - | JWT | ✅ | `{code:0,data:{list,total}}` |
| 12 | - | getEgoExecution | GET | `/api/egolite/executions/{id}` | - | - | - | JWT | ✅ | 不存在 → 404 `{code:1,"执行不存在"}` |
| 13 | - | listEgoEvents | GET | `/api/egolite/executions/{id}/events` | - | - | - | JWT | ✅ | `{list:[]}`（支持 after_id 游标） |
| 14 | - | startEgoExecution | POST | `/api/egolite/executions` | - | - | - | JWT | ✅ | `{flow_id,origin}` → 建执行；无 runner 停在 created |
| 15 | - | abortEgoExecution | POST | `/api/egolite/executions/{id}/abort` | - | - | - | JWT | ✅ | created→stopped，写 fail_reason「用户停止」+ ended_at |
| 16 | - | redispatchEgoExecution | POST | `/api/egolite/executions/{id}/redispatch` | - | - | - | JWT | ✅ | 可重派；非可重派状态走守卫文案 |
| 17 | - | confirmEgoExecution | POST | `/api/egolite/executions/{id}/confirm` | - | - | - | JWT | ✅ | 终态回 `{code:1,"执行状态不允许该操作"}`（守卫正确） |
| 18 | - | getEgoRunnerStatus | GET | `/api/egolite/runner/status` | - | - | - | JWT | ✅ | `{count,runners}`；普通用户只看自己，admin 全量 |
| 19 | - | listEgoRunnerTokens | GET | `/api/egolite/runner/tokens` | - | - | - | JWT | ✅ | 只回 token_hint（明文仅签发时一次） |
| 20 | - | issueEgoRunnerToken | POST | `/api/egolite/runner/tokens` | - | - | - | JWT | ✅ | 返回明文 token + hint，提示只显示一次 |
| 21 | - | revokeEgoRunnerToken | POST | `/api/egolite/runner/tokens/{id}/revoke` | - | - | - | JWT | ✅ | 「已撤销；该令牌立即失效」 |
| 22 | - | listEgoAssets | GET | `/api/egolite/assets` | - | - | - | JWT | ✅ | 用户端只列 published |
| 23 | - | listEgoAdminAssets | GET | `/api/egolite/admin/assets` | - | - | - | JWT | ✅ | 含 offline；支持 params 过滤 |
| 24 | - | listEgoAdminFlows | GET | `/api/egolite/admin/flows` | - | - | - | JWT | ✅ | 跨用户审核工作台 |
| 25 | - | publishEgoAssetFromFlow | POST | `/api/egolite/admin/assets/publish` | - | - | - | JWT | ✅ | `{flow_id}` → 生成资产行（version/script_hash/status=published） |
| 26 | - | publishEgoAssetPack | POST | `/api/egolite/admin/assets/pack` | - | - | - | JWT | ✅ | 缺 name / domains → `{code:1}` 中文校验文案 |
| 27 | - | offlineEgoAsset | POST | `/api/egolite/admin/assets/{id}/offline` | - | - | - | JWT | ✅ | status→offline，用户端不再可见 |
| 28 | - | listEgoSessions | GET | `/api/egolite/sessions` | - | - | - | JWT | ✅ | `{list,total}` |
| 29 | - | getEgoSession | GET | `/api/egolite/sessions/{id}` | - | - | - | JWT | ✅ | 不存在 → `{code:1,"浏览器会话不存在"}` |
| 30 | - | createEgoSession | POST | `/api/egolite/sessions` | - | - | - | JWT | ⛔ | 环境阻塞：无 ego-runner 在线 → 「ego-runner 未连接…」；另注意 body 字段是 `domains`（数组，必填白名单） |
| 31 | - | runEgoSessionRound | POST | `/api/egolite/sessions/{id}/round` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 32 | - | observeEgoSession | POST | `/api/egolite/sessions/{id}/observe` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 33 | - | listEgoSessionRounds | GET | `/api/egolite/sessions/{id}/rounds` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 34 | - | confirmEgoSession | POST | `/api/egolite/sessions/{id}/confirm` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 35 | - | abortEgoSession | POST | `/api/egolite/sessions/{id}/abort` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 36 | - | finishEgoSession | POST | `/api/egolite/sessions/{id}/finish` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 37 | - | getEgoTelemetrySummary | GET | `/api/egolite/admin/telemetry/summary` | - | - | - | JWT | ✅ | `{buckets,total,truncated,window_hours}`，只回聚合不含页面内容 |
| 38 | - | exportEgoSessionAudit | GET | `/api/egolite/sessions/{id}/export` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |
| 39 | - | saveEgoSessionAsFlow | POST | `/api/egolite/sessions/{id}/save-as-flow` | - | - | - | JWT | ⛔ | 环境阻塞（需本机 ego-runner） |

> 说明：egolite 成功响应走标准信封 `{code:0,data,message}`，错误走 `{code:1,message}`（HTTP 状态另给），前端 `isBizSuccess` 兼容。会话组（探索式录制）需本机 ego-runner 在线，本次标 ⛔。

### email.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 | 行号 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | emailApi | getEmailSmtpList | GET | /api/email/smtp | - | - | - | JWT | 🔧 | #145 查询失败原错回 400 → 现 500「获取 SMTP 配置列表失败」；当前库有 27 条历史空配置（脏数据） | L5 |
| 2 | emailApi | addEmailSmtp | POST | /api/email/smtp | `data` | data | - | JWT | 🔧 | #147 绑定失败文案中文化；必填 name/server/port/username/password/limit；响应不回密码 | L9 |
| 3 | emailApi | updateEmailSmtp | PUT | /api/email/smtp/{id} | `id, data` | data | - | JWT | 🔧 | #148 PUT 原静默不生效（控制器没读路径 id，body 无 id）；现注入路径 id + 存在性校验 → 404「SMTP配置不存在」 | L13 |
| 4 | emailApi | deleteEmailSmtp | DELETE | /api/email/smtp/{id} | `id` | - | - | JWT | 🔧 | #149 重复删原仍回成功；现删前校验存在 → 404「SMTP配置不存在」 | L17 |
| 5 | emailApi | getDrafts | GET | /api/email/drafts | - | - | - | JWT | ✅ | `{total,list}`；分页不回声 page/limit（前端未依赖） | L21 |
| 6 | emailApi | getDraftDetail | GET | /api/email/drafts/{id} | `id` | - | - | JWT | ✅ | uuid 校验 400「无效的 ID 格式」；不存在 404「邮件草稿不存在」 | L25 |
| 7 | emailApi | createDraft | POST | /api/email/drafts | `data` | data | - | JWT | ✅ | subject 必填，content/attachments 可选 | L29 |
| 8 | emailApi | updateDraft | PUT | /api/email/drafts/{id} | `id, data` | data | - | JWT | ✅ | 实测改名/改 content/attachments 生效 | L33 |
| 9 | emailApi | deleteDraft | DELETE | /api/email/drafts/{id} | `id` | - | - | JWT | ✅ | 成功 200；不存在 404「邮件草稿不存在」 | L37 |
| 10 | emailApi | uploadImage | POST | /api/upload | `formData` | formData | - | JWT | ✅ | 复用 assetBundle 模块结论（multipart file；已知缺陷：无文件时文案误导） | L41 |
| 11 | emailApi | sendEmail | POST | /api/email/send | `data` | data | - | JWT | 🔧 | #150 绑定文案改为列出必填 to/subject/content/smtpId；合法 payload → 「发送成功」并写 email_sends。注意：路由挂 AdminAuthMiddleware | L49 |
| 12 | emailApi | getEmailList | GET | /api/email/list | `page, limit` | - | params | JWT | 🔧 | #147 失败文案改「获取邮件列表失败」；当前 total 0（本租户无数据） | L52 |
| 13 | emailApi | getJobsList | GET | /api/email/jobs | `page, limit` | - | params | JWT | 🔧 | #145 查询失败文案；返回 {total,list}（jobs 字段 subject/email_total/...）；无创建端点（POST 405，前端未使用） | L56 |
| 14 | emailApi | deleteJob | DELETE | /api/email/jobs/{id} | `id` | - | - | JWT | 🔧 | uuid 非法 → 400「无效的任务 ID（需 UUID）」；不存在 → 404「邮件任务不存在」 | L59 |
| 15 | emailApi | getJobDetail | GET | /api/email/jobs/{id} | `id` | - | - | JWT | 🔧 | #146 uuid 文案；不存在 → NOT_FOUND_1002「任务不存在」 | L62 |
| 16 | emailApi | getEmailTrace | GET | /api/email/lists/{id}/tracking | `id` | - | - | JWT | 🔧 | #147 不存在 → 404「邮件列表不存在」（原笼统） | L66 |
| 17 | emailApi | deleteEmailList | DELETE | /api/email/list/{id} | `id` | - | - | JWT | 🔧 | uuid 非法 → 400「无效的列表 ID（需 UUID）」；不存在 → 404「邮件列表不存在」 | L70 |

### faq.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | list | GET | `/api/faqs` | - | - | params | JWT | 🔧 | #113 查询失败改「获取 FAQ 列表失败」（原直出 err.Error()）；`kb_id` 过滤显式 400 拒绝；分页回显 page/page_size 正常 |
| 2 | - | get | GET | `/api/faqs/{id}` | id | - | - | JWT | 🔧 | #113 不存在 → 404「FAQ 不存在」、其余错误 → 500「获取 FAQ 失败」（原任何错都 404） |
| 3 | - | create | POST | `/api/faqs` | - | data | - | JWT | 🔧 | #113 绑定文案补齐 `agent_id`（必填项：question/answer/agent_id） |
| 4 | - | update | PUT | `/api/faqs/{id}` | id | data | - | JWT | 🔧 | #113 绑定与失败文案去 err.Error() |
| 5 | - | toggle | PATCH | `/api/faqs/{id}/toggle` | id | {enabled} | - | JWT | 🔧 | #113 绑定文案「enabled 必填（true/false）」；实测 false 生效 |
| 6 | - | remove | DELETE | `/api/faqs/{id}` | id | - | - | JWT | 🔧 | #152 仓储 Delete 校 RowsAffected → 重复删 404「FAQ 不存在」（原回「删除成功」） |
| 7 | - | match | POST | `/api/faqs/match` | - | data | - | JWT | 🔧 | #113 绑定文案改为 `msg`+`agent_id`（原写 query，字段名不符） |
| 8 | - | stats | GET | `/api/faqs/stats` | - | - | - | JWT | 🔧 | #151 改用 COUNT 精确统计（原 ListEnabled 上限 10000 截断且 total=enabled）；实测 13393/13393 |

### featureFlags.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/feature-flags` | - | - | params | JWT | ✅ | 列表 `{list,total}`；不回显 page/page_size；off 单租户不过滤（既定） |
| 2 | - | - | GET | `/api/feature-flags/stale` | - | - | - | JWT | ✅ | 14 天无评估/无修改的开关（`FeatureFlagStaleDays`） |
| 3 | - | - | GET | `/api/feature-flags/{id}` | - | - | - | JWT | 🔧 | #153 不存在时走通用文案「记录不存在」（应为「功能开关不存在（id/key=…）」）；支持数字 ID 与 key |
| 4 | - | - | POST | `/api/feature-flags` | - | data | - | JWT | 🔧 | #127 绑定失败文案中文化（key 与 name 必填）；实测创建 200 且 created_by=62 |
| 5 | - | - | PUT | `/api/feature-flags/{id}` | - | data | - | JWT | 🔧 | #127 绑定文案中文化；实测 name/description/rollout_percentage 全部生效 |
| 6 | - | - | DELETE | `/api/feature-flags/{id}` | - | - | - | JWT | 🔧 | #153 删除成功 `{deleted:true}`，重复删 404 但文案为通用「记录不存在」 |
| 7 | - | - | POST | `/api/feature-flags/{id}/enable` | - | - | - | JWT | 🔧 | #127 绑定文案中文化；实测 enabled=true 且写审计（action=enable） |
| 8 | - | - | POST | `/api/feature-flags/{id}/disable` | - | - | - | JWT | 🔧 | #127 绑定文案中文化；实测 enabled=false 且写审计 |
| 9 | - | - | POST | `/api/feature-flags/{id}/rollout` | - | {percentage} | - | JWT | ✅ | 0-100 越界 → 400「rollout_percentage 必须在 0-100」；合法值生效并写审计 detail=rollout=N% |
| 10 | - | - | GET | `/api/feature-flags/{id}/audit` | - | - | - | JWT | ✅ | 审计列表（flag_key/action/actor_id/detail/created_at），limit 100 |
| 11 | - | - | GET | `/api/feature-flags/{id}/eval-log` | - | - | - | JWT | ✅ | 评估日志 `{list,total}`，limit 100 |

### feishu.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | listAccounts | - | GET | `/api/feishu/accounts` | - | - | params | JWT(Admin) | 🔧 | 22 处 err.Error() 收敛；data 为裸数组；app_secret/encrypt_key 只回掩码；on 模式返回空数组（租户隔离） |
| 2 | getAccount | - | GET | `/api/feishu/accounts/{id}` | - | - | - | JWT(Admin) | 🔧 | 不存在 → 404「飞书账号不存在」；非数字 → 400「无效的账号 ID」 |
| 3 | createAccount | - | POST | `/api/feishu/accounts` | - | data | - | JWT(Admin) | 🔧 | 必填 account_name/app_id/app_secret；默认 status=1、owner=当前用户；绑定错 → 400 中文 |
| 4 | updateAccount | - | PUT | `/api/feishu/accounts/{id}` | - | data | - | JWT(Admin) | 🔧 | 不存在 → 404；密钥留空保留原值；掩码保持 `****` |
| 5 | deleteAccount | - | DELETE | `/api/feishu/accounts/{id}` | - | - | - | JWT(Admin) | 🔧 | 重复删 → 404 |
| 6 | testSend | - | POST | `/api/feishu/accounts/{id}/test-send` | - | data | - | JWT(Admin) | 🔧 | 必填 `receive_id`（`msg_type` 可选）；假凭据 → 502 UPSTREAM_FAILURE_5005「发送飞书测试消息失败：…」 |
| 7 | refreshToken | - | POST | `/api/feishu/accounts/{id}/refresh-token` | - | - | - | JWT(Admin) | 🔧 | 假凭据 → 502「刷新飞书 Token 失败：code=10003 msg=invalid param」 |

> 全部 7 条都挂 `AdminAuthMiddleware` + `guardChannelAccountOwnership` 归属守卫；错误出口已无 `err.Error()` 直出。

### followup.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | today | - | GET | `/api/followups/today` | - | - | owner_id, date | JWT | 🔧 | #144 参数文案细化：缺 owner_id →「owner_id 必填（管理员需显式指定跟进负责人）」；date 非法 →「date 格式非法：需为 YYYY-MM-DD」。**需 `FF_LTC_ORDER_DRAFT_DB=on`（+`FF_LTC_SALES_TRIGGER=true`）才装配，否则 503「跟进服务未装配」为设计形态** |
| 2 | pending | - | GET | `/api/followups/pending` | - | - | owner_id, limit | JWT | 🔧 | #144 limit 默认 50、1-500，越界/非法 →「limit 必须是 1-500 之间的整数」；实测 `{data:{list:[]}}` |
| 3 | overdue | - | GET | `/api/followups/overdue` | - | - | owner_id | JWT | 🔧 | #144 缺 owner_id → 400 文案；实测 200 `{data:{list:[]}}` |
| 4 | complete | - | POST | `/api/followups/{id}/complete` | - | `{result,note}` | - | JWT | 🔧 | #144 result 白名单校验文案（contacted/interested/quoted/converted/rejected/lost/no_response）；不存在 → 404「提醒 fu_nope 不存在」；重复完成 → 409 带「已处理」 |
| 5 | cancel | - | POST | `/api/followups/{id}/cancel` | - | - | - | JWT | 🔧 | #144 id 非数字 →「id 必须是数字跟进 ID」；不存在 → 404「提醒 fu_nope 不存在」 |

> 备注：本模块 5 条都先判 `svc == nil` → 503；旗子开启后已逐条实测（文案、404、白名单均正确），测完已恢复默认 off。

### geo.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/geo/keywords/mine` | `data` | data | - | JWT | ✅ 通过 | 挖掘成功（测试词已软删清理） |
| 2 | - | - | POST | `/api/geo/keywords/expand` | `data` | data | - | JWT | ✅ 通过 | 200；LLM 未配置时 data:null（降级） |
| 3 | - | - | POST | `/api/geo/keywords/cluster` | `data` | data | - | JWT | ⛔ 阻塞 | 500「话题集群失败」：本环境无可用 LLM（集群依赖模型）；错误文案被终态掩码，记二次审核 |
| 4 | - | - | GET | `/api/geo/keywords/list` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 5 | - | - | POST | `/api/geo/keyword-mining/crawl-suggest` | `data` | data | - | JWT | ✅ 通过 | 必填 seeds[]；外网建议源不可达时回 errors[] 且仍返回本地结果（部分成功语义正确） |
| 6 | - | - | POST | `/api/geo/keyword-mining/longtail` | `data` | data | - | JWT | ✅ 通过 | 必填 seeds[]；返回长尾词并落库 |
| 7 | - | - | GET | `/api/geo/keyword-mining/funnel` | - | - | - | JWT | ✅ 通过 | seed/related/suggest/longtail 漏斗计数 |
| 8 | - | - | POST | `/api/geo/push/urls` | `data` | data | - | JWT | ✅ 通过 | 契约 {urls[],platforms[]}；无 site_id 概念（全局推送），返回 count |
| 9 | - | - | GET | `/api/geo/push/quota` | - | - | - | JWT | ✅ 通过 | sitemap registered/remain/used |
| 10 | - | - | GET | `/api/geo/push/sitemap` | - | - | - | JWT | ✅ 通过 | 生成 XML sitemap |
| 11 | - | - | POST | `/api/geo/index-tracking/verify/{articleId}` | `articleId` | - | - | JWT | ✅ 通过 | 不存在文章 → 404「文章不存在」 |
| 12 | - | - | GET | `/api/geo/index-tracking/funnel` | - | - | - | JWT | ✅ 通过 | article/deposit 漏斗 |
| 13 | - | - | POST | `/api/geo/index-tracking/verify-all` | - | - | - | JWT | ✅ 通过 | {started:true} 异步批量 |
| 14 | - | - | POST | `/api/geo/site/export` | `data` | data | - | JWT | ✅ 通过 | {exported:N} |
| 15 | - | - | POST | `/api/geo/site/deploy` | `data` | data | - | JWT | ✅ 通过 | 站点目录非 git → 400 可操作提示（环境配置） |
| 16 | - | - | POST | `/api/geo/site/full-pipeline` | `data` | data | - | JWT | ✅ 通过 | 同上 400 提示 |
| 17 | - | - | GET | `/api/geo/site/llms-txt-preview` | - | - | - | JWT | ✅ 通过 | 返回 llms.txt 内容 |
| 18 | - | - | GET | `/api/geo/site/robots-preview` | - | - | - | JWT | ✅ 通过 | 返回 robots.txt |
| 19 | - | - | GET | `/api/geo/sites` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 20 | - | - | POST | `/api/geo/sites` | `data` | data | - | JWT | ✅ 通过 | 创建成功（id=UUID） |
| 21 | - | - | PUT | `/api/geo/sites/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 22 | - | - | DELETE | `/api/geo/sites/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 23 | - | - | GET | `/api/geo/pushers` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 24 | - | - | POST | `/api/geo/pushers` | `data` | data | - | JWT | ✅ 通过 | 必填 platform + config_json（必须是 JSON 对象） |
| 25 | - | - | PUT | `/api/geo/pushers/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 26 | - | - | DELETE | `/api/geo/pushers/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 27 | - | - | GET | `/api/geo/schema-templates` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 28 | - | - | POST | `/api/geo/schema-templates` | `data` | data | - | JWT | ✅ 通过 | 必填 page_type/schema_type/template_json |
| 29 | - | - | PUT | `/api/geo/schema-templates/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 30 | - | - | DELETE | `/api/geo/schema-templates/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 31 | - | - | DELETE | `/api/geo/keywords/{id}` | `id` | - | - | JWT | ✅ 通过 | 软删成功 |
| 32 | - | - | POST | `/api/geo/content/generate` | `data` | data | - | JWT | ✅ 通过 | 生成文章（模板兜底） |
| 33 | - | - | POST | `/api/geo/content/batch-generate` | `data` | data | - | JWT | ✅ 通过 | {total,succeeded,failed,items} |
| 34 | - | - | POST | `/api/geo/content/score` | `data` | data | - | JWT | ✅ 通过 | improvements/mo… 评分 |
| 35 | - | - | POST | `/api/geo/content/optimize` | `data` | data | - | JWT | ✅ 通过 | 优化成功（新文章行） |
| 36 | - | - | POST | `/api/geo/content/eeat` | `data` | data | - | JWT | ✅ 通过 | model=template_v1 降级增强 |
| 37 | - | - | POST | `/api/geo/content/schema` | `data` | data | - | JWT | ✅ 通过 | 生成 schema.org 结构 |
| 38 | - | - | POST | `/api/geo/content/uniqueness` | `data` | data | - | JWT | ✅ 通过 | model=regex_v1，originality_score |
| 39 | - | - | GET | `/api/geo/content/list` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 40 | - | - | GET | `/api/geo/content/{id}` | `id` | - | - | JWT | ✅ 通过 | 文章详情 |
| 41 | - | - | POST | `/api/geo/verification/verify` | `data` | data | - | JWT | ✅ 通过 | 无可用云端模型 → 400 可操作提示 |
| 42 | - | - | POST | `/api/geo/verification/negative` | `data` | data | - | JWT | ✅ 通过 | heuristic 降级且显式告警「LLM 不可用」 |
| 43 | - | - | GET | `/api/geo/verification/results/{articleId}` | `articleId` | - | - | JWT | ✅ 通过 | 空数组为合法结果 |
| 44 | - | - | GET | `/api/geo/reports/summary` | `params` | - | params | JWT | ✅ 通过 | 汇总报表 |
| 45 | - | - | GET | `/api/geo/reports/roi` | `params` | - | params | JWT | ✅ 通过 | ROI 分析 |
| 46 | - | - | GET | `/api/geo/reports/api-costs` | `params` | - | params | JWT | ✅ 通过 | 按 provider 成本 |
| 47 | - | - | GET | `/api/geo/config` | - | - | - | JWT | ✅ 通过 | 单例配置（competitors 等以字符串存） |
| 48 | - | - | PUT | `/api/geo/config` | `data` | data | - | JWT | ✅ 通过 | 契约与 GET 不对称：入参是 brand（非 brand_name）+ competitors[] 数组；按前端形状提交成功（记二次审核） |
| 49 | - | - | POST | `/api/geo/config/optimize` | `data` | data | - | JWT | ⛔ 阻塞 | 500「配置优化失败」：依赖 LLM（本环境无可用模型）；文案被掩码，记二次审核 |

### geoAlert.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/geo/alerts` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/geo/alerts/unread-count` | - | - | - | JWT | ✅ 通过 | {count} |
| 3 | - | - | POST | `/api/geo/alerts/{id}/ack` | `id` | {} | - | JWT | ✅ 通过 | 「已确认」 |
| 4 | - | - | DELETE | `/api/geo/alerts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功（未在本轮制造新告警，复用既有记录做 ack 验证） |

### geoAutopilot.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/geo/autopilot/runs` | `data` | data | - | JWT | ✅ 通过 | 必填 keyword；返回 run（status=running） |
| 2 | - | - | GET | `/api/geo/autopilot/runs/{id}` | `id` | - | - | JWT | ✅ 通过 | 运行详情 |
| 3 | - | - | GET | `/api/geo/autopilot/runs` | `params` | - | params | JWT | ✅ 通过 | {list,total} |

### geoDecision.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/geo/decision/report` | - | - | - | JWT | ✅ 通过 | tasks_pending/tasks_done/leads_captured |
| 2 | - | - | GET | `/api/geo/decision/tasks` | `limit` | - | limit | JWT | ✅ 通过 | {list,total} |
| 3 | - | - | POST | `/api/geo/decision/tasks/{id}/done` | `id` | - | - | JWT | ✅ 通过 | 不存在 → 404「补位任务不存在」 |

### geoEntity.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/geo/entity/list` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/geo/entity/{entityId}/graph` | `entityId` | - | - | JWT | ✅ 通过 | depth/entities 关系图 |
| 3 | - | - | POST | `/api/geo/entities/extract` | `docId` | {doc_id} | - | JWT | ✅ 通过 | 文档不存在 → 400 BUSINESS_5001「加载知识库文档失败: record not found」（可定位） |
| 4 | - | - | GET | `/api/geo/kb/documents` | `keyword, sourceLevel` | - | params | JWT | ✅ 通过 | 数组 |
| 5 | - | - | POST | `/api/geo/kb/documents` | `data` | data | - | JWT | ✅ 通过 | 保存成功（测试文档已删） |
| 6 | - | - | DELETE | `/api/geo/kb/documents/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 7 | - | - | GET | `/api/geo/kb/documents/{id}` | `id` | - | - | JWT | ✅ 通过 | 文档详情 |
| 8 | - | - | GET | `/api/geo/kb/search` | `q, limit` | - | params | JWT | ✅ 通过 | 检索命中 |
| 9 | - | - | POST | `/api/geo/kb/ask` | `question` | data | - | JWT | ✅ 通过 | 无相关资料时回可操作文案（不编造） |
| 10 | - | - | GET | `/api/geo/workflow/workflows` | - | - | - | JWT | ✅ 通过 | 数组 |
| 11 | - | - | GET | `/api/geo/workflow/workflows/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 12 | - | - | POST | `/api/geo/workflow/workflows` | `data` | data | - | JWT | ✅ 通过 | 必填 name + steps[]（nodes/edges 不被识别） |
| 13 | - | - | PUT | `/api/geo/workflow/workflows/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 14 | - | - | DELETE | `/api/geo/workflow/workflows/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 15 | - | - | POST | `/api/geo/workflow/workflows/{id}/run` | `id` | data | - | JWT | ✅ 通过 | 返回 execution；**code:0 但 message「工作流执行失败」、status=failed**（未知 step type），成功信封+失败语义，记二次审核 |
| 16 | - | - | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | `workflowId` | - | - | JWT | ✅ 通过 | 执行记录数组 |
| 17 | - | - | GET | `/api/geo/workflow/templates` | - | - | - | JWT | ✅ 通过 | 数组 |
| 18 | - | - | POST | `/api/geo/workflow/templates` | `data` | data | - | JWT | ✅ 通过 | 必填 name+steps[]（测试模板已删） |

### geoPlatform.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/geo/platform/platforms` | - | - | - | JWT | ✅ 通过 | 平台能力清单 |
| 2 | - | - | GET | `/api/geo/platform/accounts` | - | - | - | JWT | ✅ 通过 | {list} |
| 3 | - | - | POST | `/api/geo/platform/accounts` | `data` | data | - | JWT | ✅ 通过 | github_blog 必须提供 credentials.access_token（缺 → 明确提示） |
| 4 | - | - | DELETE | `/api/geo/platform/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 5 | - | - | POST | `/api/geo/platform/publish` | `data` | data | - | JWT | ✅ 通过 | 文章不存在 → 400 BUSINESS_5001「文章不存在」 |
| 6 | - | - | GET | `/api/geo/platform/records` | `params` | - | params | JWT | ✅ 通过 | {list} 发布记录 |

### geoProbe.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/geo/probe/engines` | - | - | - | JWT | ✅ 通过 | 本环境未配置引擎 → [] |
| 2 | - | - | POST | `/api/geo/probe/test` | `data` | data | - | JWT | ✅ 通过 | 未注册引擎 → 400「engine … not found (available: )」 |
| 3 | - | - | GET | `/api/geo/probe/runs` | `params` | - | params | JWT | ✅ 通过 | {list} 历史探测 |
| 4 | - | - | POST | `/api/geo/probe/run-negative` | `data` | data | - | JWT | ✅ 通过 | {started:true} 异步 |
| 5 | - | - | POST | `/api/geo/probe/run-source-sync` | `data` | data | - | JWT | ✅ 通过 | {started:true} 异步 |
| 6 | - | - | POST | `/api/geo/probe/all` | `data` | data | - | JWT | ✅ 通过 | 无引擎时返回 summary 全 0（不报错） |
| 7 | - | - | GET | `/api/geo/sov` | - | - | - | JWT | ✅ 通过 | 品牌提及份额 |
| 8 | - | - | GET | `/api/geo/crawler-stats` | - | - | - | JWT | ✅ 通过 | summary 访问/词/引擎计数 |
| 9 | - | - | POST | `/api/geo/crawler/run` | `data` | data | - | JWT | ✅ 通过 | {status:started} |
| 10 | - | - | POST | `/api/geo/inaccurate-claims` | `data` | data | - | JWT | ⛔ 阻塞 | LLM 全部熔断 → 500 INTERNAL_ERROR_6002，文案含原因但状态码应为 503/502；记二次审核 |
| 11 | - | - | GET | `/api/geo/source-catalog/levels` | `url` | - | url | JWT | ✅ 通过 | {level,source}（未收录时 level 空串） |
| 12 | - | - | GET | `/api/geo/competitors` | - | - | - | JWT | ✅ 通过 | 数组 |
| 13 | - | - | GET | `/api/geo/competitors/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 14 | - | - | POST | `/api/geo/competitors` | `data` | data | - | JWT | ✅ 通过 | 创建成功（category/priority 默认值） |
| 15 | - | - | PUT | `/api/geo/competitors/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 16 | - | - | DELETE | `/api/geo/competitors/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 17 | - | - | GET | `/api/geo/visibility/engine-compare` | `days, intent` | - | params | JWT | ✅ 通过 | summary 对比窗口 |
| 18 | - | - | GET | `/api/geo/sov/trend` | `days, intent` | - | params | JWT | ✅ 通过 | current/previous 序列 |

### glossary.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | createGlossary | - | POST | `/api/glossaries` | - | data | - | JWT | 🔧 | 必填 term_id/category/translations（map）；绑定文案已中文化；错误不再直出 err.Error() |
| 2 | listGlossaries | - | GET | `/api/glossaries` | - | - | params | JWT | ✅ | `{list,page,page_size,total}` 分页回显正确；on 8206 → 4301（module_key=i18n） |
| 3 | getGlossary | - | GET | `/api/glossaries/{termId}` | termId | - | - | JWT | ✅ | 路径参数是 **term_id 字符串**（传数字主键 → 404「术语不存在」）；不存在 → 404「术语不存在」 |
| 4 | updateGlossary | - | PUT | `/api/glossaries/{termId}` | termId | data | - | JWT | 🔧 | #153 更新后改为回读，响应 created_at 不再为 0001-01-01 |
| 5 | deleteGlossary | - | DELETE | `/api/glossaries/{termId}` | termId | - | - | JWT | 🔧 | #154 重复删除 → 404「术语不存在」（原为 404 码 + 文案「删除术语失败」） |
| 6 | validateGlossary | - | POST | `/api/glossaries/validate` | - | data | - | JWT | ✅ | `{original_text,corrected_text,issues[]}`；校验文案不变 |

### humanTask.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/human-tasks` | `params` | - | kind/status/assignee/page/page_size | JWT | 🔧 | #155 收敛哨兵前缀；`kind`/`status` 支持重复键，`assignee=me` 由后端按登录身份解析；非法 kind → 400 |
| 2 | - | - | GET | `/api/human-tasks/counts` | - | - | - | JWT | 🔧 | #155 返回 `{at,total_open,by_kind:[{kind,open,overdue,sla_column}]}`；on 8206 隔离后 total_open=0 |
| 3 | - | - | GET | `/api/human-tasks/{id}` | `id` | - | - | JWT | ✅ | 不存在 → 404「待办不存在（id=…）」 |
| 4 | - | - | POST | `/api/human-tasks/{id}/claim` | `id` | - | - | JWT | 🔧 | #155 不存在 → 404（原带 `human_task:` 前缀）；审批类不可认领 → 409 |
| 5 | - | - | POST | `/api/human-tasks/{id}/release` | `id` | - | - | JWT | 🔧 | #155 同上，前缀已清洗 |
| 6 | - | - | POST | `/api/human-tasks/{id}/complete` | `id` | data | - | JWT | 🔧 | #155 同上；完成会联动审批/会话主体 |
| 7 | - | - | POST | `/api/human-tasks/{id}/cancel` | `id` | {reason} | - | JWT | 🔧 | #155 缺 reason → 400「撤销必须给 reason（…）」；前缀已清洗 |

### i18nStats.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/i18n/stats` | - | - | - | JWT | ✅ | 实测 `{days:7,total_calls:0,cache_hit_rate:0,fallback_rate:0,glossary_lang_count:2,…}`（本机无 i18n 调用日志，计数为 0 属真实无数据） |
| 2 | - | - | GET | `/api/i18n/stats/lang-dist` | - | - | days | JWT | ✅ | 实测 `{list:null,total:0}`；**观察项**：空结果回 `null` 而非 `[]`（与其它列表契约不一致，前端已按空处理，未改） |
| 3 | - | - | GET | `/api/i18n/stats/cache` | - | - | days | JWT | ✅ | 实测 `{cache_hit_rate:0,days:7}` |
| 4 | - | - | GET | `/api/i18n/stats/glossary` | - | - | - | JWT | ✅ | 实测 `{list:[{target_lang:"en",term_count:2,active_count:2},{target_lang:"zh",term_count:1,active_count:1}],total:2}` |
| 5 | - | - | GET | `/api/i18n/stats/quality` | - | - | days | JWT | ✅ | 实测 `{list:null,total:0}`；同 #2 的 null 观察项 |
| 6 | - | - | GET | `/api/i18n/stats/latency` | - | - | days | JWT | ✅ | 实测 `{list:null,total:0}`；`days=0/负数` 不报错（静默接受，观察项） |

> 说明：6 条均挂 JWT；on 模式（SaaS）未订阅 `i18n` 模块的租户统一回 **4301**（`module_key: "i18n"`），属模块门禁设计。本节仅记录观察项，无代码改动。

### inbox.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/inbox/conversations` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/inbox/conversations/{id}` | `id` | - | - | JWT | ✅ 通过 | 会话详情 |
| 3 | - | - | GET | `/api/inbox/conversations/{id}/messages` | `id, params` | - | params | JWT | ✅ 通过 | {list} 消息 |
| 4 | - | - | POST | `/api/inbox/conversations/{id}/read` | `id` | - | - | JWT | ✅ 通过 | 标记已读 |
| 5 | - | - | POST | `/api/inbox/conversations/{id}/pin` | `id` | {pinned} | - | JWT | ✅ 通过 | 置顶/取消均生效 |
| 6 | - | - | POST | `/api/inbox/conversations/{id}/star` | `id` | {starred} | - | JWT | ✅ 通过 | 星标生效 |
| 7 | - | - | POST | `/api/inbox/conversations/{id}/mute` | `id` | {muted} | - | JWT | ✅ 通过 | 静音生效 |
| 8 | - | - | POST | `/api/inbox/conversations/{id}/tags` | `id` | {tag} | - | JWT | ✅ 通过 | 添加成功 |
| 9 | - | - | DELETE | `/api/inbox/conversations/{id}/tags/{tag}` | `id, tag` | - | - | JWT | ✅ 通过 | 移除成功 |
| 10 | - | - | DELETE | `/api/inbox/conversations/{id}/messages/{mid}` | `id, mid, source` | - | source=hub|session | JWT | 🔧 已修复 | 删不存在的消息曾回 200「删除成功」并回显该 id（假成功）→ 仓储命中 0 行改报 ErrRecordNotFound，控制器明确 404「消息不存在」；回归 TestMessageHubRepository_Delete_MissingReportsNotFound |
| 11 | - | - | POST | `/api/inbox/assign` | `data` | data | - | JWT | ✅ 通过 | 必填 conversation_id/action/to_type(human|sop|ai)；非法 to_type → 400「invalid assign_to」 |
| 12 | - | - | POST | `/api/inbox/assign/auto` | `data` | {conversation_id,candidates} | - | JWT | ✅ 通过 | 分配成功 |
| 13 | - | - | GET | `/api/inbox/assignments` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 14 | - | - | GET | `/api/inbox/stats` | - | - | - | JWT | ✅ 通过 | total/unread/open/assigned/by_platform |
| 15 | - | - | POST | `/api/inbox/reconcile` | `params` | - | mode=unread|overdue|backfill | JWT | ✅ 通过 | 非法 mode → 400 列出合法值；unread 重算生效 |
| 16 | - | - | GET | `/api/inbox/staff/{staff}/load` | `staff` | - | - | JWT | ✅ 通过 | {load,staff} |
| 17 | - | - | GET | `/api/inbox/agents` | - | - | - | JWT | ✅ 通过 | {list} 可分配坐席（四列脱敏） |

### integration.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/integrations` | `params` | - | params | JWT | ✅ 通过 | 数组 |
| 2 | - | - | GET | `/api/integrations/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条（密钥回显为原值/掩码按实现） |
| 3 | - | - | POST | `/api/integrations` | `data` | data | - | JWT | ✅ 通过 | 必填 platform；account_name/api_key/api_secret/config |
| 4 | - | - | PUT | `/api/integrations/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 指针字段「出现才改」，避免把掩码写回库 |
| 5 | - | - | DELETE | `/api/integrations/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/integrations/{id}/sync-customers` | `id` | - | - | JWT | 🔧 已修复 | 不支持的平台曾回 500「操作失败，请稍后重试」（裸 errors.New 被终态掩码）→ 改 bizerr 客户端错误：400 且指名可用平台；回归 TestIntegrationSync_UnsupportedPlatformIsClientError |
| 7 | - | - | POST | `/api/integrations/{id}/sync-products` | `id` | - | - | JWT | 🔧 已修复 | 同上（商品同步） |
| 8 | - | - | GET | `/api/integration/sync-logs` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 9 | - | - | GET | `/api/integration/external-customers` | - | - | - | JWT | ✅ 通过 | {list} |
| 10 | - | - | GET | `/api/integration/external-orders` | - | - | - | JWT | ✅ 通过 | {list} |
| 11 | - | - | GET | `/api/integration/external-orders-by-customer` | `params` | - | phone,name | JWT | ✅ 通过 | {count,list} |
| 12 | - | - | GET | `/api/integration/external-products` | - | - | - | JWT | ✅ 通过 | {list} |
| 13 | - | - | POST | `/api/integrations/{id}/test` | `id` | - | - | JWT | ✅ 通过 | 连接测试成功 |

### intentRecognition.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | intentApi | recognize | POST | `/api/intent/recognize` | `data` | data | - | JWT | ✅ 通过 | text/message 双字段；空文本 400；本环境引擎 enabled=false → method=disabled 返回 unknown/0.3（环境配置非缺陷） |
| 2 | intentApi | batchRecognize | POST | `/api/intent/recognize/batch` | `data` | data | - | JWT | ✅ 通过 | messages 数组逐条返回结果 |
| 3 | intentApi | getStats | GET | `/api/intent/stats` | `params` | - | params | JWT | ✅ 通过 | by_intent/by_level/by_method/distribution/period_days=7 |
| 4 | intentApi | getRecent | GET | `/api/intent/recent` | `params` | - | params | JWT | ✅ 通过 | list 含历史识别记录 |
| 5 | intentApi | getDict | GET | `/api/intent/dict` | - | - | - | JWT | ✅ 通过 | 意图字典（type/name/keywords/examples/description） |
| 6 | intentApi | recognizeFine | POST | `/api/intent/recognize/fine` | `data` | data | - | JWT | ✅ 通过 | 必填字段是 `message`（非 text），缺字段 400；返回 major/minor/confidence/method |
| 7 | intentApi | getLogs | GET | `/api/intent/logs` | `params` | - | params | JWT | ✅ 通过 | customer_id/major/limit 过滤 |
| 8 | intentApi | getStatsFine | GET | `/api/intent/stats/fine` | `params` | - | params | JWT | ✅ 通过 | by_major/by_minor/by_method/days=7 |
| 9 | intentApi | getConfig | GET | `/api/intent/config` | - | - | - | JWT | ✅ 通过 | {enabled,updated_at,updated_by} |
| 10 | intentApi | updateConfig | PUT | `/api/intent/config` | `data` | data | - | JWT | ✅ 通过 | 回写 enabled:false 成功且 GET 同步；空 body 400 |
| 11 | intentApi | getKeywordOverride | GET | `/api/intent-records/keywords-override` | - | - | - | JWT | ✅ 通过 | 返回覆盖词表 map |
| 12 | intentApi | updateKeywordOverride | PUT | `/api/intent-records/keywords-override` | `data` | data | - | JWT | ✅ 通过 | 契约 `{"override":{type:[words]}}`；类型违例 400；未知类型键被过滤；写入即热生效（测后已清空还原） |

### kbConnector.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | kbConnectorApi | list | GET | `/api/kb-connectors` | - | - | - | JWT | ✅ 通过 | list 含 config_masked（配置脱敏）/last_status/last_summary |
| 2 | kbConnectorApi | create | POST | `/api/kb-connectors` | `data` | data | - | JWT | ✅ 通过 | type 注册制（ima/webhttp）；未知 type → 400 且列出可用类型 |
| 3 | kbConnectorApi | update | PUT | `/api/kb-connectors/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 已更新 ok:true |
| 4 | kbConnectorApi | remove | DELETE | `/api/kb-connectors/{id}` | `id` | - | - | JWT | ✅ 通过 | 已删除；note 提示已导入文档保留在知识库 |
| 5 | kbConnectorApi | sync | POST | `/api/kb-connectors/{id}/sync` | `id` | - | - | JWT | 🔧 已修复 | 上游 404 时错误文案曾整段回显响应体（HTML/远端内容泄漏，webhttp.go:55 与 ima.go:47 同族）→ 改为只报状态码+HTML 识别提示；回归测试 TestWebhttpProviderNon200DoesNotEchoUpstreamBody |
| 6 | capabilitiesApi | get | GET | `/api/capabilities` | - | - | - | JWT | ✅ 通过 | 能力矩阵 {platform,capability,via,note,implemented,configured,ready} |

### kbCrawler.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | kbCrawlerApi | listSources | GET | `/api/kb/crawler/sources` | `params` | - | params | JWT | ✅ 通过 | kb_id 过滤；含 site_verify_token/site_verified/last_status |
| 2 | kbCrawlerApi | createSource | POST | `/api/kb/crawler/sources` | `data` | data | - | JWT | ✅ 通过 | 创建即发验证 token；提示先验证再同步 |
| 3 | kbCrawlerApi | updateSource | PUT | `/api/kb/crawler/sources/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 部分更新 max_pages 生效 |
| 4 | kbCrawlerApi | pauseSource | POST | `/api/kb/crawler/sources/{id}/pause` | `id` | - | - | JWT | ✅ 通过 | paused |
| 5 | kbCrawlerApi | resumeSource | POST | `/api/kb/crawler/sources/{id}/resume` | `id` | - | - | JWT | ✅ 通过 | active |
| 6 | kbCrawlerApi | verifySite | POST | `/api/kb/crawler/sources/{id}/verify` | `id` | - | - | JWT | ✅ 通过 | 未放置验证文件 → 400 可操作提示（无上游内容回显） |
| 7 | kbCrawlerApi | deleteSource | DELETE | `/api/kb/crawler/sources/{id}` | `id` | - | - | JWT | ✅ 通过 | 已删除 |
| 8 | kbCrawlerApi | syncSource | POST | `/api/kb/crawler/sources/{id}/sync` | `id` | - | - | JWT | ✅ 通过 | 未验证源 400 拦截；已验证源排队 {batch_no,status:pending}；批次 succeeded found=2 |
| 9 | kbCrawlerApi | listBatches | GET | `/api/kb/crawler/batches` | `params` | - | params | JWT | ✅ 通过 | source_id/page 过滤；pages_found/new/failed 计数完整 |
| 10 | kbCrawlerApi | retryBatch | POST | `/api/kb/crawler/batches/{id}/retry` | `id` | - | - | JWT | ✅ 通过 | 仅失败批次可重试（succeeded → 400）；重试产生新批次（原批不动；trigger 标 cron 略怪，非缺陷） |
| 11 | kbQaCandidateApi | list | GET | `/api/kb/qa/candidates` | `params` | - | params | JWT | ✅ 通过 | source_type/status 过滤 {items} |
| 12 | kbQaCandidateApi | approve | POST | `/api/kb/qa/candidates/{id}/approve` | `id` | - | - | JWT | ✅ 通过 | 状态机 pending_review→approved；非法跃迁 409（初态是 pending_review 非 pending） |
| 13 | kbQaCandidateApi | reject | POST | `/api/kb/qa/candidates/{id}/reject` | `id` | - | - | JWT | ✅ 通过 | →rejected；不存在候选 409「候选不存在」 |
| 14 | kbQaCandidateApi | publish | POST | `/api/kb/qa/candidates/{id}/publish` | `id` | - | - | JWT | ✅ 通过 | approved→indexed，落 kb_documents(.qa) 并启动向量化；未 approve 409 |

### knowledge.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | knowledgeAPI | importUpload | POST | `/api/knowledge/import/upload` | `formData` | formData | - | JWT | ✅ 通过 | multipart 上传；doc 545 落库并向量化（测后已删） |
| 2 | knowledgeAPI | importText | POST | `/api/knowledge/import/text` | `data` | data | - | JWT | ✅ 通过 | 必填 product_id/content；缺参 400（validator 原文透传，全仓绑定层同口径）；doc 544 异步向量化完成 |
| 3 | knowledgeAPI | importURL | POST | `/api/knowledge/import/url` | `data` | data | - | JWT | ✅ 通过 | 上游 404 → 400「URL 返回错误状态: 404」（无响应体回显） |
| 4 | knowledgeAPI | listDocuments | GET | `/api/knowledge/documents` | `params` | - | params | JWT | ✅ 通过 | {items,total} 分页 |
| 5 | knowledgeAPI | getDocument | GET | `/api/knowledge/documents/{id}` | `id, params` | - | params | JWT | ✅ 通过 | 全量行 |
| 6 | knowledgeAPI | getDocumentProgress | GET | `/api/knowledge/documents/{id}/progress` | `id` | - | - | JWT | ✅ 通过 | chunk_count/embed_progress=100/embed_status=indexed |
| 7 | knowledgeAPI | getDocumentChunks | GET | `/api/knowledge/documents/{id}/chunks` | `id` | - | - | JWT | ✅ 通过 | {items} 分块内容 |
| 8 | knowledgeAPI | updateDocument | PUT | `/api/knowledge/documents/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改 title 生效 |
| 9 | knowledgeAPI | deleteDocument | DELETE | `/api/knowledge/documents/{id}` | `id, params` | - | params | JWT | ✅ 通过 | 删除成功且库中 0 行 |
| 10 | knowledgeAPI | reindexDocument | POST | `/api/knowledge/documents/{id}/reindex` | `id, params` | Literal | params | JWT | ✅ 通过 | 重建任务已启动 |
| 11 | knowledgeAPI | rebuildProductIndex | POST | `/api/knowledge/products/{productId}/rebuild-index` | `productId` | - | - | JWT | ✅ 通过 | 重建任务已启动（入队耗时 ~29s，观察项非缺陷） |
| 12 | knowledgeAPI | getProductOverview | GET | `/api/knowledge/products/{productId}/overview` | `productId` | - | - | JWT | ✅ 通过 | 11 项聚合齐全；total_tokens 口径偏小（39 vs 374 chunks）记二次审核 |
| 13 | knowledgeAPI | search | POST | `/api/knowledge/search` | `data` | data | - | JWT | ✅ 通过 | 向量检索命中刚导入文档 |
| 14 | knowledgeAPI | listImportLogs | GET | `/api/knowledge/import-logs` | `params` | - | params | JWT | ✅ 通过 | {items,total}（文本导入不落 import_logs，仅上传/URL 记） |
| 15 | knowledgeAPI | listOpenAPISources | GET | `/api/knowledge/openapi/sources` | `params` | - | params | JWT | ✅ 通过 | {items} 含 auth_config 脱敏 |
| 16 | knowledgeAPI | createOpenAPISource | POST | `/api/knowledge/openapi/sources` | `data` | data | - | JWT | ✅ 通过 | 必填 endpoint（缺 → 400「数据源端点不能为空」） |
| 17 | knowledgeAPI | getOpenAPISource | GET | `/api/knowledge/openapi/sources/{id}` | `id, params` | - | params | JWT | ✅ 通过 | 全量行 |
| 18 | knowledgeAPI | updateOpenAPISource | PUT | `/api/knowledge/openapi/sources/{id}` | `id, data` | data | - | JWT | 🔧 已修复 | 部分更新曾把未携带字段全部清零（数据丢失）；先改「非零值合并」治了丢失但让显式清空静默失效 → 最终改指针补丁三态（nil=不改 / 非 nil 含空串=赋值）；回归 TestOpenAPISourcePartialUpdateKeepsUnsentFields + TestOpenAPISourcePatch_ExplicitEmptyClears；线上复验：只传 name+schedule 保留其它字段、传 schedule: 可清空 |
| 19 | knowledgeAPI | deleteOpenAPISource | DELETE | `/api/knowledge/openapi/sources/{id}` | `id, params` | - | params | JWT | ✅ 通过 | 删除成功且库中 0 行 |
| 20 | knowledgeAPI | syncOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/sync` | `id, params` | Literal | params | JWT | ✅ 通过 | {total_items,imported,skipped,failed,status:success} |
| 21 | knowledgeAPI | testOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/test` | `id, data` | data | - | JWT | 🔧 已修复 | 空 body 曾测空 URL（`unsupported protocol scheme ""`）——控制器只绑 body 不读库；改为 body 无 endpoint 时回落库中该 id 配置（body 显式字段优先，兼容前端保存前试连），仍无 → 400；不存在 id 404 |
| 22 | knowledgeAPI | toggleOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/toggle` | `id, data, params` | data | params | JWT | ✅ 通过 | 切换成功 |
| 23 | knowledgeAPI | getOverviewStats | GET | `/api/knowledge/stats/overview` | `params` | - | params | JWT | ✅ 通过 | 全局聚合（documents/chunks/tokens/searches/hit_rate/latency） |
| 24 | knowledgeAPI | getDocumentStats | GET | `/api/knowledge/stats/documents` | `params` | - | params | JWT | ✅ 通过 | overview + 明细 |
| 25 | knowledgeAPI | getSearchStats | GET | `/api/knowledge/stats/searches` | `params` | - | params | JWT | ✅ 通过 | 同上 |
| 26 | knowledgeAPI | getImportStats | GET | `/api/knowledge/stats/imports` | `params` | - | params | JWT | ✅ 通过 | total/success/failed/rate + daily_trend |
| 27 | knowledgeAPI | getOpenAPIStats | GET | `/api/knowledge/stats/openapi` | `params` | - | params | JWT | ✅ 通过 | total/enabled/failed_sources + source_list |

### knowledgeBase.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/knowledge-bases` | - | - | params | JWT | ✅ 通过 | type/kb_type 双参数过滤、keyword、分页 {list,total} |
| 2 | - | - | GET | `/api/knowledge-bases/by-type/{kbType}` | - | - | params | JWT | ✅ 通过 | 数组返回 |
| 3 | - | - | GET | `/api/knowledge-bases/{id}` | - | - | - | JWT | ✅ 通过 | 全量行 |
| 4 | - | - | POST | `/api/knowledge-bases` | - | data | - | JWT | ✅ 通过 | 缺必填 400；owner_type 缺省 private ⇒ 必须带 owner_agent_id（service 层有精确中文提示，但被 ErrorFromDB fallback 掩码成「操作失败」——文案策略记二次审核）；shared 直建成功 |
| 5 | - | - | PUT | `/api/knowledge-bases/{id}` | - | data | - | JWT | ✅ 通过 | 指针字段部分更新语义正确（未传字段不丢）；落库核对一致 |
| 6 | - | - | DELETE | `/api/knowledge-bases/{id}` | - | - | - | JWT | ✅ 通过 | 软删；不存在 id → 404 |
| 7 | - | - | GET | `/api/knowledge-bases/{id}/stats` | - | - | - | JWT | ✅ 通过 | {agent_count,item_count} |

### knowledgeMerchant.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | knowledgeMerchantAPI | batchImport | POST | `/api/knowledge-merchant/batch/import` | `data` | data | - | JWT | ✅ 通过 | 契约字段 `items`；逐行拒绝带中文错误「第 1 行: 内容为空」 |
| 2 | knowledgeMerchantAPI | batchUpload | POST | `/api/knowledge-merchant/batch/upload` | `formData` | formData | - | JWT | 🔧 已修复 | auto 分派曾把 .txt 落进 json 分支必炸「JSON 解析失败」而前端 accept 明确含 .txt；补 text 分支（整文件单文档，标题=文件名去扩展名）；回归测试 TestParseBatchFileTxtAutoDetect |
| 3 | knowledgeMerchantAPI | playground | POST | `/api/knowledge-merchant/playground` | `data` | data | - | JWT | ✅ 通过 | 检索命中刚导入 chunk；缺 product_id 400 |
| 4 | knowledgeMerchantAPI | listDocumentChunks | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | `documentId, params` | - | params | JWT | ✅ 通过 | {items} 主键字段名是 `id`（非 chunk_id） |
| 5 | knowledgeMerchantAPI | updateChunk | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | `chunkId, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 6 | knowledgeMerchantAPI | deleteChunk | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | `chunkId` | - | - | JWT | ✅ 通过 | 删除成功且库中 0 行 |
| 7 | knowledgeMerchantAPI | splitChunk | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | `chunkId, data` | data | - | JWT | ✅ 通过 | 契约字段 `parts`（string 数组，≥2 段）；拆分后 chunk 数 +1 |
| 8 | knowledgeMerchantAPI | submitFeedback | POST | `/api/knowledge-merchant/feedback` | `data` | data | - | JWT | ✅ 通过 | rating 是 int（传 string 400）；反馈已记录 |
| 9 | knowledgeMerchantAPI | listFeedbacks | GET | `/api/knowledge-merchant/feedbacks` | `params` | - | params | JWT | ✅ 通过 | {items} 含 query_hash |
| 10 | knowledgeMerchantAPI | createToken | POST | `/api/knowledge-merchant/tokens` | `data` | data | - | JWT | ✅ 通过 | 明文仅创建响应显示一次（token_plain）；scopes 默认 read,write |
| 11 | knowledgeMerchantAPI | listTokens | GET | `/api/knowledge-merchant/tokens` | `params` | - | params | JWT | ✅ 通过 | 列表不泄漏明文 |
| 12 | knowledgeMerchantAPI | revokeToken | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | `tokenId` | - | - | JWT | ✅ 通过 | 已吊销；撤销后外部导入 401 生效 |
| 13 | knowledgeMerchantAPI | externalImport | POST | `/api/knowledge-merchant/external/import` | `data, token` | data | - | X-Knowledge-Token | ✅ 通过 | 必填 source/product_id；无/撤销 token → 401；有效 token 异步建 job（EXT-…, async:true） |
| 14 | knowledgeMerchantAPI | listExternalJobs | GET | `/api/knowledge-merchant/external/jobs` | `params` | - | params | JWT | ✅ 通过 | {items} job 状态 completed/done_items 一致 |

### kuaishouCard.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/kuaishou/list` | - | - | - | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/kuaishou/{id}` | - | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/kuaishou/create` | - | - | - | JWT | ✅ 通过 | 创建成功 |
| 4 | - | - | PUT | `/api/kuaishou/update` | - | - | - | JWT | ✅ 通过 | 全量语义 |
| 5 | - | - | DELETE | `/api/kuaishou/delete/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | GET | `/api/kuaishou/view/{id}` | - | - | - | JWT | ✅ 通过 | 视图数据 |
| 7 | - | - | POST | `/api/kuaishou/like/{id}` | - | - | - | JWT | ✅ 通过 | 点赞计数 +1 |
| 8 | - | - | POST | `/api/kuaishou/share/{id}` | - | - | - | JWT | ✅ 通过 | 分享计数 +1 |
| 9 | - | - | GET | `/api/kuaishou/stats/card/{id}` | - | - | - | JWT | ✅ 通过 | 卡片统计 |
| 10 | - | - | GET | `/api/kuaishou/stats/overall` | - | - | - | JWT | ✅ 通过 | 总体统计 |
| 11 | - | - | POST | `/api/kuaishou/{id}/generate-short-link` | - | - | - | JWT | ✅ 通过 | 短链生成成功 |

### leadMining.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/lead-mining/config` | - | - | - | JWT | ✅ 通过 | 单例配置（enabled/keywords/tags/min_intent_score） |
| 2 | - | - | POST | `/api/lead-mining/config` | - | data | - | JWT | ✅ 通过 | 写入生效并回读一致（测后已还原原值） |

### livecode.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/live-codes/list` | - | - | - | JWT | ✅ 通过 | {list,total}；本测试前为空表 |
| 2 | - | - | GET | `/api/live-codes/{id}` | - | - | - | JWT | ✅ 通过 | 全量行含三域名对象 |
| 3 | - | - | POST | `/api/live-codes/create` | - | - | - | JWT(admin) | ✅ 通过 | 必填 name/short_link/三域名 id；非活跃域名 400「活码域名不可用」；短链重复 409 |
| 4 | - | - | PUT | `/api/live-codes/{id}/update` | - | - | - | JWT(admin) | ✅ 通过 | 改名生效 |
| 5 | - | - | DELETE | `/api/live-codes/{id}/delete` | - | - | - | JWT(admin) | ✅ 通过 | 删除成功且库中 0 行 |
| 6 | - | - | GET | `/api/live-codes/{id}/stats` | - | - | - | JWT | ✅ 通过 | views/clicks/qr 计数完整 |
| 7 | - | - | GET | `/api/live-codes/{liveCodeId}/qrcodes` | - | - | - | JWT | ✅ 通过 | 数组；建后 count=1 |
| 8 | - | - | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | - | - | - | JWT(admin) | ✅ 通过 | 必填 expire_days（缺 → 400 validator 原文）；生成 PNG 落 /uploads/qrcodes |
| 9 | - | - | GET | `/api/live-codes/qrcodes/{qrId}/stats` | - | - | - | JWT | ✅ 通过 | view/click/expire_time |
| 10 | - | - | POST | `/api/live-codes/{id}/share` | - | - | - | JWT(admin) | ✅ 通过 | 无二维码 400「没有可用的二维码」；生成后返回 short/entry/landing 三链 |
| 11 | - | - | DELETE | `/api/live-codes/qrcodes/{id}/delete` | - | - | - | JWT(admin) | ✅ 通过 | 删除成功且库中 0 行 |
| 12 | - | - | PUT | `/api/live-codes/qrcodes/{id}/update` | - | - | - | JWT(admin) | ✅ 通过 | 状态更新成功 |
### llmReport.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LlmReportApi | getOverview | GET | `/api/reports/llm/overview` | `params` | - | params | JWT | ✅ 通过 | KPI 齐全（call/error_rate/cache_hit_rate/tokens/cost/延迟分位） |
| 2 | LlmReportApi | getTimeseries | GET | `/api/reports/llm/timeseries` | `params` | - | params | JWT | ✅ 通过 | 按日序列 |
| 3 | LlmReportApi | getBreakdown | GET | `/api/reports/llm/breakdown` | `params` | - | params | JWT | ✅ 通过 | 非法 by → 400 且列出合法值 |
| 4 | LlmReportApi | getErrors | GET | `/api/reports/llm/errors` | `params` | - | params | JWT | ✅ 通过 | by_class + 分页明细 |

### llmRouting.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LlmRoutingApi | getDailyUsage | GET | `/api/llm/usage/daily` | `window, groupBy` | - | window/group_by | JWT | ✅ 通过 | 生成器曾把 query 字面量误记为 Body；按日聚合正常 |
| 2 | LlmRoutingApi | getModelList | GET | `/api/llm/models` | `params` | - | params | JWT | ✅ 通过 | 列表含 api_key_set 脱敏 |
| 3 | LlmRoutingApi | getModelDetail | GET | `/api/llm/models/{name}` | `name` | - | - | JWT | 🔧 已修复 | 不存在原返回 200+code:0+null（与本文件 Update/Delete 的 404 口径矛盾）→ 改 404；已回归 |
| 4 | LlmRoutingApi | createModel | POST | `/api/llm/models` | `data` | data | - | JWT | ✅ 通过 | 创建成功 |
| 5 | LlmRoutingApi | updateModel | PUT | `/api/llm/models/{name}` | `name, data` | data | - | JWT | ✅ 通过 | quality_score 更新生效 |
| 6 | LlmRoutingApi | deleteModel | DELETE | `/api/llm/models/{name}` | `name` | - | - | JWT | ✅ 通过 | 删除后 Get 404 |
| 7 | LlmRoutingApi | updateModelStatus | PUT | `/api/llm/models/{name}` | `name, status` | {enabled} | - | JWT | ✅ 通过 | enabled 切换生效 |
| 8 | LlmRoutingApi | testModel | POST | `/api/llm/models/{name}/test` | `name, body` | body | - | JWT | ✅ 通过 | 失败返回 {success:false,error}（对不可达上游报连接错误） |
| 9 | LlmRoutingApi | getSceneRouting | GET | `/api/llm/scene-routing` | - | - | - | JWT | ✅ 通过 | 数组返回 |
| 10 | LlmRoutingApi | getScenarios | GET | `/api/llm/scenarios` | - | - | - | JWT | ✅ 通过 | 数组返回 |
| 11 | LlmRoutingApi | saveSceneRouting | PUT | `/api/llm/scene-routing` | `data` | {routes:[…]} | - | JWT | ✅ 通过 | 契约 `{routes:[…]}`（前端封装数组）；回写后 GET 一致，审计落 update_strategy |
| 12 | LlmRoutingApi | getFallbackStrategy | GET | `/api/llm/fallback` | - | - | - | JWT | ✅ 通过 | 按场景回退策略 |
| 13 | LlmRoutingApi | getAuditHistory | GET | `/api/llm/audit` | `scenario, limit` | - | params | JWT | ✅ 通过 | 审计历史 |
| 14 | LlmRoutingApi | getStats | GET | `/api/llm/stats` | - | - | - | JWT | ✅ 通过 | {}（当前空数据面） |
| 15 | LlmRoutingApi | getUsage | GET | `/api/llm/usage` | `window` | - | window | JWT | ✅ 通过 | total/by_scenario；非法 window 宽松回退全量（非缺陷） |
| 16 | LlmRoutingApi | getCostStats | GET | `/api/llm/cost-stats` | `window` | - | window | JWT | ✅ 通过 | by_model 成本聚合 |
| 17 | LlmRoutingApi | getHealth | GET | `/api/llm/health` | - | - | - | JWT | ✅ 通过 | providers 健康位图 |
| 18 | LlmRoutingApi | getModelTypeStats | GET | `/api/llm/model-type-stats` | `window` | - | window | JWT | ✅ 通过 | local/cloud 自给率 |
| 19 | LlmRoutingApi | getEgressAudit | GET | `/api/llm/egress-audit` | `params` | - | params | JWT | ✅ 通过 | {alerts,count} |
| 20 | LlmRoutingApi | getEgressAlerts | GET | `/api/llm/egress-alerts` | `params` | - | params | JWT | ✅ 通过 | {alerts,count} |

### ltcConfig.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/manage/ltc/config` | - | - | - | JWT | ✅ 通过 | 全量配置含 guarded_routes/threshold_bounds |
| 2 | - | - | PUT | `/api/manage/ltc/config` | - | cfg | - | JWT | ✅ 通过 | 窄载荷契约 {enabled,stages_enabled,thresholds}（前端 payload() 同形）；回写生效 audit_written:true；整包回贴 GET 响应会 400 unknown field（契约如此） |

### ltcRates.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LtcRatesApi | getRates | GET | `/api/ltc-rates` | - | - | - | JWT | ✅ 通过 | 三率 closure/collection/overdue 各含 rate/numerator/denominator |

### marketingFlow.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/marketing-flows` | - | - | params | JWT | ✅ 通过 | {list,page,page_size,total} |
| 2 | - | - | GET | `/api/marketing-flows/{id}` | - | - | - | JWT | ✅ 通过 | 删除后 → 404 |
| 3 | - | - | POST | `/api/marketing-flows` | - | data | - | JWT | ✅ 通过 | 节点须在 `flow_data.nodes`（顶层 nodes 被忽略 → 400「至少需要一个节点」）；必须含 trigger 节点；建出 status=draft |
| 4 | - | - | PUT | `/api/marketing-flows/{id}` | - | data | - | JWT | ✅ 通过 | 部分更新 |
| 5 | - | - | DELETE | `/api/marketing-flows/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功，随后 GET 404 |
| 6 | - | - | POST | `/api/marketing-flows/{id}/activate` | - | - | - | JWT | ✅ 通过 | 激活成功 |
| 7 | - | - | POST | `/api/marketing-flows/{id}/pause` | - | - | - | JWT | ✅ 通过 | 暂停成功 |
| 8 | - | - | POST | `/api/marketing-flows/{id}/stop` | - | - | - | JWT | ✅ 通过 | 停止成功（activate→pause→stop 逐态可走通） |
| 9 | - | - | GET | `/api/marketing-flows/{id}/executions` | - | - | - | JWT | ✅ 通过 | {list,total} |
| 10 | - | - | GET | `/api/marketing-flows/{id}/stats` | - | - | - | JWT | ✅ 通过 | {}（无执行记录时为空对象） |

### material.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/material/list` | - | - | params | JWT | ✅ 通过 | {list,total,page,limit}；另 upload（POST /api/material/upload，multipart，必填 category_id/file）已实测通过——`http.upload` 不被生成器采集故未单列 |
| 2 | - | - | DELETE | `/api/material/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 deleted:true，库中 0 行 |
| 3 | - | - | GET | `/api/material/categories` | - | - | - | JWT | ✅ 通过 | {list,total}；支持 parent_id/type 过滤 |
| 4 | - | - | POST | `/api/material/categories` | - | data | - | JWT | ✅ 通过 | 必填 name/type(oneof image|video|audio|file)；缺 type → 400 |
| 5 | - | - | PUT | `/api/material/categories/{id}` | - | data | - | JWT | ✅ 通过 | 改名生效 |
| 6 | - | - | DELETE | `/api/material/categories/{id}` | - | - | - | JWT | ✅ 通过 | deleted:true |
| 7 | - | - | GET | `/api/material/selector` | - | - | params | JWT | ✅ 通过 | {categories,…} 选择器视图 |
| 8 | - | - | POST | `/api/material/{id}/usage` | - | - | - | JWT | ✅ 通过 | 使用计数 +1（stats total_usage_count 同步） |
| 9 | - | - | GET | `/api/material/stats` | - | - | - | JWT | ✅ 通过 | 按类型/大小/今日新增聚合 |

### mcpCredential.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | mcpCredentialApi | list | GET | `/api/mcp/credentials` | - | - | - | JWT | ✅ 通过 | 数组；api_key 不回显（仅创建时一次） |
| 2 | mcpCredentialApi | create | POST | `/api/mcp/credentials` | `data` | data | - | JWT | ✅ 通过 | 返回 client_id + api_key（api_hint 提示仅显示一次） |
| 3 | mcpCredentialApi | update | PUT | `/api/mcp/credentials/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改名/禁用生效 |
| 4 | mcpCredentialApi | remove | DELETE | `/api/mcp/credentials/{id}` | `id` | - | - | JWT | ✅ 通过 | 已删除 |

### messageHub.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/message-hub/push` | `data` | data | - | JWT | ✅ 通过 | 必填 msg_id（缺失 → 400 invalid msg_id）；同 msg_id 重复推送 → 「消息已存在(幂等)」duplicate:true |
| 2 | - | - | POST | `/api/message-hub/push-batch` | `data` | data | - | JWT | ✅ 通过 | **裸数组**（非 {messages:[]}）；返回 items/errors/failed_count |
| 3 | - | - | POST | `/api/message-hub/push-from-channel` | `data` | data | - | JWT | ✅ 通过 | 契约 RawChannelMessage（platform/account_id/msg_id/from/to/content）；platform 非法 → 400 |
| 4 | - | - | GET | `/api/message-hub/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 5 | - | - | GET | `/api/message-hub/{id}` | `id` | - | - | JWT | ✅ 通过 | 全量行 |
| 6 | - | - | POST | `/api/message-hub/{id}/read` | `ids` | {ids} | - | JWT | ✅ 通过 | {updated:N}；未读数下降 |
| 7 | - | - | POST | `/api/message-hub/{id}/recall` | `id` | - | - | JWT | ✅ 通过 | 业务规则正确：inbound → 400「only outbound…」；渠道无主动撤回 → 409 MESSAGE_RECALL_UNSUPPORTED_8201 |
| 8 | - | - | GET | `/api/message-hub/stats` | `params` | - | params | JWT | ✅ 通过 | total/inbound/outbound/unread + by_platform/by_direction |
| 9 | - | - | GET | `/api/message-hub/platforms` | - | - | - | JWT | ✅ 通过 | {platforms,msg_types} |

### monitor.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/monitor/trace-tree` | `params` | - | params | JWT | ✅ 通过 | 需 trace_id；返回 spans 树 |
| 2 | - | - | GET | `/api/monitor/traces` | `params` | - | params | JWT | ✅ 通过 | 数组 + trace_id/node_count |
| 3 | - | - | GET | `/api/monitor/lifecycle` | `params` | - | params | JWT | ✅ 通过 | conversation_id 或 trace_id 必填其一（缺失 → 400） |
| 4 | - | - | GET | `/api/monitor/health` | - | - | - | JWT | ✅ 通过 | 入/出站速率等聚合 |
| 5 | - | - | GET | `/api/monitor/node-health` | - | - | - | JWT | ✅ 通过 | {nodes,window} |
| 6 | - | - | GET | `/api/monitor/latency` | - | - | - | JWT | ✅ 通过 | 数组 |
| 7 | - | - | GET | `/api/monitor/anomalies` | - | - | - | JWT | ✅ 通过 | sync_gap/stuck_reachable |
| 8 | - | - | GET | `/api/monitor/trace-eval/logs` | `params` | - | params | JWT | ✅ 通过 | 数组 |
| 9 | - | - | GET | `/api/monitor/knowledge-weights` | `params` | - | params | JWT | ✅ 通过 | 数组 |
| 10 | - | - | POST | `/api/monitor/trace-eval/trigger` | - | - | params | JWT | ✅ 通过 | {processed:N} |
| 11 | - | - | GET | `/api/rag/recall/snapshot` | - | - | - | JWT | ✅ 通过 | 无数据时 available:false + 可操作提示 |
| 12 | - | - | GET | `/api/rag/recall/snapshots` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 13 | - | - | POST | `/api/rag/recall/collect` | - | - | params | JWT | ✅ 通过 | 返回窗口聚合 |
| 14 | - | - | POST | `/api/rag/recall/start` | - | - | - | JWT | ✅ 通过 | {interval:5m,started:true} |
| 15 | - | - | POST | `/api/rag/recall/stop` | - | - | - | JWT | ✅ 通过 | started:false（测后已停，恢复原态） |
| 16 | - | - | GET | `/api/rag/recall/metrics` | `params` | - | params | JWT | ✅ 通过 | start/end RFC3339 必填（缺失 → 400） |
| 17 | - | - | GET | `/api/rag/recall/low-recall` | `params` | - | params | JWT | ✅ 通过 | 数组 |

### notification.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | list | GET | `/api/auth/notifications` | `params` | - | params | JWT | ✅ 通过 | {list} 含 type/title/content |
| 2 | - | markRead | POST | `/api/auth/notifications/{id}/read` | `id` | - | - | JWT | ✅ 通过 | 已标记为已读 |
| 3 | - | markAllRead | POST | `/api/auth/notifications/read-all` | - | - | - | JWT | ✅ 通过 | {marked:N} |
| 4 | - | unreadCount | GET | `/api/auth/notifications/unread-count` | - | - | - | JWT | ✅ 通过 | 全部已读后 count=0（与 #2/#3 一致） |

### objection.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/objection/handle` | `data` | data | - | JWT | ✅ 通过 | 返回 category/confidence/话术模板 |
| 2 | - | - | POST | `/api/objection/classify` | `data` | data | - | JWT | ✅ 通过 | 「太贵了」→ price/价格异议 |
| 3 | - | - | GET | `/api/objection/categories` | - | - | - | JWT | ✅ 通过 | {list} 六类异议 |
| 4 | - | - | POST | `/api/objection/usage` | `data` | data | - | JWT | ✅ 通过 | 记录成功 |

### obs.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/obs/config` | `params` | - | params | JWT | ✅ 通过 | {list}；secret_key 脱敏为 *** |
| 2 | - | - | GET | `/api/obs/config/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/obs/config` | `data` | data | - | JWT | ✅ 通过 | provider=local 建出配置（测后删除） |
| 4 | - | - | PUT | `/api/obs/config/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改名生效 |
| 5 | - | - | DELETE | `/api/obs/config/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/obs/config/{id}/test` | `id` | - | - | JWT | ✅ 通过 | 「连接测试成功」 |
| 7 | - | - | POST | `/api/obs/config/{id}/default` | `id` | - | - | JWT | ✅ 通过 | 设默认成功，GET default 同步；测后已还原 qiniu |
| 8 | - | - | GET | `/api/obs/config/default` | - | - | - | JWT | ✅ 通过 | 返回默认配置 |

### observabilityDashboard.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/observability/dashboard` | `window` | - | window | JWT | ✅ 通过 | 聚合 ai_cost/…，window 生效 |

### oneid.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/customer/oneid/list` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/customer/oneid/conflicts` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 3 | - | - | POST | `/api/customer/oneid/merge` | `data` | data | - | JWT | ✅ 通过 | 空 body → 400 validator |
| 4 | - | - | POST | `/api/customer/oneid/conflicts/{id}/resolve` | `data` | data | - | JWT | ✅ 通过 | 缺参数 → 400 |
| 5 | - | - | GET | `/api/customer-oneid/{customerId}/identities` | `customerId` | - | - | JWT | ✅ 通过 | {customer,identifiers} |
| 6 | - | - | POST | `/api/customer-oneid/{customerId}/identities` | `data` | data | - | JWT | ✅ 通过 | 契约是扁平身份字段（phone/email/…），非 identity_type/value；email 链接后回显 |
| 7 | - | - | POST | `/api/customer/oneid/resolve` | `data` | data | - | JWT | ✅ 通过 | 同样扁平字段；无匹配 → matched:false |
| 8 | - | - | GET | `/api/customer/oneid/stats` | - | - | - | JWT | ✅ 通过 | total/with_phone/… 计数 |
| 9 | - | - | GET | `/api/oneid/merge-rules` | - | - | - | JWT | ✅ 通过 | {built_in,custom,strategy} |
| 10 | - | - | POST | `/api/oneid/merge-rules` | `data` | data | - | JWT | ✅ 通过 | 契约 {built_in,custom,strategy}；整包回贴 {rules:[…]} 会 400「至少需要 1 条规则」 |

### operationLog.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/operation-logs` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/operation-logs/{id}` | `id` | - | - | JWT | ✅ 通过 | 详情 |
| 3 | - | - | GET | `/api/operation-logs/statistics` | `params` | - | params | JWT | 🔧 已修复 | module_stats 曾出现 "1413"/"5"/UUID 键——审计中间件把第 4 段（资源 ID）当模块名；改由 auditDomainAndResource 统一判定 ID 段，module/resource 回落域名，resource_id 支持 UUID/前缀 ID；回归 TestGetModuleFromPath_ResourceID 等 |
| 4 | - | - | GET | `/api/operation-logs/export` | `params` | - | params | JWT | ✅ 通过 | CSV（6.1KB，表头 ID,Module,Action,…），修复后 Module 列输出域名 |
| 5 | - | - | POST | `/api/operation-logs/clean` | - | {beforeDate} | - | JWT | ✅ 通过 | 返回实际清理截止时间；不存在的早日期不误删 |

### orderDraft.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | orderDraftApi | list | GET | `/api/manage/order-drafts` | `params` | - | params | JWT | ✅ 通过 | 需底座装配（FF_LTC_ORDER_DRAFT_DB=on，独立实例 8211 实测）；未装配时 503 + 档位提示；列表按 expires_at>now 过滤（NULL 视为已过期） |
| 2 | orderDraftApi | get | GET | `/api/manage/order-drafts/{id}` | `id` | - | - | JWT | 🔧 已修复 | 响应 tenant_id 恒为 0（draftFromModel 漏映射）→ 已回填；不存在 → 404 带 id；已回归（tenant_id=1） |
| 3 | orderDraftApi | edit | PATCH | `/api/manage/order-drafts/{id}` | `id, updates` | updates | - | JWT | ✅ 通过 | 改数量/单价后 total_amount 重算（3×88.5=265.5）；空对象 400（后端规则） |
| 4 | orderDraftApi | confirm | POST | `/api/manage/order-drafts/{id}/confirm` | `id` | - | - | JWT | ✅ 通过 | 过期 → 409 ORDER_DRAFT_STATE_CONFLICT_8302「草稿已过期」；有效草稿 → confirmed 且生成真实 order_id（已核对库） |
| 5 | orderDraftApi | cancel | POST | `/api/manage/order-drafts/{id}/cancel` | `id, reason` | {reason} | - | JWT | ✅ 通过 | 缺理由 → 400（提示理由同时是分析原料）；带理由 → cancelled + cancel_reason 落库 |
| 6 | orderDraftApi | stats | GET | `/api/agent/order-drafts/stats` | - | - | - | JWT | ✅ 通过 | 未装配：assembled:false/counts:null；装配后 mode=on/store=db/durable:true/counts:{}（null 与 {} 语义分离，符合设计） |

### persona.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/analytics/persona/staffs` | - | - | - | JWT | ✅ 通过 | {list,total}；主键字段 staff_id（非 id） |
| 2 | - | - | GET | `/api/analytics/persona/staffs/{staffId}` | - | - | - | JWT | ✅ 通过 | 五维评分报告（response_speed/conversion/…） |

### qqBot.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/qq/accounts` | - | - | params | JWT | ✅ 通过 | {list,total}；app_secret 恒掩码 |
| 2 | - | - | GET | `/api/qq/accounts/{id}` | - | - | - | JWT | ✅ 通过 | 单条（含 app_secret_masked） |
| 3 | - | - | POST | `/api/qq/accounts` | - | data | - | JWT | ✅ 通过 | 必填 account_name/app_id/app_secret（缺 → 400 并点名字段） |
| 4 | - | - | PUT | `/api/qq/accounts/{id}` | - | data | - | JWT | 🔧 已修复 | 曾复用创建结构（AppSecret required）⇒ 前端编辑回填空 secret 必 400，「空=保留原值」分支不可达；改专用 qqAccountUpdateReq（密钥/app_id 非必填，空则保留），account_name 也改空=不改；回归 TestQQAccountUpdateReqAllowsEmptySecrets |
| 5 | - | - | DELETE | `/api/qq/accounts/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/qq/accounts/{id}/test-send` | - | data | - | JWT | ✅ 通过 | 契约 target_type/target_openid/text（注意是 openid 不是 open_id）；假凭据 → 502 UPSTREAM_FAILURE_5005 且带上游真因（appid invalid） |
| 7 | - | - | POST | `/api/qq/accounts/{id}/verify-callback` | - | {} | - | JWT | ✅ 通过 | 未配 webhook_secret → 400 可操作提示；配置后自检通过 |

### ragProductConfig.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | ragProductConfigAPI | createRagProduct | POST | `/api/rag-config/products` | `data` | data | - | JWT | ✅ 通过 | 必填 name + category（缺 category → 400）；建出 vector_table |
| 2 | ragProductConfigAPI | getRagProducts | GET | `/api/rag-config/products` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 3 | ragProductConfigAPI | listProducts | GET | `/api/rag-config/products` | `params` | - | params | JWT | ✅ 通过 | 同端点（前端两个封装），行为一致 |
| 4 | ragProductConfigAPI | updateRagProduct | PUT | `/api/rag-config/products/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 全量更新语义：必须回传 name（只传 description → 400）；与前端 {...form} 一致 |
| 5 | ragProductConfigAPI | deleteRagProduct | DELETE | `/api/rag-config/products/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | ragProductConfigAPI | getAccountConfig | GET | `/api/rag-config/accounts/config` | `params` | - | platform/account_id | JWT | ✅ 通过 | 未配置时回「No account config found」 |
| 7 | ragProductConfigAPI | updateAccountConfig | PUT | `/api/rag-config/accounts/config` | `data` | data | - | JWT | ✅ 通过 | 字段名是 is_auto_reply_enabled/is_rag_enabled（非 rag_enabled）；platform 白名单 douyin/kuaishou/xiaohongshu/xianyu；upsert 生效 |
| 8 | ragProductConfigAPI | processMessage | POST | `/api/rag-config/process-message` | `data` | data | - | JWT | ✅ 通过 | 必填 platform/account_id/message；auto_reply 关 → 400 明确提示；开启后返回兜底回复 |

### reachPipeline.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | reachPipelineApi | getPipelines | GET | `/api/reach/pipelines` | `params` | - | params | JWT | ✅ 通过 | {list} 含 steps/retry_policy/rate_limit |
| 2 | reachPipelineApi | createPipeline | POST | `/api/reach/pipelines` | `data` | data | - | JWT | ✅ 通过 | 必填 name/channel（channel 白名单校验） |
| 3 | reachPipelineApi | getPipeline | GET | `/api/reach/pipelines/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 4 | reachPipelineApi | updatePipeline | PUT | `/api/reach/pipelines/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 复用创建结构（全量语义，必带 channel；无前端调用方） |
| 5 | reachPipelineApi | deletePipeline | DELETE | `/api/reach/pipelines/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功（连带 jobs 由服务端处理） |
| 6 | reachPipelineApi | pausePipeline | POST | `/api/reach/pipelines/{id}/pause` | `id` | {} | - | JWT | ✅ 通过 | 暂停成功 |
| 7 | reachPipelineApi | resumePipeline | POST | `/api/reach/pipelines/{id}/resume` | `id` | {} | - | JWT | ✅ 通过 | 恢复成功 |
| 8 | reachPipelineApi | archivePipeline | POST | `/api/reach/pipelines/{id}/archive` | `id` | {} | - | JWT | ✅ 通过 | 归档成功 |
| 9 | reachPipelineApi | getJobs | GET | `/api/reach/jobs` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 10 | reachPipelineApi | enqueueJob | POST | `/api/reach/jobs` | `data` | data | - | JWT | ✅ 通过 | 入队后 worker 立即执行（观测到 pending→success 极快，非缺陷） |
| 11 | reachPipelineApi | getJob | GET | `/api/reach/jobs/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 12 | reachPipelineApi | cancelJob | POST | `/api/reach/jobs/{id}/cancel` | `id` | {} | - | JWT | ✅ 通过 | pending → canceled；终态任务 → 400「job is not pending/running」 |
| 13 | reachPipelineApi | retryJob | POST | `/api/reach/jobs/{id}/retry` | `id` | {} | - | JWT | ✅ 通过 | failed → pending（已核对库）；success 态 → 400 |
| 14 | reachPipelineApi | executeJob | POST | `/api/reach/jobs/{id}/execute` | `id` | {} | - | JWT | ✅ 通过 | 手动执行成功并写 _last_send |
| 15 | reachPipelineApi | getStats | GET | `/api/reach/stats` | `params` | - | params | JWT | ✅ 通过 | active/success/failed/canceled 计数 |
| 16 | reachPipelineApi | resetRateLimit | POST | `/api/reach/rate-limit/reset` | `channel` | {} | channel | JWT | ✅ 通过 | 重置成功 |
| 17 | reachPipelineApi | getDeferredOutbounds | GET | `/api/reach/deferred-outbound` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 18 | reachPipelineApi | cancelDeferredOutbound | POST | `/api/reach/deferred-outbound/{id}/cancel` | `id` | {} | - | JWT | ✅ 通过 | 不存在/非 pending → 404「记录不存在或不可取消（仅 pending 可取消）」 |

### runtimeConfig.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/config/runtime` | - | - | params | JWT | ✅ 通过 | 扁平 key-value（frontend_ui.* 等），支持 groups 过滤 |
| 2 | - | - | GET | `/api/config/dictionaries/{parent}` | - | - | - | JWT | ✅ 通过 | 与 /api/manage 别名同源；未知域 → 404 并点名 |
| 3 | dictionariesApi | listParents | GET | `/api/manage/dictionaries` | - | - | - | JWT | ✅ 通过 | 父字典数组（code/label/description） |
| 4 | dictionariesApi | get | GET | `/api/manage/dictionaries/{parent}` | - | - | - | JWT | ✅ 通过 | {items} 完整项 |
| 5 | dictionariesApi | upsertItem | PUT | `/api/manage/dictionaries/{parent}/items` | - | item | - | JWT | ✅ 通过 | 新增项生效（测后已清理） |
| 6 | dictionariesApi | replaceTransitions | PUT | `/api/manage/dictionaries/{parent}/transitions` | - | {transitions} | - | JWT | ✅ 通过 | 空列表替换成功 |

### salesWorkbench.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | salesWorkbenchApi | overview | GET | `/api/sales-workbench/overview` | `salesId` | - | sales_id | JWT | ✅ 通过 | sales_id 必填（缺失 → 400）；返回 todos/today/month |
| 2 | salesWorkbenchApi | teamDashboard | GET | `/api/sales-workbench/team-dashboard` | `days` | - | days | JWT | ✅ 通过 | funnel/top_sales/ai_productivity |
| 3 | salesWorkbenchApi | champion | GET | `/api/sales-workbench/champion` | `days` | - | days | JWT | ✅ 通过 | top_performers/insights |
| 4 | salesWorkbenchApi | quickActions | GET | `/api/sales-workbench/quick-actions` | - | - | - | JWT | ✅ 通过 | {list} 快捷动作含深链 |

### scriptAb.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | scriptAbApi | listVersions | GET | `/api/script-library/{scriptId}/versions` | `scriptId` | - | - | JWT | ✅ 通过 | {list}（id=行主键，version=版本号，两者不同） |
| 2 | scriptAbApi | createVersion | POST | `/api/script-library/{scriptId}/versions` | `scriptId` | data | - | JWT | ✅ 通过 | body 只认 `note`，内容取自 script_library 当前行快照；新版本自动 active |
| 3 | scriptAbApi | activateVersion | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | `scriptId, versionId` | - | - | JWT | ✅ 通过 | **vid 是版本号**（前端传 row.version）；传行主键会 404「记录不存在」 |
| 4 | scriptAbApi | expireScript | POST | `/api/script-library/{scriptId}/expire` | `scriptId` | data | - | JWT | ✅ 通过 | status→expired + expires_at=now（测试后已还原 active/NULL） |
| 5 | scriptAbApi | getAbStats | GET | `/api/script-library/{scriptId}/ab-stats` | `scriptId, params` | - | params | JWT | ✅ 通过 | buckets/attribution_hrs/config |
| 6 | scriptAbApi | updateAbConfig | PUT | `/api/script-library/{scriptId}/ab-config` | `scriptId, data` | data | - | JWT | ✅ 通过 | 需 split_a/split_b ∈ (0,100)；只给 traffic_split → 400 明确提示 |
| 7 | scriptAbApi | recordConversion | POST | `/api/script-ab/conversion` | `data` | data | - | JWT | ✅ 通过 | 必填 one_id；记录成功 recorded:true |

### scriptTemplate.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/scripts` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/scripts/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/scripts` | - | data | - | JWT | ✅ 通过 | 建成功（script_templates） |
| 4 | - | - | PUT | `/api/scripts/{id}` | - | data | - | JWT | ✅ 通过 | 改标题生效 |
| 5 | - | - | DELETE | `/api/scripts/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功（测后已删夹具） |
| 6 | - | - | GET | `/api/scripts/categories` | - | - | - | JWT | ✅ 通过 | 数组（当前空） |
| 7 | - | - | GET | `/api/scripts/search` | `params` | - | params | JWT | ✅ 通过 | keyword 命中新建行 |
| 8 | - | - | GET | `/api/scripts/public` | - | - | - | JWT | ✅ 通过 | {list,total} |
| 9 | - | - | POST | `/api/scripts/recommend` | - | data | - | JWT | ✅ 通过 | 必填 message；无匹配 → data:null |

### securityAudit.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/security/audit` | `data` | data | - | JWT | ✅ 通过 | 返回 score/total_checks/passed/failed |
| 2 | - | - | GET | `/api/security/audit/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 3 | - | - | GET | `/api/security/audit/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条完整结果 |

### shortLink.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/shortlink/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/shortlink/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/shortlink/create` | `data` | data | - | JWT | ✅ 通过 | 必填 short_code + original_url(url 校验) |
| 4 | - | - | PUT | `/api/shortlink/update` | `data` | data | - | JWT | ✅ 通过 | 全量语义：必填 id + original_url；只传 id/title → 400 |
| 5 | - | - | DELETE | `/api/shortlink/delete/{id}` | `id` | - | - | JWT | 🔧 已修复 | 有访问流水的短链曾删不掉（先删主表撞 FK 23503 → 500「删除短链失败」）→ 改为先删访问流水再删主表；回归 TestShortLinkService_Delete_WithAccessLogs |
| 6 | - | - | POST | `/api/shortlink/access` | `data` | data | - | JWT | ✅ 通过 | 返回 original_url/title |
| 7 | - | - | POST | `/api/shortlink/generate` | `data` | data | - | JWT | ✅ 通过 | 必填 original_url，生成随机 short_code |
| 8 | - | - | GET | `/api/shortlink/{id}/stats` | `id` | - | params | JWT | ✅ 通过 | 单链统计 |
| 9 | - | - | GET | `/api/shortlink/stats/all` | `params` | - | params | JWT | ✅ 通过 | 全量聚合 total/today/device_type |
| 10 | - | - | POST | `/api/shortlink/{id}/share` | `data` | data | - | JWT | ✅ 通过 | 返回 short_url + base64 二维码 |

### sms.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/sms/config` | - | - | - | JWT | ✅ 通过 | provider/rateLimit/dailyLimit/retry |
| 2 | - | - | POST | `/api/sms/config` | - | data | - | JWT | ✅ 通过 | 回写成功且 GET 同步 |
| 3 | - | - | GET | `/api/sms/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 4 | - | - | GET | `/api/sms/detail/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条（含 provider/status） |
| 5 | - | - | POST | `/api/sms/resend/{id}` | `id` | data | - | JWT | 🔧 已修复 | 渠道故障曾回 400（HandleServiceError 一律 400）→ 与 ErrorFromDB 同口径：5xxx 上游失败改 502；回归 TestHandleServiceError_UpstreamFailureMaps502 |
| 6 | - | - | POST | `/api/sms/send` | `data` | data | - | JWT | ✅ 通过 | 假凭据 → 502 UPSTREAM_FAILURE_5005 且带上游真因 |
| 7 | - | - | GET | `/api/sms/draft/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 8 | - | - | GET | `/api/sms/draft/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 9 | - | - | POST | `/api/sms/draft` | `data` | data | - | JWT | ✅ 通过 | 仅 title/content（无收件人字段，与发送解耦） |
| 10 | - | - | PUT | `/api/sms/draft/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 全量 title/content |
| 11 | - | - | DELETE | `/api/sms/draft/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 12 | - | - | POST | `/api/sms/draft/{id}/send` | `id` | data | - | JWT | ✅ 通过 | 必填 `phone`（JSON 或表单）；缺 → 400；假凭据 → 502 |
| 13 | - | - | GET | `/api/sms/job/list` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 14 | - | - | GET | `/api/sms/job/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 15 | - | - | POST | `/api/sms/job/{id}/pause` | `id` | - | - | JWT | ✅ 通过 | 暂停成功 |
| 16 | - | - | POST | `/api/sms/job/{id}/resume` | `id` | - | - | JWT | ✅ 通过 | 恢复成功 |
| 17 | - | - | POST | `/api/sms/job/{id}/stop` | `id` | - | - | JWT | ✅ 通过 | 停止成功 |
| 18 | - | - | POST | `/api/sms/job` | `data` | data | - | JWT | ✅ 通过 | 字段是 camelCase：phoneList/scheduleTime（与全仓 snake_case 不一致，前端同源） |
| 19 | - | - | DELETE | `/api/sms/job/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 20 | - | - | GET | `/api/sms/job/{id}/records` | `id, params` | - | params | JWT | ✅ 通过 | {list} 明细（jobId/phone/status） |

### sopAgent.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/sop` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | POST | `/api/sop` | `data` | data | - | JWT | ✅ 通过 | 必填 name/scenario/sop_graph（图须含 start 节点与合法 type）；缺 → 400 |
| 3 | - | - | GET | `/api/sop/stats` | - | - | - | JWT | ✅ 通过 | active/success/failed/total 计数 |
| 4 | - | - | GET | `/api/sop/match` | `params` | - | params | JWT | ✅ 通过 | intent 匹配，空数组为合法结果 |
| 5 | - | - | GET | `/api/sop/executions` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 6 | - | - | GET | `/api/sop/executions/{id}` | `id` | - | - | JWT | ✅ 通过 | 执行详情 |
| 7 | - | - | POST | `/api/sop/executions/{id}/pause` | `id` | - | - | JWT | ✅ 通过 | 暂停成功 |
| 8 | - | - | POST | `/api/sop/executions/{id}/resume` | `id` | - | - | JWT | ✅ 通过 | 恢复成功 |
| 9 | - | - | POST | `/api/sop/executions/{id}/cancel` | `id` | - | - | JWT | ✅ 通过 | 取消成功 |
| 10 | - | - | GET | `/api/sop/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 11 | - | - | PUT | `/api/sop/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 全量（name/scenario/sop_graph） |
| 12 | - | - | DELETE | `/api/sop/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 13 | - | - | POST | `/api/sop/{id}/activate` | `id` | - | - | JWT | ✅ 通过 | 启用成功 |
| 14 | - | - | POST | `/api/sop/{id}/deactivate` | `id` | - | - | JWT | ✅ 通过 | 停用成功 |
| 15 | - | - | POST | `/api/sop/execute` | `data` | data | - | JWT | ✅ 通过 | 未激活 SOP → 400「sop is not active」；激活后建 execution(status=running) |
| 16 | - | - | POST | `/api/sop/step` | `data` | data | - | JWT | ✅ 通过 | 推进成功；已取消执行 → 400「execution is not running」 |

### sopTemplate.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/sop-templates` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/sop-templates/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/sop-templates` | `data` | data | - | JWT | ✅ 通过 | 必填 name/intent/stage/template/agent_id（缺 agent_id → 400） |
| 4 | - | - | PUT | `/api/sop-templates/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 5 | - | - | PATCH | `/api/sop-templates/{id}/toggle` | `id, enabled` | {enabled} | - | JWT | ✅ 通过 | 状态切换生效（enabled=false 回读一致） |
| 6 | - | - | DELETE | `/api/sop-templates/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 7 | - | - | POST | `/api/sop-templates/match` | `data` | data | - | JWT | ✅ 通过 | 按 intent/stage/agent_id 匹配，返回命中数组 |

### stats.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | StatsApi | getSystemInfo | GET | `/api/system/info` | - | - | - | JWT | ✅ 通过 | CPU/内存/磁盘/uptime/server_time（磁盘 93% 属运维观察项） |

### tagSegmentation.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/session-tags` | `params` | - | params | JWT | ✅ 通过 | 数组分页 |
| 2 | - | - | PUT | `/api/session-tags/{id}` | `id, body` | body | - | JWT | ✅ 通过 | 必填 name+code（全量语义，缺则 400 点名） |
| 3 | - | - | POST | `/api/session-tags` | `data` | data | - | JWT | ✅ 通过 | 创建成功（name/code/color） |
| 4 | - | - | DELETE | `/api/session-tags/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 5 | - | - | GET | `/api/customer-360/tag-rules` | `params` | - | params | JWT | ✅ 通过 | {items,total} |
| 6 | - | - | POST | `/api/customer-360/tag-rules` | `data` | data | - | JWT | ✅ 通过 | 保存成功，id 为 UUID |
| 7 | - | - | PUT | `/api/customer-360/tag-rules/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改 name/active 生效 |
| 8 | - | - | DELETE | `/api/customer-360/tag-rules/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 9 | - | - | GET | `/api/user-segment/layers` | - | - | - | JWT | ✅ 通过 | 分层定义数组 |
| 10 | - | - | GET | `/api/customer-360/tag-stats` | `params` | - | params | JWT | ✅ 通过 | total/by_category/by_source |

### telegram.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/telegram/accounts` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/telegram/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条（token 掩码） |
| 3 | - | - | POST | `/api/telegram/accounts` | `data` | data | - | JWT | ✅ 通过 | bot_token 校验：冒号后必须 35 位；建后异步验证 bot（失败不改创建结果，符合响应文案） |
| 4 | - | - | PUT | `/api/telegram/accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 5 | - | - | DELETE | `/api/telegram/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/telegram/accounts/{id}/register-webhook` | `id, data` | data | - | JWT | ✅ 通过 | 用服务端 PUBLIC_BASE_URL 推导回调地址；本环境为 http → 400「Telegram 强制 https」（可操作） |
| 7 | - | - | POST | `/api/telegram/accounts/{id}/test-send` | `id, data` | data | - | JWT | ⛔ 阻塞 | chat_id 须为 int64；本环境无外网到 api.telegram.org，请求超时（环境阻塞，非代码缺陷） |
| 8 | - | - | GET | `/api/telegram/gates` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 9 | - | - | POST | `/api/telegram/gates` | `data` | data | - | JWT | ✅ 通过 | 必填 account_id/chat_id(字符串)/mode(oneof join_request|mute_unlock) |
| 10 | - | - | PUT | `/api/telegram/gates/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改 mode/enabled 生效 |
| 11 | - | - | DELETE | `/api/telegram/gates/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 12 | - | - | GET | `/api/telegram/gates/{id}/members` | `id, params` | - | params | JWT | ✅ 通过 | {list,total} |
| 13 | - | - | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | `id, memberId` | data | - | JWT | ✅ 通过 | 不存在成员 → 404（文案「放行失败」偏笼统，记二次审核） |

### templateMarket.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/template-market/list` | `params` | - | params | 公开 | ✅ 通过 | 目录条目 {list} |
| 2 | - | - | GET | `/api/template-market/categories` | - | - | - | 公开 | ✅ 通过 | 分类数组 |
| 3 | - | - | GET | `/api/template-market/detail/{id}` | `id` | - | - | 公开 | ✅ 通过 | 条目详情 |
| 4 | - | - | POST | `/api/template-market/entries` | `data` | data | - | JWT | ✅ 通过 | 必填 source（事实源）+ availability∈builtin/needs_module/coming_soon；缺 → 4000 明确文案 |
| 5 | - | - | GET | `/api/template-market/my/submissions` | - | - | - | JWT | ✅ 通过 | {list,total} |
| 6 | - | - | GET | `/api/template-market/my/orders` | - | - | - | JWT | ✅ 通过 | {list,total} |
| 7 | - | - | GET | `/api/template-market/my/owned` | - | - | - | JWT | ✅ 通过 | {list,total}；支付确认后 total+1 |
| 8 | - | - | POST | `/api/template-market/entries/{entryId}/order` | `entryId, data` | data | - | JWT | ✅ 通过 | 付费条目必须带 contact_phone（否则 4000）；重复下单 → duplicate:true,order:null（幂等） |
| 9 | - | - | POST | `/api/template-market/orders/{orderNo}/cancel` | `orderNo` | - | - | JWT | ✅ 通过 | 仅 pending_payment 可取消；已支付 → 4009「当前订单状态不允许该操作」 |
| 10 | - | - | GET | `/api/template-market/admin/entries` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 11 | - | - | POST | `/api/template-market/admin/entries/{entryId}/review` | `entryId, data` | {action} | - | JWT | ✅ 通过 | action∈approve/reject/disable/enable（非法 → 4000 列出合法值）；approve 生效 |
| 12 | - | - | GET | `/api/template-market/admin/orders` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 13 | - | - | POST | `/api/template-market/admin/orders/{orderNo}/mark-paid` | `orderNo` | - | - | JWT | ✅ 通过 | 确认收款成功并置 paid；不存在 → 4004 |

### tiktokCard.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/tiktok-card/list` | - | - | - | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/tiktok-card/{id}` | - | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/tiktok-card` | - | - | - | JWT | ✅ 通过 | 创建成功 |
| 4 | - | - | PUT | `/api/tiktok-card/{id}` | - | - | - | JWT | ✅ 通过 | 更新成功 |
| 5 | - | - | DELETE | `/api/tiktok-card/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/tiktok-card/generate-short-link` | - | - | - | JWT | ✅ 通过 | body {card_id}，返回 card+短链 |
| 7 | - | - | GET | `/api/tiktok-card/stats/overall` | - | - | - | JWT | ✅ 通过 | total_cards/active_cards |
| 8 | - | - | GET | `/api/tiktok-card/{cardId}/stats` | - | - | - | JWT | ✅ 通过 | card_id/view_count |

### tuning.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/admin/tuning/confidence/signals` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/admin/tuning/confidence/signals/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | GET | `/api/admin/tuning/confidence/signals/stats` | `params` | - | params | JWT | ✅ 通过 | bands/days |
| 4 | - | - | GET | `/api/admin/tuning/confidence/calibrations` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 5 | - | - | GET | `/api/admin/tuning/confidence/policies` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 6 | - | - | PUT | `/api/admin/tuning/confidence/policies` | `data` | data | - | JWT | ✅ 通过 | **单对象**（非数组），必填 policy_id/intent_type/base_threshold |
| 7 | - | - | GET | `/api/admin/tuning/humanize/scores` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 8 | - | - | GET | `/api/admin/tuning/humanize/scores/stats` | `params` | - | params | JWT | ✅ 通过 | avg/pass_rate |
| 9 | - | - | GET | `/api/admin/tuning/humanize/baselines` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 10 | - | - | GET | `/api/admin/tuning/humanize/low-quality` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 11 | - | - | GET | `/api/admin/tuning/feedback/events` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 12 | - | - | GET | `/api/admin/tuning/feedback/events/stats` | `params` | - | params | JWT | ✅ 通过 | signals/days |
| 13 | - | - | GET | `/api/admin/tuning/feedback/dialogues` | `params` | - | params | JWT | ✅ 通过 | {list,next_cursor} |
| 14 | - | - | GET | `/api/admin/tuning/prompt/candidates` | `params` | - | params | JWT | ✅ 通过 | {list,next_cursor} |
| 15 | - | - | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | `id, data` | data | - | JWT | 🔧 已修复 | 非数字 id 曾把 SQL 原文（`invalid input syntax for type bigint … SQLSTATE 22P02`）当 400 文案回显 → 入口校验 id + 去掉 err.Error() 拼接；回归 TestUpdatePromptCandidateStatus_RejectsNonNumericIDWithoutSQLLeak |
| 16 | - | - | GET | `/api/admin/tuning/bandit/arms` | `params` | - | params | JWT | ✅ 通过 | {list,next_cursor} |

### unifiedMessage.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | unifiedMessageApi | getMessages | GET | `/api/unified-messages` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | unifiedMessageApi | getMessageById | GET | `/api/unified-messages/{id}` | `id` | - | - | JWT | ✅ 通过 | 全量行 |

### unknownQuestion.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/unknown-questions` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/unknown-questions/stats` | - | - | - | JWT | ✅ 通过 | total/adopted/pending/adopt_rate |
| 3 | - | - | GET | `/api/unknown-questions/{id}` | `id` | - | - | JWT | ✅ 通过 | {question,cluster} |
| 4 | - | - | POST | `/api/unknown-questions/{id}/submit` | `id` | - | - | JWT | ✅ 通过 | draft → pending_review |
| 5 | - | - | POST | `/api/unknown-questions/{id}/review` | `id` | data | - | JWT | ✅ 通过 | action 枚举 submit/adopt/ignore/merge；adopt → 生成 FAQ 并 adopted；非法迁移 → 400「非法状态迁移」 |
| 6 | - | - | GET | `/api/unknown-questions/clusters` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 7 | - | - | GET | `/api/unknown-questions/clusters/{id}` | `id` | - | - | JWT | ✅ 通过 | {cluster} 含 sample_questions |
| 8 | - | - | POST | `/api/unknown-questions/clusters/{id}/review` | `id` | data | - | JWT | ✅ 通过 | ignore 生效（夹具已清理） |

### userSegment.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/user-segment/rfm/list` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 2 | - | - | GET | `/api/user-segment/rfm/rule` | - | - | - | JWT | ✅ 通过 | 当前规则 |
| 3 | - | - | POST | `/api/user-segment/rfm/rule` | `data` | data | - | JWT | ✅ 通过 | 保存成功（r/f/m 分段阈值） |
| 4 | - | - | PUT | `/api/user-segment/rfm/rule/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 全量语义（只传部分阈值会把其余置 0，前端为整表单提交） |
| 5 | - | - | GET | `/api/user-segment/rfm/user?user_id=` | `userId` | - | user_id | JWT | ✅ 通过 | 返回 r/f/m 分与分层；不存在 → 404「未找到客户 RFM 信息」 |
| 6 | - | - | GET | `/api/user-segment/rfm/stats` | - | - | - | JWT | ✅ 通过 | layer_count/segment_count |
| 7 | - | - | POST | `/api/user-segment/rfm/calculate` | `data` | data | - | JWT | ✅ 通过 | {updated_count} |
| 8 | - | - | GET | `/api/user-segment/layers` | - | - | - | JWT | ✅ 通过 | 分层定义 |
| 9 | - | - | GET | `/api/user-segments` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 10 | - | - | DELETE | `/api/user-segment/rfm/rule/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 11 | - | - | GET | `/api/user-segment/rfm/list?segment_id=` | `id, params` | - | params | JWT | ✅ 通过 | segment_id 过滤生效（total 131） |

### wecomAccount.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/wecom/health/accounts` | - | - | - | JWT | ✅ 通过 | 健康度列表 |
| 2 | - | - | GET | `/api/wecom/health/accounts/risks` | - | - | - | JWT | ✅ 通过 | 风险账号数组 |
| 3 | - | - | GET | `/api/wecom/health/accounts/select` | - | - | - | JWT | ✅ 通过 | 选择器视图 |
| 4 | - | - | GET | `/api/wecom/health/accounts/summary` | - | - | - | JWT | ✅ 通过 | total/online/offline 汇总 |
| 5 | - | - | GET | `/api/wecom/health/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | {id} 为健康记录 id（非账号 id）；账号无健康记录 → 404 |
| 6 | - | - | GET | `/api/wecom/health/accounts/{id}/history` | `id, params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 7 | - | - | POST | `/api/wecom/health/accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 上报成功（可带 health_score） |
| 8 | - | - | POST | `/api/wecom/health/accounts/{id}/status` | `id, data` | {login_state} | - | JWT | ✅ 通过 | 字段名是 login_state（非 status）；缺 → 400 |
| 9 | - | - | POST | `/api/wecom/health/accounts/{id}/quota/consume` | `id, data` | {count} | - | JWT | ✅ 通过 | 消耗成功 |
| 10 | - | - | POST | `/api/wecom/health/accounts/quota/reset` | - | - | - | JWT | ✅ 通过 | {affected:N} |
| 11 | - | - | POST | `/api/wecom/messages/ingest` | `data` | data | - | JWT | ✅ 通过 | 必填 account_id(数字)/external_user_id；落 message_hub + inbox |
| 12 | - | - | POST | `/api/wecom/messages/send` | `data` | data | - | JWT | ✅ 通过 | 假凭据 → 400 带渠道真因（invalid corpid）；状态码应为 502，记二次审核 |
| 13 | - | - | POST | `/api/wecom/accounts` | `data` | data | - | JWT | ✅ 通过 | 必填 corp_id/agent_id（agent_id 必须是数字） |
| 14 | - | - | PUT | `/api/wecom/accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 复用创建结构（全量语义，必带 corp_id） |
| 15 | - | - | DELETE | `/api/wecom/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 16 | - | - | GET | `/api/wecom/customers` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 17 | - | - | GET | `/api/wecom/groups` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 18 | - | - | GET | `/api/wecom/tags` | - | - | - | JWT | ✅ 通过 | 数组 |
| 19 | - | - | GET | `/api/wecom/messages` | `params` | - | params | JWT | ✅ 通过 | {list} 分页 |
| 20 | - | - | POST | `/api/wecom/accounts/{id}/sync-customers` | `id` | - | - | JWT | ✅ 通过 | 假凭据 → 502 UPSTREAM_FAILURE_5005 + 渠道真因 |
| 21 | - | - | POST | `/api/wecom/accounts/{id}/sync-groups` | `id` | - | - | JWT | ✅ 通过 | 同上 |
| 22 | - | - | POST | `/api/wecom/accounts/{id}/sync-tags` | `id` | - | - | JWT | ✅ 通过 | 同上 |
| 23 | - | - | POST | `/api/wecom/accounts/{id}/refresh` | `id` | - | - | JWT | ✅ 通过 | 同上 |
| 24 | - | - | POST | `/api/wecom/accounts/{id}/send-message` | `id, data` | data | - | JWT | ✅ 通过 | 必填 msg_type；假凭据 → 502 + 渠道真因 |

### whatsapp.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listAccounts | GET | `/api/whatsapp/accounts` | `params` | - | params | JWT | ✅ 通过 | 账号数组 |
| 2 | - | createAccount | POST | `/api/whatsapp/accounts` | `data` | data | - | JWT | ✅ 通过 | 创建成功（status=pending） |
| 3 | - | startLogin | POST | `/api/whatsapp/accounts/{id}/login/start` | `id` | - | - | JWT | ✅ 通过 | 个人版扫码登录已下线 → 409/8401 WHATSAPP_LOGIN_RETIRED_8401，提示改用 Cloud API（业务决策，非缺陷） |
| 4 | - | loginStatus | GET | `/api/whatsapp/accounts/{id}/login/status` | `id` | - | - | JWT | ✅ 通过 | {logged_in,qr} |
| 5 | - | updateAccount | PUT | `/api/whatsapp/accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 改名生效 |
| 6 | - | deleteAccount | DELETE | `/api/whatsapp/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 7 | - | listDrafts | GET | `/api/whatsapp/drafts` | `params` | - | params | JWT | ✅ 通过 | {list,total}（S-31 lane 已改为分页口径） |
| 8 | - | createDraft | POST | `/api/whatsapp/drafts` | `data` | data | - | JWT | ✅ 通过 | 建成功 |
| 9 | - | updateDraft | PUT | `/api/whatsapp/drafts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 10 | - | deleteDraft | DELETE | `/api/whatsapp/drafts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 11 | - | listJobs | GET | `/api/whatsapp/jobs` | `params` | - | params | JWT | ✅ 通过 | {list,total} |
| 12 | - | createJob | POST | `/api/whatsapp/jobs` | `data` | data | - | JWT | ✅ 通过 | 契约是 `{draft_id}`（由草稿生成群发任务），非直接传收件人；缺/非法 UUID → 400「草稿ID错误」 |
| 13 | - | getJob | GET | `/api/whatsapp/jobs/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 14 | - | deleteJob | DELETE | `/api/whatsapp/jobs/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |

### whatsappCloud.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/whatsapp-cloud/accounts` | `params` | - | params | JWT | ✅ 通过 | 数组；access_token 掩码回显 |
| 2 | - | - | GET | `/api/whatsapp-cloud/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/whatsapp-cloud/accounts` | `data` | data | - | JWT | ✅ 通过 | 必填 account_name/phone_number_id/whatsapp_business_id/access_token |
| 4 | - | - | PUT | `/api/whatsapp-cloud/accounts/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 更新成功 |
| 5 | - | - | DELETE | `/api/whatsapp-cloud/accounts/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | POST | `/api/whatsapp-cloud/accounts/{id}/test-send` | `id, data` | data | - | JWT | 🔧 已修复 | 渠道超时/凭据失效曾回 500「发送失败」（裸 error 被 ErrorFromDB 终态掩码）→ 控制器声明 bizerr 上游失败，改 502 + 渠道真因；实测回归 502 |

### workflowOrchestrator.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/workflows/versions` | `workflowIdOrParams` | - | params | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/workflows/versions` | `workflowIdOrParams` | - | - | JWT | ✅ 通过 | 同端点（前端三种调用形态） |
| 3 | - | - | GET | `/api/workflows/versions` | `workflowIdOrParams` | - | params | JWT | ✅ 通过 | 同上 |
| 4 | - | - | GET | `/api/workflows/versions/{id}` | `id` | - | - | JWT | ✅ 通过 | 单条 |
| 5 | - | - | POST | `/api/workflows/versions` | `data` | data | - | JWT | ✅ 通过 | 建 draft（version 自增） |
| 6 | - | - | PUT | `/api/workflows/versions/{id}` | `id, data` | data | - | JWT | ✅ 通过 | 必填 name + definition（缺 definition → 400） |
| 7 | - | - | DELETE | `/api/workflows/versions/{id}` | `id` | - | - | JWT | ✅ 通过 | 删除成功 |
| 8 | - | - | POST | `/api/workflows/versions/{id}/publish` | `id` | - | - | JWT | ✅ 通过 | 发布成功 |
| 9 | - | - | POST | `/api/workflows/versions/{id}/archive` | `id` | - | - | JWT | ✅ 通过 | 归档成功 |
| 10 | - | - | POST | `/api/workflows/execute` | `data` | data | - | JWT | ✅ 通过 | 无已发布版本 → 400「no published version found」；发布后建执行(status=running) |
| 11 | - | - | GET | `/api/workflows/executions` | `params` | - | params | JWT | ✅ 通过 | {list} |
| 12 | - | - | GET | `/api/workflows/executions/{id}` | `id` | - | - | JWT | ✅ 通过 | 执行详情 |
| 13 | - | - | GET | `/api/workflows/executions/{executionId}/nodes` | `executionId` | - | - | JWT | ✅ 通过 | 节点执行数组 |
| 14 | - | - | POST | `/api/workflows/executions/{id}/stop` | `id` | - | - | JWT | ✅ 通过 | 停止成功 |

### xianyuCard.js
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/xianyu/list` | - | - | - | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/xianyu/{id}` | - | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/xianyu/create` | - | - | - | JWT | ✅ 通过 | 创建成功 |
| 4 | - | - | PUT | `/api/xianyu/update` | - | - | - | JWT | ✅ 通过 | 必填 title+image_url（缺 image_url → 400） |
| 5 | - | - | DELETE | `/api/xianyu/delete/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | GET | `/api/xianyu/view/{id}` | - | - | - | JWT | ✅ 通过 | 视图数据 |
| 7 | - | - | GET | `/api/xianyu/stats/card/{id}` | - | - | - | JWT | ✅ 通过 | chart+统计 |
| 8 | - | - | GET | `/api/xianyu/stats/overall` | - | - | - | JWT | ✅ 通过 | chart 聚合 |
| 9 | - | - | POST | `/api/xianyu/{id}/generate-short-link` | - | - | - | JWT | ✅ 通过 | 调用成功；本环境未配域名池 → short_code 为空串（非缺陷） |

### xiaohongshuCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/xiaohongshu/list` | - | - | - | JWT | ✅ 通过 | {list} |
| 2 | - | - | GET | `/api/xiaohongshu/{id}` | - | - | - | JWT | ✅ 通过 | 单条 |
| 3 | - | - | POST | `/api/xiaohongshu/create` | - | - | - | JWT | ✅ 通过 | 创建成功 |
| 4 | - | - | PUT | `/api/xiaohongshu/update` | - | - | - | JWT | ✅ 通过 | 必填 image_url |
| 5 | - | - | DELETE | `/api/xiaohongshu/delete/{id}` | - | - | - | JWT | ✅ 通过 | 删除成功 |
| 6 | - | - | GET | `/api/xiaohongshu/view/{id}` | - | - | - | JWT | ✅ 通过 | 视图数据 |
| 7 | - | - | GET | `/api/xiaohongshu/stats/card/{id}` | - | - | - | JWT | ✅ 通过 | 卡片统计 |
| 8 | - | - | GET | `/api/xiaohongshu/stats/overall` | - | - | - | JWT | ✅ 通过 | 总体统计 |
| 9 | - | - | POST | `/api/xiaohongshu/{id}/generate-short-link` | - | - | - | JWT | ✅ 通过 | 短链生成成功 |


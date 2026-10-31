# API 全量清单（user-web/src/api → user-server）

> 生成：`node user-web/scripts/gen-api-list.mjs`　扫描文件：124 个　端点数：1128 条
> 状态图例：`☐ 待测` · `✅ 通过` · `🔧 已修复` · `⛔ 阻塞` · `➖ 不适用`（off=独立部署 / on=SaaS 双形态各记一次）
> 复核判据：HTTP 状态 + 响应体 `code` + 数据落库（写操作）+ 租户隔离（on 模式）

## 汇总

| 模块 | 端点数 | 待测 | 通过 | 修复 | 阻塞 |
|---|---|---|---|---|---|
| users.js | 8 | 0 | 5 | 3 | 0 |
| tenant.js | 2 | 0 | 2 | 0 | 0 |
| permission.js | 3 | 0 | 2 | 1 | 0 |
| role.js | 3 | 0 | 3 | 0 | 0 |
| systemUser.js | 5 | 0 | 3 | 2 | 0 |
| system.js | 3 | 0 | 3 | 0 | 0 |
| menu.js | 4 | 0 | 4 | 0 | 0 |
| configParams.js | 8 | 0 | 8 | 0 | 0 |
| moduleMarket.js | 17 | 0 | 17 | 0 | 0 |
| platform.js | 2 | 0 | 2 | 0 | 0 |
| platformAccount.js | 8 | 0 | 7 | 1 | 0 |
| abExperiment.js | 11 | 0 | 10 | 1 | 0 |
| abExperimentPlus.js | 5 | 0 | 0 | 5 | 0 |
| agentKBBinding.js | 5 | 0 | 2 | 3 | 0 |
| aiAgent.js | 9 | 0 | 6 | 3 | 0 |
| aiProductivity.js | 2 | 0 | 1 | 1 | 0 |
| aiTool.js | 7 | 0 | 6 | 1 | 0 |
| approval.js | 2 | 0 | 2 | 0 | 0 |
| assetBundle.js | 16 | 0 | 6 | 10 | 0 |
| assetMarket.js | 12 | 0 | 8 | 4 | 0 |
| backup.js | 7 | 0 | 3 | 4 | 0 |
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
| churnPrediction.js | 12 | 0 | 5 | 7 | 0 |
| clue.js | 4 | 0 | 0 | 4 | 0 |
| cockpit.js | 5 | 0 | 3 | 2 | 0 |
| collaboration.js | 9 | 0 | 4 | 5 | 0 |
| community.js | 10 | 0 | 1 | 9 | 0 |
| contactConsent.js | 4 | 0 | 0 | 4 | 0 |
| conversionFunnel.js | 2 | 0 | 0 | 2 | 0 |
| csat.js | 7 | 0 | 3 | 4 | 0 |
| customer360.js | 10 | 0 | 2 | 8 | 0 |
| customerEvent.js | 11 | 0 | 2 | 9 | 0 |
| customerJourney.js | 6 | 0 | 3 | 3 | 0 |
| customerService.js | 21 | 0 | 8 | 13 | 0 |
| customerServiceAgent.js | 7 | 0 | 1 | 6 | 0 |
| customerSession.js | 18 | 0 | 8 | 10 | 0 |
| customIntegration.js | 8 | 0 | 1 | 7 | 0 |
| customReport.js | 10 | 0 | 1 | 9 | 0 |
| dashboardScreen.js | 12 | 12 | 0 | 0 | 0 |
| dialogueMemory.js | 10 | 10 | 0 | 0 | 0 |
| dingtalkApp.js | 6 | 6 | 0 | 0 | 0 |
| domainPool.js | 12 | 12 | 0 | 0 | 0 |
| douyinCard.js | 9 | 9 | 0 | 0 | 0 |
| egolite.js | 39 | 39 | 0 | 0 | 0 |
| email.js | 17 | 17 | 0 | 0 | 0 |
| faq.js | 8 | 8 | 0 | 0 | 0 |
| featureFlags.js | 11 | 11 | 0 | 0 | 0 |
| feishu.js | 7 | 7 | 0 | 0 | 0 |
| followup.js | 5 | 5 | 0 | 0 | 0 |
| geo.js | 49 | 49 | 0 | 0 | 0 |
| geoAlert.js | 4 | 4 | 0 | 0 | 0 |
| geoAutopilot.js | 3 | 3 | 0 | 0 | 0 |
| geoDecision.js | 3 | 3 | 0 | 0 | 0 |
| geoEntity.js | 18 | 18 | 0 | 0 | 0 |
| geoPlatform.js | 6 | 6 | 0 | 0 | 0 |
| geoProbe.js | 18 | 18 | 0 | 0 | 0 |
| glossary.js | 6 | 6 | 0 | 0 | 0 |
| humanTask.js | 7 | 7 | 0 | 0 | 0 |
| i18nStats.js | 6 | 6 | 0 | 0 | 0 |
| inbox.js | 17 | 17 | 0 | 0 | 0 |
| integration.js | 13 | 13 | 0 | 0 | 0 |
| intentRecognition.js | 12 | 12 | 0 | 0 | 0 |
| kbConnector.js | 6 | 6 | 0 | 0 | 0 |
| kbCrawler.js | 14 | 14 | 0 | 0 | 0 |
| knowledge.js | 27 | 27 | 0 | 0 | 0 |
| knowledgeBase.js | 7 | 7 | 0 | 0 | 0 |
| knowledgeMerchant.js | 14 | 14 | 0 | 0 | 0 |
| kuaishouCard.js | 11 | 11 | 0 | 0 | 0 |
| leadMining.js | 2 | 2 | 0 | 0 | 0 |
| livecode.js | 12 | 12 | 0 | 0 | 0 |
| llmReport.js | 4 | 4 | 0 | 0 | 0 |
| llmRouting.js | 20 | 20 | 0 | 0 | 0 |
| ltcConfig.js | 2 | 2 | 0 | 0 | 0 |
| ltcRates.js | 1 | 1 | 0 | 0 | 0 |
| marketingFlow.js | 10 | 10 | 0 | 0 | 0 |
| material.js | 9 | 9 | 0 | 0 | 0 |
| mcpCredential.js | 4 | 4 | 0 | 0 | 0 |
| messageHub.js | 9 | 9 | 0 | 0 | 0 |
| monitor.js | 17 | 17 | 0 | 0 | 0 |
| notification.js | 4 | 4 | 0 | 0 | 0 |
| objection.js | 4 | 4 | 0 | 0 | 0 |
| obs.js | 8 | 8 | 0 | 0 | 0 |
| observabilityDashboard.js | 1 | 1 | 0 | 0 | 0 |
| oneid.js | 10 | 10 | 0 | 0 | 0 |
| operationLog.js | 5 | 5 | 0 | 0 | 0 |
| orderDraft.js | 6 | 6 | 0 | 0 | 0 |
| persona.js | 2 | 2 | 0 | 0 | 0 |
| qqBot.js | 7 | 7 | 0 | 0 | 0 |
| ragProductConfig.js | 8 | 8 | 0 | 0 | 0 |
| reachPipeline.js | 18 | 18 | 0 | 0 | 0 |
| runtimeConfig.js | 6 | 6 | 0 | 0 | 0 |
| salesWorkbench.js | 4 | 4 | 0 | 0 | 0 |
| scriptAb.js | 7 | 7 | 0 | 0 | 0 |
| scriptTemplate.js | 9 | 9 | 0 | 0 | 0 |
| securityAudit.js | 3 | 3 | 0 | 0 | 0 |
| shortLink.js | 10 | 10 | 0 | 0 | 0 |
| sms.js | 20 | 20 | 0 | 0 | 0 |
| sopAgent.js | 16 | 16 | 0 | 0 | 0 |
| sopTemplate.js | 7 | 7 | 0 | 0 | 0 |
| stats.js | 1 | 1 | 0 | 0 | 0 |
| tagSegmentation.js | 10 | 10 | 0 | 0 | 0 |
| telegram.js | 13 | 13 | 0 | 0 | 0 |
| templateMarket.js | 13 | 13 | 0 | 0 | 0 |
| tiktokCard.js | 8 | 8 | 0 | 0 | 0 |
| tuning.js | 16 | 16 | 0 | 0 | 0 |
| unifiedMessage.js | 2 | 2 | 0 | 0 | 0 |
| unknownQuestion.js | 8 | 8 | 0 | 0 | 0 |
| userSegment.js | 11 | 11 | 0 | 0 | 0 |
| wecomAccount.js | 24 | 24 | 0 | 0 | 0 |
| whatsapp.js | 14 | 14 | 0 | 0 | 0 |
| whatsappCloud.js | 6 | 6 | 0 | 0 | 0 |
| workflowOrchestrator.js | 14 | 14 | 0 | 0 | 0 |
| xianyuCard.js | 9 | 9 | 0 | 0 | 0 |
| xiaohongshuCard.js | 9 | 9 | 0 | 0 | 0 |
| **合计** | **1128** | **1128** | 0 | 0 | 0 |

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
| 2 | badCaseApi | stats | GET | `{base}/stats` | - | undefined | - | JWT | ✅ 通过 | by_status/by_fix_layer/labeled_ratio；on(8206) 只统计本租户（全 0） |
| 3 | badCaseApi | taxonomy | GET | `{base}/taxonomy` | - | undefined | - | JWT | ✅ 通过 | sources/statuses/labels/fix_layers + label_layer 映射，全来自 model 常量 |
| 4 | badCaseApi | get | GET | `{base}/{expr}` | `id` | - | - | JWT | ✅ 通过 | 返回完整行（含 label/label_note/labeler_id/fix_layer）；不存在 → 404「坏例记录不存在（id=…)」 |
| 5 | badCaseApi | create | POST | `/api/bad-cases` | `input` | input | - | JWT | 🔧 已修复 | 手录 pending/manual、dedup_key 用自身 id（同 session 可补多条）；缺 query/answer → 400，文案已去哨兵前缀 |
| 6 | badCaseApi | label | POST | `{base}/{expr}/label` | `id, label, note` | {label,note} | - | JWT | 🔧 已修复 | kb_missing → labeled + fix_layer=knowledge、labeler_id=62；重复打标 409；非法类目/缺 note → 400（文案已清洗） |
| 7 | badCaseApi | dismiss | POST | `{base}/{expr}/dismiss` | `id, reason` | {reason} | - | JWT | 🔧 已修复 | 缺理由 400「撤销必须写明理由」；已导出/已裁决再撤 → 409「当前状态不允许该操作」 |
| 8 | badCaseApi | exportSet | POST | `{base}/export` | `labels, limit` | {labels,limit} | - | JWT | 🔧 已修复 | 已打标行转 exported 并回 {eval_set_id,count,rows}；队列空 → 409「没有可导出的已打标样本」；limit<0 → 400 |

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
| 1 | contactConsentApi | getTimeline | GET | `{base}/{customerId}/consents` | `customerId, params` | - | params | JWT | 🔧 已修复 | L11 |
| 2 | contactConsentApi | record | POST | `{base}/{customerId}/consents` | `customerId, data` | data | - | JWT | 🔧 已修复 | L15 |
| 3 | contactConsentApi | getPreferences | GET | `{base}/{customerId}/preferences` | `customerId` | - | - | JWT | 🔧 已修复 | L19 |
| 4 | contactConsentApi | setPreference | PUT | `{base}/{customerId}/preferences` | `customerId, data` | data | - | JWT | 🔧 已修复 | L23 |

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
| 1 | - | - | GET | `/api/dashboards` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/dashboards/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | POST | `/api/dashboards` | - | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | - | PUT | `/api/dashboards/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 5 | - | - | DELETE | `/api/dashboards/{id}` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | - | GET | `/api/dashboards/public/{code}` | - | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | ☐ 待测 | L23 |
| 8 | - | - | GET | `/api/dashboards/activities` | - | - | - | JWT | ☐ 待测 | L26 |
| 9 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | ☐ 待测 | L29 |
| 10 | - | - | GET | `/api/dashboards/data` | - | - | params | JWT | ☐ 待测 | L32 |
| 11 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | ☐ 待测 | L35 |
| 12 | - | - | GET | `/api/dashboards/data` | - | - | - | JWT | ☐ 待测 | L38 |

### dialogueMemory.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | memoryApi | appendMessage | POST | `/api/memory/messages` | `data` | data | - | JWT | ☐ 待测 | L5 |
| 2 | memoryApi | getShortTerm | GET | `/api/memory/short` | `params` | - | params | JWT | ☐ 待测 | L9 |
| 3 | memoryApi | getLongTerm | GET | `/api/memory/long` | `params` | - | params | JWT | ☐ 待测 | L13 |
| 4 | memoryApi | updateKeyFacts | POST | `/api/memory/facts` | `data` | data | - | JWT | ☐ 待测 | L17 |
| 5 | memoryApi | recordObjection | POST | `/api/memory/objections` | `data` | data | - | JWT | ☐ 待测 | L21 |
| 6 | memoryApi | updatePurchaseIntent | POST | `/api/memory/purchase-intent` | `data` | data | - | JWT | ☐ 待测 | L25 |
| 7 | memoryApi | recordIntent | POST | `/api/memory/intent-trail` | `data` | data | - | JWT | ☐ 待测 | L29 |
| 8 | memoryApi | recordSOP | POST | `/api/memory/sop-history` | `data` | data | - | JWT | ☐ 待测 | L33 |
| 9 | memoryApi | buildContext | GET | `/api/memory/context` | `params` | - | params | JWT | ☐ 待测 | L37 |
| 10 | memoryApi | list | GET | `/api/memory/list` | `params` | - | params | JWT | ☐ 待测 | L41 |

### dingtalkApp.js

| 1 | - | - | GET | `/api/dingtalk-app/accounts` | - | - | params | JWT | ☐ 待测 | L6 |
| 2 | - | - | GET | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L10 |
| 3 | - | - | POST | `/api/dingtalk-app/accounts` | - | data | - | JWT | ☐ 待测 | L14 |
| 4 | - | - | PUT | `{BASE}/{id}` | - | data | - | JWT | ☐ 待测 | L18 |
| 5 | - | - | DELETE | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L22 |
| 6 | - | - | POST | `{BASE}/{id}/test` | - | - | - | JWT | ☐ 待测 | L26 |
| 3 | - | - | DELETE | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L22 |
| 4 | - | - | POST | `{BASE}/{id}/test` | - | - | - | JWT | ☐ 待测 | L26 |

### domainPool.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getDomainHealth | GET | `/api/domain-pool/health` | - | - | - | JWT | ☐ 待测 | L3 |
| 2 | - | checkBlacklist | GET | `/api/domain-pool/{domain}/blacklist` | `domain` | - | - | JWT | ☐ 待测 | L4 |
| 3 | - | suspendDomain | POST | `/api/domain-pool/{id}/suspend` | `id` | {} | - | JWT | ☐ 待测 | L5 |
| 4 | - | rotateToBackup | POST | `/api/domain-pool/{id}/rotate` | `id` | {} | - | JWT | ☐ 待测 | L6 |
| 5 | - | listAlerts | GET | `/api/domain-pool/alerts` | `params` | - | params | JWT | ☐ 待测 | L7 |
| 6 | - | resolveAlert | POST | `/api/domain-pool/alerts/{id}/resolve` | `id` | {} | - | JWT | ☐ 待测 | L8 |
| 7 | domainPoolApi | getList | GET | `/api/domain-pool` | `params` | - | params | JWT | ☐ 待测 | L11 |
| 8 | domainPoolApi | create | POST | `/api/domain-pool` | `data` | data | - | JWT | ☐ 待测 | L12 |
| 9 | domainPoolApi | update | PUT | `/api/domain-pool/{id}` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 10 | domainPoolApi | delete | DELETE | `/api/domain-pool/{id}` | `id` | - | - | JWT | ☐ 待测 | L14 |
| 11 | domainPoolApi | checkDomain | POST | `/api/domain-pool/{id}/check` | `id` | {} | - | JWT | ☐ 待测 | L15 |
| 12 | domainPoolApi | checkAllDomains | POST | `/api/domain-pool/check-all` | - | {} | - | JWT | ☐ 待测 | L16 |

### douyinCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/douyin/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/douyin/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/douyin/create` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/douyin/update` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/douyin/delete/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/douyin/view/{id}` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/douyin/stats/card/{id}` | - | - | params | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/douyin/stats/overall` | - | - | params | JWT | ☐ 待测 | L32 |
| 9 | - | - | POST | `/api/douyin/{id}/generate-short-link` | - | - | - | JWT | ☐ 待测 | L36 |

### egolite.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listEgoFlows | GET | `/api/egolite/flows` | `params` | - | params | JWT | ☐ 待测 | L12 |
| 2 | - | getEgoFlow | GET | `/api/egolite/flows/{id}` | `id` | - | - | JWT | ☐ 待测 | L14 |
| 3 | - | rescanEgoFlow | POST | `/api/egolite/flows/{id}/rescan` | `id` | - | - | JWT | ☐ 待测 | L20 |
| 4 | - | publishEgoFlow | POST | `/api/egolite/flows/{id}/publish` | `id` | - | - | JWT | ☐ 待测 | L22 |
| 5 | - | submitEgoFlow | POST | `/api/egolite/flows/{id}/submit` | `id` | - | - | JWT | ☐ 待测 | L24 |
| 6 | - | dryRunEgoFlow | POST | `/api/egolite/flows/{id}/dry-run` | `id, data` | data | - | JWT | ☐ 待测 | L27 |
| 7 | - | deleteEgoFlow | DELETE | `/api/egolite/flows/{id}` | `id` | - | - | JWT | ☐ 待测 | L29 |
| 8 | - | getEgoFlowCron | GET | `/api/egolite/flows/{id}/cron` | `id` | - | - | JWT | ☐ 待测 | L32 |
| 9 | - | upsertEgoFlowCron | POST | `/api/egolite/flows/{id}/cron` | `id, data` | data | - | JWT | ☐ 待测 | L33 |
| 10 | - | deleteEgoFlowCron | DELETE | `/api/egolite/flows/{id}/cron` | `id` | - | - | JWT | ☐ 待测 | L34 |
| 11 | - | listEgoExecutions | GET | `/api/egolite/executions` | `params` | - | params | JWT | ☐ 待测 | L38 |
| 12 | - | getEgoExecution | GET | `/api/egolite/executions/{id}` | `id` | - | - | JWT | ☐ 待测 | L40 |
| 13 | - | listEgoEvents | GET | `/api/egolite/executions/{id}/events` | `id, params` | - | params | JWT | ☐ 待测 | L43 |
| 14 | - | startEgoExecution | POST | `/api/egolite/executions` | `data` | data | - | JWT | ☐ 待测 | L110 |
| 15 | - | abortEgoExecution | POST | `/api/egolite/executions/{id}/abort` | `id` | - | - | JWT | ☐ 待测 | L112 |
| 16 | - | redispatchEgoExecution | POST | `/api/egolite/executions/{id}/redispatch` | `id` | {} | - | JWT | ☐ 待测 | L116 |
| 17 | - | confirmEgoExecution | POST | `/api/egolite/executions/{id}/confirm` | `id, allow` | {allow} | - | JWT | ☐ 待测 | L119 |
| 18 | - | getEgoRunnerStatus | GET | `/api/egolite/runner/status` | - | - | - | JWT | ☐ 待测 | L123 |
| 19 | - | listEgoRunnerTokens | GET | `/api/egolite/runner/tokens` | - | - | - | JWT | ☐ 待测 | L126 |
| 20 | - | issueEgoRunnerToken | POST | `/api/egolite/runner/tokens` | `label` | {label} | - | JWT | ☐ 待测 | L129 |
| 21 | - | revokeEgoRunnerToken | POST | `/api/egolite/runner/tokens/{id}/revoke` | `id` | - | - | JWT | ☐ 待测 | L132 |
| 22 | - | listEgoAssets | GET | `/api/egolite/assets` | - | - | - | JWT | ☐ 待测 | L137 |
| 23 | - | listEgoAdminAssets | GET | `/api/egolite/admin/assets` | `params` | - | params | JWT | ☐ 待测 | L140 |
| 24 | - | listEgoAdminFlows | GET | `/api/egolite/admin/flows` | `params` | - | params | JWT | ☐ 待测 | L142 |
| 25 | - | publishEgoAssetFromFlow | POST | `/api/egolite/admin/assets/publish` | `flowId` | {flow_id} | - | JWT | ☐ 待测 | L145 |
| 26 | - | publishEgoAssetPack | POST | `/api/egolite/admin/assets/pack` | `data` | data | - | JWT | ☐ 待测 | L147 |
| 27 | - | offlineEgoAsset | POST | `/api/egolite/admin/assets/{id}/offline` | `id` | - | - | JWT | ☐ 待测 | L149 |
| 28 | - | listEgoSessions | GET | `/api/egolite/sessions` | `params` | - | params | JWT | ☐ 待测 | L153 |
| 29 | - | getEgoSession | GET | `/api/egolite/sessions/{id}` | `id` | - | - | JWT | ☐ 待测 | L155 |
| 30 | - | createEgoSession | POST | `/api/egolite/sessions` | `data` | data | - | JWT | ☐ 待测 | L157 |
| 31 | - | runEgoSessionRound | POST | `/api/egolite/sessions/{id}/round` | `id, data` | data | - | JWT | ☐ 待测 | L160 |
| 32 | - | observeEgoSession | POST | `/api/egolite/sessions/{id}/observe` | `id, round` | {round} | - | JWT | ☐ 待测 | L163 |
| 33 | - | listEgoSessionRounds | GET | `/api/egolite/sessions/{id}/rounds` | `id, params` | - | params | JWT | ☐ 待测 | L166 |
| 34 | - | confirmEgoSession | POST | `/api/egolite/sessions/{id}/confirm` | `id, allow` | {allow} | - | JWT | ☐ 待测 | L169 |
| 35 | - | abortEgoSession | POST | `/api/egolite/sessions/{id}/abort` | `id, reason` | {reason} | - | JWT | ☐ 待测 | L172 |
| 36 | - | finishEgoSession | POST | `/api/egolite/sessions/{id}/finish` | `id, keep` | {keep} | - | JWT | ☐ 待测 | L175 |
| 37 | - | getEgoTelemetrySummary | GET | `/api/egolite/admin/telemetry/summary` | `hours` | {hours} | - | JWT | ☐ 待测 | L181 |
| 38 | - | exportEgoSessionAudit | GET | `/api/egolite/sessions/{id}/export` | `id, format` | {format} | - | JWT | ☐ 待测 | L185 |
| 39 | - | saveEgoSessionAsFlow | POST | `/api/egolite/sessions/{id}/save-as-flow` | `id, name` | {name} | - | JWT | ☐ 待测 | L189 |

### email.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | emailApi | getEmailSmtpList | GET | `/api/email/smtp` | - | - | - | JWT | ☐ 待测 | L5 |
| 2 | emailApi | addEmailSmtp | POST | `/api/email/smtp` | `data` | data | - | JWT | ☐ 待测 | L9 |
| 3 | emailApi | updateEmailSmtp | PUT | `/api/email/smtp/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | emailApi | deleteEmailSmtp | DELETE | `/api/email/smtp/{id}` | `id` | - | - | JWT | ☐ 待测 | L17 |
| 5 | emailApi | getDrafts | GET | `/api/email/drafts` | - | - | - | JWT | ☐ 待测 | L21 |
| 6 | emailApi | getDraftDetail | GET | `/api/email/drafts/{id}` | `id` | - | - | JWT | ☐ 待测 | L25 |
| 7 | emailApi | createDraft | POST | `/api/email/drafts` | `data` | data | - | JWT | ☐ 待测 | L29 |
| 8 | emailApi | updateDraft | PUT | `/api/email/drafts/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L33 |
| 9 | emailApi | deleteDraft | DELETE | `/api/email/drafts/{id}` | `id` | - | - | JWT | ☐ 待测 | L37 |
| 10 | emailApi | uploadImage | POST | `/api/upload` | `formData` | formData | - | JWT | ☐ 待测 | L41 |
| 11 | emailApi | sendEmail | POST | `/api/email/send` | `data` | data | - | JWT | ☐ 待测 | L49 |
| 12 | emailApi | getEmailList | GET | `/api/email/list?page={page}&limit={limit}` | `page, limit` | - | - | JWT | ☐ 待测 | L52 |
| 13 | emailApi | getJobsList | GET | `/api/email/jobs?page={page}&limit={limit}` | `page, limit` | - | - | JWT | ☐ 待测 | L56 |
| 14 | emailApi | deleteJob | DELETE | `/api/email/jobs/{id}` | `id` | - | - | JWT | ☐ 待测 | L59 |
| 15 | emailApi | getJobDetail | GET | `/api/email/jobs/{id}` | `id` | - | - | JWT | ☐ 待测 | L62 |
| 16 | emailApi | getEmailTrace | GET | `/api/email/lists/{id}/tracking` | `id` | - | - | JWT | ☐ 待测 | L66 |
| 17 | emailApi | deleteEmailList | DELETE | `/api/email/list/{id}` | `id` | - | - | JWT | ☐ 待测 | L70 |

### faq.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | faqApi | list | GET | `/api/faqs` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | faqApi | get | GET | `/api/faqs/{id}` | `id` | - | - | JWT | ☐ 待测 | L9 |
| 3 | faqApi | create | POST | `/api/faqs` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | faqApi | update | PUT | `/api/faqs/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L17 |
| 5 | faqApi | toggle | PATCH | `/api/faqs/{id}/toggle` | `id, enabled` | {enabled} | - | JWT | ☐ 待测 | L21 |
| 6 | faqApi | remove | DELETE | `/api/faqs/{id}` | `id` | - | - | JWT | ☐ 待测 | L25 |
| 7 | faqApi | match | POST | `/api/faqs/match` | `data` | data | - | JWT | ☐ 待测 | L29 |
| 8 | faqApi | stats | GET | `/api/faqs/stats` | - | - | - | JWT | ☐ 待测 | L33 |

### featureFlags.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/feature-flags` | - | - | params | JWT | ☐ 待测 | L15 |
| 2 | - | - | GET | `/api/feature-flags/stale` | - | - | - | JWT | ☐ 待测 | L21 |
| 3 | - | - | GET | `/api/feature-flags/{id}` | - | - | - | JWT | ☐ 待测 | L25 |
| 4 | - | - | POST | `/api/feature-flags` | - | data | - | JWT | ☐ 待测 | L29 |
| 5 | - | - | PUT | `/api/feature-flags/{id}` | - | data | - | JWT | ☐ 待测 | L33 |
| 6 | - | - | DELETE | `/api/feature-flags/{id}` | - | - | - | JWT | ☐ 待测 | L37 |
| 7 | - | - | POST | `/api/feature-flags/{id}/enable` | - | - | - | JWT | ☐ 待测 | L44 |
| 8 | - | - | POST | `/api/feature-flags/{id}/disable` | - | - | - | JWT | ☐ 待测 | L48 |
| 9 | - | - | POST | `/api/feature-flags/{id}/rollout` | - | {percentage} | - | JWT | ☐ 待测 | L54 |
| 10 | - | - | GET | `/api/feature-flags/{id}/audit` | - | - | - | JWT | ☐ 待测 | L58 |
| 11 | - | - | GET | `/api/feature-flags/{id}/eval-log` | - | - | - | JWT | ☐ 待测 | L62 |

### feishu.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/feishu/accounts` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/feishu/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/feishu/accounts` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/feishu/accounts/{id}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/feishu/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/feishu/accounts/{id}/test-send` | - | data | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/feishu/accounts/{id}/refresh-token` | - | - | - | JWT | ☐ 待测 | L28 |

### followup.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | followupApi | today | GET | `{base}/today` | `ownerId, date` | - | params | JWT | ☐ 待测 | L28 |
| 2 | followupApi | pending | GET | `{base}/pending` | `ownerId, limit` | - | params | JWT | ☐ 待测 | L35 |
| 3 | followupApi | overdue | GET | `{base}/overdue` | `ownerId` | {owner_id} | - | JWT | ☐ 待测 | L39 |
| 4 | followupApi | complete | POST | `{base}/{expr}/complete` | `id, result, note` | {result,note} | - | JWT | ☐ 待测 | L46 |
| 5 | followupApi | cancel | POST | `{base}/{expr}/cancel` | `id` | - | - | JWT | ☐ 待测 | L50 |

### geo.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | geoApi | mineKeywords | POST | `/api/geo/keywords/mine` | `data` | data | - | JWT | ☐ 待测 | L5 |
| 2 | geoApi | semanticExpand | POST | `/api/geo/keywords/expand` | `data` | data | - | JWT | ☐ 待测 | L9 |
| 3 | geoApi | topicCluster | POST | `/api/geo/keywords/cluster` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | geoApi | getKeywordList | GET | `/api/geo/keywords/list` | `params` | - | params | JWT | ☐ 待测 | L17 |
| 5 | geoApi | crawlSuggest | POST | `/api/geo/keyword-mining/crawl-suggest` | `data` | data | - | JWT | ☐ 待测 | L22 |
| 6 | geoApi | combineLongtail | POST | `/api/geo/keyword-mining/longtail` | `data` | data | - | JWT | ☐ 待测 | L26 |
| 7 | geoApi | getKeywordFunnel | GET | `/api/geo/keyword-mining/funnel` | - | - | - | JWT | ☐ 待测 | L30 |
| 8 | geoApi | pushUrls | POST | `/api/geo/push/urls` | `data` | data | - | JWT | ☐ 待测 | L35 |
| 9 | geoApi | getPushQuota | GET | `/api/geo/push/quota` | - | - | - | JWT | ☐ 待测 | L39 |
| 10 | geoApi | getSitemapPreview | GET | `/api/geo/push/sitemap` | - | - | - | JWT | ☐ 待测 | L43 |
| 11 | geoApi | verifyIndexFull | POST | `/api/geo/index-tracking/verify/{articleId}` | `articleId` | - | - | JWT | ☐ 待测 | L48 |
| 12 | geoApi | getIndexFunnel | GET | `/api/geo/index-tracking/funnel` | - | - | - | JWT | ☐ 待测 | L52 |
| 13 | geoApi | verifyAllIndex | POST | `/api/geo/index-tracking/verify-all` | - | - | - | JWT | ☐ 待测 | L56 |
| 14 | geoApi | exportSite | POST | `/api/geo/site/export` | - | - | - | JWT | ☐ 待测 | L61 |
| 15 | geoApi | deploySite | POST | `/api/geo/site/deploy` | - | - | - | JWT | ☐ 待测 | L65 |
| 16 | geoApi | siteFullPipeline | POST | `/api/geo/site/full-pipeline` | - | - | - | JWT | ☐ 待测 | L69 |
| 17 | geoApi | getLlmsTxtPreview | GET | `/api/geo/site/llms-txt-preview` | `domain` | ConditionalExpression | - | JWT | ☐ 待测 | L73 |
| 18 | geoApi | getRobotsTxtPreview | GET | `/api/geo/site/robots-preview` | - | - | - | JWT | ☐ 待测 | L77 |
| 19 | geoApi | listSites | GET | `/api/geo/sites` | `params` | - | params | JWT | ☐ 待测 | L82 |
| 20 | geoApi | createSite | POST | `/api/geo/sites` | `data` | data | - | JWT | ☐ 待测 | L86 |
| 21 | geoApi | updateSite | PUT | `/api/geo/sites/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L90 |
| 22 | geoApi | deleteSite | DELETE | `/api/geo/sites/{id}` | `id` | - | - | JWT | ☐ 待测 | L94 |
| 23 | geoApi | listPushers | GET | `/api/geo/pushers` | `params` | - | params | JWT | ☐ 待测 | L99 |
| 24 | geoApi | createPusher | POST | `/api/geo/pushers` | `data` | data | - | JWT | ☐ 待测 | L103 |
| 25 | geoApi | updatePusher | PUT | `/api/geo/pushers/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L107 |
| 26 | geoApi | deletePusher | DELETE | `/api/geo/pushers/{id}` | `id` | - | - | JWT | ☐ 待测 | L111 |
| 27 | geoApi | listSchemaTemplates | GET | `/api/geo/schema-templates` | `params` | - | params | JWT | ☐ 待测 | L116 |
| 28 | geoApi | createSchemaTemplate | POST | `/api/geo/schema-templates` | `data` | data | - | JWT | ☐ 待测 | L120 |
| 29 | geoApi | updateSchemaTemplate | PUT | `/api/geo/schema-templates/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L124 |
| 30 | geoApi | deleteSchemaTemplate | DELETE | `/api/geo/schema-templates/{id}` | `id` | - | - | JWT | ☐ 待测 | L128 |
| 31 | geoApi | deleteKeyword | DELETE | `/api/geo/keywords/{id}` | `id` | - | - | JWT | ☐ 待测 | L132 |
| 32 | geoApi | generateContent | POST | `/api/geo/content/generate` | `data` | data | - | JWT | ☐ 待测 | L136 |
| 33 | geoApi | batchGenerateContent | POST | `/api/geo/content/batch-generate` | `data` | data | - | JWT | ☐ 待测 | L141 |
| 34 | geoApi | scoreContent | POST | `/api/geo/content/score` | `data` | data | - | JWT | ☐ 待测 | L145 |
| 35 | geoApi | optimizeContent | POST | `/api/geo/content/optimize` | `data` | data | - | JWT | ☐ 待测 | L149 |
| 36 | geoApi | enhanceEEAT | POST | `/api/geo/content/eeat` | `data` | data | - | JWT | ☐ 待测 | L153 |
| 37 | geoApi | generateSchema | POST | `/api/geo/content/schema` | `data` | data | - | JWT | ☐ 待测 | L157 |
| 38 | geoApi | checkUniqueness | POST | `/api/geo/content/uniqueness` | `data` | data | - | JWT | ☐ 待测 | L161 |
| 39 | geoApi | getArticleList | GET | `/api/geo/content/list` | `params` | - | params | JWT | ☐ 待测 | L165 |
| 40 | geoApi | getArticleByID | GET | `/api/geo/content/{id}` | `id` | - | - | JWT | ☐ 待测 | L169 |
| 41 | geoApi | verifyArticle | POST | `/api/geo/verification/verify` | `data` | data | - | JWT | ☐ 待测 | L173 |
| 42 | geoApi | monitorNegative | POST | `/api/geo/verification/negative` | `data` | data | - | JWT | ☐ 待测 | L177 |
| 43 | geoApi | getVerifyResults | GET | `/api/geo/verification/results/{articleId}` | `articleId` | - | - | JWT | ☐ 待测 | L181 |
| 44 | geoApi | getReport | GET | `/api/geo/reports/summary` | `params` | - | params | JWT | ☐ 待测 | L185 |
| 45 | geoApi | getROI | GET | `/api/geo/reports/roi` | `params` | - | params | JWT | ☐ 待测 | L189 |
| 46 | geoApi | getAPICosts | GET | `/api/geo/reports/api-costs` | `params` | - | params | JWT | ☐ 待测 | L193 |
| 47 | geoApi | getConfig | GET | `/api/geo/config` | - | - | - | JWT | ☐ 待测 | L197 |
| 48 | geoApi | updateConfig | PUT | `/api/geo/config` | `data` | data | - | JWT | ☐ 待测 | L201 |
| 49 | geoApi | optimizeConfig | POST | `/api/geo/config/optimize` | `data` | data | - | JWT | ☐ 待测 | L205 |

### geoAlert.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listGeoAlerts | GET | `/api/geo/alerts` | `params` | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | getGeoAlertsUnreadCount | GET | `/api/geo/alerts/unread-count` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | ackGeoAlert | POST | `/api/geo/alerts/{id}/ack` | `id` | {} | - | JWT | ☐ 待测 | L10 |
| 4 | - | deleteGeoAlert | DELETE | `/api/geo/alerts/{id}` | `id` | - | - | JWT | ☐ 待测 | L13 |

### geoAutopilot.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | geoAutopilotApi | createRun | POST | `/api/geo/autopilot/runs` | `data` | data | - | JWT | ☐ 待测 | L6 |
| 2 | geoAutopilotApi | getRun | GET | `/api/geo/autopilot/runs/{id}` | `id` | - | - | JWT | ☐ 待测 | L10 |
| 3 | geoAutopilotApi | listRuns | GET | `/api/geo/autopilot/runs` | `params` | - | params | JWT | ☐ 待测 | L14 |

### geoDecision.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getGeoDecisionReport | GET | `/api/geo/decision/report` | - | - | - | JWT | ☐ 待测 | L3 |
| 2 | - | getGeoGapTasks | GET | `/api/geo/decision/tasks` | `limit` | {limit} | - | JWT | ☐ 待测 | L6 |
| 3 | - | markGeoTaskDone | POST | `/api/geo/decision/tasks/{id}/done` | `id` | - | - | JWT | ☐ 待测 | L8 |

### geoEntity.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listEntities | GET | `/api/geo/entity/list` | `type, keyword` | {type,keyword} | - | JWT | ☐ 待测 | L4 |
| 2 | - | getEntityRelations | GET | `/api/geo/entity/{entityId}/graph` | `entityId` | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | extractEntities | POST | `/api/geo/entities/extract` | `docId` | {doc_id} | - | JWT | ☐ 待测 | L10 |
| 4 | - | listKBDocuments | GET | `/api/geo/kb/documents` | `keyword, sourceLevel` | {keyword,source_level} | - | JWT | ☐ 待测 | L13 |
| 5 | - | saveKBDocument | POST | `/api/geo/kb/documents` | `data` | data | - | JWT | ☐ 待测 | L16 |
| 6 | - | deleteKBDocument | DELETE | `/api/geo/kb/documents/{id}` | `id` | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | getKBDocument | GET | `/api/geo/kb/documents/{id}` | `id` | - | - | JWT | ☐ 待测 | L22 |
| 8 | - | searchKB | GET | `/api/geo/kb/search` | `q, limit` | {q,limit} | - | JWT | ☐ 待测 | L25 |
| 9 | - | askKB | POST | `/api/geo/kb/ask` | `question` | {question} | - | JWT | ☐ 待测 | L28 |
| 10 | - | listWorkflows | GET | `/api/geo/workflow/workflows` | - | - | - | JWT | ☐ 待测 | L31 |
| 11 | - | getWorkflow | GET | `/api/geo/workflow/workflows/{id}` | `id` | - | - | JWT | ☐ 待测 | L34 |
| 12 | - | saveWorkflow | POST | `/api/geo/workflow/workflows` | `data` | data | - | JWT | ☐ 待测 | L37 |
| 13 | - | updateWorkflow | PUT | `/api/geo/workflow/workflows/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L40 |
| 14 | - | deleteWorkflow | DELETE | `/api/geo/workflow/workflows/{id}` | `id` | - | - | JWT | ☐ 待测 | L43 |
| 15 | - | runWorkflow | POST | `/api/geo/workflow/workflows/{id}/run` | `id, params` | LogicalExpression | - | JWT | ☐ 待测 | L46 |
| 16 | - | listWorkflowExecutions | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | `workflowId` | - | - | JWT | ☐ 待测 | L49 |
| 17 | - | listWorkflowTemplates | GET | `/api/geo/workflow/templates` | - | - | - | JWT | ☐ 待测 | L52 |
| 18 | - | saveWorkflowTemplate | POST | `/api/geo/workflow/templates` | `data` | data | - | JWT | ☐ 待测 | L55 |

### geoPlatform.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listPlatforms | GET | `/api/geo/platform/platforms` | - | - | - | JWT | ☐ 待测 | L4 |
| 2 | - | listPlatformAccounts | GET | `/api/geo/platform/accounts` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | savePlatformAccount | POST | `/api/geo/platform/accounts` | `data` | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | deletePlatformAccount | DELETE | `/api/geo/platform/accounts/{id}` | `id` | - | - | JWT | ☐ 待测 | L13 |
| 5 | - | publishToPlatform | POST | `/api/geo/platform/publish` | `articleId, platform, opts` | {article_id,platform,} | - | JWT | ☐ 待测 | L16 |
| 6 | - | listPlatformPublishRecords | GET | `/api/geo/platform/records` | `articleId` | {article_id} | - | JWT | ☐ 待测 | L19 |

### geoProbe.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listProbeEngines | GET | `/api/geo/probe/engines` | - | - | - | JWT | ☐ 待测 | L4 |
| 2 | - | testEngineProbe | POST | `/api/geo/probe/test` | `engine, query` | {engine,query} | - | JWT | ☐ 待测 | L7 |
| 3 | - | listProbeRuns | GET | `/api/geo/probe/runs` | `engine, limit` | {engine,limit} | - | JWT | ☐ 待测 | L10 |
| 4 | - | runNegativeMonitor | POST | `/api/geo/probe/run-negative` | - | - | - | JWT | ☐ 待测 | L13 |
| 5 | - | runSourceCatalogSync | POST | `/api/geo/probe/run-source-sync` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | probeAllEngines | POST | `/api/geo/probe/all` | `engines, query` | {engines,query} | - | JWT | ☐ 待测 | L19 |
| 7 | - | getSOV | GET | `/api/geo/sov` | - | - | - | JWT | ☐ 待测 | L22 |
| 8 | - | getCrawlerStats | GET | `/api/geo/crawler-stats` | - | - | - | JWT | ☐ 待测 | L25 |
| 9 | - | runCrawler | POST | `/api/geo/crawler/run` | - | - | - | JWT | ☐ 待测 | L28 |
| 10 | - | detectInaccurateClaims | POST | `/api/geo/inaccurate-claims` | `brandName` | {brand_name} | - | JWT | ☐ 待测 | L31 |
| 11 | - | lookupSourceLevel | GET | `/api/geo/source-catalog/levels` | `url` | {url} | - | JWT | ☐ 待测 | L34 |
| 12 | - | listCompetitors | GET | `/api/geo/competitors` | - | - | - | JWT | ☐ 待测 | L37 |
| 13 | - | getCompetitor | GET | `/api/geo/competitors/{id}` | `id` | - | - | JWT | ☐ 待测 | L40 |
| 14 | - | createCompetitor | POST | `/api/geo/competitors` | `data` | data | - | JWT | ☐ 待测 | L43 |
| 15 | - | updateCompetitor | PUT | `/api/geo/competitors/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L46 |
| 16 | - | deleteCompetitor | DELETE | `/api/geo/competitors/{id}` | `id` | - | - | JWT | ☐ 待测 | L49 |
| 17 | - | getEngineCompare | GET | `/api/geo/visibility/engine-compare` | `days, intent` | {days,intent} | - | JWT | ☐ 待测 | L52 |
| 18 | - | getSOVTrend | GET | `/api/geo/sov/trend` | `days, intent` | {days,intent} | - | JWT | ☐ 待测 | L55 |

### glossary.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/glossaries` | - | data | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/glossaries` | - | - | params | JWT | ☐ 待测 | L8 |
| 3 | - | - | GET | `/api/glossaries/{termId}` | - | - | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/glossaries/{termId}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/glossaries/{termId}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/glossaries/validate` | - | data | - | JWT | ☐ 待测 | L24 |

| 1 | humanTaskApi | list | GET | `/api/human-tasks` | `params` | - | params | JWT | ☐ 待测 | L43 |
| 2 | humanTaskApi | counts | GET | `{base}/counts` | - | undefined | - | JWT | ☐ 待测 | L49 |
| 3 | humanTaskApi | get | GET | `{base}/{expr}` | `id` | - | - | JWT | ☐ 待测 | L53 |
| 4 | humanTaskApi | claim | POST | `{base}/{expr}/claim` | `id` | - | - | JWT | ☐ 待测 | L58 |
| 5 | humanTaskApi | release | POST | `{base}/{expr}/release` | `id` | - | - | JWT | ☐ 待测 | L63 |
| 6 | humanTaskApi | complete | POST | `{base}/{expr}/complete` | `id` | - | - | JWT | ☐ 待测 | L68 |
| 7 | humanTaskApi | cancel | POST | `{base}/{expr}/cancel` | `id, reason` | {reason} | - | JWT | ☐ 待测 | L73 |
| 3 | humanTaskApi | claim | POST | `{base}/{expr}/claim` | `id` | - | - | JWT | ☐ 待测 | L58 |
| 4 | humanTaskApi | release | POST | `{base}/{expr}/release` | `id` | - | - | JWT | ☐ 待测 | L63 |
| 5 | humanTaskApi | complete | POST | `{base}/{expr}/complete` | `id` | - | - | JWT | ☐ 待测 | L68 |
| 6 | humanTaskApi | cancel | POST | `{base}/{expr}/cancel` | `id, reason` | {reason} | - | JWT | ☐ 待测 | L73 |

### i18nStats.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/i18n/stats` | - | - | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/i18n/stats/lang-dist` | - | - | params | JWT | ☐ 待测 | L8 |
| 3 | - | - | GET | `/api/i18n/stats/cache` | - | - | params | JWT | ☐ 待测 | L12 |
| 4 | - | - | GET | `/api/i18n/stats/glossary` | - | - | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | GET | `/api/i18n/stats/quality` | - | - | params | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/i18n/stats/latency` | - | - | params | JWT | ☐ 待测 | L24 |

### inbox.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | inboxApi | listConversations | GET | `/api/inbox/conversations` | `params` | - | params | JWT | ☐ 待测 | L7 |
| 2 | inboxApi | getConversation | GET | `/api/inbox/conversations/{id}` | `id` | - | - | JWT | ☐ 待测 | L12 |
| 3 | inboxApi | getMessages | GET | `/api/inbox/conversations/{id}/messages` | `id, params` | - | params | JWT | ☐ 待测 | L17 |
| 4 | inboxApi | markRead | POST | `/api/inbox/conversations/{id}/read` | `id` | - | - | JWT | ☐ 待测 | L22 |
| 5 | inboxApi | setPin | POST | `/api/inbox/conversations/{id}/pin` | `id, pinned` | {pinned} | - | JWT | ☐ 待测 | L27 |
| 6 | inboxApi | setStar | POST | `/api/inbox/conversations/{id}/star` | `id, starred` | {starred} | - | JWT | ☐ 待测 | L31 |
| 7 | inboxApi | setMute | POST | `/api/inbox/conversations/{id}/mute` | `id, muted` | {muted} | - | JWT | ☐ 待测 | L35 |
| 8 | inboxApi | addTag | POST | `/api/inbox/conversations/{id}/tags` | `id, tag` | {tag} | - | JWT | ☐ 待测 | L40 |
| 9 | inboxApi | removeTag | DELETE | `/api/inbox/conversations/{id}/tags/{expr}` | `id, tag` | - | - | JWT | ☐ 待测 | L44 |
| 10 | inboxApi | deleteMessage | DELETE | `/api/inbox/conversations/{id}/messages/{mid}?source={source}` | `id, mid, source` | - | - | JWT | ☐ 待测 | L49 |
| 11 | inboxApi | assign | POST | `/api/inbox/assign` | `data` | data | - | JWT | ☐ 待测 | L54 |
| 12 | inboxApi | autoAssign | POST | `/api/inbox/assign/auto` | `data` | data | - | JWT | ☐ 待测 | L59 |
| 13 | inboxApi | listAssignments | GET | `/api/inbox/assignments` | `params` | - | params | JWT | ☐ 待测 | L64 |
| 14 | inboxApi | getStats | GET | `/api/inbox/stats` | - | - | - | JWT | ☐ 待测 | L69 |
| 15 | inboxApi | reconcile | POST | `/api/inbox/reconcile?mode={mode}` | `mode` | - | - | JWT | ☐ 待测 | L74 |
| 16 | inboxApi | staffLoad | GET | `/api/inbox/staff/{expr}/load` | `staff` | - | - | JWT | ☐ 待测 | L79 |
| 17 | inboxApi | listAgents | GET | `/api/inbox/agents` | - | - | - | JWT | ☐ 待测 | L84 |

### integration.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/integrations` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/integrations/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | POST | `/api/integrations` | - | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | - | PUT | `/api/integrations/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 5 | - | - | DELETE | `/api/integrations/{id}` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | - | POST | `/api/integrations/{id}/sync-customers` | - | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | POST | `/api/integrations/{id}/sync-products` | - | - | - | JWT | ☐ 待测 | L22 |
| 8 | - | - | GET | `/api/integration/sync-logs` | - | - | params | JWT | ☐ 待测 | L25 |
| 9 | - | - | GET | `/api/integration/external-customers` | - | - | - | JWT | ☐ 待测 | L28 |
| 10 | - | - | GET | `/api/integration/external-orders` | - | - | - | JWT | ☐ 待测 | L31 |
| 11 | - | - | GET | `/api/integration/external-orders-by-customer` | - | - | params | JWT | ☐ 待测 | L34 |
| 12 | - | - | GET | `/api/integration/external-products` | - | - | - | JWT | ☐ 待测 | L37 |
| 13 | - | - | POST | `/api/integrations/{id}/test` | - | - | - | JWT | ☐ 待测 | L56 |

### intentRecognition.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | intentApi | recognize | POST | `/api/intent/recognize` | `data` | data | - | JWT | ☐ 待测 | L5 |
| 2 | intentApi | batchRecognize | POST | `/api/intent/recognize/batch` | `data` | data | - | JWT | ☐ 待测 | L9 |
| 3 | intentApi | getStats | GET | `/api/intent/stats` | `params` | - | params | JWT | ☐ 待测 | L13 |
| 4 | intentApi | getRecent | GET | `/api/intent/recent` | `params` | - | params | JWT | ☐ 待测 | L17 |
| 5 | intentApi | getDict | GET | `/api/intent/dict` | - | - | - | JWT | ☐ 待测 | L21 |
| 6 | intentApi | recognizeFine | POST | `/api/intent/recognize/fine` | `data` | data | - | JWT | ☐ 待测 | L25 |
| 7 | intentApi | getLogs | GET | `/api/intent/logs` | `params` | - | params | JWT | ☐ 待测 | L29 |
| 8 | intentApi | getStatsFine | GET | `/api/intent/stats/fine` | `params` | - | params | JWT | ☐ 待测 | L33 |
| 9 | intentApi | getConfig | GET | `/api/intent/config` | - | - | - | JWT | ☐ 待测 | L37 |
| 10 | intentApi | updateConfig | PUT | `/api/intent/config` | `data` | data | - | JWT | ☐ 待测 | L41 |
| 11 | intentApi | getKeywordOverride | GET | `/api/intent-records/keywords-override` | - | - | - | JWT | ☐ 待测 | L45 |
| 12 | intentApi | updateKeywordOverride | PUT | `/api/intent-records/keywords-override` | `data` | data | - | JWT | ☐ 待测 | L49 |

### kbConnector.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | kbConnectorApi | list | GET | `/api/kb-connectors` | - | - | - | JWT | ☐ 待测 | L5 |
| 2 | kbConnectorApi | create | POST | `/api/kb-connectors` | `data` | data | - | JWT | ☐ 待测 | L6 |
| 3 | kbConnectorApi | update | PUT | `/api/kb-connectors/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L7 |
| 4 | kbConnectorApi | remove | DELETE | `/api/kb-connectors/{id}` | `id` | - | - | JWT | ☐ 待测 | L8 |
| 5 | kbConnectorApi | sync | POST | `/api/kb-connectors/{id}/sync` | `id` | - | - | JWT | ☐ 待测 | L9 |
| 6 | capabilitiesApi | get | GET | `/api/capabilities` | - | - | - | JWT | ☐ 待测 | L14 |

### kbCrawler.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | kbCrawlerApi | listSources | GET | `/api/kb/crawler/sources` | `params` | - | params | JWT | ☐ 待测 | L7 |
| 2 | kbCrawlerApi | createSource | POST | `/api/kb/crawler/sources` | `data` | data | - | JWT | ☐ 待测 | L11 |
| 3 | kbCrawlerApi | updateSource | PUT | `/api/kb/crawler/sources/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L14 |
| 4 | kbCrawlerApi | pauseSource | POST | `/api/kb/crawler/sources/{id}/pause` | `id` | - | - | JWT | ☐ 待测 | L17 |
| 5 | kbCrawlerApi | resumeSource | POST | `/api/kb/crawler/sources/{id}/resume` | `id` | - | - | JWT | ☐ 待测 | L20 |
| 6 | kbCrawlerApi | verifySite | POST | `/api/kb/crawler/sources/{id}/verify` | `id` | - | - | JWT | ☐ 待测 | L23 |
| 7 | kbCrawlerApi | deleteSource | DELETE | `/api/kb/crawler/sources/{id}` | `id` | - | - | JWT | ☐ 待测 | L26 |
| 8 | kbCrawlerApi | syncSource | POST | `/api/kb/crawler/sources/{id}/sync` | `id` | - | - | JWT | ☐ 待测 | L30 |
| 9 | kbCrawlerApi | listBatches | GET | `/api/kb/crawler/batches` | `params` | - | params | JWT | ☐ 待测 | L34 |
| 10 | kbCrawlerApi | retryBatch | POST | `/api/kb/crawler/batches/{id}/retry` | `id` | - | - | JWT | ☐ 待测 | L37 |
| 11 | kbQaCandidateApi | list | GET | `/api/kb/qa/candidates` | `params` | - | params | JWT | ☐ 待测 | L44 |
| 12 | kbQaCandidateApi | approve | POST | `/api/kb/qa/candidates/{id}/approve` | `id` | - | - | JWT | ☐ 待测 | L47 |
| 13 | kbQaCandidateApi | reject | POST | `/api/kb/qa/candidates/{id}/reject` | `id` | - | - | JWT | ☐ 待测 | L50 |
| 14 | kbQaCandidateApi | publish | POST | `/api/kb/qa/candidates/{id}/publish` | `id` | - | - | JWT | ☐ 待测 | L53 |

### knowledge.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | knowledgeAPI | importUpload | POST | `/api/knowledge/import/upload` | `formData` | formData | - | JWT | ☐ 待测 | L5 |
| 2 | knowledgeAPI | importText | POST | `/api/knowledge/import/text` | `data` | data | - | JWT | ☐ 待测 | L10 |
| 3 | knowledgeAPI | importURL | POST | `/api/knowledge/import/url` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | knowledgeAPI | listDocuments | GET | `/api/knowledge/documents` | `params` | - | params | JWT | ☐ 待测 | L17 |
| 5 | knowledgeAPI | getDocument | GET | `/api/knowledge/documents/{id}` | `id, params` | - | params | JWT | ☐ 待测 | L20 |
| 6 | knowledgeAPI | getDocumentProgress | GET | `/api/knowledge/documents/{id}/progress` | `id` | - | - | JWT | ☐ 待测 | L23 |
| 7 | knowledgeAPI | getDocumentChunks | GET | `/api/knowledge/documents/{id}/chunks` | `id` | - | - | JWT | ☐ 待测 | L26 |
| 8 | knowledgeAPI | updateDocument | PUT | `/api/knowledge/documents/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L29 |
| 9 | knowledgeAPI | deleteDocument | DELETE | `/api/knowledge/documents/{id}` | `id, params` | - | params | JWT | ☐ 待测 | L32 |
| 10 | knowledgeAPI | reindexDocument | POST | `/api/knowledge/documents/{id}/reindex` | `id, params` | Literal | params | JWT | ☐ 待测 | L35 |
| 11 | knowledgeAPI | rebuildProductIndex | POST | `/api/knowledge/products/{productId}/rebuild-index` | `productId` | - | - | JWT | ☐ 待测 | L39 |
| 12 | knowledgeAPI | getProductOverview | GET | `/api/knowledge/products/{productId}/overview` | `productId` | - | - | JWT | ☐ 待测 | L42 |
| 13 | knowledgeAPI | search | POST | `/api/knowledge/search` | `data` | data | - | JWT | ☐ 待测 | L46 |
| 14 | knowledgeAPI | listImportLogs | GET | `/api/knowledge/import-logs` | `params` | - | params | JWT | ☐ 待测 | L50 |
| 15 | knowledgeAPI | listOpenAPISources | GET | `/api/knowledge/openapi/sources` | `params` | - | params | JWT | ☐ 待测 | L54 |
| 16 | knowledgeAPI | createOpenAPISource | POST | `/api/knowledge/openapi/sources` | `data` | data | - | JWT | ☐ 待测 | L57 |
| 17 | knowledgeAPI | getOpenAPISource | GET | `/api/knowledge/openapi/sources/{id}` | `id, params` | - | params | JWT | ☐ 待测 | L60 |
| 18 | knowledgeAPI | updateOpenAPISource | PUT | `/api/knowledge/openapi/sources/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L63 |
| 19 | knowledgeAPI | deleteOpenAPISource | DELETE | `/api/knowledge/openapi/sources/{id}` | `id, params` | - | params | JWT | ☐ 待测 | L66 |
| 20 | knowledgeAPI | syncOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/sync` | `id, params` | Literal | params | JWT | ☐ 待测 | L69 |
| 21 | knowledgeAPI | testOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/test` | `id, data` | data | - | JWT | ☐ 待测 | L72 |
| 22 | knowledgeAPI | toggleOpenAPISource | POST | `/api/knowledge/openapi/sources/{id}/toggle` | `id, data, params` | data | params | JWT | ☐ 待测 | L75 |
| 23 | knowledgeAPI | getOverviewStats | GET | `/api/knowledge/stats/overview` | `params` | - | params | JWT | ☐ 待测 | L79 |
| 24 | knowledgeAPI | getDocumentStats | GET | `/api/knowledge/stats/documents` | `params` | - | params | JWT | ☐ 待测 | L82 |
| 25 | knowledgeAPI | getSearchStats | GET | `/api/knowledge/stats/searches` | `params` | - | params | JWT | ☐ 待测 | L85 |
| 26 | knowledgeAPI | getImportStats | GET | `/api/knowledge/stats/imports` | `params` | - | params | JWT | ☐ 待测 | L88 |
| 27 | knowledgeAPI | getOpenAPIStats | GET | `/api/knowledge/stats/openapi` | `params` | - | params | JWT | ☐ 待测 | L91 |

### knowledgeBase.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/knowledge-bases` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/knowledge-bases/by-type/{kbType}` | - | - | params | JWT | ☐ 待测 | L8 |
| 3 | - | - | GET | `/api/knowledge-bases/{id}` | - | - | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | POST | `/api/knowledge-bases` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | PUT | `/api/knowledge-bases/{id}` | - | data | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | DELETE | `/api/knowledge-bases/{id}` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/knowledge-bases/{id}/stats` | - | - | - | JWT | ☐ 待测 | L28 |

### knowledgeMerchant.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | knowledgeMerchantAPI | batchImport | POST | `/api/knowledge-merchant/batch/import` | `data` | data | - | JWT | ☐ 待测 | L5 |
| 2 | knowledgeMerchantAPI | batchUpload | POST | `/api/knowledge-merchant/batch/upload` | `formData` | formData | - | JWT | ☐ 待测 | L8 |
| 3 | knowledgeMerchantAPI | playground | POST | `/api/knowledge-merchant/playground` | `data` | data | - | JWT | ☐ 待测 | L14 |
| 4 | knowledgeMerchantAPI | listDocumentChunks | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | `documentId, params` | - | params | JWT | ☐ 待测 | L18 |
| 5 | knowledgeMerchantAPI | updateChunk | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | `chunkId, data` | data | - | JWT | ☐ 待测 | L21 |
| 6 | knowledgeMerchantAPI | deleteChunk | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | `chunkId` | - | - | JWT | ☐ 待测 | L24 |
| 7 | knowledgeMerchantAPI | splitChunk | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | `chunkId, data` | data | - | JWT | ☐ 待测 | L27 |
| 8 | knowledgeMerchantAPI | submitFeedback | POST | `/api/knowledge-merchant/feedback` | `data` | data | - | JWT | ☐ 待测 | L31 |
| 9 | knowledgeMerchantAPI | listFeedbacks | GET | `/api/knowledge-merchant/feedbacks` | `params` | - | params | JWT | ☐ 待测 | L34 |
| 10 | knowledgeMerchantAPI | createToken | POST | `/api/knowledge-merchant/tokens` | `data` | data | - | JWT | ☐ 待测 | L38 |
| 11 | knowledgeMerchantAPI | listTokens | GET | `/api/knowledge-merchant/tokens` | `params` | - | params | JWT | ☐ 待测 | L41 |
| 12 | knowledgeMerchantAPI | revokeToken | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | `tokenId` | - | - | JWT | ☐ 待测 | L44 |
| 13 | knowledgeMerchantAPI | externalImport | POST | `/api/knowledge-merchant/external/import` | `data, token` | data | - | JWT | ☐ 待测 | L48 |
| 14 | knowledgeMerchantAPI | listExternalJobs | GET | `/api/knowledge-merchant/external/jobs` | `params` | - | params | JWT | ☐ 待测 | L53 |

### kuaishouCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/kuaishou/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/kuaishou/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/kuaishou/create` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/kuaishou/update` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/kuaishou/delete/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/kuaishou/view/{id}` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/kuaishou/like/{id}` | - | - | - | JWT | ☐ 待测 | L28 |
| 8 | - | - | POST | `/api/kuaishou/share/{id}` | - | - | - | JWT | ☐ 待测 | L32 |
| 9 | - | - | GET | `/api/kuaishou/stats/card/{id}` | - | - | params | JWT | ☐ 待测 | L36 |
| 10 | - | - | GET | `/api/kuaishou/stats/overall` | - | - | params | JWT | ☐ 待测 | L40 |
| 11 | - | - | POST | `/api/kuaishou/{id}/generate-short-link` | - | - | - | JWT | ☐ 待测 | L44 |

### leadMining.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/lead-mining/config` | - | - | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | POST | `/api/lead-mining/config` | - | data | - | JWT | ☐ 待测 | L8 |

### livecode.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/live-codes/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/live-codes/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/live-codes/create` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/live-codes/{id}/update` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/live-codes/{id}/delete` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/live-codes/{id}/stats` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/live-codes/{liveCodeId}/qrcodes` | - | - | - | JWT | ☐ 待测 | L28 |
| 8 | - | - | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | - | data | - | JWT | ☐ 待测 | L32 |
| 9 | - | - | GET | `/api/live-codes/qrcodes/{qrId}/stats` | - | - | - | JWT | ☐ 待测 | L36 |
| 10 | - | - | POST | `/api/live-codes/{id}/share` | - | data | - | JWT | ☐ 待测 | L40 |
| 11 | - | - | DELETE | `/api/live-codes/qrcodes/{id}/delete` | - | - | - | JWT | ☐ 待测 | L44 |
| 12 | - | - | PUT | `/api/live-codes/qrcodes/{id}/update` | - | data | - | JWT | ☐ 待测 | L48 |

### llmReport.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LlmReportApi | getOverview | GET | `/api/reports/llm/overview` | `params` | - | params | JWT | ☐ 待测 | L7 |
| 2 | LlmReportApi | getTimeseries | GET | `/api/reports/llm/timeseries` | `params` | - | params | JWT | ☐ 待测 | L11 |
| 3 | LlmReportApi | getBreakdown | GET | `/api/reports/llm/breakdown` | `params` | - | params | JWT | ☐ 待测 | L15 |
| 4 | LlmReportApi | getErrors | GET | `/api/reports/llm/errors` | `params` | - | params | JWT | ☐ 待测 | L19 |

### llmRouting.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LlmRoutingApi | getDailyUsage | GET | `/api/llm/usage/daily` | `window, groupBy` | {window,group_by} | - | JWT | ☐ 待测 | L7 |
| 2 | LlmRoutingApi | getModelList | GET | `/api/llm/models` | `params` | - | params | JWT | ☐ 待测 | L11 |
| 3 | LlmRoutingApi | getModelDetail | GET | `/api/llm/models/{expr}` | `name` | - | - | JWT | ☐ 待测 | L15 |
| 4 | LlmRoutingApi | createModel | POST | `/api/llm/models` | `data` | data | - | JWT | ☐ 待测 | L19 |
| 5 | LlmRoutingApi | updateModel | PUT | `/api/llm/models/{expr}` | `name, data` | data | - | JWT | ☐ 待测 | L23 |
| 6 | LlmRoutingApi | deleteModel | DELETE | `/api/llm/models/{expr}` | `name` | - | - | JWT | ☐ 待测 | L27 |
| 7 | LlmRoutingApi | updateModelStatus | PUT | `/api/llm/models/{expr}` | `name, status` | {enabled} | - | JWT | ☐ 待测 | L31 |
| 8 | LlmRoutingApi | testModel | POST | `/api/llm/models/{expr}/test` | `name, body` | body | - | JWT | ☐ 待测 | L35 |
| 9 | LlmRoutingApi | getSceneRouting | GET | `/api/llm/scene-routing` | - | - | - | JWT | ☐ 待测 | L39 |
| 10 | LlmRoutingApi | getScenarios | GET | `/api/llm/scenarios` | - | - | - | JWT | ☐ 待测 | L43 |
| 11 | LlmRoutingApi | saveSceneRouting | PUT | `/api/llm/scene-routing` | `data` | payload | - | JWT | ☐ 待测 | L48 |
| 12 | LlmRoutingApi | getFallbackStrategy | GET | `/api/llm/fallback` | - | - | - | JWT | ☐ 待测 | L52 |
| 13 | LlmRoutingApi | getAuditHistory | GET | `/api/llm/audit` | `scenario, limit` | - | params | JWT | ☐ 待测 | L58 |
| 14 | LlmRoutingApi | getStats | GET | `/api/llm/stats` | - | - | - | JWT | ☐ 待测 | L62 |
| 15 | LlmRoutingApi | getUsage | GET | `/api/llm/usage` | `window` | {window} | - | JWT | ☐ 待测 | L66 |
| 16 | LlmRoutingApi | getCostStats | GET | `/api/llm/cost-stats` | `window` | {window} | - | JWT | ☐ 待测 | L70 |
| 17 | LlmRoutingApi | getHealth | GET | `/api/llm/health` | - | - | - | JWT | ☐ 待测 | L74 |
| 18 | LlmRoutingApi | getModelTypeStats | GET | `/api/llm/model-type-stats` | `window` | {window} | - | JWT | ☐ 待测 | L78 |
| 19 | LlmRoutingApi | getEgressAudit | GET | `/api/llm/egress-audit` | `params` | - | params | JWT | ☐ 待测 | L82 |
| 20 | LlmRoutingApi | getEgressAlerts | GET | `/api/llm/egress-alerts` | `params` | - | params | JWT | ☐ 待测 | L86 |

### ltcConfig.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/manage/ltc/config` | - | - | - | JWT | ☐ 待测 | L11 |
| 2 | - | - | PUT | `/api/manage/ltc/config` | - | cfg | - | JWT | ☐ 待测 | L21 |

### ltcRates.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | LtcRatesApi | getRates | GET | `/api/ltc-rates` | - | - | - | JWT | ☐ 待测 | L5 |

### marketingFlow.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/marketing-flows` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/marketing-flows/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | POST | `/api/marketing-flows` | - | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | - | PUT | `/api/marketing-flows/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 5 | - | - | DELETE | `/api/marketing-flows/{id}` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | - | POST | `/api/marketing-flows/{id}/activate` | - | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | POST | `/api/marketing-flows/{id}/pause` | - | - | - | JWT | ☐ 待测 | L22 |
| 8 | - | - | POST | `/api/marketing-flows/{id}/stop` | - | - | - | JWT | ☐ 待测 | L25 |
| 9 | - | - | GET | `/api/marketing-flows/{id}/executions` | - | - | - | JWT | ☐ 待测 | L28 |
| 10 | - | - | GET | `/api/marketing-flows/{id}/stats` | - | - | - | JWT | ☐ 待测 | L31 |

### material.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/material/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | DELETE | `/api/material/{id}` | - | - | - | JWT | ☐ 待测 | L12 |
| 3 | - | - | GET | `/api/material/categories` | - | - | - | JWT | ☐ 待测 | L16 |
| 4 | - | - | POST | `/api/material/categories` | - | data | - | JWT | ☐ 待测 | L20 |
| 5 | - | - | PUT | `/api/material/categories/{id}` | - | data | - | JWT | ☐ 待测 | L24 |
| 6 | - | - | DELETE | `/api/material/categories/{id}` | - | - | - | JWT | ☐ 待测 | L28 |
| 7 | - | - | GET | `/api/material/selector` | - | - | params | JWT | ☐ 待测 | L32 |
| 8 | - | - | POST | `/api/material/{id}/usage` | - | - | - | JWT | ☐ 待测 | L36 |
| 9 | - | - | GET | `/api/material/stats` | - | - | - | JWT | ☐ 待测 | L40 |

### mcpCredential.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | mcpCredentialApi | list | GET | `/api/mcp/credentials` | - | - | - | JWT | ☐ 待测 | L5 |
| 2 | mcpCredentialApi | create | POST | `/api/mcp/credentials` | `data` | data | - | JWT | ☐ 待测 | L6 |
| 3 | mcpCredentialApi | update | PUT | `/api/mcp/credentials/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L7 |
| 4 | mcpCredentialApi | remove | DELETE | `/api/mcp/credentials/{id}` | `id` | - | - | JWT | ☐ 待测 | L8 |

### messageHub.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | messageHubApi | pushMessage | POST | `/api/message-hub/push` | `data` | data | - | JWT | ☐ 待测 | L5 |
| 2 | messageHubApi | pushBatch | POST | `/api/message-hub/push-batch` | `data` | data | - | JWT | ☐ 待测 | L9 |
| 3 | messageHubApi | pushFromChannel | POST | `/api/message-hub/push-from-channel` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | messageHubApi | getMessages | GET | `/api/message-hub/list` | `params` | - | params | JWT | ☐ 待测 | L17 |
| 5 | messageHubApi | getMessageById | GET | `/api/message-hub/{id}` | `id` | - | - | JWT | ☐ 待测 | L21 |
| 6 | messageHubApi | markRead | POST | `/api/message-hub/{expr}/read` | `ids` | {ids} | - | JWT | ☐ 待测 | L25 |
| 7 | messageHubApi | recallMessage | POST | `/api/message-hub/{id}/recall` | `id, config` | undefined | - | JWT | ☐ 待测 | L39 |
| 8 | messageHubApi | getStats | GET | `/api/message-hub/stats` | `params` | - | params | JWT | ☐ 待测 | L43 |
| 9 | messageHubApi | getPlatforms | GET | `/api/message-hub/platforms` | - | - | - | JWT | ☐ 待测 | L47 |

### monitor.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | MonitorApi | traceTree | GET | `/api/monitor/trace-tree` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | MonitorApi | traces | GET | `/api/monitor/traces` | `params` | - | params | JWT | ☐ 待测 | L8 |
| 3 | MonitorApi | lifecycle | GET | `/api/monitor/lifecycle` | `params` | - | params | JWT | ☐ 待测 | L11 |
| 4 | MonitorApi | health | GET | `/api/monitor/health` | - | - | - | JWT | ☐ 待测 | L14 |
| 5 | MonitorApi | nodeHealth | GET | `/api/monitor/node-health` | - | - | - | JWT | ☐ 待测 | L17 |
| 6 | MonitorApi | latency | GET | `/api/monitor/latency` | - | - | - | JWT | ☐ 待测 | L20 |
| 7 | MonitorApi | anomalies | GET | `/api/monitor/anomalies` | - | - | - | JWT | ☐ 待测 | L23 |
| 8 | MonitorApi | evalLogs | GET | `/api/monitor/trace-eval/logs` | `params` | - | params | JWT | ☐ 待测 | L26 |
| 9 | MonitorApi | knowledgeWeights | GET | `/api/monitor/knowledge-weights` | `params` | - | params | JWT | ☐ 待测 | L29 |
| 10 | MonitorApi | triggerEval | POST | `/api/monitor/trace-eval/trigger` | `params` | Literal | params | JWT | ☐ 待测 | L32 |
| 11 | MonitorApi | ragRecallSnapshot | GET | `/api/rag/recall/snapshot` | - | - | - | JWT | ☐ 待测 | L36 |
| 12 | MonitorApi | ragRecallSnapshots | GET | `/api/rag/recall/snapshots` | `params` | - | params | JWT | ☐ 待测 | L39 |
| 13 | MonitorApi | ragRecallCollect | POST | `/api/rag/recall/collect` | `params` | - | params | JWT | ☐ 待测 | L42 |
| 14 | MonitorApi | ragRecallStart | POST | `/api/rag/recall/start` | - | - | - | JWT | ☐ 待测 | L45 |
| 15 | MonitorApi | ragRecallStop | POST | `/api/rag/recall/stop` | - | - | - | JWT | ☐ 待测 | L48 |
| 16 | MonitorApi | ragRecallMetrics | GET | `/api/rag/recall/metrics` | `params` | - | params | JWT | ☐ 待测 | L51 |
| 17 | MonitorApi | ragLowRecallQueries | GET | `/api/rag/recall/low-recall` | `params` | - | params | JWT | ☐ 待测 | L54 |

### notification.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | notificationApi | list | GET | `/api/auth/notifications` | `params` | - | params | JWT | ☐ 待测 | L9 |
| 2 | notificationApi | markRead | POST | `/api/auth/notifications/{id}/read` | `id` | - | - | JWT | ☐ 待测 | L11 |
| 3 | notificationApi | markAllRead | POST | `/api/auth/notifications/read-all` | - | - | - | JWT | ☐ 待测 | L13 |
| 4 | notificationApi | unreadCount | GET | `/api/auth/notifications/unread-count` | - | - | - | JWT | ☐ 待测 | L15 |

### objection.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/objection/handle` | - | data | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | POST | `/api/objection/classify` | - | data | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | GET | `/api/objection/categories` | - | - | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | POST | `/api/objection/usage` | - | data | - | JWT | ☐ 待测 | L16 |

### obs.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/obs/config` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/obs/config/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/obs/config` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/obs/config/{id}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/obs/config/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/obs/config/{id}/test` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/obs/config/{id}/default` | - | - | - | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/obs/config/default` | - | - | - | JWT | ☐ 待测 | L32 |

### observabilityDashboard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/observability/dashboard` | - | {window} | - | JWT | ☐ 待测 | L9 |

### oneid.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/customer/oneid/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/customer/oneid/conflicts` | - | - | params | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/customer/oneid/merge` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | POST | `/api/customer/oneid/conflicts/{id}/resolve` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | GET | `/api/customer-oneid/{customerId}/identities` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/customer-oneid/{customerId}/identities` | - | data | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/customer/oneid/resolve` | - | data | - | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/customer/oneid/stats` | - | - | - | JWT | ☐ 待测 | L32 |
| 9 | - | - | GET | `/api/oneid/merge-rules` | - | - | - | JWT | ☐ 待测 | L36 |
| 10 | - | - | POST | `/api/oneid/merge-rules` | - | data | - | JWT | ☐ 待测 | L40 |

### operationLog.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/operation-logs` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/operation-logs/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | GET | `/api/operation-logs/statistics` | - | - | params | JWT | ☐ 待测 | L10 |
| 4 | - | - | GET | `/api/operation-logs/export` | - | - | params | JWT | ☐ 待测 | L13 |
| 5 | - | - | POST | `/api/operation-logs/clean` | - | {beforeDate} | - | JWT | ☐ 待测 | L16 |
| 1 | orderDraftApi | list | GET | `/api/manage/order-drafts` | `params` | - | params | JWT | ☐ 待测 | L32 |
| 2 | orderDraftApi | get | GET | `{base}/{expr}` | `id` | undefined | - | JWT | ☐ 待测 | L36 |
| 3 | orderDraftApi | edit | PATCH | `{base}/{expr}` | `id, updates` | updates | - | JWT | ☐ 待测 | L43 |
| 4 | orderDraftApi | confirm | POST | `{base}/{expr}/confirm` | `id` | - | - | JWT | ☐ 待测 | L50 |
| 5 | orderDraftApi | cancel | POST | `{base}/{expr}/cancel` | `id, reason` | {reason} | - | JWT | ☐ 待测 | L55 |
| 6 | orderDraftApi | stats | GET | `/api/agent/order-drafts/stats` | - | undefined | - | JWT | ☐ 待测 | L62 |
| 1 | orderDraftApi | get | GET | `{base}/{expr}` | `id` | undefined | - | JWT | ☐ 待测 | L36 |
| 2 | orderDraftApi | edit | PATCH | `{base}/{expr}` | `id, updates` | updates | - | JWT | ☐ 待测 | L43 |
| 3 | orderDraftApi | confirm | POST | `{base}/{expr}/confirm` | `id` | - | - | JWT | ☐ 待测 | L50 |
| 4 | orderDraftApi | cancel | POST | `{base}/{expr}/cancel` | `id, reason` | {reason} | - | JWT | ☐ 待测 | L55 |
| 5 | orderDraftApi | stats | GET | `/api/agent/order-drafts/stats` | - | undefined | - | JWT | ☐ 待测 | L62 |

### persona.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/analytics/persona/staffs` | - | - | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/analytics/persona/staffs/{staffId}` | - | - | - | JWT | ☐ 待测 | L8 |

### qqBot.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/qq/accounts` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/qq/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/qq/accounts` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/qq/accounts/{id}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/qq/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/qq/accounts/{id}/test-send` | - | data | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/qq/accounts/{id}/verify-callback` | - | {} | - | JWT | ☐ 待测 | L28 |

### ragProductConfig.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | ragProductConfigAPI | createRagProduct | POST | `/api/rag-config/products` | `data` | data | - | JWT | ☐ 待测 | L12 |
| 2 | ragProductConfigAPI | getRagProducts | GET | `/api/rag-config/products` | `params` | - | params | JWT | ☐ 待测 | L21 |
| 3 | ragProductConfigAPI | listProducts | GET | `/api/rag-config/products` | `params` | - | params | JWT | ☐ 待测 | L25 |
| 4 | ragProductConfigAPI | updateRagProduct | PUT | `/api/rag-config/products/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L29 |
| 5 | ragProductConfigAPI | deleteRagProduct | DELETE | `/api/rag-config/products/{id}` | `id` | - | - | JWT | ☐ 待测 | L33 |
| 6 | ragProductConfigAPI | getAccountConfig | GET | `/api/rag-config/accounts/config` | `params` | - | params | JWT | ☐ 待测 | L37 |
| 7 | ragProductConfigAPI | updateAccountConfig | PUT | `/api/rag-config/accounts/config` | `data` | data | - | JWT | ☐ 待测 | L41 |
| 8 | ragProductConfigAPI | processMessage | POST | `/api/rag-config/process-message` | `data` | data | - | JWT | ☐ 待测 | L45 |

### reachPipeline.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | reachPipelineApi | getPipelines | GET | `/api/reach/pipelines` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | reachPipelineApi | createPipeline | POST | `/api/reach/pipelines` | `data` | data | - | JWT | ☐ 待测 | L8 |
| 3 | reachPipelineApi | getPipeline | GET | `/api/reach/pipelines/{id}` | `id` | - | - | JWT | ☐ 待测 | L11 |
| 4 | reachPipelineApi | updatePipeline | PUT | `/api/reach/pipelines/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L14 |
| 5 | reachPipelineApi | deletePipeline | DELETE | `/api/reach/pipelines/{id}` | `id` | - | - | JWT | ☐ 待测 | L17 |
| 6 | reachPipelineApi | pausePipeline | POST | `/api/reach/pipelines/{id}/pause` | `id` | {} | - | JWT | ☐ 待测 | L20 |
| 7 | reachPipelineApi | resumePipeline | POST | `/api/reach/pipelines/{id}/resume` | `id` | {} | - | JWT | ☐ 待测 | L23 |
| 8 | reachPipelineApi | archivePipeline | POST | `/api/reach/pipelines/{id}/archive` | `id` | {} | - | JWT | ☐ 待测 | L26 |
| 9 | reachPipelineApi | getJobs | GET | `/api/reach/jobs` | `params` | - | params | JWT | ☐ 待测 | L30 |
| 10 | reachPipelineApi | enqueueJob | POST | `/api/reach/jobs` | `data` | data | - | JWT | ☐ 待测 | L33 |
| 11 | reachPipelineApi | getJob | GET | `/api/reach/jobs/{id}` | `id` | - | - | JWT | ☐ 待测 | L36 |
| 12 | reachPipelineApi | cancelJob | POST | `/api/reach/jobs/{id}/cancel` | `id` | {} | - | JWT | ☐ 待测 | L39 |
| 13 | reachPipelineApi | retryJob | POST | `/api/reach/jobs/{id}/retry` | `id` | {} | - | JWT | ☐ 待测 | L42 |
| 14 | reachPipelineApi | executeJob | POST | `/api/reach/jobs/{id}/execute` | `id` | {} | - | JWT | ☐ 待测 | L45 |
| 15 | reachPipelineApi | getStats | GET | `/api/reach/stats` | `params` | - | params | JWT | ☐ 待测 | L49 |
| 16 | reachPipelineApi | resetRateLimit | POST | `/api/reach/rate-limit/reset` | `channel` | {} | inline | JWT | ☐ 待测 | L52 |
| 17 | reachPipelineApi | getDeferredOutbounds | GET | `/api/reach/deferred-outbound` | `params` | - | params | JWT | ☐ 待测 | L58 |
| 18 | reachPipelineApi | cancelDeferredOutbound | POST | `/api/reach/deferred-outbound/{id}/cancel` | `id` | {} | - | JWT | ☐ 待测 | L63 |

### runtimeConfig.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/config/runtime` | - | - | params | JWT | ☐ 待测 | L13 |
| 2 | - | - | GET | `/api/config/dictionaries/{expr}` | - | - | - | JWT | ☐ 待测 | L23 |
| 3 | dictionariesApi | listParents | GET | `/api/manage/dictionaries` | - | - | - | JWT | ☐ 待测 | L28 |
| 4 | dictionariesApi | get | GET | `/api/manage/dictionaries/{expr}` | `parent` | - | - | JWT | ☐ 待测 | L29 |
| 5 | dictionariesApi | upsertItem | PUT | `/api/manage/dictionaries/{expr}/items` | `parent, item` | item | - | JWT | ☐ 待测 | L30 |
| 6 | dictionariesApi | replaceTransitions | PUT | `/api/manage/dictionaries/{expr}/transitions` | `parent, transitions` | {transitions} | - | JWT | ☐ 待测 | L32 |

### salesWorkbench.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | salesWorkbenchApi | overview | GET | `{base}/overview` | `salesId` | {sales_id} | - | JWT | ☐ 待测 | L24 |
| 2 | salesWorkbenchApi | teamDashboard | GET | `{base}/team-dashboard` | `days` | {days} | - | JWT | ☐ 待测 | L30 |
| 3 | salesWorkbenchApi | champion | GET | `{base}/champion` | `days` | {days} | - | JWT | ☐ 待测 | L37 |
| 4 | salesWorkbenchApi | quickActions | GET | `{base}/quick-actions` | - | - | - | JWT | ☐ 待测 | L43 |

### scriptAb.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | scriptAbApi | listVersions | GET | `/api/script-library/{scriptId}/versions` | `scriptId` | - | - | JWT | ☐ 待测 | L4 |
| 2 | scriptAbApi | createVersion | POST | `/api/script-library/{scriptId}/versions` | `scriptId, data` | data | - | JWT | ☐ 待测 | L5 |
| 3 | scriptAbApi | activateVersion | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | `scriptId, versionId` | - | - | JWT | ☐ 待测 | L7 |
| 4 | scriptAbApi | expireScript | POST | `/api/script-library/{scriptId}/expire` | `scriptId, data` | data | - | JWT | ☐ 待测 | L8 |
| 5 | scriptAbApi | getAbStats | GET | `/api/script-library/{scriptId}/ab-stats` | `scriptId, params` | - | params | JWT | ☐ 待测 | L9 |
| 6 | scriptAbApi | updateAbConfig | PUT | `/api/script-library/{scriptId}/ab-config` | `scriptId, data` | data | - | JWT | ☐ 待测 | L10 |
| 7 | scriptAbApi | recordConversion | POST | `/api/script-ab/conversion` | `data` | data | - | JWT | ☐ 待测 | L11 |

### scriptTemplate.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/scripts` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/scripts/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | POST | `/api/scripts` | - | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | - | PUT | `/api/scripts/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 5 | - | - | DELETE | `/api/scripts/{id}` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | - | GET | `/api/scripts/categories` | - | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | GET | `/api/scripts/search` | - | - | params | JWT | ☐ 待测 | L22 |
| 8 | - | - | GET | `/api/scripts/public` | - | - | - | JWT | ☐ 待测 | L25 |
| 9 | - | - | POST | `/api/scripts/recommend` | - | data | - | JWT | ☐ 待测 | L28 |

### securityAudit.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | POST | `/api/security/audit` | - | data | - | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/security/audit/list` | - | - | params | JWT | ☐ 待测 | L8 |
| 3 | - | - | GET | `/api/security/audit/{id}` | - | - | - | JWT | ☐ 待测 | L12 |

### shortLink.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | shortLinkApi | getList | GET | `/api/shortlink/list` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | shortLinkApi | getById | GET | `/api/shortlink/{id}` | `id` | - | - | JWT | ☐ 待测 | L9 |
| 3 | shortLinkApi | create | POST | `/api/shortlink/create` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | shortLinkApi | update | PUT | `/api/shortlink/update` | `data` | data | - | JWT | ☐ 待测 | L17 |
| 5 | shortLinkApi | delete | DELETE | `/api/shortlink/delete/{id}` | `id` | - | - | JWT | ☐ 待测 | L21 |
| 6 | shortLinkApi | access | POST | `/api/shortlink/access` | `data` | data | - | JWT | ☐ 待测 | L25 |
| 7 | shortLinkApi | generateShortCode | POST | `/api/shortlink/generate` | `data` | data | - | JWT | ☐ 待测 | L29 |
| 8 | shortLinkApi | getStats | GET | `/api/shortlink/{id}/stats` | `id, params` | - | params | JWT | ☐ 待测 | L33 |
| 9 | shortLinkApi | getAllStats | GET | `/api/shortlink/stats/all` | `params` | - | params | JWT | ☐ 待测 | L37 |
| 10 | shortLinkApi | share | POST | `/api/shortlink/{id}/share` | `id, data` | data | - | JWT | ☐ 待测 | L41 |

### sms.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | getConfig | GET | `/api/sms/config` | - | - | - | JWT | ☐ 待测 | L5 |
| 2 | - | saveConfig | POST | `/api/sms/config` | `data` | data | - | JWT | ☐ 待测 | L8 |
| 3 | - | getSmsList | GET | `/api/sms/list` | `params` | - | params | JWT | ☐ 待测 | L12 |
| 4 | - | getSmsDetail | GET | `/api/sms/detail/{id}` | `id` | - | - | JWT | ☐ 待测 | L15 |
| 5 | - | resendSms | POST | `/api/sms/resend/{id}` | `id` | - | - | JWT | ☐ 待测 | L18 |
| 6 | - | sendSms | POST | `/api/sms/send` | `data` | data | - | JWT | ☐ 待测 | L21 |
| 7 | - | getDraftList | GET | `/api/sms/draft/list` | `params` | - | params | JWT | ☐ 待测 | L25 |
| 8 | - | getDraftDetail | GET | `/api/sms/draft/{id}` | `id` | - | - | JWT | ☐ 待测 | L28 |
| 9 | - | createDraft | POST | `/api/sms/draft` | `data` | data | - | JWT | ☐ 待测 | L31 |
| 10 | - | updateDraft | PUT | `/api/sms/draft/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L34 |
| 11 | - | deleteDraft | DELETE | `/api/sms/draft/{id}` | `id` | - | - | JWT | ☐ 待测 | L37 |
| 12 | - | sendDraft | POST | `/api/sms/draft/{id}/send` | `id, data` | data | - | JWT | ☐ 待测 | L40 |
| 13 | - | getJobList | GET | `/api/sms/job/list` | `params` | - | params | JWT | ☐ 待测 | L44 |
| 14 | - | getJobDetail | GET | `/api/sms/job/{id}` | `id` | - | - | JWT | ☐ 待测 | L47 |
| 15 | - | pauseJob | POST | `/api/sms/job/{id}/pause` | `id` | - | - | JWT | ☐ 待测 | L50 |
| 16 | - | resumeJob | POST | `/api/sms/job/{id}/resume` | `id` | - | - | JWT | ☐ 待测 | L53 |
| 17 | - | stopJob | POST | `/api/sms/job/{id}/stop` | `id` | - | - | JWT | ☐ 待测 | L56 |
| 18 | - | createJob | POST | `/api/sms/job` | `data` | data | - | JWT | ☐ 待测 | L59 |
| 19 | - | deleteJob | DELETE | `/api/sms/job/{id}` | `id` | - | - | JWT | ☐ 待测 | L62 |
| 20 | - | getJobRecords | GET | `/api/sms/job/{id}/records` | `id, params` | - | params | JWT | ☐ 待测 | L65 |

### sopAgent.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | sopApi | list | GET | `/api/sop` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | sopApi | create | POST | `/api/sop` | `data` | data | - | JWT | ☐ 待测 | L9 |
| 3 | sopApi | getStats | GET | `/api/sop/stats` | - | - | - | JWT | ☐ 待测 | L13 |
| 4 | sopApi | matchByIntent | GET | `/api/sop/match` | `params` | - | params | JWT | ☐ 待测 | L17 |
| 5 | sopApi | listExecutions | GET | `/api/sop/executions` | `params` | - | params | JWT | ☐ 待测 | L21 |
| 6 | sopApi | getExecution | GET | `/api/sop/executions/{id}` | `id` | - | - | JWT | ☐ 待测 | L25 |
| 7 | sopApi | pauseExecution | POST | `/api/sop/executions/{id}/pause` | `id` | - | - | JWT | ☐ 待测 | L29 |
| 8 | sopApi | resumeExecution | POST | `/api/sop/executions/{id}/resume` | `id` | - | - | JWT | ☐ 待测 | L33 |
| 9 | sopApi | cancelExecution | POST | `/api/sop/executions/{id}/cancel` | `id` | - | - | JWT | ☐ 待测 | L37 |
| 10 | sopApi | get | GET | `/api/sop/{id}` | `id` | - | - | JWT | ☐ 待测 | L41 |
| 11 | sopApi | update | PUT | `/api/sop/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L45 |
| 12 | sopApi | remove | DELETE | `/api/sop/{id}` | `id` | - | - | JWT | ☐ 待测 | L49 |
| 13 | sopApi | activate | POST | `/api/sop/{id}/activate` | `id` | - | - | JWT | ☐ 待测 | L53 |
| 14 | sopApi | deactivate | POST | `/api/sop/{id}/deactivate` | `id` | - | - | JWT | ☐ 待测 | L57 |
| 15 | sopApi | execute | POST | `/api/sop/execute` | `data` | data | - | JWT | ☐ 待测 | L61 |
| 16 | sopApi | step | POST | `/api/sop/step` | `data` | data | - | JWT | ☐ 待测 | L65 |

### sopTemplate.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | sopTemplateApi | list | GET | `/api/sop-templates` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | sopTemplateApi | get | GET | `/api/sop-templates/{id}` | `id` | - | - | JWT | ☐ 待测 | L9 |
| 3 | sopTemplateApi | create | POST | `/api/sop-templates` | `data` | data | - | JWT | ☐ 待测 | L13 |
| 4 | sopTemplateApi | update | PUT | `/api/sop-templates/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L17 |
| 5 | sopTemplateApi | toggle | PATCH | `/api/sop-templates/{id}/toggle` | `id, enabled` | {enabled} | - | JWT | ☐ 待测 | L21 |
| 6 | sopTemplateApi | remove | DELETE | `/api/sop-templates/{id}` | `id` | - | - | JWT | ☐ 待测 | L25 |
| 7 | sopTemplateApi | match | POST | `/api/sop-templates/match` | `data` | data | - | JWT | ☐ 待测 | L29 |

### stats.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | StatsApi | getSystemInfo | GET | `/api/system/info` | - | - | - | JWT | ☐ 待测 | L7 |

### tagSegmentation.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | TagSegmentationApi | getTags | GET | `/api/session-tags` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | TagSegmentationApi | updateTags | PUT | `/api/session-tags/{id}` | `data` | body | - | JWT | ☐ 待测 | L12 |
| 3 | TagSegmentationApi | createTag | POST | `/api/session-tags` | `data` | data | - | JWT | ☐ 待测 | L16 |
| 4 | TagSegmentationApi | deleteTag | DELETE | `/api/session-tags/{id}` | `id` | - | - | JWT | ☐ 待测 | L20 |
| 5 | TagSegmentationApi | getTagRules | GET | `/api/customer-360/tag-rules` | `params` | - | params | JWT | ☐ 待测 | L24 |
| 6 | TagSegmentationApi | saveTagRule | POST | `/api/customer-360/tag-rules` | `data` | data | - | JWT | ☐ 待测 | L28 |
| 7 | TagSegmentationApi | updateTagRule | PUT | `/api/customer-360/tag-rules/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L32 |
| 8 | TagSegmentationApi | deleteTagRule | DELETE | `/api/customer-360/tag-rules/{id}` | `id` | - | - | JWT | ☐ 待测 | L36 |
| 9 | TagSegmentationApi | getLayerStrategy | GET | `/api/user-segment/layers` | - | - | - | JWT | ☐ 待测 | L40 |
| 10 | TagSegmentationApi | getTagStats | GET | `/api/customer-360/tag-stats` | `params` | - | params | JWT | ☐ 待测 | L44 |

### telegram.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/telegram/accounts` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/telegram/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/telegram/accounts` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/telegram/accounts/{id}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/telegram/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/telegram/accounts/{id}/register-webhook` | - | data | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | POST | `/api/telegram/accounts/{id}/test-send` | - | data | - | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/telegram/gates` | - | - | params | JWT | ☐ 待测 | L34 |
| 9 | - | - | POST | `/api/telegram/gates` | - | data | - | JWT | ☐ 待测 | L38 |
| 10 | - | - | PUT | `/api/telegram/gates/{id}` | - | data | - | JWT | ☐ 待测 | L42 |
| 11 | - | - | DELETE | `/api/telegram/gates/{id}` | - | - | - | JWT | ☐ 待测 | L46 |
| 12 | - | - | GET | `/api/telegram/gates/{id}/members` | - | - | params | JWT | ☐ 待测 | L50 |
| 13 | - | - | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | - | - | - | JWT | ☐ 待测 | L54 |

### templateMarket.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | listMarketEntries | GET | `/api/template-market/list` | `params, config` | - | params | JWT | ☐ 待测 | L15 |
| 2 | - | marketCategories | GET | `/api/template-market/categories` | `config` | undefined | - | JWT | ☐ 待测 | L18 |
| 3 | - | marketEntryDetail | GET | `/api/template-market/detail/{expr}` | `id` | - | - | JWT | ☐ 待测 | L22 |
| 4 | - | submitMarketEntry | POST | `/api/template-market/entries` | `data` | data | - | JWT | ☐ 待测 | L28 |
| 5 | - | myMarketSubmissions | GET | `/api/template-market/my/submissions` | `config` | undefined | - | JWT | ☐ 待测 | L32 |
| 6 | - | myMarketOrders | GET | `/api/template-market/my/orders` | `config` | undefined | - | JWT | ☐ 待测 | L36 |
| 7 | - | myMarketOwned | GET | `/api/template-market/my/owned` | `config` | undefined | - | JWT | ☐ 待测 | L40 |
| 8 | - | orderMarketEntry | POST | `/api/template-market/entries/{expr}/order` | `entryId, data` | LogicalExpression | - | JWT | ☐ 待测 | L44 |
| 9 | - | cancelMarketOrder | POST | `/api/template-market/orders/{expr}/cancel` | `orderNo` | - | - | JWT | ☐ 待测 | L48 |
| 10 | - | marketReviewList | GET | `/api/template-market/admin/entries` | `params, config` | - | params | JWT | ☐ 待测 | L53 |
| 11 | - | marketReviewAction | POST | `/api/template-market/admin/entries/{expr}/review` | `entryId, data` | data | - | JWT | ☐ 待测 | L57 |
| 12 | - | marketAdminOrders | GET | `/api/template-market/admin/orders` | `params, config` | - | params | JWT | ☐ 待测 | L60 |
| 13 | - | marketMarkPaid | POST | `/api/template-market/admin/orders/{expr}/mark-paid` | `orderNo` | - | - | JWT | ☐ 待测 | L64 |

### tiktokCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/tiktok-card/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/tiktok-card/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/tiktok-card` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/tiktok-card/{id}` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/tiktok-card/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | POST | `/api/tiktok-card/generate-short-link` | - | {card_id} | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/tiktok-card/stats/overall` | - | - | params | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/tiktok-card/{cardId}/stats` | - | - | params | JWT | ☐ 待测 | L32 |

### tuning.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/admin/tuning/confidence/signals` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/admin/tuning/confidence/signals/{id}` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | GET | `/api/admin/tuning/confidence/signals/stats` | - | - | params | JWT | ☐ 待测 | L10 |
| 4 | - | - | GET | `/api/admin/tuning/confidence/calibrations` | - | - | params | JWT | ☐ 待测 | L13 |
| 5 | - | - | GET | `/api/admin/tuning/confidence/policies` | - | - | params | JWT | ☐ 待测 | L16 |
| 6 | - | - | PUT | `/api/admin/tuning/confidence/policies` | - | data | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | GET | `/api/admin/tuning/humanize/scores` | - | - | params | JWT | ☐ 待测 | L23 |
| 8 | - | - | GET | `/api/admin/tuning/humanize/scores/stats` | - | - | params | JWT | ☐ 待测 | L26 |
| 9 | - | - | GET | `/api/admin/tuning/humanize/baselines` | - | - | params | JWT | ☐ 待测 | L29 |
| 10 | - | - | GET | `/api/admin/tuning/humanize/low-quality` | - | - | params | JWT | ☐ 待测 | L32 |
| 11 | - | - | GET | `/api/admin/tuning/feedback/events` | - | - | params | JWT | ☐ 待测 | L36 |
| 12 | - | - | GET | `/api/admin/tuning/feedback/events/stats` | - | - | params | JWT | ☐ 待测 | L39 |
| 13 | - | - | GET | `/api/admin/tuning/feedback/dialogues` | - | - | params | JWT | ☐ 待测 | L42 |
| 14 | - | - | GET | `/api/admin/tuning/prompt/candidates` | - | - | params | JWT | ☐ 待测 | L45 |
| 15 | - | - | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | - | data | - | JWT | ☐ 待测 | L48 |
| 16 | - | - | GET | `/api/admin/tuning/bandit/arms` | - | - | params | JWT | ☐ 待测 | L51 |

### unifiedMessage.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | unifiedMessageApi | getMessages | GET | `/api/unified-messages` | `params` | - | params | JWT | ☐ 待测 | L5 |
| 2 | unifiedMessageApi | getMessageById | GET | `/api/unified-messages/{id}` | `id` | - | - | JWT | ☐ 待测 | L9 |

### unknownQuestion.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | unknownQuestionApi | list | GET | `/api/unknown-questions` | `params` | - | params | JWT | ☐ 待测 | L7 |
| 2 | unknownQuestionApi | stats | GET | `/api/unknown-questions/stats` | - | - | - | JWT | ☐ 待测 | L10 |
| 3 | unknownQuestionApi | get | GET | `/api/unknown-questions/{id}` | `id` | - | - | JWT | ☐ 待测 | L13 |
| 4 | unknownQuestionApi | submit | POST | `/api/unknown-questions/{id}/submit` | `id` | - | - | JWT | ☐ 待测 | L16 |
| 5 | unknownQuestionApi | review | POST | `/api/unknown-questions/{id}/review` | `id, data` | data | - | JWT | ☐ 待测 | L19 |
| 6 | unknownQuestionApi | clusterList | GET | `/api/unknown-questions/clusters` | `params` | - | params | JWT | ☐ 待测 | L23 |
| 7 | unknownQuestionApi | clusterGet | GET | `/api/unknown-questions/clusters/{id}` | `id` | - | - | JWT | ☐ 待测 | L26 |
| 8 | unknownQuestionApi | clusterReview | POST | `/api/unknown-questions/clusters/{id}/review` | `id, data` | data | - | JWT | ☐ 待测 | L29 |

### userSegment.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/user-segment/rfm/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/user-segment/rfm/rule` | - | - | - | JWT | ☐ 待测 | L7 |
| 3 | - | - | POST | `/api/user-segment/rfm/rule` | - | data | - | JWT | ☐ 待测 | L10 |
| 4 | - | - | PUT | `/api/user-segment/rfm/rule/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 5 | - | - | GET | `/api/user-segment/rfm/user?user_id={userId}` | - | - | - | JWT | ☐ 待测 | L16 |
| 6 | - | - | GET | `/api/user-segment/rfm/stats` | - | - | - | JWT | ☐ 待测 | L19 |
| 7 | - | - | POST | `/api/user-segment/rfm/calculate` | - | data | - | JWT | ☐ 待测 | L22 |
| 8 | - | - | GET | `/api/user-segment/layers` | - | - | - | JWT | ☐ 待测 | L25 |
| 9 | - | - | GET | `/api/user-segments` | - | - | params | JWT | ☐ 待测 | L29 |
| 10 | - | - | DELETE | `/api/user-segment/rfm/rule/{id}` | - | - | - | JWT | ☐ 待测 | L41 |
| 11 | - | - | GET | `/api/user-segment/rfm/list?segment_id={id}` | - | - | params | JWT | ☐ 待测 | L44 |

### wecomAccount.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | wecomAccountApi | listAccounts | GET | `/api/wecom/health/accounts` | - | - | - | JWT | ☐ 待测 | L5 |
| 2 | wecomAccountApi | listRiskAccounts | GET | `/api/wecom/health/accounts/risks` | - | - | - | JWT | ☐ 待测 | L9 |
| 3 | wecomAccountApi | selectHealthyAccount | GET | `/api/wecom/health/accounts/select` | - | - | - | JWT | ☐ 待测 | L13 |
| 4 | wecomAccountApi | getHealthSummary | GET | `/api/wecom/health/accounts/summary` | - | - | - | JWT | ☐ 待测 | L17 |
| 5 | wecomAccountApi | getLatestHealth | GET | `/api/wecom/health/accounts/{id}` | `id` | - | - | JWT | ☐ 待测 | L21 |
| 6 | wecomAccountApi | listHealthHistory | GET | `/api/wecom/health/accounts/{id}/history` | `id, params` | - | params | JWT | ☐ 待测 | L25 |
| 7 | wecomAccountApi | reportHealth | POST | `/api/wecom/health/accounts/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L29 |
| 8 | wecomAccountApi | updateAccountStatus | POST | `/api/wecom/health/accounts/{id}/status` | `id, data` | data | - | JWT | ☐ 待测 | L33 |
| 9 | wecomAccountApi | consumeQuota | POST | `/api/wecom/health/accounts/{id}/quota/consume` | `id, count` | {count} | - | JWT | ☐ 待测 | L37 |
| 10 | wecomAccountApi | resetDailyQuota | POST | `/api/wecom/health/accounts/quota/reset` | - | - | - | JWT | ☐ 待测 | L41 |
| 11 | wecomAccountApi | ingestMessage | POST | `/api/wecom/messages/ingest` | `data` | data | - | JWT | ☐ 待测 | L45 |
| 12 | wecomAccountApi | sendMessage | POST | `/api/wecom/messages/send` | `data` | data | - | JWT | ☐ 待测 | L49 |
| 13 | wecomAccountApi | createAccount | POST | `/api/wecom/accounts` | `data` | data | - | JWT | ☐ 待测 | L53 |
| 14 | wecomAccountApi | updateAccount | PUT | `/api/wecom/accounts/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L57 |
| 15 | wecomAccountApi | deleteAccount | DELETE | `/api/wecom/accounts/{id}` | `id` | - | - | JWT | ☐ 待测 | L61 |
| 16 | wecomAccountApi | getCustomers | GET | `/api/wecom/customers` | `params` | - | params | JWT | ☐ 待测 | L65 |
| 17 | wecomAccountApi | getGroups | GET | `/api/wecom/groups` | `params` | - | params | JWT | ☐ 待测 | L69 |
| 18 | wecomAccountApi | getTags | GET | `/api/wecom/tags` | - | - | - | JWT | ☐ 待测 | L73 |
| 19 | wecomAccountApi | getMessages | GET | `/api/wecom/messages` | `params` | - | params | JWT | ☐ 待测 | L77 |
| 20 | wecomAccountApi | syncCustomers | POST | `/api/wecom/accounts/{id}/sync-customers` | `id` | - | - | JWT | ☐ 待测 | L81 |
| 21 | wecomAccountApi | syncGroups | POST | `/api/wecom/accounts/{id}/sync-groups` | `id` | - | - | JWT | ☐ 待测 | L85 |
| 22 | wecomAccountApi | syncTags | POST | `/api/wecom/accounts/{id}/sync-tags` | `id` | - | - | JWT | ☐ 待测 | L89 |
| 23 | wecomAccountApi | refreshAccount | POST | `/api/wecom/accounts/{id}/refresh` | `id` | - | - | JWT | ☐ 待测 | L93 |
| 24 | wecomAccountApi | sendMessageById | POST | `/api/wecom/accounts/{id}/send-message` | `id, data` | data | - | JWT | ☐ 待测 | L97 |

### whatsapp.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/whatsapp/accounts` | - | - | params | JWT | ☐ 待测 | L3 |
| 2 | - | - | POST | `/api/whatsapp/accounts` | - | data | - | JWT | ☐ 待测 | L5 |
| 3 | - | - | POST | `/api/whatsapp/accounts/{id}/login/start` | - | - | - | JWT | ☐ 待测 | L6 |
| 4 | - | - | GET | `/api/whatsapp/accounts/{id}/login/status` | - | - | - | JWT | ☐ 待测 | L7 |
| 5 | - | - | PUT | `/api/whatsapp/accounts/{id}` | - | data | - | JWT | ☐ 待测 | L8 |
| 6 | - | - | DELETE | `/api/whatsapp/accounts/{id}` | - | - | - | JWT | ☐ 待测 | L9 |
| 7 | - | - | GET | `/api/whatsapp/drafts` | - | - | params | JWT | ☐ 待测 | L11 |
| 8 | - | - | POST | `/api/whatsapp/drafts` | - | data | - | JWT | ☐ 待测 | L12 |
| 9 | - | - | PUT | `/api/whatsapp/drafts/{id}` | - | data | - | JWT | ☐ 待测 | L13 |
| 10 | - | - | DELETE | `/api/whatsapp/drafts/{id}` | - | - | - | JWT | ☐ 待测 | L14 |
| 11 | - | - | GET | `/api/whatsapp/jobs` | - | - | params | JWT | ☐ 待测 | L16 |
| 12 | - | - | POST | `/api/whatsapp/jobs` | - | data | - | JWT | ☐ 待测 | L17 |
| 13 | - | - | GET | `/api/whatsapp/jobs/{id}` | - | - | - | JWT | ☐ 待测 | L18 |
| 1 | - | - | GET | `/api/whatsapp-cloud/accounts` | - | - | params | JWT | ☐ 待测 | L6 |
| 2 | - | - | GET | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L10 |
| 3 | - | - | POST | `/api/whatsapp-cloud/accounts` | - | data | - | JWT | ☐ 待测 | L14 |
| 4 | - | - | PUT | `{BASE}/{id}` | - | data | - | JWT | ☐ 待测 | L18 |
| 5 | - | - | DELETE | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L22 |
| 6 | - | - | POST | `{BASE}/{id}/test-send` | - | data | - | JWT | ☐ 待测 | L26 |
| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L10 |
| 2 | - | - | PUT | `{BASE}/{id}` | - | data | - | JWT | ☐ 待测 | L18 |
| 3 | - | - | DELETE | `{BASE}/{id}` | - | - | - | JWT | ☐ 待测 | L22 |
| 4 | - | - | POST | `{BASE}/{id}/test-send` | - | data | - | JWT | ☐ 待测 | L26 |

### workflowOrchestrator.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | workflowOrchestratorApi | listVersions | GET | `/api/workflows/versions` | `workflowIdOrParams` | {} | - | JWT | ☐ 待测 | L6 |
| 2 | workflowOrchestratorApi | listVersions | GET | `/api/workflows/versions` | `workflowIdOrParams` | {workflow_id} | - | JWT | ☐ 待测 | L9 |
| 3 | workflowOrchestratorApi | listVersions | GET | `/api/workflows/versions` | `workflowIdOrParams` | - | params | JWT | ☐ 待测 | L17 |
| 4 | workflowOrchestratorApi | getVersion | GET | `/api/workflows/versions/{id}` | `id` | - | - | JWT | ☐ 待测 | L21 |
| 5 | workflowOrchestratorApi | createVersion | POST | `/api/workflows/versions` | `data` | data | - | JWT | ☐ 待测 | L25 |
| 6 | workflowOrchestratorApi | updateVersion | PUT | `/api/workflows/versions/{id}` | `id, data` | data | - | JWT | ☐ 待测 | L29 |
| 7 | workflowOrchestratorApi | deleteVersion | DELETE | `/api/workflows/versions/{id}` | `id` | - | - | JWT | ☐ 待测 | L33 |
| 8 | workflowOrchestratorApi | publishVersion | POST | `/api/workflows/versions/{id}/publish` | `id` | - | - | JWT | ☐ 待测 | L37 |
| 9 | workflowOrchestratorApi | archiveVersion | POST | `/api/workflows/versions/{id}/archive` | `id` | - | - | JWT | ☐ 待测 | L41 |
| 10 | workflowOrchestratorApi | execute | POST | `/api/workflows/execute` | `data` | data | - | JWT | ☐ 待测 | L45 |
| 11 | workflowOrchestratorApi | listExecutions | GET | `/api/workflows/executions` | `params` | - | params | JWT | ☐ 待测 | L49 |
| 12 | workflowOrchestratorApi | getExecution | GET | `/api/workflows/executions/{id}` | `id` | - | - | JWT | ☐ 待测 | L53 |
| 13 | workflowOrchestratorApi | getNodeExecutions | GET | `/api/workflows/executions/{executionId}/nodes` | `executionId` | - | - | JWT | ☐ 待测 | L57 |
| 14 | workflowOrchestratorApi | stopExecution | POST | `/api/workflows/executions/{id}/stop` | `id` | - | - | JWT | ☐ 待测 | L61 |

### xianyuCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/xianyu/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/xianyu/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/xianyu/create` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/xianyu/update` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/xianyu/delete/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/xianyu/view/{id}` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/xianyu/stats/card/{id}` | - | - | params | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/xianyu/stats/overall` | - | - | params | JWT | ☐ 待测 | L32 |
| 9 | - | - | POST | `/api/xianyu/{id}/generate-short-link` | - | - | - | JWT | ☐ 待测 | L36 |

### xiaohongshuCard.js

| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | - | - | GET | `/api/xiaohongshu/list` | - | - | params | JWT | ☐ 待测 | L4 |
| 2 | - | - | GET | `/api/xiaohongshu/{id}` | - | - | - | JWT | ☐ 待测 | L8 |
| 3 | - | - | POST | `/api/xiaohongshu/create` | - | data | - | JWT | ☐ 待测 | L12 |
| 4 | - | - | PUT | `/api/xiaohongshu/update` | - | data | - | JWT | ☐ 待测 | L16 |
| 5 | - | - | DELETE | `/api/xiaohongshu/delete/{id}` | - | - | - | JWT | ☐ 待测 | L20 |
| 6 | - | - | GET | `/api/xiaohongshu/view/{id}` | - | - | - | JWT | ☐ 待测 | L24 |
| 7 | - | - | GET | `/api/xiaohongshu/stats/card/{id}` | - | - | params | JWT | ☐ 待测 | L28 |
| 8 | - | - | GET | `/api/xiaohongshu/stats/overall` | - | - | params | JWT | ☐ 待测 | L32 |
| 9 | - | - | POST | `/api/xiaohongshu/{id}/generate-short-link` | - | - | - | JWT | ☐ 待测 | L36 |

# 功能模块化 · 模块清单论证（任务一交付物）

> **状态**：v1.2 · 2026-10-19（定价与离线口径按 003 计费二期勘误，见 §3.5）
> **适用范围**：`hivemtk`（用户端）+ `hivemtk-platform`（平台端）
> **事实源**：
> - 菜单注册表 `user-server/internal/model/menu.go`（实测 **208 个节点**，本次由 `go run` 导出核对，非抄文档）
> - 数据模型 `user-server/internal/model/*.go`（实测 **270 张表**，归属表见
>   `internal/model/module_tables.go`，由 `TestEveryModelTableHasModuleOwner`
>   钉死「一张表都不许无主」）
> - 路由 `user-server/internal/router/*.go`（37 个非测试路由文件）
> - 平台端模型 `hivemtk-platform/platform-server/internal/model/*.go`（20 张表）

---

## 零、先说结论（一页纸）

| 项 | 结论 |
| --- | --- |
| 模块总数 | **24 个**：基础模块 **7** 个 + 收费模块 **17** 个 |
| 基础模块判据 | 「关掉它系统就**装不起来 / 进不来 / 没数据可管 / AI 没素材**」→ 必须免费且常开 |
| 收费模块判据 | 「关掉它系统仍能跑，只是少一块**增值能力**」→ 可订阅、可为 0 元 |
| 0 元收费模块 | 允许存在：`price=0` = **需开通、不收钱**（走完整订单+确认流程，落盘为 `active`），不是"免开通" |
| 模块粒度 | 按「**菜单子树 + 数据表簇 + 独立业务闭环**」三合一切，不按代码目录切 |
| 授权模型 | `module_key` 挂在菜单节点上；用户端菜单 = **平台已开通模块 ∩ 角色菜单授权** |
| 依赖模型 | `module_dependencies` 有向图，**开通时按依赖闭包自动补齐**（不开 AI 智能体却要它？自动带上依赖） |
| 收费形态 | **2026-10-30 起**：7 个基础模块 **0 元免费常开** + 17 个增值模块中 **16 个 0 元开通**、**仅浏览器自动化 ¥1 买断** + 软件订阅 **¥9（按 30 天计，15 天体验期）**；LLM 是**计量型订阅**（token 钱包：体验包 ¥1 / 标准包 ¥3 / 专业包 ¥6 / 旗舰包 ¥9，新商户注册另赠 10M 输入 + 2M 输出），资产包沿用积分制（详见 §3.5） |
| 支付形态 | **003 起**：已接入**易支付**（先 `EPAY_MOCK` 后真人，ADR-M9），线下人工确认保留为回落通道，两者共用 `MarkPaid` **唯一入账口**（ADR-M3）；订单/回调/签名仍按线上支付契约建模 |

---

## 一、模块划分原则（为什么这么切）

### 1.1 三个切分口径必须同时满足

一个"功能模块"要能被单独定价、单独关闭、单独理解，必须同时满足：

| 口径 | 含义 | 违反的后果 |
| --- | --- | --- |
| **菜单子树** | 模块覆盖的菜单节点在注册表里是一个可枚举的集合 | 用户关了模块却仍看到菜单 → 点了 403，"关了等于没关" |
| **数据表簇** | 模块独占一批表（或至少声明"核心表"） | 关模块后表还在跑定时任务 → 数据继续增长，关了不省钱也不干净 |
| **业务闭环** | 模块自己能讲清"我给用户带来什么价值" | 没法写商品详情、没法定价、没法做市场卡片 |

### 1.2 明确不作为切分依据的东西

- ❌ **Go 包目录**：`internal/geo/` 有 144 个文件、8 个顶级分组（GEO 智能优化 30+ 菜单节点），按目录切会切出一个用户看不懂的怪物。
- ❌ **数据表数量**：表多的模块不一定贵（`customer` 40 张表但是基础能力），表少的模块可能很贵（`geo` 6 张表但是 30 个页面 + 外部发布链路）。
- ❌ **代码行数 / 维护成本**：定价看的是**用户感知价值**，不是我们的开发成本。

### 1.3 粒度标尺（这次落地的具体数字）

| 判据 | 目标区间 | 本方案实际 |
| --- | --- | --- |
| 单模块顶级菜单节点数 | 1–3 | 最多 3（`im-basic` 6 个子目录但只有 1 个顶级节点 `reach`→下属 6 个目录……见 §2 说明） |
| 单模块页面数（含非菜单详情页） | 3–40 | `geo` 30 页（上限，因其天然是一个完整产品线） |
| 单模块独占表数 | 0–40 | `customer` 独占 22 张（上限） |
| 订单碎片度 | 用户一次下单最多覆盖 5 个模块 | 免费组合包会把 17 个模块压成 1 档，运营上失去颗粒度 → **不做套餐包**，只做**单模块 + 依赖自动补齐** |

---

## 二、模块总清单（24 个）

图例：`[B]` 基础模块（Base，免费常开） · `[P]` 收费模块（Paid，`price>=0`）

### L0 · 基础模块（7 个）

> **论证**：这 7 个模块构成"一个能装、能登、能收客、能管客、能喂 AI、能连平台"的最小闭环。少任何一个，付费模块都失去承载对象。

#### B1 `core` · 系统基座

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `system`(顶级) · `siteConfig` · `systemConfig` `systemObsConfig` `systemMaterialLibrary` `systemMonitor` `systemTrace` `systemGuide` `automationCenter` `dynamicThreshold` `systemDictionaries` `systemFeatureFlags` `ltcConfig` `opsOverview` · `teamGroup` `systemUser` `roleManage` `permissionManage` · `permissionGroup` `platformAccount` `integration` `bridgeToken` `operationLog` `securityAudit` · `backupList` `backupEnhanced` |
| 关键表 | `system_users` `user_mfa` `login_events` `password_history` `password_reset_tokens` `sso_identities` `role_menu_permissions` `system_config` `system_config_kv` `config_params` `config_param_groups` `config_param_audit_logs` `dictionaries` `feature_flags` `feature_flag_audit_logs` `feature_flag_eval_logs` `alert_rules` `alert_histories` `operation_logs` `security_audits` `security_audit_items` `security_alerts` `backups` `restore_records` `obs_config` `platform_accounts` `platform_account_configs` `integration_accounts` `integration_templates` `sync_logs` `api_logs` `system_metrics` `visit_logs` `mcp_credentials` |
| 依赖 | 无（根模块） |
| 为何基础 | 认证、权限、站点、审计是**合规与可运营的底线**，收费与否不影响其存在必要性 |
| 定价 | 免费常开，不可关闭 |

#### B2 `customer` · 客户中心

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `customer`(顶级) · `clueList` `clueStatistics` `leadMining` `customer360` `customerEvent` `tagSegmentation` `userSegment` `unifiedMessage` · `customerIdentity` `oneidList` `oneidConflicts` `oneidMergeRules` |
| 关键表 | `customers` `customer_channels` `customer_events` `customer_tags` `customer_tag_assignments` `customer_segments` `customer_rfm` `customer_rfms` `rfm_rules` `customer_do_not_contact` `clues` `clue_scores` `clue_engagement_events` `customer_long_term_memory` `churn_scores` `unified_messages` `unified_replies` `user_tags` `user_blacklist` `sms_number_portability_logs` `sms_unsubscribes` `email_unsubscribes` |
| 依赖 | `core` |
| 为何基础 | **所有收费模块的承载对象**：AI 智能体读客户、触达发客户、分析算客户。客户是"资产"，模块是"产能"，先给资产再谈产能 |
| 定价 | 免费常开，不可关闭 |

#### B3 `inbox` · 会话工作台

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `workspace`(顶级) · `inbox` `messageHub` `messageHubDashboard` `customerSession` · `agentWorkbench` `agentStatus` `csatDashboard` `quickReply` `sessionTag` `aiSuggestion` · `agentChannel` `chatChannelList` · `systemConfig`(跨模块复用) |
| 关键表 | `inbox_conversations` `inbox_assignments` `customer_sessions` `session_messages` `session_tags` `session_ai_summaries` `quick_replies` `quick_reply_folders` `message_hub` `msg_hourly_summary` `message` `chat_channels` `agent_statuses` `customer_service_agents` `csat_surveys` `ai_suggestions` `sla_policies` `sla_first_response_at` `sla_escalate_at` `sla_decide_at` `sla_violations` |
| 依赖 | `core` `customer` |
| 为何基础 | **"能收客"的最短路径**。用户装系统的第一天感受到的价值就是"客户消息进来了"，这是产品存在性的证明 |
| 定价 | 免费常开，不可关闭 |

> ⚠️ **跨模块耦合例外**：`chatChannelList`（客服 Web Widget 渠道）既是工作台入口，也是 `embed-sdk` 的凭据来源。关闭工作台会导致外部站点挂掉的客服窗，故 `chatChannelList` 归 `core` 而非 `inbox`（详见 §四 4.3）。

#### B4 `knowledge` · 知识库

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `knowledge`(顶级) · `knowledgeWorkspace` `knowledgeManagement` `knowledgeBatchImport` `knowledgePlayground` `knowledgeFeedbacks` `knowledgeTokens` `knowledgeExternal` `knowledgeStatistics` `knowledgeRagEval` `knowledgeOpenAPI` `knowledgeConnectors` · `faqKb` · `ragProductConfig` `ragMainConfig` `ragAccount` `ragProduct` · `ragOverview` |
| 关键表 | `knowledge_bases` `knowledge_documents` `knowledge_chunks` `knowledge_api_tokens` `knowledge_feedbacks` `knowledge_import_logs` `knowledge_openapi_sources` `knowledge_search_logs` `kb_documents` `kb_releases` `kb_change_audit_logs` `kb_change_requests` `external_kb_connectors` `external_kb_sync_items` `external_import_jobs` `faq_entries` `rag_products` `rag_metrics_daily` `rag_query_logs` `rag_eval_runs` `rag_eval_questions` `rag_recall_monitor_snapshots` |
| 依赖 | `core` |
| 为何基础 | 知识库是 **AI 能力的原材料**。用户不买 `ai-agent` 也能用知识库检索（这是可见的独立价值）；但没有知识库，AI 模块就是空壳 |
| 定价 | 免费常开，不可关闭 |

#### B5 `asset` · 资产与话术

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `assetBundle` 目录 · `assetMarket` `myAssets` `syncLog` · `scriptTemplate` `scriptTemplateAbStats` · `systemMaterialLibrary`（跨模块复用） |
| 关键表 | `local_assets` `local_asset_data` `local_asset_sync_log` `asset_bundles` `asset_bundle_version_logs` `script_library` `script_versions` `script_exposure_logs` `prompt_ab_tests` `prompt_candidates` `usage_reports`(平台侧) |
| 依赖 | `core` `knowledge` |
| 为何基础 | 资产市场是**平台化商业模式的第一个样板**（已跑通：贡献者上传→审核→商户购买→积分扣减→同步落盘）。功能模块市场直接复用它的交互形态，把它做成基础能力是降低后续模块市场开发成本的关键 |
| 定价 | 免费常开，不可关闭 |

#### B6 `llm-self` · 自有 LLM 接入（仅统计，不计费）

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `llmRouting`（`agentPassive` 子节点） |
| 关键表 | `llm_providers` `llm_routing_rules` `llm_routing_logs` `layer_decision_logs` `trace_events` |
| 依赖 | `core` |
| 为何基础 | 用户**自带 API Key** 时不应收费——收费的是"平台代付的算力"，不是"路由能力"。路由/降级/成本可视化本身是基础工具 |
| 定价 | 免费常开，不可关闭。**但**：`llm_providers.source='platform'` 的调用走 `llm-cloud` 计量模块（见 P12） |

#### B7 `platform-link` · 平台对接与模块下发（新增）

| 项 | 内容 |
| --- | --- |
| 菜单节点 | 无独立菜单（隐藏在系统设置底部 / 状态条）。功能性菜单只有 1 个待新增：`moduleCenter`（我的功能模块） |
| 关键表 | `platform_installs` `platform_heartbeats` `platform_install`(本端) · **新增** `tenant_modules` `module_sync_logs` |
| 依赖 | `core` |
| 为何基础 | 这是**模块化的物理通道**。离线部署（`PLATFORM_ENABLED=false`）时本模块降级为"全部基础模块自动开通"，不产生外呼（零出域铁律） |
| 定价 | 免费常开，不可关闭 |

---

### L1 · 收费模块（17 个）

> **定价单位**：一期统一用**元/年**（`price_cents` 存分，避免浮点）。
>
> ⚠️ **勘误（003 · 2026-10-19；2026-10-21 / 2026-10-30 定价改版）**：下表「建议定价（元/年）」是 **002 时期的建议值，已不构成售卖口径**——
> 现行目录里 7 个 base 模块 `price_cents=0`（免费常开）、17 个 paid 模块中 **16 个 `price_cents=0`（0 元开通）**、
> 仅 `browser-automation` **¥1**（`billing=once`），软件订阅 **¥9**；价格的运行时权威在**平台端目录**，编译期常量仅作种子。
> 新口径与覆盖声明见 **§3.5**。
> `price_cents = 0` 对 base 表示**免费常开**，对 paid 表示**需开通但不收费**。

| # | key | 名称 | 菜单归属 | 依赖 | 核心表簇 | 建议定价（元/年） | 一句话价值 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| P1 | `ai-agent` | AI 智能体 | `aiAgent.agentManage` `aiAgent.agentChannel` | `core` `customer` `inbox` `knowledge` `llm-self` | `ai_agents` `channel_agent_bindings` `customer_service_agents` `agent_kb_bindings` `agent_statuses` `ai_tool_configs` `ai_tool_account_bindings` | 2980 | 7×24 不休息的 AI 销售/客服，自动应答真实客户消息 |
| P2 | `ai-sales` | AI 销冠 | `aiAgent.agentPassive` `aiAgent.agentSupport` `aiAgent.agentSalesIntel` + `analytics.confidencePanel` `humanizePanel` `feedbackLoopPanel` | `core` `customer` `inbox` `llm-self` `ai-agent` | `dialogue_memories` `business_memories` `intent_records` `intent_examples` `sales_intent_scores` `objection_templates` `sales_personas` `sales_champion_profile_snapshots` `champion_baselines` `champion_dialogues` `champion_phrases` `sop_agents` `sop_templates` `sop_executions` `sop_node_transitions` `sop_state_memories` `sop_timers` `sop_outbox` `confidence_signals` `confidence_calibrations` `humanize_scores` `humanize_dimensions` `feedback_records` `feedback_signals` `feedback_events` `learning_insights` `bandit_arms` `bandit_reflux_log` `low_quality_samples` `ab_test_stats`(话术 AB 部分) `dialogue_eval_runs` `trace_eval_log` | 3980 | 把销冠的话术、意图、异议应对沉淀成可复制资产，SOP 自动推进成交 |
| P3 | `llm-cloud` | 平台 LLM 算力 | 无独立菜单（并入 `aiProductivity` 成本页 + 新增余额条） | `core` `llm-self` | **新增** `llm_plans` `llm_wallets` `llm_wallet_txns` `llm_usage_records`；复用 `llm_providers` `llm_routing_logs` | 计量（token 包，非年费） | 不想自己申请 API Key，也能用平台垫资的大模型 |
| P4 | `reach-auto` | 营销自动化 | `aiAgent.agentActive` + `reach.workflowOrchestrator` | `core` `customer` | `reach_pipelines` `reach_jobs` `reach_delayed_outbound` `reach_compliance_log` `automation_rules` `workflow_orchestrator` `workflow_versions` `workflow_executions` `workflow_node_executions` `batch` `saved_views` | 1980 | 线索进来自动跑多轮触达，不用人工群发 |
| P5 | `reach-sms` | 短信触达 | `reach.smsReach` | `core` `customer` `reach-auto` | `sms` `sms_unsubscribes` `sms_tracking` `sms_delivery_statuses` `sm_list` `sms_job_metrics` `sms_number_portability_logs` | 980 | 国内验证码/通知/营销短信 |
| P6 | `reach-email` | 邮件触达 | `reach.emailReach` | `core` `customer` `reach-auto` | `email_list` `email_smtp` `email_jobs` `email_tracking_events` `email_unsubscribes` `email_job_metrics` | 980 | 冷启动邮件触达 + 送达率管理 |
| P7 | `reach-social` | 社媒卡片 + 浏览器自动化 | `reach.douyin` `kuaishou` `xiaohongshu` `xianyu` `tiktok` + `analytics.browserAutomation` | `core` `customer` `reach-auto` | `douyin_cards` `kuaishou_cards` `xiaohongshu_cards` `xianyu_cards` `tiktok_cards`（各含 `_activities`）`live_codes` `live_code_qrs` `live_code_qr_stats` `livecode_click_log` `qr_code_click_log` | 1980 | 主流内容平台的引流卡片 + 自动私信回复（浏览器自动化） |
| P8 | `reach-link` | 短链活码与域名池 | `reach.shortLink` `reach.livecode` | `core` | `short_links` `short_link_accesses` `live_code_qrs` `domain_pool` `domain_blacklist` `domain_health_log` | 380 | 可追踪的短链/活码投放 |
| P9 | `im-wecom` | 企业微信 | `workspace.wecomAccount` + `agentChannel` | `core` `customer` `inbox` | `wecom_accounts` `wecom_account_health` `wecom_groups` `wecom_group_members` `wecom_messages` `wecom_tags` `wecom_customers` | 2980 | 国内私域主力通道 |
| P10 | `im-intl` | 海外 IM（Telegram / QQ / 飞书 / 钉钉 / WhatsApp / WhatsApp Cloud） | `reach.telegram` `qq` `feishu` `whatsapp` + `agentChannel.dingtalkApp` `whatsappCloud` + `reach.community` | `core` `customer` `inbox` | `telegram_accounts` `telegram_group_gates` `telegram_group_members` `qq_accounts` `feishu_accounts` `feishu_customers` `feishu_messages` `dingtalk_app_accounts` `whatsapp_accounts` `whatsapp_sessions` `whatsapp_messages` `whatsapp_drafts` `whatsapp_jobs` `whatsapp_job_details` `whatsapp_message_queues` `whatsapp_group_messages` `whatsapp_message_templates` `whatsapp_cloud_accounts` `community_groups` `community_members` `community_messages` | 1980 | 出海私域全渠道 |

> ⚠️ **`im-intl` 为什么合成一个而不是拆 6 个**：6 个渠道 × 每渠道 ~6 张表 = 36 张表，但**业务价值同质**（都是"接一个 IM 通道收发消息"），且**任一渠道单独定价用户都嫌贵**。拆开会让订单碎片化到 6 档却都不值当。合成一档 + 在平台端用 `module_options` 声明"支持哪些子渠道"（`whatsapp/telegram/qq/feishu/dingtalk/community`），用户可勾选。**这是本清单唯一一处"故意不细拆"**，已在 §五 5.2 记录取舍理由。

#### P11 `analytics` · 数据分析

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `analytics`(顶级) · `dashboardScreen` `conversionFunnel` `aiProductivity` `customReport` `customerJourney` `cohortPath` |
| 关键表 | `daily_stats` `conversion_funnels` `customer_journey`(视图/派生) `report_subscriptions` `ab_test_metrics` `external_customers` `web_vital_records` `msg_hourly_summary` `support_metrics` |
| 依赖 | `core` `customer` `inbox` |
| 定价 | 980 |

#### P12 `experiment` · 实验与预测

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `analytics.abExperiment` `analytics.churnPrediction` |
| 关键表 | `ab_tests` `ab_test_metrics` `ab_test_stats` `prompt_ab_tests` `churn_scores` `bandit_arms` `bandit_reflux_log` |
| 依赖 | `core` `customer` `analytics` |
| 定价 | 1280 |

#### P13 `browser-automation` 单列

> 已并入 P7 的口径说明见下表（原 P7 含 baTasks 等 4 节点）。
> **单独保留为 P13**：浏览器自动化（chromedp 接管真实浏览器做私信/评论）**技术栈与成本结构完全不同**（需要常驻 Chromium + 内存），定价必须能独立开/关，且它是 5 平台卡片自动回复的**前置依赖**。

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `analytics.browserAutomation` `baTasks` `baCreate` `baCron` `baStatus` |
| 关键表 | `automation_rules` `browser_sessions`(派生) `cron_job_leases` |
| 依赖 | `core` `customer` |
| 定价 | 1680 |

#### P14 `geo` · GEO 智能优化

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `analytics.geoTools`（30 个子节点，4 个子目录） |
| 关键表 | GEO 自有表簇（`internal/geo/model`，品牌/域名/关键词/内容/图谱/推送/竞品/SOV/告警 共 ~20 张） |
| 依赖 | `core` `knowledge` |
| 定价 | 2980 |

#### P15 `sales-crm` · 销售工作台

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `workspace.salesCockpit` `orderDraft` `salesWorkbench` `followups` |
| 关键表 | `opportunities` `quotes` `quote_line_items` `order_drafts` `bills` `payments` `account` `aftersales` `sales_events` `recovery_queue` `external_orders` `external_products` |
| 依赖 | `core` `customer` `ai-sales` |
| 定价 | 1980 |

#### P16 `approval` · 审批与待办

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `workspace.approvalTask` `workspace.badCase` |
| 关键表 | `approval_requests` `human_tasks` `bad_cases` `handoff_decisions` `rule_pending_executions` `optimization_suggestions` |
| 依赖 | `core` |
| 定价 | 680 |

#### P17 `i18n` · 多语言管理

| 项 | 内容 |
| --- | --- |
| 菜单节点 | `knowledge.i18nManage` `glossary` `i18nDashboard` |
| 关键表 | `glossaries` `dictionary_transitions` |
| 依赖 | `core` |
| 定价 | 480 |

---

### 2.1 清单速查表

| key | 类型 | 菜单节点数 | 独占表数(约) | 依赖数 | 默认定价 |
| --- | --- | --- | --- | --- | --- |
| `core` | B | 26 | 34 | 0 | 免费 |
| `customer` | B | 12 | 21 | 1 | 免费 |
| `inbox` | B | 13 | 18 | 2 | 免费 |
| `knowledge` | B | 16 | 23 | 1 | 免费 |
| `asset` | B | 6 | 12 | 2 | 免费 |
| `llm-self` | B | 1 | 6 | 1 | 免费 |
| `platform-link` | B | 1 | 2(+2新) | 1 | 免费 |
| `ai-agent` | P | 5 | 9 | 5 | 2980 |
| `ai-sales` | P | 17 | 31 | 5 | 3980 |
| `llm-cloud` | P | 0 | 4(+2复用) | 2 | 计量 |
| `reach-auto` | P | 5 | 12 | 2 | 1980 |
| `reach-sms` | P | 4 | 7 | 3 | 980 |
| `reach-email` | P | 7 | 7 | 3 | 980 |
| `reach-social` | P | 10 | 15 | 3 | 1980 |
| `reach-link` | P | 3 | 6 | 1 | 380 |
| `im-wecom` | P | 1 | 9 | 3 | 2980 |
| `im-intl` | P | 10 | 25 | 3 | 1980 |
| `analytics` | P | 6 | 9 | 3 | 980 |
| `experiment` | P | 2 | 7 | 3 | 1280 |
| `browser-automation` | P | 5 | 3 | 2 | 1680 |
| `geo` | P | 30 | 20 | 2 | 2980 |
| `sales-crm` | P | 4 | 13 | 3 | 1980 |
| `approval` | P | 2 | 6 | 1 | 680 |
| `i18n` | P | 2 | 2 | 1 | 480 |

> 合计菜单节点覆盖 **208/208**（含目录节点），无遗漏、无重叠归属。

---

## 二之二、数据表归属总表（任务一交付：模块 × 页面 × 数据表）

页面归属见 `internal/model/module_bindings.go`（`ModuleMenuBindings`）；
**数据表归属**见 `internal/model/module_tables.go`（`moduleTableRegistry`）。

两张表都不是文档清单，而是**可执行的数据结构**，各有测试钉死：

| 维度 | 事实源 | 门禁 | 实测覆盖 |
| --- | --- | --- | --- |
| 菜单页面 | `ModuleMenuBindings` | `TestModuleMenuBindingsCoverAllMenuKeys` | 212/212 节点，无重复归属 |
| 数据表 | `moduleTableRegistry` | `TestEveryModelTableHasModuleOwner` | 270/270 表，无孤儿、无重复归属 |

> **为什么表归属必须可校验**：文档里少写几行表名不致命，
> **表无主才致命** —— 一张无主的表意味着「没人知道它随哪个模块开关」，
> 关掉某模块后它可能仍被定时任务写入，数据继续增长，即"关了模块不省钱也不干净"。
> 首次实测时 270 张表里有 **32 张无主**（含 `wechat_accounts` / `webhook_events` /
> `notifications` / `bridge_accounts` 等真实业务表），现已全部归属。

各模块独占表数量（实测 304 条登记，含部分表跨模块共享语义已去重）：

| 模块 | 表数 | 模块 | 表数 |
| --- | --- | --- | --- |
| `core`（基础） | 69 | `reach-link` | 10 |
| `ai-sales` | 33 | `sales-crm` | 10 |
| `im-intl` | 22 | `asset`（基础） | 10 |
| `knowledge`（基础） | 22 | `reach-auto` | 10 |
| `geo` | 20 | `analytics` | 7 |
| `customer`（基础） | 18 | `im-wecom` | 7 |
| `inbox`（基础） | 17 | `reach-sms` / `browser-automation` | 6 / 6 |
| `reach-social` | 12 | `reach-email` / `experiment` | 6 / 5 |
| | | `ai-agent` / `approval` | 5 / 4 |
| | | `llm-cloud` / `i18n` | 3 / 2 |

**空表模块的处置**：`llm-cloud` 与 `browser-automation` 在首版实现里确实没有专属表
（前者只有镜像、后者只有会话与任务执行记录）。归属仍然先立住（`llm-cloud` 登记
`llm_wallets` / `llm_wallet_txns` / `platform_llm_usage_records`；`browser-automation`
登记浏览器配置与任务表），后续补表时往登记表加即可，无需重新论证归属。
门禁 `TestPaidModuleHasTables` 会持续盯住"收费模块却没有任何表"这一形态。

---

## 三、基础功能模块的完备性论证

### 3.1 「基础功能保证用户使用」的判据

一个模块群要被称为"基础"，必须满足：**关掉它，用户无法完成下面任何一条最小任务**。

| 最小任务 | 依赖模块 | 能不能绕过 |
| --- | --- | --- |
| ① 部署并登录进系统 | `core` `platform-link` | ❌ 不能 |
| ② 有一个地方录入/接收客户 | `customer` `inbox` | ❌ 不能 |
| ③ 有人能回复客户消息 | `inbox`（坐席/快捷回复/工单） | ❌ 不能 |
| ④ 给 AI 准备知识素材 | `knowledge` `asset` | ❌ 不能 |
| ⑤ 接入自己的大模型 | `llm-self` | ❌ 不能 |
| ⑥ 装完就能跑（不开通任何付费模块） | 全部 B 模块 | ❌ 不能 |

### 3.2 一期不开通任何付费模块，用户能得到什么

> 这是本方案最重要的产品论证，必须能一句话说清。

**用户拿到的是一套完整可用的「私域客服 + 客户资产 + 知识库 + 自带 AI」系统**：
- 客户从线索到 360 画像、标签、RFM 分层
- 微信/网页/邮件进来的消息统一收件箱处理（企微通道本身收费 → 见下）
- 知识库导入、分段、检索、API Token 对外提供
- 自己的 API Key 接入 LLM，看得到 token 与成本明细
- 资产市场可下载官方免费资产包

**用户缺的是「产能」**：AI 自动应答、多轮触达、多渠道扩张、深度分析、CRM 闭环、LLM 垫资。

### 3.3 ⚠️ 诚实标注的一处张力：`inbox` 与 `im-wecom` / `im-intl`

**问题**：消息**进来**靠渠道（企微/WhatsApp…），这些渠道全部定价了。免费用户拿什么接消息？

**结论与处置**（三条同时成立才算完备）：

| 入口 | 是否收费 | 说明 |
| --- | --- | --- |
| **客服 Web Widget**（`chatChannelList`） | **不收费** | 商户在自己网站挂浮窗，消息进 `customer_sessions`。这是**零渠道成本**的入口 |
| **邮箱线索**（`customer.clueList` 支持批量导入） | **不收费** | 手工/表格导入 |
| **人工回复**（`inbox.quickReply` `agentStatus` `sessionTag`） | **不收费** | 坐席工作台完整可用 |
| 企微 / WhatsApp / TG / QQ / 飞书 / 钉钉 | 收费 | 平台承担该渠道的适配、稳定性与风控成本 |

> **产品结论**：免费用户是「**网站客服 + 表格线索 + 人工坐席**」的完整闭环，收费后解锁「**私域 IM 通道 + AI 自动产能**」。这条边界是本次商业化最关键的定价决策，写进 spec 不可改。

### 3.4 与"基座开源"的关系

| 层 | 内容 | 许可 |
| --- | --- | --- |
| 基座开源 | `core` `platform-link` `llm-self` 的**框架能力**（路由/审计/监控/模块下发机制） | AGPL（同现状） |
| 基础功能开源 | `customer` `inbox` `knowledge` `asset` 全部功能 + 全部 24 个模块的**代码** | AGPL（同现状） |
| 收费模块 | 17 个 P 模块的**订阅权**（不是代码） | 平台端定价，代码仍在开源仓 |
| 周边 | 资产包、LLM token 包 | 平台端定价 |

> **关键承诺（必须写进 spec 与平台 UI）**：**所有模块代码都在开源仓，收费的是"授权与配额"，不是"能不能看到/编译/运行代码"**。离线独立部署用户仍拥有全部代码，平台关闭时基础模块全开（见 3.3 + §4.2）。

### 3.5 定价口径（2026-10-30 覆盖声明）

> **显式勘误**：本节为**现行定价口径**，显式**覆盖** 002 时期「收费模块按年订阅、16 个收费模块全量线年度合计 999 元/年」以及 2026-10-21「17 个增值模块全部 ¥1」的旧决策。
> 覆盖依据：2026-10-30 用户裁决「功能模块只有浏览器自动化收费，其余全部免费」。
> **任何按旧口径回滚本文档、官网文案、迁移或目录种子的改动，一律视为错误。**

| 项 | 现行口径（2026-10-30 定价收口） |
| --- | --- |
| 功能模块（24 个中的 **17 个增值**） | **16 个 0 元开通**（`price_cents=0`）：仍需开通，走完整订单+确认链落 `active`；**仅浏览器自动化 ¥1**；**7 个 base 模块仍 0 元免费常开** |
| 浏览器自动化 `browser-automation` | **¥1 买断**（100 分，`BillingCycle=once`，不写 `ExpiresAt`）——由 002 时期的旧建议 1680 元/年 → 003 的 ¥99 买断 → 2026-10-21 的 ¥1；平台端可单独控制开关 |
| 软件订阅 | **¥9**（平台级订阅商品，价格后台可配；`billing_cycle=monthly`，**按 30 天计**，见 ADR-M8）——2026-10-21 用户裁决「只改价格、不改周期」 |
| 体验期 | 新商户注册即 **15 天体验期**（`SUBSCRIPTION_TRIAL_DAYS` 默认 15，体验期等同订阅有效） |
| LLM token | 输入/输出**分列**计量（token 钱包），四档套餐 体验包 ¥1 / 标准包 ¥3 / 专业包 ¥6 / 旗舰包 ¥9；新商户注册另赠 10M 输入 + 2M 输出 |
| 支付 | 易支付（先 `EPAY_MOCK` 后真人验收，ADR-M9）+ 线下人工确认回落，共用 `MarkPaid` 唯一入账口（ADR-M3） |

**FR-5.7 离线口径勘误（两轮覆盖后现行）**：

1. 002 spec 原文「离线：基础模块全开；`price=0` 付费模块自动开；`price>0` 保持关闭」——`price=0` 那半句一度被判"未实现也不再实现"；
2. 002 收口口径收紧为「离线**只开基础模块**，空态仅放行 `model.BaseModuleKeys()`」（§4.5 表中该行即指此旧口径）；
3. **003 再次覆盖为现行口径**：离线（`PLATFORM_ENABLED=false`）启动时由 `offlineGiftModuleRows()` 把 **23 个 0 元模块以 `source=gift` 落 `tenant_modules` 行**（`EnabledKeys` = 24 − 1 = **23**），**`browser-automation` 保持锁定**——本地无支付通道 → 不可购不可用，**不做授权码旁路**（2026-10-19 用户裁决，与云端一致锁定）。存量离线部署升级时在 bootstrap 路径幂等补行（reconcile）。

---

## 四、耦合关系论证

### 4.1 依赖图（`module_dependencies` 有向边，`A → B` = A 依赖 B）

```text
core ◄── customer ◄── inbox
  ▲          ▲          ▲
  │          │          │
  ├── knowledge ────────┤
  │      ▲             │
  │      │             │
  │     geo             │
  │                     │
  ├── asset            │
  │                     │
  ├── llm-self ◄── llm-cloud
  │                     │
  └── platform-link ────┘
                        ▲
   ai-agent ◄──────────┘
      ▲
   ai-sales ──────► sales-crm
      ▲
   experiment ◄── analytics
      ▲
  reach-sms / reach-email / reach-social / im-wecom / im-intl / approval / i18n / browser-automation
      ▲
  reach-auto / reach-link
```

### 4.2 依赖闭包规则（开通/关闭时必须遵守）

1. **开通时**：用户勾选 `ai-sales`，系统自动把 `core` `customer` `inbox` `llm-self` `ai-agent` 一并置为 `active`（依赖闭包），其中基础模块不计价、免费模块按标价。
2. **关闭时**：**禁止**直接关闭被依赖的模块——必须先关依赖方。平台端拒绝并返回"仍被哪些模块依赖"。
3. **平台关闭时**（`PLATFORM_ENABLED=false`）：全部 7 个基础模块自动 `active`，17 个付费模块按**离线降级策略**：
   - 有 `price_cents = 0` 的付费模块 → 自动 active（0 元 = 免费开通）
   - `price_cents > 0` 的付费模块 → **保持 inactive 并在 UI 明确标注"需连接平台开通"**，不静默放行（否则收费模块变成免费漏洞）。
4. **新增模块时的种子**：`module_catalog` 新增一行 + `module_dependencies` 补边 + `module_menu_bindings` 补菜单映射 + `tenant_modules` 为所有租户按闭包重算（幂等脚本）。

### 4.3 菜单组织与模块的绑定

**绑定粒度**：**菜单节点（key）** 级，不是顶级分组级。理由：一个顶级分组下常常混着免费与收费节点（`system` 顶级下全是基础；`reach` 顶级下 6 个目录全是收费；`analytics` 顶级下 `dashboardScreen`(P) 与 `confidencePanel`(P2) 混排）。

**绑定表**：`module_menu_bindings(module_key, menu_key)` ——
- 一张表解决"关模块 → 菜单消失"
- 一张表解决"开模块 → 菜单出现"
- **不做 `module_key` 内嵌进 `SystemMenuNode`**：菜单注册表是编译期常量，功能模块是运行期可变数据，两者混在一起会让"离线部署 = 全部基础模块"这条规则无处安放。

**授权计算（用户端 `GET /api/menus`）**：

```text
最终菜单树 = FilterMenuTree(
    SystemMenuTree,
    keys = (角色菜单授权 ∩ 注册表上界)  ∩  (已开通模块覆盖的 menu_key ∪ 基础模块 menu_key)
)
```

`admin` 仍走全树 + **模块闸**（超管也不能调未开通模块的 API——超管权限是"角色权限高"，不是"买了没买"都能用）。

### 4.4 代码耦合清理清单（必须处理，否则模块化只是界面活）

| 耦合点 | 现状 | 必须改为 |
| --- | --- | --- |
| `router.Setup()` 一次性注册全部路由 | 全部模块路由无条件注册 | **模块闸中间件**在鉴权后拦（返回 402/403 + `module_required`），比按模块拆 `Setup` 更可行（拆 Setup 会引入注册顺序与 DI 循环） |
| `model.AllModels()` 全量 AutoMigrate | 全量建表 | **保留全量建表**（表结构属于基座；只关"可用性"不删表，避免升级/降级数据丢失） |
| `cron` 全量注册任务 | 全量启动 | 按模块 `IsEnabled(moduleKey)` 过滤**执行**（注册可保留） |
| `agent/tooluse` 45 个工具全注册 | 全量 | 按模块过滤组装（工具是模块能力的一部分） |
| `websocket` / `event bus` 订阅 | 全量 | 按模块过滤（模块关了不该收到事件） |
| `Store` 前端全局 | 无模块态 | Pinia 新增 `useModuleStore`，登出/切换时刷新 |

### 4.5 实现后实测（2026-10-19 复核）

上表"必须改为"的闸类能力落地后，逐项核验（测试名与文件均经 grep 确认存在）：

| 能力 | 实现落点 | 实现后实测 |
| --- | --- | --- |
| 能力闸（路由）：未开通拦截 4301 | `middleware/module_gate.go` `GuardModulePath`（`router.go` 在 auth 组统一挂载；4301 = `CodeModuleNotEnabled`） | `internal/router/route_policy_gate_test.go` · `internal/middleware/module_gate_test.go` · `internal/service/module_gate_test.go` 全绿 |
| 菜单过滤 | `model.FilterMenuTreeByModule` + `service.ModuleEnabledFunc` 注入 | `model/module_bindings_test.go` `TestFilterMenuTreeByModule` · `service/module_gate_test.go` `TestMenuTreeFilteredByClosedModule` |
| 数据隔离：tenancyidx 回调 fail-closed | `internal/pkg/tenancyidx` `RegisterTenantCallbacks` | `internal/pkg/db/tenant_callback_test.go`（`TestTenantRegistry_*`） |
| 空态口径（**002 旧口径，已被 003 覆盖**）：仅放行 `model.BaseModuleKeys()` | `service/module_catalog.go` `IsEnabled` ①（2026-10-18 作废"空态全放行"）；**003 起离线改为 `offlineGiftModuleRows()` 开 23 个 gift 模块并锁 `browser-automation`，见 §3.5** | `service/module_gate_test.go` `TestModuleGateEmptyBootAllowsOnlyBaseModules` |
| 租户隔离缓存 | 闸缓存按租户分份（`map[uint]*gateTenantState`），不共享开通状态 | `service/module_gate_tenant_test.go` `TestModuleGateTwoTenantCacheIsolation` |
| 激活归属：付费激活绑下单租户 | 模块同步只激活**下单租户**的模块（Fix C） | `service/module_gate_tenant_test.go` `TestModuleSyncActivatesPaidModuleOnOrderingTenant` |

---

## 五、取舍记录（有意"不做"的事）

### 5.1 不做「套餐包」

**理由**：套餐包把 17 个模块压成 1 档，运营上失去颗粒度（用户想买 GEO 但不愿为 AI 销冠付费时，套餐解不了他的问题），且套餐会让依赖闭包规则复杂化。**一期只做单模块 + 依赖自动补齐**。

### 5.2 `im-intl` 合成一档（唯一"故意不细拆"）

**理由**：见 §2 L1 表下注。**保留细拆的接口**：`module_options(module_key, option_key, option_name, price_delta_cents)` 让平台端将来可以对某个子渠道单独加价，不必改数据模型。

### 5.3 不做「按功能点（feature）计费」

**理由**：粒度过细 → 订单碎片化 + 用户看不懂 + 授权判定复杂（`feature_flags` 已经有 251 条能力面）。**模块级是当前代码结构能支撑的最细粒度**。若将来要细拆，用已有的 `feature_flags` 表做模块内的二级开关，模块订阅仍是第一道闸。

### 5.4 不做「运行时动态卸载 Go 模块」

**理由**：Go 静态编译，做不到真热卸载；"热插拔"的真实形态是**能力闸**（路由/菜单/工具/任务按开关放行），不是二进制插拔。`internal/plugin/main/auth_plugin.so` 是 Go plugin 的实验产物，非测试代码零引用，本轮**不作为模块化基础**。

### 5.5 平台关闭时不静默放行付费模块

**理由**：这是商业模式的唯一漏洞。"离线也能用全套功能"会让整套定价失效。处置：`price=0` 的付费模块自动开（这是我们自己定的价），`price>0` 的明确锁住并在 UI 写清"需连接平台开通"。

> **003 更新**：本条即现行离线口径的判据来源——002 一度收紧为「只开基础模块」，003 恢复本条并落成 `offlineGiftModuleRows()`（23 个 0 元 gift 模块开、`browser-automation` 锁），见 §3.5「FR-5.7 离线口径勘误」。

---

## 六、数据表归属总表（新增部分）

| 模块 | 新增表 | 说明 |
| --- | --- | --- |
| 平台端 `platform-link` | `module_catalog` | 模块主表：key/名称/分类(base\|paid)/价格分/状态/图标/描述 |
| 平台端 `platform-link` | `module_dependencies` | 依赖边：module_key → depends_on_key |
| 平台端 `platform-link` | `module_menu_bindings` | 模块 ↔ 菜单 key 映射 |
| 平台端 `platform-link` | `module_options` | 模块子选项（如 `im-intl` 的 whatsapp/telegram/…） |
| 平台端 `platform-link` | `tenant_modules` | 租户已开通模块（含开通来源/时间/到期时间/订阅 id） |
| 平台端 `platform-link` | `tenant_registrations` | 开户登记（公司名/负责人/电话/邮箱/证件/监管责任确认） |
| 平台端 `commerce` | `orders` | 订单主表（模块订阅 + LLM 套餐 + 资产包统一单据） |
| 平台端 `commerce` | `order_items` | 订单行 |
| 平台端 `commerce` | `order_status_logs` | 状态机流转审计 |
| 平台端 `commerce` | `payment_records` | 支付凭证（一期线下人工，二期回调） |
| 平台端 `commerce` | `payment_callbacks` | 异步回调原始报文（幂等键） |
| 平台端 `llm` | `llm_plans` | LLM 套餐（token 数/输入价/输出价/有效期） |
| 平台端 `llm` | `llm_wallets` | 租户 LLM 余额（剩余输入/输出 token 分列） |
| 平台端 `llm` | `llm_wallet_txns` | 钱包流水（充/扣/退） |
| 平台端 `llm` | `llm_usage_records` | 计量明细（每次调用一行，幂等键 trace_id） |
| 用户端 `platform-link` | `tenant_modules` | 本地镜像（平台下发，断网可用） |
| 用户端 `platform-link` | `module_sync_logs` | 下发同步日志 |
| 用户端 `commerce` | `module_orders` | 本地订单镜像（状态与平台一致） |
| 用户端 `llm-cloud` | `llm_wallets` `llm_wallet_txns` | 本地余额镜像（实时扣减的本地视图） |
| 用户端 `llm-cloud` | `llm_usage_records` | 本地计量明细（llm_routing_logs 扩展） |

---

## 七、风险与已知未解

| 风险 | 影响 | 处置 |
| --- | --- | --- |
| 208 个菜单节点手工绑模块，错绑难发现 | 用户反馈"我买了却看不到" | **启动自检 + 测试钉死**：单测断言 `208 个 key 全部有且只有一个 module 归属`，缺绑/重复绑直接红 |
| 关模块后定时任务/工具仍在跑 | 数据继续增长，关了不省钱 | 任务/工具装配处统一走 `modules.IsEnabled` |
| 依赖环 | 开通时死循环 | `module_dependencies` 写入时做拓扑检测，检测到环直接拒绝 |
| 超管绕过模块闸 | 收费模块被免费使用 | 模块闸在鉴权之后、权限判定之前，与角色无关 |
| 离线部署用户投诉"付费模块不能用" | 支持成本 | UI 明示 + 文档明示 + `price=0` 通道 |
| 一期无支付，订单状态可能卡在"待确认" | 运营忘了确认 | 订单列表超期高亮 + 超时提醒（本期只做高亮） |

---

## 八、变更记录

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1.0 | 2026-10-17 | 首版：24 模块（7 基础 + 17 收费）、依赖图、菜单绑定规则、基础能力完备性论证、取舍记录 |
| v1.1 | 2026-10-19 | §4.5 新增「实现后实测」：能力闸/菜单过滤/数据隔离/空态口径/租户隔离缓存/激活归属 六项核验（T808） |
| v1.2 | 2026-10-19 | **003 计费二期定价与离线口径勘误**（T002）：新增 §3.5 覆盖声明——23 模块 0 元 + 浏览器自动化 ¥99 买断（旧建议 1680 元/年 → ¥99，用户裁决）+ 软件订阅 149 元/月仅按月 + 15 天体验期，显式覆盖 002 年度合计式收费旧决策；FR-5.7 离线口径改为「23 个 gift 模块开 + `browser-automation` 锁」（替代「只开 `BaseModuleKeys`」）；§零 结论表收费/支付两行同步 003 |
| v1.3 | 2026-10-21 | **定价改版**（用户裁决）：7 个 base 模块 0 元免费常开 + 17 个 paid 模块 **¥1**（含 `browser-automation` 买断）+ 软件订阅 **¥9**（周期不变，仍按 30 天计）+ LLM 四档套餐 **¥1/¥3/¥6/¥9** + 新商户注册赠送 **10M 输入 + 2M 输出**。§零 结论表与 §3.5 定价表同步；平台端种子（`033` / 新增 `035` 迁移）与代码常量同步 |
| v1.4 | 2026-10-30 | **定价收口**（用户裁决）：功能模块仅 `browser-automation` 收费 **¥1 买断**，其余 16 个 paid 模块 `price_cents=0`（0 元开通）；7 个 base 模块仍 0 元免费常开，软件订阅 ¥9、LLM 四档套餐不变。同步平台迁移 `036`、源码默认价、官网文案；并修复模块关闭时菜单目录只剩空壳（有分组标题、无子菜单）的问题 |

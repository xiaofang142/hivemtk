# 硬编码点位全量清单（HARDCODE_INVENTORY）

> 生成方式：`scripts/hardcode_sweep.py`（Go 侧）+ `scripts/hardcode_sweep_web.py`（user-web 侧）
> → `scripts/hardcode_classify.py`（Go CONFIG 归类）+ `scripts/hardcode_inventory.py`（合并出清单）
> 机读全量：`hardcode-sweep/INVENTORY.csv`（7725 行，每行一个点位，带 `file:line` 与建议 `group/key`）
> 本文只承载**口径、汇总、争议点位和施工指引**，逐行明细看 CSV。

---

## 0. 怎么复现这份清单

```bash
cd hivemtk
python3 scripts/hardcode_sweep.py           # Go 侧 → hardcode-sweep/hardcode-sweep.json|md
python3 scripts/hardcode_sweep_web.py       # user-web 侧 → hardcode-sweep/hardcode-sweep-web.json|md
python3 scripts/hardcode_classify.py        # Go CONFIG 五分归类 → hardcode-sweep-classified.json
python3 scripts/hardcode_inventory.py       # 合并 + 指路 → INVENTORY.csv
python3 scripts/check-config-param-readpoints.py   # 已有接线 vs 僵尸参数
```

四个脚本全是只读，不改生产代码、不动数据库。

---

## 1. 总览

扫描面：`user-server/internal` + `user-server/cmd` 共 **1814** 个 Go 文件（非测试、非种子）；
`user-web/src` 共 **479** 个文件。

| # | 桶 | 原始量 | 去重后 | 治理结论 |
| --- | --- | --- | --- | --- |
| A | Go `CONFIG` → **TUNABLE** | 403 行 | **346** 个 `group/key` | ✅ 应进 `config_params`（含 58 条 P0） |
| B | Go `CONFIG` → PROTOCOL | 20 | 20 | ❌ 不迁（错误码/协议版本，动了就破坏兼容） |
| C | Go `CONFIG` → DEPLOY | 13 | 13 | ❌ 不迁（端口/连接池/DSN，属部署期 env） |
| D | Go `CONFIG` → INFRA | 8 | 8 | ⚠️ 半迁（向量维度/HNSW 需重建索引，须走变更单） |
| E | Go `CONFIG` → FIELD_LIT | 460 | — | ❌ 假阳性：是业务字段赋值（`FromCache=true`、`InboxCreated=true`），不是阈值 |
| F | Go `DATA` → DICTIONARY | 44 | 44 | ⚠️ 另立「字典表」模型，不进 `config_params` |
| G | Go `CN` | 12495 | — | 见 §4，走 i18n 不走配置 |
| H | Go `INLINE_NUM` | 1430 | — | ❌ 不治理（噪声为主，见 §6） |
| I | web `CONFIG` → TUNABLE | 65 行 | 15 真阈值 + 50 需人工筛 | ✅ 真阈值部分应进 `config_params` |
| J | web `CN` | 7237（用户可见 3793） | — | 见 §4 |

**可行动口径**：Go 侧 TUNABLE 去重 **346** 个点位 + 前端真阈值 **15** 个 ≈ **361 个**点位应进参数中心。
其中 **P0 = 58**、**P1 = 130**、**P2 = 259**（详见 CSV 的 `priority` 列）。

CSV 有意**不去噪**：宁可多列让人筛，也不让扫描器替人做判断。按人工复核的经验，
`frontend_ui` 那 50 行里约 30 行是 `i = 0` / `hash = 0` 这类循环下标假阳性，
所以上面表里我给了「15 真阈值」这个保守数。

---

## 2. A 桶：应进 `config_params` 的 346 个点位

完整表见 CSV。按建议 group 的分布：

| group | 唯一点位 | group | 唯一点位 |
| --- | --- | --- | --- |
| `misc` | 74 | `bridge` | 10 |
| `knowledge` | 33 | `channelgw` | 8 |
| `frontend_ui` | 50（≈15 真） | `wecom` | 7 |
| `agent_llm` | 20（`alignment_stage` 五维评分是同一份映射表，实际 6 个） | `pagination` | 6 |
| `workflow` | 16（`ALLOW_INSECURE_WEBHOOK` 14 行是同一个开关） | `reach` | 6 |
| `confidence` | 27 | `sales` | 4 |
| `telemetry` | 22 | `middleware` | 3 |
| `inbox_sales` | 16 | `frontend_ws` | 11（两个 socket 工具各有同名项） |
| `human_task` | 14 | | |
| `agent_tool` | 10 | | |

### 2.1 P0（58 个）：安全、上传、并发上限、TTL 到期

P0 判据 = 「改了以后**一定**有人想改，而且改错会出事」。分四类：

**① 上传大小上限 —— 三份互相矛盾的副本（本次扫描发现的最硬实证）**

| 位置 | 值 | 性质 |
| --- | --- | --- |
| `internal/controller/upload.go:22` | `MaxUploadSize = 10 * 1024 * 1024` | 编译期常量，**实际生效的就是它** |
| `internal/model/system_config.go:21` + `internal/service/system_config.go:40,87` | `MaxUploadSizeMB = 50` | 运维在「系统配置」页可改，`gorm:"default:50"` |
| `user-web/src/views/system/MaterialLibrary.vue:323` | `maxSize = 10` | 前端又写死一份 10 |

已核实：`MaxUploadSizeMB` 全仓只有 model / service 保存时校验 + controller 空 body 判断这 4 处读它，
**上传链路一处都不读**。也就是说运维把「上传大小上限」改成 200MB，页面显示保存成功，
实际上传永远被 10MB 拦死。这不是「没登记」，是**已登记、已暴露 UI、已落库，就是没人读**——比僵尸参数更隐蔽。

> **✅ 已于 2026-10-10 修复（阶段 0.5，commit 见 `git log --grep=MaxUploadSizeMB`）**：
> 上传链路改成 **env `UPLOAD_MAX_SIZE` > 库里的 `system_config.max_upload_size_mb` > 10MB 代码兜底**。
> 配套：`SystemConfigService.ResolveUploadMaxBytes` 只认"行真实存在且 > 0"的值（不走 `GetConfig`，
> 否则读失败时它的 `defaultConfig()` 会拿 50MB 顶上，等于把存量站点从 10MB 悄悄放宽 5 倍）；
> `systemConfigRepo.GetConfig` 加 nil 句柄哨兵错，避免上传链路因装配问题崩进程。
> **仍未收敛**：前端 `MaterialLibrary.vue:323 maxSize = 10` 第三份副本 → 阶段 3.3；
> JSON body 上限 `middleware/body_limit.go:30` → 阶段 2a。

同类：JSON body 上限 `middleware/body_limit.go:30 DefaultMaxJSONBodyMB=8`。

**② Webhook 验签降级开关（14 处重复）**
`ALLOW_INSECURE_WEBHOOK = true` 出现在
`internal/service/webhook.go`（10 处）、`webhook_channel_douyin.go:217`、
`internal/service/dingtalk_app.go:489,492,520,523`、
`internal/content/service/marketing_flow_action.go:713`（`MARKETING_WEBHOOK_ALLOW_INSECURE`）、
`internal/channelbot/telegram/telegram.go:464`（`ALLOW_INSECURE_TELEGRAM_WEBHOOK`）。
**默认 true = 默认关闭验签**。这是安全默认值反了的典型，必须第一批发 DB 化并默认 `false`。

**③ 分布式锁与幂等 TTL（收件箱/消息中台）**
`InboxLockTTL=24`、`InboxAILockTTL=15`、`InboxPendingTTL=5`、`IngestLockTTL=25`、
`InboxContentDedupTTL=5`、`InboxAIProcessingTTL=2`、`MessageHubDefaultIdemTTL=24`
（全部在 `internal/service/inbox_ingress.go` / `message_hub.go`）；
`HumanLockDefaultTTL=24`、`HumanLockReasonTTL=24`、`LockExpiryCheckInterval=time.Minute`
（`human_escalation.go`）；`PollingLockStaleThreshold=60`（`repository/telegram_polling_lock.go`）。
锁 TTL 写错 = 死锁或重复处理，不是「调优」而是「正确性」。

**④ 渠道与配额硬上限**
`QQMessageMaxLen=2000`、`TGMessageMaxLength=4096`、`TGInlineRowsMax=100`、`TGInlineButtonsPerRowMax=8`、
`SSEMaxConnPerIP=5`、`TierMinRatePerMin=12.0`、`TierQuotaMarketing=1`、`TierQuotaUtility=4`、
`WeComErrorRateDegradeThreshold=0.3`、`WeComQuotaDegradeThreshold=0.9`、`DefaultMaxJSONBodyMB=8`、
`MaxUploadSize=10`、`SSOCookieTTL=5`、`DefaultVisitorTokenTTL=7`、`MaxRetry=3`（短信）、
`ConsecutiveFailureLimit=3`、`L1TTLHours=24`、`MaxTokens=500/800/2048`、`EditLockTTL=5`、
`DefaultAudienceLimit=200`、`MaxAudienceLimit=500`、`CircuitBreakerShadow=false`、
`LTCConfigCacheTTL=60`、`TranslationCacheTTLDefault=1`、`WebHook*` 限流四项。

### 2.2 同名不同值：12 组自相矛盾的配置

CSV 里同一 `group/key` 出现多个值，说明**同一个概念在不同地方各写各的**。这是硬编码最典型的病征：

| group/key | 出现过的值 | 说明 |
| --- | --- | --- |
| `misc/misc_max_tokens` | `500` / `800` / `2048` | 三个调用点各写各的（`session_assignment.go:242`、`controller/ai_agent.go:205`、`ai_agent_domain.go:105`） |
| `frontend_ws/max_reconnect_attempts_default` | `10` / `50` | `agentSocket.js` 10 次，`chatSocket.js` 50 次 |
| `frontend_ws/initial_reconnect_delay_ms` | `1000` / `2000` | 同上 |
| `knowledge/top_k` | `5` / `50` | 工作台 5、商户 playground 50 |
| `misc/approval_shadow` | `true` / `false` | `approval_wiring.go:243 false` vs `:261 true` —— **同文件内自相矛盾** |
| `agent_llm/{empathy,enthusiasm,patience,clarity,politeness}` | `3`/`4`/`5` | 5 档评分映射表（alignment_stage.go），是 1 个表不是 5 个参数 |
| `knowledge/similarity_threshold` | `0` / `1` | `knowledge_merchant_playground.go:69,72`，真假阳性 |
| `sales/collection` | `true` / `false` | 真假阳性（`ltc_config.go:83,856` 是字段赋值） |
| `frontend_ui/i` | `0`/`1`/`6`/`29` | 循环下标，真假阳性 |

治理动作：`max_tokens`、`reconnect` 系列应**合并成 1 个键**；
alignment 五维应合并成 1 张 `alignment_dimension_weight` 字典项，不拆成 5 个标量。

### 2.3 前端阈值（15 个真点位）

| 常量 | 值 | 位置 | 建议 group/key |
| --- | --- | --- | --- |
| `MAX_RECONNECT_ATTEMPTS_DEFAULT` | `10` / `50` | `user-web/src/utils/agentSocket.js:4`、`chatSocket.js:6` | `frontend_ws/max_reconnect_attempts` |
| `INITIAL_RECONNECT_DELAY_MS` | `2000` / `1000` | 同上 `:5` / `:3` | `frontend_ws/initial_reconnect_delay_ms` |
| `MAX_RECONNECT_DELAY_MS` | `30000` | 同上 `:6` / `:2` | `frontend_ws/max_reconnect_delay_ms` |
| `PING_INTERVAL_MS` | `25000` | 同上 `:7` / `:4` | `frontend_ws/ping_interval_ms` |
| `ACK_BATCH_INTERVAL_MS` | `200` | 同上 `:8` / `:5` | `frontend_ws/ack_batch_interval_ms` |
| `MAX_HISTORY` / `MAX_QUEUE` / `FLUSH_INTERVAL_MS` | `50` / `100` / `30` | `utils/journeyTracker.js:5-7` | `frontend_ws/journey_*` |
| `COUNTS_POLL_MS` / `STATS_POLL_MS` / `REFRESH_MS` | `30000` | `views/approvalTask/List.vue:368`、`views/badCase/List.vue:548`、`views/messageHub/Dashboard.vue:112` | `frontend_ui/poll_interval_ms` |
| `POLL_FAILURE_LIMIT` | `3` | `views/browserAutomation/Monitor.vue:284` | `frontend_ui/poll_failure_limit` |
| `DEEP_LINK_SCAN_PAGE_SIZE` | `500` | `views/customerSession/List.vue:452` | `pagination/page_max_size` |
| `WINDOW_HOURS` | `1` | `views/messageHub/Dashboard.vue:111` | `telemetry/dashboard_window_hours` |
| `maxSize` | `10` | `views/system/MaterialLibrary.vue:323` | 归 `misc/upload_max_size_mb`（与 §2.1① 同一个键） |

前端自己发明的默认分页/时间窗另有 **1752 处**（`TUNE` 桶），集中在 `src/api/*.js`
（`limit=20/50`、`days=7/30`）与 `views/**`（`pageSize||20`）。这些**不是常量声明**，
扫描器只能给出候选。它们绕过了已有的 `pagination` group 参数，属于"两边各有一个真相"，
建议在接线 `pagination` 组时**顺手把前端这批默认值改为从后端参数取**（见方案 §5 阶段三）。

---

## 3. F 桶：44 个业务字典（不进 `config_params`）

`config_params` 是**标量 KV**，塞不进「有序阶段 + 多语言标签 + 状态机」这种结构。
这 44 项需要独立的**字典表模型**，硬塞进参数中心会把 `param_value` 变成第二份 JSON 泥潭：

- 状态机类（21）：`ApprovalStatuses`、`BadCaseStatuses`、`BillStatuses`、`HumanTaskStatuses`/
  `Open`/`Terminal`、`KBChangeStatuses`、`OpportunityStages/Statuses/Outcomes`、
  `OrderDraftTerminalStatuses`、`PaymentStatuses`、`QuoteStatuses`、`SystemRoleList`
- 分类标签类（13）：`BadCaseSources/Labels/FixLayers`、`HumanTaskKinds/Actions`、
  `KBChangeOps/AuditActions`、`KnownRiskLevels`、`AllKeyIntents`、`AgentStageNames`、
  `AllHumanizeDimensions`、`AllPersonaDimensions`、`AllSalesChampionDimensions`、`AllStages`
- 流程步骤类（4）：`DefaultPipelineSteps`、`DefaultSendPipelineSteps`、`LTCKnownStages`、
  `LTCKnownThresholdKeys`
- 意图/提示词类（4）：`DefaultIntents`、`SystemPromptTemplates`、`DefaultLongtailTemplates`、
  `IntentEnabled`
- 其它（2）：`HumanTaskSLAColumns`（真·DB 列名，属 MODEL 不属配置，**不迁**）

前端对应物在 `user-web/src/constants/*.js`（24 个字典文件，由 `constants/index.js` 双导出统一）。
**前后端各有一套状态字典，这是最容易出线上不一致的地方**，值得单独立项（见方案 §5 阶段四）。

---

## 4. C/G 桶：中文文案不是"配置"

12495（Go）+ 7237（web）条中文字符串，二次切分后：

| 子桶 | Go | web | 归属 |
| --- | --- | --- | --- |
| `LOG`（日志/注释，`logger.*`、注释行） | 3094 | — | ❌ 不治理（日志是自己看的） |
| `TOOL_SEED_COPY`（`cmd/` 工具种子文案） | 172 | — | ❌ 不治理（离线工具的素材） |
| `API_MESSAGE`（经 API 返回给用户的中文） | 9013 | 7205 | ✅ 走 i18n 词条 |
| `AI_OUTPUT_REPLY`（AI 回复模板/建议回复） | 212 | 32 | ✅ 走 i18n + 可选 DB 化 |
| `SQL_DEBUG` | 4 | — | ❌ |

**重要区分**：文案缺口是 **i18n 词条**问题，不是"数据库配置"问题。
前端已有完整 vue-i18n（`src/i18n/`，9 份语言 812 KB，约 3095 处 `t()`），
后端已有 `internal/pkg/i18n/`（仅覆盖 265 条中文）。机制都在，缺的是把
`API_MESSAGE` 那一万条搬进词条表。**不要把文案塞进 `config_params`**——
那会让参数中心同时承担数值阈值和多语言文案两种完全不同的生命周期。

---

## 5. 不迁项（写明理由，避免下轮又被当成漏项）

- **B 桶 PROTOCOL（20）**：`domain/errors/codes.go` 12 个错误码、`channelgw` 协议版本 v1/v2、
  `traceparent` 的 TraceID/SpanID 十六进制长度、RFM 的 `RDays1=7`。
  这些是**对外契约**，改一个字就是破坏兼容。归 `PROTOCOL`，永不进配置。
- **C 桶 DEPLOY（13）**：`config/ports.go` 6 个默认端口、`pkg/db/db.go` 的 `MaxOpenConns=20`、
  `pkg/mail` 的 SMTP 端口/SSL、`testdb` 的 `POSTGRES_TEST_PORT`。这些属于**部署期 env**，
  改要走 `check-env-coverage.py` 那条线，不是 `config_params`。
- **D 桶 INFRA（8）**：`BGEM3DefaultDimension=1024`、`SourceDim=16384`、`HNSWM=16`、
  `HNSWEfConstruction=200`、`HNSWEfSearch=100`。**能配，但改了要重建向量索引**。
  建议做成"可配 + 变更需审批 + 写审计"，`Restart=true`。
- **E 桶 FIELD_LIT（460）**：`InboxCreated = true`、`FromCache = true`、`OK = true`、
  `Processed = true`、`Delivered = 0`。扫描器按 `Ident = 布尔/数字` 抓到的一切赋值，
  其中绝大多数是**业务数据字段的字面量**，不是配置常量。

---

## 6. H 桶 INLINE_NUM（1430）：本轮结论是"不治理"

抽样归类：`os.Exit(1)`、`blockedCount.Add(1)`、`atomic.Store(0)`、
`Order("id DESC").Limit(3)` —— **1430 条里只有 146 条形似阈值，其中大部分还是计数器初值**。
把 `blockedCount.Add(1)` 抽成配置既无意义也无害处可言。
**本轮不为 INLINE_NUM 立工单**；如后续真有"某个 Limit 写死"的 bug，走单点修复，不做批量治理。

---

## 7. 72 条僵尸参数：已登记但没人读（横切本清单的第一优先）

`scripts/check-config-param-readpoints.py` 实测：**115 条登记 / 43 条真接线 / 72 条僵尸（63%）**。

这 72 条**不是"没做"，是"做了一半"**：参数已在库、已在「参数中心」页面渲染成可编辑控件、
运维改完看到「保存成功」——但生产代码从来不读它。这是对运维的**主动误导**，比没做更糟。

按 group 分布（`group / key / 名称`）：

| group | 僵尸 key |
| --- | --- |
| `misc`(16) | `visitor_token_ttl`、`sso_cookie_ttl`、`polling_lock_stale_threshold`、`domain_check_concurrency`、`sms_max_retry`、`csv_export_max_rows`、`backup_page_size`、`text_truncate_max_bytes`、`preview_max_len`、`sop_scheduler_interval`、`sop_max_wait`、`deepl_timeout`、`reply_sem_timeout`、`ownership_cache_ttl`、`summary_stale_threshold`、`agentloop_history_max_candidates` |
| `confidence`(7) | `humanize_default_threshold`、`persona_default_threshold`、`persona_max_retry`、`intent_fewshot_min_cos`、`weak_truth_min_confidence`、`emb_retry_cooldown`、`veto_low_rag_threshold` |
| `agent_llm`(7) | `vote_agreement_threshold`、`default_health_check_interval`、`default_circuit_open_duration`、`default_health_check_timeout`、`default_failure_threshold`、`default_http_timeout`、`db_sink_stop_deadline` |
| `cache`(5) | `max_keys`、`faq_decay_max_batch`、`memo_ttl`、`platform_cache_ttl`、`translation_cache_max_entries` |
| `agent_tool`(5) | `max_concurrent`、`max_content_len`、`result_cache_ttl`、`cooldown_duration`、`fail_threshold` |
| `telemetry`(4) | `node_health_window`、`trace_sink_buffer`、`geo_position_window`、`feature_flag_poll_interval` |
| `workflow`(4) | `max_subflow_depth`、`max_workflow_steps`、`max_running_per_sop`、`max_executed_node_trace` |
| `inbox_sales`(4) | `tg_lead_opportunity_threshold`、`unified_miner_lead_threshold`、`preview_sample_limit`、`geo_lead_preview_max_len` |
| `bridge`(3) | `polling_max_timeout`、`polling_default_timeout`、`max_reply_content_bytes` |
| `session`(3) | `active_ttl`、`idle_ttl`、`max_delay_seconds` |
| `pagination`(3) | `page_max_size`、`page_default_size`、`cursor_page_size` |
| `wechat`(3) | `chat_ws_ping_period`、`chat_ws_pong_wait`、`chat_ws_write_wait` |
| `sales`(2) | `insight_limit`、`identity_max_attempts` |
| `middleware`(2) | `audit_flush_interval`、`mfa_recent_verify_ttl` |
| `knowledge`(2) | `max_upload_file_size`、`merchant_knowledge_max_len` |
| `wecom`(1) | `error_rate_degrade` |
| `channelgw`(1) | `ws_push_interval` |

**逐条对上了本清单 A 桶的硬编码**（这就是"接线"的具体施工单）：

| 僵尸参数 | 对应硬编码点位 |
| --- | --- |
| `misc.visitor_token_ttl` | `pkg/security/visitor_token.go:14 DefaultVisitorTokenTTL=7` |
| `misc.sso_cookie_ttl` | `controller/sso.go:20 SSOCookieTTL=5` |
| `misc.polling_lock_stale_threshold` | `repository/telegram_polling_lock.go:20 =60` |
| `misc.sms_max_retry` | `model/sms_tracking.go:70 MaxRetry=3` |
| `misc.csv_export_max_rows` | `ops/service/custom_report_export.go:16 =30000` |
| `misc.text_truncate_max_bytes` | `pkg/textutil/truncate.go:8 DefaultMaxBytes=8192` |
| `agent_tool.fail_threshold` / `cooldown_duration` | `tooluse/tool_router.go:59,62 =5,30` |
| `agent_tool.max_content_len` | `service/message_hub.go:175 MessageHubDefaultMaxContent=64`（同类项） |
| `agent_llm.default_*`（4 条熔断/健康） | `llm/provider_failover.go:32-35` |
| `confidence.humanize_default_threshold` / `persona_default_threshold` / `persona_max_retry` | `service/humanize/service.go:16,28`、`service/persona_evaluator.go:585,587` |
| `cache.translation_cache_max_entries` | `rag/retrieval/translation_cache.go:22 =100000` |
| `bridge.polling_max_timeout` / `polling_default_timeout` | `bridge/handler_http.go:37,38 =500,30` |
| `session.active_ttl` | `repository/customer_session.go:24 =24` |
| `pagination.page_max_size` / `page_default_size` / `cursor_page_size` | `pkg/utils/pagination.go:46,48`、`pkg/pagination/cursor.go:14` |
| `wechat.chat_ws_ping_period` 等 3 条 | 前端 `utils/chatSocket.js` 对应项 |
| `wecom.error_rate_degrade` | `wecom_account_health.go:45 =0.3` |
| `telemetry.feature_flag_poll_interval` | `pkg/featureflag/flag.go:34 =5` |
| `workflow.max_subflow_depth` | `workflow_node_executors.go:14 =5` |

→ **接线动作就是把上表右列的常量替换为 `config.GetInt/GetDuration(...)`，然后把种子里的
「（未接线）」标注撤掉**（门禁第 2 条会检查标注是否过期）。
这一步零新增表、零新增 API、零前端改动，是投入产出比最高的一批。

---

## 8. 已知遗留问题

1. ~~`internal/service/config_param_test.go` 的 `const defaultParamDefsWant = 114`
   与 `check-config-param-readpoints.py` 实测的 115 漂移 1 条。~~ 
   **已于本轮修复**：实测确认种子已加 `cache.faq_answer_enabled`（第 115 条，
   读点在 `app/faq_cache_wiring.go`）但锚点漏改，`TestDefaultParamDefsCount` 一进仓库就红；
   已把锚点补到 115 并在注释里记下这条的来历，`go test -run TestDefaultParamDefs` 现为绿。
2. `config_params` 表实际索引是 `UNIQUE(key)` 单列，而 gorm tag 写的是
   `uniqueIndex:idx_group_key`（group+key 复合）。**key 必须全局唯一**，
   两条不同 group 同名 key 会在 seed 时撞唯一索引并被 `logger.Warnf` 静默丢弃。
   本清单 §2.2 的「同名不同值」和方案里的 key 命名规则都依赖这个事实。
3. `Service.validateValue` 的 `duration` 分支在 `time.ParseDuration` 失败时**直接 `return nil`**，
   跳过 min/max 校验。接线量上来之后这是个可被利用的绕过口。
4. `internal/repository/system_config_kv.go` 的 `NewSystemConfigKVRepository()`
   捕获调用那一刻的 `db.GetDB()`，为 nil 时 `Get` 会 **panic 而非返回 error**。

以上 4 项属**实施阶段的前置修复**，详见 `docs/CONFIG_DB_DRIVEN_PLAN.md` §4 阶段 0。
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
| `workflow` | 16（原记「`ALLOW_INSECURE_WEBHOOK` 14 行是同一个开关」，实测是 3 个读取点、其余为文案提及，见 §2.1②） | `reach` | 6 |
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
| `internal/controller/upload.go:24` | `MaxUploadSize = 10 * 1024 * 1024` | 编译期常量，**实际生效的就是它** |
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
> **✅ 三份副本已全部收敛到数据库（阶段 0.5 → 阶段 2a）**。最终优先级链：
> **env `UPLOAD_MAX_SIZE` > `misc.upload_max_size_mb`（参数中心，2a）>
> `system_config.max_upload_size_mb`（兼容列，0.5）> 10MB 代码兜底**。
> 关键取舍：`misc.upload_max_size_mb` **默认值取 0**，语义是「不覆盖，沿用下游」——
> 填 10 会把运维已在系统配置页调好的 200MB 又压回 10MB（把 0.5 刚修好的 bug 原样退回），
> 填 50 则让新装站点从 10MB 静默放宽 5 倍；取 0 让存量与新装站点行为与上线前逐字节一致。
> 该键每请求读，改完即时生效。
> **仍未收敛**：前端 `MaterialLibrary.vue:323 maxSize = 10` 第三份副本 → 阶段 3.3。
> **⚠️ 别混淆**：`knowledge_base_import.go:35 MaxUploadFileSize = 50<<20` 是**另一条**
> 知识库文档上传上限，已在阶段 1.8 作为 `knowledge.max_upload_file_size` 单独入库，与本节不是同一处。

同类：JSON body 上限 `middleware/body_limit.go:30 DefaultMaxJSONBodyMB=8`。

> **✅ 已于 2026-10-10 入库（阶段 2a）**：`middleware.max_json_body_mb`，默认 8、`Min 1`、`Restart true`。
> 三个决策：① 默认必须保持 8 —— `TestGlobalDefaultDoesNotTightenExistingCaps` 钉死
> 全局默认值 ≥ webhook 可调顶格 4MB，调小会把运维已放行的高段请求截断；
> ② **有意不给 0** —— env 侧 `<=0` 的「不限制」是部署层排障应急开关，参数中心是运维日常调的面，
> 再开一个关掉全局 body 封顶的口子风险大于收益，Description 里指明"临时放开请用 env 设 0"；
> ③ `Restart: true` —— 这是 `ParamDef.Restart` **首次被真正用上**（此前 115 条种子一条都没填），
> 因为 `router.Setup` 里 `r.Use(middleware.BodyLimit(...))` 是装配期读一次，不重启不生效。
> 另外 `maxMultipartMemoryMB` 保持独立不联动（它是把 gin 默认 32MB 主动调小的内存优化，
> 跟随 body 上限会顺带把每个并发上传的内存放大）。

**② Webhook 验签降级开关 —— ✅ 已处置（2026-10-10，阶段 2b），结论是「参数化不是答案」**

本清单原记录「`ALLOW_INSECURE_WEBHOOK` 14 处重复、默认 true」，**该描述经实测为假**，如实更正：

- 只有 3 个真实读取点，其余 11 处是错误文案/日志里提到变量名。
- 三处**本来就 fail-closed**（只认字面量 `"true"`，且仅限该账号压根没配密钥时才可能豁免），不存在"默认 true"。
- 诊断从"这是安全默认值写反了"修正为"三把开关的护栏强度不一致"，**真正的问题是强度不是默认值**。

| 变量 | 读取点 | 原护栏 |
| --- | --- | --- |
| `ALLOW_INSECURE_WEBHOOK` | `internal/service/webhook.go:509` | 启动护栏：非开发环境 `log.Fatalf` |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | `internal/channelbot/telegram/telegram.go:463` | **无任何环境护栏**（唯一缺陷） |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | `internal/content/service/marketing_flow_action.go:703` | 运行时判 `config.IsDevelopmentEnv()` |

顺带查出**第三个"似的缺陷"**：`internal/channelbot/whatsapp/whatsapp.go` 的 `VerifyWebhook` 在空 secret 时
直接返回 true（fail-open），无 env 开关、无护栏、无 warn 痕迹，注释还写着"与项目既有行为一致"。
它**不在硬编码盘点范围内**（不是字面量，是控制流写错），但性质更严重，已在本批一并改为 fail-closed。

**为什么最终不做成 DB 参数**（详见 `CONFIG_DB_DRIVEN_PLAN.md` §阶段二 2b）：做成数据库可改等于让
admin API 的写操作能在运行时翻转签名校验 —— 相比 env + 启动 `log.Fatalf` 是**安全回退**
（DB 改完即时生效、无重启、无启动拦截）。且 DB 镜像 env 状态必然造假值（ReadOnly 与"服务端自写"
自相矛盾，或静态 false 纯误导）。**判定标准：能不能让运行时的一个写操作翻掉安全闸** ——
凡能的一律留 env + 启动护栏，此为"全面数据库驱动"的受控例外。

实际落地 = 补齐 TG 开关的 dev 环境护栏 + whatsapp fail-open 改 fail-closed + 启动护栏扩到两个变量 +
测试逐格钉住只认字面量 `"true"`（`"TRUE"`/`"1"`/`"yes"`/`"true "` 都不开）。`wired` 计数不变（无参数键）。

**③ 分布式锁与幂等 TTL（收件箱/消息中台）**
`InboxLockTTL=24`、`InboxAILockTTL=15`、`InboxPendingTTL=5`、`IngestLockTTL=25`、
`InboxContentDedupTTL=5`、`InboxAIProcessingTTL=2`、`MessageHubDefaultIdemTTL=24`
（全部在 `internal/service/inbox_ingress.go` / `message_hub.go`）；
`HumanLockDefaultTTL=24`、`HumanLockReasonTTL=24`、`LockExpiryCheckInterval=time.Minute`
（`human_escalation.go`）；`PollingLockStaleThreshold=60`（`repository/telegram_polling_lock.go`）。
锁 TTL 写错 = 死锁或重复处理，不是「调优」而是「正确性」。

**③ 处置结果（2026-10-10，阶段二 2c）：新建 `lock` 组 7 条，13 个点位接得动 6 条**

| 点位 | 处置 |
| --- | --- |
| `InboxLockTTL=24` | ✅ `lock.inbox_human_ttl`。**顺带接上 `inbox_ingress.go:231` 一处裸 `24*time.Hour`**（人类锁 reason 键的 TTL，与锁同一把却走字面量），与锁共用同一参数 |
| `InboxPendingTTL=5` | ✅ `lock.inbox_pending_ttl`（生产调用方 `service/ai_debounce.go:68`） |
| `InboxContentDedupTTL=5` | ✅ `lock.inbox_content_dedup_ttl`（经既有 `contentDedupWindow()`，字段优先、零值回落） |
| `IngestLockTTL=25` | ✅ `lock.ingest_lock_ttl`（生产调用方 `inbox_ingress_persist.go:137`） |
| `InboxAIProcessingTTL=2` | ✅ `lock.ai_processing_ttl`（生产调用方 `:483`/`:585`，这是**真正在跑的防重复回复并发闸**） |
| `MessageHubDefaultIdemTTL=24` | ✅ `lock.message_hub_idem_ttl`，**本组唯一 `Restart=true`**：读取点在账号服务构造期，而账号服务有 6 处构造点全在启动时完成（`qq_account.go:146`、`wecom_integration.go:34`、`feishu.go:115/562/932`、`email.go:55/63`），运行时改值对已启动账号不生效 |
| `InboxAILockTTL=15` | ❌ **刻意不接**。唯一读取点 `inbox_ingress.go:265` 所在的 `tryAcquireAILock` 自带 `//nolint:unused //// 仅被 *_test.go 引用，生产路径未用`，`ReleaseAILock`/`IsSessionAIBusy` 同样无生产调用。保留「未接线」标注并在 Description 写明「真正在跑的并发闸是 `ai_processing_ttl` 那条」 |
| `PollingLockStaleThreshold=60` | ✅ **早在阶段 1.1 已接**（`misc.polling_lock_stale_threshold`），本批不重复 |
| `HumanLockDefaultTTL=24`、`HumanLockReasonTTL=24`、`LockExpiryCheckInterval=1m` | ❌ **不新增种子，只记死代码事实**。这三条常量所在的 `HumanEscalationManager` 在生产链路上死了：`NewHumanEscalationManager` 唯一生产引用为零（只有 `human_escalation_test.go` 19 处调用），`TriggerCensorshipEscalation`/`IsSessionLockedForHuman`/`StartLockExpiryChecker` 全树零生产调用。这是「运行时有同名锁、但实现不在这个文件」的典型——真正在跑的是 `inbox_ingress.go` 的 `InboxHumanLockKey` 系列（`hivemtk:lock:human:`）。给一条死代码建参数只会让人以为改它有用 |

**顺带修掉一个真 bug（与硬编码无关，是写对了才暴露的）**：`internal/cache/memory.go` 的 `PopAll` **不判过期**。`Get`/`Exists`/`LRange`/`scanExpired` 全都判 `item.expiration`，唯独 `PopAll` 只判 `listMode` 和 `len(listItems)`，所以 `LPush(key, v, 60ms)` 写入的列表过了 TTL 仍被原样 Pop 出来。影响面：只在内存缓存（`cache.NewMemoryCache()`，无 Redis 时的回退路径）上显形；Redis 路径由 `EXPIRE` 保证不受影响。对应业务：AI 防抖暂存的入站消息本该随 TTL 过期，内存回退下会被迟到的 Pop 重新投给 AI。修法是补一段与 `Get`/`Exists` 逐字同构的过期判断（`IsZero() && Before(time.Now())`），顺带把过期项从 LRU 摘掉。**这个 bug 是 `TestPendingMessagesExpireByConfiguredTTL` 首跑红暴露的**——不是因为接线写错，是「写对了才看得见底下有坑」。

**④ 渠道与配额硬上限**
`QQMessageMaxLen=2000`、`TGMessageMaxLength=4096`、`TGInlineRowsMax=100`、`TGInlineButtonsPerRowMax=8`、
`SSEMaxConnPerIP=5`、`TierMinRatePerMin=12.0`、`TierQuotaMarketing=1`、`TierQuotaUtility=4`、
`WeComErrorRateDegradeThreshold=0.3`、`WeComQuotaDegradeThreshold=0.9`、`DefaultMaxJSONBodyMB=8`、
`MaxUploadSize=10`、`SSOCookieTTL=5`、`DefaultVisitorTokenTTL=7`、`MaxRetry=3`（短信）、
`ConsecutiveFailureLimit=3`、`L1TTLHours=24`、`MaxTokens=500/800/2048`、`EditLockTTL=5`、
`DefaultAudienceLimit=200`、`MaxAudienceLimit=500`、`CircuitBreakerShadow=false`、
`LTCConfigCacheTTL=60`、`TranslationCacheTTLDefault=1`、`WebHook*` 限流四项。

> **✅ 已于 2026-10-10 部分入库（阶段 2d，接 10 条）**：新建 `channelbot`（5）+ `sse`（3）+ `bridge`（2）
> 三组种子，`wired 110→120`。已接：`QQMessageMaxLen`、`TGMessageMaxLength`、`TGInlineRowsMax`、
> `TGInlineButtonsPerRowMax`、`DefaultHTTPTimeout`、`SSEHeartbeatInterval`、`SSEMaxConnPerIP`、
> `SSEClientBufferSize`、`SSEMaxBacklogEvents`、`SSEBusBufferSize`。
> 其中 `DefaultMaxJSONBodyMB=8` 已在 2a 单独入库（见 ①），`MaxUploadSize=10` 已随 ① 收敛成代码兜底，
> `SSOCookieTTL=5` / `DefaultVisitorTokenTTL=7` / 短信 `MaxRetry=3` 已在 1.1 接为 `misc.sso_cookie_ttl`
> / `misc.visitor_token_ttl` / `misc.sms_max_retry`。
> **仍未接**：`TierMinRatePerMin`/`TierQuotaMarketing`/`TierQuotaUtility`（WhatsApp 分层配额，
> 属 P1 → 阶段三 3.1）、`WeComErrorRateDegradeThreshold`（见 ③，判定是写死三档，单一阈值表达不了）、
> `ConsecutiveFailureLimit`/`L1TTLHours`/`MaxTokens` 三处同名不同值（见 §2.2）、
> `EditLockTTL`/`DefaultAudienceLimit`/`MaxAudienceLimit`/`CircuitBreakerShadow`/`LTCConfigCacheTTL`/
> `TranslationCacheTTLDefault`/`WebHook*` 四项（P1 → 阶段三）。

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

> **处置进度（2026-10-10）：阶段一全部 8 批 + 阶段二 2a/2b/2c/2d 已接线并提交（12/12）。**
> 门禁读数：`wired 40→120`、`UNDECLARED 72→0`、`已声明未接线 14`。
> 阶段一 8 批（wired 40→102）处理的是本节的僵尸参数；阶段二起处理 §2.1 的 P0 新增点位，
> 每接一条也会让 wired 读数上涨。2a 接的 `misc.upload_max_size_mb` / `middleware.max_json_body_mb`
> 即对应 §2.1①；2c 是新登记的 `lock` 组 7 条（含 1 条刻意不接，故 wired +6 而
> 「已声明未接线」从 13 涨到 14）；2d 是新登记的 `channelbot`/`sse`/`bridge` 三组 10 条，对应 §2.1④。
> 下表是**接线前**的基线快照，各行状态见行末标注。

| group | 僵尸 key |
| --- | --- |
| `misc`(16) ✅已接线 | `visitor_token_ttl`、`sso_cookie_ttl`、`polling_lock_stale_threshold`、`domain_check_concurrency`、`sms_max_retry`、`csv_export_max_rows`、`backup_page_size`、`text_truncate_max_bytes`、`preview_max_len`、`sop_scheduler_interval`、`sop_max_wait`、`deepl_timeout`、`reply_sem_timeout`、`ownership_cache_ttl`、`summary_stale_threshold`、`agentloop_history_max_candidates` |
| `confidence`(7) ⚠️5条已接线 | `humanize_default_threshold`✅、`intent_fewshot_min_cos`✅、`weak_truth_min_confidence`✅、`emb_retry_cooldown`✅、`veto_low_rag_threshold`✅；`persona_default_threshold`/`persona_max_retry` **无处可接**——其实现 `persona_evaluator.go` 已随 `9a5f716c` 整体删除（749 行实现 + 958 行测试），代码里不存在等价能力，保留「未接线」标注 |
| `agent_llm`(7) ✅已接线 | `vote_agreement_threshold`、`default_health_check_interval`、`default_circuit_open_duration`、`default_health_check_timeout`、`default_failure_threshold`、`default_http_timeout`、`db_sink_stop_deadline` |
| `cache`(5) ✅已接线 | `max_keys`、`faq_decay_max_batch`、`memo_ttl`、`platform_cache_ttl`、`translation_cache_max_entries` |
| `agent_tool`(5) ⚠️4条已接线 | `max_concurrent`✅、`max_content_len`✅、`cooldown_duration`✅、`fail_threshold`✅；`result_cache_ttl` **无处可接**——`tooluse/result_cache.go` 的 `NewResultCache` 全仓只有 `p2_test.go` 调用，生产链路从未构造过它，没有构造点就接不上 TTL，保留「未接线」标注 |
| `telemetry`(4) ✅已接线 | `node_health_window`、`trace_sink_buffer`、`geo_position_window`、`feature_flag_poll_interval` |
| `workflow`(4) ✅已接线 | `max_subflow_depth`、`max_workflow_steps`、`max_running_per_sop`、`max_executed_node_trace` |
| `inbox_sales`(4) ✅已接线 | `tg_lead_opportunity_threshold`✅（`service/telegram_lead_miner.go`）、`unified_miner_lead_threshold`✅（`service/lead_miner_unified.go`）、`preview_sample_limit`✅（`service/oneid_merge_preview.go`）、`geo_lead_preview_max_len`✅（`service/trace_learning/insights.go`，seam 本就存在，只补注入） |
| `bridge`(3) ❌无处可接 | `polling_max_timeout`、`polling_default_timeout`、`max_reply_content_bytes` **无处可接**——`handler_http.go` 的两个长轮询常量全树只有 `defaults_test.go` 引用（文件注释自陈「服务端没有长轮询实现」），`bridge_helpers.go` 的 `maxReplyContentBytes` 自带 `//nolint:unused`，生产路径无任何回复截断 |
| `session`(3) ✅已接线 | `active_ttl`✅（`repository/customer_session.go`，service 侧改为直接读同一读取口以保持单一源）、`idle_ttl`✅（`aiagent/mcp/http.go`）、`max_delay_seconds`✅（`content/service/marketing_flow.go handleDelay`；**种子原描述「触达排程超期放弃」与实现不符——实现是「delay 节点等待上限、超过截断」，已一并改写对齐**） |
| `pagination`(3) ⚠️1条已接线 | `cursor_page_size`✅（`pkg/pagination/cursor.go`，经 `utils.ParseCursorParams`→`ClampLimit` 被 operation_log/security_audit/customer 三处生产调用）；`page_max_size`/`page_default_size` **无处可接**——对应的 `ParsePagination`/`ParsePaginationOffset` 全树只有 `pagination_test.go` 调用；`page_default_size` 在 `ParseCursorParams` 的兜底分支又被三个调用方的正数 `defaultLimit` 遮蔽。另注种子写 100、代码 `defaultMaxPageSize` 写 200 本就不一致 |
| `wechat`(3) ❌无处可接 | `chat_ws_ping_period`、`chat_ws_pong_wait`、`chat_ws_write_wait` **无处可接**——常量在 `controller/chat_ws.go` 有读取点，但承载它的 `router/ws.go RegisterWSRoutes` 全树零调用方（`GET /ws/chat` 从未注册，实测落到 SPA NoRoute 兜底）。另：后端无微信渠道 WS 客户端，`internal/channelbot` 只有 core/qq/telegram/whatsapp |
| `sales`(2) ⚠️1条已接线 | `identity_max_attempts`✅（`service/customer_identity.go findExistingWithRetry`）；`insight_limit` **无处可接**——`trace_learning.TopInsights` 的 `limit` 是**入参不是默认值**，全仓唯一调用方是 `insights_test.go`（且三处都硬传 3），生产链路没有任何地方取这个数量 |
| `middleware`(2) ✅已接线 | `audit_flush_interval`✅（`middleware/audit.go processAuditLogs` 的 `time.NewTicker`）、`mfa_recent_verify_ttl`✅（`middleware/mfa.go`，4 个读取点：Redis Set / 近期校验 / 内存表校验 / 后台清理） |
| `knowledge`(2) ✅已接线 | `max_upload_file_size`✅（`knowledge/service/knowledge_base_import.go`，5 个读取点 + `knowledge_service_import.go` 4 个；导出 const 已改为函数）、`merchant_knowledge_max_len`✅（`knowledge/service/knowledge_merchant_external.go splitMarkdownToItems` 的 flush 闭包内，原本是闭包内的局部 const） |
| `wecom`(1) ❌无处可接 | `error_rate_degrade` **无处可接**——`WeComErrorRateDegradeThreshold=0.3` 只出现在常量声明和一句**撒谎的注释**里（「运行时通过 GlobalConfigParam() 读取」），根本没有对应读取函数；现存 `computeHealthScore` 的成功率判定是写死的三档（`<50` 扣 30 / `<80` 扣 15 / `<95` 扣 5），单一阈值表达不了递减分档 |
| `channelgw`(1) ✅已接线 | `ws_push_interval`✅（`channelgw/ws.go runtimeWSPushIntervalDefault`）——**但这条原本是门禁抓不到的假接线**：读取点存在，key 却是 `"ws_push_interval_default"`，种子里登记的是 `"ws_push_interval"`，多一个后缀导致每次读都 miss、每次都回落兜底 2s，参数改了永远不生效。同组另外 4 条 key 都对，是单点笔误。readpoints 门检查的是「种子 key 有没有读取点」，看不出读取点用的是别的 key，本批已改正并补真库行为测试钉住 |

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
| `session.active_ttl` | `repository/customer_session.go:24 =24h` ✅（service 侧 `CustomerSessionActiveTTL` const 保留为兜底值声明，运行时改读 `repository.SessionActiveTTL()`） |
| `session.idle_ttl` | `aiagent/mcp/http.go:17 sessionIdleTTL=30m` ✅ |
| `session.max_delay_seconds` | `content/service/marketing_flow.go:527 函数内 const =300` ✅（**种子原描述与实现不符，已改写**） |
| `pagination.cursor_page_size` | `pkg/pagination/cursor.go:14 CursorPageSize=100` ✅ |
| `pagination.page_max_size` / `page_default_size` | `pkg/utils/pagination.go` 的 `ParsePagination`/`ParsePaginationOffset` —— **零生产调用方**（详见上表行内说明） |
| `inbox_sales.tg_lead_opportunity_threshold` | `service/telegram_lead_miner.go:25 =40` ✅ |
| `inbox_sales.unified_miner_lead_threshold` | `service/lead_miner_unified.go:48 =40` ✅ |
| `inbox_sales.preview_sample_limit` | `service/oneid_merge_preview.go:40 =20` ✅ |
| `inbox_sales.geo_lead_preview_max_len` | `service/trace_learning/insights.go:25 =200` ✅（seam 本就存在，只补注入 + 非正值守卫） |
| `wechat.chat_ws_ping_period` 等 3 条 | `controller/chat_ws.go:51-55` —— 常量有读取点但 `/ws/chat` 路由从未注册（详见上表行内说明）；另前端 `utils/chatSocket.js` 有对应常量属**前端**阈值，归阶段三 |
| `wecom.error_rate_degrade` | `wecom_account_health.go:45 =0.3` ❌（**无读取点**，且常量旁的注释谎称「运行时通过 GlobalConfigParam() 读取」，已一并修正；实现是写死三档，单一阈值接不上） |
| `telemetry.feature_flag_poll_interval` | `pkg/featureflag/flag.go:34 =5` ✅ |
| `workflow.max_subflow_depth` | `workflow_node_executors.go:14 =5` ✅ |
| `telemetry.node_health_window` | `internal/monitor/monitor.go:19 nodeHealthWindow=24h` ✅ |
| `telemetry.trace_sink_buffer` | `internal/aiagent/llm/trace_sink.go:35 dbSinkBufferSize=2048` ✅（**种子原写 8192，已按代码回填为 2048**） |
| `telemetry.geo_position_window` | `internal/geo/service/metrics.go:71 positionWindow=20` ✅（**单位是字符偏移不是条数，种子 Name/Description 已一并改写**） |
| `workflow.max_workflow_steps` | `internal/service/workflow_dispatcher.go:153 函数内 const =1000` ✅ |
| `workflow.max_running_per_sop` | `internal/service/sop_scheduler.go:233 函数内 const =50` ✅ |
| `workflow.max_executed_node_trace` | `internal/service/sop_dispatcher.go:706 =200` ✅ |
| `sales.identity_max_attempts` | `service/customer_identity.go:335 函数内 const maxAttempts =8` ✅ |
| `middleware.audit_flush_interval` | `middleware/audit.go:58 函数内 const flushInterval =5s` ✅ |
| `middleware.mfa_recent_verify_ttl` | `middleware/mfa.go:21 mfaRecentVerifyTTL =5m` ✅（4 个读取点） |
| `knowledge.max_upload_file_size` | `knowledge/service/knowledge_base_import.go:35 MaxUploadFileSize =50<<20` ✅（导出 const 已改为函数；另 `knowledge_service_import.go` 4 处） |
| `knowledge.merchant_knowledge_max_len` | `knowledge/service/knowledge_merchant_external.go:483 flush 闭包内 const maxLen =2000` ✅ |
| `channelgw.ws_push_interval` | `channelgw/ws.go:41 =2s` ✅（**原为假接线**：读取点 key 写成 `ws_push_interval_default`，种子里是 `ws_push_interval`，已改正；真库行为测试已钉住） |

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

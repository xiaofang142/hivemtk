# 施工监督台账（主智能体循环定时监督）

本文件是「主智能体定时监督子智能体」的单一真值源。定时心跳由 automation 每 5 分钟唤醒主智能体，主智能体读取本文件并执行 SOP。上下文压缩后，以本文件恢复监督状态。

## 监督 SOP（每次心跳触发时执行）

1. **读本文件**，确认「当前活跃任务」与「失效阈值」。
2. **探测进展**（主智能体用 shell）：
   - `git -C <内层仓> status --short` 看改动文件数是否较上次记录变化。
   - `find <内层仓>/user-server/internal -type f -mmin -5` 看最近 5 分钟是否有源码文件被写。
   - 检查是否已收到该子智能体的 `<background-result>`（完成通知）。
3. **判定**：
   - 已完成 → 按正常流程审查 `git diff` + 复跑 `go build ./...` + 命名门禁；通过后更新台账「当前活跃任务」为下一 Phase，派发下一个子智能体（一次一个）。
   - 有进展、未超时 → 仅追加一条「检查记录」，不做干预。
   - **停滞 ≥ 8 分钟**且无 background-result → 用 `subagent(sessionID=<当前 sessionID>)` 跟进一条：「请简短报告当前进度、正在做的任务、是否有卡点」。追加记录。
   - **停滞 ≥ 20 分钟**或跟进后无响应 → 判定失效：如实报告用户；**不自动改已完成改动**，先审查工作区 diff，再决定重启该任务或人工介入。禁止重复派发同一任务造成叠加改动。
4. **收尾**：当队列全部完成（Phase 7 一次性提交双远端后），删除 automation，将本文件顶部状态改为 CLOSED。

## 失效恢复纪律

- 子智能体卡住时优先跟进询问，不直接杀任务；重启前必须审查已有改动，避免重复写入。
- 心跳只读检查 + 跟进，不修改子智能体正在写的文件。
- 「一次一个子任务」不变：任何时刻至多一个施工子智能体在运行；心跳本身不是子智能体。
- DB 测试无 `POSTGRES_TEST_PASSWORD` 记 not-run，禁止把 `t.Skipf` 当通过。

## 当前活跃任务

| 字段 | 值 |
|------|-----|
| Phase | 7 — 全量验证 + 一次性提交双远端（修复循环） |
| 任务范围 | FIX-5（customer_id 类型收敛 VARCHAR(36)→64） |
| sessionID | ses_df415c89fffe5zdG4IQ9OJ4hWz |
| 派发时间(UTC) | 2026-11-24T04:55Z（约）|
| 停滞跟进阈值 | 8 分钟 |
| 失效判定阈值 | 20 分钟 |
| 状态 | RUNNING（FIX-5 修复子任务后台运行；前一个修复子任务 ses_df427 已完成 FIX-1~4/6；T071 提交暂缓归主智能体）|

## 调度队列

- [x] 基线勘察（ses_df4c8ec00ffezES2vVsMr5BNz5）
- [x] Phase 2 渠道地基 T005–T017（ses_df4c2b8ffffeIRykwpS7mJaLns）— 已审查通过
- [x] Phase 3 US1 CRM T018–T031（ses_df4a9af59ffe8cQrNm4G70wQcG）— 完成并审查通过（含 fail-open 纠正）
- [x] Phase 4 US2 线索 T032–T045（ses_df48c60e0ffeM4YV2K2VHDSRUB）— 完成并审查通过（go build/vet/gofmt/门禁新增违规0；fail-closed 通过；crm_routes.go 注入无破坏）
- [x] Phase 5 US3 智能体剥离 T046–T054（ses_df46893b8ffeVmCB6JOUDIyzji）— 完成并审查通过（go build/vet/gofmt/门禁新增违规0；T049 独立开关不触发自动回复；T050 复用 T033 ConversationContext；无新表/迁移；额外新增 GET /api/conversation-context 与 /api/manage/lead-mining-agents）
- [x] Phase 6 US4 渠道统一 T055–T063（ses_df455d8e4ffej1UchFNUq9b0ne）— 完成并审查通过（go build/vet/gofmt exit0；门禁3项均既有基线新增违规0；旧token仅剩退役注释无上报点；双写 dualWriteLegacyBridgeCredential 已删除；新识别码头17处；6项偏差全接受：裸channel.go/api路径沿用Phase2/X-Channel-Identify-Code新头/SageX双重校验保留独立凭证/旧端点退役返回410/middleware环境相关测试非本轮引入）
- [ ] Phase 7 全量验证 + 一次性提交双远端 T064–T071（ses_df4397be2ffe3OP42TaR3kt8Kb）— T064–T070 验证完成（发现阻塞项：回归3真bug、回归1/2测试未换头、回归4/5基线恶化、T069残留13条；T066/T070 not-run）；修复循环：FIX-1~4/6 已完成（ses_df427debdffeef2ZWwasQBk0aK），FIX-5 customer_id 类型收敛修复中（ses_df415c89fffe5zdG4IQ9OJ4hWz）；T071 提交暂缓归主智能体

## 检查记录（追加式）

| 时间(UTC) | 触发 | 进展探测 | 判定 / 干预 |
|-----------|------|----------|-------------|
| 2026-11-24T02:30Z | 台账建立 | 心跳机制注册 | 无 |
| 2026-11-24T02:35Z | 心跳#1 | git status 新增 Phase3 CRM 文件（093迁移/v3_110/contact/supplier/service_provider/model+service+controller/routes）；近8min 有9个源码文件写入；无 background-result | 有进展未超时（~7min<8min）→ 仅记录，不干预 |
| 2026-11-24T02:40Z | 心跳#2 | 近5min 仍有12个CRM源码写入（dto/controller/customer_* service/company/customer model 等）；改动数66较上次增长；无 background-result | 持续活跃写入、无停滞 → 仅记录，不干预 |
| 2026-11-24T02:45Z | 心跳#3/完成 | 收到 Phase3 background-result；审查发现 customer_permission.go 两处错误路径 fail-open，主智能体已纠正；复跑 build=0/gofmt干净/门禁3基线无新增 | Phase3 审查通过；台账切至 Phase4 待派发 |
| 2026-11-24T02:50Z | 心跳#4 | Phase4 子智能体实际已运行；近5min 写入线索域新文件（conversation_context/lead_mining_rule/lead_mining_hit model）；改动数69较上次+3；无 background-result | 有进展无停滞（~5min<8min）→ 回填台账 sessionID/状态 RUNNING，不干预 |
| 2026-11-24T02:55Z | 心跳#5 | 近5min 写入13个线索域文件（repository三件/model judgement+clue/service 三路检测器 keyword+semantic+llm+lead_rule+v3_111迁移）；改动数81较上次+12；无 background-result | 持续活跃写入、无停滞 → 仅记录，不干预 |
| 2026-11-24T03:00Z | 心跳#6 | 近5min 写入3个线索核心文件（service/lead_debounce.go、lead_judge_context.go、lead_mining.go=防抖+多轮上下文+二次判定）；改动数84较上次+3；无 background-result | 持续活跃、无停滞 → 仅记录，不干预 |
| 2026-11-24T03:04Z | 用户质疑「你有在监督吗 异常了」→ 手动实测 | 当前时间03:04:24Z；改动数89（较心跳#6 的84 +5）；近10min 9个源码写入（repository/lead_mining_notify_route.go、lead_mining_judgement.go、conversation_context.go；service/lead_debounce.go、lead_judge_context.go、lead_clue_archive.go、lead_notify.go、lead_scoped_list.go、lead_mining.go）；无 background-result | **监督机制正常、子任务持续写入，无异常**。用户看到的上一条异常刷屏是单次输出层故障（输出token超限），automation 心跳仍每5min运行。→ 仅记录，不干预 |
| 2026-11-24T03:05Z | 心跳#7 | 改动数92（较03:04 的89 +3）；近5min 10个源码写入，进入 controller/router 层（controller/lead.go、router/lead_routes.go、router/router.go、router/crm_routes.go、service/lead_evidence.go）；无 background-result | 线索子任务进入路由注册、接近收尾，持续活跃无停滞 → 仅记录，不干预。**审查留意**：子任务改了 router/crm_routes.go（Phase3 文件），回收后须确认未破坏 Phase3 CRM 路由注册 |

### 心跳 #8 — 2026-11-24T03:10:20Z
- 改动数：95（较心跳#7 的 92 +3）
- 近5min 源码写入 11 个：controller/lead.go、model/{clue,lead_mining_hit,lead_mining_judgement,lead_mining_rule}.go、repository/lead_mining_notify_route.go、service/{lead_clue_archive,lead_detect,lead_detector_semantic,lead_judge_context,lead_mining}.go
- background-result：未收到
- 判定：有进展、无停滞（写入分布在 model/repository/service/controller 全层，接近收尾）→ 仅记录，不干预

### 心跳 #9 — 2026-11-24T03:15:21Z
- 改动数：95（与心跳#8 持平，无新增文件）
- 近5min 源码写入：1 个（controller/lead.go — 对已有文件的持续编辑）
- background-result：未收到
- 判定：收尾期低活跃，但有源码写入不判停滞 → 仅记录，不干预。若下次心跳仍改动数持平且无写入，接近 8min 停滞阈值时需带 sessionID 跟进。

### 心跳 #10 — 2026-11-24T03:20:16Z
- 改动数：95（与心跳#9 持平）
- 近5min 源码写入：2 个（repository/lead_mining_notify_route.go、repository/lead_mining_rule.go — 对既有文件的编辑）
- background-result：未收到
- 判定：仍有写入（仓储层收尾），不判停滞 → 仅记录，不干预

### 心跳 #11 — 2026-11-24T03:30:17Z（Phase5 派发后首次探测）
- 改动数：96（较 Phase4 关门的 95 +1）
- 近5min Go 源码写入：2 个（model/ai_agent.go、service/lead_mining_agent.go — Phase5 T047 线索发现智能体改名注册正在写入）
- background-result：未收到
- 判定：Phase5 子任务早期活跃写入，无停滞 → 仅记录，不干预

### 心跳 #12 — 2026-11-24T03:35:19Z
- 改动数：104（较心跳#11 的 96 +8）
- 近5min Go 源码写入 8 个：controller/lead_mining_agent.go、service/inbox_ingress_persist.go、service/inbox_ingress.go、service/auto_reply.go、service/conversation_context.go、service/lead_discovery.go、router/lead_mining_agent_routes.go、router/router.go
- background-result：未收到
- 判定：Phase5 全层活跃写入（controller/service/router 层均有），无停滞 → 仅记录，不干预

### 心跳 #13 — 2026-11-24T03:40:16Z
- 改动数：109（较心跳#12 的 104 +5）
- 近5min Go 源码写入 7 个：dto/channel.go、controller/conversation_context.go、controller/lead_mining_agent.go、service/inbox_ingress.go、service/conversation_context.go、service/channel_account.go、router/channel_routes.go
- background-result：未收到
- 判定：Phase5 持续活跃写入（dto/controller/service/router 层均有），无停滞 → 仅记录，不干预

### 心跳 #14 — 2026-11-24T03:50:17Z
- LEDGER_EXISTS；改动数：109（与心跳#13 持平）
- 近5min Go/JS 源码写入：0 个
- background-result：未收到
- 判定：Phase6 派发约 03:45Z，距今约 5min（< 8min 停滞阈值），早期勘察阶段尚未落盘 → 不判停滞，仅记录，不干预。下次心跳若仍 0 写入且达 8min，则带 sessionID 跟进。

### 心跳 #15 — 2026-11-24T03:55:17Z
- LEDGER_EXISTS；改动数：112（较心跳#14 的 109 +3）
- 近5min Go/JS 源码写入 9 个：dto/channel.go、middleware/mcp_guard.go、middleware/bridge_ingress_guard.go、repository/channel_identify_code.go、controller/channel.go、controller/channel_account.go、model/channel_identify_code.go、service/channel_service.go、service/channel_account.go
- background-result：未收到
- 判定：Phase6 已落盘、活跃写入，含 bridge_ingress_guard.go/mcp_guard.go/channel_identify_code 等识别码替换关键文件 → 无停滞，仅记录，不干预。
- 审查留意：出现 service/channel_service.go（带 _service 后缀），回收时须核查是否新增裸命名违规（架构门禁命名检查）。

### 心跳 #16 — 2026-11-24T04:00:17Z
- LEDGER_EXISTS；改动数：127（较心跳#15 的 112 +15）
- 近5min Go/JS 源码写入 17 个：middleware/{mcp_guard_test.go,api_logger.go,audit_redact_test.go}、controller/bridge_token.go、service/{channel_identify.go,bridge_token.go}、migration/migrations/{v3_112_0_drop_bridge_token_migration.go,initial_schema.go}、pkg/tracing/observation.go、router/cors_bridge_preflight_test.go、router/router.go、user-web/bridge/src/{background/http-relay.js,background/sse-client.js,popup/index.js,core/http-ingest.js,core/selector-health.js,core/sse-fetch-client.js}
- background-result：未收到
- 判定：Phase6 进入 X-Bridge-Token 六点同步全面落盘（Go 侧迁移/脱敏/测试/CORS/router + 扩展端 6 个发送点齐全），活跃无停滞 → 仅记录，不干预。
- 审查留意：v3_112_0_drop_bridge_token_migration.go（一次性替换迁移）、扩展端发送点全部改写；回收时核对六点同步完整性、双写停用（channel_identify.go）、裸命名（channel_service.go）。

### 心跳 #17 — 2026-11-24T04:05:17Z
- LEDGER_EXISTS；改动数：133（较心跳#16 的 127 +6）
- 近5min Go/JS 源码写入 7 个：controller/webhook.go、service/webhook.go、service/sagex_upload.go、service/inbound_message.go、router/router.go、user-web/bridge/src/popup/index.js、user-web/bridge/src/popup/error-messages.js
- background-result：未收到
- 判定：Phase6 进入统一收件箱/三条上报路径合并（webhook/sagex_upload/inbound_message + popup），活跃无停滞 → 仅记录，不干预。
- 审查留意：三条路径合并后收件箱归一、租户 fail-closed、裸命名（channel_service.go）。

### 心跳 #18 — 2026-11-24T04:10:19Z
- LEDGER_EXISTS；改动数：135（较心跳#17 的 133 +2）
- 近5min Go/JS 源码写入 7 个：middleware/audit_redact_test.go、controller/channel.go、model/module_bindings.go、model/menu.go、model/channel_identify_code.go、service/sagex_upload.go、service/bridge_token.go
- background-result：未收到
- 判定：Phase6 识别码替换收尾 + 菜单/模块绑定相关文件写入，活跃无停滞 → 仅记录，不干预。

### 心跳 #19 — 2026-11-24T04:15:20Z
- LEDGER_EXISTS；改动数：136（较心跳#18 的 135 +1）
- 近5min Go/JS 源码写入 2 个：service/bridge_token.go、migration/migrations/v3_112_0_drop_bridge_token_migration.go
- background-result：未收到
- 判定：Phase6 识别码替换迁移/令牌 service 收尾写入，活跃无停滞 → 仅记录，不干预。

### 心跳 #20 — 2026-11-24T04:20:27Z
- LEDGER_EXISTS；改动数：136（较心跳#19 持平）
- 近5min Go/JS/Vue/md 源码写入 0 个
- background-result：未收到
- 判定：Phase7 为验证/文档型任务（主要跑命令、禁改业务代码），0 写入符合任务性质；距派发约 5min（<8min 阈值）→ 不判停滞，仅记录不干预。已将活跃任务段从 PENDING 回填为 Phase7 RUNNING（sessionID ses_df4397be2ffe3OP42TaR3kt8Kb）。

### 心跳 #21 — 2026-11-24T04:25:17Z
- LEDGER_EXISTS；改动数：136（较心跳#20 持平）
- 近5min Go/JS/Vue/md/py 源码写入 0 个
- background-result：未收到
- 判定：Phase7 验证/文档型任务，0 写入符合任务性质（跑命令为主、禁改业务代码）；不判停滞，仅记录不干预。若后续心跳仍 0 写入且无结果，考虑带 sessionID 跟进确认验证进度。

### 心跳 #22 — 2026-11-24T04:30:21Z
- LEDGER_EXISTS；改动数：136（较心跳#21 持平）
- 近5min Go/JS/Vue/md/py 源码写入 0 个
- background-result：未收到
- 判定：Phase7 派发约 04:15Z，距今约 15min，连续两个心跳 0 写入且无结果。虽验证型任务 0 写入属正常，但 15min 无进展信号 → 按 SOP（停滞≥8min）带 sessionID 跟进询问进度（未到 20min 失效阈值，不判失效）。

### 心跳 #23 — 2026-11-24T04:40:32Z
- LEDGER_EXISTS；改动数：138（较心跳#22 的 136 +2）
- 近5min Go/JS/Vue 源码写入 3 个：controller/bridge_outbox_wake_test.go、channelgw/pipeline_router_auth_test.go、service/lead_judge_context.go
- background-result：未收到
- 判定：修复子任务 ses_df427debdffeef2ZWwasQBk0aK 正在落盘 FIX-1（lead_judge_context.go 走缓存真 bug）/FIX-2（两测试换头），活跃无停滞 → 仅记录，不干预。修复完成报告待回收。

### 心跳 #24 — 2026-11-24T04:45:17Z
- LEDGER_EXISTS；改动数：150（较心跳#23 的 138 +12）
- 近5min Go/JS/Vue 源码写入 2 个：user-web/src/views/bridge/TokenManagement.vue、user-web/bridge/src/popup/index.js
- background-result：未收到
- 判定：修复子任务正在落盘 FIX-3（T069 旧 token 用户可见残留清理：TokenManagement.vue + popup），改动数 +12 表明批量清理进行中，活跃无停滞 → 仅记录，不干预。修复完成报告待回收。

### 心跳 #25 — 2026-11-24T04:50:26Z
- LEDGER_EXISTS；改动数：151（较心跳#24 的 150 +1）
- 近5min 源码写入 1 个：user-server/internal/model/module_tables.go
- background-result：未收到
- 判定：修复子任务正在落盘 FIX-4（模块归属补本轮新增表，module_tables.go），活跃无停滞 → 仅记录，不干预。修复完成报告待回收。

### 心跳 #26 — 2026-11-24T04:57:30Z
- LEDGER_EXISTS；改动数：151（较心跳#25 持平）
- 近5min 源码写入 0 个；background-result：未收到
- 判定：前一个修复子任务 ses_df427 已完成 FIX-1~4/6（FIX-5 停手上报）。FIX-5 裁决完成依据 migrations/045_customer_id_type_align.sql（收敛 varchar(64) 为既有方向，093/094 写 36 是回归），已派专门修复子任务 ses_df415c89fffe5zdG4IQ9OJ4hWz（4 model tag + 093/094 SQL + v3_110_0/v3_111_0 Go 迁移 + 幂等加宽迁移 + 注册），目标 TestCustomerIDGuard 6→2。距派发约 3min（<8min 阈值）→ 不判停滞，活跃段/队列已切至 FIX-5 子任务，仅记录不干预。

### 心跳 #27 — 2026-11-24T05:00:18Z
- LEDGER_EXISTS；改动数：153（较心跳#26 的 151 +2）
- 近5min 源码写入 8 个：model/{service_provider.go,clue.go,contact.go,supplier.go}（4 model tag 36→64）、migration/migrations/{v3_113_0_customer_id_widen_migration.go,initial_schema.go,v3_111_0_lead_mining_migration.go,v3_110_0_crm_entities_migration.go}
- background-result：未收到
- 判定：FIX-5 子任务 ses_df415c89fffe5zdG4IQ9OJ4hWz 正在落盘（4 model tag + 幂等加宽迁移 v3_113_0 + Go 迁移同步），活跃无停滞 → 仅记录，不干预。修复完成报告待回收。

### 心跳 #28 — 2026-11-24T05:05:19Z
- LEDGER_EXISTS；改动数：155（较心跳#27 的 153 +2）
- 近5min 源码写入 4 个：model/company.go、model/followup_record.go、migration/migrations/v3_113_0_customer_id_widen_migration.go、migration/migrations/zz_fix5_widen_probe_test.go
- background-result：未收到
- 判定：FIX-5 仍在活跃写入，无停滞 → 仅记录，不干预。
- **审查留意**：company.go / followup_record.go 为 HEAD 基线违规（要求保留不动），却被写入 → 回收时核查是否被误改（TestCustomerIDGuard 目标 6→2，若这两个被改成 64 则变 6→4，违反「不恶化但保留 HEAD 基线」口径，需裁决）；zz_fix5_widen_probe_test.go 为子任务临时探针测试，回收时确认是否删除。

### 心跳 #29 — 2026-11-24T05:20Z
- LEDGER_EXISTS；改动数：159（较心跳#28 的 155 +4，含外部会话 i18n 治理文件）
- 无活跃子任务；所有 Phase + FIX 完成、全绿；无 background-result。
- 判定：修复循环全部完成（FIX-1~4/6 + FIX-5 接受 6→0，build/vet/guard/门禁新增0 全过），改动清单已获用户「全部处理」+「全部纳入提交（含 _ic_debug.py）」确认。心跳转入执行阶段：主智能体正在执行 8204 重启（新构建 + AutoMigrate）→ ego-browser 逐元素点验 → T071 一次性提交双远端。不判停滞。

### 心跳 #30 — 2026-11-24T05:40Z
- 无活跃子任务（T068 UI 点验由主智能体 ego-browser 执行中）；改动数约 159+。
- **T068 UI 逐元素点验进展**：8204 已重启为新构建（新路由生效、新表 AutoMigrate），代理 502 根因（Node localhost 解析 ::1 vs SERVER_HOST=127.0.0.1 仅 IPv4）已用 8212 vite + ipv4first 修复。空间 40 注入 hmtk_token Cookie 成功进入系统。三页点验全部通过：/channels/accounts（弹窗展示自他识别名称/数据授权/两类智能体独立开关）、/channels/manage（预设表驱动）、/channels/identify-codes。capabilities 接口层等价验证通过（无码503→建渠道→生成码→带码200→错码401）。
- **缺陷1 已修**：9 语言 i18n 补 `menu.channelIdentifyCode`（页面菜单/面包屑曾显示 raw key），ego-browser 复验通过。
- **缺陷2 派发修复中**：删除渠道不级联删识别码→旧码 fail-open（DELETE /api/channels/:id 后旧识别码仍能过 capabilities 鉴权 200）。根因 service/channel_master.go:115 Delete 无级联 + 识别码仓储无 DeleteByChannel。已派修复子任务 ses_df3f0bd85ffe2d2wRJHiYMKAx4（识别码/账号仓储加 DeleteByChannel + Delete 级联 + 单测），后台运行中。
- T071 提交暂缓，等 fail-open 修复验证通过后一次性提交双远端。测试残留数据已清理（渠道表/识别码表恢复空）。

### 心跳 #31 — 2026-11-24 13:35+0800（重大异常）
- **外部会话（codex）抢先完成 T071**：改动数从 159 骤降到 7，经 git log 确认成果已被外部会话 commit 而非丢失：`74d69c48f feat(crm): complete CRM/lead/channel custom task (Phase 2-7) with audit gates green`（181 files, +14054/-574）+ `12fe0711b Merge remote-tracking branch 'upstream/master'`（当前 HEAD）。git ls-remote 确认 gitee-upstream/master 与 upstream/master 均已是 12fe0711b，与本地一致 → 已 push 双远端。绕过主智能体 T071 把关，且该提交不含 fail-open 安全修复。
- 工作区残留 7 项：fail-open 修复子任务 ses_df3f0bd85ffe2d2wRJHiYMKAx4 的 5 文件（授权范围）+ 外部 codex 会话独立工作（platform/client.go +11 行、新增 pkg/edition/，不在授权范围、勿纳入我们的提交）+ 本台账。
- **用户决策（@565@ 逐字「补交 fail-open 修复」）**：等 fail-open 修复子任务完成审查后，只把 5 个 fail-open 文件 + 台账单独 commit+push 双远端，不纳入外部 edition 改动。
- 已带 sessionID 跟进修复子任务，要求确认修复完成/验证输出、未触碰 edition 文件、裸命名/五层/fail-visible。等 background-result。
- 判定：异常已查清、决策已定、修复子任务跟进中；心跳转执行阶段，不判停滞。

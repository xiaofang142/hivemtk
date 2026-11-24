# 线索发掘模块对抗性审计报告（2026-11）

> 审计日期：2026-11-24。审计对象：`hivemtk-user` 内层仓线索发掘模块（基线外层 `docs/线索发掘模块-完整方案.md`，已落地规格 `.specify/custom-task-crm-lead-channel-20261124/`）。
> 方法：静态源码逐层追踪（Router→Controller→Service→Repository→Model）、规格/任务/台账交叉核对、测试真实性判读。**未修改任何产品代码，未执行有副作用的写操作。**
> 说明：本仓 `AGENTS.md` 已注明外层 `CLAUDE.md` 中"内层路径 hivemtk"为历史表述，本文所有路径均相对本次工作区 `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user`，报告正文以 `user-server/...`、`user-web/...` 形式给出。

---

## 0. 结论先行

**结论：不可上线。** 当前实现是一套"合成测试全绿、端到端主线未跑通、关键需求靠代码注释宣告完成"的状态。存在 2 个 P0（配置链路双轨导致发掘规则/渠道与三粒度规则脱节、群聊上下文"聚合"实际上把多成员上下文替换成了一份只由当前发送者写入的单一缓存行）、6 个 P1（个微/QQ 根本不在 Bridge 线索资格白名单、二次 LLM 双次调用与证据收集顺序错误、`lm:*` 平行身份仍在、通知双路径重复/路由口径分裂、跟进记录从未自动生成、验收证据自相矛盾）、若干 P2。

`supervision.md` 自己记录 **T066/T070 not-run**（`supervision.md:46`），而 `tasks.md:148,152` 把这两项打勾 `[x]`。在补齐真实验收证据、修复 P0/P1 之前，"Phase 7 全绿"是台账口径，不是产品事实。

---

## 1. 六组需求逐项核对（Cartesian 对照表）

| # | 用户原始需求 | 实现状态 | 关键判据 |
|---|---|---|---|
| 1 | 个微/QQ 上报消息触发发掘 | ⚠️ **仅 QQ 上报可入 MessageHub；个微(`personal_wx`)与 QQ 均被线索资格白名单排除，不会 Enqueue** | `service/bridge_lead_eligibility.go:11` + `service/douyin_lead_miner.go:208-212` |
| 2 | ReAct 分析单聊/群聊；单聊两人历史、群聊大家发言 | ❌ **无 ReAct；群聊多成员上下文被"当前会话缓存"覆盖，绝大多数情况退化为单体历史** | `service/lead_judge_context.go:197-218,224-239`；`service/conversation_context.go:192-214` |
| 3 | 检查整个发掘模块（设置表单/执行/整理/脑暴/重复匹配/渠道全覆盖） | ⚠️ 部分：设置表单仍是老单例 8 字段；三粒度规则另建了一套，二者互不相通；无执行记录；无行业词库 | `dto/lead_mining.go:5-16`；`service/lead_mining.go:304-310` vs `service/lead_rule.go:39-74`；`controller/lead_mining.go:23-59` |
| 4 | 线索列表/统计展示需求方/供货方/上下游 | ✅ 基本满足（六维统计、lead_role 过滤、索引齐全） | `repository/clue.go:256-303`；`migrations/085_lead_mining_extension.sql:12-13` |
| 5 | 客户360与客户身份、聚合消息客户自动进入与去重 | ❌ **仍在建 `lm:*` 平行身份；自动发掘链路从不调 IdentifyOrCreate/OneID；360 无 lead_mining_hit 事件** | `service/lead_mining.go:679-720,722-756`；`service/clue_archive.go:71-87` |
| 6 | 发现并通知（TG/飞书、按账号/渠道维护人、跟进建议/历史摘要/跟进记录/标签） | ⚠️ 部分：通知有三级路由但存在双路径重复与口径分裂；无历史摘要；**跟进记录（FollowupRecord）从未被自动创建** | `service/lead_mining.go:641-677` vs `service/lead_mining_notify_external.go:131-173`；`service/lead_notify.go:46-135` |
| 6(2) | 统一收件箱按聊天页展示单聊/群聊、区分渠道账号 | ✅ 基本满足（聊天视图、渠道账号头、成员名展示） | `user-web/src/views/inbox/List.vue:428-455,932-940` |

---

## 2. P0 — 上线阻塞

### P0-1 配置双轨：发掘总开关/渠道白名单与三粒度规则完全脱节

**证据**
- `user-server/internal/service/lead_mining.go:304-310`：每次 process 先用 `loadConfig()` 读 legacy `lead_mining_configs` 单例，检查 `cfg.Enabled`、`channelEnabled(cfg, hub.Platform)`，不满足直接 return。
- `user-server/internal/service/lead_judge_context.go:169-179`：随后**另行**用 `LeadRuleService.Resolve()` 解析三粒度规则，解析失败时才回填 legacy 配置的关键词。
- `user-server/internal/dto/lead_mining.go:5-16`、`user-web/src/views/leadMining/Index.vue:350-364,404-411`：设置表单只有 enabled/channels/keywords/tags/requirement/min_intent_score/model 八个字段，没有 account_scope、notify、judge_mode 等。
- `user-server/internal/router/content_routes.go:75-85`、`user-server/internal/router/lead_routes.go:26-42`：两套配置 API 各有各的入口，无联动。
- `user-server/internal/model/lead_mining_config.go:12-25`：comment 声称"主键 = tenant_id"，实现 `singletonID()` 确实是 tenant_id，但 `leadMiningConfigToDTO/fromDTO`（`lead_mining.go:209-235`）不映射 Scope/ScopeID，配置表单保存的 singleton 规则行（迁移 091 每租户一条 global 规则）与 LeadMiningRule 表的 global 规则是**两份数据**。

**为什么是问题**
1. 运营在「发掘设置」里关闭总开关或取消勾选渠道 → 整个发掘停摆；但三粒度规则（FR-011，用户第三部分明确要求）页面里的规则开关、账号粒度规则**完全不生效**，因为它们只在 legacy 开关通过后才被读取。
2. 反过来：运营在三粒度规则页为账号 A 配了专用规则，但 legacy `channels=[]`（默认全开）时也好，配了白名单时也好，判定用的 `MinIntentScore` 始终来自 legacy，而不是规则行。
3. 表单只暴露八字段，方案 §5.1 列的账号范围、敏感词、行业词库、上下游信号、通知开关、去重窗口、judge_mode **全部未实现**；`tasks.md` 却把 T045（前端规则/通知路由/判定证据/归档页面）打勾完成。
4. 用户第 3 组需求"设置表单"实际上被拆成两个互不通气的页面，运营无法判断到底哪个开关说了算。

**建议修复**
- 单一配置源：删除 `loadConfig` 作为发掘开关/渠道判据，改为 `LeadRuleService.Resolve` 返回的规则（含 enabled/channels/account_scope）为唯一判据；legacy config 仅作为数据迁移兼容层，读写时双向同步。
- 表单字段补齐方案 §5.1，并与三粒度规则 UI 合并；`SaveConfig` 与 `LeadRuleService.Save` 走同一事务。
- 加集成测试：关闭 legacy enabled 但开启账号级规则，断言仍产出线索（当前必失败）。

### P0-2 群聊上下文"聚合"是假聚合：多成员发言被覆盖为单体历史

**证据**
- `user-server/internal/service/lead_judge_context.go:197-218`：
  - `ctxRepo.Get(ctx, target.ChannelAccountID, hub.ConversationID)` 取缓存行；
  - 若 `conv != nil`（**只有缓存行已存在时才进 if**），才 `append` 当前消息并 `Upsert`；
  - 紧接着 `if jctx.Conversation != nil && len(...Messages)>0` 用**这个缓存行的 Messages 整体覆盖 `msgs`**。
- `user-server/internal/repository/conversation_context.go:48-61`：`Get` 查不到时返回空壳 `ConversationContext{...}`（非 nil）。所以首次命中时 `conv != nil` 成立，`append` 也执行，但 `Upsert`（`:74-79`）的 `DoUpdates` 只更新 member_roles/messages/tags/summary——继续看下一步。
- `user-server/internal/service/lead_discovery.go:53-69`：ingress 阶段只对群聊写 `SetMemberRole(..., "member")` 与 `AggregateGroupContext`（后者读的是**同一张尚未写入本条消息的缓存行**），从不调 `AppendMessage`。
- `user-server/internal/service/conversation_context.go:51-62`：`AppendMessage` 实现了，但全链路无任何生产调用点（grep 只有定义）。
- 结果：`conversation_contexts.Messages` 在发掘链路里只包含**被判定过的当前消息**（由 `lead_judge_context.go:199-202` 追加），没有自动回复侧入站/出站消息；群聊 `AggregateGroupContext`（`lead_judge_context.go:224-239`）对这份"单消息缓存"做角色标注，`GroupRoleCounts` 永远最多 1 个成员。
- `repository/message_hub_inbox.go:359-391`：真正的多成员历史查询 `ListRecentByConversation` / `ListRecentByGroup` 是存在的，被 `Service.fetchHistoryDB`（`lead_mining.go:442-454`）调用产出了 `history`，**却在 `lead_judge_context.go:181-191` 转成 `DetectionMessage` 后于 `:211-218` 被整体丢弃**。

**为什么是问题**
- 直接违反用户第 2 组需求"群聊结合大家发言"与 spec FR-021 / 验收标准第 2 条（`spec.md:165`）。
- 代码的"富化"只给当前发言人贴 `[member:xxx]`（角色还是 `lead_discovery.go:56` 硬编码的 `"member"`，不是 demand/supply），所以 prompt 里的"群成员发言统计"是假数据；`lead_judge_context_test.go:10-27` 只测了字符串模板包含，不测数据真实性。
- 单聊同样受损：`fetchHistoryDB` 取到的双向历史（含 outbound→assistant）被丢弃，二次 LLM 只看当前一句 + 一条共享备注；`spec.md` FR-013 "纳入多轮对话与历史消息"未兑现。

**建议修复**
- 删除 `lead_judge_context.go:211-218` 的覆盖逻辑；改为：把 `history`（真 MessageHub 双向/多成员数据）作为权威上下文，共享 `ConversationContext.Messages` 仅作为补充合并（按 MessageID 去重，按时间排序），不允许覆盖。
- 在 ingress 层（不在 worker 里）对每条入站/出站消息调用 `ConversationContextService.AppendMessage`，保证共享历史是双智能体共享的真实全量；群成员角色由 LLM/规则判定写入而非固定 `"member"`。
- 补真库测试：3 个成员在群内分别发言，断言 `jctx.Messages` 含 3 个不同 SenderID 与原文（当前代码必失败）。

---

## 3. P1 — 高优先级

### P1-1 个微/QQ 上报消息根本不会进入线索发掘

**证据**
- `service/bridge_lead_eligibility.go:10-13`：`bridgeLeadEventEligible` 第一道判据 `!isBridgeLeadMiningChannel(event.Channel)` 直接 false。
- `service/douyin_lead_miner.go:208-212`：白名单只有 `douyin/tiktok/kuaishou/xiaohongshu/xianyu`；个微常量为 `personal_wx`（`internal/bridge/channel.go:22`、`internal/model/message_event.go:20`），QQ 为 `qq`（`message_event.go:26`），二者都不在表内。
- `service/inbox_ingress_persist.go:122,154-161`：只有 `BridgeLeadEligible=true` 且被 `leadDiscoverySvc.Process` 闸门放行才会 Enqueue；`service/douyin_lead_miner.go:191` 亦强制校验 `BridgeLeadEligible`。
- `user-web/src/views/leadMining/Index.vue:353-364`：前端渠道选项写了 `wechat`/`wecom`，却没有 `qq`；`service/channel.go:24-34` 预设渠道含 `qq` 与 `wecom` 但不含 `personal_wx`。

**为什么是问题**
- 用户第 1 组需求逐字是"个微/QQ 上报消息"，第 2 组是"ReAct 分析个微/QQ 消息"——两条主链路对这两个渠道都是关闭的。QQ 消息能落 MessageHub 和收件箱，但不会被发掘；个微连预设渠道码都不一致（`wechat` vs `personal_wx`）。
- 方案 §5.5 明确要求"个微/QQ 必须纳入默认支持清单"、渠道覆盖与 MessageHub 对齐（验收标准第 1 条），当前未满足。
- 配置层 `cfg.Channels=[]`（默认全开）也救不了：资格白名单在更上游，先于配置判断。

**建议修复**
- 将 `personal_wx`/`wechat`、`qq` 加入 `leadMiningChannels` 白名单（两处 `service/douyin_lead_miner.go:208`、`internal/bridge/handler_http.go:776` 需收敛为单一注册表，避免双份漂移）；为个微/QQ 提供对应 content script 或明确标注"依赖外部伴生上报"并给人工上报入口。
- 增加渠道覆盖矩阵测试：遍历 `ChannelService.EnabledOptions()`；每个渠道注入一条标准采购消息，断言进入 Enqueue。

### P1-2 ReAct 未实现；三路检测证据收集存在"顺序颠倒 + 二次 LLM 双次调用"的正确性缺陷

**证据**
- 全仓 `grep -rn "react" user-server/internal/service/lead*` 零命中（只有编辑器 reactive 前端变量）；`service/lead_mining.go:826-845` 是单次 `DispatchStructured` 的 `dispatcherJudge`，`service/lead_judge_context.go:41-74` 是第二次 `DispatchStructured` 的 `JudgeWithContext`——**每条命中消息至少 2 次 LLM 调用**，加上 `service/lead_detector_llm.go:81` 的三路之一又是 1 次，共 3 次。
- `service/lead_detector_llm.go:81-89`：LLM 路超时/未初始化时返回 `Complete=false, Degraded=true`，但**不返回 error**。
- `service/lead_detect.go:86-107`：三路用 `errgroup.WithContext(ctx)`，任一返回 error 会取消 g；但 `g.Wait()` 得到的 error 只在上层 `lead_judge_context.go:243-250` 记 warn 然后 **继续判定**。
- `service/lead_judge_context.go:241-250` 在 `Run` 之后才设置 `jctx.Hits`；而 `service/lead_mining.go:336-346` 传 `jctx` 给 `JudgeWithContext`，即三路 hits 在 prompt 构造（`:112-119`）时**已就绪**——这部分顺序实际正确；但 `recordLeadMiningJudgement`（`lead_mining.go:884-956`）落库的 `ContentHash` 是 `fmt.Sprintf("%d", hub.ID)`（`:921`），不是 dedup_hash，`MessageID` 只用当前 hub.MsgID，无法与三路 hits 的 `history_N` 伪 ID 对齐（`lead_judge_context.go:183-187` 用 `history_%d` 作证据消息 ID，查不回真实消息）。

**为什么是问题**
- 用户第 2 组逐字要求 ReAct（工具链分析），实现里没有工具调用、没有推理-行动循环，只是"关键词/语义/LLM 三路 + 二次 LLM 总结"——方案 §5.2 承诺的 ReAct 工具化判定是 P2 未落地项（`tasks.md` 无对应任务）。
- 每条消息 3 次 LLM 调用、无 token 裁剪（`buildLeadJudgeSystemPrompt` 把全部上下文塞进 system prompt），群聊高频下成本不可控；方案 §4.2 要求"token 裁剪：优先保留命中句/报价/联系方式"，未实现。
- 证据消息 ID 用 `history_N` 伪造，导致 spec SC-003 "证据可解释/可回放"无法真正回放（`GetLeadMiningJudgementDetail` 只能返回 JSON 字符串）。

**建议修复**
- 把三路检测作为可选快路径，二次 LLM 在未命中/低置信时才走；用真实 `MessageHub.MsgID` 填充 `DetectionMessage.MessageID`，去掉 `history_%d`。
- 明确 ReAct 是 P2 还是 P0：若是 P0，需要加工具（查客户360/历史线索/词库）与推理循环；若降级为普通 LLM，应修订 spec 与方案并告知用户，不得用"ReAct 已完成"的台账口径。
- 实现上下文 token 裁剪（按时间窗+命中句+角色发言优先级）并加成本上限/熔断。

### P1-3 客户身份仍是平行 `lm:*` 体系；自动发掘链路从未走 OneID/CustomerIdentity

**证据**
- `service/lead_mining.go:683-720`：`resolveCustomer` 第一查 `legacyKey := "lm:" + platform + ":" + senderID`，找不到则 `applyLeadMiningIdentity` 生成 `GenerateCustomerUnifiedID`，失败再回退 `legacyKey`，最后 `custRepo.Create`；**全程不调用 `CustomerIdentityService.IdentifyOrCreate` / OneID repository**。
- `service/lead_mining.go:753-755`：注释明确承认"qq/sms/web/dingtalk 等暂无专用字段，回退 legacy lm: key"。
- `service/lead_mining_notify_external.go:442-448`：外发通知前 enrich 仍去查 `lm:` key。
- `service/lead_mining.go:537`：`clue.OneID = customer.UnifiedID`，因此线索表写入的 one_id 是 `lm:*` 或由 Customer 字段拼出的 ID，而不是 OneID 主身份；`:456-460` 通知侧才发现 `lm:` 开头才尝试替换，属于事后补救。
- `service/clue_archive.go:71-87`：`ArchiveClueAndLinkCustomer` 才是真正用 `IdentifyOrCreate` 的地方，但**自动发掘链路从不调用它**（它只被 `LeadClueArchiveService` 的手动 `/api/manage/leads/archive` 使用）。

**为什么是问题**
- 直接违反用户第 5 组需求与方案 §5.7/验收标准第 6 条"不产生平行 `lm:*` 客户体系"。
- 同一真实客户在抖音+微信各发一条消息，会产生两条 Customer（不同 unified_id），OneID 合并看到的仍是两个影子身份；SageX/客户360 再也拼不回一个客户。
- "客户360 时间线增加 lead_mining_hit 事件"（方案 §5.7）未实现：grep 无该事件类型。

**建议修复**
- `persistLead` 改用 `CustomerIdentityService.IdentifyOrCreate(ctx, identity.Identifiers{...})`，`applyLeadMiningIdentity` 只负责组装 identifiers，不再生成 `lm:*`；存量 `lm:*` 行写迁移映射回主身份。
- 自动发掘命中后调用 `ClueArchiveService` 完成关联与 OneID 回写（当前只有手动归档端点走那条路）。
- Customer360 时间线补 `lead_mining_hit` 事件类型。

### P1-4 通知双路径重复、路由口径分裂、外发内容未真正脱敏

**证据**
- 路径 A：`service/lead_mining.go:652-676` 同时 (a) 调 `s.notifier.NotifyOwner`（LeadNotifyService，按三级 scope 路由，站内/TG/飞书）(b) `_ = NotifyBusiness(...UserID:0...)` 一条全局广播。
- 路径 B：`NotifyBusiness`（`service/notify_business.go:97-113`）末尾触发 `dispatchLeadMiningExternalHook`，由 `service/lead_mining_notify_external.go:31-53,131-173` 用**另一套** `selectLeadMiningNotifyRoutes`（按 account+channel 字符串匹配）再发一遍 TG/飞书，且走裸 `go func`（`:166,168`）。
- 两套路由判据不一致：`service/lead_notify.go:209-247` 按 `scope` + `ChannelAccountID`(数值) 解析；`lead_mining_notify_external.go:175-205` 按旧 `account_id`(字符串) + `channel` 匹配。同一条路由在两套里可能一个命中一个不命中，或都命中 → 重复外发。
- 去重仅按 `(tenant, clueID, channel, target)` 10 分钟（`lead_mining_notify_external.go:304-326`），路径 A 的 `deliverExternal` 完全不走这个 dedup。
- 脱敏：`sanitizeLeadMiningNotifyText`（`:231-249`）只按空格切词后 mask 纯数字/带@字段；中文连续文本里的手机号（`13812345678` 夹在汉字中）、微信号、姓名、订单号不会命中；`createLeadMiningNotifyRoute` 的 `Target` 从前端直接绑定 model（`controller/lead.go:97-111`、`controller/lead_mining_notify_route.go:36-43`），无校验，任意 chatID/receiveID 可配 → 越权通知风险。
- 送达记录：路径 A 写 `lead_mining_notify_routes.delivery_records`（`service/lead_notify.go:120-124`），路径 B 只打日志，无送达记录；spec SC-008 "送达率 100%"无法核验。

**为什么是问题**
- 用户第 6 组要求"按账号或渠道维护人、不同渠道通知不同人员"，当前第三方通知可能发给错误人（两套路由取第一命中不同）或同一人发两遍；`UserID:0` 广播把所有线索推给全体用户，与"按人路由"相反。
- 外发脱敏不完整，可能把客户手机号/微信号直接发到 TG/飞书群，违反方案 §9。

**建议修复**
- 收敛为单一出口：删除 hook 外发路径（或把它降级为仅站内），所有 TG/飞书统一走 `LeadNotifyService.NotifyOwner`；去掉 `UserID:0` 广播。
- 通知路由写入时校验 scope/target 属于租户且账号在当前用户数据范围内（FR-016）；`target` 必须匹配 `^\d+:(chatID|openID|oc_...)` 白名单格式。
- 脱敏使用正则覆盖连续中文文本中的 11 位手机号、邮箱、身份证/银行卡、微信号，并默认只发摘要+系统内跳转（已在 `buildLeadMiningExternalMessage` 做了 80 字截断，但正则需补）。

### P1-5 跟进记录（FollowupRecord）从未由线索发掘自动创建；历史摘要缺失

**证据**
- 全文检索 `service/lead*`、`controller/lead*`、`service/clue_archive.go`：`FollowupRecord` 零命中；只有 `service/followup_record.go:20-35` 提供 CRUD，生产调用方仅 `controller/followup_record.go:21` 的手动端点。
- `service/lead_mining.go:52,547,572,863`：只把 LLM 输出的 `FollowupSuggestion` 写进 `clues.followup_suggestion` 字段，没有生成 `followup_records` 行，也没有 Reminder。
- 通知内容（`service/lead_mining.go:663-667`、`service/lead_notify.go:86-87`）只有"摘要+建议"，没有"历史总结摘要"（用户第 6 组明确要求"跟进建议/历史总结摘要/跟进记录/标签"）。
- `FollowupRecord` 模型（`model/followup_record.go`）字段齐全（NextAction/EvidenceJSON/Source 等），但 `SourceRef` 从未被 lead_mining 使用。

**为什么是问题**
- 需求清单逐项未兑现 3/4：有建议、有标签（`tagCustomer`）、无历史摘要、无跟进记录。
- `tasks.md` 无 T0xx 对应 FollowupRecord 自动创建；`followup_close_loop_test.go` 测的是 reminder/record 服务自身闭环，不证明发掘链路会写记录。

**建议修复**
- 在 `persistLead` 成功后（或 `LeadClueArchiveService.Archive` 内）用 `FollowupRecordService.Create` 写一条 `Source="lead_mining"`、`SourceRef=clueID`、`NextAction=jd.FollowupSuggestion`、`Content=jd.Summary` 的记录；同时创建 Reminder（用户要求"跟进记录"）。
- 通知内容增加"历史摘要"段（从 ConversationContext.Summary / 客户360 摘要取），并经脱敏。

### P1-6 验收证据与完成声明自相矛盾；大量测试是合成/fake，真实验收标记 not-run

**证据**
- `supervision.md:46`：`T066/T070 not-run`，并列出"回归3真bug、回归1/2测试未换头、回归4/5基线恶化、T069残留13条"；而 `tasks.md:148,152` 把 T066/T070 打勾 `[x]`（完成）。
- `scripts/api_verify_full.py` 全文无 `lead-mining` / `manage/leads` / `/manage/crm` 路径；一条 grep 只命中注释里的"线索发掘那条 LLM 路径"（`:605`）。因此 SC-009 "全部新增/修改 API 通过三端验证"无脚本覆盖。
- `service/lead_mining_test.go:16-296`：大量 `fakeCustRepo/fakeClueRepo/fakeCfgRepo/fakeJudge`，断言的是内存 map 行为；`service/lead_mining_integration_test.go:85-105` 用 `fakeJudge` 返回硬编码结论，不验证 LLM 判定质量。
- `service/lead_judge_context_test.go:10-58`：只断言 system prompt 字符串包含，不断言群聊消息真实含多成员原文（见 P0-2）。
- `service/bridge_lead_eligibility_test.go:38-42`：测试 `Extra["bridge_lead_eligible"]=true` 的客户端自证被拒（好），但白名单渠道之外（个微/QQ）完全没有测试。
- `internal/pkg/testutil/testdb.go:807-825`：`NewTestDBOrSkip` 在实例不可达时跳过，虽是"按设计"，但 `supervision.md:24` 已声明 DB 测试无密码记 not-run；无证据表明本轮以正确密码跑过 lead 相关真库测试。

**为什么是问题**
- 报告"全绿"如果建立在合成 fake 与未跑的 API 验证上，无法作为上线判据；用户被台账误导。
- spec SC-001（去重准确率≥95%）、SC-002（越权隔离 100%）、SC-008（通知到达率 100%）均无可复核数据。

**建议修复**
- 用真实测试库（`POSTGRES_TEST_PASSWORD` 外导）跑 `go test ./internal/service/ -run 'Lead|Notify|Conversation' -count=1` 并贴输出；跑 `python3 scripts/api_verify_full.py api` 并新增 lead/crm/channel 端点矩阵。
- tasks.md 把未跑的 T066/T070 回退为未完成，supervision 台账作为唯一真值源。
- 把 fake 测试保留为单元测试，但至少补 3 条真库端到端：①个微/QQ 上报→发掘→线索；②群聊三成员→判定含三成员原文；③通知按账号路由仅发给指定人且送达记录有值。

---

## 4. P2 — 中优先级

### P2-1 架构违约：Service 持 `*gorm.DB`、裸 `go func`

- **证据**：`service/lead_mining_agent.go:11,28-33` 构造函数接收 `*gorm.DB`；装配点 `router/lead_mining_agent_routes.go:14-15` 把 `*gorm.DB` 传进 Service。`service/lead_mining.go:110-113` worker 裸 `go s.worker(ctx)`；`service/lead_mining_notify_external.go:163-169` 裸 `go sendLeadMiningTelegram/Feishu(bgCtx,...)`，无 WaitGroup/超时/cancel 传播（`bgCtx := tenant.OnlyTenant(context.Background(), tid)` 丢弃了请求取消信号）。
- **为什么**：五层架构规定 Service 不写 SQL/不碰 `*gorm.DB`（`AGENTS.md` §2、CI `check-layer-boundary.py`）；裸 goroutine 导致关停时通知丢失、无背压。`service/lead_mining_agent.go` 只是把 DB 转成 repo，但签名本身违反契约，且 CI 若只查 import 不查函数签名会漏掉。
- **建议**：改 `NewLeadMiningAgentServiceWithRepo(repo)`，Router 只传 repo；通知外发走带超时/注入 WaitGroup 的 dispatcher 或复用 errgroup 收口。

### P2-2 队列满静默丢消息、内存去重启即失效

- **证据**：`service/lead_mining.go:245-254` 队列满 `default: logger.Warnf` 后丢弃，无死信/重试；`:78,322-327` `lastJudge` 是进程内 map，重启清零；`service/lead_debounce.go:22,26-28` 同进程内 map；`lead_mining_notify_external.go:26-29` 也是进程内 dedup。
- **为什么**：高负载或多副本部署时消息丢失与重复判定不可恢复；跨副本无共享去重。
- **建议**：队列满落 `lead_mining_dead_letter` 表；去重键与窗口落库（`lead_mining_judgements` 已有 message_id 索引）或 Redis。

### P2-3 统计口径与索引缺口

- **证据**：`repository/clue.go:256-303` 六维聚合按 `deleted_at IS NULL` 过滤但**不带 `tenant_id`**（依赖 GORM 全局租户 callback）；`GetClueStatisticsWithRange` 按 create_time 过滤，但 `migrations/085/094` 索引只覆盖 lead_role/status/feedback/customer，无 `(tenant_id, create_time)` 复合索引；`by_channel` 用 `Limit(20)` 截断但 `total` 是全量，口径可能误导。
- **为什么**：数据量大后统计查询全表扫；渠道>20 时展示不全且无 truncated 标志（与 `GetClueStatisticsWithRange` 承诺的 `truncated=false` 语义不一致）。
- **建议**：补 `(tenant_id, create_time)` / `(tenant_id, source_channel, create_time)` 索引；by_channel 不截断或返回 truncated=true。

### P2-4 通知路由创建无业务码校验/越权风险；TG/飞书账号按数值 ID 取凭证未验租户

- **证据**：`controller/lead.go:97-111`、`controller/lead_mining_notify_route.go:36-43` 直接 `ShouldBindJSON(&model.LeadMiningNotifyRoute)`，无字段白名单与范围校验；`service/lead_notify.go:175-186`、`lead_mining_notify_external.go:358-392` 从 target 解析出 `accID` 后直接 `NewTelegramIntegrationService(...).SendMessage(ctx, uint(accID), ...)`；`service/feishu.go:664-667` → `repository/feishu.go:166-172` 的 `GetByID` 只按主键查（依赖 callback 补租户）。
- **为什么**：若 callback 在 system scope/单租户档下允许，配错/恶意 target 可跨租户借用凭证发消息；路由 target 无归属校验即"越权通知"通道。
- **建议**：发外发前显式 `GetByID(ctx, tid, accID)` 验证账号属于当前租户（复用 `ChannelAccountRepository.GetByID` 口径），并写操作审计。

### P2-5 Webhook 鉴权与配置回滚口径

- **证据**：`service/feishu.go:470-504` 自动生成 webhook_secret 与 URL；本审计未发现线索发掘模块自行暴露 webhook 端点（外发用的 IntegrationService），故无新密钥硬编码（grep lead 系列无 token/secret 字面量）。但 `LeadMiningNotifyRoute.Target` 存的是 chatID/receiveID，未加密落库（迁移 094），DB 泄露即可用于定向发消息。
- **建议**：Target 加密或最小化存储；外发只保留脱敏后的 target 摘要（`maskLeadMiningTarget` 已在日志使用，落库记录里也应用）。

### P2-6 前端契约漂移

- **证据**：`.specify/.../contracts/leads.md:16-33` 自己标注多处"待核对/路由未找到"（judgements 列表、rejudge、clues CRUD）；`user-web/src/api/leadMining.js:40-49` 调 `POST /api/clues/:id/feedback`，后端在 `content_routes.go:62` 确有该路由（admin 档），但 `leadMining.js` 其它端点（quality-stats 等）与 `leads.js` 的请求封装风格不一致（一个用 URLSearchParams，一个用 params 对象），易出错。
- **建议**：以实际路由为唯一真值源更新 contracts，前端 API 客户端统一封装并加契约测试。

---

## 5. 需求→证据快速索引（便于复核）

| 结论 | 最短复现命令 / 断点 |
|---|---|
| 个微/QQ 不进发掘 | 在 `service/bridge_lead_eligibility.go:11` 打断点，发 `channel=qq`/`personal_wx` 消息，观察 `isBridgeLeadMiningChannel=false` |
| 群聊上下文被覆盖 | 构造 3 成员群消息，观察 `lead_judge_context.go:211-218` 执行后 `jctx.Messages` 只剩 1 条 |
| `lm:*` 平行身份 | 触发一次发掘，查 `customers.unified_id like 'lm:%'` 有新行 |
| 通知双路径 | 配一条 telegram 路由，发一条线索，观察 TG 收到次数（预期 1，实际可能 2）与站内 UserID=0 广播 |
| 跟进记录缺失 | 触发发掘后 `SELECT * FROM followup_records WHERE source='lead_mining'`，0 行 |
| 验收 not-run | 对照 `supervision.md:46` 与 `tasks.md:148,152` |

---

## 6. 上线判定与最小补齐清单

**判定：不可上线（NO-GO）。** 六组需求中仅第 4 组与第 6(2) 组基本满足；第 1、2、3、5、6 组均存在功能性缺口，其中 P0-1/P0-2 直接让"发掘规则"和"上下文分析"两个核心能力失真。

**上线前必须完成（按依赖顺序）**
1. **P0-2**：修复上下文覆盖，确认单聊双向/群聊多成员原文进入判定；在非 fake 测试中验证。
2. **P0-1**：合并配置源，使三粒度规则真正控制开关/渠道/阈值；前端表单字段补齐。
3. **P1-1**：个微/QQ 纳入渠道白名单并测试上报→发掘全链。
4. **P1-3**：客户身份改用 OneID/CustomerIdentity，杜绝新增 `lm:*`。
5. **P1-4**：通知双路径收敛为单出口，按账号/渠道维护人路由，补脱敏与越权校验。
6. **P1-5**：自动生成跟进记录/历史摘要。
7. **P1-6**：补真库与 API 三端验收证据，回退虚假 `[x]`。
8. **P2**：队列死信、架构签名、索引与 goroutine 收口。

**下线/降级建议**：若业务必须尽快上线个微/QQ 的"上报+收件箱查看"（需求1落库部分、需求6(2)），可在明确告知用户"线索自动发掘/分析/通知暂不启用"的前提下单独放量收件箱能力；但不得宣称线索发掘模块整体完成。

---

*审计员备注：本报告只做证据归因与风险定性，不含修复代码。所有行号基于 2026-11-24 工作区快照；若代码随后变更，需重新核对。*

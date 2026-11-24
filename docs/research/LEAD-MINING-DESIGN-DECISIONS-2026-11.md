# 完整线索发掘模块设计决策调研（2026-11）

> **性质**：产品与技术决策调研，**不改产品代码**。一手来源 = 官方开源源码（Chatwoot develop，2026-11-24 curl 实取）、官方 CDP 文档（mParticle IDSync，2026-11-24 实取）、权威论文（ReAct / Anthropic 上下文工程）、本仓真实实现（路径+行号）。
> **调研日期**：2026-11-24。**阅读基线**：`hivemtk-user` 工作树（含迁移 065 / specs 024-025 已落地状态）。
> **证据标记**：`实证` = 本轮逐行核对源码/官方文档；`仓内文档` = 本仓既有调研文档（非一手，仅作线索）；`无法验证` = 一手来源不可达，如实标注；`推论` = 结构判断，未做 A/B 实验。
> **配套**：`CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md`、`CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md`（以下称「聚合对照文」「统一收件箱文」）。

---

## 0. 执行摘要（一句话决策）

1. **统一收件箱**：Chatwoot 的地基是 `ContactInbox(inbox_id, source_id)` 唯一键 + 「按 ContactInbox 而非 Contact 复用会话」，本仓 `CustomerChannel` 唯一键**在迁移 065 后已补 `(tenant_id, one_id, channel, account_id)` + 身份锚**，旧调研文档「缺 account_id」结论已过时；真正待补的是**按「渠道账号 + 平台身份」寻址会话**与单聊/群聊展示语义（详见 D1）。
2. **客户身份**：继续用 **OneID**（不做 CDP 替换）；把 OneID 从「主表里塞 12 个身份列 + 优先级生成 UnifiedID」迭代到 **contact_inbox 式身份锚 + OneID 作为合并后的稳定主键**，并把「识别/合并/审计」与「客户 360 读模型」解耦（D2）。Segment 官方识别文档本轮**不可达（403）**，改为 mParticle 官方 IDSync 文档作一手依据。
3. **上下文与多轮**：保持「共享 `ConversationContext` + 滑动窗口 20 条 + 三路检测 + 二次 LLM 判定」，**先补摘要压缩（compaction）与状态机化判定**，再考虑 ReAct 工具调用（D3）。不做「每条消息立即 LLM 判定」——60s 防抖 + 队列是现有正确取舍。
4. **渠道自动纳入**：以 `channelgw.Default` 注册表为唯一真值源，把 `channelEnabled`（配置白名单）、两处 `leadMiningChannels` 硬编码 map、`LeadAdapterForPlatform` switch 改为**注册表投影 + parity 门**；新增渠道只登记一处（D4）。
5. **最终决策矩阵**见 §5，每项含 2–3 个选项、推荐、理由、失败信号。**本轮未运行任何测试/构建**（纯调研文档写入，不触碰产品代码）；结论基于静态源码与官方文档。

---

## 1. 统一收件箱：单聊/群聊按聊天页展示、区分渠道账号

### 1.1 Chatwoot 官方源码事实（实证）

| 机制 | 一手源码锚点 |
|---|---|
| 身份地基：`contact_inboxes` 唯一索引 `(inbox_id, source_id)` | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/db/schema.rb#L829-L839（本轮实取：`t.index ["inbox_id", "source_id"], name: "index_contact_inboxes_on_inbox_id_and_source_id", unique: true`） |
| 身份归并：`ContactInboxWithContactBuilder` 先按 `(inbox, source_id)` 找，未命中才在 `requires_new: true` 事务里 `find_contact`（identifier → email → phone_number + 候选 → Instagram source_id 跨 FacebookPage 反查），`rescue RecordNotUnique` 重试 | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/builders/contact_inbox_with_contact_builder.rb（本轮实取全文，120 行） |
| **会话归属必须限定在 ContactInbox 维度，不能按 Contact 维度**——官方注释原文：*"Reuse is scoped to the contact inbox that resolved this message, never to the contact. A contact can hold unrelated WhatsApp identities in the same inbox..."*；仅当渠道无法按身份寻址（360Dialog 只能发到 `to`）才退回 Contact 维度 | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/services/whatsapp/incoming_message_base_service.rb#L106-L125（本轮实取） |
| 会话查找：`lock_to_single_conversation ? conversations.last : conversations.where.not(status: :resolved).last || create` | 同上；仓内文档 `docs/research/CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md:213-228` |
| 幂等三件套：`find_message_by_source_id` → Redis `SET NX` 锁 → 事务落库；`source_id` 同时用于出站回声判定 | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/services/whatsapp/incoming_message_base_service.rb（本轮实取 `process_messages`）；https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/services/base/send_on_channel_service.rb（`outgoing_message_originated_from_channel? = message.source_id.present?`） |
| 消息驱动状态机：`after_create_commit` 七步固定顺序（重开→待接管→活动时间→事件/待回复计时→出站→模板→联系人活跃）；`waiting_since` 连发不刷新、有效首响清空并写 `first_reply_created_at`；muted 不重开、bot 收件箱 resolved→pending | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/models/message.rb#L367-L478（本轮实取） |
| 出站注册表：`SendReplyJob::CHANNEL_SERVICES` 映射 + `Base::SendOnChannelService` 模板方法；新渠道 = 一个子类 + 一行映射，FacebookPage/Instagram DM 二级路由用显式方法隔离 | https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/jobs/send_reply_job.rb；https://raw.githubusercontent.com/chatwoot/chatwoot/develop/app/services/base/send_on_channel_service.rb（均本轮实取） |
| 邮件按线程 header 归并而非按人：`Mailbox::ConversationFinderStrategies::{InReplyTo, References, ReceiverUuid, NewConversation}` | 仓内文档 `docs/research/CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md:408,432-434`（一手源码未逐行复核，标 `仓内文档`） |

### 1.2 本仓真实实现（实证，路径+行号）

- 入站事实源 `model.MessageHub` 已含 `platform / account_id / conversation_id / is_group / group_id / sender_id / direction / msg_id`，唯一键 `(platform, msg_id, conversation_id, direction)`：`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server/internal/model/ai_sales_champion.go:12-79`。
- 统一会话生命周期列（WaitingSince / FirstReplyAt / LastActivityAt）已落地：`user-server/internal/model/ai_sales_champion.go`（见仓内文档 `CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md:165-180`）；会话按平台账号+客户维度寻址 `FindByPlatformAccountCustomer`（仓内文档 `:150-153`）。
- **重要纠偏**：旧调研文档 `CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md:150` / `CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md:496` 称「`CustomerChannel(one_id, channel)` 唯一键不含 account_id，是当前最该修的数据缺陷」。本轮核对 `user-server/internal/model/customer_channel.go:22-35` 与 `migrations/065_customer_channel_identity_keys.sql:5-6,67-79`：**该缺陷已修**——业务键 `(tenant_id, one_id, channel, account_id)`、身份锚 `(tenant_id, channel, account_id, channel_user_id)` 均已建成，旧索引 `idx_oneid_channel` 已 DROP。决策不应重复立项。
- 群聊判定来自 `MessageHub.IsGroup/GroupID`，群成员角色由 `ConversationContextService.SetMemberRole` 写入、`AggregateGroupContext` 补 `[角色:发送者]` 前缀：`user-server/internal/service/conversation_context.go:73-87,189-214`；判定提示区分群/单聊：`user-server/internal/service/lead_judge_context.go:91-111`。
- **差距**：本仓客户会话查找尚未见 Chatwoot 那种「按 `(channel_account, channel_user_id)` 身份锚找会话、按 Contact 维度只作兜底」的显式规则；`CustomerChannel` 身份锚已具备数据条件，但 `UpsertFromMessageTx` 的会话归属是否消费该身份锚本轮未逐行核对（标 `无法验证`）。旧文档列出的「按渠道渲染正文」「模板消息」「未读计数外置」仍为待立项（仓内文档 `CHATWOOT-...:228-243`）。

### 1.3 判断

- 单聊/群聊展示**已有可用骨架**（`is_group/group_id` + 角色标注），不需要重做数据模型。
- 真正要补的是**会话寻址口径**：把「一个平台账号下同一自然人有多个平台身份」的情况收敛到身份锚，避免跨身份串会话（Chatwoot 官方注释明确点名的陷阱）。
- 消息级能力声明已有 `channelcaps`（15 条 manifest，`user-server/internal/channelcaps/manifests.go`），但 `channelcaps.FeatureGroup` 是否被线索发掘用于「该渠道群聊是否可判定」未接线（标 `无法验证`）。

---

## 2. 客户 360 / 身份自动进入并去重：继续 OneID，还是换方案

### 2.1 行业一手做法（实证）

**mParticle 官方 IDSync 文档**（2026-11-24 实取，https://docs.mparticle.com/guides/customer-360/idsync/introduction/ 及同目录页）：

- *"IDSync is mParticle's identity resolution framework, enabling you to create a unified view of your customers, with improved data governance, policy, and security."*（introduction）
- 身份解析三步：identify 请求带全部标识 → 按**身份优先级（identity priority）升序**找匹配 profile → 找到返回、找不到按策略建匿名 profile 或不动（`identify-users/`）。原文强调"customer ID or email is more likely to be unique to a single user than a device ID"。
- 五种身份策略：profile conversion（匿名→known 保留同一 MPID 与历史）、default（customer_id 唯一且不可改）、profile link（known 新建 profile + alias 归因）、profile isolation（匿名数据不归给 known，隐私合规）、best match（无登录体系、不强制唯一）。来源：`guides/customer-360/idsync/components/`。
- **关键护栏**：known profile（有 login ID）只能被包含匹配 login ID 的请求返回——*"a record with at least one login ID can only be returned if the identify request includes a matching login ID"*；否则新建 profile，宁可分裂也不误并。来源：`guides/customer-360/idsync/user-data/`。
- **重复代价官方明说**：*"Configuring multiple login IDs can result in the creation of unwanted duplicate user profiles... user data for that customer becoming split between two profiles."*（default-strategy）。这正是本仓 `LeadMining` 里 `lm:platform:sender` 平行身份膨胀的同型问题。
- alias 需要显式 `destination MPID / source MPID / start time`，且 profile conversion 默认不另建 profile：`guides/customer-360/idsync/aliasing/`。

**Chatwoot 官方**：`ContactInbox(inbox_id, source_id)` 唯一 + `Contact` 归并顺序（见 §1.1）——同一人在多渠道是多个 contact_inbox 指向一个 contact，**不是把所有渠道 ID 塞进一张 contact 表**。

**Segment 官方**：`https://segment.com/docs/connections/identify/tracking-identity/` 本轮 **403 Forbidden**，curl 与 WebFetch 均不可达；仓内 `docs/research/` 无 Segment 一手摘录。**无法验证**，不作为决策依据。

### 2.2 本仓真实实现（实证，路径+行号）

- `CustomerIdentityService.IdentifyOrCreate` 优先级 Phone > Email > WechatOpenID > DouyinOpenID > XiaohongshuID，输入先 `NormalizeIdentifiers`（`user-server/internal/service/customer_identity.go:34-56`；归一化规则在 `user-server/internal/identity/normalize.go`：`NormalizePhone` 去 +86/空格、`NormalizeEmail` 小写、`NormalizeOpenID` 仅 trim 且**不改大小写**）。
- `MergeByIdentity` 按同身份查全部候选，租户级持久化规则（`oneid_merge_rules_v1`）+ 版本 + 自动合并总闸 + 审核窗口 + 主档策略 + 冲突策略，事务内 `MergeCustomers`（`customer_identity.go:203-297`；规则 `oneid_merge_rule.go:17-35,559-588`）。默认规则集含 8 条内置（phone/unionid/email/openid/douyin/xhs 等），默认 `ConflictBehavior=auto_merge, AutoMergeEnabled=true`（`oneid_merge_rule.go:559-588`）。
- 去重扫描 `CustomerDedupeService.FindDuplicates`：先跑 OneID 规则（复用同一 `evaluateRule` 口径），再用**名称编辑距离相似度**仅作 `review` 提示，不自动合并；`SameAccountOnly` 默认 true 缩小跨账号误合并面（`user-server/internal/service/customer_dedupe.go:28-33,63-145,247-269`）。合并写 `CustomerMerge` 审计 + `OperationLog`（`:177-245`）。
- `UnifiedID` 生成优先级 Phone > Email > WhatsApp > Telegram > 微信 > 飞书 > 企微 > 抖音 > TikTok > 快手 > 小红书 > 闲鱼（`user-server/internal/model/customer.go:115-159`）。
- **OneID 与 360 的接缝**：`Customer360Service` 先按 `sessions.user_id` 或 `customers.id → unified_id → customer_sessions.one_id` 取会话，再拼 BasicInfo/Clue/Order/Profile；`resolveCustomerForSessions` 只用 phone/email 反查 customer（`user-server/internal/service/customer_360.go:203-262,305-328,338-400,855-877`）。存在**纯 OpenID 客户在 360 视图里解析不到 customer 档案**的缺口：`resolveCustomerForSessions` 只传 `FindByIdentity(ctx, phone, email, "", "", "")`（`:861-868`），而 `GetByCustomerID` 路径又要求先有 customers.id 主键（`:338-352`）。
- **线索发掘侧平行身份**：`Service.resolveCustomer` 优先复用 `lm:platform:sender` legacy key，否则按 `applyLeadMiningIdentity` 写渠道字段生成新 UnifiedID，并对未映射渠道（qq/sms/web/dingtalk 等）回退 `lm:` key（`user-server/internal/service/lead_mining.go:679-756`）。这与 mParticle 警告的「多 login ID 造成 profile 分裂」同型。
- `MergeCustomers` 事务内迁移 session/DNC/customer_channels/csat/clues/事件后**物理删除 secondary**（`user-server/internal/service/customer.go:353-425`），审计写在事务外（`:423`）；仓内旧文档 `docs/oneid/oneid-analysis.md` 已把「物理删除断链」列为 P1-4（该文件写于早期，当前 `MergeCustomers` 已迁多数关联表，仍需核对事件/标签是否全量覆盖——本轮未穷举，标 `无法验证`）。

### 2.3 判断

- **继续用 OneID，不改成 Segment/mParticle 式 CDP**——理由：Python/Go 自建栈、已有 8 条规则 + 版本化 + 审计 + 360/RFM/触达全链路依赖 `UnifiedID` 作为稳定主键（仓内文档 `docs/oneid/oneid-architecture.md:9-14,50-70`；源码 `customer.go:115-159`）。替换收益低于迁移成本。
- **采纳 mParticle 的两条护栏，不采纳其「多 login ID 任意建档」**：① 已知身份必须匹配 login ID 才返回（防止 `phone:` hash 撞到错误档案）；② 把 `lm:platform:sender` 这类**渠道局部键**从主身份降级为 `CustomerChannel` 的身份锚，避免平行主身份膨胀。
- **客户 360 与身份解耦**：360 是读模型，不应以 `sessions.user_id` 为唯一寻址键；应统一走 `customers.id → unified_id → sessions + clues/orders`（`GetCustomer360ByCustomerID` 已是该口径，`GetCustomer360` 仍以 user_id 为入口，见 `customer_360.go:203` vs `:338`）。

---

## 3. 线索发掘：单聊/群聊重复匹配与上下文/多轮

### 3.1 行业一手做法

- **ReAct（Yao et al., 2022，https://arxiv.org/abs/2210.03629）**：*"we explore the use of LLMs to generate both reasoning traces and task-specific actions in an interleaved manner... reasoning traces help the model induce, track, and update action plans as well as handle exceptions, while actions allow it to interface with external sources"*。在 HotpotQA/Fever 上通过 Wikipedia API 交互**抑制幻觉与错误传播**，在 ALFWorld/WebShop 上分别 +34%/+10% 绝对成功率。**推论**：适合「需要查库/查历史再判定」的线索场景，但本仓现有三路检测已覆盖证据获取，ReAct 的增量主要在**异常分支与工具式取证**，不是替代滑动窗口。
- **Anthropic《Effective context engineering for AI agents》（2025-09-29，https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents）**：*"context rot: as the number of tokens in the context window increases, the model's ability to accurately recall information from that context decreases"*；上下文是**有限预算**，目标应是 *"smallest possible set of high-signal tokens"*。长任务三法：**compaction**（接近上限时总结并重开窗口，保留架构决策/未解 bug/实现细节，丢弃冗余工具输出）、**structured note-taking**（上下文外持久化笔记，后续拉回）、**multi-agent**。*"do the simplest thing that works"*。
- **对话状态机**：Chatwoot 把会话状态转移集中在 `Message#after_create_commit`（`open→snoozed/resolved/pending`，入站重开、bot 收件箱 resolved→pending、muted 不重开），状态列可查（§1.1，源码 `message.rb#L367-L478`）。

### 3.2 本仓真实实现（实证，路径+行号）

| 环节 | 实现 | 锚点 |
|---|---|---|
| 非侵入异步队列 | queue 8192 / workers 4 / 60s 防抖 / 判定失败不占防抖窗 | `user-server/internal/service/lead_mining.go:32-39,246-274,321-354` |
| 上下文窗口 | `leadMiningHistorySize=20`；单聊按 conversation、群聊按 group、兜底按 sender 查最近 20 条并按时间 ASC 排列；群聊给每条消息加 `[speaker]` 前缀 | `lead_mining.go:435-487` |
| 共享上下文 | `(tenant_id, channel_account_id, external_conversation_id)` 唯一键 upsert；消息 JSONB 为全量覆盖写（非 append），`SetSummary`/`MemberRoles`/`Tags` 同表 | `user-server/internal/repository/conversation_context.go:15-18,64-80`；`user-server/internal/service/conversation_context.go:51-62,73-87,108-126,189-214`；`user-server/internal/model/conversation_context.go:96-107` |
| 富上下文组装 | 三粒度规则解析（失败回退配置兜底并 Warn）→ 历史 + 当前消息 → 写共享上下文（超 20 截断）→ 群聊角色富化 → 三路检测 → 二次 LLM 判定 | `user-server/internal/service/lead_judge_context.go:149-251`；`user-server/internal/service/lead_detect.go:41-64` |
| 证据可见性 | `complete=false / degraded=true` 的证据在提示中标注「证据不完整，不得当成完整证据」；群聊聚合失败标注不可确定角色 | `lead_judge_context.go:108-119,221-239` |
| 判定输出 | `ScenarioLowCost`、1024 tokens、temperature 0.2、JSONMode，结构化字段含 is_lead/intent_score/role/summary/confidence/reason | `lead_judge_context.go:39-74,120-127` |
| 群/单聊去重键 | 单聊 `platform:sender`，群聊 `platform:conversation:sender`，带租户前缀 | `lead_mining.go:363-372` |
| 线索落库 | 按 `(type, account)` 找已有线索，存在则更新、不存在则创建（TOCTOU 回查转更新），高分转商机 | `lead_mining.go:489-604,617-639` |
| **缺口** | `ConversationContext.Summary` 字段有 setter（`conversation_context.go:108-116`）但**在 `buildLeadJudgeContext` 里未读取、未写入**（`lead_judge_context.go:152-251` 无 `Summary` 引用）；即 compaction 地基已备而接线缺失 | 实证：grep `Summary` 在 `lead_judge_context.go` 零命中 |
| **缺口** | 共享上下文 `Messages` 每次 Upsert 全量覆盖且并发无版本号/乐观锁（`conversation_context.go:60-62` + repository `:64-80`），双 worker 同会话可能后写覆盖前写 | 实证：Upsert `DoUpdates` 含 `messages`，无 CAS 条件 |

### 3.3 判断

- **保留现有「滑动窗口 + 三路检测 + 二次判定」主干**，这是与 Anthropic "smallest high-signal tokens" 一致的设计，且已有 fail-visible 证据口径。
- **优先做摘要压缩接线（低风险、高收益）**：把 `ConversationContext.Summary` 接入 `buildLeadJudgeContext`——超出 N 条时把最旧窗口交给 LLM 生成结构化摘要（客户诉求/角色/已确认事实/未决问题），新上下文 = `Summary + 最近 K 条 + 当前消息`。不要一上来做 ReAct。
- **状态机化判定**应落在判定记录而非会话表：现有一轮一判 `LeadMiningJudgement`，缺「同一线索跨多轮的判定状态」字段；建议加 `lead_state`（candidate/confirmed/rejected/expired）与 `evidence_refs`，由判定结果驱动，避免重复生成多条线索（现有按 `(type, account)` 覆盖已部分解决，但没有状态语义）。
- **ReAct 定为第二阶段**：当出现「判定需要主动查历史/查客户档案/查渠道能力」的需求时再用工具调用；需给工具白名单、步数上限、失败可见，并保留关键词/语义检测作 fallback。
- **并发控制必须先修**：20 条窗口 + JSONB 全量覆盖在 4 worker 并发下同会话丢消息概率随量上升；加版本号（CAS）或改 append 语义。

---

## 4. 「聚合消息有多少渠道，线索发掘就要有多少渠道」的落地机制

### 4.1 现状：四套事实源并存（实证）

| 事实源 | 位置 | 覆盖 |
|---|---|---|
| `model.Channel*` 常量 | `user-server/internal/model/message_event.go:6-42` | 28 个渠道常量（含 webhook 直发与桥接） |
| `channelgw.Default` 注册表 | `user-server/internal/channelgw/registry.go:159-194` | 15 个桥接渠道 spec，含 Transport/Experimental/OutboundPath；`init()` 经 `service.SetBridgeOutboxChannels(Default.OutboundChannels(...))` 投影给出站（`:196-201`） |
| `channelcaps` manifest | `user-server/internal/channelcaps/manifests.go` | 15 条 manifest；注册重复 panic（`channelcaps.go:93-100`）；生成文档由测试钉死（`channelcaps_test.go`） |
| `service.channelEnabled(cfg, platform)` | `user-server/internal/service/lead_mining.go:770-780` | 配置 `cfg.Channels` 为空则**全部放行**，否则按大小写不敏感精确名匹配（手工维护） |
| 两处 `leadMiningChannels` map | `user-server/internal/service/douyin_lead_miner.go:208-212`（5 个）+ `user-server/internal/bridge/handler_http.go:776-781`（同 5 个） | 仅 douyin/tiktok/kuaishou/xiaohongshu/xianyu 参与桥接线索发掘；`unsupportedLeadMiningChannels` 列 9 个「需扩展/协议」原因（`:213-223`） |
| `LeadAdapterForPlatform` switch | `user-server/internal/service/lead_miner_adapters.go:314-350` | 15 个 case + default `ClueTypeCustom`；新渠道默认落 `GenericLeadAdapter` |
| 出站 sender 注册表（已插件化，可借鉴） | `user-server/internal/service/outbound_registry.go:39-87`；`outboundSenderFor` 对 bridgeOutboxCapable 渠道回退统一 `bridgeOutboxSender`（`:97-125`） | 直发渠道逐个 `RegisterChannelSender` 工厂；桥接族统一由 registry 能力位承接 |
| parity 门（已存在） | `user-server/internal/channelgw/registry_parity_test.go:175-223,301-337`；`user-server/internal/service/outbound_registry_parity_test.go:112-121` | 钉住 registry ↔ 出站能力、registry ↔ channelcaps/capregistry/枚举三面；**但没有钉住「registry ↔ lead mining channel 集合」** |
| 扩展侧适配器 | `user-web/bridge/src/channels/`（15 个 js）；实验集 `user-web/bridge/src/core/constants.js:277`（11 个 Experimental） | 注册但缺扩展适配器不得宣称全支持（registry.go:151-154） |

**失效模式（推论，可由 parity 门防复发）**：新增渠道登记了 `channelgw.Default` + `channelcaps`，自动回复可用、出站可用，但因为 `leadMiningChannels` map 没加、或 `cfg.Channels` 白名单没加，**线索发掘静默跳过**。现有 parity 测试不覆盖这条缝，且 `channelEnabled` 空配置全放行会让「名单为空=全开」与「名单非空=漏一个就静默关」两种语义并存。

### 4.2 建议机制（决策 D4 的落地配方）

1. **唯一真值源 = `channelgw.Default`**：给 `ChannelSpec` 增加 `LeadMining` 能力位（如 `MineMode: none|bridge|direct`），在 `init()` 里用 `Default.LeadMiningChannels()` 一次性投影到 service/bridge（仿 `SetBridgeOutboxChannels`）。
2. **删/降级两处硬编码 map**：`service.leadMiningChannels` 与 `bridge.leadMiningChannels` 改为读取同一投影；`unsupportedLeadMiningChannels` 保留为**解释性原因表**（不是准入表），仅用于 API 提示。
3. **`channelEnabled` 语义改为「禁用名单/覆盖」而不是「启用白名单」**：默认所有已注册且 `MineMode != none` 的渠道都参与；租户配置只表达「显式关闭某渠道」。这样新增渠道自动纳入；显式开白名单的租户可保留 old semantics 并用迁移标记。
4. **`LeadAdapterForPlatform` switch 改为注册表工厂**：`map[channel]func() ChannelLeadAdapter` + `RegisterLeadAdapter(ch, fn)`（重复 panic），default 落 `GenericLeadAdapter` 但记录一次 warn；`ChannelSpec` 里声明 `ClueType/Prefix/ChatLabel` 等可配置字段，消除 switch 与新渠道的耦合。
5. **parity 门扩写**：`registry_parity_test.go` 增加「`Default.Names()` 中每个标 `MineMode != none` 的渠道必须：① channelcaps.Known ② 有 LeadAdapter 工厂 ③ bridge 侧投影集合相等 ④ 扩展侧有对应 js 适配器（Experimental 可例外但必须登记）」。任何一项缺即红。
6. **保留现有网关/发送器注册表**作为范式：`channelgw` 用 `OutboundPath` 能力位驱动出站（`registry.go:36-40,97-111,196-201`），`outbound_registry.go` 用工厂表驱动直发 sender——线索发掘应完全同构，不再第三套名单。

---

## 5. 决策矩阵

> 推荐选项标 **★**。失败信号 = 一旦出现即应推翻/重审该决策的可观测证据。

### D1 单聊/群聊展示与会话寻址

| 选项 | 说明 |
|---|---|
| A ★ | **保持 `MessageHub` 的 `is_group/group_id/conversation_id` 骨架；把会话寻址收敛到 `(channel_account, channel_user_id)` 身份锚**（复用迁移 065 的 `idx_channel_identity`），Contact 维度仅作无身份寻址渠道兜底；群聊角色标注沿用 `AggregateGroupContext`。 |
| B | 重做「每渠道一张 contact_inbox 表」完全照搬 Chatwoot。 |
| C | 维持现状，只做前端按聊天页分组展示。 |

**推荐 A**。理由：Chatwoot 官方注释明确「会话复用必须按 ContactInbox 而非 Contact」（§1.1），本仓 `CustomerChannel` 已有身份锚数据条件（`customer_channel.go:22-35`），无需新增表；前端按聊天页展示已有 `is_group/group_id` 支撑（`ai_sales_champion.go:35-37`）。B 成本高且会与 OneID 双主；C 会把串会话风险留给线上。
**失败信号**：① 出现「同一平台账号下两个身份的回复打到错误会话」的客诉/工单；② 身份锚查询与现有 `FindByPlatformAccountCustomer` 结果不一致率 >1%；③ 迁移 065 的身份锚在真实数据里大面积 `channel_user_id=''`（无法寻址）。

### D2 OneID 去留与身份治理

| 选项 | 说明 |
|---|---|
| A ★ | **继续 OneID**：保留 `UnifiedID` 作稳定主键、保留规则引擎/版本/审计；把 `lm:platform:sender` 降级为 `CustomerChannel` 身份锚；补「login ID 才返回 known profile」护栏；修 `Customer360.resolveCustomerForSessions` 对纯 OpenID 客户的解析。 |
| B | 换成 mParticle/Segment 式 CDP（多 MPID/alias/profiles）。 |
| C | 彻底去 OneID，按渠道独立建档，360 视图按会话聚合。 |

**推荐 A**。理由：mParticle 官方文档确认「多 login ID 任意建档 → profile 分裂」是已知代价（`default-strategy`），本仓已通过 OneID + 规则版本化规避；360/RFM/线索/触达全链依赖 `UnifiedID`（§2.2）；mParticle 的 alias 模型（§2.1）在本仓无等价基础设施。B 属重写级迁移；C 直接放弃去重。
**失败信号**：① 自动合并造成跨客户误并（审计里 `CustomerMerge` 的 reason 指向错误匹配）且月均 ≥1 起；② `FindByIdentity` OR 查询把同 phone 但不同租户/账号的客户错误归并；③ OneID 规则版本在双副本间漂移（`oneid_merge_rules_v1` 的版本读不一致）。

### D3 上下文与多轮架构

| 选项 | 说明 |
|---|---|
| A ★ | **滑动窗口 20 条 + 摘要压缩（compaction）+ 判定状态机**：先把已有 `Summary` 字段接线（旧窗口→结构化摘要），给共享上下文加 CAS 版本号，判定记录加 `lead_state/evidence_refs`。 |
| B | 直接上 ReAct 工具调用（查库/查历史/查角色）替代现有三路检测 + 二次判定。 |
| C | 扩大窗口到 50–100 条，不做压缩。 |

**推荐 A**。理由：Anthropic 明示 context rot 与 "smallest high-signal tokens"（§3.1），本仓 `Summary` 有 setter 却未接线（`conversation_context.go:108-116` vs `lead_judge_context.go:152-251` 零引用），属于低风险补齐；ReAct 论文收益来自「与外部 API 交互取证」（§3.1），本仓三路检测已承担取证，增量在异常分支而非主链；扩窗口直接放大 token 成本与噪声。并发全量覆盖（`repository/conversation_context.go:64-80`）需先修。
**失败信号**：① 摘要压缩后二次判定 `is_lead` 与全窗口判定不一致率 >5%（关键信息被摘要吞掉）；② CAS 冲突率高到 >1% 判定被拒；③ 引入摘要后 LLM 成本未降反升（摘要调用 + 判定调用 > 原单次判定）；④ 线上出现「同一线索反复生成/状态来回翻转」。

### D4 渠道自动纳入线索发掘

| 选项 | 说明 |
|---|---|
| A ★ | **`channelgw.Default` 注册表为唯一真值源，加 `MineMode` 能力位 + 工厂注册 `LeadAdapter` + parity 门；删/改两处硬编码 map；`channelEnabled` 改「默认全纳入、显式关闭」。** |
| B | 保留配置白名单 + 两处 map，每新增渠道手动改 4 处（现状）。 |
| C | 线索发掘完全不设渠道闸门，所有 MessageHub 一律判定。 |

**推荐 A**。理由：现状四套事实源（§4.1）已造成「注册了但线索发掘静默跳过」的结构性风险，且出站已用同构注册表成功插件化（`outbound_registry.go`、`registry.go:196-201`）；parity 门可把漏登记变红灯。B 随渠道数线性增加回归面；C 会把 experimental/无扩展适配器的渠道也判定，产生伪线索。
**失败信号**：① 新增一个 `channelgw.Default` 渠道后，未改任何线索代码即被发现漏判/误判；② registry 投影与两处 map 在测试中不一致；③ `cfg.Channels` 非空时新渠道被静默排除的客诉 ≥1 起；④ 实验性渠道（Experimental=true）产生大量伪线索且无降级标注。

### D5 出站发送器（是否继续插件化）

| 选项 | 说明 |
|---|---|
| A ★ | **保持并推广 `RegisterChannelSender` + `bridgeOutboxSender` 能力位回退**（已落地，`outbound_registry.go:39-125`）；新渠道只加工厂 + registry spec。 |
| B | 回到单一 `sendOutbound` 大 switch（旧文档 `CHATWOOT-...:200-224` 已指出痛点）。 |

**推荐 A**（已是既成事实，仅需防回归）。理由：Chatwoot 官方 `CHANNEL_SERVICES` + 模板方法（§1.1）与本仓注册表同构；parity 门已钉住（`outbound_registry_parity_test.go:112-121`）。
**失败信号**：① `sendOutbound` 主体重新超过 150 行或新增 `case` 分支；② 新渠道 sender 未在 `channelcaps` 登记导致出站渲染拒绝（`registry_parity_test.go:301-337` 已覆盖）。

---

## 6. 落地顺序建议（不做代码，仅供排期）

1. **P0**：D4 注册表化（含 parity 门）+ D3 并发 CAS 修复——两者都防止「静默丢数据」。
2. **P1**：D3 摘要接线 + 判定状态机；D1 会话寻址切到身份锚。
3. **P2**：D2 的 `lm:` 平行身份迁移到 `CustomerChannel`、360 纯 OpenID 解析修复；D5 防回归。
4. **不做**：CDP 替换、照搬 Chatwoot Rails 回调链、ReAct 一期上线。

---

## 7. 无法验证 / 待一手复核清单

- Segment 官方识别文档 403（`https://segment.com/docs/connections/identify/tracking-identity/`），本轮未取得一手文本；相关结论未采用。
- Chatwoot `Mailbox::ConversationFinderStrategies`、`Messages::MarkdownRendererService`、`Conversations::UnreadCounts::Store` 仅来自仓内调研文档引用（§1.1、§1.2），本轮未逐行复核官方源码。
- `UpsertFromMessageTx` 是否消费 `CustomerChannel` 身份锚做会话归属：本轮未逐行核对，需在实施 D1 前确认。
- `MergeCustomers` 是否已迁移全部 secondary 关联表（尤其 `customer_tags` / `session_messages` / `rfm`）：本轮未穷举，标 `无法验证`。
- 本轮未运行任何 Go 测试或构建；所有「已落地」判断均来自静态源码与迁移文件。

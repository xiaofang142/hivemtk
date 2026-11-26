# IM 接入接缝对齐（出站注册表 / 事件类型与路由 / 渠道渲染 / 身份键 / 生命周期 / 新平台）

来源：2026-11-17 两轮调研与代码复核——

- `docs/research/LANGBOT-VS-HIVEMTK-PLATFORM-ARCH-2026-11.md`（平台差集 + 接入配方）
- `docs/research/CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md`（聚合链路 + 优化清单）
- `docs/research/LANGBOT-ARCH-DEEPDIVE-2026-11.md`（L0–L6 分层、调用链、4 案例，基线 `langbot@83b6b06`）
- `docs/research/CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md`（五接缝、状态机、4 案例，基线 `chatwoot@73abc1d` / 4.19.1）

本 spec 把两轮调研里**判定为「必须补的接缝」**的条目转成可施工任务；已在 `2f658e7b` 交付的
两项（`internal/channelcaps` 能力表、会话生命周期三列）作为本 spec 的既成前提，不重复施工。

**范围（2026-11-17 决策）**：二次审核后按「接缝 / 平台」拆分——本 spec 只交付接缝（B1–B6），
平台接入（wecomcs / OneBot v11 / Slack / wecombot / custom）见 `specs/025-im-platforms/`，
以本 spec 的 B1（出站注册表）+ B2（事件类型）为交接面。

## 一、判断依据：为什么是这五处，而不是把对方照搬过来

调研得出的结论是：LangBot / Chatwoot 的渠道数能持续扩张，靠的是**接缝**而不是平台代码本身。
把两边的接缝映射到我们的代码后，缺口收敛为五处，每处都有可指认的代码锚点：

| 接缝 | 我们现在的样子（证据） | 后果 |
|---|---|---|
| 出站分发 | `sendOutbound` 单个函数 663 行、switch 8 个分支（`webhook_outbound.go:607-1269`） | 每接一个平台都要改核心文件；分支间共享不了重试/渲染/审计 |
| 事件类型 | `MessageEvent` 没有 `EventType` 字段（`model/message_event.go:1-93`）；`ParsedPayload.EventType` 实际装的是 msg_type（`webhook.go:1006`） | 编辑、撤回、入群、按钮回调全被当作文本消息；LangBot 12 类事件我们只有 1 类语义 |
| 事件路由 | `shouldTriggerAI` 按渠道硬编码 + default 返回 true（`webhook_ai.go:63`） | 「哪个事件交给谁」不可配置，企业场景无法按群/关键词分流 |
| 渠道渲染 | 渲染逻辑分散在三个地方：`bridge_outbound_markdown.go`、`webhook_outbound.go` 的 htmlEscape*、`buildTelegramGroupReply` | 同一条 AI 回复在不同渠道格式不一致；新增渠道必然再抄一份 |
| 身份键 | `CustomerChannel` 唯一键 =`(one_id, channel)`，`account_id` 不在键内（`model/customer_channel.go`） | 同一个人对同渠道的两个账号（两个企微主体/两个 TG bot）会互相覆盖 |

生命周期（`waiting_since/first_reply_at/last_activity_at`）已在 `2f658e7b` 落地，但只做了**写**，
没有做**读**（无「待首响」筛选、无首响时长指标、无 snoozed 状态），故本 spec 只补读侧与状态机收口。

## 二、决策与边界

### D1 出站：注册表 + 每渠道一个 sender，`sendOutbound` 降为 150 行以内

照 Chatwoot `SendReplyJob::CHANNEL_SERVICES` + `Base::SendOnChannelService` 的形状，但**不照搬它的
类继承**：Go 侧用接口 + 注册表。

```go
type OutboundRequest struct {
    Channel   WebhookChannel
    AccountID string
    Payload   *ParsedPayload   // 现职：p.Sender 是收件人、p.EventID 是去重键
    Content   string
    Cards     []model.RichCard
    Hub       *model.MessageHub
    // Fail / FailUntracked 是 sendOutbound 的 markSendFailed / markSendFailedLogged 原件。
    // 必须由调用方注入、sender 在失败现场调用：sendErr 由闭包回写，defer 里的
    // 「可重试则入 send_retry」才拿得到值；且飞书「正文失败仍继续发卡片」的
    // 「先记失败、后补成功」顺序（webhook_outbound.go:709 起）只有现场回调能保持。
    Fail        func(error)    // 写失败轨迹（带 hubMsg）+ 置 sendErr
    FailUntracked func(error)  // 无轨迹行可归属时的失败（标记「未落库」语义）
}
type OutboundResult struct{ Sent bool; PlatformMsgID string }
type channelSender interface {
    Channel() WebhookChannel
    Send(ctx context.Context, req *OutboundRequest) *OutboundResult
}
func RegisterChannelSender(s channelSender)   // 重复注册 panic，空 Channel panic
func ChannelSenderOf(ch WebhookChannel) (channelSender, bool)
func RegisteredSenders() []channelSender      // 供文档生成与 parity 测试
```

`Send` 不返回 error，是**刻意的**：今天的失败口径不是「返回值」而是 `markSendFailed` 的副作用
（`sendErr` + `outboundSendFailed` 写库/告警），返回 error 会造出第二条真值来源。sender 内部照原样
调 `req.Fail(...)` / `req.FailUntracked(...)`，`Sent` 等价今天的命名返回值。

`sendOutbound` 保留的**只有横切逻辑**（这段是各渠道真正共享的，不能下沉）：
静默时段延迟入队 → `ClaimReply` 去重 → 超时上下文（`utils.MediumTimeout`）→ 富卡降级成文本
（`outboundSupportsRichCards` + `cardTextDelivery`，影响后续所有分支 + `cardsFallbackCount` 记账）
→ bridge 族 Markdown 清洗（见下）→ 查 sender → 分发 → defer（重试入队 / `ReleaseReply` /
`ReleaseAIProcessingFlag` / `RecheckUnrepliedAndTrigger`）。

横切段里有两处**必须随本轮一起搬位**（逐行复核发现，见 §七 C1/C2）：
- 富卡降级与 `cardsFallbackCount` 今天在 switch **之前**，是跨渠道的，留在 `sendOutbound`，
  通过 `req.Cards` 与一个返回值/字段把 `cardsFallbackCount` 交给 bridge sender 记进 `Extra`。
- bridge 风格的 Markdown 清洗今天在 default 分支**内部**，但它改写的是 `content` 变量本身——
  defer 里的 `enqueueSendRetry(ctx, ..., content, ...)` 捕获的是同一个变量，所以「重试投的是清洗后的文本」。
  搬进 sender 会让重试拿到未清洗文本（行为漂移）。因此清洗上移到 `sendOutbound` 横切段，
  条件写成 `bridgeOutboxCapable(channel) && stripMarkdownForBridgeChannel(...)`：
  对非 bridge 渠道与今天的「drift 渠道直接失败、不清洗」完全等价。

决策细节：
- **纯搬迁不改行为**：8 个 case 的代码原样移入 `outbound_sender_<channel>.go`，包括 Telegram 的群回复
  增强、QQ 的被动窗口 `msg_seq`、钉钉的 URL 白名单。分支内出现的 `s.integrations.*` 通过依赖注入口
  （sender struct 持 service 指针）获得，不引入新单例。
- **bridge 分支也是 sender**（`bridgeOutboxSender`），它的 `duplicateOccupied`/`undeliverable` 语义原样保留。
- **失败即拒绝**：未知渠道 → `ErrOutboundChannelUnsupported`，不再静默走 bridge 兜底（今天的 default
  分支是「非 bridge 渠道打印 drift 然后失败」，语义上等价，但改成显式错误码便于告警）。
- **结果必须自洽（二次审核补）**：登记表在 `Send` 返回后校验
  `if !res.Sent && sendErr == nil → req.Fail(&ChannelError{Category: CategoryUnknown, Retryable: true, Raw: "sender reported neither success nor failure"})`。
  今天失败全靠 `markSendFailed` 副作用（企微每条早退路径都是先 `markSendFailed` 再 `return`，
  `webhook_outbound.go:685-706`），搬迁后漏调 `Fail` 就会造出「既没发也没错」的静默丢消息。
  配套 `TestSenderOutcomeMustBeExplicit`，并约定 `Fail` 必须在 `Send` 返回前**同步**调用（`-race` 下跑）。
- **两张注册表（二次审核补）**：sender 依赖 `WebhookService` 的集成单例与仓储（QQ 的 `msg_seq` 计数器必须跨调用存活），
  若只在 `NewWebhookService` 里逐实例注册，`RegisteredSenders()` 会随装配顺序漂移，parity 测试与文档生成失去稳定基准。
  故：静态工厂表 `var senderFactories = map[WebhookChannel]func(*WebhookService) channelSender`（编译期可枚举）
  \+ 实例注册表（`NewWebhookService` 遍历工厂表构建，供热路径 `ChannelSenderOf`），两表键集由同一测试校验相等。
- **parity 门禁**：`webhookInboundCapable` 里的每个渠道 + `channelgw.Default` 的每个渠道，都必须能查到
  sender，否则测试红。这条门禁替代「靠人记得改 switch」。

### D2 事件类型：加字段 + 按类型分流，编辑/撤回/成员事件不再伪装成文本

LangBot 的事件分类法取**与我们收件箱语义兼容的子集**（不做它的 `feedback.received`、`platform.specific`
落库语义）：

```go
EventTypeMessageReceived     = "message.received"     // 默认值（空串归一）
EventTypeMessageEdited       = "message.edited"
EventTypeMessageDeleted      = "message.deleted"
EventTypeMessageReaction     = "message.reaction"
EventTypeGroupMemberJoined   = "group.member_joined"
EventTypeGroupMemberLeft     = "group.member_left"
EventTypeGroupMemberBanned   = "group.member_banned"
EventTypeBotInvited          = "bot.invited_to_group"
EventTypeBotRemoved          = "bot.removed_from_group"
EventTypeInteractionCallback = "interaction.callback"
```

- `MessageEvent.EventType` 新增；`NormalizeEvent` 在空值时归一为 `message.received`（存量调用零改动）。
- **不新增 `MessageHub.event_type` 列**：`message_hub` 只承载「消息」语义；编辑走
  `UpdateContentByChannelMsgID`（新增窄方法），入站撤回走 `MarkInboundRecalledByChannelMsgID`
  （列复用 `RecalledAt/RecallError`，但**不能复用** `MarkRecalled`，理由见下一条），
  成员/机器人事件**不落消息表**，但**同步写审计表**（见下条「事件出口」）。
- 撤回必须区分方向：现有 `repository.MarkRecalled(id, at)` 的 WHERE 带 `direction='outbound'`
  （`message_hub_recall.go:29`，服务「我方撤回自己发的消息」），**不能拿来标客户的入站撤回**。
  入站撤回在本轮新增 `MarkInboundRecalledByChannelMsgID`（或把 `MarkRecalled` 的方向条件参数化），
  `RecallError` 复用为原因承载列，`Extra.recall_source` 记为 `platform`。
- 落库现状（逐行复核）：**今天没有任何渠道解析撤回事件**——飞书解析器遇到 `Event.Message == nil`
  直接 `return nil,nil`（`webhook_channel_feishu.go:196`），Telegram 只处理了 edited（L268）。
  所以 D2 交付的是**中枢能力**（归一 + 分流 API + 测试），逐渠道接线放在各平台任务里，不在 T202 一口气全接。
- 只有 `message.received` 触发 AI 回复（`shouldTriggerAI` 前置 `eventType == received` 守卫）。
  `interaction.callback`：一期只做「**平台空 ack + 审计 + 路由留痕**」，不做表单状态机。
  理由：真答复需要 `callback_query.id` / `response_code` 这类交互上下文，`MessageEvent` 目前不带，
  引入 `channel_interactions` 表之后再谈改卡片/回填（已登记「需证据触发」）。
- **事件出口（二次审核修正）**：进程内总线是 best-effort 且队列满即丢（`internal/event/bus.go:1-17`，
  只有 `customer.message.received` 进 critical 队列），**不能作为需要留证的事件的唯一出口**。
  故 `group.member_*` / `bot.*` / `interaction.callback` 一律**同步写审计表**（复用现成
  `operation_log` / `security_audit`，幂等键 `(tenant_id, channel, channel_msg_id)` 唯一，防平台重投重复记账），
  总线只做旁路通知；纯通知类事件才允许「只发总线」，且必须显式登记为可丢。
- `channelcaps.Manifest` 增加 `Events []string`，声明每渠道能产出哪些事件；测试校验
  「已声明的事件 ⊆ 已知事件常量」+「渠道实现文件里出现的事件在 manifest 有声明」。

### D3 路由：规则表 + 匹配引擎，默认行为不变

- 新表 `channel_route_rules`：`tenant_id, channel, account_id, event_type(通配), match(JSONB), target(pipeline|agent|human|discard), target_ref, priority, enabled, note`。
- 模型必须同时进 `user-server/internal/pkg/db/migrate.go` 的 `allModels()`，否则新库不建表
  （`scripts/check_model_migration.py` 以此为判据，未登记即红）。
- 匹配语义对齐 LangBot：事件类型通配（`message.*`）、`Extra` 嵌套路径等值/包含（`_get_nested_value` 的等价物）。
- **默认放行**：无命中规则 → 走现状（`shouldTriggerAI` 判定）。**这是关键**——规则表是增量能力，
  不清空任何存量行为。
- 命中结果记入 `IngressResult.RouteRuleID`（该 struct 现只有 `Accepted/HumanLocked/QueuedForAI/SessionID/Reason`，
  要加字段）并打日志，便于「为什么这单没回」可追。
- 管理接口按仓库惯例：新建 `internal/router/channel_route_routes.go` 的 `setupChannelRouteRoutes(auth, ...)`，
  写操作挂 `middleware.RequirePermission("channel_routes.write")`，并在 `router.go` 的 setup 序列里注册。
  新路由必须过 `route_policy_gate_test.go`（未挂守卫且不在匿名清单 → 红）与
  `cross_tenant_scan_test.go` 的租户绑定清单。
- 不做 LangBot 的 pipeline 编排器：我们是单 agent 编排，路由只回答「给谁」，不回答「怎么处理」。

### D4 渲染：一处 `RenderForChannel`，能力表驱动

`channelcaps.Manifest` 增加 `OutboundFormat`（`plain|markdown|html`）与 `MaxTextLen int`；
新增 `service/channel_render.go`：

```go
func RenderForChannel(ch WebhookChannel, msgType string, content string, cards []model.RichCard) (RenderedOutbound, error)
type RenderedOutbound struct{ Text string; Cards []model.RichCard; Chunks []string }
```

规则：`plain` 渠道去 Markdown；无 `FeatureRichCard` 的渠道把卡片降级为文本；**不切块**。
`MaxTextLen` 的语义是「超过即打点 `outbound_text_overflow{channel}`」，长度处理仍留在各渠道客户端——
QQ 客户端已有 `splitQQMessage` + 可注入的 `QQMessageMaxLenEffective()`（`channelbot/qq/qq.go:38-66,195`），
在渲染层再切一次会造成双重切分、块数不可预期，并直接违反 FR006 的「搬迁前后调用序列一致」。
（LangBot 的 4000 字截断发生在**适配器**里，不是渲染层；我们对应的是渠道客户端，层级一致。）
现有 `outboundSupportsRichCards` / `stripMarkdownForBridgeChannel` / `htmlEscape*` 的调用点改由本函数统一，
但**保留旧函数签名作为薄包装**，避免一次改爆调用方（`channel_card_delivery.go` 仍是未提交在制品，本 spec 不碰它）。
每个渠道的 `OutboundFormat/MaxTextLen` 必须附「代码取证锚点」（该 sender 里处理换行/转义/HTML 的那几行），
不得凭印象填 `markdown`——否则等于给能力表再造一个漂移源。

### D5 身份：双层唯一键（身份锚 + 业务键），已定方案 B

- **机制先对齐现实（二次审核修正）**：`tenancyidx.Normalize` 会把租户表上的唯一索引**原位重建**为
  `(tenant_id, 原列…)`，并保留 `WHERE` 谓词（`internal/pkg/tenancyidx/tenancyidx.go:1-26,212,253`），
  所以今天 `customer_channels` 的实际唯一键是 **`(tenant_id, one_id, channel)`**，不是 `(one_id, channel)`。
- **决策（2026-11-17 确认，方案 B）**：一条唯一键无法同时表达两种「唯一」，所以建**两条**：
  | 键 | 列 | 语义 | 名称 |
  |---|---|---|---|
  | 身份锚 | `(tenant_id, channel, account_id, channel_user_id)` | 「这个平台身份属于谁」——同一账号下的同一平台用户只能有一条绑定 | `idx_channel_identity` |
  | 业务键 | `(tenant_id, one_id, channel, account_id)` | 「这个客户在这个账号下有哪些绑定」——同一客户在同一账号下不重复 | `idx_oneid_channel`（沿用原名） |
  - 身份锚对齐 Chatwoot 的 `(inbox_id, source_id)`：先按身份找绑定，找到就**沿用它的 OneID**，不再让新 OneID 覆盖
    （今天 `UpsertCustomerChannel` 只按 `(one_id, channel)` 定位，同一 `channel_user_id` 先后落到两个 OneID 就会写出两行、
    指向两个客户）。
  - 业务键保留「一客户一账号一行」的既有假设，前端/统计的 `(one_id, channel)` 查询不动；把 `account_id` 补进键，
    解决「同一个人对同渠道两个账号互相覆盖」。
  - `channel_user_id = ''` 的历史行不参与身份锚：索引带 `WHERE channel_user_id <> ''`（部分索引，
    `tenancyidx.Normalize` 会保留谓词），否则所有空值行会撞同一条唯一键。
- **写入顺序（Chatwoot 同款）**：先查身份锚 → 命中则复用其 `one_id`（并刷新 `last_seen_at`）→ 未命中再按业务键查 →
  都没有才插入。`ON CONFLICT` 只作为并发兜底（两个并发插入同一身份，后到者被唯一键拒绝后退回查一次）。
- **三处必须同改，少一处就报错或无效**：
  1. model tag：`AccountID` 加入 `idx_oneid_channel`；新增 `uniqueIndex:idx_channel_identity`（含 `where`）；
  2. `repository/channel_overview.go:158-169` 的 `ON CONFLICT` 目标列加 `account_id`，并改为「先查后写」两段式，
     否则写入直接报 `no unique or exclusion constraint matching the ON CONFLICT specification`；
  3. 迁移 065 用**换名法**：先各自去重（按 `last_seen_at DESC` 保留最新、其余置 `deleted_at`），
     再 `CREATE UNIQUE INDEX CONCURRENTLY ..._v2` → `DROP INDEX CONCURRENTLY` → `ALTER INDEX ... RENAME TO` 原名。
     不用「DROP 后交 AutoMigrate 重建」：AutoMigrate 建索引是**非并发**的，大表会锁写；
     换名法同时保证索引名稳定（AutoMigrate 按名判存在）且显式带 `tenant_id`（`Normalize` 复查幂等）。
- **两条索引都带 `WHERE deleted_at IS NULL`**（身份锚另加 `channel_user_id <> ''`）：软删行不再占键，
  「删了再绑同一身份」不会永久失败；写入改为「unscoped 查身份锚 → 命中则复活并沿用旧 OneID → 否则按业务键 →
  都没有才插入」，`ON CONFLICT` 只作并发兜底。身份锚冲突但 OneID 不同时**不覆盖、不复活**，只记 `operation_log`
  供人工合并——避免一次自动合并把两个客户的对话历史连到一起。
- `account_id` 空值统一存 `''`（不用 NULL，避免唯一索引失效）。
- 不加 `MessageHub.origin` 列：出站回写 `platform_msg_id` + 入站同 msg_id 幂等已覆盖回声场景，
  再加一列是重复状态源（Chatwoot 的 `source_id.present?` 判定在我们这里是 `msg_id` 唯一索引）。
- 存量数据风险：历史行 `account_id` 可能为空且 `channel_user_id` 可能重复（bridge 渠道早期版本）——迁移去重会**软删**重复行，
  必须在 065 里输出被软删行数，并在 `delivery.md` 留执行记录；软删可回滚（`deleted_at = NULL`）。

### D6 生命周期读侧收口

- 状态机补 `snoozed`（带 `snoozed_until`），与现有 `unread/open/assigned/closed` 并存；
  到期由 worker 重置为 `open`（不引入 cron 框架，复用现有延迟任务机制）。
- `reopen_count` 列：`closed → unread` 自增（今天 L332-336 重开但无计数）。
- 新增只读接口：`GET /api/inbox-conversations?filter=awaiting_first_reply`（`waiting_since IS NOT NULL AND first_reply_at IS NULL`，按 `waiting_since ASC`）。
- 新增指标：首响时长 p50/p95（按渠道/租户/时间窗），只读聚合、不改消息链路。

### D7 平台接入：已拆分为独立 spec `025-im-platforms`

二次审核判定「62 任务 / 30–34 人日混在一个 spec 里」不可交付（review.md R1/R13），已按决策拆分：

- **024（本 spec）** 只交付**接缝**：出站注册表、事件类型、路由、渲染、身份双层键、生命周期读侧、交互回调上下文、门禁。
  完工判据不含任何平台上线。
- **025** 交付平台：企业微信客服 wecomcs → OneBot v11 → Slack → 企业微信智能机器人 → 通用 custom 入口，
  按批次独立发布；平台任务的 FR 与逐任务规格见 `specs/025-im-platforms/{spec.md,tasks.md}`。
- 平台顺序的依据（`WeComAccount` 无 `kf_*` 字段、`qq_accounts` 不能承载 OneBot `self_id` 等）随 FR 一并迁到 025，
  本 spec 不再复述，避免两处漂移。

明确不做（Deviations，仍由本 spec 记录，025 沿用）：
- Discord / LINE / KOOK / Matrix：接缝就绪后按需求排期，不在 024/025。
- `wechatpad`（iPad 协议）/ `openclaw_weixin`：合规与稳定性风险，不做。
- Chatwoot 式 polymorphic `Inbox` 实体重构：成本高、收益需证据，登记「需证据触发」（见 §四）。
- Redis 外置未读计数、消息模板全量体系：同上，需压测/运营证据触发。
- 消息翻译、富文本编辑器、CSAT 问卷：不做。

### D8 交互回调：建 `channel_interactions`，本期就支持真答复

二次审核 R7 指出「空 ack + 审计」不是真支持：平台答复回调需要 `callback_id` + 响应码，而 `MessageEvent` 不带上下文。
用户决策（2026-11-17）：**本期建表，支持真实答复**，不降级。

- 新表 `channel_interactions`：唯一键 `(tenant_id, channel, interaction_id)`，承载「我们发出的卡片 ↔ 客户点击 ↔ 答复状态」
  （pending / answered / expired），字段与迁移号见 tasks T208（迁移 `068_channel_interactions.sql`）。
- 入站：`EventType == interaction.callback` 一律**不落 `message_hub`、不计未读、不触发 AI**，改为
  写交互行（幂等）+ 审计（`operation_log`，平台重投不产生重复审计）+ 路由留痕，然后回 ack。
- 答复能力放在协议层：`channelbot/core` 增 `InteractionAnswerer` 接口，Telegram 先实现 `answerCallbackQuery`；
  未实现该接口的渠道退化为「空 ack + 审计」，并在 `channelcaps` 标注 `FeatureInteraction`，
  **不允许**在 Service 层写 `switch channel` 判断谁支持。
- 出站：发可交互卡片时登记 `platform_msg_id` + 动作集合，让点击能定位到卡片；到期由 worker 置 `expired`
  （沿用 `retention_cleanup_cron.go` 的 Ticker + `utils.SafeGo` 形态）。
- 边界：不做多步表单状态机、不做卡片内容回改（edit card）——那是后续能力；本期只保证「点击有答复、事后可追」。

## 三、要求

### FR-1 出站接缝

- FR001 `internal/service/outbound_registry.go` 提供 `channelSender` 接口与注册表；重复注册 panic；
  `OutboundRequest` 携带 `Fail/FailUntracked` 两个失败回调（保持 `sendErr` 单一真值来源与「先记失败、后发卡片」顺序）。
- FR002 现有出站分支各一个 `outbound_sender_<channel>.go`，代码从 `webhook_outbound.go` 原样搬迁，
  函数体行为不变（含 Telegram 群回复、QQ `msg_seq` 惰性单例、WhatsApp 24h 窗口降级模板、钉钉 URL 白名单、
  bridge duplicate/undeliverable 语义）。分支清单以代码为准：`ChannelWeCom/Feishu/Telegram/QQ/Whatsapp/DingTalk/Wechat`
  7 个 `case` + 1 个 `default`（bridge 族），故 sender 共 8 个。
- FR003 `sendOutbound` 降到 ≤150 行，且不再出现 `switch channel`。
- FR004 未知渠道 → `ErrOutboundChannelUnsupported`（可判定错误，非静默失败）。
- FR005 门禁：`TestEveryInboundChannelHasSender` + **扩写既有** `TestBridgeOutboxParityWithWebhookOutbound`
  （该测试已存在，位于 webhook 出站测试文件；不要新建重复门禁）。
- FR006 行为冻结：搬迁前后同一组 (channel, payload) 的集成调用序列一致（用 fake integration 记录调用）；
  bridge 分支额外钉住「重试入队用的是清洗后的 Markdown」（清洗上移后仍成立）。

### FR-2 事件类型

- FR010 `model.EventType*` 常量 + `MessageEvent.EventType`；空值归一 `message.received`。
- FR011 `HandleIngressMessage` 按事件类型分流：`edited → UpdateContentByChannelMsgID`、
  `deleted → 入站撤回专用方法`（现 `MarkRecalled` 带 `direction='outbound'`，不能直接复用）、
  `group.member_* / bot.* → 事件总线 + 审计（不落消息表）`、`interaction.callback → 答复 + 审计`。
- FR012 非 `message.received` 事件一律不触发 AI 回复，且不增加 `unread_count`。
- FR013 各渠道入站解析器填充 `EventType`：telegram（edited 已解析，L268；deleted/reaction 新增）、
  feishu（现 `Event.Message == nil` 即返回 nil，撤回/成员事件需新增分支）、wecom、
  whatsapp（status 回调不当作 message）、qq、以及新接入平台。
- FR014 `channelcaps.Manifest.Events` + 声明/实现一致性测试。
- FR015 注意 `ParsedPayload.EventType` 不是事件类型来源：`webhook.go:1006` 把
  `event_type/event/type/msg_type` 一并 coalesce，语义混杂（多数渠道它等于消息类型）。
  事件类型只能由各渠道解析器显式赋值到 `MessageEvent.EventType`。

- FR016 `model.ChannelInteraction` + 仓储（`Create` 幂等 / `MarkAnswered` / `MarkExpired`）+ 迁移 068，
  模型登记 `allModels()`。
- FR017 `HandleIngressMessage` 对 `interaction.callback` 分流：不落消息表、不计未读、不触发 AI；
  写交互行（幂等）+ 审计 + `RouteRuleID` 留痕。
- FR018 协议层 `InteractionAnswerer` 接口 + Telegram `answerCallbackQuery` 实现；未实现渠道退化为空 ack + 审计，
  能力在 `channelcaps` 标 `FeatureInteraction`。
- FR019 出站发卡片登记交互上下文（`platform_msg_id` + 动作集合）与到期清理 worker（`expires_at`）。

### FR-3 路由

- FR020 `model.ChannelRouteRule` + 仓储 + 迁移 066。
- FR021 `service/route_engine.go`：通配事件类型 + `Extra` 嵌套匹配 + 优先级；无命中返回 `nil`。
- FR022 接线点：`HandleIngressMessage` 归一后、AI 触发前；命中 `discard` 时不入库不回复（仍记审计）。
- FR023 只读/写入 API（tenant 作用域），规则变更审计。
- FR024 `IngressResult.RouteRuleID` + 结构化日志 `route_rule_hit`。

### FR-4 渲染

- FR030 `channelcaps` 增 `OutboundFormat`、`MaxTextLen`（21 条 manifest 全量补齐，每条附「代码取证锚点」，
  测试防漂移；不附锚点视为未完成）。
- FR031 `RenderForChannel` + 单测（plain 去 MD、卡片降级、渠道未知即错）；**不切块**，
  超 `MaxTextLen` 只打点 `outbound_text_overflow`（长度处理留在渠道客户端，见 D4）。
- FR032 老函数改薄包装，调用方零改动；`bridge_outbound_markdown_test.go` 全绿。

### FR-5 身份

- FR040 迁移 065：建**两条**唯一键（身份锚 `idx_channel_identity` + 业务键 `idx_oneid_channel`），
  各自先去重再换名，幂等可重跑，输出被软删行数。
- FR041 `UpsertCustomerChannel` 改为「先查身份锚 → 再查业务键 → 才插入」；身份锚命中时沿用既有 OneID，
  不覆盖；并发冲突用 `ON CONFLICT` 兜底并重查一次。
- FR042 测试：同一 one_id 在两账号下两条记录并存；同账号重复 upsert 不新增行；同一 `channel_user_id`
  落到新 OneID 时被身份锚拦下（旧 OneID 不变 + 记 `operation_log`）；软删行被复活。

### FR-6 生命周期读侧

- FR050 `snoozed` 状态 + `snoozed_until` + 到期重置 worker + 迁移 067。
- FR051 `reopen_count` 列 + 重开自增（迁移 067 同批）。
- FR052 `filter=awaiting_first_reply` 查询（索引已存在：`waiting_since`）。
- FR053 首响时长聚合接口 + 单测（含无首响样本不计入）。

### FR-7 平台接入（已迁至 `025-im-platforms`）

FR060（wecomcs）/ FR061（OneBot v11）/ FR062（Slack）/ FR063（wecombot）/ FR064（能力声明一致性）/
FR070（custom 入站）/ FR071（custom 出站）连同「隐性必做项」表、触点矩阵与逐任务规格，全部见
`specs/025-im-platforms/`。本 spec 只保留一条跨平台约束：

- FR064 任何新增平台在 `channelcaps` 声明的事件/消息能力必须与实际实现一致，由 `TestCapabilityDocFreshness`
  与 025 每平台的一致性测试钉死；本 spec 交付的 `Manifest.Events` 字段（FR014）是该声明的载体。

## 四、需证据触发（不进本 spec 的施工清单，登记备查）

| 项 | 触发条件 |
|---|---|
| Chatwoot 式 `Inbox` 实体（渠道实例 + 策略容器） | 出现「同一平台多套策略并存」的真实需求（如两个企微主体不同欢迎语/工作时间） |
| Redis 外置未读计数 | `inbox_conversations` 聚合查询在压测中 p95 > 200ms 或成为慢查询 TOP3 |
| 消息模板体系（问候/离线/CSAT/自动解决） | 运营侧提出模板需求，且当前 `aiReplyQuietHours` 已不足以覆盖 |
| 消息翻译 | 跨境客户实际诉求（当前无） |
| 每渠道 typing／正在输入（Chatwoot P2-4；现状仅 webchat 访客侧 `ai_typing` + `typing_predict` 意图预测，渠道级无 sendChatAction）| 运营/客户提出「正在输入」体感诉求，且目标渠道协议支持（TG `sendChatAction` 等）（2026-11-18 补登） |
| 出站流式四细节（LangBot deepdive 标「值得抄」：附件终态不能编辑进流、超长 4000 字截断重试一次、群聊流式无可见内容删占位、`is_final` 投递失败回落一次非流式）| 出站流式渲染立项时随任务带入，不单独立项（2026-11-18 补登） |
| 邮件渠道会话归属可插拔（Chatwoot：邮件按 `In-Reply-To` → `References` header 归并，不能按人）| 立项「邮件双向会话」渠道时必做（2026-11-18 补登） |
| 入站聚合窗口（Chatwoot P2-1；2026-11-18 对照评估**不立项**——`ai_debounce` 即该机制：硬顶 30s/20 条、参数中心可配默认 12s、连发合并一批应答）| 连发合并粒度被运营证实不足（逐条轰炸再现）时重开 |

## 五、验收总纲

1. 每阶段独立可交付、可回滚；任一阶段失败不阻塞后续（除 FR-1（出站注册表）与 FR-2（事件类型）是 `025-im-platforms` 的前置）。
2. 每条 FR 至少一个可执行判据（测试名或命令），写在 `tasks.md` 对应任务的「验收」字段。
3. 全量门禁：`go build ./... && go vet ./... && go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ ./internal/channelgw/...`；
   **并发面另跑 `go test -race ./internal/service/ ./internal/channelbot/...`**（注册表 / snooze worker / 交互清理 worker）；
   `python3 scripts/check-layer-boundary.py`、`python3 scripts/check_model_migration.py`、
   `bash scripts/check-enum-consistency.sh`（新增渠道必跑，见 §七 门禁矩阵）。
4. 平台验收口径见 `025-im-platforms` §五（验签失败 4xx 且不落库、同 msg_id 二次投递不重复、私聊与群聊各一条回放、
   `*ChannelError` 分类正确）；本 spec 只要求接缝自身的门禁（FR005/FR006、T1201–T1205）。
5. 现有行为零回归：`TestCardDelivery`、bridge outbox 全量测试、telegram/whatsapp 入站测试全绿。
6. 可观测：按 `(channel, ChannelError.Category)` 计数打点（`outbound_send_failed_total`）+ 路由命中
   （`route_rule_hit` 日志已有，需计数）+ 超长（`outbound_text_overflow`），各配一条单测；
   否则上线后只能靠翻审计行判断某个渠道是否整体挂了。

## 六、风险

| 风险 | 缓解 |
|---|---|
| 搬迁 sender 引入隐性行为差异 | 每个 case 搬迁前补一条 fake-integration 断言，先红后绿；一渠道一 commit |
| 事件类型分流改坏现有入站 | `message.received` 走原路径，其余类型为新增分支；存量测试不改断言 |
| 迁移 065 换唯一键在大表上锁表 | 先建新索引（`CREATE INDEX CONCURRENTLY` 语义需手写迁移），再删旧索引；去重放事务外 |
| 新平台回调验签与已有 `Verify` 冲突 | 每个平台独立 `webhook_channel_<x>.go` 的 `Verify<X>`，`Verify` 只做 switch 分发 |
| 规则表引入后「为什么没回」变难查 | 强制 `route_rule_hit` 日志 + `IngressResult.RouteRuleID` |

## 七、逐行复核（2026-11-17，对着 `user-server` 源码逐条验）

复核方法：每条断言回到源码定位函数与行号，能用 `grep`/`awk` 判定的当场判定；不能就地判定的（如
「飞书是否解析撤回」）读解析器全文确认。下表只列**改动了结论**的项，原文中已正确的锚点不重复。

> 二次审核（换镜头找漏洞，不重复本节断言核对）见同目录 `review.md`（R1–R17）。
> 其中已直接改进本文档与 `tasks.md`／`plan.md` 的项：R2 索引机制、R3 事件出口、R4 渲染不切块、
> R5 sender 结果不变量、R6 双注册表、R8 游标落库、R9 WS 宿主与鉴权、R12 片段自相矛盾、
> R13 工期、R14 `-race`、R15 可观测。
> **三项 ⚖️ 已于 2026-11-17 决策**：R1 → 拆 024（接缝）/ 025（平台）；R2 → 双层键方案 B（D5）；
> R7 → 建 `channel_interactions` 支持真答复（D8），不再降级为空 ack。

### 7.1 断言修正（文档原话 → 代码事实 → 处置）

| # | 原断言 | 代码事实（实证） | 处置 |
|---|---|---|---|
| C1 | `sendOutbound` 有「8 个 case」 | `grep 'case Channel'` 只有 7 个（685/709/748/792/818/870/951），bridge 走 `default`，共 8 条出站路径 | FR002/D1 改写为「7 case + 1 default」 |
| C2 | 失败语义可用 `Send` 返回 error 表达 | 真值源是 `markSendFailed`/`markSendFailedLogged` 的副作用（写 `sendErr` + `outboundSendFailed` 落库/告警）；飞书分支「正文失败 → 仍发卡片 → 最终 `sent=true`」，顺序不可交换 | `OutboundRequest` 加 `Fail/FailUntracked` 回调，`Send` 不返回 error |
| C3 | 富卡降级在 switch 内 | 在 switch **之前**（`webhook_outbound.go:667` 起），且 `cardsFallbackCount` 被 bridge 分支写进 `Extra` | 明确留在横切段，经 `req.Cards` + 计数交给 bridge sender |
| C4 | Markdown 清洗属 bridge sender | 在 `default` 分支内 `content = StripMarkdownForDM(content)`；defer 里 `enqueueSendRetry(..., content, ...)` 捕获同一变量 ⇒ 重试投清洗后文本 | 清洗上移到横切段（`bridgeOutboxCapable && stripMarkdownForBridgeChannel`），sender 不再改 content |
| C5 | 前置只有「静默时段/ClaimReply/超时」 | 还有第一个 defer 里的 `agent_runtime.ReleaseReply(p.EventID)`（`!sent` 时释放），以及收尾 defer 的 `ReleaseAIProcessingFlag` + `SafeGo(RecheckUnrepliedAndTrigger)` | D1 横切清单补齐；搬迁时两个 defer 必须原样留在 `sendOutbound` |
| C6 | WhatsApp sender 只是 Cloud API 调一次 | 先 `ensureReposFromDB`，再用 `GetLastInboundByConversation` 判 24h 客服窗口；超窗降级 `SendTemplateMessage`（模板名来自参数中心 `channelbot.whatsapp_fallback_template`），未配置则 `CategoryWindowExpired` 失败 | T106 按真实逻辑搬迁；sender 需持 `svc` 取 repo 与参数 |
| C7 | 钉钉 sender 走 integration | 直接用 `http.DefaultClient` POST `sessionWebhook`（从 `hubMsg.Extra` 取，含 `session_webhook_expired_at` 校验 + `loadDingtalkWebhookHostAllowed` 域名白名单 + 解析 `errcode`） | T107 明确不引入 integration，保留 `http.DefaultClient` 与白名单 |
| C8 | QQ 只是发一条消息 | `s.qqIntegration` 惰性单例有硬理由：`msg_seq` 计数与 access_token 缓存必须跨多次出站存活，每次新建会导致 seq 恒为 1 被平台丢弃 | 注册表必须是进程级单例，sender 只能持 `svc` 指针读 `s.qqIntegration` |
| C9 | `ParsedPayload.EventType` 就是 msg_type | `webhook.go:1006` 把 `event_type/event/type/msg_type` 一并 coalesce，语义混杂 | FR015：事件类型只能由渠道解析器显式赋值到 `MessageEvent.EventType` |
| C10 | 撤回直接复用 `MarkRecalled` | `MarkRecalled(id, at)` 的 WHERE 含 `direction='outbound'`（`message_hub_recall.go:29`），语义是「撤我发出的消息」 | 入站撤回需专用方法；`RecallError` 复用为原因列 |
| C11 | 各渠道已能解析 edited/deleted | `grep -i 'recall|edited'` 在 `webhook_channel_*.go` 只命中 Telegram edited（`webhook_channel_telegram.go:268`）；飞书 `Event.Message == nil` 即 `return nil,nil`（`webhook_channel_feishu.go:196`） | D2 明确「本轮交中枢能力，逐渠道接线在平台任务」 |
| C12 | 新渠道只需登记 8 触点 | 还漏 4 项硬约束：`webhookAccountTableByChannel`（租户反查权威源，漏→多租户歧义 404）、`047_pg_enums.sql` 的 `platform_type_enum`（`check-enum-consistency.sh` 静态对照）、`internal/pkg/db/migrate.go` 的 `allModels()`、`webhookInboundRejectHints` 与一致性测试 | §三 FR-7 增「隐性必做项」表 |
| C13 | wecomcs 复用 `wecom_accounts`（FR060 现属 025） | `model/wecom.go:8` 的 `WeComAccount` 无 `kf_token/kf_aes_key/open_kfid`；且 `ResolveTenantByChannelAccount` 按渠道表反查，两渠道共用一张表会猜 | 新建 `wecom_cs_accounts`，验签复用 `verifyWeCom` |
| C14 | parity 测试要新建 | `TestBridgeOutboxParityWithWebhookOutbound` 已存在（registry ↔ 出站分支双向钉住，见 `default` 分支注释 CH-37b刀3） | FR005 改为「扩写既有测试」 |
| C15 | `IngressResult` 可直接加 `RouteRuleID` | 现状字段仅 `Accepted/HumanLocked/QueuedForAI/SessionID/Reason`（`inbox_ingress.go:129`） | D3 注明需加字段，不是「记入」即可 |
| C16 | custom 只需删拒绝提示（FR070 现属 025） | `controller/webhook_batchc_d03_http_test.go:75` 断言 `/api/webhook/custom/1` 返回「无入站适配器」，`webhookInboundRejectHints` 有 custom 行，`webhookInboundCapable` 与 `Verify` 均无 custom 分支，且**无 `custom_accounts` 表/模型** | FR070 列出四处必改 + 新建账号表并登记 `allModels()` |
| C17 | 迁移号 065 只用于身份键 | 现目录到 `064`；`063_pg_enums_channel_values.sql` 已开「渠道扩容要补 ENUM」的先例 | 编号分配：065 身份键、066 路由规则、067 生命周期读侧、068+ 各平台账号表 |
| C18 | snooze 到期扫描要自建 | 无通用调度器，但存在 `*_cron.go` 形态（`retention_cleanup_cron.go`、`message_trace_cleanup_cron.go`：Ticker + `utils.SafeGo` + 成对启停） | T602 明确新建 `inbox_snooze_cron.go`，且必须过 `check-naked-goroutine.sh` |
| C19 | 会话状态非 pg enum | 状态是 `varchar`，`CASE` 映射与 `closed` 过滤都在 `repository/message_hub_inbox_conversation_query.go` | 加 `snoozed` 不必改 047，但必须同步查询映射 |

### 7.2 复核后仍成立的关键判断

- **出站 663 行 switch 是最大单点**：`sendOutbound` 607–1269，其中 bridge 分支本身约 300 行，且它已经
  依赖 `delayedRepo/inboxConvRepo/GlobalSSEPublisher/tracing/tenant` 六处协作——所以 bridge sender 必须持
  `svc`，不能做成无状态函数。这也是「注册表 + sender 结构体」而非「函数表」的直接原因。
- **事件类型不是「加个字段」**：`MessageEvent` 经 `channelbot.InboundMessage.ToMessageEvent` 与
  `webhook_channel_*.go` 两条路构建，`NormalizeEvent` 是唯一收口点（`inbox_ingress.go:480`），默认值必须加在
  这里，否则三个 channelbot 客户端都要改。
- **身份键缺 `account_id` 是真缺口**：`CustomerChannel` 的 uniqueIndex 只有 `(one_id, channel)`
  （`model/customer_channel.go:22-23`），同一客户在同渠道两个账号下会互相覆盖。

### 7.3 门禁矩阵（新增渠道/新表必过）

| 门禁 | 命令/测试 | 何时会红 |
|---|---|---|
| 分层 | `python3 scripts/check-layer-boundary.py` | sender 里直接 import repository 以外的下层、service 反向依赖 |
| 模型登记 | `python3 scripts/check_model_migration.py` | 新模型没进 `allModels()` |
| ENUM | `bash scripts/check-enum-consistency.sh` | 加了 `model.Channel*` 没同步 `047_pg_enums.sql` |
| 路由守卫 | `route_policy_gate_test.go` / `cross_tenant_scan_test.go` | 新路由没挂守卫或没登记租户绑定 |
| 拒绝提示一致 | `TestWebhookInbound_RejectHintsStayConsistentWithCapability` | 已接入却仍在 hints 里 |
| 能力表 | `TestEveryChannelConstantHasManifest` / `TestCapabilityDocFreshness` | 新渠道没 manifest，或没 `UPDATE_GOLDEN=1` 刷新文档 |
| goroutine | `scripts/check-naked-goroutine.sh` | 异步落库/SSE 发布没用 `utils.SafeGo` |
| 租户写入 | `scripts/check-tenant-ctx-writes.py` | 新写入路径缺租户 |

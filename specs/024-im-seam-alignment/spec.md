# IM 接入接缝对齐（出站注册表 / 事件类型与路由 / 渠道渲染 / 身份键 / 生命周期 / 新平台）

来源：2026-11-17 两轮调研与代码复核——

- `docs/research/LANGBOT-VS-HIVEMTK-PLATFORM-ARCH-2026-11.md`（平台差集 + 接入配方）
- `docs/research/CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md`（聚合链路 + 优化清单）
- `docs/research/LANGBOT-ARCH-DEEPDIVE-2026-11.md`（L0–L6 分层、调用链、4 案例，基线 `langbot@83b6b06`）
- `docs/research/CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md`（五接缝、状态机、4 案例，基线 `chatwoot@73abc1d` / 4.19.1）

本 spec 把两轮调研里**判定为「必须补的接缝」**的条目转成可施工任务；已在 `2f658e7b` 交付的
两项（`internal/channelcaps` 能力表、会话生命周期三列）作为本 spec 的既成前提，不重复施工。

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
  `UpdateContentByChannelMsgID`（新增窄方法），撤回走 `MarkRecalled`（列已存在：`RecalledAt/RecallError`），
  成员/机器人事件**不落消息表**，只发事件总线 + 审计。
- 撤回必须区分方向：现有 `repository.MarkRecalled(id, at)` 的 WHERE 带 `direction='outbound'`
  （`message_hub_recall.go:29`，服务「我方撤回自己发的消息」），**不能拿来标客户的入站撤回**。
  入站撤回在本轮新增 `MarkInboundRecalledByChannelMsgID`（或把 `MarkRecalled` 的方向条件参数化），
  `RecallError` 复用为原因承载列，`Extra.recall_source` 记为 `platform`。
- 落库现状（逐行复核）：**今天没有任何渠道解析撤回事件**——飞书解析器遇到 `Event.Message == nil`
  直接 `return nil,nil`（`webhook_channel_feishu.go:196`），Telegram 只处理了 edited（L268）。
  所以 D2 交付的是**中枢能力**（归一 + 分流 API + 测试），逐渠道接线放在各平台任务里，不在 T202 一口气全接。
- 只有 `message.received` 触发 AI 回复（`shouldTriggerAI` 前置 `eventType == received` 守卫）。
  `interaction.callback`：一期只做「答复 + 审计 + 路由留痕」，不做表单状态机。
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

规则：`plain` 渠道去 Markdown；无 `FeatureRichCard` 的渠道把卡片降级为文本；超长按 `MaxTextLen` 切块
（最多 3 块，超出截断并附省略标记，与 LangBot `Message_too_long` 截 4000 字同口径）。
现有 `outboundSupportsRichCards` / `stripMarkdownForBridgeChannel` / `htmlEscape*` 的调用点改由本函数统一，
但**保留旧函数签名作为薄包装**，避免一次改爆调用方（`channel_card_delivery.go` 仍是未提交在制品，本 spec 不碰它）。

### D5 身份：唯一键补 `account_id`（含去重迁移）

- 迁移 065：先把 `(tenant_id, one_id, channel, account_id)` 重复行按 `last_seen_at` 保留最新、其余软删，
  再 `DROP INDEX idx_oneid_channel` + `CREATE UNIQUE INDEX idx_tenant_oneid_channel_account`。
- `account_id` 空值统一存 `''`（不用 NULL，避免唯一索引失效）。
- 不加 `MessageHub.origin` 列：出站回写 `platform_msg_id` + 入站同 msg_id 幂等已覆盖回声场景，
  再加一列是重复状态源（Chatwoot 的 `source_id.present?` 判定在我们这里是 `msg_id` 唯一索引）。
- 唯一键更换**必须走显式 SQL**：GORM AutoMigrate 只加索引、不删不改已变形索引
  （`repository/tenant_module.go:58` 同口径注释），只改 struct tag 会留下两套索引并存。

### D6 生命周期读侧收口

- 状态机补 `snoozed`（带 `snoozed_until`），与现有 `unread/open/assigned/closed` 并存；
  到期由 worker 重置为 `open`（不引入 cron 框架，复用现有延迟任务机制）。
- `reopen_count` 列：`closed → unread` 自增（今天 L332-336 重开但无计数）。
- 新增只读接口：`GET /api/inbox-conversations?filter=awaiting_first_reply`（`waiting_since IS NOT NULL AND first_reply_at IS NULL`，按 `waiting_since ASC`）。
- 新增指标：首响时长 p50/p95（按渠道/租户/时间窗），只读聚合、不改消息链路。

### D7 平台接入顺序与「不做」

顺序（按「我们已有账号/协议确定性」排，不是按平台热度）：
1. **企业微信客服 wecomcs**——与 `wecom` 共用 corp_id，但**不是同一张表**：`WeComAccount`
   （`model/wecom.go:8`）只有 `CorpSecret/AgentID/AgentSecret`，客服需要 `corp_secret + kf_token + kf_aes_key + open_kfid`。
   本 spec 新建 `wecom_cs_accounts` 表并登记进 `webhookAccountTableByChannel`（租户反查的权威源），
   不复用 `wecom_accounts`——否则 `ResolveTenantByChannelAccount` 会在两张表之间猜。回调验签可复用
   `verifyWeCom(token, aesKey, body, query)`，这是本平台「最短路径」的真实含义。
2. **OneBot v11**——覆盖 QQ 私域最大缺口（我们只有 QQ 官方机器人），HTTP + 反向 WS 两种接入。
3. **Slack**——Events API 验签 + `chat.postMessage`，验证「国外平台 + 线程」形态。
4. **企业微信智能机器人 wecombot**——回调 + 模板卡片事件，最后做（事件类型最多）。

明确不做（Deviations，已在文档登记，不在本 spec 范围）：
- Discord / LINE / KOOK / Matrix：本 spec 只交付接缝，平台排 `025+`。
- `wechatpad`（iPad 协议）/ `openclaw_weixin`：合规与稳定性风险，不做。
- Chatwoot 式 polymorphic `Inbox` 实体重构：成本高、收益需证据，登记「需证据触发」（见 §四）。
- Redis 外置未读计数、消息模板全量体系：同上，需压测/运营证据触发。
- 消息翻译、富文本编辑器、CSAT 问卷：不做。

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

### FR-3 路由

- FR020 `model.ChannelRouteRule` + 仓储 + 迁移 066。
- FR021 `service/route_engine.go`：通配事件类型 + `Extra` 嵌套匹配 + 优先级；无命中返回 `nil`。
- FR022 接线点：`HandleIngressMessage` 归一后、AI 触发前；命中 `discard` 时不入库不回复（仍记审计）。
- FR023 只读/写入 API（tenant 作用域），规则变更审计。
- FR024 `IngressResult.RouteRuleID` + 结构化日志 `route_rule_hit`。

### FR-4 渲染

- FR030 `channelcaps` 增 `OutboundFormat`、`MaxTextLen`（21 条 manifest 全量补齐，测试防漂移）。
- FR031 `RenderForChannel` + 单测（plain 去 MD、卡片降级、超长切块、渠道未知即错）。
- FR032 老函数改薄包装，调用方零改动；`bridge_outbound_markdown_test.go` 全绿。

### FR-5 身份

- FR040 迁移 065（去重 + 换唯一键），幂等可重跑。
- FR041 `UpsertByChannelAccount` 写入路径把 `account_id` 纳入键；并发 upsert 用 `ON CONFLICT`。
- FR042 测试：同一 one_id 在两账号下两条记录并存；同账号重复 upsert 不新增行。

### FR-6 生命周期读侧

- FR050 `snoozed` 状态 + `snoozed_until` + 到期重置 worker + 迁移 067。
- FR051 `reopen_count` 列 + 重开自增（迁移 067 同批）。
- FR052 `filter=awaiting_first_reply` 查询（索引已存在：`waiting_since`）。
- FR053 首响时长聚合接口 + 单测（含无首响样本不计入）。

### FR-7 平台接入（每个平台同一套 8 触点）

每个平台必须交付：`channelbot/<x>` 纯协议客户端（含验签）→ `webhook_channel_<x>.go`（解析 + 事件类型）
→ `webhook.go` 的 capable/dispatch/verify 三处登记 → `outbound_sender_<x>.go` → `channelcaps` manifest
→ 账号表/配置 → 路由 → 测试（fixture 回放）。

**平台接入的「隐性必做项」（逐行复核补出，漏一项就红或静默降级）**：

| 必做项 | 位置 | 漏掉的后果 |
|---|---|---|
| 账号表 + `webhookAccountTableByChannel` 条目 | `webhook.go:393` | 租户反查退到 `integration_accounts` 按平台名找，多租户共用同名平台即歧义 → fail-closed 404，回调全丢 |
| `platform_type_enum` 增枚举值 | `migrations/047_pg_enums.sql` | `scripts/check-enum-consistency.sh` 红（静态文本对照）；离线 SQL 部署库写 `message_hub.platform` 直接报 invalid input |
| 模型登记 `allModels()` | `internal/pkg/db/migrate.go` | 新库不建表；`check_model_migration.py` 红 |
| 渠道常量 + `model.Channel*` | `model/message_event.go` | 收件箱前端/统计的渠道词表缺值 |
| `channelcaps` manifest | `internal/channelcaps/manifests.go` | `TestEveryChannelConstantHasManifest` 红 |
| 验签失败 4xx 不落库的非对称测试 | 渠道测试文件 | 安全面回退无门禁 |
| 拒绝提示表同步 | `webhookInboundRejectHints` + `TestWebhookInbound_RejectHintsStayConsistentWithCapability` | 自相矛盾（已接入却仍写「不支持」）→ 门禁红 |

- FR060 wecomcs：`kf/sync_msg` 游标拉取 + `kf/send_msg`，`msg_signature` 验签，`event_type` 映射
  （`enter_session`/`msg_send_fail`/`session_status_change` → 事件总线）。账号走新表 `wecom_cs_accounts`
  （`corp_id/kf_secret/kf_token/kf_aes_key/open_kfid`）。
- FR061 OneBot v11：HTTP 上报 `POST /api/webhook/onebot/{account_id}` + 反向 WS；事件映射
  `message/notice/request/meta_event`；`send_private_msg`/`send_group_msg`；`echo` 关联请求响应。
  注意与已有 `qq`（官方机器人）区分：`qq_accounts` 不能承载 OneBot 的 `self_id/access_token`，需新表。
- FR062 Slack：Events API `v0` 签名 + `url_verification` challenge；`app_mention`/`message.im`；
  `chat.postMessage` + `thread_ts` 线程回复；`event_id` 作幂等键（Slack 重投同一 event_id）。
- FR063 wecombot：回调解密（AESKey）+ `template_card_event` + 主动推送 webhook。
- FR064 每个平台 `channelcaps` 事件/消息能力必须与实际实现一致（测试钉死）。

### FR-8 通用入口

- FR070 `POST /api/webhook/custom/{account_id}`：JSON 契约（`event_id/channel_msg_id/conversation_id/
  sender{id,name}/msg_type/content/media_url/timestamp/is_group/group_id`）+ `X-Hive-Signature`
  （HMAC-SHA256 + 5 分钟时间窗）+ msg_id 幂等。替代今天的 `ChannelCustom` 拒绝提示（`webhook.go:358-372`）。
  同时要改三处：`webhookInboundRejectHints` 删掉 custom 条目、`webhookAccountTableByChannel` 加
  `custom_channel_accounts`（否则租户反查走到歧义拒绝）、
  `controller/webhook_batchc_d03_http_test.go:75` 的断言（今天要求 custom 回「无入站适配器」）。
- FR071 出站：账号配置 `callback_url`，签名回调，重试沿用 `enqueueSendRetry`。

## 四、需证据触发（不进本 spec 的施工清单，登记备查）

| 项 | 触发条件 |
|---|---|
| Chatwoot 式 `Inbox` 实体（渠道实例 + 策略容器） | 出现「同一平台多套策略并存」的真实需求（如两个企微主体不同欢迎语/工作时间） |
| Redis 外置未读计数 | `inbox_conversations` 聚合查询在压测中 p95 > 200ms 或成为慢查询 TOP3 |
| 消息模板体系（问候/离线/CSAT/自动解决） | 运营侧提出模板需求，且当前 `aiReplyQuietHours` 已不足以覆盖 |
| 消息翻译 | 跨境客户实际诉求（当前无） |

## 五、验收总纲

1. 每阶段独立可交付、可回滚；任一阶段失败不阻塞后续（除 FR-1 是 FR-7 的前置）。
2. 每条 FR 至少一个可执行判据（测试名或命令），写在 `tasks.md` 对应任务的「验收」字段。
3. 全量门禁：`go build ./... && go vet ./... && go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ ./internal/channelgw/...`；
   `python3 scripts/check-layer-boundary.py`、`python3 scripts/check_model_migration.py`、
   `bash scripts/check-enum-consistency.sh`（新增渠道必跑，见 §七 门禁矩阵）。
4. 新平台验收必须包含：验签失败 4xx 且不落库、同 msg_id 二次投递不重复、私聊与群聊各一条真实回放、
   渠道错误返回 `*ChannelError`。
5. 现有行为零回归：`TestCardDelivery`、bridge outbox 全量测试、telegram/whatsapp 入站测试全绿。

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
| C13 | wecomcs 复用 `wecom_accounts` | `model/wecom.go:8` 的 `WeComAccount` 无 `kf_token/kf_aes_key/open_kfid`；且 `ResolveTenantByChannelAccount` 按渠道表反查，两渠道共用一张表会猜 | 新建 `wecom_cs_accounts`，验签复用 `verifyWeCom` |
| C14 | parity 测试要新建 | `TestBridgeOutboxParityWithWebhookOutbound` 已存在（registry ↔ 出站分支双向钉住，见 `default` 分支注释 CH-37b刀3） | FR005 改为「扩写既有测试」 |
| C15 | `IngressResult` 可直接加 `RouteRuleID` | 现状字段仅 `Accepted/HumanLocked/QueuedForAI/SessionID/Reason`（`inbox_ingress.go:129`） | D3 注明需加字段，不是「记入」即可 |
| C16 | custom 只需删拒绝提示 | `controller/webhook_batchc_d03_http_test.go:75` 断言 `/api/webhook/custom/1` 返回「无入站适配器」，`webhookInboundRejectHints` 有 custom 行，`webhookInboundCapable` 与 `Verify` 均无 custom 分支，且**无 `custom_accounts` 表/模型** | FR070 列出四处必改 + 新建账号表并登记 `allModels()` |
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

# Chatwoot 消息聚合链路 ↔ hivemtk 对照与优化清单

> **性质**：第三方开源项目横向对照（2026-11-17）。①Chatwoot 结论来自本地浅克隆
> `chatwoot/chatwoot`（develop，`refs/chatwoot`，10032 文件）；②hivemtk 结论来自本仓
> `master` 源码与在库测试；③优化清单每项都标了「本轮已交付 / 待立项」，已交付项附文件与测试。
> **证据标记**：`实证`=源码可直接指出（附路径）；`测试`=在库测试名；`本轮复跑`=2026-11-17 执行记录。

---

# A. Chatwoot 怎么聚合消息（一条入站消息的完整旅程）

```
渠道 webhook
  → app/controllers/webhooks/<channel>_controller.rb      # 只做验签 + 入队
  → app/jobs/webhooks/<channel>_events_job.rb             # ActiveJob 异步
  → app/services/<channel>/incoming_message_service.rb    # 每渠道一个聚合器
       ├─ 幂等：find_message_by_source_id / Redis SET NX 锁
       ├─ 身份：ContactInboxWithContactBuilder（contact + contact_inbox）
       ├─ 会话：contact_inbox.conversations.where.not(status: :resolved).last || create
       └─ 落库：Message.create!（触发 after_create_commit 回调链）
  → app/models/message.rb#after_create_commit
       reopen_conversation → set_conversation_activity → dispatch_create_events
       → send_reply → message_template_hooks → update_contact_activity
  → app/jobs/send_reply_job.rb → app/services/<channel>/send_on_<channel>_service.rb
  → 状态回执：Messages::StatusUpdateService（sent/delivered/read/failed）
  → 实时：Dispatcher → ActionCableListener → ActionCableBroadcastJob
```

## A1. 入站控制器与 Job（实证）

- `app/controllers/webhooks/telegram_controller.rb#process_payload`：
  `Webhooks::TelegramEventsJob.perform_later(params.to_unsafe_hash)`；`callback_query`
  这类需要即时应答的交互在控制器内直接回，不排队。
- 同类控制器一渠道一个：`whatsapp / line / sms / instagram / shopify / stripe_app / tiktok`。
- Job 层只负责重试与隔离，不含业务规则。

## A2. 每渠道一个 IncomingMessageService（聚合器）

以 `app/services/telegram/incoming_message_service.rb` 为例（实证）：

```ruby
pattr_initialize [:inbox!, :params!]
transform_business_message!
return unless private_message?                     # 业务过滤前置
set_contact     # ContactInboxWithContactBuilder（source_id = from_id）
update_contact_avatar
set_conversation
build Message(content:, content_attributes:, source_id:, message_type: :incoming)
process_message_attachments                        # location / files / contact
@message.save!
```

`set_conversation` 的会话查找规则（实证）：

```ruby
if inbox.lock_to_single_conversation
  contact_inbox.conversations.last
else
  contact_inbox.conversations.where.not(status: :resolved).last || create
end
```

`conversation.additional_attributes` 存平台会话级上下文（chat_id / business_connection_id），
邮件渠道另有一套 `Mailbox::ConversationFinderStrategies`（in_reply_to / references /
receiver_uuid / new_conversation）。

## A3. 身份聚合：ContactInboxWithContactBuilder（实证）

1. 先按 `(inbox, source_id)` 找 `contact_inbox`，命中即返回（唯一索引钉死）；
2. 未命中则在事务里 `find_contact`：identifier → email → phone_number（含候选）→
   Instagram source_id（同账号下 FacebookPage inbox 反查）；
3. 找不到就 `create_contact`（Haikunator 生成昵称），再 `ContactInboxBuilder` 建关联；
4. `rescue ActiveRecord::RecordNotUnique` 后重试——并发首条消息不会建出两个联系人；
5. 头像异步拉取，不阻塞落库。

**要点：`source_id`（平台侧用户 id）是聚合主键，不是昵称/手机号；同一平台账号下
一个 source_id 恒等于一个 contact_inbox。**

## A4. 幂等与并发（实证）

- `find_message_by_source_id` 先查后写；
- 写之前加**原子 Redis 锁** `lock_message_source_id!`（SET NX + TTL），
  两个 worker 同时处理同一条平台消息时只有一个能进；
- `source_id` 同时是「出站回声」的识别依据：`outgoing_message_originated_from_channel?`
  就是 `message.source_id.present?`，防止自己发出去的消息被当成新入站再回一遍。

## A5. Message 模型的回调链（聚合的状态机核心，实证）

`app/models/message.rb`：

```ruby
after_create_commit :execute_after_create_commit_callbacks
# = reopen_conversation → mark_pending_conversation_as_open_for_human_response
#   → set_conversation_activity → dispatch_create_events → send_reply
#   → execute_message_template_hooks → update_contact_activity
```

- `dispatch_create_events`：发 `MESSAGE_CREATED`；若是 `valid_first_reply?` 再发
  `FIRST_REPLY_CREATED`，并写 `conversation.first_reply_created_at`、清 `waiting_since`；
  否则 `update_waiting_since`（人工/机器人回复清空，入站且为空则写入）。
- `human_response?` = outgoing 且非转发、无 automation_rule_id、无 campaign_id，且
  sender 是 User 或带 `external_echo`；`bot_response?` = outgoing 且 sender 是 AgentBot。
- `reopen_conversation`：muted 会话跳过；入站消息把 snoozed 打开、resolved 重开。
- `ensure_in_reply_to` / `ensure_processed_message_content` 在 before_save 收口引用与正文；
  `prevent_message_flooding` 在 before_validation 限流。
- 附件：`before_add :validate_attachments_limit`，超限直接拒。

## A6. 出站：SendReplyJob + Base::SendOnChannelService（实证）

`app/jobs/send_reply_job.rb` 维护 `CHANNEL_SERVICES` 映射（Channel::Telegram →
Telegram::SendOnTelegramService 等），FacebookPage 按 `additional_attributes['type']`
特判 Instagram 直聊；翻译服务在投递前跑。

`app/services/base/send_on_channel_service.rb` 把出站契约写成了注释：

```
inherit → implement channel_class → implement perform_reply → 可选 override validation_methods
perform = validate_target_channel
        → return unless outgoing_message?（outgoing? || template?）
        → return if invalid_message?（private / source_id 已存在 / voice_call）
        → perform_reply
```

**新渠道 = 一个 Service 子类 + 一行映射**，不碰其他渠道代码。

## A7. 状态回执与实时（实证）

- `Whatsapp::IncomingMessageBaseService#process_statuses` 把 sent/delivered/read/failed
  映射为 `Messages::StatusUpdateService`，失败带 `external_error`（"code: title"）。
- 实时：`config/initializers/event_handlers.rb` 装配 `Dispatcher` + listeners；
  `app/listeners/action_cable_listener.rb` 分发 MESSAGE_CREATED/UPDATED、
  CONVERSATION_CREATED/STATUS_CHANGED、unread_count_changed、typing_on/off、
  assignee_changed、notification_*；`Pubsubable` 给每个 contact_inbox 发 `pubsub_token`。
- 未读计数：`Conversations::UnreadCounts::Store` 用 Redis 集合按
  account::inbox::label::team 维护，另有 Builder/Counter/Refresher。

## A8. 正文归一与渠道渲染（实证）

- `Messages::WebhookContentNormalizer`：去 CommonMark 硬换行反斜杠、还原代码块外的
  分隔线转义、裁尾部空行；
- `Messages::MarkdownRendererService` + `MarkdownRenderers::{base,instagram,line,plain_text,telegram,whats_app}`
  ——同一份正文按渠道渲染成该渠道能显示的形态。

# B. hivemtk 现状（对照）

| 环节 | Chatwoot | hivemtk 现状（实证） | 差距 |
|---|---|---|---|
| 入站入口 | 控制器只验签+入队 | `service/webhook.go` Receive → 队列 → handleJob → `dispatchToChannel` switch | 等价；我方 switch 是硬编码 |
| 每渠道聚合器 | `IncomingMessageService` 一渠道一类 | `webhook_channel_<x>.go` 的 dispatchXxx + 共享 `inbox_ingress` | 等价 |
| 身份聚合 | ContactInbox(source_id 唯一) | `CustomerChannel(one_id, channel)` + `InboxConversation(platform, account, customer_id)` | 我方唯一键**不含 account_id**（`model.CustomerChannel`），跨账号同 id 会串 |
| 会话查找 | 未 resolved 的最后一条 / lock_to_single | `FindByPlatformAccountCustomer` 单会话 + closed→unread 重开 | 已有；缺 waiting/snooze 语义 |
| 幂等 | source_id 查询 + Redis 锁 | `MessageHub` 唯一键 (platform, msg_id, conversation_id, direction) + 去重矩阵 ~25 个测试 | **强于 Chatwoot** |
| 状态机 | Message 回调链 | `UpsertFromMessageTx` 里 unread/status 转换 | **缺 waiting_since / first_reply_at / last_activity_at**（本轮补齐） |
| 出站 | SendReplyJob + CHANNEL_SERVICES | `sendOutbound` 1467 行巨型 switch（WeCom/Feishu/Telegram/QQ/WhatsApp/DingTalk/WeChat + 桥接 outbox） | **新渠道要改 switch**（待立项） |
| 状态回执 | StatusUpdateService | `inbox_ingress_outbound.go` 的 ack（pending→delivered/send_failed/unknown） | 等价 |
| 实时 | ActionCable + pubsub token | 桥接 SSE/outbox + 管理台轮询 | 形态不同，够用 |
| 未读 | Redis 集合 + Refresher | `inbox_conversations.unread_count` 行内计数 | 规模上来再考虑 |
| 正文归一 | WebhookContentNormalizer + 渠道 Markdown | `htmlEscapeAndPreserveMarkdown`、`AppendMediaContextToContent` | 缺"按渠道渲染"的统一层 |
| 能力声明 | 无（渠道类自身） | `capregistry` 粗粒度 7 能力 | **缺消息级能力表**（本轮补齐） |

# C. 优化清单（按优先级，含状态）

## P0（本轮已交付）

### P0-1 会话生命周期三列（waiting_since / first_reply_at / last_activity_at）

- **借鉴点**：Chatwoot 在 `Message#after_create_commit` 里维护 conversation 的
  `waiting_since` / `first_reply_created_at`，会话行只读，列表排序与 SLA 不必反查消息表。
- **落点**：
  - `internal/model/ai_sales_champion.go`：`InboxConversation` 新增三列（带索引）；
  - `internal/repository/message_hub_inbox_conversation.go#UpsertFromMessageTx`：
    客户消息「从无到有」写 waiting_since（连发不刷新起点），我方消息清 waiting_since、
    first_reply_at 只写一次，任意消息推进 last_activity_at；`UpdateLastMessage` 同步推进活动时间；
  - `migrations/064_inbox_conversation_lifecycle.sql`：幂等加列 + 索引 + 回填
    （waiting_since 只回填「最后一条是客户消息且未关闭」，first_reply_at 不回填、不编造）。
- **测试**（`测试`）：`internal/repository/message_hub_inbox_lifecycle_test.go`
  - `TestInboxConversation_UpsertFromMessage_Lifecycle`（客户首条→连发不刷新→首响清空→首响只写一次→客户再来重新等待）
  - `TestInboxConversation_UpsertFromMessage_AgentFirst`（我方开场的会话）
  - `TestInboxConversation_UpdateLastMessage_AdvancesActivity`
  - `本轮复跑`：2026-11-17，三条全 PASS（真实 PG 8217）。

### P0-2 渠道消息级能力表（channelcaps）

- **借鉴点**：LangBot 每平台 `manifest.yaml` 声明事件/API；Chatwoot 的富卡/正文渲染
  也隐含"渠道能承载什么"。我方原来只有 `outboundSupportsRichCards` 一处硬编码 switch。
- **落点**：`internal/channelcaps/{channelcaps.go,manifests.go}`：
  `Manifest{Channel, Label, Via, Inbound, Outbound, Features}`，覆盖 18 个渠道常量；
  `Markdown()` 生成 `docs/channel-capabilities.md`，文档漂移即红。
- **下一步（待立项）**：`outboundSupportsRichCards` 改为
  `channelcaps.Has(channel, FeatureRichCard)`——渠道能力只剩一处事实源。
  本次未接线：`channel_card_delivery.go` 尚未入库，避免把无关未提交文件混进提交。
- **测试**（`测试`）：`TestCapabilityDocFreshness`、`TestEveryChannelConstantHasManifest`、
  `TestRichCardOnlyWhereSenderRendersCards`、`TestBridgeChannelsAllDeclareText`、
  `TestRegisterDuplicatePanics`、`TestUnknownChannelClaimsNothing`；
  `本轮复跑`：`go test ./internal/channelcaps/` PASS，
  `go test ./internal/service/ -run TestCardDelivery` PASS（证明现有富卡行为未被改动）。

## P0（待立项）

### P0-3 出站 sender 注册表（替掉 sendOutbound 巨型 switch）

- **借鉴点**：Chatwoot `SendReplyJob::CHANNEL_SERVICES` + `Base::SendOnChannelService`
  ——一条映射 + 一个子类就是一个渠道。
- **现状痛点**：`internal/service/webhook_outbound.go#sendOutbound`（607 行起）单函数
  ~800 行，7 个渠道分支共享 `markSendFailed`/卡片降级/重试入队/补触发等收尾逻辑，
  新增平台必须改这个函数，review 面与回归面都大。
- **设计（建议）**：

  ```go
  type channelSender interface {
      // Send 只负责"把这一条交给平台"，失败必须返回 *ChannelError（可重试分类）。
      Send(ctx context.Context, req outboundRequest) (sent bool, err error)
  }
  type outboundRequest struct { Channel WebhookChannel; AccountID string; Payload *ParsedPayload
      Content string; Hub *model.MessageHub; Cards []model.RichCard }
  var channelSenders = map[WebhookChannel]channelSender{}
  func registerChannelSender(ch WebhookChannel, s channelSender) { /* 重复 panic */ }
  ```

  `sendOutbound` 保留公共收尾（quiet hours、ClaimReply、卡片降级、失败入队、补触发），
  渠道差异下沉到 `wecomSender/feishuSender/telegramSender/...`；
  桥接渠道实现 `bridgeOutboxSender`。每一步用现有测试做回归网（`webhook_outbound_b5_bridge_test.go` 等）。
- **验收**：`sendOutbound` 主体 < 150 行；新增一个渠道只需 `RegisterChannelSender` +
  实现 `Send`；`go test ./internal/service/` 全绿。

## P1（待立项）

| # | 项 | Chatwoot 证据 | hivemtk 落点 |
|---|---|---|---|
| P1-1 | 统一正文归一 + 按渠道渲染 | `Messages::WebhookContentNormalizer`、`MarkdownRenderers::{telegram,whats_app,line,...}` | 新 `internal/channelrender`：输入 `channel + content + msg_type`，输出渠道可显示文本；替换 `htmlEscapeAndPreserveMarkdown` 的散点调用 |
| P1-2 | 身份唯一键补 account_id | `ContactInbox` 唯一索引 (inbox_id, source_id) | `model.CustomerChannel` 唯一键改 `(tenant_id, one_id, channel, account_id)`；迁移需评估历史数据 |
| P1-3 | 出站回声识别显式化 | `outgoing_message_originated_from_channel?` = source_id 存在 | `MessageHub` 已有 `PlatformMsgID`/direction，建议在 ingress 明确"回声即丢弃"的单一判定函数 + 测试 |
| P1-4 | 模板消息（greeting / out-of-office / CSAT） | `MessageTemplates::{Greeting,OutOfOffice,CsatSurvey,AutoResolve,EmailCollect}` | 现有 `enqueueDelayedOutbound` 之外缺"进入会话自动问候/下班自动回复"；按渠道能力表决定是否可发 |
| P1-5 | 未读计数外置 | `Conversations::UnreadCounts::Store`（Redis 集合 + Refresher） | 当前行内计数在万级会话/多坐席下会争锁；先压测再决定 |

## P2（待立项）

| # | 项 | 说明 |
|---|---|---|
| P2-1 | 入站聚合窗口 | LangBot `pipeline/aggregator` 把连发合并成一轮；hivemtk 有 `AppendPendingMessage`/`PopPendingMessages`，可加时间窗+最大条数，减少 AI 重复触发 |
| P2-2 | 按事件类型路由 | LangBot `botmgr._evaluate_eba_event_bindings`；我方 webhook 渠道目前只有"消息→AI"一条路，编辑/撤回/入群事件无绑定机制 |
| P2-3 | 附件上限与类型校验 | Chatwoot `validate_attachments_limit`；我方 `channelbot` 各自校验，建议收到统一 `channelcaps` 上限字段 |
| P2-4 | typing / 已读回执 | Chatwoot `ConversationTypingStatusManager`、`typing_on/off`；我方桥接有 `IsRead`，缺"正在输入" |

# D. 本轮交付与验证

| 交付物 | 路径 |
|---|---|
| 会话生命周期（模型+仓储+迁移） | `user-server/internal/model/ai_sales_champion.go`、`user-server/internal/repository/message_hub_inbox_conversation.go`、`migrations/064_inbox_conversation_lifecycle.sql` |
| 生命周期测试 | `user-server/internal/repository/message_hub_inbox_lifecycle_test.go` |
| 渠道能力表（包+manifest+文档+测试） | `user-server/internal/channelcaps/*`、`user-server/docs/channel-capabilities.md` |
| 平台差集与接入配方 | `docs/research/LANGBOT-VS-HIVEMTK-PLATFORM-ARCH-2026-11.md` |

`本轮复跑`（2026-11-17）：

```
go build ./...                                              OK
go vet ./internal/channelcaps ./internal/repository ./internal/service ./internal/model   OK
go test ./internal/channelcaps/                             ok
go test ./internal/service/ -run TestCardDelivery           ok
go test ./internal/repository/ -run 'InboxConversation|UpsertFromMessage'   ok
python3 scripts/check_model_migration.py                    392 模型 / 422 注册，全绿
python3 scripts/check-layer-boundary.py                     跨层违规 0
```

# Tasks: 024-im-seam-alignment（IM 接入接缝对齐）

**Input**: Design documents from `/specs/024-im-seam-alignment/`
**Prerequisites**: 既有交付 `2f658e7b`（`internal/channelcaps` + 会话生命周期三列）；调研文档 4 篇（见 `spec.md` 来源）
**Constraint**: 每任务独立可回滚；平台接入任务以「8 触点齐全」为完工定义；所有测试命令在 `user-server/` 下执行

图例：P0 阻塞后续 / P1 本轮内 / P2 可延后至下一 spec｜🟡 纯本地可验收｜🔵 需真实账号验收（代码与 fixture 测试仍本地完成）

---

## Phase 1 出站接缝（P0，平台接入的前置）

- [ ] **T101 出站注册表与接口**（P0｜依赖 —｜S｜🟡）
  - 现状：`webhook_outbound.go:607` `sendOutbound` 单函数 663 行，`switch channel` **7 个 case + 1 个 default（bridge 族）**，共 8 条出站路径。
  - 改动：新建 `internal/service/outbound_registry.go`——`OutboundRequest{Channel, AccountID, Payload, Content, Cards, Hub, Fail, FailUntracked}`、`OutboundResult{Sent, PlatformMsgID}`、`channelSender{Channel() WebhookChannel; Send(ctx, *OutboundRequest) *OutboundResult}`（**不返回 error**，失败只走 `Fail` 回调：`sendErr` 必须单一真值来源，且飞书「正文失败仍发卡片」的顺序不能被打乱）、`RegisterChannelSender`（空 Channel / 重复注册 panic）、`ChannelSenderOf`、`RegisteredSenders()`、`ErrOutboundChannelUnsupported`。
  - 注册表必须是**进程级单例**：QQ sender 依赖 `s.qqIntegration` 的惰性单例（`msg_seq` 计数与 token 缓存必须跨次出站存活，每次新建会让 seq 恒为 1 被平台丢弃）。
  - 验收：`go test ./internal/service/ -run TestOutboundRegistry` 绿（重复注册 panic / 未知渠道取不到 / 注册后取回同一实例 / 注册表按 channel 排序稳定 / `Fail` 回调被调用时 `sendErr` 被置位）。
  - 风险：无行为变更（纯新增文件）。

- [ ] **T102 wecom sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - 现状：`webhook_outbound.go:685` `case ChannelWeCom`（`integration.SendMessage` + `WeComSendRequest{AccountID, ExternalUserID, MsgType:"text"}`）。
  - 改动：`outbound_sender_wecom.go`，`type wecomSender struct{ svc *WebhookService }`，函数体原样搬运。
  - 验收：搬迁前后 `TestCardDelivery` 断言不变；新增 `TestWecomSender_DelegatesToIntegration`（fake integration 记录调用参数一致）。

- [ ] **T103 feishu sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - 现状：`webhook_outbound.go:709`。
  - 改动：`outbound_sender_feishu.go`；卡片 → interactive JSON 的分支保持原样（后续 T404 才改由渲染层统一）。
  - 验收：`TestFeishuSender_RichCardPayload` 快照一致。

- [ ] **T104 telegram sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - 现状：`webhook_outbound.go:748`（懒加载 `s.integrations.tg()`、`ParseUint(accountID)`、`chat_id ← hubMsg.ConversationID`、`extractTelegramReplyMetaFromCtx` + `buildTelegramGroupReply`、`SendMessageEx` + 逐卡 `SendCard`）。
  - 改动：`outbound_sender_telegram.go`；群回复增强逻辑一并搬入（`buildTelegramGroupReply` 保留在 `webhook_outbound.go` 不动，供 sender 调用）。
  - 验收：`TestTelegramSender_GroupMention` 与 `TestTelegramSender_Cards` 绿；行为冻结断言含 `reply_parameters` 与 `parse_mode`。

- [ ] **T105 qq sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - 现状：`webhook_outbound.go:792`（`QQOutboundMsgID(hubMsg)`、被动窗口 5 分钟/5 条、`msg_seq`）。
  - 改动：`outbound_sender_qq.go`；被动窗口注释一并迁移（该注释是行为契约）。
  - 验收：`TestQQSender_PassiveWindowMsgSeq` 绿（第 6 条降级为主动消息或失败，与原实现一致）。

- [ ] **T106 whatsapp sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - 现状：`webhook_outbound.go:818`——不只是 Cloud API payload：先 `waIntegration.ensureReposFromDB`，再用 `messageHubRepo.GetLastInboundByConversation` 判「最后入站是否超 24h」；超窗时取参数中心 `channelbot.whatsapp_fallback_template`（语言 `..._lang`），有模板则 `SendTemplateMessage` 并 `return`，无模板则 `CategoryWindowExpired` 失败。
  - 改动：`outbound_sender_whatsapp.go`；sender 需持 `svc`（取 repo 与参数），窗口判定与模板降级逐行保留。
  - 验收：`TestWhatsappSender_PayloadShape`（`messaging_product`/`to`/`type` 不变）+ `TestWhatsappSender_WindowExpired`（>24h 无模板 → `*ChannelError{CategoryWindowExpired, Retryable:false}`）、`TestWhatsappSender_WindowExpiredWithTemplate`（走 `SendTemplateMessage`）。

- [ ] **T107 dingtalk sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - 现状：`webhook_outbound.go:870`——不经过 integration，直接 `http.DefaultClient` POST `hubMsg.Extra["session_webhook"]`；前置校验顺序为「缺 webhook → `CategoryWindowExpired`」「`session_webhook_expired_at`（秒/毫秒都兼容）已过期 → `CategoryWindowExpired`」「`url.Parse` + `loadDingtalkWebhookHostAllowed`（L1304，仅 `*.dingtalk.com`）→ `CategoryBadRequest`」，响应按 `errcode` 判定。
  - 改动：`outbound_sender_dingtalk.go`；白名单校验留在 sender 内（属该渠道安全边界，不得下沉为通用逻辑）。
  - 验收：`TestDingtalkSender_RejectsNonAllowlistedHost`（非白名单 host → `*ChannelError`，**不发请求**）、`TestDingtalkSender_ExpiredWebhook`、`TestDingtalkSender_ErrcodeNonZero`。

- [ ] **T108 wechat sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - 现状：`webhook_outbound.go:951`（`wechatIntegration.SendCustomMessage`）。
  - 改动：`outbound_sender_wechat.go`。
  - 验收：`TestWechatSender_CustomerServiceMessage` 绿。

- [ ] **T109 bridge-outbox sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - 现状：`webhook_outbound.go:967` 起 default 分支约 300 行——`bridgeOutboxCapable` 校验、Markdown 清洗、`model.MessageHub` pending 行（含租户继承、tracing carrier、`cards_fallback_count`、`isBridgeChannelUndeliverableLocal` 判 failed）、`CreateMessageHubIdempotent`（第二次带 `:conv` 后缀重试）、`duplicateOccupied` → `:dup` 失败行、`inboxConvRepo.UpsertFromMessage`、`GlobalSSEPublisher` 唤醒、以及 `persisted.ID==0 / Status=="failed" / default` 三段 sent 口径。
  - 改动：`outbound_sender_bridge.go`；**Markdown 清洗不在本任务搬**——它要上移到 `sendOutbound` 横切段（见 T110），因为 defer 里的 `enqueueSendRetry(..., content, ...)` 捕获的是同一个 `content` 变量，搬进 sender 会让重试投未清洗文本。`cardsFallbackCount` 由 `OutboundRequest` 传入。
  - 验收：`go test ./internal/service/ -run 'BridgeOutbox|Outbound'` 全绿；新增 `TestBridgeSender_DuplicateOccupied`、`TestBridgeSender_UndeliverableReason`、`TestBridgeSender_RetryCarriesStrippedContent`。

- [ ] **T110 `sendOutbound` 收口 ≤150 行**（P0｜依赖 T102–T109｜M｜🟡）
  - 改动：`sendOutbound` 只保留横切逻辑：静默时段延迟入队 → `ClaimReply` 去重 → `context.WithTimeout(utils.MediumTimeout)` → **富卡降级成文本 + `cardsFallbackCount`**（今天在 switch 之前，跨渠道，留在横切段）→ **bridge 族 Markdown 清洗**（`bridgeOutboxCapable(channel) && stripMarkdownForBridgeChannel(...)`；上移后 defer 里的重试入队仍拿到清洗后的 `content`，与今天等价）→ `ChannelSenderOf` 分发 → `defer`（重试入队 / `ReleaseReply`（`!sent` 时释放 ClaimReply）/ `ReleaseAIProcessingFlag` / `SafeGo(RecheckUnrepliedAndTrigger)`）。未知渠道返回 `ErrOutboundChannelUnsupported`。
  - 注意两个 defer 都不能丢：`claimReply` 的释放只在 `!sent` 时发生，`ReleaseAIProcessingFlag`/补触发只在 `hubMsg.ConversationID != ""` 且非重放时发生（`isDelayedReplay`）。
  - 验收：`awk` 统计该函数 ≤150 行；`grep -c 'switch channel'` = 0；`go build ./...` 绿；`TestSendOutbound_Bridge_MarkdownAlignedWithManualPath` 仍绿（该测试已存在，钉住 R5 清洗口径）。

- [ ] **T111 渠道覆盖门禁测试**（P0｜依赖 T110｜S｜🟡）
  - 现状：`TestBridgeOutboxParityWithWebhookOutbound` **已存在**（registry `OutboundPath=bridge_outbox` ↔ 出站分支双向钉住，见 `webhook_outbound.go` default 分支注释 CH-37b刀3）。不要新建重复门禁。
  - 改动：扩写该测试为「注册表 sender ↔ `webhookInboundCapable` 7 渠道 ↔ `channelgw.Default.Names()`」三向一致；新文件 `outbound_registry_parity_test.go` 只放新增断言。
  - 验收：故意删一个 sender 注册 → 两个方向都红且信息含渠道名；恢复 → 绿。

- [ ] **T112 行为冻结对照测试**（P0｜依赖 T110｜M｜🟡）
  - 改动：表驱动 `(channel, payload, cards, hubMsg)` → 通过 fake integrations 记录 `(method, args)` 序列，与搬迁前快照比对（快照在 T102 前用旧实现生成）。
  - 验收：8 渠道 × 3 组样例全绿；快照文件入库（`testdata/outbound_golden.json`）。

---

## Phase 2 事件类型体系（P0）

- [ ] **T201 `EventType` 常量与字段**（P0｜依赖 —｜S｜🟡）
  - 现状：`model/message_event.go`（93 行）无 `EventType`；`ParsedPayload.EventType`（`webhook.go:1006`）把 `event_type/event/type/msg_type` 一并 coalesce，语义混杂，**不能当事件类型来源**。
  - 改动：新增 10 个常量（见 spec D2）；`MessageEvent.EventType string`；`NormalizeEvent`（`inbox_ingress.go:480`）空值归一 `message.received`（该函数是 `channelbot.ToMessageEvent` 与各 `webhook_channel_*.go` 两条构建路径的唯一收口点）。
  - 验收：`TestNormalizeEvent_DefaultsToReceived`、`TestNormalizeEvent_PreservesExplicitType`、`TestInboundMessage_ToMessageEvent` 仍绿（channelbot/core 既有测试）。

- [ ] **T202 消息编辑/撤回仓储能力**（P0｜依赖 T201｜M｜🟡）
  - 现状：`MessageHub` 已有 `RecalledAt *time.Time` + `RecallError string`（`model/ai_sales_champion.go:71-72`）；已有 `MarkRecalled(id, at)`（`message_hub_recall.go:25`，**WHERE 带 `direction='outbound'`**，语义是「撤我发出的消息」）、`MarkRecallFailed(id, reason)`、`GetByIDForRecall`。编辑方向的窄方法不存在。
  - 改动：新增 `UpdateContentByChannelMsgID(ctx, channel, accountID, msgID, content)`；入站撤回新增 `MarkInboundRecalledByChannelMsgID(ctx, channel, accountID, msgID, at, reason)`（不能复用 `MarkRecalled`：它的方向条件会拒绝客户消息），`RecallError` 复用为原因列、`Extra.recall_source="platform"`。均按 `(platform, msg_id)` 定位。
  - 验收：`TestMessageHub_UpdateContentByChannelMsgID`、`TestMessageHub_MarkInboundRecalled_RejectsOutboundRow`（方向互斥）、`TestMessageHub_MarkInboundRecalled_NotFound`。
  - 说明：现有出站撤回链路（管理器按钮）不受影响 —— 不改 `MarkRecalled` 的条件，只加新入口。

- [ ] **T203 入站按事件类型分流**（P0｜依赖 T201/T202｜M｜🟡）
  - 现状：`HandleIngressMessage`（`inbox_ingress.go:546`）无类型分支，一切当消息处理。
  - 改动：归一后先分流——`message.received` 走原路径；`message.edited → T202 Update`；`message.deleted → T202 MarkRecalled`；`group.member_* / bot.* → 不落消息表`；`interaction.callback → 交给 T207`；`message.reaction → 只发事件总线`。
  - 验收：`TestIngress_EditedUpdatesContent`、`TestIngress_DeletedMarksRecall`、`TestIngress_MemberEventDoesNotCreateMessage`、`TestIngress_NonReceivedDoesNotTouchUnread`.

- [ ] **T204 事件总线扩展**（P0｜依赖 T203｜S｜🟡）
  - 现状：`internal/event/types.go` 仅 `customer.message.received` 等 8 个 topic。
  - 改动：新增 `customer.message.edited`、`customer.message.deleted`、`customer.group.member.changed`、`customer.interaction.callback` 四个 topic + 租户标记（复用 `StampTenantFromCtx`）。
  - 验收：`TestEvent_AuditTopicDelivery`（订阅收到、租户正确）、`TestEvent_NoSubscriberDoesNotBlock`。

- [ ] **T205 各渠道解析器填 `EventType`**（P0｜依赖 T201｜M｜🟡）
  - 现状：`webhook_channel_{wecom,feishu,telegram,qq,whatsapp,douyin}.go` 只产出消息。
  - 改动：telegram（`edited_message`/`deleted_business_message`/`message_reaction`）、feishu（`im.message.recalled_v1`）、wecom（`change_contact` 不入站）、whatsapp（`statuses` 不当作 message）、qq（`GROUP_ADD_ROBOT` 等）。
  - 验收：每渠道一条 fixture 回放断言 `EventType` 与「不产消息行」；`TestChannelParsers_EventTypeMatrix`。

- [ ] **T206 `channelcaps.Manifest.Events`**（P0｜依赖 T205｜S｜🟡）
  - 改动：`Manifest` 增 `Events []string`；21 条 manifest 补齐；`Markdown()` 增事件列；`TestManifestEventsAreKnown` + `TestDeclaredEventsCoverParser`（渠道解析器出现的事件字符串必须在其 manifest 中声明）。
  - 验收：`go test ./internal/channelcaps/` 绿；`UPDATE_GOLDEN=1` 生成文档差异仅新增列。

- [ ] **T207 交互回调最小闭环**（P1｜依赖 T203｜M｜🟡）
  - 改动：`interaction.callback` → 按渠道回 `answerCallbackQuery`（telegram）/ `ack`（feishu），写审计事件，不创建消息、不触发 AI。
  - 验收：`TestInteractionCallback_AcksAndDoesNotReply`。

---

## Phase 3 事件路由（P0）

- [ ] **T301 规则模型 + 迁移 066**（P0｜依赖 T201｜M｜🟡）
  - 改动：`model.ChannelRouteRule{TenantID, ID, Channel, AccountID, EventType, Match string(JSON), Target, TargetRef, Priority, Enabled, Note}`，表 `channel_route_rules`，索引 `(tenant_id, channel, enabled, priority)`；迁移 `066_channel_route_rules.sql`（幂等 `CREATE TABLE IF NOT EXISTS`）。
  - 必做（否则 `check_model_migration.py` 直接红）：模型需登记进 `internal/pkg/db/migrate.go` 的 `allModels()`；`AccountID` 用 `string` 且空值语义 = 全账号。
  - 验收：`python3 scripts/check_model_migration.py` 绿；迁移在空库与有库各跑一次无错；`TestChannelRouteRule_TableName` 与 `channel_route_rules` 一致。

- [ ] **T302 仓储**（P0｜依赖 T301｜S｜🟡）
  - 改动：`repository/channel_route_rule.go`——`ListByTenant`、`ListEnabled(channel, accountID)`、`Upsert`、`Delete`（全部接收 tenantID 作第一参，fail-closed）。
  - 验收：`TestChannelRouteRule_TenantIsolation`（A 租户读不到 B 租户规则）。

- [ ] **T303 匹配引擎**（P0｜依赖 T302｜M｜🟡）
  - 改动：`service/route_engine.go`——`Resolve(ctx, tenantID, evt *model.MessageEvent) (*RouteDecision, error)`；事件类型通配（`*`、`message.*`）；`Match` 支持 `Extra` 嵌套路径（`a.b.c`）等值与 `$in` 数组包含；按 `Priority` 升序取首个命中；无命中返回 `(nil, nil)`（= 默认放行）。
  - 验收：`TestRouteEngine_Wildcard`、`TestRouteEngine_NestedMatch`、`TestRouteEngine_PriorityOrder`、`TestRouteEngine_NoMatchReturnsNil`、`TestRouteEngine_InvalidJSONRuleSkipped`。

- [ ] **T304 接线 + 可观测**（P0｜依赖 T303/T203｜S｜🟡）
  - 改动：`HandleIngressMessage` 在归一后、`triggerAIForEvent` 前调用；命中 `discard` → 不入库不回复但写 `operation.log` 审计；结果写入 `IngressResult.RouteRuleID`；日志 `route_rule_hit{rule_id,event_type,channel}`。
  - 验收：`TestIngress_DiscardRuleStopsPipeline`、`TestIngress_RouteRuleIDRecorded`、`TestIngress_NoRuleKeepsLegacyBehavior`（默认放行，AI 触发行为与今天一致）。

- [ ] **T305 规则管理 API**（P1｜依赖 T302｜M｜🟡）
  - 改动：新增 `internal/router/channel_route_rule.go`（`setupChannelRouteRuleRoutes`，在 `router.go` 的 `auth` 组中注册）+ `controller/channel_route_rule.go`：`GET /api/channel-route-rules`、`POST`、`PUT /:id`、`DELETE /:id`；tenant 从上下文取，禁止跨租户；变更写审计。
  - 必做：`route_policy_gate_test.go` 要求每条新路由要么有 `middleware.RequirePermission("channel_routes.write")`/`read`，要么进匿名白名单并写理由；`cross_tenant_scan_test.go` 需能识别租户绑定。
  - 验收：`go test ./internal/controller/ -run TestChannelRouteRuleAPI`（含跨租户 404）；`go test ./internal/router/ -run 'RoutePolicy|CrossTenant'` 绿。

---

## Phase 4 渠道渲染层（P1）

- [ ] **T401 能力表补渲染字段**（P1｜依赖 —｜S｜🟡）
  - 改动：`channelcaps.Manifest` 增 `OutboundFormat string`、`MaxTextLen int`；21 条补齐（bridge=plain/2000、telegram=html/4096、feishu=markdown/30000、wechat=plain/2048 等）。
  - 验收：`TestManifestRenderFieldsNonEmpty`；文档生成差异仅为新增两列。

- [ ] **T402 `RenderForChannel`**（P1｜依赖 T401｜M｜🟡）
  - 改动：`service/channel_render.go`——`RenderForChannel(ch, msgType, content, cards) (RenderedOutbound{Text, Cards, Chunks}, error)`；规则：plain 去 MD、无 `FeatureRichCard` 卡片降文本、超 `MaxTextLen` 切块（≤3 块，超出截断加 `…`）。
  - 验收：`TestRender_PlainStripsMarkdown`、`TestRender_CardFoldedWhenUnsupported`、`TestRender_SplitsLongText`、`TestRender_UnknownChannelErrors`。

- [ ] **T403 旧函数薄包装**（P1｜依赖 T402｜S｜🟡）
  - 改动：`stripMarkdownForBridgeChannel`、`outboundSupportsRichCards`、`cardTextDelivery` 保留签名，内部改调 `RenderForChannel`（`channel_card_delivery.go` 为在制品，不动其文件，只改调用侧）。
  - 验收：`go test ./internal/service/ -run 'Markdown|CardDelivery'` 全绿。

- [ ] **T404 sender 统一走渲染**（P1｜依赖 T402/T403/T101｜M｜🟡）
  - 改动：各 sender 删掉内联 `StripMarkdown*`/`htmlEscape*` 调用，统一在 `sendOutbound` 分发前渲染一次。
  - 验收：T112 行为冻结快照更新为「渲染后内容」，逐渠道比对人工确认；`TestTelegramSender_GroupMention` 仍绿。

---

## Phase 5 身份键（P1）

- [ ] **T501 去重与换键迁移 065**（P1｜依赖 —｜L｜🟡）
  - 现状：`model/customer_channel.go` 唯一键 `(one_id, channel)`，`account_id` 仅在普通索引。
  - 改动：`migrations/065_customer_channel_account_key.sql`——① 同 `(tenant_id, one_id, channel, account_id)` 多行按 `last_seen_at DESC` 保留一条、其余 `deleted_at` 软删（幂等窗口函数）；② `CREATE UNIQUE INDEX CONCURRENTLY idx_tenant_oneid_channel_account`；③ `DROP INDEX CONCURRENTLY idx_oneid_channel`。`account_id` 空值统一 `''`。
  - 验收：在含重复数据的测试库执行两次无错；`SELECT count(*)` 去重后与新键 `GROUP BY` 结果一致；`check_model_migration.py` 绿。

- [ ] **T502 模型与写入路径**（P1｜依赖 T501｜M｜🟡）
  - 改动：`CustomerChannel` gorm tag 改新键名；`UpsertByChannelAccount` 用 `ON CONFLICT (tenant_id, one_id, channel, account_id) DO UPDATE`。
  - 验收：`TestCustomerChannel_SameOneIDTwoAccounts`（两行并存）、`TestCustomerChannel_UpsertIdempotent`、`TestCustomerChannel_EmptyAccountNormalized`。

---

## Phase 6 生命周期读侧（P1）

- [ ] **T601 `snoozed` 状态 + 迁移 067**（P1｜依赖 —｜M｜🟡）
  - 现状：状态是 `varchar` 字符串（`unread/open/assigned/closed`），无 pg enum，无需改 047；会话查询的 `CASE` 映射在 `repository/message_hub_inbox_conversation_query.go`（`closed` 过滤亦在此），加状态要同步这里。
  - 改动：`model` 增 `InboxStatusSnoozed`、`SnoozedUntil *time.Time`、`ReopenCount int`；`067_inbox_conversation_snooze.sql`（幂等加列 + `(tenant_id, snoozed_until)` 索引）。AutoMigrate 只加列不改索引，索引必须写进 SQL。
  - 验收：迁移幂等（跑两次无错）；`TestInboxConversation_SnoozeSetsUntil`；`TestInboxQuery_SnoozedNotInUnreadCount`。

- [ ] **T602 到期重置 worker**（P1｜依赖 T601｜S｜🟡）
  - 改动：新建 `service/inbox_snooze_cron.go`，沿用现有 cron 形态（`retention_cleanup_cron.go` / `message_trace_cleanup_cron.go`：`time.Ticker` + `utils.SafeGo` + 启动/停止成对）；条件更新 `UPDATE ... SET status='open', snoozed_until=NULL WHERE status='snoozed' AND snoozed_until<=now()`。
  - 验收：`TestSnoozeExpiry_ReopensOnce`（并发两次只重置一次）；`check-naked-goroutine.sh` 绿（goroutine 必须走 SafeGo）。

- [ ] **T603 重开计数**（P1｜依赖 T601｜S｜🟡）
  - 改动：`message_hub_inbox_conversation.go:332-336` 重开分支 `reopen_count = reopen_count + 1`。
  - 验收：`TestInboxConversation_ReopenIncrementsCount`。

- [ ] **T604 待首响查询 + API**（P1｜依赖 T601｜M｜🟡）
  - 现状：查询层文件是 `repository/message_hub_inbox_conversation_query.go`（`closed` 等 filter 的 `CASE` 映射在此）。
  - 改动：增 `filter=awaiting_first_reply`（`waiting_since IS NOT NULL AND first_reply_at IS NULL`，`ORDER BY waiting_since ASC`）；controller 参数透传。
  - 验收：`TestInboxQuery_AwaitingFirstReply`（含已首响/已关闭样本不计入）、API 层 `TestInboxAPI_FilterAwaitingFirstReply`。

- [ ] **T605 首响时长指标**（P2｜依赖 T604｜M｜🟡）
  - 改动：只读聚合 `AVG/percentile(first_reply_at - waiting_since)`，按渠道/租户/时间窗；无首响样本排除。
  - 验收：`TestFirstReplyMetrics_ExcludesUnanswered`。

- [ ] **T606 前端筛选与展示**（P2｜依赖 T604｜M｜🟡）
  - 改动：`user-web` 收件箱列表加「待首响」筛选与等待时长徽标。
  - 验收：`npm run build` 绿 + 页面点验（逐元素操作，留快照证据）。

---

## Phase 7 企业微信客服 wecomcs（P0 首个平台，验证接缝）

- [ ] **T701 `channelbot/wecomcs` 协议客户端**（P0｜依赖 T101｜L｜🟡）
  - 改动：`channelbot/wecomcs/wecomcs.go`——`GetToken(corpid, secret)`（缓存 + 提前 5 分钟刷新）、`SyncMsg(cursor, token, limit)`、`SendMsg(req)`、`VerifySignature(signature, timestamp, nonce, echostr)`（复用 core 的 `SecureEqual`）。纯协议层，不依赖 service。
  - 验收：`channelbot/wecomcs/wecomcs_test.go` 用 httptest 回放：token 缓存命中只请求一次、`SyncMsg` 分页游标透传、验签失败返回错误。

- [ ] **T702 入站 handler**（P0｜依赖 T701｜L｜🟡）
  - 改动：`service/webhook_channel_wecomcs.go`——`VerifyWecomcs`、`HandleWecomcsCallback`（解密 → 取 `Token`/`OpenKfId` → `SyncMsg` 增量 → `ToInbound` → `Ingress`）；`event_type` 映射（`enter_session`/`msg_send_fail`/`session_status_change`/`reject_customer_msg_switch_change` → 事件总线）。
  - 验收：fixture 回放（文本/图片/事件）+ `TestWecomcs_DuplicateCallbackIdempotent`（同 `msgid` 二次投递不重复入库）。

- [ ] **T703 三处登记 + 账号表**（P0｜依赖 T702｜M｜🟡）
  - 现状：`model/wecom.go` 的 `WeComAccount` 只有 CorpID/CorpSecret/AgentID/AgentSecret/AccessToken，**没有 `kf_token`/`kf_aes_key`/`open_kfid`**，只能新建表。
  - 改动：新增 `model.WeComCSAccount`（表 `wecom_cs_accounts`，迁移 `068_wecom_cs_accounts.sql`：tenant_id/corpid/secret/open_kfid/token/aes_key/ai_agent_enabled/status）+ 登记 `allModels()`；`webhook.go` 五处：`webhookInboundCapable`(L349)、`webhookInboundRejectHints`(移出该渠道)、`dispatchToChannel`(L1130)、`Verify`(L663)、`webhookAccountTableByChannel`(L393 → `wecom_cs_accounts`)。
  - 必做：新渠道常量要同步 `migrations/047_pg_enums.sql`（否则 `check-enum-consistency.sh` 红）＋ `migrations/063` 式的 `ALTER TYPE ... ADD VALUE IF NOT EXISTS` 增量脚本（离线 SQL 部署会拒绝未知枚举值）。
  - 验收：`TestWebhookCapability_WecomcsAccepted`、`TestWebhookDispatch_Wecomcs`、租户解析失败 4xx 且不落库、`bash scripts/check-enum-consistency.sh` 绿。

- [ ] **T704 出站 sender**（P0｜依赖 T701/T703｜M｜🟡）
  - 改动：`outbound_sender_wecomcs.go`——`SendMsg`（text/image/link/miniprogram），被动窗口超时自动转主动（`48h` 限制处理为可判定错误）。
  - 验收：`TestWecomcsSender_PassiveVsActive`、`TestWecomcsSender_OutOfWindowReturnsChannelError`。

- [ ] **T705 能力表登记**（P0｜依赖 T704｜S｜🟡）
  - 改动：`WebhookChannel` 增 `wecomcs`、`model.Channel` 增常量、`channelcaps` manifest、`capregistry` 条目；`docs/channel-capabilities.md` 重生成。
  - 验收：`TestEveryChannelConstantHasManifest` 仍绿；能力表新行 `via=webhook`。

- [ ] **T706 全链路测试**（P0｜依赖 T701–T705｜M｜🟡）
  - 验收：验签失败 4xx 不落库 / 同 msgid 幂等 / 私聊与群聊各一条回放 / 未授权租户 403 / `go test ./internal/service/ -run Wecomcs` 全绿。

- [ ] **T707 真实账号验收**（P0｜依赖 T706｜S｜🔵）
  - 前置：企业微信客服账号 + 可信回调域名。
  - 验收：真实客户发消息 → 收件箱出现 → AI 回复送达；客服端可见会话。

---

## Phase 8 OneBot v11（P0）

- [ ] **T801 `channelbot/onebot` 客户端**（P0｜依赖 T101｜L｜🟡）
  - 改动：`channelbot/onebot/onebot.go`——HTTP 模式 `SendPrivateMsg/SendGroupMsg`（带 `echo` 关联与超时）、事件解析（`post_type`: message/notice/request/meta_event）、`VerifyToken`（`Authorization: Bearer` 或 `access_token`）。纯协议层，只用 `core.BaseClient`。
  - 必做：OneBot 需要独立账号表 `onebot_accounts`（迁移 `069_onebot_accounts.sql`：self_id/access_token/http_url/ws_enabled/ai_agent_enabled）——`qq_accounts` 是 QQ 官方机器人（app_id/app_secret），字段语义不同不能复用；同样要过 `047` 枚举 + `allModels()` + `check-enum-consistency.sh`。
  - 验收：httptest 回放四种 `post_type`；`echo` 响应匹配到请求；token 缺失/错误返回可判定错误。

- [ ] **T802 反向 WS 接入**（P0｜依赖 T801｜L｜🟡）
  - 改动：`internal/channelgw` 或 `router` 下新增 `/ws/onebot/{account_id}`（复用现有 WS 升级工具），维护 `account_id → conn` 表，断线自动移除；发送优先走已有连接，无连接回落 HTTP。
  - 验收：`TestOnebotWS_ConnectSendReceive`（本地 ws client 模拟 NapCat）、`TestOnebotWS_ReconnectReplacesConn`。

- [ ] **T803 入站 handler + 登记**（P0｜依赖 T801/T802｜L｜🟡）
  - 改动：`service/webhook_channel_onebot.go`——`message`（private/group）→ `Ingress`，`notice`（`group_recall` → `message.deleted`、`group_increase/decrease` → 成员事件），`request`（加好友/入群 → 事件总线，暂不自动同意）。
  - 验收：`TestOnebot_InboundPrivateAndGroup`、`TestOnebot_RecallMapsToMessageDeleted`、`TestOnebot_RequestDoesNotCreateMessage`。

- [ ] **T804 出站 sender**（P0｜依赖 T803｜M｜🟡）
  - 改动：`outbound_sender_onebot.go`——私聊/群聊选择、CQ 码转义（`content` 中 `[CQ:` 转义防注入）、长文切块复用 T402。
  - 验收：`TestOnebotSender_EscapesCQCode`、`TestOnebotSender_GroupVsPrivate`。

- [ ] **T805 能力表 + 文档**（P0｜依赖 T804｜S｜🟡）
  - 改动：`channelcaps` manifest（事件含 `message.received/deleted`、成员事件；特征 `group/mention`）；`docs/channel-onboarding.md` 增 NapCat 接入步骤。
  - 验收：能力表生成一致；文档链接可达。

- [ ] **T806 真实账号验收**（P0｜依赖 T805｜S｜🔵）
  - 前置：NapCat/Lagrange 实例 + 一个 QQ 号。
  - 验收：私聊 + 群 @ 两条真实消息 → 收件箱 → 回复送达；断线重连后仍能发。

---

## Phase 9 Slack（P1）

- [ ] **T901 `channelbot/slack` 客户端**（P1｜依赖 T101｜M｜🟡）
  - 改动：`channelbot/slack/slack.go`——Events API 签名校验（`v0=` + `v0:{ts}:{body}` HMAC-SHA256 + 5 分钟时间窗）、`PostMessage(channel, text, threadTS)`、`OpenConversation`；账号表 `slack_accounts`（迁移 `071_slack_accounts.sql`：tenant_id/team_id/bot_token/signing_secret/app_id/ai_agent_enabled/status）+ `allModels()` + `047` 枚举 + `webhookAccountTableByChannel` 行。
  - 验收：`TestSlack_VerifySignature`（含过期时间戳拒绝）、`TestSlack_PostMessageThread`、`bash scripts/check-enum-consistency.sh` 绿。

- [ ] **T902 入站 handler + 登记**（P1｜依赖 T901｜M｜🟡）
  - 改动：`webhook_channel_slack.go`——`url_verification` challenge 短路（在验签后）、`app_mention`/`message.im` → 消息、`message_changed`/`message_deleted` → 事件类型；过滤 `bot_id`/`subtype`。
  - 验收：`TestSlack_ChallengeShortCircuit`、`TestSlack_IgnoresBotMessages`、`TestSlack_ThreadReplyConversationKey`。

- [ ] **T903 出站 sender + 能力表**（P1｜依赖 T902｜M｜🟡）
  - 改动：`outbound_sender_slack.go`（`thread_ts` 回复入同一 thread）+ manifest + capregistry。
  - 验收：`TestSlackSender_RepliesInThread`、能力表新行。

- [ ] **T904 真实工作区验收**（P1｜依赖 T903｜S｜🔵）
  - 前置：Slack app（`chat:write`、`app_mentions:read`、`im:history`）+ 公网回调。

---

## Phase 10 企业微信智能机器人 wecombot（P2）

- [ ] **T1001 回调解密客户端**（P2｜依赖 T101｜M｜🟡）
  - 改动：`channelbot/wecombot/wecombot.go`——AES-CBC 解密（`EncodingAESKey` 43 位 → 32 字节）、签名校验、`PushMessage(webhook_url)`；账号表 `wecombot_accounts`（迁移 `072_wecombot_accounts.sql`：tenant_id/token/aes_key/webhook_url/ai_agent_enabled/status）+ `allModels()` + `047` 枚举。
  - 验收：官方样例密文解密用例（固定向量）+ 签名错误用例。

- [ ] **T1002 入站 + sender + 能力表**（P2｜依赖 T1001｜L｜🟡）
  - 改动：`webhook_channel_wecombot.go`（`message`/`enter_chat`/`template_card_event`/`feedback`）+ `outbound_sender_wecombot.go`（markdown 消息体）。
  - 验收：`TestWecombot_TemplateCardEvent`、`TestWecombotSender_MarkdownBody`。

---

## Phase 11 通用入口 custom（P1）

- [ ] **T1101 入站契约 + 验签**（P1｜依赖 T203｜M｜🟡）
  - 现状：`ChannelCustom` 常量已存在（`webhook_channel_telegram.go:82`），但无账号表、无验签、无入站适配器。
  - 改动：`POST /api/webhook/custom/{account_id}`——JSON `{event_id, channel_msg_id, conversation_id, sender:{id,name}, msg_type, content, media_url, timestamp, is_group, group_id}`；`X-Hive-Signature: t=..,v1=..`（HMAC-SHA256，5 分钟窗，复用 `core.SecureEqual`）；新建 `model.CustomAccount`（表 `custom_accounts`，迁移 `070_custom_accounts.sql`：tenant_id/secret/callback_url/ai_agent_enabled/status）+ `allModels()` 登记 + `webhookAccountTableByChannel` 增行。
  - 验收：`TestCustomInbound_SignatureRejects`（错签/过期 → 401 且不落库）、`TestCustomInbound_Idempotent`、`TestCustomInbound_ContractValidation`（缺字段 400）。

- [ ] **T1102 出站回调 sender**（P1｜依赖 T1101｜M｜🟡）
  - 改动：`outbound_sender_custom.go`——POST 账号 `callback_url`，签名同款，失败走 `enqueueSendRetry`。
  - 验收：`TestCustomSender_SignsCallback`、`TestCustomSender_RetryOn5xx`。

- [ ] **T1103 移除拒绝提示 + 文档**（P1｜依赖 T1102｜S｜🟡）
  - 现状：启用 custom 会打破三处现存断言——`webhook.go:358-372` 拒绝提示、`webhookInboundCapable`(L349) 无该分支、`Verify`(L663) 无该分支；另有 `internal/controller/webhook_batchc_d03_http_test.go:75` 断言 `/api/webhook/custom/1` 返回「无入站适配器」。
  - 改动：上述四处一并更新（提示删除、能力表加分支、Verify 加验签分支、测试断言改为成功路径或 401）。
  - 验收：`grep -c '不触发 AI' internal/service/webhook.go` = 0；`TestWebhookCapability_CustomAccepted` 绿；`go test ./internal/controller/ -run BatchC` 绿。

---

## Phase 12 门禁与文档收口

- [ ] **T1201 接入触点自动化校验**（P1｜依赖 Phase 7–11｜M｜🟡）
  - 改动：`scripts/check-channel-onboarding.py`——对 `channelcaps.All()` 每个渠道校验触点存在（`model.Channel` 常量、`channelcaps` manifest、`webhookAccountTableByChannel` 行、`webhookInboundCapable` 分支、sender 注册、client 包），缺失即失败并打印清单；`webhookAccountTableByChannel` 表名读源码符号而非硬编码字符串。
  - 必做：脚本自身要被 CI 调用（沿用现有 `check-*.sh` 的接入方式），否则等于没有。
  - 验收：删任一触点 → 失败并列出具体渠道+缺失项；全量 → 通过。

- [ ] **T1202 `capregistry` ↔ `channelcaps` 一致性测试**（P1｜依赖 T1201｜S｜🟡）
  - 改动：`internal/capregistry` 测试：`capregistry` 声明的平台必须在 `channelcaps` 有 manifest，反之允许（channelcaps 更细）。
  - 验收：`go test ./internal/capregistry/` 绿。

- [ ] **T1203 文档与索引**（P1｜依赖 T1201｜S｜🟡）
  - 改动：`docs/channel-capabilities.md` 重生成（含事件/渲染字段）、`docs/channel-onboarding.md` 定稿、`docs/INDEX.md` 登记、`specs/README.md` 加 024 行。
  - 验收：文档生成测试绿；README 表格行存在。

- [ ] **T1204 全量门禁**（P1｜依赖全部｜S｜🟡）
  - 验收命令：
    ```
    cd user-server && go build ./... && go vet ./... \
      && go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ ./internal/channelgw/... ./internal/router/ ./internal/capregistry/ \
      && cd .. && python3 scripts/check-layer-boundary.py \
      && python3 scripts/check_model_migration.py \
      && bash scripts/check-enum-consistency.sh \
      && bash scripts/check-naked-goroutine.sh
    ```
  - 验收：全绿；`delivery.md` 记录命令与结果（含 `check-enum-consistency.sh` 的 2 条既有 warning，新增渠道后不得新增 warning）。

---

## 交付顺序（批次）

| 批次 | 任务 | 目的 | 可独立发布 |
|---|---|---|---|
| B1 | T101–T112 | 出站接缝，零行为变更 | 是 |
| B2 | T201–T207 | 事件类型，编辑/撤回不再伪装文本 | 是 |
| B3 | T301–T305 | 路由规则，默认行为不变 | 是 |
| B4 | T401–T404、T501–T502 | 渲染统一 + 身份键修正 | 是（T501 需人工确认去重结果） |
| B5 | T601–T606 | 生命周期读侧 | 是 |
| B6 | T701–T707（wecomcs） | 首个新平台，验证接缝 | 是 |
| B7 | T801–T806（OneBot） | QQ 私域缺口 | 是 |
| B8 | T901–T904（Slack）、T1001–T1002（wecombot） | 国外平台 + 智能机器人 | 是 |
| B9 | T1101–T1103、T1201–T1204 | 通用入口 + 门禁收口 | 是 |

外部腿（🔵）汇总：T707 企微客服真实账号、T806 NapCat QQ 号、T904 Slack 工作区。

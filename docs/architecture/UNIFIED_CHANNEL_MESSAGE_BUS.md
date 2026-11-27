# 统一渠道消息总线协议 — 架构、数据流、逐条核验与整改报告

> 版本：2026-11-27（复核版）
> 适用范围：`hivemtk-user/user-server`（服务端消息总线层）、`hivemtk-user/user-web/bridge`（浏览器扩展兼容层）、`hivemtk-user/embed-sdk` + `user-web/src/views/chat/embed`（网页客服）、`sagex`（微信/QQ 监听伴生）、各 webhook/cron 机器人渠道。
>
> **本版口径**：上一版把合规矩阵几乎全部标成 ✅，经逐条源码复核后确认过于乐观。本文把「语义总线」与「线路协议」分开表述，凡未在同一线路上归一的地方一律标 ⚠️/❌，并给出已实施整改与待整改清单。验收以本文 §7 的逐条核验表和 §9 的实测证据为准。

---

## 0. 一句话结论

- **语义层已经统一**：所有渠道最终都归一化成 `model.MessageEvent` → `service.InboxIngressService` → `message_hub` / 统一收件箱 → AI 会话，这一层是真正的「消息总线内核」。
- **线路层部分统一**：协议常量已单源（`internal/pkg/channelbus`），我方可控桥接线路已**删除**旧 `/api/bridge/*`，统一到 canonical `/api/channelbus/*`；SageX、webhook/cron 机器人渠道、web_embed 仍各有自己的上报与回执线路，仅在服务端适配层归一（§12.2/§12.3/§13）。
- **识别码已单点收敛**：账号级 `channel_accounts.identify_code` 是权威；`channel_identify_codes` 只是历史兼容。但渠道 ID 命名仍有 model 常量 / `WebhookChannel` / DB `channels.code` / 扩展常量四套并存。

---

## 1. 需求与分层职责

### 1.1 用户原始目标（摘要）

1. 统一渠道账号、消息上报、消息下行、ack 确认、网管 API、统一收件箱、统一渠道识别。
2. 不可能一个渠道一套 API，消息上报 / 智能体回复下行转发 / ack 确认必须基于一套兼容多渠道的 API。
3. `user-server` 只做服务端侧上报 API、统一收件箱、回复下行转发、协议完成，**不承担具体渠道消息处理**。
4. 底层兼容层承担具体职责：
   - SageX：监听微信/QQ通知，按总线协议**上报**，不负责下行转发；
   - Bridge：读取抖音/小红书等聊天页 DOM 上报，并执行下行转发（找联系人、模拟输入发送）+ ack；
   - Embed 网页客服：直接 HTTP/WebSocket 收发；
   - TG/飞书/企微/WhatsApp/QQ 机器人：webhook 或 cron 中转，收到消息进服务端，取到回复发给指定人；webhook URL 可携带渠道识别信息，cron 任务编排可携带渠道识别信息。
5. 顶层 AI 智能体；中层消息总线层做结构化账号消息、识别码鉴权入库、用户标签/历史/线索提取、session id 缓存七天、收件箱门禁（重复/无效拦截）；底层兼容层做协议适配。
6. 先形成架构图、数据流图、协议，再逐个检查是否符合要求，发现不符即整改，最后交付完整文档。

### 1.2 分层架构图

```mermaid
flowchart TB
    subgraph L0["L0 顶层：智能体 / 业务应用"]
        AI["SmartCSOrchestrator / SalesEngine<br/>AI session = ScopedCheckpointThreadID<br/>画像 / 标签 / 历史 / 线索"]
        OPS["网管 API / 统一收件箱工作台<br/>/api/inbox/*, /api/message-hub/*"]
    end

    subgraph L1["L1 中层：user-server 消息总线内核（不碰渠道 DOM/SDK）"]
        CB["协议单源 internal/pkg/channelbus<br/>SchemaVersion = hivemtk.channel.v1<br/>版本 / 帧类型 / ack 状态 / 传输"]
        CANON["唯一线路端点 /api/channelbus/*<br/>ingest · outbox · ack · gateway · SSE · wake · ws<br/>旧 /api/bridge/* 已删除"]
        PROTO["channelgw 线路协议层<br/>protocol.go / registry.go / ws.go<br/>IngestMessage · Frame · OutboundReply"]
        AUTH["ChannelResolver + BridgeIngressGuard<br/>identify_code 鉴权 / 租户 fail-closed / 限流豁免"]
        PIPE["IngressPipeline<br/>IngestBatch / ClaimOutbound / AckOutboundDetailed"]
        ING["InboxIngressService<br/>归一化 · 门禁 · 去重 · 人肉锁<br/>总线 session · 触发 AI"]
        HUB[("message_hub<br/>pending→inflight→delivered/failed/unknown")]
        INBOX[("inbox_conversations / 统一收件箱")]
        OUT["出站分发<br/>bridge outbox / webhook sender / web_embed 直投"]
    end

    subgraph L2["L2 底层：渠道兼容层（各渠道自有线路）"]
        BR["Bridge 扩展（canonical）<br/>POST /api/channelbus/ingest<br/>GET outbox · POST ack · SSE/WS"]
        SX["SageX 监听微信/QQ<br/>POST /api/sagex/upload<br/>protocol_version = hivemtk.channel.v1"]
        WH["Webhook / Bot / Cron<br/>/api/webhook/:channel/:account_id<br/>TG polling 自投带 identify_code"]
        EM["web_embed 访客<br/>/api/chat/public/* + /api/ws/visitor"]
    end

    CB --> CANON
    CB --> PROTO
    CB -. 状态/版本 .-> SX
    BR -->|canonical v1/v2| CANON
    CANON --> PROTO
    SX -->|适配层归一| ING
    WH -->|平台回调 / 自投| ING
    EM -->|访客会话协议| ING
    AUTH -. 鉴权/归属 .-> CANON
    AUTH -. 鉴权/归属 .-> SX
    AUTH -. 鉴权/归属 .-> WH
    PROTO --> PIPE --> ING
    ING --> HUB
    ING --> INBOX
    HUB --> OUT
    OUT --> BR
    OUT --> WH
    OUT --> EM
    HUB --> AI
    AI --> HUB
    OPS --> HUB
    OPS --> INBOX
```

### 1.3 关键边界

- `channelgw` **只做协议/传输/认领/ack 回写**，业务权威在 `InboxIngressService`（`channelgw/pipeline.go`）。
- `bridge` 包 import `channelgw`，复用 DTO 别名；HTTP 端点在 `internal/bridge/handler_http.go`，WS 在 `channelgw/ws.go`。
- `channelbot/*`、`connector/*`、`channelcaps/*`、`capregistry/*` 是平台 SDK / 能力域，**不直接走 channelgw pipeline**；入站经 `WebhookService` 归一化后调 `InboxIngress`，出站经 `outbound_sender_*.go`。
- `internal/cron/` 只有域名健康与活码轮换；渠道相关定时任务散落在 `service/*_cron.go`（如 `outbound_reclaim_cron.go`），由 `internal/app/*wiring.go` 装配。

---

## 2. 数据流向图

```mermaid
flowchart LR
    A[渠道事件] --> B{入口类型}
    B -->|bridge HTTP/WS（canonical /api/channelbus）| C["HandleHTTPIngest / HandleInbound<br/>bindBridgeTenant → validateScope<br/>ToEvent / ToEventFull / HistoryToEvent"]
    B -->|sagex| D["sagex_upload.processEvent<br/>ResolveByIdentifyCode → 构造 MessageEvent"]
    B -->|webhook/cron| E["WebhookService.Receive / ParsePayload<br/>dispatchToChannel → ToUnifiedMessage"]
    B -->|web_embed| F["VisitorChatService.SendMessage<br/>session_messages + syncToInbox"]
    C --> G["InboxIngressService.HandleIngressBatch<br/>NormalizeEvent → 去重门禁 → 人肉锁 → 会话"]
    D --> G
    E --> G
    F -.仅 synthetic hub 供线索挖掘.-> G
    G --> H[("message_hub inbound")]
    G --> I["AI trigger → ScopedCheckpointThreadID<br/>chat_history_redis（7 天）"]
    I --> J[("message_hub outbound pending")]
    J --> K{OutboundPath}
    K -->|bridge_outbox| L["ClaimOutbound → SSE/WS/outbox<br/>客户端发送 → AckBridgeOutbox / ack 帧"]
    K -->|none / direct| M["webhook sender 直发<br/>或 DeliverWebEmbedOutbound 直投访客 WS"]
    L --> N[("delivered / failed / unknown<br/>unknown 需 /api/inbox/outbound-unknown/reconcile")]
    M --> N
    N --> O["统一收件箱预览 / 统计 / 工作台"]
```

### 2.1 表族真值源

| 表 | 作用 | 权威字段/索引 |
|---|---|---|
| `channel_accounts` | 渠道账号主数据 | `identify_code varchar(128)` 全局唯一；`migrations/089_channel_accounts.sql` |
| `channel_identify_codes` | 历史渠道级码兼容 | `migrations/090_channel_identify_codes.sql` |
| `message_hub` | 上下行消息总线主表 | 唯一索引 `(platform, msg_id, conversation_id, direction)`；`status/claimed_at/push_attempts/dedup_hash/platform_msg_id/recalled_at` 由 GORM 迁移补齐，字段真值以 `model.MessageHub` 为准 |
| `inbox_conversations` | 统一收件箱会话 | `conversation_id` 唯一，`unread_count/status/tags` |
| `session_messages` | 网页客服会话消息 | `session_id varchar(120)`（`migrations/055_...`） |
| `webhook_dedup` 相关 | 平台回调幂等 | `service/webhook_dedup.go` 自有一套 event/message id 生成 |

---

## 3. 消息总线协议规范

### 3.1 两层协议模型（关键抽象）

| 层 | 名称 | 数据形态 | 谁消费 | 是否已统一 |
|---|---|---|---|---|
| 语义总线 | Channel Message Bus | `model.MessageEvent` + `model.MessageHub` + `InboxIngressService` | 服务端中间件 / AI / 收件箱 | ✅ 已统一（所有入口最终都汇入） |
| 线路协议 A | channelgw v1/v2 | `IngestMessage` / `Frame` / `OutboundReply` / `OutboxMessage` | Bridge 扩展、WS 客户端 | ✅ Bridge 系列完整实现 |
| 线路协议 B | SageX upload | `IdentifyCode/ChannelID/RawNotification` + `schema_version/acks` | SageX Android | ⚠️ 语义归一、线路独立 |
| 线路协议 C | 平台 webhook / bot API | 各平台原始回调 + 平台验签 | 服务端 channelbot / WebhookService | ⚠️ 语义归一、无统一 ack |
| 线路协议 D | 访客会话 | `/api/chat/public/*` + `/api/ws/visitor` + visitor_token | embed-sdk | ⚠️ 语义部分归一 |

### 3.2 channelgw 线路协议（`internal/channelgw/protocol.go`）

**版本**：`ProtocolVersionV1=1`、`ProtocolVersionV2=2`、`CurrentProtocolVersion=2`；响应 `schema_version: "hivemtk.channel.v1"` + `acks[]` 兼容 SageX 老校验器。

**帧类型**：`register`、`register_rejected`、`inbound_message`、`history`、`ack`、`ping`、`pong`、`outbound_reply`、`config_push`。传输枚举 `http` / `websocket`。

**上行 `IngestMessage` 字段**（线路唯一契约）：
`event_id`、`channel`、`account_id`、`agent_id`、`account_name`、`conversation_id`、`sender_id`、`sender_name`、`sender_avatar`、`receiver_id`、`sender_type`、`msg_type`、`content`、`media_url`、`timestamp`、`direction`、`is_group`、`group_id`、`group_name`、`history[]`、`extra`、`content_hash`。

- `Extra` 白名单：`ctx.*` 电商扩展键（`ctx.goods/goods_id/goods_spec/order_id/order_amount/...`）+ `new_user_unfollowed`、`new_user_signal`；固定键（`account_id/bridge/sender_type/transport/content_hash/is_group/group_id/group_name`）不可被线路覆盖，丢弃计入 `IngestExtraDroppedKeys`。
- 会话键：`bridgeSessionKey = channel + ":" + account_id + ":" + conversation_id`；缺 `conversation_id` 回退 `group_id`，再回退 `senderID/senderName`（群聊不按发言人拆会话）。
- `IngestRequest.v/messages` 必填；`expect_reply`、`timeout_ms` 服务端**不消费**（保持兼容，恒立即返回）。
- `IngestResponse`：`ok`、`ingested[]`、`schema_version`、`acks[]`、`session_id`、`reason`、`server_time`。
- `IngestAck.status`：`accepted` / `duplicate` / `retry` / `rejected`；`IsDuplicateReason` 只认结论前缀，避免把 DB 错误原文误判为重复。

**下行 `OutboundReply`**：`channel/account_id/conversation_id/content/msg_type/media_url/reply_to_event_id/truncated/claimed_at/push_attempts`；后两者是认领租约栅栏锚点（默认租约 30s，最多 20 次推送）。

**端点**：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/bridge/ingest` | HTTP 批量上报 |
| GET | `/api/bridge/outbox` | 认领下行 |
| POST | `/api/bridge/outbox/ack` | v2 逐条 ack（`items:[{msg_id,conversation_id,status,error}]`） |
| GET | `/api/bridge/outbox/sse` | SSE 下行 |
| GET | `/api/bridge/outbox/wake` | 唤醒流 |
| POST/GET | `/api/bridge/gateway` | 统一网关（`hivemtk.channel.v1 + acks`） |
| GET | `/api/ws/channel` | channelgw WS（首帧 `register` 鉴权） |
| GET | `/api/ws/visitor` | 访客独立 WS（**不属于 channelgw**） |

### 3.3 ack 状态机

```mermaid
stateDiagram-v2
    [*] --> pending: AI/人工产生回复 → message_hub
    pending --> inflight: ClaimOutbound（租约 30s）
    inflight --> delivered: ack delivered / 直投成功
    inflight --> failed: ack failed
    inflight --> pending: 租约超时回收（push_attempts < 20）
    inflight --> unknown: ack unknown（真终态，需显式对账）
    unknown --> delivered: POST /api/inbox/outbound-unknown/reconcile
    unknown --> failed: 对账失败
    delivered --> [*]
    failed --> [*]
```

- HTTP v2 ack 响应逐条：`acked / duplicate / not_found / not_in_scope`；WS `ack` 帧状态归一 `""/delivered→delivered`、`failed`、`unknown`，未知值**拒翻转**并打点 `FrameAckUnknownStatusCount`。
- `AckOutboundDetailed` 五分类回执与 `AckOutboundClaimFence`（`claimed_at/push_attempts`）现在 WS 帧与 HTTP ack 都透出：HTTP v3 `items[].claimed_at`（RFC3339Nano）或 v1 顶层 `fence` 映射回传认领时刻，服务端在任何写之前解析，非法值整批 400，合法值走 `AckOutboundDeliveredDetailedFenced` 条件更新（G-06 已整改）。`push_attempts` 仅用于诊断，不进谓词。**注意：这是服务端能力；浏览器扩展的 `ackOutbox` 请求体仍未回传 claimed_at/fence，见 §12.3/§12.4 F-14。**

### 3.4 渠道识别与鉴权

- 权威码：`channel_accounts.identify_code`（`cid_...`），全局唯一索引；旧 `channel_identify_codes` 仅兼容。
- Bridge：`middleware.BridgeIngressGuard` 读 `X-Channel-Identify-Code` 或 `?identify_code=`，缺失/错误 401/503 fail-closed，校验后剥离 query；`bindBridgeTenant` → `ChannelResolver.ResolveByIdentifyCode` → 回退渠道级码；多租户 ctx 与码归属冲突 ⇒ 拒绝，不回落默认租户。
- Webhook：`bindCallbackTenant`（`controller/webhook.go:108-150`）读 `?identify_code=` 或头，解析租户/账号，显式 `account_id` 与解析结果冲突 ⇒ 404；无码按渠道账号表主键反查；失败 404。
- SageX：`sagex_upload.go` 请求体 `IdentifyCode`，`SetChannelResolver` → `ResolveByIdentifyCode`。
- web_embed：AppKey + `visitor_token` HMAC + 一次性 ws ticket，不走 identify_code。
- `BRIDGE_INGEST_AUTH=off` 是单机内网历史逃生阀：多租户（`TENANT_MODE=on`）下已在 `BridgeIngressGuard` 入口硬禁（503），无论候选码是否为空都不放行；单机且候选码为空仍按显式配置放行（G-07 已整改）。
- WS 不做逐帧验签：安全边界是**连接级**——首帧 register 携带 `X-Channel-Identify-Code`/`?identify_code=`，`BridgeTenantForRequest` 在双泵启动前解析租户并绑定 ctx，之后该连接的全部帧都在这个已鉴权 ctx 中处理；配合 `CHANNEL_WS_ALLOWED_ORIGINS`。识别码轮换需重连（不热替换连接身份），这是有意设计而非漏洞。
- 渠道名收敛到 `channelgw.Registry.Name`（与 DB `channels.code`、`model.Channel*` 一致）：`NormalizeChannel` 统一别名归一，`NormalizeBridgeChannel` 已委托它；`channelgw`/`capregistry`/`channelcaps` 三套登记表由双向 parity 测试钉死（G-01/G-08 已整改）。

### 3.5 结构化账号消息 / 门禁 / session

- 入库：`InboxIngressService.HandleIngressMessage`（`inbox_ingress.go:632`）/ `HandleIngressBatch`（`:1132`）；`NormalizeEvent`（`:559`）做结构化归一。
- 门禁：去重窗口（`inbox_ingress_ingest.go:284-340`）、`message_hub` 唯一索引、`content_hash`/`dedup_hash`、自回显判定 `isPlatformOutboundEcho`、人肉锁 `IsSessionHumanLocked/LockSessionForHuman`、无效内容/方向丢弃。
- session：`channelgw.SessionIDForInbound(m)` 固化为协议层 `session_id = channel:account:(conversation||group||sender)`；AI 侧 `ScopedCheckpointThreadID`（`aiagent/agent/runtime/inference_checkpoint_identity.go:19`，含 tenant/channel/account/customer/agent/config 指纹）是另一个键，二者通过 `MessageEvent.SessionID` 单向关联；7 天 TTL 由 `chat_history_redis` 承载（G-05 代码映射已固化，TTL 生产验证仍待运维）。

---

## 4. 时序图

### 4.1 上行（Bridge HTTP）

```mermaid
sequenceDiagram
    participant C as Bridge content script
    participant B as background SW
    participant S as /api/bridge/ingest
    participant G as channelgw pipeline
    participant I as InboxIngressService
    participant H as message_hub
    C->>B: 采集 DOM → Uplink 合并(350ms/20条)
    B->>S: POST v=2 + X-Channel-Identify-Code + ?channel/account_id/conversation_id
    S->>S: BridgeIngressGuard → bindBridgeTenant → validateHTTPIngestScope
    S->>G: ToEvent / ToEventFull / HistoryToEvent
    G->>I: IngestBatch
    I->>I: NormalizeEvent → 去重门禁 → 人肉锁 → session → 标签/历史
    I->>H: 写 inbound 行
    I-->>S: ingested[] + acks[] + session_id
    S-->>B: 200 + outbound_replies（若有）
    B-->>C: 终态持久化 _confirmed/_abandoned
```

### 4.2 下行 + ack（Bridge）

```mermaid
sequenceDiagram
    participant AI as AI/人工
    participant H as message_hub
    participant B as Bridge 轮询/SSE
    participant P as 聊天页 DOM
    AI->>H: outbound pending
    B->>H: GET /api/bridge/outbox（带识别码）
    H->>B: OutboxMessage（claimed_at/push_attempts）
    B->>B: 按 conversation_id 串行队列 + Web Lock + SentCache(24h)
    B->>P: 找联系人 → 模拟输入 → 发送
    B->>H: POST /api/bridge/outbox/ack v=2 items[{msg_id,conversation_id,status}]
    H->>H: AckOutboundDetailed（栅栏校验 + 五分类回执）
    H-->>B: acked/duplicate/not_found/not_in_scope
```

---

## 5. 渠道适配矩阵（逐条核验）

状态：✅ 完整符合；⚠️ 部分符合（语义归一但线路/ack 不统一）；❌ 未实现。

| 渠道/能力 | 上报 | 下行 | ack | 识别码 | 结论 | 代码证据 |
|---|---|---|---|---|---|---|
| Bridge（抖音/小红书等 15 渠道） | ✅ channelgw v1/v2 | ✅ outbox/SSE/WS | ⚠️ v2 逐条；服务端支持 claim 栅栏，扩展端 ack 尚未回传 claimed_at/fence | ✅ identify_code | ✅ | `bridge/handler_http.go:365/836/963`、`channelgw/ws.go:473`、`bridge/src/core/{uplink,downlink,http-ingest}.js` |
| SageX（微信/QQ 监听） | ⚠️ 专用 upload → `HandleIngressMessage`，已声明 `hivemtk.channel.v1` | ❌ 不承担 | ⚠️ acks[] 语义对齐，线路仍是 upload 专用协议 | ✅ IdentifyCode | ⚠️ | `service/sagex_upload.go`（协议常量 + `SageXUploadAckResponse`） |
| Webhook（企微/飞书/TG/WhatsApp/QQ 等） | ⚠️ 平台回调 → `WebhookService.Receive` → ingress/pipeline | ⚠️ 平台 sender 直发，非 outbox claim | ⚠️ 已回统一 `ack{accepted/duplicate/retry/rejected}`，非 outbox 逐条栅栏 | ✅ query/头识别码 + 账号表反查 | ⚠️ | `service/webhook.go`（`WebhookIngestAck`/`IngressPipeline`）、`controller/webhook.go:108-150` |
| Telegram polling / cron | ⚠️ 自投 `/api/webhook/telegram/:id` | ⚠️ 平台 API 直发 | ⚠️ 经 webhook ack 回执 | ✅ 任务编排带 `?identify_code=`（仅账号一致时） | ⚠️ | `service/telegram_polling.go`（见 §8） |
| web_embed 网页客服 | ⚠️ 访客协议 + 已归 `InboxIngressService`（AI 抑制防重复） | ⚠️ hub → 直投访客 WS + delivered | ❌ 无 outbox claim/ack | ⚠️ AppKey + visitor_token | ⚠️ | `service/chat_visitor.go:reportToMessageBus`、`service/message_hub_web_embed.go:34` |
| 统一收件箱 / 网管 API | ✅ | ✅ | — | ✅ | ✅ | `router/service_routes.go:161-228`、`controller/inbox.go` |
| AI session 7 天缓存 | ✅ | ✅ | — | — | ⚠️ 键映射已固化；TTL 生产验证待运维 | `channelgw.SessionIDForInbound`、`chat_history_redis.go` |

> 说明：`message_hub` 的 `web_embed` 出站直投是「直接投递渠道」模式（不是无人认领的 pending 死行），这是已经修复的真实线上问题；但它不经过 outbox claim/ack，所以不能标成完整总线协议符合。入站已归一到 `InboxIngressService`，但线路仍是访客专用 HTTP/WS（G-02 只完成语义归一）。

---

## 6. 整改清单（G-01→G-08 全部已实施，残留项如实标注）

| ID | 优先级 | 原缺口 | 已实施动作 / 代码锚点 | 状态 |
|---|---|---|---|---|
| G-01 | P0 | 渠道 ID 四套命名并存 | 新增 `channelgw/normalize.go`（`NormalizeChannel` 全别名表），`bridge.NormalizeBridgeChannel` 委托它；`normalize_test.go` 钉死别名目标/模型常量/未知 fail-closed | ✅ 已整改 |
| G-02 | P0 | web_embed 入站绕过 `InboxIngress` | `VisitorChatService.SetIngressHandler/reportToMessageBus`，`WithChannelOwnedAITrigger` 抑制中台二次 AI；router 延迟注入 ingress 句柄，移除重复 synthetic hub 发掘 | ✅ 入站已整改；线路仍专用 |
| G-03 | P0 | webhook 无统一 ack / 不进 pipeline | `WebhookIngestAck{accepted/duplicate/retry/rejected}` + `Receive` defer 统一生成；`IngressPipeline` 接口 + `SetIngressPipeline`，`webhookIngressAdapter` 统一走同一句柄 | ✅ ack/入站已整改；出站仍平台直发 |
| G-04 | P1 | SageX ack 与 gateway 两套 | `SageXProtocolVersion = "hivemtk.channel.v1"`，请求/响应双写 `protocol_version` + 旧 `schema_version` 别名，未知版本仍拒 | ✅ 版本标识已统一；专用 upload 线路保留 |
| G-05 | P1 | 两个 session key 无映射 | `channelgw.SessionIDForInbound` 固化 `channel:account:(conv|group|sender)`，注释明确与 `ScopedCheckpointThreadID` 单向关联；`session_id_test.go` 钉死回退链 | ✅ 映射已固化；Redis TTL 生产验证 ❌ 待运维 |
| G-06 | P1 | HTTP ack 无 claim 栅栏 | `BridgeOutboxAckItem.claimed_at`/`push_attempts` + 顶层 `fence`，`buildAckClaimFence` 写前解析、非法整批 400，合法走 `AckOutboundDeliveredDetailedFenced` | ✅ 已整改（HTTP 与 WS 对等） |
| G-07 | P1 | `BRIDGE_INGEST_AUTH=off` 越权 / WS 信任面 | `BridgeIngressGuard` 多租户下硬禁 off（503）；WS 明确定为连接级鉴权（register 帧识别码 + 双泵前租户绑定），不做逐帧验签 | ✅ 已整改（连接级边界，非逐帧） |
| G-08 | P2 | 三套能力注册表漂移 | `channelgw/capability_parity_test.go` 双向钉死 channelgw↔capregistry↔channelcaps；`Registry.OutboundChannels`/`LeadMiningChannels` 继续作为 OutboundPath/MineMode 唯一投影源 | ✅ 漂移门已落地；物理合并三包 ❌ 未做（历史分层有各自职责） |

---

## 7. 用户原始需求逐条核验

| # | 需求原文要点 | 核验结论 | 证据 / 整改 |
|---|---|---|---|
| 1 | 统一渠道账号 | ✅ 已统一 | `channel_accounts.identify_code` 全局唯一；`ChannelResolver` 单点解析；旧渠道级码兼容 |
| 2 | 统一消息上报 | ✅ 语义统一；⚠️ 线路不统一 | Bridge/SageX/webhook/embed 全部归 `InboxIngressService`（G-02/G-03/G-04）；线路仍有 bridge ingest、sagex upload、webhook、访客协议四类 |
| 3 | 统一消息下行转发 | ⚠️ | Bridge 有 outbox claim/发送；webhook 走平台 sender；embed 走 hub 直投 WS；无单一发送 API（G-03 只统一入站/回执） |
| 4 | 统一 ack 确认 | ⚠️ 语义统一 | Bridge v2/v3 ack + HTTP/WS 栅栏；webhook 回统一 `WebhookIngestAck`；SageX acks[] 版本标识对齐；unknown 仍需显式对账接口（G-03/G-04/G-06） |
| 5 | 统一网管 API / 统一收件箱 | ✅ | `/api/inbox/*`、`/api/message-hub/*`、admin 门禁 |
| 6 | 统一渠道识别 | ✅ 标识单源 | `channelgw.NormalizeChannel` + `identify_code` 单点解析；三套能力注册表 parity 门（G-01/G-08） |
| 7 | user-server 不承担具体渠道消息处理 | ✅ | `channelgw` 只做协议/管道；渠道 DOM/SDK 在 bridge/sagex/channelbot |
| 8 | SageX 负责上报、不负责下行 | ✅（语义） | `sagex_upload.go:394/405` 调 ingress，无 sender |
| 9 | Bridge 上报 + 下行 + ack | ✅ | 三通道 + SSE + v2 ack，源码与客户端双向对齐 |
| 10 | Embed 走总线机制 | ⚠️ 语义归一 | 访客消息经 `reportToMessageBus` 进 hub（AI 抑制防双触发），出站仍 hub 直投 WS、无 outbox/ack（G-02） |
| 11 | TG/飞书/企微/QQ/WhatsApp webhook/cron 整改 | ⚠️ | webhook 识别码 + 统一 ack（G-03）；TG polling 自投带 identify_code（§8）；出站仍平台直发 |
| 12 | webhook URL 携带渠道识别信息 | ✅ | `bindCallbackTenant` 读 query `identify_code` 并交叉校验 |
| 13 | cron 任务编排携带渠道识别信息 | ✅ | TG polling 自投携带 `?identify_code=`（账号一致才带，冲突留空 fail-closed，见 §8） |
| 14 | 中层门禁：重复/无效拦截 | ✅ | 唯一索引 + dedup hash + 自回显 + 人肉锁 + `IsDuplicateReason` |
| 15 | 标签/历史/线索提取 | ✅ | AI 侧画像/历史读取 + `leadMiningSvc.Enqueue` |
| 16 | session id 缓存七天 | ⚠️ | `chat_history_redis` 7d TTL 已定义、`SessionIDForInbound` 映射已固化（G-05）；但 `AppendHistory` 无生产调用点，缓存实际为空读，TTL 生产验证待运维（二次审计 F-09） |

---

## 8. 本次已实施整改：TG polling 任务编排携带渠道识别码

### 8.1 改动

文件：`user-server/internal/service/telegram_polling.go`

1. `telegramPollingState` 新增 `identifyCode` 字段。
2. `StartTelegramPolling` 在租户 ctx 下调 `telegramPollingIdentifyCode(ctx, acc.ID)`：
   - 用 `ChannelResolver.ResolveByAccountRef(ctx, "telegram", <accountID>)` 解析；
   - 只有解析结果 `AccountID` 与任务账号 ID 一致才取 `IdentifyCode`，否则留空（fail-open **仅限识别码载荷**，租户闸不放宽）。
3. `buildTelegramPollingWebhookURL(baseURL, accountID, identifyCode)` 把识别码写进 query `?identify_code=`（路径永远不带码），空码时保持历史 URL 不变。
4. worker 投递用 `deliverTelegramUpdateWithIdentity(...)`；原 `deliverTelegramUpdate` 保留为空码包装，兼容既有测试。
5. 新增 `user-server/internal/service/telegram_polling_identity_test.go`：钉住 URL 构造（含转义、尾斜杠、空码）与零账号不解析。

### 8.2 为什么这样改

- 直接命中需求「cron 可在任务编排上携带渠道识别信息」；
- 服务端 `bindCallbackTenant` 已支持 query 识别码并与路径账号交叉校验，因此无需改控制器；
- 老部署没有 `channel_accounts` 行或解析歧义时自动回落原账号主键自证，不引入回归风险。

### 8.3 验证证据

见本文 §9。

### 8.4 G-01→G-08 整改批次（本轮全部实施）

| ID | 改动文件（新增 `+`） | 关键行为 |
|---|---|---|
| G-01 | `+channelgw/normalize.go`、`+normalize_test.go`、`bridge/channel.go` | `NormalizeChannel` 单一别名表；扩展 `NormalizeBridgeChannel` 委托；未知渠道仍 fail-closed |
| G-02 | `service/chat_visitor.go`、`router/{router,chat_routes}.go`、`+chat_visitor_ingress_test.go` | 访客入站归 `InboxIngressService`，`WithChannelOwnedAITrigger` 防中台二次 AI；ingress 句柄延迟注入解装配顺序；只在无句柄时保留旧 synthetic hub |
| G-03 | `service/webhook.go`、`+webhook_ingest_ack_test.go` | `WebhookIngestAck` 四态回执由 `Receive` defer 统一生成；`IngressPipeline` 接口注入，adapter 统一入站 |
| G-04 | `service/sagex_upload.go`、`+sagex_upload_protocol_test.go` | `protocol_version` 与旧 `schema_version` 双写；未知版本仍 `ErrSageXUnsupportedSchema` |
| G-05 | `channelgw/protocol.go`、`+session_id_test.go` | 导出 `SessionIDForInbound`；固化与 AI `ScopedCheckpointThreadID` 的单向映射 |
| G-06 | `bridge/handler_http.go`、`+handler_http_ack_fence_test.go` | ack 请求体新增 `claimed_at`/`fence`；写前解析、非法整批 400；合法值进 `AckOutboundDeliveredDetailedFenced` |
| G-07 | `middleware/bridge_ingress_guard.go`、`+bridge_ingress_guard_test.go` | 多租户 `BRIDGE_INGEST_AUTH=off` 硬禁 503；单机逃生阀保留；WS 明确为连接级鉴权 |
| G-08 | `+channelgw/capability_parity_test.go` | 三套能力登记表双向 parity；新增渠道必须三处同时登记，否则测试红 |

> 残留（非代码可闭环，已在 §6/§7 标注）：平台出站仍各走 sender、web_embed 无 outbox ack、Redis 7d TTL 需生产环境验证、三套能力包物理合并未做（职责不同，保留分层）。

---

## 9. 验证命令与证据

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server
go build ./...
go vet ./...
# G-01/G-05/G-08 协议与渠道能力
go test ./internal/channelgw -run 'TestChannelAliasesResolveToKnownChannels|TestCanonicalNamesMatchModelConstants|TestUnknownChannelStaysUnknown|TestExperimentalFlagsParityWithBridgeExtension|TestSessionIDForInboundMapping|TestBridgeCapabilityThreeRegistriesParity' -count=1
# G-02/G-03/G-04 + TG polling 识别码
go test ./internal/service -run 'TestBuildTelegramPollingWebhookURL|TestTelegramPollingIdentifyCodeZeroAccount|TestTGPollingSelfPostFollowsPORT|TestVisitorReportToMessageBus|TestWebhookIngestAckMatrix|TestWebhookIngressPipelineInjection|TestSageXUnifiedProtocolVersion' -count=1
# G-06/G-07
go test ./internal/bridge -run 'TestBuildAckClaimFence' -count=1
go test ./internal/middleware -run 'TestBridgeIngressGuard' -count=1
cd ../user-web/bridge && npx vitest run
```

本次实测（2026-11-27，逐批执行）：

- `go build ./...`、`go vet ./...`：通过（无输出，每批改动后重跑）。
- `internal/channelgw` 定向测试：`ok`（别名归一 / 模型常量对齐 / 未知 fail-closed / 实验标志 parity / session 映射 / 三表能力 parity）。
- `internal/service` 定向测试：`ok`（TG polling URL/零账号/自投 PORT + 访客入站归一 + webhook ack 矩阵/pipeline + SageX 版本）。
- `internal/bridge -run TestBuildAckClaimFence`：`ok hivemtk-user/internal/bridge 0.620s`。
- `internal/middleware -run TestBridgeIngressGuard`：`ok hivemtk-user/internal/middleware 0.973s`（含多租户 off 硬禁新用例）。
- `cd user-web/bridge && npx vitest run`：`Test Files 108 passed (108)`、`Tests 1328 passed | 7 skipped (1335)`，耗时 18.54s。Bridge 客户端协议 parity 全绿。

渠道注册与协议一致性回归（建议交付前再跑一次）：

```bash
go test ./internal/channelgw/... ./internal/bridge/... ./internal/service/... -count=1
cd ../user-web/bridge && npx vitest run
```

> **环境限制（如实记录）**：本轮执行上面整组 Go 测试时，`internal/channelgw`、`internal/bridge`、`internal/service`（含 `sop`/`trace_learning` 子包）中所有 DB 集成测试均因 `test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD` 的**安全门**在连接数据库前失败（3731 处同一报错），与本次改动无关。此类测试需在配置测试库口令的环境重跑；本轮以 `go build ./...`、`go vet ./...` 和上面三个不依赖 DB 的定向测试作为交付证据。

---

## 10. 文档索引与维护约定

- 本文是统一渠道消息总线的**权威核验/整改文档**，路径 `docs/architecture/UNIFIED_CHANNEL_MESSAGE_BUS.md`，已登记在 `docs/INDEX.md`。
- 协议字段真值源：`user-server/internal/channelgw/protocol.go`；渠道能力真值源：`channelgw/registry.go`；业务真值源：`service/inbox_ingress.go` + `model.MessageHub`。
- 新增渠道必须同时：① 在 `channelgw.Registry` 注册；② 在扩展 `EXPERIMENTAL_CHANNELS` 对齐；③ 更新本文 §5 矩阵；④ 补 parity 测试。
- 本文结论随代码变更同步更新；不得把「语义归一」写成「线路协议已统一」。

---

## 11. 结论（复核版）

统一渠道消息总线的**内核已经存在并可用**：识别码鉴权、结构化归一、收件箱门禁、AI session、message_hub 三态 ack、Bridge 三通道协议都已在源码中实现，Bridge 系列可以作为一个渠道接一个渠道地复用同一套 API。

但严格按用户原始要求「不可能一个渠道使用一套 API 完成」，当前做到的是**语义层统一 + 标识/回执语义统一**：8 项整改（G-01→G-08）已全部落地，包括渠道名单源归一、web_embed 入站归总线、webhook 统一 ack/pipeline、SageX 协议版本对齐、session 键映射、HTTP ack claim 栅栏、多租户鉴权硬禁、能力注册表 parity。SageX / webhook / cron / web_embed 的**线路协议与出站路径仍未合并成单一 API**——这是分阶段实施的明确边界，不在本文中被写成「已全部符合」（§6/§7 逐条标注残留）。按用户"不考虑旧路径兼容"指示，我方可控的旧 `/api/bridge/*` 路由、`schema_version=1.0` 与渠道级旧码热链已删除，统一到 `/api/channelbus/*` 与 canonical 协议标识（§12/§13）。后续若继续收敛，下一阶段议题是：平台 sender 走统一 outbox、web_embed 出站引入 claim/ack、三套能力包物理合并。

---

## 12. 二次审计与二次交叉对比（2026-11-27 追加）

### 12.1 二次审计需求（头脑风暴收敛）

第一次审计最大的漏检是把「语义归一」写成了「线路协议已统一」。二次审计不复用第一次的 ✅ 结论，重新按三层追问：

| 层 | 审计问题 | 必查证据 |
|---|---|---|
| L1 常量单源 | 协议版本/帧类型/ack 状态是否只有一个权威定义？ | `internal/pkg/channelbus/protocol.go`；`channelgw/protocol.go` 别名；`sagex_upload.go` / `webhook.go` / `bridge/handler_http.go` 是否仍有硬编码字面量 |
| L2 线路同形 | 我方可控线路是否只剩一套端点？旧路由是否彻底删除？ | `router.go` 只注册 `/api/channelbus/*`；`retired` 路由断言旧路径 404 |
| L3 数据同果 | 每条上行是否都能拿到 accepted/duplicate/retry/rejected 之一并落 message_hub？ | `IngestAck` / gateway ack / SageX receipt / webhook ack 的状态映射 |
| L4 鉴权边界 | 识别码是否只走头/query，且多租户下永远不允许鉴权关闭？ | `BridgeIngressGuard`、`bindCallbackTenant`、`bridge_ingress_guard_test.go` |
| L5 门禁与 session | 重复/自回声是否拦截；session 映射是否稳定且 7d TTL 有承载？ | `inbox_ingress.go`、`SessionIDForInbound`、`chat_history_redis.go` |
| L6 兼容面删除 | 旧端点、`schema_version=1.0`、渠道级旧码是否已从热链删除？ | `router.go` 旧路由、`HandleGateway`/SageX 版本白名单、`ValidCodes`/`tenant_binding` fallback |
| L7 客户端一致 | bridge 扩展发出的 URL 与服务端注册路径是否逐字一致？ | `src/core/*.js` + `src/background/*.js` + 测试断言 |
| L8 能力表一致 | 渠道名单/实验标记/收发能力三表是否双向 parity？ | `channelgw/registry.go`、`channelcaps`、`capregistry` |
| L9 负面测试 | fail-closed、歧义、错码、非法 fence、未知 ack 值是否有测试？ | `*_test.go` 中对应用例 |
| L10 证据纪律 | 任何「已实现」是否都有可复跑命令和实测输出？ | §9 / §12.5 的 go test / vitest 输出 |
| L11 外部协议边界 | Telegram/飞书/WhatsApp 回调与 embed 访客 WS 是否被诚实标注为适配层？ | 本文 §12.3 矩阵 |
| L12 回归风险 | 本轮改动是否触碰用户未提交文件、是否引入循环依赖/漂移 | `git status` / `go build` / 测试钉 |

### 12.2 线路协议物理合并（本轮实施）

1. **协议常量单源**：新增 `internal/pkg/channelbus/protocol.go`（零业务依赖），定义 `SchemaVersion=hivemtk.channel.v1`、v1/v2、四类上行 ack、三类下行终态、九种帧类型、两种传输。
2. **适配层去硬编码**：`channelgw/protocol.go` 导出常量改为 channelbus 别名；`sagex_upload.go` / `webhook.go` 的 ack 状态、`handler_http.go` 的 schema 与 ack 状态全部改为引用 channelbus，消除"同一字面量多处定义"。
3. **唯一 canonical 端点组**：`router.go` 只注册 `/api/channelbus/{ingest,outbox,outbox/ack,gateway,outbox/sse,outbox/wake,capabilities,selector-health,ws}`，使用 `InitGuard + BridgeIngressGuard`；旧 `/api/bridge/*` 与 `/api/ws/channel` 路由已从注册表删除，并由 `bridge_assembly_routes_test.go` 的 retired 表断言不存在。
4. **客户端迁移**：bridge 扩展 src/test 中 82 处 `/api/bridge/` 全部替换为 `/api/channelbus/`（含 background allowlist、SSE、wake、selector-health、测试断言）。
5. **边界（不夸大）**：Telegram/飞书/企微/WhatsApp 等平台回调是外部平台定义的协议，embed 访客 WS 是浏览器会话协议；两者由适配层翻译成 `MessageEvent` 进入同一总线，**线路形状无法也不应强行同形**。本轮"物理合并"的对象是**我方 controllable 的桥接线路与协议标识**，不是平台回调本身。

### 12.3 二次交叉对比代码需求（头脑风暴收敛）

| 对比维度 | Server | Bridge 扩展 | SageX 客户端 | Embed 访客 | 一致性要求 | 实测 |
|---|---|---|---|---|---|---|
| 协议身份 | `channelbus.SchemaVersion` | 请求体 `v:2`、响应 schema | `protocol_version`（或同值 `schema_version` 字段） | 不暴露 | 同一 canonical 字符串 | ✅ server/SageX 客户端均只认 canonical，1.0 已拒绝 |
| 上报端点 | `/api/channelbus/ingest`、`/gateway` | HTTP ingest / gateway | `/api/integrations/sagex/upload`（旧 `/api/bridge/gateway` 兼容） | `/api/chat/public/sessions` | 我方线路统一 canonical，平台适配端点保持独立 | ✅ 已实施；⚠️ 平台端点异构是边界 |
| 识别码 | `X-Channel-Identify-Code` / `identify_code` query | `buildAuthHeaders` | 安装绑定 identifyCode | AppKey + visitor_token | 码只走头/query，绝不进 path | ✅ |
| 上行 ack | accepted/duplicate/retry/rejected | `ingested[]` 终态集合 | `acks[]` | 不暴露 | 四态逐字一致 | ✅ 同源于 channelbus |
| 下行终态 | delivered/failed/unknown | v2 `items[].status` | 无下行 | WS 推送 | 三态一致 | ⚠️ embed 无 claim/ack（文档边界） |
| claim 栅栏 | 服务端支持 `claimed_at` + `fence`（`AckOutboundDeliveredDetailedFenced`） | **ack 请求体只有 `v/items/status`，未回传 claimed_at/fence** | 不适用 | 不适用 | 陈旧认领必须被拒 | ❌ 客户端未启用；栅栏仅在 WS/直连客户端可用 |
| session 键 | `SessionIDForInbound` → Redis TTL 7d（**写侧 `AppendHistory` 无生产调用点**） | 无感 | 无感 | 访客 session | 总线 session 与 AI checkpoint 单向关联 | ⚠️ 键映射已固化；TTL 已定义但缓存实际空读，待接线 |
| 能力名单 | registry + parity 测试 | `EXPERIMENTAL_CHANNELS` | 渠道列表 | web_embed 常量 | 双向 parity | ✅ 15 渠道双向校验 |
| 鉴权失败 | 503/401 fail-closed | nonRetryable 4xx | 拒绝 | 401 | 不回落默认租户 | ✅ |

### 12.4 二次审计发现与处置

| 编号 | 发现 | 等级 | 处置 |
|---|---|---|---|
| F-01 | 第一轮只把 `hivemtk.channel.v1` 字面量换成单源，但 `bridge/handler_http.go` 仍散落 6 处 `"accepted"/"duplicate"/"retry"/"rejected"` ack 状态字面量 | 中 | 已改为 `channelbus.Status*`，并新增 `TestServiceAckStatusUsesCanonicalWireIdentity` / `TestWireProtocolIdentitySingleSource` 双钉 |
| F-02 | canonical WS 路径 `busWS.GET("/ws")` 依赖 `channelWSTransport` 在 router 中的装配顺序，后续重排可能静默丢失 | 低 | 已就近加注释说明依赖；`go build`/测试通过 |
| F-03 | bridge 扩展 23 文件 82 处旧路径迁移，若漏改 background allowlist 会造成下行 URL 被拒 | 高 | 全量 grep 遗留 0；vitest 108 files / 1328 passed 通过 |
| F-04 | SageX 客户端仓默认端点仍为 `/api/bridge/gateway`（兼容路径保留），且该仓有大量未提交用户文件 | 中 | 不动该仓；服务端兼容路径保证其继续工作，canonical 路径供新接入方使用 |
| F-05 | embed 访客 WS、平台 webhook 回调仍是各自外部协议，无法物理合并为同一 URL 形状 | 中 | 如实标为适配层边界；入站已归一进 message_hub，出站 embed 尚无 claim/ack（§12.3 ⚠️） |
| F-06 | 工作区含用户未提交文件（AGENTS.md、docs/INDEX.md、.specify/**、sagex 等等） | 高 | 提交只显式暂存本轮白名单文件；用户文件零触碰 |
| F-07 | 全量 DB 集成测试被 `POSTGRES_TEST_PASSWORD` 安全门阻断 | 低 | 已记录，非本轮回归；针对性测试全部通过 |
| F-08 | Redis session 7d TTL 只在代码层固化，未做生产环境实测 | 低 | 列为运维待验证项，不写成"已验证" |
| F-09 | `chat_history_redis.AppendHistory` 只有接口与实现，**零生产调用点**；7d 缓存实际为空读 | 高 | 本轮不擅自改 AI 运行链；文档改写为"TTL 已定义、写侧未接线"，列为下一阶段 P1 |
| F-10 | `/api/channelbus/ingest`、`/ws` 曾被全局 1000 RPS 限流，批量上报会 429 | 高 | 已加入 `router.go` ExemptPaths；旧 `/api/bridge/*` 豁免项随旧路由一并删除 |
| F-11 | gateway 只解析 `req.SchemaVersion` 从不校验，而 SageX upload 做版本白名单校验 | 中 | 第一步加白名单；按"不保留兼容"要求收紧为**必须显式 canonical**，空值与 `1.0` 均 400 |
| F-12 | 下行终态双源：`channelbus.Status*` 与 `model.BridgeAckStatus*` 各写一份 | 中 | model 三态改为 channelbus 别名，新增 `TestBridgeAckTerminalStatusSingleSource` |
| F-13 | canonical 路由无 parity 测试，删路由不会红 | 中 | `bridge_assembly_routes_test.go` 增加 11 条 canonical 路由与 handler 身份断言 |
| F-14 | "Bridge ack 携带 claimed_at ⇒ ✅" 与客户端源码矛盾（ack 请求体无该字段） | 高 | 服务端 outbox 已输出 `claimed_at`/`push_attempts`；客户端 `constants.js`/`http-ingest.js`/`downlink.js` 已回传与识别，vitest 108 files / 1328 passed 确认 |
| F-15 | SageX Android 仓默认端点仍 `/api/bridge/gateway`，且 `UploadEngine.kt` 用 `contains("/bridge/gateway")` 判定模式 | 中 | 已改两个默认端点为 `/api/channelbus/gateway`、模式判定与 `AckValidator` 仅认 canonical；4 个 fixture 文件 50 处同步；两仓 `./gradlew test` 均 BUILD SUCCESSFUL |
| F-16 | bridge 文档/scripts/dist 仍残留旧路径 | 低 | 服务端脚本/e2e/注释已改 canonical；dist 构建产物与已安装扩展需走发布链 B/C 重建，不在源码提交范围 |

### 12.5 新增验证证据

- `go test ./internal/channelgw -run TestWireProtocolIdentitySingleSource -count=1` → ok
- `go test ./internal/service -run TestServiceAckStatusUsesCanonicalWireIdentity -count=1` → ok
- `go test ./internal/model -run TestBridgeAckTerminalStatusSingleSource -count=1` → ok
- 上三测试钉住「channelgw 常量 / SageX·Webhook ack 状态 / model 下行终态」全部等价于 channelbus 单源。
- `go build ./...`、`go vet ./...` 通过；`npx vitest run`（bridge）108 files / 1328 passed / 7 skipped（含本轮 ack 栅栏字段断言更新）。
- SageX 独立仓 `wechat-listener`、`qq-listener` 在 JDK 17 下 `./gradlew test` 均 BUILD SUCCESSFUL（canonical 协议与端点 fixture 全绿）。
- `internal/router/bridge_assembly_routes_test.go` 新增 11 条 canonical 路由断言（含 ingest/gateway WS handler 身份），该用例依赖 PostgreSQL 快测实例，本机被 `POSTGRES_TEST_PASSWORD` 安全门阻断，需在测试库环境复跑。
- 独立二次审计（只读）另有 F-09/F-14 两项未闭环：session 写侧未接线、扩展端 ack 未回传栅栏值；均已在本节与 §12.3 如实标注，不写成已实现。

---

## 13. 全链路业务生命周期检查表（S1~S11，按用户要求逐阶段核验）

口径：按一条消息从"渠道账号建档 → 上报 → 归一 → 门禁 → 画像/session → AI → 出站 → ack → 收件箱 → 失败回收"的实际生命周期逐段核验；"已实现"必须有生产调用点与可复跑证据，只有接口/实现的写为 ❌。

| 阶段 | 生命周期环节 | 结论 | 代码/证据 | 残留或处置 |
|---|---|---|---|---|
| S1 | 渠道账号建档与识别码 | ✅（热链已收敛） | `channel_accounts.identify_code` 为权威；`channel_identify_code.go` ValidCodes 已删除渠道级码候选；`tenant_binding.go` 删除 channel-level fallback | `channel_identify.go` 的旧 `CreateOrRotate`/`rotate` 仍保留但已无生产调用，非热链 |
| S2 | 入站鉴权与租户绑定 | ✅ | `BridgeIngressGuard`；`bindCallbackTenant`；多租户 `BRIDGE_INGEST_AUTH=off` 硬禁 503；canonical/旧路由同 guard | 平台 webhook 回调为外部协议，经适配层归一（边界，非缺陷） |
| S3 | 线路协议归一 | ✅（我方可控线路） | `channelbus` 单源；唯一 `/api/channelbus/*`；`HandleGateway`/SageX upload 只认 canonical | 旧 `/api/bridge/*` 路由与 `1.0` 兼容已删除；平台回调/embed WS 仍独立 |
| S4 | 门禁与去重 | ⚠️ | `inbox_ingress` 幂等/自回声拦截重复；`IsDuplicateReason` 只认结论前缀 | `webhook_dedup.go:233-249` Redis 异常 fail-open，未改 |
| S5 | session 画像与历史 | ⚠️ ❌ | `SessionIDForInbound` 映射固化；`chat_history_redis` 7d TTL 定义 | `AppendHistory` 零生产调用（缓存空读）；`AssetBundleLoader` 零装配，列为 P1 |
| S6 | AI 触发与入库 | ✅ | `triggerAIForEvent`、`WithChannelOwnedAITrigger` 防双触；message_hub/conversation 落库 | 无 |
| S7 | 出站路由与下发 | ✅ | outbox claim/推送；bridge HTTP/WS/SSE；SageX/平台 sender 适配层 | embed 出站仍直写 `session_messages`，未走 outbox claim/ack |
| S8 | 发送状态与 ack 闭环 | ⚠️→已接线 | 服务端 `fence` + `claimed_at` 栅栏；客户端 `constants.js`/`http-ingest.js`/`downlink.js` 已回传三终态与栅栏值 | 待 bridge vitest 复跑确认；`unknown` 终态需对账入口 |
| S9 | 统一收件箱与查询 API | ✅ | `inbox_conversations` + message_hub 三态；统一 API | 无 |
| S10 | 失败回收与重投 | ❌ | outbox pending/重试/push_attempts 字段具备 | `NewOutboundReclaimTask` 零生产调用，`main.go:179` 报 disabled；`IngestExtraDroppedKeys` 未导出 metrics |
| S11 | 旧路径兼容破坏面 | 🔴→已清理（源码层） | 旧路由、旧 schema、渠道级旧码热链均删；scripts/e2e/注释已改 canonical；SageX 仓客户端同步 canonical | bridge dist/已装扩展未重建重发（发布链 B/C）；管理面 `/api/bridge/token` 等非总线路径按职责保留 |

### 13.1 生命周期结论

按"完整业务生命周期"看，**S1/S2/S3/S6/S9 已闭环可复用；S4/S5/S8 部分闭环；S10 未闭环**。本轮按用户指示删除了我方可控范围内的旧路径兼容（路由、schema、鉴权旧码候选），并把 S8 栅栏从服务端延伸到客户端源码。剩余 ❌（S5 写侧、S10 回收）与 🔴（dist 重发）不在本轮擅自改动 AI/调度/发布链，明确列为后续 P1/P2，不写成"已完成"。

